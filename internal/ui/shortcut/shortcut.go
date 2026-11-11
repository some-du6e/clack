// Package shortcut formats and parses Clack's keyboard shortcuts the way
// Slack does on each platform. Every shortcut hint Clack shows (menu
// rows, tooltips, preferences, toasts) goes through Format or Chips.
// Clack's code writes the combo once, in a platform-neutral syntax:
//
//	"Mod+Shift+K"   Mod is ⌘ on macOS and Ctrl elsewhere (Cmd is the same)
//	"Ctrl+1"        Ctrl is the Control key everywhere (⌃ on macOS)
//	"Alt+Up"        Alt is ⌥ Option on macOS
//	"Mod++"         a trailing "+" key
//
// Read from Slack's web client (build a84662d2d135, 2026-11-08):
//
//   - Menu hints are literal per-platform strings built with
//     isMacOS() (module +oeN cX): "⌘C" / "Ctrl+C" (Copy text, Copy
//     message), "⇧⌘S" / "Ctrl+Shift+S" (Save with to-dos), "⌘F" / "Ctrl+F".
//     On macOS the glyphs come in AppKit's order ⌃⌥⇧⌘ with no
//     separator; elsewhere the names are joined with "+" as Ctrl, Alt,
//     Shift, then the key.
//   - The keyboard shortcuts dialog (shortcutsData) writes keys as
//     key_Shift, key_Cmd, key_K and getModifierKeyForOS swaps Cmd→Ctrl and
//     Option→Alt off macOS (Strg on a German keyboard).
//   - Tooltip key chips (ToolbarButtonTooltip, KeyboardKeysTooltip) list
//     ["⌘" | "Ctrl", "Option" | "Alt", "Shift", mainKey].
//   - Menu item key bindings (Menu.registerKeyboardShortcutForMenuItem,
//     module kE2I) are ids like "l", "mod+c", "mod+shift+s": see ID and
//     EventID.
package shortcut

import (
	"runtime"
	"strings"

	"gioui.org/io/key"
)

// GOOS is the platform shortcuts are formatted and bound for. Tests set
// it to check another platform's labels and bindings.
var GOOS = runtime.GOOS

// Mac reports whether shortcuts use macOS' ⌘ and glyphs.
func Mac() bool { return GOOS == "darwin" }

// Mod is the platform's primary modifier: Command on macOS, Ctrl
// elsewhere (Gio's ModShortcut, but following GOOS).
func Mod() key.Modifiers {
	if Mac() {
		return key.ModCommand
	}
	return key.ModCtrl
}

// combo is a parsed shortcut.
type combo struct {
	mod, ctrl, alt, shift bool
	key                   string // as written ("K", "Enter", "Up", ",")
}

// parse splits s into modifiers and key. ok is false when s isn't a
// combo (a bare key like "L" or a word like "delete" is a combo with no
// modifiers; "Click a file" isn't).
func parse(s string) (c combo, ok bool) {
	s = strings.TrimSpace(s)
	if s == "" {
		return c, false
	}
	var mods []string
	switch {
	case s == "+":
		c.key = "+"
	case strings.HasSuffix(s, "++"):
		c.key = "+"
		mods = strings.Split(s[:len(s)-2], "+")
	default:
		parts := strings.Split(s, "+")
		c.key = strings.TrimSpace(parts[len(parts)-1])
		mods = parts[:len(parts)-1]
	}
	if c.key == "" || strings.ContainsAny(c.key, " \t\n") {
		return c, false
	}
	for _, m := range mods {
		switch strings.ToLower(strings.TrimSpace(m)) {
		case "mod", "cmd", "command", "meta", "cmdorctrl", "⌘":
			c.mod = true
		case "ctrl", "control", "ctl", "⌃":
			c.ctrl = true
		case "alt", "option", "opt", "⌥":
			c.alt = true
		case "shift", "⇧":
			c.shift = true
		default:
			return c, false
		}
	}
	return c, true
}

// keyLabel is how a key shows in hints on goos: letters upper-cased,
// arrows as ↑↓←→, Escape as Esc, Return as Enter.
func keyLabel(k string) string {
	switch strings.ToLower(k) {
	case "up", "arrowup", "↑":
		return "↑"
	case "down", "arrowdown", "↓":
		return "↓"
	case "left", "arrowleft", "←":
		return "←"
	case "right", "arrowright", "→":
		return "→"
	case "esc", "escape":
		return "Esc"
	case "enter", "return":
		return "Enter"
	case "space":
		return "Space"
	case "tab":
		return "Tab"
	}
	if r := []rune(k); len(r) == 1 {
		return strings.ToUpper(k)
	}
	return k
}

// Format is s as Slack labels it on this platform: "⇧⌘K" on macOS,
// "Ctrl+Shift+K" elsewhere. Strings that aren't combos come back as
// they are (Slack's "delete", or a hint a plugin already formatted).
func Format(s string) string { return FormatFor(GOOS, s) }

// FormatAsWritten is Format, except that on macOS the glyphs keep the
// order s writes the modifiers in instead of AppKit's ⌃⌥⇧⌘. A few of
// Slack's literal hints are written that way: the composer's + menu
// labels Text snippet "⌘⇧Enter" (useComposerIAFormatter, boot-render
// zuQH: cX() ? "⌘⇧Enter" : "Ctrl+Shift+Enter"), so its combo is
// "Mod+Shift+Enter". Off macOS it is exactly Format.
func FormatAsWritten(s string) string { return FormatAsWrittenFor(GOOS, s) }

// FormatAsWrittenFor is FormatAsWritten on goos.
func FormatAsWrittenFor(goos, s string) string {
	c, ok := parse(s)
	if !ok || goos != "darwin" {
		return FormatFor(goos, s)
	}
	mods := strings.Split(strings.TrimSpace(s), "+")
	if c.key == "+" {
		mods = strings.Split(strings.TrimSuffix(strings.TrimSpace(s), "++"), "+")
	} else {
		mods = mods[:len(mods)-1]
	}
	var b strings.Builder
	for _, m := range mods {
		switch strings.ToLower(strings.TrimSpace(m)) {
		case "mod", "cmd", "command", "meta", "cmdorctrl", "⌘":
			b.WriteString("⌘")
		case "ctrl", "control", "ctl", "⌃":
			b.WriteString("⌃")
		case "alt", "option", "opt", "⌥":
			b.WriteString("⌥")
		case "shift", "⇧":
			b.WriteString("⇧")
		}
	}
	b.WriteString(keyLabel(c.key))
	return b.String()
}

// FormatFor is Format on goos ("darwin", "windows", "linux").
func FormatFor(goos, s string) string {
	c, ok := parse(s)
	if !ok {
		return s
	}
	k := keyLabel(c.key)
	if goos == "darwin" {
		var b strings.Builder
		if c.ctrl {
			b.WriteString("⌃")
		}
		if c.alt {
			b.WriteString("⌥")
		}
		if c.shift {
			b.WriteString("⇧")
		}
		if c.mod {
			b.WriteString("⌘")
		}
		b.WriteString(k)
		return b.String()
	}
	var parts []string
	if c.mod || c.ctrl {
		parts = append(parts, "Ctrl")
	}
	if c.alt {
		parts = append(parts, "Alt")
	}
	if c.shift {
		parts = append(parts, "Shift")
	}
	return strings.Join(append(parts, k), "+")
}

// Chips are s's key chips for a tooltip (Slack's KeyboardKeysTooltip
// modifiers then mainKey): ⌘, Option, Shift, K on macOS; Ctrl, Alt,
// Shift, K elsewhere. A string that isn't a combo is one chip.
func Chips(s string) []string { return ChipsFor(GOOS, s) }

// ChipsFor is Chips on goos.
func ChipsFor(goos, s string) []string {
	if strings.TrimSpace(s) == "" {
		return nil
	}
	c, ok := parse(s)
	if !ok {
		return []string{s}
	}
	var out []string
	if goos == "darwin" {
		if c.ctrl {
			out = append(out, "⌃")
		}
		if c.mod {
			out = append(out, "⌘")
		}
		if c.alt {
			out = append(out, "Option")
		}
	} else {
		if c.mod || c.ctrl {
			out = append(out, "Ctrl")
		}
		if c.alt {
			out = append(out, "Alt")
		}
	}
	if c.shift {
		out = append(out, "Shift")
	}
	return append(out, keyLabel(c.key))
}

// Parse is s's Gio key name and modifiers on this platform, for binding
// it. Mod is Mod(); Ctrl is the Control key.
func Parse(s string) (key.Name, key.Modifiers) {
	c, ok := parse(s)
	if !ok {
		return key.Name(strings.ToUpper(s)), 0
	}
	var m key.Modifiers
	if c.mod {
		m |= Mod()
	}
	if c.ctrl {
		m |= key.ModCtrl
	}
	if c.alt {
		m |= key.ModAlt
	}
	if c.shift {
		m |= key.ModShift
	}
	return gioName(c.key), m
}

// gioName maps a key as written to Gio's key.Name.
func gioName(k string) key.Name {
	switch strings.ToLower(k) {
	case "up", "arrowup", "↑":
		return key.NameUpArrow
	case "down", "arrowdown", "↓":
		return key.NameDownArrow
	case "left", "arrowleft", "←":
		return key.NameLeftArrow
	case "right", "arrowright", "→":
		return key.NameRightArrow
	case "esc", "escape":
		return key.NameEscape
	case "enter", "return":
		return key.NameReturn
	case "space", " ":
		return key.NameSpace
	case "tab":
		return key.NameTab
	case "backspace":
		return key.NameDeleteBackward
	case "delete", "del":
		return key.NameDeleteForward
	case "home":
		return key.NameHome
	case "end":
		return key.NameEnd
	case "pageup":
		return key.NamePageUp
	case "pagedown":
		return key.NamePageDown
	}
	return key.Name(strings.ToUpper(k))
}

// ---- menu item key ids (Slack's kE2I) ----

// shortcutID is Slack's toShortcutId: mod, alt, then shift (only for a
// letter or a named key: "?" is already shifted), then the key, joined
// with "+".
func shortcutID(k string, mod, alt, shift bool) string {
	named := len([]rune(k)) != 1 || strings.ToLower(k) != strings.ToUpper(k)
	var parts []string
	if mod {
		parts = append(parts, "mod")
	}
	if alt {
		parts = append(parts, "alt")
	}
	if shift && named {
		parts = append(parts, "shift")
	}
	return strings.Join(append(parts, strings.ToLower(k)), "+")
}

// ID normalizes a menu item's shortcut key ("L", "mod+shift+s",
// "Delete") the way Slack's Menu registers it; "" when Slack would skip
// it.
func ID(k string) string {
	if len([]rune(k)) == 1 {
		return strings.ToLower(k)
	}
	parts := strings.Split(strings.ToLower(k), "+")
	n := parts[len(parts)-1]
	mods := parts[:len(parts)-1]
	if n == "" {
		return ""
	}
	var mod, alt, shift bool
	for _, m := range mods {
		switch m {
		case "mod":
			mod = true
		case "alt":
			alt = true
		case "shift":
			shift = true
		default:
			return ""
		}
	}
	return shortcutID(n, mod, alt, shift)
}

// domKey is the KeyboardEvent.key a browser reports for a Gio key.
func domKey(n key.Name) string {
	switch n {
	case key.NameDeleteBackward:
		return "Backspace"
	case key.NameDeleteForward:
		return "Delete"
	case key.NameReturn, key.NameEnter:
		return "Enter"
	case key.NameEscape:
		return "Escape"
	case key.NameSpace:
		return " "
	case key.NameUpArrow:
		return "ArrowUp"
	case key.NameDownArrow:
		return "ArrowDown"
	case key.NameLeftArrow:
		return "ArrowLeft"
	case key.NameRightArrow:
		return "ArrowRight"
	case key.NameTab:
		return "Tab"
	}
	if len([]rune(string(n))) == 1 {
		return strings.ToLower(string(n))
	}
	return string(n)
}

// EventID is the menu shortcut id of a key press, Slack's kE2I `a`:
// Mod is ⌘ on macOS (a press with Control held matches nothing) and
// Ctrl elsewhere (a press with the Windows/Super key matches nothing).
func EventID(n key.Name, m key.Modifiers) string {
	var mod bool
	if Mac() {
		if m.Contain(key.ModCtrl) {
			return ""
		}
		mod = m.Contain(key.ModCommand)
	} else {
		if m.Contain(key.ModSuper) || m.Contain(key.ModCommand) {
			return ""
		}
		mod = m.Contain(key.ModCtrl)
	}
	k := domKey(n)
	if k == "" {
		return ""
	}
	return shortcutID(k, mod, m.Contain(key.ModAlt), m.Contain(key.ModShift))
}

// Filter is the Gio key name and modifiers that produce id (an ID
// result), for listening to it.
func Filter(id string) (key.Name, key.Modifiers, bool) {
	if id == "" {
		return "", 0, false
	}
	var k string
	var m key.Modifiers
	if id == "+" {
		k = "+"
	} else {
		parts := strings.Split(id, "+")
		k = parts[len(parts)-1]
		for _, p := range parts[:len(parts)-1] {
			switch p {
			case "mod":
				m |= Mod()
			case "alt":
				m |= key.ModAlt
			case "shift":
				m |= key.ModShift
			}
		}
	}
	if k == "del" { // Mousetrap's alias; no browser reports it as a key
		k = "delete"
	}
	return gioName(k), m, true
}
