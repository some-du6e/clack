package composer

import (
	"encoding/json"
	"html"
	"strings"
)

// Slack's own copies carry the message as the composer's Quill delta,
// "slack/texty" (Copy message: message-actions-copy-text 4EhV, the
// blocks through convertBlocksToDelta), and the selection as "slack/html"
// (the clipboard handler SC+N: the DOM with its data-stringify marks).
// Chromium keeps both in its web custom data (internal/ui/richclip).
// Texty pastes them before text/html, so a mention stays a mention.
//
// textyHTML turns a slack/texty delta into the HTML docFromHTML reads:
// text with its inline formats, each slackmention as Slack's
// data-stringify mention element, newlines as line breaks, embedded
// emoji as their :name:. "" for anything that isn't a delta.
func textyHTML(s string) string {
	if s == "" || len(s) > phMaxHTML {
		return ""
	}
	var d struct {
		Ops []struct {
			Insert     json.RawMessage `json:"insert"`
			Attributes struct {
				Bold, Italic, Strike, Code bool
				Link                       string
				Mention                    *struct {
					ID    string `json:"id"`
					Label string `json:"label"`
				} `json:"slackmention"`
			} `json:"attributes"`
		} `json:"ops"`
	}
	if json.Unmarshal([]byte(s), &d) != nil || len(d.Ops) == 0 {
		return ""
	}
	var b strings.Builder
	for _, op := range d.Ops {
		var text string
		if json.Unmarshal(op.Insert, &text) != nil {
			var emb struct {
				Emoji *struct {
					Text string `json:"text"`
				} `json:"slackemoji"`
			}
			if json.Unmarshal(op.Insert, &emb) != nil || emb.Emoji == nil {
				continue
			}
			text = emb.Emoji.Text
		}
		at := op.Attributes
		if m := at.Mention; m != nil && m.ID != "" {
			b.WriteString(`<span data-stringify-type="mention" data-stringify-id="` + html.EscapeString(m.ID) +
				`" data-stringify-label="` + html.EscapeString(m.Label) + `">` + html.EscapeString(text) + `</span>`)
			continue
		}
		open, close := "", ""
		wrap := func(o, c string, on bool) {
			if on {
				open, close = open+o, c+close
			}
		}
		wrap("<b>", "</b>", at.Bold)
		wrap("<i>", "</i>", at.Italic)
		wrap("<s>", "</s>", at.Strike)
		wrap("<code>", "</code>", at.Code)
		if at.Link != "" {
			open, close = open+`<a href="`+html.EscapeString(at.Link)+`">`, "</a>"+close
		}
		for i, part := range strings.Split(text, "\n") {
			if i > 0 {
				b.WriteString("<br>")
			}
			if part != "" {
				b.WriteString(open + strings.ReplaceAll(html.EscapeString(part), "  ", " &nbsp;") + close)
			}
		}
	}
	return b.String()
}
