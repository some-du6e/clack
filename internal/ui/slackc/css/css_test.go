package css

import (
	"fmt"
	"image/color"
	"math"
	"strings"
	"testing"
)

// --- a fake element tree ---

type fakeEl struct {
	tag, id   string
	classes   []string
	attrs     map[string]string
	parent    *fakeEl
	kids      []*fakeEl
	st        State
	text      bool
	noIndex   bool // behaves like a native widget: Index unknown
	noKids    bool // not a KidsElement (wrapped by nativeLike)
	pseudoTag string
}

// n builds an element from "tag#id.c1.c2" and children.
func n(sel string, kids ...*fakeEl) *fakeEl {
	e := &fakeEl{attrs: map[string]string{}}
	tag := sel
	if i := strings.IndexAny(sel, "#."); i >= 0 {
		tag = sel[:i]
		rest := sel[i:]
		for rest != "" {
			j := strings.IndexAny(rest[1:], "#.")
			part := rest
			if j >= 0 {
				part = rest[:j+1]
				rest = rest[j+1:]
			} else {
				rest = ""
			}
			if part[0] == '#' {
				e.id = part[1:]
			} else {
				e.classes = append(e.classes, part[1:])
			}
		}
	}
	if tag == "" {
		tag = "div"
	}
	e.tag = tag
	for _, k := range kids {
		k.parent = e
		e.kids = append(e.kids, k)
	}
	return e
}

func (e *fakeEl) with(k, v string) *fakeEl { e.attrs[k] = v; return e }
func (e *fakeEl) state(s State) *fakeEl    { e.st = s; return e }

func (e *fakeEl) Tag() string { return e.tag }
func (e *fakeEl) ID() string  { return e.id }
func (e *fakeEl) HasClass(c string) bool {
	for _, x := range e.classes {
		if x == c {
			return true
		}
	}
	return false
}
func (e *fakeEl) Classes() []string { return e.classes }
func (e *fakeEl) Attr(name string) (string, bool) {
	switch name {
	case "class":
		return strings.Join(e.classes, " "), len(e.classes) > 0
	case "id":
		return e.id, e.id != ""
	}
	v, ok := e.attrs[name]
	return v, ok
}
func (e *fakeEl) Parent() Element {
	if e.parent == nil {
		return nil
	}
	return e.parent
}
func (e *fakeEl) Prev() Element {
	if e.parent == nil || e.noIndex {
		return nil
	}
	var prev *fakeEl
	for _, k := range e.parent.kids {
		if k == e {
			break
		}
		prev = k
	}
	if prev == nil {
		return nil
	}
	return prev
}
func (e *fakeEl) Index() (int, int) {
	if e.noIndex {
		return 0, 0
	}
	if e.parent == nil {
		return 0, 1
	}
	for i, k := range e.parent.kids {
		if k == e {
			return i, len(e.parent.kids)
		}
	}
	return 0, 0
}
func (e *fakeEl) TypeIndex() (int, int) {
	if e.noIndex {
		return 0, 0
	}
	if e.parent == nil {
		return 0, 1
	}
	i, cnt := 0, 0
	for _, k := range e.parent.kids {
		if k.tag == e.tag {
			if k == e {
				i = cnt
			}
			cnt++
		}
	}
	return i, cnt
}
func (e *fakeEl) State() State { return e.st }
func (e *fakeEl) Kids() []Element {
	out := make([]Element, len(e.kids))
	for i, k := range e.kids {
		out[i] = k
	}
	return out
}
func (e *fakeEl) HasText() bool { return e.text }

// nativeLike hides Kids (like Clack's native widgets).
type nativeLike struct{ *fakeEl }

func (n nativeLike) Kids() {}

// --- helpers ---

var testEnv = Env{Width: 1200, Height: 800, RootFont: 16, Fg: color.NRGBA{A: 255}}

func eng(sheets ...string) *Engine {
	e := NewEngine()
	for i, s := range sheets {
		e.Set(fmt.Sprint("s", i), s)
	}
	return e
}

// styleOf computes el's style through its ancestor chain.
func styleOf(e *Engine, el *fakeEl, env Env) Style {
	var chain []*fakeEl
	for x := el; x != nil; x = x.parent {
		chain = append(chain, x)
	}
	var parent *Style
	for i := len(chain) - 1; i >= 0; i-- {
		c := e.Cascade(chain[i], PseudoNone, nil, env)
		s := Compute(&c, parent, env)
		parent = &s
	}
	return *parent
}

func has(c Cascaded, prop string) bool { return c.Has(prop) }

func val(t *testing.T, c Cascaded, prop string) string {
	t.Helper()
	v, _ := c.Get(prop)
	return v
}

func rgb(r, g, b uint8) color.NRGBA { return color.NRGBA{r, g, b, 255} }

func colorOf(t *testing.T, css string) color.NRGBA {
	t.Helper()
	el := n("div.x")
	e := eng(".x { color: " + css + " }")
	c := e.Cascade(el, PseudoNone, nil, testEnv)
	if !c.Has("color") {
		t.Fatalf("color %q rejected", css)
	}
	return styleOf(e, el, testEnv).Color
}

func near(a, b float32) bool { return math.Abs(float64(a-b)) < 0.01 }

// --- tokenizer ---

func TestTokenizer(t *testing.T) {
	toks := tokenize(`a/* c */.b-c\31 x "s\"t\
r" url( x.png ) url("q") #f00 12.5px -3% @media <!-- --> 1e3`)
	var kinds []tokKind
	var vals []string
	for _, tk := range toks {
		if tk.k == tWS {
			continue
		}
		kinds = append(kinds, tk.k)
		vals = append(vals, tk.s)
	}
	want := []tokKind{tIdent, tDelim, tIdent, tString, tURL, tFunction, tString, tRParen, tHash, tDimension, tPercentage, tAtKeyword, tCDO, tCDC, tNumber}
	if fmt.Sprint(kinds) != fmt.Sprint(want) {
		t.Fatalf("kinds %v want %v", kinds, want)
	}
	if vals[2] != "b-c1x" || vals[3] != `s"tr` || vals[4] != "x.png" || vals[8] != "f00" || vals[9] != "px" {
		t.Fatalf("vals %q", vals)
	}
	// bad url recovers to the next token
	toks = tokenize(`url(a b) c`)
	if toks[0].k != tBadURL || toks[len(toks)-1].s != "c" {
		t.Fatalf("bad url: %+v", toks)
	}
	// unterminated string is a bad string up to the newline
	toks = tokenize("\"abc\ndef")
	if toks[0].k != tBadString {
		t.Fatalf("bad string: %+v", toks)
	}
}

func TestParseRecovery(t *testing.T) {
	e := eng(`
		.a { color: red; ; width: 10px }
		.a { color: ; height: 5px }
		}}} .b { color: blue }
		.c { color: green; background: url(a b); margin: 1px }
		.d { color: "x }
		.e { color: red }
	`)
	c := e.Cascade(n("div.a"), PseudoNone, nil, testEnv)
	if val(t, c, "color") != "red" || val(t, c, "width") != "10px" || val(t, c, "height") != "5px" {
		t.Fatalf("a: %+v", c.Decls)
	}
	c = e.Cascade(n("div.c"), PseudoNone, nil, testEnv)
	if val(t, c, "color") != "green" || val(t, c, "margin-top") != "1px" {
		t.Fatalf("c: %+v", c.Decls)
	}
}

// --- cascade order ---

func TestCascadeOrder(t *testing.T) {
	el := n("div#x.a.b.c")
	cases := []struct {
		css, inline, want string
	}{
		{`.a { color: red } .a { color: blue }`, "", "blue"},
		{`#x { color: red } .a.b.c { color: blue }`, "", "red"},
		{`.a.b { color: red } .a { color: blue }`, "", "red"},
		{`.a { color: red !important } .a.b { color: blue }`, "", "red"},
		{`.a { color: red !important } .a { color: blue !important }`, "", "blue"},
		{`#x { color: red }`, "color: blue", "blue"},
		{`.a { color: red !important }`, "color: blue", "red"},
		{`.a { color: red !important }`, "color: blue !important", "blue"},
		{`div { color: red } * { color: blue }`, "", "red"},
		{`:where(#x) { color: red } div { color: blue }`, "", "blue"},
		{`:is(#x, .a) { color: red } .a.b { color: blue }`, "", "red"},
		{`.a { color: red; color: blue }`, "", "blue"},
		{`.a { color: red; color: nonsense }`, "", "red"},
	}
	for _, tc := range cases {
		e := eng(tc.css)
		c := e.Cascade(el, PseudoNone, ParseDecls(tc.inline), testEnv)
		if got := val(t, c, "color"); got != tc.want {
			t.Errorf("%s | %s: got %q want %q", tc.css, tc.inline, got, tc.want)
		}
	}
}

func TestSheetsOrderAndSet(t *testing.T) {
	el := n("div.a")
	e := NewEngine()
	if !e.Empty() {
		t.Fatal("new engine not empty")
	}
	v0 := e.Version()
	e.Set("one", ".a { color: red }")
	e.Set("two", ".a { color: blue }")
	if e.Empty() || e.Version() <= v0 {
		t.Fatal("Set didn't bump")
	}
	if got := val(t, e.Cascade(el, 0, nil, testEnv), "color"); got != "blue" {
		t.Fatalf("got %q", got)
	}
	// replacing "one" keeps its earlier position: "two" still wins
	e.Set("one", ".a { color: green }")
	if got := val(t, e.Cascade(el, 0, nil, testEnv), "color"); got != "blue" {
		t.Fatalf("replace moved the sheet: %q", got)
	}
	e.Remove("two")
	if got := val(t, e.Cascade(el, 0, nil, testEnv), "color"); got != "green" {
		t.Fatalf("after remove: %q", got)
	}
	e.Remove("one")
	if !e.Empty() {
		t.Fatal("not empty after removing all")
	}
	e.Set("c", "/* only a comment */ @font-face { font-family: x }")
	if !e.Empty() {
		t.Fatal("rule-less sheet should leave the engine empty")
	}
	var nilEng *Engine
	if !nilEng.Empty() {
		t.Fatal("nil engine not empty")
	}
}

func TestLayers(t *testing.T) {
	el := n("div.a.b")
	cases := []struct{ css, want string }{
		{`@layer base, theme; @layer theme { .a { color: blue } } @layer base { .a.b { color: green } }`, "blue"},
		{`@layer base, theme; @layer theme { .a { color: blue } } @layer base { .a.b { color: green } } .a { color: red }`, "red"},
		{`@layer base { .a { color: green !important } } @layer theme { .a { color: blue !important } } .a { color: red !important }`, "green"},
		{`@layer a { @layer b { .a { color: blue } } .a { color: red } }`, "red"},
		{`@layer { .a.b { color: blue } } .a { color: red }`, "red"},
		{`@layer x.y { .a { color: blue } } @layer x { .a { color: green } }`, "green"},
	}
	for _, tc := range cases {
		if got := val(t, eng(tc.css).Cascade(el, 0, nil, testEnv), "color"); got != tc.want {
			t.Errorf("%s: got %q want %q", tc.css, got, tc.want)
		}
	}
	// layer order spans sheets in insertion order
	e := eng(`@layer late, early;`, `@layer early { .a { color: green } }`, `@layer late { .a { color: blue } }`)
	if got := val(t, e.Cascade(el, 0, nil, testEnv), "color"); got != "green" {
		t.Errorf("cross-sheet layers: %q", got)
	}
}

// --- selectors ---

func TestSelectors(t *testing.T) {
	// <body.root>
	//   <div#main.app data-qa="main-view" lang="en-US">
	//     <span.first.x>   <p.second title="Hello World">   <span.third.x.y>   <button.btn disabled>
	//   </div>
	//   <ul.list><li.k/><li/><li.k/><li/><li.k/></ul>
	// </body>
	first := n("span.first.x")
	second := n("p.second").with("title", "Hello World")
	third := n("span.third.x.y")
	btn := n("button.btn").with("disabled", "")
	main := n("div#main.app", first, second, third, btn).with("data-qa", "main-view").with("lang", "en-US")
	lis := []*fakeEl{n("li.k"), n("li"), n("li.k"), n("li"), n("li.k").with("data-x", "a-b")}
	list := n("ul.list", lis...)
	root := n("body.root", main, list)
	_ = root

	cases := []struct {
		sel  string
		el   *fakeEl
		want bool
	}{
		{"span", first, true},
		{"*", second, true},
		{"#main", main, true},
		{".x.y", third, true},
		{".x.y", first, false},
		{"[data-qa]", main, true},
		{`[data-qa="main-view"]`, main, true},
		{`[data-qa=main]`, main, false},
		{`[data-qa^="main"]`, main, true},
		{`[data-qa$="view"]`, main, true},
		{`[data-qa*="n-v"]`, main, true},
		{`[title~="World"]`, second, true},
		{`[title~="Wor"]`, second, false},
		{`[lang|="en"]`, main, true},
		{`[data-qa="MAIN-VIEW" i]`, main, true},
		{`[data-qa="MAIN-VIEW" s]`, main, false},
		{".root .x", first, true},
		{".root > .x", first, false},
		{"#main > .x", first, true},
		{".first + p", second, true},
		{".first + span", third, false},
		{".first ~ span", third, true},
		{".first ~ .first", first, false},
		{":not(.first)", second, true},
		{":not(.first, .second)", second, false},
		{":is(.first, .second)", second, true},
		{":where(.app) span", first, true},
		{":first-child", first, true},
		{":last-child", btn, true},
		{":only-child", main, false},
		{"li:nth-child(2n+1)", lis[2], true},
		{"li:nth-child(odd)", lis[1], false},
		{"li:nth-child(even)", lis[1], true},
		{"li:nth-child(-n+2)", lis[1], true},
		{"li:nth-child(-n+2)", lis[2], false},
		{"li:nth-child(3)", lis[2], true},
		{"li:nth-last-child(1)", lis[4], true},
		{"li:nth-child(2 of .k)", lis[2], true},
		{"li:nth-child(2 of .k)", lis[4], false},
		{"li:nth-last-child(1 of .k)", lis[4], true},
		{"span:first-of-type", first, true},
		{"span:last-of-type", third, true},
		{"p:only-of-type", second, true},
		{"span:nth-of-type(2)", third, true},
		{"span:nth-last-of-type(2)", first, true},
		{":root", root, true},
		{":root", main, false},
		{".app:has(> .third)", main, true},
		{".app:has(.nope)", main, false},
		{"body:has(.third)", root, true},
		{"body:has(> .third)", root, false},
		{".first:has(+ .second)", first, true},
		{".first:has(~ .btn)", first, true},
		{"button:disabled", btn, true},
		{"button:enabled", btn, false},
		{":lang(en)", first, true},
		{"li:empty", lis[0], true},
		{"[data-x|=a]", lis[4], true},
		{".app:visited", main, false},
		{"div.app#main[data-qa]:not(span)", main, true},
		{"body > div > span:nth-child(3)", third, true},
	}
	for _, tc := range cases {
		e := eng(tc.sel + " { color: red }")
		got := has(e.Cascade(tc.el, 0, nil, testEnv), "color")
		if got != tc.want {
			t.Errorf("%s on %s.%v: got %v", tc.sel, tc.el.tag, tc.el.classes, got)
		}
	}
}

func TestDynamicPseudo(t *testing.T) {
	el := n("button.b")
	sheet := `.b:hover { color: red } .b:focus-visible { width: 1px } .b:active { height: 1px }
		.b:checked { opacity: .5 } .b:focus-within { order: 2 } .b:placeholder-shown { z-index: 3 }`
	e := eng(sheet)
	if c := e.Cascade(el, 0, nil, testEnv); len(c.Decls) != 0 {
		t.Fatalf("matched without state: %+v", c.Decls)
	}
	el.st = StateHover | StateFocusVisible | StateActive | StateChecked | StateFocusWithin | StatePlaceholderShown
	c := e.Cascade(el, 0, nil, testEnv)
	for _, p := range []string{"color", "width", "height", "opacity", "order", "z-index"} {
		if !c.Has(p) {
			t.Errorf("%s missing", p)
		}
	}
}

func TestStructuralUnknownIndex(t *testing.T) {
	el := n("div.a")
	el.noIndex = true
	p := n("div.p", el)
	_ = p
	e := eng(`.a:first-child { color: red } .a:nth-child(1) { width: 1px } .a:only-child { height: 1px }`)
	if c := e.Cascade(el, 0, nil, testEnv); len(c.Decls) != 0 {
		t.Fatalf("structural pseudo matched an unknown index: %+v", c.Decls)
	}
}

func TestHasNeverOnNatives(t *testing.T) {
	kid := n("span.k")
	p := n("div.p", kid)
	e := eng(`.p:has(.k) { color: red }`)
	if !has(e.Cascade(p, 0, nil, testEnv), "color") {
		t.Fatal(":has on a host node")
	}
	if has(e.Cascade(nativeLike{p}, 0, nil, testEnv), "color") {
		t.Fatal(":has matched a native")
	}
}

func TestInvalidSelectorDropsRule(t *testing.T) {
	el := n("div.b")
	for _, css := range []string{
		`.a:unknown-thing, .b { color: red }`,
		`.a::nope, .b { color: red }`,
		`.b:nth-child(foo) { color: red }`,
		`.b > { color: red }`,
		`#123, .b { color: red }`,
	} {
		if has(eng(css).Cascade(el, 0, nil, testEnv), "color") {
			t.Errorf("%s: rule should be dropped", css)
		}
	}
	// forgiving :is() keeps the valid argument
	if !has(eng(`:is(.a:unknown, .b) { color: red }`).Cascade(el, 0, nil, testEnv), "color") {
		t.Error(":is should be forgiving")
	}
	// never-matching but valid pseudo-elements keep the rest of the list
	if !has(eng(`.b::selection, .b { color: red }`).Cascade(el, 0, nil, testEnv), "color") {
		t.Error("::selection should not drop the rule")
	}
	if !has(eng(`.b::-webkit-scrollbar, .b { color: red }`).Cascade(el, 0, nil, testEnv), "color") {
		t.Error("::-webkit-scrollbar should not drop the rule")
	}
}

func TestSpecificity(t *testing.T) {
	spec := func(sel string) uint32 {
		s, ok := parseSelectorList(trimWS(tokenize(sel)), nil, false, false, false)
		if !ok {
			t.Fatalf("parse %s", sel)
		}
		return s[0].spec
	}
	cases := map[string]uint32{
		"*":                        0,
		"div":                      1,
		".a":                       1 << 8,
		"#a":                       1 << 16,
		"div.a#b":                  1<<16 | 1<<8 | 1,
		"a:hover::before":          1<<8 | 2,
		":is(#a, .b)":              1 << 16,
		":where(#a, .b)":           0,
		":not(.a, #b) span":        1<<16 | 1,
		":has(> .a .b)":            2 << 8,
		"li:nth-child(2n of .k)":   2<<8 | 1,
		"[data-qa] .x:first-child": 3 << 8,
	}
	for sel, want := range cases {
		if got := spec(sel); got != want {
			t.Errorf("%s: spec %06x want %06x", sel, got, want)
		}
	}
}

func TestPseudoElements(t *testing.T) {
	el := n("div.a").with("data-label", "Hi \"there\"")
	e := eng(`.a::before { content: "\2014 x"; color: red } .a:after { content: attr(data-label) "!" }
		.a::placeholder { color: gray } .a::after { content: attr(missing, "fb") }`)
	b := e.Cascade(el, PseudoBefore, nil, testEnv)
	parent := styleOf(e, el, testEnv)
	sb := Compute(&b, &parent, testEnv)
	if !sb.HasContent || sb.Content != "—x" || sb.Color != rgb(255, 0, 0) {
		t.Fatalf("before: %q %v %v", sb.Content, sb.HasContent, sb.Color)
	}
	a := e.Cascade(el, PseudoAfter, nil, testEnv)
	sa := Compute(&a, &parent, testEnv)
	if sa.Content != "fb" {
		t.Fatalf("after (last rule wins): %q", sa.Content)
	}
	e2 := eng(`.a:after { content: attr(data-label) "!" }`)
	a = e2.Cascade(el, PseudoAfter, nil, testEnv)
	sa = Compute(&a, &parent, testEnv)
	if sa.Content != `Hi "there"!` {
		t.Fatalf("attr(): %q", sa.Content)
	}
	if !has(e.Cascade(el, PseudoPlaceholder, nil, testEnv), "color") {
		t.Fatal("::placeholder")
	}
	if has(e.Cascade(el, PseudoNone, nil, testEnv), "content") {
		t.Fatal("pseudo-element rule leaked onto the element")
	}
	none := eng(`.a::before { content: none }`).Cascade(el, PseudoBefore, nil, testEnv)
	if s := Compute(&none, &parent, testEnv); s.HasContent {
		t.Fatal("content: none")
	}
	q := eng(`.a::before { content: open-quote "q" close-quote }`).Cascade(el, PseudoBefore, nil, testEnv)
	if s := Compute(&q, &parent, testEnv); s.Content != "“q”" {
		t.Fatalf("quotes: %q", s.Content)
	}
}

// --- nesting and at-rules ---

func TestNesting(t *testing.T) {
	c := n("span.c")
	d := n("span.d")
	ee := n("span.e")
	p := n("div.p", c, d)
	_ = n("section", p, ee)
	css := `.p {
		color: red;
		&:hover { color: blue }
		.c { color: green }
		> .d { color: pink }
		& + .e { color: orange }
		.q & { width: 1px }
		@media (min-width: 100px) { height: 7px; .c { opacity: .5 } }
		div:is(&) { order: 3 }
		margin-top: 2px;
	}`
	e := eng(css)
	cp := e.Cascade(p, 0, nil, testEnv)
	if val(t, cp, "color") != "red" || val(t, cp, "height") != "7px" || val(t, cp, "order") != "3" || val(t, cp, "margin-top") != "2px" {
		t.Fatalf("p: %+v", cp.Decls)
	}
	p.st = StateHover
	if got := val(t, e.Cascade(p, 0, nil, testEnv), "color"); got != "blue" {
		t.Fatalf("&:hover: %q", got)
	}
	cc := e.Cascade(c, 0, nil, testEnv)
	if val(t, cc, "color") != "green" || val(t, cc, "opacity") != ".5" {
		t.Fatalf(".c: %+v", cc.Decls)
	}
	if got := val(t, e.Cascade(d, 0, nil, testEnv), "color"); got != "pink" {
		t.Fatalf("> .d: %q", got)
	}
	if got := val(t, e.Cascade(ee, 0, nil, testEnv), "color"); got != "orange" {
		t.Fatalf("& + .e: %q", got)
	}
	// nested specificity is :is(parent): .p .c beats a later .c
	e2 := eng(`.p { .c { color: green } } .c { color: red }`)
	if got := val(t, e2.Cascade(c, 0, nil, testEnv), "color"); got != "green" {
		t.Fatalf("nested specificity: %q", got)
	}
	// nested declarations after a nested rule come later in order
	e3 := eng(`.p { color: red; .x & {} } .p { color: blue } `)
	if got := val(t, e3.Cascade(p, 0, nil, testEnv), "color"); got != "blue" {
		t.Fatalf("order: %q", got)
	}
}

func TestMedia(t *testing.T) {
	el := n("div.a")
	css := `
		@media (prefers-color-scheme: dark) { .a { color: white } }
		@media (prefers-color-scheme: light) { .a { color: black } }
		@media (min-width: 600px) { .a { width: 1px } }
		@media (max-width: 599px) { .a { width: 2px } }
		@media (width >= 1000px) and (height < 900px) { .a { height: 3px } }
		@media (400px <= width <= 700px) { .a { order: 1 } }
		@media print { .a { opacity: 0 } }
		@media not print { .a { z-index: 5 } }
		@media only screen and (min-width: 1px), print { .a { margin-top: 1px } }
		@media not all and (monochrome) { .a { margin-left: 1px } }
		@media (max-width: 10px) or (orientation: landscape) { .a { padding-top: 1px } }
		@media (hover: hover) and (pointer: fine) { .a { padding-left: 1px } }
		@media (min-width: 40em) { .a { padding-right: 1px } }
		@media (prefers-reduced-motion: reduce) { .a { padding-bottom: 1px } }
		@media (unknown-feature: 1) { .a { margin-bottom: 1px } }
		@media screen and (-webkit-min-device-pixel-ratio: 0) { .a { margin-right: 1px } }
	`
	e := eng(css)
	light := e.Cascade(el, 0, nil, testEnv)
	dark := testEnv
	dark.Dark, dark.Width = true, 500
	dk := e.Cascade(el, 0, nil, dark)
	if val(t, light, "color") != "black" || val(t, dk, "color") != "white" {
		t.Fatalf("color scheme: %q %q", val(t, light, "color"), val(t, dk, "color"))
	}
	if val(t, light, "width") != "1px" || val(t, dk, "width") != "2px" {
		t.Fatal("min/max-width")
	}
	if val(t, light, "height") != "3px" || dk.Has("height") {
		t.Fatal("range and")
	}
	if light.Has("order") || val(t, dk, "order") != "1" {
		t.Fatal("double range")
	}
	if light.Has("opacity") || val(t, light, "z-index") != "5" {
		t.Fatal("print / not print")
	}
	for _, p := range []string{"margin-top", "margin-left", "padding-top", "padding-left", "padding-right", "margin-right"} {
		if !light.Has(p) {
			t.Errorf("%s should match", p)
		}
	}
	if light.Has("padding-bottom") || light.Has("margin-bottom") {
		t.Fatal("reduced motion / unknown feature")
	}
	rm := testEnv
	rm.ReduceMotion = true
	if !has(e.Cascade(el, 0, nil, rm), "padding-bottom") {
		t.Fatal("reduced motion")
	}
}

func TestSupportsContainerScope(t *testing.T) {
	el := n("div.a")
	_ = n("div.s", el)
	e := eng(`
		@supports (display: grid) { .a { color: red } }
		@supports (display: nonsense) { .a { width: 1px } }
		@supports not (display: nonsense) { .a { height: 1px } }
		@supports (display: flex) and (selector(:has(a))) { .a { order: 1 } }
		@supports (foo: bar) or (color: red) { .a { opacity: .5 } }
		@container sidebar (min-width: 400px) { .a { z-index: 2 } }
		@scope (.s) { .a { margin-top: 1px } }
		@import url(x.css);
		@font-face { font-family: X; src: url(x) }
		@keyframes spin { from { color: blue } to { color: green } }
		@property --x { syntax: "<length>"; inherits: false; initial-value: 0px }
	`)
	c := e.Cascade(el, 0, nil, testEnv)
	if val(t, c, "color") != "red" || c.Has("width") || !c.Has("height") || !c.Has("order") || !c.Has("opacity") || !c.Has("z-index") || !c.Has("margin-top") {
		t.Fatalf("%+v", c.Decls)
	}
}

// --- var() ---

func TestVars(t *testing.T) {
	root := n("div.root")
	mid := n("div.mid")
	leaf := n("span.leaf")
	root.kids, mid.parent = []*fakeEl{mid}, root
	mid.kids, leaf.parent = []*fakeEl{leaf}, mid
	css := `
		.root { --c: rgb(1, 2, 3); --pad: 4px; --a: var(--b); --b: var(--a); --n: 10px }
		.mid { --pad: 6px; --deep: var(--missing, var(--also-missing, 12px)); --self: var(--self, 1px) }
		.leaf {
			color: var(--c);
			padding: var(--pad) 1px;
			margin-left: var(--deep);
			width: var(--a, 50px);
			height: var(--nope);
			min-width: calc(var(--n) * 2);
			border-color: var(--nope, red);
			background-color: rgba(var(--sk_foreground_max_solid, 97, 96, 97), 1);
			outline-color: rgba(var(--sk_highlight), 0.5);
			font-size: var(--sz, 20px);
			max-width: var(--self);
		}`
	e := eng(css)
	env := testEnv
	env.RootVars = SlackVars(false)
	s := styleOf(e, leaf, env)
	if s.Color != rgb(1, 2, 3) {
		t.Errorf("color %v", s.Color)
	}
	if s.Padding[0].Px != 6 || s.Padding[1].Px != 1 || s.Padding[2].Px != 6 {
		t.Errorf("padding (shorthand with var) %+v", s.Padding)
	}
	if s.Margin[3].Px != 12 {
		t.Errorf("nested fallback %+v", s.Margin[3])
	}
	if s.Width.Px != 50 {
		t.Errorf("cycle makes --a invalid, so the fallback applies: width %+v", s.Width)
	}
	if !s.Height.IsAuto() {
		t.Errorf("missing var without fallback: height %+v", s.Height)
	}
	if s.MinWidth.Px != 20 {
		t.Errorf("calc(var) %+v", s.MinWidth)
	}
	if s.BorderColor[0] != rgb(255, 0, 0) {
		t.Errorf("fallback color %v", s.BorderColor[0])
	}
	if s.Background.Color != rgb(97, 96, 97) {
		t.Errorf("slack idiom light %v", s.Background.Color)
	}
	if s.Outline.Color != (color.NRGBA{18, 100, 163, 128}) {
		t.Errorf("slack highlight %v", s.Outline.Color)
	}
	if s.FontSize != 20 {
		t.Errorf("font-size fallback %v", s.FontSize)
	}
	if s.MaxWidth.Kind != LenNone {
		t.Errorf("self cycle %+v", s.MaxWidth)
	}
	env.RootVars = SlackVars(true)
	if s := styleOf(e, leaf, env); s.Background.Color != rgb(171, 171, 173) {
		t.Errorf("slack idiom dark %v", s.Background.Color)
	}
	// Vars inherit, own declarations override
	if m := styleOf(e, mid, env); m.Vars["--pad"] != "6px" || m.Vars["--c"] != "rgb(1, 2, 3)" {
		t.Errorf("vars %v", m.Vars)
	}
}

func TestSlackVars(t *testing.T) {
	l, d := SlackVars(false), SlackVars(true)
	if len(l) < 300 || len(d) < 300 {
		t.Fatalf("few tokens: %d %d", len(l), len(d))
	}
	for k, v := range l {
		if strings.Contains(v, "var(") {
			t.Fatalf("%s unresolved: %s", k, v)
		}
	}
	if l["--sk_primary_background"] != "255, 255, 255" || d["--sk_primary_background"] == l["--sk_primary_background"] {
		t.Fatalf("primary background %q %q", l["--sk_primary_background"], d["--sk_primary_background"])
	}
}

// --- shorthands ---

func TestShorthands(t *testing.T) {
	cases := []struct {
		decl string
		want map[string]string
	}{
		{"margin: 1px 2px", map[string]string{"margin-top": "1px", "margin-right": "2px", "margin-bottom": "1px", "margin-left": "2px"}},
		{"padding: 1px 2px 3px", map[string]string{"padding-top": "1px", "padding-right": "2px", "padding-bottom": "3px", "padding-left": "2px"}},
		{"margin-inline: 3px 4px", map[string]string{"margin-left": "3px", "margin-right": "4px"}},
		{"padding-block-start: 5px", map[string]string{"padding-top": "5px"}},
		{"inset: 0 auto", map[string]string{"top": "0", "right": "auto", "bottom": "0", "left": "auto"}},
		{"inset-inline: 1px", map[string]string{"left": "1px", "right": "1px"}},
		{"border: 2px solid red", map[string]string{"border-top-width": "2px", "border-left-style": "solid", "border-bottom-color": "red"}},
		{"border-top: dashed", map[string]string{"border-top-width": "medium", "border-top-style": "dashed", "border-top-color": "currentcolor"}},
		{"border-inline-start: 1px solid blue", map[string]string{"border-left-width": "1px", "border-left-color": "blue"}},
		{"border-radius: 4px 8px / 2px", map[string]string{"border-top-left-radius": "4px", "border-top-right-radius": "8px", "border-bottom-right-radius": "4px"}},
		{"flex: 1", map[string]string{"flex-grow": "1", "flex-shrink": "1", "flex-basis": "0%"}},
		{"flex: none", map[string]string{"flex-grow": "0", "flex-shrink": "0", "flex-basis": "auto"}},
		{"flex: auto", map[string]string{"flex-grow": "1", "flex-shrink": "1", "flex-basis": "auto"}},
		{"flex: 1 1 0%", map[string]string{"flex-grow": "1", "flex-shrink": "1", "flex-basis": "0%"}},
		{"flex: 2 3", map[string]string{"flex-grow": "2", "flex-shrink": "3", "flex-basis": "0%"}},
		{"flex: 10px", map[string]string{"flex-grow": "1", "flex-shrink": "1", "flex-basis": "10px"}},
		{"flex: 0 0 auto", map[string]string{"flex-grow": "0", "flex-shrink": "0", "flex-basis": "auto"}},
		{"flex-flow: column wrap", map[string]string{"flex-direction": "column", "flex-wrap": "wrap"}},
		{"gap: 4px 8px", map[string]string{"row-gap": "4px", "column-gap": "8px"}},
		{"grid-gap: 3px", map[string]string{"row-gap": "3px", "column-gap": "3px"}},
		{"place-items: center", map[string]string{"align-items": "center", "justify-items": "center"}},
		{"place-content: space-between center", map[string]string{"align-content": "space-between", "justify-content": "center"}},
		{"overflow: hidden auto", map[string]string{"overflow-x": "hidden", "overflow-y": "auto"}},
		{"text-decoration: underline dotted red", map[string]string{"text-decoration-line": "underline", "text-decoration-color": "red"}},
		{"font: italic bold 12px/1.5 Lato, sans-serif", map[string]string{"font-style": "italic", "font-weight": "bold", "font-size": "12px", "line-height": "1.5", "font-family": "Lato, sans-serif"}},
		{"background: url(a.png) no-repeat center / cover, linear-gradient(red, blue) #fff", map[string]string{"background-color": "#fff", "background-repeat": "no-repeat, repeat", "background-size": "cover, auto"}},
		{"-webkit-box-shadow: 0 0 1px red", map[string]string{"box-shadow": "0 0 1px red"}},
		{"word-wrap: break-word", map[string]string{"overflow-wrap": "break-word"}},
		{"margin: inherit", map[string]string{"margin-top": "inherit", "margin-left": "inherit"}},
		{"grid-template: auto / repeat(3, 1fr)", map[string]string{"grid-template-columns": "repeat(3, 1fr)"}},
	}
	for _, tc := range cases {
		ds := ParseDecls(tc.decl)
		c := Cascaded{Decls: ds}
		for p, w := range tc.want {
			if got, _ := c.Get(p); got != w {
				t.Errorf("%s: %s = %q want %q (all: %+v)", tc.decl, p, got, w, ds)
			}
		}
	}
	for _, bad := range []string{"margin: 1px 2px 3px 4px 5px", "border: 1px 2px", "flex: 1 2 3 4", "font: bold", "padding: -1px", "colr: red", "color: 12px", "width: red"} {
		if ds := ParseDecls(bad); len(ds) != 0 {
			t.Errorf("%s should be invalid: %+v", bad, ds)
		}
	}
	// pending var() shorthands expand in Compute
	e := eng(`.a { --m: 3px 4px; margin: var(--m); border: var(--b, 2px solid rgb(0, 0, 255)) }`)
	el := n("div.a")
	s := styleOf(e, el, testEnv)
	if s.Margin[0].Px != 3 || s.Margin[1].Px != 4 || s.Margin[2].Px != 3 || s.Margin[3].Px != 4 {
		t.Errorf("pending margin %+v", s.Margin)
	}
	if s.BorderWidth[2] != 2 || s.BorderStyle[2] != BorderSolid || s.BorderColor[2] != rgb(0, 0, 255) {
		t.Errorf("pending border %v %v %v", s.BorderWidth, s.BorderStyle, s.BorderColor)
	}
	// "all"
	ds := ParseDecls("all: unset")
	if len(ds) < 50 {
		t.Errorf("all: %d longhands", len(ds))
	}
}

// --- colors ---

func TestColors(t *testing.T) {
	cases := map[string]color.NRGBA{
		"red":                               rgb(255, 0, 0),
		"RebeccaPurple":                     rgb(0x66, 0x33, 0x99),
		"lightgoldenrodyellow":              rgb(0xfa, 0xfa, 0xd2),
		"#abc":                              rgb(0xaa, 0xbb, 0xcc),
		"#abcd":                             {0xaa, 0xbb, 0xcc, 0xdd},
		"#112233":                           rgb(0x11, 0x22, 0x33),
		"#11223380":                         {0x11, 0x22, 0x33, 0x80},
		"rgb(1, 2, 3)":                      rgb(1, 2, 3),
		"rgba(1,2,3,0.5)":                   {1, 2, 3, 128},
		"rgb(1 2 3 / 50%)":                  {1, 2, 3, 128},
		"rgb(100%, 0%, 50%)":                rgb(255, 0, 128),
		"rgba(97, 96, 97,1)":                rgb(97, 96, 97),
		"rgb(255 0 0 / .25)":                {255, 0, 0, 64},
		"hsl(120, 100%, 50%)":               rgb(0, 255, 0),
		"hsla(240 100% 50% / 0.5)":          {0, 0, 255, 128},
		"hsl(0.5turn 100% 25%)":             rgb(0, 128, 128),
		"hwb(0 0% 0%)":                      rgb(255, 0, 0),
		"hwb(0 50% 50%)":                    rgb(128, 128, 128),
		"transparent":                       {},
		"color-mix(in srgb, red 50%, blue)": rgb(128, 0, 128),
		"color-mix(in srgb, #1264a3 30%, transparent)": {0x12, 0x64, 0xa3, 77},
		"oklch(62.8% 0.2577 29.23)":                    rgb(255, 0, 0),
		"oklab(1 0 0)":                                 rgb(255, 255, 255),
		"color(srgb 1 0 0)":                            rgb(255, 0, 0),
		"light-dark(red, blue)":                        rgb(255, 0, 0),
		"rgb(calc(100 + 155), 0, 0)":                   rgb(255, 0, 0),
	}
	for css, want := range cases {
		got := colorOf(t, css)
		d := func(a, b uint8) bool { return int(a)-int(b) > 1 || int(b)-int(a) > 1 }
		if d(got.R, want.R) || d(got.G, want.G) || d(got.B, want.B) || d(got.A, want.A) {
			t.Errorf("%s: got %v want %v", css, got, want)
		}
	}
	// currentColor follows color
	e := eng(`.p { color: rgb(0, 0, 255) } .a { border: 1px solid; background-color: currentColor; box-shadow: 0 0 2px }`)
	el := n("div.a")
	_ = n("div.p", el)
	s := styleOf(e, el, testEnv)
	if s.BorderColor[0] != rgb(0, 0, 255) || s.Background.Color != rgb(0, 0, 255) || s.BoxShadow[0].Color != rgb(0, 0, 255) {
		t.Errorf("currentColor: %v %v %v", s.BorderColor[0], s.Background.Color, s.BoxShadow)
	}
	for _, bad := range []string{"#ab", "#abcde", "rgb(1, 2)", "notacolor", "hsl(1 2)"} {
		if has(eng(".x { color: "+bad+" }").Cascade(n("div.x"), 0, nil, testEnv), "color") {
			t.Errorf("%s accepted", bad)
		}
	}
	if len(namedColors) != 148 {
		t.Errorf("%d named colors", len(namedColors))
	}
}

// --- units, calc ---

func TestUnits(t *testing.T) {
	leaf := n("span.c")
	p := n("div.p", leaf)
	_ = p
	env := testEnv
	env.RootFont = 10
	cases := []struct {
		decl string
		get  func(*Style) float32
		want float32
	}{
		{"width: 10px", func(s *Style) float32 { return s.Width.Px }, 10},
		{"width: 2em", func(s *Style) float32 { return s.Width.Px }, 40},
		{"width: 2rem", func(s *Style) float32 { return s.Width.Px }, 20},
		{"width: 50%", func(s *Style) float32 { return s.Width.Pct }, 50},
		{"width: 10vw", func(s *Style) float32 { return s.Width.Vw }, 10},
		{"width: 10vh", func(s *Style) float32 { return s.Width.Vh }, 10},
		{"width: 10vmin", func(s *Style) float32 { return s.Width.Vh }, 10},
		{"width: 10vmax", func(s *Style) float32 { return s.Width.Vw }, 10},
		{"width: 12pt", func(s *Style) float32 { return s.Width.Px }, 16},
		{"width: 1pc", func(s *Style) float32 { return s.Width.Px }, 16},
		{"width: 1in", func(s *Style) float32 { return s.Width.Px }, 96},
		{"width: 2.54cm", func(s *Style) float32 { return s.Width.Px }, 96},
		{"width: 25.4mm", func(s *Style) float32 { return s.Width.Px }, 96},
		{"width: 2ch", func(s *Style) float32 { return s.Width.Px }, 20},
		{"width: 0", func(s *Style) float32 { return s.Width.Px }, 0},
		{"font-size: 2em", func(s *Style) float32 { return s.FontSize }, 40},
		{"font-size: 150%", func(s *Style) float32 { return s.FontSize }, 30},
		{"font-size: 2em; padding-left: 1em", func(s *Style) float32 { return s.Padding[3].Px }, 40},
		{"font-size: larger", func(s *Style) float32 { return s.FontSize }, 24},
		{"font-size: small", func(s *Style) float32 { return s.FontSize }, 13},
		{"line-height: 2em", func(s *Style) float32 { return s.LineHeight.Px }, 40},
		{"line-height: 150%", func(s *Style) float32 { return s.LineHeight.Px }, 30},
		{"line-height: 1.5", func(s *Style) float32 { return s.LineHeight.Pct }, 150},
		{"letter-spacing: .1em", func(s *Style) float32 { return s.LetterSpacing }, 2},
		{"width: calc(100% - 20px)", func(s *Style) float32 { return s.Width.Pct*1000 + s.Width.Px }, 100000 - 20},
		{"width: calc((10px + 2em) * 2)", func(s *Style) float32 { return s.Width.Px }, 100},
		{"width: calc(100px / 4 - -5px)", func(s *Style) float32 { return s.Width.Px }, 30},
		{"width: min(10px, 2em)", func(s *Style) float32 { return s.Width.Px }, 10},
		{"width: max(10px, 2em)", func(s *Style) float32 { return s.Width.Px }, 40},
		{"width: clamp(10px, 50px, 30px)", func(s *Style) float32 { return s.Width.Px }, 30},
		{"width: min(100%, 300px)", func(s *Style) float32 { return s.Width.Resolve(1000, 0, 0, 0) }, 300},
		{"width: min(100%, 300px)", func(s *Style) float32 { return s.Width.Resolve(200, 0, 0, 0) }, 200},
		{"width: clamp(100px, 50%, 400px)", func(s *Style) float32 { return s.Width.Resolve(1000, 0, 0, 0) }, 400},
		{"width: calc(10px + min(5%, 30px))", func(s *Style) float32 { return s.Width.Resolve(100, 0, 0, 0) }, 15},
		{"width: calc(50vw + 10px)", func(s *Style) float32 { return s.Width.Resolve(0, 1000, 0, 0) }, 510},
	}
	for _, tc := range cases {
		e := eng(".p { font-size: 20px } .c { " + tc.decl + " }")
		s := styleOf(e, leaf, env)
		if got := tc.get(&s); !near(got, tc.want) {
			t.Errorf("%s: got %v want %v", tc.decl, got, tc.want)
		}
	}
	for _, bad := range []string{"width: 10", "width: 10qq", "width: calc(10px + 5)", "width: calc(100% -20px)", "width: -5px", "font-size: -1px"} {
		if has(eng(".c { "+bad+" }").Cascade(leaf, 0, nil, env), strings.Split(bad, ":")[0]) {
			t.Errorf("%s accepted", bad)
		}
	}
}

// --- computed values ---

func TestCompute(t *testing.T) {
	leaf := n("span.c").with("title", "t")
	p := n("div.p", leaf)
	_ = p
	css := `.p { color: rgb(10, 20, 30); font-weight: 600; line-height: 1.5; font-size: 10px; cursor: pointer;
			pointer-events: none; text-align: center; white-space: nowrap; visibility: hidden; display: flex;
			letter-spacing: 1px; text-transform: uppercase }
		.c {
			display: -webkit-box; -webkit-box-orient: vertical; -webkit-line-clamp: 3;
			font-size: 20px; font-weight: bolder; font-style: italic; font-family: "Slack-Lato", Helvetica Neue, sans-serif;
			position: absolute; top: 1px; left: auto; z-index: 7;
			min-width: min-content; max-height: none; box-sizing: border-box; aspect-ratio: 16 / 9;
			border-width: 1px 2px; border-style: solid none; border-color: red;
			border-top-left-radius: 3px 5px; outline: 2px auto blue; outline-offset: 1px;
			flex-wrap: wrap; justify-content: space-between; align-items: center; align-self: flex-end;
			align-content: stretch; row-gap: 1px; column-gap: 2px; flex: 2 0 10px; order: -1;
			overflow: hidden scroll; opacity: 50%;
			transform: translateX(10px) rotate(90deg) scale(2, 3) skew(10deg) matrix(1, 0, 0, 1, 5, 6);
			translate: 1px 2px; rotate: 45deg; scale: 2;
			transform-origin: top left;
			filter: blur(4px) grayscale(1) brightness(0) drop-shadow(1px 2px 3px red);
			backdrop-filter: blur(8px);
			box-shadow: inset 0 1px 2px 3px rgba(0, 0, 0, .5), 4px 4px green;
			text-shadow: 1px 1px 2px black;
			background-image: linear-gradient(to right, red 0%, blue 100%);
			background-repeat: no-repeat; background-size: contain; background-position: center;
			mix-blend-mode: multiply;
			text-decoration: underline line-through; text-overflow: ellipsis; word-break: break-all;
			vertical-align: middle; user-select: none; cursor: inherit;
			fill: none; stroke: red; stroke-width: 2; object-fit: cover;
			grid-template-columns: repeat(3, 1fr) 20px;
			white-space: initial; letter-spacing: unset; text-transform: none;
		}`
	s := styleOf(eng(css), leaf, testEnv)
	check := func(name string, ok bool) {
		t.Helper()
		if !ok {
			t.Errorf("%s wrong", name)
		}
	}
	check("display -webkit-box", s.Display == DisplayFlex && s.FlexDirection == Column && s.LineClamp == 3)
	check("inherit color", s.Color == rgb(10, 20, 30))
	check("bolder", s.FontWeight == 900)
	check("italic", s.Italic)
	check("family", s.FontFamily == "Slack-Lato, Helvetica Neue, sans-serif")
	check("line-height multiplier inherits", s.LineHeight.Pct == 150)
	check("cursor inherit", s.Cursor == CursorPointer)
	check("pointer-events inherit", !s.PointerEvents)
	check("text-align inherit", s.TextAlign == TextCenter)
	check("white-space initial", s.WhiteSpace == WSNormal)
	check("letter-spacing unset (inherited)", s.LetterSpacing == 1)
	check("text-transform", s.TextTransform == TTNone)
	check("visibility inherit", s.Visibility == Hidden)
	check("position", s.Position == PosAbsolute && s.Top.Px == 1 && s.Left.IsAuto() && s.ZIndex == 7 && !s.ZAuto)
	check("sizes", s.MinWidth.Kind == LenMinContent && s.MaxHeight.Kind == LenNone && s.BoxSizing == BorderBox)
	check("aspect", near(s.AspectRatio, 16.0/9))
	check("borders", s.BorderWidth == [4]float32{1, 0, 1, 0} && s.BorderStyle[0] == BorderSolid && s.BorderColor[1] == rgb(255, 0, 0))
	check("radius", s.Radius[0].Px == 3)
	check("outline", s.Outline.Width == 2 && s.Outline.Style == BorderSolid && s.Outline.Color == rgb(0, 0, 255) && s.Outline.Offset == 1)
	check("flex", s.FlexWrap == Wrap && s.JustifyContent == AlignSpaceBetween && s.AlignItems == AlignCenter && s.AlignSelf == AlignEnd && s.AlignContent == AlignStretch)
	check("gaps", s.RowGap.Px == 1 && s.ColGap.Px == 2)
	check("flex shorthand", s.FlexGrow == 2 && s.FlexShrink == 0 && s.FlexBasis.Px == 10 && s.Order == -1)
	check("overflow", s.OverflowX == OverflowHidden && s.OverflowY == OverflowScroll)
	check("opacity", s.Opacity == 0.5)
	if len(s.Transform) != 8 {
		t.Fatalf("transform list %+v", s.Transform)
	}
	check("individual transforms first", s.Transform[0].Kind == TTranslate && s.Transform[0].Y.Px == 2 && s.Transform[1].Kind == TRotate && near(s.Transform[1].Angle, math.Pi/4) && s.Transform[2].Kind == TScale && s.Transform[2].SY == 2)
	check("translateX", s.Transform[3].Kind == TTranslate && s.Transform[3].X.Px == 10)
	check("rotate", near(s.Transform[4].Angle, math.Pi/2))
	check("scale", s.Transform[5].SX == 2 && s.Transform[5].SY == 3)
	check("skew", near(s.Transform[6].SX, 10*math.Pi/180) && s.Transform[6].SY == 0)
	check("matrix", s.Transform[7].M == [6]float32{1, 0, 0, 1, 5, 6})
	check("origin", s.TransformOrigin[0].Pct == 0 && s.TransformOrigin[1].Pct == 0)
	check("filter", s.Filter.Blur == 4 && s.Filter.Grayscale == 1 && s.Filter.Brightness > 0 && s.Filter.Brightness < 0.001)
	check("backdrop", s.BackdropBlur == 8)
	if len(s.BoxShadow) != 3 {
		t.Fatalf("shadows %+v", s.BoxShadow)
	}
	check("box-shadow", s.BoxShadow[0].Inset && s.BoxShadow[0].Spread == 3 && s.BoxShadow[0].Color.A == 128 && s.BoxShadow[1].X == 4 && s.BoxShadow[1].Color == rgb(0, 128, 0))
	check("drop-shadow", s.BoxShadow[2].X == 1 && s.BoxShadow[2].Blur == 3 && s.BoxShadow[2].Color == rgb(255, 0, 0))
	check("text-shadow", len(s.TextShadow) == 1 && s.TextShadow[0].Blur == 2)
	g := s.Background.Gradient
	check("gradient", g != nil && !g.Radial && near(g.Angle, math.Pi/2) && len(g.Stops) == 2 && g.Stops[1].Pos == 1 && g.Stops[0].Color == rgb(255, 0, 0))
	check("background", !s.Background.Repeat && s.Background.Size == "contain" && s.Background.Position == "center")
	check("blend", s.MixBlend == "multiply")
	check("deco", s.TextDecoration == DecoUnderline|DecoLineThrough && s.TextOverflowEllipsis && s.WordBreakAll)
	check("valign", s.VerticalAlign == VAMiddle)
	check("user-select", !s.UserSelect)
	check("svg", s.FillNone && !s.StrokeNone && s.Stroke == rgb(255, 0, 0) && s.StrokeWidth == 2)
	check("object-fit", s.ObjectFit == FitCover)
	check("grid", s.GridColumns == 4)
	check("font-size", s.FontSize == 20)
}

func TestInitialAndInheritance(t *testing.T) {
	leaf := n("span.c")
	_ = n("div.p", leaf)
	e := eng(`.p { color: red; width: 10px; font-size: 30px } .c { width: inherit; color: initial; font-size: unset }`)
	env := testEnv
	env.Fg = rgb(1, 1, 1)
	s := styleOf(e, leaf, env)
	if s.Width.Px != 10 || s.Color != rgb(1, 1, 1) || s.FontSize != 30 {
		t.Errorf("width %v color %v font %v", s.Width, s.Color, s.FontSize)
	}
	// root without rules is Initial
	r := Compute(&Cascaded{}, nil, env)
	in := Initial(env.Fg)
	if r.FontSize != in.FontSize || r.Color != in.Color || !r.Width.IsAuto() || r.Opacity != 1 {
		t.Error("root initial")
	}
	// non-inherited properties reset on children
	k := Compute(&Cascaded{}, &s, env)
	if !k.Width.IsAuto() || k.FontSize != 30 {
		t.Error("children inherit only inherited properties")
	}
}

func TestCursorsAndDisplays(t *testing.T) {
	for kw, want := range cursorKw {
		ds := ParseDecls("cursor: " + kw)
		c := Cascaded{Decls: ds}
		if s := Compute(&c, nil, testEnv); s.Cursor != want {
			t.Errorf("cursor %s", kw)
		}
	}
	c := Cascaded{Decls: ParseDecls("cursor: url(x.png) 4 4, grab")}
	if s := Compute(&c, nil, testEnv); s.Cursor != CursorGrab {
		t.Error("cursor with url list")
	}
	disp := map[string]Display{"block": DisplayBlock, "inline": DisplayInline, "inline-block": DisplayInlineBlock,
		"flex": DisplayFlex, "inline-flex": DisplayInlineFlex, "grid": DisplayGrid, "inline-grid": DisplayInlineGrid,
		"contents": DisplayContents, "none": DisplayNone, "list-item": DisplayListItem, "table": DisplayTable,
		"table-row": DisplayTableRow, "table-cell": DisplayTableCell, "flow-root": DisplayBlock,
		"-webkit-inline-box": DisplayInlineFlex, "inline flex": DisplayInlineFlex}
	for kw, want := range disp {
		c := Cascaded{Decls: ParseDecls("display: " + kw)}
		if s := Compute(&c, nil, testEnv); s.Display != want {
			t.Errorf("display %s = %v", kw, s.Display)
		}
	}
}

// --- React styles ---

func TestFromReactStyle(t *testing.T) {
	ds := FromReactStyle(map[string]any{
		"marginTop":       4,
		"opacity":         0.5,
		"WebkitLineClamp": 2,
		"msTransform":     "rotate(1deg)",
		"MozUserSelect":   "none",
		"--my-var":        "3",
		"zIndex":          3,
		"lineHeight":      1.5,
		"flex":            1,
		"width":           "50%",
		"padding":         0,
		"paddingLeft":     float32(2.5),
		"color":           nil,
		"display":         false,
		"fontSize":        "",
		"bogusThing":      "x",
		"background":      "red !important",
	})
	c := Cascaded{Decls: ds}
	want := map[string]string{
		"margin-top": "4px", "opacity": "0.5", "-webkit-line-clamp": "2", "transform": "rotate(1deg)",
		"user-select": "none", "--my-var": "3", "z-index": "3", "line-height": "1.5",
		"flex-grow": "1", "flex-shrink": "1", "flex-basis": "0%", "width": "50%",
		"padding-top": "0",
	}
	for p, w := range want {
		if got, _ := c.Get(p); got != w {
			t.Errorf("%s = %q want %q", p, got, w)
		}
	}
	for _, p := range []string{"color", "display", "font-size", "bogus-thing", "background-color"} {
		if c.Has(p) {
			t.Errorf("%s should be dropped", p)
		}
	}
	// shorthand before longhand: paddingLeft overrides padding
	cs := eng("").Cascade(n("div"), 0, ds, testEnv)
	if v, _ := cs.Get("padding-left"); v != "2.5px" {
		t.Errorf("padding-left after shorthand: %q", v)
	}
}

func TestParseDecls(t *testing.T) {
	ds := ParseDecls(`color: red !important; /* c */ margin: 0 auto; --x: { a: b }; bad; : nope; width:10px`)
	c := Cascaded{Decls: ds}
	if v, _ := c.Get("color"); v != "red" || !ds[0].Important {
		t.Errorf("important: %+v", ds)
	}
	if v, _ := c.Get("margin-right"); v != "auto" {
		t.Errorf("margin %+v", ds)
	}
	if v, _ := c.Get("--x"); v != "{ a: b }" {
		t.Errorf("custom block %q", v)
	}
	if v, _ := c.Get("width"); v != "10px" {
		t.Errorf("width %q", v)
	}
}

func TestInlineRawShorthand(t *testing.T) {
	// callers may hand Cascade raw declarations
	c := eng(".a { margin-top: 9px }").Cascade(n("div.a"), 0, Decls{{Prop: "margin", Value: "1px"}}, testEnv)
	if v, _ := c.Get("margin-top"); v != "1px" {
		t.Errorf("raw inline shorthand: %q", v)
	}
}

// --- concurrency ---

func TestConcurrentSetCascade(t *testing.T) {
	e := NewEngine()
	el := n("div.a")
	done := make(chan bool)
	go func() {
		for i := 0; i < 200; i++ {
			e.Set("k", fmt.Sprintf(".a { width: %dpx }", i))
		}
		done <- true
	}()
	for i := 0; i < 2000; i++ {
		c := e.Cascade(el, 0, nil, testEnv)
		_ = Compute(&c, nil, testEnv)
	}
	<-done
}

// --- performance ---

var slackWords = []string{"message_kit", "channel_sidebar", "virtual_list", "texty", "wysiwyg_container", "reaction",
	"avatar", "button", "menu", "popover", "tooltip", "message_pane", "thread", "huddle", "search", "workspace",
	"emoji_picker", "file", "link", "mrkdwn", "member", "presence", "tab_rail", "view_contents", "client"}

func benchSheet() string {
	var b strings.Builder
	for i := 0; i < 500; i++ {
		w := slackWords[i%len(slackWords)]
		switch i % 5 {
		case 0:
			fmt.Fprintf(&b, ".c-%s__item--%d { color: rgba(var(--sk_foreground_max_solid,97,96,97),1); padding: 4px %dpx }\n", w, i, i%9)
		case 1:
			fmt.Fprintf(&b, ".p-%s .c-%s__label_%d:hover { background: #fff; border-radius: 4px }\n", w, w, i)
		case 2:
			fmt.Fprintf(&b, ".c-%s__body_%d > .c-%s__text { font-weight: 700 !important }\n", w, i, w)
		case 3:
			fmt.Fprintf(&b, "[data-qa=\"%s_%d\"] { display: none }\n", w, i)
		case 4:
			fmt.Fprintf(&b, ".c-%s--%d::before { content: \"x\" }\n", w, i)
		}
	}
	b.WriteString(".c-message_kit__gutter { display: flex; gap: 8px }\n")
	b.WriteString(".c-message_kit__message .c-message_kit__gutter { padding-left: 20px }\n")
	return b.String()
}

func benchEls() (miss, hit *fakeEl) {
	miss = n("div.c-virtual_list__item.c-virtual_list__item--x")
	miss.noIndex = true
	_ = n("div.p-workspace__primary_view_body", miss)
	hit = n("div.c-message_kit__gutter")
	hit.noIndex = true
	_ = n("div.c-message_kit__message.c-message_kit__message--hover", hit)
	return
}

func TestNonMatchingAllocs(t *testing.T) {
	e := NewEngine()
	e.Set("bench", benchSheet())
	miss, hit := benchEls()
	if c := e.Cascade(hit, 0, nil, testEnv); !c.Has("padding-left") || !c.Has("display") {
		t.Fatalf("hit element didn't match: %+v", c.Decls)
	}
	if c := e.Cascade(miss, 0, nil, testEnv); len(c.Decls) != 0 {
		t.Fatalf("miss element matched: %+v", c.Decls)
	}
	allocs := testing.AllocsPerRun(200, func() {
		e.Cascade(miss, PseudoNone, nil, testEnv)
	})
	if allocs != 0 {
		t.Errorf("non-matching cascade allocates %v times", allocs)
	}
}

func BenchmarkParse500(b *testing.B) {
	src := benchSheet()
	b.ReportAllocs()
	for i := 0; i < b.N; i++ {
		parseSheet(src)
	}
}

func BenchmarkCascadeMiss(b *testing.B) {
	e := NewEngine()
	e.Set("bench", benchSheet())
	miss, _ := benchEls()
	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		e.Cascade(miss, PseudoNone, nil, testEnv)
	}
}

func BenchmarkCascadeHit(b *testing.B) {
	e := NewEngine()
	e.Set("bench", benchSheet())
	_, hit := benchEls()
	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		e.Cascade(hit, PseudoNone, nil, testEnv)
	}
}

func BenchmarkCascadeHitCompute(b *testing.B) {
	e := NewEngine()
	e.Set("bench", benchSheet())
	_, hit := benchEls()
	env := testEnv
	env.RootVars = SlackVars(false)
	parent := Initial(env.Fg)
	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		c := e.Cascade(hit, PseudoNone, nil, env)
		Compute(&c, &parent, env)
	}
}
