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
	"slices"
	"sort"
	"time"

	"gioui.org/op"
	"gioui.org/unit"

	"github.com/some-du6e/clack/internal/core"
	"github.com/some-du6e/clack/internal/slack"
	"github.com/some-du6e/clack/internal/ui/popups"
	"github.com/some-du6e/clack/internal/ui/slackc"
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

// thrSeen is a listed thread as a load left it: its newest reply and the
// last read it had.
type thrSeen struct{ latest, lastRead string }

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
	seen := make(map[string]thrSeen, len(page.items))
	for i := range page.items {
		t := &page.items[i]
		now := thrSeen{latest: t.latest(), lastRead: v.lastRead(t)}
		if prev, ok := v.maxSeen[t.key]; ok && actTSLess(prev.latest, now.latest) {
			if v.grew == nil {
				v.grew = map[string]thrSeen{}
			}
			if _, had := v.grew[t.key]; !had {
				v.grew[t.key] = prev
			}
		}
		seen[t.key] = now
		if c := v.cards[t.key]; c != nil && len(c.older) > 0 && len(t.Replies) > 0 {
			n := len(c.older)
			for n > 0 && !actTSLess(c.older[n-1].TS, t.Replies[0].TS) {
				n--
			}
			if n < len(c.older) {
				c.older, c.shown = c.older[:n], nil
			}
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
// after a slow connect: componentDidUpdate calls reloadThreads only when
// it isn't loading (this.loading: before the view's first answer, or
// while a load that sorts the list or the next page is out), and the
// increment is spent either way.
func (v *Threads) reconnectDue() bool {
	v.mu.Lock()
	due := v.reconn
	v.reconn = false
	v.mu.Unlock()
	_, loaded, _ := v.data.get()
	return due && loaded && !(v.data.busy() && v.mode != actRefresh)
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
	var marked map[string]core.ThreadMarkOrigin
	if app := v.h.Core(); app != nil {
		marked = app.ThreadsMarkedUnread()
	}
	for i := range page.items {
		t := &page.items[i]
		if lr, seen := v.initial[t.key]; seen {
			if marked[t.key] != "" && lr != v.lastRead(t) {
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
// subscribed: core seeds it from getView's roots), else as its answer had
// it.
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

// attentionLost is the host's count of the times attention went (the
// focus lost, an overlay opened), 0 from one that doesn't count.
func (v *Threads) attentionLost() uint64 {
	if ah, ok := v.h.(interface{ AttentionLost() uint64 }); ok {
		return ah.AttentionLost()
	}
	return 0
}

// markNewReplies is maybeMarkNewMessageOnUpdate, after the frame that lays
// out a load's answer (landed's grew): for each thread whose newest reply
// moved on, unless the viewer marked it unread. Seen means its newest
// reply's row is in the list's visible range while the viewer's attention
// is on the view. Unseen, a reply from someone else moves the thread's
// arrival last read (initialLastReadByThread) to its last read when it had
// none or an older one, so the divider sees the reply as new. Seen, an
// unread followed thread is marked read (setThreadLastRead) when it came
// in read, its last read had every reply before these, or its "unread"
// row shows. Slack checks each reply as the store takes it; a refresh
// brings them together, so when a later reply of the viewer's own has
// marked the thread read, the replies before it are judged against the
// last read the thread had before them. It reports whether anything
// changed (the next frame draws it). Not ported: an unseen reply also
// counts when the window blurred between the two updates
// (lastWindowBlurEvent).
func (v *Threads) markNewReplies(page thrData) bool {
	if len(v.grew) == 0 {
		return false
	}
	grew := v.grew
	v.grew = nil
	var marked map[string]core.ThreadMarkOrigin
	if app := v.h.Core(); app != nil {
		marked = app.ThreadsMarkedUnread()
	}
	att, self, changed := v.attention(), v.selfID(), false
	for i := range page.items {
		t := &page.items[i]
		prev, ok := grew[t.key]
		if !ok || marked[t.key] != "" {
			continue
		}
		cur, lr := t.latest(), v.lastRead(t)
		unread := actTSLess(lr, cur)
		base := lr // the last read the new replies came over
		if !unread {
			base = prev.lastRead
		}
		a := v.initial[t.key]
		seen := att && v.kb.overlaps(t.key+"-"+cur)
		if !seen && (a == "" || actTSLess(a, base)) && thrOtherAfter(t, v.cards[t.key], prev.latest, base, self) {
			v.initial[t.key], changed = base, true
		}
		if unread && seen && v.followed(t) && (a == "" || !actTSLess(lr, prev.latest) || v.unreadRowSeen(t)) {
			v.markRead(t)
			changed = true
		}
	}
	return changed
}

// thrOtherAfter reports a reply of the card's newer than after and base
// from someone other than self.
func thrOtherAfter(t *thrItem, c *thrCard, after, base, self string) bool {
	for _, m := range thrShown(t, c) {
		if actTSLess(after, m.TS) && actTSLess(base, m.TS) && m.User != self {
			return true
		}
	}
	return false
}

// drawUnread is v.unread for the name's weight. In the frame that lays out
// a landing, a thread markNewReplies has yet to judge that was read
// before it is drawn read, as Slack decides in componentDidUpdate before
// the browser paints: the next frame shows what was decided.
func (v *Threads) drawUnread(t *thrItem) int {
	if g, ok := v.grew[t.key]; ok && !actTSLess(v.lastRead(t), g.latest) {
		return 0
	}
	return v.unread(t)
}

// Where a card's unread row goes (unreadRow), besides before a reply.
const (
	thrNoUnreadRow   = -1 // none
	thrUnreadAtOlder = -2 // at "Show N more replies": the unread run starts among the hidden replies
)

// unreadRow is get_rows_and_keys' unread row (749-770) for a card: a
// followed thread with an arrival last read (initialLastReadByThread) has
// it before the first reply shown that is newer, isTempMsg replies left
// out, or at "Show N more replies" when that last read is older than the
// root and replies are hidden. It is "unread" while the last read is
// still the arrival one, and "unread_marked_read" (ghost) once it moved
// on. A thread that came in read or never read (no last read) has none,
// whatever is unread in it.
func (v *Threads) unreadRow(t *thrItem, c *thrCard, replies []core.Message) (at int, ghost bool) {
	a := v.initial[t.key]
	if a == "" || !v.followed(t) {
		return thrNoUnreadRow, false
	}
	ghost = v.lastRead(t) != a
	if actTSLess(a, t.Root.TS) && c != nil && c.moreText != "" {
		return thrUnreadAtOlder, ghost
	}
	for i := range replies {
		if actTSLess(a, replies[i].TS) && !replies[i].Pending {
			return i, ghost
		}
	}
	return thrNoUnreadRow, false
}

// unreadRowSeen is the thread's "unread" row (not the ghost) in the list's
// visible range: getVisibleRange(void 0, true) over Layout.getBounds. The
// row is the divider itself, which takes no height
// (.c-message_list__unread_divider{height:0}), at the top of the first
// unread reply, or of load_older when the unread run starts among the
// hidden replies: it is in the range while that point is in the viewport,
// however much of the reply below it shows.
func (v *Threads) unreadRowSeen(t *thrItem) bool {
	c := v.cards[t.key]
	replies := thrShown(t, c)
	at, ghost := v.unreadRow(t, c, replies)
	switch {
	case at == thrNoUnreadRow || ghost:
		return false
	case at == thrUnreadAtOlder:
		return v.kb.pointIn("load_older-" + t.key)
	}
	return v.kb.pointIn(t.key + "-" + replies[at].TS)
}

// thrMarkDelay is maybeMarkVisibleThreadAsRead's setTimeout after a load
// and after a scroll; thrScrollWait is onListScrolledDebounce's 150 ms.
const (
	thrMarkDelay  = 350 * time.Millisecond
	thrScrollWait = 150 * time.Millisecond
)

// visibleDue reports whether markVisible runs in this frame, after the
// list's layout: a timer is due (350 ms after a load that sorts the list
// landed, or after onListScroll), or the viewer's attention came back
// since the frame before (componentDidUpdate; Layout tracks it in every
// frame, and one that comes back while no list is drawn marks nothing, as
// Slack's maybeMarkVisibleThreadAsRead returns without a list).
// onListScroll is onListScrolledDebounce's, jp8I debounceRAF(onListScroll,
// 150): every scroll re-arms it and it calls only once 150 ms went by
// with none, so a fling marks nothing until it has stopped, and then only
// what shows 350 ms on. It asks for the frame the next one is due in.
func (v *Threads) visibleDue(gtx C) bool {
	now := gtx.Now
	due := v.attBack
	if p := v.list.Position; p.First != v.scrollPos.First || p.Offset != v.scrollPos.Offset {
		v.scrollPos, v.scrollAt, v.scrollTail = p, now, true
	}
	if v.scrollTail {
		if at := v.scrollAt.Add(thrScrollWait); !now.Before(at) { // the debounce's call
			v.scrollTail = false
			v.visAt = append(v.visAt, now.Add(thrMarkDelay))
		} else {
			gtx.Execute(op.InvalidateCmd{At: at})
		}
	}
	keep := v.visAt[:0]
	var next time.Time
	for _, at := range v.visAt {
		if !now.Before(at) {
			due = true
			continue
		}
		keep = append(keep, at)
		if next.IsZero() || at.Before(next) {
			next = at
		}
	}
	v.visAt = keep
	if !next.IsZero() {
		gtx.Execute(op.InvalidateCmd{At: next})
	}
	return due
}

// spendChecks is maybeMarkVisibleThreadAsRead's timers coming due while no
// list is drawn (the error view, no threads): each returns without a
// listRef, spent. Those still to come wait, with a frame at the next.
func (v *Threads) spendChecks(gtx C) {
	v.scrollTail = false
	keep := v.visAt[:0]
	var next time.Time
	for _, at := range v.visAt {
		if gtx.Now.Before(at) {
			keep = append(keep, at)
			if next.IsZero() || at.Before(next) {
				next = at
			}
		}
	}
	v.visAt = keep
	if !next.IsZero() {
		gtx.Execute(op.InvalidateCmd{At: next})
	}
}

// markVisible is maybeMarkVisibleThreadAsRead (threads-view.js 1946-1968),
// how Slack's Threads view clears what it shows: with the viewer's
// attention on the view, each followed thread whose "unread" row is in the
// visible range and that is unread is marked read at its newest reply
// (setThreadLastRead read: true at maxMarkableTs) and leaves
// ThreadsMarkedUnread, unless the viewer marked it unread from the Threads
// view itself. Its row then turns ghost. It reports whether it marked any.
func (v *Threads) markVisible(page thrData) bool {
	if !v.attention() {
		return false
	}
	var marked map[string]core.ThreadMarkOrigin
	if app := v.h.Core(); app != nil {
		marked = app.ThreadsMarkedUnread()
	}
	changed := false
	for i := range page.items {
		t := &page.items[i]
		if marked[t.key] == core.MarkedInThreadsView || !v.followed(t) || !actTSLess(v.lastRead(t), t.latest()) || !v.unreadRowSeen(t) {
			continue
		}
		v.markRead(t)
		changed = true
	}
	return changed
}

// The Threads view's marked-read UnreadDivider (VxYf ghost:
// div.c-message_list__unread_divider--ghost.p-threads_view__unread_divider
// > hr.c-message_list__unread_divider__separator), for its line's
// transition.
var (
	thrClsUnread    = slackc.Class("c-message_list__unread_divider c-message_list__unread_divider--unread_threads p-threads_view__unread_divider").Mods("c-message_list__unread_divider--ghost")
	thrClsUnreadSep = slackc.Class("c-message_list__unread_divider__separator").Tag("hr")
)

// ghostLine is the ghost divider's line colour: col at the --ghost
// separator's opacity, .5 from its first frame. get_rows_and_keys keys the
// row "unread", then "unread_marked_read", so React mounts a new
// UnreadDivider with --ghost already on it and the rule's transition
// (opacity .1s 3s) never runs; a plugin rule's opacity still sets it.
func (v *Threads) ghostLine(col color.NRGBA) color.NRGBA {
	op := float32(0.5)
	if sc := slackc.CurrentScope(); sc.Active() {
		if st := sc.Child(thrClsUnread, 1).Child(thrClsUnreadSep, 0); st.Has("opacity") {
			op = st.Opacity()
		}
	}
	col.A = uint8(float32(col.A) * min(max(op, 0), 1))
	return col
}

// thrGapBackoff is how long after a failed gap fetch a card's gaps take
// it to its window at once instead of fetching.
const thrGapBackoff = 30 * time.Second

// fillGap fetches the replies a refresh's window couldn't be joined to
// (conversations.replies after the card's newest and before the window's
// first, requestReplies with an oldest bound). Slack's card has every
// reply that came, as its store takes each off the socket; a refresh here
// has only getView's latest window, so the ones it lacks join the card
// when they land (mid). One fetch per card at a time: a gap that comes
// while one is out widens the one that goes next. Shortly after a failure
// the card goes to its window instead (cutToWindow), and "Show N more
// replies" loads the rest in order.
func (v *Threads) fillGap(page thrData, g thrGap) {
	app := v.h.Core()
	t := thrByKey(page.items, g.key)
	if app == nil || t == nil || v.left {
		return // left: the remount in this frame replaces the cards
	}
	c := v.cardOf(t)
	if c.floor != "" && actTSLess(g.after, c.floor) {
		// a failure cut the card here: only what comes after it is wanted
		if !actTSLess(c.floor, g.before) {
			return
		}
		g.after = c.floor
	}
	switch {
	case c.gapOut:
		if c.gapNext.key == "" {
			c.gapNext = g
			return
		}
		if actTSLess(g.after, c.gapNext.after) {
			c.gapNext.after = g.after
		}
		if actTSLess(c.gapNext.before, g.before) {
			c.gapNext.before = g.before
		}
		return
	case time.Now().Before(c.gapBack):
		v.cutToWindow(c, t)
		return
	}
	c.gapOut = true
	ch, root, folds, from := t.Channel, t.Root.TS, c.folds, app.MessageChangeSeq()
	go func() {
		ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
		defer cancel()
		msgs, more, err := app.RepliesBetween(ctx, ch, root, g.after, g.before, thrOlderLimit)
		v.notes.run(v.h, func() { v.landGap(g, folds, from, msgs, more || err != nil) })
	}()
}

// thrByKey is the thread key among items, nil when it isn't one.
func thrByKey(items []thrItem, key string) *thrItem {
	for i := range items {
		if items[i].key == key {
			return &items[i]
		}
	}
	return nil
}

// landGap takes a gap's fetch in (UI goroutine): its replies join the
// card's (mid), current as of the change the fetch went out at, from which
// they take what changed (threads_live.go), and the gap waiting goes out.
// One asked for before the card folded lands nowhere. A failure (or more
// replies than one answer holds) takes the card to its latest window,
// and its gaps there too for a while (thrGapBackoff).
func (v *Threads) landGap(g thrGap, folds, from uint64, msgs []core.Message, failed bool) {
	c := v.cards[g.key]
	if c == nil || c.folds != folds {
		return
	}
	c.gapOut = false
	d, _, _ := v.data.get()
	t := thrByKey(d.items, g.key)
	if t == nil {
		return
	}
	if failed {
		v.cutToWindow(c, t)
		c.gapBack = time.Now().Add(thrGapBackoff)
		return
	}
	var run []core.Message
	for _, m := range msgs {
		if actTSLess(g.after, m.TS) && actTSLess(m.TS, g.before) {
			run = append(run, m)
		}
	}
	if len(run) > 0 {
		if len(c.mid) == 0 || from < c.midSeq {
			c.midSeq = from
		}
		c.mid, c.shown, c.built = thrMerge(c.mid, run), nil, false
		v.lv.items = nil // noteLiveKeys again, with them (threads_live.go)
	}
	if next := c.gapNext; next.key != "" {
		c.gapNext = thrGap{}
		v.fillGap(d, next)
	}
}

// cutToWindow takes card c of thread t back to the replies of its latest
// getView window (thrItem.window): the older replies it loaded, the gaps
// it filled and the replies refreshes carried over go, with any fetch
// still out for them (fold). A load already out read the carried replies
// before this, so settle drops what it brings from before the window
// (floor), and only from that load's answer.
func (v *Threads) cutToWindow(c *thrCard, t *thrItem) {
	v.fold(c)
	c.floor, c.floorSeq = t.window, v.seq
	v.dropCarried(t.key, t.window)
}

// dropCarried keeps only thread key's replies from ts on. The items are
// copied, as a load still out reads the old ones.
func (v *Threads) dropCarried(key, ts string) {
	if ts == "" {
		return
	}
	v.data.mutate(func(d *thrData) {
		i := slices.IndexFunc(d.items, func(t thrItem) bool { return t.key == key })
		if i < 0 || len(d.items[i].Replies) == 0 || !actTSLess(d.items[i].Replies[0].TS, ts) {
			return
		}
		items := slices.Clone(d.items)
		var keep []core.Message
		for _, m := range items[i].Replies {
			if !actTSLess(m.TS, ts) {
				keep = append(keep, m)
			}
		}
		items[i].Replies = keep
		d.items = items
	})
}

// keepFloors drops from the answer of a load asked for before a card's
// cut (cutToWindow; its fetch seq up to the cut's floorSeq) the replies it
// carries over from before the card's floor: it read them before the cut.
// The answer's own window stays wherever it starts (a deletion moves it
// back). An answer asked for after the cut has the cut in its copies, and
// the floor goes.
func (v *Threads) keepFloors(page thrData) {
	for i := range page.items {
		t := &page.items[i]
		c := v.cards[t.key]
		if c == nil || c.floor == "" {
			continue
		}
		if page.seq > c.floorSeq {
			c.floor = ""
			continue
		}
		cut := c.floor
		if t.window != "" && actTSLess(t.window, cut) {
			cut = t.window
		}
		v.dropCarried(t.key, cut)
	}
}

// run queues f for the UI goroutine, the next Layout's drain.
func (n *actNotes) run(h Host, f func()) {
	n.mu.Lock()
	n.fns = append(n.fns, f)
	n.mu.Unlock()
	h.Invalidate()
}

// shownItems is the threads mapStateToProps hands ThreadsView: it skips
// one "in non-member public channel", which Clack's store has as a
// conversation the viewer isn't in (a channel they left: core's
// dropMembership). Such a thread keeps its place in the order and shows
// again if they come back. An archived channel's threads stay: Slack's
// channelArchived sets only is_archived, where core's dropMembership drops
// the membership too. Not ported: Slack also skips a thread whose channel
// or root its store doesn't have; Clack's store looks conversations up as
// rows draw them.
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
// (isMemberConv false) and that isn't archived: a channel left or
// deleted, or one only previewed. DMs and group DMs are always theirs.
func thrLeftChannel(app *core.App, ch string) bool {
	if _, in := app.S.Conv(ch); in {
		return false
	}
	c, ok := app.S.ExtConv(ch)
	return ok && !c.IsMember && !c.IsArchived && !c.IsIM && !c.IsMPIM
}

// resort is getDerivedStateFromProps on a new refreshId (a mount, or
// reloadThreads, which drops currentRefreshId before it loads): the
// threads the view holds, followed ones only (its "subscribed" filter;
// truncateThreads drops the others when the view unmounts), sorted by
// sortThreads (8orQ) on the view's own read state (isThreadUnread reads
// the subscription store, the view's marks in it), then newest activity,
// with a new load number (the divider's thread is picked from it in the
// next frame), until the reload's answer replaces them. Each card starts
// again at its getView window (getDefaultMinimumVisibleTs, iPpN, on the
// new refreshId), "Show N more replies" above it: the replies refreshes
// carried over go, as the gaps filled among them do (fold).
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
			if t.window != "" {
				if j := slices.IndexFunc(t.Replies, func(m core.Message) bool { return !actTSLess(m.TS, t.window) }); j > 0 {
					t.Replies = t.Replies[j:]
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
		d.items, d.load = items, load
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
// The carried replies are the copies the card had, current as of had.seq:
// carry brings each up to date (thrCarryLive), as the thread's stamp is
// now the page's. When the window starts after the card's newest reply,
// replies may have come between the two that neither list has (three in
// one refresh's 5 s over a 2-reply window): the card keeps both lists and
// reports the span (thrGap) for settle to fetch (fillGap), and the
// replies that brings take the live changes as the others do (thrCard.mid,
// threads_live.go). Nothing about the counts tells: reply_count moves
// with deletions, and the root's live copy counts what the window brings.
func thrRefreshed(had, now thrItem, carry thrCarry) (thrItem, thrGap, bool) {
	if len(had.Replies) == 0 || len(now.Replies) == 0 {
		return now, thrGap{}, false
	}
	first, newest := now.Replies[0].TS, had.Replies[len(had.Replies)-1].TS
	reps := make([]core.Message, 0, len(had.Replies)+len(now.Replies))
	for i := range had.Replies {
		m := &had.Replies[i]
		if !actTSLess(m.TS, first) {
			continue
		}
		if carry == nil {
			reps = append(reps, *m)
		} else if nm, ok := carry(had.Channel, m, had.seq, now.seq); ok {
			reps = append(reps, nm)
		}
	}
	if len(reps) == 0 {
		return now, thrGap{}, false
	}
	reps = append(reps, now.Replies...)
	sort.SliceStable(reps, func(i, j int) bool { return actTSLess(reps[i].TS, reps[j].TS) })
	now.Replies = reps
	if actTSLess(newest, first) {
		return now, thrGap{key: now.key, after: newest, before: first}, true
	}
	return now, thrGap{}, false
}

// thrCarry is a reply a refresh carries over, m (a copy current as of
// message change seq), as it is now; false when it goes. now is the
// change the refresh's copies are current as of.
type thrCarry func(ch string, m *core.Message, seq, now uint64) (core.Message, bool)

// thrCarryLive is thrCarry over core's change table (the load's
// goroutine): a carried reply takes what core changed of it after its
// copy, as landLive would have had it stayed. One deleted before the
// refresh went out leaves (the page's counts have it gone); a later
// deletion is landLive's, which takes the reply out with its unread
// count. A copy older than what core forgot takes the store's, as in
// floorLive, and one with no copy left is fetched again by id (it comes
// back as a change).
func thrCarryLive(c *core.App) thrCarry {
	return func(ch string, m *core.Message, seq, now uint64) (core.Message, bool) {
		chg, ok := c.MessageChange(ch, m.TS)
		switch {
		case !ok:
			if c.MessageChangeFloor() > seq {
				if sm, ok := c.HeldMessage(ch, m.TS); ok {
					return sm, true
				}
				c.RefetchMessages(ch, []string{m.TS})
			}
			return *m, true
		case chg.Seq <= seq:
			return *m, true
		case chg.Deleted:
			return *m, chg.Seq > now
		}
		if nm, ok := c.ChangedMessage(ch, m.TS, &chg, m, seq); ok {
			return nm, true
		}
		c.RefetchMessages(ch, []string{m.TS})
		return *m, true
	}
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
