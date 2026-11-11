package fakeslack

import (
	"encoding/json"
	"slices"
	"strconv"

	"github.com/some-du6e/clack/internal/slack"
)

// Size.PreviewAgents puts agents and apps into #lounge-preview
// (PreviewChannelID), the channel the viewer previews, for Slack's
// channel_join_experience_with_agents card and channel_header_with_agents
// count (core/channel_agents.go):
//   - edge users/counts for it: 4,199 people and 11 apps;
//   - edge users/list with filter "apps" for it: an agent and two apps;
//   - its two newest messages are the agent's and one app's, so they are
//     the agents seen active (the card's faces).
//
// Other channels keep the default counts (12 apps, Deploy Bot listed).

// The fixture's bot users.
const (
	PreviewAgentID = "U0PVAGENT01" // "Standup Agent", an agent
	PreviewApp1ID  = "U0PVAPPS002" // "Changelog"
	PreviewApp2ID  = "U0PVAPPS003" // "Triage Helper"
)

// PreviewPeople and PreviewApps are #lounge-preview's membership counts
// under PreviewAgents.
const (
	PreviewPeople = 4199
	PreviewApps   = 11
)

func (s *Server) previewAgentUsers() []slack.User {
	img := func(px int, n string) string {
		return s.URL + "img/" + strconv.Itoa(px) + "x" + strconv.Itoa(px) + "/" + n + ".png"
	}
	bot := func(id, name, real, seed, botID, appID string) slack.User {
		p := slack.Profile{RealName: real, Team: TeamID, Fields: json.RawMessage(`{}`), BotID: botID, APIAppID: appID,
			Image24: img(24, seed), Image32: img(32, seed), Image48: img(48, seed), Image72: img(72, seed),
			Image192: img(192, seed), Image512: img(512, seed)}
		return slack.User{ID: id, TeamID: TeamID, Name: name, RealName: real, IsBot: true, Profile: p, Presence: "active"}
	}
	return []slack.User{
		bot(PreviewAgentID, "standup-agent", "Standup Agent", "pvagent", "B0PVAGENT01", "A0PVAGENT01"),
		bot(PreviewApp1ID, "changelog", "Changelog", "pvapp1", "B0PVAPPS002", "A0PVAPPS002"),
		bot(PreviewApp2ID, "triage-helper", "Triage Helper", "pvapp2", "B0PVAPPS003", "A0PVAPPS003"),
	}
}

func (s *Server) previewAgentUser(id string) (slack.User, bool) {
	if !s.Size.PreviewAgents {
		return slack.User{}, false
	}
	for _, u := range s.previewAgentUsers() {
		if u.ID == id {
			return u, true
		}
	}
	return slack.User{}, false
}

// previewAgentCounts answers edge users/counts for #lounge-preview.
func (s *Server) previewAgentCounts(edge map[string]json.RawMessage) (any, bool) {
	var ch string
	_ = json.Unmarshal(edge["channel"], &ch)
	if !s.Size.PreviewAgents || ch != PreviewChannelID {
		return nil, false
	}
	return ok("counts", map[string]int{"everyone": PreviewPeople + PreviewApps, "people": PreviewPeople,
		"members": PreviewPeople, "guests": 0, "bots": PreviewApps, "apps": PreviewApps}), true
}

// previewAgentList answers edge users/list's "apps" filter for
// #lounge-preview.
func (s *Server) previewAgentList(edge map[string]json.RawMessage) (any, bool) {
	var chs []string
	_ = json.Unmarshal(edge["channels"], &chs)
	if !s.Size.PreviewAgents || !slices.Contains(chs, PreviewChannelID) {
		return nil, false
	}
	return ok("results", s.previewAgentUsers(), "next_marker", ""), true
}

// previewAgentHistory makes the agent and the first app the authors of
// #lounge-preview's two newest messages (msgs newest first).
func (s *Server) previewAgentHistory(ch string, msgs []slack.Message) {
	if !s.Size.PreviewAgents || ch != PreviewChannelID || len(msgs) < 2 {
		return
	}
	for i, u := range s.previewAgentUsers()[:2] {
		msgs[i].User, msgs[i].BotID = u.ID, u.Profile.BotID
		msgs[i].Subtype = ""
	}
}
