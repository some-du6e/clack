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
//   - boot-shared Fpm/: desktop_history_menu (Ti). (Its
//     desktop_history_recents_read, shouldMergeHistoryVisits Q, merges
//     visits.list into the recents; Clack doesn't call visits.list, so it
//     doesn't read it.)
//   - keyboard-shortcuts-view u1zV: keyboard_shortcuts_search.
//   - view_header_overflow_borders: the Threads view (auBp
//     maybeSetIsScrolled), Flexpane (XVla) and other headers.
//   - +o0o j: submit_forward_keybinding (the Keyboard Shortcuts pane's
//     "Submit forward dialog" row).

// NestedSecondaryView is kl02 isNestedSecondaryViewEnabled without the
// simplified layout mode, which the caller checks.
func (a *App) NestedSecondaryView() bool {
	return a.experiment("agentic_ia") != "on" && a.experiment("nested_secondary_view") == "on"
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

// SubmitForwardKeybinding is +o0o j (submit_forward_keybinding): the
// Keyboard Shortcuts pane lists "Submit forward dialog" (⌘Enter).
func (a *App) SubmitForwardKeybinding() bool {
	return a.experiment("submit_forward_keybinding") == "on"
}

// viewsExperimentReads joins experimentReads (TestExperimentReadsListed).
var viewsExperimentReads = map[string][]string{
	"nested_secondary_view": on, "smaller_secondary_headers": on, "desktop_history_menu": on,
	"keyboard_shortcuts_search": on, "view_header_overflow_borders": on, "submit_forward_keybinding": on,
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
