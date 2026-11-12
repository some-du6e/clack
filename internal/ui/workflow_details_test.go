package ui

import (
	"strings"
	"testing"
	"time"

	"github.com/some-du6e/clack/internal/core"
	"github.com/some-du6e/clack/internal/fakeslack"
	"github.com/some-du6e/clack/internal/ui/popups"
)

// The message actions' hooks reach the ui root: the share dialog's
// schedule caret uses the composer's ScheduleSendMenu and
// ScheduleMessageDialog, and "View workflow details" opens the details
// view.
func TestMessageActionsHooksWired(t *testing.T) {
	e, _ := workflowsEnv(t)
	_ = e
	if popups.ScheduleSendMenu == nil || popups.OpenScheduleDialog == nil || popups.OpenWorkflowDetails == nil {
		t.Fatal("the share dialog's schedule hooks or the workflow details hook aren't set")
	}
	var _ popups.IconToaster = e.a // ScheduleForwardToast's icon toast
}

// The workflow details view (WorkflowDetails, HRfd) for #workflows-
// gallery's Run deploy: workflows.triggers.info with include_workflow_
// details, then functions.workflows.get; the shortcut's title, managers
// and steps; ▶ Start Workflow trips the link from the flexpane; an
// unknown trigger draws "Workflow not found". Shots light and dark
// (WORKFLOWS_SHOTS_DIR).
func TestWorkflowDetailsPaneLikeSlack(t *testing.T) {
	e, fs := workflowsEnv(t)
	e.a.OpenWorkflowDetails(fakeslack.TriggerDeploy, fakeslack.WorkflowsChannelID)
	p, ok := e.a.secondary.view.(*wfDetailsPane)
	if !ok || e.a.secondary.kind != secWorkflow || !e.a.secondary.open() {
		t.Fatalf("no workflow details pane (%T, kind %d)", e.a.secondary.view, e.a.secondary.kind)
	}
	var s wfShortcut
	e.until("the trigger and its workflow", 10*time.Second, func() bool {
		var done bool
		s, _, done = p.shortcut()
		p.mu.Lock()
		wf := p.wf
		p.mu.Unlock()
		return done && wf != nil
	})
	if s.title != "Run deploy" || s.workflowID != "Wf0FAKE01" || !s.runnable || len(s.collaborators) != 2 || len(s.steps) != 3 {
		t.Fatalf("shortcut %+v", s)
	}
	if sw := s.steps[1]; sw.Type != "switch" || len(sw.Steps) != 2 || !sw.Steps[0].Expression || sw.Steps[1].Expression {
		t.Errorf("the switch step %+v", sw)
	}
	if got := p.managers(s); got == "" {
		t.Error("no managers")
	}
	if e := stepEntity(s.steps[0]); e.glyph != "form" || e.theme != "jade" {
		t.Errorf("open_form entity %+v", e)
	}
	if e := stepEntity(s.steps[1]); e.glyph != "branch" {
		t.Errorf("switch entity %+v", e)
	}
	for k := 0; k < 4; k++ {
		e.frame()
	}
	e.a.setDark(false)
	e.frame()
	e.frame()
	e.wfShot("workflow-details-light")
	e.a.setDark(true)
	e.frame()
	e.frame()
	e.wfShot("workflow-details-dark")
	e.a.setDark(false)

	// ▶ Start Workflow: the link, from the flexpane
	p.start.Click()
	e.frame()
	e.until("the trip", 10*time.Second, func() bool { return len(fs.TripCalls()) > 0 })
	c := fs.TripCalls()[0]
	if c.URL != fakeslack.WorkflowLink(fakeslack.TriggerDeploy) || c.Context["location"] != "flexpane" {
		t.Errorf("trip %+v", c)
	}

	// an unknown trigger: Slack's error state
	e.a.OpenWorkflowDetails("Ft0NOSUCH99", "")
	q := e.a.secondary.view.(*wfDetailsPane)
	e.until("the error", 10*time.Second, func() bool {
		_, code, done := q.shortcut()
		return done && code != ""
	})
	e.frame()
	e.wfShot("workflow-details-error-light")
	// the pane closes from its header
	q.close.Click()
	e.frame()
	e.frame()
	if e.a.secondary.open() && e.a.secondary.kind == secWorkflow {
		t.Error("the close button left the pane open")
	}
	_ = core.FlexpaneTripContext
}

// Every glyph the details view can draw resolves: Slack's own paths in
// wfGlyphs, the Slackbot logo, or a registered icon (the icon set's
// TestIconRefsExist covers icons.Get; these go through wfDrawGlyph).
func TestWorkflowDetailsGlyphsResolve(t *testing.T) {
	cbs := []string{"send_message", "send_dm", "open_form", "create_channel", "archive_channel", "unarchive_channel", "add_pin",
		"remove_pin", "update_channel_topic", "invite_user_to_channel", "create_usergroup", "summarize_channel", "search_summary",
		"ai_answer", "send_slackbot_prompt", "send_prompt_to_agent", "open_3p_agent_conversation", "create_code_channel", "loop_step", "somebody_elses_step"}
	for _, cb := range cbs {
		if g := wfStepGlyph(cb); !wfHasGlyph(g) {
			t.Errorf("%s: %q doesn't resolve", cb, g)
		}
	}
	for _, k := range []string{"scheduled", "webhook", "external", "service"} {
		if g, l := wfTriggerType(k); !wfHasGlyph(g) || l == "" {
			t.Errorf("%s: %q / %q", k, g, l)
		}
	}
	for _, g := range []string{"right-click", "wrench", "workspace", "branch", "arrow-down", "refresh"} {
		if !wfHasGlyph(g) {
			t.Errorf("%q doesn't resolve", g)
		}
	}
	for name, d := range wfGlyphs {
		if !strings.HasPrefix(d, "M") || !strings.HasSuffix(d, "Z") {
			t.Errorf("%s: not a closed polygon path", name)
		}
	}
}
