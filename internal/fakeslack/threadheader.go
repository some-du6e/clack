package fakeslack

import (
	"net/url"
	"strconv"
	"strings"

	"github.com/some-du6e/clack/internal/slack"
)

// Thread header fixtures (Size.ThreadHeader): one conversation of each
// kind the thread pane's header names differently (Slack's threads
// Header), each holding one thread. The archived channel is one the
// viewer is in (users.conversations lists it, the client drops archived
// channels from the sidebar, so the header has to look it up), and
// ThreadElsewhereID a channel outside the viewer's conversations, as a
// thread opened from Activity can be.
const (
	ThreadPublicID    = "C0THPUBLIC1"
	ThreadPrivateID   = "C0THPRIVAT1"
	ThreadSharedID    = "C0THSHARED1"
	ThreadArchivedID  = "C0THARCHIV1"
	ThreadDMID        = "D0THDIRECT1"
	ThreadGroupDMID   = "C0THGROUPD1"
	ThreadElsewhereID = "C0THELSEWH1"

	// ThreadRootTS roots each fixture's thread; ThreadReplyTS(i) are its
	// replies.
	ThreadRootTS = "1762516000.000100"
)

// ThreadReplyTS is the i-th reply (0, 1) of a thread fixture.
func ThreadReplyTS(i int) string { return []string{"1762516060.000100", "1762516120.000100"}[i] }

// ThreadHeaderCase is one fixture: its conversation and what Slack's
// header shows after "Thread" (Icon is the SvgIcon name, "" for DMs; Tip
// the tooltip, group DMs only).
type ThreadHeaderCase struct {
	Name           string
	Channel        string
	Text, Icon     string
	Tip            string
	InConversation bool // in users.conversations (the archived one too)
}

// ThreadHeaderCases lists the fixtures in a fixed order.
func (s *Server) ThreadHeaderCases() []ThreadHeaderCase {
	u := s.threadHeaderUsers()
	return []ThreadHeaderCase{
		{Name: "public", Channel: ThreadPublicID, Text: "th-launch-notes", Icon: "channel", InConversation: true},
		{Name: "private", Channel: ThreadPrivateID, Text: "th-secret-plans", Icon: "lock-filled", InConversation: true},
		{Name: "shared", Channel: ThreadSharedID, Text: "th-partner-co", Icon: "channel", InConversation: true},
		{Name: "archived", Channel: ThreadArchivedID, Text: "th-old-launch (archived)", Icon: "archive", InConversation: true},
		{Name: "dm", Channel: ThreadDMID, Text: u[0].Name, InConversation: true},
		{Name: "group-dm", Channel: ThreadGroupDMID, Text: "Direct message with 3 others",
			Tip: sortedJoin(u[0].Name, u[1].Name, u[2].Name), InConversation: true},
		{Name: "elsewhere", Channel: ThreadElsewhereID, Text: "th-elsewhere", Icon: "channel"},
	}
}

// threadHeaderUsers are the generated users the DMs are with (display
// names are their handles).
func (s *Server) threadHeaderUsers() []slack.User { return s.users[1:4] }

func sortedJoin(names ...string) string {
	for i := 1; i < len(names); i++ {
		for j := i; j > 0 && strings.ToLower(names[j]) < strings.ToLower(names[j-1]); j-- {
			names[j], names[j-1] = names[j-1], names[j]
		}
	}
	return strings.Join(names, ", ")
}

func (s *Server) addThreadHeader() {
	u := s.threadHeaderUsers()
	const created = 1762000000
	ch := func(id, name string) slack.Conversation {
		return slack.Conversation{ID: id, Name: name, IsChannel: true, IsMember: true, NumMembers: 12,
			Created: created, Creator: u[0].ID, ContextTeamID: TeamID}
	}
	pub := ch(ThreadPublicID, "th-launch-notes")
	priv := ch(ThreadPrivateID, "th-secret-plans")
	priv.IsChannel, priv.IsGroup, priv.IsPrivate = false, true, true
	shared := ch(ThreadSharedID, "th-partner-co")
	shared.IsExtShared = true
	arch := ch(ThreadArchivedID, "th-old-launch")
	arch.IsArchived = true
	dm := slack.Conversation{ID: ThreadDMID, IsIM: true, User: u[0].ID, IsOpen: true, Created: created}
	handles := []string{s.users[0].Name, u[0].Name, u[1].Name, u[2].Name}
	group := slack.Conversation{ID: ThreadGroupDMID, Name: "mpdm-" + strings.Join(handles, "--") + "-1", IsMPIM: true,
		IsGroup: true, IsPrivate: true, IsOpen: true, Created: created,
		Members: []string{SelfID, u[0].ID, u[1].ID, u[2].ID}}
	s.convs = append(s.convs, pub, priv, shared, arch, dm, group)
	other := ch(ThreadElsewhereID, "th-elsewhere")
	other.IsMember = false
	s.others = append(s.others, other)
	for _, c := range s.ThreadHeaderCases() {
		s.hist[c.Channel] = []slack.Message{s.threadHeaderMsg(c.Channel, ThreadRootTS)}
	}
}

// threadHeaderMsg is a fixture message: the root (with its reply
// metadata) or a reply.
func (s *Server) threadHeaderMsg(ch, ts string) slack.Message {
	u := s.threadHeaderUsers()
	m := slack.Message{Type: "message", TS: ts, User: u[0].ID, ThreadTS: ThreadRootTS, Channel: ch}
	if ts == ThreadRootTS {
		m.Text = "Launch checklist for Friday"
		m.ReplyCount, m.LatestReply, m.ReplyUsers = 2, ThreadReplyTS(1), []string{u[1].ID}
	} else {
		m.User = u[1].ID
		m.Text = map[string]string{ThreadReplyTS(0): "Docs are ready", ThreadReplyTS(1): "Ship it"}[ts]
	}
	// a user's message has its text as rich_text too (find in thread
	// marks matches in blocks only)
	m.Blocks = rawJSON(richText(map[string]any{"type": "text", "text": m.Text}))
	return m
}

func (s *Server) isThreadHeaderConv(id string) bool {
	for _, c := range s.ThreadHeaderCases() {
		if c.Channel == id {
			return true
		}
	}
	return false
}

// handleThreadHeader answers conversations.replies for the fixtures.
func (s *Server) handleThreadHeader(method string, f url.Values) (any, bool) {
	if !s.Size.ThreadHeader || method != "conversations.replies" || !s.isThreadHeaderConv(f.Get("channel")) ||
		f.Get("ts") != ThreadRootTS {
		return nil, false
	}
	ch := f.Get("channel")
	return ok("messages", []slack.Message{s.threadHeaderMsg(ch, ThreadRootTS), s.threadHeaderMsg(ch, ThreadReplyTS(0)),
		s.threadHeaderMsg(ch, ThreadReplyTS(1))}, "has_more", false), true
}

// findInThread answers search.inline with find_in_thread (Slack's find
// in thread) for a fixture's thread: its messages whose text contains the
// query, ignoring case, oldest first, count (or FindInThreadPage) a page
// with next_cursor after each page but the last.
func (s *Server) findInThread(f url.Values) any {
	q := strings.ToLower(strings.TrimSpace(f.Get("query")))
	ch, ts := f.Get("channel"), f.Get("thread_ts")
	var hits []slack.Message
	if s.Size.ThreadHeader && s.isThreadHeaderConv(ch) && ts == ThreadRootTS && q != "" {
		for _, t := range []string{ThreadRootTS, ThreadReplyTS(0), ThreadReplyTS(1)} {
			if m := s.threadHeaderMsg(ch, t); strings.Contains(strings.ToLower(m.Text), q) {
				hits = append(hits, m)
			}
		}
	}
	per, _ := strconv.Atoi(f.Get("count"))
	if per <= 0 {
		per = 20
	}
	if s.FindInThreadPage > 0 && s.FindInThreadPage < per {
		per = s.FindInThreadPage
	}
	start := 0
	if c, found := strings.CutPrefix(f.Get("cursor"), "fit:"); found {
		start, _ = strconv.Atoi(c)
	}
	start = min(max(start, 0), len(hits))
	end := min(start+per, len(hits))
	items := []any{}
	for _, m := range hits[start:end] {
		items = append(items, map[string]any{"ts": m.TS, "user": m.User, "channel_id": ch, "text": m.Text})
	}
	next := ""
	if end < len(hits) {
		next = "fit:" + strconv.Itoa(end)
	}
	return ok("query", f.Get("query"), "items", items, "next_cursor", next)
}
