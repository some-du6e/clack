package messages

import (
	"encoding/json"
	"image"
	"strings"
	"testing"
	"time"

	"github.com/some-du6e/clack/internal/core"
	"github.com/some-du6e/clack/internal/slack"
	"github.com/some-du6e/clack/internal/store"
	"github.com/some-du6e/clack/internal/ui/slackc"
)

// Karim's "nothing showing that it was a fake user" (2026-11-24): the
// at-channel app (bot B08G06U6SJG, in Slick's ShowRealUser relay list)
// posts a person's announcement as a bot_message with no user, the
// person's name as username and their picture as icons, and names them in
// metadata.event_payload.source_user_id. With ShowRealUser and
// ShowSendingBot on, the row is the person's (their name, their card) and
// ShowSendingBot badges their avatar with the bot that really sent it:
// it reads the bot's name and icons from state.bots, so the badge needs
// the bot bots.info answers for after the first draw (core's resolveLoop,
// then "names"). Synthetic text, person ids and URLs; the bot and app
// ids are at-channel's, which the relay list names.

const (
	relayAtCh, relayAtTS = "C0ATCHAN01", "1700000701.000100"
	atBot, atApp         = "B08G06U6SJG", "A08FPGU02A2"
)

// atChannelPost is at-channel's post for alice as history returns it.
func atChannelPost() slack.Message {
	meta, _ := json.Marshal(map[string]any{"event_type": "at_channel_ping", "event_payload": map[string]any{"source_user_id": "UALICE"}})
	return slack.Message{Type: "message", TS: relayAtTS, Subtype: "bot_message", BotID: atBot, AppID: atApp, Username: "alice",
		Icons: &slack.Icons{Image48: "https://avatars.invalid/alice-48.png"}, Metadata: meta, Text: "a synthetic announcement"}
}

// checkAtChannelBadge draws the post under ShowRealUser and ShowSendingBot
// and waits for the post credited to alice with at-channel's badge.
func checkAtChannelBadge(t *testing.T, e *slickEnv) {
	t.Helper()
	e.install(t, "ShowRealUser", nil)
	e.install(t, "ShowSendingBot", nil)
	v := NewChannelView(e.h, relayAtCh)
	v.Preview = true
	f := frameFn(v)
	t.Cleanup(func() {
		if !t.Failed() {
			return
		}
		if r := v.s.rows[relayAtTS]; r != nil {
			t.Logf("row: user %q, name %q, badge %q", r.m.User, r.nameShown, r.badge)
		}
		b, ok := e.app.S.Bot(atBot)
		t.Logf("store's bot: %+v (%v)", b, ok)
		e.ui.mu.Lock()
		defer e.ui.mu.Unlock()
		for id, tr := range e.ui.trees {
			if s := string(tr); strings.Contains(s, "ConnectedBaseAvatar") {
				i := strings.Index(s, "ConnectedBaseAvatar")
				t.Logf("root %d: %.1200s", id, s[max(0, i-300):])
			}
		}
	})
	e.waitFor(t, "the post credited to alice", f, func() bool {
		r := v.s.rows[relayAtTS]
		return r != nil && r.m.User == "UALICE"
	})
	e.waitFor(t, "at-channel's badge on alice's avatar", f, func() bool {
		return e.ui.anyTree("slick-sending-bot__badge") && e.ui.anyTree(atBot)
	})
}

// The bot already known (bots.info answered before the draw).
func TestSlickViewRelayedPostBadgeKnownBot(t *testing.T) {
	e := newSlickEnv(t, func(s *store.Store) {
		s.PutConv(slack.Conversation{ID: relayAtCh, Name: "announcements", IsChannel: true, IsMember: true, NumMembers: 3})
		s.PutBot(atBot, store.BotInfo{Name: "at-channel", Icon: "https://avatars.invalid/at-channel-48.png", AppID: atApp})
		s.SetLatest(relayAtCh, []slack.Message{atChannelPost()}, false)
	})
	checkAtChannelBadge(t, e)
}

// The bot unknown when the post draws: core asks bots.info for it
// (NoteMissingBotApp, resolveLoop) and the plugins see it once it comes
// ("names" refreshes state.bots).
func TestSlickViewRelayedPostBadgeLateBot(t *testing.T) {
	e := newSlickEnv(t, func(s *store.Store) {
		s.PutConv(slack.Conversation{ID: relayAtCh, Name: "announcements", IsChannel: true, IsMember: true, NumMembers: 3})
		s.SetLatest(relayAtCh, []slack.Message{atChannelPost()}, false)
	})
	e.api.mu.Lock()
	e.api.answer["bots.info"] = `{"ok":true,"bots":[{"id":"` + atBot + `","app_id":"` + atApp + `","name":"at-channel","deleted":false,` +
		`"icons":{"image_36":"https://avatars.invalid/at-channel-36.png","image_48":"https://avatars.invalid/at-channel-48.png","image_72":"https://avatars.invalid/at-channel-72.png"}}]}`
	e.api.mu.Unlock()
	e.app.ServeLookups()
	checkAtChannelBadge(t, e)
	if calls := e.api.called("bots.info"); len(calls) == 0 || !strings.Contains(calls[0].Get("bots"), atBot) {
		t.Errorf("bots.info calls %v, want one for %s", calls, atBot)
	}
}

// relaySenderHost records BaseMessageSender's clicks (SenderOpener).
type relaySenderHost struct {
	*ctxHost
	opened []string
}

func (h *relaySenderHost) OpenSender(_ image.Point, user, bot string) {
	h.opened = append(h.opened, user+"|"+bot)
}

// Without plugins the post is Slack's user-less bot message: the
// username override's name with the APP label (memberIsHuman is false
// for a username), and the name opens the bot's card, never the member
// whose name it borrows (JA+h: no userId, botId the bot).
func TestUsernameOverrideBotLikeSlack(t *testing.T) {
	e := newCtxEnvSetup(t, relayAtCh, []slack.Message{atChannelPost()}, func(app *core.App) {
		app.S.PutBot(atBot, store.BotInfo{Name: "at-channel", Icon: "https://avatars.invalid/at-channel-48.png", AppID: atApp})
	})
	sh := &relaySenderHost{ctxHost: e.h}
	e.v = NewChannelView(sh, relayAtCh)
	e.v.Preview = true
	until(t, 5*time.Second, "the post", e.frame, func() bool { return e.row(relayAtTS) != nil })
	r := e.row(relayAtTS)
	if r.badge != core.BadgeApp || r.nameShown != "alice" || r.m.User != "" {
		t.Fatalf("sender: name %q, badge %q, user %q; want alice with APP and no member", r.nameShown, r.badge, r.m.User)
	}
	r.openSender(&e.v.s.rc)
	if len(sh.opened) != 1 || sh.opened[0] != "|"+atBot {
		t.Errorf("the name opened %q, want the bot %s's card", sh.opened, atBot)
	}
}

// senderPropsPlugin draws what BaseMessageSender is handed: its bot, the
// message's username, the name and memberIsHuman.
const senderPropsPlugin = `
class SenderProps extends TautPlugin {
  static id = 'SenderProps'
  static pluginName = 'Sender props (test)'
  static description = 'test: shows the props BaseMessageSender gets'
  static authors = ['clack']
  static category = 'messages'
  static defaultConfig = { enabled: true }
  start() {
    this.api.patchComponent('BaseMessageSender', (Original) => (props) => React.createElement(React.Fragment, null,
      React.createElement(Original, props),
      React.createElement('span', { className: 'sender-probe ' + ['bot', props.botId, 'user', props.msg?.username, 'name', props.name, 'human', String(props.memberIsHuman)].join('-') })))
  }
}
return SenderProps`

// A plugin patching BaseMessageSender gets the bot fields Slack's
// connected MessageSender hands it for the post: botId, the message with
// its username, the name and memberIsHuman false.
func TestPluginSenderPropsForBotOverride(t *testing.T) {
	app, _, tt, u, h := pluginEnv(t)
	app.S.PutConv(slack.Conversation{ID: relayAtCh, Name: "announcements", IsChannel: true, IsMember: true, NumMembers: 3})
	app.S.PutBot(atBot, store.BotInfo{Name: "at-channel", AppID: atApp})
	app.S.SetLatest(relayAtCh, []slack.Message{atChannelPost()}, false)
	install(t, tt, pluginJS(senderPropsPlugin))
	until(t, 5*time.Second, "BaseMessageSender patched", func() {}, func() bool { return slackc.Patched("BaseMessageSender") })
	v := NewChannelView(h, relayAtCh)
	v.Preview = true
	want := "bot-" + atBot + "-user-alice-name-alice-human-false"
	until(t, 10*time.Second, "the patch's probe with the bot's fields", frameFn(v), func() bool { return u.anyTree(want) })
}
