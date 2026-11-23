package core

// What SenderBot asks bots.info for (review of thread-header-hover-3):
// as getBotById, never Slack's own bots or ids that don't look like a
// bot's, with the message's app (getBotByMsg's app_id: the bot_profile's)
// for the message's bot and the member's api_app_id for the member's; only
// while the sender's trigger or click is in use (a row's draw asks
// nothing, as senderOf leaves senders with a user or a bot_profile alone);
// and only the bots whose answer can change the pick: the message's while
// the member's stands in for it.

import (
	"slices"
	"testing"

	"github.com/some-du6e/clack/internal/slack"
	"github.com/some-du6e/clack/internal/store"
)

func TestSenderBotAsksLikeSlack(t *testing.T) {
	a := New(slack.New(slack.Creds{Token: "xoxc-test", Team: "T0VIEWER01", UserID: "USELF"}), store.New(""))
	a.S.PutUsers([]slack.User{
		{ID: "UBOT00001", IsBot: true, Profile: slack.Profile{BotID: "B0LIVE0001", APIAppID: "A0MEMBER01"}},
		{ID: "UBOT00002", IsBot: true, Profile: slack.Profile{BotID: "B0UNKNOWN2", APIAppID: "A0MEMBER02"}},
	})
	a.S.PutBot("B0LIVE0001", store.BotInfo{Name: "live"})
	a.S.TakeMissingNames() // start clean
	asked := func() []string {
		_, bots := a.S.TakeMissingNames()
		slices.Sort(bots)
		return bots
	}
	// a row's draw: nothing asked
	senderBotAsk(a, &Message{User: "UBOT00001", BotID: "B0NEWER001", BotProfile: &BotProfile{ID: "B0NEWER001", AppID: "A0MSGAPP01"}}, false)
	if got := asked(); len(got) != 0 {
		t.Errorf("a row's draw asked bots.info for %v; a sender asks only while its trigger is in use", got)
	}
	// the trigger in use: the message's bot, which the member's live bot
	// stands in for, with the message's app
	senderBotAsk(a, &Message{User: "UBOT00001", BotID: "B0NEWER001", BotProfile: &BotProfile{ID: "B0NEWER001", AppID: "A0MSGAPP01"}}, true)
	if got := asked(); !slices.Equal(got, []string{"B0NEWER001"}) || a.S.MissingBotApp("B0NEWER001") != "A0MSGAPP01" {
		t.Errorf("the hovered sender asked for %v (app %q); want B0NEWER001 with A0MSGAPP01", got, a.S.MissingBotApp("B0NEWER001"))
	}
	// the member's bot unknown too: both, each with its app
	senderBotAsk(a, &Message{User: "UBOT00002", BotID: "B0NEWER002", BotProfile: &BotProfile{ID: "B0NEWER002", AppID: "A0MSGAPP02"}}, true)
	if got := asked(); !slices.Equal(got, []string{"B0NEWER002", "B0UNKNOWN2"}) || a.S.MissingBotApp("B0UNKNOWN2") != "A0MEMBER02" {
		t.Errorf("asked for %v (the member's with app %q); want B0NEWER002 and B0UNKNOWN2 with A0MEMBER02", got, a.S.MissingBotApp("B0UNKNOWN2"))
	}
	// Slack's own bots, and a bot with no member: nothing to ask
	for _, m := range []*Message{
		{User: "UBOT00001", BotID: "B01", BotProfile: &BotProfile{ID: "B01"}},
		{User: "UBOT00001", BotID: "BSLACKBOT"},
		{BotID: "B0NOMEMBR1", BotProfile: &BotProfile{ID: "B0NOMEMBR1", AppID: "A1"}},
	} {
		senderBotAsk(a, m, true)
	}
	if got := asked(); len(got) != 0 {
		t.Errorf("asked bots.info for %v; Slack's own bots are never asked for, and a bot with no member is its bot_profile's", got)
	}
}
