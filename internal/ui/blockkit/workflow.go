package blockkit

import (
	"context"
	"encoding/json"
	"errors"
	"image"
	"strings"
	"time"

	"gioui.org/io/pointer"
	"gioui.org/op/clip"
	"gioui.org/widget"

	"github.com/some-du6e/clack/internal/core"
	"github.com/some-du6e/clack/internal/taut"
	"github.com/some-du6e/clack/internal/ui/popups"
	"github.com/some-du6e/clack/internal/ui/theme"
)

// Workflows run the way Slack's web client runs them (build a84662d2d135;
// docs/WORKFLOWS.md): every link trigger, whether a workflow link in a
// message, a workflow_button, a bookmarked workflow or one started from
// channel details, goes through workflowsTriggersTrip (0/Om):
// workflows.triggers.trip with run_precheck, Slack's toasts for the answer
// (Izws), and for pretrip_ack_required the speedbump Slack would open
// (WorkflowTriggerSpeedbumpModal, chunk workflow-trigger-speedbump-modal).
// The forms a workflow opens arrive as view_opened with the trip's
// client_token and open as Block Kit modals (modal.go).

// IsWorkflowLink reports whether u is a workflow link trigger.
func IsWorkflowLink(u string) bool { return core.WorkflowTriggerID(u) != "" }

// OpenWorkflowLink starts the workflow behind link (from a message in
// ch/ts, or "" elsewhere). It reports whether link was a workflow link.
func OpenWorkflowLink(h Host, link, ch, ts string) bool {
	if !IsWorkflowLink(link) {
		return false
	}
	TripWorkflow(h, link, core.MessageTripContext(ch, ts), "")
	return true
}

// TripWorkflow runs a link trigger from tc (Slack's workflowsTriggersTrip).
// uiPage names the surface for the speedbump's rules ("" for messages;
// "channel_workflows", "channel_details"…).
func TripWorkflow(h Host, link string, tc core.TripContext, uiPage string) {
	reason := "trip-trigger-from-shortcut-ui" // onExecute (l4L4): links, lists' Start buttons
	if tc.Location() == "channel_details" {
		reason = "trip-trigger-from-channel-details-integrations-click"
	}
	tripVia(h, link, tc, uiPage, reason)
}

// tripArgs is workflowsTriggersTrip's argument as Slack's callers pass it
// (camelCase: its API fetcher snake_cases the context).
type tripArgs struct {
	LinkTriggerURL               string         `json:"linkTriggerUrl"`
	WorkflowLinkTriggerUIContext map[string]any `json:"workflowLinkTriggerUIContext,omitempty"`
	Reason                       string         `json:"reason,omitempty"`
	UIPage                       string         `json:"uiPage,omitempty"`
}

// tripVia dispatches workflowsTriggersTrip (through Taut when a plugin
// patches it, docs/PLUGINS-taut.md): the trip, then its toast or
// speedbump, all inside the thunk as in Slack, so a plugin that swallows
// it leaves nothing behind.
func tripVia(h Host, link string, tc core.TripContext, uiPage, reason string) {
	if core.WorkflowTriggerID(link) == "" {
		// "Missing or invalid linkTriggerUrl": the error toast
		h.Toast(core.TripErrorText("", ""))
		return
	}
	appFor(h) // forms the workflow opens arrive as views
	ix := h.Core().Interactive()
	args := tripArgs{LinkTriggerURL: link, WorkflowLinkTriggerUIContext: camelKeys(tc), Reason: reason, UIPage: uiPage}
	go func() {
		ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
		defer cancel()
		_, _ = taut.Dispatch(ctx, tautFor("workflowsTriggersTrip"), "workflowsTriggersTrip", args, func(ctx context.Context, a tripArgs) (struct{}, error) {
			link, tc := a.LinkTriggerURL, core.TripContext(snakeKeys(a.WorkflowLinkTriggerUIContext))
			if core.WorkflowTriggerID(link) == "" {
				post(h, func() { h.Toast(core.TripErrorText("", "")) })
				return struct{}{}, nil
			}
			res, err := ix.TripTrigger(ctx, link, tc, true)
			post(h, func() { tripAnswered(h, link, tc, a.UIPage, res, err) })
			return struct{}{}, nil
		})
	}()
}

// tripAnswered is the first trip's answer: the speedbump Slack's
// shouldShowPrecheckSpeedbump asks for, else the toast.
func tripAnswered(h Host, link string, tc core.TripContext, uiPage string, res core.TripResult, err error) {
	var te *core.TripError
	if errors.As(err, &te) && te.Code == "pretrip_ack_required" && te.PreTrip.NeedsSpeedbump(tc.Location(), uiPage) {
		page := uiPage
		if page == "" {
			page = tc.Location()
		}
		openSpeedbump(h, link, tc, page, te.PreTrip)
		return
	}
	tripDone(h, core.WorkflowTriggerID(link), res, err)
}

// camelKeys and snakeKeys convert a trip context's top-level keys between
// the thunk's camelCase and the API's snake_case (Slack's snakeCaseDeep;
// the values here are strings or the {name, value} inputs, whose keys
// are the same either way).
func camelKeys(m map[string]any) map[string]any {
	out := make(map[string]any, len(m))
	for k, v := range m {
		parts := strings.Split(k, "_")
		for i := 1; i < len(parts); i++ {
			if parts[i] != "" {
				parts[i] = strings.ToUpper(parts[i][:1]) + parts[i][1:]
			}
		}
		out[strings.Join(parts, "")] = v
	}
	return out
}

func snakeKeys(m map[string]any) map[string]any {
	out := make(map[string]any, len(m))
	for k, v := range m {
		var b strings.Builder
		for i, r := range k {
			if r >= 'A' && r <= 'Z' {
				if i > 0 {
					b.WriteByte('_')
				}
				r += 'a' - 'A'
			}
			b.WriteRune(r)
		}
		out[b.String()] = v
	}
	return out
}

// tripWorkflowButton runs a workflow_button's trigger with Slack's button
// context (WorkflowButtonElement): never blocks.actions.
func tripWorkflowButton(h Host, link string, inputs json.RawMessage, actionID, blockID, ch, ts string) {
	eph := ch != "" && ts != "" && h.Core().Interactive().Ephemeral(ch, ts)
	tripVia(h, link, core.ButtonTripContext(actionID, blockID, ch, ts, eph, inputs), "", "trip-trigger-from-bk-workflow-button")
}

// tripAgain is the speedbump's Start Workflow: the same trip without
// run_precheck (Slack's tripTrigger()).
func tripAgain(h Host, link string, tc core.TripContext) {
	id := core.WorkflowTriggerID(link)
	ix := h.Core().Interactive()
	run(h, 20*time.Second, func(ctx context.Context) (core.TripResult, error) {
		return ix.TripTrigger(ctx, link, tc, false)
	}, func(res core.TripResult, err error) { tripDone(h, id, res, err) })
}

// tripDone shows Slack's toast for a trip's answer (Izws): the error
// toast's text, else, unless Slack's workflow execution window shows the
// run instead (workflow_execution_window), "You successfully started a
// workflow" or, for a slow workflow, the longer-to-run toast.
func tripDone(h Host, id string, res core.TripResult, err error) {
	if err != nil {
		code, sub := "", ""
		var te *core.TripError
		if errors.As(err, &te) {
			code, sub = te.Code, te.SubType
		}
		h.Toast(core.TripErrorText(code, sub))
		return
	}
	c := h.Core()
	if c.Experiment("workflow_execution_window") == "on" {
		return
	}
	switch {
	case !res.Slow:
		h.Toast("You successfully started a workflow")
	case res.AISteps && c.Experiment("enable_wfb_ai_answer_step") == "on":
		h.Toast("You started a workflow. This workflow has an AI step and may take longer to run.")
	default:
		if t, ok := c.Interactive().Trigger(id); ok && t.Name != "" {
			h.Toast("You started " + t.Name + ". This workflow might take a little longer to run.")
			return
		}
		h.Toast("You started a workflow. This workflow might take a little longer to run.")
	}
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

// ---- the speedbump ----

// openSpeedbump opens the speedbump Slack's WorkflowTriggerSpeedbumpModal
// picks for pre: "Review workflow access" when the workflow has risky
// scopes, else the default one (the workflow's name, Start Workflow).
// The others (connect accounts, missing inputs, AI access, several at
// once) need Slack's OAuth window and conversation, canvas and user
// pickers, which Clack doesn't have yet: the link opens in Slack's web
// client, which trips it and shows them.
func openSpeedbump(h Host, link string, tc core.TripContext, uiPage string, pre *core.PreTripInfo) {
	auth := pre.ThirdPartyAuths.IsShown
	inputs := pre.MissingContextParameters.IsShown || pre.MissingCustomizableInputParameters.IsShown
	scopes := pre.ScopesAcknowledgements.IsShown
	ai := pre.ContainsAISteps && len(pre.RequiredContextParameters) > 0
	if auth || inputs || ai {
		openURL(link)
		return
	}
	id := core.WorkflowTriggerID(link)
	ix := h.Core().Interactive()
	sb := &speedbump{h: h, id: id}
	if scopes {
		sb.scopes = pre.ScopesAcknowledgements.BotScopes
		if len(sb.scopes) > 0 {
			// Slack lists the first three that have a description
			var keep []core.BotScope
			for _, s := range sb.scopes {
				if s.Description != "" && len(keep) < 3 {
					keep = append(keep, s)
				}
			}
			sb.scopes = keep
		}
	}
	title := func() string {
		if scopes {
			return "Review workflow access"
		}
		t, _ := ix.Trigger(id)
		return t.Title()
	}
	start := &popups.ModalButton{Label: "Start Workflow", Icon: "play", Kind: popups.BtnPrimary, Do: func() bool {
		tripAgain(h, link, tc)
		return true
	}}
	m := popups.NewModal(h, popups.ModalOpts{TitleFunc: title, Body: sb.layout, AutoFocus: start,
		Class: "p-workflow_trigger_speedbump_modal", Buttons: []*popups.ModalButton{{Label: "Cancel", Kind: popups.BtnOutline}, start}})
	sb.modal = m
	h.ShowOverlay(m)
	if speedbumpShown != nil {
		speedbumpShown(sb, start)
	}
	if _, ok := ix.Trigger(id); !ok || scopes {
		// the store's trigger (fetched with its workflow details) names
		// the modal and holds the share URL "Copy workflow link" copies;
		// the scopes subtitle names a workspace that isn't the viewer's
		c := h.Core()
		run(h, 15*time.Second, func(ctx context.Context) (core.WorkflowTrigger, error) {
			t, ok := ix.Trigger(id)
			var err error
			if !ok {
				t, err = ix.TriggerInfo(ctx, id)
			}
			if scopes && t.OwningTeam != "" {
				c.EnsureTeam(ctx, t.OwningTeam)
			}
			return t, err
		}, func(core.WorkflowTrigger, error) { h.Invalidate() })
	}
}

// speedbumpShown, when set (tests), sees each speedbump and its Start
// Workflow button.
var speedbumpShown func(sb *speedbump, start *popups.ModalButton)

// speedbump is the body of a speedbump modal: the scopes list ("What
// will the workflow be able to do?") and, right-aligned above the
// footer's buttons, "Copy workflow link" when the trigger has a share
// URL (.p-workflow_trigger_speedbump_modal_footer: a column, the link
// 8px above Cancel / Start Workflow).
type speedbump struct {
	h      Host
	id     string
	scopes []core.BotScope
	copy   widget.Clickable
	modal  *popups.Modal
}

func (s *speedbump) layout(gtx C) D {
	th := s.h.Theme()
	cl := colorsOf(th)
	w := gtx.Constraints.Max.X
	y := 0
	if s.scopes != nil {
		// .p-scopes_modal__subtitle: 15/700, 4px below; the list 8px
		// below that, items 4px apart, each a 20px icon (do: pencil,
		// view: eye) 8px before its description; 20px under the list
		at(gtx, image.Pt(0, y), func(gtx C) D {
			gtx.Constraints.Max.X = w
			return para(gtx, th, 15, 22, theme.Bold, cl.Text, s.subtitle())
		})
		y += dp(gtx, 22) + dp(gtx, 4) + dp(gtx, 8)
		for _, sc := range s.scopes {
			name := ""
			switch sc.ActionType {
			case "do":
				name = "edit"
			case "view":
				name = "eye"
			}
			rowH := dp(gtx, 22)
			x := 0
			if name != "" {
				at(gtx, image.Pt(0, y+(rowH-dp(gtx, 20))/2), func(gtx C) D { return icon(gtx, name, 20, cl.Text) })
				x = dp(gtx, 20) + dp(gtx, 8)
			}
			d := at(gtx, image.Pt(x, y), func(gtx C) D {
				gtx.Constraints.Max.X = w - x
				return para(gtx, th, 15, 22, theme.Regular, cl.Text, sc.Description)
			})
			y += max(d.Size.Y, rowH) + dp(gtx, 4)
		}
		y += dp(gtx, 20)
	}
	t, _ := s.h.Core().Interactive().Trigger(s.id)
	if t.ShareURL == "" {
		return D{Size: image.Pt(w, y)}
	}
	if s.copy.Clicked(gtx) {
		copyWorkflowLink(s.h, t.ShareURL)
	}
	lw := measure(gtx, th, 15, theme.Regular, "Copy workflow link")
	lh := dp(gtx, 22)
	at(gtx, image.Pt(w-lw, y), func(gtx C) D {
		return s.copy.Layout(gtx, func(gtx C) D {
			d := label(gtx, th, 15, theme.Regular, cl.Link, "Copy workflow link")
			if s.copy.Hovered() {
				rrect(gtx, image.Rect(0, d.Size.Y-dp(gtx, 2), d.Size.X, d.Size.Y-dp(gtx, 1)), 0, cl.Link)
			}
			defer clip.Rect{Max: d.Size}.Push(gtx.Ops).Pop()
			pointer.CursorPointer.Add(gtx.Ops)
			return d
		})
	})
	return D{Size: image.Pt(w, y+lh+dp(gtx, 8))}
}

// subtitle is ScopesModal's: the owning workspace is named when it isn't
// the viewer's.
func (s *speedbump) subtitle() string {
	ix := s.h.Core().Interactive()
	t, _ := ix.Trigger(s.id)
	if name := ix.OwningTeamName(t); name != "" {
		return "What will the workflow be able to do in " + name + "?"
	}
	return "What will the workflow be able to do?"
}

// copyWorkflowLink is Slack's copyWorkflowUrl: the share URL on the
// clipboard, then "Workflow link copied".
func copyWorkflowLink(h Host, shareURL string) {
	if c, ok := h.(interface{ CopyText(string) }); ok {
		c.CopyText(shareURL)
		h.Toast("Workflow link copied")
	}
}
