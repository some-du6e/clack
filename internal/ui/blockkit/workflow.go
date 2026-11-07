package blockkit

import (
	"context"
	"encoding/json"
	"errors"
	"time"

	"github.com/some-du6e/yoloslackgoclient/internal/core"
)

// Workflow links (https://slack.com/shortcuts/Ft…) and workflow_button
// elements start their workflow in Slack (workflowsTriggersTrip:
// workflows.triggers.trip with run_precheck first; a workflow that wants
// confirmation gets the "Start workflow?" speedbump, then runs).

// IsWorkflowLink reports whether u is a workflow link trigger.
func IsWorkflowLink(u string) bool { return core.WorkflowTriggerID(u) != "" }

// OpenWorkflowLink starts the workflow behind link (from a message in
// ch/ts, or "" elsewhere). It reports whether link was a workflow link.
func OpenWorkflowLink(h Host, link, ch, ts string) bool {
	if !IsWorkflowLink(link) {
		return false
	}
	tripWorkflow(h, link, ch, ts, nil)
	return true
}

func tripWorkflow(h Host, link, ch, ts string, inputs json.RawMessage) {
	appFor(h) // forms the workflow opens arrive as views
	ix := h.Core().Interactive()
	trip := func(precheck bool) {
		run(h, 20*time.Second, func(ctx context.Context) (struct{}, error) {
			return struct{}{}, ix.TripWorkflow(ctx, link, ch, ts, precheck, inputs)
		}, func(_ struct{}, err error) {
			switch {
			case errors.Is(err, core.ErrPretripAck):
				showConfirm(h, &Confirm{Title: &Text{Type: "plain_text", Text: "Start workflow?"},
					Text:    &Text{Type: "plain_text", Text: "This workflow will run with your permissions."},
					Confirm: &Text{Type: "plain_text", Text: "Start"}, Deny: &Text{Type: "plain_text", Text: "Cancel"}},
					func() { tripWorkflowNoCheck(h, link, ch, ts, inputs) }, nil)
			case err != nil:
				h.Toast("Couldn't start the workflow: " + errMessage(err))
			default:
				h.Toast("Workflow started")
			}
		})
	}
	trip(true)
}

func tripWorkflowNoCheck(h Host, link, ch, ts string, inputs json.RawMessage) {
	ix := h.Core().Interactive()
	run(h, 20*time.Second, func(ctx context.Context) (struct{}, error) {
		return struct{}{}, ix.TripWorkflow(ctx, link, ch, ts, false, inputs)
	}, func(_ struct{}, err error) {
		if err != nil {
			h.Toast("Couldn't start the workflow: " + errMessage(err))
			return
		}
		h.Toast("Workflow started")
	})
}

// workflowTrigger is a workflow_button's trigger.
func workflowTrigger(e *Element) (url string, inputs json.RawMessage) {
	var w struct {
		Trigger struct {
			URL    string          `json:"url"`
			Inputs json.RawMessage `json:"customizable_input_parameters"`
		} `json:"trigger"`
	}
	if json.Unmarshal(e.Workflow, &w) != nil {
		return "", nil
	}
	return w.Trigger.URL, w.Trigger.Inputs
}
