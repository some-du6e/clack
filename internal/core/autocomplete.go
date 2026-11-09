package core

import (
	"context"
	"encoding/json"
	"sync"
	"time"

	"github.com/some-du6e/yoloslackgoclient/internal/acrank"
)

// The composer autocomplete's shared state, as Slack's web client keeps
// it (docs/AUTOCOMPLETE-RULES.md):
//
//   - the frecency cache is the "frecency" user pref (RI+6/W3UT). A pick
//     in any list records {id, text} into it (recordFrecencyEvent) and
//     the pref is saved with users.prefs.set at most once a minute
//     (setFrecencyByApi through createThrottledThunk(…, 6e4): leading and
//     trailing). Emoji ids are "E"+unicode or "E"+custom name.
//   - the ranking model comes from search.autocomplete.model (sPSO's
//     AutocompleteModelCache, refreshed after 28 h); until it lands, or
//     when the workspace has none, the bundled autocompleteFallback.
//   - the navigation history (when each conversation was last open) feeds
//     HISTORY_AGE_DECAY_*.
type acState struct {
	mu sync.Mutex

	frec     *acrank.Frecency
	frecRaw  string // the pref text frec was parsed from
	frecDirt bool   // local picks not saved yet
	saving   bool
	lastSave time.Time
	timer    *time.Timer

	model   acrank.Weights
	modelAt time.Time
	modelIn bool

	history map[string]float64 // conversation id → unix seconds last opened
}

// acFrecThrottle is setFrecencyByApi's throttle.
var acFrecThrottle = 60 * time.Second

// acModelTTL is AutocompleteCache's timeout (1008e5 ms).
const acModelTTL = 28 * time.Hour

func (a *App) ac() *acState { return &a.acs }

// Frecency returns the frecency cache (read-only: Record makes copies).
func (a *App) Frecency() *acrank.Frecency {
	st := a.ac()
	raw, _ := a.Pref("frecency")
	st.mu.Lock()
	defer st.mu.Unlock()
	if st.frec == nil || (!st.frecDirt && string(raw) != st.frecRaw) {
		st.frec, st.frecRaw = acrank.ParseFrecency(raw), string(raw)
	}
	return st.frec
}

// RecordFrecency is recordFrecencyEvent: id was picked for text (the
// typed token, trigger included; "" for a visit).
func (a *App) RecordFrecency(id, text string) {
	if id == "" {
		return
	}
	f := a.Frecency()
	st := a.ac()
	st.mu.Lock()
	st.frec = f.Record(id, text, time.Now())
	st.frecDirt = true
	st.mu.Unlock()
	a.saveFrecencySoon()
}

// saveFrecencySoon saves now when the last save is a minute old, else
// once the minute is up (lodash throttle, leading and trailing).
func (a *App) saveFrecencySoon() {
	st := a.ac()
	st.mu.Lock()
	defer st.mu.Unlock()
	if st.timer != nil || st.saving {
		return
	}
	wait := acFrecThrottle - time.Since(st.lastSave)
	if wait < 0 {
		wait = 0
	}
	st.timer = time.AfterFunc(wait, a.saveFrecency)
}

func (a *App) saveFrecency() {
	st := a.ac()
	st.mu.Lock()
	st.timer = nil
	if !st.frecDirt || st.frec == nil {
		st.mu.Unlock()
		return
	}
	b, _ := st.frec.MarshalJSON()
	st.frecDirt, st.saving, st.lastSave = false, true, time.Now()
	st.mu.Unlock()
	ctx, cancel := context.WithTimeout(a.bgCtx(), 30*time.Second)
	err := a.C.SetPref(ctx, "frecency", string(b))
	cancel()
	st.mu.Lock()
	st.saving = false
	if err != nil {
		st.frecDirt = true // try again with the next pick
	} else {
		q, _ := json.Marshal(string(b)) // users.prefs.get returns JSON prefs as strings
		st.frecRaw = string(q)
		a.S.Lock()
		if a.S.RawPrefs != nil {
			a.S.RawPrefs["frecency"] = q
		}
		a.S.Unlock()
	}
	again := st.frecDirt && err == nil
	st.mu.Unlock()
	if again {
		a.saveFrecencySoon()
	}
}

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
