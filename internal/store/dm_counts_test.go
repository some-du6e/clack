package store

import (
	"testing"

	"github.com/some-du6e/clack/internal/slack"
)

// client.counts can answer an IM unread with no mentions (has_unreads
// true, mention_count 0; Karim's 2026-11-21 report was about an app's
// posts in the Slack system user's DM). Slack's applyCountsToChannels
// (client-boot mtwS) makes an IM's or group DM's unread count
// mention_count || has_unreads, which is an unmuted one's badge (fgSa
// shouldUseUnreadCountAsMentionCount). A channel keeps the answer's
// mention count, and so does a muted DM or group DM.
func TestCountsDMUnreadWithoutMentionsIsOne(t *testing.T) {
	s := New("")
	s.Muted = map[string]bool{"DMUTED": true, "GMUTED": true}
	s.SetCounts(&slack.Counts{
		Channels: []slack.CountEntry{{ID: "C1", LastRead: "1.0", Latest: "5.0", HasUnreads: true}},
		IMs: []slack.CountEntry{
			{ID: "DSYS", LastRead: "1.0", Latest: "5.0", HasUnreads: true},
			{ID: "DTWO", LastRead: "1.0", Latest: "5.0", HasUnreads: true, MentionCount: 2},
			{ID: "DREAD", LastRead: "5.0", Latest: "5.0"},
			{ID: "DMUTED", LastRead: "1.0", Latest: "5.0", HasUnreads: true},
		},
		MPIMs: []slack.CountEntry{
			{ID: "GGROUP", LastRead: "1.0", Latest: "5.0", HasUnreads: true},
			{ID: "GMUTED", LastRead: "1.0", Latest: "5.0", HasUnreads: true},
		},
	})
	for id, want := range map[string]int{"C1": 0, "DSYS": 1, "DTWO": 2, "DREAD": 0, "DMUTED": 0, "GGROUP": 1, "GMUTED": 0} {
		if e, _ := s.Count(id); e.MentionCount != want {
			t.Errorf("%s: mention count %d, want %d (%+v)", id, e.MentionCount, want, e)
		}
	}
	// a post with no user on the socket counts one more from there
	// (NoteMessage: every message in a DM is one)
	s.NoteMessage("DSYS", &slack.Message{Type: "message", Subtype: "bot_message", TS: "6.0", BotID: "B1"}, false)
	if e, _ := s.Count("DSYS"); !e.HasUnreads || e.MentionCount != 2 {
		t.Errorf("after a post: %+v, want unread with a count of 2", e)
	}
}
