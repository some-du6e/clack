package messages

// A share_shortcut card's actions through a message row's richHandler (the
// richview.WorkflowCardHandler the row gives its View), as ShareShortcut
// (boot-shared KrN7) wires WorkflowEntity:
//
//   - "open" (the card) calls popups.OpenWorkflowDetails for the trigger
//     in the message's channel;
//   - "more" (the ⋮) calls popups.WorkflowCardMenu with the button's
//     rectangle in window coordinates and the message's channel and ts;
//   - "start" (Start Workflow) trips the link from the message
//     (workflowsTriggersTrip: location message, its channel and ts), and
//     while that trip is in flight a second click sends nothing
//     (BaseWorkflowStartButton is disabled while isTriggerRunning).

import (
	"image"
	"testing"
	"time"

	"github.com/some-du6e/clack/internal/core"
	"github.com/some-du6e/clack/internal/fakeslack"
	"github.com/some-du6e/clack/internal/richtext"
	"github.com/some-du6e/clack/internal/slack"
	"github.com/some-du6e/clack/internal/store"
	"github.com/some-du6e/clack/internal/ui/blockkit"
	"github.com/some-du6e/clack/internal/ui/popups"
	"github.com/some-du6e/clack/internal/ui/theme"
)

func TestWorkflowCardHandler(t *testing.T) {
	sz := fakeslack.Small
	sz.Workflows = true
	fs, err := fakeslack.Start(sz)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { fs.Close() })
	app := core.New(slack.New(fs.Creds()), store.New(t.TempDir()))
	t.Cleanup(app.Shutdown)
	h := coreHost{app: app, th: theme.New(false)}
	ch, ts := fakeslack.WorkflowsChannelID, fakeslack.WorkflowsTS(0)

	var opened, menus []string
	var anchors []image.Rectangle
	oldOpen, oldMenu := popups.OpenWorkflowDetails, popups.WorkflowCardMenu
	t.Cleanup(func() { popups.OpenWorkflowDetails, popups.WorkflowCardMenu = oldOpen, oldMenu })
	popups.OpenWorkflowDetails = func(trigger, ch string) { opened = append(opened, trigger+" "+ch) }
	popups.WorkflowCardMenu = func(_ popups.Host, anchor image.Rectangle, c *richtext.ShortcutCard, ch, ts string) {
		menus = append(menus, c.TriggerID+" "+ch+" "+ts)
		anchors = append(anchors, anchor)
	}
	r := &richHandler{h: h, m: &core.Message{Channel: ch, TS: ts},
		anchor: func(p image.Point) image.Point { return p.Add(image.Pt(1000, 2000)) }}
	card := &richtext.ShortcutCard{TriggerID: fakeslack.TriggerDeploy, TriggerType: "shortcut",
		URL: fakeslack.WorkflowLink(fakeslack.TriggerDeploy), State: "enabled"}

	r.WorkflowCard("open", card, image.Rectangle{})
	if len(opened) != 1 || opened[0] != fakeslack.TriggerDeploy+" "+ch {
		t.Errorf("open: details for %q, want %s in %s", opened, fakeslack.TriggerDeploy, ch)
	}
	r.WorkflowCard("more", card, image.Rect(10, 20, 42, 52))
	if len(menus) != 1 || menus[0] != fakeslack.TriggerDeploy+" "+ch+" "+ts || anchors[0] != image.Rect(1010, 2020, 1042, 2052) {
		t.Errorf("more: %q at %v, want the trigger in %s/%s at the ⋮ in window coordinates (1010,2020)-(1042,2052)", menus, anchors, ch, ts)
	}

	r.WorkflowCard("start", card, image.Rectangle{})
	if !blockkit.TripRunning(card.TriggerID) {
		t.Fatal("Start Workflow's trip isn't in flight")
	}
	r.WorkflowCard("start", card, image.Rectangle{}) // while it runs: nothing
	until := func(what string, ok func() bool) {
		t.Helper()
		for end := time.Now().Add(10 * time.Second); !ok(); {
			if time.Now().After(end) {
				t.Fatalf("timed out waiting for %s", what)
			}
			time.Sleep(10 * time.Millisecond)
		}
	}
	until("the trip", func() bool { return len(fs.TripCalls()) >= 1 })
	until("its answer", func() bool { blockkit.Pump(); return !blockkit.TripRunning(card.TriggerID) })
	if n := len(fs.TripCalls()); n != 1 {
		t.Fatalf("%d trips for two clicks while the first was in flight, want 1", n)
	}
	c := fs.TripCalls()[0]
	if c.URL != card.URL || c.Context["location"] != "message" || c.Context["channel_id"] != ch || c.Context["message_ts"] != ts {
		t.Errorf("trip %+v, want the link from message %s/%s", c, ch, ts)
	}
	// answered: Start works again
	r.WorkflowCard("start", card, image.Rectangle{})
	until("the second trip", func() bool { return len(fs.TripCalls()) == 2 })
	until("its answer", func() bool { blockkit.Pump(); return !blockkit.TripRunning(card.TriggerID) })
}
