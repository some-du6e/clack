package richtext

import (
	"encoding/json"
	"testing"

	"github.com/some-du6e/clack/internal/slack"
)

// SimpleMessagePreview's text per message shape (cjey, QnfD truncateBlocks
// with fEfI isContentBlock, zqto, c7LH, l8BP; build fa938a086569). The
// agent footer is Karim's report (2026-11-12): Activity showed "done in
// under a" (the context line, cut) where Slack shows the message's text.
func TestPreviewFromMessage(t *testing.T) {
	ctx := `{"type":"context","elements":[{"type":"mrkdwn","text":"_done in under a second_"}]}`
	fb := `{"type":"context_actions","elements":[{"type":"feedback_buttons","action_id":"f",` +
		`"positive_button":{"text":{"type":"plain_text","text":"Good response"},"value":"up"},` +
		`"negative_button":{"text":{"type":"plain_text","text":"Bad response"},"value":"down"}}]}`
	rich := func(s string) string {
		b, _ := json.Marshal(s)
		return `{"type":"rich_text","elements":[{"type":"rich_text_section","elements":[{"type":"text","text":` + string(b) + `}]}]}`
	}
	unfurl := []slack.Attachment{{IsMsgUnfurl: true, FromURL: "https://x.slack.com/archives/C1/p1", Text: "the shared message"}}
	for _, tc := range []struct {
		name, text, blocks string
		atts               []slack.Attachment
		subtype            string
		want               string
		ok                 bool
	}{
		{name: "agent footer: context and feedback buttons", text: "done in under a second", blocks: "[" + ctx + "," + fb + "]",
			want: "done in under a second", ok: true},
		{name: "rich text before a context line", text: "fallback", blocks: "[" + rich("pong") + "," + ctx + "," + fb + "]",
			want: "pong", ok: true},
		{name: "context line first", text: "fallback", blocks: "[" + ctx + "," + rich("the answer") + "]", want: "the answer", ok: true},
		{name: "markdown block (chat.update markdown_text)", text: "pong",
			blocks: `[{"type":"markdown","text":"pong"}]`, want: "pong", ok: true},
		{name: "section text only, no fields or accessory", text: "x",
			blocks: `[{"type":"section","text":{"type":"mrkdwn","text":"*deploy* done"},"fields":[{"type":"mrkdwn","text":"env: prod"}],` +
				`"accessory":{"type":"button","text":{"type":"plain_text","text":"Open"},"action_id":"o"}}]`,
			want: "deploy done", ok: true},
		{name: "header and section, one per line", text: "x",
			blocks: `[{"type":"header","text":{"type":"plain_text","text":"Weekly report"}},{"type":"divider"},` +
				`{"type":"section","text":{"type":"plain_text","text":"All green"}}]`,
			want: "Weekly report\nAll green", ok: true},
		{name: "section without a text is not previewable", text: "the text",
			blocks: `[{"type":"section","fields":[{"type":"mrkdwn","text":"a"}]}]`, want: "the text", ok: true},
		{name: "empty rich_text is not previewable", text: "the text", blocks: `[{"type":"rich_text","elements":[]}]`,
			want: "the text", ok: true},
		{name: "plan only: its title", text: "agent reply",
			blocks: `[{"type":"plan","title":"Thinking completed","tasks":[{"task_id":"1","title":"Read","status":"complete"}]}]`,
			want:   "Thinking completed", ok: true},
		{name: "untitled plan: the task in progress", text: "agent reply",
			blocks: `[{"type":"plan","tasks":[{"task_id":"1","title":"Read","status":"complete"},{"task_id":"2","title":"Write","status":"in_progress"}]}]`,
			want:   "Write", ok: true},
		{name: "plan and a context line: the text", text: "agent reply",
			blocks: `[{"type":"plan","title":"T","tasks":[]},` + ctx + `]`, want: "agent reply", ok: true},
		{name: "a simple message unfurl: the shared message's text", text: "<https://x.slack.com/archives/C1/p1>",
			blocks: `[{"type":"rich_text","elements":[{"type":"rich_text_section","elements":[{"type":"link","url":"https://x.slack.com/archives/C1/p1"}]}]}]`,
			atts:   unfurl, want: "the shared message", ok: true},
		{name: "attachment fallback without text", blocks: "[" + ctx + "]",
			atts: []slack.Attachment{{Fallback: "Build #12 passed", Text: "details"}}, want: "Build #12 passed", ok: true},
		{name: "no blocks keeps the document", text: "plain", ok: false},
		{name: "nothing to preview keeps the document", blocks: "[" + ctx + "]", ok: false},
		{name: "tombstone", text: "x", blocks: "[" + rich("x") + "]", subtype: "tombstone", ok: false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			m := &slack.Message{Type: "message", TS: "1.000100", User: "U1", Text: tc.text, Subtype: tc.subtype, Attachments: tc.atts}
			if tc.blocks != "" {
				m.Blocks = json.RawMessage(tc.blocks)
			}
			d, ok := PreviewFromMessage(R, m)
			if ok != tc.ok {
				t.Fatalf("ok %v, want %v (%q)", ok, tc.ok, PlainText(d))
			}
			if got := PlainText(d); ok && got != tc.want {
				t.Errorf("preview %q, want %q", got, tc.want)
			}
			for _, b := range d.Blocks {
				if b.Kind == Context || b.Kind == Agent || b.Kind == Actions || b.Kind == AttachmentBlock || b.Kind == FileBlock {
					t.Errorf("a %v block in the preview", b.Kind)
				}
			}
		})
	}
}
