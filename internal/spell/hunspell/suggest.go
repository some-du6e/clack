package hunspell

import (
	"sort"
	"strings"
	"unicode"
	"unicode/utf8"
)

// maxSuggest is Hunspell's MAXSUGGESTION; callers take what they need
// (Chromium shows five).
const maxSuggest = 15

// Suggest returns corrections for a misspelled word, best first, in
// Hunspell's order: case fixes, REP, MAP, swapped characters, keyboard
// neighbours (KEY), an extra character, a missing one (TRY), a moved one,
// a wrong one (TRY), a doubled pair, two words; then, when none of those
// matched, n-gram neighbours from the word list.
func (d *Dict) Suggest(word string) []string {
	word = d.convert(strings.TrimSpace(word))
	if word == "" {
		return nil
	}
	ct := caseOf(word)
	s := &sugg{d: d, seen: map[string]bool{}}
	base := word
	switch ct {
	case capInit, capAll:
		s.edits(word)
		base = strings.ToLower(word)
		s.try(base)
		if ct == capAll {
			s.try(initCap(word))
		}
		s.edits(base)
	default:
		s.edits(word)
		if ct == capNone {
			s.try(initCap(word)) // proper nouns typed lower case
		}
	}
	if len(s.out) == 0 {
		s.ngram(base)
	}
	out := s.out
	// re-case to what was typed
	for i, w := range out {
		switch ct {
		case capInit:
			if caseOf(w) == capNone {
				out[i] = initCap(w)
			}
		case capAll:
			if caseOf(w) != capMixed {
				out[i] = strings.ToUpper(w)
			}
		}
	}
	return dedupe(out)
}

func dedupe(in []string) []string {
	seen := map[string]bool{}
	var out []string
	for _, w := range in {
		if !seen[w] {
			seen[w] = true
			out = append(out, w)
		}
	}
	return out
}

type sugg struct {
	d    *Dict
	seen map[string]bool
	out  []string
}

func (s *sugg) full() bool { return len(s.out) >= maxSuggest }

// try adds w when it's correct and suggestable.
func (s *sugg) try(w string) bool {
	if w == "" || s.seen[w] || s.full() {
		return false
	}
	s.seen[w] = true
	if !s.ok(w) {
		return false
	}
	s.out = append(s.out, w)
	return true
}

// ok is Check without suggesting NOSUGGEST words.
func (s *sugg) ok(w string) bool {
	if strings.Contains(w, " ") {
		for _, p := range strings.Split(w, " ") {
			if p == "" || !s.ok(p) {
				return false
			}
		}
		return true
	}
	d := s.d
	if hs, ok := d.words[w]; ok {
		for _, fs := range hs {
			if fs.has(d.noSuggest) || fs.has(d.forbidden) {
				return false
			}
		}
	}
	ok, _ := d.checkCased(w)
	return ok
}

func (s *sugg) edits(w string) {
	d := s.d
	rw := []rune(w)
	// REP: every occurrence, one at a time
	for _, r := range d.rep {
		for i := strings.Index(w, r[0]); i >= 0; {
			s.try(w[:i] + r[1] + w[i+len(r[0]):])
			j := strings.Index(w[i+1:], r[0])
			if j < 0 {
				break
			}
			i += 1 + j
		}
	}
	// MAP: related characters
	for _, g := range d.maps {
		for _, a := range g {
			for i := strings.Index(w, a); i >= 0; {
				for _, b := range g {
					if b != a {
						s.try(w[:i] + b + w[i+len(a):])
					}
				}
				j := strings.Index(w[i+1:], a)
				if j < 0 {
					break
				}
				i += 1 + j
			}
		}
	}
	// swapped neighbours, then two swaps for 4-5 letter words
	for i := 0; i+1 < len(rw); i++ {
		c := append([]rune(nil), rw...)
		c[i], c[i+1] = c[i+1], c[i]
		s.try(string(c))
	}
	if n := len(rw); n == 4 || n == 5 {
		c := append([]rune(nil), rw...)
		c[0], c[1] = c[1], c[0]
		c[n-2], c[n-1] = c[n-1], c[n-2]
		s.try(string(c))
		if n == 5 {
			c := append([]rune(nil), rw...)
			c[0], c[1] = c[1], c[0]
			c[2], c[3] = c[3], c[2]
			s.try(string(c))
		}
	}
	// long swap: two non-adjacent characters
	for i := 0; i < len(rw); i++ {
		for j := i + 2; j < len(rw) && j-i <= 4; j++ {
			c := append([]rune(nil), rw...)
			c[i], c[j] = c[j], c[i]
			s.try(string(c))
		}
	}
	// keyboard neighbours (KEY) and an upper-cased character
	for i := range rw {
		c := append([]rune(nil), rw...)
		c[i] = unicode.ToUpper(rw[i])
		if c[i] != rw[i] {
			s.try(string(c))
		}
		for _, row := range d.key {
			rr := []rune(row)
			for k, x := range rr {
				if x != rw[i] {
					continue
				}
				if k > 0 {
					c[i] = rr[k-1]
					s.try(string(c))
				}
				if k+1 < len(rr) {
					c[i] = rr[k+1]
					s.try(string(c))
				}
			}
		}
	}
	// an extra character
	for i := range rw {
		s.try(string(rw[:i]) + string(rw[i+1:]))
	}
	// a forgotten character
	for i := 0; i <= len(rw); i++ {
		for _, t := range d.try {
			s.try(string(rw[:i]) + string(t) + string(rw[i:]))
		}
	}
	// a moved character (up to 10 places)
	for i := range rw {
		for j := i + 2; j < len(rw) && j-i < 10; j++ {
			c := append([]rune(nil), rw[:i]...)
			c = append(c, rw[i+1:j+1]...)
			c = append(c, rw[i])
			c = append(c, rw[j+1:]...)
			s.try(string(c))
			c2 := append([]rune(nil), rw[:i]...)
			c2 = append(c2, rw[j])
			c2 = append(c2, rw[i:j]...)
			c2 = append(c2, rw[j+1:]...)
			s.try(string(c2))
		}
	}
	// a wrong character
	for i := range rw {
		for _, t := range d.try {
			if t == rw[i] {
				continue
			}
			c := append([]rune(nil), rw...)
			c[i] = t
			s.try(string(c))
		}
	}
	// a doubled pair ("vacacation")
	for i := 0; i+4 < len(rw)+1; i++ {
		if i+4 <= len(rw) && rw[i] == rw[i+2] && rw[i+1] == rw[i+3] {
			s.try(string(rw[:i+2]) + string(rw[i+4:]))
		}
	}
	// two words
	if !d.noSplit {
		for i := 1; i < len(rw); i++ {
			s.try(string(rw[:i]) + " " + string(rw[i:]))
		}
	}
}

// ngram adds the closest words of the list (Hunspell's ngsuggest, in
// outline): roots ranked by 3-gram similarity, their affixed forms ranked
// again by full-length n-grams, longest common subsequence and common
// prefix, above a threshold measured on mangled copies of the word.
func (s *sugg) ngram(w string) {
	d := s.d
	if d.maxNgram <= 0 {
		return
	}
	lw := []rune(strings.ToLower(w))
	n := len(lw)
	if n == 0 {
		return
	}
	type scored struct {
		w  string
		fs flags
		sc int
	}
	const maxRoots = 100
	roots := make([]scored, 0, maxRoots+1)
	for root, hs := range d.words {
		rn := utf8.RuneCountInString(root)
		if rn > n+4 || rn < n-4 {
			continue
		}
		var fs flags
		usable := false
		for _, h := range hs {
			if h.has(d.forbidden) || h.has(d.noSuggest) || h.has(d.onlyInCompound) {
				continue
			}
			fs, usable = h, true
			break
		}
		if !usable {
			continue
		}
		rr := []rune(strings.ToLower(root))
		sc := ngramScore(3, lw, rr, true) + leftCommon(lw, rr)
		if len(roots) < maxRoots || sc > roots[len(roots)-1].sc {
			roots = append(roots, scored{root, fs, sc})
			sort.SliceStable(roots, func(i, j int) bool { return roots[i].sc > roots[j].sc })
			if len(roots) > maxRoots {
				roots = roots[:maxRoots]
			}
		}
	}
	// threshold: the similarity of the word to itself with every fourth
	// character mangled, averaged over the three phases
	thresh := 0
	for sp := 1; sp < 4; sp++ {
		m := append([]rune(nil), lw...)
		for k := sp; k < len(m); k += 4 {
			m[k] = '*'
		}
		thresh += ngramScore(n, lw, m, false)
	}
	thresh = thresh/3 - 1
	var guesses []scored
	for _, r := range roots {
		for _, f := range s.forms(r.w, r.fs) {
			fr := []rune(strings.ToLower(f))
			sc := ngramScore(n, lw, fr, false)
			if sc > thresh {
				guesses = append(guesses, scored{w: f, sc: sc})
			}
		}
	}
	for i := range guesses {
		g := []rune(strings.ToLower(guesses[i].w))
		re := ngramScore(2, lw, g, false) + ngramScore(2, g, lw, false)
		guesses[i].sc = 2*lcs(lw, g) - abs(n-len(g)) + leftCommon(lw, g) + re
	}
	sort.SliceStable(guesses, func(i, j int) bool { return guesses[i].sc > guesses[j].sc })
	added := 0
	for _, g := range guesses {
		if added >= d.maxNgram {
			break
		}
		if s.seen[g.w] {
			continue
		}
		s.seen[g.w] = true
		if !s.ok(g.w) {
			continue
		}
		s.out = append(s.out, g.w)
		added++
	}
}

// forms expands a root with the suffixes and prefixes its flags allow.
func (s *sugg) forms(root string, fs flags) []string {
	d := s.d
	out := []string{}
	if !fs.has(d.needAffix) {
		out = append(out, root)
	}
	rr := []rune(root)
	var sfxForms []string
	for _, f := range fs {
		for _, a := range d.bySfxFlag[f] {
			if a.cont.has(d.needAffix) || a.cont.has(d.circumfix) {
				continue
			}
			strip := []rune(a.strip)
			if len(strip) > len(rr) || !strings.HasSuffix(root, a.strip) {
				continue
			}
			if !a.cond.matchEnd(rr) {
				continue
			}
			w := string(rr[:len(rr)-len(strip)]) + a.add
			out = append(out, w)
			if a.cross {
				sfxForms = append(sfxForms, w)
			}
		}
	}
	for _, f := range fs {
		for _, a := range d.byPfxFlag[f] {
			if a.cont.has(d.needAffix) || a.cont.has(d.circumfix) {
				continue
			}
			if !strings.HasPrefix(root, a.strip) || !a.cond.matchStart(rr) {
				continue
			}
			out = append(out, a.add+root[len(a.strip):])
			if a.cross {
				for _, sf := range sfxForms {
					if strings.HasPrefix(sf, a.strip) {
						out = append(out, a.add+sf[len(a.strip):])
					}
				}
			}
		}
	}
	return out
}

// ngramScore counts the k-grams (1..n) of a found in b. longerWorse
// penalises b being longer than a.
func ngramScore(n int, a, b []rune, longerWorse bool) int {
	if len(a) == 0 {
		return 0
	}
	score := 0
	bs := string(b)
	for k := 1; k <= n; k++ {
		ns := 0
		for i := 0; i+k <= len(a); i++ {
			if strings.Contains(bs, string(a[i:i+k])) {
				ns++
			}
		}
		score += ns
		if ns < 2 && k > 1 {
			break
		}
	}
	if longerWorse {
		if d := len(b) - len(a) - 2; d > 0 {
			score -= d
		}
	} else {
		score -= abs(len(b) - len(a))
	}
	return score
}

func leftCommon(a, b []rune) int {
	i := 0
	for i < len(a) && i < len(b) && a[i] == b[i] {
		i++
	}
	return i
}

func lcs(a, b []rune) int {
	prev := make([]int, len(b)+1)
	cur := make([]int, len(b)+1)
	for i := 1; i <= len(a); i++ {
		for j := 1; j <= len(b); j++ {
			switch {
			case a[i-1] == b[j-1]:
				cur[j] = prev[j-1] + 1
			case prev[j] >= cur[j-1]:
				cur[j] = prev[j]
			default:
				cur[j] = cur[j-1]
			}
		}
		prev, cur = cur, prev
	}
	return prev[len(b)]
}

func abs(x int) int {
	if x < 0 {
		return -x
	}
	return x
}
