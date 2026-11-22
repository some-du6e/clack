package core

import (
	"testing"

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
