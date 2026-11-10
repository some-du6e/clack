package core

import (
	"context"
	"encoding/json"
	"sync"
	"time"

	"github.com/some-du6e/clack/internal/acrank"
)

// The composer autocomplete's shared state, as Slack's web client keeps
// it (docs/AUTOCOMPLETE-RULES.md). The frecency cache is the "frecency"
// pref (frecency.go: FrecencyCache, RecordFrecency); this file adds:
//
//   - the ranking model from search.autocomplete.model (sPSO's
//     AutocompleteModelCache, refreshed after 28 h); until it lands, or
//     when the workspace has none, the bundled autocompleteFallback.
//   - the navigation history (when each conversation was last open) for
//     HISTORY_AGE_DECAY_*.
type acState struct {
	mu sync.Mutex

	model   acrank.Weights
	modelAt time.Time
	modelIn bool

	history map[string]float64 // conversation id → unix seconds last opened
}

// acModelTTL is AutocompleteCache's timeout (1008e5 ms).
const acModelTTL = 28 * time.Hour

func (a *App) ac() *acState { return &a.acs }

func (a *App) bgCtx() context.Context {
	if a.bg != nil {
		return a.bg
	}
	return context.Background()
}

// AutocompleteModel returns the ranking weights: search.autocomplete.
// model's when it has answered, else the bundled fallback. The first call
// starts the fetch (stale-while-revalidate).
func (a *App) AutocompleteModel() acrank.Weights {
	st := a.ac()
	st.mu.Lock()
	m, fresh, busy := st.model, time.Since(st.modelAt) < acModelTTL, st.modelIn
	if !fresh && !busy && a.C != nil {
		st.modelIn = true
		go a.fetchAutocompleteModel()
	}
	st.mu.Unlock()
	if m == nil {
		return acrank.FallbackWeights
	}
	return m
}

func (a *App) fetchAutocompleteModel() {
	ctx, cancel := context.WithTimeout(a.bgCtx(), 30*time.Second)
	defer cancel()
	var raw json.RawMessage
	err := a.C.Call(ctx, "search.autocomplete.model", nil, &raw)
	st := a.ac()
	st.mu.Lock()
	defer st.mu.Unlock()
	st.modelIn = false
	st.modelAt = time.Now()
	if err != nil {
		st.modelAt = time.Now().Add(-acModelTTL + 10*time.Minute) // retry in 10 min
		return
	}
	if w, ok := acrank.ParseModel(raw); ok {
		st.model = w
	}
}

// noteView records that conversation id was opened (the history store).
func (a *App) noteView(id string) {
	if id == "" {
		return
	}
	st := a.ac()
	st.mu.Lock()
	if st.history == nil {
		st.history = map[string]float64{}
	}
	st.history[id] = float64(time.Now().UnixMilli()) / 1000
	st.mu.Unlock()
}

// ViewHistory returns when each conversation was last opened (unix
// seconds).
func (a *App) ViewHistory() map[string]float64 {
	st := a.ac()
	st.mu.Lock()
	defer st.mu.Unlock()
	out := make(map[string]float64, len(st.history))
	for k, v := range st.history {
		out[k] = v
	}
	return out
}

// ThreadUsers is getThreadUserIds for a cached thread: the reply users,
// with the root's author first when they wrote it themselves (not a bot
// or a system message) and haven't replied. nil when the root isn't
// cached.
func (a *App) ThreadUsers(ch, ts string) []string {
	root, ok := a.S.Find(ch, ts)
	if !ok {
		return nil
	}
	ids := append([]string(nil), root.ReplyUsers...)
	au := root.User
	if au == "" || root.BotID != "" || root.Subtype != "" {
		return ids
	}
	for _, id := range ids {
		if id == au {
			return ids
		}
	}
	return append([]string{au}, ids...)
}

// KnownUsers is every user the store has loaded (Slack's @ list matches
// all members in its store: getAllMembers), people and bots, deactivated
// ones included.
func (a *App) KnownUsers() []User {
	ids := a.S.UserIDs()
	out := make([]User, 0, len(ids))
	for _, id := range ids {
		if u, ok := a.User(id); ok {
			out = append(out, u)
		}
	}
	return out
}
