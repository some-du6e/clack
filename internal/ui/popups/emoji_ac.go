package popups

import (
	"strconv"
	"strings"

	"github.com/some-du6e/clack/internal/acrank"
	"github.com/some-du6e/clack/internal/core"
)

// AutocompleteEmoji is the composer's ":" list as Slack builds it
// (autocompleteEmoji → the Searcher with the emoji entity, docs/
// AUTOCOMPLETE-RULES.md): every emoji whose names can match (Slack's
// character-mask prefilter) or whose keywords can (matchKeywords),
// scored by scoreEmojiNameAgainstQuery, frecency-boosted from the shared
// "frecency" pref (o.Frecency) and ranked by the autocomplete model. The
// standard emoji go in Slack's consolidated order, then the custom ones:
// keyword-only matches tie and keep it. Ids are Slack's emoji frecency
// ids ("E"+unicode, "E"+custom name). Name is the best matching name,
// Toned the name to insert at the skin tone in use.
func AutocompleteEmoji(h Host, query string, o acrank.Options) []EmojiMatch {
	x, _ := epIndexFor(h)
	if x == nil || query == "" {
		return nil
	}
	tone := epTone(h)
	q := []rune(acrank.Normalize(query))
	cands := make([]acrank.Cand, 0, 256)
	ids := make([]int32, 0, 256)
	add := func(id int32, kws []string) {
		names := x.allNames(id)
		if !epMayMatch(names, q) && !acrank.KeywordsMayMatch(kws, q) {
			return
		}
		cands = append(cands, acrank.Cand{Kind: acrank.Emoji, ID: core.EmojiFrecencyID(names[0]), Name: names[0], Names: names, Keywords: kws})
		ids = append(ids, id)
	}
	if len(x.std.order) == x.nStd {
		for _, id := range x.std.order {
			add(id, x.std.keywords[id])
		}
	} else {
		for id := int32(0); id < int32(x.nStd); id++ {
			add(id, nil)
		}
	}
	for id := int32(x.nStd); id < int32(len(x.names)); id++ {
		add(id, nil)
	}
	rs := acrank.Rank(query, cands, o)
	out := make([]EmojiMatch, 0, len(rs))
	for _, r := range rs {
		id := ids[r.Index]
		name := r.BestName
		if name == "" {
			name = x.names[id]
		}
		m := EmojiMatch{Name: name, URL: x.url(id, tone), Custom: x.custom(id), Toned: name}
		if tone >= 2 && x.hasSkins(id) {
			m.Toned = name + "::skin-tone-" + strconv.Itoa(tone)
		}
		out = append(out, m)
	}
	return out
}

// epMayMatch is emojiMayMatchOnName's mask test: every query letter
// (whitespace skipped, '-' as '_') appears in one of the names.
func epMayMatch(names []string, q []rune) bool {
	for _, r := range q {
		if r == ' ' || r == '\t' {
			continue
		}
		if r == '-' {
			r = '_'
		}
		found := false
		for _, n := range names {
			if strings.ContainsRune(n, r) || (r == '_' && strings.ContainsRune(n, '-')) || strings.ContainsRune(strings.ToLower(n), r) {
				found = true
				break
			}
		}
		if !found {
			return false
		}
	}
	return true
}
