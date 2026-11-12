package core

// Deleting a recent search: the switcher's suggestion toolbar on a Recent
// searches row (rs63 getSuggestionSecondaryActions, "query-history":
// Copy search query, Delete; CDN build 4d0c78085232).

import (
	"context"
	"net/url"
	"regexp"
)

// srChannelRefRe is xeUe's f, a channel ref with its name.
var srChannelRefRe = regexp.MustCompile(`<#([A-Z0-9]+?)\|[^>]*?>`)

// canonicalSearchQuery is xeUe canonicalSearchQuery: channel refs without
// their names, so one query matches however its channels were named.
func canonicalSearchQuery(q string) string { return srChannelRefRe.ReplaceAllString(q, "<#$1>") }

// ForgetRecentSearch is lwKM's forgetRecentSearchQuery ("Drop a deleted
// search query from the session recents buffer and later writes"): the
// query (canonicalSearchQuery) is left out of every later write of the
// recents pref (4jD3 forgetSearchQuery), with recents read on the local
// pref loses it, and so do the session's record and pending searches.
func (a *App) ForgetRecentSearch(query string) {
	o := canonicalSearchQuery(query)
	kept := func(e RecentSearch) bool { return canonicalSearchQuery(e.Query) != o }
	r := &a.rec
	r.mu.Lock()
	if r.forgotten == nil {
		r.forgotten = map[string]bool{}
	}
	r.forgotten[o] = true
	r.recorded.Search = filterSearches(r.recorded.Search, kept)
	r.pendingSearch = filterSearches(r.pendingSearch, kept)
	r.ver++
	r.mu.Unlock()
	if a.RecentsReadEnabled() {
		cur, rest := a.recentsPref()
		if n := filterSearches(cur.Search, kept); len(n) != len(cur.Search) {
			cur.Search = n
			a.setRecentsLocal(cur, rest)
		}
	}
}

// DeleteRecentSearch is rZ6M searchDelete: search.delete with the row's
// hash (search.autocomplete's history) or its terms, type "slackbot" for
// a Slackbot conversation; the QueryHistoryCache is dropped either way.
func (a *App) DeleteRecentSearch(ctx context.Context, it QueryHistoryItem) error {
	v := url.Values{}
	if it.Hash != "" {
		v.Set("hash", it.Hash)
	} else {
		v.Set("terms", it.Query)
	}
	if it.Slackbot {
		v.Set("type", "slackbot")
	}
	err := a.C.Call(ctx, "search.delete", v, nil)
	a.flushQueryHistory()
	return err
}

// filterSearches is filterKeepingIdentity: s itself when nothing goes.
func filterSearches(s []RecentSearch, keep func(RecentSearch) bool) []RecentSearch {
	n := 0
	for _, e := range s {
		if keep(e) {
			n++
		}
	}
	if n == len(s) {
		return s
	}
	out := make([]RecentSearch, 0, n)
	for _, e := range s {
		if keep(e) {
			out = append(out, e)
		}
	}
	return out
}

// withoutForgotten is 4jD3's persist filter: the searches minus the
// forgotten queries.
func (a *App) withoutForgotten(d recentsData) recentsData {
	a.rec.mu.Lock()
	defer a.rec.mu.Unlock()
	f := a.rec.forgotten
	if len(f) == 0 || d.Search == nil {
		return d
	}
	d.Search = filterSearches(d.Search, func(e RecentSearch) bool { return !f[canonicalSearchQuery(e.Query)] })
	return d
}
