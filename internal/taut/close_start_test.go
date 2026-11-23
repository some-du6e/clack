package taut

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"sync"
	"testing"
	"time"

	"github.com/some-du6e/clack/internal/core"
	"github.com/some-du6e/clack/internal/jsrt"
	"github.com/some-du6e/clack/internal/slack"
	"github.com/some-du6e/clack/internal/store"
)

// Close while a runtime is starting waits for the start, which then
// closes what it made: nothing runs, nor writes into the Dir, after
// Close. Close read no runtime and returned while the start went on
// (its boot writing the bytecode cache into CacheDir, then keeping a live
// runtime on the closed Taut): TestPatchedReactionPillLikeNative's GPU
// skip closed one mid-start, and its TempDir's cleanup found a file the
// boot wrote ("directory not empty", invalidate-relatives' full-test
// 45057092647). The start is held at its beginning (startHook) until
// Close has been called.
func TestCloseWaitsForAStartingRuntime(t *testing.T) {
	api := newFakeAPI(t)
	c := slack.New(slack.Creds{Token: "xoxc-test", Cookie: "xoxd-test%2Fcookie", UserID: selfID, Team: teamID, EnterpriseID: entID,
		TeamURL: api.srv.URL + "/", EnterpriseURL: api.srv.URL + "/"})
	app := core.New(c, store.New(""))
	t.Cleanup(app.Shutdown)
	dir := t.TempDir()
	tt, err := New(Options{App: app, Dir: dir, CredsPath: filepath.Join(dir, "creds.json"), NoAutoStart: true, CacheDir: filepath.Join(dir, "cache")})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = tt.Close() })
	entered, release := make(chan struct{}), make(chan struct{})
	var once sync.Once
	startHook = func(x *Taut) {
		if x == tt {
			once.Do(func() { close(entered); <-release })
		}
	}
	t.Cleanup(func() { startHook = nil })
	started := make(chan error, 1)
	go func() {
		_, err := tt.Runtime()
		started <- err
	}()
	select {
	case <-entered:
	case <-time.After(10 * time.Second):
		t.Fatal("the start never began")
	}
	closed := make(chan struct{})
	go func() {
		_ = tt.Close()
		close(closed)
	}()
	early := false
	select {
	case <-closed:
		early = true
	case <-time.After(300 * time.Millisecond):
	}
	close(release)
	select {
	case err = <-started:
	case <-time.After(30 * time.Second):
		t.Fatal("the start never ended")
	}
	select {
	case <-closed:
	case <-time.After(30 * time.Second):
		t.Fatal("Close never returned")
	}
	if early {
		t.Error("Close returned while the runtime was still starting")
	}
	if r := tt.liveRuntime(); r != nil {
		t.Error("a runtime runs on after Close")
	}
	if !errors.Is(err, jsrt.ErrClosed) {
		t.Errorf("the start that Close overtook gave %v, want jsrt.ErrClosed", err)
	}
}

// A register past its JS load when Close comes writes nothing into the
// Dir after Close: its cached metadata (meta.json) is dropped (whileOpen).
// It was written after Close returned, the same "TempDir RemoveAll
// cleanup: directory not empty" as a start overtaken by Close (the
// flakes-1122d review). The register is held between its load and the
// write (loadedHook) until Close has returned.
func TestCloseDropsARegistersLateWrite(t *testing.T) {
	dir := t.TempDir()
	src := `class Late extends TautPlugin {
		static id = 'Late'; static pluginName = 'Late'; static description = ''; static authors = []
		static defaultConfig = { enabled: false }
		start() {}
	}
	return Late`
	if err := os.MkdirAll(filepath.Join(dir, "plugins"), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "plugins", "Late.js"), pluginJS(src), 0o600); err != nil {
		t.Fatal(err)
	}
	tt := closeTestTaut(t, dir)
	_ = os.Remove(metaPath(dir, "Late")) // only what the held register writes counts
	entered, release := make(chan struct{}), make(chan struct{})
	var once sync.Once
	loadedHook = func(x *Taut, id string) {
		if x == tt && id == "Late" {
			once.Do(func() { close(entered); <-release })
		}
	}
	t.Cleanup(func() { loadedHook = nil })
	done := make(chan error, 1)
	go func() { done <- tt.register(context.Background(), "Late") }()
	select {
	case <-entered:
	case <-time.After(30 * time.Second):
		t.Fatal("the register never loaded the plugin")
	}
	if err := tt.Close(); err != nil {
		t.Logf("Close: %v", err)
	}
	close(release)
	select {
	case <-done:
	case <-time.After(30 * time.Second):
		t.Fatal("the register never ended")
	}
	if _, err := os.Stat(metaPath(dir, "Late")); err == nil {
		t.Error("the register wrote its meta.json into the Dir after Close returned")
	}
}

// Detach and Close each mark the bridge detached: the UI's post of
// Attached binds nothing after them.
func TestBridgeDetached(t *testing.T) {
	tt := closeTestTaut(t, t.TempDir())
	br := tt.Bridge()
	if br.Detached() || !br.Of(tt) {
		t.Fatalf("a fresh bridge: detached %v, of its Taut %v", br.Detached(), br.Of(tt))
	}
	tt.Detach()
	if !br.Detached() {
		t.Error("Detach left the bridge attached")
	}
	t2 := closeTestTaut(t, t.TempDir())
	b2 := t2.Bridge()
	_ = t2.Close()
	if !b2.Detached() {
		t.Error("Close left the bridge attached")
	}
	if b2.Of(tt) || (*Bridge)(nil).Of(tt) {
		t.Error("a bridge is another Taut's")
	}
}

// closeTestTaut is a Taut on dir against the fake API, plugins not started.
func closeTestTaut(t *testing.T, dir string) *Taut {
	t.Helper()
	api := newFakeAPI(t)
	c := slack.New(slack.Creds{Token: "xoxc-test", Cookie: "xoxd-test%2Fcookie", UserID: selfID, Team: teamID, EnterpriseID: entID,
		TeamURL: api.srv.URL + "/", EnterpriseURL: api.srv.URL + "/"})
	app := core.New(c, store.New(""))
	t.Cleanup(app.Shutdown)
	tt, err := New(Options{App: app, Dir: dir, CredsPath: filepath.Join(dir, "creds.json"), NoAutoStart: true, CacheDir: filepath.Join(dir, "cache")})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = tt.Close() })
	return tt
}
