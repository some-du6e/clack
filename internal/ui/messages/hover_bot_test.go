package messages

import (
	"testing"

	"github.com/some-du6e/yoloslackgoclient/internal/core"
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
		{"bot user", row{m: core.Message{User: "U2", BotID: "B2"}}, "U2", ""},
		{"bot only", row{m: core.Message{BotID: "B3"}}, "", "B3"},
		{"bot profile", row{m: core.Message{BotProfile: &core.BotProfile{ID: "B4"}}}, "", "B4"},
		{"custom response", row{m: core.Message{BotID: "B5"}, custom: true}, "", ""},
	} {
		if u, b := c.r.hoverUser(), c.r.hoverBot(); u != c.user || b != c.bot {
			t.Errorf("%s: user %q bot %q, want %q %q", c.name, u, b, c.user, c.bot)
		}
	}
}
