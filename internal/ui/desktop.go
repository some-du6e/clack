package ui

import (
	"context"
	"errors"
	"image"
	"image/color"
	"log"

	"gioui.org/io/system"
	"gioui.org/layout"
	"gioui.org/unit"
	"gioui.org/widget"

	"github.com/some-du6e/yoloslackgoclient/internal/ui/theme"
)

// Desktop integration the shell exposes to cmd/clack: OS deep links, raise
// and quit (single instance, tray), and the update banner.

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
					log.Printf("ui: deep link not handled: %.80s", noQuery(u)) // the query can name a search or a message
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

// ---- updates ----

// UpdateInfo describes a newer Clack release.
type UpdateInfo struct {
	Tag string // e.g. v0.3.1
	URL string // its GitHub release page
	// Release is the shell's own description of it (cmd/clack: the
	// desktop.Release), handed back to DownloadUpdate.
	Release any
}

// ErrUpdateManual (from Options.InstallUpdate): the package opened in the
// system's installer for the user to finish; Clack keeps running.
var ErrUpdateManual = errors.New("update opened in the system installer")

// updatePhase is where the banner is.
type updatePhase int

const (
	updNone        updatePhase = iota
	updAvailable               // "A new version of Clack is available" + View release
	updDownloading             // in the background; no banner yet
	updReady                   // verified: "Clack X is ready to install"
	updInstalling              // after the click
	updManual                  // the system installer is open
	updFailed                  // the install didn't work
)

type updateState struct {
	info  UpdateInfo
	phase updatePhase
	err   string
	// later hides the ready banner for this session ("Later"); the
	// download stays, so the next launch offers it again.
	later bool
	// manual: a check the user asked for (Check now, Check for
	// Updates…) found it, so the banner shows even with "Check for
	// updates" off or after × for this release.
	manual                        bool
	dlErr                         string             // the last download failed: why
	cancel                        context.CancelFunc // the running download
	check                         updateCheckState   // update_check.go
	view, dismiss, install, delay widget.Clickable
	// quit closes Clack after a successful InstallUpdate (tests replace it).
	quit func()
}

// updateFound takes a check's newer release (UI goroutine). manual: the
// user asked for the check, so the banner shows whatever the settings.
func (a *App) updateFound(info UpdateInfo, manual bool) {
	u := &a.update
	if info.Tag == u.info.Tag && u.phase != updAvailable && u.phase != updNone {
		if manual { // asked again: show it again
			u.manual, u.later = true, false
			a.Invalidate()
		}
		return // already downloading, ready or installing this one
	}
	if u.cancel != nil {
		u.cancel()
		u.cancel = nil
	}
	same := info.Tag == u.info.Tag
	if !same {
		u.dlErr = ""
	}
	u.manual = manual || u.manual && same
	u.info, u.phase, u.err, u.later = info, updAvailable, "", false
	a.startUpdateDownload(false)
	a.Invalidate()
}

// startUpdateDownload fetches the available release in the background
// (UI goroutine): when downloads are on, or force (Preferences'
// Download button).
func (a *App) startUpdateDownload(force bool) {
	u := &a.update
	if u.phase != updAvailable || a.opts.DownloadUpdate == nil {
		return
	}
	if !force && (!a.updateDownloadsOn() || a.prefs.NoUpdateCheck && !u.manual) {
		return
	}
	info := u.info
	ctx, cancel := context.WithCancel(context.Background())
	u.phase, u.cancel = updDownloading, cancel
	go func() {
		err := a.opts.DownloadUpdate(ctx, info)
		a.Post(func() {
			if u.info.Tag != info.Tag || u.phase != updDownloading {
				return
			}
			u.cancel = nil
			cancel()
			if err != nil {
				log.Printf("ui: update download: %v", err)
				u.phase = updAvailable // the release page, as without downloads
				u.dlErr = err.Error()
				a.Invalidate()
				return
			}
			u.phase, u.dlErr = updReady, ""
			a.Invalidate()
		})
	}()
}

// installUpdate is the "Install and restart" click: the only way an
// update is installed.
func (a *App) installUpdate() {
	u := &a.update
	if u.phase != updReady && u.phase != updFailed || a.opts.InstallUpdate == nil {
		return
	}
	info := u.info
	u.phase, u.err = updInstalling, ""
	go func() {
		err := a.opts.InstallUpdate(info)
		a.Post(func() {
			switch {
			case err == nil:
				if u.quit != nil {
					u.quit()
				} else {
					a.Quit() // the helper starts the new version once Clack exits
				}
			case errors.Is(err, ErrUpdateManual):
				u.phase = updManual
			default:
				log.Printf("ui: update install: %v", err)
				u.phase, u.err = updFailed, err.Error()
			}
			a.Invalidate()
		})
	}()
}

// updateChecksOn is Preferences > Advanced > "Check for updates"
// (automatic checks; Check now works either way).
func (a *App) updateChecksOn() bool { return !a.prefs.NoUpdateCheck }

func (a *App) toggleUpdateChecks() {
	a.prefs.NoUpdateCheck = !a.prefs.NoUpdateCheck
	a.updatesOff.Store(a.prefs.NoUpdateCheck)
	if a.prefs.NoUpdateCheck {
		a.stopUpdateDownload()
		a.update.info, a.update.phase, a.update.manual = UpdateInfo{}, updNone, false
	}
	a.savePrefs()
}

// updateDownloadsOn is Preferences > Advanced > "Download updates
// automatically" (on unless turned off).
func (a *App) updateDownloadsOn() bool { return !a.prefs.NoUpdateDownload }

func (a *App) toggleUpdateDownloads() {
	a.prefs.NoUpdateDownload = !a.prefs.NoUpdateDownload
	if a.prefs.NoUpdateDownload {
		a.stopUpdateDownload()
	} else {
		a.startUpdateDownload(false)
	}
	a.savePrefs()
}

// stopUpdateDownload cancels a running download (a finished one stays
// ready to install).
func (a *App) stopUpdateDownload() {
	u := &a.update
	if u.phase == updDownloading {
		if u.cancel != nil {
			u.cancel()
			u.cancel = nil
		}
		u.phase = updAvailable
	}
}

// updateBannerShown reports whether the banner is up.
func (a *App) updateBannerShown() bool {
	u := &a.update
	if u.info.Tag == "" || a.prefs.NoUpdateCheck && !u.manual {
		return false
	}
	switch u.phase {
	case updAvailable, updFailed:
		return u.manual || u.info.Tag != a.prefs.UpdateDismissed
	case updReady, updManual:
		return !u.later
	case updInstalling:
		return true
	}
	return false
}

// updateBannerText is the banner's message and buttons for its phase.
func (a *App) updateBannerText() (msg string, buttons []string) {
	u := &a.update
	switch u.phase {
	case updReady:
		return "Clack " + u.info.Tag + " is ready to install", []string{"Install and restart", "Later"}
	case updInstalling:
		return "Installing Clack " + u.info.Tag + "…", nil
	case updManual:
		return "Finish installing Clack " + u.info.Tag + " in the window that opened, then restart Clack", []string{"Later"}
	case updFailed:
		return "Couldn’t install Clack " + u.info.Tag + ": " + u.err, []string{"View release"}
	}
	return "A new version of Clack is available: " + u.info.Tag, []string{"View release"}
}

// updateBannerH is the height the banner takes above the workspace (with
// its gap), 0 when hidden.
func (a *App) updateBannerH(gtx layout.Context) int {
	if !a.updateBannerShown() {
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

// layoutUpdateBanner draws the update banner across the top of the
// workspace (r: the banner's window rectangle): a new version with View
// release, or a downloaded one with Install and restart / Later.
func (a *App) layoutUpdateBanner(gtx layout.Context, r image.Rectangle) {
	u := &a.update
	white := color.NRGBA{R: 0xff, G: 0xff, B: 0xff, A: 0xff}
	theme.RRect(gtx, r, gtx.Dp(8), bannerBlue)
	h := r.Dy()
	pad := gtx.Dp(16)
	msg, buttons := a.updateBannerText()
	right := r.Max.X - pad/2
	// Close (×), right edge, while it only points at the release page.
	if u.phase == updAvailable || u.phase == updFailed {
		box := gtx.Dp(28)
		right -= box
		at(gtx, image.Pt(right, r.Min.Y+(h-box)/2), image.Pt(box, box), func(gtx layout.Context) layout.Dimensions {
			return iconButton(gtx, &u.dismiss, "close", 28, 16, white, color.NRGBA{R: 0xff, G: 0xff, B: 0xff, A: 0x26}, 4)
		})
	} else {
		right -= pad / 2
	}
	// Buttons, right to left: the last is the rightmost.
	for i := len(buttons) - 1; i >= 0; i-- {
		label := buttons[i]
		c := &u.view
		switch label {
		case "Install and restart":
			c = &u.install
		case "Later":
			c = &u.delay
		}
		primary := i == 0
		tw := a.th.Measure(gtx, 13, theme.Bold, label)
		bw, bh := tw+gtx.Dp(24), gtx.Dp(28)
		bx := right - gtx.Dp(8) - bw
		right = bx
		at(gtx, image.Pt(bx, r.Min.Y+(h-bh)/2), image.Pt(bw, bh), func(gtx layout.Context) layout.Dimensions {
			return clickArea(gtx, c, func(gtx layout.Context, sz image.Point, hovered bool) {
				if primary {
					bg := white
					if hovered {
						bg = color.NRGBA{R: 0xf0, G: 0xf6, B: 0xfb, A: 0xff}
					}
					theme.RRect(gtx, image.Rectangle{Max: sz}, gtx.Dp(4), bg)
					return
				}
				// secondary: white outline on the blue
				theme.RRect(gtx, image.Rectangle{Max: sz}, gtx.Dp(4), white)
				in := image.Rectangle{Min: image.Pt(1, 1), Max: sz.Sub(image.Pt(1, 1))}
				bg := bannerBlue
				if hovered {
					bg = color.NRGBA{R: 0x1d, G: 0x74, B: 0xb5, A: 0xff}
				}
				theme.RRect(gtx, in, gtx.Dp(4)-1, bg)
			}, func(gtx layout.Context) layout.Dimensions {
				col := bannerBlue
				if !primary {
					col = white
				}
				at(gtx, image.Pt(gtx.Dp(12), 0), image.Pt(tw+1, bh), func(gtx layout.Context) layout.Dimensions {
					return vcenter(gtx, bh, func(gtx layout.Context) layout.Dimensions {
						return a.th.Label(gtx, 13, theme.Bold, col, label)
					})
				})
				return layout.Dimensions{Size: image.Pt(bw, bh)}
			})
		})
	}
	// Message, left-aligned, ellipsized before the buttons.
	at(gtx, image.Pt(r.Min.X+pad, r.Min.Y), image.Pt(max(0, right-gtx.Dp(12)-r.Min.X-pad), h), func(gtx layout.Context) layout.Dimensions {
		return vcenter(gtx, h, func(gtx layout.Context) layout.Dimensions {
			return a.th.Label(gtx, 15, theme.Bold, white, msg)
		})
	})
	if clicked(&u.view) {
		openURL(u.info.URL)
	}
	if clicked(&u.install) {
		a.installUpdate()
	}
	if clicked(&u.delay) { // until the next launch
		u.later = true
		a.Invalidate()
	}
	if clicked(&u.dismiss) { // hidden until a newer release than this one
		a.prefs.UpdateDismissed, u.manual = u.info.Tag, false
		a.savePrefs()
		a.Invalidate()
	}
}
