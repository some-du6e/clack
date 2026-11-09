package core

import (
	"context"
	"encoding/json"
	"strconv"
	"sync"
)

// Team prefs: the workspace settings Slack's client keeps per team
// (state.teamPrefs, read with getTeamPref: h/Bp xp(state, name, teamId)).
// Slack's web client (build a84662d2d135, 2026-11-08) fills them from the
// boot response (setGantryBootTeamPrefs: client.boot's team.prefs and
// each workspaces[].prefs; client.extras' toplevel_prefs and
// workspace_prefs top them up), reloads them with team.prefs.get
// (fetchAndUpsertTeamPrefs) and applies the RTM event team_pref_change
// {name, value, team_id} (teamPrefChange; an event for another team is
// rejected unless the client is on Grid). Clack doesn't call client.boot,
// so boot asks team.prefs.get, the method Slack itself uses to reload
// the store.
//
// The broadcast checks read two of them: loud_channel_mentions_limit
// (shouldStopChannelMentions) and warn_before_at_channel
// (shouldWarnBeforeBroadcast); see broadcast.go.

type teamPrefStore struct {
	mu     sync.RWMutex
	m      map[string]json.RawMessage
	loaded bool
}

var teamPrefIndex sync.Map // *App → *teamPrefStore

func (a *App) teamPrefStore() *teamPrefStore {
	if v, ok := teamPrefIndex.Load(a); ok {
		return v.(*teamPrefStore)
	}
	v, _ := teamPrefIndex.LoadOrStore(a, &teamPrefStore{m: map[string]json.RawMessage{}})
	return v.(*teamPrefStore)
}

// LoadTeamPrefs reloads the team's prefs with team.prefs.get (Slack's
// fetchAndUpsertTeamPrefs) and replaces the local copy.
func (a *App) LoadTeamPrefs(ctx context.Context) error {
	var r struct {
		Prefs map[string]json.RawMessage `json:"prefs"`
	}
	if err := a.C.Call(ctx, "team.prefs.get", nil, &r); err != nil {
		return err
	}
	a.SetTeamPrefs(r.Prefs)
	return nil
}

// SetTeamPrefs installs a team.prefs.get-shaped map (upsertTeamPrefs with
// replace): what boot or a test fixture loaded.
func (a *App) SetTeamPrefs(prefs map[string]json.RawMessage) {
	st := a.teamPrefStore()
	m := make(map[string]json.RawMessage, len(prefs))
	for k, v := range prefs {
		m[k] = v
	}
	st.mu.Lock()
	st.m, st.loaded = m, true
	st.mu.Unlock()
}

// TeamPrefsLoaded reports whether team prefs have been loaded.
func (a *App) TeamPrefsLoaded() bool {
	st := a.teamPrefStore()
	st.mu.RLock()
	defer st.mu.RUnlock()
	return st.loaded
}

// TeamPref is one team pref as Slack sent it (ok: present).
func (a *App) TeamPref(name string) (json.RawMessage, bool) {
	st := a.teamPrefStore()
	st.mu.RLock()
	defer st.mu.RUnlock()
	v, ok := st.m[name]
	return v, ok && string(v) != "null"
}

// TeamPrefString is a string team pref ("" when absent or not a string).
func (a *App) TeamPrefString(name string) string {
	v, _ := a.TeamPref(name)
	var s string
	if json.Unmarshal(v, &s) == nil {
		return s
	}
	return ""
}

// TeamPrefInt is a numeric team pref (numbers or numeric strings); ok is
// false when it is absent or not a number.
func (a *App) TeamPrefInt(name string) (int, bool) {
	v, present := a.TeamPref(name)
	if !present {
		return 0, false
	}
	var n float64
	if json.Unmarshal(v, &n) == nil {
		return int(n), true
	}
	var s string
	if json.Unmarshal(v, &s) == nil {
		if i, err := strconv.Atoi(s); err == nil {
			return i, true
		}
	}
	return 0, false
}

// onTeamPrefChange is Slack's teamPrefChange: set one pref from the RTM
// event. Off Grid, an event naming another team is dropped.
func (a *App) onTeamPrefChange(raw json.RawMessage) {
	var e struct {
		Name   string          `json:"name"`
		Value  json.RawMessage `json:"value"`
		TeamID string          `json:"team_id"`
	}
	if json.Unmarshal(raw, &e) != nil || e.Name == "" {
		return
	}
	if cur := a.C.Creds.Team; a.C.Creds.EnterpriseID == "" && e.TeamID != "" && cur != "" && e.TeamID != cur {
		return
	}
	st := a.teamPrefStore()
	st.mu.Lock()
	if len(e.Value) == 0 {
		e.Value = json.RawMessage("null")
	}
	st.m[e.Name] = e.Value
	st.mu.Unlock()
	a.emit(Event{Type: "team_prefs"})
}
