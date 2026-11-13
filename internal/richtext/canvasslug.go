package richtext

import (
	"encoding/json"
	"net/url"
	"strings"
	"unicode/utf8"

	"github.com/some-du6e/clack/internal/slack"
)

// Canvas links, 1:1 with Slack's web client (gantry-v2 build 0fb3750e,
// 2026-11-12):
//
//   - A rich_text "canvas" element ({file_id, section_id?, url?, text?,
//     label?, hide_title?}: a pasted canvas or canvas-section link) is
//     Slack's Canvas (boot-shared rlp/): a RichTextSlug (Pc7Q) with
//     getCanvasSlugAttributes' (client-boot Apl0) icon and label. The
//     label is the canvas's name (CanvasFileName: its title_blocks, else
//     its title with :emoji:), "{canvasName}: {sectionText}" for a
//     section link (the section's rich text, cut at 25 characters with
//     "…"; quip.getCanvasBulkSectionIdsData), "{canvasName}: …" when the
//     section has no content, or getFileStatusStringByType's "Deleted
//     canvas", "Private canvas" and "Access denied". hide_title drops the
//     name; label overrides it. Icons (canvasSlugIconResolver, ZJLk):
//     canvas-content, trash (deleted), lock (hidden, locked, missing). A
//     deleted canvas's slug is disabled. A click opens the canvas focused
//     on the section (openCanvas, source rich_text_canvas_element); the
//     slug's link is the element's url.
//   - An attachment with document_section (a canvas-section unfurl) is
//     MessageAttachmentQuipAnchorLink (boot-render): the section's blocks
//     (or its text), then AttachmentFooter: the canvas-content icon (lock
//     for a private canvas) and the canvas's title linked to from_url,
//     the "View section" permalink, and "Canvas". It has no author row
//     and no date (shouldHideUnfurlDate).

// CanvasRef is what a Resolver knows about a canvas (files.info).
type CanvasRef struct {
	Title       string
	TitleBlocks json.RawMessage
	// Status is getFileStatus (S2bC): "normal", "deleted", "hidden" or
	// "locked".
	Status string
}

// CanvasResolver is an optional Resolver extension (store.Store). ok and
// known are false while the canvas or section isn't fetched: the resolver
// asks for it and the Doc is rebuilt when it arrives.
type CanvasResolver interface {
	CanvasRef(fileID string) (ref CanvasRef, ok bool)
	// CanvasSection is the section's rich_text block
	// (rich_text_blocks[0]); nil when the section has none.
	CanvasSection(fileID, sectionID string) (raw json.RawMessage, known bool)
}

// canvasNameMax is getCanvasSlugAttributes' section preview length.
const canvasNameMax = 25

// canvasHints are what a message's own attachments say about the canvases
// it links: a document_section unfurl's footer is the canvas's title and
// its blocks are the section's rich text (block_id is the section id).
type canvasHints struct {
	title   map[string]string          // file id -> footer
	section map[string]json.RawMessage // file id + "_" + section id -> rich_text block
}

func (c *builder) hintsFrom(m *slack.Message) {
	for i := range m.Attachments {
		a := &m.Attachments[i]
		if a.FileID == "" || a.DocumentSection == nil {
			continue
		}
		if c.hints.title == nil {
			c.hints = canvasHints{title: map[string]string{}, section: map[string]json.RawMessage{}}
		}
		if a.Footer != "" {
			c.hints.title[a.FileID] = a.Footer
		}
		var bs []json.RawMessage
		if json.Unmarshal(a.Blocks, &bs) == nil && len(bs) > 0 {
			c.hints.section[a.FileID+"_"+a.DocumentSection.SectionID] = bs[0]
		}
	}
}

// canvasName is CanvasFileName: title_blocks, else the title with its
// :emoji: (FormatTextWithEmoji), else the message's hint; ok false when
// nothing names it.
func (c *builder) canvasName(fileID string) (name []Span, status string, ok, fromBlocks bool) {
	if cr, isCR := c.r.(CanvasResolver); isCR {
		if ref, known := cr.CanvasRef(fileID); known {
			if ref.Status != "" && ref.Status != "normal" {
				return nil, ref.Status, true, false
			}
			if bs := c.blocks(parseBlocks(ref.TitleBlocks)); len(bs) > 0 {
				if sp := firstLine(flatten(bs)); len(sp) > 0 {
					return sp, "normal", true, true
				}
			}
			if ref.Title != "" {
				return titleSpans(c.r, ref.Title), "normal", true, false
			}
		}
	}
	if t := c.hints.title[fileID]; t != "" {
		return titleSpans(c.r, t), "normal", true, false
	}
	return nil, "", false, false
}

func titleSpans(r Resolver, s string) []Span {
	if sp := EmojiText(r, s); sp != nil {
		return sp
	}
	return []Span{{Kind: Text, Text: s}}
}

// canvasSection is the section's preview spans; known false while it
// isn't fetched, and nil spans when the section has no content.
func (c *builder) canvasSection(fileID, sectionID string) (sp []Span, known bool) {
	raw, ok := c.hints.section[fileID+"_"+sectionID]
	if !ok {
		if cr, isCR := c.r.(CanvasResolver); isCR {
			raw, ok = cr.CanvasSection(fileID, sectionID)
		}
	}
	if !ok {
		return nil, false
	}
	if len(raw) == 0 || string(raw) == "null" {
		return nil, true
	}
	var b bk
	if json.Unmarshal(raw, &b) != nil {
		return nil, true
	}
	// stripNestedCanvasSectionIds keeps the section's own canvas links
	// from naming their sections
	return cutRunes(firstLine(flatten(c.blocks([]bk{b}))), canvasNameMax), true
}

// firstLine is sp up to its first line break.
func firstLine(sp []Span) []Span {
	for i, s := range sp {
		if s.Kind == Break {
			return sp[:i]
		}
	}
	return sp
}

// cutRunes keeps n characters of sp (an emoji counts as one) and adds
// "…" when it cut.
func cutRunes(sp []Span, n int) []Span {
	var out []Span
	left := n
	for _, s := range sp {
		if s.Kind == Text || s.Kind == Link {
			c := utf8.RuneCountInString(s.Text)
			if c > left {
				return append(out, Span{Kind: Text, Text: string([]rune(s.Text)[:left]) + "…", Style: s.Style})
			}
			left -= c
			out = append(out, Span{Kind: Text, Text: s.Text, Style: s.Style})
			continue
		}
		if left == 0 {
			return append(out, Span{Kind: Text, Text: "…"})
		}
		left--
		if s.Kind != Emoji {
			s = Span{Kind: Text, Text: s.Text, Style: s.Style}
		}
		out = append(out, s)
	}
	return out
}

// canvasStatusLabel is getFileStatusStringByType for a canvas.
func canvasStatusLabel(status string) string {
	switch status {
	case "deleted":
		return "Deleted canvas"
	case "locked":
		return "Access denied"
	}
	return "Private canvas" // hidden, missing
}

// canvasIcon is canvasSlugIconResolver.
func canvasIcon(status string) string {
	switch status {
	case "deleted":
		return "trash"
	case "hidden", "locked", "missing":
		return "lock"
	}
	return "canvas-content"
}

// CanvasURL is a link that opens canvas fileID (focused on sectionID):
// the element's url when it has one, else a docs link Clack routes
// itself. Its focus_section_id is the section.
func CanvasURL(raw, fileID, sectionID string) string {
	u := SafeURL(raw)
	if u == "" {
		u = "https://app.slack.com/docs/-/" + fileID
	}
	if sectionID == "" {
		return u
	}
	p, err := url.Parse(u)
	if err != nil {
		return u
	}
	if p.Query().Get("focus_section_id") == "" {
		q := p.Query()
		q.Set("focus_section_id", sectionID)
		p.RawQuery = q.Encode()
	}
	return p.String()
}

// SpanParts is a slug's label: text and emoji spans.
type SpanParts struct{ Spans []Span }

// PartSpans is a slug's label spans (nil without one).
func (s *Span) PartSpans() []Span {
	if s.Parts == nil {
		return nil
	}
	return s.Parts.Spans
}

// canvasSpan is the Canvas slug for element e.
func (c *builder) canvasSpan(e *bk) Span {
	sp := Span{Kind: Canvas, ID: e.FileID, URL: CanvasURL(e.URL, e.FileID, e.SectionID)}
	name, status, ok, _ := c.canvasName(e.FileID)
	if !ok {
		status = "missing" // the file isn't known yet: Slack's stub has no access
	}
	if lo := c.labelText(e); lo != "" {
		name, status = titleSpans(c.r, lo), "normal" // labelOverride
	}
	sp.Icon = canvasIcon(status)
	var parts []Span
	switch {
	case status != "normal":
		parts = []Span{{Kind: Text, Text: canvasStatusLabel(status)}}
		sp.Off = status == "deleted"
	case e.SectionID == "":
		if !e.HideTitle {
			parts = name
		}
	default:
		sec, known := c.canvasSection(e.FileID, e.SectionID)
		switch {
		case !known || len(sec) == 0: // section-missing
			if e.HideTitle {
				parts = []Span{{Kind: Text, Text: "…"}}
			} else {
				parts = append(append([]Span(nil), name...), Span{Kind: Text, Text: ": …"})
			}
		case e.HideTitle:
			parts = sec
		default:
			parts = append(append(append([]Span(nil), name...), Span{Kind: Text, Text: ": "}), sec...)
		}
	}
	sp.Parts = &SpanParts{Spans: parts}
	sp.Text = PlainSpans(parts)
	return sp
}

// labelText is the element's label (a string), "" when it has none.
func (c *builder) labelText(e *bk) string {
	if e.Label == nil {
		return ""
	}
	return e.Label.Text
}

// PlainSpans is spans' text, emoji as their :name:.
func PlainSpans(sp []Span) string {
	var b strings.Builder
	for _, s := range sp {
		switch {
		case s.Kind == Emoji && s.ID != "":
			b.WriteString(":" + s.ID + ":")
		case s.Kind == Break:
			b.WriteByte('\n')
		default:
			b.WriteString(s.Text)
		}
	}
	return b.String()
}

// CanvasAnchor is a document_section attachment
// (MessageAttachmentQuipAnchorLink): the section's canvas, the section,
// the footer's title (FooterTitle; empty when nothing names the canvas)
// and its icon (canvas-content, lock for a private canvas, trash for a
// deleted one).
type CanvasAnchor struct {
	FileID, SectionID string
	FooterTitle       []Span
	FooterFromBlocks  bool // FooterTitle is the canvas's title_blocks (QuipTitle micro)
	Icon              string
	Deleted           bool
}

// canvasAnchor fills att from a document_section attachment.
func (c *builder) canvasAnchor(a *slack.Attachment, att *Attachment) {
	an := &CanvasAnchor{FileID: a.FileID, SectionID: a.DocumentSection.SectionID, Icon: "canvas-content"}
	name, status, ok, fromBlocks := c.canvasName(a.FileID)
	switch {
	case !ok && a.Footer != "":
		an.FooterTitle = titleSpans(c.r, a.Footer)
	case status == "deleted":
		an.Deleted, an.Icon = true, "trash"
		an.FooterTitle = []Span{{Kind: Text, Text: "Deleted canvas"}}
	case status == "hidden" || status == "locked":
		an.Icon = "lock"
		an.FooterTitle = []Span{{Kind: Text, Text: "Private canvas"}}
	default:
		an.FooterTitle, an.FooterFromBlocks = name, fromBlocks
	}
	att.Anchor = an
	att.FromURL = SafeURL(firstNonEmpty(a.FromURL, a.OriginalURL))
	att.OriginalURL = SafeURL(a.OriginalURL)
	att.TS = tsSeconds(a.TS)
	if kb := c.blocks(parseBlocks(a.Blocks)); len(kb) > 0 {
		att.Text = kb
	} else if a.Text != "" {
		att.Text = c.attachmentText(a)
	}
}
