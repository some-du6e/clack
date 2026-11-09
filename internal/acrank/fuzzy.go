package acrank

import (
	"math"
	"strings"
	"unicode"
)

// Inf is the score of a candidate that doesn't match.
var Inf = math.Inf(1)

// isSep is FuzzyMatcher's separator class /[- _'.’​()/／]/.
func isSep(r rune) bool {
	switch r {
	case '-', ' ', '_', '\'', '.', '’', '​', '(', ')', '/', '／':
		return true
	}
	return false
}

func stripSeps(rs []rune) []rune {
	out := make([]rune, 0, len(rs))
	for _, r := range rs {
		if !isSep(r) {
			out = append(out, r)
		}
	}
	return out
}

// isCJK is FPvQ: Han, kana and Hangul.
func isCJK(r rune) bool {
	return unicode.In(r, unicode.Han, unicode.Hiragana, unicode.Katakana, unicode.Hangul)
}

// MatcherOpts are FuzzyMatcher's options.
type MatcherOpts struct {
	FuzzyLimit     int // 0: unlimited
	FuzzyJumpLimit int // 0: 5
	FuzzyCJK       bool
	// BeyondWordBoundaries: a 4+ letter query found anywhere scores 2
	// (ml_fuzzy_match_beyond_word_boundaries_v2).
	BeyondWordBoundaries bool
	SearchForwardOnly    bool
	SubstringsOnly       bool
}

// Matcher is Slack's FuzzyMatcher (+EXg): it scores how many word
// boundaries a match had to jump. 0 is a prefix of the text (separators
// in the query match separators), each jump to the start of a later (or
// earlier) word adds 1. The first path found wins (a depth-first search
// that tries the next letter before any jump), not the cheapest.
type Matcher struct {
	query   []rune // normalized
	noSeps  []rune
	opts    MatcherOpts
	limit   float64
	beyond  bool
	qSep    []bool
	hasQuer bool
}

// NewMatcher normalizes q and builds the matcher.
func NewMatcher(q string, o MatcherOpts) *Matcher {
	m := &Matcher{query: []rune(Normalize(q)), opts: o, limit: Inf}
	if o.FuzzyLimit > 0 {
		m.limit = float64(o.FuzzyLimit)
	}
	if len(m.query) > 1024 {
		m.query = m.query[:1024]
	}
	m.noSeps = stripSeps(m.query)
	m.beyond = o.BeyondWordBoundaries && len(m.noSeps) >= 4
	m.qSep = make([]bool, len(m.query))
	for i, r := range m.query {
		m.qSep[i] = isSep(r) && (i != 0 || len(m.query) > 1)
	}
	m.hasQuer = len(m.query) > 0
	return m
}

// Query is the normalized query.
func (m *Matcher) Query() string { return string(m.query) }

// NormalizeAndScore normalizes s first; an exact match scores 0.
func (m *Matcher) NormalizeAndScore(s string) float64 {
	t := Normalize(s)
	if t == string(m.query) {
		return 0
	}
	return m.Score(t)
}

type fnode struct {
	letter  rune
	ignored bool
	next    *fnode
	jumps   *[]*fnode
	visited bool
	index   int
	cjk     bool
}

// Score scores s as is (callers normalize).
func (m *Matcher) Score(s string) float64 {
	if s == "" {
		return Inf
	}
	rs := []rune(s)
	t := stripSeps(rs)
	if len(m.noSeps) > len(t) || !m.hasQuer || !m.containsAllLetters(s) {
		return Inf
	}
	g := m.graph(rs)
	if g == nil {
		return Inf
	}
	res, ok := m.searchWordBoundaries(g)
	if !ok {
		return m.beyondScore(t)
	}
	n := float64(res)
	switch {
	case n > m.limit:
		return m.beyondScore(t)
	case n > 2:
		return math.Min(n, m.beyondScore(t))
	}
	return n
}

func (m *Matcher) beyondScore(t []rune) float64 {
	if m.beyond && strings.Contains(string(t), string(m.noSeps)) {
		return math.Min(2, m.limit)
	}
	return Inf
}

func (m *Matcher) containsAllLetters(s string) bool {
	for i, r := range m.query {
		if !m.qSep[i] && !strings.ContainsRune(s, r) {
			return false
		}
	}
	return true
}

func (m *Matcher) graph(rs []rune) *fnode {
	jl := m.opts.FuzzyJumpLimit
	if jl == 0 {
		jl = 5
	}
	jumps := make([]*fnode, 0, jl)
	var head, prev *fnode
	for i, r := range rs {
		n := &fnode{letter: r, ignored: isSep(r), jumps: &jumps, index: i, cjk: isCJK(r)}
		if m.shouldAddJump(prev, n) && len(jumps) < jl {
			jumps = append(jumps, n)
		}
		if prev != nil {
			prev.next = n
		} else {
			head = n
		}
		prev = n
	}
	return head
}

func (m *Matcher) shouldAddJump(prev, n *fnode) bool {
	if n.ignored {
		return false
	}
	return prev == nil || prev.ignored || (m.opts.FuzzyCJK && (prev.cjk != n.cjk || n.cjk))
}

// searchWordBoundaries returns the jump count of the first path found.
func (m *Matcher) searchWordBoundaries(g *fnode) (int, bool) {
	if c, ok := m.worker(g, 0, 0); ok {
		return c, true
	}
	for _, j := range *g.jumps {
		if j == g {
			continue
		}
		if c, ok := m.worker(j, 0, 1); ok {
			return c, true
		}
	}
	return 0, false
}

// worker matches query[qi:] starting at node e; cost is what reaching e
// cost. It returns the total jumps.
func (m *Matcher) worker(e *fnode, qi int, cost int) (int, bool) {
	if e == nil || qi >= len(m.query) {
		return 0, false
	}
	if e.visited || !(e.letter == m.query[qi] || (e.ignored && m.qSep[qi])) {
		return 0, false
	}
	if qi+1 >= len(m.query) {
		return cost, true
	}
	e.visited = true
	defer func() { e.visited = false }()
	if c, ok := m.worker(e.next, qi+1, 0); ok {
		return cost + c, true
	}
	s := qi + 1
	for m.qSep[s] && s+1 < len(m.query) {
		s++
	}
	for _, n := range *e.jumps {
		if n.visited || n == e {
			continue
		}
		if (m.opts.SearchForwardOnly || m.opts.SubstringsOnly) && n.index < e.index {
			continue
		}
		if m.opts.SubstringsOnly && n.index > e.index+2 {
			continue
		}
		if c, ok := m.worker(n, s, 1); ok {
			return cost + c, true
		}
	}
	return 0, false
}

// ---- PositionAwareFuzzyMatcher (j8FV) ----

// Similarity is PositionAwareFuzzyMatcher.computeSimilarity: the cosine
// of 2- and 3-gram vectors weighted 1/position. Whitespace is dropped and
// both are lowercased; when the query has no '-' or '_' they are dropped
// from the candidate too.
func Similarity(query, cand string) float64 {
	if query == "" || cand == "" {
		return 0
	}
	keep := strings.ContainsAny(query, "-_")
	q := ngramTF(prepNgram(query, false))
	c := ngramTF(prepNgram(cand, !keep))
	var dot, nq, nc float64
	for g, a := range q {
		b := c[g]
		dot += a * b
		nq += a * a
	}
	for _, b := range c {
		nc += b * b
	}
	if nq == 0 || nc == 0 {
		return 0
	}
	return dot / (math.Sqrt(nq) * math.Sqrt(nc))
}

func prepNgram(s string, strip bool) []rune {
	out := make([]rune, 0, len(s))
	for _, r := range strings.ToLower(s) {
		if unicode.IsSpace(r) || (strip && (r == '-' || r == '_')) {
			continue
		}
		out = append(out, r)
	}
	return out
}

func ngramTF(rs []rune) map[string]float64 {
	tf := map[string]float64{}
	for n := 2; n <= 3; n++ {
		for a := 0; a+n <= len(rs); a++ {
			tf[string(rs[a:a+n])] += 1 / float64(a+1)
		}
	}
	return tf
}
