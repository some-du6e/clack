package fakeslack

import (
	"net/url"
	"strconv"
	"sync"

	"github.com/some-du6e/clack/internal/slack"
)

// The thread pane audit's fixtures (Size.ThreadsAudit; docs/THREADS-AUDIT.md),
// made-up people and words only: #th-audit holds one thread per case the
// pane draws differently, and conversations.replies serves each with the
// root's subscription (subscribed, last_read) the way Slack's parent
// message carries it.
//
//   - ThreadsAuditUnread: followed, read to its third reply (the viewer's
//     own); the "New" divider goes before the fourth.
//   - ThreadsAuditAllUnread: followed, read to the root; the "N replies"
//     separator carries the marker.
//   - ThreadsAuditOwnFirst: followed, read to the root, the first reply
//     the viewer's: the divider goes before the second.
//   - ThreadsAuditLong: 40 replies, not followed: the pane opens at the root.
//   - ThreadsAuditRead: followed and read to its last reply: no marker.
//   - ThreadsAuditHidden: reply_count 7 with three replies served: the
//     separator counts 7.
//   - ThreadsAuditFaces: seven repliers (reply_users_count 7): the reply
//     bar's fifth face carries "+2".
//   - ThreadsAuditLocked: is_locked (an admin turned its replies off): the
//     pane's footer is MessageInputLockedThreadRoadblock. The oldest root,
//     so the channel's other shots don't move. PushThreadLocked sends the
//     socket's message_locked for any root.
//   - ThreadsAuditOne: reply_count 2 with one reply served, the viewer's
//     (the short thread of Karim's 2026-11-12 gap report): the reply box
//     sits under it. Older than ThreadsAuditLocked for the same reason.
//
// subscriptions.thread.mark is recorded (ThreadsAuditMarks) and moves the
// thread's last_read, so a reload sees it read.
const (
	ThreadsAuditChannel = "C0THAUDIT01"

	ThreadsAuditUnread    = "1762700000.000100"
	ThreadsAuditAllUnread = "1762701000.000100"
	ThreadsAuditOwnFirst  = "1762702000.000100"
	ThreadsAuditLong      = "1762703000.000100"
	ThreadsAuditRead      = "1762704000.000100"
	ThreadsAuditHidden    = "1762705000.000100"
	ThreadsAuditFaces     = "1762706000.000100"
	ThreadsAuditLocked    = "1762699000.000100"
	ThreadsAuditOne       = "1762698000.000100"
)

// ThreadsAuditReply is the i-th reply of a fixture thread (a minute apart).
func ThreadsAuditReply(root string, i int) string {
	sec, _ := strconv.ParseInt(root[:len(root)-7], 10, 64)
	return strconv.FormatInt(sec+60*int64(i+1), 10) + ".000100"
}

type threadsAuditState struct {
	mu       sync.Mutex
	lastRead map[string]string // root -> last_read
	marks    []ThreadMark
	locked   map[string]bool // root -> is_locked set by PushThreadLocked
}

var threadsAuditStates sync.Map // *Server -> *threadsAuditState

func (s *Server) threadsAudit() *threadsAuditState {
	v, _ := threadsAuditStates.LoadOrStore(s, &threadsAuditState{lastRead: map[string]string{
		ThreadsAuditUnread:    ThreadsAuditReply(ThreadsAuditUnread, 2),
		ThreadsAuditAllUnread: ThreadsAuditAllUnread,
		ThreadsAuditOwnFirst:  ThreadsAuditOwnFirst,
		ThreadsAuditRead:      ThreadsAuditReply(ThreadsAuditRead, 2),
	}})
	return v.(*threadsAuditState)
}

// ThreadsAuditMarks returns the subscriptions.thread.mark calls for the
// fixtures, in order.
func (s *Server) ThreadsAuditMarks() []ThreadMark {
	st := s.threadsAudit()
	st.mu.Lock()
	defer st.mu.Unlock()
	return append([]ThreadMark(nil), st.marks...)
}

// threadsAuditUsers are the made-up repliers (generated directory users).
func (s *Server) threadsAuditUsers() []slack.User { return s.users[1:8] }

// threadsAuditCase is one fixture thread: its repliers by index (-1 the
// viewer), how many replies there are and how many Slack says there are.
type threadsAuditCase struct {
	root     string
	repliers []int
	count    int // reply_count (0: the replies served)
	text     string
	locked   bool // is_locked
}

func (s *Server) threadsAuditCases() []threadsAuditCase {
	long := make([]int, 40)
	for i := range long {
		long[i] = i % 3
	}
	return []threadsAuditCase{
		{root: ThreadsAuditUnread, repliers: []int{0, 1, -1, 0, 1, 0}, text: "Draft agenda for the offsite"},
		{root: ThreadsAuditAllUnread, repliers: []int{0, 1}, text: "Who has the projector key?"},
		{root: ThreadsAuditOwnFirst, repliers: []int{-1, 0}, text: "Moving the standup to 10"},
		{root: ThreadsAuditLong, repliers: long, text: "Long running thread about the garden"},
		{root: ThreadsAuditRead, repliers: []int{1, 0, 1}, text: "Lunch order for Friday"},
		{root: ThreadsAuditHidden, repliers: []int{0, 1, 0}, count: 7, text: "Photos from the trip"},
		{root: ThreadsAuditFaces, repliers: []int{0, 1, 2, 3, 4, 5, 6}, text: "Sign-up sheet for the potluck"},
		{root: ThreadsAuditLocked, repliers: []int{1, 2}, text: "Rules for the raffle", locked: true},
		{root: ThreadsAuditOne, repliers: []int{-1}, count: 2, text: "Has anyone seen the spare charger?"},
	}
}

func (s *Server) addThreadsAudit() {
	u := s.threadsAuditUsers()
	s.convs = append(s.convs, slack.Conversation{ID: ThreadsAuditChannel, Name: "th-audit", IsChannel: true, IsMember: true,
		NumMembers: 9, Created: 1762000000, Creator: u[0].ID, ContextTeamID: TeamID})
	var msgs []slack.Message
	for _, c := range s.threadsAuditCases() {
		msgs = append([]slack.Message{s.threadsAuditRoot(c)}, msgs...) // newest first
	}
	s.hist[ThreadsAuditChannel] = msgs
}

func (s *Server) threadsAuditUser(i int) string {
	if i < 0 {
		return SelfID
	}
	return s.threadsAuditUsers()[i].ID
}

func (s *Server) threadsAuditRoot(c threadsAuditCase) slack.Message {
	m := slack.Message{Type: "message", TS: c.root, ThreadTS: c.root, User: s.threadsAuditUser(0), Text: c.text, Channel: ThreadsAuditChannel}
	m.ReplyCount = len(c.repliers)
	if c.count > 0 {
		m.ReplyCount = c.count
	}
	seen := map[string]bool{}
	for _, r := range c.repliers {
		if id := s.threadsAuditUser(r); !seen[id] {
			seen[id] = true
			m.ReplyUsers = append(m.ReplyUsers, id)
		}
	}
	m.ReplyUsersN = len(m.ReplyUsers)
	m.LatestReply = ThreadsAuditReply(c.root, len(c.repliers)-1)
	st := s.threadsAudit()
	st.mu.Lock()
	m.IsLocked = c.locked
	if l, ok := st.locked[c.root]; ok {
		m.IsLocked = l
	}
	if lr, ok := st.lastRead[c.root]; ok {
		m.Subscribed, m.LastRead = true, lr
	}
	st.mu.Unlock()
	return m
}

func (s *Server) threadsAuditReplies(c threadsAuditCase) []slack.Message {
	out := []slack.Message{s.threadsAuditRoot(c)}
	for i, r := range c.repliers {
		out = append(out, slack.Message{Type: "message", TS: ThreadsAuditReply(c.root, i), ThreadTS: c.root,
			User: s.threadsAuditUser(r), Channel: ThreadsAuditChannel, Text: "Reply " + strconv.Itoa(i+1) + " to: " + c.text})
	}
	return out
}

// handleThreadsAudit answers conversations.replies and
// subscriptions.thread.mark for the fixtures (handled=false otherwise).
func (s *Server) handleThreadsAudit(method string, f url.Values) (any, bool) {
	if !s.Size.ThreadsAudit || f.Get("channel") != ThreadsAuditChannel {
		return nil, false
	}
	switch method {
	case "conversations.replies":
		for _, c := range s.threadsAuditCases() {
			if c.root == f.Get("ts") {
				return ok("messages", s.threadsAuditReplies(c), "has_more", false), true
			}
		}
	case "subscriptions.thread.mark":
		st := s.threadsAudit()
		st.mu.Lock()
		st.marks = append(st.marks, ThreadMark{Channel: f.Get("channel"), ThreadTS: f.Get("thread_ts"), TS: f.Get("ts")})
		if _, followed := st.lastRead[f.Get("thread_ts")]; followed {
			st.lastRead[f.Get("thread_ts")] = f.Get("ts")
		}
		st.mu.Unlock()
		return ok(), true
	}
	return nil, false
}

// PushThreadLocked sends the socket's message_locked for root in ch, as
// Slack's server does when an admin turns a thread's replies off or back
// on: the fields boot-render's messageLocked (`yYRM`) reads (channel,
// thread_ts, is_locked, locked_by_team_id). The server records it first,
// so conversations.history and conversations.replies serve the new
// is_locked to a client that reloads on the update.
func (s *Server) PushThreadLocked(ch, root string, locked bool) {
	st := s.threadsAudit()
	st.mu.Lock()
	if st.locked == nil {
		st.locked = map[string]bool{}
	}
	st.locked[root] = locked
	st.mu.Unlock()
	s.histMu.Lock()
	for i := range s.hist[ch] {
		if s.hist[ch][i].TS == root {
			s.hist[ch][i].IsLocked = locked
			s.hist[ch][i].LockedByTeamID = ""
			if locked {
				s.hist[ch][i].LockedByTeamID = TeamID
			}
		}
	}
	s.histMu.Unlock()
	s.Push(map[string]any{"type": "message", "subtype": "message_locked", "channel": ch, "thread_ts": root,
		"is_locked": locked, "locked_by_team_id": TeamID, "event_ts": root})
}
