package canvasview

// File sections in the canvas header (section_button.go):
// FileSectionDropdownButton before the title in the side panel and the
// full view (not in a channel tab), a click opens its menu, in every
// group: Slack dropped custom_file_sections and kept its on path (the
// 2026-11-21 drift), so the old gate forced off changes nothing. Light and
// dark shots with the canvas in the starred collection
// and in an emoji section (canvas-section-button-*.png).

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

// sectionLook is the shell's face choice (views.SectionButtonFor).
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
	h, _ := newEnv(t, false)
	a := h.a
	a.SetExperimentGroup("custom_file_sections", "off")
	var menus []image.Rectangle
	act := Actions{Close: func() {}, FullView: func() {},
		SectionButton: func() (int, string, bool) { return sectionLook(h, fakeslack.CanvasShared) },
		SectionMenu:   func(b image.Rectangle) { menus = append(menus, b) },
	}
	v := New(h, fakeslack.CanvasShared, Side, act)
	r := new(input.Router)
	sz := image.Pt(520, 700)
	frame := func() {
		ops := new(op.Ops)
		gtx := layout.Context{Ops: ops, Now: time.Now(), Metric: unit.Metric{PxPerDp: 1, PxPerSp: 1}, Constraints: layout.Exact(sz), Source: r.Source()}
		v.Layout(gtx)
		r.Frame(ops)
	}
	deadline := time.Now().Add(20 * time.Second)
	for v.Doc() == nil {
		if time.Now().After(deadline) {
			t.Fatal("the canvas didn't load")
		}
		frame()
		time.Sleep(10 * time.Millisecond)
	}
	frame()
	// the button: 18px in (the header's padding), centred in the 49px bar
	btn := f32.Pt(18+14, 24)
	click := func() {
		r.Queue(pointer.Event{Kind: pointer.Move, Source: pointer.Mouse, Position: btn},
			pointer.Event{Kind: pointer.Press, Source: pointer.Mouse, Buttons: pointer.ButtonPrimary, Position: btn},
			pointer.Event{Kind: pointer.Release, Source: pointer.Mouse, Position: btn})
		frame()
		frame()
	}
	if !v.sectionButtonShown() {
		t.Fatal("on: no section button")
	}
	click()
	if len(menus) != 1 {
		t.Fatalf("on: %d section menus after the click", len(menus))
	}
	if New(h, fakeslack.CanvasShared, Tab, act).sectionButtonShown() {
		t.Error("a channel tab draws the header's section button")
	}

	ctx := context.Background()
	for _, dark := range []bool{false, true} {
		mode := map[bool]string{false: "light", true: "dark"}[dark]
		hh, fs2 := newEnv(t, dark)
		launch := fs2.AddFileCollection("Launch", ":rocket:")
		if err := hh.a.ForceFetchFileCollections(ctx); err != nil {
			t.Fatal(err)
		}
		sact := act
		sact.SectionButton = func() (int, string, bool) { return sectionLook(hh, fakeslack.CanvasShared) }
		sv := New(hh, fakeslack.CanvasShared, Side, sact)
		s := newShooter(t, image.Pt(520, 240))
		w := func(gtx layout.Context) { sv.Layout(gtx) }
		settle(t, s, sv, "the canvas", func() bool { return sv.Doc() != nil })
		for _, to := range []string{fakeslack.StarredCollectionID, launch} {
			if _, err := hh.a.MoveFileToCollection(ctx, fakeslack.CanvasShared, to); err != nil {
				t.Fatal(err)
			}
			look, _, _ := sact.SectionButton()
			face := map[int]string{SectionStarred: "starred", SectionEmoji: "emoji"}[look]
			if face == "" {
				t.Fatalf("in %s: face %d", to, look)
			}
			for i := 0; i < 6; i++ { // the emoji's image arrives asynchronously
				s.frame(w)
				time.Sleep(30 * time.Millisecond)
			}
			s.shot("canvas-section-button-"+face+"-"+mode, s.frame(w))
		}
	}
}
