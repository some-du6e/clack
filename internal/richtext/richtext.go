package richtext

import "github.com/some-du6e/yoloslackgoclient/internal/slack"

// FromMessage builds the document for a message: blocks if rich, else mrkdwn
// text; plus files and attachments.
func FromMessage(r Resolver, m *slack.Message) Doc {
	return Doc{Blocks: FromMrkdwn(r, m.Text)}
}

// FromMrkdwn parses Slack mrkdwn text.
func FromMrkdwn(r Resolver, text string) []Block {
	if text == "" {
		return nil
	}
	return []Block{{Kind: Paragraph, Spans: []Span{{Kind: Text, Text: text}}}}
}

// PlainText flattens a document to text (copy, previews, notifications).
func PlainText(d Doc) string {
	return ""
}
