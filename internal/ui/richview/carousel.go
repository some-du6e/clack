package richview

// A carousel block: Slack's CarouselBlock (boot-shared ZDqH) in its
// GalleryScroller (LSo8; CDN build 9f450b8ad3db), as it draws with
// a11y_carousel off, as on the accounts Clack follows. From
// client-boot-styles:
//
//   - .carousel: width 100%, margin 8px 0 (blocks.go's margins); the
//     label above the scroller when isLabelVisible;
//   - the cards side by side 16px apart (the content's flex row, gap 16),
//     each 340px (.cardWrapper), drawn as blocks of their own
//     (renderBlocks with noBlockMargin) and stretched to the tallest
//     (align-items stretch);
//   - the scroller (.p-gallery_scroller__wrapper--can_scroll): overflow-x
//     scroll with no scrollbar, so a wheel's or trackpad's sideways
//     scroll moves it; its arrows inside (12px in, centred: 32px circles
//     of base-pry with the arrow shadows and caret-left / caret-right in
//     foreground_max_solid), the left one while it's scrolled, the right
//     one while more is to come; an arrow scrolls a page (width - width %
//     356), to the end when that's under 50px away, over 160ms.
//
// A wheel gesture stays on the scroller its first event was over, as
// Chromium latches it (WheelLatch, NoteWheel): a carousel takes only a
// gesture that began on it, or any while none is going.
//
// Left out: the arrows' hover scale and their fade (they show or hide at
// once), the track animation's exact easing (a cubic ease-out here), a
// text selection across the cards (each card's view selects its own),
// and focus with the keyboard.

import (
	"image"
	"strconv"
	"time"

	"gioui.org/f32"
	"gioui.org/io/event"
	"gioui.org/io/pointer"
	"gioui.org/layout"
	"gioui.org/op"
	"gioui.org/op/clip"
	"gioui.org/op/paint"
	"gioui.org/widget"

	"github.com/some-du6e/clack/internal/richtext"
)

// Carousel geometry (Dp).
const (
	crCard  = 340 // .cardWrapper
	crGap   = 16  // the content's gap
	crArrow = 32  // .p-gallery_scroller__arrow
	crInset = 12  // --inside: left 12px / right 12px
	crSnap  = 50  // scrollByDirection snaps to an end this close
	crPage  = crCard + crGap
)

// crAnim is scrollWithAnimation's duration.
const crAnim = 160 * time.Millisecond

// carouselState is a carousel's scroller: its cards' views, the scroll
// (px) and the arrows. It outlives relayouts (View.carousels).
type carouselState struct {
	cards []*View
	src   []*richtext.Block
	// scroll is scrollLeft; from, to and start the arrow's animation (on
	// while it runs)
	scroll, from, to float32
	start            time.Time
	on               bool
	left, right      widget.Clickable
	// obs sees the wheel over the scroller and takes none of it; seen is
	// the first event it saw of a gesture, seenAt when it saw the last
	obs    int
	seen   time.Duration
	seenAt time.Time
	// cardW, gap (px), the content's width and the scroller's, and its
	// page (pageSize)
	cardW, gap, contentW, viewW, page int
}

// carouselSlot is a laid out carousel: the scroller's box, view-local.
type carouselSlot struct {
	r  image.Rectangle
	st *carouselState
}

// maxScroll is getMaxScroll: the content's scrollWidth past the scroller.
func (c *carouselState) maxScroll() float32 {
	return float32(max(c.contentW-c.viewW, 0))
}

// clampScroll keeps scroll in [0, maxScroll].
func (c *carouselState) clampScroll() {
	c.scroll = min(max(c.scroll, 0), c.maxScroll())
	c.to = min(max(c.to, 0), c.maxScroll())
}

// scrollBy is scrollByDirection: a page left (dir < 0) or right, clamped,
// to the end when it'd stop under 50px from it, animated from now.
func (c *carouselState) scrollBy(dir int, now time.Time, snap float32) {
	mx := c.maxScroll()
	t := c.scroll + float32(dir*c.page)
	t = min(max(t, 0), mx)
	switch {
	case mx-t < snap:
		t = mx
	case t < snap:
		t = 0
	}
	c.from, c.to, c.start, c.on = c.scroll, t, now, true
}

// tick advances the animation to now; it reports whether it still runs.
func (c *carouselState) tick(now time.Time) bool {
	if !c.on {
		return false
	}
	t := now.Sub(c.start)
	if t >= crAnim || now.IsZero() {
		c.scroll, c.on = c.to, false
		return false
	}
	f := float32(t) / float32(crAnim)
	f = 1 - (1-f)*(1-f)*(1-f)
	c.scroll = c.from + (c.to-c.from)*f
	return true
}

// crMaxDepth bounds carousels in carousels' cards, as blockkit's
// maxNesting does nested blocks: a deeper one draws nothing (each level
// lays its cards out again).
const crMaxDepth = 8

// carousel lays out a carousel block: the label, then the scroller as
// tall as its tallest card; the cards draw in it (drawCarousels).
func (l *lay) carousel(b *richtext.Block, x, y, w int) int {
	if l.opts.carouselDepth >= crMaxDepth {
		return 0
	}
	key := b.BlockID
	if key == "" {
		key = "#" + strconv.Itoa(l.carouselN)
	}
	l.carouselN++
	c := l.carousels[key]
	if c == nil {
		c = &carouselState{}
		if l.carousels != nil {
			l.carousels[key] = c
		}
	}
	yy := y
	if cr := b.Carousel; cr != nil && cr.LabelVisible {
		fs := l.bodyStyle()
		l.plainAdd("\n")
		h, _ := l.plainFlow(cr.Label, x, yy, w, &fs, -1)
		yy += h
	}
	c.cardW, c.gap = l.dp(crCard), l.dp(crGap)
	n := len(b.Children)
	if len(c.cards) != n {
		c.cards, c.src = make([]*View, n), make([]*richtext.Block, n)
	}
	o := l.opts
	o.NoOuterMargins, o.CardHeight = true, 0
	// a card's view is none of the message's own: no "(edited)" or
	// stopped label after its text, no line cap, one level deeper
	o.Edited, o.EditedTS, o.EditedBy = false, "", ""
	o.Stopped, o.StoppedBy = false, ""
	o.MaxLines, o.Truncate = 0, false
	o.carouselDepth++
	tall := 0
	for i := range b.Children {
		bk := &b.Children[i]
		if c.cards[i] == nil || c.src[i] != bk {
			c.cards[i], c.src[i] = New(richtext.Doc{Blocks: []richtext.Block{*bk}}, o), bk
		}
		cv := c.cards[i]
		cv.opts = o
		cv.relayout(l.st, c.cardW, l.scale, l.spx)
		tall = max(tall, cv.d.size.Y)
	}
	for _, cv := range c.cards { // align-items: stretch, the tallest's frame too
		cv.opts.CardHeight = tall
		if cv.d.size.Y != tall { // the tallest already is: the stretch changes nothing
			cv.relayout(l.st, c.cardW, l.scale, l.spx)
		}
	}
	c.viewW = w
	c.contentW = max(n*c.cardW+max(n-1, 0)*c.gap, w) // min-width 100%
	page := l.dp(crPage)
	c.page = w - w%max(page, 1)
	c.clampScroll()
	l.d.carousels = append(l.d.carousels, carouselSlot{r: image.Rect(x, yy, x+w, yy+tall), st: c})
	return yy + tall - y
}

// drawCarousels draws each carousel's visible cards under its scroller's
// clip, then its arrows, and takes its sideways wheel.
func (v *View) drawCarousels(gtx layout.Context, st *Style, h Handler) {
	for i := range v.d.carousels {
		cs := &v.d.carousels[i]
		c := cs.st
		for {
			ev, ok := gtx.Event(pointer.Filter{Target: &c.obs, Kinds: pointer.Scroll}) // no range: it takes none
			if !ok {
				break
			}
			if e, ok := ev.(pointer.Event); ok && e.Kind == pointer.Scroll {
				if c.seenAt.IsZero() || gtx.Now.Sub(c.seenAt) >= WheelLatch {
					c.seen = e.Time // the first it saw of a gesture
				}
				c.seenAt = gtx.Now
			}
		}
		// the gesture going on began elsewhere: it isn't the carousel's
		// until it ends (one frame asked for then)
		rng := pointer.ScrollRange{Min: -int(c.scroll), Max: int(c.maxScroll() - c.scroll)}
		if wheelGoing(gtx.Now) && c.seen != wheelG.start {
			rng = pointer.ScrollRange{}
			gtx.Execute(op.InvalidateCmd{At: wheelG.lastAt.Add(WheelLatch)})
		}
		for {
			ev, ok := gtx.Event(pointer.Filter{Target: c, Kinds: pointer.Scroll, ScrollX: rng})
			if !ok {
				break
			}
			if e, ok := ev.(pointer.Event); ok && e.Kind == pointer.Scroll && e.Scroll.X != 0 {
				c.on = false
				c.scroll += e.Scroll.X
				c.clampScroll()
			}
		}
		if c.left.Clicked(gtx) {
			c.scrollBy(-1, gtx.Now, float32(v.px(crSnap)))
		}
		if c.right.Clicked(gtx) {
			c.scrollBy(1, gtx.Now, float32(v.px(crSnap)))
		}
		if c.tick(gtx.Now) {
			gtx.Execute(op.InvalidateCmd{})
		}
		cl := clip.Rect(cs.r).Push(gtx.Ops)
		func() {
			defer pointer.PassOp{}.Push(gtx.Ops).Pop()
			event.Op(gtx.Ops, &c.obs)
		}()
		event.Op(gtx.Ops, c)
		sx := int(c.scroll + 0.5)
		for k, cv := range c.cards {
			cx := cs.r.Min.X + k*(c.cardW+c.gap) - sx
			if cx+c.cardW <= cs.r.Min.X || cx >= cs.r.Max.X {
				continue
			}
			off := op.Offset(image.Pt(cx, cs.r.Min.Y)).Push(gtx.Ops)
			g := gtx
			g.Constraints = layout.Exact(image.Pt(c.cardW, cs.r.Dy()))
			cv.Origin = v.Origin.Add(image.Pt(cx, cs.r.Min.Y)) // its anchors in the parent's terms
			cv.Layout(g, st, h)
			off.Pop()
		}
		cl.Pop()
		v.carouselArrow(gtx, st, cs, &c.left, "caret-left", c.scroll > 0.5)
		v.carouselArrow(gtx, st, cs, &c.right, "caret-right", c.contentW > c.viewW && c.scroll < c.maxScroll()-0.5)
	}
}

// carouselArrowRect is an arrow's 32px box in cs's scroller: 12px in from
// its side, centred.
func (v *View) carouselArrowRect(cs *carouselSlot, left bool) image.Rectangle {
	s, in := v.px(crArrow), v.px(crInset)
	y := cs.r.Min.Y + (cs.r.Dy()-s)/2
	x := cs.r.Max.X - in - s
	if left {
		x = cs.r.Min.X + in
	}
	return image.Rect(x, y, x+s, y+s)
}

// carouselArrow draws one of a scroller's arrows while it shows: the
// filled circle with the arrow shadows (0 1px 3px and 0 1px 2px -1px,
// #00000019) and the caret, a click scrolling a page.
func (v *View) carouselArrow(gtx layout.Context, st *Style, cs *carouselSlot, btn *widget.Clickable, icon string, shown bool) {
	if !shown {
		return
	}
	p := &st.Palette
	r := v.carouselArrowRect(cs, icon == "caret-left")
	rad := r.Dx() / 2
	sh := rgba(0, 0, 0, 0x19/255.0)
	drawShadow(gtx.Ops, r.Add(image.Pt(0, v.px(1))), rad, float32(v.px(3)), sh)
	drawShadow(gtx.Ops, r.Add(image.Pt(0, v.px(1))).Inset(v.px(1)), rad, float32(v.px(2)), sh)
	paint.FillShape(gtx.Ops, p.Panel, clip.UniformRRect(r, rad).Op(gtx.Ops))
	off := op.Offset(r.Min).Push(gtx.Ops)
	g := gtx
	g.Constraints = layout.Exact(r.Size())
	btn.Layout(g, func(gtx layout.Context) layout.Dimensions {
		pointer.CursorPointer.Add(gtx.Ops)
		return layout.Dimensions{Size: r.Size()}
	})
	off.Pop()
	v.barIcon(gtx, icon, r, 20, p.Text2)
}

// CarouselRect is a laid out carousel (tests): its scroller, cards'
// boxes as drawn now (view-local), its arrows (empty while hidden) and
// scroll.
type CarouselRect struct {
	R, Left, Right image.Rectangle
	Cards          []image.Rectangle
	Scroll         float32
}

// CarouselRects lists the view's carousels (tests).
func (v *View) CarouselRects() []CarouselRect {
	var out []CarouselRect
	for i := range v.d.carousels {
		cs := &v.d.carousels[i]
		c := cs.st
		r := CarouselRect{R: cs.r, Scroll: c.scroll}
		sx := int(c.scroll + 0.5)
		for k := range c.cards {
			cx := cs.r.Min.X + k*(c.cardW+c.gap) - sx
			r.Cards = append(r.Cards, image.Rect(cx, cs.r.Min.Y, cx+c.cardW, cs.r.Max.Y))
		}
		if c.scroll > 0.5 {
			r.Left = v.carouselArrowRect(cs, true)
		}
		if c.contentW > c.viewW && c.scroll < c.maxScroll()-0.5 {
			r.Right = v.carouselArrowRect(cs, false)
		}
		out = append(out, r)
	}
	return out
}

// CarouselCard is a carousel's card k's view (tests).
func (v *View) CarouselCard(i, k int) *View {
	if i < 0 || i >= len(v.d.carousels) || k < 0 || k >= len(v.d.carousels[i].st.cards) {
		return nil
	}
	return v.d.carousels[i].st.cards[k]
}

// inCarousel reports whether p (view-local) is in a carousel's box, whose
// cards' views and arrows take their own clicks: a press there is no
// plain click on this view (a search result opens its message for one).
func (v *View) inCarousel(p f32.Point) bool {
	pt := image.Pt(int(p.X), int(p.Y))
	for i := range v.d.carousels {
		if pt.In(v.d.carousels[i].r) {
			return true
		}
	}
	return false
}

// WheelLatch is how long a wheel gesture stays on the scroller its first
// event was over after its last event: Chromium's MouseWheelPhaseHandler
// ends a phaseless scroll sequence after kDefaultMouseWheelLatchingTransaction
// (hovercard's WheelLatch).
const WheelLatch = 500 * time.Millisecond

// wheelG is the window's wheel gesture as NoteWheel hears it: its first
// event's time and its last's, and the frame the last came in (UI
// goroutine).
var wheelG struct {
	start, last time.Duration
	lastAt      time.Time
}

// NoteWheel tells richview of each wheel event in the window (t its event
// time, now the frame's), from the ui root's window-wide observer, which
// takes none of the scroll: a carousel takes a gesture only if it began
// on it.
func NoteWheel(t time.Duration, now time.Time) {
	if !wheelGoing(now) {
		wheelG.start = t
	}
	wheelG.last, wheelG.lastAt = t, now
}

// wheelGoing reports whether a wheel gesture is going on at now.
func wheelGoing(now time.Time) bool {
	return !wheelG.lastAt.IsZero() && now.Sub(wheelG.lastAt) < WheelLatch
}

// WheelElsewhere reports whether the wheel gesture going on at now began
// before seen, the first of its events a scroller saw (so that scroller
// takes none of it, as a carousel doesn't), and when the gesture will
// have ended: blockkit's file gallery latches as the carousels do.
func WheelElsewhere(now time.Time, seen time.Duration) (bool, time.Time) {
	return wheelGoing(now) && seen != wheelG.start, wheelG.lastAt.Add(WheelLatch)
}
