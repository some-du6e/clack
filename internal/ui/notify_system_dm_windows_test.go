package ui

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/some-du6e/clack/assets/icon"
	"github.com/some-du6e/clack/internal/desktop"
	"github.com/some-du6e/clack/internal/desktop/uiatest"
	"github.com/some-du6e/clack/internal/fakeslack"
)

// TestWindowsToastSystemDM: the system DM's welcome post, over the socket,
// reaches Notification Center as Slack's toast: titled "New message from
// Slack" with the blocks' text, and neither the post's username nor its
// image's alt text anywhere in it (CLACK_WINDOWS_TOAST_TESTS: the
// windows-applog job, with the other TestWindowsToast tests).
func TestWindowsToastSystemDM(t *testing.T) {
	if os.Getenv("CLACK_WINDOWS_TOAST_TESTS") == "" {
		t.Skip("set CLACK_WINDOWS_TOAST_TESTS=1 to show real toasts (the windows-applog job)")
	}
	e, fs, _, _ := sysDMEnv(t)
	a := e.a
	id := fmt.Sprintf("%s.sysdm%d", desktop.AppUserModelID, time.Now().UnixNano()%1e9)
	if _, err := desktop.RegisterAppUserModelID(id, "Clack", filepath.Join(t.TempDir(), "clack.png"), icon.PNG128); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		_ = desktop.RemoveToasts(id, "", "")
		_ = desktop.UnregisterAppUserModelID(id)
	})
	a.notify = &toastNotifier{aumid: id, cache: t.TempDir()}
	nonce := fmt.Sprint(time.Now().UnixNano() % 1e6)
	section := "A synthetic welcome note " + nonce + ": pick the channels to join."
	ts := sysPost(fs, sysWelcomeHeader+" "+nonce, sysWelcomeBlocksWith(fs, section))
	tag := a.messageNote(fakeslack.SystemDMID, ts, "").ID
	deadline := time.Now().Add(15 * time.Second)
	for {
		e.frame()
		h, _ := desktop.ToastHistory(id)
		found := false
		for _, r := range h {
			found = found || r.Tag == tag
		}
		if found {
			break
		}
		if time.Now().After(deadline) {
			t.Fatalf("the system DM's toast (tag %s) isn't in Notification Center: %v", tag, h)
		}
		time.Sleep(200 * time.Millisecond)
	}
	time.Sleep(800 * time.Millisecond)
	uiNotificationCenter(t, true)
	t.Cleanup(func() { uiNotificationCenter(t, false) })
	uiScreenshot(t, "clack-system-dm-notification-center")
	has := func(text string) func(uiatest.Element) bool {
		return func(e uiatest.Element) bool { return strings.Contains(e.Name, text) || strings.Contains(e.Help, text) }
	}
	if _, err := uiatest.Find(has(section), 10*time.Second); err != nil {
		t.Fatalf("the system DM's toast isn't in Notification Center: %v\n%s", err, uiatest.Dump(60))
	}
	if e, err := uiatest.Find(has("New message from Slack"), 5*time.Second); err != nil {
		t.Errorf("no toast titled \"New message from Slack\" in Notification Center: %v\n%s", err, uiatest.Dump(60))
	} else {
		t.Logf("titled: %s", e)
	}
	for _, s := range []string{fakeslack.SystemDMUsername, sysWelcomeAlt} {
		if e, err := uiatest.Find(has(s), 2*time.Second); err == nil {
			t.Errorf("Notification Center shows %q: %s", s, e)
		}
	}
}
