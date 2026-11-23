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

// A legacy attachment whose text is only spaces: Slack's
// MessageAttachmentGeneric draws it when Dv89's hasFooter holds (footer,
// footer_icon or a ts make it renderable), without its fallback (a set
// text rules the fallback div out), and draws nothing when nothing else is
// set.
func TestBlankAttachmentTextLikeSlack(t *testing.T) {
	atts := func(d Doc) []*Attachment {
		var out []*Attachment
		for i := range d.Blocks {
			if b := &d.Blocks[i]; b.Kind == AttachmentBlock && b.Att != nil {
				out = append(out, b.Att)
			}
		}
		return out
	}
	for _, c := range []struct {
		name, att string
		drawn     bool
		footer    string
	}{
		{"spaces and a footer", `{"color":"#2eb67d","text":"  ","footer":"Deploybot"}`, true, "Deploybot"},
		{"spaces, a footer and a fallback", `{"color":"#2eb67d","text":" \t","footer":"Deploybot","fallback":"synthetic fallback"}`, true, "Deploybot"},
		{"spaces and a footer icon", `{"text":"  ","footer_icon":"https://example.com/icon.png"}`, true, ""},
		{"spaces and a ts", `{"text":"  ","ts":1700000000}`, true, ""},
		{"spaces alone", `{"color":"#2eb67d","text":"  "}`, false, ""},
		{"spaces and a fallback", `{"text":"  ","fallback":"synthetic fallback"}`, false, ""},
	} {
		t.Run(c.name, func(t *testing.T) {
			d := FromMessage(testNames{}, msg(t, `{"ts":"1.0","text":"","attachments":[`+c.att+`]}`))
			as := atts(d)
			if !c.drawn {
				if len(as) != 0 {
					t.Errorf("drawn (%d attachments); Slack's Dv89 finds nothing to render", len(as))
				}
				return
			}
			if len(as) != 1 {
				t.Fatalf("%d attachments, want the one Slack renders", len(as))
			}
			a := as[0]
			if got := trimText(flatten(a.Text)); got != "" {
				t.Errorf("the attachment draws the text %q; Slack draws none (no fallback while text is set)", got)
			}
			if got := trimText(a.Footer); got != c.footer {
				t.Errorf("footer %q, want %q", got, c.footer)
			}
		})
	}
}

// An element whose text formats to nothing keeps its box in a mixed
// context (Context renders a p-context_block_element for every element,
// the 16px spacing after each but the last).
func TestEmptyContextElementKeepsItsBoxLikeSlack(t *testing.T) {
	d := FromMessage(testNames{}, msg(t, `{"ts":"1.0","text":"x","blocks":[
		{"type":"context","block_id":"c1","elements":[{"type":"mrkdwn","text":" "},{"type":"mrkdwn","text":"a synthetic footnote"}]}]}`))
	var ctx *Block
	for i := range d.Blocks {
		if d.Blocks[i].Kind == Context {
			ctx = &d.Blocks[i]
		}
	}
	if ctx == nil {
		t.Fatal("no context block")
	}
	if len(ctx.Children) != 2 || len(ctx.Children[0].Spans) != 0 || trimText(ctx.Children[1].Spans) != "a synthetic footnote" {
		t.Errorf("the context's elements are %d (%+v); Slack's draws two, the first empty", len(ctx.Children), ctx.Children)
	}
}
