package composer

import (
	"encoding/json"
	"fmt"
	"os"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/some-du6e/clack/internal/core"
	"github.com/some-du6e/clack/internal/slack"
	"github.com/some-du6e/clack/internal/store"
)

// End to end over the fake API: the @ # : / lists ranked by Slack's
// Searcher (internal/acrank) from the composer's real sources: the
// conversation's members (edge users/list), the store's users, the
// server search (edge users/search, channels/search), usergroups, the
// "frecency" pref and the search.autocomplete.model weights. Synthetic
// people only; nothing here talks to Slack.

// acSlackModel is the search.autocomplete.model answer: the weights
// app.slack.com served on 2026-11-09 (internal/acrank/testdata).
func acSlackModel(t testing.TB) map[string]any {
	t.Helper()
	b, err := os.ReadFile("../../acrank/testdata/remote_model_2026-11-09.json")
	if err != nil {
		t.Fatal(err)
	}
	var w map[string]any
	if err := json.Unmarshal(b, &w); err != nil {
		t.Fatal(err)
	}
	return map[string]any{"ok": true, "model": w}
}

func tsAgo(d time.Duration) string {
	t := time.Now().Add(-d)
	return fmt.Sprintf("%d.000100", t.Unix())
}

// acSlackApp seeds a workspace shaped for the ranking rules.
func acSlackApp(t testing.TB) (*core.App, *fakeSlack) {
	t.Helper()
	person := func(id, display, real string) slack.User {
		return slack.User{ID: id, Name: strings.ToLower(strings.ReplaceAll(real, " ", ".")), RealName: real, TeamID: tTeam,
			Profile: slack.Profile{DisplayName: display, RealName: real, FirstName: strings.Fields(real)[0]}}
	}
	alice := person("UALICE", "alice", "Alice Example")
	app, fs := syntheticApp(t, func(s *store.Store) {
		s.PutUsers([]slack.User{
			person("UALFRED", "alfred", "Alfred Nobel"), // a prefix match, not in #general
			person("UALBA", "alba", "Alba Frecent"),     // picked for "al" lately (frecency)
			person("UZED", "zed", "Zed Alvarez"),        // in #general, matches one word in
			{ID: "UALAN", Name: "alan", RealName: "Alan Gone", Deleted: true, // deactivated
				Profile: slack.Profile{DisplayName: "alan", RealName: "Alan Gone"}},
			{ID: "UPAL", Name: "pal", RealName: "Pal", Profile: slack.Profile{DisplayName: "pal", RealName: "Pal"}}, // "al" inside a word: no match
		})
		s.PutConv(slack.Conversation{ID: "C0GENAI", Name: "gen-ai", IsChannel: true, IsMember: true})
		s.PutConv(slack.Conversation{ID: "C0OLDGEN", Name: "general-old", IsChannel: true, IsMember: true, IsArchived: true})
		s.PutConv(slack.Conversation{ID: "C0TEAMGEN", Name: "team-gen", IsChannel: true, IsMember: true})
		s.SetCounts(&slack.Counts{Channels: []slack.CountEntry{
			{ID: tChGen, LastRead: tsAgo(time.Hour), Latest: tsAgo(time.Hour)},
			{ID: "C0GENAI", LastRead: tsAgo(40 * 24 * time.Hour), Latest: tsAgo(30 * 24 * time.Hour)},
		}})
		s.SetUsergroups([]slack.Usergroup{{ID: "S0ALUM", Handle: "alumni", Name: "Alumni", UserCount: 40}})
		fr, _ := json.Marshal(fmt.Sprintf(`{"al":{"id":"UALBA","count":6,"visits":[%d,%d,%d]},"UALBA":{"id":"UALBA","count":6,"visits":[%d]}}`,
			time.Now().Add(-time.Hour).UnixMilli(), time.Now().Add(-2*time.Hour).UnixMilli(), time.Now().Add(-3*time.Hour).UnixMilli(),
			time.Now().Add(-time.Hour).UnixMilli()))
		s.SetRawPrefs(map[string]json.RawMessage{"frecency": fr})
	})
	members := []slack.User{alice, person("UZED", "zed", "Zed Alvarez"),
		{ID: "UALERT", Name: "alertbot", RealName: "Alertbot", IsBot: true, Profile: slack.Profile{RealName: "Alertbot"}}}
	fs.mu.Lock()
	fs.reply = map[string]any{
		"search.autocomplete.model": acSlackModel(t),
		"edge/users/list":           map[string]any{"ok": true, "results": members},
		"edge/users/search": map[string]any{"ok": true, "results": []slack.User{
			person("UALINA", "alina", "Alina Remote"), // only the server knows her
		}},
		"edge/channels/search": map[string]any{"ok": true, "results": []map[string]any{
			{"id": "C0GENETIC", "name": "genetics", "is_channel": true, "is_member": false},
		}},
		"usergroups.list": map[string]any{"ok": true, "usergroups": []slack.Usergroup{{ID: "S0ALUM", Handle: "alumni", Name: "Alumni", UserCount: 40}}},
	}
	fs.mu.Unlock()
	app.AutocompleteModel() // start the model fetch, as the first list does
	return app, fs
}

// acSettle drives an autocomplete until cond holds over its rows.
func acSettle(t *testing.T, ac *Autocomplete, text string, cond func(names []string) bool) []string {
	t.Helper()
	var mu sync.Mutex
	var q []func()
	ac.later = func(f func()) { mu.Lock(); q = append(q, f); mu.Unlock() }
	d := acDocOf(text)
	var names []string
	deadline := time.Now().Add(8 * time.Second)
	for time.Now().Before(deadline) {
		mu.Lock()
		fs := q
		q = nil
		mu.Unlock()
		for _, f := range fs {
			f()
		}
		ac.snapAt = time.Time{} // re-read the snapshot (counts, frecency, model)
		ac.Update(d, d.Len())
		names = names[:0]
		items, _ := ac.Items()
		for _, it := range items {
			names = append(names, it.Name)
		}
		if cond(names) {
			return names
		}
		time.Sleep(15 * time.Millisecond)
	}
	t.Fatalf("%q never settled: rows %v", text, names)
	return nil
}

func TestAutocompleteSlackPeople(t *testing.T) {
	app, fs := acSlackApp(t)
	ac := newAutocomplete(&slickHost{app: app}, tChGen)
	t.Cleanup(ac.Close)
	got := acSettle(t, ac, "@al", func(n []string) bool { return hasName(n, "Alina Remote") || hasName(n, "alina") })
	t.Logf("@al: %v", got)
	at := func(n string) int { return acIndex(got, n) }
	if at("pal") >= 0 {
		t.Errorf("'al' inside a word matched: %v", got)
	}
	// six recent picks of alba for exactly "al" (FRECENCY_* ≈ +5.7)
	// outweigh being in the channel (MEMBER_OF_CURRENT_CHANNEL +3.05);
	// then the channel members, then the other prefix matches
	if got[0] != "alba" || got[1] != "alice" {
		t.Errorf("the frecent pick, then the channel member: %v", got)
	}
	if at("Alertbot") > at("alfred") || at("zed") < at("alfred") {
		t.Errorf("members first, a word match after the prefix matches: %v", got)
	}
	if at("alan") >= 0 && at("alan") < at("alfred") {
		t.Errorf("deactivated below an active equal match: %v", got)
	}
	if at("Alumni") < 0 {
		t.Errorf("the usergroup is offered: %v", got)
	}

	// the server search's args (getRemoteMembers)
	reqs := fs.called("edge/users/search")
	if len(reqs) == 0 {
		t.Fatal("no users/search")
	}
	var body map[string]any
	_ = json.Unmarshal([]byte(reqs[len(reqs)-1].Get("body")), &body)
	if body["query"] != "al" || body["count"] != float64(25) || body["fuzz"] != float64(1) ||
		body["current_channel"] != tChGen || body["enable_workspace_ranking"] != true {
		t.Errorf("users/search body %v", body)
	}
	if _, ok := body["search_email"]; ok {
		t.Errorf("the composer doesn't send search_email: %v", body)
	}
	if len(fs.called("search.autocomplete.model")) == 0 {
		t.Error("search.autocomplete.model never asked")
	}

	// a pick records {id, text} into the frecency pref and saves it
	items, _ := ac.Items()
	for i, it := range items {
		if it.ID == "UALFRED" {
			ac.accept(i)
		}
	}
	if r := app.FrecencyCache().Hits("al", time.Now())["UALFRED"]; r == nil || !r.Exact {
		t.Errorf("the pick isn't under \"al\": %+v", r)
	}
	deadline := time.Now().Add(5 * time.Second)
	for time.Now().Before(deadline) && len(fs.called("users.prefs.set")) == 0 {
		time.Sleep(10 * time.Millisecond)
	}
	sets := fs.called("users.prefs.set")
	if len(sets) == 0 || sets[0].Get("name") != "frecency" || !strings.Contains(sets[0].Get("value"), `"UALFRED"`) {
		t.Errorf("users.prefs.set %v", sets)
	}
}

func TestAutocompleteSlackChannels(t *testing.T) {
	app, fs := acSlackApp(t)
	ac := newAutocomplete(&slickHost{app: app}, tChGen)
	t.Cleanup(ac.Close)
	got := acSettle(t, ac, "#gen", func(n []string) bool { return hasName(n, "genetics") })
	t.Logf("#gen: %v", got)
	at := func(n string) int { return acIndex(got, n) }
	if got[0] != "general" {
		t.Errorf("the recently read member channel first: %v", got)
	}
	if at("gen-ai") > at("genetics") {
		t.Errorf("member channel before the non-member at equal match: %v", got)
	}
	if at("general-old") < at("gen-ai") {
		t.Errorf("archived after the live prefix matches: %v", got)
	}
	reqs := fs.called("edge/channels/search")
	if len(reqs) == 0 {
		t.Fatal("no channels/search")
	}
	var body map[string]any
	_ = json.Unmarshal([]byte(reqs[len(reqs)-1].Get("body")), &body)
	if body["query"] != "gen" || body["count"] != float64(25) || body["filter"] != "xws" {
		t.Errorf("channels/search body %v", body)
	}
}

func TestAutocompleteSlackCommandsAndEmoji(t *testing.T) {
	app, _ := acSlackApp(t)
	ac := newAutocomplete(&slickHost{app: app}, tChGen)
	t.Cleanup(ac.Close)
	ac.commands = []core.Command{{Name: "/remind", Desc: "Set a reminder"}, {Name: "/rename", Desc: "Rename a channel"},
		{Name: "/feed", Desc: "Manage RSS feeds"}, {Name: "/away", Desc: "Toggle your away status"}}
	got := acSettle(t, ac, "/rem", func(n []string) bool { return len(n) > 0 })
	if got[0] != "remind" {
		t.Errorf("/rem: %v", got)
	}
	got = acSettle(t, newAutocompleteFor(t, app), ":smi", func(n []string) bool { return len(n) > 3 })
	if got[0] != "smile" {
		t.Errorf(":smi: %v", got)
	}
	app.RecordEmojiFrecency("sweat_smile", ":smi")
	app.RecordEmojiFrecency("sweat_smile", ":smi")
	got = acSettle(t, newAutocompleteFor(t, app), ":smi", func(n []string) bool { return len(n) > 0 && n[0] == "sweat_smile" })
	t.Logf(":smi after picks: %v", got)
}

func newAutocompleteFor(t *testing.T, app *core.App) *Autocomplete {
	ac := newAutocomplete(&slickHost{app: app}, tChGen)
	t.Cleanup(ac.Close)
	return ac
}
