package fakeslack

import (
	"sort"

	"github.com/some-du6e/clack/internal/slack"
)

// PostBroadcastQuiet is PostReplyQuiet for a reply also sent to the
// channel ("Also send to #channel"): the reply is subtype
// thread_broadcast in the thread and in ch's history too, carrying its
// root as the API does (the parent with its reply metadata). It returns
// the broadcast's ts, unpushed.
func (s *Server) PostBroadcastQuiet(ch, root, user, text string) string {
	ts := s.PostReplyQuiet(ch, root, user, text)
	s.histMu.Lock()
	defer s.histMu.Unlock()
	k := ch + "/" + root
	rs := s.quietReplies[k]
	var parent *slack.Message
	h := s.histLocked(ch)
	for i := range h {
		if h[i].TS == root {
			p := h[i]
			parent = &p
		}
	}
	for i := range rs {
		if rs[i].TS == ts {
			rs[i].Subtype, rs[i].Root = "thread_broadcast", parent
			b := rs[i]
			h = append(h, b)
			sort.SliceStable(h, func(i, j int) bool { return h[i].TS > h[j].TS }) // newest first
			s.hist[ch] = h
		}
	}
	return ts
}

// PushMessageReplied pushes the message_replied event a thread's new
// reply brings: the parent (root, a message of ch's history) with its
// reply metadata as it is now.
func (s *Server) PushMessageReplied(ch, root string) bool {
	s.histMu.Lock()
	var parent *slack.Message
	for _, m := range s.hist[ch] {
		if m.TS == root {
			p := m
			parent = &p
		}
	}
	s.histMu.Unlock()
	if parent == nil {
		return false
	}
	s.Push(map[string]any{"type": "message", "subtype": "message_replied", "channel": ch, "hidden": true,
		"message": parent, "event_ts": parent.LatestReply, "ts": parent.LatestReply})
	return true
}
