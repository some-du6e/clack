package ui

import (
	"io"
	"strings"

	"gioui.org/io/clipboard"
	"gioui.org/io/event"
	"gioui.org/io/key"
	"gioui.org/layout"
	"gioui.org/op/clip"
)

// shortcut is one global key binding.
type shortcut struct {
	name key.Name
	mods key.Modifiers
	desc string
	do   func(a *App)
}

// shortcuts is the global shortcut registry. Feature packages register more
// via (*App).RegisterShortcut.
type shortcuts struct {
	list []shortcut
	tag  int
}

// RegisterShortcut adds a global binding (e.g. "Ctrl+Shift+K"). Bindings are
// matched when no focused widget consumes the key.
func (a *App) RegisterShortcut(combo, desc string, do func()) {
	n, m := parseCombo(combo)
	a.keys.list = append(a.keys.list, shortcut{name: n, mods: m, desc: desc, do: func(*App) { do() }})
}

func parseCombo(s string) (key.Name, key.Modifiers) {
	var m key.Modifiers
	parts := strings.Split(s, "+")
	for _, p := range parts[:len(parts)-1] {
		switch strings.ToLower(p) {
		case "ctrl", "cmd", "mod":
			m |= key.ModShortcut
		case "shift":
			m |= key.ModShift
		case "alt":
			m |= key.ModAlt
		}
	}
	last := parts[len(parts)-1]
	switch strings.ToLower(last) {
	case "up":
		return key.NameUpArrow, m
	case "down":
		return key.NameDownArrow, m
	case "left":
		return key.NameLeftArrow, m
	case "right":
		return key.NameRightArrow, m
	case "esc", "escape":
		return key.NameEscape, m
	case "enter":
		return key.NameReturn, m
	}
	return key.Name(strings.ToUpper(last)), m
}

func (a *App) defaultShortcuts() *shortcuts {
	s := &shortcuts{}
	add := func(combo, desc string, do func(a *App)) {
		n, m := parseCombo(combo)
		s.list = append(s.list, shortcut{name: n, mods: m, desc: desc, do: do})
	}
	add("Ctrl+K", "Jump to a conversation", func(a *App) { a.openSwitcher() })
	add("Ctrl+T", "Jump to a conversation", func(a *App) { a.openSwitcher() })
	add("Ctrl+G", "Search", func(a *App) { a.openSearch("") })
	add("Ctrl+F", "Search in conversation", func(a *App) {
		if ch := a.router.Current().Channel(); ch != "" {
			a.openSearch("in:<#" + ch + ">")
		} else {
			a.openSearch("")
		}
	})
	add("Ctrl+Shift+A", "All unreads", func(a *App) { a.Navigate("unreads") })
	add("Ctrl+Shift+T", "Threads", func(a *App) { a.Navigate("threads") })
	add("Ctrl+Shift+K", "DMs", func(a *App) { a.Navigate("dms") })
	add("Ctrl+Shift+M", "Activity", func(a *App) { a.Navigate("activity") })
	add("Ctrl+Shift+S", "Later", func(a *App) { a.Navigate("later") })
	add("Ctrl+Shift+E", "Directories", func(a *App) { a.Navigate("directories") })
	add("Ctrl+Shift+D", "Toggle sidebar", func(a *App) { a.sidebar.hidden = !a.sidebar.hidden })
	add("Ctrl+Shift+Y", "Toggle dark mode", func(a *App) { a.ToggleDark() })
	add("Ctrl+N", "Compose", func(a *App) { a.compose() })
	add("Ctrl+,", "Preferences", func(a *App) { a.openPrefs() })
	add("Ctrl+.", "Toggle the right pane", func(a *App) { a.toggleSecondary() })
	add("Ctrl+[", "Back", func(a *App) { a.Back() })
	add("Ctrl+]", "Forward", func(a *App) { a.Forward() })
	add("Alt+Left", "Back", func(a *App) { a.Back() })
	add("Alt+Right", "Forward", func(a *App) { a.Forward() })
	add("Alt+Up", "Previous conversation", func(a *App) { a.stepConv(-1, false) })
	add("Alt+Down", "Next conversation", func(a *App) { a.stepConv(1, false) })
	add("Alt+Shift+Up", "Previous unread", func(a *App) { a.stepConv(-1, true) })
	add("Alt+Shift+Down", "Next unread", func(a *App) { a.stepConv(1, true) })
	add("Esc", "Close / mark as read", func(a *App) {
		if a.dismissTop() {
			return
		}
		if ch := a.router.Current().Channel(); ch != "" {
			a.markRead(ch)
		}
	})
	add("Shift+Esc", "Mark all as read", func(a *App) {
		if a.data != nil {
			a.data.markAllRead()
		}
	})
	return s
}

// process registers the root key handler and dispatches matched shortcuts.
func (s *shortcuts) process(gtx layout.Context, a *App) {
	// The root tag covers the whole window so unfocused keys reach it.
	area := clip.Rect{Max: gtx.Constraints.Max}.Push(gtx.Ops)
	event.Op(gtx.Ops, &s.tag)
	area.Pop()
	filters := make([]event.Filter, 0, len(s.list))
	for _, sc := range s.list {
		filters = append(filters, key.Filter{Name: sc.name, Required: sc.mods, Optional: 0})
	}
	for {
		e, ok := gtx.Event(filters...)
		if !ok {
			break
		}
		ke, ok := e.(key.Event)
		if !ok || ke.State != key.Press {
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

// copyText puts text on the system clipboard at the next frame.
func (a *App) copyText(s string) {
	a.clipboard = s
	a.Invalidate()
}

// stepConv moves to the previous/next sidebar conversation (Alt+↑/↓), or
// unread conversation when unreadOnly.
func (a *App) stepConv(dir int, unreadOnly bool) {
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
