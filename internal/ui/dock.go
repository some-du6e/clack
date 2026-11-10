package ui

import (
	"runtime"
	"strconv"

	"github.com/some-du6e/clack/internal/desktop"
	"github.com/some-du6e/clack/internal/ui/uipref"
)

// The macOS Dock icon, as Slack's desktop app drives it from the web
// client's badge state (boot-render eow, the shell's app.dock):
//
//   - the badge is the app badge count (mentions and DMs) when there is
//     one; otherwise, with mac_ssb_bullet (default on), "•" while
//     anything is unread; otherwise nothing;
//   - each notification bounces the icon when mac_ssb_bounce is set:
//     "short" once, "long" until Clack is brought forward.

// dockBadge and dockBounce are desktop's (tests swap them).
var (
	dockBadge  = desktop.SetDockBadge
	dockBounce = desktop.BounceDock
)

// dockBadgeLabel is the badge for count highlights and unread state.
func dockBadgeLabel(count int, unread, bullet bool) string {
	switch {
	case count > 0:
		return strconv.Itoa(count)
	case unread && bullet:
		return "•"
	}
	return ""
}

// updateDock sets the badge from the model when it changed (macOS only).
func (a *App) updateDock() {
	if runtime.GOOS != "darwin" && !dockAnyOS {
		return
	}
	if a.data != nil && !a.data.core.CountsLive() {
		// a warm start's counts are the boot snapshot's: the Dock (seen
		// outside Clack) waits for client.counts, as Slack's badge waits
		// for its counts to apply (unreadCounts.countsApplied)
		return
	}
	m := a.model()
	l := dockBadgeLabel(m.HomeCount, m.HomeUnread || m.DMUnread, !uipref.Get().NoMacBullet)
	if l == a.dock.label && a.dock.set {
		return
	}
	a.dock.label, a.dock.set = l, true
	dockBadge(l)
}

// bounceDock is a notification's bounce (MacAppearancePref).
func (a *App) bounceDock() {
	if runtime.GOOS != "darwin" && !dockAnyOS {
		return
	}
	switch uipref.Get().MacBounce {
	case "short":
		dockBounce(false)
	case "long":
		dockBounce(true)
	}
}

type dockState struct {
	label string
	set   bool
}

// dockAnyOS runs the Dock logic on every OS (tests).
var dockAnyOS bool

// macAppearance is Slack's MacAppearancePref (Sounds & appearance, macOS).
func (p *prefsModal) macAppearance(f *prefsForm) {
	bullet := p.prefBool("mac_ssb_bullet", true)
	f.option("pref.mac_ssb_bullet", "Show a badge on Slack’s icon to indicate new activity", "", bullet, false,
		func() { p.setPref("mac_ssb_bullet", !bullet) })
	r := p.prefStr("mac_ssb_bounce")
	if p.ra.bounce == "" { // useState(""===r?"short":r)
		p.ra.bounce = r
		if r == "" {
			p.ra.bounce = "short"
		}
	}
	l := p.ra.bounce
	f.option("pref.mac_ssb_bounce", "Bounce Slack’s icon when receiving a notification", "", r != "", false, func() {
		v := ""
		if r == "" {
			v = l
		}
		p.setPref("mac_ssb_bounce", v)
	})
	f.optionAt("pref.mac_ssb_bounce_short", "Only bounce it once", "", l == "short", false, r != "", func() {
		v := "long"
		if l != "short" {
			v = "short"
		}
		p.ra.bounce = v
		p.setPref("mac_ssb_bounce", v)
	})
}
