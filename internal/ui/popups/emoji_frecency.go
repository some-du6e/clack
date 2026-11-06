package popups

import (
	"encoding/json"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"
)

// Emoji frecency: how often and how recently each emoji was picked, kept
// in <UserConfigDir>/yoloslack/emoji_frecency.json. Only emoji names, use
// counts and timestamps are stored (plus the chosen skin tone).
//
// Scoring follows Slack/Firefox frecency: each of the last 10 uses earns
// points by age (4 h: 100, 1 day: 80, 3 days: 60, 1 week: 40, 1 month: 20,
// 3 months: 10, older: 0); the score is count × the mean of those points.

const (
	epFrequentMax = 18
	epUseHistory  = 10
)

var epDefaultFrequent = []string{
	"slightly_smiling_face", "heart", "+1", "100", "white_check_mark", "eyes",
	"joy", "tada", "pray", "fire", "raised_hands", "rocket",
}

type epUse struct {
	N int     `json:"n"`
	T []int64 `json:"t"` // unix seconds, oldest first
}

type epFrecencyFile struct {
	Version  int               `json:"version"`
	SkinTone int               `json:"skin_tone,omitempty"`
	Emoji    map[string]*epUse `json:"emoji"`
}

var (
	epFrecMu     sync.Mutex
	epFrecLoaded bool
	epFrec       epFrecencyFile
	epFrecDirty  bool
	epFrecSaving bool
	epFrecVer    uint64 // bumped by every recorded use
)

// epFrecencyPath is where the file lives ("" disables persistence).
var epFrecencyPath = func() string {
	dir, err := os.UserConfigDir()
	if err != nil || dir == "" {
		return ""
	}
	return filepath.Join(dir, "yoloslack", "emoji_frecency.json")
}

// epFrecLoadLocked reads the file once (small; called with epFrecMu held).
func epFrecLoadLocked() {
	if epFrecLoaded {
		return
	}
	epFrecLoaded = true
	if p := epFrecencyPath(); p != "" {
		if b, err := os.ReadFile(p); err == nil {
			var f epFrecencyFile
			if json.Unmarshal(b, &f) == nil {
				epFrec = f
			}
		}
	}
	if epFrec.Emoji == nil {
		epFrec.Emoji = map[string]*epUse{}
	}
	epFrec.Version = 1
}

// epBaseName strips colons and a skin tone suffix.
func epBaseName(name string) string {
	name = strings.Trim(name, ":")
	if i := strings.Index(name, "::"); i >= 0 {
		name = name[:i]
	}
	return name
}

// RecordEmojiUse notes that name was used (picked in the picker, sent as a
// reaction, completed in the composer). Persists off the UI goroutine.
func RecordEmojiUse(name string) {
	name = epBaseName(name)
	if name == "" {
		return
	}
	epFrecMu.Lock()
	epFrecLoadLocked()
	u := epFrec.Emoji[name]
	if u == nil {
		u = &epUse{}
		epFrec.Emoji[name] = u
	}
	u.N++
	u.T = append(u.T, time.Now().Unix())
	if len(u.T) > epUseHistory {
		u.T = append(u.T[:0], u.T[len(u.T)-epUseHistory:]...)
	}
	epFrecVer++
	epFrecMarkDirtyLocked()
	epFrecMu.Unlock()
}

// epFrecMarkDirtyLocked schedules a save (one writer goroutine at a time).
func epFrecMarkDirtyLocked() {
	epFrecDirty = true
	if epFrecSaving {
		return
	}
	epFrecSaving = true
	go epFrecSave()
}

func epFrecSave() {
	for {
		epFrecMu.Lock()
		if !epFrecDirty {
			epFrecSaving = false
			epFrecMu.Unlock()
			return
		}
		epFrecDirty = false
		b, err := json.Marshal(&epFrec)
		epFrecMu.Unlock()
		if err != nil {
			continue
		}
		p := epFrecencyPath()
		if p == "" {
			continue
		}
		_ = epWriteFileAtomic(p, b)
	}
}

// epWriteFileAtomic writes via a temp file + rename.
func epWriteFileAtomic(p string, b []byte) error {
	dir := filepath.Dir(p)
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return err
	}
	f, err := os.CreateTemp(dir, ".emoji_frecency-*.tmp")
	if err != nil {
		return err
	}
	tmp := f.Name()
	if _, err := f.Write(b); err != nil {
		f.Close()
		os.Remove(tmp)
		return err
	}
	if err := f.Close(); err != nil {
		os.Remove(tmp)
		return err
	}
	if err := os.Rename(tmp, p); err != nil {
		os.Remove(tmp)
		return err
	}
	return nil
}

// epScore is the frecency score of u at now.
func epScore(u *epUse, now time.Time) float64 {
	if u == nil || u.N <= 0 || len(u.T) == 0 {
		return 0
	}
	sum := 0.0
	for _, t := range u.T {
		age := now.Sub(time.Unix(t, 0))
		switch {
		case age < 4*time.Hour:
			sum += 100
		case age < 24*time.Hour:
			sum += 80
		case age < 3*24*time.Hour:
			sum += 60
		case age < 7*24*time.Hour:
			sum += 40
		case age < 30*24*time.Hour:
			sum += 20
		case age < 90*24*time.Hour:
			sum += 10
		}
	}
	return float64(u.N) * sum / float64(len(u.T))
}

// epTopFrecency returns up to limit names by score (ties: more uses, then
// name).
func epTopFrecency(m map[string]*epUse, now time.Time, limit int) []string {
	type sc struct {
		name string
		s    float64
		n    int
	}
	all := make([]sc, 0, len(m))
	for name, u := range m {
		if s := epScore(u, now); s > 0 {
			all = append(all, sc{name, s, u.N})
		}
	}
	sort.Slice(all, func(i, j int) bool {
		if all[i].s != all[j].s {
			return all[i].s > all[j].s
		}
		if all[i].n != all[j].n {
			return all[i].n > all[j].n
		}
		return all[i].name < all[j].name
	})
	if len(all) > limit {
		all = all[:limit]
	}
	out := make([]string, len(all))
	for i, e := range all {
		out[i] = e.name
	}
	return out
}

// epScored is one emoji name with its frecency score.
type epScored struct {
	name  string
	score float64
}

// epFrecSnapshot appends every recorded emoji with a positive score at now
// to dst[:0] and returns it with the store's version (for caching). It
// costs O(recorded emoji), never O(all emoji).
func epFrecSnapshot(dst []epScored, now time.Time) ([]epScored, uint64) {
	dst = dst[:0]
	epFrecMu.Lock()
	defer epFrecMu.Unlock()
	epFrecLoadLocked()
	for name, u := range epFrec.Emoji {
		if s := epScore(u, now); s > 0 {
			dst = append(dst, epScored{name, s})
		}
	}
	return dst, epFrecVer
}

// FrequentEmoji returns the top 18 emoji by frecency, or Slack's default
// set when nothing has been recorded yet.
func FrequentEmoji() []string {
	if names := epFrecencyNames(); len(names) > 0 {
		return names
	}
	return append([]string(nil), epDefaultFrequent...)
}

func epFrecencyNames() []string {
	epFrecMu.Lock()
	defer epFrecMu.Unlock()
	epFrecLoadLocked()
	return epTopFrecency(epFrec.Emoji, time.Now(), epFrequentMax)
}

// epFrequentNames is the picker's Frequently Used list: the frecency file,
// else Slack's own emoji_use pref, else the default set.
func epFrequentNames(h Host) []string {
	if names := epFrecencyNames(); len(names) > 0 {
		return names
	}
	if h != nil && h.Core() != nil {
		if p := h.Core().Prefs().FrequentEmoji; len(p) > 0 {
			if len(p) > epFrequentMax {
				p = p[:epFrequentMax]
			}
			return p
		}
	}
	return epDefaultFrequent
}

// epParseTone turns Slack's preferred_skin_tone ("", "1".."6",
// "skin-tone-3") into 1..6.
func epParseTone(s string) int {
	s = strings.TrimPrefix(strings.Trim(s, ":"), "skin-tone-")
	if n, err := strconv.Atoi(s); err == nil && n >= 1 && n <= 6 {
		return n
	}
	return 1
}

// epTone is the skin tone in use: the one chosen in the picker, else the
// Slack pref.
func epTone(h Host) int {
	epFrecMu.Lock()
	epFrecLoadLocked()
	t := epFrec.SkinTone
	epFrecMu.Unlock()
	if t >= 1 && t <= 6 {
		return t
	}
	if h != nil && h.Core() != nil {
		return epParseTone(h.Core().Prefs().SkinTone)
	}
	return 1
}

// epSetTone remembers the picker's skin tone.
func epSetTone(t int) {
	if t < 1 || t > 6 {
		return
	}
	epFrecMu.Lock()
	epFrecLoadLocked()
	if epFrec.SkinTone != t {
		epFrec.SkinTone = t
		epFrecMarkDirtyLocked()
	}
	epFrecMu.Unlock()
}
