package canvasview

// The canvas's table of contents (canvas_table_of_contents): Slack's
// TableOfContentsDrawer (client-boot-shared tshn), which QuipEmbed (foOi)
// mounts in the canvas's embed with Quip's outline on (feature 188,
// CANVAS_ALLOW_SLACK_OUTLINE → allowSlackOutline).
//
//   - Entries (quip-collab-editor computeTableOfContentsInfo): the title
//     section and every heading with text, in document order. The title
//     is level 0; the headings' levels are renumbered from 0 in order of
//     the levels present (normalizeHeadingLevels), so a canvas of H2s and
//     H3s indents like one of H1s and H2s.
//   - Collapsed (TableOfContentsCollapsed): at most 24 lines, 2px tall,
//     10px apart, 20/14/8px wide by indent, in --dt_color-otl-sec, the
//     current section's in --dt_color-otl-hgl-1, 16px under the drawer's
//     top, the drawer 12px from the embed's left. It follows the first
//     entry down the page and sticks 58px from the top (0 when the embed
//     is under 546px tall). Fewer than two entries draw nothing.
//   - Hovering the lines expands it after 250ms into a 244px panel
//     (12px padding, a 1px otl-ter border, radius 12, the two-layer
//     shadow) of 28px entries in 13px text, indented 16px per level, the
//     title bold, the current one bold in content-hgl-1; it scrolls when
//     longer than the room under it. Leaving collapses it (not within
//     150ms of expanding). Clicking an entry scrolls its section into
//     view (focusSection) and keeps it current until the page scrolls on.
//   - The current section (computeActiveSectionId): the entry at the top
//     of the view, or the one past the top in its upper half.
//   - Quip reserves a 64px gutter for it (setExternalGutterReservedWidth:
//     the left margin is max(padding, 64+32), canvasDocumentMargins),
//     whether or not there is anything to list.

import (
	"image"
	"image/color"
	"strings"
	"time"

	"gioui.org/io/event"
	"gioui.org/io/pointer"
	"gioui.org/layout"
	"gioui.org/op"
	"gioui.org/op/clip"
	"gioui.org/unit"
	"gioui.org/widget"

	"github.com/some-du6e/clack/internal/canvas"
	"github.com/some-du6e/clack/internal/richtext"
	"github.com/some-du6e/clack/internal/ui/theme"
)

// tocEntry is one entry of the table of contents.
type tocEntry struct {
	row    int    // its block's row in layoutDoc's list (0: the title, in the header row)
	id     string // the section id
	indent int    // normalizeHeadingLevels
	title  bool
	level  int // the heading's level, 1-3 (0: the title)
	text   string
}

// tocState is the drawer's state (UI goroutine only).
type tocState struct {
	built   []*blk
	entries []tocEntry

	// the rows layoutDoc's list laid out this frame: their tops in the
	// view (after the list's layout) and the gap above each block
	rowTop, rowGap map[int]int
	rowH           map[int]int
	first, last    int // the visible rows

	tag       int // pointer target: the drawer (hover)
	panelTag  int // the open panel's background
	inside    bool
	hoverAt   time.Time // the pointer reached the lines: expand at +250ms
	expanded  bool
	openedAt  time.Time // the 150ms in which leaving doesn't collapse it yet
	leftEarly bool
	latchTop  int // the top the expanded panel opened at
	list      layout.List
	clicks    map[string]*widget.Clickable

	jumped   string // the section clicked: current until the page scrolls on
	jumpedAt time.Time
	jumpPos  *layout.Position
}

func newTOC() *tocState {
	t := &tocState{rowTop: map[int]int{}, rowGap: map[int]int{}, rowH: map[int]int{}, clicks: map[string]*widget.Clickable{}}
	t.list.Axis = layout.Vertical
	return t
}

// tocOn: the drawer is mounted (canvas_table_of_contents on).
func (v *View) tocOn() bool {
	// QuipEmbed renders the TableOfContentsDrawer only outside a preview
	// (tZ && !l && !isPreview)
	return v.mode != Peek && v.exp("canvas_table_of_contents")
}

// tocEntries builds the entries from the blocks (rebuilt when they change).
func (v *View) tocEntries() []tocEntry {
	t := v.toc
	if sameBlks(t.built, v.blocks) && t.entries != nil {
		return t.entries
	}
	t.built = append(t.built[:0], v.blocks...)
	t.entries = buildTOC(v.blocks)
	return t.entries
}

func buildTOC(blocks []*blk) []tocEntry {
	out := []tocEntry{}
	rows := blocks
	if titleSection(blocks) {
		if s := tocText(blocks[0].b); s != "" {
			out = append(out, tocEntry{row: 0, id: blocks[0].b.ID, title: true, text: s})
		}
		rows = blocks[1:]
	}
	var walk func(row int, b *blk)
	walk = func(row int, b *blk) {
		if b == nil || b.b == nil {
			return
		}
		lvl := 0
		switch b.b.Kind {
		case canvas.Heading1:
			lvl = 1
		case canvas.Heading2:
			lvl = 2
		case canvas.Heading3:
			lvl = 3
		}
		if lvl > 0 && b.b.ID != "" {
			if s := tocText(b.b); s != "" {
				out = append(out, tocEntry{row: row, id: b.b.ID, level: lvl, text: s})
			}
		}
		for _, k := range b.kids {
			walk(row, k)
		}
		for _, c := range b.cols {
			for _, k := range c {
				walk(row, k)
			}
		}
	}
	for i, b := range rows {
		walk(i+1, b)
	}
	// normalizeHeadingLevels: the levels present, in order, become 0, 1, 2…
	present := map[int]bool{}
	for _, e := range out {
		if !e.title {
			present[e.level] = true
		}
	}
	rank := map[int]int{}
	n := 0
	for l := 1; l <= 3; l++ {
		if present[l] {
			rank[l] = n
			n++
		}
	}
	for i := range out {
		if !out[i].title {
			out[i].indent = rank[out[i].level]
		}
	}
	return out
}

// tocText is a section's text as the entry shows it: the heading's plain
// text with its emoji, leading space dropped (TableOfContentsEntry skips
// the leading whitespace-only text elements).
func tocText(b *canvas.Block) string {
	var sb strings.Builder
	for _, s := range b.Spans {
		switch s.Kind {
		case richtext.Emoji:
			if s.Text != "" {
				sb.WriteString(s.Text)
			} else {
				sb.WriteString(":" + s.ID + ":")
			}
		case richtext.Break:
			sb.WriteString(" ")
		default:
			sb.WriteString(s.Text)
		}
	}
	return strings.TrimSpace(sb.String())
}

// rowLaidOut records a row layoutDoc's list drew this frame.
func (t *tocState) rowLaidOut(i, gap, h int) {
	t.rowGap[i], t.rowH[i] = gap, h
}

// listDone turns the frame's rows into their tops in the view.
func (t *tocState) listDone(pos layout.Position) {
	t.first, t.last = pos.First, pos.First+max(0, pos.Count-1)
	for k := range t.rowTop {
		delete(t.rowTop, k)
	}
	y := -pos.Offset
	for i := pos.First; i < pos.First+pos.Count; i++ {
		h, ok := t.rowH[i]
		if !ok {
			break
		}
		t.rowTop[i] = y
		y += h
	}
	for k := range t.rowH {
		delete(t.rowH, k)
	}
}

// entryTop is an entry's top in the view (effectiveTop less the scroll):
// above the view or below it when its row isn't on screen.
func (t *tocState) entryTop(gtx layout.Context, e tocEntry) (y int, known bool) {
	if top, ok := t.rowTop[e.row]; ok {
		if e.row == 0 {
			return top + gtx.Dp(docTop), true
		}
		return top + t.rowGap[e.row], true
	}
	if e.row < t.first {
		return -1 << 30, false
	}
	return 1 << 30, false
}

// active is computeActiveSectionId over the view: the last entry above
// its top, unless one starts in the top half.
func (t *tocState) active(gtx layout.Context, entries []tocEntry, h int) string {
	if len(entries) == 0 {
		return ""
	}
	mid := h / 2
	cand := ""
	for _, e := range entries {
		y, _ := t.entryTop(gtx, e)
		if y > 0 {
			if y > mid && cand != "" {
				return cand
			}
			return e.id
		}
		cand = e.id
	}
	if cand == "" {
		return entries[0].id
	}
	return cand
}

// tocColors are the drawer's tokens (client-boot-styles' TOC rules).
type tocColors struct {
	line, lineHi, border, bg, hover, text, textHi color.NRGBA
}

func tocPalette(th *theme.Theme) tocColors {
	rgba := func(v uint32) color.NRGBA {
		return color.NRGBA{R: uint8(v >> 24), G: uint8(v >> 16), B: uint8(v >> 8), A: uint8(v)}
	}
	// the default light and dark tokens (slackc/css slackvars)
	c := tocColors{line: rgba(0x5e5d6073), lineHi: rgba(0x1264a3ff), border: rgba(0x5e5d6021), bg: rgba(0xffffffff),
		hover: rgba(0x1d1c1d0f), text: rgba(0x1d1c1dff), textHi: rgba(0x1264a3ff)}
	if th.Dark {
		c = tocColors{line: rgba(0x797c8180), lineHi: rgba(0x2ba5ceff), border: rgba(0x797c814d), bg: rgba(0x1a1d21ff),
			hover: rgba(0xf8f8f80f), text: rgba(0xf8f8f8ff), textHi: rgba(0x2ba5ceff)}
	}
	if th.Tokens != nil { // a theme's own tokens
		theme.SetColor(th.Tokens, &c.line, "var(--dt_color-otl-sec)")
		theme.SetColor(th.Tokens, &c.lineHi, "var(--dt_color-otl-hgl-1)")
		theme.SetColor(th.Tokens, &c.border, "var(--dt_color-otl-ter)")
		theme.SetColor(th.Tokens, &c.bg, "var(--dt_color-base-pry)")
		theme.SetColor(th.Tokens, &c.hover, "var(--dt_color-surf-pry)")
		theme.SetColor(th.Tokens, &c.text, "var(--dt_color-content-pry)")
		theme.SetColor(th.Tokens, &c.textHi, "var(--dt_color-content-hgl-1)")
	}
	return c
}

// lineWidth is the collapsed line's width by indent (large, medium, small).
func lineWidth(indent int) int {
	switch indent {
	case 0:
		return 20
	case 1:
		return 14
	}
	return 8
}

// layoutTOC draws the drawer over the document (gtx is the canvas body).
func (v *View) layoutTOC(gtx layout.Context) {
	if v.toc == nil || !v.tocOn() {
		return
	}
	t := v.toc
	entries := v.tocEntries()
	if len(entries) < 2 {
		t.expanded, t.inside = false, false
		return
	}
	th := v.h.Theme()
	col := tocPalette(th)
	sz := gtx.Constraints.Max
	overlap := sz.Y < gtx.Dp(546) // allowOverlapHeader

	// the current section
	cur := t.active(gtx, entries, sz.Y)
	if t.jumped != "" {
		switch {
		case t.jumpPos == nil && gtx.Now.Sub(t.jumpedAt) >= 500*time.Millisecond:
			p := v.list.Position
			t.jumpPos = &p
			cur = t.jumped
		case t.jumpPos == nil:
			cur = t.jumped
			gtx.Execute(op.InvalidateCmd{At: t.jumpedAt.Add(500 * time.Millisecond)})
		case t.jumpPos.First == v.list.Position.First && t.jumpPos.Offset == v.list.Position.Offset:
			cur = t.jumped
		default:
			t.jumped, t.jumpPos = "", nil
		}
	}

	// where it sits: with the first entry, at least 58px down
	y0, known := t.entryTop(gtx, entries[0])
	if !known && y0 > 0 {
		return // the first entry is further down the page
	}
	minTop := gtx.Dp(58)
	if overlap {
		minTop = 0
	}
	top := max(y0, minTop)
	if t.expanded {
		top = t.latchTop
		if sz.Y > 0 {
			top = min(top, max(0, sz.Y-gtx.Dp(60)-gtx.Dp(136)))
		}
	}
	room := sz.Y - top - gtx.Dp(60) // --_toc-available-height
	if overlap && !t.expanded && room < gtx.Dp(30) {
		return
	}

	// hover: the lines (plus the 6px trigger padding), and the panel
	n := min(len(entries), 24)
	svgH := 2*n + 8*(n-1)
	x0 := gtx.Dp(12)
	lines := image.Rect(x0-gtx.Dp(6), top, x0+gtx.Dp(24)+gtx.Dp(6), top+gtx.Dp(16)+gtx.Dp(unit.Dp(svgH))+gtx.Dp(6))
	panel := image.Rect(x0, top, x0+gtx.Dp(244), top)
	if t.expanded {
		panel.Max.Y = top + min(t.panelHeight(gtx, len(entries)), max(0, room))
	}
	v.tocPointer(gtx, lines, panel)

	// the collapsed lines
	for i := 0; i < n; i++ {
		e := entries[i]
		hi := e.id == cur
		if cur == "" && i == 0 {
			hi = true
		}
		c := col.line
		if hi {
			c = col.lineHi
		}
		ly := top + gtx.Dp(16) + gtx.Dp(10)*i
		r := image.Rect(x0, ly, x0+gtx.Dp(unit.Dp(lineWidth(e.indent))), ly+gtx.Dp(2))
		theme.RRect(gtx, r, gtx.Dp(1), c)
	}
	if t.expanded {
		v.layoutTOCPanel(gtx, entries, cur, panel, col)
	}
}

// panelHeight is the expanded panel's natural height: 12px padding
// around the 28px entries and a 1px border.
func (t *tocState) panelHeight(gtx layout.Context, n int) int {
	return 2*gtx.Dp(1) + 2*gtx.Dp(12) + n*gtx.Dp(28)
}

// tocPointer follows the pointer over the drawer: Slack expands it 250ms
// after the pointer reaches the lines and collapses it when the pointer
// leaves (after the 150ms that follow expanding).
func (v *View) tocPointer(gtx layout.Context, lines, panel image.Rectangle) {
	t := v.toc
	area := lines
	if t.expanded {
		area = area.Union(panel)
	}
	for {
		ev, ok := gtx.Event(pointer.Filter{Target: &t.tag, Kinds: pointer.Enter | pointer.Leave | pointer.Move})
		if !ok {
			break
		}
		pe, ok := ev.(pointer.Event)
		if !ok {
			continue
		}
		switch pe.Kind {
		case pointer.Enter, pointer.Move:
			if !t.inside {
				t.inside = true
				if !t.expanded {
					t.hoverAt = gtx.Now
				}
				t.leftEarly = false
			}
		case pointer.Leave:
			t.inside = false
			t.hoverAt = time.Time{}
			if t.expanded {
				if gtx.Now.Sub(t.openedAt) < 150*time.Millisecond {
					t.leftEarly = true
				} else {
					t.expanded = false
				}
			}
		}
	}
	switch {
	case !t.expanded && t.inside && !t.hoverAt.IsZero():
		if at := t.hoverAt.Add(250 * time.Millisecond); !gtx.Now.Before(at) {
			v.expandTOC(gtx, lines.Min.Y)
		} else {
			gtx.Execute(op.InvalidateCmd{At: at})
		}
	case t.expanded && t.leftEarly:
		if at := t.openedAt.Add(150 * time.Millisecond); !gtx.Now.Before(at) {
			t.expanded, t.leftEarly = false, false
		} else {
			gtx.Execute(op.InvalidateCmd{At: at})
		}
	}
	// hover only: presses and the wheel still reach the document
	ca := clip.Rect(area).Push(gtx.Ops)
	pass := pointer.PassOp{}.Push(gtx.Ops)
	event.Op(gtx.Ops, &t.tag)
	pass.Pop()
	ca.Pop()
	if t.expanded { // the open panel takes its clicks and its wheel
		pa := clip.Rect(panel).Push(gtx.Ops)
		event.Op(gtx.Ops, &t.panelTag)
		pa.Pop()
	}
}

func (v *View) expandTOC(gtx layout.Context, top int) {
	t := v.toc
	t.expanded, t.openedAt, t.hoverAt = true, gtx.Now, time.Time{}
	t.latchTop = top
	t.list.Position = layout.Position{}
	v.h.Invalidate()
}

// layoutTOCPanel draws the expanded entries in panel.
func (v *View) layoutTOCPanel(gtx layout.Context, entries []tocEntry, cur string, panel image.Rectangle, col tocColors) {
	t := v.toc
	th := v.h.Theme()
	r := panel
	// box-shadow: 0 4px 6px -4px #00000019, 0 10px 15px -3px #00000019
	theme.Shadow(gtx, r.Add(image.Pt(0, gtx.Dp(10))).Inset(gtx.Dp(3)), gtx.Dp(12), gtx.Dp(15), color.NRGBA{A: 0x19})
	theme.Shadow(gtx, r.Add(image.Pt(0, gtx.Dp(4))).Inset(gtx.Dp(4)), gtx.Dp(12), gtx.Dp(6), color.NRGBA{A: 0x19})
	theme.RRect(gtx, r, gtx.Dp(12), col.bg)
	theme.Border(gtx, r, gtx.Dp(12), gtx.Dp(1), col.border)
	pad := gtx.Dp(12)
	inner := image.Rect(r.Min.X+gtx.Dp(1)+pad, r.Min.Y+gtx.Dp(1)+pad, r.Max.X-gtx.Dp(1)-pad-gtx.Dp(4), r.Max.Y-gtx.Dp(1)-pad)
	if inner.Dx() <= 0 || inner.Dy() <= 0 {
		return
	}
	defer op.Offset(inner.Min).Push(gtx.Ops).Pop()
	g := gtx
	g.Constraints = layout.Exact(inner.Size())
	defer clip.Rect{Max: inner.Size()}.Push(gtx.Ops).Pop()
	t.list.Layout(g, len(entries), func(gtx layout.Context, i int) layout.Dimensions {
		e := entries[i]
		h := gtx.Dp(28)
		w := gtx.Constraints.Max.X
		clk := t.clicks[e.id]
		if clk == nil {
			clk = new(widget.Clickable)
			t.clicks[e.id] = clk
		}
		if clk.Clicked(gtx) { // before Layout, which drains a Clickable's events
			v.tocJump(gtx, e)
		}
		ind := gtx.Dp(16) * e.indent
		box := image.Rect(ind, 0, w, h)
		func() {
			defer op.Offset(box.Min).Push(gtx.Ops).Pop()
			bg := gtx
			bg.Constraints = layout.Exact(box.Size())
			clk.Layout(bg, func(gtx layout.Context) layout.Dimensions {
				s := gtx.Constraints.Max
				if clk.Hovered() {
					theme.RRect(gtx, image.Rectangle{Max: s}, gtx.Dp(4), col.hover)
				}
				c, wt := col.text, theme.Regular
				if e.title {
					wt = theme.Bold
				}
				if e.id == cur {
					c, wt = col.textHi, theme.Bold
				}
				at(gtx, image.Pt(gtx.Dp(8), 0), image.Pt(max(0, s.X-gtx.Dp(16)), s.Y), func(gtx layout.Context) layout.Dimensions {
					return vcenter(gtx, s.Y, func(gtx layout.Context) layout.Dimensions {
						return th.Text(gtx, theme.LabelOpts{Font: theme.Font(wt), Size: 13, Color: c, MaxLines: 1}, e.text)
					})
				})
				return layout.Dimensions{Size: s}
			})
		}()
		return layout.Dimensions{Size: image.Pt(w, h)}
	})
}

// tocJump scrolls an entry's section into view (focusSection) and keeps
// it current until the page moves on.
func (v *View) tocJump(gtx layout.Context, e tocEntry) {
	t := v.toc
	v.list.Position = layout.Position{First: e.row, BeforeEnd: true}
	t.jumped, t.jumpedAt, t.jumpPos = e.id, gtx.Now, nil
	v.h.Invalidate()
}
