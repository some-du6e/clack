package messages

import (
	"testing"

	"github.com/some-du6e/clack/internal/core"
)

// A sender with a user opens the user's card; a bot message without one
// opens the bot's (Slack passes bot_id as ProfileHoverTrigger's
// serviceId); a custom response opens nothing.
func TestRowHoverTarget(t *testing.T) {
	for _, c := range []struct {
		name      string
		r         row
		user, bot string
	}{
		{"member", row{m: core.Message{User: "U1"}}, "U1", ""},
		// MessageSender's botId is the message's bot with a user too (JA+h)
		{"bot user", row{m: core.Message{User: "U2", BotID: "B2"}}, "U2", "B2"},
		// Hack Club's #new-bots "who dat" shape: the member's profile names a
		// stale bot, the message carries the app's newer one
		{"bot user posting with a newer bot", row{m: core.Message{User: "U0NBTWOBOT", BotID: "B0NBTWONEW",
			BotProfile: &core.BotProfile{ID: "B0NBTWONEW", AppID: "A0NBTWOBOT", TeamID: "T0FAKE0001"}}}, "U0NBTWOBOT", "B0NBTWONEW"},
		{"bot only", row{m: core.Message{BotID: "B3"}}, "", "B3"},
		{"bot profile", row{m: core.Message{BotProfile: &core.BotProfile{ID: "B4"}}}, "", "B4"},
		{"custom response", row{m: core.Message{BotID: "B5"}, custom: true}, "", ""},
	} {
		if u, b := c.r.hoverUser(), c.r.hoverBot(); u != c.user || b != c.bot {
			t.Errorf("%s: user %q bot %q, want %q %q", c.name, u, b, c.user, c.bot)
		}
	}
}
