package messages

import (
	"fmt"
	"testing"

	"github.com/some-du6e/clack/internal/core"
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
	m.upsert(old)
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
