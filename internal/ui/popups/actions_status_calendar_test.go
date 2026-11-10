package popups

import (
	"image"
	"net/url"
	"testing"

	"gioui.org/layout"
	"gioui.org/op"
	"gioui.org/unit"

	"github.com/some-du6e/clack/internal/fakeslack"
)

// calHook answers the calendar methods (fakeslack has no calendar apps)
// and records them; installed is calendar.getInstalledCalendars' reply,
// user calendar.user.info's cal_user (nil: none).
func calHook(t *testing.T, fs *fakeslack.Server, installed map[string]any, user map[string]any) *actCalls {
	t.Helper()
	old := [3]any{actCalInstalled, actCalCurrent, actCalUser0}
	actCalInstalled, actCalCurrent, actCalUser0 = nil, nil, nil
	t.Cleanup(func() {
		actCalInstalled, _ = old[0].(*actCals)
		actCalCurrent, _ = old[1].(*actCalendar)
		actCalUser0, _ = old[2].(*actCalUser)
	})
	c := &actCalls{}
	fs.SetHook(func(method string, f url.Values) (any, bool) {
		c.mu.Lock()
		defer c.mu.Unlock()
		switch method {
		case "calendar.getInstalledCalendars":
			c.calls = append(c.calls, actCall{method, f})
			r := map[string]any{"ok": true}
			for k, v := range installed {
				r[k] = v
			}
			return r, true
		case "calendar.user.info":
			c.calls = append(c.calls, actCall{method, f})
			if user == nil {
				return map[string]any{"ok": true}, true
			}
			return map[string]any{"ok": true, "cal_user": user}, true
		case "calendar.user.toggleStatusSync":
			c.calls = append(c.calls, actCall{method, f})
			return map[string]any{"ok": true}, true
		case "users.customStatus.list":
			return map[string]any{"ok": true, "statuses": []any{}, "custom_sections": []any{}}, true
		}
		return nil, false
	})
	return c
}

func calApp(installed, manageable bool, id string) map[string]any {
	return map[string]any{"is_installed": installed, "is_manageable_by_user": manageable, "app_id": id}
}

// getUserMXCurrentCalendar, getAllCalendarPresets and the step's copy, as
// Slack's aq7I / 20cD / D72a have them.
func TestActCalendarRules(t *testing.T) {
	if ps := actCalendarPresets(nil, nil); len(ps) != 0 {
		t.Errorf("rows before getInstalledCalendars answered: %v", ps)
	}
	none := &actCals{Gcal: actCalApp{IsManageable: true}, Ocal: actCalApp{}}
	if c := actCurrentCalendar(none, ""); c != nil {
		t.Errorf("current with nothing installed and no pref: %v", c)
	}
	if ps := actCalendarPresets(nil, none); len(ps) != 1 || ps[0].Name != "Google" || ps[0].AppID != actGcalAppID {
		t.Errorf("manageable Google only: %v", ps)
	}
	if ps := actCalendarPresets(nil, &actCals{}); len(ps) != 0 {
		t.Errorf("nothing installed or manageable: %v", ps)
	}
	g := &actCals{Gcal: actCalApp{IsInstalled: true, AppID: actGcalAppID}}
	cur := actCurrentCalendar(g, "OUTLOOK")
	if cur == nil || cur.Name != "Google" {
		t.Fatalf("one installed: %v", cur)
	}
	if ps := actCalendarPresets(cur, g); len(ps) != 1 || ps[0] != *cur {
		t.Errorf("installed current calendar: %v", ps)
	}
	both := &actCals{Gcal: actCalApp{IsInstalled: true, AppID: actGcalAppID}, Ocal: actCalApp{IsInstalled: true, AppID: actOcalAppID}}
	if c := actCurrentCalendar(both, "OUTLOOK"); c == nil || c.Name != "Outlook" {
		t.Errorf("both installed, pref OUTLOOK: %v", c)
	}
	if c := actCurrentCalendar(both, ""); c == nil || c.Name != "Google" {
		t.Errorf("both installed, no pref: %v", c)
	}
	// a current calendar that's neither installed nor manageable has no row
	if ps := actCalendarPresets(actMXCalendar("OUTLOOK"), none); len(ps) != 0 {
		t.Errorf("pref calendar not installed: %v", ps)
	}
	goog := actGoogle(actGcalAppID)
	if n := actCalendarNote(nil, goog); n != " — Based on your Google Calendar" {
		t.Errorf("note %q", n)
	}
	if n := actCalendarNote(&actCalUser{IsStatusSync: true}, goog); n != " — Google Calendar updates are on" {
		t.Errorf("syncing note %q", n)
	}
	for _, c := range []struct {
		u            *actCalUser
		bold, action string
	}{
		{nil, "Add Google Calendar", "Add"},
		{&actCalUser{}, "Connect your Google Calendar account", "Connect"},
		{&actCalUser{IsProviderConnected: true}, "Update your status with Google Calendar", "Turn On"},
		{&actCalUser{IsProviderConnected: true, IsStatusSync: true}, "Google Calendar updates are on", "Turn Off"},
	} {
		bold, rest := actCalendarText(goog, c.u)
		if bold != c.bold || rest != actCalendarBlurb || actCalendarAction(c.u) != c.action {
			t.Errorf("user %+v: %q / %q / %q", c.u, bold, rest, actCalendarAction(c.u))
		}
	}
	if actCalendarPicture(actGcalAppID, false) != actSlackImg+"google-calendar/lt-google-calendar-status-sync.png" ||
		actCalendarPicture(actOcalAppID, true) != actSlackImg+"outlook-calendar/dk-outlook-calendar-status-sync.png" {
		t.Error("status sync pictures")
	}
}

// No calendar app: no "Automatically updates" row, nothing to press, and
// no toast (the old stand-in said calendar sync isn't available).
func TestActStatusNoCalendar(t *testing.T) {
	h, fs := ccEnv(t)
	c := calHook(t, fs, map[string]any{"gcal": calApp(false, false, actGcalAppID), "ocal": calApp(false, false, actOcalAppID)}, nil)
	s := actNewStatusState(h, false)
	ccUntil(t, s.modal, "calendar.getInstalledCalendars", func() bool { return actCalInstalled != nil })
	if ps := s.calPresets(); len(ps) != 0 {
		t.Errorf("rows %v", ps)
	}
	if len(c.of("calendar.user.info")) != 0 {
		t.Error("calendar.user.info without an installed calendar")
	}
	for _, x := range h.toastList() {
		t.Errorf("toast %q", x)
	}
}

// Google Calendar installed and connected but not syncing: the row says
// "Based on your Google Calendar", the step offers Turn On, which sets
// is_status_sync at once, calls calendar.user.toggleStatusSync and closes.
// Back returns to the form.
func TestActStatusCalendarStep(t *testing.T) {
	h, fs := ccEnv(t)
	c := calHook(t, fs, map[string]any{"gcal": calApp(true, true, actGcalAppID), "ocal": calApp(false, false, actOcalAppID)},
		map[string]any{"id": "CU1", "app_type": "gcal", "is_provider_connected": true, "is_status_sync": false})
	s := actNewStatusState(h, false)
	ccUntil(t, s.modal, "the calendar user", func() bool { return actCalUser0 != nil })
	if got := c.of("calendar.user.info"); len(got) != 1 || got[0].Get("app_type") != "gcal" {
		t.Fatalf("calendar.user.info %v", got)
	}
	ps := s.calPresets()
	if len(ps) != 1 || ps[0].Name != "Google" || actCalendarNote(actCalUser0, ps[0]) != " — Based on your Google Calendar" {
		t.Fatalf("rows %v", ps)
	}
	s.cal.btns[0].Click()
	ccFrame(s.modal)
	if !s.cal.on || s.cal.picked.Name != "Google" {
		t.Fatal("the row didn't open the calendar step")
	}
	ccFrame(s.modal)
	if len(s.modal.o.Buttons) != 2 || s.modal.o.Buttons[0].Label != "Back" || s.modal.o.Buttons[1].Label != "Turn On" {
		t.Fatalf("footer %v", btnLabels(s.modal.o.Buttons))
	}
	// Back: the form again, with its own footer
	s.modal.press(s.modal.o.Buttons[0])
	ccFrame(s.modal)
	if s.cal.on || s.modal.Dismissed() || s.modal.o.Buttons[len(s.modal.o.Buttons)-1] != s.saveBtn {
		t.Fatalf("Back: on %v dismissed %v footer %v", s.cal.on, s.modal.Dismissed(), btnLabels(s.modal.o.Buttons))
	}
	s.cal.btns[0].Click()
	ccFrame(s.modal)
	ccFrame(s.modal)
	s.modal.press(s.modal.o.Buttons[1])
	if actCalUser0 == nil || !actCalUser0.IsStatusSync {
		t.Error("is_status_sync wasn't set at once")
	}
	ccUntil(t, s.modal, "the toggle", func() bool { return len(c.of("calendar.user.toggleStatusSync")) > 0 })
	f := c.of("calendar.user.toggleStatusSync")[0]
	if f.Get("calapp_user_id") != "CU1" || f.Get("enable") != "true" {
		t.Errorf("toggle form %v", f)
	}
	ccUntil(t, s.modal, "the modal to close", s.modal.Dismissed)
	if n := actCalendarNote(actCalUser0, ps[0]); n != " — Google Calendar updates are on" {
		t.Errorf("after Turn On the row says %q", n)
	}
}

func btnLabels(bs []*ModalButton) []string {
	var out []string
	for _, b := range bs {
		out = append(out, b.Label)
	}
	return out
}

// The "Tip:" pill follows used_custom_status_kb_shortcut, whatever opened
// the modal, and Mod+Shift+Y's modal never has it; it sits 50px under the
// modal's bottom edge, centered on it.
func TestActStatusTipPill(t *testing.T) {
	h, fs := ccEnv(t)
	calHook(t, fs, map[string]any{}, nil)
	if s := actNewStatusState(h, false); !s.showTip {
		t.Error("no tip with the pref unset")
	}
	if s := actNewStatusState(h, true); s.showTip {
		t.Error("the shortcut's modal has the tip")
	}
	if err := h.a.SetPref(t.Context(), "used_custom_status_kb_shortcut", true); err != nil {
		t.Fatal(err)
	}
	if s := actNewStatusState(h, false); s.showTip {
		t.Error("tip after the shortcut was used")
	}
	gtx := layout.Context{Ops: new(op.Ops), Metric: unit.Metric{PxPerDp: 1, PxPerSp: 1}}
	mr := image.Rect(460, 183, 980, 717)
	r := actTipRect(gtx, mr, 300)
	if r.Max.Y != 767 || r.Dy() != 34 || r.Min.X+r.Dx()/2 != 720 {
		t.Errorf("pill at %v", r)
	}
}
