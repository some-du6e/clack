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

	// failed loads back off (see backoff); no retries at all after auth fails
	fails, olderFails     int
	retryAt, olderRetryAt time.Time
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
	userRv uint64            // bumps on user/presence/emoji events
	// authFailed stops every load after core's auth_failed (or an auth
	// error) until the connection comes back or the user hits Retry.
	authFailed atomic.Bool
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

func (hb *hub) invalidate() {
	if hb.h != nil {
		hb.h.Invalidate()
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
	}
	m.used = time.Now()
	return m
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
			if err == nil {
				m.setAll(msgs)
				m.loaded = true
				m.fetched = time.Now()
				m.fails, m.retryAt = 0, time.Time{}
			} else {
				m.failed(hb, err, false, time.Now())
			}
			m.rev++
			m.mu.Unlock()
		} else {
			p, err := hb.app.History(ctx, m.ch, "")
			m.mu.Lock()
			m.loading = false
			m.err = err
			if err == nil {
				m.mergeNewest(p.Messages, p.HasMore)
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
		}
		hb.invalidate()
	}()
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
// history that precedes it.
func (m *model) mergeNewest(page []core.Message, hasMore bool) {
	if len(m.msgs) == 0 || len(page) == 0 {
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

func (hb *hub) run(evs <-chan core.Event) {
	for ev := range evs {
		if hb.apply(ev) {
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
		thread := ev.Msg.ThreadTS
		chm, thm := hb.models(ev.Channel, thread)
		changed := false
		if chm != nil {
			chm.mu.Lock()
			if ev.Type == "message" || chm.find(ev.Msg.TS) >= 0 || chm.visible(ev.Msg) {
				chm.upsert(*ev.Msg)
				if ev.Type == "message" {
					delete(chm.typing, ev.Msg.User)
				}
				chm.rev++
				changed = true
			}
			chm.mu.Unlock()
		}
		if thm != nil {
			thm.mu.Lock()
			thm.upsert(*ev.Msg)
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
			m.mu.Unlock()
		}
		return len(ms) > 0
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
		return true
	case "user", "presence", "emoji_changed", "prefs":
		hb.mu.Lock()
		hb.userRv++
		hb.mu.Unlock()
		return true
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
}

func (m *model) snapshot(dst *snap) bool {
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.rev == dst.rev && dst.rev != 0 {
		return false
	}
	dst.msgs = append(dst.msgs[:0], m.msgs...)
	dst.hasMore, dst.lastRead, dst.loaded, dst.loading, dst.older, dst.err = m.hasMore, m.lastRead, m.loaded, m.loading, m.older, m.err
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
