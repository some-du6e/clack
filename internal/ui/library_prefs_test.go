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

	"gioui.org/op"

	"github.com/some-du6e/yoloslackgoclient/internal/pluginlib"
	"github.com/some-du6e/yoloslackgoclient/internal/ui/icons"
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

// Preferences > Taut > Library lists the release's index, installs an entry
// as a user plugin after checking its sha256, and offers an update when the
// release's bundle changes.
func TestLibraryInstallsAndUpdatesTautPlugin(t *testing.T) {
	var mu sync.Mutex
	files := map[string][]byte{}
	publish := func(code []byte) {
		mu.Lock()
		defer mu.Unlock()
		ix := pluginlib.Index{Schema: 1,
			Sources: map[string]pluginlib.Source{pluginlib.Taut: {Name: "Taut", Repo: "https://github.com/jeremy46231/taut", Commit: "6fe5123e3b4cffaa", License: "GPL-3.0"}},
			Plugins: []pluginlib.Entry{{System: pluginlib.Taut, ID: "LibTest", Name: "Lib Test", Description: "A library test plugin",
				CategoryLabel: "Other", Authors: []pluginlib.Author{{Name: "Jeremy"}}, File: pluginlib.File{File: "taut-LibTest.js", SHA256: pluginlib.Hash(code)}}}}
		b, _ := json.Marshal(ix)
		files["index.json"], files["taut-LibTest.js"] = b, code
	}
	v1, v2 := libTestPlugin("one"), libTestPlugin("two")
	publish(v1)
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		mu.Lock()
		b, ok := files[strings.TrimPrefix(r.URL.Path, "/rel/")]
		mu.Unlock()
		if !ok {
			http.NotFound(w, r)
			return
		}
		_, _ = w.Write(b)
	}))
	defer srv.Close()
	t.Setenv("CLACK_PLUGIN_LIBRARY", srv.URL+"/rel/index.json")

	a, _ := ttApp(t, t.TempDir())
	a.data.startTaut(a)
	tt := a.Taut()
	if tt == nil {
		t.Fatal("taut didn't start")
	}
	t.Cleanup(func() { a.data.stopTaut() })
	p := newTautPage(a)
	p.go_("library", "")
	draw := func() {
		gtx := ttContext(new(op.Ops), image.Pt(800, 2000))
		a.slackcFrame(gtx)
		p.layout(gtx)
		a.drain()
	}
	wait := func(what string, cond func() bool) {
		t.Helper()
		deadline := time.Now().Add(20 * time.Second)
		for !cond() {
			if time.Now().After(deadline) {
				t.Fatalf("timed out waiting for %s (status %v, load error %q)", what, p.lib.status, p.lib.loadErr)
			}
			draw()
			time.Sleep(10 * time.Millisecond)
		}
	}
	draw()
	wait("the index", func() bool { return p.lib != nil && p.lib.ix != nil })
	es := p.lib.ix.For(pluginlib.Taut)
	if len(es) != 1 || pluginlib.StateOf(es[0], tt.InstalledSHA256("LibTest"), "") != pluginlib.NotInstalled {
		t.Fatalf("entries %+v", es)
	}
	if got := p.lib.sourceLine(); got != "from Taut · GPLv3 · upstream commit 6fe5123" {
		t.Errorf("source line %q", got)
	}

	p.lib.installEntry(p.libraryHost(tt), es[0], false)
	wait("the install", func() bool { return p.lib.status["LibTest"] != "" })
	if p.lib.failed["LibTest"] || tt.InstalledSHA256("LibTest") != pluginlib.Hash(v1) {
		t.Fatalf("install: %q, installed %q", p.lib.status["LibTest"], tt.InstalledSHA256("LibTest"))
	}
	found := false
	for _, c := range tt.Settings().Categories {
		for _, pl := range c.Plugins {
			found = found || (pl.ID == "LibTest" && pl.IsUser)
		}
	}
	if !found {
		t.Error("the Library plugin isn't listed as a user plugin")
	}

	// the release moves on: the entry offers an update, which replaces the code
	publish(v2)
	p.lib.load(true)
	wait("the new index", func() bool {
		return !p.lib.loading && pluginlib.StateOf(p.lib.ix.For(pluginlib.Taut)[0], tt.InstalledSHA256("LibTest"), "") == pluginlib.UpdateAvailable
	})
	delete(p.lib.status, "LibTest")
	p.lib.installEntry(p.libraryHost(tt), p.lib.ix.For(pluginlib.Taut)[0], true)
	wait("the update", func() bool { return p.lib.status["LibTest"] != "" })
	if p.lib.failed["LibTest"] || tt.InstalledSHA256("LibTest") != pluginlib.Hash(v2) {
		t.Fatalf("update: %q", p.lib.status["LibTest"])
	}

	// a bundle that doesn't match the index is refused
	mu.Lock()
	files["taut-LibTest.js"] = []byte("tampered")
	mu.Unlock()
	delete(p.lib.status, "LibTest")
	p.lib.installEntry(p.libraryHost(tt), p.lib.ix.For(pluginlib.Taut)[0], true)
	wait("the tampered install", func() bool { return p.lib.status["LibTest"] != "" })
	if !p.lib.failed["LibTest"] || !strings.Contains(p.lib.status["LibTest"], "integrity") || tt.InstalledSHA256("LibTest") != pluginlib.Hash(v2) {
		t.Errorf("tampered: %q", p.lib.status["LibTest"])
	}
}

// Every Slick tab's icon is registered (Library first drew the "missing" box).
func TestSlickTabIconsRegistered(t *testing.T) {
	for _, tab := range slickTabs {
		if !icons.Has(tab.icon) {
			t.Errorf("Slick tab %s: icon %q isn't registered", tab.id, tab.icon)
		}
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
