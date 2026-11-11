package ui

import (
	"encoding/json"
	"github.com/some-du6e/clack/internal/spell"
	"image"
	"reflect"
	"strings"
	"sync"
	"testing"
	"time"

	"gioui.org/op"
	"gioui.org/widget"

	"github.com/some-du6e/clack/internal/core"
	"github.com/some-du6e/clack/internal/ui/theme"
	"github.com/some-du6e/clack/internal/ui/uipref"
)

// pageRows draws one Preferences page against the fake account and
// returns every row it drew, in order.
func pageRows(t *testing.T, a *App, p *prefsModal, id string) []prefsDrawn {
	t.Helper()
	var rows []prefsDrawn
	p.rec = &rows
	p.section = id
	gtx := ttContext(new(op.Ops), image.Pt(800, 624))
	a.slackcFrame(gtx)
	p.layout(gtx)
	p.rec = nil
	return rows
}

// want is one expected row: kind and label (a trailing "*" matches a
// prefix). disabled marks a row Slack has that Clack draws greyed.
type want struct {
	kind, label string
	disabled    bool
}

func (w want) matches(r prefsDrawn) bool {
	if r.Kind != w.kind {
		return false
	}
	if p, ok := strings.CutSuffix(w.label, "*"); ok {
		return strings.HasPrefix(r.Label, p)
	}
	return r.Label == w.label
}

func h2(l string) want     { return want{"h2", l, false} }
func lg(l string) want     { return want{"legend", l, false} }
func ck(l string) want     { return want{"check", l, false} }
func ckOff(l string) want  { return want{"check", l, true} }
func rd(l string) want     { return want{"radio", l, false} }
func rdOff(l string) want  { return want{"radio", l, true} }
func sel(l string) want    { return want{"select", l, false} }
func selOff(l string) want { return want{"select", l, true} }
func btn(l string) want    { return want{"button", l, false} }
func btnOff(l string) want { return want{"button", l, true} }

// Each page's controls, labels, headings and order, as Slack's desktop
// client draws them (the live dialog, 2026-11-08, and its prefs-dialog
// chunk for the desktop-only rows). Rows match in order; text rows in
// between aren't listed.
var slackPages = map[string][]want{
	"notifications": {
		h2("Messaging defaults"), lg("How to notify you"), ck("Desktop notifications"), ck("Mobile notifications"),
		lg("What to notify you about"), sel("Mentions and direct messages"),
		lg("Also notify you about:"), ck("Thread replies"), ck("Messages from VIPs, even if notifications are paused"), ck("New huddles"),
		ck("Set up mobile overrides"),
		lg("Channel keywords"), {"input", "", false},
		h2("Reminders"), lg("Set a default time for reminder notifications:"), sel("9:00 AM"),
		lg("When you’re not active on desktop…"), sel("As soon as I’m inactive"),
		ck("If I’m not active for more than 2 weeks, send me a mobile notification, summarizing activity I’ve missed"),
		btn("Set Email Notifications"),
		h2("Sounds & appearance"), ck("Include a preview of the message in each notification"),
		ck("Mute all messaging sounds from Slack"), lg("Notification sound (messages)"), sel("Knock Brush"),
		ck("Mute all huddle sounds from Slack"), lg("Notification sound (huddles)"), sel("Boop Plus"),
	},
	"vip": {
		lg("VIP Notifications"), ck("Always allow notifications from VIPs"),
		lg("VIP List"), {"input", "", false}, lg("Added to your VIP list"),
	},
	"navigation": {
		lg("Configure these tabs in the navigation bar:"), ckOff("Home"), ck("DMs"), ckOff("Activity"), ck("Files"),
		btnOff("Move up"), btnOff("Move down"),
		lg("Navigation Tab Appearance"), rd("Icons & text"), rd("Icons only"),
		lg("App agents & assistants"), ckOff("Show AI Agents in top bar"), ck("Show Agents in direct messages"),
	},
	"sidebar": {
		lg("Channel organization"), ck("Show suggestions for getting started in new channels"), ck("Sort channels into sidebar sections automatically"),
		lg("Show activity"), ck("Show a dot on the Home icon when there is unread activity"),
		lg("Always show in the sidebar:"), ck("Unreads"), ck("Huddles"), ck("Threads"), ck("Saved"), ck("Drafts & sent"), ck("Directories"),
		lg("Filter conversations by:"), rd("Active only"), rd("Unreads"), rd("Mentions"), rd("All"),
		lg("Sort conversations by:"), rd("A–Z"), rd("Most recent"), rd("Priority"),
		lg("Sidebar Organization"), ck("Show profile photos next to DMs"), ck("Separate private channels from public ones in sidebar"),
		ck("Separate direct messages and apps from channels in sidebar"), ck("Move items with unread Mentions to top of sections"),
		ck("Organize external conversations into the External Connections section"), ck("Hide muted channels from your sidebar"),
	},
	"appearance": {
		lg("Font"), sel("Lato (Default)"), lg("Color Mode"), rd("Light"), rd("Dark"), rd("System"),
		ck("Always use dark color mode when in focus mode."),
		{"tab", "Slack themes", false}, {"tab", "Custom theme", false},
		lg("Single color"), rd("Aubergine"), rd("Clementine"), lg("Vision assistive"), rd("Tritanopia"),
		lg("Fun and new"), rd("Raspberry Beret"), lg("Updated classics"), rd("Choco Mint"), rd("Sweet Treat"),
	},
	"messages_media": {
		lg("Theme"), rd("Clean"), rd("Compact"), {"example-clean", "Slackbot", false},
		lg("Names"), rd("Full & display names"), rd("Just display names"), {"example-clean", "Meg", false}, {"link", "your profile", false},
		lg("Additional options"), ck("Display information about who is currently typing a message"), ck("Show times with 24-hour clock"),
		ck("Display color swatches next to hexadecimal values"),
		lg("Emoji"), lg("Default Skin Tone"), rd("No skin tone"), rd("light skin tone"), rd("medium-light skin tone"), rd("medium skin tone"),
		rd("medium-dark skin tone"), rd("dark skin tone"),
		ck("Display emoji as plain text"), ck("Show JUMBOMOJI"), ck("Convert my typed emoticons to emoji, so :D becomes 😃"),
		ck("Show one-click reactions on messages"),
		lg("Inline media & links"), ck("Show images and files uploaded to Slack"), ck("Show images and files from linked websites"),
		ck("Even if they’re larger than 20MB"), ck("Show text previews of linked websites"),
		lg("Bring emails into Slack"), btn("Get a Forwarding Address"),
	},
	"language_region": {
		lg("Language"), sel("English (US)"), lg("Time zone"), ck("Set time zone automatically"), selOff("(UTC-08:00) Pacific Time (US and Canada)"),
		lg("Keyboard layout"), selOff("English (US)"), lg("Spellcheck"), ck("Enable spellcheck on your messages and canvases"),
		{"select", spell.Labels[spell.DefaultLanguages()[0]], false}, // the languages (Windows, Linux)
	},
	"accessibility": {
		lg("Display"), lg("Simplified layout mode"), btn("Turn On"), lg("Alt text reminders"),
		ckOff("Remind me to add an image description on image uploads"), lg("Other display options"),
		ckOff("Show previews when hovering over tabs"), ck("Underline links to websites"), ck("Allow animations"),
		lg("Keyboard"), lg("Press ↑ in the empty message field to:"), rd("Move focus to the message list"),
		rd("Move focus to the message list, only if the message field is empty"), rd("Edit your last message"),
		lg("Lists keyboard interactions"), rd("Data entry mode (default)"), rd("Full keyboard mode (best for assistive technology)"),
		lg("Screen reader"), lg("Message verbosity"), lg("Screen reader announcements"),
		ckOff("Automatically read new messages in the current conversation"), ckOff("Read emoji reactions received in a huddle"),
		ckOff("Play a sound when an emoji is sent or received in a huddle"),
	},
	"mark_as_read": {
		lg("When I view a channel:"), rd("Start me where I left off, and mark the channel read"),
		rd("Start me at the newest message, and mark the channel read"), rd("Start me at the newest message, but leave unseen messages unread"),
		lg("When I mark everything as read:"), ck("Prompt to confirm"), lg("Keyboard shortcuts"),
	},
	"video_audio": {
		lg("Camera"), lg("Microphone"), ck("Enable automatic gain control"), ck("Enable noise suppression"),
		lg("When audio or video is playing…"), ckOff("Automatically dock to corner upon scroll or when navigating away"),
		lg("When joining a huddle…"), ck("Mute my microphone"), ck("Automatically turn on captions when you’re in a huddle"),
		ck("Send a warning if you’re starting a huddle in a channel with more than 150 members"),
		ck("Blur your video background when you’re in a huddle with your camera on"), ck("Show audio and video preview"),
		lg("When Slack is in the background:"), ck("Allow keyboard shortcut to mute"),
		lg("When your screen saver starts or computer locks…"), ck("Automatically leave huddles"),
		lg("Huddle motion"), ckOff("Allow for reactions & effects"), lg("When alone in a huddle…"), ck("Play music"),
	},
	"salesforce":         {h2("Salesforce orgs"), h2("No Salesforce org")},
	"connected_accounts": {h2("Connected accounts"), lg("Apps connected through workflows")},
	"privacy_visibility": {
		lg("Slack Connect discoverability"), rd("Anyone with your email address*"), rd("No one"),
		lg("Preferences only apply to this workspace"), btn("Find my workspaces"),
		lg("Contact sharing"), rd("All your contacts"), rd("Only people at *"), rd("No one"),
		lg("Invitations you’ve blocked"), lg("People you’ve hidden"),
	},
	"advanced": {
		lg("Input options"), ck("Enable smart quotes and symbols"), ck("When typing code with ```, Enter should not send the message."),
		ck("Format messages with markup"), ck("Shorten external links"),
		lg("When writing a message, press Enter to…"), rd("Send the message"), rd("Start a new line (use *"),
		lg("Search options"), lg("Exclude these channels from search results:"), {"select", "", false},
		lg("Sort option default:"), rd("Most relevant"), rd("Last used:"),
		lg("Download location"), lg("Confirmations and warnings"),
		ck("Show me a confirmation when I use *"), ck("Ask if I want to toggle my away status when I log in after having set myself away"),
		ck("Warn me about potentially malicious links"), ckOff("Warn me when sharing files with external organizations"),
		ckOff("Warn me when sharing canvases with external organizations"),
		lg("Other options"), ck("Send me occasional channel suggestions"), ck("Send me occasional surveys"),
	},
}

// Rows Clack used to show that Slack has no such control.
var invented = []string{
	"Show the sidebar", "Reset sidebar width", "Emoji set", "Apple", "Google", "Brightness", "Privacy Mode",
	"Enable auto-detection", "Limit message line length", "On this computer", "Show desktop notifications",
	"Display muted items outside of sidebar menus", "Set my status to “In a huddle”", "Turn off my video",
	"Copy message text as Markdown",
}

func TestPrefsPagesMatchSlack(t *testing.T) {
	a, _, _ := fakePrefsApp(t)
	for id, ws := range slackPages {
		p := &prefsModal{a: a, clicks: map[string]*widget.Clickable{}}
		rows := pageRows(t, a, p, id)
		i := 0
		for _, w := range ws {
			for i < len(rows) && !w.matches(rows[i]) {
				i++
			}
			if i == len(rows) {
				t.Errorf("%s: no %s %q (in order) in %s", id, w.kind, w.label, dumpRows(rows))
				break
			}
			if rows[i].Enabled == w.disabled {
				t.Errorf("%s: %s %q enabled=%v, want %v", id, w.kind, w.label, rows[i].Enabled, !w.disabled)
			}
			i++
		}
		for _, r := range rows {
			for _, bad := range invented {
				if r.Kind != "text" && r.Label == bad {
					t.Errorf("%s: %q isn't one of Slack's controls", id, bad)
				}
			}
		}
		p.close()
	}
	// Custom theme: the four colour fields with Slack's default hexes
	p := &prefsModal{a: a, clicks: map[string]*widget.Clickable{}, themeTab: "custom"}
	rows := pageRows(t, a, p, "appearance")
	var got []string
	for _, r := range rows {
		if r.Kind == "hex" || r.Kind == "check" || (r.Kind == "button" && strings.HasPrefix(r.Key, "button.theme_")) {
			got = append(got, r.Label)
		}
	}
	wantCustom := []string{"Always use dark color mode when in focus mode.", "Share", "Import", "Surprise me",
		"System navigation #611F69", "Selected items #F9EDFF", "Presence indication #4CC894", "Notifications #EABDFB",
		"Window gradient", "Darker sidebars"}
	if !reflect.DeepEqual(got, wantCustom) {
		t.Errorf("Custom theme rows %q,\nwant %q", got, wantCustom)
	}
}

func dumpRows(rows []prefsDrawn) string {
	var b strings.Builder
	for _, r := range rows {
		if r.Kind != "text" {
			b.WriteString("\n  " + r.Kind + " " + r.Label)
		}
	}
	return b.String()
}

// recNotifier counts desktop notifications.
type recNotifier struct {
	mu sync.Mutex
	n  int
}

func (r *recNotifier) Notify(string, string) error { r.mu.Lock(); r.n++; r.mu.Unlock(); return nil }
func (r *recNotifier) NotifyActions(t, b string, _ []string, _ func(string)) error {
	return r.Notify(t, b)
}
func (r *recNotifier) count() int { r.mu.Lock(); defer r.mu.Unlock(); return r.n }

// The prefs this audit started applying: each row writes Slack's key with
// Slack's value, core reads it back, and Clack acts on it.
func TestPrefsAuditAppliedRoundTrip(t *testing.T) {
	a, ca, fs := fakePrefsApp(t)
	t.Cleanup(uipref.Reset)
	p := &prefsModal{a: a, clicks: map[string]*widget.Clickable{}}
	snap := func() { uipref.Set(ca.Prefs()) }

	// Notifications > Desktop notifications: global_desktop_push_enabled
	rn := &recNotifier{}
	a.notify = rn
	p.setNotify("global_desktop_push_enabled", false)
	waitFor(t, a, "desktop push off", func() bool { return fakeNotify(fs, "global_desktop_push_enabled") == false && ca.Prefs().NoDesktopPush })
	snap()
	a.Notify("title", "body")
	time.Sleep(50 * time.Millisecond)
	if rn.count() != 0 {
		t.Fatal("a notification showed with Desktop notifications off")
	}
	p.setNotify("global_desktop_push_enabled", true)
	waitFor(t, a, "desktop push on", func() bool { return fakeNotify(fs, "global_desktop_push_enabled") == true && !ca.Prefs().NoDesktopPush })
	snap()
	a.Notify("title", "body")
	waitFor(t, a, "a notification", func() bool { return rn.count() == 1 })

	// What to notify you about: desktop and mobile in one setNotifications
	p.setNotifies(map[string]any{"global_desktop": "everything", "global_mobile": "everything"})
	waitFor(t, a, "both levels", func() bool {
		return fakeNotify(fs, "global_desktop") == "everything" && fakeNotify(fs, "global_mobile") == "everything" && ca.Prefs().GlobalDesktop == "everything"
	})
	p.setNotify("push_idle_wait", "5")
	waitFor(t, a, "push_idle_wait", func() bool { return normNum(fakeNotify(fs, "push_idle_wait")) == float64(5) })

	// Advanced > Enter starts a new line: msg_input_send_btn
	p.setPref("msg_input_send_btn", true)
	waitFor(t, a, "msg_input_send_btn", func() bool { v, _ := fs.Pref("msg_input_send_btn"); return v == true && ca.Prefs().SendOnModEnter })

	// Home > Always show in the sidebar: Threads off hides its link
	p.setPref("enable_threads_view", false)
	waitFor(t, a, "enable_threads_view", func() bool {
		v, _ := fs.Pref("enable_threads_view")
		return v == false && reflect.DeepEqual(ca.Prefs().HiddenViews, []string{"enable_unread_view", "enable_threads_view"})
	})
	snap()
	a.sidebar.rebuild(a.model())
	for _, r := range a.sidebar.rows {
		if r.kind == sbLink && r.key == "threads" {
			t.Fatal("Threads is still in the sidebar")
		}
	}
	if len(a.sidebar.rows) == 0 || a.sidebar.rows[0].key != "huddles" {
		t.Fatalf("sidebar starts with %+v", a.sidebar.rows[:1])
	}

	// Navigation: a tab off (tab_rail_customization + reasons), order, compact
	p.setRailTab(core.TabFiles, false)
	waitFor(t, a, "Files off", func() bool {
		v, _ := fs.Pref("tab_rail_customization")
		var m map[string]bool
		vs, _ := v.(string)
		_ = json.Unmarshal([]byte(vs), &m)
		r, _ := fs.Pref("tab_rail_customization_reasons")
		rs, _ := r.(string)
		return m != nil && !m[core.TabFiles] && m[core.TabHome] && strings.Contains(rs, `"unified-files":"manual"`) &&
			indexOf(ca.Rail().Rail, core.TabFiles) < 0 && indexOf(ca.Rail().Available, core.TabFiles) >= 0
	})
	shown := ca.Rail().Available
	i := indexOf(shown, core.TabDMs)
	p.nav.active = core.TabDMs // the listbox's active option (hovered or arrowed to)
	p.moveActive(p.navTabs(), -1)
	waitFor(t, a, "DMs moved up", func() bool {
		av := ca.Rail().Available
		return indexOf(av, core.TabDMs) == i-1 && ca.Prefs().TabRailOrderSet
	})
	p.setPref("ia4_compact_mode", true)
	waitFor(t, a, "compact", func() bool { return ca.Rail().Compact })

	// Messages & media > Theme: Compact writes messages_theme dense, Clean
	// light_with_avatars (MessageThemePref), and the list reads it
	p.setPref("messages_theme", "dense")
	waitFor(t, a, "Compact", func() bool { v, _ := fs.Pref("messages_theme"); return v == "dense" && ca.Prefs().Dense })
	snap()
	if !uipref.Get().Dense {
		t.Fatal("uipref doesn't see Compact")
	}
	p.setPref("messages_theme", "light_with_avatars")
	waitFor(t, a, "Clean", func() bool {
		v, _ := fs.Pref("messages_theme")
		return v == "light_with_avatars" && !ca.Prefs().Dense
	})
	snap()

	// Messages & media > Default Skin Tone
	p.setPref("preferred_skin_tone", "3")
	waitFor(t, a, "skin tone", func() bool { v, _ := fs.Pref("preferred_skin_tone"); return v == "3" && ca.Prefs().SkinTone == "3" })

	// Appearance > Custom theme: a typed hex makes a custom-hex ia_theme
	p.setRoleHex(0, "#123ABC")
	waitFor(t, a, "custom primary", func() bool {
		v, _ := fs.Pref("ia_theme")
		vs, _ := v.(string)
		th, ok := theme.ParseIATheme(vs)
		return ok && th.UseCustomHex && th.Primary.Custom == "#123ABC" && th.Highlight1.Custom == "#F9EDFF" &&
			a.accountTheme().Primary.Custom == "#123ABC"
	})
}

// "Set up mobile overrides" shows a mobile level select; turning it off
// puts mobile back on the desktop level.
func TestPrefsMobileOverrides(t *testing.T) {
	a, _, fs := fakePrefsApp(t)
	p := &prefsModal{a: a, clicks: map[string]*widget.Clickable{}}
	has := func(rows []prefsDrawn, key string) bool {
		for _, r := range rows {
			if r.Key == key {
				return true
			}
		}
		return false
	}
	if has(pageRows(t, a, p, "notifications"), "sel.global_mobile") {
		t.Fatal("mobile select shown with the levels in sync")
	}
	p.clicks["mobile_overrides"].Click()
	pageRows(t, a, p, "notifications")
	if !has(pageRows(t, a, p, "notifications"), "sel.global_mobile") {
		t.Fatal("no mobile select after Set up mobile overrides")
	}
	p.setNotify("global_mobile", "everything")
	waitFor(t, a, "mobile everything", func() bool { return fakeNotify(fs, "global_mobile") == "everything" })
	p.clicks["mobile_overrides"].Click()
	pageRows(t, a, p, "notifications")
	waitFor(t, a, "mobile back in sync", func() bool { return fakeNotify(fs, "global_mobile") == "mentions_dms" })
}

// Lists keyboard interactions: Data entry is checked while the pref is
// unset, a pick writes lists_tabbing_navigation_option_type, and the
// section is only there with the lists_tabbing_navigation experiment
// (ListsTabbingPref returns null without it).
func TestPrefsListsKeyboard(t *testing.T) {
	a, ca, fs := fakePrefsApp(t)
	p := &prefsModal{a: a, clicks: map[string]*widget.Clickable{}}
	state := func() (tab, arrow, shown bool) {
		for _, r := range pageRows(t, a, p, "accessibility") {
			switch r.Key {
			case "pref.lists_tab_nav":
				tab, shown = r.On && r.Enabled, true
			case "pref.lists_arrow_nav":
				arrow = r.On && r.Enabled
			}
		}
		return
	}
	if tab, arrow, shown := state(); !shown || !tab || arrow {
		t.Fatalf("unset: data entry %v, full keyboard %v, shown %v", tab, arrow, shown)
	}
	p.setPref("lists_tabbing_navigation_option_type", "arrow_nav")
	waitFor(t, a, "arrow_nav", func() bool {
		v, _ := fs.Pref("lists_tabbing_navigation_option_type")
		return v == "arrow_nav" && ca.PrefString("lists_tabbing_navigation_option_type") == "arrow_nav"
	})
	if tab, arrow, _ := state(); tab || !arrow {
		t.Fatalf("arrow_nav: data entry %v, full keyboard %v", tab, arrow)
	}
	ca.SetExperimentGroup("lists_tabbing_navigation", "off")
	if _, _, shown := state(); shown {
		t.Fatal("the section shows without the experiment")
	}
}
