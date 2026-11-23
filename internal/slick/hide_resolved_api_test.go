package slick

// HideResolved's hooks into Taut on the red tree (hide_resolved_test.go,
// over the code before HideResolved's port): Taut tells of nothing hidden,
// and a navigation's highlight goes nowhere.

func historyHidden(h *harness, ch string) (has func(string) bool, n int, ok bool) {
	return func(string) bool { return false }, 0, true
}

func setViewHighlight(h *harness, ch, ts string) {}
