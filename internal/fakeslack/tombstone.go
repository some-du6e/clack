package fakeslack

import (
	"fmt"

	"github.com/some-du6e/clack/internal/slack"
)

// The archived tombstone thread (Size.TombstoneThread): Karim's α.108
// capture of a thread opened from an archived public channel ("You are
// viewing a thread from an archived channel"), whose root was deleted
// ("This message was deleted.", 40 replies). The channel's history runs
// TombstoneOlder messages back from the root and TombstoneNewer forward,
// a minute apart, so a jump to the root loads the window around it and
// paging up from there takes several pages. Slack keeps a deleted root
// that has replies as a tombstone: subtype tombstone, from Slackbot,
// hidden. Synthetic text throughout.
const (
	TombstoneChannelID = "C0TOMBARCH1"
	TombstoneRootTS    = "1762480000.000100"
	TombstoneReplies   = 40
	TombstoneOlder     = 200
	TombstoneNewer     = 150
)

const tombstoneRootSec = 1762480000

// TombstoneReplyTS is the i-th reply (0 … TombstoneReplies-1).
func TombstoneReplyTS(i int) string { return fmt.Sprintf("%d.%06d", tombstoneRootSec+20+i*30, 200) }

// TombstoneOlderTS is the i-th message before the root (0: the oldest).
func TombstoneOlderTS(i int) string {
	return fmt.Sprintf("%d.%06d", tombstoneRootSec-(TombstoneOlder-i)*60, 300)
}

// TombstoneOlderText is that message's text.
func TombstoneOlderText(i int) string {
	return fmt.Sprintf("Archive note %d from before the thread", i)
}

func (s *Server) addTombstoneThread() {
	u := s.users[1:4]
	s.convs = append(s.convs, slack.Conversation{ID: TombstoneChannelID, Name: "old-launch-archive", IsChannel: true,
		IsMember: true, IsArchived: true, NumMembers: 9, Created: tombstoneRootSec - 400000, Creator: u[0].ID,
		ContextTeamID: TeamID})
	var hist []slack.Message // newest first, as history() builds it
	for i := TombstoneNewer - 1; i >= 0; i-- {
		ts := fmt.Sprintf("%d.%06d", tombstoneRootSec+(i+1)*1800, 400)
		hist = append(hist, slack.Message{Type: "message", TS: ts, User: u[i%3].ID, ClientMsgID: "tomb-new-" + ts,
			Text: fmt.Sprintf("Archive note %d from after the thread", i)})
	}
	var replies []slack.Message
	var repliers []string
	for i := range TombstoneReplies {
		ts := TombstoneReplyTS(i)
		user := u[i%3].ID
		replies = append(replies, slack.Message{Type: "message", TS: ts, User: user, ThreadTS: TombstoneRootTS,
			ClientMsgID: "tomb-reply-" + ts, Text: fmt.Sprintf("Thread reply %d", i+1)})
		if i < 3 {
			repliers = append(repliers, user)
		}
	}
	hist = append(hist, slack.Message{Type: "message", Subtype: "tombstone", TS: TombstoneRootTS, User: SlackbotID,
		Hidden: true, Text: "This message was deleted.", ThreadTS: TombstoneRootTS, ReplyCount: TombstoneReplies,
		ReplyUsers: repliers, LatestReply: TombstoneReplyTS(TombstoneReplies - 1)})
	for i := TombstoneOlder - 1; i >= 0; i-- {
		ts := TombstoneOlderTS(i)
		hist = append(hist, slack.Message{Type: "message", TS: ts, User: u[i%3].ID, ClientMsgID: "tomb-old-" + ts,
			Text: TombstoneOlderText(i)})
	}
	s.hist[TombstoneChannelID] = hist
	if s.quietReplies == nil {
		s.quietReplies = map[string][]slack.Message{}
	}
	s.quietReplies[TombstoneChannelID+"/"+TombstoneRootTS] = replies
}
