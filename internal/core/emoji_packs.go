package core

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"net/url"
	"slices"
	"sync"
	"time"

	"github.com/some-du6e/clack/internal/slack"
)

// Emoji packs, Slack's "emoji collections": the Add emoji dialog's Emoji
// packs tab (client-boot 8DMj AddEmojiPackDialogContent) reads them from
// the emojiCollections store (5BgF, not persisted), which this ports:
//
//   - fetchAllEmojiCollections (eyHx) runs once a session: hasFetched is
//     set before the call and cleared when it fails, so a failed read is
//     tried again on the next open and a good one never is. The call is
//     fetchEmojiCollections (wv1J): emoji.collections.list
//     {installed_only: false}. The store keeps the answer's available
//     packs, in its order, each marked installed when installed lists its
//     id, its localEmoji the installed copy's emoji (else its own). A pack
//     only in installed (Hack Club's Halloween, out of season on
//     2026-11-22) isn't kept. Then handlePotentialNewPack.
//   - addEmojiCollection: emoji.addCollection {collection_id}; Slack's
//     already_installed isn't an error. The store doesn't change on the
//     answer: the server's emoji_collection_added ({collection:
//     {collection_id, emoji}}) marks the pack installed with the event's
//     emoji (installEmojiCollection), and the emoji reach the custom emoji
//     map as emoji_changed adds.
//   - removeEmojiCollection: uninstallEmojiCollection first, which puts
//     the whole store back to its initial state (the next open fetches
//     again), then emoji.removeCollection {collection_id}.
//     emoji_collection_removed does the same reset.
//   - badgeEmojiCollection, when the add toast has closed: inputBadge (the
//     channel composer's emoji button dot, cleared by a click on that
//     button: removeEmojiCollectionInputBadge) and tabBadge (the picker's
//     Custom tab gets p-emoji_picker__group_tab--with_dot, which the 2026
//     stylesheet gives only position: relative, so nothing shows).
//
// Every change emits an "emoji_packs" event.

// PackEmoji is one emoji of a pack: its name and image.
type PackEmoji struct {
	Name, URL string
}

// PackEmojiList is a pack's emoji map ({name: url}) in the answer's order,
// the order Slack's lodash map walks it in. nil: the pack had no emoji
// field (Slack's card renders nothing then); an empty object is a non-nil
// empty list.
type PackEmojiList []PackEmoji

// UnmarshalJSON reads the {name: url} object in its own order; a value
// that isn't a string is left out.
func (l *PackEmojiList) UnmarshalJSON(b []byte) error {
	d := json.NewDecoder(bytes.NewReader(b))
	t, err := d.Token()
	if err != nil {
		return err
	}
	if dl, ok := t.(json.Delim); !ok || dl != '{' {
		return nil // null, or not an object: no emoji
	}
	out := PackEmojiList{}
	for d.More() {
		kt, err := d.Token()
		if err != nil {
			return err
		}
		var raw json.RawMessage
		if err := d.Decode(&raw); err != nil {
			return err
		}
		var u string
		if k, ok := kt.(string); ok && json.Unmarshal(raw, &u) == nil {
			out = append(out, PackEmoji{Name: k, URL: u})
		}
	}
	*l = out
	return nil
}

// EmojiPack is one pack (an emoji.collections.list entry) as the store
// holds it.
type EmojiPack struct {
	ID         string        `json:"id"`
	Name       string        `json:"name"`
	Author     string        `json:"author"`
	TeamID     string        `json:"team_id"`
	Locale     string        `json:"locale"`
	DateCreate int64         `json:"date_create"`
	IsDraft    bool          `json:"is_draft"`
	Emoji      PackEmojiList `json:"emoji"`
	// Installed and LocalEmoji are the store's (fetchAllEmojiCollections,
	// installEmojiCollection): the workspace has the pack, under these
	// names
	Installed  bool          `json:"-"`
	LocalEmoji PackEmojiList `json:"-"`
}

// Emoji pack prefs (users.prefs): when the newest pack became available
// to the viewer, and which of its calls to action they've clicked.
const (
	PrefEmojiPacksAvailableTime  = "emoji_packs_most_recent_available_time"
	PrefEmojiPacksPickerCTA      = "emoji_packs_clicked_picker_cta"
	PrefEmojiPacksPostInstallCTA = "emoji_packs_clicked_picker_post_install_cta"
	PrefEmojiPacksCollectionCTA  = "emoji_packs_clicked_collection_cta"
)

// emojiPackNewFor is how long a pack counts as new after
// emoji_packs_most_recent_available_time (UY+B fl: 1209600 s).
const emojiPackNewFor = 1209600 * time.Second

// PermAddCustomEmoji is permissions/info's ADD_CUSTOM_EMOJI (Llxp).
const PermAddCustomEmoji = "ADD_CUSTOM_EMOJI"

type packState struct {
	mu         sync.Mutex
	list       []EmojiPack // the store's collections, keyBy order
	fetched    bool        // hasFetched
	inputBadge bool
	tabBadge   bool
	gen        uint64 // bumped by every change of list
	// canAdd is Llxp D as the permission store holds it (permissions/info
	// ADD_CUSTOM_EMOJI, asked at boot: LoadEmojiPermissions), answered at
	// canAddAt (zero: never); permWait is closed when the call out answers
	canAdd   bool
	canAddAt time.Time
	permWait chan struct{}
	// now is the clock the Halloween season reads (tests set it)
	now func() time.Time
}

var packIndex = newPerApp() // *App → *packState

func (a *App) packs() *packState {
	if v, ok := packIndex.Load(a); ok {
		return v.(*packState)
	}
	v, _ := packIndex.LoadOrStore(a, &packState{})
	return v.(*packState)
}

// EmojiPacks is the store's collections (emojiCollections.collections,
// UY+B uI) in the order emoji.collections.list gave them, and whether
// they've been asked for (hasFetched).
func (a *App) EmojiPacks() (packs []EmojiPack, fetched bool) {
	ps := a.packs()
	ps.mu.Lock()
	defer ps.mu.Unlock()
	return append([]EmojiPack(nil), ps.list...), ps.fetched
}

// EmojiPacksGen changes whenever the store's collections do (a reader
// that keeps something built from EmojiPacks rebuilds it then).
func (a *App) EmojiPacksGen() uint64 {
	ps := a.packs()
	ps.mu.Lock()
	defer ps.mu.Unlock()
	return ps.gen
}

// NewestEmojiPack is UY+B _J: the collection with the latest date_create
// (lodash maxBy: the first of equals).
func (a *App) NewestEmojiPack() (EmojiPack, bool) {
	packs, _ := a.EmojiPacks()
	return newestPack(packs)
}

func newestPack(packs []EmojiPack) (EmojiPack, bool) {
	best := -1
	for i, p := range packs {
		if best < 0 || p.DateCreate > packs[best].DateCreate {
			best = i
		}
	}
	if best < 0 {
		return EmojiPack{}, false
	}
	return packs[best], true
}

// EmojiPacksRecent is UY+B fl: emoji_packs_most_recent_available_time is
// a number and less than 14 days ago.
func (a *App) EmojiPacksRecent() bool {
	v, ok := a.Pref(PrefEmojiPacksAvailableTime)
	if !ok {
		return false
	}
	var t float64
	if json.Unmarshal(v, &t) != nil {
		return false // lodash isNumber: a string doesn't count
	}
	return time.Now().Before(time.UnixMilli(int64(t * 1000)).Add(emojiPackNewFor))
}

// EmojiPackIsNew is EmojiPackCard's "new" pill: id is the newest pack,
// within the 14 days, and the viewer hasn't clicked a pack since
// (emoji_packs_clicked_collection_cta).
func (a *App) EmojiPackIsNew(id string) bool {
	p, ok := a.NewestEmojiPack()
	return ok && p.ID == id && a.EmojiPacksRecent() && !a.PrefBool(PrefEmojiPacksCollectionCTA, false)
}

// EmojiPackNew is the pack whose card draws the "new" pill (EmojiPackIsNew),
// "" for none: one read of the store and the prefs for every card.
func (a *App) EmojiPackNew() string {
	p, ok := a.NewestEmojiPack()
	if !ok || !a.EmojiPacksRecent() || a.PrefBool(PrefEmojiPacksCollectionCTA, false) {
		return ""
	}
	return p.ID
}

// ClickedEmojiPackCTA is EmojiPackCard's handleOnClick on the new pack:
// setUserPrefByApi emoji_packs_clicked_collection_cta true, the local
// pref first (Slack's is optimistic), then users.prefs.set.
func (a *App) ClickedEmojiPackCTA() {
	a.mergePrefs(map[string]json.RawMessage{PrefEmojiPacksCollectionCTA: json.RawMessage("true")})
	ps := a.packs()
	ps.mu.Lock()
	ps.gen++ // what the cards drew from (EmojiPackNew) changed
	ps.mu.Unlock()
	a.emit(Event{Type: "emoji_packs"})
	go func() {
		ctx, cancel := context.WithTimeout(a.bgCtx(), 30*time.Second)
		defer cancel()
		_ = a.SetPref(ctx, PrefEmojiPacksCollectionCTA, true)
	}()
}

// FetchEmojiPacks is fetchAllEmojiCollections: the packs once a session
// (again after a failure or a removal), off the caller's goroutine. The
// store answers with an "emoji_packs" event.
func (a *App) FetchEmojiPacks() {
	ps := a.packs()
	ps.mu.Lock()
	if ps.fetched || a.C == nil {
		ps.mu.Unlock()
		return
	}
	ps.fetched = true
	ps.mu.Unlock()
	go func() {
		// a mount the viewer opened (the dialog's tab, the picker) asks
		// again after a failure, as Slack's does: past the client's
		// identical-failure backoff
		ctx, cancel := context.WithTimeout(slack.NoBackoff(a.bgCtx()), 30*time.Second)
		defer cancel()
		list, err := a.fetchEmojiPacks(ctx)
		ps.mu.Lock()
		if err != nil {
			ps.fetched = false
			ps.mu.Unlock()
			return
		}
		ps.list, ps.fetched = list, true // setEmojiCollections
		ps.gen++
		// the newest as stored now (a copy: emoji_collection_added
		// writes the list under the lock)
		newest, ok := newestPack(ps.list)
		ps.mu.Unlock()
		// the store first: Slack dispatches setEmojiCollections without
		// waiting for handlePotentialNewPack's reads and writes
		a.emit(Event{Type: "emoji_packs"})
		// Llxp D from the permission store, which boot filled
		// (LoadEmojiPermissions): the picker's call to action
		// (EmojiPickerPackCTA) read it from its first frame
		can := a.emojiCanAdd(ctx)
		if ok {
			a.potentialNewPack(ctx, newest, can)
		}
	}()
}

func (a *App) fetchEmojiPacks(ctx context.Context) ([]EmojiPack, error) {
	var r struct {
		Installed []EmojiPack `json:"installed"`
		Available []EmojiPack `json:"available"`
	}
	if err := a.C.Call(ctx, "emoji.collections.list", url.Values{"installed_only": {"false"}}, &r); err != nil {
		return nil, err
	}
	inst := make(map[string]EmojiPack, len(r.Installed))
	for _, p := range r.Installed {
		inst[p.ID] = p
	}
	out := make([]EmojiPack, 0, len(r.Available))
	seen := map[string]int{}
	for _, p := range r.Available {
		if i, ok := inst[p.ID]; ok {
			p.Installed, p.LocalEmoji = true, i.Emoji
		} else {
			p.LocalEmoji = p.Emoji
		}
		if k, dup := seen[p.ID]; dup { // keyBy: the last one wins, in the first one's place
			out[k] = p
			continue
		}
		seen[p.ID] = len(out)
		out = append(out, p)
	}
	return out, nil
}

// potentialNewPack is handlePotentialNewPack: when the newest pack is
// newer than emoji_packs_most_recent_available_time and the viewer may
// add custom emoji, or it's installed, the four emoji pack prefs start
// over (setMultipleUserPrefsByApi): available now, nothing clicked.
func (a *App) potentialNewPack(ctx context.Context, p EmojiPack, canAdd bool) {
	since := 0.0
	if v, ok := a.Pref(PrefEmojiPacksAvailableTime); ok {
		_ = json.Unmarshal(v, &since)
	}
	if float64(p.DateCreate) <= since {
		return
	}
	if !p.Installed && !canAdd {
		return
	}
	_ = a.SetMultiplePrefs(ctx, map[string]any{
		PrefEmojiPacksAvailableTime:  float64(time.Now().UnixMilli()) / 1000,
		PrefEmojiPacksPickerCTA:      false,
		PrefEmojiPacksPostInstallCTA: false,
		PrefEmojiPacksCollectionCTA:  false,
	})
}

// emojiClientPermissions is Slack's client permission set's custom emoji
// group (client-boot's getPermissionSet({context: "client"}), its w:
// ADD_CUSTOM_EMOJI, DELETE_CUSTOM_EMOJI, EDIT_SLACKBOT_RESPONSES), which
// maybeFetchClientPermissions asks for at boot.
var emojiClientPermissions = []string{PermAddCustomEmoji, "DELETE_CUSTOM_EMOJI", "EDIT_SLACKBOT_RESPONSES"}

// LoadEmojiPermissions is maybeFetchClientPermissions for the custom emoji
// group: permissions/info, its ADD_CUSTOM_EMOJI kept (Llxp D) for the
// picker's call to action and the pack read. Boot asks, so the first
// picker of a session draws the call to action from its first frame, as
// Slack's does; a caller while a call is out waits for its answer. A
// failed call allows nothing and keeps nothing (the next read asks).
func (a *App) LoadEmojiPermissions(ctx context.Context) bool {
	ps := a.packs()
	ps.mu.Lock()
	if w := ps.permWait; w != nil {
		ps.mu.Unlock()
		select {
		case <-w:
		case <-ctx.Done():
		}
		ps.mu.Lock()
		defer ps.mu.Unlock()
		return ps.canAdd
	}
	w := make(chan struct{})
	ps.permWait = w
	ps.mu.Unlock()
	can, err := a.askEmojiPermissions(ctx)
	ps.mu.Lock()
	ps.permWait = nil
	if err == nil {
		ps.canAdd, ps.canAddAt = can, time.Now()
	}
	ps.mu.Unlock()
	close(w)
	if err == nil {
		a.emit(Event{Type: "emoji_packs"})
	}
	return can
}

// emojiCanAdd is Llxp D from the permission store: the last answer while
// under an hour old (Slack's permissions are kept an hour), else asked.
func (a *App) emojiCanAdd(ctx context.Context) bool {
	ps := a.packs()
	ps.mu.Lock()
	if !ps.canAddAt.IsZero() && time.Since(ps.canAddAt) <= chPermMaxAge {
		defer ps.mu.Unlock()
		return ps.canAdd
	}
	ps.mu.Unlock()
	return a.LoadEmojiPermissions(ctx)
}

// askEmojiPermissions is the permissions/info call: whether it grants
// ADD_CUSTOM_EMOJI.
func (a *App) askEmojiPermissions(ctx context.Context) (bool, error) {
	body := map[string]any{"user_id": a.S.SelfID(), "permissions": emojiClientPermissions}
	// a workspace permission: on Grid asked for the viewer's workspace,
	// as the broadcast permissions' group DM case asks (broadcast.go)
	team := a.C.Creds.Team
	perTeam := a.C.Creds.EnterpriseID != "" && team != ""
	if perTeam {
		body["team_ids"] = []string{team}
	}
	var r struct {
		Allowed        []string            `json:"allowed"`
		AllowedPerTeam map[string][]string `json:"allowed_per_team"`
	}
	if err := a.C.Edge(ctx, "permissions/info", body, &r); err != nil {
		return false, err
	}
	allowed := r.Allowed
	if perTeam && r.AllowedPerTeam != nil {
		allowed = append(allowed, r.AllowedPerTeam[team]...)
	}
	return slices.Contains(allowed, PermAddCustomEmoji), nil
}

// AddEmojiPack is addEmojiCollection: emoji.addCollection {collection_id}.
// already_installed counts as added. The store changes when the server's
// emoji_collection_added arrives.
func (a *App) AddEmojiPack(ctx context.Context, id string) error {
	// a click: a retry after a failure is deliberate (past the backoff)
	err := a.C.Call(slack.NoBackoff(ctx), "emoji.addCollection", url.Values{"collection_id": {id}}, nil)
	var se *slack.Error
	if errors.As(err, &se) && se.Code == "already_installed" {
		return nil
	}
	return err
}

// RemoveEmojiPack is removeEmojiCollection: the store back to its initial
// state (uninstallEmojiCollection), then emoji.removeCollection
// {collection_id}.
func (a *App) RemoveEmojiPack(ctx context.Context, id string) error {
	a.resetEmojiPacks()
	return a.C.Call(slack.NoBackoff(ctx), "emoji.removeCollection", url.Values{"collection_id": {id}}, nil) // a click
}

// resetEmojiPacks is uninstallEmojiCollection: the store's initial state.
func (a *App) resetEmojiPacks() {
	ps := a.packs()
	ps.mu.Lock()
	ps.list, ps.fetched, ps.inputBadge, ps.tabBadge = nil, false, false, false
	ps.gen++
	ps.mu.Unlock()
	a.emit(Event{Type: "emoji_packs"})
}

// BadgeEmojiPacks is badgeEmojiCollection (the add toast's
// onCloseComplete): the composer's and the picker's badges.
func (a *App) BadgeEmojiPacks() {
	ps := a.packs()
	ps.mu.Lock()
	ps.inputBadge, ps.tabBadge = true, true
	ps.mu.Unlock()
	a.emit(Event{Type: "emoji_packs"})
}

// EmojiPackInputBadge is UY+B _o: the channel composer's emoji button
// shows its dot.
func (a *App) EmojiPackInputBadge() bool {
	ps := a.packs()
	ps.mu.Lock()
	defer ps.mu.Unlock()
	return ps.inputBadge
}

// ClearEmojiPackInputBadge is removeEmojiCollectionInputBadge (a click
// on the emoji button).
func (a *App) ClearEmojiPackInputBadge() {
	ps := a.packs()
	ps.mu.Lock()
	was := ps.inputBadge
	ps.inputBadge = false
	ps.mu.Unlock()
	if was {
		a.emit(Event{Type: "emoji_packs"})
	}
}

// onEmojiCollectionAdded is the emoji_collection_added handler
// (installEmojiCollection): a pack the store holds, not yet installed,
// becomes installed with the event's emoji as its local ones.
func (a *App) onEmojiCollectionAdded(raw json.RawMessage) {
	var e struct {
		Collection *struct {
			ID    json.RawMessage `json:"collection_id"`
			Emoji PackEmojiList   `json:"emoji"`
		} `json:"collection"`
	}
	if json.Unmarshal(raw, &e) != nil || e.Collection == nil {
		return
	}
	id := rawID(e.Collection.ID)
	if id == "" {
		return
	}
	ps := a.packs()
	ps.mu.Lock()
	changed := false
	for i := range ps.list {
		if ps.list[i].ID == id && !ps.list[i].Installed {
			ps.list[i].Installed, ps.list[i].LocalEmoji = true, e.Collection.Emoji
			changed = true
		}
	}
	if changed {
		ps.gen++
	}
	ps.mu.Unlock()
	if changed {
		a.emit(Event{Type: "emoji_packs"})
	}
}

// onEmojiCollectionRemoved is the emoji_collection_removed handler
// (uninstallEmojiCollection): the store starts over.
func (a *App) onEmojiCollectionRemoved(raw json.RawMessage) {
	var e struct {
		ID json.RawMessage `json:"collection_id"`
	}
	if json.Unmarshal(raw, &e) != nil || rawID(e.ID) == "" {
		return
	}
	a.resetEmojiPacks()
}

// rawID is a collection id as Slack's toString() reads it: a string's
// text, a number's digits.
func rawID(b json.RawMessage) string {
	var s string
	if json.Unmarshal(b, &s) == nil {
		return s
	}
	var n json.Number
	if json.Unmarshal(b, &n) == nil {
		return n.String()
	}
	return "" // null, absent or not an id
}
