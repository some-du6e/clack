package huddleui

import (
	"image"
	"image/color"
	"runtime"

	"github.com/some-du6e/clack/internal/huddle"
	"github.com/some-du6e/clack/internal/huddle/avperm"
	"github.com/some-du6e/clack/internal/ui/openurl"
	"github.com/some-du6e/clack/internal/ui/popups"
	"github.com/some-du6e/clack/internal/ui/theme"
)

// camUI is the huddle window's camera state: the denied notice, the
// unsupported tooltip, and the last status seen (to toast failures once).
type camUI struct {
	last      huddle.CameraState
	hideDeny  bool
	tip       string
	tipAnchor image.Rectangle // the hovered control, in toolbar coordinates
}

const camUnsupported = "Camera isn't supported on this platform yet"

// cameraSupported is huddle.CameraSupported (a var for tests).
var cameraSupported = huddle.CameraSupported

// cameraClick toggles your camera. SetCamera returns at once: the
// permission prompt, the camera and the encoder start on a goroutine.
func (w *window) cameraClick(s *huddle.Session, st huddle.State) {
	switch st.Camera.State {
	case huddle.CameraOn, huddle.CameraStarting:
		s.SetCamera(false)
	default:
		if !cameraSupported() {
			w.note(camUnsupported)
			return
		}
		w.cam.hideDeny = false
		s.SetCamera(true)
	}
}

// HuddleToolbarCameraButton (gantry-v2-async-client-boot-shared LnqA,
// build d3de3a487b1c) is segmented in every toolbar (.segmentedButton):
//
//   - the camera half (.segmentedButtonMain, 36px), toggled on while your
//     camera is on, video-off or video-filled (7d+n with
//     shouldUseFilledActiveIcon); its tooltip is HuddlesToolTipDeviceContent
//     with useCameraButtonLabels' (I4aw z) videoLabel, "Turn on video" or
//     "Turn off video" (deviceLabel is never set), its aria-label "Turn on
//     camera" / "Turn off camera";
//   - the "Video settings" caret (.segmentedButtonPopover, 28px):
//     caret-up, caret-down while its menu is open, toggled on while open
//     or while the camera is on. It opens HuddleVideoDevicesMenu (DU15 O)
//     above it (position "top", closeOnSelect false).
//
// HuddleToolbar (9d6D) draws the button only under
// isHuddlesVideoEnabledForOrgWorkspaces (zq6z F7, videoAllowed). The
// menu, in Slack's order:
//
//	Video                       (header; F7)
//	  ◉ each camera             (Tml2 useCameraSettingsMenuItems: radio rows,
//	                             the one in use checked; "No cameras found")
//	────
//	Video preview               (HuddleCameraPreviewMenuItem; F7)
//	Video background            (useChangeVideoBackgroundMenuItem; F7, not Firefox)
//	────
//	Audio & video preferences   (yBJd; hidden for an external guest, cB5s U)
//
// Clack lists the camera it opens (AVFoundation's default device; the test
// pattern under CLACK_HUDDLE_TESTCAM): it can't switch cameras. Video
// preview (a camera preview with a Camera source picker, NfdB) and Video
// background (HuddleBackgroundPickerDialog's images, or the older
// ChangeVideoBackgroundDialog) aren't drawn: Clack has no camera switch,
// no live preview in a menu and no background images (only blur, in
// Preferences › Audio & video). docs/HUDDLES-AUDIT.md "Camera button".

// listCameras is huddle.CameraDevices (tests list fakes).
var listCameras = huddle.CameraDevices

// cameraCtls is the segmented camera button's two halves (none when video
// isn't allowed).
func (c *Controller) cameraCtls(st huddle.State, menuOpen bool) []ctl {
	if !c.videoAllowed() {
		return nil
	}
	on := st.Camera.State == huddle.CameraOn
	main := ctl{id: "camera", icon: "video-off", tip: nsHuddles.T("Turn on video"), seg: segMain}
	switch st.Camera.State {
	case huddle.CameraOn:
		main.icon, main.on, main.tip = "video-filled", true, nsHuddles.T("Turn off video")
	case huddle.CameraStarting:
		main.icon, main.tip = "video", nsHuddles.T("Turn off video")
	}
	if !cameraSupported() && !on {
		main.dim = true
	}
	caret := ctl{id: "videomenu", icon: "caret-up", on: menuOpen || on, tip: nsHuddles.T("Video settings"), seg: segCaret}
	if menuOpen {
		caret.icon = "caret-down"
	}
	return []ctl{main, caret}
}

// videoAllowed is isHuddlesVideoEnabledForOrgWorkspaces (zq6z F7): you're
// an external guest in an external huddle (cB5s U), or the team pref
// allow_huddles_video is on (h/Bp xp). Slack reads a missing pref as off;
// Clack only hides video when the loaded prefs turn it off, since it can't
// tell a pref team.prefs.get leaves out from one that's unset.
func (c *Controller) videoAllowed() bool {
	app := c.h.Core()
	if app == nil {
		return true
	}
	if c.selfExternalGuest() {
		return true
	}
	if !app.TeamPrefsLoaded() {
		return true
	}
	v, ok := app.TeamPref("allow_huddles_video")
	if !ok {
		return true
	}
	return string(v) != "false" && string(v) != `"false"` && string(v) != "0"
}

// selfExternalGuest is cB5s U: external huddles on and you're an external
// guest (wY11 EG: is_restricted, restricted_user_type external_guest).
func (c *Controller) selfExternalGuest() bool {
	app := c.h.Core()
	if app == nil || !c.on(extHuddles) {
		return false
	}
	u, ok := app.S.User(c.selfID())
	return ok && u.IsRestricted && u.RestrictedUserType == "external_guest"
}

// videoMenu is HuddleVideoDevicesMenu's rows.
func (c *Controller) videoMenu(s *huddle.Session) []popups.MenuItem {
	var items []popups.MenuItem
	if c.videoAllowed() {
		items = append(items, popups.MenuItem{Header: nsHuddles.T("Video")})
		cams := listCameras()
		if len(cams) == 0 {
			// useCameraSettingsMenuItems' case 2 (KI/8 q: no devices)
			items = append(items, popups.MenuItem{Key: "no-cameras", Label: nsHuddles.T("No cameras found"), Radio: true})
		}
		for i, d := range cams {
			items = append(items, popups.MenuItem{Key: "av-camera-device-menu-item_" + d.Name, Label: d.Name, Radio: true,
				Checked: i == 0, Do: func() {}}) // the one Clack opens
		}
		items = append(items, popups.Sep())
	}
	if !c.selfExternalGuest() { // useAVPreferencesSettingsMenuItem
		items = append(items, popups.MenuItem{Key: "preferences", Label: nsHuddles.T("Audio & video preferences"), Radio: true, Do: func() {
			if p, ok := c.h.(interface{ OpenPreferences(section string) }); ok {
				c.post(func() { p.OpenPreferences("video_audio") })
			}
		}})
	}
	return items
}

// toggleVideo opens (or closes) Video settings above the camera's caret.
func (w *window) toggleVideo(s *huddle.Session, st huddle.State) {
	if w.video != nil {
		w.video.Dismiss()
		w.video = nil
		return
	}
	w.closeMenus()
	anchor := w.videoBtn.Add(image.Pt(0, w.barTop))
	w.video = popups.NewMenuOpts(winHost{w}, w.c.videoMenu(s), popups.MenuOpts{Anchor: anchor, Side: popups.Above,
		MenuClass: "p-huddle_more_actions_menu"})
}

// cameraWatch toasts a camera failure once, when it happens.
func (w *window) cameraWatch(st huddle.State) {
	cs := st.Camera
	if cs.State != w.cam.last {
		switch cs.State {
		case huddle.CameraFailed:
			msg := "Couldn't start your camera"
			if cs.Err != "" {
				msg += ": " + cs.Err
			}
			w.note(msg)
		case huddle.CameraUnsupported:
			w.note(camUnsupported)
		}
		w.cam.last = cs.State
	}
}

// selfCam is the video key of your own tile while your camera is on.
func selfCam(st huddle.State, cams map[string]string) {
	if st.Camera.State == huddle.CameraOn && st.SelfAttendee != "" {
		cams[st.SelfAttendee] = huddle.SelfVideoKey
	}
}

// cameraNotice is the banner over the stage when the OS denied camera
// access, with a button to the privacy settings (opened off the UI
// goroutine by openurl) and a close button.
func (w *window) cameraNotice(gtx C, st huddle.State, stage image.Rectangle) {
	if w.click("cam-settings").Clicked(gtx) {
		if err := openurl.OpenSettings(avperm.SettingsURL(avperm.Camera)); err != nil {
			w.note("Open your system settings to allow camera access")
		}
	}
	if w.click("cam-dismiss").Clicked(gtx) {
		w.cam.hideDeny = true
	}
	if st.Camera.State != huddle.CameraDenied || w.cam.hideDeny {
		return
	}
	where := "your system settings"
	if runtime.GOOS == "darwin" {
		where = "System Settings › Privacy & Security › Camera"
	} else if runtime.GOOS == "windows" {
		where = "Settings › Privacy › Camera"
	}
	text := AppName + " doesn't have access to your camera. Allow it in " + where + "."
	url := avperm.SettingsURL(avperm.Camera)
	pad := gtx.Dp(10)
	bh := gtx.Dp(28)
	btn := nsHuddles.T("Open Settings")
	bw := labelSize(gtx, w.th, 13, theme.Bold, btn).X + 2*gtx.Dp(12)
	if url == "" {
		bw = 0
	}
	xw := gtx.Dp(24)
	maxText := stage.Dx() - 2*gtx.Dp(16) - bw - xw - 4*pad
	ts := labelSize(gtx, w.th, 13, theme.Bold, text)
	if ts.X > maxText {
		ts.X = maxText
	}
	width := ts.X + bw + xw + 4*pad
	h := max(ts.Y, bh) + 2*pad
	r := image.Rect((stage.Min.X+stage.Max.X-width)/2, stage.Min.Y+gtx.Dp(10), (stage.Min.X+stage.Max.X+width)/2, stage.Min.Y+gtx.Dp(10)+h)
	theme.RRect(gtx, r, gtx.Dp(8), color.NRGBA{R: 0x1d, G: 0x1c, B: 0x1d, A: 0xf2})
	at(gtx, image.Pt(r.Min.X+pad, r.Min.Y+(h-ts.Y)/2), ts, func(gtx C) D {
		return w.th.Label(gtx, 13, theme.Bold, hudWhite, text)
	})
	x := r.Min.X + 2*pad + ts.X
	if bw > 0 {
		bs := image.Pt(bw, bh)
		at(gtx, image.Pt(x, r.Min.Y+(h-bh)/2), bs, func(gtx C) D {
			return button(gtx, w.click("cam-settings"), bs, func(gtx C, hov bool) {
				bg := hudBtn
				if hov {
					bg = hudBtnHover
				}
				theme.RRect(gtx, image.Rectangle{Max: bs}, gtx.Dp(6), bg)
			}, func(gtx C) D { centerLabel(gtx, w.th, bs, 13, theme.Bold, hudWhite, btn); return D{} })
		})
		x += bw + pad
	}
	xs := image.Pt(xw, xw)
	at(gtx, image.Pt(x, r.Min.Y+(h-xw)/2), xs, func(gtx C) D {
		return button(gtx, w.click("cam-dismiss"), xs, func(gtx C, hov bool) {
			if hov {
				theme.RRect(gtx, image.Rectangle{Max: xs}, gtx.Dp(6), hudBtnHover)
			}
		}, func(gtx C) D { iconAt(gtx, xs, "close", 14, hudWhite); return D{} })
	})
}

// toolbarTip draws the hovered control's tooltip above it; bar is where
// the toolbar sits in the window.
func (w *window) toolbarTip(gtx C, bar image.Rectangle) {
	if w.cam.tip == "" {
		return
	}
	hudTip(gtx, w.th, w.cam.tip, w.cam.tipAnchor.Add(bar.Min), gtx.Constraints.Max, theme.TipTop)
	w.cam.tip = ""
}

// hudTip draws a huddle window toolbar tooltip for target (window
// coordinates) at pos, placed by theme.PlaceTooltip like every other
// tooltip (8px from the control, kept inside the win-sized window, the
// opposite side when there's no room).
func hudTip(gtx C, th *theme.Theme, tip string, target image.Rectangle, win image.Point, pos string) {
	ts := labelSize(gtx, th, 12, theme.Bold, tip)
	pad := gtx.Dp(8)
	pl := theme.PlaceTooltip(gtx, target, image.Pt(ts.X+2*pad, ts.Y+pad), image.Rectangle{Max: win}, theme.BaseTooltip(gtx, pos))
	r := pl.Box
	theme.RRect(gtx, r, gtx.Dp(6), color.NRGBA{R: 0x1d, G: 0x1c, B: 0x1d, A: 0xf2})
	at(gtx, image.Pt(r.Min.X+pad, r.Min.Y+pad/2), ts, func(gtx C) D {
		return th.Label(gtx, 12, theme.Bold, hudWhite, tip)
	})
}
