package ui

import (
	"image"

	"gioui.org/layout"

	"github.com/some-du6e/clack/internal/ui/popups"
	"github.com/some-du6e/clack/internal/ui/theme"
)

// Preferences > Clack > Replay introduction, in clack_prefs.go's slot.
func init() { clackIntroRow = (*prefsModal).prefsReplayIntroRow }

// prefsReplayIntroRow is one plain row: the label on the left, a medium
// outline Replay button on the right (the update row's button, its
// placement). Replay closes Preferences and plays the introduction over
// the app, ending on "You're all set" (intro.go).
func (p *prefsModal) prefsReplayIntroRow(f *prefsForm) {
	a, gtx := p.a, f.gtx
	pc := p.colors()
	label, btn := pt("Replay introduction"), pt("Replay")
	f.record("legend", "replay_intro", label, false, true)
	f.record("button", "replay_intro", btn, false, true)
	b := p.btn("replay_intro")
	hit := b.Clicked(gtx) // popups.Button lays the clickable out: read it first
	call, bd := measured(gtx, func(gtx layout.Context) layout.Dimensions {
		// as wide as its label, whatever the page's constraints
		gtx.Constraints = layout.Constraints{Max: image.Pt(f.w/2, gtx.Dp(36))}
		return popups.Button(gtx, a.th, b, popups.BtnOutline, popups.BtnMedium, btn, "", 0)
	})
	rowH := max(gtx.Dp(36), bd.Size.Y)
	at(gtx, image.Pt(0, f.y), image.Pt(max(0, f.w-bd.Size.X-gtx.Dp(16)), rowH), func(gtx layout.Context) layout.Dimensions {
		return vcenter(gtx, rowH, func(gtx layout.Context) layout.Dimensions {
			return a.th.Label(gtx, 15, theme.Regular, pc.text, label)
		})
	})
	place(gtx, call, image.Pt(f.w-bd.Size.X, f.y+(rowH-bd.Size.Y)/2))
	if hit {
		// after this frame: Preferences closes, the introduction opens
		a.Post(a.replayIntro)
	}
	f.y += rowH + gtx.Dp(4)
}
