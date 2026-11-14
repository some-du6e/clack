package richview

import (
	"image"
	"image/color"

	"gioui.org/f32"
	"gioui.org/layout"
	"gioui.org/op"
	"gioui.org/op/clip"
	"gioui.org/op/paint"
	"gioui.org/unit"

	"github.com/some-du6e/clack/internal/richtext"
	"github.com/some-du6e/clack/internal/ui/icons"
)

// The file cards' FileSectionDropdownButton (custom_file_sections; CDN
// build fa938a086569, 2026-11-13):
//
//   - A canvas in a message (gaA3 FileActions, the pillow's
//     c-file__actions c-message_actions__group: top 12px, right 12px,
//     shown while the pillow is hovered) has renderFavoriteCanvas, which
//     with the experiment on is renderFavoriteButton: the button (size
//     small, 28px, c-message_actions__button, tooltip "Move file" on top,
//     source "file_pillow") instead of the star.
//   - A list in a message (N1hO ListEntityActionsBar, the entity's
//     actionsBar__Nlq8R: top 8px, right 8px, on hover) has the same
//     button (size smedium, 32px) in place of its star.
//
// The bars' other buttons (Share, file references, More actions) aren't
// drawn by Clack yet: the bar holds the button alone. The button's face
// is the shell's (FileSectionFace: views.SectionButtonFor) and a click
// opens FileSectionMoveToMenu (FileSectionOpener).

// FileSectionFace is the button's face for a file: Starred's filled star
// on sunflower, the section's emoji, the section icon, or the star; ok
// false without custom_file_sections (renderFavoriteButton returns null).
// nil: never drawn.
var FileSectionFace func(fileID string) (look SectionLook, emojiURL string, ok bool)

// SectionLook is FileSectionDropdownButton's face.
type SectionLook int

const (
	SectionLookStar SectionLook = iota
	SectionLookStarred
	SectionLookEmoji
	SectionLookIcon
)

// FileSectionOpener is optional on a Handler: the button's click, with
// the button's box in window coordinates (the menu opens under it).
type FileSectionOpener interface {
	OpenFileSection(f *richtext.File, btn image.Rectangle)
}

// fileSec is a card with a section button: its box, the bar's inset and
// the button's size (Dp).
type fileSec struct {
	card       image.Rectangle
	f          *richtext.File
	inset, btn int
}

// addFileSec records card r for file f's bar (not in a hover card or a
// canvas, which have no FileActions).
func (l *lay) addFileSec(f *richtext.File, r image.Rectangle, inset, btn int) {
	if f == nil || f.ID == "" || l.opts.NoHoverCards || l.opts.Canvas {
		return
	}
	l.d.fileSecs = append(l.d.fileSecs, fileSec{card: r, f: f, inset: inset, btn: btn})
}

// secBar is the bar (c-message_actions__group: padding 4, radius 12) and
// its button for card s.
func (v *View) secBar(s *fileSec) (bar, btn image.Rectangle) {
	in, b, pad := v.px(s.inset), v.px(s.btn), v.px(4)
	bar = image.Rect(s.card.Max.X-in-b-2*pad, s.card.Min.Y+in, s.card.Max.X-in, s.card.Min.Y+in+b+2*pad)
	return bar, bar.Inset(pad)
}

// fileSecAt is the card whose (shown) section button is under p, or -1.
func (v *View) fileSecAt(p f32.Point) int {
	if FileSectionFace == nil {
		return -1
	}
	pt := image.Pt(int(p.X), int(p.Y))
	for i := range v.d.fileSecs {
		s := &v.d.fileSecs[i]
		if _, btn := v.secBar(s); pt.In(btn) {
			if _, _, ok := FileSectionFace(s.f.ID); ok {
				return i
			}
		}
	}
	return -1
}

// The starred face (p-view_header_star_action_button_filled):
// plt-sunflower-0 / -5 (hover) under plt-sunflower-70, both modes.
var (
	secSun0  = color.NRGBA{R: 255, G: 250, B: 224, A: 255}
	secSun5  = color.NRGBA{R: 255, G: 244, B: 184, A: 255}
	secSun70 = color.NRGBA{R: 170, G: 128, B: 0, A: 255}
)

// drawFileSecs draws the hovered card's bar and button, and its tooltip
// while the pointer rests on the button.
func (v *View) drawFileSecs(gtx layout.Context, st *Style) {
	if !v.hovering || FileSectionFace == nil {
		return
	}
	pt := image.Pt(int(v.ptr.X), int(v.ptr.Y))
	p := &st.Palette
	for i := range v.d.fileSecs {
		s := &v.d.fileSecs[i]
		if !pt.In(s.card) {
			continue
		}
		look, emojiURL, ok := FileSectionFace(s.f.ID)
		if !ok {
			continue
		}
		bar, btn := v.secBar(s)
		// the group: primary background, a 1px foreground_low .13 ring
		// and 0 1px 3px #00000014
		bg, ring := rgb(255, 255, 255), rgba(29, 28, 29, 0.13)
		if p.Dark {
			bg, ring = rgb(26, 29, 33), rgba(232, 232, 232, 0.13)
		}
		rad := v.px(12)
		paint.FillShape(gtx.Ops, rgba(0, 0, 0, 0x14/255.0), clip.UniformRRect(bar.Add(image.Pt(0, v.px(1))).Inset(-v.px(1)), rad).Op(gtx.Ops))
		paint.FillShape(gtx.Ops, ring, clip.UniformRRect(bar.Inset(-max(1, v.px(1))), rad+v.px(1)).Op(gtx.Ops))
		paint.FillShape(gtx.Ops, bg, clip.UniformRRect(bar, rad).Op(gtx.Ops))
		hot := pt.In(btn)
		switch look {
		case SectionLookStarred:
			c := secSun0
			if hot {
				c = secSun5
			}
			r := btn.Add(image.Pt(0, v.px(1)))
			paint.FillShape(gtx.Ops, c, clip.UniformRRect(r, v.px(8)).Op(gtx.Ops))
			ip := v.px(20)
			ir := image.Rectangle{Min: r.Min.Add(image.Pt((r.Dx()-ip)/2, (r.Dy()-ip)/2))}
			ir.Max = ir.Min.Add(image.Pt(ip, ip))
			v.secIcon(gtx, "star-filled", ir, secSun70)
		default:
			// c-message_actions__button: content-sec; :hover foreground_min
			// .04 and primary_foreground
			col := p.Text3
			if hot {
				hov := rgba(29, 28, 29, 0.04)
				if p.Dark {
					hov = rgba(232, 232, 232, 0.04)
				}
				paint.FillShape(gtx.Ops, hov, clip.UniformRRect(btn, v.px(8)).Op(gtx.Ops))
				col = p.Text
			}
			ip := v.px(s.btn/2 + 2) // calc(.5 * size + 2px)
			ir := image.Rectangle{Min: btn.Min.Add(image.Pt((btn.Dx()-ip)/2, (btn.Dy()-ip)/2))}
			ir.Max = ir.Min.Add(image.Pt(ip, ip))
			switch {
			case look == SectionLookEmoji && emojiURL != "" && st.Images != nil:
				if img, ok := st.Images.Get(emojiURL, ip*2); ok {
					drawImage(gtx.Ops, img, ir, [4]int{}, false)
				}
			case look == SectionLookIcon:
				v.secIcon(gtx, "section", ir, col)
			default:
				v.secIcon(gtx, "star", ir, col)
			}
		}
		if hot {
			v.copyTip(gtx, st, btn, "Move file")
		}
	}
}

func (v *View) secIcon(gtx layout.Context, name string, r image.Rectangle, col color.NRGBA) {
	t := op.Offset(r.Min).Push(gtx.Ops)
	g := gtx
	g.Constraints = layout.Exact(r.Size())
	g.Metric = unit.Metric{PxPerDp: 1, PxPerSp: 1}
	icons.Get(name)(g, unit.Dp(r.Dx()), col)
	t.Pop()
}
