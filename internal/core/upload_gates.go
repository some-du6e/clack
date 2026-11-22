package core

// The composer's upload gates, as Slack's web client has them (build
// 2c0d02ca130f, read from the public CDN without a cookie):
//
//   - ShortcutsMenuTriggerButton (boot-render VxbD), the composer's +,
//     reads two selectors. Its double-click (onDoubleClick: close the
//     menu, then E({action: DOUBLE_CLICK})) clicks the hidden file input
//     "Upload from your computer" clicks, unless either answers yes. The
//     first, N, also drops the button tooltip's "Tip: double click to
//     upload" and the overlay trick that lets the second click reach the
//     button (internal/ui/composer).
//   - N is rscm O: the conversation is externally shared (5DYq ju:
//     is_ext_shared) and its conversationsListPrefs entry has
//     can_upload_files off. A missing entry or pref reads as allowed
//     (`?? true`). The + menu's formatter (useComposerIAFormatter) passes
//     it as slackConnectFileSharingBlocked: the Upload from your computer
//     row is disabled (and buildOriginalItems' Recent file row).
//   - M is uu7O f, the formatter's isRestrictedExternalHuddle:
//     external_huddles on (pSxm a_) and either the viewer is an external
//     guest (cB5s U: wY11 EG on the current member, is_restricted with
//     restricted_user_type external_guest) or the conversation is a huddle
//     channel (aK5v cb: properties.huddles.is_huddle_channel). The menu
//     then has no file rows, GIF, Salesforce record or Upload from your
//     computer.
//
// conversationsListPrefs (client-boot j1ML, persisted) holds each
// conversation's list prefs: fetchAndUpsertConversationsListPrefs (4YuU)
// stores conversations.listPrefs {channel, reason "client_redux_store"}'s
// prefs under the channel. The selector N reads (iY2W Q,
// getConversationsListPref(state, name, id)) runs only once ju passed,
// and only with a team id and an id that looks like a channel's (jh4t
// /^(C|G|D|Z)[A-Z0-9]{8,}$/); it asks the fetch manager's maybeRefetch
// for the id, then reads prefs[id][name] (null before the answer). The
// manager is a LoZG ConditionalFetchManager with no maxAge: an id stays
// fresh after its fetch until something marks it stale, and a failed
// fetch counts too (the fetch's catch swallows the error, the finally
// marks the ids fresh). Fetches start 200 ms after the previous one at
// the earliest. What marks ids stale (boot-render VxbD): channel_deleted
// (that channel), channel_updated with unlinked or connected_team_ids in
// its updates (that channel), slack_connect_pref_changed for
// allow_sc_file_uploads or external_awareness_context_bar (every
// externally shared channel), and a slow reconnect (every id,
// _maybeResetState).

import (
	"context"
	"encoding/json"
	"log"
	"net/url"
	"regexp"
	"sync"
	"time"
)

// ExpExternalHuddles is external_huddles (pSxm a_,
// isExternalHuddlesEnabled).
const ExpExternalHuddles = "external_huddles"

type listPrefsState struct {
	mu    sync.Mutex
	prefs map[string]map[string]json.RawMessage // conversationsListPrefs
	fresh map[string]bool                       // the fetch manager's fresh ids
	// pending ids wait for the next fetch; busy while one runs
	pending map[string]bool
	busy    bool
	last    time.Time // when the last fetch started (the 200 ms spacing)
	// slow is the slowConnectionCount last seen (0: none yet)
	slow int
}

var listPrefsStates sync.Map // *App → *listPrefsState

func (a *App) listPrefs() *listPrefsState {
	v, _ := listPrefsStates.LoadOrStore(a, &listPrefsState{prefs: map[string]map[string]json.RawMessage{},
		fresh: map[string]bool{}, pending: map[string]bool{}})
	return v.(*listPrefsState)
}

// looksLikeChannelID is jh4t looksLikeChannelId.
var looksLikeChannelID = regexp.MustCompile(`^(C|G|D|Z)[A-Z0-9]{8,}$`).MatchString

// listPrefsRefetchGap is _refetchPendingObjects' spacing between fetches.
const listPrefsRefetchGap = 200 * time.Millisecond

// conversationsListPref is getConversationsListPref (iY2W Q): ch's pref
// name from conversationsListPrefs, nil before the answer (or without
// one); the read asks for the conversation's prefs when they aren't
// fresh (maybeRefetch). (Slack also needs a team id, which a signed-in
// App always has.)
func (a *App) conversationsListPref(ch, name string) json.RawMessage {
	if !looksLikeChannelID(ch) {
		return nil
	}
	st := a.listPrefs()
	st.mu.Lock()
	defer st.mu.Unlock()
	a.listPrefsResetLocked(st)
	if !st.fresh[ch] && !st.pending[ch] {
		st.pending[ch] = true
		a.listPrefsFetchLocked(st)
	}
	return st.prefs[ch][name]
}

// listPrefsResetLocked is _maybeResetState: a slowConnectionCount other
// than the one last seen marks every id stale. st.mu is held.
func (a *App) listPrefsResetLocked(st *listPrefsState) {
	if st.slow == 0 {
		st.slow = 1
	}
	if n := max(int(a.slowConnectionCount().Load()), 1); n != st.slow {
		st.slow = n
		clear(st.fresh)
	}
}

// listPrefsFetchLocked is _refetchPendingObjects: unless a fetch runs, the
// pending ids go out together once 200 ms have passed since the last one
// started. st.mu is held.
func (a *App) listPrefsFetchLocked(st *listPrefsState) {
	if st.busy || len(st.pending) == 0 {
		return
	}
	st.busy = true
	wait := max(0, time.Until(st.last.Add(listPrefsRefetchGap)))
	st.last = time.Now()
	go func() {
		if wait > 0 {
			time.Sleep(wait)
		}
		st.mu.Lock()
		ids := make([]string, 0, len(st.pending))
		for id := range st.pending {
			ids = append(ids, id)
		}
		clear(st.pending)
		st.mu.Unlock()
		var wg sync.WaitGroup
		for _, id := range ids { // the manager's fetch: one call per id, together
			wg.Add(1)
			go func() {
				defer wg.Done()
				a.fetchListPrefs(id)
			}()
		}
		wg.Wait()
		st.mu.Lock()
		for _, id := range ids {
			st.fresh[id] = true    // _updateAfterRefetch, failures included,
			delete(st.pending, id) // and not again for a read while it ran
		}
		st.busy = false
		a.listPrefsFetchLocked(st) // ids that came in meanwhile
		st.mu.Unlock()
		for _, id := range ids {
			a.emit(Event{Type: "list_prefs", Channel: id})
		}
	}()
}

// fetchListPrefs is fetchAndUpsertConversationsListPrefs:
// conversations.listPrefs for ch, its prefs stored as the conversation's
// (setConversationsListPrefs). A failure stores nothing.
func (a *App) fetchListPrefs(ch string) {
	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()
	var r struct {
		Prefs map[string]json.RawMessage `json:"prefs"`
	}
	if err := a.C.Call(ctx, "conversations.listPrefs", url.Values{"channel": {ch}, "reason": {"client_redux_store"}}, &r); err != nil {
		log.Printf("conversations.listPrefs %s: %v", ch, err)
		return
	}
	st := a.listPrefs()
	st.mu.Lock()
	st.prefs[ch] = r.Prefs
	st.mu.Unlock()
}

// listPrefsStale is markObjectsAsStale: the next read of each id fetches
// its prefs again. Only ids fetched before count; the UI is told, so a
// composer that reads them asks again.
func (a *App) listPrefsStale(ids ...string) {
	v, ok := listPrefsStates.Load(a)
	if !ok {
		return
	}
	st := v.(*listPrefsState)
	var stale []string
	st.mu.Lock()
	for _, id := range ids {
		if st.fresh[id] {
			delete(st.fresh, id)
			stale = append(stale, id)
		}
	}
	st.mu.Unlock()
	for _, id := range stale {
		a.emit(Event{Type: "list_prefs", Channel: id})
	}
}

// onSlackConnectPrefChanged is handleSlackConnectPrefsUpdate
// (slack_connect_pref_changed): allow_sc_file_uploads and
// external_awareness_context_bar mark every externally shared
// conversation's list prefs stale (handlePrefUpdated).
func (a *App) onSlackConnectPrefChanged(raw json.RawMessage) {
	var e struct {
		PrefName string `json:"pref_name"`
	}
	_ = json.Unmarshal(raw, &e)
	if e.PrefName != "allow_sc_file_uploads" && e.PrefName != "external_awareness_context_bar" {
		return
	}
	var ids []string
	a.S.RLock()
	for id, c := range a.S.Convs {
		if c.IsExtShared {
			ids = append(ids, id)
		}
	}
	a.S.RUnlock()
	a.listPrefsStale(ids...)
}

// SlackConnectFileSharingBlocked is rscm O for ch: an externally shared
// conversation whose list prefs turn can_upload_files off. Reading it
// for such a conversation fetches its list prefs when they aren't fresh;
// a "list_prefs" event follows the answer. A missing or null pref is
// allowed (`?? true`); any other falsy value blocks.
func (a *App) SlackConnectFileSharingBlocked(ch string) bool {
	c, ok := a.S.Conv(ch)
	if !ok || !c.IsExtShared {
		return false
	}
	v := a.conversationsListPref(ch, "can_upload_files")
	return len(v) > 0 && string(v) != "null" && !jsonTruthy(v)
}

// jsonTruthy is JavaScript's truthiness for a JSON value (null, false, 0,
// "" and NaN are falsy).
func jsonTruthy(v json.RawMessage) bool {
	var x any
	if json.Unmarshal(v, &x) != nil {
		return false
	}
	switch x := x.(type) {
	case nil:
		return false
	case bool:
		return x
	case float64:
		return x != 0
	case string:
		return x != ""
	}
	return true
}

// RestrictedExternalHuddle is uu7O f for ch (the + menu's
// isRestrictedExternalHuddle): external_huddles on, and the viewer is an
// external guest or ch is a huddle channel.
func (a *App) RestrictedExternalHuddle(ch string) bool {
	if a.experiment(ExpExternalHuddles) != "on" {
		return false
	}
	if u, ok := a.S.User(a.S.SelfID()); ok && u.IsRestricted && u.RestrictedUserType == "external_guest" {
		return true
	}
	if ch == "" {
		return false
	}
	c, ok := a.S.Conv(ch)
	return ok && convHuddleChannel(c)
}
