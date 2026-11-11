package core

import (
	"context"
	"reflect"
	"testing"

	"github.com/some-du6e/clack/internal/fakeslack"
	"github.com/some-du6e/clack/internal/slack"
	"github.com/some-du6e/clack/internal/store"
)

func filterIDs(out []FilterSection) map[string][]string {
	m := map[string][]string{}
	for _, s := range out {
		m[s.ID] = s.Rows
	}
	return m
}

// The filter (B7kx tD) over a sidebar like the test account's: bFRk's
// match, sections without a match dropped, the rest keeping their order.
func TestSidebarTextFilterMatches(t *testing.T) {
	secs := []FilterSection{
		{ID: "S1", Type: "stars", Rows: []string{"C1"}},
		{ID: "S2", Type: "channels", Rows: []string{"C2", "C3", "C4"}},
		{ID: "S3", Type: "direct_messages", Rows: []string{"D1", "G1"}},
	}
	n := FilterNames{Names: map[string]string{
		"C1": "hack-night", "C2": "bot-tests", "C3": "clack", "C4": "lounge",
		"D1": "Robin Example", "G1": "ann,bob,cat",
	}, Members: map[string][]string{"G1": {"ann", "bob", "cat"}}}
	if FilterSidebar(secs, "  ", false, false, n) != nil {
		t.Error("a blank filter filters")
	}
	cases := []struct {
		q    string
		want map[string][]string
	}{
		{"hack night", map[string][]string{"S1": {"C1"}}},                    // spaces and dashes don't count
		{"HACKNIGHT", map[string][]string{"S1": {"C1"}}},                     // nor case
		{"t", map[string][]string{"S1": {"C1"}, "S2": {"C2"}, "S3": {"G1"}}}, // substring anywhere
		{"robinex", map[string][]string{"S3": {"D1"}}},
		{"ann,bob", map[string][]string{"S3": {"G1"}}}, // the joined names
		{"zzz", map[string][]string{}},
	}
	for _, c := range cases {
		got := FilterSidebar(secs, c.q, false, false, n)
		if got == nil || !reflect.DeepEqual(filterIDs(got), c.want) {
			t.Errorf("%q: %v, want %v", c.q, filterIDs(got), c.want)
		}
	}
	got := FilterSidebar(secs, "t", false, false, n)
	if len(got) != 3 || got[0].ID != "S1" || got[1].ID != "S2" || got[2].ID != "S3" {
		t.Errorf("sections out of order: %v", got)
	}
	if got := FilterSidebar(secs, "zzz", false, false, n); got == nil || len(got) != 0 {
		t.Errorf("no match should be empty, not nil: %v", got)
	}
}

// sidebar_text_filter_m3 (Fyfu createSidebarFuzzyFilter): word starts
// match fuzzily, dashed words match inside a name, and a group DM matches
// when every word of the query matches one member.
func TestSidebarTextFilterM3(t *testing.T) {
	secs := []FilterSection{
		{ID: "S2", Type: "channels", Rows: []string{"C1", "C2"}},
		{ID: "S3", Type: "direct_messages", Rows: []string{"G1", "D1"}},
	}
	n := FilterNames{Names: map[string]string{"C1": "hack-club-events", "C2": "lounge", "G1": "ann lee,bob ray", "D1": "Robin Example"},
		Members: map[string][]string{"G1": {"ann lee", "bob ray"}}}
	for _, c := range []struct {
		q       string
		m3, hit bool
		id      string
	}{
		{"hce", true, true, "C1"},     // h(ack) c(lub) e(vents): word boundaries
		{"hce", false, false, "C1"},   // bFRk: not a substring
		{"club-ev", true, true, "C1"}, // dashed words inside the name
		{"ann bob", true, true, "G1"}, // each word one member
		{"ann bob", false, false, "G1"},
		{"ann zed", true, false, "G1"},
		{"@bob", true, true, "G1"}, // a leading @ is dropped
		{"rob ex", true, true, "D1"},
	} {
		got := filterIDs(FilterSidebar(secs, c.q, false, c.m3, n))
		hit := false
		for _, rows := range got {
			for _, id := range rows {
				hit = hit || id == c.id
			}
		}
		if hit != c.hit {
			t.Errorf("%q (m3 %v): %s matched %v, want %v (%v)", c.q, c.m3, c.id, hit, c.hit, got)
		}
	}
}

// sidebar_text_filter_m2: closed DMs whose names match join Direct
// messages, everything there by read cursor (newest first) then name; with
// no open DM matching, Direct messages comes last.
func TestSidebarTextFilterM2ClosedDMs(t *testing.T) {
	secs := []FilterSection{
		{ID: "S3", Type: "direct_messages", Rows: []string{"D1", "D2"}},
		{ID: "S2", Type: "channels", Rows: []string{"C1"}},
	}
	n := FilterNames{
		Names:       map[string]string{"D1": "robin", "D2": "sam", "C1": "robins-nest"},
		Closed:      []string{"D3", "D4", "G9"},
		ClosedNames: map[string]string{"D3": "robbie", "D4": "zed", "G9": "ann,robert"},
		ClosedMembers: map[string][]string{
			"G9": {"ann", "robert"},
		},
		LastRead: map[string]string{"D1": "1700000000.000100", "D3": "1700000500.000100", "G9": "1600000000.000000"},
	}
	got := FilterSidebar(secs, "rob", true, false, n)
	want := []FilterSection{
		{ID: "S3", Type: "direct_messages", Rows: []string{"D3", "D1", "G9"}},
		{ID: "S2", Type: "channels", Rows: []string{"C1"}},
	}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("m2: %v, want %v", got, want)
	}
	if got := FilterSidebar(secs, "rob", false, false, n); len(got[0].Rows) != 1 {
		t.Errorf("without m2 closed DMs joined: %v", got)
	}
	// no open DM matches: Direct messages is appended after the others
	got = FilterSidebar(secs, "zed", true, false, n)
	want = []FilterSection{{ID: "S3", Type: "direct_messages", Rows: []string{"D4"}}}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("m2 closed only: %v, want %v", got, want)
	}
	got = FilterSidebar(secs, "nest", true, false, n)
	if !reflect.DeepEqual(got, []FilterSection{{ID: "S2", Type: "channels", Rows: []string{"C1"}}}) {
		t.Errorf("m2 with no DM match: %v", got)
	}
	// m3 matches group DMs by member words
	got = FilterSidebar(secs, "ann rob", true, true, n)
	if len(got) != 1 || !reflect.DeepEqual(got[0].Rows, []string{"G9"}) {
		t.Errorf("m2+m3 group DM: %v", got)
	}
	// equal cursors fall back to the names (QJub)
	n.LastRead = map[string]string{}
	got = FilterSidebar(secs, "rob", true, false, n)
	if !reflect.DeepEqual(got[0].Rows, []string{"G9", "D3", "D1"}) {
		t.Errorf("by name: %v", got[0].Rows)
	}
}

// sidebar_text_filter_m2 against the fake: a closed DM is in FilterNames'
// Closed list once sidebar.dms says so, and FetchSidebarDMs (the input's
// onFocus) loads sidebar.dms when boot's call failed.
func TestSidebarTextFilterClosedDMsFake(t *testing.T) {
	fs, err := fakeslack.Start(fakeslack.Small)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = fs.Close() })
	const closed, user = "D0CLOSED0001", "U0FAKE00050"
	fs.AddClosedDM(closed, user)
	fs.SetFault("sidebar.dms", fakeslack.Fault{Error: "fatal_error", Times: 1})
	a := New(slack.New(fs.Creds()), store.New(""))
	ctx := context.Background()
	if err := a.Boot(ctx); err != nil {
		t.Fatal(err)
	}
	if a.SidebarDMsKnown() {
		t.Fatal("known after a failed boot call")
	}
	if !a.SidebarTextFilterM2() || !a.SidebarTextFilterM3() {
		t.Errorf("the fake account's groups: m2 %v m3 %v", a.SidebarTextFilterM2(), a.SidebarTextFilterM3())
	}
	if err := a.FetchSidebarDMs(ctx); err != nil {
		t.Fatal(err) // the boot failure's backoff doesn't hold a user's retry
	}
	if !a.SidebarDMsKnown() {
		t.Fatal("FetchSidebarDMs didn't record sidebar.dms")
	}
	n := a.Snapshot().FilterNames
	found := false
	for _, id := range n.Closed {
		found = found || id == closed
	}
	if !found || n.ClosedNames[closed] == "" {
		t.Errorf("closed DM not offered: closed %v names %q", n.Closed, n.ClosedNames[closed])
	}
}
