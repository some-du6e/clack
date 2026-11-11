package huddleui

import (
	"image"
	"image/color"
	"strconv"

	"github.com/some-du6e/clack/internal/ui/icons"
	"github.com/some-du6e/clack/internal/ui/theme"
)

// A sidebar row with a huddle on: Slack's BaseChannelSuffixHuddleIcon
// (render PZNh) in the row's suffix, client-boot-styles' rules:
//
//   - HuddleAvatarStackContent (qgIY): up to two participants (maxMembers
//     2, no "+N", no headphones), x_small 20px faces, 25% corners, the
//     second overlapping the first (margin-left -2.235%), 8px after the
//     name; not drawn while you're in that huddle.
//   - HuddlesBadgeFakeMask: a 2px sliver in the badge's colour that rounds
//     the badge's left end against the faces.
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
	faces := !in
	rad := gtx.Dp(4)
	if faces { // corners 0 4px 4px 0 against the faces
		segFill(gtx, r, segCaret, rad, bg)
	} else {
		theme.RRect(gtx, r, rad, bg)
	}
	cx := r.Min.X + (w-(is+gtx.Dp(2)+nw))/2
	at(gtx, image.Pt(cx, y0+(h-is)/2), image.Pt(is, is), func(gtx C) D { return icons.Get("headphones")(gtx, 16, ink) })
	at(gtx, image.Pt(cx+is+gtx.Dp(2), y0), image.Pt(nw, h), func(gtx C) D {
		m := labelSize(gtx, th, 13, theme.Bold, num)
		at(gtx, image.Pt(0, (h-m.Y)/2), m, func(gtx C) D { return th.Label(gtx, 13, theme.Bold, ink, num) })
		return D{Size: image.Pt(nw, h)}
	})
	right = x0
	if !faces {
		return right
	}
	// the fake mask: a 2px sliver that rounds the badge's left end
	mw := gtx.Dp(2)
	segFill(gtx, image.Rect(right-mw, y0, right, y0+h), segMain, mw, bg)
	right -= mw
	// the faces: two 20px avatars, the second 18.5px on
	fs := gtx.Dp(hdrFace)
	n := min(len(people), 2)
	fw := fs + (n-1)*gtx.Dp(hdrFaceGap)
	fx := right - fw
	for i, u := range people[:n] {
		u := u
		at(gtx, image.Pt(fx+i*gtx.Dp(hdrFaceGap), (rowH-fs)/2), image.Pt(fs, fs), func(gtx C) D {
			avatar(gtx, th, c.h.Images(), c.avatarURL(u, fs), image.Pt(fs, fs), fs/4)
			return D{Size: image.Pt(fs, fs)}
		})
	}
	return fx - gtx.Dp(8) // margin-left 8px
}
