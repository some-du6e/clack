package fakeslack

import (
	"encoding/json"
	"net/url"
	"slices"
	"strconv"
	"strings"

	"github.com/some-du6e/clack/internal/slack"
)

// Size.ChannelAgents gives two member channels people, agents and apps
// for channel details under agents_in_channels_improvements
// (ui/views/details_agents.go):
//   - #agents-lab (ChannelAgentsID): people (the viewer, Robin, Avery,
//     a guest, a frontline worker and a few others), and an agent and two
//     apps (Research Agent, Build Status, Deploy Bot). edge users/list
//     answers its people filter with the people only and "apps" with the
//     agents and apps; users/counts has 3 apps and 1 guest.
//   - #no-agents (ChannelNoAgentsID): the same people, no agents or apps
//     (users/list "apps" is empty, users/counts 0 apps).
//   - Triage Agent and Poll Maker are in neither; edge users/search finds
//     them (and the channel's), and conversations.invite into either
//     channel adds them to its agents and apps.
//   - conversations.kick {channel, user} out of either channel takes an
//     agent, an app or a person off its members (Kicked lists them);
//     kicking the viewer is cant_kick_self, anyone else not_in_channel.
//
// Both channels are public (#no-agents private under
// Size.ChannelAgentsPrivate).

const (
	ChannelAgentsID   = "C0FAKE00001" // #agents-lab
	ChannelNoAgentsID = "C0FAKE00002" // #no-agents

	ChannelAgentID    = "U0CAAGENT1" // "Research Agent", an agent
	ChannelAppID      = "U0CAAPPS01" // "Build Status", an app
	ChannelGuestID    = "U0CAGUEST1" // "Gale Guest", a guest
	ChannelFrontID    = "U0CAFRONT1" // "Finn Frontline", a frontline worker
	OutsideAgentID    = "U0CAOUTAG1" // "Triage Agent", not in the channels
	OutsideAppID      = "U0CAOUTAP1" // "Poll Maker", not in the channels
	channelAgentsUsed = 6            // the generated people listed after the fixed ones
)

func (s *Server) channelAgentUsers() []slack.User {
	img := func(px int, n string) string {
		return s.URL + "img/" + strconv.Itoa(px) + "x" + strconv.Itoa(px) + "/" + n + ".png"
	}
	prof := func(display, real, title, seed string) slack.Profile {
		return slack.Profile{DisplayName: display, RealName: real, Title: title, Team: TeamID, Fields: json.RawMessage(`{}`),
			Image24: img(24, seed), Image32: img(32, seed), Image48: img(48, seed), Image72: img(72, seed),
			Image192: img(192, seed), Image512: img(512, seed)}
	}
	bot := func(id, name, real, title, seed string, agent bool) slack.User {
		p := prof("", real, title, seed)
		p.BotID, p.APIAppID = "B"+id[1:], "A"+id[1:]
		u := slack.User{ID: id, TeamID: TeamID, Name: name, RealName: real, IsBot: true, Profile: p}
		if agent {
			u.AgentStatus = "consistently_an_agent"
		}
		return u
	}
	g := prof("gale", "Gale Guest", "Visiting contractor", "guest")
	f := prof("finn", "Finn Frontline", "Store associate", "front")
	return []slack.User{
		bot(ChannelAgentID, "research-agent", "Research Agent", "Answers questions from the channel", "caagent", true),
		bot(ChannelAppID, "build-status", "Build Status", "Posts CI results", "caapp", false),
		{ID: ChannelGuestID, TeamID: TeamID, Name: "gale", RealName: "Gale Guest", IsRestricted: true, Profile: g, Presence: "away"},
		{ID: ChannelFrontID, TeamID: TeamID, Name: "finn", RealName: "Finn Frontline", RestrictedUserType: "frontline_worker", Profile: f, Presence: "active"},
		bot(OutsideAgentID, "triage-agent", "Triage Agent", "Sorts incoming requests", "caout1", true),
		bot(OutsideAppID, "poll-maker", "Poll Maker", "Runs quick polls", "caout2", false),
	}
}

func (s *Server) channelAgentUser(id string) (slack.User, bool) {
	if !s.Size.ChannelAgents {
		return slack.User{}, false
	}
	for _, u := range s.channelAgentUsers() {
		if u.ID == id {
			return u, true
		}
	}
	return slack.User{}, false
}

// channelAgentsConvs names the two channels and makes them public
// (#no-agents private under Size.ChannelAgentsPrivate).
func (s *Server) channelAgentsConvs() {
	if !s.Size.ChannelAgents {
		return
	}
	for i := range s.convs {
		switch s.convs[i].ID {
		case ChannelAgentsID:
			s.convs[i].Name, s.convs[i].IsPrivate = "agents-lab", false
		case ChannelNoAgentsID:
			s.convs[i].Name, s.convs[i].IsPrivate = "no-agents", s.Size.ChannelAgentsPrivate
		}
	}
}

func channelAgentsChannel(ch string) bool { return ch == ChannelAgentsID || ch == ChannelNoAgentsID }

// channelPeople is both channels' people.
func (s *Server) channelPeople() []slack.User {
	byID := map[string]slack.User{}
	for _, u := range s.variants() {
		byID[u.ID] = u
	}
	out := []slack.User{s.users[0], byID[ProfileID], byID[AdminID]}
	for _, u := range s.channelAgentUsers() {
		if u.ID == ChannelGuestID || u.ID == ChannelFrontID {
			out = append(out, u)
		}
	}
	n := 0
	for _, u := range s.users[1:] {
		if n == channelAgentsUsed {
			break
		}
		if u.IsBot || u.Deleted || slices.ContainsFunc(out, func(o slack.User) bool { return o.ID == u.ID }) {
			continue
		}
		out = append(out, u)
		n++
	}
	return out
}

// channelPeopleIn is ch's people: channelPeople without those
// conversations.kick took out.
func (s *Server) channelPeopleIn(ch string) []slack.User {
	s.convMu.Lock()
	kicked := append([]string(nil), s.caKicks[ch]...)
	s.convMu.Unlock()
	return slices.DeleteFunc(s.channelPeople(), func(u slack.User) bool { return slices.Contains(kicked, u.ID) })
}

// channelApps is ch's agents and apps, the invited ones first.
func (s *Server) channelApps(ch string) []slack.User {
	s.convMu.Lock()
	invited := append([]string(nil), s.caInvites[ch]...)
	kicked := append([]string(nil), s.caKicks[ch]...)
	s.convMu.Unlock()
	var out []slack.User
	for i := len(invited) - 1; i >= 0; i-- {
		if u, ok := s.userByID(invited[i]); ok && u.IsBot {
			out = append(out, u)
		}
	}
	if ch == ChannelAgentsID {
		fixed := s.channelAgentUsers()
		out = append(out, fixed[0], fixed[1])
		for _, u := range s.variants() {
			if u.ID == AppUserID {
				out = append(out, u)
			}
		}
	}
	return slices.DeleteFunc(out, func(u slack.User) bool { return slices.Contains(kicked, u.ID) })
}

// channelAgentsList answers edge users/list for the two channels.
func (s *Server) channelAgentsList(edge map[string]json.RawMessage, filter string) (any, bool) {
	var chs []string
	_ = json.Unmarshal(edge["channels"], &chs)
	if !s.Size.ChannelAgents || len(chs) != 1 || !channelAgentsChannel(chs[0]) {
		return nil, false
	}
	switch filter {
	case "apps":
		return ok("results", s.channelApps(chs[0]), "next_marker", ""), true
	case "everyone AND NOT bots AND NOT apps":
		return ok("results", s.channelPeopleIn(chs[0]), "next_marker", ""), true
	}
	return ok("results", append(s.channelPeopleIn(chs[0]), s.channelApps(chs[0])...), "next_marker", ""), true
}

// channelAgentsCounts answers edge users/counts for the two channels.
func (s *Server) channelAgentsCounts(edge map[string]json.RawMessage) (any, bool) {
	var ch string
	_ = json.Unmarshal(edge["channel"], &ch)
	if !s.Size.ChannelAgents || !channelAgentsChannel(ch) {
		return nil, false
	}
	in := s.channelPeopleIn(ch)
	people, apps := len(in), len(s.channelApps(ch))
	guests := 0 // Gale, until a kick takes her out
	for _, u := range in {
		if u.IsRestricted {
			guests++
		}
	}
	return ok("counts", map[string]int{"everyone": people + apps, "people": people, "members": people - guests,
		"guests": guests, "bots": apps, "apps": apps}), true
}

// channelAgentsSearch adds the fixture's users matching q to edge
// users/search's results.
func (s *Server) channelAgentsSearch(q string, res []slack.User) []slack.User {
	if !s.Size.ChannelAgents || q == "" {
		return res
	}
	lq := strings.ToLower(q)
	for _, u := range s.channelAgentUsers() {
		if strings.Contains(strings.ToLower(u.Name), lq) || strings.Contains(strings.ToLower(u.RealName), lq) {
			res = append(res, u)
		}
	}
	return res
}

// handleChannelAgents is conversations.invite into the two channels and
// conversations.kick out of them.
func (s *Server) handleChannelAgents(method string, f url.Values) (any, bool) {
	if !s.Size.ChannelAgents || !channelAgentsChannel(f.Get("channel")) {
		return nil, false
	}
	if method == "conversations.kick" {
		ch, u := f.Get("channel"), f.Get("user")
		if u == SelfID {
			return fail("cant_kick_self"), true
		}
		in := func(m slack.User) bool { return m.ID == u }
		if !slices.ContainsFunc(s.channelApps(ch), in) && !slices.ContainsFunc(s.channelPeopleIn(ch), in) {
			return fail("not_in_channel"), true
		}
		s.convMu.Lock()
		if s.caKicks == nil {
			s.caKicks = map[string][]string{}
		}
		s.caKicks[ch] = append(s.caKicks[ch], u)
		s.convMu.Unlock()
		return ok(), true
	}
	if method != "conversations.invite" {
		return nil, false
	}
	users := strings.Split(f.Get("users"), ",")
	if f.Get("users") == "" {
		return fail("no_user"), true
	}
	ch := f.Get("channel")
	s.convMu.Lock()
	if s.caInvites == nil {
		s.caInvites = map[string][]string{}
	}
	s.caInvites[ch] = append(s.caInvites[ch], users...)
	s.convMu.Unlock()
	return ok("channel", map[string]any{"id": ch, "is_channel": true, "is_member": true}), true
}

// ChannelInvites returns the users invited into ch through the fixture.
func (s *Server) ChannelInvites(ch string) []string {
	s.convMu.Lock()
	defer s.convMu.Unlock()
	return append([]string(nil), s.caInvites[ch]...)
}

// Kicked returns the users conversations.kick took out of ch through the
// fixture.
func (s *Server) Kicked(ch string) []string {
	s.convMu.Lock()
	defer s.convMu.Unlock()
	return append([]string(nil), s.caKicks[ch]...)
}
