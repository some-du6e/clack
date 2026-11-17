package views

// The Threads view's order and loads as Slack's ThreadsView keeps them
// (client-threads-view 9Ra5, boot-render VxbD, client-boot 181D): the
// threads a socket event names (the only ones a refresh puts on top),
// initialLastReadByThread (what hides the "You’re up to date" divider),
// the order a new refreshId gives (a mount, reloadThreads), a card's
// replies across a refresh, and a mount load's floating loader.

import (
	"context"
	"image"
	"image/color"
	"sort"

	"gioui.org/op"
	"gioui.org/unit"

	"github.com/some-du6e/clack/internal/core"
	"github.com/some-du6e/clack/internal/slack"
	"github.com/some-du6e/clack/internal/ui/popups"
	"github.com/some-du6e/clack/internal/ui/slackc/css"
	"github.com/some-du6e/clack/internal/ui/theme"
)

// noteNamed records the thread a new reply or thread_subscribed names
// (the event goroutine). Slack's messageReceived (a message with a
// thread_ts) and threadSubscribed call maybeUpdateAllThreadsOnNewMessage
// for that one thread, which fetches a followed thread the list doesn't
// have and prepends it: no other thread ever comes in on top.
func (v *Threads) noteNamed(ev core.Event) {
	if ev.Channel == "" || ev.ThreadTS == "" {
		return
	}
	if !(ev.Type == "message" && ev.Reply) && !(ev.Type == "thread_subscription" && ev.Name == "follow") {
		return
	}
	v.mu.Lock()
	if v.named == nil {
		v.named = map[string]int{}
	}
	v.named[ev.Channel+"/"+ev.ThreadTS] = 0
	v.mu.Unlock()
}

// thrNamedTries is how many refreshes a named thread goes into before it
// is forgotten: a reply in a thread the viewer doesn't follow never comes
// in, and one the server hadn't indexed yet comes with the next refresh
// or the one after (they are 5 s apart).
const thrNamedTries = 3

// namedNow is what a refresh may put on top, copied for its loader
// goroutine (nil for the other loads); each goes into one more refresh.
func (v *Threads) namedNow(mode actLoad) map[string]bool {
	if mode != actRefresh {
		return nil
	}
	v.mu.Lock()
	defer v.mu.Unlock()
	out := make(map[string]bool, len(v.named))
	for k := range v.named {
		out[k] = true
		v.named[k]++
	}
	return out
}

// landNamed forgets the named threads the list has once a load is in, and
// those that went into thrNamedTries refreshes without coming in. The
// others wait for the next refresh: a reply the server's answer didn't
// have yet comes with a later one.
func (v *Threads) landNamed(page thrData) {
	v.mu.Lock()
	defer v.mu.Unlock()
	if len(v.named) == 0 {
		return
	}
	for i := range page.items {
		delete(v.named, page.items[i].key)
	}
	for k, n := range v.named {
		if n >= thrNamedTries {
			delete(v.named, k)
		}
	}
}

// sortingLoad is a load that sorts the list going out: its answer has
// every reply and named thread before it, so the view's staleness and
// names so far go. What comes while it is out stays for the refresh
// after it.
func (v *Threads) sortingLoad() {
	v.w.clear()
	v.mu.Lock()
	clear(v.named)
	v.mu.Unlock()
}

// thrLoadCtx is a load's context. A load that sorts the list is the
// viewer's doing (the first visit, Try Again, a revisit, the row clicked
// again) and Slack's loadThreads sends each, so it skips the client's
// backoff of a request that just failed the same way (slack.NoBackoff),
// which would answer a Try Again right after the failure with that failure
// and send nothing.
func thrLoadCtx(ctx context.Context, mode actLoad) context.Context {
	if mode == actFresh {
		return slack.NoBackoff(ctx)
	}
	return ctx
}

// noteEntries is initialLastReadByThread (getDerivedStateFromProps): the
// last read each thread had when it first came into the list, kept for a
// thread unread then (isThreadUnread) and "" for one read then, as
// Slack's null, or never read, which Slack's rule skips too. It starts
// afresh with each load that sorts the list.
func (v *Threads) noteEntries(page thrData) {
	if v.initial == nil || v.initLoad != page.load {
		v.initial, v.initLoad = map[string]string{}, page.load
	}
	for i := range page.items {
		t := &page.items[i]
		if _, seen := v.initial[t.key]; seen {
			continue
		}
		lr := ""
		if v.unread(t) > 0 {
			lr = t.Root.ThreadLastRead
			if r, ok := v.read[t.key]; ok && actTSLess(lr, r) {
				lr = r
			}
		}
		v.initial[t.key] = lr
	}
}

// resort is getDerivedStateFromProps on a new refreshId (a mount, or
// reloadThreads, which drops currentRefreshId before it loads): the
// threads the view holds, followed ones only (its "subscribed" filter;
// truncateThreads drops the others when the view unmounts), sorted by
// sortThreads (8orQ) on the view's own read state (isThreadUnread reads
// the subscription store, the view's marks in it), then newest activity,
// with a new load number and divider thread, until the reload's answer
// replaces them.
func (v *Threads) resort() {
	app := v.h.Core()
	v.loads++
	load := v.loads
	v.data.mutate(func(d *thrData) {
		items := make([]thrItem, 0, len(d.items)) // a load still out reads the old ones
		for _, t := range d.items {
			if app != nil {
				if sub, ok := app.S.ThreadSub(t.Channel, t.Root.TS); ok && !sub.Subscribed {
					continue
				}
			}
			items = append(items, t)
		}
		sort.SliceStable(items, func(i, j int) bool {
			if ui, uj := v.unread(&items[i]) > 0, v.unread(&items[j]) > 0; ui != uj {
				return ui
			}
			return actTSLess(items[j].sortTS(), items[i].sortTS())
		})
		d.items, d.load, d.readKey = items, load, ""
		for i := range items {
			if v.unread(&items[i]) == 0 {
				d.readKey = items[i].key
				break
			}
		}
	})
}

// thrRefreshed is a listed thread with a refresh's copy of it, now: its
// root, counts and unread state, and its replies with the ones the card
// had before the page's window (getView's latest or unread replies,
// which a new reply moves on). Slack's card draws the thread's messages
// from a first visible one fixed per refreshId (minimumVisibleTsByThread),
// so a realtime reply only adds a row. With the window alone the oldest
// reply dropped out, and with older replies loaded that left a gap no row
// could bring back. A reply the card had inside the window that the page
// lacks is gone (deleted), so only older ones stay.
func thrRefreshed(had, now thrItem) thrItem {
	if len(had.Replies) == 0 || len(now.Replies) == 0 {
		return now
	}
	first := now.Replies[0].TS
	reps := make([]core.Message, 0, len(had.Replies)+len(now.Replies))
	for _, m := range had.Replies {
		if actTSLess(m.TS, first) {
			reps = append(reps, m)
		}
	}
	if len(reps) == 0 {
		return now
	}
	reps = append(reps, now.Replies...)
	sort.SliceStable(reps, func(i, j int) bool { return actTSLess(reps[i].TS, reps[j].TS) })
	now.Replies = reps
	return now
}

// floatingLoader is get_rows_and_keys' "global-loading" row, the list's
// first while a mount's load is out (isDoingGlobalRefresh): a
// div.p-threads_view__floating_loader around InfiniteSpinner (s6So) at
// size medium (16px), noMargin and floating. The .floating pill is the
// spinner's own box (inline-flex) on base-pry with a 1px otl-ter outline,
// round, box-shadow 0 4px 6px -4px and 0 10px 15px -3px #00000019, and
// .floatingPositioned sets it 8px down the row (absolute, top 8px, left
// 50%, translate -50%) over the first thread, so the row takes no
// height. y is the row's top in the list, w the list's width.
func (v *Threads) floatingLoader(gtx C, w, y int) {
	s := gtx.Dp(16)
	x, top := (w-s)/2, y+gtx.Dp(8)
	r := image.Rect(x, top, x+s, top+s)
	sh := argb(0x19000000)
	for _, b := range []struct{ dy, blur, spread unit.Dp }{{4, 6, -4}, {10, 15, -3}} {
		sr := r.Add(image.Pt(0, gtx.Dp(b.dy))).Inset(gtx.Dp(-b.spread))
		rad := sr.Dx() / 2
		rrect(gtx, sr, rad, sh)
		theme.Shadow(gtx, sr, rad, gtx.Dp(b.blur)/2, sh)
	}
	rrect(gtx, r, s/2, v.thrTok("base-pry"))
	o := max(1, gtx.Dp(1))
	border(gtx, r.Inset(-o), s/2+o, o, v.thrTok("otl-ter"))
	defer op.Offset(r.Min).Push(gtx.Ops).Pop()
	popups.InfiniteSpinner(gtx, s, true, v.thrTok("content-ter"), v.thrTok("content-hgl-1"))
}

// thrTok is Slack's --dt_color-<name> in the view's mode, a stylesheet
// theme's own value when it sets one.
func (v *Threads) thrTok(name string) color.NRGBA {
	c, _ := css.ParseColor(css.SlackVars(v.th.Dark)["--dt_color-"+name])
	if v.th.Tokens != nil {
		theme.SetColor(v.th.Tokens, &c, "var(--dt_color-"+name+")")
	}
	return c
}

// Settled reports that a load answered and none is out (tests).
func (v *Threads) Settled() bool {
	_, ok, _ := v.data.get()
	return ok && !v.data.busy()
}
