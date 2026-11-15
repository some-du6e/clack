package composer

import (
	"image"
	"testing"
	"time"

	"gioui.org/f32"
	"gioui.org/io/input"
	"gioui.org/io/key"
	"gioui.org/io/pointer"
	"gioui.org/layout"
	"gioui.org/op"
	"gioui.org/op/clip"
	"gioui.org/op/paint"
	"gioui.org/unit"

	"github.com/some-du6e/clack/internal/store"
	"github.com/some-du6e/clack/internal/ui/popups"
	"github.com/some-du6e/clack/internal/ui/slackc"
	"github.com/some-du6e/clack/internal/ui/theme"
	"github.com/some-du6e/clack/internal/ui/theme/pixeltest"
)

// fmtRig is a channel composer driven through a real input.Router at a
// given width, with the formatting toolbar on or off.
type fmtRig struct {
	t   *testing.T
	c   *Composer
	h   *ovHost
	r   input.Router
	ops op.Ops
	now time.Time
	w   int
}

func newFmtRig(t *testing.T, w int, toolbar bool) *fmtRig {
	t.Helper()
	app, _ := syntheticApp(t, func(*store.Store) {})
	stubTyping(t)
	old := showFormatting
	t.Cleanup(func() { showFormatting = old })
	formattingWrite.Lock()
	formattingWrite.pending, formattingWrite.on = 1, toolbar // stands in for the pref
	formattingWrite.Unlock()
	t.Cleanup(func() { formattingWrite.Lock(); formattingWrite.pending = 0; formattingWrite.Unlock() })
	showFormatting = toolbar
	g := &fmtRig{t: t, h: &ovHost{slickHost: &slickHost{app: app, th: theme.New(false)}}, now: time.Now(), w: w}
	g.c = freshComposer(t, g.h)
	g.c.Focus()
	g.settle()
	if !g.c.ed.Focused(layout.Context{Source: g.r.Source()}) {
		t.Fatal("the editor didn't take the focus")
	}
	return g
}

func (g *fmtRig) frame() {
	g.ops.Reset()
	gtx := layout.Context{Ops: &g.ops, Source: g.r.Source(), Now: g.now, Metric: unit.Metric{PxPerDp: 1, PxPerSp: 1},
		Constraints: layout.Constraints{Max: image.Pt(g.w, 400)}}
	slackc.Frame(gtx)
	g.c.Layout(gtx)
	g.r.Frame(&g.ops)
	g.now = g.now.Add(16 * time.Millisecond)
}

func (g *fmtRig) settle() {
	for range 4 {
		g.frame()
	}
}

// draft puts text in the composer and selects [lo, hi).
func (g *fmtRig) draft(text string, lo, hi int) {
	d := NewDoc()
	d.Insert(0, text, Attr{})
	g.c.ed.SetDoc(d)
	g.c.ed.SetSelection(lo, hi)
	g.settle()
}

// click is a left click at p (composer coordinates).
func (g *fmtRig) click(p image.Point) {
	pos := f32.Pt(float32(p.X)+.5, float32(p.Y)+.5)
	g.r.Queue(pointer.Event{Kind: pointer.Move, Source: pointer.Mouse, Position: pos})
	g.frame()
	g.r.Queue(
		pointer.Event{Kind: pointer.Press, Source: pointer.Mouse, Buttons: pointer.ButtonPrimary, Position: pos},
		pointer.Event{Kind: pointer.Release, Source: pointer.Mouse, Position: pos},
	)
	g.settle()
}

// expect checks [lo, hi) has s (or not) and the rest of the draft never
// does, and that the selection is still [lo, hi) (Slack keeps it).
func (g *fmtRig) expect(how string, s Style, lo, hi int, on bool) {
	g.t.Helper()
	d := g.c.ed.Doc()
	if got := d.HasStyle(lo, hi, s); got != on {
		g.t.Errorf("%s: style %v on [%d,%d) is %v, want %v (styles %v)", how, s, lo, hi, got, on, edStyles(d))
	}
	if !on {
		for i := lo; i < hi; i++ {
			if d.Attrs[i].Style&s != 0 {
				g.t.Errorf("%s: rune %d keeps %v after the toggle off", how, i, s)
			}
		}
	}
	for i := 0; i < d.Len(); i++ {
		if (i < lo || i >= hi) && d.Attrs[i].Style&s != 0 {
			g.t.Errorf("%s: rune %d outside the selection got %v", how, i, s)
		}
	}
	if a, b := g.c.ed.Selection(); min(a, b) != lo || max(a, b) != hi {
		g.t.Errorf("%s: selection now [%d,%d), want [%d,%d) kept", how, a, b, lo, hi)
	}
}

var fmtKeys = []struct {
	name  string
	key   key.Name
	mods  key.Modifiers
	style Style
	btn   crBtn
}{
	// SlackKeyboard's bindings (applyFormattingFromShortcut)
	{"bold", "B", key.ModShortcut, Bold, crBold},
	{"italic", "I", key.ModShortcut, Italic, crItalic},
	{"strike", "X", key.ModShortcut | key.ModShift, Strike, crStrike},
	{"code", "C", key.ModShortcut | key.ModShift, Code, crCode},
}

// The format shortcuts on a selection (SlackKeyboard → applyFormatting
// FromShortcut → applyFormattingToSelection): each puts its format on the
// selected text and nowhere else, keeps the selection, and a second press
// takes it off (getFormat(index, length)[format] is set only when all of
// it has it). Karim (α.85): ⌘⇧C didn't make a selection code while typed
// backticks did.
func TestFormatKeysOnSelection(t *testing.T) {
	g := newFmtRig(t, 900, true)
	for _, k := range fmtKeys {
		g.draft("make this formatted", 5, 9)
		for _, on := range []bool{true, false} {
			g.r.Queue(key.Event{Name: k.key, Modifiers: k.mods, State: key.Press})
			g.settle()
			g.expect(k.name+" key", k.style, 5, 9, on)
		}
	}
}

// The formatting toolbar's buttons, clicked with the mouse on a selection,
// do what their shortcuts do: TextyFormattingBar's onClick runs
// applyFormattingToSelection on quill.getSelection().
func TestFormatButtonsOnSelection(t *testing.T) {
	g := newFmtRig(t, 900, true)
	if g.c.ui.fmtHidden != 0 {
		t.Fatalf("%d buttons folded at 900px", g.c.ui.fmtHidden)
	}
	for _, k := range fmtKeys {
		g.draft("make this formatted", 5, 9)
		r := g.c.ui.rect[k.btn].Add(image.Pt(0, g.c.top))
		if r.Empty() {
			t.Fatalf("%s: no button rect", k.name)
		}
		for _, on := range []bool{true, false} {
			g.click(r.Min.Add(r.Size().Div(2)))
			g.expect(k.name+" button", k.style, 5, 9, on)
		}
	}
}

// A narrow composer folds the trailing buttons (Code among them) into the
// bar's ⋯ menu; its Code item formats the selection like the button.
func TestFormatMoreMenuCodeOnSelection(t *testing.T) {
	g := newFmtRig(t, 300, true)
	folded := false
	for _, id := range crFmtOrder[len(crFmtOrder)-g.c.ui.fmtHidden:] {
		folded = folded || id == crCode
	}
	if !folded {
		t.Fatalf("Code isn't in the ⋯ menu at 300px (%d folded)", g.c.ui.fmtHidden)
	}
	g.draft("make this formatted", 5, 9)
	for _, on := range []bool{true, false} {
		r := g.c.ui.rect[crFmtMore].Add(image.Pt(0, g.c.top))
		n := len(g.h.overlays)
		g.click(r.Min.Add(r.Size().Div(2)))
		if len(g.h.overlays) == n {
			t.Fatal("⋯ opened no menu")
		}
		m, ok := g.h.last().(*popups.Menu)
		if !ok {
			t.Fatalf("⋯ opened %T", g.h.last())
		}
		var code *popups.MenuItem
		for i, it := range m.Items() {
			if it.Label == "Code" {
				code = &m.Items()[i]
			}
		}
		if code == nil || code.Do == nil || code.Disabled {
			t.Fatalf("no enabled Code item in %+v", m.Items())
		}
		code.Do()
		g.settle()
		g.expect("⋯ Code", Code, 5, 9, on)
	}
}

// With the toolbar off the floating bar's Code button formats the
// selection too.
func TestFloatBarCodeOnSelection(t *testing.T) {
	g := newFmtRig(t, 900, false)
	g.draft("make this formatted", 5, 9)
	if !g.c.fbar.shown {
		t.Fatal("no floating bar")
	}
	bi := -1
	for i, id := range crFmtOrder {
		if id == crCode {
			bi = i
		}
	}
	for _, on := range []bool{true, false} {
		g.c.fbar.btns[bi].Click()
		g.settle()
		g.expect("floating Code", Code, 5, 9, on)
	}
}

// Code on a selection across a line break and over text that's already
// bold: every selected character takes the code format (the line break
// none), and the bold stays (Slack's formatText only sets code).
func TestCodeOnMixedSelection(t *testing.T) {
	e := NewEditor()
	d := NewDoc()
	d.Insert(0, "ab cd\nef gh", Attr{})
	d.SetStyle(3, 5, Bold, true)
	e.SetDoc(d)
	e.SetSelection(1, 8)
	e.ToggleStyle(Code)
	for i := 1; i < 8; i++ {
		if d := e.Doc(); d.Runes[i] != '\n' && d.Attrs[i].Style&Code == 0 {
			t.Errorf("rune %d (%q) not code: %v", i, d.Runes[i], edStyles(d))
		}
	}
	if !e.Doc().HasStyle(3, 5, Bold) {
		t.Error("code dropped the bold")
	}
	e.ToggleStyle(Code)
	if !e.Doc().HasStyle(3, 5, Bold) || e.Doc().HasStyle(1, 8, Code) {
		t.Errorf("toggle off: %v", edStyles(e.Doc()))
	}
}

// TestComposerSelectionOverCode: selected text on a code block line and in
// an inline code chip shows the highlight (Slack's ::selection over the
// pre's and the code's backgrounds): under the selection's rects most
// pixels differ from the same draft unselected. Light and dark, with the
// colours Composer.editorStyle takes from the theme.
func TestComposerSelectionOverCode(t *testing.T) {
	sh := theme.NewShaper()
	for _, dark := range []bool{false, true} {
		name := "light"
		if dark {
			name = "dark"
		}
		t.Run(name, func(t *testing.T) {
			th := theme.Bare(dark)
			st := DefaultEditorStyle(sh)
			p := &th.C
			st.Color, st.CodeColor, st.CodeBg, st.CodeBorder = p.Fg, p.CodeFg, p.CodeBg, p.CodeBorder
			st.PreBg, st.PreBorder, st.Selection = p.PreBg, p.CodeBorder, p.Selection

			e := NewEditor()
			d := NewDoc()
			d.Insert(0, "plain\nselected code line\na chipword b", Attr{})
			d.Lines[1].Kind = CodeBlock
			chip0 := len([]rune("plain\nselected code line\na "))
			d.SetStyle(chip0, chip0+len("chipword"), Code, true)
			e.SetDoc(d)

			const w, h = 360, 160
			render := func(lo, hi int) *image.RGBA {
				e.SetSelection(lo, hi)
				var ops op.Ops
				paint.FillShape(&ops, p.Bg, clip.Rect{Max: image.Pt(w, h)}.Op())
				gtx := layout.Context{Ops: &ops, Metric: unit.Metric{PxPerDp: 1, PxPerSp: 1}, Constraints: layout.Exact(image.Pt(w, h))}
				e.Layout(gtx, st)
				return pixeltest.Render(t, w, h, &ops)
			}
			plain := render(0, 0)
			sel := render(0, d.Len())
			for _, c := range []struct {
				what   string
				lo, hi int
			}{{"code block", 6, 6 + len("selected code line")}, {"inline code", chip0, chip0 + len("chipword")}} {
				changed, area := 0, 0
				e.lay.forRange(c.lo, c.hi, func(li, x0, x1 int, nl bool) {
					ln := &e.lay.lines[li]
					r := image.Rect(x0+1, ln.top+1, x1-1, ln.top+ln.h-1)
					for y := r.Min.Y; y < r.Max.Y; y++ {
						for x := r.Min.X; x < r.Max.X; x++ {
							area++
							a, b := plain.RGBAAt(x, y), sel.RGBAAt(x, y)
							if absDiff(a.R, b.R) > 8 || absDiff(a.G, b.G) > 8 || absDiff(a.B, b.B) > 8 {
								changed++
							}
						}
					}
				})
				if area == 0 {
					t.Fatalf("%s: no selection rects", c.what)
				}
				if changed < area*4/10 {
					t.Errorf("%s: %d of %d pixels under the selection changed: the highlight is hidden", c.what, changed, area)
				}
			}
		})
	}
}

func absDiff(a, b uint8) int {
	if a > b {
		return int(a - b)
	}
	return int(b - a)
}

// Code on a selection with an emoji and a truncated link
// (maybeAdaptDeltaForCode): the emoji becomes its :name: text and the
// link's "[…]" text its URL, unformatted and then code like the rest; the
// selection covers the new text. Taking code off leaves the text.
func TestCodeOnSelectionWithEmojiAndLink(t *testing.T) {
	const url = "https://example.com/a/long/path/z"
	e := NewEditor()
	d := NewDoc()
	pos := d.Insert(0, "see ", Attr{})
	pos = d.InsertAtom(pos, &Atom{Kind: AtomEmoji, ID: "smile", Text: ":smile:", Uni: "\U0001F604"})
	pos = d.Insert(pos, " at ", Attr{})
	pos = d.Insert(pos, "https://example.com/a[…]/z", Attr{Link: url, Style: Bold})
	d.Insert(pos, " ok", Attr{})
	e.SetDoc(d)
	e.SetSelection(0, e.Doc().Len()-3) // up to before " ok"
	e.ToggleStyle(Code)
	want := "see :smile: at " + url
	if got := e.Doc().String(0, e.Doc().Len()); got != want+" ok" {
		t.Fatalf("text %q, want %q", got, want+" ok")
	}
	n := len([]rune(want))
	if a, b := e.Selection(); a != 0 || b != n {
		t.Errorf("selection [%d,%d), want [0,%d) over the new text", a, b, n)
	}
	dd := e.Doc()
	for i := 0; i < n; i++ {
		if dd.Attrs[i].Atom != nil || dd.Attrs[i].Style&Code == 0 {
			t.Errorf("rune %d (%q) not code text: %+v", i, dd.Runes[i], dd.Attrs[i])
		}
		if i >= len([]rune("see :smile: at ")) && (dd.Attrs[i].Link != "" || dd.Attrs[i].Style&Bold != 0) {
			t.Errorf("rune %d of the URL keeps the link's formats: %+v", i, dd.Attrs[i])
		}
	}
	for i := n; i < dd.Len(); i++ {
		if dd.Attrs[i].Style&Code != 0 {
			t.Errorf("rune %d after the selection is code", i)
		}
	}
	e.ToggleStyle(Code)
	if got := e.Doc().String(0, e.Doc().Len()); got != want+" ok" || e.Doc().HasStyle(0, n, Code) {
		t.Errorf("toggle off: %q code %v", got, e.Doc().HasStyle(0, n, Code))
	}
}

func TestTruncatedLinkText(t *testing.T) {
	const url = "https://example.com/a/long/path/z"
	for _, c := range []struct {
		text string
		want bool
	}{
		{"https://example.com/a[…]/z", true},
		{"https://example.com/a[…]path/z", true},
		{"https://example.com/a[…]", false}, // at the end
		{"[…]/z", false},                    // at the start
		{"https://other.com/[…]/z", false},
		{"https://example.com/a[…]/q", false},
		{"https://example.com/a/long/path/z", false},
		{"a[…]b[…]z", false},
	} {
		if got := truncatedLinkText(c.text, url); got != c.want {
			t.Errorf("%q: %v, want %v", c.text, got, c.want)
		}
	}
}
