package core

import (
	"context"
	"time"
)

// The third wave of the account's experiments (branch exp-wave-3, CDN
// build fa938a086569, 2026-11-13; docs/EXPERIMENTS.md), each read the way
// Slack's client reads it.

// URLHoverCardExperiment is BaseMrkdwnLink's
// experimentTruncatedUrlHoverCardGroupOn (boot-shared kQRT): "on" ===
// getGroupForUser("truncated_url_hover_card"). On, a message link's
// hover opens UrlHoverCard (cXYm) instead of its URL tooltip.
func (a *App) URLHoverCardExperiment() bool {
	return a.experiment("truncated_url_hover_card") == "on"
}

// HistoryRecentsRead is Fpm/ shouldMergeHistoryVisits (Q,
// isDesktopHistoryRecentsReadEnabled): "on" ===
// getGroupForUser("desktop_history_recents_read"). On, the history menu
// and the History dialog merge visits.list into the client's recents
// (qsEE B7), and opening them refreshes visits.list (WAHR
// refreshHistoryMenuVisits).
func (a *App) HistoryRecentsRead() bool {
	return a.experiment("desktop_history_recents_read") == "on"
}

// HistoryVisits is visits.list's store as the history menu reads it
// (jtFG getVisitsListSnapshot, without threads: their visits count for
// their channel), newest first; nil before the first answer.
func (a *App) HistoryVisits() []RecentNavigation {
	a.rec.mu.Lock()
	defer a.rec.mu.Unlock()
	if !a.rec.visitsLoaded {
		return nil
	}
	return append([]RecentNavigation(nil), a.rec.visits...)
}

// RefreshHistoryVisits is WAHR refreshHistoryMenuVisits, run when the
// history menu or the History dialog opens: with
// desktop_history_recents_read on, visits.list again unless its last
// answer is under a minute old (Clack's store isn't event-driven, so
// O2By f$'s ten minutes don't apply). It reports whether it called.
func (a *App) RefreshHistoryVisits(ctx context.Context) bool {
	if !a.HistoryRecentsRead() {
		return false
	}
	a.rec.mu.Lock()
	fresh := a.rec.visitsLoaded && a.rec.clock().Sub(a.rec.visitsAt) < time.Minute
	a.rec.mu.Unlock()
	if fresh {
		return false
	}
	_ = a.LoadVisits(ctx)
	return true
}

// TabOverflowRightClick is client-boot mwX5 yG: "treatment" ===
// getGroupForUser("channel_tab_overflow_right_click"). Then a right-click
// on a row of the tab bar's More menu (useTabsOverflowMenuRenderer)
// opens that tab's context menu.
func (a *App) TabOverflowRightClick() bool {
	return a.experiment("channel_tab_overflow_right_click") == "treatment"
}

// wave3Reads join experimentReads (TestExperimentReadsListed); their
// defaults are the account's groups (TestDefaultExperimentsMatchAccount).
var wave3Reads = map[string][]string{
	"truncated_url_hover_card": on, "desktop_history_recents_read": on,
	"channel_tab_overflow_right_click": {"treatment"},
}

var wave3Defaults = map[string]string{
	"truncated_url_hover_card": "on", "desktop_history_recents_read": "on",
	"channel_tab_overflow_right_click": "treatment",
}

func init() {
	for name, groups := range wave3Reads {
		experimentReads[name] = groups
	}
	for name, g := range wave3Defaults {
		defaultExperimentGroups[name] = g
	}
}
