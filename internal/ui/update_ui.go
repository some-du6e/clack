package ui

import (
	"image"
	"image/color"
	"strings"

	"gioui.org/layout"
	"gioui.org/unit"
	"gioui.org/widget"

	"github.com/some-du6e/yoloslackgoclient/internal/ui/icons"
	"github.com/some-du6e/yoloslackgoclient/internal/ui/popups"
	"github.com/some-du6e/yoloslackgoclient/internal/ui/theme"
)

// How Slack desktop (4.53.276) shows an update, and Clack with it. The
// Electron shell has no banner: it keeps updateStatus and, once an update
// has downloaded, updateInfo (app.asar, dist/main.bundle.cjs: the nt
// enum, the UPDATE_DOWNLOADED reducer; dist/preload.bundle.js hands both
// to the web client with quitAndInstallUpdate). The web client draws it:
//
//   - The top nav's Help button gets a dot (HelpButton, boot-render
//     chunk: unread help tickets + 1 when getLatestAppUpdateTimeStamp
//     isn't -1; with no tickets it's p-top_nav__help__badge--dot, and the
//     label "Help, with 1 unread").
//   - Help opens the Help flexpane, whose "Take action" section
//     (Actionables, notifications icon) has the AppUpdate card:
//     ResourceCard{title: the release name, "Click to update to a new
//     version of the app.", a badge dot, actionable} (help-flexpane chunk).
//   - The card opens AppUpdateDialog: "Restart and update Slack?", "Just a
//     quick restart and your Slack app will be all up to date. Curious
//     what’s new? View the release notes", a warning while you're in a
//     huddle, [Not Now] [Restart Slack] (quitAndInstallUpdate).
//   - The menus' item (dY: macOS Slack menu after About, the tray menu)
//     reads Check for Updates… / Checking for Update / Downloading Update /
//     An Update Is Available (greyed) / Restart to Apply Update, which
//     restarts right away.
//   - A check from the menu answers in a message box: "You’re all good" or
//     "Couldn’t update Slack" (yI, yR). Finding one says nothing: it
//     downloads, then the dot appears.
//
// Nothing is dismissed or snoozed: Not Now closes the dialog and the dot
// stays until the restart. Slack's other place, DesktopUpdateBanner at the
// top of the sidebar, shows only on its dogfood release channel, so Clack
// leaves it out. Clack keeps one rule of its own: an update downloads in
// the background but installs only when the user picks Restart.

// updatePending reports whether a downloaded update waits for a restart
// (Slack's getAppUpdate: updateInfo is set): the Help dot and the Take
// action card.
func (a *App) updatePending() bool {
	u := &a.update
	if u.info.Tag == "" {
		return false
	}
	switch u.phase {
	case updReady, updInstalling, updManual, updFailed:
		return true
	}
	return false
}

// helpBadge draws the Help button's dot (p-top_nav__help__badge--dot) on
// its box-sized button's top-right corner.
func (a *App) helpBadge(gtx layout.Context, box int) {
	if !a.updatePending() {
		return
	}
	d := gtx.Dp(8)
	ring := gtx.Dp(2)
	c := image.Pt(box-gtx.Dp(5), gtx.Dp(5))
	theme.Circle(gtx, c, d+2*ring, a.th.C.ChromeB)
	theme.Circle(gtx, c, d, a.th.C.Danger)
}

// helpClick is the top nav's Help button: the Help flexpane when it has
// something to act on (Clack has no Help Center content to show in it
// otherwise), else the keyboard shortcuts as before.
func (a *App) helpClick() {
	if a.secondary.kind == secHelp && a.secondary.open() {
		a.CloseSecondary() // Slack's Help button toggles its flexpane
		return
	}
	if a.updatePending() {
		a.openHelpPane()
		return
	}
	a.showShortcutsHelp()
}

func (a *App) openHelpPane() {
	a.secondary = secondaryState{kind: secHelp, width: a.secondary.width}
	a.secondary.view = &helpPane{a: a}
	a.Invalidate()
}

// helpPane is Slack's HelpFlexpane, as far as Clack has content for it:
// the header (Help, Keyboard shortcuts, close) and the Take action
// section.
type helpPane struct{ a *App }

func (p *helpPane) Layout(gtx layout.Context) layout.Dimensions {
	a := p.a
	u := &a.update
	c := &a.th.C
	sz := gtx.Constraints.Max
	// header: .p-flexpane_header, title 18px/24px Black at 16px; the
	// controls are 28x28 (radius 8) right to left 6px apart from 12px.
	hh := gtx.Dp(theme.HeaderH)
	at(gtx, image.Pt(gtx.Dp(16), 0), image.Pt(sz.X-gtx.Dp(100), hh), func(gtx layout.Context) layout.Dimensions {
		return vcenter(gtx, hh, func(gtx layout.Context) layout.Dimensions {
			return a.th.Label(gtx, 18, theme.Black, c.Fg, "Help")
		})
	})
	bs := gtx.Dp(28)
	cx := sz.X - gtx.Dp(12) - bs
	kx := cx - gtx.Dp(6) - bs
	for _, b := range []struct {
		c    *widget.Clickable
		x    int
		icon string
	}{{&u.close, cx, "close"}, {&u.keys, kx, icons.Slack("keyboard")}} {
		at(gtx, image.Pt(b.x, (hh-bs)/2), image.Pt(bs, bs), func(gtx layout.Context) layout.Dimensions {
			return iconButton(gtx, b.c, b.icon, 28, 18, c.FgMuted, c.BgHover, 8)
		})
	}
	theme.Fill(gtx, image.Rect(0, hh-gtx.Dp(1), sz.X, hh), c.Border)
	if clicked(&u.close) {
		a.CloseSecondary()
	}
	if clicked(&u.keys) {
		a.showShortcutsHelp()
	}
	if a.updatePending() {
		atExact(gtx, image.Pt(0, hh), image.Pt(sz.X, sz.Y-hh), p.actionables)
	}
	return layout.Dimensions{Size: sz}
}

// updateCardText is the Take action card's title and description: Slack's
// AppUpdate (the release name, "Click to update…"), and what happened
// after a restart that didn't install.
func (a *App) updateCardText() (title, desc string) {
	u := &a.update
	title = AppName + " " + u.info.Tag
	switch u.phase {
	case updInstalling:
		return title, "Installing " + AppName + " " + u.info.Tag + "…"
	case updManual:
		return title, "Finish installing in the window that opened, then restart " + AppName + "."
	case updFailed:
		return title, "Couldn’t install: " + u.err
	}
	return title, "Click to update to a new version of the app."
}

// actionables is Slack's Actionables: .p-help_home__actionables_container
// (margin 7px 18px 14px), its title (13px bold, rgba(29,28,29,.7), a
// notifications icon 4px before it, 14px above the cards) and the
// AppUpdate ResourceCard.
func (p *helpPane) actionables(gtx layout.Context) layout.Dimensions {
	a := p.a
	u := &a.update
	c := &a.th.C
	w := gtx.Constraints.Max.X
	x0, y := gtx.Dp(18), gtx.Dp(7)
	lh := gtx.Dp(18) // 13px, line-height 1.38463
	muted := theme.WithAlpha(c.Fg, 0xb3)
	at(gtx, image.Pt(x0, y+(lh-gtx.Dp(16))/2), image.Pt(gtx.Dp(16), gtx.Dp(16)), func(gtx layout.Context) layout.Dimensions {
		return icons.Get(icons.Slack("notifications"))(gtx, 16, muted)
	})
	at(gtx, image.Pt(x0+gtx.Dp(20), y), image.Pt(w-x0*2, lh), func(gtx layout.Context) layout.Dimensions {
		return vcenter(gtx, lh, func(gtx layout.Context) layout.Dimensions {
			return a.th.Label(gtx, 13, theme.Bold, muted, "Take action")
		})
	})
	y += lh + gtx.Dp(14) - gtx.Dp(8) // the card's own margin-top: 8px
	title, desc := a.updateCardText()
	cw := w - 2*x0
	d := at(gtx, image.Pt(x0, y+gtx.Dp(8)), image.Pt(cw, gtx.Dp(400)), func(gtx layout.Context) layout.Dimensions {
		return a.resourceCard(gtx, &u.card, cw, title, desc)
	})
	if clicked(&u.card) {
		a.openAppUpdateDialog()
	}
	return layout.Dimensions{Size: image.Pt(w, y+gtx.Dp(8)+d.Size.Y+gtx.Dp(14))}
}

// resourceCard is Slack's ResourceCard{showBadge, actionable} without an
// image: .p-help_search__resource_card__container (radius 8, padding 12,
// a 1px #1d1c1d21 ring; actionable: #e01e5a1a fill and border), the badge
// column (8px wide, 36px tall, 12px gap; an 8px
// --dt_color-base-inv-imp dot with a 1px white border), the title
// (15px/22px bold) and its text (13px, line-height 1.2, #616061, 3px
// below; the text column is centered, margin -4px).
func (a *App) resourceCard(gtx layout.Context, ck *widget.Clickable, w int, title, desc string) layout.Dimensions {
	c := &a.th.C
	pad := gtx.Dp(12)
	tx := pad + gtx.Dp(8) + gtx.Dp(12)
	tw := max(0, w-tx-pad)
	// measure the text column first: the card is as tall as it needs
	textCol := func(gtx layout.Context) layout.Dimensions {
		gtx.Constraints = layout.Constraints{Max: image.Pt(tw, 1<<20)}
		td := a.th.Layout(gtx, theme.TextStyle{Size: 15, Weight: theme.Bold, Color: c.Fg, LineHeight: 22, MaxLines: 2}, title)
		yy := td.Size.Y
		if desc != "" {
			yy += gtx.Dp(3)
			dd := at(gtx, image.Pt(0, yy), image.Pt(tw, 1<<20), func(gtx layout.Context) layout.Dimensions {
				return a.th.Layout(gtx, theme.TextStyle{Size: 13, Color: c.FgMuted, LineHeight: 16, MaxLines: 3}, desc)
			})
			yy += dd.Size.Y
		}
		return layout.Dimensions{Size: image.Pt(tw, yy)}
	}
	call, td := measured(gtx, textCol)
	inner := max(gtx.Dp(36), td.Size.Y-gtx.Dp(8))
	h := inner + 2*pad
	return atExact(gtx, image.Point{}, image.Pt(w, h), func(gtx layout.Context) layout.Dimensions {
		return clickArea(gtx, ck, func(gtx layout.Context, sz image.Point, hovered bool) {
			r := image.Rectangle{Max: sz}
			rad := gtx.Dp(8)
			theme.RRect(gtx, r, rad, theme.WithAlpha(c.Danger, 0x1a)) // the border, #e01e5a1a
			bg := theme.Over(theme.WithAlpha(c.Danger, 0x1a), c.Bg)
			if hovered { // :hover adds 0 1px 3px #00000014
				theme.Shadow(gtx, r.Add(image.Pt(0, gtx.Dp(1))), rad, gtx.Dp(3), theme.WithAlpha(c.PanelShadow, 0x14))
			}
			theme.RRect(gtx, r.Inset(gtx.Dp(1)), rad-gtx.Dp(1), bg)
		}, func(gtx layout.Context) layout.Dimensions {
			// badge column, vertically centered
			bc := image.Pt(pad+gtx.Dp(4), h/2)
			theme.Circle(gtx, bc, gtx.Dp(8)+2, c.Bg)
			theme.Circle(gtx, bc, gtx.Dp(8), c.Danger)
			place(gtx, call, image.Pt(tx, (h-td.Size.Y)/2))
			return layout.Dimensions{Size: image.Pt(w, h)}
		})
	})
}

// ---- the dialog ----

// updateDialog is AppUpdateDialog: the modal, its buttons and the body's
// release-notes link.
type updateDialog struct {
	a               *App
	modal           *popups.Modal
	notNow, restart *popups.ModalButton
	notes           widget.Clickable
}

func (d *updateDialog) open() bool { return d != nil && !d.modal.Dismissed() }

// openAppUpdateDialog is Slack's openAppUpdateDialog (the Take action
// card). Its Restart button is the user's choice to install.
func (a *App) openAppUpdateDialog() {
	u := &a.update
	if u.phase != updReady && u.phase != updFailed {
		return // nothing downloaded to restart into, or already restarting
	}
	if u.dialog.open() {
		return
	}
	d := &updateDialog{a: a}
	d.notNow = &popups.ModalButton{Label: "Not Now", Kind: popups.BtnOutline}
	d.restart = &popups.ModalButton{Label: "Restart " + AppName, Kind: popups.BtnPrimary, Do: func() bool { a.installUpdate(); return true }}
	d.modal = popups.NewModal(a, popups.ModalOpts{
		Title:        "Restart and update " + AppName + "?",
		Body:         d.layout,
		EnterSubmits: true,
		Buttons:      []*popups.ModalButton{d.notNow, d.restart},
	})
	u.dialog = d
	a.ShowOverlay(d.modal)
}

// inHuddle reports whether you're in or joining a huddle (the dialog's
// warning: a restart drops the call). huddleOverride is the tests'.
func (a *App) inHuddle() bool {
	if huddleOverride != nil {
		return *huddleOverride
	}
	return a.hud != nil && a.hud.Active()
}

var huddleOverride *bool

const (
	updDialogText  = "Just a quick restart and your " + AppName + " app will be all up to date. Curious what’s new? "
	updDialogLink  = "View the release notes"
	updHuddleAlert = "You’re currently in a huddle. Restarting " + AppName + " will disconnect you from the huddle."
)

func (d *updateDialog) layout(gtx layout.Context) layout.Dimensions {
	a := d.a
	c := &a.th.C
	w := gtx.Constraints.Max.X
	h := linkPara(gtx, a.th, w, 15, 22, c.Fg, c.Link, updDialogText, updDialogLink, &d.notes)
	if clicked(&d.notes) {
		url := a.update.info.URL
		if url == "" {
			url = releasesPage
		}
		openURL(url)
	}
	if a.inHuddle() {
		// c-alert type warning, margin_top_75 (12px): the warning icon
		// (20px) and the text 8px after it (margin_left_50).
		y := h + gtx.Dp(12)
		pad := gtx.Dp(12)
		tx := pad + gtx.Dp(20) + gtx.Dp(8)
		call, td := measured(gtx, func(gtx layout.Context) layout.Dimensions {
			gtx.Constraints = layout.Constraints{Max: image.Pt(max(0, w-tx-pad), 1<<20)}
			return a.th.Para(gtx, 15, 22, theme.Regular, c.Fg, updHuddleAlert)
		})
		bh := max(td.Size.Y, gtx.Dp(22)) + 2*pad
		r := image.Rect(0, y, w, y+bh)
		theme.RRect(gtx, r, gtx.Dp(8), theme.WithAlpha(c.Warning, 0x26))
		theme.Border(gtx, r, gtx.Dp(8), gtx.Dp(1), theme.WithAlpha(c.Warning, 0x66))
		at(gtx, image.Pt(pad, y+pad+gtx.Dp(1)), image.Pt(gtx.Dp(20), gtx.Dp(20)), func(gtx layout.Context) layout.Dimensions {
			return icons.Get("warning")(gtx, 20, c.Warning)
		})
		place(gtx, call, image.Pt(tx, y+pad))
		h = y + bh
	}
	return layout.Dimensions{Size: image.Pt(w, h)}
}

// linkPara lays out text then a link on the same flow, word-wrapped to w
// (the link kept whole: it wraps as one piece), and returns the height.
func linkPara(gtx layout.Context, th *theme.Theme, w int, size, lh float32, col, linkCol color.NRGBA, text, link string, ck *widget.Clickable) int {
	lineH := gtx.Sp(unit.Sp(lh))
	space := th.Measure(gtx, unit.Sp(size), theme.Regular, " ")
	x, y := 0, 0
	for _, word := range strings.Fields(text) {
		ww := th.Measure(gtx, unit.Sp(size), theme.Regular, word)
		if x > 0 && x+ww > w {
			x, y = 0, y+lineH
		}
		at(gtx, image.Pt(x, y), image.Pt(ww+gtx.Dp(2), lineH), func(gtx layout.Context) layout.Dimensions {
			return vcenter(gtx, lineH, func(gtx layout.Context) layout.Dimensions {
				return th.Label(gtx, unit.Sp(size), theme.Regular, col, word)
			})
		})
		x += ww + space
	}
	lw := th.Measure(gtx, unit.Sp(size), theme.Regular, link)
	if x > 0 && x+lw > w {
		x, y = 0, y+lineH
	}
	at(gtx, image.Pt(x, y), image.Pt(lw+gtx.Dp(2), lineH), func(gtx layout.Context) layout.Dimensions {
		return clickArea(gtx, ck, nil, func(gtx layout.Context) layout.Dimensions {
			d := vcenter(gtx, lineH, func(gtx layout.Context) layout.Dimensions {
				return th.Label(gtx, unit.Sp(size), theme.Regular, linkCol, link)
			})
			if ck.Hovered() { // c-link: underline on hover
				theme.Fill(gtx, image.Rect(0, lineH-gtx.Dp(3), lw, lineH-gtx.Dp(2)), linkCol)
			}
			return layout.Dimensions{Size: image.Pt(lw, lineH), Baseline: d.Baseline}
		})
	})
	return y + lineH
}

// updateMessageBox is Slack's showMessageBox for the updater (yI, yR): the
// message as the title, the detail, OK.
func (a *App) updateMessageBox(title, detail string) {
	u := &a.update
	if d, ok := u.box.(dismisser); ok && !u.box.Dismissed() {
		d.Dismiss() // one at a time: a second check replaces the first answer
	}
	body := func(gtx layout.Context) layout.Dimensions {
		gtx.Constraints.Min = image.Point{}
		return a.th.Para(gtx, 15, 22, theme.Regular, a.th.C.Fg, detail)
	}
	m := popups.NewModal(a, popups.ModalOpts{
		Title: title, Body: body, EnterSubmits: true,
		Buttons: []*popups.ModalButton{{Label: "OK", Kind: popups.BtnPrimary}},
	})
	u.box, u.boxTitle = m, title
	a.ShowOverlay(m)
}

// updateBoxShown is the open updater message box's title, "" if none.
func (a *App) updateBoxShown() string {
	if u := &a.update; u.box != nil && !u.box.Dismissed() {
		return u.boxTitle
	}
	return ""
}

// ---- the menus ----

// Slack's update item titles (dY).
const (
	updMenuCheck     = "Check for Updates…"
	updMenuChecking  = "Checking for Update"
	updMenuDownload  = "Downloading Update"
	updMenuAvailable = "An Update Is Available"
	updMenuRestart   = "Restart to Apply Update"
)

// updateMenuItem is the update item in the menus (macOS: Clack after
// About; the workspace menu elsewhere): its title and whether it can be
// chosen, as Slack's dY decides them.
func (a *App) updateMenuItem() (title string, enabled bool) {
	u := &a.update
	switch {
	case u.check.checking:
		return updMenuChecking, false
	case u.info.Tag == "":
		return updMenuCheck, true
	}
	switch u.phase {
	case updDownloading:
		return updMenuDownload, false
	case updAvailable:
		// Slack greys this out (its updater always downloads next).
		// Clack's waits for the user when downloads are off or the
		// download failed: the item opens Preferences > Advanced, which
		// has Download and View release.
		return updMenuAvailable, true
	case updReady:
		return updMenuRestart, true
	case updInstalling:
		return updMenuRestart, false
	}
	return updMenuCheck, true // manual, failed: Slack's error state
}

// updateMenuClick runs the update item (UI goroutine).
func (a *App) updateMenuClick() {
	title, ok := a.updateMenuItem()
	if !ok {
		return
	}
	switch title {
	case updMenuRestart: // restart-to-apply: no dialog in Slack either
		a.installUpdate()
	case updMenuAvailable:
		a.openPrefsSection("advanced")
	default:
		a.checkUpdatesNotify()
	}
}
