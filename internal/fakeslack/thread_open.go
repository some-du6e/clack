package fakeslack

import (
	"fmt"
	"net/url"
	"strconv"
	"sync"

	"github.com/some-du6e/clack/internal/slack"
)

// Where a thread opens (Size.ThreadOpen), made-up people and words only:
// #open-top is a long channel of one-line messages ten minutes apart,
// five of them thread roots with ThreadOpenReplies one-line replies a
// minute apart. conversations.replies serves each a page at a time as
// live Slack does (replies_paging.go), with the root's subscription
// (subscribed, last_read) the way Slack's parent message carries it, in
// conversations.history too:
//
//   - ThreadOpenRead: followed and read to its newest reply;
//   - ThreadOpenUnread: followed, read to reply 50, so its first unread
//     reply is past the first page of 28 that a read from the root gets;
//   - ThreadOpenEarly: followed, read to reply 10;
//   - ThreadOpenFresh: not followed;
//   - ThreadOpenNewest: the channel's newest message, followed and read.
//
// The channel is read to its newest message. subscriptions.thread.mark
// is recorded (ThreadOpenMarks), moves the thread's last_read and pushes
// thread_marked, as Slack's server does; subscriptions.thread.getView
// lists the followed threads, the newest reply first.
const (
	ThreadOpenChannel = "C0THOPEN001"
	ThreadOpenMsgs    = 160 // the channel's messages
	ThreadOpenReplies = 80  // each thread's replies

	threadOpenBase = 1762800000 // the oldest message's second

	ThreadOpenRead   = "1762824000.000100" // message 40
	ThreadOpenUnread = "1762842000.000100" // message 70
	ThreadOpenFresh  = "1762860000.000100" // message 100
	ThreadOpenEarly  = "1762872000.000100" // message 120
	ThreadOpenNewest = "1762895400.000100" // message 159, the newest
)

// ThreadOpenMsg is the channel's message i (0 the oldest).
func ThreadOpenMsg(i int) string {
	return strconv.FormatInt(threadOpenBase+600*int64(i), 10) + ".000100"
}

// ThreadOpenReply is reply i of a fixture thread (0 the oldest): a minute
// apart after the root, its microseconds the root's message number, so no
// two threads' replies share a ts.
func ThreadOpenReply(root string, i int) string {
	sec, _ := strconv.ParseInt(root[:len(root)-7], 10, 64)
	return fmt.Sprintf("%d.%06d", sec+60*int64(i+1), 200+(sec-threadOpenBase)/600)
}

// ThreadOpenRoots are the fixture threads, oldest first.
var ThreadOpenRoots = []string{ThreadOpenRead, ThreadOpenUnread, ThreadOpenFresh, ThreadOpenEarly, ThreadOpenNewest}

type threadOpenState struct {
	mu       sync.Mutex
	lastRead map[string]string // root -> last_read, followed threads only
	marks    []ThreadMark
	reqs     []url.Values // conversations.replies requests, in order
}

var threadOpenStates = newPerServer() // *Server -> *threadOpenState

func (s *Server) threadOpen() *threadOpenState {
	v, _ := threadOpenStates.LoadOrStore(s, &threadOpenState{lastRead: map[string]string{
		ThreadOpenRead:   ThreadOpenReply(ThreadOpenRead, ThreadOpenReplies-1),
		ThreadOpenUnread: ThreadOpenReply(ThreadOpenUnread, 49),
		ThreadOpenEarly:  ThreadOpenReply(ThreadOpenEarly, 9),
		ThreadOpenNewest: ThreadOpenReply(ThreadOpenNewest, ThreadOpenReplies-1),
	}})
	return v.(*threadOpenState)
}

// ThreadOpenMarks returns the subscriptions.thread.mark calls for the
// fixtures, in order.
func (s *Server) ThreadOpenMarks() []ThreadMark {
	st := s.threadOpen()
	st.mu.Lock()
	defer st.mu.Unlock()
	return append([]ThreadMark(nil), st.marks...)
}

// ThreadOpenRequests returns the conversations.replies requests for the
// fixtures, in order.
func (s *Server) ThreadOpenRequests() []url.Values {
	st := s.threadOpen()
	st.mu.Lock()
	defer st.mu.Unlock()
	out := make([]url.Values, len(st.reqs))
	for i, r := range st.reqs {
		c := url.Values{}
		for k, v := range r {
			c[k] = append([]string(nil), v...)
		}
		out[i] = c
	}
	return out
}

// threadOpenUser is the made-up author of message or reply i.
func (s *Server) threadOpenUser(i int) string { return s.users[1+i%4].ID }

func threadOpenIsRoot(ts string) bool {
	for _, r := range ThreadOpenRoots {
		if r == ts {
			return true
		}
	}
	return false
}

func (s *Server) addThreadOpen() {
	s.convs = append(s.convs, slack.Conversation{ID: ThreadOpenChannel, Name: "open-top", IsChannel: true, IsMember: true,
		NumMembers: 5, Created: threadOpenBase - 86400, Creator: s.threadOpenUser(0), ContextTeamID: TeamID,
		LastRead: ThreadOpenMsg(ThreadOpenMsgs - 1)}) // read to its newest message (client.counts too)
	msgs := make([]slack.Message, 0, ThreadOpenMsgs)
	for i := ThreadOpenMsgs - 1; i >= 0; i-- { // newest first
		ts := ThreadOpenMsg(i)
		if threadOpenIsRoot(ts) {
			msgs = append(msgs, s.threadOpenRoot(ts))
			continue
		}
		msgs = append(msgs, slack.Message{Type: "message", TS: ts, User: s.threadOpenUser(i), Channel: ThreadOpenChannel,
			Text: "Status note " + strconv.Itoa(i+1)})
	}
	s.hist[ThreadOpenChannel] = msgs
}

func (s *Server) threadOpenRoot(root string) slack.Message {
	sec, _ := strconv.ParseInt(root[:len(root)-7], 10, 64)
	i := int((sec - threadOpenBase) / 600)
	m := slack.Message{Type: "message", TS: root, ThreadTS: root, User: s.threadOpenUser(i), Channel: ThreadOpenChannel,
		Text: "Thread starter " + strconv.Itoa(i+1), ReplyCount: ThreadOpenReplies,
		LatestReply: ThreadOpenReply(root, ThreadOpenReplies-1)}
	for k := 0; k < 4; k++ {
		m.ReplyUsers = append(m.ReplyUsers, s.threadOpenUser(k))
	}
	m.ReplyUsersN = len(m.ReplyUsers)
	st := s.threadOpen()
	st.mu.Lock()
	if lr, ok := st.lastRead[root]; ok {
		m.Subscribed, m.LastRead = true, lr
	}
	st.mu.Unlock()
	return m
}

func (s *Server) threadOpenReplies(root string) []slack.Message {
	out := []slack.Message{s.threadOpenRoot(root)}
	for i := 0; i < ThreadOpenReplies; i++ {
		out = append(out, slack.Message{Type: "message", TS: ThreadOpenReply(root, i), ThreadTS: root,
			User: s.threadOpenUser(i + 1), Channel: ThreadOpenChannel, Text: "Reply " + strconv.Itoa(i+1)})
	}
	return out
}

// threadOpenRootOf is the fixture thread ts is the root or a reply of
// ("" for none): conversations.replies answers a reply's ts with its
// parent's thread.
func threadOpenRootOf(ts string) string {
	for _, r := range ThreadOpenRoots {
		if r == ts {
			return r
		}
		for i := 0; i < ThreadOpenReplies; i++ {
			if ThreadOpenReply(r, i) == ts {
				return r
			}
		}
	}
	return ""
}

// handleThreadOpen answers the fixtures' conversations.replies,
// subscriptions.thread.mark and subscriptions.thread.getView
// (handled=false otherwise, or without Size.ThreadOpen).
func (s *Server) handleThreadOpen(method string, f url.Values) (any, bool) {
	if !s.Size.ThreadOpen {
		return nil, false
	}
	switch method {
	case "conversations.replies":
		if f.Get("channel") != ThreadOpenChannel {
			return nil, false
		}
		root := threadOpenRootOf(f.Get("ts"))
		if root == "" {
			return nil, false
		}
		st := s.threadOpen()
		st.mu.Lock()
		c := url.Values{}
		for k, v := range f {
			c[k] = append([]string(nil), v...)
		}
		st.reqs = append(st.reqs, c)
		st.mu.Unlock()
		q := url.Values{}
		for k, v := range f {
			q[k] = v
		}
		q.Set("ts", root)
		page, more, next := pageReplies(s.threadOpenReplies(root), q)
		return ok("messages", page, "has_more", more, "response_metadata", map[string]any{"next_cursor": next}), true
	case "subscriptions.thread.mark":
		if f.Get("channel") != ThreadOpenChannel {
			return nil, false
		}
		root, ts := f.Get("thread_ts"), f.Get("ts")
		st := s.threadOpen()
		st.mu.Lock()
		st.marks = append(st.marks, ThreadMark{Channel: ThreadOpenChannel, ThreadTS: root, TS: ts})
		_, followed := st.lastRead[root]
		if followed {
			st.lastRead[root] = ts
		}
		st.mu.Unlock()
		if followed {
			s.Push(map[string]any{"type": "thread_marked", "subscription": map[string]any{"type": "thread", "channel": ThreadOpenChannel,
				"thread_ts": root, "last_read": ts, "active": true}, "event_ts": ts})
		}
		return ok(), true
	case "subscriptions.thread.getView":
		var threads []any
		unread := 0
		for i := len(ThreadOpenRoots) - 1; i >= 0; i-- { // the newest reply first
			root := ThreadOpenRoots[i]
			rm := s.threadOpenRoot(root)
			if !rm.Subscribed {
				continue
			}
			reps := s.threadOpenReplies(root)[1:]
			var un []slack.Message
			for _, r := range reps {
				if slackTSLess(rm.LastRead, r.TS) {
					un = append(un, r)
				}
			}
			unread += len(un)
			if un == nil {
				un = []slack.Message{}
			}
			threads = append(threads, map[string]any{"root_msg": rm, "latest_replies": reps[len(reps)-2:], "unread_replies": un})
		}
		return ok("threads", threads, "has_more", false, "total_unread_replies", unread,
			"threads_state", map[string]any{"has_unreads": unread > 0, "mention_count": 0}), true
	}
	return nil, false
}
