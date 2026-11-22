package composer

import (
	"context"
	"fmt"
	"image"
	"image/png"
	"os"
	"path/filepath"
	"testing"
	"time"

	"gioui.org/io/input"
	"gioui.org/layout"
	"gioui.org/op"
	"gioui.org/op/clip"
	"gioui.org/op/paint"
	"gioui.org/unit"

	"github.com/some-du6e/clack/internal/ui/popups"
	"github.com/some-du6e/clack/internal/ui/slackc"
	"github.com/some-du6e/clack/internal/ui/theme"
	"github.com/some-du6e/clack/internal/ui/theme/pixeltest"
)

// TestDraftsButtonShots draws the composer's bottom right as Karim's
// capture had it (1x, 900px wide, light, then dark) with "hey" typed in a
// channel holding five other drafts: the drafts button with its count
// left of the send button and the formatting tip under the box, then the
// button's menu open above it. Into COMPOSER_AUDIT_SHOTS_DIR (the
// composer-audit-shots workflow; skipped without it). Synthetic drafts on
// internal/fakeslack.
func TestDraftsButtonShots(t *testing.T) {
	dir := os.Getenv("COMPOSER_AUDIT_SHOTS_DIR")
	if dir == "" {
		t.Skip("COMPOSER_AUDIT_SHOTS_DIR not set")
	}
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	setOnline(t, func() (bool, time.Time) { return true, time.Time{} })
	app, _ := fakeslackApp(t)
	app.SetExperimentGroup("draft_management_in_composer", "treatment")
	ch := tChGen
	forget := func() { forgetDraft(ch); serverDraftIDs.Delete(draftKey(ch, "")) }
	forget()
	t.Cleanup(forget)
	ctx := context.Background()
	for i := 1; i <= 5; i++ {
		if _, err := dmStash(ctx, app, ch, fmt.Sprintf("synthetic stashed draft %d", i)); err != nil {
			t.Fatal(err)
		}
		time.Sleep(3 * time.Millisecond)
	}
	if _, err := app.Drafts(ctx); err != nil {
		t.Fatal(err)
	}
	for _, dark := range []bool{false, true} {
		mode := map[bool]string{false: "light", true: "dark"}[dark]
		th := theme.New(dark)
		h := &ovHost{slickHost: &slickHost{app: app, th: th}}
		c := New(h, ch, "")
		d := NewDoc()
		d.Insert(0, "hey", Attr{})
		c.ed.SetDoc(d)
		c.ed.SetSelection(3, 3)
		const w, hh, boxH = 900, 420, 150
		var ops op.Ops
		var r input.Router
		frame := func(menu bool) {
			ops.Reset()
			paint.FillShape(&ops, th.C.Bg, clip.Rect{Max: image.Pt(w, hh)}.Op())
			gtx := layout.Context{Ops: &ops, Metric: unit.Metric{PxPerDp: 1, PxPerSp: 1},
				Constraints: layout.Exact(image.Pt(w, hh)), Now: time.Now(), Source: r.Source()}
			slackc.Frame(gtx)
			c.Origin = image.Pt(0, hh-boxH)
			off := op.Offset(c.Origin).Push(gtx.Ops)
			g := gtx
			g.Constraints = layout.Constraints{Max: image.Pt(w, boxH)}
			c.Layout(g)
			off.Pop()
			if menu {
				for _, o := range h.overlays {
					if !o.Dismissed() {
						o.Layout(gtx)
					}
				}
			}
			r.Frame(&ops)
		}
		save := func(name string) {
			img := pixeltest.Render(t, w, hh, &ops)
			f, err := os.Create(filepath.Join(dir, name+".png"))
			if err != nil {
				t.Fatal(err)
			}
			defer f.Close()
			if err := png.Encode(f, img); err != nil {
				t.Fatal(err)
			}
			t.Logf("wrote %s.png", name)
		}
		for range 6 { // the tip's 200ms fade-in, the drafts counted
			frame(false)
			time.Sleep(60 * time.Millisecond)
		}
		rb, _, n := dmButton(c)
		if n != 6 {
			t.Errorf("%s: the drafts button counts %d, want 6", mode, n)
		}
		save("drafts-button-" + mode + "-1x")
		dmOpenMenu(c, rb.Add(c.Origin).Add(image.Pt(0, c.top)))
		for range 3 { // menus measure, then place
			frame(true)
			time.Sleep(40 * time.Millisecond)
		}
		if m, ok := h.last().(*popups.Menu); ok {
			t.Logf("%s menu %q", mode, m.Labels())
		}
		save("drafts-menu-" + mode + "-1x")
		c.Close()
	}
}
