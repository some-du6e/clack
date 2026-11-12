package core

// gap-w24: the messages sort's server override, from Slack's CDN build
// fa938a086569. client-boot rZ6M getSearchParams sends
// disable_sort_override when the window's disableSortOverride is on (the
// sort select's disableSortOverride thunk, dispatched when Most relevant
// is picked), as search in channel (O8Qt) does from its sort handler; the
// messages response's sort_forced_to_recent (only a JSON true) is the
// module's sortForcedToRecent. Fake Slack only.

import (
	"context"
	"net/url"
	"testing"

	"github.com/some-du6e/clack/internal/fakeslack"
)

func TestSearchSortOverrideParams(t *testing.T) {
	s := NewSearchSession()
	for _, c := range []struct {
		name string
		o    SearchOpts
		want string
	}{
		{"results page, off", SearchOpts{Query: "test", Session: s}, ""},
		{"results page, on", SearchOpts{Query: "test", Session: s, DisableSortOverride: true}, "true"},
		{"in channel, off", SearchOpts{Query: "test", InChannel: "C1", Session: s}, ""},
		{"in channel, on", SearchOpts{Query: "test", InChannel: "C1", Session: s, DisableSortOverride: true}, "true"},
	} {
		p := messageSearchParams(c.o, false, false)
		if _, has := p["disable_sort_override"]; has != (c.want != "") || p.Get("disable_sort_override") != c.want {
			t.Errorf("%s: disable_sort_override %q (sent %v), want %q", c.name, p.Get("disable_sort_override"), has, c.want)
		}
	}
	// the DMs module never sends it (getDMSearchApiArguments)
	if p := messageSearchParams(SearchOpts{Query: "test", Module: ModDMs, Session: s, DisableSortOverride: true}, false, false); p.Has("disable_sort_override") {
		t.Error("dms sent disable_sort_override")
	}
}

func TestSearchSortForcedToRecent(t *testing.T) {
	a, fs := canvasApp(t)
	ctx := context.Background()
	for _, c := range []struct {
		raw    any
		module string
		want   bool
	}{
		{true, ModMessages, true},
		{false, ModMessages, false},
		{"true", ModMessages, false}, // !0 === a.sort_forced_to_recent
		{1, ModMessages, false},
		{nil, ModMessages, false},
		{true, ModDMs, false}, // only the messages module's state reads it
	} {
		raw := c.raw
		fs.SetHook(func(method string, f url.Values) (any, bool) {
			if method != "search.modules.messages" && method != "search.modules.dms" {
				return nil, false
			}
			out := map[string]any{"ok": true, "items": []any{}, "pagination": map[string]any{"total_count": 0, "page": 1, "page_count": 0}}
			if raw != nil {
				out["sort_forced_to_recent"] = raw
			}
			return out, true
		})
		r, err := a.SearchMessages(ctx, SearchOpts{Query: fakeslack.SearchWord, Module: c.module})
		if err != nil {
			t.Fatalf("%v (%s): %v", c.raw, c.module, err)
		}
		if r.SortForcedToRecent != c.want {
			t.Errorf("sort_forced_to_recent %v on %s: %v, want %v", c.raw, c.module, r.SortForcedToRecent, c.want)
		}
	}
	fs.SetHook(nil)
}
