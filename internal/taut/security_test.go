package taut

import (
	"bytes"
	"context"
	"io"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
	"time"
)

// Plugins are untrusted: their ids and storage namespaces must not name
// files outside Taut's directories, installs must come over https, and a
// plugin that hangs the engine must not take a core forever.

// A plugin's id names plugins/<id>.js, so Go checks it even though JS
// already did: the bundle runs before the JS check, in the same realm,
// and here hands out a different id on the second read.
func TestImportRejectsBadIDs(t *testing.T) {
	h := newHarness(t)
	escaped := filepath.Join(filepath.Dir(h.dir), "escaped.js")
	for _, id := range []string{"../escaped", "../../escaped", "/tmp/abs", `..\escaped`, "a/b", "x\x00y", ".", "..", "CON", "nul", "com1.x", strings.Repeat("a", 101)} {
		code := `let reads = 0
		class Evil extends TautPlugin {
			static get id() { return reads++ === 0 ? 'Evil' : ` + jsQuote(id) + ` }
			static defaultConfig = { enabled: false }
		}
		return Evil`
		if got, err := h.tt.Import(h.ctx(), pluginJS(code), ""); err == nil {
			t.Errorf("imported id %q as %q", id, got)
		}
	}
	if _, err := os.Stat(escaped); !os.IsNotExist(err) {
		t.Errorf("a file was written outside plugins/: %v", err)
	}
	entries, _ := os.ReadDir(filepath.Join(h.dir, "plugins"))
	for _, e := range entries {
		t.Errorf("left behind in plugins/: %s", e.Name())
	}
	for _, id := range []string{"Nicknames", "a.b-c_d", "x"} {
		if !validPluginID(id) {
			t.Errorf("validPluginID(%q) = false", id)
		}
	}
	// ids that differ only in case would share a file on Windows and macOS
	mk := func(id string) []byte {
		return pluginJS(`return class P extends TautPlugin { static id = '` + id + `'; static defaultConfig = { enabled: false } }`)
	}
	if _, err := h.tt.Import(h.ctx(), mk("Twin"), ""); err != nil {
		t.Fatal(err)
	}
	if _, err := h.tt.Import(h.ctx(), mk("twin"), ""); err == nil || !strings.Contains(err.Error(), "case") {
		t.Errorf("case twin: %v", err)
	}
}

func jsQuote(s string) string {
	var b strings.Builder
	b.WriteByte('"')
	for _, r := range s {
		switch {
		case r == '"' || r == '\\':
			b.WriteByte('\\')
			b.WriteRune(r)
		case r < 0x20:
			b.WriteString(`\u00`)
			b.WriteByte("0123456789abcdef"[r>>4])
			b.WriteByte("0123456789abcdef"[r&15])
		default:
			b.WriteRune(r)
		}
	}
	b.WriteByte('"')
	return b.String()
}

func TestPluginURLs(t *testing.T) {
	for _, u := range []string{"http://example.com/p.js", "HTTP://example.com/p.js", "file:///etc/passwd", "ftp://x/p.js", "javascript:alert(1)", "example.com/p.js", "//example.com/p.js", "http://localhost.evil.com/p.js", "http://127.0.0.2/p.js", ""} {
		if _, err := CheckPluginURL(u); err == nil {
			t.Errorf("CheckPluginURL(%q) accepted", u)
		}
	}
	for _, u := range []string{"https://raw.githubusercontent.com/a/b/main/p.js", "http://localhost:5173/p.js", "http://127.0.0.1/p.js", "http://[::1]:8000/p.js"} {
		if _, err := CheckPluginURL(u); err != nil {
			t.Errorf("CheckPluginURL(%q): %v", u, err)
		}
	}
	h := newHarness(t)
	var fetched []string
	big := bytes.Repeat([]byte("x"), maxPluginBytes+10)
	h.tt.opts.Fetch = func(ctx context.Context, req *http.Request) (*http.Response, error) {
		fetched = append(fetched, req.URL.String())
		res := &http.Response{StatusCode: 200, Header: http.Header{}, Body: io.NopCloser(bytes.NewReader(nil)), Request: req}
		switch req.URL.Path {
		case "/big.js":
			res.Body = io.NopCloser(bytes.NewReader(big))
		case "/redirect.js":
			// what net/http reports after following a redirect to plain http
			res.Request = &http.Request{URL: &url.URL{Scheme: "http", Host: "evil.example", Path: "/p.js"}}
			res.Body = io.NopCloser(strings.NewReader(string(pluginJS(`return class R extends TautPlugin { static id = 'R'; static defaultConfig = { enabled: false } }`))))
		}
		return res, nil
	}
	if _, err := h.tt.InstallURL(h.ctx(), "http://example.com/p.js"); err == nil || !strings.Contains(err.Error(), "https://") {
		t.Errorf("http install: %v", err)
	}
	if len(fetched) != 0 {
		t.Errorf("fetched %v for a refused URL", fetched)
	}
	if _, err := h.tt.InstallURL(h.ctx(), "https://example.com/big.js"); err == nil || !strings.Contains(err.Error(), "larger") {
		t.Errorf("oversized plugin: %v", err)
	}
	if _, err := h.tt.InstallURL(h.ctx(), "https://example.com/redirect.js"); err == nil || !strings.Contains(err.Error(), "redirected") {
		t.Errorf("redirect to http: %v", err)
	}
	if h.tt.get("R") != nil {
		t.Error("installed a plugin that came over http")
	}
}

// Every namespace a plugin can pass to __clack.taut_kv maps to one file
// directly inside storage/ (or secrets/), distinct namespaces to distinct
// files, and the names existing data uses stay the same.
func TestStorageNamespaceFiles(t *testing.T) {
	d := newDataStore(t.TempDir())
	compat := map[string]string{
		"plugin:Nicknames:storage": "plugin~Nicknames~storage.json",
		"plugin:Nicknames:cache":   "plugin~Nicknames~cache.json",
		"slick:plugin:snappy":      "slick~plugin~snappy.json",
		"localStorage":             "localStorage.json",
	}
	for ns, want := range compat {
		if got := filepath.Base(d.fileFor(ns)); got != want {
			t.Errorf("fileFor(%q) = %s, want %s", ns, got, want)
		}
	}
	if got := d.fileFor("plugin:Nicknames:secrets"); got != filepath.Join(d.dir, "secrets", "plugin~Nicknames~secrets.json") {
		t.Errorf("secrets namespace: %s", got)
	}
	nss := []string{"", ".", "..", "...", "../../creds", "../secrets", "/etc/passwd", `C:\Windows\x`, `a\b`, "a/b", "a\x00b", "\x00session",
		"secrets", "CON", "nul", "com1:x", "Aux.json", strings.Repeat("z", 300), strings.Repeat("z", 300) + "!",
		"plugin:a:storage", "plugin~a~storage", "plugin/a/storage", "plugin a storage", "é", "e", ".hidden", "x:secrets", "x~secrets"}
	seen := map[string]string{}
	for ns := range compat {
		seen[d.fileFor(ns)] = ns
	}
	for _, ns := range nss {
		p := d.fileFor(ns)
		dir, base := filepath.Split(p)
		if dir != filepath.Join(d.dir, "storage")+string(filepath.Separator) && dir != filepath.Join(d.dir, "secrets")+string(filepath.Separator) {
			t.Errorf("fileFor(%q) = %s: outside storage/ and secrets/", ns, p)
		}
		if len(base) > 200 || strings.ContainsAny(base, `/\:`+"\x00") || strings.HasPrefix(base, ".") || windowsDeviceName(base) {
			t.Errorf("fileFor(%q) = %q: not a safe file name", ns, base)
		}
		if other, dup := seen[p]; dup {
			t.Errorf("namespaces %q and %q share %s", ns, other, p)
		}
		seen[p] = ns
	}
	// and the files really land there
	for _, ns := range nss {
		d.write(ns, "k", "v")
	}
	if err := d.flush(); err != nil {
		t.Fatal(err)
	}
	entries, _ := os.ReadDir(d.dir)
	for _, e := range entries {
		if e.Name() != "storage" && e.Name() != "secrets" {
			t.Errorf("unexpected %s in the data dir", e.Name())
		}
	}
	d2 := newDataStore(d.dir)
	for _, ns := range nss {
		if v, ok := d2.read(ns, "k"); !ok || v != "v" {
			t.Errorf("%q didn't round-trip", ns)
		}
	}
}

// Writes replace a symlink at the destination instead of following it,
// and plugin data and secrets are 0600 in 0700 directories.
func TestWritesDontFollowSymlinks(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("symlinks and modes differ on Windows")
	}
	dir := t.TempDir()
	victim := filepath.Join(t.TempDir(), "victim")
	if err := os.WriteFile(victim, []byte("keep"), 0o644); err != nil {
		t.Fatal(err)
	}
	d := newDataStore(dir)
	p := d.fileFor("plugin:X:storage")
	if err := os.MkdirAll(filepath.Dir(p), 0o700); err != nil {
		t.Fatal(err)
	}
	for _, link := range []string{p, p + ".tmp", d.secretsPath(), d.secretsPath() + ".tmp"} {
		if err := os.Symlink(victim, link); err != nil {
			t.Fatal(err)
		}
	}
	d.write("plugin:X:storage", "k", "v")
	d.writeSecret("plugin:X:token", "s")
	d.write("plugin:X:secrets", "k", "v")
	if err := d.flush(); err != nil {
		t.Fatal(err)
	}
	if b, _ := os.ReadFile(victim); string(b) != "keep" {
		t.Fatalf("wrote through a symlink: %q", b)
	}
	for _, f := range []string{p, d.secretsPath(), d.fileFor("plugin:X:secrets")} {
		fi, err := os.Lstat(f)
		if err != nil {
			t.Fatal(err)
		}
		if fi.Mode()&os.ModeSymlink != 0 || fi.Mode().Perm() != 0o600 {
			t.Errorf("%s: mode %v", f, fi.Mode())
		}
		di, _ := os.Stat(filepath.Dir(f))
		if filepath.Dir(f) != dir && di.Mode().Perm() != 0o700 {
			t.Errorf("%s: mode %v", filepath.Dir(f), di.Mode())
		}
	}
	h := newHarness(t)
	if fi, err := os.Stat(h.dir); err != nil || fi.Mode().Perm() != 0o700 {
		t.Errorf("taut dir mode: %v %v", fi.Mode(), err)
	}
}

// A plugin that wedges the engine in a loop QuickJS can't interrupt gets
// the runtime stopped by jsrt's watchdog; Taut restarts plugins after a
// crash, but not forever: after maxCrashes the plugin stays stopped with
// an error.
func TestHungPluginStopsAfterRepeatedCrashes(t *testing.T) {
	h := newHarness(t)
	h.tt.opts.TaskTimeout = 200 * time.Millisecond
	code := `class Hang extends TautPlugin {
		static id = 'Hang'; static pluginName = 'Hang'; static description = ''; static authors = []
		static defaultConfig = { enabled: true }
		start() { setTimeout(() => Array.prototype.reverse.call({ length: 2 ** 53 - 1 }), 50) }
	}
	return Hang`
	if _, err := h.tt.Import(h.ctx(), pluginJS(code), ""); err != nil {
		t.Fatal(err)
	}
	for i := 1; i <= maxCrashes; i++ {
		eventually(t, 20*time.Second, "the engine to be stopped", func() bool { return h.tt.liveRuntime() == nil })
		start := time.Now()
		if _, err := h.tt.ensureRuntime(); err != nil {
			t.Fatal(err)
		}
		if d := time.Since(start); d > 15*time.Second {
			t.Errorf("restart took %v", d)
		}
		if i < maxCrashes {
			eventually(t, 20*time.Second, "the plugin to restart", func() bool { p := h.tt.get("Hang"); return p != nil && h.tt.info(p).Running })
		}
	}
	time.Sleep(500 * time.Millisecond)
	info := h.tt.info(h.tt.get("Hang"))
	if info.Running || !strings.Contains(info.Error, "crashed") {
		t.Fatalf("after %d crashes: %+v", maxCrashes, info)
	}
	// the fresh runtime works
	var n float64
	h.jsInto(`return 6 * 7`, &n)
	if n != 42 {
		t.Fatalf("fresh runtime: %v", n)
	}
}

// The bridge (called from the UI goroutine) never waits for the runtime to
// start.
func TestBridgeDoesNotBlockOnStart(t *testing.T) {
	h := newHarness(t)
	if h.tt.Started() {
		t.Fatal("runtime already started")
	}
	start := time.Now()
	h.tt.Bridge().SetViewport(800, 600, 1, false)
	if d := time.Since(start); d > 100*time.Millisecond {
		t.Errorf("SetViewport waited %v for the runtime", d)
	}
	eventually(t, 20*time.Second, "the runtime to start in the background", h.tt.Started)
}
