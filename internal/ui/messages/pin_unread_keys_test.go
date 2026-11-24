package messages

import (
	"strings"
	"testing"
	"time"

	"gioui.org/io/event"
	"gioui.org/io/key"
	"gioui.org/layout"

	"github.com/some-du6e/clack/internal/core"
	"github.com/some-du6e/clack/internal/slack"
	"github.com/some-du6e/clack/internal/ui/theme"
)

// P and U on a focused message are the pin and mark unread actions' keys,
// so they're bound only where Slack's rules give the message the action:
//   - P (Zn/q canPin → XRuW canPinMessage): none on a channel_join,
//     channel_leave, group_join or group_leave message, a pinned_item or
//     unpinned_item notice, nor in a conversation the viewer can't post in
//     (archived);
//   - U (QRkh → L4xk canMarkUnread): none on an automated message under
//     system_messages_redesign_m1 (a join, a topic change), nor in an
//     archived channel or one the viewer isn't a member of.
//
// Unbound, the letter goes to the input like any other.

const (
	tRuleKeys = "C0RULEKEY"
	tRuleArc  = "C0RULEARC"
	tRuleOut  = "C0RULEOUT"

	tsRulePlain = "1700000601.000100"
	tsRuleJoin  = "1700000602.000100"
	tsRuleTopic = "1700000603.000100"
	tsRuleArc   = "1700000604.000100"
	tsRuleOut   = "1700000605.000100"
)

func TestFocusedKeysFollowPinAndUnreadRules(t *testing.T) {
	app, f := newTestApp(t)
	app.S.PutConv(slack.Conversation{ID: tRuleKeys, Name: "rulekeys", IsChannel: true, IsMember: true, NumMembers: 3})
	app.S.PutConv(slack.Conversation{ID: tRuleArc, Name: "rulearc", IsChannel: true, IsMember: true, IsArchived: true, NumMembers: 3})
	app.S.PutConv(slack.Conversation{ID: tRuleOut, Name: "ruleout", IsChannel: true, NumMembers: 3})
	app.S.SetLatest(tRuleKeys, []slack.Message{
		{Type: "message", TS: tsRulePlain, User: "UALICE", Text: "a synthetic plan"},
		{Type: "message", TS: tsRuleJoin, User: "UBOB", Subtype: "channel_join", Text: "<@UBOB> has joined the channel"},
		{Type: "message", TS: tsRuleTopic, User: "UALICE", Subtype: "channel_topic", Text: "set the channel topic: synthetic"},
	}, false)
	app.S.SetLatest(tRuleArc, []slack.Message{{Type: "message", TS: tsRuleArc, User: "UALICE", Text: "an archived synthetic note"}}, false)
	app.S.SetLatest(tRuleOut, []slack.Message{{Type: "message", TS: tsRuleOut, User: "UALICE", Text: "a synthetic note outside"}}, false)
	for _, c := range []struct {
		name, ch, ts string
		key          key.Name
		text, method string
		bound        bool
	}{
		{"P pins a plain message", tRuleKeys, tsRulePlain, "P", "p", "pins.add", true},
		{"P on a join", tRuleKeys, tsRuleJoin, "P", "p", "pins.add", false},
		{"P in an archived channel", tRuleArc, tsRuleArc, "P", "p", "pins.add", false},
		{"U marks a plain message unread", tRuleKeys, tsRulePlain, "U", "u", "conversations.mark", true},
		{"U on a join", tRuleKeys, tsRuleJoin, "U", "u", "conversations.mark", false},
		{"U on a topic change", tRuleKeys, tsRuleTopic, "U", "u", "conversations.mark", false},
		{"U in an archived channel", tRuleArc, tsRuleArc, "U", "u", "conversations.mark", false},
		{"U where the viewer isn't a member", tRuleOut, tsRuleOut, "U", "u", "conversations.mark", false},
	} {
		t.Run(strings.ReplaceAll(c.name, " ", "_"), func(t *testing.T) {
			k := newRuleKeysEnv(t, app, f, c.ch, c.ts)
			k.selectMsg(t, c.ts)
			k.quiet()
			n := len(f.called(c.method))
			k.press(c.key, 0, c.text)
			k.quiet()
			ran := len(f.called(c.method)) > n
			switch {
			case c.bound && !ran:
				t.Errorf("%s: no %s", c.name, c.method)
			case c.bound && k.stolen != "":
				t.Errorf("%s: the action ran and the input got %q too", c.name, k.stolen)
			case !c.bound && ran:
				t.Errorf("%s: %s went out, where Slack binds no %s", c.name, c.method, c.key)
			case !c.bound && k.stolen != c.text:
				t.Errorf("%s: the input got %q, want the letter %q", c.name, k.stolen, c.text)
			}
		})
	}
}

// newRuleKeysEnv is a keysEnv over channel ch of app, with ts laid out.
func newRuleKeysEnv(t *testing.T, app *core.App, f *fakeSlack, ch, ts string) *keysEnv {
	t.Helper()
	e := &ctxEnv{t: t, h: &ctxHost{coreHost: coreHost{app: app, th: theme.New(false)}}, now: time.Now()}
	k := &keysEnv{ctxEnv: e, kh: &keysHost{ctxHost: e.h}, f: f}
	cv := NewChannelView(k.kh, ch)
	cv.Preview = true
	cv.SetSteal(func(gtx layout.Context, text string, keys []key.Event) event.Tag {
		k.stolen += text
		return nil
	})
	k.s, k.lay = cv.s, cv.Layout
	until(t, 5*time.Second, "the row "+ts, k.frame, func() bool { return k.s.rows[ts] != nil })
	return k
}
