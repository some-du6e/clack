package richview

import (
	"image"
	"strings"

	"gioui.org/f32"

	"github.com/some-du6e/yoloslackgoclient/internal/richtext"
)

// HitKind is what a point of the view is over (context menus).
type HitKind uint8

const (
	HitNone      HitKind = iota // plain text or empty space
	HitLink                     // a link (URL)
	HitUser                     // a user mention (ID)
	HitChannel                  // a channel link (ID)
	HitUsergroup                // a user group mention (ID)
	HitImage                    // an image block, attachment image or thumb (Image)
	HitFile                     // a file card or gallery image (File)
	HitButton                   // a Block Kit or attachment button
	HitCard                     // a whole-card link (URL)
	HitOther                    // another clickable (Show more)
)

// Hit describes the point HitAt was asked about.
type Hit struct {
	Kind  HitKind
	URL   string
	ID    string
	Image *richtext.Image
	File  *richtext.File
	// Emoji is the :name: of the emoji image under the point, EmojiURL its
	// image ("" when the point isn't on an emoji image).
	Emoji, EmojiURL string
	// Text is true when the point is on a glyph of text.
	Text bool
}

// HitAt reports what is at p (View coordinates, after the first Layout).
func (v *View) HitAt(p f32.Point) Hit {
	var h Hit
	pt := image.Pt(int(p.X), int(p.Y))
	for i := range v.d.frags {
		f := &v.d.frags[i]
		switch f.kind {
		case fText:
			if !h.Text && pt.In(fragRect(f)) {
				h.Text = true
			}
		case fImage:
			if h.EmojiURL == "" && f.url != "" && !strings.HasPrefix(f.url, iconURL) && p.X >= f.x && p.X < f.x+float32(f.size) && p.Y >= float32(f.y) && p.Y < float32(f.y+f.size) {
				h.EmojiURL = f.url
				if f.tip > 0 && int(f.tip) <= len(v.d.tips) {
					h.Emoji = v.d.tips[f.tip-1]
				}
			}
		}
	}
	t := v.targetAt(p)
	if t < 0 || int(t) >= len(v.d.targets) {
		return h
	}
	tg := &v.d.targets[t]
	switch tg.kind {
	case tLink:
		h.Kind, h.URL = HitLink, tg.url
	case tUser:
		h.Kind, h.ID = HitUser, tg.id
	case tChannel:
		h.Kind, h.ID = HitChannel, tg.id
	case tUsergroup:
		h.Kind, h.ID = HitUsergroup, tg.id
	case tImage:
		h.Kind, h.Image = HitImage, tg.img
		if tg.inCard {
			h.Kind = HitCard
		}
	case tFile:
		h.Kind, h.File = HitFile, tg.file
	case tButton:
		h.Kind = HitButton
	case tCard:
		h.Kind, h.URL = HitCard, tg.url
	default:
		h.Kind = HitOther
	}
	return h
}

// FileRect is a file's clickable box (a gallery tile, a file card).
type FileRect struct {
	File *richtext.File
	R    image.Rectangle
}

// FileRects lists the files' clickable boxes in View coordinates (after
// the first Layout), in layout order. A gallery's "+ N" tile is the box of
// the file under it.
func (v *View) FileRects() []FileRect {
	var out []FileRect
	for _, h := range v.d.hits {
		if h.t < 0 || int(h.t) >= len(v.d.targets) || h.r.Empty() {
			continue
		}
		if tg := &v.d.targets[h.t]; tg.kind == tFile && tg.file != nil {
			out = append(out, FileRect{File: tg.file, R: h.r})
		}
	}
	return out
}

// ImageRect is an image's clickable box: an image block or an attachment
// image that opens the image viewer.
type ImageRect struct {
	Image *richtext.Image
	R     image.Rectangle
}

// ImageRects lists the boxes that open the image viewer, in View
// coordinates (after the first Layout), in layout order.
func (v *View) ImageRects() []ImageRect {
	var out []ImageRect
	for _, h := range v.d.hits {
		if h.t < 0 || int(h.t) >= len(v.d.targets) || h.r.Empty() {
			continue
		}
		if tg := &v.d.targets[h.t]; tg.kind == tImage && tg.img != nil {
			out = append(out, ImageRect{Image: tg.img, R: h.r})
		}
	}
	return out
}

// LinkRect is a link's box: a URL, an @mention, a channel or a user
// group mention.
type LinkRect struct {
	Kind HitKind // HitLink, HitUser, HitChannel or HitUsergroup
	ID   string  // the member, channel or user group
	URL  string  // a link's URL
	R    image.Rectangle
}

// LinkRects lists the links' boxes in View coordinates (after the first
// Layout), in layout order: one per line box a link wraps over.
func (v *View) LinkRects() []LinkRect {
	var out []LinkRect
	for i := range v.d.frags {
		f := &v.d.frags[i]
		if f.target < 0 || int(f.target) >= len(v.d.targets) {
			continue
		}
		tg := &v.d.targets[f.target]
		var k HitKind
		switch tg.kind {
		case tLink:
			k = HitLink
		case tUser:
			k = HitUser
		case tChannel:
			k = HitChannel
		case tUsergroup:
			k = HitUsergroup
		default:
			continue
		}
		if r := fragRect(f); !r.Empty() {
			out = append(out, LinkRect{Kind: k, ID: tg.id, URL: tg.url, R: r})
		}
	}
	return out
}
