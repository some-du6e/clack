package core

import (
	"context"
	"encoding/json"
	"net/url"
	"strings"
	"sync"

	"github.com/some-du6e/clack/internal/slack"
)

// The message menu's server calls and the data its rows read: Slack's
// MessageActionsMenu (gantry-v2-async-client-boot-shared 2rcC) and the
// message action thunks of Pult, build 4d0c78085232.

// MarkThreadUnread is "Mark unread" in a thread container (JsXu
// markThreadUnread, thunk tC): setThreadLastRead {lastRead: the reply,
// read: false} (lMb8), which writes "Updates the last_read for a thread"
// (v4te) through markThread (rmwl): subscriptions.thread.mark {channel,
// thread_ts, ts: the reply as clicked, read: 0}. Locally Slack moves the
// thread's read point just before it; the counts follow from the server.
func (a *App) MarkThreadUnread(ctx context.Context, ch, threadTS, ts string) error {
	err := a.C.Call(ctx, "subscriptions.thread.mark", url.Values{"channel": {ch}, "thread_ts": {threadTS}, "ts": {ts}, "read": {"0"}}, nil)
	if err == nil {
		a.refreshCountsSoon()
	}
	return err
}

// BroadcastReply is "Send reply to this channel" (broadcastMessage, thunk
// an): the reply is marked thread_broadcast at once, then chat.update
// {channel, ts, reply_broadcast: true} (34d3 chatUpdateFetcher; the
// generated fetcher snake-cases replyBroadcast); a failure puts the
// message back as it was.
func (a *App) BroadcastReply(ctx context.Context, ch, ts string) error {
	var before slack.Message
	if m, ok := a.S.Mutate(ch, ts, func(m *slack.Message) { before = *m; m.Subtype = "thread_broadcast" }); ok {
		a.emitUpdate(ch, &m)
	}
	err := a.C.Call(ctx, "chat.update", url.Values{"channel": {ch}, "ts": {ts}, "reply_broadcast": {"true"}}, nil)
	if err != nil && before.TS != "" {
		if m, ok := a.S.Mutate(ch, ts, func(m *slack.Message) { m.Subtype, m.ReplyBroadcast = before.Subtype, before.ReplyBroadcast }); ok {
			a.emitUpdate(ch, &m)
		}
	}
	return err
}

// RemoveBroadcast is "Remove reply from channel…" once its dialog says
// "Remove message" (BaseRemoveBroadcastDialog onGo, L16H):
// animateAndDeleteMessageApi {broadcastDelete: true} (bCRN) → chatDeleteApi
// (bhnX): chat.delete {channel, ts, broadcast_delete: true}. The reply
// stays in its thread; the copy in the conversation goes.
func (a *App) RemoveBroadcast(ctx context.Context, ch, ts string) error {
	err := a.C.Call(ctx, "chat.delete", url.Values{"channel": {ch}, "ts": {ts}, "broadcast_delete": {"true"}}, nil)
	if err == nil {
		// what the server's message_changed brings: a plain reply, which
		// the conversation's list no longer shows
		if m, ok := a.S.Mutate(ch, ts, func(m *slack.Message) {
			if m.Subtype == "thread_broadcast" {
				m.Subtype = ""
			}
			m.ReplyBroadcast = false
		}); ok {
			a.emitUpdate(ch, &m)
		}
	}
	return err
}

// Content reporting: the menu's "Report to Slack" (2rcC tH) shows when
// isReportToSlackEnabled (NXFJ), bootData.isContentReportingEnabled,
// which boot fills from client.extras' misc.is_content_reporting_enabled
// (the client.extras handler in boot-render; Slack asks every extra but
// dnd and onboarding, uji1 getEnabledClientExtras). Clack asks only
// "misc" (sNvN's ClientExtras names), after boot.

type extrasState struct {
	mu        sync.Mutex
	reporting bool
}

var extrasIndex sync.Map // *App → *extrasState

func (a *App) extras() *extrasState {
	if v, ok := extrasIndex.Load(a); ok {
		return v.(*extrasState)
	}
	v, _ := extrasIndex.LoadOrStore(a, &extrasState{})
	return v.(*extrasState)
}

// ContentReportingEnabled is isReportToSlackEnabled: client.extras said
// is_content_reporting_enabled (false until it answers).
func (a *App) ContentReportingEnabled() bool {
	x := a.extras()
	x.mu.Lock()
	defer x.mu.Unlock()
	return x.reporting
}

// LoadClientExtras asks client.extras for its misc extra.
func (a *App) LoadClientExtras(ctx context.Context) error {
	var r struct {
		Misc *struct {
			IsContentReportingEnabled *bool `json:"is_content_reporting_enabled"`
		} `json:"misc"`
	}
	if err := a.C.Call(ctx, "client.extras", url.Values{"extras": {"misc"}}, &r); err != nil {
		return err
	}
	if r.Misc != nil && r.Misc.IsContentReportingEnabled != nil {
		x := a.extras()
		x.mu.Lock()
		x.reporting = *r.Misc.IsContentReportingEnabled
		x.mu.Unlock()
	}
	return nil
}

// SetContentReporting sets what client.extras would say (tests).
func (a *App) SetContentReporting(on bool) {
	x := a.extras()
	x.mu.Lock()
	x.reporting = on
	x.mu.Unlock()
}

// Content review: the menu's "Flag this message…" (2rcC ed) shows on an
// Enterprise team or workspace (jDlU isEnterpriseOrEnterpriseWorkspace)
// whose org locks content_review_enabled on (getLockedOrgPref, 8wTz SI),
// and FlagMessageDialog (Pult, over FlagDialogModal 5GOt) renders only
// then (0vTf isContentReviewEnabled). Clack reads the locked prefs from
// enterprise.prefs.get (enterprise_prefs.go).

// ContentReviewEnabled is isContentReviewEnabled: the org's locked
// content_review_enabled pref is truthy.
func (a *App) ContentReviewEnabled() bool {
	v := a.EnterpriseLockedPref("content_review_enabled")
	return len(v) > 0 && string(v) != "false" && string(v) != "null" && string(v) != "0" && string(v) != `""`
}

// EnterpriseTeam is isEnterpriseOrEnterpriseWorkspace for the viewer's
// team: an enterprise itself, or a workspace in one.
func (a *App) EnterpriseTeam() bool {
	return a.C.Creds.EnterpriseID != "" || strings.HasPrefix(a.C.Creds.Team, "E")
}

// EnterpriseID is the viewer's enterprise ("" outside Grid).
func (a *App) EnterpriseID() string { return a.C.Creds.EnterpriseID }

// FlagCustomLink is the org's flag_message_custom_link (FlagMessageDialog's
// "View your organization’s policies."), "" when unset.
func (a *App) FlagCustomLink() string {
	var s string
	_ = json.Unmarshal(a.EnterpriseLockedPref("flag_message_custom_link"), &s)
	return s
}

// FlagContent is FlagMessageDialog's onSubmit (Pult v): the API call
// moderation.flags.create (oe8+ f9) with channel_id, item_id (the
// message's ts), type "message" and the reason; a channel name, topic or
// description message flags the channel's details instead: type
// "channel_details" and channel_details_flagged [detail] ("channel_name",
// "channel_topic", "channel_description", "mpdm_name").
func (a *App) FlagContent(ctx context.Context, ch, ts, detail, reason string) error {
	p := url.Values{"channel_id": {ch}, "item_id": {ts}, "type": {"message"}, "reason": {reason}}
	if detail != "" {
		p.Set("type", "channel_details")
		b, _ := json.Marshal([]string{detail})
		p.Set("channel_details_flagged", string(b))
	}
	return a.C.Call(ctx, "moderation.flags.create", p, nil)
}
