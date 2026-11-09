// Package menubar is the macOS menu bar (NSMenu) Clack installs in place of
// Gio's two-item app menu. The model (Menu, Item) is plain Go and builds on
// every OS so it can be tested anywhere; Install builds the AppKit menus on
// macOS with cgo (menubar_darwin.go/.m) and does nothing elsewhere.
//
// Threading: AppKit runs menu actions on the main thread, where waiting on
// Gio's window (Window.Perform, or anything that waits for a frame)
// deadlocks. So every Do runs on a fresh goroutine, and the callbacks that
// AppKit calls synchronously (validation, titles, the recent list) only
// read the last State that SetState stored.
package menubar

import (
	"strings"
	"sync"
	"sync/atomic"

	"github.com/some-du6e/yoloslackgoclient/internal/ui/shortcut"
)

// Mods are the modifier keys of a key equivalent.
type Mods uint8

const (
	Cmd Mods = 1 << iota
	Shift
	Option
	Ctrl
)

// Item is one menu entry.
type Item struct {
	// ID is Slack's menu item id ("menuitem-preferences"), which State
	// refers to. Separators have none.
	ID    string
	Title string
	// Key is the key equivalent as AppKit takes it: one lowercase
	// character ("k", ",", "[", "+", "0"); empty for none.
	Key  string
	Mods Mods
	// Role is a standard AppKit action ("copy:", "hide:", "terminate:")
	// sent down the responder chain, so native text fields and NSApp
	// handle it. Gio's view gets the edit actions (EditRoles) added.
	Role string
	// Forward: Clack's window binds Key itself (keys.go). A key press is
	// handed to the focused Gio view unchanged, exactly as before the menu
	// existed (modals, focused widgets); a click runs Do.
	Forward bool
	// Sep is a separator line.
	Sep bool
	// Sub is a submenu.
	Sub []Item
	// Recent marks where the recent-views list (State.Recent) goes.
	Recent bool
	// Do runs when the item is chosen and has no Role. It runs on its own
	// goroutine, never AppKit's main thread.
	Do func()
}

// Menu is one top-level menu.
type Menu struct {
	Title string
	// Kind tells AppKit what the menu is: "app" (the application menu),
	// "window" (NSApp.windowsMenu: AppKit lists the windows), "help"
	// (NSApp.helpMenu: AppKit adds its search field), or "".
	Kind  string
	Items []Item
}

// EditRoles are the AppKit edit actions Install adds to Gio's view, with
// the key Clack's editors bind for each (its press goes to Gio).
var EditRoles = map[string]struct {
	Key  string
	Mods Mods
}{
	"undo:":             {"z", Cmd},
	"redo:":             {"z", Cmd | Shift},
	"cut:":              {"x", Cmd},
	"copy:":             {"c", Cmd},
	"paste:":            {"v", Cmd},
	"pasteAsPlainText:": {"v", Cmd | Shift},
	"delete:":           {"\x7f", 0}, // forward delete, only over a selection
	"selectAll:":        {"a", Cmd},
}

// Sep is a separator.
var Sep = Item{Sep: true}

// Accel renders Key and Mods as macOS shows them ("⇧⌘K"), for tests and
// docs, through Clack's one shortcut formatter (AppKit's ⌃⌥⇧⌘ order).
func (it Item) Accel() string {
	if it.Key == "" {
		return ""
	}
	var b strings.Builder
	for _, m := range []struct {
		mod  Mods
		name string
	}{{Ctrl, "Ctrl+"}, {Option, "Alt+"}, {Shift, "Shift+"}, {Cmd, "Mod+"}} {
		if it.Mods&m.mod != 0 {
			b.WriteString(m.name)
		}
	}
	k := it.Key
	if k == "\x7f" {
		k = "⌦"
	}
	b.WriteString(k)
	return shortcut.FormatFor("darwin", b.String())
}

// State is what the menus show that changes while Clack runs.
type State struct {
	Disabled map[string]bool   // by Item.ID
	Titles   map[string]string // overrides Item.Title, by Item.ID
	Recent   []Recent          // History's recent views, most recent first
}

// Recent is one entry of History's recent views.
type Recent struct {
	Title string
	Route string // what Install's onRecent gets
}

// Equal reports whether s and o show the same.
func (s *State) Equal(o *State) bool {
	if s == nil || o == nil {
		return s == o
	}
	if len(s.Disabled) != len(o.Disabled) || len(s.Titles) != len(o.Titles) || len(s.Recent) != len(o.Recent) {
		return false
	}
	for k, v := range s.Disabled {
		if o.Disabled[k] != v {
			return false
		}
	}
	for k, v := range s.Titles {
		if ov, ok := o.Titles[k]; !ok || ov != v {
			return false
		}
	}
	for i := range s.Recent {
		if s.Recent[i] != o.Recent[i] {
			return false
		}
	}
	return true
}

var (
	state  atomic.Pointer[State]
	shown  atomic.Pointer[State] // the State the open History menu was built from
	mu     sync.Mutex
	flat   []*Item            // Install's items by tag (index)
	recent func(route string) // chooses a State.Recent entry
)

// SetState updates the menus. Safe from any goroutine; AppKit reads it when
// a menu opens or a key equivalent is matched.
func SetState(s *State) { state.Store(s) }

// Flatten numbers the items depth-first, the tags the native menus use.
func Flatten(menus []Menu) []*Item {
	var out []*Item
	var walk func(items []Item)
	walk = func(items []Item) {
		for i := range items {
			out = append(out, &items[i])
			walk(items[i].Sub)
		}
	}
	for m := range menus {
		walk(menus[m].Items)
	}
	return out
}

// Install replaces the menu bar with menus (macOS; a no-op elsewhere).
// onRecent runs, on its own goroutine, with the Route of the State.Recent
// entry chosen. Safe from any goroutine; the menus appear once AppKit's run loop
// runs.
func Install(menus []Menu, onRecent func(route string)) {
	menus = Clean(menus)
	mu.Lock()
	flat = Flatten(menus)
	recent = onRecent
	mu.Unlock()
	install(menus)
}

// item returns the item with tag, or nil.
func item(tag int) *Item {
	mu.Lock()
	defer mu.Unlock()
	if tag < 0 || tag >= len(flat) {
		return nil
	}
	return flat[tag]
}

// choose runs the item's action: on its own goroutine, so the caller
// (AppKit's main thread) never waits.
func choose(tag int) {
	if it := item(tag); it != nil && it.Do != nil {
		go it.Do()
	}
}

// enabled is AppKit's validation for tag.
func enabled(tag int) bool {
	it := item(tag)
	if it == nil {
		return false
	}
	s := state.Load()
	return s == nil || !s.Disabled[it.ID]
}

// title is tag's current title, "" to keep Item.Title.
func title(tag int) string {
	it := item(tag)
	if it == nil {
		return ""
	}
	if s := state.Load(); s != nil {
		return s.Titles[it.ID]
	}
	return ""
}

// recentShown pins the current recent list for the History menu that is
// opening and returns it; chooseRecent indexes the pinned list.
func recentShown() []Recent {
	s := state.Load()
	shown.Store(s)
	if s == nil {
		return nil
	}
	return s.Recent
}

func chooseRecent(i int) {
	s := shown.Load()
	mu.Lock()
	f := recent
	mu.Unlock()
	if s == nil || i < 0 || i >= len(s.Recent) || f == nil {
		return
	}
	go f(s.Recent[i].Route)
}

// Clean drops the separators that items left out of a menu would leave
// behind: leading, trailing and repeated ones, in every submenu.
func Clean(menus []Menu) []Menu {
	out := make([]Menu, 0, len(menus))
	for _, m := range menus {
		m.Items = cleanItems(m.Items)
		if len(m.Items) > 0 {
			out = append(out, m)
		}
	}
	return out
}

func cleanItems(items []Item) []Item {
	var out []Item
	for _, it := range items {
		if it.Sub != nil {
			it.Sub = cleanItems(it.Sub)
			if len(it.Sub) == 0 {
				continue
			}
		}
		if it.Sep && !it.Recent && (len(out) == 0 || (out[len(out)-1].Sep && !out[len(out)-1].Recent)) {
			continue
		}
		out = append(out, it)
	}
	for len(out) > 0 && out[len(out)-1].Sep && !out[len(out)-1].Recent {
		out = out[:len(out)-1]
	}
	return out
}
