package ui

import (
	"context"
	"fmt"
	"net/url"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/some-du6e/clack/assets/icon"
	"github.com/some-du6e/clack/internal/core"
	"github.com/some-du6e/clack/internal/desktop"
	"github.com/some-du6e/clack/internal/desktop/uiatest"
	"github.com/some-du6e/clack/internal/ui/huddleui"
)

// huddleNotificationFor is an invite's notification as huddleui makes it
// with desktop_notification_actions unassigned: Dismiss and Join.
func huddleNotificationFor(ch string) huddleui.Notification {
	return huddleui.Notification{Channel: ch, User: "U0FAKEUSER1", Call: "R0FAKECALL1", Buttons: []huddleui.NotifyAction{
		{Key: "Dismiss", Label: "Dismiss"}, {Key: "Join", Label: "Join", URI: "slack://huddle?id=" + ch + "&team=T0FAKE", Do: func() {}}}}
}

// Clack's own toasts on the windows-applog job (CLACK_WINDOWS_TOAST_TESTS=1):
// the real notifier (WinRT, under a scratch app id registered as Clack
// registers its own) shows a fakeslack DM's toast; UI Automation
// (internal/desktop/uiatest) clicks it in the shell, and Clack opens the
// DM; a reply typed into a toast's box is sent to the fake.
// CLACK_TOAST_SHOTS names a folder for screenshots.

func uiNotificationCenter(t *testing.T, open bool) {
	t.Helper()
	cmd := exec.Command("cmd", "/c", "start", "", "ms-actioncenter:")
	if !open {
		cmd = exec.Command("powershell", "-NoProfile", "-NonInteractive", "-Command",
			`Add-Type -AssemblyName System.Windows.Forms; [System.Windows.Forms.SendKeys]::SendWait('{ESC}')`)
	}
	_ = cmd.Run()
	time.Sleep(2 * time.Second)
}

func uiScreenshot(t *testing.T, name string) {
	t.Helper()
	dir := os.Getenv("CLACK_TOAST_SHOTS")
	if dir == "" {
		return
	}
	_ = os.MkdirAll(dir, 0o755)
	out := filepath.Join(dir, name+".png")
	ps := `Add-Type -AssemblyName System.Windows.Forms, System.Drawing; $b = [System.Windows.Forms.SystemInformation]::VirtualScreen; ` +
		`$bmp = New-Object System.Drawing.Bitmap $b.Width, $b.Height; $g = [System.Drawing.Graphics]::FromImage($bmp); ` +
		`$g.CopyFromScreen($b.Left, $b.Top, 0, 0, $bmp.Size); $bmp.Save('` + out + `', [System.Drawing.Imaging.ImageFormat]::Png)`
	if err := exec.Command("powershell", "-NoProfile", "-NonInteractive", "-Command", ps).Run(); err != nil {
		t.Logf("screenshot %s: %v", name, err)
	}
}

func TestWindowsToastOpensConversation(t *testing.T) {
	if os.Getenv("CLACK_WINDOWS_TOAST_TESTS") == "" {
		t.Skip("set CLACK_WINDOWS_TOAST_TESTS=1 to show real toasts (the windows-applog job)")
	}
	a, ca, fs, _ := noteApp(t)
	posted := make(chan url.Values, 4)
	fs.OnPost = func(f url.Values, _ map[string]any) { posted <- f }
	id := fmt.Sprintf("%s.uici%d", desktop.AppUserModelID, time.Now().UnixNano()%1e9)
	if _, err := desktop.RegisterAppUserModelID(id, "Clack", filepath.Join(t.TempDir(), "clack.png"), icon.PNG128); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		_ = desktop.RemoveToasts(id, "", "")
		_ = desktop.UnregisterAppUserModelID(id)
	})
	a.notify = &toastNotifier{aumid: id, cache: t.TempDir()}
	team := storeTeam(ca)
	nonce := fmt.Sprint(time.Now().UnixNano() % 1e6)
	open := false
	t.Cleanup(func() {
		if open {
			uiNotificationCenter(t, false)
		}
	})
	named := func(text string) func(uiatest.Element) bool {
		return func(e uiatest.Element) bool { return strings.Contains(e.Name, text) }
	}

	// a DM: its toast reaches Notification Center under its tag (the
	// shell's id, team_ts)
	msg := noteMsg("1762520099.000300", "", "hello "+nonce)
	a.data.handle(a, core.Event{Type: "message", Channel: "D0NOTE0001", TS: msg.TS, ConvKind: "im", Msg: msg})
	tag := team + "_" + msg.TS
	deadline := time.Now().Add(10 * time.Second)
	for {
		a.drain()
		h, _ := desktop.ToastHistory(id)
		found := false
		for _, r := range h {
			found = found || r.Tag == tag
		}
		if found {
			break
		}
		if time.Now().After(deadline) {
			t.Fatalf("the DM's toast (tag %s) isn't in Notification Center: %v", tag, h)
		}
		time.Sleep(200 * time.Millisecond)
	}
	time.Sleep(800 * time.Millisecond)
	uiScreenshot(t, "clack-dm-banner")

	// the reply box, when a banner shows it (Do not disturb hides banners)
	reply := "on my way " + nonce
	if e, err := uiatest.SetValue(func(e uiatest.Element) bool {
		return strings.Contains(e.Name, "Reply") || strings.Contains(e.Help, "Reply")
	}, reply, 6*time.Second); err == nil {
		t.Logf("typed into %s", e)
		if e, err := uiatest.Act(func(e uiatest.Element) bool { return e.Name == "➤" }, 4*time.Second); err == nil {
			t.Logf("clicked %s", e)
			// the click comes back through App.Post, which the frame loop
			// runs: drain while waiting
			deadline := time.Now().Add(15 * time.Second)
		wait:
			for {
				a.drain()
				select {
				case f := <-posted:
					if f.Get("channel") != "D0NOTE0001" || f.Get("text") != reply || f.Get("thread_ts") != "" {
						t.Errorf("the toast's reply posted %v", f)
					}
					break wait
				case <-time.After(50 * time.Millisecond):
				}
				if time.Now().After(deadline) {
					t.Errorf("the reply typed into the toast wasn't sent")
					break
				}
			}
		} else {
			t.Logf("UI Automation couldn't press the reply's ➤: %v", err)
		}
	} else {
		t.Logf("no reply box on the screen (%v):\n%s", err, uiatest.Dump(40))
	}

	// a second DM: clicked in Notification Center, Clack opens it
	msg2 := noteMsg("1762520101.000500", "", "open me "+nonce)
	a.data.handle(a, core.Event{Type: "message", Channel: "D0NOTE0002", TS: msg2.TS, ConvKind: "im", Msg: msg2})
	time.Sleep(1500 * time.Millisecond)
	a.drain()
	uiNotificationCenter(t, true)
	open = true
	uiScreenshot(t, "clack-dm-notification-center")
	e, err := uiatest.Act(named("open me "+nonce), 10*time.Second)
	if err != nil {
		t.Fatalf("UI Automation couldn't click the DM's toast in Notification Center: %v\n%s", err, uiatest.Dump(60))
	}
	t.Logf("clicked %s", e)
	deadline = time.Now().Add(10 * time.Second)
	for a.router.Current().Channel() != "D0NOTE0002" {
		if time.Now().After(deadline) {
			t.Fatalf("after the click Clack shows %q, want the DM D0NOTE0002", a.router.Current())
		}
		time.Sleep(50 * time.Millisecond)
		a.drain()
	}
	uiNotificationCenter(t, false)
	open = false
}

// firstLines is the first n lines of s.
func firstLines(s string, n int) string {
	ls := strings.SplitN(s, "\n", n+1)
	if len(ls) > n {
		ls = ls[:n]
	}
	return strings.Join(ls, "\n")
}

// setWindowsTheme switches the shell between light and dark (the
// Personalize keys Settings writes) and tells it so (WM_SETTINGCHANGE
// "ImmersiveColorSet").
func setWindowsTheme(t *testing.T, dark bool) {
	t.Helper()
	v := "1"
	if dark {
		v = "0"
	}
	ps := `$k = 'HKCU:\Software\Microsoft\Windows\CurrentVersion\Themes\Personalize'; ` +
		`New-Item -Path $k -Force | Out-Null; ` +
		`Set-ItemProperty -Path $k -Name AppsUseLightTheme -Type DWord -Value ` + v + `; ` +
		`Set-ItemProperty -Path $k -Name SystemUsesLightTheme -Type DWord -Value ` + v + `; ` +
		`Add-Type -Namespace W -Name U -MemberDefinition '[DllImport("user32.dll", CharSet=CharSet.Unicode)] public static extern IntPtr SendMessageTimeout(IntPtr h, uint m, UIntPtr w, string l, uint f, uint t, out UIntPtr r);'; ` +
		`$r = [UIntPtr]::Zero; [W.U]::SendMessageTimeout([IntPtr]0xffff, 0x1A, [UIntPtr]::Zero, 'ImmersiveColorSet', 2, 5000, [ref]$r) | Out-Null`
	if b, err := exec.Command("powershell", "-NoProfile", "-NonInteractive", "-Command", ps).CombinedOutput(); err != nil {
		t.Logf("theme: %v %s", err, b)
	}
	time.Sleep(2 * time.Second)
}

// TestWindowsToastShots: a DM's toast (the sender's picture, the header,
// the reply box) and a huddle invite's (Dismiss, Join) as Windows draws
// them, light and dark, as banners and in Notification Center.
func TestWindowsToastShots(t *testing.T) {
	if os.Getenv("CLACK_WINDOWS_TOAST_TESTS") == "" || os.Getenv("CLACK_TOAST_SHOTS") == "" {
		t.Skip("set CLACK_WINDOWS_TOAST_TESTS=1 and CLACK_TOAST_SHOTS (the windows-applog job)")
	}
	a, _, _, _ := noteApp(t)
	id := fmt.Sprintf("%s.shots%d", desktop.AppUserModelID, time.Now().UnixNano()%1e9)
	if _, err := desktop.RegisterAppUserModelID(id, "Clack", filepath.Join(t.TempDir(), "clack.png"), icon.PNG128); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		_ = desktop.RemoveToasts(id, "", "")
		_ = desktop.UnregisterAppUserModelID(id)
		setWindowsTheme(t, false)
	})
	// the sender's picture: Clack's icon standing in for a Slack CDN avatar
	tn := &toastNotifier{aumid: id, cache: t.TempDir(), fetch: func(_ context.Context, raw string) ([]byte, error) {
		return icon.PNG128, nil
	}}
	for _, dark := range []bool{false, true} {
		mode := map[bool]string{false: "light", true: "dark"}[dark]
		setWindowsTheme(t, dark)
		dm := a.messageNote("D0NOTE0001", "1762520099.00030"+map[bool]string{false: "1", true: "2"}[dark], "", "")
		dm.Avatar = "https://avatars.slack-edge.com/ci/ada_192.png"
		dm.Title, dm.Body, dm.ChannelName, dm.Grouped, dm.Workspace = "New message from Ada", "are we still on for lunch?", "Ada", true, "Fake Workspace"
		if err := tn.NotifyNote(dm); err != nil {
			t.Fatal(err)
		}
		time.Sleep(1500 * time.Millisecond)
		uiScreenshot(t, "toast-dm-"+mode)
		// Windows shows one banner at a time: the DM's goes first
		_ = desktop.RemoveToasts(id, "", "")
		time.Sleep(700 * time.Millisecond)
		inv := a.huddleNote(huddleNotificationFor("D0NOTE0001"))
		inv.Body, inv.Workspace = "Ada is inviting you to a huddle", "Fake Workspace"
		inv.Avatar = "https://avatars.slack-edge.com/ci/ada_192.png"
		if err := tn.NotifyNote(inv); err != nil {
			t.Fatal(err)
		}
		time.Sleep(1500 * time.Millisecond)
		uiScreenshot(t, "toast-huddle-"+mode)
		if err := tn.NotifyNote(dm); err != nil { // both, for Notification Center
			t.Fatal(err)
		}
		time.Sleep(500 * time.Millisecond)
		uiNotificationCenter(t, true)
		uiScreenshot(t, "toast-notification-center-"+mode)
		uiNotificationCenter(t, false)
		_ = desktop.RemoveToasts(id, "", "")
		time.Sleep(700 * time.Millisecond)
	}
}
