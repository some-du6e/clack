package messages

import (
	"image"
	"time"

	"gioui.org/io/event"
	"gioui.org/io/pointer"
	"gioui.org/layout"
	"gioui.org/op"
	"gioui.org/op/clip"

	"github.com/some-du6e/clack/internal/ui/theme"
)

// Slack's overlay scrollbar: a thin rounded thumb over the right edge that
// shows while the pointer is over the list or for a moment after scrolling,
// and can be dragged.

type scrollbar struct {
	known      map[string]int // item key -> last measured height
	knownFG    uint32         // theme.FontGen known was measured at
	lastPos    layout.Position
	lastScroll time.Time
	dragging   bool
	dragY      float32
	hover      bool
	thumb      image.Rectangle
}

func (s *listState) layoutScrollbar(gtx layout.Context, c *colors, size image.Point, now time.Time) {
	sb := &s.sb
	if sb.known == nil || sb.knownFG != theme.FontGen() {
		// a new UI font: every row's height is new (the ones on screen
		// were just measured with it)
		sb.known, sb.knownFG = map[string]int{}, theme.FontGen()
	}
	total, before, n := 0, 0, 0
	p := s.list.Position
	for i, it := range s.items {
		if i < len(s.heights) && s.heights[i] > 0 {
			sb.known[it.key] = s.heights[i]
		}
	}
	for i, it := range s.items {
		hgt, ok := sb.known[it.key]
		if !ok {
			continue
		}
		total += hgt
		n++
		if i < p.First {
			before += hgt
		}
	}
	if n == 0 {
		return
	}
	avg := total / n
	missing := len(s.items) - n
	total += missing * avg
	for i := 0; i < p.First && i < len(s.items); i++ {
		if _, ok := sb.known[s.items[i].key]; !ok {
			before += avg
		}
	}
	if total <= size.Y {
		return
	}
	top := before + p.Offset
	if !p.BeforeEnd {
		top = total - size.Y
	}
	if p != sb.lastPos {
		sb.lastPos = p
		sb.lastScroll = now
	}

	// events
	for {
		ev, ok := gtx.Event(pointer.Filter{Target: sb, Kinds: pointer.Press | pointer.Drag | pointer.Release | pointer.Enter | pointer.Leave | pointer.Cancel})
		if !ok {
			break
		}
		e, ok := ev.(pointer.Event)
		if !ok {
			continue
		}
		switch e.Kind {
		case pointer.Enter:
			sb.hover = true
		case pointer.Leave:
			sb.hover = false
		case pointer.Press:
			sb.dragging, sb.dragY = true, e.Position.Y
		case pointer.Drag:
			if sb.dragging {
				track := size.Y - sb.thumb.Dy()
				if track > 0 {
					dy := e.Position.Y - sb.dragY
					px := int(dy * float32(total-size.Y) / float32(track))
					s.list.Position.BeforeEnd = true
					s.list.Position.Offset += px
					sb.dragY = e.Position.Y
					gtx.Execute(op.InvalidateCmd{})
				}
			}
		case pointer.Release, pointer.Cancel:
			sb.dragging = false
		}
	}

	recent := now.Sub(sb.lastScroll) < 1200*time.Millisecond
	if !(s.pointerIn || recent || sb.dragging) {
		sb.thumb = image.Rectangle{}
		return
	}
	if recent {
		gtx.Execute(op.InvalidateCmd{At: sb.lastScroll.Add(1250 * time.Millisecond)})
	}
	th := max(dp(gtx, 32), size.Y*size.Y/total)
	ty := 0
	if total > size.Y {
		ty = (size.Y - th) * min(max(top, 0), total-size.Y) / (total - size.Y)
	}
	w := dp(gtx, 8)
	if sb.hover || sb.dragging {
		w = dp(gtx, 10)
	}
	x := size.X - w - dp(gtx, 2)
	sb.thumb = image.Rect(x, ty, x+w, ty+th)
	col := c.ScrollThumb
	if sb.hover || sb.dragging {
		col = c.ScrollThumbHover
	}
	rrect(gtx.Ops, sb.thumb, w/2, col)
	// hit area a bit wider than the thumb
	hit := image.Rect(size.X-dp(gtx, 14), ty, size.X, ty+th)
	st := clip.Rect(hit).Push(gtx.Ops)
	pointer.CursorDefault.Add(gtx.Ops)
	event.Op(gtx.Ops, sb)
	st.Pop()
}
