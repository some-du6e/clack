package ui

import (
	"fmt"
	"log"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/some-du6e/clack/assets/icon"
	"github.com/some-du6e/clack/internal/desktop"
	"github.com/some-du6e/clack/internal/logtag"
)

// toastNotifier shows Clack's Windows toasts (notify_toast.go: Chrome's
// layout) through WinRT, as Clack's app id (internal/desktop).
type toastNotifier struct {
	once  sync.Once
	cache string // the toasts' pictures: Clack's icon, the logo Chrome's toast shows
	aumid string
	logo  string // Clack's icon in cache ("" when it couldn't be written)

	mu       sync.Mutex
	sent     []time.Time // the toasts of the last toastBurst (toastBurstAt)
	switches string      // Windows' notification settings for Clack as last logged (noteSwitches)
}

// toastBurst is how far back the toasts count toward a burst: Windows
// shows their banners one after another.
const toastBurst = 10 * time.Second

func newNotifier() notifier {
	return &toastNotifier{aumid: desktop.AppUserModelID}
}

// toastPicturesDir is the folder for the toasts' pictures (Chromium's
// NotificationImageRetainer keeps its in the profile; Clack's goes in the
// temp folder, as Slack desktop's "Notification Cache" did).
func toastPicturesDir() string { return filepath.Join(os.TempDir(), "Clack Notification Cache") }

// ready registers Clack's app id, which Windows needs before it shows a
// toast (desktop.EnsureAppUserModelID), empties the pictures' folder (an
// older Clack saved senders' pictures there) and writes Clack's icon for
// the toasts' logo. Once, before the first toast.
func (t *toastNotifier) ready() {
	t.once.Do(func() {
		if t.aumid == desktop.AppUserModelID {
			if changed, err := desktop.EnsureAppUserModelID(icon.PNG128); err != nil {
				log.Printf("notify: registering Clack's app id for toasts: %v", err)
			} else if changed {
				log.Printf("notify: registered Clack's app id for toasts")
			}
		}
		if t.cache == "" {
			t.cache = toastPicturesDir()
		}
		if es, err := os.ReadDir(t.cache); err == nil {
			for _, e := range es {
				if !e.IsDir() && strings.HasSuffix(e.Name(), ".png") {
					_ = os.Remove(filepath.Join(t.cache, e.Name()))
				}
			}
		}
		p := filepath.Join(t.cache, "clack.png")
		if err := os.MkdirAll(t.cache, 0o700); err == nil && os.WriteFile(p, icon.PNG128, 0o600) == nil {
			t.logo = filepath.ToSlash(p)
		}
	})
}

func (t *toastNotifier) Notify(title, body string) error {
	return t.NotifyNote(&desktopNote{Title: title, Body: body})
}

// NotifyActions is the plain notification with buttons (a caller with no
// conversation to point at): each one activates the app, and on gets its
// key.
func (t *toastNotifier) NotifyActions(title, body string, actions []string, on func(string)) error {
	n := &desktopNote{Title: title, Body: body}
	for i := 0; i+1 < len(actions); i += 2 {
		key := actions[i]
		n.Buttons = append(n.Buttons, noteButton{Content: actions[i+1], LaunchURI: "slack://noop", Do: func() {
			if on != nil {
				on(key)
			}
		}})
	}
	n.Activated = func(act toastActivation) {
		if act.Type == "action" && act.Index >= 0 && act.Index < len(n.Buttons) {
			n.Buttons[act.Index].Do()
		}
	}
	return t.NotifyNote(n)
}

// NotifyNote shows n's toast: its XML (Chrome's layout, stamped now, as a
// Notification without a timestamp is), its id as the toast's tag and its
// group id as the group (Electron's Notification id and groupId), and its
// clicks back to n.Activated.
func (t *toastNotifier) NotifyNote(n *desktopNote) error {
	t.ready()
	if n.ID == "" {
		n.ID = shellNoteID(n.Team, n.Msg)
	}
	if n.When.IsZero() {
		n.When = time.Now()
	}
	act := n.Activated
	// the decision log's "notify: show" is the rules' (notify_decisions.go);
	// whether Windows shows the toast is logged here, with its tags
	at := logtag.ID(n.Channel) + "/" + logtag.ID(n.Msg)
	sent := time.Now()
	err := desktop.ShowToast(t.aumid, desktop.Toast{XML: n.toastXML(t.logo), Tag: n.ID, Group: n.GroupID,
		Activated: func(args string, _ map[string]string) {
			a, ok := parseToastActivation(args)
			if !ok {
				log.Printf("notify: a toast's activation arguments didn't parse")
				return
			}
			if act != nil {
				act(a)
			}
		},
		Failed: func(err error) { log.Printf("notify: Windows didn't show the toast for %s: %v", at, err) },
		Dismissed: func(reason int) {
			log.Printf("notify: the toast for %s left the screen after %s: %s", at, time.Since(sent).Round(100*time.Millisecond), toastDismissWhy(reason))
		},
	})
	if err != nil {
		log.Printf("notify: the toast for %s didn't go to Windows: %v", at, err)
	}
	// Windows takes a toast it won't show, or shows without its banner:
	// say so next to the decision
	t.whyHidden(at)
	if k := t.burstAt(sent); k > 2 {
		log.Printf("notify: %d toasts in %s, the one for %s last: Windows shows their banners one after another, so its own may come late", k, toastBurst, at)
	}
	return err
}

// whyHidden logs what in Windows keeps the toast for at from showing, or
// from showing its banner (desktop notifystate.go). Slack desktop asks
// the toast notifier's setting before a toast and Focus Assist before a
// sound (checkNotificationState, main.bundle.cjs 4.53.276 win32); Clack's
// app id is unpackaged, for which that setting fails (0x80070490), so the
// switches Settings > System > Notifications writes say whether Clack's
// toasts, or every app's, are off. Then Focus Assist (Do not disturb),
// and the shell holding banners (an app full screen, presentation mode,
// the screen locked, the quiet hour after a first sign-in), which send the
// toast to the notification center unshown. Windows shows the app in
// focus its own toasts: only Clack's rules hold those (16).
func (t *toastNotifier) whyHidden(at string) {
	if s, err := desktop.ToastSetting(t.aumid); err == nil && s != desktop.ToastEnabled {
		log.Printf("notify: Windows has Clack's toasts off (%s): the toast for %s won't show", toastSettingWhy(s), at)
		return
	}
	all, app, values := desktop.ToastSwitches(t.aumid)
	t.noteSwitches(values)
	switch {
	case all == desktop.ToastSwitchOff:
		log.Printf("notify: Windows has every app's notifications off (Settings > System > Notifications): the toast for %s won't show", at)
		return
	case app == desktop.ToastSwitchOff:
		log.Printf("notify: Windows has Clack's notifications off (Settings > System > Notifications > Clack): the toast for %s won't show", at)
		return
	}
	if desktop.FocusAssistActive() {
		mode := "on"
		switch desktop.FocusAssist() {
		case desktop.FocusAssistAlarmsOnly:
			mode = "alarms only"
		case desktop.FocusAssistPriorityOnly:
			mode = "priority only, Clack not on its list"
		}
		log.Printf("notify: Focus Assist is on (%s): the toast for %s goes to the notification center unshown", mode, at)
	}
	if why := desktop.NotifyStateWhy(desktop.NotificationState()); why != "" {
		log.Printf("notify: Windows holds notification banners now (%s): the toast for %s goes to the notification center unshown", why, at)
	}
}

// noteSwitches logs Windows' notification settings for Clack (the DWORD
// values of its Notifications\Settings key: Enabled, ShowInActionCenter,
// the banners', the sound) whenever they differ from the last toast's.
func (t *toastNotifier) noteSwitches(values map[string]uint32) {
	names := make([]string, 0, len(values))
	for n := range values {
		names = append(names, n)
	}
	sort.Strings(names)
	var b strings.Builder
	for _, n := range names {
		fmt.Fprintf(&b, " %s=%d", n, values[n])
	}
	s := b.String()
	if s == "" {
		s = " none (every one on)"
	}
	t.mu.Lock()
	changed := s != t.switches
	t.switches = s
	t.mu.Unlock()
	if changed {
		log.Printf("notify: Windows' notification settings for Clack:%s", s)
	}
}

// burstAt counts the toasts of the last toastBurst, the one at now
// included.
func (t *toastNotifier) burstAt(now time.Time) int {
	t.mu.Lock()
	defer t.mu.Unlock()
	kept := t.sent[:0]
	for _, at := range t.sent {
		if now.Sub(at) < toastBurst {
			kept = append(kept, at)
		}
	}
	t.sent = append(kept, now)
	return len(t.sent)
}

// toastSettingWhy names a ToastNotifier.Setting that isn't Enabled.
func toastSettingWhy(s int) string {
	switch s {
	case desktop.ToastDisabledForApplication:
		return "off for Clack in Settings"
	case desktop.ToastDisabledForUser:
		return "off for every app in Settings"
	case desktop.ToastDisabledByGroupPolicy:
		return "off by a group policy"
	case desktop.ToastDisabledByManifest:
		return "off in the app's manifest"
	}
	return fmt.Sprintf("setting %d", s)
}

// toastDismissWhy names a ToastDismissalReason.
func toastDismissWhy(r int) string {
	switch r {
	case desktop.ToastUserCanceled:
		return "closed"
	case desktop.ToastApplicationHidden:
		return "taken back by Clack"
	case desktop.ToastTimedOut:
		return "its banner timed out (the toast stays in the notification center)"
	}
	return fmt.Sprintf("reason %d", r)
}
