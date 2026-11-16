package richview

import (
	"encoding/json"
	"image"
	"image/png"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"gioui.org/op"
	"gioui.org/op/clip"
	"gioui.org/op/paint"

	rt "github.com/some-du6e/clack/internal/richtext"
	"github.com/some-du6e/clack/internal/slack"
	"github.com/some-du6e/clack/internal/ui/theme/pixeltest"
)

// All messages here are synthetic.

// newlineDoc is a message whose rich_text block has these elements.
func newlineDoc(els string) rt.Doc {
	blocks := json.RawMessage(`[{"type":"rich_text","elements":[` + els + `]}]`)
	m := slack.Message{Type: "message", TS: "1762700000.000400", User: "U0", Text: "x", Blocks: blocks}
	return rt.FromMessage(codeRes{}, &m)
}

func nlSection(els ...string) string {
	return `{"type":"rich_text_section","elements":[` + strings.Join(els, ",") + `]}`
}

func nlText(s string) string {
	b, _ := json.Marshal(s)
	return `{"type":"text","text":` + string(b) + `}`
}

// Slack's message body draws a rich_text newline as BaseText does (kWQh
// insertTextBreakPlaceholders): a single "\n" is a <br>, the next line
// 22px down (.p-rich_text_block 15px/1.46668); each newline after the
// first of a run is a .c-mrkdwn__br, an 8px block (22px straight inside a
// bordered quote). Newlines split across two text elements are two <br>s,
// a full empty line; a later section's leading "\n" is doubled into a
// block; the first section's leading newlines are trimmed.
func TestNewlineGapsLikeSlack(t *testing.T) {
	st := newTestStyle()
	const w = 560
	// top of the line holding word
	top := func(t *testing.T, v *View, word string) int {
		t.Helper()
		return findFrag(t, &v.d, word).top
	}
	for _, c := range []struct {
		name string
		els  string
		gap  int // Bravo's line top - Alpha's
	}{
		{"single newline: <br>", nlSection(nlText("Alpha\nBravo")), 22},
		{"double newline: one c-mrkdwn__br", nlSection(nlText("Alpha\n\nBravo")), 22 + 8},
		{"triple newline: two c-mrkdwn__br", nlSection(nlText("Alpha\n\n\nBravo")), 22 + 16},
		{"blank line of spaces: still a run", nlSection(nlText("Alpha\n   \nBravo")), 22 + 8},
		{"newlines in two elements: two <br>", nlSection(`{"type":"text","text":"Alpha\n","style":{"bold":true}}`, nlText("\nBravo")), 22 + 22},
		{"run inside a later element", nlSection(`{"type":"text","text":"Alpha","style":{"bold":true}}`, nlText("\n\nBravo")), 22 + 8},
		{"next section's leading newline: doubled", nlSection(nlText("Alpha")) + `,` + nlSection(nlText("\nBravo")), 22 + 8},
		{"next section's two leading newlines", nlSection(nlText("Alpha")) + `,` + nlSection(nlText("\n\nBravo")), 22 + 16},
		{"quote", `{"type":"rich_text_quote","elements":[` + nlText("Alpha\n\nBravo") + `]}`, 22 + 8},
		{"bordered quote: a 22px c-mrkdwn__br", `{"type":"rich_text_quote","border":1,"elements":[` + nlText("Alpha\n\nBravo") + `]}`, 22 + 22},
		{"list item", `{"type":"rich_text_list","style":"bullet","elements":[` + nlSection(nlText("Alpha\n\nBravo")) + `]}`, 22 + 8},
	} {
		t.Run(c.name, func(t *testing.T) {
			v := layoutAt(st, newlineDoc(c.els), Options{}, w)
			if g := top(t, v, "Bravo") - top(t, v, "Alpha"); g != c.gap {
				t.Errorf("Bravo's line is %dpx under Alpha's, want %d", g, c.gap)
			}
		})
	}

	// heights: the first section's leading newlines draw nothing, a
	// trailing run keeps its block, a trailing <br> adds no line
	one := layoutAt(st, newlineDoc(nlSection(nlText("Alpha"))), Options{}, w).d.size.Y
	for _, c := range []struct {
		name  string
		els   string
		extra int
	}{
		{"leading newlines trimmed", nlSection(nlText("\n\n \nAlpha")), 0},
		{"trailing <br>", nlSection(nlText("Alpha\n")), 0},
		{"trailing run", nlSection(nlText("Alpha\n\n")), 8},
		{"trailing <br> <br>", nlSection(nlText("Alpha\n"), nlText("\n")), 22},
	} {
		t.Run(c.name, func(t *testing.T) {
			v := layoutAt(st, newlineDoc(c.els), Options{}, w)
			if h := v.d.size.Y - one; h != c.extra {
				t.Errorf("%dpx taller than one line, want %d", h, c.extra)
			}
			if c.extra == 0 {
				if y := top(t, v, "Alpha"); y != findFrag(t, &layoutAt(st, newlineDoc(nlSection(nlText("Alpha"))), Options{}, w).d, "Alpha").top {
					t.Errorf("Alpha's line at %d, not where a one-line message has it", y)
				}
			}
		})
	}

	// the plain text (copy) keeps every newline
	v := layoutAt(st, newlineDoc(nlSection(nlText("Alpha\n\nBravo\nCharlie"))), Options{}, w)
	if got := string(v.d.plain); !strings.Contains(got, "Alpha\n\nBravo\nCharlie") {
		t.Errorf("plain text %q", got)
	}
	// and mrkdwn (no blocks) keeps its 8px empty line
	m := slack.Message{Type: "message", TS: "1762700000.000500", User: "U0", Text: "Alpha\n\nBravo"}
	mv := layoutAt(st, rt.FromMessage(codeRes{}, &m), Options{}, w)
	if g := top(t, mv, "Bravo") - top(t, mv, "Alpha"); g != 30 {
		t.Errorf("mrkdwn: Bravo's line is %dpx under Alpha's, want 30", g)
	}
}

// The message body a sender gets from Shift+Enter once, twice and three
// times, then a new section, drawn in light and dark: each line's ink
// starts exactly as far under the one before as Slack's spacing puts it
// (22px for a <br>, 30 for a blank line, 38 for two), with nothing drawn
// in the 8px blocks.
func TestNewlineGapShots(t *testing.T) {
	dir := os.Getenv("MESSAGES_AUDIT_SHOTS_DIR")
	els := nlSection(nlText("Line one\nLine two\n\nLine three\n\n\nLine four")) + `,` +
		`{"type":"rich_text_quote","elements":[` + nlText("Line five\n\nLine six") + `]},` +
		nlSection(nlText("\nLine seven"))
	words := []string{"one", "two", "three", "four", "five", "six", "seven"}
	// line top steps from the line before: <br>, one block, two blocks,
	// the quote's 4px margin, a block in the quote, the quote's 4px margin
	// and the next section's doubled leading newline (one block)
	want := []int{22, 30, 38, 26, 30, 34}
	for _, dark := range []bool{false, true} {
		mode := map[bool]string{false: "light", true: "dark"}[dark]
		pal := LightPalette()
		if dark {
			pal = DarkPalette()
		}
		st := NewStyle(themeShaper(), noImages{}, pal)
		const w, pad = 480, 16
		v := layoutAt(st, newlineDoc(els), Options{}, w-2*pad)
		var tops []int
		for i, wd := range words {
			f := findFrag(t, &v.d, wd)
			tops = append(tops, f.top)
			if i > 0 {
				if g := f.top - tops[i-1]; g != want[i-1] {
					t.Errorf("%s: %q's line is %dpx under the one before, want %d", mode, wd, g, want[i-1])
				}
			}
		}
		h := v.d.size.Y + 2*pad
		var ops op.Ops
		paint.FillShape(&ops, pal.Panel, clip.Rect{Max: image.Pt(w, h)}.Op())
		tr := op.Offset(image.Pt(pad, pad)).Push(&ops)
		v.Layout(testGtx(&ops, w-2*pad), st, &recHandler{})
		tr.Pop()
		img := pixeltest.Render(t, w, h, &ops)
		if dir != "" {
			if f, err := os.Create(filepath.Join(dir, "newline-gaps-"+mode+".png")); err == nil {
				_ = png.Encode(f, img)
				f.Close()
			}
		}
		// the first row with ink of the "Line" that starts wd's line (the
		// same word in the same face on every line, clear of the quote bar)
		inkTop := func(wd string) int {
			f := findFrag(t, &v.d, wd)
			var ln *frag
			for _, g := range textFrags(&v.d) {
				if g.sh.str == "Line" && g.line == f.line {
					ln = g
				}
			}
			if ln == nil {
				t.Fatalf("%s: no \"Line\" on %q's line", mode, wd)
			}
			x0, x1 := int(ln.x)+pad, int(ln.x+ln.w+0.999)+pad
			for y := f.top + pad; y < f.bot+pad; y++ {
				if pixeltest.Ink(img, image.Rect(x0, y, x1, y+1), pal.Text, pal.Panel) > 0 {
					return y
				}
			}
			t.Fatalf("%s: no ink in %q's line box", mode, wd)
			return 0
		}
		prev := inkTop(words[0])
		for i, wd := range words[1:] {
			y := inkTop(wd)
			if g := y - prev; g != want[i] {
				t.Errorf("%s: %q's ink starts %dpx under the line before's, want %d", mode, wd, g, want[i])
			}
			prev = y
		}
		// nothing drawn in the blocks between lines two and three
		two, three := findFrag(t, &v.d, "two"), findFrag(t, &v.d, "three")
		blk := image.Rect(pad, two.bot+pad, w-pad, three.top+pad)
		if blk.Dy() != 8 {
			t.Errorf("%s: the block between lines two and three is %dpx, want 8", mode, blk.Dy())
		}
		if n := pixeltest.Ink(img, blk, pal.Text, pal.Panel); n > 0 {
			t.Errorf("%s: %d px of text ink in the 8px block %v", mode, n, blk)
		}
	}
}
