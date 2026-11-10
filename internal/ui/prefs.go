package ui

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"sync"
	"time"

	"github.com/some-du6e/clack/internal/ui/composer"
	"github.com/some-du6e/clack/internal/ui/uifont"
	"github.com/some-du6e/clack/internal/ui/uiguard"
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
	// Advanced).
	NoUpdateCheck bool `json:"no_update_check,omitempty"`
	// NoUpdateDownload turns off "Download updates automatically" (on by
	// default): a newer release then only shows its release page.
	NoUpdateDownload bool `json:"no_update_download,omitempty"`
	// UpdateCheckedAt is when GitHub last answered an update check, and
	// UpdateLimitedUntil when its rate limit ends (unix seconds): a
	// restart waits out both (update_check.go).
	UpdateCheckedAt    int64 `json:"update_checked_at,omitempty"`
	UpdateLimitedUntil int64 `json:"update_limited_until,omitempty"`
	// Realtime is the live-updates choice (Slack's websocket): nil until
	// the user answers the first-run banner, which means off (realtime.go).
	Realtime *bool `json:"realtime,omitempty"`
	// ThemeSync is Appearance's "Sync with OS setting" (Slack keeps it in
	// localStorage, per device). nil: on while Dark was never chosen.
	ThemeSync *bool `json:"theme_sync,omitempty"`
	// PlainText is Preferences > Advanced > "Format messages with markup"
	// (Slack's plain_text_mode); nil follows the pref Slack sent.
	PlainText *bool `json:"plain_text_mode,omitempty"`
	// ComposerUpArrow is Accessibility > "Press ↑ in the empty message
	// field to:" (Slack's localStorage composerUpArrowShortcutBehavior):
	// 0 unset, 1 focus the message list, 2 only when empty, 3 edit.
	ComposerUpArrow int `json:"composer_up_arrow_shortcut,omitempty"`
	// Zoom is View > Zoom's level, -3…5 (Slack's zoomLevel; macmenu.go).
	Zoom int `json:"zoom,omitempty"`
	// ClientTypeface is Appearance > Font: Slack's option object, kept
	// per device as Slack keeps it in localConfig "client-typeface"
	// (prefs_font.go). nil is Lato.
	ClientTypeface *uifont.Option `json:"client_typeface,omitempty"`
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

// savePrefs snapshots prefs on the UI goroutine and writes ui.json in the
// background (prefsSaver).
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
	a.prefsSave.save(p, b)
}

// flushPrefs waits until ui.json is written (exit, tests).
func (a *App) flushPrefs() { a.prefsSave.flush() }

// prefsSaver writes ui.json off the UI goroutine: savePrefs runs on every
// Preferences click, and a slow disk (a sleeping drive, a busy iCloud or
// antivirus scan) must not hold the frame. The latest bytes win.
type prefsSaver struct {
	mu      sync.Mutex
	path    string
	pending []byte
	busy    bool
	done    chan struct{} // closed when the writer goes idle
}

func (s *prefsSaver) save(path string, b []byte) {
	s.mu.Lock()
	if !s.busy && !uiguard.Marked() {
		// no window (headless tests and tools): nothing to keep responsive
		s.mu.Unlock()
		writePrefsFile(path, b)
		return
	}
	s.path, s.pending = path, b
	if s.busy {
		s.mu.Unlock()
		return
	}
	s.busy = true
	s.done = make(chan struct{})
	s.mu.Unlock()
	go s.loop()
}

func (s *prefsSaver) loop() {
	for {
		s.mu.Lock()
		b, p := s.pending, s.path
		s.pending = nil
		if b == nil {
			s.busy = false
			close(s.done)
			s.mu.Unlock()
			return
		}
		s.mu.Unlock()
		writePrefsFile(p, b)
	}
}

func (s *prefsSaver) flush() {
	s.mu.Lock()
	busy, d := s.busy, s.done
	s.mu.Unlock()
	if busy {
		<-d
	}
}

func writePrefsFile(p string, b []byte) {
	uiguard.OffUI("ui.json write")
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

// setComposerUpArrow is setComposerUpArrowShortcutBehavior: stored on this
// device (ui.json) and read by every composer.
func (a *App) setComposerUpArrow(v int) {
	a.prefs.ComposerUpArrow = v
	a.savePrefs()
	a.Invalidate()
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
