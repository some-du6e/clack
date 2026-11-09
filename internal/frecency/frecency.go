// Package frecency is a port of Slack's web-client frecency store: the
// "frecency" user pref (users.prefs.get / users.prefs.set) that ranks
// emoji, channels, people and commands by how often and how recently the
// viewer picked them.
//
// Sources (app.slack.com client-boot, 2026-11): RI+6 (the store: buckets,
// calculateScore, getHitsForQuery, scoreItems, createCacheRecord,
// countVisit), W3UT (recordFrecencyEvent), 1TmA (setFrecencyByApi,
// throttled 60 s), pRCT/ZNeq (normalizeText). Emoji ids are Ak7A
// constructEmojiFrecencyId: "E" + the unicode code points for a standard
// emoji ("E1f44d"), "E" + the name for a custom one ("Eparrot").
//
// The pref is a JSON object. Each key is either an item id (its record)
// or a normalized query text (the record, or an array of records, of
// what was picked after typing it):
//
//	{"E1f44d":{"id":"E1f44d","count":3,"visits":[ms,…]},
//	 "thu":[{"id":"E1f44d","count":2,"visits":[…]}, …]}
//
// A Cache keeps every entry it did not change byte for byte, so writing
// the pref back never drops what other Slack clients stored in it.
package frecency

import (
	"bytes"
	"encoding/json"
	"math"
	"sort"
	"strconv"
	"strings"
	"time"
	"unicode"

	"golang.org/x/text/unicode/norm"
)

// MaxVisits is how many visit timestamps a record keeps (countVisit).
const MaxVisits = 10

// InfSearch is the id Slack aggregates search counts under (INF_SEARCH).
const InfSearch = "INF_SEARCH"

// Record is one frecency record.
type Record struct {
	ID      string  `json:"id"`
	Count   int     `json:"count"`
	Visits  []int64 `json:"visits"` // unix ms, oldest first
	Reduced bool    `json:"_reduced,omitempty"`
}

// entry is one key's value: a record or an array of records.
type entry struct {
	raw  json.RawMessage // as parsed (nil once changed)
	recs []Record
	arr  bool
	ok   bool // recs decoded
}

func (e *entry) decode() {
	if e.ok {
		return
	}
	e.ok = true
	t := bytes.TrimSpace(e.raw)
	if len(t) > 0 && t[0] == '[' {
		e.arr = true
		_ = json.Unmarshal(t, &e.recs)
		return
	}
	var r Record
	if json.Unmarshal(t, &r) == nil && r.ID != "" {
		e.recs = []Record{r}
	}
}

// Cache is the parsed frecency pref. The zero value is empty and usable.
type Cache struct {
	keys []string // insertion order
	m    map[string]*entry
}

// Parse reads the pref value: a JSON object, or that object JSON-encoded
// as a string (how users.prefs.get returns it). Anything else is empty,
// as in getFrecencyCache.
func Parse(v []byte) *Cache {
	c := &Cache{m: map[string]*entry{}}
	v = bytes.TrimSpace(v)
	if len(v) > 0 && v[0] == '"' {
		var s string
		if json.Unmarshal(v, &s) != nil {
			return c
		}
		v = []byte(s)
	}
	dec := json.NewDecoder(bytes.NewReader(v))
	if t, err := dec.Token(); err != nil || t != json.Delim('{') {
		return &Cache{m: map[string]*entry{}}
	}
	for dec.More() {
		t, err := dec.Token()
		if err != nil {
			return &Cache{m: map[string]*entry{}}
		}
		k, _ := t.(string)
		var raw json.RawMessage
		if dec.Decode(&raw) != nil {
			return &Cache{m: map[string]*entry{}}
		}
		if _, dup := c.m[k]; !dup {
			c.keys = append(c.keys, k)
		}
		c.m[k] = &entry{raw: raw}
	}
	return c
}

// Clone copies c (entries are shared until changed).
func (c *Cache) Clone() *Cache {
	n := &Cache{m: make(map[string]*entry, len(c.mm()))}
	n.keys = append([]string(nil), c.keys...)
	for k, e := range c.m {
		cp := *e
		cp.recs = append([]Record(nil), e.recs...)
		n.m[k] = &cp
	}
	return n
}

func (c *Cache) mm() map[string]*entry {
	if c.m == nil {
		c.m = map[string]*entry{}
	}
	return c.m
}

// Len is the number of keys.
func (c *Cache) Len() int { return len(c.keys) }

// arrayIndex reports whether k is a JS array-index key (canonical uint32
// below 2^32-1), which Object.keys lists first, ascending.
func arrayIndex(k string) (uint64, bool) {
	if k == "" || (len(k) > 1 && k[0] == '0') {
		return 0, false
	}
	n, err := strconv.ParseUint(k, 10, 32)
	if err != nil || n == math.MaxUint32 {
		return 0, false
	}
	return n, true
}

// Keys returns the keys in JavaScript's Object.keys order.
func (c *Cache) Keys() []string {
	var idx, rest []string
	for _, k := range c.keys {
		if _, ok := arrayIndex(k); ok {
			idx = append(idx, k)
		} else {
			rest = append(rest, k)
		}
	}
	if len(idx) == 0 {
		return rest
	}
	sort.Slice(idx, func(i, j int) bool {
		a, _ := arrayIndex(idx[i])
		b, _ := arrayIndex(idx[j])
		return a < b
	})
	return append(idx, rest...)
}

// get returns key k's records and whether the value is an array.
func (c *Cache) get(k string) ([]Record, bool, bool) {
	e, ok := c.mm()[k]
	if !ok {
		return nil, false, false
	}
	e.decode()
	return e.recs, e.arr, true
}

func (c *Cache) set(k string, recs []Record, arr bool) {
	if _, ok := c.mm()[k]; !ok {
		c.keys = append(c.keys, k)
	}
	c.m[k] = &entry{recs: recs, arr: arr, ok: true}
}

// Item is the record stored under its own id (nil when absent or when
// that key holds an array, which getCount/weightVisits treat as empty).
func (c *Cache) Item(id string) *Record {
	recs, arr, ok := c.get(id)
	if !ok || arr || len(recs) == 0 {
		return nil
	}
	r := recs[0]
	return &r
}

// MarshalJSON writes the pref object, untouched entries verbatim.
func (c *Cache) MarshalJSON() ([]byte, error) {
	var b bytes.Buffer
	b.WriteByte('{')
	for i, k := range c.Keys() {
		if i > 0 {
			b.WriteByte(',')
		}
		kb, _ := json.Marshal(k)
		b.Write(kb)
		b.WriteByte(':')
		e := c.m[k]
		if e.raw != nil {
			b.Write(e.raw)
			continue
		}
		var v any = e.recs
		if !e.arr {
			if len(e.recs) == 0 {
				b.WriteString("null")
				continue
			}
			v = e.recs[0]
		}
		vb, err := json.Marshal(v)
		if err != nil {
			return nil, err
		}
		b.Write(vb)
	}
	b.WriteByte('}')
	return b.Bytes(), nil
}

// ---- scoring (RI+6) ----

type bucket struct {
	age    time.Duration
	weight float64
}

// buckets are RI+6's b: 4 h, 1 d, 3 d, 1 w, 30 d, 90 d.
var buckets = [...]bucket{
	{4 * time.Hour, 100}, {24 * time.Hour, 80}, {72 * time.Hour, 60},
	{7 * 24 * time.Hour, 40}, {30 * 24 * time.Hour, 20}, {90 * 24 * time.Hour, 10},
}

// weighVisit is the weight of one visit age ms old.
func weighVisit(ageMS int64) float64 {
	for _, b := range buckets {
		if ageMS < b.age.Milliseconds() {
			return b.weight
		}
	}
	return 0
}

// WeightVisits is the mean visit weight, halved for _reduced records.
func WeightVisits(r *Record, now time.Time) float64 {
	if r == nil || len(r.Visits) == 0 {
		return 0
	}
	ms := now.UnixMilli()
	t := 0.0
	for i := len(r.Visits) - 1; i >= 0; i-- {
		t += weighVisit(ms - r.Visits[i])
	}
	if r.Reduced {
		t *= .5
	}
	return t / float64(len(r.Visits))
}

func count(r *Record) int {
	if r == nil {
		return 0
	}
	return r.Count
}

// Score is calculateScore: weighted visits × count.
func Score(r *Record, now time.Time) float64 {
	return WeightVisits(r, now) * float64(count(r))
}

// latestMS is latestVisit in ms (0 without visits).
func latestMS(r *Record) int64 {
	if r == nil || len(r.Visits) == 0 {
		return 0
	}
	m := r.Visits[0]
	for _, v := range r.Visits[1:] {
		m = max(m, v)
	}
	return m
}

// Hit is getHitsForQuery's best record for one id.
type Hit struct {
	Record
	Exact bool // the record is stored under the query itself
}

// Hits is getHitsForQuery(cache, query, true) without the INF_SEARCH
// aggregate: for every key that contains the normalized query or is
// contained in it (every key for an empty query), the highest-scoring
// record per id.
func (c *Cache) Hits(query string, now time.Time) map[string]*Hit {
	q := NormalizeText(query)
	out := map[string]*Hit{}
	for _, k := range c.Keys() {
		if !strings.Contains(k, q) && !strings.Contains(q, k) {
			continue
		}
		recs, _, _ := c.get(k)
		exact := k == q
		for i := range recs {
			r := recs[i]
			if h, ok := out[r.ID]; ok {
				if Score(&r, now) > Score(&h.Record, now) {
					out[r.ID] = &Hit{Record: r, Exact: exact}
				}
				continue
			}
			out[r.ID] = &Hit{Record: r, Exact: exact}
		}
	}
	return out
}

// Scored is one item's scoreItems features.
type Scored struct {
	ID                 string
	HitCount           int
	ItemCount          int
	WeightHitVisits    float64
	WeightItemVisits   float64
	RawFrecencyScore   float64 // normalized to 0..100 over the list
	LogHitCountScaled  float64
	LogItemCountScaled float64
	FrecencyScore      float64 // RawFrecencyScore + bonus points
	Index              int     // position in the input list
}

// ScoreItems is scoreItems for ids (in list order). bonus (optional)
// returns an id's bonus points (Slack's bonusPoints functions).
func (c *Cache) ScoreItems(hits map[string]*Hit, ids []string, now time.Time, bonus func(id string) float64) []Scored {
	out := make([]Scored, len(ids))
	var maxRaw, maxLH, maxLI float64
	day := int64(24 * time.Hour / time.Millisecond)
	for i, id := range ids {
		var hr *Record
		exact := false
		if h := hits[id]; h != nil {
			hr, exact = &h.Record, h.Exact
		}
		ir := c.Item(id)
		o, l := WeightVisits(hr, now), float64(count(hr))
		d, u := WeightVisits(ir, now), float64(count(ir))
		a := l*o + u*d
		if exact {
			a = 2*l*o + u*d
		}
		if s := max(latestMS(hr), latestMS(ir)); s != 0 {
			a *= math.Pow(.9755, math.Floor(float64(now.UnixMilli()-s)/float64(day)))
		}
		out[i] = Scored{ID: id, HitCount: int(l), ItemCount: int(u), WeightHitVisits: o, WeightItemVisits: d,
			RawFrecencyScore: a, LogHitCountScaled: math.Log1p(l), LogItemCountScaled: math.Log1p(u), Index: i}
		if i == 0 || a > maxRaw {
			maxRaw = a
		}
		if i == 0 || out[i].LogHitCountScaled > maxLH {
			maxLH = out[i].LogHitCountScaled
		}
		if i == 0 || out[i].LogItemCountScaled > maxLI {
			maxLI = out[i].LogItemCountScaled
		}
	}
	for i := range out {
		s := &out[i]
		if s.RawFrecencyScore > 0 {
			s.RawFrecencyScore = s.RawFrecencyScore / maxRaw * 100
		}
		if s.LogHitCountScaled > 0 {
			s.LogHitCountScaled /= maxLH
		}
		if s.LogItemCountScaled > 0 {
			s.LogItemCountScaled /= maxLI
		}
		s.FrecencyScore = s.RawFrecencyScore
		if bonus != nil {
			s.FrecencyScore += bonus(s.ID)
		}
	}
	return out
}

// SortItems is sortItems: frecencyScore descending, ties in list order.
func SortItems(s []Scored) {
	sort.SliceStable(s, func(i, j int) bool {
		if s[i].FrecencyScore == s[j].FrecencyScore {
			return s[i].Index < s[j].Index
		}
		return s[i].FrecencyScore > s[j].FrecencyScore
	})
}

// ---- recording (W3UT) ----

func newRecord(id string, now time.Time) Record {
	return Record{ID: id, Count: 1, Visits: []int64{now.UnixMilli()}}
}

// countVisit adds a visit (the oldest of 10 falls off); _reduced is
// dropped, as countVisit builds a fresh {id, count, visits}.
func countVisit(r Record, now time.Time) Record {
	v := append([]int64(nil), r.Visits...)
	if len(v) == MaxVisits {
		v = v[1:]
	}
	return Record{ID: r.ID, Count: r.Count + 1, Visits: append(v, now.UnixMilli())}
}

// Record is recordFrecencyEvent {id, text}: it bumps id under the
// normalized text (when text is non-empty) and under id itself (a first
// record there is _reduced), changing c in place.
func (c *Cache) Record(id, text string, now time.Time) {
	if id == "" {
		return
	}
	if text != "" {
		k := NormalizeText(text)
		recs, arr, ok := c.get(k)
		switch {
		case ok && !arr && len(recs) > 0:
			if recs[0].ID == id {
				c.set(k, []Record{countVisit(recs[0], now)}, false)
			} else {
				c.set(k, []Record{recs[0], newRecord(id, now)}, true)
			}
		case ok && arr:
			n := append([]Record(nil), recs...)
			found := false
			for i := range n {
				if n[i].ID == id {
					n[i] = countVisit(n[i], now)
					found = true
					break
				}
			}
			if !found {
				n = append(n, newRecord(id, now))
			}
			c.set(k, n, true)
		default:
			c.set(k, []Record{newRecord(id, now)}, false)
		}
	}
	recs, arr, ok := c.get(id)
	switch {
	case ok && arr:
		n := make([]Record, len(recs))
		other := false
		for i, r := range recs {
			if r.ID == id {
				n[i] = countVisit(r, now)
			} else {
				n[i] = r
				other = true
			}
		}
		if other {
			n = append(n, newRecord(id, now))
		}
		c.set(id, n, true)
	case ok && len(recs) > 0:
		c.set(id, []Record{countVisit(recs[0], now)}, false)
	case id != InfSearch:
		r := newRecord(id, now)
		r.Reduced = true
		c.set(id, []Record{r}, false)
	}
}

// ---- text (ZNeq formatQuery, pRCT normalizeStringForMatching) ----

// NormalizeText is RI+6 normalizeText: formatQuery drops one leading
// : @ # / + (and their full-width forms), then the text is trimmed,
// lower-cased, de-accented and its whitespace runs collapsed (text that
// is already plain ASCII lower case with single spaces is kept as is).
func NormalizeText(s string) string {
	if s == "" {
		return ""
	}
	for _, p := range []string{":", "@", "＠", "#", "＃", "/", "／", "+", "＋"} {
		if strings.HasPrefix(s, p) {
			s = s[len(p):]
			break
		}
	}
	if s == "" || plain(s) {
		return s
	}
	s = strings.TrimSpace(s)
	if s == "" {
		return ""
	}
	s = deburr(strings.ToLower(s))
	return strings.Join(strings.Fields(s), " ")
}

// plain is !/[^\x20-\x7f]|[A-Z]|^ | $| {2}/.test(s).
func plain(s string) bool {
	if s[0] == ' ' || s[len(s)-1] == ' ' || strings.Contains(s, "  ") {
		return false
	}
	for i := 0; i < len(s); i++ {
		if c := s[i]; c < 0x20 || c > 0x7f || (c >= 'A' && c <= 'Z') {
			return false
		}
	}
	return true
}

func deburr(s string) string {
	return strings.Map(func(r rune) rune {
		if unicode.Is(unicode.Mn, r) {
			return -1
		}
		return r
	}, norm.NFKD.String(s))
}
