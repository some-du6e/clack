package blockkit

// A slim plan task's or task card's content as Slack's TaskItem draws it:
// details and output past 3 lines or 250 characters cut, with Show more
// (TruncatedBlocks), clicked through Gio's router; and the MCP task card
// that closes when its task is done
// (slackbot_mcp_tasks_card_auto_collapse_when_complete). Shots go to
// BK_AGENT_SHOTS_DIR (agent-slim-content-{light,dark}.png). Only helpers
// that predate the fix, so the file also runs against the old code.

import (
	"image"
	"image/png"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"gioui.org/f32"
	"gioui.org/io/pointer"
	"gioui.org/op"

	"github.com/some-du6e/clack/internal/core"
	"github.com/some-du6e/clack/internal/fakeslack"
	"github.com/some-du6e/clack/internal/slack"
	"github.com/some-du6e/clack/internal/store"
	"github.com/some-du6e/clack/internal/ui/richview"
	"github.com/some-du6e/clack/internal/ui/theme"
	"github.com/some-du6e/clack/internal/ui/theme/pixeltest"
)

// drawnLabels collects what theme.Text draws (labels, not message bodies).
type drawnLabels struct{ s []string }

func captureLabels(t *testing.T) *drawnLabels {
	d := &drawnLabels{}
	theme.DrawHook = func(s string) { d.s = append(d.s, s) }
	t.Cleanup(func() { theme.DrawHook = nil })
	return d
}

func (d *drawnLabels) count(s string) int {
	n := 0
	for _, x := range d.s {
		if x == s {
			n++
		}
	}
	return n
}

// viewTexts is the text of each of the message's rich text views that
// starts with prefix.
func viewTexts(st *agentState, prefix string) []string {
	var out []string
	for _, at := range st.views {
		if at == nil || at.view == nil || at.view.v == nil {
			continue
		}
		v := at.view.v
		if s := strings.TrimSpace(v.TextRange(0, v.PlainLen())); strings.HasPrefix(s, prefix) {
			out = append(out, s)
		}
	}
	return out
}

// hoverFind moves the pointer down the column at x, a frame a step, until
// the tap at key reports the hover: where the tap is drawn.
func (s *agentShot) hoverFind(st *agentState, key string, x, y0, y1, w, hgt int, now time.Time) (image.Point, bool) {
	for y := y0; y < y1; y += 2 {
		s.r.Queue(pointer.Event{Kind: pointer.Move, Source: pointer.Mouse, Position: f32.Pt(float32(x), float32(y))})
		s.frame(w, hgt, now)
		if tp := st.taps[key]; tp != nil && tp.g.Hovered() {
			return image.Pt(x, y), true
		}
	}
	return image.Point{}, false
}

// slotH is the height of the row's first element slot as last drawn.
func slotH(t *testing.T, r *agentRow) int {
	t.Helper()
	sl := r.v.ElementSlots()
	if len(sl) == 0 {
		t.Fatal("no element slot")
	}
	return sl[0].Dy()
}

func TestAgentTaskShowMore(t *testing.T) {
	drawn := captureLabels(t)
	h := &agentHost{th: theme.New(false)} // no core: slim, as the account
	m := agentMsg(t, "plan-long-details")
	s := newAgentShot(h, m)
	row := s.rows[0]
	st := row.me.agentState()
	st.open["plan\x00p5"] = true
	const w, hgt = 560, 900
	// Slot sizes are measured on the wall clock (Message.scratch) and drawn
	// on the frame's: frames an hour ahead end every height transition in
	// a few frames, however slow the runner.
	now := time.Now().Add(time.Hour)
	tick := func(n int) {
		for i := 0; i < n; i++ {
			now = now.Add(100 * time.Millisecond)
			if i == n-1 {
				drawn.s = nil // the labels of the last frame
			}
			s.frame(w, hgt, now)
		}
	}
	// two frames: the first also measures the slot, and theme.DrawHook
	// sees that pass's labels too
	tick(2)
	// the long details and the five-line ones are cut; "All green." isn't
	if n := drawn.count("Show more"); n != 2 {
		t.Errorf("Show more drawn %d times, want 2 (labels %q)", n, drawn.s)
	}
	long := fakeslack.AgentLongDetail
	cut := strings.TrimRight(long[:250], " ") + "\u2026"
	if got := viewTexts(st, long[:40]); len(got) != 1 || got[0] != cut {
		t.Errorf("long details drawn as %q, want the first 250 characters and an ellipsis %q", got, cut)
	}
	if got := viewTexts(st, "Opened the fixture."); len(got) != 1 || got[0] != "Opened the fixture.\nFound the start date.\nIt is a year early." {
		t.Errorf("five-line details drawn as %q, want their first three lines (no ellipsis)", got)
	}
	closedH := slotH(t, row)

	// Show more under the long details: the whole text and Show less
	sl := row.v.ElementSlots()[0].Add(row.at)
	moreKey := "task\x00p5\x00t1\x00d\x00more"
	p, ok := s.hoverFind(st, moreKey, sl.Min.X+20+12, sl.Min.Y, sl.Max.Y, w, hgt, now)
	if !ok {
		t.Fatalf("no Show more tap under the long details (slot %v)", sl)
	}
	s.click(p)
	tick(6) // the height transition, then a frame that measures and one that only draws
	if n, m := drawn.count("Show less"), drawn.count("Show more"); n != 1 || m != 1 {
		t.Errorf("after Show more: Show less %d, Show more %d, want 1 and 1 (the other task's)", n, m)
	}
	if got := viewTexts(st, long[:40]); !containsStr(got, long) {
		t.Errorf("after Show more the details are %q, want the whole text", got)
	}
	openH := slotH(t, row)
	if openH <= closedH {
		t.Errorf("Show more: slot %d, not taller than the cut %d", openH, closedH)
	}

	// Show less cuts it again
	sl2 := row.v.ElementSlots()[0].Add(row.at)
	p, ok = s.hoverFind(st, moreKey, sl2.Min.X+20+12, sl2.Min.Y, sl2.Max.Y, w, hgt, now)
	if !ok {
		t.Fatal("no Show less tap")
	}
	s.click(p)
	tick(6) // the height transition, then a frame that measures and one that only draws
	if n := drawn.count("Show more"); n != 2 {
		t.Errorf("after Show less: Show more drawn %d times, want 2", n)
	}
	if got := slotH(t, row); got != closedH {
		t.Errorf("after Show less: slot %d, want the cut %d", got, closedH)
	}

	// open it, close the plan and open it again: TaskItem remounts, cut
	p, _ = s.hoverFind(st, moreKey, sl.Min.X+20+12, sl.Min.Y, sl.Max.Y, w, hgt, now)
	s.click(p)
	tick(6) // the height transition, then a frame that measures and one that only draws
	toggle := image.Pt(sl.Min.X+24, sl.Min.Y+9)
	s.click(toggle)
	tick(6)
	s.click(toggle)
	tick(6)
	if n, l := drawn.count("Show more"), drawn.count("Show less"); n != 2 || l != 0 {
		t.Errorf("plan reopened: Show more %d, Show less %d, want 2 and 0 (TaskItem starts over)", n, l)
	}
	if got := slotH(t, row); got != closedH {
		t.Errorf("plan reopened: slot %d, want the cut %d", got, closedH)
	}
}

func containsStr(xs []string, s string) bool {
	for _, x := range xs {
		if x == s {
			return true
		}
	}
	return false
}

// mcpHost is a slim host whose core has
// slackbot_mcp_tasks_card_auto_collapse_when_complete in group.
func mcpHost(dark bool, group string) *agentHost {
	app := core.New(slack.New(slack.Creds{Token: "xoxc-test", UserID: "U1", Team: "T1"}), store.New(""))
	app.SetExperimentGroup("agent_streaming_slimmer_task_cards", "on")
	app.SetExperimentGroup("slackbot_mcp_tasks_card_auto_collapse_when_complete", group)
	return &agentHost{app: app, th: theme.New(dark)}
}

// streamTo is m with fixture name's blocks: the stream's next update of
// the same message (same ts, so the same block state).
func streamTo(t *testing.T, m core.Message, name string) core.Message {
	t.Helper()
	n := agentMsg(t, name)
	n.TS = m.TS
	return n
}

func TestAgentMCPCardAutoCollapse(t *testing.T) {
	const w, hgt = 560, 400
	const closedMax = 30 // the 19.5px toggle line and its margin
	run := func(group string, toggles int, done string) (running, settled, mid int) {
		h := mcpHost(false, group)
		m := agentMsg(t, "task-card-mcp-running")
		s := newAgentShot(h, m)
		now := time.Now().Add(time.Hour) // see TestAgentTaskShowMore
		tick := func(n int) {
			for i := 0; i < n; i++ {
				now = now.Add(100 * time.Millisecond)
				s.frame(w, hgt, now)
			}
		}
		tick(2)
		running = slotH(t, s.rows[0])
		sl := s.rows[0].v.ElementSlots()[0].Add(s.rows[0].at)
		for i := 0; i < toggles; i++ {
			s.click(image.Pt(sl.Min.X+30, sl.Min.Y+9))
			tick(6)
		}
		// the stream's update: same message, the task done
		s.rows[0].m = streamTo(t, m, done)
		me := MessageElements(h, &s.rows[0].m)
		s.rows[0].me = me
		s.rows[0].v.Set(s.rows[0].m.Doc, richview.Options{Elements: me})
		now = now.Add(16 * time.Millisecond)
		s.frame(w, hgt, now)
		mid = slotH(t, s.rows[0])
		tick(6)
		settled = slotH(t, s.rows[0])
		return
	}
	running, settled, mid := run("on", 0, "task-card-mcp-done")
	if running < 40 {
		t.Errorf("running MCP card: slot %d, want it open", running)
	}
	if settled > closedMax {
		t.Errorf("MCP card done: slot %d, want it closed (at most %d)", settled, closedMax)
	}
	if mid <= settled {
		t.Errorf("MCP card done: the first frame is %d, want the .25s height transition from %d", mid, running)
	}
	// opened and closed by the viewer: left as they had it
	if r, sd, _ := run("on", 2, "task-card-mcp-done"); sd != r {
		t.Errorf("toggled MCP card done: slot %d, want it still open (%d)", sd, r)
	}
	// without the experiment it stays open
	if r, sd, _ := run("off", 0, "task-card-mcp-done"); sd != r {
		t.Errorf("experiment off, MCP card done: slot %d, want it still open (%d)", sd, r)
	}

	// already done when first drawn: closed; without the experiment open
	for _, c := range []struct {
		group string
		open  bool
	}{{"on", false}, {"off", true}, {"", true}} {
		h := mcpHost(false, c.group)
		m := agentMsg(t, "task-card-mcp-done")
		s := newAgentShot(h, m)
		s.frame(w, hgt, time.Now().Add(time.Hour))
		got := slotH(t, s.rows[0]) > closedMax
		if got != c.open {
			t.Errorf("group %q: a done MCP card starts open=%v, want %v", c.group, got, c.open)
		}
	}
}

// A plan the app updates (chat.update, the same block) while it is open
// grows to its new height with .block's .25s height transition from the
// height on screen, as Slack's ExpandableContent does, instead of jumping;
// collapsed, a title that wraps anew does the same. The toggle's own
// transition is the same rule.
func TestAgentPlanUpdateTransition(t *testing.T) {
	const w, hgt = 560, 600
	for _, c := range []struct {
		name     string
		open     bool
		from, to string
	}{
		{"open", true, "plan-grow-1", "plan-grow-2"},
		{"collapsed", false, "plan-grow-1", "plan-grow-title"},
	} {
		h := &agentHost{th: theme.New(false)}
		m := agentMsg(t, c.from)
		s := newAgentShot(h, m)
		if c.open {
			s.rows[0].me.agentState().open["plan\x00p6"] = true
		}
		now := time.Now().Add(time.Hour) // see TestAgentTaskShowMore
		tick := func(n int) {
			for i := 0; i < n; i++ {
				now = now.Add(100 * time.Millisecond)
				s.frame(w, hgt, now)
			}
		}
		tick(2)
		before := slotH(t, s.rows[0])
		r := s.rows[0]
		r.m = streamTo(t, m, c.to)
		r.me = MessageElements(h, &r.m)
		r.v.Set(r.m.Doc, richview.Options{Elements: r.me})
		now = now.Add(16 * time.Millisecond)
		s.frame(w, hgt, now)
		first := slotH(t, r)
		tick(6)
		after := slotH(t, r)
		if after <= before {
			t.Errorf("%s: the update left the plan %dpx tall, was %d: want it taller", c.name, after, before)
		}
		if first != before {
			t.Errorf("%s: the frame after the update is %dpx, want the %dpx on screen (the transition to %d starts there)", c.name, first, before, after)
		}
	}
}

// TestAgentSlimContentShots: a plan's cut details with Show more, the
// same plan with its long details shown in full (Show less), the running
// MCP card and the same card once done, and the plan the app updates
// (open, then grown; collapsed, then its title wrapped), settled.
func TestAgentSlimContentShots(t *testing.T) {
	for _, dark := range []bool{false, true} {
		h := mcpHost(dark, "on")
		cut, full := agentMsg(t, "plan-long-details"), agentMsg(t, "plan-long-details")
		running, done := agentMsg(t, "task-card-mcp-running"), agentMsg(t, "task-card-mcp-running")
		grow, wrap := agentMsg(t, "plan-grow-1"), agentMsg(t, "plan-grow-1")
		s := newAgentShot(h, cut, full, running, done, grow, wrap)
		for _, i := range []int{0, 1} {
			s.rows[i].me.agentState().open["plan\x00p5"] = true
		}
		s.rows[4].me.agentState().open["plan\x00p6"] = true
		s.rows[1].me.agentState().open["task\x00p5\x00t1\x00d\x00more"] = true
		const w, hgt = 560, 1400
		now := time.Now().Add(time.Hour) // see TestAgentTaskShowMore
		var ops *op.Ops
		for i := 0; i < 2; i++ {
			now = now.Add(100 * time.Millisecond)
			ops = s.frame(w, hgt, now)
		}
		for i, to := range map[int]string{3: "task-card-mcp-done", 4: "plan-grow-2", 5: "plan-grow-title"} {
			r := s.rows[i]
			r.m = streamTo(t, r.m, to)
			r.me = MessageElements(h, &r.m)
			r.v.Set(r.m.Doc, richview.Options{Elements: r.me})
		}
		for i := 0; i < 6; i++ {
			now = now.Add(100 * time.Millisecond)
			ops = s.frame(w, hgt, now)
		}
		img := pixeltest.Render(t, w, hgt, ops)
		if dir := os.Getenv("BK_AGENT_SHOTS_DIR"); dir != "" {
			name := "agent-slim-content-light.png"
			if dark {
				name = "agent-slim-content-dark.png"
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
