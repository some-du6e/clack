package composer

import (
	"image"
	"image/png"
	"os"
	"path/filepath"
	"testing"

	"gioui.org/layout"
	"gioui.org/op"
	"gioui.org/op/clip"
	"gioui.org/op/paint"
	"gioui.org/unit"

	"github.com/some-du6e/clack/internal/ui/theme"
	"github.com/some-du6e/clack/internal/ui/theme/pixeltest"
)

// The thread composer's "Also send to #clack" (BroadcastControls, 63o2)
// draws the channel as Slack's InlineChannelEntity: the "channel" SvgIcon
// at 1em (12px) sitting 0.125em under the baseline, 2px, then the name in
// bold. alpha.59 drew the stroked 12px hash autocomplete uses, heavier
// and lower than the text, which read as "# clack" (Karim, 2026-11-13).
// The native checkbox's bottom is on the text's baseline and the label
// text sits 1px up (c-label--inline .c-label__text top -1px).
func TestBroadcastRowShots(t *testing.T) {
	for _, dark := range []bool{false, true} {
		th := theme.Bare(dark)
		th.Shaper = theme.NewShaper()
		var c chrome
		p := c.palette(th)
		w, h := 320, 26
		draw := func(f func(gtx layout.Context)) *image.RGBA {
			var ops op.Ops
			paint.FillShape(&ops, p.bg, clip.Rect{Max: image.Pt(w, h)}.Op())
			gtx := layout.Context{Ops: &ops, Metric: unit.Metric{PxPerDp: 1, PxPerSp: 1}, Constraints: layout.Exact(image.Pt(w, h))}
			f(gtx)
			return pixeltest.Render(t, w, h, &ops)
		}
		mode := "light"
		if dark {
			mode = "dark"
		}
		shot := func(name string, img *image.RGBA) {
			dir := os.Getenv("ICON_SHOTS_DIR") // the icon-shots workflow's artifact
			if dir == "" {
				return
			}
			if f, err := os.Create(filepath.Join(dir, name+"-"+mode+".png")); err == nil {
				png.Encode(f, img)
				f.Close()
			}
		}
		st := &chromeState{Mode: modeThread, ChannelName: "clack"}
		img := draw(func(gtx layout.Context) { c.layoutBroadcast(gtx, th, p, st, 0, w, 0) })
		shot("broadcast-channel", img)
		on := *st
		on.Broadcast = true
		shot("broadcast-channel-checked", draw(func(gtx layout.Context) { c.layoutBroadcast(gtx, th, p, &on, 0, w, 0) }))
		dm := chromeState{Mode: modeThread, IsDM: true}
		shot("broadcast-dm", draw(func(gtx layout.Context) { c.layoutBroadcast(gtx, th, p, &dm, 0, w, 0) }))
		priv := *st
		priv.Private = true
		shot("broadcast-private", draw(func(gtx layout.Context) { c.layoutBroadcast(gtx, th, p, &priv, 0, w, 0) }))

		gtx := layout.Context{Ops: new(op.Ops), Metric: unit.Metric{PxPerDp: 1, PxPerSp: 1}}
		s := textStyle{Size: 12, LineHeight: 18}
		textX := gtx.Dp(13.5) + 13 + 12
		preW := crMeasure(gtx, th.Shaper, s, "Also send to")
		hashX := textX + preW + c.space(gtx, th.Shaper, 1)
		aW := crMeasure(gtx, th.Shaper, s, "A")
		capBox, capInk := inkBox(img, image.Rect(textX, 0, textX+aW, h), p.hint, p.bg)
		hashBox, hashInk := inkBox(img, image.Rect(hashX, 0, hashX+12, h), p.hint, p.bg)
		if capInk == 0 || hashInk == 0 {
			t.Fatalf("%s: nothing drawn (A ink %d, # ink %d)", mode, capInk, hashInk)
		}
		// Slack's channel glyph spans its 12px box from 2.75 to 17.25 of
		// 20 units: as tall as Lato's capitals, give or take a pixel, and
		// ending about 1.5px under their baseline
		if hashBox.Dy() > capBox.Dy()+2 || hashBox.Min.Y < capBox.Min.Y-1 || hashBox.Max.Y > capBox.Max.Y+2 {
			t.Errorf("%s: # ink %v vs the A's %v", mode, hashBox, capBox)
		}
		old := draw(func(gtx layout.Context) {
			defer op.Offset(image.Pt(2, 6)).Push(gtx.Ops).Pop()
			drawHashIcon(gtx, 12, p.hint)
		})
		oldBox, oldInk := inkBox(old, image.Rect(0, 0, w, h), p.hint, p.bg)
		t.Logf("%s: # ink %d in %v; the stroked hash alpha.59 drew %d in %v", mode, hashInk, hashBox, oldInk, oldBox)
		// the name starts 2px after the 12px icon, in bold
		nameX := hashX + 12 + 2
		if _, ink := inkBox(img, image.Rect(hashX+12, 0, nameX, h), p.hint, p.bg); ink > capInk/8 {
			t.Errorf("%s: ink %d in the 2px gap after the icon", mode, ink)
		}
		boldW := crMeasure(gtx, th.Shaper, textStyle{Size: 12, LineHeight: 18, Weight: theme.Bold}, "clack")
		nameBox, _ := inkBox(img, image.Rect(nameX, 0, w, h), p.hint, p.bg)
		if nameBox.Min.X > nameX+1 || nameBox.Max.X < nameX+boldW-2 || nameBox.Max.X > nameX+boldW+1 {
			t.Errorf("%s: the name's ink %v, want it over [%d,%d) (bold clack)", mode, nameBox, nameX, nameX+boldW)
		}

		// the checkbox (unchecked: a 1px border) ends on the baseline;
		// the text is drawn 1px up from the label's top
		cbBox, _ := inkBox(img, image.Rect(gtx.Dp(13.5), 0, gtx.Dp(13.5)+13, h), p.checkBorder, p.bg)
		_, ld := crRec(gtx, th.Shaper, 1<<12, s, "Also send to")
		base := 8 + ld.Size.Y - ld.Baseline
		if cbBox.Dy() != 13 || cbBox.Max.Y != base {
			t.Errorf("%s: checkbox ink %v, want 13px ending on the baseline at y=%d", mode, cbBox, base)
		}
		if capBox.Max.Y > base || capBox.Max.Y < base-2 {
			t.Errorf("%s: the A's ink ends at %d, want on the baseline 1px up (%d)", mode, capBox.Max.Y, base-1)
		}
	}
}
