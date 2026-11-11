package composer

import (
	"strings"
	"testing"

	"gioui.org/io/key"
)

// mdDescribe draws a Doc the way the expected values below were written
// down from Slack's own code (nTS+, Yu2X, 4NfZ run on quill-delta in
// node): one "kind|runs" per line, a run's formats as {b,i,u,s,c,a=URL}.
func mdDescribe(d *Doc) string {
	var b strings.Builder
	for li := range d.Lines {
		if li > 0 {
			b.WriteByte('\n')
		}
		l := d.Lines[li]
		switch l.Kind {
		case Bullet:
			b.WriteString("bullet" + itoa(l.Indent))
		case Ordered:
			b.WriteString("ordered" + itoa(l.Indent))
		case Quote:
			b.WriteString("quote")
		case CodeBlock:
			b.WriteString("code")
		case Header1, Header2, Header3:
			b.WriteString("h" + itoa(int(l.Kind-Header1)+1))
		default:
			b.WriteString("para")
		}
		b.WriteByte('|')
		if l.Kind == Divider {
			b.WriteString("<hr>")
		}
		ls, le := d.LineStart(li), d.LineEnd(li)
		for i := ls; i < le; {
			a := d.Attrs[i]
			if at := a.Atom; at != nil {
				if at.Kind == AtomEmoji {
					b.WriteString("<emoji " + at.Text + ">")
				} else {
					b.WriteString("<@" + at.Text + ">")
				}
				i++
				continue
			}
			j := i
			for j < le && d.Attrs[j].Atom == nil && d.Attrs[j] == a {
				j++
			}
			var tags []string
			for _, s := range [...]struct {
				st Style
				t  string
			}{{Bold, "b"}, {Italic, "i"}, {Underline, "u"}, {Strike, "s"}, {Code, "c"}} {
				if a.Style&s.st != 0 {
					tags = append(tags, s.t)
				}
			}
			if a.Link != "" {
				tags = append(tags, "a="+a.Link)
			}
			txt := string(d.Runes[i:j])
			if len(tags) > 0 {
				b.WriteString("{" + strings.Join(tags, ",") + "}" + txt + "{/}")
			} else {
				b.WriteString(txt)
			}
			i = j
		}
	}
	return b.String()
}

// Mod+Shift+F on typed markdown: the results and selections Slack's
// convertMarkdown gives for the same text (its modules run in node on
// quill-delta 5.1.0; a collapsed caret converts the whole message, a
// selection only itself).
func TestConvertMarkdownLikeSlack(t *testing.T) {
	cases := []struct {
		name, text     string
		lo, hi         int
		want           string
		wantLo, wantHi int
	}{
		{"bold", "make *this* bold", 16, 16, "para|make {b}this{/} bold", 14, 14},
		{"italic", "an _italic_ word", 16, 16, "para|an {i}italic{/} word", 14, 14},
		{"strike", "~gone~ now", 10, 10, "para|{s}gone{/} now", 8, 8},
		{"code", "run `go vet` please", 19, 19, "para|run {c}go vet{/} please", 17, 17},
		{"all inline", "*b* _i_ ~s~ `c`", 15, 15, "para|{b}b{/} {i}i{/} {s}s{/} {c}c{/}", 7, 7},
		{"double star", "**bold**", 8, 8, "para|*{b}bold{/}*", 6, 6},
		{"nested", "*_both_*", 8, 8, "para|{b,i}both{/}", 4, 4},
		{"mid-word no", "a*b*c", 5, 5, "para|a*b*c", 5, 5},
		{"space inside no", "* not bold *", 12, 12, "bullet0|not bold *", 10, 10},
		{"unclosed", "*open", 5, 5, "para|*open", 5, 5},
		{"code masks bold", "`*not bold*` *bold*", 19, 19, "para|{c}*not bold*{/} {b}bold{/}", 15, 15},
		{"shrug", "¯\\_(ツ)_/¯", 9, 9, "para|¯\\_(ツ)_/¯", 9, 9},
		{"bullets", "- one\n- two\n- three", 19, 19, "bullet0|one\nbullet0|two\nbullet0|three", 13, 13},
		{"star bullets", "* one\n* two", 11, 11, "bullet0|one\nbullet0|two", 7, 7},
		{"nested bullets", "- one\n    - two\n        - three\n- four", 38, 38, "bullet0|one\nbullet1|two\nbullet2|three\nbullet0|four", 18, 18},
		{"tab bullets", "- one\n\t- two", 12, 12, "bullet0|one\nbullet1|two", 7, 7},
		{"ordered", "1. one\n2. two\n3. three", 22, 22, "ordered0|one\nordered0|two\nordered0|three", 13, 13},
		{"single ordered no", "1. alone", 8, 8, "para|1. alone", 8, 8},
		{"ordered not from one", "2. two\n3. three", 15, 15, "para|2. two\npara|3. three", 15, 15},
		{"ordered after blank", "1. one\n2. two\n\n3. three", 23, 23, "ordered0|one\nordered0|two\npara|\npara|3. three", 17, 17},
		{"mixed list", "- a\n1. one\n2. two", 17, 17, "bullet0|a\nordered0|one\nordered0|two", 9, 9},
		{"list with bold", "- *bold* item\n- _it_", 20, 20, "bullet0|{b}bold{/} item\nbullet0|{i}it{/}", 12, 12},
		{"quote", "> quoted", 8, 8, "quote|quoted", 6, 6},
		{"quote two lines", "> one\n> two", 11, 11, "quote|one\nquote|two", 7, 7},
		{"quote then text", "> quoted\nplain", 14, 14, "quote|quoted\npara|plain", 12, 12},
		{"lone gt first", ">\n*bold*", 8, 8, "para|\npara|{b}*bo{/}d*", 6, 6},
		{"triple quote", ">>> all\nof this\nis quoted", 25, 25, "quote|all\nquote|of this\nquote|is quoted", 21, 21},
		{"triple quote alone", ">>>\nquoted\nlines", 16, 16, "quote|quoted\nquote|lines", 12, 12},
		{"fence", "```code```", 10, 10, "code|code", 4, 4},
		{"fence multi", "```\nline one\nline two\n```", 25, 25, "code|line one\ncode|line two", 17, 17},
		{"fence inline", "see ```x = 1``` here", 20, 20, "para|see\ncode|x = 1\npara|here", 14, 14},
		{"fence with markdown", "```*not bold*```", 16, 16, "code|*not bold*", 10, 10},
		{"fence after text", "text\n```\ncode\n```\nmore", 22, 22, "para|text\ncode|code\npara|more", 14, 14},
		{"two fences", "```a``` and ```b```", 19, 19, "code|a\npara|and\ncode|b", 7, 7},
		{"fullwidth fence", "｀｀｀code｀｀｀", 10, 10, "code|code", 4, 4},
		{"link", "see [docs](https://example.com) now", 35, 35, "para|see {a=https://example.com}docs{/} now", 12, 12},
		{"link www", "[home](www.example.com)", 23, 23, "para|{a=http://www.example.com}home{/}", 4, 4},
		{"link mailto", "[mail](mailto:a@b.com)", 22, 22, "para|{a=mailto:a@b.com}mail{/}", 4, 4},
		{"link bare email", "[mail](a@b.com)", 15, 15, "para|{a=mailto:a@b.com}mail{/}", 4, 4},
		{"link bad", "[x](not a url)", 14, 14, "para|[x](not a url)", 14, 14},
		{"link parens", "[w](https://en.wikipedia.org/wiki/Go_(game)) ok", 47, 47, "para|{a=https://en.wikipedia.org/wiki/Go_(game)}w{/} ok", 4, 4},
		{"bold inside link no", "[*b*](https://x.com)", 20, 20, "para|{a=https://x.com}*b*{/}", 3, 3},
		{"cjk bold", "これは＊太字＊です", 9, 9, "para|これは{b}太字{/}です", 7, 7},
		{"cjk code", "コード｀x｀です", 8, 8, "para|コード{c}x{/}です", 6, 6},
		{"fw italic", "＿it＿ ok", 7, 7, "para|{i}it{/} ok", 5, 5},
		{"quote list", "> - item\n> - item2", 18, 18, "bullet0|item\nbullet0|item2", 10, 10},
		{"triple quote list", ">>> - a\n- b", 11, 11, "quote|- a\nbullet0|b", 5, 5},
		{"pipe", "|*b*| =*c*=", 11, 11, "para||{b}b{/}| ={b}c{/}=", 7, 7},
		{"plus", "+*b*+", 5, 5, "para|+{b}b{/}+", 3, 3},
		{"selection bold", "a *b* c *d*", 0, 5, "para|a {b}b{/} c *d*", 0, 3},
		{"selection mid line", "x *b* y", 2, 5, "para|x {b}b{/} y", 2, 3},
		{"selection list", "intro\n- a\n- b\nend", 6, 13, "para|intro\nbullet0|a\nbullet0|b\npara|\npara|end", 6, 9},
		{"selection fence", "a ```code``` b", 2, 12, "para|a \ncode|code\npara| b", 2, 8},
		{"caret middle", "one *two* three", 4, 4, "para|one {b}two{/} three", 4, 4},
		{"caret after", "*a* b", 5, 5, "para|{b}a{/} b", 3, 3},
		{"selection reversed", "a *b* c", 2, 5, "para|a {b}b{/} c", 2, 3},
		{"long ordered", "1. a\n2. b\n3. c\n4. d\n5. e\n6. f\n7. g\n8. h\n9. i\n10. j\n11. k", 56, 56, "ordered0|a\nordered0|b\nordered0|c\nordered0|d\nordered0|e\nordered0|f\nordered0|g\nordered0|h\nordered0|i\nordered0|j\nordered0|k", 21, 21},
		{"nested ordered", "1. a\n    1. b\n    2. c\n2. d", 27, 27, "ordered0|a\nordered1|b\nordered1|c\nordered0|d", 7, 7},
		{"ordered back out", "1. a\n    - b\n2. c", 17, 17, "para|1. a\nbullet1|b\npara|2. c", 11, 11},
		{"underscore list", ">_x\n- a", 7, 7, "para|>_x\nbullet0|a", 5, 5},
		{"quote then fence", "> q\n```c```", 11, 11, "quote|q\ncode|c", 3, 3},
		{"triple then fence", ">>> q\n```c```", 13, 13, "quote|q\ncode|c", 3, 3},
		{"empty", "", 0, 0, "para|", 0, 0},
		{"only markers", "** __ ~~ ``", 11, 11, "para|** __ ~~ ``", 11, 11},
		{"adjacent", "*a**b*", 6, 6, "para|{b}a{/}*b*", 4, 4},
		{"adjacent spaced", "*a* *b*", 7, 7, "para|{b}a{/} {b}b{/}", 3, 3},
		{"code then bold overlap", "`a *b` c*", 9, 9, "para|{c}a *b{/} c*", 7, 7},
		{"strike in bold", "*a ~b~ c*", 9, 9, "para|{b}a {/}{b,s}b{/}{b} c{/}", 5, 5},
		{"url autolink not", "see https://x.com *b*", 21, 21, "para|see https://x.com {b}b{/}", 19, 19},
	}
	for _, c := range cases {
		d := NewDoc()
		d.Insert(0, c.text, Attr{})
		anchor, caret := c.lo, c.hi
		if c.name == "selection reversed" {
			anchor, caret = c.hi, c.lo
		}
		nd, a, k, _ := mdConvert(d, anchor, caret)
		if got := mdDescribe(nd); got != c.want {
			t.Errorf("%s: %q\n got %q\nwant %q", c.name, c.text, got, c.want)
			continue
		}
		lo, hi := min(a, k), max(a, k)
		if lo != c.wantLo || hi != c.wantHi {
			t.Errorf("%s: selection %d..%d, want %d..%d", c.name, lo, hi, c.wantLo, c.wantHi)
		}
		if c.name == "selection reversed" && (a != c.wantHi || k != c.wantLo) {
			t.Errorf("%s: anchor %d caret %d, want the anchor after the caret", c.name, a, k)
		}
		if len(nd.Lines) != strings.Count(string(nd.Runes), "\n")+1 {
			t.Errorf("%s: %d lines for %q", c.name, len(nd.Lines), string(nd.Runes))
		}
	}
}

// Text that already has formats, mentions, emoji or block formats, the
// same cases run through Slack's code.
func TestConvertMarkdownKeepsWhatIsThere(t *testing.T) {
	karim := &Atom{Kind: AtomUser, ID: "U1", Text: "@karim"}
	smile := &Atom{Kind: AtomEmoji, ID: "smile", Text: ":smile:"}
	tada := &Atom{Kind: AtomEmoji, ID: "tada", Text: ":tada:"}
	type run struct {
		s    string
		a    Attr
		atom *Atom
	}
	build := func(lines []Line, runs ...run) *Doc {
		d := NewDoc()
		for _, r := range runs {
			if r.atom != nil {
				d.InsertAtom(d.Len(), r.atom)
				continue
			}
			d.Insert(d.Len(), r.s, r.a)
		}
		for i, l := range lines {
			d.Lines[i] = l
		}
		return d
	}
	cases := []struct {
		name string
		d    *Doc
		want string
	}{
		{"existing bold kept", build(nil, run{s: "pre "}, run{s: "bold", a: Attr{Style: Bold}}, run{s: " *new*"}), "para|pre {b}bold{/} {b}new{/}"},
		{"existing code masked", build(nil, run{s: "x "}, run{s: "*a*", a: Attr{Style: Code}}, run{s: " y"}), "para|x {c}*a*{/} y"},
		{"existing link masked", build(nil, run{s: "go "}, run{s: "*l*", a: Attr{Link: "https://l.com"}}, run{s: " *b*"}), "para|go {a=https://l.com}*l*{/} {b}b{/}"},
		{"mention in bold", build(nil, run{s: "*hi "}, run{atom: karim}, run{s: "*"}), "para|{b}hi {/}<@@karim>"},
		{"emoji in fence", build(nil, run{s: "```a "}, run{atom: smile}, run{s: " b```"}), "code|a :smile: b"},
		{"mention in fence", build(nil, run{s: "```hi "}, run{atom: karim}, run{s: "```"}), "code|hi @karim"},
		{"emoji in code", build(nil, run{s: "`x "}, run{atom: tada}, run{s: " y`"}), "para|{c}x :tada: y{/}"},
		{"already a list", build([]Line{{Kind: Bullet}}, run{s: "*b*\n- x"}), "bullet0|{b}b{/}\nbullet0|x"},
		{"already quote", build([]Line{{Kind: Quote}}, run{s: "> a"}), "quote|> a"},
		{"in code block", build([]Line{{Kind: CodeBlock}}, run{s: "*b*"}), "code|*b*"},
		{"header line", build([]Line{{Kind: Header1}}, run{s: "*b* - x"}), "h1|{b}b{/} - x"},
		{"hr", build([]Line{{Kind: Divider}}, run{s: "\n*b*"}), "para|<hr>\npara|{b}b{/}"},
	}
	for _, c := range cases {
		nd, _, _, _ := mdConvert(c.d, c.d.Len(), c.d.Len())
		if got := mdDescribe(nd); got != c.want {
			t.Errorf("%s:\n got %q\nwant %q", c.name, got, c.want)
		}
	}
}

// The binding: Mod+Shift+F converts as one undo step; markup mode has no
// such binding; Mod+F alone isn't it.
func TestConvertMarkdownKey(t *testing.T) {
	e := NewEditor()
	d := NewDoc()
	d.Insert(0, "make *this* bold", Attr{}) // as pasted: typing would format it live
	e.SetDoc(d)
	edPress(e, "F", key.ModShortcut)
	if e.Doc().Text() != "make *this* bold" {
		t.Fatal("Mod+F converted")
	}
	edPress(e, "F", key.ModShortcut|key.ModShift)
	if got := mdDescribe(e.Doc()); got != "para|make {b}this{/} bold" {
		t.Fatalf("after Mod+Shift+F %q", got)
	}
	if _, c := e.Selection(); c != 14 {
		t.Errorf("caret %d, want 14 (still at the end)", c)
	}
	e.Undo()
	if e.Doc().Text() != "make *this* bold" {
		t.Fatalf("undo gave %q", e.Doc().Text())
	}
	e.plainText = true
	edPress(e, "F", key.ModShortcut|key.ModShift)
	if e.Doc().Text() != "make *this* bold" {
		t.Error("converted in markup mode")
	}
}
