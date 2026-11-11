package slackc

import (
	"testing"
	"time"
)

// Natives that draw one of Slack's CSS transitions (NativeTransitionsCSS)
// keep Slack's timing with no CSS and under a sheet that doesn't touch it,
// follow a rule over it, and Slick Snappy's override (every transition
// Slack's stylesheet declares at .01ms, no delay) makes them instant.
func TestNativeTransitionsFollowCSS(t *testing.T) {
	defer func() { stack = stack[:0] }()
	var (
		sepRow  = Class("p-thread_separator_row_generic").Mods("p-thread_separator_row_generic--ghost")
		sepIn   = Class("p-threads_flexpane__separator")
		sepLine = Class("p-threads_flexpane__separator_line p-threads_flexpane__separator_line__read").Tag("hr")
		banner  = Class("channelJoinBannerOuterWrapper__TpBQA").Mods("animateSlideDown__Bj5IF")
		block   = Class("block__G68co")
		other   = Class("c-button")
	)
	type got struct{ dur, delay time.Duration }
	query := func() (ghost, notGhost, bannerT, bannerO, blockH, unrelated got) {
		t.Helper()
		sc := CurrentScope()
		q := func(s Styled, prop string, def, defDelay time.Duration) got {
			d, dl := s.Transition(prop, def, defDelay)
			return got{d, dl}
		}
		ghost = q(sc.Child(sepRow, 1).Child(sepIn, 0).Child(sepLine, 0), "opacity", time.Second, time.Minute)
		notGhost = q(sc.Child(sepRow, 0).Child(sepIn, 0).Child(sepLine, 0), "opacity", time.Second, time.Minute)
		bannerT = q(sc.Child(banner, 1), "transform", time.Second, 0)
		bannerO = q(sc.Child(banner, 1), "opacity", time.Second, 0)
		blockH = q(sc.Child(block, 0), "height", time.Second, 0)
		unrelated = q(sc.Child(other, 0), "opacity", 7*time.Millisecond, 0)
		return
	}
	approx := func(g got, dur, delay time.Duration) bool {
		near := func(a, b time.Duration) bool { return a-b < time.Microsecond && b-a < time.Microsecond }
		return near(g.dur, dur) && near(g.delay, delay)
	}

	// no CSS: every native keeps its own (default) timing, at no cost
	if g, _, _, _, _, u := query(); g != (got{time.Second, time.Minute}) || u != (got{7 * time.Millisecond, 0}) {
		t.Fatalf("no CSS: ghost %v, unrelated %v: want the natives' defaults", g, u)
	}

	// a sheet that doesn't touch transitions: Slack's own rules (base sheet)
	SetStyle("x-theme", ".c-message_kit__message { color: red }")
	defer RemoveStyle("x-theme")
	g, ng, bt, bo, bh, u := query()
	if !approx(g, 100*time.Millisecond, 3*time.Second) {
		t.Errorf("theme: ghost line %v, want Slack's opacity .1s 3s", g)
	}
	if ng != (got{time.Second, time.Minute}) {
		t.Errorf("theme: a separator that isn't the ghost got %v, want its default (no Slack rule reaches it)", ng)
	}
	if !approx(bt, 300*time.Millisecond, 0) || !approx(bo, 300*time.Millisecond, 0) {
		t.Errorf("theme: join banner transform %v opacity %v, want Slack's .3s", bt, bo)
	}
	if !approx(bh, 250*time.Millisecond, 0) {
		t.Errorf("theme: agent block %v, want Slack's height .25s", bh)
	}
	if u != (got{7 * time.Millisecond, 0}) {
		t.Errorf("theme: unrelated native %v, want its default", u)
	}

	// Snappy's override, as its transitions scan emits it for Slack's
	// stylesheet (the page's copy of NativeTransitionsCSS)
	SetStyle("plugin:Snappy:transitions:0", `.p-thread_separator_row_generic.p-thread_separator_row_generic--ghost .p-threads_flexpane__separator_line__read,
.c-message_list__unread_divider--ghost .c-message_list__unread_divider__separator,
.channelJoinBannerOuterWrapper__TpBQA,
.block__G68co { transition-duration: .01ms !important; transition-delay: 0s !important; }`)
	g, _, bt, bo, bh, _ = query()
	for name, x := range map[string]got{"ghost": g, "banner transform": bt, "banner opacity": bo, "block": bh} {
		if !approx(x, 10*time.Microsecond, 0) {
			t.Errorf("Snappy: %s %v, want .01ms with no delay", name, x)
		}
	}
	RemoveStyle("plugin:Snappy:transitions:0")

	// transition: none from user.css: the native changes at once
	SetStyle("slick:user", ".block__G68co { transition: none }")
	defer RemoveStyle("slick:user")
	if _, _, _, _, bh, _ = query(); bh != (got{}) {
		t.Errorf("transition: none: agent block %v, want 0, 0", bh)
	}
}

func TestTransitionProgress(t *testing.T) {
	t0 := time.Unix(1000, 0)
	at := func(d time.Duration) time.Time { return t0.Add(d) }
	cases := []struct {
		now        time.Duration
		dur, delay time.Duration
		p          float32
		done       bool
	}{
		{0, 100 * time.Millisecond, 3 * time.Second, 0, false},
		{3*time.Second + 50*time.Millisecond, 100 * time.Millisecond, 3 * time.Second, .5, false},
		{4 * time.Second, 100 * time.Millisecond, 3 * time.Second, 1, true},
		{0, 10 * time.Microsecond, 0, 0, false}, // Snappy: over on the next frame
		{time.Millisecond, 10 * time.Microsecond, 0, 1, true},
		{0, 0, 0, 1, true},
		{0, 200 * time.Millisecond, -100 * time.Millisecond, .5, false}, // a negative delay starts partway
	}
	for _, c := range cases {
		p, done := TransitionProgress(t0, at(c.now), c.dur, c.delay)
		if done != c.done || p-c.p > 1e-3 || c.p-p > 1e-3 {
			t.Errorf("now %v dur %v delay %v: %v %v, want %v %v", c.now, c.dur, c.delay, p, done, c.p, c.done)
		}
	}
}
