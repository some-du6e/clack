package views

import (
	"context"
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
