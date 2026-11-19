package fakeslack

import (
	"fmt"
	"slices"

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

// canvasMentionBlocks is the canvas: its title, a roster long enough to
// put the mention's paragraph below the fold (a jump to the mention
// scrolls), then the paragraph that mentions the viewer.
func canvasMentionBlocks() []canvas.Block {
	blocks := []canvas.Block{{ID: "temp:C:CNVM0000001", Kind: canvas.Heading1, Spans: []richtext.Span{
		{Kind: richtext.Text, Text: "Pet roster "}, {Kind: richtext.Emoji, ID: CanvasMentionEmoji}, {Kind: richtext.Text, Text: " tribute"}}}}
	for i := 1; i <= 24; i++ {
		blocks = append(blocks, canvas.Block{ID: fmt.Sprintf("temp:C:CNVF%07d", i), Kind: canvas.Paragraph, Spans: []richtext.Span{
			{Kind: richtext.Text, Text: fmt.Sprintf("Roster entry %d: a synthetic pet, fed and walked on schedule.", i)}}})
	}
	return append(blocks, canvas.Block{ID: CanvasMentionSection, Kind: canvas.Paragraph, Spans: []richtext.Span{
		{Kind: richtext.User, ID: SelfID, Text: "@self"}, {Kind: richtext.Text, Text: " pays tribute on Tuesday, November 10."}}})
}

// AddCanvasMention adds the canvas, its file channel and emoji, the
// document_mention message and its Activity entry (newest in the feed).
func (s *Server) AddCanvasMention() {
	s.emoji[CanvasMentionEmoji] = s.CanvasMentionEmojiURL()
	s.AddCanvas(FakeCanvas{ID: CanvasMentionFile, Title: CanvasMentionTitle, TitleBlocks: CanvasMentionTitleBlocks(),
		Created: 1762600000, Updated: 1762702000, Creator: CanvasMentionUser,
		Doc: canvas.Doc{Blocks: canvasMentionBlocks()}})
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
	// conversations.replies on the mention answers the message alone, as
	// Slack's does for a message without replies: the thread view on it
	// (RedirectFileChannel's toMessage)
	if s.quietReplies == nil {
		s.quietReplies = map[string][]slack.Message{}
	}
	s.quietReplies[CanvasMentionChannel+"/"+CanvasMentionTS] = nil
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

// Canvas comments in Activity (opt-in, AddCanvasCommentActivity), made-up
// content only: a reply in the Launch plan canvas's (CanvasChannel)
// comment thread CommentOpen @-mentions the viewer (an at_user item, no
// document_mention), and CommentUnread's unread reply is a thread_v2 item.
// Both sit in the canvas's file channel, FileChanLaunch.
const (
	CanvasCommentKey       = "fa-canvas-comment"        // the at_user reply
	CanvasCommentTS        = "1762510300.000400"        // the reply, in CommentOpen
	CanvasCommentThreadKey = "fa-canvas-comment-thread" // CommentUnread's thread_v2
	CanvasCommentLatest    = "1762520200.000300"        // CommentUnread's unread reply
)

// AddCanvasCommentActivity adds the reply to CommentOpen and the two
// Activity entries.
func (s *Server) AddCanvasCommentActivity() {
	reply := commentReply(CanvasCommentTS, CommentOpen, "U0FAKE00001", "<@"+SelfID+"> can we keep it on Wednesday?")
	cs := s.comments()
	cs.mu.Lock()
	msgs := cs.msgs[FileChanLaunch]
	at := len(msgs)
	for i := range msgs {
		m := &msgs[i]
		if m.TS == CommentOpen && m.Subtype == "document_comment_root" {
			m.ReplyCount++
			m.LatestReply = CanvasCommentTS
			if !slices.Contains(m.ReplyUsers, reply.User) {
				m.ReplyUsers = append(m.ReplyUsers, reply.User)
				m.ReplyUsersN = len(m.ReplyUsers)
			}
		}
		if m.ThreadTS == CommentOpen {
			at = i + 1
		}
	}
	cs.msgs[FileChanLaunch] = slices.Insert(msgs, at, reply)
	cs.mu.Unlock()
	reply.Channel = FileChanLaunch
	v := s.views()
	v.mu.Lock()
	st := v.act()
	st.extra = append(st.extra,
		map[string]any{"key": CanvasCommentKey, "feed_ts": CanvasCommentTS, "is_unread": true, "is_bot": false,
			"item": map[string]any{"type": "at_user", "message": map[string]any{"ts": CanvasCommentTS, "channel": FileChanLaunch,
				"thread_ts": CommentOpen, "author_user_id": "U0FAKE00001"}}},
		map[string]any{"key": CanvasCommentThreadKey, "feed_ts": CanvasCommentLatest, "is_unread": true, "is_bot": false,
			"item": map[string]any{"type": "thread_v2", "bundle_info": map[string]any{"payload": map[string]any{"thread_entry": map[string]any{
				"channel_id": FileChanLaunch, "latest_ts": CanvasCommentLatest, "thread_ts": CommentUnread, "min_unread_ts": CanvasCommentLatest,
				"unread_msg_count": 1}}}}})
	st.extraMsgs = append(st.extraMsgs, reply)
	v.mu.Unlock()
}
