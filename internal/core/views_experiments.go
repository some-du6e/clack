package core

// The views and navigation experiments, read the way Slack's web client
// reads them (build 4d0c78085232, 2026-11-10; docs/EXPERIMENTS.md, "Views,
// rail and navigation"). Every check is "on" === getGroupForUser(name),
// so an experiment the viewer isn't assigned to reads as off.
//
//   - client-boot kl02: isNestedSecondaryViewEnabled (W7) is
//     !isBBMEnabled && !isAgenticIaEnabled && !tbd client &&
//     nested_secondary_view; secondary_view_default_width (k5) and
//     smaller_secondary_headers (I$) only count under it. The simplified
//     layout mode (BBM) is a device pref the UI adds; Clack is never
//     Slack's "tbd" client (uYza Wx: a desktop environment flag or
//     ?tbd_client=1).
//   - boot-shared Fpm/: desktop_history_menu (Ti) and
//     desktop_history_recents_read (isDesktopHistoryRecentsReadEnabled,
//     shouldMergeHistoryVisits Q).
//   - keyboard-shortcuts-view u1zV: keyboard_shortcuts_search.
//   - view_header_overflow_borders: the Threads view (auBp
//     maybeSetIsScrolled), Flexpane (XVla) and other headers.
//   - client-boot Hm0j isListsInLaterExperimentEnabled (v): lists_in_later.

// NestedSecondaryView is kl02 isNestedSecondaryViewEnabled without the
// simplified layout mode, which the caller checks.
func (a *App) NestedSecondaryView() bool {
	return a.experiment("agentic_ia") != "on" && a.experiment("nested_secondary_view") == "on"
}

// SecondaryViewDefaultWidth is kl02 k5: the secondary view's ideal width
// is 40% of the region beside the sidebar instead of 30% of the window.
func (a *App) SecondaryViewDefaultWidth() bool {
	return a.NestedSecondaryView() && a.experiment("secondary_view_default_width") == "on"
}

// SmallerSecondaryHeaders is kl02 I$ (JvjC useIsSmallerSecondaryHeader
// adds the container and view type).
func (a *App) SmallerSecondaryHeaders() bool {
	return a.NestedSecondaryView() && a.experiment("smaller_secondary_headers") == "on"
}

// HistoryMenuExperiment is Fpm/ Ti (desktop_history_menu): the history
// menu lists 25 places instead of 15, and the desktop app gets the
// History menu (a6xy gJ).
func (a *App) HistoryMenuExperiment() bool { return a.experiment("desktop_history_menu") == "on" }

// MergeHistoryVisits is Fpm/ shouldMergeHistoryVisits (Q,
// desktop_history_recents_read): the history menu merges visits.list
// into the recents.
func (a *App) MergeHistoryVisits() bool { return a.experiment("desktop_history_recents_read") == "on" }

// KeyboardShortcutsSearch is the Keyboard Shortcuts pane's search box
// (u1zV KeyboardShortcutsSearch, keyboard_shortcuts_search).
func (a *App) KeyboardShortcutsSearch() bool {
	return a.experiment("keyboard_shortcuts_search") == "on"
}

// ViewHeaderOverflowBorders is view_header_overflow_borders: a view
// header without a border gets one once its list scrolls under it.
func (a *App) ViewHeaderOverflowBorders() bool {
	return a.experiment("view_header_overflow_borders") == "on"
}

// ListsInLater is Hm0j isListsInLaterExperimentEnabled (lists_in_later).
func (a *App) ListsInLater() bool { return a.experiment("lists_in_later") == "on" }

// viewsExperimentReads joins experimentReads (TestExperimentReadsListed).
var viewsExperimentReads = map[string][]string{
	"nested_secondary_view": on, "secondary_view_default_width": on, "smaller_secondary_headers": on,
	"desktop_history_menu": on, "desktop_history_recents_read": on, "keyboard_shortcuts_search": on,
	"view_header_overflow_borders": on, "lists_in_later": on,
}

// viewsDefaultGroups are the test account's groups for them
// (experiments.getByUser, 2026-11-10), read before it answers
// (defaultExperimentGroups).
var viewsDefaultGroups = map[string]string{
	"nested_secondary_view": "on", "secondary_view_default_width": "on", "smaller_secondary_headers": "on",
	"desktop_history_menu": "on", "desktop_history_recents_read": "on", "keyboard_shortcuts_search": "on",
	"view_header_overflow_borders": "on", "lists_in_later": "on",
}

func init() {
	for name, groups := range viewsExperimentReads {
		experimentReads[name] = groups
	}
	for name, g := range viewsDefaultGroups {
		defaultExperimentGroups[name] = g
	}
}
