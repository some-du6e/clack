package demo

import (
	"encoding/json"
	"fmt"
	"net/url"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/some-du6e/clack/internal/slack"
)

// Pulse's polls and stand-up check-ins: Block Kit messages whose buttons
// work. blocks.actions records the vote, rewrites the message (counts,
// voters' avatars and names) and pushes message_changed; the answer is
// just ok, as core/interactive.go expects (the update comes over the
// socket). The cast's scripted votes trickle in after a poll is posted.

// Poll element ids. Each option is a section (block_id PollOptionBlock +
// the option's value) whose Vote button has action_id PollVoteAction and
// the option's value; "Close poll" is PollCloseAction in the
// PollActionsBlock actions block. The stand-up's static_select is
// StandupAction.
const (
	PollVoteAction   = "pulse_poll_vote"
	PollCloseAction  = "pulse_poll_close"
	PollOptionBlock  = "pulse_opt_"
	PollActionsBlock = "pulse_actions"
	StandupAction    = "pulse_standup"
)

type pollState struct {
	ch, ts   string
	poll     Poll
	votes    map[string]string // user -> option value
	order    []string          // voters, first vote first
	closed   bool
	closedBy string
	standup  bool
}

func rawJSON(v any) json.RawMessage {
	b, _ := json.Marshal(v)
	return b
}

func bkPlain(s string) map[string]any {
	return map[string]any{"type": "plain_text", "text": s, "emoji": true}
}
func bkMrkdwn(s string) map[string]any { return map[string]any{"type": "mrkdwn", "text": s} }

// renderLocked is the poll's blocks now (w.mu held).
func (w *World) renderPollLocked(ps *pollState) []any {
	if ps.standup {
		return w.renderStandupLocked(ps)
	}
	title := "*:bar_chart: " + ps.poll.Question + "*"
	if ps.closed {
		title += "  _(closed)_"
	}
	blocks := []any{map[string]any{"type": "section", "block_id": "pulse_q", "text": bkMrkdwn(title)}}
	total := len(ps.votes)
	best, bestN := "", 0
	for _, o := range ps.poll.Options {
		var voters []string
		for _, u := range ps.order {
			if ps.votes[u] == o.Value {
				voters = append(voters, u)
			}
		}
		n := len(voters)
		if n > bestN {
			best, bestN = o.Text, n
		}
		bar := ""
		if total > 0 {
			bar = strings.Repeat("▓", n*10/total) + strings.Repeat("░", 10-n*10/total) + "  "
		}
		sec := map[string]any{"type": "section", "block_id": PollOptionBlock + o.Value,
			"text": bkMrkdwn(fmt.Sprintf("%s *%s*\n%s`%d`", o.Emoji, o.Text, bar, n))}
		if !ps.closed {
			btn := map[string]any{"type": "button", "action_id": PollVoteAction, "value": o.Value, "text": bkPlain("Vote")}
			if ps.votes[SelfID] == o.Value {
				btn["text"], btn["style"] = bkPlain(":white_check_mark: Voted"), "primary"
			}
			sec["accessory"] = btn
		}
		blocks = append(blocks, sec)
		var els []any
		var names []string
		for i, u := range voters {
			usr, ok := w.users[u]
			if !ok {
				continue
			}
			name := orStr(usr.Profile.DisplayName, usr.Name)
			if u == SelfID {
				name = "you"
			}
			names = append(names, name)
			if i < 8 && usr.Profile.Image48 != "" {
				els = append(els, map[string]any{"type": "image", "image_url": usr.Profile.Image48, "alt_text": usr.RealName})
			}
		}
		label := "No votes"
		if n > 0 {
			label = strconv.Itoa(n) + " vote" + map[bool]string{true: "", false: "s"}[n == 1] + ": " + strings.Join(names, ", ")
		}
		els = append(els, bkMrkdwn(label))
		blocks = append(blocks, map[string]any{"type": "context", "block_id": "pulse_voters_" + o.Value, "elements": els})
	}
	blocks = append(blocks, map[string]any{"type": "divider"})
	if ps.closed {
		who := "Pulse"
		if u, ok := w.users[ps.closedBy]; ok {
			who = orStr(u.Profile.DisplayName, u.Name)
		}
		res := "No votes, no winner. Democracy is hard."
		if bestN > 0 {
			res = "Winner: *" + best + "*"
		}
		blocks = append(blocks, map[string]any{"type": "context", "block_id": "pulse_result",
			"elements": []any{bkMrkdwn(":lock: Closed by " + who + ". " + res)}})
	} else {
		blocks = append(blocks, map[string]any{"type": "actions", "block_id": PollActionsBlock, "elements": []any{
			map[string]any{"type": "button", "action_id": PollCloseAction, "value": "close", "text": bkPlain("Close poll"), "style": "danger"}}})
	}
	blocks = append(blocks, map[string]any{"type": "context", "block_id": "pulse_footer",
		"elements": []any{bkMrkdwn(fmt.Sprintf("%d vote%s · one vote each, change it any time · Pulse polls", total, map[bool]string{true: "", false: "s"}[total == 1]))}})
	return blocks
}

func (w *World) renderStandupLocked(ps *pollState) []any {
	st := w.scripts.Standup
	var opts []any
	for _, o := range st.Options {
		opts = append(opts, map[string]any{"text": bkPlain(o.Emoji + " " + o.Text), "value": o.Value})
	}
	sel := map[string]any{"type": "static_select", "action_id": StandupAction, "placeholder": bkPlain("How's it going?"), "options": opts}
	if v := ps.votes[SelfID]; v != "" {
		for _, o := range opts {
			if o.(map[string]any)["value"] == v {
				sel["initial_option"] = o
			}
		}
	}
	var parts []string
	for _, u := range ps.order {
		usr, ok := w.users[u]
		if !ok {
			continue
		}
		emo := ""
		for _, o := range st.Options {
			if o.Value == ps.votes[u] {
				emo = o.Emoji + " "
			}
		}
		name := orStr(usr.Profile.DisplayName, usr.Name)
		if u == SelfID {
			name = "you"
		}
		parts = append(parts, emo+name)
	}
	who := "Nobody has checked in yet."
	if len(parts) > 0 {
		who = "Checked in: " + strings.Join(parts, " · ")
	}
	return []any{
		map[string]any{"type": "section", "block_id": "pulse_standup_q", "text": bkMrkdwn("*:sunrise: " + st.Prompt + "*"), "accessory": sel},
		map[string]any{"type": "context", "block_id": "pulse_standup_who", "elements": []any{bkMrkdwn(who)}},
	}
}

// PostPoll has Pulse post its next poll in channel now and starts the
// cast's votes; it returns the poll's ts. Its first option's Vote button
// is action_id PollVoteAction, value Poll.Options[0].Value, block_id
// PollOptionBlock + that value.
func (w *World) PostPoll(channel string) string {
	polls := w.scripts.Polls
	if len(polls) == 0 {
		return ""
	}
	e := w.bots
	e.mu.Lock()
	p := polls[e.pollNext%len(polls)]
	e.pollNext++
	e.mu.Unlock()
	ps := &pollState{ch: channel, poll: p, votes: map[string]string{}}
	w.mu.Lock()
	blocks := w.renderPollLocked(ps)
	w.mu.Unlock()
	ts, _ := w.post(channel, slack.Message{User: PulseID, Text: "Poll: " + p.Question, Blocks: rawJSON(blocks)}, false)
	ps.ts = ts
	w.mu.Lock()
	w.polls[channel+"/"+ts] = ps
	w.mu.Unlock()
	w.goBusy(func() { e.castVotes(ps) })
	return ts
}

// PostStandup has Pulse post the stand-up check-in in channel now.
func (w *World) PostStandup(channel string) string {
	st := w.scripts.Standup
	if len(st.Options) == 0 {
		return ""
	}
	ps := &pollState{ch: channel, votes: map[string]string{}, standup: true}
	w.mu.Lock()
	blocks := w.renderPollLocked(ps)
	w.mu.Unlock()
	ts, _ := w.post(channel, slack.Message{User: PulseID, Text: "Check-in: " + st.Prompt, Blocks: rawJSON(blocks)}, false)
	ps.ts = ts
	w.mu.Lock()
	w.polls[channel+"/"+ts] = ps
	w.mu.Unlock()
	answers := map[string]string{}
	for h, v := range st.Answers {
		answers[h] = v
	}
	w.goBusy(func() { w.bots.trickle(ps, answers, nil) })
	return ts
}

func (e *engine) castVotes(ps *pollState) {
	e.trickle(ps, ps.poll.Votes, ps.poll.Comments)
}

// trickle casts the scripted votes a few seconds apart, in a random
// order; a commenter votes, then says their piece in the thread.
func (e *engine) trickle(ps *pollState, votes map[string]string, comments []PollComment) {
	w := e.w
	var who []string
	for h := range votes {
		who = append(who, h)
	}
	sort.Strings(who)
	for i := len(who) - 1; i > 0; i-- {
		j := w.pick(i + 1)
		who[i], who[j] = who[j], who[i]
	}
	said := map[int]bool{}
	for _, h := range who {
		p, ok := w.person(h)
		if !ok {
			continue
		}
		if !w.sleep(w.randDur(3*time.Second, 10*time.Second)) {
			return
		}
		if !w.vote(ps, p.ID, votes[h], false) {
			return // closed
		}
		for i, c := range comments {
			if c.Who != h || said[i] {
				continue
			}
			said[i] = true
			text := e.expand(c.Text, "")
			if !e.typeThenWait(p.ID, ps.ch, ps.ts, text, w.randDur(2*time.Second, 5*time.Second)) {
				return
			}
			w.post(ps.ch, slack.Message{User: p.ID, Text: text, ThreadTS: ps.ts}, false)
		}
	}
}

// vote records user's vote (toggle: the same option again takes it back)
// and rewrites the message; false when the poll is closed.
func (w *World) vote(ps *pollState, user, value string, toggle bool) bool {
	open := true
	w.change(ps.ch, ps.ts, func(m *slack.Message) {
		if ps.closed {
			open = false
			return
		}
		if cur, had := ps.votes[user]; had && cur == value && toggle {
			delete(ps.votes, user)
			var keep []string
			for _, u := range ps.order {
				if u != user {
					keep = append(keep, u)
				}
			}
			ps.order = keep
		} else {
			if _, had := ps.votes[user]; !had {
				ps.order = append(ps.order, user)
			}
			ps.votes[user] = value
		}
		m.Blocks = rawJSON(w.renderPollLocked(ps))
	})
	return open
}

// PollVotes is a poll's votes: option value -> voter ids, in vote order.
func (w *World) PollVotes(channel, ts string) map[string][]string {
	w.mu.Lock()
	defer w.mu.Unlock()
	out := map[string][]string{}
	ps := w.polls[channel+"/"+ts]
	if ps == nil {
		return out
	}
	for _, u := range ps.order {
		out[ps.votes[u]] = append(out[ps.votes[u]], u)
	}
	return out
}

// blockAction answers blocks.actions for Pulse's and Inkwell's messages
// (anything else is the fake's).
func (w *World) blockAction(f url.Values) (any, bool) {
	var acts []struct {
		ActionID string `json:"action_id"`
		BlockID  string `json:"block_id"`
		Value    string `json:"value"`
		Selected *struct {
			Value string `json:"value"`
		} `json:"selected_option"`
	}
	var cont struct {
		Channel string `json:"channel_id"`
		TS      string `json:"message_ts"`
	}
	_ = json.Unmarshal([]byte(f.Get("actions")), &acts)
	_ = json.Unmarshal([]byte(f.Get("container")), &cont)
	if len(acts) == 0 {
		return nil, false
	}
	a := acts[0]
	w.mu.Lock()
	ps := w.polls[cont.Channel+"/"+cont.TS]
	w.mu.Unlock()
	switch {
	case ps != nil && a.ActionID == PollVoteAction:
		if !w.vote(ps, SelfID, a.Value, true) {
			return map[string]any{"ok": false, "error": "action_disabled", "message": "This poll is closed."}, true
		}
		return okMap(), true
	case ps != nil && a.ActionID == PollCloseAction:
		w.change(ps.ch, ps.ts, func(m *slack.Message) {
			ps.closed, ps.closedBy = true, SelfID
			m.Blocks = rawJSON(w.renderPollLocked(ps))
		})
		w.goBusy(func() { w.bots.pollClosed(ps) })
		return okMap(), true
	case ps != nil && a.ActionID == StandupAction && a.Selected != nil:
		v := a.Selected.Value
		w.vote(ps, SelfID, v, false)
		w.goBusy(func() {
			st := w.scripts.Standup
			line := orStr(st.Thanks[v], st.Thanks[""])
			if line != "" && w.sleep(w.randDur(800*time.Millisecond, 2*time.Second)) {
				w.post(ps.ch, slack.Message{User: PulseID, Text: w.bots.expand(line, ""), ThreadTS: ps.ts}, false)
			}
		})
		return okMap(), true
	case a.ActionID == "inkwell_regenerate":
		w.bots.mu.Lock()
		prompt, ok := w.bots.prompts[cont.Channel+"/"+cont.TS]
		w.bots.mu.Unlock()
		if ok {
			m, _ := w.Message(cont.Channel, cont.TS)
			w.RunAgent(cont.Channel, m.ThreadTS, prompt)
		}
		return okMap(), true
	case a.ActionID == "inkwell_rerun": // the summary view's Re-run checks
		w.goBusy(func() { w.bots.runCI("success") })
		return okMap(), true
	case a.ActionID == "inkwell_feedback":
		if strings.HasPrefix(a.Value, "bad") {
			m, _ := w.Message(cont.Channel, cont.TS)
			w.goBusy(func() {
				if w.sleep(w.randDur(time.Second, 2*time.Second)) {
					w.post(cont.Channel, slack.Message{User: InkwellID, ThreadTS: m.ThreadTS,
						Text: "Thanks for the feedback. I'll double-check my work (and my rounding)."}, false)
				}
			})
		}
		return okMap(), true
	}
	return nil, false
}

// pollClosed: someone who voted for the winner celebrates.
func (e *engine) pollClosed(ps *pollState) {
	w := e.w
	w.mu.Lock()
	counts := map[string]int{}
	for _, v := range ps.votes {
		counts[v]++
	}
	best, n := "", 0
	for _, o := range ps.poll.Options {
		if counts[o.Value] > n {
			best, n = o.Value, counts[o.Value]
		}
	}
	var fans []string
	for _, u := range ps.order {
		if ps.votes[u] == best && u != SelfID {
			fans = append(fans, u)
		}
	}
	w.mu.Unlock()
	if len(fans) == 0 || !w.sleep(w.randDur(2*time.Second, 5*time.Second)) {
		return
	}
	w.react(fans[w.pick(len(fans))], ps.ch, ps.ts, "tada", true)
}

// pollLoop: the first poll after a minute or two, the stand-up next, then
// a poll every 6–10 minutes.
func (e *engine) pollLoop() {
	w := e.w
	if !w.sleep(w.randDur(60*time.Second, 100*time.Second)) {
		return
	}
	w.PostPoll(LunchID)
	if !w.sleep(w.randDur(3*time.Minute, 5*time.Minute)) {
		return
	}
	w.PostStandup(GeneralID)
	for {
		if !w.sleep(w.randDur(6*time.Minute, 10*time.Minute)) {
			return
		}
		w.PostPoll(LunchID)
	}
}
