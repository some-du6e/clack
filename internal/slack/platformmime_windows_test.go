//go:build windows

package slack

import (
	"fmt"
	"os"
	"strings"
	"testing"

	"golang.org/x/sys/windows/registry"
)

// The registry as Chromium reads it: an extension nothing registered has
// no type. With CLACK_WINDOWS_ASSOC_TESTS=1 (the windows-shell job) the
// test registers its own extensions under HKCU\Software\Classes, which HKCR
// shows over HKLM's, and reads them back as RegKey::ReadValue does: a
// REG_SZ, a REG_EXPAND_SZ expanded, at most 1024 UTF-16 units with the NUL
// (after expansion too), any case of the key's name; nothing for "", a
// REG_DWORD or a key without the value. What this machine's own keys say
// (.txt, .mov, .py and the rest Karim's reports name, what Slack desktop
// here would send) is logged, not asserted: the runner image decides it.
// The extension is quoted in the log so setup-go's problem matcher
// doesn't read the .go line as a Go error.
func TestRegistryMimeTypes(t *testing.T) {
	base := fmt.Sprintf("clacktest%d", os.Getpid())
	if m, ok := osMimeType(base + "unset"); ok {
		t.Errorf("an extension nothing registered: %q", m)
	}
	for _, ext := range []string{"txt", "mov", "heic", "py", "go", "ts", "rs", "md", "csv", "json", "log", "sh", "ico", "bin"} {
		m, ok := osMimeType(ext)
		t.Logf("HKCR %q: %q (%v); FileType %q", "."+ext, m, ok, FileType("a."+ext))
	}
	if os.Getenv("CLACK_WINDOWS_ASSOC_TESTS") != "1" {
		t.Skip(`CLACK_WINDOWS_ASSOC_TESTS=1 registers test extensions under HKCU\Software\Classes`)
	}
	t.Setenv("CLACK_MIME_TEST", "clack-expanded")
	t.Setenv("CLACK_MIME_LONG", strings.Repeat("b", 1100))
	fits := "application/" + strings.Repeat("a", 1023-len("application/")) // 1023 units and the NUL
	for _, c := range []struct {
		key  string
		set  func(registry.Key) error
		look string // the extension looked up ("" for key)
		want string
	}{
		{key: "sz", set: sz("application/x-clack-test"), want: "application/x-clack-test"},
		{key: "Upper", set: sz("application/x-clack-upper"), look: "upper", want: "application/x-clack-upper"},
		{key: "exp", set: expand("application/x-%CLACK_MIME_TEST%"), want: "application/x-clack-expanded"},
		{key: "explong", set: expand("application/x-%CLACK_MIME_LONG%")},
		{key: "fits", set: sz(fits), want: fits},
		{key: "long", set: sz(fits + "a")},
		{key: "empty", set: sz("")},
		{key: "dword", set: func(k registry.Key) error { return k.SetDWordValue("Content Type", 1) }},
		{key: "novalue", set: func(registry.Key) error { return nil }},
	} {
		path := `Software\Classes\.` + base + c.key
		k, _, err := registry.CreateKey(registry.CURRENT_USER, path, registry.SET_VALUE)
		if err != nil {
			t.Fatal(err)
		}
		t.Cleanup(func() { registry.DeleteKey(registry.CURRENT_USER, path) })
		err = c.set(k)
		k.Close()
		if err != nil {
			t.Fatal(err)
		}
		look := base + c.key
		if c.look != "" {
			look = base + c.look
		}
		if m, ok := osMimeType(look); m != c.want || ok != (c.want != "") {
			t.Errorf(".%s: %.60q, %v; want %.60q", look, m, ok, c.want)
		}
		if c.want != "" && FileType("a."+look) != c.want {
			t.Errorf("FileType(a.%s) = %.60q, want the registry's %.60q", look, FileType("a."+look), c.want)
		}
	}
}

func sz(v string) func(registry.Key) error {
	return func(k registry.Key) error { return k.SetStringValue("Content Type", v) }
}

func expand(v string) func(registry.Key) error {
	return func(k registry.Key) error { return k.SetExpandStringValue("Content Type", v) }
}
