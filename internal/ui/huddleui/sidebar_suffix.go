package huddleui

import (
	"image"
	"image/color"
	"strconv"

	"gioui.org/op"
	"gioui.org/op/clip"

	"github.com/some-du6e/clack/internal/huddle"
	"github.com/some-du6e/clack/internal/ui/icons"
	"github.com/some-du6e/clack/internal/ui/theme"
)

// A sidebar row with a huddle on: Slack's BaseChannelSuffixHuddleIcon
// (render PZNh) in the row's suffix, client-boot-styles' rules:
//
//   - HuddleAvatarStack (client-boot qgIY) with excludeIcon and
//     excludeMemberOverflowCount: the first two participants (maxMembers
//     defaults to 2; no "+N", no headphones), x_small 20px faces
//     (qSyV) with 25% corners (--huddles-radius), 8px after the name.
//     Every face after the first sits margin-left -2.235% over the one
//     before (a percentage of the stack's own width, so about 0.87px for
//     two) and is clipped by the stack's clipPath (--huddles-mask: the
//     concave notch on its left). Slack hides the stack only while your
//     huddle here is starting or ending (isHuddleTransitionState: STARTING,
//     PENDING, COMPLETING_PRE_JOIN, ENDING), not while you're in it. A face
//     that hasn't loaded is the base-ter tile (.c-base_icon).
//   - HuddlesBadgeFakeMask: a 2×20 sliver in the badge's colour whose
//     concave left edge meets the faces.
//   - HuddleSidebarLinkPeerCountBadge (lw/3): the headphones and the head
//     count (2px apart, 13px bold), min-width 36px, 20px tall, padding 2px
//     6px, corners 0 4px 4px 0, 4px before what follows; filled
//     theme-base-inv-hgl-1 with content-inv-pry ink, and on the selected
//     row surf-inv with theme-content-hgl-1 (your huddle) or base-pry with
//     content-pry (one you're not in).

// sbBadge is the badge's colours by theme (Slack's default theme tables:
// the inverted sidebar scope in light mode, dark mode).
var sbBadge = [2]struct{ bg, ink, selBg, selInk, selOtherBg, selOtherInk color.NRGBA }{
	{bg: rgba(97, 31, 105, 1), ink: rgba(255, 255, 255, 1), selBg: rgba(255, 255, 255, .18), selInk: rgba(57, 6, 58, 1),
		selOtherBg: rgba(255, 255, 255, 1), selOtherInk: rgba(29, 28, 29, 1)},
	{bg: rgba(125, 57, 134, 1), ink: rgba(248, 248, 248, 1), selBg: rgba(255, 255, 255, .18), selInk: rgba(255, 255, 255, 1),
		selOtherBg: rgba(26, 29, 33, 1), selOtherInk: rgba(248, 248, 248, 1)},
}

// The stack's clipPath (qgIY, clipPathUnits objectBoundingBox) and the
// fake mask's path (PZNh HuddlesBadgeFakeMask, in px of its 2×20 box).
const (
	sbFaceMask = "M0.03,1 h0.742 a0.258,0.2,0,0,0,0.258,-0.2 V0.2 a0.258,0.2,0,0,0,-0.258,-0.2 H0.03 a0.385,0.299,0,0,1,0.098,0.2 v0.6 A0.385,0.299,0,0,1,0.03,1 Z"
	sbFakeMask = "M2.98805e-07 20H2.52782V0H0C0.95008 1.06151 1.52779 2.46329 1.52779 4V16C1.52779 17.5367 0.95008 18.9385 2.98805e-07 20Z"
	// sbFaceOffset is --huddles-offset, a fraction of the stack's width.
	sbFaceOffset = 0.02235
	sbMaxFaces   = 2 // HuddleAvatarStackContent's maxMembers default
)

// transitioning is Slack's hide rule for the stack: your huddle is in ch
// and starting or ending (isHuddleTransitionState of the optimistic huddle
// state). Joining before the session exists is STARTING/PENDING; a session
// still connecting is COMPLETING_PRE_JOIN; one that ended is ENDING.
func (c *Controller) transitioning(ch string) bool {
	c.mu.Lock()
	joining, s := c.joining, c.s
	c.mu.Unlock()
	if joining == ch {
		return true
	}
	if s == nil || s.Channel() != ch {
		return false
	}
	switch s.State().Phase {
	case huddle.Connecting, huddle.Ended:
		return true
	}
	return false
}

// sbFaceSteps is where each face starts (px from the stack's left) for n
// faces of s px: margin-left -2.235% resolves against the stack's final
// width, W = n·s − (n−1)·0.02235·W.
func sbFaceSteps(n, s int) []int {
	if n <= 0 {
		return nil
	}
	w := float64(n*s) / (1 + float64(n-1)*sbFaceOffset)
	step := float64(s) - sbFaceOffset*w
	out := make([]int, n)
	for i := range out {
		out[i] = int(float64(i)*step + .5)
	}
	return out
}

// SidebarSuffix draws a conversation row's huddle suffix ending at right
// (row coordinates, rowH tall) when a huddle is on in ch, and returns the
// new right edge (right itself when there's none). sel is the selected
// row. Main window goroutine.
func (c *Controller) SidebarSuffix(gtx C, th *theme.Theme, ch string, right, rowH int, sel bool) int {
	people := c.headerPeople(ch)
	if _, live := c.Live(ch); !live && !c.In(ch) || len(people) == 0 {
		return right
	}
	k := sbBadge[mode(th)]
	bg, ink := k.bg, k.ink
	theme.SetColor(th.Tokens, &bg, "var(--dt_color-theme-base-inv-hgl-1)")
	theme.SetColor(th.Tokens, &ink, "var(--dt_color-content-inv-pry)")
	in := c.In(ch)
	if sel {
		bg, ink = k.selOtherBg, k.selOtherInk
		if in {
			bg, ink = k.selBg, k.selInk
		}
	}
	right -= gtx.Dp(4) // margin-right
	num := strconv.Itoa(len(people))
	nw := th.Measure(gtx, 13, theme.Bold, num)
	is := gtx.Dp(16)
	h := gtx.Dp(20)
	w := max(gtx.Dp(36), gtx.Dp(6)+is+gtx.Dp(2)+nw+gtx.Dp(6))
	x0 := right - w
	y0 := (rowH - h) / 2
	r := image.Rect(x0, y0, right, y0+h)
	segFill(gtx, r, segCaret, gtx.Dp(4), bg) // corners 0 4px 4px 0
	cx := r.Min.X + (w-(is+gtx.Dp(2)+nw))/2
	at(gtx, image.Pt(cx, y0+(h-is)/2), image.Pt(is, is), func(gtx C) D { return icons.Get("headphones")(gtx, 16, ink) })
	at(gtx, image.Pt(cx+is+gtx.Dp(2), y0), image.Pt(nw, h), func(gtx C) D {
		m := labelSize(gtx, th, 13, theme.Bold, num)
		at(gtx, image.Pt(0, (h-m.Y)/2), m, func(gtx C) D { return th.Label(gtx, 13, theme.Bold, ink, num) })
		return D{Size: image.Pt(nw, h)}
	})
	right = x0
	// the fake mask: its path clipped to its 2×20 box
	mw := gtx.Dp(2)
	func() {
		defer op.Offset(image.Pt(right-mw, y0)).Push(gtx.Ops).Pop()
		defer clip.Rect{Max: image.Pt(mw, h)}.Push(gtx.Ops).Pop()
		sc := float32(h) / 20
		defer clip.Outline{Path: icons.ClipPath(gtx, sbFakeMask, sc)}.Op().Push(gtx.Ops).Pop()
		theme.Fill(gtx, image.Rect(0, 0, mw, h), bg)
	}()
	right -= mw
	if c.transitioning(ch) {
		return right
	}
	// the faces
	fs := gtx.Dp(hdrFace)
	n := min(len(people), sbMaxFaces)
	steps := sbFaceSteps(n, fs)
	fw := steps[n-1] + fs
	fx := right - fw
	for i, u := range people[:n] {
		p := image.Pt(fx+steps[i], (rowH-fs)/2)
		c.sbFace(gtx, th, u, p, fs, i > 0)
	}
	return fx - gtx.Dp(8) // margin-left 8px
}

// sbFace draws one face of the stack at p (s px), clipped by the stack's
// mask when it isn't the first.
func (c *Controller) sbFace(gtx C, th *theme.Theme, id string, p image.Point, s int, masked bool) {
	defer op.Offset(p).Push(gtx.Ops).Pop()
	if masked {
		defer clip.Outline{Path: icons.ClipPath(gtx, sbFaceMask, float32(s))}.Op().Push(gtx.Ops).Pop()
	}
	sz := image.Pt(s, s)
	rad := s / 4 // --huddles-radius
	if url := c.avatarURL(id, s); url != "" && c.h.Images() != nil {
		if img, ok := c.h.Images().Get(url, s); ok {
			theme.Image(gtx, img, sz, rad)
			return
		}
	}
	theme.RRect(gtx, image.Rectangle{Max: sz}, rad, th.C.Skeleton) // .c-base_icon: base-ter
}
