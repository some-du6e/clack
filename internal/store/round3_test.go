package store

import (
	"testing"

	"github.com/some-du6e/yoloslackgoclient/internal/slack"
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
