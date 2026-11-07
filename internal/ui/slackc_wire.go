package ui

import (
	"image"

	"gioui.org/io/pointer"
	"gioui.org/layout"

	"github.com/some-du6e/yoloslackgoclient/internal/core"
	"github.com/some-du6e/yoloslackgoclient/internal/ui/slackc"
)

// slackcState wires the Taut component registry (internal/ui/slackc) to
// the app: its environment, the per-frame hook and the plugin overlay
// layer. Free while no plugin runtime is attached.
type slackcState struct {
	core   *core.App
	inited bool
}

// slackcFrame runs at the start of every frame.
func (a *App) slackcFrame(gtx layout.Context) {
	if c := a.Core(); !a.slackc.inited || c != a.slackc.core {
		a.slackc.inited, a.slackc.core = true, c
		slackc.Init(slackc.Env{
			Theme:      a.th,
			Images:     a.images,
			Invalidate: a.Invalidate,
			ShowOverlay: func(o interface {
				Layout(gtx layout.Context) layout.Dimensions
				Dismissed() bool
			}) {
				a.ShowOverlay(o)
			},
			Toast:           a.Toast,
			OpenURL:         openURL,
			Navigate:        a.Navigate,
			Core:            c,
			PointerWin:      func() image.Point { return a.ptrWin },
			OpenEmojiPicker: a.OpenEmojiPicker,
			OpenProfile:     a.OpenProfile,
		})
	}
	slackc.Frame(gtx)
}

// slackcOverlays draws plugin modals, menus and document.body content
// above the native overlays.
func (a *App) slackcOverlays(gtx layout.Context) {
	if slackc.HasOverlays() {
		slackc.LayoutOverlays(gtx)
	}
}

// globalPointer forwards window pointer input to plugins that listen on
// document/window (Oneko's mousemove); free while none listens.
func (a *App) globalPointer(gtx layout.Context, pe pointer.Event) {
	var typ string
	switch pe.Kind {
	case pointer.Move, pointer.Drag:
		typ = "mousemove"
	case pointer.Press:
		typ = "mousedown"
	case pointer.Release:
		typ = "mouseup"
	default:
		return
	}
	if slackc.WantsGlobal(typ) {
		slackc.GlobalPointer(typ, pe.Position.X, pe.Position.Y, gtx.Metric.PxPerDp, int(pe.Buttons))
	}
}
