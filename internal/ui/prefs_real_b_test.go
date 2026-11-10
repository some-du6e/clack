package ui

import (
	"encoding/json"
	"net/url"
	"reflect"
	"strings"
	"sync"
	"testing"
	"time"

	"gioui.org/widget"

	"github.com/some-du6e/clack/internal/core"
	"github.com/some-du6e/clack/internal/fakeslack"
	"github.com/some-du6e/clack/internal/ui/composer"
	"github.com/some-du6e/clack/internal/ui/popups"
)

// callLog records the API calls a fakeslack hook answered.
type callLog struct {
	mu    sync.Mutex
	calls []string
	forms []url.Values
}

func (l *callLog) add(m string, f url.Values) {
	l.mu.Lock()
	l.calls = append(l.calls, m)
	l.forms = append(l.forms, f)
	l.mu.Unlock()
}

func (l *callLog) form(m string) (url.Values, bool) {
	l.mu.Lock()
	defer l.mu.Unlock()
	for i := len(l.calls) - 1; i >= 0; i-- {
		if l.calls[i] == m {
			return l.forms[i], true
		}
	}
	return nil, false
}

func topModal(a *App) (*popups.Modal, bool) {
	if len(a.overlays) == 0 {
		return nil, false
	}
	m, ok := a.overlays[len(a.overlays)-1].(*popups.Modal)
	return m, ok
}

func fakePrefStr(fs *fakeslack.Server, name string) string {
	v, _ := fs.Pref(name)
	s, _ := v.(string)
	return s
}

// Part B of the greyed-row work (Navigation, Accessibility, Audio & video,
// Connected accounts, Privacy & visibility, Advanced): each row writes
// Slack's key and value, core reads it back, and Clack acts on it.
func TestPrefsAuditAppliedRoundTripB(t *testing.T) {
	a, ca, fs := fakePrefsApp(t)
	ca.S.Self.ID = fakeslack.SelfID // what boot sets
	p := &prefsModal{a: a, clicks: map[string]*widget.Clickable{}}
	var log callLog
	var hookMu sync.Mutex
	hooks := map[string]any{}
	fs.SetHook(func(m string, f url.Values) (any, bool) {
		log.add(m, f)
		hookMu.Lock()
		out, ok := hooks[m]
		hookMu.Unlock()
		return out, ok
	})
	answer := func(m string, out any) { hookMu.Lock(); hooks[m] = out; hookMu.Unlock() }

	// Navigation: a drag onto another row (onOptionReordered) writes the
	// listbox order as tab_rail_customization_order
	opts := p.navTabs()
	ids := reorderIDs(opts)
	if len(ids) < 3 {
		t.Fatalf("navigation lists %v", ids)
	}
	moved := reorderMove(ids, ids[len(ids)-1], ids[0])
	if moved[0] != ids[len(ids)-1] || len(moved) != len(ids) {
		t.Fatalf("reorderMove %v -> %v", ids, moved)
	}
	p.setRailOrder(moved)
	waitFor(t, a, "dragged order", func() bool {
		var got []string
		_ = json.Unmarshal([]byte(fakePrefStr(fs, "tab_rail_customization_order")), &got)
		return reflect.DeepEqual(got, moved) && reflect.DeepEqual(ca.Rail().Available, moved)
	})
	// Move down acts on the active (hovered or arrowed-to) option
	p.nav.active = moved[0]
	p.moveActive(p.navTabs(), 1)
	waitFor(t, a, "moved down", func() bool { return ca.Rail().Available[1] == moved[0] })

	// Navigation > Show Agents in direct messages: hide_agents_in_dm_list
	// is the box's negation (core.AgentInDMSidebar files agent DMs by it)
	p.setPref("hide_agents_in_dm_list", true)
	waitFor(t, a, "agents hidden", func() bool { v, _ := fs.Pref("hide_agents_in_dm_list"); return v == true && ca.HideAgentsInDMs() })
	p.setPref("hide_agents_in_dm_list", false)
	waitFor(t, a, "agents shown", func() bool { return !ca.HideAgentsInDMs() })

	// Advanced > Enable smart quotes and symbols: disable_smart_symbols
	p.setPref("disable_smart_symbols", true)
	waitFor(t, a, "smart symbols off", func() bool { v, _ := fs.Pref("disable_smart_symbols"); return v == true && !composer.SmartSymbols(ca) })
	p.setPref("disable_smart_symbols", false)
	waitFor(t, a, "smart symbols on", func() bool { return composer.SmartSymbols(ca) })

	// Advanced > Exclude these channels from search results
	m := a.model()
	if m.Convs == nil {
		m.Convs = map[string]*convInfo{}
	}
	chans := []pickerOption{{"C0EXCL0001", "random"}, {"C0EXCL0002", "offtopic"}}
	for _, c := range chans {
		m.Convs[c.id] = &convInfo{ID: c.id, Name: c.name, Kind: "channel"}
	}
	if ms := p.pickerMatches("off", nil); len(ms) != 1 || ms[0].id != "C0EXCL0002" {
		t.Fatalf("picker matches %v", ms)
	}
	p.setExcluded(chans)
	want := chans[0].id + "," + chans[1].id
	waitFor(t, a, "search_exclude_channels", func() bool {
		return fakePrefStr(fs, "search_exclude_channels") == want && ca.PrefString("search_exclude_channels") == want
	})
	if got := p.excludeOptions(); len(got) != 2 {
		t.Fatalf("picker tokens %v", got)
	}
	answer("search.modules.messages", map[string]any{"ok": true, "items": []any{}, "pagination": map[string]any{"total_count": 0, "page": 1, "page_count": 1},
		"filter_suggestions": map[string]any{"in": []any{map[string]any{"id": chans[0].id, "facet_count": 3}, map[string]any{"id": "C0KEEP", "facet_count": 1}}, "from": []any{}}})
	res, err := ca.SearchMessages(t.Context(), core.SearchOpts{Query: "x", Page: 1})
	if err != nil {
		t.Fatal(err)
	}
	if len(res.FilterIn) != 1 || res.FilterIn[0].ID != "C0KEEP" {
		t.Fatalf("In suggestions keep an excluded channel: %+v", res.FilterIn)
	}

	// Advanced > Sort option default: Last used (stick_user_sort)
	p.setPrefs(map[string]any{"stick_user_sort": true, "search_sort": "recentMessages"})
	waitFor(t, a, "sticky sort", func() bool {
		v, _ := fs.Pref("stick_user_sort")
		return v == true && popups.DefaultSearchSort(ca) == "timestamp"
	})
	p.setPref("stick_user_sort", false)
	waitFor(t, a, "most relevant", func() bool { return popups.DefaultSearchSort(ca) == "score" })

	// Advanced > Warn me about potentially malicious links (suppress_link_warning)
	n := len(a.overlays)
	if !popups.MaybeWarnLink(a, "paypal.com", "https://evil.test/login") {
		t.Fatal("no warning for a link naming another site")
	}
	if md, ok := topModal(a); !ok || len(a.overlays) != n+1 || md == nil {
		t.Fatal("no Double-check this link dialog")
	}
	p.setPref("suppress_link_warning", true)
	waitFor(t, a, "warning off", func() bool {
		v, _ := fs.Pref("suppress_link_warning")
		return v == true && ca.PrefBool("suppress_link_warning", false)
	})
	if popups.MaybeWarnLink(a, "paypal.com", "https://evil.test/login") {
		t.Fatal("warned with suppress_link_warning")
	}

	// Advanced > Show me a confirmation when I use Mod+Z to unsend
	ch := chans[0].id
	answer("conversations.history", map[string]any{"ok": true, "messages": []any{
		map[string]any{"type": "message", "ts": "1700000002.000100", "user": fakeslack.SelfID, "text": "oops typo"},
		map[string]any{"type": "message", "ts": "1700000001.000100", "user": "U0OTHER", "text": "hi"},
	}, "has_more": false})
	answer("chat.delete", map[string]any{"ok": true})
	p.setPref("should_show_unsend_message_confirmation", true)
	waitFor(t, a, "unsend confirmation on", func() bool { return ca.PrefBool("should_show_unsend_message_confirmation", false) })
	var put *composer.Doc
	n = len(a.overlays)
	a.UnsendLast(ch, "", func(d *composer.Doc) { put = d })
	waitFor(t, a, "Unsend message dialog", func() bool { return len(a.overlays) == n+1 })
	if _, ok := log.form("chat.delete"); ok || put != nil {
		t.Fatal("unsent before the dialog's Unsend")
	}
	p.setPref("should_show_unsend_message_confirmation", false)
	waitFor(t, a, "unsend confirmation off", func() bool {
		v, _ := fs.Pref("should_show_unsend_message_confirmation")
		return v == false && !ca.PrefBool("should_show_unsend_message_confirmation", true)
	})
	a.UnsendLast(ch, "", func(d *composer.Doc) { put = d })
	waitFor(t, a, "unsent", func() bool {
		f, ok := log.form("chat.delete")
		return ok && f.Get("ts") == "1700000002.000100" && f.Get("channel") == ch && put != nil && put.Text() == "oops typo"
	})

	// Advanced > Ask if I want to toggle my away status (confirm_user_marked_away)
	answer("users.getPresence", map[string]any{"ok": true, "presence": "away", "manual_away": true})
	p.setPref("confirm_user_marked_away", true)
	waitFor(t, a, "away prompt on", func() bool {
		v, _ := fs.Pref("confirm_user_marked_away")
		return v == true && ca.PrefBool("confirm_user_marked_away", false)
	})
	n = len(a.overlays)
	a.data.maybePresencePrompt(a)
	waitFor(t, a, "Set yourself to active?", func() bool { return len(a.overlays) == n+1 })
	p.setPref("confirm_user_marked_away", false)
	waitFor(t, a, "away prompt off", func() bool { return !ca.PrefBool("confirm_user_marked_away", true) })
	n = len(a.overlays)
	a.data.maybePresencePrompt(a)
	time.Sleep(100 * time.Millisecond)
	a.drain()
	if len(a.overlays) != n {
		t.Fatal("presence prompt with confirm_user_marked_away off")
	}

	// Accessibility > Press ↑ in the empty message field to: (this device, ui.json)
	p.clicks["up.2"] = &widget.Clickable{}
	a.setComposerUpArrow(2)
	if composer.UpArrowShortcut() != 2 || a.prefs.ComposerUpArrow != 2 {
		t.Fatal("up-arrow choice not applied")
	}
	a.flushPrefs()
	a2, _, _ := fakePrefsApp(t)
	a2.opts.ConfigDir = a.opts.ConfigDir
	a2.loadPrefs()
	if a2.prefs.ComposerUpArrow != 2 {
		t.Fatal("up-arrow choice not kept in ui.json")
	}
	composer.UpArrowShortcut = func() int { return a.prefs.ComposerUpArrow }

	// Connected accounts: apps.auth.external.userAuths.get
	answer("apps.auth.external.userAuths.get", map[string]any{"ok": true, "apps": map[string]any{
		"A0GDRIVE": map[string]any{"app_name": "Google Drive", "app_icon_url": ""}}})
	rows := pageRows(t, a, p, "connected_accounts")
	waitFor(t, a, "connected apps", func() bool { return p.lists.connDone })
	if f, ok := log.form("apps.auth.external.userAuths.get"); !ok || f.Get("reason") != "fetch_user_auth" {
		t.Fatal("no fetchUserAuth call")
	}
	rows = pageRows(t, a, p, "connected_accounts")
	if !hasRow(rows, "list", "Google Drive") {
		t.Fatalf("connected accounts rows %s", dumpRows(rows))
	}

	// Privacy & visibility: People you've hidden (hidden_users), Find my workspaces
	fs.SetPrefs(map[string]any{"hidden_users": fakeslack.ProfileID})
	if _, err := ca.RefreshPrefs(t.Context()); err != nil {
		t.Fatal(err)
	}
	answer("users.emailVerificationStatus.get", map[string]any{"ok": true, "is_verified": true})
	answer("users.listIgnoredUsers", map[string]any{"ok": true, "emails": []string{"spam@example.com"}})
	answer("users.removeUserBlock", map[string]any{"ok": true})
	pageRows(t, a, p, "privacy_visibility")
	waitFor(t, a, "blocked list", func() bool { return p.lists.blockDone })
	rows = pageRows(t, a, p, "privacy_visibility")
	if !hasRow(rows, "list", "spam@example.com · Unblock email") || !hasRowPrefix(rows, "list", "", "· Unhide user") {
		t.Fatalf("privacy rows %s", dumpRows(rows))
	}
	p.btn("row.hidden." + fakeslack.ProfileID).Click()
	pageRows(t, a, p, "privacy_visibility")
	waitFor(t, a, "unhidden", func() bool { return fakePrefStr(fs, "hidden_users") == "" && ca.PrefString("hidden_users") == "" })
	var opened []string
	prefsOpenURL = func(u string) { opened = append(opened, u) }
	t.Cleanup(func() { prefsOpenURL = openURL })
	p.btn("button.find_workspaces").Click()
	pageRows(t, a, p, "privacy_visibility")
	if len(opened) != 1 || opened[0] != findWorkspacesURL {
		t.Fatalf("Find my workspaces opened %v", opened)
	}
	p.confirmUnblock("spam@example.com")
	md, _ := topModal(a)
	md.PressDefault()
	waitFor(t, a, "unblocked", func() bool {
		f, ok := log.form("users.removeUserBlock")
		return ok && f.Get("email") == "spam@example.com" && len(p.lists.blocked) == 0
	})
}

func hasRow(rows []prefsDrawn, kind, label string) bool {
	for _, r := range rows {
		if r.Kind == kind && r.Label == label {
			return true
		}
	}
	return false
}

func hasRowPrefix(rows []prefsDrawn, kind, prefix, suffix string) bool {
	for _, r := range rows {
		if r.Kind == kind && strings.HasPrefix(r.Label, prefix) && strings.HasSuffix(r.Label, suffix) {
			return true
		}
	}
	return false
}
