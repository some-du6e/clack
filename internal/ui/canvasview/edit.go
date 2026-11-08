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
	"strings"
	"time"

	"gioui.org/io/event"
	"gioui.org/io/key"
	"gioui.org/io/pointer"
	"gioui.org/layout"
	"gioui.org/op"
	"gioui.org/op/clip"
	"gioui.org/unit"
	"gioui.org/widget"

	"github.com/some-du6e/yoloslackgoclient/internal/canvas"
	"github.com/some-du6e/yoloslackgoclient/internal/core"
	"github.com/some-du6e/yoloslackgoclient/internal/richtext"
	"github.com/some-du6e/yoloslackgoclient/internal/ui/composer"
	"github.com/some-du6e/yoloslackgoclient/internal/ui/icons"
	"github.com/some-du6e/yoloslackgoclient/internal/ui/theme"
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

func (e *editState) toolbarShown() bool { return false } // the toolbar floats (layoutFloatingToolbar)

func (e *editState) layoutToolbar(gtx layout.Context, v *View, p image.Point, w int) int { return 0 }

// textArea makes a text block's area start editing on click (links,
// mentions and selection keep working: the click passes through).
func (e *editState) textArea(gtx layout.Context, v *View, b *blk, r image.Rectangle) {
	if e == nil {
		return
	}
	for {
		ev, ok := gtx.Event(pointer.Filter{Target: b, Kinds: pointer.Press | pointer.Release})
		if !ok {
			break
		}
		pe, ok := ev.(pointer.Event)
		if !ok || pe.Kind != pointer.Release || pe.Buttons != 0 && pe.Buttons != pointer.ButtonPrimary {
			continue
		}
		if b.rv != nil && (b.rv.Pointer() || b.rv.HasSelection()) {
			continue // a link or mention was clicked, or text selected
		}
		e.start(v, b)
	}
	defer clip.Rect(r).Push(gtx.Ops).Pop()
	defer pointer.PassOp{}.Push(gtx.Ops).Pop()
	pointer.CursorText.Add(gtx.Ops)
	event.Op(gtx.Ops, b)
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
	e.finish(v)
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
	d.Info.Title, d.Info.TitleSpans = title, nil
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
	e.finish(v)
	k := b.b.Kind
	if !k.IsText() && k != canvas.Code {
		return
	}
	s := &session{target: b, id: b.b.ID, kind: k, level: b.b.Level, checked: b.b.Checked, orig: canvas.Markdown(*b.b),
		ed: composer.NewEditor(), focus: true}
	s.ed.SetDoc(toDoc(b.b))
	s.ver = s.ed.Version()
	e.cur = s
	v.h.Invalidate()
}

// finish saves the current session, if any.
func (e *editState) finish(v *View) {
	if e == nil || e.cur == nil {
		return
	}
	s := e.cur
	e.cur = nil
	e.pick = picker{}
	bs := fromDoc(s.ed.Doc(), s)
	md := blocksMarkdown(bs)
	var ch core.CanvasChange
	switch {
	case s.target != nil && md == s.orig:
		return
	case s.target != nil && md == "":
		if s.id == "" {
			return
		}
		ch = core.CanvasChange{Operation: "delete", SectionID: s.id}
		e.replaceLocal(v, s.target, nil)
	case s.target != nil:
		if s.id == "" {
			return
		}
		ch = core.CanvasChange{Operation: "replace", SectionID: s.id, Markdown: md}
		if len(bs) > 0 {
			bs[0].ID = s.id
		}
		e.replaceLocal(v, s.target, bs)
	default: // a new block
		e.dropPlace(v, s)
		if md == "" {
			return
		}
		ch = core.CanvasChange{Operation: "insert_after", SectionID: s.afterID, Markdown: md}
		if s.afterID == "" {
			ch.Operation = "insert_at_end"
		}
		e.insertLocal(v, s.afterID, bs)
	}
	e.save(v, func(ctx context.Context, a *core.App) error { return a.EditCanvas(ctx, v.id, ch) })
}

// StartNewBlock begins typing a new paragraph after the document's last
// block (the empty area under a canvas, Slack's "Type anything…").
func (v *View) StartNewBlock() {
	e := v.edit
	if e == nil || !e.canEdit(v) {
		return
	}
	e.finish(v)
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
		if b == s.place {
			v.blocks = append(v.blocks[:i], v.blocks[i+1:]...)
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
	if len(nb) == 1 { // nested (a quote, a column, a cell): swap in place
		*b = *nb[0]
	}
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
// block (Slack: clicking under a canvas's content starts a new line).
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
			e.finish(v)
			return gtx.Dp(bodyLine)
		case composer.EvSubmit: // Ctrl/Cmd+Enter saves
			e.finish(v)
			return gtx.Dp(bodyLine)
		case composer.EvChange:
			shortcuts(s)
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
		tx = x + gtx.Dp(indentDp)
		v.drawMarker(gtx, probe, x, lineH)
	}
	st := editorStyle(th, kind, v.h)
	g := gtx
	g.Constraints = layout.Constraints{Max: image.Pt(max(1, w-tx), gtx.Constraints.Max.Y)}
	var d layout.Dimensions
	func() {
		defer op.Offset(image.Pt(tx, 0)).Push(gtx.Ops).Pop()
		d = s.ed.Layout(g, st)
	}()
	h := max(d.Size.Y, lineH)
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
	st.Color, st.Placeholder, st.Selection, st.Caret = c.Fg, c.FgSubtle, c.Selection, c.Fg
	st.LinkColor, st.CodeColor, st.CodeBg, st.CodeBorder = c.Link, c.CodeFg, c.CodeBg, c.CodeBorder
	st.PreBg, st.PreBorder, st.QuoteBar = c.PreBg, c.CodeBorder, c.QuoteBar
	st.MentionColor, st.MentionBg, st.BroadcastColor, st.BroadcastBg = c.Mention, c.MentionBg, c.Fg, c.SelfMention
	st.PlaceholderText = "Type something…"
	if k.IsList() {
		st.PlaceholderText = "List item"
	}
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

// ---- the formatting toolbar ----

type tool struct {
	key, icon, tip, label string
}

var tools = []tool{
	{"bold", "bold", "Bold", ""}, {"italic", "italic", "Italic", ""}, {"strike", "strike", "Strikethrough", ""},
	{"code", "code", "Code", ""}, {"link", "link", "Link", ""}, {"|", "", "", ""},
	{"h1", "", "Heading 1", "H1"}, {"h2", "", "Heading 2", "H2"}, {"h3", "", "Heading 3", "H3"}, {"para", "", "Text", "Aa"}, {"|", "", "", ""},
	{"bullet", "list-bullet", "Bulleted list", ""}, {"numbered", "list-ordered", "Numbered list", ""}, {"check", "todos", "Checklist", ""},
	{"quote", "quote", "Quote", ""}, {"codeblock", "code-block", "Code block", ""}, {"|", "", "", ""},
	{"mention", "at", "Mention someone", ""}, {"emoji", "emoji", "Emoji", ""},
}

// layoutFloatingToolbar docks the toolbar at the top of the document while
// a block is being edited.
func (e *editState) layoutFloatingToolbar(gtx layout.Context, v *View) {
	if e == nil || e.cur == nil {
		return
	}
	s := e.cur
	th := v.h.Theme()
	c := &th.C
	bs := gtx.Dp(28)
	gap := gtx.Dp(2)
	w := gtx.Dp(8)
	for _, t := range tools {
		if t.key == "|" {
			w += gtx.Dp(9)
		} else {
			w += bs + gap
		}
	}
	w += gtx.Dp(6)
	sz := gtx.Constraints.Max
	x := (sz.X - w) / 2
	r := image.Rect(x, gtx.Dp(8), x+w, gtx.Dp(8)+bs+gtx.Dp(8))
	theme.Shadow(gtx, r.Add(image.Pt(0, gtx.Dp(2))), gtx.Dp(8), gtx.Dp(8), theme.WithAlpha(c.PanelShadow, 0x30))
	theme.RRect(gtx, r, gtx.Dp(8), c.BgRaised)
	theme.Border(gtx, r, gtx.Dp(8), gtx.Dp(1), c.Border)
	// clicks between the buttons must not reach the background (which
	// saves); registered under the buttons so they still get theirs
	area := clip.Rect(r).Push(gtx.Ops)
	event.Op(gtx.Ops, &e.tool)
	area.Pop()
	tx := r.Min.X + gtx.Dp(6)
	active := s.ed.ActiveStyle()
	for _, t := range tools {
		if t.key == "|" {
			theme.Fill(gtx, image.Rect(tx+gtx.Dp(4), r.Min.Y+gtx.Dp(10), tx+gtx.Dp(5), r.Max.Y-gtx.Dp(10)), c.Border)
			tx += gtx.Dp(9)
			continue
		}
		t := t
		clk := e.btn(t.key)
		on := false
		switch t.key {
		case "bold":
			on = active&composer.Bold != 0
		case "italic":
			on = active&composer.Italic != 0
		case "strike":
			on = active&composer.Strike != 0
		case "code":
			on = active&composer.Code != 0
		case "h1":
			on = s.kind == canvas.Heading1
		case "h2":
			on = s.kind == canvas.Heading2
		case "h3":
			on = s.kind == canvas.Heading3
		case "para":
			on = s.kind == canvas.Paragraph
		case "bullet":
			on = s.kind == canvas.Bullet
		case "numbered":
			on = s.kind == canvas.Numbered
		case "check":
			on = s.kind == canvas.Check
		}
		at(gtx, image.Pt(tx, r.Min.Y+gtx.Dp(4)), image.Pt(bs, bs), func(gtx layout.Context) layout.Dimensions {
			return clk.Layout(gtx, func(gtx layout.Context) layout.Dimensions {
				s := gtx.Constraints.Max
				if on {
					theme.RRect(gtx, image.Rectangle{Max: s}, gtx.Dp(6), theme.WithAlpha(c.Accent, 0x26))
				} else if clk.Hovered() {
					theme.RRect(gtx, image.Rectangle{Max: s}, gtx.Dp(6), c.BgHover)
				}
				col := c.FgMuted
				if on {
					col = c.Fg
				}
				if t.icon != "" {
					p := gtx.Dp(18)
					at(gtx, image.Pt((s.X-p)/2, (s.Y-p)/2), image.Pt(p, p), func(gtx layout.Context) layout.Dimensions {
						return icons.Get(t.icon)(gtx, 18, col)
					})
				} else {
					lw := th.Measure(gtx, 13, theme.Black, t.label)
					at(gtx, image.Pt((s.X-lw)/2, 0), image.Pt(lw+2, s.Y), func(gtx layout.Context) layout.Dimensions {
						return vcenter(gtx, s.Y, func(gtx layout.Context) layout.Dimensions {
							return th.Label(gtx, 13, theme.Black, col, t.label)
						})
					})
				}
				return layout.Dimensions{Size: s}
			})
		})
		if clk.Clicked(gtx) {
			e.apply(gtx, v, t.key, image.Pt(tx, r.Max.Y))
		}
		tx += bs + gap
	}
}

// apply runs a toolbar button on the session.
func (e *editState) apply(gtx layout.Context, v *View, k string, anchor image.Point) {
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
		setKind(canvas.Heading1)
	case "h2":
		setKind(canvas.Heading2)
	case "h3":
		setKind(canvas.Heading3)
	case "para":
		setKind(canvas.Paragraph)
	case "bullet":
		setKind(canvas.Bullet)
	case "numbered":
		setKind(canvas.Numbered)
	case "check":
		setKind(canvas.Check)
	case "quote":
		ed.ToggleBlock(composer.Quote)
	case "codeblock":
		ed.ToggleBlock(composer.CodeBlock)
	case "mention":
		ed.InsertText("@")
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
