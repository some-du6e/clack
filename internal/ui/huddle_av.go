package ui

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
