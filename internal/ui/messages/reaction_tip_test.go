package messages

import (
	"encoding/json"
	"image"
	"strings"
	"testing"
	"time"

	"gioui.org/layout"
	"gioui.org/op"
	"gioui.org/op/clip"
	"gioui.org/op/paint"
	"gioui.org/unit"

	"github.com/some-du6e/clack/internal/core"
	"github.com/some-du6e/clack/internal/i18n"
	"github.com/some-du6e/clack/internal/ui/theme"
	"github.com/some-du6e/clack/internal/ui/theme/pixeltest"
)

// Slack's getTip (Qvn6): every reactor, the viewer as You/you, listify's
// serial comma, "others" past 50, "You (click to remove)" alone.
func TestReactionTipText(t *testing.T) {
	many := make([]string, 50)
	for i := range many {
		many[i] = "U" + itoa(i)
	}
	for _, c := range []struct {
		re   core.Reaction
		want string
	}{
		{core.Reaction{Name: "tada", Count: 5, Users: []string{"USELF", "U1", "U2", "U3", "U4"}}, "You, U1, U2, U3, and U4 reacted with :tada:"},
		{core.Reaction{Name: "eyes", Count: 1, Users: []string{"U1"}}, "U1 reacted with :eyes:"},
		{core.Reaction{Name: "eyes", Count: 1, Users: []string{"USELF"}, Self: true}, "You (click to remove) reacted with :eyes:"},
		{core.Reaction{Name: "eyes", Count: 2, Users: []string{"U1", "USELF"}, Self: true}, "U1 and you reacted with :eyes:"},
		{core.Reaction{Name: "fire", Count: 73, Users: many}, strings.Join(many, ", ") + ", and others reacted with :fire:"},
	} {
		if got := plainTip(reactionTip(nilHost{}, &c.re, "USELF")); got != c.want {
			t.Errorf("%v: %q, want %q", c.re.Users, got, c.want)
		}
		if tipCountFor(reactionTip(nilHost{}, &c.re, "USELF")) != c.re.Count {
			t.Errorf("%v: tooltip width from count %d, want %d", c.re.Users, tipCountFor(c.want), c.re.Count)
		}
	}
	names, sub := splitReactionTip("You, U1, and U2 reacted with :tada:")
	if names != "You, U1, and U2" || sub != "reacted with :tada:" {
		t.Errorf("split %q / %q", names, sub)
	}
	// the pill's aria-label and count (Reaction 9FoR, formatNumber)
	if got := reactionAria(&core.Reaction{Name: "white_check_mark", Count: 1}); got != "1 reaction, react with white check mark emoji" {
		t.Errorf("aria %q", got)
	}
	if got := reactionAria(&core.Reaction{Name: "tada", Count: 5}); got != "5 reactions, react with tada emoji" {
		t.Errorf("aria %q", got)
	}
	for n, want := range map[int]string{7: "7", 999: "999", 1000: "1,000", 12345: "12,345", 1234567: "1,234,567"} {
		if got := reactionCount(n); got != want {
			t.Errorf("count %d: %q, want %q", n, got, want)
		}
	}
}

// The reaction tooltip: a dark box above the pill with the light emoji
// tile and the (wrapped) text, inside the window.
func TestReactionTooltipDraws(t *testing.T) {
	th := theme.New(false)
	c := colorsFor(th)
	const w, h = 480, 300
	var ops op.Ops
	paint.FillShape(&ops, c.Bg, clip.Rect{Max: image.Pt(w, h)}.Op())
	gtx := layout.Context{Ops: &ops, Metric: unit.Metric{PxPerDp: 1, PxPerSp: 1}, Constraints: layout.Exact(image.Pt(w, h)), Now: time.Now()}
	long := "You, Alice Example, Bob Example and 12 others reacted with :large_green_circle:"
	anchor := image.Pt(458, 250) // 6px over a pill near the right edge: kept inside
	reactionTooltip(gtx, nilHost{}, th, c, long, "", image.Rect(440, 256, 476, 280), image.Rect(0, 0, w, h))
	img := pixeltest.Render(t, w, h, &ops)
	box := image.Rect(0, 0, w, anchor.Y)
	if n := pixeltest.Count(img, box, c.Tooltip, 4); n < 3000 {
		t.Fatalf("tooltip box %d px", n)
	}
	if n := pixeltest.Count(img, image.Rect(w-3, 0, w, h), c.Tooltip, 4); n > 0 { // keepInBounds: allowanceX 3
		t.Errorf("tooltip runs off the window's right edge (%d px)", n)
	}
	if n := pixeltest.Count(img, box, rgb(0xffffff), 2); n < 64*64-80 {
		t.Errorf("no 64px emoji tile (%d white px)", n)
	}
	// margin-bottom 8 over the pill (anchor + 6), the arrow's tip 6px
	// under the box, at the pill's centre
	if n := pixeltest.Count(img, image.Rect(anchor.X-2, anchor.Y-1, anchor.X+2, anchor.Y+2), c.Tooltip, 4); n == 0 {
		t.Errorf("no arrow under the box")
	}
	if n := pixeltest.Count(img, image.Rect(0, anchor.Y+5, w, h), c.Tooltip, 4); n > 0 {
		t.Errorf("tooltip reaches the pill (%d px)", n)
	}
	if n := pixeltest.Ink(img, box, c.TooltipText, c.Tooltip); n < 60 {
		t.Errorf("no text (%d px)", n)
	}
	// the text wrapped onto several lines instead of running out of the box
	rows := 0
	for y := 0; y < anchor.Y; y++ {
		if pixeltest.Ink(img, image.Rect(0, y, w, y+1), c.TooltipText, c.Tooltip) > 0 {
			rows++
		}
	}
	if rows < 18 {
		t.Errorf("text spans %d px rows, want 2+ lines", rows)
	}
}

// ReactionBar's collapse (Ltiy), worked by hand: twelve one-digit
// reactions estimate 44px each; at 300px six fit a line (264), the limit is
// the pills under 264+300-38 = 526 (11) and the target those under 462
// (10). Twelve collapse to ten and "2 more"; eleven don't.
func TestReactionBarCollapseLikeSlack(t *testing.T) {
	bar := func(n int, self int) *row {
		r := &row{rc: &rowCtx{selfID: "USELF"}}
		for i := 0; i < n; i++ {
			re := core.Reaction{Name: "e" + itoa(i), Count: 1 + i%9, Users: []string{"U1"}}
			if i == self {
				re.Users, re.Self = []string{"USELF"}, true
			}
			r.m.Reactions = append(r.m.Reactions, re)
		}
		r.growPills()
		return r
	}
	gtx := layout.Context{Metric: unit.Metric{PxPerDp: 1, PxPerSp: 1}}
	if shown, more, sel := bar(12, -1).reactionCollapse(gtx, 300); shown != 10 || more != reactMore || sel {
		t.Errorf("12 at 300px: %d shown, button %d, sel %v; want 10, more", shown, more, sel)
	}
	if shown, more, _ := bar(11, -1).reactionCollapse(gtx, 300); shown != 11 || more != 0 {
		t.Errorf("11 at 300px: %d shown, button %d; want all, none", shown, more)
	}
	if shown, more, _ := bar(5, -1).reactionCollapse(gtx, 40); shown != 5 || more != 0 {
		t.Errorf("five never collapse: %d, %d", shown, more)
	}
	// your reaction among the hidden ones marks "N more"
	if _, _, sel := bar(12, 11).reactionCollapse(gtx, 300); !sel {
		t.Errorf("hidden own reaction: not selected")
	}
	// expanded: everything, then "Show less"
	r := bar(12, -1)
	r.reactExpanded = true
	if shown, more, _ := r.reactionCollapse(gtx, 300); shown != 12 || more != reactLess {
		t.Errorf("expanded: %d shown, button %d", shown, more)
	}
	// counts of ten and up estimate 8px wider: 52px each from the 10th
	r = bar(12, -1)
	for i := range r.m.Reactions {
		r.m.Reactions[i].Count = 10
	}
	// 52s: line 1 holds 5 (260), limit under 260+262 = 522 is 10, target under 458 is 8
	if shown, more, _ := r.reactionCollapse(gtx, 300); shown != 8 || more != reactMore {
		t.Errorf("two-digit counts: %d shown, button %d; want 8, more", shown, more)
	}
	// the bar's width is CSS px: at 2x the same 600 device px is 300
	gtx.Metric = unit.Metric{PxPerDp: 2, PxPerSp: 2}
	if shown, _, _ := bar(12, -1).reactionCollapse(gtx, 600); shown != 10 {
		t.Errorf("2x: %d shown, want 10", shown)
	}
}

// ja-JP's getTip strings (the bundle's shape): the subtitle follows the
// name with no space ("U1さんが :tada: でリアクションしました"), the viewer
// is 自分 and listify joins with 、 and no conjunction. The tooltip draws
// the name in bold, glued to the regular subtitle.
func TestReactionTipLocale(t *testing.T) {
	sub := func(s string) string { return `<span className="c-tooltip__subtitle">` + s + `</span>` }
	b, _ := json.Marshal(map[string]map[string]string{
		"reactions": {
			i18n.Key(tipUser)[:7]:        "{user}" + sub("さんが {emoji} でリアクションしました"),
			i18n.Key(tipUsersAndYou)[:7]: "{users_and_you}" + sub("が {emoji} でリアクションしました"),
		},
		"channel_token": {i18n.Key("you")[:7]: "自分"},
	})
	cat, err := i18n.ParseBundle("ja-JP", b)
	if err != nil {
		t.Fatal(err)
	}
	i18n.Use("ja-JP", cat)
	t.Cleanup(func() { i18n.Use(i18n.EnUS, nil) })
	one := reactionTip(nilHost{}, &core.Reaction{Name: "tada", Count: 1, Users: []string{"U1"}}, "USELF")
	if got := plainTip(one); got != "U1さんが :tada: でリアクションしました" {
		t.Errorf("one = %q", got)
	}
	bold, reg := textStyle{size: 13, weight: theme.Bold}, textStyle{size: 13}
	runs := tipRuns(one, bold, reg)
	if len(runs) != 2 || runs[0] != (tipRun{bold, "U1"}) || runs[1] != (tipRun{reg, "さんが :tada: でリアクションしました"}) {
		t.Errorf("runs %+v", runs)
	}
	toks := tipTokens(runs)
	if len(toks) < 2 || toks[0].s != "U1" || toks[1].s != "さ" || toks[1].space {
		t.Errorf("the name and さ should meet with no space: %+v", toks)
	}
	two := reactionTip(nilHost{}, &core.Reaction{Name: "eyes", Count: 2, Users: []string{"U1", "USELF"}}, "USELF")
	if got := plainTip(two); got != "U1、 自分が :eyes: でリアクションしました" {
		t.Errorf("two = %q", got)
	}
}
