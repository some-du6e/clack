package ui

import (
	"image"
	"strings"
	"testing"

	"gioui.org/op"
	"gioui.org/widget"

	"github.com/some-du6e/clack/internal/ui/popups"
)

// No page says "Not available in Clack yet": each row is Slack's, live
// (writing Slack's pref, acting on it where Clack has the feature) or,
// where Clack truly lacks what it needs, greyed the way Slack greys a
// disabled row, without a note of Clack's own.
func TestPrefsNoInventedNotes(t *testing.T) {
	a, _, _ := fakePrefsApp(t)
	ids := []string{clackTabID}
	for _, it := range prefsNav {
		ids = append(ids, it.id)
	}
	for _, id := range ids {
		p := &prefsModal{a: a, clicks: map[string]*widget.Clickable{}}
		for _, r := range pageRows(t, a, p, id) {
			if strings.Contains(r.Label, "Not available") {
				t.Errorf("%s: %s %q", id, r.Kind, r.Label)
			}
		}
		p.close()
	}
}

// Rows that used to be greyed write Slack's pref now: Show AI Agents in
// top bar (show_ai_apps), the Slack Connect file warning (its box is
// !hide_sc_files_sharing_speed_bump).
func TestPrefsFormerlyGreyedWrite(t *testing.T) {
	a, _, fs := fakePrefsApp(t)
	p := &prefsModal{a: a, clicks: map[string]*widget.Clickable{}}
	defer p.close()
	for _, c := range []struct {
		page, key, pref string
		want            any
	}{
		{"navigation", "pref.show_ai_apps", "show_ai_apps", false},
		{"advanced", "pref.hide_sc_files_sharing_speed_bump", "hide_sc_files_sharing_speed_bump", true},
		{"accessibility", "pref.a11y_peeks_enabled_on_mouse_enter", "a11y_peeks_enabled_on_mouse_enter", false},
	} {
		pageRows(t, a, p, c.page)
		b, ok := p.clicks[c.key]
		if !ok {
			t.Fatalf("%s: no %s row", c.page, c.key)
		}
		b.Click()
		pageRows(t, a, p, c.page)
		waitFor(t, a, c.pref, func() bool { v, _ := fs.Pref(c.pref); return v == c.want })
	}
}

// Audio & video > Huddle motion is huddles_reaction_explosions, greyed and
// unticked while Allow animations is off, with Slack's line pointing at
// Accessibility.
func TestHuddleMotionRow(t *testing.T) {
	a, _, fs := fakePrefsApp(t)
	p := &prefsModal{a: a, clicks: map[string]*widget.Clickable{}}
	defer p.close()
	row := func() (prefsDrawn, bool) {
		t.Helper()
		var r prefsDrawn
		note := false
		for _, x := range pageRows(t, a, p, "video_audio") {
			if x.Label == "Allow for reactions & effects" {
				r = x
			}
			note = note || strings.HasPrefix(x.Label, "Animations are turned off in")
		}
		return r, note
	}
	if r, note := row(); !r.On || !r.Enabled || note {
		t.Fatalf("animations on: %+v, note %v", r, note)
	}
	p.clicks["pref.huddles_reaction_explosions"].Click()
	row()
	waitFor(t, a, "huddles_reaction_explosions", func() bool { v, _ := fs.Pref("huddles_reaction_explosions"); return v == false })
	p.setPref("a11y_animations", false)
	waitFor(t, a, "animations off", func() bool { return !p.prefBool("a11y_animations", true) })
	if r, note := row(); r.On || r.Enabled || !note {
		t.Fatalf("animations off: %+v, note %v", r, note)
	}
}

// Advanced > Download location: the box asks each time (popups'
// AskDownload says asked only while it's on), the folder is what
// popups saves into, Change… greys while the box is on.
func TestDownloadLocationPref(t *testing.T) {
	a, _, _ := fakePrefsApp(t)
	p := &prefsModal{a: a, clicks: map[string]*widget.Clickable{}}
	defer p.close()
	a.prefs.DownloadPath = "/tmp/clack-dl-test"
	a.syncDownloadPrefs()
	if got := popups.DownloadDir(); got != "/tmp/clack-dl-test" {
		t.Errorf("folder %q", got)
	}
	if _, asked, _ := a.askDownload("x.txt", nil); asked {
		t.Error("asked with the box off")
	}
	change := func() prefsDrawn {
		for _, r := range pageRows(t, a, p, "advanced") {
			if r.Kind == "button" && r.Label == "Change…" {
				return r
			}
		}
		t.Fatal("no Change… button")
		return prefsDrawn{}
	}
	if !change().Enabled {
		t.Error("Change… greyed with the box off")
	}
	p.clicks["download_ask"].Click()
	if c := change(); c.Enabled || !a.prefs.DownloadAsk || !a.dl.ask.Load() {
		t.Errorf("box on: Change… enabled %v, ask %v/%v", c.Enabled, a.prefs.DownloadAsk, a.dl.ask.Load())
	}
}

// Advanced > Launch app on login (Linux, Windows) goes through the shell's
// hooks, read once per dialog.
func TestLaunchOnLoginRow(t *testing.T) {
	a, _, _ := fakePrefsApp(t)
	on, reads := false, 0
	a.opts.LaunchOnLogin = func() bool { reads++; return on }
	a.opts.SetLaunchOnLogin = func(v bool) error { on = v; return nil }
	p := &prefsModal{a: a, clicks: map[string]*widget.Clickable{}}
	defer p.close()
	draw := func() {
		gtx := ttContext(new(op.Ops), image.Pt(523, 900))
		a.slackcFrame(gtx)
		p.launchOnLoginRow(p.form(gtx))
	}
	draw()
	p.clicks["launch_on_login"].Click()
	draw()
	draw()
	if !on || reads != 1 || !*p.launchOn {
		t.Errorf("after the click: on %v, %d reads, shown %v", on, reads, *p.launchOn)
	}
}
