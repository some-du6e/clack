package huddleui

import (
	"image"
	"testing"
	"time"

	"gioui.org/op"

	"github.com/some-du6e/clack/internal/huddle"
	"github.com/some-du6e/clack/internal/imgcache"
	"github.com/some-du6e/clack/internal/slack"
	"github.com/some-du6e/clack/internal/ui/popups"
	"github.com/some-du6e/clack/internal/ui/theme"
	"github.com/some-du6e/clack/internal/ui/theme/pixeltest"
)

// gap-w9's pictures (huddles-audit-shots), light and dark: the compact
// window with Video settings open, with Leave options open (an external
// guest in the huddle, you its host), and with the quick picker's
// Reactions tab; the docked bar with the segmented camera and Leave, and
// its quick picker over the main window.
func TestHuddleShotsW9(t *testing.T) {
	const W, H = 589, 351
	old := listCameras
	listCameras = func() []huddle.VideoDevice { return []huddle.VideoDevice{{ID: "cam-1", Name: "FaceTime HD Camera"}} }
	t.Cleanup(func() { listCameras = old })
	for _, dark := range []bool{false, true} {
		mode := "light"
		if dark {
			mode = "dark"
		}
		e, s := windowShotEnv(t, dark, 2)
		e.h.th.Images = e.h.imgs // the dock draws with the main theme
		app := e.h.app
		app.SetExperimentGroup(extHuddles, "on")
		remote, _ := app.S.User(huddle.FakeRemoteUser)
		g := *remote
		g.IsRestricted, g.RestrictedUserType = true, "external_guest"
		app.S.PutUsers([]slack.User{g})
		app.S.PutConv(slack.Conversation{ID: hcChannel, Name: "huddle-test", IsChannel: true, IsMember: true, NumMembers: 2, Creator: huddle.FakeSelfUser})
		settle(t, e, W, H)
		var ops op.Ops
		e.w.layout(shotCtx(&ops, W, H))

		// Video settings above the camera's caret
		e.w.toggleVideo(s, s.State())
		img := render(t, "w9-window-video-"+mode, W, H, func(gtx C) { e.w.layout(gtx) })
		if e.w.videoBtn.Empty() || e.w.video == nil {
			t.Fatalf("%s: no Video settings caret or menu", mode)
		}
		// the caret is toggled on while its menu is open (hudBtnOn)
		vb := e.w.videoBtn.Add(image.Pt(0, e.w.barTop))
		if n := pixeltest.Count(img, vb, hudBtnOn, 12); n < vb.Dx()*vb.Dy()/3 {
			t.Errorf("%s: the Video settings caret isn't toggled on (%d px)", mode, n)
		}
		e.w.closeMenus()

		// Leave options: the segmented Leave and its menu
		e.w.layout(shotCtx(&ops, W, H))
		if e.w.leaveBtn.Empty() {
			t.Fatalf("%s: Leave isn't segmented with an external guest in the huddle", mode)
		}
		e.w.toggleLeaveOptions(s)
		img = render(t, "w9-window-leave-"+mode, W, H, func(gtx C) { e.w.layout(gtx) })
		lb := e.w.leaveBtn.Add(image.Pt(0, e.w.barTop))
		if n := pixeltest.Count(img, lb, hudLeave, 12); n < lb.Dx()*lb.Dy()/3 {
			t.Errorf("%s: the Leave options caret isn't red (%d px)", mode, n)
		}
		e.w.closeMenus()

		// the quick picker's Reactions tab
		e.w.toggleQuick(s)
		settle(t, e, W, H) // the picker's emoji pictures
		img = render(t, "w9-window-quick-"+mode, W, H, func(gtx C) { e.w.layout(gtx) })
		q := e.w.quick
		if q == nil {
			t.Fatalf("%s: the quick picker closed", mode)
		}
		bg := popups.ColorsOf(e.w.th).EmojiBg
		in := q.card.Inset(8 * shotScale)
		if n := pixeltest.Count(img, in, bg, 6); n < in.Dx()*in.Dy()/4 {
			t.Errorf("%s: the quick picker card isn't sk_primary_background (%d px)", mode, n)
		}
		e.w.closeMenus()

		// the docked bar (window closed): segmented camera and Leave
		e.c.mu.Lock()
		e.c.win = nil
		e.c.mu.Unlock()
		oh := &overlayHost{hcHost: *e.h}
		e.c.h = oh
		dock := func(gtx C) {
			theme.Fill(gtx, image.Rectangle{Max: gtx.Constraints.Max}, hudFrame)
			e.c.LayoutDock(gtx, e.h.th)
		}
		loadPictures(t, e, 1000, dockBar+dockPad, dock) // the faces and the raise-hand
		img = render(t, "w9-dock-"+mode, 1000, dockBar+dockPad, dock)
		right := image.Rect(820*shotScale, 0, 1000*shotScale, dockBar*shotScale)
		if n := pixeltest.Count(img, right, hudLeave, 12); n < 3000 {
			t.Errorf("%s: no Leave in the dock (%d px)", mode, n)
		}
		// its emoji button's quick picker over the main window
		const MH = 520
		e.c.dockClick(shotCtx(&ops, 1000, MH), "react", s, s.State(), image.Rect(560*shotScale, (MH-50)*shotScale, 600*shotScale, (MH-10)*shotScale))
		if len(oh.shown) == 0 {
			t.Fatalf("%s: the dock's emoji button opened nothing", mode)
		}
		ov := oh.shown[len(oh.shown)-1]
		render(t, "w9-dock-quick-"+mode, 1000, MH, func(gtx C) {
			theme.Fill(gtx, image.Rectangle{Max: gtx.Constraints.Max}, e.h.th.C.Bg)
			// the client's bottom row under the dock, as in the other dock shots
			theme.Fill(gtx, image.Rect(0, gtx.Dp(MH-dockBar-dockPad), gtx.Constraints.Max.X, gtx.Constraints.Max.Y), hudFrame)
			off := op.Offset(image.Pt(0, gtx.Dp(MH-dockBar-dockPad))).Push(gtx.Ops)
			g := gtx
			g.Constraints.Max.Y = gtx.Dp(dockBar + dockPad)
			e.c.LayoutDock(g, e.h.th)
			off.Pop()
			ov.Layout(gtx)
		})
		ov.(*quickPicker).Dismiss()
	}
}

// withPictures gives a shot env the shots' picture cache, on the hosts
// and the window's theme, as the real huddle window copies the main
// theme's images (window.go run): the toolbar's raise-hand and the
// stickers draw Slack's emoji images.
func withPictures(e *shotEnv) {
	e.h.imgs = imgcache.New(shotPictures, "", nil)
	e.w.th.Images = e.h.imgs
	e.h.th.Images = e.h.imgs
}

// loadPictures lays draw out until no picture is pending.
func loadPictures(t *testing.T, e *shotEnv, w, h int, draw func(gtx C)) {
	t.Helper()
	var ops op.Ops
	for deadline := time.Now().Add(20 * time.Second); time.Now().Before(deadline); {
		ops.Reset()
		draw(shotCtx(&ops, w, h))
		if e.h.imgs.Pending() == 0 {
			return
		}
		time.Sleep(20 * time.Millisecond)
	}
	t.Fatalf("%d pictures still pending", e.h.imgs.Pending())
}
