package desktop

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"syscall"
	"testing"
	"time"
	"unsafe"

	"golang.org/x/sys/windows"
	"golang.org/x/sys/windows/registry"
)

// scratchKeys moves the registration under a scratch key in HKCU, so the
// test never touches this user's associations. owner stands in for the
// shell's answer, and notified counts SHChangeNotify calls.
func scratchKeys(t *testing.T, owner *string, notified *int) urlKeys {
	t.Helper()
	base := fmt.Sprintf(`Software\ClackTest-%d-%d`, os.Getpid(), time.Now().UnixNano())
	t.Cleanup(func() {
		if _, err := deleteTree(base); err != nil {
			t.Errorf("cleanup: %v", err)
		}
	})
	return urlKeys{
		classes:  base + `\Classes`,
		software: base,
		owner:    func() string { return *owner },
		notify:   func() { *notified++ },
	}
}

func setString(t *testing.T, path, name, val string) {
	t.Helper()
	var w regWriter
	w.str(path, name, val)
	if w.err != nil {
		t.Fatal(w.err)
	}
}

func keyExists(path string) bool {
	k, err := registry.OpenKey(registry.CURRENT_USER, path, registry.QUERY_VALUE)
	if err != nil {
		return false
	}
	k.Close()
	return true
}

func valueType(path, name string) (uint32, bool) {
	k, err := registry.OpenKey(registry.CURRENT_USER, path, registry.QUERY_VALUE)
	if err != nil {
		return 0, false
	}
	defer k.Close()
	_, typ, err := k.GetValue(name, nil)
	return typ, err == nil
}

// Registration writes Clack's ProgID, Capabilities and
// RegisteredApplications value, and the legacy slack key only when no
// other app opens slack:// (or when asked); it writes nothing the second
// time; paths follow a moved exe; unregistering removes only Clack's.
func TestURLHandlerRegistration(t *testing.T) {
	var owner string
	var notified int
	r := scratchKeys(t, &owner, &notified)
	exe := `C:\Users\Someone\AppData\Local\Programs\Clack\Clack.exe`
	check := func(when, exe string, legacy bool) {
		t.Helper()
		cmd := `"` + exe + `" "%1"`
		want := []struct{ path, name, val string }{
			{r.progID(), "", "URL:slack Protocol"},
			{r.progID(), "URL Protocol", ""},
			{r.progID() + `\DefaultIcon`, "", `"` + exe + `",0`},
			{r.progID() + `\shell\open\command`, "", cmd},
			{r.caps(), "ApplicationName", "Clack"},
			{r.caps(), "ApplicationDescription", urlAppDesc},
			{r.caps() + `\URLAssociations`, "slack", "Clack.slack"},
			{r.apps(), "Clack", r.caps()},
		}
		if legacy {
			want = append(want, []struct{ path, name, val string }{
				{r.legacy(), "URL Protocol", ""},
				{r.legacy() + `\DefaultIcon`, "", `"` + exe + `",0`},
				{r.legacy() + `\shell\open\command`, "", cmd},
			}...)
		}
		for _, c := range want {
			if !hasValue(c.path, c.name) {
				t.Errorf("%s: no %s [%s]", when, c.path, c.name)
			} else if got := readString(registry.CURRENT_USER, c.path, c.name); got != c.val {
				t.Errorf("%s: %s [%s] = %q, want %q", when, c.path, c.name, got, c.val)
			}
		}
	}

	// nothing else has slack://: Clack's keys, and the legacy key
	res, err := registerURL(r, exe, false)
	if err != nil {
		t.Fatal(err)
	}
	if !res.Changed || !res.Default || res.Owner != "" || notified != 1 {
		t.Fatalf("first registration: %+v, notified %d", res, notified)
	}
	check("first", exe, true)
	if typ, ok := valueType(r.legacy()+`\OpenWithProgids`, urlProgID); !ok || typ != registry.NONE {
		t.Errorf("OpenWithProgids: %v %v, want an empty REG_NONE Clack.slack", typ, ok)
	}

	// the next start: nothing to write, the shell isn't told
	if res, err = registerURL(r, exe, false); err != nil || res.Changed || !res.Default || notified != 1 {
		t.Fatalf("second registration: %+v %v, notified %d", res, err, notified)
	}

	// a moved portable copy (or an update elsewhere): the paths follow
	moved := `D:\Apps\Clack\Clack.exe`
	if res, err = registerURL(r, moved, false); err != nil || !res.Changed || !res.Default || notified != 2 {
		t.Fatalf("moved: %+v %v, notified %d", res, err, notified)
	}
	check("moved", moved, true)

	// Slack desktop takes the legacy key at its next start (Electron's
	// setAsDefaultProtocolClient): Clack keeps its own keys up to date
	// and leaves Slack's alone
	slackExe := `C:\Users\Someone\AppData\Local\slack\slack.exe`
	slackCmd := `"` + slackExe + `" --protocol-launcher "%1"`
	setString(t, r.legacy()+`\shell\open\command`, "", slackCmd)
	owner = slackExe
	if res, err = registerURL(r, exe, false); err != nil || res.Default || res.Owner != slackExe {
		t.Fatalf("Slack's legacy key: %+v %v", res, err)
	}
	check("Slack's legacy key", exe, false)
	if got := readString(registry.CURRENT_USER, r.legacy()+`\shell\open\command`, ""); got != slackCmd {
		t.Errorf("Clack replaced Slack's legacy key: %q", got)
	}

	// -register-url-handler takes it (the shell then resolves to Clack)
	owner = ""
	if res, err = registerURL(r, exe, true); err != nil || !res.Default {
		t.Fatalf("-register-url-handler: %+v %v", res, err)
	}
	check("-register-url-handler", exe, true)
	// …unless the user picked another app with Always, which still wins
	owner = "AppXq0fevzme2pys62n3e0fbqa7peapykr8v"
	if res, err = registerURL(r, exe, true); err != nil || res.Default || res.Owner != owner {
		t.Fatalf("-register-url-handler under UserChoice: %+v %v", res, err)
	}
	owner = ""

	// unregistering with Slack's legacy key: only Clack's keys go
	setString(t, r.legacy()+`\shell\open\command`, "", slackCmd)
	res, err = unregisterURL(r)
	if err != nil || !res.Changed || res.Owner != slackExe {
		t.Fatalf("unregister beside Slack: %+v %v", res, err)
	}
	for _, p := range []string{r.progID(), r.caps(), r.software + `\Clack`} {
		if keyExists(p) {
			t.Errorf("unregister left %s", p)
		}
	}
	if hasValue(r.apps(), "Clack") || hasValue(r.legacy()+`\OpenWithProgids`, urlProgID) {
		t.Error("unregister left the RegisteredApplications value or the OpenWithProgids entry")
	}
	if got := readString(registry.CURRENT_USER, r.legacy()+`\shell\open\command`, ""); got != slackCmd {
		t.Errorf("unregister changed Slack's legacy key: %q", got)
	}
	if keyExists(r.legacy() + `\DefaultIcon`) {
		t.Error("unregister left Clack's icon under Slack's legacy key")
	}

	// unregistering with Clack's legacy key: the key goes too
	if _, err := deleteTree(r.legacy()); err != nil {
		t.Fatal(err)
	}
	if _, err := registerURL(r, exe, false); err != nil {
		t.Fatal(err)
	}
	if res, err = unregisterURL(r); err != nil || !res.Changed || res.Owner != "" {
		t.Fatalf("unregister: %+v %v", res, err)
	}
	if keyExists(r.legacy()) {
		t.Error("unregister left Clack's legacy slack key")
	}

	// a Store app has slack:// (no legacy key): Clack creates no slack key,
	// not even for OpenWithProgids
	owner = "AppXq0fevzme2pys62n3e0fbqa7peapykr8v"
	if res, err = registerURL(r, exe, false); err != nil || res.Default {
		t.Fatalf("beside a Store app: %+v %v", res, err)
	}
	if keyExists(r.legacy()) {
		t.Error("Clack created a slack key beside a Store app's registration")
	}
}

func TestHandlerCommands(t *testing.T) {
	for _, c := range []struct {
		cmd, exe string
		clack    bool
	}{
		{`"C:\Program Files\Clack\Clack.exe" "%1"`, `C:\Program Files\Clack\Clack.exe`, true},
		{`"C:\Users\k\Downloads\clack-windows-amd64.exe" "%1"`, `C:\Users\k\Downloads\clack-windows-amd64.exe`, true},
		{`"C:\Users\k\AppData\Local\slack\slack.exe" --protocol-launcher "%1"`, `C:\Users\k\AppData\Local\slack\slack.exe`, false},
		{`C:\Users\k\AppData\Local\Programs\Slick\Slick.exe "%1"`, `C:\Users\k\AppData\Local\Programs\Slick\Slick.exe`, false},
		{`C:\tools\handler.exe %1`, `C:\tools\handler.exe`, false},
	} {
		if got := commandExe(c.cmd); got != c.exe {
			t.Errorf("commandExe(%s) = %q, want %q", c.cmd, got, c.exe)
		}
		if got := isClackExe(c.exe); got != c.clack {
			t.Errorf("isClackExe(%s) = %v", c.exe, got)
		}
	}
}

// The hand-off on Windows, over the socket Clack really uses (AF_UNIX in
// %TEMP%): a second launch's Forward reaches the running instance's
// Listen, which gets the link as sent.
func TestForwardOnWindows(t *testing.T) {
	cfg := t.TempDir()
	got := make(chan Request, 1)
	in, err := Listen(cfg, func(r Request) { got <- r })
	if err != nil {
		t.Fatal(err)
	}
	defer in.Close()
	const link = "slack://T0FAKE/magic-login/xyz"
	if !Forward(cfg, Request{URL: link}) {
		t.Fatal("not forwarded")
	}
	select {
	case r := <-got:
		if r.URL != link {
			t.Fatalf("got %q", r.URL)
		}
	case <-time.After(3 * time.Second):
		t.Fatal("the running instance didn't get the link")
	}
	if Forward(t.TempDir(), Request{}) {
		t.Fatal("forwarded to another config dir's instance")
	}
}

// On CI's Windows runner only (it changes this user's slack://
// association): with another program holding the legacy key the way
// Slack desktop does, the registration leaves that key alone, and the
// shell's own list of slack:// handlers
// (SHAssocEnumHandlersForProtocolByApplication) has Clack in it.
// Unregistering takes Clack out of it and leaves the other program.
func TestURLHandlerShell(t *testing.T) {
	if os.Getenv("CLACK_WINDOWS_ASSOC_TESTS") == "" {
		t.Skip("changes this user's slack:// association; CI's windows-applog job sets CLACK_WINDOWS_ASSOC_TESTS=1")
	}
	if o := shellOwner(); o != "" {
		t.Fatalf("slack:// already opens with %s here", o)
	}
	t.Logf("handlers with nothing registered: %v", assocHandlers(t))
	// cmd.exe stands in for slack.exe (a program that exists)
	other := filepath.Join(os.Getenv("SystemRoot"), "System32", "cmd.exe")
	otherCmd := `"` + other + `" /c exit "%1"`
	setString(t, `Software\Classes\slack`, "", "URL:slack Protocol")
	setString(t, `Software\Classes\slack`, "URL Protocol", "")
	setString(t, `Software\Classes\slack\shell\open\command`, "", otherCmd)
	assocChanged()
	t.Cleanup(func() {
		_, _ = unregisterURL(userURLKeys)
		_, _ = deleteTree(`Software\Classes\slack`)
		assocChanged()
	})
	t.Logf("handlers with the other program on the legacy key: %v", assocHandlers(t))
	if o := shellOwner(); !strings.EqualFold(o, other) {
		t.Errorf("shellOwner = %q, want the legacy key's %s", o, other)
	}
	exe, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}
	res, err := registerURL(userURLKeys, exe, false)
	if err != nil {
		t.Fatal(err)
	}
	if res.Default || !strings.EqualFold(res.Owner, other) {
		t.Errorf("registered beside the other program: %+v", res)
	}
	if got := readString(registry.CURRENT_USER, `Software\Classes\slack\shell\open\command`, ""); got != otherCmd {
		t.Errorf("the legacy key was changed: %q", got)
	}
	hs := assocHandlers(t)
	t.Logf("handlers with Clack registered too: %v", hs)
	if !listsHandler(hs, exe) {
		t.Errorf("the shell's slack:// handlers don't include Clack (%s): %v", exe, hs)
	}
	if _, err := unregisterURL(userURLKeys); err != nil {
		t.Fatal(err)
	}
	hs = assocHandlers(t)
	t.Logf("handlers after unregistering: %v", hs)
	if listsHandler(hs, exe) {
		t.Errorf("Clack is still a slack:// handler after unregistering: %v", hs)
	}
	if got := readString(registry.CURRENT_USER, `Software\Classes\slack\shell\open\command`, ""); got != otherCmd {
		t.Errorf("unregistering changed the other program's legacy key: %q", got)
	}
}

type assocHandler struct {
	name, ui    string
	recommended bool
}

func (h assocHandler) String() string {
	return fmt.Sprintf("%q (%s, recommended %t)", h.ui, h.name, h.recommended)
}

// listsHandler reports whether hs has exe (the shell may give a long path
// for a short one, so the file name decides) or an app named Clack.
func listsHandler(hs []assocHandler, exe string) bool {
	for _, h := range hs {
		if strings.EqualFold(filepath.Base(h.name), filepath.Base(exe)) || h.ui == "Clack" {
			return true
		}
	}
	return false
}

// comObject is a COM interface pointer; its first word is the vtable
// (IUnknown's three methods, then the interface's, as in shobjidl_core.h).
type comObject struct{ vtbl *[10]uintptr }

func (o *comObject) call(method int, args ...uintptr) uintptr {
	r, _, _ := syscall.SyscallN(o.vtbl[method], append([]uintptr{uintptr(unsafe.Pointer(o))}, args...)...)
	return r
}

// comString calls a method that hands back a CoTaskMemAlloc'd string.
func (o *comObject) comString(method int) string {
	var p *uint16
	if hr := o.call(method, uintptr(unsafe.Pointer(&p))); uint32(hr) != 0 || p == nil {
		return ""
	}
	defer windows.CoTaskMemFree(unsafe.Pointer(p))
	return windows.UTF16PtrToString(p)
}

var (
	iidIEnumAssocHandlers    = windows.GUID{Data1: 0x973810ae, Data2: 0x9599, Data3: 0x4b88, Data4: [8]byte{0x9e, 0x4d, 0x6e, 0xe9, 0x8c, 0x95, 0x52, 0xda}}
	procEnumProtocolHandlers = windows.NewLazySystemDLL("shell32.dll").NewProc("SHAssocEnumHandlersForProtocolByApplication")
)

// assocHandlers is the shell's list of slack:// handlers:
// SHAssocEnumHandlersForProtocolByApplication's IEnumAssocHandlers (Next
// is method 3), each IAssocHandler's GetName (3), GetUIName (4) and
// IsRecommended (6).
func assocHandlers(t *testing.T) []assocHandler {
	t.Helper()
	runtime.LockOSThread()
	defer runtime.UnlockOSThread()
	if err := windows.CoInitializeEx(0, windows.COINIT_APARTMENTTHREADED); err != nil && !errors.Is(err, syscall.Errno(1)) { // S_FALSE: already initialized
		t.Fatalf("CoInitializeEx: %v", err)
	}
	defer windows.CoUninitialize()
	if err := procEnumProtocolHandlers.Find(); err != nil {
		t.Fatal(err)
	}
	scheme, _ := windows.UTF16PtrFromString("slack")
	var e *comObject
	hr, _, _ := procEnumProtocolHandlers.Call(uintptr(unsafe.Pointer(scheme)), uintptr(unsafe.Pointer(&iidIEnumAssocHandlers)), uintptr(unsafe.Pointer(&e)))
	if uint32(hr) != 0 || e == nil {
		t.Logf("SHAssocEnumHandlersForProtocolByApplication: HRESULT %#x", uint32(hr))
		return nil
	}
	defer e.call(2) // Release
	var out []assocHandler
	for len(out) < 64 {
		var h *comObject
		var n uint32
		if hr := e.call(3, 1, uintptr(unsafe.Pointer(&h)), uintptr(unsafe.Pointer(&n))); uint32(hr) != 0 || n == 0 || h == nil {
			break
		}
		out = append(out, assocHandler{name: h.comString(3), ui: h.comString(4), recommended: h.call(6) == 0})
		h.call(2)
	}
	return out
}
