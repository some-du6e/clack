package slackc

import (
	"encoding/json"
	"strconv"
	"strings"
	"sync"
	"unicode/utf8"

	"gioui.org/font"
	"gioui.org/op"
	"gioui.org/op/clip"
	"gioui.org/text"
	"golang.org/x/image/math/fixed"

	"github.com/some-du6e/clack/internal/emoji"
	"github.com/some-du6e/clack/internal/ui/slackc/css"
	"github.com/some-du6e/clack/internal/ui/theme"
)

// Words are shaped once per (shaper, font, size, letter-spacing) and
// reused by every tree and width: the steady-state frame never touches
// the shaper. Paths are built lazily and kept (text.Shaper builds each
// path in its own op.Ops, so they stay valid).

type faceKey struct {
	sh   *text.Shaper
	f    font.Font
	size fixed.Int26_6
	ls   fixed.Int26_6 // letter spacing
}

type wordKey struct {
	faceKey
	s string
}

type shaped struct {
	glyphs    []text.Glyph
	adv       float32
	asc, desc float32
	str       string
	pathDone  bool
	path      clip.PathSpec
	bitmaps   op.CallOp
}

type spaceMetrics struct{ adv, asc, desc float32 }

var shapeCache = struct {
	words map[wordKey]*shaped
	space map[faceKey]spaceMetrics
}{words: map[wordKey]*shaped{}, space: map[faceKey]spaceMetrics{}}

const maxShapedWords = 40000

var (
	fallbackShaperOnce sync.Once
	fallbackShaper     *text.Shaper
)

// shaper is the theme's shaper (or a Lato shaper when no theme is set,
// e.g. in tests).
func shaper() *text.Shaper {
	if th := env().Theme; th != nil && th.Shaper != nil {
		return th.Shaper
	}
	fallbackShaperOnce.Do(func() { fallbackShaper = theme.NewShaperFor(false) })
	return fallbackShaper
}

func f26(v fixed.Int26_6) float32 { return float32(v) / 64 }

// fontFor maps a computed style to a Gio font.
func fontFor(st *css.Style) font.Font {
	w := font.Weight(st.FontWeight - 400)
	var f font.Font
	if st.Italic {
		f = theme.ItalicFont(w)
	} else {
		f = theme.Font(w)
	}
	if fam := st.FontFamily; fam != "" && isMonoFamily(fam) {
		f.Typeface = theme.MonoTypeface + ", monospace" // not the system Monaco (theme.MonoTypeface)
	}
	return f
}

func isMonoFamily(f string) bool {
	if f == "Lato" {
		return false
	}
	return containsFold(f, "mono") || containsFold(f, "monaco") || containsFold(f, "menlo") || containsFold(f, "consolas") || containsFold(f, "courier")
}

// containsFold is an allocation-free ASCII case-insensitive Contains.
func containsFold(s, sub string) bool {
	for i := 0; i+len(sub) <= len(s); i++ {
		j := 0
		for j < len(sub) {
			c := s[i+j]
			if c >= 'A' && c <= 'Z' {
				c += 'a' - 'A'
			}
			if c != sub[j] {
				break
			}
			j++
		}
		if j == len(sub) {
			return true
		}
	}
	return false
}

func (v *View) face(st *css.Style) faceKey {
	px := st.FontSize * v.sp
	if px < 1 {
		px = 1
	}
	return faceKey{sh: shaper(), f: fontFor(st), size: fixed.Int26_6(px*64 + 0.5), ls: fixed.Int26_6(st.LetterSpacing * v.dp * 64)}
}

func word(k faceKey, s string) *shaped {
	wk := wordKey{k, s}
	if w, ok := shapeCache.words[wk]; ok {
		return w
	}
	if len(shapeCache.words) > maxShapedWords {
		shapeCache.words = map[wordKey]*shaped{}
	}
	w := shapeNow(k, s)
	shapeCache.words[wk] = w
	return w
}

func shapeNow(k faceKey, s string) *shaped {
	sh := k.sh
	sh.LayoutString(text.Parameters{
		Font:             k.f,
		PxPerEm:          k.size,
		MaxWidth:         1 << 24,
		WrapPolicy:       text.WrapGraphemes,
		DisableSpaceTrim: true,
	}, s)
	w := &shaped{str: s}
	first := true
	var adv fixed.Int26_6
	for g, ok := sh.NextGlyph(); ok; g, ok = sh.NextGlyph() {
		theme.ScanGlyph(sh, g)
		if first {
			first = false
			w.asc, w.desc = f26(g.Ascent), f26(g.Descent)
		}
		if g.Flags&text.FlagParagraphBreak != 0 && g.Runes == 0 {
			continue
		}
		g.X = adv
		g.Y = 0
		adv += g.Advance + k.ls
		w.glyphs = append(w.glyphs, g)
	}
	w.adv = f26(adv)
	if first {
		m := spaceOf(k)
		w.asc, w.desc = m.asc, m.desc
	}
	return w
}

// spaceOf returns the metrics of a space (and the font's ascent/descent).
func spaceOf(k faceKey) spaceMetrics {
	if m, ok := shapeCache.space[k]; ok {
		return m
	}
	k.sh.LayoutString(text.Parameters{Font: k.f, PxPerEm: k.size, MaxWidth: 1 << 24, DisableSpaceTrim: true}, " ")
	var m spaceMetrics
	var adv fixed.Int26_6
	got := false
	for g, ok := k.sh.NextGlyph(); ok; g, ok = k.sh.NextGlyph() {
		if !got {
			got = true
			m.asc, m.desc = f26(g.Ascent), f26(g.Descent)
		}
		adv += g.Advance
	}
	m.adv = f26(adv + k.ls)
	if m.adv <= 0 {
		m.adv = f26(k.size) / 4
	}
	if !got || m.asc+m.desc <= 0 {
		m.asc, m.desc = f26(k.size)*0.987, f26(k.size)*0.213
	}
	shapeCache.space[k] = m
	return m
}

// pathOf returns the cached outline and bitmap ops of w.
func (w *shaped) pathOf(sh *text.Shaper) (clip.PathSpec, op.CallOp) {
	if !w.pathDone {
		w.pathDone = true
		if len(w.glyphs) > 0 {
			w.path = sh.Shape(w.glyphs)
			if needsBitmaps(w.str) {
				w.bitmaps = sh.Bitmaps(w.glyphs)
			}
		}
	}
	return w.path, w.bitmaps
}

func needsBitmaps(s string) bool {
	for i := 0; i < len(s); i++ {
		if s[i] >= 0xE2 {
			return true
		}
	}
	return false
}

// splitAt cuts w into the longest prefix (at least one cluster unless
// allowEmpty) that fits maxW px, and the rest.
func splitAt(k faceKey, w *shaped, maxW float32, allowEmpty bool) (head, tail *shaped) {
	cut := -1
	for i, g := range w.glyphs {
		if g.Runes == 0 {
			continue
		}
		if f26(g.X+g.Advance) > maxW && (cut >= 0 || allowEmpty) {
			break
		}
		cut = i
	}
	if cut < 0 {
		return nil, w
	}
	if cut >= len(w.glyphs)-1 {
		return w, nil
	}
	nr := 0
	for i := 0; i <= cut; i++ {
		nr += int(w.glyphs[i].Runes)
	}
	b := 0
	for i := 0; i < nr && b < len(w.str); i++ {
		_, sz := utf8.DecodeRuneInString(w.str[b:])
		b += sz
	}
	if b <= 0 {
		return nil, w
	}
	if b >= len(w.str) {
		return w, nil
	}
	return word(k, w.str[:b]), word(k, w.str[b:])
}

// --- unicode emoji → Slack CDN images (like theme.Text) ---

type emojiSeg struct {
	text string
	stem string
}

var (
	emojiOnce   sync.Once
	emojiIndex  map[string]string
	emojiMaxLen int
	emojiSplits = map[string][]emojiSeg{}
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

// splitEmoji returns text/emoji segments of a word, or nil when it has none.
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
	if segs, ok := emojiSplits[s]; ok {
		return segs
	}
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
	if len(emojiSplits) > 4096 {
		emojiSplits = map[string][]emojiSeg{}
	}
	emojiSplits[s] = segs
	return segs
}

func emojiURL(stem string) string {
	if th := env().Theme; th != nil {
		return th.EmojiURL(stem)
	}
	return emoji.CDN + "apple-medium/" + stem + ".png"
}
