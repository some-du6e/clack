package popups

import (
	"image"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/some-du6e/clack/internal/acrank"
	"github.com/some-du6e/clack/internal/frecency"
)

// The ":" list in Slack's order (scoreEmojiNameAgainstQuery, the model)
// at the skin tone in use; a frecent emoji climbs to the top.
func TestAutocompleteEmoji(t *testing.T) {
	epToneMu.Lock()
	was := epToneSet
	epToneSet = 3
	epToneMu.Unlock()
	defer func() {
		epToneMu.Lock()
		epToneSet = was
		epToneMu.Unlock()
	}()
	now := time.Date(2026, 11, 9, 12, 0, 0, 0, time.UTC)
	o := acrank.Options{Now: now, EmojiExactBoost: true}
	ms := AutocompleteEmoji(nil, "wave", o)
	if len(ms) == 0 || ms[0].Name != "wave" || ms[0].Toned != "wave::skin-tone-3" || !strings.HasSuffix(ms[0].URL, "/1f44b-1f3fc.png") {
		t.Fatalf("wave at tone 3 first: %+v", ms)
	}
	ms = AutocompleteEmoji(nil, "smi", o)
	names := make([]string, len(ms))
	for i, m := range ms {
		names[i] = m.Name
	}
	if len(names) < 3 || names[0] != "smile" {
		t.Errorf("tightest prefix first: %v", names)
	}
	f := frecency.Parse(nil)
	for i := 0; i < 5; i++ {
		f.Record("E1f605", ":smi", now.Add(-time.Duration(i)*time.Minute)) // sweat_smile
	}
	o.Frecency = f
	if ms := AutocompleteEmoji(nil, "smi", o); len(ms) == 0 || ms[0].Name != "sweat_smile" {
		t.Errorf("frecent first: %+v", ms[:min(3, len(ms))])
	}
	if len(AutocompleteEmoji(nil, "smi", o)) > 25 {
		t.Error("25 rows at most")
	}
}

// epRemoteModel is the search.autocomplete.model answer app.slack.com
// served on 2026-11-09 (internal/acrank/testdata).
func epRemoteModel(t *testing.T) acrank.Weights {
	t.Helper()
	b, err := os.ReadFile("../../acrank/testdata/remote_model_2026-11-09.json")
	if err != nil {
		t.Fatal(err)
	}
	w, ok := acrank.ParseModel(append(append([]byte(`{"model":`), b...), '}'))
	if !ok {
		t.Fatal("model")
	}
	return w
}

func epMatchNames(ms []EmojiMatch) []string {
	out := make([]string, len(ms))
	for i, m := range ms {
		out[i] = m.Name
	}
	return out
}

// Karim's ":tu" (2026-11-14): in their workspace tu is a custom alias
// (emoji.list "tu": "alias:thumbs-up") of a custom thumbs-up image, so
// ":tu" is an exact name and Slack lists it first, shown as ":tu:". The
// rows are Slack's: the emoji-rank harness (build efbd526e8fb3, Karim's
// experiments, 2026-11-14 12:00 UTC) with these custom emoji stored the
// way "upsert emoji from flannel" (dQ/Y) stores them and merged after the
// standard set by getConsolidatedEmoji (Ak7A), where an alias joins its
// target's aliases. An exact name scores 0 and takes the +400 exact bonus
// (emoji_exact_match_boost); without the alias tulip leads the standard
// set (internal/acrank's TestEmojiAutocompleteSlackOracle). Over the whole
// workspace (65,908 entries, 593 matches) the same harness lists tu
// (thumbs-up), tuba, tube, tuff, tuna, tunk, stu, then tulip. Synthetic
// custom emoji only.
func TestAutocompleteEmojiCustomAlias(t *testing.T) {
	remote := epRemoteModel(t)
	const thumbsUp = "https://e.test/thumbs-up.png"
	karim := map[string]string{"thumbs-up": thumbsUp, "tu": "alias:thumbs-up"}
	more := map[string]string{"thumbs-up": thumbsUp, "tu": "alias:thumbs-up",
		"turbo": "https://e.test/turbo.png", "tux": "https://e.test/tux.png", "yay": "alias:tada"}
	noAlias := map[string]string{"thumbs-up": thumbsUp,
		"turbo": "https://e.test/turbo.png", "tux": "https://e.test/tux.png", "yay": "alias:tada"}
	toPlusOne := map[string]string{"tu": "alias:+1"} // a custom alias of a standard emoji

	tuRemote := []string{"tu", "tulip", "root_vegetable", "turkey", "turtle", "tumbler_glass", "lotus", "student", "cactus", "bathtub", "test_tube", "stuck_out_tongue", "studio_microphone", "stuffed_flatbread", "flag-tc", "flag-tm", "flag-tn", "flag-tr", "flag-tv", "poultry_leg", "bath", "auto_rickshaw", "mammoth", "male-student", "fortune_cookie"}
	tuFallback := []string{"tu", "tulip", "turkey", "turtle", "tumbler_glass", "lotus", "student", "cactus", "stuck_out_tongue", "studio_microphone", "stuffed_flatbread", "bathtub", "stuck_out_tongue_closed_eyes", "stuck_out_tongue_winking_eye", "test_tube", "fortune_cookie", "statue_of_liberty", "male-student", "man_in_tuxedo", "congratulations", "female-student", "man-gesturing-no", "man-gesturing-ok", "woman_in_tuxedo", "man_with_turban"}
	cases := []struct {
		name   string
		custom map[string]string
		q      string
		w      acrank.Weights
		want   []string
	}{
		{"tu, Karim's alias", karim, "tu", remote, tuRemote},
		{"tu, Karim's alias, fallback model", karim, "tu", acrank.FallbackWeights, tuFallback},
		{"tu with more custom emoji", more, "tu", remote,
			[]string{"tu", "tux", "tulip", "turbo", "root_vegetable", "turkey", "turtle", "tumbler_glass", "lotus", "student", "cactus", "bathtub", "test_tube", "stuck_out_tongue", "studio_microphone", "stuffed_flatbread", "flag-tc", "flag-tm", "flag-tn", "flag-tr", "flag-tv", "poultry_leg", "bath", "auto_rickshaw", "mammoth"}},
		{"tu with more custom emoji, fallback model", more, "tu", acrank.FallbackWeights,
			[]string{"tu", "tux", "tulip", "turbo", "turkey", "turtle", "tumbler_glass", "lotus", "student", "cactus", "stuck_out_tongue", "studio_microphone", "stuffed_flatbread", "bathtub", "stuck_out_tongue_closed_eyes", "stuck_out_tongue_winking_eye", "test_tube", "fortune_cookie", "statue_of_liberty", "male-student", "man_in_tuxedo", "congratulations", "female-student", "man-gesturing-no", "man-gesturing-ok"}},
		{"tu without the alias", noAlias, "tu", remote,
			[]string{"tux", "tulip", "turbo", "root_vegetable", "turkey", "turtle", "tumbler_glass", "lotus", "student", "cactus", "bathtub", "test_tube", "stuck_out_tongue", "studio_microphone", "stuffed_flatbread", "flag-tc", "flag-tm", "flag-tn", "flag-tr", "flag-tv", "poultry_leg", "bath", "auto_rickshaw", "mammoth", "male-student"}},
		{"tu, an alias of +1", toPlusOne, "tu", remote, tuRemote},
		{"tu, an alias of +1, fallback model", toPlusOne, "tu", acrank.FallbackWeights, tuFallback},
		{"yay, an alias of tada", more, "yay", remote, []string{"yay", "clap", "smiley"}},
		{"tux", more, "tux", remote, []string{"tux", "man_in_tuxedo", "woman_in_tuxedo", "person_in_tuxedo"}},
	}
	now := time.Date(2026, 11, 14, 12, 0, 0, 0, time.UTC)
	for _, c := range cases {
		x := epBuild(epStandard(), "google", c.custom)
		o := acrank.Options{Source: acrank.SourceTexty, Limit: 25, Weights: c.w, Now: now,
			Passthrough: true, DemotePassthrough: true, EmojiExactBoost: true, ExactNameGroup: "treatment_5"}
		got := epMatchNames(epAutocomplete(x, 1, c.q, o))
		if strings.Join(got, " ") != strings.Join(c.want, " ") {
			t.Errorf("%s: :%s\n got  %v\n want %v", c.name, c.q, got, c.want)
		}
	}

	// the alias row is its target: thumbs-up's image, a custom emoji, put
	// in as ":tu:" (no skin tones); an alias of +1 is +1 at the tone
	o := acrank.Options{Source: acrank.SourceTexty, Weights: remote, Now: now, EmojiExactBoost: true}
	ms := epAutocomplete(epBuild(epStandard(), "google", karim), 3, "tu", o)
	if len(ms) == 0 || ms[0] != (EmojiMatch{Name: "tu", URL: thumbsUp, Custom: true, Toned: "tu"}) {
		t.Errorf("tu (thumbs-up) at tone 3: %+v", ms[:min(2, len(ms))])
	}
	x := epBuild(epStandard(), "google", toPlusOne)
	plus, _ := x.lookup("+1")
	ms = epAutocomplete(x, 3, "tu", o)
	if len(ms) == 0 || ms[0] != (EmojiMatch{Name: "tu", URL: x.url(plus, 3), Custom: false, Toned: "tu::skin-tone-3"}) {
		t.Errorf("tu (+1) at tone 3: %+v", ms[:min(2, len(ms))])
	}
}

// Every custom alias is a name of the emoji it points at, through a chain
// of aliases too, so typing it exactly lists that emoji first under the
// alias; a broken alias is no emoji.
func TestAutocompleteEmojiAliasNames(t *testing.T) {
	x := epBuild(epStandard(), "google", epTestCustom())
	o := acrank.Options{Now: time.Date(2026, 11, 14, 12, 0, 0, 0, time.UTC), EmojiExactBoost: true}
	for alias, target := range map[string]string{"fiesta": "party", "party2": "party", "thumbs": "+1"} {
		id, _ := x.lookup(target)
		ms := epAutocomplete(x, 1, alias, o)
		if len(ms) == 0 || ms[0].Name != alias || ms[0].URL != x.url(id, 1) || ms[0].Custom != x.custom(id) {
			t.Errorf(":%s: want %s first, shown as %s: %v", alias, target, alias, epMatchNames(ms))
		}
	}
	for _, n := range []string{"broken", "selfloop"} {
		for _, m := range epAutocomplete(x, 1, n, o) {
			if m.Name == n {
				t.Errorf(":%s: the broken alias is listed", n)
			}
		}
	}
}

// The search box's has: emoji rows (srACEmoji, the same Searcher) rank
// again when the index of the workspace's custom emoji lands, like the
// composer's ":" list: the rows shown while it built came from the
// standard set, without the custom alias tu.
func TestAutocompleteEmojiSearchHasRanksAgain(t *testing.T) {
	e := srNewEnv(t, false)
	const thumbsUp = "https://e.test/thumbs-up.png"
	e.h.a.S.CustomEmoji.Set(map[string]string{"thumbs-up": thumbsUp, "tu": "alias:thumbs-up",
		"tuxedo-cat": "https://e.test/tuxedo-cat.png", "tuna-can": "https://e.test/tuna-can.png",
		"turbo-snail": "https://e.test/turbo-snail.png", "tulip-row": "https://e.test/tulip-row.png"})
	p := NewSearchPopover(e.h, "", image.Rectangle{})
	d := &srDrv{size: image.Pt(1280, 800), w: p}
	d.frame()
	d.frame()
	srType(d, p, "has::tu")
	firstEmoji := func() (srACItem, bool) {
		for _, it := range p.ac.items {
			if it.kind == srAIEmoji {
				return it, true
			}
		}
		return srACItem{}, false
	}
	d.until(t, "the has: emoji rows", func() bool {
		_, ok := firstEmoji()
		return p.ac.m.typ == "has" && ok
	})
	d.until(t, "tu (thumbs-up) first among the has: emoji", func() bool {
		it, _ := firstEmoji()
		return it.id == "tu" && it.emojiURL == thumbsUp
	})
}
