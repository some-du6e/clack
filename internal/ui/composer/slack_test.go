package composer

import (
	"image/color"
	"testing"
	"time"

	"github.com/some-du6e/yoloslackgoclient/internal/ui/slackc"
)

func TestHideBits(t *testing.T) {
	if h := hideBits(nil); h != 0 {
		t.Fatalf("nil props hide %b", h)
	}
	p := slackc.Props{"enableMentionButton": false, "enableFileButton": true, "enableShortcutsButton": false, "minButtonsForOverflow": 1}
	want := uint32(1<<crMention | 1<<crShortcut)
	if h := hideBits(p); h != want {
		t.Fatalf("hideBits = %b, want %b", h, want)
	}
	q := slackc.Props{}
	putEnableFlags(q, want)
	if h := hideBits(q); h != want {
		t.Fatalf("round trip = %b, want %b", h, want)
	}
	if q["enableFileButton"] != true || q["enableSlashCommandsButton"] != false {
		t.Fatalf("putEnableFlags = %v", q)
	}
}

func TestFoldFooter(t *testing.T) {
	var c chrome
	c.cur.Mode = modeChannel
	c.f.minOverflow = 1
	if c.foldFooter() {
		t.Fatal("wide composer folded")
	}
	c.f.narrow = true
	if !c.foldFooter() {
		t.Fatal("narrow composer didn't fold")
	}
	c.f.minOverflow = 5 // Slack's default: a group of 3 never folds
	if c.foldFooter() {
		t.Fatal("folded fewer than minButtonsForOverflow")
	}
	c.f.minOverflow = 1
	c.f.hide = 1<<crVideo | 1<<crAudio | 1<<crShortcut
	if c.foldFooter() {
		t.Fatal("folded with nothing to fold")
	}
	c.f.hide = 0
	c.cur.Mode = modeEdit
	if c.foldFooter() {
		t.Fatal("edit composer folded")
	}
}

func TestKidUnpatched(t *testing.T) {
	if _, patched := kid(nil, "MessageInput"); patched {
		t.Fatal("nil props reported patched")
	}
	e, patched := kid(slackc.Props{"channelId": "C1"}, "MessageInput")
	if !patched || len(e.Nodes) != 0 {
		t.Fatalf("dropped children: %v %v", e, patched)
	}
}

func TestParseCSSColor(t *testing.T) {
	for in, want := range map[string]color.NRGBA{
		"#fff":                  {255, 255, 255, 255},
		"#11223380":             {0x11, 0x22, 0x33, 0x80},
		"rgb(26, 73, 214)":      {26, 73, 214, 255},
		"rgba(26,73,214,0.5)":   {26, 73, 214, 128},
		"rgb(26 73 214 / 50%)":  {26, 73, 214, 128},
		"RGBA(255, 0, 0, 1)":    {255, 0, 0, 255},
		"rgba(100%, 0%, 0%, 0)": {255, 0, 0, 0},
	} {
		got, ok := parseCSSColor(in)
		if !ok || got != want {
			t.Errorf("parseCSSColor(%q) = %v %v, want %v", in, got, ok, want)
		}
	}
	for _, bad := range []string{"", "red", "#12", "rgb(1,2)", "hsl(1,2%,3%)", "rgb(a,b,c)"} {
		if _, ok := parseCSSColor(bad); ok {
			t.Errorf("parseCSSColor(%q) accepted", bad)
		}
	}
}

func TestCSSVarColor(t *testing.T) {
	// OwlMode's composer theme.
	vars := map[string]string{
		"--taut-owl":                         "26, 73, 214",
		"--TextyTheme-send-bg":               "rgba(var(--taut-owl), 1)",
		"--TextyTheme-wysiwyg-border-color":  "rgba(var(--taut-owl), 0.6)",
		"--TextyTheme-send-bg-hover":         "rgba(var(--missing, 1, 2, 3), 1)",
		"--TextyTheme-unbalanced":            "rgba(var(--taut-owl, 1)",
		"--TextyTheme-wysiwyg-border-color2": "var(--TextyTheme-send-bg)",
	}
	cases := map[string]color.NRGBA{
		"--TextyTheme-send-bg":               {26, 73, 214, 255},
		"--TextyTheme-wysiwyg-border-color":  {26, 73, 214, 153},
		"--TextyTheme-send-bg-hover":         {1, 2, 3, 255},
		"--TextyTheme-wysiwyg-border-color2": {26, 73, 214, 255},
	}
	for name, want := range cases {
		if got, ok := cssVarColor(vars, name); !ok || got != want {
			t.Errorf("cssVarColor(%s) = %v %v, want %v", name, got, ok, want)
		}
	}
	if _, ok := cssVarColor(vars, "--TextyTheme-unbalanced"); ok {
		t.Error("unbalanced var() accepted")
	}
	if _, ok := cssVarColor(vars, "--absent"); ok {
		t.Error("absent var accepted")
	}
}

func docOf(s string) *Doc {
	d := NewDoc()
	d.Insert(0, s, Attr{})
	return d
}

func TestMarkupInline(t *testing.T) {
	defer func(f func(string) []MarkupSpan) { InlineMarkup = f }(InlineMarkup)
	var got string
	InlineMarkup = func(line string) []MarkupSpan {
		got = line
		// "**bold**" at the end of the line
		n := len([]rune(line))
		if n >= 6 && string([]rune(line)[n-2:]) == "**" {
			return []MarkupSpan{{Start: n - 8, End: n, ContentStart: n - 6, ContentEnd: n - 2, Format: "bold"}}
		}
		return nil
	}
	d := docOf("hi **bold**")
	nc, st, ok := markupInline(d, d.Len())
	if !ok || st != Bold || nc != 7 {
		t.Fatalf("markupInline = %d %v %v", nc, st, ok)
	}
	if got != "hi **bold**" {
		t.Fatalf("parse got %q", got)
	}
	if d.Text() != "hi bold" {
		t.Fatalf("text %q", d.Text())
	}
	for i := 3; i < 7; i++ {
		if d.Attrs[i].Style&Bold == 0 {
			t.Fatalf("rune %d not bold", i)
		}
	}
	if d.Attrs[2].Style&Bold != 0 {
		t.Fatal("space before bold styled")
	}

	// A span not ending at the caret is ignored; so is code.
	d = docOf("x")
	if _, _, ok := markupInline(d, 1); ok {
		t.Fatal("applied without a span")
	}
	d = docOf("**a**")
	d.SetStyle(0, d.Len(), Code, true)
	if _, _, ok := markupInline(d, d.Len()); ok {
		t.Fatal("applied inside code")
	}
}

func TestMarkupInlineLinkAndEscape(t *testing.T) {
	defer func(f func(string) []MarkupSpan) { InlineMarkup = f }(InlineMarkup)
	InlineMarkup = func(line string) []MarkupSpan {
		switch line {
		case "[a](u)":
			return []MarkupSpan{{Start: 0, End: 6, ContentStart: 1, ContentEnd: 2, Format: "link", Link: "https://u"}}
		case `\*`:
			return []MarkupSpan{{Start: 0, End: 2, ContentStart: 1, ContentEnd: 2}}
		}
		return nil
	}
	d := docOf("[a](u)")
	if nc, _, ok := markupInline(d, d.Len()); !ok || nc != 1 || d.Text() != "a" || d.Attrs[0].Link != "https://u" {
		t.Fatalf("link: %d %v %q %q", nc, ok, d.Text(), d.Attrs[0].Link)
	}
	d = docOf(`\*`)
	if nc, st, ok := markupInline(d, d.Len()); !ok || nc != 1 || st != 0 || d.Text() != "*" {
		t.Fatalf("escape: %d %v %v %q", nc, st, ok, d.Text())
	}
}

func TestTransformPastedLinks(t *testing.T) {
	defer func(f func(string) string) { LinkTransforms = f }(LinkTransforms)
	LinkTransforms = func(u string) string {
		if u == "https://a.com/x?utm_source=y" {
			return "https://a.com/x"
		}
		return u
	}
	d := docOf("see https://a.com/x?utm_source=y and https://b.com")
	end := transformPastedLinks(d, 0, d.Len())
	if d.Text() != "see https://a.com/x and https://b.com" || end != d.Len() {
		t.Fatalf("got %q end %d len %d", d.Text(), end, d.Len())
	}
	if transformLink("https://a.com/x?utm_source=y") != "https://a.com/x" {
		t.Fatal("transformLink")
	}
	LinkTransforms = nil
	if transformLink("u") != "u" {
		t.Fatal("transformLink without a hook")
	}
}

func TestDelta(t *testing.T) {
	d := docOf("hi bold\nitem")
	d.SetStyle(3, 7, Bold, true)
	d.Lines[1] = Line{Kind: Bullet, Indent: 1}
	d.InsertAtom(0, &Atom{Kind: AtomUser, ID: "U1", Text: "@k"})
	ops := d.Delta()["ops"].([]any)
	if len(ops) != 6 {
		t.Fatalf("ops = %v", ops)
	}
	op0 := ops[0].(map[string]any)["insert"].(map[string]any)
	if op0["slackuser"].(map[string]any)["id"] != "U1" {
		t.Fatalf("embed %v", op0)
	}
	if ops[1].(map[string]any)["insert"] != "hi " {
		t.Fatalf("text %v", ops[1])
	}
	b := ops[2].(map[string]any)
	if b["insert"] != "bold" || b["attributes"].(map[string]any)["bold"] != true {
		t.Fatalf("bold %v", b)
	}
	if nl := ops[3].(map[string]any); nl["insert"] != "\n" || nl["attributes"] != nil {
		t.Fatalf("first newline %v", nl)
	}
	last := ops[5].(map[string]any)
	at := last["attributes"].(map[string]any)
	if last["insert"] != "\n" || at["list"] != "bullet" || at["indent"] != 1 {
		t.Fatalf("list line %v", last)
	}
}

func TestTypersSorted(t *testing.T) {
	s := &typingState{m: map[string]map[string]typer{}}
	until := time.Now().Add(time.Minute)
	s.m[draftKey("C", "")] = map[string]typer{
		"U2": {name: "Zed", until: until},
		"U1": {name: "Amy", until: until},
		"U3": {name: "Me", until: until},
	}
	ids, names := s.typers("C", "", "U3")
	if len(ids) != 2 || ids[0] != "U1" || names[0] != "Amy" || ids[1] != "U2" || names[1] != "Zed" {
		t.Fatalf("typers = %v %v", ids, names)
	}
	if got := typingLine(names); got != "Amy and Zed are typing…" {
		t.Fatalf("line %q", got)
	}
}
