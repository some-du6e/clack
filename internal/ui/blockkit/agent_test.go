package blockkit

// Agent blocks (agent.go): parsing and Slack's status rules, the
// blocks.actions payloads, slot sizes for every state (collapsed, open,
// streaming), a real click through Gio's router to fakeslack's
// blocks.actions, and light/dark screenshots (BK_AGENT_SHOTS_DIR, the
// bk-agent-shots workflow). Synthetic fixtures only (fakeslack/agents.go).

import (
	"context"
	"encoding/json"
	"image"
	"image/color"
	"image/png"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"gioui.org/f32"
	"gioui.org/io/input"
	"gioui.org/io/pointer"
	"gioui.org/layout"
	"gioui.org/op"
	"gioui.org/op/paint"
	"gioui.org/unit"

	"github.com/some-du6e/clack/internal/core"
	"github.com/some-du6e/clack/internal/fakeslack"
	"github.com/some-du6e/clack/internal/imgcache"
	"github.com/some-du6e/clack/internal/richtext"
	"github.com/some-du6e/clack/internal/slack"
	"github.com/some-du6e/clack/internal/store"
	"github.com/some-du6e/clack/internal/ui/popups"
	"github.com/some-du6e/clack/internal/ui/richview"
	"github.com/some-du6e/clack/internal/ui/theme"
	"github.com/some-du6e/clack/internal/ui/theme/pixeltest"
)

type agentHost struct {
	app *core.App
	th  *theme.Theme
}

func (h *agentHost) Core() *core.App                           { return h.app }
func (h *agentHost) Theme() *theme.Theme                       { return h.th }
func (h *agentHost) Images() *imgcache.Cache                   { return nil }
func (h *agentHost) Invalidate()                               {}
func (h *agentHost) Navigate(string)                           {}
func (h *agentHost) OpenThread(string, string)                 {}
func (h *agentHost) CloseSecondary()                           {}
func (h *agentHost) ShowOverlay(popups.Overlay)                {}
func (h *agentHost) OpenEmojiPicker(image.Point, func(string)) {}
func (h *agentHost) OpenProfile(image.Point, string)           {}
func (h *agentHost) OpenMessageMenu(image.Point, core.Message) {}
func (h *agentHost) OpenLightbox(richtext.Image, core.Message) {}
func (h *agentHost) Toast(string)                              {}

var agentTS = 0

// agentMsg is fixture name as a core message with a fresh ts.
func agentMsg(t *testing.T, name string) core.Message {
	t.Helper()
	fx, ok := fakeslack.AgentFixtures()[name]
	if !ok {
		t.Fatalf("no fixture %q", name)
	}
	return agentMsgOf(t, fx, "completed")
}

func agentMsgOf(t *testing.T, blocks []any, streaming string) core.Message {
	t.Helper()
	agentTS++
	raw, _ := json.Marshal(blocks)
	ts := "1762520000." + strings.Repeat("0", 6-len(itoa(agentTS))) + itoa(agentTS)
	sm := slack.Message{TS: ts, User: fakeslack.AgentUserID, BotID: fakeslack.AgentBotID, Blocks: raw, ThreadTS: fakeslack.AgentThreadTS,
		StreamingState: streaming, BotProfile: &slack.BotProfile{ID: fakeslack.AgentBotID, AppID: fakeslack.AgentAppID, UserID: fakeslack.AgentUserID}}
	return core.Message{TS: ts, Channel: fakeslack.AgentChannelID, User: sm.User, BotID: sm.BotID, ThreadTS: sm.ThreadTS,
		Blocks: raw, StreamingState: streaming, Doc: richtext.FromMessage(nilResolver{}, &sm),
		BotProfile: &core.BotProfile{ID: fakeslack.AgentBotID, AppID: fakeslack.AgentAppID, UserID: fakeslack.AgentUserID}}
}

func TestAgentParseAndStatus(t *testing.T) {
	fx := fakeslack.AgentFixtures()
	cases := []struct {
		name, status, title string
	}{
		{"plan-complete", StatusComplete, "Thinking completed"},
		{"plan-in-progress", StatusInProgress, "Thinking…"},
		{"plan-error", StatusError, "Couldn't finish"},
		{"plan-untitled", StatusInProgress, "Summarise what I found"},
		{"task-card-pending", StatusComplete, "Queue the build"}, // Slack: neither in progress nor errored shows complete
		{"task-card-in-progress", StatusInProgress, "Running the tests"},
		{"task-card-complete", StatusComplete, "Searched the web"},
		{"task-card-error", StatusError, "Deploy failed"},
	}
	for _, c := range cases {
		raw, _ := json.Marshal(fx[c.name][0])
		b, ok := ParseAgentBlock(raw)
		if !ok {
			t.Fatalf("%s: not parsed", c.name)
		}
		var st, title string
		if b.Type == "plan" {
			st, title = PlanStatus(b.Tasks), PlanTitle(b)
		} else {
			st, title = CardStatus(b.Status), b.Title
		}
		if st != c.status || title != c.title {
			t.Errorf("%s: status %q title %q, want %q %q", c.name, st, title, c.status, c.title)
		}
	}
	raw, _ := json.Marshal(fx["task-card-complete"][0])
	b, _ := ParseAgentBlock(raw)
	if !b.hasContent() || len(b.Sources) != 2 || b.Sources[0].URL != "https://example.com/docs/plan" {
		t.Errorf("task card content: %+v", b.AgentTask)
	}
}

func TestAgentPayloads(t *testing.T) {
	raw, _ := json.Marshal(fakeslack.AgentFeedback()[1])
	b, ok := ParseAgentBlock(raw)
	if !ok || len(b.Elements) != 2 {
		t.Fatalf("context_actions: %+v", b)
	}
	got := string(FeedbackPayload("fb1", &b.Elements[0], true))
	want := `{"action_id":"feedback","block_id":"fb1","text":{"type":"plain_text","text":"Good response","emoji":true},"type":"feedback_buttons","value":"good-feedback"}`
	if got != want {
		t.Errorf("positive:\n got %s\nwant %s", got, want)
	}
	got = string(FeedbackPayload("fb1", &b.Elements[0], false))
	if !strings.Contains(got, `"value":"bad-feedback"`) || !strings.Contains(got, `"text":"Bad response"`) {
		t.Errorf("negative: %s", got)
	}
	got = string(IconButtonPayload("fb1", &b.Elements[1]))
	want = `{"action_id":"regenerate","block_id":"fb1","icon":"refresh","text":{"type":"plain_text","text":"Regenerate"},"type":"icon_button","value":"regen-1"}`
	if got != want {
		t.Errorf("icon_button:\n got %s\nwant %s", got, want)
	}
}

// slotSize is the agent slot's size for the message's first agent block.
func slotSize(t *testing.T, me *Message, m *core.Message, w int) image.Point {
	t.Helper()
	for _, b := range m.Doc.Blocks {
		if b.Kind == richtext.Agent {
			sz, _ := me.ElementsSize(b.Buttons, w, false, unit.Metric{PxPerDp: 1, PxPerSp: 1})
			return sz
		}
	}
	t.Fatal("no agent block")
	return image.Point{}
}

func agentButton(m *core.Message, i int) richtext.Button {
	n := 0
	for _, b := range m.Doc.Blocks {
		if b.Kind == richtext.Agent {
			if n == i {
				return b.Buttons[0]
			}
			n++
		}
	}
	return richtext.Button{}
}

func TestAgentSlotSizes(t *testing.T) {
	h := &agentHost{th: theme.New(false)}
	m := agentMsg(t, "plan-complete")
	me := MessageElements(h, &m)
	if me == nil {
		t.Fatal("plan message has no element host")
	}
	closed := slotSize(t, me, &m, 480)
	if closed.Y < 20 || closed.Y > 30 {
		t.Errorf("collapsed plan: %v (one 19.5px line + margin)", closed)
	}
	st := me.agentState()
	st.open["plan\x00p1"] = true
	open := slotSize(t, me, &m, 480)
	if open.Y < closed.Y+50 {
		t.Errorf("open plan %v not taller than collapsed %v by two tasks", open, closed)
	}
	// a plan task collapses on its own (tasks start open)
	st.open["task\x00p1\x00t1\x000"] = false
	if fold := slotSize(t, me, &m, 480); fold.Y >= open.Y {
		t.Errorf("collapsing a task: %v, open %v", fold, open)
	}

	// task cards start collapsed, MCP ones open
	c := agentMsg(t, "task-card-complete")
	cme := MessageElements(h, &c)
	if sz := slotSize(t, cme, &c, 480); sz.Y > 30 {
		t.Errorf("task card should start collapsed: %v", sz)
	}
	mc := agentMsg(t, "task-card-mcp")
	mme := MessageElements(h, &mc)
	if sz := slotSize(t, mme, &mc, 480); sz.Y < 40 {
		t.Errorf("mcp task card should start open: %v", sz)
	}

	// the reply's feedback row: two 32px buttons and the icon button
	r := agentMsg(t, "reply-feedback")
	rme := MessageElements(h, &r)
	bt := agentButton(&r, 0)
	if bt.Kind != "context_actions" {
		t.Fatalf("reply agent block: %+v", bt)
	}
	sz, _ := rme.ElementsSize([]richtext.Button{bt}, 480, false, unit.Metric{PxPerDp: 1, PxPerSp: 1})
	if sz != image.Pt(96, 36) {
		t.Errorf("context_actions %v, want 96×36", sz)
	}
}

// The streamed reply's slot follows the stream: the in-progress plan,
// then the settled one (collapsed again: the user hadn't opened it).
func TestAgentStreamingStates(t *testing.T) {
	h := &agentHost{th: theme.New(true)}
	evs := fakeslack.AgentStream("1762520099.000100")
	var msgs []map[string]any
	for _, ev := range evs {
		if ev["type"] == "message" {
			if mm, ok := ev["message"].(map[string]any); ok {
				msgs = append(msgs, mm)
			} else {
				msgs = append(msgs, ev)
			}
		}
	}
	if len(msgs) != 4 {
		t.Fatalf("stream messages %d", len(msgs))
	}
	var states []string
	for _, mm := range msgs {
		bs, _ := json.Marshal(mm["blocks"])
		var blocks []any
		_ = json.Unmarshal(bs, &blocks)
		m := agentMsgOf(t, blocks, mm["streaming_state"].(string))
		raw, _ := json.Marshal(blocks[0])
		b, _ := ParseAgentBlock(raw)
		states = append(states, m.StreamingState+"/"+PlanStatus(b.Tasks))
		if MessageElements(h, &m) == nil {
			t.Fatal("no host")
		}
	}
	want := "in_progress/in_progress in_progress/in_progress in_progress/in_progress completed/complete"
	if got := strings.Join(states, " "); got != want {
		t.Errorf("states %q, want %q", got, want)
	}
}

// ---- rendering ----

type agentShot struct {
	h    *agentHost
	rows []*agentRow
	r    input.Router
	st   *richview.Style
}

type agentRow struct {
	m  core.Message
	v  *richview.View
	me *Message
	at image.Point
}

func newAgentShot(h *agentHost, msgs ...core.Message) *agentShot {
	s := &agentShot{h: h, st: styleFor(h.th, nil)}
	for i := range msgs {
		m := msgs[i]
		me := MessageElements(h, &m)
		s.rows = append(s.rows, &agentRow{m: m, me: me, v: richview.New(m.Doc, richview.Options{Elements: me})})
	}
	return s
}

// frame lays the rows out down a 480px column, Slack's message gutter.
func (s *agentShot) frame(w, hgt int, now time.Time) *op.Ops {
	ops := new(op.Ops)
	gtx := layout.Context{Ops: ops, Metric: unit.Metric{PxPerDp: 1, PxPerSp: 1}, Now: now,
		Constraints: layout.Exact(image.Pt(w, hgt)), Source: s.r.Source()}
	bg := color.NRGBA{R: 255, G: 255, B: 255, A: 255}
	if s.h.th.Dark {
		bg = color.NRGBA{R: 0x1a, G: 0x1d, B: 0x21, A: 255}
	}
	paint.Fill(ops, bg)
	y := 12
	hand := &surfHandler{h: s.h}
	for _, r := range s.rows {
		r.at = image.Pt(20, y)
		off := op.Offset(r.at).Push(ops)
		g := gtx
		g.Constraints = layout.Constraints{Max: image.Pt(480, hgt)}
		d := r.v.Layout(g, s.st, hand)
		off.Pop()
		y += d.Size.Y + 16
	}
	s.r.Frame(ops)
	return ops
}

func (s *agentShot) click(p image.Point) {
	pt := f32.Pt(float32(p.X), float32(p.Y))
	s.r.Queue(pointer.Event{Kind: pointer.Move, Source: pointer.Mouse, Position: pt},
		pointer.Event{Kind: pointer.Press, Source: pointer.Mouse, Buttons: pointer.ButtonPrimary, Position: pt},
		pointer.Event{Kind: pointer.Release, Source: pointer.Mouse, Position: pt})
}

// TestAgentFeedbackClick clicks 👍 through Gio's router: the button
// fills and fakeslack gets Slack's blocks.actions.
func TestAgentFeedbackClick(t *testing.T) {
	fs, err := fakeslack.Start(fakeslack.Small)
	if err != nil {
		t.Fatal(err)
	}
	defer fs.Close()
	app := core.New(slack.New(fs.Creds()), store.New(t.TempDir()))
	defer app.Shutdown()
	if err := app.Boot(context.Background()); err != nil {
		t.Fatal(err)
	}
	h := &agentHost{app: app, th: theme.New(false)}
	m := agentMsg(t, "reply-feedback")
	s := newAgentShot(h, m)
	now := time.Now()
	s.frame(600, 300, now)
	// the context_actions slot is the row's last 36px; 👍 is its first 32px
	row := s.rows[0]
	var slot image.Rectangle
	for _, b := range m.Doc.Blocks {
		if b.Kind == richtext.Agent {
			sz, _ := row.me.ElementsSize(b.Buttons, 480, false, unit.Metric{PxPerDp: 1, PxPerSp: 1})
			slot.Max = sz
		}
	}
	d := row.v.Layout(layout.Context{Ops: new(op.Ops), Metric: unit.Metric{PxPerDp: 1, PxPerSp: 1}, Constraints: layout.Constraints{Max: image.Pt(480, 300)}, Now: now}, s.st, &surfHandler{h: h})
	thumb := row.at.Add(image.Pt(16, d.Size.Y-slot.Max.Y+4+16))
	s.click(thumb)
	for i := 0; i < 3; i++ {
		s.frame(600, 300, now.Add(time.Duration(i+1)*16*time.Millisecond))
	}
	st := row.me.agentState()
	if got := st.rating["fb1\x00feedback\x000"]; got != "positive" {
		t.Fatalf("rating after click = %q (clicked %v, row %v, doc %v)", got, thumb, row.at, d.Size)
	}
	deadline := time.Now().Add(5 * time.Second)
	for len(fs.BlockActions()) == 0 && time.Now().Before(deadline) {
		time.Sleep(20 * time.Millisecond)
		s.frame(600, 300, time.Now())
	}
	calls := fs.BlockActions()
	if len(calls) != 1 {
		t.Fatalf("blocks.actions calls: %d", len(calls))
	}
	var acts []map[string]any
	if err := json.Unmarshal([]byte(calls[0].Get("actions")), &acts); err != nil || len(acts) != 1 {
		t.Fatalf("actions %q: %v", calls[0].Get("actions"), err)
	}
	a := acts[0]
	if a["type"] != "feedback_buttons" || a["action_id"] != "feedback" || a["block_id"] != "fb1" || a["value"] != "good-feedback" {
		t.Errorf("action %v", a)
	}
	if calls[0].Get("service_id") != fakeslack.AgentBotID {
		t.Errorf("service_id %q", calls[0].Get("service_id"))
	}
	var ctr map[string]any
	_ = json.Unmarshal([]byte(calls[0].Get("container")), &ctr)
	if ctr["type"] != "message" || ctr["channel_id"] != fakeslack.AgentChannelID || ctr["message_ts"] != m.TS {
		t.Errorf("container %v", ctr)
	}
}

// TestAgentShots renders every fixture (collapsed and open), a frame of
// the stream and the feedback footer, light and dark. On a GPU it checks
// the toggle line has ink; BK_AGENT_SHOTS_DIR keeps the PNGs.
func TestAgentShots(t *testing.T) {
	for _, dark := range []bool{false, true} {
		h := &agentHost{th: theme.New(dark)}
		names := []string{"plan-complete", "plan-complete-open", "plan-in-progress", "plan-error", "plan-untitled",
			"task-card-pending", "task-card-in-progress", "task-card-complete", "task-card-error", "task-card-mcp", "reply-feedback"}
		var msgs []core.Message
		for _, n := range names {
			msgs = append(msgs, agentMsg(t, n))
		}
		s := newAgentShot(h, msgs...)
		// open the "-open" plan, the in-progress plan and the complete card
		for i, n := range names {
			me := s.rows[i].me
			switch n {
			case "plan-complete-open", "plan-in-progress", "plan-error":
				b := agentButton(&s.rows[i].m, 0)
				me.agentState().open[b.Kind+"\x00"+b.BlockID] = true
			case "task-card-complete", "task-card-error", "task-card-in-progress":
				b := agentButton(&s.rows[i].m, 0)
				me.agentState().open[b.Kind+"\x00"+b.BlockID] = true
			}
		}
		const w, hgt = 560, 1400
		now := time.Date(2026, 11, 8, 12, 0, 0, 0, time.UTC)
		var ops *op.Ops
		for i := 0; i < 3; i++ {
			ops = s.frame(w, hgt, now.Add(time.Duration(i)*300*time.Millisecond))
		}
		img := pixeltest.Render(t, w, hgt, ops)
		pal := agentColors(h.th)
		bg := color.NRGBA{R: 255, G: 255, B: 255, A: 255}
		if dark {
			bg = color.NRGBA{R: 0x1a, G: 0x1d, B: 0x21, A: 255}
		}
		line := image.Rect(20+20, s.rows[0].at.Y, 20+200, s.rows[0].at.Y+20)
		if n := pixeltest.Ink(img, line, pal.ter, bg); n < 40 {
			t.Errorf("dark=%v: the plan's toggle line has %d px of text", dark, n)
		}
		if dir := os.Getenv("BK_AGENT_SHOTS_DIR"); dir != "" {
			name := "agent-blocks-light.png"
			if dark {
				name = "agent-blocks-dark.png"
			}
			f, err := os.Create(filepath.Join(dir, name))
			if err != nil {
				t.Fatal(err)
			}
			_ = png.Encode(f, img)
			_ = f.Close()
		}
	}
}

// TestAgentStreamShots renders the stream frame by frame (streaming
// plan with spinners, text growing, the settled reply with feedback).
func TestAgentStreamShots(t *testing.T) {
	dir := os.Getenv("BK_AGENT_SHOTS_DIR")
	for _, dark := range []bool{false, true} {
		h := &agentHost{th: theme.New(dark)}
		evs := fakeslack.AgentStream("1762520099.000100")
		k := 0
		for _, ev := range evs {
			if ev["type"] != "message" {
				continue
			}
			mm, ok := ev["message"].(map[string]any)
			if !ok {
				mm = ev
			}
			bs, _ := json.Marshal(mm["blocks"])
			var blocks []any
			_ = json.Unmarshal(bs, &blocks)
			m := agentMsgOf(t, blocks, mm["streaming_state"].(string))
			s := newAgentShot(h, m)
			b := agentButton(&m, 0)
			s.rows[0].me.agentState().open[b.Kind+"\x00"+b.BlockID] = true
			const w, hgt = 560, 260
			now := time.Date(2026, 11, 8, 12, 0, 0, int(k)*150e6, time.UTC)
			var ops *op.Ops
			for i := 0; i < 2; i++ {
				ops = s.frame(w, hgt, now)
			}
			img := pixeltest.Render(t, w, hgt, ops)
			if dir != "" {
				mode := "light"
				if dark {
					mode = "dark"
				}
				f, err := os.Create(filepath.Join(dir, "agent-stream-"+itoa(k)+"-"+mode+".png"))
				if err != nil {
					t.Fatal(err)
				}
				_ = png.Encode(f, img)
				_ = f.Close()
			}
			k++
		}
	}
}

// The icon paths parse into closed subpaths and the evenodd rewinding
// leaves a nested hole opposite its outline.
func TestAgentIconPaths(t *testing.T) {
	for name, d := range agentIconPaths {
		subs := parseSVG(d)
		if len(subs) == 0 {
			t.Errorf("%s: no subpaths", name)
		}
		for _, s := range subs {
			if len(s.segs) == 0 {
				t.Errorf("%s: empty subpath", name)
			}
		}
	}
	subs := evenOdd(parseSVG(agentIconPaths["check-circle-filled"]))
	if len(subs) != 2 {
		t.Fatalf("check-circle-filled: %d subpaths", len(subs))
	}
	a0, a1 := area(flatten(subs[0])), area(flatten(subs[1]))
	if (a0 > 0) == (a1 > 0) {
		t.Errorf("circle and check wind the same way (%v, %v): the check would not cut out", a0, a1)
	}
}

// plan_block_coding_enhancements (FiNg.XI): a plan task with hide_title
// has no title line under the experiment (its content stays, with the
// no-title spacing); off, or not assigned, it keeps its title.
func TestAgentPlanHideTitle(t *testing.T) {
	app := core.New(slack.New(slack.Creds{Token: "xoxc-test", UserID: "U1", Team: "T1"}), store.New(""))
	h := &agentHost{app: app, th: theme.New(false)}
	rich := func(s string) map[string]any {
		return map[string]any{"type": "rich_text", "elements": []any{map[string]any{"type": "rich_text_section",
			"elements": []any{map[string]any{"type": "text", "text": s}}}}}
	}
	plan := []any{map[string]any{"type": "plan", "block_id": "ph", "title": "Coding", "tasks": []any{
		map[string]any{"task_id": "t1", "title": "Edit redirect.ts", "status": "complete", "hide_title": true, "details": rich("Changed two lines.")},
		map[string]any{"task_id": "t2", "title": "Run the tests", "status": "complete"},
	}}}
	app.SetExperimentGroup("agent_streaming_slimmer_task_cards", "on") // hide_title is a slim plan task's
	size := func(group string) int {
		app.SetExperimentGroup("plan_block_coding_enhancements", group)
		m := agentMsgOf(t, plan, "completed")
		me := MessageElements(h, &m)
		me.agentState().open["plan\x00ph"] = true
		return slotSize(t, me, &m, 480).Y
	}
	on, off, unassigned := size("on"), size("off"), size("")
	if off != unassigned {
		t.Errorf("off %d and unassigned %d should draw alike", off, unassigned)
	}
	// the title line (4+2+19.5+2+4) gives way to the no-title spacing (2+4 above, 2 below)
	if d := off - on; d < 15 || d > 30 {
		t.Errorf("hide_title saved %dpx (on %d, off %d), want the title line less the no-title spacing", d, on, off)
	}
	// without hide_title the experiment changes nothing
	plan[0].(map[string]any)["tasks"].([]any)[0].(map[string]any)["hide_title"] = false
	if a, b := size("on"), size("off"); a != b {
		t.Errorf("no hide_title: on %d, off %d", a, b)
	}
}

// cardHost is an agentHost whose core has agent_streaming_slimmer_task_cards
// in group: "on" draws the slim blocks, anything else Slack's cards.
func cardHost(dark bool, group string) *agentHost {
	app := core.New(slack.New(slack.Creds{Token: "xoxc-test", UserID: "U1", Team: "T1"}), store.New(""))
	app.SetExperimentGroup("agent_streaming_slimmer_task_cards", group)
	return &agentHost{app: app, th: theme.New(dark)}
}

// agent_streaming_slimmer_task_cards off (or not assigned): plans and task
// cards are cards (agent_card.go): a collapsed plan is 40px plus its
// border, a collapsed task card fits its 40px bar, both up to 480px wide
// with margin 8px; open, a plan shows every task's header and content.
func TestAgentCardSizes(t *testing.T) {
	for _, group := range []string{"off", ""} {
		h := cardHost(false, group)
		m := agentMsg(t, "plan-complete")
		me := MessageElements(h, &m)
		closed := slotSize(t, me, &m, 560)
		if closed.Y != 8+42+4 { // first block: margin 8 above, 40 + 2px border, 4 below (richview's gap is the rest)
			t.Errorf("group %q: collapsed plan card %v, want 54 tall", group, closed)
		}
		me.agentState().open["plan\x00p1"] = true
		open := slotSize(t, me, &m, 560)
		if open.Y < closed.Y+2*24 {
			t.Errorf("group %q: open plan card %v not taller than collapsed %v by two task headers", group, open, closed)
		}
		c := agentMsg(t, "task-card-complete")
		cme := MessageElements(h, &c)
		if sz := slotSize(t, cme, &c, 560); sz.Y != 8+42+4 {
			t.Errorf("group %q: collapsed task card %v, want its 40px bar", group, sz)
		}
	}
	// "on": the slim line (TestAgentSlotSizes)
	h := cardHost(false, "on")
	m := agentMsg(t, "plan-complete")
	me := MessageElements(h, &m)
	if sz := slotSize(t, me, &m, 560); sz.Y < 20 || sz.Y > 30 {
		t.Errorf("slim plan %v", sz)
	}
}

// TestAgentCardShots renders every fixture as cards (agent-cards-{light,dark}.png).
func TestAgentCardShots(t *testing.T) {
	for _, dark := range []bool{false, true} {
		h := cardHost(dark, "off")
		names := []string{"plan-complete", "plan-complete-open", "plan-in-progress", "plan-error", "plan-untitled",
			"task-card-pending", "task-card-in-progress", "task-card-complete", "task-card-error", "task-card-mcp"}
		var msgs []core.Message
		for _, n := range names {
			msgs = append(msgs, agentMsg(t, n))
		}
		s := newAgentShot(h, msgs...)
		for i, n := range names {
			switch n {
			case "plan-complete-open", "plan-in-progress", "plan-error", "task-card-complete", "task-card-error", "task-card-in-progress":
				b := agentButton(&s.rows[i].m, 0)
				s.rows[i].me.agentState().open[b.Kind+"\x00"+b.BlockID] = true
			}
		}
		const w, hgt = 620, 2200
		now := time.Date(2026, 11, 8, 12, 0, 0, 0, time.UTC)
		var ops *op.Ops
		for i := 0; i < 3; i++ {
			ops = s.frame(w, hgt, now.Add(time.Duration(i)*300*time.Millisecond))
		}
		img := pixeltest.Render(t, w, hgt, ops)
		if dir := os.Getenv("BK_AGENT_SHOTS_DIR"); dir != "" {
			name := "agent-cards-light.png"
			if dark {
				name = "agent-cards-dark.png"
			}
			f, err := os.Create(filepath.Join(dir, name))
			if err != nil {
				t.Fatal(err)
			}
			_ = png.Encode(f, img)
			_ = f.Close()
		}
	}
}
