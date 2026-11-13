package core

import (
	"context"
	"encoding/json"
	"net/url"
	"reflect"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/some-du6e/clack/internal/fakeslack"
	"github.com/some-du6e/clack/internal/frecency"
	"github.com/some-du6e/clack/internal/slack"
	"github.com/some-du6e/clack/internal/store"
)

const expFrecencyFU = "autocomplete_frequently_used_emojis_frecency"

// TestFrecencyGrid: on Enterprise Grid a full users.prefs.get has
// frecency "" (and frecency_jumper ""), and the account's frecency is
// frecency_ent_jumper, which Slack serves as "frecency" to the named
// loadFrecencyPrefs request. α.48 took the full map's "" as the account's
// frecency, so with autocomplete_frequently_used_emojis_frecency on
// (count order over the frecency's emoji records, emoji_use unused)
// Frequently Used was Slack's five defaults. Now Frequently Used, the
// jumper's records and the saves all go through the named pref.
func TestFrecencyGrid(t *testing.T) {
	fs, err := fakeslack.Start(fakeslack.Small)
	if err != nil {
		t.Fatal(err)
	}
	defer fs.Close()
	fs.UseGridFrecency()
	var mu sync.Mutex
	var gets, sets []url.Values
	fs.SetHook(func(method string, form url.Values) (any, bool) {
		mu.Lock()
		defer mu.Unlock()
		switch method {
		case "users.prefs.get":
			gets = append(gets, form)
		case "users.prefs.set":
			sets = append(sets, form)
		}
		return nil, false
	})
	defer fs.SetHook(nil)
	a := New(slack.New(fs.Creds()), store.New(t.TempDir()))
	defer a.Shutdown()
	now := time.UnixMilli(1_790_000_000_000)
	var clockMu sync.Mutex
	a.SetFrecencyClock(func() time.Time { clockMu.Lock(); defer clockMu.Unlock(); return now })
	advance := func(d time.Duration) { clockMu.Lock(); now = now.Add(d); clockMu.Unlock() }
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	if err := a.Boot(ctx); err != nil {
		t.Fatal(err)
	}

	// boot: the full map, then the named fetch, as Slack's fetchDeferredData
	mu.Lock()
	named := 0
	for _, g := range gets {
		if g.Get("prefs") == "emoji_use,frecency" && g.Get("reason") == "fetch-frecency-prefs" {
			named++
		}
	}
	mu.Unlock()
	if named != 1 {
		t.Fatalf("boot sent %d loadFrecencyPrefs requests, want 1 (gets %v)", named, gets)
	}
	if v, _ := fs.Pref("frecency"); v != "" {
		t.Fatalf("fixture: the plain frecency pref = %v, want \"\"", v)
	}

	// the jumper's store: channel, member and menu-action records
	c := a.FrecencyCache()
	for id, n := range map[string]int{"C0GENERAL": 9, "U0FAKE00001": 4, "MAcopy_link": 3, "E1f680": 5} {
		if r := c.Item(id); r == nil || r.Count != n {
			t.Errorf("FrecencyCache %s = %+v, want count %d", id, r, n)
		}
	}
	if h := c.Hits("gen", now)["C0GENERAL"]; h == nil || !h.Exact {
		t.Errorf(`jumper hit for "gen" = %+v`, h)
	}

	// experiment off: the searcher's order (item count), filled up from
	// emoji_use (smile 10, eyes 9, heart 4 → padded with the defaults)
	a.SetExperimentGroup(expFrecencyFU, "off")
	if got, want := a.FrequentlyUsedEmoji(), []string{"rocket", "+1", "thumbs-up", "smile", "eyes"}; !reflect.DeepEqual(got, want) {
		t.Errorf("Frequently Used (off) = %v, want %v", got, want)
	}
	// experiment on (the account's group): bEcp M, the frecency's own emoji
	// records by count (the toned wave folded), then the defaults to 5
	a.SetExperimentGroup(expFrecencyFU, "on")
	wantOn := []string{"rocket", "+1", "wave", "thumbs-up", "slightly_smiling_face"}
	if got := a.FrequentlyUsedEmoji(); !reflect.DeepEqual(got, wantOn) {
		t.Errorf("Frequently Used (on) = %v, want %v", got, wantOn)
	}

	// a later full users.prefs.get (pref_change) keeps the account's
	// frecency instead of the map's ""
	if _, err := a.RefreshPrefs(ctx); err != nil {
		t.Fatal(err)
	}
	if got := a.FrequentlyUsedEmoji(); !reflect.DeepEqual(got, wantOn) {
		t.Errorf("after a full users.prefs.get: %v, want %v", got, wantOn)
	}

	// a pick: users.prefs.set name=frecency, stored as frecency_ent_jumper
	// with every record kept; M follows the use at once
	a.RecordEmojiFrecency("tada", ":ta")
	entSaved := func(sub string) bool {
		v, _ := fs.Pref("frecency_ent_jumper")
		s, _ := v.(string)
		return strings.Contains(s, sub)
	}
	deadline := time.Now().Add(10 * time.Second)
	for !entSaved(`"E1f389"`) && time.Now().Before(deadline) {
		time.Sleep(20 * time.Millisecond)
	}
	for _, sub := range []string{`"E1f389"`, `"ta":{"id":"E1f389"`, `"MAcopy_link"`, `"U0FAKE00001"`, `"Ethumbs-up"`, `"gen":[`} {
		if !entSaved(sub) {
			v, _ := fs.Pref("frecency_ent_jumper")
			t.Fatalf("frecency_ent_jumper after the save lacks %s: %v", sub, v)
		}
	}
	if v, _ := fs.Pref("frecency"); v != "" {
		t.Errorf("the plain frecency pref was written: %v", v)
	}
	mu.Lock()
	if len(sets) == 0 || sets[0].Get("name") != "frecency" {
		t.Errorf("users.prefs.set forms %v, want name=frecency", sets)
	}
	mu.Unlock()
	if got, want := a.FrequentlyUsedEmoji(), []string{"rocket", "+1", "wave", "tada", "thumbs-up"}; !reflect.DeepEqual(got, want) {
		t.Errorf("Frequently Used after tada (on) = %v, want %v", got, want)
	}

	// the jumper records into the same pref: a channel picked for "gen"
	advance(2 * frecencySaveEvery)
	a.RecordFrecency("C0GENERAL", "gen")
	deadline = time.Now().Add(10 * time.Second)
	for !entSaved(`"C0GENERAL":{"id":"C0GENERAL","count":10`) && time.Now().Before(deadline) {
		time.Sleep(20 * time.Millisecond)
	}
	if !entSaved(`"C0GENERAL":{"id":"C0GENERAL","count":10`) {
		v, _ := fs.Pref("frecency_ent_jumper")
		t.Fatalf("jumper record not saved to frecency_ent_jumper: %v", v)
	}
	if v, _ := fs.Pref("frecency"); v != "" {
		t.Errorf("the plain frecency pref was written: %v", v)
	}

	// the named fetch (search_frecency "updated") reads it back
	if err := a.LoadFrecencyPrefs(ctx); err != nil {
		t.Fatal(err)
	}
	if r := a.FrecencyCache().Item("C0GENERAL"); r == nil || r.Count != 10 {
		t.Errorf("C0GENERAL after reload = %+v", r)
	}
}

// TestFrecencyFullMapIgnored: the full users.prefs.get's frecency never
// becomes the account's, on any workspace: before the named fetch answers
// it is dropped, and Frequently Used isn't cached.
func TestFrecencyFullMapIgnored(t *testing.T) {
	a, _ := testApp(t)
	a.setExperiments(map[string]string{})
	a.installPrefs(map[string]json.RawMessage{"frecency": json.RawMessage(`""`), "emoji_use": json.RawMessage(`"{\"smile\":3}"`)})
	if _, ok := a.Pref("frecency"); ok {
		t.Fatal("the full map's frecency was kept")
	}
	if got, want := a.FrequentlyUsedEmoji(), frecency.UsedEmojiFromPref(map[string]float64{"smile": 3}, a.emojiSet(), frecency.FrequentMax, frecency.FrequentMin); !reflect.DeepEqual(got, want) {
		t.Errorf("Frequently Used = %v, want %v", got, want)
	}
	// the named answer wins over a later full map
	a.mergePrefs(map[string]json.RawMessage{"frecency": json.RawMessage(`"{\"E1f680\":{\"id\":\"E1f680\",\"count\":1,\"visits\":[1]}}"`)})
	a.installPrefs(map[string]json.RawMessage{"frecency": json.RawMessage(`""`)})
	if r := a.FrecencyCache().Item("E1f680"); r == nil {
		t.Error("a full map replaced the named frecency")
	}
}
