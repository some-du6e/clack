package ui

import (
	"image"
	"testing"
	"time"

	"github.com/some-du6e/clack/internal/ui/popups"
)

// Karim, 2026-11-12: "you cant even make a channel here bc it flickers".
// The Channels heading's ⋯ and + show on Slack's
// .p-channel_sidebar__section_heading:hover, the whole row's, buttons
// included. Moving the pointer along the heading onto ⋯ and then + keeps
// them up on every frame in between, and clicking + opens the
// create-channel modal (SectionMenuPlusButton: "Create a channel").
func TestSectionHeadingButtonsStayHovered(t *testing.T) {
	e, _ := previewEnv(t)
	e.settle(300 * time.Millisecond)
	a := e.a
	var id string
	for _, s := range a.model().Sections {
		if s.Type == "channels" {
			id = s.ID
			break
		}
	}
	if id == "" {
		t.Fatal("no Channels section")
	}
	from := e.sbRowPoint(t, "sectionHeading-"+id)
	// the buttons at the row's right (sbHeadingControls): + 8px in, ⋯ 2px
	// before it, 24px each
	right := a.sidebar.origin.X + a.sidebar.width
	plus := image.Pt(right-8-12, from.Y)
	more := image.Pt(right-8-24-2-12, from.Y)

	e.movePtr(from)
	e.frame()
	if a.sidebar.headCtl != id {
		t.Fatalf("pointer on the heading: its buttons aren't drawn (headCtl %q)", a.sidebar.headCtl)
	}
	sawMore := false
	for x := from.X; x <= plus.X; x += 2 {
		e.movePtr(image.Pt(x, from.Y))
		if a.sidebar.headCtl != id {
			t.Fatalf("pointer at x=%d (⋯ at %d, + at %d): the heading's buttons went away", x, more.X, plus.X)
		}
		if x >= more.X-4 && x <= more.X+4 && a.clicks.get("sbh.more."+id).Hovered() {
			sawMore = true
		}
	}
	if !sawMore {
		t.Error("the pointer never hovered ⋯ on its way to +")
	}
	for range 3 { // resting on +: still steady
		e.frame()
		if a.sidebar.headCtl != id {
			t.Fatal("resting on +: the heading's buttons went away")
		}
	}
	if !a.clicks.get("sbh.plus." + id).Hovered() {
		t.Fatalf("+ at %v isn't hovered", plus)
	}
	e.click(plus)
	e.frame()
	e.frame()
	if _, ok := topOverlay[*popups.CreateChannelModal](a); !ok {
		t.Fatal("clicking + didn't open the create-channel modal")
	}
	if a.sbCollapsed(&sbRow{kind: sbHeading, key: id, section: id}) {
		t.Error("clicking + toggled the section too")
	}
}
