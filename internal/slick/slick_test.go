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
	_ = s.SetTheme(id)
	if css := h.ui.style("slick:theme"); !strings.Contains(css, "--a:b !important") {
		t.Fatalf("imported %q", css)
	}
	_ = s.SetUserCSS("body{zoom:2}")
	if h.ui.style("slick:user") != "body{zoom:2}" {
		t.Fatal("user css")
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
