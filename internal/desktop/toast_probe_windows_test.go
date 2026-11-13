package desktop

import (
	"errors"
	"fmt"
	"os"
	"os/exec"
	"strings"
	"testing"
	"time"

	"github.com/some-du6e/clack/internal/desktop/uiatest"
)

// TestToastPlatformProbe (CLACK_WINDOWS_TOAST_PROBE=1, flake-check's
// toast-1113 jobs) measures what ToastNotifier.Show returns, every 250 ms
// for 20 s, after Do not disturb is switched in Notification Center and
// after the notification platform's per-user service (WpnUserService_*)
// restarts, as at a sign-in: with the notifier made before (what Clack,
// Electron and Chromium keep for the process's life) and with a new one
// each time. It logs the timelines and fails nothing.
func TestToastPlatformProbe(t *testing.T) {
	if os.Getenv("CLACK_WINDOWS_TOAST_PROBE") == "" {
		t.Skip("set CLACK_WINDOWS_TOAST_PROBE=1 (flake-check's toast-1113 jobs)")
	}
	id := scratchAUMID(t, true)
	if err := ShowToast(id, probeToast("before")); err != nil {
		t.Fatalf("the first Show: %v", err)
	}
	var kept *rtObject // the notifier the first Show made, kept as Clack keeps it
	_ = onRT(func() error { kept = rt.notif[id]; return nil })
	t.Logf("platform: %s", platformPIDs())
	since := time.Now()
	for i := 1; i <= 4; i++ {
		closeDND, how := switchDND(t)
		at := time.Now()
		t.Logf("toggle %d (%s), then the switch's window left open", i, how)
		probeTimeline(t, id, kept, fmt.Sprintf("toggle %d", i), at, 20*time.Second)
		closeDND()
		t.Logf("after toggle %d: %s", i, platformPIDs())
		t.Logf("events since the probe started:\n%s", wpnEvents(since))
	}
	// the platform starting: its per-user service stopped and started
	before := platformPIDs()
	out, err := exec.Command("powershell", "-NoProfile", "-NonInteractive", "-Command",
		`Get-Service -Name 'WpnUserService_*' | Restart-Service -Force -PassThru | Format-Table -AutoSize Name, Status | Out-String`).CombinedOutput()
	at := time.Now()
	t.Logf("Restart-Service WpnUserService_*: %v %s", err, strings.TrimSpace(string(out)))
	probeTimeline(t, id, kept, "service restart", at, 30*time.Second)
	t.Logf("platform before the restart: %s; after: %s", before, platformPIDs())
	// the same notifier, a minute on
	time.Sleep(30 * time.Second)
	probeTimeline(t, id, kept, "service restart +60s", time.Now(), 2*time.Second)
}

// switchDND flips Do not disturb: its button in Notification Center (as
// TestToastInNotificationCenter does), or else its switch on Settings'
// Notifications page. It returns how to close what it opened.
func switchDND(t *testing.T) (func(), string) {
	t.Helper()
	dnd := func(e uiatest.Element) bool {
		return strings.Contains(strings.ToLower(e.Name), "do not disturb") && e.Window != ""
	}
	notificationCenter(t, true)
	if e, err := uiatest.Act(dnd, 10*time.Second); err == nil {
		return func() { notificationCenter(t, false) }, "Notification Center: " + e.String()
	} else {
		t.Logf("Notification Center has no Do not disturb button (%v):\n%s", err, uiatest.Dump(80))
	}
	notificationCenter(t, false)
	if b, err := exec.Command("cmd", "/c", "start", "", "ms-settings:notifications").CombinedOutput(); err != nil {
		t.Logf("ms-settings:notifications: %v %s", err, b)
	}
	e, err := uiatest.Act(dnd, 20*time.Second)
	if err != nil {
		t.Fatalf("Settings has no Do not disturb switch either (%v):\n%s", err, uiatest.Dump(80))
	}
	return func() { _ = exec.Command("taskkill", "/IM", "SystemSettings.exe", "/F").Run() }, "Settings: " + e.String()
}

// wpnEvents: the System and Application log entries since since that
// name the notification platform (the service manager's stops, starts and
// crashes, Windows Error Reporting's).
func wpnEvents(since time.Time) string {
	ps := fmt.Sprintf(`$t = [datetime]::Parse('%s'); `+
		`Get-WinEvent -FilterHashtable @{LogName='System','Application'; StartTime=$t} -ErrorAction SilentlyContinue | `+
		`Where-Object { $_.Message -match 'Wpn|notification' } | Sort-Object TimeCreated | `+
		`ForEach-Object { "$($_.TimeCreated.ToString('HH:mm:ss.fff')) $($_.LogName)/$($_.ProviderName) $($_.Id): $(($_.Message -split "`+"`"+`n")[0..3] -join ' | ')" }`,
		since.Format("2006-01-02T15:04:05"))
	out, err := exec.Command("powershell", "-NoProfile", "-NonInteractive", "-Command", ps).CombinedOutput()
	if err != nil {
		return fmt.Sprintf("%v %s", err, out)
	}
	return strings.TrimSpace(string(out))
}

func probeToast(body string) Toast {
	return Toast{XML: ciToastXML("Clack CI probe", body, ""), Tag: "probe", Group: "ci"}
}

// probeTimeline Shows a toast every 250 ms for d, with the kept notifier
// and with a new one, and logs each run of the same result.
func probeTimeline(t *testing.T, id string, kept *rtObject, what string, start time.Time, d time.Duration) {
	t.Helper()
	type run struct {
		res      string
		from, to time.Duration
		n        int
	}
	var withKept, withNew []run
	add := func(rs *[]run, res string, at time.Duration) {
		if l := len(*rs); l > 0 && (*rs)[l-1].res == res {
			(*rs)[l-1].to, (*rs)[l-1].n = at, (*rs)[l-1].n+1
			return
		}
		*rs = append(*rs, run{res: res, from: at, to: at, n: 1})
	}
	res := func(err error) string {
		var hr HResultError
		switch {
		case err == nil:
			return "ok"
		case errors.As(err, &hr):
			return fmt.Sprintf("%#08x", uint32(hr))
		default:
			return err.Error()
		}
	}
	for time.Since(start) < d {
		add(&withKept, res(showWith(id, kept, kept, probeToast(what))), time.Since(start))
		add(&withNew, res(showWith(id, nil, kept, probeToast(what+" (new notifier)"))), time.Since(start))
		time.Sleep(250 * time.Millisecond)
	}
	for _, c := range []struct {
		name string
		rs   []run
	}{{"kept notifier", withKept}, {"new notifier", withNew}} {
		var b strings.Builder
		for _, r := range c.rs {
			fmt.Fprintf(&b, " [%.2f–%.2fs %s ×%d]", r.from.Seconds(), r.to.Seconds(), r.res, r.n)
		}
		t.Logf("PROBE %s, %s:%s", what, c.name, b.String())
	}
}

// showWith is ShowToast with notifier n (nil: one made for this call and
// released after), then kept back in the cache.
func showWith(id string, n, kept *rtObject, toast Toast) error {
	_ = onRT(func() error {
		if n == nil {
			delete(rt.notif, id)
		} else {
			rt.notif[id] = n
		}
		return nil
	})
	err := ShowToast(id, toast)
	_ = onRT(func() error {
		if made := rt.notif[id]; made != nil && made != kept {
			made.release()
		}
		rt.notif[id] = kept
		return nil
	})
	return err
}

// platformPIDs: the notification platform's processes (the per-user
// service's svchost, the shell's) as Windows lists them now.
func platformPIDs() string {
	out, err := exec.Command("powershell", "-NoProfile", "-NonInteractive", "-Command",
		`$s = Get-CimInstance Win32_Service -Filter "Name like 'WpnUserService%' or Name = 'WpnService'" | ForEach-Object { "$($_.Name)=$($_.State)/$($_.ProcessId)" }; `+
			`$p = Get-Process ShellExperienceHost, explorer -ErrorAction SilentlyContinue | ForEach-Object { "$($_.Name)=$($_.Id)" }; `+
			`($s + $p) -join ' '`).CombinedOutput()
	if err != nil {
		return fmt.Sprintf("%v %s", err, out)
	}
	return strings.TrimSpace(string(out))
}
