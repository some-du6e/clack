package messages

import (
	"testing"
	"time"

	"github.com/some-du6e/clack/internal/slack"
	"github.com/some-du6e/clack/internal/ui/slackc"
	"github.com/some-du6e/clack/internal/ui/theme"
)

// ReactionBar's canReact is canAddReaction: in a thread whose replies are
// turned off the bar has no add button and its pills are disabled
// (disabled={!canReact}, .c-reaction:disabled pointer-events none: no
// click, hover or tooltip, and a right-click is the message's); an open
// message's bar is as before.
func TestReactionBarDisabledLikeSlack(t *testing.T) {
	app, _, ts := shotsApp(t)
	b1, ok := app.S.Find(tShots, ts["b1"])
	if !ok {
		t.Fatal("no b1")
	}
	b1.Reactions = []slack.Reaction{{Name: "wave", Count: 1, Users: []string{"UBEN00001"}}}
	b1.ThreadTS, b1.ReplyCount, b1.IsLocked = b1.TS, 1, true
	app.S.Upsert(tShots, b1)
	b2, ok := app.S.Find(tShots, ts["b2"])
	if !ok {
		t.Fatal("no b2")
	}
	b2.Reactions = []slack.Reaction{{Name: "tada", Count: 1, Users: []string{"UBEN00001"}}}
	app.S.Upsert(tShots, b2)
	if !app.ThreadLocked(tShots, b1.TS) {
		t.Fatal("b1's thread isn't locked: the test checks nothing")
	}
	host := coreHost{app: app, th: theme.New(false)}
	v := NewChannelView(host, tShots)
	v.Preview = true
	f := frameFn(v)
	until(t, 5*time.Second, "the pills", f, func() bool {
		r1, r2 := v.s.rows[ts["b1"]], v.s.rows[ts["b2"]]
		return r1 != nil && r2 != nil && len(r1.groups) == 1 && len(r2.groups) == 1 && r1.rc != nil && r2.rc != nil
	})
	f()
	addKid := func(p slackc.Props) bool {
		for _, k := range p.List("children") {
			if el, ok := k.(slackc.Slot); ok && el.Name == "ReactionAddButton" {
				return true
			}
		}
		return false
	}
	r1 := v.s.rows[ts["b1"]]
	if !rxOff(r1) {
		t.Error("the locked thread's root's pills aren't disabled")
	}
	if p := r1.reactionBarProps(); p.Bool("canReact") || addKid(p) {
		t.Errorf("the locked root's bar: canReact %v, add button %v", p.Bool("canReact"), addKid(p))
	}
	if !r1.addRect.Empty() {
		t.Error("the locked root's bar draws the add button")
	}
	if r1.pills[0].props().Bool("canReact") {
		t.Error("the locked root's pill has canReact")
	}
	r2 := v.s.rows[ts["b2"]]
	if rxOff(r2) {
		t.Error("an open message's pills are disabled")
	}
	if p := r2.reactionBarProps(); !p.Bool("canReact") || !addKid(p) || r2.addRect.Empty() {
		t.Errorf("an open message's bar: canReact %v, add button %v, drawn %v", p.Bool("canReact"), addKid(p), r2.addRect)
	}
}
