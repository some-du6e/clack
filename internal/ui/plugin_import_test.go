package ui

import (
	"context"
	"crypto/sha256"
	"encoding/json"
	"fmt"
	"image"
	"io/fs"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"sync"
	"testing"
	"time"

	"gioui.org/io/key"
	"gioui.org/layout"
	"gioui.org/widget"

	"github.com/some-du6e/clack/internal/pluginimport"
	"github.com/some-du6e/clack/internal/pluginlib"
	"github.com/some-du6e/clack/internal/ui/theme"
)

// Tests never find this machine's own Slick or Taut unless they point
// pluginimport at a fixture.
func init() {
	for _, k := range []string{"SLICK_HANDOFF_PROFILE", "TAUT_CONFIG_DIR"} {
		if os.Getenv(k) == "" {
			os.Setenv(k, filepath.Join(os.TempDir(), "clack-tests-none", strings.ToLower(k)))
		}
	}
}

func writeFixture(t *testing.T, path, s string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(s), 0o600); err != nil {
		t.Fatal(err)
	}
}

// dirSnapshot is every file under dir: its mode, time and contents' hash.
func dirSnapshot(t *testing.T, dir string) map[string]string {
	t.Helper()
	out := map[string]string{}
	err := filepath.WalkDir(dir, func(p string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		st, err := d.Info()
		if err != nil {
			return err
		}
		v := st.Mode().String() + " " + st.ModTime().String()
		if !d.IsDir() {
			b, err := os.ReadFile(p)
			if err != nil {
				return err
			}
			v += fmt.Sprintf(" %x", sha256.Sum256(b))
		}
		out[p] = v
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	return out
}

// importFixtures is a Slick and a Taut install with plugins in and out of
// the Library, and a Library that has some of them; it returns both
// folders and the index.
func importFixtures(t *testing.T) (slick, taut string, ls *libServer) {
	t.Helper()
	slick, taut = t.TempDir(), t.TempDir()
	t.Setenv("SLICK_HANDOFF_PROFILE", slick)
	t.Setenv("TAUT_CONFIG_DIR", taut)
	writeFixture(t, filepath.Join(slick, "slick", "settings.json"), `{"plugins": {
		"LibSlick": {"enabled": false, "color": "red"},
		"BetterHuddles": {"enabled": true}
	}}`)
	writeFixture(t, filepath.Join(slick, "profile", "Cookies"), "session data")
	writeFixture(t, filepath.Join(taut, "config.json"), `{"plugins": {
		"LibTest": {"enabled": true, "speed": 5, "token": ""},
		"Ghost": {"enabled": true}
	}}`)
	writeFixture(t, filepath.Join(taut, "user-plugins", "MyTaut.js"), "(() => { class MyTaut {}; return MyTaut })()")
	writeFixture(t, filepath.Join(taut, "secrets.dat"), "secret")
	ls = newLibServer(t)
	code, sc, sm := libTestPlugin("one"), []byte(libSlickPlugin), []byte(libSlickMain)
	huddles := []byte("synthetic BetterHuddles")
	ix := pluginlib.Index{Schema: 1, Plugins: []pluginlib.Entry{
		{System: pluginlib.Taut, ID: "LibTest", Name: "Lib Test", File: pluginlib.File{File: "taut-LibTest.js", SHA256: pluginlib.Hash(code)}},
		{System: pluginlib.Slick, ID: "LibSlick", Name: "Lib Slick",
			File: pluginlib.File{File: "slick-LibSlick.js", SHA256: pluginlib.Hash(sc)}, Main: &pluginlib.File{File: "slick-LibSlick.main.js", SHA256: pluginlib.Hash(sm)}},
		{System: pluginlib.Slick, ID: "BetterHuddles", Name: "Better Huddles", File: pluginlib.File{File: "slick-BetterHuddles.js", SHA256: pluginlib.Hash(huddles)}},
	}}
	b, _ := json.Marshal(ix)
	ls.mu.Lock()
	ls.files["index.json"], ls.files["taut-LibTest.js"] = b, code
	ls.files["slick-LibSlick.js"], ls.files["slick-LibSlick.main.js"] = sc, sm
	ls.files["slick-BetterHuddles.js"] = huddles
	ls.mu.Unlock()
	return slick, taut, ls
}

// fakeImport records what an import does in each system, in order.
type fakeImport struct {
	mu    sync.Mutex
	calls []string
}

func (fk *fakeImport) note(s string) {
	fk.mu.Lock()
	fk.calls = append(fk.calls, s)
	fk.mu.Unlock()
}

func (fk *fakeImport) got() []string {
	fk.mu.Lock()
	defer fk.mu.Unlock()
	return append([]string(nil), fk.calls...)
}

func (fk *fakeImport) importer(a *App) *importer {
	return &importer{a: a,
		systems: func(sys string) (importSystem, bool) {
			return importSystem{
				installed: func(string) bool { return false },
				importCode: func(_ context.Context, code []byte) error {
					fk.note(sys + " import " + pluginlib.Hash(code)[:8])
					return nil
				},
				setOption: func(_ context.Context, id, k string, v any) error {
					fk.note(fmt.Sprintf("%s %s.%s=%v", sys, id, k, v))
					return nil
				},
				setEnabled: func(_ context.Context, id string, on bool) error {
					fk.note(fmt.Sprintf("%s %s on=%v", sys, id, on))
					return nil
				},
				isSecret: func(_, key string) bool { return key == "token" },
			}, true
		},
		hosts: func(sys string) (libraryHost, bool) {
			return libraryHost{install: func(_ context.Context, e pluginlib.Entry, code, main []byte, _ bool) error {
				m := ""
				if main != nil {
					m = " +main"
				}
				fk.note(sys + " install " + e.ID + " " + pluginlib.Hash(code)[:8] + m)
				return nil
			}}, true
		},
	}
}

// Bringing Slick's and Taut's plugins over (Karim, 2026-11-23): each
// plugin another app has is listed by its Library name, with the on or
// off it has there; a plugin the Library says doesn't work in Clack, or
// one the Library hasn't got (and the app no file of), is listed with why
// and can't be picked. What's picked comes over through the Library's
// install (its download, sha256-checked), a user plugin as its own file,
// then gets its settings and, where its app set it, its on or off. The
// other apps' folders are only read.
func TestPluginImportBringsPluginsOver(t *testing.T) {
	slick, taut, _ := importFixtures(t)
	beforeSlick, beforeTaut := dirSnapshot(t, slick), dirSnapshot(t, taut)
	a, _ := ttApp(t, t.TempDir())
	ctx := context.Background()
	ix, err := a.pluginLib().Index(ctx, true)
	if err != nil {
		t.Fatal(err)
	}
	items := importItems(ix, pluginimport.Find(), nil)
	type row struct {
		key, name, reason string
		on, pick          bool
	}
	var got []row
	for _, it := range items {
		got = append(got, row{it.key(), it.name, it.reason, it.on, it.pick})
	}
	want := []row{
		{"slick/BetterHuddles", "Better Huddles", pluginlib.CompatOf(pluginlib.Slick, "BetterHuddles").Line, true, false},
		{"slick/LibSlick", "Lib Slick", "", false, false},
		{"taut/Ghost", "Ghost", "Not in the plugin Library", true, false},
		{"taut/LibTest", "Lib Test", "", true, true},
		{"taut/MyTaut", "MyTaut", "", false, false},
	}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("items\n got %+v\nwant %+v", got, want)
	}
	if want[0].reason == "" || !strings.HasPrefix(want[0].reason, "Doesn't work in Clack") {
		t.Fatalf("BetterHuddles' compat row: %q", want[0].reason)
	}
	// the user picks the ones that are off there too; the others can't be
	for i := range items {
		switch items[i].found.ID {
		case "LibSlick", "MyTaut", "BetterHuddles", "Ghost":
			items[i].pick = true
		}
	}
	fk := &fakeImport{}
	rs := fk.importer(a).run(ctx, items)
	wantCalls := []string{
		"slick install LibSlick " + pluginlib.Hash([]byte(libSlickPlugin))[:8] + " +main",
		"slick LibSlick.color=red",
		"slick LibSlick on=false",
		"taut install LibTest " + pluginlib.Hash(libTestPlugin("one"))[:8],
		"taut LibTest.speed=5",
		"taut LibTest on=true",
		"taut import " + pluginlib.Hash([]byte("(() => { class MyTaut {}; return MyTaut })()"))[:8],
	}
	if calls := fk.got(); !reflect.DeepEqual(calls, wantCalls) {
		t.Errorf("the import did\n %q\nwant\n %q", calls, wantCalls)
	}
	if len(rs) != 3 {
		t.Fatalf("%d results, want the 3 that can come", len(rs))
	}
	for _, r := range rs {
		if r.err != nil {
			t.Errorf("%s: %v", r.it.key(), r.err)
		}
	}
	if s := importSummary(rs); s != "Brought over 3 plugins." {
		t.Errorf("summary %q", s)
	}
	if !reflect.DeepEqual(dirSnapshot(t, slick), beforeSlick) || !reflect.DeepEqual(dirSnapshot(t, taut), beforeTaut) {
		t.Error("the import changed something in Slick's or Taut's folder")
	}
}

// A pick that fails (its download, here) stays for the next start, and
// the toast says what failed, without the check mark; the picks that came
// over go.
func TestPluginImportKeepsFailedPicks(t *testing.T) {
	importFixtures(t)
	a, _ := ttApp(t, t.TempDir())
	ix, err := a.pluginLib().Index(context.Background(), true)
	if err != nil {
		t.Fatal(err)
	}
	items := importItems(ix, pluginimport.Find(), nil)
	for i := range items {
		items[i].pick = items[i].reason == "" && (items[i].found.ID == "LibTest" || items[i].found.ID == "LibSlick")
	}
	a.savePendingImport(items)
	fk := &fakeImport{}
	old := importerFor
	importerFor = func(a *App) *importer {
		im := fk.importer(a)
		hosts := im.hosts
		im.hosts = func(sys string) (libraryHost, bool) {
			h, ok := hosts(sys)
			if sys == pluginlib.Slick {
				h.install = func(context.Context, pluginlib.Entry, []byte, []byte, bool) error {
					return fmt.Errorf("the Library's download stopped")
				}
			}
			return h, ok
		}
		return im
	}
	t.Cleanup(func() { importerFor = old })
	a.resumePendingImport()
	deadline := time.Now().Add(30 * time.Second)
	for a.toast.text == "" {
		if time.Now().After(deadline) {
			t.Fatalf("no toast (calls %q)", fk.got())
		}
		a.drain()
		time.Sleep(10 * time.Millisecond)
	}
	if !strings.Contains(a.toast.text, "Couldn't bring over Lib Slick") || !strings.Contains(a.toast.text, "try again") {
		t.Errorf("toast %q", a.toast.text)
	}
	b, err := os.ReadFile(a.pendingImportPath())
	if err != nil || string(b) != `{"picks":["slick/LibSlick"]}` {
		t.Errorf("the kept picks: %s, %v; want the one that failed", b, err)
	}
}

// The introduction's picks wait for the sign-in: kept in Clack's config
// folder, then brought over once the plugin systems run, from the apps as
// they are then, and the toast says how it went. The picks go after one
// run.
func TestPluginImportAfterSignIn(t *testing.T) {
	importFixtures(t)
	a, _ := ttApp(t, t.TempDir())
	ix, err := a.pluginLib().Index(context.Background(), true)
	if err != nil {
		t.Fatal(err)
	}
	items := importItems(ix, pluginimport.Find(), nil)
	a.savePendingImport(items) // the defaults: Lib Test
	b, err := os.ReadFile(a.pendingImportPath())
	if err != nil {
		t.Fatal(err)
	}
	if string(b) != `{"picks":["taut/LibTest"]}` {
		t.Fatalf("the kept picks: %s", b)
	}
	fk := &fakeImport{}
	old := importerFor
	importerFor = fk.importer
	t.Cleanup(func() { importerFor = old })
	a.resumePendingImport()
	deadline := time.Now().Add(30 * time.Second)
	for a.toast.text == "" {
		if time.Now().After(deadline) {
			t.Fatalf("no toast after the sign-in's import (calls %q)", fk.got())
		}
		a.drain()
		time.Sleep(10 * time.Millisecond)
	}
	if a.toast.text != "Brought over Lib Test." {
		t.Errorf("toast %q", a.toast.text)
	}
	if calls := fk.got(); len(calls) == 0 || !strings.HasPrefix(calls[0], "taut install LibTest") {
		t.Errorf("calls %q", calls)
	}
	if _, err := os.Stat(a.pendingImportPath()); !os.IsNotExist(err) {
		t.Errorf("the picks stayed after their run: %v", err)
	}
}

// The first launch's plugins beat offers Slick's and Taut's plugins when
// an install is found: its card lists them in place of its illustration,
// it waits there for an answer (no auto-advance), a switch changes a
// pick, and Continue (here "Bring them over") keeps the picks for after
// the sign-in. Without an install the beat is as it was
// (TestIntroAutoAdvanceAndSkip).
func TestIntroOffersPluginImport(t *testing.T) {
	importFixtures(t)
	t.Setenv("CLACK_PLUGIN_LIBRARY", "off") // the offer by ids, the Library not reached
	a := introApp(t, "", nil)
	e := newIntroEnv(t, a, image.Pt(1280, 800))
	e.started()
	in := a.intro
	for i := 0; i < 300 && len(in.imp.installs) == 0; i++ {
		e.frame(time.Second / 60)
		time.Sleep(time.Millisecond)
	}
	if len(in.imp.installs) != 2 {
		t.Fatalf("the intro found %d installs, want Slick's and Taut's", len(in.imp.installs))
	}
	for in.cur < beatPlugins {
		e.key(key.NameRightArrow)
	}
	if in.cur != beatPlugins || !in.importing(a) {
		t.Fatalf("beat %d, offering %v", in.cur, in.importing(a))
	}
	rec := []introDrawn{}
	in.rec = &rec
	e.run(introDwell[beatPlugins] + 2*time.Second)
	if in.cur != beatPlugins {
		t.Fatalf("the plugins beat moved on by itself to %d while it offered the import", in.cur)
	}
	names := map[string]bool{}
	for _, d := range rec {
		names[d.name] = true
	}
	if !names["import-card"] || names["plugins-card"] {
		t.Errorf("the beat drew import-card %v, plugins-card %v", names["import-card"], names["plugins-card"])
	}
	in.rec = nil
	// LibSlick is off in Slick: its switch picks it
	var slickPick func() bool
	for i := range in.imp.items {
		if it := &in.imp.items[i]; it.key() == "slick/LibSlick" {
			slickPick = func() bool { return in.imp.items[i].pick }
			if it.pick {
				t.Fatal("LibSlick, off in Slick, was picked")
			}
		}
	}
	if slickPick == nil {
		t.Fatal("no LibSlick in the offer")
	}
	in.importBtn("slick/LibSlick").Click()
	e.frame(time.Second / 60)
	if !slickPick() {
		t.Error("its switch didn't pick it")
	}
	in.nextBtn.Click()
	e.frame(time.Second / 60)
	if in.cur != beatHackClub {
		t.Errorf("Bring them over went to beat %d", in.cur)
	}
	b, err := os.ReadFile(a.pendingImportPath())
	if err != nil {
		t.Fatal(err)
	}
	var pi pendingImport
	if err := json.Unmarshal(b, &pi); err != nil {
		t.Fatal(err)
	}
	// Ghost too: with no index the intro can't tell the Library hasn't
	// got it (the run after the sign-in leaves it out, with the index)
	if got := strings.Join(pi.Picks, " "); got != "slick/LibSlick taut/Ghost taut/LibTest" {
		t.Errorf("kept picks %q, want LibSlick (picked), Ghost and LibTest (on in Taut)", got)
	}
}

// Preferences > Plugins has a row to bring Slick's and Taut's plugins over
// when either is found, and its screen lists them, brings the picked ones
// over, and says how each went.
func TestPluginImportScreen(t *testing.T) {
	importFixtures(t)
	a, _ := ttApp(t, t.TempDir())
	a.data.startTaut(a)
	t.Cleanup(func() { a.data.stopTaut() })
	if a.Taut() == nil {
		t.Fatal("taut didn't start")
	}
	fk := &fakeImport{}
	old := importerFor
	importerFor = fk.importer
	t.Cleanup(func() { importerFor = old })
	pm := &prefsModal{a: a, clicks: map[string]*widget.Clickable{}}
	pm.selectTab(pluginsTabID)
	defer pm.close()
	r := &ttRenderer{t: t, a: a, sz: image.Pt(800, 1200)}
	draw := func() string {
		var b strings.Builder
		theme.DrawHook = func(s string) { b.WriteString(s + "\n") }
		defer func() { theme.DrawHook = nil }()
		r.frame(func(gtx layout.Context) { pm.layout(gtx) })
		return b.String()
	}
	until := func(what string, cond func(text string) bool) string {
		t.Helper()
		deadline := time.Now().Add(30 * time.Second)
		for {
			text := draw()
			if cond(text) {
				return text
			}
			if time.Now().After(deadline) {
				t.Fatalf("timed out waiting for %s:\n%s", what, text)
			}
			time.Sleep(10 * time.Millisecond)
		}
	}
	until("the Plugins row to bring them over", func(s string) bool { return strings.Contains(s, "Bring over your plugins") })
	pm.pluginsPage().openScreen("import")
	text := until("the screen's list", func(s string) bool { return strings.Contains(s, "Lib Test") && strings.Contains(s, "Lib Slick") })
	for _, want := range []string{"From Slick", "From Taut", "Better Huddles", "Not in the plugin Library", "MyTaut (off there) (its own file)"} {
		if !strings.Contains(text, want) {
			t.Errorf("the screen doesn't say %q:\n%s", want, text)
		}
	}
	pm.taut.btn("import.run").Click()
	until("how it went", func(s string) bool { return strings.Contains(s, "Brought over Lib Test.") })
	if calls := fk.got(); len(calls) == 0 || !strings.HasPrefix(calls[0], "taut install LibTest") {
		t.Errorf("calls %q", calls)
	}
}
