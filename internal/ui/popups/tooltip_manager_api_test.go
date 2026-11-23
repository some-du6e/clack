package popups

// The tooltip tests' reads of the TooltipManager and the header's tip
// (tooltip_manager_test.go).

func hideTipsT() { HideTooltips() }

func notifyButtonTipT(h Host, ch string, dm, muted bool) []TipLine {
	return NotifyButtonTip(h, ch, dm, muted)
}
