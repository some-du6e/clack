package ui

import (
	"encoding/json"
	"os"
	"path/filepath"
	"time"
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
	a.prefs.Route = string(a.router.Current())
	a.prefs.LastChannel = a.lastChannel
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
