package huddleui

import (
	"image"
	"image/color"
	"runtime"

	"github.com/some-du6e/clack/internal/huddle"
	"github.com/some-du6e/clack/internal/huddle/avperm"
	"github.com/some-du6e/clack/internal/ui/openurl"
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

// cameraCtl is the toolbar's camera button: lit while on, greyed with a
// tooltip where there is no camera backend.
func (w *window) cameraCtl(st huddle.State) ctl {
	switch st.Camera.State {
	case huddle.CameraOn:
		return ctl{id: "camera", icon: "video", on: true}
	case huddle.CameraStarting:
		return ctl{id: "camera", icon: "video"}
	}
	c := ctl{id: "camera", icon: "video-off"}
	if !cameraSupported() {
		c.dim, c.tip = true, camUnsupported
	}
	return c
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
	a := w.cam.tipAnchor.Add(bar.Min)
	ts := labelSize(gtx, w.th, 12, theme.Bold, w.cam.tip)
	pad := gtx.Dp(8)
	r := image.Rect(0, 0, ts.X+2*pad, ts.Y+pad)
	r = r.Add(image.Pt((a.Min.X+a.Max.X-r.Dx())/2, a.Min.Y-r.Dy()-gtx.Dp(6)))
	if r.Min.X < gtx.Dp(4) {
		r = r.Add(image.Pt(gtx.Dp(4)-r.Min.X, 0))
	}
	theme.RRect(gtx, r, gtx.Dp(6), color.NRGBA{R: 0x1d, G: 0x1c, B: 0x1d, A: 0xf2})
	at(gtx, image.Pt(r.Min.X+pad, r.Min.Y+pad/2), ts, func(gtx C) D {
		return w.th.Label(gtx, 12, theme.Bold, hudWhite, w.cam.tip)
	})
	w.cam.tip = ""
}
