package core

import (
	"context"
	"sort"
	"strings"
	"testing"

	"github.com/some-du6e/yoloslackgoclient/internal/slack"
)

// Synthetic fixtures only.

func TestMissingNamesAreRecorded(t *testing.T) {
	a, _ := testApp(t)
	a.S.TakeMissing() // drop anything recorded by setup
	a.message("C1", &slack.Message{TS: "1.0", User: "U1", Text: "see <#C0A9ZZZZ> and <@U0NOPE> and <#C1>"})
	chans, users, _ := a.S.TakeMissing()
	if strings.Join(chans, ",") != "C0A9ZZZZ" || strings.Join(users, ",") != "U0NOPE" {
		t.Errorf("missing chans=%v users=%v", chans, users)
	}
	a.message("C1", &slack.Message{TS: "2.0", User: "U1", Text: "<#C0A9ZZZZ> again"})
	if c, _, _ := a.S.TakeMissing(); len(c) != 0 {
		t.Errorf("recorded twice: %v", c)
	}
}

func TestResolveDMsAndBots(t *testing.T) {
	a, f := testApp(t)
	f.reply["conversations.info"] = `{"ok":true,"channel":{"id":"D9","is_im":true,"user":"U2"}}`
	f.reply["bots.info"] = `{"ok":true,"bots":[{"id":"B7","name":"deploy-bot","icons":{"image_72":"https://img.example/b72.png"}}]}`
	got := a.resolve(context.Background(), []string{"D9"}, nil, []string{"B7"})
	sort.Strings(got)
	if strings.Join(got, ",") != "B7,D9" {
		t.Fatalf("resolved %v", got)
	}
	if f.last("bots.info").Get("bots") != "B7" {
		t.Errorf("bots.info params: %v", f.last("bots.info"))
	}
	if n := a.S.ChannelName("D9"); n != "Bob B" {
		t.Errorf("closed DM name = %q", n)
	}
	c, ok := a.Conv("D9")
	if !ok || c.Kind != "im" || c.User != "U2" || c.Name != "Bob B" {
		t.Errorf("Conv(D9) = %+v %v", c, ok)
	}
	if snap := (func() Snapshot { close(a.ready); return a.Snapshot() })(); len(snap.Convs) != 2 {
		t.Errorf("closed DM leaked into the sidebar: %d convs", len(snap.Convs))
	}
	v := a.message("C1", &slack.Message{TS: "3.0", BotID: "B7", Subtype: "bot_message", Text: "x"})
	if v.Avatar != "https://img.example/b72.png" || v.Name != "deploy-bot" {
		t.Errorf("bot without profile: avatar=%q name=%q", v.Avatar, v.Name)
	}
	a.message("C1", &slack.Message{TS: "4.0", BotID: "B8", Subtype: "bot_message"})
	if _, _, bots := a.S.TakeMissing(); strings.Join(bots, ",") != "B8" {
		t.Errorf("unknown bot not recorded: %v", bots)
	}
}

func TestHistoryResolvesClosedDM(t *testing.T) {
	a, f := testApp(t)
	f.reply["conversations.info"] = `{"ok":true,"channel":{"id":"D9","is_im":true,"user":"U2"}}`
	f.reply["conversations.history"] = `{"ok":true,"messages":[{"ts":"5.000001","user":"U2","text":"hey"}],"has_more":false}`
	p, err := a.History(context.Background(), "D9", "")
	if err != nil || p.Conv == nil || p.Conv.Name != "Bob B" || p.Conv.Kind != "im" || len(p.Messages) != 1 {
		t.Fatalf("closed DM history: %+v %v", p, err)
	}
}

func TestEmptyAppDMsHiddenAndSorted(t *testing.T) {
	a, _ := testApp(t)
	a.S.PutUsers([]slack.User{{ID: "USLACKBOT", Name: "slackbot"}, {ID: "UZ", Name: "zeta", IsBot: true}, {ID: "UA", Name: "Alpha", IsBot: true}})
	a.S.PutConv(slack.Conversation{ID: "D5", IsIM: true, User: "USLACKBOT"})
	a.S.PutConv(slack.Conversation{ID: "D6", IsIM: true, User: "UZ"})
	a.S.PutConv(slack.Conversation{ID: "D7", IsIM: true, User: "UA"})
	a.S.PutConv(slack.Conversation{ID: "D2", IsIM: true, User: "UB"})
	a.S.SetCounts(&slack.Counts{IMs: []slack.CountEntry{{ID: "D5", Latest: "0000000000.000000"}, {ID: "D6", Latest: "9.0"}, {ID: "D7", Latest: "8.0"}}})
	close(a.ready)
	for _, s := range a.Snapshot().Sections {
		if s.Type == "apps" {
			if got := strings.Join(s.Channels, ","); got != "D7,D2,D6" { // Alpha, bot, zeta; empty Slackbot DM hidden
				t.Errorf("apps = %s", got)
			}
		}
	}
}

func TestAddEmojiValidation(t *testing.T) {
	a, _ := testApp(t)
	if err := a.AddEmoji(context.Background(), " :: ", []byte("x")); err == nil {
		t.Error("empty name accepted")
	}
	if err := a.AddEmoji(context.Background(), "parrot", nil); err == nil {
		t.Error("empty image accepted")
	}
	if emojiExt([]byte("GIF89a....")) != ".gif" || emojiExt([]byte{0x89, 'P', 'N', 'G', '\r', '\n', 0x1a, '\n'}) != ".png" {
		t.Error("emojiExt")
	}
}
