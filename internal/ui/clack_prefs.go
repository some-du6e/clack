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

// clackPage is the Clack tab: three rows, no headings or notes (Karim,
// 2026-11-10: "keep the settings thing calm, 3 max"). Updates (one row:
// automatic download, the step, its button), live updates, and
// troubleshooting.
func (p *prefsModal) clackPage(f *prefsForm) {
	a := p.a
	p.updatesSection(f) // update_check.go
	hint := ""
	if a.opts.NoRealtime {
		hint = "Turned off with -no-realtime or CLACK_NO_REALTIME"
	}
	f.option("realtime", "Live updates (typing, presence, instant messages)", hint, a.realtimeOn(), false, a.toggleRealtime)
	p.troubleshootingRow(f) // diagnostics.go
}
