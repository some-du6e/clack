package emoji

import (
	"strconv"
	"strings"
	"sync"
)

// Unicode emoji to Slack names, for the composer: Slack's message input
// replaces native emoji typed or pasted into it with emoji blots named
// after the standard emoji (getEmojiNameFromUnicodeString), so a typed 👍
// is sent as the "+1" emoji element, like a picked one.

var (
	uniOnce   sync.Once
	uniIndex  map[string]uniEnt // code points without VS16 -> entry
	uniMaxLen int
)

type uniEnt struct {
	name string
	// vs: the table's form has VS16 after the first code point (a
	// text-default character such as ©, ™ or ❤): bare, it stays text.
	vs bool
}

var skinTones = map[string]string{"1F3FB": "2", "1F3FC": "3", "1F3FD": "4", "1F3FE": "5", "1F3FF": "6"}

func loadUnicode() {
	once.Do(load)
	uniIndex = make(map[string]uniEnt, len(byName)*2)
	add := func(hexSeq, name string) {
		var b strings.Builder
		n, vs := 0, false
		for i, p := range strings.Split(hexSeq, "-") {
			v, err := strconv.ParseUint(p, 16, 32)
			if err != nil {
				return
			}
			if v == 0xFE0F {
				if i == 1 {
					vs = true
				}
				continue
			}
			b.WriteRune(rune(v))
			n++
		}
		if n == 0 {
			return
		}
		k := b.String()
		if _, ok := uniIndex[k]; ok {
			return
		}
		uniIndex[k] = uniEnt{name: name, vs: vs}
		uniMaxLen = max(uniMaxLen, n)
	}
	for _, e := range allStd {
		if len(e.Names) == 0 {
			continue
		}
		name := e.Names[0]
		add(e.Uni, name)
		add(e.Image, name)
		for code, img := range e.Skins {
			if t, ok := skinTones[strings.ToUpper(code)]; ok {
				add(img, name+"::skin-tone-"+t)
			} else {
				add(img, name) // two-tone pairs: Slack names them by the base
			}
		}
	}
}

// MatchUnicode finds the standard emoji whose sequence starts at rs[i]
// (the longest one; VS16 optional except right after a text-default
// character). It returns Slack's name for it, with ::skin-tone-N for a
// toned variant, and the index just past the sequence.
func MatchUnicode(rs []rune, i int) (name string, end int, ok bool) {
	if i < 0 || i >= len(rs) || !maybeEmoji(rs[i]) {
		return "", 0, false
	}
	uniOnce.Do(loadUnicode)
	var kb [16]rune
	key := kb[:0]
	for j := i; j < len(rs) && len(key) < uniMaxLen; j++ {
		if rs[j] == 0xFE0F {
			continue
		}
		key = append(key, rs[j])
		e, hit := uniIndex[string(key)]
		if !hit {
			continue
		}
		if e.vs && (i+1 >= len(rs) || rs[i+1] != 0xFE0F) {
			continue
		}
		name, end, ok = e.name, j+1, true
	}
	if ok {
		for end < len(rs) && rs[end] == 0xFE0F {
			end++
		}
	}
	return name, end, ok
}

// maybeEmoji is a cheap pre-filter for the first rune of an emoji.
func maybeEmoji(r rune) bool {
	return r >= 0x203C || r == '#' || r == '*' || (r >= '0' && r <= '9') || r == 0xA9 || r == 0xAE
}

// MayHaveEmoji reports whether s has a rune MatchUnicode could start at,
// besides ASCII (a byte scan).
func MayHaveEmoji(s string) bool {
	for i := 0; i < len(s); i++ {
		if s[i] >= 0xe2 || s[i] == 0xc2 {
			return true
		}
	}
	return false
}
