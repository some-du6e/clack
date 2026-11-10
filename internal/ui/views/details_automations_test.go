package views

import (
	"image"
	"image/png"
	"os"
	"path/filepath"
	"sync"
	"testing"

	"gioui.org/layout"

	"github.com/some-du6e/clack/internal/core"
	"github.com/some-du6e/clack/internal/fakeslack"
	"github.com/some-du6e/clack/internal/slack"
	"github.com/some-du6e/clack/internal/store"
	"github.com/some-du6e/clack/internal/ui/theme"
	"github.com/some-du6e/clack/internal/ui/theme/pixeltest"
)

// Channel details > Automations against internal/fakeslack's
// #workflows-gallery (Size.Workflows) only.

func autosFake(t *testing.T) (*core.App, *fakeslack.Server) {
	t.Helper()
	sz := fakeslack.Small
	sz.Workflows = true
	fs, err := fakeslack.Start(sz)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = fs.Close() })
	s := store.New("")
	s.Lock()
	s.TeamID, s.TeamName = fakeslack.TeamID, "Synthetic Club"
	s.Unlock()
	s.PutUsers([]slack.User{{ID: fakeslack.SelfID, Name: "self", RealName: "Test Self"}})
	s.PutConv(slack.Conversation{ID: fakeslack.WorkflowsChannelID, Name: "workflows-gallery", IsChannel: true, IsMember: true, NumMembers: 5})
	app := core.New(slack.New(fs.Creds()), s)
	if u, ok := s.User(fakeslack.SelfID); ok {
		app.S.Self = *u
	}
	app.ServeOffline()
	return app, fs
}

// TestAutomationsTab: the tab lists the workflow apps first (Run deploy,
// Request time off), then the legacy workflow; a workflow app's click
// trips its link trigger with Slack's channel_details context, a legacy
// workflow's runs apps.actions.run with Slack's toast.
func TestAutomationsTab(t *testing.T) {
	app, fs := autosFake(t)
	app.SetExperimentGroup("channel_details_agents_apps_tab", "on")
	h := &slickHost{app: app, th: theme.New(false)}
	d := NewChannelDetails(h, fakeslack.WorkflowsChannelID)
	d.ShowTab("Automations")
	e := &vaEnv{size: image.Pt(1000, 760)}
	e.draw = func(gtx layout.Context) { d.Layout(gtx) }
	st := d.autos()
	e.until(t, "the automations", func() bool { _, ok, _ := st.data.get(); return ok })
	items, _, err := st.data.get()
	if err != nil || len(items) != 3 {
		t.Fatalf("automations %d, %v", len(items), err)
	}
	if items[0].app == nil || items[0].app.Title() != "Run deploy" || items[1].app == nil || items[1].app.Title() != "Request time off" ||
		items[2].action == nil || items[2].action.Name != fakeslack.LegacyActionName {
		t.Errorf("order: %+v %+v %+v", items[0], items[1], items[2])
	}
	var mu sync.Mutex
	var trips []core.TripContext
	var links []string
	old := TripWorkflow
	TripWorkflow = func(_ Host, link string, tc core.TripContext, uiPage string) {
		mu.Lock()
		trips, links = append(trips, tc), append(links, link)
		mu.Unlock()
	}
	t.Cleanup(func() { TripWorkflow = old })
	d.runAutomation(items[1])
	mu.Lock()
	if len(links) != 1 || links[0] != fakeslack.WorkflowLink(fakeslack.TriggerTimeOff) || trips[0].Location() != "channel_details" ||
		trips[0]["channel_id"] != fakeslack.WorkflowsChannelID {
		t.Errorf("trip %v %v", links, trips)
	}
	mu.Unlock()
	d.runAutomation(items[2])
	e.until(t, "the legacy run's toast", func() bool {
		for _, s := range h.toasts() {
			if s == "You used the workflow "+fakeslack.LegacyActionName {
				return true
			}
		}
		return false
	})
	if runs := fs.ActionRuns(); len(runs) != 1 || runs[0].Get("action_id") != fakeslack.LegacyActionID {
		t.Errorf("apps.actions.run %v", runs)
	}
	vaShotTo(t, e, "WORKFLOWS_SHOTS_DIR", "automations-tab", false)
}

// TestAutomationsEmpty: a channel without workflows shows Slack's empty
// state, worded by the channel_details_agents_apps_tab experiment.
func TestAutomationsEmpty(t *testing.T) {
	for _, dark := range []bool{false, true} {
		app, fs := autosFake(t)
		app.SetExperimentGroup("channel_details_agents_apps_tab", "on")
		ch := fs.ChannelIDs()[0] // a generated channel: conversations.info knows it, it has no workflows
		app.S.PutConv(slack.Conversation{ID: ch, Name: "no-workflows", IsChannel: true, IsMember: true, NumMembers: 2})
		h := &slickHost{app: app, th: theme.New(dark)}
		d := NewChannelDetails(h, ch)
		d.ShowTab("Automations")
		e := &vaEnv{size: image.Pt(1000, 760)}
		e.draw = func(gtx layout.Context) { d.Layout(gtx) }
		st := d.autos()
		e.until(t, "the automations", func() bool { _, ok, _ := st.data.get(); return ok })
		if items, _, _ := st.data.get(); len(items) != 0 {
			t.Errorf("automations in an empty channel: %d", len(items))
		}
		vaShotTo(t, e, "WORKFLOWS_SHOTS_DIR", "automations-empty", dark)
	}
}

// TestAutomationsTabDark: the list, dark.
func TestAutomationsTabDark(t *testing.T) {
	app, _ := autosFake(t)
	app.SetExperimentGroup("channel_details_agents_apps_tab", "on")
	d := NewChannelDetails(&slickHost{app: app, th: theme.New(true)}, fakeslack.WorkflowsChannelID)
	d.ShowTab("Automations")
	e := &vaEnv{size: image.Pt(1000, 760)}
	e.draw = func(gtx layout.Context) { d.Layout(gtx) }
	st := d.autos()
	e.until(t, "the automations", func() bool { _, ok, _ := st.data.get(); return ok })
	vaShotTo(t, e, "WORKFLOWS_SHOTS_DIR", "automations-tab", true)
}

// vaShotTo is vaShot with the directory from env (the workflows-shots
// workflow sets WORKFLOWS_SHOTS_DIR).
func vaShotTo(t *testing.T, e *vaEnv, env, name string, dark bool) {
	t.Helper()
	dir := os.Getenv(env)
	if dir == "" {
		return
	}
	mode := "light"
	if dark {
		mode = "dark"
	}
	win, err := pixeltest.NewWindow(e.size.X, e.size.Y)
	if err != nil {
		t.Fatalf("headless window: %v", err)
	}
	defer win.Release()
	for range 3 {
		e.frame()
	}
	if err := win.Frame(&e.ops); err != nil {
		t.Fatalf("frame: %v", err)
	}
	img := image.NewRGBA(image.Rectangle{Max: e.size})
	if err := win.Screenshot(img); err != nil {
		t.Fatalf("screenshot: %v", err)
	}
	f, err := os.Create(filepath.Join(dir, name+"-"+mode+".png"))
	if err != nil {
		t.Fatal(err)
	}
	defer f.Close()
	if err := png.Encode(f, img); err != nil {
		t.Fatal(err)
	}
}
