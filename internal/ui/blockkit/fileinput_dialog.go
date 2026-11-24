package blockkit

// FileInputDialog (n9p0), file_input's Add file modal: stacked over the
// view (openModal "workflow-file-input-dialog", isStackable, z-index
// 1012), Kit's Modal at maxWidth 720 titled "Add file", its ModalContent
// (no padding) the drop target, from client-boot-styles:
//
//   - .dropTarget: a 1px dashed plt-gray-20 border, radius 12, 356px
//     tall, margin 0 28px 28px, its column centred both ways:
//   - .animContainer: a 188px circle holding three cards cut from
//     dragdrop_sprites@2x (background-size 100x358, in this order: card3
//     at 0 -189, 92x77, top 20 left 55; card2 at 0 -273, 92x77, top 55
//     left 35; card1 at 0 0, 100x89, top 70 left 55), and the sad cloud
//     (empty-state-low-connection, 112 wide, centred) in their place
//     after an error (.dropTargetError);
//   - .dropPrompt "Drop to upload": no height, shown only during a drag;
//   - .uploadPrompt, a column 16px apart: "Paste or drag a file here, or"
//     (15px/1.46668) and the primary medium Upload File button (the 20px
//     file-upload 4px before its label), which opens the chooser (accept
//     per iSZO, several files when max_files > 1). After an error, the
//     error's text and an outline "Try Another File" (back to the
//     prompt), which takes the focus.
//
// The chosen files go through fileChecks: an error stays in the dialog;
// files that pass close it and upload (uploadFiles).
//
// Left out: dropping files onto the target or pasting them, and the
// drag's look (.dropTargetActive): part 10.

import (
	"bytes"
	"image"
	"image/color"
	"image/draw"
	"image/png"
	"math"
	"sync"

	"gioui.org/f32"
	"gioui.org/io/key"
	"gioui.org/layout"
	"gioui.org/op"
	"gioui.org/op/clip"
	"gioui.org/op/paint"
	"gioui.org/unit"

	"github.com/some-du6e/clack/internal/core"
	"github.com/some-du6e/clack/internal/slackasset"
	"github.com/some-du6e/clack/internal/ui/popups"
	"github.com/some-du6e/clack/internal/ui/theme"
)

// fiDialog is an open Add file dialog.
type fiDialog struct {
	w       *ew
	m       *popups.Modal
	err     string // the error the target shows (FileInputDialog's _)
	pick    tap    // Upload File
	again   tap    // Try Another File
	picking bool   // the chooser is open
	focus   bool   // an error came: Try Another File takes the focus
}

// fiDialogShown, when set (tests), sees each dialog as it opens.
var fiDialogShown func(*fiDialog)

// openFileDialog opens the Add file dialog over the view.
func (w *ew) openFileDialog() {
	h := w.o.host()
	d := &fiDialog{w: w}
	d.pick.keys, d.again.keys = true, true
	d.m = popups.NewModal(h, popups.ModalOpts{
		Title: blockKitNS.T("Add file"),
		Width: 720,
		Flush: true,
		Body:  d.layout,
		OnClose: func() {
			if w.fi != nil && w.fi.dlg == d {
				w.fi.dlg = nil
			}
		},
	})
	w.fileState().dlg = d
	if fiDialogShown != nil {
		fiDialogShown(d)
	}
	h.ShowOverlay(d.m)
}

// choose opens the chooser (the hidden input's click).
func (d *fiDialog) choose() {
	w := d.w
	h := w.o.host()
	if PickFiles == nil || d.picking {
		return
	}
	accept := fiAccept(w.e.FileTypes, w.uploadPref())
	multiple := w.maxFiles() > 1
	d.picking = true
	go func() {
		fs, err := PickFiles(accept, multiple)
		post(h, func() {
			d.picking = false
			if err == nil && len(fs) > 0 {
				d.chosen(fs)
			}
		})
	}()
}

// chosen takes the chooser's files (the input's change: O): an error
// stays in the dialog, else they upload and the dialog closes
// (closeNamed).
func (d *fiDialog) chosen(fs []core.UploadFile) {
	keep, msg := d.w.fileChecks(fs)
	if msg != "" {
		d.err, d.focus = msg, true
		d.w.o.changed()
		return
	}
	if len(keep) == 0 { // uploads are off
		return
	}
	d.w.uploadFiles(keep)
	d.m.Dismiss()
}

// layout draws the modal's content: the drop target and its margins.
func (d *fiDialog) layout(gtx C) D {
	h := d.w.o.host()
	th := d.w.th()
	if d.pick.clicked(gtx, h) && d.err == "" {
		d.choose()
	}
	if d.again.clicked(gtx, h) && d.err != "" {
		d.err = "" // W: back to the prompt
	}
	bw := gtx.Constraints.Max.X
	m := dp(gtx, 28)
	tr := image.Rect(m, 0, bw-m, dp(gtx, 356))
	b := max(1, dp(gtx, 1))
	fiDashed(gtx, tr, dp(gtx, 12), b, theme.PltColor("gray", "20", th.Dark))
	in := tr.Inset(b)
	anim := dp(gtx, 188)
	ph := d.promptHeight(gtx, th, in.Dx())
	y := in.Min.Y + (in.Dy()-anim-ph)/2
	d.drawAnim(gtx, image.Pt(in.Min.X+(in.Dx()-anim)/2, y))
	d.drawPrompt(gtx, th, image.Rect(in.Min.X, y+anim, in.Max.X, y+anim+ph))
	return D{Size: image.Pt(bw, tr.Max.Y+m)}
}

// promptText is the prompt's line, or the error.
func (d *fiDialog) promptText() string {
	if d.err != "" {
		return d.err
	}
	return blockKitNS.T("Paste or drag a file here, or")
}

// fiPromptText lays out the prompt's text (the modal's 15px/1.46668) in
// at most w.
func fiPromptText(gtx C, th *theme.Theme, w int, col color.NRGBA, txt string) D {
	gtx.Constraints = layout.Constraints{Max: image.Pt(max(w, 1), 1<<20)}
	return th.Layout(gtx, theme.TextStyle{Size: unit.Sp(15), Color: col, LineHeight: unit.Sp(22)}, txt)
}

// promptHeight is .uploadPrompt's: the text, 16px, the button.
func (d *fiDialog) promptHeight(gtx C, th *theme.Theme, w int) int {
	m := op.Record(gtx.Ops)
	td := fiPromptText(gtx, th, w, color.NRGBA{}, d.promptText())
	m.Stop()
	return td.Size.Y + dp(gtx, 16) + dp(gtx, fiBtnH)
}

// drawPrompt draws .uploadPrompt in r, each item centred.
func (d *fiDialog) drawPrompt(gtx C, th *theme.Theme, r image.Rectangle) {
	m := op.Record(gtx.Ops)
	td := fiPromptText(gtx, th, r.Dx(), fiTok(th, "content-pry"), d.promptText())
	call := m.Stop()
	at(gtx, image.Pt(r.Min.X+(r.Dx()-td.Size.X)/2, r.Min.Y), func(gtx C) D { call.Add(gtx.Ops); return td })
	y := r.Min.Y + td.Size.Y + dp(gtx, 16)
	if d.err != "" {
		lbl := blockKitNS.T("Try Another File")
		sz := fiButtonSize(gtx, th, 0, lbl)
		at(gtx, image.Pt(r.Min.X+(r.Dx()-sz.X)/2, y), func(gtx C) D {
			fg := fiKitButton(gtx, th, sz, "outline", d.again.g.Hovered(), false)
			fiButtonContent(gtx, th, sz, "", 0, lbl, fg)
			d.again.add(gtx, sz)
			if d.focus {
				d.focus = false
				gtx.Execute(key.FocusCmd{Tag: &d.again})
			}
			return D{}
		})
		return
	}
	lbl := blockKitNS.T("Upload File")
	sz := fiButtonSize(gtx, th, 20, lbl)
	at(gtx, image.Pt(r.Min.X+(r.Dx()-sz.X)/2, y), func(gtx C) D {
		fg := fiKitButton(gtx, th, sz, "primary", d.pick.g.Hovered(), false)
		fiButtonContent(gtx, th, sz, "file-upload", 20, lbl, fg)
		d.pick.add(gtx, sz)
		return D{}
	})
}

// drawAnim draws .animContainer at p: the cards, or after an error the
// sad cloud.
func (d *fiDialog) drawAnim(gtx C, p image.Point) {
	at(gtx, p, func(gtx C) D {
		a := dp(gtx, 188)
		if d.err != "" {
			cw := dp(gtx, 112)
			ch := int(float32(cw)*471/589 + .5) // the SVG's 589x471
			r := image.Rect((a-cw)/2, (a-ch)/2, (a-cw)/2+cw, (a-ch)/2+ch)
			if im := d.w.o.host().Images(); im != nil {
				if img, ok := im.Get(slackasset.EmptyStateLowConnection, cw); ok {
					fiStretch(gtx, img, r, 1)
				}
			}
			return D{}
		}
		sp := fiSprites()
		if !sp.ok {
			return D{}
		}
		for _, c := range []struct {
			img        paint.ImageOp
			x, y, w, h float32
		}{{sp.card3, 55, 20, 92, 77}, {sp.card2, 35, 55, 92, 77}, {sp.card1, 55, 70, 100, 89}} {
			fiStretch(gtx, c.img, image.Rect(dp(gtx, c.x), dp(gtx, c.y), dp(gtx, c.x+c.w), dp(gtx, c.y+c.h)), 1)
		}
		return D{}
	})
}

// fiSpriteSet is the dialog's cards cut from dragdrop_sprites@2x (the
// background positions in its 100x358 background size, so twice that in
// the image's pixels).
type fiSpriteSet struct {
	ok                  bool
	card1, card2, card3 paint.ImageOp
}

var (
	fiSpritesOnce sync.Once
	fiSpritesSet  fiSpriteSet
)

func fiSprites() *fiSpriteSet {
	fiSpritesOnce.Do(func() {
		data, ok := slackasset.Get(slackasset.DragDropSprites)
		if !ok {
			return
		}
		img, err := png.Decode(bytes.NewReader(data))
		if err != nil {
			return
		}
		cut := func(x, y, w, h int) paint.ImageOp {
			r := image.Rect(x, y, x+w, y+h).Intersect(img.Bounds())
			dst := image.NewNRGBA(image.Rect(0, 0, r.Dx(), r.Dy()))
			draw.Draw(dst, dst.Bounds(), img, r.Min, draw.Src)
			return paint.NewImageOp(dst)
		}
		s := &fiSpritesSet
		s.card1 = cut(0, 0, 200, 178)   // 0 0, 100x89
		s.card3 = cut(0, 378, 184, 154) // 0 -189, 92x77
		s.card2 = cut(0, 546, 184, 154) // 0 -273, 92x77
		s.ok = true
	})
	return &fiSpritesSet
}

// fiDashed draws a w-wide dashed border on r's rounded outline (radius
// rad), the corners included: dashes and gaps 3w long, Chromium's for a
// thin dashed border. Gio's strokes have round caps, so each dash's path
// is w shorter.
func fiDashed(gtx C, r image.Rectangle, rad, w int, col color.NRGBA) {
	hw := float32(w) / 2
	x0, y0, x1, y1 := float32(r.Min.X)+hw, float32(r.Min.Y)+hw, float32(r.Max.X)-hw, float32(r.Max.Y)-hw
	rr := max(float32(rad)-hw, 0)
	var pts []f32.Point
	corner := func(cx, cy float32, a0 float64) {
		for i := 0; i <= 8; i++ {
			a := a0 + float64(i)*math.Pi/16
			pts = append(pts, f32.Pt(cx+rr*float32(math.Cos(a)), cy+rr*float32(math.Sin(a))))
		}
	}
	corner(x1-rr, y0+rr, -math.Pi/2) // the top edge into the top right corner, then clockwise
	corner(x1-rr, y1-rr, 0)
	corner(x0+rr, y1-rr, math.Pi/2)
	corner(x0+rr, y0+rr, math.Pi)
	pts = append(pts, pts[0])
	on, off := float32(2*w), float32(4*w)
	var p clip.Path
	p.Begin(gtx.Ops)
	dash, pos, moved := true, float32(0), false
	for i := 1; i < len(pts); i++ {
		a, b := pts[i-1], pts[i]
		seg := b.Sub(a)
		l := float32(math.Hypot(float64(seg.X), float64(seg.Y)))
		for t := float32(0); t < l; {
			want := off
			if dash {
				want = on
			}
			step := min(want-pos, l-t)
			if dash {
				if !moved {
					p.MoveTo(a.Add(seg.Mul(t / l)))
					moved = true
				}
				p.LineTo(a.Add(seg.Mul((t + step) / l)))
			}
			t += step
			pos += step
			if pos >= want-1e-3 {
				dash, pos, moved = !dash, 0, false
			}
		}
	}
	paint.FillShape(gtx.Ops, col, clip.Stroke{Path: p.End(), Width: float32(w)}.Op())
}
