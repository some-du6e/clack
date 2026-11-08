package composer

import (
	"context"
	"encoding/json"
	"image"
	"strings"
	"sync"
	"time"

	"gioui.org/layout"
	"gioui.org/op"
	"gioui.org/op/paint"

	"github.com/some-du6e/yoloslackgoclient/internal/core"
	"github.com/some-du6e/yoloslackgoclient/internal/taut"
	"github.com/some-du6e/yoloslackgoclient/internal/ui/popups"
	"github.com/some-du6e/yoloslackgoclient/internal/ui/slackc"
	"github.com/some-du6e/yoloslackgoclient/internal/ui/uipref"
)

// Composer is the message box under a channel or thread, or the inline
// editor of an existing message (NewEdit).
type Composer struct {
	h        Host
	api      coreAPI
	ch, ts   string
	edit     *core.Message // edit mode: the message being edited
	onDone   func()
	done     bool
	ed       *Editor
	ac       *Autocomplete
	ui       chrome
	att      *attachments
	st       EditorStyle
	focusReq bool
	focused  bool

	// Origin is the composer's top-left corner in window coordinates. The
	// parent sets it each frame (Gio has no global transform query); menus
	// and the emoji picker are anchored with it.
	Origin image.Point

	// OnEditLast is called when Up is pressed in an empty composer (Slack
	// edits your last message). The ui root wires it to the message list.
	OnEditLast func()

	// SaveEdit, when set, saves an inline edit instead of core.Edit
	// (ui/messages routes it through taut.Dispatch("commitMessageEdit")).
	// The blocks are the composer's own; plugin send transforms are the
	// saver's job (commitMessageEdit's native applies them).
	SaveEdit func(ctx context.Context, ch, ts, text string, blocks json.RawMessage) error

	broadcast  bool
	lastTyping time.Time
	typingOn   bool // typing reported since the composer was last empty
	lastVer    uint64
	emojiVer   uint64 // custom emoji set version the doc was last scanned at

	// Server draft sync (syncDraftCreate / syncDraftUpdates /
	// deleteDraftApi): see drafts_sync.go.
	dsync draftSync

	setBroadcastFn func(bool) // bound once: the plugin buttons' broadcast.set

	// Slack boundaries (slack.go): this frame's chrome state, the props
	// patched boundaries handed back, and widgets bound once so the
	// unpatched path allocates nothing.
	cs            chromeState
	paneP, inputP slackc.Props
	sendJS        slackc.JSFunc // a patched prepareAndSendMessage (send checks)
	pending       *outgoing     // the message sendJS is deciding on
	top           int           // context bar height above the box
	ctxShowing    bool          // onIsContextBarShowing
	drewInput     bool          // the editor was laid out (no roadblock)
	roadblock     string        // SetRoadblock: Slack's notice instead of the editor
	wPane, wInput layout.Widget
	wEditor, wAtt layout.Widget
	keyS, draftS  string
}

// plainTextLocal is the "Format messages with markup" choice made in
// Clack's Preferences; until then Slack's plain_text_mode pref decides.
var plainTextLocal *bool

// SetPlainTextMode sets Preferences > Advanced > "Format messages with
// markup" (Slack's plain_text_mode) for every composer.
func SetPlainTextMode(on bool) { plainTextLocal = &on }

// ResetPlainTextMode forgets the local choice (the account pref applies).
func ResetPlainTextMode() { plainTextLocal = nil }

// PlainTextMode reports the effective setting: the local choice, else the
// pref Slack sent (off by default: live formatting, like Slack).
func PlainTextMode(a *core.App) bool {
	if plainTextLocal != nil {
		return *plainTextLocal
	}
	return a != nil && coreAPI{a}.plainTextMode()
}

// showFormatting is Slack's "Show formatting" (Aa) toggle; one setting for
// every composer, like Slack's pref.
var showFormatting = true

// SetShowFormatting sets the Aa toggle for every composer (the ui root can
// restore it from prefs); ShowFormatting reads it.
func SetShowFormatting(on bool) { showFormatting = on }
func ShowFormatting() bool      { return showFormatting }

// New returns the composer for a channel (threadTS "") or a thread.
func New(h Host, channelID, threadTS string) *Composer {
	c := &Composer{h: h, api: coreAPI{h.Core()}, ch: channelID, ts: threadTS}
	c.init()
	if d := loadDraft(channelID, threadTS); d != nil {
		c.ed.SetDoc(d)
	}
	c.lastVer = c.ed.Version()
	return c
}

// NewEdit returns an inline editor for m (one of the viewer's messages).
// onDone runs after Save or Cancel; the caller then drops the composer.
func NewEdit(h Host, m core.Message, onDone func()) *Composer {
	c := &Composer{h: h, api: coreAPI{h.Core()}, ch: m.Channel, ts: m.ThreadTS, onDone: onDone}
	mm := m
	c.edit = &mm
	c.init()
	d := DocFromRichtext(m.Doc.Blocks)
	if d == nil || d.Len() == 0 {
		d = NewDoc()
		d.Insert(0, m.Text, Attr{})
	}
	c.ed.SetDoc(d)
	c.lastVer = c.ed.Version()
	c.focusReq = true
	return c
}

func (c *Composer) init() {
	c.ed = NewEditor()
	if c.api.a != nil {
		warmEmoji()
		c.ed.emoji = c.lookupEmoji
		c.emojiVer = c.api.emojiVersion()
	}
	c.ac = newAutocomplete(c.h, c.ch)
	bindAutocomplete(c.ed, c.ac, c.h.Invalidate)
	c.ac.later, c.ac.thread = c.later, c.ts != ""
	c.att = newAttachments(c.h)
	typingHub(c.h.Core()).addHost(c.h) // listen for typing events
	c.initIDs()
	c.wPane, c.wInput = c.layoutPaneBody, c.layoutInputBody
	c.setBroadcastFn = c.setBroadcast
	c.wEditor, c.wAtt = c.layoutEditor, c.att.Layout
}

// Focus asks for keyboard focus on the next frame.
func (c *Composer) Focus() { c.focusReq = true; c.h.Invalidate() }

// Done reports whether an edit composer was saved or cancelled.
func (c *Composer) Done() bool { return c.done }

// Editing reports whether this is an edit composer.
func (c *Composer) Editing() bool { return c.edit != nil }

// InsertText inserts plain text at the caret.
func (c *Composer) InsertText(s string) { c.ed.InsertText(s); c.Focus() }

// InsertEmoji inserts an emoji atom at the caret.
func (c *Composer) InsertEmoji(name string) {
	lo, hi := c.ed.Selection()
	if lo > hi {
		lo, hi = hi, lo
	}
	c.ed.InsertAtom(lo, hi, c.emojiAtom(name))
	c.Focus()
}

// Close stores the draft and releases background work.
func (c *Composer) Close() {
	c.saveDraft()
	c.ac.Close()
}

func (c *Composer) emojiAtom(name string) *Atom {
	name = strings.Trim(name, ":")
	url, txt, _ := c.api.emoji(name)
	return emojiAtomFrom(name, url, txt)
}

// lookupEmoji resolves a typed shortcode against the loaded set: the
// custom map (aliases followed) then the standard table, both O(1).
func (c *Composer) lookupEmoji(name string) (*Atom, bool) {
	url, txt, ok := c.api.emoji(name)
	if !ok {
		return nil, false
	}
	return emojiAtomFrom(name, url, txt), true
}

// syncEmojiSet converts shortcodes typed before the custom emoji set they
// name had loaded (or changed): once per new set version.
func (c *Composer) syncEmojiSet() {
	if c.api.a == nil {
		return
	}
	if v := c.api.emojiVersion(); v != c.emojiVer {
		c.emojiVer = v
		if c.ed.ConvertEmoji() {
			c.h.Invalidate()
		}
	}
}

// prepareSend is the document submit serializes: a copy with every
// remaining ":name:" shortcode as an emoji (Slack converts on send too)
// and, under "Format messages with markup", the typed markup parsed into
// formatting the way Slack's send does.
func (c *Composer) prepareSend(d *Doc) *Doc {
	doc := d.Clone()
	var look emojiLookup
	if c.api.a != nil {
		look = c.lookupEmoji
	}
	convertEmojiRange(doc, 0, doc.Len(), look)
	if PlainTextMode(c.api.a) && c.api.a != nil {
		doc = parseMarkup(doc, c.api.resolver())
	}
	return doc
}

// ---- layout ----

// Layout draws the composer. Channel/thread composers take the full width
// they're given (margins included) and size their height to the content;
// edit composers draw just the box.
func (c *Composer) Layout(gtx layout.Context) layout.Dimensions {
	runRestores()
	syncHooks()
	th := c.h.Theme()
	if c.st.Shaper != th.Shaper || c.st.Shaper == nil {
		c.st = c.editorStyle()
	}
	c.st.Images = c.emojiImage
	c.st.PlaceholderText = ""

	// Not in a measure pass (gtx.Disabled(): a patched boundary above the
	// composer, ChatTabs' ClientPrimaryView, measures it first), where a
	// FocusCmd goes nowhere and the request would be lost.
	if c.focusReq && gtx.Source.Enabled() {
		c.focusReq = false
		c.ed.Focus(gtx)
	}
	c.ed.plainText = PlainTextMode(c.api.a)
	c.syncEmojiSet()
	c.handleEditor(gtx)
	c.handleActions(gtx)

	// Slack's boundaries (slack.go): MessagePaneInput / InputContainer
	// (the context bar, the roadblock, MessageInput), or MessageInput for
	// an edit. Unpatched each is a direct call.
	c.cs = c.chromeState(gtx)
	c.ui.prime(&c.cs)
	c.top, c.drewInput = 0, false
	c.paneP, c.inputP = nil, nil
	var dims layout.Dimensions
	switch {
	case c.edit != nil:
		dims = slackc.Component(gtx, "MessageInput", c, (*Composer).inputProps, (*Composer).layoutInput)
	case c.ts != "":
		dims = slackc.Component(gtx, "InputContainer", c, (*Composer).paneProps, (*Composer).layoutPane)
	default:
		dims = slackc.Component(gtx, "MessagePaneInput", c, (*Composer).paneProps, (*Composer).layoutPane)
	}

	// Autocomplete after the editor laid out, anchored at the trigger,
	// with what plugins changed on TextyAutocomplete.
	c.ac.setOverrides(c.ui.f.acP)
	c.ac.Update(c.ed.Doc(), caretOf(c.ed))
	c.acceptAutocomplete()
	if c.ac.Open() && c.drewInput {
		c.layoutPopup(gtx)
	}
	c.handleActions(gtx)
	return dims
}

func caretOf(e *Editor) int { _, car := e.Selection(); return car }

func (c *Composer) editorStyle() EditorStyle {
	th := c.h.Theme()
	st := DefaultEditorStyle(th.Shaper)
	p := &th.C
	st.Color = p.Fg
	st.Caret = p.Fg
	st.LinkColor = p.Link
	st.MentionColor = p.Mention
	st.MentionBg = p.MentionBg
	st.BroadcastColor = p.Mention
	st.BroadcastBg = p.SelfMention
	st.CodeColor = p.CodeFg
	st.CodeBg = p.CodeBg
	st.CodeBorder = p.CodeBorder
	st.PreBg = p.PreBg
	st.PreBorder = p.CodeBorder
	st.QuoteBar = p.QuoteBar
	st.Selection = p.Selection
	st.Placeholder = p.FgSubtle
	st.MaxHeight = 460 - 16
	return st
}

func (c *Composer) emojiImage(url string, px int) (paint.ImageOp, bool) {
	if url == "" {
		return paint.ImageOp{}, false
	}
	return c.h.Images().Get(url, px)
}

func (c *Composer) layoutPopup(gtx layout.Context) {
	org := c.ui.InputOffset().Add(image.Pt(0, c.top))
	r := c.ed.PosRect(c.ac.Anchor())
	left := org.X + r.Min.X - gtx.Dp(17)
	bottom := org.Y + r.Min.Y - gtx.Dp(5)
	// The popup may extend above the composer (it floats over the
	// message list), up to the composer's top-of-window position.
	avail := gtx.Constraints.Max
	pg := gtx
	pg.Constraints = layout.Constraints{Max: image.Pt(max(avail.X-left, gtx.Dp(420)), gtx.Dp(300))}
	m := op.Record(gtx.Ops)
	d := c.ac.Layout(pg, gtx.Dp(575))
	call := m.Stop()
	if d.Size.Y == 0 {
		return
	}
	left = max(left, 0)
	if left+d.Size.X > avail.X {
		left = max(avail.X-d.Size.X, 0)
	}
	off := op.Offset(image.Pt(left, bottom-d.Size.Y)).Push(gtx.Ops)
	op.Defer(gtx.Ops, call)
	off.Pop()
}

// acceptAutocomplete applies a queued pick (only used when no OnAccept is
// bound; bindAutocomplete applies picks synchronously).
func (c *Composer) acceptAutocomplete() {
	it, s, e, ok := c.ac.Accepted()
	if !ok {
		return
	}
	applyPick(c.ed, it, s, e)
	c.h.Invalidate()
}

// bindAutocomplete connects the popup to the editor. Picks are applied
// inside the key event that made them (OnAccept) and keys re-sync against
// the live document (Live), so text typed in the same input batch as the
// Tab/Enter lands after the inserted pill instead of being reordered.
func bindAutocomplete(ed *Editor, ac *Autocomplete, invalidate func()) {
	ed.Intercept = ac.Key
	ac.Live = func() (*Doc, int) { return ed.Doc(), caretOf(ed) }
	ac.OnAccept = func(it acItem, start, end int) {
		applyPick(ed, it, start, end)
		if invalidate != nil {
			invalidate()
		}
	}
}

// applyPick replaces the trigger token [start, end) with the pick: a pill
// (mention, channel, emoji) or the command text, caret after it.
func applyPick(ed *Editor, it acItem, start, end int) {
	if it.Kind == acCommand {
		ed.Replace(start, end, "/"+it.Name+" ")
	} else if it.Atom != nil {
		ed.InsertAtom(start, end, it.Atom)
	}
}

func (c *Composer) chromeState(gtx layout.Context) chromeState {
	st := chromeState{
		Focused:        c.ed.Focused(gtx),
		ShowFormatting: showFormatting && !c.ed.plainText, // Slack hides the toolbar under markup mode
		Active:         c.ed.ActiveStyle(),
		Block:          c.ed.ActiveBlock(),
		Link:           c.ed.ActiveLink(),
		CanSend:        !c.ed.Empty() || c.att.Len() > 0,
		Broadcast:      c.broadcast,
		SetBroadcast:   c.setBroadcastFn,
		Origin:         c.Origin,
		Key:            c.key(),
		Channel:        c.ch,
		ThreadTS:       c.ts,
		DraftID:        c.draftID(),
		ViewContext:    c.viewContext(),
		Count:          c.ed.Doc().Len(),
	}
	if lo, hi := c.ed.Selection(); lo <= hi {
		st.SelLo, st.SelN = lo, hi-lo
	} else {
		st.SelLo, st.SelN = hi, lo-hi
	}
	st.InCode = st.Block == CodeBlock
	st.ShowPlaceholder = c.ed.Empty() && c.ed.Doc().Len() == 0
	switch {
	case c.edit != nil:
		st.Mode = modeEdit
		st.ShowPlaceholder = false
	case c.ts != "":
		st.Mode = modeThread
		st.Placeholder = "Reply to thread…"
	default:
		st.Mode = modeChannel
	}
	if cv, ok := c.api.conv(c.ch); ok {
		st.ChannelName = cv.Name
		switch cv.Kind {
		case "private":
			st.Private = true
		case "im", "mpim":
			st.IsDM = true
		}
		if st.Mode == modeChannel {
			if st.IsDM {
				st.Placeholder = "Message " + cv.Name
			} else {
				st.Placeholder = "Message " + cv.Name // the chrome draws the #/lock icon before the name
			}
		}
	} else if st.Mode == modeChannel {
		st.Placeholder = "Message"
	}
	if uipref.Get().ShowTyping { // Messages & media > "Display information about who is currently typing"
		st.TypingIDs, st.TypingNames = typingHub(c.h.Core()).typers(c.ch, c.ts, c.api.self().ID)
	}
	st.Typing = typingLine(st.TypingNames)
	st.ScrolledTop = c.ed.scrollY > 0
	st.ScrolledBottom = c.ed.scrollY+c.ed.viewH < c.ed.contentH
	if st.ThreadTS != "" && slackc.Patched(tautButtons) {
		st.ChannelType = c.channelType() // only plugin buttons read it
	}
	return st
}

// ---- editor events ----

func (c *Composer) handleEditor(gtx layout.Context) {
	for {
		ev, ok := c.ed.Update(gtx)
		if !ok {
			break
		}
		switch ev.Kind {
		case EvChange:
			c.changed()
		case EvSubmit:
			c.submit()
		case EvEditLast:
			if c.edit == nil && c.OnEditLast != nil {
				c.OnEditLast()
			}
		case EvCancel:
			if c.edit != nil {
				c.finishEdit()
			}
		case EvLinkDialog:
			c.openLink()
		case EvPasted:
			c.att.PasteClipboardImage()
		case EvFocus:
			c.focused = true
		case EvBlur:
			c.focused = false
		}
	}
}

func (c *Composer) changed() {
	v := c.ed.Version()
	if v == c.lastVer {
		return
	}
	c.lastVer = v
	if c.edit != nil {
		return
	}
	c.saveDraft()
	c.syncDraftSoon()
	if c.ed.Empty() {
		c.typingStopped()
	} else if time.Since(c.lastTyping) > 3*time.Second {
		c.lastTyping = time.Now()
		c.typingOn = true
		api, ch, ts := c.api, c.ch, c.ts
		go sendTyping(api, ch, ts)
	}
}

func (c *Composer) handleActions(gtx layout.Context) {
	for _, a := range c.ui.Actions() {
		a.Anchor = a.Anchor.Add(c.Origin).Add(image.Pt(0, c.top))
		switch a.Kind {
		case caFocus:
			c.ed.Focus(gtx)
		case caStyle:
			if !(c.ed.ActiveBlock() == CodeBlock) {
				c.ed.ToggleStyle(a.Style)
			}
			c.ed.Focus(gtx)
		case caBlock:
			c.ed.ToggleBlock(a.Block)
			c.ed.Focus(gtx)
		case caLink:
			c.openLink()
		case caFormatting:
			showFormatting = !showFormatting
			c.ed.Focus(gtx)
		case caAttach:
			c.att.Menu(a.Anchor, c.ch)
		case caEmoji:
			c.h.OpenEmojiPicker(image.Pt(a.Anchor.Min.X, a.Anchor.Min.Y), func(name string) { c.InsertEmoji(name) })
		case caMention:
			c.insertTrigger("@")
			c.ed.Focus(gtx)
		case caShortcut:
			if c.ed.Empty() {
				c.ed.SetDoc(NewDoc())
				c.ed.InsertText("/")
			} else {
				c.insertTrigger("/")
			}
			c.ed.Focus(gtx)
		case caVideo, caAudio:
			c.h.Toast("Recording clips isn't available in yoloslack yet")
		case caSend:
			c.submit()
		case caSchedule:
			c.scheduleMenu(a.Anchor)
		case caBroadcast:
			c.broadcast = !c.broadcast
		case caSave:
			c.submit()
		case caCancel:
			c.finishEdit()
		case caParagraph:
			c.paragraphMenu(a.Anchor)
		case caDivider:
			c.ed.InsertDivider()
			c.ed.Focus(gtx)
		}
		c.h.Invalidate()
	}
}

// insertTrigger types s at the caret, with a space first if the caret
// follows a word (so @ and / open autocomplete).
func (c *Composer) insertTrigger(s string) {
	_, car := c.ed.Selection()
	d := c.ed.Doc()
	if car > 0 && d.Runes[car-1] != ' ' && d.Runes[car-1] != '\n' {
		s = " " + s
	}
	c.ed.InsertText(s)
}

func (c *Composer) openLink() {
	lo, hi, url := c.ed.LinkAt()
	txt := c.ed.Doc().String(lo, hi)
	if strings.ContainsRune(txt, ObjRune) {
		txt = c.ed.Doc().Slice(lo, hi).Text()
	}
	if lo != hi {
		c.ed.SetSelection(lo, hi)
	}
	openLinkDialog(c.h, txt, url, func(text, url string) {
		c.ed.SetLink(text, url)
		c.Focus()
	}, c.Focus)
}

func (c *Composer) paragraphMenu(anchor image.Rectangle) {
	// Slack's ¶ menu: Paragraph and Big/Medium/Small heading, the caret's
	// current kind checked. Picking one sets the selected lines to it.
	cur := c.ed.ActiveBlock()
	item := func(label string, k BlockKind) popups.MenuItem {
		return popups.MenuItem{Label: label, Checked: cur == k, Do: func() {
			c.ed.SetBlock(k)
			c.Focus()
		}}
	}
	items := []popups.MenuItem{
		item("Paragraph", Para),
		item("Big heading", Header1),
		item("Medium heading", Header2),
		item("Small heading", Header3),
	}
	c.h.ShowOverlay(popups.NewMenuOpts(c.h, items, popups.MenuOpts{Width: 248, Anchor: anchor, Side: popups.Above}))
}

func (c *Composer) scheduleMenu(anchor image.Rectangle) {
	if !c.ed.Empty() || c.att.Len() > 0 {
		now := time.Now()
		tomorrow := time.Date(now.Year(), now.Month(), now.Day()+1, 9, 0, 0, 0, now.Location())
		monday := tomorrow
		for monday.Weekday() != time.Monday {
			monday = monday.AddDate(0, 0, 1)
		}
		sched := func(t time.Time) func() {
			return func() { c.schedule(t) }
		}
		items := []popups.MenuItem{
			{Header: "Schedule message"},
			{Label: "Tomorrow at 9:00 AM", Do: sched(tomorrow)},
			{Label: "Monday at 9:00 AM", Do: sched(monday)},
			popups.Sep(),
			{Label: "Custom time…", Do: func() {
				openSchedulePicker(c.h, func(t time.Time) { c.schedule(t) })
			}},
		}
		c.h.ShowOverlay(popups.NewMenuOpts(c.h, items, popups.MenuOpts{Width: 260, Anchor: anchor, Side: popups.Above, AlignRight: true}))
	}
}

// schedule posts the current message at time t via core (chat scheduling
// goes through drafts in core). Files aren't scheduled (Slack schedules
// text/blocks only); a scheduled message clears the composer like a send.
func (c *Composer) schedule(t time.Time) {
	if c.edit != nil || c.ed.Empty() {
		return
	}
	if !t.After(time.Now()) {
		c.h.Toast("Pick a time in the future")
		return
	}
	doc := c.prepareSend(c.ed.Doc())
	text, blocks := doc.Mrkdwn(), doc.RichTextBlocks()
	api, ch, ts := c.api, c.ch, c.ts
	broadcast := c.broadcast && ts != ""
	when := t.Format(uipref.Layout("Mon Jan 2 at 3:04 PM"))
	c.clearAfterSend()
	go func() {
		ctx := context.Background()
		text, blocks := transformBlocks(ctx, text, blocks)
		_, err := api.scheduleMessage(ctx, ch, ts, text, blocks, broadcast, t.Unix())
		if err != nil {
			c.later(func() { c.h.Toast("Couldn't schedule: " + err.Error()) })
			c.restore(doc)
			return
		}
		c.later(func() { c.h.Toast("Message scheduled for " + when) })
	}()
}

// ---- sending ----

func (c *Composer) submit() {
	d := c.ed.Doc()
	if d.Empty() && c.att.Len() == 0 {
		if c.edit != nil {
			// Slack asks to delete when an edit is emptied; keep it simple.
			c.h.Toast("A message can't be empty")
		}
		return
	}
	doc := c.prepareSend(d)
	text := doc.Mrkdwn()
	blocks := doc.RichTextBlocks()
	api, ch, ts := c.api, c.ch, c.ts

	if c.edit != nil {
		m := c.edit
		save := c.SaveEdit
		go func() {
			ctx := context.Background()
			var err error
			if save != nil {
				err = save(ctx, m.Channel, m.TS, text, blocks)
			} else {
				text, blocks := transformBlocks(ctx, text, blocks)
				err = api.edit(ctx, m.Channel, m.TS, text, blocks)
			}
			if err != nil {
				c.later(func() { c.h.Toast("Couldn't save your edit: " + err.Error()) })
			}
		}()
		c.finishEdit()
		return
	}

	// Slash commands (only when the message starts with a known command
	// or any /word; Slack sends unknown /words as commands too).
	if c.att.Len() == 0 {
		plain := strings.TrimSpace(d.Text()) // as typed: markup mode must not eat a command's * or _
		if strings.HasPrefix(plain, "/") && !strings.HasPrefix(plain, "//") {
			cmd, rest, _ := strings.Cut(plain, " ")
			c.clearAfterSend()
			go func() {
				res, err := api.command(context.Background(), core.CommandOpts{Channel: ch, Command: cmd, Text: strings.TrimSpace(rest), ThreadTS: ts})
				if err != nil {
					c.later(func() { c.h.Toast(cmd + " failed: " + err.Error()) })
					c.restore(doc)
					return
				}
				if res.Response != "" {
					c.later(func() { c.h.Toast(res.Response) })
				}
			}()
			return
		}
	}

	o := &outgoing{doc: doc, text: text, blocks: blocks, broadcast: c.broadcast && ts != ""}
	c.clearAfterSend()
	switch {
	case c.sendJS.ID != 0:
		c.sendViaJS(o)
	case SendChecks != nil:
		c.checkThenSend(o)
	default:
		c.send(o)
	}
}

// outgoing is a message on its way out of the composer.
type outgoing struct {
	doc       *Doc
	text      string
	blocks    json.RawMessage
	broadcast bool
}

// send posts the message (with any attached files) through Slack's
// prepareAndSendMessage → sendMessage thunks.
func (c *Composer) send(o *outgoing) {
	prepared, rawFiles, hasFiles := c.att.uploadBatch()
	if hasFiles {
		c.att.SetUploading(true)
	}
	job := &sendJob{api: c.api, ch: c.ch, ts: c.ts, o: o, prepared: prepared, raw: rawFiles, files: hasFiles,
		draftID: c.draftID(), view: c.viewContext()}
	go func() {
		err := job.run(context.Background())
		if hasFiles {
			c.later(func() {
				c.att.SetUploading(false)
				if err != nil {
					c.h.Toast("Upload failed: " + err.Error())
					return
				}
				c.att.Clear()
			})
		} else if err != nil {
			c.later(func() { c.h.Toast("Message not sent: " + err.Error()) })
		}
		if err != nil {
			c.restore(o.doc)
		}
	}()
}

// sendJob is one outgoing message; run happens off the UI goroutine.
type sendJob struct {
	api           coreAPI
	ch, ts        string
	o             *outgoing
	prepared      []core.PreparedUpload
	raw           []core.UploadFile
	files         bool
	draftID, view string
}

func (j *sendJob) run(ctx context.Context) error {
	if t := currentTaut(); t.ThunkPatched(thPrepareAndSend) {
		args := prepareSendArgs{ChannelID: j.ch, ReplyToTs: j.ts, Delta: j.o.doc.Delta(), Text: j.o.text, Blocks: j.o.blocks,
			ShouldBroadcast: j.o.broadcast, Broadcast: j.o.broadcast, DraftID: j.draftID, ViewContext: j.view}
		for _, p := range j.prepared {
			args.FileIDs = append(args.FileIDs, p.ID)
		}
		_, err := taut.Dispatch(ctx, t, thPrepareAndSend, args, func(ctx context.Context, a prepareSendArgs) (any, error) {
			broadcast := (a.ShouldBroadcast || a.Broadcast) && a.ReplyToTs != ""
			return nil, j.deliver(ctx, a.ChannelID, a.ReplyToTs, a.Text, a.Blocks, broadcast)
		})
		return err
	}
	return j.deliver(ctx, j.ch, j.ts, j.o.text, j.o.blocks, j.o.broadcast)
}

// deliver is prepareAndSendMessage's native: share the files with the
// message as their comment, or post the message.
func (j *sendJob) deliver(ctx context.Context, ch, ts, text string, blocks json.RawMessage, broadcast bool) error {
	if j.files {
		comment := text
		if j.o.doc.Empty() {
			comment = ""
		}
		return sendFiles(ctx, j.api, ch, ts, comment, blocks, j.prepared, j.raw)
	}
	return postMessage(ctx, j.api, ch, ts, text, blocks, broadcast)
}

// checkThenSend runs the SendChecks hook (composer.addSendCheck) and
// sends, or puts the message back (files stay attached).
func (c *Composer) checkThenSend(o *outgoing) {
	check := SendChecks
	if check == nil {
		c.send(o)
		return
	}
	check(SendCheck{ChannelID: c.ch, ThreadTS: c.ts, Blocks: o.blocks, Text: o.text}, func(ok bool) {
		c.later(func() {
			if ok {
				c.send(o)
			} else {
				c.restore(o.doc)
			}
		})
	})
}

// sendViaJS sends through a plugin's prepareAndSendMessage (taut's send
// checks wrap it on MessagePaneInput / InputContainer). It calls back
// into prepareAndSend to really send; a thrown error or rejected promise
// puts the message back.
func (c *Composer) sendViaJS(o *outgoing) {
	c.pending = o
	opts := slackc.Props{
		"delta":           o.doc.Delta(),
		"channelId":       c.ch,
		"shouldBroadcast": o.broadcast,
		"blocks":          o.blocks,
		"text":            o.text,
	}
	if c.ts != "" {
		opts["replyToTs"] = c.ts
	}
	c.sendJS.CallResult(func(_ any, err error) {
		if c.pending == o {
			c.pending = nil // a plugin sent it itself, or held it back
		}
		if err != nil {
			c.restore(o.doc)
		}
	}, opts)
}

// prepareAndSend is the prepareAndSendMessage({delta, channelId,
// replyToTs, shouldBroadcast}) MessagePaneInput and InputContainer pass
// to JS. It sends the message the composer handed to sendViaJS, with a
// plugin's shouldBroadcast override (SlimMessageBox).
func (c *Composer) prepareAndSend(opts slackc.Props) {
	o := c.pending
	c.pending = nil
	if o == nil {
		return
	}
	if opts.Has("shouldBroadcast") {
		o.broadcast = opts.Bool("shouldBroadcast") && c.ts != ""
	}
	c.checkThenSend(o)
}

func (c *Composer) clearAfterSend() {
	c.ed.SetDoc(NewDoc())
	c.lastVer = c.ed.Version()
	c.deleteServerDraft("message_sent")
	c.broadcast = false
	c.lastTyping = time.Time{}
	c.typingStopped()
	clearDraft(c.ch, c.ts)
	c.h.Invalidate()
}

// restore puts an unsent message back if the user hasn't typed since.
func (c *Composer) restore(d *Doc) {
	c.later(func() {
		if c.ed.Empty() {
			c.ed.SetDoc(d)
			c.lastVer = c.ed.Version()
			c.saveDraft()
		}
	})
}

// later runs f on the UI goroutine at the next composer Layout.
func (c *Composer) later(f func()) {
	restoreQ.Lock()
	restoreQ.fns = append(restoreQ.fns, f)
	restoreQ.Unlock()
	c.h.Invalidate()
}

func (c *Composer) finishEdit() {
	if c.done {
		return
	}
	c.done = true
	if c.onDone != nil {
		c.onDone()
	}
	c.h.Invalidate()
}

// restoreQ runs editor mutations from background goroutines on the UI
// goroutine (the next Layout of any composer).
var restoreQ struct {
	sync.Mutex
	fns []func()
}

func runRestores() {
	restoreQ.Lock()
	fns := restoreQ.fns
	restoreQ.fns = nil
	restoreQ.Unlock()
	for _, f := range fns {
		f()
	}
}

// ---- drafts (memory only: message contents never touch disk) ----

var drafts struct {
	sync.Mutex
	m map[string]*Doc
}

func draftKey(ch, ts string) string { return ch + "/" + ts }

func loadDraft(ch, ts string) *Doc {
	drafts.Lock()
	defer drafts.Unlock()
	if d := drafts.m[draftKey(ch, ts)]; d != nil {
		return d.Clone()
	}
	return nil
}

func (c *Composer) saveDraft() {
	if c.edit != nil {
		return
	}
	drafts.Lock()
	defer drafts.Unlock()
	if drafts.m == nil {
		drafts.m = map[string]*Doc{}
	}
	k := draftKey(c.ch, c.ts)
	if c.ed.Empty() {
		delete(drafts.m, k)
		return
	}
	drafts.m[k] = c.ed.Doc().Clone()
}

func clearDraft(ch, ts string) {
	drafts.Lock()
	delete(drafts.m, draftKey(ch, ts))
	drafts.Unlock()
}

// HasDraft reports whether a channel or thread has an unsent draft (for
// the sidebar's draft pencil).
func HasDraft(ch, ts string) bool {
	drafts.Lock()
	defer drafts.Unlock()
	_, ok := drafts.m[draftKey(ch, ts)]
	return ok
}

// setBroadcast sets the thread composer's "Also send to" checkbox (a
// plugin button's broadcast.set). Runs on the UI goroutine (slackc calls
// Go funcs from its pump).
func (c *Composer) setBroadcast(on bool) {
	if c.ts == "" || c.broadcast == on {
		return
	}
	c.broadcast = on
	c.h.Invalidate()
}
