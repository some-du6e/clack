package desktop

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/some-du6e/clack/assets/icon"
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

// uiaScript is the UI Automation helper (Windows PowerShell 5.1, which
// has .NET Framework's UIAutomationClient):
//
//	invoke NAME  find the element named NAME anywhere on the desktop and
//	             invoke it (or toggle it)
//	invokelike TEXT  the same for the first element whose name contains
//	             TEXT and that can be invoked or toggled
//	has TEXT     wait for an element whose name contains TEXT
//	dump         the top-level windows, and the named elements in the
//	             shell's notification windows
const uiaScript = `param([string]$Mode, [string]$Name, [int]$Secs = 10)
Add-Type -AssemblyName UIAutomationClient
Add-Type -AssemblyName UIAutomationTypes
$A = [System.Windows.Automation.AutomationElement]
$TS = [System.Windows.Automation.TreeScope]
$All = [System.Windows.Automation.Condition]::TrueCondition
$root = $A::RootElement
function Where-Is($el) {
  $w = [System.Windows.Automation.TreeWalker]::ControlViewWalker
  $top = $el; $p = $w.GetParent($top)
  while ($p -ne $null -and -not $p.Equals($root)) { $top = $p; $p = $w.GetParent($top) }
  $pn = ''
  try { $pn = (Get-Process -Id $el.Current.ProcessId).ProcessName } catch {}
  return $el.Current.ControlType.ProgrammaticName + " '" + $el.Current.Name + "' in [" + $top.Current.ClassName + "] '" + $top.Current.Name + "' (" + $pn + ")"
}
function Dump {
  foreach ($w in $root.FindAll($TS::Children, $All)) {
    "[" + $w.Current.ClassName + "] '" + $w.Current.Name + "' pid " + $w.Current.ProcessId
    if ($w.Current.ClassName -like '*CoreWindow*' -or $w.Current.Name -like '*otification*') {
      $i = 0
      foreach ($e in $w.FindAll($TS::Descendants, $All)) {
        if ($e.Current.Name) { "    " + $e.Current.ControlType.ProgrammaticName + " '" + $e.Current.Name + "'" }
        $i++; if ($i -gt 400) { "    …"; break }
      }
    }
  }
}
$deadline = (Get-Date).AddSeconds($Secs)
switch ($Mode) {
  'invoke' {
    $c = New-Object System.Windows.Automation.PropertyCondition($A::NameProperty, $Name)
    while ((Get-Date) -lt $deadline) {
      $el = $root.FindFirst($TS::Descendants, $c)
      if ($el -ne $null) {
        "found " + (Where-Is $el)
        $pat = $null
        if ($el.TryGetCurrentPattern([System.Windows.Automation.InvokePattern]::Pattern, [ref]$pat)) { $pat.Invoke(); "invoked"; exit 0 }
        if ($el.TryGetCurrentPattern([System.Windows.Automation.TogglePattern]::Pattern, [ref]$pat)) { "toggle state before: " + $pat.Current.ToggleState; $pat.Toggle(); "toggled"; exit 0 }
        "no Invoke or Toggle pattern"; exit 2
      }
      Start-Sleep -Milliseconds 250
    }
  }
  'invokelike' {
    while ((Get-Date) -lt $deadline) {
      foreach ($el in $root.FindAll($TS::Descendants, $All)) {
        if (-not ($el.Current.Name -like ("*" + $Name + "*"))) { continue }
        $pat = $null
        if ($el.TryGetCurrentPattern([System.Windows.Automation.InvokePattern]::Pattern, [ref]$pat)) { "found " + (Where-Is $el); $pat.Invoke(); "invoked"; exit 0 }
        if ($el.TryGetCurrentPattern([System.Windows.Automation.TogglePattern]::Pattern, [ref]$pat)) { "found " + (Where-Is $el); "toggle state before: " + $pat.Current.ToggleState; $pat.Toggle(); "toggled"; exit 0 }
      }
      Start-Sleep -Milliseconds 500
    }
  }
  'has' {
    while ((Get-Date) -lt $deadline) {
      foreach ($e in $root.FindAll($TS::Descendants, $All)) {
        if ($e.Current.Name -like ("*" + $Name + "*")) { "found " + (Where-Is $e); exit 0 }
      }
      Start-Sleep -Milliseconds 500
    }
  }
  'dump' { Dump; exit 0 }
}
"not found: " + $Name
Dump
exit 1
`

// uia runs uiaScript; ok is its exit status 0.
func uia(t *testing.T, mode, name string, secs int) (string, bool) {
	t.Helper()
	script := filepath.Join(t.TempDir(), "uia.ps1")
	if err := os.WriteFile(script, []byte(uiaScript), 0o644); err != nil {
		t.Fatal(err)
	}
	b, err := exec.Command("powershell", "-NoProfile", "-NonInteractive", "-ExecutionPolicy", "Bypass", "-File", script,
		"-Mode", mode, "-Name", name, "-Secs", fmt.Sprint(secs)).CombinedOutput()
	return strings.TrimSpace(string(b)), err == nil
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
// Notification Center shows them; clicks there (the toast, then a
// button) reaching this process; then, with Do not disturb (on on the
// runner) turned off, a banner's button.
func TestToastInNotificationCenter(t *testing.T) {
	toastTests(t)
	nonce := fmt.Sprint(time.Now().UnixNano() % 1e6)
	bare := scratchAUMID(t, false)
	reg := scratchAUMID(t, true)
	if bare == reg {
		t.Fatalf("the scratch ids are the same: %s", bare)
	}
	for _, id := range []string{bare, reg} {
		s, err := ToastSetting(id)
		t.Logf("%s: Setting %d (err %v)", id, s, err)
	}
	bareFailed := make(chan error, 1)
	if err := ShowToast(bare, Toast{XML: ciToastXML("Clack CI", "unregistered app id "+nonce, ""), Tag: "bare", Group: "ci",
		Failed: func(err error) { bareFailed <- err }}); err != nil {
		t.Logf("unregistered: Show: %v", err)
	}
	got := make(chan string, 8)
	activated := func(a string, _ map[string]string) { got <- a }
	if err := ShowToast(reg, Toast{XML: ciToastXML("Clack CI", "registered app id "+nonce, ""), Tag: "reg", Group: "ci",
		Activated: activated}); err != nil {
		t.Fatalf("registered: Show: %v", err)
	}
	for _, c := range []struct{ id, tag string }{{bare, "bare"}, {reg, "reg"}} {
		h, ok := inHistory(t, c.id, c.tag, 5*time.Second)
		t.Logf("%s: in the history: %t (%v)", c.id, ok, h)
	}
	t.Logf("unregistered: Failed: %v", pending(bareFailed))

	notificationCenter(t, true)
	screenshot(t, "notification-center")
	out, _ := uia(t, "dump", "", 1)
	t.Logf("UI Automation, the desktop with Notification Center open:\n%s", out)
	out, ok := uia(t, "has", "registered app id "+nonce, 10)
	t.Logf("UI Automation, the registered toast:\n%s", firstLines(out, 3))
	if !ok {
		t.Errorf("Notification Center doesn't show the registered id's toast")
	}
	out, bareShown := uia(t, "has", "unregistered app id "+nonce, 3)
	if bareShown {
		t.Logf("CONTROL: Notification Center shows the unregistered id's toast too:\n%s", firstLines(out, 3))
	} else {
		t.Logf("CONTROL: Notification Center doesn't show the unregistered id's toast (Windows dropped it)")
	}
	// a click on the toast in Notification Center: its launch arguments
	out, ok = uia(t, "invokelike", "registered app id "+nonce, 10)
	t.Logf("UI Automation, the toast in Notification Center:\n%s", firstLines(out, 3))
	if ok {
		wantActivated(t, got, "body-click", "a click on the toast in Notification Center")
	} else {
		t.Errorf("UI Automation couldn't click the toast in Notification Center:\n%s", out)
	}
	// a button on a toast in Notification Center
	label := "Open CI " + nonce
	actions := `<actions><action activationType="system" arguments="dismiss" content="Dismiss"/>` +
		`<action activationType="foreground" arguments="button-click" content="` + label + `"/></actions>`
	if err := ShowToast(reg, Toast{XML: ciToastXML("Clack CI", "buttons "+nonce, actions), Tag: "buttons", Group: "ci",
		Activated: activated}); err != nil {
		t.Fatalf("buttons: Show: %v", err)
	}
	notificationCenter(t, false)
	notificationCenter(t, true)
	screenshot(t, "notification-center-buttons")
	out, ok = uia(t, "invoke", label, 8)
	t.Logf("UI Automation, the button in Notification Center:\n%s", firstLines(out, 3))
	if ok {
		wantActivated(t, got, "button-click", "a click on the toast's button in Notification Center")
	} else {
		t.Logf("Notification Center doesn't show the toast's buttons (a collapsed toast): the banner's are checked below")
	}

	// Do not disturb off, for banners
	out, ok = uia(t, "invokelike", "o not disturb", 5)
	t.Logf("UI Automation, Do not disturb:\n%s", firstLines(out, 4))
	notificationCenter(t, false)
	if !ok {
		t.Errorf("UI Automation couldn't turn Do not disturb off")
		return
	}
	label2 := "Open banner " + nonce
	actions2 := `<actions><action activationType="system" arguments="dismiss" content="Dismiss"/>` +
		`<action activationType="foreground" arguments="banner-click" content="` + label2 + `"/></actions>`
	if err := ShowToast(reg, Toast{XML: ciToastXML("Clack CI", "a banner "+nonce, actions2), Tag: "banner", Group: "ci",
		Activated: activated}); err != nil {
		t.Fatalf("banner: Show: %v", err)
	}
	time.Sleep(1500 * time.Millisecond)
	screenshot(t, "banner")
	out, ok = uia(t, "invoke", label2, 10)
	t.Logf("UI Automation, the banner's button:\n%s", firstLines(out, 3))
	if !ok {
		t.Errorf("UI Automation couldn't click %q on the banner:\n%s", label2, out)
		return
	}
	wantActivated(t, got, "banner-click", "a click on the banner's button")
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
	out, ok := uia(t, "has", "installed "+nonce, 10)
	t.Logf("UI Automation:\n%s", firstLines(out, 3))
	if !ok {
		t.Errorf("Notification Center doesn't show the installed id's toast")
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
