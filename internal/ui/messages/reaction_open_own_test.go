package messages

// Slack's ReactionBar (getDerivedStateFromProps) opens a collapsed bar
// when the viewer's own new reaction lands among the hidden pills: the
// reaction they just added shows, with "Show less", instead of hiding
// behind "N more". Clack kept the bar collapsed. Synthetic data.

import (
	"fmt"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/some-du6e/clack/internal/slack"
	"github.com/some-du6e/clack/internal/ui/theme"
)

func TestOwnHiddenReactionOpensTheBarLikeSlack(t *testing.T) {
	const ch, ts = "C0RXOPEN01", "1700500000.000100"
	app, _ := newTestApp(t)
	self := app.S.SelfID()
	if self == "" {
		t.Fatal("the test app has no viewer")
	}
	app.S.PutConv(slack.Conversation{ID: ch, Name: "reacts", IsChannel: true, IsMember: true, NumMembers: 4})
	var rx []slack.Reaction
	for i := 0; i < 40; i++ {
		rx = append(rx, slack.Reaction{Name: fmt.Sprintf("synth%02d", i), Count: 1, Users: []string{"UBOB"}})
	}
	app.S.SetLatest(ch, []slack.Message{{Type: "message", TS: ts, User: "UALICE", Text: "a synthetic message everyone reacted to", Reactions: rx}}, false)
	var mu sync.Mutex
	var drawn []string
	theme.DrawHook = func(s string) {
		mu.Lock()
		drawn = append(drawn, s)
		mu.Unlock()
	}
	t.Cleanup(func() { theme.DrawHook = nil })
	saw := func(what func(string) bool) bool {
		mu.Lock()
		defer mu.Unlock()
		for _, s := range drawn {
			if what(s) {
				return true
			}
		}
		return false
	}
	reset := func() {
		mu.Lock()
		drawn = drawn[:0]
		mu.Unlock()
	}
	more := func(s string) bool { return strings.HasSuffix(s, " more") }
	less := func(s string) bool { return s == "Show less" }
	v := NewChannelView(coreHost{app: app, th: theme.New(false)}, ch)
	f := frameFn(v)
	until(t, 5*time.Second, "the collapsed bar", f, func() bool { return saw(more) })
	// the viewer reacts with a new emoji: its pill is the last, hidden
	injectEvent(app, ch, "reaction_added", "", map[string]any{"user": self, "reaction": "synthmine", "item": map[string]any{"type": "message", "channel": ch, "ts": ts}})
	until(t, 5*time.Second, "the viewer's reaction on the message", f, func() bool {
		if r := v.s.rows[ts]; r != nil {
			for _, re := range r.m.Reactions {
				if re.Name == "synthmine" {
					return true
				}
			}
		}
		return false
	})
	reset()
	for i := 0; i < 5; i++ {
		f()
	}
	if saw(more) || !saw(less) {
		t.Errorf("after the viewer's own hidden reaction the bar draws %q / %q; Slack's opens it (all the pills, \"Show less\")",
			map[bool]string{true: "N more"}[saw(more)], map[bool]string{true: "Show less"}[saw(less)])
	}
}
