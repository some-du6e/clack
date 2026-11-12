package core

// Recent searches and recent places the way Slack's client keeps them
// (CDN build 4d0c78085232, 2026-11-10; docs/SEARCH.md):
//
//   - desktop_search_recents_write "on" (PEUt isDesktopSearchRecentsWriteEnabled):
//     every navigation (maybeRecordRecentNavigationEvent, lwKM) and every
//     search run from the switcher (maybeRecordRecentSearchEvent) is
//     buffered and, at the next idle moment (requestIdleCallback, 5 s
//     timeout), merged into the session's cumulative record: navigation
//     capped at 40 events with at most 10 threads, searches at 100, newest
//     first (upsertNavigationEvent / upsertSearchEvent). The record goes to
//     recordVisitsByApi (XtzY, throttled to once a minute, leading and
//     trailing): the navigation as visits.record, and the searches, when
//     they changed, as the "recents" user pref (setRecentsByApi:
//     users.prefs.set name=recents, the JSON object). When the navigation
//     can't be mapped to visits, visits.record failed, or three drains
//     failed in five minutes, the whole record goes to the pref instead.
//   - desktop_search_recents_read "on" (isDesktopSearchRecentsReadEnabled):
//     the "recents" pref is loaded (users.prefs.get prefs=recents) and
//     visits.list (limit 40, with the last checksum) feeds Recent places;
//     the switcher's Recent searches (suggestionsBlankSlateQueryHistory)
//     are the pref's searches, Salesforce ones left out, newest first.
//     With it off Recent searches come from search.autocomplete (query "",
//     limit 100, use_hash: the QueryHistoryCache, CZjT, kept 5 minutes and
//     dropped after search.save) and Recent places from local history.
//
// Clack keeps none of it on disk: the record lives in memory for the
// session, the server copies are Slack's own.

import (
	"context"
	"encoding/json"
	"errors"
	"net/url"
	"regexp"
	"slices"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/some-du6e/clack/internal/slack"
)

const prefRecents = "recents"

// Navigation event object types (EsBt Z).
const (
	NavDM                = "DM"
	NavChannel           = "CHANNEL"
	NavThread            = "THREAD"
	NavRecordChannel     = "RECORD_CHANNEL"
	NavRecordChannelSFDC = "RECORD_CHANNEL_SFDC"
	NavFile              = "FILE"
	NavPendingFile       = "PENDING_FILE"
	NavTrigger           = "TRIGGER"
	NavUser              = "USER"
	NavPage              = "PAGE"
)

// Recent search types (EsBt _).
const (
	RecentSearchSlack      = "SLACK"
	RecentSearchSalesforce = "SALESFORCE"
	RecentSearchSlackbot   = "SLACKBOT"
)

const (
	recentsNavCap      = 40  // capNavigationEvents
	recentsThreadCap   = 10  // … of which threads
	recentsSearchCap   = 100 // upsertSearchEvent
	recentsLatchDrains = 3   // isLatched: 3 failed drains …
	// … within 5 minutes (recentFailedDrains, 3e5)
	recentsLatchWindow = 5 * time.Minute
	// recordVisitsByApi's throttle (createThrottledThunk(v, 6e4)).
	recentsRecordEvery = time.Minute
	// queryHistoryTTL is QueryHistoryCache's lifetime (CZjT, 3e5).
	queryHistoryTTL = 5 * time.Minute
	// recentsVisitsLimit is visits.list's page (fetchAndStore).
	recentsVisitsLimit = 40
)

// RecentsFlushDelay is when the buffered events are merged and synced.
// Slack waits for the browser's next idle period (requestIdleCallback,
// at most 5 s); Clack has no idle signal and waits this long.
var RecentsFlushDelay = time.Second

// RecentNavigation is one navigation event: a "recents" pref navigation
// entry, or a visits.list row as Slack converts it (toNavigationEvents).
type RecentNavigation struct {
	ID         string `json:"id"`
	ObjectType string `json:"object_type"`
	Timestamp  int64  `json:"timestamp"` // unix ms
	ThreadTS   string `json:"thread_ts,omitempty"`
}

// RecentSearch is one search event in the "recents" pref.
type RecentSearch struct {
	Query     string `json:"query"`
	Type      string `json:"type"`
	Timestamp int64  `json:"timestamp"` // unix ms
	ThreadTS  string `json:"thread_ts,omitempty"`
}

// recentsData is the pref's two lists; nil means the key is absent.
type recentsData struct {
	Navigation []RecentNavigation
	Search     []RecentSearch
}

// QueryHistoryItem is one Recent searches row.
type QueryHistoryItem struct {
	Query    string
	Slackbot bool   // a Slackbot conversation ("slackbot" type)
	ThreadTS string // its Slackbot thread
	Hash     string // search.autocomplete's hash (read off)
}

type recentsState struct {
	mu sync.Mutex

	// the write buffer (lwKM getRecentsWriteBuffer)
	recorded      recentsData
	pendingNav    []RecentNavigation
	pendingSearch []RecentSearch
	scheduled     bool

	// recordVisitsByApi (XtzY): the navigation visits.record last took,
	// the searches last written to the pref, the failed drains, the call
	// in flight
	acceptedNav  []RecentNavigation
	acceptedOK   bool
	writtenSrch  []RecentSearch
	writtenOK    bool
	drains       int
	lastFailure  time.Time
	visitsFlight bool
	// the 60 s leading+trailing throttle around it
	lastRecord   time.Time
	recordTimer  *time.Timer
	recordLatest *recentsData

	// visits.list's store (xotU)
	visits         []RecentNavigation
	visitsLoaded   bool
	visitsChecksum string
	loadTried      bool // a load was claimed with recents read on (claimRecentsLoad)

	// search.autocomplete's QueryHistoryCache (CZjT)
	hist   []QueryHistoryItem
	histAt time.Time
	histOK bool

	// forgotten are the deleted queries (canonicalSearchQuery) later
	// writes of the pref leave out (4jD3 forgetSearchQuery)
	forgotten map[string]bool

	ver uint64           // bumped whenever what Recent places / searches read changed
	now func() time.Time // tests
}

func (r *recentsState) clock() time.Time {
	if r.now != nil {
		return r.now()
	}
	return time.Now()
}

// SetRecentsClock replaces the recents clock (tests).
func (a *App) SetRecentsClock(now func() time.Time) {
	a.rec.mu.Lock()
	a.rec.now = now
	a.rec.mu.Unlock()
}

// RecentsVersion counts changes to what RecentPlacesEvents and
// RecentSearches read, so the switcher can redraw when they arrive.
func (a *App) RecentsVersion() uint64 {
	a.rec.mu.Lock()
	defer a.rec.mu.Unlock()
	return a.rec.ver
}

// RecentsReadEnabled is isDesktopSearchRecentsReadEnabled.
func (a *App) RecentsReadEnabled() bool {
	return a.experiment("desktop_search_recents_read") == "on"
}

// RecentsWriteEnabled is isDesktopSearchRecentsWriteEnabled.
func (a *App) RecentsWriteEnabled() bool {
	return a.experiment("desktop_search_recents_write") == "on"
}

// ---- ids → object types (xeUe objectTypeForId) ----

var (
	navChannelID = regexp.MustCompile(`^(C|G|D|Z)[A-Z0-9]{8,}$`) // looksLikeChannelId
	navFileID    = regexp.MustCompile(`^F[A-Z0-9]{8,14}$`)       // looksLikeFileId
	navTriggerID = regexp.MustCompile(`^Ft[A-Z0-9]{7,}$`)        // looksLikeTriggerId
	navMemberID  = regexp.MustCompile(`^@?(U|W)[A-Z0-9]{8,}$`)   // looksLikeMemberId
)

// SlackPageIDs are the page views Slack records as PAGE (97W+ c, the
// ids isValidPageId accepts, and "Vall_threads" for Threads), by Clack's
// page route. Clack's DMs and Directories pages have no such id: Slack
// doesn't record them either.
var SlackPageIDs = map[string]string{
	"activity": "Pactivity", "later": "Plater", "saved": "Psaved", "drafts": "Pdrafts",
	"files": "Pbrowse-unified-files", "huddles": "Phuddles", "unreads": "Punreads", "threads": "Vall_threads",
}

// SlackPageRoute is the Clack page route of a Slack page id ("" none).
func SlackPageRoute(id string) string {
	for k, v := range SlackPageIDs {
		if v == id {
			return k
		}
	}
	return ""
}

// slackPageIDValid is isValidPageId (97W+ c) for the ids Clack records.
func slackPageIDValid(id string) bool { return SlackPageRoute(id) != "" }

// navObjectType is objectTypeForId: "" when the id isn't a place.
func navObjectType(id string) string {
	switch {
	case strings.Count(id, ":") == 2 && strings.HasPrefix(id, "S:"):
		return NavRecordChannelSFDC
	case navTriggerID.MatchString(id):
		return NavTrigger
	case navFileID.MatchString(id):
		return NavFile
	case navChannelID.MatchString(id):
		switch id[0] {
		case 'D':
			return NavDM
		case 'Z':
			return NavRecordChannel
		}
		return NavChannel
	case navMemberID.MatchString(id) || id == "USLACK":
		return NavUser
	case slackPageIDValid(id):
		return NavPage
	}
	return ""
}

// ---- the merge (lwKM) ----

func sameNavTarget(a, b RecentNavigation) bool { return a.ID == b.ID && a.ThreadTS == b.ThreadTS }

// capNavigationEvents: newest first, at most 10 threads and 40 in all.
func capNavigationEvents(e []RecentNavigation) []RecentNavigation {
	t := append([]RecentNavigation(nil), e...)
	sort.SliceStable(t, func(i, j int) bool { return t[i].Timestamp > t[j].Timestamp })
	var threads, rest []RecentNavigation
	for _, ev := range t {
		if ev.ObjectType == NavThread {
			if len(threads) < recentsThreadCap {
				threads = append(threads, ev)
			}
		}
	}
	for _, ev := range t {
		if ev.ObjectType != NavThread && len(rest) < recentsNavCap-len(threads) {
			rest = append(rest, ev)
		}
	}
	out := append(threads, rest...)
	sort.SliceStable(out, func(i, j int) bool { return out[i].Timestamp > out[j].Timestamp })
	return out
}

// upsertNavigationEvent keeps the newer event per target.
func upsertNavigationEvent(e []RecentNavigation, t RecentNavigation) []RecentNavigation {
	for _, n := range e {
		if sameNavTarget(n, t) && n.Timestamp >= t.Timestamp {
			return e
		}
	}
	out := []RecentNavigation{t}
	for _, n := range e {
		if !sameNavTarget(n, t) {
			out = append(out, n)
		}
	}
	return capNavigationEvents(out)
}

func sortSearchesCap(e []RecentSearch) []RecentSearch {
	sort.SliceStable(e, func(i, j int) bool { return e[i].Timestamp > e[j].Timestamp })
	if len(e) > recentsSearchCap {
		e = e[:recentsSearchCap]
	}
	return e
}

// upsertSearchEvent keeps one event per query and type, the newest, with
// a Slackbot thread_ts kept from either.
func upsertSearchEvent(e []RecentSearch, t RecentSearch) []RecentSearch {
	at := -1
	for i, n := range e {
		if n.Query == t.Query && n.Type == t.Type {
			at = i
			break
		}
	}
	if at < 0 {
		return sortSearchesCap(append([]RecentSearch{t}, e...))
	}
	n := e[at]
	a := t
	if n.Timestamp > t.Timestamp {
		a = n
	}
	th := a.ThreadTS
	if th == "" {
		th = t.ThreadTS
	}
	if th == "" {
		th = n.ThreadTS
	}
	r := a
	if th != "" && a.ThreadTS != th {
		r.ThreadTS = th
	}
	if n.Timestamp == r.Timestamp && n.ThreadTS == r.ThreadTS {
		return e
	}
	out := []RecentSearch{r}
	for _, x := range e {
		if x.Query != t.Query || x.Type != t.Type {
			out = append(out, x)
		}
	}
	return sortSearchesCap(out)
}

// mergeRecents folds t's events into e; ok=false when nothing changed.
func mergeRecents(e, t recentsData) (recentsData, bool) {
	out, changed := e, false
	if t.Navigation != nil {
		n := e.Navigation
		if n == nil {
			n = []RecentNavigation{}
		}
		for _, ev := range t.Navigation {
			n = upsertNavigationEvent(n, ev)
		}
		if !slices.Equal(n, e.Navigation) || e.Navigation == nil {
			out.Navigation, changed = n, true
		}
	}
	if t.Search != nil {
		s := e.Search
		if s == nil {
			s = []RecentSearch{}
		}
		for _, ev := range t.Search {
			s = upsertSearchEvent(s, ev)
		}
		if !slices.Equal(s, e.Search) || e.Search == nil {
			out.Search, changed = s, true
		}
	}
	return out, changed
}

// ---- the pref ----

// recentsPref is getRecentsData: the "recents" pref parsed (a JSON
// string, as users.prefs.get sends JSON prefs, or an object), with the
// object's other keys kept for writing it back.
func (a *App) recentsPref() (recentsData, map[string]json.RawMessage) {
	raw, ok := a.Pref(prefRecents)
	if !ok || len(raw) == 0 {
		return recentsData{}, nil
	}
	var s string
	if json.Unmarshal(raw, &s) == nil {
		raw = json.RawMessage(s)
	}
	var obj map[string]json.RawMessage
	if json.Unmarshal(raw, &obj) != nil {
		return recentsData{}, nil
	}
	var d recentsData
	if v, ok := obj["navigation"]; ok {
		_ = json.Unmarshal(v, &d.Navigation)
		if d.Navigation == nil {
			d.Navigation = []RecentNavigation{}
		}
	}
	if v, ok := obj["search"]; ok {
		_ = json.Unmarshal(v, &d.Search)
		if d.Search == nil {
			d.Search = []RecentSearch{}
		}
	}
	return d, obj
}

// recentsJSON encodes d over the pref's other keys.
func recentsJSON(d recentsData, rest map[string]json.RawMessage) string {
	obj := make(map[string]json.RawMessage, len(rest)+2)
	for k, v := range rest {
		obj[k] = v
	}
	if d.Navigation != nil {
		obj["navigation"], _ = json.Marshal(d.Navigation)
	}
	if d.Search != nil {
		obj["search"], _ = json.Marshal(d.Search)
	}
	b, _ := json.Marshal(obj)
	return string(b)
}

// setRecentsLocal is updateUserPref without the API (n3H2 rX): the local
// pref changes, Slack's server copy doesn't.
func (a *App) setRecentsLocal(d recentsData, rest map[string]json.RawMessage) {
	s, _ := json.Marshal(recentsJSON(d, rest))
	a.mergePrefs(map[string]json.RawMessage{prefRecents: s})
	a.bumpRecents()
}

func (a *App) bumpRecents() {
	a.rec.mu.Lock()
	a.rec.ver++
	a.rec.mu.Unlock()
}

// LoadRecentsPrefs is loadRecentsPrefs: users.prefs.get for "recents",
// upserted.
func (a *App) LoadRecentsPrefs(ctx context.Context) error {
	var r struct {
		Prefs map[string]json.RawMessage `json:"prefs"`
	}
	if err := a.C.Call(ctx, "users.prefs.get", url.Values{"prefs": {prefRecents}}, &r); err != nil {
		return err
	}
	v, ok := r.Prefs[prefRecents]
	if !ok {
		return nil
	}
	a.mergePrefs(map[string]json.RawMessage{prefRecents: v})
	a.bumpRecents()
	return nil
}

// visitRaw is one visits.list row.
type visitRaw struct {
	EntityID   string `json:"entity_id"`
	EntityType string `json:"entity_type"`
	LastMS     int64  `json:"date_last_visited_ms"`
	ThreadTS   string `json:"thread_ts"`
}

// visitTypes maps visits' entity types to navigation object types
// (xotU toNavigationEvents; triggers are dropped).
var visitTypes = map[string]string{
	"channel": NavChannel, "thread": NavThread, "dm": NavDM, "record_channel": NavRecordChannel,
	"record_channel_sfdc": NavRecordChannelSFDC, "file": NavFile, "view": NavPage,
}

// LoadVisits is fetchVisitsIntoStore: visits.list {limit 40, checksum};
// an unchanged checksum keeps the store.
func (a *App) LoadVisits(ctx context.Context) error {
	a.rec.mu.Lock()
	sum := a.rec.visitsChecksum
	a.rec.mu.Unlock()
	p := url.Values{"limit": {strconv.Itoa(recentsVisitsLimit)}}
	if sum != "" {
		p.Set("checksum", sum)
	}
	var r struct {
		Checksum string     `json:"checksum"`
		Visits   []visitRaw `json:"visits"`
	}
	if err := a.C.Call(ctx, "visits.list", p, &r); err != nil {
		return err
	}
	a.rec.mu.Lock()
	defer a.rec.mu.Unlock()
	if sum != "" && r.Checksum == sum {
		return nil
	}
	evs := make([]RecentNavigation, 0, len(r.Visits))
	for _, v := range r.Visits {
		if t, ok := visitTypes[v.EntityType]; ok {
			evs = append(evs, RecentNavigation{ID: v.EntityID, ObjectType: t, Timestamp: v.LastMS, ThreadTS: v.ThreadTS})
		}
	}
	a.rec.visits, a.rec.visitsLoaded, a.rec.visitsChecksum = evs, true, r.Checksum
	a.rec.ver++
	return nil
}

// MaybeLoadRecents is maybeLoadRecentsPrefs at boot: with recents read
// on, the pref and visits.list, unless a forced group's load claimed them
// first (boot runs it on a goroutine, which can start after a group set
// right after boot). Slack's "the pref changed" reload is the
// search_recents event's LoadRecentsPrefs (events.go).
func (a *App) MaybeLoadRecents(ctx context.Context) {
	if a.claimRecentsLoad() {
		a.loadRecents(ctx)
	}
}

// claimRecentsLoad reports whether the caller loads the recents: recents
// read is on and no load claimed them yet. The check and the claim are one
// step under the lock, so boot's trigger and a forced group's arriving
// together load them once.
func (a *App) claimRecentsLoad() bool {
	if !a.RecentsReadEnabled() {
		return false
	}
	a.rec.mu.Lock()
	defer a.rec.mu.Unlock()
	if a.rec.loadTried {
		return false
	}
	a.rec.loadTried = true
	return true
}

func (a *App) loadRecents(ctx context.Context) {
	_ = a.LoadRecentsPrefs(ctx)
	_ = a.LoadVisits(ctx)
}

// recentsAfterExperiments loads the recents when a forced group turned
// recents read on after boot (Slack reads them at its next load; forced
// groups apply live in Clack). The load is claimed before its goroutine
// starts. Before boot is done, boot's own trigger loads them.
func (a *App) recentsAfterExperiments() {
	select {
	case <-a.ready:
	default:
		return
	}
	if a.claimRecentsLoad() {
		go a.loadRecents(a.bg)
	}
}

// ---- recording ----

// RecordRecentNavigation is maybeRecordRecentNavigationEvent {id,
// threadTs}: with recents write on, the place is buffered for the next
// sync. id is a conversation, file, user or Slack page id
// (SlackPageIDs); a thread is its channel with threadTS.
func (a *App) RecordRecentNavigation(id, threadTS string) {
	if !a.RecentsWriteEnabled() {
		return
	}
	t := NavThread
	if threadTS == "" {
		t = navObjectType(id)
	}
	if t == "" {
		return
	}
	a.rec.mu.Lock()
	ev := RecentNavigation{ID: id, ObjectType: t, Timestamp: a.rec.clock().UnixMilli(), ThreadTS: threadTS}
	a.rec.pendingNav = append(a.rec.pendingNav, ev)
	first := !a.rec.scheduled
	a.rec.scheduled = true
	a.rec.mu.Unlock()
	if first {
		a.scheduleRecentsFlush()
	}
}

// RecordRecentSearch is maybeRecordRecentSearchEvent {query, type}: a
// search run from the switcher (type RecentSearchSlack), buffered with
// recents write on.
func (a *App) RecordRecentSearch(query, typ string) {
	query = strings.TrimSpace(query)
	if query == "" || !a.RecentsWriteEnabled() {
		return
	}
	if typ == "" {
		typ = RecentSearchSlack
	}
	a.rec.mu.Lock()
	delete(a.rec.forgotten, canonicalSearchQuery(query)) // unforgetSearchQuery
	ev := RecentSearch{Query: query, Type: typ, Timestamp: a.rec.clock().UnixMilli()}
	a.rec.pendingSearch = append(a.rec.pendingSearch, ev)
	first := !a.rec.scheduled
	a.rec.scheduled = true
	a.rec.mu.Unlock()
	if first {
		a.scheduleRecentsFlush()
	}
}

func (a *App) scheduleRecentsFlush() {
	time.AfterFunc(RecentsFlushDelay, func() {
		select {
		case <-a.bg.Done():
			return
		default:
		}
		a.FlushRecents()
	})
}

// FlushRecents is flushBufferedRecentEvents: the buffered events merged
// into the session's record; with recents read on the local pref takes
// them too; then the record is synced (recordVisitsByApi, throttled).
func (a *App) FlushRecents() {
	r := &a.rec
	r.mu.Lock()
	var s recentsData
	if len(r.pendingNav) > 0 {
		s.Navigation = r.pendingNav
	}
	if len(r.pendingSearch) > 0 {
		s.Search = r.pendingSearch
	}
	l, changed := mergeRecents(r.recorded, s)
	r.recorded, r.pendingNav, r.pendingSearch, r.scheduled = l, nil, nil, false
	if changed {
		r.ver++
	}
	r.mu.Unlock()
	if !changed {
		return
	}
	if a.RecentsReadEnabled() {
		cur, rest := a.recentsPref()
		t, _ := mergeRecents(cur, l)
		a.setRecentsLocal(t, rest)
	}
	a.recordVisitsSoon(l)
}

// recordVisitsSoon is the 60 s leading+trailing throttle around
// recordVisitsByApi: the first sync goes at once, later ones in the
// window once at its end with the latest record.
func (a *App) recordVisitsSoon(d recentsData) {
	r := &a.rec
	r.mu.Lock()
	defer r.mu.Unlock()
	now := r.clock()
	if r.recordTimer == nil && (r.lastRecord.IsZero() || now.Sub(r.lastRecord) >= recentsRecordEvery) {
		r.lastRecord = now
		go a.recordVisits(d)
		return
	}
	dd := d
	r.recordLatest = &dd
	if r.recordTimer == nil {
		wait := recentsRecordEvery - now.Sub(r.lastRecord)
		r.recordTimer = time.AfterFunc(max(wait, 0), a.recordVisitsTrailing)
	}
}

func (a *App) recordVisitsTrailing() {
	r := &a.rec
	r.mu.Lock()
	r.recordTimer = nil
	d := r.recordLatest
	r.recordLatest = nil
	if d != nil {
		r.lastRecord = r.clock()
	}
	r.mu.Unlock()
	if d != nil {
		a.recordVisits(*d)
	}
}

// FlushRecentsSync sends a sync still waiting in the throttle now (tests,
// sign-out).
func (a *App) FlushRecentsSync() {
	r := &a.rec
	r.mu.Lock()
	if r.recordTimer != nil {
		r.recordTimer.Stop()
		r.recordTimer = nil
	}
	d := r.recordLatest
	r.recordLatest = nil
	r.mu.Unlock()
	if d != nil {
		a.recordVisits(*d)
	}
}

// visitTypeOf maps object types to visits.record entity types (XtzY _);
// pending files, triggers and users have none.
var visitTypeOf = map[string]string{
	NavChannel: "channel", NavThread: "thread", NavDM: "dm", NavRecordChannel: "record_channel",
	NavRecordChannelSFDC: "record_channel_sfdc", NavFile: "file", NavPage: "view",
}

type visitOut struct {
	EntityID          string `json:"entity_id"`
	EntityType        string `json:"entity_type"`
	ThreadTS          string `json:"thread_ts,omitempty"`
	ClientEventTimeMS int64  `json:"client_event_time_ms"`
}

// navigationEventsToVisits: nil unless every event maps (a thread needs
// its thread_ts).
func navigationEventsToVisits(e []RecentNavigation) []visitOut {
	out := make([]visitOut, 0, len(e))
	for _, ev := range e {
		t, ok := visitTypeOf[ev.ObjectType]
		if !ok || (t == "thread" && ev.ThreadTS == "") {
			return nil
		}
		v := visitOut{EntityID: ev.ID, EntityType: t, ClientEventTimeMS: ev.Timestamp}
		if t == "thread" {
			v.ThreadTS = ev.ThreadTS
		}
		out = append(out, v)
	}
	if len(out) == 0 {
		return nil
	}
	return out
}

// errVisitsUnavailable is visits.record's restricted_action.
var errVisitsUnavailable = errors.New("visits unavailable")

// recordVisits is recordVisitsByApi {recorded}.
func (a *App) recordVisits(n recentsData) {
	ctx, cancel := context.WithTimeout(a.bg, 30*time.Second)
	defer cancel()
	r := &a.rec
	writePref := func(d recentsData) {
		d = a.withoutForgotten(d)
		if err := a.C.SetPref(ctx, prefRecents, recentsJSON(d, nil)); err != nil {
			return // Slack logs "recents pref write failed"
		}
		if d.Search != nil {
			r.mu.Lock()
			r.writtenSrch, r.writtenOK = d.Search, true
			r.mu.Unlock()
		}
	}
	r.mu.Lock()
	l := n.Navigation
	var f []RecentSearch
	if !r.writtenOK || !slices.Equal(n.Search, r.writtenSrch) {
		f = n.Search
	}
	sameNav := r.acceptedOK && slices.Equal(l, r.acceptedNav)
	latched := r.drains >= recentsLatchDrains && r.clock().Sub(r.lastFailure) < recentsLatchWindow
	busy := r.visitsFlight
	r.mu.Unlock()
	u := navigationEventsToVisits(l)
	switch {
	case u == nil, busy, latched:
		writePref(n) // unmappable, no navigation, latched or in flight
		return
	case sameNav:
		if f != nil {
			writePref(recentsData{Search: f})
		}
		return
	}
	r.mu.Lock()
	r.visitsFlight = true
	r.mu.Unlock()
	err := a.visitsRecord(ctx, u)
	r.mu.Lock()
	r.visitsFlight = false
	switch {
	case err == nil:
		r.drains = 0
		r.acceptedNav, r.acceptedOK = l, true
		r.mu.Unlock()
		if f != nil {
			writePref(recentsData{Search: f})
		}
		return
	case errors.Is(err, errVisitsUnavailable):
		r.drains, r.lastFailure = recentsLatchDrains, r.clock()
	default:
		if r.clock().Sub(r.lastFailure) >= recentsLatchWindow {
			r.drains = 0
		}
		r.drains++
		r.lastFailure = r.clock()
	}
	r.mu.Unlock()
	writePref(n) // fallback
}

// visitsRecord is visits.record {visits, client_now_ms}; an answer with
// errors counts as failed, restricted_action as unavailable.
func (a *App) visitsRecord(ctx context.Context, visits []visitOut) error {
	b, err := json.Marshal(visits)
	if err != nil {
		return err
	}
	a.rec.mu.Lock()
	now := a.rec.clock().UnixMilli()
	a.rec.mu.Unlock()
	var res struct {
		Errors   []json.RawMessage `json:"errors"`
		Checksum string            `json:"checksum"`
	}
	err = a.C.Call(ctx, "visits.record", url.Values{"visits": {string(b)}, "client_now_ms": {strconv.FormatInt(now, 10)}}, &res)
	var se *slack.Error
	if errors.As(err, &se) && se.Code == "restricted_action" {
		return errVisitsUnavailable
	}
	if err != nil {
		return err
	}
	if res.Checksum != "" { // recordOwnVisitsWriteChecksum
		a.rec.mu.Lock()
		a.rec.visitsChecksum = res.Checksum
		a.rec.mu.Unlock()
	}
	if len(res.Errors) > 0 {
		return errors.New("visits.record: errors")
	}
	return nil
}

// ---- reading ----

// foldThreadNavigationEvents: threads read as their conversation, one
// event per id, newest first.
func foldThreadNavigationEvents(e []RecentNavigation) []RecentNavigation {
	if !slices.ContainsFunc(e, func(ev RecentNavigation) bool { return ev.ObjectType == NavThread }) {
		return e
	}
	byID := map[string]RecentNavigation{}
	var order []string
	for _, ev := range e {
		t := ev.ObjectType
		if t == NavThread {
			t = navObjectType(ev.ID)
		}
		if t == "" {
			continue
		}
		if old, ok := byID[ev.ID]; ok && old.Timestamp >= ev.Timestamp {
			continue
		}
		if _, ok := byID[ev.ID]; !ok {
			order = append(order, ev.ID)
		}
		byID[ev.ID] = RecentNavigation{ID: ev.ID, ObjectType: t, Timestamp: ev.Timestamp}
	}
	out := make([]RecentNavigation, 0, len(order))
	for _, id := range order {
		out = append(out, byID[id])
	}
	sort.SliceStable(out, func(i, j int) bool { return out[i].Timestamp > out[j].Timestamp })
	return out
}

// RecentPlacesEvents is getRecentNavigationEventsForRead with recents
// read on: visits.list's rows (else the pref's navigation) with this
// session's recorded navigation upserted, threads folded into their
// conversation, newest first. ok=false with recents read off: Recent
// places then come from local history.
func (a *App) RecentPlacesEvents() ([]RecentNavigation, bool) {
	if !a.RecentsReadEnabled() {
		return nil, false
	}
	a.rec.mu.Lock()
	var n []RecentNavigation
	if a.rec.visitsLoaded {
		n = append(n, a.rec.visits...)
	}
	loaded := a.rec.visitsLoaded
	recorded := append([]RecentNavigation(nil), a.rec.recorded.Navigation...)
	a.rec.mu.Unlock()
	if !loaded {
		d, _ := a.recentsPref()
		n = d.Navigation
	}
	for _, ev := range recorded {
		n = upsertNavigationEvent(n, ev)
	}
	return foldThreadNavigationEvents(n), true
}

// RecentSearches is suggestionsBlankSlateQueryHistory {limit}: with
// recents read on the pref's searches (Salesforce ones left out, newest
// first, each query once), else search.autocomplete's history.
func (a *App) RecentSearches(ctx context.Context, limit int) ([]QueryHistoryItem, error) {
	if limit <= 0 {
		limit = 10
	}
	if a.RecentsReadEnabled() {
		d, _ := a.recentsPref()
		s := append([]RecentSearch(nil), d.Search...)
		sort.SliceStable(s, func(i, j int) bool { return s[i].Timestamp > s[j].Timestamp })
		seen := map[string]bool{}
		var out []QueryHistoryItem
		for _, e := range s {
			if len(out) >= limit {
				break
			}
			if e.Type == RecentSearchSalesforce {
				continue
			}
			bot := e.Type == RecentSearchSlackbot
			k := strconv.FormatBool(bot) + "\x00" + e.Query
			if seen[k] {
				continue
			}
			seen[k] = true
			out = append(out, QueryHistoryItem{Query: e.Query, Slackbot: bot, ThreadTS: e.ThreadTS})
		}
		return out, nil
	}
	hs, err := a.queryHistory(ctx)
	if err != nil {
		return nil, err
	}
	return uniqHistory(hs, limit), nil
}

// uniqHistory is uniqBy(id).slice(0, limit) over the history rows.
func uniqHistory(hs []QueryHistoryItem, limit int) []QueryHistoryItem {
	seen := map[string]bool{}
	var out []QueryHistoryItem
	for _, h := range hs {
		k := strconv.FormatBool(h.Slackbot) + "\x00" + h.Query
		if seen[k] {
			continue
		}
		seen[k] = true
		out = append(out, h)
		if len(out) == limit {
			break
		}
	}
	return out
}

// queryHistory is QueryHistoryCache.get: search.autocomplete {query "",
// limit 100, use_hash}, kept 5 minutes.
func (a *App) queryHistory(ctx context.Context) ([]QueryHistoryItem, error) {
	r := &a.rec
	r.mu.Lock()
	if r.histOK && r.clock().Sub(r.histAt) < queryHistoryTTL {
		out := append([]QueryHistoryItem(nil), r.hist...)
		r.mu.Unlock()
		return out, nil
	}
	r.mu.Unlock()
	var res struct {
		Suggestions []struct {
			Count    *float64 `json:"count"`
			Hash     *string  `json:"hash"`
			Text     *string  `json:"text"`
			TS       *float64 `json:"ts"`
			Type     *string  `json:"type"`
			ThreadTS *string  `json:"slackbot_thread_ts"`
		} `json:"suggestions"`
	}
	err := a.C.Call(ctx, "search.autocomplete", url.Values{"query": {""}, "limit": {"100"}, "use_hash": {"true"}}, &res)
	if err != nil {
		return nil, err
	}
	out := make([]QueryHistoryItem, 0, len(res.Suggestions))
	for _, s := range res.Suggestions {
		// isQueryHistoryCacheSuggestion: count, hash, text, ts and type
		if s.Count == nil || s.Hash == nil || s.Text == nil || s.TS == nil || s.Type == nil {
			continue
		}
		it := QueryHistoryItem{Query: *s.Text, Hash: *s.Hash, Slackbot: *s.Type == "slackbot"}
		if s.ThreadTS != nil {
			it.ThreadTS = *s.ThreadTS
		}
		out = append(out, it)
	}
	r.mu.Lock()
	r.hist, r.histAt, r.histOK = out, r.clock(), true
	r.ver++
	r.mu.Unlock()
	return append([]QueryHistoryItem(nil), out...), nil
}

// flushQueryHistory is QueryHistoryCache.flushAll (after search.save).
func (a *App) flushQueryHistory() {
	a.rec.mu.Lock()
	a.rec.hist, a.rec.histOK = nil, false
	a.rec.ver++
	a.rec.mu.Unlock()
}

// CachedRecentSearches returns the history already in hand (the pref
// with recents read on, else the cached search.autocomplete answer)
// without calling Slack; ok=false when it must be fetched first.
func (a *App) CachedRecentSearches(limit int) ([]QueryHistoryItem, bool) {
	if limit <= 0 {
		limit = 10
	}
	if a.RecentsReadEnabled() {
		out, _ := a.RecentSearches(context.Background(), limit) // the pref: no call
		return out, true
	}
	a.rec.mu.Lock()
	ok := a.rec.histOK && a.rec.clock().Sub(a.rec.histAt) < queryHistoryTTL
	hs := append([]QueryHistoryItem(nil), a.rec.hist...)
	a.rec.mu.Unlock()
	if !ok {
		return nil, false
	}
	return uniqHistory(hs, limit), true
}
