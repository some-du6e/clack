package richview

import (
	"image/color"
	"testing"

	rt "github.com/some-du6e/clack/internal/richtext"
)

// Slack's mention classes (client-boot-styles .c-member_slug--link and
// --mention, .c-mrkdwn__user_group--link and --mention, .c-mrkdwn__broadcast
// --link --mention; the 2026 tokens of slack-kit-gantry-v2-styles): every
// mention box has 3px corners and 1px 2px padding. A person or group:
// content-hgl-1 on surf-hgl-1 (light #1264a3 on #36c5f021, dark #2ba5ce on
// #03669640). A mention of you, or of a group you're in: surf-hgl-3 behind
// the link color (#ffc6002e; dark #a073002e under content-hgl-3 #dea700).
// A broadcast: bold, surf-hgl-3, the text color (dark content-hgl-3).
// Karim's dark "@channel" in Activity (2026-11-10) was light text on a
// rgba(242,199,68,.3) box.
func TestMentionColorsLikeSlack(t *testing.T) {
	for _, dark := range []bool{false, true} {
		st := newTestStyle()
		if dark {
			st.SetPalette(DarkPalette())
		}
		p := st.Palette
		d := doc(para(
			rt.Span{Kind: rt.User, Text: "@alice", ID: "U1"}, txt(" "),
			rt.Span{Kind: rt.User, Text: "@me", ID: "U9", Self: true}, txt(" "),
			rt.Span{Kind: rt.Usergroup, Text: "@design", ID: "S1", Self: true}, txt(" "),
			rt.Span{Kind: rt.Usergroup, Text: "@ops", ID: "S2"}, txt(" "),
			rt.Span{Kind: rt.Broadcast, Text: "@channel", ID: "channel", Self: true},
		))
		v := layoutAt(st, d, Options{}, 800)
		mode := map[bool]string{false: "light", true: "dark"}[dark]
		link, self, text := rgb(18, 100, 163), rgb(18, 100, 163), rgb(29, 28, 29)
		linkBg, selfBg := Over(rgba(54, 197, 240, 0x21/255.0), p.Panel), Over(rgba(255, 198, 0, 0x2e/255.0), p.Panel)
		if dark {
			link, self, text = rgb(43, 165, 206), rgb(222, 167, 0), rgb(222, 167, 0)
			linkBg, selfBg = Over(rgba(3, 102, 150, 0x40/255.0), p.Panel), Over(rgba(160, 115, 0, 0x2e/255.0), p.Panel)
		}
		for _, c := range []struct {
			text   string
			fg, bg color.NRGBA
		}{
			{"@alice", link, linkBg}, {"@me", self, selfBg}, {"@design", self, selfBg}, {"@ops", link, linkBg}, {"@channel", text, selfBg},
		} {
			f := findFrag(t, &v.d, c.text)
			if f.col != c.fg {
				t.Errorf("%s %s: text %v, want %v", mode, c.text, f.col, c.fg)
			}
			var pill *box
			for i := range v.d.boxes {
				if b := &v.d.boxes[i]; b.kind == bFill && boxContainsFrag(*b, f) {
					pill = b
				}
			}
			if pill == nil {
				t.Errorf("%s %s: no box", mode, c.text)
				continue
			}
			if pill.fill != c.bg {
				t.Errorf("%s %s: box %v, want %v", mode, c.text, pill.fill, c.bg)
			}
			if pill.radius != 3 {
				t.Errorf("%s %s: corners %d, want 3", mode, c.text, pill.radius)
			}
			if l, r := f.x-float32(pill.r.Min.X), float32(pill.r.Max.X)-(f.x+f.w); l < 1.5 || l > 2.5 || r < 1.5 || r > 2.5 {
				t.Errorf("%s %s: horizontal padding %.1f / %.1f, want 2", mode, c.text, l, r)
			}
		}
	}
}
