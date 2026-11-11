package popups

// Working hours and automatic statuses as Slack's Preferences > Availability
// reads and writes them (prefs-dialog 67652df5246ac5dc AvailabilitySection,
// DndPreferences, DndSchedule, DndCustomPreferences, BaseDndTimeSelect;
// constants jDuV in client-boot-shared):
//
//   - Every day / Weekdays: one "Allow notifications" window, its first
//     select dnd_end_hour (notifications start when DND ends), its second
//     dnd_start_hour; the second's options wrap past midnight.
//   - Custom: a row per day, Monday first: dnd_before_<day> (first select,
//     with a "None" option = dnd_enabled_<day> all_day while fewer than
//     six other days are None) and dnd_after_<day>; a day with
//     dnd_enabled_<day> "disabled" shows 12:00 AM to Midnight.
//   - Every change writes only what changed plus dnd_enabled: true
//     (setMultiplePrefsByApi: users.prefs.set prefs=JSON). An end at or
//     before its start bumps the start to the next half hour
//     (shouldBumpEndTime / getBumpedEndTimeValue).
//   - Times are "HH:mm" half hours (06oL getTimePickerHourOptions) plus
//     "Midnight" = 23:59.
//   - Automatic statuses: huddles_set_status (bool), focus_mode_status and
//     scheduled_dnd_status (objects whose show toggles; default shown and
//     hidden), each written alone (setUserPrefByApi).

import (
	"context"
	"encoding/json"
	"fmt"
	"net/url"
	"strings"

	"github.com/some-du6e/clack/internal/core"
)

// The days Slack's custom schedule lists, in its order.
var avWeek = [7]struct{ id, label string }{
	{"monday", "Monday"}, {"tuesday", "Tuesday"}, {"wednesday", "Wednesday"}, {"thursday", "Thursday"},
	{"friday", "Friday"}, {"saturday", "Saturday"}, {"sunday", "Sunday"},
}

// dnd_enabled_<day> values (jDuV rc).
const (
	avAllDay   = "all_day"
	avDisabled = "disabled"
	avPartial  = "partial"
	avMidnight = "23:59"
)

// avDay is one day of the custom schedule.
type avDay struct {
	Enabled       string // all_day | disabled | partial
	Before, After string // dnd_before_<day>, dnd_after_<day>
}

// avHours is the page's prefs.
type avHours struct {
	Enabled    bool   // dnd_enabled
	Days       string // every_day | weekdays | custom
	Start, End string // dnd_start_hour, dnd_end_hour
	Week       [7]avDay

	Huddle  bool           // huddles_set_status
	Focus   map[string]any // focus_mode_status
	Outside map[string]any // scheduled_dnd_status
}

// avShow is an object pref's show (def when unset).
func avShow(o map[string]any, def bool) bool {
	if v, ok := o["show"].(bool); ok {
		return v
	}
	return def
}

func (hr avHours) FocusShown() bool   { return avShow(hr.Focus, true) }
func (hr avHours) OutsideShown() bool { return avShow(hr.Outside, false) }

// avPad is padStart(v, 5, "0"): "8:00" → "08:00". An unset pref stays ""
// (padStart would make it "00000"): BaseDndTimeSelect checks the raw
// value first, so an unset end hour shows its "--:--" placeholder and gets
// no option of its own (insertNonIntervalTimeOption runs only when the
// raw value is set).
func avPad(v string) string {
	if v == "" {
		return ""
	}
	for len(v) < 5 {
		v = "0" + v
	}
	return v
}

// avTimes are the half-hour options, "00:00" … "23:30".
func avTimes() []string {
	out := make([]string, 0, 48)
	for m := 0; m < 24*60; m += 30 {
		out = append(out, avFormatHHMM(m))
	}
	return out
}

func avFormatHHMM(m int) string { return fmt.Sprintf("%02d:%02d", m/60, m%60) }

// avLabel is an option's label: None, Midnight, or the time.
func avLabel(v string) string {
	switch v {
	case avAllDay:
		return "None"
	case avMidnight:
		return "Midnight"
	case "":
		return "--:--"
	}
	if m, ok := avParseHM(v); ok {
		return dtTimeLabel(m)
	}
	return v
}

// avInsert is insertNonIntervalTimeOption: v placed among the sorted times
// unless already there ("None" stays first).
func avInsert(opts []string, v string) []string {
	if v == "" {
		return opts
	}
	for _, o := range opts {
		if o == v {
			return opts
		}
	}
	i := len(opts)
	for j, o := range opts {
		if o != avAllDay && o > v {
			i = j
			break
		}
	}
	out := append(append(append([]string(nil), opts[:i]...), v), opts[i:]...)
	return out
}

// avStartOpts are the first select's options: the half hours (no
// Midnight), "None" first when allowed, and cur if it isn't a half hour.
func avStartOpts(cur string, none bool) []string {
	opts := avTimes()
	if none {
		opts = append([]string{avAllDay}, opts...)
	}
	if cur != avAllDay {
		opts = avInsert(opts, cur)
	}
	return opts
}

// avEndOpts are the second select's options after start: later half hours
// and Midnight, then (wrap) the earlier ones but midnight itself.
func avEndOpts(start, cur string, wrap bool) []string {
	all := append(avTimes(), avMidnight)
	var opts, before []string
	for _, v := range all {
		switch {
		case v > start:
			opts = append(opts, v)
		case v < start && v != "00:00":
			before = append(before, v)
		}
	}
	if wrap {
		opts = append(opts, before...)
	}
	if cur != "" {
		opts = avInsert(opts, cur)
	}
	return opts
}

// avBump is getBumpedEndTimeValue: the first half hour after v, else
// Midnight.
func avBump(v string) string {
	for _, o := range avTimes() {
		if o > v {
			return o
		}
	}
	return avMidnight
}

// avNoneCount is how many days are None (all_day).
func (hr avHours) avNoneCount() int {
	n := 0
	for _, d := range hr.Week {
		if d.Enabled == avAllDay {
			n++
		}
	}
	return n
}

// shown are the generic window's values as DndPreferences passes them:
// with DND off, every day from 12:00 AM to Midnight.
func (hr avHours) shownDays() string {
	if !hr.Enabled {
		return "every_day"
	}
	return hr.Days
}

func (hr avHours) shownFrom() string {
	if !hr.Enabled {
		return "00:00"
	}
	return avPad(hr.End)
}

func (hr avHours) shownTo() string {
	if !hr.Enabled {
		return avMidnight
	}
	return avPad(hr.Start)
}

// dayShown are day i's first and second values (a disabled day is 12:00 AM
// to Midnight).
func (hr avHours) dayShown(i int) (before, after string) {
	d := hr.Week[i]
	if d.Enabled == avDisabled {
		return "00:00", avMidnight
	}
	return avPad(d.Before), avPad(d.After)
}

// ---- the prefs a change writes (pure; tested) ----

// avSetDays is handleDndDaysChange.
func (hr *avHours) avSetDays(days string) map[string]any {
	hr.Days, hr.Enabled = days, true
	return map[string]any{"dnd_days": days, "dnd_enabled": true}
}

// avSetFrom is handleDndEndTimeChange (the first select).
func (hr *avHours) avSetFrom(v string) map[string]any {
	ch := map[string]any{"dnd_end_hour": v, "dnd_enabled": true}
	if v >= avPad(hr.Start) {
		b := avBump(v)
		ch["dnd_start_hour"] = b
		hr.Start = b
	}
	hr.End, hr.Enabled = v, true
	return ch
}

// avSetTo is handleDndStartTimeChange (the second select).
func (hr *avHours) avSetTo(v string) map[string]any {
	hr.Start, hr.Enabled = v, true
	return map[string]any{"dnd_start_hour": v, "dnd_enabled": true}
}

// avSetDayFrom is a day's first select: None (updateCustomDndEnabled
// all_day) or handleCustomDndEndTimeChange.
func (hr *avHours) avSetDayFrom(i int, v string) map[string]any {
	day := avWeek[i].id
	d := &hr.Week[i]
	hr.Enabled = true
	if v == avAllDay {
		d.Enabled = avAllDay
		return map[string]any{"dnd_enabled_" + day: avAllDay, "dnd_enabled": true}
	}
	ch := map[string]any{"dnd_before_" + day: v, "dnd_enabled_" + day: avPartial, "dnd_enabled": true}
	if v >= avPad(d.After) {
		b := avBump(v)
		ch["dnd_after_"+day] = b
		d.After = b
	}
	d.Before, d.Enabled = v, avPartial
	return ch
}

// avSetDayTo is handleCustomDndStartTimeChange (a day's second select).
func (hr *avHours) avSetDayTo(i int, v string) map[string]any {
	day := avWeek[i].id
	d := &hr.Week[i]
	d.After, d.Enabled = v, avPartial
	hr.Enabled = true
	return map[string]any{"dnd_after_" + day: v, "dnd_enabled_" + day: avPartial, "dnd_enabled": true}
}

// ---- Slack calls ----

// avReadHours reads the page's prefs (users.prefs.get).
func avReadHours(ctx context.Context, a *core.App) (avHours, error) {
	raw, err := a.C.Prefs(ctx)
	if err != nil {
		return avHours{}, err
	}
	str := func(k string) string {
		var s string
		_ = json.Unmarshal(raw[k], &s)
		return s
	}
	boolean := func(k string, def bool) bool {
		v, ok := raw[k]
		if !ok {
			return def
		}
		var b bool
		if json.Unmarshal(v, &b) == nil {
			return b
		}
		switch strings.Trim(string(v), `"`) {
		case "true", "1":
			return true
		case "false", "0", "":
			return false
		}
		return def
	}
	// JSON prefs come back as strings holding the object (or as objects)
	object := func(k string) map[string]any {
		var o map[string]any
		if json.Unmarshal(raw[k], &o) == nil {
			return o
		}
		if s := str(k); s != "" && json.Unmarshal([]byte(s), &o) == nil {
			return o
		}
		return nil
	}
	hr := avHours{
		Enabled: boolean("dnd_enabled", true), Days: str("dnd_days"),
		Start: str("dnd_start_hour"), End: str("dnd_end_hour"),
		Huddle: boolean("huddles_set_status", true), Focus: object("focus_mode_status"), Outside: object("scheduled_dnd_status"),
	}
	if hr.Days == "" {
		hr.Days = "every_day"
	}
	for i, d := range avWeek {
		hr.Week[i] = avDay{Enabled: str("dnd_enabled_" + d.id), Before: str("dnd_before_" + d.id), After: str("dnd_after_" + d.id)}
	}
	return hr, nil
}

// avWritePrefs is setMultiplePrefsByApi: users.prefs.set prefs=JSON.
func avWritePrefs(ctx context.Context, a *core.App, prefs map[string]any) error {
	b, err := json.Marshal(prefs)
	if err != nil {
		return err
	}
	return a.C.Call(ctx, "users.prefs.set", url.Values{"prefs": {string(b)}}, nil)
}

// avToggleShow is an object pref with show flipped ({...o, show: !shown}).
func avToggleShow(o map[string]any, shown bool) map[string]any {
	out := make(map[string]any, len(o)+1)
	for k, v := range o {
		out[k] = v
	}
	out["show"] = !shown
	return out
}
