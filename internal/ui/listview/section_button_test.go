package listview

// custom_file_sections in the list header (section_button.go, more.go):
// on, FileSectionDropdownButton before the list's icon and title (a
// click opens its menu), the More menu without the star row (a channel
// tab's with Move to section ▸); off, neither. Light and dark shots with
// the file in the starred collection and in an emoji section
// (list-section-button-*.png).

import (
	"context"
	"image"
	"strings"
	"testing"
	"time"

	"gioui.org/f32"
	"gioui.org/io/input"
	"gioui.org/io/pointer"
	"gioui.org/layout"
	"gioui.org/op"
	"gioui.org/unit"

	"github.com/some-du6e/clack/internal/fakeslack"
)

// sectionLook is the shell's face choice (views.SectionButtonFor) on
// core's store.
func sectionLook(h *testHost, id string) (int, string, bool) {
	a := h.a
	if !a.FileSectionsOn() {
		return 0, "", false
	}
	c, in := a.FileCollectionOf(id)
	switch {
	case !in:
		return SectionStar, "", true
	case c.Starred():
		return SectionStarred, "", true
	case c.Emoji != "":
		u, _, _ := a.Emoji(strings.Trim(c.Emoji, ":"))
		return SectionEmoji, u, true
	}
	return SectionIcon, "", true
}

func TestSectionButton(t *testing.T) {
	h, fs := newEnv(t, false)
	a := h.a
	if a.FileSectionsOn() {
		t.Fatal("custom_file_sections is assigned in fakeslack; the test expects it unassigned (off)")
	}
	var menus []image.Rectangle
	act := Actions{SideView: func() {}, Copy: func(string) {}, Menu: func([]MenuItem) {},
		SectionButton: func() (int, string, bool) { return sectionLook(h, fakeslack.ListKitchen) },
		SectionMenu:   func(b image.Rectangle) { menus = append(menus, b) },
		SectionItems:  func() any { return "shell rows" },
	}
	v := New(h, fakeslack.ListKitchen, Full, act)
	r := new(input.Router)
	sz := image.Pt(1200, 700)
	frame := func() {
		ops := new(op.Ops)
		gtx := layout.Context{Ops: ops, Now: time.Now(), Metric: unit.Metric{PxPerDp: 1, PxPerSp: 1}, Constraints: layout.Exact(sz), Source: r.Source()}
		v.Layout(gtx)
		r.Frame(ops)
	}
	deadline := time.Now().Add(20 * time.Second)
	for v.Doc() == nil {
		if time.Now().After(deadline) {
			t.Fatal("the list didn't load")
		}
		frame()
		time.Sleep(10 * time.Millisecond)
	}
	frame()
	// the button: 12px in, centred in the 50px header
	btn := f32.Pt(12+14, 25)
	click := func() {
		r.Queue(pointer.Event{Kind: pointer.Move, Source: pointer.Mouse, Position: btn},
			pointer.Event{Kind: pointer.Press, Source: pointer.Mouse, Buttons: pointer.ButtonPrimary, Position: btn},
			pointer.Event{Kind: pointer.Release, Source: pointer.Mouse, Position: btn})
		frame()
		frame()
	}

	// off: no button, the star row
	if v.sectionButtonShown() {
		t.Error("off: the header draws the section button")
	}
	click()
	if len(menus) != 0 {
		t.Errorf("off: a click there opened the section menu %v", menus)
	}
	if got := moreLabels(v.moreItems()); !strings.HasPrefix(got, "Add to starred, ─, Save for later") {
		t.Errorf("off: More menu %s", got)
	}

	// on: the button and its menu; the menu drops the star row
	a.SetExperimentGroup("custom_file_sections", "on")
	frame()
	if !v.sectionButtonShown() {
		t.Fatal("on: no section button")
	}
	if look, _, _ := act.SectionButton(); look != SectionStar {
		t.Errorf("on, in no collection: face %d, want the star", look)
	}
	click()
	if len(menus) != 1 {
		t.Fatalf("on: %d section menus after the click", len(menus))
	}
	if got := moreLabels(v.moreItems()); !strings.HasPrefix(got, "Save for later") || strings.Contains(got, "starred") || strings.Contains(got, "Move to section") {
		t.Errorf("on: More menu %s", got)
	}
	tab := New(h, fakeslack.ListKitchen, Tab, act)
	for tab.Doc() == nil {
		if time.Now().After(deadline) {
			t.Fatal("the tab's list didn't load")
		}
		ops := new(op.Ops)
		tab.Layout(gtxFor(ops, sz))
		time.Sleep(10 * time.Millisecond)
	}
	items := tab.moreItems()
	if len(items) == 0 || items[0].Label != "Move to section" || items[0].Icon != "folder" || items[0].Ext != "shell rows" {
		t.Errorf("on, a channel tab: More menu %s (first %+v)", moreLabels(items), items[0])
	}
	if tab.sectionButtonShown() {
		t.Error("a channel tab draws the header's section button")
	}

	// the faces, as the shell picks them: in Starred, in an emoji section
	ctx := context.Background()
	if err := a.FetchFileCollections(ctx); err != nil {
		t.Fatal(err)
	}
	if _, err := a.MoveFileToCollection(ctx, fakeslack.ListKitchen, fakeslack.StarredCollectionID); err != nil {
		t.Fatal(err)
	}
	if look, _, _ := act.SectionButton(); look != SectionStarred {
		t.Errorf("in Starred: face %d", look)
	}
	_ = fs
	for _, dark := range []bool{false, true} {
		mode := map[bool]string{false: "light", true: "dark"}[dark]
		hh, fs2 := newEnv(t, dark)
		launch := fs2.AddFileCollection("Launch", ":rocket:")
		hh.a.SetExperimentGroup("custom_file_sections", "on")
		if err := hh.a.FetchFileCollections(ctx); err != nil {
			t.Fatal(err)
		}
		sact := act
		sact.SectionButton = func() (int, string, bool) { return sectionLook(hh, fakeslack.ListKitchen) }
		sv := New(hh, fakeslack.ListKitchen, Full, sact)
		s := newShooter(t, image.Pt(900, 220))
		w := func(gtx layout.Context) { sv.Layout(gtx) }
		settle(t, s, w, "the list", func() bool { return sv.Doc() != nil })
		for _, to := range []string{fakeslack.StarredCollectionID, launch} {
			if _, err := hh.a.MoveFileToCollection(ctx, fakeslack.ListKitchen, to); err != nil {
				t.Fatal(err)
			}
			look, _, _ := sact.SectionButton()
			face := map[int]string{SectionStarred: "starred", SectionEmoji: "emoji"}[look]
			if face == "" {
				t.Fatalf("in %s: face %d", to, look)
			}
			s.shot("list-section-button-"+face+"-"+mode, w)
		}
	}
}
