package acrank

import (
	"encoding/json"
	"fmt"
	"math"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/some-du6e/clack/internal/frecency"
)

var t0 = time.Date(2026, 11, 9, 12, 0, 0, 0, time.UTC)

func ts(d time.Duration) float64 { return float64(t0.Add(-d).Unix()) }

func TestNormalize(t *testing.T) {
	for in, want := range map[string]string{
		"karim": "karim", "Karim  Hacker ": "karim hacker", "José": "jose", "Ærøskøbing": "aeroskobing",
		"Ивана": "ivana", "": "", " x": "x",
	} {
		if got := Normalize(in); got != want {
			t.Errorf("Normalize(%q) = %q, want %q", in, got, want)
		}
	}
	if FormatQuery("@kar") != "kar" || FormatQuery("#gen") != "gen" || SearcherQuery(":+1") != ":+1" || SearcherQuery("+1") != "+1" {
		t.Error("formatQuery trims")
	}
}

// The matcher counts word-boundary jumps; substrings inside a word don't
// match (beyond-word-boundary matching is off for Hack Club).
func TestMatcherScores(t *testing.T) {
	m := func(q string) *Matcher {
		return NewMatcher(q, MatcherOpts{FuzzyLimit: 3, FuzzyJumpLimit: 20, FuzzyCJK: true})
	}
	cases := []struct {
		q, s string
		want float64
	}{
		{"kar", "karim hacker", 0},
		{"karim h", "karim hacker", 0}, // the space matches the separator
		{"hac", "karim hacker", 1},
		{"kh", "karim hacker", 1},
		{"ker", "karim hacker", Inf}, // inside a word
		{"smi", "john.smith", 1},
		{"js", "john.smith", 1},
		{"gen", "general", 0},
		{"proj", "team-proj-x", 1},
		{"tpx", "team-proj-x", 2},
		{"karimx", "karim", Inf},
	}
	for _, c := range cases {
		if got := m(c.q).Score(c.s); got != c.want {
			t.Errorf("score(%q, %q) = %v, want %v", c.q, c.s, got, c.want)
		}
	}
	bm := NewMatcher("acke", MatcherOpts{FuzzyLimit: 3, FuzzyJumpLimit: 20, BeyondWordBoundaries: true})
	if got := bm.Score("karim hacker"); got != 2 {
		t.Errorf("beyond word boundaries: %v, want 2", got)
	}
}

// Values from Slack's own PositionAwareFuzzyMatcher (j8FV) run in node.
func TestSimilarityOracle(t *testing.T) {
	cases := []struct {
		q, c string
		want float64
	}{
		{"kar", "karim", 0.898877105}, {"kar", "akari", 0.466084425}, {"karim", "karim", 1}, {"k", "karim", 0},
		{"gen", "general", 0.872594591}, {"sweat-sm", "sweat_smile", 0.920906099}, {"ale", "alex", 0.928279122},
		{"ale", "alexis", 0.882777292},
	}
	for _, c := range cases {
		if got := Similarity(c.q, c.c); math.Abs(got-c.want) > 1e-8 {
			t.Errorf("Similarity(%q, %q) = %.9f, want %.9f", c.q, c.c, got, c.want)
		}
	}
}

func TestSimilarity(t *testing.T) {
	if s := Similarity("karim", "karim"); math.Abs(s-1) > 1e-9 {
		t.Errorf("identical = %v", s)
	}
	if Similarity("k", "karim") != 0 {
		t.Error("a 1-letter query has no n-grams")
	}
	a, b := Similarity("kar", "karim"), Similarity("kar", "akari")
	if !(a > b && b > 0) {
		t.Errorf("prefix grams weigh more: %v vs %v", a, b)
	}
}

func TestScoreEmojiName(t *testing.T) {
	cases := []struct {
		q, name string
		score   float64
		combo   int
	}{
		{"smi", "smile", 1 - 3.0/5, 0},
		{"smile", "smile", 0, 0},
		{"smi", "sweat_smile", 1 - 3.0/11, 0},
		{"tu", "thumbsup", Inf, 1},   // 2 letters: no breaks allowed
		{"thup", "thumbsup", 1.5, 1}, // 4 letters: one break allowed
		{"tada", "tada", 0, 0},
		{"ok", "ok_hand", 0, 0},
		{"hi", "shit", Inf, -1}, // Slack's blocked pairs
		{"hand", "raised_hands", 1 - 4.0/12, 0},
		{"rai", "raised_hands", 1 - 3.0/12, 0},
		{"ras", "raised_hands", 1.75, 1}, // 3 letters: one break, gaps of 1
		{"smilecat", "smile_cat", 1 - 8.0/9, 0},
		{"smicat", "smiley_cat", 1.4, 1},
	}
	for _, c := range cases {
		got := scoreEmojiName(c.q, c.name)
		if math.Abs(got.score-c.score) > 1e-9 && !(math.IsInf(got.score, 1) && math.IsInf(c.score, 1)) {
			t.Errorf("scoreEmojiName(%q, %q) = %v, want %v", c.q, c.name, got.score, c.score)
		}
		if !math.IsInf(c.score, 1) && got.combo != c.combo {
			t.Errorf("scoreEmojiName(%q, %q) breaks = %d, want %d", c.q, c.name, got.combo, c.combo)
		}
	}
}

func names(cs []Cand, rs []Result) []string {
	out := make([]string, len(rs))
	for i, r := range rs {
		c := cs[r.Index]
		out[i] = c.Name
		if c.Kind == Member && c.DisplayName != "" {
			out[i] = c.DisplayName
		}
	}
	return out
}

func eq(a, b []string) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i] != b[i] {
			return false
		}
	}
	return true
}

func person(id, display, real string) Cand {
	return Cand{Kind: Member, ID: id, Name: id, DisplayName: display, RealName: real, Active: true, SameTeam: true}
}

// Members of the conversation outrank everyone else (MEMBER_OF_CURRENT_
// CHANNEL is the model's biggest member weight), then prefix beats word
// jumps (FUZZY_SCORE), active humans beat bots and deactivated people.
func TestRankMembers(t *testing.T) {
	cs := []Cand{
		person("U1", "kara", "Kara Zed"),
		person("U2", "", "Zed Karlsson"), // "kar" one jump in
		person("U3", "karim", "Karim Hacker"),
		{Kind: Member, ID: "B1", Name: "karbot", RealName: "Karbot", Bot: true},
		{Kind: Member, ID: "U4", Name: "karl", DisplayName: "karl", RealName: "Karl Old", Deleted: true},
		person("U5", "bob", "Bob"),
	}
	cs[2].InChannel, cs[2].HasPresence = true, true
	for _, w := range []Weights{FallbackWeights, remoteWeights(t)} {
		got := names(cs, Rank("kar", cs, Options{Source: SourceTexty, HasChannel: true, Weights: w, Now: t0}))
		if got[0] != "karim" || got[1] != "kara" {
			t.Errorf("channel member, then the active prefix match: %v", got)
		}
		// a deactivated prefix match still beats a one-jump match:
		// FUZZY_SCORE (-1.73 a jump) outweighs USER_IS_ACTIVE
		if idx(got, "karl") < idx(got, "kara") || idx(got, "U2") < idx(got, "karl") || idx(got, "karbot") < idx(got, "kara") {
			t.Errorf("order: %v", got)
		}
		if idx(got, "bob") >= 0 {
			t.Errorf("bob doesn't match: %v", got)
		}
	}
	// the username is not matched
	if r := Rank("karb", []Cand{{Kind: Member, ID: "B1", Name: "karbot", RealName: "Robot"}}, Options{Now: t0}); len(r) != 0 {
		t.Errorf("username matched: %+v", r)
	}
}

func idx(xs []string, s string) int {
	for i, x := range xs {
		if x == s {
			return i
		}
	}
	return -1
}

func remoteWeights(t *testing.T) Weights {
	b, err := os.ReadFile("testdata/remote_model_2026-11-09.json")
	if err != nil {
		t.Fatal(err)
	}
	w, ok := ParseModel(append(append([]byte(`{"model":`), b...), '}'))
	if !ok || len(w) != 75 {
		t.Fatalf("model: %d", len(w))
	}
	return w
}

// Frecency: a person picked often for this query climbs over an equal match.
func TestRankFrecency(t *testing.T) {
	cs := []Cand{person("U1", "alex", "Alex A"), person("U2", "alexis", "Alexis B")}
	o := Options{Now: t0, Weights: remoteWeights(t)}
	if got := names(cs, Rank("ale", cs, o)); got[0] != "alex" {
		t.Fatalf("shorter name first without frecency: %v", got)
	}
	f := frecency.Parse(nil)
	for i := 0; i < 4; i++ {
		f.Record("U2", "@ale", t0.Add(-time.Duration(i)*time.Hour))
	}
	o.Frecency = f
	if got := names(cs, Rank("ale", cs, o)); got[0] != "alexis" {
		t.Errorf("frecent pick first: %v", got)
	}
}

// Keywords and usergroups: @here is offered with the people;
// "here" exactly gets HERE_KEYWORD_EXACT_MATCH; @channel answers "group".
func TestRankKeywordsAndGroups(t *testing.T) {
	cs := []Cand{
		{Kind: Keyword, ID: "BKhere", Name: "here", Name2: "here"},
		{Kind: Keyword, ID: "BKchannel", Name: "channel", Name2: "channel"},
		{Kind: UserGroup, ID: "S1", Name: "Design Team", Handle: "design"},
		person("U1", "henry", "Henry H"),
	}
	got := names(cs, Rank("he", cs, Options{Now: t0, HasChannel: true}))
	if idx(got, "here") < 0 || idx(got, "henry") < 0 {
		t.Fatalf("rows: %v", got)
	}
	if got := names(cs, Rank("gro", cs, Options{Now: t0})); !eq(got, []string{"channel"}) {
		t.Errorf("@channel for 'gro': %v", got)
	}
	if got := names(cs, Rank("des", cs, Options{Now: t0})); !eq(got, []string{"Design Team"}) {
		t.Errorf("usergroup by handle: %v", got)
	}
	if got := names(cs, Rank("team", cs, Options{Now: t0})); !eq(got, []string{"Design Team"}) {
		t.Errorf("usergroup by name word: %v", got)
	}
}

// Channels: member channels before others and archived last at equal
// match; an exact name is boosted; last read recency lifts a channel.
func TestRankChannels(t *testing.T) {
	cs := []Cand{
		{Kind: Channel, ID: "C1", Name: "general-old", IsMember: true, Archived: true},
		{Kind: Channel, ID: "C2", Name: "gen-z", IsMember: false},
		{Kind: Channel, ID: "C3", Name: "general", IsMember: true, LastRead: ts(time.Hour)},
		{Kind: Channel, ID: "C4", Name: "genai", IsMember: true},
		{Kind: Channel, ID: "C5", Name: "team-gen", IsMember: true},
	}
	o := Options{Now: t0, Weights: remoteWeights(t)}
	got := names(cs, Rank("gen", cs, o))
	if got[0] != "general" {
		t.Errorf("recently read member channel first: %v", got)
	}
	// archived sorts last before the model; the model only docks it
	// CHANNEL_IS_ARCHIVED, less than a word jump costs
	if idx(got, "general-old") < idx(got, "general") || idx(got, "general-old") < idx(got, "genai") {
		t.Errorf("archived after the live prefix matches: %v", got)
	}
	if idx(got, "team-gen") < idx(got, "genai") {
		t.Errorf("word match after prefix match: %v", got)
	}
	if got := names(cs, Rank("genai", cs, o)); got[0] != "genai" {
		t.Errorf("exact: %v", got)
	}
}

func TestRankEmoji(t *testing.T) {
	cs := []Cand{
		{Kind: Emoji, ID: "E1f604", Name: "smile", Names: []string{"smile"}},
		{Kind: Emoji, ID: "E1f603", Name: "smiley", Names: []string{"smiley"}},
		{Kind: Emoji, ID: "E1f605", Name: "sweat_smile", Names: []string{"sweat_smile"}},
		{Kind: Emoji, ID: "E1f63c", Name: "smirk_cat", Names: []string{"smirk_cat"}},
		{Kind: Emoji, ID: "E1f642", Name: "slightly_smiling_face", Names: []string{"slightly_smiling_face"}},
	}
	o := Options{Now: t0, Weights: remoteWeights(t), EmojiExactBoost: true}
	got := names(cs, Rank("smi", cs, o))
	if got[0] != "smile" || got[1] != "smiley" {
		t.Errorf("tightest prefix first: %v", got)
	}
	if idx(got, "sweat_smile") < idx(got, "smirk_cat") {
		t.Errorf("word-start substring after prefix: %v", got)
	}
	f := frecency.Parse(nil)
	for i := 0; i < 5; i++ {
		f.Record("E1f605", ":smi", t0.Add(-time.Duration(i)*time.Minute))
	}
	o.Frecency = f
	if got := names(cs, Rank("smi", cs, o)); got[0] != "sweat_smile" {
		t.Errorf("frecent emoji first: %v", got)
	}
}

func TestRankCommands(t *testing.T) {
	cs := []Cand{
		{Kind: Command, ID: "/remind", Name: "/remind", Desc: "Set a reminder"},
		{Kind: Command, ID: "/rename", Name: "/rename", Desc: "Rename a channel"},
		{Kind: Command, ID: "/away", Name: "/away", Desc: "Toggle your away status"},
		{Kind: Command, ID: "/feed", Name: "/feed", Desc: "Manage RSS subscriptions"},
	}
	o := Options{Now: t0, Source: SourceSlash}
	got := names(cs, Rank("rem", cs, o))
	if len(got) < 1 || got[0] != "/remind" {
		t.Errorf("name prefix first: %v", got)
	}
	if got := names(cs, Rank("rss", cs, o)); !eq(got, []string{"/feed"}) {
		t.Errorf("description words match: %v", got)
	}
	if got := names(cs, Rank("", cs, o)); len(got) != 4 {
		t.Errorf("empty query lists all: %v", got)
	}
}

func TestParseModel(t *testing.T) {
	w, ok := ParseModel([]byte(`{"ok":true,"model":{"fuzzy_score":"-1.5","USER_IS_BOT":-0.4}}`))
	if !ok || w["FUZZY_SCORE"] != -1.5 || w["USER_IS_BOT"] != -.4 {
		t.Errorf("%v", w)
	}
	if _, ok := ParseModel([]byte(`{"ok":true}`)); ok {
		t.Error("no model")
	}
	if FallbackWeights.weight("USER_IS_EXTERNAL", SourceTexty) != FallbackWeights["USER_IS_EXTERNAL"]-1 {
		t.Error("USER_IS_EXTERNAL carries -1")
	}
	var x map[string]any
	_ = json.Unmarshal([]byte(`{}`), &x)
}

// search_autocomplete_promote_exact_name_matches in the composer's
// autocomplete: in treatment_5 a member whose name is exactly the query
// ends up in the first five people; in context, first; other groups keep
// the model's order.
func TestExactNameRulesTexty(t *testing.T) {
	var cs []Cand
	for i, n := range []string{"Annabel Lee", "Annie Hall", "Anna Karenina", "Annika Larsson", "Annette Bening",
		"Anneliese Michel", "Annalise Keating", "Annemarie Smith", "Annora Brown", "Annis Grey"} {
		// people you talk to: in the channel, recent DMs
		cs = append(cs, Cand{Kind: Member, ID: fmt.Sprintf("U0000000%02d", i), Name: strings.ToLower(strings.Fields(n)[0]),
			RealName: n, Active: true, SameTeam: true, HasPresence: true, InChannel: true,
			IM: true, IMLatest: ts(time.Duration(i+1) * time.Hour), History: ts(time.Duration(i+1) * time.Minute)})
	}
	cs = append(cs, Cand{Kind: Member, ID: "U0000000AN", Name: "zz", RealName: "Ann", Active: true, SameTeam: true})
	pos := func(group string, inChannel bool) int {
		c := append([]Cand(nil), cs...)
		c[len(c)-1].InChannel = inChannel
		rs := Rank("ann", c, Options{Source: SourceTexty, Limit: 25, Now: t0, ExactNameGroup: group})
		for i, r := range rs {
			if c[r.Index].ID == "U0000000AN" {
				return i
			}
		}
		return -1
	}
	base := pos("", false)
	if base < 5 {
		t.Fatalf("the exact match is at %d without the rules; the fixture should rank it lower", base)
	}
	if got := pos("control", false); got != base {
		t.Errorf("control moved the exact match: %d → %d", base, got)
	}
	if got := pos("treatment_5", false); got < 0 || got > 4 {
		t.Errorf("treatment_5: the exact match at %d, want in the first five (base %d)", got, base)
	}
	if got := pos("treatment_5", true); got != 0 {
		t.Errorf("treatment_5 in context: at %d, want first", got)
	}
	if got := pos("treatment_3", false); got < 0 || got > 2 {
		t.Errorf("treatment_3: at %d, want in the first three", got)
	}
}
