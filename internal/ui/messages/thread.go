package messages

import (
	"context"
	"image"
	"sort"
	"strconv"
	"strings"
	"sync/atomic"
	"time"

	"gioui.org/io/pointer"
	"gioui.org/layout"
	"gioui.org/op"
	"gioui.org/op/clip"
	"gioui.org/unit"
	"gioui.org/widget"

	"github.com/some-du6e/yoloslackgoclient/internal/core"

	"github.com/some-du6e/yoloslackgoclient/internal/ui/icons"
	"github.com/some-du6e/yoloslackgoclient/internal/ui/popups"
	"github.com/some-du6e/yoloslackgoclient/internal/ui/slackc"
	"github.com/some-du6e/yoloslackgoclient/internal/ui/theme"
)

// ThreadView is the thread pane: header "Thread" and the conversation, the parent
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

	bellBtn, moreBtn, nameBtn widget.Clickable
	bellTip, moreTip, nameTip popups.TipState
	nameRect                  image.Rectangle        // the subtitle, view coordinates (last frame)
	nameSub                   HeaderSubtitle         // what it showed
	resolving                 bool                   // the conversation is being looked up for the subtitle
	follow                    *bool                  // the bell's state clicked, until the root message has it
	followAt                  time.Time              // when follow was set
	failed                    atomic.Pointer[string] // the subscribe request's error (set off the UI goroutine)
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

// Typing returns the "X is typing" text for this thread.
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

// HeaderSubtitle is the header's conversation name after "Thread":
// Slack's threads Header (client-boot-shared NCqL). Channels draw
// InlineChannelEntity (an icon from getChannelIconType, then the name,
// " (archived)" after an archived one's); a DM the partner's name; a
// group DM "Direct message with N others" with the members in a tooltip.
type HeaderSubtitle struct {
	Icon string // Slack icon name, "" for DMs
	Text string
	Tip  string
	// Link: the name links to the root's permalink (once the root is
	// loaded, like Slack's permalink prop).
	Link bool
}

// subtitle is what the header shows after "Thread", false for nothing:
// ThreadSubtitle hides it (hideChannelName) when the primary view shows
// the thread's own channel, and Clack draws nothing until core knows the
// conversation's name (Slack draws its "Loading…" shimmer then): an
// unknown or nameless conversation, as a thread opened from Activity can
// be, is looked up through core (ResolveConv) and an unknown DM partner
// with users.info.
func (v *ThreadView) subtitle() (HeaderSubtitle, bool) {
	if ph, ok := v.h.(PrimaryHost); ok && ph.PrimaryChannel() == v.ch {
		return HeaderSubtitle{}, false
	}
	app := v.h.Core()
	if app == nil {
		return HeaderSubtitle{}, false
	}
	cv, ok := app.Conv(v.ch)
	if !ok || (cv.Kind == "im" && cv.User == "") || (cv.Kind != "im" && cv.Name == "") {
		v.resolve(app)
		return HeaderSubtitle{}, false
	}
	switch cv.Kind {
	case "im":
		u, ok := app.User(cv.User)
		if !ok || u.Name == "" {
			ensureUser(v.h, cv.User)
			return HeaderSubtitle{}, false
		}
		return HeaderSubtitle{Text: u.Name}, true
	case "mpim":
		for _, id := range cv.Members {
			if _, ok := app.User(id); !ok {
				ensureUser(v.h, id)
			}
		}
		n, names := mpimMembers(app, cv)
		return HeaderSubtitle{Text: "Direct message with " + strconv.Itoa(n-1) + " others", Tip: names}, true
	}
	// getChannelIconType: archived (or frozen) first, then private, else
	// the channel hash; InlineChannelIcon asks for the filled variant for a
	// private channel. Shared channels draw the same icons. ChannelName
	// adds " (archived)".
	sub := HeaderSubtitle{Icon: "channel", Text: cv.Name}
	private := cv.Kind == "private"
	switch {
	case cv.IsArchived && private:
		sub.Icon, sub.Text = "archive-filled", cv.Name+" (archived)"
	case cv.IsArchived:
		sub.Icon, sub.Text = "archive", cv.Name+" (archived)"
	case private:
		sub.Icon = "lock-filled"
	}
	return sub, true
}

// resolve looks the thread's conversation up once (off the UI goroutine).
func (v *ThreadView) resolve(app *core.App) {
	if v.resolving {
		return
	}
	v.resolving = true
	ch, h := v.ch, v.h
	go func() {
		ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
		defer cancel()
		if _, ok := app.ResolveConv(ctx, ch); ok {
			h.Invalidate()
		}
	}()
}

// mpimMembers is a group DM's member count (the viewer included, Slack's
// getMpimMembers) and getMpimDisplayNameAlphabetical: the others' names,
// sorted, comma-separated. Without a member list the mpdm-a--b--c-1 name
// lists the handles.
func mpimMembers(app *core.App, cv core.Conv) (int, string) {
	self := app.Self()
	var names []string
	n := len(cv.Members)
	for _, id := range cv.Members {
		if id == self.ID {
			continue
		}
		if u, ok := app.User(id); ok && u.Name != "" {
			names = append(names, u.Name)
		}
	}
	if n == 0 {
		hs := strings.TrimPrefix(cv.Name, "mpdm-")
		if i := strings.LastIndexByte(hs, '-'); i > 0 && strings.Trim(hs[i+1:], "0123456789") == "" {
			hs = hs[:i]
		}
		for _, h := range strings.Split(hs, "--") {
			n++
			if h != self.Handle {
				names = append(names, h)
			}
		}
	}
	sort.Slice(names, func(i, j int) bool { return strings.ToLower(names[i]) < strings.ToLower(names[j]) })
	return n, strings.Join(names, ", ")
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
	end := left // where the title column ends
	if sd.Size.X > 0 {
		end = left - gap - sd.Size.X
		t := op.Offset(image.Pt(end, (hh-sd.Size.Y)/2)).Push(gtx.Ops)
		call.Add(gtx.Ops)
		t.Pop()
	}
	v.layoutSubtitle(gtx, th, c, px+d.Size.X+dp(gtx, 8), ty+(d.Size.Y-d.Baseline), end-dp(gtx, 8), rootOK)
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

// layoutSubtitle draws the subtitle from x to at most maxX, on the
// title's baseline bl (align-items: baseline). Slack's
// .p-threads_flexpane__header_channel_name: 13px/18px, one line with an
// ellipsis, foreground at 70% (unchanged on hover, no underline, pointer
// cursor), the inline icon 1em with 2px after it. It is a link to the
// root's permalink (getPermalinkForMessage without thread_ts): Clack's
// c/<id>/m/<ts> route, which opens the channel scrolled to the root and
// flashes it, the thread pane staying open on its own channel.
func (v *ThreadView) layoutSubtitle(gtx layout.Context, th *theme.Theme, c *colors, x, bl, maxX int, link bool) {
	v.nameRect = image.Rectangle{}
	sub, ok := v.subtitle()
	if !ok || maxX <= x {
		return
	}
	if link && v.nameBtn.Clicked(gtx) {
		v.h.Navigate("c/" + v.ch + "/m/" + v.ts)
	}
	ns := textStyle{size: 13, col: c.Text2, lineH: 18}
	top := bl - cssBaseline(gtx, ns.size, ns.lineH)
	d := at(gtx, x, top, func(gtx layout.Context) layout.Dimensions {
		gtx.Constraints = layout.Constraints{Max: image.Pt(maxX-x, gtx.Sp(unit.Sp(ns.lineH)))}
		w := func(gtx layout.Context) layout.Dimensions {
			ix := 0
			if sub.Icon != "" {
				is := gtx.Sp(unit.Sp(ns.size))
				at(gtx, 0, (gtx.Sp(unit.Sp(ns.lineH))-is)/2, func(gtx layout.Context) layout.Dimensions {
					return icons.Get(icons.Slack(sub.Icon))(gtx, unit.Dp(float32(is)/gtx.Metric.PxPerDp), ns.col)
				})
				ix = is + dp(gtx, 2)
			}
			g := gtx
			g.Constraints.Max.X = max(0, gtx.Constraints.Max.X-ix)
			td := at(g, ix, 0, func(gtx layout.Context) layout.Dimensions {
				return lineBox(gtx, ns, func(gtx layout.Context) layout.Dimensions { return label(gtx, th, ns, sub.Text) })
			})
			sz := image.Pt(ix+td.Size.X, gtx.Sp(unit.Sp(ns.lineH)))
			if link {
				defer clip.Rect{Max: sz}.Push(gtx.Ops).Pop()
				pointer.CursorPointer.Add(gtx.Ops)
			}
			return layout.Dimensions{Size: sz}
		}
		var d layout.Dimensions
		if link {
			d = v.nameBtn.Layout(gtx, w)
		} else {
			d = w(gtx)
		}
		if sub.Tip != "" {
			popups.Tooltip(gtx, th, &v.nameTip, v.nameBtn.Hovered(), d.Size, popups.Above, sub.Tip, "")
		}
		return d
	})
	sub.Link = link
	v.nameRect, v.nameSub = image.Rectangle{Min: image.Pt(x, top), Max: image.Pt(x, top).Add(d.Size)}, sub
}

// HeaderName is the header's subtitle as drawn last frame and where it
// is in window coordinates; ok is false when there is none.
func (v *ThreadView) HeaderName() (sub HeaderSubtitle, r image.Rectangle, ok bool) {
	if v.nameRect.Empty() {
		return HeaderSubtitle{}, image.Rectangle{}, false
	}
	return v.nameSub, v.nameRect.Add(v.Origin), true
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
