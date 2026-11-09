package theme

import (
	_ "embed"
	"image"
	"strings"
	"sync"
	"sync/atomic"

	"gioui.org/layout"
	"gioui.org/op"
	"gioui.org/text"
	"golang.org/x/image/math/fixed"
)

// Unicode emoji in wrapping labels.
//
// Slack's web client draws unicode emoji in UI text as its own emoji
// images (the c-emoji sprites), never as a font's glyphs. Single-line
// labels did that already (textWithEmoji). Wrapping labels (para, Para,
// any MaxLines other than 1: profile titles, custom fields, topics,
// modal bodies) went straight to Gio's shaper, where Lato has no emoji
// and the system fallback found none on macOS, so 😱 drew Lato's .notdef
// box.
//
// Each emoji now becomes one placeholder rune shaped from emojislot.ttf
// (one empty glyph, 1.27em wide: the 1.2em image plus Slack's 1px gap;
// emojislot_gen.py builds it). The same face also maps the emoji
// themselves for editors, which must keep the real runes (editoremoji.go).
// Gio wraps, aligns and truncates the line as usual, and the image is
// drawn where each placeholder glyph landed.

// EmojiSlotTypeface is the placeholder face's family (last in FacesFor).
const EmojiSlotTypeface = "Clack Emoji Slot"

// emojiSlotRune is the placeholder's code point (plane 16 private use: no
// system font maps it, so Gio always resolves it to the slot face).
const emojiSlotRune = '\U0010FFFD'

// emojiSlotGID is the placeholder's glyph in emojislot.ttf (2 is a space).
const emojiSlotGID = 1

//go:embed emojislot.ttf
var emojiSlotTTF []byte

// slotDraw collects the placeholder glyphs of one drawText call.
type slotDraw struct {
	face  int      // slot face index in the shaper's collection
	stems []string // emoji image stems, in text order
	n     int      // placeholders seen so far (visible or not)
	pos   []slotPos
}

type slotPos struct {
	stem string
	x    fixed.Int26_6 // pen position of the placeholder
	adv  fixed.Int26_6
	y    int32 // baseline
}

// see records g when it is a placeholder; it reports whether it was one.
func (s *slotDraw) see(g text.Glyph, visible bool) bool {
	if s == nil {
		return false
	}
	if _, fi, gid := splitGlyphID(g.ID); fi != s.face || gid != emojiSlotGID {
		return false // text, or a space go-text shaped with the slot face
	}
	if visible && s.n < len(s.stems) {
		s.pos = append(s.pos, slotPos{stem: s.stems[s.n], x: g.X, adv: g.Advance, y: g.Y})
	}
	s.n++
	return true
}

// slotText replaces each emoji of segs with emojiSlotRune and returns
// the stems in order.
func slotText(segs []segment) (string, []string) {
	var b strings.Builder
	var stems []string
	for _, s := range segs {
		if s.emoji != "" {
			b.WriteRune(emojiSlotRune)
			stems = append(stems, s.emoji)
			continue
		}
		b.WriteString(s.text)
	}
	return b.String(), stems
}

// textWithSlots draws wrapping text whose emoji are images.
func (t *Theme) textWithSlots(gtx layout.Context, o LabelOpts, segs []segment, face int) layout.Dimensions {
	s, stems := slotText(segs)
	sd := &slotDraw{face: face, stems: stems}
	d := drawTextSlots(gtx, t.Shaper, o, s, EmboldenFor(o.Color), sd)
	es := gtx.Sp(o.Size * 6 / 5) // the single-line size: 18px next to 15px text
	for _, p := range sd.pos {
		img, ok := t.Images.Get(t.EmojiURL(p.stem), es*2)
		if !ok {
			continue
		}
		x := p.x.Round() + (p.adv.Round()-es)/2
		y := int(p.y) - es*4/5
		st := op.Offset(image.Pt(x, y)).Push(gtx.Ops)
		Image(gtx, img, image.Pt(es, es), 0)
		st.Pop()
	}
	return d
}

// ---- tofu scan (tests) ----

// TofuStats counts glyphs a scan saw: Notdef from any face's glyph 0 (the
// tofu box), Fallback from a face outside the shaper's own collection (a
// system font: what Gio picks for an emoji when no image replaced it).
type TofuStats struct {
	Glyphs, Notdef, Fallback int
	// Slots counts blank emoji slot glyphs: placeholders whose image is
	// drawn over them, or emoji an editor font shaped with no image yet.
	Slots int
	Texts []string // every string Theme.Text (and Theme.Editor) drew, in order
}

var (
	scanOn    atomic.Bool
	scanMu    sync.Mutex
	scanStats TofuStats
)

// StartTofuScan starts counting the glyphs every text path shapes (theme
// labels, richview, slackc report through ScanGlyph); stop ends the scan
// and returns the counts. Scans don't nest.
func StartTofuScan() (stop func() TofuStats) {
	scanMu.Lock()
	scanStats = TofuStats{}
	scanMu.Unlock()
	scanOn.Store(true)
	return func() TofuStats {
		scanOn.Store(false)
		scanMu.Lock()
		defer scanMu.Unlock()
		return scanStats
	}
}

// scanText records a label drawn during a scan.
func scanText(s string) {
	if !scanOn.Load() {
		return
	}
	scanMu.Lock()
	scanStats.Texts = append(scanStats.Texts, s)
	scanMu.Unlock()
}

// ScanGlyph reports a glyph sh shaped to a running tofu scan (one atomic
// load otherwise).
func ScanGlyph(sh *text.Shaper, g text.Glyph) {
	if !scanOn.Load() {
		return
	}
	if g.Flags&text.FlagParagraphBreak != 0 && g.Runes == 0 {
		return
	}
	_, fi, gid := splitGlyphID(g.ID)
	nf, slot := -1, -1
	if v, ok := shaperFaces.Load(sh); ok {
		nf, slot = len(v.(*emboldener).faces), v.(*emboldener).slot
	}
	scanMu.Lock()
	scanStats.Glyphs++
	if fi == slot && gid == emojiSlotGID {
		scanStats.Slots++
	}
	if gid == 0 && g.Flags&text.FlagParagraphBreak == 0 {
		scanStats.Notdef++
	}
	if nf >= 0 && fi >= nf {
		scanStats.Fallback++
	}
	scanMu.Unlock()
}
