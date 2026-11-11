package ui

import (
	"image"

	"github.com/some-du6e/clack/internal/core"
	"github.com/some-du6e/clack/internal/ui/shortcut"
	"github.com/some-du6e/clack/internal/ui/views"
)

// The Keyboard Shortcuts pane (views.KeyboardShortcuts, Slack's
// KeyboardShortcutsFlexpane) in the secondary column: ⌘/ (Ctrl+/) toggles
// it ("⌘/ to toggle this panel"), as do Help > Keyboard Shortcuts and the
// top bar's help button. It replaces the toast that listed four
// shortcuts.

// secShortcuts is the pane's secondary kind, outside secondary.go's
// iota block (which the split view's branch reshapes).
const secShortcuts secKind = 64

// toggleKeyboardShortcuts opens the pane, or closes it when it shows.
func (a *App) toggleKeyboardShortcuts() {
	if a.secondary.kind == secShortcuts && a.secondary.open() {
		a.CloseSecondary()
		return
	}
	a.secondary = secondaryState{kind: secShortcuts, width: a.secondary.width}
	a.secondary.view = views.NewKeyboardShortcuts(a, a.shortcutListOpts)
	a.Invalidate()
}

// shortcutListOpts are shortcutsData's inputs.
func (a *App) shortcutListOpts() shortcut.ListOpts {
	o := shortcut.ListOpts{
		GOOS: shortcut.GOOS, KeyboardLocale: "us",
		ActivityTab: func(t string) bool {
			return t == core.TabActivity || t == core.TabActivityInbox || t == core.TabActivityProto
		},
		Huddles: true, CanvasAccess: true, CanvasCreate: true, // Hack Club has both (ed.pl, zK7j, u+51)
		UndoSend: true, // UnsendLast (unsend.go)
		UpToEdit: a.prefs.ComposerUpArrow == 3,
	}
	r := a.model().Rail
	o.TabRail = r.Rail
	o.TabLabels = map[string]string{}
	for id, t := range r.Tabs {
		o.TabLabels[id] = t.Label
	}
	o.ActivityLabel = "Activity"
	if c := a.Core(); c != nil {
		o.ActivityInbox = c.Experiment("activity_inbox") == "on"
		o.LaterTodos = c.Experiment("later_todos") == "on"
		o.SubmitForward = c.SubmitForwardKeybinding()
		raw, _ := c.TeamPref("private_message_forwarding")
		o.PrivateForward = string(raw) == "true"
	}
	return o
}

// RunShortcut is executeKeyboardShortcut (u1zV eu): a row's tab opens
// that tab, its id runs the shortcut.
func (a *App) RunShortcut(id, tab string) {
	if tab != "" {
		a.railClick(railKey(tab), image.Pt(0, a.px(100)))
		return
	}
	switch id {
	case "home":
		a.railClick("home", image.Point{})
	case "browse_dms":
		a.Navigate("dms")
	case "open_preferences":
		a.openPrefs()
	case "open_help":
		a.helpClick()
	case "open_all_threads":
		a.Navigate("threads")
	case "open_all_unreads":
		a.Navigate("unreads")
	case "browse_channels":
		a.Navigate(channelsRoute)
	case "new_message":
		a.compose()
	case "new_canvas":
		a.CreateCanvas()
	case "set_a_status":
		a.setStatusPrompt()
	case "quick_switcher":
		a.openSwitcher()
	case "open_later":
		a.Navigate("later")
	case "open_activity":
		a.Navigate("activity")
	}
}
