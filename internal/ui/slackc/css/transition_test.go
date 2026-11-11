package css

import (
	"math"
	"testing"
)

func nearSec(a, b float32) bool { return math.Abs(float64(a-b)) < 1e-6 }

// The transition shorthand and longhands compute as in a browser, and a
// later longhand rule (Slick Snappy's override) wins over a shorthand.
func TestTransitionFor(t *testing.T) {
	type want struct {
		prop       string
		dur, delay float32
		ok         bool
	}
	cases := []struct {
		name  string
		sheet string
		want  []want
	}{
		{"none set: initial all 0s 0s", ``,
			[]want{{"opacity", 0, 0, true}}},
		{"Slack's ghost line", `.x { transition: opacity .1s 3s }`,
			[]want{{"opacity", .1, 3, true}, {"height", 0, 0, false}}},
		{"two properties", `.x { transition: transform .3s, opacity .3s }`,
			[]want{{"transform", .3, 0, true}, {"opacity", .3, 0, true}, {"color", 0, 0, false}}},
		{"ms and an easing", `.x { transition: height 250ms ease-in-out 40ms }`,
			[]want{{"height", .25, .04, true}}},
		{"cubic-bezier, property last", `.x { transition: .2s cubic-bezier(.36,.19,.29,1) background }`,
			[]want{{"background", .2, 0, true}}},
		{"Snappy's override over the shorthand",
			`.x { transition: opacity .1s 3s } .x { transition-duration: .01ms !important; transition-delay: 0s !important }`,
			[]want{{"opacity", .00001, 0, true}}},
		{"transition: none", `.x { transition: opacity .1s 3s } .x { transition: none }`,
			[]want{{"opacity", 0, 0, false}}},
		{"transition-property: none", `.x { transition: opacity .1s 3s; transition-property: none }`,
			[]want{{"opacity", 0, 0, false}}},
		{"all", `.x { transition: all .4s }`,
			[]want{{"opacity", .4, 0, true}, {"height", .4, 0, true}}},
		{"lists repeat", `.x { transition-property: opacity, transform, height; transition-duration: .1s, .2s; transition-delay: 1s }`,
			[]want{{"opacity", .1, 1, true}, {"transform", .2, 1, true}, {"height", .1, 1, true}}},
		{"the last entry for a property wins", `.x { transition: opacity .1s, opacity .5s 1s }`,
			[]want{{"opacity", .5, 1, true}}},
		{"invalid declarations drop out", `.x { transition: opacity .1s 3s } .x { transition: opacity 1 } .x { transition-duration: -1s } .x { transition: none, opacity 1s }`,
			[]want{{"opacity", .1, 3, true}}},
		{"negative delays are kept", `.x { transition: opacity .2s -.1s }`,
			[]want{{"opacity", .2, -.1, true}}},
		{"var()", `.x { --d: .3s; transition: opacity var(--d) }`,
			[]want{{"opacity", .3, 0, true}}},
		{"-webkit-transition", `.x { -webkit-transition: opacity .3s }`,
			[]want{{"opacity", .3, 0, true}}},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			el := n("div.x")
			st := styleOf(eng(c.sheet), el, testEnv)
			for _, w := range c.want {
				d, dl, ok := st.TransitionFor(w.prop)
				if ok != w.ok || ok && (!nearSec(d, w.dur) || !nearSec(dl, w.delay)) {
					t.Errorf("%s: got %v %v %v, want %v %v %v (property %v duration %v delay %v)", w.prop, d, dl, ok, w.dur, w.delay, w.ok, st.TransitionProperty, st.TransitionDuration, st.TransitionDelay)
				}
			}
		})
	}
}

// Transitions aren't inherited, but inherit takes the parent's.
func TestTransitionInheritance(t *testing.T) {
	kid := n("div.k")
	n("div.p", kid)
	e := eng(`.p { transition: opacity .3s }`)
	st := styleOf(e, kid, testEnv)
	if d, _, _ := st.TransitionFor("opacity"); d != 0 {
		t.Errorf("inherited by default: %v", d)
	}
	e = eng(`.p { transition: opacity .3s } .k { transition: inherit }`)
	st = styleOf(e, kid, testEnv)
	if d, _, ok := st.TransitionFor("opacity"); !ok || !nearSec(d, .3) {
		t.Errorf("transition: inherit gave %v %v", d, ok)
	}
}
