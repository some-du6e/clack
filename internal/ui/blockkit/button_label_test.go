package blockkit

import (
	"strings"
	"testing"

	"github.com/some-du6e/clack/internal/ui/theme"
)

// ButtonElement's label is PlainText (boot-shared iY3m) through the
// formatter with emoji on unless the text says emoji: false: one space for
// any run of white space (c-button's white-space: nowrap), a standard
// :shortcode: as its emoji, a workspace one as its 16px image, at most 40
// characters then "…"; the button is at least c-button--small's 56px.
// Karim's vote buttons (":upvote:  Upvote", 2026-11-10) showed the
// shortcode as text.
func TestButtonLabelLikeSlack(t *testing.T) {
	e := newBKEnv(t, false, false)
	up := e.fs.URL + "img/16x16/upvote.png"
	e.h.a.S.CustomEmoji.Put("upvote", up)
	no := false
	cases := []struct {
		name string
		text *Text
		want []btnRun
	}{
		{"workspace emoji", &Text{Type: "plain_text", Text: ":upvote:  Upvote"}, []btnRun{{url: up}, {text: " Upvote"}}},
		{"standard emoji", &Text{Type: "plain_text", Text: ":tada:   Party  time"}, []btnRun{{text: "🎉 Party time"}}},
		{"unknown shortcode", &Text{Type: "plain_text", Text: ":nope: x"}, []btnRun{{text: ":nope: x"}}},
		{"emoji false", &Text{Type: "plain_text", Text: ":upvote: Upvote", Emoji: &no}, []btnRun{{text: ":upvote: Upvote"}}},
		{"40 characters", &Text{Type: "plain_text", Text: strings.Repeat("a", 45)}, []btnRun{{text: strings.Repeat("a", 40) + "…"}}},
	}
	for _, c := range cases {
		got := buttonRuns(e.h, c.text)
		if len(got) != len(c.want) {
			t.Errorf("%s: runs %+v, want %+v", c.name, got, c.want)
			continue
		}
		for i := range got {
			if got[i] != c.want[i] {
				t.Errorf("%s: run %d %+v, want %+v", c.name, i, got[i], c.want[i])
			}
		}
	}
	// widths: the emoji is 16px, not its shortcode; a short label is 56
	me := e.show("buttons")
	gtx := e.context()
	w := newEW(me, &Block{Type: "actions", BlockID: "b"}, &Element{Type: "button", ActionID: "a", Text: &Text{Type: "plain_text", Text: ":upvote:  Upvote"}}, false)
	got := w.size(gtx, 600, false).X
	want := dp(gtx, 16) + measure(gtx, e.h.th, 13, theme.Bold, " Upvote") + 2*dp(gtx, 8)
	if got != want {
		t.Errorf(":upvote: Upvote button %dpx wide, want %d (16px emoji + text + padding)", got, want)
	}
	w = newEW(me, &Block{Type: "actions", BlockID: "b"}, &Element{Type: "button", ActionID: "a", Text: &Text{Type: "plain_text", Text: "OK"}}, false)
	if got := w.size(gtx, 600, false).X; got != dp(gtx, 56) {
		t.Errorf("OK button %dpx wide, want c-button--small's 56", got)
	}
}
