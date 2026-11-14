package core

// The window's search filter suggestions (search_suggest.go): Slack's
// parseSuggestions (client-boot rZ6M, build 2c0d02ca130f) merges each new
// search's filter_suggestions into the last ones, the request's own in: /
// from: first. Synthetic ids only.

import (
	"context"
	"strings"
	"testing"

	"github.com/some-du6e/clack/internal/fakeslack"
)

func suggIDs(ss []FilterSuggestion) string {
	var out []string
	for _, s := range ss {
		id := s.ID
		if s.Checked {
			id += "*"
		}
		out = append(out, id)
	}
	return strings.Join(out, ",")
}

// uniqBy(concat(checked, take(uniqBy(sortBy(concat(fresh, prev),
// -facet_count), "id"), 5)), "id"): most results first, ties in order
// (the answer's before the last ones), five at most, the request's own
// first and checked.
func TestSearchSuggestionsMergeLikeSlack(t *testing.T) {
	fs := func(pairs ...any) []FilterSuggestion {
		var out []FilterSuggestion
		for i := 0; i < len(pairs); i += 2 {
			out = append(out, FilterSuggestion{ID: pairs[i].(string), Count: pairs[i+1].(int)})
		}
		return out
	}
	cases := []struct {
		name           string
		checked        []string
		fresh, prev    []FilterSuggestion
		want           string
		wantFirstCount int
	}{
		{"first search", nil, fs("C1", 5, "C2", 3), nil, "C1,C2", 5},
		{"merged by count", nil, fs("C3", 2, "C4", 7), fs("C1", 5, "C2", 3), "C4,C1,C2,C3", 7},
		{"the request's own first", []string{"C3"}, fs("C3", 2, "C4", 7), fs("C1", 5, "C2", 3), "C3*,C4,C1,C2", 0},
		{"five at most", nil, fs("C1", 1, "C2", 2, "C3", 3, "C4", 4, "C5", 5, "C6", 6, "C7", 7), nil, "C7,C6,C5,C4,C3", 7},
		{"ties keep the answer's first", nil, fs("C9", 4), fs("C8", 4), "C9,C8", 4},
		{"one per id (the higher count)", nil, fs("C1", 2), fs("C1", 9, "C2", 1), "C1,C2", 9},
	}
	for _, c := range cases {
		got := suggMerge(c.checked, c.fresh, c.prev)
		if suggIDs(got) != c.want {
			t.Errorf("%s: %s, want %s", c.name, suggIDs(got), c.want)
			continue
		}
		if got[0].Count != c.wantFirstCount {
			t.Errorf("%s: first count %d, want %d", c.name, got[0].Count, c.wantFirstCount)
		}
	}
	in := suggFilterIDs(suggInRef, "in:<#C0AAA0001|general> in:<@U0AAA0002> -in:<#C0AAA0003> deploy")
	from := suggFilterIDs(suggFromRef, "from:<@U0AAA0004> -from:<@U0AAA0005> hi")
	if strings.Join(in, ",") != "C0AAA0001,U0AAA0002" || strings.Join(from, ",") != "U0AAA0004" {
		t.Errorf("request filters: in %v from %v", in, from)
	}
}

// A new messages search (FetchSuggestions) fills the window's
// suggestions from the fake's filter_suggestions; one without it, a DM
// search or a search in a channel leaves them; a page 2 too.
func TestSearchSuggestionsAgainstFake(t *testing.T) {
	a, _ := canvasApp(t)
	ctx := context.Background()
	if in, from := a.SearchSuggestions(); len(in) != 0 || len(from) != 0 {
		t.Fatalf("suggestions before any search: %v %v", in, from)
	}
	if _, err := a.SearchMessages(ctx, SearchOpts{Query: fakeslack.SearchWord}); err != nil {
		t.Fatal(err)
	}
	if in, _ := a.SearchSuggestions(); len(in) != 0 {
		t.Fatalf("a search without fetchSuggestions filled them: %v", in)
	}
	r, err := a.SearchMessages(ctx, SearchOpts{Query: fakeslack.SearchWord, FetchSuggestions: true})
	if err != nil {
		t.Fatal(err)
	}
	in, from := a.SearchSuggestions()
	if len(in) == 0 || len(in) > searchSuggTop || len(from) == 0 {
		t.Fatalf("in %v from %v", in, from)
	}
	if want := suggIDs(suggMerge(nil, r.FilterIn, nil)); suggIDs(in) != want {
		t.Errorf("in %s, want %s", suggIDs(in), want)
	}
	for _, s := range from {
		if s.ID == a.Self().ID {
			t.Errorf("you are a from suggestion")
		}
	}
	if a.SearchMessagesLoading() {
		t.Error("still loading after the answer")
	}
	// a search in one of them: it leads, checked
	ch := in[len(in)-1].ID
	if _, err := a.SearchMessages(ctx, SearchOpts{Query: "in:<#" + ch + "> " + fakeslack.SearchWord, FetchSuggestions: true}); err != nil {
		t.Fatal(err)
	}
	in2, _ := a.SearchSuggestions()
	if len(in2) == 0 || in2[0].ID != ch || !in2[0].Checked {
		t.Errorf("after in:%s: %s", ch, suggIDs(in2))
	}
}
