package core

import (
	"context"
	"encoding/json"
	"fmt"
	"net/url"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/some-du6e/clack/internal/fakeslack"
	"github.com/some-du6e/clack/internal/slack"
)

// Slack's merge rules for the recents record (lwKM): one event per
// target, the newer kept; at most 40 navigation events with at most 10
// threads; one search per query and type, at most 100; newest first.
func TestSearchRecentsMerge(t *testing.T) {
	var nav []RecentNavigation
	for i := 0; i < 15; i++ {
		nav = upsertNavigationEvent(nav, RecentNavigation{ID: "C0FAKE00001", ObjectType: NavThread, ThreadTS: fmt.Sprintf("1.%d", i), Timestamp: int64(100 + i)})
	}
	for i := 0; i < 45; i++ {
		nav = upsertNavigationEvent(nav, RecentNavigation{ID: fmt.Sprintf("C0FAKE%05d", i), ObjectType: NavChannel, Timestamp: int64(200 + i)})
	}
	threads := 0
	for i, ev := range nav {
		if ev.ObjectType == NavThread {
			threads++
		}
		if i > 0 && nav[i-1].Timestamp < ev.Timestamp {
			t.Fatalf("not newest first at %d", i)
		}
	}
	if len(nav) != 40 || threads != 10 {
		// the cap keeps the 10 newest threads however old, then fills to
		// 40 with the newest of the rest
		t.Errorf("navigation %d events, %d threads", len(nav), threads)
	}
	nav = nil
	for i := 0; i < 15; i++ {
		nav = upsertNavigationEvent(nav, RecentNavigation{ID: "C0FAKE00001", ObjectType: NavThread, ThreadTS: fmt.Sprintf("1.%d", i), Timestamp: int64(300 + i)})
	}
	for i := 0; i < 45; i++ {
		nav = upsertNavigationEvent(nav, RecentNavigation{ID: fmt.Sprintf("C0FAKE%05d", i), ObjectType: NavChannel, Timestamp: int64(200 + i)})
	}
	threads = 0
	for _, ev := range nav {
		if ev.ObjectType == NavThread {
			threads++
		}
	}
	if len(nav) != 40 || threads != 10 {
		t.Errorf("newer threads: %d events, %d threads (want 40 with 10 threads)", len(nav), threads)
	}
	// an older visit of the same target changes nothing
	n2 := upsertNavigationEvent(nav, RecentNavigation{ID: "C0FAKE00044", ObjectType: NavChannel, Timestamp: 1})
	if len(n2) != len(nav) || n2[len(n2)-1] != nav[len(nav)-1] {
		t.Error("an older event replaced a newer one")
	}

	var s []RecentSearch
	s = upsertSearchEvent(s, RecentSearch{Query: "zephyr", Type: RecentSearchSlack, Timestamp: 10})
	s = upsertSearchEvent(s, RecentSearch{Query: "zephyr", Type: RecentSearchSlackbot, Timestamp: 11, ThreadTS: "9.1"})
	s = upsertSearchEvent(s, RecentSearch{Query: "zephyr", Type: RecentSearchSlack, Timestamp: 12})
	s = upsertSearchEvent(s, RecentSearch{Query: "zephyr", Type: RecentSearchSlackbot, Timestamp: 13})
	if len(s) != 2 || s[0].Timestamp != 13 || s[0].ThreadTS != "9.1" || s[1].Type != RecentSearchSlack || s[1].Timestamp != 12 {
		t.Errorf("searches %+v", s)
	}
	for i := 0; i < 120; i++ {
		s = upsertSearchEvent(s, RecentSearch{Query: fmt.Sprint("q", i), Type: RecentSearchSlack, Timestamp: int64(100 + i)})
	}
	if len(s) != 100 || s[0].Query != "q119" {
		t.Errorf("%d searches, newest %q", len(s), s[0].Query)
	}

	for id, want := range map[string]string{"C0FAKE00001": NavChannel, "G0FAKE00001": NavChannel, "D0FAKE00001": NavDM,
		"Z0FAKE00001": NavRecordChannel, "F0FAKESRCH1": NavFile, "Ft0FAKE0001": NavTrigger, "U0FAKE00001": NavUser,
		"Pactivity": NavPage, "Vall_threads": NavPage, "S:a:b": NavRecordChannelSFDC, "Pdms": "", "nope": ""} {
		if got := navObjectType(id); got != want {
			t.Errorf("navObjectType(%q) = %q, want %q", id, got, want)
		}
	}
}

func waitForms(t *testing.T, what string, n int, get func() int) {
	t.Helper()
	for end := time.Now().Add(10 * time.Second); get() < n; {
		if time.Now().After(end) {
			t.Fatalf("%s: %d calls, want %d", what, get(), n)
		}
		time.Sleep(10 * time.Millisecond)
	}
}

// recentsPrefSets is the users.prefs.set calls for "recents" the fake saw.
func recentsPrefOf(t *testing.T, a *App) recentsData {
	t.Helper()
	var r struct {
		Prefs map[string]json.RawMessage `json:"prefs"`
	}
	if err := a.C.Call(context.Background(), "users.prefs.get", nil, &r); err != nil {
		t.Fatal(err)
	}
	var s string
	_ = json.Unmarshal(r.Prefs[prefRecents], &s)
	var d struct {
		Navigation []RecentNavigation `json:"navigation"`
		Search     []RecentSearch     `json:"search"`
	}
	if s != "" {
		if err := json.Unmarshal([]byte(s), &d); err != nil {
			t.Fatalf("recents pref %q: %v", s, err)
		}
	}
	return recentsData{Navigation: d.Navigation, Search: d.Search}
}

// desktop_search_recents_write: off (not assigned), nothing is recorded;
// on, navigation goes to visits.record and searches to the recents pref,
// once a minute at most; forced off again, nothing more.
func TestSearchRecentsWriteGroups(t *testing.T) {
	a, fs := canvasApp(t)
	old := RecentsFlushDelay
	RecentsFlushDelay = time.Hour // the tests flush by hand
	t.Cleanup(func() { RecentsFlushDelay = old })
	now := time.Date(2026, 11, 10, 12, 0, 0, 0, time.UTC)
	a.SetRecentsClock(func() time.Time { return now })

	if a.Experiment("desktop_search_recents_write") != "" {
		t.Fatalf("the fake account isn't assigned desktop_search_recents_write")
	}
	a.RecordRecentNavigation("C0FAKE00001", "")
	a.RecordRecentSearch("zephyr", RecentSearchSlack)
	a.FlushRecents()
	a.FlushRecentsSync()
	if n := len(fs.VisitForms("visits.record")); n != 0 {
		t.Errorf("not assigned: %d visits.record calls", n)
	}

	a.SetExperimentGroup("desktop_search_recents_write", "on")
	a.RecordRecentNavigation("C0FAKE00001", "")
	a.RecordRecentNavigation("D0FAKE00001", "")
	a.RecordRecentNavigation("C0FAKE00002", "1762500000.000100") // a thread
	a.RecordRecentNavigation("Pactivity", "")
	a.RecordRecentNavigation("nope", "") // not a place: dropped
	a.RecordRecentSearch("zephyr", RecentSearchSlack)
	a.FlushRecents()
	waitForms(t, "visits.record", 1, func() int { return len(fs.VisitForms("visits.record")) })
	f := fs.VisitForms("visits.record")[0]
	if f.Get("client_now_ms") != fmt.Sprint(now.UnixMilli()) {
		t.Errorf("client_now_ms %q", f.Get("client_now_ms"))
	}
	var visits []map[string]any
	if err := json.Unmarshal([]byte(f.Get("visits")), &visits); err != nil || len(visits) != 4 {
		t.Fatalf("visits %q: %v", f.Get("visits"), err)
	}
	types := map[string]string{}
	for _, v := range visits {
		types[v["entity_id"].(string)] = v["entity_type"].(string)
		if v["client_event_time_ms"] != float64(now.UnixMilli()) {
			t.Errorf("visit %v: client_event_time_ms", v)
		}
	}
	if types["C0FAKE00001"] != "channel" || types["D0FAKE00001"] != "dm" || types["C0FAKE00002"] != "thread" || types["Pactivity"] != "view" {
		t.Errorf("visit types %v", types)
	}
	// the searches changed: written to the pref after visits.record took the navigation
	waitForms(t, "recents pref", 1, func() int { return len(recentsPrefOf(t, a).Search) })
	if d := recentsPrefOf(t, a); d.Search[0].Query != "zephyr" || d.Search[0].Type != RecentSearchSlack || d.Navigation != nil {
		t.Errorf("recents pref %+v (navigation goes to visits.record)", d)
	}

	// within the minute: held, then sent once with the latest record
	a.RecordRecentSearch("launch plan", RecentSearchSlack)
	a.FlushRecents()
	time.Sleep(50 * time.Millisecond)
	if d := recentsPrefOf(t, a); len(d.Search) != 1 {
		t.Errorf("throttled sync went out early: %+v", d.Search)
	}
	a.FlushRecentsSync()
	if d := recentsPrefOf(t, a); len(d.Search) != 2 || d.Search[0].Query != "launch plan" {
		t.Errorf("trailing sync: %+v", d.Search)
	}
	if n := len(fs.VisitForms("visits.record")); n != 1 {
		t.Errorf("unchanged navigation was sent again (%d visits.record calls)", n)
	}

	a.SetExperimentGroup("desktop_search_recents_write", "off")
	a.RecordRecentSearch("ignored", RecentSearchSlack)
	a.FlushRecents()
	a.FlushRecentsSync()
	for _, s := range recentsPrefOf(t, a).Search {
		if s.Query == "ignored" {
			t.Error("recorded with recents write off")
		}
	}
}

// desktop_search_recents_read: off (not assigned), Recent searches are
// search.autocomplete's history (cached, refetched after search.save) and
// Recent places stay local; on, both come from the recents pref and
// visits.list, with this session's navigation merged in.
// Boot's recents load and a forced group's load them once between them.
// Boot's MaybeLoadRecents runs on its own goroutine, which can start after
// a forced group turned recents read on and started that load: both
// loading made TestSearchRecentsReadGroups see two visits.list calls.
func TestRecentsLoadOnce(t *testing.T) {
	a, fs := canvasApp(t)
	ctx := context.Background()
	fs.SetVisits(fakeslack.Visit{EntityID: "C0FAKE00001", EntityType: "channel", LastMS: 300})
	// the forced group's visits.list waits until boot's trigger has run
	gate := make(chan struct{})
	var once sync.Once
	release := func() { once.Do(func() { close(gate) }) }
	t.Cleanup(release)
	var calls atomic.Int32
	fs.SetHook(func(method string, _ url.Values) (any, bool) {
		if method == "visits.list" && calls.Add(1) == 1 {
			<-gate
		}
		return nil, false
	})
	t.Cleanup(func() { fs.SetHook(nil) })
	a.SetExperimentGroup("desktop_search_recents_read", "on") // loads the recents
	waitForms(t, "the forced group's visits.list", 1, func() int { return int(calls.Load()) })
	a.MaybeLoadRecents(ctx) // boot's trigger, running late
	if n := calls.Load(); n != 1 {
		t.Errorf("visits.list called %d times: boot's load ran after the forced group's", n)
	}
	release()
	waitForms(t, "the visits", 1, func() int {
		if evs, _ := a.RecentPlacesEvents(); len(evs) > 0 {
			return 1
		}
		return 0
	})
}

func TestSearchRecentsReadGroups(t *testing.T) {
	a, fs := canvasApp(t)
	ctx := context.Background()
	fs.SetSearchHistory("alpha", "beta")
	hs, err := a.RecentSearches(ctx, 10)
	if err != nil || len(hs) != 2 || hs[0].Query != "beta" || hs[1].Query != "alpha" || hs[0].Hash == "" {
		t.Fatalf("search.autocomplete history: %v %+v", err, hs)
	}
	forms := fs.SearchForms("search.autocomplete")
	f := forms[len(forms)-1]
	f.Del("token")
	wantForm(t, "search.autocomplete", f, map[string]string{"query": "", "limit": "100", "use_hash": "true"})
	if _, _ = a.RecentSearches(ctx, 10); len(fs.SearchForms("search.autocomplete")) != len(forms) {
		t.Error("history not cached")
	}
	if c, ok := a.CachedRecentSearches(10); !ok || len(c) != 2 {
		t.Errorf("cached history %v %+v", ok, c)
	}
	if err := a.SaveSearch(ctx, "gamma"); err != nil {
		t.Fatal(err)
	}
	if _, ok := a.CachedRecentSearches(10); ok {
		t.Error("search.save didn't drop the cached history")
	}
	if hs, _ = a.RecentSearches(ctx, 10); len(hs) != 3 || hs[0].Query != "gamma" {
		t.Errorf("after search.save: %+v", hs)
	}
	if _, ok := a.RecentPlacesEvents(); ok {
		t.Error("recents read off: Recent places must stay local")
	}

	pref := `{"navigation":[{"id":"C0FAKE00003","object_type":"CHANNEL","timestamp":5}],` +
		`"search":[{"query":"older","type":"SLACK","timestamp":1},{"query":"sf","type":"SALESFORCE","timestamp":9},` +
		`{"query":"newer","type":"SLACK","timestamp":7},{"query":"ask","type":"SLACKBOT","timestamp":3,"thread_ts":"5.5"}],"other":1}`
	if err := a.C.SetPref(ctx, prefRecents, pref); err != nil {
		t.Fatal(err)
	}
	fs.SetVisits(fakeslack.Visit{EntityID: "C0FAKE00001", EntityType: "channel", LastMS: 300},
		fakeslack.Visit{EntityID: "C0FAKE00002", EntityType: "thread", LastMS: 400, ThreadTS: "1.1"},
		fakeslack.Visit{EntityID: "D0FAKE00001", EntityType: "dm", LastMS: 200},
		fakeslack.Visit{EntityID: "Pactivity", EntityType: "view", LastMS: 100},
		fakeslack.Visit{EntityID: "Ft0FAKE0001", EntityType: "trigger", LastMS: 50})
	a.SetExperimentGroup("desktop_search_recents_read", "on") // loads the pref and visits.list (once)
	waitForms(t, "the recents load", 1, func() int {
		if evs, _ := a.RecentPlacesEvents(); len(evs) > 1 {
			return 1
		}
		return 0
	})
	hs, _ = a.RecentSearches(ctx, 10)
	var qs []string
	for _, h := range hs {
		qs = append(qs, h.Query)
	}
	if strings.Join(qs, ",") != "newer,ask,older" || !hs[1].Slackbot || hs[1].ThreadTS != "5.5" {
		t.Errorf("recents pref searches %q %+v", qs, hs)
	}
	evs, ok := a.RecentPlacesEvents()
	var ids []string
	for _, e := range evs {
		ids = append(ids, e.ID+"/"+e.ObjectType)
	}
	if !ok || strings.Join(ids, ",") != "C0FAKE00002/CHANNEL,C0FAKE00001/CHANNEL,D0FAKE00001/DM,Pactivity/PAGE" {
		t.Errorf("Recent places from visits.list: %v", ids)
	}
	if f := fs.VisitForms("visits.list"); len(f) != 1 || f[0].Get("limit") != "40" {
		t.Errorf("visits.list %v", f)
	}
	// an unchanged checksum keeps the store
	_ = a.LoadVisits(ctx)
	if f := fs.VisitForms("visits.list"); len(f) != 2 || f[1].Get("checksum") == "" {
		t.Errorf("second visits.list without the checksum: %v", f)
	}

	// this session's navigation merges in, newest first
	old := RecentsFlushDelay
	RecentsFlushDelay = time.Hour
	t.Cleanup(func() { RecentsFlushDelay = old })
	a.SetExperimentGroup("desktop_search_recents_write", "on")
	a.RecordRecentNavigation("C0FAKE00004", "")
	a.RecordRecentSearch("fresh", RecentSearchSlack)
	a.FlushRecents()
	waitForms(t, "recents sync", 1, func() int {
		n := 0
		for _, x := range recentsPrefOf(t, a).Search {
			if x.Query == "fresh" {
				n++
			}
		}
		return n
	})
	if evs, _ = a.RecentPlacesEvents(); len(evs) == 0 || evs[0].ID != "C0FAKE00004" {
		t.Errorf("recorded navigation not first: %+v", evs)
	}
	if hs, _ = a.RecentSearches(ctx, 10); len(hs) == 0 || hs[0].Query != "fresh" {
		t.Errorf("recorded search not in the local pref: %+v", hs)
	}
	if d, rest := a.recentsPref(); string(rest["other"]) != "1" || len(d.Search) != 5 {
		t.Errorf("the local pref lost keys or searches: %v %+v", rest, d.Search)
	}

	// Slack's "search_recents" pref update reloads it
	if err := a.C.SetPref(ctx, prefRecents, `{"search":[{"query":"from another device","type":"SLACK","timestamp":99999999999999}]}`); err != nil {
		t.Fatal(err)
	}
	a.handle(slack.Event{Type: "search_recents", Raw: json.RawMessage(`{"type":"search_recents","value":"updated"}`)})
	if hs, _ = a.RecentSearches(ctx, 10); len(hs) == 0 || hs[0].Query != "from another device" {
		t.Errorf("search_recents update not reloaded: %+v", hs)
	}
}
