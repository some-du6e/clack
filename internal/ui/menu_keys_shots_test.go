package ui

import (
	"image"
	"os"
	"testing"
	"time"

	"github.com/some-du6e/yoloslackgoclient/internal/core"
	"github.com/some-du6e/yoloslackgoclient/internal/fakeslack"
	"github.com/some-du6e/yoloslackgoclient/internal/ui/popups"
	"github.com/some-du6e/yoloslackgoclient/internal/ui/shortcut"
)

// With MENU_KEYS_SHOTS_DIR set (the menu-keys workflow, under xvfb) the
// message context menu is saved with macOS hints (⌘C, ⇧⌘S) and with
// Windows/Linux ones (Ctrl+C, Ctrl+Shift+S), light and dark, plus the
// text selection menu and the Create menu (⌘N, ⇧⌘N).
func TestMenuKeysShots(t *testing.T) {
	dir := os.Getenv("MENU_KEYS_SHOTS_DIR")
	if dir == "" {
		t.Skip("MENU_KEYS_SHOTS_DIR not set")
	}
	t.Setenv("CLACK_BENCH_GPU", "1")
	prevOS := shortcut.GOOS
	t.Cleanup(func() { shortcut.GOOS = prevOS })
	e, _ := sectionEnv(t)
	a := e.a
	closeAll := func() {
		for _, o := range a.overlays {
			if d, ok := o.(dismisser); ok {
				d.Dismiss()
			}
		}
		a.overlays = nil
		e.frame()
	}
	ch := ""
	if s := sectionOfType(a, "channels"); s != nil && len(s.Rows) > 0 {
		ch = s.Rows[0]
		a.Navigate("c/" + ch)
	}
	e.settle(800 * time.Millisecond)
	own := core.Message{Channel: ch, TS: "1700000000.000100", User: fakeslack.SelfID, Text: "a synthetic message of mine"}
	for _, c := range []struct {
		goos, name string
	}{{"darwin", "mac"}, {"linux", "ctrl"}} {
		shortcut.GOOS = c.goos
		for _, dark := range []bool{false, true} {
			a.th.SetDark(dark) // as taut_prefs_test's shots do
			a.Invalidate()
			e.settle(6 * time.Second) // llvmpipe frames are slow; tokens restyle over several
			if a.th.Dark != dark {
				t.Fatalf("dark=%v didn't stick", dark)
			}
			mode := "light"
			if dark {
				mode = "dark"
			}
			shot := func(name string) {
				e.frame()
				e.frame()
				e.frame()
				e.shotTo(t, dir, name+"-"+c.name+"-"+mode)
			}
			closeAll()
			a.ShowOverlay(popups.NewMessageContextMenu(a, image.Pt(520, 110), own, popups.InChannel, popups.MessageMenuOpts{}))
			shot("1-message-context-menu")
			closeAll()
			a.ShowOverlay(popups.NewTextSelectionMenu(a, image.Pt(520, 200), "synthetic words"))
			shot("2-text-selection-menu")
			closeAll()
			a.openCreateMenu(image.Rect(8, 120, 44, 156))
			shot("3-create-menu")
		}
	}
	closeAll()
}
