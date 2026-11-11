package ui

import (
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
	rows := pageRows(t, a, p, clackTabID)
	i := 0
	for _, w := range []want{
		lg("Updates"), ck("Check for updates"), ck("Download updates automatically"),
		lg("Live updates"), ck("Live updates (typing, presence, instant messages)"),
		lg("Troubleshooting"),
	} {
		for i < len(rows) && !w.matches(rows[i]) {
			i++
		}
		if i == len(rows) {
			t.Fatalf("Clack tab: no %s %q (in order) in %s", w.kind, w.label, dumpRows(rows))
		}
		i++
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
