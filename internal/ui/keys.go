package ui

import (
	"io"
	"strings"

	"gioui.org/io/clipboard"
	"gioui.org/io/event"
	"gioui.org/io/key"
	"gioui.org/layout"
	"gioui.org/op/clip"

	"github.com/some-du6e/clack/internal/ui/shortcut"
	"github.com/some-du6e/clack/internal/ui/slackc"
)

// keyBinding is one global key binding.
type keyBinding struct {
	name key.Name
	mods key.Modifiers
	desc string
	do   func(a *App)
}

// shortcuts is the global shortcut registry. Feature packages register more
// via (*App).RegisterShortcut.
type shortcuts struct {
	list []keyBinding
	tag  int
}

// RegisterShortcut adds a global binding (e.g. "Mod+Shift+K": ⇧⌘K on
// macOS, Ctrl+Shift+K elsewhere; see internal/ui/shortcut). Bindings are
// matched when no focused widget consumes the key. shortcut.Format(combo)
// is its hint.
func (a *App) RegisterShortcut(combo, desc string, do func()) {
	n, m := parseCombo(combo)
	a.keys.list = append(a.keys.list, keyBinding{name: n, mods: m, desc: desc, do: func(*App) { do() }})
}

// parseCombo binds a combo in internal/ui/shortcut's syntax: Mod is ⌘
// on macOS and Ctrl elsewhere, Ctrl the Control key, Alt Option.
func parseCombo(s string) (key.Name, key.Modifiers) { return shortcut.Parse(s) }

func (a *App) defaultShortcuts() *shortcuts {
	s := &shortcuts{}
	add := func(combo, desc string, do func(a *App)) {
		n, m := parseCombo(combo)
		s.list = append(s.list, keyBinding{name: n, mods: m, desc: desc, do: do})
	}
	add("Mod+K", "Jump to a conversation", func(a *App) { a.openSwitcher() })
	add("Mod+T", "Jump to a conversation", func(a *App) { a.openSwitcher() })
	add("Mod+G", "Search", func(a *App) { a.openSearch("") })
	add("Mod+F", "Search in conversation", func(a *App) { a.findInConversation() })
	add("Mod+Shift+A", "All unreads", func(a *App) { a.Navigate("unreads") })
	add("Mod+Shift+T", "Threads", func(a *App) { a.Navigate("threads") })
	add("Mod+Shift+K", "DMs", func(a *App) { a.Navigate("dms") })
	add("Mod+Shift+M", "Activity", func(a *App) { a.Navigate("activity") })
	add("Mod+Shift+S", "Later", func(a *App) { a.Navigate("later") })
	add("Mod+Shift+E", "People & user groups", func(a *App) { a.Navigate(peopleRoute) })
	add("Mod+Shift+L", "Channel browser", func(a *App) { a.Navigate(channelsRoute) })
	add("Mod+Shift+D", "Toggle sidebar", func(a *App) {
		if !a.bbmOn() { // PG1Q: menuitem-toggle-sidebar ignored in the simplified layout
			a.toggleSidebar()
		}
	})
	add("Mod+Shift+Y", "Toggle dark mode", func(a *App) { a.ToggleDark() })
	// Slack's mod+shift+h (menuitem-huddle-action): "Toggle huddles start / stop"
	add("Mod+Shift+H", "Toggle huddle", func(a *App) { a.toggleHuddle() })
	add("Mod+N", "Compose", func(a *App) { a.compose() })
	add("Mod+Shift+N", "New canvas", func(a *App) { a.CreateCanvas() }) // the Create menu's Canvas hint
	add("Mod+,", "Preferences", func(a *App) { a.openPrefs() })
	add("Mod+.", "Toggle the right pane", func(a *App) { a.toggleSecondary() })
	// Slack's zoom (View > Actual Size, Zoom In, Zoom Out): Cmd/Ctrl with
	// 0, = or + (shifted or not, by keyboard), -.
	mod := shortcut.Mod()
	add("Mod+0", "Actual size", func(a *App) { a.zoom(0) })
	for _, z := range []struct {
		name key.Name
		mods key.Modifiers
		d    int
	}{
		{"=", mod, 1}, {"+", mod, 1}, {"+", mod | key.ModShift, 1},
		{"-", mod, -1},
	} {
		d := z.d
		s.list = append(s.list, keyBinding{name: z.name, mods: z.mods, desc: "Zoom", do: func(a *App) { a.zoom(d) }})
	}
	for _, b := range navBindings(shortcut.GOOS) {
		desc, dir := "Back", b.dir
		if dir > 0 {
			desc = "Forward"
		}
		s.list = append(s.list, keyBinding{name: b.name, mods: b.mods, desc: desc, do: func(a *App) { a.navigate(dir) }})
	}
	add("Alt+Up", "Previous conversation", func(a *App) { a.stepConv(-1, false) })
	add("Alt+Down", "Next conversation", func(a *App) { a.stepConv(1, false) })
	add("Alt+Shift+Up", "Previous unread", func(a *App) { a.stepConv(-1, true) })
	add("Alt+Shift+Down", "Next unread", func(a *App) { a.stepConv(1, true) })
	add("Esc", "Close / mark as read", func(a *App) {
		if a.dismissTop() {
			return
		}
		if a.splitEscape() { // the split view has the focus (split.go)
			return
		}
		// the Unreads page's Esc (UnreadsList onMarkReadKeyCommand, a
		// global key command there)
		if v, ok := a.views[a.router.Current()].(interface{ MarkActiveRead() bool }); ok && v.MarkActiveRead() {
			return
		}
		if ch := a.router.Current().Channel(); ch != "" {
			a.markRead(ch)
		}
	})
	add("Shift+Esc", "Mark all as read", func(a *App) { a.confirmMarkAllRead() })
	return s
}

// process registers the root key handler and dispatches matched shortcuts.
// It runs first in the frame, so the shortcuts win over widgets, as
// before. Keys for plugins' window/document listeners go through
// processGlobal at the end of the frame instead.
func (s *shortcuts) process(gtx layout.Context, a *App) {
	// The root tag covers the whole window so unfocused keys reach it.
	area := clip.Rect{Max: gtx.Constraints.Max}.Push(gtx.Ops)
	event.Op(gtx.Ops, &s.tag)
	area.Pop()
	filters := make([]event.Filter, 0, len(s.list))
	// a focused widget that handles plain Escape itself (shortcut.OwnEscape)
	// keeps it from "Close / mark as read"
	escOwned := false
	for _, t := range shortcut.TakeEscapeOwners() {
		if gtx.Focused(t) {
			escOwned = true
		}
	}
	if !slackc.ModalOpen() { // a modal dialog makes the page inert: its keys go to it
		for _, sc := range s.list {
			if escOwned && sc.name == key.NameEscape && sc.mods == 0 {
				continue
			}
			filters = append(filters, key.Filter{Name: sc.name, Required: sc.mods, Optional: 0})
		}
	}
	globalKeys := slackc.WantsGlobal("keydown") || slackc.WantsGlobal("keyup")
	for {
		e, ok := gtx.Event(filters...)
		if !ok {
			break
		}
		ke, ok := e.(key.Event)
		if !ok {
			continue
		}
		if globalKeys {
			forwardKey(ke)
		}
		if ke.State != key.Press {
			continue
		}
		for _, sc := range s.list {
			if sc.name == ke.Name && sc.mods == ke.Modifiers {
				sc.do(a)
				a.Invalidate()
				break
			}
		}
	}
	if a.clipboard != "" {
		gtx.Execute(clipboard.WriteCmd{Type: "application/text", Data: io.NopCloser(strings.NewReader(a.clipboard))})
		a.clipboard = ""
	}
}

// processGlobal hands plugins with window/document key listeners
// (StreamerMode, ChatTabs) the keys no widget claimed; free while none
// listens. It must run after everything else in the frame: Gio gives a
// key event to the first Event call whose filters match, and the empty
// Name matches every key, so asked any earlier it took the focused
// composer's Enter (no send, no send checks: HaikuWarning never asked)
// and every other key a widget handles.
func (s *shortcuts) processGlobal(gtx layout.Context) {
	if !slackc.WantsGlobal("keydown") && !slackc.WantsGlobal("keyup") {
		return
	}
	f := key.Filter{Optional: key.ModCtrl | key.ModShift | key.ModAlt | key.ModSuper | key.ModCommand}
	for {
		e, ok := gtx.Event(f)
		if !ok {
			break
		}
		if ke, ok := e.(key.Event); ok {
			forwardKey(ke)
		}
	}
}

func forwardKey(ke key.Event) {
	if ke.State == key.Press {
		slackc.GlobalKey("keydown", ke)
	} else {
		slackc.GlobalKey("keyup", ke)
	}
}

// copyText puts text on the system clipboard at the next frame.
func (a *App) copyText(s string) {
	a.clipboard = s
	a.Invalidate()
}

// stepConv moves to the previous/next sidebar conversation (Alt+↑/↓), or
// unread conversation when unreadOnly.
func (a *App) stepConv(dir int, unreadOnly bool) {
	if a.bbmNoListKeys() { // N8Zu: not while the list fills the simplified layout
		return
	}
	cur := a.router.Current().Channel()
	var ids []string
	for _, r := range a.sidebar.rows {
		if r.kind == sbConv {
			ids = append(ids, r.key)
		}
	}
	if len(ids) == 0 {
		return
	}
	idx := -1
	for i, id := range ids {
		if id == cur {
			idx = i
			break
		}
	}
	m := a.model()
	for step := 1; step <= len(ids); step++ {
		j := idx + dir*step
		if idx < 0 && dir < 0 {
			j = len(ids) - step
		}
		if j < 0 || j >= len(ids) {
			return
		}
		if c := m.conv(ids[j]); !unreadOnly || (c != nil && c.Unread) {
			a.Navigate("c/" + ids[j])
			return
		}
	}
}
