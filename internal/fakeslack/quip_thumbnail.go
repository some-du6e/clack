package fakeslack

import (
	"encoding/json"
	"strconv"
	"time"

	"github.com/some-du6e/clack/internal/canvas"
	"github.com/some-du6e/clack/internal/richtext"
)

// quipThumbnail is the BLOCK_KIT_THUMBNAIL Quip keeps for canvas c when
// the fixture sets none: the canvas's title as a header, then its blocks
// as the rich text Quip's Slack writer emits for them (sections, headings
// in bold, list items as rich_text_lists at their indent and offset,
// quotes, code, dividers; a date as the UTC calendar day it names, at 00:00
// UTC with timezone "UTC" and format "{date}", as slackDateTextElement
// writes it). Tables, images, embeds, files and the like are left out.
func quipThumbnail(c *FakeCanvas) string {
	blocks := []any{}
	if c.Title != "" {
		blocks = append(blocks, map[string]any{"type": "header", "text": map[string]any{"type": "plain_text", "text": c.Title}})
	}
	rich := func(el map[string]any) {
		blocks = append(blocks, map[string]any{"type": "rich_text", "elements": []any{el}})
	}
	section := func(spans []richtext.Span, bold bool) map[string]any {
		return map[string]any{"type": "rich_text_section", "elements": thumbElements(spans, bold)}
	}
	for i := range c.Doc.Blocks {
		b := &c.Doc.Blocks[i]
		switch b.Kind {
		case canvas.Paragraph:
			if len(b.Spans) > 0 {
				rich(section(b.Spans, false))
			}
		case canvas.Heading1, canvas.Heading2, canvas.Heading3:
			rich(section(b.Spans, true))
		case canvas.Bullet, canvas.Check:
			rich(map[string]any{"type": "rich_text_list", "style": "bullet", "indent": b.Level,
				"elements": []any{section(b.Spans, false)}})
		case canvas.Numbered:
			rich(map[string]any{"type": "rich_text_list", "style": "ordered", "indent": b.Level, "offset": max(b.Number-1, 0),
				"elements": []any{section(b.Spans, false)}})
		case canvas.Quote:
			rich(map[string]any{"type": "rich_text_quote", "elements": thumbElements(b.Spans, false)})
		case canvas.Code:
			rich(map[string]any{"type": "rich_text_preformatted", "elements": []any{map[string]any{"type": "text", "text": b.Code}}})
		case canvas.Divider:
			blocks = append(blocks, map[string]any{"type": "divider"})
		}
	}
	out, _ := json.Marshal(map[string]any{"blocks": blocks})
	return string(out)
}

// thumbElements are spans as rich text elements (bold: every one bold, a
// heading's).
func thumbElements(spans []richtext.Span, bold bool) []any {
	out := []any{}
	for _, s := range spans {
		var el map[string]any
		switch s.Kind {
		case richtext.Link:
			el = map[string]any{"type": "link", "url": s.URL, "text": s.Text}
		case richtext.User:
			el = map[string]any{"type": "user", "user_id": s.ID}
		case richtext.Channel:
			el = map[string]any{"type": "channel", "channel_id": s.ID}
		case richtext.Usergroup:
			el = map[string]any{"type": "usergroup", "usergroup_id": s.ID}
		case richtext.Broadcast:
			el = map[string]any{"type": "broadcast", "range": s.ID}
		case richtext.Emoji:
			el = map[string]any{"type": "emoji", "name": s.ID}
		case richtext.Date:
			sec, _ := strconv.ParseInt(s.ID, 10, 64)
			day := time.Unix(sec, 0).UTC().Truncate(24 * time.Hour)
			el = map[string]any{"type": "date", "timestamp": day.Unix(), "timezone": "UTC", "format": "{date}", "fallback": s.Text}
		case richtext.Break:
			el = map[string]any{"type": "text", "text": "\n"}
		default:
			el = map[string]any{"type": "text", "text": s.Text}
		}
		if t := el["type"]; t == "text" || t == "link" {
			st := map[string]any{}
			if bold || s.Style&richtext.Bold != 0 {
				st["bold"] = true
			}
			if s.Style&richtext.Italic != 0 {
				st["italic"] = true
			}
			if s.Style&richtext.Strike != 0 {
				st["strike"] = true
			}
			if s.Style&richtext.Code != 0 {
				st["code"] = true
			}
			if len(st) > 0 {
				el["style"] = st
			}
		}
		out = append(out, el)
	}
	return out
}
