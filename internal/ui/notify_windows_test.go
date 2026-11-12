package ui

import (
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
)

// Clack's own toasts on the windows-applog job (CLACK_WINDOWS_TOAST_TESTS=1):
// the real notifier (WinRT, under a scratch app id registered as Clack
// registers its own) shows a fakeslack DM's toast; UI Automation clicks it
// in the shell, and Clack opens the DM; a reply typed into a toast's
// box is sent to the fake. CLACK_TOAST_SHOTS names a folder for
// screenshots.

const uiToastUIA = `param([string]$Mode, [string]$Name, [string]$Text = '', [int]$Secs = 10)
Add-Type -AssemblyName UIAutomationClient
Add-Type -AssemblyName UIAutomationTypes
$A = [System.Windows.Automation.AutomationElement]
$TS = [System.Windows.Automation.TreeScope]
$All = [System.Windows.Automation.Condition]::TrueCondition
$root = $A::RootElement
$deadline = (Get-Date).AddSeconds($Secs)
while ((Get-Date) -lt $deadline) {
  foreach ($el in $root.FindAll($TS::Descendants, $All)) {
    $n = $el.Current.Name
    if ($Mode -eq 'setvalue') {
      if ($el.Current.ControlType.ProgrammaticName -ne 'ControlType.Edit') { continue }
      if (-not (($n -like ("*" + $Name + "*")) -or ($el.Current.HelpText -like ("*" + $Name + "*")))) { continue }
      $pat = $null
      if ($el.TryGetCurrentPattern([System.Windows.Automation.ValuePattern]::Pattern, [ref]$pat)) { $pat.SetValue($Text); "set '" + $n + "'"; exit 0 }
      continue
    }
    $match = if ($Mode -eq 'invoke') { $n -eq $Name } else { $n -like ("*" + $Name + "*") }
    if (-not $match) { continue }
    $pat = $null
    if ($el.TryGetCurrentPattern([System.Windows.Automation.InvokePattern]::Pattern, [ref]$pat)) { "invoked " + $el.Current.ControlType.ProgrammaticName + " '" + $n + "'"; $pat.Invoke(); exit 0 }
  }
  Start-Sleep -Milliseconds 400
}
"not found: " + $Name
foreach ($w in $root.FindAll($TS::Children, $All)) {
  "[" + $w.Current.ClassName + "] '" + $w.Current.Name + "'"
  if ($w.Current.ClassName -like '*CoreWindow*') {
    $i = 0
    foreach ($e in $w.FindAll($TS::Descendants, $All)) {
      "    " + $e.Current.ControlType.ProgrammaticName + " '" + $e.Current.Name + "' help '" + $e.Current.HelpText + "'"
      $i++; if ($i -gt 200) { break }
    }
  }
}
exit 1
`

func uiUIA(t *testing.T, mode, name, text string, secs int) (string, bool) {
	t.Helper()
	script := filepath.Join(t.TempDir(), "uia.ps1")
	if err := os.WriteFile(script, []byte(uiToastUIA), 0o644); err != nil {
		t.Fatal(err)
	}
	b, err := exec.Command("powershell", "-NoProfile", "-NonInteractive", "-ExecutionPolicy", "Bypass", "-File", script,
		"-Mode", mode, "-Name", name, "-Text", text, "-Secs", fmt.Sprint(secs)).CombinedOutput()
	return strings.TrimSpace(string(b)), err == nil
}

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
	a, ca, fs := fakeApp(t, map[string]any{"mute_sounds": true})
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

	// a DM: its toast reaches Notification Center under its tag
	msg := &core.Message{TS: "1762520099.000300", User: "U0FAKEUSER1", Name: "Ada", Text: "hello " + nonce}
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

	// the reply box (a banner shows it; Notification Center may not)
	reply := "on my way " + nonce
	if out, ok := uiUIA(t, "setvalue", "Reply", reply, 6); ok {
		t.Logf("UI Automation: %s", out)
		if out, ok := uiUIA(t, "invoke", "➤", "", 4); ok {
			t.Logf("UI Automation: %s", out)
			select {
			case f := <-posted:
				if f.Get("channel") != "D0NOTE0001" || f.Get("text") != reply || f.Get("thread_ts") != "" {
					t.Errorf("the toast's reply posted %v", f)
				}
			case <-time.After(15 * time.Second):
				t.Errorf("the reply typed into the toast wasn't sent")
			}
		} else {
			t.Logf("UI Automation couldn't press the reply's ➤:\n%s", out)
		}
	} else {
		t.Logf("UI Automation found no reply box (Do not disturb on: no banner):\n%s", firstLines(out, 40))
	}

	// a second DM: clicked in Notification Center, Clack opens it
	msg2 := &core.Message{TS: "1762520101.000500", User: "U0FAKEUSER1", Name: "Ada", Text: "open me " + nonce}
	a.data.handle(a, core.Event{Type: "message", Channel: "D0NOTE0002", TS: msg2.TS, ConvKind: "im", Msg: msg2})
	time.Sleep(1500 * time.Millisecond)
	a.drain()
	uiNotificationCenter(t, true)
	uiScreenshot(t, "clack-dm-notification-center")
	out, ok := uiUIA(t, "invokelike", "open me "+nonce, "", 10)
	t.Logf("UI Automation:\n%s", firstLines(out, 60))
	if !ok {
		t.Fatalf("UI Automation couldn't click the DM's toast in Notification Center")
	}
	deadline = time.Now().Add(10 * time.Second)
	for a.router.Current().Channel() != "D0NOTE0002" {
		if time.Now().After(deadline) {
			t.Fatalf("after the click Clack shows %q, want the DM D0NOTE0002", a.router.Current())
		}
		time.Sleep(50 * time.Millisecond)
		a.drain()
	}
	uiNotificationCenter(t, false)
}

// firstLines is the first n lines of s.
func firstLines(s string, n int) string {
	ls := strings.SplitN(s, "\n", n+1)
	if len(ls) > n {
		ls = ls[:n]
	}
	return strings.Join(ls, "\n")
}
