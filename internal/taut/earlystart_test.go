package taut

import (
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/some-du6e/clack/internal/core"
	"github.com/some-du6e/clack/internal/slack"
	"github.com/some-du6e/clack/internal/store"
)

// Enabled plugins start while core is still booting (in parallel with the
// network), and reading state that comes from boot doesn't wait for it:
// the plugin's start() reads the sidebar sections through the redux
// mirror, which used to block on core.Snapshot until Boot finished.
func TestPluginsStartBeforeBoot(t *testing.T) {
	dir := t.TempDir()
	early := `class Early extends TautPlugin {
		static id = 'Early'; static pluginName = 'Early'; static description = ''; static authors = []
		static defaultConfig = { enabled: true }
		start() { globalThis.__early = Object.keys(globalThis.__taut.redux.getRawState().channelSections || {}).length + 1 }
	}
	return Early`
	if err := os.MkdirAll(filepath.Join(dir, "plugins"), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "plugins", "Early.js"), pluginJS(early), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "config.json"), []byte(`{"plugins":{"Early":{"enabled":true}}}`), 0o600); err != nil {
		t.Fatal(err)
	}
	api := newFakeAPI(t)
	c := slack.New(slack.Creds{Token: "xoxc-test", Cookie: "xoxd-test%2Fcookie", UserID: selfID, Team: teamID, EnterpriseID: entID,
		TeamURL: api.srv.URL + "/", EnterpriseURL: api.srv.URL + "/"})
	app := core.New(c, store.New("")) // never booted: Ready stays open
	t.Cleanup(app.Shutdown)
	ui := &testUI{trees: map[uint64][]byte{}, treeCh: make(chan uint64, 64), styles: map[string]string{}, inputs: map[string]string{}}
	tt, err := New(Options{App: app, Dir: dir, CredsPath: dir + "/creds.json", UI: ui.hooks(), CacheDir: t.TempDir()})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = tt.Close() })
	select {
	case <-tt.StartDone():
	case <-time.After(20 * time.Second):
		t.Fatal("StartEnabled didn't finish while core was booting")
	}
	select {
	case <-app.Ready():
		t.Fatal("core booted (the test needs it not to)")
	default:
	}
	running := false
	for _, id := range tt.Running() {
		running = running || id == "Early"
	}
	if !running {
		t.Fatalf("Early isn't running before boot (running %v)", tt.Running())
	}
}
