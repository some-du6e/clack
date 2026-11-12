package ui

// Preferences > Clack: Clack's own settings (updates, Replay
// introduction), out of Slack's Advanced page. The tab sits where a
// Taut plugin's registered tab lands: after Slack's tabs[] (Advanced
// stays last there), drawn with the same nav item (Slack's c-tabs__tab,
// a 15px Slack svgIcon at 9,7). Clack's plugin tabs follow it below
// their divider.

const clackTabID = "clack"

// clackNav is the Clack tab's nav entry (extraNav puts it first).
var clackNav = navExtra{id: clackTabID, label: AppName, icon: "info"}

// clackIntroRow is the tab's second row, Replay introduction: branch
// intro-motion's self-contained prefsReplayIntroRow, hooked in from that
// file (an init doing clackIntroRow = (*prefsModal).prefsReplayIntroRow),
// so either branch can merge first. nil: the slot stays empty.
var clackIntroRow func(p *prefsModal, f *prefsForm)

// clackPage is the Clack tab: three rows at most, no headings or notes
// (Karim, 2026-11-10: "keep the settings thing calm, 3 max"). Updates
// (one row: automatic download, the step, its button), Replay
// introduction. Live updates have no switch: they're always on, as in
// Slack (realtime.go). Troubleshooting (logs, Copy Diagnostics,
// freeze reports) is Help > Troubleshooting (appmenu.go).
func (p *prefsModal) clackPage(f *prefsForm) {
	p.updatesSection(f) // update_check.go
	if clackIntroRow != nil {
		clackIntroRow(p, f)
	}
}
