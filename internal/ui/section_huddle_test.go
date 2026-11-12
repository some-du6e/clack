package ui

import (
	"encoding/json"
	"image"
	"slices"
	"testing"
	"time"

	"gioui.org/f32"
	"gioui.org/io/pointer"

	"github.com/some-du6e/clack/internal/core"
	"github.com/some-du6e/clack/internal/ui/huddleui"
)

// SectionHuddleIndicator (render 2LNF) on the whole client over the fake:
// a collapsed section holding a channel with a huddle on ends its heading
// with the huddle's two faces under huddle_section_indicator, not without
// it, and not while the section is open; hovering the faces shows
// HuddleChannelTooltip; clicking them doesn't toggle the section (they
// sit outside toggleAndLabel); the heading's ⋯ moves left of them.
func TestSectionHuddleIndicator(t *testing.T) {
	e, fs := filterEnv(t, nil)
	a := e.a
	ch := fakeslackMemberChannel(e)
	if ch == "" {
		t.Fatal("no member channel in the fake")
	}
	var sec *sbSection
	for i := range a.model().Sections {
		if slices.Contains(a.model().Sections[i].All, ch) {
			sec = &a.model().Sections[i]
			break
		}
	}
	if sec == nil {
		t.Fatalf("no section holds %s", ch)
	}
	key := sec.ID
	users := fs.GroupDMUsers()
	raw, _ := json.Marshal(map[string]any{"user": users[0].ID, "room": map[string]any{
		"id": "R0SECTION", "channels": []string{ch}, "participants": []string{users[0].ID, users[1].ID},
		"date_start": time.Now().Unix() - 60, "name": ""}})
	a.huddles().Handle(core.Event{Type: "huddle", Name: "sh_room_join", Channel: ch, Raw: raw})
	heading := func() *sbRow {
		for i := range a.sidebar.rows {
			if r := &a.sidebar.rows[i]; r.kind == sbHeading && r.key == key {
				return r
			}
		}
		t.Fatalf("no heading %s", key)
		return nil
	}
	a.sbSetCollapsed(heading(), true)
	e.settle(200 * time.Millisecond)
	if !a.sbCollapsed(heading()) {
		t.Fatal("the section didn't collapse")
	}
	if got := a.sbHeadingHuddle(heading()); got != "" {
		t.Errorf("experiment off: the heading shows %q's huddle", got)
	}
	a.Core().SetExperimentGroup(huddleui.ExpSectionIndicator, "on")
	e.settle(150 * time.Millisecond)
	if got := a.sbHeadingHuddle(heading()); got != ch {
		t.Fatalf("collapsed with the experiment on: %q, want %s", got, ch)
	}

	// find the faces: sweep the heading from the sidebar's right edge
	p := e.sbRowPoint(t, "sectionHeading-"+key)
	right := a.sidebar.origin.X + a.sidebar.width
	hit := image.Point{}
	for x := right; x > right-80; x -= 2 {
		e.r.Queue(pointer.Event{Kind: pointer.Move, Source: pointer.Mouse, Position: f32.Pt(float32(x), float32(p.Y))})
		e.frame()
		if a.huddles().SectionIndicatorHovered(e.context(), key) {
			hit = image.Pt(x-6, p.Y)
			break
		}
	}
	if hit == (image.Point{}) {
		t.Fatal("no SectionHuddleIndicator in the collapsed heading")
	}
	e.r.Queue(pointer.Event{Kind: pointer.Move, Source: pointer.Mouse, Position: f32.Pt(float32(hit.X), float32(hit.Y))})
	e.settle(300 * time.Millisecond) // past the tooltip's 150 ms; the heading is hovered, so ⋯ shows left of the faces
	e.shotBoth("section-huddle-indicator-tip")
	// a click on the faces leaves the section collapsed
	e.click(hit)
	e.frame()
	e.frame()
	if !a.sbCollapsed(heading()) {
		t.Error("clicking the indicator expanded the section")
	}
	// the label still toggles it
	e.click(p)
	e.frame()
	e.frame()
	if a.sbCollapsed(heading()) {
		t.Error("clicking the heading's label didn't expand it")
	}
	if got := a.sbHeadingHuddle(heading()); got != "" {
		t.Errorf("open section: the heading shows %q's huddle", got)
	}
	e.settle(150 * time.Millisecond)
	e.shotBoth("section-huddle-indicator-open")
}
