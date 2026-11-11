package huddleui

import (
	"image"

	"gioui.org/layout"

	"github.com/some-du6e/clack/internal/ui/popups"
	"github.com/some-du6e/clack/internal/ui/theme"
)

// A huddle you're already in on another device (huddles_multi_device,
// "treatment"), from client-boot e5et startHuddleWithConfirmation and
// gantry-v2-async-client-boot-shared 6e8N HuddleMultiDeviceModal (build
// 4d0c78085232):
//
//   - starting or joining the huddle in a conversation where you're
//     huddling but this client isn't (zq6z r5: isUserHuddlingInChannel and
//     not the active huddle's channel) opens HuddleMultiDeviceModal first;
//   - the modal: your face once per device you're in it on, then the
//     others', 56px (x_large), six at most; "You’re in this huddle on a
//     different device" (one device) or "… on multiple devices"; "Use
//     both devices" (outline, focused) and "Switch to this device"
//     (primary);
//   - either goes on with startHuddleWithShoutyRooster, overrideJoinPreview
//     set (no join preview) and isMultiDevice true for "Use both devices",
//     which rooms.join sends as multidevice.

// pendingMulti is the modal's answer for the next start in channel.
type pendingMulti struct {
	channel string
	multi   bool
}

// huddlingElsewhere reports whether you're in ch's huddle on another
// device: the room lists you while this client isn't in it. devices is
// how many times it lists you.
func (c *Controller) huddlingElsewhere(ch string) (devices int, others []string) {
	self := c.selfID()
	c.mu.Lock()
	r, ok := c.live[ch]
	here := c.s != nil && c.s.Channel() == ch
	c.mu.Unlock()
	if !ok || here || self == "" {
		return 0, nil
	}
	for _, u := range r.Participants {
		if u == self {
			devices++
		} else {
			others = append(others, u)
		}
	}
	return devices, others
}

// multiDeviceModal opens HuddleMultiDeviceModal; pick gets isMultiDevice.
func (c *Controller) multiDeviceModal(devices int, others []string, pick func(multi bool)) {
	ph, ok := c.h.(popups.Host)
	if !ok {
		pick(false)
		return
	}
	faces := []string{}
	for i := 0; i < devices; i++ {
		faces = append(faces, c.selfID())
	}
	faces = append(faces, others...)
	if len(faces) > 6 {
		faces = faces[:6]
	}
	title := "You’re in this huddle on multiple devices"
	if devices == 1 {
		title = "You’re in this huddle on a different device"
	}
	both := &popups.ModalButton{Label: "Use both devices", Kind: popups.BtnOutline, Do: func() bool { pick(true); return true }}
	sw := &popups.ModalButton{Label: "Switch to this device", Kind: popups.BtnPrimary, Do: func() bool { pick(false); return true }}
	c.post(func() {
		th := ph.Theme()
		ph.ShowOverlay(popups.NewModal(ph, popups.ModalOpts{
			Class:     "p-huddle_multi_device_modal",
			AutoFocus: both,
			Body: func(gtx layout.Context) layout.Dimensions {
				w := gtx.Constraints.Max.X
				av, gap := gtx.Dp(56), gtx.Dp(8) // x_large faces, padding 0 4px
				rowW := len(faces)*av + (len(faces)-1)*gap
				x := (w - rowW) / 2
				for i, u := range faces {
					at(gtx, image.Pt(x+i*(av+gap), 0), image.Pt(av, av), func(gtx C) D {
						avatar(gtx, th, c.h.Images(), c.avatarURL(u, av), image.Pt(av, av), gtx.Dp(8))
						return D{Size: image.Pt(av, av)}
					})
				}
				// .p-huddle_multi_device_modal__title: 22px/1.36 black, margin 20px 0
				ts := labelSize(gtx, th, 22, theme.Black, title)
				y := av + gtx.Dp(20)
				at(gtx, image.Pt((w-ts.X)/2, y), ts, func(gtx C) D { return th.Label(gtx, 22, theme.Black, th.C.Fg, title) })
				return D{Size: image.Pt(w, y+ts.Y+gtx.Dp(20))}
			},
			Buttons: []*popups.ModalButton{both, sw},
		}))
		ph.Invalidate()
	})
}
