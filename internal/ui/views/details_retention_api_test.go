package views

// What details_retention_test.go reaches of details_retention.go and
// core's retention API (a red run stubs this file alone).

import (
	"slices"

	"github.com/some-du6e/clack/internal/core"
)

const retSecExport = cdSecExport

type retR struct{ Type, Duration int }

func retPaid(e *cdTestEnv) { e.h.app.SetTeamPlan("enterprise", "E1") }

func retHistoryShown(e *cdTestEnv) bool {
	inf, _, _ := e.d.info.get()
	return e.d.historyShown(inf)
}

func retRowText(e *cdTestEnv) string {
	inf, _, _ := e.d.info.get()
	return e.d.retentionRowText(inf)
}

func retSettingsTab(e *cdTestEnv) bool { return slices.Contains(e.d.tabKinds, cdSettings) }

func retLoaded(e *cdTestEnv) bool { rt := e.d.x.dlg.rt; return rt != nil && rt.loaded }

func retType(e *cdTestEnv) int {
	if rt := e.d.x.dlg.rt; rt != nil {
		return rt.typ
	}
	return -1
}

func retDuration(e *cdTestEnv) string {
	if rt := e.d.x.dlg.rt; rt != nil {
		return rt.dur.Text()
	}
	return ""
}

func retSetDuration(e *cdTestEnv, s string) {
	if rt := e.d.x.dlg.rt; rt != nil {
		rt.dur.SetText(s)
	}
}

// retPickUnit picks the select's option i (0 days, 1 years) as a click
// on the menu does.
func retPickUnit(e *cdTestEnv, i int) {
	if rt := e.d.x.dlg.rt; rt != nil {
		rt.unitDD.picked = i + 1
	}
}

func retErr(e *cdTestEnv) string {
	if rt := e.d.x.dlg.rt; rt != nil {
		return rt.err
	}
	return ""
}

func retText(r retR) string { return cdRetentionText(core.Retention(r)) }

func retTeamDesc(dur, typ int) string { return cdTeamRetentionDescription(dur, typ) }

func retWarning(typ, n int, unit int, team retR, admin bool) string {
	return cdRetentionWarning(typ, n, true, unit, core.Retention(team), admin)
}

func retDurErr(n, unit int) string { return cdRetentionDurationError(n, unit) }
