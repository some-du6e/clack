package theme

import (
	"bytes"
	"errors"
	"fmt"
	"sync"
	"sync/atomic"

	"gioui.org/font"
	"gioui.org/text"
	gtfont "github.com/go-text/typesetting/font"
	gtot "github.com/go-text/typesetting/font/opentype"
)

// The UI font (Preferences > Appearance > Font).
//
// Slack's Theme component sets :root's --font-family-default to the
// chosen font and gives <body> the use-theme-font class, and body's
// font-family is var(--font-family-default): every label, message and
// input switches, while code keeps --font-family-monospace and emoji are
// images. Clack's equivalent is the UI family: every Font(w) asks for
// Typeface, so a shaper whose UI family holds the chosen font's faces
// draws the whole app in it. MonoTypeface (code) and the emoji slot face
// are untouched.

// Family is the family name the UI faces are registered under, and
// ExtFamily (and ExtFamilies) the ones for a font's unicode-range
// companions: Slack's OpenDyslexic and Roboto Mono have Cyrillic and
// box-drawing files. Chrome prefers those within their unicode-range;
// Gio has no unicode-range, so here they fill in what the main face
// lacks.
const (
	Family    = "Clack Lato"
	ExtFamily = "Clack Lato Ext"
)

// ExtFamilies are the companion families in Typeface's order: Gio tries
// one face per family, so each unicode-range group needs its own.
var ExtFamilies = [2]string{ExtFamily, ExtFamily + "2"}

// Slot is a weight and style the UI family registers a face under.
type Slot struct {
	Weight font.Weight
	Style  font.Style
}

// Slots are the UI family's faces in collection order: Lato's eight
// files (FacesFor). A UIFont fills the same slots, so every Font(w)
// request resolves exactly as it does with Lato.
var Slots = [8]Slot{
	{font.Normal, font.Regular}, {font.Bold, font.Regular}, {font.Black, font.Regular}, {font.Light, font.Regular},
	{font.Normal, font.Italic}, {font.Bold, font.Italic}, {font.Black, font.Italic}, {font.Light, font.Italic},
}

// FaceSlot is one face of a UI font: a font file (or one member of a
// collection) at a variation instance, registered under Family (or
// ExtFamily, or a preview family) with a weight and style.
type FaceSlot struct {
	Family string // "" is Family
	Slot
	Data  []byte
	Index int                // collection member
	Vars  []gtfont.Variation // design coordinates (wght, opsz, ital); nil for static fonts
}

// UIFont is a resolved UI typeface: Faces fill Slots (index for index),
// Ext are its unicode-range companions. A nil *UIFont is the embedded
// Lato.
type UIFont struct {
	Name  string // Slack's option value ("arial"), for logs and tests
	Faces [8]FaceSlot
	Ext   []FaceSlot
	// Tracking turns on Slack's --custom-font-tracking-* letter spacing
	// (tracking.go): San Francisco Pro and Segoe UI (System), Roboto Mono,
	// Noto Sans, Meiryo.
	Tracking bool
}

// varFace is a gio font.Face at a variation instance. Gio asks Face()
// for a fresh go-text Face per shaper; each gets the coordinates.
type varFace struct {
	ft   *gtfont.Font
	vars []gtfont.Variation
}

func (v varFace) Face() *gtfont.Face {
	f := gtfont.NewFace(v.ft)
	if len(v.vars) > 0 {
		f.SetVariations(v.vars)
	}
	return f
}

// ParseFace parses one face. Every call returns a new font: Gio's
// shaper keys faces by their *Font and drops a repeat, and two slots may
// share a file (Arial Bold serves Bold and Black).
func ParseFace(s FaceSlot) (font.FontFace, error) {
	lds, err := gtot.NewLoaders(bytes.NewReader(s.Data))
	if err != nil {
		return font.FontFace{}, err
	}
	if s.Index < 0 || s.Index >= len(lds) {
		return font.FontFace{}, fmt.Errorf("theme: font has %d faces, want #%d", len(lds), s.Index)
	}
	ft, err := gtfont.NewFont(lds[s.Index])
	if err != nil {
		return font.FontFace{}, err
	}
	fam := s.Family
	if fam == "" {
		fam = Family
	}
	return font.FontFace{
		Font: font.Font{Typeface: font.Typeface(fam), Weight: s.Weight, Style: s.Style},
		Face: varFace{ft: ft, vars: s.Vars},
	}, nil
}

// facesWith is FacesFor with u's faces in the UI slots (and extra
// families, e.g. the font menu's previews, after them).
func facesWith(u *UIFont, extra []FaceSlot) ([]font.FontFace, error) {
	if u == nil && len(extra) == 0 {
		return FacesFor(false), nil
	}
	var out []font.FontFace
	if u == nil {
		out = latoFaces()
	} else {
		var slots []FaceSlot
		slots = append(slots, u.Faces[:]...)
		slots = append(slots, u.Ext...)
		ff, err := parseAll(slots)
		if err != nil {
			return nil, err
		}
		out = ff
	}
	ex, err := parseAll(extra)
	if err != nil {
		return nil, err
	}
	out = append(out, ex...)
	out = append(out, MonoFaces()...)
	return append(out, font.FontFace{Font: font.Font{Typeface: EmojiSlotTypeface}, Face: parseFace(emojiSlotTTF)}), nil
}

// parseAll parses faces in parallel (a variable Noto Sans is 2 MB).
func parseAll(ss []FaceSlot) ([]font.FontFace, error) {
	out := make([]font.FontFace, len(ss))
	errs := make([]error, len(ss))
	var wg sync.WaitGroup
	for i := range ss {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			out[i], errs[i] = ParseFace(ss[i])
		}(i)
	}
	wg.Wait()
	return out, errors.Join(errs...)
}

// NewShaperWith builds a shaper whose UI family is u (nil: Lato), plus
// extra families. Safe from any goroutine; it is slow (font parsing), so
// the app calls it off the UI goroutine.
func NewShaperWith(u *UIFont, extra ...FaceSlot) (*text.Shaper, error) {
	coll, err := facesWith(u, extra)
	if err != nil {
		return nil, err
	}
	sh := newShaper(coll)
	setTracking(sh, u)
	return sh, nil
}

func newShaper(coll []font.FontFace) *text.Shaper {
	sh := text.NewShaper(text.WithCollection(coll))
	faces := make([]*gtfont.Face, len(coll))
	for i, f := range coll {
		faces[i] = f.Face.Face()
	}
	registerShaperFaces(sh, faces) // Gio indexes collection faces in order
	return sh
}

// defaultUI is the UI font NewShaperFor builds with (nil: Lato), so
// windows opened later (huddles) match the main one, as Slack's huddle
// window gets body.use-theme-font too.
var defaultUI atomic.Pointer[UIFont]

// SetDefaultUIFont makes u the font of shapers NewShaperFor builds from
// now on (nil: Lato).
func SetDefaultUIFont(u *UIFont) { defaultUI.Store(u) }

// SetUIShaper installs a shaper with a new UI font for both modes and
// bumps FontGen and Gen once, so every cache of laid-out text starts over
// in the same frame (UI goroutine).
func (t *Theme) SetUIShaper(sh *text.Shaper) {
	t.SetShapers(sh, sh)
	BumpFontGen()
	t.Gen++
}

// Extents are a UI face's hhea ascent and descent per em (Lato's are
// 1974/-426 per 2000: 0.987, 0.213).
type Extents struct{ Ascent, Descent float32 }

// LatoExtents are the embedded Lato's.
var LatoExtents = Extents{0.987, 0.213}

// UIExtents is the regular UI face's ascent and descent per em in sh
// (LatoExtents for shapers NewShaperFor / NewShaperWith didn't build).
// Chrome splits a line box's leading around these (the CSS strut).
func UIExtents(sh *text.Shaper) Extents {
	v, ok := shaperFaces.Load(sh)
	if !ok {
		return LatoExtents
	}
	e := v.(*emboldener)
	if p := e.ext.Load(); p != nil {
		return *p
	}
	x := LatoExtents
	if len(e.faces) > 0 && e.faces[0] != nil {
		if fe, ok := e.faces[0].FontHExtents(); ok {
			if upem := float32(e.faces[0].Upem()); upem > 0 && fe.Ascender > 0 {
				x = Extents{fe.Ascender / upem, -fe.Descender / upem}
			}
		}
	}
	e.ext.Store(&x)
	return x
}

// fontGen counts UI font installs (see SetUIFontGen).
var fontGen atomic.Uint32

// FontGen changes whenever the app installs a new UI font: caches of
// measured text that aren't keyed by shaper (row heights for the
// scrollbar, measured widths) start over.
func FontGen() uint32 { return fontGen.Load() }

// BumpFontGen marks a UI font switch (the app calls it once, as it
// installs the new shapers).
func BumpFontGen() { fontGen.Add(1) }
