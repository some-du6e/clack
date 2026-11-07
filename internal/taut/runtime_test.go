package taut

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/some-du6e/yoloslackgoclient/internal/slack"
)

// pluginJS wraps a class body in build:user-plugin's format (an IIFE
// returning the class, `$taut` read from the globals).
func pluginJS(src string) []byte {
	return []byte("(() => {\nconst TautPlugin = globalThis.TautPlugin, opt = globalThis.TautOpt;\n" + src + "\n})()")
}

func (h *harness) install(code []byte, enabled bool) string {
	h.t.Helper()
	var meta Meta
	if err := h.rt().call(h.ctx(), "load", &meta, string(code)); err != nil {
		h.t.Fatalf("load: %v", err)
	}
	if enabled {
		if err := h.tt.cfg.edit(meta.ID, func(b map[string]json.RawMessage) { b["enabled"] = json.RawMessage("true") }); err != nil {
			h.t.Fatal(err)
		}
	}
	id, err := h.tt.Import(h.ctx(), code, "")
	if err != nil {
		h.t.Fatalf("import %s: %v", meta.ID, err)
	}
	return id
}

func TestTautExports(t *testing.T) {
	h := newHarness(t)
	var got struct {
		Labels  []string
		Select  any
		Cats    map[string]string
		Days    []string
		Default bool
		Number  any
		Secret  any
	}
	h.jsInto(`
		const $ = T.$taut
		return {
			Labels: ['keepFirstLetter', 'max2Lines', 'snake_case-key', 'URLThing'].map($.labelFromKey),
			Select: $.opt.select(['dense', {value: 'x', label: 'X!'}], 'dense', 'c').kind,
			Cats: $.PLUGIN_CATEGORIES, Days: $.WEEKDAYS,
			Default: $.default === $.TautPlugin && globalThis.TautPlugin === $.TautPlugin && globalThis.TautOpt === $.opt,
			Number: $.opt.number(3, 'n', {min: 1, max: 9}).kind,
			Secret: $.opt.secret('s'),
		}`, &got)
	want := []string{"Keep first letter", "Max 2 lines", "Snake case key", "Urlthing"}
	for i, w := range want {
		if got.Labels[i] != w {
			t.Errorf("labelFromKey %d = %q, want %q", i, got.Labels[i], w)
		}
	}
	if got.Cats["messageBox"] != "Message Box" || len(got.Days) != 7 || got.Days[0] != "sun" || !got.Default {
		t.Errorf("exports: %+v", got)
	}
	b, _ := json.Marshal(got.Select)
	if string(b) != `{"options":[{"label":"Dense","value":"dense"},{"label":"X!","value":"x"}],"type":"select"}` {
		t.Errorf("select kind %s", b)
	}
}

const configPlugin = `
class Cfg extends TautPlugin {
  static id = 'Cfg'
  static pluginName = 'Config Test'
  static description = 'tests config'
  static authors = ['jeremy', {name: 'Someone', slackId: 'U123'}]
  static category = 'fun'
  static defaultConfig = {
    enabled: false,
    word: opt('hi', 'a word', {check: (v) => v.includes(' ') ? 'one word only' : null}),
    count: opt.number(2, 'a count', {min: 0, max: 10}),
    style: opt.select(['a', 'b'], 'a', 'a style'),
    tags: opt.list(['x'], 'tags'),
    color: opt.color('#ff0000', 'a color'),
    token: opt.secret('an API token'),
    nested: {a: 1},
  }
  static migrateConfig(saved) { if ('old' in saved) { saved.word = saved.old; delete saved.old } }
  start() { globalThis.__cfgSeen = this.config }
}
return Cfg`

func TestConfigSemantics(t *testing.T) {
	h := newHarness(t)
	// a saved block with an old key (migrated), a bad value (problem) and a secret typed into config.json (moved)
	if err := h.tt.cfg.edit("Cfg", func(b map[string]json.RawMessage) {
		b["enabled"] = json.RawMessage("true")
		b["old"] = json.RawMessage(`"migrated"`)
		b["count"] = json.RawMessage(`"seven"`)
		b["token"] = json.RawMessage(`"sekrit"`)
	}); err != nil {
		t.Fatal(err)
	}
	h.install(pluginJS(configPlugin), false)
	var seen map[string]any
	h.jsInto(`return globalThis.__cfgSeen`, &seen)
	if seen["word"] != "migrated" || seen["count"] != float64(2) || seen["token"] != "sekrit" || seen["style"] != "a" {
		t.Errorf("config the plugin got: %v", seen)
	}
	block := h.tt.cfg.block("Cfg")
	if _, ok := block["old"]; ok {
		t.Error("migrateConfig result not saved")
	}
	if _, ok := block["token"]; ok {
		t.Error("secret left in config.json")
	}
	if v, _ := h.tt.data.readSecret("plugin:Cfg:token"); v != "sekrit" {
		t.Errorf("secret store has %q", v)
	}
	// only set options are stored: setting the default removes the key
	if err := h.tt.SetOption(h.ctx(), "Cfg", "style", "b"); err != nil {
		t.Fatal(err)
	}
	if string(h.tt.cfg.block("Cfg")["style"]) != `"b"` {
		t.Error("style not saved")
	}
	if err := h.tt.SetOption(h.ctx(), "Cfg", "style", "a"); err != nil {
		t.Fatal(err)
	}
	if _, ok := h.tt.cfg.block("Cfg")["style"]; ok {
		t.Error("default value stored")
	}
	// checks: kind check in Go, the option's own check in JS
	if err := h.tt.SetOption(h.ctx(), "Cfg", "word", "two words"); err == nil || !strings.Contains(err.Error(), "one word only") {
		t.Errorf("custom check: %v", err)
	}
	if err := h.tt.SetOption(h.ctx(), "Cfg", "style", "zzz"); err == nil {
		t.Error("select accepted a non-choice")
	}
	if err := h.tt.SetOption(h.ctx(), "Cfg", "color", "notacolor"); err == nil {
		t.Error("color accepted a non-color")
	}
	if err := h.tt.SetOption(h.ctx(), "Cfg", "color", "rgb(1 2 3 / 50%)"); err != nil {
		t.Errorf("color: %v", err)
	}
	// a config change restarts the plugin as a fresh instance
	h.jsInto(`globalThis.__cfgSeen = null; return 1`, new(int))
	if err := h.tt.SetOption(h.ctx(), "Cfg", "count", 5); err != nil {
		t.Fatal(err)
	}
	h.jsInto(`return globalThis.__cfgSeen`, &seen)
	if seen["count"] != float64(5) {
		t.Errorf("restart with new config: %v", seen)
	}
	// config.json on disk is Taut's format
	b, _ := os.ReadFile(filepath.Join(h.dir, "config.json"))
	var file struct {
		Plugins map[string]map[string]any `json:"plugins"`
	}
	if err := json.Unmarshal(b, &file); err != nil || file.Plugins["Cfg"]["count"] != float64(5) || !strings.HasSuffix(string(b), "}\n") {
		t.Errorf("config.json: %s", b)
	}
	// the settings model
	s := h.tt.Settings()
	var info *PluginInfo
	for _, c := range s.Categories {
		for i := range c.Plugins {
			if c.Plugins[i].ID == "Cfg" {
				info = &c.Plugins[i]
				if c.Key != "fun" {
					t.Errorf("category %s", c.Key)
				}
			}
		}
	}
	if info == nil || !info.Running || len(info.Authors) != 2 || info.Authors[0].Name != "Jeremy" || info.Authors[1].SlackID != "U123" {
		t.Fatalf("settings: %+v", info)
	}
	kinds := map[string]string{}
	for _, o := range info.Options {
		kinds[o.Key] = o.Kind.Type
		if o.Key == "token" && !o.SecretSet {
			t.Error("secret not reported as set")
		}
		if o.Key == "word" && !o.HasCheck {
			t.Error("check not reported")
		}
	}
	for k, want := range map[string]string{"word": "string", "count": "number", "style": "select", "tags": "list", "color": "color", "token": "secret", "nested": "json"} {
		if kinds[k] != want {
			t.Errorf("kind %s = %s, want %s", k, kinds[k], want)
		}
	}
	// disabling stops it; metadata is cached so a new Taut lists it without JS
	if err := h.tt.SetEnabled(h.ctx(), "Cfg", false); err != nil {
		t.Fatal(err)
	}
	if h.tt.get("Cfg").running {
		t.Error("still running after disable")
	}
	if readMeta(h.dir, "Cfg", h.tt.get("Cfg").hash) == nil {
		t.Error("metadata not cached")
	}
}

func TestLifecycleTimeoutAndErrors(t *testing.T) {
	h := newHarness(t)
	h.tt.opts.LifecycleTimeout = 5 * time.Second
	slow := `class Slow extends TautPlugin {
		static id = 'Slow'; static pluginName = 'Slow'; static description = ''; static authors = []
		static defaultConfig = { enabled: true }
		start() { return new Promise(() => {}) }
	}
	return Slow`
	start := time.Now()
	_, err := h.tt.Import(h.ctx(), pluginJS(slow), "")
	if err == nil || !strings.Contains(err.Error(), "timed out after 5 seconds") {
		t.Fatalf("slow start: %v", err)
	}
	if d := time.Since(start); d < 4*time.Second || d > 9*time.Second {
		t.Errorf("timeout took %s", d)
	}
	// a plugin that fails to start isn't installed (Taut rolls back)
	if h.tt.get("Slow") != nil {
		t.Error("failed plugin stayed installed")
	}
	bad := `class Bad extends TautPlugin {
		static id = 'Bad'; static pluginName = 'Bad'; static description = ''; static authors = []
		static defaultConfig = { enabled: false }
		start() { throw new Error('boom') }
	}
	return Bad`
	id, err := h.tt.Import(h.ctx(), pluginJS(bad), "")
	if err != nil || id != "Bad" {
		t.Fatalf("disabled plugin import: %v", err)
	}
	if err := h.tt.SetEnabled(h.ctx(), "Bad", true); err == nil || !strings.Contains(err.Error(), "boom") {
		t.Errorf("start error: %v", err)
	}
	info := h.tt.info(h.tt.get("Bad"))
	if !strings.Contains(info.Error, "Failed to start: boom") || info.Running {
		t.Errorf("error capture: %+v", info)
	}
	// invalid ids and non-plugins are refused
	for _, code := range []string{
		`return class X extends TautPlugin { static id = '../x'; static defaultConfig = {enabled: false} }`,
		`return class Y { static id = 'Y' }`,
		`return class Z extends TautPlugin { static id = 'Z'; static defaultConfig = {enabled: 'yes'} }`,
	} {
		if _, err := h.tt.Import(h.ctx(), pluginJS(code), ""); err == nil {
			t.Errorf("accepted %s", code)
		}
	}
	// the same id twice needs "Update code"
	if _, err := h.tt.Import(h.ctx(), pluginJS(bad), ""); err == nil || !strings.Contains(err.Error(), "already exists") {
		t.Errorf("duplicate: %v", err)
	}
	if _, err := h.tt.Import(h.ctx(), pluginJS(bad), "Bad"); err == nil {
		// enabled, and still throws: refused and the old code stays
		t.Error("update with a failing start was accepted")
	}
	if err := h.tt.Remove(h.ctx(), "Bad"); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(filepath.Join(h.dir, "plugins", "Bad.js")); !os.IsNotExist(err) {
		t.Error("plugin file left behind")
	}
}

func TestMirrorShapes(t *testing.T) {
	h := newHarness(t)
	var got map[string]any
	h.jsInto(`
		const s = T.redux.getRawState()
		return {
			general: s.channels['`+chGen+`'].name, lc: s.channels['`+chGen+`']._name_lc, member: s.channels['`+chGen+`'].is_member,
			alice: s.members.UALICE.profile.display_name, aliceLc: s.members.UALICE._display_name_lc,
			unknown: s.members.UNOPE === undefined,
			msgs: Object.keys(s.messages['`+chGen+`']).length,
			msgChannel: s.messages['`+chGen+`']['1700000001.000100'].channel,
			rxn: s.messages['`+chGen+`']['1700000002.000200']._rxn_key,
			count: s.membershipCounts['`+chGen+`'].counts.member_count,
			threadOnly: s.threadOnlyChannels['`+chAnn+`']?.isThreadOnly ?? false,
			readOnly: !!s.readOnlyChannels['`+chAnn+`'],
			self: s.bootData.user_id, team: s.selfTeamIds.enterpriseId,
			hc: api.workspace.isHackClub(),
			bot: s.bots.B0BOT.name,
			inKeys: 'UALICE' in s.members,
			hist: s.channelHistory['`+chGen+`'].slices[0].timestamps.length,
			cached: api.members.getCachedMember('UBOB').real_name,
			current: api.members.getCurrentMemberId(),
			sameIdentity: s.channels === T.redux.getRawState().channels,
		}`, &got)
	want := map[string]any{"general": "general", "lc": "general", "member": true, "alice": "alice", "aliceLc": "alice", "unknown": true,
		"msgs": float64(3), "msgChannel": chGen, "rxn": "message-1700000001.000100-" + chGen, "count": float64(1234),
		"threadOnly": true, "readOnly": false, "self": selfID, "team": entID, "hc": true, "bot": "Helper Bot", "inKeys": true,
		"hist": float64(3), "cached": "Bob Example", "current": selfID, "sameIdentity": true}
	want["rxn"] = "message-1700000002.000200-" + chGen
	for k, w := range want {
		if got[k] != w {
			t.Errorf("%s = %v, want %v", k, got[k], w)
		}
	}
	// an event invalidates the cached entry and the slice identity
	h.app.S.Mutate(chGen, "1700000003.000300", func(m *slack.Message) { m.Text = "edited text" })
	h.tt.mirror.invalidate("messages", chGen)
	time.Sleep(50 * time.Millisecond)
	var text string
	h.jsInto(`return T.redux.getRawState().messages['`+chGen+`']['1700000003.000300'].text`, &text)
	if text != "edited text" {
		t.Errorf("after invalidate: %q", text)
	}
}

func TestPatchesAndViews(t *testing.T) {
	h := newHarness(t)
	h.js(`
		globalThis.__unpatch = T.redux.patchSlice('members', (id, m) => id === 'UALICE' ? api.members.modifyMemberObject(m, {name: 'Ally'}) : m)
		globalThis.__unpatchText = api.messages.patchMessageText((t) => t.replace(/job/g, '***'))
		return 1`)
	eventually(t, 2*time.Second, "members patched", func() bool { return h.tt.SlicePatched("members") })
	var mf MemberFields
	eventually(t, 3*time.Second, "member view", func() bool {
		var ok bool
		mf, ok = h.tt.MemberView("UALICE")
		return ok
	})
	if mf.Name != "Ally" || mf.RealName != "Ally" {
		t.Errorf("member view %+v", mf)
	}
	var msgText string
	eventually(t, 3*time.Second, "message view", func() bool {
		m, ok := h.tt.MessageView(chGen, "1700000001.000100")
		msgText = m.Text
		return ok
	})
	if msgText != "the *** fair is today" {
		t.Errorf("message view %q", msgText)
	}
	h.js(`globalThis.__unpatch(); globalThis.__unpatchText(); return 1`)
	eventually(t, 2*time.Second, "unpatched", func() bool { return !h.tt.SlicePatched("members") && !h.tt.SlicePatched("messages") })
	// a throwing patch is switched off instead of breaking reads
	var after string
	h.jsInto(`
		T.redux.patchState(() => { throw new Error('bad patch') })
		return T.redux.getStore().getState().channels['`+chGen+`'].name`, &after)
	if after != "general" {
		t.Errorf("state after a bad patch: %q", after)
	}
}

func TestDispatch(t *testing.T) {
	h := newHarness(t)
	type typing struct {
		ChannelID string `json:"channelId"`
	}
	calls := 0
	native := func(ctx context.Context, a typing) (any, error) { calls++; return nil, nil }
	// unpatched: straight to native, no JS
	if _, err := Dispatch(h.ctx(), h.tt, "currentUserStartedTyping", typing{chGen}, native); err != nil || calls != 1 {
		t.Fatalf("unpatched: %v %d", err, calls)
	}
	if h.tt.Started() {
		t.Error("an unpatched dispatch started JS")
	}
	h.js(`
		T.redux.patchThunk('currentUserStartedTyping', (original) => (args, ...rest) => args?.channelId === '` + dmAlice + `' ? () => {} : original({...args, channelId: args.channelId + '-x'}, ...rest))
		return 1`)
	eventually(t, 2*time.Second, "thunk patched", func() bool { return h.tt.ThunkPatched("currentUserStartedTyping") })
	var seen string
	native2 := func(ctx context.Context, a typing) (any, error) { calls++; seen = a.ChannelID; return "done", nil }
	r, err := Dispatch(h.ctx(), h.tt, "currentUserStartedTyping", typing{chGen}, native2)
	if err != nil || seen != chGen+"-x" || r != "done" {
		t.Errorf("patched: %v %q %v", err, seen, r)
	}
	before := calls
	if _, err := Dispatch(h.ctx(), h.tt, "currentUserStartedTyping", typing{dmAlice}, native2); err != nil || calls != before {
		t.Errorf("suppressed: %v calls %d→%d", err, before, calls)
	}
	// plugins dispatching a native thunk themselves reach core
	h.js(`await T.redux.dispatchThunk('setUserPrefByApi', {pref: 'messages_theme', value: 'dense'}); return 1`)
	if len(h.api.called("users.prefs.set")) != 1 {
		t.Error("setUserPrefByApi didn't call users.prefs.set")
	}
}

func TestStorageAndShared(t *testing.T) {
	h := newHarness(t)
	code := `class Store1 extends TautPlugin {
		static id = 'Store1'; static pluginName = 'S'; static description = ''; static authors = []
		static defaultConfig = { enabled: true }
		async start() {
			this.s = this.api.storage.store('k', {n: 0})
			await this.s.ready
			await this.s.update((v) => ({n: v.n + 1}))
			await this.api.secrets.store('key', '').set('hidden')
			this.shared = new this.api.SharedStore('state', 'on')
			const c = new this.api.Cache('c', {ttl: 1000})
			c.set('a', 1)
			globalThis.__store1 = {n: this.s.get().n, shared: this.api.sharedFrom('Store1', 'state').get(), cache: c.get('a')}
		}
	}
	return Store1`
	h.install(pluginJS(code), false)
	var got map[string]any
	h.jsInto(`return globalThis.__store1`, &got)
	if got["n"] != float64(1) || got["shared"] != "on" || got["cache"] != float64(1) {
		t.Errorf("storage: %v", got)
	}
	if err := h.tt.data.flush(); err != nil {
		t.Fatal(err)
	}
	b, err := os.ReadFile(h.tt.data.fileFor("plugin:Store1:storage"))
	if err != nil || !strings.Contains(string(b), `{\"n\":1}`) {
		t.Errorf("storage file: %s %v", b, err)
	}
	if _, err := os.Stat(h.tt.data.fileFor("plugin:Store1:secrets")); err != nil || !strings.Contains(h.tt.data.fileFor("plugin:Store1:secrets"), string(filepath.Separator)+"secrets"+string(filepath.Separator)) {
		t.Errorf("secrets not in the secret store: %v", err)
	}
	// a restart keeps storage; stopping disposes the shared store
	if err := h.tt.SetEnabled(h.ctx(), "Store1", false); err != nil {
		t.Fatal(err)
	}
	var shared any
	h.jsInto(`return api.sharedFrom('Store1', 'state').get() ?? null`, &shared)
	if shared != nil {
		t.Errorf("shared store survived stop: %v", shared)
	}
	if err := h.tt.SetEnabled(h.ctx(), "Store1", true); err != nil {
		t.Fatal(err)
	}
	h.jsInto(`return globalThis.__store1`, &got)
	if got["n"] != float64(2) {
		t.Errorf("storage after restart: %v", got)
	}
}

func TestHackClubGating(t *testing.T) {
	h := newHarness(t)
	h.app.C.Creds.Team, h.app.C.Creds.EnterpriseID = "T0OTHER", ""
	h.app.S.Lock()
	h.app.S.TeamID = "T0OTHER"
	h.app.S.Self.TeamID = "T0OTHER"
	h.app.S.Unlock()
	code := `class HC extends TautPlugin {
		static id = 'HC'; static pluginName = 'HC'; static description = ''; static authors = []
		static hackClubOnly = true
		static defaultConfig = { enabled: true }
		start() { globalThis.__hc = true }
	}
	return HC`
	if _, err := h.tt.Import(h.ctx(), pluginJS(code), ""); err != nil {
		t.Fatal(err)
	}
	if h.tt.get("HC").running {
		t.Error("hackClubOnly plugin ran outside Hack Club")
	}
	for _, c := range h.tt.Settings().Categories {
		for _, p := range c.Plugins {
			if p.ID == "HC" {
				t.Error("hackClubOnly plugin listed outside Hack Club")
			}
		}
	}
}

func TestRTMAndEvents(t *testing.T) {
	h := newHarness(t)
	h.js(`globalThis.__rtm = []; api.rtm.on('message', (e) => globalThis.__rtm.push(e.text)); return 1`)
	h.tt.mirror.start()
	eventually(t, 2*time.Second, "rtm types reach Go", func() bool {
		h.tt.ui.mu.Lock()
		defer h.tt.ui.mu.Unlock()
		return h.tt.ui.rtmTypes["message"]
	})
	// the raw tap itself
	raws, stop := h.app.SubscribeRaw()
	defer stop()
	raw := json.RawMessage(`{"type":"message","channel":"` + chGen + `","user":"UBOB","text":"realtime words","ts":"1700000099.000100"}`)
	h.app.Inject(slack.Event{Type: "message", Channel: json.RawMessage(`"` + chGen + `"`), Raw: raw})
	select {
	case ev := <-raws:
		if ev.Type != "message" {
			t.Errorf("raw tap got %q", ev.Type)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("the raw tap delivered nothing")
	}
	var got []string
	delivered := func() bool {
		h.jsInto(`return globalThis.__rtm`, &got)
		return len(got) >= 1
	}
	deadline := time.Now().Add(3 * time.Second)
	for time.Now().Before(deadline) && !delivered() {
		time.Sleep(20 * time.Millisecond)
	}
	if len(got) == 0 {
		// tell apart the mirror's pump from the post into JS
		h.tt.onRaw(slack.Event{Type: "message", Raw: raw})
		time.Sleep(300 * time.Millisecond)
		if delivered() {
			t.Fatal("onRaw delivers, but the mirror's pump never handed the raw event over")
		}
		h.rt().post("rtmEvent", raw)
		time.Sleep(300 * time.Millisecond)
		if delivered() {
			t.Fatal("post delivers, but onRaw dropped the event")
		}
		t.Fatal("nothing reaches rtm listeners, even a direct post")
	}
	if got[0] != "realtime words" {
		t.Errorf("rtm: %v", got)
	}
}

func TestScheduleEnd(t *testing.T) {
	ny, _ := time.LoadLocation("America/New_York")
	mon9 := time.Date(2026, 11, 2, 9, 30, 0, 0, ny) // a Monday
	w := []ScheduleWindow{{Days: []string{"mon", "tue"}, Start: "09:00", End: "17:00"}}
	end, ok := ScheduleEnd(w, "America/New_York", mon9)
	if !ok || !end.Equal(time.Date(2026, 11, 2, 17, 0, 0, 0, ny)) {
		t.Errorf("simple: %v %v", end, ok)
	}
	if _, ok := ScheduleEnd(w, "America/New_York", time.Date(2026, 11, 2, 18, 0, 0, 0, ny)); ok {
		t.Error("outside the window")
	}
	// overnight and back-to-back windows merge
	w2 := []ScheduleWindow{{Days: []string{"mon"}, Start: "22:00", End: "02:00"}, {Days: []string{"tue"}, Start: "02:00", End: "06:00"}}
	end, ok = ScheduleEnd(w2, "America/New_York", time.Date(2026, 11, 2, 23, 0, 0, 0, ny))
	if !ok || !end.Equal(time.Date(2026, 11, 3, 6, 0, 0, 0, ny)) {
		t.Errorf("chained: %v", end)
	}
	// every day all day stops a week out
	all := []ScheduleWindow{{Days: []string{"sun", "mon", "tue", "wed", "thu", "fri", "sat"}, Start: "00:00", End: "00:00"}}
	end, ok = ScheduleEnd(all, "America/New_York", mon9)
	if !ok || end.Sub(mon9) != 7*24*time.Hour {
		t.Errorf("capped: %v", end.Sub(mon9))
	}
}

func TestUTF16ToRune(t *testing.T) {
	f := utf16ToRune("a😀**b**")
	// UTF-16: a=0, 😀=1..2, *=3, *=4, b=5; runes: a=0, 😀=1, *=2, *=3, b=4
	for u, want := range map[int]int{0: 0, 1: 1, 3: 2, 5: 4, 8: 7, 99: 7} {
		if got := f(u); got != want {
			t.Errorf("utf16 %d → rune %d, want %d", u, got, want)
		}
	}
}
