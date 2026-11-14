package ui

// The canvas and list headers' FileSectionDropdownButton as the shell
// wires it (file_section_button.go): off, no button; on, the face follows
// the file's collection and the menu is FileSectionMoveToMenu (Move to…,
// the collections with the current one checked, Create section, Remove
// from {name}); a channel tab's "Move to section" ▸ carries the same rows.

import (
	"context"
	"slices"
	"testing"

	"github.com/some-du6e/clack/internal/core"
	"github.com/some-du6e/clack/internal/fakeslack"
	"github.com/some-du6e/clack/internal/ui/canvasview"
	"github.com/some-du6e/clack/internal/ui/listview"
)

func TestFileSectionHeaderWiring(t *testing.T) {
	fs, err := fakeslack.Start(fakeslack.Small)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { fs.Close() })
	e := startEnv(t, fs, t.TempDir())
	e.waitReady()
	a := e.a
	c := a.Core()
	if _, _, ok := a.fileSectionButton(fakeslack.CanvasShared); ok {
		t.Error("custom_file_sections unassigned (fakeslack): the button shows")
	}
	if items := a.fileSectionItems(core.File{ID: fakeslack.CanvasShared, IsCanvas: true}, "canvas_header"); len(items) != 0 {
		t.Errorf("off: menu rows %d", len(items))
	}
	launch := fs.AddFileCollection("Launch", ":rocket:", fakeslack.CanvasShared)
	c.SetExperimentGroup("custom_file_sections", "on")
	if err := c.FetchFileCollections(context.Background()); err != nil {
		t.Fatal(err)
	}
	look, _, ok := a.fileSectionButton(fakeslack.CanvasShared)
	if !ok || look != canvasview.SectionEmoji {
		t.Errorf("in Launch (:rocket:): face %d %v, want the emoji", look, ok)
	}
	if look, _, _ := a.fileSectionButton(fakeslack.ListKitchen); look != canvasview.SectionStar || canvasview.SectionStar != listview.SectionStar {
		t.Errorf("in no collection: face %d, want the star", look)
	}
	items := a.fileSectionItems(core.File{ID: fakeslack.CanvasShared, IsCanvas: true, Title: "Shared canvas"}, "canvas_header")
	var labels []string
	checked := ""
	for _, it := range items {
		switch {
		case it.Divider:
			labels = append(labels, "-")
		default:
			labels = append(labels, it.Label)
		}
		if it.Radio && it.Checked {
			checked = it.Label
		}
	}
	if len(labels) == 0 || labels[0] != "Move to…" || !items[0].Header || !slices.Contains(labels, "Launch") ||
		!slices.Contains(labels, "Create section") || !slices.Contains(labels, "Remove from Launch") || checked != "Launch" {
		t.Errorf("menu %q (checked %q)", labels, checked)
	}
	// the tab menu's submenu: listview hands the shell's rows through Ext
	got := listMenuItems([]listview.MenuItem{{Label: "Move to section", Icon: "folder", Ext: a.fileSectionItems(core.File{ID: fakeslack.ListKitchen, IsList: true}, "canvas_overflow_menu")}})
	if len(got) != 1 || len(got[0].Sub) == 0 || got[0].Sub[0].Label != "Move to…" {
		t.Errorf("Move to section ▸ %+v", got)
	}
	_ = launch
}
