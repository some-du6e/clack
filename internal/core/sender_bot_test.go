package core

// SenderBot against Slack's Emf2 maybeGetBotAndMemberByIds (MessageSender's
// botId, the one the sender's hover card and click both ask for): the
// message's bot when the store holds it; else the member's own bot
// (getBotIdFromMember) when it's another one the store holds; else the
// message's bot_profile; else the last tried. A message posted with a bot
// that's gone (an app reinstalled under a new bot) stands for the
// member's live bot.

import (
	"testing"

	"github.com/some-du6e/clack/internal/slack"
	"github.com/some-du6e/clack/internal/store"
)

func TestSenderBotLikeSlack(t *testing.T) {
	a := New(slack.New(slack.Creds{Token: "xoxc-test", Team: "T0VIEWER01", UserID: "USELF"}), store.New(""))
	a.S.PutUsers([]slack.User{
		{ID: "UBOT00001", IsBot: true, Profile: slack.Profile{BotID: "B0LIVE0001"}},
		{ID: "UBOT00002", IsBot: true, Profile: slack.Profile{BotID: "B0STALE001"}},
		{ID: "UHUMAN001", Profile: slack.Profile{BotID: "B0LIVE0001"}}, // a person's bot_id is no member bot
	})
	a.S.PutBot("B0LIVE0001", store.BotInfo{Name: "live"})
	a.S.PutBot("B0NEWER001", store.BotInfo{Name: "newer"})
	a.S.PutNonexistentBots([]string{"B0GONE0001", "B0STALE001"})
	bp := func(id string) *BotProfile { return &BotProfile{ID: id, AppID: "A1"} }
	for _, c := range []struct {
		name, user, bot string
		bp              *BotProfile
		want            string
	}{
		{"the message's bot, held", "UBOT00002", "B0NEWER001", bp("B0NEWER001"), "B0NEWER001"},
		{"the message's bot gone: the member's live bot", "UBOT00001", "B0GONE0001", bp("B0GONE0001"), "B0LIVE0001"},
		{"the message's bot gone, the member's stale too: the bot_profile", "UBOT00002", "B0GONE0001", bp("B0GONE0001"), "B0GONE0001"},
		{"a person posting through an app: the message's bot", "UHUMAN001", "B0GONE0001", bp("B0GONE0001"), "B0GONE0001"},
		{"no bot on the message: the member's", "UBOT00001", "", nil, "B0LIVE0001"},
		{"a bot with no member", "", "B0NEWER001", nil, "B0NEWER001"},
		{"nothing", "UHUMAN001", "", nil, ""},
	} {
		if got := senderBotFor(a, c.user, c.bot, c.bp); got != c.want {
			t.Errorf("%s: %q, want %q (Emf2 maybeGetBotAndMemberByIds)", c.name, got, c.want)
		}
	}
}
