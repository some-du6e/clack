package fakeslack

// Group DM naming fixtures (Size.GroupDMs): group DMs with and without a
// custom name (properties.has_custom_mpdm_name), a Slack Connect one and
// one with five other members, plus what naming one calls:
// conversations.rename {channel, name, validate} answers the group DM
// under its new name (an empty name gives back the internal mpdm-… name)
// and keeps has_custom_mpdm_name for later conversations.info calls; the
// answer itself leaves the property as it was, the way Slack's
// renameChannelApi sets it on the client. enterprise.prefs.get answers
// the org prefs SetEnterpriseOrgPrefs set (none by default).

import (
	"encoding/json"
	"net/url"
	"strings"
	"sync"

	"github.com/some-du6e/clack/internal/slack"
)

// The fixtures' ids.
const (
	GroupDMNamedID   = "G0FAKEGDM01" // "Design crew": three others
	GroupDMPlainID   = "G0FAKEGDM02" // three others, no custom name
	GroupDMConnectID = "G0FAKEGDM03" // Slack Connect, named "Partners"
	GroupDMBigID     = "G0FAKEGDM04" // five others, no custom name
)

type gdmState struct {
	mu      sync.Mutex
	org     map[string]any
	renames []url.Values
}

var gdmStates sync.Map // *Server -> *gdmState

func (s *Server) gdm() *gdmState {
	v, _ := gdmStates.LoadOrStore(s, &gdmState{})
	return v.(*gdmState)
}

// GroupDMUsers are the fixtures' other members, in order: the named,
// plain and Slack Connect group DMs have the first three, the big one all
// five.
func (s *Server) GroupDMUsers() []slack.User { return s.users[4:9] }

func (s *Server) gdmInternalName(members []slack.User) string {
	hs := []string{s.users[0].Name}
	for _, u := range members {
		hs = append(hs, u.Name)
	}
	return "mpdm-" + strings.Join(hs, "--") + "-1"
}

func (s *Server) addGroupDMs() {
	u := s.GroupDMUsers()
	const created = 1762000000
	mk := func(id string, members []slack.User) slack.Conversation {
		ids := []string{SelfID}
		for _, m := range members {
			ids = append(ids, m.ID)
		}
		return slack.Conversation{ID: id, Name: s.gdmInternalName(members), IsMPIM: true, IsGroup: true, IsPrivate: true,
			IsOpen: true, IsMember: true, Created: created, Members: ids, NumMembers: len(ids)}
	}
	named := mk(GroupDMNamedID, u[:3])
	named.Name, named.Properties = "Design crew", json.RawMessage(`{"has_custom_mpdm_name":true}`)
	plain := mk(GroupDMPlainID, u[:3])
	plain.Name = strings.TrimSuffix(plain.Name, "-1") + "-2" // a second group DM of the same people
	connect := mk(GroupDMConnectID, u[:3])
	connect.Name, connect.IsExtShared, connect.Properties = "Partners", true, json.RawMessage(`{"has_custom_mpdm_name":true}`)
	big := mk(GroupDMBigID, u)
	s.convs = append(s.convs, named, plain, connect, big)
}

// SetEnterpriseOrgPrefs sets what enterprise.prefs.get answers in
// org_prefs. Set it before the client boots.
func (s *Server) SetEnterpriseOrgPrefs(kv map[string]any) {
	g := s.gdm()
	g.mu.Lock()
	g.org = kv
	g.mu.Unlock()
}

// Renames returns the conversations.rename calls the fake answered.
func (s *Server) Renames() []url.Values {
	g := s.gdm()
	g.mu.Lock()
	defer g.mu.Unlock()
	return append([]url.Values(nil), g.renames...)
}

func (s *Server) handleGroupDMs(method string, f url.Values) (any, bool) {
	switch method {
	case "enterprise.prefs.get":
		g := s.gdm()
		g.mu.Lock()
		org := g.org
		g.mu.Unlock()
		if org == nil {
			org = map[string]any{}
		}
		return ok("org_prefs", org, "locked_prefs", map[string]any{}, "unlocked_prefs", map[string]any{}), true
	case "conversations.rename":
		g := s.gdm()
		g.mu.Lock()
		g.renames = append(g.renames, f)
		g.mu.Unlock()
		return s.renameGroupDM(f.Get("channel"), f.Get("name"))
	}
	return nil, false
}

// renameGroupDM renames one of the fixtures (other ids: not handled).
func (s *Server) renameGroupDM(id, name string) (any, bool) {
	s.convMu.Lock()
	defer s.convMu.Unlock()
	for i := range s.convs {
		c := &s.convs[i]
		if c.ID != id || !c.IsMPIM {
			continue
		}
		answer := *c
		name = strings.TrimSpace(name)
		var members []slack.User
		for _, m := range c.Members {
			if m == SelfID {
				continue
			}
			for _, u := range s.users {
				if u.ID == m {
					members = append(members, u)
				}
			}
		}
		if name == "" {
			c.Name = s.gdmInternalName(members)
		} else {
			c.Name = name
		}
		c.Properties = json.RawMessage(`{"has_custom_mpdm_name":` + map[bool]string{true: "true", false: "false"}[name != ""] + `}`)
		answer.Name = c.Name
		return ok("channel", answer), true
	}
	return nil, false
}
