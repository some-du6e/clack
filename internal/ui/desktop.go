package ui

import (
	"context"
	"image"
	"image/color"
	"log"
	"time"

	"gioui.org/io/system"
	"gioui.org/layout"
	"gioui.org/unit"
	"gioui.org/widget"

	"github.com/some-du6e/yoloslackgoclient/internal/ui/theme"
)

// Desktop integration the shell exposes to cmd/clack: OS deep links, raise
// and quit (single instance, tray), and the "Update available" banner.

// OpenURL opens a slack:// link the OS handed Clack (its URL handler, or a
// second launch forwarding its argument). It goes through Slack's
// handleDeepLink thunk, so plugins see it like an in-app link, and lands
// in the shell's deep-link handler (UIHooks.DeepLink). Safe from any
// goroutine, also before the window or the workspace is up.
func (a *App) OpenURL(uri string) {
	a.Post(func() {
		a.dispatch("handleDeepLink", map[string]any{"uri": uri},
			func(ctx context.Context, args map[string]any) (any, error) {
				u := argStr(args, "uri")
				ok := a.openDeepLink(u)
				if !ok {
					log.Printf("ui: deep link not handled: %.80s", u)
				}
				return ok, nil
			})
	})
}

// openDeepLink is the shell's slack:// handler: it navigates (or opens a
// profile) on the UI goroutine and reports whether it understood uri.
func (a *App) openDeepLink(uri string) bool {
	route, user, ok := deepLinkRoute(uri)
	if ok {
		a.Post(func() {
			if user != "" {
				a.OpenUserProfile(user)
			} else {
				a.Navigate(route)
			}
		})
	}
	return ok
}

// Raise brings the window to the front (restoring it if minimized). Safe
// from any goroutine; it doesn't wait for a frame, which a minimized window
// may never get.
func (a *App) Raise() { a.perform(system.ActionRaise) }

// Quit closes the window; Run then returns. Safe from any goroutine.
func (a *App) Quit() { a.perform(system.ActionClose) }

func (a *App) perform(act system.Action) {
	if w := a.winLive.Load(); w != nil {
		w.Perform(act) // runs on the window's event loop
		return
	}
	a.Post(func() { a.win.Perform(act) }) // before Run: on the first frame
}

// ---- update check ----

// UpdateInfo describes a newer Clack release.
type UpdateInfo struct {
	Tag string // e.g. v0.3.1
	URL string // its GitHub release page
}

type updateState struct {
	info          UpdateInfo
	view, dismiss widget.Clickable
}

// updateLoop runs Options.CheckUpdate shortly after start and twice a day
// while "Check for updates" is on. It only reads release metadata; the
// banner's button opens the release page in the browser, so nothing is
// downloaded without the user.
func (a *App) updateLoop() {
	if a.opts.CheckUpdate == nil {
		return
	}
	time.Sleep(15 * time.Second)
	for {
		if !a.updatesOff.Load() {
			ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
			info, ok, err := a.opts.CheckUpdate(ctx)
			cancel()
			if err != nil {
				log.Printf("ui: update check: %v", err)
			} else if ok {
				a.Post(func() { a.update.info = info })
			}
		}
		time.Sleep(12 * time.Hour)
	}
}

// updateChecksOn is Preferences > Advanced > "Check for updates".
func (a *App) updateChecksOn() bool { return !a.prefs.NoUpdateCheck }

func (a *App) toggleUpdateChecks() {
	a.prefs.NoUpdateCheck = !a.prefs.NoUpdateCheck
	a.updatesOff.Store(a.prefs.NoUpdateCheck)
	if a.prefs.NoUpdateCheck {
		a.update.info = UpdateInfo{}
	}
	a.savePrefs()
}

// updateBannerH is the height the banner takes above the workspace (with
// its gap), 0 when hidden.
func (a *App) updateBannerH(gtx layout.Context) int {
	u := a.update.info
	if u.Tag == "" || u.Tag == a.prefs.UpdateDismissed || a.prefs.NoUpdateCheck {
		return 0
	}
	return gtx.Dp(bannerH + bannerGap)
}

const (
	bannerH   = unit.Dp(40)
	bannerGap = unit.Dp(6)
)

// Slack's info banner blue (c-banner, the desktop app's update notice).
var bannerBlue = color.NRGBA{R: 0x12, G: 0x64, B: 0xa3, A: 0xff}

// layoutUpdateBanner draws "A new version of Clack is available" across
// the top of the workspace (r: the banner's window rectangle).
func (a *App) layoutUpdateBanner(gtx layout.Context, r image.Rectangle) {
	u := &a.update
	white := color.NRGBA{R: 0xff, G: 0xff, B: 0xff, A: 0xff}
	theme.RRect(gtx, r, gtx.Dp(8), bannerBlue)
	h := r.Dy()
	pad := gtx.Dp(16)
	// Close (×), right edge.
	box := gtx.Dp(28)
	cx := r.Max.X - pad/2 - box
	at(gtx, image.Pt(cx, r.Min.Y+(h-box)/2), image.Pt(box, box), func(gtx layout.Context) layout.Dimensions {
		return iconButton(gtx, &u.dismiss, "close", 28, 16, white, color.NRGBA{R: 0xff, G: 0xff, B: 0xff, A: 0x26}, 4)
	})
	// "View release" button, left of the close.
	label := "View release"
	tw := a.th.Measure(gtx, 13, theme.Bold, label)
	bw, bh := tw+gtx.Dp(24), gtx.Dp(28)
	bx := cx - gtx.Dp(8) - bw
	at(gtx, image.Pt(bx, r.Min.Y+(h-bh)/2), image.Pt(bw, bh), func(gtx layout.Context) layout.Dimensions {
		return clickArea(gtx, &u.view, func(gtx layout.Context, sz image.Point, hovered bool) {
			bg := white
			if hovered {
				bg = color.NRGBA{R: 0xf0, G: 0xf6, B: 0xfb, A: 0xff}
			}
			theme.RRect(gtx, image.Rectangle{Max: sz}, gtx.Dp(4), bg)
		}, func(gtx layout.Context) layout.Dimensions {
			at(gtx, image.Pt(gtx.Dp(12), 0), image.Pt(tw+1, bh), func(gtx layout.Context) layout.Dimensions {
				return vcenter(gtx, bh, func(gtx layout.Context) layout.Dimensions {
					return a.th.Label(gtx, 13, theme.Bold, bannerBlue, label)
				})
			})
			return layout.Dimensions{Size: image.Pt(bw, bh)}
		})
	})
	// Message, left-aligned, ellipsized before the button.
	msg := "A new version of Clack is available: " + u.info.Tag
	at(gtx, image.Pt(r.Min.X+pad, r.Min.Y), image.Pt(max(0, bx-gtx.Dp(12)-r.Min.X-pad), h), func(gtx layout.Context) layout.Dimensions {
		return vcenter(gtx, h, func(gtx layout.Context) layout.Dimensions {
			return a.th.Label(gtx, 15, theme.Bold, white, msg)
		})
	})
	if clicked(&u.view) {
		openURL(u.info.URL)
	}
	if clicked(&u.dismiss) { // hidden until a newer release than this one
		a.prefs.UpdateDismissed = u.info.Tag
		a.savePrefs()
		a.Invalidate()
	}
}
