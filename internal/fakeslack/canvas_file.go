package fakeslack

// A canvas shared as an app's message file, in the shape of a live app
// reply (2026-11-12, keys and types only; every text here is the
// fixture's own): blocks a mrkdwn section that links the canvas (<docs
// URL|the FAQ>) and an actions block with two buttons; no attachments;
// files[0] the canvas (filetype and mode "quip", mimetype
// application/vnd.slack-docs, pretty_type "Canvas", has_rich_preview
// false, a title ":emoji: FAQ" whose title_blocks are emoji and text),
// owned (user, canvas_creator_id) by a person, not the app; upload and
// display_as_bot false; edited by the app's own bot in the message's
// second. The canvas (Size.Gallery, or AddFAQCanvas) opens with its
// title, then headings, paragraphs with a link and inline code, a list
// and more lines than the message card's 240px preview shows.

import (
	"strings"

	"github.com/some-du6e/clack/internal/canvas"
	"github.com/some-du6e/clack/internal/richtext"
	"github.com/some-du6e/clack/internal/slack"
)

// Fixture ids: the FAQ canvas and the app's message.
const (
	CanvasFAQ   = "F0CANVFAQ01"
	CanvasFAQTS = "1762515720.000100"
)

func init() {
	registerGallery("canvas-file", canvasFileMessages)
}

// CanvasFAQOwner is the person who owns the FAQ canvas.
func (s *Server) CanvasFAQOwner() slack.User { return s.users[2] }

// CanvasFAQTitle is the FAQ canvas's title.
func (s *Server) CanvasFAQTitle() string {
	n, _ := s.fixtureEmoji()
	return ":" + n + ": FAQ"
}

func (s *Server) faqTitleBlocks() []any {
	n, u := s.fixtureEmoji()
	return []any{map[string]any{"type": "rich_text", "block_id": "faqt", "elements": []any{map[string]any{"type": "rich_text_section", "elements": []any{
		map[string]any{"type": "emoji", "name": n, "display_url": u, "url": u},
		map[string]any{"type": "text", "text": " FAQ"},
	}}}}}
}

func (s *Server) faqCanvas() *FakeCanvas {
	sp := func(t string) []richtext.Span { return []richtext.Span{{Kind: richtext.Text, Text: t}} }
	n, u := s.fixtureEmoji()
	bs := []canvas.Block{
		{ID: "temp:C:FAQ00000000000000000000000000", Kind: canvas.Heading1, Spans: []richtext.Span{{Kind: richtext.Emoji, ID: n, URL: u, Text: ":" + n + ":"}, {Kind: richtext.Text, Text: " FAQ"}}},
		{ID: "temp:C:FAQ00000000000000000000000001", Kind: canvas.Heading2, Spans: sp("Getting started")},
		{ID: "temp:C:FAQ00000000000000000000000002", Kind: canvas.Paragraph, Spans: []richtext.Span{
			{Kind: richtext.Text, Text: "Read the "}, {Kind: richtext.Link, Text: "setup guide", URL: "https://example.com/setup"},
			{Kind: richtext.Text, Text: " first, then run "}, {Kind: richtext.Text, Text: "make setup", Style: richtext.Code}, {Kind: richtext.Text, Text: " once."}}},
		{ID: "temp:C:FAQ00000000000000000000000003", Kind: canvas.Heading2, Spans: sp("Asking for help")},
		{ID: "temp:C:FAQ00000000000000000000000004", Kind: canvas.Paragraph, Spans: sp("Post in the help channel with what you tried and what you expected to happen.")},
		{ID: "temp:C:FAQ00000000000000000000000005", Kind: canvas.Bullet, Spans: sp("Include the error text")},
		{ID: "temp:C:FAQ00000000000000000000000006", Kind: canvas.Bullet, Spans: sp("Say which version you run")},
		{ID: "temp:C:FAQ00000000000000000000000007", Kind: canvas.Heading2, Spans: sp("Common problems")},
		{ID: "temp:C:FAQ00000000000000000000000008", Kind: canvas.Paragraph, Spans: sp("If the build fails, clear the cache and try again before you ask.")},
		{ID: "temp:C:FAQ00000000000000000000000009", Kind: canvas.Paragraph, Spans: sp("Still stuck? Mark your question resolved once it is answered.")},
		{ID: "temp:C:FAQ00000000000000000000000010", Kind: canvas.Paragraph, Spans: sp("This line is past the preview.")},
	}
	owner := s.CanvasFAQOwner()
	return &FakeCanvas{ID: CanvasFAQ, Title: s.CanvasFAQTitle(), Creator: owner.ID, Doc: canvas.Doc{Blocks: bs}, TitleBlocks: s.faqTitleBlocks()}
}

// AddFAQCanvas adds the FAQ canvas (Size.Gallery has it already).
func (s *Server) AddFAQCanvas() { s.AddCanvas(*s.faqCanvas()) }

// canvasFAQFile is the FAQ canvas as message.files[0] carries it.
func (s *Server) canvasFAQFile() slack.File {
	owner := s.CanvasFAQOwner()
	n, _ := s.fixtureEmoji()
	return slack.File{ID: CanvasFAQ, Name: "_" + strings.ReplaceAll(n, "-", "_") + "__FAQ", Title: s.CanvasFAQTitle(),
		Mimetype: "application/vnd.slack-docs", Filetype: "quip", PrettyType: "Canvas", Mode: "quip", User: owner.ID,
		Size: 3782, URLPrivate: s.URL + "files-pri/" + TeamID + "-" + CanvasFAQ + "/canvas",
		Permalink:   "https://fakeclub.enterprise.slack.com/docs/" + TeamID + "/" + CanvasFAQ,
		TitleBlocks: rawJSON(s.faqTitleBlocks())}
}

func canvasFileMessages(s *Server) []slack.Message {
	link := "https://fakeclub.enterprise.slack.com/docs/" + TeamID + "/" + CanvasFAQ
	m := surfBot(s, CanvasFAQTS, "Thanks for asking! Check <"+link+"|the FAQ> first.", []any{
		map[string]any{"type": "section", "block_id": "faq1", "text": map[string]any{"type": "mrkdwn", "verbatim": false,
			"text": "Thanks for asking! Check <" + link + "|the FAQ> first; if that doesn't answer it, someone will be along soon."}},
		map[string]any{"type": "actions", "block_id": "faq2", "elements": []any{
			map[string]any{"type": "button", "action_id": "mark_resolved", "text": pt("Mark resolved"), "style": "primary", "value": "resolve:" + CanvasFAQTS},
			map[string]any{"type": "button", "action_id": "ask_ai", "text": pt("Ask the bot"), "value": "ask:" + CanvasFAQTS},
		}},
	}, nil)
	m.Subtype = "" // an app's reply: user and bot_id, no bot_message subtype
	m.Files = []slack.File{s.canvasFAQFile()}
	m.Edited = &slack.Edited{User: AppBotID, TS: strings.Split(CanvasFAQTS, ".")[0] + ".000000"}
	return []slack.Message{m}
}
