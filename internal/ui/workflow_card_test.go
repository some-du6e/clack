package ui

// A share_shortcut card's actions reach the ui root the way ShareShortcut
// (boot-shared KrN7, on WorkflowEntity) wires them for a viewer who
// doesn't manage the workflow:
//
//   - the card opens the workflow details view for its trigger;
//   - More actions (MoreActionsMenu, 9mPk) lists Start workflow first for
//     a link trigger (WjTy), then Copy workflow link and View details,
//     the bot's VIP item on a paid team, Copy workflow ID;
//   - Start workflow trips the link from the message, as the card's own
//     button does (workflowsTriggersTrip: location message, the
//     message's channel and ts).
//
// The row's richHandler drives the same hooks (messages
// TestWorkflowCardHandler).

import (
	"image"
	"slices"
	"testing"
	"time"

	"github.com/some-du6e/clack/internal/fakeslack"
	"github.com/some-du6e/clack/internal/richtext"
	"github.com/some-du6e/clack/internal/ui/popups"
)

func TestShareShortcutCardWiring(t *testing.T) {
	e, fs := workflowsEnv(t)
	if popups.WorkflowCardMenu == nil || popups.OpenWorkflowDetails == nil {
		t.Fatal("the share_shortcut card's hooks aren't set")
	}
	ch, ts := fakeslack.WorkflowsChannelID, fakeslack.WorkflowsTS(0)
	card := &richtext.ShortcutCard{TriggerID: fakeslack.TriggerDeploy, Title: "Run deploy", TriggerType: "shortcut",
		URL: fakeslack.WorkflowLink(fakeslack.TriggerDeploy), ShareURL: fakeslack.WorkflowShareURL(fakeslack.TriggerDeploy),
		WorkflowID: "Wf0FAKE01", BotUserID: "U0BOT00001", State: "enabled"}
	n := len(e.a.overlays)
	popups.WorkflowCardMenu(e.a, image.Rect(600, 300, 632, 332), card, ch, ts)
	e.frame()
	if len(e.a.overlays) != n+1 {
		t.Fatalf("More actions didn't open (%d overlays)", len(e.a.overlays))
	}
	m, ok := e.a.overlays[n].(*popups.Menu)
	if !ok {
		t.Fatalf("More actions is a %T", e.a.overlays[n])
	}
	var keys []string
	for _, it := range m.Items() {
		if !it.Separator {
			keys = append(keys, it.Key)
		}
	}
	want := []string{"start_workflow", "copy_workflow", "view_details"}
	if e.a.Core().PaidTeam() {
		want = append(want, "workflow_menu_item_add_to_vip")
	}
	want = append(want, "copy_workflow_id")
	if !slices.Equal(keys, want) {
		t.Errorf("More actions %q, want %q", keys, want)
	}
	if items := m.Items(); len(items) < 2 || items[0].Key != "start_workflow" || !items[1].Separator {
		t.Errorf("Start workflow isn't first, in its own group: %q", keys)
	}
	for _, it := range m.Items() {
		if it.Key == "start_workflow" {
			it.Do()
		}
	}
	e.until("the trip", 10*time.Second, func() bool { return len(fs.TripCalls()) == 1 })
	c := fs.TripCalls()[0]
	if c.URL != card.URL || c.Context["location"] != "message" || c.Context["channel_id"] != ch || c.Context["message_ts"] != ts {
		t.Errorf("Start workflow's trip %+v, want the link from message %s/%s", c, ch, ts)
	}
	// the card itself: the details view for its trigger
	popups.OpenWorkflowDetails(card.TriggerID, ch)
	if _, ok := e.a.secondary.view.(*wfDetailsPane); !ok || e.a.secondary.kind != secWorkflow {
		t.Errorf("no workflow details pane (%T, kind %d)", e.a.secondary.view, e.a.secondary.kind)
	}
}
