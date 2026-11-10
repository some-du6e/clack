package ui

import (
	"context"
	"encoding/json"
	"errors"
	"image"
	"reflect"
	"testing"
	"time"

	"gioui.org/op"
	"gioui.org/widget"

	"github.com/some-du6e/clack/internal/core"
	"github.com/some-du6e/clack/internal/fakeslack"
	"github.com/some-du6e/clack/internal/imgcache"
	"github.com/some-du6e/clack/internal/slack"
	"github.com/some-du6e/clack/internal/store"
	"github.com/some-du6e/clack/internal/ui/theme"
	"github.com/some-du6e/clack/internal/ui/uipref"
)

// Slack's Preferences sidebar, in order (2026 desktop client).
func TestPrefsNavOrder(t *testing.T) {
	want := []struct{ id, label string }{
		{"availability", "Availability"}, {"notifications", "Notifications"}, {"vip", "VIP"},
		{"navigation", "Navigation"}, {"sidebar", "Home"}, {"appearance", "Appearance"},
		{"messages_media", "Messages & media"}, {"language_region", "Language & region"},
		{"accessibility", "Accessibility"}, {"mark_as_read", "Mark as read"}, {"video_audio", "Audio & video"},
		{"salesforce", "Salesforce"}, {"connected_accounts", "Connected accounts"},
		{"privacy_visibility", "Privacy & visibility"}, {"advanced", "Advanced"},
	}
	if len(prefsNav) != len(want) {
		t.Fatalf("%d sections, want %d", len(prefsNav), len(want))
	}
	for i, w := range want {
		if prefsNav[i].id != w.id || prefsNav[i].label != w.label {
			t.Errorf("section %d = %s %q, want %s %q", i, prefsNav[i].id, prefsNav[i].label, w.id, w.label)
		}
	}
	if prefsNav[len(prefsNav)-1].id != "advanced" {
		t.Error("Advanced must stay last (Taut appends its tabs after it)")
	}
	// Slack's ids for the two sections Clack named first.
	for in, out := range map[string]string{"tab_rail": "navigation", "themes": "appearance", "vip": "vip", "advanced": "advanced"} {
		if got := prefsSection(in); got != out {
			t.Errorf("prefsSection(%q) = %q, want %q", in, got, out)
		}
	}
	// every page but Availability (popups') and Appearance has a page func
	for _, it := range prefsNav {
		if it.id != "availability" && it.id != "appearance" && pageFns[it.id] == nil {
			t.Errorf("section %s has no page", it.id)
		}
	}
}

// fakePrefsApp is a ui.App whose core talks to fakeslack.
func fakePrefsApp(t *testing.T) (*App, *core.App, *fakeslack.Server) {
	t.Helper()
	fs, err := fakeslack.Start(fakeslack.Small)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = fs.Close() })
	ca := core.New(slack.New(fs.Creds()), store.New(t.TempDir()))
	if _, err := ca.RefreshPrefs(context.Background()); err != nil {
		t.Fatal(err)
	}
	images := imgcache.New(func(ctx context.Context, url string) ([]byte, error) { return nil, errors.New("offline") }, "", nil)
	a := New(Options{ConfigDir: t.TempDir(), NoRealtime: true}, images)
	sh := theme.NewShaper()
	a.th.SetShapers(sh, sh)
	a.data = &backend{core: ca, app: a}
	return a, ca, fs
}

// waitFor polls until ok, running posted UI work.
func waitFor(t *testing.T, a *App, what string, ok func() bool) {
	t.Helper()
	deadline := time.Now().Add(10 * time.Second)
	for !ok() {
		if time.Now().After(deadline) {
			t.Fatalf("timed out waiting for %s", what)
		}
		time.Sleep(10 * time.Millisecond)
		a.drain()
	}
	a.drain()
}

// fakeNotify reads a global notification pref from the fake's
// all_notifications_prefs.
func fakeNotify(fs *fakeslack.Server, name string) any {
	v, _ := fs.Pref("all_notifications_prefs")
	s, _ := v.(string)
	var n struct {
		Global map[string]any `json:"global"`
	}
	_ = json.Unmarshal([]byte(s), &n)
	return n.Global[name]
}

// Every wired setting writes Slack's key with Slack's value through the
// function its row calls, and core reads it back.
func TestPrefsRoundTrip(t *testing.T) {
	a, ca, fs := fakePrefsApp(t)
	p := &prefsModal{a: a, clicks: map[string]*widget.Clickable{}}
	type rt struct {
		name  string
		set   func()
		fake  func() any // what Slack (the fake) stores
		want  any
		check func() bool // core's reading
	}
	user := func(name string, v any, check func() bool) rt {
		return rt{name: name, set: func() { p.setPref(name, v) }, fake: func() any { x, _ := fs.Pref(name); return x }, want: v, check: check}
	}
	notif := func(name string, v any, check func() bool) rt {
		return rt{name: "notify:" + name, set: func() { p.setNotify(name, v) }, fake: func() any { return fakeNotify(fs, name) }, want: v, check: check}
	}
	cases := []rt{
		user("time24", true, func() bool { return ca.PrefBool("time24", false) && ca.Prefs().Time24 }),
		user("display_real_names_override", 1, func() bool { return ca.PrefInt("display_real_names_override", 0) == 1 && ca.Prefs().DisplayRealNames }),
		user("show_typing", false, func() bool { return !ca.PrefBool("show_typing", true) && !ca.Prefs().ShowTyping }),
		user("hide_hex_swatch", true, func() bool { return ca.Prefs().HideHexSwatch }),
		user("jumbomoji", false, func() bool { return !ca.Prefs().Jumbomoji }),
		user("expand_internal_inline_imgs", false, func() bool { return ca.Prefs().NoInternalImgs }),
		user("expand_inline_imgs", false, func() bool { return ca.Prefs().NoInlineImgs }),
		user("expand_non_media_attachments", false, func() bool { return ca.Prefs().NoTextPreviews }),
		user("a11y_animations", false, func() bool { return ca.Prefs().NoAnimations }),
		user("underline_links", true, func() bool { return ca.Prefs().UnderlineLinks }),
		user("enter_is_special_in_tbt", true, func() bool { return ca.Prefs().EnterIsSpecial }),
		user("confirm_clear_all_unreads", false, func() bool { return !ca.PrefBool("confirm_clear_all_unreads", true) }),
		user("sidebar_behavior", "hide_read_channels", func() bool { return ca.Prefs().SidebarBehavior == "hide_read_channels" }),
		user("channel_sort", "recent", func() bool { return ca.Prefs().ChannelSort == "recent" }),
		user("separate_private_channels", true, func() bool { return ca.Prefs().SeparatePrivate }),
		user("undo_channel_intermix", true, func() bool { return ca.Prefs().UndoIntermix }),
		user("boost_mentions", true, func() bool { return ca.Prefs().BoostMentions }),
		user("show_muted_items_outside_sidebar_menus", true, func() bool { return ca.Prefs().ShowMutedOutside }),
		user("who_can_see_account_by_searching_email", "NO_ONE", func() bool { return ca.PrefString("who_can_see_account_by_searching_email") == "NO_ONE" }),
		user("who_can_share_contact_card", "TEAM_MEMBERS", func() bool { return ca.PrefString("who_can_share_contact_card") == "TEAM_MEMBERS" }),
		user("priority_dnd_override", true, func() bool { return ca.PrefBool("priority_dnd_override", false) }),
		user("huddles_mute_by_default", true, func() bool { return ca.PrefBool("huddles_mute_by_default", false) }),
		user("huddles_set_status", false, func() bool { return !ca.PrefBool("huddles_set_status", true) }),
		// Accessibility > Lists keyboard interactions (ListsTabbingPref)
		user("lists_tabbing_navigation_option_type", "arrow_nav", func() bool { return ca.PrefString("lists_tabbing_navigation_option_type") == "arrow_nav" }),
		notif("no_text_in_notifications", true, func() bool {
			return ca.GlobalNotifyBool("no_text_in_notifications", false) && ca.Prefs().NoTextInNotifications
		}),
		notif("global_desktop", "everything", func() bool { return ca.Prefs().GlobalDesktop == "everything" }),
		notif("global_mobile", "nothing", func() bool { return ca.GlobalNotifyString("global_mobile") == "nothing" }),
		notif("threads_everything", false, func() bool { return !ca.GlobalNotifyBool("threads_everything", true) }),
	}
	for _, c := range cases {
		c.set()
		waitFor(t, a, c.name, func() bool { return reflect.DeepEqual(normNum(c.fake()), normNum(c.want)) && c.check() })
		if _, held := p.pend[c.name]; held {
			t.Errorf("%s: pending value not cleared after the write", c.name)
		}
	}

	// Mark as read writes both read-state prefs in one users.prefs.set.
	p.setPrefs(map[string]any{"mark_msgs_read_immediately": true, "start_scroll_at_oldest": true})
	waitFor(t, a, "read state", func() bool {
		m, _ := fs.Pref("mark_msgs_read_immediately")
		o, _ := fs.Pref("start_scroll_at_oldest")
		pr := ca.Prefs()
		return m == true && o == true && pr.MarkReadImmediately && pr.StartAtOldest
	})

	// VIP: the list is vip_users; Remove is users.priority.remove.
	fs.SetPrefs(map[string]any{"vip_users": "U0VIP1,U0VIP2"})
	if _, err := ca.RefreshPrefs(context.Background()); err != nil {
		t.Fatal(err)
	}
	if got := ca.VIPs(); !reflect.DeepEqual(got, []string{"U0VIP1", "U0VIP2"}) {
		t.Fatalf("VIPs %v", got)
	}
	a.removeVIP("U0VIP1", nil)
	waitFor(t, a, "VIP removed", func() bool {
		v, _ := fs.Pref("vip_users")
		return v == "U0VIP2" && reflect.DeepEqual(ca.VIPs(), []string{"U0VIP2"})
	})
}

// normNum makes 1 and 1.0 compare equal (the fake keeps ints, JSON floats).
func normNum(v any) any {
	switch x := v.(type) {
	case int:
		return float64(x)
	case int64:
		return float64(x)
	}
	return v
}

// The prefs snapshot render paths read follows core's "prefs" events.
func TestPrefsEventUpdatesSnapshot(t *testing.T) {
	a, ca, _ := fakePrefsApp(t)
	t.Cleanup(uipref.Reset)
	uipref.Set(ca.Prefs())
	if uipref.Get().Time24 {
		t.Fatal("time24 on by default")
	}
	p := ca.Prefs()
	p.Time24 = true
	a.data.handle(a, core.Event{Type: "prefs", Prefs: &p})
	if !uipref.Get().Time24 {
		t.Fatal("a prefs event didn't reach uipref")
	}
}

// Notifications > Desktop notifications drives Clack's own notifications.
func TestWantsNotify(t *testing.T) {
	s := store.New("")
	b := &backend{core: core.New(slack.New(slack.Creds{Token: "xoxc-test"}), s)}
	set := func(global, override string) {
		p := store.Prefs{GlobalDesktop: global}
		if override != "" {
			p.ChannelNotify = map[string]store.ChannelNotify{"C1": {Desktop: override}}
		}
		s.SetPrefs(p, nil)
	}
	for _, c := range []struct {
		global, override string
		mention, want    bool
	}{
		{"mentions_dms", "", true, true},
		{"mentions_dms", "", false, false},
		{"", "", true, true},
		{"everything", "", false, true},
		{"nothing", "", true, false},
		{"mentions_dms", "everything", false, true},
		{"everything", "nothing", true, false},
		{"nothing", "default", true, false},
	} {
		set(c.global, c.override)
		if got := b.wantsNotify("C1", c.mention); got != c.want {
			t.Errorf("global %q override %q mention %v: %v, want %v", c.global, c.override, c.mention, got, c.want)
		}
	}
}

// Home's sidebar prefs (sidebar_behavior, channel_sort, boost_mentions…)
// are applied by core's sidebar rules (internal/core/sidebar_rules.go and
// its tests); a "prefs" event rebuilds the sidebar from a fresh snapshot.
func TestPrefsEventResnapshots(t *testing.T) {
	a, ca, _ := fakePrefsApp(t)
	b := a.data
	b.handle(a, core.Event{Type: "prefs", Prefs: func() *core.Prefs { p := ca.Prefs(); return &p }()})
	b.snapMu.Lock()
	pending := b.snapPending
	b.snapMu.Unlock()
	if !pending {
		t.Fatal("a prefs event didn't rebuild the sidebar")
	}
}

// Every section lays out (Slack's sections with every pref absent and
// with the fake's values), and long pages scroll.
func TestPrefsSectionsDraw(t *testing.T) {
	a, ca, fs := fakePrefsApp(t)
	fs.SetPrefs(map[string]any{"vip_users": "U0VIP1"})
	_, _ = ca.RefreshPrefs(context.Background())
	for _, it := range prefsNav {
		if it.id == "availability" { // popups' page, tested there
			continue
		}
		p := &prefsModal{a: a, clicks: map[string]*widget.Clickable{}, section: it.id}
		gtx := ttContext(new(op.Ops), image.Pt(800, 624))
		a.slackcFrame(gtx)
		p.layout(gtx)
		p.close()
		if it.id != "appearance" && p.scroll.contentH <= 0 {
			t.Errorf("%s: empty page", it.id)
		}
	}
}
