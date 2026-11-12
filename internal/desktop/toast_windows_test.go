package desktop

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
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

// scratchAUMID is an app id for one test, removed with its toasts after.
func scratchAUMID(t *testing.T, register bool) string {
	t.Helper()
	id := fmt.Sprintf("%s.ci%d", AppUserModelID, time.Now().UnixNano()%1e9)
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

// TestToastNeedsRegisteredAUMID: the registration is what makes Windows
// keep a toast. The same toast for an id nobody registered is the
// control.
func TestToastNeedsRegisteredAUMID(t *testing.T) {
	toastTests(t)
	bare := scratchAUMID(t, false)
	s, err := ToastSetting(bare)
	t.Logf("unregistered %s: Setting %d (err %v)", bare, s, err)
	bareFailed := make(chan error, 1)
	err = ShowToast(bare, Toast{XML: ciToastXML("Clack CI", "unregistered app id", ""), Tag: "bare", Group: "ci",
		Failed: func(err error) { bareFailed <- err }})
	t.Logf("unregistered: Show: %v", err)
	h, ok := inHistory(t, bare, "bare", 3*time.Second)
	t.Logf("unregistered: in Notification Center: %t (history %v, Failed: %v)", ok, h, pending(bareFailed))

	reg := scratchAUMID(t, true)
	s, err = ToastSetting(reg)
	if err != nil || s != ToastEnabled {
		t.Errorf("registered %s: Setting %d, %v; want %d (Enabled)", reg, s, err, ToastEnabled)
	}
	regFailed := make(chan error, 1)
	if err := ShowToast(reg, Toast{XML: ciToastXML("Clack CI", "registered app id", ""), Tag: "reg", Group: "ci",
		Failed: func(err error) { regFailed <- err }}); err != nil {
		t.Fatalf("registered: Show: %v", err)
	}
	time.Sleep(700 * time.Millisecond) // the banner's slide-in
	screenshot(t, "toast-registered")
	h, okReg := inHistory(t, reg, "reg", 10*time.Second)
	if !okReg {
		t.Errorf("registered %s: the toast isn't in Notification Center (history %v, Failed: %v)", reg, h, pending(regFailed))
	}
	if ok {
		t.Logf("CONTROL: Windows kept the unregistered id's toast too, so delivery alone doesn't show the registration matters")
	} else if okReg {
		t.Logf("CONTROL: Windows dropped the unregistered id's toast and kept the registered one")
	}
	if err := RemoveToasts(reg, "reg", "ci"); err != nil {
		t.Errorf("RemoveToasts: %v", err)
	}
	if h, ok := inHistory(t, reg, "reg", 0); ok {
		t.Errorf("RemoveToasts left the toast in Notification Center (%v)", h)
	}
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

// TestToastInstalledAUMID: Clack's own id as the installer registered it
// (the workflow installs Clack first and sets CLACK_TOAST_INSTALLED=1).
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
	if s, err := ToastSetting(AppUserModelID); err != nil || s != ToastEnabled {
		t.Errorf("Setting %d, %v; want Enabled", s, err)
	}
	defer RemoveToasts(AppUserModelID, "installed", "ci")
	if err := ShowToast(AppUserModelID, Toast{XML: ciToastXML("Clack", "installed: the toast shows", ""), Tag: "installed", Group: "ci"}); err != nil {
		t.Fatalf("Show: %v", err)
	}
	time.Sleep(700 * time.Millisecond)
	screenshot(t, "toast-installed")
	if h, ok := inHistory(t, AppUserModelID, "installed", 10*time.Second); !ok {
		t.Errorf("the installed id's toast isn't in Notification Center (history %v)", h)
	}
}

// uiaInvoke has UI Automation find the element named name anywhere on
// the desktop and invoke it (the shell's toast window holds the toast's
// buttons). It returns the script's output.
func uiaInvoke(t *testing.T, name string, secs int) (string, bool) {
	t.Helper()
	script := filepath.Join(t.TempDir(), "invoke.ps1")
	ps := `param([string]$Name, [int]$Secs)
Add-Type -AssemblyName UIAutomationClient
Add-Type -AssemblyName UIAutomationTypes
$A = [System.Windows.Automation.AutomationElement]
$root = $A::RootElement
$cond = New-Object System.Windows.Automation.PropertyCondition($A::NameProperty, $Name)
$deadline = (Get-Date).AddSeconds($Secs)
while ((Get-Date) -lt $deadline) {
  $el = $root.FindFirst([System.Windows.Automation.TreeScope]::Descendants, $cond)
  if ($el -ne $null) {
    $w = [System.Windows.Automation.TreeWalker]::ControlViewWalker
    $top = $el; $p = $w.GetParent($top)
    while ($p -ne $null -and -not $p.Equals($root)) { $top = $p; $p = $w.GetParent($top) }
    "found " + $el.Current.ControlType.ProgrammaticName + " '" + $el.Current.Name + "' in [" + $top.Current.ClassName + "] '" + $top.Current.Name + "', pid " + $el.Current.ProcessId + " (" + (Get-Process -Id $el.Current.ProcessId).ProcessName + ")"
    $pat = $null
    if ($el.TryGetCurrentPattern([System.Windows.Automation.InvokePattern]::Pattern, [ref]$pat)) { $pat.Invoke(); "invoked"; exit 0 }
    "no InvokePattern"; exit 2
  }
  Start-Sleep -Milliseconds 250
}
"not found; the top-level windows:"
foreach ($w in $root.FindAll([System.Windows.Automation.TreeScope]::Children, [System.Windows.Automation.Condition]::TrueCondition)) {
  "  [" + $w.Current.ClassName + "] '" + $w.Current.Name + "' pid " + $w.Current.ProcessId
}
exit 1
`
	if err := os.WriteFile(script, []byte(ps), 0o644); err != nil {
		t.Fatal(err)
	}
	b, err := exec.Command("powershell", "-NoProfile", "-NonInteractive", "-ExecutionPolicy", "Bypass", "-File", script, "-Name", name, "-Secs", fmt.Sprint(secs)).CombinedOutput()
	out := strings.TrimSpace(string(b))
	return out, err == nil
}

// TestToastActivatedByClick: invoking a toast's button in the shell
// raises the toast's Activated event in this process with the button's
// arguments (activationType="foreground", no COM activator).
func TestToastActivatedByClick(t *testing.T) {
	toastTests(t)
	id := scratchAUMID(t, true)
	label := fmt.Sprintf("Open CI %d", time.Now().UnixNano()%1e6)
	got := make(chan string, 4)
	dismissed := make(chan int, 4)
	actions := `<actions><action activationType="system" arguments="dismiss" content="Dismiss"/>` +
		`<action activationType="foreground" arguments="button-click" content="` + label + `"/></actions>`
	if err := ShowToast(id, Toast{XML: ciToastXML("Clack CI", "click test", actions), Tag: "click", Group: "ci",
		Activated: func(a string) { got <- a }, Dismissed: func(r int) { dismissed <- r }}); err != nil {
		t.Fatalf("Show: %v", err)
	}
	time.Sleep(700 * time.Millisecond)
	screenshot(t, "toast-click-before")
	out, ok := uiaInvoke(t, label, 20)
	t.Logf("UI Automation:\n%s", out)
	if !ok {
		t.Errorf("UI Automation couldn't invoke the toast's %q button", label)
		return
	}
	select {
	case a := <-got:
		if a != "button-click" {
			t.Errorf("Activated with %q, want the button's arguments %q", a, "button-click")
		}
	case r := <-dismissed:
		t.Errorf("the click dismissed the toast (reason %d) instead of activating it", r)
	case <-time.After(15 * time.Second):
		t.Errorf("no Activated event 15 s after the click")
	}
	screenshot(t, "toast-click-after")
}
