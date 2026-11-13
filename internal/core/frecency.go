package core

import (
	"bytes"
	"context"
	"encoding/json"
	"log"
	"net/url"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/some-du6e/clack/internal/emoji"
	"github.com/some-du6e/clack/internal/frecency"
)

// Slack's frecency store and the emoji picker's Frequently Used list,
// ported in internal/frecency. Here they live on the App: the "frecency"
// pref is read from users.prefs.get, updated locally on every recorded
// use (recordFrecencyEvent) and written back with users.prefs.set at most
// once a minute (setFrecencyByApi, a 60 s leading+trailing throttle).
// emoji_use is only read; Slack's client never writes it.

const (
	prefFrecency = "frecency"
	prefEmojiUse = "emoji_use"
	// frecencySaveEvery is setFrecencyByApi's throttle (1TmA: 6e4).
	frecencySaveEvery = time.Minute
)

type frecencyState struct {
	mu sync.Mutex

	// the save throttle
	lastSave time.Time
	timer    *time.Timer
	pending  bool

	// getFrequentlyUsedEmoji's per-team cache (bEcp T)
	freq    []string
	freqExp time.Time

	now func() time.Time // tests
	ver uint64           // bumped whenever the frecency pref changes

	// loaded: the account's frecency has been read from Slack (a
	// users.prefs.get that had it, or the explicit emoji_use,frecency
	// fetch). Until then a use is applied locally and queued, not saved:
	// a save then would replace the account's synced frecency with a
	// fresh one. The queue is replayed over Slack's copy once it's in.
	loaded  bool
	loading bool
	queued  []frecQueued
}

// frecQueued is a use recorded before the account's frecency was read.
type frecQueued struct {
	id, text string
	at       time.Time
}

// FrecencyVersion counts changes to the frecency pref (for caches).
func (a *App) FrecencyVersion() uint64 {
	a.frec.mu.Lock()
	defer a.frec.mu.Unlock()
	return a.frec.ver
}

func (f *frecencyState) clock() time.Time {
	if f.now != nil {
		return f.now()
	}
	return time.Now()
}

// SetFrecencyClock replaces the frecency clock (tests).
func (a *App) SetFrecencyClock(now func() time.Time) {
	a.frec.mu.Lock()
	a.frec.now = now
	a.frec.mu.Unlock()
}

// FrecencyCache returns a copy of the viewer's frecency pref, parsed.
func (a *App) FrecencyCache() *frecency.Cache {
	v, _ := a.Pref(prefFrecency)
	return frecency.Parse(v)
}

// RecordFrecency is recordFrecencyEvent {id, text}: the pref changes
// locally at once and is saved through the throttle.
func (a *App) RecordFrecency(id, text string) {
	if id == "" {
		return
	}
	a.frec.mu.Lock()
	now := a.frec.clock()
	loaded := a.frec.loaded
	if !loaded {
		a.frec.queued = append(a.frec.queued, frecQueued{id: id, text: text, at: now})
	}
	a.frec.mu.Unlock()
	a.applyFrecency(id, text, now)
	if !loaded {
		go func() { _ = a.ensureFrecencyLoaded(a.bg) }()
		return
	}
	a.saveFrecencySoon()
}

// applyFrecency records one use in the local frecency pref.
func (a *App) applyFrecency(id, text string, now time.Time) {
	a.prefsMu.Lock() // a merge in flight would install a copy without this record (mergePrefs)
	a.S.Lock()
	c := frecency.Parse(a.S.RawPrefs[prefFrecency])
	c.Record(id, text, now)
	b, err := json.Marshal(c)
	if err == nil {
		s, _ := json.Marshal(string(b)) // users.prefs.get returns it as a string
		raw := make(map[string]json.RawMessage, len(a.S.RawPrefs)+1)
		for k, v := range a.S.RawPrefs {
			raw[k] = v
		}
		raw[prefFrecency] = s
		a.S.RawPrefs = raw
	}
	a.S.Unlock()
	a.prefsMu.Unlock()
	if err != nil {
		return
	}
	a.frec.mu.Lock()
	a.frec.ver++
	// the Frequently Used cache stays (bEcp T): Slack's list follows a use
	// when the save's search_frecency "updated" reloads the prefs
	a.frec.mu.Unlock()
}

// frecencyFromServer marks the account's frecency read (raw came from
// Slack and had it, or the explicit fetch answered) and replays the uses
// recorded before, over Slack's copy, then saves them.
func (a *App) frecencyFromServer() {
	a.frec.mu.Lock()
	a.frec.loaded = true
	q := a.frec.queued
	a.frec.queued = nil
	a.frec.mu.Unlock()
	for _, u := range q {
		a.applyFrecency(u.id, u.text, u.at)
	}
	if len(q) > 0 {
		a.saveFrecencySoon()
	}
}

// frecencyAfterPrefs follows a full users.prefs.get: with both frecency
// prefs in it the account's frecency is read; without, they're fetched on
// their own.
func (a *App) frecencyAfterPrefs(raw map[string]json.RawMessage) {
	_, hasF := raw[prefFrecency]
	_, hasU := raw[prefEmojiUse]
	if hasF && hasU {
		a.frecencyFromServer()
		return
	}
	go func() {
		if err := a.ensureFrecencyLoaded(a.bg); err != nil {
			log.Printf("frecency: users.prefs.get emoji_use,frecency: %v", err)
		}
	}()
}

// ensureFrecencyLoaded fetches emoji_use and frecency unless they're in
// (loadFrecencyPrefs' request, reason fetch-frecency-prefs). Slack's web
// client has them from its boot prefs; Clack's users.prefs.get at boot may
// not carry them, so boot asks for them when they're missing.
func (a *App) ensureFrecencyLoaded(ctx context.Context) error {
	a.frec.mu.Lock()
	if a.frec.loaded || a.frec.loading {
		a.frec.mu.Unlock()
		return nil
	}
	a.frec.loading = true
	a.frec.mu.Unlock()
	err := a.LoadFrecencyPrefs(ctx)
	a.frec.mu.Lock()
	a.frec.loading = false
	a.frec.mu.Unlock()
	return err
}

// EmojiFrecencyID is recordEmojiFrecencyEvent's id for an emoji name
// (skin tone dropped): "E"+unicode for a standard emoji, else "E"+name.
func EmojiFrecencyID(name string) string {
	name = frecency.StripSkinTone(strings.Trim(name, ":"))
	if u := emoji.StandardUnicode(name); u != "" {
		return frecency.EmojiID(u, name)
	}
	return frecency.EmojiID("", name)
}

// RecordEmojiFrecency is recordEmojiFrecencyEvent {emojiName, text}: the
// picker records its search text, a reaction "" and the composer's ":"
// autocomplete the text it completed.
func (a *App) RecordEmojiFrecency(name, text string) {
	a.RecordFrecency(EmojiFrecencyID(name), text)
}

// saveFrecencySoon is the 60 s leading+trailing throttle around
// setFrecencyByApi: the first change is sent at once, later ones in the
// window once at its end with the latest pref.
func (a *App) saveFrecencySoon() {
	f := &a.frec
	f.mu.Lock()
	defer f.mu.Unlock()
	now := f.clock()
	if f.timer == nil && (f.lastSave.IsZero() || now.Sub(f.lastSave) >= frecencySaveEvery) {
		f.lastSave = now
		go a.saveFrecency()
		return
	}
	f.pending = true
	if f.timer == nil {
		wait := frecencySaveEvery - now.Sub(f.lastSave)
		f.timer = time.AfterFunc(max(wait, 0), a.frecencyTrailing)
	}
}

func (a *App) frecencyTrailing() {
	f := &a.frec
	f.mu.Lock()
	f.timer = nil
	send := f.pending
	f.pending = false
	if send {
		f.lastSave = f.clock()
	}
	f.mu.Unlock()
	if send {
		a.saveFrecency()
	}
}

// FlushFrecency sends a pending frecency save now (tests, sign-out).
func (a *App) FlushFrecency() {
	f := &a.frec
	f.mu.Lock()
	if f.timer != nil {
		f.timer.Stop()
		f.timer = nil
	}
	send := f.pending
	f.pending = false
	f.mu.Unlock()
	if send {
		a.saveFrecency()
	}
}

// saveFrecency is setUserPrefByApi {pref: "frecency", value}: the object
// JSON-encoded as the form value.
func (a *App) saveFrecency() {
	v, ok := a.Pref(prefFrecency)
	if !ok {
		return
	}
	b, err := json.Marshal(frecency.Parse(v))
	if err != nil {
		return
	}
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	if err := a.C.SetPref(ctx, prefFrecency, string(b)); err != nil {
		log.Printf("frecency: users.prefs.set: %v", err)
	}
}

// LoadFrecencyPrefs is loadFrecencyPrefs ("search_frecency" updated):
// users.prefs.get for emoji_use and frecency, upserted, and the
// Frequently Used cache dropped.
func (a *App) LoadFrecencyPrefs(ctx context.Context) error {
	var r struct {
		Prefs map[string]json.RawMessage `json:"prefs"`
	}
	err := a.C.Call(ctx, "users.prefs.get", url.Values{"prefs": {prefEmojiUse + "," + prefFrecency}, "reason": {"fetch-frecency-prefs"}}, &r)
	if err != nil {
		return err
	}
	set := map[string]json.RawMessage{}
	for _, k := range []string{prefEmojiUse, prefFrecency} {
		if v, ok := r.Prefs[k]; ok {
			set[k] = v
		}
	}
	a.mergePrefs(set)
	a.invalidateFrequentEmoji()
	a.frecencyFromServer()
	return nil
}

// frecencyPrefsChanged drops the Frequently Used cache when Slack sent a
// different frecency or emoji_use than the one in use.
// A save still waiting in the throttle keeps the local frecency (the
// server's copy is older).
func (a *App) frecencyPrefsChanged(old, nw map[string]json.RawMessage) {
	a.frec.mu.Lock()
	pending := a.frec.pending
	a.frec.mu.Unlock()
	if v, ok := old[prefFrecency]; pending && ok && nw != nil {
		nw[prefFrecency] = v
	}
	if !bytes.Equal(old[prefFrecency], nw[prefFrecency]) {
		a.frec.mu.Lock()
		a.frec.ver++
		a.frec.mu.Unlock()
	}
	for _, k := range []string{prefEmojiUse, prefFrecency} {
		if !bytes.Equal(old[k], nw[k]) {
			a.invalidateFrequentEmoji()
			return
		}
	}
}

func (a *App) invalidateFrequentEmoji() {
	a.frec.mu.Lock()
	a.frec.freq, a.frec.freqExp = nil, time.Time{}
	a.frec.mu.Unlock()
}

// FrequentlyUsedEmoji is getFrequentlyUsedEmoji (max 36, min 5): the
// picker's Frequently Used names. A computed list is reused for 4 hours
// (pushed to 15 min out when read near expiry), as in Slack, until
// Slack's frecency prefs are reloaded.
func (a *App) FrequentlyUsedEmoji() []string {
	f := &a.frec
	f.mu.Lock()
	now := f.clock()
	if f.freq != nil && !now.After(f.freqExp) {
		if now.Add(frecency.FrequentExtend).After(f.freqExp) {
			f.freqExp = now.Add(frecency.FrequentExtend)
		}
		out := append([]string(nil), f.freq...)
		f.mu.Unlock()
		return out
	}
	f.mu.Unlock()

	a.S.RLock()
	fv, uv := a.S.RawPrefs[prefFrecency], a.S.RawPrefs[prefEmojiUse]
	a.S.RUnlock()
	f.mu.Lock()
	loaded := f.loaded
	f.mu.Unlock()
	out := frecency.FrequentlyUsed(frecency.Parse(fv), frecency.EmojiUse(uv), a.emojiSet(),
		frecency.FrequentOptions{FrecencyExperiment: a.experiment("autocomplete_frequently_used_emojis_frecency") == "on"}, now)
	if loaded { // before the account's frecency is in, don't keep the defaults for 4 h
		f.mu.Lock()
		f.freq, f.freqExp = append([]string(nil), out...), now.Add(frecency.FrequentTTL)
		f.mu.Unlock()
	}
	return out
}

// emojiSet is the consolidated emoji store for frecency: the standard
// emoji (primary names) then the workspace's custom emoji by name;
// aliases fold into their targets.
func (a *App) emojiSet() frecency.EmojiSet {
	return &coreEmojiSet{custom: a.S.CustomEmoji.Snapshot()}
}

type coreEmojiSet struct {
	custom map[string]string
}

var (
	stdFrecOnce sync.Once
	stdFrec     []frecency.Emoji
)

func (s *coreEmojiSet) All() []frecency.Emoji {
	stdFrecOnce.Do(func() {
		for _, e := range emoji.StandardList() {
			if len(e.Names) > 0 {
				stdFrec = append(stdFrec, frecency.Emoji{Name: e.Names[0], ID: frecency.EmojiID(e.Unicode, e.Names[0])})
			}
		}
	})
	names := make([]string, 0, len(s.custom))
	for n, v := range s.custom {
		if !strings.HasPrefix(v, "alias:") {
			names = append(names, n)
		}
	}
	sort.Strings(names)
	out := make([]frecency.Emoji, 0, len(stdFrec)+len(names))
	out = append(out, stdFrec...)
	for _, n := range names {
		out = append(out, frecency.Emoji{Name: n, ID: frecency.EmojiID("", n)})
	}
	return out
}

// Canonical is getCanonicalEmojiName: a standard name as given, a custom
// emoji's own name or its alias target.
func (s *coreEmojiSet) Canonical(name string) (string, bool) {
	if name == "" {
		return "", false
	}
	if emoji.IsStandard(name) {
		return name, true
	}
	v, ok := s.custom[name]
	if !ok {
		return "", false
	}
	if t, ok := strings.CutPrefix(v, "alias:"); ok && t != "" {
		return t, true
	}
	return name, true
}

var (
	stdByUniOnce sync.Once
	stdByUni     map[string]string // unicode -> primary name
)

// EmojiFrecencyName maps an emoji frecency id back to its name: a
// standard emoji's primary name, else the custom name ("" for ids that
// are not emoji).
func EmojiFrecencyName(id string) string {
	rest, ok := strings.CutPrefix(frecency.StripSkinToneID(id), "E")
	if !ok || rest == "" {
		return ""
	}
	stdByUniOnce.Do(func() {
		stdByUni = map[string]string{}
		for _, e := range emoji.StandardList() {
			if len(e.Names) > 0 {
				stdByUni[e.Unicode] = e.Names[0]
			}
		}
	})
	if n, ok := stdByUni[rest]; ok {
		return n
	}
	return rest
}

// EmojiFrecencyScores is every emoji's frecency score (calculateScore of
// its own record) by name, for ranking emoji search results.
func (a *App) EmojiFrecencyScores() map[string]float64 {
	c := a.FrecencyCache()
	a.frec.mu.Lock()
	now := a.frec.clock()
	a.frec.mu.Unlock()
	out := map[string]float64{}
	for _, k := range c.Keys() {
		if !strings.HasPrefix(k, "E") {
			continue
		}
		r := c.Item(k)
		if r == nil || r.ID != k {
			continue
		}
		if s := frecency.Score(r, now); s > 0 {
			if n := EmojiFrecencyName(k); n != "" {
				out[n] += s
			}
		}
	}
	return out
}
