package ui

import (
	"context"
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
	"gioui.org/op"
	"gioui.org/unit"
	"gioui.org/widget"

	"github.com/some-du6e/clack/internal/pluginlib"
	"github.com/some-du6e/clack/internal/slack"
	"github.com/some-du6e/clack/internal/store"
	"github.com/some-du6e/clack/internal/taut"
	"github.com/some-du6e/clack/internal/ui/theme"
	"github.com/some-du6e/clack/internal/ui/theme/pixeltest"
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
	if c := (creditResolver{}).libraryCredit(es[1]); c != "Jeremy, Rowan" { // outside the Hack Club Slack: names, never links
		t.Errorf("taut credit %q", c)
	}
	if c := (creditResolver{hc: true}).libraryCredit(es[1]); c != "<@U06UYA5GMB5|Jeremy>, <@U080A3QP42C|Rowan>" {
		t.Errorf("taut credit in the Hack Club Slack %q", c)
	}
	if c := (creditResolver{}).libraryCredit(es[0]); c != "synthetic-dev" {
		t.Errorf("slick credit (contributors) %q", c)
	}
	lp.s.filter = pluginlib.Slick
	for _, g := range libraryGroups(es) {
		if lp.matches(g, nil) != (g.first().System == pluginlib.Slick) {
			t.Errorf("Slick filter: %s/%s", g.first().System, g.first().ID)
		}
	}
	lp.s.filter = "all"
	// the search matches names and descriptions, never a category or a
	// system name alone ("Lib Slick" is under Messages)
	if !libraryMatches(es[0], []string{"slick"}) || libraryMatches(es[0], []string{"slick", "messages"}) || libraryMatches(es[1], []string{"slick"}) {
		t.Error("the search matched on a category or system name, or missed a name")
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
	// Install turns each on in its own system, no restart (Karim: no
	// "turn it on" step; LibTest's own default is off)
	for _, k := range []string{"taut/LibTest", "slick/LibSlick"} {
		if lp.status[k] != "Installed and turned on." {
			t.Errorf("%s after Install: %q", k, lp.status[k])
		}
	}
	wait("both on and running", func() bool {
		tOn, sOn := false, false
		for _, c := range tt.Settings().Categories {
			for _, pl := range c.Plugins {
				tOn = tOn || pl.ID == "LibTest" && pl.Enabled && pl.Running
			}
		}
		for _, pl := range sl.Settings().Plugins {
			sOn = sOn || pl.ID == "LibSlick" && pl.Enabled && pl.Running
		}
		return tOn && sOn
	})
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

// Plugin credits draw as Slack user mentions (plugin_credit.go): a Slack
// id from the data in the Hack Club Slack (or wherever that member is
// loaded), a unique member name match for people the data names without
// one, and plain text, never a link, for anyone else.
func TestPluginCreditMentions(t *testing.T) {
	st := store.New("")
	st.PutUsers([]slack.User{
		{ID: "U0ECHO0001", Name: "echo", Profile: slack.Profile{DisplayName: "Echo", RealName: "Echo Example"}},
		{ID: "U0TWIN0001", Name: "twin1", Profile: slack.Profile{DisplayName: "Twin"}},
		{ID: "U0TWIN0002", Name: "twin2", Profile: slack.Profile{DisplayName: "twin"}},
		{ID: "U0GONE0001", Name: "gone", Deleted: true, Profile: slack.Profile{DisplayName: "Gone"}},
		{ID: "U0BOT00001", Name: "botty", IsBot: true, Profile: slack.Profile{DisplayName: "Botty"}},
		{ID: "U0WORK0001", Name: "jeremy", Profile: slack.Profile{DisplayName: "Jeremy"}}, // a namesake, not Taut's Jeremy
		{ID: "U0PAGE0001", Name: "page", Profile: slack.Profile{DisplayName: "Paige"}},
	})
	hc, here := creditResolver{s: st, hc: true}, creditResolver{s: st}
	pl := taut.PluginInfo{Authors: []taut.Author{{Name: "Jeremy", SlackID: "U06UYA5GMB5", URL: "https://jeremywoolley.com"}, {Name: "adryd", URL: "https://adryd.com"}, {Name: "A <b>"}}}
	if got := hc.tautAuthors(pl); got != "<@U06UYA5GMB5|Jeremy>, adryd, A &lt;b&gt;" {
		t.Errorf("hack club: %q", got)
	}
	if got := here.tautAuthors(pl); got != "Jeremy, adryd, A &lt;b&gt;" { // Taut's Jeremy isn't U0WORK0001
		t.Errorf("elsewhere: %q", got)
	}
	old := taut.PluginInfo{AuthorsMrkdwn: "<@U06UYA5GMB5>, <@U0NOBODY01|Someone>, <@U0NOBODY02>"}
	if got := here.tautAuthors(old); got != "Jeremy, Someone, a Hack Club Slack member" {
		t.Errorf("mrkdwn elsewhere: %q", got)
	}
	if got := hc.tautAuthors(old); got != "<@U06UYA5GMB5|Jeremy>, <@U0NOBODY01|Someone>, <@U0NOBODY02>" {
		t.Errorf("mrkdwn in the Hack Club Slack: %q", got)
	}
	// contributors and Slick's names: one live human member with the name
	e := pluginlib.Entry{Contributors: []string{"echo example", "Twin", "Gone", "Botty", "Nobody Here"}}
	if got := here.libraryCredit(e); got != "<@U0ECHO0001|echo example>, Twin, Gone, Botty, Nobody Here" {
		t.Errorf("contributors: %q", got)
	}
	// an id the data carries that this workspace has; a /team/ URL
	e = pluginlib.Entry{Authors: []pluginlib.Author{{Name: "Echo", SlackID: "U0ECHO0001"}, {Name: "Paige", URL: "https://acme.slack.com/team/U0PAGE0001"},
		{Name: "Ghost", URL: "https://acme.slack.com/team/U0NOPE0001"}, {Name: "Club", URL: "https://hackclub.slack.com/team/U0CLUB0001"}, {Name: "Site", URL: "https://example.com"}}}
	if got := here.libraryCredit(e); got != "<@U0ECHO0001|Echo>, <@U0PAGE0001|Paige>, Ghost, Club, Site" {
		t.Errorf("ids here: %q", got)
	}
	if got := hc.libraryCredit(e); got != "<@U0ECHO0001|Echo>, <@U0PAGE0001|Paige>, Ghost, <@U0CLUB0001|Club>, Site" {
		t.Errorf("ids in the Hack Club Slack: %q", got)
	}
	if got := here.mrkdwn("<@U0ECHO0001>, Paige and <@U0NOBODY03|Nemo>"); got != "<@U0ECHO0001|Echo>, <@U0PAGE0001|Paige>, Nemo" {
		t.Errorf("slick authors: %q", got)
	}
	for _, got := range []string{here.tautAuthors(pl), hc.tautAuthors(pl), here.libraryCredit(e), hc.libraryCredit(e)} {
		if strings.Contains(got, "http") {
			t.Errorf("a credit links somewhere: %q", got)
		}
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

// The search suggestions (Preferences > Plugins with nothing installed
// that matches) rank the library's plugins by how they match: the name's
// start, a word of the name, the name, then a word of the description.
// A category's or an author's name alone is no match (Karim, α.37:
// "message" suggested everything filed under Messages). Names and
// descriptions are the published library's.
func TestPluginSuggestionsRank(t *testing.T) {
	lib := []pluginlib.Entry{
		{System: pluginlib.Slick, ID: "MessageLogger", Name: "Message Logger", Description: "Keep deleted and edited messages visible", CategoryLabel: "Messages"},
		{System: pluginlib.Taut, ID: "SlimMessageBox", Name: "Slim Message Box", Description: "Fits the message box on one line and hides the buttons you don't use", CategoryLabel: "Message Box"},
		{System: pluginlib.Taut, ID: "CopyReacted", Name: "Copy Reacted", Description: "Copies the list of people who reacted to a message", CategoryLabel: "Messages"},
		{System: pluginlib.Slick, ID: "CopyReacted", Name: "Copy Reacted", Description: "Copy the list of people who reacted to a message", CategoryLabel: "Messages"},
		{System: pluginlib.Taut, ID: "BringBackSlackbot", Name: "Bring Back Slackbot", Description: "Shows Slackbot's custom responses as sent by Slackbot again", CategoryLabel: "Messages"},
		{System: pluginlib.Slick, ID: "CustomSlackbot", Name: "Custom Slackbot", Description: "Customize the name and avatar used for workspace custom responses", CategoryLabel: "Messages",
			Authors: []pluginlib.Author{{Name: "messagebird"}}},
		{System: pluginlib.Taut, ID: "ShutUpSlackbot", Name: "Shut Up Slackbot", Description: "Marks Slackbot's \"an app took over your slash command\" DMs as read and silences them", CategoryLabel: "Messages"},
		{System: pluginlib.Slick, ID: "DeArrow", Name: "DeArrow", Description: "Replace clickbait YouTube titles and thumbnails with crowdsourced ones", CategoryLabel: "Messages"},
		{System: pluginlib.Slick, ID: "IgnoreTime", Name: "Ignore Time", Description: "Makes Slack not copy that stupid little timestamp", CategoryLabel: "Messages"},
		{System: pluginlib.Slick, ID: "CallTimer", Name: "Call Timer", Description: "Show how long you’ve been in a huddle", CategoryLabel: "Huddles & sounds"},
		{System: pluginlib.Slick, ID: "LocalTime", Name: "Local Time", Description: "Show each person's local time beside their messages and in their profile, even while they're away.", CategoryLabel: "Other"},
	}
	groups := libraryGroups(lib)
	names := func(q string) []string {
		var out []string
		for _, g := range rankGroups(groups, strings.Fields(q), nil) {
			out = append(out, g.first().Name)
		}
		return out
	}
	for _, c := range []struct {
		q    string
		want []string
	}{
		// the name first; then descriptions that say it; never Bring Back
		// Slackbot, Custom Slackbot (an author is "messagebird"), DeArrow
		// or Ignore Time, which are only filed under Messages
		{"message", []string{"Message Logger", "Slim Message Box", "Copy Reacted", "Local Time"}},
		{"slackbot", []string{"Bring Back Slackbot", "Custom Slackbot", "Shut Up Slackbot"}},
		{"time", []string{"Call Timer", "Ignore Time", "Local Time"}},
		{"mesage", nil}, // a typo: no suggestion rather than a guess
		{"copyreacted", []string{"Copy Reacted"}},
		{"messages", []string{"Local Time", "Message Logger"}}, // descriptions only: "Messages" is a category, not a match
		{"is", nil}, // under 3 letters a description word doesn't count
	} {
		if got := names(c.q); strings.Join(got, ", ") != strings.Join(c.want, ", ") {
			t.Errorf("%q: %q, want %q", c.q, got, c.want)
		}
	}
	// "See all N results" counts the same rows the library's search shows
	lp := &libraryPage{s: &slickPage{filter: "all"}}
	n := 0
	for _, g := range groups {
		if lp.matches(g, []string{"message"}) {
			n++
		}
	}
	if want := len(names("message")); n != want {
		t.Errorf("the library's search shows %d rows for \"message\", the suggestions count %d", n, want)
	}
}

// Install turns a plugin on at once, from the library and from its search
// suggestions alike, but one that doesn't work in Clack (its ❌ row in
// docs/PLUGINS-live-*.md: pluginlib.CompatOf) installs and stays off.
// Its line is on the library row and the suggestion before Install.
func TestLibraryInstallTurnsOn(t *testing.T) {
	ls := newLibServer(t)
	a, _ := ttApp(t, t.TempDir())
	var es []pluginlib.Entry
	ls.mu.Lock()
	for _, id := range []string{"MentionProfiles", "BetterHuddles"} {
		code := []byte("synthetic " + id)
		e := pluginlib.Entry{System: pluginlib.Slick, ID: id, Name: id, Description: "A synthetic plugin", Category: "messages",
			File: pluginlib.File{File: "slick-" + id + ".js", SHA256: pluginlib.Hash(code)}}
		es = append(es, e)
		ls.files[e.File.File] = code
	}
	ix, _ := json.Marshal(pluginlib.Index{Schema: 1, Plugins: es})
	ls.files["index.json"] = ix
	ls.mu.Unlock()

	lp := newLibraryPage(a, nil)
	defer lp.close()
	var mu sync.Mutex
	var installed, enabled []string
	lp.hosts = func(string) (libraryHost, bool) {
		return libraryHost{
			installed: func(string) (string, string) { return "", "" },
			install: func(_ context.Context, e pluginlib.Entry, _, _ []byte, _ bool) error {
				mu.Lock()
				installed = append(installed, e.ID)
				mu.Unlock()
				return nil
			},
			enable: func(_ context.Context, id string) error {
				mu.Lock()
				enabled = append(enabled, id)
				mu.Unlock()
				return nil
			},
		}, true
	}
	ops := new(op.Ops)
	gtx := layout.Context{Ops: ops, Metric: unit.Metric{PxPerDp: 1, PxPerSp: 1}, Constraints: layout.Constraints{Max: image.Pt(600, 400)}, Now: time.Now()}
	drawn := func(draw func()) string {
		var b strings.Builder
		theme.DrawHook = func(s string) { b.WriteString(s + "\n") }
		defer func() { theme.DrawHook = nil }()
		ops.Reset()
		draw()
		return b.String()
	}
	groups := libraryGroups(es)
	var bh libGroup
	for _, g := range groups {
		if g.first().ID == "BetterHuddles" {
			bh = g
		}
	}
	const line = "Doesn't work in Clack: needs browser WebRTC"
	if got := drawn(func() { lp.row(gtx, 600, bh) }); !strings.Contains(got, line) {
		t.Errorf("the library row doesn't say %q before Install: %q", line, got)
	}
	pp := &pluginsPage{a: a, s: newSlickPage(a), taut: newTautPage(a), lib: lp}
	if got := drawn(func() { pp.suggestionRow(gtx, 600, bh) }); !strings.Contains(got, line) {
		t.Errorf("the suggestion doesn't say %q before Install: %q", line, got)
	}

	for _, e := range es {
		lp.installEntry(e, false)
	}
	deadline := time.Now().Add(30 * time.Second)
	for lp.status["slick/MentionProfiles"] == "" || lp.status["slick/BetterHuddles"] == "" {
		if time.Now().After(deadline) {
			t.Fatalf("installs didn't finish: %v", lp.status)
		}
		a.drain()
		time.Sleep(10 * time.Millisecond)
	}
	mu.Lock()
	defer mu.Unlock()
	if len(installed) != 2 {
		t.Errorf("installed %v, want both", installed)
	}
	if strings.Join(enabled, ",") != "MentionProfiles" {
		t.Errorf("turned on %v, want MentionProfiles only (BetterHuddles doesn't work in Clack)", enabled)
	}
	if st := lp.status["slick/MentionProfiles"]; st != "Installed and turned on." {
		t.Errorf("MentionProfiles: %q", st)
	}
	if st := lp.status["slick/BetterHuddles"]; st != "Installed and left off: it doesn't work in Clack." {
		t.Errorf("BetterHuddles: %q", st)
	}
}

// A plugin both systems ship credits each person once, in order (Karim
// 2026-11-13: Copy Reacted read "By @Jeremy, @Rowan, @Rowan"): by the
// member they resolve to, else by name.
func TestGroupCreditDedupes(t *testing.T) {
	st := store.New("")
	st.PutUsers([]slack.User{{ID: "U0ECHO0001", Name: "echo", Profile: slack.Profile{DisplayName: "Echo", RealName: "Echo Example"}}})
	g := libGroup{Entries: []pluginlib.Entry{
		{System: pluginlib.Taut, Authors: []pluginlib.Author{{Name: "Jeremy", SlackID: "U06UYA5GMB5"}, {Name: "Rowan", SlackID: "U080A3QP42C"}}},
		{System: pluginlib.Slick, Authors: []pluginlib.Author{{Name: "rowan", SlackID: "U080A3QP42C"}, {Name: "Echo"}}},
		{System: pluginlib.Slick, Contributors: []string{"echo example", "Jeremy", "Nemo", "nemo"}},
	}}
	hc, here := creditResolver{s: st, hc: true}, creditResolver{s: st}
	if got := groupCredit(hc, g); got != "<@U06UYA5GMB5|Jeremy>, <@U080A3QP42C|Rowan>, <@U0ECHO0001|Echo>, Nemo" {
		t.Errorf("hack club: %q", got)
	}
	if got := groupCredit(here, g); got != "Jeremy, Rowan, <@U0ECHO0001|Echo>, Nemo" {
		t.Errorf("elsewhere: %q", got)
	}
	if got := here.mrkdwn("<@U0ECHO0001>, Echo and <@U0ECHO0001|Echo Example>"); got != "<@U0ECHO0001|Echo>" {
		t.Errorf("one member three ways: %q", got)
	}
}
