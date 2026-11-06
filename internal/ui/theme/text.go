package theme

import (
	"encoding/json"
	"image"
	"image/color"
	"strconv"
	"strings"
	"sync"
	"unicode/utf8"

	"gioui.org/f32"
	"gioui.org/font"
	"gioui.org/layout"
	"gioui.org/op"
	"gioui.org/op/clip"
	"gioui.org/op/paint"
	"gioui.org/text"
	"gioui.org/unit"
	"golang.org/x/image/math/fixed"

	"github.com/some-du6e/yoloslackgoclient/internal/emoji"
)

// Embolden is the outline stroke (dp) added to every glyph. Chrome renders
// Lato visibly heavier than Gio's linear-coverage rasterizer; a hairline
// stroke of the outline matches it. richview.Style.Embolden uses the same
// value so message bodies and chrome text agree.
const Embolden = 0.3

// LabelOpts describes a run of label text for Text / DrawText.
type LabelOpts struct {
	Font       font.Font
	Size       unit.Sp
	LineHeight unit.Sp
	MaxLines   int // 0 = unlimited; >0 ellipsizes with "…"
	Align      text.Alignment
	Wrap       text.WrapPolicy
	Color      color.NRGBA
}

// EmojiImages supplies decoded emoji images (imgcache.Cache satisfies it).
type EmojiImages interface {
	Get(url string, sizePx int) (paint.ImageOp, bool)
}

var shaperThemes sync.Map // *text.Shaper -> *Theme

// SetShaper installs the shaper and registers the theme for DrawText.
func (t *Theme) SetShaper(sh *text.Shaper) {
	t.Shaper = sh
	shaperThemes.Store(sh, t)
}

// ForShaper returns the theme that owns sh (nil if none).
func ForShaper(sh *text.Shaper) *Theme {
	if v, ok := shaperThemes.Load(sh); ok {
		return v.(*Theme)
	}
	return nil
}

// DrawText is a drop-in for widget.Label for packages that only hold a
// shaper: same layout, plus Embolden, emoji images and ignorable-codepoint
// cleanup when the shaper belongs to a Theme.
func DrawText(gtx layout.Context, sh *text.Shaper, o LabelOpts, txt string) layout.Dimensions {
	if t := ForShaper(sh); t != nil {
		return t.Text(gtx, o, txt)
	}
	return drawText(gtx, sh, o, CleanText(txt), Embolden)
}

// Text draws txt with Slack-matching rendering. Single-line text containing
// unicode emoji draws them as Slack's CDN images (like the web client's
// emoji sprites) instead of relying on a color font.
func (t *Theme) Text(gtx layout.Context, o LabelOpts, txt string) layout.Dimensions {
	txt = CleanText(txt)
	if o.MaxLines == 1 && t.Images != nil {
		if segs := splitEmoji(txt); segs != nil {
			return t.textWithEmoji(gtx, o, segs)
		}
	}
	return drawText(gtx, t.Shaper, o, txt, Embolden)
}

// EmojiURL is the CDN image for an emoji image stem in the theme's style.
func (t *Theme) EmojiURL(stem string) string {
	style := t.EmojiStyle
	if style == "" {
		style = "apple"
	}
	return emoji.CDN + style + "-medium/" + stem + ".png"
}

func (t *Theme) textWithEmoji(gtx layout.Context, o LabelOpts, segs []segment) layout.Dimensions {
	maxW := gtx.Constraints.Max.X
	es := gtx.Sp(o.Size * 6 / 5) // Slack: 18px emoji next to 15px text
	type placed struct {
		call  op.CallOp
		x, w  int
		h, bl int // height and baseline (from top) for text; 0 for emoji
		emoji string
	}
	var ps []placed
	x, asc, desc := 0, 0, 0
	ellW := -1
	for _, s := range segs {
		if x >= maxW {
			break
		}
		if s.emoji != "" {
			if x+es > maxW {
				if ellW < 0 {
					ellW = t.Measure(gtx, o.Size, o.Font.Weight, "…")
				}
				m := op.Record(gtx.Ops)
				g := gtx
				g.Constraints = layout.Constraints{Max: image.Pt(maxW-x, gtx.Constraints.Max.Y)}
				d := drawText(g, t.Shaper, LabelOpts{Font: o.Font, Size: o.Size, LineHeight: o.LineHeight, MaxLines: 1, Color: o.Color}, "…", Embolden)
				ps = append(ps, placed{call: m.Stop(), x: x, w: d.Size.X, h: d.Size.Y, bl: d.Size.Y - d.Baseline})
				break
			}
			ps = append(ps, placed{x: x, w: es, emoji: s.emoji})
			x += es + gtx.Dp(1)
			continue
		}
		m := op.Record(gtx.Ops)
		g := gtx
		g.Constraints = layout.Constraints{Max: image.Pt(maxW-x, gtx.Constraints.Max.Y)}
		lo := o
		lo.MaxLines = 1
		d := drawText(g, t.Shaper, lo, s.text, Embolden)
		ps = append(ps, placed{call: m.Stop(), x: x, w: d.Size.X, h: d.Size.Y, bl: d.Size.Y - d.Baseline})
		if a := d.Size.Y - d.Baseline; a > asc {
			asc = a
		}
		if d.Baseline > desc {
			desc = d.Baseline
		}
		x += d.Size.X
	}
	if asc == 0 { // emoji only: synthesize line metrics from the font size
		px := gtx.Sp(o.Size)
		asc, desc = px*95/100, px*25/100
		if lh := gtx.Sp(o.LineHeight); lh > asc+desc {
			extra := lh - asc - desc
			asc += extra / 2
			desc += extra - extra/2
		}
	}
	h := asc + desc
	for _, p := range ps {
		if p.emoji != "" {
			// center the image on the text's x-height band, Slack-like
			y := asc - es*4/5
			if y+es > h {
				y = h - es
			}
			if y < 0 {
				y = 0
			}
			if img, ok := t.Images.Get(t.EmojiURL(p.emoji), es*2); ok {
				st := op.Offset(image.Pt(p.x, y)).Push(gtx.Ops)
				Image(gtx, img, image.Pt(es, es), 0)
				st.Pop()
			}
			continue
		}
		st := op.Offset(image.Pt(p.x, asc-p.bl)).Push(gtx.Ops)
		p.call.Add(gtx.Ops)
		st.Pop()
	}
	w := x
	if w > maxW {
		w = maxW
	}
	if w < gtx.Constraints.Min.X {
		w = gtx.Constraints.Min.X
	}
	return layout.Dimensions{Size: image.Pt(w, h), Baseline: desc}
}

// ---- glyph painting (adapted from gioui.org/widget's Label, MIT/Unlicense) ----

func drawText(gtx layout.Context, sh *text.Shaper, o LabelOpts, txt string, emb float32) layout.Dimensions {
	cs := gtx.Constraints
	trunc := ""
	if o.MaxLines > 0 {
		trunc = "…"
	}
	sh.LayoutString(text.Parameters{
		Font:       o.Font,
		PxPerEm:    fixed.I(gtx.Sp(o.Size)),
		MaxLines:   o.MaxLines,
		Truncator:  trunc,
		Alignment:  o.Align,
		WrapPolicy: o.Wrap,
		MaxWidth:   cs.Max.X,
		MinWidth:   cs.Min.X,
		Locale:     gtx.Locale,
		LineHeight: fixed.I(gtx.Sp(o.LineHeight)),
	}, txt)
	m := op.Record(gtx.Ops)
	viewport := image.Rectangle{Max: cs.Max}
	it := glyphIter{viewport: viewport, maxLines: o.MaxLines, col: o.Color, emb: emb * gtx.Metric.PxPerDp}
	var glyphs [32]text.Glyph
	line := glyphs[:0]
	for g, ok := sh.NextGlyph(); ok; g, ok = sh.NextGlyph() {
		var ok bool
		if line, ok = it.paint(gtx, sh, g, line); !ok {
			break
		}
	}
	call := m.Stop()
	viewport.Min = viewport.Min.Add(it.padding.Min)
	viewport.Max = viewport.Max.Add(it.padding.Max)
	cl := clip.Rect(viewport).Push(gtx.Ops)
	call.Add(gtx.Ops)
	cl.Pop()
	dims := layout.Dimensions{Size: cs.Constrain(it.bounds.Size())}
	dims.Baseline = dims.Size.Y - it.baseline
	return dims
}

type glyphIter struct {
	viewport  image.Rectangle
	maxLines  int
	col       color.NRGBA
	emb       float32
	linesSeen int
	lineOff   f32.Point
	padding   image.Rectangle
	bounds    image.Rectangle
	visible   bool
	first     bool
	baseline  int
}

func (it *glyphIter) process(g text.Glyph) bool {
	if it.maxLines > 0 {
		if g.Flags&text.FlagLineBreak != 0 {
			it.linesSeen++
		}
		if it.linesSeen == it.maxLines && g.Flags&text.FlagParagraphBreak != 0 {
			return false
		}
	}
	if d := g.Bounds.Min.X.Floor(); d < it.padding.Min.X {
		it.padding.Min.X = d
	}
	if d := (g.Bounds.Max.X - g.Advance).Ceil(); d > it.padding.Max.X {
		it.padding.Max.X = d
	}
	if d := (g.Bounds.Min.Y + g.Ascent).Floor(); d < it.padding.Min.Y {
		it.padding.Min.Y = d
	}
	if d := (g.Bounds.Max.Y - g.Descent).Ceil(); d > it.padding.Max.Y {
		it.padding.Max.Y = d
	}
	// room for the embolden stroke
	it.padding.Min.X, it.padding.Max.X = it.padding.Min.X-1, it.padding.Max.X+1
	lb := image.Rectangle{
		Min: image.Pt(g.X.Floor(), int(g.Y)-g.Ascent.Ceil()),
		Max: image.Pt((g.X + g.Advance).Ceil(), int(g.Y)+g.Descent.Ceil()),
	}
	if !it.first {
		it.first = true
		it.baseline = int(g.Y)
		it.bounds = lb
	}
	above := lb.Max.Y < it.viewport.Min.Y
	below := lb.Min.Y > it.viewport.Max.Y
	left := lb.Max.X < it.viewport.Min.X
	right := lb.Min.X > it.viewport.Max.X
	it.visible = !above && !below && !left && !right
	if it.visible {
		it.bounds.Min.X = min(it.bounds.Min.X, lb.Min.X)
		it.bounds.Min.Y = min(it.bounds.Min.Y, lb.Min.Y)
		it.bounds.Max.X = max(it.bounds.Max.X, lb.Max.X)
		it.bounds.Max.Y = max(it.bounds.Max.Y, lb.Max.Y)
	}
	return !below
}

func (it *glyphIter) paint(gtx layout.Context, sh *text.Shaper, g text.Glyph, line []text.Glyph) ([]text.Glyph, bool) {
	ok := it.process(g)
	if it.visible {
		if len(line) == 0 {
			it.lineOff = f32.Point{X: float32(g.X) / 64, Y: float32(g.Y)}.Sub(layout.FPt(it.viewport.Min))
		}
		line = append(line, g)
	}
	if g.Flags&text.FlagLineBreak != 0 || cap(line)-len(line) == 0 || !ok {
		t := op.Affine(f32.AffineId().Offset(it.lineOff)).Push(gtx.Ops)
		path := sh.Shape(line)
		paint.FillShape(gtx.Ops, it.col, clip.Outline{Path: path}.Op())
		if it.emb > 0 {
			paint.FillShape(gtx.Ops, it.col, clip.Stroke{Path: path, Width: it.emb}.Op())
		}
		if call := sh.Bitmaps(line); call != (op.CallOp{}) {
			call.Add(gtx.Ops)
		}
		t.Pop()
		line = line[:0]
	}
	return line, ok
}

// ---- unicode cleanup and emoji segmentation ----

// CleanText drops default-ignorable code points that fonts draw as tofu
// (Hangul fillers used for "blank" channel names, zero-width spaces, BOM),
// keeping ZWJ/VS16 which emoji sequences need.
func CleanText(s string) string {
	fast := true
	for i := 0; i < len(s); i++ {
		if s[i] >= 0xe1 { // U+1000 and above start with 0xe1+
			fast = false
			break
		}
	}
	if fast {
		return s
	}
	drop := func(r rune) bool {
		switch r {
		case 0x115F, 0x1160, 0x3164, 0xFFA0, 0x200B, 0x200C, 0x200E, 0x200F, 0x2060, 0xFEFF, 0x180E, 0x17B4, 0x17B5:
			return true
		}
		return false
	}
	keep := true
	for _, r := range s {
		if drop(r) {
			keep = false
			break
		}
	}
	if keep {
		return s
	}
	var b strings.Builder
	for _, r := range s {
		if !drop(r) {
			b.WriteRune(r)
		}
	}
	return b.String()
}

type segment struct {
	text  string
	emoji string // image stem when this segment is an emoji
}

var (
	emojiOnce   sync.Once
	emojiIndex  map[string]string // runes without VS16, as a string -> image stem
	emojiMaxLen int

	splitMu    sync.Mutex
	splitCache = map[string][]segment{}
)

func loadEmojiIndex() {
	var f struct {
		Emoji []struct {
			I string            `json:"i"`
			U string            `json:"u"`
			S map[string]string `json:"s"`
		} `json:"emoji"`
	}
	_ = json.Unmarshal(emoji.StandardJSON, &f)
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
		k := b.String()
		if _, ok := emojiIndex[k]; !ok {
			emojiIndex[k] = stem
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

// mayStartEmoji is a cheap pre-filter.
func mayStartEmoji(r rune) bool {
	return r >= 0x203C || r == '#' || r == '*' || (r >= '0' && r <= '9') || r == 0xA9 || r == 0xAE
}

// splitEmoji returns text/emoji segments, or nil when s has no emoji.
func splitEmoji(s string) []segment {
	has := false
	for i := 0; i < len(s); i++ {
		if s[i] >= 0xe2 || s[i] == 0xc2 { // U+2000+ (and ©/®)
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
	var segs []segment
	var txt strings.Builder
	found := false
	for i := 0; i < len(rs); {
		r := rs[i]
		if mayStartEmoji(r) && (r >= 0x203C || (i+1 < len(rs) && (rs[i+1] == 0xFE0F || rs[i+1] == 0x20E3))) {
			// longest match, ignoring VS16
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
					segs = append(segs, segment{text: txt.String()})
					txt.Reset()
				}
				segs = append(segs, segment{emoji: best})
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
		segs = append(segs, segment{text: txt.String()})
	}
	splitMu.Lock()
	if len(splitCache) > 4096 {
		splitCache = map[string][]segment{}
	}
	splitCache[s] = segs
	splitMu.Unlock()
	return segs
}

var _ = utf8.RuneError
