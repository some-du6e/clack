package fakeslack

import (
	"strconv"
	"strings"

	"github.com/some-du6e/clack/internal/slack"
)

// #files-gallery's snippets as Slack's server sends them: the first lines
// as CodeMirror's static highlight (preview_highlight; lines_more, the
// rest), and the whole of it from files.info (content and
// content_highlight_html at the response's top level, beside "file";
// content_highlight_html_truncated when the server cut it). The code is
// synthetic; the markup is CodeMirror's python mode's token classes in
// Slack's server wrapper.

// cmServer wraps highlighted lines as Slack's server does (an empty line
// holds a zero-width space).
func cmServer(lines []string) string {
	var b strings.Builder
	b.WriteString(`<div class="CodeMirror cm-s-default CodeMirrorServer" oncopy="if(event.clipboardData){event.clipboardData.setData('text/plain',window.getSelection().toString().replace(/​/g,''));event.preventDefault();event.stopPropagation();}">` + "\n")
	b.WriteString(`<div class="CodeMirror-code">` + "\n")
	for _, l := range lines {
		if l == "" {
			l = "&#8203;"
		}
		b.WriteString("<div><pre>" + l + "</pre></div>\n")
	}
	b.WriteString("</div>\n</div>\n")
	return b.String()
}

// cmTok is one CodeMirror token span.
func cmTok(class, s string) string { return `<span class="cm-` + class + `">` + s + `</span>` }

// cmPlain is the lines' text (tags dropped, entities read).
func cmPlain(lines []string) string {
	var out []string
	for _, l := range lines {
		var b strings.Builder
		in := false
		for _, r := range l {
			switch {
			case r == '<':
				in = true
			case r == '>':
				in = false
			case !in:
				b.WriteRune(r)
			}
		}
		t := strings.NewReplacer("&lt;", "<", "&gt;", ">", "&quot;", `"`, "&#39;", "'", "&amp;", "&").Replace(b.String())
		out = append(out, t)
	}
	return strings.Join(out, "\n")
}

// fibLines is fib.py, highlighted.
func fibLines() []string {
	k, d, v, o, n, bi := func(s string) string { return cmTok("keyword", s) }, func(s string) string { return cmTok("def", s) },
		func(s string) string { return cmTok("variable", s) }, func(s string) string { return cmTok("operator", s) },
		func(s string) string { return cmTok("number", s) }, func(s string) string { return cmTok("builtin", s) }
	return []string{
		k("def") + " " + d("fib") + "(" + v("n") + "):",
		"    " + v("a") + ", " + v("b") + " " + o("=") + " " + n("0") + ", " + n("1"),
		"    " + k("for") + " " + v("_") + " " + k("in") + " " + bi("range") + "(" + v("n") + "):",
		"        " + v("a") + ", " + v("b") + " " + o("=") + " " + v("b") + ", " + v("a") + " " + o("+") + " " + v("b"),
		"    " + k("return") + " " + v("a"),
		"",
		bi("print") + "(" + v("fib") + "(" + n("10") + "))",
	}
}

// longLines is the viewer's own 40-line script, highlighted.
func longLines() []string {
	k, d, v, o, n, s, c, m, p := func(s string) string { return cmTok("keyword", s) }, func(s string) string { return cmTok("def", s) },
		func(s string) string { return cmTok("variable", s) }, func(s string) string { return cmTok("operator", s) },
		func(s string) string { return cmTok("number", s) }, func(s string) string { return cmTok("string", s) },
		func(s string) string { return cmTok("comment", s) }, func(s string) string { return cmTok("meta", s) },
		func(s string) string { return cmTok("property", s) }
	out := []string{
		c("# Synthetic fixture: batch the queue and report."),
		k("import") + " " + v("json"),
		k("from") + " " + v("dataclasses") + " " + k("import") + " " + v("dataclass"),
		"",
		"",
		m("@dataclass"),
		k("class") + " " + d("Job") + ":",
		"    " + v("name") + ": " + v("str"),
		"    " + v("size") + ": " + v("int") + " " + o("=") + " " + n("0"),
		"",
		"",
		k("def") + " " + d("batch") + "(" + v("jobs") + ", " + v("limit") + o("=") + n("8") + "):",
		"    " + s(`"""Group jobs so no batch passes the limit."""`),
		"    " + v("out") + ", " + v("cur") + " " + o("=") + " [], []",
		"    " + k("for") + " " + v("job") + " " + k("in") + " " + v("jobs") + ":",
		"        " + k("if") + " " + v("len") + "(" + v("cur") + ") " + o("&gt;=") + " " + v("limit") + ":",
		"            " + v("out") + "." + p("append") + "(" + v("cur") + ")",
		"            " + v("cur") + " " + o("=") + " []",
		"        " + v("cur") + "." + p("append") + "(" + v("job") + ")",
		"    " + k("if") + " " + v("cur") + ":",
		"        " + v("out") + "." + p("append") + "(" + v("cur") + ")",
		"    " + k("return") + " " + v("out"),
		"",
	}
	for i := len(out); i < 40; i++ {
		out = append(out, v("report")+"("+s(`"step `+strconv.Itoa(i)+`"`)+", "+n(strconv.Itoa(i*3))+")  "+c("# &lt;- logged"))
	}
	return out
}

// snippetContent is files.info's top-level content of the snippets that
// have more than their preview: content, content_highlight_html and
// whether the server cut it.
type snippetContent struct {
	content, html string
	truncated     bool
}

// snippetContents are the #files-gallery snippets' files.info contents.
func snippetContents() map[string]snippetContent {
	long := longLines()
	return map[string]snippetContent{
		"F0FILESSNP02": {content: cmPlain(long), html: cmServer(long), truncated: true},
	}
}

// longSnippet is the viewer's own 40-line snippet: 12 lines of preview.
func (s *Server) longSnippet() slack.File {
	long := longLines()
	pre := long[:12]
	return slack.File{ID: "F0FILESSNP02", Name: "batch.py", Title: "batch.py", Mimetype: "text/plain", Filetype: "python",
		PrettyType: "Python", Size: int64(len(cmPlain(long))), Mode: "snippet", Lines: len(long), LinesMore: len(long) - len(pre),
		Preview: cmPlain(pre), PreviewHighlight: cmServer(pre), User: SelfID,
		URLPrivate: s.URL + "files-pri/" + TeamID + "-F0FILESSNP02/batch.py", Permalink: s.URL + "files/F0FILESSNP02/batch.py",
		EditLink: s.URL + "files/F0FILESSNP02/batch.py/edit"}
}
