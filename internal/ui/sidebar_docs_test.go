package ui

// Canvases and lists in the sidebar (core/sidebar_docs.go) over fakeslack
// Size.FileChannels: synthetic file channels shaped like the real
// account's. Nothing here talks to Slack. With PREVIEW_SHOTS_DIR set
// (the sidebar-experiments workflow) the rows are saved light and dark.

import (
	"strings"
	"testing"
	"time"

	"github.com/some-du6e/clack/internal/core"
	"github.com/some-du6e/clack/internal/fakeslack"
	"github.com/some-du6e/clack/internal/ui/theme/pixeltest"
)

func docsEnv(t *testing.T, groups map[string]string) *perfEnv {
	t.Helper()
	sz := fakeslack.Small
	sz.FileChannels = true
	fs, err := fakeslack.Start(sz)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { fs.Close() })
	e := startEnv(t, fs, t.TempDir())
	if organizeShotsDir() != "" && e.gpu == nil {
		w, err := pixeltest.NewWindow(e.size.X, e.size.Y)
		if err != nil {
			t.Fatalf("headless window: %v", err)
		}
		e.gpu = w
	}
	e.waitReady()
	c := e.a.data.core
	for _, g := range []string{core.ExpListsPinToSidebar, core.ExpProgrammableSidebar, core.ExpSidebarIA6} {
		c.SetExperimentGroup(g, groups[g]) // "" = not assigned, Slack's default
	}
	e.settle(300 * time.Millisecond)
	return e
}

// sbRowHeight is the drawn height of the sidebar row with item key k.
func (e *perfEnv) sbRowHeight(t *testing.T, k string) int {
	t.Helper()
	s := &e.a.sidebar
	p := s.sbPos(k)
	if p < 0 {
		t.Fatalf("no sidebar row %q", k)
	}
	return s.kb.rowOff[p+1] - s.kb.rowOff[p]
}

// With GH off (no lists_pin_to_sidebar, programmable_sidebar or
// sidebar_ia6) a canvas's or list's conversation isn't in the sidebar,
// the model or the switcher's conversations, even in a section.
func TestSidebarDocsHiddenByDefault(t *testing.T) {
	e := docsEnv(t, nil)
	e.until("sidebar rows", 10*time.Second, func() bool { return len(e.a.model().Sections) > 0 })
	for _, id := range []string{fakeslack.FileChanSnacks, fakeslack.FileChanStatus, fakeslack.FileChanKitchen} {
		if e.sidebarHas(id) {
			t.Errorf("%s has a sidebar row with GH off", id)
		}
		if e.a.model().conv(id) != nil {
			t.Errorf("%s is in the model's conversations", id)
		}
		for _, c := range e.a.data.core.Snapshot().Convs {
			if c.ID == id {
				t.Errorf("%s is in the snapshot (the switcher, pickers and unread totals read it)", id)
			}
		}
	}
	e.organizeShot(t, "sidebar-docs-hidden-light")
}

// programmable_sidebar: the section's canvas and list are document rows
// (their icons, the canvas's :emoji: drawn), as tall as a channel row; a
// click opens the canvas view or the list view, and the row is selected
// while its document is open.
func TestSidebarDocRows(t *testing.T) {
	e := docsEnv(t, map[string]string{core.ExpProgrammableSidebar: "on"})
	a := e.a
	e.until("document rows", 20*time.Second, func() bool {
		cv, lv := a.model().conv(fakeslack.FileChanSnacks), a.model().conv(fakeslack.FileChanKitchen)
		return cv != nil && lv != nil && cv.Doc == "canvas" && lv.Doc == "list" &&
			e.sidebarHas(fakeslack.FileChanSnacks) && e.sidebarHas(fakeslack.FileChanKitchen)
	})
	if e.sidebarHas(fakeslack.FileChanStatus) {
		t.Error("the file channel no section lists has a row")
	}
	in := sbConvs(a, fakeslack.FileChanSection)
	if !strings.Contains(strings.Join(in, ","), fakeslack.FileChanSnacks) {
		t.Errorf("Projects rows %v, want the canvas's", in)
	}
	if ic := a.sbDocIcon("canvas"); ic != "canvas" {
		t.Errorf("canvas icon %q", ic)
	}
	if ic := a.sbDocIcon("list"); ic != "lists" {
		t.Errorf("list icon %q under programmable_sidebar", ic)
	}
	// the title's :shopping_trolley: is an emoji, not text
	var d dropDrawn
	d.hook(t)
	e.settle(150 * time.Millisecond)
	if d.has(fakeslack.CanvasSnacksTitle) {
		t.Errorf("the canvas row drew %q as text", fakeslack.CanvasSnacksTitle)
	}
	if !d.has(" Snack run") {
		t.Error("the canvas row didn't draw the title's text after the emoji")
	}
	if !d.has("Launch tracker") {
		t.Error("the list row's name wasn't drawn")
	}
	// row heights stay those of a channel row
	ch := ""
	for _, id := range in {
		if c := a.model().conv(id); c != nil && !c.File {
			ch = id
			break
		}
	}
	if ch == "" {
		t.Fatal("no channel row in Projects to compare with")
	}
	if h, want := e.sbRowHeight(t, fakeslack.FileChanSnacks), e.sbRowHeight(t, ch); h != want {
		t.Errorf("canvas row %dpx, channel row %dpx", h, want)
	}
	e.organizeShot(t, "sidebar-docs-light")

	e.clickWith(e.sbRowPoint(t, fakeslack.FileChanSnacks), 0)
	e.until("the canvas view", 10*time.Second, func() bool { return string(a.router.Current()) == "docs/"+fakeslack.CanvasSnacks })
	if !sbDocSelected(a.model().conv(fakeslack.FileChanSnacks), a.router.Current()) {
		t.Error("the canvas row isn't selected while its canvas is open")
	}
	if sbDocSelected(a.model().conv(fakeslack.FileChanKitchen), a.router.Current()) {
		t.Error("the list row is selected while the canvas is open")
	}
	e.settle(500 * time.Millisecond)
	e.organizeShot(t, "sidebar-docs-canvas-open-light")

	e.clickWith(e.sbRowPoint(t, fakeslack.FileChanKitchen), 0)
	e.until("the list view", 10*time.Second, func() bool { return string(a.router.Current()) == "lists/"+fakeslack.ListKitchen })
	e.settle(500 * time.Millisecond)
	e.organizeShot(t, "sidebar-docs-list-open-light")

	a.setDark(true)
	e.settle(300 * time.Millisecond)
	e.organizeShot(t, "sidebar-docs-list-open-dark")
	e.clickWith(e.sbRowPoint(t, fakeslack.FileChanSnacks), 0)
	e.until("the canvas view again", 10*time.Second, func() bool { return string(a.router.Current()) == "docs/"+fakeslack.CanvasSnacks })
	e.settle(500 * time.Millisecond)
	e.organizeShot(t, "sidebar-docs-canvas-open-dark")
	a.Navigate("c/" + ch)
	e.settle(300 * time.Millisecond)
	e.organizeShot(t, "sidebar-docs-dark")
}

// lists_pin_to_sidebar alone: the list is a document row with the
// bullet-list icon; the canvas stays a private channel row (Slack wants
// programmable_sidebar for a canvas document row), its name as plain
// text, and opens as a conversation.
func TestSidebarDocsListsPinOnly(t *testing.T) {
	e := docsEnv(t, map[string]string{core.ExpListsPinToSidebar: "on"})
	a := e.a
	e.until("rows", 20*time.Second, func() bool {
		lv := a.model().conv(fakeslack.FileChanKitchen)
		return lv != nil && lv.Doc == "list" && e.sidebarHas(fakeslack.FileChanSnacks)
	})
	e.settle(500 * time.Millisecond) // the canvas's files.info
	cv := a.model().conv(fakeslack.FileChanSnacks)
	if cv == nil || cv.Doc != "" || cv.Kind != "private" || cv.NameSpans != nil {
		t.Fatalf("canvas row %+v, want a private channel row", cv)
	}
	if ic := a.sbDocIcon("list"); ic != "bullet-list" {
		t.Errorf("list icon %q", ic)
	}
	var d dropDrawn
	d.hook(t)
	e.settle(150 * time.Millisecond)
	if !d.has(fakeslack.CanvasSnacksTitle) {
		t.Errorf("the canvas row didn't draw its name %q as text", fakeslack.CanvasSnacksTitle)
	}
	e.organizeShot(t, "sidebar-docs-lists-pin-light")
	e.clickWith(e.sbRowPoint(t, fakeslack.FileChanSnacks), 0)
	e.until("the conversation", 10*time.Second, func() bool { return a.router.Current().Channel() == fakeslack.FileChanSnacks })
	e.clickWith(e.sbRowPoint(t, fakeslack.FileChanKitchen), 0)
	e.until("the list view", 10*time.Second, func() bool { return string(a.router.Current()) == "lists/"+fakeslack.ListKitchen })
	a.setDark(true)
	e.settle(300 * time.Millisecond)
	e.organizeShot(t, "sidebar-docs-lists-pin-dark")
}
