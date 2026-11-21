package fakeslack

// Channel details' Settings writes (views/details_managing.go,
// views/details_retention.go), answered in memory. They change a channel
// for everyone, so Clack's tests send them here and never to a real
// workspace:
//
//   - conversations.archive {channel}: the channel is_archived (its
//     membership stays, as Slack's channelArchived keeps it), then the
//     channel_archive (group_archive for a private channel) event.
//     #general answers cant_archive_general, an archived channel
//     already_archived.
//   - conversations.unarchive {channel}: not_archived unless archived,
//     then channel_unarchive / group_unarchive.
//   - conversations.convertToPrivate / convertToPublic {channel}: the
//     channel's is_private flips (#general can't be made private:
//     method_not_supported_for_channel_type; an archived channel
//     is_archived).
//   - conversations.delete {channel}: the channel goes away
//     (conversations.info: channel_not_found), then channel_deleted /
//     group_deleted.
//   - conversations.getRetention {channel} → {retention: {retention_type,
//     retention_duration}} (0, 0 until set); conversations.setRetention
//     {channel, retention_type 0 | 1, retention_duration days for 1}
//     keeps it (invalid_retention_type, invalid_retention_duration).
//   - conversations.export {channel}: recorded.
//
// cant_archive_general, already_archived and not_archived are Slack's
// documented codes; the others are fakeslack's own. ManageCalls lists
// every write in order with its form.

import (
	"net/url"
	"slices"
	"strconv"
	"sync"
	"time"

	"github.com/some-du6e/clack/internal/slack"
)

// ManageCall is one of the writes above as it arrived.
type ManageCall struct {
	Method, Channel string
	Form            url.Values
}

// ChannelRetention is a channel's conversations.getRetention answer.
type ChannelRetention struct {
	Type, Duration int
}

type manageState struct {
	mu        sync.Mutex
	calls     []ManageCall
	retention map[string]ChannelRetention
	deleted   map[string]bool
}

var manageStates sync.Map // *Server → *manageState

func (s *Server) manage() *manageState {
	v, _ := manageStates.LoadOrStore(s, &manageState{retention: map[string]ChannelRetention{}, deleted: map[string]bool{}})
	return v.(*manageState)
}

// ManageCalls returns the Settings writes so far, oldest first.
func (s *Server) ManageCalls() []ManageCall {
	m := s.manage()
	m.mu.Lock()
	defer m.mu.Unlock()
	return slices.Clone(m.calls)
}

// ManageCallsTo returns the calls of one method.
func (s *Server) ManageCallsTo(method string) []ManageCall {
	var out []ManageCall
	for _, c := range s.ManageCalls() {
		if c.Method == method {
			out = append(out, c)
		}
	}
	return out
}

// SetChannelRetention sets what conversations.getRetention answers for ch.
func (s *Server) SetChannelRetention(ch string, r ChannelRetention) {
	m := s.manage()
	m.mu.Lock()
	m.retention[ch] = r
	m.mu.Unlock()
}

// Retention is ch's retention as conversations.setRetention left it.
func (s *Server) Retention(ch string) ChannelRetention {
	m := s.manage()
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.retention[ch]
}

// Conv is the channel as conversations.info would answer it now.
func (s *Server) Conv(ch string) (slack.Conversation, bool) {
	if s.manage().isDeleted(ch) {
		return slack.Conversation{}, false
	}
	return s.conv(ch)
}

func (m *manageState) isDeleted(ch string) bool {
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.deleted[ch]
}

// editConv changes ch in place wherever it lives; false when unknown.
func (s *Server) editConv(ch string, f func(c *slack.Conversation)) bool {
	s.convMu.Lock()
	defer s.convMu.Unlock()
	for _, list := range []*[]slack.Conversation{&s.convs, &s.others} {
		for i := range *list {
			if (*list)[i].ID == ch {
				f(&(*list)[i])
				return true
			}
		}
	}
	return false
}

// dropConv removes ch from the workspace.
func (s *Server) dropConv(ch string) {
	s.convMu.Lock()
	s.convs = slices.DeleteFunc(s.convs, func(c slack.Conversation) bool { return c.ID == ch })
	s.others = slices.DeleteFunc(s.others, func(c slack.Conversation) bool { return c.ID == ch })
	s.convMu.Unlock()
}

func (s *Server) pushChannelEvent(typ, ch string) {
	ts := strconv.FormatFloat(float64(time.Now().UnixMicro())/1e6, 'f', 6, 64)
	s.Push(map[string]any{"type": typ, "channel": ch, "user": SelfID, "event_ts": ts})
}

func (s *Server) handleChannelManage(method string, f url.Values) (any, bool) {
	switch method {
	case "conversations.archive", "conversations.unarchive", "conversations.convertToPrivate",
		"conversations.convertToPublic", "conversations.delete", "conversations.getRetention",
		"conversations.setRetention", "conversations.export":
	default:
		return nil, false
	}
	ch := f.Get("channel")
	m := s.manage()
	if method != "conversations.getRetention" {
		m.mu.Lock()
		m.calls = append(m.calls, ManageCall{Method: method, Channel: ch, Form: f})
		m.mu.Unlock()
	}
	c, found := s.Conv(ch)
	if !found {
		return fail("channel_not_found"), true
	}
	group := func(chType, grpType string) string {
		if c.IsPrivate {
			return grpType
		}
		return chType
	}
	switch method {
	case "conversations.archive":
		switch {
		case c.IsGeneral:
			return fail("cant_archive_general"), true
		case c.IsArchived:
			return fail("already_archived"), true
		}
		s.editConv(ch, func(c *slack.Conversation) { c.IsArchived = true })
		s.pushChannelEvent(group("channel_archive", "group_archive"), ch)
		return ok(), true
	case "conversations.unarchive":
		if !c.IsArchived {
			return fail("not_archived"), true
		}
		s.editConv(ch, func(c *slack.Conversation) { c.IsArchived = false })
		s.pushChannelEvent(group("channel_unarchive", "group_unarchive"), ch)
		return ok(), true
	case "conversations.convertToPrivate", "conversations.convertToPublic":
		private := method == "conversations.convertToPrivate"
		switch {
		case c.IsArchived:
			return fail("is_archived"), true
		case private && c.IsGeneral:
			return fail("method_not_supported_for_channel_type"), true
		}
		s.editConv(ch, func(c *slack.Conversation) { c.IsPrivate, c.IsGroup = private, private })
		return ok(), true
	case "conversations.delete":
		if c.IsGeneral {
			return fail("cant_delete_general"), true
		}
		m.mu.Lock()
		m.deleted[ch] = true
		m.mu.Unlock()
		s.dropConv(ch)
		s.pushChannelEvent(group("channel_deleted", "group_deleted"), ch)
		return ok(), true
	case "conversations.getRetention":
		m.mu.Lock()
		r := m.retention[ch]
		m.mu.Unlock()
		return ok("retention", map[string]any{"retention_type": r.Type, "retention_duration": r.Duration}), true
	case "conversations.setRetention":
		typ, err := strconv.Atoi(f.Get("retention_type"))
		if err != nil || typ != 0 && typ != 1 {
			return fail("invalid_retention_type"), true
		}
		r := ChannelRetention{Type: typ}
		if typ == 1 {
			d, err := strconv.Atoi(f.Get("retention_duration"))
			if err != nil || d <= 0 || d >= 36500 {
				return fail("invalid_retention_duration"), true
			}
			r.Duration = d
		}
		m.mu.Lock()
		m.retention[ch] = r
		m.mu.Unlock()
		return ok(), true
	case "conversations.export":
		return ok(), true
	}
	return nil, false
}
