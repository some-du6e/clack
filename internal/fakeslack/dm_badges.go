package fakeslack

import (
	"encoding/json"
	"fmt"
	"strconv"

	"github.com/some-du6e/clack/internal/slack"
)

// The DM badges' fixtures (Size.DMBadges), made-up content only. Four open
// DMs:
//
//   - DMBadgeSlackbotID with Slackbot: two unread messages;
//   - DMBadgeSlackID with the Slack system user (SlackUserID, "Slack"),
//     served is_bot false as the real account's users.info serves it: two
//     unread messages (Karim's two "ghost pings", 2026-11-16);
//   - DMBadgeAppID with the app's bot user (AppUserID): one unread
//     message;
//   - DMBadgePersonID with Robin (ProfileID): read.
//
// Every other DM answers read, so these are all a DM badge can count.
// client.counts reads a fixture's history against its read cursor, so a
// message PostQuiet adds counts as unread (one more mention, a DM's every
// message is one) until conversations.mark reads it (applyMark). The
// fixtures' messages are from 2026-11-05, older than the generated DMs',
// so the DMs page preselects a generated, read DM rather than one of
// these. client.dms answers their own latest messages.

const (
	DMBadgeSlackbotID = "D0DMBDG0001"
	DMBadgeSlackID    = "D0DMBDG0002"
	DMBadgeAppID      = "D0DMBDG0003"
	DMBadgePersonID   = "D0DMBDG0004"

	// SlackUserID is the Slack system user, who sends account and
	// security notices in a DM of its own (Size.DMBadges serves it).
	SlackUserID = "USLACK"
)

// dmBadgesBase is the fixtures' clock: 2026-11-05 09:00 UTC.
const dmBadgesBase = 1793955600

// dmBadgeFixture is one fixture DM: its member, its messages (oldest
// first) and how many of the newest are unread.
type dmBadgeFixture struct {
	id, user string
	texts    []string
	unread   int
}

func dmBadgeFixtures() []dmBadgeFixture {
	return []dmBadgeFixture{
		{DMBadgeSlackbotID, SlackbotID, []string{"a synthetic tip from Slackbot", "a synthetic reminder", "another synthetic reminder"}, 2},
		{DMBadgeSlackID, SlackUserID, []string{"a synthetic account notice", "a synthetic sign-in notice", "another synthetic notice"}, 2},
		{DMBadgeAppID, AppUserID, []string{"synthetic deploy started", "synthetic deploy finished"}, 1},
		{DMBadgePersonID, ProfileID, []string{"a synthetic hello", "a synthetic reply"}, 0},
	}
}

// addDMBadges adds the fixture DMs and their histories (newest first, like
// the generated ones) and records each one's read cursor.
func (s *Server) addDMBadges() {
	s.dmBadgeRead = map[string]string{}
	for i, fx := range dmBadgeFixtures() {
		s.convs = append(s.convs, slack.Conversation{ID: fx.id, IsIM: true, User: fx.user, IsOpen: true, Created: dmBadgesBase - 9e5})
		var h []slack.Message
		for j, text := range fx.texts {
			ts := fmt.Sprintf("%d.%06d", dmBadgesBase+int64(i)*3600+int64(j)*60, 100+j)
			h = append([]slack.Message{{Type: "message", TS: ts, User: fx.user, Text: text, ClientMsgID: "dmbdg-" + ts}}, h...)
		}
		s.hist[fx.id] = h
		s.dmBadgeRead[fx.id] = h[fx.unread].TS
	}
}

// dmBadgeCount is client.counts' entry for an IM under Size.DMBadges: a
// fixture's from its history and read cursor, any other IM read.
func (s *Server) dmBadgeCount(c slack.Conversation, e slack.CountEntry) slack.CountEntry {
	if !s.Size.DMBadges || !c.IsIM {
		return e
	}
	lr, ok := s.dmBadgeRead[c.ID]
	if !ok {
		e.HasUnreads, e.MentionCount, e.LastRead = false, 0, e.Latest
		return e
	}
	s.histMu.Lock()
	h := s.hist[c.ID]
	latest, mentions := lr, 0
	for _, m := range h {
		if slackTSLess(latest, m.TS) {
			latest = m.TS
		}
		if slackTSLess(lr, m.TS) && m.User != SelfID {
			mentions++
		}
	}
	s.histMu.Unlock()
	return slack.CountEntry{ID: c.ID, LastRead: lr, Latest: latest, HasUnreads: mentions > 0, MentionCount: mentions}
}

// dmsLatest is the newest message client.dms answers for DM id: a
// fixture's own (and whatever PostQuiet added), else the generated
// history's.
func (s *Server) dmsLatest(id string) (slack.Message, bool) {
	if _, ok := s.dmBadgeRead[id]; ok && s.Size.DMBadges {
		s.histMu.Lock()
		defer s.histMu.Unlock()
		if h := s.hist[id]; len(h) > 0 {
			return h[0], true
		}
		return slack.Message{}, false
	}
	if h := s.history(id); len(h) > 0 {
		return h[0], true
	}
	return slack.Message{}, false
}

// slackSystemUser is the Slack system user as users.info serves it on
// the real account: is_bot and is_app_user false, always active.
func (s *Server) slackSystemUser() slack.User {
	img := func(px int) string {
		return s.URL + "img/" + strconv.Itoa(px) + "x" + strconv.Itoa(px) + "/slack.png"
	}
	return slack.User{ID: SlackUserID, TeamID: TeamID, Name: "slack", RealName: "Slack", Presence: "active",
		Profile: slack.Profile{DisplayName: "Slack", RealName: "Slack", Team: TeamID, Fields: json.RawMessage(`{}`),
			Image24: img(24), Image32: img(32), Image48: img(48), Image72: img(72), Image192: img(192), Image512: img(512)}}
}
