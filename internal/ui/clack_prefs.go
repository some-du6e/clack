package ui

// Preferences > Clack: Clack's own settings (updates, live updates,
// troubleshooting), out of Slack's Advanced page. The tab sits where a
// Taut plugin's registered tab lands: after Slack's tabs[] (Advanced
// stays last there), drawn with the same nav item (Slack's c-tabs__tab,
// a 15px Slack svgIcon at 9,7). Clack's plugin tabs follow it below
// their divider.

const clackTabID = "clack"

// clackNav is the Clack tab's nav entry (extraNav puts it first).
var clackNav = navExtra{id: clackTabID, label: AppName, icon: "info"}

// clackPage is the Clack tab's page.
func (p *prefsModal) clackPage(f *prefsForm) {
	a := p.a
	p.updatesSection(f) // update_check.go
	f.hr()
	f.subheading("Live updates")
	rtHint := "Opens Slack's realtime connection. Off: messages refresh every 20 s"
	if a.opts.NoRealtime {
		rtHint = "Turned off with -no-realtime or CLACK_NO_REALTIME"
	}
	f.option("realtime", "Live updates (typing, presence, instant messages)", rtHint, a.realtimeOn(), false, a.toggleRealtime)
	f.note(AppName+" shares your browser's Slack session. On some Enterprise Grid workspaces Slack can sign that session out everywhere if it notices.", true, 0)
	f.hr()
	p.troubleshootingSection(f) // diagnostics.go
}
