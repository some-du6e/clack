package composer

import (
	"context"
	"encoding/json"
	"image"
	"strings"
	"sync"
	"time"

	"gioui.org/font"
	"gioui.org/layout"
	"gioui.org/op"
	"gioui.org/op/paint"
	"gioui.org/widget"

	"github.com/some-du6e/clack/internal/core"
	"github.com/some-du6e/clack/internal/i18n"
	"github.com/some-du6e/clack/internal/richtext"
	"github.com/some-du6e/clack/internal/taut"
	"github.com/some-du6e/clack/internal/ui/popups"
	"github.com/some-du6e/clack/internal/ui/slackc"
	"github.com/some-du6e/clack/internal/ui/uipref"
)

// Composer is the message box under a channel or thread, or the inline
// editor of an existing message (NewEdit).
type Composer struct {
	h      Host
	api    coreAPI
	ch, ts string
	edit   *core.Message // edit mode: the message being edited
	// unsendBy is when the message just sent stops being unsendable
	// (Slack's messageEligibleForUnsending, expiryTs = send + 15 s).
	unsendBy time.Time
	onDone   func()
	done     bool
	ed       *Editor
	ac       *Autocomplete
	ui       chrome
	att      *attachments
	st       EditorStyle
	focusReq bool
	// focus dropped by Gio without a blur (restoreDroppedFocus)
	focusPress    uint64 // pressCount when last seen focused
	focusRestored bool   // asked for it back once since
	focused       bool

	// Origin is the composer's top-left corner in window coordinates. The
	// parent sets it each frame (Gio has no global transform query); menus
	// and the emoji picker are anchored with it.
	Origin image.Point

	// OnEditLast is called when Up is pressed in an empty composer (Slack
	// edits your last message). The ui root wires it to the message list.
	OnEditLast func()
	// OnFocusList moves keyboard focus to the message list with its newest
	// message selected (Accessibility > "Press ↑ in the empty message
	// field to: Move focus to the message list").
	OnFocusList func()

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
	// charsOverLimit at limitVer (overLimit: counted once per change)
	limitVer  uint64
	limitOver int
	limitOK   bool

	// Server draft sync (syncDraftCreate / syncDraftUpdates /
	// deleteDraftApi): see drafts_sync.go.
	dsync draftSync
	// the files in the draft (draft_files.go): attVer counts changes to
	// them; touched is set once the user (or a restore) filled the
	// composer, after which server drafts no longer restore into it;
	// srvGen is the core.DraftsGen last looked at.
	attVer  uint64
	touched bool
	srvGen  uint64

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
	rbLink, rbURL string        // SetRoadblockLink: a link after it ("Learn more")
	rbClick       widget.Clickable
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
// every composer, like Slack's pref: the account pref
// msg_input_sticky_composer (boot-shared Ex/u kx reads it; ComposerButton's
// click writes it with setUserPrefByApi, WysiwygContainer). Without a
// signed-in core (tests) it is this package value alone.
var showFormatting = true

// formattingWrite is the Aa value on its way to users.prefs.set: until
// the write lands (or fails) it stands in for the pref.
var formattingWrite struct {
	sync.Mutex
	pending int
	on      bool
}

// syncShowFormatting reads msg_input_sticky_composer (shown when Slack
// sends no value: the toolbar is on by default).
func syncShowFormatting(api coreAPI) {
	if api.a == nil {
		return
	}
	formattingWrite.Lock()
	pending, on := formattingWrite.pending, formattingWrite.on
	formattingWrite.Unlock()
	if pending > 0 {
		showFormatting = on
		return
	}
	showFormatting = api.a.PrefBool("msg_input_sticky_composer", true)
}

// toggleShowFormatting is ComposerButton's click: flip the toolbar and
// store the choice in msg_input_sticky_composer, off the UI goroutine.
func (c *Composer) toggleShowFormatting() {
	showFormatting = !showFormatting
	if c.api.a == nil {
		return
	}
	on, api := showFormatting, c.api
	formattingWrite.Lock()
	formattingWrite.pending++
	formattingWrite.on = on
	formattingWrite.Unlock()
	go func() {
		err := api.a.SetPref(context.Background(), "msg_input_sticky_composer", on)
		formattingWrite.Lock()
		formattingWrite.pending--
		formattingWrite.Unlock()
		if err != nil {
			c.later(func() {}) // redraw with the pref Slack kept
		}
	}()
}

// UpArrowShortcut is Accessibility > "Press ↑ in the empty message field
// to:" (Slack's client-side composerUpArrowShortcutBehavior, uMEH): 0
// unset, 1 LIST (focus the message list), 2 LIST_IF_EMPTY, 3 EDIT. The ui
// root sets it from ui.json.
var UpArrowShortcut = func() int { return 0 }

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
	c.ed.Snippets = c.edit == nil // MessageInput's enableSnippetCreation; not the edit composer
	c.bindSlugs()
	if c.api.a != nil {
		warmEmoji()
		c.ed.emoji = c.lookupEmoji
		c.emojiVer = c.api.emojiVersion()
	}
	c.ac = newAutocomplete(c.h, c.ch)
	bindAutocomplete(c.ed, c.ac, c.h.Invalidate)
	c.ac.later, c.ac.thread, c.ac.threadTS = c.later, c.ts != "", c.ts
	if c.edit == nil { // the conversation's files, kept across composers
		c.att = attachmentsFor(draftKey(c.ch, c.ts), c.h)
		c.att.setOnChange(c.attChanged)
	} else {
		c.att = newAttachments(c.h)
	}
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
	c.resolvePendingSlugs(doc)
	var look emojiLookup
	if c.api.a != nil {
		look = c.lookupEmoji
	}
	convertEmojiRange(doc, 0, doc.Len(), look)
	convertNativeEmojiRange(doc, 0, doc.Len(), look)
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
	c.maybeRestoreServerDraft()
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
	c.ed.smart = SmartSymbols(c.api.a)
	syncShowFormatting(c.api)
	c.ed.upMode, c.ed.upBrowse = UpArrowShortcut(), c.edit == nil && c.api.a != nil && c.api.upToBrowse()
	c.syncEmojiSet()
	c.handleEditor(gtx)
	c.restoreDroppedFocus(gtx)
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
		// the pick's recordFrecencyEvent ran in Autocomplete.accept
		// (recordPick), for every list
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
		OverLimit:      c.overLimit(),
	}
	if isHeading(st.Block) && c.api.a != nil {
		st.HeadingFormats = c.api.a.Experiment("rich_text_headers_and_dividers") == "on"
	}
	if d := c.ed.Doc(); d.Len() == 0 && (len(d.Lines) == 0 || d.Lines[0].Kind == Para) {
		st.InputEmpty = true // checkIfTextyIsEmptyOfTextOrFormats
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
		// Slack's MessageInput: "Reply to thread…" only under the
		// nested_secondary_view experiment, else "Reply…".
		st.Placeholder = i18n.T("threads", "Reply…")
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
	if uipref.Get().ShowTyping {
		if name, status := typingHub(c.h.Core()).assistantLine(c.ch, c.ts, c.api.self().ID); status != "" {
			st.Typing, st.TypingStatus, st.TypingAgent = name+" "+status, status, name // AiAppStatusIndicator
		}
	}
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
		case EvFocusList:
			if c.edit == nil && c.OnFocusList != nil {
				c.OnFocusList()
			}
		case EvUndoEmpty:
			if !c.tryUnsend() {
				c.ed.Undo()
			}
		case EvCancel:
			if c.edit != nil {
				c.finishEdit()
			}
		case EvLinkDialog:
			c.openLink()
		case EvPasted:
			c.att.PasteClipboardImage()
		case EvPasteLimit:
			c.pasteLimitDialog(c.ed.RefusedPaste())
		case EvSnippet:
			c.openSnippet(true)
		case EvPlusMenu:
			// onPlusMenuShortcut: forcePlusMenuOpen, the + menu at its button
			if c.edit == nil && c.ui.rect[crAttach] != (image.Rectangle{}) && !c.ui.hidden(crAttach) {
				c.att.Menu(c.ui.rect[crAttach].Add(c.Origin).Add(image.Pt(0, c.top)), c.plusMenu())
			}
		case EvNavBack, EvNavForward:
			if n, ok := c.h.(historyNavigator); ok && c.edit == nil {
				if ev.Kind == EvNavBack {
					n.Back()
				} else {
					n.Forward()
				}
			}
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
	c.touched = true
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
			c.toggleShowFormatting()
			c.ed.Focus(gtx)
		case caAttach:
			c.att.Menu(a.Anchor, c.plusMenu())
		case caEmoji:
			c.h.OpenEmojiPicker(image.Pt(a.Anchor.Min.X, a.Anchor.Min.Y), func(name string) { c.InsertEmoji(name) })
		case caMention:
			c.mentionTrigger()
			c.ed.Focus(gtx)
		case caShortcut:
			// maybeTriggerSlashCommandsAutocomplete: only into an empty
			// input without a block format (the button is disabled
			// otherwise)
			if d := c.ed.Doc(); d.Len() == 0 && d.Lines[0].Kind == Para {
				c.ed.InsertText("/")
			}
			c.ed.Focus(gtx)
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

// plusMenu is the + menu's composer side: the conversation and the
// Text snippet row's snippet dialog (create-text-snippet, the same dialog
// Ctrl+Shift+Enter opens with the message).
func (c *Composer) plusMenu() plusMenu {
	return plusMenu{ch: c.ch, snippet: func() { c.openSnippet(false) }}
}

// mentionTrigger is the @ button's maybeTriggerMentionAutocomplete: it
// replaces the selection with "@" (an "@" right before the caret is
// replaced, not doubled) with a space first when the character before
// the caret is text other than a space (getText(index-1, 1): a newline
// counts as text, an emoji or mention pill has none), and leaves the
// caret after it so the member autocomplete opens. Nothing happens with
// the caret between two characters of one mention.
func (c *Composer) mentionTrigger() {
	lo, hi := c.ed.Selection()
	if lo > hi {
		lo, hi = hi, lo
	}
	d := c.ed.Doc()
	start, s := lo, "@"
	if lo > 0 && d.Attrs[lo-1].Atom == nil {
		switch d.Runes[lo-1] {
		case '@':
			start = lo - 1
		case ' ':
		default:
			s = " @"
		}
	}
	c.ed.Replace(start, hi, s)
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
	// (ComposerMenuTrigger: radio items with IconForFormat, the caret's
	// kind checked, paragraph for anything that isn't a heading)
	cur := c.ed.ActiveBlock()
	if !isHeading(cur) {
		cur = Para
	}
	item := func(label, icon string, k BlockKind) popups.MenuItem {
		return popups.MenuItem{Label: label, Icon: icon, Radio: true, Checked: cur == k, Do: func() {
			c.ed.SetBlock(k)
			c.Focus()
		}}
	}
	items := []popups.MenuItem{
		item("Paragraph", "paragraph", Para),
		item("Big heading", "heading-1", Header1),
		item("Medium heading", "heading-2", Header2),
		item("Small heading", "heading-3", Header3),
	}
	c.h.ShowOverlay(popups.NewMenuOpts(c.h, items, popups.MenuOpts{Width: 248, Anchor: anchor, Side: popups.Above}))
}

func (c *Composer) scheduleMenu(anchor image.Rectangle) {
	if c.overLimit() > 0 || c.ed.Empty() && c.att.Len() == 0 {
		return
	}
	// ScheduleSendMenu (schedule_menu.go): the rows for now, the Custom
	// time dialog last
	now, m := time.Now(), c.scheduleMember()
	h24 := uipref.Get().Time24
	exp := c.api.a != nil && c.api.a.Experiment("updated_scheduled_sends_desktop") == "on"
	items := []popups.MenuItem{{Header: "Schedule message"}}
	for _, r := range scheduleRows(now, h24, exp, m) {
		r := r
		if r.opt == schedCustom {
			items = append(items, popups.Sep(), popups.MenuItem{Label: r.label, Do: func() {
				// openScheduleMessageDialog
				m := c.scheduleMember()
				openSchedulePicker(c.h, schedCustomDefault(time.Now(), m), m, func(t time.Time) {
					lastCustomSchedule = t
					c.schedule(t)
				})
			}})
			continue
		}
		items = append(items, popups.MenuItem{Label: r.label, Subtitle: r.desc, Do: func() {
			at := r.at
			if r.opt < schedMember { // the time for the option when it is picked
				at = schedStandardTime(r.opt, time.Now())
			}
			c.schedule(at)
		}})
	}
	c.h.ShowOverlay(popups.NewMenuOpts(c.h, items, popups.MenuOpts{Width: 260, Anchor: anchor, Side: popups.Above, AlignRight: true}))
}

// scheduleMember is the DM's other member for the schedule menu's time
// zone rows (getMemberFromDraftForTimeZoneDetails: exactly one member).
func (c *Composer) scheduleMember() *schedMemberInfo {
	if c.api.a == nil {
		return nil
	}
	cv, ok := c.api.conv(c.ch)
	if !ok || cv.Kind != "im" || cv.User == "" {
		return nil
	}
	u, ok := c.api.user(cv.User)
	if !ok || u.TZ == "" && u.TZOffset == 0 {
		return nil
	}
	me := c.api.self()
	return &schedMemberInfo{name: u.Name, loc: schedMemberLoc(u), sameTZ: u.TZ == "" || u.TZ == me.TZ, offset: u.TZOffset != 0}
}

// scheduleFailedText is scheduleSendMessage's ephemeral when the
// schedule fails.
const scheduleFailedText = "We’re not sure what happened, but your message couldn’t be scheduled. It’s been saved in your Drafts so you can try again."

// schedule posts the current message at time t via core (chat scheduling
// goes through drafts in core). Files aren't scheduled (Slack schedules
// text/blocks only); a scheduled message clears the composer like a send.
func (c *Composer) schedule(t time.Time) {
	if c.edit != nil || c.ed.Empty() || !t.After(time.Now()) {
		return // the menu's times are ahead; the dialog only takes 5 min to 120 days out
	}
	doc := c.prepareSend(c.ed.Doc())
	text, blocks := doc.Mrkdwn(), doc.RichTextBlocks()
	api, ch, ts := c.api, c.ch, c.ts
	broadcast := c.broadcast && ts != ""
	c.clearAfterSend()
	go func() {
		ctx := context.Background()
		// Slack's scheduleMessage goes through prepareAndSendMessage with
		// includeBroadcastKeywordWarning and dateScheduled: the warning
		// (its scheduled wording, "Schedule Message"), then the keyword
		// check (broadcast.go)
		if used := deltaKeywords(doc.Delta()); len(used) > 0 {
			j := &sendJob{api: api, ch: ch, ts: ts, confirm: c.confirmBroadcast}
			enabled := api.broadcastEnabled(ctx, ch)
			if !j.confirmed(ctx, broadcastAsk{Channel: ch, Used: used, Scheduled: true}, enabled) {
				c.restore(doc)
				return
			}
			if msg, no := j.prohibited(ctx, ch, ts, used, enabled); no {
				api.localEphemeral(ch, ts, msg)
				c.restore(doc)
				return
			}
		}
		text, blocks := transformBlocks(ctx, text, blocks)
		_, err := api.scheduleMessage(ctx, ch, ts, text, blocks, broadcast, t.Unix())
		if err != nil {
			// scheduleSendMessage's failure: a local ephemeral
			// (scheduleSendError); the message stays a draft
			api.localEphemeral(ch, ts, scheduleFailedText)
			c.restore(doc)
		}
		// success says nothing: Slack's context bar shows the schedule
	}()
}

// ---- sending ----

// pasteLimitDialog is renderPasteLimitDialog: the title, the limit's
// explanation, "Create a Snippet" (the snippet dialog with the refused
// paste) and OK.
func (c *Composer) pasteLimitDialog(pasted string) {
	th := c.h.Theme()
	m := popups.NewModal(c.h, popups.ModalOpts{
		Title:        i18n.T("msg_input", pasteLimitTitle),
		EnterSubmits: true,
		Body: func(gtx layout.Context) layout.Dimensions {
			st := textStyle{Size: 15, LineHeight: 22, Color: th.C.Fg}
			b := st
			b.Weight = font.Bold
			return richLabel(gtx, th.Shaper, []richSpan{{pasteLimitText, st}, {pasteLimitBold, b}, {pasteLimitAfter, st}})
		},
		Buttons: []*popups.ModalButton{
			{Label: i18n.T("msg_input", "Create a Snippet"), Kind: popups.BtnOutline, Do: func() bool {
				c.openSnippetFromPaste(pasted)
				return true
			}},
			{Label: i18n.T("msg_input", "OK"), Kind: popups.BtnPrimary, Do: func() bool { return true }},
		},
	}).OldDialog()
	c.h.ShowOverlay(m)
}

// historyNavigator is the ui root's history (triggerNavigateBack /
// triggerNavigateForward): Mod+Left / Mod+Right in an empty composer.
type historyNavigator interface {
	Back()
	Forward()
}

// slashHuddler is the ui root's /huddle: start or join the huddle in a
// conversation (internal/ui/huddleui through ui/huddle_wire.go).
type slashHuddler interface{ SlashHuddle(channel string) }

// deleteEmptiedEdit is the edit composer's startDelete (boot-shared cjbg
// maybeSave): saving an edit with nothing left in it (whitespace aside,
// no files) opens Slack's delete-message dialog for the message instead
// of saving; the editor stays open behind it.
func (c *Composer) deleteEmptiedEdit() {
	if c.edit == nil {
		return
	}
	popups.ConfirmDelete(c.h, *c.edit)
}

// editHasFiles reports whether the message being edited shares files
// (maybeSave keeps them and saves an empty comment).
func editHasFiles(m *core.Message) bool {
	for _, b := range m.Doc.Blocks {
		if b.Kind == richtext.FileBlock {
			return true
		}
	}
	return false
}

// overLimit is how far the message is past the 12,000-character limit
// (charlimit.go), counted once per document change.
func (c *Composer) overLimit() int {
	if v := c.ed.Version(); !c.limitOK || v != c.limitVer {
		c.limitVer, c.limitOver, c.limitOK = v, charsOverLimit(c.ed.Doc()), true
	}
	return c.limitOver
}

func (c *Composer) submit() {
	d := c.ed.Doc()
	if c.overLimit() > 0 {
		// isSendingDisabled (isOverMaxLength): Enter does nothing and the
		// send button is disabled; an edit can't be saved (cjbg maybeSave
		// returns while textyExceedsMaxLength)
		return
	}
	if d.Empty() && c.att.Len() == 0 {
		if c.edit == nil {
			return
		}
		if !editHasFiles(c.edit) {
			c.deleteEmptiedEdit()
			return
		}
		// cjbg: a message with files (fileIdsToSave) saves the emptied
		// comment instead of offering to delete
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
			if hs, ok := c.h.(slashHuddler); ok && strings.EqualFold(cmd, "/huddle") && ts == "" {
				// /huddle starts or joins the huddle here (Slack's
				// startHuddle), like the header button
				c.clearAfterSend()
				hs.SlashHuddle(ch)
				return
			}
			c.clearAfterSend()
			go func() {
				// handleSlashCommand: the reply or the failure is
				// Slackbot's ephemeral; the app's modal arrives as
				// view_opened for the command's client_token
				res, err := runCommand(context.Background(), api, core.CommandOpts{Channel: ch, Command: cmd, Text: strings.TrimSpace(rest), ThreadTS: ts})
				if err == nil && res.KeepInput {
					c.restore(doc)
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
	// delta is the delta a plugin handed back to the composer's
	// prepareAndSendMessage (bChannel's "/bchannel …"); nil: doc's
	delta map[string]any
}

// send posts the message (with any attached files) through Slack's
// prepareAndSendMessage → sendMessage thunks.
//
// Attached files leave the composer at once, as in Slack (the draft is
// gone and the placeholder message shows them while their uploads
// finish); a failed send puts them back.
func (c *Composer) send(o *outgoing) {
	batch := c.att.takeForSend()
	hasFiles := !batch.empty()
	job := &sendJob{api: c.api, ch: c.ch, ts: c.ts, o: o, prepared: batch.prepared, pending: batch.pending, raw: batch.raw,
		files: hasFiles, draftID: c.draftID(), view: c.viewContext(), warn: c.ts == "", confirm: c.confirmBroadcast}
	go func() {
		err := job.run(context.Background())
		if hasFiles && err != nil {
			c.later(func() {
				// the placeholder is gone and nothing was posted: the
				// message (restore, below) and its files go back into the
				// draft, and Slack's FileUploadErrorDialog says so (a
				// dismissed warning or Slackbot's answer needs no dialog)
				c.att.putBack(batch.chips)
				if !isQuietSendStop(err) {
					popups.FileUploadError(c.h, func() { c.h.Navigate("drafts") })
				}
			})
		} else if err != nil && !isQuietSendStop(err) { // Slackbot already said why, or the warning was dismissed
			c.later(func() { c.h.Toast("Message not sent: " + err.Error()) })
		}
		if err != nil {
			c.restore(o.doc)
			return
		}
		c.later(func() { c.unsendBy = time.Now().Add(unsendWindow) })
	}()
}

// unsendWindow is how long Mod+Z can unsend a message (15e3 ms).
const unsendWindow = 15 * time.Second

// unsender is the host's undoSendLastMessageInChannel: it unsends the
// viewer's last message in the conversation (asking first when
// should_show_unsend_message_confirmation is on) and hands its contents
// back for the composer.
type unsender interface {
	UnsendLast(channel, threadTS string, put func(*Doc))
}

// tryUnsend is onCmdZWhenEmpty: within the window after a send from this
// composer, Mod+Z in the empty composer unsends it. false: plain undo.
func (c *Composer) tryUnsend() bool {
	u, ok := c.h.(unsender)
	if !ok || c.edit != nil || c.unsendBy.IsZero() || time.Now().After(c.unsendBy) {
		return false
	}
	c.unsendBy = time.Time{}
	u.UnsendLast(c.ch, c.ts, c.restore)
	return true
}

// DocForMessage is a message's contents as composer text (Slack's
// getDeltaForMessageObject): its rich text, else its text.
func DocForMessage(m core.Message) *Doc {
	d := DocFromRichtext(m.Doc.Blocks)
	if d == nil || d.Len() == 0 {
		d = NewDoc()
		d.Insert(0, m.Text, Attr{})
	}
	return d
}

// sendJob is one outgoing message; run happens off the UI goroutine.
type sendJob struct {
	api           coreAPI
	ch, ts        string
	o             *outgoing
	prepared      []core.PreparedUpload
	pending       []*core.PendingFile // eager uploads still running: the share waits for them
	raw           []core.UploadFile
	files         bool
	draftID, view string
	// warn is includeBroadcastKeywordWarning (the channel composer, not
	// threads); confirm asks the user through the warning dialog and
	// waits (nil: no dialog, so a due warning stops the send).
	warn    bool
	confirm func(ctx context.Context, ask broadcastAsk) bool
}

func (j *sendJob) run(ctx context.Context) error {
	delta := j.o.delta
	if delta == nil {
		delta = j.o.doc.Delta()
	}
	args := prepareSendArgs{ChannelID: j.ch, ReplyToTs: j.ts, Delta: delta, Text: j.o.text, Blocks: j.o.blocks,
		ShouldBroadcast: j.o.broadcast, Broadcast: j.o.broadcast, DraftID: j.draftID, ViewContext: j.view,
		IncludeBroadcastKeywordWarning: j.warn}
	if t := currentTaut(); t.ThunkPatched(thPrepareAndSend) {
		for _, p := range j.prepared {
			args.FileIDs = append(args.FileIDs, p.ID)
		}
		_, err := taut.Dispatch(ctx, t, thPrepareAndSend, args, func(ctx context.Context, a prepareSendArgs) (any, error) {
			return nil, j.prepareAndSend(ctx, a) // broadcast.go
		})
		return err
	}
	return j.prepareAndSend(ctx, args)
}

// deliver is prepareAndSendMessage's native: share the files with the
// message as their comment, or post the message.
func (j *sendJob) deliver(ctx context.Context, ch, ts, text string, blocks json.RawMessage, broadcast bool) error {
	if j.files {
		comment := text
		if j.o.doc.Empty() {
			comment = ""
		}
		return sendFiles(ctx, j.api, ch, ts, comment, blocks, broadcast, j.prepared, j.pending, j.raw)
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
// plugin's shouldBroadcast override (SlimMessageBox) and delta (bChannel
// turns the message into its /bchannel command; sendJob runs it as one).
func (c *Composer) prepareAndSend(opts slackc.Props) {
	o := c.pending
	c.pending = nil
	if o == nil {
		return
	}
	if opts.Has("shouldBroadcast") {
		o.broadcast = opts.Bool("shouldBroadcast") && c.ts != ""
	}
	if d := opts.Map("delta"); d != nil {
		o.delta = map[string]any(d)
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

// HasDraft reports whether a channel or thread has an unsent draft, text
// or files (for the sidebar's draft pencil).
func HasDraft(ch, ts string) bool {
	drafts.Lock()
	_, ok := drafts.m[draftKey(ch, ts)]
	drafts.Unlock()
	return ok || storedFiles(draftKey(ch, ts))
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

// restoreDroppedFocus is the fallback for a focus Gio dropped anyway
// (editor_focus.go): the editor got no blur (c.focused, from its
// FocusEvents) but isn't focused, so its handler vanished for a frame
// rather than anything taking the focus. It asks for the focus back once,
// unless the pointer was pressed since it was last focused (a click may
// have been meant for something else).
func (c *Composer) restoreDroppedFocus(gtx layout.Context) {
	if !gtx.Source.Enabled() {
		return
	}
	if c.ed.Focused(gtx) {
		c.focusPress, c.focusRestored = pressCount(), false
		return
	}
	if !c.focused || c.focusRestored {
		return
	}
	c.focusRestored = true
	focusStats.dropped.Add(1)
	if pressCount() != c.focusPress {
		c.focused = false
		return
	}
	focusStats.restored.Add(1)
	c.ed.Focus(gtx)
}

// FocusTag is the key-focus tag of the composer's editor (tests check
// that nothing takes focus from it).
func (c *Composer) FocusTag() any { return c.ed }

// TypingLine is the typing indicator's text under the composer as last
// laid out ("" none; tests).
func (c *Composer) TypingLine() string { return c.ui.cur.Typing }

// DocText is the composer's text as plain characters (tests).
func (c *Composer) DocText() string { return c.ed.Doc().Text() }

// AttachFile attaches a file held in memory, like a paste or the + menu's
// picker (its eager upload starts at once). Safe from any goroutine.
func (c *Composer) AttachFile(name string, data []byte) { c.att.Add(name, data) }

// Attached is the number of files attached now.
func (c *Composer) Attached() int { return c.att.Len() }
