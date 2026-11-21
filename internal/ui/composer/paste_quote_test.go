package composer

import (
	"fmt"
	"strings"
	"testing"
)

// Karim, 2026-11-21: a quote started in the composer (">" typed, or the
// formatting bar's Blockquote button), then a paste. Every paste into a
// quote goes in as Slack's composer puts it there (pasteQuoted: Texty's
// prepareDeltaForPaste with the caret's blockquote, then
// convertDeltaToBlockFormat with {blockquote: true}): each pasted line a
// quote line, a list item or code line one inside the quote, a pasted
// quote a quote line, and the caret after the paste.

// quoteDump is phDump with ">" before a line inside a quote (Line.Quoted).
func quoteDump(d *Doc) []string {
	out := phDump(d)
	for i := range out {
		if d.Lines[i].Quoted {
			out[i] = ">" + out[i]
		}
	}
	return out
}

func quoteCheck(t *testing.T, name string, e *Editor, want ...string) {
	t.Helper()
	if got := quoteDump(e.doc); strings.Join(got, "\n") != strings.Join(want, "\n") {
		t.Errorf("%s:\n got %q\nwant %q", name, got, want)
	}
}

// quoted is an editor whose one line is a quote, started either way, with
// text typed into it.
func quoted(t *testing.T, way, text string) *Editor {
	t.Helper()
	e := NewEditor()
	switch way {
	case "typed":
		edType(e, ">")
	case "button":
		e.ToggleBlock(Quote)
	}
	edType(e, text)
	if len(e.doc.Lines) != 1 || e.doc.Lines[0].Kind != Quote {
		t.Fatalf("%s: the quote didn't start: %q", way, quoteDump(e.doc))
	}
	return e
}

// caretAt reports where the caret is: its line and offset in it.
func caretAt(e *Editor) string {
	l := e.doc.LineOf(e.caret)
	return fmt.Sprintf("line %d at %d", l, e.caret-e.doc.LineStart(l))
}

func TestPasteIntoQuote(t *testing.T) {
	var long, longText []string
	for i := 0; i < 400; i++ {
		l := fmt.Sprintf("line %03d of a long paste, with enough words in it to wrap the composer", i)
		long = append(long, "q "+l)
		longText = append(longText, l)
	}
	page := `<p style="margin:0">A paragraph with <b>bold</b> words</p><ul><li>item one</li><li>item two</li></ul>` +
		`<blockquote>an inner quote</blockquote><pre>code line</pre><p>the end</p>`
	texty := `{"ops":[{"insert":"quoted from slack"},{"attributes":{"blockquote":true},"insert":"\n"},{"insert":"and a ` +
		`line after it"},{"insert":"\n"}]}`
	for _, way := range []string{"typed", "button"} {
		for _, c := range []struct {
			name       string
			text, html string
			texty      string
			want       []string
			wantCaret  string
		}{
			{name: "one line", text: "quoted words", want: []string{"q quoted words"}, wantCaret: "line 0 at 12"},
			{name: "several lines", text: "first line\nsecond line\nthird line",
				want: []string{"q first line", "q second line", "q third line"}, wantCaret: "line 2 at 10"},
			{name: "CRLF line ends", text: "first line\r\nsecond line\r\nthird line",
				want: []string{"q first line", "q second line", "q third line"}, wantCaret: "line 2 at 10"},
			{name: "a trailing newline", text: "ends with a break\r\n",
				want: []string{"q ends with a break", "q "}, wantCaret: "line 1 at 0"},
			{name: "a long text", text: strings.Join(longText, "\r\n"), want: long, wantCaret: "line 399 at 70"},
			// Chromium's copy of a web page: a paragraph, a list, a quote,
			// code and a paragraph
			{name: "a web page (HTML)", text: "A paragraph with bold words\nitem one\nitem two\nan inner quote\ncode line\nthe end",
				html:      page,
				want:      []string{"q A paragraph with *bold* words", ">ul0 item one", ">ul0 item two", "q an inner quote", ">pre code line", "q the end"},
				wantCaret: "line 5 at 7"},
			// Slack's Copy message of a quote and a line after it: both quote
			// lines (a pasted quote in a quote is a quote line)
			{name: "Slack's Copy message (slack/texty)", text: "quoted from slack\nand a line after it", texty: texty,
				want: []string{"q quoted from slack", "q and a line after it"}, wantCaret: "line 1 at 19"},
		} {
			t.Run(way+", "+c.name, func(t *testing.T) {
				e := quoted(t, way, "")
				var td *Doc
				if c.texty != "" {
					if td = textyDoc(c.texty); td == nil {
						t.Fatal("textyDoc took nothing")
					}
				}
				e.pasteClip(c.text, c.html, td)
				quoteCheck(t, "the composer", e, c.want...)
				if got := caretAt(e); got != c.wantCaret {
					t.Errorf("caret %s, want %s", got, c.wantCaret)
				}
			})
		}
	}
}

// A paste into a quote that has text on it, and the paste's lists, code
// and quotes (prepareDeltaForPaste's rules for a line with text, and
// convertDeltaToBlockFormat's final newline).
func TestPasteIntoQuoteLines(t *testing.T) {
	mid := func(t *testing.T) *Editor {
		e := quoted(t, "typed", "abcd") // ">" went: the quote line holds "abcd"
		e.anchor, e.caret = 2, 2        // "ab|cd"
		return e
	}
	t.Run("a list mid-line", func(t *testing.T) {
		e := mid(t)
		e.pasteHTML("x\ny\nz", `<ul><li>x</li><li>y</li><li>z</li></ul>`)
		// Quill's newline carries the list: "ab" joins the first item, and
		// "cd" stays a quote line after the list (its final newline kept)
		quoteCheck(t, "list", e, ">ul0 abx", ">ul0 y", ">ul0 z", "q cd")
		if got := caretAt(e); got != "line 3 at 0" {
			t.Errorf("caret %s, want line 3 at 0", got)
		}
	})
	t.Run("one list item on a line with text", func(t *testing.T) {
		e := mid(t)
		e.pasteHTML("x", `<ul><li>x</li></ul>`)
		quoteCheck(t, "one item", e, "q abxcd")
	})
	t.Run("one list item on an empty quote line", func(t *testing.T) {
		e := quoted(t, "button", "")
		e.pasteHTML("x", `<ul><li>x</li></ul>`)
		quoteCheck(t, "one item", e, ">ul0 x", "q ")
	})
	t.Run("two items: both at indent 0", func(t *testing.T) {
		e := quoted(t, "typed", "")
		e.pasteHTML("a\nb", `<ul><li>a<ul><li>b</li></ul></li></ul>`)
		quoteCheck(t, "two items", e, ">ul0 a", ">ul0 b", "q ")
	})
	t.Run("three items keep their indents", func(t *testing.T) {
		e := quoted(t, "typed", "")
		e.pasteHTML("a\nb\nc", `<ul><li>a<ul><li>b</li><li>c</li></ul></li></ul>`)
		quoteCheck(t, "three items", e, ">ul0 a", ">ul1 b", ">ul1 c", "q ")
	})
	t.Run("code after text: on a line of its own", func(t *testing.T) {
		e := quoted(t, "typed", "ab")
		e.pasteHTML("x\ny", `<pre>x
y</pre>`)
		quoteCheck(t, "code", e, "q ab", ">pre x", ">pre y", "q ")
	})
	t.Run("a lone code line after text: text", func(t *testing.T) {
		e := quoted(t, "typed", "ab")
		e.pasteHTML("x", `<pre>x</pre>`)
		quoteCheck(t, "lone code line", e, "q abx")
	})
	t.Run("a quote and a heading: quote lines", func(t *testing.T) {
		e := quoted(t, "button", "")
		e.pasteHTML("inner\ntitle", `<blockquote>inner</blockquote><h2>title</h2>`)
		quoteCheck(t, "quote and heading", e, "q inner", "q *title*")
	})
	t.Run("Slack's copy of a list and code", func(t *testing.T) {
		e := quoted(t, "typed", "")
		d := textyDoc(`{"ops":[{"insert":"item"},{"attributes":{"list":"bullet"},"insert":"\n"},` +
			`{"insert":"nested"},{"attributes":{"list":"bullet","indent":1},"insert":"\n"},` +
			`{"insert":"more"},{"attributes":{"list":"bullet"},"insert":"\n"},` +
			`{"insert":"code"},{"attributes":{"code-block":true},"insert":"\n"}]}`)
		e.pasteClip("item\nnested\nmore\ncode", "", d)
		quoteCheck(t, "texty", e, ">ul0 item", ">ul1 nested", ">ul0 more", ">pre code", "q ")
	})
	t.Run("the composer's own copy", func(t *testing.T) {
		src := NewEditor()
		src.pasteHTML("a\nb\nc", `<ul><li><b>a</b></li><li>b</li></ul><p>c</p>`)
		sl := src.doc.Slice(0, src.doc.Len())
		e := quoted(t, "button", "")
		e.clipText, e.clipDoc = sl.copyText(), sl
		e.pasteHTML(e.clipText, "")
		quoteCheck(t, "own copy", e, ">ul0 *a*", ">ul0 b", "q c")
	})
	t.Run("plain text (Shift): quote lines", func(t *testing.T) {
		e := quoted(t, "typed", "")
		e.plainPaste = true
		e.pasteHTML("- a\n- b", `<ul><li>a</li><li>b</li></ul>`)
		quoteCheck(t, "plain", e, "q - a", "q - b")
	})
}

// Slack's Copy message keeps its line formats in a plain line too: a
// quote stays a quote, a list a list (textyDoc, where textyHTML dropped
// them), and its mentions stay mentions.
func TestTextyDocLines(t *testing.T) {
	d := textyDoc(`{"ops":[{"insert":"quoted from slack"},{"attributes":{"blockquote":true},"insert":"\n"},` +
		`{"insert":"cc "},{"insert":"@karim","attributes":{"slackmention":{"id":"U0FAKE00001","label":"@karim"}}},` +
		`{"insert":" now","attributes":{"bold":true}},{"insert":"\n"},` +
		`{"insert":"a"},{"attributes":{"list":"ordered","indent":2},"insert":"\n"},` +
		`{"insert":"in a quote"},{"attributes":{"list":"bullet","border":1},"insert":"\n"},` +
		`{"insert":{"slackhorizontalrule":true}},{"insert":{"slackemoji":{"name":"wave","text":":wave:"}}},{"insert":"\n"}]}`)
	got := quoteDump(d)
	want := []string{"q quoted from slack", "p cc " + string(ObjRune) + "* now*", "ol2 a", ">ul0 in a quote", "p ", "p :wave:"}
	if strings.Join(got, "\n") != strings.Join(want, "\n") {
		t.Errorf("textyDoc:\n got %q\nwant %q", got, want)
	}
	if a := pasteAtoms(d); len(a) != 1 || a[0] != fmt.Sprintf("%d:U0FAKE00001:@karim", AtomUser) {
		t.Errorf("atoms %q", a)
	}
	e := NewEditor()
	e.pasteClip("quoted from slack\nand a line after it", "", textyDoc(`{"ops":[{"insert":"quoted from slack"},`+
		`{"attributes":{"blockquote":true},"insert":"\n"},{"insert":"and a line after it"},{"insert":"\n"}]}`))
	quoteCheck(t, "into an empty composer", e, "q quoted from slack", "p and a line after it")
	for _, junk := range []string{"", "not json", `{"ops":[]}`, `{"ops":[{"insert":"\n"}]}`} {
		if textyDoc(junk) != nil {
			t.Errorf("textyDoc took %q", junk)
		}
	}
}
