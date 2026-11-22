package ui

import "github.com/some-du6e/clack/internal/ui/views"

// OpenExternalOrg is views.ExternalOrgOpener: the external organization
// page (Slack's ExternalOrganizationView, a secondary view) for teamID,
// beside the Slack Connect hub. The page already open for the team stays.
func (a *App) OpenExternalOrg(teamID string) {
	if teamID == "" {
		return
	}
	cur := a.secondary
	if o, ok := cur.view.(*views.ExternalOrg); ok && cur.open() && o.TeamID() == teamID {
		return
	}
	if cur.kind == secThread && cur.open() {
		a.leaveThreadRoute(cur.channel, cur.ts)
	}
	a.secondary = secondaryState{kind: secDetails, width: cur.width}
	a.secondary.view = views.NewExternalOrg(a, teamID)
	a.Invalidate()
}
