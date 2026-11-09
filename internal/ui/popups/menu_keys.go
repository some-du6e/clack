package popups

import (
	"sort"
	"strings"
	"time"

	"gioui.org/io/event"
	"gioui.org/io/key"
	"gioui.org/layout"

	"github.com/some-du6e/yoloslackgoclient/internal/ui/shortcut"
	"github.com/some-du6e/yoloslackgoclient/internal/ui/slackc"
)

// Menu item keys, read from Slack's web client (build a84662d2d135,
// 2026-11-08).
//
// A menu item's shortcut is either a plain label string (shown only) or
// {label, key}. Slack's Menu registers every enabled item whose shortcut
// has a key (registerKeyboardShortcutForMenuItem) into a map from the
// key's id (kE2I: "l", "mod+c", "mod+shift+s") to the item, later items
// winning. While the menu has focus, a key press whose id is in the map
// clicks that item and closes the menu (onMenuKeyDown); arrows, Home,
// End, Enter, Space and Escape navigate as before.
//
// The message action keys are module EtVr's P2: EDIT e, REACT r,
// COMMENT t, PIN p, SHARE s f, STAR a, SAVE a, SAVE_WITH_TODOS
// mod+shift+s, ADD_TO_LATER_TODOS a, MARK_UNREAD u, MARK_THREAD_UNREAD u,
// REMIND m, DELETE (del, Delete; plus backspace, Backspace on macOS),
// COPY_LINK l, COPY_TEXT mod+c, ADD_TO_LIST v. The activity item menu's
// Add to-do / Remove to-do uses {label "A", key "a"}. File, channel and
// sidebar menus have display-only hints ("⌘ Click"), no keys.
//
// Type-ahead (onCharacterKeyPressed) only runs behind the
// sk_menu_typeahead session toggle, which is off unless set in
// localStorage; MenuTypeAhead mirrors it.
var (
	KeyEdit       = []string{"e"}
	KeyReact      = []string{"r"}
	KeyComment    = []string{"t"}
	KeyPin        = []string{"p"}
	KeyShare      = []string{"s", "f"}
	KeySave       = []string{"a"}
	KeySaveTodos  = []string{"mod+shift+s"}
	KeyLaterTodos = []string{"a"}
	KeyMarkUnread = []string{"u"}
	KeyRemind     = []string{"m"}
	KeyCopyLink   = []string{"l"}
	KeyCopyText   = []string{"mod+c"}
	KeyAddToList  = []string{"v"}
)

// KeyDelete is P2.DELETE on this platform: Delete everywhere, and
// Backspace too on macOS (whose delete key sends it).
func KeyDelete() []string {
	if shortcut.Mac() {
		return []string{"del", "backspace", "Delete", "Backspace"}
	}
	return []string{"del", "Delete"}
}

// MenuTypeAhead is Slack's sk_menu_typeahead toggle: letters move the
// highlight to the item they start (and, off macOS, run a unique match)
// instead of running item shortcuts without modifiers.
var MenuTypeAhead = false

// menuNavKeys are Menu's navigation keys (Slack's k map).
var menuNavKeys = []key.Name{
	key.NameUpArrow, key.NameDownArrow, key.NameLeftArrow, key.NameRightArrow,
	key.NameReturn, key.NameEnter, key.NameSpace, key.NameEscape, key.NameHome, key.NameEnd,
}

// shortcutMap is Slack's keyboardShortcutsMap for m's rows: key id →
// row index, later rows winning.
func (m *Menu) shortcutMap() map[string]int {
	var sm map[string]int
	for i := range m.items {
		it := &m.items[i]
		if len(it.Keys) == 0 || !m.selectable(i) {
			continue
		}
		for _, k := range it.Keys {
			if id := shortcut.ID(k); id != "" {
				if sm == nil {
					sm = map[string]int{}
				}
				sm[id] = i
			}
		}
	}
	return sm
}

// keyFilters are the keys the root menu listens for: navigation, every
// open level's item keys and, with type-ahead, letters and digits.
func (m *Menu) keyFilters() []event.Filter {
	fs := make([]event.Filter, 0, len(menuNavKeys)+8)
	for _, n := range menuNavKeys {
		fs = append(fs, key.Filter{Focus: m, Name: n})
	}
	type nm struct {
		n key.Name
		m key.Modifiers
	}
	seen := map[nm]bool{}
	for s := m; s != nil; s = s.sub {
		for id := range s.shortcutMap() {
			n, mods, ok := shortcut.Filter(id)
			if !ok || seen[nm{n, mods}] {
				continue
			}
			seen[nm{n, mods}] = true
			fs = append(fs, key.Filter{Focus: m, Name: n, Required: mods})
		}
	}
	if MenuTypeAhead {
		for _, r := range "ABCDEFGHIJKLMNOPQRSTUVWXYZ0123456789" {
			n := key.Name(string(r))
			if !seen[nm{n, 0}] {
				fs = append(fs, key.Filter{Focus: m, Name: n, Optional: key.ModShift})
			}
		}
	}
	return fs
}

// shortcutMatch is Slack's getShortcutMatch for a press in menu a: the
// row its id maps to, or -1. With type-ahead a key without modifiers is
// left to type-ahead.
func (a *Menu) shortcutMatch(ke key.Event) int {
	id := shortcut.EventID(ke.Name, ke.Modifiers)
	if id == "" {
		return -1
	}
	if MenuTypeAhead && !strings.Contains(id, "+") {
		return -1
	}
	if i, ok := a.shortcutMap()[id]; ok {
		return i
	}
	return -1
}

// runShortcut runs row i as a shortcut press: Slack clicks the item and
// closes the menu; a submenu row opens its submenu with the keyboard.
func (a *Menu) runShortcut(gtx layout.Context, i int) {
	if len(a.items[i].Sub) > 0 {
		a.sel = i
		a.openSub(i, true)
		return
	}
	a.activate(gtx, i)
	if r := a.root(); !r.done {
		r.closeAll()
	}
}

// typeAhead is Slack's onCharacterKeyPressed for a press in menu a.
func (a *Menu) typeAhead(gtx layout.Context, ke key.Event) {
	if ke.Modifiers&^key.ModShift != 0 || len([]rune(string(ke.Name))) != 1 || ke.Name == key.NameSpace {
		return
	}
	ch := strings.ToLower(string(ke.Name))
	type label struct {
		i    int
		text string
	}
	var ls []label
	for i := range a.items {
		if t := strings.ToLower(strings.TrimSpace(a.items[i].Label)); t != "" && a.selectable(i) {
			ls = append(ls, label{i, t})
		}
	}
	if len(ls) == 0 {
		return
	}
	if !shortcut.Mac() {
		// findNextMatchAfterCurrent: a unique match runs; else the
		// highlight cycles through the matches.
		var ms []label
		for _, l := range ls {
			if strings.HasPrefix(l.text, ch) {
				ms = append(ms, l)
			}
		}
		switch len(ms) {
		case 0:
			return
		case 1:
			a.typeBuf = ""
			a.runShortcut(gtx, ms[0].i)
			return
		}
		next := 0
		for k, l := range ms {
			if l.i == a.sel {
				next = (k + 1) % len(ms)
			}
		}
		a.sel, a.kbd = ms[next].i, true
		return
	}
	// macOS: a 1s buffer; the first label it starts, else the first one
	// sorting at or after it, else the last.
	if gtx.Now.Sub(a.typeAt) > time.Second {
		a.typeBuf = ""
	}
	a.typeBuf += ch
	a.typeAt = gtx.Now
	for _, l := range ls {
		if strings.HasPrefix(l.text, a.typeBuf) {
			a.sel, a.kbd = l.i, true
			return
		}
	}
	sorted := append([]label(nil), ls...)
	sort.SliceStable(sorted, func(x, y int) bool { return sorted[x].text < sorted[y].text })
	pick := sorted[len(sorted)-1]
	for _, l := range sorted {
		if l.text >= a.typeBuf {
			pick = l
			break
		}
	}
	a.sel, a.kbd = pick.i, true
}

// templateShortcut is a MenuTemplateItem's shortcut: a label string, or
// {label, key} with key a string or a list of them.
func templateShortcut(v any) (hint string, keys []string) {
	switch s := v.(type) {
	case string:
		return s, nil
	case nil:
		return "", nil
	}
	p := asProps(v)
	if p == nil {
		return "", nil
	}
	hint, _ = p["label"].(string)
	switch k := p["key"].(type) {
	case string:
		keys = []string{k}
	default:
		for _, x := range p.List("key") {
			if s, ok := x.(string); ok {
				keys = append(keys, s)
			}
		}
	}
	return hint, keys
}

// TemplateShortcut is templateShortcut for slackc/kit's MenuTemplateItem
// rows.
func TemplateShortcut(v any) (hint string, keys []string) { return templateShortcut(v) }

// shortcutProp is it's MenuTemplateItem shortcut ("" none): the hint, or
// {label, key} when the row has keys.
func shortcutProp(it *MenuItem) any {
	if it.Hint == "" {
		return nil
	}
	if len(it.Keys) == 0 {
		return it.Hint
	}
	ks := make([]any, len(it.Keys))
	for i, k := range it.Keys {
		ks[i] = k
	}
	return slackc.Props{"label": it.Hint, "key": ks}
}
