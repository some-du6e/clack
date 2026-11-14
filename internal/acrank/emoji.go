package acrank

import (
	"math"
	"strings"
)

// emojiBlocked are query → emoji names Slack never offers for that query
// (1uar's table).
var emojiBlocked = map[string][]string{
	"china": {"pinching_hand", "pinching-hand"}, "dead": {"older_adult"}, "dump": {"hindu_temple"},
	"fail": {"family"}, "gross": {"flag-progress-pride"}, "hi": {"shit"}, "hit": {"shit"},
	"hitler": {"white_flower"}, "it": {"shit"}, "macht": {"mannlichkeitssymbol"}, "sh": {"shit"},
	"shi": {"shit"}, "shoot": {"school_satchel"}, "s": {"shit"}, "sit": {"shit"},
	"sith": {"person_with_headscarf"}, "war": {"man-wearing-turban"}, "whine": {"menorah_with_nine_branches"},
	"worst": {"woman-wrestling"}, "wrong": {"woman-rowing-boat"}, "rage": {"transgender"},
}

type emojiScore struct {
	score float64
	ratio float64
	combo int // -1: null
}

func runeIndex(h, n []rune) int {
	if len(n) == 0 {
		return 0
	}
outer:
	for i := 0; i+len(n) <= len(h); i++ {
		for j := range n {
			if h[i+j] != n[j] {
				continue outer
			}
		}
		return i
	}
	return -1
}

func dashesToUnderscore(rs []rune) []rune {
	out := make([]rune, 0, len(rs))
	for i := 0; i < len(rs); i++ {
		if rs[i] == '-' {
			for i+1 < len(rs) && rs[i+1] == '-' {
				i++
			}
			out = append(out, '_')
			continue
		}
		out = append(out, rs[i])
	}
	return out
}

// scoreEmojiName is scoreEmojiNameAgainstQuery(query, name): the query's
// letters must appear in order from where the query (or its first letter)
// first occurs; each run of letters skipped outside the match is a "combo
// break" (at most 2; 1 for 3–4 letter queries, 0 under 3) and a gap may
// not exceed 5 letters (1 for 3 letters, 0 under 3). Score is
// 1 − len(query)/len(name) plus the breaks; an exact name scores 0.
func scoreEmojiName(e, t string) emojiScore {
	n := emojiScore{score: Inf, combo: -1}
	if t == "" {
		return n
	}
	if t == "ok_hand" && e == "ok" {
		return emojiScore{score: 0, ratio: 1, combo: 0}
	}
	for _, b := range emojiBlocked[e] {
		if t == b || strings.Contains(t, b) {
			return n
		}
	}
	er, tr := []rune(e), []rune(t)
	il := len(er)
	maxBreaks, maxGap, underscoreResets := 2, 5, true
	switch {
	case il < 3:
		maxBreaks, maxGap, underscoreResets = 0, 0, false
	case il == 3:
		maxBreaks, maxGap, underscoreResets = 1, 1, false
	case il == 4:
		maxBreaks, underscoreResets = 1, false
	}
	d, c, u := Inf, 0.0, -1
	f := runeIndex(tr, er)
	if f == -1 && len(er) > 0 {
		f = runeIndex(tr, er[:1])
	}
	if f != -1 {
		var stripped []rune
		for _, r := range er {
			if r != ' ' && r != '\t' && r != '\n' && r != '\r' && r != '\f' && r != '\v' {
				stripped = append(stripped, r)
			}
		}
		stripped = dashesToUnderscore(stripped)
		m := dashesToUnderscore(tr)
		h := append([]rune(nil), stripped...)
		u = 0
		p, g, inRun := 0, 0, true
		for idx := f; idx < len(m); idx++ {
			if len(h) > 0 && m[idx] == h[0] {
				inRun = true
				h = h[1:]
				if g > p {
					p = g
				}
				g = 0
				if len(h) == 0 {
					break
				}
			} else if m[idx] == '_' {
				if inRun && underscoreResets {
					g = 0
				} else {
					g++
				}
			} else {
				if inRun {
					u++
				}
				inRun = false
				g++
			}
			if u > maxBreaks || g > maxGap {
				return emojiScore{score: d, ratio: c, combo: u}
			}
		}
		if len(h) > 0 {
			return n
		}
		c = float64(len(stripped)) / float64(len(m))
		d = 1 - c
		if u > 0 {
			d += float64(u)
		}
	}
	return emojiScore{score: d, ratio: c, combo: u}
}

// matchEmoji is filterEmoji/scanEmojiNames over the emoji's names
// (normalized); the best scoring name wins, ties keep the first. When no
// name matches, matchKeywords lets a keyword in (any finite score from
// the Searcher's fuzzy matcher m): the row scores Inf, carries the names
// scan's last result (its ratio and combo breaks are model features) and
// shows the emoji's own name (formatEmojiResult over matchEmoji's d).
func matchEmoji(e *entry, q string, m *Matcher) bool {
	c := e.c
	if q == "" {
		e.score = 0
		e.emojiBest = c.Name
		return true
	}
	best := emojiScore{score: Inf, combo: -1}
	bestName := ""
	exact := false
	names := c.Names
	if len(names) == 0 {
		names = []string{c.Name}
	}
	for _, name := range names {
		s := scoreEmojiName(q, Normalize(name))
		if s.score < best.score || math.IsInf(best.score, 1) {
			best, bestName = s, name
			exact = q == Normalize(name) || (name == "ok_hand" && q == "ok")
		}
	}
	if math.IsInf(best.score, 1) {
		if m == nil || !keywordMatches(c.Keywords, m) {
			return false
		}
		e.score, e.emojiScore = Inf, Inf
		e.emojiRatio, e.emojiCombo = best.ratio, best.combo
		e.emojiBest = c.Name
		return true
	}
	e.score, e.emojiScore = best.score, best.score
	e.emojiRatio, e.emojiCombo = best.ratio, best.combo
	e.emojiBest = bestName
	e.exact, e.emojiExact = exact, exact
	return true
}

// keywordMatches is matchEmoji's t.keywords.some(k => Inf >
// matcher.normalizeAndScore(k)).
func keywordMatches(kws []string, m *Matcher) bool {
	for _, k := range kws {
		if !math.IsInf(m.NormalizeAndScore(k), 1) {
			return true
		}
	}
	return false
}

// KeywordsMayMatch is a cheap test in front of the keyword match, for
// callers gathering candidates: some keyword (normalized) holds every
// letter of q (the normalized query) but its separators, which the fuzzy
// matcher requires of any match (containsAllLetters).
func KeywordsMayMatch(kws []string, q []rune) bool {
	if len(q) == 0 {
		return false
	}
next:
	for _, k := range kws {
		for i, r := range q {
			if isSep(r) && (i != 0 || len(q) > 1) {
				continue
			}
			if !strings.ContainsRune(k, r) {
				continue next
			}
		}
		return true
	}
	return false
}
