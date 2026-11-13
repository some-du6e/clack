package richtext

import (
	"encoding/json"
	"net/url"
	"strings"
	"testing"

	"github.com/some-du6e/clack/internal/slack"
)

// Synthetic fixtures only. The shapes are a live canvas-section link's
// (2026-11-12, keys and types only): a rich_text "canvas" element and a
// document_section unfurl.

type canvasRes struct {
	fakeRes
	refs   map[string]CanvasRef
	secs   map[string]json.RawMessage
	missed []string
}

func (r *canvasRes) CanvasRef(id string) (CanvasRef, bool) {
	c, ok := r.refs[id]
	if !ok {
		r.missed = append(r.missed, "f"+id)
	}
	return c, ok
}

func (r *canvasRes) CanvasSection(f, s string) (json.RawMessage, bool) {
	raw, ok := r.secs[f+"_"+s]
	if !ok {
		r.missed = append(r.missed, "s"+f+"_"+s)
	}
	return raw, ok
}

const csLink = "https://synthetic.enterprise.slack.com/docs/T1/F0CANV0001?focus_section_id=temp%3AC%3ASEC1"

func csMessage(withAttachment bool) *slack.Message {
	m := &slack.Message{Text: "<" + csLink + ">", Blocks: json.RawMessage(`[{"type":"rich_text","block_id":"b1","elements":[{"type":"rich_text_section","elements":[
		{"type":"canvas","file_id":"F0CANV0001","section_id":"temp:C:SEC1","text":"` + csLink + `","url":"` + csLink + `"}]}]}]`)}
	if withAttachment {
		_ = json.Unmarshal([]byte(`[{"from_url":"`+csLink+`","original_url":"`+csLink+`","text":"Ship the beta","fallback":"Ship the beta",
			"file_id":"F0CANV0001","document_section":{"section_id":"temp:C:SEC1","snippet":{"authors":[{"user_id":"U1","is_author":true,
			"is_recent_editor":false,"is_most_recent_editor":true}],"section_edited_ts":1762515690123456,"file_ids":[]}},
			"ts":1762515690,"footer":"Plans :smile:","author_name":"​","author_id":"U1",
			"blocks":[{"type":"rich_text","block_id":"temp:C:SEC1","elements":[{"type":"rich_text_section","elements":[{"type":"text","text":"Ship the beta"}]}]}],"id":1}]`),
			&m.Attachments)
	}
	return m
}

func slugOf(t *testing.T, d Doc) Span {
	t.Helper()
	for _, b := range d.Blocks {
		for _, s := range b.Spans {
			if s.Kind == Canvas {
				return s
			}
		}
	}
	t.Fatalf("no canvas slug in %+v", d.Blocks)
	return Span{}
}

// The slug names the canvas and the section ("{canvasName}: {sectionText}"),
// links to the canvas with the section, and draws the title's emoji.
func TestCanvasSlugSection(t *testing.T) {
	r := &canvasRes{refs: map[string]CanvasRef{"F0CANV0001": {Title: "Plans :smile:", Status: "normal"}},
		secs: map[string]json.RawMessage{"F0CANV0001_temp:C:SEC1": json.RawMessage(`{"type":"rich_text","elements":[{"type":"rich_text_section","elements":[{"type":"text","text":"A section whose text runs past the cut"}]}]}`)}}
	sp := slugOf(t, FromMessage(r, csMessage(false)))
	if sp.Text != "Plans :smile:: A section whose text runs…" {
		t.Errorf("label %q", sp.Text)
	}
	if sp.Icon != "canvas-content" || sp.Off || sp.ID != "F0CANV0001" {
		t.Errorf("slug %+v", sp)
	}
	if p := sp.PartSpans(); len(p) < 2 || p[1].Kind != Emoji || p[1].ID != "smile" {
		t.Errorf("the title's emoji isn't an emoji: %+v", p)
	}
	u, err := url.Parse(sp.URL)
	if err != nil || u.Query().Get("focus_section_id") != "temp:C:SEC1" || !strings.Contains(u.Path, "/docs/T1/F0CANV0001") {
		t.Errorf("url %q", sp.URL)
	}
}

// Without the canvas fetched yet, the message's own unfurl names it (its
// footer and blocks); without either it reads "Private canvas" with the
// lock (Slack's file stub) and asks for it.
func TestCanvasSlugHints(t *testing.T) {
	r := &canvasRes{}
	d := FromMessage(r, csMessage(true))
	if sp := slugOf(t, d); sp.Text != "Plans :smile:: Ship the beta" {
		t.Errorf("label from the unfurl %q", sp.Text)
	}
	var an *CanvasAnchor
	var att *Attachment
	for _, b := range d.Blocks {
		if b.Kind == AttachmentBlock && b.Att != nil && b.Att.Anchor != nil {
			att, an = b.Att, b.Att.Anchor
		}
	}
	if an == nil {
		t.Fatal("no canvas-section card")
	}
	if an.FileID != "F0CANV0001" || an.SectionID != "temp:C:SEC1" || an.Icon != "canvas-content" || att.FromURL != csLink {
		t.Errorf("card %+v from %q", an, att.FromURL)
	}
	if PlainSpans(an.FooterTitle) != "Plans :smile:" || len(att.Text) != 1 || SpansText(att.Text[0].Spans) != "Ship the beta" {
		t.Errorf("footer %q, text %+v", PlainSpans(an.FooterTitle), att.Text)
	}
	if att.AuthorName != "" {
		t.Errorf("the card has an author row (%q); Slack's QuipAnchorLink draws none", att.AuthorName)
	}

	r2 := &canvasRes{}
	sp := slugOf(t, FromMessage(r2, csMessage(false)))
	if sp.Text != "Private canvas" || sp.Icon != "lock" || sp.Off {
		t.Errorf("unknown canvas %+v", sp)
	}
	if len(r2.missed) == 0 || r2.missed[0] != "fF0CANV0001" {
		t.Errorf("not asked for: %v", r2.missed)
	}
}

// A deleted canvas is the disabled "Deleted canvas" slug with the trash
// icon; a missing section reads "{canvasName}: …"; hide_title drops the
// name; a bare element is the canvas's name alone.
func TestCanvasSlugVariants(t *testing.T) {
	r := &canvasRes{refs: map[string]CanvasRef{"F0CANV0001": {Status: "deleted"}, "F0CANV0002": {Title: "Notes", Status: "normal"}},
		secs: map[string]json.RawMessage{"F0CANV0002_temp:C:GONE": nil, "F0CANV0002_temp:C:S2": json.RawMessage(`{"type":"rich_text","elements":[{"type":"rich_text_section","elements":[{"type":"text","text":"Two"}]}]}`)}}
	doc := func(el string) Span {
		return slugOf(t, FromMessage(r, &slack.Message{Blocks: json.RawMessage(`[{"type":"rich_text","elements":[{"type":"rich_text_section","elements":[` + el + `]}]}]`)}))
	}
	if sp := doc(`{"type":"canvas","file_id":"F0CANV0001"}`); sp.Text != "Deleted canvas" || sp.Icon != "trash" || !sp.Off {
		t.Errorf("deleted %+v", sp)
	}
	if sp := doc(`{"type":"canvas","file_id":"F0CANV0002","section_id":"temp:C:GONE"}`); sp.Text != "Notes: …" {
		t.Errorf("missing section %q", sp.Text)
	}
	if sp := doc(`{"type":"canvas","file_id":"F0CANV0002","section_id":"temp:C:S2","hide_title":true}`); sp.Text != "Two" {
		t.Errorf("hide_title %q", sp.Text)
	}
	if sp := doc(`{"type":"canvas","file_id":"F0CANV0002"}`); sp.Text != "Notes" || !strings.HasPrefix(sp.URL, "https://app.slack.com/docs/-/F0CANV0002") {
		t.Errorf("bare %+v", sp)
	}
}
