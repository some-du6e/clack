package ui

import (
	"log"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"github.com/some-du6e/clack/assets/icon"
	"github.com/some-du6e/clack/internal/desktop"
)

// toastNotifier shows Clack's Windows toasts (notify_toast.go: Chrome's
// layout) through WinRT, as Clack's app id (internal/desktop).
type toastNotifier struct {
	once  sync.Once
	cache string // the toasts' pictures: Clack's icon, the logo Chrome's toast shows
	aumid string
	logo  string // Clack's icon in cache ("" when it couldn't be written)
}

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
		Failed: func(err error) { log.Printf("notify: Windows didn't show a toast: %v", err) },
	})
	if err != nil {
		log.Printf("notify: toast: %v", err)
	}
	return err
}
