package desktop

import (
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/some-du6e/clack/assets/icon"
	"github.com/some-du6e/clack/internal/desktop/uiatest"
)

// Real toasts on the windows-applog job (CLACK_WINDOWS_TOAST_TESTS=1):
// what Windows does with an app id nobody registered, with the key Clack
// writes, and with a click on a toast's button (UI Automation invokes it
// in the shell's toast window). CLACK_TOAST_SHOTS names a folder for
// screenshots of the desktop while the toasts show.

func toastTests(t *testing.T) {
	t.Helper()
	if os.Getenv("CLACK_WINDOWS_TOAST_TESTS") == "" {
		t.Skip("set CLACK_WINDOWS_TOAST_TESTS=1 to show real toasts (the windows-applog job)")
	}
}

var scratchSeq atomic.Int64

// scratchAUMID is an app id for one test, removed with its toasts after.
func scratchAUMID(t *testing.T, register bool) string {
	t.Helper()
	id := fmt.Sprintf("%s.ci%d.%d", AppUserModelID, time.Now().UnixNano()%1e9, scratchSeq.Add(1))
	t.Cleanup(func() {
		_ = RemoveToasts(id, "", "")
		_ = UnregisterAppUserModelID(id)
	})
	if register {
		png := filepath.Join(t.TempDir(), "clack.png")
		if _, err := RegisterAppUserModelID(id, "Clack CI", png, icon.PNG128); err != nil {
			t.Fatalf("registering %s: %v", id, err)
		}
		if !AppUserModelIDRegistered(id) {
			t.Fatalf("%s isn't registered after RegisterAppUserModelID", id)
		}
	}
	return id
}

func ciToastXML(title, body, actions string) string {
	return `<toast activationType="foreground" launch="body-click"><visual><binding template="ToastGeneric">` +
		`<text hint-wrap="false" hint-maxLines="1">` + title + `</text>` +
		`<text hint-maxLines="10" hint-style="bodySubtle" hint-wrap="true">` + body + `</text>` +
		`</binding></visual><audio silent="true" />` + actions + `</toast>`
}

// inHistory waits up to d for tag to be among id's toasts in
// Notification Center.
func inHistory(t *testing.T, id, tag string, d time.Duration) ([]ToastRef, bool) {
	t.Helper()
	deadline := time.Now().Add(d)
	for {
		h, err := ToastHistory(id)
		if err != nil {
			t.Logf("ToastHistory(%s): %v", id, err)
		}
		for _, r := range h {
			if r.Tag == tag {
				return h, true
			}
		}
		if time.Now().After(deadline) {
			return h, false
		}
		time.Sleep(200 * time.Millisecond)
	}
}

func screenshot(t *testing.T, name string) {
	t.Helper()
	dir := os.Getenv("CLACK_TOAST_SHOTS")
	if dir == "" {
		return
	}
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Log(err)
		return
	}
	out := filepath.Join(dir, name+".png")
	ps := `Add-Type -AssemblyName System.Windows.Forms, System.Drawing; ` +
		`$b = [System.Windows.Forms.SystemInformation]::VirtualScreen; ` +
		`$bmp = New-Object System.Drawing.Bitmap $b.Width, $b.Height; ` +
		`$g = [System.Drawing.Graphics]::FromImage($bmp); ` +
		`$g.CopyFromScreen($b.Left, $b.Top, 0, 0, $bmp.Size); ` +
		`$bmp.Save('` + out + `', [System.Drawing.Imaging.ImageFormat]::Png)`
	if b, err := exec.Command("powershell", "-NoProfile", "-NonInteractive", "-Command", ps).CombinedOutput(); err != nil {
		t.Logf("screenshot %s: %v %s", name, err, b)
		return
	}
	t.Logf("screenshot: %s", out)
}

// notificationCenter opens the shell's Notification Center (the
// ms-actioncenter: link), or closes it with Esc.
func notificationCenter(t *testing.T, open bool) {
	t.Helper()
	var cmd *exec.Cmd
	if open {
		cmd = exec.Command("cmd", "/c", "start", "", "ms-actioncenter:")
	} else {
		cmd = exec.Command("powershell", "-NoProfile", "-NonInteractive", "-Command",
			`Add-Type -AssemblyName System.Windows.Forms; [System.Windows.Forms.SendKeys]::SendWait('{ESC}')`)
	}
	if b, err := cmd.CombinedOutput(); err != nil {
		t.Logf("notification center (open %t): %v %s", open, err, b)
	}
	time.Sleep(2 * time.Second)
}

// TestToastInNotificationCenter: what Windows does with a toast from an
// app id nobody registered and from one with Clack's registration, as
// Notification Center shows them (UI Automation reads it); clicks there
// (the toast, then a button) reaching this process; then, with Do not
// disturb (on on the runner) turned off, a banner's button.
func TestToastInNotificationCenter(t *testing.T) {
	toastTests(t)
	nonce := fmt.Sprint(time.Now().UnixNano() % 1e6)
	bare := scratchAUMID(t, false)
	reg := scratchAUMID(t, true)
	if bare == reg {
		t.Fatalf("the scratch ids are the same: %s", bare)
	}
	bareFailed := make(chan error, 1)
	if err := ShowToast(bare, Toast{XML: ciToastXML("Clack CI", "Unknown id "+nonce, ""), Tag: "bare", Group: "ci",
		Failed: func(err error) { bareFailed <- err }}); err != nil {
		t.Logf("unregistered: Show: %v", err)
	}
	got := make(chan string, 8)
	activated := func(a string, _ map[string]string) { got <- a }
	if err := ShowToast(reg, Toast{XML: ciToastXML("Clack CI", "Clack id "+nonce, ""), Tag: "reg", Group: "ci",
		Activated: activated}); err != nil {
		t.Fatalf("registered: Show: %v", err)
	}
	for _, c := range []struct{ id, tag string }{{bare, "bare"}, {reg, "reg"}} {
		h, ok := inHistory(t, c.id, c.tag, 5*time.Second)
		t.Logf("%s: in the history API: %t (%v)", c.id, ok, h)
	}
	t.Logf("unregistered: Failed: %v", pending(bareFailed))

	notificationCenter(t, true)
	screenshot(t, "notification-center")
	t.Logf("UI Automation, with Notification Center open:\n%s", uiatest.Dump(60))
	named := func(text string) func(uiatest.Element) bool {
		return func(e uiatest.Element) bool { return strings.Contains(e.Name, text) }
	}
	if e, err := uiatest.Find(named("Clack id "+nonce), 10*time.Second); err != nil {
		t.Errorf("Notification Center doesn't show the registered id's toast: %v", err)
	} else {
		t.Logf("the registered id's toast: %s", e)
	}
	// the control: the same toast from an id Windows doesn't know is in the
	// history API but nowhere the user sees it
	if e, err := uiatest.Find(named("Unknown id "+nonce), 3*time.Second); err == nil {
		t.Errorf("Notification Center shows the unregistered id's toast (%s): the registration wouldn't be what makes Windows show Clack's", e)
	} else {
		t.Logf("CONTROL: Windows dropped the unregistered id's toast (in the history API, not in Notification Center)")
	}
	// a click on the toast in Notification Center: its launch arguments
	if e, err := uiatest.Act(named("Clack id "+nonce), 10*time.Second); err != nil {
		t.Errorf("UI Automation couldn't click the toast in Notification Center: %v", err)
	} else {
		t.Logf("clicked %s", e)
		wantActivated(t, got, "body-click", "a click on the toast in Notification Center")
	}
	notificationCenter(t, false)

	// Do not disturb off, for banners: its switch in Notification Center
	// (not the taskbar's bell, which only opens the panel)
	notificationCenter(t, true)
	dnd, err := uiatest.Act(func(e uiatest.Element) bool {
		return strings.Contains(strings.ToLower(e.Name), "do not disturb") && e.Window != ""
	}, 5*time.Second)
	t.Logf("Do not disturb: %s %v", dnd, err)
	notificationCenter(t, false)
	if err != nil {
		t.Errorf("UI Automation couldn't turn Do not disturb off: %v", err)
		return
	}
	label := "Open banner " + nonce
	actions := `<actions><action activationType="system" arguments="dismiss" content="Dismiss"/>` +
		`<action activationType="foreground" arguments="banner-click" content="` + label + `"/></actions>`
	if err := showOncePlatformUp(t, reg, Toast{XML: ciToastXML("Clack CI", "a banner "+nonce, actions), Tag: "banner", Group: "ci",
		Activated: activated}, 30*time.Second); err != nil {
		t.Fatalf("banner: Show: %v", err)
	}
	time.Sleep(1500 * time.Millisecond)
	screenshot(t, "banner")
	e, err := uiatest.Act(func(e uiatest.Element) bool { return e.Name == label }, 10*time.Second)
	if err != nil {
		t.Errorf("UI Automation couldn't click %q on the banner: %v\n%s", label, err, uiatest.Dump(60))
		return
	}
	t.Logf("clicked %s", e)
	wantActivated(t, got, "banner-click", "a click on the banner's button")
}

// The notification platform's errors while its per-user service
// (WpnUserService_*) restarts: WPN_E_PLATFORM_UNAVAILABLE, and
// RPC_S_CALL_FAILED from a call that was in flight when it went.
const (
	hrPlatformUnavailable = 0x803e0105
	hrRPCCallFailed       = 0x800706be
)

// showOncePlatformUp is ShowToast, tried again with backoff (up to d)
// while the notification platform is down. On the Server 2025 runner,
// switching Do not disturb crashes svchost.exe_WpnUserService (Application
// Error 1000, then the service manager's 7031, restarting it after 1, 2,
// 4, 8 s), and Show fails until it is back: 0.6 to 8.2 s after the switch
// (flake-check's toast-1113 probe). The kept notifier works again then.
// Windows 11 doesn't crash (the probe on windows-11-arm: 4 switches, no
// failed Show, the same service pid), so Clack's own path doesn't retry.
func showOncePlatformUp(t *testing.T, aumid string, toast Toast, d time.Duration) error {
	t.Helper()
	start, wait := time.Now(), 250*time.Millisecond
	for n := 1; ; n++ {
		err := ShowToast(aumid, toast)
		var hr HResultError
		if err == nil || !errors.As(err, &hr) || (hr != hrPlatformUnavailable && hr != hrRPCCallFailed) || time.Since(start)+wait > d {
			if n > 1 {
				t.Logf("Show: %d tries over %v while the notification platform came back: %v", n, time.Since(start).Round(time.Millisecond), err)
			}
			return err
		}
		time.Sleep(wait)
		wait = min(2*wait, 2*time.Second)
	}
}

// wantActivated waits for an Activated event with args.
func wantActivated(t *testing.T, got chan string, args, what string) {
	t.Helper()
	select {
	case a := <-got:
		if a != args {
			t.Errorf("%s: Activated with %q, want %q", what, a, args)
		}
	case <-time.After(10 * time.Second):
		t.Errorf("%s raised no Activated event", what)
	}
}

// firstLines is the first n lines of s.
func firstLines(s string, n int) string {
	ls := strings.SplitN(s, "\n", n+1)
	if len(ls) > n {
		ls = ls[:n]
	}
	return strings.Join(ls, "\n")
}

// TestToastInstalledAUMID: Clack's own id as the installer registered it
// (the workflow installs Clack first and sets CLACK_TOAST_INSTALLED=1):
// its toast is in Notification Center.
func TestToastInstalledAUMID(t *testing.T) {
	toastTests(t)
	if os.Getenv("CLACK_TOAST_INSTALLED") == "" {
		t.Skip("set CLACK_TOAST_INSTALLED=1 after installing Clack")
	}
	if !AppUserModelIDRegistered(AppUserModelID) {
		t.Fatalf("%s isn't registered: the installer didn't write HKCU\\%s", AppUserModelID, aumidKey(AppUserModelID))
	}
	icon, err := AUMIDIconPath()
	if err != nil {
		t.Fatal(err)
	}
	if st, err := os.Stat(icon); err != nil || st.Size() == 0 {
		t.Errorf("the registration's icon %s: %v", icon, err)
	}
	s, err := ToastSetting(AppUserModelID)
	t.Logf("Setting %d (err %v)", s, err)
	nonce := fmt.Sprint(time.Now().UnixNano() % 1e6)
	defer RemoveToasts(AppUserModelID, "installed", "ci")
	if err := ShowToast(AppUserModelID, Toast{XML: ciToastXML("Clack", "installed "+nonce, ""), Tag: "installed", Group: "ci"}); err != nil {
		t.Fatalf("Show: %v", err)
	}
	time.Sleep(1500 * time.Millisecond)
	screenshot(t, "installed-banner")
	notificationCenter(t, true)
	screenshot(t, "installed-notification-center")
	if e, err := uiatest.Find(func(e uiatest.Element) bool { return strings.Contains(e.Name, "installed "+nonce) }, 10*time.Second); err != nil {
		t.Errorf("Notification Center doesn't show the installed id's toast: %v\n%s", err, uiatest.Dump(60))
	} else {
		t.Logf("the installed id's toast: %s", e)
	}
	notificationCenter(t, false)
}

// pending is what ch holds now, or nil.
func pending(ch chan error) error {
	select {
	case err := <-ch:
		return err
	default:
		return nil
	}
}
