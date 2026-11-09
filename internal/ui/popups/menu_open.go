package popups

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
