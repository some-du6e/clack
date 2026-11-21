package store

import (
	"slices"
	"strings"

	"github.com/some-du6e/clack/internal/slack"
)

// A thread's history as Slack's web client holds it (build 35b42de8b870,
// 2026-11-19): the messages store has the messages (Clack's s.threads
// holds the thread's: its root and every reply a replies read or the
// socket brought), and channelHistory[threadKey] lists which of them a
// view may draw, as slices of the thread's timestamps that each read
// covered without a gap, and whether the first slice reaches the thread's
// start (reachedStart) and the last its end (reachedEnd). The thread pane
// draws the one slice its start ts falls in (getPotentiallyOutdatedTimestamps)
// and grows it a page at a time as it scrolls (requestReplies, core
// thread_pages.go); nothing ever reads the whole thread. The reducer is
// client-boot 6Yed (addTimestamps, removeTimestamp, resetReachedEnd,
// truncateChannelHistory, removeAllTimestamps, removeCachedReplyTimestamps)
// over om2Z's slices (createSlice, intersects, union, insert, remove); the
// selector is dTxY (getTimestampsByChannelId tp, getPotentiallyOutdatedTimestamps r7).

// tslice is one of om2Z's slices: the range it covers (start, end: the
// read's bounds, or its first and last timestamps) and the timestamps in
// it, ascending.
type tslice struct {
	start, end string
	tss        []string
}

// tbool is a channelHistory flag, which Slack leaves undefined (neither
// true nor false: isBoolean is false) until a read sets it.
type tbool int8

const (
	tUnset tbool = iota
	tFalse
	tTrue
)

func (b tbool) isSet() bool  { return b != tUnset }
func (b tbool) isTrue() bool { return b == tTrue }

func tb(v bool) tbool {
	if v {
		return tTrue
	}
	return tFalse
}

// threadHist is channelHistory[threadKey].
type threadHist struct {
	slices                   []tslice
	reachedStart, reachedEnd tbool
	prevReachedEnd           bool
}

// tsCmp compares two timestamps (Slack compares the strings, whose
// "1234567890.123456" form sorts as the numbers do).
func tsCmp(a, b string) int {
	switch {
	case TSLess(a, b):
		return -1
	case TSLess(b, a):
		return 1
	}
	return 0
}

// isTempTs is dUDN: a pending message's local ts (or none).
func isTempTs(ts string) bool { return ts == "" || strings.Contains(ts, "x") }

// createSlice is om2Z Z0: the timestamps sorted, from oldest (else the
// first, else latest) to latest (else the last, else oldest).
func createSlice(tss []string, oldest, latest string) tslice {
	r := append([]string(nil), tss...)
	slices.SortStableFunc(r, tsCmp)
	start, end := oldest, latest
	if start == "" && len(r) > 0 {
		start = r[0]
	}
	if start == "" {
		start = latest
	}
	if end == "" && len(r) > 0 {
		end = r[len(r)-1]
	}
	if end == "" {
		end = oldest
	}
	return tslice{start: start, end: end, tss: r}
}

// intersects is om2Z HY: both ranges are known and overlap.
func intersects(e, t *tslice) bool {
	if e == nil || t == nil {
		return false
	}
	n, a, i, r := e.start, e.end, t.start, t.end
	if n == "" || a == "" || i == "" || r == "" {
		return false
	}
	ge := func(x, y string) bool { return tsCmp(x, y) >= 0 }
	le := func(x, y string) bool { return tsCmp(x, y) <= 0 }
	return ge(n, i) && le(n, r) || ge(a, i) && le(a, r) || le(n, i) && ge(a, r) || ge(n, i) && le(a, r)
}

// union is om2Z's: both slices' timestamps merged in order, without
// repeats, over both ranges.
func union(e, t *tslice) *tslice {
	switch {
	case e == nil && t == nil:
		return nil
	case e == nil:
		c := *t
		return &c
	case t == nil:
		c := *e
		return &c
	}
	a, r := e.tss, t.tss
	n := make([]string, 0, len(a)+len(r))
	last := func() string {
		if len(n) == 0 {
			return ""
		}
		return n[len(n)-1]
	}
	for i, j := 0, 0; i+j < len(a)+len(r); {
		if i == len(a) || j < len(r) && tsCmp(r[j], a[i]) < 0 {
			if r[j] != last() || len(n) == 0 {
				n = append(n, r[j])
			}
			j++
		} else {
			if a[i] != last() || len(n) == 0 {
				n = append(n, a[i])
			}
			i++
		}
	}
	out := &tslice{tss: n}
	switch {
	case e.start != "" && t.start != "":
		out.start = e.start
		if tsCmp(t.start, e.start) < 0 {
			out.start = t.start
		}
	case e.start != "":
		out.start = e.start
	default:
		out.start = t.start
	}
	switch {
	case e.end != "" && t.end != "":
		out.end = e.end
		if tsCmp(e.end, t.end) < 0 {
			out.end = t.end
		}
	case e.end != "":
		out.end = e.end
	default:
		out.end = t.end
	}
	return out
}

// insert is om2Z Yr: t goes in before the first slice that starts after
// it, merged with what it overlaps; with mergeStart (the read reached the
// thread's start) every slice before it merges into it, with mergeEnd
// (it reached the end) every slice after it, and with appendEnd (the
// history had reached the end) a t past every slice joins the last one.
func insert(e []tslice, t tslice, appendEnd, mergeStart, mergeEnd bool) []tslice {
	if len(e) == 0 {
		return []tslice{t}
	}
	var r *tslice
	var s []tslice
	o := false
	for n := range e {
		l := e[n]
		if !o && l.start != "" && t.start != "" && tsCmp(l.start, t.start) > 0 {
			if intersects(r, &t) || mergeStart {
				r = union(r, &t)
			} else {
				if r != nil {
					s = append(s, *r)
				}
				tt := t
				r = &tt
			}
			o = true
		}
		if intersects(r, &l) || mergeStart && !o || mergeEnd && o {
			r = union(r, &l)
		} else {
			if r != nil {
				s = append(s, *r)
			}
			ll := l
			r = &ll
		}
	}
	if !o {
		if r != nil && (intersects(r, &t) || appendEnd || mergeStart) {
			r = union(r, &t)
		} else {
			if r != nil {
				s = append(s, *r)
			}
			tt := t
			r = &tt
		}
	}
	if r != nil {
		s = append(s, *r)
	}
	return s
}

// ThreadPageBounds is what addMessagesFromHistory hands addTimestamps for
// one replies read: its bounds (oldest, latest: the request's, "" for
// none) and has_more, either as the answer's one boolean (HasMore: which
// end it is for follows from the bounds) or for each end (Ends set: the
// around read's {hasMoreStart, hasMoreEnd}).
type ThreadPageBounds struct {
	Oldest, Latest           string
	HasMore                  bool
	Ends                     bool
	HasMoreStart, HasMoreEnd bool
	// NoBounds is a live message's addTimestamps: no bounds and no
	// has_more at all (every one of them undefined)
	NoBounds bool
}

// addTimestampsLocked is 6Yed's addTimestamps for the thread key k.
func (s *Store) addTimestampsLocked(k string, tss []string, b ThreadPageBounds) {
	if tss == nil {
		return // `!n`: no timestamps array at all
	}
	if s.threadHist == nil {
		s.threadHist = map[string]*threadHist{}
	}
	f := s.threadHist[k]
	had := f != nil
	if f == nil {
		f = &threadHist{}
	}
	if had && f.reachedStart.isSet() && f.reachedEnd.isSet() && b.NoBounds {
		// every timestamp in some slice already: nothing changes
		in := false
		for i := range f.slices {
			all := true
			for _, ts := range tss {
				if isTempTs(ts) || !sortedHas(f.slices[i].tss, ts) {
					all = false
					break
				}
			}
			if all {
				in = true
				break
			}
		}
		if in {
			return
		}
	}
	y, bEnd := b.hasMoreEnds()
	var setStart, setEnd tbool // A.reachedStart, A.reachedEnd: what this read sets
	if !f.reachedStart.isSet() {
		setStart = tFalse
	}
	if !f.reachedEnd.isSet() {
		setEnd = tFalse
	}
	if !y {
		setStart = tTrue
	}
	if !bEnd {
		setEnd = tTrue
	}
	clearPrev := f.prevReachedEnd && b.Latest == "" && (b.Oldest != "" || !b.NoBounds)
	w := f.reachedEnd.isTrue()
	t := createSlice(tss, b.Oldest, b.Latest)
	if w && len(f.slices) > 0 {
		e := f.slices[len(f.slices)-1]
		if isTempTs(t.start) {
			t.start = e.end
		}
		if isTempTs(t.end) {
			t.end = e.end
		}
	}
	f.slices = insert(f.slices, t, w, setStart.isTrue(), setEnd.isTrue())
	if setStart.isSet() {
		f.reachedStart = setStart
	}
	if setEnd.isSet() {
		f.reachedEnd = setEnd
	}
	if clearPrev {
		f.prevReachedEnd = false
	}
	s.threadHist[k] = f
}

// hasMoreEnds is addTimestamps' y and b: get(hasMore, "hasMoreStart",
// latest ? hasMore : oldest !== "0000000000.000000") and get(hasMore,
// "hasMoreEnd", !oldest || hasMore); a boolean hasMore has neither key, so
// the bounds decide which end it is for.
func (b ThreadPageBounds) hasMoreEnds() (start, end bool) {
	switch {
	case b.Ends:
		return b.HasMoreStart, b.HasMoreEnd
	case b.NoBounds:
		return true, true
	}
	if b.Latest != "" {
		start = b.HasMore
	} else {
		start = b.Oldest != "0000000000.000000"
	}
	return start, b.Oldest == "" || b.HasMore
}

// sortedHas reports whether the ascending tss holds ts (sortedIndexOf).
func sortedHas(tss []string, ts string) bool {
	_, ok := slices.BinarySearchFunc(tss, ts, tsCmp)
	return ok
}

// removeTimestampLocked is 6Yed's removeTimestamp: ts leaves the slices
// that cover it (an emptied one goes), and the history no longer reaches
// the end when the last slice went.
func (s *Store) removeTimestampLocked(k, ts string) {
	a := s.threadHist[k]
	if a == nil || len(a.slices) == 0 || ts == "" {
		return
	}
	temp := isTempTs(ts)
	one := createSlice([]string{ts}, "", "")
	out := make([]tslice, 0, len(a.slices))
	lastChanged := false
	for i := range a.slices {
		e := a.slices[i]
		if intersects(&e, &one) || temp && slices.Contains(e.tss, ts) {
			kept := make([]string, 0, len(e.tss))
			for _, x := range e.tss {
				if x != ts {
					kept = append(kept, x)
				}
			}
			n := createSlice(kept, e.start, e.end)
			if i == len(a.slices)-1 {
				lastChanged = true
			}
			if len(n.tss) > 0 || len(e.tss) == 0 {
				out = append(out, n)
			}
			continue
		}
		out = append(out, e)
	}
	if len(out) == 0 && len(a.slices) == 1 && a.reachedStart.isTrue() && a.reachedEnd.isTrue() {
		out = append(out, createSlice(nil, "", ""))
	}
	dropped := len(out) != len(a.slices) && lastChanged
	a.slices = out
	if a.reachedEnd.isTrue() && dropped {
		a.reachedEnd = tFalse
	}
}

// ThreadHistTruncateLimit and ThreadHistTruncateAllowance are
// truncateChannelHistory's defaults: past limit+allowance timestamps, only
// the newest limit stay.
const (
	ThreadHistTruncateLimit     = 1000
	ThreadHistTruncateAllowance = 50
)

// TruncateThread is truncateThreadHistory (boot-shared l6n6 eY), as the
// thread pane closes or shows another thread: with drop (an archived
// channel, or a public one the viewer isn't in) removeAllTimestamps, the
// thread's history and its replies gone; else truncateChannelHistory,
// which keeps the newest 1,000 timestamps once there are more than 1,050
// (and the history no longer reaches the start). The root stays, as the
// channel's message. It reports whether anything was let go.
func (s *Store) TruncateThread(ch, ts string, drop bool) bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	k := ch + "/" + ts
	a := s.threadHist[k]
	if a == nil {
		return false
	}
	if drop {
		// the replies go with the timestamps (the root is the channel's
		// message: its window or the fetched copy hold it)
		delete(s.threadHist, k)
		delete(s.threads, k)
		return true
	}
	total := 0
	for i := range a.slices {
		total += len(a.slices[i].tss)
	}
	if total <= ThreadHistTruncateLimit+ThreadHistTruncateAllowance {
		return false
	}
	l := total - ThreadHistTruncateLimit
	var out []tslice
	gone := map[string]bool{}
	for _, e := range a.slices {
		t := e.tss
		switch {
		case len(t) == 0:
			continue
		case l <= 0:
			out = append(out, e)
		case l >= len(t):
			for _, x := range t {
				gone[x] = true
			}
			l -= len(t)
		default:
			for _, x := range t[:l] {
				gone[x] = true
			}
			n := append([]string(nil), t[l:]...)
			l = 0
			out = append(out, createSlice(n, n[0], e.end))
		}
	}
	a.slices = out
	a.reachedStart = tFalse
	// the messages store keeps what the remaining slices list (and the
	// root, the channel's message): the rest of the thread's replies go
	if th, ok := s.threads[k]; ok {
		keep := th[:0]
		for i := range th {
			if th[i].TS == ts || !gone[th[i].TS] {
				keep = append(keep, th[i])
			}
		}
		s.threads[k] = keep
	}
	return true
}

// resetThreadEndsLocked is resetReachedEnd for every thread history (the
// threads of ch, all with ch ""), the thread keys of the one reset Slack
// makes at a slow connect (ResetReachedEnd: resetReachedEnd({}) covers
// every key, a channel's and its threads'): what the socket would have
// added past each end is unknown. prevReachedEnd keeps that it had
// reached it (the pane keeps drawing its end and its reply box:
// getPotentiallyOutdatedTimestamps), and a reply the socket brings now
// makes a slice of its own (addTimestamps appends only while reachedEnd),
// which the pane's loadPost joins. It reports how many it reset; s.mu
// held.
func (s *Store) resetThreadEndsLocked(ch string) int {
	n := 0
	for k, a := range s.threadHist {
		if ch != "" && !strings.HasPrefix(k, ch+"/") {
			continue
		}
		if a.reachedEnd.isTrue() {
			a.prevReachedEnd = true
			a.reachedEnd = tUnset // null: neither true nor false
			n++
		}
	}
	return n
}

// ResetThreadReachedEnd is resetReachedEnd for the one thread ch/ts
// (requestReplies' threads_perf_quality read, when the root's
// latest_reply had moved past the page it read).
func (s *Store) ResetThreadReachedEnd(ch, ts string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if a := s.threadHist[ch+"/"+ts]; a != nil && a.reachedEnd.isTrue() {
		a.prevReachedEnd = true
		a.reachedEnd = tUnset
	}
}

// removeCachedReplyLocked is removeCachedReplyTimestamps for the thread
// key k: evicted timestamps leave its slices (which keep their ranges).
func (s *Store) removeCachedReplyLocked(k string, gone map[string]bool) {
	a := s.threadHist[k]
	if a == nil {
		return
	}
	for i := range a.slices {
		e := &a.slices[i]
		kept := e.tss[:0:0]
		for _, x := range e.tss {
			if !gone[x] {
				kept = append(kept, x)
			}
		}
		if len(kept) != len(e.tss) {
			e.tss = kept
		}
	}
}

// ThreadSlice is what the thread pane reads of a thread's history: the
// slice its start ts falls in (getPotentiallyOutdatedTimestamps), its
// range and timestamps, and whether it is the first slice of a history
// that reached the thread's start, the last of one that reached its end,
// and the last of one that had reached it before a disconnect.
type ThreadSlice struct {
	Start, End               string
	TSs                      []string
	ReachedStart, ReachedEnd bool
	PrevReachedEnd           bool
}

// Empty is dTxY's f: no slice at all.
func (t ThreadSlice) Empty() bool { return t.Start == "" && t.End == "" && len(t.TSs) == 0 }

// ThreadSliceAt is getPotentiallyOutdatedTimestamps(threadKey, start):
// with no start the last slice of a history that reached the end, with
// "0000000000.000000" the first of one that reached the start, else the
// slice whose range holds start, or the last (first) slice when start is
// past the end (before the start) of a history that reached it; the empty
// slice when none. ok is false when nothing loaded the thread.
func (s *Store) ThreadSliceAt(ch, ts, start string) (ThreadSlice, bool) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	a := s.threadHist[ch+"/"+ts]
	if a == nil {
		return ThreadSlice{}, false
	}
	if len(a.slices) == 0 {
		return ThreadSlice{}, true
	}
	r, found := a.sliceAt(start)
	if a.prevReachedEnd {
		last := a.slices[len(a.slices)-1]
		if start != "" || found {
			r.PrevReachedEnd = found && r.End == last.end
			return r, true
		}
		r = view(last, len(a.slices) == 1 && a.reachedStart.isTrue(), a.reachedEnd.isTrue())
		r.PrevReachedEnd = true
		return r, true
	}
	return r, true
}

func view(t tslice, reachedStart, reachedEnd bool) ThreadSlice {
	return ThreadSlice{Start: t.start, End: t.end, TSs: append([]string(nil), t.tss...), ReachedStart: reachedStart, ReachedEnd: reachedEnd}
}

// sliceAt is getTimestampsByChannelId (dTxY tp) over the history's
// slices (callers hold s.mu and saw a slice); found is false for f.
func (a *threadHist) sliceAt(n string) (ThreadSlice, bool) {
	r := a.slices
	s, o := r[0], r[len(r)-1]
	one := len(r) == 1
	if n == "" {
		if a.reachedEnd.isTrue() {
			return view(o, one && a.reachedStart.isTrue(), true), true
		}
		return ThreadSlice{}, false
	}
	if n == "0000000000.000000" {
		if a.reachedStart.isTrue() {
			return view(s, true, one && a.reachedEnd.isTrue()), true
		}
		return ThreadSlice{}, false
	}
	l := createSlice([]string{n}, "", "")
	for e := range r {
		if intersects(&r[e], &l) {
			return view(r[e], e == 0 && a.reachedStart.isTrue(), e == len(r)-1 && a.reachedEnd.isTrue()), true
		}
	}
	if a.reachedEnd.isTrue() && tsCmp(o.end, n) < 0 {
		return view(o, one && a.reachedStart.isTrue(), true), true
	}
	if a.reachedStart.isTrue() && s.start != "" && tsCmp(s.start, n) > 0 {
		return view(s, true, one && a.reachedEnd.isTrue()), true
	}
	return ThreadSlice{}, false
}

// ThreadHeld reports whether a replies read loaded anything of the thread
// (channelHasAnyHistory).
func (s *Store) ThreadHeld(ch, ts string) bool {
	s.mu.RLock()
	defer s.mu.RUnlock()
	a := s.threadHist[ch+"/"+ts]
	return a != nil && len(a.slices) > 0
}

// ThreadSliceMsgs is the messages of the slice t of thread ch/ts, in its
// order: the copies the store holds (a timestamp it no longer holds is
// left out).
func (s *Store) ThreadSliceMsgs(ch, ts string, t ThreadSlice) []slack.Message {
	s.mu.RLock()
	defer s.mu.RUnlock()
	th := s.threads[ch+"/"+ts]
	out := make([]slack.Message, 0, len(t.TSs))
	j := 0
	for _, want := range t.TSs {
		// both ascending: walk the thread once
		for j < len(th) && TSLess(th[j].TS, want) {
			j++
		}
		if j < len(th) && th[j].TS == want {
			out = append(out, th[j])
		}
	}
	return out
}

// AddThreadPageSince is addMessagesFromHistory for one replies read of
// thread ch/ts whose request began at LiveSeq since: msgs is the answer
// (the root with it), which the messages store keeps as SetThreadSince
// did (a message the socket changed or deleted after since keeps the
// socket's copy or stays out, and a reply the socket brought inside the
// read's range that the answer predates is kept); tss is what of it the
// read's slice lists (requestReplies leaves the root out of a page short
// of the thread's start), b its bounds. It returns the copies kept.
func (s *Store) AddThreadPageSince(ch, ts string, msgs []slack.Message, tss []string, b ThreadPageBounds, since uint64) []slack.Message {
	s.mu.Lock()
	defer s.mu.Unlock()
	k := ch + "/" + ts
	newer := func(t string) (liveRec, bool) {
		r, ok := s.live[ch+"/"+t]
		return r, ok && r.seq > since
	}
	list := make(map[string]bool, len(tss))
	for _, t := range tss {
		list[t] = true
	}
	out := make([]slack.Message, 0, len(msgs)+1)
	have := make(map[string]bool, len(msgs))
	for _, m := range msgs {
		have[m.TS] = true
		if r, ok := newer(m.TS); ok {
			if r.gone {
				delete(list, m.TS)
				continue
			}
			mm := *r.msg
			s.keepPinLocked(ch, &mm, &m)
			if mm.ThreadTS == "" { // a message_changed copy without thread_ts stays in its thread
				mm.ThreadTS = m.ThreadTS
			}
			m = mm
		}
		out = append(out, m)
	}
	// the read's range: its bounds, else what it listed, and no bound at
	// an end it reached (a reply the socket brought past it belongs)
	lo, hi := b.Oldest, b.Latest
	if len(tss) > 0 {
		sorted := append([]string(nil), tss...)
		slices.SortFunc(sorted, tsCmp)
		if lo == "" {
			lo = sorted[0]
		}
		if hi == "" {
			hi = sorted[len(sorted)-1]
		}
	}
	moreStart, moreEnd := b.hasMoreEnds()
	if !moreStart {
		lo = ""
	}
	if !moreEnd {
		hi = ""
	}
	inRange := func(t string) bool {
		return (lo == "" || !TSLess(t, lo)) && (hi == "" || !TSLess(hi, t))
	}
	for key, r := range s.live {
		if r.seq <= since || r.gone || r.msg == nil || len(key) <= len(ch) || key[:len(ch)+1] != ch+"/" {
			continue
		}
		if m := r.msg; !have[m.TS] && m.ThreadTS == ts && m.TS != ts && inRange(m.TS) {
			out = append(out, *m)
			list[m.TS] = true
		}
	}
	SortAsc(out)
	s.replayAllReactsLocked(ch, out, since) // the socket's reactions meanwhile (reacts.go)
	s.readThreadLocked(ch, ts, out)         // a new revision reaches every copy, and core (read_revisions.go)
	// the messages store: the thread's held messages with these over them
	held := s.threads[k]
	byTS := make(map[string]int, len(held))
	for i := range held {
		byTS[held[i].TS] = i
	}
	merged := append([]slack.Message(nil), held...)
	for _, m := range out {
		if i, ok := byTS[m.TS]; ok {
			merged[i] = m
		} else {
			merged = append(merged, m)
		}
	}
	SortAsc(merged)
	s.threads[k] = merged
	// channelHistory: the read's timestamps as its slice
	keep := make([]string, 0, len(list))
	for t := range list {
		keep = append(keep, t)
	}
	s.addTimestampsLocked(k, keep, b)
	return out
}

// ThreadRepliesIn is the ts of the replies of thread ch/ts the messages
// store holds from lo to hi ("": no bound), oldest first, but for those
// the socket brought, changed or deleted after since (a fetch's LiveSeq
// when it went out): what a replies answer covering lo to hi should have.
func (s *Store) ThreadRepliesIn(ch, ts, lo, hi string, since uint64) []string {
	s.mu.RLock()
	defer s.mu.RUnlock()
	var out []string
	for _, m := range s.threads[ch+"/"+ts] {
		if m.TS == ts || lo != "" && TSLess(m.TS, lo) || hi != "" && TSLess(hi, m.TS) {
			continue
		}
		if r, ok := s.live[ch+"/"+m.TS]; ok && r.seq > since {
			continue
		}
		out = append(out, m.TS)
	}
	return out
}

// setThreadWholeLocked makes msgs the thread's one slice, both ends
// reached: SetThread's and SetThreadSince's whole thread.
func (s *Store) setThreadWholeLocked(k string, msgs []slack.Message) {
	if s.threadHist == nil {
		s.threadHist = map[string]*threadHist{}
	}
	tss := make([]string, len(msgs))
	for i := range msgs {
		tss[i] = msgs[i].TS
	}
	sl := createSlice(tss, "", "")
	s.threadHist[k] = &threadHist{slices: []tslice{sl}, reachedStart: tTrue, reachedEnd: tTrue}
}

// liveReplyLocked is the messages processor's addTimestamps for a reply
// the socket (or the viewer's own send) brought to thread key k: no
// bounds, so it joins the last slice when the history reached the
// thread's end and is a slice of its own otherwise (6Yed's insert with
// append).
func (s *Store) liveReplyLocked(k, ts string) {
	if s.threadHist[k] == nil {
		return
	}
	s.addTimestampsLocked(k, []string{ts}, ThreadPageBounds{NoBounds: true})
}

// threadHistDropLocked forgets the thread key k's history (its cache
// went: an eviction of the whole thread).
func (s *Store) threadHistDropLocked(k string) { delete(s.threadHist, k) }

// ThreadHistory is channelHistory[threadKey] whole, as plugins read it:
// every slice, oldest first, and the flags (one never set reads false).
// ok is false when nothing loaded the thread.
func (s *Store) ThreadHistory(ch, ts string) (sl []ThreadSlice, reachedStart, reachedEnd, prevReachedEnd, ok bool) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	a := s.threadHist[ch+"/"+ts]
	if a == nil {
		return nil, false, false, false, false
	}
	for i := range a.slices {
		sl = append(sl, view(a.slices[i], i == 0 && a.reachedStart.isTrue(), i == len(a.slices)-1 && a.reachedEnd.isTrue()))
	}
	return sl, a.reachedStart.isTrue(), a.reachedEnd.isTrue(), a.prevReachedEnd, true
}
