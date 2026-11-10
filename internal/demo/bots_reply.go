package demo

import (
	"regexp"
	"strings"
	"time"

	"github.com/some-du6e/clack/internal/slack"
)

// Answers to the viewer. When you post, the cast replies in character
// within a few seconds: in a DM that person, otherwise whoever you
// mentioned (by @ or first name), whoever started the thread, the
// topic's owner (honey: Dale, printer: Theo…) or the channel's regulars,
// sometimes two of them. Lines come from data/replies*.json by intent.
// Someone usually reacts to your message too.

// genericIntent intents are speech acts; a topic match wins over them when
// picking what to say.
var genericIntent = map[string]bool{"greeting": true, "thanks": true, "bye": true, "question": true, "help": true,
	"joke": true, "compliment": true, "fallback": true}

// intents are the names of the intents text matches, topics first then
// speech acts, each group in script order; "fallback" last.
func (e *engine) intents(text string) []string {
	low := " " + normText(text) + " "
	var topics, acts []string
	for _, in := range e.s.Replies.Intents {
		matched := false
		switch in.Name {
		case "fallback":
			continue
		case "question":
			matched = strings.HasSuffix(strings.TrimSpace(text), "?")
		}
		for _, p := range in.Patterns {
			if matched {
				break
			}
			matched = patternHit(low, p)
		}
		if !matched {
			continue
		}
		if genericIntent[in.Name] {
			acts = append(acts, in.Name)
		} else {
			topics = append(topics, in.Name)
		}
	}
	return append(append(topics, acts...), "fallback")
}

var (
	slackMarkupRe = regexp.MustCompile(`<[^>]*>`)
	nonWordRe     = regexp.MustCompile(`[^a-z0-9'@+*]+`)
)

// normalize lowercases text, drops Slack markup (mentions, links) and
// turns punctuation into spaces.
func normText(text string) string {
	t := slackMarkupRe.ReplaceAllString(strings.ToLower(text), " ")
	t = strings.ReplaceAll(t, "’", "'")
	return strings.Join(strings.Fields(nonWordRe.ReplaceAllString(t, " ")), " ")
}

// patternHit matches a pattern on word boundaries in " "+normalized+" ";
// a trailing * matches a prefix.
func patternHit(low, p string) bool {
	p = normText(p)
	if p == "" {
		return false
	}
	if strings.HasSuffix(p, "*") {
		return strings.Contains(low, " "+strings.TrimSuffix(p, "*"))
	}
	return strings.Contains(low, " "+p+" ")
}

// onViewerPost wakes the bots for the viewer's message m in ch.
func (e *engine) onViewerPost(ch string, m slack.Message, fromAPI bool) {
	switch m.Subtype {
	case "", "me_message", "thread_broadcast":
	default:
		return
	}
	e.w.goBusy(func() { e.respond(ch, m) })
}

func (e *engine) respond(ch string, m slack.Message) {
	w := e.w
	w.mu.Lock()
	c, ok := w.convLocked(ch)
	var conv slack.Conversation
	if ok {
		conv = *c
	}
	var parent slack.Message
	if m.ThreadTS != "" {
		parent, _ = w.findLocked(ch, m.ThreadTS)
	}
	w.mu.Unlock()
	if !ok {
		return
	}
	ments := mentionIDs(m.Text)
	thread := orStr(m.ThreadTS, m.TS)
	if (conv.IsIM || conv.IsMPIM) && m.ThreadTS == "" {
		thread = ""
	}
	// Inkwell: its DM, its code channel, or a mention
	if (conv.IsIM && conv.User == InkwellID) || hasStr(ments, InkwellID) || ch == CodeID {
		e.runAgent(ch, orStr(m.ThreadTS, m.TS), m.Text)
		if ch == CodeID && !hasStr(ments, InkwellID) && w.chance(0.3) {
			e.castReply(ch, thread, m, e.intents(m.Text), []string{[]string{"theo", "priya"}[w.pick(2)]}, 6*time.Second)
		}
		return
	}
	if hasStr(ments, PulseID) || (conv.IsIM && conv.User == PulseID) {
		e.pulseReply(ch, thread, m)
		return
	}
	intents := e.intents(m.Text)
	var who []string
	add := func(h string) { // a handle or user id
		if p, ok := w.person(h); ok && !p.Self && !p.Bot && !hasStr(who, p.Handle) {
			who = append(who, p.Handle)
		}
	}
	switch {
	case conv.IsIM:
		add(conv.User) // a DM is always answered by that person
	default:
		for _, id := range ments {
			add(id)
		}
		low := " " + normText(m.Text) + " "
		for _, p := range w.ws.People {
			if p.Self || p.Bot {
				continue
			}
			first := strings.ToLower(strings.Fields(p.Name + " x")[0])
			if strings.Contains(low, " "+first+" ") || strings.Contains(low, " "+p.Handle+" ") {
				add(p.Handle)
			}
		}
		if len(who) == 0 && parent.User != "" && parent.User != SelfID {
			add(parent.User)
		}
		if len(who) == 0 {
			key := e.channelKey(ch)
			var pool []string
			for _, in := range intents {
				if !genericIntent[in] {
					pool = append(pool, e.s.Replies.Owners[in]...)
				}
			}
			if conv.IsMPIM {
				pool = e.filterMembers(pool, conv)
			}
			if len(pool) == 0 {
				pool = append(pool, e.s.Replies.ChannelOwners[key]...)
			}
			if len(pool) == 0 {
				for _, in := range intents {
					pool = append(pool, e.s.Replies.Owners[in]...)
				}
			}
			if conv.IsMPIM {
				pool = e.filterMembers(pool, conv)
			}
			if len(pool) == 0 {
				for _, p := range w.ws.People {
					if !p.Self && !p.Bot {
						pool = append(pool, p.Handle)
					}
				}
			}
			add(pool[w.pick(len(pool))])
			// sometimes a second voice from the channel's regulars
			if w.chance(0.35) {
				var others []string
				for _, h := range append(e.s.Replies.ChannelOwners[key], pool...) {
					if !hasStr(who, h) {
						others = append(others, h)
					}
				}
				if conv.IsMPIM {
					others = e.filterMembers(others, conv)
				}
				if len(others) > 0 {
					add(others[w.pick(len(others))])
				}
			}
		}
		if len(who) > 2 {
			who = who[:2]
		}
	}
	if len(who) == 0 {
		return
	}
	// a reaction first, usually from the first responder
	if w.chance(0.6) {
		reactor := who[0]
		if len(who) > 1 && w.chance(0.5) {
			reactor = who[1]
		}
		e.reactTo(ch, m.TS, reactor, intents)
	}
	e.castReply(ch, thread, m, intents, who, 0)
}

func (e *engine) filterMembers(handles []string, conv slack.Conversation) []string {
	var out []string
	for _, h := range handles {
		if p, ok := e.w.person(h); ok && hasStr(conv.Members, p.ID) {
			out = append(out, h)
		}
	}
	return out
}

// castReply has each of who answer m, one after the other.
func (e *engine) castReply(ch, thread string, m slack.Message, intents []string, who []string, extra time.Duration) {
	w := e.w
	dm := strings.HasPrefix(ch, "D")
	for i, h := range who {
		p, ok := w.person(h)
		if !ok {
			continue
		}
		text := e.lineFor(h, intents, dm)
		if text == "" {
			continue
		}
		wait := extra + w.randDur(1500*time.Millisecond, 4*time.Second)
		if i > 0 {
			wait += w.randDur(3*time.Second, 8*time.Second)
		}
		if !e.typeThenWait(p.ID, ch, thread, text, wait) {
			return
		}
		w.post(ch, slack.Message{User: p.ID, Text: text, ThreadTS: thread}, false)
	}
}

// lineFor is h's answer for the first intent they have lines for (in a
// DM, dm_fallback before fallback).
func (e *engine) lineFor(h string, intents []string, dm bool) string {
	lines := e.s.Replies.Lines[h]
	for _, in := range intents {
		if in == "fallback" && dm && len(lines["dm_fallback"]) > 0 {
			in = "dm_fallback"
		}
		if ls := lines[in]; len(ls) > 0 {
			return e.expand(e.line(ls), "")
		}
	}
	return ""
}

// reactTo has h react to the viewer's message with an emoji that fits.
func (e *engine) reactTo(ch, ts, h string, intents []string) {
	w := e.w
	p, ok := w.person(h)
	if !ok {
		return
	}
	var names []string
	for _, in := range intents {
		for _, n := range e.s.Replies.Reactions[in] {
			if w.validEmoji(n) {
				names = append(names, n)
			}
		}
		if len(names) > 0 {
			break
		}
	}
	if len(names) == 0 {
		names = []string{"eyes", "+1", "raised_hands"}
	}
	name := names[w.pick(len(names))]
	if !w.sleep(w.randDur(800*time.Millisecond, 2500*time.Millisecond)) {
		return
	}
	w.react(p.ID, ch, ts, name, true)
}

// pulseReply: Pulse only does polls, and says so with one.
func (e *engine) pulseReply(ch, thread string, m slack.Message) {
	w := e.w
	if !w.sleep(w.randDur(time.Second, 2*time.Second)) {
		return
	}
	lunch := e.expand("{#lunch}", "")
	w.post(ch, slack.Message{User: PulseID, ThreadTS: thread,
		Text: "Beep. I'm Pulse: I run polls and check-ins. Starting a fresh poll in " + lunch + " now :bar_chart:"}, false)
	if w.sleep(w.randDur(time.Second, 3*time.Second)) {
		w.PostPoll(LunchID)
	}
}
