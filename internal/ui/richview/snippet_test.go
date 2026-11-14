package richview

// Snippets (snippet.go). Synthetic documents only: the server's
// CodeMirror highlight in the theme's colours, light and dark; collapsed,
// expanding and expanded; the pill, the truncation note; what a click on
// each does.

import (
	"image/color"
	"strconv"
	"strings"
	"testing"
	"time"

	"gioui.org/f32"
	"gioui.org/io/pointer"

	rt "github.com/some-du6e/clack/internal/richtext"
)

// cmHTML is Slack's server highlight around lines (each line's HTML).
func cmHTML(lines ...string) string {
	var b strings.Builder
	b.WriteString(`<div class="CodeMirror cm-s-default CodeMirrorServer"><div class="CodeMirror-code">`)
	for _, l := range lines {
		b.WriteString("\n<div><pre>" + l + "</pre></div>")
	}
	b.WriteString("\n</div></div>")
	return b.String()
}

func fibSnippet() *rt.File {
	return &rt.File{ID: "F0SNIP", Name: "fib.py", Type: "python", Mode: "snippet", IsSnippet: true, Lines: 3, Permalink: "https://x.test/F0SNIP",
		Highlight: rt.ParseCodeMirror(cmHTML(
			`<span class="cm-keyword">def</span> <span class="cm-def">fib</span>(<span class="cm-variable">n</span>): <span class="cm-comment">#doc</span>`,
			`    <span class="cm-keyword">return</span> <span class="cm-string">"str"</span> <span class="cm-number">42</span> <span class="cm-bracket">[]</span>`,
			`<span class="cm-variable cm-property">obj</span>`))}
}

func fragCol(t *testing.T, d *display, s string) (color.NRGBA, rt.Style) {
	t.Helper()
	f := findFrag(t, d, s)
	return f.col, f.style
}

// TestSnippetHighlight: the spans take cm-s-default's colours in light
// mode and cm-s-yonce's in dark, the server root's cm-s-default filling
// in what yonce leaves (a bracket); untokened text is the pane's text.
func TestSnippetHighlight(t *testing.T) {
	for _, c := range []struct {
		dark bool
		want map[string]color.NRGBA
		ital map[string]bool
	}{
		{false, map[string]color.NRGBA{"def": rgb(0x77, 0x00, 0x88), "fib": rgb(0, 0, 0xff), "#doc": rgb(0xaa, 0x55, 0),
			`"str"`: rgb(0xaa, 0x11, 0x11), "42": rgb(0x11, 0x66, 0x44), "[]": rgb(0x99, 0x99, 0x77)},
			map[string]bool{"#doc": false}},
		{true, map[string]color.NRGBA{"def": rgb(0, 0xa7, 0xaa), "fib": rgb(0x98, 0xe3, 0x42), "#doc": rgb(0x69, 0x6d, 0x70),
			`"str"`: rgb(0xe6, 0xdb, 0x74), "42": rgb(0xa0, 0x6f, 0xca), "[]": rgb(0x99, 0x99, 0x77), "obj": rgb(0xd1, 0xd2, 0xd3)},
			map[string]bool{"#doc": true, "obj": true}},
	} {
		st := newTestStyle()
		if c.dark {
			st.SetPalette(DarkPalette())
		}
		v := layoutAt(st, doc(fileBlock(fibSnippet())), Options{CompactFiles: true}, 600)
		for s, want := range c.want {
			if col, _ := fragCol(t, &v.d, s); col != want {
				t.Errorf("dark %v: %q is %v, want %v", c.dark, s, col, want)
			}
		}
		for s, it := range c.ital {
			if _, sty := fragCol(t, &v.d, s); (sty&rt.Italic != 0) != it {
				t.Errorf("dark %v: %q italic %v, want %v", c.dark, s, sty&rt.Italic != 0, it)
			}
		}
		if col, _ := fragCol(t, &v.d, "("); col != st.Palette.Text {
			t.Errorf("dark %v: plain text %v, want the pane's %v", c.dark, col, st.Palette.Text)
		}
		if got := string(v.d.plain); !strings.Contains(got, `def fib(n): #doc`+"\n"+`    return "str" 42 []`+"\n"+`obj`) {
			t.Errorf("dark %v: plain %q", c.dark, got)
		}
	}
	// the cascade: yonce's two-class rules beat its one-class ones
	th := map[string]bool{"cm-s-yonce": true, "cm-s-default": true}
	if lk := cmLookOf([]string{"cm-type cm-def"}, th); lk.col != rgb(0xfc, 0x43, 0x84) || lk.italic || !lk.underline {
		t.Errorf("yonce .cm-type.cm-def %+v", lk)
	}
	if lk := cmLookOf([]string{"cm-tag cm-bracket"}, th); lk.col != rgb(0xd1, 0xd2, 0xd3) {
		t.Errorf("yonce .cm-tag.cm-bracket %+v", lk)
	}
	if lk := cmLookOf([]string{"cm-header"}, map[string]bool{"cm-s-default": true}); lk.col != rgb(0, 0, 0xff) || !lk.bold {
		t.Errorf("default .cm-header %+v", lk)
	}
	if lk := cmLookOf([]string{"cm-string", "cm-link"}, map[string]bool{"cm-s-default": true}); lk.col != rgb(0, 0, 0xcc) || !lk.underline {
		t.Errorf("a link in a string %+v", lk)
	}
}

// TestSnippetCollapsed: an expandable snippet (lines_more) is cut at
// 200px under the fade; hovering shows "Show more (N lines)" centred on
// it; a click anywhere on it asks to expand (fetchAndExpand); a short
// snippet has no pill and a click on it does nothing.
func TestSnippetCollapsed(t *testing.T) {
	var lines []string
	for i := 0; i < 30; i++ {
		lines = append(lines, `<span class="cm-keyword">let</span> v`+strconv.Itoa(i)+` = 1;`)
	}
	f := &rt.File{ID: "F0LONG", Name: "long.js", Mode: "snippet", IsSnippet: true, Lines: 42, LinesMore: 12, Highlight: rt.ParseCodeMirror(cmHTML(lines...))}
	h := &actHandler{}
	hs := newHarness(doc(fileBlock(f)), h)
	hs.w = 600
	hs.frame()
	if hs.v.d.size.Y != 1+200+1 {
		t.Errorf("collapsed height %d", hs.v.d.size.Y)
	}
	if len(hs.v.d.pills) != 1 {
		t.Fatalf("%d pills", len(hs.v.d.pills))
	}
	pl := hs.v.d.pills[0]
	if !pl.more || pl.label != "Show more (42 lines)" || pl.btn.Dy() != 24 || pl.btn.Min.Y+12 != 1+100 {
		t.Errorf("pill %+v", pl)
	}
	if mid := (pl.btn.Min.X + pl.btn.Max.X) / 2; mid < 299 || mid > 301 {
		t.Errorf("the pill isn't centred: %v", pl.btn)
	}
	p := f32.Pt(80, 150) // on the code, under the fade
	hs.send(pointer.Event{Kind: pointer.Move, Source: pointer.Mouse, Position: p})
	if hit := hs.v.HitAt(p); hit.Kind != HitFile || hit.File != f {
		t.Errorf("right-click target %+v, want the file", hit)
	}
	hs.send(press(p, time.Second), release(p, time.Second+20*time.Millisecond))
	if len(h.acts) != 1 || h.acts[0] != FileShowMore || h.files[0] != f {
		t.Fatalf("a click on the collapsed snippet: %v", h.acts)
	}
	if len(h.recHandler.files) != 0 {
		t.Error("the click opened the file")
	}

	short := fibSnippet()
	h = &actHandler{}
	hs = newHarness(doc(fileBlock(short)), h)
	hs.w = 600
	hs.frame()
	if len(hs.v.d.pills) != 0 || len(boxesOf(&hs.v.d, bFade)) != 0 {
		t.Errorf("a short snippet: %d pills", len(hs.v.d.pills))
	}
	p = center(findFrag(t, &hs.v.d, "fib"))
	hs.send(pointer.Event{Kind: pointer.Move, Source: pointer.Mouse, Position: p})
	hs.send(press(p, time.Second), release(p, time.Second+20*time.Millisecond))
	if len(h.acts) != 0 || len(h.recHandler.files) != 0 || len(h.recHandler.urls) != 0 {
		t.Errorf("a click on a short snippet: actions %v, files %d, urls %v", h.acts, len(h.recHandler.files), h.recHandler.urls)
	}
	if hit := hs.v.HitAt(p); hit.Kind != HitFile || hit.File != short {
		t.Errorf("the short snippet's right-click target %+v", hit)
	}
}

// TestSnippetExpanded: expanded, the snippet draws its fetched content in
// full with "Show less" 12px off its bottom and Collapse first in its
// FileActions; cut for display, the note under it links the file;
// expanding, the pill is the spinner.
func TestSnippetExpanded(t *testing.T) {
	var lines []string
	for i := 0; i < 30; i++ {
		lines = append(lines, `<span class="cm-keyword">let</span> v`+strconv.Itoa(i)+` = 1;`)
	}
	f := &rt.File{ID: "F0LONG", Name: "long.js", Mode: "snippet", IsSnippet: true, Lines: 30, LinesMore: 25, Permalink: "https://x.test/F0LONG",
		Highlight: rt.ParseCodeMirror(cmHTML(lines[:5]...)), Expanded: true, Content: rt.ParseCodeMirror(cmHTML(lines...)), ContentTruncated: true}
	h := &actHandler{}
	hs := newHarness(doc(fileBlock(f)), h)
	hs.w = 600
	hs.frame()
	findFrag(t, &hs.v.d, "v29")
	note := findFrag(t, &hs.v.d, "truncated")
	code := 1 + 4 + 30*20 + 4
	if note.top < code {
		t.Errorf("the note at %d, the code ends at %d", note.top, code)
	}
	if h := hs.v.d.size.Y; h != code+1+4+19+4+1 {
		t.Errorf("height %d, want %d", h, code+1+4+19+4+1)
	}
	link := findFrag(t, &hs.v.d, "full")
	if tg := hs.v.d.targets[link.target]; tg.kind != tLink || tg.url != f.Permalink {
		t.Errorf("see it in full: %+v", tg)
	}
	pl := hs.v.d.pills[0]
	if pl.more || pl.label != "Show less" || pl.btn.Max.Y != code-12 {
		t.Errorf("pill %+v (code ends at %d)", pl, code)
	}
	if acts := actNames(hs.v.d.bars[0].btns); !strings.HasPrefix(acts, "Collapse Download") {
		t.Errorf("FileActions %s", acts)
	}
	p := rectCenter(pl.btn)
	hs.send(pointer.Event{Kind: pointer.Move, Source: pointer.Mouse, Position: p})
	hs.send(press(p, time.Second), release(p, time.Second+20*time.Millisecond))
	if len(h.acts) != 1 || h.acts[0] != FileShowLess {
		t.Errorf("Show less: %v", h.acts)
	}

	f.Expanded, f.Expanding, f.Content = false, true, nil
	hs.v.Set(doc(fileBlock(f)), Options{})
	hs.frame()
	if pl := hs.v.d.pills[0]; !pl.expanding || pl.btn.Dx() != 16 || pl.btn.Dy() != 16 {
		t.Errorf("expanding: %+v", pl)
	}
	if hs.v.d.size.Y != 1+4+5*20+4+1 {
		t.Errorf("expanding: height %d, want the preview's", hs.v.d.size.Y)
	}
}
