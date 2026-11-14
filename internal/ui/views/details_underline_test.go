package views

// HideUserConfirmationModal's "preferences" (details_more.go
// hideLinkLine): a HelpLink to the hidden-people article, underlined
// under a11y_underline_button_link with underline_links (core
// UnderlineButtonLinks; the look is in ch-details-shots).

import (
	"slices"
	"testing"
)

func TestDetailsHideHelpLink(t *testing.T) {
	srv := &cdTestServer{}
	e := newCDTestEnv(t, srv, "D1")
	e.until("info", e.loaded)
	e.d.hide.Click()
	e.frame()
	if e.d.Dialog() != "hide" {
		t.Fatalf("Hide opened %q", e.d.Dialog())
	}
	e.d.x.hidePrefs.Click()
	e.frame()
	e.frame()
	e.h.mu.Lock()
	got := slices.Clone(e.h.tsts)
	e.h.mu.Unlock()
	if !slices.Contains(got, cdHiddenHelpArticle) {
		t.Errorf("preferences opened %q, want the help article", got)
	}
	if e.d.Dialog() != "hide" {
		t.Errorf("the link closed the modal (%q)", e.d.Dialog())
	}
	// CX15 bF: the experiment and the pref
	app := e.h.app
	app.SetExperimentGroup("a11y_underline_button_link", "on")
	app.S.Lock()
	app.S.Prefs.UnderlineLinks = true
	app.S.Unlock()
	if !app.UnderlineButtonLinks() {
		t.Error("on with underline_links: not underlined")
	}
	app.SetExperimentGroup("a11y_underline_button_link", "off")
	if app.UnderlineButtonLinks() {
		t.Error("off: underlined")
	}
	e.frame()
}
