package popups

// Pure logic for search and the quick switcher: fuzzy ranking, query /
// modifier parsing, term highlighting and the in-memory MRU lists. Nothing
// here touches Gio, so it is covered by search_test.go.
//
// Scraping policy: recent queries and conversations live in memory only.

import (
	"sort"
	"strings"
	"sync"
	"time"
	"unicode"
)

// ---- fuzzy matching ----

// Match tiers, best first.
const (
	srNone      = iota // no match
	srSubseq           // all query runes appear in order
	srSubstr           // contiguous match inside a word
	srWordStart        // a later word starts with the query
	srPrefix           // the name starts with the query
	srExact            // whole name equals the query
)

// srFold lower-cases s rune by rune (one rune in, one rune out, so rune
// indexes line up with the original string).
func srFold(s string) []rune {
	rs := []rune(s)
	for i, r := range rs {
		rs[i] = unicode.ToLower(r)
	}
	return rs
}

// srSep reports whether r separates words in names ("hack-club", "a_b",
// "Ann Joseph", "x.y").
func srSep(r rune) bool {
	return r == ' ' || r == '-' || r == '_' || r == '.' || r == '/' || r == ',' || r == '(' || r == ')' || r == '@' || r == '#'
}

// srMatch scores name against q (case-insensitive) and returns the tier
// and the matched byte range [start, end) in name for bolding (start ==
// end when the match is not contiguous).
func srMatch(name, q string) (tier, start, end int) {
	q = strings.TrimSpace(q)
	if q == "" || name == "" {
		return srNone, 0, 0
	}
	n, m := srFold(name), srFold(q)
	if len(m) > len(n) {
		return srNone, 0, 0
	}
	idx := srRuneIndex(n, m)
	if idx >= 0 {
		bs, be := srByteRange(name, idx, idx+len(m))
		switch {
		case idx == 0 && len(m) == len(n):
			return srExact, bs, be
		case idx == 0:
			return srPrefix, bs, be
		}
		// is there a word start match anywhere (not only the first hit)?
		for i := idx; i >= 0 && i <= len(n)-len(m); i = srRuneIndexFrom(n, m, i+1) {
			if srSep(n[i-1]) {
				bs, be := srByteRange(name, i, i+len(m))
				return srWordStart, bs, be
			}
		}
		return srSubstr, bs, be
	}
	// subsequence
	j := 0
	for _, r := range n {
		if j < len(m) && r == m[j] {
			j++
		}
	}
	if j == len(m) {
		return srSubseq, 0, 0
	}
	return srNone, 0, 0
}

func srRuneIndex(s, sub []rune) int { return srRuneIndexFrom(s, sub, 0) }

// srRuneIndexFrom finds sub in s at or after from (-1 if absent).
func srRuneIndexFrom(s, sub []rune, from int) int {
	if len(sub) == 0 {
		return -1
	}
outer:
	for i := from; i+len(sub) <= len(s); i++ {
		for k := range sub {
			if s[i+k] != sub[k] {
				continue outer
			}
		}
		return i
	}
	return -1
}

// srByteRange converts a rune range of s into a byte range.
func srByteRange(s string, rs, re int) (int, int) {
	bs, be := len(s), len(s)
	ri := 0
	for bi := range s {
		if ri == rs {
			bs = bi
		}
		if ri == re {
			be = bi
			break
		}
		ri++
	}
	return bs, be
}

// srKind is what a suggestion opens.
type srKind uint8

const (
	srKConv srKind = iota // a sidebar conversation (channel, private, mpim)
	srKUser               // a person (DM opened or created on Enter)
	srKChan               // a channel from the directory (maybe not a member)
)

// srCand is a quick-switcher candidate.
type srCand struct {
	Kind   srKind
	ID     string // conv id (srKConv, srKChan) or user id (srKUser)
	Conv   string // srKUser: existing DM conversation id, if any
	Name   string // primary text (channel name, display name)
	Alt    string // secondary text (real name); also matched
	Handle string // extra match key (user handle)
	ConvK  string // conversation kind: channel | private | im | mpim
	Avatar string
	Boost  int    // unread / mention / recent bonus
	Note   string // display-only secondary text (not matched)
	Remote bool   // came from a directory search

	// set by srRank
	Tier         int
	Bold0, Bold1 int // bold byte range in Name
}

// srScore matches c against q over all its keys.
func srScore(c *srCand, q string) {
	t, s, e := srMatch(c.Name, q)
	c.Tier, c.Bold0, c.Bold1 = t, s, e
	for _, k := range [...]string{c.Alt, c.Handle} {
		if k == "" {
			continue
		}
		if t2, _, _ := srMatch(k, q); t2 > c.Tier {
			// a match on a secondary key ranks a notch lower than the
			// same match on the name.
			if t2 > srSubseq {
				t2--
			}
			if t2 > c.Tier {
				c.Tier, c.Bold0, c.Bold1 = t2, 0, 0
			}
		}
	}
}

// srRank returns the candidates matching q, best first: tier (exact >
// prefix > word prefix > substring > subsequence), then boost, then
// shorter names, then alphabetical. keepAll keeps non-matching candidates
// (server-side directory hits) at the lowest tier. At most limit results
// (limit <= 0: all).
func srRank(cands []srCand, q string, limit int, keepAll bool) []srCand {
	out := make([]srCand, 0, minInt(len(cands), 64))
	for i := range cands {
		c := cands[i]
		srScore(&c, q)
		if c.Tier == srNone {
			if !keepAll {
				continue
			}
			c.Tier = srSubseq
		}
		out = append(out, c)
	}
	sort.SliceStable(out, func(i, j int) bool {
		a, b := &out[i], &out[j]
		if a.Tier != b.Tier {
			return a.Tier > b.Tier
		}
		if a.Boost != b.Boost {
			return a.Boost > b.Boost
		}
		if len(a.Name) != len(b.Name) {
			return len(a.Name) < len(b.Name)
		}
		return strings.ToLower(a.Name) < strings.ToLower(b.Name)
	})
	if limit > 0 && len(out) > limit {
		out = out[:limit]
	}
	return out
}

// srMerge appends remote candidates that are not already present (same
// user, or a DM with that user, or the same channel) after local ones,
// keeping at most total and reserving up to reserve slots for remote hits.
func srMerge(local, remote []srCand, total, reserve int) []srCand {
	seen := make(map[string]bool, len(local)+len(remote))
	for _, c := range local {
		seen[c.ID] = true
		if c.Conv != "" {
			seen[c.Conv] = true
		}
	}
	var extra []srCand
	for _, c := range remote {
		if seen[c.ID] || (c.Conv != "" && seen[c.Conv]) {
			continue
		}
		seen[c.ID] = true
		extra = append(extra, c)
	}
	keep := total - minInt(reserve, len(extra))
	if keep < 0 {
		keep = 0
	}
	if len(local) > keep {
		local = local[:keep]
	}
	out := append([]srCand(nil), local...)
	for _, c := range extra {
		if len(out) >= total {
			break
		}
		out = append(out, c)
	}
	return out
}

// ---- query parsing ----

// srModKeys are the modifiers Slack's search.messages understands.
var srModKeys = map[string]bool{
	"in": true, "from": true, "to": true, "with": true, "before": true, "after": true,
	"on": true, "during": true, "has": true, "is": true,
}

// srMod is one key:value modifier.
type srMod struct{ Key, Val string }

// srQuery is a parsed search query: free terms and modifiers, in order.
type srQuery struct {
	Terms []string // words; quoted phrases keep their spaces (no quotes)
	Mods  []srMod
}

// srTokens splits q on spaces, keeping "quoted phrases" together (quotes
// kept in the token).
func srTokens(q string) []string {
	var out []string
	var b strings.Builder
	inQ := false
	for _, r := range q {
		switch {
		case r == '"':
			inQ = !inQ
			b.WriteRune(r)
		case unicode.IsSpace(r) && !inQ:
			if b.Len() > 0 {
				out = append(out, b.String())
				b.Reset()
			}
		default:
			b.WriteRune(r)
		}
	}
	if b.Len() > 0 {
		out = append(out, b.String())
	}
	return out
}

// srParse parses a query string.
func srParse(q string) srQuery {
	var out srQuery
	for _, t := range srTokens(q) {
		if k, v, ok := strings.Cut(t, ":"); ok && v != "" && srModKeys[strings.ToLower(k)] {
			out.Mods = append(out.Mods, srMod{Key: strings.ToLower(k), Val: v})
			continue
		}
		t = strings.Trim(t, `"`)
		if strings.TrimSpace(t) != "" {
			out.Terms = append(out.Terms, t)
		}
	}
	return out
}

// String rebuilds the query: terms first, then modifiers.
func (q srQuery) String() string {
	parts := make([]string, 0, len(q.Terms)+len(q.Mods))
	for _, t := range q.Terms {
		if strings.ContainsAny(t, " \t") {
			t = `"` + t + `"`
		}
		parts = append(parts, t)
	}
	for _, m := range q.Mods {
		parts = append(parts, m.Key+":"+m.Val)
	}
	return strings.Join(parts, " ")
}

// Text is the free text part (terms joined by spaces).
func (q srQuery) Text() string { return strings.Join(q.Terms, " ") }

// Get returns the first value of key ("" if absent).
func (q srQuery) Get(key string) string {
	for _, m := range q.Mods {
		if m.Key == key {
			return m.Val
		}
	}
	return ""
}

// Has reports whether key:val is present (case-insensitive value).
func (q srQuery) Has(key, val string) bool {
	for _, m := range q.Mods {
		if m.Key == key && strings.EqualFold(m.Val, val) {
			return true
		}
	}
	return false
}

// Set replaces every key modifier with key:val (val "" removes them).
func (q srQuery) Set(key, val string) srQuery {
	out := srQuery{Terms: q.Terms}
	added := false
	for _, m := range q.Mods {
		if m.Key == key {
			if !added && val != "" {
				out.Mods = append(out.Mods, srMod{key, val})
				added = true
			}
			continue
		}
		out.Mods = append(out.Mods, m)
	}
	if !added && val != "" {
		out.Mods = append(out.Mods, srMod{key, val})
	}
	return out
}

// Toggle adds key:val, or removes it when present (other values of the
// same key are kept: has:link and has:pin can coexist).
func (q srQuery) Toggle(key, val string) srQuery {
	out := srQuery{Terms: q.Terms}
	found := false
	for _, m := range q.Mods {
		if m.Key == key && strings.EqualFold(m.Val, val) {
			found = true
			continue
		}
		out.Mods = append(out.Mods, m)
	}
	if !found {
		out.Mods = append(out.Mods, srMod{key, val})
	}
	return out
}

// ClearMods drops every modifier.
func (q srQuery) ClearMods() srQuery { return srQuery{Terms: q.Terms} }

// srRefID extracts the id from Slack refs: "<#C123|name>" → "C123",
// "<@U1>" → "U1"; other values return "".
func srRefID(v string) string {
	if len(v) < 4 || v[0] != '<' || v[len(v)-1] != '>' {
		return ""
	}
	v = v[1 : len(v)-1]
	if v == "" || (v[0] != '#' && v[0] != '@') {
		return ""
	}
	v = v[1:]
	if i := strings.IndexByte(v, '|'); i >= 0 {
		v = v[:i]
	}
	return v
}

// srDate formats a search date modifier value.
func srDate(t time.Time) string { return t.Format("2006-01-02") }

// srLast7 is the after: value meaning "the last 7 days" (today and the six
// days before; after: is exclusive).
func srLast7(now time.Time) string { return srDate(now.AddDate(0, 0, -7)) }

// ---- highlighting ----

// srSeg is a run of text, highlighted or not.
type srSeg struct {
	Text string
	Hit  bool
}

func srWordRune(r rune) bool {
	return unicode.IsLetter(r) || unicode.IsDigit(r) || r == '_' || r == '\''
}

// srHighlight splits s into segments, marking whole words that start with
// a term (Slack highlights "annoying" for "ann") and, for terms with
// punctuation or spaces, every case-insensitive occurrence.
func srHighlight(s string, terms []string) []srSeg {
	if s == "" {
		return nil
	}
	if len(terms) == 0 {
		return []srSeg{{Text: s}}
	}
	rs := []rune(s)
	low := make([]rune, len(rs))
	for i, r := range rs {
		low[i] = unicode.ToLower(r)
	}
	hit := make([]bool, len(rs))
	var words, phrases [][]rune
	for _, t := range terms {
		t = strings.Trim(t, `"*`)
		if t == "" {
			continue
		}
		ft := srFold(t)
		simple := true
		for _, r := range ft {
			if !srWordRune(r) {
				simple = false
				break
			}
		}
		if simple {
			words = append(words, ft)
		} else {
			phrases = append(phrases, ft)
		}
	}
	if len(words) > 0 {
		for i := 0; i < len(rs); {
			if !srWordRune(rs[i]) {
				i++
				continue
			}
			j := i
			for j < len(rs) && srWordRune(rs[j]) {
				j++
			}
			w := low[i:j]
			for _, t := range words {
				if len(t) <= len(w) && srRuneIndex(w[:len(t)], t) == 0 {
					for k := i; k < j; k++ {
						hit[k] = true
					}
					break
				}
			}
			i = j
		}
	}
	for _, p := range phrases {
		for i := srRuneIndex(low, p); i >= 0; i = srRuneIndexFrom(low, p, i+1) {
			for k := i; k < i+len(p); k++ {
				hit[k] = true
			}
		}
	}
	var out []srSeg
	start := 0
	for i := 1; i <= len(rs); i++ {
		if i == len(rs) || hit[i] != hit[start] {
			out = append(out, srSeg{Text: string(rs[start:i]), Hit: hit[start]})
			start = i
		}
	}
	return out
}

// srFlatten joins a multi-line message onto one line the way Slack's
// search previews do ("first line … second line").
func srFlatten(s string) string {
	lines := strings.Split(s, "\n")
	parts := lines[:0]
	for _, l := range lines {
		if l = strings.TrimSpace(l); l != "" {
			parts = append(parts, l)
		}
	}
	return strings.Join(parts, " … ")
}

// ---- recents (memory only) ----

var (
	srMu          sync.Mutex
	srRecentConvs []string // most recent first
	srRecentQs    []string
)

const (
	srMaxRecentConvs = 12
	srMaxRecentQs    = 8
)

// srPush moves v to the front of list, de-duplicated (case-insensitive for
// queries), capped at limit.
func srPush(list []string, v string, limit int, fold bool) []string {
	out := make([]string, 0, minInt(len(list)+1, limit))
	out = append(out, v)
	for _, x := range list {
		if x == v || (fold && strings.EqualFold(x, v)) {
			continue
		}
		if len(out) >= limit {
			break
		}
		out = append(out, x)
	}
	return out
}

// RecordRecentConversation feeds the quick switcher's "Recent places"
// (memory only). The ui root may call it whenever a conversation opens.
func RecordRecentConversation(id string) {
	if id == "" {
		return
	}
	srMu.Lock()
	srRecentConvs = srPush(srRecentConvs, id, srMaxRecentConvs, false)
	srMu.Unlock()
}

// srRecordQuery remembers a submitted search (memory only).
func srRecordQuery(q string) {
	q = strings.TrimSpace(q)
	if q == "" {
		return
	}
	srMu.Lock()
	srRecentQs = srPush(srRecentQs, q, srMaxRecentQs, true)
	srMu.Unlock()
}

func srRecents() (convs, queries []string) {
	srMu.Lock()
	defer srMu.Unlock()
	return append([]string(nil), srRecentConvs...), append([]string(nil), srRecentQs...)
}

// srShortDay is the compact time on quick-switcher message cards
// ("Today", "Yesterday", "Nov 3rd", "Nov 3rd, 2024").
func srShortDay(t, now time.Time) string {
	if t.IsZero() {
		return ""
	}
	y1, m1, d1 := t.Date()
	y2, m2, d2 := now.Date()
	switch {
	case y1 == y2 && m1 == m2 && d1 == d2:
		return "Today"
	case now.AddDate(0, 0, -1).Format("20060102") == t.Format("20060102"):
		return "Yesterday"
	case y1 == y2:
		return t.Format("Jan ") + ordinal(d1)
	}
	return t.Format("Jan ") + ordinal(d1) + t.Format(", 2006")
}
