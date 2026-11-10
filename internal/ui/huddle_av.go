package ui

import "github.com/some-du6e/clack/internal/huddle"

// Preferences > Audio & video's device prefs for the huddle microphone:
// Slack keeps "Enable automatic gain control" and "Enable noise
// suppression" in localStorage (prefs.audio: enableAGC defaults to true,
// enableNoiseSuppression to false), so Clack keeps them in ui.json.

// HuddleAudioPrefs is huddleui.AudioPrefsHost.
func (a *App) HuddleAudioPrefs() (agc, ns bool) {
	return a.prefs.HuddleAGC == nil || *a.prefs.HuddleAGC, a.prefs.HuddleNS
}

// setHuddleAudio stores them and applies them to the huddle you're in
// (Slack's setAGC / setNoiseSuppression).
func (a *App) setHuddleAudio(agc, ns bool) {
	a.prefs.HuddleAGC, a.prefs.HuddleNS = &agc, ns
	a.savePrefs()
	if a.hud != nil {
		a.hud.SetMicProcessing(agc, ns)
	}
	a.Invalidate()
}

// RaiseMainWindow is huddleui.RaiseHost: the global mute shortcut brings
// the main window forward, as Slack's main process does.
func (a *App) RaiseMainWindow() { a.Raise() }

// HuddleVideoEffect is huddleui.VideoPrefsHost: Slack's localStorage
// prefs.video.effect, kept in ui.json.
func (a *App) HuddleVideoEffect() huddle.VideoEffect {
	return huddle.VideoEffect(a.prefs.HuddleVideoEffect)
}

// huddleBlurOn is the checkbox's state: checked when the effect is "blur".
func (a *App) huddleBlurOn() bool { return a.HuddleVideoEffect() == huddle.VideoEffectBlur }

// toggleHuddleBlur is the "Blur your video background…" checkbox: Slack's
// onChange sets effect to "blur", or unsets it when it was "blur" (a "bg"
// image becomes "blur"), saves prefs and dispatches the effect, which the
// huddle's camera picks up from its next frame.
func (a *App) toggleHuddleBlur() {
	e := huddle.VideoEffectBlur
	if a.HuddleVideoEffect() == huddle.VideoEffectBlur {
		e = huddle.VideoEffectNone
	}
	a.prefs.HuddleVideoEffect = string(e)
	a.savePrefs()
	if a.hud != nil {
		a.hud.SetVideoEffect(e)
	}
	a.Invalidate()
}
