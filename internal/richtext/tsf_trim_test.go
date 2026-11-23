package richtext

// Slack's mrkdwn renderer trims its text first (TSF: `var Z=r.trim(H);
// if(0==Z.length)return""`), both ends, of isSpace's characters: char
// codes 9 to 13 and 32, not U+00A0. A section or context element whose
// text trims to nothing keeps its box, empty. Synthetic text.

import (
	"strings"
	"testing"
)

func trimText(ss []Span) string {
	var b strings.Builder
	for _, s := range ss {
		switch s.Kind {
		case Break:
			b.WriteString("⏎")
		default:
			b.WriteString(s.Text)
		}
	}
	return b.String()
}

func TestMrkdwnTrimLikeTSF(t *testing.T) {
	for _, c := range []struct{ in, want string }{
		{"\nhello", "hello"},
		{"hello\n\n", "hello"},
		{" \t\r\n\v\fhello there\n \t", "hello there"},
		{"line one\nline two\n", "line one⏎line two"},
		{"\u00a0kept\u00a0", "\u00a0kept\u00a0"}, // not isSpace
		{"\u2003em space\u2003", "\u2003em space\u2003"},
	} {
		if got := trimText(flatten(FromMrkdwn(testNames{}, c.in))); got != c.want {
			t.Errorf("mrkdwn %q renders %q, want %q", c.in, got, c.want)
		}
	}
	for _, in := range []string{"\n", " \n\t ", "\r\n\r\n"} {
		if got := FromMrkdwn(testNames{}, in); got != nil {
			t.Errorf("mrkdwn %q renders %d blocks, want none", in, len(got))
		}
	}
}

// A section and a context whose mrkdwn is only "\n": both blocks stay
// (Slack draws their boxes, empty), the actions between them too.
func TestNewlineOnlyBlocksKeptLikeSlack(t *testing.T) {
	d := FromMessage(testNames{}, msg(t, `{"ts":"1.0","text":"x","blocks":[
		{"type":"section","block_id":"s1","text":{"type":"mrkdwn","text":"\n"}},
		{"type":"actions","block_id":"a1","elements":[{"type":"button","action_id":"b1","text":{"type":"plain_text","text":"Go"}}]},
		{"type":"context","block_id":"c1","elements":[{"type":"mrkdwn","text":"\n"}]},
		{"type":"section","block_id":"s2","text":{"type":"mrkdwn","text":""}}]}`))
	var kinds []BlockKind
	var sec, ctx *Block
	for i := range d.Blocks {
		b := &d.Blocks[i]
		kinds = append(kinds, b.Kind)
		switch {
		case b.Kind == Section && sec == nil:
			sec = b
		case b.Kind == Context:
			ctx = b
		}
	}
	if sec == nil {
		t.Errorf("the section whose text is \"\\n\" was dropped (blocks %v)", kinds)
	} else if len(sec.Spans) != 0 || len(sec.Children) != 0 {
		t.Errorf("the section whose text is \"\\n\" draws %q", trimText(sec.Spans))
	}
	if ctx == nil {
		t.Errorf("the context whose text is \"\\n\" was dropped (blocks %v)", kinds)
	} else if len(ctx.Spans) != 0 {
		t.Errorf("the context whose text is \"\\n\" draws %q", trimText(ctx.Spans))
	}
}
