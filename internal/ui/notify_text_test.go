package ui

import (
	"encoding/json"
	"testing"

	"github.com/some-du6e/clack/internal/richtext"
	"github.com/some-du6e/clack/internal/slack"
)

// TestNotifyTextLikeSlack: a notification's text is Slack's
// extractTextFromMessage (Eley; notifyText, notify_dm.go):
// convertMessageToPlainText with blockSpacing single (2xQY) over the
// blocks, else the text as written (also when the blocks give none; a
// video block gives its title); only a message with neither reads its
// first file's title, then its attachments (a message unfurl's blocks,
// else the first fallback, text, pretext or footer); with nothing at all,
// "message {subtype} (message missing text)", but never for a message
// with attachments. Synthetic messages.
func TestNotifyTextLikeSlack(t *testing.T) {
	rich := func(s string) string {
		return `{"type":"rich_text","elements":[{"type":"rich_text_section","elements":[{"type":"text","text":"` + s + `"}]}]}`
	}
	for _, c := range []struct {
		name, msg, want string
	}{
		{"the welcome post: header and section, no alt text, no button",
			`{"type":"message","subtype":"bot_message","bot_id":"B0SYNTH0001","text":"Welcome! A synthetic note.","blocks":[` +
				`{"type":"image","image_url":"https://example.invalid/banner.png","alt_text":"A synthetic banner"},` +
				`{"type":"header","text":{"type":"plain_text","text":"Welcome!","emoji":true}},` +
				`{"type":"section","text":{"type":"mrkdwn","text":"A synthetic note: *pick* the channels."}},` +
				`{"type":"actions","elements":[{"type":"button","action_id":"a","text":{"type":"plain_text","text":"Get started"}}]}]}`,
			"Welcome!\nA synthetic note: pick the channels."},
		{"an image block is its title",
			`{"type":"message","text":"","blocks":[{"type":"image","image_url":"https://example.invalid/b.png","alt_text":"alt words",` +
				`"title":{"type":"plain_text","text":"A synthetic banner title"}}]}`,
			"A synthetic banner title"},
		{"a context block: its text elements a space apart",
			`{"type":"message","text":"","blocks":[{"type":"context","elements":[{"type":"image","image_url":"https://example.invalid/i.png","alt_text":"icon"},` +
				`{"type":"mrkdwn","text":"by *bot*"},{"type":"plain_text","text":"ok"}]}]}`,
			"by bot ok"},
		{"a section's fields, one line each",
			`{"type":"message","text":"","blocks":[{"type":"section","text":{"type":"mrkdwn","text":"Synthetic summary"},` +
				`"fields":[{"type":"mrkdwn","text":"*Status:* done"},{"type":"plain_text","text":"Owner: Robin"}]}]}`,
			"Synthetic summary\nStatus: done\nOwner: Robin"},
		{"the text, without its link preview",
			`{"type":"message","text":"see <https://example.invalid/page>","attachments":[{"id":1,"from_url":"https://example.invalid/page",` +
				`"title":"A synthetic page","text":"Its synthetic description","fallback":"A synthetic page"}]}`,
			"see https://example.invalid/page"},
		{"attachments only: the first fallback",
			`{"type":"message","subtype":"bot_message","bot_id":"B0SYNTH0001","text":"","attachments":[{"id":1,` +
				`"fallback":"Synthetic build 12 passed","pretext":"CI","text":"All green"}]}`,
			"Synthetic build 12 passed"},
		{"attachments only, no fallback: the text before the pretext",
			`{"type":"message","subtype":"bot_message","bot_id":"B0SYNTH0001","text":"","attachments":[{"id":1,"pretext":"CI","text":"All green"}]}`,
			"All green"},
		{"a message unfurl's blocks",
			`{"type":"message","text":"","attachments":[{"id":1,"is_msg_unfurl":true,"fallback":"[synthetic] a forward","blocks":[` + rich("forwarded words") + `]}]}`,
			"forwarded words"},
		{"rich text blocks over the text",
			`{"type":"message","user":"U0SYNTH0001","text":"the fallback","blocks":[` + rich("the rich words") + `]}`,
			"the rich words"},
		{"blocks that give nothing: the text",
			`{"type":"message","subtype":"bot_message","bot_id":"B0SYNTH0001","text":"A synthetic *request* to approve","blocks":[` +
				`{"type":"actions","elements":[{"type":"button","action_id":"a","text":{"type":"plain_text","text":"Approve"}}]}]}`,
			"A synthetic request to approve"},
		{"a video block is its title",
			`{"type":"message","text":"","blocks":[{"type":"video","video_url":"https://example.invalid/v.mp4","thumbnail_url":"https://example.invalid/t.png",` +
				`"alt_text":"a synthetic clip","title":{"type":"plain_text","text":"A synthetic video title"},` +
				`"description":{"type":"plain_text","text":"Its synthetic description"}}]}`,
			"A synthetic video title"},
		{"a text-only message as written: a mention that reads as nothing leaves its space",
			`{"type":"message","user":"U0SYNTH0001","text":"<@U0NOBODY01> the synthetic deploy finished"}`,
			" the synthetic deploy finished"},
		{"no text: the file's title",
			`{"type":"message","text":"","files":[{"id":"F0SYNTH0001","name":"report.pdf","title":"A synthetic report","mimetype":"application/pdf","filetype":"pdf"}]}`,
			"A synthetic report"},
		{"attachments with nothing to read: no body (textContentFromAttachments), not the missing text",
			`{"type":"message","subtype":"bot_message","bot_id":"B0SYNTH0001","text":"","attachments":[{"id":1,"image_url":"https://example.invalid/i.png"}]}`,
			""},
		{"nothing to show",
			`{"type":"message","subtype":"bot_message","bot_id":"B0SYNTH0001","text":"","blocks":[` +
				`{"type":"actions","elements":[{"type":"button","action_id":"a","text":{"type":"plain_text","text":"Go"}}]}]}`,
			"message bot_message (message missing text)"},
	} {
		var m slack.Message
		if err := json.Unmarshal([]byte(c.msg), &m); err != nil {
			t.Fatalf("%s: %v", c.name, err)
		}
		m.TS = "1762520000.000100"
		cm := noteTestMessage(&m)
		// a member the store doesn't hold reads as nothing (noteSpanText)
		unknown := func(s richtext.Span) (string, bool) { return "", s.Kind == richtext.User }
		if got := notifyText(noResolver{}, cm, unknown); got != c.want {
			t.Errorf("%s: %q, Slack %q", c.name, got, c.want)
		}
	}
}
