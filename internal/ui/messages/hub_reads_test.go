package messages

import (
	"encoding/json"
	"fmt"
	"testing"
	"time"

	"github.com/some-du6e/clack/internal/core"
	"github.com/some-du6e/clack/internal/slack"
	"github.com/some-du6e/clack/internal/ui/theme"
)

// hubReadsPage is a channel's newest page: ten messages, oldest first.
func hubReadsPage(ch string) []core.Message {
	var page []core.Message
	for i := 0; i < 10; i++ {
		page = append(page, core.Message{TS: fmt.Sprintf("1762500%03d.000100", 10+i), Channel: ch, Text: fmt.Sprintf("synthetic %d", i)})
	}
	return page
}

// hubReadsEnv is a hub with no core, holding the channel ch's model, its
// newest page loaded and older history beyond it (hasMore).
func hubReadsEnv(ch string) (*hub, *model) {
	hb := &hub{chans: map[string]*model{}, thread: map[string]*model{}}
	m := &model{ch: ch, typing: map[string]typingEntry{}}
	m.setAll(hubReadsPage(ch))
	m.hasMore, m.loaded = true, true
	hb.chans[ch] = m
	return hb, m
}

func hubTSs(m *model) []string {
	out := make([]string, len(m.msgs))
	for i := range m.msgs {
		out[i] = m.msgs[i].m.TS
	}
	return out
}

// A revision of an old message the channel list never loaded (an Activity
// or Saved row's copy read again, a Threads root, a refetch: core
// announces each as an "update") changes it where a list holds it and
// nowhere else: the open channel's list doesn't take it in above its
// oldest loaded message, which would skip the history between at the
// next load-older (its cursor is the oldest message) and draw a 3-day-old
// message right above today's. message_replied for such a root is the
// same. A message inside what the list loaded is taken as before.
func TestHubUpdateOutsideLoadedHistory(t *testing.T) {
	const ch = "C0HUBREAD1"
	hb, m := hubReadsEnv(ch)
	before := hubTSs(m)
	old := core.Message{TS: "1762300000.000100", Channel: ch, Text: "a synthetic message from three days ago, edited"}
	if hb.apply(core.Event{Type: "update", Channel: ch, TS: old.TS, Msg: &old}) {
		t.Error("the update of a message the list never loaded changed it")
	}
	root := core.Message{TS: "1762300100.000100", ThreadTS: "1762300100.000100", Channel: ch, Text: "an old synthetic root",
		ReplyCount: 3, LatestReply: "1762500999.000100"}
	hb.apply(core.Event{Type: "update", Channel: ch, TS: root.TS, Msg: &root}) // message_replied's
	if got := hubTSs(m); fmt.Sprint(got) != fmt.Sprint(before) {
		t.Fatalf("the list after updates of older messages: %v, want %v", got, before)
	}
	if m.msgs[0].m.TS != before[0] || !m.hasMore {
		t.Fatalf("load-older would read before %s (hasMore %v), want before %s", m.msgs[0].m.TS, m.hasMore, before[0])
	}
	// a message the list holds takes its revision
	held := m.msgs[3].m
	held.Text = "synthetic 3, edited"
	if !hb.apply(core.Event{Type: "update", Channel: ch, TS: held.TS, Msg: &held}) || m.msgs[3].m.Text != held.Text {
		t.Errorf("a held message's update: %q", m.msgs[3].m.Text)
	}
	// one inside the loaded range that it doesn't hold goes in
	in := core.Message{TS: "1762500012.500100", Channel: ch, Text: "a synthetic message the list missed"}
	if !hb.apply(core.Event{Type: "update", Channel: ch, TS: in.TS, Msg: &in}) || m.find(in.TS) < 0 {
		t.Error("the update of a message inside the loaded history stayed out")
	}
	// a list that loaded the whole history (no hasMore) holds every
	// message's place
	m.hasMore = false
	if !hb.apply(core.Event{Type: "update", Channel: ch, TS: old.TS, Msg: &old}) || m.find(old.TS) != 0 {
		t.Error("a list holding the whole history left out an older message's update")
	}
}

// After a whole eviction the model holds nothing and has loaded nothing:
// an update doesn't make a one-message list (drawn under the
// conversation's intro, hasMore false), and the next load's page decides
// what the list holds and whether older history exists.
func TestHubUpdateAfterWholeEviction(t *testing.T) {
	const ch = "C0HUBREAD2"
	hb, m := hubReadsEnv(ch)
	m.evict("")
	old := core.Message{TS: "1762300000.000100", Channel: ch, Text: "a synthetic message from three days ago, edited"}
	hb.apply(core.Event{Type: "update", Channel: ch, TS: old.TS, Msg: &old})
	if len(m.msgs) != 0 {
		t.Fatalf("an evicted model took an update in: %v", hubTSs(m))
	}
	// whatever an unloaded model holds, its first load's page and hasMore
	// win (ensureLoaded: takeLive, mergeNewest, keepLive)
	m.upsert(old, 1)
	m.loading = true
	live := m.takeLive()
	m.mergeNewest(hubReadsPage(ch), true)
	m.keepLive(live)
	m.loading, m.loaded = false, true
	if !m.hasMore || len(m.msgs) != 10 || m.find(old.TS) >= 0 {
		t.Fatalf("the first load after the eviction: %d msgs %v, hasMore %v; want the page, older history beyond it", len(m.msgs), hubTSs(m), m.hasMore)
	}
}

// An update that lands while the model's first load is out, of a message
// the model doesn't hold yet: the load's page takes it where it holds the
// message (its answer may predate the update), and nowhere else.
func TestHubUpdateDuringFirstLoad(t *testing.T) {
	const ch = "C0HUBREAD3"
	hb := &hub{chans: map[string]*model{}, thread: map[string]*model{}}
	m := &model{ch: ch, typing: map[string]typingEntry{}, loading: true}
	hb.chans[ch] = m
	page := hubReadsPage(ch)
	edited := page[4]
	edited.Text = "synthetic 4, edited"
	hb.apply(core.Event{Type: "update", Channel: ch, TS: edited.TS, Msg: &edited})
	old := core.Message{TS: "1762300000.000100", Channel: ch, Text: "a synthetic old message, edited"}
	hb.apply(core.Event{Type: "update", Channel: ch, TS: old.TS, Msg: &old})
	if len(m.msgs) != 0 {
		t.Fatalf("a model with nothing loaded took updates in: %v", hubTSs(m))
	}
	live := m.takeLive()
	m.mergeNewest(page, true)
	m.keepLive(live)
	m.loading, m.loaded = false, true
	if i := m.find(edited.TS); i < 0 || m.msgs[i].m.Text != edited.Text {
		t.Errorf("the page's copy of the message edited while it was out: %+v", m.msgs[max(i, 0)].m)
	}
	if m.find(old.TS) >= 0 || len(m.msgs) != 10 {
		t.Errorf("the load kept an update of a message outside its page: %v", hubTSs(m))
	}
}

// A send that failed long ago and is kept at the top of the list (older
// than the page from Slack: msg-latency keeps unconfirmed sends whatever
// the page's range) doesn't widen what the list loaded: the range starts
// at its oldest message from Slack, where load-older reads from, so an
// update of a message between the two stays out.
func TestHubUpdateAfterLocalCopyAtTop(t *testing.T) {
	const ch = "C0HUBREAD4"
	hb, m := hubReadsEnv(ch)
	m.upsert(core.Message{TS: "1762200000.000100", Channel: ch, Text: "a synthetic send that failed", Failed: true}, 1)
	before := hubTSs(m)
	between := core.Message{TS: "1762300000.000100", Channel: ch, Text: "an older synthetic message, edited"}
	hb.apply(core.Event{Type: "update", Channel: ch, TS: between.TS, Msg: &between})
	if got := hubTSs(m); fmt.Sprint(got) != fmt.Sprint(before) {
		t.Fatalf("the list after an update between a kept failed send and the page: %v, want %v", got, before)
	}
}

// An answer that starts the list over (msg-latency's takeNewest, the
// window started again at a page past a gap) takes back none of what the
// events changed during its load that it doesn't hold and that is older
// than its range: those messages are from before the gap. A message newer
// than the answer goes in as before.
func TestHubKeepLiveAfterStartOver(t *testing.T) {
	const ch = "C0HUBREAD5"
	_, m := hubReadsEnv(ch)
	m.loading = true
	pre := m.msgs[2].m
	pre.Text += ", reacted"
	m.upsert(pre, 1)
	m.noteLive(pre.TS, false)
	late := core.Message{TS: "1762900000.000100", Channel: ch, Text: "a synthetic message posted during the load"}
	m.upsert(late, 1)
	m.noteLive(late.TS, false)
	live := m.takeLive()
	var fresh []core.Message
	for i := 0; i < 5; i++ {
		fresh = append(fresh, core.Message{TS: fmt.Sprintf("1762800%03d.000100", i), Channel: ch, Text: fmt.Sprintf("after the gap %d", i)})
	}
	m.setAll(fresh) // takeNewest's start-over
	m.hasMore = true
	m.keepLive(live)
	m.loading = false
	if m.find(pre.TS) >= 0 {
		t.Errorf("a pre-gap message noted during the load came back above the new page: %v", hubTSs(m))
	}
	if m.find(late.TS) < 0 {
		t.Errorf("a message posted during the load stayed out: %v", hubTSs(m))
	}
}

// A message_changed of a message older than core's window (a vote on a
// poll 80 messages back, a root's reply count after a reply's deletion)
// stays out of the window, so the open list, loading again from it, keeps
// its oldest message and load-older's cursor.
func TestHubReloadKeepsRangeAfterOldChange(t *testing.T) {
	app, _ := newTestApp(t)
	const ch = "C0HUBOLD01"
	app.S.PutConv(slack.Conversation{ID: ch, Name: "old-changes", IsChannel: true, IsMember: true, NumMembers: 3})
	var page []slack.Message
	for i := 0; i < 10; i++ {
		page = append(page, slack.Message{Type: "message", TS: fmt.Sprintf("1762500%03d.000100", 10+i), User: "UALICE", Text: fmt.Sprintf("synthetic %d", i)})
	}
	app.S.SetLatest(ch, page, true)
	old := slack.Message{Type: "message", TS: "1762300000.000100", User: "UBOB", Text: "votes: 1"}
	app.S.PutFetched(ch, []slack.Message{old}, app.S.LiveSeq()) // Activity's copy
	hb := hubFor(coreHost{app: app, th: theme.New(false)})
	m := hb.channel(ch)
	load := func() {
		t.Helper()
		m.mu.Lock()
		m.fetched = time.Time{}
		m.mu.Unlock()
		hb.ensureLoaded(m)
		for deadline := time.Now().Add(5 * time.Second); ; time.Sleep(5 * time.Millisecond) {
			m.mu.Lock()
			done := m.loaded && !m.loading
			m.mu.Unlock()
			if done {
				return
			}
			if time.Now().After(deadline) {
				t.Fatal("the list never loaded")
			}
		}
	}
	load()
	m.mu.Lock()
	first := m.msgs[0].m.TS
	m.mu.Unlock()
	edited := old
	edited.Text = "votes: 2"
	raw, _ := json.Marshal(map[string]any{"type": "message", "subtype": "message_changed", "hidden": true, "channel": ch,
		"ts": "1762500100.000100", "message": edited, "previous_message": old})
	app.Inject(slack.Event{Type: "message", Subtype: "message_changed", Channel: json.RawMessage(`"` + ch + `"`), Raw: raw})
	if w := app.S.Timeline(ch); w == nil || w.Msgs[0].TS != page[0].TS {
		t.Errorf("core's window after the change starts at %v, want %s", w.Msgs[0].TS, page[0].TS)
	}
	load()
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.msgs[0].m.TS != first || !m.hasMore || m.find(old.TS) >= 0 {
		t.Fatalf("the list loaded again from %s (hasMore %v, holds the old message %v); load-older would read before it, want %s",
			m.msgs[0].m.TS, m.hasMore, m.find(old.TS) >= 0, first)
	}
}
