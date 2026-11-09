package acrank

import (
	"bytes"
	"encoding/json"
	"math"
	"strings"
	"time"
)

// Frecency is Slack's frecency cache: the "frecency" user pref (RI+6,
// W3UT). Keys are either an item id ("U…", "C…", "E1f44d" for emoji)
// holding that item's visits, or a normalized query text holding the
// items picked for it (one record or a list). Keys keep their JSON order:
// Slack walks them in object order and the first of equal scores wins.
type Frecency struct {
	Keys []string
	M    map[string]FrecencyEntry
}

// FrecencyEntry is one key's value: a single record or a list.
type FrecencyEntry struct {
	List bool
	Recs []FrecencyRecord
}

// FrecencyRecord is {id, count, visits[, _reduced]}. Visits are ms
// timestamps, at most 10, oldest first.
type FrecencyRecord struct {
	ID      string    `json:"id"`
	Count   float64   `json:"count"`
	Visits  []float64 `json:"visits"`
	Reduced *bool     `json:"_reduced,omitempty"`
}

func (r FrecencyRecord) reduced() bool { return r.Reduced != nil && *r.Reduced }

// infSearch is the aggregate record Slack keeps for searches.
const infSearch = "INF_SEARCH"

// ParseFrecency reads the pref. users.prefs.get returns it as a JSON
// string holding the object; a bare object works too. Garbage gives an
// empty cache, as getFrecencyCache does.
func ParseFrecency(raw []byte) *Frecency {
	f := &Frecency{M: map[string]FrecencyEntry{}}
	raw = bytes.TrimSpace(raw)
	if len(raw) > 0 && raw[0] == '"' {
		var s string
		if json.Unmarshal(raw, &s) != nil {
			return f
		}
		raw = []byte(s)
	}
	dec := json.NewDecoder(bytes.NewReader(raw))
	if tok, err := dec.Token(); err != nil || tok != json.Delim('{') {
		return f
	}
	for dec.More() {
		tok, err := dec.Token()
		if err != nil {
			return &Frecency{M: map[string]FrecencyEntry{}}
		}
		k, _ := tok.(string)
		var v json.RawMessage
		if dec.Decode(&v) != nil {
			return &Frecency{M: map[string]FrecencyEntry{}}
		}
		var e FrecencyEntry
		v = bytes.TrimSpace(v)
		if len(v) > 0 && v[0] == '[' {
			e.List = true
			if json.Unmarshal(v, &e.Recs) != nil {
				continue
			}
		} else {
			var r FrecencyRecord
			if json.Unmarshal(v, &r) != nil || r.ID == "" {
				continue
			}
			e.Recs = []FrecencyRecord{r}
		}
		if _, dup := f.M[k]; !dup {
			f.Keys = append(f.Keys, k)
		}
		f.M[k] = e
	}
	return f
}

// MarshalJSON writes the object in key order (the pref's value).
func (f *Frecency) MarshalJSON() ([]byte, error) {
	var b bytes.Buffer
	b.WriteByte('{')
	for i, k := range f.Keys {
		if i > 0 {
			b.WriteByte(',')
		}
		kb, _ := json.Marshal(k)
		b.Write(kb)
		b.WriteByte(':')
		e := f.M[k]
		var vb []byte
		if e.List {
			vb, _ = json.Marshal(e.Recs)
		} else if len(e.Recs) > 0 {
			vb, _ = json.Marshal(e.Recs[0])
		} else {
			vb = []byte("null")
		}
		b.Write(vb)
	}
	b.WriteByte('}')
	return b.Bytes(), nil
}

// Clone copies f (records are copied, visit slices shared read-only).
func (f *Frecency) Clone() *Frecency {
	if f == nil {
		return &Frecency{M: map[string]FrecencyEntry{}}
	}
	g := &Frecency{Keys: append([]string(nil), f.Keys...), M: make(map[string]FrecencyEntry, len(f.M))}
	for k, e := range f.M {
		g.M[k] = FrecencyEntry{List: e.List, Recs: append([]FrecencyRecord(nil), e.Recs...)}
	}
	return g
}

func (f *Frecency) set(k string, e FrecencyEntry) {
	if _, ok := f.M[k]; !ok {
		f.Keys = append(f.Keys, k)
	}
	f.M[k] = e
}

// NormalizeText is normalizeText: the trigger dropped, then Normalize.
func NormalizeText(s string) string { return Normalize(FormatQuery(s)) }

func newRecord(id string, now time.Time) FrecencyRecord {
	return FrecencyRecord{ID: id, Count: 1, Visits: []float64{float64(now.UnixMilli())}}
}

// countVisit adds a visit, keeping the last 10 (it drops _reduced).
func countVisit(r FrecencyRecord, now time.Time) FrecencyRecord {
	v := append([]float64(nil), r.Visits...)
	if len(v) == 10 {
		v = v[1:]
	}
	v = append(v, float64(now.UnixMilli()))
	return FrecencyRecord{ID: r.ID, Count: r.Count + 1, Visits: v}
}

// Record is recordFrecencyEvent: a pick of id for the typed text (the
// autocomplete's matchText, trigger included; "" for a plain visit). It
// returns the new cache; f is unchanged.
func (f *Frecency) Record(id, text string, now time.Time) *Frecency {
	d := f.Clone()
	if text != "" {
		k := NormalizeText(text)
		t, ok := d.M[k]
		switch {
		case ok && !t.List && len(t.Recs) > 0:
			if t.Recs[0].ID == id {
				d.set(k, FrecencyEntry{Recs: []FrecencyRecord{countVisit(t.Recs[0], now)}})
			} else {
				d.set(k, FrecencyEntry{List: true, Recs: []FrecencyRecord{t.Recs[0], newRecord(id, now)}})
			}
		case ok && t.List:
			recs := append([]FrecencyRecord(nil), t.Recs...)
			found := false
			for i := range recs {
				if recs[i].ID == id {
					recs[i] = countVisit(recs[i], now)
					found = true
					break
				}
			}
			if !found {
				recs = append(recs, newRecord(id, now))
			}
			d.set(k, FrecencyEntry{List: true, Recs: recs})
		default:
			d.set(k, FrecencyEntry{Recs: []FrecencyRecord{newRecord(id, now)}})
		}
	}
	u, ok := d.M[id]
	switch {
	case ok && u.List:
		recs := make([]FrecencyRecord, 0, len(u.Recs)+1)
		other := false
		for _, r := range u.Recs {
			if r.ID == id {
				r = countVisit(r, now)
			} else {
				other = true
			}
			recs = append(recs, r)
		}
		if other {
			recs = append(recs, newRecord(id, now))
		}
		d.set(id, FrecencyEntry{List: true, Recs: recs})
	case ok && len(u.Recs) > 0:
		d.set(id, FrecencyEntry{Recs: []FrecencyRecord{countVisit(u.Recs[0], now)}})
	case id != infSearch:
		r := newRecord(id, now)
		t := true
		r.Reduced = &t
		d.set(id, FrecencyEntry{Recs: []FrecencyRecord{r}})
	}
	return d
}

// frecency buckets: age limit, points.
var frecBuckets = []struct {
	age    time.Duration
	points float64
}{
	{4 * time.Hour, 100}, {24 * time.Hour, 80}, {72 * time.Hour, 60},
	{7 * 24 * time.Hour, 40}, {30 * 24 * time.Hour, 20}, {90 * 24 * time.Hour, 10},
}

func weighVisit(age time.Duration) float64 {
	for _, b := range frecBuckets {
		if age < b.age {
			return b.points
		}
	}
	return 0
}

// weightVisits is the mean of the visits' points (halved when reduced).
func weightVisits(r *FrecencyRecord, now time.Time) float64 {
	if r == nil || len(r.Visits) == 0 {
		return 0
	}
	ms := float64(now.UnixMilli())
	t := 0.0
	for i := len(r.Visits) - 1; i >= 0; i-- {
		t += weighVisit(time.Duration(ms-r.Visits[i]) * time.Millisecond)
	}
	if r.reduced() {
		t *= 0.5
	}
	return t / float64(len(r.Visits))
}

func calcScore(r *FrecencyRecord, now time.Time) float64 {
	if r == nil {
		return 0
	}
	return weightVisits(r, now) * r.Count
}

func latestVisit(r *FrecencyRecord) float64 {
	if r == nil || len(r.Visits) == 0 {
		return 0
	}
	m := r.Visits[0]
	for _, v := range r.Visits[1:] {
		m = math.Max(m, v)
	}
	return m / 1000
}

type hit struct {
	rec   FrecencyRecord
	exact bool
}

// hitsFor is getHitsForQuery(…, true): every text key that contains the
// query or is contained in it lends its records; per id the best scoring
// record wins (ties: the first key in order).
func (f *Frecency) hitsFor(q string, now time.Time) map[string]hit {
	out := map[string]hit{}
	if f == nil {
		return out
	}
	i := NormalizeText(q)
	for _, k := range f.Keys {
		if !(strings.Contains(k, i) || strings.Contains(i, k)) {
			continue
		}
		exact := k == i
		for _, r := range f.M[k].Recs {
			if r.ID == infSearch {
				continue
			}
			if old, ok := out[r.ID]; ok {
				if calcScore(&r, now) > calcScore(&old.rec, now) {
					out[r.ID] = hit{r, exact}
				}
				continue
			}
			out[r.ID] = hit{r, exact}
		}
	}
	return out
}

// item returns the id key's own record (nil for a list: Slack reads
// count/visits off the array and gets nothing useful).
func (f *Frecency) item(id string) *FrecencyRecord {
	if f == nil {
		return nil
	}
	e, ok := f.M[id]
	if !ok || e.List || len(e.Recs) == 0 {
		return nil
	}
	r := e.Recs[0]
	return &r
}

// frecScore is what scoreItems adds to a result.
type frecScore struct {
	raw, score               float64
	weightHit, weightItem    float64
	hitCount, itemCount      float64
	logHitScaled, logItemSca float64
	bonus                    map[string]float64
	origIndex                int
}

// scoreItems is scoreByFrecency: per item the hit (query) and item (id)
// visits, the raw score normalized to 0–100 over the list, and the bonus
// points (bonus is getFrecencyBonusPointsFunction).
func (f *Frecency) scoreItems(q string, ids []string, now time.Time, bonus func(i int, fs *frecScore) map[string]float64) []frecScore {
	hits := f.hitsFor(q, now)
	out := make([]frecScore, len(ids))
	nowS := float64(now.UnixMilli())
	maxRaw, maxHit, maxItem := 0.0, 0.0, 0.0
	for i, id := range ids {
		var r *FrecencyRecord
		var exact bool
		if h, ok := hits[id]; ok {
			rr := h.rec
			r, exact = &rr, h.exact
		}
		s := f.item(id)
		o, l := weightVisits(r, now), 0.0
		if r != nil {
			l = r.Count
		}
		d, u := weightVisits(s, now), 0.0
		if s != nil {
			u = s.Count
		}
		a := l*o + u*d
		if exact {
			a = 2*l*o + u*d
		}
		if sm := math.Max(1000*latestVisit(r), 1000*latestVisit(s)); sm != 0 {
			a *= math.Pow(0.9755, math.Floor((nowS-sm)/864e5))
		}
		fs := frecScore{raw: a, score: a, weightHit: o, hitCount: l, weightItem: d, itemCount: u,
			logHitScaled: math.Log1p(l), logItemSca: math.Log1p(u), origIndex: i}
		out[i] = fs
		maxRaw, maxHit, maxItem = math.Max(maxRaw, a), math.Max(maxHit, fs.logHitScaled), math.Max(maxItem, fs.logItemSca)
	}
	for i := range out {
		fs := &out[i]
		if fs.raw > 0 {
			fs.raw = fs.raw / maxRaw * 100
		}
		if fs.logHitScaled > 0 {
			fs.logHitScaled /= maxHit
		}
		if fs.logItemSca > 0 {
			fs.logItemSca /= maxItem
		}
		fs.bonus = bonus(i, fs)
		sum := 0.0
		for _, v := range fs.bonus {
			sum += v
		}
		fs.score = fs.raw + sum
	}
	return out
}
