package core

import (
	"context"
	"strings"
	"testing"
	"time"

	"github.com/some-du6e/clack/internal/slack"
)

// The Files browser's advanced filters (the Filters modal's From, In and
// Date) reach search.modules.files as tokens of the query, in the order
// Slack's browser writes them. Every expected query is Slack's own output:
// client-boot nzlP querySuggestionFromQueryAndFilter (build 8f19be21ed7e)
// run in node over the same filters, with the token classes' toString from
// pYM5 (MemberFilterQuerySuggestionToken, InQuerySuggestionToken,
// DateQuerySuggestionToken, TypeQuerySuggestionToken) and the store
// testApp seeds (C1 "general", D1 the DM with U1); docs/FILES-AUDIT.md.
func TestFilesViewAdvancedFiltersQuery(t *testing.T) {
	a, f := testApp(t)
	a.S.PutConv(slack.Conversation{ID: "C2", Name: "a>b", IsChannel: true})
	a.S.PutConv(slack.Conversation{ID: "G1", Name: "mpdm-a--b-1", IsMPIM: true, IsGroup: true})
	f.reply["search.modules.files"] = `{"ok":true,"items":[],"pagination":{"total_count":0,"page":1,"page_count":1}}`
	const all = "type:lists type:quip type:documents type:spreadsheets type:presentations type:pdfs type:folder_skill_set"
	for _, c := range []struct {
		name string
		o    FilesOpts
		want string
	}{
		{"from", FilesOpts{From: []string{"U2"}}, "from:<@U2> " + all},
		{"query-from-in-dates", FilesOpts{Query: " roadmap ", From: []string{"U1", "U2"}, In: []string{"C1"},
			Before: "2026-11-22", After: "2026-11-13"},
			"roadmap from:<@U1> from:<@U2> in:<#C1|general> before:2026-11-22 after:2026-11-13 " + all},
		// a DM is its member, a group DM bare, a person <@U…>, a channel
		// name without ">"
		{"in-kinds", FilesOpts{Kind: "lists", In: []string{"D1", "G1", "U2", "C2"}}, "in:<@U1> in:<#G1> in:<@U2> in:<#C2|ab> type:lists"},
		{"mine-from-after", FilesOpts{Mine: true, From: []string{"U2"}, After: "2026-10-01"},
			"from:<@U2> after:2026-10-01 type:lists type:quip type:documents creator:<@USELF>"},
		// the date menu's Any time: before tomorrow
		{"any-time", FilesOpts{Kind: "canvases", Before: "2026-11-22"}, "before:2026-11-22 type:quip"},
		{"query-only", FilesOpts{Query: "plan"}, "plan " + all},
		{"lists-query", FilesOpts{Kind: "lists", Query: "roadmap"}, "roadmap type:lists"},
	} {
		if _, err := a.Files(t.Context(), c.o); err != nil {
			t.Fatal(err)
		}
		if got := formVal(f.last("search.modules.files"), "query"); got != c.want {
			t.Errorf("%s:\n got %q\nwant %q", c.name, got, c.want)
		}
	}
	// the filters leave the request's params alone (getSearchAPIParams'
	// files branch reads no from/in/dates)
	if _, err := a.Files(t.Context(), FilesOpts{From: []string{"U2"}, After: "2026-10-01", Sort: "timestamp"}); err != nil {
		t.Fatal(err)
	}
	p := f.last("search.modules.files")
	if formVal(p, "sort") != "timestamp" || formVal(p, "search_context") != "desktop_files_browser" || formVal(p, "shared_with_you") != "0" ||
		formVal(p, "search_only_my_channels") != "false" || formVal(p, "from") != "" || formVal(p, "after") != "" {
		t.Errorf("params %v", p)
	}
}

// The typed query goes through Slack's tokenizer first (pYM5
// tokenListFromString): a typed token of a filter's kind stays only when
// the filter holds the same value, mentions resolve to ids, unknown names
// stay as typed, and words join with one space. Expected values are
// Slack's: nzlP over pYM5's rawTokensFromString / tokenListFromString and
// the tokens' fromString, run in node on the same store (testApp: U1
// alice, U2 bob, C1 general, USELF me).
func TestFilesViewTypedTokensQuery(t *testing.T) {
	a, f := testApp(t)
	f.reply["search.modules.files"] = `{"ok":true,"items":[],"pagination":{"total_count":0,"page":1,"page_count":1}}`
	lists := []string{"lists"}
	for _, c := range []struct {
		name string
		o    FilesOpts
		want string
	}{
		// a typed date of a kind the filters set gives way to theirs
		{"typed-before-with-date", FilesOpts{Types: lists, Query: "plan before:2026-01-01", Before: "2026-11-22", After: "2026-11-14"},
			"plan before:2026-11-22 after:2026-11-14 type:lists"},
		{"typed-before-same", FilesOpts{Types: lists, Query: "before:2026-11-22 plan", Before: "2026-11-22", After: "2026-11-14"},
			"before:2026-11-22 plan after:2026-11-14 type:lists"},
		// every pass runs, so a known person typed with no From is dropped
		{"typed-from-no-filter", FilesOpts{Types: lists, Query: "from:@alice report"}, "report type:lists"},
		{"typed-from-unknown", FilesOpts{Types: lists, Query: "from:@nobody report"}, "from:@nobody report type:lists"},
		{"typed-from-kept", FilesOpts{Types: lists, Query: "from:<@U1> report", From: []string{"U1", "U2"}},
			"from:<@U1> report from:<@U2> type:lists"},
		{"typed-in", FilesOpts{Types: lists, Query: "in:#general in:#nowhere notes", In: []string{"C1"}},
			"in:<#C1|general> notes type:lists"},
		{"typed-type-has-is", FilesOpts{Types: []string{"lists", "quip"}, Query: "type:pdfs type:lists has:link is:thread is:bogus -has:star"},
			"type:lists is:bogus -has:star type:quip"},
		{"mentions-phrases", FilesOpts{Types: lists, Query: `@alice   "q3 plan" <#C1> x  y`}, `<@U1> "q3 plan" <#C1|general> x y type:lists`},
		{"creator-me", FilesOpts{Mine: true, Query: "creator:me draft"}, "creator:<@USELF> draft type:lists type:quip type:documents"},
		{"empty-values", FilesOpts{Types: lists, Query: "from: in: before: to:@alice"}, "from: in: before: to:@alice type:lists"},
	} {
		if _, err := a.Files(t.Context(), c.o); err != nil {
			t.Fatal(err)
		}
		if got := formVal(f.last("search.modules.files"), "query"); got != c.want {
			t.Errorf("%s:\n got %q\nwant %q", c.name, got, c.want)
		}
	}
}

// The list pickers (SearchLists: useListExplorer) resolve typed tokens
// against the store too: from:, with: and in: go, @bob becomes <@U2>.
// Expected: Slack's tokenizer and nzlP in node on testApp's store.
func TestFilesViewListPickerTokens(t *testing.T) {
	a, f := testApp(t)
	f.reply["search.modules.files"] = `{"ok":true,"items":[],"pagination":{"total_count":0,"page":1,"page_count":1}}`
	if _, _, err := a.SearchLists(t.Context(), "from:@alice @bob with:@bob in:#general plan", 1); err != nil {
		t.Fatal(err)
	}
	if got := formVal(f.last("search.modules.files"), "query"); got != "<@U2> plan type:lists" {
		t.Errorf("query %q", got)
	}
}

// A 30 KB paste into the Files search tokenizes in one pass over it (the
// token regex at an advancing offset, the name indexes built once), well
// inside a second; a cancelled ctx stops the request.
func TestFilesViewQueryLongPaste(t *testing.T) {
	a, f := testApp(t)
	f.reply["search.modules.files"] = `{"ok":true,"items":[],"pagination":{"total_count":0,"page":1,"page_count":1}}`
	const unit = `from:@alice report "q3 plan" @bob in:#general before:2026-01-01 type:pdfs word `
	var b strings.Builder
	n := 0
	for b.Len() < 30<<10 {
		b.WriteString(unit)
		n++
	}
	t0 := time.Now()
	if _, err := a.Files(t.Context(), FilesOpts{Query: b.String()}); err != nil {
		t.Fatal(err)
	}
	if d := time.Since(t0); d > time.Second {
		t.Errorf("a %d-byte query took %v", b.Len(), d)
	}
	q := formVal(f.last("search.modules.files"), "query")
	if strings.Contains(q, "from:") || strings.Contains(q, "in:") || strings.Contains(q, "before:") ||
		strings.Count(q, "<@U2>") != n || strings.Count(q, "type:pdfs") != 1 || !strings.HasSuffix(q, "type:folder_skill_set") {
		t.Errorf("long query's tokens: %d bytes, %d <@U2>, %d type:pdfs, ends %q", len(q), strings.Count(q, "<@U2>"),
			strings.Count(q, "type:pdfs"), q[max(0, len(q)-40):])
	}
	ctx, cancel := context.WithCancel(t.Context())
	cancel()
	if _, err := a.Files(ctx, FilesOpts{Query: b.String()}); err == nil {
		t.Error("a cancelled search went out")
	}
}
