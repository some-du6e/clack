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
	"github.com/some-du6e/clack/internal/richtext"
	"github.com/some-du6e/clack/internal/ui/composer"
	"github.com/some-du6e/clack/internal/ui/icons"
	"github.com/some-du6e/clack/internal/ui/theme"
)

// editState is the view's editing state (UI goroutine only).
type editState struct {
	cur      *session
	renaming bool
	rename   widget.Editor
	renameOn bool // focus requested
	bg       int  // tag: clicks on the document's background end editing
	tool     map[string]*widget.Clickable
	pick     picker
	saving   int // edits in flight
	insBar   int // tag: the bottom toolbar's background
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
	orig    string // the markdown it started from
	ed      *composer.Editor
	focus   bool
	ver     uint64
	refused bool // the last save was refused: Escape now discards
	quoted  bool // a paragraph of a quote: each line saves as a "> " section
	press   *pressAt
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
	if e == nil || v.shown == nil {
		return false
	}
	return v.shown.Info.Editable
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
	defer clip.Rect(r).Push(gtx.Ops).Pop()
	defer pointer.PassOp{}.Push(gtx.Ops).Pop()
	if b.edOK {
		pointer.CursorText.Add(gtx.Ops)
	}
	event.Op(gtx.Ops, b)
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

func (e *editState) refuse(v *View, b *blk) {
	msg := cantEdit
	if b.edWhy != "" {
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
	defer clip.Rect(r).Push(gtx.Ops).Pop()
	defer pointer.PassOp{}.Push(gtx.Ops).Pop()
	pointer.CursorText.Add(gtx.Ops)
	event.Op(gtx.Ops, tag)
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
	if !e.finish(v) {
		return
	}
	if !b.edOK {
		e.refuse(v, b)
		return
	}
	s := &session{target: b, id: b.b.ID, kind: b.b.Kind, level: b.b.Level, checked: b.b.Checked, orig: canvas.Markdown(*b.b),
		ed: composer.NewEditor(), focus: true}
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
		if ok, why := canvas.Editable(b); !ok {
			if why == "" {
				why = "this"
			}
			v.h.Toast("Clack can't save " + why + " to a canvas yet. Remove it, or press Escape again to discard your changes")
			s.refused = true
			s.focus = true
			return false
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
	case s.target != nil && md == s.orig && !s.def:
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
		e.insertLocal(v, s.afterID, local)
	}
	e.save(v, func(ctx context.Context, a *core.App) error { return a.EditCanvas(ctx, v.id, ch) })
	return true
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
	place := &blk{b: &canvas.Block{Kind: canvas.Paragraph}}
	v.blocks = append(v.blocks, place)
	v.list.ScrollTo(len(v.blocks)) // the header row comes first: this is the new block's row
	s := &session{afterID: after, place: place, kind: canvas.Paragraph, ed: composer.NewEditor(), focus: true}
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
	nb := v.makeBlocks(v.shown, bs, nil)
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
	nb := v.makeBlocks(v.shown, bs, nil)
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
			if e.pick.active() {
				e.pick = picker{}
				continue
			}
			if s.refused { // discard: the block shows what is saved
				e.discard(v)
				return gtx.Dp(bodyLine)
			}
			if e.finish(v) {
				return gtx.Dp(bodyLine)
			}
		case composer.EvSubmit: // Ctrl/Cmd+Enter saves
			if e.finish(v) {
				return gtx.Dp(bodyLine)
			}
		case composer.EvChange:
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
	st.EmojiSize = 20
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
		sp := lineSpans(d, lo, hi)
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
				composer.Header2: canvas.Heading2, composer.Header3: canvas.Heading3}[ln.Kind], Spans: unbold(sp)}
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
		if b.Kind == canvas.Heading1 || b.Kind == canvas.Heading2 || b.Kind == canvas.Heading3 {
			b.Spans = unbold(b.Spans)
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
		return len(b.Children) == 0 || strings.TrimSpace(b.Children[0].Text()) == ""
	}
	return strings.TrimSpace(canvas.SpansText(b.Spans)) == ""
}

func unbold(sp []richtext.Span) []richtext.Span {
	out := make([]richtext.Span, len(sp))
	for i, s := range sp {
		s.Style &^= richtext.Bold
		out[i] = s
	}
	return out
}

// lineSpans converts runes [lo, hi) to spans.
func lineSpans(d *composer.Doc, lo, hi int) []richtext.Span {
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
	for len(out) > 0 {
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

// selectionMenu is the floating bar's block-style menu (zk's menuItems:
// paragraph, h1, h2, h3, ─, check-list, ordered-list, bullet-list, ─,
// code-block, then blockquote unless canvas_blockquote_layout is on,
// when the blockquote is a button of its own).
var selectionMenu = []blockFormat{fmtParagraph, fmtH1, fmtH2, fmtH3, fmtDivider, fmtCheck, fmtOrdered, fmtBullet, fmtDivider, fmtCodeBlock, fmtQuote}

// formattingMenu is the bottom toolbar's Aa menu
// (InsertionToolbarFormattingMenu, which opens upwards: blockquote, code
// block, ─, bulleted, ordered, check list, ─, h3, h2, h1, paragraph).
// Under canvas_blockquote_layout the menu drops the blockquote and the
// separator after code block; the blockquote moves to the insert menu.
var formattingMenu = []blockFormat{fmtQuote, fmtCodeBlock, fmtDivider, fmtBullet, fmtOrdered, fmtCheck, fmtDivider, fmtH3, fmtH2, fmtH1, fmtParagraph}

// selectionMenuFor and formattingMenuFor are the two menus in the
// viewer's canvas_blockquote_layout group (bq: on).
func selectionMenuFor(bq bool) []blockFormat {
	if !bq {
		return selectionMenu
	}
	return selectionMenu[:len(selectionMenu)-1]
}

func formattingMenuFor(bq bool) []blockFormat {
	if !bq {
		return formattingMenu
	}
	return append([]blockFormat{fmtCodeBlock}, formattingMenu[3:]...)
}

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
	for _, f := range selectionMenu {
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
// disabled state (.c-menu_item__button--disabled, opacity .5). Under
// canvas_blockquote_layout the menu has Blockquote after Callout
// (BLOCKQUOTE_LAYOUT, hidden otherwise): a new quote after the insert
// point (insertSection("blockquote-layout")).
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
	}
	if v.exp("canvas_blockquote_layout") {
		items = append(items, MenuItem{Label: nsCanvas.T("Blockquote"), Icon: "quote", Do: func() { v.post(func() { e.insertQuote(v) }) }})
	}
	items = append(items,
		MenuItem{Label: nsCanvas.T("Canvas"), Icon: "canvas", Disabled: true},
		MenuItem{Label: nsCanvas.T("Image"), Icon: "image", Do: func() { v.post(func() { e.insertUpload(v, true) }) }},
		MenuItem{Label: nsCanvas.T("File"), Icon: "attach", Do: func() { v.post(func() { e.insertUpload(v, false) }) }},
	)
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
		e.openFormatMenu(v, formattingMenuFor(v.exp("canvas_blockquote_layout")))
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
	{{key: "code", icon: "code"}},
	{{key: "link", icon: "link"}, {key: "callout", icon: "callout"}},
}

// selGroupsFor is the bar in the viewer's canvas_blockquote_layout group:
// on, zk adds [blockquote-layout] between the text styles and [code]
// (the "quote" icon, getDerivedFormatSvgIcon's BLOCKQUOTE_LAYOUT).
func selGroupsFor(bq bool) [][]selButton {
	if !bq {
		return selGroups
	}
	out := append([][]selButton{}, selGroups[:2]...)
	out = append(out, []selButton{{key: "bqlayout", icon: "quote"}})
	return append(out, selGroups[2:]...)
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
	bq := v.exp("canvas_blockquote_layout")
	groups := selGroupsFor(bq)
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
						e.openFormatMenu(v, selectionMenuFor(bq))
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
