package views

import (
	"image"
	"strings"
	"testing"
	"time"

	"github.com/some-du6e/clack/internal/core"
	"github.com/some-du6e/clack/internal/imgcache"
	"github.com/some-du6e/clack/internal/richtext"
	"github.com/some-du6e/clack/internal/ui/theme"
)

func optLabels(os []dirPickOpt) string {
	var s []string
	for _, o := range os {
		l := o.label
		switch {
		case o.header:
			l = "[" + l + "]"
		case o.group:
			l = "  " + l
		}
		s = append(s, l)
	}
	return strings.Join(s, " | ")
}

// SearchFilterTitle / SearchFilterLocation: the selections, "Clear all"
// after two, then up to five suggestions not chosen; a query lists the
// matching suggestions and echoes itself.
func TestDirFacetOpts(t *testing.T) {
	p := newDirPick("Ex. Director", true)
	fs := []core.Facet{{Value: "Engineer", Count: 3}, {Value: "Designer", Count: 2}, {Value: "Director", Count: 2}, {Value: "Writer", Count: 1}, {Value: "Organizer", Count: 1}, {Value: "Sixth", Count: 1}}
	if got := optLabels(facetOpts(&p, nil, fs)); got != "[Suggestions] |   Engineer |   Designer |   Director |   Writer |   Organizer" {
		t.Errorf("nothing chosen: %s", got)
	}
	if got := optLabels(facetOpts(&p, []string{"Designer"}, fs)); got != "Designer | [Suggestions] |   Engineer |   Director |   Writer |   Organizer" {
		t.Errorf("one chosen: %s", got)
	}
	if got := optLabels(facetOpts(&p, []string{"Designer", "Mayor"}, fs)); got != "Designer | Mayor | Clear all | [Suggestions] |   Engineer |   Director |   Writer |   Organizer" {
		t.Errorf("two chosen: %s", got)
	}
	p.srch.ed.SetText("dir")
	if got := optLabels(facetOpts(&p, nil, fs)); got != "Director | dir" {
		t.Errorf("query: %s", got)
	}
	if got := optLabels(facetOpts(&p, nil, nil)); got != "dir" {
		t.Errorf("query without suggestions: %s", got)
	}
}

// getFilterTriggerContent's texts, and a pick toggling a multi select.
func TestDirPickTriggerAndApply(t *testing.T) {
	d := &Directories{}
	for _, c := range []struct {
		label string
		set   func()
		want  string
		on    bool
	}{
		{"Title", func() {}, "Title", false},
		{"Title", func() { d.titles = []string{"Engineer"} }, "Title: Engineer", true},
		{"Title", func() { d.titles = []string{"Engineer", "Designer"} }, "Multiple titles", true},
		{"Location", func() {}, "Location", false},
		{"Location", func() { d.locs = []string{"Tokyo"} }, "Location: Tokyo", true},
		{"Location", func() { d.locs = []string{"Tokyo", "Oslo"} }, "Multiple locations", true},
		{"Organizations", func() {}, "Organizations", false},
		{"Organizations", func() { d.orgs = []string{"E1", "E2"} }, "With 2 organizations", true},
	} {
		c.set()
		if text, _, on := d.pickTrigger(dirTabPeople, c.label); text != c.want || on != c.on {
			t.Errorf("%s: %q active %v, want %q %v", c.label, text, on, c.want, c.on)
		}
	}
	d.titles = nil
	if !d.applyPick(dirTabPeople, "Title", dirPickOpt{value: "Engineer"}) || !d.applyPick(dirTabPeople, "Title", dirPickOpt{value: "Writer"}) {
		t.Fatal("a pick didn't ask for a reload")
	}
	d.applyPick(dirTabPeople, "Title", dirPickOpt{value: "Engineer"})
	if strings.Join(d.titles, ",") != "Writer" {
		t.Errorf("toggled titles %v", d.titles)
	}
	d.applyPick(dirTabPeople, "Title", dirPickOpt{value: "clear", clear: true})
	if d.titles != nil {
		t.Errorf("Clear all left %v", d.titles)
	}
	d.team[dirTabChannels] = "T1"
	if d.applyPick(dirTabChannels, "Workspaces", dirPickOpt{value: "T1"}) {
		t.Error("picking the selected workspace again reloaded")
	}
	if !d.applyPick(dirTabChannels, "Workspaces", dirPickOpt{value: ""}) || d.team[dirTabChannels] != "" {
		t.Error("All workspaces didn't clear the workspace")
	}
	if d.pickFor(dirTabGroups, "Workspaces") != &d.wsPick[dirTabGroups] || d.pickFor(dirTabGroups, "Exclude deactivated") != nil ||
		d.pickFor(dirTabChannels, "Title") != nil {
		t.Error("pickFor")
	}
}

// hydrateFacetSuggestions: merged by value, at most every 4 minutes.
func TestDirTakeFacets(t *testing.T) {
	d := &Directories{}
	t0 := time.Unix(1000, 0)
	d.takeFacets(t0, []core.Facet{{Value: "A", Count: 1}}, []core.Facet{{Value: "X", Count: 2}})
	d.takeFacets(t0.Add(time.Minute), []core.Facet{{Value: "B", Count: 1}}, nil)
	if len(d.facetT) != 1 || d.facetT[0].Value != "A" || len(d.facetL) != 1 {
		t.Errorf("within 4 minutes: %+v %+v", d.facetT, d.facetL)
	}
	d.takeFacets(t0.Add(5*time.Minute), []core.Facet{{Value: "A", Count: 9}, {Value: "B", Count: 1}}, nil)
	if len(d.facetT) != 2 || d.facetT[0] != (core.Facet{Value: "A", Count: 1}) || d.facetT[1].Value != "B" {
		t.Errorf("merged: %+v", d.facetT)
	}
	if !dirMatch("Senior Engineer", "sen eng") || dirMatch("Engineer", "eng sen") || !dirMatch("x", "") {
		t.Error("dirMatch")
	}
}

// leaveHost records the browser's maybeLeaveChannel calls.
type leaveHost struct {
	calls []string
}

func (h *leaveHost) Core() *core.App                           { return nil }
func (h *leaveHost) Theme() *theme.Theme                       { return nil }
func (h *leaveHost) Images() *imgcache.Cache                   { return nil }
func (h *leaveHost) Invalidate()                               {}
func (h *leaveHost) Navigate(string)                           {}
func (h *leaveHost) OpenThread(string, string)                 {}
func (h *leaveHost) CloseSecondary()                           {}
func (h *leaveHost) OpenProfile(image.Point, string)           {}
func (h *leaveHost) OpenMessageMenu(image.Point, core.Message) {}
func (h *leaveHost) OpenLightbox(richtext.Image, core.Message) {}
func (h *leaveHost) Toast(string)                              {}
func (h *leaveHost) MaybeLeaveChannel(name, kind string, general bool, leave func()) {
	h.calls = append(h.calls, name+"/"+kind)
}

// The row's Leave is Slack's maybeLeaveChannel (the host's dialogs), not
// an inline confirmation.
func TestDirLeaveAsksTheHost(t *testing.T) {
	h := &leaveHost{}
	d := &Directories{base: newBase(h)}
	d.leave(core.ChannelHit{ID: "G1", Name: "plans", Kind: "private", IsMember: true})
	d.leave(core.ChannelHit{ID: "C1", Name: "general-ish", Kind: "channel", IsMember: true})
	if strings.Join(h.calls, ",") != "plans/private,general-ish/channel" {
		t.Errorf("calls %v", h.calls)
	}
	if len(d.ops) != 0 {
		t.Error("left before the host said so")
	}
}
