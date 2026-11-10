package richview

import (
	"strconv"
	"testing"
	"time"

	"gioui.org/f32"
)

// EditedLabel's tooltip (boot-shared Y5SQ): "{date} at {time}" with
// toCalendarDateOrNameDay's Today / Yesterday / Tomorrow, else "Nov 7th"
// (", 2025" unless shouldExcludeYear).
func TestEditedTipLikeSlack(t *testing.T) {
	loc := time.FixedZone("test", 0)
	ts := func(x time.Time) string { return strconv.FormatInt(x.Unix(), 10) + ".000100" }
	now := time.Date(2026, 11, 10, 15, 0, 0, 0, loc)
	for _, c := range []struct {
		at   time.Time
		now  time.Time
		want string
	}{
		{time.Date(2026, 11, 10, 11, 1, 0, 0, loc), now, "Today at 11:01 AM"},
		{time.Date(2026, 11, 9, 16, 26, 0, 0, loc), now, "Yesterday at 4:26 PM"},
		{time.Date(2026, 11, 11, 9, 5, 0, 0, loc), now, "Tomorrow at 9:05 AM"},
		{time.Date(2026, 11, 7, 14, 46, 0, 0, loc), now, "Nov 7th at 2:46 PM"},
		{time.Date(2026, 3, 1, 0, 5, 0, 0, loc), now, "Mar 1st at 12:05 AM"},
		{time.Date(2025, 12, 20, 18, 0, 0, 0, loc), now, "Dec 20th, 2025 at 6:00 PM"},
		// last year within 31 days keeps no year
		{time.Date(2025, 12, 20, 18, 0, 0, 0, loc), time.Date(2026, 1, 10, 9, 0, 0, 0, loc), "Dec 20th at 6:00 PM"},
	} {
		if got := editedTip(ts(c.at), c.now); got != c.want {
			t.Errorf("%v: %q, want %q", c.at, got, c.want)
		}
	}
	if got := editedTip("", now); got != "" {
		t.Errorf("no ts: %q", got)
	}
}

// The "(edited)" word carries the tooltip: emojiAt finds it under the
// pointer (and only with an edit time), HoveredEmoji doesn't report it as
// an emoji.
func TestEditedLabelTooltipTarget(t *testing.T) {
	st := newTestStyle()
	v := layoutAt(st, doc(para(txt("hello"))), Options{Edited: true, EditedTS: "1794150000.000100"}, 400)
	ed := findFrag(t, &v.d, "(edited)")
	if ed.tip == 0 || int(ed.tip) > len(v.d.tips) || v.d.tips[ed.tip-1] != editedTipKey {
		t.Fatalf("(edited) frag tip %d, tips %q", ed.tip, v.d.tips)
	}
	p := f32.Pt(ed.x+ed.w/2, float32(ed.top+ed.bot)/2)
	i := v.emojiAt(p)
	if i < 0 || &v.d.frags[i] != ed {
		t.Fatalf("emojiAt(%v) = %d, want the (edited) frag", p, i)
	}
	v.tipFrag = i
	if name, ok := v.HoveredEmoji(); ok {
		t.Errorf("HoveredEmoji reports %q over (edited)", name)
	}
	if name, ok := v.hoveredTip(); !ok || name != editedTipKey {
		t.Errorf("hoveredTip %q %v", name, ok)
	}
	hello := findFrag(t, &v.d, "hello")
	if i := v.emojiAt(f32.Pt(hello.x+1, float32(hello.top+hello.bot)/2)); i >= 0 {
		t.Errorf("the text itself has a tooltip (frag %d)", i)
	}

	v = layoutAt(st, doc(para(txt("hello"))), Options{Edited: true}, 400)
	if ed := findFrag(t, &v.d, "(edited)"); ed.tip != 0 {
		t.Errorf("(edited) without an edit time has tooltip %d", ed.tip)
	}
}
