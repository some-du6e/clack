package core

import (
	"context"
	"testing"
	"time"
)

// A Recent searches row's Delete (rs63's query-history toolbar): rZ6M
// searchDelete sends search.delete with the row's hash (its terms without
// one; type slackbot for a Slackbot conversation) and drops the cached
// history; lwKM's forget takes the query out of the local recents pref
// (matched by canonicalSearchQuery, channel names aside) and out of every
// later write, until it is searched again.
func TestSearchDeleteRecent(t *testing.T) {
	a, fs := canvasApp(t)
	ctx := context.Background()
	fs.SetSearchHistory("alpha", "beta")
	hs, err := a.RecentSearches(ctx, 10)
	if err != nil || len(hs) != 2 || hs[0].Query != "beta" || hs[0].Hash == "" {
		t.Fatalf("history %v %+v", err, hs)
	}
	if err := a.DeleteRecentSearch(ctx, hs[0]); err != nil {
		t.Fatal(err)
	}
	forms := fs.SearchForms("search.delete")
	if len(forms) != 1 || forms[0].Get("hash") != hs[0].Hash || forms[0].Has("terms") || forms[0].Has("type") {
		t.Fatalf("search.delete %v", forms)
	}
	if _, ok := a.CachedRecentSearches(10); ok {
		t.Error("the cached history survived search.delete")
	}
	if hs, _ = a.RecentSearches(ctx, 10); len(hs) != 1 || hs[0].Query != "alpha" {
		t.Errorf("history after the delete %+v", hs)
	}
	if err := a.DeleteRecentSearch(ctx, QueryHistoryItem{Query: "ask it", Slackbot: true}); err != nil {
		t.Fatal(err)
	}
	if f := fs.SearchForms("search.delete"); len(f) != 2 || f[1].Get("terms") != "ask it" || f[1].Get("type") != "slackbot" || f[1].Has("hash") {
		t.Errorf("search.delete by terms %v", f)
	}

	if q := canonicalSearchQuery("in:<#C0FAKE00001|general> plan <#C2>"); q != "in:<#C0FAKE00001> plan <#C2>" {
		t.Errorf("canonicalSearchQuery %q", q)
	}

	// recents read on: the local pref loses the query
	old := RecentsFlushDelay
	RecentsFlushDelay = time.Hour // flushed by hand
	t.Cleanup(func() { RecentsFlushDelay = old })
	pref := `{"search":[{"query":"keep","type":"SLACK","timestamp":2},` +
		`{"query":"in:<#C0FAKE00001|general> plan","type":"SLACK","timestamp":1}]}`
	if err := a.C.SetPref(ctx, prefRecents, pref); err != nil {
		t.Fatal(err)
	}
	a.SetExperimentGroup("desktop_search_recents_read", "on") // loads the pref
	waitForms(t, "the recents pref", 2, func() int { hs, _ := a.RecentSearches(ctx, 10); return len(hs) })
	a.ForgetRecentSearch("in:<#C0FAKE00001|renamed> plan")
	hs, _ = a.RecentSearches(ctx, 10)
	if len(hs) != 1 || hs[0].Query != "keep" {
		t.Errorf("local recents after the forget %+v", hs)
	}

	// later writes leave it out; searching for it again brings it back
	a.SetExperimentGroup("desktop_search_recents_write", "on")
	a.rec.mu.Lock()
	a.rec.recorded.Search = []RecentSearch{{Query: "in:<#C0FAKE00001> plan", Type: RecentSearchSlack, Timestamp: 3}}
	a.rec.mu.Unlock()
	a.RecordRecentSearch("fresh", RecentSearchSlack)
	a.FlushRecents()
	a.FlushRecentsSync()
	waitForms(t, "the recents write", 1, func() int {
		for _, s := range recentsPrefOf(t, a).Search {
			if s.Query == "fresh" {
				return 1
			}
		}
		return 0
	})
	for _, s := range recentsPrefOf(t, a).Search {
		if canonicalSearchQuery(s.Query) == "in:<#C0FAKE00001> plan" {
			t.Errorf("a forgotten query was written: %+v", recentsPrefOf(t, a).Search)
		}
	}
	a.RecordRecentSearch("in:<#C0FAKE00001|general> plan", RecentSearchSlack)
	a.rec.mu.Lock()
	gone := a.rec.forgotten["in:<#C0FAKE00001> plan"]
	a.rec.mu.Unlock()
	if gone {
		t.Error("searching for it again didn't unforget it")
	}
}
