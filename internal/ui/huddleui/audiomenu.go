package huddleui

import (
	"github.com/some-du6e/clack/internal/huddle"
	"github.com/some-du6e/clack/internal/ui/popups"
)

// The mute button in the huddle window and the bottom bar
// (HuddleToolbarMuteButton with isHuddleWindow or inBottomBar,
// gantry-v2-async-client-boot-shared Q9S9) is segmented: the mute half
// (36px) and an "Audio settings" caret (28px, caret-up, caret-down while
// its menu is open; toggled on with the menu open or the mic on). The
// caret opens HuddleAVDevicesMenu (LmPT) above it:
//
//	Microphone            (header)
//	  ◉ each microphone   (gmEJ: radio rows, the one in use checked)
//	────
//	Speaker               (header; the desktop app always lists them)
//	  ◉ each output       (QdEk: picking one also plays Test Speaker)
//	────
//	Test Speaker          (complete_quest_requirement, on your speakers)
//	[────  Enable noise suppression]   (huddles_noise_suppression_awareness, NS off)
//	────
//	Audio & video preferences          (yBJd: Preferences › Audio & video)
//
// A pick goes to SelectAudioDevices and the session reopens that side of
// the sound card mid-huddle (huddle.Session.SwitchAudio).

// micCtls is the segmented mute button: its two halves.
func (c *Controller) micCtls(st huddle.State, menuOpen bool) []ctl {
	denied := c.micDenied(st)
	enabled := !st.Muted // isMicrophoneEnabled
	main := ctl{id: "mic", icon: "mic", on: enabled, tip: nsHuddles.T("Mute mic"), seg: segMain}
	if !enabled {
		main.icon, main.tip = "mic-off", nsHuddles.T("Unmute mic")
	}
	if denied { // micDisabled, the warning badge, "Microphone permission denied"
		main.icon, main.on, main.warn, main.dim, main.tip = "mic-off", false, true, true, nsHuddles.T("Microphone permission denied")
	}
	caret := ctl{id: "micmenu", icon: "caret-up", on: menuOpen || (enabled && !denied), tip: nsHuddles.T("Audio settings"), seg: segCaret}
	if menuOpen {
		caret.icon = "caret-down"
	}
	return []ctl{main, caret}
}

// audioMenu is HuddleAVDevicesMenu's rows for s.
func (c *Controller) audioMenu(s *huddle.Session) []popups.MenuItem {
	mics, _ := listAudioDevices(true)
	outs, _ := listAudioDevices(false)
	micSel, outSel := huddle.SelectedAudioDevices()
	c.mic.mu.Lock()
	c.mic.mics = len(mics)
	c.mic.mu.Unlock()
	items := []popups.MenuItem{{Header: nsHuddles.T("Microphone")}}
	for _, d := range mics {
		id := d.ID
		items = append(items, popups.MenuItem{Key: "av-microphone-device-menu-item_" + d.Name, Label: d.Name, Radio: true,
			Checked: inUse(d, micSel), Do: func() { c.switchMic(s, id) }})
	}
	items = append(items, popups.Sep(), popups.MenuItem{Header: nsHuddles.T("Speaker")})
	for _, d := range outs {
		id := d.ID
		items = append(items, popups.MenuItem{Key: "av-speaker-device-menu-item_" + d.Name, Label: d.Name, Radio: true,
			Checked: inUse(d, outSel), Do: func() { c.switchSpeaker(s, id) }})
	}
	items = append(items, popups.Sep(), popups.MenuItem{Key: "test-audio", Label: nsHuddles.T("Test Speaker"), Icon: "sound",
		KeepOpen: true, Do: func() { c.testSpeaker(s) }})
	if s != nil && c.on(ExpNoiseSuppAwareness) {
		if _, ns := s.MicProcessing(); !ns { // useShowNoiseSuppressionMenuItem: only while it's off
			items = append(items, popups.Sep(), popups.MenuItem{Key: "noise-suppression", Label: nsHuddles.T("Enable noise suppression"),
				Checked: ns, KeepOpen: true, Do: func() { c.setNoiseSuppression(s, true) }})
		}
	}
	items = append(items, popups.Sep(), popups.MenuItem{Key: "av-preferences", Label: nsHuddles.T("Audio & video preferences"), External: true, Do: func() {
		if p, ok := c.h.(interface{ OpenPreferences(section string) }); ok {
			c.post(func() { p.OpenPreferences("video_audio") })
		}
	}})
	return items
}

// inUse reports whether d is the device in use: the pick, or the
// system's default when there's none (Chromium lists the default first,
// and Slack checks the first).
func inUse(d huddle.AudioDevice, sel string) bool {
	if sel == "" {
		return d.Default
	}
	return d.ID == sel
}

// AudioPrefsSaver stores Audio & video's device prefs (the ui root's
// ui.json copy of Slack's localStorage prefs.audio).
type AudioPrefsSaver interface {
	SetHuddleAudioPrefs(agc, ns bool)
}

// setNoiseSuppression is the menu's checkbox: prefs.audio.
// enableNoiseSuppression saved, and the session's processing follows.
func (c *Controller) setNoiseSuppression(s *huddle.Session, on bool) {
	agc, _ := c.audioPrefs()
	if h, ok := c.h.(AudioPrefsSaver); ok {
		c.post(func() { h.SetHuddleAudioPrefs(agc, on) })
	} else {
		s.SetMicProcessing(agc, on)
	}
	c.changed()
}
