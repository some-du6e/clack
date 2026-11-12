package ui

import (
	"image"
	"runtime"

	"gioui.org/layout"

	"github.com/some-du6e/clack/internal/ui/theme"
)

// WindowsAppearancePref: the Windows block at the end of Preferences >
// Notifications > Sounds & appearance (gantry-v2-async-prefs-dialog
// b5a3ec9ffe4c9e38, build 2cd9270e786c: PrefsSectionNotifications renders
// it after SoundAppearancePref when the desktop app runs on Windows,
// es.uF). Its three settings are the desktop app's own, not users.prefs
// (getInitialDesktopState reads them from the shell's settings and cGU
// writes them back), so Clack keeps them in ui.json:
//
//   - "Flash window when a notification is received": the radios Never,
//     When left idle "(inactive for at least 10 seconds)" and Always, the
//     setting win32.windowFlashBehavior (default "idle"; dock.go flashes
//     the frame by it, and runfromtray.go minimizes on close under
//     "always");
//   - unless the force_native_notifications experiment is on, "Deliver
//     notifications via…": a 400px select of the methods o6 enables on
//     Windows 10 and 11 (es.jy): "Slack’s built-in notifications" (html),
//     "Windows Action Center" (winrt) and "Windows Action Center
//     (abbreviated)" (window). notificationMethod's default is
//     settings.notificationMethod || (isGpuCompositionAvailable ? "winrt"
//     : null), and DWM composition is always on since Windows 8;
//   - with html picked, "Notification position" (the corners o9) and
//     "Display" (o7), Slack's notifyPosition {corner, display}.
//
// The win32 main process shows winrt and window the same way, through
// its toast class (the method only matters as "html", isShowingHtml
// Notifications @750913), and Clack shows both as its WinRT toasts. Clack
// has no HTML notification window, so html shows toasts too, as Slack's
// own shell does when it can't draw HTML notifications (no GPU
// composition); its corner and display are kept and not used.

// winAppearanceAnyOS draws the block on every OS (tests, the shots).
var winAppearanceAnyOS bool

// Slack's notificationMethod options on Windows 10 and 11 (o6, the
// enabled ones) and notifyPosition's corners (o9) and displays (o7).
var (
	notificationMethods = []struct{ value, label string }{
		{"html", "Slack’s built-in notifications"},
		{"winrt", "Windows Action Center"},
		{"window", "Windows Action Center (abbreviated)"},
	}
	notifyCorners = []struct{ value, label string }{
		{"bottom_right", "bottom-right"},
		{"top_right", "top-right"},
		{"top_left", "top-left"},
		{"bottom_left", "bottom-left"},
	}
	notifyDisplays = []struct{ value, label string }{
		{"same_as_app", "same display as app"},
		{"primary", "primary display"},
	}
)

// notificationMethod is getInitialDesktopState's notificationMethod.
func (a *App) notificationMethod() string {
	if v := a.prefs.NotificationMethod; v != "" {
		return v
	}
	return "winrt"
}

// notifyPosition is notifyPosition with o8's defaults.
func (a *App) notifyPosition() (corner, display string) {
	corner, display = a.prefs.NotifyCorner, a.prefs.NotifyDisplay
	if corner == "" {
		corner = "bottom_right"
	}
	if display == "" {
		display = "same_as_app"
	}
	return corner, display
}

// winAppearanceOn: the block shows (Windows).
func winAppearanceOn() bool { return runtime.GOOS == "windows" || winAppearanceAnyOS }

// setDesktopSetting is cGU({name, value}) for one of the block's settings.
func (a *App) setDesktopSetting(set func(p *prefs)) {
	set(&a.prefs)
	a.savePrefs()
	a.Invalidate()
}

// winAppearance is WindowsAppearancePref.
func (p *prefsModal) winAppearance(f *prefsForm) {
	a := p.a
	forceNative := false
	if c := a.Core(); c != nil {
		forceNative = c.Experiment("force_native_notifications") == "on"
	}
	f.cLabel("Flash window when a notification is received")
	flash := a.windowFlashBehavior()
	for _, r := range []struct{ value, label, sub string }{
		{flashNever, "Never", ""},
		{flashIdle, "When left idle", "(inactive for at least 10 seconds)"},
		{flashAlways, "Always", ""},
	} {
		v := r.value
		f.option("flashwindow="+v, r.label, r.sub, flash == v, true, func() {
			a.setDesktopSetting(func(u *prefs) { u.WindowFlashBehavior = v })
		})
	}
	if forceNative {
		return
	}
	method := a.notificationMethod()
	f.y += f.gtx.Dp(16) // margin_top_100
	f.text("Deliver notifications via…")
	f.selectBoxW("winssb_notification_method", "", optionLabel(notificationMethods, method), true,
		optionItems(notificationMethods, func(v string) {
			a.setDesktopSetting(func(u *prefs) { u.NotificationMethod = v })
		}), 400, nil)
	if method != "html" {
		return
	}
	corner, display := a.notifyPosition()
	f.cLabel("Notification position")
	f.selectBoxW("winssb_notify_corner", "", optionLabel(notifyCorners, corner), true,
		optionItems(notifyCorners, func(v string) {
			a.setDesktopSetting(func(u *prefs) { u.NotifyCorner, u.NotifyDisplay = v, display })
		}), 300, nil)
	f.cLabel("Display")
	f.selectBoxW("winssb_notify_display", "", optionLabel(notifyDisplays, display), true,
		optionItems(notifyDisplays, func(v string) {
			a.setDesktopSetting(func(u *prefs) { u.NotifyCorner, u.NotifyDisplay = corner, v })
		}), 300, nil)
}

// optionLabel is a select's shown option (selectedOption; "" when none
// matches, as Slack's select then shows none).
func optionLabel(opts []struct{ value, label string }, v string) string {
	for _, o := range opts {
		if o.value == v {
			return o.label
		}
	}
	return ""
}

// optionItems is a select's menu.
func optionItems(opts []struct{ value, label string }, set func(string)) []MenuItem {
	items := make([]MenuItem, 0, len(opts))
	for _, o := range opts {
		v := o.value
		items = append(items, MenuItem{Label: o.label, Do: func() { set(v) }})
	}
	return items
}

// cLabel is a c-label standing alone (text, class "inline_block
// margin_right_100 margin_top_100"): 16px above, the 700 15px/22 text
// and its c-label__text padding-bottom of 8px.
func (f *prefsForm) cLabel(t string) {
	t = pt(t)
	gtx, a := f.gtx, f.p.a
	f.y += gtx.Dp(16)
	f.record("label", "", t, false, true)
	at(gtx, image.Pt(0, f.y), image.Pt(f.w, gtx.Dp(22)), func(gtx layout.Context) layout.Dimensions {
		return a.th.Label(gtx, 15, theme.Bold, f.p.colors().text, t)
	})
	f.y += gtx.Dp(30)
}

// text is a plain line of the dialog's 15px/22 text with nothing under
// it but the control that follows (Slack's bare text node before a
// select).
func (f *prefsForm) text(t string) {
	t = pt(t)
	gtx, a := f.gtx, f.p.a
	f.record("text", "", t, false, true)
	d := at(gtx, image.Pt(0, f.y), image.Pt(f.w, gtx.Dp(400)), func(gtx layout.Context) layout.Dimensions {
		return a.th.Layout(gtx, theme.TextStyle{Size: 15, LineHeight: 22, Color: f.p.colors().text}, t)
	})
	f.y += max(d.Size.Y, gtx.Dp(22))
}
