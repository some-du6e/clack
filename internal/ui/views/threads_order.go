package views

// The Threads view's order and loads as Slack's ThreadsView keeps them
// (client-threads-view 9Ra5, boot-render VxbD, client-boot 181D, boot-shared
// k0ZD): the threads a socket event names (the only ones a refresh puts on
// top), initialLastReadByThread (what hides the "You’re up to date"
// divider) and how a new reply or a mark unread moves it
// (maybeMarkNewMessageOnUpdate, getDerivedStateFromProps), the order a new
// refreshId gives (a mount, reloadThreads, a slow reconnect), a card's
// replies across a refresh, the threads mapStateToProps leaves out, and a
// mount load's floating loader.

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
		v.named = map[string]uint64{}
	}
	v.namedSeq++
	v.named[ev.Channel+"/"+ev.ThreadTS] = v.namedSeq
	if len(v.named) > thrNamedMax { // the oldest goes
		old, at := "", uint64(0)
		for k, n := range v.named {
			if old == "" || n < at {
				old, at = k, n
			}
		}
		delete(v.named, old)
	}
	v.mu.Unlock()
}

// thrNamedMax caps the named threads. Each waits for a refresh that brings
// it, as Slack's fetched thread waits in the store until the viewer has
// unread threads (getDerivedStateFromProps prepends it then), or for the
// next load that sorts the list; one that never comes (a reply in a
// thread the viewer doesn't follow) only costs its place.
const thrNamedMax = 200

// namedNow is what a refresh may put on top, copied for its loader
// goroutine (nil for the other loads).
func (v *Threads) namedNow(mode actLoad) map[string]bool {
	if mode != actRefresh {
		return nil
	}
	v.mu.Lock()
	defer v.mu.Unlock()
	out := make(map[string]bool, len(v.named))
	for k := range v.named {
		out[k] = true
	}
	return out
}

// landed is a load's answer in (settle): the named threads it listed go,
// the threads whose newest reply it moved on wait for the frame that lays
// them out (markNewReplies), and older replies a card loaded that the
// answer's window now has go from the card (the window moved back over
// them: a reply deleted) so none is drawn twice.
func (v *Threads) landed(page thrData) {
	v.mu.Lock()
	for i := range page.items {
		delete(v.named, page.items[i].key)
	}
	v.mu.Unlock()
	seen := make(map[string]string, len(page.items))
	for i := range page.items {
		t := &page.items[i]
		cur := t.latest()
		if prev, ok := v.maxSeen[t.key]; ok && actTSLess(prev, cur) {
			if v.grew == nil {
				v.grew = map[string]string{}
			}
			if _, had := v.grew[t.key]; !had {
				v.grew[t.key] = prev
			}
		}
		seen[t.key] = cur
		if c := v.cards[t.key]; c != nil && len(c.older) > 0 && len(t.Replies) > 0 {
			n := len(c.older)
			for n > 0 && !actTSLess(c.older[n-1].TS, t.Replies[0].TS) {
				n--
			}
			c.older = c.older[:n]
		}
	}
	v.maxSeen = seen
}

// sortingLoad is a load that sorts the list going out: its answer has
// every reply and named thread before it, so the view's staleness, names
// so far and a reconnect's reload waiting go. What comes while it is out
// stays for the refresh after it.
func (v *Threads) sortingLoad() {
	v.w.clear()
	v.mu.Lock()
	clear(v.named)
	v.reconn = false
	v.mu.Unlock()
}

// reconnectDue is maybeRefreshAllThreads' counter reaching ThreadsView
// (reloadThreads) after a slow connect. Before the view's first answer
// there is nothing to reload (Slack's this.loading); while a load that
// sorts the list or the next page is out the reload waits for it, as that
// answer may predate the connect.
func (v *Threads) reconnectDue() bool {
	v.mu.Lock()
	due := v.reconn
	v.mu.Unlock()
	if !due {
		return false
	}
	_, loaded, _ := v.data.get()
	if loaded && v.data.busy() && v.mode != actRefresh {
		return false
	}
	v.mu.Lock()
	v.reconn = false
	v.mu.Unlock()
	return loaded
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
// afresh with each load that sorts the list. A thread the viewer marked
// unread (threadsManuallyMarkedUnread) takes its last read as it moves,
// so its replies after the mark are new to the divider. A reply the
// viewer didn't see moves it too (markNewReplies).
func (v *Threads) noteEntries(page thrData) {
	if v.initial == nil || v.initLoad != page.load {
		v.initial, v.initLoad = map[string]string{}, page.load
	}
	var marked map[string]bool
	if app := v.h.Core(); app != nil {
		marked = app.ThreadsMarkedUnread()
	}
	for i := range page.items {
		t := &page.items[i]
		if lr, seen := v.initial[t.key]; seen {
			if marked[t.key] && lr != v.lastRead(t) {
				v.initial[t.key] = v.lastRead(t)
			}
			continue
		}
		lr := ""
		if v.unread(t) > 0 {
			lr = v.lastRead(t)
		}
		v.initial[t.key] = lr
	}
}

// lastRead is the thread's last read as the view has it: its own mark or
// thread_marked's (read), else the answer's.
func (v *Threads) lastRead(t *thrItem) string {
	if r, ok := v.read[t.key]; ok {
		return r
	}
	return t.Root.ThreadLastRead
}

// followed is the thread's subscription as the store has it now (Slack's
// subscribed), else as its answer had it.
func (v *Threads) followed(t *thrItem) bool {
	if app := v.h.Core(); app != nil {
		if sub, ok := app.S.ThreadSub(t.Channel, t.Root.TS); ok {
			return sub.Subscribed
		}
	}
	return t.Root.FollowingThread
}

// attention is isUserAttentionOnThreadsView: the window has the focus
// and no overlay is open (a host that can't tell counts as attentive).
func (v *Threads) attention() bool {
	if ah, ok := v.h.(interface{ UserAttention() bool }); ok {
		return ah.UserAttention()
	}
	return true
}

// markNewReplies is maybeMarkNewMessageOnUpdate, after the frame that lays
// out a load's answer (landed's grew): for each thread whose newest reply
// moved on and is unread, not marked unread by the viewer. Seen means its
// newest reply's row is on screen while the viewer's attention is on the
// view. Unseen, a reply from someone else moves the thread's arrival last
// read (initialLastReadByThread) to its last read when it had none or an
// older one, so the divider sees the reply as new. Seen, the thread is
// marked read (setThreadLastRead) while it is followed, when it came in
// read, its last read had every reply before this one, or its "unread"
// row shows. It reports whether anything changed (the next frame draws
// it). Not ported: an unseen reply also counts when the window blurred
// between the two updates (lastWindowBlurEvent).
func (v *Threads) markNewReplies(page thrData) bool {
	if len(v.grew) == 0 {
		return false
	}
	grew := v.grew
	v.grew = nil
	var marked map[string]bool
	if app := v.h.Core(); app != nil {
		marked = app.ThreadsMarkedUnread()
	}
	att, self, changed := v.attention(), v.selfID(), false
	for i := range page.items {
		t := &page.items[i]
		prev, ok := grew[t.key]
		if !ok {
			continue
		}
		cur, lr := t.latest(), v.lastRead(t)
		if !actTSLess(lr, cur) || marked[t.key] {
			continue
		}
		a := v.initial[t.key]
		seen := att && v.kb.inView(t.key+"-"+cur)
		if !seen && (a == "" || actTSLess(a, lr)) && thrAuthor(t, cur) != self {
			v.initial[t.key], changed = lr, true
		}
		if seen && v.followed(t) && (a == "" || !actTSLess(lr, prev) || v.unreadRowSeen(t)) {
			v.markRead(t)
			changed = true
		}
	}
	return changed
}

// thrAuthor is the user of the thread's reply ts ("" when the card doesn't
// have it).
func thrAuthor(t *thrItem, ts string) string {
	for i := len(t.Replies) - 1; i >= 0; i-- {
		if t.Replies[i].TS == ts {
			return t.Replies[i].User
		}
	}
	return ""
}

// unreadRowSeen is the thread's "unread" row on screen: get_rows_and_keys
// puts it before the first reply newer than the thread's arrival last read
// (a followed thread that came in unread), and types it "unread" while
// that is still the thread's last read. Clack's row there is the reply's.
func (v *Threads) unreadRowSeen(t *thrItem) bool {
	a := v.initial[t.key]
	if a == "" || v.lastRead(t) != a {
		return false
	}
	for _, m := range thrShown(t, v.cards[t.key]) {
		if actTSLess(a, m.TS) {
			return v.kb.inView(t.key + "-" + m.TS)
		}
	}
	return false
}

// shownItems is the threads mapStateToProps hands ThreadsView: it skips
// one "in non-member public channel", which Clack's store has as a
// conversation the viewer isn't in (a channel they left: core's
// dropMembership). Such a thread keeps its place in the order and shows
// again if they come back. Not ported: Slack also skips a thread whose
// channel or root its store doesn't have; Clack's store looks
// conversations up as rows draw them.
func (v *Threads) shownItems(items []thrItem) []thrItem {
	app := v.h.Core()
	if app == nil {
		return items
	}
	var out []thrItem
	for i := range items {
		if !thrLeftChannel(app, items[i].Channel) {
			if out != nil {
				out = append(out, items[i])
			}
			continue
		}
		if out == nil {
			out = append(make([]thrItem, 0, len(items)), items[:i]...)
		}
	}
	if out == nil {
		return items
	}
	return out
}

// thrLeftChannel reports a conversation core knows the viewer isn't in
// (isMemberConv false): a channel left, archived or deleted, or one only
// previewed. DMs and group DMs are always theirs.
func thrLeftChannel(app *core.App, ch string) bool {
	if _, in := app.S.Conv(ch); in {
		return false
	}
	c, ok := app.S.ExtConv(ch)
	return ok && !c.IsMember && !c.IsIM && !c.IsMPIM
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
//
// The card's replies carry over only while the two lists join: the window
// starts at or before the card's newest reply, or every reply since
// (reply_count's rise) is in the window. Otherwise replies came between
// them that neither list has (three in one refresh's 5 s over a 2-reply
// window), and "Show N more replies", which loads before the oldest one
// shown, would never bring them: the card takes the window alone and
// folds (true), so the row loads the rest in order.
func thrRefreshed(had, now thrItem) (thrItem, bool) {
	if len(had.Replies) == 0 || len(now.Replies) == 0 {
		return now, false
	}
	first, newest := now.Replies[0].TS, had.Replies[len(had.Replies)-1].TS
	if actTSLess(newest, first) {
		added := 0
		for _, m := range now.Replies {
			if actTSLess(newest, m.TS) {
				added++
			}
		}
		if now.Root.ReplyCount-had.Root.ReplyCount != added {
			return now, true
		}
	}
	reps := make([]core.Message, 0, len(had.Replies)+len(now.Replies))
	for _, m := range had.Replies {
		if actTSLess(m.TS, first) {
			reps = append(reps, m)
		}
	}
	if len(reps) == 0 {
		return now, false
	}
	reps = append(reps, now.Replies...)
	sort.SliceStable(reps, func(i, j int) bool { return actTSLess(reps[i].TS, reps[j].TS) })
	now.Replies = reps
	return now, false
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
