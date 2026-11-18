package messages

import (
	"context"
	"reflect"
	"sort"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/some-du6e/clack/internal/core"
)

// entry is one message in a model, with a revision that bumps whenever the
// message changes (so rows re-layout only what changed).
type entry struct {
	m   core.Message
	rev uint32
}

// model is the shared, goroutine-safe state of one conversation (or one
// thread). Network loads and realtime events mutate it; views copy it on
// the UI goroutine when rev changes. Models outlive views, so switching
// back to a channel is instant.
type model struct {
	mu       sync.Mutex
	ch       string
	threadTS string // "" for a channel timeline
	// id tells the hub's models apart (live.go: a revision of a message
	// is its model's and its entry's); set once, when made
	id uint32

	msgs     []entry // ascending
	hasMore  bool
	lastRead string
	loaded   bool
	loading  bool
	older    bool // loading an older page
	err      error
	rev      uint64
	nextRev  uint32
	typing   map[string]typingEntry
	used     time.Time
	fetched  time.Time // last full refresh
	refreshQ bool
	renames  [][2]string // pending ts -> confirmed ts, consumed by views
	jump     string      // a reply to scroll to (taut jumpToReply), consumed by views
	// newestMissing: the window misses the newest page (snap.newestMissing)
	newestMissing bool
	// live: messages an event changed (true: deleted) while a load was
	// out; the load's answer, read before them, keeps them (keepLive)
	live map[string]bool
	// livePlace: updates of messages the model didn't hold while a load
	// was out (one outside what it loaded, or anything before its first
	// load): the answer takes each where it holds the message, and
	// nowhere else (keepLive)
	livePlace map[string]core.Message

	// failed loads back off (see backoff); no retries at all after auth fails
	fails, olderFails     int
	retryAt, olderRetryAt time.Time

	// evictGen counts evict calls: sweepRows releases the rows of lists
	// not on screen whose model was trimmed since they last looked
	evictGen uint64

	// drawn is the frame (gtx.Now) a list last drew the model at; the
	// hub's mu guards it (markDrawn, onScreen)
	drawn time.Time

	// reconv is what changed of what Docs read while a load was out
	// (reconvert): its answer may hold messages converted before it, so
	// they're converted again once it lands (afterLoad)
	reconv core.RefChange
}

// backoff is the wait after the n-th consecutive failed load: 1 s, 2 s,
// 4 s … capped at maxBackoff.
func backoff(n int) time.Duration {
	d := time.Second
	for i := 1; i < n && d < maxBackoff; i++ {
		d *= 2
	}
	return min(d, maxBackoff)
}

const maxBackoff = time.Minute

// authError reports Slack errors that mean the session is gone: retrying
// can't help until the user signs in again.
func authError(err error) bool {
	if err == nil {
		return false
	}
	s := err.Error()
	for _, code := range []string{"invalid_auth", "not_authed", "token_revoked", "token_expired", "account_inactive", "auth_failed"} {
		if strings.Contains(s, code) {
			return true
		}
	}
	return false
}

// failed records a failed load (caller holds m.mu).
func (m *model) failed(hb *hub, err error, older bool, now time.Time) {
	if older {
		m.olderFails++
		m.olderRetryAt = now.Add(backoff(m.olderFails))
	} else {
		m.fails++
		m.retryAt = now.Add(backoff(m.fails))
	}
	if authError(err) {
		hb.authFailed.Store(true)
	}
}

type typingEntry struct {
	name string
	at   time.Time
}

type hub struct {
	h      Host
	app    *core.App
	mu     sync.Mutex
	chans  map[string]*model
	thread map[string]*model // key ch + "/" + ts
	nextID uint32            // the last model's id (guarded by mu)
	userRv uint64            // bumps on profile, name, pref and members-cache events
	// authFailed stops every load after core's auth_failed (or an auth
	// error) until the connection comes back or the user hits Retry.
	authFailed atomic.Bool
	// frame is the latest frame a list drew at (markDrawn): the models
	// drawn then are the ones on screen
	frame time.Time
}

// markDrawn notes that a list drew m in the frame at (gtx.Now).
func (hb *hub) markDrawn(m *model, at time.Time) {
	hb.mu.Lock()
	if at.After(hb.frame) {
		hb.frame = at
	}
	m.drawn = at
	hb.mu.Unlock()
}

// onScreen reports whether a list of ch (the channel or one of its
// threads) drew in the latest frame a list drew.
func (hb *hub) onScreen(ch string) bool {
	hb.mu.Lock()
	defer hb.mu.Unlock()
	if m := hb.chans[ch]; m != nil && m.drawn.Equal(hb.frame) {
		return true
	}
	for _, m := range hb.thread {
		if m.ch == ch && m.drawn.Equal(hb.frame) {
			return true
		}
	}
	return false
}

// drawsMember reports whether a list drew the member a profile change is
// about in the latest frame a list drew: every row reads its sender and
// every mention its member as it draws (store.MemberAccessed, which the
// event carries from before the change: core.Event.Seen).
func (hb *hub) drawsMember(ev core.Event) bool {
	if ev.Seen.IsZero() {
		return false
	}
	hb.mu.Lock()
	frame := hb.frame
	hb.mu.Unlock()
	return frame.IsZero() || ev.Seen.UnixMilli() >= frame.UnixMilli()
}

// redraws reports whether ev's change to the models shows: a
// conversation's in a list on screen (the 40 models kept for lists left
// earlier take a busy workspace's messages, edits and reactions too, many
// a second, and drew nothing new for them), and the members', prefs' and
// connection's changes every list reads.
func (hb *hub) redraws(ev core.Event) bool {
	switch ev.Type {
	case "message", "update", "sent", "delete", "evicted":
		return hb.onScreen(ev.Channel)
	}
	return true
}

var (
	hubsMu sync.Mutex
	hubs   = map[*core.App]*hub{}
)

const maxModels = 40

func hubFor(h Host) *hub {
	app := h.Core()
	hubsMu.Lock()
	defer hubsMu.Unlock()
	if hb, ok := hubs[app]; ok {
		hb.h = h
		return hb
	}
	hb := &hub{h: h, app: app, chans: map[string]*model{}, thread: map[string]*model{}}
	hubs[app] = hb
	if app != nil {
		evs, _ := app.Subscribe()
		go hb.run(evs)
	}
	return hb
}

// invalidate asks the current host for a frame. hubFor swaps hb.h under
// hubsMu on the UI goroutine while load goroutines call this, so the read
// takes the lock too (polish-1113: -race caught it in TestChannelLinkTargets).
func (hb *hub) invalidate() {
	hubsMu.Lock()
	h := hb.h
	hubsMu.Unlock()
	if h != nil {
		h.Invalidate()
	}
}

func (hb *hub) channel(ch string) *model {
	hb.mu.Lock()
	defer hb.mu.Unlock()
	m, ok := hb.chans[ch]
	if !ok {
		m = &model{ch: ch, typing: map[string]typingEntry{}}
		hb.chans[ch] = m
		hb.evict(hb.chans)
		m.id = hb.newID()
	}
	m.used = time.Now()
	return m
}

func (hb *hub) threadModel(ch, ts string) *model {
	hb.mu.Lock()
	defer hb.mu.Unlock()
	k := ch + "/" + ts
	m, ok := hb.thread[k]
	if !ok {
		m = &model{ch: ch, threadTS: ts, typing: map[string]typingEntry{}}
		hb.thread[k] = m
		hb.evict(hb.thread)
		m.id = hb.newID()
	}
	m.used = time.Now()
	return m
}

// newID is a new model's id (hb.mu held).
func (hb *hub) newID() uint32 {
	hb.nextID++
	return hb.nextID
}

func (hb *hub) evict(ms map[string]*model) {
	if len(ms) <= maxModels {
		return
	}
	var oldest string
	var t time.Time
	for k, m := range ms {
		if oldest == "" || m.used.Before(t) {
			oldest, t = k, m.used
		}
	}
	delete(ms, oldest)
}

// ---- loading ----

// ensureLoaded starts the first load (or a background refresh of a cached
// model). Never blocks.
func (hb *hub) ensureLoaded(m *model) {
	if hb.authFailed.Load() {
		return
	}
	now := time.Now()
	m.mu.Lock()
	if m.loading || (m.loaded && now.Sub(m.fetched) < 30*time.Second) || now.Before(m.retryAt) {
		m.mu.Unlock()
		return
	}
	m.loading = true
	m.mu.Unlock()
	go func() {
		ctx, cancel := context.WithTimeout(context.Background(), 45*time.Second)
		defer cancel()
		if m.threadTS != "" {
			msgs, err := hb.app.Replies(ctx, m.ch, m.threadTS)
			m.mu.Lock()
			m.loading = false
			m.err = err
			live := m.takeLive()
			if err == nil {
				m.setAll(msgs)
				m.keepLive(live)
				m.loaded = true
				m.fetched = time.Now()
				m.fails, m.retryAt = 0, time.Time{}
			} else {
				m.failed(hb, err, false, time.Now())
			}
			m.rev++
			m.mu.Unlock()
			hb.afterLoad(m)
		} else {
			p, err := hb.app.History(ctx, m.ch, "")
			m.mu.Lock()
			m.loading = false
			m.err = err
			live := m.takeLive()
			if err == nil {
				m.mergeNewest(p.Messages, p.HasMore)
				m.keepLive(live)
				if !m.loaded {
					m.lastRead = p.LastRead
				}
				m.loaded = true
				m.fetched = time.Now()
				m.fails, m.retryAt = 0, time.Time{}
			} else {
				m.failed(hb, err, false, time.Now())
			}
			m.rev++
			m.mu.Unlock()
			hb.afterLoad(m)
		}
		hb.invalidate()
	}()
}

// afterLoad converts again what of m's messages read something that
// changed while its load was out (reconvert recorded it).
func (hb *hub) afterLoad(m *model) {
	m.mu.Lock()
	c := m.reconv
	if !m.loading && !m.older {
		m.reconv = core.RefChange{}
	} else {
		c = core.RefChange{} // another load is still out: it does this
	}
	m.mu.Unlock()
	if !c.Empty() {
		hb.reconvertIn([]*model{m}, c)
	}
}

// retry is the Retry link: forget the backoff (and an auth stop) and load.
func (hb *hub) retry(m *model) {
	hb.authFailed.Store(false)
	m.mu.Lock()
	m.fails, m.retryAt, m.olderFails, m.olderRetryAt = 0, time.Time{}, 0, time.Time{}
	m.fetched = time.Time{}
	m.mu.Unlock()
	hb.ensureLoaded(m)
}

// loadOlder fetches the page before the oldest loaded message.
func (hb *hub) loadOlder(m *model) {
	if hb.authFailed.Load() {
		return
	}
	m.mu.Lock()
	if m.older || !m.hasMore || len(m.msgs) == 0 || m.threadTS != "" || time.Now().Before(m.olderRetryAt) {
		m.mu.Unlock()
		return
	}
	m.older = true
	before := m.msgs[0].m.TS
	m.rev++
	m.mu.Unlock()
	hb.invalidate()
	go func() {
		ctx, cancel := context.WithTimeout(context.Background(), 45*time.Second)
		defer cancel()
		p, err := hb.app.History(ctx, m.ch, before)
		m.mu.Lock()
		m.older = false
		if err == nil {
			m.prepend(p.Messages, p.HasMore)
			m.olderFails, m.olderRetryAt = 0, time.Time{}
		} else {
			m.err = err
			m.failed(hb, err, true, time.Now())
		}
		m.rev++
		m.mu.Unlock()
		hb.afterLoad(m)
		hb.invalidate()
	}()
}

// ---- model mutation (callers hold m.mu) ----

func (m *model) bump() uint32 {
	m.nextRev++
	return m.nextRev
}

func (m *model) visible(msg *core.Message) bool {
	if m.threadTS != "" {
		return msg.ThreadTS == m.threadTS || msg.TS == m.threadTS
	}
	// Channel timeline: top-level messages and broadcast replies.
	return msg.ThreadTS == "" || msg.ThreadTS == msg.TS || msg.Broadcast
}

func (m *model) setAll(msgs []core.Message) {
	old := make(map[string]*entry, len(m.msgs))
	for i := range m.msgs {
		old[m.msgs[i].m.TS] = &m.msgs[i]
	}
	out := make([]entry, 0, len(msgs))
	for _, x := range msgs {
		if !m.visible(&x) {
			continue
		}
		if o, ok := old[x.TS]; ok && sameMessage(&o.m, &x) {
			out = append(out, entry{m: x, rev: o.rev})
			continue
		}
		out = append(out, entry{m: x, rev: m.bump()})
	}
	sort.SliceStable(out, func(i, j int) bool { return tsLess(out[i].m.TS, out[j].m.TS) })
	m.msgs = out
}

// mergeNewest replaces the newest window with page, keeping older loaded
// history that precedes it. A model that hasn't loaded (its first load,
// or the first after a whole eviction) has no history to keep: the page
// is the list, and says whether older messages exist (what the events
// left in it meanwhile comes back through keepLive).
func (m *model) mergeNewest(page []core.Message, hasMore bool) {
	if len(m.msgs) == 0 || len(page) == 0 || !m.loaded {
		m.setAll(page)
		m.hasMore = hasMore
		return
	}
	first := page[0].TS
	var keep []core.Message
	for _, e := range m.msgs {
		if tsLess(e.m.TS, first) {
			keep = append(keep, e.m)
		}
	}
	if len(keep) == 0 {
		m.hasMore = hasMore
	}
	m.setAll(append(keep, page...))
}

// evict drops the messages before oldest, or everything when oldest is ""
// (the whole history went: the next view loads it again), as core's store
// did. The callers hold m.mu.
func (m *model) evict(oldest string) {
	if oldest == "" {
		m.msgs = nil
		m.loaded, m.hasMore, m.lastRead = false, false, ""
		m.fetched = time.Time{}
	} else {
		i := sort.Search(len(m.msgs), func(i int) bool { return !tsLess(m.msgs[i].m.TS, oldest) })
		if i == 0 {
			return
		}
		m.msgs = append([]entry(nil), m.msgs[i:]...)
		m.hasMore = true
	}
	m.evictGen++
	m.rev++
}

func (m *model) prepend(page []core.Message, hasMore bool) {
	all := make([]core.Message, 0, len(page)+len(m.msgs))
	all = append(all, page...)
	for _, e := range m.msgs {
		all = append(all, e.m)
	}
	// dedupe by ts
	seen := make(map[string]bool, len(all))
	out := all[:0]
	for _, x := range all {
		if seen[x.TS] {
			continue
		}
		seen[x.TS] = true
		out = append(out, x)
	}
	m.setAll(out)
	m.hasMore = hasMore
}

func (m *model) find(ts string) int {
	i := sort.Search(len(m.msgs), func(i int) bool { return !tsLess(m.msgs[i].m.TS, ts) })
	if i < len(m.msgs) && m.msgs[i].m.TS == ts {
		return i
	}
	return -1
}

func (m *model) upsert(msg core.Message) {
	if !m.visible(&msg) {
		// a reply taken back from the conversation (removeBroadcast) leaves
		// its list; its thread keeps it
		m.remove(msg.TS)
		return
	}
	if i := m.find(msg.TS); i >= 0 {
		m.msgs[i] = entry{m: msg, rev: m.bump()}
		return
	}
	// Replace an optimistic pending message with the same text from self.
	i := sort.Search(len(m.msgs), func(i int) bool { return !tsLess(m.msgs[i].m.TS, msg.TS) })
	m.msgs = append(m.msgs, entry{})
	copy(m.msgs[i+1:], m.msgs[i:])
	m.msgs[i] = entry{m: msg, rev: m.bump()}
}

// noteLive records that an event changed (or deleted) ts while a load is
// out (callers hold m.mu).
func (m *model) noteLive(ts string, gone bool) {
	if !m.loading || ts == "" {
		return
	}
	if m.live == nil {
		m.live = map[string]bool{}
	}
	m.live[ts] = gone
	delete(m.livePlace, ts) // the model's entry (or its deletion) is newer
}

// notePlace records an update of msg, which the model doesn't hold, while
// a load is out: the answer may hold the message, read before the update
// (callers hold m.mu).
func (m *model) notePlace(msg core.Message) {
	if !m.loading || msg.TS == "" {
		return
	}
	if m.livePlace == nil {
		m.livePlace = map[string]core.Message{}
	}
	m.livePlace[msg.TS] = msg
}

// liveChanges is what the events changed during a load: at, each
// message's entry then (nil for one deleted); place, the updates of
// messages the model didn't hold, which go only where the answer holds
// them.
type liveChanges struct {
	at    map[string]*entry
	place map[string]core.Message
}

// takeLive is what the events changed during the load that just answered
// (callers hold m.mu).
func (m *model) takeLive() liveChanges {
	var out liveChanges
	if len(m.live) > 0 {
		out.at = make(map[string]*entry, len(m.live))
		for ts, gone := range m.live {
			if i := m.find(ts); !gone && i >= 0 {
				e := m.msgs[i]
				out.at[ts] = &e
			} else if gone {
				out.at[ts] = nil
			}
		}
	}
	out.place = m.livePlace
	m.live, m.livePlace = nil, nil
	return out
}

// keepLive puts back what the events changed during a load after its
// answer replaced the list: Slack's message_changed replaces the stored
// message at once, so an answer read before an edit (or a delete, or a
// new reply) mustn't show the old state again. A change goes back where
// the answer holds the message or inside the answer's range (covers): an
// answer that starts the list over (msg-latency's takeNewest, past a gap)
// takes back none of the messages from before its range, which load-older
// would then read before. An update of a message the model didn't hold
// changes the answer's copy, and adds nothing (callers hold m.mu).
func (m *model) keepLive(live liveChanges) {
	for ts, e := range live.at {
		switch {
		case e == nil:
			m.remove(ts)
		case m.find(ts) >= 0 || m.covers(ts):
			m.upsert(e.m)
		}
	}
	for ts, msg := range live.place {
		if _, ok := live.at[ts]; !ok && m.find(ts) >= 0 {
			m.upsert(msg)
		}
	}
}

// inRange reports whether ts falls inside what the channel model loaded:
// at or after its oldest message from Slack, or anywhere once it holds the
// whole history (no hasMore). A model that hasn't loaded holds no range
// (callers hold m.mu).
func (m *model) inRange(ts string) bool {
	return m.loaded && m.covers(ts)
}

// covers reports whether ts falls inside the list's range as it holds it
// now (inRange without the loaded check: keepLive asks it of an answer
// that just landed). The range starts at the oldest message from Slack,
// not at a local copy (a pending or failed send, an ephemeral message: no
// history page holds one, and msg-latency keeps them whatever the page's
// range), which is no boundary: load-older reads from the oldest message
// from Slack (msg-latency's oldestFromSlack), and the range is the same
// (callers hold m.mu).
func (m *model) covers(ts string) bool {
	if !m.hasMore {
		return true
	}
	for i := range m.msgs {
		if x := &m.msgs[i].m; !x.Pending && !x.Failed && !x.Ephemeral {
			return !tsLess(ts, x.TS)
		}
	}
	return false
}

func (m *model) remove(ts string) {
	if i := m.find(ts); i >= 0 {
		m.msgs = append(m.msgs[:i], m.msgs[i+1:]...)
	}
}

func sameMessage(a, b *core.Message) bool {
	// all of it: a message converted again with names it didn't have
	// before (a mention, a channel, custom emoji) has the same text and
	// block count but another doc, and a row keeping its rev would draw
	// the old one (wrapped differently) until something else refreshed it
	return reflect.DeepEqual(a, b)
}

// sameView is the looser test applyView (taut.go) uses on the plugins'
// view of a message every viewRefresh: the fields a view change shows in.
func sameView(a, b *core.Message) bool {
	if a.Text != b.Text || a.EditedTS != b.EditedTS || a.ReplyCount != b.ReplyCount || a.LatestReply != b.LatestReply ||
		a.Pinned != b.Pinned || a.Pending != b.Pending || len(a.Reactions) != len(b.Reactions) ||
		len(a.Doc.Blocks) != len(b.Doc.Blocks) || a.Name != b.Name || a.Avatar != b.Avatar {
		return false
	}
	for i := range a.Reactions {
		ra, rb := &a.Reactions[i], &b.Reactions[i]
		if ra.Name != rb.Name || ra.Count != rb.Count || ra.Self != rb.Self || ra.URL != rb.URL {
			return false
		}
	}
	return true
}

// ---- realtime ----

// ApplyEvent applies a core event to app's message models now, on the
// caller's goroutine: a reaction picked this frame (core.ReactBegin, Slack's
// optimistic update) draws in the next frame instead of after the
// subscription's goroutine hands it over. The subscription's copy of the
// same event lands later and changes nothing.
func ApplyEvent(app *core.App, ev core.Event) {
	hubsMu.Lock()
	hb := hubs[app]
	hubsMu.Unlock()
	if hb != nil && hb.apply(ev) && hb.redraws(ev) {
		hb.invalidate()
	}
}

func (hb *hub) run(evs <-chan core.Event) {
	for ev := range evs {
		if hb.apply(ev) && hb.redraws(ev) {
			hb.invalidate()
		}
	}
}

func (hb *hub) models(ch, threadTS string) (chm, thm *model) {
	hb.mu.Lock()
	defer hb.mu.Unlock()
	chm = hb.chans[ch]
	if threadTS != "" {
		thm = hb.thread[ch+"/"+threadTS]
	}
	return
}

func (hb *hub) apply(ev core.Event) bool {
	switch ev.Type {
	case "message", "update":
		if ev.Msg == nil {
			return false
		}
		if ev.Type == "update" && ev.Name == core.UpdateReactions {
			return hb.applyReactions(ev)
		}
		thread := ev.Msg.ThreadTS
		chm, thm := hb.models(ev.Channel, thread)
		changed := false
		if chm != nil {
			chm.mu.Lock()
			// an update changes the list's message where it holds it, and
			// goes in only inside what the list loaded: a revision of an
			// old message the list never loaded (core announces a read's:
			// an Activity row's copy, a Threads root, a refetch) taken in
			// above the oldest loaded would skip the history between at
			// the next load-older, whose cursor is the oldest message
			switch {
			case ev.Type == "message" || chm.find(ev.Msg.TS) >= 0 || chm.visible(ev.Msg) && chm.inRange(ev.Msg.TS):
				chm.upsert(*ev.Msg)
				chm.noteLive(ev.Msg.TS, false)
				if ev.Type == "message" {
					delete(chm.typing, ev.Msg.User)
				}
				chm.rev++
				changed = true
			case chm.visible(ev.Msg):
				chm.notePlace(*ev.Msg) // a load out may hold it
			}
			chm.mu.Unlock()
		}
		if thm != nil {
			thm.mu.Lock()
			thm.upsert(*ev.Msg)
			thm.noteLive(ev.Msg.TS, false)
			if ev.Type == "message" {
				delete(thm.typing, ev.Msg.User)
			}
			thm.rev++
			thm.mu.Unlock()
			changed = true
		}
		// A parent update (reply count) also refreshes an open thread's parent.
		if ev.Msg.ThreadTS == "" || ev.Msg.ThreadTS == ev.Msg.TS {
			if _, t2 := hb.models(ev.Channel, ev.Msg.TS); t2 != nil {
				t2.mu.Lock()
				t2.upsert(*ev.Msg)
				t2.noteLive(ev.Msg.TS, false)
				t2.rev++
				t2.mu.Unlock()
				changed = true
			}
		}
		return changed
	case "sent":
		// A confirmed optimistic send: swap the pending copy (ReplacesTS) for
		// the real message in place; views move the row to the new ts so it
		// isn't re-laid out (no flicker).
		if ev.Msg == nil {
			return false
		}
		chm, thm := hb.models(ev.Channel, ev.Msg.ThreadTS)
		for _, m := range []*model{chm, thm} {
			if m == nil {
				continue
			}
			m.mu.Lock()
			if ev.ReplacesTS != "" && ev.ReplacesTS != ev.Msg.TS && m.find(ev.ReplacesTS) >= 0 {
				m.remove(ev.ReplacesTS)
				m.renames = append(m.renames, [2]string{ev.ReplacesTS, ev.Msg.TS})
			}
			m.upsert(*ev.Msg)
			m.rev++
			m.mu.Unlock()
		}
		// a reply's parent (reply count) arrives as an "update"
		return chm != nil || thm != nil
	case "delete":
		hb.mu.Lock()
		var ms []*model
		if m := hb.chans[ev.Channel]; m != nil {
			ms = append(ms, m)
		}
		for _, m := range hb.thread {
			if m.ch == ev.Channel {
				ms = append(ms, m)
			}
		}
		hb.mu.Unlock()
		for _, m := range ms {
			m.mu.Lock()
			if m.find(ev.TS) >= 0 {
				m.remove(ev.TS)
				m.rev++
			}
			m.noteLive(ev.TS, true)
			m.mu.Unlock()
		}
		return len(ms) > 0
	case "evicted":
		// core trimmed the conversation (or thread) to Slack's message
		// cache budget (core/evict.go): its model drops the same messages,
		// or starts over when the whole history went, and the lists
		// showing it let their rows go (sweepRows)
		chm, thm := hb.models(ev.Channel, ev.ThreadTS)
		m := chm
		if ev.ThreadTS != "" {
			m = thm
		}
		if m == nil {
			return false
		}
		m.mu.Lock()
		m.evict(ev.TS)
		m.mu.Unlock()
		return true
	case "typing":
		chm, thm := hb.models(ev.Channel, ev.ThreadTS)
		m := chm
		if ev.ThreadTS != "" {
			m = thm
		}
		if m == nil {
			return false
		}
		m.mu.Lock()
		m.typing[ev.UserID] = typingEntry{name: ev.Name, at: time.Now()}
		m.rev++
		m.mu.Unlock()
		// the list's next sync reads it; nothing it draws shows typing
		// (the composer's typing line has its own hub, composer/typing.go)
		return false
	case "presence", "emoji_changed":
		// No row draws presence, and a row reads its emoji as it draws:
		// redrawing every list for them drew nothing new, many times a
		// second on a busy workspace's socket (Karim 2026-11-14)
		return false
	case "user", "prefs", "names", "members_evicted":
		// user: a profile change, for a member a list drew in its latest
		// frame (drawsMember); another draws nowhere now, and a row that
		// wants one it doesn't hold draws the unknown stub until its
		// fetch's "names" redraws it. names: core
		// fetched members (and names) a lookup missed, an evicted
		// member's among them (store/members.go): the rows that drew them
		// unknown resolve again. members_evicted: the members cache let
		// some go, and the rows read their members again, as Slack's
		// re-render on bulkRemoveMembersById (one that still draws an
		// evicted member asks for it)
		// Each also converts again the messages whose Doc names one of
		// the members (reconvert), drawn or not: a file's uploader, a
		// huddle's people, a message slug's author aren't the rows'
		// senders, so drawsMember never saw them read
		re := hb.reconvert(ev)
		if ev.Type == "user" && !hb.drawsMember(ev) {
			return re
		}
		hb.mu.Lock()
		hb.userRv++
		hb.mu.Unlock()
		return true
	case "conv", "conv_removed", "tabs":
		// a channel's name, icon, membership or tabs: the messages whose
		// Doc names it (a channel mention, a message slug's "in #x", a
		// tab slug) are converted again
		return hb.reconvert(ev)
	case "auth_failed":
		// core: the session is gone (signed out, token revoked). Stop
		// loading anything until the connection is back or Retry.
		hb.authFailed.Store(true)
		return true
	case "resync", "connection", "poll":
		if ev.Type == "connection" && ev.Connected {
			hb.authFailed.Store(false)
		}
		hb.mu.Lock()
		var ms []*model
		for _, m := range hb.chans {
			ms = append(ms, m)
		}
		for _, m := range hb.thread {
			ms = append(ms, m)
		}
		hb.mu.Unlock()
		for _, m := range ms {
			m.mu.Lock()
			m.fetched = time.Time{}
			if ev.Type == "connection" && ev.Connected {
				m.fails, m.retryAt, m.olderFails, m.olderRetryAt = 0, time.Time{}, 0, time.Time{}
			}
			m.refreshQ = true
			m.rev++
			m.mu.Unlock()
		}
		return true
	}
	return false
}

// reconvert converts again the messages whose Doc read a member or a
// conversation ev changed (core.RefsChanged, the ids each conversion
// recorded in Message.Refs), and only those: a profile change on a busy
// workspace converted nothing before, and every message's names stayed as
// they were until the model reloaded. Each model holding one takes the new
// Doc and keeps the rest of its copy, as a reaction change does
// (applyReactions): the store's copy can be another conversation's.
func (hb *hub) reconvert(ev core.Event) bool {
	c := core.RefsChanged(ev)
	if c.Empty() || hb.app == nil {
		return false
	}
	hb.mu.Lock()
	ms := make([]*model, 0, len(hb.chans)+len(hb.thread))
	for _, m := range hb.chans {
		ms = append(ms, m)
	}
	for _, m := range hb.thread {
		ms = append(ms, m)
	}
	hb.mu.Unlock()
	return hb.reconvertIn(ms, c)
}

// reconvertIn is reconvert over the models ms. Each conversion is
// StoredMessage's, whose reads don't count as a view's: a message
// converted again for a name in it touched the members it names, so their
// next profile change looked drawn (drawsMember) and every list redrew.
func (hb *hub) reconvertIn(ms []*model, c core.RefChange) bool {
	type key struct{ ch, ts string }
	var fresh map[key]*core.Message // converted once for every model holding it
	changed := false
	for _, m := range ms {
		m.mu.Lock()
		if m.loading || m.older {
			m.reconv = mergeRefChange(m.reconv, c) // its answer may be older than c
		}
		var stale []string
		for i := range m.msgs {
			if m.msgs[i].m.Refs.Reads(c) {
				stale = append(stale, m.msgs[i].m.TS)
			}
		}
		m.mu.Unlock()
		for _, ts := range stale {
			k := key{m.ch, ts}
			f, seen := fresh[k]
			if !seen {
				if v, ok := hb.app.StoredMessage(m.ch, ts); ok {
					f = &v
				}
				if fresh == nil {
					fresh = map[key]*core.Message{}
				}
				fresh[k] = f
			}
			if f == nil {
				continue // the store let it go: the next load converts it
			}
			m.mu.Lock()
			if i := m.find(ts); i >= 0 {
				msg := m.msgs[i].m
				if !reflect.DeepEqual(msg.Doc, f.Doc) || !reflect.DeepEqual(msg.Refs, f.Refs) {
					msg.Doc, msg.Refs = f.Doc, f.Refs
					m.msgs[i] = entry{m: msg, rev: m.bump()}
					m.rev++
					changed = true
				}
			}
			m.mu.Unlock()
		}
	}
	return changed
}

// mergeRefChange is a and b together.
func mergeRefChange(a, b core.RefChange) core.RefChange {
	return core.RefChange{Members: append(a.Members, b.Members...), Channels: append(a.Channels, b.Channels...),
		Files: append(a.Files, b.Files...), AnyMember: a.AnyMember || b.AnyMember}
}

// applyReactions is a reaction change: each model holding the message
// takes its reactions and keeps the rest of its copy (Slack's reactions
// reducer), so the thread pane's root, loaded by conversations.replies,
// isn't swapped for the channel timeline's copy. A model without the
// message gains nothing.
func (hb *hub) applyReactions(ev core.Event) bool {
	ts := ev.Msg.TS
	hb.mu.Lock()
	var ms []*model
	if m := hb.chans[ev.Channel]; m != nil {
		ms = append(ms, m)
	}
	for _, m := range hb.thread {
		if m.ch == ev.Channel {
			ms = append(ms, m)
		}
	}
	hb.mu.Unlock()
	changed := false
	for _, m := range ms {
		m.mu.Lock()
		if i := m.find(ts); i >= 0 {
			msg := m.msgs[i].m
			msg.Reactions = ev.Msg.Reactions
			m.msgs[i] = entry{m: msg, rev: m.bump()}
			m.noteLive(ts, false)
			m.rev++
			changed = true
		}
		m.mu.Unlock()
	}
	return changed
}

func (hb *hub) usersRev() uint64 {
	hb.mu.Lock()
	defer hb.mu.Unlock()
	return hb.userRv
}

// snapshot copies the model's visible state if it changed since rev.
type snap struct {
	msgs     []entry
	hasMore  bool
	lastRead string
	loaded   bool
	loading  bool
	older    bool
	err      error
	typing   []string
	rev      uint64
	refresh  bool
	renames  [][2]string
	jump     string
	// newestMissing is Slack's reachedEnd === false: the loaded window
	// stops short of the newest message (ArchivesBanner's "Latest
	// messages"). Clack loads history newest first and every refresh
	// merges the newest page (mergeNewest), so nothing sets it yet.
	newestMissing bool
}

func (m *model) snapshot(dst *snap) bool {
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.rev == dst.rev && dst.rev != 0 {
		return false
	}
	dst.msgs = append(dst.msgs[:0], m.msgs...)
	dst.hasMore, dst.lastRead, dst.loaded, dst.loading, dst.older, dst.err = m.hasMore, m.lastRead, m.loaded, m.loading, m.older, m.err
	dst.newestMissing = m.newestMissing
	dst.typing = dst.typing[:0]
	now := time.Now()
	for id, t := range m.typing {
		if now.Sub(t.at) > 6*time.Second {
			delete(m.typing, id)
			continue
		}
		dst.typing = append(dst.typing, t.name)
	}
	sort.Strings(dst.typing)
	dst.rev = m.rev
	if dst.rev == 0 {
		dst.rev = 1
		m.rev = 1
	}
	dst.refresh = m.refreshQ
	m.refreshQ = false
	dst.renames = append(dst.renames[:0], m.renames...)
	m.renames = m.renames[:0]
	dst.jump, m.jump = m.jump, ""
	return true
}

// mutate applies f to the message ts (optimistic local changes).
func (m *model) mutate(ts string, f func(*core.Message)) {
	m.mu.Lock()
	if i := m.find(ts); i >= 0 {
		f(&m.msgs[i].m)
		m.msgs[i].rev = m.bump()
		m.rev++
	}
	m.mu.Unlock()
}
