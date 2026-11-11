package ui

import (
	"context"
	"image"
	"strings"
	"testing"

	"gioui.org/op"
	"gioui.org/widget"
)

// Clack's own rows live in Preferences > Clack, not in Slack's Advanced
// page; Advanced keeps only Slack's rows, with no invented notes.
func TestClackPrefsTab(t *testing.T) {
	a, _, _ := fakePrefsApp(t)
	p := &prefsModal{a: a, clicks: map[string]*widget.Clickable{}}
	defer p.close()
	for _, r := range pageRows(t, a, p, "advanced") {
		switch {
		case r.Kind == "legend" && (r.Label == "Updates" || r.Label == "Live updates" || r.Label == "Troubleshooting"),
			r.Kind == "h2" && r.Label == "Clack",
			strings.Contains(r.Label, "Shortened links"),
			strings.HasPrefix(r.Label, "Live updates ("):
			t.Errorf("Advanced still has Clack's %s %q", r.Kind, r.Label)
		}
	}
	// three rows at most, no headings or notes: updates (the box, its
	// step as the subtext, one button), Replay introduction (intro-motion's
	// row, through clackIntroRow), live updates
	a.opts.CheckUpdate = func(context.Context) (UpdateInfo, bool, error) { return UpdateInfo{}, false, nil }
	rows := pageRows(t, a, p, clackTabID)
	var got []string
	for _, r := range rows {
		switch r.Kind {
		case "check", "radio", "legend", "h2", "select":
			got = append(got, r.Kind+" "+r.Label)
		case "text":
			if !strings.HasPrefix(r.Label, "Clack ") { // the update row's subtext
				t.Errorf("a note on the Clack tab: %q", r.Label)
			}
		}
	}
	// the Replay introduction row is intro_prefs.go's, in clackIntroRow:
	// second, and the tab stays at three
	if clackIntroRow == nil {
		t.Fatal("intro_prefs.go didn't hook prefsReplayIntroRow into clackIntroRow")
	}
	if w := "check Download updates automatically|legend Replay introduction|check Live updates (typing, presence, instant messages)"; strings.Join(got, "|") != w {
		t.Errorf("Clack tab rows %q, want %q", got, w)
	}
	var buttons []string
	for _, r := range rows {
		if r.Kind == "button" {
			buttons = append(buttons, r.Label)
		}
	}
	if w := "Check for Updates|Replay"; strings.Join(buttons, "|") != w {
		t.Errorf("buttons %q, want %q", buttons, w)
	}
	// the box is both settings: off stops the checks and the downloads
	if !a.autoUpdatesOn() {
		t.Fatal("automatic updates off by default")
	}
	p.clicks["update_auto"].Click()
	pageRows(t, a, p, clackTabID)
	if a.autoUpdatesOn() || !a.prefs.NoUpdateCheck || !a.prefs.NoUpdateDownload {
		t.Errorf("after unticking: checks off %v, downloads off %v", a.prefs.NoUpdateCheck, a.prefs.NoUpdateDownload)
	}
	p.clicks["update_auto"].Click()
	pageRows(t, a, p, clackTabID)
	if !a.autoUpdatesOn() || a.prefs.NoUpdateCheck || a.prefs.NoUpdateDownload {
		t.Errorf("after ticking again: checks off %v, downloads off %v", a.prefs.NoUpdateCheck, a.prefs.NoUpdateDownload)
	}
	// the tab: right after Slack's (Advanced last), before the plugin group
	if nav := p.extraNav(); len(nav) < 2 || nav[0].id != clackTabID || nav[0].divider || !nav[1].divider {
		t.Errorf("nav after Advanced: %+v", nav)
	}
	if prefsNav[len(prefsNav)-1].id != "advanced" {
		t.Error("Advanced must stay last in Slack's tabs[]")
	}
}

// Advanced > Shorten external links is Slack's InputOptionsPref box: on
// is disable_link_truncation false, written with setUserPrefByApi.
func TestShortenExternalLinksPref(t *testing.T) {
	a, _, fs := fakePrefsApp(t)
	p := &prefsModal{a: a, clicks: map[string]*widget.Clickable{}}
	defer p.close()
	row := func() prefsDrawn {
		t.Helper()
		for _, r := range pageRows(t, a, p, "advanced") {
			if r.Label == "Shorten external links" {
				return r
			}
		}
		t.Fatal("no Shorten external links row")
		return prefsDrawn{}
	}
	if r := row(); !r.On || !r.Enabled || r.Key != "pref.disable_link_truncation" {
		t.Fatalf("by default: %+v", r)
	}
	p.clicks["pref.disable_link_truncation"].Click()
	row()
	waitFor(t, a, "disable_link_truncation", func() bool { v, _ := fs.Pref("disable_link_truncation"); return v == true })
	waitFor(t, a, "the box off", func() bool { a.drain(); return !row().On })
}

// A checkbox row is as tall as its label and hint wrap to: the smart
// quotes hint (Karim's screenshot: clipped mid-line, over the next row)
// pushes the next row down by all its lines.
func TestPrefsCheckRowWraps(t *testing.T) {
	a, _, _ := fakePrefsApp(t)
	p := &prefsModal{a: a, clicks: map[string]*widget.Clickable{}}
	defer p.close()
	const hint = "Type quotation marks \" \" to create curly ones “ ”. Type -- to create an em-dash —. Type ... to create an ellipsis …. Type - and >, or < and - to create arrows → ←"
	height := func(w int, label, hint string) int {
		gtx := ttContext(new(op.Ops), image.Pt(w, 900))
		a.slackcFrame(gtx)
		f := p.form(gtx)
		f.option("t", label, hint, true, false, func() {})
		return f.y
	}
	gtx := ttContext(new(op.Ops), image.Pt(523, 900))
	line := gtx.Dp(22)
	one := height(523, "Enable smart quotes and symbols", "")
	if one < line {
		t.Fatalf("one-line row %d px", one)
	}
	full := height(523, "Enable smart quotes and symbols", hint)
	// the hint is ~1,300 px of text: at least 3 lines in a 498 px column
	if full-one < 3*line {
		t.Errorf("row with the hint is %d px, the label alone %d: the hint got %d px, want ≥ %d", full, one, full-one, 3*line)
	}
	if narrow := height(300, "Enable smart quotes and symbols", hint); narrow <= full {
		t.Errorf("a narrower page didn't grow the row: %d ≤ %d", narrow, full)
	}
}
