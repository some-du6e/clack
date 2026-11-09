package ui

import (
	"reflect"
	"sync"
	"testing"
	"time"

	"gioui.org/widget"

	"github.com/some-du6e/yoloslackgoclient/internal/core"
	"github.com/some-du6e/yoloslackgoclient/internal/richtext"
	"github.com/some-du6e/yoloslackgoclient/internal/slack"
	"github.com/some-du6e/yoloslackgoclient/internal/ui/huddleui"
	"github.com/some-du6e/yoloslackgoclient/internal/ui/uipref"
)

// soundLog records what huddleui.PlaySlackSound would play.
type soundLog struct {
	mu    sync.Mutex
	files []string
}

func (s *soundLog) hook(t *testing.T) {
	huddleui.PlayHook = func(f string) { s.mu.Lock(); s.files = append(s.files, f); s.mu.Unlock() }
	t.Cleanup(func() { huddleui.PlayHook = nil })
}

func (s *soundLog) got() []string {
	s.mu.Lock()
	defer s.mu.Unlock()
	return append([]string(nil), s.files...)
}

// Notifications > Sounds & appearance: mute_sounds / desktop_sound decide
// the sound a message notification plays (canPlayMessageSound), and a
// pick in the select previews it; huddle sounds feed the invite ring.
func TestPrefsAuditAppliedRoundTripSounds(t *testing.T) {
	a, ca, fs := fakePrefsApp(t)
	t.Cleanup(uipref.Reset)
	uipref.Set(ca.Prefs())
	p := &prefsModal{a: a, clicks: map[string]*widget.Clickable{}}
	var snd soundLog
	snd.hook(t)
	a.notify = &recNotifier{}
	ca.SetDNDStatus(core.DNDStatus{})

	p.setNotify("desktop_sound", "hummus.mp3")
	waitFor(t, a, "desktop_sound", func() bool {
		return fakeNotify(fs, "desktop_sound") == "hummus.mp3" && ca.GlobalNotifyString("desktop_sound") == "hummus.mp3"
	})
	a.notifyMessage("t", "b")
	waitFor(t, a, "the message sound", func() bool { return reflect.DeepEqual(snd.got(), []string{"hummus.mp3"}) })

	p.setPref("mute_sounds", true)
	waitFor(t, a, "mute_sounds", func() bool { v, _ := fs.Pref("mute_sounds"); return v == true && ca.PrefBool("mute_sounds", false) })
	a.notifyMessage("t", "b")
	time.Sleep(50 * time.Millisecond)
	if n := len(snd.got()); n != 1 {
		t.Fatalf("a sound played with mute_sounds on: %v", snd.got())
	}
	rows := pageRows(t, a, p, "notifications")
	for _, r := range rows {
		if r.Key == "sel.desktop_sound" {
			t.Fatal("the messages sound select shows with sounds muted")
		}
	}
	p.setPref("mute_sounds", false)
	waitFor(t, a, "unmuted", func() bool { return !ca.PrefBool("mute_sounds", true) })

	// DND holds the sound (canPlaySound)
	ca.SetDNDStatus(core.DNDStatus{SnoozeEnabled: true, SnoozeEnd: time.Now().Add(time.Hour).Unix()})
	a.notifyMessage("t", "b")
	time.Sleep(50 * time.Millisecond)
	if n := len(snd.got()); n != 1 {
		t.Fatalf("a sound played under DND: %v", snd.got())
	}
	ca.SetDNDStatus(core.DNDStatus{})

	// the select: Slack's names in Slack's order, a pick plays and saves
	var items []MenuItem
	for _, s := range huddleui.MessageSounds {
		items = append(items, MenuItem{Label: s.Label})
	}
	if items[0].Label != "None" || items[1].Label != "Ding" || items[len(items)-1].Label != "Boop" {
		t.Fatalf("message sounds %v", huddleui.MessageSounds)
	}
	soundItems(huddleui.MessageSounds, func(v string) { p.setNotify("desktop_sound", v) })[2].Do() // Boing
	waitFor(t, a, "Boing", func() bool { return fakeNotify(fs, "desktop_sound") == "animal_stick.mp3" })
	if g := snd.got(); g[len(g)-1] != "animal_stick.mp3" {
		t.Fatalf("picking Boing didn't preview it: %v", g)
	}

	// huddle sounds: huddle_invite_sound_v2 and mute_huddle_sounds are
	// what the invite ring plays (huddleui.InviteSound)
	soundItems(huddleui.HuddleSounds, func(v string) { p.setPref("huddle_invite_sound_v2", v) })[len(huddleui.HuddleSounds)-1].Do()
	waitFor(t, a, "Boop Plus", func() bool {
		v, _ := fs.Pref("huddle_invite_sound_v2")
		return v == "boop_remix.mp3" && a.huddles().InviteSound() == "boop_remix.mp3"
	})
	p.setPref("mute_huddle_sounds", true)
	waitFor(t, a, "huddle sounds muted", func() bool {
		v, _ := fs.Pref("mute_huddle_sounds")
		return v == true && a.huddles().InviteSound() == ""
	})
}

// Notifications > Channel keywords: the field writes global_keywords
// (setNotifications, ", " stripped); core reads them back as highlight
// words, which count as mentions and are highlighted in messages.
func TestPrefsAuditAppliedRoundTripKeywords(t *testing.T) {
	a, ca, fs := fakePrefsApp(t)
	p := &prefsModal{a: a, clicks: map[string]*widget.Clickable{}}
	pageRows(t, a, p, "notifications")
	if p.ra.kw == nil {
		t.Fatal("no keywords field")
	}
	p.ra.kw.SetText("deploy, ship it")
	p.ra.kwDirty = true
	p.close() // unmount flushes the debounce, like Slack's
	waitFor(t, a, "global_keywords", func() bool {
		return fakeNotify(fs, "global_keywords") == "deploy,ship it" && reflect.DeepEqual(ca.Prefs().Keywords, []string{"deploy", "ship it"})
	})
	doc := richtext.FromMessage(ca.S, &slack.Message{Text: "time to Deploy the app"})
	var hl []string
	for _, sp := range doc.Blocks[0].Spans {
		if sp.Style&richtext.Highlight != 0 {
			hl = append(hl, sp.Text)
		}
	}
	if !reflect.DeepEqual(hl, []string{"Deploy"}) {
		t.Fatalf("highlighted %q", hl)
	}
	// more than 100: Slack's error, nothing saved
	p2 := &prefsModal{a: a, clicks: map[string]*widget.Clickable{}}
	pageRows(t, a, p2, "notifications")
	if got := p2.ra.kw.Text(); got != "deploy, ship it" {
		t.Fatalf("field shows %q", got)
	}
	if keywordCount(stripKeywordWhitespace("a, b, , c")) != 3 {
		t.Fatal("keyword count")
	}
}

// Sounds & appearance (macOS): mac_ssb_bullet decides the "•" badge for
// unreads without mentions; mac_ssb_bounce bounces the icon on each
// notification, once ("short") or until Clack is brought forward.
func TestPrefsAuditAppliedRoundTripDock(t *testing.T) {
	a, ca, fs := fakePrefsApp(t)
	t.Cleanup(uipref.Reset)
	dockAnyOS = true
	var badge []string
	var bounces []bool
	oldB, oldN := dockBadge, dockBounce
	dockBadge = func(l string) { badge = append(badge, l) }
	dockBounce = func(c bool) { bounces = append(bounces, c) }
	t.Cleanup(func() { dockAnyOS, dockBadge, dockBounce = false, oldB, oldN })
	p := &prefsModal{a: a, clicks: map[string]*widget.Clickable{}}
	snap := func() { uipref.Set(ca.Prefs()) }
	m := a.model()
	m.HomeUnread, m.HomeCount = true, 0
	snap()
	a.updateDock()
	if badge[len(badge)-1] != "•" {
		t.Fatalf("badge %q, want •", badge)
	}
	p.setPref("mac_ssb_bullet", false)
	waitFor(t, a, "mac_ssb_bullet", func() bool { v, _ := fs.Pref("mac_ssb_bullet"); return v == false && ca.Prefs().NoMacBullet })
	snap()
	a.updateDock()
	if badge[len(badge)-1] != "" {
		t.Fatalf("badge %q with the bullet off", badge)
	}
	m.HomeCount = 3
	a.updateDock()
	if badge[len(badge)-1] != "3" {
		t.Fatalf("badge %q, want 3", badge)
	}

	a.notify = &recNotifier{}
	a.Notify("t", "b")
	if len(bounces) != 0 {
		t.Fatal("bounced with mac_ssb_bounce unset")
	}
	p.setPref("mac_ssb_bounce", "long")
	waitFor(t, a, "mac_ssb_bounce", func() bool { v, _ := fs.Pref("mac_ssb_bounce"); return v == "long" && ca.Prefs().MacBounce == "long" })
	snap()
	a.Notify("t", "b")
	p.setPref("mac_ssb_bounce", "short")
	waitFor(t, a, "short", func() bool { return ca.Prefs().MacBounce == "short" })
	snap()
	a.Notify("t", "b")
	if !reflect.DeepEqual(bounces, []bool{true, false}) {
		t.Fatalf("bounces %v", bounces)
	}
}

// Home: home_unread_dot off hides the Home tab's dot and badge;
// show_sidebar_avatars and should_show_connect_section round-trip to
// what the sidebar draws from (core's sections test covers the section).
func TestPrefsAuditAppliedRoundTripHome(t *testing.T) {
	a, ca, fs := fakePrefsApp(t)
	t.Cleanup(uipref.Reset)
	p := &prefsModal{a: a, clicks: map[string]*widget.Clickable{}}
	snap := func() { uipref.Set(ca.Prefs()) }
	m := a.model()
	m.HomeUnread = true
	snap()
	if _, dot := a.railBadge("home"); !dot {
		t.Fatal("no Home dot for unreads")
	}
	p.setPref("home_unread_dot", false)
	waitFor(t, a, "home_unread_dot", func() bool { v, _ := fs.Pref("home_unread_dot"); return v == false && ca.Prefs().NoHomeDot })
	snap()
	if n, dot := a.railBadge("home"); dot || n != 0 {
		t.Fatalf("Home badge %d dot %v with home_unread_dot off", n, dot)
	}
	p.setPref("show_sidebar_avatars", false)
	waitFor(t, a, "show_sidebar_avatars", func() bool { v, _ := fs.Pref("show_sidebar_avatars"); return v == false && !ca.Prefs().SidebarAvatars })
	p.setPref("should_show_connect_section", false)
	waitFor(t, a, "should_show_connect_section", func() bool {
		v, _ := fs.Pref("should_show_connect_section")
		return v == false && !ca.PrefBool("should_show_connect_section", true)
	})
	p.setPref("disable_auto_channel_sort", true)
	waitFor(t, a, "disable_auto_channel_sort", func() bool { v, _ := fs.Pref("disable_auto_channel_sort"); return v == true })
}
