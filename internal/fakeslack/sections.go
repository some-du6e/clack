package fakeslack

import (
	"encoding/json"
	"fmt"
	"net/url"
	"strconv"
	"strings"
	"sync"
)

// Sidebar sections, as Slack's server answers the web client's section
// menus (method names and arguments from the web client, build
// a84662d2d135, 2026-11-08; nothing here was sent to Slack):
//
//   - users.channelSections.list: the account's sections, chained by
//     next_channel_section_id, each with channel_ids_page.
//   - users.channelSections.create {name, emoji, next_channel_section_id}
//     → {channel_section_id}; the new section goes before next (first
//     without one).
//   - users.channelSections.set {channel_section_id, name, emoji}.
//   - users.channelSections.delete {channel_section_id}, or
//     section_not_found.
//   - users.channelSections.createLink {section_name, emoji, channels} →
//     {url}.
//   - users.prefs.setNotifications global=false {channel_ids, name, value}
//     or {channel_ids, prefs}: all_notifications_prefs.channels.
//   - conversations.mark {channel, ts}: client.counts reads it back.
//
// Everything stays in memory.

type fakeSection struct {
	ID, Name, Type, Emoji, Next string
	base                        []string // generated members (the default sections)
}

type secState struct {
	mu    sync.Mutex
	init  bool
	list  []*fakeSection
	seq   int
	links []url.Values
	marks map[string]string // channel -> last conversations.mark ts
}

// SectionCall is a section as users.channelSections.list shows it now.
type SectionCall struct {
	ID, Name, Type, Emoji, Next string
	Channels                    []string
}

func (s *Server) sectionsLocked() []*fakeSection {
	st := &s.secs
	if st.init {
		return st.list
	}
	st.init = true
	ids := s.ChannelIDs()
	per := max(1, len(ids)/6)
	names := []string{"Starred", "Hack Club", "Projects", "Events", "Random stuff", "Bots"}
	for i, n := range names {
		lo, hi := i*per, min(len(ids), (i+1)*per)
		if i == len(names)-1 {
			hi = len(ids)
		}
		typ := "standard"
		if i == 0 {
			typ = "stars"
		}
		st.list = append(st.list, &fakeSection{ID: fmt.Sprintf("L0FAKE%02d", i), Name: n, Type: typ, base: ids[lo:hi]})
	}
	st.list = append(st.list, &fakeSection{ID: "L0FAKECH", Type: "channels"},
		&fakeSection{ID: "L0FAKEDM", Type: "direct_messages"}, &fakeSection{ID: "L0FAKETMP", Type: "temporary_channels"})
	relink(st.list)
	return st.list
}

func relink(l []*fakeSection) {
	for i, x := range l {
		x.Next = ""
		if i+1 < len(l) {
			x.Next = l[i+1].ID
		}
	}
}

func (s *Server) sectionMembers(x *fakeSection) []string {
	s.chans.mu.Lock()
	ins := append([]string(nil), s.chans.inserted[x.ID]...)
	rem := s.chans.removed[x.ID]
	s.chans.mu.Unlock()
	seen := map[string]bool{}
	var out []string
	for _, id := range append(ins, x.base...) {
		if seen[id] || rem[id] {
			continue
		}
		seen[id] = true
		out = append(out, id)
	}
	if out == nil {
		out = []string{}
	}
	return out
}

// Sections returns the sections users.channelSections.list serves, in order.
func (s *Server) Sections() []SectionCall {
	s.secs.mu.Lock()
	l := append([]*fakeSection(nil), s.sectionsLocked()...)
	s.secs.mu.Unlock()
	out := make([]SectionCall, 0, len(l))
	for _, x := range l {
		out = append(out, SectionCall{ID: x.ID, Name: x.Name, Type: x.Type, Emoji: x.Emoji, Next: x.Next, Channels: s.sectionMembers(x)})
	}
	return out
}

// SectionLinks returns the users.channelSections.createLink calls.
func (s *Server) SectionLinks() []url.Values {
	s.secs.mu.Lock()
	defer s.secs.mu.Unlock()
	return append([]url.Values(nil), s.secs.links...)
}

// Marked is the ts conversations.mark last set on ch ("" for none).
func (s *Server) Marked(ch string) string {
	s.secs.mu.Lock()
	defer s.secs.mu.Unlock()
	return s.secs.marks[ch]
}

// applyMark is client.counts after conversations.mark: a cursor at or past
// the latest message reads the conversation.
func (s *Server) applyMark(id string, lastRead, latest string, unread bool, mentions int) (string, bool, int) {
	s.secs.mu.Lock()
	m, ok := s.secs.marks[id]
	s.secs.mu.Unlock()
	if !ok {
		return lastRead, unread, mentions
	}
	if !slackTSLess(m, latest) {
		return m, false, 0
	}
	return m, true, mentions
}

func (s *Server) handleSections(method string, f url.Values) (any, bool) {
	switch method {
	case "users.channelSections.list":
		var secs []map[string]any
		for _, x := range s.Sections() {
			m := map[string]any{"channel_section_id": x.ID, "name": x.Name, "type": x.Type, "emoji": x.Emoji,
				"next_channel_section_id": x.Next}
			if x.Type == "standard" || x.Type == "stars" {
				m["channel_ids_page"] = map[string]any{"channel_ids": x.Channels, "count": len(x.Channels)}
			}
			secs = append(secs, m)
		}
		return ok("channel_sections", secs), true
	case "users.channelSections.create":
		name := f.Get("name")
		if strings.TrimSpace(name) == "" {
			return fail("invalid_name"), true
		}
		s.secs.mu.Lock()
		l := s.sectionsLocked()
		for _, x := range l {
			if strings.EqualFold(x.Name, name) {
				s.secs.mu.Unlock()
				return fail("already_exists"), true
			}
		}
		s.secs.seq++
		x := &fakeSection{ID: fmt.Sprintf("L0NEW%04d", s.secs.seq), Name: name, Type: "standard", Emoji: f.Get("emoji")}
		at := 0
		if n := f.Get("next_channel_section_id"); n != "" {
			for i, y := range l {
				if y.ID == n {
					at = i
				}
			}
		}
		l = append(l[:at], append([]*fakeSection{x}, l[at:]...)...)
		relink(l)
		s.secs.list = l
		s.secs.mu.Unlock()
		return ok("channel_section_id", x.ID), true
	case "users.channelSections.set":
		id := f.Get("channel_section_id")
		s.secs.mu.Lock()
		defer s.secs.mu.Unlock()
		for _, x := range s.sectionsLocked() {
			if x.ID == id {
				if _, has := f["name"]; has {
					x.Name, x.Emoji = f.Get("name"), f.Get("emoji")
				}
				return ok(), true
			}
		}
		return fail("section_not_found"), true
	case "users.channelSections.delete":
		id := f.Get("channel_section_id")
		s.secs.mu.Lock()
		defer s.secs.mu.Unlock()
		l := s.sectionsLocked()
		for i, x := range l {
			if x.ID == id {
				if x.Type != "standard" && x.Type != "stars" {
					return fail("invalid_section_type"), true
				}
				l = append(l[:i], l[i+1:]...)
				relink(l)
				s.secs.list = l
				return ok(), true
			}
		}
		return fail("section_not_found"), true
	case "users.channelSections.createLink":
		s.secs.mu.Lock()
		s.secs.links = append(s.secs.links, f)
		n := len(s.secs.links)
		s.secs.mu.Unlock()
		return ok("url", fmt.Sprintf("%s/section/fake%04d", strings.TrimSuffix(s.URL, "/"), n)), true
	case "conversations.mark":
		s.secs.mu.Lock()
		if s.secs.marks == nil {
			s.secs.marks = map[string]string{}
		}
		s.secs.marks[f.Get("channel")] = f.Get("ts")
		s.secs.mu.Unlock()
		return ok(), true
	case "users.prefs.setNotifications":
		if f.Get("global") == "true" {
			return nil, false
		}
		ids := strings.Split(f.Get("channel_ids"), ",")
		if f.Get("channel_ids") == "" {
			ids = []string{f.Get("channel_id")}
		}
		var ps []struct{ Name, Value string }
		if n := f.Get("name"); n != "" {
			ps = append(ps, struct{ Name, Value string }{n, f.Get("value")})
		} else if json.Unmarshal([]byte(f.Get("prefs")), &ps) != nil {
			return fail("invalid_prefs"), true
		}
		for _, id := range ids {
			if id == "" {
				return fail("channel_not_found"), true
			}
			for _, p := range ps {
				s.setChannelNotify(id, p.Name, p.Value)
			}
		}
		return ok(), true
	}
	return nil, false
}

// setChannelNotify writes one channel's notification override into
// all_notifications_prefs.channels (muted and the other flags as booleans,
// as Slack serves them).
func (s *Server) setChannelNotify(ch, name, value string) {
	s.prefsMu.Lock()
	defer s.prefsMu.Unlock()
	var n map[string]any
	if str, _ := s.prefs["all_notifications_prefs"].(string); str != "" {
		_ = json.Unmarshal([]byte(str), &n)
	}
	if n == nil {
		n = map[string]any{}
	}
	cs, _ := n["channels"].(map[string]any)
	if cs == nil {
		cs = map[string]any{}
	}
	c, _ := cs[ch].(map[string]any)
	if c == nil {
		c = map[string]any{}
	}
	var v any = value
	switch name {
	case "muted", "follow_all_threads", "suppress_at_channel", "badge_all_unreads", "desktop_push_enabled":
		b, _ := strconv.ParseBool(value)
		v = b
	}
	c[name] = v
	cs[ch] = c
	n["channels"] = cs
	b, _ := json.Marshal(n)
	s.prefs["all_notifications_prefs"] = string(b)
	if name == "muted" { // the legacy list too
		cur, _ := s.prefs["muted_channels"].(string)
		var ids []string
		for _, id := range strings.Split(cur, ",") {
			if id != "" && id != ch {
				ids = append(ids, id)
			}
		}
		if v == true {
			ids = append(ids, ch)
		}
		s.prefs["muted_channels"] = strings.Join(ids, ",")
	}
}
