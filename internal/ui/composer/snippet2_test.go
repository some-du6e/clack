package composer

import (
	"image"
	"image/color"
	"testing"

	"gioui.org/font"
	"gioui.org/layout"
	"gioui.org/op"
	"gioui.org/unit"
	"gioui.org/widget"

	"github.com/some-du6e/clack/internal/core"
	"github.com/some-du6e/clack/internal/slack"
	"github.com/some-du6e/clack/internal/store"
	"github.com/some-du6e/clack/internal/ui/theme"
)

// edLaidOut lays ed out once in the snippet editor's font.
func edLaidOut(th *theme.Theme, ed *widget.Editor) {
	gtx := layout.Context{Ops: new(op.Ops), Metric: unit.Metric{PxPerDp: 1, PxPerSp: 1}, Constraints: layout.Constraints{Max: image.Pt(2000, 400)}}
	ed.Layout(gtx, th.Shaper, font.Font{Typeface: font.Typeface(Mono)}, snippetCodeSize, op.CallOp{}, op.CallOp{})
}

// Gio patch 7 and CodeMirror's tabSize: a tab reaches the next multiple
// of 4 columns from the line's start, so "a\tb" puts b where four
// characters would, and "abcd\te" puts e at column 8.
func TestSnippetTabStops(t *testing.T) {
	th := theme.New(false)
	x := func(s string, at int) int {
		var ed widget.Editor
		ed.TabStop = snippetTabSize
		ed.SetText(s)
		edLaidOut(th, &ed)
		rs := ed.Regions(at, at+1, nil)
		if len(rs) == 0 {
			t.Fatalf("no region for %q at %d", s, at)
		}
		return rs[0].Bounds.Min.X
	}
	// a tab after one character takes three columns of spaces
	if a, b := x("a\tb", 2), x("a   b", 4); a != b {
		t.Errorf("after a tab: x %d, want a and three spaces' %d", a, b)
	}
	// after four, a whole stop
	if a, b := x("abcd\te", 5), x("abcd    e", 8); a != b {
		t.Errorf("after abcd and a tab: x %d, want abcd and four spaces' %d", a, b)
	}
	if a, b := x("\t\tz", 2), x("        z", 8); a != b {
		t.Errorf("two tabs: x %d, want eight spaces' %d", a, b)
	}
}

// CodeMirror's defaultTab with indentWithTabs: a tab at the caret, or a
// tab at the start of each selected line.
func TestSnippetTabKey(t *testing.T) {
	var ed widget.Editor
	ed.SetText("ab")
	ed.SetCaret(1, 1)
	snippetTab(&ed)
	if ed.Text() != "a\tb" {
		t.Errorf("tab at the caret: %q", ed.Text())
	}
	ed.SetText("one\ntwo\nthree")
	ed.SetCaret(1, 6) // "ne\ntw"
	snippetTab(&ed)
	if ed.Text() != "\tone\n\ttwo\nthree" {
		t.Errorf("indented lines: %q", ed.Text())
	}
	ed.SetText("one\ntwo")
	ed.SetCaret(0, 4) // to the start of "two": that line stays
	snippetTab(&ed)
	if ed.Text() != "\tone\ntwo" {
		t.Errorf("selection ending at a line start: %q", ed.Text())
	}
}

// Highlighting follows the picked Type (no mode: no colours) in the
// CodeMirror theme's classes.
func TestSnippetHighlightClasses(t *testing.T) {
	if snippetLangs["auto"] != "" || snippetLangs["text"] != "" {
		t.Error("Auto Detect Type and Plain Text have no CodeMirror mode")
	}
	spans := highlightLine("go", `func main() { s := "hi" // done`)
	want := map[string]cmClass{"func": cmKeyword, `"hi"`: cmString, "// done": cmComment}
	line := []rune(`func main() { s := "hi" // done`)
	got := map[string]cmClass{}
	for _, sp := range spans {
		got[string(line[sp.start:sp.end])] = sp.class
	}
	for w, c := range want {
		if got[w] != c {
			t.Errorf("%q class %d, want %d (spans %v)", w, got[w], c, spans)
		}
	}
	if spans := highlightLine("sql", "SELECT 1 FROM t"); len(spans) == 0 || spans[0].class != cmKeyword {
		t.Errorf("SQL keywords are case-insensitive: %v", spans)
	}
	light, dark := cmColors(false), cmColors(true)
	if light[cmKeyword] != (color.NRGBA{R: 0x77, G: 0x00, B: 0x88, A: 0xff}) || dark[cmKeyword] != (color.NRGBA{R: 0x00, G: 0xa7, B: 0xaa, A: 0xff}) {
		t.Errorf("keyword colours %v / %v, want cm-s-default #708 and cm-s-yonce #00a7aa", light[cmKeyword], dark[cmKeyword])
	}
}

// ShareDialogAlert's willJoinChannel: a public channel the viewer isn't in
// warns that sharing joins it; one they're in doesn't.
func TestSnippetShareWarning(t *testing.T) {
	app, _ := syntheticApp(t, func(s *store.Store) {
		s.PutConv(slack.Conversation{ID: "C0PUBLIC1", Name: "lounge", IsChannel: true})
	})
	h := &ovHost{slickHost: &slickHost{app: app, th: theme.New(false)}}
	c := freshComposer(t, h)
	d := openSnippetDialog(c, snippetSpec{text: "x", ch: tChGen})
	if w := d.shareWarning(); w != "" {
		t.Errorf("#general (a member): %q", w)
	}
	d.ch = "C0SEARCHED"
	d.picked = core.Conv{ID: "C0SEARCHED", Name: "elsewhere", Kind: "channel", Preview: true}
	if w := d.shareWarning(); w != "You will join this channel when you share the file into it." {
		t.Errorf("a searched channel you're not in: %q", w)
	}
}

// ShareDialogChannelPicker: typing filters the conversations (prefix
// matches first), the empty query lists them all, and a server hit joins
// the rows.
func TestSnippetConvPicker(t *testing.T) {
	app, _ := syntheticApp(t, func(s *store.Store) {
		s.PutConv(slack.Conversation{ID: "C0RANDOM1", Name: "random", IsChannel: true, IsMember: true})
		s.PutConv(slack.Conversation{ID: "C0GENDEV1", Name: "dev-general", IsChannel: true, IsMember: true})
	})
	h := &ovHost{slickHost: &slickHost{app: app, th: theme.New(false)}}
	c := freshComposer(t, h)
	d := openSnippetDialog(c, snippetSpec{text: "x", ch: tChGen})
	p := newConvPicker(d, image.Rect(100, 600, 360, 636))
	if len(p.rows) < 3 {
		t.Fatalf("empty query rows %v", p.rows)
	}
	p.query = "gen"
	p.refilter()
	if len(p.rows) != 2 || p.rows[0].Name != "general" || p.rows[1].Name != "dev-general" {
		t.Errorf("\"gen\" rows %v, want general then dev-general", p.rows)
	}
	p.server = []core.Conv{{ID: "C0FAR0001", Name: "general-far", Kind: "channel", Preview: true}}
	p.refilter()
	if len(p.rows) != 3 || p.rows[2].ID != "C0FAR0001" {
		t.Errorf("with a server hit %v", p.rows)
	}
	p.pick(2)
	if d.ch != "C0FAR0001" || !p.done {
		t.Errorf("picked %q (done %v)", d.ch, p.done)
	}
	if cv, ok := d.conv(); !ok || cv.Name != "general-far" {
		t.Errorf("the dialog's conversation %+v %v", cv, ok)
	}
}
