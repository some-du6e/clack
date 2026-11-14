package icons

import (
	"image"
	"image/color"
	"sync"

	"gioui.org/layout"
	"gioui.org/op/clip"
	"gioui.org/op/paint"
)

// Illustrations Slack ships as image files (an <img> of a CDN SVG), drawn
// as vectors in the file's own colours: each part is one of the file's
// shapes as path data in its viewBox (rects with their rotate() applied,
// circles as two arcs), filled in its fill colour, in the file's order.
// Slack's files are not in the repo: the icon-shots workflow downloads
// them and TestArtMatchesSlack holds each colour's shapes to the file's.

type artPart struct {
	d    string
	fill uint32 // 0xRRGGBB
}

type artDef struct {
	view  float32 // the viewBox side
	parts []artPart
	once  sync.Once
	segs  [][]seg
}

var artRegistry = map[string]*artDef{}

func regArt(name string, view float32, parts ...artPart) {
	artRegistry[name] = &artDef{view: view, parts: parts}
}

// HasArt reports whether name is a registered illustration.
func HasArt(name string) bool { _, ok := artRegistry[name]; return ok }

// Art paints the named illustration px pixels square at the origin. An
// unknown name draws nothing (and is logged once, like Get's misses).
func Art(gtx layout.Context, name string, px int) layout.Dimensions {
	d, ok := artRegistry[name]
	if !ok {
		noteMissing(name, "nothing")
		return layout.Dimensions{Size: image.Pt(px, px)}
	}
	return d.draw(gtx, px, nil)
}

// draw fills the parts scaled to px; ink, when set, picks each part's
// colour from its fill and skips the parts it reports false for (the
// art test draws one colour at a time).
func (d *artDef) draw(gtx layout.Context, px int, ink func(fill uint32) (color.NRGBA, bool)) layout.Dimensions {
	d.once.Do(func() {
		d.segs = make([][]seg, len(d.parts))
		for i, p := range d.parts {
			d.segs[i] = parsePath(p.d)
		}
	})
	sc := float32(px) / d.view
	for i, p := range d.parts {
		c := hexRGB(p.fill)
		if ink != nil {
			var ok bool
			if c, ok = ink(p.fill); !ok {
				continue
			}
		}
		paint.FillShape(gtx.Ops, c, clip.Outline{Path: pathSpec(gtx, d.segs[i], sc)}.Op())
	}
	return layout.Dimensions{Size: image.Pt(px, px)}
}

func hexRGB(v uint32) color.NRGBA {
	return color.NRGBA{R: uint8(v >> 16), G: uint8(v >> 8), B: uint8(v), A: 0xff}
}

func init() {
	// slack_logo_mark-8890ba0.svg (the same file as the web app's
	// /img/slack_logo_mark.svg): Slack's four-colour mark, eight paths in
	// a 256 box. Drawn before every Slack source in the search source
	// pill (SearchFilterSourcesDisplay's appIconSmall, 15px) and as a
	// built-in slash command's icon (boot-shared olrj
	// SlashCommandsAutocompleteMenuEntityIcon: an appName "Slack" command,
	// which omi8 gives every core and client command, shows
	// /img/slack_logo_mark.svg at 20px on the row's own background).
	regArt("slack-logo-mark", 256,
		artPart{"M53.79 161.37C53.79 176.22 41.75 188.26 26.89 188.26C12.04 188.26 0 176.22 0 161.37C0 146.52 12.04 134.47 26.89 134.47L53.79 134.47L53.79 161.37Z", 0xe3066a},
		artPart{"M67.24 161.37C67.24 146.52 79.28 134.47 94.13 134.47C108.99 134.47 121.03 146.52 121.03 161.37L121.03 228.61C121.03 243.46 108.99 255.5 94.13 255.5C79.28 255.5 67.24 243.46 67.24 228.61L67.24 161.37Z", 0xe3066a},
		artPart{"M94.13 53.79C79.28 53.79 67.24 41.75 67.24 26.89C67.24 12.04 79.28 0 94.13 0C108.99 0 121.03 12.04 121.03 26.89L121.03 53.79L94.13 53.79Z", 0x00b3ff},
		artPart{"M94.13 67.24C108.99 67.24 121.03 79.28 121.03 94.13C121.03 108.99 108.99 121.03 94.13 121.03L26.89 121.03C12.04 121.03 0 108.99 0 94.13C0 79.28 12.04 67.24 26.89 67.24L94.13 67.24Z", 0x00b3ff},
		artPart{"M201.71 94.13C201.71 79.28 213.75 67.24 228.61 67.24C243.46 67.24 255.5 79.28 255.5 94.13C255.5 108.99 243.46 121.03 228.61 121.03L201.71 121.03L201.71 94.13Z", 0x41b658},
		artPart{"M188.26 94.13C188.26 108.99 176.22 121.03 161.37 121.03C146.52 121.03 134.47 108.99 134.47 94.13L134.47 26.89C134.47 12.04 146.52 0 161.37 0C176.22 0 188.26 12.04 188.26 26.89L188.26 94.13Z", 0x41b658},
		artPart{"M161.37 201.71C176.22 201.71 188.26 213.75 188.26 228.61C188.26 243.46 176.22 255.5 161.37 255.5C146.52 255.5 134.47 243.46 134.47 228.61L134.47 201.71L161.37 201.71Z", 0xfcc003},
		artPart{"M161.37 188.26C146.52 188.26 134.47 176.22 134.47 161.37C134.47 146.52 146.52 134.47 161.37 134.47L228.61 134.47C243.46 134.47 255.5 146.52 255.5 161.37C255.5 176.22 243.46 188.26 228.61 188.26L161.37 188.26Z", 0xfcc003},
	)
	// focus-d8ca34d.svg: the focus mode peek's telescope (boot-render
	// FocusModePopoverContent's statusBanner, 64px; the first open's
	// Lottie isn't ported, reduced motion shows this file too). A 1000
	// box: the two tubes, the eyepiece, the band, the mount and the legs.
	regArt("focus-telescope", 1000,
		artPart{"M622.78 348.01 L862.95 486.97 A19.14 19.14 0 0 1 869.93 513.12 L822.52 595.06 A19.14 19.14 0 0 1 796.37 602.04 L556.2 463.08 A19.14 19.14 0 0 1 549.23 436.94 L596.64 354.99 A19.14 19.14 0 0 1 622.78 348.01 Z", 0xabb7bf},
		artPart{"M287.88 107.24 L695.33 342.98 A19.14 19.14 0 0 1 702.31 369.13 L614.15 521.51 A19.14 19.14 0 0 1 588 528.49 L180.55 292.74 A19.14 19.14 0 0 1 173.57 266.6 L261.74 114.22 A19.14 19.14 0 0 1 287.88 107.24 Z", 0xabb7bf},
		artPart{"M261.737 114.216C267.03 105.069 278.736 101.944 287.883 107.237L412.106 179.111L304.774 364.616L180.551 292.742C171.404 287.45 168.279 275.744 173.572 266.596L261.737 114.216Z", 0x292f33},
		artPart{"M691.52 387.79 L742.87 417.49 L676.29 532.56 L624.94 502.85 Z", 0x292f33},
		artPart{"M434.91 537.79 A56.45 56.45 0 1 1 547.81 537.79 A56.45 56.45 0 1 1 434.91 537.79 Z", 0xabb7bf},
		artPart{"M525.33 642.54 V852.1 A33.97 33.97 0 0 1 457.39 852.1 V642.54 A33.97 33.97 0 0 1 525.33 642.54 Z", 0xabb7bf},
		artPart{"M457.82 662.98 L339.7 867.56 A33.97 33.97 0 0 1 280.87 833.59 L398.99 629.02 A33.97 33.97 0 0 1 457.82 662.98 Z", 0xabb7bf},
		artPart{"M581.86 625.76 L701.85 833.59 A33.97 33.97 0 0 1 643.02 867.56 L523.03 659.73 A33.97 33.97 0 0 1 581.86 625.76 Z", 0xabb7bf},
		artPart{"M400.47 594.24 H582.26 A33.97 33.97 0 0 1 582.26 662.18 H400.47 A33.97 33.97 0 0 1 400.47 594.24 Z", 0x292f33},
	)
}
