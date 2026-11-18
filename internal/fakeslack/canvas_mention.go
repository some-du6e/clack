package fakeslack

import (
	"github.com/some-du6e/clack/internal/canvas"
	"github.com/some-du6e/clack/internal/richtext"
	"github.com/some-du6e/clack/internal/slack"
)

// A canvas @-mention of the viewer in Activity (Karim, α.102: "kinda
// messed up on canvas pings"), opt-in (AddCanvasMention), made-up content
// only. Slack's entry is an at_user item whose message, subtype
// document_mention, sits in the canvas's hidden file channel: the file id
// with F swapped for C, is_file and private, named after the canvas's
// title with its :shortcodes:, the viewer a member. The message carries
// document_mention {file_id, section_id, mentioning_user_ids}. The
// canvas's title_blocks hold an emoji element the fake's emoji.list knows.

const (
	CanvasMentionKey     = "fa-canvas-mention"
	CanvasMentionFile    = "F0FAKECNVM1"
	CanvasMentionChannel = "C0FAKECNVM1" // the file channel
	CanvasMentionTS      = "1762702100.000100"
	CanvasMentionSection = "temp:C:CNVM0000002" // the paragraph that mentions the viewer
	CanvasMentionEmoji   = "pet-fixture"
	CanvasMentionUser    = "U0FAKE00001" // who mentioned the viewer
	// CanvasMentionTitle is the title string (files.info title, the file
	// channel's name), its emoji a shortcode
	CanvasMentionTitle = "Pet roster :" + CanvasMentionEmoji + ": tribute"
)

// CanvasMentionTitleBlocks is the canvas's title_blocks: the title with
// its emoji as an emoji element.
func CanvasMentionTitleBlocks() []any {
	return []any{map[string]any{"type": "rich_text", "block_id": "title", "elements": []any{
		map[string]any{"type": "rich_text_section", "elements": []any{
			map[string]any{"type": "text", "text": "Pet roster "},
			map[string]any{"type": "emoji", "name": CanvasMentionEmoji},
			map[string]any{"type": "text", "text": " tribute"},
		}},
	}}}
}

// CanvasMentionEmojiURL is the fixture emoji's image on the fake.
func (s *Server) CanvasMentionEmojiURL() string {
	return s.URL + "img/64x64/" + TeamID + "/" + CanvasMentionEmoji + "/00000000000000cf.png"
}

// AddCanvasMention adds the canvas, its file channel and emoji, the
// document_mention message and its Activity entry (newest in the feed).
func (s *Server) AddCanvasMention() {
	s.emoji[CanvasMentionEmoji] = s.CanvasMentionEmojiURL()
	s.AddCanvas(FakeCanvas{ID: CanvasMentionFile, Title: CanvasMentionTitle, TitleBlocks: CanvasMentionTitleBlocks(),
		Created: 1762600000, Updated: 1762702000, Creator: CanvasMentionUser,
		Doc: canvas.Doc{Blocks: []canvas.Block{
			{ID: "temp:C:CNVM0000001", Kind: canvas.Heading1, Spans: []richtext.Span{
				{Kind: richtext.Text, Text: "Pet roster "}, {Kind: richtext.Emoji, ID: CanvasMentionEmoji}, {Kind: richtext.Text, Text: " tribute"}}},
			{ID: CanvasMentionSection, Kind: canvas.Paragraph, Spans: []richtext.Span{
				{Kind: richtext.User, ID: SelfID, Text: "@self"}, {Kind: richtext.Text, Text: " pays tribute on Tuesday, November 10."}}},
		}}})
	s.convMu.Lock()
	s.others = append(s.others, slack.Conversation{ID: CanvasMentionChannel, Name: CanvasMentionTitle, IsChannel: true, IsGroup: true,
		IsPrivate: true, IsFile: true, IsMember: true, NumMembers: 2, ContextTeamID: TeamID})
	s.convMu.Unlock()
	m := slack.Message{Type: "message", Subtype: "document_mention", Channel: CanvasMentionChannel, TS: CanvasMentionTS,
		User: CanvasMentionUser, Text: "<@" + SelfID + "> pays tribute on Tuesday, November 10.",
		DocumentMention: &slack.MessageDocumentMention{FileID: CanvasMentionFile, SectionID: CanvasMentionSection,
			MentioningUserIDs: []string{CanvasMentionUser}}}
	s.histMu.Lock()
	s.hist[CanvasMentionChannel] = append([]slack.Message{m}, s.histLocked(CanvasMentionChannel)...)
	s.histMu.Unlock()
	v := s.views()
	v.mu.Lock()
	st := v.act()
	st.extra = append(st.extra, map[string]any{"key": CanvasMentionKey, "feed_ts": CanvasMentionTS, "is_unread": true, "is_bot": false,
		"item": map[string]any{"type": "at_user", "message": map[string]any{"ts": CanvasMentionTS, "channel": CanvasMentionChannel,
			"author_user_id": CanvasMentionUser}}})
	st.extraMsgs = append(st.extraMsgs, m)
	v.mu.Unlock()
}
