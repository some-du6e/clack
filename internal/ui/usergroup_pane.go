package ui

import "github.com/some-du6e/clack/internal/ui/popups"

// OpenUserGroup shows Slack's user group profile (popups.UserGroupView)
// in the secondary pane: what a @group mention's UserGroupElement opens
// (toUserGroup, the UserGroup flexpane). It takes the profile pane's slot.
func (a *App) OpenUserGroup(id string) {
	if id == "" {
		return
	}
	a.secondary = secondaryState{kind: secProfile, width: a.secondary.width}
	a.secondary.view = popups.NewUserGroupView(a, id)
	a.Invalidate()
}
