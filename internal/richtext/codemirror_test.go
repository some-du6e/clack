package richtext

import (
	"crypto/sha256"
	"encoding/hex"
	"strconv"
	"strings"
	"testing"
)

// TestTruncateHTML holds the port to Slack's html-truncate (gantry-v2-vendors
// LvCQ): every expected value is what that module returned in node for
// the same input (/root/clack-ops/scratch/file-pillows/harness), its
// quirks included: newlines between tags count, "@" counts as "&#64;", a
// cut splits an entity, a closing tag after a self-closing one is written
// as that one again, and a URL is kept whole only when no tag follows it.
func TestTruncateHTML(t *testing.T) {
	for _, c := range []struct {
		name, in string
		n        int
		out      string
		cut      bool
	}{
		{"plain short", "hello world", 20, "hello world", false},
		{"plain long", "abcdefghijklmnopqrstuvwxyz", 10, "abcdefghij...", true},
		{"exact", "abcdefghij", 10, "abcdefghij", false},
		{"one over", "abcdefghijk", 10, "abcdefghij...", true},
		{"html short", "<div class=\"CodeMirror cm-s-default CodeMirrorServer\">\n<div class=\"CodeMirror-code\">\n<div><pre>a</pre></div>\n<div><pre>b</pre></div>\n</div>\n</div>\n", 50, "<div class=\"CodeMirror cm-s-default CodeMirrorServer\">\n<div class=\"CodeMirror-code\">\n<div><pre>a</pre></div>\n<div><pre>b</pre></div>\n</div>\n</div>\n", false},
		{"html cut in text", "<div class=\"CodeMirror cm-s-default CodeMirrorServer\">\n<div class=\"CodeMirror-code\">\n<div><pre>xxxxxxxxxxxxxxxxxxxxxxxxxxxxxx</pre></div>\n<div><pre>yyyyyyyyyyyyyyyyyyyyyyyyyyyyyy</pre></div>\n</div>\n</div>\n", 40, "<div class=\"CodeMirror cm-s-default CodeMirrorServer\">\n<div class=\"CodeMirror-code\">\n<div><pre>xxxxxxxxxxxxxxxxxxxxxxxxxxxxxx</pre></div>\n<div><pre>yyyyyyy...</pre></div></div></div>", true},
		{"html cut in span", "<div class=\"CodeMirror cm-s-default CodeMirrorServer\">\n<div class=\"CodeMirror-code\">\n<div><pre><span class=\"cm-keyword\">def</span> ffffffffffffffffffffffffffffffffffffffff(<span class=\"cm-keyword\">return</span>)</pre></div>\n</div>\n</div>\n", 20, "<div class=\"CodeMirror cm-s-default CodeMirrorServer\">\n<div class=\"CodeMirror-code\">\n<div><pre><span class=\"cm-keyword\">def</span> ffffffffffffff...</pre></div></div></div>", true},
		{"at sign", "<div class=\"CodeMirror cm-s-default CodeMirrorServer\">\n<div class=\"CodeMirror-code\">\n<div><pre>user@example.com is here zzzzzzzzzzzzzzzzzzzzzzzzzzzzzzzzzzzzzzzz</pre></div>\n</div>\n</div>\n", 30, "<div class=\"CodeMirror cm-s-default CodeMirrorServer\">\n<div class=\"CodeMirror-code\">\n<div><pre>user&#64;example.com is here...</pre></div></div></div>", true},
		{"entities", "<div class=\"CodeMirror cm-s-default CodeMirrorServer\">\n<div class=\"CodeMirror-code\">\n<div><pre>a &lt; b &amp;&amp; c &gt; d qqqqqqqqqqqqqqqqqqqqqqqqqqqqqqqqqqqqqqqq</pre></div>\n</div>\n</div>\n", 25, "<div class=\"CodeMirror cm-s-default CodeMirrorServer\">\n<div class=\"CodeMirror-code\">\n<div><pre>a &lt; b &amp;&amp; c &...</pre></div></div></div>", true},
		{"url text", "see https://example.com/a/b?x=1 and mail me@host.org then more text after it all", 20, "see https://example.com/a/b?x=1...", true},
		{"url early", "go https://example.com/a/b/c/d/e/f/g/h now", 10, "go https://example.com/a/b/c/d/e/f/g/h now", false},
		{"rest short", "<div class=\"CodeMirror cm-s-default CodeMirrorServer\">\n<div class=\"CodeMirror-code\">\n<div><pre>aaaaaaaaaaaa</pre></div>\n</div>\n</div>\n", 10, "<div class=\"CodeMirror cm-s-default CodeMirrorServer\">\n<div class=\"CodeMirror-code\">\n<div><pre>aaaaaaaa...</pre></div></div></div>", true},
		{"self closing", "<div>one<br/>two</div><p>xxxxxxxxxxxxxxxxxxxxxxxxxxxxxx</p>", 10, "<div>one<br/>two<br/><p>xxxx...</p>", true},
		{"unicode", "<div class=\"CodeMirror cm-s-default CodeMirrorServer\">\n<div class=\"CodeMirror-code\">\n<div><pre>héllo wörld 日本語のテキスト日本語のテキスト日本語のテキスト日本語のテキスト 🎉🎉🎉 end</pre></div>\n</div>\n</div>\n", 30, "<div class=\"CodeMirror cm-s-default CodeMirrorServer\">\n<div class=\"CodeMirror-code\">\n<div><pre>héllo wörld 日本語のテキスト日本語のテキスト...</pre></div></div></div>", true},
		{"emoji pair cut", "<div class=\"CodeMirror cm-s-default CodeMirrorServer\">\n<div class=\"CodeMirror-code\">\n<div><pre>ab🎉cdeeeeeeeeeeeeeeeeeeee</pre></div>\n</div>\n</div>\n", 5, "<div class=\"CodeMirror cm-s-default CodeMirrorServer\">\n<div class=\"CodeMirror-code\">\n<div><pre>ab\uFFFD...</pre></div></div></div>", true},
	} {
		out, cut := TruncateHTML(c.in, c.n)
		if out != c.out || cut != c.cut {
			t.Errorf("%s: TruncateHTML(…, %d) = %q, %v; want %q, %v", c.name, c.n, out, cut, c.out, c.cut)
		}
	}
	// 600 highlighted lines: Slack's 12,800 limit, as computePreviewsForSnippet cuts them
	var lines []string
	for i := 0; i < 600; i++ {
		lines = append(lines, `<span class="cm-keyword">let</span> v`+strconv.Itoa(i)+` = <span class="cm-string">"`+strings.Repeat("abc", 3)+`"</span>; <span class="cm-comment">// line `+strconv.Itoa(i)+`</span>`)
	}
	big := cmDoc(lines...)
	if len(big) != 91679 {
		t.Fatalf("the big input is %d long, want 91679 (as in node)", len(big))
	}
	out, cut := TruncateHTML(big, SnippetDisplayMax)
	sum := sha256.Sum256([]byte(out))
	if !cut || len(out) != 55220 || hex.EncodeToString(sum[:]) != "dcb2327583bfd6c4299ba09163ee5b177799d34468175cb912bbf096aae350db" {
		t.Errorf("12,800: cut %v, %d long (want 55220), tail %q", cut, len(out), out[max(0, len(out)-160):])
	}
}

// cmDoc is Slack's server highlight around lines (each line's HTML).
func cmDoc(lines ...string) string {
	var b strings.Builder
	b.WriteString("<div class=\"CodeMirror cm-s-default CodeMirrorServer\">\n<div class=\"CodeMirror-code\">\n")
	for i, l := range lines {
		if i > 0 {
			b.WriteString("\n")
		}
		b.WriteString("<div><pre>" + l + "</pre></div>")
	}
	b.WriteString("\n</div>\n</div>\n")
	return b.String()
}

// TestParseCodeMirror reads Slack's server highlight: one line per div of
// .CodeMirror-code, the text of its pre with the spans' classes, entities
// as their characters, the zero-width space of an empty line dropped, the
// root's theme class kept.
func TestParseCodeMirror(t *testing.T) {
	src := `<div class="CodeMirror cm-s-default CodeMirrorServer" oncopy="if(event.clipboardData){event.clipboardData.setData('text/plain',window.getSelection().toString().replace(/​/g,''));event.preventDefault();event.stopPropagation();}">
<div class="CodeMirror-code">
<div><pre><span class="cm-keyword">def</span> <span class="cm-def">fib</span>(<span class="cm-variable">n</span>):</pre></div>
<div><pre>    <span class="cm-keyword">return</span> <span class="cm-string">"a &lt; b &amp; c"</span> <span class="cm-comment"># x</span></pre></div>
<div><pre>&#8203;</pre></div>
<div><pre><span class="cm-variable cm-property">obj</span><span class="cm-string"><span class="cm-link">http://x</span></span></pre></div>
</div>
</div>
`
	cm := ParseCodeMirror(src)
	if cm == nil {
		t.Fatal("no code")
	}
	if strings.Join(cm.Root, " ") != "CodeMirror cm-s-default CodeMirrorServer" || cm.CSV {
		t.Errorf("root %q csv %v", cm.Root, cm.CSV)
	}
	if len(cm.Lines) != 4 {
		t.Fatalf("%d lines, want 4: %+v", len(cm.Lines), cm.Lines)
	}
	want := []string{`def fib(n):`, `    return "a < b & c" # x`, ``, `objhttp://x`}
	for i, w := range want {
		if got := cm.Lines[i].Plain(); got != w {
			t.Errorf("line %d %q, want %q", i+1, got, w)
		}
	}
	l0 := cm.Lines[0]
	if len(l0) != 6 || l0[0].Text != "def" || strings.Join(l0[0].Classes, "|") != "cm-keyword" || l0[1].Text != " " || len(l0[1].Classes) != 0 ||
		l0[2].Text != "fib" || l0[2].Classes[0] != "cm-def" || l0[5].Text != "):" {
		t.Errorf("line 1 runs %+v", l0)
	}
	if l3 := cm.Lines[3]; len(l3) != 2 || strings.Join(l3[0].Classes, "|") != "cm-variable cm-property" || strings.Join(l3[1].Classes, "|") != "cm-string|cm-link" {
		t.Errorf("line 4 runs %+v", l3)
	}
	if cm.Plain() != strings.Join(want, "\n") {
		t.Errorf("plain %q", cm.Plain())
	}
	// a newline straight after <pre> isn't text; whitespace between the
	// line's div and its pre isn't either
	cm = ParseCodeMirror("<div class=\"CodeMirror\"><div class=\"CodeMirror-code\"><div>\n<pre>\nx  y</pre>\n</div></div></div>")
	if cm == nil || len(cm.Lines) != 1 || cm.Lines[0].Plain() != "x  y" {
		t.Errorf("pre newline: %+v", cm)
	}
	if ParseCodeMirror("") != nil || ParseCodeMirror("plain text") != nil {
		t.Error("a non-CodeMirror string parsed")
	}
}

type expResolver struct {
	Resolver
	exp, going bool
	code       *CodeMirror
	cut        bool
	keys       []string
}

func (r *expResolver) FileExpansion(ch, ts, id string) (bool, bool) {
	r.keys = append(r.keys, ch+"/"+ts+"/"+id)
	return r.exp, r.going
}

func (r *expResolver) SnippetContent(id string) (*CodeMirror, bool, bool) {
	return r.code, r.cut, r.code != nil
}

// TestSnippetFields: a file whose mode is "snippet" is a snippet with its
// highlight, lines_more, preview_is_truncated and edit_link; an uploaded
// text file (mode "hosted") isn't; the message's expandable and the
// fetched content come from the resolver (core's store), by channel,
// message and file.
func TestSnippetFields(t *testing.T) {
	js := `{"ts":"1700000000.000100","text":"","files":[
		{"id":"F1","name":"a.py","title":"a.py","mimetype":"text/plain","filetype":"python","mode":"snippet","lines":9,"lines_more":3,
		 "preview":"a = 1","preview_highlight":"<div class=\"CodeMirror cm-s-default CodeMirrorServer\"><div class=\"CodeMirror-code\"><div><pre><span class=\"cm-variable\">a</span> = 1</pre></div></div></div>",
		 "preview_is_truncated":true,"edit_link":"https://ws.slack.com/files/U1/F1/a.py/edit","url_private":"https://files.slack.com/files-pri/T1-F1/a.py"},
		{"id":"F2","name":"notes.txt","mimetype":"text/plain","filetype":"text","mode":"hosted","preview":"hello","lines":1,
		 "url_private":"https://files.slack.com/files-pri/T1-F2/notes.txt"}]}`
	m := msg(t, js)
	r := &expResolver{Resolver: R, exp: true, code: ParseCodeMirror("<div class=\"CodeMirror\"><div class=\"CodeMirror-code\"><div><pre>x</pre></div><div><pre>y</pre></div></div></div>"), cut: true}
	d := FromMessageIn(r, m, "C1")
	if len(d.Blocks) != 2 {
		t.Fatalf("%d blocks", len(d.Blocks))
	}
	sn, txt := d.Blocks[0].File, d.Blocks[1].File
	if !sn.IsSnippet || sn.Highlight == nil || len(sn.Highlight.Lines) != 1 || sn.Highlight.Lines[0].Plain() != "a = 1" ||
		sn.LinesMore != 3 || !sn.PreviewTruncated || sn.EditLink != "https://ws.slack.com/files/U1/F1/a.py/edit" || sn.Lines != 9 {
		t.Errorf("snippet %+v", sn)
	}
	if !sn.Expanded || sn.Expanding || sn.Content == nil || len(sn.Content.Lines) != 2 || !sn.ContentTruncated {
		t.Errorf("snippet's expandable: expanded %v content %+v truncated %v", sn.Expanded, sn.Content, sn.ContentTruncated)
	}
	if len(r.keys) != 1 || r.keys[0] != "C1/1700000000.000100/F1" {
		t.Errorf("expandable keys %v", r.keys)
	}
	if txt.IsSnippet || txt.Preview != "" || txt.Highlight != nil {
		t.Errorf("a hosted text file %+v", txt)
	}
}
