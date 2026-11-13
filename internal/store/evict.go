package store

import (
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/some-du6e/clack/internal/slack"
)

// Slack's web client keeps a bounded message cache: CacheManager "messages"
// (client-boot WcSL and its messages manager, build 0e695dd18302fa08,
// 2026-11): once the messages it may evict (getUnusedMessages) number
// cacheSizeMaximum, or their JSON (getApproximateMessageSize,
// JSON.stringify(message).length) reaches byteBudget.maximum, it keeps the
// newest cacheSizeTarget of them within byteBudget.target and removes the
// rest (bulkRemoveMessagesById with isCacheEviction; a channel losing all
// of them has its history reset, evictUncountedChannelHistories). Clack
// held every message of every conversation it ever opened (5,000 a
// conversation): at the Hack Club account's scale the messages, their
// documents, rows and plugin mirror were several hundred megabytes.
const (
	EvictMaximum     = 3000    // cacheSizeMaximum
	EvictTarget      = 2000    // cacheSizeTarget
	EvictBytesMax    = 3145728 // byteBudget.maximum
	EvictBytesTarget = 2097152 // byteBudget.target
	// EvictRecent is the comparator's window: messages accessed within it
	// sort by when they were accessed, older ones by their ts; a
	// conversation not accessed within it may be reset whole.
	EvictRecent = time.Hour
)

// EvictInput is what Slack's getUnusedMessages and comparator read
// besides the messages themselves.
type EvictInput struct {
	Now time.Time
	// Accessed is when each conversation's messages were last read
	// (opened, scrolled, refreshed); Slack records every message read.
	Accessed map[string]time.Time
	// Keep are conversations (and "channel/thread_ts" threads) on screen:
	// nothing in them is evicted.
	Keep map[string]bool
	// Conv reports what Slack's getChannel says: whether the conversation
	// is known, and whether the viewer is a member (isMemberOfChannel).
	Conv func(ch string) (known, member bool)
	// WholeOK is isUncountedChannelEvictable: a member conversation whose
	// history may be reset whole when it hasn't been accessed within
	// EvictRecent (no mention or VIP unreads, not one Slack keeps).
	WholeOK func(ch string) bool
	// LastRead is the conversation's read cursor (getLastReadTs).
	LastRead func(ch string) string
	// Pending are the client_msg_ids of sends not confirmed yet (or
	// failed): local messages that never come back from history.
	Pending map[string]bool
}

// Evicted is one conversation (or thread, Thread set) EvictMessages
// trimmed: Whole when its history went entirely, else Oldest is the
// oldest message it kept (older ones load again from Slack).
type Evicted struct {
	Channel, Thread string
	Whole           bool
	Oldest          string
}

// EvictStats is what one EvictMessages pass saw and did (core logs it).
type EvictStats struct {
	Timelines, Threads, Messages int
	Kept                         int // conversations and threads skipped as on screen
	Unknown                      int // conversations skipped as unknown
	Candidates, Bytes            int // the messages it may evict, and their JSON size
	Evicted                      int // messages removed
}

type evictCand struct {
	key   string // channel, or channel/thread_ts
	ts    string
	order float64 // the comparator's value: ascending is evicted first
	size  int
}

// messageSize approximates JSON.stringify(message).length: the text,
// blocks and attachments as sent, plus the message's other fields.
func messageSize(m *slack.Message) int {
	n := 240 + len(m.Text) + len(m.Blocks) + len(m.RawAttachments) + len(m.Metadata)
	if len(m.RawAttachments) == 0 {
		n += 400 * len(m.Attachments)
	}
	n += 900 * len(m.Files)
	for _, r := range m.Reactions {
		n += 40 + 14*len(r.Users)
	}
	n += 14 * len(m.ReplyUsers)
	return n
}

// persistable is Slack's shouldPersistMessage: no ephemeral or local
// message is ever evicted (they don't come back from history).
func (in *EvictInput) persistable(m *slack.Message) bool {
	return !m.IsEphemeral && m.TS != "" && !(m.ClientMsgID != "" && in.Pending[m.ClientMsgID])
}

func tsMillis(ts string) float64 {
	f, _ := strconv.ParseFloat(ts, 64)
	return f * 1000
}

// convFacts is what EvictInput's funcs said about one conversation (read
// before the store is locked: they may read the store themselves).
type convFacts struct {
	known, member, wholeOK bool
	lastRead               string
}

// EvictMessages applies Slack's message cache budget to the cached
// conversations and threads, and reports what it removed.
func (s *Store) EvictMessages(in EvictInput) []Evicted {
	out, _ := s.EvictMessagesStats(in)
	return out
}

// EvictMessagesStats is EvictMessages with what the pass saw.
func (s *Store) EvictMessagesStats(in EvictInput) ([]Evicted, EvictStats) {
	var st EvictStats
	s.mu.RLock()
	ids := make([]string, 0, len(s.timelines))
	for ch := range s.timelines {
		ids = append(ids, ch)
	}
	for k := range s.threads {
		ch, _, _ := strings.Cut(k, "/")
		ids = append(ids, ch)
	}
	s.mu.RUnlock()
	facts := make(map[string]convFacts, len(ids))
	for _, ch := range ids {
		if _, done := facts[ch]; done {
			continue
		}
		var f convFacts
		f.known, f.member = in.Conv(ch)
		if f.known && f.member {
			if in.WholeOK != nil {
				f.wholeOK = in.WholeOK(ch)
			}
			f.lastRead = in.LastRead(ch)
		}
		facts[ch] = f
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	recent := func(key string) (time.Time, bool) {
		at, ok := in.Accessed[key]
		return at, ok && in.Now.Sub(at) < EvictRecent
	}
	order := func(key, ts string) float64 {
		if at, ok := recent(key); ok {
			return float64(at.UnixMilli())
		}
		return tsMillis(ts)
	}
	var cands []evictCand
	add := func(key string, msgs []slack.Message, pick func(*slack.Message) bool) {
		for i := range msgs {
			m := &msgs[i]
			if in.persistable(m) && pick(m) {
				cands = append(cands, evictCand{key: key, ts: m.TS, order: order(key, m.TS), size: messageSize(m)})
			}
		}
	}
	all := func(*slack.Message) bool { return true }
	st.Timelines, st.Threads = len(s.timelines), len(s.threads)
	for _, t := range s.timelines {
		st.Messages += len(t.Msgs)
	}
	for _, th := range s.threads {
		st.Messages += len(th)
	}
	for ch, t := range s.timelines {
		if in.Keep[ch] || len(t.Msgs) == 0 {
			st.Kept++
			continue
		}
		f, ok := facts[ch]
		switch {
		case !ok || !f.known:
			st.Unknown++
			continue // a conversation added since: next time
		case !f.member:
			add(ch, t.Msgs, all)
		default:
			if _, ok := recent(ch); !ok && f.wholeOK && in.allPersistable(t.Msgs) {
				add(ch, t.Msgs, all) // evictUncountedChannelHistories
				continue
			}
			lr := f.lastRead
			if lr == "" {
				lr = "0000000000.000000"
			}
			add(ch, t.Msgs, func(m *slack.Message) bool { return !TSLess(lr, m.TS) })
		}
	}
	// threads: a reply is a message like any other; one on screen stays
	for k, th := range s.threads {
		ch, _, _ := strings.Cut(k, "/")
		if in.Keep[k] || in.Keep[ch] || len(th) == 0 {
			st.Kept++
			continue
		}
		if !facts[ch].known {
			st.Unknown++
			continue
		}
		add(k, th, all)
	}
	bytes := 0
	for _, c := range cands {
		bytes += c.size
	}
	st.Candidates, st.Bytes = len(cands), bytes
	if len(cands) < EvictMaximum && bytes < EvictBytesMax {
		return nil, st
	}
	sort.SliceStable(cands, func(i, j int) bool {
		if cands[i].order != cands[j].order {
			return cands[i].order < cands[j].order
		}
		return TSLess(cands[i].ts, cands[j].ts)
	})
	// takeItemsToEvict: keep from the newest end until the count or the
	// byte target is reached, evict everything before it
	kept, keptBytes, r := 0, 0, len(cands)
	for r > 0 && kept < EvictTarget {
		n := cands[r-1].size
		if r != len(cands) && keptBytes+n > EvictBytesTarget {
			break
		}
		keptBytes += n
		kept++
		r--
	}
	gone := map[string]map[string]bool{}
	st.Evicted = r
	for _, c := range cands[:r] {
		if gone[c.key] == nil {
			gone[c.key] = map[string]bool{}
		}
		gone[c.key][c.ts] = true
	}
	var out []Evicted
	for key, ts := range gone {
		ch, thread, isThread := strings.Cut(key, "/")
		if isThread {
			th := s.threads[key]
			keep := dropTS(th, ts)
			if len(keep) == 0 || len(keep) == 1 && keep[0].TS == thread {
				delete(s.threads, key)
				out = append(out, Evicted{Channel: ch, Thread: thread, Whole: true})
			} else {
				s.threads[key] = keep
				out = append(out, Evicted{Channel: ch, Thread: thread, Oldest: keep[0].TS})
			}
			continue
		}
		t := s.timelines[ch]
		keep := dropTS(t.Msgs, ts)
		if len(keep) == 0 {
			// evictUncountedChannelHistories: the history starts over
			delete(s.timelines, ch)
			for k := range s.threads {
				if strings.HasPrefix(k, ch+"/") && !in.Keep[k] {
					delete(s.threads, k)
				}
			}
			out = append(out, Evicted{Channel: ch, Whole: true})
			continue
		}
		t.Msgs, t.HasMore = keep, true
		out = append(out, Evicted{Channel: ch, Oldest: keep[0].TS})
	}
	sort.Slice(out, func(i, j int) bool {
		if out[i].Channel != out[j].Channel {
			return out[i].Channel < out[j].Channel
		}
		return out[i].Thread < out[j].Thread
	})
	return out, st
}

func (in *EvictInput) allPersistable(msgs []slack.Message) bool {
	for i := range msgs {
		if !in.persistable(&msgs[i]) {
			return false
		}
	}
	return true
}

// dropTS is msgs without the messages whose ts is in gone, in a new array
// (the old one, with every evicted message in it, is let go).
func dropTS(msgs []slack.Message, gone map[string]bool) []slack.Message {
	n := 0
	for i := range msgs {
		if !gone[msgs[i].TS] {
			n++
		}
	}
	out := make([]slack.Message, 0, n)
	for i := range msgs {
		if !gone[msgs[i].TS] {
			out = append(out, msgs[i])
		}
	}
	return out
}
