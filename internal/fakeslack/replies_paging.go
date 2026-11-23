package fakeslack

import (
	"net/url"
	"sort"
	"strconv"
	"strings"

	"github.com/some-du6e/clack/internal/slack"
)

// pageReplies answers conversations.replies over a thread held whole
// (msgs: the root, f's ts, and its replies) as live Slack pages it, the
// way huge_thread.go's replies do: the root first in every answer, then
// up to limit replies (1000 at most) in order, forward from oldest
// (inclusive unless inclusive=false) when it is set or no bound is, else
// the newest up to latest; more says whether replies are left past the
// page in the direction it read, and a page read forward names its next
// cursor. Synthetic threads only.
func pageReplies(msgs []slack.Message, f url.Values) (page []slack.Message, more bool, next string) {
	root := f.Get("ts")
	var reps []slack.Message
	for _, m := range msgs {
		if m.TS == root {
			page = append(page, m)
		} else {
			reps = append(reps, m)
		}
	}
	at := func(i int) int64 { us, _ := micros(reps[i].TS); return us }
	sort.SliceStable(reps, func(i, j int) bool { return at(i) < at(j) })
	limit, _ := strconv.Atoi(f.Get("limit"))
	if limit <= 0 || limit > 1000 {
		limit = 1000
	}
	strict := f.Get("inclusive") == "false" || f.Get("inclusive") == "0"
	lo, hi := 0, len(reps)-1
	oldest, latest := f.Get("oldest"), f.Get("latest")
	if us, ok := micros(oldest); ok && oldest != "" {
		for lo <= hi && (at(lo) < us || strict && at(lo) == us) {
			lo++
		}
	}
	if us, ok := micros(latest); ok && latest != "" {
		for hi >= lo && (at(hi) > us || strict && at(hi) == us) {
			hi--
		}
	}
	from, to := lo, hi
	switch cur := f.Get("cursor"); {
	case strings.HasPrefix(cur, "i:"):
		n, _ := strconv.Atoi(cur[2:])
		from = max(n, lo)
		fallthrough
	case oldest != "" || latest == "":
		// forward from oldest (or the start)
		if from+limit-1 < hi {
			to, more = from+limit-1, true
			next = "i:" + strconv.Itoa(to+1)
		}
	default:
		// the newest up to latest
		if hi-limit+1 > lo {
			from, more = hi-limit+1, true
		}
	}
	for i := max(from, 0); i <= to && i < len(reps); i++ {
		page = append(page, reps[i])
	}
	return page, more, next
}

// quietReplyAt finds reply ts of thread ch/root (PostReplyQuiet's);
// callers hold s.histMu.
func (s *Server) quietReplyAt(ch, root, ts string) (int, bool) {
	for i, m := range s.quietReplies[ch+"/"+root] {
		if m.TS == ts {
			return i, true
		}
	}
	return -1, false
}

// EditReplyQuiet makes reply ts of thread ch/root (PostReplyQuiet's) say
// text, edited, in the fake's history, unpushed (no realtime event), and
// reports whether it found the reply.
func (s *Server) EditReplyQuiet(ch, root, ts, text string) bool {
	s.histMu.Lock()
	defer s.histMu.Unlock()
	i, ok := s.quietReplyAt(ch, root, ts)
	if !ok {
		return false
	}
	r := &s.quietReplies[ch+"/"+root][i]
	r.Text = text
	r.Blocks = nil
	r.Edited = &slack.Edited{User: r.User, TS: uptimeEditTS(ts)}
	return true
}

// ReactReplyQuiet adds the reaction name by users to reply ts of thread
// ch/root, unpushed, and reports whether it found the reply.
func (s *Server) ReactReplyQuiet(ch, root, ts, name string, users ...string) bool {
	s.histMu.Lock()
	defer s.histMu.Unlock()
	i, ok := s.quietReplyAt(ch, root, ts)
	if !ok {
		return false
	}
	r := &s.quietReplies[ch+"/"+root][i]
	r.Reactions = append(r.Reactions, slack.Reaction{Name: name, Count: len(users), Users: append([]string(nil), users...)})
	return true
}

// DeleteReplyQuiet deletes reply ts of thread ch/root, unpushed: the
// root's reply_count and latest_reply follow. It reports whether it
// found the reply.
func (s *Server) DeleteReplyQuiet(ch, root, ts string) bool {
	s.histMu.Lock()
	defer s.histMu.Unlock()
	i, ok := s.quietReplyAt(ch, root, ts)
	if !ok {
		return false
	}
	k := ch + "/" + root
	rs := append(s.quietReplies[k][:i:i], s.quietReplies[k][i+1:]...)
	s.quietReplies[k] = rs
	h := s.histLocked(ch)
	for j := range h {
		if h[j].TS == root {
			h[j].ReplyCount = len(rs)
			if len(rs) > 0 {
				h[j].LatestReply = rs[len(rs)-1].TS
			}
		}
	}
	return true
}

// FollowQuiet makes the viewer follow the thread under root in ch's
// history with lastRead the last reply they read, as the server's copies
// of the root then carry it (subscribed, last_read), unpushed. It
// reports whether it found the root.
func (s *Server) FollowQuiet(ch, root, lastRead string) bool {
	s.histMu.Lock()
	defer s.histMu.Unlock()
	h := s.histLocked(ch)
	for i := range h {
		if h[i].TS == root {
			h[i].Subscribed, h[i].LastRead = true, lastRead
			return true
		}
	}
	return false
}
