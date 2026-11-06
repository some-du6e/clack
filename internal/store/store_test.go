package store

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/some-du6e/yoloslackgoclient/internal/slack"
)

// Synthetic fixtures only.

func msg(ts string) slack.Message { return slack.Message{TS: ts, User: "U1", Text: "m" + ts} }

func tss(ms []slack.Message) string {
	var out []string
	for _, m := range ms {
		out = append(out, m.TS)
	}
	return strings.Join(out, ",")
}

func TestTSLessAndSort(t *testing.T) {
	if !TSLess("999999999.000001", "1000000000.000000") {
		t.Error("shorter ts should sort first")
	}
	if !TSLess("1700000000.000001", "1700000000.000002") || TSLess("1700000000.000002", "1700000000.000002") {
		t.Error("TSLess wrong for same-length ts")
	}
	m := []slack.Message{msg("3.0"), msg("1.0"), msg("2.0")}
	SortAsc(m)
	if got := tss(m); got != "1.0,2.0,3.0" {
		t.Errorf("SortAsc = %s", got)
	}
}

func TestTimeline(t *testing.T) {
	s := New("")
	if s.Timeline("C1") != nil {
		t.Fatal("unloaded timeline should be nil")
	}
	s.SetLatest("C1", []slack.Message{msg("5.0"), msg("4.0")}, true)
	tl := s.Timeline("C1")
	if tl == nil || !tl.Loaded || !tl.HasMore || tss(tl.Msgs) != "4.0,5.0" {
		t.Fatalf("SetLatest: %+v", tl)
	}
	// The returned copy must not alias store memory.
	tl.Msgs[0].Text = "mutated"
	if s.Timeline("C1").Msgs[0].Text == "mutated" {
		t.Error("Timeline returned an aliased slice")
	}
	s.PrependOlder("C1", []slack.Message{msg("3.0"), msg("4.0")}, false)
	tl = s.Timeline("C1")
	if tss(tl.Msgs) != "3.0,4.0,5.0" || tl.HasMore {
		t.Errorf("PrependOlder = %s hasMore=%v", tss(tl.Msgs), tl.HasMore)
	}
	s.Upsert("C1", msg("6.0"))
	s.Upsert("C1", slack.Message{TS: "4.0", Text: "edited"})
	tl = s.Timeline("C1")
	if tss(tl.Msgs) != "3.0,4.0,5.0,6.0" || tl.Msgs[1].Text != "edited" {
		t.Errorf("Upsert = %s %q", tss(tl.Msgs), tl.Msgs[1].Text)
	}
	s.Upsert("C9", msg("1.0")) // not loaded: ignored
	if s.Timeline("C9") != nil {
		t.Error("Upsert should not create unloaded timelines")
	}
	s.Remove("C1", "5.0")
	if got := tss(s.Timeline("C1").Msgs); got != "3.0,4.0,6.0" {
		t.Errorf("Remove = %s", got)
	}
}

func TestTimelineBound(t *testing.T) {
	s := New("")
	s.SetLatest("C1", []slack.Message{msg("9999999999.000000")}, true)
	var older []slack.Message
	for i := 0; i < 6000; i++ {
		older = append(older, msg("1000000000."+leftPad(i)))
	}
	s.PrependOlder("C1", older, true)
	tl := s.Timeline("C1")
	if len(tl.Msgs) != 5000 || tl.Msgs[len(tl.Msgs)-1].TS != "9999999999.000000" {
		t.Errorf("bound: len=%d last=%s", len(tl.Msgs), tl.Msgs[len(tl.Msgs)-1].TS)
	}
}

func leftPad(i int) string { return fmt.Sprintf("%06d", i) }

func TestThreadsAndMutate(t *testing.T) {
	s := New("")
	s.SetLatest("C1", []slack.Message{{TS: "1.0", ThreadTS: "1.0", ReplyCount: 1}}, false)
	if _, ok := s.Thread("C1", "1.0"); ok {
		t.Error("uncached thread reported as cached")
	}
	s.SetThread("C1", "1.0", []slack.Message{{TS: "1.1", ThreadTS: "1.0"}, {TS: "1.0", ThreadTS: "1.0"}})
	s.AddReply("C1", slack.Message{TS: "1.2", ThreadTS: "1.0"})
	s.AddReply("C1", slack.Message{TS: "1.1", ThreadTS: "1.0", Text: "edited"})
	s.AddReply("C1", slack.Message{TS: "2.1", ThreadTS: "2.0"}) // uncached thread: ignored
	th, ok := s.Thread("C1", "1.0")
	if !ok || tss(th) != "1.0,1.1,1.2" || th[1].Text != "edited" {
		t.Fatalf("thread = %s ok=%v", tss(th), ok)
	}
	if _, ok := s.Thread("C1", "2.0"); ok {
		t.Error("AddReply created an uncached thread")
	}
	// Mutate reaches both the timeline and the cached thread copy of the parent.
	m, ok := s.Mutate("C1", "1.0", func(m *slack.Message) { m.ReplyCount = 2 })
	if !ok || m.ReplyCount != 2 {
		t.Fatalf("Mutate = %+v %v", m, ok)
	}
	th, _ = s.Thread("C1", "1.0")
	if th[0].ReplyCount != 2 || s.Timeline("C1").Msgs[0].ReplyCount != 2 {
		t.Error("Mutate did not update every copy")
	}
	// A reply only lives in the thread cache.
	if f, ok := s.Find("C1", "1.2"); !ok || f.TS != "1.2" {
		t.Error("Find did not search threads")
	}
	s.Remove("C1", "1.2")
	if _, ok := s.Find("C1", "1.2"); ok {
		t.Error("Remove did not remove a reply")
	}
	if _, ok := s.Mutate("C2", "1.0", func(*slack.Message) {}); ok {
		t.Error("Mutate matched another channel")
	}
}

func TestUsers(t *testing.T) {
	s := New("")
	s.PutUsers([]slack.User{{ID: "U1", Name: "al", Profile: slack.Profile{DisplayName: "Alice"}}})
	s.Presence["U1"] = "active"
	if got := s.MissingUsers([]string{"U1", "U2", "U2", "", "B1", "W3", "C1"}); strings.Join(got, ",") != "U2,W3" {
		t.Errorf("MissingUsers = %v", got)
	}
	for _, c := range []struct {
		u    slack.User
		want string
	}{
		{slack.User{Name: "h", Profile: slack.Profile{DisplayName: " D ", RealName: "R"}}, "D"},
		{slack.User{Name: "h", Profile: slack.Profile{RealName: "R"}}, "R"},
		{slack.User{Name: "h", RealName: "Top"}, "Top"},
		{slack.User{Name: "h", Profile: slack.Profile{RealName: "​"}}, "h"},
	} {
		if got := DisplayName(&c.u); got != c.want {
			t.Errorf("DisplayName(%+v) = %q, want %q", c.u, got, c.want)
		}
	}
	if DisplayName(nil) != "" || s.UserName("U1") != "Alice" || s.UserName("U404") != "" {
		t.Error("UserName wrong")
	}
	s.PutConv(slack.Conversation{ID: "D1", IsIM: true, User: "U1"})
	s.PutConv(slack.Conversation{ID: "C1", Name: "general", NumMembers: 10})
	s.PutConv(slack.Conversation{ID: "C1", Name: "general"}) // keeps num_members
	if s.ChannelName("D1") != "Alice" || s.ChannelName("C1") != "general" || s.ChannelName("C404") != "C404" {
		t.Error("ChannelName wrong")
	}
	if c, _ := s.Conv("C1"); c.NumMembers != 10 {
		t.Error("PutConv dropped num_members")
	}
	s.SetUsergroups([]slack.Usergroup{{ID: "S1", Handle: "design"}, {ID: "S2", Handle: "gone", DateDelete: 1}})
	if s.UsergroupName("S1") != "design" || s.UsergroupName("S2") != "S2" {
		t.Error("usergroups wrong")
	}
}

func TestCounts(t *testing.T) {
	s := New("")
	s.Self = slack.User{ID: "USELF"}
	c := &slack.Counts{Channels: []slack.CountEntry{{ID: "C1", LastRead: "10.0", Latest: "10.0"}}, IMs: []slack.CountEntry{{ID: "D1", LastRead: "5.0", Latest: "5.0"}}}
	c.Threads.HasUnreads = true
	c.Threads.MentionCount = 2
	s.SetCounts(c)
	s.PutConv(slack.Conversation{ID: "D1", IsIM: true, User: "U1"})
	if !s.ThreadsUnread || s.ThreadMentions != 2 {
		t.Error("threads counts not stored")
	}
	e := s.NoteMessage("C1", &slack.Message{TS: "11.0", User: "U1"}, false)
	if !e.HasUnreads || e.MentionCount != 0 || e.Latest != "11.0" {
		t.Errorf("channel message: %+v", e)
	}
	e = s.NoteMessage("C1", &slack.Message{TS: "12.0", User: "U1"}, true)
	if e.MentionCount != 1 {
		t.Errorf("mention: %+v", e)
	}
	e = s.NoteMessage("D1", &slack.Message{TS: "6.0", User: "U1"}, false)
	if !e.HasUnreads || e.MentionCount != 1 {
		t.Errorf("DM should badge: %+v", e)
	}
	e = s.NoteMessage("D1", &slack.Message{TS: "7.0", User: "USELF"}, false)
	if e.HasUnreads || e.MentionCount != 0 || e.LastRead != "7.0" {
		t.Errorf("own message should read: %+v", e)
	}
	e = s.NoteMessage("C1", &slack.Message{TS: "9.0", User: "U1"}, true) // older than last_read 10.0? no: last_read is 10.0
	if e.MentionCount != 1 {
		t.Errorf("already-read message counted: %+v", e)
	}
	// Mark unread (earlier ts) keeps the channel unread; mark at latest clears it.
	e = s.MarkRead("C1", "10.5")
	if !e.HasUnreads || e.LastRead != "10.5" {
		t.Errorf("mark unread: %+v", e)
	}
	e = s.MarkRead("C1", "12.0")
	if e.HasUnreads || e.MentionCount != 0 {
		t.Errorf("mark read: %+v", e)
	}
	if got, ok := s.Count("C404"); ok || got.ID != "C404" {
		t.Error("Count of unknown channel")
	}
	e = s.MarkRead("C2", "1.0")
	if e.HasUnreads {
		t.Error("mark of unknown channel should be read")
	}
}

func TestPrefs(t *testing.T) {
	raw := map[string]json.RawMessage{
		"emoji_mode":                  json.RawMessage(`"android"`),
		"time24":                      json.RawMessage(`true`),
		"display_real_names_override": json.RawMessage(`1`),
		"jumbomoji":                   json.RawMessage(`false`),
		"sidebar_behavior":            json.RawMessage(`"hide_inactive_channels"`),
		"muted_channels":              json.RawMessage(`"C1,C2"`),
		"all_notifications_prefs":     json.RawMessage(`"{\"channels\":{\"C2\":{\"muted\":false},\"C3\":{\"muted\":true}}}"`),
		"emoji_use":                   json.RawMessage(`"{\"+1\":5,\"tada\":9,\"eyes\":5}"`),
	}
	p, muted := ParsePrefs(raw)
	if p.EmojiStyle != "google" || !p.Time24 || !p.DisplayRealNames || p.Jumbomoji || !p.HideInactiveConvs {
		t.Errorf("prefs = %+v", p)
	}
	if strings.Join(p.Muted, ",") != "C1,C3" || !muted["C1"] || muted["C2"] || !muted["C3"] {
		t.Errorf("muted = %v %v", p.Muted, muted)
	}
	if strings.Join(p.FrequentEmoji, ",") != "tada,+1,eyes" {
		t.Errorf("frequent = %v", p.FrequentEmoji)
	}
	d, _ := ParsePrefs(map[string]json.RawMessage{})
	if d.EmojiStyle != "apple" || !d.Jumbomoji || d.Muted == nil {
		t.Errorf("default prefs = %+v", d)
	}
	s := New("")
	s.SetPrefs(p, muted)
	if s.EmojiStyle != "google" {
		t.Error("SetPrefs did not set emoji style")
	}
}

// The disk cache must hold metadata only: never message contents.
func TestCacheRoundTripMetadataOnly(t *testing.T) {
	dir := t.TempDir()
	s := New(dir)
	s.PutUsers([]slack.User{{ID: "U1", Name: "al", Profile: slack.Profile{DisplayName: "Alice"}}})
	s.CustomEmoji.Set(map[string]string{"parrot": "https://emoji.example/p.gif"})
	s.SetUsergroups([]slack.Usergroup{{ID: "S1", Handle: "design"}})
	const secret = "SYNTHETIC-SECRET-MESSAGE-BODY"
	s.SetLatest("C1", []slack.Message{{TS: "1.0", User: "U1", Text: secret}}, false)
	s.SetThread("C1", "1.0", []slack.Message{{TS: "1.0", Text: secret}})
	if err := s.SaveCache(); err != nil {
		t.Fatal(err)
	}
	b, err := os.ReadFile(filepath.Join(dir, "meta.json"))
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(b), secret) {
		t.Fatal("message contents written to disk")
	}
	if fi, _ := os.Stat(filepath.Join(dir, "meta.json")); fi.Mode().Perm() != 0o600 {
		t.Errorf("cache mode = %v", fi.Mode().Perm())
	}
	entries, _ := os.ReadDir(dir)
	if len(entries) != 1 {
		t.Errorf("unexpected files in cache dir: %v", entries)
	}
	s2 := New(dir)
	s2.LoadCache()
	if s2.UserName("U1") != "Alice" || s2.CustomEmoji.Len() != 1 || s2.UsergroupName("S1") != "design" {
		t.Error("cache round trip lost metadata")
	}
	if s2.Timeline("C1") != nil {
		t.Error("timeline restored from disk")
	}
}
