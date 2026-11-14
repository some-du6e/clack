package huddleui

import (
	"strings"
	"testing"

	"gioui.org/op"

	"github.com/some-du6e/clack/internal/ui/theme"
)

// Huddle text Clack wraps itself (the invite card and the preview's title,
// the captions) breaks where the browser breaks Slack's text: after spaces,
// and between CJK characters, since ja-JP's sentences have no spaces
// (kit.go lineBreaks).

func spansText(lines [][]span) (text string, bold []bool) {
	var b strings.Builder
	for _, ln := range lines {
		for _, s := range ln {
			b.WriteString(s.Text)
			for range s.Text {
				bold = append(bold, s.Bold)
			}
		}
	}
	return b.String(), bold
}

func TestWrapSpansCJK(t *testing.T) {
	th := theme.New(false)
	var ops op.Ops
	gtx := shotCtx(&ops, 400, 400)
	w := gtx.Dp(200)
	for _, in := range [][]span{
		// the ja-JP card: "{sender}さんから{channel}でのハドルミーティングに招待されました"
		{{Text: "Remote Example", Bold: true}, {Text: "さんから "}, {Text: "#huddle-test", Bold: true}, {Text: " でのハドルミーティングに招待されました。"}},
		{{Text: "Remote Example", Bold: true}, {Text: " is inviting you to a huddle in "}, {Text: "#huddle-test", Bold: true}},
	} {
		lines := wrapSpans(gtx, th, 15, in, w)
		if len(lines) < 2 {
			t.Fatalf("%v in one line at %d px", in, w)
		}
		for i, ln := range lines {
			lw := 0
			for j, s := range ln {
				wt := theme.Regular
				if s.Bold {
					wt = theme.Bold
				}
				txt := s.Text
				if j == len(ln)-1 {
					txt = strings.TrimRight(txt, " ") // a line's last spaces hang
				}
				lw += labelSize(gtx, th, 15, wt, txt).X
			}
			if lw > w {
				t.Errorf("line %d %v is %d px, over %d", i, ln, lw, w)
			}
			if i > 0 && strings.HasPrefix(ln[0].Text, " ") {
				t.Errorf("line %d %v starts with a space", i, ln)
			}
		}
		got, gotBold := spansText(lines)
		want, wantBold := spansText([][]span{in})
		if got != want {
			t.Fatalf("lines %q, want the text %q", got, want)
		}
		for i := range wantBold {
			if gotBold[i] != wantBold[i] {
				t.Fatalf("rune %d bold %v, want %v (%v)", i, gotBold[i], wantBold[i], lines)
			}
		}
	}
}

// TestCaptionsWrapCJK: a ja-JP caption line ("Captions are on.") wraps in
// a narrow caption box instead of running out of it.
func TestCaptionsWrapCJK(t *testing.T) {
	w := &window{th: theme.New(false)}
	var ops op.Ops
	gtx := shotCtx(&ops, 400, 400)
	maxW := gtx.Dp(120)
	// ccEvent's width is clamped to maxW; the lines show the wrapping
	sz := w.ccEvent(gtx, "", "字幕がオンになっています。字幕がオンになっています。", maxW)
	if sz.Y < 2*gtx.Sp(22) {
		t.Errorf("caption %d px tall: didn't wrap", sz.Y)
	}
}
