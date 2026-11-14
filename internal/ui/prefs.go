package ui

import (
	"context"
	"encoding/json"
	"github.com/some-du6e/clack/internal/ui/slackc"
	"os"
	"path/filepath"
	"sync"
	"time"

	"github.com/some-du6e/clack/internal/ui/composer"
	"github.com/some-du6e/clack/internal/ui/theme"
	"github.com/some-du6e/clack/internal/ui/uifont"
	"github.com/some-du6e/clack/internal/ui/uiguard"
)

// prefs are UI-only settings persisted to <config>/ui.json (no message
// content: routes hold only ids).
type prefs struct {
	Dark *bool `json:"dark,omitempty"`
	// SidebarW and SidebarWMode are the sidebar's dragged width (dp) and
	// Slack's sidebarWidthMode ("auto", "manual"), per device as Slack
	// keeps viewLayoutSidebarWidthIntent / Mode in localStorage
	// (sidebar_width.go).
	SidebarW     int    `json:"sidebar_w,omitempty"`
	SidebarWMode string `json:"sidebar_w_mode,omitempty"`
	// SidebarWideW and SidebarWideWMode are Activity's and the DMs
	// list's column (Slack's wide sidebar): its own dragged width and
	// mode, per device like viewLayoutSidebarWideWidthIntent / Mode
	// (sidebar_wide.go).
	SidebarWideW     int    `json:"sidebar_wide_w,omitempty"`
	SidebarWideWMode string `json:"sidebar_wide_w_mode,omitempty"`
	// SidebarXWideW and SidebarXWideWMode are the extra wide column's
	// (Activity under activity_inbox; viewLayoutSidebarExtraWideWidth…).
	SidebarXWideW     int    `json:"sidebar_extra_wide_w,omitempty"`
	SidebarXWideWMode string `json:"sidebar_extra_wide_w_mode,omitempty"`
	// SecondaryWActivity and SecondaryWLater are the pane's dragged widths
	// in Activity and To-dos (Slack's …ForActivity / …ForLater intents;
	// SecondaryW is the channel views'). 0 is auto.
	SecondaryWActivity int    `json:"secondary_w_activity,omitempty"`
	SecondaryWLater    int    `json:"secondary_w_later,omitempty"`
	SecondaryW         int    `json:"secondary_w,omitempty"`
	Route              string `json:"route,omitempty"`
	LastChannel        string `json:"last_channel,omitempty"`
	Notifications      *bool  `json:"notifications,omitempty"`
	SidebarHidden      bool   `json:"sidebar_hidden,omitempty"`
	// NoUpdateCheck turns off the GitHub Releases check (Preferences >
	// Clack).
	NoUpdateCheck bool `json:"no_update_check,omitempty"`
	// NoUpdateDownload turns off "Download updates automatically" (on by
	// default): a newer release then only shows its release page.
	NoUpdateDownload bool `json:"no_update_download,omitempty"`
	// DownloadPath is Preferences > Advanced > Download location (Slack's
	// desktop setting PrefSSBFileDownloadPath; "": ~/Downloads), and
	// DownloadAsk its "Choose location for each download" (the path
	// cleared, in Slack): each download asks where with the system's
	// save dialog (downloads.go).
	DownloadPath string `json:"download_path,omitempty"`
	DownloadAsk  bool   `json:"download_ask,omitempty"`
	// UpdateCheckedAt is when GitHub last answered an update check, and
	// UpdateLimitedUntil when its rate limit ends (unix seconds): a
	// restart waits out both (update_check.go).
	UpdateCheckedAt    int64 `json:"update_checked_at,omitempty"`
	UpdateLimitedUntil int64 `json:"update_limited_until,omitempty"`
	// UpdateFound is the newer release the last check found (cleared
	// when a check finds none): Clack quit before restarting into it, so
	// the check right after launch runs even within the hour and its
	// download (or the verified copy already in the cache) is ready in
	// seconds.
	UpdateFound string `json:"update_found,omitempty"`
	// Realtime is the live-updates choice (Slack's websocket): nil until
	// the user switches it in Preferences > Clack, which means on
	// (realtime.go).
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
	// HuddleAGC and HuddleNS are Audio & video's "Enable automatic gain
	// control" (nil: on) and "Enable noise suppression" (Slack's
	// localStorage prefs.audio; huddle_av.go).
	HuddleAGC *bool `json:"huddle_agc,omitempty"`
	HuddleNS  bool  `json:"huddle_noise_suppression,omitempty"`
	// HuddleVideoEffect is the camera's background effect (Slack's
	// localStorage prefs.video.effect: "" none, "blur"; huddle_av.go).
	HuddleVideoEffect string `json:"huddle_video_effect,omitempty"`
	// Zoom is View > Zoom's level, -3…5 (Slack's zoomLevel; macmenu.go).
	Zoom int `json:"zoom,omitempty"`
	// Simplified layout mode (bbm.go). Slack keeps all of it on the
	// device, none of it in users.prefs: BBM is the client store "bbm"
	// {enabled} (persisted, plus localConfig bbmEnabled per team);
	// BBMHoverActions is bbmSettings.persistentActions=false (localStorage
	// slack-persistent-actions-bbm-setting, true when unset);
	// BBMOnboarded is slack-onboarded-setting (the welcome dialog was
	// seen); BBMZoom is slack-zoom-bbm-setting and NonBBMZoom /
	// NonBBMSidebarHidden are nonBBMSettings' zoom and isSidebarVisible,
	// put back when the mode ends.
	BBM                 bool  `json:"bbm,omitempty"`
	BBMHoverActions     bool  `json:"bbm_hover_actions,omitempty"`
	BBMOnboarded        bool  `json:"bbm_onboarded,omitempty"`
	BBMZoom             *int  `json:"bbm_zoom,omitempty"`
	NonBBMZoom          *int  `json:"non_bbm_zoom,omitempty"`
	NonBBMSidebarHidden *bool `json:"non_bbm_sidebar_hidden,omitempty"`
	// ClientTypeface is Appearance > Font: Slack's option object, kept
	// per device as Slack keeps it in localConfig "client-typeface"
	// (prefs_font.go). nil is Lato.
	ClientTypeface *uifont.Option `json:"client_typeface,omitempty"`
	// AcctTheme / AcctNoGradients are the account's ia_theme and
	// window_background_gradients=false as last seen (applyAccountPrefs),
	// for the first frame of the next start.
	AcctTheme       string `json:"acct_theme,omitempty"`
	AcctNoGradients bool   `json:"acct_no_gradients,omitempty"`
	// PluginSlots are where plugins put the components they wrap (dp
	// insets by component, slackc/reserve.go): the next start keeps that
	// space free until the plugin renders, so its first tree moves nothing.
	PluginSlots map[string]slackc.Insets `json:"plugin_slots,omitempty"`
	// SpellLanguages are Language & region's spellcheck languages on
	// Windows and Linux (the desktop app keeps them in Electron's session,
	// per device): nil is the OS locale's language.
	SpellLanguages []string `json:"spellcheck_languages,omitempty"`
	// Locale is the account's locale as last seen (Slack's localConfig
	// user_locale): the next start loads that bundle from the cache before
	// users.prefs answers (locale_wire.go).
	Locale string `json:"locale,omitempty"`
	// SplitW and SplitWMode are the split view's width (dp) and Slack's
	// splitWidthMode ("sticky", "manual", "auto"), kept per device as Slack
	// keeps viewLayoutSplitWidthIntent / Mode in localStorage (split.go).
	SplitW     int    `json:"split_w,omitempty"`
	SplitWMode string `json:"split_w_mode,omitempty"`
	// IntroSeen: the first launch's introduction ran to the sign-in card
	// (or was skipped there), so later launches go straight to sign-in.
	// Preferences > Clack > Replay introduction plays it again.
	IntroSeen bool `json:"intro_seen,omitempty"`
	// NoRunFromTray is Advanced's "Leave app running in notification area
	// when the window is closed" unticked (Slack's desktop setting
	// runFromTray, default on), and HasRunFromTray Slack's
	// hasRunFromTray: the "Why didn’t Clack quit?" balloon was shown
	// (runfromtray.go).
	NoRunFromTray  bool `json:"no_run_from_tray,omitempty"`
	HasRunFromTray bool `json:"has_run_from_tray,omitempty"`
	// ShowOnStartup is the tray's Launch Control > Show Clack
	// Immediately: Slack's desktop setting hideOnStartup false (its
	// default is true, Keep in Background: a launch on login leaves the
	// window in the notification area; tray.go).
	ShowOnStartup bool `json:"show_on_startup,omitempty"`
	// Windows' Notifications block (prefs_win_appearance.go), Slack's
	// desktop settings: WindowFlashBehavior is win32.windowFlashBehavior
	// ("never", "idle", "always"; "" the default, idle) and
	// HasExplainedWindowFlash win32.hasExplainedWindowFlash (the "always
	// flash" balloon was shown); NotificationMethod is notificationMethod
	// ("" null, "winrt", "window", "html") and NotifyCorner /
	// NotifyDisplay are notifyPosition ("" bottom_right / same_as_app).
	WindowFlashBehavior     string `json:"window_flash_behavior,omitempty"`
	HasExplainedWindowFlash bool   `json:"has_explained_window_flash,omitempty"`
	NotificationMethod      string `json:"notification_method,omitempty"`
	NotifyCorner            string `json:"notify_corner,omitempty"`
	NotifyDisplay           string `json:"notify_display,omitempty"`
	// OpenFiles is the Files sidebar's Recently viewed by team: Slack's
	// filesOpenInSessionStore in local storage (file ids only;
	// files_open.go).
	OpenFiles map[string]openFilesPrefs `json:"open_files,omitempty"`
	// InspectTool turns on the inspect tool's shortcut (inspect_wire.go),
	// like CLACK_INSPECT=1: a developer setting with no UI, set by hand.
	InspectTool bool `json:"inspect_tool,omitempty"`
	// MainWindow is Slack's mainWindowSettings: where the main window was
	// (its frame in Gio patch 25's screen coordinates: physical pixels on
	// Windows, points on macOS; on Linux its size in dp), and whether it
	// was maximized or full screen. The next launch opens it there, on a
	// display that is still connected (winbounds.go).
	MainWindow *mainWindowSettings `json:"main_window_settings,omitempty"`
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
	a.sidebar.loadWidth(a.prefs.SidebarW, a.prefs.SidebarWMode)   // sidebar_width.go
	a.sbWide.load(a.prefs.SidebarWideW, a.prefs.SidebarWideWMode) // sidebar_wide.go
	a.sbXWide.load(a.prefs.SidebarXWideW, a.prefs.SidebarXWideWMode)
	// a width the user dragged to ("manual"); builds before the auto
	// width saved their fixed 400 default, which is auto now
	if w := a.prefs.SecondaryW; w >= 300 && w != theme.SecondaryW {
		a.secondary.width = w
	}
	a.secTabs.w = map[string]int{}
	if w := a.prefs.SecondaryWActivity; w >= 300 {
		a.secTabs.w["activity"] = w
	}
	if w := a.prefs.SecondaryWLater; w >= 300 {
		a.secTabs.w["later"] = w
	}
	a.lastChannel = a.prefs.LastChannel
	if a.prefs.PlainText != nil {
		composer.SetPlainTextMode(*a.prefs.PlainText)
	}
	a.sidebar.hidden = a.prefs.SidebarHidden
	slackc.SetReserved(a.prefs.PluginSlots)
	if a.prefs.AcctTheme != "" || a.prefs.AcctNoGradients {
		a.setAcct(acctFromPrefs(a.prefs.AcctTheme, a.prefs.AcctNoGradients))
	}
	if a.prefs.BBM {
		// Simplified layout mode: a cold boot lands on the BBM tab (4P57
		// RedirectColdBootToBBM)
		a.router.Push("bbm")
	} else if a.prefs.Route != "" && !isBBMRoute(Route(a.prefs.Route)) {
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
	a.prefs.SidebarW, a.prefs.SidebarWMode = a.sidebar.intent, a.sidebar.mode
	a.prefs.SidebarWideW, a.prefs.SidebarWideWMode = a.sbWide.intent, a.sbWide.mode
	a.prefs.SidebarXWideW, a.prefs.SidebarXWideWMode = a.sbXWide.intent, a.sbXWide.mode
	a.prefs.SecondaryW = a.secTabWidth("")
	a.prefs.SecondaryWActivity, a.prefs.SecondaryWLater = a.secTabWidth("activity"), a.secTabWidth("later")
	if r := a.router.Current(); r.Page() != "search" { // search queries stay in memory
		a.prefs.Route = string(r)
	}
	a.prefs.LastChannel = a.lastChannel
	a.prefs.SidebarHidden = a.sidebar.hidden
	a.winSaveDue = false // the bytes below carry mainWindowSettings
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

// togglePlainText flips it through setUserPrefByApi,
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
