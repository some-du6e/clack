package fakeslack

import (
	"encoding/json"
	"fmt"
	"net/url"
	"strconv"
	"strings"
	"sync"
	"time"
	"unicode"
	"unicode/utf8"

	"github.com/some-du6e/yoloslackgoclient/internal/slack"
)

// Creating channels, as Slack's server answers the web client's create
// modal (checked read-only against Hack Club, 2026-11-08):
//
//   - conversations.validateName {name, team_id, reason} → {channel_name}
//     or one of invalid_name_required, invalid_name_maxlength (over 80
//     characters), invalid_name_punctuation (only "-" and "_"),
//     invalid_name_specials (capitals, spaces, punctuation), name_taken (a
//     channel, username or user group handle).
//   - conversations.create {name, validate_name, is_private, team_id} →
//     {channel}, after the same checks. Tests make it answer
//     restricted_action with Server.SetHook.
//   - conversations.invite {channel, users} records who was added.
//   - users.channelSections.channels.bulkUpdate {insert, remove} moves
//     channels into custom sections (users.channelSections.list shows it).
//   - stars.add {channel}.
//
// Everything stays in memory.

type chanState struct {
	mu       sync.Mutex
	created  []slack.Conversation
	invites  map[string][]string        // channel -> users added
	inserted map[string][]string        // section -> channels bulkUpdate put there
	removed  map[string]map[string]bool // section -> channels bulkUpdate took out
	starred  map[string]bool
	seq      int
}

// Created returns the channels conversations.create made, oldest first.
func (s *Server) Created() []slack.Conversation {
	s.chans.mu.Lock()
	defer s.chans.mu.Unlock()
	return append([]slack.Conversation(nil), s.chans.created...)
}

// Invited returns the users conversations.invite added to ch.
func (s *Server) Invited(ch string) []string {
	s.chans.mu.Lock()
	defer s.chans.mu.Unlock()
	return append([]string(nil), s.chans.invites[ch]...)
}

// SectionInserts returns the channels bulkUpdate put into section.
func (s *Server) SectionInserts(section string) []string {
	s.chans.mu.Lock()
	defer s.chans.mu.Unlock()
	return append([]string(nil), s.chans.inserted[section]...)
}

// Starred reports whether stars.add starred ch.
func (s *Server) Starred(ch string) bool {
	s.chans.mu.Lock()
	defer s.chans.mu.Unlock()
	return s.chans.starred[ch]
}

// usergroupHandle is the handle usergroups.list gives group i.
func (s *Server) usergroupHandle(i int) string { return s.words[i%len(s.words)] + strconv.Itoa(i) }

// channelNameError is the server's verdict on a new channel's name.
func (s *Server) channelNameError(name string) string {
	switch {
	case strings.TrimSpace(name) == "":
		return "invalid_name_required"
	case utf8.RuneCountInString(name) > 80:
		return "invalid_name_maxlength"
	case strings.Trim(name, "-_") == "":
		return "invalid_name_punctuation"
	}
	for _, r := range name {
		ok := r == '-' || r == '_' || unicode.IsDigit(r) ||
			(unicode.IsLetter(r) && !unicode.IsUpper(r) && !unicode.IsTitle(r))
		if !ok {
			return "invalid_name_specials"
		}
	}
	if s.nameTaken(name) {
		return "name_taken"
	}
	return ""
}

func (s *Server) nameTaken(name string) bool {
	for _, c := range s.convs {
		if !c.IsIM && !c.IsMPIM && c.Name == name {
			return true
		}
	}
	for _, u := range s.users {
		if u.Name == name {
			return true
		}
	}
	for i := 0; i < s.Size.Usergroups; i++ {
		if s.usergroupHandle(i) == name {
			return true
		}
	}
	s.chans.mu.Lock()
	defer s.chans.mu.Unlock()
	for _, c := range s.chans.created {
		if c.Name == name {
			return true
		}
	}
	return false
}

// createdConv finds a channel conversations.create made.
func (s *Server) createdConv(id string) (slack.Conversation, bool) {
	s.chans.mu.Lock()
	defer s.chans.mu.Unlock()
	for _, c := range s.chans.created {
		if c.ID == id {
			return c, true
		}
	}
	return slack.Conversation{}, false
}

func (s *Server) handleChannels(method string, f url.Values) (any, bool) {
	switch method {
	case "conversations.validateName":
		if t := f.Get("team_id"); t != "" && t != TeamID {
			return fail("team_not_found"), true
		}
		name := f.Get("name")
		if e := s.channelNameError(name); e != "" {
			return fail(e), true
		}
		return ok("channel_name", name), true
	case "conversations.create":
		if t := f.Get("team_id"); t != "" && t != TeamID {
			return fail("team_not_found"), true
		}
		name := f.Get("name")
		if e := s.channelNameError(name); e != "" {
			return fail(e), true
		}
		private := f.Get("is_private") == "true"
		now := time.Now().Unix()
		s.chans.mu.Lock()
		s.chans.seq++
		c := slack.Conversation{ID: fmt.Sprintf("C0NEW%05d", s.chans.seq), Name: name, IsChannel: true,
			IsPrivate: private, IsMember: true, Created: now, Creator: SelfID, NumMembers: 1}
		c.ContextTeamID = TeamID
		s.chans.created = append(s.chans.created, c)
		s.chans.mu.Unlock()
		return ok("channel", c), true
	case "conversations.invite":
		ch := f.Get("channel")
		c, found := s.createdConv(ch)
		if !found {
			return fail("channel_not_found"), true
		}
		users := strings.Split(f.Get("users"), ",")
		if f.Get("users") == "" {
			return fail("no_user"), true
		}
		s.chans.mu.Lock()
		if s.chans.invites == nil {
			s.chans.invites = map[string][]string{}
		}
		s.chans.invites[ch] = append(s.chans.invites[ch], users...)
		s.chans.mu.Unlock()
		c.NumMembers += len(users)
		return ok("channel", c), true
	case "users.channelSections.channels.bulkUpdate":
		var ins, rem []struct {
			Section  string   `json:"channel_section_id"`
			Channels []string `json:"channel_ids"`
		}
		if json.Unmarshal([]byte(f.Get("insert")), &ins) != nil || json.Unmarshal([]byte(f.Get("remove")), &rem) != nil {
			return fail("invalid_arguments"), true
		}
		s.chans.mu.Lock()
		if s.chans.inserted == nil {
			s.chans.inserted = map[string][]string{}
		}
		if s.chans.removed == nil {
			s.chans.removed = map[string]map[string]bool{}
		}
		for _, it := range rem {
			if s.chans.removed[it.Section] == nil {
				s.chans.removed[it.Section] = map[string]bool{}
			}
			drop := map[string]bool{}
			for _, ch := range it.Channels {
				s.chans.removed[it.Section][ch] = true
				drop[ch] = true
			}
			var keep []string
			for _, ch := range s.chans.inserted[it.Section] {
				if !drop[ch] {
					keep = append(keep, ch)
				}
			}
			s.chans.inserted[it.Section] = keep
		}
		for _, it := range ins {
			s.chans.inserted[it.Section] = append(s.chans.inserted[it.Section], it.Channels...)
			for _, ch := range it.Channels {
				delete(s.chans.removed[it.Section], ch)
			}
		}
		s.chans.mu.Unlock()
		return ok(), true
	case "stars.add":
		s.chans.mu.Lock()
		if s.chans.starred == nil {
			s.chans.starred = map[string]bool{}
		}
		s.chans.starred[f.Get("channel")] = true
		s.chans.mu.Unlock()
		return ok(), true
	case "stars.remove":
		s.chans.mu.Lock()
		delete(s.chans.starred, f.Get("channel"))
		s.chans.mu.Unlock()
		return ok(), true
	}
	return nil, false
}
