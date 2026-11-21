package store

import (
	"testing"

	"github.com/some-du6e/clack/internal/slack"
)

func TestNamesBotsAndExtConvsCache(t *testing.T) {
	dir := t.TempDir()
	s := New(dir)
	s.PutName("C9", "elsewhere")
	s.PutBot("B1", BotInfo{Name: "bot", Icon: "https://img.example/b.png"})
	s.PutUsers([]slack.User{{ID: "U1", Name: "al", Profile: slack.Profile{DisplayName: "Alice"}}})
	s.PutExtConv(slack.Conversation{ID: "D9", IsIM: true, User: "U1"})
	if s.ChannelName("D9") != "Alice" {
		t.Error("ext IM name")
	}
	if _, ok := s.Conv("D9"); ok {
		t.Error("ext conv must not be a sidebar conv")
	}
	if err := s.SaveCache(); err != nil {
		t.Fatal(err)
	}
	s2 := New(dir)
	s2.LoadCache()
	if s2.ChannelName("C9") != "elsewhere" {
		t.Error("channel name not cached")
	}
	if b, ok := s2.Bot("B1"); !ok || b.Icon == "" {
		t.Error("bot not cached")
	}
	if s2.ChannelName("D9") != "D9" {
		t.Error("ext convs are memory-only")
	}
	chans, _, _ := s2.TakeMissing()
	if len(chans) != 1 || chans[0] != "D9" {
		t.Errorf("missing = %v", chans)
	}
}

// A bot this session stored, or found gone, before meta.json was read
// keeps that answer: LoadCache merges the disk's bots in under it (the
// session's is newer, and nothing asks for a bot twice in a session).
func TestLoadCacheKeepsSessionBots(t *testing.T) {
	dir := t.TempDir()
	s := New(dir)
	s.PutBot("B1", BotInfo{Name: "older"})
	s.PutBot("B2", BotInfo{Name: "cached"})
	s.PutBot("B3", BotInfo{Name: "gone since"})
	if err := s.SaveCache(); err != nil {
		t.Fatal(err)
	}
	s2 := New(dir)
	s2.PutBot("B1", BotInfo{Name: "newer"}) // answered while meta.json was decoding
	s2.PutBot("B4", BotInfo{Name: "fresh"}) // a bot meta.json doesn't hold
	s2.PutNonexistentBots([]string{"B3"})
	s2.LoadCache()
	for id, want := range map[string]string{"B1": "newer", "B2": "cached", "B4": "fresh"} {
		if b, ok := s2.Bot(id); !ok || b.Name != want {
			t.Errorf("%s = %+v %v, want %q", id, b, ok, want)
		}
	}
	if _, ok := s2.Bot("B3"); ok || !s2.BotNonexistent("B3") {
		t.Error("a bot this session found gone came back from the disk cache")
	}
}
