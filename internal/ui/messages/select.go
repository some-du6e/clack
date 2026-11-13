package messages

import (
	"html"
	"image"
	"strings"
	"time"

	"gioui.org/f32"
	"gioui.org/io/key"
	"gioui.org/io/pointer"
	"gioui.org/layout"

	"github.com/some-du6e/clack/internal/ui/ctxclick"
	"github.com/some-du6e/clack/internal/ui/richview"
	"github.com/some-du6e/clack/internal/ui/slackc"
)

// Cross-message text selection: the list (an ancestor of every row) owns
// the selection; rows' richviews run in External mode and only paint the
// range the list gives them.

type selPoint struct {
	key string // item key (message ts or group key)
	off int32  // plain-text offset in that row's body
}

type selection struct {
	a, b     selPoint
	active   bool // a != b somewhere
	dragging bool
	focused  bool
	lastTap  time.Duration
	taps     int
	pressPos f32.Point
	keyIdx   map[string]int // item key -> index, rebuilt with items
}

func (s *listState) rebuildKeyIdx() {
	if s.sel.keyIdx == nil {
		s.sel.keyIdx = make(map[string]int, len(s.items))
	}
	for k := range s.sel.keyIdx {
		delete(s.sel.keyIdx, k)
	}
	for i, it := range s.items {
		if it.kind == itMsg {
			s.sel.keyIdx[it.key] = i
		}
	}
}

// rowAt returns the visible message item under y (clamped to the nearest
// one), its row and the point in the row body's coordinates.
func (s *listState) rowAt(p f32.Point) (int, *row, f32.Point) {
	best, bestD := -1, 1<<30
	y := int(p.Y)
	for i := s.visible[0]; i < s.visible[1] && i < len(s.items); i++ {
		if s.items[i].kind != itMsg || s.heights[i] == 0 {
			continue
		}
		top, bot := s.pos[i], s.pos[i]+s.heights[i]
		d := 0
		if y < top {
			d = top - y
		} else if y >= bot {
			d = y - bot + 1
		}
		if d < bestD {
			best, bestD = i, d
		}
	}
	if best < 0 {
		return -1, nil, f32.Point{}
	}
	r := s.rows[s.items[best].key]
	if r == nil {
		return -1, nil, f32.Point{}
	}
	local := p.Sub(f32.Pt(float32(r.bodyRect.Min.X), float32(s.pos[best]+r.bodyRect.Min.Y)))
	return best, r, local
}

// offsetIn maps a body-local point to an offset, treating points above or
// below the body as its start or end.
func offsetIn(r *row, local f32.Point) int32 {
	if local.Y < 0 {
		return 0
	}
	if int(local.Y) >= r.bodyRect.Dy() {
		return r.rv.PlainLen()
	}
	return r.rv.OffsetAt(local)
}

func (s *listState) selPointer(gtx layout.Context, h Host, e pointer.Event, size image.Point) {
	switch e.Kind {
	case pointer.Press:
		if !ctxclick.Primary(e) { // right-clicks keep the selection for its menu
			return
		}
		i, r, local := s.rowAt(e.Position)
		inBody := r != nil && local.X >= 0 && local.Y >= 0 && int(local.X) <= r.bodyRect.Dx()+4 && int(local.Y) < r.bodyRect.Dy()
		if !inBody || r.edit != nil {
			s.clearSelection()
			return
		}
		if r.rv.OnCopyButton(local) { // the code block's copy button (richview codecopy.go)
			return
		}
		if e.Time-s.sel.lastTap < 400*time.Millisecond {
			s.sel.taps++
		} else {
			s.sel.taps = 1
		}
		s.sel.lastTap = e.Time
		k := s.items[i].key
		off := r.rv.OffsetAt(local)
		switch {
		case s.sel.taps == 2:
			a, b := r.rv.WordAt(off)
			s.sel.a, s.sel.b = selPoint{k, a}, selPoint{k, b}
		case s.sel.taps >= 3:
			a, b := r.rv.LineAt(off)
			s.sel.a, s.sel.b = selPoint{k, a}, selPoint{k, b}
		case e.Modifiers.Contain(key.ModShift) && s.sel.active:
			s.sel.b = selPoint{k, off}
		default:
			s.sel.a, s.sel.b = selPoint{k, off}, selPoint{k, off}
		}
		s.sel.pressPos = e.Position
		s.sel.dragging = s.sel.taps == 1
		s.sel.active = s.sel.a != s.sel.b
		gtx.Execute(key.FocusCmd{Tag: s})
	case pointer.Drag:
		if !s.sel.dragging {
			return
		}
		if d := e.Position.Sub(s.sel.pressPos); d.X*d.X+d.Y*d.Y < 9 {
			return
		}
		if i, r, local := s.rowAt(e.Position); r != nil {
			s.sel.b = selPoint{s.items[i].key, offsetIn(r, local)}
			s.sel.active = s.sel.a != s.sel.b
		}
		// auto-scroll at the edges
		edge := dp(gtx, 24)
		if int(e.Position.Y) < edge {
			s.list.Position.BeforeEnd = true
			s.list.Position.Offset -= dp(gtx, 12)
			h.Invalidate()
		} else if int(e.Position.Y) > size.Y-edge {
			s.list.Position.Offset += dp(gtx, 12)
			h.Invalidate()
		}
	case pointer.Release, pointer.Cancel:
		s.sel.dragging = false
	}
}

func (s *listState) clearSelection() {
	s.sel.a, s.sel.b = selPoint{}, selPoint{}
	s.sel.active = false
}

// ordered returns the selection as (item, offset) pairs, start first.
func (s *listState) ordered() (i0 int, o0 int32, i1 int, o1 int32, ok bool) {
	if !s.sel.active {
		return
	}
	ia, okA := s.sel.keyIdx[s.sel.a.key]
	ib, okB := s.sel.keyIdx[s.sel.b.key]
	if !okA || !okB {
		return
	}
	i0, o0, i1, o1 = ia, s.sel.a.off, ib, s.sel.b.off
	if i1 < i0 || (i1 == i0 && o1 < o0) {
		i0, o0, i1, o1 = i1, o1, i0, o0
	}
	return i0, o0, i1, o1, true
}

// rowSelection is the range item i shows selected.
func (s *listState) rowSelection(i int, r *row) (int32, int32) {
	i0, o0, i1, o1, ok := s.ordered()
	if !ok || i < i0 || i > i1 {
		return 0, 0
	}
	a, b := int32(0), r.rv.PlainLen()
	if i == i0 {
		a = o0
	}
	if i == i1 {
		b = o1
	}
	return a, b
}

// selectedText is the copy text: one message's selection as is; several
// messages as "Name  3:04 PM" header lines followed by their text, like
// Slack's copy of a multi-message selection.
func (s *listState) selectedText(h Host) string {
	t, _, _ := s.selectedRich(h)
	return t
}

// selectedRich is the selection as Slack's copy puts it on the clipboard
// (richview.CopyRich): its text, and its slack/html and text/html, each
// message's body with its mentions' marks (richview.HTMLRange) and the
// header lines as text.
func (s *listState) selectedRich(h Host) (text, slackHTML, cleanHTML string) {
	i0, o0, i1, o1, ok := s.ordered()
	if !ok {
		return "", "", ""
	}
	if i0 == i1 {
		if r := s.rows[s.items[i0].key]; r != nil {
			sh, ch := r.rv.HTMLRange(o0, o1)
			return r.rv.TextRange(o0, o1), sh, ch
		}
		return "", "", ""
	}
	var b, sb, cb strings.Builder
	line := func(t string) {
		b.WriteString(t)
		e := html.EscapeString(t)
		sb.WriteString(e)
		cb.WriteString(e)
	}
	brk := func() {
		b.WriteByte('\n')
		sb.WriteString("<br>")
		cb.WriteString("<br>")
	}
	for i := i0; i <= i1; i++ {
		it := s.items[i]
		if it.kind != itMsg {
			continue
		}
		r := s.rows[it.key]
		if r == nil {
			continue
		}
		a, z := int32(0), r.rv.PlainLen()
		if i == i0 {
			a = o0
		}
		if i == i1 {
			z = o1
		}
		if b.Len() > 0 {
			brk()
		}
		if it.header && (i > i0 || a == 0) {
			line(r.senderName())
			if timeSelectable() {
				line("  " + clock(tsTime(r.m.TS)))
			}
			brk()
		}
		b.WriteString(r.rv.TextRange(a, z))
		sh, ch := r.rv.HTMLRange(a, z)
		sb.WriteString(strings.TrimPrefix(sh, `<meta charset="utf-8">`))
		cb.WriteString(ch)
	}
	if b.Len() == 0 {
		return "", "", ""
	}
	return b.String(), `<meta charset="utf-8">` + sb.String(), cb.String()
}

// tsLabelClass is the timestamp's text (Timestamp's
// span.c-timestamp__label inside a.c-timestamp).
var tsLabelClass = slackc.Class("c-timestamp__label").Tag("span")

// timeSelectable reports whether a header's time is copied: the copy is
// the selected text, and a rule that makes the timestamp's label
// unselectable (Slick IgnoreTime: .c-timestamp__label {user-select: none})
// leaves it out, as the browser's copy does.
func timeSelectable() bool {
	st := slackc.CurrentScope().Child(tsClass, 0).Child(tsLabelClass, 0).Style()
	return st == nil || st.UserSelect
}

func (s *listState) selKeys(gtx layout.Context, h Host) {
	for {
		ev, ok := gtx.Event(
			key.FocusFilter{Target: s},
			key.Filter{Focus: s, Name: "C", Required: key.ModShortcut},
			key.Filter{Focus: s, Name: key.NameEscape},
		)
		if !ok {
			return
		}
		switch e := ev.(type) {
		case key.FocusEvent:
			s.sel.focused = e.Focus
			if !e.Focus && !s.sel.dragging {
				s.clearSelection()
			}
		case key.Event:
			if e.State != key.Press {
				continue
			}
			switch e.Name {
			case "C":
				if t, sh, ch := s.selectedRich(h); t != "" {
					richview.CopyRich(gtx, t, sh, ch) // Slack's copy handler: text, html, slack/html
				}
			case key.NameEscape:
				s.clearSelection()
			}
		}
	}
}
