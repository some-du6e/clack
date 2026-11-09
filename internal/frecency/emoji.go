package frecency

import (
	"encoding/json"
	"math"
	"regexp"
	"sort"
	"strings"
	"time"
)

// Frequently Used, as Slack's emoji picker computes it (boot-shared bEcp
// getFrequentlyUsedEmoji, 2026-11).

const (
	// FrequentMax and FrequentMin are getFrequentlyUsedEmoji's maxNum and
	// minNum defaults.
	FrequentMax = 36
	FrequentMin = 5
	// FrequentTTL is how long Slack reuses a computed list (expirationTime
	// = now + 4 h); a read within FrequentExtend of expiry pushes it out
	// to now + 15 min.
	FrequentTTL    = 4 * time.Hour
	FrequentExtend = 15 * time.Minute
)

// DefaultFrequent is DEFAULT_FREQUENTLY_USED_EMOJI_NAMES (bEcp F and N).
var DefaultFrequent = []string{"slightly_smiling_face", "heart", "+1", "100", "bug"}

// EmojiID is constructEmojiFrecencyId: "E"+unicode for a standard emoji,
// "E"+name otherwise ("" when both are empty).
func EmojiID(unicode, name string) string {
	switch {
	case unicode != "":
		return "E" + unicode
	case name != "":
		return "E" + name
	}
	return ""
}

var skinToneID = regexp.MustCompile(`-1f3f[b-f]`)

// StripSkinToneID is stripSkinToneFromId ("E1f44d-1f3fd" -> "E1f44d").
func StripSkinToneID(id string) string { return skinToneID.ReplaceAllString(id, "") }

// StripSkinTone is stripSkinTone ("wave::skin-tone-3" -> "wave").
func StripSkinTone(name string) string {
	if i := strings.Index(name, "::"); i >= 0 {
		return name[:i]
	}
	return name
}

// Emoji is one consolidated emoji (getConsolidatedEmoji): its primary
// name and frecency id.
type Emoji struct {
	Name string
	ID   string
}

// EmojiSet is what the computation needs from the emoji store.
type EmojiSet interface {
	// All is every emoji (standard, then custom) in the store's order.
	All() []Emoji
	// Canonical is getCanonicalEmojiName: a standard name as given, a
	// custom name or its alias target; ok=false when it is not an emoji.
	Canonical(name string) (string, bool)
}

// EmojiUse parses the emoji_use pref ({"name": count}, usually sent as a
// JSON string) into name -> count. Keys keep no order: Slack sorts them.
func EmojiUse(v []byte) map[string]float64 {
	var s string
	if json.Unmarshal(v, &s) == nil {
		v = []byte(s)
	}
	var m map[string]float64
	if json.Unmarshal(v, &m) != nil {
		return nil
	}
	return m
}

// FrequentOptions are getFrequentlyUsedEmoji's inputs.
type FrequentOptions struct {
	Max, Min int // 0: FrequentMax, FrequentMin
	// FrecencyExperiment is autocomplete_frequently_used_emojis_frecency
	// "on" (count order straight from the pref).
	FrecencyExperiment bool
	// NoTeam: Slack returns the first Min defaults without a team id.
	NoTeam bool
}

// FrequentlyUsed is getFrequentlyUsedEmoji without its 4 h cache (the
// caller keeps that): the emoji the frecency pref ranks, filled up from
// the emoji_use pref (or the defaults) to Max.
func FrequentlyUsed(c *Cache, use map[string]float64, set EmojiSet, o FrequentOptions, now time.Time) []string {
	if o.Max <= 0 {
		o.Max = FrequentMax
	}
	if o.Min <= 0 {
		o.Min = FrequentMin
	}
	if c == nil {
		c = &Cache{}
	}
	if o.FrecencyExperiment {
		return frequentByCount(c, set)
	}
	if o.NoTeam {
		return append([]string(nil), DefaultFrequent[:min(o.Min, len(DefaultFrequent))]...)
	}
	s := frequentBySearch(c, set, now)
	if len(s) > o.Max {
		s = s[:o.Max]
	}
	return ensureMinResults(s, use, set, o.Max, o.Min)
}

// frequentBySearch is the searcher's empty-query emoji sort with
// sort {frecency, model: "emojiFrequentlyUsed"}: SearcherSorter orders by
// frecency score (with its +1 isPopularEmojiVariant bonus for thumbsup
// and point_up), then the Sorter re-ranks by the emojiFrequentlyUsed
// model, FRECENCY_LOG_ITEM_COUNT × 1 = log1p(itemCount), stably. Only
// emoji with a hit or item count stay.
func frequentBySearch(c *Cache, set EmojiSet, now time.Time) []string {
	all := set.All()
	hits := c.Hits("", now)
	// Every emoji without a record scores 0 and sorts after the recorded
	// ones (then is filtered out), so only recorded ids are scored; the
	// normalization max is the same.
	var ids []string
	var names []string
	for _, e := range all {
		if e.ID == "" {
			continue
		}
		if hits[e.ID] == nil && c.Item(e.ID) == nil {
			continue
		}
		ids = append(ids, e.ID)
		names = append(names, e.Name)
	}
	if len(ids) == 0 {
		return nil
	}
	nameOf := make(map[string]string, len(ids))
	for i, id := range ids {
		nameOf[id] = names[i]
	}
	sc := c.ScoreItems(hits, ids, now, func(id string) float64 {
		if n := nameOf[id]; n == "thumbsup" || n == "point_up" {
			return 1
		}
		return 0
	})
	SortItems(sc)
	sort.SliceStable(sc, func(i, j int) bool {
		return math.Log1p(float64(sc[i].ItemCount)) > math.Log1p(float64(sc[j].ItemCount))
	})
	out := make([]string, 0, len(sc))
	for _, s := range sc {
		if s.HitCount > 0 || s.ItemCount > 0 {
			out = append(out, names[s.Index])
		}
	}
	return out
}

// frequentByCount is bEcp M (the frecency experiment): top-level "E…"
// records by count (skin tones folded, ties by name), at most 36, at
// least 5 with the defaults.
func frequentByCount(c *Cache, set EmojiSet) []string {
	counts := map[string]int{}
	for _, k := range c.Keys() {
		if !strings.HasPrefix(k, "E") {
			continue
		}
		recs, arr, _ := c.get(k)
		if arr || len(recs) == 0 || recs[0].ID != k {
			continue
		}
		id := StripSkinToneID(k)
		counts[id] = max(counts[id], recs[0].Count)
	}
	nameOf := map[string]string{}
	for _, e := range set.All() {
		if e.ID != "" {
			nameOf[e.ID] = e.Name
		}
	}
	type item struct {
		name  string
		count int
	}
	var items []item
	for id, n := range counts {
		if name := nameOf[id]; name != "" {
			items = append(items, item{name, n})
		}
	}
	sort.Slice(items, func(i, j int) bool {
		if items[i].count != items[j].count {
			return items[i].count > items[j].count
		}
		return items[i].name < items[j].name
	})
	var out []string
	for _, it := range items {
		out = append(out, it.name)
		if len(out) >= FrequentMax {
			break
		}
	}
	if len(out) < FrequentMin {
		for _, d := range DefaultFrequent {
			if !contains(out, d) {
				out = append(out, d)
			}
			if len(out) >= FrequentMin {
				break
			}
		}
	}
	return out
}

// ensureMinResults fills s up from getUsedEmojiFromPref (at most as many
// as that list is long).
func ensureMinResults(s []string, use map[string]float64, set EmojiSet, maxN, minN int) []string {
	if len(s) >= maxN {
		return s
	}
	r := UsedEmojiFromPref(use, set, maxN, minN)
	if len(s) == 0 {
		return r
	}
	need := len(r) - len(s)
	seen := map[string]bool{}
	for _, n := range s {
		seen[n] = true
	}
	for _, n := range r {
		if need <= 0 {
			break
		}
		if !seen[n] {
			seen[n] = true
			need--
			s = append(s, n)
		}
	}
	return s
}

// UsedEmojiFromPref is getUsedEmojiFromPref: emoji_use counts summed per
// canonical name (skin tones stripped, unknown names dropped), by count
// then name; shorter than minN it is padded with the first defaults
// (without de-duplicating, as Slack does), else cut to maxN.
func UsedEmojiFromPref(use map[string]float64, set EmojiSet, maxN, minN int) []string {
	sum := map[string]float64{}
	for n, v := range use {
		c, ok := set.Canonical(StripSkinTone(n))
		if !ok {
			continue
		}
		sum[c] += v
	}
	o := make([]string, 0, len(sum))
	for n := range sum {
		o = append(o, n)
	}
	sort.Slice(o, func(i, j int) bool {
		a, b := sum[o[i]], sum[o[j]]
		if a == b {
			return o[i] < o[j]
		}
		return a > b
	})
	if len(o) < minN {
		e := minN - len(o)
		return append(o, DefaultFrequent[:min(e, len(DefaultFrequent))]...)
	}
	if len(o) > maxN {
		o = o[:maxN]
	}
	return o
}

func contains(s []string, v string) bool {
	for _, x := range s {
		if x == v {
			return true
		}
	}
	return false
}
