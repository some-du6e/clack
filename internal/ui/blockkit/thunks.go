package blockkit

// Slack's thunks for interactivity (docs/PLUGINS-taut.md): the UI calls
// them through taut.Dispatch, so a plugin wrapping one sees Slack's
// argument shapes, and the natives are registered for plugins that
// dispatch them themselves.
//
//   sendActions        {serviceId, appId, serviceTeamId, container, actions, functionExecutionId}
//   executeAppAction   {appId, actionId, channelId, messageTs, applicableTeamId, clientToken}
//   submitView         {viewId, viewState: {values}} → {updatedView, responseAction, toastMessage}
//   blocksFormatFetcher {blocks, reason} → blocks.format's answer

import (
	"context"
	"encoding/json"
	"errors"
	"net/url"

	"github.com/some-du6e/yoloslackgoclient/internal/core"
	"github.com/some-du6e/yoloslackgoclient/internal/taut"
)

// sendActionsArgs is Slack's sendActions argument (camelCase; the
// action payloads are already in the app's snake_case).
type sendActionsArgs struct {
	ServiceID           string            `json:"serviceId"`
	AppID               string            `json:"appId,omitempty"`
	ServiceTeamID       string            `json:"serviceTeamId,omitempty"`
	Container           map[string]any    `json:"container"`
	Actions             []json.RawMessage `json:"actions"`
	FunctionExecutionID string            `json:"functionExecutionId,omitempty"`
	State               json.RawMessage   `json:"state,omitempty"`
}

func containerArgs(c core.ActionContainer) map[string]any {
	m := map[string]any{"type": c.Type}
	switch c.Type {
	case "view":
		m["view_id"] = c.ViewID
	default:
		m["messageTs"], m["channelId"], m["isEphemeral"] = c.MessageTS, c.ChannelID, c.IsEphemeral
		if c.Type == "message_attachment" {
			m["attachmentId"] = c.AttachmentID
		}
		if c.IsEphemeral && c.ThreadTS != "" {
			m["threadTs"] = c.ThreadTS
		}
	}
	return m
}

func containerFrom(m map[string]any, fallback core.ActionContainer) core.ActionContainer {
	if m == nil {
		return fallback
	}
	s := func(k string) string { v, _ := m[k].(string); return v }
	c := core.ActionContainer{Type: s("type"), ChannelID: s("channelId"), MessageTS: s("messageTs"), ThreadTS: s("threadTs"),
		ViewID: firstNonEmpty(s("view_id"), s("viewId"))}
	c.IsEphemeral, _ = m["isEphemeral"].(bool)
	if f, ok := m["attachmentId"].(float64); ok {
		c.AttachmentID = int(f)
	} else if i, ok := m["attachmentId"].(int); ok {
		c.AttachmentID = i
	}
	if c.Type == "" {
		return fallback
	}
	return c
}

func firstNonEmpty(s ...string) string {
	for _, x := range s {
		if x != "" {
			return x
		}
	}
	return ""
}

func toBlockAction(a sendActionsArgs, fallback core.ActionContainer) core.BlockAction {
	b := core.BlockAction{ServiceID: a.ServiceID, AppID: a.AppID, ServiceTeamID: a.ServiceTeamID,
		Container: containerFrom(a.Container, fallback), FunctionExecutionID: a.FunctionExecutionID, State: a.State}
	if len(a.Actions) > 0 {
		b.Action = a.Actions[0]
	}
	return b
}

// dispatchSendActions sends a block action through sendActions.
func dispatchSendActions(ctx context.Context, h Host, b core.BlockAction) (string, error) {
	ix := h.Core().Interactive()
	args := sendActionsArgs{ServiceID: b.ServiceID, AppID: b.AppID, ServiceTeamID: b.ServiceTeamID,
		Container: containerArgs(b.Container), Actions: []json.RawMessage{b.Action}, FunctionExecutionID: b.FunctionExecutionID, State: b.State}
	fallback := b.Container
	return taut.Dispatch(ctx, tautFor("sendActions"), "sendActions", args, func(ctx context.Context, a sendActionsArgs) (string, error) {
		return ix.BlockActionSend(ctx, toBlockAction(a, fallback))
	})
}

type executeArgs struct {
	AppID            string `json:"appId"`
	ActionID         string `json:"actionId"`
	ChannelID        string `json:"channelId,omitempty"`
	MessageTS        string `json:"messageTs,omitempty"`
	ApplicableTeamID string `json:"applicableTeamId,omitempty"`
	ClientToken      string `json:"clientToken,omitempty"`
	Reason           string `json:"reason,omitempty"`
}

// dispatchExecute runs an app shortcut through executeAppAction.
func dispatchExecute(ctx context.Context, h Host, s core.AppShortcut, ch, ts string) (string, error) {
	ix := h.Core().Interactive()
	args := executeArgs{AppID: s.AppID, ActionID: s.ActionID, ChannelID: ch, MessageTS: ts, ApplicableTeamID: s.TeamID,
		Reason: "app-global-actions-api/execute-app-action"}
	return taut.Dispatch(ctx, tautFor("executeAppAction"), "executeAppAction", args, func(ctx context.Context, a executeArgs) (string, error) {
		sc := s
		sc.AppID, sc.ActionID, sc.TeamID = firstNonEmpty(a.AppID, s.AppID), firstNonEmpty(a.ActionID, s.ActionID), firstNonEmpty(a.ApplicableTeamID, s.TeamID)
		return ix.RunShortcut(ctx, sc, firstNonEmpty(a.ChannelID, ch), firstNonEmpty(a.MessageTS, ts))
	})
}

type submitArgs struct {
	ViewID    string `json:"viewId"`
	ViewState struct {
		Values json.RawMessage `json:"values"`
	} `json:"viewState"`
}

type submitResult struct {
	UpdatedView    json.RawMessage   `json:"updatedView,omitempty"`
	ResponseAction string            `json:"responseAction,omitempty"`
	ToastMessage   string            `json:"toastMessage,omitempty"`
	Errors         map[string]string `json:"errors,omitempty"`
	ViewError      string            `json:"viewError,omitempty"`
	Code           string            `json:"code,omitempty"`
}

// dispatchSubmit submits a modal through submitView.
func dispatchSubmit(ctx context.Context, h Host, viewID string, values json.RawMessage) (core.SubmitResult, error) {
	ix := h.Core().Interactive()
	var args submitArgs
	args.ViewID = viewID
	args.ViewState.Values = values
	t := tautFor("submitView")
	if t == nil {
		return ix.SubmitView(ctx, viewID, values)
	}
	var native core.SubmitResult
	r, err := taut.Dispatch(ctx, t, "submitView", args, func(ctx context.Context, a submitArgs) (submitResult, error) {
		res, err := ix.SubmitView(ctx, firstNonEmpty(a.ViewID, viewID), a.ViewState.Values)
		native = res
		return toSubmitResult(res), err
	})
	if err != nil {
		return core.SubmitResult{}, err
	}
	out := native
	out.ResponseAction, out.Toast = r.ResponseAction, r.ToastMessage
	if r.Errors != nil {
		out.Errors = r.Errors
	}
	if len(r.UpdatedView) > 0 && string(r.UpdatedView) != "null" {
		if v, err := core.ParseView(r.UpdatedView); err == nil {
			out.View = v
		}
	}
	return out, nil
}

func toSubmitResult(r core.SubmitResult) submitResult {
	out := submitResult{ResponseAction: r.ResponseAction, ToastMessage: r.Toast, Errors: r.Errors, ViewError: r.ViewError, Code: r.Code}
	if r.View != nil {
		out.UpdatedView = viewJSON(r.View)
	}
	return out
}

// viewJSON is a view in Slack's shape (for plugins).
func viewJSON(v *core.View) json.RawMessage {
	m := map[string]any{"id": v.ID, "root_view_id": v.RootViewID, "previous_view_id": nullable(v.PreviousViewID), "type": v.Type,
		"app_id": v.AppID, "bot_id": v.BotID, "team_id": v.TeamID, "app_installed_team_id": v.AppInstalledTeamID,
		"callback_id": v.CallbackID, "external_id": v.ExternalID, "hash": v.Hash, "private_metadata": v.PrivateMetadata,
		"title": rawOrNil(v.Title), "submit": rawOrNil(v.Submit), "close": rawOrNil(v.Close), "blocks": rawOrNil(v.Blocks),
		"state": rawOrNil(v.State), "clear_on_close": v.ClearOnClose, "notify_on_close": v.NotifyOnClose}
	b, _ := json.Marshal(m)
	return b
}

func nullable(s string) any {
	if s == "" {
		return nil
	}
	return s
}

func rawOrNil(r json.RawMessage) any {
	if len(r) == 0 {
		return nil
	}
	return r
}

// registerThunks gives plugins Clack's natives for the thunks blockkit owns.
func registerThunks(t *taut.Taut) {
	app := t.App()
	if app == nil {
		return
	}
	ix := app.Interactive()
	arg := func(raw []json.RawMessage, v any) error {
		if len(raw) == 0 {
			return errors.New("missing arguments")
		}
		return json.Unmarshal(raw[0], v)
	}
	t.RegisterThunk("sendActions", func(ctx context.Context, raw []json.RawMessage) (any, error) {
		var a sendActionsArgs
		if err := arg(raw, &a); err != nil {
			return nil, err
		}
		_, err := ix.BlockActionSend(ctx, toBlockAction(a, core.ActionContainer{Type: "message"}))
		return map[string]any{"ok": err == nil}, err
	})
	t.RegisterThunk("executeAppAction", func(ctx context.Context, raw []json.RawMessage) (any, error) {
		var a executeArgs
		if err := arg(raw, &a); err != nil {
			return nil, err
		}
		_, err := ix.RunShortcut(ctx, core.AppShortcut{AppID: a.AppID, ActionID: a.ActionID, TeamID: a.ApplicableTeamID}, a.ChannelID, a.MessageTS)
		return map[string]any{"ok": err == nil}, err
	})
	t.RegisterThunk("submitView", func(ctx context.Context, raw []json.RawMessage) (any, error) {
		var a submitArgs
		if err := arg(raw, &a); err != nil {
			return nil, err
		}
		res, err := ix.SubmitView(ctx, a.ViewID, a.ViewState.Values)
		if err != nil {
			return nil, err
		}
		return toSubmitResult(res), nil
	})
	t.RegisterThunk("blocksFormatFetcher", func(ctx context.Context, raw []json.RawMessage) (any, error) {
		var a struct {
			Blocks json.RawMessage `json:"blocks"`
			Reason string          `json:"reason"`
		}
		if err := arg(raw, &a); err != nil {
			return nil, err
		}
		msg, _ := json.Marshal(map[string]json.RawMessage{"blocks": a.Blocks})
		return app.C.Raw(ctx, "blocks.format", url.Values{"message": {string(msg)}})
	})
}
