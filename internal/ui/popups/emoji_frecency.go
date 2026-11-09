package popups

import (
	"context"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/some-du6e/yoloslackgoclient/internal/core"
	"github.com/some-du6e/yoloslackgoclient/internal/frecency"
)

// Emoji frecency comes from Slack's own "frecency" and "emoji_use" user
// prefs (core/frecency.go, a port of the web client's store): the picker
// shows core.FrequentlyUsedEmoji and records picks with
// core.RecordEmojiFrecency, which saves the pref with users.prefs.set.
// Nothing is kept on disk here.

// epCoreApp is the App the last picker or ":" search ran for, so the
// searcher can read the frecency pref without a Host.
var epCoreApp atomic.Pointer[core.App]

func epNoteCore(h Host) {
	if h != nil && h.Core() != nil {
		epCoreApp.Store(h.Core())
	}
}

// epScored is one emoji name with its frecency score.
type epScored struct {
	name  string
	score float64
}

// epFrecSnapshot appends every emoji with a positive frecency score to
// dst[:0]. The version is the pref's change count (for caching).
func epFrecSnapshot(dst []epScored, _ time.Time) ([]epScored, uint64) {
	dst = dst[:0]
	a := epCoreApp.Load()
	if a == nil {
		return dst, 0
	}
	for n, s := range a.EmojiFrecencyScores() {
		dst = append(dst, epScored{n, s})
	}
	return dst, a.FrecencyVersion()
}

// epFrecVersion is the current pref's change count.
func epFrecVersion() uint64 {
	if a := epCoreApp.Load(); a != nil {
		return a.FrecencyVersion()
	}
	return 0
}

// epFrequentNames is the picker's Frequently Used list
// (getFrequentlyUsedEmoji), or Slack's defaults without a core.
func epFrequentNames(h Host) []string {
	if h != nil && h.Core() != nil {
		return h.Core().FrequentlyUsedEmoji()
	}
	return frecency.DefaultFrequent[:frecency.FrequentMin]
}

// epRecordUse is the picker's recordEmojiFrecencyEvent {emojiName, text:
// the search query}.
func epRecordUse(h Host, name, query string) {
	if h != nil && h.Core() != nil {
		h.Core().RecordEmojiFrecency(name, query)
	}
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

// epToneSet is the tone the picker just chose while its users.prefs.set
// is in flight (setUserPrefByApi updates the pref locally first).
var (
	epToneMu  sync.Mutex
	epToneSet int
)

// epTone is the skin tone in use: Slack's preferred_skin_tone.
func epTone(h Host) int {
	epToneMu.Lock()
	t := epToneSet
	epToneMu.Unlock()
	if t >= 1 && t <= 6 {
		return t
	}
	if h != nil && h.Core() != nil {
		return epParseTone(h.Core().Prefs().SkinTone)
	}
	return 1
}

// epSetTone is the picker's onSkinToneChanged: setUserPrefByApi
// {pref: "preferred_skin_tone", value: "1".."6"}.
func epSetTone(h Host, t int) {
	if t < 1 || t > 6 {
		return
	}
	epToneMu.Lock()
	epToneSet = t
	epToneMu.Unlock()
	if h == nil || h.Core() == nil {
		return
	}
	a := h.Core()
	run(h, func(ctx context.Context) (struct{}, error) {
		return struct{}{}, a.SetPref(ctx, "preferred_skin_tone", strconv.Itoa(t))
	}, func(struct{}, error) {
		epToneMu.Lock()
		if epToneSet == t {
			epToneSet = 0 // the pref has it now (or kept the old one)
		}
		epToneMu.Unlock()
	})
}
