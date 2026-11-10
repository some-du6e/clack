package views

import (
	"context"
	"slices"
	"strconv"
	"testing"
)

// useDefaultExcludeHistory (boot-shared J1Xx): the add-people modal's
// history step starts on "Don't include" only under
// mpdm_default_exclude_history_clients_webapp with the org pref
// mpdm_add_people_default_exclude_history (enterprise.prefs.get), else
// on "From today".
func TestDetailsAddPeopleDefaultHistory(t *testing.T) {
	cases := []struct {
		group string
		pref  bool
		want  int
	}{
		{"on", true, cdHistNone},
		{"on", false, cdHistToday},
		{"", true, cdHistToday},
		{"off", true, cdHistToday},
		{"control", true, cdHistToday},
	}
	for _, c := range cases {
		srv := &cdTestServer{org: `{"mpdm_add_people_default_exclude_history":` + strconv.FormatBool(c.pref) + `}`}
		e := newCDTestEnv(t, srv, "D1")
		e.h.app.SetExperimentGroup("mpdm_default_exclude_history_clients_webapp", c.group)
		if err := e.h.app.LoadEnterprisePrefs(context.Background()); err != nil {
			t.Fatal(err)
		}
		e.until("info", e.loaded)
		e.d.addPeople.Click()
		e.frame()
		a := e.d.x.dlg.add
		if a == nil {
			t.Fatalf("%q/%v: no add-people modal", c.group, c.pref)
		}
		if a.hist != c.want {
			t.Errorf("experiment %q, org pref %v: history %d, want %d", c.group, c.pref, a.hist, c.want)
		}
	}
}

// MPDMInfo's title: the other members (the viewer left out) up to three,
// regular size for a short internal name; past three, two and "N others".
func TestDetailsMpdmTitle(t *testing.T) {
	srv := &cdTestServer{}
	e := newCDTestEnv(t, srv, "G9")
	e.until("info", e.loaded)
	inf, _, _ := e.d.info.get()
	title, named, small := e.d.mpdmTitle(e.d.conv(inf, true))
	want := e.d.userName("U1") + ", " + e.d.userName("U2")
	if e.d.userName("U2") < e.d.userName("U1") {
		want = e.d.userName("U2") + ", " + e.d.userName("U1")
	}
	if title != want || named || small { // "mpdm-self--alice--bob-1" is under 30 characters
		t.Errorf("title %q named %v small %v, want %q", title, named, small, want)
	}
	if got := cdMpdmList([]string{"a", "b", "c", "d"}); got != "a, b, 2 others" {
		t.Errorf("four: %q", got)
	}
	if got := cdMpdmList([]string{"a", "b", "c"}); got != "a, b, c" {
		t.Errorf("three: %q", got)
	}
}

// MessageAndFileSharingRestrictionsField (enable_new_channel_settings):
// "Sharing & file downloads" when canSetProperties lets the viewer set
// restrict_message_and_file_sharing (read-only for another reason,
// hidden for disallowed_action); Restrict opens the modal, whose button
// is conversations.setProperties. Without the experiment it never shows.
func TestDetailsSharingField(t *testing.T) {
	srv := &cdTestServer{people: 3, canSet: `[{"prop":"restrict_guests","can_set":false,"reason":"disallowed_action"},` +
		`{"prop":"restrict_message_and_file_sharing","can_set":true}]`}
	e := newCDTestEnv(t, srv, "C1")
	e.h.app.SetExperimentGroup("enable_new_channel_settings", "on")
	e.until("info", e.loaded)
	e.d.ShowTab("Settings")
	e.until("canSetProperties", func() bool { _, ok, _ := e.d.x.canProps.get(); return ok })
	if st, ok := e.d.sharingState(); !ok || !st.CanSet {
		t.Fatalf("sharing state %+v %v", st, ok)
	}
	if _, ok := e.d.guestAccessState(); ok {
		t.Error("guest access shows for disallowed_action")
	}
	e.d.Press("sharing")
	e.frame()
	if e.d.Dialog() != "sharing" {
		t.Fatalf("Sharing & file downloads opened %q", e.d.Dialog())
	}
	e.d.Press("ok")
	e.until("conversations.setProperties", func() bool { return len(srv.seen("conversations.setProperties")) > 0 })
	f := srv.seen("conversations.setProperties")[0].form
	if f.Get("channel_id") != "C1" || f.Get("props") != `{"restrict_message_and_file_sharing":true}` {
		t.Errorf("setProperties form %v", f)
	}
	e.until("closed", func() bool { return e.d.Dialog() == "" })

	e.h.app.SetExperimentGroup("enable_new_channel_settings", "off")
	if _, ok := e.d.sharingState(); ok {
		t.Error("enable_new_channel_settings off: the field shows")
	}
}

// ManageChannelTabs: without move_channel_tabs_settings the "who can
// manage tabs" field heads the Tabs tab (and Settings has no
// tab-permissions section); with it, the field lives in Settings.
func TestDetailsTabPermissionsPlacement(t *testing.T) {
	srv := &cdTestServer{people: 3}
	e := newCDTestEnv(t, srv, "C1")
	e.until("info", e.loaded)
	e.d.tabsItems() // starts the tab list
	e.until("tabs", func() bool { _, ok, _ := e.d.tabList.get(); return ok })
	inf, _, _ := e.d.info.get()
	e.h.app.SetExperimentGroup("move_channel_tabs_settings", "on")
	on := len(e.d.tabsItems())
	if !slices.Contains(e.d.settingsSections(inf), cdSecTabPermissions) {
		t.Error("on: Settings has no tab permissions")
	}
	for _, g := range []string{"off", ""} {
		e.h.app.SetExperimentGroup("move_channel_tabs_settings", g)
		if n := len(e.d.tabsItems()); n != on+2 {
			t.Errorf("%q: the Tabs tab has %d items, want %d (the field first)", g, n, on+2)
		}
		if slices.Contains(e.d.settingsSections(inf), cdSecTabPermissions) {
			t.Errorf("%q: Settings keeps tab permissions", g)
		}
	}
}
