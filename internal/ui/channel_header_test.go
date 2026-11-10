package ui

import (
	"encoding/json"
	"net/url"
	"strings"
	"sync"
	"testing"
	"time"

	"gioui.org/f32"

	"github.com/some-du6e/clack/internal/fakeslack"
	"github.com/some-du6e/clack/internal/ui/popups"
)

// fakeChannelNotify is the fake account's all_notifications_prefs entry
// for ch.
func fakeChannelNotify(t *testing.T, fs *fakeslack.Server, ch string) map[string]any {
	t.Helper()
	v, _ := fs.Pref("all_notifications_prefs")
	s, _ := v.(string)
	var n struct {
		Channels map[string]map[string]any `json:"channels"`
	}
	_ = json.Unmarshal([]byte(s), &n)
	return n.Channels[ch]
}

// The bell's More options › Advanced settings opens Slack's channel
// notifications modal; every switch is one users.prefs.setNotifications
// call {name, value, channel_id, global: false, sync: false}.
func TestChannelNotifyAdvancedSettings(t *testing.T) {
	e, fs := previewEnv(t)
	ch := fakeslackMemberChannel(e)
	var mu sync.Mutex
	var calls []url.Values
	fail := false
	fs.SetHook(func(method string, form url.Values) (any, bool) {
		if method != "users.prefs.setNotifications" || form.Get("global") == "true" {
			return nil, false
		}
		mu.Lock()
		defer mu.Unlock()
		calls = append(calls, form)
		if fail {
			return map[string]any{"ok": false, "error": "fatal_error"}, true
		}
		return nil, false
	})
	last := func() url.Values {
		mu.Lock()
		defer mu.Unlock()
		if len(calls) == 0 {
			return nil
		}
		return calls[len(calls)-1]
	}
	e.openChannel(ch)
	e.settle(200 * time.Millisecond)
	pick := func(label string) popups.MenuItem {
		for _, it := range popups.ChannelNotifyItems(e.a, ch) {
			for _, s := range append([]popups.MenuItem{it}, it.Sub...) {
				if s.Label == label {
					return s
				}
			}
		}
		t.Fatalf("no %q row", label)
		return popups.MenuItem{}
	}
	var subs []string
	for _, it := range popups.ChannelNotifyItems(e.a, ch) {
		if it.Label == "More options" {
			for _, s := range it.Sub {
				subs = append(subs, s.Label+s.Header)
			}
		}
	}
	if got, want := strings.Join(subs, " | "), "Temporarily mute… | Until tomorrow | 1 hour |  | Edit default preferences | Advanced settings"; got != want {
		t.Errorf("More options:\n got %s\nwant %s", got, want)
	}
	pick("Advanced settings").Do()
	e.settle(150 * time.Millisecond)
	m, ok := topOverlay[*popups.ChannelNotifyModal](e.a)
	if !ok {
		t.Fatal("Advanced settings opened no modal")
	}
	if got := m.Subtitle(); got != "Notifications set to just mentions" {
		t.Errorf("subtitle %q", got)
	}
	if got := strings.Join(m.Rows(), ","); got != "desktop,mobile,overrides,follow,broadcast" {
		t.Errorf("rows (just mentions): %s", got)
	}
	e.shot("channel-notify-modal")

	check := func(what, name, value string, stored func(map[string]any) bool) {
		t.Helper()
		e.until(what, 10*time.Second, func() bool {
			p := last()
			return p != nil && p.Get("name") == name && p.Get("value") == value && !m.Pending() && stored(fakeChannelNotify(t, fs, ch))
		})
		p := last()
		if p.Get("channel_id") != ch || p.Get("global") != "false" || p.Get("sync") != "false" || p.Get("prefs") != "" || p.Get("channel_ids") != "" {
			t.Errorf("%s: params %v", what, p)
		}
	}
	m.Toggle("follow")
	check("follow every thread", "follow_all_threads", "true", func(c map[string]any) bool { return c["follow_all_threads"] == true })
	if !m.On("follow") || !e.a.Core().NotifyPrefs(ch).FollowAllThreads {
		t.Error("Follow every thread isn't on after the write")
	}
	m.Toggle("broadcast")
	check("broadcasts off", "suppress_at_channel", "true", func(c map[string]any) bool { return c["suppress_at_channel"] == true })
	if m.On("broadcast") {
		t.Error("Get broadcast messages still on")
	}
	m.Toggle("desktop")
	check("desktop push off", "desktop_push_enabled", "false", func(c map[string]any) bool { return c["desktop_push_enabled"] == false })
	if m.On("desktop") {
		t.Error("Desktop notifications still on")
	}

	// mobile: the overrides unfold; off writes nothing and folds them,
	// on writes the desktop setting back
	m.ToggleOverrides()
	if !m.OverridesShown() {
		t.Error("the mobile overrides didn't unfold")
	}
	e.shot("channel-notify-modal-overrides")
	m.SetMobile("everything")
	check("mobile everything", "mobile", "everything", func(c map[string]any) bool { return c["mobile"] == "everything" })
	m.Toggle("mobile")
	check("mobile off", "mobile", "nothing", func(c map[string]any) bool { return c["mobile"] == "nothing" })
	if m.On("mobile") || m.OverridesShown() {
		t.Errorf("mobile off: on %v, overrides shown %v", m.On("mobile"), m.OverridesShown())
	}
	m.ToggleOverrides() // disabled while mobile is off
	if m.OverridesShown() {
		t.Error("the overrides unfolded with mobile off")
	}
	m.Toggle("mobile")
	check("mobile back on", "mobile", "mentions_dms", func(c map[string]any) bool { return c["mobile"] == "mentions_dms" })

	// All new posts (from the menu): the badge row appears, broadcasts go
	m.Dismiss()
	pick("All new posts").Do()
	e.until("desktop everything", 10*time.Second, func() bool { return e.a.Core().NotifyPrefs(ch).Desktop == "everything" })
	pick("Advanced settings").Do()
	e.settle(150 * time.Millisecond)
	m, _ = topOverlay[*popups.ChannelNotifyModal](e.a)
	if m == nil {
		t.Fatal("no modal the second time")
	}
	if got := strings.Join(m.Rows(), ","); got != "desktop,mobile,overrides,badge,follow" {
		t.Errorf("rows (all new posts): %s", got)
	}
	if got := m.Subtitle(); got != "Notifications set to all new posts" {
		t.Errorf("subtitle %q", got)
	}
	m.Toggle("badge")
	check("badge every message", "badge_all_unreads", "true", func(c map[string]any) bool { return c["badge_all_unreads"] == true })
	e.shot("channel-notify-modal-everything")

	// a refused write shows Slack's error alert
	mu.Lock()
	fail = true
	mu.Unlock()
	m.Toggle("follow")
	e.until("the error alert", 10*time.Second, func() bool { return m.Failed() })
	e.shot("channel-notify-modal-error")
	if !e.a.Core().NotifyPrefs(ch).FollowAllThreads {
		t.Error("a refused write changed follow_all_threads")
	}
}

// The bell glyph follows the channel's setting: the ringing bell for All
// new posts, notifications-off when muted.
func TestHeaderBellIcon(t *testing.T) {
	e, _ := previewEnv(t)
	ch := fakeslackMemberChannel(e)
	e.openChannel(ch)
	e.settle(200 * time.Millisecond)
	conv := e.a.model().conv(ch)
	if got := e.a.headerBellIcon(ch, conv); got != "bell" {
		t.Errorf("just mentions: %q", got)
	}
	for _, it := range popups.ChannelNotifyItems(e.a, ch) {
		if it.Label == "All new posts" {
			if it.Icon != "notifications-all-new-posts" {
				t.Errorf("All new posts row icon %q", it.Icon)
			}
			it.Do()
		}
	}
	e.until("desktop everything", 10*time.Second, func() bool { return e.a.Core().NotifyPrefs(ch).Desktop == "everything" })
	if got := e.a.headerBellIcon(ch, e.a.model().conv(ch)); got != "notifications-all-new-posts" {
		t.Errorf("all new posts: %q", got)
	}
	e.shot("channel-header-bell-everything")
	muted := *conv
	muted.Muted = true
	if got := e.a.headerBellIcon(ch, &muted); got != "bell-off" {
		t.Errorf("muted: %q", got)
	}
}

// The header topic: hovering it shows Slack's "Edit" link; Edit opens
// "Edit topic", which saves with conversations.setTopic (a refusal keeps
// it open with getChannelTopicError's text).
func TestHeaderTopicEdit(t *testing.T) {
	e, fs := previewEnv(t)
	ch := fakeslackMemberChannel(e)
	var mu sync.Mutex
	var topics []string
	fs.SetHook(func(method string, form url.Values) (any, bool) {
		if method != "conversations.setTopic" {
			return nil, false
		}
		mu.Lock()
		defer mu.Unlock()
		topics = append(topics, form.Get("channel")+" "+form.Get("topic"))
		if len(topics) == 1 {
			return map[string]any{"ok": false, "error": "too_long"}, true
		}
		return map[string]any{"ok": true}, true
	})
	e.openChannel(ch)
	e.settle(300 * time.Millisecond)
	conv := e.a.model().conv(ch)
	if conv == nil || conv.Topic == "" {
		t.Skip("the fake channel has no topic")
	}
	if !e.a.canSetTopic(ch, conv) {
		t.Fatal("a member can't set the topic")
	}
	r := hdrTopicRect.Add(e.a.primaryOrigin)
	e.move(f32.Pt(float32(r.Min.X+4), float32(r.Min.Y+r.Dy()/2)))
	e.settle(100 * time.Millisecond)
	e.shot("channel-header-topic-hover")
	if !e.a.clicks.get("hdr.topicbox").Hovered() {
		t.Error("the topic box isn't hovered")
	}
	e.a.openTopicDialog(ch, conv)
	e.settle(150 * time.Millisecond)
	d, ok := topOverlay[*popups.TopicDialog](e.a)
	if !ok {
		t.Fatal("no Edit topic dialog")
	}
	if d.Text() != conv.Topic {
		t.Errorf("prefilled %q, want %q", d.Text(), conv.Topic)
	}
	e.shot("channel-topic-dialog")
	d.SetText("Release notes go here")
	if d.Save() {
		t.Error("a changed topic closed the dialog before Slack answered")
	}
	e.until("the refusal", 10*time.Second, func() bool { return d.Error() != "" })
	if !strings.Contains(d.Error(), "up to 250 characters") || d.Dismissed() {
		t.Errorf("after too_long: error %q, dismissed %v", d.Error(), d.Dismissed())
	}
	e.shot("channel-topic-dialog-error")
	d.Save()
	e.until("saved", 10*time.Second, func() bool { return d.Dismissed() })
	mu.Lock()
	defer mu.Unlock()
	if len(topics) != 2 || topics[1] != ch+" Release notes go here" {
		t.Errorf("setTopic calls %q", topics)
	}

	// a channel you aren't in has no Edit
	if preview := (convInfo{Kind: "channel", Preview: true, Topic: "x"}); e.a.canSetTopic(ch, &preview) {
		t.Error("a preview channel offers Edit")
	}
}
