package fakeslack

import (
	"encoding/json"
	"fmt"
	"strconv"
	"strings"

	"github.com/some-du6e/clack/internal/slack"
)

// The DM badges' fixtures (Size.DMBadges), made-up content only:
//
//   - DMBadgeSlackbotID with Slackbot: two unread messages;
//   - DMBadgeSlackID with the Slack system user (SlackUserID, "Slack"),
//     served is_bot false as the real account's users.info serves it: two
//     unread messages (Karim's two "ghost pings", 2026-11-16);
//   - DMBadgeAppID with the app's bot user (AppUserID): one unread
//     message;
//   - DMBadgePersonID with Robin (ProfileID): read;
//   - DMBadgeMutedID with Avery (AdminID), muted (muted_channels): two
//     unread messages;
//   - DMBadgeGroupID, a group DM with Robin and Avery: read;
//   - DMBadgeLateAppID, closed, with another app's bot user
//     (DMBadgeLateBotID): read. Nothing lists a closed, read DM, so
//     nothing asks for its member until it is unread.
//
// The generated DMs (D0FAKE…) answer read, so these are all a DM badge
// can count; other fixtures' DMs and group DMs keep their own counts.
// client.counts reads a fixture's history against its read cursor, so a
// message PostQuiet adds counts as unread (one more mention: a DM's every
// message is one) until conversations.mark reads it (applyMark). The
// fixtures' messages are from 2026-11-06, older than the generated DMs',
// so the DMs page preselects a generated, read DM rather than one of
// these. client.dms answers their own latest messages.

const (
	DMBadgeSlackbotID = "D0DMBDG0001"
	DMBadgeSlackID    = "D0DMBDG0002"
	DMBadgeAppID      = "D0DMBDG0003"
	DMBadgePersonID   = "D0DMBDG0004"
	DMBadgeMutedID    = "D0DMBDG0005"
	DMBadgeGroupID    = "G0DMBDG0006"
	DMBadgeLateAppID  = "D0DMBDG0007"

	// DMBadgeLateBotID is DMBadgeLateAppID's member, an app's bot user
	// (bot DMBadgeLateBotBot, app DMBadgeLateBotApp).
	DMBadgeLateBotID  = "U0DMBDGBOT1"
	DMBadgeLateBotBot = "B0DMBDGBOT1"
	DMBadgeLateBotApp = "A0DMBDGBOT1"

	// SlackUserID is the Slack system user, who sends account and
	// security notices in a DM of its own (Size.DMBadges serves it).
	SlackUserID = "USLACK"
)

// dmBadgesBase is the fixtures' clock: 2026-11-06 09:00 UTC.
const dmBadgesBase = 1793955600

// dmBadgeFixture is one fixture conversation: its member (an IM's) or
// members (a group DM's), its messages (oldest first, from user unless
// from says otherwise), how many of the newest are unread, and whether
// it is open.
type dmBadgeFixture struct {
	id, user string
	members  []string
	texts    []string
	from     []string
	unread   int
	closed   bool
}

func dmBadgeFixtures() []dmBadgeFixture {
	return []dmBadgeFixture{
		{id: DMBadgeSlackbotID, user: SlackbotID, texts: []string{"a synthetic tip from Slackbot", "a synthetic reminder", "another synthetic reminder"}, unread: 2},
		{id: DMBadgeSlackID, user: SlackUserID, texts: []string{"a synthetic account notice", "a synthetic sign-in notice", "another synthetic notice"}, unread: 2},
		{id: DMBadgeAppID, user: AppUserID, texts: []string{"synthetic deploy started", "synthetic deploy finished"}, unread: 1},
		{id: DMBadgePersonID, user: ProfileID, texts: []string{"a synthetic hello", "a synthetic reply"}},
		{id: DMBadgeMutedID, user: AdminID, texts: []string{"a synthetic heads-up", "a synthetic follow-up", "another synthetic follow-up"}, unread: 2},
		{id: DMBadgeGroupID, members: []string{SelfID, ProfileID, AdminID}, texts: []string{"a synthetic plan", "a synthetic thumbs up"},
			from: []string{ProfileID, AdminID}},
		{id: DMBadgeLateAppID, user: DMBadgeLateBotID, texts: []string{"a synthetic weekly digest"}, closed: true},
	}
}

// addDMBadges adds the fixture conversations and their histories (newest
// first, like the generated ones), records each one's read cursor and
// mutes DMBadgeMutedID.
func (s *Server) addDMBadges() {
	s.dmBadgeRead = map[string]string{}
	for i, fx := range dmBadgeFixtures() {
		c := slack.Conversation{ID: fx.id, IsIM: fx.user != "", User: fx.user, IsOpen: !fx.closed, Created: dmBadgesBase - 9e5}
		if fx.members != nil {
			c.Name, c.IsMPIM, c.IsGroup, c.IsPrivate = "mpdm-robin--avery.admin--self-1", true, true, true
			c.Members = append([]string(nil), fx.members...)
		}
		s.convs = append(s.convs, c)
		var h []slack.Message
		for j, text := range fx.texts {
			ts := fmt.Sprintf("%d.%06d", dmBadgesBase+int64(i)*3600+int64(j)*60, 100+j)
			user := fx.user
			if j < len(fx.from) {
				user = fx.from[j]
			}
			h = append([]slack.Message{{Type: "message", TS: ts, User: user, Text: text, ClientMsgID: "dmbdg-" + ts}}, h...)
		}
		s.hist[fx.id] = h
		s.dmBadgeRead[fx.id] = h[fx.unread].TS
	}
	s.prefsMu.Lock()
	cur, _ := s.prefs["muted_channels"].(string)
	if cur != "" {
		cur += ","
	}
	s.prefs["muted_channels"] = cur + DMBadgeMutedID
	s.prefsMu.Unlock()
}

// dmBadgeCount is client.counts' entry for a DM or group DM under
// Size.DMBadges: a fixture's from its history and read cursor, a generated
// DM's read, any other's as it came.
func (s *Server) dmBadgeCount(c slack.Conversation, e slack.CountEntry) slack.CountEntry {
	if !s.Size.DMBadges || !c.IsIM && !c.IsMPIM {
		return e
	}
	lr, ok := s.dmBadgeRead[c.ID]
	if !ok {
		if c.IsIM && strings.HasPrefix(c.ID, "D0FAKE") {
			e.HasUnreads, e.MentionCount, e.LastRead = false, 0, e.Latest
		}
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
	if _, ok := s.dmBadgeRead[id]; ok && s.Size.DMBadges || id == SystemDMID && s.Size.SystemDM {
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

// dmBadgeUser is users.info for the fixtures' own users: the Slack system
// user as the real account's users.info serves it (is_bot and
// is_app_user false, always active), and the late DM's app bot user.
func (s *Server) dmBadgeUser(id string) (slack.User, bool) {
	if !s.Size.DMBadges {
		return slack.User{}, false
	}
	img := func(px int, n string) string {
		return s.URL + "img/" + strconv.Itoa(px) + "x" + strconv.Itoa(px) + "/" + n + ".png"
	}
	prof := func(display, real, n string) slack.Profile {
		return slack.Profile{DisplayName: display, RealName: real, Team: TeamID, Fields: json.RawMessage(`{}`),
			Image24: img(24, n), Image32: img(32, n), Image48: img(48, n), Image72: img(72, n), Image192: img(192, n), Image512: img(512, n)}
	}
	switch id {
	case SlackUserID:
		return slack.User{ID: SlackUserID, TeamID: TeamID, Name: "slack", RealName: "Slack", Presence: "active",
			Profile: prof("Slack", "Slack", "slack")}, true
	case DMBadgeLateBotID:
		p := prof("", "Digest Bot", "digest")
		p.BotID, p.APIAppID = DMBadgeLateBotBot, DMBadgeLateBotApp
		return slack.User{ID: DMBadgeLateBotID, TeamID: TeamID, Name: "digestbot", RealName: "Digest Bot", IsBot: true,
			Profile: p, Presence: "active"}, true
	}
	return slack.User{}, false
}
