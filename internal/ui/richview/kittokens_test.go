package richview

import (
	"image/color"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/some-du6e/clack/internal/richtext"
	"github.com/some-du6e/clack/internal/ui/slackc/css"
)

// TestKitCardTokens: the new cards' texts take Slack's tokens, light and
// dark (client-boot-styles):
//   - a container's title content-pry, its subtitle content-sec, its
//     collapse caret 1em (15px) at the header's right padding;
//   - an alert's text content-pry (.c-alert--boxed), its links inheriting
//     it, always underlined (.c-alert :any-link);
//   - a call card's title and status rgba(var(--sk_primary_foreground),1),
//     its Meeting ID rgba(var(--sk_foreground_max),.7) over the card body;
//   - a callout's border over its own fill (background-clip border-box).
func TestKitCardTokens(t *testing.T) {
	start := strconv.FormatInt(time.Now().Unix()-600, 10)
	for _, dark := range []bool{false, true} {
		st := newTestStyle()
		if dark {
			st.SetPalette(DarkPalette())
		}
		p := st.Palette
		d := kitDoc(t, `[
			{"type":"container","title":{"type":"plain_text","text":"Alpha"},"subtitle":{"type":"plain_text","text":"Bravo"},"is_collapsible":true,
			 "child_blocks":[{"type":"section","text":{"type":"mrkdwn","text":"Charlie"}}]},
			{"type":"alert","level":"error","text":{"type":"mrkdwn","text":"Delta <https://x.test|Echo>"}},
			{"type":"call","call_id":"R1","call":{"v1":{"id":"R1","name":"Foxtrot","date_start":`+start+`,"display_id":"123-456",
			 "join_url":"https://call.test/j","active_participants":[{"slack_id":"U1"}],"all_participants":[]}}},
			{"type":"callout","background_color":"jade","child_blocks":[{"type":"section","text":{"type":"mrkdwn","text":"Golf"}}]}
		]`)
		v := layoutAt(st, d, Options{}, 800)
		vars := css.SlackVars(dark)
		tok := func(name string) color.NRGBA {
			s := vars["--dt_color-"+name]
			if strings.Count(s, ",") == 2 && !strings.Contains(s, "(") {
				s = "rgb(" + s + ")"
			}
			c, _ := css.ParseColor(s)
			return Over(c, p.Panel)
		}
		sk := func(name string, a float64) color.NRGBA {
			c, _ := css.ParseColor("rgb(" + vars["--"+name] + ")")
			c.A = uint8(float64(c.A)*a + 0.5)
			return c
		}
		body := Over(rgba(p.Text.R, p.Text.G, p.Text.B, 0.04), p.Panel)
		mode := map[bool]string{false: "light", true: "dark"}[dark]
		for _, c := range []struct {
			word string
			want color.NRGBA
		}{
			{"Alpha", tok("content-pry")},
			{"Bravo", tok("content-sec")},
			{"Delta", tok("content-pry")},
			{"Echo", tok("content-pry")},
			{"Foxtrot", Over(sk("sk_primary_foreground", 1), p.Panel)},
			{"Meeting", Over(sk("sk_foreground_max", 0.7), body)},
		} {
			if f := findFrag(t, &v.d, c.word); f.col != c.want {
				t.Errorf("%s: %q drawn in %v, want %v", mode, c.word, f.col, c.want)
			}
		}
		if f := findFrag(t, &v.d, "Echo"); f.style&richtext.Underline == 0 {
			t.Errorf("%s: the alert's link isn't underlined", mode)
		}
		carets := imgsWithURL(&v.d, iconURL+"caret-down")
		var box0 *box
		for i := range v.d.boxes {
			if b := &v.d.boxes[i]; b.kind == bBorder && b.radius == 12 {
				box0 = b
				break
			}
		}
		if len(carets) != 1 || box0 == nil {
			t.Fatalf("%s: %d carets, container %v", mode, len(carets), box0)
		}
		if r := carets[0].r; r.Dx() != 15 || r.Max.X != box0.r.Max.X-1-12 {
			t.Errorf("%s: caret %v, want 15px ending at the header's padding (%d)", mode, r, box0.r.Max.X-13)
		}
		// the callout's translucent border lies over its own jade fill
		jade, otl := rgb(227, 255, 243), rgba(0x5e, 0x5d, 0x60, 0x21/255.0) // plt-jade-0, otl-ter
		if dark {
			jade, otl = rgb(5, 36, 27), rgba(0x79, 0x7c, 0x81, 0x80/255.0) // otl-sec
		}
		var callout *box
		for i := range v.d.boxes {
			if b := &v.d.boxes[i]; b.kind == bBorder && b.radius == 12 && b.fill == jade {
				callout = b
			}
		}
		if callout == nil || callout.stroke != Over(otl, jade) {
			t.Errorf("%s: callout %+v, want its border %v over the jade", mode, callout, Over(otl, jade))
		}
	}
}
