package richview

import (
	"encoding/json"
	"strconv"
	"strings"
	"sync"

	"github.com/some-du6e/clack/internal/emoji"
)

// Unicode emoji inside plain text (mrkdwn text, button labels, attachment
// text) are drawn as Slack's CDN emoji images, like the web client and the
// theme's label renderer; Lato has no emoji glyphs and the system color
// font isn't Slack's set.

type emojiSeg struct {
	text string // plain text run, or the emoji's original characters
	stem string // non-empty for an emoji: CDN image stem
}

var (
	emojiOnce   sync.Once
	emojiIndex  map[string]string // rune sequence without VS16 -> image stem
	emojiMaxLen int
	splitMu     sync.Mutex
	splitCache  = map[string][]emojiSeg{}
)

func loadEmojiIndex() {
	var f struct {
		Emoji []struct {
			I string            `json:"i"`
			U string            `json:"u"`
			S map[string]string `json:"s"`
		} `json:"emoji"`
	}
	_ = json.Unmarshal(emoji.StandardData(), &f)
	emojiIndex = make(map[string]string, len(f.Emoji)*3)
	add := func(hexSeq, stem string) {
		var b strings.Builder
		n := 0
		for _, p := range strings.Split(hexSeq, "-") {
			v, err := strconv.ParseUint(p, 16, 32)
			if err != nil {
				return
			}
			if v == 0xFE0F {
				continue
			}
			b.WriteRune(rune(v))
			n++
		}
		if n == 0 {
			return
		}
		if _, ok := emojiIndex[b.String()]; !ok {
			emojiIndex[b.String()] = stem
		}
		if n > emojiMaxLen {
			emojiMaxLen = n
		}
	}
	for _, e := range f.Emoji {
		add(e.I, e.I)
		if e.U != "" {
			add(e.U, e.I)
		}
		for _, img := range e.S {
			add(img, img)
		}
	}
}

func mayStartEmoji(r rune) bool {
	return r >= 0x203C || r == '#' || r == '*' || (r >= '0' && r <= '9') || r == 0xA9 || r == 0xAE
}

// splitEmoji returns text/emoji segments, or nil when s has no emoji.
func splitEmoji(s string) []emojiSeg {
	has := false
	for i := 0; i < len(s); i++ {
		if s[i] >= 0xe2 || s[i] == 0xc2 {
			has = true
			break
		}
	}
	if !has {
		return nil
	}
	splitMu.Lock()
	if segs, ok := splitCache[s]; ok {
		splitMu.Unlock()
		return segs
	}
	splitMu.Unlock()
	emojiOnce.Do(loadEmojiIndex)
	rs := []rune(s)
	var segs []emojiSeg
	var txt strings.Builder
	found := false
	for i := 0; i < len(rs); {
		r := rs[i]
		if mayStartEmoji(r) && (r >= 0x203C || (i+1 < len(rs) && (rs[i+1] == 0xFE0F || rs[i+1] == 0x20E3))) {
			var key []rune
			best, bestEnd := "", 0
			for j := i; j < len(rs) && len(key) < emojiMaxLen; j++ {
				if rs[j] == 0xFE0F {
					continue
				}
				key = append(key, rs[j])
				if stem, ok := emojiIndex[string(key)]; ok {
					best, bestEnd = stem, j+1
				}
			}
			if best != "" {
				for bestEnd < len(rs) && rs[bestEnd] == 0xFE0F {
					bestEnd++
				}
				if txt.Len() > 0 {
					segs = append(segs, emojiSeg{text: txt.String()})
					txt.Reset()
				}
				segs = append(segs, emojiSeg{text: string(rs[i:bestEnd]), stem: best})
				found = true
				i = bestEnd
				continue
			}
		}
		txt.WriteRune(r)
		i++
	}
	if !found {
		segs = nil
	} else if txt.Len() > 0 {
		segs = append(segs, emojiSeg{text: txt.String()})
	}
	splitMu.Lock()
	if len(splitCache) > 4096 {
		splitCache = map[string][]emojiSeg{}
	}
	splitCache[s] = segs
	splitMu.Unlock()
	return segs
}

// emojiURL is the CDN image for an emoji stem in the Style's emoji style.
func (s *Style) emojiURL(stem string) string {
	if s.EmojiURL != nil {
		return s.EmojiURL(stem)
	}
	return emoji.CDN + "apple-medium/" + stem + ".png"
}
