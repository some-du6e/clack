package messages

import (
	"image"

	"gioui.org/layout"
	"gioui.org/op"
	"gioui.org/unit"

	"github.com/some-du6e/yoloslackgoclient/internal/ui/icons"
	"github.com/some-du6e/yoloslackgoclient/internal/ui/slackc"
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
	v.s.closeEdits()
	v.s.hb.ensureLoaded(v.s.m)
	return v
}

// Channel and TS identify the thread.
func (v *ThreadView) Channel() string { return v.ch }
func (v *ThreadView) TS() string      { return v.ts }

// EditLast edits the viewer's most recent reply (composer.OnEditLast).
func (v *ThreadView) EditLast() bool { return v.s.editLast(v.h) }

// Edit starts inline editing of message ts.
func (v *ThreadView) Edit(ts string) { v.s.startEdit(v.h, ts) }

// Leave closes any inline editor in the thread.
func (v *ThreadView) Leave() { v.s.closeEdits() }

// Typing returns the "X is typing…" text for this thread.
func (v *ThreadView) Typing() string { return typingText(v.s.snap.typing) }

// ScrollToBottom jumps to the newest reply.
func (v *ThreadView) ScrollToBottom() { v.s.list.Position = layout.Position{} }

// Layout draws the header and the list: Slack's Serialized(ThreadsFlexpane).
func (v *ThreadView) Layout(gtx layout.Context) layout.Dimensions {
	return slackc.Component(gtx, "Serialized(ThreadsFlexpane)", v, (*ThreadView).flexpaneProps, (*ThreadView).layoutPane)
}

// flexpaneProps are ThreadsFlexpane's: the loaded reply timestamps and a
// ref with scrollToTs / listRef.getScrollmark (Go funcs return nothing to
// JS: scrollToTs scrolls, getScrollmark is a no-op).
func (v *ThreadView) flexpaneProps() slackc.Props {
	ts := make([]any, len(v.s.snap.msgs))
	for i := range v.s.snap.msgs {
		ts[i] = v.s.snap.msgs[i].m.TS
	}
	scrollTo := func(ts string) { v.s.scrollTo(ts); v.h.Invalidate() }
	return v.s.paneProps(slackc.Props{"$key": v.ch + "-" + v.ts, "channelId": v.ch, "threadTs": v.ts, "windowId": "main",
		"timestamps": ts, "highlightKey": v.s.highlight,
		"ref": slackc.Props{"current": slackc.Props{"scrollToTs": scrollTo,
			"listRef": slackc.Props{"current": slackc.Props{"getScrollmark": func() {}}}}}})
}

func (v *ThreadView) layoutPane(gtx layout.Context, _ slackc.Props) layout.Dimensions {
	size := gtx.Constraints.Max
	st := threadClass.Enter(gtx, 0, 0)
	defer st.Leave()
	if st.Hidden() {
		return layout.Dimensions{}
	}
	if st.Active() {
		return st.Wrap(gtx, func(gtx layout.Context) layout.Dimensions { return v.pane(gtx, size) })
	}
	return v.pane(gtx, size)
}

func (v *ThreadView) pane(gtx layout.Context, size image.Point) layout.Dimensions {
	th := v.h.Theme()
	c := colorsFor(th)
	fill(gtx.Ops, image.Rectangle{Max: size}, c.Bg)
	hh := dp(gtx, 49)
	hs := flexHeaderClass.Enter(gtx, 0, 0)
	if !hs.Hidden() {
		v.header(gtx, th, c, size.X, hh)
	}
	hs.Leave()
	// ThreadFlexpaneAdminTools: nothing natively; plugin content pushes
	// the list down.
	g := gtx
	g.Constraints = layout.Constraints{Max: image.Pt(size.X, max(0, size.Y-hh))}
	ad := at(g, 0, hh, func(gtx layout.Context) layout.Dimensions {
		return slackc.Component(gtx, "ThreadFlexpaneAdminTools", v, (*ThreadView).adminToolsProps, (*ThreadView).layoutEmpty)
	})
	top := hh + ad.Size.Y
	t := op.Offset(image.Pt(0, top)).Push(gtx.Ops)
	g.Constraints = layout.Exact(image.Pt(size.X, max(0, size.Y-top)))
	shown(&lastThread, v.s)
	v.s.layoutList(g, v.h, v.Origin.Add(image.Pt(0, top)))
	t.Pop()
	return layout.Dimensions{Size: size}
}

func (v *ThreadView) adminToolsProps() slackc.Props {
	return v.s.paneProps(slackc.Props{"$key": v.ch + "-" + v.ts, "channelId": v.ch, "threadTs": v.ts, "isLockedThread": false, "maxMarkableTs": v.ts})
}

func (v *ThreadView) searchButtonProps() slackc.Props {
	return v.s.paneProps(slackc.Props{"$key": v.ch + "-" + v.ts, "threadKey": v.ch + "-" + v.ts, "windowId": "main", "isThreadTypeSupported": true})
}

// layoutEmpty is the native of boundaries Clack has no surface for
// (ThreadSearchButton, ThreadFlexpaneAdminTools): plugin siblings show.
func (v *ThreadView) layoutEmpty(gtx layout.Context, _ slackc.Props) layout.Dimensions {
	return layout.Dimensions{}
}

func (v *ThreadView) header(gtx layout.Context, th *theme.Theme, c *colors, w, hh int) {
	px := dp(gtx, 16)
	tsT := textStyle{size: 15, weight: theme.Black, col: c.Text, lineH: 22}
	d := at(gtx, px, (hh-dp(gtx, 22))/2, func(gtx layout.Context) layout.Dimensions { return label(gtx, th, tsT, "Thread") })
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
	// header controls: ThreadSearchButton (plugin content only), close
	bs := dp(gtx, 28)
	cx := w - dp(gtx, 12) - bs
	rec := op.Record(gtx.Ops)
	sd := slackc.Component(gtx, "ThreadSearchButton", v, (*ThreadView).searchButtonProps, (*ThreadView).layoutEmpty)
	call := rec.Stop()
	if sd.Size.X > 0 {
		t := op.Offset(image.Pt(cx-dp(gtx, 4)-sd.Size.X, (hh-sd.Size.Y)/2)).Push(gtx.Ops)
		call.Add(gtx.Ops)
		t.Pop()
	}
	if v.s.closeBtn.Clicked(gtx) {
		v.h.CloseSecondary()
	}
	st := flexControlClass.Enter(gtx, btnState(gtx, &v.s.closeBtn), 0)
	defer st.Leave()
	if st.Hidden() {
		return
	}
	at(gtx, cx, (hh-bs)/2, func(gtx layout.Context) layout.Dimensions {
		gtx.Constraints = layout.Exact(image.Pt(bs, bs))
		if st.Active() {
			return st.Wrap(gtx, func(gtx layout.Context) layout.Dimensions { return v.closeButton(gtx, c, st, bs) })
		}
		return v.closeButton(gtx, c, st, bs)
	})
}

func (v *ThreadView) closeButton(gtx layout.Context, c *colors, st slackc.Styled, bs int) layout.Dimensions {
	return v.s.closeBtn.Layout(gtx, func(gtx layout.Context) layout.Dimensions {
		if v.s.closeBtn.Hovered() {
			rrect(gtx.Ops, image.Rectangle{Max: image.Pt(bs, bs)}, dp(gtx, 8), st.Background(c.CloseHover))
		}
		is := dp(gtx, 20)
		at(gtx, (bs-is)/2, (bs-is)/2, func(gtx layout.Context) layout.Dimensions {
			return icons.Get("close")(gtx, unit.Dp(20), st.Color(c.Text3))
		})
		return layout.Dimensions{Size: image.Pt(bs, bs)}
	})
}
