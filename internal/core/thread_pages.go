package core

import (
	"cmp"
	"context"
	"log"
	"net/url"
	"slices"
	"strconv"
	"sync"
	"time"

	"github.com/some-du6e/clack/internal/logtag"
	"github.com/some-du6e/clack/internal/slack"
	"github.com/some-du6e/clack/internal/store"
)

// A thread loads a page at a time, as Slack's thread pane does (build
// 35b42de8b870, 2026-11-19):
//
//   - ThreadsFlexpane (boot-shared l6n6) starts at getThreadStartTs
//     (client-boot 7Kqu QT): the reply a jump asked for (the thread
//     metadata's startTs, jumpToReply, or the view's replyTs), else the
//     subscription's last read (withOriginalLastRead), else the root.
//   - It draws the slice of channelHistory[threadKey] that start falls in
//     (dTxY getPotentiallyOutdatedTimestamps; store/thread_slices.go). An
//     empty one shows FlexpaneLoading, and BaseMessageList (Twp/), which
//     the virtual list (akeQ) asks to load whenever it is within 400px of
//     an end, calls loadAround: requestHistory({oldest: start, latest:
//     start}).
//   - requestReplies (client-boot gOq3, fetchAndAddReplies) makes that
//     one conversations.replies call from the root ({oldest: root}) when
//     start is the root, else two at once around start ({latest: start}
//     and {oldest: start}); scrolled near the top loadPre asks
//     {latest: the slice's start}, near the bottom loadPost {oldest: its
//     end}. fetchReplies (rGEL $b) sends {channel, ts, inclusive: true,
//     limit} with the limit determineDefaultHistoryLimit gives a thread:
//     28. Each answer holds the root as well, and the limit counts only
//     replies (live Slack answers 29 messages: the root and 28 replies,
//     the bound itself among them); a page short of the thread's start
//     leaves the root out of the slice (the messages store keeps it).
//   - Closing the pane is truncateThreadHistory (l6n6 eY): an archived
//     channel's or a public channel's the viewer isn't in thread history
//     goes (removeAllTimestamps), any other keeps its newest 1,000
//     timestamps (truncateChannelHistory).
//
// So a thread opens with one or two requests of 28 replies whatever its
// size, and holds only the pages it was scrolled through.

// ThreadPageLimit is determineDefaultHistoryLimit() for a thread (rGEL
// UL: 3 for two special channels, 28 for anything else, a thread key
// among them).
const ThreadPageLimit = 28

// ThreadSlice is what the thread pane draws of a thread: the root
// message (rootMsg: the messages store's copy, nil when not held) and
// the slice its start falls in, oldest first, the root first when the
// slice reaches the thread's start.
type ThreadSlice struct {
	Root                     *Message
	Msgs                     []Message
	Start, End               string // the slice's range (what loadPre and loadPost page from)
	ReachedStart, ReachedEnd bool
	PrevReachedEnd           bool // the end it had reached before a disconnect (the reply box stays)
	// Held: a replies read loaded the thread (else the slice is empty
	// and the pane loads around its start)
	Held bool
}

// Empty is the empty slice (dTxY's f): nothing drawn but FlexpaneLoading.
func (t ThreadSlice) Empty() bool { return len(t.Msgs) == 0 && t.Start == "" && t.End == "" }

// ThreadStartTs is getThreadStartTs (7Kqu QT): the start a jump asked for
// (startTs, else replyTs: a real or local ts), else the subscription's
// last read when the viewer follows the thread, else the root; the root
// whenever startAtTop.
func (a *App) ThreadStartTs(ch, ts, startTs, replyTs string, startAtTop bool) string {
	if startAtTop {
		return ts
	}
	for _, s := range []string{startTs, replyTs} {
		if s != "" {
			return s
		}
	}
	if lr := a.threadOriginalLastRead(ch, ts); lr != "" {
		return lr
	}
	return ts
}

// threadOriginalLastRead is withOriginalLastRead's lastRead (boot-shared
// l6n6): the thread subscription's last read when the viewer follows the
// thread, "" when not. Slack's mapStateToProps reads the subscription by
// the thread's key (getSubscriptionState, client-boot YFI6 nq), with no
// need of the root message: the store's subscription, else the one a
// copy of the root carries (slurpThreadSubscriptions takes it from every
// message the client holds), a copy fetched by id among them (Pins,
// Drafts, Activity and a file's thread hold their roots there, in no
// window or cached thread).
func (a *App) threadOriginalLastRead(ch, ts string) string {
	sub, ok := a.S.ThreadSub(ch, ts)
	if !ok {
		m, found := a.S.FindHeld(ch, ts)
		if !found {
			return ""
		}
		sub = a.S.ThreadSubOf(ch, &m)
	}
	if sub.Subscribed {
		return sub.LastRead
	}
	return ""
}

// ThreadSliceAt is the slice of thread ch/ts the pane started at start
// draws, from memory (no request): getPotentiallyOutdatedTimestamps over
// the thread's history, with the messages and the root the store holds.
//
// A reply's ts is its parent's thread at the reply: conversations.replies
// answers a reply's ts with its parent's thread, which repliesOne and
// repliesAround keep under the parent's key (replyRooted), so a pane
// opened with the reply's ts draws the reply in its parent's slice and
// hands on to the parent's thread (the list's ReplyAsThreadRoot).
func (a *App) ThreadSliceAt(ch, ts, start string) ThreadSlice {
	if p, ok := a.ReplyParent(ch, ts); ok {
		ts = p
	}
	sl, held := a.S.ThreadSliceAt(ch, ts, start)
	out := ThreadSlice{Start: sl.Start, End: sl.End, ReachedStart: sl.ReachedStart, ReachedEnd: sl.ReachedEnd,
		PrevReachedEnd: sl.PrevReachedEnd, Held: held}
	if root, ok := a.threadRoot(ch, ts); ok {
		r := a.message(ch, &root)
		out.Root = &r
	}
	if len(sl.TSs) > 0 {
		out.Msgs = a.messages(ch, a.S.ThreadSliceMsgs(ch, ts, sl))
	}
	return out
}

// threadRoot is the store's copy of a thread's root: the one its replies
// reads keep, else the channel's (its window, a fetched copy).
func (a *App) threadRoot(ch, ts string) (slack.Message, bool) {
	if th, ok := a.S.Thread(ch, ts); ok {
		for i := range th {
			if th[i].TS == ts {
				return th[i], true
			}
		}
	}
	return a.S.Find(ch, ts)
}

// ThreadAt is the slice the pane started at start draws, loaded first
// when it is empty: BaseMessageList's loadAround over an empty slice,
// requestHistory({oldest: start, latest: start}). It watches the thread
// (pollOnce) and notes its use (the eviction's order), as History does a
// channel's.
func (a *App) ThreadAt(ctx context.Context, ch, ts, start string) (ThreadSlice, error) {
	if p, ok := a.ReplyParent(ch, ts); ok {
		return a.ThreadAt(ctx, ch, p, ts) // a reply's ts: its parent's thread at the reply, watched as the parent's
	}
	a.watch(ch, ts)
	a.noteAccess(ch + "/" + ts)
	if start == "" {
		start = ts
	}
	sl := a.ThreadSliceAt(ch, ts, start)
	if sl.Empty() {
		if err := a.RequestReplies(ctx, ch, ts, start, start); err != nil {
			return sl, err
		}
		sl = a.ThreadSliceAt(ch, ts, start)
	}
	a.subscribeThreadPresence(sl)
	return sl, nil
}

func (a *App) subscribeThreadPresence(sl ThreadSlice) {
	var ids []string
	for i := range sl.Msgs {
		if u := sl.Msgs[i].User; u != "" {
			ids = append(ids, u)
		}
	}
	if len(ids) > 0 {
		a.SubscribePresence(ids)
	}
}

// RequestReplies is requestReplies (gOq3 fetchAndAddReplies) with the
// bounds a load asked for: one conversations.replies call, or two at once
// when both bounds are set and differ from the root (around a reply);
// the answers go into the store as addMessagesFromHistory puts them in
// the messages store and the thread's channelHistory.
func (a *App) RequestReplies(ctx context.Context, ch, ts, oldest, latest string) error {
	h := ts
	A := latest
	if oldest == latest && latest == h {
		A = ""
	}
	root, _ := a.threadRoot(ch, ts)
	S := root.LatestReply // latestReplyTs: the root's latest_reply
	v := S != "" && store.TSLess(h, S) && oldest == A && A == S && a.experiment("threads_perf_quality") == "on"
	w := oldest
	if v {
		w = ""
	}
	key := "rr:" + ch + "/" + ts + "|" + w + "|" + A
	return a.once(key, func() error {
		if w != "" && A != "" {
			return a.repliesAround(ctx, ch, ts, w, A)
		}
		reason := ""
		switch {
		case v:
			reason = "request-around-older"
		case A != "":
			reason = "request-older"
		case w != "":
			reason = "request-newer"
		}
		return a.repliesOne(ctx, ch, ts, w, A, reason, v, S)
	})
}

// repliesPage is one fetchReplies answer.
type repliesPage struct {
	msgs    []slack.Message
	hasMore bool
	since   uint64 // LiveSeq when its request went out
	pins    uint64
	subs    uint64
	// parent: the ts asked for was a reply's, and this is its parent's
	// thread (conversations.replies answers a reply's ts with it)
	parent string
}

// fetchReplies is rGEL $b: conversations.replies {channel, ts, inclusive:
// true, limit: 28} with oldest / latest when set (Clack also asks for the
// messages' metadata, as every replies read of its does).
func (a *App) fetchReplies(ctx context.Context, ch, ts, oldest, latest, reason string) (repliesPage, error) {
	p := url.Values{"channel": {ch}, "ts": {ts}, "inclusive": {"true"}, "limit": {strconv.Itoa(ThreadPageLimit)},
		"include_all_metadata": {"true"}}
	if oldest != "" {
		p.Set("oldest", oldest)
	}
	if latest != "" {
		p.Set("latest", latest)
	}
	var r struct {
		Messages []slack.Message `json:"messages"`
		HasMore  bool            `json:"has_more"`
	}
	pg := repliesPage{since: a.S.LiveSeq(), pins: a.S.PinSeq(), subs: a.S.SubSeq()}
	t0 := time.Now()
	err := a.C.Call(ctx, "conversations.replies", p, &r)
	log.Printf("replies %s/%s %s: %d (%s)", logtag.ID(ch), logtag.ID(ts), reason, len(r.Messages), time.Since(t0).Round(time.Millisecond))
	if err != nil {
		return pg, err
	}
	pg.msgs, pg.hasMore = r.Messages, r.HasMore
	thread := ts
	if pg.parent = replyParentIn(pg.msgs, ts); pg.parent != "" {
		thread = pg.parent
	}
	for i := range pg.msgs {
		if pg.msgs[i].ThreadTS == "" && pg.msgs[i].TS != thread {
			pg.msgs[i].ThreadTS = thread // a reply's copy names its thread
		}
	}
	return pg, nil
}

func withoutTS(msgs []slack.Message, ts string) []slack.Message {
	out := make([]slack.Message, 0, len(msgs))
	for _, m := range msgs {
		if m.TS != ts {
			out = append(out, m)
		}
	}
	return out
}

func findTS(msgs []slack.Message, ts string) *slack.Message {
	for i := range msgs {
		if msgs[i].TS == ts {
			return &msgs[i]
		}
	}
	return nil
}

func tssOf(msgs []slack.Message) []string {
	out := make([]string, len(msgs))
	for i := range msgs {
		out[i] = msgs[i].TS
	}
	return out
}

// repliesAround is fetchAndAddReplies with both bounds: {latest} and
// {oldest} at once, the replies of both in one slice from the older
// read's start to the newer's end, the root first when the older reached
// the thread's start.
func (a *App) repliesAround(ctx context.Context, ch, ts, w, A string) error {
	var older, newer repliesPage
	var errOlder, errNewer error
	var wg sync.WaitGroup
	wg.Add(2)
	go func() { defer wg.Done(); older, errOlder = a.fetchReplies(ctx, ch, ts, "", A, "request-around-older") }()
	go func() { defer wg.Done(); newer, errNewer = a.fetchReplies(ctx, ch, ts, w, "", "request-around-newer") }()
	wg.Wait()
	if errOlder != nil {
		return errOlder
	}
	if errNewer != nil {
		return errNewer
	}
	if parent := cmp.Or(older.parent, newer.parent); parent != "" {
		a.replyRooted(ch, ts, parent) // a reply's ts: the answers are its parent's thread, kept under its key
		ts = parent
	}
	root := findTS(older.msgs, ts)
	if root == nil {
		root = findTS(newer.msgs, ts)
	}
	var page []slack.Message
	seen := map[string]bool{}
	for _, m := range append(withoutTS(older.msgs, ts), withoutTS(newer.msgs, ts)...) {
		if !seen[m.TS] {
			seen[m.TS] = true
			page = append(page, m)
		}
	}
	tss := tssOf(page)
	if !older.hasMore && root != nil {
		tss = append([]string{ts}, tss...)
	}
	lo, hi := w, A
	for _, t := range tss {
		if store.TSLess(t, lo) {
			lo = t
		}
		if store.TSLess(hi, t) {
			hi = t
		}
	}
	all := page
	if root != nil {
		all = append([]slack.Message{*root}, page...)
	}
	b := store.ThreadPageBounds{Oldest: lo, Latest: hi, Ends: true, HasMoreStart: older.hasMore, HasMoreEnd: newer.hasMore}
	a.addRepliesPage(ch, ts, all, tss, b, min(older.since, newer.since), min(older.pins, newer.pins), min(older.subs, newer.subs))
	return nil
}

// repliesOne is fetchAndAddReplies' one request: from the root, older
// than latest (loadPre), newer than oldest (loadPost), or (v: the
// threads_perf_quality experiment, starting at the root's latest_reply)
// the newest page alone.
func (a *App) repliesOne(ctx context.Context, ch, ts, w, A, reason string, v bool, S string) error {
	pg, err := a.fetchReplies(ctx, ch, ts, w, A, reason)
	if err != nil {
		return err
	}
	a.addOnePage(ch, ts, pg, w, A, v, S)
	return nil
}

// addOnePage puts repliesOne's answer pg (read with oldest w and latest A)
// in the store, as fetchAndAddReplies' one request does; it returns the
// thread's key (the parent's, when ts was a reply's).
func (a *App) addOnePage(ch, ts string, pg repliesPage, w, A string, v bool, S string) string {
	if pg.parent != "" {
		a.replyRooted(ch, ts, pg.parent) // a reply's ts: the answer is its parent's thread, kept under its key
		ts = pg.parent
	}
	t := pg.msgs
	s := t
	if (A == "" || pg.hasMore) && w != ts && A != ts && (w != "" || A != "" || pg.hasMore) {
		s = withoutTS(t, ts)
	}
	o := findTS(t, ts)
	l := ""
	if o != nil {
		l = o.LatestReply
	}
	d := v && l != "" && !store.TSLess(S, l)
	b := store.ThreadPageBounds{Oldest: w, Latest: A, HasMore: pg.hasMore}
	if !((w != "" || A != "") && !d) {
		b = store.ThreadPageBounds{Oldest: w, Latest: A, Ends: true, HasMoreStart: pg.hasMore, HasMoreEnd: false}
	}
	all := s
	if o != nil && findTS(s, ts) == nil {
		all = append([]slack.Message{*o}, s...) // the messages store keeps the root
	}
	a.addRepliesPage(ch, ts, all, tssOf(s), b, pg.since, pg.pins, pg.subs)
	if v && !d {
		a.S.ResetThreadReachedEnd(ch, ts)
	}
	if o != nil && w == ts {
		// the root, its own read up to itself: the history reaches the
		// thread's start
		a.S.AddThreadPageSince(ch, ts, []slack.Message{*o}, []string{ts}, store.ThreadPageBounds{Latest: ts, HasMore: false}, pg.since)
		a.announceReads() // the root's revision, if the read brought another (read_changes.go)
	}
	return ts
}

// addRepliesPage is addMessagesFromHistory for a replies read: the
// messages store keeps the answer (as the socket left them meanwhile),
// the thread's history lists tss, and what came with it follows: the
// root's thread subscription and pins as the answer has them unless an
// event moved them meanwhile, a revision the store held another of
// announced, and the files a message names without their info fetched.
func (a *App) addRepliesPage(ch, ts string, msgs []slack.Message, tss []string, b store.ThreadPageBounds, since, pins, subs uint64) {
	a.S.SyncSubs(ch, subs, msgs) // the root's thread subscription (store/thread_subs.go)
	kept := a.S.AddThreadPageSince(ch, ts, msgs, tss, b, since)
	a.S.SyncPins(ch, pins, kept) // a pin event newer than the answer (store/pins.go)
	a.announceReads()            // read_changes.go
	for i := range kept {
		if needsFileInfo(&kept[i]) {
			go a.fillFiles(ch, kept[i].TS)
		}
	}
	// the replies' people are asked for as rows draw them (lazy_users.go)
}

// ThreadNewer is loadPost: the page after end (the slice's end).
func (a *App) ThreadNewer(ctx context.Context, ch, ts, end string) error {
	return a.RequestReplies(ctx, ch, ts, end, "")
}

// ThreadOlder is loadPre: the page before start (the slice's start).
func (a *App) ThreadOlder(ctx context.Context, ch, ts, start string) error {
	return a.RequestReplies(ctx, ch, ts, "", start)
}

// pollThread is what a poll (no socket: pollLoop, Clack's own: Slack's
// client reads nothing while its socket is away) reads of a watched
// thread, so its pane shows what the socket would have told it:
//
//   - the replies the pane has on screen, first to last (NoteThreadView),
//     read again over the store's copies (refreshThreadRange): an edit,
//     a deletion or a reaction meanwhile shows within a poll, as each
//     replies read of Slack's brings the changes in its range (fetchReplies'
//     cached_latest_updates answer: the changed messages, and the deleted
//     ones, which addMessagesFromHistory drops);
//   - the page after the end of a history that had reached it, as
//     loadPost does once the socket is back (resetReachedEnd, then the
//     list at its end loads the newer page). The end goes stale first, as
//     at the connect: with no socket, what came past it is unknown, and a
//     page that leaves more after it keeps the history short of the end
//     (a read past a reached end would leave it reached). A thread the
//     pane reads elsewhere waits for its list to page there. A re-read of
//     the rows that reached the thread's end read that page already.
func (a *App) pollThread(ctx context.Context, ch, ts, first, last string) error {
	if first != "" {
		end, err := a.refreshThreadRange(ctx, ch, ts, first, last)
		if err != nil || end {
			return err
		}
	}
	sl, _ := a.S.ThreadSliceAt(ch, ts, "")
	if sl.Empty() || sl.End == "" || !(sl.ReachedEnd || sl.PrevReachedEnd) {
		return nil
	}
	a.S.ResetThreadReachedEnd(ch, ts)
	return a.RequestReplies(ctx, ch, ts, sl.End, "")
}

// maxPollPages caps the pages a poll reads again of a pane's rows: about
// three screens of replies.
const maxPollPages = 3

// refreshThreadRange reads the replies of thread ch/ts from first to
// last again, as loadPost reads (fetchReplies {oldest}, 28 a page, from
// first), each answer over the store's copies (addOnePage): an edit or
// a reaction shows. A reply the store holds within the range an answer
// covers (from its oldest to its last reply, or on to the thread's end
// when it has no more) that the answer lacks was deleted meanwhile
// (dropThreadGone). It reports whether a page reached the thread's end.
func (a *App) refreshThreadRange(ctx context.Context, ch, ts, first, last string) (bool, error) {
	if last == "" || store.TSLess(last, first) {
		last = first
	}
	cur := first
	for range maxPollPages {
		pg, err := a.fetchReplies(ctx, ch, ts, cur, "", "poll-refresh")
		if err != nil {
			return false, err
		}
		key := a.addOnePage(ch, ts, pg, cur, "", false, "")
		replies := withoutTS(pg.msgs, key)
		hi := ""
		if pg.hasMore && len(replies) > 0 {
			hi = replies[len(replies)-1].TS
		}
		a.dropThreadGone(ch, key, replies, cur, hi, pg.since)
		if !pg.hasMore {
			return true, nil
		}
		if len(replies) == 0 || !store.TSLess(replies[len(replies)-1].TS, last) {
			return false, nil
		}
		cur = replies[len(replies)-1].TS
	}
	return false, nil
}

// dropThreadGone takes the deletions a replies answer shows: a reply of
// thread ch/ts the store holds from lo to hi (hi "": on to the thread's
// end) that the answer got lacks is gone, unless the socket brought it
// after the request went out (since) or it is a send of the viewer's
// core still tracks (sendsIn: a pending or failed copy the server never
// had). Each goes as the socket's message_deleted takes it (the store's
// copies, noted gone, and a "delete" event), as Slack's replies read
// drops the deleted ts its answer lists (gOq3 hands rGEL fetchReplies'
// deleted to addMessagesFromHistory).
func (a *App) dropThreadGone(ch, ts string, got []slack.Message, lo, hi string, since uint64) {
	have := make(map[string]bool, len(got))
	for i := range got {
		have[got[i].TS] = true
	}
	own := a.sendsIn(ch)
	for _, t := range a.S.ThreadRepliesIn(ch, ts, lo, hi, since) {
		if have[t] || own[t] || a.isLocalTS(t) {
			continue
		}
		a.S.NoteRemove(ch, t)
		a.emit(Event{Type: "delete", Channel: ch, TS: t, ThreadTS: ts})
	}
}

// NoteThreadView notes the replies the pane of thread ch/ts has on
// screen, first to last, for the poll that reads them again while the
// socket is away (pollThread). The thread list calls it as what it shows
// changes.
func (a *App) NoteThreadView(ch, ts, first, last string) {
	a.mu.Lock()
	defer a.mu.Unlock()
	for i := range a.watchTh {
		if t := &a.watchTh[i]; t.ch == ch && t.ts == ts {
			t.first, t.last = first, last
			return
		}
	}
}

// TrackedSends is the ts of the viewer's sends in ch core still tracks
// (pending and failed copies, confirmed ones for a while: sendsIn): a
// thread pane keeps them through the read past a stale end, as the
// channel list does (reached_end.go).
func (a *App) TrackedSends(ch string) map[string]bool { return a.sendsIn(ch) }

// CloseThread is truncateThreadHistory, as the thread pane shows the
// thread no more: the history of a thread in an archived channel, or in a
// public channel the viewer isn't in, goes whole; any other keeps its
// newest 1,000 timestamps. It reports whether the store let anything go.
func (a *App) CloseThread(ch, ts string) bool {
	if ch == "" || ts == "" {
		return false
	}
	drop := false
	if c, ok := a.Conv(ch); ok {
		drop = c.IsArchived || c.Kind == "channel" && !c.Member
	}
	return a.S.TruncateThread(ch, ts, drop)
}

// threadDefaultStart is getThreadStartTs with nothing asked for: the last
// read of a followed thread, else the root.
func (a *App) threadDefaultStart(ch, ts string) string { return a.ThreadStartTs(ch, ts, "", "", false) }

// threadMsgs is a slice as Replies returns it: the root first (the
// slice's own, or the store's when the slice starts past it), then the
// slice's replies.
func threadMsgs(sl ThreadSlice, ts string) []Message {
	out := make([]Message, 0, len(sl.Msgs)+1)
	if len(sl.Msgs) == 0 || sl.Msgs[0].TS != ts {
		if sl.Root != nil {
			out = append(out, *sl.Root)
		}
	}
	out = append(out, sl.Msgs...)
	return slices.Clip(out)
}
