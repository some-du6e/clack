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
	srKFile               // a file or canvas (search.autocomplete.files)
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
	// group DMs: every member (the tile draws the first other one) and
	// extra match keys (members' real names and handles; their display
	// names are already in Name).
	Members []string
	Keys    []string

	// set by srRank
	Tier         int
	Bold0, Bold1 int // bold byte range in Name
}

// srScore matches c against q over all its keys.
func srScore(c *srCand, q string) {
	t, s, e := srMatch(c.Name, q)
	c.Tier, c.Bold0, c.Bold1 = t, s, e
	srScoreKey(c, c.Alt, q)
	srScoreKey(c, c.Handle, q)
	for _, k := range c.Keys {
		srScoreKey(c, k, q)
	}
}

// srScoreKey raises c's tier when the secondary key k matches q better.
func srScoreKey(c *srCand, k, q string) {
	if k == "" {
		return
	}
	if t2, _, _ := srMatch(k, q); t2 > c.Tier {
		// a match on a secondary key ranks a notch lower than the same
		// match on the name.
		if t2 > srSubseq {
			t2--
		}
		if t2 > c.Tier {
			c.Tier, c.Bold0, c.Bold1 = t2, 0, 0
		}
	}
}

// srMpdmNames turns a group DM's internal name ("mpdm-ann--bo--cy-1") into
// its members' handles, the viewer's (self) left out. It is the fallback
// title for group DMs the sidebar doesn't know (search results).
func srMpdmNames(name, self string) []string {
	rest, ok := strings.CutPrefix(name, "mpdm-")
	if !ok || rest == "" {
		return nil
	}
	if i := strings.LastIndexByte(rest, '-'); i > 0 && i < len(rest)-1 {
		num := true
		for _, r := range rest[i+1:] {
			if r < '0' || r > '9' {
				num = false
				break
			}
		}
		if num {
			rest = rest[:i]
		}
	}
	var out []string
	for _, h := range strings.Split(rest, "--") {
		if h = strings.Trim(h, "-"); h != "" && !strings.EqualFold(h, self) {
			out = append(out, h)
		}
	}
	return out
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

// srModKeys are the modifiers Slack's search understands (the filters
// menus write in:, from:, with:, has:, is:, type:, before:, after:,
// on:; people type to:, during: and creator: too).
var srModKeys = map[string]bool{
	"in": true, "from": true, "to": true, "with": true, "before": true, "after": true,
	"on": true, "during": true, "has": true, "is": true, "type": true, "creator": true,
}

// srMod is one key:value modifier; Neg is the "-" exclusion
// (-from:@ann, -in:#random).
type srMod struct {
	Key, Val string
	Neg      bool
}

// srQuery is a parsed search query: free terms and modifiers, in order.
type srQuery struct {
	Terms  []string // words; quoted phrases keep their spaces (no quotes); "-word" excludes
	Quoted []bool   // Terms[i] was "quoted" (an exact phrase or word)
	Mods   []srMod
}

// srTokens splits q on spaces, keeping "quoted phrases" together (quotes
// kept in the token).
func srTokens(q string) []string {
	var out []string
	var b strings.Builder
	inQ := false
	for _, r := range q {
		switch {
		case r == '"' || r == '“' || r == '”':
			inQ = !inQ
			b.WriteRune('"')
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
		neg := len(t) > 1 && t[0] == '-'
		body := t
		if neg {
			body = t[1:]
		}
		if k, v, ok := strings.Cut(body, ":"); ok && v != "" && srModKeys[strings.ToLower(k)] {
			out.Mods = append(out.Mods, srMod{Key: strings.ToLower(k), Val: strings.Trim(v, `"`), Neg: neg})
			continue
		}
		quoted := strings.Contains(t, `"`)
		t = strings.Trim(t, `"`)
		if neg && quoted {
			t = "-" + strings.Trim(t[1:], `"`)
		}
		if strings.TrimSpace(strings.TrimPrefix(t, "-")) != "" {
			out.Terms = append(out.Terms, t)
			out.Quoted = append(out.Quoted, quoted)
		}
	}
	return out
}

func (q srQuery) quoted(i int) bool { return i < len(q.Quoted) && q.Quoted[i] }

// String rebuilds the query: terms first, then modifiers.
func (q srQuery) String() string {
	parts := make([]string, 0, len(q.Terms)+len(q.Mods))
	for i, t := range q.Terms {
		if q.quoted(i) || strings.ContainsAny(t, " \t") {
			if strings.HasPrefix(t, "-") && q.quoted(i) {
				t = `-"` + t[1:] + `"`
			} else {
				t = `"` + t + `"`
			}
		}
		parts = append(parts, t)
	}
	for _, m := range q.Mods {
		v := m.Val
		if strings.ContainsAny(v, " \t") {
			v = `"` + v + `"`
		}
		k := m.Key
		if m.Neg {
			k = "-" + k
		}
		parts = append(parts, k+":"+v)
	}
	return strings.Join(parts, " ")
}

// Text is the free text part (terms joined by spaces), exclusions left
// out: what the Channels and People tabs search for.
func (q srQuery) Text() string {
	var parts []string
	for _, t := range q.Terms {
		if !strings.HasPrefix(t, "-") {
			parts = append(parts, t)
		}
	}
	return strings.Join(parts, " ")
}

// Highlights are the terms results highlight (not the exclusions).
func (q srQuery) Highlights() []string {
	var out []string
	for _, t := range q.Terms {
		if !strings.HasPrefix(t, "-") {
			out = append(out, t)
		}
	}
	return out
}

// withMods is q's terms with mods.
func (q srQuery) withMods(mods []srMod) srQuery {
	return srQuery{Terms: q.Terms, Quoted: q.Quoted, Mods: mods}
}

// Get returns the first (non-excluded) value of key ("" if absent).
func (q srQuery) Get(key string) string {
	for _, m := range q.Mods {
		if m.Key == key && !m.Neg {
			return m.Val
		}
	}
	return ""
}

// Has reports whether key:val is present (case-insensitive value).
func (q srQuery) Has(key, val string) bool {
	for _, m := range q.Mods {
		if m.Key == key && !m.Neg && strings.EqualFold(m.Val, val) {
			return true
		}
	}
	return false
}

// Set replaces every key modifier with key:val (val "" removes them).
func (q srQuery) Set(key, val string) srQuery {
	var mods []srMod
	added := false
	for _, m := range q.Mods {
		if m.Key == key && !m.Neg {
			if !added && val != "" {
				mods = append(mods, srMod{Key: key, Val: val})
				added = true
			}
			continue
		}
		mods = append(mods, m)
	}
	if !added && val != "" {
		mods = append(mods, srMod{Key: key, Val: val})
	}
	return q.withMods(mods)
}

// Toggle adds key:val, or removes it when present (other values of the
// same key are kept: has:link and has:pin can coexist).
func (q srQuery) Toggle(key, val string) srQuery {
	var mods []srMod
	found := false
	for _, m := range q.Mods {
		if m.Key == key && !m.Neg && strings.EqualFold(m.Val, val) {
			found = true
			continue
		}
		mods = append(mods, m)
	}
	if !found {
		mods = append(mods, srMod{Key: key, Val: val})
	}
	return q.withMods(mods)
}

// ClearMods drops every modifier.
func (q srQuery) ClearMods() srQuery { return q.withMods(nil) }

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

// srPlace is a page in Recent places (Slack's view-navigation rows).
type srPlace struct{ title, icon string }

var (
	srPlaces  = map[string]srPlace{} // "view:<page>" -> its row
	srCurrent string                 // the open place: Recent places leaves it out
)

// RecordRecentPlace is the ui root's navigation hook: id is a
// conversation id or "view:<page>" with the page's title and icon.
// Slack's Recent places is this history, newest first, without the
// place you are on.
func RecordRecentPlace(id, title, icon string) {
	if id == "" {
		return
	}
	srMu.Lock()
	if strings.HasPrefix(id, "view:") {
		srPlaces[id] = srPlace{title, icon}
	}
	srCurrent = id
	srRecentConvs = srPush(srRecentConvs, id, srMaxRecentConvs, false)
	srMu.Unlock()
}

// srPlaceOf returns a recorded page's row.
func srPlaceOf(id string) (srPlace, bool) {
	srMu.Lock()
	defer srMu.Unlock()
	p, ok := srPlaces[id]
	return p, ok
}

func srCurrentPlace() string {
	srMu.Lock()
	defer srMu.Unlock()
	return srCurrent
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
