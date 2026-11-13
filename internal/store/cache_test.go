package store

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/some-du6e/clack/internal/slack"
)

func TestEmojiPackedRoundTripAndAge(t *testing.T) {
	dir := t.TempDir()
	s := New(dir)
	if s.EmojiAge() < 24*time.Hour {
		t.Error("unknown emoji age should read as stale")
	}
	s.SetEmojiList(map[string]string{"parrot": "https://emoji.example/p.gif", "pp": "alias:parrot", "bad\nname": "x"})
	if s.EmojiAge() > time.Minute {
		t.Error("fresh emoji list has an old age")
	}
	if err := s.SaveCache(); err != nil {
		t.Fatal(err)
	}
	s2 := New(dir)
	s2.LoadCache()
	m := s2.CustomEmoji.Snapshot()
	if len(m) != 2 || m["parrot"] != "https://emoji.example/p.gif" || m["pp"] != "alias:parrot" {
		t.Fatalf("round trip: %v", m)
	}
	if s2.EmojiAge() > time.Minute {
		t.Error("emoji age not restored")
	}
}

func TestOldEmojiCacheStillLoads(t *testing.T) {
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "meta.json"), []byte(`{"users":[],"emoji":{"parrot":"https://emoji.example/p.gif"}}`), 0o600); err != nil {
		t.Fatal(err)
	}
	s := New(dir)
	s.LoadCache()
	if s.CustomEmoji.Len() != 1 {
		t.Fatal("pre-packed cache lost its emoji")
	}
}

func TestSaveCacheSkipsWhenUnchanged(t *testing.T) {
	dir := t.TempDir()
	s := New(dir)
	s.PutUsers([]slack.User{{ID: "U1", Name: "al"}})
	if err := s.SaveCache(); err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(dir, "meta.json")
	old := time.Now().Add(-time.Hour)
	_ = os.Chtimes(path, old, old)
	stamp := func() time.Time { fi, _ := os.Stat(path); return fi.ModTime() }

	s2 := New(dir)
	s2.LoadCache()
	s2.SetUsergroups(nil) // empty refresh
	s2.SetPrefs(s2.Prefs, s2.Muted)
	if err := s2.SaveCache(); err != nil {
		t.Fatal(err)
	}
	if !stamp().Equal(old) {
		t.Error("unchanged cache was rewritten")
	}
	s2.CustomEmoji.Put("new", "https://emoji.example/n.png")
	if err := s2.SaveCache(); err != nil {
		t.Fatal(err)
	}
	if stamp().Equal(old) {
		t.Error("emoji change was not saved")
	}
}

// meta.json streams in and out one user at a time (metacache.go): every
// field comes back, the file is still one JSON object, and a file cut
// short loads nothing, as the whole-file unmarshal did.
func TestMetaCacheStreams(t *testing.T) {
	dir := t.TempDir()
	s := New(dir)
	var us []slack.User
	for i := 0; i < 500; i++ {
		us = append(us, slack.User{ID: fmt.Sprintf("U%04d", i), Name: fmt.Sprintf("user%d", i), Presence: "away",
			Profile: slack.Profile{DisplayName: "<b>&amp;</b>\n", Image48: "https://avatars.example/a_48.png"}})
	}
	s.PutUsers(us)
	s.SetEmojiList(map[string]string{"parrot": "https://emoji.example/p.gif", "pp": "alias:parrot"})
	s.SetUsergroups([]slack.Usergroup{{ID: "S1", Handle: "team"}})
	s.PutName("C0OTHER", "elsewhere")
	if err := s.SaveCache(); err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(dir, "meta.json")
	b, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	var whole diskCache
	if err := json.Unmarshal(b, &whole); err != nil {
		t.Fatalf("meta.json isn't one JSON object: %v", err)
	}
	if len(whole.Users) != 500 || whole.EmojiPacked == "" || len(whole.Usergroups) != 1 || whole.Names["C0OTHER"] != "elsewhere" || whole.Prefs == nil {
		t.Fatalf("meta.json lost fields: %d users, groups %v, names %v", len(whole.Users), whole.Usergroups, whole.Names)
	}
	s2 := New(dir)
	s2.LoadCache()
	if len(s2.Users) != 500 {
		t.Fatalf("loaded %d users, want 500", len(s2.Users))
	}
	if u := s2.Users["U0042"]; u == nil || u.Name != "user42" || u.Presence != "away" || u.Profile.DisplayName != "<b>&amp;</b>\n" || u.Profile.Image48 != "https://avatars.example/a_48.png" {
		t.Fatalf("U0042 came back as %+v", s2.Users["U0042"])
	}
	if m := s2.CustomEmoji.Snapshot(); m["pp"] != "alias:parrot" || m["parrot"] != "https://emoji.example/p.gif" {
		t.Fatalf("emoji came back as %v", m)
	}
	if s2.Usergroups["S1"] != "team" || s2.names["C0OTHER"] != "elsewhere" || !s2.HasCachedPrefs() {
		t.Fatalf("groups %v, names %v, prefs cached %v", s2.Usergroups, s2.names, s2.HasCachedPrefs())
	}
	if err := os.WriteFile(path, b[:len(b)/2], 0o600); err != nil {
		t.Fatal(err)
	}
	s3 := New(dir)
	s3.LoadCache()
	if len(s3.Users) != 0 || s3.CustomEmoji.Len() != 0 {
		t.Fatalf("a file cut short loaded %d users and %d emoji", len(s3.Users), s3.CustomEmoji.Len())
	}
}
