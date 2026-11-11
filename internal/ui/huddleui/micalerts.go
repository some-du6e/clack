package huddleui

import (
	"image"
	"sync"
	"time"

	"gioui.org/layout"
	"gioui.org/widget"

	"github.com/some-du6e/clack/internal/huddle"
	"github.com/some-du6e/clack/internal/huddle/avperm"
	"github.com/some-du6e/clack/internal/ui/openurl"
	"github.com/some-du6e/clack/internal/ui/popups"
	"github.com/some-du6e/clack/internal/ui/theme"
)

// The microphone's permission, its no-audio check and the muted banner,
// from gantry-v2-async-client-boot-shared (build 4d0c78085232):
//
//   - huddles_av_button_permission_handling (QGma useGetDevicePermissions,
//     Q9S9 HuddleToolbarMuteButton): with it on, the mute button reads the
//     OS's microphone permission; "denied" greys it (micDisabled), puts the
//     warning-filled badge on it, its tooltip reads "Microphone permission
//     denied" and a click opens DeviceWarningModal ("Microphone access
//     required", canOpenDeviceWarningModal: the desktop app or macOS)
//     instead of unmuting. Off, the permission reads "granted".
//   - huddles_switch_microphone_helper (owyz): with more than one
//     microphone and someone else in the huddle, 20 one-second checks in a
//     row of an unmuted microphone that hasn't sent any sound raise the
//     NoAudioDetected alert, once per microphone: "We are not detecting any
//     audio from your microphone. Click here to switch to a different
//     microphone" (NoKD), whose link opens the Audio settings menu. Sound
//     from it (hasSelfAudioBeenDetected) drops the alert.
//   - useHuddleMutedVolumeTracker raises SpeakingWhenMuted while you talk
//     muted (internal/huddle devices.go); HuddlesInformationalBanner (7tWB)
//     shows it as "You’re muted." (microphone-slash) unless you closed it
//     in this call; 60 s on, it goes away and doesn't come back
//     (canShowMutedBanner false).

// micAlerts is the controller's per-huddle state for the three.
type micAlerts struct {
	mu   sync.Mutex
	s    *huddle.Session
	stop chan struct{}
	mics int // microphones listed (L.length)

	// NoAudioDetected
	device    string // the microphone the check ran for (audioDetectionDeviceId)
	performed bool   // audioDetectionWasPerformed
	count     int
	alert     bool
	closed    bool // dismissed

	// SpeakingWhenMuted's banner
	mutedShown    time.Time // the banner went up (its 60 s)
	canShowMuted  bool
	dismissedCall string
}

// micDenied is useGetDevicePermissions' "denied" for the microphone: only
// under huddles_av_button_permission_handling.
func (c *Controller) micDenied(st huddle.State) bool {
	return c.on(ExpAVButtonPermissions) && st.Mic == huddle.MicDenied
}

// micClick is the mute button: a denied microphone opens
// DeviceWarningModal (canOpenDeviceWarningModal: the experiment, and
// Clack is the desktop app), else the mute toggles. ph is where the modal
// opens.
func (c *Controller) micClick(s *huddle.Session, st huddle.State, ph popups.Host) {
	if c.micDenied(st) {
		if ph != nil {
			deviceWarningModal(ph)
		}
		return
	}
	s.SetMuted(!st.Muted)
}

// deviceWarningModal is DeviceWarningModal: "Microphone access required",
// "Allow microphone access in <a>system preferences</a>. If you’ve
// already granted permissions, quit and reopen Slack.", Close.
func deviceWarningModal(ph popups.Host) {
	th := ph.Theme()
	var link widget.Clickable
	page := micSettings(avperm.Mic)
	closeBtn := &popups.ModalButton{Label: nsHuddles.T("Close"), Kind: popups.BtnPrimary, Do: func() bool { return true }}
	ph.ShowOverlay(popups.NewModal(ph, popups.ModalOpts{
		Title:     nsHuddles.T("Microphone access required"),
		Class:     "DeviceWarningModal",
		AutoFocus: closeBtn,
		Body: func(gtx layout.Context) layout.Dimensions {
			if link.Clicked(gtx) && page != "" {
				_ = openurl.OpenSettings(page)
			}
			return linkPara(gtx, th, &link, "Allow microphone access in ", "system preferences",
				". If you’ve already granted permissions, quit and reopen "+AppName+".", page != "")
		},
		Buttons: []*popups.ModalButton{closeBtn},
	}))
	ph.Invalidate()
}

// linkPara draws before + link + after as one 15/22 paragraph, the link
// in Slack's c-link blue (plain text when it can't open anything).
func linkPara(gtx C, th *theme.Theme, cl *widget.Clickable, before, link, after string, live bool) D {
	if !live {
		return th.Para(gtx, 15, 22, theme.Regular, th.C.Fg, before+link+after)
	}
	maxW := gtx.Constraints.Max.X
	bw := th.Measure(gtx, 15, theme.Regular, before)
	lw := th.Measure(gtx, 15, theme.Regular, link)
	lh := gtx.Sp(22)
	if bw+lw > maxW { // no room on the first line: one paragraph, the link not clickable
		return th.Para(gtx, 15, 22, theme.Regular, th.C.Fg, before+link+after)
	}
	g := gtx
	g.Constraints = layout.Constraints{Max: image.Pt(maxW, lh)}
	th.Label(g, 15, theme.Regular, th.C.Fg, before)
	at(gtx, image.Pt(bw, 0), image.Pt(lw, lh), func(gtx C) D {
		return button(gtx, cl, image.Pt(lw, lh), nil, func(gtx C) D {
			d := th.Label(gtx, 15, theme.Regular, th.C.Link, link)
			if cl.Hovered() {
				theme.Fill(gtx, image.Rect(0, d.Baseline+gtx.Dp(2), lw, d.Baseline+gtx.Dp(3)), th.C.Link)
			}
			return D{Size: image.Pt(lw, lh)}
		})
	})
	// the rest wraps after the link: the remainder of its line, then lines
	rest := after
	aw := th.Measure(gtx, 15, theme.Regular, rest)
	if bw+lw+aw <= maxW {
		at(gtx, image.Pt(bw+lw, 0), image.Pt(aw, lh), func(gtx C) D {
			return th.Label(gtx, 15, theme.Regular, th.C.Fg, rest)
		})
		return D{Size: image.Pt(maxW, lh)}
	}
	var d D
	at(gtx, image.Pt(0, lh), image.Pt(maxW, gtx.Constraints.Max.Y), func(gtx C) D {
		d = th.Para(gtx, 15, 22, theme.Regular, th.C.Fg, trimLead(rest))
		return d
	})
	return D{Size: image.Pt(maxW, lh+d.Size.Y)}
}

func trimLead(s string) string {
	for len(s) > 0 && (s[0] == '.' || s[0] == ' ') {
		if s[0] == '.' {
			return s
		}
		s = s[1:]
	}
	return s
}

// startMicChecks starts the no-audio check's 1 s interval for s.
func (c *Controller) startMicChecks(s *huddle.Session) {
	m := &c.mic
	mic, _ := huddle.SelectedAudioDevices()
	stop := make(chan struct{})
	m.mu.Lock()
	if m.stop != nil {
		close(m.stop)
	}
	m.reset()
	m.s, m.stop, m.device, m.canShowMuted = s, stop, mic, true
	m.mu.Unlock()
	c.listMics()
	go func() {
		t := time.NewTicker(micCheckEvery)
		defer t.Stop()
		for {
			select {
			case <-stop:
				return
			case <-s.Done():
				return
			case <-t.C:
				c.checkNoAudio()
				c.mutedBannerTick()
			}
		}
	}()
}

// micCheckEvery is owyz's setInterval(…, 1e3); mutedBannerFor is the
// muted banner's setTimeout(…, 6e4) (tests shorten both).
var (
	micCheckEvery  = time.Second
	mutedBannerFor = 60 * time.Second
)

// stopMicChecks ends them (the huddle is over).
func (c *Controller) stopMicChecks() {
	m := &c.mic
	m.mu.Lock()
	if m.stop != nil {
		close(m.stop)
	}
	m.reset()
	m.mu.Unlock()
}

// reset clears everything but the mutex (callers hold mu).
func (m *micAlerts) reset() {
	m.s, m.stop, m.mics = nil, nil, 0
	m.device, m.performed, m.count, m.alert, m.closed = "", false, 0, false, false
	m.mutedShown, m.canShowMuted, m.dismissedCall = time.Time{}, false, ""
}

// listMics counts the microphones (the devices' microphone list).
func (c *Controller) listMics() {
	go func() {
		l, err := listAudioDevices(true)
		if err != nil {
			return
		}
		c.mic.mu.Lock()
		c.mic.mics = len(l)
		c.mic.mu.Unlock()
	}()
}

// listAudioDevices is huddle.AudioDevices (tests list fakes).
var listAudioDevices = huddle.AudioDevices

// checkNoAudio is the owyz interval's body.
func (c *Controller) checkNoAudio() {
	m := &c.mic
	m.mu.Lock()
	s := m.s
	m.mu.Unlock()
	if s == nil {
		return
	}
	if !c.on(ExpSwitchMicHelper) {
		m.mu.Lock()
		m.alert = false
		m.mu.Unlock()
		return
	}
	st := s.State()
	detected := s.SelfAudioDetected()
	mic, _ := huddle.SelectedAudioDevices()
	raised := false
	m.mu.Lock()
	if m.device != mic { // another microphone: check it afresh
		m.device, m.performed, m.count, m.alert, m.closed = mic, false, 0, false, false
	}
	e := m.alert
	switch {
	case m.performed && !e:
	case m.mics <= 1:
	case detected && !m.performed:
		m.performed = true
	case detected && e:
		m.alert, m.performed = false, true
	case len(st.Participants) > 1 && !e:
		if st.Muted && !m.performed {
			m.count = 0
		} else if !st.Muted && !detected && !m.performed {
			m.count++
			if m.count >= 20 {
				m.performed, m.alert, raised = true, true, true
			}
		}
	}
	m.mu.Unlock()
	if raised {
		c.changed()
	}
}

// noAudioAlert reports whether the NoAudioDetected alert shows.
func (c *Controller) noAudioAlert() bool {
	m := &c.mic
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.alert && !m.closed && c.on(ExpSwitchMicHelper)
}

// closeNoAudio is the alert's close button.
func (c *Controller) closeNoAudio() {
	c.mic.mu.Lock()
	c.mic.closed = true
	c.mic.mu.Unlock()
	c.changed()
}

// mutedBanner reports whether "You’re muted." shows for s.
func (c *Controller) mutedBanner(s *huddle.Session) bool {
	if s == nil || !s.SpeakingWhileMuted() {
		return false
	}
	m := &c.mic
	m.mu.Lock()
	defer m.mu.Unlock()
	if !m.canShowMuted || m.dismissedCall == s.RoomID() {
		return false
	}
	if m.mutedShown.IsZero() {
		m.mutedShown = time.Now()
	}
	return true
}

// mutedBannerTick runs the banner's 60 s timeout: the alert goes away and
// so does the banner, for good (canShowMutedBanner false).
func (c *Controller) mutedBannerTick() {
	m := &c.mic
	m.mu.Lock()
	s := m.s
	up := !m.mutedShown.IsZero()
	expired := up && time.Since(m.mutedShown) >= mutedBannerFor
	if s != nil && up && !s.SpeakingWhileMuted() {
		m.mutedShown = time.Time{} // unmuted: the next one gets its own 60 s
	}
	if expired {
		m.canShowMuted, m.mutedShown = false, time.Time{}
	}
	m.mu.Unlock()
	if expired && s != nil {
		s.ClearSpeakingWhileMuted()
		c.changed()
	}
}

// closeMutedBanner is its close button: not again in this call.
func (c *Controller) closeMutedBanner(s *huddle.Session) {
	m := &c.mic
	m.mu.Lock()
	if s != nil {
		m.dismissedCall = s.RoomID()
	}
	m.canShowMuted, m.mutedShown = false, time.Time{}
	m.mu.Unlock()
	if s != nil {
		s.ClearSpeakingWhileMuted()
	}
	c.changed()
}

// switchMic is the menus' microphone pick: SelectAudioDevices, then the
// session reopens it (setNewPreferredDevice).
func (c *Controller) switchMic(s *huddle.Session, id string) {
	_, out := huddle.SelectedAudioDevices()
	huddle.SelectAudioDevices(id, out)
	if s != nil {
		s.SwitchAudio(false, true)
	}
	c.changed()
}

// switchSpeaker is the Speaker pick: the speakers reopen on it and play
// the test sound (useAudioOutSettingsMenuItems awaits onTestAudioSelected).
func (c *Controller) switchSpeaker(s *huddle.Session, id string) {
	mic, _ := huddle.SelectedAudioDevices()
	huddle.SelectAudioDevices(mic, id)
	if s != nil {
		s.SwitchAudio(true, false)
		c.testSpeaker(s)
	}
	c.changed()
}

// testSpeakerSound is Test Speaker's sound (BLe4 pW).
const testSpeakerSound = "complete_quest_requirement-74d22f6.mp3"

// testSpeaker plays it on your speakers only.
func (c *Controller) testSpeaker(s *huddle.Session) {
	go func() {
		pcm, err := loadMusic(testSpeakerSound)
		if err != nil || s == nil {
			return
		}
		s.PlayLocal(pcm, false)
	}()
}

// layoutNoAudio is the NoAudioDetected alert in the huddle window: the
// alert card with microphone-slash, Slack's bold text and its link,
// which opens the Audio settings menu; a close button dismisses it.
func (w *window) layoutNoAudio(gtx C, s *huddle.Session, width int) D {
	th := w.th
	if w.click("noaudio-close").Clicked(gtx) {
		w.c.closeNoAudio()
		return D{}
	}
	if w.click("noaudio-link").Clicked(gtx) {
		w.toggleAudio(s)
	}
	pad, gap := gtx.Dp(10), gtx.Dp(8)
	ic, xs := gtx.Dp(16), gtx.Dp(20)
	tw := max(0, width-2*pad-ic-gap-xs-gap)
	const text = "We are not detecting any audio from your microphone."
	const link = "Click here to switch to a different microphone"
	tg := gtx
	tg.Constraints = layout.Constraints{Max: image.Pt(tw, gtx.Dp(200))}
	tm := recordPara(tg, th, theme.Bold, text)
	lw := labelSize(gtx, th, 13, theme.Bold, link)
	space := th.Measure(gtx, 13, theme.Bold, " ")
	lx, ly := tm.size.X+space, tm.size.Y-lw.Y // inline after the text's last line when it fits
	if lx+lw.X > tw {
		lx, ly = 0, tm.size.Y
	}
	h := pad + max(ly+lw.Y, ic) + pad
	theme.RRect(gtx, image.Rectangle{Max: image.Pt(width, h)}, gtx.Dp(8), hudWarnBg)
	at(gtx, image.Pt(pad, pad+gtx.Dp(1)), image.Pt(ic, ic), func(gtx C) D {
		iconAt(gtx, image.Pt(ic, ic), "mic-off", 16, hudWarn)
		return D{Size: image.Pt(ic, ic)}
	})
	at(gtx, image.Pt(pad+ic+gap, pad), tm.size, func(gtx C) D { tm.call.Add(gtx.Ops); return D{Size: tm.size} })
	cl := w.click("noaudio-link")
	at(gtx, image.Pt(pad+ic+gap+lx, pad+ly), lw, func(gtx C) D {
		return button(gtx, cl, lw, nil, func(gtx C) D {
			d := th.Label(gtx, 13, theme.Bold, hudWhite, link)
			theme.Fill(gtx, image.Rect(0, d.Baseline+gtx.Dp(2), lw.X, d.Baseline+gtx.Dp(3)), hudWhite)
			return d
		})
	})
	at(gtx, image.Pt(width-pad-xs, pad-gtx.Dp(2)), image.Pt(xs, xs), func(gtx C) D {
		return button(gtx, w.click("noaudio-close"), image.Pt(xs, xs), func(gtx C, hov bool) {
			if hov {
				theme.RRect(gtx, image.Rectangle{Max: image.Pt(xs, xs)}, gtx.Dp(4), hudBtn)
			}
		}, func(gtx C) D { iconAt(gtx, image.Pt(xs, xs), "close", 12, hudWhite); return D{} })
	})
	return D{Size: image.Pt(width, h)}
}
