package demo

import (
	"fmt"
	"regexp"
	"strings"
	"time"

	"github.com/some-du6e/yoloslackgoclient/internal/slack"
)

// Inkwell, the AI coding agent. Its replies stream the way Slack's AI
// apps do (fakeslack.AgentStream's sequence): ai_assistant_status
// "is thinking…", a message with a plan block whose tasks go pending →
// in_progress → complete through message_changed, the answer's text
// appended in chunks, then "Thinking completed" with the feedback footer
// and suggested prompts. Every few minutes it also moves PR #217 along in
// its code channel: a task card, commits, CI going pending then green,
// the channel's code_channel properties updated with channel_updated.

// RunAgent has Inkwell answer prompt now in channel (threadTS "": top
// level), streaming.
func (w *World) RunAgent(channel, threadTS, prompt string) {
	w.bots.runAgent(channel, threadTS, prompt)
}

func (e *engine) runAgent(ch, thread, prompt string) {
	e.w.goBusy(func() { e.stream(ch, thread, prompt) })
}

func (e *engine) answerFor(prompt string) AgentAnswer {
	low := " " + normText(prompt) + " "
	var fallback *AgentAnswer
	for i := range e.s.Agent.Answers {
		a := &e.s.Agent.Answers[i]
		if len(a.Patterns) == 0 {
			if fallback == nil {
				fallback = a
			}
			continue
		}
		for _, p := range a.Patterns {
			if patternHit(low, p) {
				return *a
			}
		}
	}
	if fallback != nil {
		return *fallback
	}
	return AgentAnswer{Tasks: []AgentTask{{Title: "Read the request"}}, Text: "On it."}
}

func agentTaskBlock(i int, t AgentTask, status string) map[string]any {
	m := map[string]any{"task_id": fmt.Sprintf("t%d", i+1), "title": t.Title, "status": status}
	if status == "complete" || status == "in_progress" {
		if t.Detail != "" {
			m["details"] = liveRich("", t.Detail)
		}
	}
	if status == "complete" && t.Output != "" {
		m["output"] = liveRich("", t.Output)
	}
	return m
}

func (e *engine) stream(ch, thread, prompt string) {
	w := e.w
	ans := e.answerFor(prompt)
	start := time.Now()
	status := func(s string) {
		w.fs.Push(map[string]any{"type": "ai_assistant_status", "channel_id": ch, "thread_ts": thread, "bot_user_id": InkwellID,
			"status": s, "status_type": "typing", "loading_messages": []string{"Reading the diff…", "Checking the tests…", "Counting pennies…"}})
	}
	status("is thinking…")
	if !w.sleep(w.randDur(1200*time.Millisecond, 2500*time.Millisecond)) {
		status("")
		return
	}
	plan := func(done int, title string) map[string]any {
		var tasks []any
		for i, t := range ans.Tasks {
			st := "pending"
			switch {
			case i < done:
				st = "complete"
			case i == done:
				st = "in_progress"
			}
			tasks = append(tasks, agentTaskBlock(i, t, st))
		}
		return map[string]any{"type": "plan", "block_id": "sp", "title": title, "tasks": tasks}
	}
	m := slack.Message{User: InkwellID, Text: "Thinking…", ThreadTS: thread, StreamingState: "in_progress",
		Blocks: rawJSON([]any{plan(0, "Thinking…")})}
	ts, _ := w.post(ch, m, false)
	e.mu.Lock()
	e.prompts[ch+"/"+ts] = prompt
	e.mu.Unlock()
	set := func(blocks []any, text, state string) {
		w.change(ch, ts, func(m *slack.Message) {
			m.Blocks, m.Text, m.StreamingState = rawJSON(blocks), text, state
		})
	}
	for i := 1; i < len(ans.Tasks); i++ {
		if !w.sleep(w.randDur(900*time.Millisecond, 2*time.Second)) {
			status("")
			return
		}
		set([]any{plan(i, "Thinking…")}, "Thinking…", "in_progress")
	}
	// the answer streams in, the last task still running
	last := max(len(ans.Tasks)-1, 0)
	chunks := streamChunks(ans.Text, 4)
	for i := range chunks {
		if !w.sleep(w.randDur(400*time.Millisecond, 900*time.Millisecond)) {
			status("")
			return
		}
		sofar := strings.Join(chunks[:i+1], "")
		set([]any{plan(last, "Thinking…"), liveRich("st", sofar)}, sofar, "in_progress")
	}
	if !w.sleep(w.randDur(300*time.Millisecond, 700*time.Millisecond)) {
		status("")
		return
	}
	took := float64(time.Since(start).Round(100*time.Millisecond)) / float64(time.Second) * w.opts.Speed
	final := append([]any{plan(len(ans.Tasks), "Thinking completed"), liveRich("st", ans.Text)}, agentFeedback(took)...)
	set(final, ans.Text, "completed")
	status("")
	if len(e.s.Agent.Prompts) > 0 {
		var ps []any
		for _, p := range e.s.Agent.Prompts {
			ps = append(ps, map[string]any{"title": p.Title, "message": p.Message})
		}
		w.fs.Push(map[string]any{"type": "ai_assistant_threads_suggested_prompts", "channel_id": ch, "thread_ts": thread,
			"bot_user_id": InkwellID, "title": "Try asking", "prompts": ps})
	}
}

// streamChunks splits text into about n pieces on word boundaries
// (joined they are text again).
func streamChunks(text string, n int) []string {
	words := strings.SplitAfter(text, " ")
	per := max(1, (len(words)+n-1)/n)
	var out []string
	for i := 0; i < len(words); i += per {
		out = append(out, strings.Join(words[i:min(len(words), i+per)], ""))
	}
	if len(out) == 0 {
		out = []string{text}
	}
	return out
}

// agentFeedback is the reply footer Slack's AI apps send: a context line
// and context_actions with feedback buttons and Regenerate.
func agentFeedback(took float64) []any {
	return []any{
		map[string]any{"type": "context", "block_id": "ctx1", "elements": []any{map[string]any{"type": "mrkdwn",
			"text": fmt.Sprintf("_done in %.1fs · Inkwell can make mistakes, double-check the pennies_", took)}}},
		map[string]any{"type": "context_actions", "block_id": "fb1", "elements": []any{
			map[string]any{"type": "feedback_buttons", "action_id": "inkwell_feedback",
				"positive_button": map[string]any{"text": map[string]any{"type": "plain_text", "text": "Good response", "emoji": true},
					"accessibility_label": "Mark this response as good", "value": "good-feedback"},
				"negative_button": map[string]any{"text": map[string]any{"type": "plain_text", "text": "Bad response", "emoji": true},
					"accessibility_label": "Mark this response as bad", "value": "bad-feedback"}},
			map[string]any{"type": "icon_button", "action_id": "inkwell_regenerate", "icon": "refresh", "value": "regenerate",
				"text": map[string]any{"type": "plain_text", "text": "Regenerate"}},
		}},
	}
}

var mrkdwnInlineRe = regexp.MustCompile("\\*[^*\\n]+\\*|`[^`\\n]+`|_[^_\\n]+_")

// liveRich turns a little mrkdwn (*bold*, `code`, _italic_, "• " bullet
// lines) into a rich_text block, as an app's streamed text arrives.
func liveRich(blockID, text string) map[string]any {
	inline := func(s string) []any {
		var els []any
		last := 0
		for _, loc := range mrkdwnInlineRe.FindAllStringIndex(s, -1) {
			if loc[0] > last {
				els = append(els, map[string]any{"type": "text", "text": s[last:loc[0]]})
			}
			tok := s[loc[0]:loc[1]]
			style := map[string]bool{map[byte]string{'*': "bold", '`': "code", '_': "italic"}[tok[0]]: true}
			els = append(els, map[string]any{"type": "text", "text": tok[1 : len(tok)-1], "style": style})
			last = loc[1]
		}
		if last < len(s) {
			els = append(els, map[string]any{"type": "text", "text": s[last:]})
		}
		return els
	}
	var out []any
	var para []string
	var bullets []any
	flushPara := func() {
		if len(para) > 0 {
			out = append(out, map[string]any{"type": "rich_text_section", "elements": inline(strings.Join(para, "\n"))})
			para = nil
		}
	}
	flushList := func() {
		if len(bullets) > 0 {
			out = append(out, map[string]any{"type": "rich_text_list", "style": "bullet", "indent": 0, "elements": bullets})
			bullets = nil
		}
	}
	for _, ln := range strings.Split(text, "\n") {
		if b, ok := strings.CutPrefix(ln, "• "); ok {
			flushPara()
			bullets = append(bullets, map[string]any{"type": "rich_text_section", "elements": inline(b)})
			continue
		}
		flushList()
		para = append(para, ln)
	}
	flushPara()
	flushList()
	if len(out) == 0 {
		out = []any{map[string]any{"type": "rich_text_section", "elements": []any{map[string]any{"type": "text", "text": text}}}}
	}
	m := map[string]any{"type": "rich_text", "elements": out}
	if blockID != "" {
		m["block_id"] = blockID
	}
	return m
}

// agentLoop posts Inkwell's next PR update every 3–5 minutes.
func (e *engine) agentLoop() {
	w := e.w
	if !w.sleep(w.randDur(90*time.Second, 150*time.Second)) {
		return
	}
	for {
		w.busy.Add(1)
		e.progressStep()
		w.busy.Add(-1)
		if !w.sleep(w.randDur(3*time.Minute, 5*time.Minute)) {
			return
		}
	}
}

// AgentProgress posts Inkwell's next code channel update now (the smoke
// test; the agent loop does it every few minutes).
func (w *World) AgentProgress() { w.goBusy(w.bots.progressStep) }

func (e *engine) progressStep() {
	w := e.w
	ps := e.s.Agent.Progress
	if len(ps) == 0 {
		return
	}
	e.mu.Lock()
	i := e.progress
	e.progress++
	e.mu.Unlock()
	if i >= len(ps) { // the story's over: loop over the last few updates
		tail := min(3, len(ps))
		i = len(ps) - tail + (i-len(ps))%tail
	}
	p := ps[i]
	expire := w.now().Add(time.Hour).Unix()
	w.setCodeProps(map[string]any{"agent_session": map[string]any{"status": "processing", "date_status_processing_expire": expire}})
	card := func(status string) []any {
		c := map[string]any{"type": "task_card", "block_id": "card", "task_id": fmt.Sprintf("k%d", i+1), "title": p.Card, "status": status}
		if p.Detail != "" {
			c["details"] = liveRich("", p.Detail)
		}
		if status == "complete" && p.Output != "" {
			c["output"] = liveRich("", p.Output)
		}
		return []any{c}
	}
	w.typing(InkwellID, CodeID, "")
	ts, _ := w.post(CodeID, slack.Message{User: InkwellID, Text: p.Card, Blocks: rawJSON(card("in_progress"))}, false)
	if !w.sleep(w.randDur(4*time.Second, 9*time.Second)) {
		return
	}
	w.change(CodeID, ts, func(m *slack.Message) { m.Blocks = rawJSON(card("complete")) })
	if p.Text != "" {
		if !w.sleep(w.randDur(1500*time.Millisecond, 3*time.Second)) {
			return
		}
		w.post(CodeID, slack.Message{User: InkwellID, Text: p.Text, Blocks: rawJSON([]any{liveRich("", p.Text)})}, false)
	}
	if p.CI != "" {
		e.bumpDiff()
		e.runCI(p.CI)
	}
	// Inkwell keeps working until the story's last update
	if i >= len(ps)-1 {
		w.setCodeProps(map[string]any{"agent_session": map[string]any{"status": "active"}})
	}
	if p.Who != "" && p.Reply != "" {
		if who, ok := w.person(p.Who); ok {
			text := e.expand(p.Reply, "")
			if e.typeThenWait(who.ID, CodeID, ts, text, w.randDur(4*time.Second, 10*time.Second)) {
				w.post(CodeID, slack.Message{User: who.ID, Text: text, ThreadTS: ts}, false)
			}
		}
	}
}

// bumpDiff moves diff_file_version on (the Code view refetches the diff).
func (e *engine) bumpDiff() {
	e.mu.Lock()
	n := 3 + e.progress
	e.mu.Unlock()
	e.w.setCodeProps(map[string]any{"code_channel": map[string]any{"diff_file_version": n}})
}

// runCI runs the checks: ci_state pending, then result, with Inkwell's
// one-line report.
func (e *engine) runCI(result string) {
	w := e.w
	w.setCodeProps(map[string]any{"code_channel": map[string]any{"ci_state": "pending"}})
	if !w.sleep(w.randDur(8*time.Second, 15*time.Second)) {
		return
	}
	w.setCodeProps(map[string]any{"code_channel": map[string]any{"ci_state": result}})
	text := ":white_check_mark: CI passed on `" + DemoBranch + "`: 38 checks, 0 failures."
	if result != "success" {
		text = ":x: CI failed on `" + DemoBranch + "`. Looking into it now."
	}
	w.post(CodeID, slack.Message{User: InkwellID, Text: text, Blocks: rawJSON([]any{liveRich("", text)})}, false)
}
