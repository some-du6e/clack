package fakeslack

import (
	"encoding/json"
	"fmt"

	"github.com/some-du6e/clack/internal/slack"
)

// Channel links: Size.ChannelLinks adds #channel-links, a channel the
// viewer is in whose one message mentions every kind of channel Slack
// draws differently (BaseMrkdwnChannel): one the viewer is in, a private
// one the viewer is in, a public one they aren't in (PreviewChannelID),
// an archived one, a private one they can't see (HiddenChannelID: no
// conversations.info, edge channels/info lists it in failed_ids), one on
// another workspace of the org (OtherTeamChannelID), and a mention inside
// inline code.

const (
	ChannelLinksID     = "C0LINKS0001"
	HiddenChannelID    = "C0OTHER00003"
	OtherTeamChannelID = "C0OTHER00004"
	OtherTeamID        = "T0FAKE0002"
	ChannelLinksTS     = "1762500000.000100"
)

// ChannelLinkCase is one mention in the #channel-links message.
type ChannelLinkCase struct {
	Name string // what it is
	ID   string // the mentioned channel
}

// ChannelLinkCases are the mentions in message order. MemberID and
// PrivateID are channels of the generated set.
func (s *Server) ChannelLinkCases() []ChannelLinkCase {
	var member, private string
	for _, c := range s.convs[:min(len(s.convs), s.Size.Channels)] {
		switch {
		case c.IsPrivate && private == "":
			private = c.ID
		case !c.IsPrivate && !c.IsGeneral && member == "":
			member = c.ID
		}
	}
	return []ChannelLinkCase{
		{"member", member},
		{"private member", private},
		{"public non-member", PreviewChannelID},
		{"archived", ArchivedChannelID},
		{"hidden private", HiddenChannelID},
		{"other workspace", OtherTeamChannelID},
		{"in code", member},
	}
}

func (s *Server) addChannelLinks() {
	s.convs = append(s.convs, slack.Conversation{ID: ChannelLinksID, Name: "channel-links", IsChannel: true, IsMember: true,
		NumMembers: 5, ContextTeamID: TeamID, Purpose: &slack.Purpose{Value: "Channel mention fixtures."}})
	s.others = append(s.others, slack.Conversation{ID: OtherTeamChannelID, Name: "partner-workspace-lobby", IsChannel: true,
		Created: 1.7e9, Creator: s.users[3].ID, NumMembers: 120, ContextTeamID: OtherTeamID,
		Purpose: &slack.Purpose{Value: "A channel on another workspace of the org."}})
	var els []map[string]any
	text := ""
	for i, c := range s.ChannelLinkCases() {
		if i > 0 {
			els = append(els, map[string]any{"type": "text", "text": " · "})
			text += " · "
		}
		el := map[string]any{"type": "channel", "channel_id": c.ID}
		mk := "<#" + c.ID + ">"
		if c.Name == "in code" {
			el["style"] = map[string]any{"code": true}
			mk = "`" + mk + "`"
		}
		els = append(els, el)
		text += mk
	}
	blocks, _ := json.Marshal([]any{map[string]any{"type": "rich_text", "block_id": "links",
		"elements": []any{map[string]any{"type": "rich_text_section", "elements": els}}}})
	s.hist[ChannelLinksID] = []slack.Message{{Type: "message", TS: ChannelLinksTS, User: s.users[1].ID, Text: text,
		Blocks: blocks, ClientMsgID: fmt.Sprintf("%08x-links", 1)}}
}

// edgeChannelsInfo is edge channels/info: the conversations the viewer
// can see (their own and the public others), the rest in failed_ids.
func (s *Server) edgeChannelsInfo(ids map[string]int) any {
	res := []slack.Conversation{}
	failed := []string{}
	for id := range ids {
		if c, ok := s.conv(id); ok && (!c.IsPrivate || c.IsMember) {
			res = append(res, c)
		} else {
			failed = append(failed, id)
		}
	}
	return ok("results", res, "failed_ids", failed)
}
