package popups

import "image"

// OpenSubmenu highlights the row labelled label (in the deepest open
// submenu) and opens its submenu, as the keyboard does; false when there is
// no such row with a submenu. Tests and screenshots use it.
func (m *Menu) OpenSubmenu(label string) bool {
	a := m.active()
	for i, it := range a.items {
		if it.Label == label && len(it.Sub) > 0 && a.selectable(i) {
			a.sel, a.kbd = i, true
			a.openSub(i, true)
			return true
		}
	}
	return false
}

// RowRect is the last drawn box of the root menu's row labelled label, in
// window coordinates (empty when there is none). Tests use it.
func (m *Menu) RowRect(label string) image.Rectangle {
	for i, it := range m.items {
		if it.Label == label && i < len(m.rows) {
			return m.rows[i]
		}
	}
	return image.Rectangle{}
}

// Highlighted is the root menu's highlighted row's label ("" none).
func (m *Menu) Highlighted() string {
	if m.sel < 0 || m.sel >= len(m.items) {
		return ""
	}
	return m.items[m.sel].Label
}
