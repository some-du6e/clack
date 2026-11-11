package taut

import (
	"context"
	"encoding/json"
	"time"

	"github.com/some-du6e/clack/internal/slack"
)

// Slack's appActions store (client-boot NZY5): the workspace's message
// shortcuts. loadClientAppCommands fills it from apps.actions.v2.list:
// every app's message_action actions become {appId, id: action_id, type:
// "message_action", name, description, teams: the app's teams}, keyed by
// id, with actionIdsByAppId beside them (global actions go to
// appGlobalActions). OwlMode finds owl's "Reply"/"React" shortcuts here
// (state.appActions.actions) and runs them through executeAppAction.
// Clack loads the list (core's Interactive.Shortcuts, the same call) the
// first time a plugin reads the slice, and again after
// app_actions_updated.

// emptyAppActions is the store before the list loads.
func emptyAppActions() map[string]any {
	return map[string]any{"actions": map[string]any{}, "actionIdsByAppId": map[string]any{}, "actionIdsByInstalledTeamId": map[string]any{},
		"isLoaded": false, "fetchFailed": false, "runCount": 0}
}

func (m *mirror) appActionsSlice() any {
	m.mu.Lock()
	acts, loading := m.actions, m.actLoading
	if acts == nil && !loading {
		m.actLoading = true
	}
	m.mu.Unlock()
	if acts == nil && !loading {
		go m.loadAppActions()
	}
	if acts == nil {
		return emptyAppActions()
	}
	return acts
}

func (m *mirror) loadAppActions() {
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	list, err := m.t.app.Interactive().Shortcuts(ctx)
	actions := map[string]any{}
	byApp := map[string][]string{}
	for _, s := range list {
		if s.Type != "message_action" || s.ActionID == "" {
			continue
		}
		a := map[string]any{"appId": s.AppID, "id": s.ActionID, "type": "message_action", "name": s.Name, "description": s.Desc}
		if s.Teams != nil {
			a["teams"] = s.Teams
		}
		if _, dup := actions[s.ActionID]; !dup {
			byApp[s.AppID] = append(byApp[s.AppID], s.ActionID)
		}
		actions[s.ActionID] = a
	}
	if err != nil { // the next read (after an invalidation) tries again
		m.mu.Lock()
		m.actLoading = false
		m.mu.Unlock()
		return
	}
	st := emptyAppActions()
	st["actions"], st["actionIdsByAppId"], st["isLoaded"] = actions, byApp, true
	b, _ := json.Marshal(st)
	m.mu.Lock()
	m.actions = b
	m.actLoading = false
	m.mu.Unlock()
	m.invalidate("appActions")
}

// noteRaw follows the realtime frames the mirror's slices depend on.
func (m *mirror) noteRaw(ev slack.Event) {
	if ev.Type != "app_actions_updated" {
		return
	}
	m.mu.Lock()
	had := m.actions != nil
	if !m.actLoading {
		m.actions = nil // the next read loads the new list
	}
	m.mu.Unlock()
	if had {
		m.invalidate("appActions")
	}
}
