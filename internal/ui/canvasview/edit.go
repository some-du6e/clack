package canvasview

// Editing (docs/CANVASES.md). A click on a text block (paragraph, heading,
// list item, code) turns it into the composer's rich editor, with the
// canvas's toolbar docked at the top of the document. Enter adds a block
// below in the same editor (a list stays a list); Escape, or a click
// elsewhere, saves: one canvases.edit replace (or insert_after for a
// block typed below the document's last one, delete for a cleared one) in
// canvas markdown. The view updates at once; the realtime echo then
// re-reads the export. Markdown shortcuts at the start of a block: "# ",
// "## ", "### " headings, "- " bullets, "1. " numbers, "[] " a checklist,
// "> " a quote, "```" code; *bold* _italic_ ~strike~ `code` inline. "@" and
// "#" open a picker for people and channels; the toolbar's emoji button
// opens the emoji picker.

import (
	"context"
	"errors"
	"image"
	"image/color"
	"strings"
	"time"

	"gioui.org/font"
	"gioui.org/io/event"
	"gioui.org/io/key"
	"gioui.org/io/pointer"
	"gioui.org/layout"
	"gioui.org/op"
	"gioui.org/op/clip"
	"gioui.org/unit"
	"gioui.org/widget"

	"github.com/some-du6e/clack/internal/canvas"
	"github.com/some-du6e/clack/internal/core"
	"github.com/some-du6e/clack/internal/quip"
	"github.com/some-du6e/clack/internal/richtext"
	"github.com/some-du6e/clack/internal/ui/composer"
	"github.com/some-du6e/clack/internal/ui/icons"
	"github.com/some-du6e/clack/internal/ui/theme"
)

// editState is the view's editing state (UI goroutine only).
type editState struct {
	cur      *session
	renaming bool
	// renameFrom is the title the rename began from: a save through
	// edit-document goes only while it's still the title section's
	renameFrom string
	rename     widget.Editor
	renameOn   bool // focus requested
	bg         int  // tag: clicks on the document's background end editing
	tool       map[string]*widget.Clickable
	pick       picker
	saving     int // edits in flight
	insBar     int // tag: the bottom toolbar's background
	// local are the new blocks made here through edit-document that the
	// export may not have yet (keepLocal)
	local []localBlock
}

// session is one block being edited.
type session struct {
	target  *blk   // the block (nil: a new block typed after afterID)
	id      string // its section id
	afterID string // a new block: the section it follows ("" = the end)
	place   *blk   // a new block's placeholder row
	kind    canvas.Kind
	level   int
	checked bool
	orig    string          // the markdown it started from
	spans   []richtext.Span // and its spans: the save checks the section still holds them
	// and the kind, level and box it began with (the toolbar's formats
	// change kind, level and checked as the session goes)
	kind0    canvas.Kind
	level0   int
	checked0 bool
	code0    string // a code block's text when it began
	seq0     int64  // its section's sequence when it began (core.CanvasSectionSeq; 0: unknown)
	ed       *composer.Editor
	focus    bool
	ver      uint64
	refused  bool // the last save was refused: Escape now discards
	quoted   bool // a paragraph of a quote: each line saves as a "> " section
	// quip: it saves through edit-document (quipOn when it began), which
	// keeps what the editor holds as it is: a heading's bold, trailing
	// spaces (canvas markdown drops both)
	quip  bool
	press *pressAt
	// def: the block is Quip's unedited default content (Block.Default).
	// Focusing it empties it (Section.hideDefaultContent, HIDE_ON_FOCUS):
	// the editor opens empty with the caret at 0. Typing makes it an
	// ordinary section (TextSectionOverlay.onSelectionChange →
	// clearDefaultContent), saved as a replace; left empty, it gets its
	// default content back and nothing is sent (unfocusDefaultContent_ →
	// restoreDefaultContent_). defText is that content's text.
	def     bool
	defText string
}

// pressAt is the click that opened the session: where it landed in the
// block's text (the editor's coordinates), when, and whether its drag
// is still going (the block keeps the pointer grab until the release).
type pressAt struct {
	pt     image.Point
	at     time.Time
	placed bool // the caret was put at pt
	drag   bool
	anchor int
	to     *image.Point // a drag that came before the editor's first frame
}

func newEditState() *editState {
	return &editState{tool: map[string]*widget.Clickable{}}
}

func (e *editState) btn(k string) *widget.Clickable {
	b := e.tool[k]
	if b == nil {
		b = new(widget.Clickable)
		e.tool[k] = b
	}
	return b
}

// canEdit: the viewer may edit the canvas.
func (e *editState) canEdit(v *View) bool {
	if e == nil || v.shown == nil || v.mode == Peek {
		return false
	}
	if a := v.h.Core(); a != nil && a.QuipEditOn() && a.CanvasSaveState(v.id).ReadOnly {
		return false // the save error state's lock (cant_sync)
	}
	return v.shown.Info.Editable
}

// quipOn reports whether the canvas's edits save through edit-document.
func quipOn(v *View) bool {
	a := v.h.Core()
	return a != nil && a.QuipEditOn()
}

// saveQuip runs a save through edit-document off the UI goroutine. A save
// Clack can't make the way Slack's client would (nothing was sent) gets
// the can't-edit toast and the canvas reloads; a failed one stays pending
// in core and is retried (core.ErrCanvasSavePending), and the banner says
// so when Slack's would; any other failure says so too. What a later
// attempt drops comes as core's notices (showSaveNotices).
func (e *editState) saveQuip(v *View, f func(ctx context.Context, a *core.App) error) {
	a := v.h.Core()
	if a == nil {
		return
	}
	e.saving++
	go func() {
		ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
		defer cancel()
		err := f(ctx, a)
		v.post(func() {
			e.saving--
			switch {
			case err == nil, errors.Is(err, core.ErrCanvasSavePending):
				// saved, or pending in core: retried, the banner once it's
				// Slack's error state
			case errors.Is(err, core.ErrCanvasConflict):
				v.h.Toast(changedMeanwhile)
				v.load(true)
			case errors.Is(err, quip.ErrUnsupported) || errors.Is(err, quip.ErrRefused) || errors.Is(err, core.ErrCanvasReadOnly):
				v.h.Toast(cantEdit + " (" + friendly(err) + ")")
				v.load(true)
			default:
				v.h.Toast("Couldn't save your change to the canvas: " + friendly(err))
				v.load(true)
			}
			v.h.Invalidate()
		})
	}()
}

// showSaveNotices tells what core's later attempts dropped (a conflict
// found on a retry, an edit refused after waiting behind another save) as
// a direct save's result is told, and reloads the canvas.
func (v *View) showSaveNotices(ns []core.CanvasSaveNotice) {
	if len(ns) == 0 {
		return
	}
	conflict, reason := false, ""
	for _, n := range ns {
		if n.Conflict {
			conflict = true
		} else if reason == "" {
			reason = n.Reason
		}
	}
	if conflict {
		v.h.Toast(changedMeanwhile)
	}
	if reason != "" {
		v.h.Toast(cantEdit + " (" + strings.ReplaceAll(reason, "_", " ") + ")")
	}
	v.load(true)
}

// quipRefuses reports whether a finished session with markdown md (blocks
// bs) changes something edit-document can't save yet: Quip's default
// content typed over, a quote's paragraph made more than its own text, a
// block's style changed into or out of a list, a code block made more
// than its code, new blocks that start or leave a list (each
// new block has to keep the list, and the level, of the block it follows),
// or a mention, emoji or date in a new block (a control section of its
// own). A session that changed nothing, or a new block left empty, saves
// nothing and closes; a block emptied is deleted.
func quipRefuses(v *View, s *session, md string, bs []canvas.Block) bool {
	switch {
	case !quipChanged(s, md, bs):
		return false
	case s.def:
		return true
	case s.target != nil && md == "":
		return false
	case len(bs) == 0:
		return true
	case s.quoted: // a quote's paragraph (a section of style 16): its own text, for now
		q := unquote(bs)
		return len(q) != 1 || q[0].Kind != canvas.Paragraph || q[0].Level != 0
	}
	if s.target != nil && s.kind0 == canvas.Code { // a code block's text, nothing more
		return len(bs) != 1 || bs[0].Kind != canvas.Code
	}
	news := bs
	var kind canvas.Kind
	var level int
	if s.target != nil {
		b := bs[0]
		same := b.Kind == s.kind0 && b.Level == s.level0 && b.Checked == s.checked0
		switch {
		case !quipTextKind(b.Kind):
			return true
		case len(bs) > 1 && !same: // a split keeps the block as it was
			return true
		case b.Kind != s.kind0 && (b.Kind.IsList() || s.kind0.IsList()): // into or out of a list, or another list's kind
			return true
		case b.Level != s.level0 && !b.Kind.IsList(), b.Checked != s.checked0 && b.Kind != canvas.Check:
			return true
		}
		news, kind, level = bs[1:], s.kind0, s.level0
	} else {
		after := findBlockByID(v.blocks, s.afterID)
		if after == nil {
			return true
		}
		kind, level = after.Kind, after.Level
	}
	if !quipTextKind(kind) {
		return true
	}
	for _, b := range news {
		if !quipTextKind(b.Kind) || kind.IsList() != b.Kind.IsList() || b.Kind.IsList() && (b.Kind != kind || b.Level != level) ||
			!b.Kind.IsList() && b.Level != 0 || b.Checked && b.Kind != canvas.Check {
			return true
		}
		for _, sp := range b.Spans {
			if sp.Kind != richtext.Text && sp.Kind != richtext.Link && sp.Kind != richtext.Break {
				return true
			}
		}
	}
	return false
}

// newControl reports whether a session's block holds a mention or emoji
// the block didn't when it began: core's spanRuns matches each to one of
// its section's controls, and refuses one it can't.
func newControl(s *session, bs []canvas.Block) bool {
	if s.quoted {
		bs = unquote(bs)
	}
	if len(bs) == 0 {
		return false
	}
	used := make([]bool, len(s.spans))
	for _, sp := range bs[0].Spans {
		switch sp.Kind {
		case richtext.User, richtext.Channel, richtext.Emoji:
		default:
			continue
		}
		found := false
		for k, o := range s.spans {
			if !used[k] && o.Kind == sp.Kind && o.ID == sp.ID && (o.ID != "" || o.Text == sp.Text) {
				used[k], found = true, true
				break
			}
		}
		if !found {
			return true
		}
	}
	return false
}

// quipNewInLayout reports whether a finished session makes a new block in
// a callout or column (Enter in one of its paragraphs, or typing after
// one), which core doesn't save yet.
func quipNewInLayout(v *View, s *session, md string, bs []canvas.Block) bool {
	a := v.h.Core()
	switch {
	case a == nil:
		return false
	case s.target != nil && len(bs) > 1:
		if p := s.target.parent; p != nil && p.b != nil && p.b.Kind == canvas.Callout {
			return true
		}
		return a.CanvasSectionInLayout(v.id, s.id)
	case s.target == nil && md != "" && s.afterID != "":
		return a.CanvasSectionInLayout(v.id, s.afterID)
	}
	return false
}

// newBlocks are blocks bs as new sections with ids.
func newBlocks(bs []canvas.Block, ids []string) []core.CanvasNewBlock {
	out := make([]core.CanvasNewBlock, 0, len(bs))
	for i, b := range bs {
		id := ""
		if i < len(ids) {
			id = ids[i]
		}
		out = append(out, core.CanvasNewBlock{ID: id, Kind: b.Kind, Level: b.Level, Checked: b.Checked, Spans: b.Spans})
	}
	return out
}

// findBlockByID is the block of the view with section id (nil: none).
func findBlockByID(bs []*blk, id string) *canvas.Block {
	for _, b := range bs {
		if b.b != nil && id != "" && b.b.ID == id {
			return b.b
		}
		if x := findBlockByID(b.kids, id); x != nil {
			return x
		}
	}
	return nil
}

// blockEditable is editable for a block as drawn: with the switch on, its
// section has to hold only what the block shows, else an edit would lose
// the rest (core.CanvasBlockEditable: a link Clack's export drops,
// formatting it can't hold). finish checks editable alone: the edited
// blocks aren't the section any more.
func (v *View) blockEditable(b canvas.Block) (bool, string) {
	ok, why := v.editable(b)
	if ok && quipOn(v) && v.titleWhy != "" { // no edit of the canvas saves (checkTitle)
		return false, v.titleWhy
	}
	if !ok || !quipOn(v) || b.ID == "" || b.Kind == canvas.Code {
		return ok, why
	}
	if a := v.h.Core(); a != nil {
		return a.CanvasBlockEditable(v.id, b.ID, b.Spans)
	}
	return true, ""
}

// quipTextKind: a block that is one text section in Quip (a paragraph, a
// heading, a list item).
func quipTextKind(k canvas.Kind) bool {
	switch k {
	case canvas.Paragraph, canvas.Heading1, canvas.Heading2, canvas.Heading3, canvas.Bullet, canvas.Numbered, canvas.Check:
		return true
	}
	return false
}

// editable is whether block b opens in the editor and saves, and else
// what is in the way: through edit-document (the switch on) whatever its
// section's text holds that the editor keeps as it is; else what canvas
// markdown carries (canvas.Editable).
func (v *View) editable(b canvas.Block) (bool, string) {
	if !quipOn(v) {
		return canvas.Editable(b)
	}
	if b.Kind == canvas.Code { // its text, line by line (core.SaveCanvasCode)
		return true, ""
	}
	if !quipTextKind(b.Kind) {
		return false, "this kind of block"
	}
	for _, s := range b.Spans {
		switch s.Kind {
		case richtext.Text, richtext.Emoji:
		case richtext.Break:
			// the editor makes each line a block of its own (toDoc's \n):
			// the block would come back split, and save as sections
			return false, "a line break"
		case richtext.Link:
			if s.Control {
				return false, "a canvas, file or message mention"
			}
		case richtext.User, richtext.Channel:
			if s.ID == "" {
				return false, "a mention Clack can't match to a person or channel"
			}
		case richtext.Date:
			return false, "a date"
		case richtext.Broadcast:
			return false, "an @" + s.ID + " mention"
		default:
			return false, "an inline object"
		}
		if s.Style&richtext.Highlight != 0 {
			return false, "highlighted text"
		}
	}
	return true, ""
}

// quipChanged reports whether finishing a session with markdown md
// (blocks bs) saves anything through edit-document (finish's cases that
// return without a save don't).
func quipChanged(s *session, md string, bs []canvas.Block) bool {
	return !(s.def && md == "") && !(s.target != nil && !s.def && quipSame(s, bs)) && !(s.target == nil && md == "")
}

// quipSame reports whether a session's blocks bs are still the block it
// began as, as edit-document saves a block: the same kind, level and box,
// and the same text, formatting, links and mentions. Canvas markdown can't
// tell: it has no underline, and writes a link whose text is its URL as
// bare text, so an underline or the Link button alone looked like nothing.
func quipSame(s *session, bs []canvas.Block) bool {
	if s.quoted {
		bs = unquote(bs)
	}
	if len(bs) == 0 { // the editor holds nothing: the same when the block held nothing either
		return blankSpans(s.spans) && strings.TrimSpace(s.code0) == ""
	}
	if len(bs) != 1 {
		return false
	}
	b := bs[0]
	if b.Kind != s.kind0 || b.Level != s.level0 || b.Checked != s.checked0 {
		return false
	}
	if b.Kind == canvas.Code {
		return b.Code == s.code0
	}
	return spanKey(b.Spans) == spanKey(s.spans)
}

// spanKey is spans as a save sees them: each character with its
// formatting and link, each line break, each mention or emoji by what it
// names.
func spanKey(sp []richtext.Span) string {
	const styles = richtext.Bold | richtext.Italic | richtext.Underline | richtext.Strike | richtext.Code | richtext.Highlight
	var b strings.Builder
	for _, x := range sp {
		switch x.Kind {
		case richtext.Text, richtext.Link:
			t, url := x.Text, ""
			if x.Kind == richtext.Link {
				url = x.URL
				if t == "" {
					t = x.URL
				}
			}
			for _, r := range t {
				b.WriteRune(r)
				b.WriteRune(rune(x.Style & styles))
				b.WriteString(url)
				b.WriteByte(1)
			}
		case richtext.Break:
			b.WriteString("\n\x01")
		default:
			id := x.ID
			if id == "" {
				id = x.Text
			}
			b.WriteByte(2)
			b.WriteRune(rune(x.Kind))
			b.WriteString(id)
			b.WriteByte(1)
		}
	}
	return b.String()
}

func (e *editState) editing(b *blk) bool {
	return e != nil && e.cur != nil && (e.cur.target == b || e.cur.place == b)
}

// textArea makes a text block's area editable the way Quip's
// contenteditable document is: a press anywhere on the block's text
// (headings, list items, quote and callout paragraphs, code) opens it in
// the editor with the caret where the press landed (the browser's
// caretPositionFromPoint, which Quip's editor relies on), a drag selects
// from there, and a double or triple click selects the word or the line.
// A press on a link or mention is left to richview, which opens it. A
// block Clack can't edit keeps richview's selection and explains itself
// on release. r is the text's area in the editor's coordinates.
func (e *editState) textArea(gtx layout.Context, v *View, b *blk, r image.Rectangle) {
	if e == nil {
		return
	}
	for {
		ev, ok := gtx.Event(pointer.Filter{Target: b, Kinds: pointer.Press | pointer.Drag | pointer.Release})
		if !ok {
			break
		}
		pe, ok := ev.(pointer.Event)
		if !ok {
			continue
		}
		if p := e.pressOn(b); p != nil { // the press that just opened b: its drag and release
			switch pe.Kind {
			case pointer.Drag:
				pt := pe.Position.Round()
				p.to = &pt
			case pointer.Release:
				pt := pe.Position.Round()
				p.to, p.drag = &pt, false
			}
			continue
		}
		onTarget := b.rv != nil && b.rv.Hovered() // a link or mention under the pointer
		switch pe.Kind {
		case pointer.Press:
			if !primary(pe) || onTarget || !b.edOK {
				continue
			}
			e.startAt(v, b, pe.Position.Round(), gtx.Now)
		case pointer.Release:
			if b.edOK || onTarget || b.rv != nil && b.rv.HasSelection() {
				continue
			}
			e.refuse(v, b)
		}
	}
	func() {
		defer clip.Rect(r).Push(gtx.Ops).Pop()
		defer pointer.PassOp{}.Push(gtx.Ops).Pop()
		if b.edOK {
			pointer.CursorText.Add(gtx.Ops)
		}
		event.Op(gtx.Ops, b)
	}()
	// Quip's cursors over the text: the hand on a link, a mention, a
	// file or canvas link and an editor's date (canvas-controller CSS:
	// .document a:not(.c-link):hover and control.c68
	// .slack-date-contents:not(.read-only) take cursor:pointer), the
	// text cursor only on plain text. The press area above is on top of
	// the block's read view, so its text cursor would win everywhere.
	if b.rv != nil && !hidden(b) {
		b.rv.HandCursors(gtx, true)
	}
}

// primary: a left-button mouse press, or a touch.
func primary(pe pointer.Event) bool {
	if pe.Source == pointer.Touch {
		return true
	}
	return pe.Buttons == pointer.ButtonPrimary && !pe.Modifiers.Contain(key.ModCtrl|key.ModCommand)
}

// pressOn is the opening press of b's session while it is held.
func (e *editState) pressOn(b *blk) *pressAt {
	if s := e.cur; s != nil && s.target == b && s.press != nil && s.press.drag {
		return s.press
	}
	return nil
}

// startAt opens b for editing from a press at pt (in the block's text
// coordinates); the editor puts its caret there on its first frame.
func (e *editState) startAt(v *View, b *blk, pt image.Point, now time.Time) {
	e.start(v, b)
	s := e.cur
	if s == nil || s.target != b {
		return
	}
	s.press = &pressAt{pt: pt, at: now, drag: true}
	if b.rv != nil {
		b.rv.ClearSelection() // the read view took the press too
	}
}

// pressEvents follows the opening press's drag in the editor (the block
// keeps the pointer grab it got with the press): the selection runs from
// the press to the pointer, clamped to the block, as Clack edits one
// section at a time. The block's tag stays registered over the editor
// until the release, or the router would cancel the grab.
func (e *editState) pressEvents(gtx layout.Context, s *session, b *blk, size image.Point) {
	p := s.press
	if p == nil || !p.drag {
		return
	}
	for {
		ev, ok := gtx.Event(pointer.Filter{Target: b, Kinds: pointer.Drag | pointer.Release | pointer.Cancel})
		if !ok {
			break
		}
		pe, ok := ev.(pointer.Event)
		if !ok {
			continue
		}
		switch pe.Kind {
		case pointer.Drag:
			if d := pe.Position.Round().Sub(p.pt); d.X*d.X+d.Y*d.Y > 9 {
				s.ed.SetSelection(p.anchor, s.ed.HitPos(pe.Position.Round()))
			}
		case pointer.Release:
			if d := pe.Position.Round().Sub(p.pt); d.X*d.X+d.Y*d.Y > 9 {
				s.ed.SetSelection(p.anchor, s.ed.HitPos(pe.Position.Round()))
			}
			p.drag = false
		case pointer.Cancel:
			p.drag = false
		}
	}
	if !p.drag {
		return
	}
	defer clip.Rect{Max: size}.Push(gtx.Ops).Pop()
	defer pointer.PassOp{}.Push(gtx.Ops).Pop()
	event.Op(gtx.Ops, b)
}

// cantEdit is Slack's feedback for a part of a canvas the client can't
// edit: a toast, and nothing is sent.
const cantEdit = "This part can't be edited in Clack yet"

// lockedText is Clack's feedback for a block finished while the canvas's
// saves are locked (the banner's error state): it stays open with its text.
const lockedText = "Clack can't save to this canvas right now. Your text stays here until it can"

// changedMeanwhile is Clack's feedback for an edit through edit-document of
// a section someone else saved first (core.ErrCanvasConflict): Slack's
// editor would merge the two; Clack doesn't send it.
const changedMeanwhile = "Someone else changed this part of the canvas while you were editing it, so your change wasn't saved"

func (e *editState) refuse(v *View, b *blk) {
	msg := cantEdit
	switch {
	case strings.HasPrefix(b.edWhy, "the canvas"):
		msg += " (" + b.edWhy + ")"
	case b.edWhy != "":
		msg += " (it has " + b.edWhy + ")"
	}
	v.h.Toast(msg)
}

// titleArea: a click on the title renames.
func (e *editState) titleArea(gtx layout.Context, v *View, r image.Rectangle) {
	if e == nil {
		return
	}
	tag := &e.renaming
	for {
		ev, ok := gtx.Event(pointer.Filter{Target: tag, Kinds: pointer.Release})
		if !ok {
			break
		}
		if pe, ok := ev.(pointer.Event); ok && pe.Kind == pointer.Release && (v.title == nil || !v.title.HasSelection()) {
			v.startRename()
		}
	}
	func() {
		defer clip.Rect(r).Push(gtx.Ops).Pop()
		defer pointer.PassOp{}.Push(gtx.Ops).Pop()
		pointer.CursorText.Add(gtx.Ops)
		event.Op(gtx.Ops, tag)
	}()
	if v.title != nil {
		v.title.HandCursors(gtx, true) // as textArea: a link in the title keeps the hand
	}
}

// startRename turns the title into an editor (More actions > Rename).
func (v *View) startRename() {
	e := v.edit
	if e == nil || v.shown == nil || !v.shown.Info.Editable {
		v.h.Toast("You can't edit this canvas")
		return
	}
	if !e.finish(v) {
		return
	}
	e.renaming, e.renameOn = true, true
	e.renameFrom = v.shown.Info.Title
	e.rename.SingleLine, e.rename.Submit = true, true
	e.rename.SetText(v.shown.Info.Title)
	e.rename.SetCaret(len([]rune(v.shown.Info.Title)), 0)
	v.h.Invalidate()
}

func (e *editState) layoutRename(gtx layout.Context, v *View, p image.Point, w int) int {
	th := v.h.Theme()
	h := gtx.Dp(titleLine + 8)
	for {
		ev, ok := e.rename.Update(gtx)
		if !ok {
			break
		}
		if _, ok := ev.(widget.SubmitEvent); ok {
			e.commitRename(v)
			return h
		}
	}
	if e.renameOn {
		gtx.Execute(key.FocusCmd{Tag: &e.rename})
		e.renameOn = false
	} else if !gtx.Focused(&e.rename) {
		e.commitRename(v)
		return h
	}
	for {
		ev, ok := gtx.Event(key.Filter{Focus: &e.rename, Name: key.NameEscape})
		if !ok {
			break
		}
		if k, ok := ev.(key.Event); ok && k.State == key.Press {
			e.renaming = false
			return h
		}
	}
	at(gtx, p, image.Pt(w, h), func(gtx layout.Context) layout.Dimensions {
		theme.Border(gtx, image.Rectangle{Max: gtx.Constraints.Max}, gtx.Dp(6), gtx.Dp(1), th.C.Accent)
		return at(gtx, image.Pt(gtx.Dp(8), gtx.Dp(4)), image.Pt(w-gtx.Dp(16), gtx.Dp(titleLine)), func(gtx layout.Context) layout.Dimensions {
			return th.Editor(gtx, &e.rename, titleSize, th.C.Fg, th.C.Selection)
		})
	})
	return h
}

func (e *editState) commitRename(v *View) {
	e.renaming = false
	title := strings.TrimSpace(e.rename.Text())
	if title == "" || v.shown == nil || title == v.shown.Info.Title {
		return
	}
	d := *v.shown
	d.Info.Title, d.Info.TitleSpans, d.Info.TitleFromText = title, nil, false
	if a := v.h.Core(); a != nil { // until the refetch brings title_blocks
		d.Info.TitleSpans, d.Info.TitleFromText = richtext.EmojiText(a.S, title), true
	}
	v.mu.Lock()
	v.doc = &d
	v.mu.Unlock()
	v.title = nil
	v.shown = &d
	if quipOn(v) { // the title section's text
		from := e.renameFrom
		e.saveQuip(v, func(ctx context.Context, a *core.App) error { return a.SaveCanvasTitle(ctx, v.id, from, title) })
		return
	}
	e.save(v, func(ctx context.Context, a *core.App) error { return a.RenameCanvas(ctx, v.id, title) })
}

// save runs an edit off the UI goroutine; a failure reloads the canvas.
func (e *editState) save(v *View, f func(ctx context.Context, a *core.App) error) {
	a := v.h.Core()
	if a == nil {
		return
	}
	e.saving++
	go func() {
		ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
		defer cancel()
		err := f(ctx, a)
		v.post(func() {
			e.saving--
			if err != nil {
				v.h.Toast("Couldn't save your change to the canvas: " + friendly(err))
				v.load(true)
			}
		})
	}()
}

func friendly(err error) string {
	s := err.Error()
	if i := strings.LastIndex(s, ": "); i >= 0 {
		s = s[i+2:]
	}
	return strings.ReplaceAll(s, "_", " ")
}

// toggleCheck ticks or unticks a checklist item.
func (e *editState) toggleCheck(v *View, b *blk) {
	if e == nil || b.b.ID == "" {
		return
	}
	if quipOn(v) { // the box alone (setSectionAttribute "checked"): the text isn't sent
		nb := *b.b
		nb.Checked = !nb.Checked
		e.replaceLocal(v, b, []canvas.Block{nb})
		id, on := nb.ID, nb.Checked
		e.saveQuip(v, func(ctx context.Context, a *core.App) error {
			return a.SaveCanvas(ctx, v.id, quip.Edit{Kind: quip.EditCheck, SectionID: id, Checked: on})
		})
		return
	}
	if !b.edOK { // saving the item would rewrite its text lossily
		e.refuse(v, b)
		return
	}
	nb := *b.b
	nb.Checked = !nb.Checked
	md := canvas.Markdown(nb)
	e.replaceLocal(v, b, []canvas.Block{nb})
	id := nb.ID
	e.save(v, func(ctx context.Context, a *core.App) error {
		return a.EditCanvas(ctx, v.id, core.CanvasChange{Operation: "replace", SectionID: id, Markdown: md})
	})
}

// start begins editing b (saving any other block first).
func (e *editState) start(v *View, b *blk) {
	if e.cur != nil && e.cur.target == b {
		return
	}
	if !e.canEdit(v) { // a viewer, or the save error state's lock
		return
	}
	if !e.finish(v) {
		return
	}
	if !b.edOK {
		e.refuse(v, b)
		return
	}
	s := &session{target: b, id: b.b.ID, kind: b.b.Kind, level: b.b.Level, checked: b.b.Checked, orig: canvas.Markdown(*b.b),
		spans: append([]richtext.Span(nil), b.b.Spans...), kind0: b.b.Kind, level0: b.b.Level, checked0: b.b.Checked, code0: b.b.Code,
		ed: newEditor(), focus: true, quip: quipOn(v)}
	if a := v.h.Core(); a != nil && quipOn(v) && b.b.ID != "" {
		s.seq0 = a.CanvasSectionSeq(v.id, b.b.ID)
	}
	if b.parent != nil && b.parent.b.Kind == canvas.Quote && b.b.Kind == canvas.Paragraph {
		// a quote's paragraph is a Quip section of its own (style 16):
		// it saves as "> …" so it stays in the quote
		s.quoted = true
		s.orig = canvas.Markdown(canvas.Block{Kind: canvas.Quote, Children: []canvas.Block{*b.b}})
	}
	if hidden(b) {
		s.def, s.defText = true, canvas.SpansText(b.b.Spans)
		s.ed.SetDoc(composer.NewDoc())
	} else {
		s.ed.SetDoc(toDoc(b.b))
	}
	s.ver = s.ed.Version()
	e.cur = s
	v.h.Invalidate()
}

// finish saves the current session, if any. It reports false (and keeps
// editing) when the edit holds something canvas markdown can't carry
// (underline, say): nothing is sent; a second Escape discards the edit.
func (e *editState) finish(v *View) bool {
	if e == nil || e.cur == nil {
		return true
	}
	s := e.cur
	bs := fromDoc(s.ed.Doc(), s)
	// "> " around text canvas markdown carries: what has to round-trip is
	// the quote's paragraph (a block quoted from the toolbar, or by typing
	// "> ", saves the same way as a quote's own paragraph)
	check := unquote(bs)
	for _, b := range check {
		if ok, why := v.editable(b); !ok {
			if why == "" {
				why = "this"
			}
			v.h.Toast("Clack can't save " + why + " to a canvas yet. Remove it, or press Escape again to discard your changes")
			s.refused = true
			s.focus = true
			return false
		}
	}
	var quipNew []string // new blocks' section ids (edit-document)
	if md := blocksMarkdown(bs); quipOn(v) && quipChanged(s, md, bs) {
		switch {
		case v.h.Core().CanvasSaveState(v.id).ReadOnly:
			// the error state's lock (cant_sync): nothing saves until a
			// save succeeds; the text stays here, open, and Escape saves it
			// once the lock lifts (Slack keeps text typed before the lock
			// and saves it then; it has no way to throw it away)
			v.h.Toast(lockedText)
			s.focus = true
			return false
		case s.target != nil && newControl(s, bs):
			// a mention or emoji the block didn't hold is a control
			// section of its own, which core doesn't save yet
			// (spanRuns): said while the text is still here
			v.h.Toast("Clack can't save a new mention or emoji to a canvas yet. Remove it, or press Escape again to discard your changes")
			s.refused = true
			s.focus = true
			return false
		case quipNewInLayout(v, s, md, bs):
			// Slack's new section there is the layout's child too
			// (onChildSiblingCreated), which core can't make yet
			v.h.Toast("Clack can't add a block inside a callout or column yet. Remove the new line, or press Escape again to discard your changes")
			s.refused = true
			s.focus = true
			return false
		case s.target != nil && len(bs) > 1 && s.kind0 != canvas.Code && v.h.Core().CanvasSplitMovesAnchors(v.id, s.id, s.spans, bs[0].Spans, newBlocks(bs[1:], nil)):
			// Slack's split carries a comment's anchor with its text
			// (moveComments); core can't yet, and the comment would lose
			// its place
			v.h.Toast("Clack can't move commented text into a new block yet. Undo the Enter, or press Escape again to discard your changes")
			s.refused = true
			s.focus = true
			return false
		case quipRefuses(v, s, md, bs):
			// edit-document saves a block's own text (formatting, links,
			// line breaks, its mentions and emoji), the block emptied, and
			// new blocks that keep the list they follow, for now: nothing
			// else is sent, and the text stays
			v.h.Toast(cantEdit)
			s.focus = true
			return false
		}
		if md != "" && (s.target == nil || len(bs) > 1) { // each new block's section id, so later edits reach it
			n := len(bs)
			if s.target != nil {
				n--
			}
			for i := 0; i < n; i++ {
				nid := v.h.Core().NewCanvasSectionID(v.id)
				if nid == "" { // core doesn't hold the canvas's model yet: the text stays
					v.h.Toast(cantEdit + " (the canvas is still loading)")
					s.focus = true
					return false
				}
				quipNew = append(quipNew, nid)
			}
		}
	}
	e.cur = nil
	e.pick = picker{}
	local := bs
	if s.quoted { // saved as quote sections; drawn as the quote's paragraphs
		local = unquote(bs)
	}
	md := blocksMarkdown(bs)
	var ch core.CanvasChange
	switch {
	case s.def && md == "": // restoreDefaultContent_: the line is as it was
		return true
	case s.target != nil && !s.def && (quipOn(v) && quipSame(s, bs) || !quipOn(v) && md == s.orig):
		return true
	case s.target != nil && md == "":
		if s.id == "" {
			return true
		}
		ch = core.CanvasChange{Operation: "delete", SectionID: s.id}
		e.replaceLocal(v, s.target, nil)
	case s.target != nil:
		if s.id == "" {
			return true
		}
		ch = core.CanvasChange{Operation: "replace", SectionID: s.id, Markdown: md}
		if len(local) > 0 {
			local[0].ID = s.id
		}
		for i := 1; i < len(local) && i-1 < len(quipNew); i++ {
			local[i].ID = quipNew[i-1]
			e.noteLocal(local[i], local[i-1].ID)
		}
		e.replaceLocal(v, s.target, local)
	default: // a new block
		e.dropPlace(v, s)
		if md == "" {
			return true
		}
		ch = core.CanvasChange{Operation: "insert_after", SectionID: s.afterID, Markdown: md}
		if s.afterID == "" {
			ch.Operation = "insert_at_end"
		}
		for i := 0; i < len(local) && i < len(quipNew); i++ {
			local[i].ID = quipNew[i]
			after := s.afterID
			if i > 0 {
				after = local[i-1].ID
			}
			e.noteLocal(local[i], after)
		}
		e.insertLocal(v, s.afterID, local)
	}
	if quipOn(v) { // against what the session began from (session.spans)
		id, from, seq0 := s.id, s.spans, s.seq0
		if s.kind0 == canvas.Code { // a code block holds its text as code, not spans
			from = []richtext.Span{{Kind: richtext.Text, Text: s.code0}}
		}
		if ch.Operation == "delete" {
			e.saveQuip(v, func(ctx context.Context, a *core.App) error { return a.SaveCanvasDelete(ctx, v.id, id, from) })
			return true
		}
		switch {
		case s.target == nil: // new blocks after the one the session followed
			after, blocks := s.afterID, newBlocks(bs, quipNew)
			e.saveQuip(v, func(ctx context.Context, a *core.App) error { return a.SaveCanvasNewBlocks(ctx, v.id, after, blocks) })
		case s.kind0 == canvas.Code: // a code block's text
			from, code := s.code0, bs[0].Code
			e.saveQuip(v, func(ctx context.Context, a *core.App) error { return a.SaveCanvasCode(ctx, v.id, id, from, code) })
		case len(bs) > 1: // Enter: the block's own text and new blocks after it
			first, rest := bs[0].Spans, newBlocks(bs[1:], quipNew)
			e.saveQuip(v, func(ctx context.Context, a *core.App) error {
				return a.SaveCanvasSplit(ctx, v.id, id, from, first, rest, seq0)
			})
		default: // the block's text, and its style, level or box when they changed
			b := bs[0]
			if s.quoted { // the quote's paragraph itself
				b = unquote(bs)[0]
			}
			be := core.CanvasBlockEdit{SectionID: id, Orig: from, OrigKind: s.kind0, OrigLevel: s.level0, OrigChecked: s.checked0, OrigSeq: seq0,
				Spans: b.Spans, Kind: b.Kind, Level: b.Level, Checked: b.Checked}
			e.saveQuip(v, func(ctx context.Context, a *core.App) error { return a.SaveCanvasBlock(ctx, v.id, be) })
		}
		return true
	}
	e.save(v, func(ctx context.Context, a *core.App) error { return a.EditCanvas(ctx, v.id, ch) })
	return true
}

// escape is Escape in the open block: it closes the picker; after a
// refusal whose toast says Escape again discards (Editable's), it
// discards the edit; else it saves the block. It reports whether the
// session ended.
func (e *editState) escape(v *View) bool {
	if e.pick.active() {
		e.pick = picker{}
		return false
	}
	if s := e.cur; s != nil && s.refused { // discard: the block shows what is saved
		e.discard(v)
		return true
	}
	return e.finish(v)
}

// discard ends the session without saving.
func (e *editState) discard(v *View) {
	if s := e.cur; s != nil && s.place != nil {
		e.dropPlace(v, s)
	}
	e.cur = nil
	e.pick = picker{}
	v.h.Invalidate()
}

// StartNewBlock begins typing a new paragraph after the document's last
// block (the empty area under a canvas, Slack's "Type anything…").
func (v *View) StartNewBlock() {
	e := v.edit
	if e == nil || !e.canEdit(v) || !e.finish(v) {
		return
	}
	after := ""
	for i := len(v.blocks) - 1; i >= 0; i-- {
		if v.blocks[i].b.ID != "" {
			after = v.blocks[i].b.ID
			break
		}
	}
	if quipOn(v) { // a new section through edit-document follows a text block, for now
		if ab := findBlockByID(v.blocks, after); ab == nil || !quipTextKind(ab.Kind) || ab.Kind.IsList() {
			v.h.Toast(cantEdit)
			return
		}
	}
	place := &blk{b: &canvas.Block{Kind: canvas.Paragraph}}
	v.blocks = append(v.blocks, place)
	v.list.ScrollTo(len(v.blocks)) // the header row comes first: this is the new block's row
	s := &session{afterID: after, place: place, kind: canvas.Paragraph, ed: newEditor(), focus: true, quip: quipOn(v)}
	s.ed.SetDoc(composer.NewDoc())
	e.cur = s
	v.h.Invalidate()
}

func (e *editState) dropPlace(v *View, s *session) {
	for i, b := range v.blocks {
		if b == s.place { // a new slice: a list layout in progress keeps its rows
			v.blocks = append(append([]*blk(nil), v.blocks[:i]...), v.blocks[i+1:]...)
			return
		}
	}
}

// replaceLocal shows bs in place of b at once (optimistic).
func (e *editState) replaceLocal(v *View, b *blk, bs []canvas.Block) {
	nb := v.localBlocks(bs)
	if i := indexOf(v.blocks, b); i >= 0 {
		out := append([]*blk(nil), v.blocks[:i]...)
		out = append(out, nb...)
		v.blocks = append(out, v.blocks[i+1:]...)
		return
	}
	if p := b.parent; p != nil { // a quote's or callout's paragraph: splice into its kids
		if i := indexOf(p.kids, b); i >= 0 {
			for _, k := range nb {
				k.parent = p
			}
			out := append([]*blk(nil), p.kids[:i]...)
			out = append(out, nb...)
			p.kids = append(out, p.kids[i+1:]...)
			return
		}
	}
	if len(nb) == 1 { // nested (a column, a cell): swap in place
		*b = *nb[0]
	}
}

// localBlocks are blocks an edit just made, drawn until the refetch: each
// opens as the editor can hold it (editable), as its section will be what
// the editor saved; core's check against the section (blockEditable)
// comes again once the model has the save (recheckEditable).
func (v *View) localBlocks(bs []canvas.Block) []*blk {
	nb := v.makeBlocks(v.shown, bs, nil)
	for _, b := range nb {
		if b.b != nil {
			b.edOK, b.edWhy = v.editable(*b.b)
			if b.edOK && quipOn(v) && v.titleWhy != "" {
				b.edOK, b.edWhy = false, v.titleWhy
			}
		}
	}
	return nb
}

// newEditor is a block's editor: lists no deeper than Slack's canvas
// editor makes them (maxListIndentationLevel).
func newEditor() *composer.Editor {
	ed := composer.NewEditor()
	ed.MaxIndent = quip.MaxListLevel
	return ed
}

// unquote is a quoted session's blocks as the quote's own paragraphs.
func unquote(bs []canvas.Block) []canvas.Block {
	var out []canvas.Block
	for _, b := range bs {
		if b.Kind == canvas.Quote {
			out = append(out, b.Children...)
			continue
		}
		out = append(out, b)
	}
	return out
}

func (e *editState) insertLocal(v *View, after string, bs []canvas.Block) {
	nb := v.localBlocks(bs)
	i := len(v.blocks)
	for j, b := range v.blocks {
		if after != "" && b.b.ID == after {
			i = j + 1
		}
	}
	out := append([]*blk(nil), v.blocks[:i]...)
	out = append(out, nb...)
	v.blocks = append(out, v.blocks[i:]...)
}

func indexOf(bs []*blk, b *blk) int {
	for i, x := range bs {
		if x == b {
			return i
		}
	}
	return -1
}

// localBlock is a new block this view made, with the section it follows,
// kept until the export has it.
type localBlock struct {
	b     canvas.Block
	after string
}

// noteLocal records new block b (after section after) made through
// edit-document, so a refetch older than its save doesn't take it away.
func (e *editState) noteLocal(b canvas.Block, after string) {
	if b.ID != "" {
		e.local = append(e.local, localBlock{b: b, after: after})
	}
}

// keepLocal puts back the new blocks this view made whose sections a
// refetch doesn't have yet (their save is pending, or the export is older
// than it): a session on one stays, and blocks typed after it keep their
// place. One the export has, or core no longer knows (it was dropped or
// deleted), is let go.
func (e *editState) keepLocal(v *View) {
	if e == nil || len(e.local) == 0 {
		return
	}
	a := v.h.Core()
	if a == nil || !quipOn(v) {
		e.local = nil
		return
	}
	keep := e.local[:0]
	for _, l := range e.local {
		id := l.b.ID
		if real := a.CanvasSectionID(v.id, id); findBlockByID(v.blocks, real) != nil || findBlockByID(v.blocks, id) != nil {
			continue // the export has it
		}
		if !a.CanvasSectionKnown(v.id, id) {
			continue
		}
		e.insertLocal(v, l.after, []canvas.Block{l.b})
		keep = append(keep, l)
	}
	e.local = keep
}

// docChanged keeps the session on its block when a refetch rebuilds them.
func (e *editState) docChanged(v *View) {
	if e == nil || e.cur == nil {
		return
	}
	s := e.cur
	if s.place != nil {
		i := len(v.blocks)
		for j, b := range v.blocks {
			if s.afterID != "" && b.b.ID == s.afterID {
				i = j + 1
			}
		}
		out := append([]*blk(nil), v.blocks[:i]...)
		out = append(out, s.place)
		v.blocks = append(out, v.blocks[i:]...)
		return
	}
	var found *blk
	var walk func(bs []*blk)
	walk = func(bs []*blk) {
		for _, b := range bs {
			if found != nil {
				return
			}
			if b.b.ID == s.id {
				found = b
				return
			}
			walk(b.kids)
			for _, c := range b.cols {
				walk(c)
			}
		}
	}
	walk(v.blocks)
	if found == nil && quipOn(v) { // a new block's section, under the real id the server gave it
		if real := v.h.Core().CanvasSectionID(v.id, s.id); real != s.id {
			s.id = real
			walk(v.blocks)
		}
	}
	if found == nil {
		e.cur = nil // someone deleted it
		v.h.Toast("The block you were editing was deleted")
		return
	}
	s.target = found
}

// events: clicks on the document's background save and stop editing;
// Ctrl/Cmd+B/I and the like are the composer editor's own.
func (e *editState) events(gtx layout.Context, v *View) {
	if e == nil {
		return
	}
	for {
		ev, ok := gtx.Event(pointer.Filter{Target: &e.bg, Kinds: pointer.Release})
		if !ok {
			break
		}
		if _, ok := ev.(pointer.Event); ok {
			e.finish(v)
		}
	}
	r := image.Rectangle{Max: gtx.Constraints.Max}
	area := clip.Rect(r).Push(gtx.Ops)
	event.Op(gtx.Ops, &e.bg)
	area.Pop()
}

// belowArea is the empty space under the last block: a click there adds a
// block (Slack: clicking under a canvas's content starts a new line). When
// the last block is the unedited default line, the click focuses it
// instead, emptied (Quip's isEventBelowLastSection: the last section
// gets hideDefaultContent and switchFocus).
func (e *editState) belowArea(gtx layout.Context, v *View, size image.Point) {
	if e == nil || !e.canEdit(v) {
		return
	}
	for {
		ev, ok := gtx.Event(pointer.Filter{Target: &e.saving, Kinds: pointer.Release})
		if !ok {
			break
		}
		if _, ok := ev.(pointer.Event); ok && (e.cur == nil || e.cur.place == nil) {
			if n := len(v.blocks); n > 0 && hidden(v.blocks[n-1]) && v.blocks[n-1].edOK {
				e.start(v, v.blocks[n-1])
				continue
			}
			v.StartNewBlock()
		}
	}
	defer clip.Rect{Max: size}.Push(gtx.Ops).Pop()
	defer pointer.PassOp{}.Push(gtx.Ops).Pop()
	pointer.CursorText.Add(gtx.Ops)
	event.Op(gtx.Ops, &e.saving)
}

// layoutEditor draws the editing session in place of block b.
func (e *editState) layoutEditor(gtx layout.Context, v *View, b *blk, top, w int) int {
	s := e.cur
	th := v.h.Theme()
	defer op.Offset(image.Pt(0, top)).Push(gtx.Ops).Pop()
	// keys first: the editor's Layout drops events it didn't hand out
	s.ed.Intercept = func(name key.Name, mods key.Modifiers) bool {
		if e.pick.active() && e.pick.key(name) {
			return true
		}
		if (name == key.NameReturn || name == key.NameEnter) && mods == key.ModShift && s.quip && s.kind != canvas.Code &&
			s.ed.Doc().Lines[s.ed.Doc().LineOf(cursor(s.ed))].Kind != composer.CodeBlock {
			// Slack's Shift+Enter is a line break inside the block (a <br>
			// in its section); the editor would make it a block of its
			// own, saved as a new section, so it isn't taken
			v.h.Toast("Clack can't put a line break inside a canvas block yet. Press Enter for a new block")
			return true
		}
		if (name == key.NameReturn || name == key.NameEnter) && mods == 0 && s.kind != canvas.Code {
			if s.ed.Doc().Lines[s.ed.Doc().LineOf(cursor(s.ed))].Kind == composer.CodeBlock {
				return false
			}
			s.ed.InsertText("\n")
			return true
		}
		return false
	}
	for {
		ev, ok := s.ed.Update(gtx)
		if !ok {
			break
		}
		switch ev.Kind {
		case composer.EvCancel:
			if e.escape(v) {
				return gtx.Dp(bodyLine)
			}
		case composer.EvSubmit: // Ctrl/Cmd+Enter saves
			if e.finish(v) {
				return gtx.Dp(bodyLine)
			}
		case composer.EvChange:
			s.refused = false // changed since the refusal: Escape saves again, never discards it
			shortcuts(s)
			v.cover.typed()
		}
	}
	if s.focus {
		s.ed.Focus(gtx)
		s.focus = false
	}
	// the gutter marker, at the block's level
	kind := s.kind
	tx := 0
	lineH := gtx.Dp(unit.Dp(lineFor(kind)))
	if kind.IsList() {
		probe := &blk{b: &canvas.Block{Kind: kind, Level: s.level, Number: max(1, b.b.Number), Checked: s.checked}}
		x := gtx.Dp(indentDp) * s.level
		tx = x + v.drawMarker(gtx, probe, x, lineH)
	}
	st := editorStyle(th, kind, v.h)
	if s.target != nil && titleSection(v.blocks) && v.blocks[0] == s.target {
		st.TextSize, st.LineHeight = titleSize, titleLine // the title section
		st.EmojiSize = titleLine                          // its emoji: 34px (data-section-style 48 control.c54 img)
		lineH = gtx.Dp(titleLine)
		st.PlaceholderText = nsCanvas.T(titlePlaceholder) // TitleSectionOverlay, until the title has text
	} else if s.target != nil && s.target == v.placeholderRow() {
		st.PlaceholderText = v.textPlaceholder() // TextSectionOverlay
	} else if s.def {
		// TextSectionOverlay's secondary overlay (Slack sets
		// slashShortcutCommandsNuxEnabled): a focused default line in a
		// canvas with other text shows its saved default content
		// (contentForSecondaryOverlay) until something is typed
		st.PlaceholderText = s.defText
	}
	st.Embolden = v.styles.body.Embolden // richview's ink, so the text doesn't go grey while edited
	g := gtx
	g.Constraints = layout.Constraints{Max: image.Pt(max(1, w-tx), gtx.Constraints.Max.Y)}
	var d layout.Dimensions
	func() {
		defer op.Offset(image.Pt(tx, 0)).Push(gtx.Ops).Pop()
		if p := s.press; p != nil && !p.placed {
			// the press that opened the editor: lay the text out once
			// (not drawn) to find the caret position under it, so the
			// first frame already shows the caret there
			m := op.Record(gtx.Ops)
			s.ed.Layout(g, st)
			m.Stop()
			pos := s.ed.HitPos(p.pt)
			p.placed, p.anchor = true, pos
			s.ed.SetSelection(pos, pos)
			s.ed.SeedClick(p.at, p.pt)
			if to := p.to; to != nil {
				if d := to.Sub(p.pt); d.X*d.X+d.Y*d.Y > 9 {
					s.ed.SetSelection(pos, s.ed.HitPos(*to))
				}
			}
		}
		e.pressEvents(gtx, s, b, image.Pt(max(1, w-tx), gtx.Constraints.Max.Y))
		d = s.ed.Layout(g, st)
	}()
	h := max(d.Size.Y, lineH)
	e.layoutSelectionBar(gtx, v, s, image.Pt(tx, 0), w-tx)
	e.pick.update(v, s)
	if e.pick.active() {
		cr := s.ed.CaretRect().Add(image.Pt(tx, 0))
		e.pick.layout(gtx, v, s, image.Pt(cr.Min.X, cr.Max.Y+gtx.Dp(4)), w)
	}
	return h + top
}

func cursor(ed *composer.Editor) int { _, c := ed.Selection(); return c }

func lineFor(k canvas.Kind) float32 {
	switch k {
	case canvas.Heading1:
		return h1Line
	case canvas.Heading2:
		return h2Line
	case canvas.Heading3:
		return h3Line
	}
	return bodyLine
}

func editorStyle(th *theme.Theme, k canvas.Kind, h Host) composer.EditorStyle {
	c := &th.C
	st := composer.DefaultEditorStyle(th.Shaper)
	st.TextSize, st.LineHeight = bodySize, bodyLine
	switch k {
	case canvas.Heading1:
		st.TextSize, st.LineHeight = h1Size, h1Line
	case canvas.Heading2:
		st.TextSize, st.LineHeight = h2Size, h2Line
	case canvas.Heading3:
		st.TextSize, st.LineHeight = h3Size, h3Line
	}
	if k == canvas.Heading1 || k == canvas.Heading2 || k == canvas.Heading3 {
		st.Weight = font.Black // as richview draws it, so the text (and a click's caret) stays put
	}
	ink := docText(th.Dark)
	st.Color, st.Placeholder, st.Selection, st.Caret = ink, placeholderText(th.Dark), c.Selection, ink
	st.LinkColor, st.CodeColor, st.CodeBg, st.CodeBorder = c.Link, c.CodeFg, c.CodeBg, c.CodeBorder
	st.PreBg, st.PreBorder, st.QuoteBar = c.PreBg, c.CodeBorder, c.QuoteBar
	st.MentionColor, st.MentionBg, st.BroadcastColor, st.BroadcastBg = c.Mention, c.MentionBg, c.Fg, c.SelfMention
	st.PlaceholderText = "" // Slack's empty line hint offers "/" commands, which Clack has no menu for
	st.MaxHeight = 0
	st.EmojiSize = unit.Dp(lineFor(k))            // control.c54 img: the section's line height (as render.go draws it)
	st.NativeEmojiGap = unit.Dp(nativeEmojiGap()) // as render.go draws it
	if im := h.Images(); im != nil {
		st.Images = im.Get
	}
	return st
}

// shortcuts applies the canvas's markdown shortcuts at the start of the
// first line, and takes the composer's own list autoformat ("- ", "1. ")
// as a change of block kind (the gutter draws the marker).
func shortcuts(s *session) {
	d := s.ed.Doc()
	if len(d.Lines) == 0 {
		return
	}
	switch d.Lines[0].Kind {
	case composer.Bullet:
		s.kind, s.level = canvas.Bullet, d.Lines[0].Indent
		d.SetLineKind(0, 0, composer.Para)
		s.ed.Touch()
	case composer.Ordered:
		s.kind, s.level = canvas.Numbered, d.Lines[0].Indent
		d.SetLineKind(0, 0, composer.Para)
		s.ed.Touch()
	}
	t := d.LineText(0)
	for _, p := range []struct {
		pre  string
		kind canvas.Kind
	}{{"### ", canvas.Heading3}, {"## ", canvas.Heading2}, {"# ", canvas.Heading1}, {"[] ", canvas.Check}, {"[ ] ", canvas.Check}, {"[x] ", canvas.Check}} {
		if strings.HasPrefix(t, p.pre) && s.kind != canvas.Code {
			n := len([]rune(p.pre))
			c := max(0, cursor(s.ed)-n)
			d.Delete(0, n)
			s.kind = p.kind
			s.checked = p.pre == "[x] "
			if p.kind == canvas.Check {
				s.level = 0
			}
			s.ed.SetSelection(c, c)
			s.ed.Touch()
			return
		}
	}
}

// ---- conversions between canvas blocks and the composer's Doc ----

var styleMap = []struct {
	r richtext.Style
	c composer.Style
}{{richtext.Bold, composer.Bold}, {richtext.Italic, composer.Italic}, {richtext.Strike, composer.Strike},
	{richtext.Underline, composer.Underline}, {richtext.Code, composer.Code}}

func toComposer(st richtext.Style) composer.Style {
	var c composer.Style
	for _, m := range styleMap {
		if st&m.r != 0 {
			c |= m.c
		}
	}
	return c
}

func fromComposer(c composer.Style) richtext.Style {
	var st richtext.Style
	for _, m := range styleMap {
		if c&m.c != 0 {
			st |= m.r
		}
	}
	return st
}

// toDoc is a block's text as an editor document.
func toDoc(b *canvas.Block) *composer.Doc {
	d := composer.NewDoc()
	if b.Kind == canvas.Code {
		d.Insert(0, b.Code, composer.Attr{})
		for i := range d.Lines {
			d.Lines[i].Kind = composer.CodeBlock
		}
		return d
	}
	pos := 0
	for _, s := range b.Spans {
		a := composer.Attr{Style: toComposer(s.Style)}
		switch s.Kind {
		case richtext.Break:
			pos = d.Insert(pos, "\n", composer.Attr{})
		case richtext.Link:
			a.Link = s.URL
			pos = d.Insert(pos, s.Text, a)
		case richtext.User:
			pos = d.InsertAtom(pos, &composer.Atom{Kind: composer.AtomUser, ID: s.ID, Text: s.Text})
		case richtext.Channel:
			pos = d.InsertAtom(pos, &composer.Atom{Kind: composer.AtomChannel, ID: s.ID, Text: s.Text})
		case richtext.Usergroup:
			pos = d.InsertAtom(pos, &composer.Atom{Kind: composer.AtomUsergroup, ID: s.ID, Text: s.Text})
		case richtext.Broadcast:
			pos = d.InsertAtom(pos, &composer.Atom{Kind: composer.AtomBroadcast, ID: s.ID, Text: s.Text})
		case richtext.Emoji:
			pos = d.InsertAtom(pos, &composer.Atom{Kind: composer.AtomEmoji, ID: s.ID, Text: ":" + s.ID + ":", URL: s.URL})
		default:
			pos = d.Insert(pos, s.Text, a)
		}
	}
	return d
}

// fromDoc turns the editor's lines back into blocks: the first line keeps
// the session's kind, later lines continue a list (or are paragraphs);
// lines the composer formatted (quotes, code, headings, dividers) take
// that kind.
func fromDoc(d *composer.Doc, s *session) []canvas.Block {
	var out []canvas.Block
	for i := range d.Lines {
		lo, hi := d.LineStart(i), d.LineEnd(i)
		sp := lineSpans(d, lo, hi, !s.quip)
		ln := d.Lines[i]
		var b canvas.Block
		switch ln.Kind {
		case composer.CodeBlock:
			txt := string(d.Runes[lo:hi])
			if n := len(out); n > 0 && out[n-1].Kind == canvas.Code {
				out[n-1].Code += "\n" + txt
				continue
			}
			out = append(out, canvas.Block{Kind: canvas.Code, Code: txt})
			continue
		case composer.Divider:
			out = append(out, canvas.Block{Kind: canvas.Divider})
			continue
		case composer.Quote:
			b = canvas.Block{Kind: canvas.Quote, Children: []canvas.Block{{Kind: canvas.Paragraph, Spans: sp}}}
		case composer.Header1, composer.Header2, composer.Header3:
			b = canvas.Block{Kind: map[composer.BlockKind]canvas.Kind{composer.Header1: canvas.Heading1,
				composer.Header2: canvas.Heading2, composer.Header3: canvas.Heading3}[ln.Kind], Spans: sp}
		case composer.Bullet:
			k := canvas.Bullet
			if s.kind == canvas.Check {
				k = canvas.Check
			}
			b = canvas.Block{Kind: k, Level: ln.Indent, Spans: sp}
		case composer.Ordered:
			b = canvas.Block{Kind: canvas.Numbered, Level: ln.Indent, Spans: sp}
		default:
			switch {
			case i == 0:
				b = canvas.Block{Kind: s.kind, Level: s.level, Checked: s.checked, Spans: sp}
			case s.kind.IsList():
				b = canvas.Block{Kind: s.kind, Level: s.level, Spans: sp}
			default:
				b = canvas.Block{Kind: canvas.Paragraph, Spans: sp}
			}
		}
		if !s.quip && (b.Kind == canvas.Heading1 || b.Kind == canvas.Heading2 || b.Kind == canvas.Heading3) {
			b.Spans = unbold(b.Spans) // canvas markdown's heading is bold already
		}
		out = append(out, b)
	}
	// number the list items
	count := map[int]int{}
	for i := range out {
		if out[i].Kind == canvas.Numbered {
			count[out[i].Level]++
			out[i].Number = count[out[i].Level]
		} else if !out[i].Kind.IsList() {
			count = map[int]int{}
		}
	}
	if s.target != nil && s.kind == canvas.Numbered && len(out) > 0 && out[0].Kind == canvas.Numbered {
		base := max(1, s.target.b.Number) - 1
		for i := range out {
			if out[i].Kind == canvas.Numbered && out[i].Level == out[0].Level {
				out[i].Number += base
			}
		}
	}
	// trailing empty lines are dropped; an editor holding nothing is no block
	for len(out) > 0 && emptyBlock(out[len(out)-1]) {
		out = out[:len(out)-1]
	}
	if s.quoted { // each line is a quote section of its own
		for i, b := range out {
			if b.Kind == canvas.Paragraph {
				out[i] = canvas.Block{Kind: canvas.Quote, Children: []canvas.Block{b}}
			}
		}
	}
	return out
}

func emptyBlock(b canvas.Block) bool {
	switch b.Kind {
	case canvas.Code:
		return strings.TrimSpace(b.Code) == ""
	case canvas.Divider:
		return false
	case canvas.Quote:
		return len(b.Children) == 0 || blankSpans(b.Children[0].Spans)
	}
	return blankSpans(b.Spans)
}

// blankSpans: no text and no inline object. A mention counts whatever it
// reads (one still loading has no text yet).
func blankSpans(sp []richtext.Span) bool {
	for _, s := range sp {
		switch s.Kind {
		case richtext.Text, richtext.Link, richtext.Break:
			if strings.TrimSpace(s.Text) != "" {
				return false
			}
		default:
			return false
		}
	}
	return true
}

func unbold(sp []richtext.Span) []richtext.Span {
	out := make([]richtext.Span, len(sp))
	for i, s := range sp {
		s.Style &^= richtext.Bold
		out[i] = s
	}
	return out
}

// lineSpans converts runes [lo, hi) to spans; trim drops trailing spaces
// (canvas markdown's save: edit-document keeps them, as Slack's pre-wrap
// editor does, the space after a picked mention too).
func lineSpans(d *composer.Doc, lo, hi int, trim bool) []richtext.Span {
	var out []richtext.Span
	var cur richtext.Span
	flush := func() {
		if cur.Text != "" {
			out = append(out, cur)
		}
		cur = richtext.Span{}
	}
	for i := lo; i < hi; i++ {
		a := d.Attrs[i]
		if at := a.Atom; at != nil {
			flush()
			sp := richtext.Span{ID: at.ID, Text: at.Text, URL: at.URL}
			switch at.Kind {
			case composer.AtomUser:
				sp.Kind = richtext.User
			case composer.AtomChannel:
				sp.Kind = richtext.Channel
			case composer.AtomUsergroup:
				sp.Kind = richtext.Usergroup
			case composer.AtomBroadcast:
				sp.Kind, sp.Self = richtext.Broadcast, true
			case composer.AtomEmoji:
				sp.Kind, sp.Text = richtext.Emoji, ""
			}
			out = append(out, sp)
			continue
		}
		k, st := richtext.Text, fromComposer(a.Style)
		if a.Link != "" {
			k = richtext.Link
		}
		if cur.Text != "" && (cur.Kind != k || cur.Style != st || cur.URL != a.Link) {
			flush()
		}
		cur.Kind, cur.Style, cur.URL = k, st, a.Link
		cur.Text += string(d.Runes[i])
	}
	flush()
	// trailing spaces (the one after an inserted pill) aren't saved
	for trim && len(out) > 0 {
		l := &out[len(out)-1]
		if l.Kind != richtext.Text && l.Kind != richtext.Link {
			break
		}
		l.Text = strings.TrimRight(l.Text, " \t\u00a0")
		if l.Text != "" {
			break
		}
		out = out[:len(out)-1]
	}
	return out
}

// blocksMarkdown joins blocks as canvas markdown (list items on adjacent
// lines, other blocks separated by a blank line).
func blocksMarkdown(bs []canvas.Block) string {
	var b strings.Builder
	for i, x := range bs {
		if i > 0 {
			if x.Kind.IsList() && bs[i-1].Kind.IsList() {
				b.WriteByte('\n')
			} else {
				b.WriteString("\n\n")
			}
		}
		b.WriteString(canvas.Markdown(x))
	}
	return b.String()
}

// ---- Slack's canvas toolbars ----
//
// Slack draws no formatting bar while a block is edited. An editor gets
// the insertion toolbar at the bottom of the canvas
// (InsertionToolbarButtons in client-boot-shared: insert, formatting,
// emoji, file, checklist, table, layouts), and selecting text shows the
// floating formatting bar over the selection (QuipFloatingFormattingBar,
// buttons from getButtonsAndFormats' canvas set).

// blockFormat is one of Slack's block formats, with its label
// (getToolbarButtonLabel / the insertion menus) and icon
// (getDerivedFormatSvgIcon).
type blockFormat struct{ key, label, icon string }

var (
	fmtParagraph = blockFormat{"para", "Paragraph", "paragraph"}
	fmtH1        = blockFormat{"h1", "Big heading", "heading-1"}
	fmtH2        = blockFormat{"h2", "Medium heading", "heading-2"}
	fmtH3        = blockFormat{"h3", "Small heading", "heading-3"}
	fmtCheck     = blockFormat{"check", "Check list", "checkbox"}
	fmtOrdered   = blockFormat{"numbered", "Ordered list", "list-ordered"}
	fmtBullet    = blockFormat{"bullet", "Bulleted list", "list-bullet"}
	fmtCodeBlock = blockFormat{"codeblock", "Code block", "code-block"}
	fmtQuote     = blockFormat{"quote", "Blockquote", "quote"}
	fmtDivider   = blockFormat{}
)

// The blockquote has its own button (the floating bar's blockquote-layout)
// and an Insert menu item; neither block-style menu lists it. Slack shipped
// that layout to everyone and dropped canvas_blockquote_layout (LlXB
// InsertionToolbarFormattingMenu's fixed list, 27hg's /blockquote; build
// 8f19be21ed7e, the 2026-11-21 drift).

// blockFormats are every block format with its icon: the block-style
// menu trigger draws the current one's, a quote's included.
var blockFormats = []blockFormat{fmtParagraph, fmtH1, fmtH2, fmtH3, fmtCheck, fmtOrdered, fmtBullet, fmtCodeBlock, fmtQuote}

// selectionMenu is the floating bar's block-style menu (zk's menuItems:
// paragraph, h1, h2, h3, ─, check-list, ordered-list, bullet-list, ─,
// code-block).
var selectionMenu = []blockFormat{fmtParagraph, fmtH1, fmtH2, fmtH3, fmtDivider, fmtCheck, fmtOrdered, fmtBullet, fmtDivider, fmtCodeBlock}

// formattingMenu is the bottom toolbar's Aa menu
// (InsertionToolbarFormattingMenu, which opens upwards: code block,
// bulleted, ordered, check list, ─, h3, h2, h1, paragraph).
var formattingMenu = []blockFormat{fmtCodeBlock, fmtBullet, fmtOrdered, fmtCheck, fmtDivider, fmtH3, fmtH2, fmtH1, fmtParagraph}

// current is the session's block format key.
func (s *session) current() string {
	switch s.kind {
	case canvas.Heading1:
		return "h1"
	case canvas.Heading2:
		return "h2"
	case canvas.Heading3:
		return "h3"
	case canvas.Bullet:
		return "bullet"
	case canvas.Numbered:
		return "numbered"
	case canvas.Check:
		return "check"
	}
	switch s.ed.ActiveBlock() {
	case composer.Quote:
		return "quote"
	case composer.CodeBlock:
		return "codeblock"
	}
	return "para"
}

func (s *session) icon() string {
	for _, f := range blockFormats {
		if f.key == s.current() {
			return f.icon
		}
	}
	return "paragraph"
}

// openFormatMenu shows formats as a menu with the session's checked.
func (e *editState) openFormatMenu(v *View, formats []blockFormat) {
	s := e.cur
	if s == nil || v.act.Menu == nil {
		return
	}
	cur := s.current()
	var items []MenuItem
	for _, f := range formats {
		if f.key == "" {
			items = append(items, MenuItem{Divider: true})
			continue
		}
		k := f.key
		items = append(items, MenuItem{Label: nsCanvas.T(f.label), Icon: f.icon, Checked: k == cur, Do: func() { v.post(func() { e.apply(v, k, image.Point{}) }) }})
	}
	v.act.Menu(items)
}

// openInsertMenu is the bottom toolbar's insert menu
// (InsertionToolbarMenu): Divider, then Date, Profile, Callout, Canvas,
// Image and File. What canvases.edit's markdown can make works through
// it: a divider ("---"), a profile card (a mention alone in its block,
// "![](@U…)", which Slack unfurls as a card), an image (uploaded, then
// "![name](permalink)") and a file ("![](permalink)", a file unfurl).
// Date and Callout have no canvas markdown, and Slack's Canvas item is a
// canvas search Clack doesn't have yet: those three are drawn in Slack's
// disabled state (.c-menu_item__button--disabled, opacity .5). Blockquote
// follows Callout (BLOCKQUOTE_LAYOUT): a new quote after the insert point
// (insertSection("blockquote-layout")).
func (e *editState) openInsertMenu(v *View) {
	if v.act.Menu == nil {
		return
	}
	items := []MenuItem{
		{Label: nsCanvas.T("Divider"), Icon: "divider", Do: func() { v.post(func() { e.insertDivider(v) }) }},
		{Divider: true},
		{Label: nsCanvas.T("Date"), Icon: "calendar", Disabled: true},
		{Label: nsCanvas.T("Profile"), Icon: "user", Do: func() { v.post(func() { e.insertProfile(v) }) }},
		{Label: nsCanvas.T("Callout"), Icon: "callout", Disabled: true},
		{Label: nsCanvas.T("Blockquote"), Icon: "quote", Do: func() { v.post(func() { e.insertQuote(v) }) }},
		{Label: nsCanvas.T("Canvas"), Icon: "canvas", Disabled: true},
		{Label: nsCanvas.T("Image"), Icon: "image", Do: func() { v.post(func() { e.insertUpload(v, true) }) }},
		{Label: nsCanvas.T("File"), Icon: "attach", Do: func() { v.post(func() { e.insertUpload(v, false) }) }},
	}
	v.act.Menu(items)
}

// insertDivider adds a divider after the block being edited (else at the
// end): canvases.edit insert_after with canvas markdown's "---".
func (e *editState) insertDivider(v *View) {
	div := canvas.Block{Kind: canvas.Divider}
	e.insertBlocks(v, canvas.Markdown(div), []canvas.Block{div})
}

// insertion toolbar buttons, in Slack's order.
type insButton struct {
	key, icon string
	primary   bool // the green circular insert button
}

var insButtons = []insButton{
	{"insert", "plus", true}, {"formatting", "text-format", false}, {"emoji", "emoji", false},
	{"file", "attach", false}, {"checklist", "checkbox", false}, {"table", "spreadsheet", false},
	{"layouts", "column-two", false},
}

// insEnabled: what Clack can do from the insertion toolbar. Insert file
// uploads and embeds a file, Insert table adds Quip's new table (3 rows,
// 2 columns) as a canvas markdown table; the layouts menu (columns) has
// no canvas markdown, so it is drawn in Slack's disabled state
// (.c-icon_button.c-button--disabled: opacity .5). Formatting needs a
// block being edited (Slack disables it outside a text section).
func (e *editState) insEnabled(k string) bool {
	switch k {
	case "layouts":
		return false
	case "formatting":
		return e.cur != nil
	}
	return true
}

// layoutInsertionToolbar is Slack's .p-quip_insertion_toolbar, shown to
// anyone who can edit: centred at the canvas's bottom, 18px up (24px in
// the side panel), on the primary background with a 1px ring and a
// 0 4px 12px shadow, radius 8, 2px padding; 30px buttons with 20px icons,
// 5px around the first and 2px around the rest.
func (e *editState) layoutInsertionToolbar(gtx layout.Context, v *View) {
	if e == nil || !e.canEdit(v) {
		return
	}
	th := v.h.Theme()
	c := &th.C
	sz := gtx.Constraints.Max
	bw := gtx.Dp(30)
	inner := gtx.Dp(5) + bw + gtx.Dp(5) + (len(insButtons)-1)*(gtx.Dp(2)+bw+gtx.Dp(2))
	w, h := inner+2*gtx.Dp(2), bw+2*gtx.Dp(5)+2*gtx.Dp(2)
	bottom := gtx.Dp(18)
	if v.mode == Side {
		bottom = gtx.Dp(24)
	}
	x := (sz.X - w) / 2
	r := image.Rect(x, sz.Y-bottom-h, x+w, sz.Y-bottom)
	theme.Shadow(gtx, r.Add(image.Pt(0, gtx.Dp(4))), gtx.Dp(8), gtx.Dp(12), theme.WithAlpha(c.PanelShadow, 0x1a))
	theme.RRect(gtx, r, gtx.Dp(8), c.Bg)
	theme.Border(gtx, r, gtx.Dp(8), gtx.Dp(1), theme.WithAlpha(c.Fg, 0x21))
	area := clip.Rect(r).Push(gtx.Ops) // clicks between the buttons don't reach the background
	event.Op(gtx.Ops, &e.insBar)
	area.Pop()
	bx := r.Min.X + gtx.Dp(2) + gtx.Dp(5)
	by := r.Min.Y + gtx.Dp(2) + gtx.Dp(5)
	for i, b := range insButtons {
		if i > 0 {
			bx += gtx.Dp(2)
		}
		b := b
		on := e.insEnabled(b.key)
		clk := e.btn("ins." + b.key)
		hit := clk.Clicked(gtx) // before Layout, which drains a Clickable's events
		at(gtx, image.Pt(bx, by), image.Pt(bw, bw), func(gtx layout.Context) layout.Dimensions {
			s := gtx.Constraints.Max
			col := c.Fg
			switch {
			case b.primary:
				theme.RRect(gtx, image.Rectangle{Max: s}, s.Y/2, c.Send)
				col = c.SendText
			case !on:
				col = theme.WithAlpha(col, col.A/2) // c-button--disabled: opacity .5
			case clk.Hovered():
				theme.RRect(gtx, image.Rectangle{Max: s}, gtx.Dp(8), c.BgHover)
			}
			p := gtx.Dp(20)
			at(gtx, image.Pt((s.X-p)/2, (s.Y-p)/2), image.Pt(p, p), func(gtx layout.Context) layout.Dimensions {
				return icons.Get(b.icon)(gtx, 20, col)
			})
			if !on {
				return layout.Dimensions{Size: s}
			}
			return clk.Layout(gtx, func(gtx layout.Context) layout.Dimensions { return layout.Dimensions{Size: s} })
		})
		if hit && on {
			e.insertion(v, b.key, image.Pt(bx, r.Min.Y))
		}
		bx += bw
		if i == 0 {
			bx += gtx.Dp(5)
		} else {
			bx += gtx.Dp(2)
		}
	}
}

// insertion runs an insertion toolbar button.
func (e *editState) insertion(v *View, k string, anchor image.Point) {
	switch k {
	case "insert":
		e.openInsertMenu(v)
	case "formatting":
		e.openFormatMenu(v, formattingMenu)
	case "file":
		e.insertUpload(v, false)
	case "table":
		e.insertTable(v)
	case "emoji", "checklist":
		if e.cur == nil {
			v.StartNewBlock() // no caret: Slack inserts at the end
		}
		if e.cur == nil {
			return
		}
		if k == "checklist" {
			k = "checktoggle"
		}
		e.apply(v, k, anchor)
	}
}

// floating formatting bar buttons (the canvas set of
// getButtonsAndFormats: [menu] [bold italic underline strike] [code]
// [link callout]).
type selButton struct {
	key, icon string
	menu      bool // the block-style menu trigger: the format's icon and a caret
}

var selGroups = [][]selButton{
	{{key: "menu", menu: true}},
	{{key: "bold", icon: "bold"}, {key: "italic", icon: "italic"}, {key: "underline", icon: "underline"}, {key: "strike", icon: "strike"}},
	// zk's [blockquote-layout] (the "quote" icon, getDerivedFormatSvgIcon's
	// BLOCKQUOTE_LAYOUT)
	{{key: "bqlayout", icon: "quote"}},
	{{key: "code", icon: "code"}},
	{{key: "link", icon: "link"}, {key: "callout", icon: "callout"}},
}

// selEnabled: canvases.edit's markdown has no underline, and Clack can't
// make callouts yet; those two are drawn disabled.
func selEnabled(k string) bool { return k != "underline" && k != "callout" }

var (
	floatBg      = color.NRGBA{R: 0x1a, G: 0x1d, B: 0x21, A: 0xff} // .p-composer__body--floating, both themes
	floatRing    = color.NRGBA{R: 0xe8, G: 0xe8, B: 0xe8, A: 0x40}
	floatSep     = color.NRGBA{R: 0xff, G: 0xff, B: 0xff, A: 0x40}
	floatHover   = color.NRGBA{R: 0xff, G: 0xff, B: 0xff, A: 0x13}
	floatPressed = color.NRGBA{R: 0xe8, G: 0xe8, B: 0xe8, A: 0x21}
	floatOff     = color.NRGBA{R: 0xff, G: 0xff, B: 0xff, A: 0x4d}
	white        = color.NRGBA{R: 0xff, G: 0xff, B: 0xff, A: 0xff}
)

// layoutSelectionBar floats Slack's formatting bar over the editor's
// selection (it shows only while text is selected: QuipFloatingFormattingBar
// needs a non-collapsed, non-empty range). origin is the editor's offset in
// gtx; it is drawn last (op.Defer), over the document.
func (e *editState) layoutSelectionBar(gtx layout.Context, v *View, s *session, origin image.Point, w int) {
	a, b := s.ed.Selection()
	if a == b {
		return
	}
	lo, hi := min(a, b), max(a, b)
	r1, r2 := s.ed.PosRect(lo), s.ed.PosRect(hi)
	groups := selGroups
	bh := gtx.Dp(28) + 2*gtx.Dp(2) + 2*gtx.Dp(2) // buttons, their margins, the body's padding
	bw := 2 * gtx.Dp(2)
	for gi, g := range groups {
		if gi > 0 {
			bw += gtx.Dp(9) // separator: 1px, 4px each side
		}
		for _, sb := range g {
			bw += gtx.Dp(28) + 2*gtx.Dp(2)
			if sb.menu {
				bw += gtx.Dp(16)
			}
		}
	}
	cx := r1.Min.X
	if r1.Min.Y == r2.Min.Y {
		cx = (r1.Min.X + r2.Min.X) / 2
	}
	x := origin.X + min(max(0, cx-bw/2), max(0, w-bw))
	y := origin.Y + r1.Min.Y - bh - gtx.Dp(8)
	m := op.Record(gtx.Ops)
	func() {
		defer op.Offset(image.Pt(x, y)).Push(gtx.Ops).Pop()
		rr := image.Rect(0, 0, bw, bh)
		theme.Shadow(gtx, rr.Add(image.Pt(0, gtx.Dp(1))), gtx.Dp(8), gtx.Dp(3), color.NRGBA{A: 0x14})
		theme.RRect(gtx, rr, gtx.Dp(8), floatBg)
		theme.Border(gtx, rr, gtx.Dp(8), gtx.Dp(1), floatRing)
		area := clip.Rect(rr).Push(gtx.Ops)
		event.Op(gtx.Ops, &e.tool)
		area.Pop()
		active := s.ed.ActiveStyle()
		bx := gtx.Dp(2)
		by := gtx.Dp(2) + gtx.Dp(2)
		for gi, g := range groups {
			if gi > 0 {
				theme.Fill(gtx, image.Rect(bx+gtx.Dp(4), (bh-gtx.Dp(20))/2, bx+gtx.Dp(5), (bh+gtx.Dp(20))/2), floatSep)
				bx += gtx.Dp(9)
			}
			for _, sb := range g {
				sb := sb
				bx += gtx.Dp(2)
				w := gtx.Dp(28)
				if sb.menu {
					w += gtx.Dp(16)
				}
				on := selEnabled(sb.key)
				pressed := false
				switch sb.key {
				case "bold":
					pressed = active&composer.Bold != 0
				case "italic":
					pressed = active&composer.Italic != 0
				case "strike":
					pressed = active&composer.Strike != 0
				case "code":
					pressed = active&composer.Code != 0
				case "link":
					pressed = s.ed.ActiveLink()
				case "bqlayout": // activeFormats has blockquote-layout in a quote
					pressed = s.current() == "quote"
				}
				clk := e.btn("sel." + sb.key)
				hit := clk.Clicked(gtx) // before Layout, which drains a Clickable's events
				at(gtx, image.Pt(bx, by), image.Pt(w, gtx.Dp(28)), func(gtx layout.Context) layout.Dimensions {
					sz := gtx.Constraints.Max
					switch {
					case !on:
					case pressed && clk.Hovered():
						theme.RRect(gtx, image.Rectangle{Max: sz}, gtx.Dp(4), theme.WithAlpha(floatPressed, 0x33))
					case pressed:
						theme.RRect(gtx, image.Rectangle{Max: sz}, gtx.Dp(4), floatPressed)
					case clk.Hovered():
						theme.RRect(gtx, image.Rectangle{Max: sz}, gtx.Dp(4), floatHover)
					}
					col := white
					if !on {
						col = floatOff
					}
					icon := sb.icon
					if sb.menu {
						icon = s.icon()
					}
					p := gtx.Dp(18)
					ix := (gtx.Dp(28) - p) / 2
					at(gtx, image.Pt(ix, (sz.Y-p)/2), image.Pt(p, p), func(gtx layout.Context) layout.Dimensions {
						return icons.Get(icon)(gtx, 18, col)
					})
					if sb.menu {
						cp := gtx.Dp(14)
						at(gtx, image.Pt(ix+p, (sz.Y-cp)/2), image.Pt(cp, cp), func(gtx layout.Context) layout.Dimensions {
							return icons.Get("caret-down")(gtx, 14, col)
						})
					}
					if !on {
						return layout.Dimensions{Size: sz}
					}
					return clk.Layout(gtx, func(gtx layout.Context) layout.Dimensions { return layout.Dimensions{Size: sz} })
				})
				if hit && on {
					if sb.menu {
						e.openFormatMenu(v, selectionMenu)
					} else {
						e.apply(v, sb.key, image.Point{})
					}
				}
				bx += w + gtx.Dp(2)
			}
		}
	}()
	op.Defer(gtx.Ops, m.Stop())
}

// apply runs a toolbar or menu action on the session.
func (e *editState) apply(v *View, k string, anchor image.Point) {
	s := e.cur
	if s == nil {
		return
	}
	ed := s.ed
	setKind := func(nk canvas.Kind) {
		was := s.kind
		if was == nk && nk != canvas.Paragraph {
			nk = canvas.Paragraph
		}
		s.kind = nk
		if nk == canvas.Check && was != canvas.Check {
			s.checked = false
		}
		if !nk.IsList() {
			s.level = 0
		}
	}
	plain := func() { // a block format replaces a quote or code line format
		switch ed.ActiveBlock() {
		case composer.Quote:
			ed.ToggleBlock(composer.Quote)
		case composer.CodeBlock:
			ed.ToggleBlock(composer.CodeBlock)
		}
	}
	switch k {
	case "bold":
		ed.ToggleStyle(composer.Bold)
	case "italic":
		ed.ToggleStyle(composer.Italic)
	case "strike":
		ed.ToggleStyle(composer.Strike)
	case "code":
		ed.ToggleStyle(composer.Code)
	case "link":
		sel := strings.TrimSpace(ed.SelectedText())
		if strings.HasPrefix(sel, "https://") || strings.HasPrefix(sel, "http://") {
			ed.SetLink(sel, sel)
		} else {
			v.h.Toast("Select a link's address to make it a link")
		}
	case "h1":
		plain()
		setKind(canvas.Heading1)
	case "h2":
		plain()
		setKind(canvas.Heading2)
	case "h3":
		plain()
		setKind(canvas.Heading3)
	case "para":
		plain()
		s.kind, s.level = canvas.Paragraph, 0
	case "bullet":
		plain()
		setKind(canvas.Bullet)
	case "numbered":
		plain()
		setKind(canvas.Numbered)
	case "check":
		plain()
		if s.kind != canvas.Check {
			setKind(canvas.Check)
		}
	case "checktoggle": // Insert checklist (CHECK_LIST_TOGGLE)
		plain()
		setKind(canvas.Check)
	case "quote":
		s.kind, s.level = canvas.Paragraph, 0
		if ed.ActiveBlock() != composer.Quote {
			ed.ToggleBlock(composer.Quote)
		}
	case "bqlayout": // performBlockquoteConversion(!activeFormat): in or out of the quote
		if ed.ActiveBlock() == composer.Quote {
			ed.ToggleBlock(composer.Quote)
			break
		}
		s.kind, s.level = canvas.Paragraph, 0
		plain()
		ed.ToggleBlock(composer.Quote)
	case "codeblock":
		s.kind, s.level = canvas.Paragraph, 0
		if ed.ActiveBlock() != composer.CodeBlock {
			ed.ToggleBlock(composer.CodeBlock)
		}
	case "emoji":
		if ep, ok := v.h.(EmojiPicker); ok {
			ep.OpenEmojiPicker(anchor, func(name string) { v.post(func() { e.insertEmoji(v, name) }) })
		}
	}
	s.focus = true
	v.h.Invalidate()
}

// EmojiPicker is optional on the Host: the shell's emoji picker.
type EmojiPicker interface {
	OpenEmojiPicker(anchor image.Point, onPick func(name string))
}

func (e *editState) insertEmoji(v *View, name string) {
	s := e.cur
	if s == nil || name == "" {
		return
	}
	at := &composer.Atom{Kind: composer.AtomEmoji, ID: name, Text: ":" + name + ":"}
	if a := v.h.Core(); a != nil {
		if u, t, ok := a.Emoji(name); ok {
			at.URL, at.Uni = u, t
		}
	}
	c := cursor(s.ed)
	s.ed.InsertAtom(c, c, at)
	s.focus = true
}
