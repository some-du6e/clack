package popups

import (
	"encoding/json"
	"net/url"
	"strings"
	"testing"

	"github.com/some-du6e/clack/internal/core"
	"github.com/some-du6e/clack/internal/fakeslack"
	"github.com/some-du6e/clack/internal/richtext"
	"github.com/some-du6e/clack/internal/slack"
)

// sbMessage is sm as a search answer's message (its document as the
// channel builds it).
func sbMessage(e *srEnv, sm slack.Message) core.Message {
	sm.Type = "message"
	if sm.TS == "" {
		sm.TS = "1762448500.000100"
	}
	return core.Message{Channel: e.fs.ChannelIDs()[0], TS: sm.TS, User: sm.User, Text: sm.Text, Doc: richtext.FromMessage(e.h.a.S, &sm)}
}

// sbText joins segs' text and lists their mention slugs.
func sbText(segs []srSeg) (string, []string) {
	var b strings.Builder
	var mentions []string
	for _, sg := range segs {
		b.WriteString(sg.Text)
		if scSegMention(sg) {
			mentions = append(mentions, sg.Text)
		}
	}
	return b.String(), mentions
}

func sbBlocks(bs ...any) json.RawMessage {
	raw, _ := json.Marshal(bs)
	return raw
}

func sbRichText(els ...any) map[string]any {
	return map[string]any{"type": "rich_text", "elements": els}
}

func sbSection(text string) map[string]any {
	return map[string]any{"type": "rich_text_section", "elements": []any{map[string]any{"type": "text", "text": text}}}
}

// What a message's text line (the quick message rows, Search in channel,
// the original-density card) and the dialogs' message preview (Forward,
// Share file, Delete, Unsend, Remove from channel: Slack's
// ShareDialogPreview draws the message as its unfurl, its files and
// attachments under the text) say for messages whose text isn't in plain
// rich text blocks: a caption-less upload, an attachment-only bot post, a
// Block Kit section with fields and a quote, a block Clack can't draw.
// Each says what the old plain text path said (richtext.PlainText: a
// file's title, an attachment's pretext, title and text), with mentions as
// mention slugs and never a raw <@U…> or <https://…|…> token.
func TestSearchBodyTextFallbacks(t *testing.T) {
	e := srNewEnv(t, false)
	self := fakeslack.SelfID
	file := slack.File{ID: "F0SB000001", Name: "q3.pdf", Title: "Q3 report", Mimetype: "application/pdf", Filetype: "pdf",
		Permalink: "https://fake.slack.com/files/" + self + "/F0SB000001/q3.pdf"}
	section := map[string]any{"type": "section",
		"text": map[string]any{"type": "mrkdwn", "text": "> quoted line\nplain line"},
		"fields": []any{map[string]any{"type": "mrkdwn", "text": "*Owner:* <@" + self + ">"},
			map[string]any{"type": "plain_text", "text": "Status: done"}}}
	cases := []struct {
		name                   string
		sm                     slack.Message
		row, dialog            []string // what each must say
		rowNot, dialogNot      []string // what each mustn't
		rowMention, dlgMention bool     // a mention slug
	}{
		{name: "a caption-less upload", sm: slack.Message{User: self, Files: []slack.File{file}},
			row: []string{"Q3 report"}, dialog: []string{"Q3 report"}},
		{name: "an attachment-only bot post", sm: slack.Message{BotID: "B0SB000001", Username: "deploybot",
			Attachments: []slack.Attachment{{ID: 1, Pretext: "Deploy finished", Title: "build 42", Text: "ready for <@" + self + ">", Fallback: "build 42"}}},
			row: []string{"Deploy finished", "build 42", "ready for"}, dialog: []string{"Deploy finished", "build 42", "ready for"},
			rowMention: true, dlgMention: true},
		{name: "a section's quote and fields", sm: slack.Message{User: self, Text: "fallback text <@" + self + ">", Blocks: sbBlocks(section)},
			row: []string{"> quoted line", "plain line", "Owner:", "Status: done"}, dialog: []string{"> quoted line", "plain line", "Owner:", "Status: done"},
			rowNot: []string{"fallback text"}, dialogNot: []string{"fallback text"}, rowMention: true, dlgMention: true},
		{name: "a block Clack can't draw", sm: slack.Message{User: self, Text: "see <@" + self + "> and <https://example.com/page|the page>",
			Blocks: sbBlocks(map[string]any{"type": "mystery_widget_sb"})},
			row: []string{"see ", "the page"}, rowNot: []string{"fully displayed"}, rowMention: true},
		{name: "text and a file", sm: slack.Message{User: self, Text: "see the numbers", Files: []slack.File{file}},
			row: []string{"see the numbers"}, rowNot: []string{"Q3 report"}, dialog: []string{"see the numbers", "Q3 report"}},
	}
	for _, c := range cases {
		m := sbMessage(e, c.sm)
		row, rowM := sbText(scRowSegs(e.h, &m))
		dlg, dlgM := sbText(scPreviewSegs(e.h, &m))
		for _, s := range []string{row, dlg} {
			if strings.Contains(s, "<@") || strings.Contains(s, "<http") || strings.Contains(s, "<#") {
				t.Errorf("%s: a raw token in %q", c.name, s)
			}
		}
		for _, w := range c.row {
			if !strings.Contains(row, w) {
				t.Errorf("%s: the row says %q, without %q", c.name, row, w)
			}
		}
		for _, w := range c.rowNot {
			if strings.Contains(row, w) {
				t.Errorf("%s: the row says %q, with %q", c.name, row, w)
			}
		}
		for _, w := range c.dialog {
			if !strings.Contains(dlg, w) {
				t.Errorf("%s: the dialog preview says %q, without %q", c.name, dlg, w)
			}
		}
		for _, w := range c.dialogNot {
			if strings.Contains(dlg, w) {
				t.Errorf("%s: the dialog preview says %q, with %q", c.name, dlg, w)
			}
		}
		if c.rowMention && (len(rowM) == 0 || !strings.HasPrefix(rowM[0], "@")) {
			t.Errorf("%s: the row's mentions %q in %q", c.name, rowM, row)
		}
		if c.dlgMention && (len(dlgM) == 0 || !strings.HasPrefix(dlgM[0], "@")) {
			t.Errorf("%s: the dialog preview's mentions %q in %q", c.name, dlgM, dlg)
		}
	}
}

// The dialogs' preview keeps the message's lines as the plain text path
// writes them: list items with their bullets, quotes with "> ", and no
// blank line for a block that says nothing (a divider).
func TestSearchPreviewKeepsListsAndQuotes(t *testing.T) {
	e := srNewEnv(t, false)
	list := map[string]any{"type": "rich_text_list", "style": "bullet", "elements": []any{sbSection("one"), sbSection("two")}}
	quote := map[string]any{"type": "rich_text_quote", "elements": []any{map[string]any{"type": "text", "text": "quoted"}}}
	m := sbMessage(e, slack.Message{User: fakeslack.SelfID, Text: "intro one two quoted",
		Blocks: sbBlocks(sbRichText(sbSection("intro")), map[string]any{"type": "divider"}, sbRichText(list, quote))})
	if got, _ := sbText(scPreviewSegs(e.h, &m)); got != "intro\n• one\n• two\n> quoted" {
		t.Errorf("the preview says %q, want %q", got, "intro\n• one\n• two\n> quoted")
	}
}

// Attachments without ids are keyed by their index among the message's
// attachments as the answer lists them (core.searchMatches): an agent
// channel unfurl, which isn't an attachment block, still counts, so the
// card shows the attachment the server marked, not the one after it.
func TestSearchCardAttachmentKeysByIndex(t *testing.T) {
	e := srNewEnv(t, false)
	m := sbMessage(e, slack.Message{User: fakeslack.SelfID, Text: "the cards", Attachments: []slack.Attachment{
		{AgentChannelUnfurl: &slack.AgentChannelUnfurl{ChannelID: "C0SBAGENT1", ChannelName: "agent-place"}, Fallback: "agent-place"},
		{Title: "Alpha card", Text: "the zephyr inside", Fallback: "Alpha card"},
		{Title: "Beta card", Text: "nothing here", Fallback: "Beta card"}}})
	sm := &core.SearchMatches{Attachments: map[string]bool{core.AttachmentKey("", 1): true}} // the second one in the answer
	_, extra, _ := srRichDocs(m.Doc, sm, true)
	if got := richtext.PlainText(richtext.Doc{Blocks: extra}); !strings.Contains(got, "Alpha card") || strings.Contains(got, "Beta card") {
		t.Errorf("the card shows %q, want the Alpha card only", got)
	}
}

// The card highlights the query's words as the results page did before
// it drew rich text (srHighlight, Slack's "annoying" for "ann"): a word
// a term starts, a wildcard term's stem.
func TestSearchCardHighlightsPrefixes(t *testing.T) {
	e := srNewEnv(t, false)
	const word, ts = "zqprefixhl", "1762516200.000100"
	ch := e.fs.ChannelIDs()[0]
	text := word + " the announcement about deployment day"
	e.fs.SetHook(func(method string, f url.Values) (any, bool) {
		if method != "search.modules.messages" || !strings.Contains(f.Get("query"), word) {
			return nil, false
		}
		m := map[string]any{"ts": ts, "user": fakeslack.SelfID, "iid": "I" + ts, "text": text,
			"blocks": []any{sbRichText(sbSection(text))}}
		return map[string]any{"ok": true, "items": []any{map[string]any{"channel": map[string]any{"id": ch, "name": "general", "is_channel": true},
			"messages": []any{m}}}, "pagination": map[string]any{"total_count": 1, "page": 1, "page_count": 1}}, true
	})
	t.Cleanup(func() { e.fs.SetHook(nil) })
	v, d := srView(t, e, word+" ann deplo*", false)
	d.until(t, "the card", func() bool { return len(v.cards) == 1 })
	scBuild(v, 0)
	doc := scBodyDoc(&v.cards[0])
	for _, w := range []string{word, "announcement", "deployment"} {
		if !scHasStyle(doc, w, richtext.Highlight) {
			t.Errorf("%q isn't highlighted in %q", w, scText(&v.cards[0]))
		}
	}
	for _, w := range []string{"about", "day"} {
		if scHasStyle(doc, w, richtext.Highlight) {
			t.Errorf("%q is highlighted in %q", w, scText(&v.cards[0]))
		}
	}
}

// The title shows the typed query, less the modifiers the page dropped
// since: a pill's menu clearing a typed one ("Anywhere"), "Search without
// filters". A filter a pill adds stays out of it (Slack keeps those in the
// query filter).
func TestSearchTitleFollowsDroppedModifiers(t *testing.T) {
	e := srNewEnv(t, false)
	ch := e.fs.ChannelIDs()[0]
	chName := e.h.a.ConvRef(ch).Name
	w := fakeslack.SearchWord
	v, d := srView(t, e, w+" in:<#"+ch+"> has:link", false)
	step := func(what, want string, q srQuery) {
		t.Helper()
		if what != "" {
			v.setQuery(q)
			d.until(t, what, func() bool { return v.started && !v.loading })
		}
		if x := d.scan(); !srHas(x, want) {
			t.Errorf("%s: the title isn't %q: %q", firstNonEmptyStr(what, "typed"), want, x)
		}
	}
	step("", w+" in:#"+chName+" has:link", srQuery{})
	step("Anywhere", w+" has:link", v.q.Set("in", ""))
	step("a pill's filter", w+" has:link", v.q.Toggle("is", "thread"))
	step("Search without filters", w, srWithoutFilters(v.q))
}
