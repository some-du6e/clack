package core

import (
	"encoding/json"
	"net/url"
	"sync"
	"testing"
	"time"

	"github.com/some-du6e/clack/internal/slack"
)

// The composer's upload gates (upload_gates.go), checked against Slack's
// selectors: rscm O over conversationsListPrefs and uu7O f. Synthetic
// conversations only.

// listPrefsCalls counts conversations.listPrefs by channel and checks
// each goes out as fetchAndUpsertConversationsListPrefs sends it.
func listPrefsCalls(t *testing.T, f *fakeSlack) func(ch string) int {
	t.Helper()
	var mu sync.Mutex
	n := map[string]int{}
	f.mu.Lock()
	f.hooks["conversations.listPrefs"] = func(v url.Values) {
		if v.Get("reason") != "client_redux_store" {
			t.Errorf("conversations.listPrefs %v: want reason client_redux_store", v)
		}
		mu.Lock()
		n[v.Get("channel")]++
		mu.Unlock()
	}
	f.mu.Unlock()
	return func(ch string) int {
		mu.Lock()
		defer mu.Unlock()
		return n[ch]
	}
}

// setListPrefs makes conversations.listPrefs answer prefs (JSON).
func setListPrefs(f *fakeSlack, body string) {
	f.mu.Lock()
	f.reply["conversations.listPrefs"] = body
	f.mu.Unlock()
}

// listPrefsSettled waits until ch's list prefs are fresh and no fetch runs.
func listPrefsSettled(t *testing.T, a *App, ch string) {
	t.Helper()
	st := a.listPrefs()
	dl := time.Now().Add(5 * time.Second)
	for {
		st.mu.Lock()
		done := st.fresh[ch] && !st.busy && len(st.pending) == 0
		st.mu.Unlock()
		if done {
			return
		}
		if time.Now().After(dl) {
			t.Fatalf("%s's list prefs never settled", ch)
		}
		time.Sleep(5 * time.Millisecond)
	}
}

func listPrefsFresh(a *App, ch string) bool {
	st := a.listPrefs()
	st.mu.Lock()
	defer st.mu.Unlock()
	return st.fresh[ch]
}

// rscm O: an externally shared conversation whose list prefs turn
// can_upload_files off. The read fetches the prefs once (maybeRefetch)
// and reads them after the answer; other conversations never ask.
// The composer reads the + button's gate on every frame (chromeState),
// in every Slack Connect conversation's composer, thread pane and Threads
// row: once the conversation's list prefs are fresh, a read allocates
// nothing, and nor does a conversation that isn't shared.
func TestSlackConnectGateAllocs(t *testing.T) {
	a, f := testApp(t)
	const shared, internal = "C0SHARED05", "C0INTERNA2"
	a.S.PutConv(slack.Conversation{ID: shared, Name: "partners-allocs", IsChannel: true, IsMember: true, IsExtShared: true})
	a.S.PutConv(slack.Conversation{ID: internal, Name: "internal-allocs", IsChannel: true, IsMember: true})
	setListPrefs(f, `{"ok":true,"prefs":{"can_upload_files":false}}`)
	a.SlackConnectFileSharingBlocked(shared)
	listPrefsSettled(t, a, shared)
	if !a.SlackConnectFileSharingBlocked(shared) {
		t.Fatal("can_upload_files false: not blocked")
	}
	if n := testing.AllocsPerRun(200, func() { a.SlackConnectFileSharingBlocked(shared) }); n != 0 {
		t.Errorf("a read of a Slack Connect conversation's gate allocated %v times, want none", n)
	}
	if n := testing.AllocsPerRun(200, func() { a.SlackConnectFileSharingBlocked(internal) }); n != 0 {
		t.Errorf("a read of a conversation that isn't shared allocated %v times, want none", n)
	}
}

// can_upload_files null reads as allowed, as Slack's `?? true` reads it
// (rscm O: !(Q(e, "can_upload_files", t) ?? !0)): a null pref is no pref,
// and only false (or 0, "") blocks.
func TestSlackConnectNullPrefAllowsUploads(t *testing.T) {
	a, f := testApp(t)
	const null, zero = "C0SHARED03", "C0SHARED04"
	for _, id := range []string{null, zero} {
		a.S.PutConv(slack.Conversation{ID: id, Name: "partners-" + id, IsChannel: true, IsMember: true, IsExtShared: true})
	}
	setListPrefs(f, `{"ok":true,"prefs":{"can_upload_files":null}}`)
	a.SlackConnectFileSharingBlocked(null)
	listPrefsSettled(t, a, null)
	if a.SlackConnectFileSharingBlocked(null) {
		t.Error("can_upload_files null: blocked, want allowed (?? true)")
	}
	setListPrefs(f, `{"ok":true,"prefs":{"can_upload_files":0}}`)
	a.SlackConnectFileSharingBlocked(zero)
	listPrefsSettled(t, a, zero)
	if !a.SlackConnectFileSharingBlocked(zero) {
		t.Error("can_upload_files 0: allowed, want blocked (falsy)")
	}
}

func TestSlackConnectFileSharingBlockedLikeSlack(t *testing.T) {
	a, f := testApp(t)
	const shared, open, short, internal = "C0SHARED01", "C0SHARED02", "CSHORT", "C0INTERNAL"
	for _, c := range []slack.Conversation{
		{ID: shared, Name: "partners", IsChannel: true, IsMember: true, IsExtShared: true},
		{ID: open, Name: "partners-open", IsChannel: true, IsMember: true, IsExtShared: true},
		{ID: short, Name: "odd-id", IsChannel: true, IsMember: true, IsExtShared: true},
		{ID: internal, Name: "internal", IsChannel: true, IsMember: true},
	} {
		a.S.PutConv(c)
	}
	calls := listPrefsCalls(t, f)
	evs, stop := a.Subscribe()
	defer stop()

	// not shared (5DYq ju), or an id jh4t doesn't take: allowed, no read
	if a.SlackConnectFileSharingBlocked(internal) || a.SlackConnectFileSharingBlocked(short) || a.SlackConnectFileSharingBlocked("C0NOSUCH01") {
		t.Fatal("blocked without a Slack Connect conversation's prefs")
	}
	if calls(internal)+calls(short)+calls("C0NOSUCH01") != 0 {
		t.Fatal("conversations.listPrefs for a conversation the selector doesn't read")
	}

	// can_upload_files false: allowed until the answer, then blocked
	setListPrefs(f, `{"ok":true,"prefs":{"can_upload_files":false,"who_can_post":"type:admin"}}`)
	if a.SlackConnectFileSharingBlocked(shared) {
		t.Fatal("blocked before conversations.listPrefs answered (the store has no entry: ?? true)")
	}
	next(t, evs, "list_prefs")
	listPrefsSettled(t, a, shared)
	for range 3 {
		if !a.SlackConnectFileSharingBlocked(shared) {
			t.Fatal("can_upload_files false: not blocked")
		}
	}
	if n := calls(shared); n != 1 {
		t.Fatalf("conversations.listPrefs %d times for %s, want once (fresh after the answer, no maxAge)", n, shared)
	}

	// can_upload_files true, or missing: allowed
	setListPrefs(f, `{"ok":true,"prefs":{"can_upload_files":true}}`)
	a.SlackConnectFileSharingBlocked(open)
	listPrefsSettled(t, a, open)
	if a.SlackConnectFileSharingBlocked(open) {
		t.Error("can_upload_files true: blocked")
	}

	// channel_updated with connected_team_ids: stale, the next read asks
	// again and the old prefs count until the answer
	setListPrefs(f, `{"ok":true,"prefs":{}}`)
	a.handle(rtm("channel_updated", map[string]any{"channel": shared, "updates": map[string]any{"topic": map[string]any{"value": "x"}}}))
	if !listPrefsFresh(a, shared) {
		t.Fatal("a channel_updated without unlinked or connected_team_ids marked the prefs stale")
	}
	a.handle(rtm("channel_updated", map[string]any{"channel": shared, "updates": map[string]any{"connected_team_ids": []string{"T1", "T2"}}}))
	if listPrefsFresh(a, shared) {
		t.Fatal("channel_updated with connected_team_ids left the prefs fresh")
	}
	if !a.SlackConnectFileSharingBlocked(shared) {
		t.Error("the stale prefs stopped counting before the new answer")
	}
	listPrefsSettled(t, a, shared)
	if a.SlackConnectFileSharingBlocked(shared) || calls(shared) != 2 {
		t.Errorf("after the refetch (prefs without can_upload_files): blocked %v, %d calls; want allowed, 2",
			a.SlackConnectFileSharingBlocked(shared), calls(shared))
	}
	a.handle(rtm("channel_updated", map[string]any{"channel": shared, "updates": map[string]any{"unlinked": 1}}))
	if listPrefsFresh(a, shared) {
		t.Error("channel_updated with unlinked left the prefs fresh")
	}
	a.SlackConnectFileSharingBlocked(shared)
	listPrefsSettled(t, a, shared)

	// slack_connect_pref_changed: allow_sc_file_uploads and
	// external_awareness_context_bar mark every shared conversation stale
	a.handle(rtm("slack_connect_pref_changed", map[string]any{"pref_name": "shared_channel_invite_requested"}))
	if !listPrefsFresh(a, shared) || !listPrefsFresh(a, open) {
		t.Error("shared_channel_invite_requested marked prefs stale")
	}
	for _, p := range []string{"allow_sc_file_uploads", "external_awareness_context_bar"} {
		a.handle(rtm("slack_connect_pref_changed", map[string]any{"pref_name": p}))
		if listPrefsFresh(a, shared) || listPrefsFresh(a, open) {
			t.Errorf("slack_connect_pref_changed %s left a shared conversation's prefs fresh", p)
		}
		a.SlackConnectFileSharingBlocked(shared)
		a.SlackConnectFileSharingBlocked(open)
		listPrefsSettled(t, a, shared)
		listPrefsSettled(t, a, open)
	}

	// channel_deleted, and a slow reconnect (_maybeResetState: the
	// refetcher starts at count 1, the boot's connect)
	a.handle(rtm("channel_deleted", map[string]any{"channel": open}))
	if listPrefsFresh(a, open) {
		t.Error("channel_deleted left the prefs fresh")
	}
	before := calls(shared)
	a.socketConnected(false) // the first connect
	a.socketConnected(true)  // a fast reconnect
	a.SlackConnectFileSharingBlocked(shared)
	if listPrefsSettled(t, a, shared); calls(shared) != before {
		t.Error("the first connect or a fast reconnect fetched the prefs again")
	}
	a.socketConnected(false)
	a.SlackConnectFileSharingBlocked(shared)
	if listPrefsSettled(t, a, shared); calls(shared) != before+1 {
		t.Errorf("a slow reconnect: %d calls, want %d", calls(shared), before+1)
	}

	// a failed read stores nothing and still counts as fresh (the fetch's
	// catch, then _updateAfterRefetch)
	const failing = "C0SHARED03"
	a.S.PutConv(slack.Conversation{ID: failing, Name: "partners-3", IsChannel: true, IsMember: true, IsExtShared: true})
	setListPrefs(f, `{"ok":false,"error":"channel_not_found"}`)
	a.SlackConnectFileSharingBlocked(failing)
	listPrefsSettled(t, a, failing)
	if a.SlackConnectFileSharingBlocked(failing) || calls(failing) != 1 {
		t.Errorf("after a failed read: blocked %v, %d calls; want allowed, 1", a.SlackConnectFileSharingBlocked(failing), calls(failing))
	}
}

// JavaScript truthiness for the pref's value (`!(value ?? true)`).
func TestListPrefTruthiness(t *testing.T) {
	for v, want := range map[string]bool{`false`: false, `true`: true, `0`: false, `1`: true, `""`: false, `"no"`: true,
		`null`: false, `{}`: true, `[]`: true} {
		if got := jsonTruthy(json.RawMessage(v)); got != want {
			t.Errorf("jsonTruthy(%s) = %v, want %v", v, got, want)
		}
	}
}

// uu7O f: external_huddles on, and the viewer is an external guest or the
// conversation a huddle channel.
func TestRestrictedExternalHuddleLikeSlack(t *testing.T) {
	a, _ := testApp(t)
	const huddleCh = "C0HUDDLE01"
	a.S.PutConv(slack.Conversation{ID: huddleCh, Name: "huddle", IsChannel: true, IsMember: true,
		Properties: json.RawMessage(`{"huddles":{"is_huddle_channel":true}}`)})
	a.SetExperimentGroup(ExpExternalHuddles, "")
	if a.RestrictedExternalHuddle(huddleCh) {
		t.Error("restricted without external_huddles")
	}
	a.SetExperimentGroup(ExpExternalHuddles, "on")
	if !a.RestrictedExternalHuddle(huddleCh) {
		t.Error("a huddle channel under external_huddles: not restricted")
	}
	if a.RestrictedExternalHuddle("C1") || a.RestrictedExternalHuddle("") {
		t.Error("restricted outside a huddle channel for a regular member")
	}
	for _, c := range []struct {
		typ  string
		want bool
	}{{"external_guest", true}, {"", false}, {"frontline_worker", false}} {
		a.S.PutUsers([]slack.User{{ID: "USELF", Name: "me", IsRestricted: true, RestrictedUserType: c.typ}})
		if got := a.RestrictedExternalHuddle("C1"); got != c.want {
			t.Errorf("a restricted viewer (%q) in C1: %v, want %v", c.typ, got, c.want)
		}
		if got := a.RestrictedExternalHuddle(""); got != c.want {
			t.Errorf("a restricted viewer (%q), no conversation: %v, want %v", c.typ, got, c.want)
		}
	}
	a.SetExperimentGroup(ExpExternalHuddles, "off")
	if a.RestrictedExternalHuddle("C1") || a.RestrictedExternalHuddle(huddleCh) {
		t.Error("restricted with external_huddles off")
	}
}
