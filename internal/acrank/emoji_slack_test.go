package acrank

import (
	"encoding/json"
	"fmt"
	"sort"
	"strings"
	"testing"
	"time"

	"github.com/some-du6e/clack/internal/emoji"
	"github.com/some-du6e/clack/internal/frecency"
)

// The composer's ":" list over the whole standard emoji set, checked
// against Slack's own code. The expected rows were printed by a node
// harness that ran autocompleteEmoji's local Searcher path from Slack's
// web client, build efbd526e8fb3 (client-boot bbd3a8c1f7bb6f10): the
// SearcherMatcher (1uar: scoreEmojiNameAgainstQuery, matchKeywords), the
// SearcherSorter (j8FV: the group sort, preferExactMatch, frecency RI+6
// and bonus points), the model Sorter (I/0x: prepSuggestions, gSR8
// features, 94aK LinearModel, X46X fallback, J3WV rules,
// boostExactMatches) and the AutocompleteFeatures defaults (1Fxd), with
// Karim's experiments of 2026-11-14 (emoji_v17 off, emoji_exact_match_
// boost on), at 2026-11-14 12:00 UTC. The emoji set is Clack's
// (standard.json.gz) in Slack's consolidated order with Slack's English
// keywords (52eK), which gives Slack's own top 25 for every query here.
//
// ":tu" never offers +1/thumbsup in Slack: a query under 3 letters may
// skip no letters (scoreEmojiNameAgainstQuery), "thumbsup" has an "h"
// between the t and the u, and none of +1's keywords starts with "tu".
// Frecency only scores rows that matched, so no use of it, even picks
// stored under "tu" itself, brings it in.

var tSlackNow = time.Date(2026, 11, 14, 12, 0, 0, 0, time.UTC)

// slackEmojiCands is popups.AutocompleteEmoji's candidate list without
// its prefilters: every standard emoji in Slack's order, ids "E"+unicode,
// keywords normalized.
func slackEmojiCands(t *testing.T) []Cand {
	t.Helper()
	var f struct {
		Emoji []struct {
			Names []string `json:"n"`
			Image string   `json:"i"`
		} `json:"emoji"`
	}
	if err := json.Unmarshal(emoji.StandardData(), &f); err != nil {
		t.Fatal(err)
	}
	type row struct {
		names []string
		image string
		pos   int
	}
	var rows []row
	for i, e := range f.Emoji {
		if len(e.Names) == 0 || e.Image == "" {
			continue
		}
		p := emoji.SlackOrder(e.Names[0])
		if p < 0 {
			p = 1<<20 + i
		}
		rows = append(rows, row{e.Names, e.Image, p})
	}
	sort.SliceStable(rows, func(a, b int) bool { return rows[a].pos < rows[b].pos })
	out := make([]Cand, 0, len(rows))
	for _, r := range rows {
		var kws []string
		for _, k := range emoji.Keywords(r.names[0]) {
			kws = append(kws, Normalize(k))
		}
		out = append(out, Cand{Kind: Emoji, ID: "E" + r.image, Name: r.names[0], Names: r.names, Keywords: kws})
	}
	return out
}

// frecencyAt builds a frecency pref: key → {id, count, visits ago}.
func frecencyAt(recs ...any) *frecency.Cache {
	var b strings.Builder
	b.WriteByte('{')
	for i := 0; i+3 < len(recs); i += 4 {
		key, id, count, ago := recs[i].(string), recs[i+1].(string), recs[i+2].(int), recs[i+3].([]time.Duration)
		if i > 0 {
			b.WriteByte(',')
		}
		vs := make([]string, len(ago))
		for j, d := range ago {
			vs[j] = fmt.Sprint(tSlackNow.Add(-d).UnixMilli())
		}
		fmt.Fprintf(&b, `%q:{"id":%q,"count":%d,"visits":[%s]}`, key, id, count, strings.Join(vs, ","))
	}
	b.WriteByte('}')
	return frecency.Parse([]byte(b.String()))
}

func TestEmojiAutocompleteSlackOracle(t *testing.T) {
	cands := slackEmojiCands(t)
	if len(cands) < 1900 {
		t.Fatalf("standard set: %d emoji", len(cands))
	}
	const m, h, d = time.Minute, time.Hour, 24 * time.Hour
	// +1 used a lot (reactions record only the id key, text "")
	thumbs := []time.Duration{5 * m, 2 * h, 5 * h, 20 * h, 2 * d, 3 * d, 4 * d, 6 * d, 9 * d, 12 * d}
	reactions := frecencyAt("E1f44d", "E1f44d", 400, thumbs)
	// as if a client had also stored +1 picks under "tu"
	underTu := frecencyAt("E1f44d", "E1f44d", 400, thumbs, "tu", "E1f44d", 30, thumbs)
	turtle := []time.Duration{h, 3 * h, 30 * h}
	turtleTu := frecencyAt("tu", "E1f422", 3, turtle, "E1f422", "E1f422", 3, turtle)

	remote := remoteWeights(t)
	tuRemote := []string{"tulip", "root_vegetable", "turkey", "turtle", "tumbler_glass", "lotus", "student", "cactus", "bathtub", "test_tube", "stuck_out_tongue", "studio_microphone", "stuffed_flatbread", "flag-tc", "flag-tm", "flag-tn", "flag-tr", "flag-tv", "poultry_leg", "bath", "auto_rickshaw", "mammoth", "male-student", "fortune_cookie", "stuck_out_tongue_closed_eyes"}
	tuFallback := []string{"tulip", "turkey", "turtle", "tumbler_glass", "lotus", "student", "cactus", "stuck_out_tongue", "studio_microphone", "stuffed_flatbread", "bathtub", "stuck_out_tongue_closed_eyes", "stuck_out_tongue_winking_eye", "test_tube", "fortune_cookie", "statue_of_liberty", "male-student", "man_in_tuxedo", "congratulations", "female-student", "man-gesturing-no", "man-gesturing-ok", "woman_in_tuxedo", "man_with_turban", "hairy_creature"}
	cases := []struct {
		name, q string
		w       Weights
		f       *frecency.Cache
		want    []string
	}{
		{"tu cold", "tu", remote, nil, tuRemote},
		{"tu cold fallback model", "tu", FallbackWeights, nil, tuFallback},
		{"tu with +1 reactions", "tu", remote, reactions, tuRemote},
		{"tu with +1 picks under tu", "tu", remote, underTu, tuRemote},
		{"tu with turtle picks", "tu", remote, turtleTu,
			append([]string{"turtle", "tulip", "root_vegetable", "turkey"}, tuRemote[4:]...)},
		{"tu with turtle picks, fallback model", "tu", FallbackWeights, turtleTu,
			append([]string{"turtle", "tulip", "turkey"}, tuFallback[3:]...)},
		{"thu cold", "thu", remote, nil,
			[]string{"thunder_cloud_and_rain", "+1", "-1", "beach_with_umbrella", "camel", "pushpin", "zap", "hand_with_index_finger_and_thumb_crossed", "bathtub", "thought_balloon", "snowman_without_snow", "derelict_house_building"}},
		{"thu with +1 reactions", "thu", remote, reactions,
			[]string{"+1", "thunder_cloud_and_rain", "-1", "beach_with_umbrella", "camel", "pushpin", "zap", "hand_with_index_finger_and_thumb_crossed", "bathtub", "thought_balloon", "snowman_without_snow", "derelict_house_building"}},
		{"like: keywords", "like", remote, nil, []string{"light_blue_heart", "+1", "pink_heart", "linked_paperclips"}},
		{"like with +1 reactions", "like", remote, reactions, []string{"+1", "light_blue_heart", "pink_heart", "linked_paperclips"}},
		{"yes", "yes", remote, nil,
			[]string{"eyes", "heart_eyes", "heart_eyes_cat", "+1", "head_shaking_vertically", "saluting_face", "kissing_closed_eyes", "kissing_smiling_eyes", "face_with_spiral_eyes", "face_with_rolling_eyes", "face_with_bags_under_eyes", "stuck_out_tongue_closed_eyes", "star-struck", "face_with_open_eyes_and_hand_over_mouth", "face_with_hand_over_mouth"}},
		// 135 matches: frecency sorts before the model's cut to 75
		{"sh", "sh", remote, nil,
			[]string{"ship", "shark", "sheep", "shell", "shirt", "shrug", "leftwards_hand", "rightwards_hand", "deciduous_tree", "shield", "shorts", "shovel", "shower", "shrimp", "mans_shoe", "dash", "fish", "hash", "shamrock", "shaved_ice", "shushing_face", "shinto_shrine", "shaking_face", "shopping_bags", "sushi"}},
		{"smi", "smi", remote, nil,
			[]string{"smile", "smirk", "smiley", "smile_cat", "smirk_cat", "smiley_cat", "smiling_imp", "smiling_face_with_tear", "smiling_face_with_3_hearts", "sweat_smile", "laughing", "sunglasses", "face_with_spiral_eyes", "heart_eyes_cat", "upside_down_face", "star-struck", "kissing_smiling_eyes", "slightly_smiling_face", "flag-pm", "flag-um", "grinning", "grin", "innocent", "blush", "yum"}},
	}
	for _, c := range cases {
		o := Options{Source: SourceTexty, Limit: 25, Weights: c.w, Frecency: c.f, Now: tSlackNow,
			Passthrough: true, DemotePassthrough: true, EmojiExactBoost: true, ExactNameGroup: "treatment_5"}
		rs := Rank(c.q, cands, o)
		got := names(cands, rs)
		if !eq(got, c.want) {
			var b strings.Builder
			for i, r := range rs {
				fmt.Fprintf(&b, "\n  %2d %-28s score=%.4f frec=%.4f model=%.6f", i+1, got[i], r.Score, r.FrecencyScore, r.ModelScore)
			}
			t.Errorf("%s: :%s\n got  %v\n want %v%s", c.name, c.q, got, c.want, b.String())
		}
		if c.q == "tu" && (idx(got, "+1") >= 0 || idx(got, "thumbsup") >= 0) {
			t.Errorf("%s: Slack never offers +1 for :tu: %v", c.name, got)
		}
	}
}

// Keyword-only matches score Inf, keep the names scan's ratio and combo
// breaks, and tie in the list's order (Inf − Inf is NaN in Slack's sort).
// The same five through the harness, fallback model: tulip (model 2.538),
// then flag-tc, poultry_leg, bath (each -0.700647); ":like" is +1 alone.
func TestEmojiKeywordMatch(t *testing.T) {
	cs := []Cand{
		{Kind: Emoji, ID: "E1f1f9-1f1e8", Name: "flag-tc", Names: []string{"flag-tc"}, Keywords: []string{"flag", "turks and caicos islands"}},
		{Kind: Emoji, ID: "E1f357", Name: "poultry_leg", Names: []string{"poultry_leg"}, Keywords: []string{"turkey", "food"}},
		{Kind: Emoji, ID: "E1f6c0", Name: "bath", Names: []string{"bath"}, Keywords: []string{"tub"}},
		{Kind: Emoji, ID: "E1f337", Name: "tulip", Names: []string{"tulip"}, Keywords: []string{"flower"}},
		{Kind: Emoji, ID: "E1f44d", Name: "+1", Names: []string{"+1", "thumbsup"}, Keywords: []string{"thumb", "up", "like"}},
	}
	got := names(cs, Rank("tu", cs, Options{Now: tSlackNow, Weights: FallbackWeights}))
	if !eq(got, []string{"tulip", "flag-tc", "poultry_leg", "bath"}) {
		t.Errorf(":tu: %v", got)
	}
	rs := Rank("like", cs, Options{Now: tSlackNow, Weights: FallbackWeights})
	if len(rs) != 1 || cs[rs[0].Index].Name != "+1" || rs[0].BestName != "+1" || !isInf(rs[0].Score) {
		t.Errorf(":like: %+v", rs)
	}
	q := []rune(Normalize("tu"))
	if !KeywordsMayMatch([]string{"turks and caicos islands"}, q) || KeywordsMayMatch([]string{"thumb", "up"}, []rune("tx")) ||
		!KeywordsMayMatch([]string{"thumbs up"}, []rune("t u")) || KeywordsMayMatch(nil, q) {
		t.Error("KeywordsMayMatch")
	}
}

func isInf(f float64) bool { return f > 1e308 }

// The same harness with custom emoji in the set, as getConsolidatedEmoji
// (Ak7A) merges them after the standard ones: a custom emoji is a row
// with id "E"+name and no keywords, a custom alias one more name of its
// target. In Karim's workspace tu is "alias:thumbs-up", a custom image,
// so ":tu" is an exact name: score 0, +400 exact bonus (100, and 300 for
// emoji_exact_match_boost), first, shown as the alias. Synthetic custom
// emoji only.
func TestEmojiAutocompleteSlackOracleCustomAlias(t *testing.T) {
	std := slackEmojiCands(t)
	custom := Cand{Kind: Emoji, ID: "Ethumbs-up", Name: "thumbs-up", Names: []string{"thumbs-up", "tu"}}
	withAlias := func(target string, aliases ...string) []Cand {
		cs := append([]Cand(nil), std...)
		for i := range cs {
			if cs[i].Name == target {
				cs[i].Names = append(append([]string(nil), cs[i].Names...), aliases...)
				return cs
			}
		}
		t.Fatalf("no %s", target)
		return nil
	}
	karim := append(append([]Cand(nil), std...), custom)
	more := append(withAlias("tada", "yay"), custom,
		Cand{Kind: Emoji, ID: "Eturbo", Name: "turbo", Names: []string{"turbo"}},
		Cand{Kind: Emoji, ID: "Etux", Name: "tux", Names: []string{"tux"}})
	noAlias := append([]Cand(nil), more...)
	noAlias[len(std)].Names = []string{"thumbs-up"}
	toPlusOne := withAlias("+1", "tu")

	remote := remoteWeights(t)
	tuRemote := []string{"tulip", "root_vegetable", "turkey", "turtle", "tumbler_glass", "lotus", "student", "cactus", "bathtub", "test_tube", "stuck_out_tongue", "studio_microphone", "stuffed_flatbread", "flag-tc", "flag-tm", "flag-tn", "flag-tr", "flag-tv", "poultry_leg", "bath", "auto_rickshaw", "mammoth", "male-student", "fortune_cookie"}
	tuFallback := []string{"tulip", "turkey", "turtle", "tumbler_glass", "lotus", "student", "cactus", "stuck_out_tongue", "studio_microphone", "stuffed_flatbread", "bathtub", "stuck_out_tongue_closed_eyes", "stuck_out_tongue_winking_eye", "test_tube", "fortune_cookie", "statue_of_liberty", "male-student", "man_in_tuxedo", "congratulations", "female-student", "man-gesturing-no", "man-gesturing-ok", "woman_in_tuxedo", "man_with_turban"}
	cases := []struct {
		name, q string
		cands   []Cand
		w       Weights
		want    []string
		best    string // the first row's shown name ("" no exact row)
	}{
		{"tu, Karim's alias", "tu", karim, remote, append([]string{"thumbs-up"}, tuRemote...), "tu"},
		{"tu, Karim's alias, fallback model", "tu", karim, FallbackWeights, append([]string{"thumbs-up"}, tuFallback...), "tu"},
		{"tu with more custom emoji", "tu", more, remote,
			[]string{"thumbs-up", "tux", "tulip", "turbo", "root_vegetable", "turkey", "turtle", "tumbler_glass", "lotus", "student", "cactus", "bathtub", "test_tube", "stuck_out_tongue", "studio_microphone", "stuffed_flatbread", "flag-tc", "flag-tm", "flag-tn", "flag-tr", "flag-tv", "poultry_leg", "bath", "auto_rickshaw", "mammoth"}, "tu"},
		{"tu without the alias", "tu", noAlias, remote,
			[]string{"tux", "tulip", "turbo", "root_vegetable", "turkey", "turtle", "tumbler_glass", "lotus", "student", "cactus", "bathtub", "test_tube", "stuck_out_tongue", "studio_microphone", "stuffed_flatbread", "flag-tc", "flag-tm", "flag-tn", "flag-tr", "flag-tv", "poultry_leg", "bath", "auto_rickshaw", "mammoth", "male-student"}, ""},
		{"tu, an alias of +1", "tu", toPlusOne, remote, append([]string{"+1"}, tuRemote...), "tu"},
		{"tu, an alias of +1, fallback model", "tu", toPlusOne, FallbackWeights, append([]string{"+1"}, tuFallback...), "tu"},
		{"yay, an alias of tada", "yay", more, remote, []string{"tada", "clap", "smiley"}, "yay"},
	}
	for _, c := range cases {
		o := Options{Source: SourceTexty, Limit: 25, Weights: c.w, Now: tSlackNow,
			Passthrough: true, DemotePassthrough: true, EmojiExactBoost: true, ExactNameGroup: "treatment_5"}
		rs := Rank(c.q, c.cands, o)
		got := names(c.cands, rs)
		if !eq(got, c.want) {
			t.Errorf("%s: :%s\n got  %v\n want %v", c.name, c.q, got, c.want)
			continue
		}
		r := rs[0]
		if c.best == "" {
			if r.Exact {
				t.Errorf("%s: an exact first row without the alias: %+v", c.name, r)
			}
			continue
		}
		if r.BestName != c.best || !r.Exact || r.Score != 0 || r.FrecencyScore != 400 {
			t.Errorf("%s: first row %+v, want %s exact at score 0, frecency 400 (the exact bonus)", c.name, r, c.best)
		}
	}
}
