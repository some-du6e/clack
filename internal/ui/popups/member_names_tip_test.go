package popups

import (
	"fmt"
	"image"
	"strings"
	"testing"

	"gioui.org/layout"
	"gioui.org/op"
	"gioui.org/unit"

	"github.com/some-du6e/clack/internal/ui/theme"
)

// TooltipMemberNames (CB7H) and TooltipMemberName (NZA6): every member
// up to 11, else the first 10 and "{count} others"; the viewer is "You"
// first, "you" after; a member not loaded is a missing-text bar; listify
// joins them ("A and B", "A, B, and C").
func TestTooltipMemberNamesLikeSlack(t *testing.T) {
	names := map[string]string{}
	ids := func(n int) []string {
		out := make([]string, n)
		for i := range out {
			out[i] = fmt.Sprintf("U%02d", i)
			names[out[i]] = fmt.Sprintf("Member %d", i)
		}
		return out
	}
	name := func(id string) (string, bool) { n, ok := names[id]; return n, ok }
	texts := func(items []TipName) []string {
		var out []string
		for _, it := range items {
			if it.Missing {
				out = append(out, "▭")
				continue
			}
			out = append(out, it.Text)
		}
		return out
	}
	if got := texts(TooltipMemberNameItems(ids(11), "", name)); len(got) != 11 || got[10] != "Member 10" {
		t.Errorf("11 members: %q, want all 11", got)
	}
	if got := texts(TooltipMemberNameItems(ids(12), "", name)); len(got) != 11 || got[9] != "Member 9" || got[10] != "2 others" {
		t.Errorf("12 members: %q, want 10 and \"2 others\"", got)
	}
	if got := texts(TooltipMemberNameItems(ids(40), "", name)); got[10] != "30 others" {
		t.Errorf("40 members: last %q, want \"30 others\"", got[10])
	}
	three := ids(3)
	if got := texts(TooltipMemberNameItems(three, "U00", name)); strings.Join(got, "|") != "You|Member 1|Member 2" {
		t.Errorf("the viewer first: %q", got)
	}
	if got := texts(TooltipMemberNameItems(three, "U02", name)); strings.Join(got, "|") != "Member 0|Member 1|you" {
		t.Errorf("the viewer last: %q", got)
	}
	if got := texts(TooltipMemberNameItems([]string{"U00", "UNOBODY"}, "", name)); strings.Join(got, "|") != "Member 0|▭" {
		t.Errorf("a member not loaded: %q", got)
	}

	join := func(items ...TipName) string {
		var b strings.Builder
		for _, r := range tipRuns(items) {
			if r.bar {
				b.WriteString("▭")
				continue
			}
			b.WriteString(r.text)
		}
		return b.String()
	}
	for _, c := range []struct {
		items []TipName
		want  string
	}{
		{[]TipName{{Text: "Ada"}}, "Ada"},
		{[]TipName{{Text: "Ada"}, {Text: "Grace"}}, "Ada and Grace"},
		{[]TipName{{Text: "You"}, {Text: "Ada"}, {Text: "Grace"}}, "You, Ada, and Grace"},
		{[]TipName{{Text: "Ada"}, {Missing: true}, {Text: "4 others"}}, "Ada, ▭, and 4 others"},
	} {
		if got := join(c.items...); got != c.want {
			t.Errorf("listify %+v: %q, want %q", c.items, got, c.want)
		}
	}
}

// The c-tooltip__tip body: at most 200px wide (padding 8 12 inside),
// lines of 13px at 1.25, a long list wrapping onto several.
func TestMemberNamesTipWraps(t *testing.T) {
	sh := theme.NewShaper()
	th := theme.New(false)
	th.SetShapers(sh, sh)
	gtx := layout.Context{Ops: new(op.Ops), Metric: unit.Metric{PxPerDp: 1, PxPerSp: 1}, Constraints: layout.Constraints{Max: image.Pt(1000, 1000)}}
	one := MemberNamesTip(gtx, th, []TipName{{Text: "Ada"}})
	if one.Size.X > 200 || one.Size.Y < 16+16 || one.Size.Y > 16+18 {
		t.Errorf("one name: %v, want one 16px line inside 8px padding", one.Size)
	}
	var many []TipName
	for i := range 10 {
		many = append(many, TipName{Text: fmt.Sprintf("Member Number %d", i)})
	}
	many = append(many, TipName{Text: "4 others"})
	d := MemberNamesTip(gtx, th, many)
	if d.Size.X > 200 || d.Size.X < 150 {
		t.Errorf("ten names: %d wide, want the tip's 200px max-width filled", d.Size.X)
	}
	if lh := one.Size.Y - 16; d.Size.Y < 4*lh+16 || (d.Size.Y-16)%lh != 0 {
		t.Errorf("ten names: %d tall, want whole %dpx lines, at least 4 of them", d.Size.Y, lh)
	}
	long := MemberNamesTip(gtx, th, []TipName{{Text: strings.Repeat("W", 40)}})
	if long.Size.X > 200 {
		t.Errorf("an unbreakable name: %d wide, want it broken at 200 (overflow-wrap: break-word)", long.Size.X)
	}
}
