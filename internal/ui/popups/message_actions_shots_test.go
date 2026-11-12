package popups

import (
	"context"
	"image"
	"image/png"
	"os"
	"path/filepath"
	"testing"
	"time"

	"gioui.org/f32"
	"gioui.org/io/input"
	"gioui.org/io/pointer"
	"gioui.org/layout"
	"gioui.org/op"
	"gioui.org/op/clip"
	"gioui.org/op/paint"
	"gioui.org/unit"

	"github.com/some-du6e/clack/internal/core"
	"github.com/some-du6e/clack/internal/fakeslack"
	"github.com/some-du6e/clack/internal/slack"
	"github.com/some-du6e/clack/internal/ui/theme"
	"github.com/some-du6e/clack/internal/ui/theme/pixeltest"
)

// TestMessageActionsShots draws what branch message-actions-2 changed,
// light and dark, for the menu-keys workflow (EXP_MENUS_SHOTS_DIR,
// artifact menu-keys-shots): the share dialog (v2 ButtonGroup with the
// schedule caret and Save Draft; the legacy split; 32px option rows;
// Copy Link's "Copied!"), FlagMessageDialog (the form, the speed bump,
// the submitted state) and the More actions menu with the Flag and
// workflow rows. Synthetic content only.
func TestMessageActionsShots(t *testing.T) {
	dir := os.Getenv("EXP_MENUS_SHOTS_DIR")
	if dir == "" {
		t.Skip("EXP_MENUS_SHOTS_DIR not set")
	}
	h, fs, _ := emBooted(t)
	fs.SetEnterpriseLockedPrefs(map[string]any{"content_review_enabled": true})
	if err := h.a.LoadEnterprisePrefs(context.Background()); err != nil {
		t.Fatal(err)
	}
	h.a.SetContentReporting(true)
	h.a.SetExperimentGroup(ExpFlagXorgRefinement, "control")
	h.a.SetExperimentGroup(ExpFlagXorgExternalTeamCheck, "off")
	maForwarding(h, true)
	h.a.S.PutUsers([]slack.User{{ID: fakeslack.IdentityWorkflowID, TeamID: fakeslack.TeamID, Name: "standup",
		RealName: "Standup Workflow", IsBot: true, IsWorkflowBot: true}})
	sh := theme.NewShaper()
	const W, H = 1200, 900 // px at 2px per dp: a 600×450dp window
	// shoot lays draw out in frames through a router (the pointer at ptr,
	// px, when set, the clock moving on) and saves the last frame.
	shoot := func(name string, dark bool, ptr image.Point, frames int, draw func(gtx layout.Context)) {
		t.Helper()
		th := theme.New(dark)
		th.SetShapers(sh, sh)
		h.th = th
		var r input.Router
		var ops op.Ops
		now := time.Now()
		for i := 0; i < frames; i++ {
			ops.Reset()
			paint.FillShape(&ops, th.C.Bg, clip.Rect{Max: image.Pt(W, H)}.Op())
			gtx := layout.Context{Ops: &ops, Source: r.Source(), Metric: unit.Metric{PxPerDp: 2, PxPerSp: 2}, Now: now,
				Constraints: layout.Exact(image.Pt(W, H))}
			draw(gtx)
			r.Frame(&ops)
			if ptr != (image.Point{}) {
				r.Queue(pointer.Event{Kind: pointer.Move, Source: pointer.Mouse, Position: f32.Pt(float32(ptr.X), float32(ptr.Y))})
			}
			now = now.Add(400 * time.Millisecond)
		}
		img := pixeltest.Render(t, W, H, &ops)
		f, err := os.Create(filepath.Join(dir, name))
		if err != nil {
			t.Fatal(err)
		}
		defer f.Close()
		if err := png.Encode(f, img); err != nil {
			t.Fatal(err)
		}
	}
	m := core.Message{Channel: emCh, TS: "1700000000.000100", User: fakeslack.ProfileID, Text: "A synthetic message to forward."}
	for _, mode := range []struct {
		name string
		dark bool
	}{{"light", false}, {"dark", true}} {
		// the account's footer: v2 Buttons, Save Draft, Forward + caret;
		// a query shows the 32px rows
		h.a.SetExperimentGroup(ExpSlackKitV2Button, "on")
		h.a.SetExperimentGroup(ExpScheduleSendForwards, "on")
		h.a.SetExperimentGroup(ExpForwardingSaveAsDraft, "on")
		f := newForward(h, m)
		f.to.SetText("g")
		f.refilter()
		shoot("share-dialog-v2-"+mode.name+".png", mode.dark, image.Point{}, 3, func(gtx layout.Context) { f.modal.Layout(gtx) })
		g := newForward(h, m)
		g.chips = append(g.chips, &actFwdChip{t: &g.all[0]})
		g.note.SetText("Have a look at this one")
		shoot("share-dialog-chip-"+mode.name+".png", mode.dark, image.Point{}, 3, func(gtx layout.Context) { g.modal.Layout(gtx) })
		// the legacy split: c-button--primary and the schedule c-icon_button
		h.a.SetExperimentGroup(ExpSlackKitV2Button, "off")
		k := newForward(h, m)
		k.chips = append(k.chips, &actFwdChip{t: &k.all[0]})
		shoot("share-dialog-legacy-"+mode.name+".png", mode.dark, image.Point{}, 3, func(gtx layout.Context) { k.modal.Layout(gtx) })
		h.a.SetExperimentGroup(ExpSlackKitV2Button, "on")
		// Copy Link's tooltip after a click: "Copied!"
		c := newForward(h, m)
		var ptr image.Point
		shoot("share-dialog-copied-"+mode.name+".png", mode.dark, image.Point{}, 2, func(gtx layout.Context) {
			c.modal.Layout(gtx)
			ptr = c.copy.Rect().Min.Add(c.copy.Rect().Size().Div(2))
		})
		c.copied = true
		shoot("share-dialog-copied-"+mode.name+".png", mode.dark, ptr, 5, func(gtx layout.Context) { c.modal.Layout(gtx) })

		// FlagMessageDialog: the form, its speed bump, the submitted state
		fl := openFlagDialog(h, m)
		fl.reason.SetText("A synthetic reason for flagging")
		shoot("flag-dialog-"+mode.name+".png", mode.dark, image.Point{}, 3, func(gtx layout.Context) { fl.modal.Layout(gtx) })
		fl.bump = true
		shoot("flag-speedbump-"+mode.name+".png", mode.dark, image.Point{}, 3, func(gtx layout.Context) { fl.modal.Layout(gtx) })
		fl.bump, fl.done = false, true
		fl.modal.o.Buttons, fl.modal.o.FooterLeft = nil, nil
		shoot("flag-submitted-"+mode.name+".png", mode.dark, image.Point{}, 3, func(gtx layout.Context) { fl.modal.Layout(gtx) })

		// the More actions menu of a workflow's message: the workflow rows,
		// then Flag this message… in Report to Slack's place
		wf := core.Message{Channel: emCh, TS: "1700000000.000600", User: fakeslack.IdentityWorkflowID, TriggerID: fakeslack.TriggerDeploy, Text: "synthetic"}
		menu := NewMessageMenuOpts(h, image.Pt(1100, 20), wf, MessageMenuOpts{})
		shoot("message-menu-flag-"+mode.name+".png", mode.dark, image.Point{}, 2, func(gtx layout.Context) { menu.Layout(gtx) })
	}
}
