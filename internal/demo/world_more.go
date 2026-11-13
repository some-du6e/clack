package demo

import (
	"encoding/json"
	"net/url"
	"slices"
)

// The rest of the Web API Clack asks a workspace for, answered the way
// Quillfeather's Slack would: channel managers, permissions, the viewer's
// user groups, Slack Connect orgs, VIP suggestions and the account
// checks behind Preferences. Shapes follow the real methods (fakeslack's
// handlers, core's decoders).

func (w *World) more(method string, f url.Values) (any, bool) {
	switch method {
	case "admin.roles.entity.listAssignments": // "Managed by": the channel's creator
		w.mu.Lock()
		c, ok := w.convLocked(f.Get("entity_id"))
		var users []string
		if ok && c.Creator != "" && (c.IsChannel || c.IsPrivate) && !c.IsMPIM {
			users = []string{c.Creator}
		}
		w.mu.Unlock()
		as := []any{}
		if len(users) > 0 {
			as = append(as, map[string]any{"role_id": "Rl0A", "entity_id": f.Get("entity_id"), "users": users})
		}
		return okMap("role_assignments", as), true
	case "megaphone.notifications.list": // no announcements in the demo
		return okMap("notifications", map[string]any{}), true
	case "client.extras":
		return okMap("misc", map[string]any{"is_content_reporting_enabled": true}), true
	case "usergroups.membership":
		var mine []string
		for _, g := range w.ws.Usergroups {
			if slices.Contains(g.Members, SelfID) {
				mine = append(mine, g.Group.ID)
			}
		}
		return okMap("usergroups", mine), true
	case "users.slackConnect.orgList":
		t := w.partnerTeamInfo()
		return okMap("orgs", []any{map[string]any{"team_summary": map[string]any{"id": PartnerTeamID, "name": PartnerTeamName,
			"icon": t["icon"], "domain": PartnerTeamDomain, "is_verified": true}, "connected_channels_count": 1, "member_count": 2}},
			"response_metadata", map[string]any{"next_cursor": ""}), true
	case "search.modules.workObjects": // no connected apps with work objects
		return okMap("items", []any{}, "pagination", map[string]any{"total_count": 0, "page": 1, "page_count": 0}), true
	case "apps.actions.listForResource":
		return okMap("actions", []any{}), true
	case "functions.workflows.findWorkflowForBotUsers":
		return okMap("workflow_apps", []any{}), true
	case "permissions.info": // a member's rights: everything a full member may do in a channel
		var perms []string
		_ = json.Unmarshal([]byte(f.Get("permissions")), &perms)
		if perms == nil {
			perms = []string{}
		}
		return okMap("allowed", perms), true
	case "conversations.permissions.accountTypes.list":
		return okMap("account_types", []any{}), true
	case "users.emailVerificationStatus.get":
		return okMap("is_verified", true), true
	case "apps.auth.external.userAuths.get":
		return okMap("apps", map[string]any{}), true
	case "users.priority.getRecommendations": // the VIP page's suggestions
		rec := func(h, reason string) map[string]any {
			p, _ := w.person(h)
			return map[string]any{"user_id": p.ID, "source": "org_chart", "reason": reason}
		}
		return okMap("users", []any{rec("gordon", "manager"), rec("priya", "frequent_collaborator"), rec("marisol", "frequently_used")}), true
	}
	return nil, false
}
