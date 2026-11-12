package core

import (
	"encoding/json"
	"net/url"
	"sync"
	"testing"
	"time"

	"github.com/some-du6e/clack/internal/slack"
)

// A section's collapsed state is the account's channel_sections "c"
// (setChannelSectionCollapsed, hJIH): read from users.prefs, written back
// with users.prefs.set after the debounce, and live on pref_change.
func TestSectionCollapsedSyncsWithAccount(t *testing.T) {
	fs, a := bootFake(t)
	old := SectionCollapseDelay
	SectionCollapseDelay = time.Hour // the test flushes
	t.Cleanup(func() { SectionCollapseDelay = old })

	var mu sync.Mutex
	var sets []url.Values
	fs.SetHook(func(method string, f url.Values) (any, bool) {
		if method == "users.prefs.set" && f.Get("name") == "channel_sections" {
			mu.Lock()
			sets = append(sets, f)
			mu.Unlock()
		}
		return nil, false
	})
	collapsed := func(id string) bool { return a.sectionCollapsed(slack.Section{ID: id}) }
	prefChange := func() {
		a.handle(slack.Event{Type: "pref_change", Raw: json.RawMessage(`{"type":"pref_change","name":"channel_sections"}`)})
	}

	// read: another client collapsed L0FAKE01 (its Filter value kept)
	fs.SetPrefs(map[string]any{"channel_sections": `{"L0FAKE01":{"c":"1","sidebar":"hid"}}`})
	prefChange()
	if !collapsed("L0FAKE01") || collapsed("L0FAKE02") {
		t.Fatalf("after pref_change: L0FAKE01 %v, L0FAKE02 %v; want true, false", collapsed("L0FAKE01"), collapsed("L0FAKE02"))
	}

	// write: the local pref changes at once, the account after the debounce
	a.SetSectionCollapsed([]string{"L0FAKE01"}, false)
	a.SetSectionCollapsed([]string{"L0FAKE02"}, true)
	if collapsed("L0FAKE01") || !collapsed("L0FAKE02") {
		t.Fatal("local state didn't follow SetSectionCollapsed")
	}
	mu.Lock()
	n := len(sets)
	mu.Unlock()
	if n != 0 {
		t.Fatalf("%d users.prefs.set before the debounce ended", n)
	}
	// a reload while the write waits keeps the local object
	prefChange()
	if collapsed("L0FAKE01") || !collapsed("L0FAKE02") {
		t.Fatal("pref_change during the debounce undid the local toggle")
	}

	a.FlushSectionCollapsed()
	mu.Lock()
	got := sets
	mu.Unlock()
	if len(got) != 1 {
		t.Fatalf("%d users.prefs.set, want 1 (the debounce sends the last object once)", len(got))
	}
	if f := got[0]; f.Get("name") != "channel_sections" || f.Get("reason") != "set_channel_section_collapse_in_home_sidebar" {
		t.Errorf("users.prefs.set %v", f)
	}
	p := channelSectionsPref(t, fs)
	if p["L0FAKE01"]["c"] != "0" || p["L0FAKE01"]["sidebar"] != "hid" || p["L0FAKE02"]["c"] != "1" {
		t.Errorf("account channel_sections %v; want L0FAKE01 c 0 (sidebar hid kept), L0FAKE02 c 1", p)
	}
	a.FlushSectionCollapsed() // nothing pending: no second write
	mu.Lock()
	n = len(sets)
	mu.Unlock()
	if n != 1 {
		t.Errorf("%d users.prefs.set after a second flush, want 1", n)
	}

	// live: the account changes again elsewhere
	fs.SetPrefs(map[string]any{"channel_sections": `{"L0FAKE01":{"c":"1"},"L0FAKE02":{"c":"0"}}`})
	prefChange()
	if !collapsed("L0FAKE01") || collapsed("L0FAKE02") {
		t.Error("pref_change after the write didn't apply the account's state")
	}
}

// The debounce fires on its own after SectionCollapseDelay.
func TestSectionCollapsedDebounceFires(t *testing.T) {
	fs, a := bootFake(t)
	old := SectionCollapseDelay
	SectionCollapseDelay = 20 * time.Millisecond
	t.Cleanup(func() { SectionCollapseDelay = old })
	a.SetSectionCollapsed([]string{"L0FAKE03"}, true)
	deadline := time.Now().Add(5 * time.Second)
	for time.Now().Before(deadline) {
		if channelSectionsPref(t, fs)["L0FAKE03"]["c"] == "1" {
			return
		}
		time.Sleep(10 * time.Millisecond)
	}
	t.Fatalf("channel_sections never got L0FAKE03 c 1: %v", channelSectionsPref(t, fs))
}
