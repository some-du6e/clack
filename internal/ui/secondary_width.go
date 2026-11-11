package ui

// nestedSecondary is Slack's isNestedSecondaryViewEnabled (client-boot
// kl02 W7): !isBBMEnabled && !isAgenticIaEnabled && !tbd client &&
// nested_secondary_view. Under it a channel's thread and profile panes
// nest below the channel header (layoutPrimary), the column may grow to
// fill the region and secondary_view_default_width counts (secondary.go),
// and view headers follow it (views.NestedSecondary).
func (a *App) nestedSecondary() bool {
	c := a.Core()
	return c != nil && !a.bbmOn() && c.NestedSecondaryView()
}

// NestedSecondaryView is views.NestedSecondary.
func (a *App) NestedSecondaryView() bool { return a.nestedSecondary() }
