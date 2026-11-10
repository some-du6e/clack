package popups

import (
	"encoding/json"
	"net/url"
	"reflect"
	"sync"
	"testing"

	"gioui.org/layout"
)

func TestAvTimeOptions(t *testing.T) {
	ts := avTimes()
	if len(ts) != 48 || ts[0] != "00:00" || ts[17] != "08:30" || ts[47] != "23:30" {
		t.Fatalf("times %v…", ts[:3])
	}
	st := avStartOpts("09:00", true)
	if st[0] != avAllDay || len(st) != 49 {
		t.Errorf("start with None: %d %q", len(st), st[0])
	}
	if st := avStartOpts("09:15", false); len(st) != 49 || st[18] != "09:00" || st[19] != "09:15" || st[20] != "09:30" {
		t.Errorf("a non-interval start is inserted in order: %v", st[17:21])
	}
	// generic end options wrap past midnight (but never list 00:00 itself);
	// Midnight closes the later ones
	end := avEndOpts("09:00", "17:00", true)
	if end[0] != "09:30" || end[len(end)-1] != "08:30" {
		t.Errorf("wrapped: first %q last %q", end[0], end[len(end)-1])
	}
	mid := -1
	for i, v := range end {
		if v == avMidnight {
			mid = i
		}
		if v == "00:00" {
			t.Error("00:00 offered as an end")
		}
	}
	if mid != 29 { // 09:30 … 23:30 is 29 slots
		t.Errorf("Midnight at %d", mid)
	}
	if e := avEndOpts("09:00", "17:00", false); e[len(e)-1] != avMidnight {
		t.Errorf("custom days don't wrap: %q", e[len(e)-1])
	}
	if avBump("17:00") != "17:30" || avBump("23:30") != avMidnight || avBump("08:15") != "08:30" {
		t.Error("getBumpedEndTimeValue")
	}
	if avLabel(avAllDay) != "None" || avLabel(avMidnight) != "Midnight" || avLabel("08:30") != dtTimeLabel(8*60+30) || avLabel("") != "--:--" {
		t.Error("labels")
	}
	if avPad("8:00") != "08:00" || avPad("22:00") != "22:00" {
		t.Error("padStart")
	}
}

func TestAvChangesWriteWhatSlackWrites(t *testing.T) {
	hr := avHours{Enabled: true, Days: "every_day", Start: "17:00", End: "9:00"}
	if got := hr.avSetDays("custom"); !reflect.DeepEqual(got, map[string]any{"dnd_days": "custom", "dnd_enabled": true}) {
		t.Errorf("days %v", got)
	}
	if got := hr.avSetFrom("08:00"); !reflect.DeepEqual(got, map[string]any{"dnd_end_hour": "08:00", "dnd_enabled": true}) {
		t.Errorf("from %v", got)
	}
	// a start at or after the end bumps the end to the next half hour
	if got := hr.avSetFrom("17:00"); !reflect.DeepEqual(got, map[string]any{"dnd_end_hour": "17:00", "dnd_start_hour": "17:30", "dnd_enabled": true}) {
		t.Errorf("bumped %v", got)
	}
	if hr.Start != "17:30" || hr.End != "17:00" {
		t.Errorf("state %q %q", hr.Start, hr.End)
	}
	if got := hr.avSetTo("22:00"); !reflect.DeepEqual(got, map[string]any{"dnd_start_hour": "22:00", "dnd_enabled": true}) {
		t.Errorf("to %v", got)
	}
	hr.Week[2] = avDay{Enabled: avPartial, Before: "09:00", After: "17:00"}
	if got := hr.avSetDayFrom(2, avAllDay); !reflect.DeepEqual(got, map[string]any{"dnd_enabled_wednesday": avAllDay, "dnd_enabled": true}) {
		t.Errorf("None %v", got)
	}
	if got := hr.avSetDayFrom(2, "18:00"); !reflect.DeepEqual(got, map[string]any{"dnd_before_wednesday": "18:00", "dnd_after_wednesday": "18:30",
		"dnd_enabled_wednesday": avPartial, "dnd_enabled": true}) {
		t.Errorf("day bumped %v", got)
	}
	if got := hr.avSetDayTo(6, "20:00"); !reflect.DeepEqual(got, map[string]any{"dnd_after_sunday": "20:00", "dnd_enabled_sunday": avPartial, "dnd_enabled": true}) {
		t.Errorf("sunday to %v", got)
	}
	// DND off shows every day, 12:00 AM to Midnight; a disabled day too
	off := avHours{Days: "weekdays", Start: "17:00", End: "09:00"}
	if off.shownDays() != "every_day" || off.shownFrom() != "00:00" || off.shownTo() != avMidnight {
		t.Errorf("off: %q %q %q", off.shownDays(), off.shownFrom(), off.shownTo())
	}
	off.Week[0].Enabled = avDisabled
	if b, a := off.dayShown(0); b != "00:00" || a != avMidnight {
		t.Errorf("disabled day %q %q", b, a)
	}
	// automatic statuses keep the object's other fields
	if got := avToggleShow(map[string]any{"emoji": ":telescope:", "show": true}, true); !reflect.DeepEqual(got, map[string]any{"emoji": ":telescope:", "show": false}) {
		t.Errorf("toggle %v", got)
	}
	if !(avHours{}).FocusShown() || (avHours{}).OutsideShown() {
		t.Error("defaults: focus shown, outside hidden")
	}
}

func TestAvWrap(t *testing.T) {
	w := func(s string) int { return len(s) }
	got := avWrap([]string{"aa", "bb", "cc", "dd"}, 5, w)
	if !reflect.DeepEqual(got, []string{"aa bb", "cc dd"}) {
		t.Errorf("wrap %q", got)
	}
}

// The page reads Slack's prefs (JSON prefs as strings), and its writes go
// through users.prefs.set: several at once as prefs=JSON, one alone as
// name/value.
func TestAvPageReadsAndWrites(t *testing.T) {
	h, fs := ccEnv(t)
	var mu sync.Mutex
	var sets []url.Values
	fs.SetHook(func(method string, f url.Values) (any, bool) {
		switch method {
		case "users.prefs.get":
			return map[string]any{"ok": true, "prefs": map[string]any{
				"dnd_enabled": true, "dnd_days": "custom", "dnd_start_hour": "17:00", "dnd_end_hour": "09:00",
				"dnd_enabled_monday": "partial", "dnd_before_monday": "08:00", "dnd_after_monday": "16:30",
				"dnd_enabled_saturday": "all_day", "dnd_enabled_sunday": "disabled",
				"huddles_set_status": false, "focus_mode_status": `{"show":false,"emoji":":telescope:"}`,
			}}, true
		case "users.prefs.set":
			mu.Lock()
			sets = append(sets, f)
			mu.Unlock()
			return map[string]any{"ok": true}, true
		}
		return nil, false
	})
	pg := NewAvailabilityPage(h)
	var nav []string
	pg.SetSectionNav(func(id string) { nav = append(nav, id) })
	o := avPageOverlay{pg}
	ccUntil(t, o, "the prefs", func() bool { return pg.p.loaded })
	hr := pg.p.hr
	if hr.Days != "custom" || hr.Week[0] != (avDay{avPartial, "08:00", "16:30"}) || hr.Week[5].Enabled != avAllDay || hr.Week[6].Enabled != avDisabled {
		t.Fatalf("read %+v", hr)
	}
	if hr.Huddle || hr.FocusShown() || hr.OutsideShown() || hr.Focus["emoji"] != ":telescope:" {
		t.Errorf("automatic statuses %v %v %v", hr.Huddle, hr.Focus, hr.Outside)
	}
	pg.p.write(pg.p.hr.avSetDayTo(0, "18:00"))
	pg.p.setPref("focus_mode_status", avToggleShow(pg.p.hr.Focus, pg.p.hr.FocusShown()))
	ccUntil(t, o, "the writes", func() bool { mu.Lock(); defer mu.Unlock(); return len(sets) == 2 })
	mu.Lock()
	defer mu.Unlock()
	var multi map[string]any
	if err := json.Unmarshal([]byte(sets[0].Get("prefs")), &multi); err != nil {
		t.Fatalf("prefs %q", sets[0].Get("prefs"))
	}
	if !reflect.DeepEqual(multi, map[string]any{"dnd_after_monday": "18:00", "dnd_enabled_monday": "partial", "dnd_enabled": true}) {
		t.Errorf("multi %v", multi)
	}
	if sets[1].Get("name") != "focus_mode_status" || sets[1].Get("value") != `{"emoji":":telescope:","show":true}` {
		t.Errorf("single %v", sets[1])
	}
	// the VIPs link switches the dialog's section
	pg.p.toSection("vip")
	if len(nav) != 1 || nav[0] != "vip" {
		t.Errorf("nav %v", nav)
	}
}

// avPageOverlay lays the page out as an Overlay for ccUntil.
type avPageOverlay struct{ p *AvailabilityPage }

func (o avPageOverlay) Layout(gtx layout.Context) layout.Dimensions { return o.p.Layout(gtx) }
func (o avPageOverlay) Dismissed() bool                             { return false }
func (o avPageOverlay) Dismiss()                                    {}
