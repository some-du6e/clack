package store

import (
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/some-du6e/yoloslackgoclient/internal/slack"
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
