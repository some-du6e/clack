package ui

import "gioui.org/unit"

// Live updates (Slack's realtime websocket) are always on, as in Slack
// desktop, which has no switch for them. They were opt-in while the
// 2026-11-08 sign-outs were open; the cause was Go's TLS fingerprint, and
// every connection to Slack, the socket included, now goes through
// internal/slack/tlsfp. The Preferences switch is gone (Karim, 2026-11-12:
// "thats a stupid fucking setting"), and a saved "realtime": false from
// an older Clack is ignored. Only -no-realtime (CLACK_NO_REALTIME=1), for
// debugging, keeps the socket closed; then core polls the Web API (counts
// and the open conversations every 20 s).

// realtimeOn reports whether the socket should be open.
func (a *App) realtimeOn() bool { return !a.opts.NoRealtime }

// bannerGap is the space between a banner (demo_banner.go) and the
// workspace.
const bannerGap = unit.Dp(6)
