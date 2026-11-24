package canvasview

import (
	"image"
	"image/color"

	"gioui.org/layout"
	"gioui.org/unit"

	"github.com/some-du6e/clack/internal/ui/icons"
	"github.com/some-du6e/clack/internal/ui/theme"
)

// QuipEmbed's save messages (boot-shared QuipSystemMessage, namespace
// quip): a non-fixed warning c-banner above the canvas while saving has
// trouble, without a close button for these kinds. Drawn only when canvas
// edits save through edit-document (core.QuipEditOn).
var saveBannerText = map[string]string{
	"quipOffline":          "We’re experiencing issues saving edits to this canvas.",
	"quipDocumentTooLarge": "This canvas reached its size limit and can’t save edits. Copy any unsaved changes before closing it.",
	"unresolved":           "We’re experiencing issues creating a new canvas.",
}

// saveBanner is the banner the canvas's save state asks for ("" none).
func (v *View) saveBanner() string {
	a := v.h.Core()
	if a == nil || !a.QuipEditOn() || v.mode == Peek {
		return ""
	}
	b := a.CanvasSaveState(v.id).Banner
	if saveBannerText[b] == "" {
		return ""
	}
	return b
}

// layoutSaveBanner draws the banner w wide at the top of gtx's area and
// returns its height: .c-banner (at least 48px, its text centred, 12px
// 20px around it) in .p-quip_system_messages' colours
// (light-dark(foreground_soft_solid, foreground_min_solid), the primary
// foreground or white) and type (15px, line height 1.46668, weight 400):
// the kind's icon at the text's size, a no-break space, the message.
func (v *View) layoutSaveBanner(gtx layout.Context, kind string, w int) int {
	th := v.h.Theme()
	bg, fg := color.NRGBA{R: 243, G: 243, B: 243, A: 255}, color.NRGBA{R: 29, G: 28, B: 29, A: 255}
	if th.Dark {
		bg, fg = color.NRGBA{R: 0x1a, G: 0x1d, B: 0x21, A: 255}, color.NRGBA{R: 255, G: 255, B: 255, A: 255}
	}
	text := nsCanvas.T(saveBannerText[kind])
	icon := "cloud-offline-filled"
	if kind == "quipDocumentTooLarge" {
		icon = "warning-filled"
	}
	lh := gtx.Dp(22) // 15px × 1.46668
	h := max(gtx.Dp(48), lh+2*gtx.Dp(12))
	theme.Fill(gtx, image.Rect(0, 0, w, h), bg)
	is := gtx.Dp(15)
	gap := th.Measure(gtx, 15, theme.Regular, " ") // the no-break space's advance
	tw := th.Measure(gtx, 15, theme.Regular, text)
	x := max(gtx.Dp(20), (w-(is+gap+tw))/2)
	y := (h - lh) / 2
	at(gtx, image.Pt(x, y+(lh-is)/2), image.Pt(is, is), func(gtx layout.Context) layout.Dimensions {
		icons.Get(icon)(gtx, unit.Dp(15), fg)
		return layout.Dimensions{Size: image.Pt(is, is)}
	})
	at(gtx, image.Pt(x+is+gap, y), image.Pt(min(tw+2, w-x-is-gap), lh), func(gtx layout.Context) layout.Dimensions {
		return th.Label(gtx, 15, theme.Regular, fg, text)
	})
	return h
}
