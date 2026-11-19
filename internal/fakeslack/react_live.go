package fakeslack

import (
	"fmt"
	"slices"
	"time"

	"github.com/some-du6e/clack/internal/slack"
)

// ReactLive is user's reaction_added (add) or reaction_removed of name on
// the message ts of ch (its history's or a quiet thread's reply), as
// another client's reaction reaches the server: the message's reactions
// change in the fake's history (a refresh reads them so) and the event
// goes out over the socket. It reports whether it found the message.
func (s *Server) ReactLive(ch, ts, name, user string, add bool) bool {
	s.histMu.Lock()
	var author string
	found := false
	apply := func(m *slack.Message) {
		author, found = m.User, true
		for i := range m.Reactions {
			r := &m.Reactions[i]
			if r.Name != name {
				continue
			}
			has := slices.Contains(r.Users, user)
			switch {
			case add && !has:
				r.Users = append(r.Users, user)
				r.Count++
			case !add && has:
				r.Users = slices.DeleteFunc(r.Users, func(u string) bool { return u == user })
				r.Count--
				if r.Count < 1 || len(r.Users) == 0 {
					m.Reactions = slices.Delete(m.Reactions, i, i+1)
				}
			}
			return
		}
		if add {
			m.Reactions = append(m.Reactions, slack.Reaction{Name: name, Count: 1, Users: []string{user}})
		}
	}
	h := s.histLocked(ch)
	for i := range h {
		if h[i].TS == ts {
			apply(&h[i])
		}
	}
	if !found {
		for k, rs := range s.quietReplies {
			if len(k) <= len(ch) || k[:len(ch)+1] != ch+"/" {
				continue
			}
			for i := range rs {
				if rs[i].TS == ts {
					apply(&rs[i])
				}
			}
		}
	}
	s.histMu.Unlock()
	if !found {
		return false
	}
	typ := "reaction_added"
	if !add {
		typ = "reaction_removed"
	}
	ets := fmt.Sprintf("%d.%06d", time.Now().Unix(), time.Now().Nanosecond()/1000)
	s.Push(map[string]any{"type": typ, "user": user, "reaction": name, "item_user": author, "event_ts": ets,
		"item": map[string]any{"type": "message", "channel": ch, "ts": ts}})
	return true
}

// PostFileQuiet adds a message from user sharing one file (a PDF) to ch's
// history (newest) and returns its ts, unpushed.
func (s *Server) PostFileQuiet(ch, user, text string) string {
	ts, _ := s.PostQuiet(ch, user, text)
	s.histMu.Lock()
	defer s.histMu.Unlock()
	if h := s.hist[ch]; len(h) > 0 && h[0].TS == ts {
		h[0].Upload = true
		h[0].Files = []slack.File{{ID: "F0REACT" + ts[len(ts)-6:], Name: "outage-report.pdf", Title: "outage-report.pdf",
			Mimetype: "application/pdf", Filetype: "pdf", PrettyType: "PDF", Size: 48213, User: user,
			URLPrivate: "https://files.slack.com/files-pri/T0FAKE/outage-report.pdf", Mode: "hosted"}}
	}
	return ts
}
