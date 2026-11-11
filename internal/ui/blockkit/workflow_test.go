package blockkit

import (
	"context"
	"image"
	"image/png"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"gioui.org/layout"
	"gioui.org/op"
	"gioui.org/op/clip"
	"gioui.org/op/paint"
	"gioui.org/unit"

	"github.com/some-du6e/clack/internal/core"
	"github.com/some-du6e/clack/internal/fakeslack"
	"github.com/some-du6e/clack/internal/imgcache"
	"github.com/some-du6e/clack/internal/slack"
	"github.com/some-du6e/clack/internal/store"
	"github.com/some-du6e/clack/internal/ui/popups"
	"github.com/some-du6e/clack/internal/ui/theme"
	"github.com/some-du6e/clack/internal/ui/theme/pixeltest"
)

// Workflow runs against internal/fakeslack's #workflows-gallery only
// (Size.Workflows): never a real workspace.

type wfEnv struct {
	t  *testing.T
	h  *bkHost
	fs *fakeslack.Server
	a  *core.App
}

func newWFEnv(t *testing.T, dark bool) *wfEnv {
	t.Helper()
	sz := fakeslack.Small
	sz.Workflows = true
	fs, err := fakeslack.Start(sz)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { fs.Close() })
	a := core.New(slack.New(fs.Creds()), store.New(t.TempDir()))
	t.Cleanup(a.Shutdown)
	if err := a.Boot(context.Background()); err != nil {
		t.Fatal(err)
	}
	h := &bkHost{a: a, th: theme.New(dark)}
	h.img = imgcache.New(func(ctx context.Context, u string) ([]byte, error) {
		b, _, err := a.Fetch(ctx, u)
		return b, err
	}, "", nil)
	return &wfEnv{t: t, h: h, fs: fs, a: a}
}

// until runs the host's posts until cond holds.
func (e *wfEnv) until(what string, cond func() bool) {
	e.t.Helper()
	deadline := time.Now().Add(10 * time.Second)
	for time.Now().Before(deadline) {
		e.h.runPosts()
		drain()
		if cond() {
			return
		}
		time.Sleep(10 * time.Millisecond)
	}
	e.t.Fatalf("timed out waiting for %s", what)
}

func (e *wfEnv) toast(want string) {
	e.t.Helper()
	e.until("toast "+want, func() bool {
		for _, s := range e.h.toasts {
			if s == want {
				return true
			}
		}
		return false
	})
}

func (e *wfEnv) trips(n int) []fakeslack.TripCall {
	e.t.Helper()
	e.until("trips", func() bool { return len(e.fs.TripCalls()) >= n })
	return e.fs.TripCalls()
}

// TestTripWorkflow: a workflow link in a message trips with Slack's
// message context and precheck, and Slack's toast answers.
func TestTripWorkflow(t *testing.T) {
	e := newWFEnv(t, false)
	ts := fakeslack.WorkflowsTS(0)
	if !OpenWorkflowLink(e.h, fakeslack.WorkflowLink(fakeslack.TriggerDeploy), fakeslack.WorkflowsChannelID, ts) {
		t.Fatal("not a workflow link")
	}
	e.toast("You successfully started a workflow")
	c := e.trips(1)[0]
	if c.Precheck != "true" || c.Context["location"] != "message" || c.Context["channel_id"] != fakeslack.WorkflowsChannelID ||
		c.Context["message_ts"] != ts || !strings.HasPrefix(c.ClientToken, "web-") {
		t.Errorf("trip %+v", c)
	}
	if OpenWorkflowLink(e.h, "https://example.com/shortcuts/Ft0FAKEWF01/x", "", "") {
		t.Error("not a workflow link")
	}

	// a workflow the viewer can't run here: Slack's error toast
	OpenWorkflowLink(e.h, fakeslack.WorkflowLink(fakeslack.TriggerBug), fakeslack.WorkflowsChannelID, ts)
	e.toast("Unfortunately you can’t run new workflows in private channels")

	// a workflow_button: location button with the element's ids, no
	// inputs on a message that isn't ephemeral
	tripWorkflowButton(e.h, fakeslack.WorkflowLink(fakeslack.TriggerDeploy), []byte(`[{"name":"env","value":"staging"}]`),
		"wf_deploy", "wf_actions", fakeslack.WorkflowsChannelID, fakeslack.WorkflowsTS(1))
	c = e.trips(3)[2]
	want := map[string]any{"location": "button", "action_id": "wf_deploy", "block_id": "wf_actions",
		"channel_id": fakeslack.WorkflowsChannelID, "message_ts": fakeslack.WorkflowsTS(1)}
	if len(c.Context) != len(want) {
		t.Errorf("button context %v", c.Context)
	}
	for k, v := range want {
		if c.Context[k] != v {
			t.Errorf("button context %s = %v, want %v", k, c.Context[k], v)
		}
	}
}

// TestWorkflowSpeedbump: a workflow with risky scopes answers the
// precheck with pre_trip_info; the "Review workflow access" speedbump
// lists them and Start Workflow trips again without run_precheck. The
// slow workflow's toast names it.
func TestWorkflowSpeedbump(t *testing.T) {
	e := newWFEnv(t, false)
	var sb *speedbump
	var start *popups.ModalButton
	speedbumpShown = func(s *speedbump, b *popups.ModalButton) { sb, start = s, b }
	t.Cleanup(func() { speedbumpShown = nil })
	TripWorkflow(e.h, fakeslack.WorkflowLink(fakeslack.TriggerTimeOff), core.MessageTripContext(fakeslack.WorkflowsChannelID, fakeslack.WorkflowsTS(0)), "")
	e.until("the speedbump", func() bool { return sb != nil })
	if len(sb.scopes) != 2 || sb.scopes[0].ActionType != "do" || sb.subtitle() != "What will the workflow be able to do?" {
		t.Errorf("scopes %+v %q", sb.scopes, sb.subtitle())
	}
	if start.Label != "Start Workflow" || start.Icon != "play" {
		t.Errorf("start button %+v", start)
	}
	e.until("the trigger for Copy workflow link", func() bool {
		tr, ok := e.a.Interactive().Trigger(fakeslack.TriggerTimeOff)
		return ok && tr.ShareURL == fakeslack.WorkflowShareURL(fakeslack.TriggerTimeOff)
	})
	if len(e.h.toasts) != 0 {
		t.Fatalf("toasts before Start: %v", e.h.toasts)
	}
	start.Do()
	e.toast("You started Request time off. This workflow might take a little longer to run.")
	cs := e.trips(2)
	if cs[0].Precheck != "true" || cs[1].Precheck != "" || cs[1].Context["location"] != "message" {
		t.Errorf("trips %+v", cs)
	}
}

// TestWorkflowPicker: the picker searches every link trigger the viewer
// may use (empty query first), drops excluded and automated ones, and
// hands the picked trigger to OnInsert.
func TestWorkflowPicker(t *testing.T) {
	e := newWFEnv(t, false)
	var p *workflowPicker
	pickerShown = func(wp *workflowPicker) { p = wp }
	t.Cleanup(func() { pickerShown = nil })
	var got core.WorkflowTrigger
	OpenWorkflowPicker(e.h, WorkflowPickerOpts{Source: PickerShortcutsMenu, ChannelID: fakeslack.WorkflowsChannelID,
		Excluded: []string{fakeslack.TriggerBug}, OnInsert: func(t core.WorkflowTrigger) { got = t }})
	if p == nil {
		t.Fatal("no picker")
	}
	e.until("results", func() bool { return !p.loading })
	var ids []string
	for _, r := range p.results {
		ids = append(ids, r.ID)
	}
	if strings.Join(ids, ",") != fakeslack.TriggerDeploy+","+fakeslack.TriggerTimeOff {
		t.Errorf("results %v (the bug report is excluded, the scheduled standup automated)", ids)
	}
	if p.primary.Label != "Insert" || p.primary.Enabled() {
		t.Errorf("primary %q enabled=%v before a pick", p.primary.Label, p.primary.Enabled())
	}
	p.picked = fakeslack.TriggerTimeOff
	if !p.primary.Enabled() || !p.primary.Do() || got.ID != fakeslack.TriggerTimeOff || got.Name != "Request time off" ||
		got.ShareURL != fakeslack.WorkflowShareURL(fakeslack.TriggerTimeOff) {
		t.Errorf("insert %+v", got)
	}

	// with automated triggers and a query
	p = nil
	OpenWorkflowPicker(e.h, WorkflowPickerOpts{Source: PickerChannelWorkflows, IncludeAutomated: true})
	e.until("results", func() bool { return p != nil && !p.loading })
	if len(p.results) != len(fakeslackTriggerIDs()) || p.primary.Label != "Add" {
		t.Errorf("automated: %d results, primary %q", len(p.results), p.primary.Label)
	}
	p.search("standup")
	e.until("standup", func() bool { return len(p.results) == 1 && p.results[0].ID == fakeslack.TriggerStandup })
}

func fakeslackTriggerIDs() []string {
	return []string{fakeslack.TriggerDeploy, fakeslack.TriggerTimeOff, fakeslack.TriggerBug, fakeslack.TriggerStandup}
}

// TestWorkflowShots saves the speedbump and the picker, light and dark,
// when WORKFLOWS_SHOTS_DIR is set (the workflows-shots workflow).
func TestWorkflowShots(t *testing.T) {
	dir := os.Getenv("WORKFLOWS_SHOTS_DIR")
	if dir == "" {
		t.Skip("WORKFLOWS_SHOTS_DIR not set")
	}
	for _, dark := range []bool{false, true} {
		mode := "light"
		if dark {
			mode = "dark"
		}
		e := newWFEnv(t, dark)
		var sb *speedbump
		speedbumpShown = func(s *speedbump, _ *popups.ModalButton) { sb = s }
		TripWorkflow(e.h, fakeslack.WorkflowLink(fakeslack.TriggerTimeOff), core.MessageTripContext(fakeslack.WorkflowsChannelID, fakeslack.WorkflowsTS(0)), "")
		e.until("the speedbump", func() bool {
			_, ok := e.a.Interactive().Trigger(fakeslack.TriggerTimeOff)
			return sb != nil && ok
		})
		e.shot(dir, "workflow-speedbump-scopes-"+mode)
		e.h.overlays = nil
		var p *workflowPicker
		pickerShown = func(wp *workflowPicker) { p = wp }
		OpenWorkflowPicker(e.h, WorkflowPickerOpts{Source: PickerShortcutsMenu, ChannelID: fakeslack.WorkflowsChannelID})
		e.until("results", func() bool { return p != nil && !p.loading })
		p.picked = fakeslack.TriggerTimeOff
		e.shot(dir, "workflow-picker-attach-"+mode)
		e.h.overlays = nil
		speedbumpShown, pickerShown = nil, nil
	}
}

// shot lays the host's overlays out over the app background and saves
// them as name.png.
func (e *wfEnv) shot(dir, name string) {
	e.t.Helper()
	size := image.Pt(1000, 760)
	w, err := pixeltest.NewWindow(size.X, size.Y)
	if err != nil {
		e.t.Fatalf("headless window: %v", err)
	}
	defer w.Release()
	var ops op.Ops
	frame := func() {
		e.h.runPosts()
		drain()
		ops.Reset()
		gtx := layout.Context{Ops: &ops, Now: time.Now(), Metric: unit.Metric{PxPerDp: 1, PxPerSp: 1}, Constraints: layout.Exact(size)}
		paint.FillShape(gtx.Ops, colorsOf(e.h.th).Bg, clip.Rect{Max: size}.Op())
		for _, o := range e.h.overlays {
			g := gtx
			g.Constraints = layout.Constraints{Max: size}
			o.Layout(g)
		}
	}
	// frames for up to 3 s: the app icons decode off the UI goroutine
	for i := 0; i < 100; i++ {
		frame()
		time.Sleep(30 * time.Millisecond)
	}
	if err := w.Frame(&ops); err != nil {
		e.t.Fatalf("frame: %v", err)
	}
	img := image.NewRGBA(image.Rectangle{Max: size})
	if err := w.Screenshot(img); err != nil {
		e.t.Fatalf("screenshot: %v", err)
	}
	f, err := os.Create(filepath.Join(dir, name+".png"))
	if err != nil {
		e.t.Fatal(err)
	}
	defer f.Close()
	if err := png.Encode(f, img); err != nil {
		e.t.Fatal(err)
	}
}
