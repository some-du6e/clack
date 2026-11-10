package fakeslack

import (
	"encoding/json"
	"fmt"
	"net/url"
	"strings"
	"sync"

	"github.com/some-du6e/clack/internal/slack"
)

// The Unreads view's and the channel join banner's fixtures (Size.Unreads),
// made-up content only:
//
//   - #unreads-design: four read messages, then three unread ones (two in
//     a row from one person).
//   - #unreads-launch: two read messages, then 31 unread ones over two
//     days (more than one fetchHistory page of 28), one mentioning the
//     viewer.
//   - #unreads-muted: muted, with two unread messages (Slack's
//     shouldCountUnreadsInModel leaves it out of the view).
//   - #new-joiners: read; megaphone.notifications.list answers
//     education_channel_join_banner for its message_input space until a
//     dismissal is recorded.
//
// The methods: conversations.history with oldest (the messages just
// after it, like Slack's), unread.markRead {markers}, unread.collapse /
// unread.expand {channel}, megaphone.notifications.list {event,
// channel_id} and megaphone.setNotificationAsSeen {notification, action,
// channel_id}, each recorded.

const (
	UnreadsDesignID = "C0UNRD00001"
	UnreadsLaunchID = "C0UNRD00002"
	UnreadsMutedID  = "C0UNRD00003"
	JoinBannerID    = "C0JOIN00001"
)

// unreadsBase is the fixtures' clock: 2026-11-06 09:00 UTC.
const unreadsBase = 1794042000

// UnreadsCall is one recorded unread.* or megaphone.* call.
type UnreadsCall struct {
	Method string
	Form   url.Values
}

type unreadsState struct {
	mu        sync.Mutex
	lastRead  map[string]string // initial read cursors
	calls     []UnreadsCall
	dismissed map[string]bool // channel -> join banner dismissed
}

var unreadsStates sync.Map // *Server -> *unreadsState

func (s *Server) unreads() *unreadsState {
	v, _ := unreadsStates.LoadOrStore(s, &unreadsState{lastRead: map[string]string{}, dismissed: map[string]bool{}})
	return v.(*unreadsState)
}

// UnreadsCalls returns the recorded unread.* / megaphone.* calls, in order.
func (s *Server) UnreadsCalls() []UnreadsCall {
	u := s.unreads()
	u.mu.Lock()
	defer u.mu.Unlock()
	return append([]UnreadsCall(nil), u.calls...)
}

// UnreadsLastRead is a fixture channel's initial read cursor.
func (s *Server) UnreadsLastRead(ch string) string {
	u := s.unreads()
	u.mu.Lock()
	defer u.mu.Unlock()
	return u.lastRead[ch]
}

func unreadsTS(sec int64, n int) string { return fmt.Sprintf("%d.%06d", sec, 100+n) }

func (s *Server) addUnreads() {
	u := s.unreads()
	ch := func(id, name string) slack.Conversation {
		return slack.Conversation{ID: id, Name: name, IsChannel: true, IsMember: true, NumMembers: 24,
			Created: unreadsBase - 9e6, Creator: s.users[1].ID, ContextTeamID: TeamID,
			Purpose: &slack.Purpose{Value: "Synthetic fixture channel."}}
	}
	s.convs = append(s.convs, ch(UnreadsDesignID, "unreads-design"), ch(UnreadsLaunchID, "unreads-launch"),
		ch(UnreadsMutedID, "unreads-muted"), ch(JoinBannerID, "new-joiners"))
	msg := func(ts, user, text string) slack.Message {
		return slack.Message{Type: "message", TS: ts, User: user, Text: text, ClientMsgID: "unrd-" + ts}
	}
	a, b, c := s.users[1].ID, s.users[2].ID, s.users[3].ID
	// newest first, like the generated histories
	build := func(id string, read []slack.Message, unread []slack.Message) {
		var h []slack.Message
		for i := len(unread) - 1; i >= 0; i-- {
			h = append(h, unread[i])
		}
		for i := len(read) - 1; i >= 0; i-- {
			h = append(h, read[i])
		}
		s.hist[id] = h
		if len(unread) > 0 {
			u.lastRead[id] = read[len(read)-1].TS
		} else {
			u.lastRead[id] = h[0].TS
		}
	}
	t := int64(unreadsBase)
	build(UnreadsDesignID, []slack.Message{
		msg(unreadsTS(t, 1), a, "first pass at the synthetic mockups is up"),
		msg(unreadsTS(t+300, 2), b, "looks tidy, left two notes"),
		msg(unreadsTS(t+600, 3), c, "thanks both"),
		msg(unreadsTS(t+900, 4), a, "will fold the notes in tomorrow"),
	}, []slack.Message{
		msg(unreadsTS(t+86400, 5), a, "notes are folded in, second pass is ready"),
		msg(unreadsTS(t+86460, 6), a, "the spacing on the cards changed the most"),
		msg(unreadsTS(t+87000, 7), b, "nice, I'll review after lunch"),
	})
	var launch []slack.Message
	for i := 0; i < 31; i++ {
		at := t + 3600 + int64(i)*2400 // spans two days
		user := []string{a, b, c}[i%3]
		text := fmt.Sprintf("synthetic launch checklist item %d is done", i+1)
		if i == 20 {
			text = "<@" + SelfID + "> can you double-check item 21?"
		}
		launch = append(launch, msg(unreadsTS(at, 10+i), user, text))
	}
	build(UnreadsLaunchID, []slack.Message{
		msg(unreadsTS(t-7200, 8), c, "kicking off the synthetic launch"),
		msg(unreadsTS(t-3600, 9), a, "checklist is pinned"),
	}, launch)
	build(UnreadsMutedID, []slack.Message{msg(unreadsTS(t-100, 1), b, "a quiet channel")},
		[]slack.Message{msg(unreadsTS(t+500, 2), b, "still quiet"), msg(unreadsTS(t+600, 3), c, "very quiet")})
	build(JoinBannerID, []slack.Message{
		msg(unreadsTS(t-9000, 1), a, "welcome, newcomers"),
		msg(unreadsTS(t-8000, 2), b, "say hi and tell us what you are making"),
	}, nil)
	s.prefsMu.Lock()
	cur, _ := s.prefs["muted_channels"].(string)
	if cur != "" {
		cur += ","
	}
	s.prefs["muted_channels"] = cur + UnreadsMutedID
	s.prefsMu.Unlock()
}

// unreadsCount is client.counts' entry for a fixture channel.
func (s *Server) unreadsCount(id string) (slack.CountEntry, bool) {
	if !s.Size.Unreads {
		return slack.CountEntry{}, false
	}
	u := s.unreads()
	u.mu.Lock()
	lr, ok := u.lastRead[id]
	u.mu.Unlock()
	if !ok { // the generated conversations are read, so the view holds only the fixtures
		return slack.CountEntry{ID: id, LastRead: "1762516800.000000", Latest: "1762516800.000000"}, true
	}
	s.histMu.Lock()
	h := s.hist[id]
	latest := ""
	mentions := 0
	for _, m := range h {
		if latest == "" {
			latest = m.TS
		}
		if slackTSLess(lr, m.TS) && strings.Contains(m.Text, "<@"+SelfID+">") {
			mentions++
		}
	}
	s.histMu.Unlock()
	return slack.CountEntry{ID: id, LastRead: lr, Latest: latest, HasUnreads: slackTSLess(lr, latest), MentionCount: mentions}, true
}

// PostUnread adds a message from user to a fixture channel and pushes it
// as a realtime message event; it returns its ts.
func (s *Server) PostUnread(ch, user, text string) string {
	s.histMu.Lock()
	h := s.hist[ch]
	var sec int64 = unreadsBase + 3*86400
	if len(h) > 0 {
		fmt.Sscanf(h[0].TS, "%d.", &sec)
		sec += 60
	}
	ts := unreadsTS(sec, len(h))
	m := slack.Message{Type: "message", TS: ts, User: user, Text: text, ClientMsgID: "unrd-" + ts}
	s.hist[ch] = append([]slack.Message{m}, h...)
	s.histMu.Unlock()
	s.Push(map[string]any{"type": "message", "channel": ch, "user": user, "text": text, "ts": ts, "event_ts": ts,
		"client_msg_id": m.ClientMsgID})
	return ts
}

func (s *Server) handleUnreads(method string, f url.Values) (any, bool) {
	if !s.Size.Unreads {
		return nil, false
	}
	u := s.unreads()
	rec := func() {
		u.mu.Lock()
		u.calls = append(u.calls, UnreadsCall{Method: method, Form: f})
		u.mu.Unlock()
	}
	switch method {
	case "unread.markRead":
		var markers map[string]string
		if json.Unmarshal([]byte(f.Get("markers")), &markers) != nil {
			return fail("invalid_markers"), true
		}
		rec()
		s.secs.mu.Lock()
		if s.secs.marks == nil {
			s.secs.marks = map[string]string{}
		}
		for ch, ts := range markers {
			s.secs.marks[ch] = ts
		}
		s.secs.mu.Unlock()
		return ok(), true
	case "unread.collapse", "unread.expand":
		if f.Get("channel") == "" {
			return fail("channel_not_found"), true
		}
		rec()
		ch := f.Get("channel")
		s.prefsMu.Lock()
		cur, _ := s.prefs["unread_collapsed_channels"].(string)
		var ids []string
		for _, id := range strings.Split(cur, ",") {
			if id != "" && id != ch {
				ids = append(ids, id)
			}
		}
		if method == "unread.collapse" {
			ids = append(ids, ch)
		}
		s.prefs["unread_collapsed_channels"] = strings.Join(ids, ",")
		s.prefsMu.Unlock()
		return ok(), true
	case "megaphone.notifications.list":
		rec()
		var ev struct {
			Type   string   `json:"type"`
			Spaces []string `json:"spaces"`
		}
		_ = json.Unmarshal([]byte(f.Get("event")), &ev)
		out := map[string]any{}
		ch := f.Get("channel_id")
		for _, sp := range ev.Spaces {
			out[sp] = nil
			u.mu.Lock()
			gone := u.dismissed[ch]
			u.mu.Unlock()
			if sp == "message_input" && ch == JoinBannerID && !gone {
				out[sp] = map[string]any{"name": "education_channel_join_banner", "data": map[string]any{}}
			}
		}
		return ok("notifications", out), true
	case "megaphone.setNotificationAsSeen":
		rec()
		if f.Get("action") == "dismiss" && f.Get("notification") == "education_channel_join_banner" {
			u.mu.Lock()
			u.dismissed[f.Get("channel_id")] = true
			u.mu.Unlock()
		}
		return ok(), true
	}
	return nil, false
}
