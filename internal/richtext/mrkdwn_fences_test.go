package richtext

import (
	"strings"
	"testing"
)

// fenceShape is bs as "p[...] pre[...] q[...]", each block's text with
// its breaks as newlines.
func fenceShape(bs []Block) string {
	var parts []string
	for _, b := range bs {
		var s strings.Builder
		for _, sp := range b.Spans {
			if sp.Kind == Break {
				s.WriteString("\n")
				continue
			}
			s.WriteString(sp.Text)
		}
		k := map[BlockKind]string{Paragraph: "p", Preformatted: "pre", Quote: "q"}[b.Kind]
		if k == "" {
			k = "?"
		}
		parts = append(parts, k+"["+s.String()+"]")
	}
	return strings.Join(parts, " ")
}

// ``` blocks as Slack's formatter finds them (TSF's pre_rx, gantry-v2-
// vendors, build 031dbe6a7400): a ``` after the text's start, whitespace
// or one of _*?.,-!^;:{([%$#+=" opens one; the first ``` after it with
// the end, whitespace or one of _*?.,-!^;:})]%$#+=…" after it closes it;
// a block of only whitespace keeps its backticks, as do fences without
// those edges or without a close; a <…> token before a fence is an edge
// (TSF's placeholder ends in "-"), and only ASCII whitespace is blank.
// Karim (2026-11-18): an app's "Inspect message" modal, "Here's the raw
// JSON from that message: ```{ …".
func TestMrkdwnFences(t *testing.T) {
	for _, c := range []struct{ name, in, want string }{
		{"after text", "Here's the raw JSON from that message: ```{\n  \"type\": \"message\",\n  \"text\": \"hi\"\n}```",
			"p[Here's the raw JSON from that message: ] pre[{\n  \"type\": \"message\",\n  \"text\": \"hi\"\n}]"},
		{"own lines", "before\n```\na\nb\n```\nafter", "p[before] pre[a\nb] p[after]"},
		{"brackets", "(```a```)", "p[(] pre[a] p[)]"},
		{"in a word", "a```b```c", "p[a```b```c]"},
		{"letter after the close", "x ```y```z", "p[x ```y```z]"},
		{"closes at the edged fence", "```a```b ```c```", "pre[a```b ```c]"},
		{"only spaces", "``` ```", "p[``` ```]"},
		{"empty", "``````", "p[``````]"},
		{"spaces, then a block", "``` ``` ```x```", "p[``` ``` ] pre[x]"},
		{"unterminated", "look: ```{ \"a\": 1", "p[look: ```{ \"a\": 1]"},
		{"two", "```a```\n```b```", "pre[a] pre[b]"},
		// a <…> token is a placeholder ending in "-" when pre_rx runs
		{"after a mention", "<@U1>```code```", "p[@alice] pre[code]"},
		{"after a link", "<https://x.example|log>```a```", "p[log] pre[a]"},
		// StringTools.trim keeps a no-break space; JS's \s has U+FEFF
		{"no-break space block", "```\u00a0```", "pre[\u00a0]"},
		{"U+FEFF edge", "a\ufeff```x```", "p[a\ufeff] pre[x]"},
	} {
		got := FromMrkdwn(R, c.in)
		if s := fenceShape(got); s != c.want {
			t.Errorf("%s: %q\n got %q\nwant %q", c.name, c.in, s, c.want)
		}
		checkInvariants(t, c.name, got)
	}
}
