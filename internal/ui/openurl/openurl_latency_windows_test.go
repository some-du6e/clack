package openurl

import (
	"fmt"
	"os"
	"path/filepath"
	"slices"
	"strconv"
	"strings"
	"testing"
	"time"

	"golang.org/x/sys/windows"
	"golang.org/x/sys/windows/registry"
)

// Karim clicked a link on Windows (alpha.157) and "sat for 3 secs" before
// anything happened. TestOpenHandOffLatency times the hand-off from the
// call to the URL's handler process being created, for Clack's old
// rundll32 url.dll,FileProtocolHandler and for ShellExecuteEx (Electron's
// shell.openExternal, which Slack desktop uses: openurl_windows.go). The
// handler is this test binary, registered for a clackstub: scheme under
// HKCU (TestOpenStubHandler), so no browser starts on the runner and the
// browser's own start-up isn't in the numbers: each run reports when the
// OS created the handler's process.

const stubScheme = "clackstub"

// stubOut is where the handler run for nonce writes its times.
func stubOut(nonce string) string { return filepath.Join(os.TempDir(), "clackstub-"+nonce+".txt") }

// TestOpenStubHandler is the clackstub: handler: run by the shell with the
// URL as its last argument, it writes its process's creation time and the
// time it got here (UnixNano) for the test that opened it.
func TestOpenStubHandler(t *testing.T) {
	var u string
	for _, a := range os.Args[1:] {
		if strings.HasPrefix(a, stubScheme+":") {
			u = a
		}
	}
	if u == "" {
		t.Skip("not run as the clackstub: handler")
	}
	nonce := strings.TrimPrefix(u, stubScheme+":")
	if strings.Trim(nonce, "abcdefghijklmnopqrstuvwxyz0123456789-") != "" {
		t.Fatalf("odd nonce %q", nonce)
	}
	var created, exited, kernel, user windows.Filetime
	if err := windows.GetProcessTimes(windows.CurrentProcess(), &created, &exited, &kernel, &user); err != nil {
		t.Fatal(err)
	}
	line := fmt.Sprintf("%d %d", created.Nanoseconds(), time.Now().UnixNano())
	if err := os.WriteFile(stubOut(nonce), []byte(line), 0o600); err != nil {
		t.Fatal(err)
	}
}

// registerStub makes this binary the clackstub: handler for the test.
func registerStub(t *testing.T) {
	t.Helper()
	if os.Getenv("CLACK_WINDOWS_ASSOC_TESTS") == "" {
		// it registers a URL scheme for the user and opens console windows
		t.Skip("CLACK_WINDOWS_ASSOC_TESTS unset (windows-shell sets it)")
	}
	root := `Software\Classes\` + stubScheme
	t.Cleanup(func() { // before any write, so a failed one is undone too
		for _, p := range []string{root + `\shell\open\command`, root + `\shell\open`, root + `\shell`, root} {
			_ = registry.DeleteKey(registry.CURRENT_USER, p)
		}
	})
	exe, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}
	k, _, err := registry.CreateKey(registry.CURRENT_USER, root, registry.ALL_ACCESS)
	if err != nil {
		t.Fatal(err)
	}
	defer k.Close()
	if err := k.SetStringValue("", "URL:Clack test stub"); err != nil {
		t.Fatal(err)
	}
	if err := k.SetStringValue("URL Protocol", ""); err != nil {
		t.Fatal(err)
	}
	c, _, err := registry.CreateKey(registry.CURRENT_USER, root+`\shell\open\command`, registry.ALL_ACCESS)
	if err != nil {
		t.Fatal(err)
	}
	defer c.Close()
	if err := c.SetStringValue("", `"`+exe+`" -test.run=^TestOpenStubHandler$ "%1"`); err != nil {
		t.Fatal(err)
	}
}

// handOff is one open's times from the call: when the call returned
// (ShellExecuteEx, or rundll32's Start), when rundll32 exited, and when
// the OS created the handler's process.
type handOff struct{ ret, exit, handler time.Duration }

func openStub(t *testing.T, method string, i int) handOff {
	t.Helper()
	nonce := fmt.Sprintf("%s-%d-%d", method, i, time.Now().UnixNano())
	u := stubScheme + ":" + nonce
	var h handOff
	t0 := time.Now()
	switch method {
	case "launch": // Open's hand-off (ShellExecuteEx, rundll32 only if that fails)
		launch(u)
		h.ret = time.Since(t0)
	case "rundll32":
		cmd := command(u)
		if err := cmd.Start(); err != nil {
			t.Fatal(err)
		}
		h.ret = time.Since(t0)
		_ = cmd.Wait()
		h.exit = time.Since(t0)
	}
	deadline := time.Now().Add(20 * time.Second)
	for {
		b, err := os.ReadFile(stubOut(nonce))
		if err == nil {
			if f := strings.Fields(string(b)); len(f) == 2 {
				ns, _ := strconv.ParseInt(f[0], 10, 64)
				h.handler = time.Unix(0, ns).Sub(t0)
				_ = os.Remove(stubOut(nonce))
				return h
			}
		}
		if time.Now().After(deadline) {
			t.Fatalf("%s run %d: the handler never ran", method, i)
		}
		time.Sleep(5 * time.Millisecond)
	}
}

func median(ds []time.Duration) time.Duration {
	s := slices.Clone(ds)
	slices.Sort(s)
	return s[len(s)/2]
}

// handOffBudget is Open's hand-off budget, from the call to the handler's
// process being created (the median of 7). windows-latest measured
// ShellExecuteEx at 5.6ms and rundll32 at 58.6ms (cold: 35ms and 110ms).
const handOffBudget = 30 * time.Millisecond

func TestOpenHandOffLatency(t *testing.T) {
	registerStub(t)
	// one of each first: the shell's and the DLLs' caches are cold
	for _, m := range []string{"launch", "rundll32"} {
		h := openStub(t, m, 0)
		t.Logf("%s, cold: returned %v, rundll32 exited %v, handler created %v", m, h.ret, h.exit, h.handler)
	}
	got := map[string][]handOff{}
	for i := 1; i <= 7; i++ {
		for _, m := range []string{"launch", "rundll32"} {
			got[m] = append(got[m], openStub(t, m, i))
		}
	}
	med := map[string]handOff{}
	for _, m := range []string{"launch", "rundll32"} {
		var ret, exit, handler []time.Duration
		for _, h := range got[m] {
			ret, exit, handler = append(ret, h.ret), append(exit, h.exit), append(handler, h.handler)
		}
		med[m] = handOff{median(ret), median(exit), median(handler)}
		t.Logf("%s, median of %d: call returned %v, rundll32 exited %v, handler process created %v (all: %v)",
			m, len(handler), med[m].ret, med[m].exit, med[m].handler, handler)
	}
	if h := med["launch"].handler; h > handOffBudget {
		t.Errorf("Open's hand-off took %v (median) to the handler's process, over the %v budget", h, handOffBudget)
	}
}
