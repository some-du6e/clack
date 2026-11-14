package ui

import (
	"fmt"
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
	"github.com/some-du6e/clack/internal/slack"
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
// DM. CLACK_TOAST_SHOTS names a folder for screenshots.

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
	a, ca, _, _ := noteApp(t)
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

	// Chrome's layout: no reply box on the banner, when one shows (Do not
	// disturb hides banners)
	if e, err := uiatest.Find(func(e uiatest.Element) bool {
		return strings.Contains(e.Name, "Reply") || strings.Contains(e.Help, "Reply")
	}, 3*time.Second); err == nil {
		t.Errorf("the toast has a reply box (%s); Chrome's has none", e)
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

// TestWindowsToastPrivateChannel: a private channel's message, through
// maybeNotify, reaches Notification Center titled with the channel's
// bare name (getDisplayNameForChannel), and the channel's id shows
// nowhere in it. The channel is core's and not in the sidebar's rows yet,
// where Clack's title used to be the sender's ("New message from Ada").
func TestWindowsToastPrivateChannel(t *testing.T) {
	if os.Getenv("CLACK_WINDOWS_TOAST_TESTS") == "" {
		t.Skip("set CLACK_WINDOWS_TOAST_TESTS=1 to show real toasts (the windows-applog job)")
	}
	a, ca, _, _ := noteApp(t)
	id := fmt.Sprintf("%s.priv%d", desktop.AppUserModelID, time.Now().UnixNano()%1e9)
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
	const ch = "C0NOTEPRIV1"
	name := "secret-plans-" + nonce
	ca.S.PutConv(slack.Conversation{ID: ch, Name: name, IsGroup: true, IsPrivate: true, IsMember: true})
	msg := noteMsg("1762520199.000100", "", "private "+nonce)
	a.data.handle(a, core.Event{Type: "message", Channel: ch, TS: msg.TS, ConvKind: "private", Msg: msg, Mention: true})
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
			t.Fatalf("the private channel's toast (tag %s) isn't in Notification Center: %v", tag, h)
		}
		time.Sleep(200 * time.Millisecond)
	}
	time.Sleep(800 * time.Millisecond)
	uiNotificationCenter(t, true)
	t.Cleanup(func() { uiNotificationCenter(t, false) })
	uiScreenshot(t, "clack-private-notification-center")
	has := func(text string) func(uiatest.Element) bool {
		return func(e uiatest.Element) bool { return strings.Contains(e.Name, text) || strings.Contains(e.Help, text) }
	}
	if _, err := uiatest.Find(has("private "+nonce), 10*time.Second); err != nil {
		t.Fatalf("the private channel's toast isn't in Notification Center: %v\n%s", err, uiatest.Dump(60))
	}
	if e, err := uiatest.Find(has("New message in "+name), 5*time.Second); err != nil {
		t.Errorf("no toast titled %q in Notification Center: %v\n%s", "New message in "+name, err, uiatest.Dump(60))
	} else {
		t.Logf("titled: %s", e)
	}
	if e, err := uiatest.Find(has(ch), 2*time.Second); err == nil {
		t.Errorf("Notification Center shows the channel id: %s", e)
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

// TestWindowsToastShots: a DM's toast and a channel message's in Chrome's
// layout (the title, the body, the workspace line, Clack's icon), and a
// huddle invite's (Dismiss, Join), as Windows draws them, light and dark,
// as banners and in Notification Center.
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
	tn := &toastNotifier{aumid: id, cache: t.TempDir()}
	for _, dark := range []bool{false, true} {
		mode := map[bool]string{false: "light", true: "dark"}[dark]
		setWindowsTheme(t, dark)
		dm := a.messageNote("D0NOTE0001", "1762520099.00030"+map[bool]string{false: "1", true: "2"}[dark], "")
		dm.Title, dm.Body, dm.Workspace = "New message from Ada", "are we still on for lunch?", "Fake Workspace"
		if err := tn.NotifyNote(dm); err != nil {
			t.Fatal(err)
		}
		time.Sleep(1500 * time.Millisecond)
		uiScreenshot(t, "toast-dm-"+mode)
		// Windows shows one banner at a time: each goes before the next
		_ = desktop.RemoveToasts(id, "", "")
		time.Sleep(700 * time.Millisecond)
		chm := a.messageNote("C0NOTE0001", "1762520100.00040"+map[bool]string{false: "1", true: "2"}[dark], "")
		chm.Title, chm.Workspace = "New message in #clack", "Fake Workspace"
		chm.Body = "Ada: the notification now looks like the one Chrome shows for web Slack: the title, up to four lines of " +
			"the message, and the workspace underneath, with no header line and no reply box. This sentence runs long on purpose."
		if err := tn.NotifyNote(chm); err != nil {
			t.Fatal(err)
		}
		time.Sleep(1500 * time.Millisecond)
		uiScreenshot(t, "toast-channel-"+mode)
		_ = desktop.RemoveToasts(id, "", "")
		time.Sleep(700 * time.Millisecond)
		inv := a.huddleNote(huddleNotificationFor("D0NOTE0001"))
		inv.Body, inv.Workspace = "Ada is inviting you to a huddle", "Fake Workspace"
		if err := tn.NotifyNote(inv); err != nil {
			t.Fatal(err)
		}
		time.Sleep(1500 * time.Millisecond)
		uiScreenshot(t, "toast-huddle-"+mode)
		if err := tn.NotifyNote(dm); err != nil { // all three, for Notification Center
			t.Fatal(err)
		}
		if err := tn.NotifyNote(chm); err != nil {
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
