package slick

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/some-du6e/yoloslackgoclient/internal/taut"
)

// A plugin in exactly the shape Slick's `build:plugin` writes (CommonJS in a
// factory queued on __slickPlugins, $slick left to the host, JSX as
// React.createElement), written by hand so these tests don't need Slick.
const helloPlugin = `// slick-plugin {"id":"HelloSlick","name":"Hello Slick","description":"test"}
(globalThis.__slickPlugins ??= []).push((require) => {
var module = { exports: {} }, exports = module.exports;
"use strict";
var __defProp = Object.defineProperty;
var __getOwnPropNames = Object.getOwnPropertyNames;
var __hasOwnProp = Object.prototype.hasOwnProperty;
var __export = (target, all) => { for (var name in all) __defProp(target, name, { get: all[name], enumerable: true }); };
var __copyProps = (to, from) => { if (from && typeof from === "object" || typeof from === "function") for (let key of __getOwnPropNames(from)) if (!__hasOwnProp.call(to, key)) __defProp(to, key, { get: () => from[key], enumerable: true }); return to; };
var __toCommonJS = (mod) => __copyProps(__defProp({}, "__esModule", { value: true }), mod);
var index_exports = {};
__export(index_exports, { default: () => HelloSlick });
module.exports = __toCommonJS(index_exports);
var import_slick = require("$slick");
const settings = {
  selectionColor: { type: "color", label: "Selection color", default: "#ec4899" },
  nickname: { type: "text", label: "Nickname", default: "Ally" },
  size: { type: "number", label: "Size", default: 3, min: 1, max: 10 },
  mode: { type: "select", label: "Mode", default: "a", options: [{ value: "a", label: "A" }, { value: "b", label: "B" }] },
};
class HelloSlick extends import_slick.SlickPlugin {
  static id = "HelloSlick";
  static pluginName = "Hello Slick";
  static description = "It changes the selection color.";
  static category = "appearance";
  static authors = "<@UALICE0001>";
  static settings = settings;
  static liveSettings = ["selectionColor"];
  static requiredAPIs = ["redux", "onMessageSendDelta", "addSendCheck"];
  starts = 0;
  start() {
    globalThis.__helloStarts = (globalThis.__helloStarts ?? 0) + 1;
    this.applyStyle();
    this.api.patchComponent("MessageSender", (Original) => (props) => React.createElement(Original, { ...props, hello: this.config.nickname }));
    this.api.redux.patchSlice("members", (id, member) => id !== "UALICE0001" || !member ? member : { ...member, profile: { ...member.profile, display_name: this.config.nickname } });
    this.api.onMessageSendDelta((delta) => {
      const ops = delta.ops.map((op) => typeof op.insert === "string" ? { ...op, insert: op.insert.replace(/teh/g, "the") } : op);
      return new delta.constructor(ops);
    });
    this.api.addSendCheck((blocks, ctx) => !JSON.stringify(blocks).includes("forbidden") && ctx.channelId !== "C0NOPE0001");
    void this.api.storage.set("started", { at: 1 });
    this.log("selection color acquired");
  }
  onSettingsChange(changed) {
    globalThis.__helloLive = changed;
    this.applyStyle();
  }
  applyStyle() {
    this.api.setStyle("::selection { background: " + this.config.selectionColor + "; }");
  }
  stop() { globalThis.__helloStopped = true; }
}
return module.exports.default;
});
`

// Its privileged half, in build.mjs's main format.
const helloMain = `// slick-main {"id":"HelloSlick"}
(globalThis.__slickMainPlugins ??= []).push((require) => {
var module = { exports: {} }, exports = module.exports;
const path = require("node:path");
module.exports = { default: {
  id: "HelloSlick",
  capabilities: ["net"],
  ready(ctx) { globalThis.__helloMainReady = true; return () => { globalThis.__helloMainReady = false; }; },
  rpc: {
    status(ctx, args) { return { enabled: ctx.settings.enabled, ext: path.extname("a/b.woff2"), echo: args[0] }; },
    async ping(ctx) { ctx.emit("pong", { n: 1 }); return true; },
  },
} };
return module.exports.default;
});
`

func TestZeroCostWithoutEnabledPlugins(t *testing.T) {
	h := newHarness(t)
	if err := os.WriteFile(filepath.Join(h.s.Dir(), "plugins", "HelloSlick.js"), []byte(helloPlugin), 0o600); err != nil {
		t.Fatal(err)
	}
	s, err := New(Options{Taut: h.tt, Dir: h.s.Dir(), NoUpdates: true})
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	s.StartEnabled(h.ctx())
	time.Sleep(100 * time.Millisecond)
	if h.tt.Started() || s.Started() {
		t.Fatal("a disabled plugin started the JS runtime")
	}
	if got := s.Settings(); len(got.Plugins) != 1 || got.Plugins[0].Enabled {
		t.Fatalf("settings %+v", got.Plugins)
	}
}

func TestLifecycleSettingsAndAPIs(t *testing.T) {
	h := newHarness(t)
	if name := mustImport(t, h, helloPlugin); name != "Hello Slick" {
		t.Fatalf("name %q", name)
	}
	if h.tt.Started() == false {
		t.Fatal("import should evaluate the plugin")
	}
	info := h.plugin("HelloSlick")
	if info.Enabled || info.Running || info.CategoryLabel != "Look & feel" || len(info.Options) != 4 || info.Options[0].Key != "selectionColor" {
		t.Fatalf("info %+v", info)
	}
	if err := h.s.SetEnabled(h.ctx(), "HelloSlick", true); err != nil {
		t.Fatal(err)
	}
	info = h.plugin("HelloSlick")
	if !info.Running || info.StartError != "" {
		t.Fatalf("not running: %+v", info)
	}
	if css := h.ui.style("plugin:HelloSlick:default"); !strings.Contains(css, "#ec4899") {
		t.Fatalf("style %q", css)
	}
	st := h.state()
	if !has(st.Components, "MessageSender") || !has(st.Slices, "members") || st.Delta != 1 || st.SendChecks != 1 {
		t.Fatalf("registrations %+v", st)
	}
	// the members patch reaches the UI's member view
	eventually(t, 3*time.Second, "patched member", func() bool {
		m, ok := h.tt.MemberView(aliceID)
		return ok && m.DisplayName == "Ally"
	})
	// the Delta transform runs on sends
	out := h.tt.TransformSendBlocks(h.ctx(), json.RawMessage(`[{"type":"rich_text","elements":[{"type":"rich_text_section","elements":[{"type":"text","text":"teh cat"}]}]}]`))
	if !strings.Contains(string(out), `"the cat`) {
		t.Fatalf("delta transform: %s", out)
	}
	// send checks see the blocks
	if ok := h.tt.SendCheck(h.ctx(), composerCtx(chGen, `[{"type":"rich_text","elements":[{"type":"rich_text_section","elements":[{"type":"text","text":"forbidden word"}]}]}]`)); ok {
		t.Fatal("send check let a forbidden message through")
	}
	if ok := h.tt.SendCheck(h.ctx(), composerCtx(chGen, `[{"type":"rich_text","elements":[]}]`)); !ok {
		t.Fatal("send check blocked a fine message")
	}
	// live setting: same instance, onSettingsChange, new style
	if err := h.s.SetOption(h.ctx(), "HelloSlick", "selectionColor", "#00aa88"); err != nil {
		t.Fatal(err)
	}
	var starts float64
	var live []string
	h.jsInto(`return globalThis.__helloStarts`, &starts)
	h.jsInto(`return globalThis.__helloLive`, &live)
	if starts != 1 || len(live) != 1 || live[0] != "selectionColor" {
		t.Fatalf("live change: starts=%v live=%v", starts, live)
	}
	if css := h.ui.style("plugin:HelloSlick:default"); !strings.Contains(css, "#00aa88") {
		t.Fatalf("live style %q", css)
	}
	// coercion: out of range and wrong types
	if err := h.s.SetOption(h.ctx(), "HelloSlick", "size", 99); err != nil {
		t.Fatal(err)
	}
	if err := h.s.SetOption(h.ctx(), "HelloSlick", "mode", "zzz"); err != nil {
		t.Fatal(err)
	}
	opts := map[string]any{}
	for _, o := range h.plugin("HelloSlick").Options {
		opts[o.Key] = o.Value
	}
	if opts["size"] != 10.0 || opts["mode"] != "a" {
		t.Fatalf("coerced %v", opts)
	}
	// non-live setting restarts it
	// size restarted it; mode coerced back to its default changed nothing
	h.jsInto(`return globalThis.__helloStarts`, &starts)
	if starts != 2 {
		t.Fatalf("restarts: %v", starts)
	}
	// storage landed in Taut's data store under the plugin's namespace
	if v, ok := h.tt.DataRead("slick:plugin:HelloSlick", "started"); !ok || v != `{"at":1}` {
		t.Fatalf("storage %q %v", v, ok)
	}
	// settings.json is Slick's format
	b, _ := os.ReadFile(filepath.Join(h.s.Dir(), "settings.json"))
	var file struct {
		Plugins map[string]map[string]any `json:"plugins"`
	}
	if json.Unmarshal(b, &file) != nil || file.Plugins["HelloSlick"]["enabled"] != true || file.Plugins["HelloSlick"]["selectionColor"] != "#00aa88" {
		t.Fatalf("settings.json %s", b)
	}
	// disable: everything it registered is gone, stop() ran
	if err := h.s.SetEnabled(h.ctx(), "HelloSlick", false); err != nil {
		t.Fatal(err)
	}
	st = h.state()
	if has(st.Components, "MessageSender") || has(st.Slices, "members") || st.Delta != 0 || st.SendChecks != 0 || hasPrefix(st.Styles, "plugin:HelloSlick") {
		t.Fatalf("left behind %+v", st)
	}
	if css := h.ui.style("plugin:HelloSlick"); css != "" {
		t.Fatalf("style left: %q", css)
	}
	var stopped bool
	h.jsInto(`return globalThis.__helloStopped === true`, &stopped)
	if !stopped {
		t.Fatal("stop() didn't run")
	}
}

func TestRequiredAPIsAreEnforced(t *testing.T) {
	h := newHarness(t)
	mustImport(t, h, strings.Replace(helloPlugin, `static requiredAPIs = ["redux", "onMessageSendDelta", "addSendCheck"];`, `static requiredAPIs = [];`, 1))
	_ = h.s.SetEnabled(h.ctx(), "HelloSlick", true)
	info := h.plugin("HelloSlick")
	if info.Running || !strings.Contains(info.StartError, "Plugin must declare requiredAPIs: redux") {
		t.Fatalf("%+v", info)
	}
	// it registered patchComponent before throwing: torn down
	if st := h.state(); has(st.Components, "MessageSender") {
		t.Fatal("a failed start left its patch")
	}
}

func TestMainHalf(t *testing.T) {
	h := newHarness(t)
	mustImport(t, h, helloPlugin)
	if _, err := h.s.Import(h.ctx(), []byte(helloMain)); err != nil {
		t.Fatal(err)
	}
	info := h.plugin("HelloSlick")
	if !info.HasMain || len(info.Capabilities) != 1 || info.Capabilities[0] != "net" {
		t.Fatalf("%+v", info)
	}
	if err := h.s.SetEnabled(h.ctx(), "HelloSlick", true); err != nil {
		t.Fatal(err)
	}
	var r struct {
		Status struct {
			Enabled bool   `json:"enabled"`
			Ext     string `json:"ext"`
			Echo    string `json:"echo"`
		} `json:"status"`
		Pong   any    `json:"pong"`
		Ready  bool   `json:"ready"`
		Denied string `json:"denied"`
	}
	h.jsInto(`
		// what the renderer's api.main.call / api.main.on do
		const status = await S.callMain('HelloSlick', 'status', ['x'])
		let pong
		const off = S.onMain('HelloSlick', 'pong', (v) => (pong = v))
		await S.callMain('HelloSlick', 'ping', [])
		await new Promise((r) => setTimeout(r, 10))
		off()
		let denied = ''
		try { await S.callMain('HelloSlick', 'nope', []) } catch (e) { denied = e.message }
		return { status, pong, ready: globalThis.__helloMainReady === true, denied }`, &r)
	if !r.Status.Enabled || r.Status.Ext != ".woff2" || r.Status.Echo != "x" || r.Pong == nil || !r.Ready || !strings.Contains(r.Denied, "unknown method: HelloSlick.nope") {
		t.Fatalf("%+v", r)
	}
	_ = h.s.SetEnabled(h.ctx(), "HelloSlick", false)
	var ready bool
	h.jsInto(`return globalThis.__helloMainReady === true`, &ready)
	if ready {
		t.Fatal("main half not disposed")
	}
}

// A renderer and main half that use the rpc sender the way StreamerMode
// (once), ChatTabs (getURL, on/removeListener before-input-event) and
// InstalledApps (session.fetch) do.
const senderPlugin = `// slick-plugin {"id":"SenderProbe","name":"Sender Probe","description":"test"}
(globalThis.__slickPlugins ??= []).push((require) => {
const { SlickPlugin } = require("$slick");
class SenderProbe extends SlickPlugin {
  static id = "SenderProbe";
  static pluginName = "Sender Probe";
  static description = "test";
  start() {}
}
return SenderProbe;
});
`

const senderMain = `// slick-main {"id":"SenderProbe"}
(globalThis.__slickMainPlugins ??= []).push((require) => {
let hooked;
return {
  id: "SenderProbe",
  capabilities: [],
  rpc: {
    async probe(ctx, args, sender) {
      let gone = false;
      sender.once("destroyed", () => (gone = true));
      hooked = (event, input) => {
        if (input.type === "keyDown" && input.control && input.key === "w") { event.preventDefault(); ctx.emitTo(sender, "close-tab"); }
      };
      sender.on("before-input-event", hooked);
      const res = await sender.session.fetch("https://example.test/page?x=1", { credentials: "include", signal: AbortSignal.timeout(15000) });
      return { id: sender.id, url: sender.getURL(), ok: res.ok, status: res.status, body: await res.text(), gone, destroyed: sender.isDestroyed() };
    },
    unhook(ctx, args, sender) { sender.removeListener("before-input-event", hooked); },
  },
};
});
`

func TestMainHalfSender(t *testing.T) {
	h := newHarness(t)
	h.answer("https://example.test/page", `{"page":1}`)
	mustImport(t, h, senderPlugin)
	if _, err := h.s.Import(h.ctx(), []byte(senderMain)); err != nil {
		t.Fatal(err)
	}
	if err := h.s.SetEnabled(h.ctx(), "SenderProbe", true); err != nil {
		t.Fatal(err)
	}
	h.s.SetRoute(teamID, chGen)
	eventually(t, 3*time.Second, "route", func() bool { return h.jsBool(`return location.pathname.endsWith('/` + chGen + `')`) })
	var r struct {
		Probe struct {
			ID        int    `json:"id"`
			URL       string `json:"url"`
			OK        bool   `json:"ok"`
			Status    int    `json:"status"`
			Body      string `json:"body"`
			Gone      bool   `json:"gone"`
			Destroyed bool   `json:"destroyed"`
		} `json:"probe"`
		Hooked   bool `json:"hooked"`
		Closed   int  `json:"closed"`
		Unhooked bool `json:"unhooked"`
		Other    bool `json:"other"`
	}
	h.jsInto(`
		const probe = await S.callMain('SenderProbe', 'probe', [])
		let closed = 0
		const off = S.onMain('SenderProbe', 'close-tab', () => closed++)
		const key = (k) => window.dispatchEvent(new KeyboardEvent('keydown', { key: k, ctrlKey: true, cancelable: true }))
		const hooked = !key('w')
		const other = !key('q')
		await new Promise((r) => setTimeout(r, 10))
		await S.callMain('SenderProbe', 'unhook', [])
		const unhooked = key('w')
		await new Promise((r) => setTimeout(r, 10))
		off()
		return { probe, hooked, closed, unhooked, other }`, &r)
	p := r.Probe
	if p.ID != 1 || p.URL != "https://app.slack.com/client/"+teamID+"/"+chGen || !p.OK || p.Status != 200 || p.Body != `{"page":1}` || p.Gone || p.Destroyed {
		t.Fatalf("probe %+v", p)
	}
	if !r.Hooked || r.Other || r.Closed != 1 || !r.Unhooked {
		t.Fatalf("before-input-event: %+v", r)
	}
	h.fetchMu.Lock()
	defer h.fetchMu.Unlock()
	if len(h.cookied) > 0 {
		t.Fatalf("session.fetch sent a cookie: %v", h.cookied)
	}
}

func TestThemesAndUserCSS(t *testing.T) {
	h := newHarness(t)
	_ = os.MkdirAll(filepath.Join(h.s.Dir(), "themes"), 0o700)
	_ = os.WriteFile(filepath.Join(h.s.Dir(), "themes", "nord.json"), []byte(`{"name":"Nord","mode":"dark","palette":{"aubergine":{"0":"1,2,3"}},"vars":{"--dt_color-base-pry":"#2e3440"},"css":[".x{color:red}"]}`), 0o600)
	s, err := New(Options{Taut: h.tt, Dir: h.s.Dir(), NoAutoStart: true, NoUpdates: true})
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	if err := s.SetTheme("nord"); err != nil {
		t.Fatal(err)
	}
	if css := h.ui.style("slick:theme"); css != "" {
		t.Fatalf("dark-only theme applied in light mode: %q", css)
	}
	s.SetDark(true)
	css := h.ui.style("slick:theme")
	if !strings.Contains(css, "--dt_color-plt-aubergine-0:1,2,3 !important") || !strings.Contains(css, "--dt_color-base-pry:#2e3440 !important") || !strings.HasSuffix(css, ".x{color:red}") {
		t.Fatalf("theme css %q", css)
	}
	id, err := s.ImportTheme([]byte(`{"vars":{"--a":"b"}}`), "Mine.json")
	if err != nil || !strings.HasPrefix(id, "imported:") {
		t.Fatalf("import %q %v", id, err)
	}
	// importing selects it (Slick's configStore.importTheme sets config.theme)
	if css := h.ui.style("slick:theme"); !strings.Contains(css, "--a:b !important") {
		t.Fatalf("imported theme not applied: %q", css)
	}
	if sel := selectedTheme(s); sel != id {
		t.Fatalf("selected %q, want %q", sel, id)
	}
	_ = s.SetUserCSS("body{zoom:2}")
	if h.ui.style("slick:user") != "body{zoom:2}" {
		t.Fatal("user css")
	}
	// pausing Slick stops plugins only; the theme and custom CSS stay (Slick's
	// theme.ts and bootstrap.ts never read the global switch)
	if err := s.SetGlobalEnabled(h.ctx(), false); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(h.ui.style("slick:theme"), "--a:b !important") || h.ui.style("slick:user") != "body{zoom:2}" {
		t.Fatalf("paused Slick dropped styles: theme %q user %q", h.ui.style("slick:theme"), h.ui.style("slick:user"))
	}
	if err := s.SetGlobalEnabled(h.ctx(), true); err != nil {
		t.Fatal(err)
	}
	cards := s.Settings().Themes
	if cards[0].ID != "" || cards[len(cards)-1].ID != "custom" || len(cards) != 4 {
		t.Fatalf("cards %+v", cards)
	}
	if h.tt.Started() {
		t.Fatal("themes started JS")
	}
	if _, err := s.ImportTheme([]byte(`{"name":"x"}`), "x.json"); err == nil {
		t.Fatal("a theme with no fields was accepted")
	}
}

// Slick installs the theme, then user.css, so custom CSS beats the theme's
// !important rules. In Clack a dark-only theme only appears once SetDark(true)
// comes, after New already installed user.css; it must still land first.
func TestThemeBeforeUserCSS(t *testing.T) {
	h := newHarness(t)
	_ = os.MkdirAll(filepath.Join(h.s.Dir(), "themes"), 0o700)
	_ = os.WriteFile(filepath.Join(h.s.Dir(), "themes", "nord.json"), []byte(`{"mode":"dark","css":[".c-menu{background:#3b4252 !important}"]}`), 0o600)
	_ = os.WriteFile(filepath.Join(h.s.Dir(), "user.css"), []byte(".c-menu{background:red !important}"), 0o600)
	_ = os.WriteFile(filepath.Join(h.s.Dir(), "settings.json"), []byte(`{"theme":"nord","plugins":{}}`), 0o600)
	s, err := New(Options{Taut: h.tt, Dir: h.s.Dir(), NoAutoStart: true, NoUpdates: true})
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	if h.ui.style("slick:user") == "" || h.ui.style("slick:theme") != "" {
		t.Fatalf("light mode: theme %q user %q", h.ui.style("slick:theme"), h.ui.style("slick:user"))
	}
	s.SetDark(true)
	if !h.ui.before("slick:theme", "slick:user") {
		t.Fatalf("theme isn't before user.css: %v", h.ui.order)
	}
	// light → dark again, and switching themes, keep the order
	s.SetDark(false)
	s.SetDark(true)
	_ = s.SetTheme("custom")
	_ = s.SetTheme("nord")
	if !h.ui.before("slick:theme", "slick:user") {
		t.Fatalf("theme isn't before user.css after toggles: %v", h.ui.order)
	}
}

func TestLocalConfigAndRoute(t *testing.T) {
	h := newHarness(t)
	h.s.SetRoute(teamID, chGen)
	var r struct {
		Team    string `json:"team"`
		User    string `json:"user"`
		Channel string `json:"channel"`
	}
	eventually(t, 3*time.Second, "route", func() bool {
		h.jsInto(`
			const lc = JSON.parse(localStorage.getItem('localConfig_v2') || '{}')
			const t = lc.teams?.[lc.lastActiveTeamId]
			return { team: t?.id, user: t?.user_id, channel: location.pathname.match(/^\/client\/[^/]+\/([CDG][A-Z0-9]+)/)?.[1] ?? '' }`, &r)
		return r.Channel == chGen
	})
	if r.Team != teamID || r.User != selfID {
		t.Fatalf("%+v", r)
	}
	localStorage := ""
	h.jsInto(`localStorage.setItem('localConfig_v2', 'x'); return localStorage.getItem('localConfig_v2')`, &localStorage)
	if localStorage == "x" {
		t.Fatal("localConfig_v2 is writable")
	}
}

func TestSchemaCoercion(t *testing.T) {
	var sc Schema
	if err := json.Unmarshal([]byte(`{"b":{"type":"boolean","label":"B","default":true},"n":{"type":"number","label":"N","default":5,"min":0,"max":9},"names":{"type":"names","label":"x","default":{}},"sched":{"type":"schedule","label":"s","default":[],"statuses":[{"value":"away","label":"Away"}]},"enabled":{"type":"boolean","label":"no","default":true}}`), &sc); err != nil {
		t.Fatal(err)
	}
	if strings.Join(sc.Keys, ",") != "b,n,names,sched" {
		t.Fatalf("keys %v", sc.Keys)
	}
	got := sc.resolve(false, map[string]any{"b": "yes", "n": "12", "names": map[string]any{"UALICE0001": "Ally", "bad": "x"}, "sched": []any{map[string]any{"days": []any{5.0, 1.0, 1.0, 9.0}, "start": "09:00", "end": "17:00", "status": "nope"}}, "enabled": true})
	b, _ := json.Marshal(got)
	want := `{"b":false,"enabled":true,"n":9,"names":{"UALICE0001":"Ally"},"sched":[{"days":[1,5],"end":"17:00","start":"09:00","status":"away"}]}`
	if string(b) != want {
		t.Fatalf("got  %s\nwant %s", b, want)
	}
}

func composerCtx(ch, blocks string) taut.ComposerContext {
	return taut.ComposerContext{Kind: "channel", ChannelID: ch, Blocks: json.RawMessage(blocks)}
}

func mustImport(t *testing.T, h *harness, code string) string {
	t.Helper()
	name, err := h.s.Import(h.ctx(), []byte(code))
	if err != nil {
		t.Fatalf("import: %v", err)
	}
	return name
}

func selectedTheme(s *Slick) string {
	for _, c := range s.Settings().Themes {
		if c.Selected {
			return c.ID
		}
	}
	return ""
}

// Slick's isThemeJson / parseThemeJson / themeToCss on the edge cases:
// a present-but-null field is rejected, numbers print like String(n), and
// the file name loses .json in any case.
func TestThemeValidation(t *testing.T) {
	for raw, want := range map[string]bool{
		`{"vars":{"--a":"b"}}`:                     true,
		`{"vars":{"--a":1}}`:                       true,
		`{"palette":{}}`:                           true,
		`{"css":"x{}"}`:                            true,
		`{"css":["x{}","y{}"]}`:                    true,
		`{"palette":null}`:                         false,
		`{"palette":{"aubergine":null}}`:           false,
		`{"sidebar":null}`:                         false,
		`{"vars":null}`:                            false,
		`{"vars":[]}`:                              false,
		`{"css":null}`:                             false,
		`{"css":["x{}",1]}`:                        false,
		`{"vars":{"--a":"b"},"mode":null}`:         false,
		`{"vars":{"--a":"b"},"mode":"dim"}`:        false,
		`{"vars":{"--a":"b"},"name":null}`:         false,
		`{"vars":{"--a":"b"},"description":false}`: false,
		`{"name":"only a name"}`:                   false,
		`null`:                                     false,
		`[]`:                                       false,
	} {
		if got := validTheme(json.RawMessage(raw)); got != want {
			t.Errorf("validTheme(%s) = %v, want %v", raw, got, want)
		}
	}
	var th Theme
	_ = json.Unmarshal([]byte(`{"vars":{"--n":1000000,"--f":0.5,"--big":1e21,"--tiny":1.5e-7,"--neg":-12.25}}`), &th)
	css := themeCSS(th)
	for _, d := range []string{"--n:1000000 !important", "--f:0.5 !important", "--big:1e+21 !important", "--tiny:1.5e-7 !important", "--neg:-12.25 !important"} {
		if !strings.Contains(css, d) {
			t.Errorf("no %q in %q", d, css)
		}
	}
	for file, want := range map[string]string{"Mine.Json": "Mine", "a.JSON": "a", "b.json": "b", "c.txt": "c.txt", ".json": "Imported theme"} {
		raw, err := ParseTheme([]byte(`{"vars":{"--a":"b"}}`), file)
		var m struct {
			Name string `json:"name"`
		}
		if err != nil || json.Unmarshal(raw, &m) != nil || m.Name != want {
			t.Errorf("ParseTheme(%q) name %q, want %q (%v)", file, m.Name, want, err)
		}
	}
}
