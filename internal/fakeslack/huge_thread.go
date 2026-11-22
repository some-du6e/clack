package fakeslack

import (
	"encoding/json"
	"fmt"
	"net/url"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/some-du6e/clack/internal/slack"
)

// A huge thread: the shape of Karim's #largest-thread-on-slack (a public
// channel the viewer isn't in, one root with 1,078,423 replies from 233
// people, the newest a few hours old), every reply synthetic and made on
// request from its index, so nothing of the real thread is ever read or
// kept. conversations.replies answers it as Slack's server does: the root
// first in every answer, then up to limit replies (1000 at most) in
// order, from oldest (inclusive unless inclusive=false) when it is set or
// no bound is, else the newest up to latest; has_more says whether
// replies are left past the page in the direction it read, and a page
// read forward names its next_cursor. Every request is kept, in order
// (HugeThreadRequests), so tests can hold the client to Slack's page
// sizes and order. With SetHugeThreadMention, activity.feed answers one
// unread mention of the viewer in a reply in the middle of the thread,
// and messages.list answers the thread's messages.

// HugeThread is the thread AddHugeThread made.
type HugeThread struct {
	Channel, Name string
	Root          string // the root's ts
	Latest        string // the newest reply's ts (the root's latest_reply)
	Replies       int
	Authors       []string
}

// HugeThreadChannelID is the huge thread's channel.
const HugeThreadChannelID = "C0HUGE00001"

type hugeState struct {
	mu      sync.Mutex
	t       HugeThread
	base    int64 // the root's second; reply i is at base+i seconds
	root    slack.Message
	older   []slack.Message // the channel's messages before the root, newest first
	reqs    []url.Values
	mention int // the reply activity.feed names (0: none)
	// find is what search.inline's find_in_thread answers for a query
	// (SetHugeThreadFind): reply numbers
	find map[string][]int
}

// SetHugeThreadFind makes search.inline's find_in_thread over the huge
// thread answer query (lowercased, trimmed, as the bar sends it) with
// replies, made up like every other (the fake searches no text of its
// own).
func (s *Server) SetHugeThreadFind(query string, replies ...int) {
	h := s.hugeThread()
	if h == nil {
		return
	}
	h.mu.Lock()
	defer h.mu.Unlock()
	if h.find == nil {
		h.find = map[string][]int{}
	}
	h.find[strings.ToLower(strings.TrimSpace(query))] = append([]int(nil), replies...)
}

// hugeThreadFind is find_in_thread's hits in the huge thread ch/ts for q
// (SetHugeThreadFind's), ok false when ch/ts isn't it.
func (s *Server) hugeThreadFind(ch, ts, q string) ([]slack.Message, bool) {
	h := s.hugeThread()
	if h == nil || ch != h.t.Channel || ts != h.t.Root {
		return nil, false
	}
	h.mu.Lock()
	defer h.mu.Unlock()
	var out []slack.Message
	for _, i := range h.find[q] {
		if i >= 1 && i <= h.t.Replies {
			out = append(out, h.reply(i))
		}
	}
	return out, true
}

var hugeStates = newPerServer() // *Server -> *hugeState

func (s *Server) hugeThread() *hugeState {
	v, ok := hugeStates.Load(s)
	if !ok {
		return nil
	}
	return v.(*hugeState)
}

// AddHugeThread adds #name, a public channel the viewer isn't in
// (HugeThreadChannelID), whose newest message is the root of a thread
// with n replies from the first authors people of the directory, the
// newest two hours old, one a second.
func (s *Server) AddHugeThread(name string, n, authors int) HugeThread {
	ids := s.UserIDs()
	if authors > len(ids)-1 {
		authors = len(ids) - 1
	}
	if authors < 1 {
		authors = 1
	}
	people := append([]string(nil), ids[1:1+authors]...) // the viewer isn't one of them
	base := time.Now().Unix() - int64(n) - 2*3600
	h := &hugeState{base: base}
	h.t = HugeThread{Channel: HugeThreadChannelID, Name: name, Root: fmt.Sprintf("%d.%06d", base, 100),
		Latest: h.replyTS(n), Replies: n, Authors: people}
	reply := min(5, len(people))
	h.root = slack.Message{Type: "message", TS: h.t.Root, User: people[0], Text: "a synthetic thread: every reply is made up on request",
		ThreadTS: h.t.Root, ReplyCount: n, ReplyUsersN: len(people), ReplyUsers: append([]string(nil), people[:reply]...),
		LatestReply: h.t.Latest, Blocks: hugeBlocks("a synthetic thread: every reply is made up on request")}
	for k := 1; k <= 3; k++ {
		ts := fmt.Sprintf("%d.%06d", base-int64(3600*k), 100)
		txt := fmt.Sprintf("synthetic message %d before the thread", k)
		h.older = append(h.older, slack.Message{Type: "message", TS: ts, User: people[k%len(people)], Text: txt, Blocks: hugeBlocks(txt)})
	}
	hugeStates.Store(s, h)
	s.AddOtherChannel(slack.Conversation{ID: HugeThreadChannelID, Name: name, Created: base - 9e6, Creator: people[0],
		NumMembers: 233, Purpose: &slack.Purpose{Value: "A synthetic channel with one very long thread."}})
	s.histMu.Lock()
	s.hist[HugeThreadChannelID] = append([]slack.Message{h.root}, h.older...)
	s.histMu.Unlock()
	return h.t
}

// SetHugeThreadMention makes activity.feed answer an unread mention of
// the viewer in reply i of the huge thread (0: none); it returns the
// reply's ts.
func (s *Server) SetHugeThreadMention(i int) string {
	h := s.hugeThread()
	if h == nil {
		return ""
	}
	h.mu.Lock()
	defer h.mu.Unlock()
	if i < 0 || i > h.t.Replies {
		i = 0
	}
	h.mention = i
	if i == 0 {
		return ""
	}
	return h.replyTS(i)
}

// HugeThreadRequests is every conversations.replies request for the huge
// thread so far, oldest first.
func (s *Server) HugeThreadRequests() []url.Values {
	h := s.hugeThread()
	if h == nil {
		return nil
	}
	h.mu.Lock()
	defer h.mu.Unlock()
	out := make([]url.Values, len(h.reqs))
	for i, r := range h.reqs {
		c := url.Values{}
		for k, v := range r {
			c[k] = append([]string(nil), v...)
		}
		out[i] = c
	}
	return out
}

// HugeThreadReplyTS is reply i's ts (1 is the oldest).
func (s *Server) HugeThreadReplyTS(i int) string {
	h := s.hugeThread()
	if h == nil {
		return ""
	}
	return h.replyTS(i)
}

func (h *hugeState) replyTS(i int) string { return fmt.Sprintf("%d.%06d", h.base+int64(i), 200) }

// micros is ts as microseconds (false when it isn't one).
func micros(ts string) (int64, bool) {
	a, b, ok := strings.Cut(ts, ".")
	if !ok {
		b = "0"
	}
	sec, err := strconv.ParseInt(a, 10, 64)
	if err != nil {
		return 0, false
	}
	for len(b) < 6 {
		b += "0"
	}
	us, err := strconv.ParseInt(b[:6], 10, 64)
	if err != nil {
		return 0, false
	}
	return sec*1e6 + us, true
}

func (h *hugeState) replyMicros(i int) int64 { return (h.base+int64(i))*1e6 + 200 }

// firstAtOrAfter is the first reply index at or after (after, with
// strict) us; past n when none.
func (h *hugeState) firstAtOrAfter(us int64, strict bool) int {
	n := h.t.Replies
	lo, hi := 1, n+1
	for lo < hi {
		mid := (lo + hi) / 2
		v := h.replyMicros(mid)
		if v > us || !strict && v == us {
			hi = mid
		} else {
			lo = mid + 1
		}
	}
	return lo
}

// lastAtOrBefore is the last reply index at or before (before, with
// strict) us; 0 when none.
func (h *hugeState) lastAtOrBefore(us int64, strict bool) int {
	return h.firstAtOrAfter(us, !strict) - 1
}

// words are the replies' synthetic words.
var hugeWords = []string{"ok", "lol", "yes", "no", "hi", "same", "wow", "ha", "gg", "nice", "real", "true", "fr", "omg", "yo",
	"ship", "it", "thread", "longest", "counting", "still", "here", "again", "more", "next"}

// reply is reply i, made up from i alone.
func (h *hugeState) reply(i int) slack.Message {
	p := h.t.Authors
	user := p[(i*131+i/7)%len(p)]
	var txt string
	switch k := i % 10; {
	case k < 6:
		txt = hugeWords[(i/10)%len(hugeWords)]
	case k < 9:
		var b strings.Builder
		for w := 0; w < 2+i%4; w++ {
			if w > 0 {
				b.WriteByte(' ')
			}
			b.WriteString(hugeWords[(i+w*7)%len(hugeWords)])
		}
		txt = b.String()
	default:
		txt = fmt.Sprintf("reply %d of the synthetic thread, a longer one: %s %s %s, and the thread goes on and on without an end in sight",
			i, hugeWords[i%len(hugeWords)], hugeWords[(i/3)%len(hugeWords)], hugeWords[(i/5)%len(hugeWords)])
	}
	if i == h.mention {
		txt = "<@" + SelfID + "> this one's for you: " + txt
	}
	m := slack.Message{Type: "message", TS: h.replyTS(i), User: user, Text: txt, ThreadTS: h.t.Root, ParentUser: h.root.User,
		Blocks: hugeBlocks(txt)}
	if i%18 == 0 {
		m.Reactions = []slack.Reaction{{Name: []string{"fire", "eyes", "joy"}[i%3], Count: 1 + i%3, Users: p[:1+i%3]}}
	}
	return m
}

// hugeBlocks is text as one rich_text block.
func hugeBlocks(txt string) json.RawMessage {
	b, _ := json.Marshal([]any{map[string]any{"type": "rich_text", "block_id": "hg", "elements": []any{
		map[string]any{"type": "rich_text_section", "elements": []any{map[string]any{"type": "text", "text": txt}}}}}})
	return b
}

// handleHugeThread answers the huge thread's methods.
func (s *Server) handleHugeThread(method string, f url.Values) (any, bool) {
	h := s.hugeThread()
	if h == nil {
		return nil, false
	}
	switch method {
	case "conversations.replies":
		if f.Get("channel") != h.t.Channel || f.Get("ts") != h.t.Root {
			return nil, false
		}
		return h.replies(f), true
	case "activity.feed":
		h.mu.Lock()
		i := h.mention
		var author string
		if i > 0 {
			author = h.reply(i).User
		}
		h.mu.Unlock()
		if i == 0 {
			return nil, false
		}
		ts := h.replyTS(i)
		item := map[string]any{"key": "fa-huge-thread", "feed_ts": ts, "is_unread": true, "is_bot": false,
			"item": map[string]any{"type": "at_user", "message": map[string]any{"ts": ts, "channel": h.t.Channel, "thread_ts": h.t.Root,
				"is_broadcast": false, "author_user_id": author}}}
		return ok("items", []any{item}, "response_metadata", map[string]any{"next_cursor": ""}), true
	case "messages.list":
		var want []struct {
			Channel    string   `json:"channel"`
			Timestamps []string `json:"timestamps"`
		}
		if json.Unmarshal([]byte(f.Get("message_ids")), &want) != nil {
			return nil, false
		}
		mine := false
		for _, w := range want {
			mine = mine || w.Channel == h.t.Channel
		}
		if !mine {
			return nil, false
		}
		out := map[string][]slack.Message{}
		for _, w := range want {
			for _, ts := range w.Timestamps {
				if w.Channel == h.t.Channel {
					if m, ok := h.message(ts); ok {
						out[w.Channel] = append(out[w.Channel], m)
					}
				} else if m, ok := s.messageByTS(w.Channel, ts); ok {
					out[w.Channel] = append(out[w.Channel], m)
				}
			}
		}
		return ok("messages", out), true
	}
	return nil, false
}

// message is the huge channel's message ts: the root, a reply, or one of
// the messages before the root.
func (h *hugeState) message(ts string) (slack.Message, bool) {
	if ts == h.t.Root {
		return h.root, true
	}
	for _, m := range h.older {
		if m.TS == ts {
			return m, true
		}
	}
	us, ok := micros(ts)
	if !ok {
		return slack.Message{}, false
	}
	i := h.firstAtOrAfter(us, false)
	if i < 1 || i > h.t.Replies || h.replyMicros(i) != us {
		return slack.Message{}, false
	}
	h.mu.Lock()
	defer h.mu.Unlock()
	return h.reply(i), true
}

// replies is conversations.replies for the huge thread.
func (h *hugeState) replies(f url.Values) any {
	h.mu.Lock()
	defer h.mu.Unlock()
	c := url.Values{}
	for k, v := range f {
		if k != "token" {
			c[k] = append([]string(nil), v...)
		}
	}
	h.reqs = append(h.reqs, c)
	n := h.t.Replies
	limit, _ := strconv.Atoi(f.Get("limit"))
	if limit <= 0 || limit > 1000 {
		limit = 1000
	}
	strict := f.Get("inclusive") == "false" || f.Get("inclusive") == "0"
	lo, hi := 1, n
	oldest, latest := f.Get("oldest"), f.Get("latest")
	if us, ok := micros(oldest); ok && oldest != "" {
		lo = max(lo, h.firstAtOrAfter(us, strict))
	}
	if us, ok := micros(latest); ok && latest != "" {
		hi = min(hi, h.lastAtOrBefore(us, strict))
	}
	from, to, more, next := lo, hi, false, ""
	switch cur := f.Get("cursor"); {
	case strings.HasPrefix(cur, "i:"):
		from, _ = strconv.Atoi(cur[2:])
		from = max(from, lo)
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
	msgs := make([]slack.Message, 0, max(0, to-from+1)+1)
	msgs = append(msgs, h.root)
	for i := from; i <= to && i <= n; i++ {
		msgs = append(msgs, h.reply(i))
	}
	return ok("messages", msgs, "has_more", more, "response_metadata", map[string]any{"next_cursor": next})
}
