package fakeslack

import (
	"encoding/json"
	"net/url"
	"strings"

	"github.com/some-du6e/clack/internal/slack"
)

// messages.list and conversations.replies for any message the fake
// holds, behind the fixtures' own answers (Size.MessagesByID's answer,
// messagesByID, goes before them):
//
//   - messages.list (message_ids: [{channel, timestamps}]) finds a
//     conversation's history, the replies PostReplyQuiet added and the
//     thread header fixtures' messages; a ts the fake doesn't hold is
//     left out, as Slack leaves out a deleted message. Slack's
//     jumpToMessage asks it for a message's thread_ts
//     (core.ReplyThread).
//   - conversations.replies with a reply's ts answers the whole thread,
//     parent first: Slack's ts is "a thread's parent message or a
//     message in the thread".

// messagesList answers messages.list.
func (s *Server) messagesList(f url.Values) any {
	var want []struct {
		Channel    string   `json:"channel"`
		Timestamps []string `json:"timestamps"`
	}
	if json.Unmarshal([]byte(f.Get("message_ids")), &want) != nil {
		return map[string]any{"ok": false, "error": "invalid_arguments"}
	}
	out := map[string][]slack.Message{}
	for _, w := range want {
		for _, ts := range w.Timestamps {
			if m, found := s.messageAt(w.Channel, ts); found {
				out[w.Channel] = append(out[w.Channel], m)
			}
		}
	}
	return ok("messages", out)
}

// messageAt is message ts of ch, wherever the fake holds it.
func (s *Server) messageAt(ch, ts string) (slack.Message, bool) {
	if s.Size.ThreadHeader && s.isThreadHeaderConv(ch) && isThreadHeaderTS(ts) {
		return s.threadHeaderMsg(ch, ts), true
	}
	s.histMu.Lock()
	defer s.histMu.Unlock()
	for _, m := range s.histLocked(ch) {
		if m.TS == ts {
			return m, true
		}
	}
	if m, found := s.quietReplyLocked(ch, ts); found {
		return m, true
	}
	return slack.Message{}, false
}

// quietReplyLocked is the reply ts of ch PostReplyQuiet added (histMu
// held).
func (s *Server) quietReplyLocked(ch, ts string) (slack.Message, bool) {
	for k, rs := range s.quietReplies {
		if !strings.HasPrefix(k, ch+"/") {
			continue
		}
		for _, m := range rs {
			if m.TS == ts {
				return m, true
			}
		}
	}
	return slack.Message{}, false
}

// isThreadHeaderTS is a thread header fixture's root or reply.
func isThreadHeaderTS(ts string) bool {
	return ts == ThreadRootTS || ts == ThreadReplyTS(0) || ts == ThreadReplyTS(1)
}

// quietRootOf is the root of the thread reply ts of ch is in, for a reply
// PostReplyQuiet added ("" otherwise).
func (s *Server) quietRootOf(ch, ts string) string {
	s.histMu.Lock()
	defer s.histMu.Unlock()
	if m, found := s.quietReplyLocked(ch, ts); found {
		return m.ThreadTS
	}
	return ""
}

// messagesByID is messages.list for Size.MessagesByID: each message asked
// for that the fake has (a channel's history, a quiet thread's replies),
// by ts; one it doesn't have is left out of its channel's list, which
// Slack's answer means it doesn't exist.
func (s *Server) messagesByID(f url.Values) (any, bool) {
	var want []struct {
		Channel    string   `json:"channel"`
		Timestamps []string `json:"timestamps"`
	}
	if json.Unmarshal([]byte(f.Get("message_ids")), &want) != nil {
		return nil, false
	}
	out := map[string][]slack.Message{}
	for _, w := range want {
		for _, ts := range w.Timestamps {
			if m, ok := s.messageByTS(w.Channel, ts); ok {
				out[w.Channel] = append(out[w.Channel], m)
			}
		}
	}
	return ok("messages", out), true
}
