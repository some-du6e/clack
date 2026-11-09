package ui

import (
	"encoding/json"
	"image"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"sync"
	"testing"
	"time"

	"gioui.org/layout"
	"gioui.org/widget"

	"github.com/some-du6e/yoloslackgoclient/internal/pluginlib"
	"github.com/some-du6e/yoloslackgoclient/internal/taut"
	"github.com/some-du6e/yoloslackgoclient/internal/ui/theme/pixeltest"
)

// Tests never reach the published Library unless they point it somewhere.
func init() {
	if os.Getenv("CLACK_PLUGIN_LIBRARY") == "" {
		os.Setenv("CLACK_PLUGIN_LIBRARY", "off")
	}
}

func libTestPlugin(desc string) []byte {
	return []byte(`(() => {
const TautPlugin = globalThis.TautPlugin, opt = globalThis.TautOpt;
class LibTest extends TautPlugin {
  static id = 'LibTest'
  static pluginName = 'Lib Test'
  static description = '` + desc + `'
  static authors = ['jeremy']
  static defaultConfig = { enabled: false }
  start() {}
  stop() {}
}
return LibTest
})()`)
}

// a Slick renderer and its privileged half, in build:plugin's / build.mjs's shape
const libSlickPlugin = `// slick-plugin {"id":"LibSlick","name":"Lib Slick","description":"A library test plugin for Slick"}
(globalThis.__slickPlugins ??= []).push((require) => {
var import_slick = require("$slick");
class LibSlick extends import_slick.SlickPlugin {
  static id = "LibSlick";
  static pluginName = "Lib Slick";
  static description = "A library test plugin for Slick";
  static category = "messages";
  static settings = {};
  static requiredAPIs = [];
  start() {}
}
return LibSlick;
});
`

const libSlickMain = `// slick-main {"id":"LibSlick"}
(globalThis.__slickMainPlugins ??= []).push((require) => {
return { id: "LibSlick", capabilities: [] };
});
`

// libServer serves a synthetic plugins release; publish swaps its files.
type libServer struct {
	mu    sync.Mutex
	files map[string][]byte
	srv   *httptest.Server
}

func newLibServer(t *testing.T) *libServer {
	l := &libServer{files: map[string][]byte{}}
	l.srv = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		l.mu.Lock()
		b, ok := l.files[strings.TrimPrefix(r.URL.Path, "/rel/")]
		l.mu.Unlock()
		if !ok {
			http.NotFound(w, r)
			return
		}
		_, _ = w.Write(b)
	}))
	t.Cleanup(l.srv.Close)
	t.Setenv("CLACK_PLUGIN_LIBRARY", l.srv.URL+"/rel/index.json")
	return l
}

// publish writes index.json for tautCode / the Slick pair, plus listing-only
// entries (never installed) so the page has several categories to show.
func (l *libServer) publish(tautCode []byte) {
	l.mu.Lock()
	defer l.mu.Unlock()
	sc, sm := []byte(libSlickPlugin), []byte(libSlickMain)
	ix := pluginlib.Index{Schema: 1,
		Sources: map[string]pluginlib.Source{
			pluginlib.Taut:  {Name: "Taut", Repo: "https://github.com/jeremy46231/taut", Commit: "6fe5123e3b4cffaa", License: "GPL-3.0"},
			pluginlib.Slick: {Name: "Slick", Repo: "https://github.com/3kh0/slick", Commit: "f52d55dbd458feb0", License: "GPL-3.0"},
		},
		Plugins: []pluginlib.Entry{
			{System: pluginlib.Taut, ID: "LibTest", Name: "Lib Test", Description: "A library test plugin", Category: "messages",
				CategoryLabel: "Messages", Authors: []pluginlib.Author{{Name: "Jeremy", URL: "https://jeremywoolley.com"}, {Name: "Rowan"}},
				File: pluginlib.File{File: "taut-LibTest.js", SHA256: pluginlib.Hash(tautCode)}},
			{System: pluginlib.Slick, ID: "LibSlick", Name: "Lib Slick", Description: "A library test plugin for Slick", Category: "messages",
				CategoryLabel: "Messages", Contributors: []string{"synthetic-dev"},
				File: pluginlib.File{File: "slick-LibSlick.js", SHA256: pluginlib.Hash(sc)}, Main: &pluginlib.File{File: "slick-LibSlick.main.js", SHA256: pluginlib.Hash(sm)}},
			{System: pluginlib.Taut, ID: "LibOther", Name: "Another Example", Description: "Listed only, never installed", Category: "fun",
				CategoryLabel: "Fun", Authors: []pluginlib.Author{{Name: "Example Author"}}, File: pluginlib.File{File: "taut-LibOther.js", SHA256: pluginlib.Hash([]byte("x"))}},
			{System: pluginlib.Slick, ID: "LibClub", Name: "Club Example", Description: "A Hack Club-only example", Category: "hackclub",
				CategoryLabel: "Hack Club Specialities", HackClubOnly: true, File: pluginlib.File{File: "slick-LibClub.js", SHA256: pluginlib.Hash([]byte("y"))}},
			{System: pluginlib.Slick, ID: "LibOff", Name: "Off Example", Description: "Turned off upstream", Category: "privacy",
				CategoryLabel: "Privacy", UpstreamDisabled: true, File: pluginlib.File{File: "slick-LibOff.js", SHA256: pluginlib.Hash([]byte("z"))}},
		}}
	b, _ := json.Marshal(ix)
	l.files["index.json"], l.files["taut-LibTest.js"] = b, tautCode
	l.files["slick-LibSlick.js"], l.files["slick-LibSlick.main.js"] = sc, sm
}

func (l *libServer) set(name string, b []byte) {
	l.mu.Lock()
	l.files[name] = b
	l.mu.Unlock()
}

// Preferences > Plugin library is one tab for both systems: it merges the
// release's Taut and Slick entries (each tagged with its system, grouped by
// category), installs each into its own system after checking its sha256
// (Taut: one bundle; Slick: renderer, then the privileged half), offers an
// update when the release's bundle changes, and refuses a tampered one.
func TestLibraryMergesTautAndSlick(t *testing.T) {
	ls := newLibServer(t)
	v1, v2 := libTestPlugin("one"), libTestPlugin("two")
	ls.publish(v1)

	a, _ := ttApp(t, t.TempDir())
	a.data.startTaut(a)
	t.Cleanup(func() { a.data.stopTaut() })
	tt, sl := a.Taut(), a.Slick()
	if tt == nil || sl == nil {
		t.Fatal("taut or slick didn't start")
	}
	pm := &prefsModal{a: a, clicks: map[string]*widget.Clickable{}}
	pm.selectTab(libraryTabID) // the old Library tab: Plugins > Browse
	defer pm.close()
	shots := os.Getenv("TAUT_SHOTS_DIR")
	r := &ttRenderer{t: t, a: a, sz: image.Pt(800, 1400), dir: shots}
	if shots != "" {
		w, err := pixeltest.NewWindow(r.sz.X, r.sz.Y)
		if err != nil {
			t.Fatalf("headless window: %v", err)
		}
		r.win = w
		defer w.Release()
	}
	dialog := func(gtx layout.Context) { pm.layout(gtx) }
	tall := func(gtx layout.Context) {
		gtx.Constraints = layout.Exact(image.Pt(600, r.sz.Y))
		pm.plugins.layout(gtx)
	}
	r.frame(dialog)
	if pm.section != pluginsTabID || pm.plugins == nil || pm.plugins.mode != "browse" {
		t.Fatalf("the library opened section %q", pm.section)
	}
	lp := pm.plugins.lib
	wait := func(what string, cond func() bool) {
		t.Helper()
		deadline := time.Now().Add(30 * time.Second)
		for !cond() {
			if time.Now().After(deadline) {
				t.Fatalf("timed out waiting for %s (status %v, load error %q)", what, lp.status, lp.loadErr)
			}
			r.frame(dialog)
			time.Sleep(10 * time.Millisecond)
		}
	}
	wait("the index", func() bool { return lp.ix != nil && !lp.loading })

	// one list, both systems, Taut's and Slick's category order, by name within one
	es := libraryEntries(lp.ix)
	var got []string
	for _, e := range es {
		got = append(got, e.System+":"+e.ID+"@"+libCategory(e).Label)
	}
	want := []string{"slick:LibSlick@Messages", "taut:LibTest@Messages", "slick:LibOff@Privacy", "taut:LibOther@Fun", "slick:LibClub@Hack Club"}
	if strings.Join(got, " ") != strings.Join(want, " ") {
		t.Fatalf("entries\n got %v\nwant %v", got, want)
	}
	counts := func() map[string]int {
		m := map[string]int{}
		for _, f := range lp.chips() { // the current index
			m[f.ID] = f.Count
		}
		return m
	}
	if c := counts(); c["all"] != 5 || c["taut"] != 2 || c["slick"] != 3 || c["installed"] != 0 || c["updates"] != 0 {
		t.Errorf("filters %v", c)
	}
	if lp.sourceLine() != "<https://github.com/jeremy46231/taut/tree/6fe5123e3b4cffaa|Taut 6fe5123> and <https://github.com/3kh0/slick/tree/f52d55dbd458feb0|Slick f52d55d>" {
		t.Errorf("source line %q", lp.sourceLine())
	}
	if c := libraryCredit(es[1]); c != "<https://jeremywoolley.com|Jeremy>, Rowan" {
		t.Errorf("taut credit %q", c)
	}
	if c := libraryCredit(es[0]); c != "synthetic-dev" {
		t.Errorf("slick credit (contributors) %q", c)
	}
	lp.s.filter = pluginlib.Slick
	for _, g := range libraryGroups(es) {
		if lp.matches(g, nil) != (g.first().System == pluginlib.Slick) {
			t.Errorf("Slick filter: %s/%s", g.first().System, g.first().ID)
		}
	}
	lp.s.filter = "all"
	if !libraryMatches(es[0], []string{"slick", "messages"}) || libraryMatches(es[1], []string{"slick"}) {
		t.Error("search doesn't match on the system name")
	}
	r.save("plugins-browse-light", r.frame(dialog))
	r.save("plugins-browse-light-full", r.frame(tall), r.sz.Y)

	// install each into its own system
	byID := func(id string) pluginlib.Entry {
		for _, e := range libraryEntries(lp.ix) {
			if e.ID == id {
				return e
			}
		}
		t.Fatalf("no entry %s", id)
		return pluginlib.Entry{}
	}
	lp.installEntry(byID("LibTest"), false)
	lp.installEntry(byID("LibSlick"), false)
	wait("the installs", func() bool { return lp.status["taut/LibTest"] != "" && lp.status["slick/LibSlick"] != "" })
	if lp.failed["taut/LibTest"] || tt.InstalledSHA256("LibTest") != pluginlib.Hash(v1) {
		t.Fatalf("taut install: %q, installed %q", lp.status["taut/LibTest"], tt.InstalledSHA256("LibTest"))
	}
	if rc, mc := sl.InstalledSHA256("LibSlick"); lp.failed["slick/LibSlick"] || rc != pluginlib.Hash([]byte(libSlickPlugin)) || mc != pluginlib.Hash([]byte(libSlickMain)) {
		t.Fatalf("slick install: %q, installed %q %q", lp.status["slick/LibSlick"], rc, mc)
	}
	if tt.InstalledSHA256("LibSlick") != "" {
		t.Error("the Slick entry landed in Taut")
	}
	if rc, _ := sl.InstalledSHA256("LibTest"); rc != "" {
		t.Error("the Taut entry landed in Slick")
	}
	found := false
	for _, c := range tt.Settings().Categories {
		for _, pl := range c.Plugins {
			found = found || (pl.ID == "LibTest" && pl.IsUser)
		}
	}
	if !found {
		t.Error("the Library's Taut plugin isn't listed as a Taut user plugin")
	}
	found = false
	for _, pl := range sl.Settings().Plugins {
		found = found || (pl.ID == "LibSlick" && pl.User && pl.HasMain)
	}
	if !found {
		t.Error("the Library's Slick plugin isn't listed as a Slick user plugin with its main half")
	}
	if c := counts(); c["installed"] != 2 || c["updates"] != 0 {
		t.Errorf("filters after install %v", c)
	}

	// the release moves on: the Taut entry offers an update, which replaces the code
	ls.publish(v2)
	lp.load(true)
	wait("the new index", func() bool { return !lp.loading && lp.state(byID("LibTest")) == pluginlib.UpdateAvailable })
	if lp.state(byID("LibSlick")) != pluginlib.Installed {
		t.Error("the unchanged Slick entry offers an update")
	}
	if c := counts(); c["updates"] != 1 {
		t.Errorf("filters with an update %v", c)
	}
	r.save("plugins-browse-update-light", r.frame(dialog))
	for _, dark := range []bool{true, false} {
		a.th.SetDark(dark)
		mode := map[bool]string{true: "dark", false: "light"}[dark]
		r.save("plugins-browse-installed-"+mode+"-full", r.settle(tall, 0, nil), r.sz.Y)
	}
	pm.plugins.s.query.SetText("slick") // the search Installed and Browse share
	r.save("plugins-browse-search-light", r.frame(dialog))
	pm.plugins.s.query.SetText("")

	delete(lp.status, "taut/LibTest")
	lp.installEntry(byID("LibTest"), true)
	wait("the update", func() bool { return lp.status["taut/LibTest"] != "" })
	if lp.failed["taut/LibTest"] || tt.InstalledSHA256("LibTest") != pluginlib.Hash(v2) {
		t.Fatalf("update: %q", lp.status["taut/LibTest"])
	}

	// a bundle that doesn't match the index is refused
	ls.set("taut-LibTest.js", []byte("tampered"))
	delete(lp.status, "taut/LibTest")
	lp.installEntry(byID("LibTest"), true)
	wait("the tampered install", func() bool { return lp.status["taut/LibTest"] != "" })
	if !lp.failed["taut/LibTest"] || !strings.Contains(lp.status["taut/LibTest"], "integrity") || tt.InstalledSHA256("LibTest") != pluginlib.Hash(v2) {
		t.Errorf("tampered: %q", lp.status["taut/LibTest"])
	}

	// Settings on an installed entry opens its system's own page under Plugins
	pm.selectTab("taut:LibTest")
	if pm.section != pluginsTabID || pm.taut.screen != "plugin" || pm.taut.plugin != "LibTest" {
		t.Errorf("taut settings: section %q screen %q plugin %q", pm.section, pm.taut.screen, pm.taut.plugin)
	}
	r.frame(dialog)
	pm.selectTab("slick_plugins:LibSlick")
	r.frame(dialog)
	if pm.section != pluginsTabID || pm.plugins.s.open != "LibSlick" || pm.taut.screen == "plugin" {
		t.Errorf("slick settings: section %q open %q (taut screen %q)", pm.section, pm.plugins.s.open, pm.taut.screen)
	}
	// both installed plugins are in the merged Installed list, tagged
	var merged []string
	for _, u := range pm.plugins.installed() {
		merged = append(merged, u.System+":"+u.ID)
	}
	if got := strings.Join(merged, " "); !strings.Contains(got, "taut:LibTest") || !strings.Contains(got, "slick:LibSlick") {
		t.Errorf("installed list %q", got)
	}
	// and the old Library tab id comes back to Plugins > Browse
	pm.plugins.s.open = ""
	pm.plugins.setMode("installed")
	pm.selectTab(libraryTabID)
	if pm.section != pluginsTabID || pm.plugins.mode != "browse" {
		t.Errorf("Taut's library row: section %q mode %q", pm.section, pm.plugins.mode)
	}
}

// Taut shows authors as Slack mentions in the Hack Club Slack and as names
// (linked to their page) anywhere else, joined with ", ".
func TestTautAuthorsMrkdwn(t *testing.T) {
	pl := taut.PluginInfo{Authors: []taut.Author{{Name: "Jeremy", SlackID: "U06UYA5GMB5", URL: "https://jeremywoolley.com"}, {Name: "adryd", URL: "https://adryd.com"}, {Name: "A <b>"}}}
	if got := tautAuthorsMrkdwn(pl, true); got != "<@U06UYA5GMB5|Jeremy>, <https://adryd.com|adryd>, A &lt;b&gt;" {
		t.Errorf("hack club: %q", got)
	}
	if got := tautAuthorsMrkdwn(pl, false); got != "<https://jeremywoolley.com|Jeremy>, <https://adryd.com|adryd>, A &lt;b&gt;" {
		t.Errorf("elsewhere: %q", got)
	}
	old := taut.PluginInfo{AuthorsMrkdwn: "<@U06UYA5GMB5>, <@U0NOBODY01|Someone>, <@U0NOBODY02>"}
	if got := tautAuthorsMrkdwn(old, false); got != "<https://jeremywoolley.com|Jeremy>, Someone, a Hack Club Slack member" {
		t.Errorf("mrkdwn elsewhere: %q", got)
	}
	if got := tautAuthorsMrkdwn(old, true); got != "<@U06UYA5GMB5|Jeremy>, <@U0NOBODY01|Someone>, <@U0NOBODY02>" {
		t.Errorf("mrkdwn in the Hack Club Slack: %q", got)
	}
	if got := tautMrkdwnHere("say hi in <#C0A057686SF>", true); got != "say hi in <#C0A057686SF|taut>" {
		t.Errorf("channel in the Hack Club Slack: %q", got)
	}
	if got := tautMrkdwnHere("say hi in <#C0A057686SF>", false); got != "say hi in <https://hackclub.slack.com/archives/C0A057686SF|#taut on the Hack Club Slack>" {
		t.Errorf("channel: %q", got)
	}
}

// showNotification carries Slack's message object; a plugin that removes it
// (ShutUpSlackbot, StreamerMode) silences the notification, and one that
// rewrites its text (Censorship) changes what it shows.
func TestNotificationText(t *testing.T) {
	orig := map[string]any{"text": "the job fair", "user": "U1"}
	if _, b, ok := notificationText(map[string]any{"title": "T", "message": orig}, orig, "the job fair"); !ok || b != "the job fair" {
		t.Errorf("unchanged: %q %v", b, ok)
	}
	if _, _, ok := notificationText(map[string]any{"title": "T"}, orig, "x"); ok {
		t.Error("a removed message still notified")
	}
	if _, _, ok := notificationText(map[string]any{"title": "T", "message": nil}, orig, "x"); ok {
		t.Error("a null message still notified")
	}
	if _, b, ok := notificationText(map[string]any{"title": "T", "message": map[string]any{"text": "the *** fair"}}, orig, "the job fair"); !ok || b != "the *** fair" {
		t.Errorf("rewritten: %q", b)
	}
	if ti, b, _ := notificationText(map[string]any{"title": "New", "message": "plain"}, orig, "x"); ti != "New" || b != "plain" {
		t.Errorf("string message: %q %q", ti, b)
	}
}
