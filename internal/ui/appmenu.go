package ui

import (
	"image"
	"runtime"

	"gioui.org/layout"

	"github.com/some-du6e/clack/internal/ui/menubar"
	"github.com/some-du6e/clack/internal/ui/shortcut"
)

// The app menu on Linux and Windows. Slack desktop draws it there from the
// same Electron menu template as the macOS menu bar (app.asar,
// dist/main.bundle.cjs: buildFileMenu … buildHelpMenu), opened from the
// window chrome's app-menu button, which the web client seats in the top
// nav (AppMenuButton in the boot-render chunk: window-chrome-app-menu in
// .p-ia4_top_nav__native_ui_spacer, 26px wide, 8px from the left). Clack
// draws that button and opens its macMenus model as an in-app menu: each
// top-level menu a submenu, the macOS-only items (the app menu's Services
// and Hide, AppKit roles, the Window menu) left out, and the app menu's
// Preferences, Sign Out and Quit in File, as Slack's template has them
// outside macOS.

// troubleshootingMenu is Help > Troubleshooting (Slack's hj: Report
// Issue…, the hardware acceleration toggle, Show Logs in File Manager,
// the net and verbose logs, Copy Installation ID, Clear Cache and Restart,
// Reset App Data…), the items Clack has: the logs, and in place of Copy
// Installation ID its own Copy Diagnostics and freeze reports.
func (a *App) troubleshootingMenu(on func(func()) func()) menubar.Item {
	return menubar.Item{ID: "menuitem-troubleshooting", Title: "Troubleshooting", Sub: []menubar.Item{
		{ID: "menuitem-logs", Title: "Show Logs in File Manager", Do: a.showLogs},
		{ID: "menuitem-freeze-reports", Title: "Show Freeze Reports", Do: a.showFreezeReports},
		{ID: "menuitem-copy-diagnostics", Title: "Copy Diagnostics", Do: on(a.copyDiagnostics)},
	}}
}

// appMenuItems is the Linux and Windows app menu: macMenus without the
// macOS-only parts, as in-app menu items, with the menus' current state
// (greyed items, the sidebar item's and Sign Out's titles).
func (a *App) appMenuItems() []MenuItem {
	st := a.menuState(a.menuKeyNow())
	menus := a.macMenus()
	var app []menubar.Item
	var out []MenuItem
	for _, m := range menus {
		switch m.Kind {
		case "app":
			for _, it := range m.Items {
				switch it.ID {
				case "menuitem-preferences", "menuitem-sign-out", "menuitem-quit":
					app = append(app, menubar.Sep, it)
				}
			}
			continue
		case "window": // Minimize, Zoom, Bring All to Front: AppKit's
			continue
		}
		items := m.Items
		if m.Title == "File" {
			items = append(append([]menubar.Item(nil), items...), app...)
		}
		sub := appMenuConvert(menubar.Clean([]menubar.Menu{{Items: items}}), st)
		if len(sub) > 0 {
			out = append(out, MenuItem{Key: "appmenu-" + m.Title, Label: m.Title, Sub: sub})
		}
	}
	return out
}

// appMenuConvert turns a cleaned menu's items into MenuItems: AppKit
// roles and the recent views left out, separators kept between items.
func appMenuConvert(menus []menubar.Menu, st *menubar.State) []MenuItem {
	if len(menus) == 0 {
		return nil
	}
	var walk func(items []menubar.Item) []MenuItem
	walk = func(items []menubar.Item) []MenuItem {
		var out []MenuItem
		for _, it := range items {
			switch {
			case it.Sep:
				if it.Recent || st.Hidden[it.ID] || len(out) == 0 || out[len(out)-1].Divider {
					continue
				}
				out = append(out, MenuItem{Divider: true})
				continue
			case it.Role != "", st.Hidden[it.ID]:
				continue
			}
			title := it.Title
			if t, ok := st.Titles[it.ID]; ok {
				title = t
			}
			mi := MenuItem{Key: it.ID, Label: title, Disabled: st.Disabled[it.ID]}
			if it.Sub != nil {
				mi.Sub = walk(it.Sub)
				if len(mi.Sub) == 0 {
					continue
				}
			} else {
				if it.Do == nil {
					continue
				}
				do := it.Do
				switch it.ID {
				case "menuitem-logs", "menuitem-freeze-reports":
					mi.Do = func() { go do() } // they stat folders: off the UI goroutine
				default:
					mi.Do = do
				}
				mi.Hint = appMenuAccel(it)
			}
			out = append(out, mi)
		}
		for len(out) > 0 && out[len(out)-1].Divider {
			out = out[:len(out)-1]
		}
		return out
	}
	return walk(menus[0].Items)
}

// appMenuAccel is an item's shortcut as this OS shows it ("Ctrl+Shift+K").
func appMenuAccel(it menubar.Item) string {
	if it.Key == "" {
		return ""
	}
	s := ""
	for _, m := range []struct {
		mod  menubar.Mods
		name string
	}{{menubar.Ctrl, "Ctrl+"}, {menubar.Option, "Alt+"}, {menubar.Shift, "Shift+"}, {menubar.Cmd, "Mod+"}} {
		if it.Mods&m.mod != 0 {
			s += m.name
		}
	}
	return shortcut.Format(s + it.Key)
}

// hasAppMenuButton reports whether the top nav has the app-menu button:
// Linux and Windows (macOS has the menu bar).
func hasAppMenuButton() bool { return runtime.GOOS != "darwin" }

// appMenuButton draws the button at the top nav's left (8px in, 26x26,
// vertically centred in h) and opens the app menu under it.
func (a *App) appMenuButton(gtx layout.Context, h int) {
	if !hasAppMenuButton() {
		return
	}
	c := &a.th.C
	b := gtx.Dp(26)
	org := image.Pt(gtx.Dp(8), (h-b)/2)
	btn := a.clicks.get("nav.appmenu")
	at(gtx, org, image.Pt(b, b), func(gtx layout.Context) layout.Dimensions {
		return a.chromeButton(gtx, org, btn, "menu", 26, 18, c.ChromeText, 4)
	})
	if clicked(btn) {
		a.menu(image.Pt(org.X, org.Y+b+gtx.Dp(4)), a.appMenuItems())
	}
}
