package messages

// A message sender's hover card and its click ask for the same bot
// (thread-header-hover-3): Slack's BaseMessageSender hands botId (Emf2's
// bot) to both ProfileHoverTrigger and its onClick's openProfile. A
// message posted with a bot that's gone (an app reinstalled under a new
// bot) opens the member's live bot both ways; the click opened App details
// for the member's profile bot by its own rule (OpenProfile), and the
// hover the gone one.

import (
	"image"
	"testing"

	"github.com/some-du6e/clack/internal/core"
	"github.com/some-du6e/clack/internal/slack"
	"github.com/some-du6e/clack/internal/store"
	"github.com/some-du6e/clack/internal/ui/theme"
)

type senderHost struct {
	coreHost
	sender  [][2]string // OpenSender user, bot
	profile []string    // OpenProfile user
}

func (h *senderHost) OpenSender(_ image.Point, user, bot string) {
	h.sender = append(h.sender, [2]string{user, bot})
}
func (h *senderHost) OpenProfile(_ image.Point, id string) { h.profile = append(h.profile, id) }

func TestSenderHoverAndClickAgree(t *testing.T) {
	app, _ := newTestApp(t)
	app.S.PutUsers([]slack.User{{ID: "UBOT00001", IsBot: true, Profile: slack.Profile{BotID: "B0LIVE0001"}}})
	app.S.PutBot("B0LIVE0001", store.BotInfo{Name: "live"})
	app.S.PutNonexistentBots([]string{"B0GONE0001"})
	h := &senderHost{coreHost: coreHost{app: app, th: theme.New(false)}}
	rc := &rowCtx{h: h, origin: func(p image.Point) image.Point { return p }}
	for _, c := range []struct {
		name      string
		m         core.Message
		user, bot string
	}{
		{"posted with a gone bot", core.Message{User: "UBOT00001", BotID: "B0GONE0001", BotProfile: &core.BotProfile{ID: "B0GONE0001"}}, "UBOT00001", "B0LIVE0001"},
		{"a bot with no member", core.Message{BotID: "B0LIVE0001"}, "", "B0LIVE0001"},
	} {
		h.sender, h.profile = nil, nil
		r := &row{m: c.m, rc: rc}
		if got := r.hoverBot(); got != c.bot {
			t.Errorf("%s: the hover card asks for %q, want %q (Emf2's bot)", c.name, got, c.bot)
		}
		clickSender(r, rc)
		if len(h.sender) != 1 || h.sender[0] != [2]string{c.user, c.bot} {
			t.Errorf("%s: the click opened sender %v, profile %v; want %s with %s, the hover card's bot", c.name, h.sender, h.profile, c.user, c.bot)
		}
	}
}
