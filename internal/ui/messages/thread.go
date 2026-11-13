package messages

import (
	"bytes"
	"context"
	_ "embed"
	"image"
	"image/draw"
	"image/gif"
	"sort"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"gioui.org/f32"
	"gioui.org/io/pointer"
	"gioui.org/layout"
	"gioui.org/op"
	"gioui.org/op/clip"
	"gioui.org/op/paint"
	"gioui.org/unit"
	"gioui.org/widget"

	"github.com/some-du6e/clack/internal/core"
	"github.com/some-du6e/clack/internal/i18n"

	"github.com/some-du6e/clack/internal/ui/icons"
	"github.com/some-du6e/clack/internal/ui/popups"
	"github.com/some-du6e/clack/internal/ui/slackc"
	"github.com/some-du6e/clack/internal/ui/theme"
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
	backBtn                   widget.Clickable // FlexpaneBackButton (PaneBack)
	bellTip, moreTip, nameTip popups.TipState
	closeTip                  popups.TipState
	banner                    threadBanner           // jump to unread (thread_banner.go)
	nameRect                  image.Rectangle        // the subtitle, view coordinates (last frame)
	nameSub                   HeaderSubtitle         // what it showed
	resolving                 bool                   // the conversation is being looked up for the subtitle
	follow                    *bool                  // the bell's state clicked, until the root message has it
	followAt                  time.Time              // when follow was set
	failed                    atomic.Pointer[string] // the subscribe request's error (set off the UI goroutine)
	find                      threadFind             // find in thread (thread_find.go)
	// announced is what Slack's announcer API would have said (a live
	// region; Clack has no screen-reader bridge yet): tests read it.
	announced []string
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

// NewestRowBottom is the window y of the newest reply's (or, with none,
// the root's) bottom edge as of the last frame, false when it wasn't on
// screen.
func (v *ThreadView) NewestRowBottom() (int, bool) { return v.s.newestBottom() }

// NewThreadView returns the thread pane for the thread rooted at ts.
func NewThreadView(h Host, ch, ts string) *ThreadView { return NewThreadViewIn(h, ch, ts, "") }

// NewThreadViewIn is NewThreadView in view container pane ("split":
// Slack's split view), with a list state of its own (stateIn).
func NewThreadViewIn(h Host, ch, ts, pane string) *ThreadView {
	v := &ThreadView{h: h, ch: ch, ts: ts, s: stateIn(h, ch, ts, pane)}
	v.s.closeEdits()
	v.Reopen()
	v.s.hb.ensureLoaded(v.s.m)
	return v
}

// Reopen is the pane showing the thread again (Slack mounts a new
// ThreadsFlexpane): the "New" marker is decided afresh from the
// subscription's lastRead, and the reading position is kept, as Slack
// restores the scrollmark it saved on close (maybeSaveThreadScrollmarks),
// except in a public channel the viewer isn't in, where it saves none
// and the pane opens at its start position again.
func (v *ThreadView) Reopen() {
	v.closeFind(false) // ThreadSearchBar unmounts with the pane
	v.s.thr = threadRead{}
	v.s.snap.rev = 0 // rebuild the rows with the new marker
	if app := v.h.Core(); app != nil {
		if cv, ok := app.Conv(v.ch); ok && cv.Kind == "channel" && cv.Preview {
			v.s.positioned = false
		}
	}
}

// Esc is the pane's escape handler (ThreadsFlexpane onEsc): with
// something to mark (shouldMarkThreadAsRead) it marks the thread read,
// drops the "New" marker and reports true; false lets the secondary
// view's Escape close the pane.
func (v *ThreadView) Esc() bool {
	if !v.s.shouldMarkThreadRead() {
		return false
	}
	v.s.markThreadRead(v.h)
	v.h.Invalidate()
	return true
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
	// find in thread: a search's answer, then the rows' highlight words
	// and current result (thread_find.go)
	v.findTick(g)
	v.s.findWords, v.s.findTS = v.find.words, v.findCurrent()
	shown(&lastThread, v.s)
	v.s.layoutList(g, v.h, v.Origin.Add(image.Pt(0, top)))
	v.layoutBanner(g, g.Constraints.Max) // "N new replies" (thread_banner.go)
	if v.find.active {
		v.layoutFindBar(g, th, c, size.X) // over p-threads_flexpane__content's top right
		v.find.bar = v.find.bar.Add(image.Pt(0, top))
	}
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
// (ThreadFlexpaneAdminTools): plugin siblings show.
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

// threadExperiment reports whether one of the thread pane's Slack
// experiments is on for the account (core.App.Experiment; its defaults
// are the account's, docs/THREADS-AUDIT.md).
func (v *ThreadView) threadExperiment(name string) bool {
	app := v.h.Core()
	return app != nil && app.Experiment(name) == "on"
}

// bellShown is the flexpane header's eX (client-boot-shared XVla): the
// bell only under thread_header_mute_button (off for the account: Follow
// is in the ⋮ menu) once the root is loaded, then module 5mp4's null
// cases (ojBT returns null for a channel the viewer isn't in unless they
// already follow the thread).
func (v *ThreadView) bellShown(m core.Message, ok bool) bool {
	if !ok || v.h.Core() == nil || !v.threadExperiment("thread_header_mute_button") {
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
	// .p-flexpane__title_container: 18px/24px Black, the subtitle 8px
	// after it (margin 0 8px); p-flexpane_header--smaller
	// (smaller_secondary_headers) makes the title 15px/22px Black and the
	// controls 4px apart (gap 4 over --gap's 6).
	smaller := v.h.Core() != nil && v.h.Core().SmallerSecondaryHeaders() // kl02 I$: only under the nested view
	tsT := textStyle{size: 18, weight: theme.Black, col: c.Text, lineH: 24}
	gap := dp(gtx, 6)
	if smaller {
		tsT.size, tsT.lineH, gap = 15, 22, dp(gtx, 4)
	}
	// FlexpaneBackButton while the view container has a back stack (the
	// split view's view.prev): 28px, margin-right 6 plus the header's gap.
	pb, back := v.h.(PaneBack)
	back = back && pb.PaneBack()
	if back {
		px += dp(gtx, 28) + dp(gtx, 6) + gap
		if v.backBtn.Clicked(gtx) {
			pb.GoPaneBack()
		}
	}
	ty := (hh - gtx.Sp(unit.Sp(tsT.lineH))) / 2
	d := at(gtx, px, ty, func(gtx layout.Context) layout.Dimensions {
		return lineBox(gtx, tsT, func(gtx layout.Context) layout.Dimensions { return label(gtx, th, tsT, "Thread") })
	})
	// header controls, right to left gap apart: close, ⋮
	// (SecondaryHeaderMore), the bell (thread-mute-button), then plugin
	// content in ThreadSearchButton. Slack's viewHeaderActionButton is
	// 28×28, radius 8, the close 12px from the edge.
	bs := dp(gtx, 28)
	cx := w - dp(gtx, 12) - bs
	root, rootOK := v.root()
	sub := v.subscribed(root)
	bell := v.bellShown(root, rootOK)
	mx := cx - gap - bs
	left := mx
	if bell {
		left = mx - gap - bs
	}
	// a ThreadSearchButton patch's items are as wide as they are, in the
	// header row's height, in what the title leaves them (Slack's title
	// column shrinks first)
	bgtx := gtx
	bgtx.Constraints = layout.Constraints{Max: image.Pt(max(left-gap-px-d.Size.X, 0), hh)}
	rec := op.Record(gtx.Ops)
	hs := flexPrimaryClass.Enter(bgtx, 0, 0)
	sd := slackc.Component(bgtx, "ThreadSearchButton", v, (*ThreadView).searchButtonProps, (*ThreadView).layoutSearchButton)
	hs.Leave()
	call := rec.Stop()
	end := left // where the title column ends
	v.find.btnRect = image.Rectangle{}
	if sd.Size.X > 0 {
		end = left - gap - sd.Size.X
		if !slackc.Patched("ThreadSearchButton") { // the native's own Search in thread
			v.find.btnRect = image.Rect(end, (hh-sd.Size.Y)/2, end+sd.Size.X, (hh+sd.Size.Y)/2)
		}
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
	// tooltip delays: the bell's 300, More actions' BaseTooltip default
	// 150, Close 300 (the header has additional buttons)
	v.bellTip.Delay, v.moreTip.Delay, v.closeTip.Delay = 300*time.Millisecond, 150*time.Millisecond, 300*time.Millisecond
	if back { // no tooltip: Slack's FlexpaneBackButton only has its aria-label "Back"
		at(gtx, dp(gtx, 16), (hh-bs)/2, func(gtx layout.Context) layout.Dimensions {
			gtx.Constraints = layout.Exact(image.Pt(bs, bs))
			return v.backBtn.Layout(gtx, func(gtx layout.Context) layout.Dimensions {
				if v.backBtn.Hovered() {
					rrect(gtx.Ops, image.Rectangle{Max: image.Pt(bs, bs)}, dp(gtx, 8), st.Background(c.CloseHover))
				}
				is := dp(gtx, 20)
				at(gtx, (bs-is)/2, (bs-is)/2, func(gtx layout.Context) layout.Dimensions {
					return icons.Get("caret-left-full")(gtx, unit.Dp(20), st.Color(c.Text3))
				})
				return layout.Dimensions{Size: image.Pt(bs, bs)}
			})
		})
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
		var d layout.Dimensions
		if st.Active() {
			d = st.Wrap(gtx, func(gtx layout.Context) layout.Dimensions { return v.closeButton(gtx, c, st, bs) })
		} else {
			d = v.closeButton(gtx, c, st, bs)
		}
		// Flexpane's close: tip "Close", position bottom-right
		popups.Tooltip(gtx, th, &v.closeTip, v.s.closeBtn.Hovered(), d.Size, popups.BelowRight, i18n.T("flexpane", "Close"), "")
		return d
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
			// a channel is Slack's InlineChannelEntity (className
			// p-threads_flexpane__header_channel_name, truncate): its icon
			// and name are the elements plugins' rules see (StreamerMode
			// blurs a private channel's name)
			var nst slackc.Styled
			if sub.Icon != "" {
				ent := thrHeaderEntity.Enter(gtx, btnState(gtx, &v.nameBtn), v.ch, sub.Icon)
				defer ent.Leave()
				if ent.Hidden() {
					return layout.Dimensions{}
				}
				nst = thrHeaderEntity.Name(ent)
			}
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
				name := func(gtx layout.Context) layout.Dimensions {
					return lineBox(gtx, ns, func(gtx layout.Context) layout.Dimensions { return label(gtx, th, ns, sub.Text) })
				}
				if nst.Active() {
					return nst.Wrap(gtx, name)
				}
				return name(gtx)
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

// ---- before the replies arrive (ThreadsFlexpane, client-boot-shared puxw) ----

// threadPending draws the pane while it has no messages yet, as
// ThreadsFlexpane does (CDN build 4d0c78085232):
//
//   - isLoading (no timestamps yet): FlexpaneLoading (usav) with the
//     loadingMessage "Loading thread…" (the threads I18n): .p-flexpane_loading,
//     absolute 200px from the pane's top, centred, 13px in
//     rgba(sk_foreground_high, .5), Slack's loading_hash_animation (40px,
//     margin 0 16px 8px 8px; the _dark one in the dark theme), a line
//     break, then the message. The list's only row is the input, so the
//     composer sits under the header (ContentHeight).
//   - a failed fetchReplies (isThreadErrored): ThreadsError, the pane's
//     only content (no list, no composer): .p-threads_flexpane__error, a
//     column centred in the pane: :mag: at 64px (margin-bottom 16),
//     "Couldn’t load thread" (22px/1.36365 Black, margin-bottom 4), "If
//     the problem persists, please drop us a line." (15px/1.46668,
//     margin-bottom 16; the link is https://slack.com/help/contact) and a
//     primary "Try Again" (tryAgain: fetch again). thread_not_found
//     (deletedThreadKey) says "This message was deleted." (the message
//     I18n) under the :mag: and nothing else.
//
// False leaves the pane to the list (signed out: its sign-in prompt).
func (s *listState) threadPending(gtx layout.Context, h Host, c *colors, size image.Point) bool {
	if s.snap.err != nil && !s.snap.loading {
		if app := h.Core(); app != nil && app.SignedOut() {
			return false
		}
		s.threadsError(gtx, h, c, size, threadNotFound(s.snap.err))
		return true
	}
	s.flexpaneLoading(gtx, h, c, size)
	return true
}

// threadNotFound: fetchReplies failed with thread_not_found (the root is
// gone).
func threadNotFound(err error) bool {
	return err != nil && strings.Contains(err.Error(), "thread_not_found")
}

// threadErrored is ThreadsFlexpane's isThreadErrored with nothing loaded:
// ThreadsError is all the pane shows.
func (s *listState) threadErrored(h Host) bool {
	if !s.thread || s.snap.loaded || len(s.snap.msgs) > 0 || s.snap.err == nil || s.snap.loading {
		return false
	}
	app := h.Core()
	return app == nil || !app.SignedOut()
}

// threadLoading is isLoading: the pane has no messages yet and no error.
func (s *listState) threadLoading() bool {
	return s.thread && !s.snap.loaded && len(s.snap.msgs) == 0 && (s.snap.err == nil || s.snap.loading)
}

// HidesComposer: ThreadsError replaces the whole pane, its reply input
// included.
func (v *ThreadView) HidesComposer() bool { return v.s.threadErrored(v.h) }

// flexpaneLoading is FlexpaneLoading with "Loading thread…".
func (s *listState) flexpaneLoading(gtx layout.Context, h Host, c *colors, size image.Point) {
	th := h.Theme()
	if s.pendAt.IsZero() {
		s.pendAt = gtx.Now
	}
	col := c.Muted70 // sk_foreground_high (Muted70's rgb) at .5
	col.A = 128
	ts := textStyle{size: 13, col: col, lineH: 19}
	msg := threadsNS.T("Loading thread…")
	// the line box of the 40px image: its bottom margin edge (8px) on the
	// baseline of a 13px line
	sp := dp(gtx, 40)
	top := dp(gtx, 200)
	inline := dp(gtx, 8) + sp + dp(gtx, 16)
	x := (size.X-inline)/2 + dp(gtx, 8)
	m := measure(gtx, th, ts, msg)
	desc := dp(gtx, 6) // the image's line box below its baseline (Slack's markup: the text's ink 17px under the image)
	hashSpinner(gtx, th.Dark, image.Rect(x, top, x+sp, top+sp), s.pendAt)
	y := top + sp + dp(gtx, 8) + desc
	at(gtx, (size.X-m.X)/2, y, func(gtx layout.Context) layout.Dimensions { return label(gtx, th, ts, msg) })
}

// threadsError is ThreadsError (puxw).
func (s *listState) threadsError(gtx layout.Context, h Host, c *colors, size image.Point, deleted bool) {
	th := h.Theme()
	if s.retryBtn.Clicked(gtx) {
		s.hb.retry(s.m) // tryAgain: the pane fetches again
	}
	if s.helpLink.Clicked(gtx) {
		openURL("https://slack.com/help/contact")
	}
	es := dp(gtx, 64)
	head := textStyle{size: 22, weight: theme.Black, col: c.Text, lineH: 30}
	body := textStyle{size: 15, col: c.Text, lineH: 22}
	link := body
	link.col = c.Link
	var hdr string
	if deleted {
		hdr = msgNS.T("This message was deleted.")
	} else {
		hdr = threadsNS.T("Couldn’t load thread")
	}
	var parts []i18n.Part
	btnH := 0
	h1 := es + dp(gtx, 16) + dp(gtx, 30)
	if !deleted {
		parts = threadsNS.RT("If the problem persists, please <a>drop us a line.</a>")
		btnH = dp(gtx, 36)
		h1 += dp(gtx, 4) + dp(gtx, 22) + dp(gtx, 16) + btnH
	}
	y := max(0, (size.Y-h1)/2)
	// :mag:
	ex := (size.X - es) / 2
	if app := h.Core(); app != nil {
		url, txt, _ := app.Emoji("mag")
		if !drawEmoji(gtx, h, url, image.Rect(ex, y, ex+es, y+es)) && txt != "" {
			es := textStyle{size: 48, col: c.Text, lineH: 64}
			w := measure(gtx, th, es, txt).X
			at(gtx, (size.X-w)/2, y, func(gtx layout.Context) layout.Dimensions { return label(gtx, th, es, txt) })
		}
	}
	y += es + dp(gtx, 16)
	hw := measure(gtx, th, head, hdr).X
	at(gtx, (size.X-hw)/2, y, func(gtx layout.Context) layout.Dimensions { return label(gtx, th, head, hdr) })
	y += dp(gtx, 30)
	if deleted {
		return
	}
	y += dp(gtx, 4)
	// the body: its text, the link (Link, underlined under the pointer)
	w := 0
	for _, p := range parts {
		st := body
		if p.Tag == "a" {
			st = link
		}
		w += measure(gtx, th, st, p.Text).X
	}
	x := (size.X - w) / 2
	for _, p := range parts {
		if p.Tag != "a" {
			d := at(gtx, x, y, func(gtx layout.Context) layout.Dimensions { return label(gtx, th, body, p.Text) })
			x += d.Size.X
			continue
		}
		d := at(gtx, x, y, func(gtx layout.Context) layout.Dimensions {
			return s.helpLink.Layout(gtx, func(gtx layout.Context) layout.Dimensions {
				d := label(gtx, th, link, p.Text)
				if s.helpLink.Hovered() {
					base := d.Size.Y - d.Baseline
					fill(gtx.Ops, image.Rect(0, base+dp(gtx, 1), d.Size.X, base+dp(gtx, 2)), c.Link)
				}
				return d
			})
		})
		x += d.Size.X
	}
	y += dp(gtx, 22) + dp(gtx, 16)
	// Try Again: a primary Button (R2sM), medium
	rec := op.Record(gtx.Ops)
	gtx.Constraints.Min = image.Point{} // the button's own width, not the pane's
	bd := popups.Button(gtx, th, &s.retryBtn, popups.BtnPrimary, popups.BtnMedium, threadsNS.T("Try Again"), "", 0)
	call := rec.Stop()
	t := op.Offset(image.Pt((size.X-bd.Size.X)/2, y)).Push(gtx.Ops)
	call.Add(gtx.Ops)
	t.Pop()
}

// loadingRepliesRow is getThreadRowsAndKeys' "loading" row while the
// replies load after some are known (reachedEnd false): "Loading
// replies…" (the threads I18n) in .p-threads_flexpane__loading, 96px
// tall, centred, 13px italic in sk_foreground_high_solid.
func (s *listState) loadingRepliesRow(gtx layout.Context, h Host, c *colors, w int) layout.Dimensions {
	th := h.Theme()
	col := c.Edited
	if th.Dark {
		col = rgb(0x818385)
	}
	ts := textStyle{size: 13, italic: true, col: col, lineH: 96}
	msg := threadsNS.T("Loading replies…")
	m := measure(gtx, th, ts, msg)
	at(gtx, (w-m.X)/2, 0, func(gtx layout.Context) layout.Dimensions { return label(gtx, th, ts, msg) })
	return layout.Dimensions{Size: image.Pt(w, dp(gtx, 96))}
}

// Slack's loading_hash_animation@2x (light and dark), the image
// FlexpaneLoading shows: 80px frames for a 40px box, 30ms each.
var (
	//go:embed assets/loading_hash_animation_2x.gif
	hashGIFLight []byte
	//go:embed assets/loading_hash_animation_2x_dark.gif
	hashGIFDark []byte
)

// hashAnim is the decoded animation, composed frame by frame (each frame
// draws over the last: disposal "none").
type hashAnim struct {
	once   sync.Once
	frames []paint.ImageOp
	ends   []time.Duration
	total  time.Duration
}

var hashAnims [2]hashAnim // light, dark

func (a *hashAnim) load(raw []byte) {
	a.once.Do(func() {
		g, err := gif.DecodeAll(bytes.NewReader(raw))
		if err != nil || len(g.Image) == 0 {
			return
		}
		bounds := image.Rect(0, 0, g.Config.Width, g.Config.Height)
		canvas := image.NewRGBA(bounds)
		for i, f := range g.Image {
			var prev *image.RGBA
			disp := byte(0)
			if i < len(g.Disposal) {
				disp = g.Disposal[i]
			}
			if disp == gif.DisposalPrevious {
				prev = image.NewRGBA(bounds)
				draw.Draw(prev, bounds, canvas, image.Point{}, draw.Src)
			}
			draw.Draw(canvas, f.Bounds(), f, f.Bounds().Min, draw.Over)
			out := image.NewRGBA(bounds)
			draw.Draw(out, bounds, canvas, image.Point{}, draw.Src)
			a.frames = append(a.frames, paint.NewImageOp(out))
			d := 30 * time.Millisecond
			if i < len(g.Delay) && g.Delay[i] > 0 {
				d = time.Duration(g.Delay[i]) * 10 * time.Millisecond
			}
			a.total += d
			a.ends = append(a.ends, a.total)
			switch disp {
			case gif.DisposalBackground:
				draw.Draw(canvas, f.Bounds(), image.Transparent, image.Point{}, draw.Src)
			case gif.DisposalPrevious:
				canvas = prev
			}
		}
	})
}

// hashSpinner draws the animation's frame for now in r, from its start,
// and asks for the frame after it (an InvalidateCmd deadline, so nothing
// redraws once the pane stops drawing it).
func hashSpinner(gtx layout.Context, dark bool, r image.Rectangle, start time.Time) {
	a, raw := &hashAnims[0], hashGIFLight
	if dark {
		a, raw = &hashAnims[1], hashGIFDark
	}
	a.load(raw)
	if len(a.frames) == 0 || a.total <= 0 {
		return
	}
	now := gtx.Now
	if now.IsZero() {
		now = time.Now()
	}
	el := now.Sub(start) % a.total
	if el < 0 {
		el = 0
	}
	i := sort.Search(len(a.ends), func(i int) bool { return a.ends[i] > el })
	i = min(i, len(a.frames)-1)
	img := a.frames[i]
	sz := img.Size()
	t := op.Offset(r.Min).Push(gtx.Ops)
	sc := op.Affine(f32.Affine2D{}.Scale(f32.Point{}, f32.Pt(float32(r.Dx())/float32(sz.X), float32(r.Dy())/float32(sz.Y)))).Push(gtx.Ops)
	img.Filter = paint.FilterLinear
	img.Add(gtx.Ops)
	cl := clip.Rect(image.Rectangle{Max: sz}).Push(gtx.Ops)
	paint.PaintOp{}.Add(gtx.Ops)
	cl.Pop()
	sc.Pop()
	t.Pop()
	gtx.Execute(op.InvalidateCmd{At: start.Add(now.Sub(start) - el + a.ends[i])})
}

// ---- Mod+J ----

// JumpToUnread is ThreadsFlexpane's onKeyDown for j with the desktop
// modifier (isDesktopApp, isCmdKey: ⌘ on macOS, Ctrl elsewhere) while
// the key focus is in the pane and no overlay is up: jumpToUnread scrolls
// firstUnreadTs's row to the list's top (scrollToKey, offset 0); without
// one it announces "No unread messages" (the threads I18n). Reports
// whether there was a reply to jump to.
func (v *ThreadView) JumpToUnread() bool {
	ts := v.s.firstUnreadTs()
	if ts == "" {
		v.announced = append(v.announced, threadsNS.T("No unread messages"))
		return false
	}
	if v.s.scrollToFind(ts, 0) {
		v.h.Invalidate()
	}
	return true
}

// Announced is what the pane's announcer said (Slack's live region;
// Clack has no screen-reader bridge yet), oldest first.
func (v *ThreadView) Announced() []string { return append([]string(nil), v.announced...) }

// firstUnreadTs is ThreadsFlexpane's: the first of the thread's
// timestamps after originalLastRead (the lastRead the pane opened with)
// that isn't the root, else originalLastRead itself; "" when the pane
// opened on a thread it doesn't follow.
func (s *listState) firstUnreadTs() string {
	lr := s.thr.open
	if lr == "" {
		return ""
	}
	for i := range s.snap.msgs {
		if ts := s.snap.msgs[i].m.TS; ts != s.ts && tsLess(lr, ts) {
			return ts
		}
	}
	return lr
}
