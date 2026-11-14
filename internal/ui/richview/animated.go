package richview

// Animated GIFs in messages (imgcache plays them while they're drawn:
// imgcache/anim.go), and Slack's controls for them with Accessibility >
// "Allow animations" off (CDN build 2c0d02ca130f):
//
//   - An animated image draws every frame, between the recording's two
//     halves (View.record), so it plays where a recorded one would freeze
//     and keeps its place in the stack; the rest stays recorded.
//   - Animations off (uipref NoAnimations: imgcache draws stills), a file
//     draws Slack's deanimated still (richtext File.Still: deanimate,
//     deanimate_gif, a static thumb), and attachments' and image blocks'
//     images their first frame, the pixels Slack's image proxy returns for
//     o1=gu.
//   - Each is then Slack's PlayPauseAnimations (CfuZ): a round Play button
//     over the still, and a click anywhere on it plays the animated
//     source (the button hidden) until the next click. Slack wraps a file
//     in it when the file has deanimate (FileImageThumbnail CyQH:
//     suppressAnimations && file.deanimate), an attachment's or image
//     block's image when its proxy answers x-slack-animated
//     (AttachmentImage NANx's HEAD): Clack, when the decoded image has
//     more than one frame.
//   - With the image's click taken by that, its viewer opens from an
//     Expand button shown while the image is hovered: FileActions'
//     renderExpand (gaA3: a GIF or PNG with deanimate; zoom-in,
//     "Expand"), the gallery image's c-file__actions--image
//     (gallery_v3: top 8, right 8, the group without background or
//     padding), a plain MessageActionButton (smedium 32px, the 18px
//     glyph); AttachmentImageActions' Expand (NANx: search, "Expand",
//     only while its PlayPauseAnimations is on), a 32px icon button in its
//     c-message_actions__group at top 10, right 10. The bars' other
//     buttons (Download, Share, Open in new window, More actions) aren't
//     drawn by Clack yet: the bar holds the button alone.

import (
	"image"
	"strings"

	"gioui.org/f32"
	"gioui.org/layout"
	"gioui.org/op"
	"gioui.org/op/clip"
	"gioui.org/op/paint"

	"github.com/some-du6e/clack/internal/i18n"
	"github.com/some-du6e/clack/internal/richtext"
)

// playImages is implemented by imgcache.Cache.
type playImages interface {
	IsAnimation(url string, px int) bool
	GetPlaying(url string, px int) (paint.ImageOp, bool)
}

func isAnimation(imgs Images, url string, px int) bool {
	a, ok := imgs.(playImages)
	return ok && a.IsAnimation(url, px)
}

// playCtl is one PlayPauseAnimations: image img, drawn as its still until
// the viewer plays it.
type playCtl struct {
	img   int    // the imgs index
	key   string // its key in View.playing: the animated source's URL
	known bool   // a file's deanimate says it animates; else the decoded image does
}

// expandBtn is an Expand button, shown while card is hovered.
type expandBtn struct {
	card  image.Rectangle // the image's box
	inset int             // Dp from its top and right
	group bool            // in a c-message_actions__group (attachments); else bare (gallery_v3)
	file  *richtext.File  // opens the file's viewer, or
	img   *richtext.Image // the image's
	play  int             // the playCtl it shows with (only while that's on), -1: always
}

// playable makes image i (just added) a PlayPauseAnimations: drawn
// playing when the viewer played key, its still otherwise.
func (l *lay) playable(i int, key string, known bool) int {
	l.d.imgs[i].play = l.playing[key]
	l.d.plays = append(l.d.plays, playCtl{img: i, key: key, known: known})
	return len(l.d.plays) - 1
}

// fileStill is the source of file f's gallery image: Slack's
// FileImageThumbnail (CyQH) draws the deanimated still with animations
// off, the animated thumb once the viewer played it.
func (l *lay) fileStill(f *richtext.File) *richtext.Image {
	if !l.noAnim || f.IsVideo || f.Thumb == nil || f.Still == nil || f.Deanimate && l.playing[f.Thumb.URL] {
		return f.Thumb
	}
	return f.Still
}

// fileControls adds a gallery tile's PlayPauseAnimations and Expand
// (animations off) for file f, its image at imgs index i in tile r.
func (l *lay) fileControls(f *richtext.File, i int, r image.Rectangle) {
	if !l.noAnim || f.IsVideo || f.Thumb == nil || !f.Deanimate {
		return
	}
	l.playable(i, f.Thumb.URL, true)
	if t := strings.ToLower(f.Type); (t == "gif" || t == "png") && !l.opts.NoHoverCards && !l.opts.Canvas {
		l.d.expands = append(l.d.expands, expandBtn{card: r, inset: 8, file: f, play: -1})
	}
}

// imageControls adds an attachment's or image block's PlayPauseAnimations
// and its Expand (animations off), the image at imgs index i in r.
func (l *lay) imageControls(img *richtext.Image, i int, r image.Rectangle) {
	if !l.noAnim || img == nil || img.URL == "" {
		return
	}
	p := l.playable(i, img.URL, false)
	if !l.opts.NoHoverCards && !l.opts.Canvas {
		l.d.expands = append(l.d.expands, expandBtn{card: r, inset: 10, group: true, img: img, play: p})
	}
}

// liveImg reports whether im draws every frame instead of from the
// recording: an animated image, or a still the viewer played.
func (v *View) liveImg(st *Style, im *imgPrim) bool {
	if im.url == "" || strings.HasPrefix(im.url, iconURL) {
		return false
	}
	return im.play || animated(st.Images, im.url, im.px)
}

// getImg is image im at px: through GetPlaying when the viewer played it.
func getImg(st *Style, im *imgPrim, px int) (paint.ImageOp, bool) {
	if st.Images == nil {
		return paint.ImageOp{}, false
	}
	if im.play {
		if p, ok := st.Images.(playImages); ok {
			return p.GetPlaying(im.url, px)
		}
	}
	return st.Images.Get(im.url, px)
}

// drawImg draws image i (static: into the recording, which tracks what's
// still loading; else the frame due now of a live one).
func (v *View) drawImg(ops *op.Ops, st *Style, i int, static bool) {
	im := &v.d.imgs[i]
	if im.url == "" {
		fillRadii(ops, im.r, im.radii, st.Palette.Placeholder)
		return
	}
	if name, ok := strings.CutPrefix(im.url, iconURL); ok { // a vector icon (unfurl footers, placeholders)
		paintIconAt(ops, name, im.r, im.tint)
		return
	}
	img, ok := getImg(st, im, im.px)
	if ok && !im.fit {
		if px := coverPx(img.Size(), im.r.Size(), im.px); px > im.px {
			// cover: the copy scaled to px by its larger side is short of
			// the box on its smaller side and would be stretched (a
			// 1200x630 og:image in a 105x104 card thumb drawn twice too
			// tall). The copy that covers the box's device pixels: scaled
			// down only, drawn 1:1. Until it decodes, the short one, and
			// the recording waits.
			if big, bok := getImg(st, im, px); bok {
				img = big
			} else if static {
				v.pending = append(v.pending, int32(i))
			}
			im.px = px
		}
	}
	if !ok {
		fillRadii(ops, im.r, im.radii, st.Palette.Placeholder)
		if static {
			v.pending = append(v.pending, int32(i))
		}
		return
	}
	if im.fit {
		fillRadii(ops, im.r, im.radii, st.Palette.Placeholder)
	}
	if im.alpha > 0 && im.alpha < 1 {
		o := paint.PushOpacity(ops, im.alpha)
		drawImage(ops, img, im.r, im.radii, !im.fit)
		o.Pop()
		return
	}
	drawImage(ops, img, im.r, im.radii, !im.fit)
}

// playOn reports whether c is a PlayPauseAnimations now (a file's always;
// an attachment's once its image decoded as an animation).
func (v *View) playOn(st *Style, c *playCtl) bool {
	if c.known {
		return true
	}
	im := &v.d.imgs[c.img]
	return im.play || isAnimation(st.Images, im.url, im.px)
}

// playAt is the PlayPauseAnimations under p, or -1.
func (v *View) playAt(st *Style, p f32.Point) int {
	pt := image.Pt(int(p.X), int(p.Y))
	for i := range v.d.plays {
		if c := &v.d.plays[i]; pt.In(v.d.imgs[c.img].r) && v.playOn(st, c) {
			return i
		}
	}
	return -1
}

// togglePlay plays or stops PlayPauseAnimations i (its useState): the
// layout draws the other source.
func (v *View) togglePlay(i int) {
	if i < 0 || i >= len(v.d.plays) {
		return
	}
	k := v.d.plays[i].key
	if v.playing[k] {
		delete(v.playing, k)
	} else {
		v.playing[k] = true
	}
	v.dirty, v.redraw = true, true
}

// drawPlays draws the Play buttons of the stills not playing
// (.p-play_pause_animations__btn: primary background and foreground, a
// 32px play-filled glyph in 20px of padding, 72px round, centred on the
// image and clipped to it by the container's overflow: hidden; hidden
// while it plays).
func (v *View) drawPlays(gtx layout.Context, st *Style) {
	p := &st.Palette
	for i := range v.d.plays {
		c := &v.d.plays[i]
		im := &v.d.imgs[c.img]
		if im.play || !v.playOn(st, c) || im.r.Empty() {
			continue
		}
		mid := im.r.Min.Add(im.r.Max).Div(2)
		s := v.px(72)
		b := image.Rect(mid.X-s/2, mid.Y-s/2, mid.X-s/2+s, mid.Y-s/2+s)
		cl := clip.RRect{Rect: im.r, NW: im.radii[0], NE: im.radii[1], SE: im.radii[2], SW: im.radii[3]}.Push(gtx.Ops)
		paint.FillShape(gtx.Ops, p.Panel, clip.Ellipse(b).Op(gtx.Ops))
		// play-filled in its 32px glyph box: the triangle of the video
		// pill's 16px one, doubled (cards.go videoPill)
		g := b.Min.Add(image.Pt(v.px(20), v.px(20)))
		tri := box{kind: bPlay, r: image.Rect(g.X+v.px(8), g.Y+v.px(4), g.X+v.px(28), g.Y+v.px(28)), fill: p.Text}
		drawBox(gtx.Ops, &tri)
		cl.Pop()
	}
}

// expandOn reports whether Expand button i shows: its image hovered (and
// for an attachment, its PlayPauseAnimations on).
func (v *View) expandOn(st *Style, i int) bool {
	e := &v.d.expands[i]
	if !v.hovering || !image.Pt(int(v.ptr.X), int(v.ptr.Y)).In(e.card) {
		return false
	}
	return e.play < 0 || v.playOn(st, &v.d.plays[e.play])
}

// expandBar is button i's group and the button in it.
func (v *View) expandBar(i int) (bar, btn image.Rectangle) {
	e := &v.d.expands[i]
	in, s := v.px(e.inset), v.px(32)
	pad := 0
	if e.group {
		pad = v.px(4)
	}
	bar = image.Rect(e.card.Max.X-in-s-2*pad, e.card.Min.Y+in, e.card.Max.X-in, e.card.Min.Y+in+s+2*pad)
	return bar, bar.Inset(pad)
}

// expandAt is the shown Expand button under p, or -1.
func (v *View) expandAt(st *Style, p f32.Point) int {
	pt := image.Pt(int(p.X), int(p.Y))
	for i := range v.d.expands {
		if _, btn := v.expandBar(i); pt.In(btn) && v.expandOn(st, i) {
			return i
		}
	}
	return -1
}

// expand opens button i's viewer (FileActions' onExpand: openViewer;
// AttachmentImageActions' onExpand: the image's media viewer).
func (v *View) expand(i int, h Handler) {
	if h == nil || i < 0 || i >= len(v.d.expands) {
		return
	}
	switch e := &v.d.expands[i]; {
	case e.file != nil:
		h.OpenFile(e.file)
	case e.img != nil:
		h.OpenImage(*e.img)
	}
}

// drawExpands draws the hovered image's Expand button and, while the
// pointer rests on it, its tooltip.
func (v *View) drawExpands(gtx layout.Context, st *Style) {
	if !v.hovering || len(v.d.expands) == 0 {
		return
	}
	p := &st.Palette
	pt := image.Pt(int(v.ptr.X), int(v.ptr.Y))
	for i := range v.d.expands {
		if !v.expandOn(st, i) {
			continue
		}
		e := &v.d.expands[i]
		bar, btn := v.expandBar(i)
		if e.group { // c-message_actions__group: primary background, a 1px foreground_low .13 ring, 0 1px 3px #00000014
			bg, ring := rgb(255, 255, 255), rgba(29, 28, 29, 0.13)
			if p.Dark {
				bg, ring = rgb(26, 29, 33), rgba(232, 232, 232, 0.13)
			}
			rad := v.px(12)
			paint.FillShape(gtx.Ops, rgba(0, 0, 0, 0x14/255.0), clip.UniformRRect(bar.Add(image.Pt(0, v.px(1))).Inset(-v.px(1)), rad).Op(gtx.Ops))
			paint.FillShape(gtx.Ops, ring, clip.UniformRRect(bar.Inset(-max(1, v.px(1))), rad+v.px(1)).Op(gtx.Ops))
			paint.FillShape(gtx.Ops, bg, clip.UniformRRect(bar, rad).Op(gtx.Ops))
		}
		// c-message_actions__button: content-sec at .75; :hover
		// foreground_min .04 and primary_foreground
		hot := pt.In(btn)
		col := p.Text3
		if hot {
			hov := rgba(29, 28, 29, 0.04)
			if p.Dark {
				hov = rgba(232, 232, 232, 0.04)
			}
			paint.FillShape(gtx.Ops, hov, clip.UniformRRect(btn, v.px(8)).Op(gtx.Ops))
			col = p.Text
		}
		name, tip := "zoom-in", i18n.T("file", "Expand")
		if e.img != nil {
			name, tip = "search", i18n.T("attachment_media", "Expand")
		}
		ip := v.px(18)
		ir := image.Rectangle{Min: btn.Min.Add(image.Pt((btn.Dx()-ip)/2, (btn.Dy()-ip)/2))}
		ir.Max = ir.Min.Add(image.Pt(ip, ip))
		v.secIcon(gtx, name, ir, col)
		if hot {
			v.copyTip(gtx, st, btn, tip)
		}
	}
}
