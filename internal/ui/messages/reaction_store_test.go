package messages

// ReactionBar keeps its isExpanded in the expandables store under the
// message's reactionKey (uFSC: getObjectById, setExpanded; KtEr J:
// "message-<ts>-<channel>"), the account's store: "N more" opens the bar
// for every view of the message, a view made afterwards starts open, and
// "Show less" closes it in the store too. Synthetic data.

import (
	"fmt"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/some-du6e/clack/internal/slack"
	"github.com/some-du6e/clack/internal/ui/richview"
	"github.com/some-du6e/clack/internal/ui/theme"
)

func TestReactionBarExpandedInTheStore(t *testing.T) {
	const ch, ts = "C0RXSTORE1", "1700600000.000100"
	app, _ := newTestApp(t)
	app.S.PutConv(slack.Conversation{ID: ch, Name: "reacts", IsChannel: true, IsMember: true, NumMembers: 4})
	var rx []slack.Reaction
	for i := 0; i < 40; i++ {
		rx = append(rx, slack.Reaction{Name: fmt.Sprintf("synth%02d", i), Count: 1, Users: []string{"UBOB"}})
	}
	app.S.SetLatest(ch, []slack.Message{{Type: "message", TS: ts, User: "UALICE", Text: "a synthetic message everyone reacted to", Reactions: rx}}, false)
	key := "message-" + ts + "-" + ch
	t.Cleanup(func() { richview.SetExpandable(key, false) })
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
	if on, ok := richview.Expandable(key); on || ok {
		t.Fatalf("the bar's key has a state before any click: %v %v", on, ok)
	}
	v.s.rows[ts].reactExpand.Click() // "N more"
	for i := 0; i < 3; i++ {
		f()
	}
	if on, _ := richview.Expandable(key); !on {
		t.Fatal(`"N more" didn't open the bar's key`)
	}

	// another view of the message starts open
	reset()
	w := NewChannelView(coreHost{app: app, th: theme.New(false)}, ch)
	g := frameFn(w)
	until(t, 5*time.Second, "the second view's open bar", g, func() bool { return saw(less) })
	if saw(more) {
		t.Error(`the second view drew "N more": it didn't read the store`)
	}

	// "Show less" closes it in the store
	w.s.rows[ts].reactExpand.Click()
	for i := 0; i < 3; i++ {
		g()
	}
	if on, ok := richview.Expandable(key); !ok || on {
		t.Errorf(`"Show less" left the key %v %v, want a stored false`, on, ok)
	}
	reset()
	for i := 0; i < 3; i++ {
		g()
	}
	if !saw(more) || saw(less) {
		t.Error(`after "Show less" the bar isn't collapsed`)
	}
}
