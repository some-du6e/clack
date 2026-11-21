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
	lastPre      string // the latest a loadPre asked for (loadMoreForPendingScroll's lastLoadedEarliestTs)
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

// setThreadSlice makes sl the model's messages and slice (callers hold
// mu).
func (m *model) setThreadSlice(sl core.ThreadSlice) {
	m.setAll(sl.Msgs)
	if sl.Root != nil {
		r := *sl.Root
		m.th.root = &r
	}
	m.th.sliceStart, m.th.sliceEnd = sl.Start, sl.End
	m.th.reachedStart, m.th.reachedEnd, m.th.prevReachedEnd = sl.ReachedStart, sl.ReachedEnd, sl.PrevReachedEnd
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
	case m.find(msg.TS) >= 0, m.loaded && m.threadCovers(msg.TS):
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
		m.setThreadSlice(sl)
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
		t.lastPre = first
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
		if err == nil {
			sl = hb.app.ThreadSliceAt(m.ch, m.threadTS, start)
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
			m.setThreadSlice(sl)
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
			m.setThreadSlice(sl)
			m.loaded = true
			m.fetched = time.Now()
		}
		m.th.lastPre = ""
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

// CloseThread is the thread pane's unmount for ch/thread: clearStartTs
// (a later open starts at getThreadStartTs again) and
// truncateThreadHistory (core.CloseThread). When the store let any of the
// thread go, the model and the panes' lists start over, as a pane mounted
// afresh does. UI goroutine.
func CloseThread(app *core.App, ch, thread string) {
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
	if m == nil {
		return
	}
	m.mu.Lock()
	if cut && !m.loadOut() {
		m.msgs = nil
		m.loaded, m.hasMore = false, false
		m.fetched = time.Time{}
		m.th = threadState{root: m.th.root, gen: m.th.gen + 1}
		m.rev++
	} else {
		m.th.start = ""
		m.th.lastPre = ""
	}
	m.mu.Unlock()
	if !cut {
		return
	}
	for _, s := range listCache {
		if s.hb == hb && s.thread && s.ch == ch && s.ts == thread {
			s.positioned = false
			s.thr = threadRead{}
			s.pendingJump = ""
			s.snap.rev = 0
			s.tl = threadList{}
		}
	}
}

// threadList is the thread list's paging state (listState.tl).
type threadList struct {
	// h is each item's height as last laid out, by key: the list's
	// layout knows only the items on screen, and loadMore measures what
	// lies above and below them
	h map[string]int
	// findPend is a find result the slice doesn't hold yet:
	// loadAndScrollToTs pages back (loadMoreForPendingScroll) until it
	// does, or the slice reaches the thread's start
	findPend string
	findOff  int
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
// result the slice doesn't hold paged back to. A list whose slice stops
// short of the thread's end never sits at its end: the newer page lands
// under the reader.
func (s *listState) threadPaging(gtx layout.Context, h Host, size image.Point, now time.Time) {
	t := &s.snap.th
	if !t.reachedEnd && !s.list.Position.BeforeEnd {
		s.list.Position.BeforeEnd = true // setStickToBottom(reachedEnd)
	}
	s.noteThreadHeights()
	if !s.snap.loaded || len(s.snap.msgs) == 0 || s.snap.err != nil && !s.snap.loading {
		return
	}
	// a find result past the slice: older pages until it's in
	if f := s.tl.findPend; f != "" {
		if s.scrollToFind(f, s.tl.findOff) {
			s.tl.findPend = ""
			s.findFlash, s.findFlashAt = f, now
		} else if t.reachedStart {
			s.tl.findPend = "" // not in the thread's replies: nothing to show
		} else if first := firstNonEmpty(t.sliceStart, s.snap.msgs[0].m.TS); t.lastPre != first {
			s.askThreadPage(gtx, true, false, now) // lastLoadedEarliestTs: once per earliest ts
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

// firstNonEmpty is a if set, else b.
func firstNonEmpty(a, b string) string {
	if a != "" {
		return a
	}
	return b
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
