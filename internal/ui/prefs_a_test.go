package ui

import (
	"context"
	"fmt"
	"image"
	"reflect"
	"strings"
	"sync"
	"testing"
	"time"

	"gioui.org/widget"

	"github.com/some-du6e/clack/internal/core"
	"github.com/some-du6e/clack/internal/richtext"
	"github.com/some-du6e/clack/internal/slack"
	"github.com/some-du6e/clack/internal/ui/huddleui"
	"github.com/some-du6e/clack/internal/ui/richview"
	"github.com/some-du6e/clack/internal/ui/theme"
	"github.com/some-du6e/clack/internal/ui/uipref"
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

// Messages & media > Emoji and Inline media: emoji_mode as_text turns
// emoji into their text (and convert_emoticons off, like EmojiPref);
// convert_emoticons is what the composer reads; obey_inline_img_limit
// decides whether a linked image over 20MB expands; one-click reactions
// write 1/0 and the custom slots.
func TestPrefsAuditAppliedRoundTripMessages(t *testing.T) {
	a, ca, fs := fakePrefsApp(t)
	t.Cleanup(uipref.Reset)
	p := &prefsModal{a: a, clicks: map[string]*widget.Clickable{}}
	click := func(page, key string) {
		t.Helper()
		pageRows(t, a, p, page)
		b, ok := p.clicks[key]
		if !ok {
			t.Fatalf("no %s on %s", key, page)
		}
		b.Click()
		pageRows(t, a, p, page)
	}
	click("messages_media", "pref.emoji_mode")
	waitFor(t, a, "emoji as text", func() bool {
		m, _ := fs.Pref("emoji_mode")
		e, _ := fs.Pref("convert_emoticons")
		return m == "as_text" && e == false && ca.Prefs().EmojiAsText && !ca.Prefs().ConvertEmoticons
	})
	doc := richtext.FromMessage(ca.S, &slack.Message{Text: "hi :wave::skin-tone-2: :tada:"})
	if got := richtext.SpansText(doc.Blocks[0].Spans); got != "hi :wave::skin-tone-2: :tada:" || doc.Jumbo {
		t.Fatalf("as text: %q jumbo=%v", got, doc.Jumbo)
	}
	for _, sp := range doc.Blocks[0].Spans {
		if sp.Kind == richtext.Emoji {
			t.Fatalf("an emoji span with emoji_mode as_text: %+v", sp)
		}
	}
	rows := pageRows(t, a, p, "messages_media")
	for _, r := range rows {
		if (r.Label == "Show JUMBOMOJI" || r.Label == "Convert my typed emoticons to emoji, so :D becomes 😃") && r.Enabled {
			t.Errorf("%q enabled with emoji as text", r.Label)
		}
	}
	click("messages_media", "pref.emoji_mode")
	waitFor(t, a, "emoji back", func() bool { m, _ := fs.Pref("emoji_mode"); return m == "default" && !ca.Prefs().EmojiAsText })
	click("messages_media", "pref.convert_emoticons")
	waitFor(t, a, "emoticons on", func() bool { return ca.Prefs().ConvertEmoticons })

	// obey_inline_img_limit: the box is its negation
	uipref.Set(ca.Prefs())
	big := &richtext.Image{URL: "https://x/big.png", Bytes: 0x1400000 + 1}
	if !richview.ImageTooLarge(big) {
		t.Fatal("a 20MB+ image expands with the limit on")
	}
	click("messages_media", "pref.obey_inline_img_limit")
	waitFor(t, a, "limit off", func() bool { v, _ := fs.Pref("obey_inline_img_limit"); return v == false && ca.Prefs().LargeImgs })
	uipref.Set(ca.Prefs())
	if richview.ImageTooLarge(big) {
		t.Fatal("a 20MB+ image held back with the limit off")
	}

	// one-click reactions
	click("messages_media", "pref.quick_reactions")
	waitFor(t, a, "quick reactions", func() bool {
		v, _ := fs.Pref("user_customized_quick_reactions_display_feature")
		return normNum(v) == float64(1) && ca.Prefs().QuickReactions
	})
	click("messages_media", "pref.quick_reactions=frequently_used_emoji_quick_reactions")
	waitFor(t, a, "frequent", func() bool {
		v, _ := fs.Pref("user_customized_quick_reactions_use_frequently_used_emoji")
		return v == true
	})
}

// Notifications > Reminders and Language & region > Time zone:
// reminder_notification_time is written as Slack's "H:mm"; the time zone
// select shows Slack's labels and writes tz; with set_tz_automatically
// the detected zone (mapped through Slack's tables) is written, at once
// and at sign-in (MaybeUpdateTimezone).
func TestPrefsAuditAppliedRoundTripTime(t *testing.T) {
	a, ca, fs := fakePrefsApp(t)
	p := &prefsModal{a: a, clicks: map[string]*widget.Clickable{}}
	if reminderMinutes("") != 540 || reminderMinutes("13:30") != 810 || reminderMinutes("x") != 540 {
		t.Fatal("reminderMinutes")
	}
	p.setPref("reminder_notification_time", "13:30")
	waitFor(t, a, "reminder time", func() bool {
		v, _ := fs.Pref("reminder_notification_time")
		return v == "13:30" && ca.PrefString("reminder_notification_time") == "13:30"
	})
	found := false
	for _, r := range pageRows(t, a, p, "notifications") {
		if r.Key == "sel.reminder_notification_time" && r.Label == "1:30 PM" && r.Enabled {
			found = true
		}
	}
	if !found {
		t.Fatal("the reminder select doesn't show 1:30 PM")
	}

	old := core.TestSystemZone
	t.Cleanup(func() { core.TestSystemZone = old })
	core.TestSystemZone = "Asia/Calcutta" // Slack lists it as Asia/Kolkata
	if z := core.DetectTimezone(); z != "Asia/Kolkata" {
		t.Fatalf("detected %q", z)
	}
	if z, err := ca.MaybeUpdateTimezone(context.Background()); err != nil || z != "Asia/Kolkata" {
		t.Fatalf("MaybeUpdateTimezone %q %v", z, err)
	}
	waitFor(t, a, "tz detected", func() bool { v, _ := fs.Pref("tz"); return v == "Asia/Kolkata" })
	if _, err := ca.RefreshPrefs(context.Background()); err != nil {
		t.Fatal(err)
	}
	click := func(key string) {
		pageRows(t, a, p, "language_region")
		p.clicks[key].Click()
		pageRows(t, a, p, "language_region")
	}
	click("pref.set_tz_automatically") // off: the select opens
	waitFor(t, a, "auto off", func() bool { v, _ := fs.Pref("set_tz_automatically"); return v == false })
	var sel prefsDrawn
	for _, r := range pageRows(t, a, p, "language_region") {
		if r.Key == "sel.tz" {
			sel = r
		}
	}
	if !sel.Enabled || sel.Label != "(UTC+05:30) Chennai, Kolkata, Mumbai, New Delhi" {
		t.Fatalf("tz select %+v", sel)
	}
	p.setPref("tz", core.SlackTimezones[0].ID)
	waitFor(t, a, "tz picked", func() bool { v, _ := fs.Pref("tz"); return v == "Pacific/Midway" })
	if z, _ := ca.MaybeUpdateTimezone(context.Background()); z != "" {
		t.Fatalf("wrote %q with set_tz_automatically off", z)
	}
	core.TestSystemZone = "Europe/Paris"
	click("pref.set_tz_automatically") // on: the detected zone at once
	waitFor(t, a, "auto on", func() bool {
		v, _ := fs.Pref("set_tz_automatically")
		z, _ := fs.Pref("tz")
		return v == true && z == "Europe/Brussels" // intl_api_manual comes first
	})
}

// Messages & media > Default Skin Tone is six :hand: radios (Slack's
// EmojiSkinTonePickerRadioGroup); Appearance shows the Miles Davis group
// first only under miles_davis_themes.
func TestPrefsSkinToneHandsAndMilesDavis(t *testing.T) {
	a, ca, fs := fakePrefsApp(t)
	p := &prefsModal{a: a, clicks: map[string]*widget.Clickable{}}
	pageRows(t, a, p, "messages_media")
	p.clicks["pref.preferred_skin_tone=4"].Click()
	pageRows(t, a, p, "messages_media")
	waitFor(t, a, "tone 4", func() bool { v, _ := fs.Pref("preferred_skin_tone"); return v == "4" && ca.Prefs().SkinTone == "4" })
	has := func(rows []prefsDrawn, label string) bool {
		for _, r := range rows {
			if r.Label == label {
				return true
			}
		}
		return false
	}
	if has(pageRows(t, a, p, "appearance"), theme.MilesDavisGroup) {
		t.Fatal("Miles Davis themes without the experiment")
	}
	ca.SetExperimentGroup("miles_davis_themes", "on")
	rows := pageRows(t, a, p, "appearance")
	if !has(rows, theme.MilesDavisGroup) || !has(rows, "Kind of Blue") {
		t.Fatalf("no Miles Davis group: %s", dumpRows(rows))
	}
	if id := theme.MilesDavisPresets[0].Theme.PresetID(); id != "miles-kind-of-blue" {
		t.Fatalf("PresetID %q", id)
	}
}

// VIP: Recommended comes from users.priority.getRecommendations (up to
// six who aren't VIPs, with Slack's reason line); Add and a search pick
// are users.priority.add, and the person joins "Added to your VIP list".
func TestPrefsAuditAppliedRoundTripVIP(t *testing.T) {
	a, ca, fs := fakePrefsApp(t)
	p := &prefsModal{a: a, clicks: map[string]*widget.Clickable{}}
	pageRows(t, a, p, "vip")
	waitFor(t, a, "recommendations", func() bool { return len(p.ra.vipRecs) > 0 })
	rows := pageRows(t, a, p, "vip")
	var rec string
	sawReason := false
	for _, r := range rows {
		if strings.HasPrefix(r.Key, "row.vip_rec.") && rec == "" {
			rec = strings.TrimPrefix(r.Key, "row.vip_rec.")
		}
		if r.Kind == "text" && r.Label == "Your manager" {
			sawReason = true
		}
	}
	if rec == "" || !sawReason {
		t.Fatalf("no recommendation rows: %s", dumpRows(rows))
	}
	p.clicks["row.vip_rec."+rec].Click()
	pageRows(t, a, p, "vip")
	waitFor(t, a, "recommended VIP added", func() bool {
		v, _ := fs.Pref("vip_users")
		vs, _ := v.(string)
		return strings.Contains(vs, rec) && reflect.DeepEqual(ca.VIPs(), []string{rec})
	})
	rows = pageRows(t, a, p, "vip")
	added := false
	for _, r := range rows {
		if r.Key == "row.vip."+rec {
			added = true
		}
		if r.Key == "row.vip_rec."+rec {
			t.Fatal("a VIP still listed as recommended")
		}
	}
	if !added {
		t.Fatalf("not under Added: %s", dumpRows(rows))
	}
	// search: the fake's directory answers users/search
	p.ra.vipQ.SetText("a")
	p.searchVIPs("a")
	waitFor(t, a, "search hits", func() bool { return len(p.ra.vipHits) > 0 })
	rows = pageRows(t, a, p, "vip")
	var hit string
	for _, r := range rows {
		if strings.HasPrefix(r.Key, "row.vip_add.") {
			hit = strings.TrimPrefix(r.Key, "row.vip_add.")
			break
		}
	}
	if hit == "" || hit == rec {
		t.Fatalf("no search result row (or a VIP offered): %q %s", hit, dumpRows(rows))
	}
	p.clicks["row.vip_add."+hit].Click()
	pageRows(t, a, p, "vip")
	waitFor(t, a, "searched VIP added", func() bool { return len(ca.VIPs()) == 2 && ca.VIPs()[1] == hit })
}

// Messages & media > Bring emails into Slack: Get a Forwarding Address is
// users.emailtoken.create and shows the address; reopening reads it back
// (users.emailtoken.get); disabling deletes it.
func TestPrefsEmailForwarding(t *testing.T) {
	a, ca, _ := fakePrefsApp(t)
	p := &prefsModal{a: a, clicks: map[string]*widget.Clickable{}}
	pageRows(t, a, p, "messages_media")
	waitFor(t, a, "emailtoken.get", func() bool { return p.ra.emailsGot })
	p.clicks["button.email_forwarding"].Click()
	pageRows(t, a, p, "messages_media")
	waitFor(t, a, "an address", func() bool {
		return len(p.ra.emails) == 1 && strings.HasSuffix(p.ra.emails[0].Address, "@fake.slack.com")
	})
	if ts, err := ca.EmailTokens(context.Background()); err != nil || len(ts) != 1 {
		t.Fatalf("emailtoken.get: %v %v", ts, err)
	}
	p2 := &prefsModal{a: a, clicks: map[string]*widget.Clickable{}}
	pageRows(t, a, p2, "messages_media")
	waitFor(t, a, "read back", func() bool { return p2.ra.emailsGot && len(p2.ra.emails) == 1 })
	rows := pageRows(t, a, p2, "messages_media")
	found := false
	for _, r := range rows {
		if r.Kind == "button" && r.Label == "Copy" {
			found = true
		}
		if r.Label == "Get a Forwarding Address" {
			t.Fatal("the button shows with an address")
		}
	}
	if !found {
		t.Fatalf("no Copy: %s", dumpRows(rows))
	}
	if err := ca.DeleteEmailToken(context.Background(), p2.ra.emails[0].ID.String()); err != nil {
		t.Fatal(err)
	}
	if ts, err := ca.EmailTokens(context.Background()); err != nil || len(ts) != 0 {
		t.Fatalf("after delete %v %v", ts, err)
	}
}

// Appearance > Custom theme: Import applies a theme string to ia_theme
// and turns window gradients off; Surprise me writes a random palette
// theme (useCustomHex, Slack's randomizeTheme).
func TestPrefsAuditAppliedRoundTripThemeButtons(t *testing.T) {
	a, _, fs := fakePrefsApp(t)
	p := &prefsModal{a: a, clicks: map[string]*widget.Clickable{}, themeTab: "custom"}
	for _, r := range pageRows(t, a, p, "appearance") {
		if (r.Label == "Import" || r.Label == "Surprise me") && !r.Enabled {
			t.Fatalf("%s greyed", r.Label)
		}
	}
	if p.applyThemeString("nope") {
		t.Fatal("junk imported")
	}
	if !p.applyThemeString("#124426, #350D36, #EEB317, #392104") {
		t.Fatal("import refused")
	}
	iaTheme := func() theme.IATheme {
		v, _ := fs.Pref("ia_theme")
		s, _ := v.(string)
		th, _ := theme.ParseIATheme(s)
		return th
	}
	waitFor(t, a, "imported", func() bool {
		g, _ := fs.Pref("window_background_gradients")
		th := iaTheme()
		return th.UseCustomHex && th.Primary.Custom == "#124426" && th.Important.Custom == "#392104" && g == false
	})
	pageRows(t, a, p, "appearance")
	p.clicks["button.theme_surprise"].Click()
	pageRows(t, a, p, "appearance")
	waitFor(t, a, "surprise", func() bool {
		th := iaTheme()
		return th.UseCustomHex && th.Primary.Custom == "" && th.Primary.Palette != ""
	})
}

// Appearance > Custom theme: the pencil opens the colour popover; its
// square/slider and hex field set the role's colour (a custom primary
// also gets getBrightnessFromCustomHex's brightness).
func TestColorPopover(t *testing.T) {
	for _, hex := range []string{"#611F69", "#FF0000", "#00FF7F", "#123ABC", "#FFFFFF", "#000000"} {
		h, s, v := hexToHSV(hex)
		c := hsvToRGB(h, s, v)
		if got := strings.ToUpper(fmt.Sprintf("#%02X%02X%02X", c.R, c.G, c.B)); got != hex {
			t.Errorf("%s round-trips to %s", hex, got)
		}
	}
	a, _, fs := fakePrefsApp(t)
	pm := &prefsModal{a: a, clicks: map[string]*widget.Clickable{}, themeTab: "custom"}
	var got []string
	p := newColorPopover(a, image.Pt(10, 10), "#611F69", func(h string) { got = append(got, h) })
	p.h, p.s, p.v = 0, 1, 1
	p.apply(p.hex())
	p.ed.SetText("123abc")
	p.commitText()
	p.ed.SetText("nope")
	p.commitText()
	if !reflect.DeepEqual(got, []string{"#FF0000", "#123ABC"}) || p.ed.Text() != "123ABC" {
		t.Fatalf("applied %v, field %q", got, p.ed.Text())
	}
	found := false
	for _, r := range pageRows(t, a, pm, "appearance") {
		if r.Key == "button.hexedit.primary" {
			found = true
		}
	}
	if !found {
		t.Fatal("no colour popover trigger")
	}
	pm.setRoleHex(0, "#F2F2F2") // a light primary: low brightness
	waitFor(t, a, "brightness", func() bool {
		v, _ := fs.Pref("ia_theme")
		s, _ := v.(string)
		th, _ := theme.ParseIATheme(s)
		return th.Primary.Custom == "#F2F2F2" && th.Brightness == theme.BrightnessFromCustomHex("#F2F2F2") && th.Brightness <= 2
	})
}
