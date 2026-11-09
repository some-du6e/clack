package core

import (
	"context"
	"encoding/json"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/some-du6e/yoloslackgoclient/internal/fakeslack"
	"github.com/some-du6e/yoloslackgoclient/internal/frecency"
	"github.com/some-du6e/yoloslackgoclient/internal/slack"
	"github.com/some-du6e/yoloslackgoclient/internal/store"
)

// The frecency pref round-trips through fakeslack the way Slack's client
// keeps it: Frequently Used is computed from users.prefs.get's frecency
// and emoji_use, a recorded use changes the pref at once, is saved with
// users.prefs.set (the first at once, later ones once per minute), and
// the 4 h Frequently Used cache holds until the frecency prefs reload.
func TestFrecencyFakeslack(t *testing.T) {
	fs, err := fakeslack.Start(fakeslack.Small)
	if err != nil {
		t.Fatal(err)
	}
	defer fs.Close()
	a := New(slack.New(fs.Creds()), store.New(t.TempDir()))
	defer a.Shutdown()
	now := time.UnixMilli(1_790_000_000_000)
	a.SetFrecencyClock(func() time.Time { return now })
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	if err := a.Boot(ctx); err != nil {
		t.Fatal(err)
	}

	// fakeslack's frecency: +1 ×6, tada ×4, fire ×2; emoji_use fills two
	want := []string{"+1", "tada", "fire", "smile", "eyes"}
	if got := a.FrequentlyUsedEmoji(); !reflect.DeepEqual(got, want) {
		t.Fatalf("Frequently Used = %v, want %v", got, want)
	}

	// a reaction (no text), then the picker with a query
	a.RecordEmojiFrecency("rocket", "")
	c := a.FrecencyCache()
	if r := c.Item("E1f680"); r == nil || r.Count != 1 || !r.Reduced {
		t.Fatalf("rocket record = %+v", r)
	}
	if c.Item("C0GENERAL") == nil {
		t.Fatal("the channel record was dropped")
	}
	saved := func(sub string) bool {
		v, _ := fs.Pref("frecency")
		s, _ := v.(string)
		return strings.Contains(s, sub)
	}
	deadline := time.Now().Add(10 * time.Second)
	for !saved(`"E1f680"`) && time.Now().Before(deadline) {
		time.Sleep(20 * time.Millisecond)
	}
	if !saved(`"E1f680"`) || !saved(`"C0GENERAL":{"id":"C0GENERAL","count":12,"visits":[1762540000000]}`) {
		v, _ := fs.Pref("frecency")
		t.Fatalf("users.prefs.set frecency = %v", v)
	}
	// within the minute: held back until the trailing save
	now = now.Add(10 * time.Second)
	a.RecordEmojiFrecency("tada::skin-tone-3", ":TA")
	time.Sleep(100 * time.Millisecond)
	if saved(`"ta":{"id":"E1f389","count":3`) {
		t.Fatal("second save inside the throttle window")
	}
	if r := a.FrecencyCache().Item("E1f389"); r == nil || r.Count != 5 {
		t.Fatalf("tada locally = %+v", r)
	}
	a.FlushFrecency()
	if !saved(`"ta":{"id":"E1f389","count":3`) {
		v, _ := fs.Pref("frecency")
		t.Fatalf("trailing save = %v", v)
	}

	// Slack keeps the computed list for 4 h
	if got := a.FrequentlyUsedEmoji(); !reflect.DeepEqual(got, want) {
		t.Errorf("cached list changed: %v", got)
	}
	// search_frecency "updated": both prefs reloaded, the cache dropped
	if err := a.LoadFrecencyPrefs(ctx); err != nil {
		t.Fatal(err)
	}
	if got, w := a.FrequentlyUsedEmoji(), []string{"+1", "tada", "fire", "rocket", "smile"}; !reflect.DeepEqual(got, w) {
		t.Errorf("after reload = %v, want %v", got, w)
	}
	// expiry: 4 h later it is computed afresh
	a.RecordEmojiFrecency("rocket", "")
	a.RecordEmojiFrecency("rocket", "")
	now = now.Add(frecency.FrequentTTL + time.Minute)
	if got, w := a.FrequentlyUsedEmoji(), []string{"+1", "tada", "rocket", "fire", "smile"}; !reflect.DeepEqual(got, w) { // rocket ×3 passes fire ×2
		t.Errorf("after expiry = %v, want %v", got, w)
	}
	a.FlushFrecency()
	var m map[string]json.RawMessage
	v, _ := fs.Pref("frecency")
	if json.Unmarshal([]byte(v.(string)), &m) != nil || m["E1f680"] == nil {
		t.Errorf("saved frecency isn't the pref object: %v", v)
	}
}

func TestEmojiFrecencyIDs(t *testing.T) {
	for name, want := range map[string]string{
		"+1": "E1f44d", "thumbsup": "E1f44d", ":tada:": "E1f389", "wave::skin-tone-4": "E1f44b",
		"relaxed": "E263a-fe0f", "partyparrot": "Epartyparrot",
	} {
		if got := EmojiFrecencyID(name); got != want {
			t.Errorf("EmojiFrecencyID(%q) = %q, want %q", name, got, want)
		}
	}
	if EmojiFrecencyName("E1f44d-1f3fc") != "+1" || EmojiFrecencyName("Epartyparrot") != "partyparrot" || EmojiFrecencyName("C1") != "" {
		t.Error("EmojiFrecencyName")
	}
}
