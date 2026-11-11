package core

import (
	"testing"

	"github.com/some-du6e/clack/internal/slack"
	"github.com/some-du6e/clack/internal/store"
)

// A bot replier's reply-bar picture (iR00 oV): reply_user_icons first;
// under agent_identity_overrides the latest reply's icons when it's the
// bot's; else the bot's own icon.
func TestReplyBarBotAvatar(t *testing.T) {
	a, _ := testApp(t)
	const bot = "B0EXPBOT01"
	a.S.PutBot(bot, store.BotInfo{Name: "Deploy Bot", Icon: "https://avatars.invalid/bot-72.png"})
	a.S.SetThread("C1", "1.000100", []slack.Message{
		{TS: "1.000100", User: "U1", Text: "a synthetic root", ThreadTS: "1.000100", ReplyCount: 1},
		{TS: "1.000200", BotID: bot, Text: "a synthetic agent reply", ThreadTS: "1.000100",
			Icons: &slack.Icons{Image48: "https://avatars.invalid/agent-48.png"}},
	})
	root := &Message{TS: "1.000100", Channel: "C1", ThreadTS: "1.000100", ReplyCount: 1, ReplyUsers: []string{bot}, LatestReply: "1.000200"}
	for group, want := range map[string]string{"on": "https://avatars.invalid/agent-48.png", "off": "https://avatars.invalid/bot-72.png"} {
		a.SetExperimentGroup("agent_identity_overrides", group)
		if got := a.ReplyBarBotAvatar(root, bot, 48); got != want {
			t.Errorf("%s: %q, want %q", group, got, want)
		}
	}
	root.ReplyUserIcons = []slack.ReplyUserIcon{{ID: bot, IconURL: "https://avatars.invalid/override.png"}}
	if got := a.ReplyBarBotAvatar(root, bot, 48); got != "https://avatars.invalid/override.png" {
		t.Errorf("reply_user_icons: %q", got)
	}
	for id, want := range map[string]bool{bot: true, "@B0EXPBOT01": true, "U0EXPUSER1": false, "B12": false, "Bxxxxxxxxx": false} {
		if LooksLikeBotID(id) != want {
			t.Errorf("LooksLikeBotID(%q) != %v", id, want)
		}
	}
}
