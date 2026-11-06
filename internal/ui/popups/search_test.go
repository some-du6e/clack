package popups

import (
	"reflect"
	"strings"
	"testing"
	"time"
)

// Synthetic data only.

func TestSearchMatchTiers(t *testing.T) {
	cases := []struct {
		name, q string
		tier    int
		bold    string
	}{
		{"general", "general", srExact, "general"},
		{"General", "gen", srPrefix, "Gen"},
		{"hack-club-ops", "club", srWordStart, "club"},
		{"Ann Joseph", "jos", srWordStart, "Jos"},
		{"random", "and", srSubstr, "and"},
		{"design-team", "dt", srSubseq, ""},
		{"design", "xyz", srNone, ""},
		{"ab", "abc", srNone, ""},
		{"x", "", srNone, ""},
		{"café-chat", "chat", srWordStart, "chat"},
		{"ÉCOLE", "éc", srPrefix, "ÉC"},
	}
	for _, c := range cases {
		tier, s, e := srMatch(c.name, c.q)
		if tier != c.tier {
			t.Errorf("srMatch(%q, %q) tier = %d, want %d", c.name, c.q, tier, c.tier)
			continue
		}
		if got := c.name[s:e]; got != c.bold {
			t.Errorf("srMatch(%q, %q) bold = %q, want %q", c.name, c.q, got, c.bold)
		}
	}
}

func TestSearchMatchPrefersWordStartOverEarlierSubstring(t *testing.T) {
	// "an" occurs inside "plan" before the word start "an-…".
	tier, s, e := srMatch("plan-annex", "an")
	if tier != srWordStart || "plan-annex"[s:e] != "an" || s != 5 {
		t.Fatalf("got tier %d range [%d,%d)", tier, s, e)
	}
}

func srTestNames(cs []srCand) []string {
	out := make([]string, len(cs))
	for i, c := range cs {
		out[i] = c.Name
	}
	return out
}

func TestSearchRankOrder(t *testing.T) {
	cands := []srCand{
		{ID: "C1", Name: "random"},
		{ID: "C2", Name: "annoucements-old"},
		{ID: "C3", Name: "ann"},
		{ID: "C4", Name: "team-annual"},
		{ID: "C5", Name: "planning"},
		{ID: "C6", Name: "a-n-n"},
		{ID: "C7", Name: "annex"},
		{ID: "C8", Name: "zzz"},
	}
	got := srTestNames(srRank(cands, "ann", 0, false))
	want := []string{"ann", "annex", "annoucements-old", "team-annual", "planning", "a-n-n"}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("rank = %v, want %v", got, want)
	}
}

func TestSearchRankBoostWithinTier(t *testing.T) {
	cands := []srCand{
		{ID: "C1", Name: "dev-a"},
		{ID: "C2", Name: "dev-b", Boost: 4},
		{ID: "C3", Name: "dev", Boost: 0},
	}
	got := srTestNames(srRank(cands, "dev", 0, false))
	// exact beats prefix even without boost; boost orders the prefix tier.
	want := []string{"dev", "dev-b", "dev-a"}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("rank = %v, want %v", got, want)
	}
}

func TestSearchRankSecondaryKeys(t *testing.T) {
	cands := []srCand{
		{Kind: srKUser, ID: "U1", Name: "zed", Alt: "Ann Smith"},
		{Kind: srKUser, ID: "U2", Name: "annie"},
		{Kind: srKUser, ID: "U3", Name: "bo", Handle: "ann.b"},
	}
	r := srRank(cands, "ann", 0, false)
	if len(r) != 3 || r[0].ID != "U2" {
		t.Fatalf("got %v", srTestNames(r))
	}
	for _, c := range r[1:] {
		if c.Bold0 != c.Bold1 {
			t.Errorf("%s: secondary-key match should not bold the name", c.Name)
		}
	}
}

func TestSearchRankGroupDMMembers(t *testing.T) {
	cands := []srCand{
		{Kind: srKConv, ID: "G1", ConvK: "mpim", Name: "ann, bo, cy", Members: []string{"U0", "U1", "U2", "U3"},
			Keys: []string{"Robert Stone", "bo.s"}},
		{Kind: srKConv, ID: "C1", ConvK: "channel", Name: "boats"},
		{Kind: srKUser, ID: "U2", Name: "bo"},
	}
	// a later member's name is a word start in the title, and bolds there
	r := srRank(cands, "bo", 0, false)
	if got := srTestNames(r); !reflect.DeepEqual(got, []string{"bo", "boats", "ann, bo, cy"}) {
		t.Fatalf("rank = %v", got)
	}
	if g := r[2]; g.Name[g.Bold0:g.Bold1] != "bo" {
		t.Fatalf("bold = %q", g.Name[g.Bold0:g.Bold1])
	}
	// members' real names and handles find the group DM, without bolding
	for _, q := range []string{"rob", "stone", "bo.s"} {
		r := srRank(cands, q, 0, false)
		if len(r) == 0 || r[0].ID != "G1" || r[0].Bold0 != r[0].Bold1 {
			t.Fatalf("%q: %v", q, srTestNames(r))
		}
	}
	// a name match beats the same match on a member key
	r = srRank([]srCand{
		{ID: "G1", ConvK: "mpim", Name: "ann, cy", Keys: []string{"dev"}},
		{ID: "C1", Name: "dev"},
	}, "dev", 0, false)
	if r[0].ID != "C1" || r[1].ID != "G1" {
		t.Fatalf("key tier: %v", srTestNames(r))
	}
}

func TestSearchMpdmNames(t *testing.T) {
	cases := []struct {
		name, self string
		want       []string
	}{
		{"mpdm-ann--bo--cy-1", "bo", []string{"ann", "cy"}},
		{"mpdm-ann--bo-12", "", []string{"ann", "bo"}},
		{"mpdm-ann-lee--bo-1", "BO", []string{"ann-lee"}},
		{"mpdm-ann--bo", "", []string{"ann", "bo"}},
		{"general", "", nil},
		{"mpdm-", "", nil},
	}
	for _, c := range cases {
		if got := srMpdmNames(c.name, c.self); !reflect.DeepEqual(got, c.want) {
			t.Errorf("srMpdmNames(%q, %q) = %q, want %q", c.name, c.self, got, c.want)
		}
	}
}

func TestSearchRankLimitAndKeepAll(t *testing.T) {
	cands := []srCand{{ID: "1", Name: "alpha"}, {ID: "2", Name: "beta"}, {ID: "3", Name: "alpine"}}
	if r := srRank(cands, "al", 1, false); len(r) != 1 || r[0].Name != "alpha" {
		t.Fatalf("limit: %v", srTestNames(r))
	}
	if r := srRank(cands, "al", 0, true); len(r) != 3 || r[2].Name != "beta" {
		t.Fatalf("keepAll: %v", srTestNames(r))
	}
}

func TestSearchMerge(t *testing.T) {
	local := []srCand{
		{Kind: srKUser, ID: "U1", Conv: "D1", Name: "ann"},
		{Kind: srKConv, ID: "C1", Name: "ann-club"},
		{Kind: srKConv, ID: "C2", Name: "annex"},
		{Kind: srKConv, ID: "C3", Name: "annals"},
	}
	remote := []srCand{
		{Kind: srKUser, ID: "U1", Name: "ann"},      // dup by user id
		{Kind: srKChan, ID: "C1", Name: "ann-club"}, // dup by channel
		{Kind: srKUser, ID: "U9", Name: "anna"},
		{Kind: srKChan, ID: "C9", Name: "ann-public"},
	}
	got := srMerge(local, remote, 5, 3)
	var ids []string
	for _, c := range got {
		ids = append(ids, c.ID)
	}
	want := []string{"U1", "C1", "C2", "U9", "C9"}
	if !reflect.DeepEqual(ids, want) {
		t.Fatalf("merge = %v, want %v", ids, want)
	}
	// no remote: local fills everything
	if got := srMerge(local, nil, 3, 3); len(got) != 3 {
		t.Fatalf("local only: %d", len(got))
	}
}

func TestSearchParse(t *testing.T) {
	q := srParse(`deploy  "build failed" in:<#C123|eng> from:@ann has:link IS:thread http://x.y foo:bar`)
	wantTerms := []string{"deploy", "build failed", "http://x.y", "foo:bar"}
	if !reflect.DeepEqual(q.Terms, wantTerms) {
		t.Fatalf("terms = %q", q.Terms)
	}
	wantMods := []srMod{{"in", "<#C123|eng>"}, {"from", "@ann"}, {"has", "link"}, {"is", "thread"}}
	if !reflect.DeepEqual(q.Mods, wantMods) {
		t.Fatalf("mods = %v", q.Mods)
	}
	if got := q.Text(); got != "deploy build failed http://x.y foo:bar" {
		t.Errorf("Text = %q", got)
	}
	if got := q.String(); got != `deploy "build failed" http://x.y foo:bar in:<#C123|eng> from:@ann has:link is:thread` {
		t.Errorf("String = %q", got)
	}
	if q.Get("in") != "<#C123|eng>" || q.Get("before") != "" {
		t.Error("Get")
	}
	if !q.Has("has", "LINK") || q.Has("has", "pin") {
		t.Error("Has")
	}
}

func TestSearchParseEdgeCases(t *testing.T) {
	q := srParse("from: in:general  ")
	if len(q.Mods) != 1 || q.Mods[0] != (srMod{"in", "general"}) {
		t.Fatalf("mods = %v", q.Mods)
	}
	if !reflect.DeepEqual(q.Terms, []string{"from:"}) {
		t.Fatalf("terms = %q", q.Terms)
	}
	if got := srParse("").String(); got != "" {
		t.Fatalf("empty = %q", got)
	}
	if got := srParse(`in:"my channel" x`); got.Get("in") != `"my channel"` || got.Text() != "x" {
		t.Fatalf("quoted mod = %+v", got)
	}
}

func TestSearchSetToggle(t *testing.T) {
	q := srParse("hello from:<@U1> has:link")
	q2 := q.Set("from", "<@U2>")
	if got := q2.String(); got != "hello from:<@U2> has:link" {
		t.Fatalf("Set replace = %q", got)
	}
	if got := q.Set("from", "").String(); got != "hello has:link" {
		t.Fatalf("Set remove = %q", got)
	}
	if got := q.Set("in", "<#C1>").String(); got != "hello from:<@U1> has:link in:<#C1>" {
		t.Fatalf("Set add = %q", got)
	}
	if got := q.Toggle("has", "pin").String(); got != "hello from:<@U1> has:link has:pin" {
		t.Fatalf("Toggle add = %q", got)
	}
	if got := q.Toggle("has", "link").String(); got != "hello from:<@U1>" {
		t.Fatalf("Toggle remove = %q", got)
	}
	if got := q.ClearMods().String(); got != "hello" {
		t.Fatalf("ClearMods = %q", got)
	}
	// the original is not modified
	if got := q.String(); got != "hello from:<@U1> has:link" {
		t.Fatalf("original changed: %q", got)
	}
}

func TestSearchRefID(t *testing.T) {
	for in, want := range map[string]string{
		"<#C123|general>": "C123",
		"<#C123>":         "C123",
		"<@U42>":          "U42",
		"@ann":            "",
		"general":         "",
		"<!here>":         "",
		"<>":              "",
	} {
		if got := srRefID(in); got != want {
			t.Errorf("srRefID(%q) = %q, want %q", in, got, want)
		}
	}
}

func TestSearchDates(t *testing.T) {
	now := time.Date(2026, 11, 6, 15, 0, 0, 0, time.UTC)
	if got := srLast7(now); got != "2026-10-30" {
		t.Errorf("srLast7 = %q", got)
	}
	cases := map[time.Time]string{
		now.Add(-2 * time.Hour):                       "Today",
		now.AddDate(0, 0, -1):                         "Yesterday",
		time.Date(2026, 3, 2, 9, 0, 0, 0, time.UTC):   "Mar 2nd",
		time.Date(2024, 12, 23, 9, 0, 0, 0, time.UTC): "Dec 23rd, 2024",
		time.Date(2026, 11, 1, 0, 0, 0, 0, time.UTC):  "Nov 1st",
		time.Date(2026, 1, 11, 0, 0, 0, 0, time.UTC):  "Jan 11th",
	}
	for in, want := range cases {
		if got := srShortDay(in, now); got != want {
			t.Errorf("srShortDay(%v) = %q, want %q", in, got, want)
		}
	}
}

func srTestHits(segs []srSeg) []string {
	var out []string
	for _, s := range segs {
		if s.Hit {
			out = append(out, s.Text)
		}
	}
	return out
}

func srTestJoined(segs []srSeg) string {
	var b strings.Builder
	for _, s := range segs {
		b.WriteString(s.Text)
	}
	return b.String()
}

func TestSearchHighlight(t *testing.T) {
	s := "[Widget backend] the widgets are annoying; Ann said WIDGET-ish"
	segs := srHighlight(s, []string{"widget", "ann"})
	if srTestJoined(segs) != s {
		t.Fatalf("segments lose text: %q", srTestJoined(segs))
	}
	want := []string{"Widget", "widgets", "annoying", "Ann", "WIDGET"}
	if got := srTestHits(segs); !reflect.DeepEqual(got, want) {
		t.Fatalf("hits = %q, want %q", got, want)
	}
	// phrases and punctuation terms match as substrings
	segs = srHighlight("see foo-bar and Foo-Bar2", []string{"foo-bar"})
	if got := srTestHits(segs); !reflect.DeepEqual(got, []string{"foo-bar", "Foo-Bar"}) {
		t.Fatalf("phrase hits = %q", got)
	}
	segs = srHighlight("the build failed twice", []string{"build failed"})
	if got := srTestHits(segs); !reflect.DeepEqual(got, []string{"build failed"}) {
		t.Fatalf("quoted phrase hits = %q", got)
	}
	// unicode case folding keeps rune alignment
	segs = srHighlight("Ünïcode ünï", []string{"ÜNÏ"})
	if got := srTestHits(segs); !reflect.DeepEqual(got, []string{"Ünïcode", "ünï"}) {
		t.Fatalf("unicode hits = %q", got)
	}
	if segs := srHighlight("plain", nil); len(segs) != 1 || segs[0].Hit {
		t.Fatalf("no terms: %+v", segs)
	}
	if segs := srHighlight("", []string{"x"}); segs != nil {
		t.Fatalf("empty: %+v", segs)
	}
	// a term in the middle of a word does not highlight
	if got := srTestHits(srHighlight("banner", []string{"ann"})); got != nil {
		t.Fatalf("mid-word: %q", got)
	}
}

func TestSearchFlatten(t *testing.T) {
	if got := srFlatten("first line\n\n  second \nthird"); got != "first line … second … third" {
		t.Fatalf("flatten = %q", got)
	}
	if got := srFlatten("one"); got != "one" {
		t.Fatalf("flatten one = %q", got)
	}
}

func TestSearchSplitWords(t *testing.T) {
	got := srSplitWords("ab  cd\nef \n", nil)
	want := []string{"ab", "  ", "cd", "\n", "ef", " ", "\n"}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("split = %q, want %q", got, want)
	}
}

func TestSearchPushMRU(t *testing.T) {
	var l []string
	l = srPush(l, "a", 3, false)
	l = srPush(l, "b", 3, false)
	l = srPush(l, "c", 3, false)
	l = srPush(l, "a", 3, false)
	if !reflect.DeepEqual(l, []string{"a", "c", "b"}) {
		t.Fatalf("mru = %v", l)
	}
	l = srPush(l, "d", 3, false)
	if !reflect.DeepEqual(l, []string{"d", "a", "c"}) {
		t.Fatalf("cap = %v", l)
	}
	q := srPush([]string{"Deploy", "x"}, "deploy", 5, true)
	if !reflect.DeepEqual(q, []string{"deploy", "x"}) {
		t.Fatalf("fold = %v", q)
	}
}

func TestSearchRecents(t *testing.T) {
	srMu.Lock()
	oldC, oldQ := srRecentConvs, srRecentQs
	srRecentConvs, srRecentQs = nil, nil
	srMu.Unlock()
	defer func() {
		srMu.Lock()
		srRecentConvs, srRecentQs = oldC, oldQ
		srMu.Unlock()
	}()
	RecordRecentConversation("C1")
	RecordRecentConversation("")
	RecordRecentConversation("C2")
	srRecordQuery("  hello ")
	srRecordQuery("")
	convs, qs := srRecents()
	if !reflect.DeepEqual(convs, []string{"C2", "C1"}) || !reflect.DeepEqual(qs, []string{"hello"}) {
		t.Fatalf("recents = %v %v", convs, qs)
	}
}
