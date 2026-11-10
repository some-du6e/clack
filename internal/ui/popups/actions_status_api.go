package popups

// The "Set a status" modal's Slack calls, as the web client makes them
// (build a84662d2d135, modern.vendor module tHjK, CustomStatusModal):
//
//   - Save now: users.profile.set with profile {status_emoji,
//     status_expiration (when set), status_is_dnd (only under the
//     status_improvements experiment), status_text, status_text_canonical,
//     ooo_message, status_duration} (thunk "Update custom status emoji,
//     text, expiration, and DND").
//   - Save with a later start: users.customStatus.schedule {emoji, text,
//     text_canonical, date_scheduled, date_expire, is_dnd, ooo_message,
//     duration: "custom"}, plus custom_status_id when editing one
//     (createScheduledStatus / editScheduledStatus). The generated fetcher
//     snake_cases the camelCase args (WH55 createApiMethodFetcher).
//   - The "Scheduled" section: users.customStatus.list
//     {statuses_count_per_section: 0} (fetchCustomStatuses), rows with
//     date_scheduled in the future, by start.
//   - Deleting one: users.customStatus.deleteScheduled {custom_status_id}.
//
// ooo_message is JSON.stringify of the automatic reply's rich_text blocks
// (OmIz jsonStringifyOrFallback); "" clears it.

import (
	"context"
	"encoding/json"
	"errors"
	"net/url"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/some-du6e/clack/internal/core"
)

// actOOOCanonical is the Out of office preset's text_canonical (eU).
const actOOOCanonical = "Out of office"

// actDefaultEmoji is the emoji Slack saves for a text-only status (zQ).
const actDefaultEmoji = ":speech_balloon:"

// actMaxScheduled is the most scheduled statuses Slack allows (aZ).
const actMaxScheduled = 5

// actScheduled is one users.customStatus.list entry.
type actScheduled struct {
	ID            string `json:"id"`
	Emoji         string `json:"emoji"`
	Text          string `json:"text"`
	TextCanonical string `json:"text_canonical"`
	Duration      string `json:"duration"`
	Scheduled     int64  `json:"date_scheduled"`
	Expire        int64  `json:"date_expire"`
	IsDnd         bool   `json:"is_dnd"`
	OOOMessage    string `json:"ooo_message"`
}

// actColons wraps an emoji name in colons ("" stays "").
func actColons(e string) string {
	e = strings.Trim(e, ":")
	if e == "" {
		return ""
	}
	return ":" + e + ":"
}

// actSaveEmoji is the emoji Slack saves: the chosen one, or :speech_balloon:
// when only text is set.
func actSaveEmoji(emoji, text string) string {
	if e := actColons(emoji); e != "" {
		return e
	}
	if text != "" {
		return actDefaultEmoji
	}
	return ""
}

// actCanonicalToSave is resolveTextCanonicalToSave: a preset's canonical
// text is kept while the text still matches it (Out of office always).
func actCanonicalToSave(canonical, text string) string {
	if canonical != "" && (text == "" || text == canonical) || canonical == actOOOCanonical {
		return canonical
	}
	return ""
}

// actStatusSave is what a save sends.
type actStatusSave struct {
	Emoji, Text, Canonical string
	Exp                    int64  // status_expiration / date_expire (0: none)
	Start                  int64  // date_scheduled (0: now)
	Duration               string // status_duration (30_minutes, all_day, custom, …)
	IsDnd                  bool
	// OOO is the ooo_message value; OOOSet false leaves it out (an
	// unedited existing reply, which the server keeps).
	OOO    string
	OOOSet bool
	EditID string // custom_status_id of the scheduled status being edited
}

// actProfileSet saves a status now (users.profile.set).
func actProfileSet(ctx context.Context, a *core.App, s actStatusSave) error {
	p := map[string]any{
		"status_emoji":          actSaveEmoji(s.Emoji, s.Text),
		"status_text":           s.Text,
		"status_text_canonical": actCanonicalToSave(s.Canonical, s.Text),
	}
	if s.Exp != 0 {
		p["status_expiration"] = s.Exp
	}
	if a.Experiment("status_improvements") == "on" {
		p["status_is_dnd"] = s.IsDnd
	}
	if s.OOOSet {
		p["ooo_message"] = s.OOO
	}
	if s.Duration != "" {
		p["status_duration"] = s.Duration
	}
	b, _ := json.Marshal(p)
	return a.C.Call(ctx, "users.profile.set", url.Values{"profile": {string(b)}}, nil)
}

// actScheduleStatus creates (or, with EditID, edits) a scheduled status.
func actScheduleStatus(ctx context.Context, a *core.App, s actStatusSave) error {
	v := url.Values{}
	if s.EditID != "" {
		v.Set("custom_status_id", s.EditID)
	}
	v.Set("emoji", actSaveEmoji(s.Emoji, s.Text))
	v.Set("text", s.Text)
	v.Set("text_canonical", actCanonicalToSave(s.Canonical, s.Text))
	v.Set("date_scheduled", strconv.FormatInt(s.Start, 10))
	if s.Exp != 0 {
		v.Set("date_expire", strconv.FormatInt(s.Exp, 10))
	}
	v.Set("is_dnd", strconv.FormatBool(s.IsDnd))
	if s.OOOSet {
		v.Set("ooo_message", s.OOO)
	}
	v.Set("duration", "custom")
	var r struct {
		Status struct {
			Scheduled int64 `json:"date_scheduled"`
		} `json:"scheduled_status"`
	}
	if err := a.C.Call(ctx, "users.customStatus.schedule", v, &r); err != nil {
		return err
	}
	if r.Status.Scheduled == 0 { // Slack treats a reply without one as a failure
		return errors.New("no scheduled_status")
	}
	return nil
}

// actListScheduled returns the scheduled statuses that start after now,
// soonest first.
func actListScheduled(ctx context.Context, a *core.App, now int64) ([]actScheduled, error) {
	var r struct {
		Statuses []actScheduled `json:"statuses"`
	}
	if err := a.C.Call(ctx, "users.customStatus.list", url.Values{"statuses_count_per_section": {"0"}}, &r); err != nil {
		return nil, err
	}
	out := []actScheduled{}
	for _, s := range r.Statuses {
		if s.Scheduled > now {
			out = append(out, s)
		}
	}
	sort.SliceStable(out, func(i, j int) bool { return out[i].Scheduled < out[j].Scheduled })
	return out, nil
}

// actDeleteScheduled removes a scheduled status.
func actDeleteScheduled(ctx context.Context, a *core.App, id string) error {
	return a.C.Call(ctx, "users.customStatus.deleteScheduled", url.Values{"custom_status_id": {id}}, nil)
}

// actDNDInfo reads the viewer's DND state (dnd.info).
func actDNDInfo(ctx context.Context, a *core.App) (core.DNDStatus, error) {
	var st core.DNDStatus
	raw, err := a.DND(ctx)
	if err != nil {
		return st, err
	}
	err = json.Unmarshal(raw, &st)
	return st, err
}

// actExpMatchesDND is doesExpirationMatchDndEnd: both set and within a
// minute of each other.
func actExpMatchesDND(exp, dndEnd int64) bool {
	d := exp - dndEnd
	if d < 0 {
		d = -d
	}
	return exp > 0 && dndEnd > 0 && d <= 60
}

// actDNDMinutes is setDndFromExpiration's snooze length: until exp, or
// 60480 minutes (six weeks) for a status that doesn't clear.
func actDNDMinutes(exp int64, now time.Time) int {
	if exp == 0 {
		return 60480
	}
	return actMinutesUntil(now, time.Unix(exp, 0))
}

// actDNDSave is what handleSave needs to apply the notification pause.
type actDNDSave struct {
	Pause bool // "Pause notifications until status is cleared"
	// PresetDnd is a preset that pauses notifications by itself
	// (isDndEnabledCanonicalText: Vacationing); Picked says a preset was
	// chosen at all, which replaces the modal's isDnd (else: snoozed now).
	PresetDnd, Picked bool
	Exp, InitialExp   int64
}

// actApplyDND applies the pause after a save, as handleSave does: without
// "Pause notifications" a snooze ends; then, for a pausing preset or when
// shouldSetDndFromExpiration says so, setDndFromExpiration snoozes until
// the status clears (or ends the snooze when the modal's isDnd is off). It
// reports whether notifications stay snoozed.
func actApplyDND(ctx context.Context, a *core.App, st core.DNDStatus, d actDNDSave, now time.Time) (bool, error) {
	snoozed := st.SnoozeEnabled
	set := false // shouldSetDndFromExpiration (not clearing)
	if d.Pause {
		set = !snoozed || actExpMatchesDND(d.InitialExp, st.SnoozeEnd) || d.Exp > st.SnoozeEnd
	}
	on := snoozed
	if !d.Pause && snoozed {
		if err := a.SetDND(ctx, 0); err != nil {
			return on, err
		}
		on = false
	}
	if !d.PresetDnd && !set {
		return on, nil
	}
	isDnd := snoozed
	if d.Picked {
		isDnd = d.PresetDnd
	}
	if d.Pause || isDnd {
		return true, a.SetDND(ctx, actDNDMinutes(d.Exp, now))
	}
	if on {
		return false, a.SetDND(ctx, 0)
	}
	return false, nil
}

// actClearStatus is clearUserStatus: users.profile.set with the status
// emptied (status_text_canonical null), then dnd.endSnooze when the pause
// belonged to it (a Vacationing status, or a snooze ending with the
// status). It reports whether the snooze was ended.
func actClearStatus(ctx context.Context, a *core.App, canonical string, exp int64) (bool, error) {
	b, _ := json.Marshal(map[string]any{"status_emoji": "", "status_expiration": 0, "status_text": "", "status_text_canonical": nil})
	if err := a.C.Call(ctx, "users.profile.set", url.Values{"profile": {string(b)}}, nil); err != nil {
		return false, err
	}
	st, err := actDNDInfo(ctx, a)
	if err != nil || !st.SnoozeEnabled {
		return false, nil
	}
	if canonical == "Vacationing" || actExpMatchesDND(exp, st.SnoozeEnd) {
		return true, a.SetDND(ctx, 0)
	}
	return false, nil
}

// actSelfOOO reads the viewer's saved automatic reply and the current
// status's canonical text (users.profile.get).
func actSelfOOO(ctx context.Context, a *core.App) (reply, canonical string, err error) {
	var r struct {
		Profile struct {
			OOO       string `json:"ooo_message"`
			Canonical string `json:"status_text_canonical"`
		} `json:"profile"`
	}
	if err := a.C.Call(ctx, "users.profile.get", nil, &r); err != nil {
		return "", "", err
	}
	return actOOOPlain(r.Profile.OOO), r.Profile.Canonical, nil
}

// actOOOBlocks is the ooo_message for a plain reply: one rich_text block,
// one section, one text element (the shape Slack's prefill builds); ""
// for an empty reply.
func actOOOBlocks(reply string) string {
	reply = strings.TrimSpace(reply)
	if reply == "" {
		return ""
	}
	b, _ := json.Marshal([]map[string]any{{
		"type": "rich_text",
		"elements": []map[string]any{{
			"type":     "rich_text_section",
			"elements": []map[string]any{{"type": "text", "text": reply}},
		}},
	}})
	return string(b)
}

// actOOOPlain flattens an ooo_message (rich_text blocks JSON) to text.
func actOOOPlain(raw string) string {
	if raw == "" {
		return ""
	}
	type el struct {
		Type     string `json:"type"`
		Text     string `json:"text"`
		Name     string `json:"name"`
		URL      string `json:"url"`
		UserID   string `json:"user_id"`
		Channel  string `json:"channel_id"`
		Elements []el   `json:"elements"`
	}
	var blocks []el
	if json.Unmarshal([]byte(raw), &blocks) != nil {
		return ""
	}
	var sb strings.Builder
	var walk func(es []el, depth int)
	walk = func(es []el, depth int) {
		for i, e := range es {
			switch e.Type {
			case "text":
				sb.WriteString(e.Text)
			case "emoji":
				sb.WriteString(":" + e.Name + ":")
			case "link":
				if e.Text != "" {
					sb.WriteString(e.Text)
				} else {
					sb.WriteString(e.URL)
				}
			case "user":
				sb.WriteString("<@" + e.UserID + ">")
			case "channel":
				sb.WriteString("<#" + e.Channel + ">")
			default:
				walk(e.Elements, depth+1)
				if depth == 1 && i < len(es)-1 {
					sb.WriteString("\n")
				}
			}
		}
	}
	for _, b := range blocks {
		walk(b.Elements, 1)
	}
	return strings.TrimSpace(sb.String())
}
