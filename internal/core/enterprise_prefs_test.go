package core

import (
	"context"
	"net/url"
	"sync"
	"testing"
	"time"

	"github.com/some-du6e/clack/internal/fakeslack"
	"github.com/some-du6e/clack/internal/slack"
	"github.com/some-du6e/clack/internal/store"
)

// The enterprise.prefs.get boot starts once it's done (Boot's go
// LoadEnterprisePrefs) and one asked while it was out come back in either
// order, and the older answer, boot's, put the org's prefs as it read them
// back over the newer ones: popups' TestMessageMenuFlagRowLikeSlack drew
// "Flag this message…" after content review went off (push ci 45351365915
// on oneko-data-images), and TestFlagMessageDialogLikeSlack found it off
// before it went on. The fake takes boot's answer as the call comes in
// (content review on) and holds it until a call made after it has
// answered (off); the newer answer stays.
func TestEnterprisePrefsLaterCallWins(t *testing.T) {
	ctx := context.Background()
	fs, err := fakeslack.Start(fakeslack.Small)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { fs.Close() })
	fs.SetEnterpriseLockedPrefs(map[string]any{"content_review_enabled": true})
	held, release := make(chan struct{}), make(chan struct{})
	var first, released sync.Once
	letGo := func() { released.Do(func() { close(release) }) }
	t.Cleanup(letGo)
	fs.SetHook(func(method string, form url.Values) (any, bool) {
		if method != "enterprise.prefs.get" {
			return nil, false
		}
		var out any
		boot := false
		first.Do(func() { boot, out = true, fs.Answer(method, form) })
		if !boot {
			return nil, false
		}
		close(held)
		<-release
		return out, true
	})
	a := New(slack.New(fs.Creds()), store.New(t.TempDir()))
	t.Cleanup(a.Shutdown)
	if err := a.Boot(ctx); err != nil {
		t.Fatal(err)
	}
	select {
	case <-held:
	case <-time.After(10 * time.Second):
		t.Fatal("boot asked for no enterprise prefs")
	}
	fs.SetEnterpriseLockedPrefs(map[string]any{})
	if err := a.LoadEnterprisePrefs(ctx); err != nil {
		t.Fatal(err)
	}
	if a.ContentReviewEnabled() {
		t.Fatal("control: the later call's answer (content review off) didn't go in")
	}
	letGo() // boot's answer, from before, comes back now
	for dl := time.Now().Add(500 * time.Millisecond); time.Now().Before(dl); time.Sleep(5 * time.Millisecond) {
		if a.ContentReviewEnabled() {
			t.Fatal("boot's enterprise.prefs.get, answered after a call made later, put the org's older prefs back (content review on again)")
		}
	}
}
