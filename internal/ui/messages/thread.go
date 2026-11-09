package messages

import (
	"context"
	"image"
	"sync/atomic"
	"time"

	"gioui.org/layout"
	"gioui.org/op"
	"gioui.org/unit"
	"gioui.org/widget"

	"github.com/some-du6e/yoloslackgoclient/internal/core"

	"github.com/some-du6e/yoloslackgoclient/internal/ui/icons"
	"github.com/some-du6e/yoloslackgoclient/internal/ui/popups"
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

	top int // header (+ admin tools) height of the last frame

	bellBtn, moreBtn widget.Clickable
	bellTip, moreTip popups.TipState
	follow           *bool                  // the bell's state clicked, until the root message has it
	followAt         time.Time              // when follow was set
	failed           atomic.Pointer[string] // the subscribe request's error (set off the UI goroutine)
}

// ContentHeight is the height the pane needs to show the whole thread (header,
// parent, divider and replies) as of the last frame, or 0 when the thread is
// taller than the pane. Slack top-aligns a short thread with its reply
// composer right under the last reply; the ui root places the composer with it.
func (v *ThreadView) ContentHeight() int {
	if v.s.contentH <= 0 || len(v.s.items) == 0 {
		return 0
	}
	return v.top + v.s.contentH
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
	v.top = top
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

// PrimaryHost is optional: the channel the primary view shows ("" when it
// isn't a channel), for the thread header's channel name.
type PrimaryHost interface {
	PrimaryChannel() string
}

// channelName is the header's subtitle (Slack's thread Header): "#name",
// the DM partner's name, or the group DM's. ThreadSubtitle hides it
// (hideChannelName) when the primary view is the thread's own channel.
func (v *ThreadView) channelName() string {
	if ph, ok := v.h.(PrimaryHost); ok && ph.PrimaryChannel() == v.ch {
		return ""
	}
	app := v.h.Core()
	if app == nil {
		return ""
	}
	cv, ok := app.Conv(v.ch)
	if !ok {
		return ""
	}
	switch cv.Kind {
	case "im":
		if u, ok := app.User(cv.User); ok {
			return u.Name
		}
		return cv.Name
	case "mpim":
		return cv.Name
	}
	return "#" + cv.Name
}

// root is the thread's parent message, once loaded.
func (v *ThreadView) root() (core.Message, bool) {
	for i := range v.s.snap.msgs {
		if m := &v.s.snap.msgs[i].m; m.TS == v.ts {
			return *m, true
		}
	}
	return core.Message{}, false
}

// subscribed is whether the viewer follows the thread: the root's
// subscribed flag, or the toggle just clicked until the store has it.
func (v *ThreadView) subscribed(m core.Message) bool {
	if v.follow != nil {
		if *v.follow == m.FollowingThread || time.Since(v.followAt) > 10*time.Second {
			v.follow = nil
		} else {
			return *v.follow
		}
	}
	return m.FollowingThread
}

// bellShown mirrors module 5mp4's null cases: the root is loaded, and the
// subscribe action exists (ojBT returns null for a channel the viewer
// isn't in unless they already follow the thread).
func (v *ThreadView) bellShown(m core.Message, ok bool) bool {
	if !ok || v.h.Core() == nil {
		return false
	}
	if cv, found := v.h.Core().Conv(v.ch); found && cv.Preview && !v.subscribed(m) {
		return false
	}
	return true
}

// toggleFollow is toggleThreadSubscription: flip the flag now, then
// subscriptions.thread.add / remove.
func (v *ThreadView) toggleFollow(m core.Message) {
	app := v.h.Core()
	if app == nil {
		return
	}
	on := !v.subscribed(m)
	v.follow, v.followAt = &on, time.Now()
	ch, ts, h := v.ch, v.ts, v.h
	go func() {
		ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
		defer cancel()
		if err := app.SubscribeThread(ctx, ch, ts, on); err != nil {
			msg := "Couldn't do that: " + err.Error()
			v.failed.Store(&msg)
		}
		h.Invalidate()
	}()
}

func (v *ThreadView) header(gtx layout.Context, th *theme.Theme, c *colors, w, hh int) {
	if msg := v.failed.Swap(nil); msg != nil { // the request failed: back to the store's state
		v.follow = nil
		v.h.Toast(*msg)
	}
	px := dp(gtx, 16)
	// .p-flexpane_header__primary: 18px/24px Black, gap 8 to the subtitle.
	tsT := textStyle{size: 18, weight: theme.Black, col: c.Text, lineH: 24}
	ty := (hh - dp(gtx, 24)) / 2
	d := at(gtx, px, ty, func(gtx layout.Context) layout.Dimensions {
		return lineBox(gtx, tsT, func(gtx layout.Context) layout.Dimensions { return label(gtx, th, tsT, "Thread") })
	})
	if name := v.channelName(); name != "" {
		// 13px/18px (.p-threads_flexpane__header_channel_name), on the
		// title's baseline (align-items: baseline).
		ns := textStyle{size: 13, col: c.Text2, lineH: 18}
		top := ty + (d.Size.Y - d.Baseline) - cssBaseline(gtx, ns.size, ns.lineH)
		at(gtx, px+d.Size.X+dp(gtx, 8), top, func(gtx layout.Context) layout.Dimensions {
			gtx.Constraints.Max.X = w - px - d.Size.X - dp(gtx, 120)
			return lineBox(gtx, ns, func(gtx layout.Context) layout.Dimensions { return label(gtx, th, ns, name) })
		})
	}
	// header controls, right to left 6px apart (p-flexpane_header__primary
	// --gap): close, ⋮ (SecondaryHeaderMore), the bell
	// (thread-mute-button), then plugin content in ThreadSearchButton.
	// Slack's viewHeaderActionButton is 28×28, radius 8.
	bs := dp(gtx, 28)
	gap := dp(gtx, 6)
	cx := w - dp(gtx, 12) - bs
	root, rootOK := v.root()
	sub := v.subscribed(root)
	bell := v.bellShown(root, rootOK)
	mx := cx - gap - bs
	left := mx
	if bell {
		left = mx - gap - bs
	}
	rec := op.Record(gtx.Ops)
	sd := slackc.Component(gtx, "ThreadSearchButton", v, (*ThreadView).searchButtonProps, (*ThreadView).layoutEmpty)
	call := rec.Stop()
	if sd.Size.X > 0 {
		t := op.Offset(image.Pt(left-gap-sd.Size.X, (hh-sd.Size.Y)/2)).Push(gtx.Ops)
		call.Add(gtx.Ops)
		t.Pop()
	}
	if v.s.closeBtn.Clicked(gtx) {
		v.h.CloseSecondary()
	}
	if v.moreBtn.Clicked(gtx) {
		r := image.Rect(mx, (hh-bs)/2, mx+bs, (hh+bs)/2).Add(v.Origin)
		m := root
		v.h.ShowOverlay(popups.NewThreadHeaderMenu(v.h, r, m, sub, bell, func() { v.toggleFollow(m) }))
	}
	if v.bellBtn.Clicked(gtx) && bell {
		v.toggleFollow(root)
		sub = !sub
	}
	st := flexControlClass.Enter(gtx, btnState(gtx, &v.s.closeBtn), 0)
	defer st.Leave()
	if st.Hidden() {
		return
	}
	if bell {
		icon := "notifications-off"
		if sub {
			icon = "notifications"
		}
		v.headerButton(gtx, th, c, st, &v.bellBtn, &v.bellTip, image.Pt(left, (hh-bs)/2), bs, icon, popups.ThreadFollowLabel(sub))
	}
	v.headerButton(gtx, th, c, st, &v.moreBtn, &v.moreTip, image.Pt(mx, (hh-bs)/2), bs, "ellipsis-vertical-filled", "More actions")
	at(gtx, cx, (hh-bs)/2, func(gtx layout.Context) layout.Dimensions {
		gtx.Constraints = layout.Exact(image.Pt(bs, bs))
		if st.Active() {
			return st.Wrap(gtx, func(gtx layout.Context) layout.Dimensions { return v.closeButton(gtx, c, st, bs) })
		}
		return v.closeButton(gtx, c, st, bs)
	})
}

// headerButton is a 28px header icon button with its tooltip below
// (Slack's: position bottom, delay 300ms).
func (v *ThreadView) headerButton(gtx layout.Context, th *theme.Theme, c *colors, st slackc.Styled, btn *widget.Clickable, tip *popups.TipState, pos image.Point, bs int, icon, label string) {
	at(gtx, pos.X, pos.Y, func(gtx layout.Context) layout.Dimensions {
		gtx.Constraints = layout.Exact(image.Pt(bs, bs))
		d := btn.Layout(gtx, func(gtx layout.Context) layout.Dimensions {
			if btn.Hovered() {
				rrect(gtx.Ops, image.Rectangle{Max: image.Pt(bs, bs)}, dp(gtx, 8), st.Background(c.CloseHover))
			}
			is := dp(gtx, 20)
			at(gtx, (bs-is)/2, (bs-is)/2, func(gtx layout.Context) layout.Dimensions {
				return icons.Get(icons.Slack(icon))(gtx, unit.Dp(20), st.Color(c.Text3))
			})
			return layout.Dimensions{Size: image.Pt(bs, bs)}
		})
		popups.Tooltip(gtx, th, tip, btn.Hovered(), d.Size, popups.Below, label, "")
		return d
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
