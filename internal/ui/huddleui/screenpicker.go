package huddleui

import (
	"errors"
	"image"
	"image/color"
	"sync"

	"gioui.org/io/pointer"
	"gioui.org/layout"
	"gioui.org/op"
	"gioui.org/op/clip"
	"gioui.org/op/paint"
	"gioui.org/widget"

	"github.com/some-du6e/clack/internal/huddle"
	"github.com/some-du6e/clack/internal/ui/popups"
	"github.com/some-du6e/clack/internal/ui/theme"
)

// ScreenPickerModal (boot-shared 0S5z) with its source tabs
// (SourceSegmentedButton, D7xu) and grid (ScreenPicker, cXsV), measured
// on client-boot-styles' rules:
//
//   - the modal: maxWidth 800, no title (its aria label is "Share your
//     screen"); the header (70px) holds the tabs, centred 16px down:
//     .container__gcSkQ (base-sec, radius 12), two .item__wdSUv 165px wide
//     (15px content-sec, padding 8px 12px, 1px clear border, radius 12;
//     selected: base-pry, otl-ter border, bold content-pry): "Share entire
//     screen" and "Window".
//   - the content is 400px tall (.modalContent__X6KqL), the grid centred
//     in it (.screenPickerWrapper__vGPGR) 16px from the top and bottom
//     (.screenPicker__kXcTy), 32px from the sides (.screensGrid__FPDWV),
//     with huddles_screen_picker_app_groups off (this account): legacy
//     spacing, rows 12px apart, centred, 24px between tiles, 8px under
//     each. Columns: getColumnCount — 2 for screens, 3 for windows, 2
//     whenever the picker is narrower than 800px.
//   - a tile (.screenPickerTile__F1YMT): 1px clear border, radius 4,
//     padding 4px 4px 8px; the thumbnail centred in a 140px row (205px
//     for two-column screens 800px wide), radius 8, 8px above the title;
//     the title 13px/18px centred on one line. Hover: base-hgl-1-hover;
//     selected: otl-hgl-1 border, surf-hgl-1, content-hgl-1 and a bold
//     title. Clicking the selected tile unselects it. Selecting outlines
//     the source (BIqb X), unselecting or switching tabs takes it off.
//   - the footer: "Share system audio" (a checkbox, 24px from the left)
//     under huddles_screenshare_share_audio where the OS can capture it,
//     Cancel and Share (disabled until a tile is picked).
//   - no screens at all closes it with the noSystemScreenShare alert
//     (onNoScreensDetected); the border comes off on close unless Share
//     closed it (shouldSkipBorderCleanup).

// pickerInk is the Slack Kit tokens the picker paints with.
type pickerInk struct {
	basePry, baseSec, baseSecHover, otlTer       color.NRGBA
	otlHgl, surfHgl, contentHgl, hglHover        color.NRGBA
	contentPry, contentSec, basePryHover, cardBg color.NRGBA
}

func hexA(v uint32) color.NRGBA {
	return color.NRGBA{R: uint8(v >> 24), G: uint8(v >> 16), B: uint8(v >> 8), A: uint8(v)}
}

// pickerInks are slack-kit-gantry-v2-styles' :root (light) and
// .sk-client-theme--dark values.
func pickerInks(dark bool) pickerInk {
	if dark {
		return pickerInk{basePry: hexA(0x1a1d21ff), baseSec: hexA(0x1a1d21ff), baseSecHover: hexA(0xf8f8f814), otlTer: hexA(0x797c814d),
			otlHgl: hexA(0x2ba5ceff), surfHgl: hexA(0x03669640), contentHgl: hexA(0x2ba5ceff), hglHover: hexA(0xd8eef614),
			contentPry: hexA(0xf8f8f8ff), contentSec: hexA(0xb9babdff), basePryHover: hexA(0xf8f8f814)}
	}
	return pickerInk{basePry: hexA(0xffffffff), baseSec: hexA(0xf8f8f8ff), baseSecHover: hexA(0x4544470f), otlTer: hexA(0x5e5d6021),
		otlHgl: hexA(0x1264a3ff), surfHgl: hexA(0x36c5f021), contentHgl: hexA(0x1264a3ff), hglHover: hexA(0x0071a40f),
		contentPry: hexA(0x1d1c1dff), contentSec: hexA(0x454447ff), basePryHover: hexA(0x4544470f)}
}

// pickTile is one source in the grid.
type pickTile struct {
	src   huddle.ShareSource
	img   paint.ImageOp
	have  bool
	click widget.Clickable
}

// screenPicker is one open ScreenPickerModal.
type screenPicker struct {
	c  *Controller
	s  *huddle.Session
	ph popups.Host
	m  *popups.Modal

	mu      sync.Mutex
	kind    huddle.ShareKind
	screens []*pickTile // nil until listed
	windows []*pickTile
	loaded  bool
	sel     string // the selected source's id
	audio   bool   // Share system audio
	shared  bool   // closed by Share (shouldSkipBorderCleanup)

	tabs  [2]widget.Clickable
	check widget.Clickable
	list  layout.List
	width int // the picker's width last frame (getColumnCount)
}

// shareAudioOffered is the checkbox's condition: huddles_screenshare_share_audio,
// a desktop app on macOS or Windows (0S5z: l && dY("4.49.75") && (cX() ||
// uF())), and a capture that can take system audio.
func (c *Controller) shareAudioOffered() bool {
	return c.on(ExpShareAudio) && (shareGOOS == "darwin" || shareGOOS == "windows") && huddle.ShareSystemAudio()
}

// openScreenPicker shows ScreenPickerModal over ph (its goroutine).
func (c *Controller) openScreenPicker(s *huddle.Session, ph popups.Host) {
	p := &screenPicker{c: c, s: s, ph: ph, kind: huddle.ShareScreen}
	p.list.Axis = layout.Vertical
	cancel := &popups.ModalButton{Label: nsHuddles.T("Cancel"), Kind: popups.BtnOutline, Do: func() bool { return true }}
	share := &popups.ModalButton{Label: nsHuddles.T("Share"), Kind: popups.BtnPrimary,
		Enabled: func() bool { return p.selected() != nil }, Do: p.share}
	o := popups.ModalOpts{
		Width:   800,
		HeadH:   70,
		Flush:   true,
		Header:  p.header,
		Body:    p.body,
		Buttons: []*popups.ModalButton{cancel, share},
		OnClose: func() {
			p.mu.Lock()
			shared := p.shared
			p.mu.Unlock()
			if !shared { // the grid's unmount cleanup
				huddle.HideShareBorder()
			}
			c.share.mu.Lock()
			c.share.picking = false
			if c.share.picker == p {
				c.share.picker = nil
			}
			c.share.mu.Unlock()
			c.changed()
		},
	}
	if c.shareAudioOffered() {
		o.FooterLeft = p.audioBox
	}
	p.m = popups.NewModal(ph, o)
	c.share.mu.Lock()
	c.share.picker = p
	c.share.mu.Unlock()
	ph.ShowOverlay(p.m)
	ph.Invalidate()
	go p.load()
}

// share is the Share button: toggleScreenShare with the picked source
// and the checkbox; the border stays (shouldSkipBorderCleanup).
func (p *screenPicker) share() bool {
	src := p.selected()
	if src == nil {
		return false
	}
	p.mu.Lock()
	p.shared = true
	audio := p.audio && p.c.shareAudioOffered()
	p.mu.Unlock()
	p.c.startShare(p.s, *src, audio)
	return true
}

// load lists the screens, then the windows (ScreenPicker's effect).
func (p *screenPicker) load() {
	screens, err := huddle.ListShareSources(huddle.ShareScreen)
	if errors.Is(err, huddle.ErrShareDenied) || err == nil && len(screens) == 0 || errors.Is(err, huddle.ErrShareUnsupported) {
		p.c.setShareAlert(alertNoScreenShare) // onNoScreensDetected
		p.close()
		return
	}
	p.mu.Lock()
	p.screens = tilesOf(screens)
	p.mu.Unlock()
	p.ph.Invalidate()
	windows, _ := huddle.ListShareSources(huddle.ShareWindow)
	p.mu.Lock()
	p.windows = tilesOf(windows)
	p.loaded = true
	p.mu.Unlock()
	p.ph.Invalidate()
}

func (p *screenPicker) close() {
	if h, ok := p.ph.(winHost); ok {
		h.post(func() { p.m.Dismiss() })
	} else {
		p.c.post(func() { p.m.Dismiss() })
	}
	p.ph.Invalidate()
}

func tilesOf(l []huddle.ShareSource) []*pickTile {
	out := make([]*pickTile, 0, len(l))
	for _, s := range l {
		out = append(out, &pickTile{src: s})
	}
	return out
}

func (p *screenPicker) selected() *huddle.ShareSource {
	p.mu.Lock()
	defer p.mu.Unlock()
	for _, t := range append(append([]*pickTile(nil), p.screens...), p.windows...) {
		if t.src.ID == p.sel {
			s := t.src
			return &s
		}
	}
	return nil
}

// header is the source tabs, centred 16px down.
func (p *screenPicker) header(gtx C) D {
	th := p.ph.Theme()
	ink := pickerInks(th.Dark)
	for i := range p.tabs {
		if p.tabs[i].Clicked(gtx) {
			k := huddle.ShareKind(i)
			p.mu.Lock()
			if k != p.kind { // onSourceTypeSelected: clear the pick and the border
				p.kind, p.sel = k, ""
				huddle.HideShareBorder()
			}
			p.mu.Unlock()
		}
	}
	p.mu.Lock()
	kind := p.kind
	p.mu.Unlock()
	iw, ih := gtx.Dp(165), gtx.Dp(8)+gtx.Sp(22)+gtx.Dp(8)+2
	w := 2 * iw
	x := (gtx.Constraints.Max.X - w) / 2
	y := gtx.Dp(16)
	theme.RRect(gtx, image.Rect(x, y, x+w, y+ih), gtx.Dp(12), ink.baseSec)
	labels := [2]string{nsHuddles.T("Share entire screen"), nsHuddles.T("Window")}
	for i := range labels {
		on := huddle.ShareKind(i) == kind
		cl := &p.tabs[i]
		at(gtx, image.Pt(x+i*iw, y), image.Pt(iw, ih), func(gtx C) D {
			return button(gtx, cl, image.Pt(iw, ih), func(gtx C, hov bool) {
				r := image.Rectangle{Max: image.Pt(iw, ih)}
				if on {
					theme.RRect(gtx, r, gtx.Dp(12), ink.otlTer)
					theme.RRect(gtx, r.Inset(1), gtx.Dp(12)-1, ink.basePry)
					if hov {
						theme.RRect(gtx, r.Inset(1), gtx.Dp(12)-1, ink.basePryHover)
					}
				} else if hov {
					theme.RRect(gtx, r, gtx.Dp(12), ink.baseSecHover)
				}
			}, func(gtx C) D {
				wt, col := theme.Regular, ink.contentSec
				if on {
					wt, col = theme.Bold, ink.contentPry
				}
				centerLabel(gtx, th, image.Pt(iw, ih), 15, wt, col, labels[i])
				return D{}
			})
		})
	}
	return D{Size: gtx.Constraints.Max}
}

// pickColumns is getColumnCount.
func pickColumns(width, preferred int) int {
	if width == 0 {
		return preferred
	}
	if preferred == 2 || width < 800 {
		return 2
	}
	return 3
}

// body is the 400px content with the grid.
func (p *screenPicker) body(gtx C) D {
	th := p.ph.Theme()
	ink := pickerInks(th.Dark)
	w := gtx.Constraints.Max.X
	h := gtx.Dp(400)
	p.mu.Lock()
	kind := p.kind
	tiles := p.screens
	if kind == huddle.ShareWindow {
		tiles = p.windows
	}
	sel := p.sel
	p.mu.Unlock()
	for _, t := range tiles {
		if t.click.Clicked(gtx) {
			p.mu.Lock()
			if p.sel == t.src.ID { // a click on the selected tile unselects it
				p.sel = ""
				huddle.HideShareBorder()
			} else {
				p.sel = t.src.ID
				huddle.ShowShareBorder(t.src)
			}
			sel = p.sel
			p.mu.Unlock()
		}
	}
	preferred := 2
	if kind == huddle.ShareWindow {
		preferred = 3
	}
	pw := gtx.Metric.PxToDp(w)
	cols := pickColumns(int(pw), preferred)
	large := preferred == 2 && pw >= 800
	gw := w - 2*gtx.Dp(32)
	gap := gtx.Dp(24)
	tw := (gw - gap*(cols-1)) / cols
	thumbH := gtx.Dp(140)
	if large {
		thumbH = gtx.Dp(205)
	}
	tileH := 2 + gtx.Dp(4) + thumbH + gtx.Dp(8) + gtx.Sp(18) + gtx.Dp(8)
	rows := (len(tiles) + cols - 1) / cols
	rowH := tileH + gtx.Dp(8) // + the legacy margin-bottom
	contentH := gtx.Dp(16) + rows*rowH + max(0, rows-1)*gtx.Dp(12) + gtx.Dp(16)
	defer clip.Rect{Max: image.Pt(w, h)}.Push(gtx.Ops).Pop()
	y0 := 0
	if contentH < h {
		y0 = (h - contentH) / 2 // .screenPickerWrapper: centred
	}
	lg := gtx
	lg.Constraints = layout.Exact(image.Pt(w, h-y0))
	defer op.Offset(image.Pt(0, y0)).Push(gtx.Ops).Pop()
	p.list.Layout(lg, rows+2, func(gtx C, r int) D {
		switch {
		case r == 0 || r == rows+1:
			return D{Size: image.Pt(w, gtx.Dp(16))}
		}
		r--
		n := min(cols, len(tiles)-r*cols)
		x := (w - (n*tw + (n-1)*gap)) / 2 // the row's justify-content: center
		for i := 0; i < n; i++ {
			t := tiles[r*cols+i]
			p.tile(gtx, th, ink, t, image.Pt(x+i*(tw+gap), 0), image.Pt(tw, tileH), thumbH, t.src.ID == sel)
		}
		bottom := rowH
		if r < rows-1 {
			bottom += gtx.Dp(12)
		}
		return D{Size: image.Pt(w, bottom)}
	})
	return D{Size: image.Pt(w, h)}
}

// tile draws one ScreenPickerTile at p, size sz.
func (p *screenPicker) tile(gtx C, th *theme.Theme, ink pickerInk, t *pickTile, pos, sz image.Point, thumbH int, on bool) {
	if !t.have && t.src.Thumb != nil {
		t.img, t.have = paint.NewImageOp(t.src.Thumb), true
	}
	at(gtx, pos, sz, func(gtx C) D {
		return button(gtx, &t.click, sz, func(gtx C, hov bool) {
			r := image.Rectangle{Max: sz}
			rad := gtx.Dp(4)
			if on {
				theme.RRect(gtx, r, rad, ink.otlHgl)
				theme.RRect(gtx, r.Inset(1), rad-1, ink.basePry)
				theme.RRect(gtx, r.Inset(1), rad-1, ink.surfHgl)
			}
			if hov {
				theme.RRect(gtx, r, rad, ink.hglHover)
			}
		}, func(gtx C) D {
			inner := sz.X - 2 - 2*gtx.Dp(4)
			if t.have {
				is := t.img.Size()
				s := min(1, min(float32(inner)/float32(max(1, is.X)), float32(thumbH)/float32(max(1, is.Y))))
				dw, dh := int(float32(is.X)*s), int(float32(is.Y)*s)
				ox, oy := 1+gtx.Dp(4)+(inner-dw)/2, 1+gtx.Dp(4)+(thumbH-dh)/2
				at(gtx, image.Pt(ox, oy), image.Pt(dw, dh), func(gtx C) D {
					fit(gtx, t.img, image.Pt(dw, dh), gtx.Dp(8))
					return D{Size: image.Pt(dw, dh)}
				})
			}
			wt, col := theme.Regular, ink.contentPry
			if on {
				wt, col = theme.Bold, ink.contentHgl
			}
			ty := 1 + gtx.Dp(4) + thumbH + gtx.Dp(8)
			lg := gtx
			lg.Constraints = layout.Constraints{Max: image.Pt(inner, gtx.Sp(18))}
			m := op.Record(gtx.Ops)
			d := th.Label(lg, 13, wt, col, t.src.Name)
			call := m.Stop()
			at(gtx, image.Pt(1+gtx.Dp(4)+(inner-d.Size.X)/2, ty+(gtx.Sp(18)-d.Size.Y)/2), d.Size, func(gtx C) D {
				call.Add(gtx.Ops)
				return d
			})
			return D{}
		})
	})
}

// audioBox is the footer's "Share system audio" checkbox.
func (p *screenPicker) audioBox(gtx C) D {
	th := p.ph.Theme()
	ink := pickerInks(th.Dark)
	if p.check.Clicked(gtx) {
		p.mu.Lock()
		p.audio = !p.audio
		p.mu.Unlock()
	}
	p.mu.Lock()
	on := p.audio
	p.mu.Unlock()
	label := nsHuddles.T("Share system audio")
	ls := labelSize(gtx, th, 15, theme.Regular, label)
	bh := gtx.Dp(36)
	w := gtx.Dp(16) + gtx.Dp(8) + ls.X
	// .shareAudioContainer: 24px from the modal's left (the footer pads 28)
	defer op.Offset(image.Pt(-gtx.Dp(4), 0)).Push(gtx.Ops).Pop()
	return p.check.Layout(gtx, func(gtx C) D {
		at(gtx, image.Pt(0, (bh-gtx.Dp(16))/2), image.Pt(gtx.Dp(16), gtx.Dp(16)), func(gtx C) D {
			return popups.Checkbox(gtx, th, on, false, false)
		})
		at(gtx, image.Pt(gtx.Dp(16)+gtx.Dp(8), (bh-ls.Y)/2), ls, func(gtx C) D {
			return th.Label(gtx, 15, theme.Regular, ink.contentPry, label)
		})
		pointer.CursorPointer.Add(gtx.Ops)
		return D{Size: image.Pt(w, bh)}
	})
}
