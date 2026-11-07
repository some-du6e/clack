package slick

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"
)

// Security checks for the privileged parts: file settings and node:fs,
// shell.openExternal, capabilities, protocol schemes, install ids and
// updates, and the memory-only storage of message-keeping plugins.

// probePlugin is a renderer in build:plugin's shape that keeps its api on
// globalThis.__probe_<id> for the test to drive.
func probePlugin(id, settings, start string) string {
	return fmt.Sprintf(`// slick-plugin {"id":%q,"name":%q,"description":"probe"}
(globalThis.__slickPlugins ??= []).push((require) => {
var import_slick = require("$slick");
class Probe extends import_slick.SlickPlugin {
  static id = %q;
  static pluginName = %q;
  static settings = %s;
  static requiredAPIs = [];
  async start() { globalThis["__probe_" + %q] = this.api; %s }
}
return Probe;
});
`, id, id, id, id, settings, id, start)
}

// probeMain is a main half in build.mjs's shape.
func probeMain(id, caps, body string) string {
	return fmt.Sprintf(`// slick-main {"id":%q}
(globalThis.__slickMainPlugins ??= []).push((require) => {
const fs = require("node:fs");
const plugin = { id: %q, capabilities: %s, %s };
return plugin;
});
`, id, id, caps, body)
}

func TestFileSettingsOnlyReadWhatTheUserPicked(t *testing.T) {
	h := newHarness(t)
	elsewhere := t.TempDir()
	secret := filepath.Join(elsewhere, "secret.txt")
	font := filepath.Join(elsewhere, "font.woff2")
	creds := filepath.Join(h.dir, "creds.json") // next to slick/, like $CLACK_HOME/creds.json
	link := filepath.Join(elsewhere, "innocent.woff2")
	for p, v := range map[string]string{secret: "TOPSECRET", font: "FONTDATA", creds: `{"token":"xoxc-test"}`} {
		if err := os.WriteFile(p, []byte(v), 0o600); err != nil {
			t.Fatal(err)
		}
	}
	if err := os.Symlink(creds, link); err != nil {
		t.Fatal(err)
	}
	settings := fmt.Sprintf(`{ f: { type: "file", label: "F", default: %q }, g: { type: "file", label: "G", default: "" } }`, secret)
	mustImport(t, h, probePlugin("FileProbe", settings, ""))
	mustImport(t, h, probeMain("FileProbe", `[]`, `rpc: { read(ctx, [p]) { try { return fs.readFileSync(p, "utf8") } catch (e) { return "ERR:" + e.code } } }`))
	if err := h.s.SetEnabled(h.ctx(), "FileProbe", true); err != nil {
		t.Fatal(err)
	}
	read := func(p string) string {
		return h.jsString(fmt.Sprintf(`return await S.callMain("FileProbe", "read", [%q])`, p))
	}
	stored := func(key string) bool {
		return h.jsBool(fmt.Sprintf(`return (await globalThis.__probe_FileProbe.storedFileUrl(%q)) !== null`, key))
	}
	// the schema's default is the plugin's, not the user's choice
	if got := read(secret); got != "ERR:ENOENT" {
		t.Fatalf("main half read the default path: %q", got)
	}
	if stored("f") {
		t.Fatal("storedFileUrl served the default path")
	}
	// a plugin can't point its own file setting at a path
	if h.jsBool(fmt.Sprintf(`return await globalThis.__probe_FileProbe.settings.set("g", %q)`, secret)) {
		t.Fatal("settings.set pointed a file setting at a path")
	}
	time.Sleep(50 * time.Millisecond)
	if got := read(secret); got != "ERR:ENOENT" {
		t.Fatalf("read after settings.set: %q", got)
	}
	// what the user picks in Preferences is readable
	if err := h.s.SetOption(h.ctx(), "FileProbe", "g", font); err != nil {
		t.Fatal(err)
	}
	if got := read(font); got != "FONTDATA" {
		t.Fatalf("picked file: %q", got)
	}
	if !stored("g") {
		t.Fatal("storedFileUrl didn't serve the picked file")
	}
	if got := read(filepath.Join(elsewhere, "x", "..", "secret.txt")); got != "ERR:ENOENT" {
		t.Fatalf("a .. path reached another file: %q", got)
	}
	// never Clack's own files, picked or through a symlink
	for _, p := range []string{creds, link} {
		if err := h.s.SetOption(h.ctx(), "FileProbe", "g", p); err != nil {
			t.Fatal(err)
		}
		if got := read(p); got != "ERR:ENOENT" {
			t.Fatalf("read %s: %q", p, got)
		}
		if stored("g") {
			t.Fatalf("storedFileUrl served %s", p)
		}
	}
}

func TestHostFunctionsAreOffTheGlobal(t *testing.T) {
	h := newHarness(t)
	mustImport(t, h, helloPlugin)
	var left []string
	h.jsInto(`return Object.keys(globalThis.__clack ?? {}).filter((k) => k.startsWith("slick_"))`, &left)
	if len(left) != 0 {
		t.Fatalf("host functions plugins could call directly: %v", left)
	}
	if h.jsBool(`return typeof globalThis.__clack?.slick_fs === "function" || typeof globalThis.__clack?.slick_blob === "function"`) {
		t.Fatal("slick_fs / slick_blob reachable")
	}
}

func TestMainCapabilitiesAreEnforced(t *testing.T) {
	h := newHarness(t)
	var mu sync.Mutex
	var opened []string
	h.s.opts.OpenURL = func(u string) { mu.Lock(); opened = append(opened, u); mu.Unlock() }
	mustImport(t, h, probePlugin("CapProbe", `{}`, ""))
	mustImport(t, h, probeMain("CapProbe", `["net"]`, `rpc: {
		async open(ctx, [u]) { try { await ctx.shell.openExternal(u); return "opened" } catch (e) { return e.message } },
		async escalate(ctx) { plugin.capabilities.push("shell"); try { await ctx.shell.openExternal("https://example.com/"); return "opened" } catch (e) { return e.message } },
		async secret(ctx) { try { await ctx.secrets.read("k"); return "read" } catch (e) { return e.message } },
	}`))
	if err := h.s.SetEnabled(h.ctx(), "CapProbe", true); err != nil {
		t.Fatal(err)
	}
	for _, m := range []string{"secret", "escalate"} {
		if got := h.jsString(fmt.Sprintf(`return await S.callMain("CapProbe", %q, ["https://example.com/"])`, m)); !strings.Contains(got, "without declaring it") {
			t.Fatalf("%s: %q", m, got)
		}
	}
	// declared, then: only web links, checked again in Go
	h2 := newHarness(t)
	h2.s.opts.OpenURL = func(u string) { mu.Lock(); opened = append(opened, u); mu.Unlock() }
	mustImport(t, h2, probePlugin("CapProbe", `{}`, ""))
	mustImport(t, h2, probeMain("CapProbe", `["shell"]`, `rpc: {
		async open(ctx, [u]) { try { await ctx.shell.openExternal(u); return "opened" } catch (e) { return e.message } },
		async electron(ctx, [u]) { try { await require("electron").shell.openExternal(u); return "opened" } catch (e) { return e.message } },
	}`))
	if err := h2.s.SetEnabled(h2.ctx(), "CapProbe", true); err != nil {
		t.Fatal(err)
	}
	for _, u := range []string{"file:///etc/passwd", "javascript:alert(1)", "slack://open", "x-custom://run"} {
		for _, m := range []string{"open", "electron"} {
			if got := h2.jsString(fmt.Sprintf(`return await S.callMain("CapProbe", %q, [%q])`, m, u)); got == "opened" {
				t.Fatalf("%s opened %s", m, u)
			}
		}
	}
	if got := h2.jsString(`return await S.callMain("CapProbe", "open", ["https://example.com/a"])`); got != "opened" {
		t.Fatalf("https: %q", got)
	}
	mu.Lock()
	defer mu.Unlock()
	if len(opened) != 1 || opened[0] != "https://example.com/a" {
		t.Fatalf("opened %v", opened)
	}
}

func TestExternalURLAndRateLimit(t *testing.T) {
	for raw, ok := range map[string]bool{
		"https://example.com/x?y=1": true, "http://example.com": true, "mailto:a@example.com": true, "HTTPS://example.com/": true,
		"file:///etc/passwd": false, "javascript:alert(1)": false, "slack://open": false, "-a https://x": false,
		"--help": false, "https://exa mple.com": false, "https://x/\n": false, "x-custom:run": false, "https:///nohost": false, "": false,
	} {
		if _, err := externalURL(raw); (err == nil) != ok {
			t.Errorf("externalURL(%q): err=%v, want ok=%v", raw, err, ok)
		}
	}
	var l rateLimit
	now := time.Now()
	for i := 0; i < openBurst; i++ {
		if !l.allow(now) {
			t.Fatalf("open %d refused", i)
		}
	}
	if l.allow(now.Add(time.Second)) {
		t.Fatal("a burst past the limit was allowed")
	}
	if !l.allow(now.Add(openWindow + time.Second)) {
		t.Fatal("still limited after the window")
	}
}

func TestProtocolSchemesAreValidated(t *testing.T) {
	h := newHarness(t)
	mustImport(t, h, probePlugin("ProtoProbe", `{}`, ""))
	mustImport(t, h, probeMain("ProtoProbe", `["protocol"]`, `boot(ctx) {
		globalThis.__protoErrors = [];
		for (const s of ["https", "HTTP", "file", "slack", "bad scheme", "proto-probe"]) {
			try { ctx.protocol.register(s, {}, () => new Response("hijacked")) } catch (e) { globalThis.__protoErrors.push(s) }
		}
	}`))
	if err := h.s.SetEnabled(h.ctx(), "ProtoProbe", true); err != nil {
		t.Fatal(err)
	}
	var errs []string
	h.jsInto(`return globalThis.__protoErrors`, &errs)
	if strings.Join(errs, ",") != "https,HTTP,file,slack,bad scheme" {
		t.Fatalf("refused %v", errs)
	}
	st := h.state()
	if len(st.Protocols) != 1 || st.Protocols[0] != "proto-probe" {
		t.Fatalf("protocols %v", st.Protocols)
	}
	if got := h.jsString(`return await (await fetch("PROTO-PROBE://x")).text()`); got != "hijacked" {
		t.Fatalf("own scheme, any case: %q", got)
	}
}

func TestPluginIDs(t *testing.T) {
	for id, ok := range map[string]bool{
		"HelloSlick": true, "my-plugin_2": true, "a.b": true,
		"": false, ".": false, "..": false, ".hidden": false, "Foo.main": false, "foo.META": false, "a/b": false, `a\b`: false, "a\x00b": false, strings.Repeat("a", 101): false,
	} {
		if validPluginID(id) != ok {
			t.Errorf("validPluginID(%q) = %v", id, !ok)
		}
	}
	h := newHarness(t)
	if _, err := h.s.Import(h.ctx(), []byte(probePlugin("Sneaky.main", `{}`, ""))); err == nil {
		t.Fatal("installed a renderer whose file would be another plugin's main half")
	}
	mustImport(t, h, helloPlugin)
	if _, err := h.s.Import(h.ctx(), []byte(probePlugin("helloslick", `{}`, ""))); err == nil {
		t.Fatal("installed an id that differs from an installed one only in case")
	}
}

func TestURLInstallsAndUpdates(t *testing.T) {
	h := newHarness(t)
	if _, err := h.s.InstallURL(h.ctx(), "http://plugins.example/Hello.js"); err == nil {
		t.Fatal("installed over http")
	}
	h.answer("https://plugins.example/", helloPlugin)
	if _, err := h.s.InstallURL(h.ctx(), "https://plugins.example/Hello.js"); err != nil {
		t.Fatal(err)
	}
	if src := h.plugin("HelloSlick").Source; src != "https://plugins.example/Hello.js" {
		t.Fatalf("source %q", src)
	}
	// the server now serves a privileged half: an update must not add it
	h.answer("https://plugins.example/", helloMain)
	if res := h.s.UpdateUserPlugins(h.ctx()); res.Failed != 1 || len(res.Updated) != 0 {
		t.Fatalf("update %+v", res)
	}
	if h.plugin("HelloSlick").HasMain {
		t.Fatal("an update installed a main half")
	}
	if _, err := os.Stat(filepath.Join(h.s.Dir(), "plugins", "HelloSlick.main.js")); err == nil {
		t.Fatal("HelloSlick.main.js written by an update")
	}
}

func TestPeekReadsTheHeaderOnly(t *testing.T) {
	h := newHarness(t)
	pk, err := h.s.Peek([]byte(helloPlugin))
	if err != nil || pk.ID != "HelloSlick" || pk.Name != "Hello Slick" || pk.Main || pk.Installed {
		t.Fatalf("%+v %v", pk, err)
	}
	if h.tt.Started() {
		t.Fatal("Peek ran JS")
	}
	pk, err = h.s.Peek([]byte(helloMain))
	if err != nil || pk.ID != "HelloSlick" || !pk.Main {
		t.Fatalf("main %+v %v", pk, err)
	}
	if _, err := h.s.Peek([]byte("console.log(1)")); err == nil {
		t.Fatal("peeked a file that isn't a plugin")
	}
	pk, _ = h.s.Peek([]byte(strings.Replace(helloPlugin, `"id":"HelloSlick"`, `"id":"../../evil"`, 1)))
	if pk.ID != "" {
		t.Fatalf("unsafe id %q", pk.ID)
	}
	pk, _ = h.s.Peek([]byte(probePlugin("MessageLogger", `{}`, "")))
	if !pk.StoresMessages {
		t.Fatal("MessageLogger not flagged")
	}
}

func TestMessageKeepingPluginsStayInMemory(t *testing.T) {
	h := newHarness(t)
	mustImport(t, h, probePlugin("MessageLogger", `{}`, `await this.api.storage.set("entry:C1:1", { text: "a deleted message" })`))
	info := h.plugin("MessageLogger")
	if !info.StoresMessages || info.KeepOnDisk {
		t.Fatalf("%+v", info)
	}
	if err := h.s.SetEnabled(h.ctx(), "MessageLogger", true); err != nil {
		t.Fatal(err)
	}
	onDisk := func() bool {
		_, ok := h.tt.DataRead("slick:plugin:MessageLogger", "entry:C1:1")
		return ok
	}
	readBack := func() string {
		return h.jsString(`return (await globalThis.__probe_MessageLogger.storage.get("entry:C1:1"))?.text ?? ""`)
	}
	if onDisk() {
		t.Fatal("message contents went to the data store")
	}
	if got := readBack(); got != "a deleted message" {
		t.Fatalf("memory read %q", got)
	}
	if err := h.s.SetKeepOnDisk(h.ctx(), "MessageLogger", true); err != nil {
		t.Fatal(err)
	}
	if !onDisk() || !h.plugin("MessageLogger").KeepOnDisk || readBack() != "a deleted message" {
		t.Fatal("Keep on disk didn't move the data to disk")
	}
	if err := h.s.SetKeepOnDisk(h.ctx(), "MessageLogger", false); err != nil {
		t.Fatal(err)
	}
	if onDisk() || readBack() != "a deleted message" {
		t.Fatal("turning Keep on disk off left the data on disk or lost it")
	}
	var prefs map[string]string
	b, _ := os.ReadFile(filepath.Join(h.s.Dir(), "plugins", "prefs.json"))
	_ = json.Unmarshal(b, &prefs)
	if prefs[keepOnDiskPref+"MessageLogger"] != "" {
		t.Fatalf("prefs %s", b)
	}
	// other plugins are unaffected
	mustImport(t, h, helloPlugin)
	_ = h.s.SetEnabled(h.ctx(), "HelloSlick", true)
	eventually(t, 3*time.Second, "an ordinary plugin's storage in the data store", func() bool {
		_, ok := h.tt.DataRead("slick:plugin:HelloSlick", "started")
		return ok
	})
	if err := h.s.Remove(h.ctx(), "MessageLogger"); err != nil {
		t.Fatal(err)
	}
	h.s.mem.mu.Lock()
	n := len(h.s.mem.ns["plugin:MessageLogger"])
	h.s.mem.mu.Unlock()
	if n != 0 {
		t.Fatal("Remove left the memory store")
	}
}

func TestImportedThemeNameIsABaseName(t *testing.T) {
	h := newHarness(t)
	id, err := h.s.ImportTheme([]byte(`{"vars":{"--a":"b"}}`), `../../evil\dir/Mine.json`)
	if err != nil {
		t.Fatal(err)
	}
	for _, c := range h.s.Settings().Themes {
		if c.ID == id && c.Name != "Mine" {
			t.Fatalf("name %q", c.Name)
		}
	}
}

func TestFilePickerIsOneAtATime(t *testing.T) {
	h := newHarness(t)
	release := make(chan struct{})
	var mu sync.Mutex
	calls := 0
	h.s.opts.OpenFile = func(title, accept string) (string, error) {
		mu.Lock()
		calls++
		mu.Unlock()
		<-release
		return "/picked/by/user.woff2", nil
	}
	mustImport(t, h, probePlugin("PickProbe", `{}`, ""))
	mustImport(t, h, probeMain("PickProbe", `["dialog"]`, `rpc: {
		async spam(ctx) { const all = []; for (let i = 0; i < 5; i++) all.push(ctx.dialog.openFile({ title: "x" })); globalThis.__picks = Promise.all(all); return true },
		async picks() { return await globalThis.__picks },
	}`))
	if err := h.s.SetEnabled(h.ctx(), "PickProbe", true); err != nil {
		t.Fatal(err)
	}
	h.js(`return await S.callMain("PickProbe", "spam", [])`)
	time.Sleep(100 * time.Millisecond)
	close(release)
	var picks []string
	h.jsInto(`return await S.callMain("PickProbe", "picks", [])`, &picks)
	mu.Lock()
	defer mu.Unlock()
	got := 0
	for _, p := range picks {
		if p == "/picked/by/user.woff2" {
			got++
		} else if p != "" {
			t.Fatalf("picks %v", picks)
		}
	}
	if calls != 1 || len(picks) != 5 || got != 1 {
		t.Fatalf("calls=%d picks=%v: more than one picker", calls, picks)
	}
}
