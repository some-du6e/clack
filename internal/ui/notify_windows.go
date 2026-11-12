package ui

import (
	"context"
	"crypto/md5"
	"encoding/hex"
	"log"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"github.com/some-du6e/clack/assets/icon"
	"github.com/some-du6e/clack/internal/desktop"
	"github.com/some-du6e/clack/internal/imgcache"
)

// toastNotifier shows Slack's Windows toasts (notify_toast.go) through
// WinRT, as Clack's app id (internal/desktop).
type toastNotifier struct {
	once  sync.Once
	cache string // the pictures' folder: Slack's %TEMP%\Notification Cache, Clack's own
	aumid string
	fetch func(ctx context.Context, url string) ([]byte, error)
}

func newNotifier() notifier {
	return &toastNotifier{aumid: desktop.AppUserModelID, fetch: imgcache.PublicFetch}
}

// toastAvatarDir is the folder for the toasts' pictures (Slack's is
// "${temp}/Notification Cache", which Slack desktop empties at start).
func toastAvatarDir() string { return filepath.Join(os.TempDir(), "Clack Notification Cache") }

// ready registers Clack's app id, which Windows needs before it shows a
// toast (desktop.EnsureAppUserModelID), and empties the pictures' folder
// as the shell does at start. Once, before the first toast.
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
			t.cache = toastAvatarDir()
		}
		if es, err := os.ReadDir(t.cache); err == nil {
			for _, e := range es {
				if !e.IsDir() && strings.HasSuffix(e.Name(), ".png") {
					_ = os.Remove(filepath.Join(t.cache, e.Name()))
				}
			}
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

// NotifyNote shows n's toast: createToastNotification's XML with the
// sender's picture downloaded first (downloadImages), its id as the
// toast's tag and its group id as the group (Electron's Notification id
// and groupId), and its clicks back to n.Activated.
func (t *toastNotifier) NotifyNote(n *desktopNote) error {
	t.ready()
	avatar := t.avatarFile(n.Avatar)
	if n.ID == "" {
		n.ID = shellNoteID(n.Team, n.Msg)
	}
	act := n.Activated
	err := desktop.ShowToast(t.aumid, desktop.Toast{XML: n.toastXML(avatar), Tag: n.ID, Group: n.GroupID, Inputs: []string{"replyBox"},
		Activated: func(args string, in map[string]string) {
			a, ok := parseToastActivation(args, in["replyBox"])
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

// avatarFile is downloadImages for avatarImage: the picture saved as
// <md5 of its URL>.png in the pictures' folder (an unpackaged app's
// toast can't load a web address), after verifyImageSources' host check.
// "" when there is none or it didn't download; the toast then shows
// Clack's icon.
func (t *toastNotifier) avatarFile(raw string) string {
	if raw == "" || !toastImageAllowed(raw) || t.fetch == nil {
		return ""
	}
	sum := md5.Sum([]byte(raw))
	p := filepath.Join(t.cache, hex.EncodeToString(sum[:])+".png")
	if st, err := os.Stat(p); err == nil && st.Size() > 0 {
		return filepath.ToSlash(p)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	b, err := t.fetch(ctx, raw)
	if err != nil || len(b) == 0 {
		return ""
	}
	if err := os.MkdirAll(t.cache, 0o700); err != nil {
		return ""
	}
	if err := os.WriteFile(p, b, 0o600); err != nil {
		return ""
	}
	return filepath.ToSlash(p)
}
