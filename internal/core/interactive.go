package core

// Interactive apps: Block Kit actions, modals (views), App Home, app
// shortcuts, legacy attachment actions and dialogs, and ephemeral messages.
// The UI is internal/ui/blockkit; this file is the service it calls.
//
// Everything mirrors Slack's web client (gantry-v2, 2026-11), read from its
// thunks and fetchers:
//
//   - sendActions → blocks.actions {service_id, app_id, service_team_id,
//     actions: [element payload + block_id], container, client_token,
//     state: {values}}. Containers: message {message_ts, channel_id,
//     is_ephemeral, thread_ts only for ephemerals}, message_attachment
//     {message_ts, attachment_id, channel_id, is_ephemeral}, view {view_id}.
//   - fetchExternalSuggestions → blocks.suggestions (external selects).
//   - callAttachmentAction → chat.attachmentAction {payload, client_token,
//     service_id, app_id, bot_user_id} (legacy attachment buttons/menus).
//   - submitView → views.submit {client_token, view_id, state}; answers
//     {view, response_action, toast_message}, or ok:false with
//     error=validation_failed and errors {block_id: message} (or view_error).
//   - deleteView → views.close {client_token, view_id, root_view_id}.
//   - executeAppAction → apps.actions.v2.execute {action_id, app_id,
//     team_id, context: {channel_id, message_ts}, client_token}; the list
//     is apps.actions.v2.list {resource_id: team, resource_type: team}.
//   - App Home: conversations.info {channel, return_app_home} → app_home +
//     home_view; apps.home.dispatchOpenEvent {id: channel, type: home|messages}.
//   - The modal itself arrives on the RTM socket: view_opened {view,
//     view_id, view_type, previous_view_id, client_token, timeout_range,
//     title, submit, close, app_id, channel_id} (older payloads carry the
//     view's fields flat on the event), then view_updated {view_id, app_id,
//     view}. Like Slack, only views answering a client_token this client
//     issued, within timeout_range, are shown: a click in another client
//     of the same account must not open the modal here too.
//   - dialog_opened {app_id, client_token, dialog_id, dialog_title,
//     dialog_submit_label, timeout_range} → dialog.get / dialog.submit.
//
// Tokens: blocks.actions, views.* and chat.attachmentAction need the
// enterprise token on Enterprise Grid (the team token gets
// team_is_restricted, and slack.Client retries on the enterprise host);
// the RTM socket that carries view_opened is the team one. Live-verified
// on the Hack Club workspace: docs/methods/block-actions-and-modals-2026-09
// on redocumented, plus this client's own tests.
//
// Scraping policy: views, actions and ephemeral messages live in memory
// only; nothing here is written to disk.

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/url"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/some-du6e/yoloslackgoclient/internal/slack"
)

// View is a Block Kit surface (modal, home tab) as Slack sends it.
type View struct {
	ID, RootViewID, PreviousViewID string
	Type                           string // modal | home | workflow_step | …
	AppID, BotID                   string
	TeamID, AppInstalledTeamID     string
	CallbackID, ExternalID, Hash   string
	PrivateMetadata                string
	Title, Submit, Close           json.RawMessage // plain_text objects (nil = none)
	Blocks                         json.RawMessage
	State                          json.RawMessage // {"values": {...}}
	ClearOnClose, NotifyOnClose    bool
	SubmitDisabled                 bool
	// ChannelID is the conversation of a home view (App Home DM).
	ChannelID string
}

// ViewEvent is what Interactive.Subscribe delivers.
type ViewEvent struct {
	// Kind: "opened" (a new modal, or a push when PreviousViewID is the
	// open modal), "updated" (views.update / a newer copy of a view),
	// "home" (an App Home view for ChannelID), "dialog" (a legacy dialog),
	// "ephemeral" (Channel/TS became known as ephemeral: repaint the row),
	// "actions" (the app shortcuts list changed).
	Kind        string
	View        *View
	Dialog      *Dialog
	ClientToken string
	Channel, TS string
}

// Dialog is a legacy dialog.open dialog (dialog.get fills Elements).
type Dialog struct {
	ID, AppID, Title, SubmitLabel string
	CallbackID, State             string
	NotifyOnCancel                bool
	Elements                      json.RawMessage
	ClientToken                   string
}

// ActionContainer is blocks.actions' container.
type ActionContainer struct {
	Type         string // message | message_attachment | view
	ChannelID    string
	MessageTS    string
	ThreadTS     string // sent only for ephemeral messages, like Slack
	IsEphemeral  bool
	AttachmentID int
	ViewID       string
}

// JSON is the container exactly as Slack's client sends it.
func (c ActionContainer) JSON() json.RawMessage {
	var m map[string]any
	switch c.Type {
	case "view":
		m = map[string]any{"type": "view", "view_id": c.ViewID}
	case "message_attachment":
		m = map[string]any{"type": "message_attachment", "message_ts": c.MessageTS, "attachment_id": c.AttachmentID,
			"channel_id": c.ChannelID, "is_ephemeral": c.IsEphemeral}
	default:
		m = map[string]any{"type": "message", "message_ts": c.MessageTS, "channel_id": c.ChannelID, "is_ephemeral": c.IsEphemeral}
		if c.IsEphemeral && c.ThreadTS != "" {
			m["thread_ts"] = c.ThreadTS
		}
	}
	b, _ := json.Marshal(m)
	return b
}

// BlockAction is one blocks.actions call.
type BlockAction struct {
	ServiceID     string // the app's bot id (message bot_id / view bot_id)
	AppID         string
	ServiceTeamID string // the bot's team
	Container     ActionContainer
	// Action is the element payload Slack sends: the element's own
	// fields (type, action_id, text, value, style, …) plus block_id and
	// the selection (selected_option(s), selected_date, …). Snake case.
	Action json.RawMessage
	// State is the container's stateful inputs {block_id: {action_id:
	// {type, value…}}}, or nil.
	State json.RawMessage
	// FunctionExecutionID comes from a message's function_execution_id.
	FunctionExecutionID string
}

// ActionError is a failed interaction with the app's message for the
// user (Slack shows it under the element).
type ActionError struct {
	Code    string // Slack's error code (post_error, invalid_container, …)
	Message string // human text, e.g. "App took too long to respond…"
}

func (e *ActionError) Error() string {
	if e.Message != "" {
		return e.Message
	}
	return e.Code
}

// ActionErrorText is the text Slack's client shows for a failed action.
func ActionErrorText(code string) string {
	switch code {
	case "post_error", "dispatch_failed", "curl_error":
		return "This app is not responding. Please try again or contact the app's developer."
	case "invalid_container", "bot_does_not_own_message":
		return "You can't interact with this message."
	case "action_disabled":
		return "This action has been disabled."
	case "no_app", "invalid_service_id":
		return "This app is no longer installed."
	case "user_auth_required":
		return "You need to authorize this app first."
	case "connected_team_denied", "org_install_no_permission":
		return "You don't have permission to use this app."
	}
	return "Something went wrong. Please try again."
}

// SubmitResult is views.submit's answer.
type SubmitResult struct {
	ResponseAction string            // "", update, push, clear, errors
	View           *View             // the new view for update/push
	Toast          string            // toast_message
	Errors         map[string]string // block_id → message (validation_failed)
	ViewError      string            // a view-level error message
	Code           string            // the error code when the submit failed
}

// AppHome is an app DM's tab configuration.
type AppHome struct {
	AppID, AppInstalledTeamID, ChannelID string
	HomeTab, MessagesTab, MessagesRO     bool
	HomeViewID                           string
}

// AppShortcut is one app shortcut (global or message action).
type AppShortcut struct {
	ActionID, AppID, AppName, TeamID string
	Name, Desc, CallbackID           string
	Type                             string // global_action | message_action | workflow_step_edit
	Icon                             string // app icon (image_32/48)
}

// AppProfile is apps.profile.get's app_profile (the About tab).
type AppProfile struct {
	ID, Name, Desc, LongDesc, Developer string
	URL, SupportURL, PrivacyURL         string
	Icon                                string
	Commands                            []AppCommand
	Raw                                 json.RawMessage
}

// AppCommand is a slash command of an app.
type AppCommand struct{ Name, Desc, Usage string }

// Interactive is the Block Kit interaction service of an App.
type Interactive struct {
	a *App

	mu      sync.Mutex
	subs    map[chan ViewEvent]struct{}
	tokens  map[string]time.Time // client_tokens this client issued
	ephem   map[string]string    // "channel/ts" → thread_ts of ephemeral messages
	homes   map[string]*View     // channel → home view
	views   map[string]*View     // view id → latest copy (modals)
	actions []AppShortcut
	actAt   time.Time
	started atomic.Bool
	stop    func()
	lastTok int64
}

var interactiveIndex sync.Map // *App → *Interactive

// Interactive returns the App's interaction service (created on first
// use; it starts listening for view and ephemeral events right away).
func (a *App) Interactive() *Interactive {
	if v, ok := interactiveIndex.Load(a); ok {
		return v.(*Interactive)
	}
	v, _ := interactiveIndex.LoadOrStore(a, &Interactive{a: a, subs: map[chan ViewEvent]struct{}{},
		tokens: map[string]time.Time{}, ephem: map[string]string{}, homes: map[string]*View{}, views: map[string]*View{}})
	ix := v.(*Interactive)
	ix.start()
	return ix
}

func (ix *Interactive) start() {
	if !ix.started.CompareAndSwap(false, true) {
		return
	}
	ch, stop := ix.a.SubscribeRaw()
	ix.stop = stop
	go func() {
		for ev := range ch {
			ix.handle(ev)
		}
	}()
}

// Subscribe delivers view, dialog and ephemeral events. Call cancel to stop.
func (ix *Interactive) Subscribe() (<-chan ViewEvent, func()) {
	c := make(chan ViewEvent, 64)
	ix.mu.Lock()
	ix.subs[c] = struct{}{}
	ix.mu.Unlock()
	var once sync.Once
	return c, func() {
		once.Do(func() {
			ix.mu.Lock()
			delete(ix.subs, c)
			ix.mu.Unlock()
		})
	}
}

func (ix *Interactive) emit(e ViewEvent) {
	ix.mu.Lock()
	defer ix.mu.Unlock()
	for c := range ix.subs {
		select {
		case c <- e:
		default:
		}
	}
}

// ClientToken issues a fresh client_token ("web-<unix ms>", Slack's
// format) and remembers it for matching view_opened.
func (ix *Interactive) ClientToken() string {
	ms := time.Now().UnixMilli()
	ix.mu.Lock()
	if ms <= ix.lastTok {
		ms = ix.lastTok + 1
	}
	ix.lastTok = ms
	tok := "web-" + strconv.FormatInt(ms, 10)
	ix.tokens[tok] = time.Now()
	if len(ix.tokens) > 32 { // Slack keeps the last 10; keep a few more
		cut := time.Now().Add(-2 * time.Minute)
		for k, t := range ix.tokens {
			if t.Before(cut) {
				delete(ix.tokens, k)
			}
		}
	}
	ix.mu.Unlock()
	return tok
}

// ours reports whether tok is one of ours and still inside timeoutMS.
func (ix *Interactive) ours(tok string, timeoutMS int64) bool {
	ix.mu.Lock()
	t, ok := ix.tokens[tok]
	ix.mu.Unlock()
	if !ok {
		return false
	}
	if timeoutMS <= 0 {
		timeoutMS = 10000
	}
	// Slack measures from the token's timestamp; allow for clock skew on
	// slow links by never cutting below 30 s.
	return time.Since(t) < time.Duration(max(timeoutMS, 30000))*time.Millisecond
}

// ---- realtime ----

func (ix *Interactive) handle(ev slack.Event) {
	switch ev.Type {
	case "message":
		var e struct {
			Subtype     string `json:"subtype"`
			TS          string `json:"ts"`
			ThreadTS    string `json:"thread_ts"`
			Channel     string `json:"channel"`
			IsEphemeral bool   `json:"is_ephemeral"`
			Message     *struct {
				TS          string `json:"ts"`
				ThreadTS    string `json:"thread_ts"`
				IsEphemeral bool   `json:"is_ephemeral"`
			} `json:"message"`
		}
		if json.Unmarshal(ev.Raw, &e) != nil {
			return
		}
		ts, thread, eph := e.TS, e.ThreadTS, e.IsEphemeral
		if e.Message != nil && e.Subtype == "message_changed" {
			ts, thread, eph = e.Message.TS, e.Message.ThreadTS, e.Message.IsEphemeral || eph
		}
		if !eph || ts == "" {
			return
		}
		ch := firstNonEmpty(e.Channel, chanID(ev.Channel))
		ix.mu.Lock()
		ix.ephem[ch+"/"+ts] = thread
		if len(ix.ephem) > 2000 {
			for k := range ix.ephem {
				delete(ix.ephem, k)
				if len(ix.ephem) <= 1500 {
					break
				}
			}
		}
		ix.mu.Unlock()
		ix.emit(ViewEvent{Kind: "ephemeral", Channel: ch, TS: ts})
	case "view_opened", "view_updated":
		ix.onView(ev)
	case "dialog_opened":
		var e struct {
			AppID       string `json:"app_id"`
			ClientToken string `json:"client_token"`
			DialogID    string `json:"dialog_id"`
			Title       string `json:"dialog_title"`
			SubmitLabel string `json:"dialog_submit_label"`
			Timeout     int64  `json:"timeout_range"`
		}
		if json.Unmarshal(ev.Raw, &e) != nil || !ix.ours(e.ClientToken, e.Timeout) {
			return
		}
		ix.emit(ViewEvent{Kind: "dialog", ClientToken: e.ClientToken, Dialog: &Dialog{ID: e.DialogID, AppID: e.AppID,
			Title: e.Title, SubmitLabel: e.SubmitLabel, ClientToken: e.ClientToken}})
	case "app_actions_updated":
		ix.mu.Lock()
		ix.actAt = time.Time{}
		ix.mu.Unlock()
		ix.emit(ViewEvent{Kind: "actions"})
	}
}

// rawView is a view as Slack serializes it.
type rawView struct {
	ID                 string          `json:"id"`
	ViewID             string          `json:"view_id"`
	RootViewID         string          `json:"root_view_id"`
	PreviousViewID     string          `json:"previous_view_id"`
	Type               string          `json:"type"`
	ViewType           string          `json:"view_type"`
	AppID              string          `json:"app_id"`
	BotID              string          `json:"bot_id"`
	TeamID             string          `json:"team_id"`
	AppInstalledTeamID string          `json:"app_installed_team_id"`
	CallbackID         string          `json:"callback_id"`
	ExternalID         string          `json:"external_id"`
	Hash               string          `json:"hash"`
	PrivateMetadata    string          `json:"private_metadata"`
	Title              json.RawMessage `json:"title"`
	Submit             json.RawMessage `json:"submit"`
	Close              json.RawMessage `json:"close"`
	Blocks             json.RawMessage `json:"blocks"`
	State              json.RawMessage `json:"state"`
	ClearOnClose       bool            `json:"clear_on_close"`
	NotifyOnClose      bool            `json:"notify_on_close"`
	SubmitDisabled     bool            `json:"submit_disabled"`
}

func nullRaw(r json.RawMessage) json.RawMessage {
	if len(r) == 0 || string(r) == "null" {
		return nil
	}
	return r
}

func (r *rawView) view() *View {
	v := &View{ID: firstNonEmpty(r.ID, r.ViewID), RootViewID: r.RootViewID, PreviousViewID: r.PreviousViewID,
		Type: firstNonEmpty(r.Type, r.ViewType), AppID: r.AppID, BotID: r.BotID, TeamID: r.TeamID,
		AppInstalledTeamID: r.AppInstalledTeamID, CallbackID: r.CallbackID, ExternalID: r.ExternalID, Hash: r.Hash,
		PrivateMetadata: r.PrivateMetadata, Title: nullRaw(r.Title), Submit: nullRaw(r.Submit), Close: nullRaw(r.Close),
		Blocks: nullRaw(r.Blocks), State: nullRaw(r.State), ClearOnClose: r.ClearOnClose, NotifyOnClose: r.NotifyOnClose,
		SubmitDisabled: r.SubmitDisabled}
	if v.RootViewID == "" {
		v.RootViewID = v.ID
	}
	return v
}

// ParseView decodes a Slack view object.
func ParseView(raw json.RawMessage) (*View, error) {
	var r rawView
	if err := json.Unmarshal(raw, &r); err != nil {
		return nil, err
	}
	if r.ID == "" && r.ViewID == "" {
		return nil, errors.New("view without id")
	}
	return r.view(), nil
}

func (ix *Interactive) onView(ev slack.Event) {
	var e struct {
		rawView
		View         json.RawMessage `json:"view"`
		ClientToken  string          `json:"client_token"`
		TimeoutRange int64           `json:"timeout_range"`
		ChannelID    string          `json:"channel_id"`
	}
	if json.Unmarshal(ev.Raw, &e) != nil {
		return
	}
	var v *View
	if len(nullRaw(e.View)) > 0 {
		if pv, err := ParseView(e.View); err == nil {
			v = pv
		}
	}
	flat := e.rawView.view()
	if v == nil {
		v = flat
	} else {
		// The event's own fields win where the nested view is thinner.
		if v.ID == "" {
			v.ID = flat.ID
		}
		if v.Type == "" {
			v.Type = flat.Type
		}
		if v.PreviousViewID == "" {
			v.PreviousViewID = e.PreviousViewID
		}
		if v.AppID == "" {
			v.AppID = e.AppID
		}
		if v.Title == nil {
			v.Title = flat.Title
		}
		if v.Submit == nil {
			v.Submit = flat.Submit
		}
		if v.Close == nil {
			v.Close = flat.Close
		}
	}
	if v.ID == "" {
		return
	}
	v.ChannelID = e.ChannelID
	switch {
	case ev.Type == "view_updated":
		ix.mu.Lock()
		if v.Type == "home" || ix.homeOf(v.ID) != "" {
			if ch := ix.homeOf(v.ID); ch != "" {
				v.ChannelID = ch
				ix.homes[ch] = v
			}
			ix.mu.Unlock()
			if v.ChannelID != "" {
				ix.emit(ViewEvent{Kind: "home", View: v, Channel: v.ChannelID})
			}
			return
		}
		ix.views[v.ID] = v
		ix.mu.Unlock()
		ix.emit(ViewEvent{Kind: "updated", View: v})
	case v.Type == "home":
		if v.ChannelID == "" {
			return
		}
		ix.mu.Lock()
		ix.homes[v.ChannelID] = v
		ix.mu.Unlock()
		ix.emit(ViewEvent{Kind: "home", View: v, Channel: v.ChannelID})
	default:
		if !ix.ours(e.ClientToken, e.TimeoutRange) {
			return
		}
		ix.mu.Lock()
		ix.views[v.ID] = v
		ix.mu.Unlock()
		ix.emit(ViewEvent{Kind: "opened", View: v, ClientToken: e.ClientToken})
	}
}

// homeOf returns the channel whose home view is id (ix.mu held).
func (ix *Interactive) homeOf(id string) string {
	for ch, v := range ix.homes {
		if v != nil && v.ID == id {
			return ch
		}
	}
	return ""
}

// Ephemeral reports whether the message is an ephemeral one ("Only visible
// to you"). Cheap; safe on the UI goroutine.
func (ix *Interactive) Ephemeral(ch, ts string) bool {
	ix.mu.Lock()
	_, ok := ix.ephem[ch+"/"+ts]
	ix.mu.Unlock()
	return ok
}

// DismissEphemeral removes an ephemeral message locally, as Slack's
// "Dismiss message" does (ephemerals never reach the server's history).
func (ix *Interactive) DismissEphemeral(ch, ts string) {
	ix.mu.Lock()
	delete(ix.ephem, ch+"/"+ts)
	ix.mu.Unlock()
	ix.a.S.Remove(ch, ts)
	ix.a.emit(Event{Type: "delete", Channel: ch, TS: ts})
}

// ---- calls ----

// call runs a Slack method that wants the enterprise token on Grid
// (slack.Client falls back from the team host on team_is_restricted).
func (ix *Interactive) call(ctx context.Context, method string, p url.Values, out any) error {
	return ix.a.C.Call(ctx, method, p, out)
}

// BlockActionSend clicks: sends one block action to the app. The modal
// it may open arrives later as a ViewEvent with the returned token.
func (ix *Interactive) BlockActionSend(ctx context.Context, b BlockAction) (string, error) {
	if b.ServiceID == "" {
		return "", &ActionError{Code: "invalid_service_id", Message: ActionErrorText("invalid_service_id")}
	}
	tok := ix.ClientToken()
	p := url.Values{
		"service_id":   {b.ServiceID},
		"actions":      {"[" + string(b.Action) + "]"},
		"container":    {string(b.Container.JSON())},
		"client_token": {tok},
	}
	if b.AppID != "" {
		p.Set("app_id", b.AppID)
	}
	if b.ServiceTeamID != "" {
		p.Set("service_team_id", b.ServiceTeamID)
	}
	if b.FunctionExecutionID != "" {
		p.Set("function_execution_id", b.FunctionExecutionID)
	}
	if len(b.State) > 0 && string(b.State) != "{}" && string(b.State) != "null" {
		p.Set("state", `{"values":`+string(b.State)+`}`)
	}
	return tok, actionErr(ix.call(ctx, "blocks.actions", p, nil))
}

// actionErr turns a Slack error into an ActionError with Slack's text.
func actionErr(err error) error {
	if err == nil {
		return nil
	}
	var se *slack.Error
	if errors.As(err, &se) {
		ae := &ActionError{Code: se.Code, Message: ActionErrorText(se.Code)}
		// post_error carries the app's or Slack's own text ("App took too
		// long to respond…"); Slack shows that one.
		var body struct {
			Message string `json:"message"`
		}
		if json.Unmarshal(se.Body, &body) == nil && body.Message != "" {
			ae.Message = body.Message
		}
		return ae
	}
	return err
}

// Option is a Block Kit option object (raw) as an app sent it.
type Option = json.RawMessage

// Suggestions asks the app for an external select's options.
func (ix *Interactive) Suggestions(ctx context.Context, b BlockAction, actionID, blockID, value string) (options, groups json.RawMessage, err error) {
	p := url.Values{
		"service_id": {b.ServiceID},
		"container":  {string(b.Container.JSON())},
		"action_id":  {actionID},
		"block_id":   {blockID},
		"value":      {value},
	}
	if b.AppID != "" {
		p.Set("app_id", b.AppID)
	}
	if b.ServiceTeamID != "" {
		p.Set("service_team_id", b.ServiceTeamID)
	}
	if b.FunctionExecutionID != "" {
		p.Set("function_execution_id", b.FunctionExecutionID)
	}
	var res struct {
		Options      json.RawMessage `json:"options"`
		OptionGroups json.RawMessage `json:"option_groups"`
	}
	if err := ix.call(ctx, "blocks.suggestions", p, &res); err != nil {
		return nil, nil, actionErr(err)
	}
	return nullRaw(res.Options), nullRaw(res.OptionGroups), nil
}

// AttachmentAction is one legacy attachment action (chat.attachmentAction).
type AttachmentAction struct {
	ServiceID, AppID, BotUserID, TeamID string
	Action                              json.RawMessage // {id, name, text, type, value, style, selected_options?}
	AttachmentID                        int
	CallbackID                          string
	ChannelID, MessageTS, ThreadTS      string
	IsEphemeral                         bool
}

// AttachmentActionSend sends a legacy attachment button/menu action.
func (ix *Interactive) AttachmentActionSend(ctx context.Context, x AttachmentAction) (string, error) {
	payload := map[string]any{
		"actions": []json.RawMessage{x.Action}, "attachment_id": x.AttachmentID, "callback_id": x.CallbackID,
		"channel_id": x.ChannelID, "is_ephemeral": x.IsEphemeral, "message_ts": x.MessageTS,
		"prompt_app_install": false, "team_id": firstNonEmpty(x.TeamID, ix.teamID()),
	}
	if x.ThreadTS != "" {
		payload["thread_ts"] = x.ThreadTS
	}
	b, err := json.Marshal(payload)
	if err != nil {
		return "", err
	}
	tok := ix.ClientToken()
	p := url.Values{"payload": {string(b)}, "client_token": {tok}, "service_id": {x.ServiceID}}
	if x.AppID != "" {
		p.Set("app_id", x.AppID)
	}
	if x.BotUserID != "" {
		p.Set("bot_user_id", x.BotUserID)
	}
	return tok, actionErr(ix.call(ctx, "chat.attachmentAction", p, nil))
}

func (ix *Interactive) teamID() string {
	ix.a.S.RLock()
	defer ix.a.S.RUnlock()
	return ix.a.S.TeamID
}

// SubmitView submits a modal with its state values ({block_id: {action_id:
// {type, …}}}). A failed validation is a SubmitResult with Errors, not an
// error; err is for transport failures and other Slack errors.
func (ix *Interactive) SubmitView(ctx context.Context, viewID string, values json.RawMessage) (SubmitResult, error) {
	if len(values) == 0 || string(values) == "null" {
		values = json.RawMessage("{}")
	}
	tok := ix.ClientToken()
	p := url.Values{"client_token": {tok}, "view_id": {viewID}, "state": {`{"values":` + string(values) + `}`}}
	raw, err := ix.a.C.Raw(ctx, "views.submit", p)
	var se *slack.Error
	if errors.As(err, &se) && len(se.Body) > 0 {
		raw = se.Body
	}
	var res struct {
		OK             bool              `json:"ok"`
		Error          string            `json:"error"`
		Errors         map[string]string `json:"errors"`
		ViewError      string            `json:"view_error"`
		View           json.RawMessage   `json:"view"`
		ResponseAction string            `json:"response_action"`
		Toast          string            `json:"toast_message"`
	}
	if len(raw) > 0 {
		_ = json.Unmarshal(raw, &res)
	}
	if err != nil && res.Error == "" {
		if se != nil {
			res.Error = se.Code
		} else {
			return SubmitResult{}, err
		}
	}
	out := SubmitResult{ResponseAction: res.ResponseAction, Toast: res.Toast, Errors: res.Errors, ViewError: res.ViewError}
	if !res.OK && res.Error != "" {
		out.Code = res.Error
		if res.Error == "validation_failed" && len(res.Errors) > 0 {
			out.ResponseAction = "errors"
			return out, nil
		}
		if out.ViewError == "" {
			out.ViewError = submitErrorText(res.Error)
		}
		return out, nil
	}
	if v, err := ParseView(res.View); err == nil {
		out.View = v
		ix.mu.Lock()
		ix.views[v.ID] = v
		ix.mu.Unlock()
	}
	return out, nil
}

func submitErrorText(code string) string {
	switch code {
	case "post_error", "dispatch_failed", "curl_error", "timeout":
		return "We had some trouble connecting. Try again?"
	case "view_not_found", "not_found":
		return "This form is no longer available."
	}
	return "Something went wrong. Please try again."
}

// CloseView tells Slack the user closed a view (views.close): the app
// gets view_closed when it asked for notify_on_close. rootViewID closes
// the whole stack.
func (ix *Interactive) CloseView(ctx context.Context, viewID, rootViewID string) error {
	p := url.Values{"client_token": {ix.ClientToken()}, "view_id": {viewID}}
	if rootViewID != "" {
		p.Set("root_view_id", rootViewID)
	}
	ix.mu.Lock()
	delete(ix.views, viewID)
	ix.mu.Unlock()
	return ix.call(ctx, "views.close", p, nil)
}

// GetView fetches a view (views.get), e.g. to reload a modal.
func (ix *Interactive) GetView(ctx context.Context, viewID string) (*View, error) {
	var res struct {
		View json.RawMessage `json:"view"`
	}
	if err := ix.call(ctx, "views.get", url.Values{"view_id": {viewID}}, &res); err != nil {
		return nil, err
	}
	return ParseView(res.View)
}

// ---- legacy dialogs ----

// DialogGet loads a legacy dialog's elements (dialog.get).
func (ix *Interactive) DialogGet(ctx context.Context, d *Dialog) error {
	var res struct {
		Dialog struct {
			CallbackID     string          `json:"callback_id"`
			Title          string          `json:"title"`
			SubmitLabel    string          `json:"submit_label"`
			State          string          `json:"state"`
			NotifyOnCancel bool            `json:"notify_on_cancel"`
			Elements       json.RawMessage `json:"elements"`
		} `json:"dialog"`
	}
	if err := ix.call(ctx, "dialog.get", url.Values{"dialog_id": {d.ID}}, &res); err != nil {
		return err
	}
	d.CallbackID, d.State, d.NotifyOnCancel, d.Elements = res.Dialog.CallbackID, res.Dialog.State, res.Dialog.NotifyOnCancel, res.Dialog.Elements
	if res.Dialog.Title != "" {
		d.Title = res.Dialog.Title
	}
	if res.Dialog.SubmitLabel != "" {
		d.SubmitLabel = res.Dialog.SubmitLabel
	}
	return nil
}

// DialogSubmit submits a legacy dialog; errs are per-element messages.
func (ix *Interactive) DialogSubmit(ctx context.Context, d *Dialog, submission map[string]string) (map[string]string, error) {
	b, _ := json.Marshal(submission)
	raw, err := ix.a.C.Raw(ctx, "dialog.submit", url.Values{"dialog_id": {d.ID}, "submission": {string(b)}, "client_token": {ix.ClientToken()}})
	var se *slack.Error
	if errors.As(err, &se) && len(se.Body) > 0 {
		raw = se.Body
	}
	var res struct {
		Errors []struct{ Name, Error string } `json:"dialog_errors"`
	}
	if len(raw) > 0 {
		_ = json.Unmarshal(raw, &res)
	}
	if len(res.Errors) > 0 {
		m := map[string]string{}
		for _, e := range res.Errors {
			m[e.Name] = e.Error
		}
		return m, nil
	}
	return nil, err
}

// DialogCancel tells the app the dialog was cancelled.
func (ix *Interactive) DialogCancel(ctx context.Context, d *Dialog) error {
	if !d.NotifyOnCancel {
		return nil
	}
	return ix.call(ctx, "dialog.notifyCancel", url.Values{"dialog_id": {d.ID}}, nil)
}

// ---- app shortcuts ----

// Shortcuts lists the workspace's app shortcuts (apps.actions.v2.list),
// cached for ten minutes (app_actions_updated drops the cache).
func (ix *Interactive) Shortcuts(ctx context.Context) ([]AppShortcut, error) {
	ix.mu.Lock()
	if ix.actions != nil && time.Since(ix.actAt) < 10*time.Minute {
		out := ix.actions
		ix.mu.Unlock()
		return out, nil
	}
	ix.mu.Unlock()
	var res struct {
		AppActions []struct {
			AppID   string `json:"app_id"`
			AppName string `json:"app_name"`
			TeamID  string `json:"team_id"`
			Icons   struct {
				Image32 string `json:"image_32"`
				Image48 string `json:"image_48"`
				Image64 string `json:"image_64"`
			} `json:"icons"`
			Actions []struct {
				ActionID   string `json:"action_id"`
				AppID      string `json:"app_id"`
				Name       string `json:"name"`
				Desc       string `json:"desc"`
				CallbackID string `json:"callback_id"`
				Type       string `json:"type"`
			} `json:"actions"`
		} `json:"app_actions"`
	}
	if err := ix.call(ctx, "apps.actions.v2.list", url.Values{"resource_id": {ix.teamID()}, "resource_type": {"team"}}, &res); err != nil {
		return nil, err
	}
	var out []AppShortcut
	for _, a := range res.AppActions {
		icon := firstNonEmpty(a.Icons.Image64, a.Icons.Image48, a.Icons.Image32)
		for _, x := range a.Actions {
			out = append(out, AppShortcut{ActionID: x.ActionID, AppID: firstNonEmpty(x.AppID, a.AppID), AppName: a.AppName,
				TeamID: a.TeamID, Name: x.Name, Desc: x.Desc, CallbackID: x.CallbackID, Type: x.Type, Icon: icon})
		}
	}
	if out == nil {
		out = []AppShortcut{}
	}
	ix.mu.Lock()
	ix.actions, ix.actAt = out, time.Now()
	ix.mu.Unlock()
	return out, nil
}

// RunShortcut runs an app shortcut (apps.actions.v2.execute). Message
// shortcuts pass the message; global ones the current channel only.
// The modal it opens arrives as a ViewEvent carrying the returned token.
func (ix *Interactive) RunShortcut(ctx context.Context, s AppShortcut, channelID, messageTS string) (string, error) {
	tok := ix.ClientToken()
	c := map[string]string{"channel_id": channelID}
	if messageTS != "" {
		c["message_ts"] = messageTS
	}
	cb, _ := json.Marshal(c)
	p := url.Values{"action_id": {s.ActionID}, "app_id": {s.AppID}, "context": {string(cb)}, "client_token": {tok}}
	if s.TeamID != "" {
		p.Set("team_id", s.TeamID)
	}
	return tok, actionErr(ix.call(ctx, "apps.actions.v2.execute", p, nil))
}

// ---- App Home ----

// AppHome reads an app DM's tabs and home view (conversations.info with
// return_app_home, as Slack's client does on opening the DM).
func (ix *Interactive) AppHome(ctx context.Context, ch string) (AppHome, *View, error) {
	var res struct {
		AppHome *struct {
			AppID              string `json:"app_id"`
			AppInstalledTeamID string `json:"app_installed_team_id"`
			ConversationID     string `json:"conversation_id"`
			HomeTab            bool   `json:"home_tab_enabled"`
			MessagesTab        bool   `json:"messages_tab_enabled"`
			MessagesRO         bool   `json:"messages_tab_read_only_enabled"`
			HomeViewID         string `json:"home_view_id"`
		} `json:"app_home"`
		HomeView json.RawMessage `json:"home_view"`
	}
	if err := ix.a.C.Call(ctx, "conversations.info", url.Values{"channel": {ch}, "return_app_home": {"true"}}, &res); err != nil {
		return AppHome{}, nil, err
	}
	if res.AppHome == nil {
		return AppHome{}, nil, nil
	}
	h := AppHome{AppID: res.AppHome.AppID, AppInstalledTeamID: res.AppHome.AppInstalledTeamID, ChannelID: ch,
		HomeTab: res.AppHome.HomeTab, MessagesTab: res.AppHome.MessagesTab, MessagesRO: res.AppHome.MessagesRO,
		HomeViewID: res.AppHome.HomeViewID}
	var v *View
	if pv, err := ParseView(res.HomeView); err == nil {
		pv.ChannelID = ch
		v = pv
		ix.mu.Lock()
		ix.homes[ch] = v
		ix.mu.Unlock()
	}
	return h, v, nil
}

// Home returns the latest known home view of ch (from AppHome or a
// realtime update).
func (ix *Interactive) Home(ch string) *View {
	ix.mu.Lock()
	defer ix.mu.Unlock()
	return ix.homes[ch]
}

// HomeOpened tells the app its Home or Messages tab was opened
// (apps.home.dispatchOpenEvent; the app gets app_home_opened and usually
// publishes a fresh home view, which arrives as a "home" ViewEvent).
func (ix *Interactive) HomeOpened(ctx context.Context, ch, tab, serviceTeamID string) error {
	p := url.Values{"id": {ch}, "type": {tab}}
	if serviceTeamID != "" {
		p.Set("service_team_id", serviceTeamID)
	}
	return ix.call(ctx, "apps.home.dispatchOpenEvent", p, nil)
}

// AppProfileOf reads an app's profile (apps.profile.get) for the About tab.
func (ix *Interactive) AppProfileOf(ctx context.Context, botID, appID string) (AppProfile, error) {
	p := url.Values{}
	if botID != "" {
		p.Set("bot", botID)
	}
	if appID != "" {
		p.Set("app", appID)
	}
	var res struct {
		AppProfile json.RawMessage `json:"app_profile"`
	}
	if err := ix.call(ctx, "apps.profile.get", p, &res); err != nil {
		return AppProfile{}, err
	}
	var r struct {
		ID         string `json:"id"`
		Name       string `json:"name"`
		Desc       string `json:"desc"`
		LongDesc   string `json:"long_desc"`
		Developer  string `json:"developer_name"`
		URL        string `json:"url"`
		SupportURL string `json:"support_url"`
		PrivacyURL string `json:"privacy_policy_url"`
		Commands   map[string]struct {
			Desc  string `json:"desc"`
			Usage string `json:"usage"`
		} `json:"commands"`
	}
	_ = json.Unmarshal(res.AppProfile, &r)
	var icons map[string]any
	var wrap struct {
		Icons map[string]any `json:"icons"`
	}
	if json.Unmarshal(res.AppProfile, &wrap) == nil {
		icons = wrap.Icons
	}
	pr := AppProfile{ID: r.ID, Name: r.Name, Desc: r.Desc, LongDesc: r.LongDesc, Developer: r.Developer, URL: r.URL,
		SupportURL: r.SupportURL, PrivacyURL: r.PrivacyURL, Raw: res.AppProfile}
	for _, k := range []string{"image_192", "image_96", "image_72", "image_64", "image_48"} {
		if s, ok := icons[k].(string); ok && s != "" {
			pr.Icon = s
			break
		}
	}
	for name, c := range r.Commands {
		if !strings.HasPrefix(name, "/") {
			name = "/" + name
		}
		pr.Commands = append(pr.Commands, AppCommand{Name: name, Desc: c.Desc, Usage: c.Usage})
	}
	return pr, nil
}

// ---- helpers for the UI ----

// MessageContainer builds the blocks.actions container and service ids
// of message m (attachment > 0: an attachment's blocks, attachment id).
func (ix *Interactive) MessageContainer(m *Message, attachmentID int) BlockAction {
	b := BlockAction{ServiceID: m.BotID}
	if m.BotProfile != nil {
		b.AppID, b.ServiceTeamID = m.BotProfile.AppID, m.BotProfile.TeamID
		if b.ServiceID == "" {
			b.ServiceID = m.BotProfile.ID
		}
	}
	if b.ServiceTeamID == "" {
		b.ServiceTeamID = ix.teamID()
	}
	eph := ix.Ephemeral(m.Channel, m.TS)
	c := ActionContainer{Type: "message", ChannelID: m.Channel, MessageTS: m.TS, IsEphemeral: eph}
	if eph {
		ix.mu.Lock()
		c.ThreadTS = ix.ephem[m.Channel+"/"+m.TS]
		ix.mu.Unlock()
		if c.ThreadTS == "" {
			c.ThreadTS = m.ThreadTS
		}
	}
	if attachmentID > 0 {
		c.Type, c.AttachmentID = "message_attachment", attachmentID
	}
	b.Container = c
	return b
}

// ViewContainer builds the blocks.actions container of a view.
func (ix *Interactive) ViewContainer(v *View) BlockAction {
	return BlockAction{ServiceID: v.BotID, AppID: v.AppID, ServiceTeamID: firstNonEmpty(v.AppInstalledTeamID, v.TeamID, ix.teamID()),
		Container: ActionContainer{Type: "view", ViewID: v.ID}}
}

// String is for logs (ids only, never contents).
func (c ActionContainer) String() string {
	return fmt.Sprintf("%s ch=%s ts=%s view=%s eph=%v", c.Type, c.ChannelID, c.MessageTS, c.ViewID, c.IsEphemeral)
}
