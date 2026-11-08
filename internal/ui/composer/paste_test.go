package composer

import (
	"fmt"
	"strings"
	"testing"
)

// phDump renders a Doc one line per entry: "kind[indent] text", with runs
// marked *bold* _italic_ ~strike~ +underline+ `code` and [text](url).
func phDump(d *Doc) []string {
	if d == nil {
		return nil
	}
	names := map[BlockKind]string{Para: "p", Bullet: "ul", Ordered: "ol", Quote: "q", CodeBlock: "pre",
		Header1: "h1", Header2: "h2", Header3: "h3", Divider: "hr"}
	var out []string
	for i := range d.Lines {
		l := d.Lines[i]
		var b strings.Builder
		b.WriteString(names[l.Kind])
		if rtIsList(l.Kind) {
			fmt.Fprintf(&b, "%d", l.Indent)
		}
		b.WriteByte(' ')
		s, e := d.LineStart(i), d.LineEnd(i)
		for j := s; j < e; {
			a := d.Attrs[j]
			k := j + 1
			for k < e && d.Attrs[k].Style == a.Style && d.Attrs[k].Link == a.Link {
				k++
			}
			txt := string(d.Runes[j:k])
			for _, m := range []struct {
				s Style
				w string
			}{{Code, "`"}, {Strike, "~"}, {Underline, "+"}, {Italic, "_"}, {Bold, "*"}} {
				if a.Style&m.s != 0 {
					txt = m.w + txt + m.w
				}
			}
			if a.Link != "" {
				txt = "[" + txt + "](" + a.Link + ")"
			}
			b.WriteString(txt)
			j = k
		}
		out = append(out, b.String())
	}
	return out
}

func phCheck(t *testing.T, name string, d *Doc, want ...string) {
	t.Helper()
	got := phDump(d)
	if strings.Join(got, "\n") != strings.Join(want, "\n") {
		t.Errorf("%s:\n got %q\nwant %q", name, got, want)
	}
}

func TestDocFromHTMLLists(t *testing.T) {
	// Chrome/Notes-style nested lists, with the whitespace pretty HTML has.
	phCheck(t, "nested ul", docFromHTML(`<meta charset='utf-8'>
<ul>
  <li>one</li>
  <li>two
    <ul>
      <li>two a</li>
      <li>two b<ol><li>deep</li></ol></li>
    </ul>
  </li>
  <li>three</li>
</ul>`),
		"ul0 one", "ul0 two", "ul1 two a", "ul1 two b", "ol2 deep", "ul0 three")

	phCheck(t, "ol with p items and unclosed li", docFromHTML(`<ol><li><p>first</p><li><p>second</p></ol><p>after</p>`),
		"ol0 first", "ol0 second", "p after")

	// Text after a nested list continues the outer item's list.
	phCheck(t, "text after nested list", docFromHTML(`<ul><li>a<ul><li>b</li></ul>tail</li></ul>`),
		"ul0 a", "ul1 b", "ul0 tail")

	// Slack's own copy: nesting as sibling lists with data-indent.
	phCheck(t, "slack data-indent", docFromHTML(`<ul data-stringify-type="unordered-list" data-indent="0" class="p-rich_text_list"><li data-stringify-indent="0">top</li></ul><ul data-indent="1"><li>nested</li></ul><ol data-indent="0"><li>num</li></ol>`),
		"ul0 top", "ul1 nested", "ol0 num")

	// Google Docs: <b style="font-weight:normal"> wrapper, styles in spans.
	phCheck(t, "google docs", docFromHTML(`<meta charset="utf-8"><b style="font-weight:normal;" id="docs-internal-guid-1"><ul style="margin-top:0"><li dir="ltr" style="list-style-type:disc;"><p dir="ltr"><span style="font-weight:700;">Bold</span><span style="font-weight:400;"> and </span><span style="font-style:italic;">it</span></p></li><li style="margin-left:36pt"><p><span style="text-decoration:line-through;">gone</span></p></li></ul></b>`),
		"ul0 *Bold* and _it_", "ul0 ~gone~")

	// Word: list paragraphs with mso-list levels; the marker is dropped and
	// decides bullet vs number.
	phCheck(t, "word", docFromHTML(`<html><head><style><!-- p.MsoListParagraph {margin:0} span.x {font-weight:bold} --></style></head><body>
<p class=MsoListParagraphCxSpFirst style='text-indent:-18pt;mso-list:l0 level1 lfo1'><![if !supportLists]><span style='font-family:Symbol;mso-list:Ignore'>·<span style='font:7.0pt "Times New Roman"'>&nbsp;&nbsp; </span></span><![endif]>Alpha <span class=x>strong</span></p>
<p class=MsoListParagraphCxSpLast style='margin-left:72pt;mso-list:l0 level2 lfo1'><span style='mso-list:Ignore'>1.<span>&nbsp; </span></span>Beta</p>
</body></html>`),
		"ul0 Alpha *strong*", "ol1 Beta")

	// Writers that also spell the marker out (AppKit's RTF export, tab
	// bullets): the marker goes, the list stays.
	phCheck(t, "spelled-out markers", docFromHTML("<ul><li>\t•\tone</li><li>◦ two</li></ul><ol><li>1.\tthree</li><li>2026 plans</li></ol><p>• para stays</p>"),
		"ul0 one", "ul0 two", "ol0 three", "ol0 2026 plans", "p • para stays")

	// A stray <li> outside a list is still an item; depth caps at 5.
	deep := strings.Repeat("<ul><li>x", 9) + strings.Repeat("</li></ul>", 9)
	d := docFromHTML(deep)
	if d == nil || d.Lines[len(d.Lines)-1].Indent != edMaxIndent {
		t.Errorf("deep list: %q", phDump(d))
	}
	phCheck(t, "stray li", docFromHTML(`<li>loose</li>`), "ul0 loose")
}

func TestDocFromHTMLMarks(t *testing.T) {
	phCheck(t, "mixed marks", docFromHTML(`<p>a <b>bold <i>both</i></b> <s>s</s> <del>d</del> <code>c</code> <em>e</em> <strong>st</strong> <u>u</u></p>`),
		"p a *bold **_both_* ~s~ ~d~ `c` _e_ *st* +u+")
	phCheck(t, "links", docFromHTML(`<p>see <a href="https://example.com/x">the docs</a> or <a href="mailto:a@b.test">mail</a>, bare <a href="https://example.com">https://example.com</a></p>`),
		"p see [the docs](https://example.com/x) or [mail](mailto:a@b.test), bare https://example.com")
	phCheck(t, "bold link", docFromHTML(`<a href="https://e.test/"><b>bold</b> link</a>`),
		"p [*bold*](https://e.test/)[ link](https://e.test/)")
	phCheck(t, "code drops links", docFromHTML(`<a href="https://e.test/"><code>x</code></a>`), "p `x`")
	phCheck(t, "pre", docFromHTML("<p>before</p><pre>\nline 1\n  <b>line</b> 2\n\nline 4</pre><p>after</p>"),
		"p before", "pre line 1", "pre   line 2", "pre ", "pre line 4", "p after")
	phCheck(t, "slack pre", docFromHTML(`<pre class="c-mrkdwn__pre" data-stringify-type="pre"><div class="p-rich_text_block--no-overflow">go test ./...</div></pre>`),
		"pre go test ./...")
	phCheck(t, "quote", docFromHTML(`<blockquote>quoted <i>it</i><br>second</blockquote><p>out</p>`),
		"q quoted _it_", "q second", "p out")
	phCheck(t, "br and p", docFromHTML(`<div>one<br>two<br><br>four</div><p>p1</p><p>p2<br></p><p>p3</p>`),
		"p one", "p two", "p ", "p four", "p p1", "p p2", "p p3")
	phCheck(t, "headings bold", docFromHTML(`<h2>Title</h2><p>body</p>`), "p *Title*", "p body")
	phCheck(t, "whitespace", docFromHTML("  <p>  lots   of\n\tspace  </p>  <p>x&nbsp;&nbsp;y</p>"), "p lots of space", "p x  y")
	phCheck(t, "class css", docFromHTML(`<style>span.s1 {text-decoration: line-through} .b {font: bold 12px Helvetica}</style><p class="p1"><span class="s1">old</span> <span class="b">new</span></p>`),
		"p ~old~ *new*")
	phCheck(t, "slack emoji and mention", docFromHTML(`<p>hi <a href="https://x.slack.com/team/U1" data-stringify-type="mention">@karim</a> <img alt=":wave:" src="https://e.test/w.png" data-stringify-emoji=":wave:"> <img alt="😀"></p>`),
		"p hi @karim :wave: 😀")
	phCheck(t, "table", docFromHTML(`<table><tr><td>a</td><td>b</td></tr><tr><th>c</th></tr></table>`), "p a b", "p *c*")
}

func TestDocFromHTMLHostile(t *testing.T) {
	phCheck(t, "scripts and styles", docFromHTML(`<script>alert("x")</script><style>p{}</style><p>ok<script>document.cookie</script></p><noscript>ns</noscript><template>t</template><svg><text>s</text></svg>`),
		"p ok")
	phCheck(t, "bad links", docFromHTML(`<a href="javascript:alert(1)">js</a> <a href="JaVaScRiPt:x">js2</a> <a href="data:text/html,x">data</a> <a href="file:///etc/passwd">file</a> <a href="/rel">rel</a> <a href="slack://open">app</a> <a href="https://user:pw@e.test/">cred</a> <a href="https://e.test/a b">sp</a> <a href=" java&#x0A;script:x">nl</a> <a>none</a>`),
		"p js js2 data file rel app cred sp nl none")
	phCheck(t, "images and hidden", docFromHTML(`<p>a<img src="https://e.test/x.png" alt="a picture" onerror="x()"><span style="display:none">hidden</span><span hidden>h2</span><span style="visibility:hidden">h3</span>b</p>`),
		"p ab")
	phCheck(t, "controls", docFromHTML("<p>a\x00b\x07c\u0085d\uFFFCe</p>"), "p abcd e")
	phCheck(t, "comments and cdata", docFromHTML(`<p>a<!-- <b>no</b> -->b<![CDATA[c]]></p>`), "p ab")

	long := "https://e.test/" + strings.Repeat("a", phMaxURL)
	phCheck(t, "long url", docFromHTML(`<a href="`+long+`">x</a>`), "p x")

	// Deep nesting doesn't blow up; text survives.
	deep := strings.Repeat("<span><b>", 20000) + "deep" + strings.Repeat("</b></span>", 20000)
	if d := docFromHTML(deep); d == nil || d.Text() != "deep" || !d.HasStyle(0, 4, Bold) {
		t.Errorf("deep nesting: %q", phDump(d))
	}
	// Unbalanced and garbage markup.
	phCheck(t, "unbalanced", docFromHTML(`</b></ul><b>x</i></li>y`), "p *xy*")
	phCheck(t, "garbage", docFromHTML("<<<>>><p<<>a</p>"), "p <<<>>>a")

	// Over the caps: nil, so the caller pastes plain text.
	if d := docFromHTML(strings.Repeat("<p>x</p>", phMaxRunes)); d != nil {
		t.Errorf("over the rune cap: %d runes", d.Len())
	}
	if d := docFromHTML("<p>" + strings.Repeat("y", phMaxHTML) + "</p>"); d != nil {
		t.Error("over the input cap")
	}
	for _, empty := range []string{"", "<img src=x>", "<script>x</script>", "<p> </p>"} {
		if d := docFromHTML(empty); d != nil {
			t.Errorf("%q: %q, want nil", empty, phDump(d))
		}
	}
}

func TestDocFromPlainText(t *testing.T) {
	phCheck(t, "dashes", docFromPlainText("- one\n- two\n  - nested\n- three", true),
		"ul0 one", "ul0 two", "ul1 nested", "ul0 three")
	phCheck(t, "bullets and numbers", docFromPlainText("Groceries:\n• milk\n◦ eggs\n1. first\n2) second\n10. tenth", true),
		"p Groceries:", "ul0 milk", "ul0 eggs", "ol0 first", "ol0 second", "ol0 tenth")
	phCheck(t, "four-space and tab nesting", docFromPlainText("* a\n    * b\n\t\t* c", true),
		"ul0 a", "ul1 b", "ul2 c")
	phCheck(t, "crlf", docFromPlainText("- a\r\n- b\r\n", true), "ul0 a", "ul0 b", "p ")
	// Mid-line paste: the first line merges, so it stays text.
	phCheck(t, "mid-line", docFromPlainText("- a\n- b", false), "p - a", "ul0 b")
	for _, s := range []string{"plain text", "-not a list", "1.5 million", "2026-11-08", "- ", "*bold*", "100. ok but 1000. no", "a. letter"} {
		if d := docFromPlainText(s, true); d != nil && s != "100. ok but 1000. no" {
			t.Errorf("%q: %q, want nil", s, phDump(d))
		}
	}
	phCheck(t, "three digits", docFromPlainText("100. ok", true), "ol0 ok")
	if d := docFromPlainText("1000. no", true); d != nil {
		t.Errorf("four digits: %q", phDump(d))
	}
}

func TestClipPickTypes(t *testing.T) {
	cases := []struct {
		in   []string
		want string
	}{
		{[]string{"TARGETS", "MULTIPLE", "UTF8_STRING", "text/html", "text/plain"}, "text/html"},
		{[]string{"text/plain;charset=utf-8", "text/html;charset=utf-8"}, "text/html;charset=utf-8"},
		{[]string{"text/html;charset=utf-8", "text/html"}, "text/html"},
		{[]string{"UTF8_STRING", "STRING", "image/png"}, ""},
		{[]string{"chromium/x-web-custom-data", "text/_moz_htmlcontext"}, ""},
		{nil, ""},
	}
	for _, c := range cases {
		if got := clipPickHTMLType(c.in); got != c.want {
			t.Errorf("clipPickHTMLType(%q) = %q, want %q", c.in, got, c.want)
		}
	}
	mac := []struct {
		in   []string
		want string
		kind int
	}{
		{[]string{"public.utf8-plain-text", "public.html"}, "public.html", macHTML},
		{[]string{"public.rtf", "public.html", "public.utf8-plain-text"}, "public.html", macHTML},
		{[]string{"com.apple.flat-rtfd", "public.rtf"}, "public.rtf", macRTF},
		{[]string{"com.apple.flat-rtfd", "public.utf8-plain-text"}, "com.apple.flat-rtfd", macRTFD},
		{[]string{"Apple HTML pasteboard type"}, "Apple HTML pasteboard type", macHTML},
		{[]string{"public.utf8-plain-text", "public.png"}, "", macNone},
	}
	for _, c := range mac {
		if got, k := clipPickMacType(c.in); got != c.want || k != c.kind {
			t.Errorf("clipPickMacType(%q) = %q %d, want %q %d", c.in, got, k, c.want, c.kind)
		}
	}
}

func TestCFHTML(t *testing.T) {
	body := "<html><body>\r\n<!--StartFragment--><ul><li>a</li></ul><!--EndFragment-->\r\n</body></html>"
	hdr := "Version:0.9\r\nStartHTML:%010d\r\nEndHTML:%010d\r\nStartFragment:%010d\r\nEndFragment:%010d\r\nSourceURL:https://e.test/x\r\n"
	n := len(fmt.Sprintf(hdr, 0, 0, 0, 0))
	fs := strings.Index(body, "<ul>")
	fe := strings.Index(body, "<!--EndFragment-->")
	data := fmt.Sprintf(hdr, n, n+len(body), n+fs, n+fe) + body + "\x00"
	if got := string(cfHTML([]byte(data))); got != body {
		t.Errorf("cfHTML = %q", got)
	}
	phCheck(t, "cf_html", docFromHTML(string(cfHTML([]byte(data)))), "ul0 a")

	// StartHTML -1 (fragment only) and nonsense offsets.
	data2 := fmt.Sprintf("Version:1.0\r\nStartHTML:-1\r\nEndHTML:-1\r\nStartFragment:%010d\r\nEndFragment:%010d\r\n", 0, 0)
	frag := "<b>x</b>"
	data2 = strings.Replace(data2, fmt.Sprintf("StartFragment:%010d", 0), fmt.Sprintf("StartFragment:%010d", len(data2)), 1)
	data2 = strings.Replace(data2, fmt.Sprintf("EndFragment:%010d", 0), fmt.Sprintf("EndFragment:%010d", len(data2)+len(frag)), 1)
	data2 += frag
	if got := string(cfHTML([]byte(data2))); got != frag {
		t.Errorf("fragment only: %q", got)
	}
	bad := "Version:0.9\r\nStartHTML:99999\r\nEndHTML:5\r\n<p>y</p>"
	if got := string(cfHTML([]byte(bad))); got != "<p>y</p>" {
		t.Errorf("bad offsets: %q", got)
	}
}

func TestClipHTMLText(t *testing.T) {
	le := []byte{0xff, 0xfe, '<', 0, 'b', 0, '>', 0, 0xe9, 0, '<', 0}
	if got := clipHTMLText(le); got != "<b>é<" {
		t.Errorf("utf-16le: %q", got)
	}
	be := []byte{0xfe, 0xff, 0, '<', 0, 'p'}
	if got := clipHTMLText(be); got != "<p" {
		t.Errorf("utf-16be: %q", got)
	}
	if got := clipHTMLText([]byte("<p>a</p>")); got != "<p>a</p>" {
		t.Errorf("utf-8: %q", got)
	}
	if got := clipHTMLText([]byte("\xef\xbb\xbf<i>\xff</i>")); got != "<i>\uFFFD</i>" {
		t.Errorf("bom + invalid: %q", got)
	}
}

func TestPasteHTMLIntoEditor(t *testing.T) {
	list := `<ul><li><b>one</b></li><li>two<ul><li>deep</li></ul></li></ul>`
	plain := "one\ntwo\ndeep"

	// Into an empty composer: the list as is.
	e := NewEditor()
	e.pasteHTML(plain, list)
	phCheck(t, "empty", e.doc, "ul0 *one*", "ul0 two", "ul1 deep")
	if e.caret != e.doc.Len() {
		t.Errorf("caret %d, want %d", e.caret, e.doc.Len())
	}
	// Sent as a Slack rich_text list.
	if js := string(e.doc.RichTextBlocks()); !strings.Contains(js, `"rich_text_list"`) || !strings.Contains(js, `"indent":1`) {
		t.Errorf("blocks: %s", js)
	}

	// Mid-paragraph: the line splits and the list starts on its own line.
	e = NewEditor()
	edType(e, "see:")
	e.pasteHTML(plain, list)
	phCheck(t, "mid-line", e.doc, "p see:", "ul0 *one*", "ul0 two", "ul1 deep")

	// Into a nested list item: the pasted list nests under it.
	e = NewEditor()
	e.pasteHTML("x", `<ul><li>a<ul><li>b</li></ul></li></ul>`)
	e.newline()
	e.pasteHTML(plain, list)
	phCheck(t, "into list", e.doc, "ul0 a", "ul1 b", "ul1 *one*", "ul1 two", "ul2 deep")

	// Paste and match style: plain text, no list.
	e = NewEditor()
	e.plainPaste = true
	e.pasteHTML("- a\n- b", list)
	phCheck(t, "match style", e.doc, "p - a", "p - b")

	// Inside a code block: plain text.
	e = NewEditor()
	e.ToggleBlock(CodeBlock)
	e.pasteHTML(plain, list)
	phCheck(t, "code block", e.doc, "pre one", "pre two", "pre deep")

	// No HTML: a plain-text list still becomes a list; ordinary text doesn't.
	e = NewEditor()
	e.paste("- a\n- b")
	phCheck(t, "plain list", e.doc, "ul0 a", "ul0 b")
	e = NewEditor()
	edType(e, "x ")
	e.paste("1. a")
	phCheck(t, "plain mid-line", e.doc, "p x 1. a")

	// HTML that converts to nothing falls back to the text.
	e = NewEditor()
	e.pasteHTML("pic", `<img src="https://e.test/p.png">`)
	phCheck(t, "image only", e.doc, "p pic")

	// Undo removes the whole paste.
	e = NewEditor()
	e.pasteHTML(plain, list)
	e.Undo()
	if !e.doc.Empty() || len(e.doc.Lines) != 1 || e.doc.Lines[0].Kind != Para {
		t.Errorf("undo: %q", phDump(e.doc))
	}
}

func TestPasteLinkTransforms(t *testing.T) {
	old := LinkTransforms
	defer func() { LinkTransforms = old }()
	LinkTransforms = func(u string) string { return strings.Replace(u, "?utm=1", "", 1) }
	e := NewEditor()
	e.pasteHTML("x", `<a href="https://e.test/a?utm=1">x</a>`)
	phCheck(t, "transformed", e.doc, "p [x](https://e.test/a)")
}
