//go:build darwin && cgo

package openurl

import (
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
	"time"

	"golang.org/x/sys/unix"
)

// macOS's half of the link hand-off timing (Windows':
// openurl_latency_windows_test.go): from the call to the URL's handler
// process being created, for Electron's NSWorkspace openURL (shellOpen,
// which Slack desktop uses) and for Clack's old `open <url>`. The handler
// is this test binary copied into a throwaway ClackStub.app that claims
// a clackstub: scheme (lsregister), so no browser starts on the runner:
// run from inside the bundle, TestMain records the process's start time
// (sysctl kern.proc.pid) and exits.

const macStubScheme = "clackstub"

func TestMain(m *testing.M) {
	if exe, err := os.Executable(); err == nil && strings.Contains(exe, ".app/Contents/MacOS/") {
		macStubRecord(exe)
		os.Exit(0)
	}
	os.Exit(m.Run())
}

// macStubRecord appends this process's start time (UnixNano) to
// stub-times.txt beside the bundle.
func macStubRecord(exe string) {
	kp, err := unix.SysctlKinfoProc("kern.proc.pid", os.Getpid())
	if err != nil {
		return
	}
	st := kp.Proc.P_starttime
	ns := time.Unix(int64(st.Sec), int64(st.Usec)*1000).UnixNano()
	out := filepath.Join(filepath.Dir(exe), "..", "..", "..", "stub-times.txt")
	f, err := os.OpenFile(out, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0o600)
	if err != nil {
		return
	}
	fmt.Fprintf(f, "%d\n", ns)
	f.Close()
}

const lsregister = "/System/Library/Frameworks/CoreServices.framework/Frameworks/LaunchServices.framework/Support/lsregister"

// macStubApp builds and registers ClackStub.app in dir; it returns the
// times file.
func macStubApp(t *testing.T, dir string) string {
	t.Helper()
	app := filepath.Join(dir, "ClackStub.app")
	t.Cleanup(func() { _ = exec.Command(lsregister, "-u", app).Run() }) // before it's registered, so a failed run is undone too
	if err := os.MkdirAll(filepath.Join(app, "Contents", "MacOS"), 0o755); err != nil {
		t.Fatal(err)
	}
	exe, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}
	src, err := os.Open(exe)
	if err != nil {
		t.Fatal(err)
	}
	defer src.Close()
	dst, err := os.OpenFile(filepath.Join(app, "Contents", "MacOS", "clackstub"), os.O_CREATE|os.O_WRONLY, 0o755)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := io.Copy(dst, src); err != nil {
		t.Fatal(err)
	}
	dst.Close()
	plist := `<?xml version="1.0" encoding="UTF-8"?>
<!DOCTYPE plist PUBLIC "-//Apple//DTD PLIST 1.0//EN" "http://www.apple.com/DTDs/PropertyList-1.0.dtd">
<plist version="1.0"><dict>
<key>CFBundleIdentifier</key><string>com.clack.test.openurlstub</string>
<key>CFBundleInfoDictionaryVersion</key><string>6.0</string>
<key>CFBundleVersion</key><string>1</string>
<key>CFBundleShortVersionString</key><string>1.0</string>
<key>CFBundleName</key><string>ClackStub</string>
<key>CFBundleExecutable</key><string>clackstub</string>
<key>CFBundlePackageType</key><string>APPL</string>
<key>LSBackgroundOnly</key><true/>
<key>CFBundleURLTypes</key><array><dict>
<key>CFBundleURLName</key><string>Clack test stub</string>
<key>CFBundleURLSchemes</key><array><string>` + macStubScheme + `</string></array>
</dict></array>
</dict></plist>
`
	if err := os.WriteFile(filepath.Join(app, "Contents", "Info.plist"), []byte(plist), 0o644); err != nil {
		t.Fatal(err)
	}
	if out, err := exec.Command("codesign", "--force", "-s", "-", app).CombinedOutput(); err != nil {
		t.Logf("codesign (ad hoc): %v %s", err, out)
	}
	if out, err := exec.Command(lsregister, "-f", app).CombinedOutput(); err != nil {
		t.Fatalf("lsregister: %v %s", err, out)
	}
	// the scheme's default handler, through LSSetDefaultHandlerForURLScheme
	// (JXA: test files can't use cgo)
	js := `ObjC.import('CoreServices'); $.LSSetDefaultHandlerForURLScheme($('` + macStubScheme + `'), $('com.clack.test.openurlstub'))`
	if out, err := exec.Command("osascript", "-l", "JavaScript", "-e", js).CombinedOutput(); err != nil {
		t.Logf("LSSetDefaultHandlerForURLScheme: %v %s", err, out)
	}
	return filepath.Join(dir, "stub-times.txt")
}

func macStubTimes(path string) []int64 {
	b, _ := os.ReadFile(path)
	var out []int64
	for _, f := range strings.Fields(string(b)) {
		if n, err := strconv.ParseInt(f, 10, 64); err == nil {
			out = append(out, n)
		}
	}
	return out
}

// macOpen opens a stub URL the given way and returns the call's time
// and the handler's start, both from the call.
func macOpen(t *testing.T, times, method string, i int) (ret, handler time.Duration) {
	t.Helper()
	before := len(macStubTimes(times))
	u := fmt.Sprintf("%s:%s-%d", macStubScheme, method, i)
	t0 := time.Now()
	switch method {
	case "nsworkspace":
		err := shellOpen(u)
		ret = time.Since(t0)
		if err != nil {
			t.Logf("%s run %d: %v (the stub exits without answering Launch Services)", method, i, err)
		}
	case "open":
		cmd := command(u)
		if err := cmd.Run(); err != nil {
			t.Logf("%s run %d: %v", method, i, err)
		}
		ret = time.Since(t0)
	}
	deadline := time.Now().Add(20 * time.Second)
	for {
		if ts := macStubTimes(times); len(ts) > before {
			handler = time.Unix(0, ts[len(ts)-1]).Sub(t0)
			time.Sleep(300 * time.Millisecond) // the stub gone before the next open launches it anew
			return
		}
		if time.Now().After(deadline) {
			t.Fatalf("%s run %d: the handler never ran", method, i)
		}
		time.Sleep(5 * time.Millisecond)
	}
}

func TestOpenHandOffLatencyMac(t *testing.T) {
	if os.Getenv("CLACK_MACOS_OPENURL_TESTS") == "" {
		t.Skip("CLACK_MACOS_OPENURL_TESTS unset: it registers a URL scheme with Launch Services")
	}
	times := macStubApp(t, t.TempDir())
	// Launch Services takes a moment to learn the scheme
	for dl := time.Now().Add(10 * time.Second); ; time.Sleep(200 * time.Millisecond) {
		if err := shellOpen(macStubScheme + ":probe"); !errors.Is(err, errNoApp) {
			break
		}
		if time.Now().After(dl) {
			out, _ := exec.Command(lsregister, "-dump").Output()
			for _, l := range strings.Split(string(out), "\n") {
				if strings.Contains(l, "ClackStub") || strings.Contains(l, macStubScheme) {
					t.Logf("lsregister -dump: %.200s", l)
				}
			}
			t.Fatal("Launch Services never found ClackStub.app for clackstub:")
		}
	}
	for len(macStubTimes(times)) == 0 {
		time.Sleep(20 * time.Millisecond)
	}
	time.Sleep(300 * time.Millisecond)
	for _, m := range []string{"nsworkspace", "open"} {
		ret, h := macOpen(t, times, m, 0)
		t.Logf("%s, cold: call took %v, handler created %v", m, ret, h)
	}
	med := map[string][2]time.Duration{}
	for _, m := range []string{"nsworkspace", "open"} {
		var rets, hs []time.Duration
		for i := 1; i <= 5; i++ {
			ret, h := macOpen(t, times, m, i)
			rets, hs = append(rets, ret), append(hs, h)
		}
		med[m] = [2]time.Duration{median(rets), median(hs)}
		t.Logf("%s, median of 5: call took %v, handler process created %v (all: %v)", m, med[m][0], med[m][1], hs)
	}
}
