package ui

// Clack's sidebar channel rows, for side-by-sides with the same rows in
// Slack's markup under Slack's shipping CSS (.github/scripts/sidebar_rows_ref.py,
// the font-shots workflow's sidebar-rows jobs). With SIDEBAR_ROWS_DIR set it
// draws the client over fakeslack's Unreads fixtures (synthetic data,
// nothing talks to Slack) at 1x and 1.25x, dark and light, and saves the
// rows cropped from the window (clack-<mode>-<scale>x-<id>.png) with
// rows.json: each row's name, state and place.

import (
	"encoding/json"
	"fmt"
	"image"
	"image/png"
	"os"
	"path/filepath"
	"testing"
	"time"

	"gioui.org/unit"

	"github.com/some-du6e/clack/internal/fakeslack"
)

type sbShotRow struct {
	ID       string `json:"id"`
	Name     string `json:"name"`
	Unread   bool   `json:"unread"`
	Muted    bool   `json:"muted"`
	Selected bool   `json:"selected"`
	Hover    bool   `json:"hover"`
	Mentions int    `json:"mentions"`
	Y        int    `json:"y"` // px, the row's top in the window
	H        int    `json:"h"`
}

type sbShot struct {
	File  string      `json:"file"` // the crops' prefix: <file>-<id>.png
	Scale float32     `json:"scale"`
	Dark  bool        `json:"dark"`
	X     int         `json:"x"` // px, the sidebar's left in the window
	W     int         `json:"w"` // px, its width
	WDp   int         `json:"w_dp"`
	Rows  []sbShotRow `json:"rows"`
	// Vars are the inverted sidebar's tokens a light client gives its
	// sidebar (theme_scope.go), for Slack's markup
	Vars map[string]string `json:"vars,omitempty"`
}

func TestSidebarRowShots(t *testing.T) {
	dir := os.Getenv("SIDEBAR_ROWS_DIR")
	if dir == "" {
		t.Skip("SIDEBAR_ROWS_DIR unset (the font-shots workflow's sidebar-rows jobs)")
	}
	t.Setenv("CLACK_BENCH_GPU", "1")
	var shots []sbShot
	for _, scale := range []float32{1, 1.25} {
		sz := fakeslack.Small
		sz.Unreads = true
		fs, err := fakeslack.Start(sz)
		if err != nil {
			t.Fatal(err)
		}
		t.Cleanup(func() { fs.Close() })
		e := startEnv(t, fs, t.TempDir())
		if e.gpu == nil {
			t.Fatal("no GPU renderer")
		}
		e.scale = scale
		e.waitReady()
		e.until("the unread fixtures", 20*time.Second, func() bool {
			c := e.a.model().conv(fakeslack.UnreadsLaunchID)
			return c != nil && c.Unread && c.Mentions > 0
		})
		ids := []string{fakeslack.JoinBannerID, fakeslack.JoinBannerOldID, fakeslack.UnreadsDesignID, fakeslack.UnreadsLaunchID, fakeslack.UnreadsMutedID}
		for _, dark := range []bool{true, false} {
			e.setDark(dark)
			e.openChannel(fakeslack.JoinBannerID) // selected
			e.settle(300 * time.Millisecond)
			s := &e.a.sidebar
			mode := "light"
			if dark {
				mode = "dark"
			}
			sh := sbShot{Scale: scale, Dark: dark, X: s.origin.X, W: e.context().Dp(unit.Dp(s.width)), WDp: s.width, Vars: e.a.scopeVars}
			for _, id := range ids {
				if s.sbPos(id) < 0 { // the sidebar's filters can leave a fixture out (muted)
					t.Logf("%s: not in the sidebar, skipped", id)
					continue
				}
				// the row in view, the pointer on it only for the hovered one
				pt := e.sbRowPoint(t, id)
				hover := id == fakeslack.JoinBannerOldID
				if !hover {
					pt = e.listPoint()
				}
				e.movePtr(pt)
				e.settle(400 * time.Millisecond)
				c := e.a.model().conv(id)
				p := s.sbPos(id)
				if c == nil || p < 0 || !s.inView(p, 0) {
					t.Fatalf("%s: no row in view", id)
				}
				r := sbShotRow{ID: id, Name: c.displayName(), Unread: c.Unread, Muted: c.Muted,
					Selected: id == fakeslack.JoinBannerID, Hover: hover, Mentions: c.Mentions,
					Y: s.origin.Y + s.listTop + s.kb.rowOff[p] - s.scrollTop(), H: s.kb.rowOff[p+1] - s.kb.rowOff[p]}
				img := image.NewRGBA(image.Rectangle{Max: e.size})
				if err := e.gpu.Screenshot(img); err != nil {
					t.Fatal(err)
				}
				crop := img.SubImage(image.Rect(sh.X, r.Y, sh.X+sh.W, r.Y+r.H))
				f, err := os.Create(filepath.Join(dir, fmt.Sprintf("clack-%s-%gx-%s.png", mode, scale, id)))
				if err != nil {
					t.Fatal(err)
				}
				if err := png.Encode(f, crop); err != nil {
					t.Fatal(err)
				}
				f.Close()
				sh.Rows = append(sh.Rows, r)
			}
			sh.File = fmt.Sprintf("clack-%s-%gx", mode, scale)
			shots = append(shots, sh)
		}
	}
	b, _ := json.MarshalIndent(shots, "", " ")
	if err := os.WriteFile(filepath.Join(dir, "rows.json"), b, 0o644); err != nil {
		t.Fatal(err)
	}
}
