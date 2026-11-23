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
//
// Its geometry is Slack's legacy Scrollbar's (ySQw, the virtual list's
// through okQl; the ScrollArea variant is the scroll_area session toggle,
// off unless a session is assigned it): an 8px track 4px in from the
// list's top, bottom and right edges (.c-scrollbar__track{width:8px;
// top:4px;bottom:4px;right:4px}) with the bar filling its width; the bar
// is view²/content tall, at least 50px, at scrollTop/(content-view) of
// the track's free length (update()); a drag moves the content
// content/track px per px (onDragStart's speed). The content is every row
// (BaseList getContentHeight), a thread's reply box among them: the
// thread pane is one scroll container with the box inside it.

type scrollbar struct {
	known      map[string]int // item key -> last measured height
	knownFG    uint32         // theme.FontGen known was measured at
	lastPos    layout.Position
	lastScroll time.Time
	dragging   bool
	dragY      float32
	hover      bool
	thumb      image.Rectangle
	// over: the pointer is over the list's box, the host's reply box in
	// it too (.c-scrollbar:hover shows the bar over the whole scroll
	// container): ThreadView.DrawScrollbar tracks it over the box
	over bool
}

// sbInset and sbWidth are the track's inset from the list's edges and
// its width (.c-scrollbar__track); sbMinBar is update()'s least bar.
const sbInset, sbWidth, sbMinBar = 4, 8, 50

func (s *listState) layoutScrollbar(gtx layout.Context, c *colors, size image.Point, now time.Time) {
	sb := &s.sb
	for {
		// Enter and Leave are enough: Gio hit-tests a still pointer again
		// after every frame
		ev, ok := gtx.Event(pointer.Filter{Target: &sb.over, Kinds: pointer.Enter | pointer.Leave | pointer.Cancel})
		if !ok {
			break
		}
		if e, ok := ev.(pointer.Event); ok {
			sb.over = e.Kind != pointer.Leave && e.Kind != pointer.Cancel
		}
	}
	if !(s.thread && s.inputOn) {
		sb.over = false // no host box: DrawScrollbar tracks nothing
	}
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
		if it.kind == itInput {
			hgt, ok = s.inputH, true // the reply box's row: the host's box, laid out or not
		}
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
				track := size.Y - 2*dp(gtx, sbInset)
				if track > 0 {
					dy := e.Position.Y - sb.dragY
					px := int(dy * float32(total) / float32(track)) // onDragStart's speed
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
	if !(s.pointerIn || sb.over || sb.hover || recent || sb.dragging) {
		sb.thumb = image.Rectangle{}
		return
	}
	if recent {
		gtx.Execute(op.InvalidateCmd{At: sb.lastScroll.Add(1250 * time.Millisecond)})
	}
	inset, w := dp(gtx, sbInset), dp(gtx, sbWidth)
	track := size.Y - 2*inset
	th := min(max(dp(gtx, sbMinBar), size.Y*size.Y/total), max(track, 0))
	ty := inset
	if total > size.Y {
		ty += (track - th) * min(max(top, 0), total-size.Y) / (total - size.Y)
	}
	x := size.X - inset - w
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
