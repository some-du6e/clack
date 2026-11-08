package ui

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"time"

	"github.com/some-du6e/yoloslackgoclient/internal/ui/composer"
)

// prefs are UI-only settings persisted to <config>/ui.json (no message
// content: routes hold only ids).
type prefs struct {
	Dark          *bool    `json:"dark,omitempty"`
	SidebarW      int      `json:"sidebar_w,omitempty"`
	SecondaryW    int      `json:"secondary_w,omitempty"`
	Route         string   `json:"route,omitempty"`
	LastChannel   string   `json:"last_channel,omitempty"`
	Notifications *bool    `json:"notifications,omitempty"`
	Collapsed     []string `json:"collapsed,omitempty"`
	EmojiStyle    string   `json:"emoji_style,omitempty"`
	SidebarHidden bool     `json:"sidebar_hidden,omitempty"`
	// NoUpdateCheck turns off the GitHub Releases check (Preferences >
	// Advanced); UpdateDismissed is the release whose banner was closed.
	NoUpdateCheck   bool   `json:"no_update_check,omitempty"`
	UpdateDismissed string `json:"update_dismissed,omitempty"`
	// NoUpdateDownload turns off "Download updates automatically" (on by
	// default): a newer release then only shows its release page.
	NoUpdateDownload bool `json:"no_update_download,omitempty"`
	// Realtime is the live-updates choice (Slack's websocket): nil until
	// the user answers the first-run banner, which means off (realtime.go).
	Realtime *bool `json:"realtime,omitempty"`
	// ThemeSync is Appearance's "Sync with OS setting" (Slack keeps it in
	// localStorage, per device). nil: on while Dark was never chosen.
	ThemeSync *bool `json:"theme_sync,omitempty"`
	// PlainText is Preferences > Advanced > "Format messages with markup"
	// (Slack's plain_text_mode); nil follows the pref Slack sent.
	PlainText *bool `json:"plain_text_mode,omitempty"`
}

func (a *App) prefsPath() string {
	if a.opts.ConfigDir == "" {
		return ""
	}
	return filepath.Join(a.opts.ConfigDir, "ui.json")
}

func (a *App) loadPrefs() {
	p := a.prefsPath()
	if p == "" {
		return
	}
	b, err := os.ReadFile(p)
	if err != nil {
		return
	}
	if json.Unmarshal(b, &a.prefs) != nil {
		return
	}
	a.updatesOff.Store(a.prefs.NoUpdateCheck)
	if a.prefs.Dark != nil {
		a.th.SetDark(*a.prefs.Dark)
	}
	if w := a.prefs.SidebarW; w >= 160 && w <= 480 {
		a.sidebar.width = w
	}
	if w := a.prefs.SecondaryW; w >= 300 {
		a.secondary.width = w
	}
	if len(a.prefs.Collapsed) > 0 {
		a.sidebar.collapsed = map[string]bool{}
		for _, id := range a.prefs.Collapsed {
			a.sidebar.collapsed[id] = true
		}
	}
	a.lastChannel = a.prefs.LastChannel
	a.th.EmojiStyle = a.prefs.EmojiStyle
	if a.prefs.PlainText != nil {
		composer.SetPlainTextMode(*a.prefs.PlainText)
	}
	a.sidebar.hidden = a.prefs.SidebarHidden
	if a.prefs.Route != "" {
		a.router.Push(Route(a.prefs.Route))
	}
}

// savePrefs writes prefs synchronously (cheap, tiny file).
func (a *App) savePrefs() {
	p := a.prefsPath()
	if p == "" {
		return
	}
	d := a.th.Dark
	a.prefs.Dark = &d
	a.prefs.SidebarW = a.sidebar.width
	a.prefs.SecondaryW = a.secondary.width
	if r := a.router.Current(); r.Page() != "search" { // search queries stay in memory
		a.prefs.Route = string(r)
	}
	a.prefs.LastChannel = a.lastChannel
	a.prefs.SidebarHidden = a.sidebar.hidden
	a.prefs.Collapsed = a.prefs.Collapsed[:0]
	for id, c := range a.sidebar.collapsed {
		if c {
			a.prefs.Collapsed = append(a.prefs.Collapsed, id)
		}
	}
	b, _ := json.MarshalIndent(a.prefs, "", "  ")
	_ = os.MkdirAll(filepath.Dir(p), 0o700)
	tmp := p + ".tmp"
	if os.WriteFile(tmp, b, 0o600) == nil {
		_ = os.Rename(tmp, p)
	}
}

// savePrefsSoon coalesces saves after navigation.
func (a *App) savePrefsSoon() {
	if a.prefsDirty {
		return
	}
	a.prefsDirty = true
	time.AfterFunc(2*time.Second, func() {
		a.Post(func() {
			a.prefsDirty = false
			a.savePrefs()
		})
	})
}

func (a *App) notifyOn() bool {
	return a.prefs.Notifications == nil || *a.prefs.Notifications
}

func (a *App) toggleNotify() {
	v := !a.notifyOn()
	a.prefs.Notifications = &v
	a.savePrefs()
}

// plainTextOn is "Format messages with markup" (Slack's plain_text_mode):
// off by default, so the composer formats markup live.
func (a *App) plainTextOn() bool { return composer.PlainTextMode(a.Core()) }

// togglePlainText flips it through setUserPrefByApi (like emoji_style),
// applied locally and kept in ui.json.
func (a *App) togglePlainText() {
	v := !a.plainTextOn()
	a.dispatch("setUserPrefByApi", map[string]any{"pref": "plain_text_mode", "value": v},
		func(ctx context.Context, args map[string]any) (any, error) {
			a.Post(func() {
				composer.SetPlainTextMode(v)
				a.prefs.PlainText = &v
				a.savePrefs()
				a.Invalidate()
			})
			return nil, nil
		})
}
