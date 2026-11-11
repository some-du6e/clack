//go:build linux && !android && !nox11

package huddleui

import (
	"fmt"
	"image"
	"os"
	"os/exec"
	"strings"
	"testing"
	"time"

	"github.com/some-du6e/clack/internal/huddle"
)

// TestMiniPanelShowsInactive opens the presenter's mini panel as a real X11
// window (Xvfb in huddles-audit-shots) and checks it was mapped the way
// Electron's showInactive maps one: _NET_WM_USER_TIME 0, so the window
// manager leaves the focus where it was (Gio's app.Panel, Clack's Gio
// patch §7).
func TestMiniPanelShowsInactive(t *testing.T) {
	if os.Getenv("DISPLAY") == "" || os.Getenv("WAYLAND_DISPLAY") != "" {
		t.Skip("no X11 display")
	}
	xprop, err := exec.LookPath("xprop")
	if err != nil {
		t.Skip("no xprop (x11-utils)")
	}
	e := newShotEnv(t, false)
	p := &miniPanel{c: e.c, src: huddle.ShareSource{ID: "screen:1:0", Name: "Entire Screen", Bounds: image.Rect(0, 0, 1280, 720)}}
	go p.run()
	defer p.close()
	var h huddle.PanelHandle
	for deadline := time.Now().Add(20 * time.Second); ; {
		p.mu.Lock()
		have, closed := p.have, p.closed
		h = p.handle
		p.mu.Unlock()
		if have {
			break
		}
		if closed {
			t.Fatal("the panel's window closed before its X11ViewEvent")
		}
		if time.Now().After(deadline) {
			t.Fatal("no X11 window for the panel")
		}
		time.Sleep(20 * time.Millisecond)
	}
	out, err := exec.Command(xprop, "-id", fmt.Sprintf("0x%x", h.View), "_NET_WM_USER_TIME").CombinedOutput()
	if err != nil {
		t.Fatalf("xprop: %v: %s", err, out)
	}
	if got := strings.TrimSpace(string(out)); got != "_NET_WM_USER_TIME(CARDINAL) = 0" {
		t.Errorf("the panel's _NET_WM_USER_TIME is %q, want 0 (shown inactive)", got)
	}
}
