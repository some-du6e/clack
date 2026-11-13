package composer

// The pointer over a mention pill: Texty's onMouseOver / onMouseOut
// (boot-shared TextyInput, build 32bb3cc8205b) show a tooltip for the
// .c-member_slug under the pointer (showTooltip: none while text is
// selected; its target bounds the slug's client rect under the pointer)
// and hide it when the pointer leaves the slug. The editor tracks the
// pointer; HoverAtom is the pill under it.

import (
	"image"

	"gioui.org/io/event"
	"gioui.org/io/pointer"
	"gioui.org/layout"
)

// edHover is the pointer over the editor, viewport px.
type edHover struct {
	in bool
	pt image.Point
}

// hoverEvents reads the editor's pointer moves (Layout registers the tag).
func (e *Editor) hoverEvents(gtx layout.Context) {
	for {
		ev, ok := gtx.Event(pointer.Filter{Target: &e.hover, Kinds: pointer.Move | pointer.Enter | pointer.Leave | pointer.Cancel})
		if !ok {
			return
		}
		pe, ok := ev.(pointer.Event)
		if !ok {
			continue
		}
		switch pe.Kind {
		case pointer.Leave, pointer.Cancel:
			e.hover.in = false
		default:
			e.hover.in, e.hover.pt = true, pe.Position.Round()
		}
	}
}

// HoverPoint is the pointer over the editor in viewport px (HoverAtom's
// coordinates); false when it's elsewhere.
func (e *Editor) HoverPoint() (image.Point, bool) { return e.hover.pt, e.hover.in }

// addHover registers the pointer tracker over the editor's clip area.
func (e *Editor) addHover(gtx layout.Context) { event.Op(gtx.Ops, &e.hover) }

// HoverAtom is the atom under the pointer (a mention pill: a user,
// channel, user group or broadcast; or an emoji, Texty's img.emoji) and
// its box in the editor's viewport px, as Layout last drew it. None while
// text is selected (showTooltip returns when the selection has a length).
func (e *Editor) HoverAtom() (*Atom, image.Rectangle, bool) {
	if !e.hover.in || !e.ensureLayout() {
		return nil, image.Rectangle{}, false
	}
	if lo, hi := e.sel(); lo != hi {
		return nil, image.Rectangle{}, false
	}
	pt := e.hover.pt.Add(image.Pt(0, e.scrollY))
	pos := e.doc.clamp(e.lay.hit(pt))
	for _, i := range []int{pos - 1, pos} {
		if i < 0 || i >= e.doc.Len() {
			continue
		}
		a := e.doc.Attrs[i].Atom
		if a == nil {
			continue
		}
		if r, ok := e.lay.atomBox(i, pt); ok {
			return a, r.Sub(image.Pt(0, e.scrollY)), true
		}
	}
	return nil, image.Rectangle{}, false
}

// atomBox is the pill box of the atom at rune pos on the visual line
// under pt, as paintStatic fills it (the line's ascent and descent plus
// the border width, within the line box).
func (l *tlLayout) atomBox(pos int, pt image.Point) (image.Rectangle, bool) {
	m := &l.m
	for pi := range l.paras {
		p := &l.paras[pi]
		if pos < p.start || pos >= p.end {
			continue
		}
		for ii := range p.items {
			it := &p.items[ii]
			if it.r0 != pos || it.attr.Atom == nil {
				continue
			}
			for li := p.l0; li < p.l1; li++ {
				a, b, ok := l.segment(li, it)
				if !ok {
					continue
				}
				ln := &l.lines[li]
				r := image.Rect(l.vx(li, a), max(ln.top, ln.base-p.asc-m.border), l.vx(li, b), min(ln.top+ln.h, ln.base+p.desc+m.border))
				if pt.In(r) {
					return r, true
				}
			}
		}
	}
	return image.Rectangle{}, false
}
