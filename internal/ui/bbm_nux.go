package ui

// The simplified layout's welcome dialog (HnPE BBMNuxModal, a three-pane
// modal: Welcome → Zoom → Hover actions). maybeOpenBBMNUXModal opens it
// the first time the mode starts (bbmSettings.onboarded unset), marks it
// seen and, on the desktop app, sets the mode's zoom to 3 (150%).

import (
	"image"

	"gioui.org/layout"
	"gioui.org/widget"

	"github.com/some-du6e/clack/internal/ui/popups"
	"github.com/some-du6e/clack/internal/ui/theme"
)

// The dialog's images, on the public CDN (a.p + the asset names HnPE
// imports: qOw/, cTmY, FQRB; SquF is Slackbot's avatar).
const (
	bbmNuxGIF        = "https://a.slack-edge.com/bv1-13-br/bbm-nux-transition-53381be.gif"       // 1194×554
	bbmPersistentPNG = "https://a.slack-edge.com/bv1-13-br/persistent-hover-actions-f6d14dc.png" // 676×294
	bbmHoverPNG      = "https://a.slack-edge.com/bv1-13-br/full-message-actions-on-hover-9281d14.png"
	bbmSlackbotPNG   = "https://a.slack-edge.com/bv1-13-br/slackbot_72-5b8ea22.png"
)

// bbmNux holds the dialog's radios.
type bbmNux struct {
	zoom  [9]widget.Clickable // -3…5
	cards [2]widget.Clickable // persistent, hover
}

// openBBMNux is maybeOpenBBMNUXModal.
func (a *App) openBBMNux() {
	if a.prefs.BBMOnboarded {
		return
	}
	a.prefs.BBMOnboarded = true // setOnboarded
	z := 3                      // g9M (desktop app): setZoom(3) for the mode
	a.prefs.BBMZoom = &z
	a.showBBMNuxPane(&bbmNux{}, 0)
}

// showBBMNuxPane shows pane i (0 Welcome, 1 Zoom, 2 Hover actions); each
// pane is its own modal and Go Back / Next swap them.
func (a *App) showBBMNuxPane(n *bbmNux, i int) {
	next := func(j int) func() bool {
		return func() bool { a.Post(func() { a.showBBMNuxPane(n, j) }); return true }
	}
	var o popups.ModalOpts
	switch i {
	case 0: // WelcomePane
		o = popups.ModalOpts{
			Title: "Welcome to Simplified layout mode",
			// the GIF above the header, .WelcomeGif{width:100%}
			Hero: func(gtx layout.Context) layout.Dimensions {
				return a.bbmImage(gtx, bbmNuxGIF, gtx.Constraints.Max.X, gtx.Constraints.Max.X*554/1194)
			},
			HeroH: 520 * 554 / 1194,
			Body: func(gtx layout.Context) layout.Dimensions {
				return a.th.Layout(gtx, theme.TextStyle{Size: 15, LineHeight: 22, Color: a.th.C.Fg},
					"Simplified layout mode helps you focus by showing one section of Slack at a time. You choose the size of click targets and how message actions are displayed.")
			},
		}
		nb := &popups.ModalButton{Label: "Next", Kind: popups.BtnPrimary, Do: next(1)}
		o.Buttons, o.AutoFocus = []*popups.ModalButton{nb}, nb
	case 1: // ZoomPane (the desktop app: the message pillow and the zoom radios)
		o = popups.ModalOpts{Title: "Let’s set your zoom level: ", Body: func(gtx layout.Context) layout.Dimensions { return a.bbmNuxZoom(gtx, n) }}
		o.Buttons = []*popups.ModalButton{
			{Label: "Go Back", Kind: popups.BtnOutline, Do: next(0)},
			{Label: "Next", Kind: popups.BtnPrimary, Do: next(2)},
		}
	default: // HoverActionsPane
		o = popups.ModalOpts{Title: "Choose how to display message actions", Body: func(gtx layout.Context) layout.Dimensions { return a.bbmNuxCards(gtx, n) }}
		o.Buttons = []*popups.ModalButton{
			{Label: "Go Back", Kind: popups.BtnOutline, Do: next(1)},
			{Label: "Done", Kind: popups.BtnPrimary},
		}
	}
	a.ShowOverlay(popups.NewModal(a, o))
}

// bbmImage draws a CDN image w×h (blank until it loads).
func (a *App) bbmImage(gtx layout.Context, url string, w, h int) layout.Dimensions {
	if img, ok := a.images.Get(url, w); ok {
		return theme.Image(gtx, img, image.Pt(w, h), 0)
	}
	return layout.Dimensions{Size: image.Pt(w, h)}
}

// bbmNuxZoom: Slackbot's "We’re so glad you’re here" in a message pillow,
// then the zoom radios (GGM/ options, 150% marked Default); a pick zooms
// at once (Qp8).
func (a *App) bbmNuxZoom(gtx layout.Context, n *bbmNux) layout.Dimensions {
	c := &a.th.C
	w := gtx.Constraints.Max.X
	y := 0
	// the pillow: avatar 36, "Slackbot 👍" 15/900, the message 15px
	ph := gtx.Dp(36) + gtx.Dp(24)
	theme.RRect(gtx, image.Rect(0, 0, w, ph), gtx.Dp(8), c.BgInset)
	at(gtx, image.Pt(gtx.Dp(12), gtx.Dp(12)), image.Pt(gtx.Dp(36), gtx.Dp(36)), func(gtx layout.Context) layout.Dimensions {
		return a.avatar(gtx, bbmSlackbotPNG, "Slackbot", gtx.Dp(36), gtx.Dp(8))
	})
	at(gtx, image.Pt(gtx.Dp(56), gtx.Dp(10)), image.Pt(w-gtx.Dp(64), gtx.Dp(22)), func(gtx layout.Context) layout.Dimensions {
		return a.th.Label(gtx, 15, theme.Black, c.Fg, "Slackbot 👍")
	})
	at(gtx, image.Pt(gtx.Dp(56), gtx.Dp(32)), image.Pt(w-gtx.Dp(64), gtx.Dp(22)), func(gtx layout.Context) layout.Dimensions {
		return a.th.Label(gtx, 15, theme.Regular, c.Fg, "We’re so glad you’re here")
	})
	y = ph + gtx.Dp(20) // margin_bottom_250
	labels := [9]string{"70%", "80%", "90%", "100%", "110%", "125%", "150%", "175%", "200%"}
	for i := range n.zoom {
		v := i - 3
		btn := &n.zoom[i]
		lbl := labels[i]
		def := v == 3
		at(gtx, image.Pt(0, y), image.Pt(w, gtx.Dp(28)), func(gtx layout.Context) layout.Dimensions {
			return clickArea(gtx, btn, nil, func(gtx layout.Context) layout.Dimensions {
				bbmRadio(gtx, a.th, a.prefs.Zoom == v)
				lw := a.th.Measure(gtx, 15, theme.Bold, lbl)
				wt := theme.Regular
				if def {
					wt = theme.Bold
				}
				at(gtx, image.Pt(gtx.Dp(28), 0), image.Pt(lw+2, gtx.Dp(28)), func(gtx layout.Context) layout.Dimensions {
					return vcenter(gtx, gtx.Dp(28), func(gtx layout.Context) layout.Dimensions { return a.th.Label(gtx, 15, wt, c.Fg, lbl) })
				})
				if def { // "<strong>{label}</strong> Default"
					at(gtx, image.Pt(gtx.Dp(28)+lw+gtx.Dp(4), 0), image.Pt(w, gtx.Dp(28)), func(gtx layout.Context) layout.Dimensions {
						return vcenter(gtx, gtx.Dp(28), func(gtx layout.Context) layout.Dimensions { return a.th.Label(gtx, 15, theme.Regular, c.Fg, "Default") })
					})
				}
				return layout.Dimensions{Size: image.Pt(w, gtx.Dp(28))}
			})
		})
		if clicked(btn) {
			a.setZoomLevel(v)
			a.noteZoom()
			a.savePrefs()
		}
		y += gtx.Dp(28)
	}
	return layout.Dimensions{Size: image.Pt(w, y)}
}

// bbmNuxCards are the two RadioButtonCards (225px wide, 16px apart, the
// image then the radio with its two lines): "Always show collapsed menu"
// (persistent, checked by default) and "Show full action menu on hover
// only"; a pick is setPersistentActions.
func (a *App) bbmNuxCards(gtx layout.Context, n *bbmNux) layout.Dimensions {
	c := &a.th.C
	cw := gtx.Dp(225)
	ih := gtx.Dp(120) // .c-radio_button_card__image_container{min-height:120px}
	cards := []struct{ img, primary, secondary string }{
		{bbmPersistentPNG, "Always show collapsed menu", "Doesn’t support one click reactions"},
		{bbmHoverPNG, "Show full action menu on hover only", "Supports one click reaction"},
	}
	h := 0
	for i, cd := range cards {
		btn := &n.cards[i]
		on := (i == 0) == !a.prefs.BBMHoverActions
		x := i * (cw + gtx.Dp(16))
		d := at(gtx, image.Pt(x, 0), image.Pt(cw, gtx.Dp(400)), func(gtx layout.Context) layout.Dimensions {
			return clickArea(gtx, btn, nil, func(gtx layout.Context) layout.Dimensions {
				at(gtx, image.Pt(0, (ih-cw*294/676)/2), image.Pt(cw, cw*294/676), func(gtx layout.Context) layout.Dimensions {
					return a.bbmImage(gtx, cd.img, cw, cw*294/676)
				})
				y := ih + gtx.Dp(20)
				at(gtx, image.Pt(gtx.Dp(20), y), image.Pt(gtx.Dp(20), gtx.Dp(20)), func(gtx layout.Context) layout.Dimensions {
					return bbmRadio(gtx, a.th, on)
				})
				tw := cw - gtx.Dp(68)
				d1 := at(gtx, image.Pt(gtx.Dp(48), y-gtx.Dp(6)), image.Pt(tw, gtx.Dp(200)), func(gtx layout.Context) layout.Dimensions {
					return a.th.Layout(gtx, theme.TextStyle{Size: 15, LineHeight: 22, Weight: theme.Bold, Color: c.Fg}, cd.primary)
				})
				d2 := at(gtx, image.Pt(gtx.Dp(48), y-gtx.Dp(6)+d1.Size.Y+gtx.Dp(4)), image.Pt(tw, gtx.Dp(200)), func(gtx layout.Context) layout.Dimensions {
					return a.th.Layout(gtx, theme.TextStyle{Size: 12, LineHeight: 18, Color: c.FgMuted}, cd.secondary)
				})
				ch := y - gtx.Dp(6) + d1.Size.Y + gtx.Dp(4) + d2.Size.Y + gtx.Dp(20)
				theme.Border(gtx, image.Rect(0, 0, cw, ch), gtx.Dp(4), max(1, gtx.Dp(1)), c.Border)
				return layout.Dimensions{Size: image.Pt(cw, ch)}
			})
		})
		h = max(h, d.Size.Y)
		if clicked(btn) {
			a.setBBMPersistActions(i == 0)
		}
	}
	return layout.Dimensions{Size: image.Pt(gtx.Constraints.Max.X, h)}
}

// setBBMPersistActions is DE2Y setPersistentActions (this device).
func (a *App) setBBMPersistActions(persist bool) {
	a.prefs.BBMHoverActions = !persist
	a.savePrefs()
	a.Invalidate()
}

// bbmRadio draws a 20px radio (checked: the accent ring with a dot).
func bbmRadio(gtx layout.Context, th *theme.Theme, on bool) layout.Dimensions {
	s := gtx.Dp(20)
	ctr := image.Pt(s/2, s/2)
	if on {
		theme.Circle(gtx, ctr, gtx.Dp(18), th.C.Accent)
		theme.Circle(gtx, ctr, gtx.Dp(8), th.C.Bg)
	} else {
		theme.Circle(gtx, ctr, gtx.Dp(18), th.C.BorderStrong)
		theme.Circle(gtx, ctr, gtx.Dp(16), th.C.Bg)
	}
	return layout.Dimensions{Size: image.Pt(s, s)}
}
