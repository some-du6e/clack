package ui

// Every Preferences page Slack has (Availability … Advanced), and Clack's
// own tab after them (Preferences > Clack), drawn over
// the whole client signed in to internal/fakeslack at 1440x900, light and
// dark, a shot per screenful of each page (prefs-audit-shots workflow).
// PREFS_AUDIT_SHOTS_DIR gets the PNGs; without it the test only lays the
// pages out. Synthetic data only: nothing reaches Slack.

import (
	"image"
	"image/png"
	"os"
	"path/filepath"
	"strconv"
	"testing"
	"time"

	"gioui.org/gpu/headless"

	"github.com/some-du6e/clack/internal/core"
	"github.com/some-du6e/clack/internal/fakeslack"
	"github.com/some-du6e/clack/internal/slack"
)

func TestPrefsAuditShots(t *testing.T) {
	fs, err := fakeslack.Start(fakeslack.Small)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { fs.Close() })
	fs.SetPrefs(map[string]any{"vip_users": "U0VIP1"})
	home := t.TempDir()
	t.Setenv("CLACK_HOME", home)
	// gap-w16: an app with workflow accounts (one domain-wide), an
	// unverified email and its code flow, all answered here
	w16 := newW16Hooks(fs)
	w16.answer("apps.auth.external.userAuths.get", gdriveAuths(tok("Et1", "me@example.com", "user_oauth"), tok("Et2", "it@example.com", "dwa_user")))
	w16.answer("users.emailVerificationStatus.get", map[string]any{"ok": true, "is_verified": false})
	w16.answer("signup.confirmEmail", map[string]any{"ok": true})
	w16.answer("signin.confirmCode", map[string]any{"ok": false, "error": "failed"})
	w16.answer("users.listIgnoredUsers", map[string]any{"ok": true, "emails": []string{"spam@example.com"}})
	t.Cleanup(func() { fs.SetHook(nil) })
	e := startEnv(t, fs, home)
	// gap-w18: Advanced's runFromTray row as Windows draws it (a tray
	// icon runs there; Linux has none and greys the row)
	e.a.SetTray(&fakeTray{})
	dir := os.Getenv("PREFS_AUDIT_SHOTS_DIR")
	if dir != "" && e.gpu == nil {
		w, err := headless.NewWindow(e.size.X, e.size.Y)
		if err != nil {
			t.Fatalf("headless window: %v", err)
		}
		e.gpu = w
	}
	e.waitReady()
	shot := func(name string) {
		e.settle(150 * time.Millisecond)
		if dir == "" || e.gpu == nil {
			return
		}
		img := image.NewRGBA(image.Rectangle{Max: e.size})
		if err := e.gpu.Screenshot(img); err != nil {
			t.Fatalf("screenshot: %v", err)
		}
		f, err := os.Create(filepath.Join(dir, name+".png"))
		if err != nil {
			t.Fatal(err)
		}
		defer f.Close()
		if err := png.Encode(f, img); err != nil {
			t.Fatal(err)
		}
	}
	var pm *prefsModal
	prefsTap = func(p *prefsModal) { pm = p }
	t.Cleanup(func() { prefsTap = nil })

	// a page's screenfuls: its own scroll, a screen (the page area is
	// 603dp high) at a time
	pages := func(name string) {
		shot(name)
		for i := 2; i <= 8; i++ {
			s := &pm.scroll
			vh := e.a.px(560)
			if s.contentH <= s.y+e.a.px(603) {
				return
			}
			s.y += vh
			shot(name + "-" + strconv.Itoa(i))
		}
	}
	for _, dark := range []bool{false, true} {
		mode := map[bool]string{false: "light", true: "dark"}[dark]
		e.a.setDark(dark)
		e.settle(200 * time.Millisecond)
		tabs := append(append([]struct{ id, label, icon, slackIcon string }(nil), prefsNav...), struct{ id, label, icon, slackIcon string }{id: clackTabID})
		for _, it := range tabs {
			for e.a.dismissTop() {
			}
			pm = nil
			e.a.showPrefs(it.id)
			e.until("Preferences "+it.id, 5*time.Second, func() bool { return pm != nil })
			pages("prefs-" + it.id + "-" + mode)
			if it.id == "appearance" {
				pm.themeTab, pm.scroll.y = "custom", 0
				pages("prefs-appearance-custom-" + mode)
			}
			if it.id == "messages_media" {
				// Theme > Compact: the examples turn into CompactMessageExample
				pm.setPref("messages_theme", "dense")
				e.until("Compact", 5*time.Second, func() bool { return pm.prefStr("messages_theme") == "dense" })
				pm.scroll.y = 0
				shot("prefs-messages_media-compact-" + mode)
				pm.setPref("messages_theme", "light_with_avatars")
				e.until("Clean", 5*time.Second, func() bool { return pm.prefStr("messages_theme") == "light_with_avatars" })
			}
		}
		for e.a.dismissTop() {
		}
		// gap-w19: Notifications' Windows block (WindowsAppearancePref) as
		// Windows draws it, then with "Slack’s built-in notifications"
		// picked (Notification position and Display)
		winAppearanceAnyOS = true
		pm = nil
		e.a.showPrefs("notifications")
		e.until("Preferences notifications (Windows)", 5*time.Second, func() bool { return pm != nil })
		pm.scroll.y = 1 << 20 // the end; layout clamps it
		shot("prefs-notifications-windows-" + mode)
		e.a.Post(func() { e.a.setDesktopSetting(func(u *prefs) { u.NotificationMethod = "html" }) })
		pm.scroll.y = 1 << 20
		shot("prefs-notifications-windows-html-" + mode)
		e.a.Post(func() { e.a.setDesktopSetting(func(u *prefs) { u.NotificationMethod = "" }) })
		e.settle(100 * time.Millisecond)
		winAppearanceAnyOS = false
		for e.a.dismissTop() {
		}
		w16Shots(t, e, shot, mode, func() *prefsModal { return pm })
		for e.a.dismissTop() {
		}
		// Linux and Windows: the app menu from the top nav's button
		// (appmenu.go), with Help's Troubleshooting submenu
		e.a.menu(image.Pt(e.a.px(8), e.a.px(38)), e.a.appMenuItems())
		shot("appmenu-" + mode)
		var help []MenuItem
		for _, it := range e.a.appMenuItems() {
			if it.Label == "Help" {
				help = it.Sub
			}
		}
		for e.a.dismissTop() {
		}
		e.a.menu(image.Pt(e.a.px(8), e.a.px(38)), help)
		shot("appmenu-help-" + mode)
		for e.a.dismissTop() {
		}
		for _, it := range help {
			if it.Label == "Troubleshooting" {
				e.a.menu(image.Pt(e.a.px(8), e.a.px(38)), it.Sub)
			}
		}
		shot("appmenu-troubleshooting-" + mode)
		for e.a.dismissTop() {
		}
	}
}

// w16Shots are Connected accounts' manage-accounts modal and its
// disconnect confirmation, and Privacy's VerifyEmailModal in each state.
func w16Shots(t *testing.T, e *perfEnv, shot func(string), mode string, pm func() *prefsModal) {
	t.Helper()
	e.a.showPrefs("connected_accounts")
	e.until("Connected accounts", 5*time.Second, func() bool { return pm() != nil && pm().lists.connDone })
	p := pm()
	m := p.openManageAccounts("A0GDRIVE")
	if m == nil {
		t.Fatal("no Google Drive row")
	}
	shot("prefs-connected-manage-" + mode)
	ap, _ := p.connectedApp("A0GDRIVE")
	p.confirmDisconnect(m, ap, "Google", core.WorkflowToken{ID: "Et1", ExternalUserID: "me@example.com", AuthModel: "user_oauth"}, false)
	shot("prefs-connected-disconnect-" + mode)
	for e.a.dismissTop() {
	}

	e.a.showPrefs("privacy_visibility")
	e.until("Privacy", 5*time.Second, func() bool { return pm() != nil && pm().lists.blockDone })
	p = pm()
	p.sendVerificationEmail("me@example.com")
	e.until("VerifyEmailModal", 5*time.Second, func() bool { return p.verify != nil && p.verify.modal != nil })
	v := p.verify
	shot("prefs-verify-email-" + mode)
	v.paste(0, "AB1")
	shot("prefs-verify-email-typing-" + mode)
	// the rest of the code, as typing the sixth cell submits it: fakeslack
	// answers signin.confirmCode with failed
	v.paste(3, "2CD")
	v.changed()
	e.until("the invalid code", 5*time.Second, func() bool { return v.status == codeError })
	shot("prefs-verify-email-invalid-" + mode)
	v.confirmed(&slack.Error{Code: "expired"})
	shot("prefs-verify-email-expired-" + mode)
	v.confirmed(&slack.Error{Code: "ratelimited"})
	shot("prefs-verify-email-ratelimited-" + mode)
	v.status = codeProcessing
	v.paste(0, "AB12CD")
	shot("prefs-verify-email-checking-" + mode)
	v.confirmed(nil)
	shot("prefs-verify-email-verified-" + mode)
	v.modal.Close() // Got It
	e.until("the blocked list", 5*time.Second, func() bool { return p.lists.blockDone })
	shot("prefs-privacy-after-verify-" + mode)
}
