package fakeslack

// Canvas comments (core/canvas_comments.go, docs/CANVASES.md "Comments"):
// the Launch plan canvas's (CanvasChannel) comment threads, served from
// its file channel (F → C) the way Slack serves them. Each thread's root
// is a document_comment_root message whose text is the highlighted part of
// the canvas; its comments are replies. Synthetic text only.
//
//   - conversations.history with document_stream: latest_reply (open
//     threads, latest reply first), unread (open threads whose last_seen
//     isn't the latest reply), archived (resolved threads); each root
//     comes with its latest reply.
//   - conversations.replies, chat.postMessage / chat.update / chat.delete
//     in a thread, reactions.add / reactions.remove.
//   - quip.thread.archive / quip.thread.unarchive {channel_id, thread_ts}
//     answer the changed root; quip.thread.archiveAll {channel_id}.

import (
	"encoding/json"
	"fmt"
	"net/url"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/some-du6e/clack/internal/slack"
)

// FileChanLaunch is CanvasChannel's file channel; CommentOpen,
// CommentUnread, CommentResolved and CommentReacted are its threads' roots.
const (
	FileChanLaunch  = "C0CANVAS001"
	CommentOpen     = "1762510000.000100" // two replies, read
	CommentUnread   = "1762520000.000100" // two replies, the last one unread
	CommentResolved = "1762480000.000100" // resolved, one reply
	CommentReacted  = "1762500000.000100" // one reply, the root has a reaction
)

type commentStore struct {
	mu      sync.Mutex
	msgs    map[string][]slack.Message // file channel → root and replies, oldest first
	calls   []url.Values               // quip.thread.* calls, as sent (with "method")
	history []url.Values               // conversations.history calls on file channels
	next    int
}

var commentStores sync.Map // *Server -> *commentStore

func (s *Server) comments() *commentStore {
	if v, ok := commentStores.Load(s); ok {
		return v.(*commentStore)
	}
	cs := &commentStore{msgs: map[string][]slack.Message{}}
	if !s.Size.NoFixtureDocs {
		cs.msgs[FileChanLaunch] = launchComments()
	}
	v, _ := commentStores.LoadOrStore(s, cs)
	return v.(*commentStore)
}

func commentRoot(ts, user, editor, thread, text string, replies []slack.Message, resolved bool, seen string) []slack.Message {
	root := slack.Message{Type: "message", Subtype: "document_comment_root", TS: ts, ThreadTS: ts, User: user, Text: text,
		DocumentComment: &slack.MessageDocumentComment{ThreadID: thread, Authors: []string{user}, IsArchived: resolved,
			IsVisible: true, MostRecentEditor: editor}}
	users := map[string]bool{}
	for _, r := range replies {
		root.ReplyCount++
		root.LatestReply = r.TS
		if !users[r.User] {
			users[r.User] = true
			root.ReplyUsers = append(root.ReplyUsers, r.User)
		}
	}
	root.ReplyUsersN = len(root.ReplyUsers)
	if seen != "" {
		root.DocumentComment.LastSeen = microsJSON(seen)
	}
	return append([]slack.Message{root}, replies...)
}

func commentReply(ts, thread, user, text string) slack.Message {
	return slack.Message{Type: "message", TS: ts, ThreadTS: thread, User: user, Text: text}
}

// microsJSON is a ts as document_comment.last_seen (µs).
func microsJSON(ts string) json.Number {
	sec, frac, _ := strings.Cut(ts, ".")
	n, _ := strconv.ParseInt(sec, 10, 64)
	f, _ := strconv.ParseInt((frac + "000000")[:6], 10, 64)
	return json.Number(strconv.FormatInt(n*1e6+f, 10))
}

func launchComments() []slack.Message {
	var out []slack.Message
	out = append(out, commentRoot(CommentResolved, "U0FAKE00003", "U0FAKE00003", "temp:C:FAKcmt0001", "Ship the beta to the first ten teams",
		[]slack.Message{commentReply("1762480100.000200", CommentResolved, "U0FAKE00003", "Done, all ten are in.")}, true, "1762480100.000200")...)
	out = append(out, commentRoot(CommentReacted, "U0FAKE00002", "U0FAKE00002", "temp:C:FAKcmt0002", "Write the changelog",
		[]slack.Message{commentReply("1762500100.000200", CommentReacted, "U0FAKE00002", "I can take this one.")}, false, "1762500100.000200")...)
	out[len(out)-2].Reactions = []slack.Reaction{{Name: "eyes", Users: []string{"U0FAKE00001"}, Count: 1}}
	out = append(out, commentRoot(CommentOpen, "U0FAKE00001", "U0FAKE00001", "temp:C:FAKcmt0003", "Launch on Tuesday",
		[]slack.Message{
			commentReply("1762510100.000200", CommentOpen, "U0FAKE00001", "Can we move this to Wednesday?"),
			commentReply("1762510200.000300", CommentOpen, SelfID, "Works for me."),
		}, false, "1762510200.000300")...)
	out = append(out, commentRoot(CommentUnread, "U0FAKE00004", "U0FAKE00001", "temp:C:FAKcmt0004", "Book the demo room",
		[]slack.Message{
			commentReply("1762520100.000200", CommentUnread, "U0FAKE00004", "Room 4 is free all week."),
			commentReply("1762520200.000300", CommentUnread, "U0FAKE00001", "Booked it for Thursday."),
		}, false, "1762520100.000200")...)
	return out
}

// CommentCalls are the quip.thread.* calls (tests); each has "method".
func (s *Server) CommentCalls() []url.Values {
	cs := s.comments()
	cs.mu.Lock()
	defer cs.mu.Unlock()
	return append([]url.Values(nil), cs.calls...)
}

// CommentHistoryCalls are the conversations.history calls on file
// channels (tests).
func (s *Server) CommentHistoryCalls() []url.Values {
	cs := s.comments()
	cs.mu.Lock()
	defer cs.mu.Unlock()
	return append([]url.Values(nil), cs.history...)
}

// CommentThread is a copy of a file channel thread: root then replies.
func (s *Server) CommentThread(ch, ts string) []slack.Message {
	cs := s.comments()
	cs.mu.Lock()
	defer cs.mu.Unlock()
	var out []slack.Message
	for _, m := range cs.msgs[ch] {
		if m.TS == ts || m.ThreadTS == ts {
			out = append(out, m)
		}
	}
	return out
}

// ReplyAsOther adds user's reply to the thread at ts in ch and pushes it
// over the socket, as Slack does for someone else's comment.
func (s *Server) ReplyAsOther(ch, ts, user, text string) string {
	cs := s.comments()
	cs.mu.Lock()
	cs.next++
	rts := fmt.Sprintf("%d.%06d", time.Now().Unix(), 500+cs.next)
	m := commentReply(rts, ts, user, text)
	cs.addReplyLocked(ch, m)
	cs.mu.Unlock()
	s.Push(map[string]any{"type": "message", "channel": ch, "user": user, "text": text, "ts": rts, "thread_ts": ts, "event_ts": rts})
	return rts
}

func (cs *commentStore) addReplyLocked(ch string, m slack.Message) {
	msgs := cs.msgs[ch]
	for i := range msgs {
		if msgs[i].TS != m.ThreadTS {
			continue
		}
		r := &msgs[i]
		r.ReplyCount++
		r.LatestReply = m.TS
		found := false
		for _, u := range r.ReplyUsers {
			found = found || u == m.User
		}
		if !found {
			r.ReplyUsers = append(r.ReplyUsers, m.User)
			r.ReplyUsersN = len(r.ReplyUsers)
		}
		if m.User == SelfID && r.DocumentComment != nil { // your own comment is seen
			r.DocumentComment.LastSeen = microsJSON(m.TS)
		}
	}
	cs.msgs[ch] = append(msgs, m)
}

// has reports whether ch is a fake canvas's file channel with comments.
func (cs *commentStore) has(ch string) bool { _, ok := cs.msgs[ch]; return ok }

func (cs *commentStore) find(ch, ts string) *slack.Message {
	for i := range cs.msgs[ch] {
		if cs.msgs[ch][i].TS == ts {
			return &cs.msgs[ch][i]
		}
	}
	return nil
}

// handleComments answers the file channels' methods; handled=false passes on.
func (s *Server) handleComments(method string, f url.Values) (any, bool) {
	ch := f.Get("channel")
	if strings.HasPrefix(method, "quip.thread.") {
		ch = f.Get("channel_id")
	}
	cs := s.comments()
	cs.mu.Lock()
	defer cs.mu.Unlock()
	if !cs.has(ch) {
		return nil, false
	}
	switch method {
	case "conversations.info":
		return ok("channel", fileChannelConv(ch, "Launch plan")), true
	case "conversations.history":
		cs.history = append(cs.history, f)
		return ok("messages", cs.stream(ch, f.Get("document_stream")), "has_more", false,
			"response_metadata", map[string]any{"next_cursor": ""}), true
	case "conversations.replies":
		var out []slack.Message
		for _, m := range cs.msgs[ch] {
			if m.TS == f.Get("ts") || m.ThreadTS == f.Get("ts") {
				out = append(out, m)
			}
		}
		if len(out) == 0 {
			return map[string]any{"ok": false, "error": "thread_not_found"}, true
		}
		return ok("messages", out, "has_more", false), true
	case "chat.postMessage":
		th := f.Get("thread_ts")
		if th == "" || cs.find(ch, th) == nil {
			return map[string]any{"ok": false, "error": "restricted_action_thread_only_channel"}, true
		}
		cs.next++
		ts := fmt.Sprintf("%d.%06d", time.Now().Unix(), 100+cs.next)
		m := commentReply(ts, th, SelfID, f.Get("text"))
		m.ClientMsgID = f.Get("client_msg_id")
		if b := f.Get("blocks"); b != "" {
			m.Blocks = []byte(b)
		}
		cs.addReplyLocked(ch, m)
		return ok("ts", ts, "channel", ch, "message", m), true
	case "chat.update":
		m := cs.find(ch, f.Get("ts"))
		if m == nil {
			return map[string]any{"ok": false, "error": "message_not_found"}, true
		}
		m.Text = f.Get("text")
		if b := f.Get("blocks"); b != "" {
			m.Blocks = []byte(b)
		}
		m.Edited = &slack.Edited{User: SelfID, TS: fmt.Sprintf("%d.000000", time.Now().Unix())}
		return ok("ts", m.TS, "channel", ch, "text", m.Text, "message", *m), true
	case "chat.delete":
		msgs := cs.msgs[ch]
		for i := range msgs {
			if msgs[i].TS != f.Get("ts") {
				continue
			}
			th := msgs[i].ThreadTS
			cs.msgs[ch] = append(msgs[:i:i], msgs[i+1:]...)
			if r := cs.find(ch, th); r != nil && th != f.Get("ts") {
				r.ReplyCount--
				r.LatestReply = ""
				for _, x := range cs.msgs[ch] {
					if x.ThreadTS == th && x.TS != th {
						r.LatestReply = x.TS
					}
				}
			}
			return ok("ts", f.Get("ts"), "channel", ch), true
		}
		return map[string]any{"ok": false, "error": "message_not_found"}, true
	case "reactions.add", "reactions.remove":
		m := cs.find(ch, f.Get("timestamp"))
		if m == nil {
			return map[string]any{"ok": false, "error": "message_not_found"}, true
		}
		name := f.Get("name")
		add := method == "reactions.add"
		for i := range m.Reactions {
			r := &m.Reactions[i]
			if r.Name != name {
				continue
			}
			users := r.Users[:0:0]
			for _, u := range r.Users {
				if u != SelfID {
					users = append(users, u)
				}
			}
			if add {
				users = append(users, SelfID)
			}
			r.Users, r.Count = users, len(users)
			if r.Count == 0 {
				m.Reactions = append(m.Reactions[:i:i], m.Reactions[i+1:]...)
			}
			return ok(), true
		}
		if add {
			m.Reactions = append(m.Reactions, slack.Reaction{Name: name, Users: []string{SelfID}, Count: 1})
		}
		return ok(), true
	case "quip.thread.archive", "quip.thread.unarchive":
		call := url.Values{"method": {method}}
		for k, v := range f {
			call[k] = v
		}
		cs.calls = append(cs.calls, call)
		m := cs.find(ch, f.Get("thread_ts"))
		if m == nil || m.DocumentComment == nil {
			return map[string]any{"ok": false, "error": "thread_not_found"}, true
		}
		m.DocumentComment.IsArchived = method == "quip.thread.archive"
		return ok("message", *m), true
	case "quip.thread.archiveAll":
		call := url.Values{"method": {method}}
		for k, v := range f {
			call[k] = v
		}
		cs.calls = append(cs.calls, call)
		for i := range cs.msgs[ch] {
			if dc := cs.msgs[ch][i].DocumentComment; dc != nil {
				dc.IsArchived = true
			}
		}
		return ok(), true
	}
	return nil, false
}

// stream is conversations.history's document_stream answer: the roots
// the filter keeps, latest reply first, each followed by its latest reply.
func (cs *commentStore) stream(ch, filter string) []slack.Message {
	var roots []slack.Message
	for _, m := range cs.msgs[ch] {
		dc := m.DocumentComment
		if m.Subtype != "document_comment_root" || dc == nil {
			continue
		}
		switch filter {
		case "archived":
			if !dc.IsArchived {
				continue
			}
		case "unread":
			if dc.IsArchived || m.LatestReply == "" || dc.LastSeen == microsJSON(m.LatestReply) {
				continue
			}
		default:
			if dc.IsArchived {
				continue
			}
		}
		roots = append(roots, m)
	}
	key := func(m slack.Message) string {
		if m.LatestReply != "" {
			return m.LatestReply
		}
		return m.TS
	}
	sort.SliceStable(roots, func(i, j int) bool { return slackTSLess(key(roots[j]), key(roots[i])) })
	var out []slack.Message
	for _, r := range roots {
		out = append(out, r)
		for _, m := range cs.msgs[ch] {
			if m.ThreadTS == r.TS && m.TS == r.LatestReply && m.TS != r.TS {
				out = append(out, m)
			}
		}
	}
	return out
}
