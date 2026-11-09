package core

import (
	"context"
	"net/url"

	"github.com/some-du6e/yoloslackgoclient/internal/slack"
)

// connectSectionOn is useCalculateSidebarSections' showConnectSection:
// isSlackConnectSectionVisible (yl3L: !!should_show_connect_section) and
// the viewer has a slack_connect section.
func (a *App) connectSectionOn(ordered []slack.Section) bool {
	return a.PrefBool("should_show_connect_section", false) && hasSectionType(ordered, "slack_connect")
}

// extShared is 5DYq ju: the conversation is shared with another
// organization (is_ext_shared).
func (a *App) extShared(id string) bool {
	c, ok := a.S.Conv(id)
	return ok && c.IsExtShared
}

// VIPRecommendation is one of users.priority.getRecommendations' users.
type VIPRecommendation struct {
	User   string `json:"user_id"`
	Source string `json:"source"`
	Reason string `json:"reason"` // manager, skip_level, frequent_collaborator, frequently_used
}

// VIPRecommendations is fetchAndUpsertRecommendedPriorityUsers (the VIP
// page asks on open: limit 99, reason vip-list-preferences-modal).
func (a *App) VIPRecommendations(ctx context.Context) ([]VIPRecommendation, error) {
	var r struct {
		Users []VIPRecommendation `json:"users"`
	}
	err := a.C.Call(ctx, "users.priority.getRecommendations", url.Values{"limit": {"99"}, "reason": {"vip-list-preferences-modal"}}, &r)
	return r.Users, err
}
