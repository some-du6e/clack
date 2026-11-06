package messages

import (
	"image"

	"gioui.org/layout"
	"gioui.org/op"
	"gioui.org/unit"

	"github.com/some-du6e/yoloslackgoclient/internal/ui/icons"
	"github.com/some-du6e/yoloslackgoclient/internal/ui/theme"
)

// ThreadView is the thread pane: header "Thread · #channel", the parent
// message, an "N replies" divider and the replies. The composer is embedded
// by the ui root below it.
type ThreadView struct {
	h  Host
	ch string
	ts string
	s  *listState

	// Origin is the view's top-left in window coordinates (see ChannelView).
	Origin image.Point
}

// NewThreadView returns the thread pane for the thread rooted at ts.
func NewThreadView(h Host, ch, ts string) *ThreadView {
	v := &ThreadView{h: h, ch: ch, ts: ts, s: stateFor(h, ch, ts)}
	v.s.hb.ensureLoaded(v.s.m)
	return v
}

// Channel and TS identify the thread.
func (v *ThreadView) Channel() string { return v.ch }
func (v *ThreadView) TS() string      { return v.ts }

// Typing returns the "X is typing…" text for this thread.
func (v *ThreadView) Typing() string { return typingText(v.s.snap.typing) }

// ScrollToBottom jumps to the newest reply.
func (v *ThreadView) ScrollToBottom() { v.s.list.Position = layout.Position{} }

// Layout draws the header and the list.
func (v *ThreadView) Layout(gtx layout.Context) layout.Dimensions {
	th := v.h.Theme()
	c := colorsFor(th)
	size := gtx.Constraints.Max
	fill(gtx.Ops, image.Rectangle{Max: size}, c.Bg)
	hh := dp(gtx, 49)
	v.header(gtx, th, c, size.X, hh)
	t := op.Offset(image.Pt(0, hh)).Push(gtx.Ops)
	g := gtx
	g.Constraints = layout.Exact(image.Pt(size.X, max(0, size.Y-hh)))
	v.s.layoutList(g, v.h, v.Origin.Add(image.Pt(0, hh)))
	t.Pop()
	return layout.Dimensions{Size: size}
}

func (v *ThreadView) header(gtx layout.Context, th *theme.Theme, c *colors, w, hh int) {
	px := dp(gtx, 16)
	tsT := textStyle{size: 18, weight: theme.Black, col: c.Text, lineH: 24}
	d := at(gtx, px, (hh-dp(gtx, 24))/2, func(gtx layout.Context) layout.Dimensions { return label(gtx, th, tsT, "Thread") })
	name := ""
	if app := v.h.Core(); app != nil {
		if cv, ok := app.Conv(v.ch); ok {
			name = cv.Name
			switch cv.Kind {
			case "im":
				if u, ok := app.User(cv.User); ok {
					name = u.Name
				}
			case "mpim":
			default:
				name = "#" + name
			}
		}
	}
	if name != "" {
		at(gtx, px+d.Size.X+dp(gtx, 8), (hh-dp(gtx, 18))/2+dp(gtx, 1), func(gtx layout.Context) layout.Dimensions {
			gtx.Constraints.Max.X = w - px - d.Size.X - dp(gtx, 60)
			return label(gtx, th, textStyle{size: 13, col: c.Text2, lineH: 18}, name)
		})
	}
	// close button
	bs := dp(gtx, 32)
	if v.s.closeBtn.Clicked(gtx) {
		v.h.CloseSecondary()
	}
	at(gtx, w-dp(gtx, 12)-bs, (hh-bs)/2, func(gtx layout.Context) layout.Dimensions {
		gtx.Constraints = layout.Exact(image.Pt(bs, bs))
		return v.s.closeBtn.Layout(gtx, func(gtx layout.Context) layout.Dimensions {
			if v.s.closeBtn.Hovered() {
				rrect(gtx.Ops, image.Rectangle{Max: image.Pt(bs, bs)}, dp(gtx, 6), c.CloseHover)
			}
			is := dp(gtx, 18)
			at(gtx, (bs-is)/2, (bs-is)/2, func(gtx layout.Context) layout.Dimensions {
				return icons.Get("close")(gtx, unit.Dp(18), c.Text2)
			})
			return layout.Dimensions{Size: image.Pt(bs, bs)}
		})
	})
	fill(gtx.Ops, image.Rect(0, hh-dp(gtx, 1), w, hh), c.HeaderBorder)
}
