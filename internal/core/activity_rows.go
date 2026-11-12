package core

// The Activity rows' own actions, as the gantry build's rows call them
// (client-boot-render, 2026-11; docs/ACTIVITY.md "Row actions"):
//
//   - Slack Connect invites (Qt0R ActivityRowContentInviteActions → 3Oc+
//     SlackConnectInviteActions, v+N2 FI): Ignore opens the forked modal
//     (rPCk); "Ignore this invitation" calls conversations.ignoreInvite
//     (channel invites) or sharedInvites.ignoreInvite (DMs), "Block
//     invitations" conversations.ignoreUser / sharedInvites.ignoreUser.
//     Accept on a DM invite (SlackConnectAcceptDmInviteAction) checks
//     sharedInvites.canAccept {sig}, then sharedInvites.accept {sig,
//     invite} (straight away for a verified organisation, else after the
//     trust modal).
//   - Reminder rows (xSv9 ActivityItemMarkCompleteButton → CCch
//     onToggleComplete; Pck2 ActivityItemRescheduleButton → SP97
//     RemindMenu): saved.update with mark/todo_state (mheF
//     toggle*Complete) or date_due (mheF updateSaved*). The saved item
//     itself comes from saved.get (YeRq getMaybeUnknownSavedItemById →
//     2ar2 fetchSavedReminders, modern.vendor savedGetFetcher).
//   - "Notifications cleared." (ActivityClearAllToast): Undo is
//     activity.clearAll {undo_key} (Wy3r activityClearAllFetcher).
//   - Saved views (9Gp updateActivityViewsByApi): a view other than All
//     saves its density with activity.views.update {view_id, sort,
//     density}.

import (
	"context"
	"encoding/json"
	"net/url"
	"strconv"
	"time"

	"github.com/some-du6e/clack/internal/slack"
)

// ---- Slack Connect invites ----

// Invite overall states (v+N2 ln).
const (
	InviteDefault   = "default"
	InvitePending   = "pending"
	InviteAccepted  = "accepted"
	InviteInactive  = "inactive"
	InviteRejected  = "rejected"
	InviteDenied    = "denied"
	InviteRequested = "requested"
)

// InviteState is getInviteOverallStatus for the current user's acceptance
// (uOF7 picks the user's own; Clack's first is the same for a received
// invite, which only the recipient accepts).
func (ci *ActivityInvite) InviteState(self string, now time.Time) string {
	st := InviteDefault
	if ci.Status == "requested" {
		st = InviteRequested
	}
	var acc *InviteAcceptance
	for i := range ci.Acceptances {
		if ci.Acceptances[i].User == self || acc == nil {
			acc = &ci.Acceptances[i]
		}
	}
	switch {
	case acc != nil:
		switch acc.Status {
		case "rejected":
			st = InviteRejected
		case "approved":
			st = InviteAccepted
		case "expired":
			st = InviteInactive
		case "pending_approval":
			st = InvitePending
		}
	case ci.expired(now) || ci.Status == "revoked":
		st = InviteInactive
	}
	return st
}

func (ci *ActivityInvite) expired(now time.Time) bool {
	n := now.Unix()
	return ci.Status == "expired" || (ci.Status == "sent" || ci.Status == "requested") && ci.DateInvalid != 0 && ci.DateInvalid < n
}

// Connect invite actions (v+N2 Eo), in the order FI lists them.
const (
	InviteActIgnore        = "ignore-invite"
	InviteActAcceptDM      = "accept-dm"
	InviteActAcceptChannel = "accept-channel"
)

// InviteActions is FI for a received invite: Ignore while it's open
// (a channel invite also once pending or accepted, when it can be
// accepted again: not by you yet), then Accept for a DM or a channel
// invite with a signature. Outgoing invites' Copy link / Revoke aren't in
// the Activity inbox (it only lists invites you received).
func (ci *ActivityInvite) InviteActions(self string, now time.Time) []string {
	if ci == nil || ci.Direction != "incoming" {
		return nil
	}
	st := ci.InviteState(self, now)
	reaccept := false
	if ci.Kind == "channel" {
		reaccept = true
		for _, a := range ci.Acceptances {
			if a.User == self {
				reaccept = false
			}
		}
	}
	var out []string
	switch {
	case ci.Kind == "im" && st == InviteDefault,
		ci.Kind == "channel" && (st == InviteDefault || (st == InvitePending || st == InviteAccepted) && reaccept):
		out = append(out, InviteActIgnore)
	}
	if ci.Kind == "im" && st == InviteDefault && ci.Sig != "" {
		out = append(out, InviteActAcceptDM)
	}
	if ci.Kind == "channel" && (st == InviteDefault || (st == InvitePending || st == InviteAccepted) && reaccept) && ci.Sig != "" {
		out = append(out, InviteActAcceptChannel)
	}
	return out
}

// IgnoreConnectInvite is IgnoreThisModal's "Ignore Invitation":
// conversations.ignoreInvite for a channel invite, sharedInvites.ignoreInvite
// for a DM (rPCk: yid0 ZV / JSmX yK, reason ignore_this_modal__ignore).
func (a *App) IgnoreConnectInvite(ctx context.Context, inviteID string, channel bool) error {
	method := "sharedInvites.ignoreInvite"
	if channel {
		method = "conversations.ignoreInvite"
	}
	return a.C.Call(ctx, method, url.Values{"invite": {inviteID}, "reason": {"ignore_this_modal__ignore"}}, nil)
}

// BlockConnectInvites is IgnoreAllModal's "Block Invitations":
// conversations.ignoreUser for a channel invite, sharedInvites.ignoreUser
// for a DM (hCGE: NIaL Lf / QiAE o0, reason ignore_all_modal__ignore).
func (a *App) BlockConnectInvites(ctx context.Context, inviteID string, channel bool) error {
	method := "sharedInvites.ignoreUser"
	if channel {
		method = "conversations.ignoreUser"
	}
	return a.C.Call(ctx, method, url.Values{"invite": {inviteID}, "reason": {"ignore_all_modal__ignore"}}, nil)
}

// ConnectDMInviteEligible is fetchAndUpsertSCDMAcceptanceEligibility:
// sharedInvites.canAccept {sig} (57h3 sharedInvitesCanAcceptFetcher).
func (a *App) ConnectDMInviteEligible(ctx context.Context, sig string) (bool, error) {
	var r struct {
		IsEligible bool `json:"is_eligible"`
	}
	err := a.C.Call(ctx, "sharedInvites.canAccept", url.Values{"sig": {sig}, "reason": {"fetch-and-upsert-scdm-acceptance-eligibility"}}, &r)
	return r.IsEligible, err
}

// AcceptConnectDMInvite is sharedInvites.accept {sig, invite} (Suyz
// sharedInvitesAcceptFetcher, reason slack-connect-accept-dm-invite).
func (a *App) AcceptConnectDMInvite(ctx context.Context, sig, inviteID string) error {
	return a.C.Call(ctx, "sharedInvites.accept", url.Values{"sig": {sig}, "invite": {inviteID}, "reason": {"slack-connect-accept-dm-invite"}}, nil)
}

// ---- reminders (saved items) ----

// SavedKey names a saved item the way Slack's compound key does
// (WNA1 Cg: item id, type, ts, detail).
type SavedKey struct {
	ItemID, ItemType, TS, Detail string
}

// ReminderKey is Gjwd qy: the saved item a saved_reminder entry is about
// (ok false for a payload Slack doesn't key).
func ReminderKey(it *ActivityItem) (SavedKey, bool) {
	if it == nil || it.Type != "saved_reminder" || it.ReminderID == "" {
		return SavedKey{}, false
	}
	k := SavedKey{ItemID: it.ReminderID, ItemType: it.ReminderItem}
	switch it.ReminderItem {
	case "file", "reminder":
	case "message", "list_record":
		k.TS = it.ReminderTS
	case "canvas_section":
		k.Detail = it.ReminderDetail
	case "list_record_field":
		k.TS, k.Detail = it.ReminderTS, it.ReminderDetail
	default:
		return SavedKey{}, false
	}
	return k, true
}

// SavedGet is saved.get {items}: the saved items for keys (fetchSavedReminders;
// the fetcher snake-cases each key). Items Slack doesn't return are
// left out (Slack marks them isNonExistent).
func (a *App) SavedGet(ctx context.Context, keys []SavedKey) ([]SavedItem, error) {
	type item struct {
		ItemID     string `json:"item_id"`
		ItemType   string `json:"item_type"`
		TS         string `json:"ts"`
		ItemDetail string `json:"item_detail"`
	}
	in := make([]item, 0, len(keys))
	for _, k := range keys {
		in = append(in, item{k.ItemID, k.ItemType, k.TS, k.Detail})
	}
	b, _ := json.Marshal(in)
	var r struct {
		SavedItems []savedRaw `json:"saved_items"`
	}
	if err := a.C.Call(ctx, "saved.get", url.Values{"items": {string(b)}, "reason": {"get-saved-items"}}, &r); err != nil {
		return nil, err
	}
	out := make([]SavedItem, 0, len(r.SavedItems))
	for i := range r.SavedItems {
		out = append(out, savedItemOf(&r.SavedItems[i]))
	}
	return out, nil
}

// Completed is qb0V sN: a saved item that's done.
func (s SavedItem) Completed() bool {
	return s.TodoState == "completed" || s.State == "completed" || s.Progress == "completed"
}

func savedItemParams(s SavedItem) url.Values {
	p := url.Values{"item_type": {s.ItemType}, "item_id": {s.ItemID}}
	switch s.ItemType {
	case "message", "list_record", "list_record_field":
		if s.TS != "" {
			p.Set("ts", s.TS)
		}
	}
	return p
}

// CompleteSavedItem is onToggleComplete on an item that isn't done:
// saved.update {mark: completed, todo_state: completed, date_due: 0}
// (mheF toggleSavedMessageComplete / toggleReminderComplete /
// toggleSavedFileComplete; a completed item's due date is cleared).
func (a *App) CompleteSavedItem(ctx context.Context, s SavedItem) error {
	p := savedItemParams(s)
	p.Set("mark", "completed")
	p.Set("todo_state", "completed")
	p.Set("date_due", "0")
	p.Set("reason", "manually_mark_completed")
	if err := a.C.Call(ctx, "saved.update", p, nil); err != nil {
		return err
	}
	if ch := firstNonEmpty(s.Channel, s.ItemID); s.ItemType == "message" && ch != "" { // the row's metadata (saved_meta.go)
		a.patchSavedMeta(ch, s.TS, func(m *SavedMeta) { m.TodoState, m.DateCompleted, m.DateDue = "completed", time.Now().Unix(), 0 })
	}
	return nil
}

// SetSavedItemDue is DateDueMenuItem / RemindMenu's Clear: saved.update
// {date_due} (mheF updateSavedMessage / updateSavedFile /
// updateSavedReminder). due 0 clears the reminder.
func (a *App) SetSavedItemDue(ctx context.Context, s SavedItem, due int64) error {
	p := savedItemParams(s)
	p.Set("date_due", strconv.FormatInt(due, 10))
	if err := a.C.Call(ctx, "saved.update", p, nil); err != nil {
		return err
	}
	if ch := firstNonEmpty(s.Channel, s.ItemID); s.ItemType == "message" && ch != "" {
		a.patchSavedMeta(ch, s.TS, func(m *SavedMeta) { m.DateDue = due })
	}
	return nil
}

// ---- clear all, saved views, @channel ----

// UndoClearAll is ActivityClearAllToast's Undo: activity.clearAll
// {undo_key} (reason undo-clear-all).
func (a *App) UndoClearAll(ctx context.Context, undoKey string) error {
	return a.C.Call(ctx, "activity.clearAll", url.Values{"undo_key": {undoKey}, "reason": {"undo-clear-all"}}, nil)
}

// ActivityView is one of activity.views' saved views: a default one
// (view_type all, dms, mentions or threads) or a custom one (custom),
// with its name, emoji (":tada:"), position, sort, density and filters
// (activity_views.go).
type ActivityView struct {
	ID, Type, Sort, Density string
	Name, Emoji, Position   string
	Filters                 ActivityViewFilters
}

// ActivityViews answers activity.views' saved views (fetchActivityViews).
func (a *App) ActivityViews(ctx context.Context) ([]ActivityView, error) {
	st, err := a.ActivityViewsState(ctx)
	return st.Views, err
}

// UpdateActivityView is updateActivityViewsByApi for the layout toggle
// (isAutoSave, so no filters): activity.views.update {view_id, sort,
// density}. Slack never calls it for the All view.
func (a *App) UpdateActivityView(ctx context.Context, v ActivityView, dense bool) error {
	d := "compact"
	if dense {
		d = "dense"
	}
	p := url.Values{"view_id": {v.ID}, "density": {d}, "reason": {"[Activity Views] Update views by API"}}
	if v.Sort != "" {
		p.Set("sort", v.Sort)
	}
	return a.C.Call(ctx, "activity.views.update", p, nil)
}

// SetSuppressAtChannel is the item menu's "Don't notify for @here and
// @channel" / "Notify for @here and @channel": the channel's
// suppress_at_channel notification pref (AvPY My →
// users.prefs.setNotifications).
func (a *App) SetSuppressAtChannel(ctx context.Context, ch string, on bool) error {
	return a.setNotifications(ctx, ch, []notifyPref{{Name: "suppress_at_channel", Value: strconv.FormatBool(on)}})
}

// ---- the read rows' marks ----

// actMentionTypes is Gjwd X8: the mention entries.
func actMentionEntry(t string) bool {
	switch t {
	case "at_user", "at_user_group", "at_channel", "at_everyone", "unjoined_channel_mention", "keyword", "code_review_requested":
		return true
	}
	return false
}

// YouRepliedTS is rZx3 getYouRepliedMessageTs: the ts of your reply after
// the item, or "" (the read row's "You replied" mark). A thread's entry
// counts a latest reply other than the one that notified you; a mention
// looks for your newer message in its thread (or the channel); a DM,
// group DM, app DM or channel post for your newer message in the
// conversation (top level for channels), else its latest message when
// it's yours.
func (a *App) YouRepliedTS(it *ActivityItem) string {
	self := a.S.SelfID()
	switch {
	case it.Type == "thread_v2":
		if it.LatestTS != "" && it.FeedTS != it.LatestTS {
			return it.LatestTS
		}
		return ""
	case actMentionEntry(it.Type):
		if it.Channel == "" || it.TS == "" {
			return ""
		}
		var latest *slack.Message
		for _, m := range a.cachedMsgs(it.Channel, it.ThreadTS) {
			m := m
			in := it.ThreadTS != "" && m.ThreadTS == it.ThreadTS || it.ThreadTS == "" && (m.ThreadTS == "" || m.ThreadTS == m.TS)
			if in && (latest == nil || m.TS > latest.TS) {
				latest = &m
			}
		}
		if latest != nil && latest.TS > it.TS && latest.User == self {
			return latest.TS
		}
		return ""
	case it.Type == "dm" || it.Type == "mpdm" || it.Type == "channel" || it.Type == "bot_dm" || it.Type == "bot_dm_bundle":
		if it.Channel == "" {
			return ""
		}
		var latest *slack.Message
		for _, m := range a.cachedMsgs(it.Channel, "") {
			m := m
			if it.Type == "channel" && m.ThreadTS != "" && m.ThreadTS != m.TS {
				continue
			}
			if latest == nil || m.TS > latest.TS {
				latest = &m
			}
		}
		if latest != nil && latest.TS > it.FeedTS {
			if latest.User == self {
				return latest.TS
			}
			return ""
		}
		if it.LatestTS != "" && it.FeedTS != it.LatestTS {
			if m, ok := a.S.Find(it.Channel, it.LatestTS); ok && m.User == self {
				return it.LatestTS
			}
		}
	}
	return ""
}

// cachedMsgs is every cached message of ch (its newest window, and the
// thread when thread is set): Slack's per-channel message map.
func (a *App) cachedMsgs(ch, thread string) []slack.Message {
	var out []slack.Message
	if t := a.S.Timeline(ch); t != nil {
		out = append(out, t.Msgs...)
	}
	if thread != "" {
		if th, ok := a.S.Thread(ch, thread); ok {
			out = append(out, th...)
		}
	}
	return out
}

// YouReacted is rZx3 jW: the reactions you added to the item's message
// (feed_ts) for mentions, threads, DMs, channel posts, channel invites
// and app DMs; nil for anything else. The last one is the badge's first.
func (a *App) YouReacted(it *ActivityItem) []string {
	switch it.Type {
	case "at_user", "at_channel", "at_everyone", "at_user_group", "keyword", "thread_v2", "dm", "channel", "internal_channel_invite", "bot_dm_bundle":
	default:
		return nil
	}
	if it.FeedTS == "" || it.Channel == "" {
		return nil
	}
	m, ok := a.S.Find(it.Channel, it.FeedTS)
	if !ok {
		return nil
	}
	self := a.S.SelfID()
	var out []string
	for _, r := range m.Reactions {
		for _, u := range r.Users {
			if u == self {
				out = append(out, r.Name)
				break
			}
		}
	}
	return out
}
