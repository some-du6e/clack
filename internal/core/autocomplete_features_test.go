package core

import (
	"net/url"
	"sync/atomic"
	"testing"
	"time"

	"github.com/some-du6e/clack/internal/fakeslack"
	"github.com/some-du6e/clack/internal/slack"
	"github.com/some-du6e/clack/internal/store"
)

// search.autocomplete.offlineFeatures' priority features (1Fxd
// AutocompleteFeaturesCache): fetched once in the background on first use,
// empty until the answer lands, then kept (an AutocompleteCache, 28 h).
func TestAutocompleteAffinityFetch(t *testing.T) {
	fs, err := fakeslack.Start(fakeslack.Small)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = fs.Close() })
	var calls atomic.Int32
	fs.SetHook(func(method string, _ url.Values) (any, bool) {
		if method != "search.autocomplete.offlineFeatures" {
			return nil, false
		}
		calls.Add(1)
		return map[string]any{"ok": true,
			"user_affinity":        map[string]any{"U0AFFINITY": 0.75, "U0STRING01": "0.5"},
			"channel_priority_90d": map[string]any{"C0PRIORITY": 2.5},
			"top_users":            map[string]any{}}, true
	})
	a := New(slack.New(fs.Creds()), store.New(""))
	users, chans := a.AutocompleteAffinity()
	if len(users) != 0 || len(chans) != 0 {
		t.Fatalf("before the answer: %v %v", users, chans)
	}
	deadline := time.Now().Add(5 * time.Second)
	for {
		users, chans = a.AutocompleteAffinity()
		if len(users) > 0 || time.Now().After(deadline) {
			break
		}
		time.Sleep(10 * time.Millisecond)
	}
	if users["U0AFFINITY"] != 0.75 || users["U0STRING01"] != 0.5 || chans["C0PRIORITY"] != 2.5 {
		t.Fatalf("features: users %v channels %v", users, chans)
	}
	for i := 0; i < 3; i++ {
		a.AutocompleteAffinity()
	}
	time.Sleep(50 * time.Millisecond)
	if n := calls.Load(); n != 1 {
		t.Fatalf("%d offlineFeatures calls, want 1 (cached)", n)
	}
}
