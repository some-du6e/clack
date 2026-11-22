package messages

import (
	"context"
	"image"
	"time"

	"gioui.org/layout"
	"gioui.org/op"

	"github.com/some-du6e/clack/internal/core"
)

// The thread pane draws one slice of the thread, a page at a time, as
// Slack's ThreadsFlexpane does (core thread_pages.go, build 35b42de8b870):
//
//   - the slice is the one its start falls in (getThreadStartTs: a jump's
//     reply, the subscription's last read, the root), from the store; an
//     empty one shows FlexpaneLoading while BaseMessageList's loadAround
//     reads around the start (requestReplies);
//   - the list (akeQ BaseDynamicList.loadMore, after every update) asks
//     for the page before the slice (loadPre: {latest: its start}) once
//     its first row is within 400px of the content's top, and for the page
//     after it (loadPost: {oldest: its end}) once its last is within 400px
//     of the bottom; both with the list shorter than the pane (loadAround:
//     both bounds), with nothing past a reached end;
//   - one load at a time, and none until 250ms after the last one settled
//     (finishLoading's hasRequestInFlight and its c(250)), when the list
//     looks again;
//   - getThreadRowsAndKeys draws a "loading" row above a slice short of
//     the thread's start, the root and the "N replies" separator only in
//     the slice that starts with it, and a "loading" row instead of the
//     reply box ("input") until the slice reaches the end (or had, before
//     a disconnect: prevReachedEnd);
//   - the list sticks to its end only once the slice reached it
//     (setStickToBottom(reachedEnd)): a newer page lands under the reader;
//   - a reply the socket brings joins only a slice at the thread's end
//     (addTimestamps' append); the root's copy (rootMsg) is the messages
//     store's, wherever the slice is.

// threadState is a thread model's slice (callers hold the model's mu).
type threadState struct {
	// start is the thread metadata's startTs, or getThreadStartTs' pick
	// for an open with none: the slice drawn is the one it falls in. ""
	// until the first load picks it (or after the pane closed: clearStartTs)
	start string
	// gen counts moves of start: a load out for an older start lands
	// nowhere
	gen uint64
	// sliceStart and sliceEnd are the slice's range, what loadPre and
	// loadPost page from (the reads' bounds, not always a message's ts)
	sliceStart, sliceEnd                     string
	reachedStart, reachedEnd, prevReachedEnd bool
	// root is rootMsg: the store's copy of the root, wherever the slice is
	root *core.Message
	// newer: loadPost's page is out (model.older is loadPre's)
	newer bool
	// settleAt: finishLoading's 250ms after a page settled, before the
	// list loads again
	settleAt     time.Time
	newerFails   int
	newerRetryAt time.Time
	// endGap: the slice had reached the thread's end when a slow connect
	// made it stale, and no read has reached it since: the viewer's sends
	// past the slice stay drawn (setThreadSlice)
	endGap bool
}

// loadOut reports a load whose answer replaces the model's messages: the
// first (or a refresh), or for a thread a page (callers hold mu). Events
// meanwhile are noted for keepLive.
func (m *model) loadOut() bool {
	return m.loading || m.threadTS != "" && (m.older || m.th.newer)
}

// threadCovers reports whether ts falls inside the slice the thread model
// draws: at or past its start (anything before, once it reached the
// thread's start) and at or before its end (anything after, once it
// reached the end). Callers hold mu.
func (m *model) threadCovers(ts string) bool {
	t := &m.th
	first, last := t.sliceStart, t.sliceEnd
	if first == "" || last == "" {
		if len(m.msgs) == 0 {
			return false
		}
		first, last = m.msgs[0].m.TS, m.msgs[len(m.msgs)-1].m.TS
	}
	return (t.reachedStart || !tsLess(ts, first)) && (t.reachedEnd || !tsLess(last, ts))
}

// staleThreadEnd is the hub's copy of the slow connect's reset
// (store resetThreadEndsLocked, which core made before it told the lists
// of the connection): a slice that reached the thread's end has only
// prevReachedEnd now, so the socket's replies past it wait until the
// pane's read joins them, and the list at its end reads forward
// (loadPost). Callers hold mu.
func (m *model) staleThreadEnd() {
	if m.threadTS != "" && m.th.reachedEnd {
		m.th.reachedEnd, m.th.prevReachedEnd, m.th.endGap = false, true, true
	}
}

// setThreadSlice makes sl the model's messages and slice (callers hold
// mu).
//
// own is the viewer's sends core tracks in the channel (core.TrackedSends:
// confirmed ones for a while). A model at the thread's end (reached, or
// had before a slow connect) keeps those of its messages past a slice
// that no longer reaches the end, with its pending and failed copies:
// after a socket gap a send lands in a slice of its own until the pane's
// read past the stale end joins it, and the pane keeps it meanwhile, as
// the channel list keeps the viewer's sends through its catch-up (core
// reached_end.go).
func (m *model) setThreadSlice(sl core.ThreadSlice, own map[string]bool) {
	msgs := sl.Msgs
	if (m.th.reachedEnd || m.th.prevReachedEnd || m.th.endGap) && !sl.ReachedEnd {
		end := sl.End
		if n := len(sl.Msgs); n > 0 && tsLess(end, sl.Msgs[n-1].TS) {
			end = sl.Msgs[n-1].TS
		}
		var keep []core.Message
		for i := range m.msgs {
			if x := &m.msgs[i].m; (x.Pending || x.Failed || own[x.TS]) && (end == "" || tsLess(end, x.TS)) {
				keep = append(keep, *x)
			}
		}
		if len(keep) > 0 {
			msgs = append(append(make([]core.Message, 0, len(sl.Msgs)+len(keep)), sl.Msgs...), keep...)
		}
	}
	m.setAll(msgs)
	if sl.Root != nil {
		r := *sl.Root
		m.th.root = &r
	}
	m.th.sliceStart, m.th.sliceEnd = sl.Start, sl.End
	m.th.reachedStart, m.th.reachedEnd, m.th.prevReachedEnd = sl.ReachedStart, sl.ReachedEnd, sl.PrevReachedEnd
	if sl.ReachedEnd {
		m.th.endGap = false // the read joined the end: the slice holds the sends
	}
	m.hasMore = !sl.ReachedStart
}

// applyThreadMsg puts a message an event brought (isNew: a new one) in a
// thread model, as Slack's stores take it: the root's copy is rootMsg
// (and the slice's row when the slice holds the root); a reply changes
// the slice's copy, and a new one goes in only where the slice reaches
// (addTimestamps joins it to a slice at the thread's end). It reports
// whether the model changed. Callers hold mu.
func (m *model) applyThreadMsg(msg core.Message, seq uint64, isNew bool) bool {
	if msg.TS == m.threadTS {
		r := msg
		m.th.root = &r
		if m.find(msg.TS) >= 0 {
			m.upsert(msg, seq)
		}
		m.noteLive(msg.TS, false)
		m.rev++
		return true
	}
	if isNew {
		delete(m.typing, msg.User)
	}
	switch {
	case m.find(msg.TS) >= 0, m.loaded && m.threadCovers(msg.TS),
		// the viewer's own pending or failed send, from the reply box at the
		// thread's end: it stays drawn while a read past a stale end is out
		m.loaded && (msg.Pending || msg.Failed) && (m.th.reachedEnd || m.th.prevReachedEnd || m.th.endGap):
		m.upsert(msg, seq)
		m.noteLive(msg.TS, false)
	case !m.loaded:
		m.notePlace(msg, seq) // a load out may hold it
	default:
		return isNew // past the slice: a slice of its own in the store
	}
	m.rev++
	return true
}

// loadThreadSlice is a thread model's load (ensureLoaded's goroutine):
// the slice at its start, from the store, read around the start first
// when the store has none there (core.ThreadAt). A jump that moved the
// start meanwhile leaves this answer out: the next load reads its slice.
func (hb *hub) loadThreadSlice(ctx context.Context, m *model) {
	m.mu.Lock()
	gen, start := m.th.gen, m.th.start
	m.mu.Unlock()
	if start == "" {
		start = hb.app.ThreadStartTs(m.ch, m.threadTS, "", "", false)
	}
	sl, err := hb.app.ThreadAt(ctx, m.ch, m.threadTS, start)
	own := hb.app.TrackedSends(m.ch)
	seq := hb.app.EmitSeq()
	if f := loadLanded.Load(); f != nil {
		(*f)(m)
	}
	m.mu.Lock()
	m.loadSeq = seq
	m.loading = false
	if m.th.gen != gen {
		m.live, m.livePlace, m.liveReact = nil, nil, nil
		m.rev++
		m.mu.Unlock()
		return
	}
	m.err = err
	live := m.takeLive()
	if err == nil {
		m.th.start = start
		m.setThreadSlice(sl, own)
		m.dropped = false // the store's slice, whole
		m.keepLive(live)
		m.loaded = true
		m.fetched = time.Now()
		m.fails, m.retryAt = 0, time.Time{}
	} else {
		m.failed(hb, err, false, time.Now())
	}
	m.markLoadChanged()
	m.rev++
	m.mu.Unlock()
	hb.afterLoad(m)
}

// threadPage is BaseMessageList's loads over the model's slice: older
// (loadPre: {latest: its start}), newer (loadPost: {oldest: its end}) or
// both (loadAround: {latest: its end, oldest: its start}). It reports
// whether it asked for one (none past a reached end, while another load
// is out, or within 250ms of the last).
func (hb *hub) threadPage(m *model, older, newer bool) bool {
	if hb.authFailed.Load() || !older && !newer {
		return false
	}
	now := time.Now()
	m.mu.Lock()
	t := &m.th
	if older && newer {
		// loadAround: past a reached end it is loadPre, past a reached
		// start loadPost
		switch {
		case t.reachedEnd:
			newer = false
		case t.reachedStart:
			older = false
		}
	}
	if older && t.reachedStart {
		older = false // loadPre: reachedStart
	}
	if newer && t.reachedEnd {
		newer = false // loadPost: reachedEnd
	}
	busy := m.loading || m.older || t.newer || !m.loaded || m.dropped || now.Before(t.settleAt)
	if older && now.Before(m.olderRetryAt) || newer && now.Before(t.newerRetryAt) {
		busy = true
	}
	if busy || !older && !newer {
		m.mu.Unlock()
		return false
	}
	first, last := t.sliceStart, t.sliceEnd
	if first == "" && len(m.msgs) > 0 {
		first = m.msgs[0].m.TS
	}
	if last == "" && len(m.msgs) > 0 {
		last = m.msgs[len(m.msgs)-1].m.TS
	}
	if first == "" {
		first = t.start
	}
	if last == "" {
		last = t.start
	}
	// both: loadAround's two bounds; older: loadPre's; newer: loadPost's
	oldest, latest := "", ""
	switch {
	case older && newer:
		oldest, latest = first, last
		m.older, t.newer = true, true
	case older:
		latest = first
		m.older = true
	default:
		oldest = last
		t.newer = true
	}
	gen, start := t.gen, t.start
	m.rev++
	m.mu.Unlock()
	hb.invalidate()
	go func() {
		ctx, cancel := context.WithTimeout(context.Background(), 45*time.Second)
		defer cancel()
		err := hb.app.RequestReplies(ctx, m.ch, m.threadTS, oldest, latest)
		var sl core.ThreadSlice
		var own map[string]bool
		if err == nil {
			sl = hb.app.ThreadSliceAt(m.ch, m.threadTS, start)
			own = hb.app.TrackedSends(m.ch)
		}
		seq := hb.app.EmitSeq()
		if f := loadLanded.Load(); f != nil {
			(*f)(m)
		}
		m.mu.Lock()
		m.loadSeq = seq
		if older {
			m.older = false
		}
		if newer {
			m.th.newer = false
		}
		m.th.settleAt = time.Now().Add(250 * time.Millisecond)
		live := m.takeLive()
		switch {
		case m.th.gen != gen || m.dropped || !m.loaded:
			// the start moved (a jump) or the hub let the model go: its
			// own load reads the slice
		case err == nil:
			m.setThreadSlice(sl, own)
			m.keepLive(live)
			if older {
				m.olderFails, m.olderRetryAt = 0, time.Time{}
			}
			if newer {
				m.th.newerFails, m.th.newerRetryAt = 0, time.Time{}
			}
		default:
			if older {
				m.failed(hb, err, true, time.Now())
			}
			if newer {
				m.th.newerFails++
				m.th.newerRetryAt = time.Now().Add(backoff(m.th.newerFails))
				if authError(err) {
					hb.authFailed.Store(true)
				}
			}
		}
		m.markLoadChanged()
		m.rev++
		m.mu.Unlock()
		hb.afterLoad(m)
		hb.invalidate()
	}()
	return true
}

// jumpThread is jumpToReply for the model: its start moves to ts. A slice
// that holds ts stays (the list scrolls to it); else the model draws the
// store's slice at ts, empty (FlexpaneLoading) until its around read
// lands. It reports whether the model's messages changed.
func (hb *hub) jumpThread(m *model, ts string) bool {
	if ts == "" {
		return false
	}
	m.mu.Lock()
	t := &m.th
	if t.start == ts {
		m.mu.Unlock()
		return false
	}
	held := m.loaded && (m.find(ts) >= 0 || m.threadCovers(ts))
	t.start = ts
	if held {
		m.mu.Unlock()
		return false
	}
	t.gen++
	gen := t.gen
	m.mu.Unlock()
	sl := hb.app.ThreadSliceAt(m.ch, m.threadTS, ts)
	m.mu.Lock()
	if m.th.gen == gen {
		m.live, m.livePlace, m.liveReact = nil, nil, nil
		if sl.Empty() {
			m.msgs = nil
			m.loaded, m.hasMore = false, false
			m.fetched = time.Time{}
			m.th.sliceStart, m.th.sliceEnd = "", ""
			m.th.reachedStart, m.th.reachedEnd, m.th.prevReachedEnd = false, false, false
			if sl.Root != nil {
				r := *sl.Root
				m.th.root = &r
			}
		} else {
			m.setThreadSlice(sl, nil)
			m.loaded = true
			m.fetched = time.Now()
			m.markLoadChanged() // the slice's messages new to the model, for the plugins' copy (setAll)
		}
		m.rev++
	}
	m.mu.Unlock()
	hb.ensureLoaded(m)
	return true
}

// hubOf is app's hub, nil when no list was made for it.
func hubOf(app *core.App) *hub {
	hubsMu.Lock()
	defer hubsMu.Unlock()
	return hubs[app]
}

// SetThreadStart is the navigation's jumpToThreadReply (client-boot
// eCxs) for a thread about to open at reply ts: the thread's start, set
// before its pane mounts, so the pane's first load reads around ts and
// nothing else. UI goroutine.
func SetThreadStart(app *core.App, ch, thread, ts string) {
	hb := hubOf(app)
	if hb == nil || ch == "" || thread == "" || ts == "" {
		return
	}
	hb.jumpThread(hb.threadModel(ch, thread), ts)
}

// CloseThread is a thread pane leaving ch/thread: truncateThreadHistory
// (core.CloseThread), and with unmount (the pane unmounted, not switched
// to another thread) clearStartTs too (a later open starts at
// getThreadStartTs again). When the pane unmounted, or the store let any
// of the thread go, the model and the panes' lists start over, as a pane
// mounted afresh does: Slack's next ThreadsFlexpane is a new component,
// which draws the store's slice at getThreadStartTs (empty: loadAround)
// from getThreadStartPosition. A page or a load out meanwhile lands
// nowhere (the model's gen moves), so it can't make the model's
// messages a slice at no start, or a cut history's. UI goroutine.
func CloseThread(app *core.App, ch, thread string, unmount bool) {
	if app == nil || ch == "" || thread == "" {
		return
	}
	cut := app.CloseThread(ch, thread)
	hb := hubOf(app)
	if hb == nil {
		return
	}
	hb.mu.Lock()
	m := hb.thread[ch+"/"+thread]
	hb.mu.Unlock()
	if m == nil || !cut && !unmount {
		return
	}
	m.mu.Lock()
	m.msgs = nil
	m.loaded, m.hasMore = false, false
	m.fetched = time.Time{}
	m.live, m.livePlace, m.liveReact = nil, nil, nil
	m.th = threadState{root: m.th.root, gen: m.th.gen + 1}
	m.rev++
	m.mu.Unlock()
	// maybeSaveThreadScrollmarks (componentWillUnmount): where the reader
	// was, by message, for the next mount, except in a public channel the
	// viewer isn't in
	mark := unmount && !cut
	if c, ok := app.Conv(ch); ok && c.Kind == "channel" && c.Preview {
		mark = false
	}
	for _, s := range listCache {
		if s.hb == hb && s.thread && s.ch == ch && s.ts == thread {
			s.thrMark = threadMark{}
			if i := s.list.Position.First; mark && s.positioned && s.laidOut && i >= 0 && i < len(s.items) && s.items[i].kind == itMsg {
				s.thrMark = threadMark{key: s.items[i].key, off: s.list.Position.Offset}
			}
			s.positioned = false
			s.thr = threadRead{}
			s.pendingJump = ""
			s.place = jumpPlace{}
			s.snap.rev = 0
			s.tl = threadList{}
		}
	}
}

// threadMark is a thread list's scrollmark, saved as its pane unmounts
// (maybeSaveThreadScrollmarks): the message at the list's top and its
// offset. The next mount starts there when nothing is highlighted and
// the slice it draws holds that message (getThreadStartPosition's first
// rule), else at the thread's start position: a list's place is a
// message, never a row index into a slice it no longer draws.
type threadMark struct {
	key string
	off int
}

// threadList is the thread list's paging state (listState.tl).
type threadList struct {
	// h is each item's height as last laid out, by key: the list's
	// layout knows only the items on screen, and loadMore measures what
	// lies above and below them
	h map[string]int
	// findPend is a find result the slice didn't hold: findGo moved the
	// model's start to it (jumpThread), and the list scrolls to it and
	// flashes it once its row is in (threadPaging), or lets it go once the
	// slice at it is in without it. A bar closed meanwhile leaves it, as
	// Slack's pendingScrollToTs outlives the bar: the slice is the
	// result's already, and nothing reads for it but that one load
	findPend string
	findOff  int
	// noted is the replies on screen core was last told of
	// (NoteThreadView), first and last
	noted [2]string
}

// threadEdgeNear is akeQ loadMore's 400px.
const threadEdgeNear = 400

// noteThreadHeights keeps the heights of the items just laid out.
func (s *listState) noteThreadHeights() {
	if s.tl.h == nil {
		s.tl.h = map[string]int{}
	}
	for i := s.visible[0]; i < s.visible[1] && i < len(s.items); i++ {
		if h := s.heights[i]; h > 0 {
			s.tl.h[s.items[i].key] = h
		}
	}
	if len(s.tl.h) > 4*len(s.items)+64 {
		live := make(map[string]bool, len(s.items))
		for _, it := range s.items {
			live[it.key] = true
		}
		for k := range s.tl.h {
			if !live[k] {
				delete(s.tl.h, k)
			}
		}
	}
}

// threadEdges is loadMore's measure: the height of the items above the
// first one on screen and below the last (an item never laid out counts
// as est), each counted only up to 400px.
func (s *listState) threadEdges(est int) (above, below int) {
	for i := s.visible[0] - 1; i >= 0 && above < threadEdgeNear; i-- {
		above += s.itemH(i, est)
	}
	for i := s.visible[1]; i < len(s.items) && below < threadEdgeNear; i++ {
		below += s.itemH(i, est)
	}
	return above, below
}

func (s *listState) itemH(i, est int) int {
	if h, ok := s.tl.h[s.items[i].key]; ok {
		return h
	}
	return est
}

// threadPaging is loadMore for the thread list, after its layout: pages
// before or after the slice as the list nears an end (one at a time,
// 250ms apart: a frame is asked for when that wait ends), and a find
// result findGo moved the slice to scrolled to once it's in. A list whose
// slice stops short of the thread's end never sits at its end: the newer
// page lands under the reader. Core hears which replies are on screen
// (noteThreadView).
func (s *listState) threadPaging(gtx layout.Context, h Host, size image.Point, now time.Time) {
	t := &s.snap.th
	if !t.reachedEnd && !s.list.Position.BeforeEnd {
		s.list.Position.BeforeEnd = true // setStickToBottom(reachedEnd)
	}
	s.noteThreadHeights()
	if !s.snap.loaded || len(s.snap.msgs) == 0 || s.snap.err != nil && !s.snap.loading {
		return
	}
	s.noteThreadView()
	// a find result past the slice: the slice at it (findGo), then the
	// scroll and the flash
	if f := s.tl.findPend; f != "" {
		switch {
		case s.scrollToFind(f, s.tl.findOff):
			s.tl.findPend = ""
			s.findFlash, s.findFlashAt = f, now
		case t.start == f && !s.snap.loading:
			s.tl.findPend = "" // the slice at it is in, and it isn't a reply drawn: nothing to show
		}
	}
	above, below := s.threadEdges(dp(gtx, 40))
	content := 0
	for i := s.visible[0]; i < s.visible[1] && i < len(s.heights); i++ {
		content += s.heights[i]
	}
	short := s.visible[0] == 0 && s.visible[1] == len(s.items) && content <= size.Y
	pre := short || above < threadEdgeNear
	post := short || below < threadEdgeNear
	if pre || post {
		s.askThreadPage(gtx, pre, post, now)
	}
}

// noteThreadView tells core which replies the pane has on screen, first
// to last, as that changes: what a poll reads again while the socket is
// away (core NoteThreadView, pollThread).
func (s *listState) noteThreadView() {
	if s.hb == nil || s.hb.app == nil {
		return
	}
	var v [2]string
	for i := s.visible[0]; i < s.visible[1] && i < len(s.items); i++ {
		if it := s.items[i]; it.kind == itMsg {
			if v[0] == "" {
				v[0] = it.key
			}
			v[1] = it.key
		}
	}
	if v[0] == "" || v == s.tl.noted {
		return
	}
	s.tl.noted = v
	s.hb.app.NoteThreadView(s.ch, s.ts, v[0], v[1])
}

// askThreadPage asks the hub for the page(s), and for a frame when the
// 250ms after the last page ends (loadMore runs again then).
func (s *listState) askThreadPage(gtx layout.Context, older, newer bool, now time.Time) {
	if s.hb.threadPage(s.m, older, newer) {
		return
	}
	s.m.mu.Lock()
	at := s.m.th.settleAt
	out := s.m.loadOut()
	s.m.mu.Unlock()
	if !out && now.Before(at) {
		gtx.Execute(op.InvalidateCmd{At: at})
	}
}

// SliceRange is the slice the pane drew last: its first and last
// message, and whether it reaches the thread's start and its end.
func (v *ThreadView) SliceRange() (first, last string, reachedStart, reachedEnd bool) {
	s := v.s
	if n := len(s.snap.msgs); n > 0 {
		first, last = s.snap.msgs[0].m.TS, s.snap.msgs[n-1].m.TS
	}
	return first, last, s.snap.th.reachedStart, s.snap.th.reachedEnd
}
