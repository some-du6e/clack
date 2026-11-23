package messages

import (
	"context"
	"fmt"
	"image"
	"net/url"
	"sync"
	"testing"
	"time"

	"github.com/some-du6e/clack/internal/core"
	"github.com/some-du6e/clack/internal/fakeslack"
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

// rxLocalRec records a bar's local changes (ReactionLocal).
type rxLocalRec struct {
	mu    sync.Mutex
	calls []string
}

func (l *rxLocalRec) Reactions() []core.Reaction { return nil }
func (l *rxLocalRec) Change(name string, add, undo bool) {
	l.mu.Lock()
	l.calls = append(l.calls, fmt.Sprintf("%s add=%v undo=%v", name, add, undo))
	l.mu.Unlock()
}

// A bar outside the list (submitOutside): a failed reactions.add takes
// back only a change the bar's own copy made. The store's message, which
// core didn't change (the viewer reacted already), takes none back; a
// message the store doesn't hold changes locally and back.
func TestSubmitOutsideUndoesOnlyItsOwn(t *testing.T) {
	app, fs, ts := shotsApp(t)
	fs.SetHook(func(method string, _ url.Values) (any, bool) {
		if method == "reactions.add" || method == "reactions.remove" {
			return map[string]any{"ok": false, "error": "too_many_reactions"}, true
		}
		return nil, false
	})
	b1, ok := app.S.Find(tShots, ts["b1"])
	if !ok {
		t.Fatal("no b1")
	}
	b1.Reactions = []slack.Reaction{{Name: "wave", Count: 1, Users: []string{fakeslack.SelfID}}}
	app.S.Upsert(tShots, b1)
	h := coreHost{app: app, th: theme.New(false)}
	held := &rxLocalRec{}
	if err := submitOutside(context.Background(), h, app, held, tShots, b1.TS, "wave", true); err == nil {
		t.Fatal("reactions.add didn't fail")
	}
	if len(held.calls) != 0 {
		t.Errorf("the store's message: local changes %v, want none", held.calls)
	}
	gone := &rxLocalRec{}
	if err := submitOutside(context.Background(), h, app, gone, tShots, "1700000099.000100", "wave", true); err == nil {
		t.Fatal("reactions.add didn't fail")
	}
	if want := []string{"wave add=true undo=false", "wave add=true undo=true"}; fmt.Sprint(gone.calls) != fmt.Sprint(want) {
		t.Errorf("a message the store doesn't hold: local changes %v, want %v", gone.calls, want)
	}
}

// A pill that turns disabled under the pointer (its thread locked) loses
// its ReactionTip: a disabled pill takes no hover (.c-reaction:disabled).
func TestReactionTipLeavesADisabledPill(t *testing.T) {
	e := newTipEnvRows(t, twoReactions, 1)
	var top, height int
	until(t, 5*time.Second, "the row", e.frame, func() bool {
		var r *row
		r, top, height, _ = e.item(tsMulti)
		return r != nil && len(r.groups) == 2
	})
	var on image.Point
	for y := top + 2; y < top+height && on == (image.Point{}); y += 3 {
		for x := 40; x < 400; x += 3 {
			if e.move(image.Pt(x, y)); e.v.s.tipKey != "" {
				on = image.Pt(x, y)
				break
			}
		}
	}
	if on == (image.Point{}) {
		t.Fatal("no pill took the pointer")
	}
	e.rest(on, reactionTipDelay+100*time.Millisecond)
	if e.v.s.tip == "" {
		t.Fatal("the hovered pill shows no tip")
	}
	// its thread's replies are turned off: canAddReaction is false
	m, ok := e.app.S.Find(tGen, tsMulti)
	if !ok {
		t.Fatal("the store lost the message")
	}
	m.ThreadTS, m.ReplyCount, m.IsLocked = m.TS, 1, true
	e.app.S.Upsert(tGen, m)
	cm := e.app.MessageFromSlack(tGen, &m)
	ApplyEvent(e.app, core.Event{Type: "update", Channel: tGen, TS: m.TS, ThreadTS: m.TS, Msg: &cm})
	e.rest(on, 200*time.Millisecond)
	if r, _, _, _ := e.item(tsMulti); r == nil || !rxOff(r) {
		t.Fatal("the locked thread's pills aren't disabled: the test checks nothing")
	}
	if e.v.s.tip != "" {
		t.Errorf("the disabled pill keeps its tip %q", e.v.s.tip)
	}
}
