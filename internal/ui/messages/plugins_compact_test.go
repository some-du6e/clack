package messages

// The plugins that wrap message rows, under Compact (messages_theme
// "dense"): the Slick ones run in the ui-slick workflow (TestSlickView*),
// the Taut ones in messages-taut (*Plugin*). The layout-independent
// checks are the Clean tests run again with the pref on; ShowSendingBot
// and LocalTime get Compact's own: every channel row's gutter is the
// compact time (Message's getCompactGutter), so the sending bot's badge
// lands there, and BaseBroadcastPreamble's children are the sender alone
// (the time is the gutter's).

import (
	"testing"
	"time"

	"github.com/some-du6e/clack/internal/slack"
	"github.com/some-du6e/clack/internal/store"
	"github.com/some-du6e/clack/internal/ui/slackc"
)

func TestSlickViewCompactBetterBlock(t *testing.T) {
	setDense(t, true)
	TestSlickViewBetterBlock(t)
}

func TestSlickViewCompactMessageLogger(t *testing.T) {
	setDense(t, true)
	TestSlickViewMessageLogger(t)
}

func TestSlickViewCompactMessageLoggerThreads(t *testing.T) {
	setDense(t, true)
	TestSlickViewMessageLoggerThreads(t)
}

func TestSlickViewCompactWhoReacted(t *testing.T) {
	setDense(t, true)
	TestSlickViewWhoReacted(t)
}

func TestSlickViewCompactNicknames(t *testing.T) {
	setDense(t, true)
	TestSlickViewNicknamesRenamesSender(t)
}

func TestSlickViewCompactShowSendingBot(t *testing.T) {
	setDense(t, true)
	const ch, ts, ts2 = "C0BOTPOST", "1700000301.000100", "1700000361.000100"
	e := newSlickEnv(t, func(s *store.Store) {
		s.PutConv(slack.Conversation{ID: ch, Name: "bot-posts", IsChannel: true, IsMember: true, NumMembers: 3})
		s.PutBot("B0SENDER1", store.BotInfo{Name: "Sender Bot", Icon: "https://avatars.invalid/sender-bot-48.png"})
		s.SetLatest(ch, []slack.Message{
			{Type: "message", TS: ts, User: "UALICE", BotID: "B0SENDER1", Text: "posted for alice by a bot"},
			{Type: "message", TS: ts2, User: "UALICE", BotID: "B0SENDER1", Text: "and a grouped follow-up"},
		}, false)
	})
	e.install(t, "ShowSendingBot", nil)
	patched(t, "MessageKitGutter", "ProgressiveDisclosureSuccessCoachmarkWrapper")
	v := NewChannelView(e.h, ch)
	v.Preview = true
	f := frameFn(v)
	e.waitFor(t, "the gutter badge (every Compact row's gutter is the time)", f, func() bool {
		return e.ui.anyTree("slick-sending-bot__badge--gutter")
	})
	e.waitFor(t, "both rows one line tall", f, func() bool {
		return itemHeight(v.s, ts) > 0 && itemHeight(v.s, ts2) > 0 && v.s.rows[ts] != nil && v.s.rows[ts].nameShown != ""
	})
}

func TestSlickViewCompactLocalTime(t *testing.T) {
	setDense(t, true)
	e := newSlickEnv(t, nil)
	e.install(t, "LocalTime", nil)
	patched(t, "BaseBroadcastPreamble")
	v := NewChannelView(e.h, tGen)
	v.Preview = true
	var r *row
	e.waitFor(t, "a channel row with its sender", frameFn(v), func() bool {
		for k, x := range v.s.rows {
			if x != nil && x.m.User != "" && x.rc != nil && x.nameShown != "" && itemHeight(v.s, k) > 0 && x.inlineBody() && startsGroup(v.s, k) {
				r = x
				return true
			}
		}
		return false
	})
	// LocalTime's time goes after the sender (its anchor without a
	// Timestamp child), and the text still runs on after both
	if l := r.rv.Lead(); l <= 0 || l > 600 {
		t.Errorf("the body's first line starts at %d after the patched preamble", l)
	}
	kids, _ := r.preambleProps()["children"].([]any)
	if len(kids) != 1 {
		t.Fatalf("Compact preamble children = %v, want the sender only", kids)
	}
	if s, ok := kids[0].(slackc.Slot); !ok || s.Name != "MessageSender" {
		t.Errorf("preamble children = %v", kids)
	}
}

func TestPluginCompactWhoReacted(t *testing.T) {
	setDense(t, true)
	TestPluginWhoReacted(t)
}

func TestPluginCompactUserPronouns(t *testing.T) {
	setDense(t, true)
	TestPluginUserPronouns(t)
}

func TestPluginCompactShowSendingBot(t *testing.T) {
	setDense(t, true)
	code := pluginFile(t, "ShowSendingBot")
	app, _, tt, u, h := pluginEnv(t)
	const ch = "C0BOTPOSTS"
	app.S.PutConv(slack.Conversation{ID: ch, Name: "bot-posts", IsChannel: true, IsMember: true, NumMembers: 3})
	app.S.PutBot("B0SENDER1", store.BotInfo{Name: "Sender Bot", Icon: "https://avatars.invalid/sender-bot-48.png"})
	app.S.SetLatest(ch, []slack.Message{
		{Type: "message", TS: "1700000301.000100", User: "UALICE", BotID: "B0SENDER1", Text: "posted for alice by a bot"},
	}, false)
	install(t, tt, code)
	until(t, 3*time.Second, "components patched", func() {}, func() bool {
		return slackc.Patched("MessageBackground") && slackc.Patched("MessageKitGutter")
	})
	v := NewChannelView(h, ch)
	v.Preview = true
	// one message, so it starts its group: under Clean it has an avatar,
	// under Compact the time gutter, which gets the badge
	until(t, 10*time.Second, "the gutter badge on a group's first row", frameFn(v), func() bool {
		return u.anyTree("taut-sending-bot__badge--gutter")
	})
}

// startsGroup: the item keyed key draws its sender.
func startsGroup(s *listState, key string) bool {
	for _, it := range s.items {
		if it.kind == itMsg && it.key == key {
			return it.header
		}
	}
	return false
}
