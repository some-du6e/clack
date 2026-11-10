package popups

import (
	"context"
	"encoding/json"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/some-du6e/clack/internal/emoji"
)

// Emoji data for the picker and the composer's ":" autocomplete.
//
// The standard table (internal/emoji.StandardData(), iamcal order) is parsed
// once. An emojiIndex combines it with the workspace's custom emoji
// (Hack Club has ~65,000) into id-indexed slices: ids [0, nStd) are standard
// emoji, the rest custom emoji sorted by name. Custom aliases are attached
// to their target as extra names (they are not separate grid cells), broken
// aliases are dropped. The search table holds every name lowercased and
// sorted by (length, name), so one linear scan yields each rank bucket
// already in Slack's order; only the handful of frecency-boosted hits
// are sorted per keystroke.

// Picker groups, in display order.
const (
	epGroupFrequent = iota
	epGroupSmileys
	epGroupAnimals
	epGroupFood
	epGroupTravel
	epGroupActivities
	epGroupObjects
	epGroupSymbols
	epGroupFlags
	epGroupCustom
	epNumGroups
)

// epGroupSearch is the list slot used for search results.
const epGroupSearch = epNumGroups

var epGroupNames = [epNumGroups + 1]string{
	"Frequently Used", "Smileys & People", "Animals & Nature", "Food & Drink", "Travel & Places",
	"Activities", "Objects", "Symbols", "Flags", "Custom", "Search results",
}

// epTabNames are the category tab tooltips (tab i scrolls to group i;
// tab 0 is Search, whose group is Frequently Used).
var epTabNames = [epNumGroups]string{
	"Search", "Smileys & People", "Animals & Nature", "Food & Drink", "Travel & Places",
	"Activities", "Objects", "Symbols", "Flags", "Custom",
}

// skin tone modifier code points for tones 2..6.
var epSkinCodes = [5]string{"1F3FB", "1F3FC", "1F3FD", "1F3FE", "1F3FF"}

// epStdTable is the parsed standard emoji table.
type epStdTable struct {
	names  [][]string // first is primary
	image  []string   // file stem
	group  []uint8
	skins  []int16     // index into skin, -1 none
	skin   [][5]string // file stems for tones 2..6
	byName map[string]int32
}

var (
	epStdOnce sync.Once
	epStd     *epStdTable
)

func epStandard() *epStdTable {
	epStdOnce.Do(func() { epStd = epParseStd(emoji.StandardData()) })
	return epStd
}

// epParseStd parses an iamcal-style table ({"emoji":[{n,i,c,s}]}).
func epParseStd(b []byte) *epStdTable {
	var f struct {
		Emoji []struct {
			Names []string          `json:"n"`
			Image string            `json:"i"`
			Cat   int               `json:"c"`
			Skins map[string]string `json:"s"`
		} `json:"emoji"`
	}
	_ = json.Unmarshal(b, &f)
	t := &epStdTable{byName: make(map[string]int32, len(f.Emoji)*2)}
	for _, e := range f.Emoji {
		if len(e.Names) == 0 || e.Image == "" {
			continue
		}
		id := int32(len(t.names))
		g := uint8(epGroupSmileys)
		if e.Cat >= 2 && e.Cat <= 8 {
			g = uint8(e.Cat)
		}
		sk := int16(-1)
		if len(e.Skins) > 0 {
			var v [5]string
			ok := true
			for i, c := range epSkinCodes {
				if v[i] = e.Skins[c]; v[i] == "" {
					ok = false
				}
			}
			if ok {
				sk = int16(len(t.skin))
				t.skin = append(t.skin, v)
			}
		}
		t.names = append(t.names, e.Names)
		t.image = append(t.image, e.Image)
		t.group = append(t.group, g)
		t.skins = append(t.skins, sk)
		for _, n := range e.Names {
			if _, dup := t.byName[n]; !dup {
				t.byName[n] = id
			}
		}
	}
	// aliases Slack accepts that the table may lack
	for a, n := range map[string]string{"thumbsup": "+1", "thumbsdown": "-1", "simple_smile": "slightly_smiling_face"} {
		if id, ok := t.byName[n]; ok {
			if _, exists := t.byName[a]; !exists {
				t.byName[a] = id
				t.names[id] = append(t.names[id][:len(t.names[id]):len(t.names[id])], a)
			}
		}
	}
	return t
}

// epKey identifies a built index.
type epKey struct {
	n     int // len(custom map)
	style string
}

// emojiIndex is the combined standard + custom emoji table.
type emojiIndex struct {
	key   epKey
	std   *epStdTable
	nStd  int
	names []string // id -> primary name
	urls  []string // id -> image URL (default tone)
	skin  []int16  // id -> skin URL set, -1 none
	skins [][5]string
	extra map[int32][]string // custom alias names attached to id
	group [epNumGroups][]int32

	byName map[string]int32 // every name (incl. aliases) -> id

	// search table sorted by (len, lower name)
	sLower []string
	sName  []string
	sID    []int32
}

func epStdURL(style, stem string) string {
	return emoji.CDN + style + "-medium/" + stem + ".png"
}

// epBuild builds the index. custom maps name -> URL or "alias:target".
func epBuild(std *epStdTable, style string, custom map[string]string) *emojiIndex {
	if style == "" {
		style = "apple"
	}
	nStd := len(std.names)
	x := &emojiIndex{key: epKey{len(custom), style}, std: std, nStd: nStd}
	n := nStd + len(custom)
	x.names = make([]string, nStd, n)
	x.urls = make([]string, nStd, n)
	x.skin = make([]int16, nStd, n)
	x.byName = make(map[string]int32, len(std.byName)+len(custom))
	x.skins = make([][5]string, len(std.skin))
	for i, v := range std.skin {
		for t, stem := range v {
			x.skins[i][t] = epStdURL(style, stem)
		}
	}
	for i := 0; i < nStd; i++ {
		x.names[i] = std.names[i][0]
		x.urls[i] = epStdURL(style, std.image[i])
		x.skin[i] = std.skins[i]
		g := std.group[i]
		x.group[g] = append(x.group[g], int32(i))
	}
	for name, id := range std.byName {
		x.byName[name] = id
	}

	var direct, aliases []string
	for name, v := range custom {
		if strings.HasPrefix(v, "alias:") {
			aliases = append(aliases, name)
		} else if v != "" {
			direct = append(direct, name)
		}
	}
	sort.Strings(direct)
	sort.Strings(aliases)
	for _, name := range direct {
		if _, isStd := x.byName[name]; isStd {
			continue // a custom emoji can't shadow a standard name
		}
		id := int32(len(x.names))
		x.names = append(x.names, name)
		x.urls = append(x.urls, custom[name])
		x.skin = append(x.skin, -1)
		x.byName[name] = id
		x.group[epGroupCustom] = append(x.group[epGroupCustom], id)
	}
	if len(aliases) > 0 {
		x.extra = make(map[int32][]string)
	}
	for _, name := range aliases {
		if _, taken := x.byName[name]; taken {
			continue
		}
		id, ok := epResolveAlias(x, custom, name)
		if !ok {
			continue // broken alias
		}
		x.byName[name] = id
		x.extra[id] = append(x.extra[id], name)
	}

	// search table
	type ent struct {
		lower, name string
		id          int32
	}
	ents := make([]ent, 0, len(x.byName))
	for name, id := range x.byName {
		ents = append(ents, ent{strings.ToLower(name), name, id})
	}
	sort.Slice(ents, func(i, j int) bool {
		a, b := ents[i].lower, ents[j].lower
		if len(a) != len(b) {
			return len(a) < len(b)
		}
		if a != b {
			return a < b
		}
		return ents[i].name < ents[j].name
	})
	x.sLower = make([]string, len(ents))
	x.sName = make([]string, len(ents))
	x.sID = make([]int32, len(ents))
	for i, e := range ents {
		x.sLower[i], x.sName[i], x.sID[i] = e.lower, e.name, e.id
	}
	return x
}

// epResolveAlias follows "alias:" chains (5 hops) to a custom or standard
// emoji id.
func epResolveAlias(x *emojiIndex, custom map[string]string, name string) (int32, bool) {
	target := strings.TrimPrefix(custom[name], "alias:")
	for hop := 0; hop < 5; hop++ {
		if i := strings.Index(target, "::"); i >= 0 {
			target = target[:i]
		}
		v, ok := custom[target]
		if !ok {
			id, ok := x.std.byName[target]
			return id, ok
		}
		if !strings.HasPrefix(v, "alias:") {
			id, ok := x.byName[target]
			return id, ok
		}
		target = strings.TrimPrefix(v, "alias:")
	}
	return 0, false
}

// url is the image URL of id at skin tone t (1 = default, 2..6).
func (x *emojiIndex) url(id int32, tone int) string {
	if tone >= 2 && tone <= 6 {
		if s := x.skin[id]; s >= 0 {
			return x.skins[s][tone-2]
		}
	}
	return x.urls[id]
}

// hasSkins reports whether id has skin tone variants.
func (x *emojiIndex) hasSkins(id int32) bool { return x.skin[id] >= 0 }

func (x *emojiIndex) custom(id int32) bool { return id >= int32(x.nStd) }

// allNames lists id's names, primary first.
func (x *emojiIndex) allNames(id int32) []string {
	var out []string
	if int(id) < x.nStd {
		out = append(out, x.std.names[id]...)
	} else {
		out = append(out, x.names[id])
	}
	return append(out, x.extra[id]...)
}

// lookup resolves a name (":x:", skin tone suffix allowed) to an id.
func (x *emojiIndex) lookup(name string) (int32, bool) {
	name = strings.Trim(name, ":")
	if i := strings.Index(name, "::"); i >= 0 {
		name = name[:i]
	}
	id, ok := x.byName[name]
	return id, ok
}

// ---- search ----

// epRank ranks name against q (both lowercase): 0 exact, 1 prefix,
// 2 at a word boundary (after _ - +), 3 other substring, -1 no match.
func epRank(name, q string) int {
	i := strings.Index(name, q)
	if i < 0 {
		return -1
	}
	if i == 0 {
		if len(name) == len(q) {
			return 0
		}
		return 1
	}
	for {
		if c := name[i-1]; c == '_' || c == '-' || c == '+' {
			return 2
		}
		j := strings.Index(name[i+1:], q)
		if j < 0 {
			return 3
		}
		i += 1 + j
	}
}

// epSearcher holds reusable scratch buffers so a keystroke allocates only
// the result slices (and nothing proportional to the emoji count once
// warm).
type epSearcher struct {
	stamp   []uint32
	best    []uint8
	gen     uint32
	buckets [4][]int32 // search table rows, (len, name) order
	boost   [4][]int32 // rows of emoji with a frecency score, per tier
	ids     []int32
	names   []int32 // matched search-table row per result

	// frecency boost: fscore[id] is valid when fstamp[id] == gen
	fstamp []uint32
	fscore []float32
	frec   []epScored
	frecV  uint64
	frecAt time.Time
	fixed  bool // frec was set by setFrecency (tests); don't reload
}

// epFrecTTL is how long a frecency snapshot is reused (scores age slowly).
const epFrecTTL = time.Minute

// setFrecency pins the scores used for boosting (name -> score) instead of
// the persisted frecency store. Used by tests.
func (s *epSearcher) setFrecency(scores map[string]float64) {
	s.fixed = true
	s.frec = s.frec[:0]
	for n, v := range scores {
		if v > 0 {
			s.frec = append(s.frec, epScored{n, v})
		}
	}
}

// loadFrecency refreshes the frecency snapshot when the store changed or
// it is older than epFrecTTL.
func (s *epSearcher) loadFrecency() {
	if s.fixed {
		return
	}
	now := time.Now()
	v := epFrecVersion()
	if v == s.frecV && !s.frecAt.IsZero() && now.Sub(s.frecAt) < epFrecTTL {
		return
	}
	s.frec, s.frecV = epFrecSnapshot(s.frec, now)
	s.frecAt = now
}

// epNormQuery lowercases and trims a query (":par" -> "par").
func epNormQuery(q string) string {
	q = strings.TrimSpace(q)
	q = strings.Trim(q, ":")
	if i := strings.Index(q, "::"); i >= 0 {
		q = q[:i]
	}
	return strings.ToLower(q)
}

// search returns matching ids (best first, one per emoji) and the
// search-table row that matched each. The slices are reused by the next
// call. limit <= 0 means all.
//
// Order: rank tier (exact, prefix, word boundary, substring); within a
// tier, emoji with a frecency score first (highest score first), then
// shorter names, then alphabetical. The (length, name) order comes free
// from the presorted search table; only the few frecency hits are sorted.
func (s *epSearcher) search(x *emojiIndex, q string, limit int) ([]int32, []int32) {
	s.ids, s.names = s.ids[:0], s.names[:0]
	q = epNormQuery(q)
	if q == "" || x == nil {
		return s.ids, s.names
	}
	if len(s.stamp) != len(x.names) {
		s.stamp = make([]uint32, len(x.names))
		s.best = make([]uint8, len(x.names))
		s.fstamp = make([]uint32, len(x.names))
		s.fscore = make([]float32, len(x.names))
		s.gen = 0
	}
	s.gen++
	if s.gen == 0 {
		for i := range s.stamp {
			s.stamp[i], s.fstamp[i] = 0, 0
		}
		s.gen = 1
	}
	g := s.gen

	s.loadFrecency()
	boosted := false
	for _, f := range s.frec {
		id, ok := x.byName[f.name]
		if !ok {
			continue
		}
		if s.fstamp[id] != g {
			s.fstamp[id], s.fscore[id] = g, 0
		}
		s.fscore[id] += float32(f.score) // an emoji used under several names
		boosted = true
	}

	for r := range s.buckets {
		s.buckets[r] = s.buckets[r][:0]
		s.boost[r] = s.boost[r][:0]
	}
	for i, name := range x.sLower {
		if len(name) < len(q) {
			continue
		}
		r := epRank(name, q)
		if r < 0 {
			continue
		}
		id := x.sID[i]
		if s.stamp[id] != g || uint8(r) < s.best[id] {
			s.stamp[id] = g
			s.best[id] = uint8(r)
		}
		if boosted && s.fstamp[id] == g {
			s.boost[r] = append(s.boost[r], int32(i))
		} else {
			s.buckets[r] = append(s.buckets[r], int32(i))
		}
	}
	for r := range s.buckets {
		if b := s.boost[r]; len(b) > 0 {
			// stable insertion sort by score: ties keep (length, name)
			for i := 1; i < len(b); i++ {
				row := b[i]
				sc := s.fscore[x.sID[row]]
				j := i
				for j > 0 && s.fscore[x.sID[b[j-1]]] < sc {
					b[j] = b[j-1]
					j--
				}
				b[j] = row
			}
			if s.emit(x, b, r, limit) {
				return s.ids, s.names
			}
		}
		if s.emit(x, s.buckets[r], r, limit) {
			return s.ids, s.names
		}
	}
	return s.ids, s.names
}

// emit appends the rows of tier r whose emoji ranked best in r (once per
// emoji) and reports whether limit was reached.
func (s *epSearcher) emit(x *emojiIndex, rows []int32, r, limit int) bool {
	for _, row := range rows {
		id := x.sID[row]
		if s.best[id] != uint8(r) {
			continue
		}
		s.best[id] = 0xff // emitted
		s.ids = append(s.ids, id)
		s.names = append(s.names, row)
		if limit > 0 && len(s.ids) >= limit {
			return true
		}
	}
	return false
}

// ---- index cache ----
//
// One full index is cached per process, keyed by the custom map's length
// and the emoji style, so reopening the picker is instant. Standard-only
// indexes (what the picker shows while the full one builds) are cached per
// style.

var (
	epCacheMu  sync.Mutex
	epFull     *emojiIndex
	epStdIdx   = map[string]*emojiIndex{}
	epBuilding = map[epKey]bool{}

	epLastUse  time.Time // guarded by epCacheMu
	epReleaser sync.Once
)

// epIndexIdle: the full index (65k custom emoji on Hack Club, ~10 MB) is
// dropped after this long unused (picker and ":" autocomplete). The next
// use shows standard emoji at once and rebuilds the full index (~170 ms)
// off the UI goroutine, as on first use.
const epIndexIdle = 5 * time.Minute

func epNoteUse() {
	epCacheMu.Lock()
	epLastUse = time.Now()
	epCacheMu.Unlock()
	epReleaser.Do(func() {
		go func() {
			for {
				time.Sleep(epIndexIdle / 5)
				epCacheMu.Lock()
				if epFull != nil && len(epBuilding) == 0 && time.Since(epLastUse) > epIndexIdle {
					epFull = nil
				}
				epCacheMu.Unlock()
			}
		}()
	})
}

func epStyle(h Host) string {
	if h != nil && h.Core() != nil && h.Core().Prefs().EmojiStyle == "google" {
		return "google"
	}
	return "apple"
}

func epStdIndex(style string) *emojiIndex {
	epCacheMu.Lock()
	x := epStdIdx[style]
	epCacheMu.Unlock()
	if x != nil {
		return x
	}
	x = epBuild(epStandard(), style, nil)
	epCacheMu.Lock()
	epStdIdx[style] = x
	epCacheMu.Unlock()
	return x
}

// epIndexFor returns the best available index for h and whether it is
// complete (includes the current custom emoji). When it isn't, a build is
// started off the UI goroutine; h is invalidated when it lands. Must be
// called on the UI goroutine.
func epIndexFor(h Host) (*emojiIndex, bool) {
	epNoteUse()
	epNoteCore(h)
	style := epStyle(h)
	var custom map[string]string
	if h != nil && h.Core() != nil {
		custom = h.Core().CustomEmoji()
	}
	if len(custom) == 0 {
		return epStdIndex(style), true
	}
	key := epKey{len(custom), style}
	epCacheMu.Lock()
	full := epFull
	building := epBuilding[key]
	if full == nil || full.key != key {
		epBuilding[key] = true
	}
	epCacheMu.Unlock()
	if full != nil && full.key == key {
		return full, true
	}
	if !building {
		std := epStandard()
		run(h, func(ctx context.Context) (*emojiIndex, error) {
			return epBuild(std, style, custom), nil
		}, func(x *emojiIndex, _ error) {
			epCacheMu.Lock()
			delete(epBuilding, key)
			if x != nil {
				epFull = x
			}
			epCacheMu.Unlock()
			if h != nil {
				h.Invalidate()
			}
		})
	}
	if full != nil && full.key.style == style {
		return full, false // stale but close: better than standard only
	}
	return epStdIndex(style), false
}

// ---- exported API for the composer ----

// EmojiMatch is one ":" autocomplete suggestion.
type EmojiMatch struct {
	Name, URL string
	Custom    bool
	// Toned is the name to insert: Name with the user's skin tone
	// ("+1::skin-tone-3") when the emoji has tones and one is set, like
	// Slack's emoji autocomplete; URL is that tone's image.
	Toned string
}

var (
	epSharedMu sync.Mutex
	epShared   epSearcher
)

// SearchEmoji returns up to limit emoji matching query with the picker's
// ranking (exact, prefix, word boundary, substring; frecency first within
// each tier, then length, then name).
// Name is the matched name (an alias when the alias matched), URL the
// image at the skin tone in use (the picker's, else Slack's pref).
func SearchEmoji(h Host, query string, limit int) []EmojiMatch {
	x, _ := epIndexFor(h)
	tone := epTone(h)
	epSharedMu.Lock()
	defer epSharedMu.Unlock()
	ids, rows := epShared.search(x, query, limit)
	if len(ids) == 0 {
		return nil
	}
	out := make([]EmojiMatch, len(ids))
	for i, id := range ids {
		name := x.sName[rows[i]]
		m := EmojiMatch{Name: name, URL: x.url(id, tone), Custom: x.custom(id), Toned: name}
		if tone >= 2 && x.hasSkins(id) {
			m.Toned = name + "::skin-tone-" + strconv.Itoa(tone)
		}
		out[i] = m
	}
	return out
}
