package composer

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"strings"
	"sync"
	"testing"
	"time"

	"gioui.org/io/key"

	"github.com/some-du6e/clack/internal/core"
	"github.com/some-du6e/clack/internal/slack"
	"github.com/some-du6e/clack/internal/store"
	"github.com/some-du6e/clack/internal/ui/popups"
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

// ":tu" then Tab (Karim, 2026-11-14) through the composer: Slack's own
// ranking (a node harness over build efbd526e8fb3, the cases in
// internal/acrank/emoji_slack_test.go) puts tulip first and root_vegetable
// second (only its keyword "turnip" matches), and never offers +1 for
// "tu", however often it's used: a query under 3 letters may skip no
// letters and none of +1's keywords starts with "tu". ":thu" does reach
// it ("thumbsup"), first once its uses count; ":like" reaches it by
// keyword. A pick stored under ":tu" moves an emoji to the top. (In
// Karim's workspace tu is also a custom alias, which Slack lists first:
// TestAutocompleteSlackEmojiCustomAlias.)
func TestAutocompleteSlackEmojiTu(t *testing.T) {
	app, _ := acSlackApp(t)
	noThumbs := func(rows []string) bool { return acIndex(rows, "+1") < 0 && acIndex(rows, "thumbsup") < 0 }
	// with the remote model in, root_vegetable is second (the fallback
	// model puts turkey there)
	got := acSettle(t, newAutocompleteFor(t, app), ":tu", func(n []string) bool { return len(n) > 1 && n[1] == "root_vegetable" })
	t.Logf(":tu cold: %v", got)
	if got[0] != "tulip" || !noThumbs(got) {
		t.Errorf(":tu: Slack's tulip first, no +1: %v", got)
	}
	for _, want := range []string{"turkey", "turtle", "flag-tc", "poultry_leg"} {
		if acIndex(got, want) < 0 {
			t.Errorf(":tu: %s (keyword or name match) missing: %v", want, got)
		}
	}
	got = acSettle(t, newAutocompleteFor(t, app), ":like", func(n []string) bool { return len(n) > 1 })
	if acIndex(got, "+1") < 0 {
		t.Errorf(":like: +1 by its keyword: %v", got)
	}

	// +1 used a lot: reactions record its id key only (text ""), picks
	// under ":thu" the text too
	for i := 0; i < 20; i++ {
		app.RecordEmojiFrecency("+1", "")
	}
	for i := 0; i < 3; i++ {
		app.RecordEmojiFrecency("thumbsup", ":thu")
	}
	got = acSettle(t, newAutocompleteFor(t, app), ":tu", func(n []string) bool { return len(n) > 1 })
	t.Logf(":tu with +1 frecent: %v", got)
	if got[0] != "tulip" || !noThumbs(got) {
		t.Errorf(":tu with +1 frecent: still tulip first, no +1: %v", got)
	}
	got = acSettle(t, newAutocompleteFor(t, app), ":thu", func(n []string) bool { return len(n) > 0 && n[0] == "thumbsup" })
	t.Logf(":thu with +1 frecent: %v", got)

	// picks under ":tu" itself
	for i := 0; i < 3; i++ {
		app.RecordEmojiFrecency("turtle", ":tu")
	}
	got = acSettle(t, newAutocompleteFor(t, app), ":tu", func(n []string) bool { return len(n) > 0 && n[0] == "turtle" })
	if got[1] != "tulip" || !noThumbs(got) {
		t.Errorf(":tu after turtle picks: turtle, then tulip: %v", got)
	}
}

// A frecency that lands while ":tu" is open counts before Tab acts.
// Slack's copy of the pref (turtle picked under "tu") arrives through
// loadFrecencyPrefs after the rows are shown, and nothing is typed. The
// Tab's Update ranks the list again (the frecency version changed), so
// it takes turtle, not the stale first row (tulip).
func TestAutocompleteSlackRanksAgainWhenFrecencyLands(t *testing.T) {
	app, fs := acSlackApp(t)
	// the model first, so that only the frecency changes below
	deadline := time.Now().Add(5 * time.Second)
	for app.AutocompleteModelVersion() == 0 && time.Now().Before(deadline) {
		app.AutocompleteModel()
		time.Sleep(10 * time.Millisecond)
	}
	if app.AutocompleteModelVersion() == 0 {
		t.Fatal("search.autocomplete.model never landed")
	}
	ac := newAutocompleteFor(t, app)
	d := acDocOf(":tu")
	ac.Live = func() (*Doc, int) { return d, d.Len() }
	ac.Update(d, d.Len())
	items, _ := ac.Items()
	if len(items) < 2 || items[0].Name != "tulip" {
		t.Fatalf(":tu before the frecency: %v", acNames(items))
	}

	now := time.Now()
	ms := func(d time.Duration) int64 { return now.Add(-d).UnixMilli() }
	fr := fmt.Sprintf(`{"tu":{"id":"E1f422","count":3,"visits":[%d,%d,%d]},"E1f422":{"id":"E1f422","count":3,"visits":[%d,%d,%d]}}`,
		ms(time.Hour), ms(3*time.Hour), ms(30*time.Hour), ms(time.Hour), ms(3*time.Hour), ms(30*time.Hour))
	fs.mu.Lock()
	fs.reply["users.prefs.get"] = map[string]any{"ok": true, "prefs": map[string]any{"frecency": fr, "emoji_use": "{}"}}
	fs.mu.Unlock()
	if err := app.LoadFrecencyPrefs(context.Background()); err != nil {
		t.Fatal(err)
	}
	if items, _ := ac.Items(); items[0].Name != "tulip" {
		t.Fatalf("rows changed without an Update: %v", acNames(items))
	}
	if !ac.Key(key.NameTab, 0) {
		t.Fatal("Tab not taken")
	}
	it, _, _, ok := ac.Accepted()
	if !ok || it.Name != "turtle" {
		t.Errorf("Tab after the frecency landed took %q (ok %v), want turtle", it.Name, ok)
	}
}

func newAutocompleteFor(t *testing.T, app *core.App) *Autocomplete {
	ac := newAutocomplete(&slickHost{app: app}, tChGen)
	t.Cleanup(ac.Close)
	return ac
}

// acEmojiIndexed waits until the popups emoji index for h lists name
// first for its own query, i.e. the index with the workspace's custom
// emoji has landed. It runs popups' queued callbacks each round, as the
// ui root's pump overlay does each frame.
func acEmojiIndexed(t *testing.T, h *slickHost, name string) {
	t.Helper()
	deadline := time.Now().Add(8 * time.Second)
	for time.Now().Before(deadline) {
		popups.Pump()
		if ms := popups.SearchEmoji(h, name, 1); len(ms) == 1 && ms[0].Name == name {
			return
		}
		time.Sleep(10 * time.Millisecond)
	}
	t.Fatalf("the emoji index never listed %s", name)
}

// Karim's ":tu" then Tab (2026-11-14), in the shape of their workspace:
// tu is a custom alias ("alias:thumbs-up") of a custom thumbs-up image,
// so ":tu" is an exact name and Slack lists it first (internal/ui/popups'
// TestAutocompleteEmojiCustomAlias pins Slack's rows for this set). The
// workspace's custom emoji (65,908 on Hack Club) are indexed off the UI
// goroutine: on first use, and again after the full index's 5-minute
// idle drop. ":tu" is the first query the ":" list answers, so it opens
// on the standard set alone (tulip first). Tab used to take that row;
// it waits for the index now and takes tu.
func TestAutocompleteSlackEmojiCustomAlias(t *testing.T) {
	app, _ := acSlackApp(t)
	const thumbsUp = "https://e.test/thumbs-up.png"
	app.S.CustomEmoji.Set(map[string]string{"thumbs-up": thumbsUp, "tu": "alias:thumbs-up",
		"turbo": "https://e.test/turbo.png", "tux": "https://e.test/tux.png", "yay": "alias:tada"})

	// typed and tabbed at once: the list opens before the custom emoji
	// are indexed
	ac := newAutocompleteFor(t, app)
	d := acDocOf(":tu")
	ac.Live = func() (*Doc, int) { return d, d.Len() }
	ac.Update(d, d.Len())
	items, _ := ac.Items()
	t.Logf(":tu as it opens: %v", acNames(items))
	if !ac.Key(key.NameTab, 0) {
		t.Fatal("Tab not taken")
	}
	it, _, _, ok := ac.Accepted()
	if !ok || it.Name != "tu" || it.Atom == nil || it.Atom.Text != ":tu:" || it.Atom.URL != thumbsUp {
		t.Errorf("Tab on :tu took %q (ok %v, atom %+v), want the alias tu of thumbs-up", it.Name, ok, it.Atom)
	}

	// once the remote model is in, the rows are Slack's: the alias, the
	// custom tux, tulip, the custom turbo, root_vegetable (turnip)
	got := acSettle(t, newAutocompleteFor(t, app), ":tu", func(n []string) bool { return len(n) > 4 && n[4] == "root_vegetable" })
	t.Logf(":tu: %v", got)
	if want := []string{"tu", "tux", "tulip", "turbo", "root_vegetable"}; !acEq(got[:5], want) {
		t.Errorf(":tu: %v, want %v first", got, want)
	}
}

// An open ":" list is ranked again when an emoji index lands. A custom
// emoji that arrives while Clack runs (emoji_changed: here a new alias,
// tum, of thumbs-up) is indexed off the UI goroutine. The rows shown in
// the meantime come from the index without it, and give way to the new
// alias on the next frame with nothing typed.
func TestAutocompleteSlackEmojiIndexLands(t *testing.T) {
	app, _ := acSlackApp(t)
	h := &slickHost{app: app}
	const thumbsUp = "https://e.test/thumbs-up.png"
	app.S.CustomEmoji.Set(map[string]string{"thumbs-up": thumbsUp, "tu": "alias:thumbs-up", "tuba": "https://e.test/tuba.png",
		"tunk": "https://e.test/tunk.png", "tuna": "https://e.test/tuna.png", "tuff": "https://e.test/tuff.png", "tube": "https://e.test/tube.png"})
	acEmojiIndexed(t, h, "tuba")
	app.S.CustomEmoji.Put("tum", "alias:thumbs-up")

	ac := newAutocomplete(h, tChGen)
	t.Cleanup(ac.Close)
	d := acDocOf(":tum")
	ac.Live = func() (*Doc, int) { return d, d.Len() }
	ac.Update(d, d.Len())
	items, _ := ac.Items()
	if len(items) == 0 || items[0].Name == "tum" {
		t.Fatalf(":tum opened on an index that has tum: %v", acNames(items))
	}
	t.Logf(":tum before the index lands: %v", acNames(items))
	acEmojiIndexed(t, h, "tum")
	ac.Update(d, d.Len())
	items, _ = ac.Items()
	if len(items) == 0 || items[0].Name != "tum" || items[0].Atom == nil || items[0].Atom.URL != thumbsUp {
		t.Errorf(":tum after the index landed: %v, want tum (thumbs-up) first", acNames(items))
	}
}

// An emoji token starts the index of the custom emoji before the list
// opens: ":" or ":t" (too short to list) starts it, so the list that
// opens at ":tu" is usually ranked with the custom alias from its first
// frame.
func TestAutocompleteSlackEmojiIndexWarms(t *testing.T) {
	app, _ := acSlackApp(t)
	h := &slickHost{app: app}
	const thumbsUp = "https://e.test/thumbs-up.png"
	app.S.CustomEmoji.Set(map[string]string{"thumbs-up": thumbsUp, "tu": "alias:thumbs-up",
		"tuba": "https://e.test/tuba.png", "tube": "https://e.test/tube.png"})
	ac := newAutocomplete(h, tChGen)
	t.Cleanup(ac.Close)
	gen := popups.EmojiIndexGen()
	d := acDocOf("hi :t")
	ac.Update(d, d.Len())
	if ac.Open() {
		t.Fatalf(":t opened a list: %v", acNames(ac.items))
	}
	deadline := time.Now().Add(8 * time.Second)
	for popups.EmojiIndexGen() == gen && time.Now().Before(deadline) {
		time.Sleep(10 * time.Millisecond)
	}
	if popups.EmojiIndexGen() == gen {
		t.Fatal(`":t" didn't start indexing the custom emoji`)
	}
	d = acDocOf("hi :tu")
	ac.Update(d, d.Len())
	items, _ := ac.Items()
	if len(items) == 0 || items[0].Name != "tu" || items[0].Atom == nil || items[0].Atom.URL != thumbsUp {
		t.Errorf(":tu after :t: %v, want tu (thumbs-up) first", acNames(items))
	}
}
