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

	"gioui.org/layout"
	"gioui.org/op"
	"gioui.org/op/clip"
	"gioui.org/op/paint"
	"gioui.org/unit"

	"github.com/some-du6e/clack/internal/core"
	"github.com/some-du6e/clack/internal/slack"
	"github.com/some-du6e/clack/internal/ui/popups"
	"github.com/some-du6e/clack/internal/ui/slackc"
	"github.com/some-du6e/clack/internal/ui/theme"
	"github.com/some-du6e/clack/internal/ui/theme/pixeltest"
)

// TestExpComposerShots draws what the exp-composer branch ported, light
// and dark at 2x, into COMPOSER_AUDIT_SHOTS_DIR (the composer-audit-shots
// workflow; skipped without it): the Custom time dialog
// (ScheduleMessageDialog), the snippet dialog, the paste-limit dialog
// with Create a Snippet, the + menu, and the composer under the
// scheduled-send context bar. Synthetic data and internal/fakeslack only.
func TestExpComposerShots(t *testing.T) {
	dir := os.Getenv("COMPOSER_AUDIT_SHOTS_DIR")
	if dir == "" {
		t.Skip("COMPOSER_AUDIT_SHOTS_DIR not set")
	}
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	app, fs := fakeslackApp(t)
	stubTyping(t)
	ch := fs.ChannelIDs()[2]
	app.S.PutConv(slack.Conversation{ID: ch, Name: "design-crit", IsChannel: true, IsMember: true})
	clearDraft(ch, "")
	t.Cleanup(func() { clearDraft(ch, "") })
	const scale = 2
	save := func(t *testing.T, name string, w, h int, ops *op.Ops) {
		img := pixeltest.Render(t, w, h, ops)
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
	frame := func(th *theme.Theme, ops *op.Ops, w, h int, draw func(gtx layout.Context)) {
		ops.Reset()
		paint.FillShape(ops, th.C.Bg, clip.Rect{Max: image.Pt(w, h)}.Op())
		gtx := layout.Context{Ops: ops, Metric: unit.Metric{PxPerDp: scale, PxPerSp: scale},
			Constraints: layout.Exact(image.Pt(w, h)), Now: time.Now()}
		slackc.Frame(gtx)
		draw(gtx)
	}
	// overlay draws o (twice: overlays settle on their second frame)
	overlay := func(t *testing.T, th *theme.Theme, name string, o popups.Overlay) {
		w, h := 1100*scale, 760*scale
		var ops op.Ops
		for i := 0; i < 3; i++ {
			frame(th, &ops, w, h, func(gtx layout.Context) { o.Layout(gtx) })
		}
		save(t, name, w, h, &ops)
	}
	for _, dark := range []bool{false, true} {
		mode := map[bool]string{false: "light", true: "dark"}[dark]
		th := theme.New(dark)
		h := &ovHost{slickHost: &slickHost{app: app, th: th}}
		c := New(h, ch, "")
		t.Cleanup(c.Close)

		t.Run("schedule-dialog-"+mode, func(t *testing.T) {
			p := openSchedulePicker(h, schedCustomDefault(time.Now(), nil), nil, func(time.Time) {})
			overlay(t, th, fmt.Sprintf("exp-schedule-dialog-%s", mode), p.m)
		})
		t.Run("snippet-dialog-"+mode, func(t *testing.T) {
			d := openSnippetDialog(c, snippetSpec{text: "package main\n\nfunc main() {\n\tprintln(\"hello\")\n}\n", ch: ch})
			d.title.SetText("main.go")
			d.typ = "go"
			overlay(t, th, fmt.Sprintf("exp-snippet-dialog-%s", mode), d.m)
		})
		t.Run("paste-limit-"+mode, func(t *testing.T) {
			c.pasteLimitDialog("x")
			overlay(t, th, fmt.Sprintf("exp-paste-limit-%s", mode), h.last())
		})
		t.Run("plus-menu-"+mode, func(t *testing.T) {
			c.att.Menu(image.Rect(40*scale, 600*scale, 68*scale, 628*scale), c.plusMenu())
			overlay(t, th, fmt.Sprintf("exp-plus-menu-%s", mode), h.last())
		})
	}
	// the scheduled-send bar over the composer
	if _, err := app.ScheduleMessage(context.Background(), core.SendOpts{Channel: ch, Text: "later"}, time.Now().Add(26*time.Hour).Unix()); err != nil {
		t.Fatal(err)
	}
	for _, dark := range []bool{false, true} {
		mode := map[bool]string{false: "light", true: "dark"}[dark]
		t.Run("scheduled-bar-"+mode, func(t *testing.T) {
			th := theme.New(dark)
			h := &ovHost{slickHost: &slickHost{app: app, th: th}}
			c := New(h, ch, "")
			t.Cleanup(c.Close)
			c.sbar.seen, c.sbar.init, c.sbar.lastLay = 0, true, time.Now() // opened before the schedule
			w, hh := 760*scale, 240*scale
			var ops op.Ops
			for i := 0; i < 2; i++ {
				frame(th, &ops, w, hh, func(gtx layout.Context) {
					gtx.Constraints = layout.Constraints{Max: image.Pt(w, hh)}
					c.Layout(gtx)
				})
			}
			if c.top == 0 {
				t.Fatal("no scheduled-send bar")
			}
			save(t, fmt.Sprintf("exp-scheduled-bar-%s", mode), w, hh, &ops)
		})
	}
}
