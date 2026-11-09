package popups

import (
	"strconv"
	"strings"
	"testing"

	"github.com/some-du6e/yoloslackgoclient/internal/emoji"
)

// Synthetic emoji data only.
const epTestStd = `{"emoji":[
 {"n":["grinning"],"i":"1f600","c":0},
 {"n":["+1","thumbsup"],"i":"1f44d","c":1,"s":{"1F3FB":"1f44d-1f3fb","1F3FC":"1f44d-1f3fc","1F3FD":"1f44d-1f3fd","1F3FE":"1f44d-1f3fe","1F3FF":"1f44d-1f3ff"}},
 {"n":["dog"],"i":"1f436","c":2},
 {"n":["tada"],"i":"1f389","c":5},
 {"n":["checkered_flag"],"i":"1f3c1","c":8}
]}`

func epTestCustom() map[string]string {
	return map[string]string{
		"party":          "https://e.test/party.png",
		"partybox":       "https://e.test/partybox.png",
		"partygfm":       "https://e.test/partygfm.png",
		"partying":       "https://e.test/partying.png",
		"partyowl":       "https://e.test/partyowl.png",
		"party-hen":      "https://e.test/party-hen.png",
		"birthday_party": "https://e.test/birthday_party.png",
		"thirdparty":     "https://e.test/thirdparty.png",
		"dog":            "https://e.test/dog.png", // can't shadow the standard dog
		"fiesta":         "alias:party",
		"party2":         "alias:fiesta",
		"thumbs":         "alias:+1",
		"broken":         "alias:nope",
		"selfloop":       "alias:selfloop",
	}
}

func epTestIndex(t testing.TB) *emojiIndex {
	t.Helper()
	std := epParseStd([]byte(epTestStd))
	if len(std.names) != 5 {
		t.Fatalf("parsed %d standard emoji, want 5", len(std.names))
	}
	return epBuild(std, "apple", epTestCustom())
}

func epNames(x *emojiIndex, ids []int32) []string {
	out := make([]string, len(ids))
	for i, id := range ids {
		out[i] = x.names[id]
	}
	return out
}

func TestEmojiIndexGroups(t *testing.T) {
	x := epTestIndex(t)
	want := map[int]string{
		epGroupSmileys:    "grinning,+1",
		epGroupAnimals:    "dog",
		epGroupActivities: "tada",
		epGroupFlags:      "checkered_flag",
		epGroupCustom:     "birthday_party,party,party-hen,partybox,partygfm,partying,partyowl,thirdparty",
	}
	for g := 1; g < epNumGroups; g++ {
		got := strings.Join(epNames(x, x.group[g]), ",")
		if got != want[g] {
			t.Errorf("group %s = %q, want %q", epGroupNames[g], got, want[g])
		}
	}
	if x.nStd != 5 || len(x.names) != 13 {
		t.Errorf("nStd %d, total %d", x.nStd, len(x.names))
	}
}

func TestEmojiIndexAliases(t *testing.T) {
	x := epTestIndex(t)
	party, ok := x.lookup("party")
	if !ok || !x.custom(party) {
		t.Fatal("party missing")
	}
	for _, a := range []string{"fiesta", "party2", ":party:"} {
		if id, ok := x.lookup(a); !ok || id != party {
			t.Errorf("%s -> %d %v, want party", a, id, ok)
		}
	}
	plus, _ := x.lookup("+1")
	if id, ok := x.lookup("thumbs"); !ok || id != plus {
		t.Errorf("thumbs should alias the standard +1")
	}
	if id, ok := x.lookup("thumbsup::skin-tone-2"); !ok || id != plus {
		t.Errorf("skin tone suffix not stripped")
	}
	for _, n := range []string{"broken", "selfloop"} {
		if _, ok := x.lookup(n); ok {
			t.Errorf("%s should be dropped", n)
		}
	}
	if dog, _ := x.lookup("dog"); x.custom(dog) {
		t.Errorf("custom dog shadowed the standard one")
	}
	if got := strings.Join(x.allNames(party), " "); got != "party fiesta party2" {
		t.Errorf("party names = %q", got)
	}
	if got := strings.Join(x.allNames(plus), " "); got != "+1 thumbsup thumbs" {
		t.Errorf("+1 names = %q", got)
	}
}

func TestEmojiURLs(t *testing.T) {
	x := epTestIndex(t)
	plus, _ := x.lookup("+1")
	if got, want := x.url(plus, 1), emoji.CDN+"apple-medium/1f44d.png"; got != want {
		t.Errorf("url = %q, want %q", got, want)
	}
	if got, want := x.url(plus, 3), emoji.CDN+"apple-medium/1f44d-1f3fc.png"; got != want {
		t.Errorf("tone 3 url = %q, want %q", got, want)
	}
	dog, _ := x.lookup("dog")
	if x.hasSkins(dog) || x.url(dog, 4) != x.url(dog, 1) {
		t.Errorf("dog has no skin tones")
	}
	party, _ := x.lookup("party")
	if x.url(party, 5) != "https://e.test/party.png" {
		t.Errorf("custom url = %q", x.url(party, 5))
	}
	g := epBuild(epParseStd([]byte(epTestStd)), "google", nil)
	if id, _ := g.lookup("tada"); g.url(id, 1) != emoji.CDN+"google-medium/1f389.png" {
		t.Errorf("google url = %q", g.url(id, 1))
	}
}

func TestEmojiRank(t *testing.T) {
	cases := []struct {
		name string
		want int
	}{
		{"party", 0}, {"partyx", 1}, {"birthday_party", 2}, {"a-party", 2}, {"a+party", 2},
		{"bparty_party", 2}, {"thirdparty", 3}, {"part", -1}, {"x", -1},
	}
	for _, c := range cases {
		if got := epRank(c.name, "party"); got != c.want {
			t.Errorf("epRank(%q) = %d, want %d", c.name, got, c.want)
		}
	}
}

func TestEmojiSearchRanking(t *testing.T) {
	x := epTestIndex(t)
	var s epSearcher
	s.setFrecency(nil) // no frecency: tier, then length, then name
	want := "party,partybox,partygfm,partying,partyowl,party-hen,birthday_party,thirdparty"
	for _, q := range []string{"party", "PARTY", ":party:", "  Party "} {
		ids, _ := s.search(x, q, 0)
		if got := strings.Join(epNames(x, ids), ","); got != want {
			t.Errorf("search %q = %s\nwant %s", q, got, want)
		}
	}
	ids, _ := s.search(x, "party", 3)
	if got := strings.Join(epNames(x, ids), ","); got != "party,partybox,partygfm" {
		t.Errorf("limit 3 = %s", got)
	}
	// an alias match returns the emoji once, reporting the matched name
	ids, rows := s.search(x, "thumbs", 0)
	if len(ids) != 1 || x.names[ids[0]] != "+1" || x.sName[rows[0]] != "thumbs" {
		t.Errorf("thumbs = %v %v", epNames(x, ids), rows)
	}
	ids, _ = s.search(x, "fiesta", 0)
	if got := strings.Join(epNames(x, ids), ","); got != "party" {
		t.Errorf("fiesta = %s", got)
	}
	if ids, _ := s.search(x, "", 0); len(ids) != 0 {
		t.Errorf("empty query matched %d", len(ids))
	}
	if ids, _ := s.search(x, "zzz", 0); len(ids) != 0 {
		t.Errorf("zzz matched %d", len(ids))
	}
}

func TestEmojiSearchFrecencyBoost(t *testing.T) {
	x := epTestIndex(t)
	var s epSearcher
	s.setFrecency(map[string]float64{
		"partyowl":   100,
		"partying":   400,
		"thirdparty": 50,
		"party-hen":  10,
		"dog":        999, // doesn't match: no effect
	})
	// boosted first within each tier (by score), tiers stay ordered
	want := "party,partying,partyowl,party-hen,partybox,partygfm,birthday_party,thirdparty"
	ids, _ := s.search(x, "party", 0)
	if got := strings.Join(epNames(x, ids), ","); got != want {
		t.Errorf("boosted = %s\nwant %s", got, want)
	}
	ids, _ = s.search(x, "party", 2)
	if got := strings.Join(epNames(x, ids), ","); got != "party,partying" {
		t.Errorf("boosted limit 2 = %s", got)
	}
	// a use recorded under an alias boosts its emoji; scores add up
	s.setFrecency(map[string]float64{"thumbs": 40, "thumbsup": 40, "tada": 70})
	ids, rows := s.search(x, "t", 0)
	names := epNames(x, ids)
	if len(names) < 2 || names[0] != "+1" || names[1] != "tada" {
		t.Errorf("t = %v", names)
	}
	if len(rows) > 0 && x.sName[rows[0]] != "thumbs" {
		t.Errorf("+1 matched as %q, want the shorter alias thumbs", x.sName[rows[0]])
	}
	// no scores: back to length then name
	s.setFrecency(nil)
	ids, _ = s.search(x, "party", 3)
	if got := strings.Join(epNames(x, ids), ","); got != "party,partybox,partygfm" {
		t.Errorf("unboosted = %s", got)
	}
}

// Slack's EmojiList rows: per group a 1px empty-header-row and a 27px
// group row, 33px emoji rows, the last one 43 (getRowHeight); si is the
// screen row index the cells' data-color-index derives from.
func TestEmojiLayoutRows(t *testing.T) {
	var lists epLists
	lists[epGroupFrequent] = []int32{1, 2, 3}
	lists[epGroupSmileys] = []int32{0, 1, 2, 3, 4, 5, 6, 7, 8, 9}
	rows, at, total := epLayoutRows(nil, &lists, false, 27, 1, 33, 10)
	type r struct {
		y, start, si int32
		list, n      uint8
	}
	want := []r{{0, 0, 1, 0, 0}, {28, 0, 2, 0, 3}, {61, 0, 4, 1, 0}, {89, 0, 5, 1, 9}, {122, 9, 6, 1, 1}}
	if len(rows) != len(want) {
		t.Fatalf("rows = %+v", rows)
	}
	for i, w := range want {
		g := rows[i]
		if (r{g.y, g.start, g.si, g.list, g.n}) != w {
			t.Errorf("row %d = %+v, want %+v", i, g, w)
		}
	}
	if total != 165 || at[epGroupFrequent] != 0 || at[epGroupSmileys] != 2 || at[epGroupCustom] != -1 {
		t.Errorf("total %d at %v", total, at)
	}
	// the live picker's tints: Frequently Used's first cell 0 (green),
	// Smileys' first (grinning) 3 (#eea6fc)
	if epCellTint(int(rows[1].si)*epCols) != epTints[0] || epCellTint(int(rows[3].si)*epCols) != epTints[3] {
		t.Error("data-color-index")
	}
	lists[epGroupSearch] = make([]int32, 20)
	rows, at, total = epLayoutRows(rows, &lists, true, 27, 1, 33, 10)
	if len(rows) != 4 || rows[3].n != 2 || at[epGroupSearch] != 0 || at[epGroupFrequent] != -1 || total != 28+3*33+10 {
		t.Errorf("search rows = %+v total %d", rows, total)
	}
}

func TestEmojiParseTone(t *testing.T) {
	for in, want := range map[string]int{"": 1, "1": 1, "3": 3, "skin-tone-4": 4, "9": 1} {
		if got := epParseTone(in); got != want {
			t.Errorf("epParseTone(%q) = %d, want %d", in, got, want)
		}
	}
}

var epBigWords = []string{"party", "blob", "cat", "parrot", "hackclub", "dino", "orpheus", "yay", "sad", "fast"}

// epBigName is synthetic custom emoji i (every 5th is an alias of i+1).
func epBigName(i int) string {
	w := epBigWords
	return w[i%len(w)] + "_" + w[(i/7)%len(w)] + strconv.Itoa(i)
}

func epBigCustom(n int) map[string]string {
	m := make(map[string]string, n)
	w := epBigWords
	for i := 0; i < n; i++ {
		name := epBigName(i)
		if i%5 == 0 {
			m[name] = "alias:" + w[i%len(w)] + "_" + w[(i/7)%len(w)] + strconv.Itoa(i+1)
			continue
		}
		m[name] = "https://e.test/" + name + ".png"
	}
	return m
}

func TestEmojiBigIndex(t *testing.T) {
	if testing.Short() {
		t.Skip("short")
	}
	x := epBuild(epParseStd([]byte(epTestStd)), "apple", epBigCustom(65000))
	var s epSearcher
	s.setFrecency(map[string]float64{epBigName(21): 80, epBigName(11): 40}) // blob_parrot21, blob_blob11
	ids, _ := s.search(x, "party", 0)
	if len(ids) == 0 {
		t.Fatal("no results")
	}
	seen := map[int32]bool{}
	for _, id := range ids {
		if seen[id] {
			t.Fatalf("duplicate result %s", x.names[id])
		}
		seen[id] = true
	}
	// (search reuses its result slices: ids is stale from here on)
	blob, _ := s.search(x, "blob", 0)
	if len(blob) < 3 || x.names[blob[0]] != "blob_parrot21" || x.names[blob[1]] != "blob_blob11" {
		t.Errorf("boosted first = %v", epNames(x, blob[:minInt(3, len(blob))]))
	}
	var lists epLists
	lists[epGroupCustom] = x.group[epGroupCustom]
	rows, _, _ := epLayoutRows(nil, &lists, false, 27, 1, 33, 10)
	if want := 1 + (len(x.group[epGroupCustom])+8)/9; len(rows) != want {
		t.Errorf("rows = %d, want %d", len(rows), want)
	}
}

func BenchmarkEmojiSearch(b *testing.B) {
	x := epBuild(epParseStd([]byte(epTestStd)), "apple", epBigCustom(70000))
	var s epSearcher
	scores := map[string]float64{}
	for i := 1; i < 4000; i += 10 {
		scores[epBigName(i)] = float64(i) // blob_* prefix hits
	}
	s.setFrecency(scores)
	qs := []string{"p", "pa", "par", "part", "party", "party_b"}
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		s.search(x, qs[i%len(qs)], 0)
	}
}
