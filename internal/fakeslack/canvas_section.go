package fakeslack

// A link to a canvas section, as Slack sends it. The shapes are a real
// message's and a real canvas export's structure (read 2026-11-12, keys
// and types only; every text here is the fixture's own):
//
//   - The message's rich_text holds a "canvas" element {file_id,
//     section_id, text: the docs URL, url: the docs URL with
//     focus_section_id}, and its attachment is the section's unfurl:
//     from_url / original_url (that URL), text and fallback (the
//     section's text), file_id, document_section {section_id, snippet
//     {authors [{user_id, is_author, is_recent_editor,
//     is_most_recent_editor}], section_edited_ts (µs), file_ids []}}, ts,
//     footer (the canvas's title with its :emoji:), author_name (a
//     zero-width space: the author's real name), author_link, author_id,
//     author_icon, blocks (one rich_text whose block_id is the section id)
//     and id.
//   - The canvas's export opens with an <h1> whose emoji is a <control
//     data-remapped><img src alt>:name:</img></control>, then plain
//     <p class="line"> sections and an empty one; files.info's
//     title_blocks are text, emoji, text " ".
//   - A tabbed_canvas_updated-style message links the whole canvas: a
//     bare {type: canvas, file_id}.
//
// quip.getCanvasBulkSectionIdsData {file_id, section_ids} answers
// {sections: [{id, blocks, rich_text_blocks}]} from the canvas's
// document. Listed with Size.Gallery (the canvas) and in the gallery
// channel (the messages).

import (
	"encoding/json"
	"fmt"
	"net/url"
	"sort"
	"strings"

	"github.com/some-du6e/clack/internal/canvas"
	"github.com/some-du6e/clack/internal/slack"
)

// Fixture ids: the canvas, its linked section (35 characters, like
// Quip's), and the two messages.
const (
	CanvasSection      = "F0CANVSECT1"
	CanvasSectionID    = "temp:C:FXS0000000000000000000000040"
	CanvasSectionText  = "The section the link points at"
	CanvasSectionLinkT = "1762515700.000100" // the section link and its unfurl
	CanvasWholeLinkTS  = "1762515710.000100" // a bare canvas element
	CanvasSectionLines = 40                  // plain lines before the linked one
)

func init() {
	registerGallery("canvas-section", canvasSectionMessages)
}

// fixtureEmoji is a custom emoji the fake serves (the first by name).
func (s *Server) fixtureEmoji() (name, u string) {
	var names []string
	for n, u := range s.emoji {
		if !strings.HasPrefix(u, "alias:") {
			names = append(names, n)
		}
	}
	sort.Strings(names)
	if len(names) == 0 {
		return "", ""
	}
	return names[0], s.emoji[names[0]]
}

// CanvasSectionTitle is the fixture canvas's title (files.info title).
func (s *Server) CanvasSectionTitle() string {
	n, _ := s.fixtureEmoji()
	return "Fixture canvas :" + n + ":"
}

// sectionCanvas is the fixture canvas, in the real export's markup.
func (s *Server) sectionCanvas() *FakeCanvas {
	name, u := s.fixtureEmoji()
	var b strings.Builder
	b.WriteString(`<div class="quip-canvas-content"><h1 id="temp:C:FXS0000000000000000000000000">Fixture canvas <control id="temp:C:FXS0000000000000000000000099" data-remapped="true">`)
	b.WriteString(`<img src="` + u + `" alt="` + name + `" style="width: 30px; height: 30px">:` + name + `:</img></control></h1>`)
	for i := 1; i < CanvasSectionLines; i++ {
		fmt.Fprintf(&b, `<p id="temp:C:FXS%025d" class="line">Line %d.</p>`, i, i)
	}
	b.WriteString(`<p id="` + CanvasSectionID + `" class="line">` + CanvasSectionText + `</p>`)
	b.WriteString(`<p id="temp:C:FXS0000000000000000000000041" class="line"></p></div>`)
	html := b.String()
	d, _ := canvas.Parse(html)
	tb := []any{map[string]any{"type": "rich_text", "block_id": "tb1", "elements": []any{map[string]any{"type": "rich_text_section", "elements": []any{
		map[string]any{"type": "text", "text": "Fixture canvas "},
		map[string]any{"type": "emoji", "name": name, "display_url": u, "url": u},
		map[string]any{"type": "text", "text": " "},
	}}}}}
	return &FakeCanvas{ID: CanvasSection, Title: s.CanvasSectionTitle(), Channel: "", Doc: d, HTML: html, TitleBlocks: tb}
}

// CanvasSectionURL is the docs link to the fixture section.
func CanvasSectionURL() string {
	return "https://fakeclub.enterprise.slack.com/docs/" + TeamID + "/" + CanvasSection + "?focus_section_id=" + url.QueryEscape(CanvasSectionID)
}

func canvasSectionMessages(s *Server) []slack.Message {
	author := s.users[0]
	link := CanvasSectionURL()
	att := map[string]any{
		"from_url": link, "original_url": link, "text": CanvasSectionText, "fallback": CanvasSectionText,
		"file_id": CanvasSection,
		"document_section": map[string]any{"section_id": CanvasSectionID, "snippet": map[string]any{
			"authors":           []any{map[string]any{"user_id": author.ID, "is_author": true, "is_recent_editor": false, "is_most_recent_editor": true}},
			"section_edited_ts": int64(1762515690123456), "file_ids": []any{}}},
		"ts": 1762515690, "footer": s.CanvasSectionTitle(),
		"author_name": "​", "author_link": "https://fakeclub.enterprise.slack.com/team/" + author.ID, "author_id": author.ID,
		"author_icon": author.Profile.Image48,
		"blocks": []any{map[string]any{"type": "rich_text", "block_id": CanvasSectionID, "elements": []any{
			map[string]any{"type": "rich_text_section", "elements": []any{map[string]any{"type": "text", "text": CanvasSectionText}}}}}},
		"id": 1,
	}
	var a slack.Attachment
	_ = json.Unmarshal(rawJSON(att), &a)
	return []slack.Message{
		{Type: "message", TS: CanvasSectionLinkT, User: author.ID, Text: "<" + link + ">",
			Blocks: rawJSON([]any{map[string]any{"type": "rich_text", "block_id": "cs1", "elements": []any{map[string]any{"type": "rich_text_section", "elements": []any{
				map[string]any{"type": "canvas", "file_id": CanvasSection, "section_id": CanvasSectionID, "text": link, "url": link},
			}}}}}),
			Attachments: []slack.Attachment{a}},
		{Type: "message", TS: CanvasWholeLinkTS, User: author.ID, Text: "updated the canvas",
			Blocks: rawJSON([]any{map[string]any{"type": "rich_text", "block_id": "cs2", "elements": []any{map[string]any{"type": "rich_text_section", "elements": []any{
				map[string]any{"type": "text", "text": "updated the canvas "},
				map[string]any{"type": "canvas", "file_id": CanvasSection},
			}}}}})},
	}
}

// bulkSections is quip.getCanvasBulkSectionIdsData for canvas c: each
// asked-for section's rich text (cs.mu held).
func bulkSections(c *FakeCanvas, ids []string) []any {
	var out []any
	for _, id := range ids {
		b := c.Doc.Find(id)
		if b == nil {
			continue
		}
		els := []any{map[string]any{"type": "text", "text": b.Text()}}
		sec := map[string]any{"type": "rich_text_section", "elements": els}
		out = append(out, map[string]any{"id": id, "blocks": []any{sec},
			"rich_text_blocks": []any{map[string]any{"type": "rich_text", "elements": []any{sec}, "block_id": id}}})
	}
	return out
}

// AddSectionCanvas adds the canvas-section link's canvas (Size.Gallery
// has it already).
func (s *Server) AddSectionCanvas() { s.AddCanvas(*s.sectionCanvas()) }

// FixtureEmoji is the custom emoji the fixture canvas's title uses.
func (s *Server) FixtureEmoji() (name, url string) { return s.fixtureEmoji() }
