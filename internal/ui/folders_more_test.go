package ui

// folders-2 on the whole client against internal/fakeslack's
// #pins-gallery (synthetic only): files dragged onto a folder tab
// (FileDragDrop location folder_tab: FolderTabDragOverlay, then
// uploadFilesToChannelFolder's prompt and the files in the folder), the
// add menu's file row opening the unified file picker, and the empty
// folder's picture. PREVIEW_SHOTS_DIR (channel-actions-shots) saves light
// and dark shots.

import (
	"os"
	"path/filepath"
	"testing"
	"time"

	"gioui.org/app"

	"github.com/some-du6e/clack/internal/fakeslack"
	"github.com/some-du6e/clack/internal/ui/popups"
	"github.com/some-du6e/clack/internal/ui/views"
)

func (e *perfEnv) topModal(title string) *popups.Modal {
	for i := len(e.a.overlays) - 1; i >= 0; i-- {
		if m, ok := e.a.overlays[i].(*popups.Modal); ok && !m.Dismissed() && m.Title() == title {
			return m
		}
	}
	return nil
}

func TestFolderDropLikeSlack(t *testing.T) {
	e, fs := pfEnv(t, "PREVIEW_SHOTS_DIR")
	e.a.motionOverride = 1 // reduced motion: the overlay's last frame at once
	ch := fakeslack.PinsChannelID
	e.openChannel(ch)
	e.until("the tabs", 10*time.Second, func() bool { return e.a.data.tabsDone[ch] })
	later := pfTab(t, e, ch, "folder", "Later")
	pfClickTab(t, e, ch, later)
	v := e.a.folderTab(ch, later)
	e.until("the empty folder", 10*time.Second, func() bool { e.frame(); return v.Loaded() })
	pfShot(e, "folder-tab-empty-picture")

	// the zone and its overlay
	var fd *folderDrop
	var zone dropZone
	e.until("the folder's drop zone", 5*time.Second, func() bool {
		e.frame()
		for _, z := range e.a.drop.zones {
			if z.fd != nil {
				fd, zone = z.fd, z
			}
		}
		return fd != nil
	})
	if fd.name != "Later" || fd.ch != ch {
		t.Errorf("zone %+v", fd)
	}
	dir := t.TempDir()
	a, b := filepath.Join(dir, "notes.txt"), filepath.Join(dir, "plan.md")
	if err := os.WriteFile(a, []byte("synthetic notes"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(b, []byte("# synthetic plan"), 0o600); err != nil {
		t.Fatal(err)
	}
	e.a.fileDrop(dropAt(app.FileDragEnter, centre(zone.r), 0, a, b))
	if e.a.drop.hover != any(fd) {
		t.Fatalf("over the folder: hover %v", e.a.drop.hover)
	}
	pfShotKeep(e, "folder-drop-overlay")
	e.a.fileDrop(dropAt(app.FileDrop, centre(zone.r), 0, a, b))
	if e.a.drop.on || e.a.drop.hover != nil {
		t.Fatal("the overlay stayed after the drop")
	}
	var m *popups.Modal
	e.until("the share prompt", 10*time.Second, func() bool { e.frame(); m = e.topModal("Share these files?"); return m != nil })
	pfShot(e, "folder-drop-share")
	m.Press("Share")
	e.until("the files in the folder", 20*time.Second, func() bool { e.frame(); return len(v.Rows()) == 2 })
	for _, f := range fs.PinsFoldersForms("bookmarks.add") {
		if f.Get("type") != "file" || f.Get("parent_id") != fakeslack.PinsEmptyFolderID {
			t.Errorf("bookmarks.add %v", f)
		}
	}
	e.settle(500 * time.Millisecond)
	pfShot(e, "folder-tab-uploaded")

	// the add menu's file row opens the unified file picker
	e.a.Core().SetExperimentGroup("unified_file_picker", "on")
	for _, it := range v.AddMenuItems() {
		if it.Key == "file" {
			it.Do()
		}
	}
	e.frame()
	var p *views.FilePicker
	for _, o := range e.a.overlays {
		if fp, ok := o.(*views.FilePicker); ok {
			p = fp
		}
	}
	if p == nil {
		t.Fatal("no file picker")
	}
	e.until("the picker's files", 10*time.Second, func() bool { e.frame(); return len(p.Files()) == 2 })
	pfShot(e, "folder-file-picker")
}

// pfShotKeep saves light and dark shots without settling (the drag
// overlay lasts only while the drag is on).
func pfShotKeep(e *perfEnv, name string) {
	for _, dark := range []bool{false, true} {
		e.setDark(dark)
		e.frame()
		e.frame()
		e.shot(name + "-" + map[bool]string{false: "light", true: "dark"}[dark])
	}
	e.setDark(false)
	e.frame()
}
