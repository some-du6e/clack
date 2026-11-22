package taut

import (
	"context"
	"encoding/json"
	"hash/fnv"
	"sort"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"time"
	"unicode"

	"golang.org/x/text/unicode/norm"

	"github.com/some-du6e/clack/internal/core"
	"github.com/some-du6e/clack/internal/slack"
)

// The Redux mirror's Go half. JS reads each slice (or one entry of an
// id-keyed slice) through taut_get / taut_keys on first use and caches it
// until Go invalidates it. The shapes are the ones Slack's web client keeps
// in its store (recorded from the live client, 2026-11): see
// docs/PLUGINS-state.md.

// mapSlices are id-keyed and read entry by entry.
var mapSlices = []string{
	"channels", "members", "messages", "bots", "membershipCounts", "membership",
	"readOnlyChannels", "threadOnlyChannels", "presence", "unreadChannelCounts",
	"channelCursors", "userGroups", "channelHistory", "teams", "reactions",
	"pendingFileUploads", "files", // the composer's attachments (files.go)
}

// wholeSlices are read whole.
var wholeSlices = []string{
	"bootData", "selfTeamIds", "prefs", "userPrefs", "teamPrefs", "channelSections", "typing",
	"dndV2", "drafts", "slashCommand", "experiments", "huddleEvents", "allCalls", "peers",
	"messageEditWindow", "searchResults", "huddlesCanvas", "alert", "unreadCounts",
	"mutedChannels", "starredChannels", "huddles", "messagesMeta", "threadSub", "userGroupMembership",
	"clientHuddles", "appActions",
}

type change struct {
	Slice string   `json:"slice"`
	Keys  []string `json:"keys,omitempty"`
	// ts: a change to that message of the bucket Keys[0] alone
	// (invalidateMsg): JS reads the bucket again all the same, but only
	// that message's view stops being ready
	ts string
}

type typingRec struct {
	ChannelID    string  `json:"channelId"`
	ThreadTs     *string `json:"threadTs"`
	MemberID     string  `json:"memberId"`
	MemberIsSelf bool    `json:"memberIsSelf"`
	Timestamp    int64   `json:"timestamp"`
	expires      time.Time
}

type membershipRec struct {
	IsKnown  bool `json:"isKnown"`
	IsMember bool `json:"isMember"`
}

type mirror struct {
	t *Taut

	mu         sync.Mutex
	typing     map[string]typingRec
	membership map[string]map[string]membershipRec
	editing    map[string]map[string]any
	drafts     map[string]core.Draft // server drafts by id (drafts.go)
	local      map[string]localDraft // the composers' live drafts by client_draft_id (drafts.go)
	search     json.RawMessage
	calls      map[string]json.RawMessage
	commands   json.RawMessage
	cmdLoading bool
	actions    json.RawMessage // the appActions store (appactions.go), nil until loaded
	actLoading bool
	toasts     []any
	watched    map[string]map[string]uint64 // slice → key → fingerprint JS last saw

	pending []change
	flush   *time.Timer
	cancel  context.CancelFunc
	started bool

	// What JS holds of the id-keyed slices whose changes come by the
	// thousand on a busy workspace (members, presence): the ids it read
	// since their last invalidation (jsRead), and whether it listed the
	// slice's keys since its last whole invalidation (jsListed: then any
	// id's change may be news). A change to an id JS doesn't hold has
	// nothing to tell it: it reads the store as it is when it first reads
	// the id. Every status change of anyone used to post JS a task that
	// re-ran every store listener. jsFull: too many ids to track, so
	// every change goes (until the next whole invalidation).
	jsRead   map[string]map[string]struct{}
	jsListed map[string]bool
	jsFull   map[string]bool

	// the invalidations posted to JS whose task hasn't taken them yet
	// (postInvalidate): later batches join it rather than queue tasks of
	// their own behind a busy runtime
	qmu     sync.Mutex
	queued  []change
	qposted bool
	qrt     *runtime

	// hold, set by HoldMirror (tests), stops the loop before it handles
	// the next core event until it's closed
	hold atomic.Pointer[chan struct{}]
	// handled, set by AfterMirrorEvent (tests), runs after the loop
	// handled an event, before it notes it handled (noteSeen)
	handled atomic.Pointer[func(ev core.Event)]
}

// HoldMirror (tests) stops taut's mirror before it handles the next core
// event, and every one after, until release: a mirror that's behind (a
// backlog of events ahead of one, a long sweep), which the views that
// wait for it (MessageViewState's need) must wait out.
func (t *Taut) HoldMirror() (release func()) {
	ch := make(chan struct{})
	t.mirror.hold.Store(&ch)
	var once sync.Once
	return func() {
		once.Do(func() {
			t.mirror.hold.Store(nil)
			close(ch)
		})
	}
}

// AfterMirrorEvent (tests) runs f on taut's mirror after it handles each
// core event and before it notes the event handled (noteSeen): what f
// emits is emitted while the mirror was busy with the event before.
func (t *Taut) AfterMirrorEvent(f func(ev core.Event)) (restore func()) {
	t.mirror.handled.Store(&f)
	return func() { t.mirror.handled.Store(nil) }
}

// sweepEvery is how often the mirror compares what it sent JS with the
// store (sweep): a change no event announces reaches JS within it.
var sweepEvery atomic.Int64

func init() { sweepEvery.Store(int64(750 * time.Millisecond)) }

// SetSweepEvery (tests) makes the mirrors started from now sweep every d:
// a long one leaves JS only what marks tell it.
func SetSweepEvery(d time.Duration) (restore func()) {
	prev := sweepEvery.Swap(int64(d))
	return func() { sweepEvery.Store(prev) }
}

// jsTracked are the slices whose changes go to JS only for ids it holds.
var jsTracked = map[string]bool{"members": true, "presence": true}

// jsReadMax bounds jsRead per slice: past it every change goes.
const jsReadMax = 50000

func newMirror(t *Taut) *mirror {
	return &mirror{
		t: t, typing: map[string]typingRec{}, membership: map[string]map[string]membershipRec{},
		editing: map[string]map[string]any{}, drafts: map[string]core.Draft{}, local: map[string]localDraft{}, calls: map[string]json.RawMessage{},
		watched: map[string]map[string]uint64{},
		jsRead:  map[string]map[string]struct{}{}, jsListed: map[string]bool{}, jsFull: map[string]bool{},
	}
}

// noteJSRead records that JS read slice[key] (a tracked slice).
func (m *mirror) noteJSRead(slice, key string) {
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.jsFull[slice] {
		return
	}
	r := m.jsRead[slice]
	if r == nil {
		r = map[string]struct{}{}
		m.jsRead[slice] = r
	}
	if len(r) >= jsReadMax {
		m.jsFull[slice] = true
		delete(m.jsRead, slice)
		return
	}
	r[key] = struct{}{}
}

// jsHoldsLocked filters keys of a tracked slice to those JS holds,
// forgetting them (their invalidation drops them in JS); mu held.
func (m *mirror) jsHoldsLocked(slice string, keys []string) []string {
	if m.jsListed[slice] || m.jsFull[slice] {
		return keys
	}
	r := m.jsRead[slice]
	var out []string
	for _, k := range keys {
		if _, ok := r[k]; ok {
			delete(r, k)
			out = append(out, k)
		}
	}
	return out
}

// start follows core's events and raw realtime frames while the runtime is up.
func (m *mirror) start() {
	m.mu.Lock()
	if m.started {
		m.mu.Unlock()
		return
	}
	m.started = true
	ctx, cancel := context.WithCancel(context.Background())
	m.cancel = cancel
	m.mu.Unlock()

	evs, stop := m.t.app.Subscribe()
	// what core emitted before this is in the store, which JS reads afresh
	// (a new runtime holds nothing): handled
	m.t.ui.views.noteSeen(m.t.app.EmitSeq())
	raws, stopRaw := m.t.app.SubscribeRaw()
	// a message a fetch by id brings that the store held no copy of has no
	// change of its own to announce: marked as it lands, on the fetching
	// goroutine before its caller draws it, as the hub marks what a
	// window's load changed (MessagesChanged); the plugins' view of it
	// waited for the sweep
	unhook := m.t.app.SetFetchedHook(func(ch string, tss []string) { m.t.MessagesChanged(ch, tss) })
	go func() {
		defer stop()
		defer stopRaw()
		defer unhook()
		tick := time.NewTicker(time.Duration(sweepEvery.Load()))
		defer tick.Stop()
		for {
			select {
			case <-ctx.Done():
				return
			case ev, ok := <-evs:
				if !ok {
					return
				}
				if h := m.hold.Load(); h != nil {
					<-*h // tests: HoldMirror
				}
				resync := ev.Type == "resync" && ev.EmitSeq == 0
				var seen uint64
				if resync {
					// fell behind: what it missed is in the store, and the
					// resync invalidates everything. Read before it: an
					// event emitted while it runs is queued behind it, its
					// marks not made, and read after, its need passed
					seen = m.t.app.EmitSeq()
				}
				m.onEvent(ev)
				if f := m.handled.Load(); f != nil {
					(*f)(ev) // tests: AfterMirrorEvent
				}
				if resync {
					m.t.ui.views.noteSeen(seen)
				} else {
					m.t.ui.views.noteSeen(ev.EmitSeq)
				}
			case ev := <-raws:
				m.t.onRaw(ev)
				m.noteRaw(ev)
			case <-tick.C:
				m.sweep()
			}
		}
	}()
}

func (m *mirror) stop() {
	m.mu.Lock()
	if m.cancel != nil {
		m.cancel()
	}
	m.mu.Unlock()
}

// invalidate queues a change for JS; changes are sent together a moment
// later. Of a tracked slice (members, presence) only the ids JS holds go.
func (m *mirror) invalidate(slice string, keys ...string) {
	if !jsTracked[slice] {
		// queued for JS from now until send posts it: a view JS gives
		// meanwhile is from before it (MessageViewState's ready)
		m.t.ui.views.pendingAdd(slice, keys, 1)
	}
	if len(keys) == 0 {
		m.t.ui.views.stale(slice, "")
	}
	for _, k := range keys {
		m.t.ui.views.stale(slice, k)
	}
	m.mu.Lock()
	if jsTracked[slice] {
		if len(keys) == 0 {
			delete(m.jsRead, slice)
			delete(m.jsListed, slice)
			delete(m.jsFull, slice)
		} else if keys = m.jsHoldsLocked(slice, keys); len(keys) == 0 {
			m.mu.Unlock()
			return // nothing JS holds: nothing to tell it
		}
	}
	m.pending = append(m.pending, change{Slice: slice, Keys: keys})
	if w := m.watched[slice]; w != nil {
		if len(keys) == 0 {
			delete(m.watched, slice)
		} else {
			for _, k := range keys {
				delete(w, k)
			}
		}
	}
	if m.flush == nil {
		m.flush = time.AfterFunc(8*time.Millisecond, m.send)
	}
	m.mu.Unlock()
}

// invalidateMsg is invalidate("messages", ch) for a change to message ts
// alone (a new message, an update, a reaction, a delete): JS reads the
// channel's bucket again, and only ts's view stops being ready until JS's
// answer for it comes (MessageViewState). With the channel's marks, any
// change in a busy channel held every message's view.
func (m *mirror) invalidateMsg(ch, ts string) {
	if ch == "" || ts == "" {
		m.invalidate("messages", ch)
		return
	}
	m.t.ui.views.msgPending(ch, ts, 1)
	m.t.ui.views.msgStale(ch, ts)
	m.mu.Lock()
	m.pending = append(m.pending, change{Slice: "messages", Keys: []string{ch}, ts: ts})
	if w := m.watched["messages"]; w != nil {
		delete(w, ch)
	}
	if m.flush == nil {
		m.flush = time.AfterFunc(8*time.Millisecond, m.send)
	}
	m.mu.Unlock()
}

func (m *mirror) send() {
	m.mu.Lock()
	m.flush = nil
	batch := m.pending
	m.pending = nil
	m.mu.Unlock()
	if len(batch) == 0 {
		return
	}
	defer func() {
		for _, c := range batch { // posted (behind any later read), or no runtime to post to
			switch {
			case c.ts != "":
				m.t.ui.views.msgPending(c.Keys[0], c.ts, -1)
			case !jsTracked[c.Slice]:
				m.t.ui.views.pendingAdd(c.Slice, c.Keys, -1)
			}
		}
	}()
	out := mergeChanges(batch)
	m.t.mu.Lock()
	rt := m.t.rt
	m.t.mu.Unlock()
	if rt != nil {
		m.postInvalidate(rt, out)
		// A view the UI read between invalidate and this post holds JS's
		// old value: invalidate marked it stale before JS knew of the
		// change, and its fetch could run first. Mark it stale again now
		// that the change is queued ahead of any later read (the runtime
		// runs posts and calls in order), and redraw so the UI asks.
		marked := false
		// a whole slice's or bucket's change marks it whole again; one
		// message's marks that message (and its bucket's fetch)
		whole := map[string]bool{} // messages buckets changed as a whole in this batch
		for _, c := range batch {
			if c.Slice == "messages" && c.ts == "" {
				for _, k := range c.Keys {
					whole[k] = true
				}
			}
		}
		for _, c := range out {
			if len(c.Keys) == 0 {
				marked = m.t.ui.views.stale(c.Slice, "") || marked
			}
			for _, k := range c.Keys {
				if c.Slice == "messages" && !whole[k] {
					continue // its messages' own marks, below
				}
				marked = m.t.ui.views.stale(c.Slice, k) || marked
			}
		}
		for _, c := range batch {
			if c.ts != "" && !whole[c.Keys[0]] {
				marked = m.t.ui.views.msgStale(c.Keys[0], c.ts) || marked
			}
		}
		if f := m.t.opts.UI.Invalidate; marked && f != nil {
			f()
		}
	}
}

// mergeChanges merges a batch per slice: an all-keys change swallows key
// lists.
func mergeChanges(batch []change) []change {
	merged := map[string]*change{}
	var order []string
	for _, c := range batch {
		cur := merged[c.Slice]
		if cur == nil {
			cc := c
			cc.Keys = append([]string(nil), c.Keys...)
			merged[c.Slice] = &cc
			order = append(order, c.Slice)
			continue
		}
		if len(cur.Keys) == 0 {
			continue
		}
		if len(c.Keys) == 0 {
			cur.Keys = nil
			continue
		}
		cur.Keys = append(cur.Keys, c.Keys...)
	}
	out := make([]change, 0, len(order))
	for _, s := range order {
		out = append(out, *merged[s])
	}
	return out
}

// postInvalidate hands changes to JS. While one invalidate task waits in
// the runtime's queue, later batches join it: a runtime busy for a moment
// gets one task for what came meanwhile, not one per 8 ms batch (a
// profile-change flood queued hundreds, Go's renders waiting behind them,
// each re-running every store listener). Joining a task that is already
// queued keeps the order send relies on: it runs before any read posted
// after it. A view fetched in between runs after the joined change too,
// with its bytes but a ver below send's second mark: not ready until the
// refetch, which announces itself (viewCache.work). Merging msg-latency:
// a send's chain pulls its task ahead of the plain queue, and a later
// invalidation that joins it rides ahead with it; the view fetches go on
// the same in-order lane, so none queued before the task runs after it.
func (m *mirror) postInvalidate(rt *runtime, out []change) {
	m.qmu.Lock()
	if m.qrt != rt { // a new runtime: what the old one queued is gone with it
		m.qrt, m.queued, m.qposted = rt, nil, false
	}
	m.queued = append(m.queued, out...)
	if m.qposted {
		m.qmu.Unlock()
		return
	}
	m.qposted = true
	m.qmu.Unlock()
	rt.postWith("invalidate", func() []any {
		m.qmu.Lock()
		batch := mergeChanges(m.queued)
		if m.qrt == rt {
			m.queued, m.qposted = nil, false
		}
		m.qmu.Unlock()
		return []any{batch}
	})
}

// watching reports whether JS holds key of slice as it read it last with
// no change told since (watch): an invalidation forgets it until JS reads
// again, and that read takes whatever changed meanwhile.
func (m *mirror) watching(slice, key string) bool {
	m.mu.Lock()
	defer m.mu.Unlock()
	_, ok := m.watched[slice][key]
	return ok
}

// watch records what JS just read, so sweep can tell when it changes
// without an event (a history page core loaded on the UI's request).
func (m *mirror) watch(slice, key string, fp uint64) {
	m.mu.Lock()
	w := m.watched[slice]
	if w == nil {
		w = map[string]uint64{}
		m.watched[slice] = w
	}
	if len(w) > 4096 {
		for k := range w { // bounded: forget an arbitrary one
			delete(w, k)
			break
		}
	}
	w[key] = fp
	m.mu.Unlock()
}

func (m *mirror) sweep() {
	m.mu.Lock()
	now := time.Now()
	typingGone := false
	for k, r := range m.typing {
		if now.After(r.expires) {
			delete(m.typing, k)
			typingGone = true
		}
	}
	type item struct {
		slice, key string
		fp         uint64
	}
	var items []item
	for s, w := range m.watched {
		for k, fp := range w {
			items = append(items, item{s, k, fp})
		}
	}
	m.mu.Unlock()
	if typingGone {
		m.invalidate("typing")
	}
	for _, it := range items {
		var fp uint64
		switch it.slice {
		case "messages":
			fp = m.messagesFP(it.key)
		case "members":
			fp = m.memberFP(it.key)
		case "channels":
			fp = m.channelFP(it.key)
		default:
			continue
		}
		if fp != it.fp {
			m.invalidate(it.slice, it.key)
			if it.slice == "messages" {
				m.invalidate("channelHistory")
				m.invalidate("reactions")
			}
		}
	}
}

// ---- events ----

func (m *mirror) onEvent(ev core.Event) {
	m.noteConv(ev)
	switch ev.Type {
	case "message", "update", "delete", "sent":
		ts := ev.TS
		if ev.Msg != nil && ev.Msg.TS != "" {
			ts = ev.Msg.TS
		}
		m.invalidateMsg(ev.Channel, ts)
		if ev.Type == "sent" && ev.ReplacesTS != "" && ev.ReplacesTS != ts {
			m.invalidateMsg(ev.Channel, ev.ReplacesTS) // the pending copy it replaces goes
		}
		m.invalidate("channelHistory")
		m.invalidate("reactions")
		if ev.Count != nil {
			m.invalidate("unreadChannelCounts", ev.Channel)
			m.invalidate("channelCursors", ev.Channel)
		}
		if ev.Type == "message" && ev.Msg != nil && ev.Msg.User != "" {
			m.stopTyping(ev.Channel, ev.Msg.ThreadTS, ev.Msg.User)
		}
	case "evicted":
		// core trimmed the conversation to Slack's message cache budget
		// (core/evict.go): the JS side drops what it cached of it, as
		// Slack's store drops the evicted messages, and so do the UI's
		// patched views of them (the pass never trims what's on screen)
		m.t.ui.views.dropEvicted(ev.Channel)
		m.invalidate("messages", ev.Channel)
		m.invalidate("channelHistory")
		m.invalidate("reactions")
	case "members_evicted":
		// core trimmed the members cache (store/members.go): the JS side
		// drops them, as Slack's store does (bulkRemoveMembersById), and
		// so do the UI's patched views of them
		if len(ev.Users) > 0 {
			m.t.ui.views.drop("members", ev.Users...)
		}
		m.invalidate("members", ev.Users...)
		m.invalidate("presence", ev.Users...)
	case "names":
		// members (and names) core fetched for lookups that missed: the
		// ones the JS side read as absent are there now
		var ids []string
		for _, id := range ev.Users {
			if strings.HasPrefix(id, "U") || strings.HasPrefix(id, "W") {
				ids = append(ids, id)
			}
		}
		if len(ids) > 0 {
			m.invalidate("members", ids...)
		}
	case "typing":
		m.startTyping(ev.Channel, ev.ThreadTS, ev.UserID)
	case "marked":
		m.invalidate("unreadChannelCounts", ev.Channel)
		m.invalidate("channelCursors", ev.Channel)
	case "counts":
		m.invalidate("unreadChannelCounts")
		m.invalidate("channelCursors")
		m.invalidate("unreadCounts")
	case "presence":
		m.invalidate("presence", ev.Users...)
	case "user":
		id := ev.UserID
		if id == "" && ev.User != nil {
			id = ev.User.ID
		}
		if id != "" {
			m.invalidate("members", id)
		} else {
			m.invalidate("members")
		}
		if id == m.t.app.S.SelfID() {
			m.invalidate("dndV2")
		}
	case "member_count": // users/counts answered (core loadMemberCount): Slack's membershipCounts entry
		if ev.Channel != "" {
			m.invalidate("membershipCounts", ev.Channel)
		}
	case "conv", "conv_removed":
		id := ev.Channel
		if id == "" && ev.Conv != nil {
			id = ev.Conv.ID
		}
		for _, s := range []string{"channels", "readOnlyChannels", "threadOnlyChannels", "membershipCounts", "membership"} {
			if id != "" {
				m.invalidate(s, id)
			} else {
				m.invalidate(s)
			}
		}
	case "sidebar_changed", "channel_sections_upserted", "channel_sections_channels_upserted", "channel_sections_channels_removed", "channel_sections_deleted":
		m.invalidate("channelSections")
		m.invalidate("starredChannels")
	case "experiments":
		m.invalidate("experiments")
	case "prefs", "pref_change":
		m.invalidate("prefs")
		m.invalidate("userPrefs")
		m.invalidate("mutedChannels")
	case "huddle":
		m.noteHuddle(ev)
	case "draft":
		m.noteDraft(ev)
	case "dnd_updated", "dnd_updated_user":
		m.invalidate("dndV2")
	case "emoji_changed":
	case "resync", "connection", "booted", "hydrated":
		if ev.Type != "connection" { // booted: everything boot fetched is in; hydrated: a warm start's boot data (core warmboot.go)
			for _, s := range mapSlices {
				m.invalidate(s)
			}
			for _, s := range wholeSlices {
				m.invalidate(s)
			}
		}
	}
}

// noteConv tells MessageView's cached conversions what ev may have
// changed of what they read: the message it names (core announces every
// change to a message that way, a thread's broadcasts included); for a
// member, a conversation, a bot, an emoji, a canvas or a list (a profile
// change, a channel's name, icon, membership or tabs, names lookups
// resolved) the messages whose conversion read it (core.RefsChanged,
// viewCache.bumpChange: a file's uploader, a huddle's sentence, a message
// slug, a work object, a channel mention, a tab slug); for what every
// conversion reads (the highlight words, emoji_mode, experiment groups, a
// resync) every message. Nothing else: raw passthrough events (badge
// counts, a thread marked read, Slack's pref_change before the prefs
// themselves are fetched, a followed thread's notification) re-converted
// every message and redrew the window, a few times a minute.
func (m *mirror) noteConv(ev core.Event) {
	switch ev.Type {
	case "user", "conv", "conv_removed", "tabs", "names", "emoji_changed", "canvas", "list", "list_record_info",
		"prefs", "experiments":
		// a profile change comes many a second on a busy workspace's
		// socket: it converts again only the messages that named the
		// member, drawn or not (a file's uploader isn't the row's sender,
		// so the frame that drew the row never read them); a status
		// change, a topic, a bookmark, a pref no conversion reads: none
		m.t.ui.views.bumpChange(core.RefsChanged(ev))
		return
	case "resync", "booted", "hydrated":
		m.t.ui.views.bumpConv("", "")
		return
	case "message", "update", "delete", "sent", "saved", "thread_subscription", "evicted":
		// what changes the message a conversion reads, or core's state of
		// it a conversion draws (Later, a followed thread)
		ts := ev.TS
		if ts == "" && ev.Msg != nil {
			ts = ev.Msg.TS
		}
		if ev.Channel != "" && ts != "" {
			m.t.ui.views.bumpConv(ev.Channel, ts)
		}
		if ev.Type == "delete" {
			// and the message slugs linking to it (core.RefsChanged)
			m.t.ui.views.bumpChange(core.RefsChanged(ev))
		}
	}
	// the rest name a message with nothing in it a conversion reads: a
	// mark (each read of the open channel's newest message, then Slack's
	// channel_marked echo, had it converted again and the window redrawn),
	// typing, a draft, counts
}

func typingKey(ch, thread, user string) string { return ch + "|" + thread + "|" + user }

func (m *mirror) startTyping(ch, thread, user string) {
	if ch == "" || user == "" {
		return
	}
	r := typingRec{ChannelID: ch, MemberID: user, MemberIsSelf: user == m.t.app.S.SelfID(), Timestamp: time.Now().UnixMilli(), expires: time.Now().Add(6 * time.Second)}
	if thread != "" {
		th := thread
		r.ThreadTs = &th
	}
	m.mu.Lock()
	m.typing[typingKey(ch, thread, user)] = r
	m.mu.Unlock()
	m.invalidate("typing")
}

func (m *mirror) stopTyping(ch, thread, user string) {
	m.mu.Lock()
	_, had := m.typing[typingKey(ch, thread, user)]
	delete(m.typing, typingKey(ch, thread, user))
	m.mu.Unlock()
	if had {
		m.invalidate("typing")
	}
}

func (m *mirror) noteHuddle(ev core.Event) {
	h, ok := core.ParseHuddle(ev.Raw)
	if !ok || h.ID == "" {
		return
	}
	call := map[string]any{"id": h.ID, "name": h.Name, "channels": h.Channels, "createdBy": h.CreatedBy, "hasEnded": !h.Active && h.End > 0, "participants": h.Participants, "dateStart": h.Start, "dateEnd": h.End, "isDmCall": h.DM}
	b, _ := json.Marshal(call)
	m.mu.Lock()
	m.calls[h.ID] = b
	m.mu.Unlock()
	m.invalidate("allCalls")
}

// SetEditing tells plugins a message is open in an editor (Slack's
// messageEditWindow); patchMessageText leaves it unchanged meanwhile.
func (t *Taut) SetEditing(windowID, ch, ts string, open bool) {
	if !t.Started() {
		return
	}
	t.mirror.mu.Lock()
	if open {
		t.mirror.editing[windowID] = map[string]any{"channelId": ch, "ts": ts}
	} else {
		delete(t.mirror.editing, windowID)
	}
	t.mirror.mu.Unlock()
	t.mirror.invalidate("messageEditWindow")
}

// SetSearchResults shows a search response (Slack's search.modules /
// search.messages JSON) to plugins under searchResults[instance].
func (t *Taut) SetSearchResults(instance string, response json.RawMessage) {
	if !t.Started() {
		return
	}
	wrapped, _ := json.Marshal(map[string]any{instance: map[string]any{"response": json.RawMessage(response)}})
	t.mirror.mu.Lock()
	t.mirror.search = wrapped
	t.mirror.mu.Unlock()
	t.mirror.invalidate("searchResults")
}

// noteMembership records answers of getChannelMembershipInfoForUsers.
func (m *mirror) noteMembership(ch string, users map[string]bool) {
	m.mu.Lock()
	b := m.membership[ch]
	if b == nil {
		b = map[string]membershipRec{}
		m.membership[ch] = b
	}
	for u, in := range users {
		b[u] = membershipRec{IsKnown: true, IsMember: in}
	}
	m.mu.Unlock()
	m.invalidate("membership", ch)
}

// ---- reads (JS thread, through host functions) ----

func (m *mirror) slices() map[string][]string {
	return map[string][]string{"maps": mapSlices, "wholes": wholeSlices}
}

func (m *mirror) keys(slice string) []string {
	a := m.t.app
	switch slice {
	case "pendingFileUploads", "files":
		return pendingKeys(slice)
	case "channels", "readOnlyChannels", "threadOnlyChannels", "membershipCounts", "unreadChannelCounts", "channelCursors":
		ids := a.S.ConvIDs()
		if slice == "readOnlyChannels" || slice == "threadOnlyChannels" {
			out := ids[:0:0]
			for _, id := range ids {
				if m.get(slice, id) != nil {
					out = append(out, id)
				}
			}
			return out
		}
		return ids
	case "members", "presence":
		m.mu.Lock()
		m.jsListed[slice] = true
		m.mu.Unlock()
		return a.S.UserIDs()
	case "messages":
		return m.messageChannels()
	case "channelHistory":
		var out []string
		for _, ch := range a.S.MessageChannels() {
			out = append(out, ch)
			for root := range a.S.ThreadsIn(ch) {
				out = append(out, ch+"-"+root)
			}
		}
		return out
	case "reactions":
		// Slack's reactions slice: _rxn_key → the message's reactions
		var out []string
		for _, ch := range m.messageChannels() {
			for ts, msg := range m.bucket(ch) {
				if mm, ok := msg.(map[string]any); ok && mm["reactions"] != nil {
					out = append(out, "message-"+ts+"-"+ch)
				}
			}
		}
		sort.Strings(out)
		return out
	case "bots":
		return a.S.BotIDs()
	case "membership":
		m.mu.Lock()
		defer m.mu.Unlock()
		out := make([]string, 0, len(m.membership))
		for k := range m.membership {
			out = append(out, k)
		}
		sort.Strings(out)
		return out
	case "userGroups":
		a.S.RLock()
		defer a.S.RUnlock()
		out := make([]string, 0, len(a.S.Groups))
		for _, g := range a.S.Groups {
			out = append(out, g.ID)
		}
		return out
	case "teams":
		return m.teamIDs()
	}
	return nil
}

// get returns slice[key] (key "" for whole slices) as JSON-able data, or nil.
func (m *mirror) get(slice, key string) any {
	a := m.t.app
	switch slice {
	// ---- id-keyed ----
	case "pendingFileUploads", "files":
		return pendingEntry(slice, key)
	case "channels":
		c, ok := a.S.Conv(key)
		if !ok {
			if c, ok = a.S.ExtConv(key); !ok {
				return nil
			}
		}
		m.watch("channels", key, m.channelFP(key))
		return m.channel(c)
	case "members":
		// state.members[id]: a read of the store as it is, which (unlike
		// getMemberById) neither counts as a hit for the members cache nor
		// asks for a member it doesn't hold; an evicted member is gone
		// from it as from Slack's (memberFP's change invalidates it)
		m.noteJSRead("members", key) // read as absent too: a member arriving is news
		u, ok := a.S.RawUser(key)
		if !ok {
			return nil
		}
		m.watch("members", key, m.memberFP(key))
		return m.member(u)
	case "messages":
		b := m.bucket(key)
		m.watch("messages", key, m.messagesFP(key))
		if b == nil {
			return nil
		}
		return b
	case "reactions":
		rest, ok := strings.CutPrefix(key, "message-")
		i := strings.LastIndex(rest, "-")
		if !ok || i < 0 {
			return nil
		}
		msg, _ := m.bucket(rest[i+1:])[rest[:i]].(map[string]any)
		if msg == nil || msg["reactions"] == nil {
			return nil
		}
		return msg["reactions"]
	case "bots":
		bi, ok := a.S.Bot(key)
		if !ok {
			return nil
		}
		bot := map[string]any{"id": key, "name": bi.Name, "deleted": false}
		if bi.Icon != "" {
			bot["icons"] = map[string]any{"image_36": bi.Icon, "image_48": bi.Icon, "image_72": bi.Icon}
		}
		return pruneEmpty(bot, false)
	case "membershipCounts":
		n, ok := a.MemberCountCached(key)
		guests, apps := 0, 0
		if ok {
			guests, apps = a.GuestCountCached(key), a.AppCountCached(key)
		}
		if !ok {
			c, have := a.S.Conv(key)
			if !have || c.NumMembers == 0 {
				return nil
			}
			n = c.NumMembers
		}
		team := a.C.Creds.EnterpriseID
		if team == "" {
			team = a.C.Creds.Team
		}
		return map[string]any{"counts": map[string]any{"member_count": n, "current_team_member_count": n, "restricted_member_count": guests, "app_count": apps, "member_count_by_team": map[string]int{team: n}}}
	case "membership":
		out := map[string]membershipRec{}
		if c, ok := a.S.Conv(key); ok {
			for _, u := range c.Members {
				out[u] = membershipRec{IsKnown: true, IsMember: true}
			}
		}
		m.mu.Lock()
		for u, r := range m.membership[key] {
			out[u] = r
		}
		m.mu.Unlock()
		if len(out) == 0 {
			return nil
		}
		return out
	case "readOnlyChannels", "threadOnlyChannels":
		c, ok := a.S.Conv(key)
		if !ok {
			return nil
		}
		post, thread := m.restrictions(c)
		if slice == "readOnlyChannels" && post && thread {
			return map[string]bool{"isReadOnly": true}
		}
		if slice == "threadOnlyChannels" && post && !thread {
			return map[string]bool{"isThreadOnly": true}
		}
		return nil
	case "presence":
		m.noteJSRead("presence", key)
		a.S.RLock()
		p := a.S.Presence[key]
		a.S.RUnlock()
		if p == "" {
			return nil
		}
		return map[string]string{"id": key, "presence": p}
	case "unreadChannelCounts":
		e, ok := a.S.Count(key)
		if !ok {
			return nil
		}
		unread := 0
		if e.HasUnreads {
			unread = 1
		}
		return map[string]any{"unreadCnt": unread, "unreadHighlightCnt": e.MentionCount, "unreadVIPMentionCnt": 0}
	case "channelCursors":
		e, ok := a.S.Count(key)
		if !ok || e.LastRead == "" {
			return nil
		}
		return e.LastRead
	case "userGroups":
		a.S.RLock()
		defer a.S.RUnlock()
		for _, g := range a.S.Groups {
			if g.ID == key {
				return map[string]any{"id": g.ID, "handle": g.Handle, "name": g.Name, "description": g.Description, "user_count": g.UserCount,
					"date_delete": g.DateDelete, "is_usergroup": true, "is_subteam": true, "_name_lc": strings.ToLower(g.Name), "isNonExistent": false}
			}
		}
		return nil
	case "channelHistory":
		return m.history(key)
	case "teams":
		return m.team(key)

	// ---- whole ----
	case "bootData":
		return pruneEmpty(map[string]any{"user_id": a.S.SelfID(), "team_id": a.C.Creds.Team, "enterprise_id": a.C.Creds.EnterpriseID,
			"page_needs_enterprise": a.C.Creds.EnterpriseID != "", "version_ts": time.Now().Unix(), "version_uid": "clack"}, false)
	case "selfTeamIds":
		ent := a.C.Creds.EnterpriseID
		team := a.C.Creds.Team
		top := team
		if ent != "" {
			top = ent
		}
		ws := []string{}
		if team != "" {
			ws = append(ws, team)
		}
		return pruneEmpty(map[string]any{"teamId": top, "enterpriseId": ent, "internalWorkspaceIds": ws, "defaultWorkspaceId": team}, false)
	case "prefs":
		return map[string]any{"user": m.userPrefs(), "team": map[string]any{}, "channel": map[string]any{}, "enterprise": map[string]any{}}
	case "userPrefs":
		return m.userPrefs()
	case "teamPrefs":
		return map[string]any{}
	case "channelSections":
		return m.sections()
	case "starredChannels":
		ids := []string{}
		for _, id := range a.S.ConvIDs() {
			if a.Starred(id) {
				ids = append(ids, id)
			}
		}
		return ids
	case "mutedChannels":
		a.S.RLock()
		defer a.S.RUnlock()
		out := map[string]bool{}
		for id, mu := range a.S.Muted {
			if mu {
				out[id] = true
			}
		}
		return out
	case "typing":
		m.mu.Lock()
		defer m.mu.Unlock()
		out := make(map[string]typingRec, len(m.typing))
		for k, r := range m.typing {
			out[k] = r
		}
		return out
	case "dndV2":
		return m.dnd()
	case "drafts":
		return m.draftsSlice()
	case "slashCommand":
		return m.slashCommands()
	case "appActions":
		return m.appActionsSlice()
	case "experiments": // Slack's store: experiments.getByUser's records (forced groups are patches over it)
		if as := m.t.app.ExperimentAssignments(); as != nil {
			return as
		}
		return map[string]any{}
	case "huddleEvents":
		return m.t.huddleEventsSlice()
	case "allCalls":
		m.mu.Lock()
		defer m.mu.Unlock()
		return map[string]any{"byId": m.calls, "inviteResponseById": map[string]any{}}
	case "peers":
		return m.t.peersSlice()
	case "clientHuddles":
		return m.t.clientHuddlesSlice()
	case "messageEditWindow":
		m.mu.Lock()
		defer m.mu.Unlock()
		out := make(map[string]any, len(m.editing))
		for k, v := range m.editing {
			out[k] = v
		}
		return out
	case "searchResults":
		m.mu.Lock()
		defer m.mu.Unlock()
		if m.search == nil {
			return map[string]any{}
		}
		return m.search
	case "huddlesCanvas":
		return m.t.huddlesCanvasSlice()
	case "alert":
		return m.t.alertSlice()
	case "unreadCounts":
		counts := a.Counts()
		total, chans := 0, 0
		for _, c := range counts {
			if c.Unread && !c.Muted {
				chans++
				total += c.Mentions
			}
		}
		return map[string]any{"totalUnreads": total, "totalUnreadsInChannels": total, "totalUnreadChannelConversations": chans}
	case "huddles", "messagesMeta", "threadSub", "userGroupMembership":
		return map[string]any{}
	}
	return nil
}

// ---- shapes ----

var nonSpacing = unicode.Mn

func deburr(s string) string {
	d := norm.NFKD.String(s)
	return strings.Map(func(r rune) rune {
		if unicode.Is(nonSpacing, r) {
			return -1
		}
		return r
	}, d)
}

func toMap(v any) map[string]any {
	b, _ := json.Marshal(v)
	out := map[string]any{}
	_ = json.Unmarshal(b, &out)
	return out
}

func (m *mirror) channel(c *slack.Conversation) map[string]any {
	e := toMap(c)
	delete(e, "latest")
	name := c.Name
	e["name_normalized"] = deburr(name)
	e["_name_lc"] = deburr(strings.ToLower(name))
	team := c.ContextTeamID
	if team == "" {
		team = m.t.app.C.Creds.Team
	}
	ids := []string{}
	if team != "" {
		ids = append(ids, team)
	}
	for k, v := range map[string]any{
		"is_channel": c.IsChannel, "is_group": c.IsGroup, "is_im": c.IsIM, "is_mpim": c.IsMPIM, "is_private": c.IsPrivate,
		"is_archived": c.IsArchived, "is_general": c.IsGeneral, "is_member": c.IsMember || c.IsIM, "is_ext_shared": c.IsExtShared,
		"is_org_shared": c.IsOrgShared, "isNonExistent": false, "isUnknown": false, "fromAnotherTeam": false,
		"internalTeamIds": ids, "internal_team_ids": ids, "previous_names": []string{}, "unread_highlights": []any{}, "unreads": []any{},
		"connectedTeamIds": []string{}, "connectedLimitedTeamIds": []string{},
	} {
		if _, has := e[k]; !has || k == "is_member" {
			e[k] = v
		}
	}
	if team != "" {
		e["context_team_id"] = team
	}
	if _, ok := e["topic"]; !ok {
		e["topic"] = map[string]any{"value": "", "creator": "", "last_set": 0}
	}
	if _, ok := e["purpose"]; !ok {
		e["purpose"] = map[string]any{"value": "", "creator": "", "last_set": 0}
	}
	if len(c.Properties) > 0 {
		var props map[string]any
		if json.Unmarshal(c.Properties, &props) == nil {
			e["properties"] = props
		}
	}
	return e
}

// restrictions reports whether the viewer can't post at the top level /
// can't reply in threads, the source of readOnlyChannels and
// threadOnlyChannels. The rule is Slack's (core/posting.go:
// setPostingPermissions + computeChannelPostingPermissions).
func (m *mirror) restrictions(c *slack.Conversation) (postRestricted, threadRestricted bool) {
	p := m.t.app.PostingFor(c)
	return p.ReadOnly || p.ThreadOnly, p.ReadOnly
}

func (m *mirror) member(u *slack.User) map[string]any {
	e := toMap(u)
	p, _ := e["profile"].(map[string]any)
	if p == nil {
		p = map[string]any{}
		e["profile"] = p
	}
	pr := u.Profile
	p["real_name_normalized"] = deburr(pr.RealName)
	p["display_name_normalized"] = deburr(pr.DisplayName)
	// Slack always sends these as strings, "" when unset. Avatar URLs it
	// sends only when it has them, so absent ones stay absent (plugins
	// fall back with ??).
	for _, k := range []string{"real_name", "display_name", "status_text", "status_emoji", "first_name", "last_name", "title"} {
		if _, ok := p[k]; !ok {
			p[k] = ""
		}
	}
	if _, ok := p["status_expiration"]; !ok {
		p["status_expiration"] = 0
	}
	// the edge API's slim profiles carry avatar_hash but no image_* URLs;
	// Slack's client fills them in, and plugins read them (WhoReacted's
	// image_24 ?? image_48)
	if pr.AvatarHash != "" {
		team := u.TeamID
		if team == "" {
			team = pr.Team
		}
		if team == "" {
			m.t.app.S.RLock()
			team = m.t.app.S.TeamID
			m.t.app.S.RUnlock()
		}
		for _, size := range []int{24, 32, 48, 72, 192, 512} {
			k := "image_" + strconv.Itoa(size)
			if v, _ := p[k].(string); v == "" {
				p[k] = "https://ca.slack-edge.com/" + team + "-" + u.ID + "-" + pr.AvatarHash + "-" + strconv.Itoa(size)
			}
		}
	}
	realName := u.RealName
	if realName == "" {
		realName = pr.RealName
	}
	e["real_name"] = realName
	lcs := map[string]string{
		"_name_lc": u.Name, "_real_name_lc": realName, "_display_name_lc": pr.DisplayName,
		"_first_name_lc": pr.FirstName, "_last_name_lc": pr.LastName,
	}
	for k, v := range lcs {
		e[k] = strings.ToLower(v)
	}
	e["_real_name_normalized_lc"] = deburr(strings.ToLower(realName))
	e["_display_name_normalized_lc"] = deburr(strings.ToLower(pr.DisplayName))
	for k, v := range map[string]any{"deleted": u.Deleted, "is_admin": u.IsAdmin, "is_owner": u.IsOwner, "is_bot": u.IsBot,
		"is_app_user": u.IsAppUser, "is_restricted": u.IsRestricted, "is_ultra_restricted": u.IsUltraRestricted,
		"isUnknown": false, "isNonExistent": false, "isExternal": false, "is_stranger": u.IsStranger} {
		e[k] = v
	}
	// Slack's computeLocality: a Slack Connect member is isExternal
	if cu, ok := m.t.app.User(u.ID); ok && cu.External {
		e["isExternal"] = true
	}
	if e["team_id"] == nil && m.t.app.C.Creds.EnterpriseID != "" {
		e["team_id"] = m.t.app.C.Creds.EnterpriseID
	}
	if u.ID == m.t.app.S.SelfID() {
		e["is_self"] = true
		mp := "active"
		m.t.app.S.RLock()
		if m.t.app.S.Presence[u.ID] == "away" {
			mp = "away"
		}
		m.t.app.S.RUnlock()
		e["manual_presence"] = mp
	}
	return e
}

// messageJSON is a stored message as Slack's store keeps it.
func messageJSON(ch string, sm *slack.Message) map[string]any {
	e := toMap(sm)
	// attachments exactly as Slack sent them (the typed ones drop fields
	// and add zero values); blocks, metadata and bot_profile are already
	// Slack's own JSON
	delete(e, "attachments")
	if len(sm.RawAttachments) > 0 {
		var raw any
		if json.Unmarshal(sm.RawAttachments, &raw) == nil {
			e["attachments"] = raw
		}
	} else if len(sm.Attachments) > 0 {
		e["attachments"] = pruneEmpty(toMap(map[string]any{"a": sm.Attachments})["a"], true)
	}
	if files, ok := e["files"]; ok {
		e["files"] = pruneEmpty(files, true)
		// a file's initial_comment is Slack's own object (an old
		// file_share's comment), not pruned: empty fields in it stay
		if fs, ok := e["files"].([]any); ok && len(fs) == len(sm.Files) {
			for i := range sm.Files {
				f, ok := fs[i].(map[string]any)
				if raw := sm.Files[i].InitialComment; ok && len(raw) > 0 {
					var v any
					if json.Unmarshal(raw, &v) == nil {
						f["initial_comment"] = v
					}
				}
			}
		}
	}
	if bp, ok := e["bot_profile"]; ok {
		e["bot_profile"] = pruneEmpty(bp, false)
	}
	if sm.Root != nil {
		e["root"] = messageJSON(ch, sm.Root)
	}
	e["channel"] = ch
	if e["type"] == nil {
		e["type"] = "message"
	}
	if _, ok := e["reply_count"]; !ok {
		e["reply_count"] = 0
	}
	e["_rxn_key"] = "message-" + sm.TS + "-" + ch
	e["no_display"] = false
	if _, ok := e["text"]; !ok {
		e["text"] = ""
	}
	return e
}

// pruneEmpty drops map entries that are "" or null (and, with zeros, 0),
// recursively: Go zero values Slack would leave out, which would defeat
// plugins' `??` fallbacks. Array elements are kept.
func pruneEmpty(v any, zeros bool) any {
	switch x := v.(type) {
	case map[string]any:
		for k, val := range x {
			switch y := val.(type) {
			case nil:
				delete(x, k)
			case string:
				if y == "" {
					delete(x, k)
				}
			case float64:
				if zeros && y == 0 {
					delete(x, k)
				}
			default:
				x[k] = pruneEmpty(val, zeros)
			}
		}
		return x
	case []any:
		for i := range x {
			x[i] = pruneEmpty(x[i], zeros)
		}
		return x
	}
	return v
}

// messageChannels are messages' keys: the conversations with a timeline,
// a jump's window or a cached thread, and those with messages fetched by
// id.
func (m *mirror) messageChannels() []string {
	a := m.t.app
	out := a.S.MessageChannels()
	seen := make(map[string]bool, len(out))
	for _, ch := range out {
		seen[ch] = true
	}
	for _, ch := range a.S.FetchedChannels() {
		if !seen[ch] {
			out = append(out, ch)
		}
	}
	sort.Strings(out)
	return out
}

// bucket is messages[ch]: the cached timeline, a jump's windows, open
// threads' replies, and the messages fetched by id. Slack keeps one
// messages store: every channelHistory slice's messages are in it, and a
// message fetched by id (fetchObjectWithId: Activity's and Saved's rows,
// the Threads view) lands in it beside them, so a plugin patching the
// slice (Censorship, ShowRealUser) reaches all of them there; built from
// the timeline and threads alone, a jump's window and such messages never
// got a view (store.WindowsIn, store.FetchedIn). A message several hold
// is the timeline's, a window's, a thread's, then the fetched copy.
func (m *mirror) bucket(ch string) map[string]any {
	a := m.t.app
	tl := a.S.Timeline(ch)
	wins := a.S.WindowsIn(ch)
	threads := a.S.ThreadsIn(ch)
	fetched := a.S.FetchedIn(ch)
	if tl == nil && len(wins) == 0 && len(threads) == 0 && len(fetched) == 0 {
		return nil
	}
	out := map[string]any{}
	if tl != nil {
		for i := range tl.Msgs {
			out[tl.Msgs[i].TS] = messageJSON(ch, &tl.Msgs[i])
		}
	}
	for _, w := range wins {
		for i := range w.Msgs {
			if _, ok := out[w.Msgs[i].TS]; !ok {
				out[w.Msgs[i].TS] = messageJSON(ch, &w.Msgs[i])
			}
		}
	}
	for _, msgs := range threads {
		for i := range msgs {
			if _, ok := out[msgs[i].TS]; !ok {
				out[msgs[i].TS] = messageJSON(ch, &msgs[i])
			}
		}
	}
	for i := range fetched { // a window's or a thread's copy wins
		if _, ok := out[fetched[i].TS]; !ok {
			out[fetched[i].TS] = messageJSON(ch, &fetched[i])
		}
	}
	return out
}

// history is channelHistory[ch] or channelHistory[ch-threadTs].
func (m *mirror) history(key string) any {
	a := m.t.app
	ch, thread, isThread := strings.Cut(key, "-")
	var ts []string
	reachedStart := false
	if isThread {
		// the thread's slices as its replies reads left them (store
		// thread_slices.go: Slack's channelHistory[threadKey])
		sls, rs, re, pre, ok := a.S.ThreadHistory(ch, thread)
		if !ok {
			return nil
		}
		out := make([]any, 0, len(sls))
		for _, x := range sls {
			tss := x.TSs
			if tss == nil {
				tss = []string{}
			}
			sl := map[string]any{"timestamps": tss, "start": nil, "end": nil}
			if x.Start != "" {
				sl["start"] = x.Start
			}
			if x.End != "" {
				sl["end"] = x.End
			}
			out = append(out, sl)
		}
		return map[string]any{"reachedStart": rs, "reachedEnd": re, "slices": out, "prevReachedEnd": pre}
	} else {
		tl := a.S.Timeline(ch)
		wins := a.S.WindowsIn(ch)
		if tl == nil && len(wins) == 0 {
			return nil
		}
		// a jump's windows first, oldest first (each older than the
		// timeline, which one joins once they meet), each a slice of its
		// own: its range, the stretch it holds whole (createSlice's
		// oldest and latest); the timeline's slice last
		var slices []any
		for _, w := range wins {
			var wts []string
			for i := range w.Msgs {
				if x := &w.Msgs[i]; x.ThreadTS == "" || x.ThreadTS == x.TS || x.Subtype == "thread_broadcast" {
					wts = append(wts, x.TS)
				}
			}
			slices = append(slices, historySlice(wts, w.Start, w.End))
		}
		if len(wins) > 0 {
			reachedStart = !wins[0].HasOlder
		}
		if tl == nil {
			// only windows: the newest messages aren't loaded
			return map[string]any{"reachedStart": reachedStart, "reachedEnd": false, "slices": slices, "prevReachedEnd": false}
		}
		for _, x := range tl.Msgs {
			if x.ThreadTS == "" || x.ThreadTS == x.TS || x.Subtype == "thread_broadcast" {
				ts = append(ts, x.TS)
			}
		}
		if len(wins) == 0 {
			reachedStart = !tl.HasMore
		}
		slices = append(slices, historySlice(ts, "", ""))
		return map[string]any{"reachedStart": reachedStart, "reachedEnd": true, "slices": slices, "prevReachedEnd": true}
	}
}

// historySlice is a channelHistory slice: its timestamps, and its start
// and end (the range given, else its first and last timestamp).
func historySlice(ts []string, start, end string) map[string]any {
	if ts == nil {
		ts = []string{}
	}
	slice := map[string]any{"timestamps": ts, "start": nil, "end": nil}
	if len(ts) > 0 {
		slice["start"], slice["end"] = ts[0], ts[len(ts)-1]
	}
	if start != "" {
		slice["start"] = start
	}
	if end != "" {
		slice["end"] = end
	}
	return slice
}

func (m *mirror) teamIDs() []string {
	a := m.t.app
	var ids []string
	if a.C.Creds.Team != "" {
		ids = append(ids, a.C.Creds.Team)
	}
	if e := a.C.Creds.EnterpriseID; e != "" && e != a.C.Creds.Team {
		ids = append(ids, e)
	}
	return ids
}

func (m *mirror) team(id string) any {
	a := m.t.app
	a.S.RLock()
	name, icon, domain, teamID := a.S.TeamName, a.S.TeamIcon, a.S.TeamDomain, a.S.TeamID
	a.S.RUnlock()
	if teamID == "" {
		teamID = a.C.Creds.Team
	}
	if id != teamID && id != a.C.Creds.EnterpriseID {
		return nil
	}
	t := map[string]any{"id": id, "name": name, "domain": domain, "url": a.C.Creds.TeamURL, "enterprise_id": a.C.Creds.EnterpriseID,
		"enterprise_name": name, "isUnknown": false, "isNonExistent": false}
	if icon != "" {
		t["icon"] = map[string]any{"image_68": icon, "image_132": icon}
	}
	return pruneEmpty(t, false)
}

func (m *mirror) userPrefs() map[string]any {
	p := m.t.app.Prefs()
	muted := strings.Join(p.Muted, ",")
	realNames := 0
	if p.DisplayRealNames {
		realNames = 1
	}
	return map[string]any{
		"emoji_mode": p.EmojiMode, "preferred_skin_tone": p.SkinTone, "time24": p.Time24,
		"display_real_names_override": realNames, "display_real_names": p.DisplayRealNames, "jumbomoji": p.Jumbomoji,
		"convert_emoticons": p.ConvertEmoticons, "sidebar_behavior": p.SidebarBehavior, "channel_sort": p.ChannelSort,
		"locale": p.Locale, "theme": p.Theme, "dark_mode": p.DarkMode, "sidebar_theme": p.SidebarTheme,
		"messages_theme": p.MessagesTheme, "enter_is_special_in_tbt": p.EnterIsSpecial, "show_typing": p.ShowTyping,
		"color_names_in_list": p.ColorNames, "muted_channels": muted, "global_desktop": p.GlobalDesktop,
		"emoji_use": "{}",
	}
}

func (m *mirror) sections() map[string]any {
	snap := m.t.app.SnapshotNow() // never wait for boot on the JS thread
	byID := map[string]any{}
	ids := map[string][]string{}
	byChannel := map[string][]string{}
	ordered := make([]any, 0, len(snap.Sections))
	for i, s := range snap.Sections {
		next := ""
		if i+1 < len(snap.Sections) {
			next = snap.Sections[i+1].ID
		}
		e := map[string]any{"id": s.ID, "type": s.Type, "name": s.Name, "emoji": s.Emoji, "nextChannelSectionId": next}
		byID[s.ID] = e
		ordered = append(ordered, e)
		ids[s.ID] = append([]string{}, s.Channels...)
		for _, c := range s.Channels {
			byChannel[c] = append(byChannel[c], s.ID)
		}
	}
	return map[string]any{"channelSectionById": byID, "orderedChannelSectionList": ordered, "channelIdsByChannelSectionId": ids,
		"channelSectionIdsByChannelId": byChannel, "autoSortedSectionIdByChannelId": map[string]any{}}
}

func (m *mirror) dnd() map[string]any {
	// Clack keeps no DND cache; plugins that need it ask profile.getSnooze,
	// which this answers as "not paused" until a dnd event says otherwise.
	snooze := map[string]any{"snoozeEnabled": false}
	return map[string]any{"currentMemberSnooze": snooze, "currentStatuses": map[string]bool{}, "schedules": map[string]any{}, "profileStatuses": map[string]any{}}
}

// slashCommands is Slack's slashCommand slice. The list needs a request,
// so the first read starts it and invalidates when it lands.
func (m *mirror) slashCommands() any {
	m.mu.Lock()
	cmds := m.commands
	loading := m.cmdLoading
	if cmds == nil && !loading {
		m.cmdLoading = true
	}
	m.mu.Unlock()
	if cmds == nil && !loading {
		go func() {
			ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
			defer cancel()
			list, err := m.t.app.Commands(ctx, "")
			out := map[string]any{}
			for _, c := range list {
				key := c.Name
				e := map[string]any{"name": c.Name, "desc": c.Desc, "usage": c.Usage, "type": c.Type, "autocomplete": true, "isServer": true}
				if c.AppID != "" {
					key = c.AppID + "-" + c.Name
					e["app"] = c.AppID
					e["app_name"] = c.AppName
				}
				out[key] = e
			}
			b, _ := json.Marshal(map[string]any{"commands": out})
			m.mu.Lock()
			if err == nil {
				m.commands = b
			}
			m.cmdLoading = false
			m.mu.Unlock()
			if err == nil {
				m.invalidate("slashCommand")
			}
		}()
	}
	if cmds == nil {
		return map[string]any{"commands": map[string]any{}}
	}
	return cmds
}

// ---- fingerprints (cheap change detection for watched entries) ----

func (m *mirror) messagesFP(ch string) uint64 {
	// in place and without allocating (fnv64a below is FNV-1a over the
	// same bytes the hash/fnv writes produced): the sweep fingerprints
	// every watched channel every 750 ms, and copying each timeline and
	// its threads out made megabytes of garbage a sweep in a long session.
	// Threads go in ts order: a map's order changes from one range to the
	// next, and an unsorted walk gave a channel with two cached threads a
	// new fingerprint every sweep, so the mirror re-sent its messages to
	// JS every 750 ms and every row a messages-reading plugin patched
	// (ShowRealUser, ShowSendingBot, Slick's Censorship) re-rendered forever
	h := fnv64a(fnvOffset64)
	m.t.app.S.VisitMessages(ch, h.str, func(sm *slack.Message) { fpMsg(&h, sm) })
	return uint64(h)
}

// fnv64a is an FNV-1a hash that takes strings and ints without allocating.
type fnv64a uint64

const (
	fnvOffset64 fnv64a = 14695981039346656037
	fnvPrime64  fnv64a = 1099511628211
)

func (h *fnv64a) str(s string) {
	for i := 0; i < len(s); i++ {
		*h ^= fnv64a(s[i])
		*h *= fnvPrime64
	}
}

// int hashes n's decimal digits (strconv.Itoa's bytes).
func (h *fnv64a) int(n int) {
	var b [20]byte
	for _, c := range strconv.AppendInt(b[:0], int64(n), 10) {
		*h ^= fnv64a(c)
		*h *= fnvPrime64
	}
}

func fpMsg(h *fnv64a, sm *slack.Message) {
	h.str(sm.TS)
	if sm.Edited != nil {
		h.str(sm.Edited.TS)
	}
	h.int(sm.ReplyCount)
	h.int(len(sm.Text))
	for _, r := range sm.Reactions {
		h.str(r.Name)
		h.int(r.Count)
	}
	h.int(len(sm.Files))
	h.str(sm.Subtype)
}

func (m *mirror) memberFP(id string) uint64 {
	u, ok := m.t.app.S.RawUser(id)
	if !ok {
		return 0
	}
	// the same bytes as before (Updated's digits, then the fields joined by
	// NULs), hashed without building them: up to 4096 members a sweep
	h := fnvOffset64
	h.int(int(u.Updated))
	for i, f := range [...]string{u.Name, u.RealName, u.Profile.DisplayName, u.Profile.StatusText, u.Profile.StatusTextCanonical, u.Profile.StatusEmoji, u.Profile.Image72} {
		if i > 0 {
			h.str("\x00")
		}
		h.str(f)
	}
	return uint64(h)
}

func (m *mirror) channelFP(id string) uint64 {
	c, ok := m.t.app.S.Conv(id)
	if !ok {
		if c, ok = m.t.app.S.ExtConv(id); !ok {
			return 0
		}
	}
	h := fnv.New64a()
	b, _ := json.Marshal(c)
	h.Write(b)
	return h.Sum64()
}

// SlackMessage is message ts in ch as Slack's store keeps it (what thunks
// such as showNotification carry as `message`), or nil when it isn't cached.
func (t *Taut) SlackMessage(ch, ts string) map[string]any {
	if t == nil || t.app == nil || t.app.S == nil {
		return nil
	}
	sm, ok := t.app.S.Find(ch, ts)
	if !ok {
		return nil
	}
	return messageJSON(ch, &sm)
}
