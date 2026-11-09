package frecency

import (
	"encoding/json"
	"reflect"
	"strings"
	"testing"
	"time"
)

var t0 = time.UnixMilli(1_800_000_000_000)

func ago(d time.Duration) int64 { return t0.Add(-d).UnixMilli() }

// testSet is a tiny consolidated emoji store: standard emoji first.
type testSet struct {
	all   []Emoji
	alias map[string]string // custom alias -> target
}

func (s *testSet) All() []Emoji { return s.all }
func (s *testSet) Canonical(n string) (string, bool) {
	if t, ok := s.alias[n]; ok {
		return t, true
	}
	for _, e := range s.all {
		if e.Name == n {
			return n, true
		}
	}
	return "", false
}

func stdSet() *testSet {
	return &testSet{all: []Emoji{
		{"grinning", "E1f600"}, {"slightly_smiling_face", "E1f642"}, {"heart", "E2764-fe0f"},
		{"+1", "E1f44d"}, {"100", "E1f4af"}, {"bug", "E1f41b"}, {"tada", "E1f389"},
		{"fire", "E1f525"}, {"eyes", "E1f440"}, {"smile", "E1f604"}, {"joy", "E1f602"},
		{"thumbs-up", "Ethumbs-up"}, {"parrot", "Eparrot"},
	}, alias: map[string]string{"party_parrot": "parrot"}}
}

func names(t *testing.T, got []string, want ...string) {
	t.Helper()
	if !reflect.DeepEqual(got, want) {
		t.Errorf("got %v\nwant %v", got, want)
	}
}

// The weights are RI+6's buckets; calculateScore is mean weight × count,
// halved for a _reduced record.
func TestScore(t *testing.T) {
	for _, c := range []struct {
		age  time.Duration
		want float64
	}{{time.Hour, 100}, {5 * time.Hour, 80}, {2 * 24 * time.Hour, 60}, {5 * 24 * time.Hour, 40},
		{20 * 24 * time.Hour, 20}, {60 * 24 * time.Hour, 10}, {100 * 24 * time.Hour, 0}} {
		r := &Record{ID: "E1", Count: 1, Visits: []int64{ago(c.age)}}
		if got := Score(r, t0); got != c.want {
			t.Errorf("age %v: %v, want %v", c.age, got, c.want)
		}
	}
	r := &Record{ID: "E1", Count: 4, Visits: []int64{ago(time.Hour), ago(30 * time.Hour)}, Reduced: true}
	if got := Score(r, t0); got != (100+60)*.5/2*4 {
		t.Errorf("reduced = %v", got)
	}
}

// recordFrecencyEvent: a first use is a _reduced record under the id and
// a plain one under the query; later uses countVisit (no _reduced, 10
// visits kept); a second id under the same query turns it into an array.
func TestRecord(t *testing.T) {
	c := Parse(nil)
	c.Record("E1f389", ":Tada", t0)
	if r := c.Item("E1f389"); r == nil || r.Count != 1 || !r.Reduced || len(r.Visits) != 1 {
		t.Fatalf("first: %+v", r)
	}
	if recs, arr, _ := c.get("tada"); arr || len(recs) != 1 || recs[0].ID != "E1f389" || recs[0].Reduced {
		t.Fatalf("query key: %+v %v", recs, arr)
	}
	for i := 1; i <= 11; i++ {
		c.Record("E1f389", "tada", t0.Add(time.Duration(i)*time.Minute))
	}
	if r := c.Item("E1f389"); r.Count != 12 || r.Reduced || len(r.Visits) != MaxVisits || r.Visits[9] != t0.Add(11*time.Minute).UnixMilli() {
		t.Fatalf("after 12: %+v", r)
	}
	c.Record("E1f525", "ta", t0)
	c.Record("E1f389", "ta", t0)
	if recs, arr, _ := c.get("ta"); !arr || len(recs) != 2 || recs[0].ID != "E1f525" || recs[1].ID != "E1f389" {
		t.Fatalf("shared query: %+v", recs)
	}
	c.Record("E1f525", "ta", t0)
	if recs, _, _ := c.get("ta"); recs[0].Count != 2 || recs[1].Count != 1 {
		t.Fatalf("array bump: %+v", recs)
	}
	// a reaction records with no text: only the id key
	n := c.Len()
	c.Record("E1f440", "", t0)
	if c.Len() != n+1 || c.Item("E1f440") == nil {
		t.Fatal("no-text record")
	}
	if c.Record("", "x", t0); c.Len() != n+1 {
		t.Fatal("empty id recorded")
	}
}

// Writing the pref back keeps what Clack didn't touch byte for byte
// (other clients' channel and member records, unknown fields) and lists
// keys in Object.keys order (array indices first).
func TestRoundTrip(t *testing.T) {
	in := `{"C123":{"id":"C123","count":3,"visits":[1],"extra":true},"zz":[{"id":"U1","count":1,"visits":[2]}],"100":{"id":"E1f4af","count":1,"visits":[3]}}`
	c := Parse([]byte(in))
	b, _ := json.Marshal(c)
	want := `{"100":{"id":"E1f4af","count":1,"visits":[3]},"C123":{"id":"C123","count":3,"visits":[1],"extra":true},"zz":[{"id":"U1","count":1,"visits":[2]}]}`
	if string(b) != want {
		t.Errorf("round trip\n%s\nwant\n%s", b, want)
	}
	// as users.prefs.get sends it: a JSON string
	s, _ := json.Marshal(in)
	if Parse(s).Len() != 3 {
		t.Error("string-encoded pref")
	}
	c.Record("E1f389", "", t0)
	b, _ = json.Marshal(c)
	if !strings.Contains(string(b), `"C123":{"id":"C123","count":3,"visits":[1],"extra":true}`) ||
		!strings.HasSuffix(string(b), `"E1f389":{"id":"E1f389","count":1,"visits":[1800000000000],"_reduced":true}}`) {
		t.Errorf("after record: %s", b)
	}
	for _, bad := range []string{"", "null", "[]", `"nope"`, "{"} {
		if Parse([]byte(bad)).Len() != 0 {
			t.Errorf("Parse(%q) not empty", bad)
		}
	}
}

func TestNormalizeText(t *testing.T) {
	for in, want := range map[string]string{
		"tada": "tada", ":tada": "tada", "  Tada  ": "tada", "Café": "cafe", "a  b": "a b",
		"@karim": "karim", "#general": "general", "+1": "1", "::x": ":x", "": "",
	} {
		if got := NormalizeText(in); got != want {
			t.Errorf("NormalizeText(%q) = %q, want %q", in, got, want)
		}
	}
}

// A new user (no frecency, no emoji_use) gets Slack's five defaults.
func TestFrequentlyUsedDefaults(t *testing.T) {
	names(t, FrequentlyUsed(nil, nil, stdSet(), FrequentOptions{}, t0),
		"slightly_smiling_face", "heart", "+1", "100", "bug")
	names(t, FrequentlyUsed(nil, nil, stdSet(), FrequentOptions{NoTeam: true}, t0),
		"slightly_smiling_face", "heart", "+1", "100", "bug")
}

// What app.slack.com showed on 2026-11-09 for an account whose frecency
// pref has one custom emoji and whose emoji_use is empty: that emoji,
// then the defaults up to five (ensureMinResults adds as many as the
// padded emoji_use list is longer than the frecency list).
func TestFrequentlyUsedLiveShape(t *testing.T) {
	c := Parse(nil)
	c.Record("Ethumbs-up", "", t0.Add(-time.Hour))
	names(t, FrequentlyUsed(c, nil, stdSet(), FrequentOptions{}, t0),
		"thumbs-up", "slightly_smiling_face", "heart", "+1", "100")
}

// Frecency order: the emojiFrequentlyUsed model (log1p(itemCount)) first,
// ties by frecency score (recent visits weigh more), then store order;
// emoji_use fills up only as many as its own list is longer.
func TestFrequentlyUsedOrder(t *testing.T) {
	c := Parse(nil)
	rec := func(id string, n int, age time.Duration) {
		for i := 0; i < n; i++ {
			c.Record(id, "", t0.Add(-age))
		}
	}
	rec("E1f525", 3, 50*24*time.Hour)  // fire: 3 uses, old
	rec("E1f389", 3, time.Hour)        // tada: 3 uses, recent
	rec("E1f440", 5, 200*24*time.Hour) // eyes: 5 uses, past every bucket (score 0, still counted)
	rec("Eparrot", 1, time.Hour)       // a custom emoji
	rec("Egone", 9, time.Hour)         // not in the store: dropped
	use := map[string]float64{"smile": 10, "joy": 4, "tada": 30, "party_parrot": 2, "nope": 50, "+1::skin-tone-3": 3}
	got := FrequentlyUsed(c, use, stdSet(), FrequentOptions{}, t0)
	// ensureMinResults adds len(emoji_use list) - len(frecency list) = 1
	names(t, got, "eyes", "tada", "fire", "parrot", "smile")
	// emoji_use alone: canonical names (aliases folded, skin tones
	// stripped, unknown dropped), by count then name
	names(t, UsedEmojiFromPref(use, stdSet(), 36, 5), "tada", "smile", "joy", "+1", "parrot")
	// fewer than five: padded with the defaults, duplicates and all
	names(t, UsedEmojiFromPref(map[string]float64{"heart": 3}, stdSet(), 36, 5),
		"heart", "slightly_smiling_face", "heart", "+1", "100")
}

// The frecency experiment (autocomplete_frequently_used_emojis_frecency)
// reads counts straight from the top-level "E" records.
func TestFrequentlyUsedExperiment(t *testing.T) {
	c := Parse(nil)
	for i := 0; i < 3; i++ {
		c.Record("E1f525", "", t0)
	}
	c.Record("E1f389", "", t0)
	c.Record("E1f600", "", t0)
	names(t, FrequentlyUsed(c, nil, stdSet(), FrequentOptions{FrecencyExperiment: true}, t0),
		"fire", "grinning", "tada", "slightly_smiling_face", "heart")
}

func TestEmojiIDs(t *testing.T) {
	if EmojiID("1f44d", "+1") != "E1f44d" || EmojiID("", "parrot") != "Eparrot" || EmojiID("", "") != "" {
		t.Error("EmojiID")
	}
	if StripSkinToneID("E1f44d-1f3fd") != "E1f44d" || StripSkinTone("wave::skin-tone-2") != "wave" {
		t.Error("skin tone stripping")
	}
	if m := EmojiUse([]byte(`"{\"tada\":2}"`)); m["tada"] != 2 {
		t.Errorf("EmojiUse = %v", m)
	}
}
