package messages

import (
	"image"

	"gioui.org/io/pointer"
	"gioui.org/layout"
	"gioui.org/op"
	"gioui.org/op/clip"
	"gioui.org/op/paint"
	"gioui.org/unit"
	"gioui.org/widget"

	"github.com/some-du6e/clack/internal/i18n"
	"github.com/some-du6e/clack/internal/ui/icons"
	"github.com/some-du6e/clack/internal/ui/theme"
)

// threadBanner is the thread pane's jump to unread: ThreadFloatingBanners
// with ThreadUnreadBannerGeneric (client-boot-shared puxw, ntqA, JwHt).
// While the thread has unread replies (timestamps past the
// subscription's lastRead, and lastRead short of maxMarkableTs) and the
// "unread" marker (the "New" divider, or the separator carrying it) is
// above or below the visible rows, a pill says "↑ N new replies" at the
// top of the message area (top 5) or "↓ N new replies" at its bottom
// (bottom 8), centred: 28px, #1264a3, radius 14, 13px bold white, the
// label button with 8px sides and the arrow 4px before the text, then a
// 36px close button (times-small); a hovered button is #0b4c8c. The
// label scrolls the marker 100px below the top; the close marks the
// thread read (setThreadLastRead). Once the pane has marked the thread
// read (350 ms after it opens with the user's attention) it is gone.
type threadBanner struct {
	jump, close widget.Clickable
}

// unreadBanner is get_unread_replies over this frame's rows: the marker
// item (-1 none), whether it is above or below the visible rows, and the
// count of unread timestamps.
func (s *listState) unreadBanner() (marker int, up, down bool, n int) {
	sub, lr := s.threadSub()
	if !sub || !tsLess(lr, s.maxMarkable()) {
		return -1, false, false, 0
	}
	marker = -1
	for i, it := range s.items {
		if (it.kind == itUnread || it.kind == itReplies) && it.mark == thrMarkUnread {
			marker = i
			break
		}
	}
	if marker < 0 {
		return -1, false, false, 0
	}
	for i := range s.snap.msgs {
		if tsLess(lr, s.snap.msgs[i].m.TS) {
			n++
		}
	}
	return marker, marker < s.visible[0], marker >= s.visible[1], n
}

// layoutBanner draws the banner over the list area (size) after the list.
func (v *ThreadView) layoutBanner(gtx layout.Context, size image.Point) {
	s := v.s
	marker, up, down, n := s.unreadBanner()
	if v.banner.jump.Clicked(gtx) && marker >= 0 {
		// scrollToKey(separator or divider, {animate, offset: -100})
		s.list.Position = layout.Position{BeforeEnd: true, First: marker, Offset: -dp(gtx, 100)}
		v.h.Invalidate()
	}
	if v.banner.close.Clicked(gtx) {
		if mx := s.maxMarkable(); mx != "" {
			s.setThreadLastRead(v.h, mx)
		}
	}
	if n == 0 || (!up && !down) {
		return
	}
	th := v.h.Theme()
	txt := newRepliesLabel(n)
	ls := textStyle{size: 13, weight: theme.Bold, col: rgb(0xffffff), lineH: 28}
	tw := measure(gtx, th, ls, txt).X
	bh := dp(gtx, 28)
	is := dp(gtx, 16)
	space := measure(gtx, th, ls, " ").X
	jw := dp(gtx, 8) + is + dp(gtx, 4) + space + tw + dp(gtx, 8)
	cw := dp(gtx, 36)
	w := jw + cw
	x := (size.X - w) / 2
	y := dp(gtx, 5)
	icon := "arrow-up"
	if !up {
		y, icon = size.Y-dp(gtx, 8)-bh, "arrow-down"
	}
	r := bh / 2
	blue, hover := rgb(0x1264a3), rgb(0x0b4c8c)
	t := op.Offset(image.Pt(x, y)).Push(gtx.Ops)
	defer t.Pop()
	rrect(gtx.Ops, image.Rectangle{Max: image.Pt(w, bh)}, r, blue)
	btn := func(gtx layout.Context, c *widget.Clickable, x0, bw int, left bool, draw func(gtx layout.Context)) {
		at(gtx, x0, 0, func(gtx layout.Context) layout.Dimensions {
			gtx.Constraints = layout.Exact(image.Pt(bw, bh))
			return c.Layout(gtx, func(gtx layout.Context) layout.Dimensions {
				if c.Hovered() {
					rr := clip.RRect{Rect: image.Rectangle{Max: image.Pt(bw, bh)}}
					if left {
						rr.NW, rr.SW = r, r
					} else {
						rr.NE, rr.SE = r, r
					}
					paint.FillShape(gtx.Ops, hover, rr.Op(gtx.Ops))
				}
				draw(gtx)
				defer clip.Rect{Max: image.Pt(bw, bh)}.Push(gtx.Ops).Pop()
				pointer.CursorPointer.Add(gtx.Ops)
				return layout.Dimensions{Size: image.Pt(bw, bh)}
			})
		})
	}
	btn(gtx, &v.banner.jump, 0, jw, true, func(gtx layout.Context) {
		// the inline icon: top 2px against the 28px line
		at(gtx, dp(gtx, 8), (bh-is)/2+dp(gtx, 2), func(gtx layout.Context) layout.Dimensions {
			return icons.Get(icons.Slack(icon))(gtx, unit.Dp(16), rgb(0xffffff))
		})
		at(gtx, dp(gtx, 8)+is+dp(gtx, 4)+space, 0, func(gtx layout.Context) layout.Dimensions {
			return lineBox(gtx, ls, func(gtx layout.Context) layout.Dimensions { return label(gtx, th, ls, txt) })
		})
	})
	btn(gtx, &v.banner.close, jw, cw, false, func(gtx layout.Context) {
		cs := dp(gtx, 20)
		// text-align center, padding-top 1, padding-right 2
		at(gtx, (cw-dp(gtx, 2)-cs)/2, (bh-cs)/2+dp(gtx, 1), func(gtx layout.Context) layout.Dimensions {
			return icons.Get("times-small")(gtx, unit.Dp(20), rgb(0xffffff))
		})
	})
}

// newRepliesLabel is ThreadUnreadBannerGeneric's threads string
// "{replies,plural,=1{{replies} new reply}other{{replies} new replies}}".
func newRepliesLabel(n int) string {
	if i18n.Locale() == i18n.EnUS {
		if n == 1 {
			return "1 new reply"
		}
		return itoa(n) + " new replies"
	}
	return threadsNS.T("{replies,plural,=1{{replies} new reply}other{{replies} new replies}}", "replies", n)
}
