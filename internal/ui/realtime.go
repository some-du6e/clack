package ui

import "gioui.org/unit"

// Live updates (Slack's realtime websocket) are on unless the user turned
// them off, as in Slack desktop. They were opt-in while the 2026-11-08
// sign-outs were open; the cause was Go's TLS fingerprint, and every
// connection to Slack, the socket included, now goes through
// internal/slack/tlsfp. Preferences > Clack has the switch (a saved "off"
// stays off), and -no-realtime (CLACK_NO_REALTIME=1) forces it off. With
// it off, core polls the Web API (counts and the open conversations every
// 20 s).

// realtimeOn reports whether the socket should be open.
func (a *App) realtimeOn() bool {
	return !a.opts.NoRealtime && (a.prefs.Realtime == nil || *a.prefs.Realtime)
}

// setRealtime saves the choice and applies it to the running session.
func (a *App) setRealtime(on bool) {
	if a.opts.NoRealtime {
		on = false
	}
	a.prefs.Realtime = &on
	a.savePrefs()
	if a.data != nil {
		c := a.data.core
		go c.SetRealtime(on) // turning it off waits for the socket to close
	}
	a.Invalidate()
}

func (a *App) toggleRealtime() { a.setRealtime(!a.realtimeOn()) }

// bannerGap is the space between a banner (demo_banner.go) and the
// workspace.
const bannerGap = unit.Dp(6)
