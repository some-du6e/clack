package ui

// Every Preferences page Slack has (Availability … Advanced), drawn over
// the whole client signed in to internal/fakeslack at 1440x900, light and
// dark, a shot per screenful of each page (prefs-audit-shots workflow).
// PREFS_AUDIT_SHOTS_DIR gets the PNGs; without it the test only lays the
// pages out. Synthetic data only: nothing reaches Slack.

import (
	"image"
	"image/png"
	"os"
	"path/filepath"
	"strconv"
	"testing"
	"time"

	"gioui.org/gpu/headless"

	"github.com/some-du6e/clack/internal/fakeslack"
)

func TestPrefsAuditShots(t *testing.T) {
	fs, err := fakeslack.Start(fakeslack.Small)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { fs.Close() })
	fs.SetPrefs(map[string]any{"vip_users": "U0VIP1"})
	home := t.TempDir()
	t.Setenv("CLACK_HOME", home)
	e := startEnv(t, fs, home)
	dir := os.Getenv("PREFS_AUDIT_SHOTS_DIR")
	if dir != "" && e.gpu == nil {
		w, err := headless.NewWindow(e.size.X, e.size.Y)
		if err != nil {
			t.Fatalf("headless window: %v", err)
		}
		e.gpu = w
	}
	e.waitReady()
	shot := func(name string) {
		e.settle(150 * time.Millisecond)
		if dir == "" || e.gpu == nil {
			return
		}
		img := image.NewRGBA(image.Rectangle{Max: e.size})
		if err := e.gpu.Screenshot(img); err != nil {
			t.Fatalf("screenshot: %v", err)
		}
		f, err := os.Create(filepath.Join(dir, name+".png"))
		if err != nil {
			t.Fatal(err)
		}
		defer f.Close()
		if err := png.Encode(f, img); err != nil {
			t.Fatal(err)
		}
	}
	var pm *prefsModal
	prefsTap = func(p *prefsModal) { pm = p }
	t.Cleanup(func() { prefsTap = nil })

	// a page's screenfuls: its own scroll, a screen (the page area is
	// 603dp high) at a time
	pages := func(name string) {
		shot(name)
		for i := 2; i <= 8; i++ {
			s := &pm.scroll
			vh := e.a.px(560)
			if s.contentH <= s.y+e.a.px(603) {
				return
			}
			s.y += vh
			shot(name + "-" + strconv.Itoa(i))
		}
	}
	for _, dark := range []bool{false, true} {
		mode := map[bool]string{false: "light", true: "dark"}[dark]
		e.a.setDark(dark)
		e.settle(200 * time.Millisecond)
		for _, it := range prefsNav {
			for e.a.dismissTop() {
			}
			pm = nil
			e.a.showPrefs(it.id)
			e.until("Preferences "+it.id, 5*time.Second, func() bool { return pm != nil })
			pages("prefs-" + it.id + "-" + mode)
			if it.id == "appearance" {
				pm.themeTab, pm.scroll.y = "custom", 0
				pages("prefs-appearance-custom-" + mode)
			}
		}
		for e.a.dismissTop() {
		}
	}
}
