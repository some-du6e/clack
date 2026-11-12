package theme

import (
	"image"
	"strings"

	"gioui.org/font"
	"gioui.org/layout"
	"gioui.org/op"
	"gioui.org/op/clip"
	"gioui.org/text"
	"gioui.org/unit"
	"gioui.org/widget"
	"golang.org/x/image/math/fixed"
)

// Unicode emoji in editors.
//
// Slack's inputs never show a font's emoji: the composer turns them into
// emoji blots (images), and its other texty inputs draw the same sprites.
// Labels swap each emoji for a placeholder rune (emojislot.go), but an
// editor can't: its text is what the user typed, and caret movement,
// selection, IME and undo all count its runes. So editors keep the real
// emoji and change only the font: EditorFont names the slot face after
// Lato. That face maps every emoji code point Lato lacks to an empty glyph
// and ligates ZWJ sequences and flags (emojislot_gen.py), so each emoji
// grapheme shapes as one 1.27em blank glyph, Gio's editor moves over it as
// one cluster, and EditorEmoji draws Slack's image where it landed.

// EditorFont returns f with the emoji slot face as the last family, so
// emoji Lato lacks shape as blank slots rather than tofu or a system font.
func EditorFont(f font.Font) font.Font {
	if f.Typeface == "" {
		f.Typeface = Typeface
	}
	if !strings.Contains(string(f.Typeface), EmojiSlotTypeface) {
		f.Typeface += ", " + EmojiSlotTypeface
	}
	return f
}

// EmojiRun is one emoji sequence in a text: runes [Start, End) and the
// image stem Slack draws for it.
type EmojiRun struct {
	Start, End int
	Stem       string
}

// EmojiRuns lists the emoji sequences of rs (longest match, VS16
// ignored), the ones labels draw as images.
func EmojiRuns(rs []rune) []EmojiRun {
	var out []EmojiRun
	for i := 0; i < len(rs); {
		if stem, end := matchEmoji(rs, i); stem != "" {
			out = append(out, EmojiRun{Start: i, End: end, Stem: stem})
			i = end
			continue
		}
		i++
	}
	return out
}

// mayHaveEmoji is EmojiRuns' cheap byte pre-filter (U+2000 and up, ©, ®).
func mayHaveEmoji(s string) bool {
	for i := 0; i < len(s); i++ {
		if s[i] >= 0xe2 || s[i] == 0xc2 {
			return true
		}
	}
	return false
}

// IsEmojiSlot reports whether g is the slot face's blank emoji glyph in
// a shaper built by NewShaperFor.
func IsEmojiSlot(sh *text.Shaper, g text.Glyph) bool {
	slot := slotIndex(sh)
	if slot < 0 {
		return false
	}
	_, fi, gid := splitGlyphID(g.ID)
	return fi == slot && gid == emojiSlotGID
}

// shapesAsText reports whether the shaper's Lato maps r, so an emoji
// starting with r draws as Lato's glyph and needs no image.
func shapesAsText(sh *text.Shaper, r rune) bool {
	v, ok := shaperFaces.Load(sh)
	if !ok {
		return false
	}
	fs := v.(*emboldener).faces
	if len(fs) == 0 || fs[0] == nil {
		return false
	}
	_, has := fs[0].NominalGlyph(r)
	return has
}

// EditorEmoji draws Slack's emoji images over the emoji in e, which was
// just laid out with EditorFont at size and returned d. It is a no-op
// for shapers without a Theme or images, and for masked editors.
func EditorEmoji(gtx layout.Context, sh *text.Shaper, e *widget.Editor, size unit.Sp, d layout.Dimensions) {
	t := ForShaper(sh)
	if t == nil || t.Images == nil || e.Mask != 0 || e.Len() == 0 {
		return
	}
	txt := e.Text()
	if !mayHaveEmoji(txt) {
		return
	}
	runs := EmojiRuns([]rune(txt))
	if len(runs) == 0 {
		return
	}
	es := gtx.Sp(size * 6 / 5)
	cl := clip.Rect(image.Rectangle{Max: d.Size}).Push(gtx.Ops)
	defer cl.Pop()
	var regs []widget.Region
	rs := []rune(txt)
	for _, r := range runs {
		if shapesAsText(sh, rs[r.Start]) {
			continue
		}
		regs = e.Regions(r.Start, r.End, regs[:0])
		if len(regs) == 0 {
			continue
		}
		img, ok := t.Images.Get(t.EmojiURL(r.Stem), es*2)
		if !ok {
			continue // loading: the blank slot holds its place
		}
		b := regs[0].Bounds
		base := b.Max.Y - regs[0].Baseline
		x := b.Min.X + (b.Dx()-es)/2
		y := base - es*4/5
		st := op.Offset(image.Pt(x, y)).Push(gtx.Ops)
		Image(gtx, img, image.Pt(es, es), 0)
		st.Pop()
	}
}

// scanEditor reports an editor's glyphs to a running tofu scan: it shapes
// the text the way widget.Editor does (same font, size and width).
func scanEditor(gtx layout.Context, sh *text.Shaper, e *widget.Editor, f font.Font, size unit.Sp) {
	if !scanOn.Load() {
		return
	}
	txt := e.Text()
	scanText(txt)
	if txt == "" {
		return
	}
	if e.Mask != 0 {
		return
	}
	maxW := gtx.Constraints.Max.X
	if e.SingleLine || maxW <= 0 {
		maxW = 1 << 20
	}
	params := text.Parameters{
		Font:     f,
		PxPerEm:  fixed.I(gtx.Sp(size)),
		MaxWidth: maxW,
		Locale:   gtx.Locale,
	}
	paramsPxPerSp(&params, gtx.Metric.PxPerSp)
	sh.LayoutString(params, txt)
	for g, ok := sh.NextGlyph(); ok; g, ok = sh.NextGlyph() {
		ScanGlyph(sh, g)
	}
}
