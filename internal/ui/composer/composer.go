package composer

import (
	"context"
	"image"
	"strings"
	"sync"
	"time"

	"gioui.org/layout"
	"gioui.org/op"
	"gioui.org/op/paint"

	"github.com/some-du6e/yoloslackgoclient/internal/core"
	"github.com/some-du6e/yoloslackgoclient/internal/ui/popups"
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

	broadcast  bool
	lastTyping time.Time
	lastVer    uint64
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
	c.ac = newAutocomplete(c.h, c.ch)
	c.ed.Intercept = c.ac.Key
	c.att = newAttachments(c.h)
	typingHub(c.h.Core()).addHost(c.h) // listen for typing events
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
	a := &Atom{Kind: AtomEmoji, ID: name, Text: ":" + name + ":", URL: url}
	if txt != "" && !strings.HasPrefix(txt, ":") {
		a.Uni = txt
	}
	return a
}

// ---- layout ----

// Layout draws the composer. Channel/thread composers take the full width
// they're given (margins included) and size their height to the content;
// edit composers draw just the box.
func (c *Composer) Layout(gtx layout.Context) layout.Dimensions {
	runRestores()
	th := c.h.Theme()
	if c.st.Shaper != th.Shaper || c.st.Shaper == nil {
		c.st = c.editorStyle()
	}
	c.st.Images = c.emojiImage
	c.st.PlaceholderText = ""

	if c.focusReq {
		c.focusReq = false
		c.ed.Focus(gtx)
	}
	c.handleEditor(gtx)
	c.handleActions(gtx)

	st := c.chromeState(gtx)
	input := func(gtx layout.Context) layout.Dimensions {
		dims := c.ed.Layout(gtx, c.st)
		return dims
	}
	dims := c.ui.Layout(gtx, c.h, st, input, c.att.Layout)

	// Autocomplete after the editor laid out, anchored at the trigger.
	c.ac.Update(c.ed.Doc(), caretOf(c.ed))
	c.acceptAutocomplete()
	if c.ac.Open() {
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
	org := c.ui.InputOffset()
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

func (c *Composer) acceptAutocomplete() {
	it, s, e, ok := c.ac.Accepted()
	if !ok {
		return
	}
	if it.Kind == acCommand {
		c.ed.Replace(s, e, "/"+it.Name+" ")
	} else if it.Atom != nil {
		c.ed.InsertAtom(s, e, it.Atom)
	}
	c.h.Invalidate()
}

func (c *Composer) chromeState(gtx layout.Context) chromeState {
	st := chromeState{
		Focused:        c.ed.Focused(gtx),
		ShowFormatting: showFormatting,
		Active:         c.ed.ActiveStyle(),
		Block:          c.ed.ActiveBlock(),
		Link:           c.ed.ActiveLink(),
		CanSend:        !c.ed.Empty() || c.att.Len() > 0,
		Broadcast:      c.broadcast,
		Origin:         c.Origin,
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
	st.Typing = typingText(c.h.Core(), c.ch, c.ts, c.api.self().ID)
	st.ScrolledTop = c.ed.scrollY > 0
	st.ScrolledBottom = c.ed.scrollY+c.ed.viewH < c.ed.contentH
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
	if !c.ed.Empty() && time.Since(c.lastTyping) > 3*time.Second {
		c.lastTyping = time.Now()
		api, ch, ts := c.api, c.ch, c.ts
		go api.typing(ch, ts)
	}
}

func (c *Composer) handleActions(gtx layout.Context) {
	for _, a := range c.ui.Actions() {
		a.Anchor = a.Anchor.Add(c.Origin)
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
			c.att.Menu(a.Anchor)
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
			{Label: "Custom time", Do: func() { c.h.Toast("Custom schedule times aren't available yet") }},
		}
		c.h.ShowOverlay(popups.NewMenuOpts(c.h, items, popups.MenuOpts{Width: 260, Anchor: anchor, Side: popups.Above, AlignRight: true}))
	}
}

func (c *Composer) schedule(t time.Time) {
	// core has no chat.scheduleMessage wrapper yet.
	c.h.Toast("Scheduling messages needs core support (chat.scheduleMessage)")
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
	doc := d.Clone()
	text := doc.Mrkdwn()
	blocks := doc.RichTextBlocks()
	api, ch, ts := c.api, c.ch, c.ts

	if c.edit != nil {
		m := c.edit
		go func() {
			if err := api.edit(context.Background(), m.Channel, m.TS, text, blocks); err != nil {
				c.later(func() { c.h.Toast("Couldn't save your edit: " + err.Error()) })
			}
		}()
		c.finishEdit()
		return
	}

	// Slash commands (only when the message starts with a known command
	// or any /word; Slack sends unknown /words as commands too).
	if c.att.Len() == 0 {
		plain := strings.TrimSpace(doc.Text())
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

	files := c.att.Files()
	broadcast := c.broadcast && ts != ""
	c.clearAfterSend()
	if len(files) > 0 {
		c.att.SetUploading(true)
		go func() {
			comment := text
			if doc.Empty() {
				comment = ""
			}
			_, err := api.upload(context.Background(), core.UploadOpts{Channel: ch, ThreadTS: ts, Comment: comment, Files: files})
			c.later(func() {
				c.att.SetUploading(false)
				if err != nil {
					c.h.Toast("Upload failed: " + err.Error())
					return
				}
				c.att.Clear()
			})
			if err != nil {
				c.restore(doc)
			}
		}()
		return
	}
	go func() {
		_, err := api.send(context.Background(), ch, ts, text, blocks, broadcast)
		if err != nil {
			c.later(func() { c.h.Toast("Message not sent: " + err.Error()) })
			c.restore(doc)
		}
	}()
}

func (c *Composer) clearAfterSend() {
	c.ed.SetDoc(NewDoc())
	c.lastVer = c.ed.Version()
	c.broadcast = false
	c.lastTyping = time.Time{}
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
