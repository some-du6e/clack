package theme

import (
	"encoding/json"
	"image"
	"image/color"
	"os"
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

	"github.com/some-du6e/clack/internal/emoji"
)

// Embolden is the outline stroke, in device pixels (not dp), added to dark
// glyphs. Gio blends glyph coverage in linear light, so dark-on-light Lato
// renders thinner than Chrome's. The missing ink is a fixed pixel amount at
// any scale. Measured by total ink against Chrome at 1x: UI labels (13px
// topic) match at ~0.31px, message bodies (15px, ui-messages at 1x and 2x)
// at ~0.45px.
const (
	Embolden     = 0.3  // labels (Text / DrawText), regular weight
	BodyEmbolden = 0.33 // richview message bodies (BodyEmboldenFor): light bodies were -3% at 0.3 (1x and 2x)
)

// Outline emboldening on the embedded Lato (2026-11-07, 1x, ink vs Chrome):
// regular dark text matches at ~0.29px (13px topic, 15px bodies: the old
// 0.45 body value was for the stroke embolden on the system Lato), Bold at
// ~0.32 and Black (sender names) at ~0.47, so heavier faces scale it up.
func weightEmbolden(w font.Weight) float32 {
	switch {
	case w >= font.Black:
		return 1.7 // names: -1.6% (1x) / -6.2% (2x) at 1.6
	case w >= font.Bold:
		return 1.1
	}
	return 1
}

// BodyEmboldenFor is EmboldenFor at the message-body strength.
func BodyEmboldenFor(c color.NRGBA) float32 { return EmboldenFor(c) * BodyEmbolden / Embolden }

// LightTextFade fades light text (luminance >= 0.6, i.e. light-on-dark) in
// labels to cancel the extra weight Gio's linear-light glyph blending gives
// it compared with Chrome. The excess is an edge effect that halves at 2x,
// so alpha = 1 - LightTextFade/pxPerDp (ui-messages' model; richview uses
// its own k for message bodies, Style.LightTextFade). Measured on the dark
// sidebar (15px lilac at 80% on aubergine) at 1x: alpha 0.85 → 96.8k,
// 0.75 → 90.9k ink vs Chrome 94.1k, so k = 0.2. CLACK_LABEL_TEXT_FADE
// overrides it for tuning; 0 = off.
var LightTextFade = envFloat("CLACK_LABEL_TEXT_FADE", 0.2)

func envFloat(name string, def float32) float32 {
	if v, err := strconv.ParseFloat(os.Getenv(name), 32); err == nil {
		return float32(v)
	}
	return def
}

// lightTextColor applies LightTextFade to light glyph colors.
//
// Measured at 1x on the embedded Lato vs Chrome (ink with a fixed white
// reference), regular weight:
//   - on the aubergine chrome (chrome=true), sidebar rows (#f6e4ff at .9
//     over light mode's aubergine, #e7d2ef flat, y=.85) match at k≈0.15;
//     dimmer chrome text needs more, as on the main pane: dark mode's
//     rows (#e3ceeb at .8 over #201024, #bca8c3 flat, y=.69) came out
//     9-11% heavy at 0.15 (feel-shots against Chrome drawing Slack's rows
//     in the same colours), +3-6% at 0.23, so k = 0.15 + max(0, .85-y)·0.65
//     (0.25 there);
//   - on the dark main pane, the fade grows as the text dims: #b9babd
//     (topic, y=.73) at 0.26-0.27 and #ababad (timestamps, y=.67) at 0.39,
//     so k = 0.27 + max(0, .73-y)·2.
//
// The result is scaled by LightTextFade/0.2 (the env knob). Bold needs 3/4
// of it (dark tabs, #b9babd and #f8f8f8). Black gets no fade: the white
// team name is already light, see lightBlackEmbolden.
func lightTextColor(c color.NRGBA, pxPerDp float32, w font.Weight, chrome bool) color.NRGBA {
	if LightTextFade <= 0 || pxPerDp <= 0 || w >= font.Black || !LegacyTextInk() {
		return c
	}
	y := (0.299*float32(c.R) + 0.587*float32(c.G) + 0.114*float32(c.B)) / 255
	if y < 0.6 {
		return c
	}
	k := 0.15 + max(0, 0.85-y)*0.65 // light rows -3.5% (1x) / -2.9% (2x) at 0.17
	if !chrome {
		k = 0.27 + max(0, 0.73-y)*2
	}
	k = min(k, 0.45) * LightTextFade / 0.2
	if w >= font.Bold {
		k *= 0.75
	}
	a := 1 - k/pxPerDp
	if a <= 0 || a >= 1 {
		return c
	}
	c.A = uint8(float32(c.A)*a + 0.5)
	return c
}

// lightBlackEmbolden (px) gives light Black text (the sidebar team name)
// the ink it lacks next to Chrome: 8% at 1x unfaded.
const lightBlackEmbolden = 0.12

// IsDarkText reports whether c is dark text (luminance under 0.5).
func IsDarkText(c color.NRGBA) bool {
	return 0.299*float32(c.R)+0.587*float32(c.G)+0.114*float32(c.B) < 0.5*255
}

// EmboldenFor is the stroke (px) for text of color c: Embolden for dark
// text, fading to none for light text (light-on-dark is already heavy in
// linear blending). CLACK_NOEMBOLDEN=1 disables it for A/B timing. None
// under Skia's model (textgamma.go), which inks dark text with contrast.
func EmboldenFor(c color.NRGBA) float32 {
	if noEmbolden || !LegacyTextInk() {
		return 0
	}
	return emboldenStroke(c)
}

func emboldenStroke(c color.NRGBA) float32 {
	y := (0.299*float32(c.R) + 0.587*float32(c.G) + 0.114*float32(c.B)) / 255
	// Mid greys need much less than dark text: #616061 (timestamps, y=.38)
	// matches Chrome at ~0.15px, #454447 (y=.27) at the full 0.3.
	switch {
	case y <= 0.33:
		return Embolden
	case y >= 0.43:
		return 0
	}
	return Embolden * (0.43 - y) / 0.10
}

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

var noEmbolden = envOn("CLACK_NOEMBOLDEN") || envOn("YOLOSLACK_NOEMBOLDEN")

// envOn reports whether a boolean env var is set: "", "0", "false", "no"
// and "off" mean unset, so CLACK_NOEMBOLDEN=0 keeps the embolden.
func envOn(name string) bool {
	switch strings.ToLower(strings.TrimSpace(os.Getenv(name))) {
	case "", "0", "false", "no", "off":
		return false
	}
	return true
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
	o.Color = lightTextColor(o.Color, gtx.Metric.PxPerDp, o.Font.Weight, false)
	return drawText(gtx, sh, o, CleanText(txt), EmboldenFor(o.Color))
}

// Text draws txt with Slack-matching rendering. Unicode emoji draw as
// Slack's CDN images (like the web client's emoji sprites) instead of
// relying on a color font: single lines through textWithEmoji, wrapping
// text through placeholder glyphs (emojislot.go).
func (t *Theme) Text(gtx layout.Context, o LabelOpts, txt string) layout.Dimensions {
	if DrawHook != nil {
		DrawHook(txt)
	}
	return t.text(gtx, o, txt, true)
}

// DrawHook, when set (tests: the "no English left" checks), sees every
// string Text draws.
var DrawHook func(txt string)

// text is Text; scan says whether a tofu scan records txt as drawn
// (Measure's layouts aren't).
func (t *Theme) text(gtx layout.Context, o LabelOpts, txt string, scan bool) layout.Dimensions {
	txt = CleanText(txt)
	if scan {
		scanText(txt)
	}
	o.Color = lightTextColor(o.Color, gtx.Metric.PxPerDp, o.Font.Weight, t.Chrome)
	if t.Images != nil {
		if segs := splitEmoji(txt); segs != nil {
			if o.MaxLines == 1 {
				return t.textWithEmoji(gtx, o, segs)
			}
			if face := slotIndex(t.Shaper); face >= 0 {
				return t.textWithSlots(gtx, o, segs, face)
			}
		}
	}
	return drawText(gtx, t.Shaper, o, txt, EmboldenFor(o.Color))
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
				d := drawText(g, t.Shaper, LabelOpts{Font: o.Font, Size: o.Size, LineHeight: o.LineHeight, MaxLines: 1, Color: o.Color}, "…", EmboldenFor(o.Color))
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
		d := drawText(g, t.Shaper, lo, s.text, EmboldenFor(o.Color))
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
	return drawTextSlots(gtx, sh, o, txt, emb, nil)
}

// drawTextSlots is drawText recording emoji placeholder glyphs in slots
// (nil: none) instead of painting them.
func drawTextSlots(gtx layout.Context, sh *text.Shaper, o LabelOpts, txt string, emb float32, slots *slotDraw) layout.Dimensions {
	emb *= weightEmbolden(o.Font.Weight)
	if emb == 0 && o.Font.Weight >= font.Black && !noEmbolden && !IsDarkText(o.Color) && LegacyTextInk() {
		emb = lightBlackEmbolden
	}
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
	it := glyphIter{viewport: viewport, maxLines: o.MaxLines, col: o.Color, emb: emb, slots: slots} // px, not dp
	var glyphs [512]text.Glyph                                                                      // one batch per line in practice (see paint)
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
	slots     *slotDraw // emoji placeholders (Text's wrapping path), or nil
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
	ScanGlyph(sh, g)
	if it.slots.see(g, it.visible) {
		// an emoji placeholder: the image is drawn instead (textWithSlots)
	} else if it.visible {
		if len(line) == 0 {
			it.lineOff = f32.Point{X: float32(g.X) / 64, Y: float32(g.Y)}.Sub(layout.FPt(it.viewport.Min))
		}
		line = append(line, g)
	}
	if g.Flags&text.FlagLineBreak != 0 || cap(line)-len(line) == 0 || !ok {
		t := op.Affine(f32.AffineId().Offset(it.lineOff)).Push(gtx.Ops)
		// Dark text gets its extra ink from an emboldened outline (one
		// fill, see EmboldenedShape) under the legacy model, from Skia's
		// contrast under the Windows one (FillText); fallback-font runs
		// keep the stroke.
		if path, ok := EmboldenedShape(sh, line, it.emb); ok {
			FillText(gtx.Ops, it.col, path)
		} else {
			path := sh.Shape(line)
			FillText(gtx.Ops, it.col, path)
			if it.emb > 0 {
				paint.FillShape(gtx.Ops, it.col, clip.Stroke{Path: path, Width: it.emb}.Op())
			}
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
// (zero-width spaces, BOM), keeping ZWJ/VS16 which emoji sequences need.
// The Hangul fillers (U+115F, U+1160, U+3164, U+FFA0) stay: they are
// default-ignorable too, but HarfBuzz (Chrome's shaper and go-text's,
// harfbuzz/unicode.go) shapes them as ordinary spacing glyphs, so Slack
// draws a channel named only "ㅤ" as the fallback font's blank glyph, or
// as tofu where no font has one (Linux Chrome). Gio's system-font
// fallback does the same (Lato's .notdef box as the last resort); dropping
// them left the name empty.
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
		case 0x200B, 0x200C, 0x200E, 0x200F, 0x2060, 0xFEFF, 0x180E, 0x17B4, 0x17B5:
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

// matchEmoji returns the image stem of the longest emoji sequence starting
// at rs[i] (VS16 ignored) and the index just past it, or "" when none
// starts there.
func matchEmoji(rs []rune, i int) (stem string, end int) {
	r := rs[i]
	if !mayStartEmoji(r) || !(r >= 0x203C || (i+1 < len(rs) && (rs[i+1] == 0xFE0F || rs[i+1] == 0x20E3))) {
		return "", 0
	}
	emojiOnce.Do(loadEmojiIndex)
	var kb [16]rune
	key := kb[:0]
	for j := i; j < len(rs) && len(key) < emojiMaxLen; j++ {
		if rs[j] == 0xFE0F {
			continue
		}
		key = append(key, rs[j])
		if s, ok := emojiIndex[string(key)]; ok {
			stem, end = s, j+1
		}
	}
	if stem != "" {
		for end < len(rs) && rs[end] == 0xFE0F {
			end++
		}
	}
	return stem, end
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
		if best, bestEnd := matchEmoji(rs, i); best != "" {
			if txt.Len() > 0 {
				segs = append(segs, segment{text: txt.String()})
				txt.Reset()
			}
			segs = append(segs, segment{emoji: best})
			found = true
			i = bestEnd
			continue
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
