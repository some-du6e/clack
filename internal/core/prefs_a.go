package core

import (
	"context"
	"encoding/json"
	"net/url"
	"strings"

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

// EmailToken is one "Bring emails into Slack" forwarding address.
type EmailToken struct {
	ID      json.Number `json:"token_id"`
	Token   string      `json:"token"`
	Address string      `json:"email_address"`
}

// EmailTokens is users.emailtoken.get (EmailForwardingPref on mount):
// none is no_emailtoken.
func (a *App) EmailTokens(ctx context.Context) ([]EmailToken, error) {
	var r struct {
		Addresses []EmailToken `json:"email_forwarding_addresses"`
	}
	// no_emailtoken is an answer, not a failure: each open asks again
	// (the guard would otherwise replay it for a minute after a create)
	err := a.C.Call(slack.NoBackoff(ctx), "users.emailtoken.get", url.Values{"reason": {"prefs"}}, &r)
	if err != nil && strings.Contains(err.Error(), "no_emailtoken") {
		return nil, nil
	}
	return r.Addresses, err
}

// CreateEmailToken is "Get a Forwarding Address" (users.emailtoken.create).
func (a *App) CreateEmailToken(ctx context.Context) (EmailToken, error) {
	var t EmailToken
	err := a.C.Call(ctx, "users.emailtoken.create", url.Values{"reason": {"prefs"}}, &t)
	return t, err
}

// DeleteEmailToken is "Disable this address" (users.emailtoken.delete).
func (a *App) DeleteEmailToken(ctx context.Context, id string) error {
	return a.C.Call(ctx, "users.emailtoken.delete", url.Values{"email_token_id": {id}, "reason": {"prefs"}}, nil)
}
