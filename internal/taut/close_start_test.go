package taut

import (
	"errors"
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
