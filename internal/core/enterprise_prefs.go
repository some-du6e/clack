package core

import (
	"context"
	"encoding/json"
	"sync"
)

// Enterprise prefs: Slack's boot runs enterprise.prefs.get (the boot API
// HkM8 PB) and dispatchEnterpriseData merges the answer into the
// enterprisePrefs store, {locked_prefs, org_prefs, unlocked_prefs}
// (client-boot 8wTz, n3H2 setEnterprisePrefs). getOrgPref (8wTz wc) and
// getLockedOrgPref (SI) read it. Clack asks once boot is done (Grid
// only); until it answers every pref reads as unset, as in Slack before
// the boot API lands.

type enterprisePrefs struct {
	mu       sync.Mutex
	org      map[string]json.RawMessage
	locked   map[string]json.RawMessage
	unlocked map[string]json.RawMessage
	live     bool // enterprise.prefs.get answered this session
}

var entPrefsIndex = newPerApp() // *App → *enterprisePrefs

func (a *App) entPrefs() *enterprisePrefs {
	v, _ := entPrefsIndex.LoadOrStore(a, &enterprisePrefs{})
	return v.(*enterprisePrefs)
}

// LoadEnterprisePrefs is the enterprise.prefs.get boot API.
func (a *App) LoadEnterprisePrefs(ctx context.Context) error {
	var raw map[string]json.RawMessage
	if err := a.C.Call(ctx, "enterprise.prefs.get", nil, &raw); err != nil {
		return err
	}
	a.setEnterprisePrefs(raw)
	ep := a.entPrefs()
	ep.mu.Lock()
	ep.live = true
	ep.mu.Unlock()
	if b, err := json.Marshal(raw); err == nil {
		a.S.SetEnterprisePrefs(b) // the disk cache (installCachedEnterprisePrefs)
	}
	a.emit(Event{Type: "enterprise_prefs"})
	return nil
}

// installCachedEnterprisePrefs reads the answer the last session's
// enterprise.prefs.get left in the disk cache, unless this session's has
// answered: Slack's enterprisePrefs store is persisted (TI4y,
// persistenceEnabled), so a Grid org's prefs (Message history's unlocked
// allow_retention_override, channel_retention.go) hold from the first
// frame. A first start reads them unset until the call answers, as
// Slack's does.
func (a *App) installCachedEnterprisePrefs() {
	b := a.S.CachedEnterprisePrefs()
	if len(b) == 0 {
		return
	}
	var raw map[string]json.RawMessage
	if json.Unmarshal(b, &raw) != nil {
		return
	}
	ep := a.entPrefs()
	ep.mu.Lock()
	live := ep.live
	ep.mu.Unlock()
	if !live {
		a.setEnterprisePrefs(raw)
	}
}

// setEnterprisePrefs is updateEnterprisePrefs over the call's answer: the
// three maps, at the top level or under "prefs".
func (a *App) setEnterprisePrefs(raw map[string]json.RawMessage) {
	if p, ok := raw["prefs"]; ok {
		var inner map[string]json.RawMessage
		if json.Unmarshal(p, &inner) == nil {
			raw = inner
		}
	}
	var org, locked, unlocked map[string]json.RawMessage
	_ = json.Unmarshal(raw["org_prefs"], &org)
	_ = json.Unmarshal(raw["locked_prefs"], &locked)
	_ = json.Unmarshal(raw["unlocked_prefs"], &unlocked)
	ep := a.entPrefs()
	ep.mu.Lock()
	ep.org, ep.locked, ep.unlocked = org, locked, unlocked
	ep.mu.Unlock()
}

// EnterpriseOrgPref is getOrgPref: the org pref's raw value, nil when the
// org hasn't set it (or enterprise.prefs.get hasn't answered).
func (a *App) EnterpriseOrgPref(name string) json.RawMessage {
	ep := a.entPrefs()
	ep.mu.Lock()
	defer ep.mu.Unlock()
	return ep.org[name]
}

// EnterpriseLockedPref is getLockedOrgPref.
func (a *App) EnterpriseLockedPref(name string) json.RawMessage {
	ep := a.entPrefs()
	ep.mu.Lock()
	defer ep.mu.Unlock()
	return ep.locked[name]
}

// EnterpriseUnlockedPref is getUnlockedOrgPref (client-boot TI4y D1):
// enterprisePrefs.unlocked_prefs[name], nil when unset.
func (a *App) EnterpriseUnlockedPref(name string) json.RawMessage {
	ep := a.entPrefs()
	ep.mu.Lock()
	defer ep.mu.Unlock()
	return ep.unlocked[name]
}

// EnterpriseOrgPrefTrue is `!0 === getOrgPref(state, name)`.
func (a *App) EnterpriseOrgPrefTrue(name string) bool {
	var b bool
	return json.Unmarshal(a.EnterpriseOrgPref(name), &b) == nil && b
}
