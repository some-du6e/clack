package ui

import (
	"encoding/json"
	"runtime"
	"strconv"
	"strings"

	"github.com/some-du6e/clack/internal/ui/huddleui"
	"github.com/some-du6e/clack/internal/ui/shortcut"
)

// Slack's Preferences pages, row for row as its desktop client draws them
// (gantry-v2-async-prefs-dialog and the Audio & video module in
// client-boot-shared, build a84662d2d135, checked against the live dialog
// 2026-11-08; see docs/PREFS-AUDIT.md). A row Clack applies, or whose pref
// Slack's servers act on (mobile push, email, privacy), reads and writes
// Slack's key; a row Clack can't honour yet is drawn greyed with naNote.

// ---- Notifications ----

// notifyLevels are NotificationsGlobalPlatformSelect's options
// (n9iP.B0 with globalCopy).
var notifyLevels = []struct{ v, label, desc string }{
	{"everything", "Everything", ""},
	{"mentions_dms", "Mentions and direct messages", "@you, @channel, @here"},
}

func notifyLevelLabel(v string) string {
	for _, l := range notifyLevels {
		if l.v == v {
			return l.label
		}
	}
	if v == "nothing" {
		return "Nothing"
	}
	return "Mentions and direct messages"
}

// pushIdleWaits are NotificationsMobilePushIdleWaitSelect's options
// (push_idle_wait, minutes; -1 even while active).
var pushIdleWaits = []string{"-1", "0", "1", "2", "5", "10", "15", "20", "25", "30"}

func pushIdleLabel(v string) string {
	switch v {
	case "-1":
		return "Immediately, even if I’m active"
	case "", "0":
		return "As soon as I’m inactive"
	case "1":
		return "After I’ve been inactive for 1 minute"
	}
	return "After I’ve been inactive for " + v + " minutes"
}

func (p *prefsModal) notificationsPage(f *prefsForm) {
	a := p.a
	f.headingLink("Messaging defaults", "About notifications", slackHelp("201355156"))
	f.para("Tell us what you’d generally like to be notified about. You can also set specific rules for specific conversations.")
	f.gap()

	desktop := p.notifyStr("global_desktop")
	mobile := p.notifyStr("global_mobile")
	mobileOn := mobile != "nothing"
	ovr := desktop != mobile && mobileOn
	if p.mobileOvr != nil {
		ovr = *p.mobileOvr
	}
	f.subheading("How to notify you")
	f.para("These appear as a banner on your desktop or mobile device.")
	// global_desktop_push_enabled turns Clack's desktop notifications off
	// too (notify.go); a.prefs.Notifications is the old local switch.
	push := p.notifyBool("global_desktop_push_enabled", true) && a.notifyOn()
	f.option("notify.global_desktop_push_enabled", "Desktop notifications", "", push, false, func() {
		if !push && !a.notifyOn() {
			a.toggleNotify()
		}
		p.setNotify("global_desktop_push_enabled", !push)
	})
	f.option("notify.mobile", "Mobile notifications", "", mobileOn, false, func() {
		v := "nothing"
		if !mobileOn {
			v = desktop
			if v == "" || v == "nothing" {
				v = "mentions_dms"
			}
		} else {
			off := false
			p.mobileOvr = &off
		}
		p.setNotify("global_mobile", v)
	})
	f.gap()

	f.subheading("What to notify you about")
	var items []MenuItem
	for _, l := range notifyLevels {
		l := l
		items = append(items, MenuItem{Label: l.label, Hint: l.desc, Do: func() {
			kv := map[string]any{"global_desktop": l.v}
			if mobileOn && !ovr { // syncWithPrefsName: mobile follows
				kv["global_mobile"] = l.v
			}
			p.setNotifies(kv)
		}})
	}
	f.selectBox("global_desktop", "", notifyLevelLabel(desktop), true, items)
	f.gap()

	f.subheading("Also notify you about:")
	f.notifyCheck("threads_everything", "Thread replies", "", true)
	f.check("priority_dnd_override", "Messages from VIPs, even if notifications are paused", "", false, false)
	f.check("huddles_allow_smart_notif", "New huddles", "", true, false)
	inbox := false
	if c := a.Core(); c != nil {
		inbox = c.Experiment("activity_inbox") == "on"
	}
	if !inbox {
		f.check("activity_include_threads_in_badge_count", "Include thread replies in badge counts on Activity", "", false, false)
	}
	f.optionAt("mobile_overrides", "Set up mobile overrides", "", ovr, false, mobileOn, func() {
		v := !ovr
		p.mobileOvr = &v
		if !v { // back in sync: mobile takes the desktop level
			p.setNotify("global_mobile", desktop)
		}
	})
	if ovr && mobileOn {
		var mitems []MenuItem
		for _, l := range notifyLevels {
			l := l
			mitems = append(mitems, MenuItem{Label: l.label, Hint: l.desc, Do: func() { p.setNotify("global_mobile", l.v) }})
		}
		f.selectBox("global_mobile", "", notifyLevelLabel(mobile), true, mitems)
	}
	f.gap()

	if inbox {
		f.subheading("What to show in Activity")
		f.para("You’ll always see mentions, reactions, and thread replies.")
		dms := !p.prefBool("activity_dms_opt_out", false)
		f.option("pref.activity_dms_opt_out", "DMs and group DMs", "", dms, false, func() { p.setPref("activity_dms_opt_out", dms) })
		f.notifyCheck("global_channels_in_activity", "Channels with notifications set to “All new posts”", "", false)
		f.notifyCheck("global_reminders_in_activity", "To-do reminders", "", true)
		f.gap()
	}

	f.subheading("Channel keywords")
	f.para("These act like mentions but can be set for topics that are important to you.")
	f.link("keywords_help", "Learn more.", slackHelp("201355156"))
	p.keywordsField(f)
	f.muted("Use commas to separate each keyword. Keywords are not case sensitive.", false)
	f.hr()

	f.heading("Reminders")
	p.reminderTimePref(f) // prefs_a.go
	f.muted("Reminders you set for a specific day (like “tomorrow”) will be sent at the time you select.", false)
	f.gap()
	f.subheading("When you’re not active on desktop…")
	f.para("Send notifications to my mobile devices:")
	var idle []MenuItem
	for _, v := range pushIdleWaits {
		v := v
		idle = append(idle, MenuItem{Label: pushIdleLabel(v), Do: func() { p.setNotify("push_idle_wait", v) }})
	}
	f.selectBox("push_idle_wait", "", pushIdleLabel(p.notifyStr("push_idle_wait")), true, idle)
	f.check("inactive_user_summary_notification", "If I’m not active for more than 2 weeks, send me a mobile notification, summarizing activity I’ve missed", "", false, false)
	f.para("Set email notifications, including for when you’re not active on desktop, in your account settings")
	f.button("email_notifications", "Set Email Notifications", true, func() { prefsOpenURL(p.a.teamURL("/account/notifications")) })
	f.hr()

	f.headingLink("Sounds & appearance", "Get help", slackHelp("360001559367"))
	prev := !p.notifyBool("no_text_in_notifications", false)
	f.option("notify.no_text", "Include a preview of the message in each notification", "(disable this for extra privacy)", prev, false,
		func() { p.setNotify("no_text_in_notifications", prev) })
	// mute_sounds / mute_huddle_sounds are users.prefs (setUserPrefByApi);
	// desktop_sound is a global notification pref (setNotifications).
	// Picking a sound plays it, like Slack's onSelectionChange.
	muted := p.prefBool("mute_sounds", false)
	f.option("pref.mute_sounds", "Mute all messaging sounds from Slack", "", muted, false, func() { p.setPref("mute_sounds", !muted) })
	if !muted {
		f.selectBox("desktop_sound", "Notification sound (messages)", soundName(p.notifyStr("desktop_sound"), "Knock Brush"), true,
			soundItems(huddleui.MessageSounds, func(v string) { p.setNotify("desktop_sound", v) }))
	}
	hmuted := p.prefBool("mute_huddle_sounds", false)
	f.option("pref.mute_huddle_sounds", "Mute all huddle sounds from Slack", "", hmuted, false, func() { p.setPref("mute_huddle_sounds", !hmuted) })
	if !hmuted {
		f.selectBox("huddle_invite_sound_v2", "Notification sound (huddles)", soundName(p.prefStr("huddle_invite_sound_v2"), "Boop Plus"), true,
			soundItems(huddleui.HuddleSounds, func(v string) { p.setPref("huddle_invite_sound_v2", v) }))
	}
	if runtime.GOOS == "darwin" { // MacAppearancePref
		p.macAppearance(f)
	}
}

// soundItems is a sound select's menu: picking one previews it and saves.
func soundItems(ss []huddleui.Sound, set func(string)) []MenuItem {
	var items []MenuItem
	for _, s := range ss {
		s := s
		items = append(items, MenuItem{Label: s.Label, Do: func() {
			huddleui.PlaySlackSound(s.Value)
			set(s.Value)
		}})
	}
	return items
}

// soundName is Slack's label for a notification sound file (def: none set).
func soundName(v, def string) string {
	if l := huddleui.SoundLabel(v); l != "" {
		return l
	}
	v = strings.TrimSuffix(v, ".mp3")
	switch v {
	case "":
		return def
	case "none":
		return "None"
	}
	w := strings.Fields(strings.ReplaceAll(v, "_", " "))
	for i := range w {
		w[i] = strings.ToUpper(w[i][:1]) + w[i][1:]
	}
	return strings.Join(w, " ")
}

// teamURL is a page on the workspace's own host (the browser opens it).
func (a *App) teamURL(path string) string {
	if c := a.Core(); c != nil {
		if u := c.TeamURL(); u != "" {
			return strings.TrimSuffix(u, "/") + path
		}
	}
	return "https://app.slack.com" + path
}

// ---- VIP ----

func (p *prefsModal) vipPage(f *prefsForm) {
	f.subheading("VIP Notifications")
	f.check("priority_dnd_override", "Always allow notifications from VIPs",
		"Still get message notifications from your VIPs, even when general notifications are paused", false, false)
	// isVipSectionPrefEnabled: the row shows under vip_section_pref; core
	// fills the VIP unreads section (core/prefs_a.go priorityChannels)
	if c := p.a.Core(); c != nil && c.Experiment("vip_section_pref") == "on" {
		f.check("priority_sidebar_section", "Create VIP unreads section", "This section will appear at the top of your sidebar so you never miss a mention or message.", false, false)
	}
	f.gap()
	f.subheading("VIP List")
	f.para("Decide who or what you want prioritized at the top of your Home, DMs and Activity.")
	p.vipList(f) // prefs_a.go: search, Added to your VIP list, Recommended
}

// ---- Navigation (Slack's tab_rail) ----

func (p *prefsModal) navigationPage(f *prefsForm) {
	a := p.a
	c := a.Core()
	f.subheading("Configure these tabs in the navigation bar:")
	f.muted("Select the tabs you want to show, and drag to reorder them. At smaller window sizes, not all selected tabs may appear.", false)
	if c != nil {
		f.reorderList(p.navTabs())
	}
	f.gap()
	f.subheading("Navigation Tab Appearance")
	compact := p.prefBool("ia4_compact_mode", false)
	f.option("pref.ia4_compact_mode=off", "Icons & text", "", !compact, true, func() { p.setPref("ia4_compact_mode", false) })
	f.option("pref.ia4_compact_mode=on", "Icons only", "Makes navigation tabs smaller, and automatically turns off numerical badges.", compact, true,
		func() { p.setPref("ia4_compact_mode", true) })
	f.gap()
	f.subheading("App agents & assistants")
	f.off("show_ai_apps", "Show AI Agents in top bar", "", p.prefBool("show_ai_apps", true), false)
	f.muted("Show AI Agents in top bar: "+naNote, false)
	if c != nil && c.Experiment("agents_in_dm_sidebar") == "on" && c.Experiment("agents_in_dm_sidebar_pref") == "on" { // Mq && n7
		// off: agent DMs go back under Apps (core.AgentInDMSidebar)
		f.check("hide_agents_in_dm_list", "Show Agents in direct messages", "", false, true)
	}
}

// setRailTab is setTabRailCustomizationByTabName: tab_rail_customization
// is the merged map with tab set, tab_rail_customization_reasons records
// "manual".
func (p *prefsModal) setRailTab(tab string, on bool) {
	c := p.a.Core()
	if c == nil {
		return
	}
	m := c.RailCustomization()
	m[tab] = on
	reasons := map[string]any{}
	_ = json.Unmarshal([]byte(c.PrefString("tab_rail_customization_reasons")), &reasons)
	reasons[tab] = "manual"
	mj, _ := json.Marshal(m)
	rj, _ := json.Marshal(reasons)
	done := p.hold("rail:"+tab, on)
	p.a.setSlackPrefs(map[string]any{"tab_rail_customization": string(mj), "tab_rail_customization_reasons": string(rj)}, done)
}

// ---- Home (Slack's "sidebar" tab) ----

// sidebarViews are SyntheticViewPagePreferences' rows in Slack's order;
// Clack's sidebar hides a link whose pref is off (sidebar.go).
var sidebarViews = []struct{ pref, label string }{
	{"enable_unread_view", "Unreads"}, {"enable_huddles_home_view", "Huddles"}, {"enable_threads_view", "Threads"},
	{"enable_saved_view", "Saved"}, {"enable_sent_view", "Drafts & sent"}, {"enable_unified_directory_view", "Directories"},
}

func (p *prefsModal) homePage(f *prefsForm) {
	f.subheading("Channel organization")
	f.off("disable_channel_join_banner", "Show suggestions for getting started in new channels", "", !p.prefBool("disable_channel_join_banner", false), false)
	f.na()
	// Slack's servers file a joined channel into a section (the client
	// only reads the placement back): the box is the pref's negation.
	f.check("disable_auto_channel_sort", "Sort channels into sidebar sections automatically", "", false, true)
	f.hr()
	f.subheading("Show activity")
	// HomeUnreadDotPreferences: the input is named enable_home_unread_dot
	// but the pref is home_unread_dot; off, the Home tab shows no dot or
	// badge (host.go railBadge)
	f.check("home_unread_dot", "Show a dot on the Home icon when there is unread activity", "", true, false)
	f.link("home_dot_help", "Learn more", slackHelp("212596808"))
	f.gap()
	f.subheading("Always show in the sidebar:")
	for _, v := range sidebarViews {
		if v.pref == "enable_unread_view" { // Clack has no Unreads view
			f.off(v.pref, v.label, "", p.prefBool(v.pref, false), false)
			continue
		}
		f.check(v.pref, v.label, "", true, false)
	}
	f.muted("Unreads: "+naNote, false)
	f.hr()

	custom := func(key string) int {
		n := 0
		if c := p.a.Core(); c != nil {
			for _, sp := range c.Prefs().SectionPrefs {
				if sp[key] != "" {
					n++
				}
			}
		}
		return n
	}
	f.subheading("Filter conversations by:")
	f.radio("sidebar_behavior", "hide_inactive_channels", "Active only", "New activity within the last 30 days")
	f.radio("sidebar_behavior", "hide_read_channels", "Unreads", "")
	f.radio("sidebar_behavior", "show_mentions_only", "Mentions", "")
	f.radio("sidebar_behavior", "", "All", "")
	if n := custom("sidebar"); n > 0 {
		f.muted("You have a custom filter for "+plural(n, "1 section", "sections")+" in your channel list", false)
	}
	f.hr()
	f.subheading("Sort conversations by:")
	// core's sidebar rules sort by channel_sort (sectionSort)
	sort := p.prefStr("channel_sort")
	f.option("pref.channel_sort=default", "A–Z", "", sort == "" || sort == "default", true, func() { p.setPref("channel_sort", "default") })
	f.radio("channel_sort", "recent", "Most recent", "")
	f.radio("channel_sort", "priority", "Priority", "")
	if n := custom("sort"); n > 0 {
		f.muted("You have a custom sort setting for "+plural(n, "1 section", "sections")+" in your sidebar.", false)
	}
	f.hr()
	f.subheading("Sidebar Organization")
	// core's sidebar rules apply these (internal/core/sidebar_rules.go)
	// SidebarAvatarsPref reads !!show_sidebar_avatars; off, DM rows show a
	// presence badge (slackc_shell.go)
	f.check("show_sidebar_avatars", "Show profile photos next to DMs", "", false, false)
	f.check("separate_private_channels", "Separate private channels from public ones in sidebar", "", false, false)
	f.check("undo_channel_intermix", "Separate direct messages and apps from channels in sidebar", "", false, false)
	f.check("boost_mentions", "Move items with unread Mentions to top of sections", "", false, false)
	// isSlackConnectSectionVisible: core files externally shared
	// conversations into the slack_connect section (core/app.go sections)
	f.check("should_show_connect_section", "Organize external conversations into the External Connections section", "", false, false)
	// improve_mute_pref: the box is the pref's negation
	f.check("show_muted_items_outside_sidebar_menus", "Hide muted channels from your sidebar", "", false, true)
	f.muted("You’ll always be able to find them in the channel section menu and they’ll pop up again if someone mentions you.", true)
}

// ---- Messages & media ----

// skinTones are EmojiSkinTonePicker's choices (preferred_skin_tone).
var skinTones = []string{"No skin tone", "light skin tone", "medium-light skin tone", "medium skin tone", "medium-dark skin tone", "dark skin tone"}

func (p *prefsModal) messagesPage(f *prefsForm) {
	f.subheading("Theme")
	mt := p.prefStr("messages_theme")
	f.off("messages_theme.clean", "Clean", "", mt != "dense", true)
	f.off("messages_theme.dense", "Compact", "", mt == "dense", true)
	f.na()
	f.hr()
	f.subheading("Names")
	// MessageNamesPref: display_real_names_override is 1/-1 against the
	// team's display_real_names default, 0 when it matches.
	team := false
	if c := p.a.Core(); c != nil {
		team = c.PrefBool("display_real_names", false)
	}
	ov := p.prefInt("display_real_names_override", 0)
	full := ov > 0 || (ov == 0 && team)
	setNames := func(full bool) {
		v := 0
		switch {
		case team && !full:
			v = -1
		case !team && full:
			v = 1
		}
		p.setPref("display_real_names_override", v)
	}
	f.option("names.real", "Full & display names", "", full, true, func() { setNames(true) })
	f.option("names.display", "Just display names", "", !full, true, func() { setNames(false) })
	f.para("To change your full or display name, head to your profile.")
	f.linkDo("own_profile", "your profile", func() { p.a.OpenProfile(p.a.PointerPos(), p.a.model().SelfID) })
	f.hr()
	f.subheading("Additional options")
	f.check("show_typing", "Display information about who is currently typing a message", "", true, false)
	f.check("time24", "Show times with 24-hour clock", "", false, false)
	f.check("hide_hex_swatch", "Display color swatches next to hexadecimal values", "", false, true)
	f.hr()
	f.subheading("Emoji")
	f.subheading("Default Skin Tone")
	f.para("Choose the default skin tone that will be used whenever you use certain emojis in reactions and messages.")
	p.skinTonePicker(f) // prefs_a.go: Slack's ✋ radios
	// EmojiPref: emoji_mode as_text / default; turning text on also turns
	// convert_emoticons off (0), and both JUMBOMOJI and emoticons grey
	// while it's on (JUMBOMOJI also under Compact)
	asText := p.prefStr("emoji_mode") == "as_text"
	emoticons := p.prefBool("convert_emoticons", true)
	f.option("pref.emoji_mode", "Display emoji as plain text", "", asText, false, func() {
		if asText {
			p.setPref("emoji_mode", "default")
			return
		}
		kv := map[string]any{"emoji_mode": "as_text"}
		if emoticons {
			kv["convert_emoticons"] = 0
		}
		p.setPrefs(kv)
	})
	jumbo := p.prefBool("jumbomoji", true)
	f.optionAt("pref.jumbomoji", "Show JUMBOMOJI", "Display the jumbo versions of emoji (up to 23 at a time!) in messages without text.", jumbo, false,
		!asText && p.prefStr("messages_theme") != "dense", func() { p.setPref("jumbomoji", !jumbo) })
	f.optionAt("pref.convert_emoticons", "Convert my typed emoticons to emoji, so :D becomes 😃", "", emoticons, false, !asText,
		func() { p.setPref("convert_emoticons", !emoticons) }) // the composer converts them (composer/emoticon.go)
	p.quickReactionsPref(f) // prefs_a.go
	f.hr()
	f.subheading("Inline media & links")
	f.check("expand_internal_inline_imgs", "Show images and files uploaded to Slack", "", true, false)
	f.check("expand_inline_imgs", "Show images and files from linked websites", "", true, false)
	// InlineMediaPref: the box is !obey_inline_img_limit, greyed without
	// expand_inline_imgs (richview imageTooLarge)
	obey := p.prefBool("obey_inline_img_limit", true)
	f.optionAt("pref.obey_inline_img_limit", "Even if they’re larger than 20MB", "", !obey, false, p.prefBool("expand_inline_imgs", true),
		func() { p.setPref("obey_inline_img_limit", !obey) })
	f.check("expand_non_media_attachments", "Show text previews of linked websites", "", true, false)
	f.hr()
	f.subheading("Bring emails into Slack")
	f.para("You can upload any important email to Slack by forwarding it to a unique email address. Slack will deliver the email, and you can keep it private or share it with your team.")
	p.emailForwarding(f) // prefs_a.go
}

// ---- Language & region ----

func (p *prefsModal) languagePage(f *prefsForm) {
	loc := p.prefStr("locale")
	if loc == "" {
		loc = "en-US"
	}
	f.selectBox("locale", "Language", localeName(loc), false, nil)
	f.muted("Choose the language you’d like to use with Slack.", false)
	f.hr()
	f.subheading("Time zone")
	p.timezonePref(f) // prefs_a.go
	f.muted("Slack uses your time zone to send summary and notification emails, for times in your activity feeds, and for reminders.", false)
	f.hr()
	f.selectBox("keyboard_layout", "Keyboard layout", "English (US)", false, nil)
	f.hr()
	f.subheading("Spellcheck")
	f.off("webapp_spellcheck", "Enable spellcheck on your messages and canvases", "", p.prefBool("webapp_spellcheck", true), false)
	f.na()
}

// ---- Accessibility ----

func (p *prefsModal) accessibilityPage(f *prefsForm) {
	f.subheading("Display")
	f.subheading("Simplified layout mode")
	f.para("All of Slack’s functionality in a compact, more predictable layout designed to be easier to navigate and promote focus.")
	f.button("simplified_layout", "Turn On", false, nil)
	f.subheading("Alt text reminders")
	f.off("a11y_show_alt_text_reminders", "Remind me to add an image description on image uploads", "", p.prefBool("a11y_show_alt_text_reminders", false), false)
	f.subheading("Other display options")
	f.off("a11y_peeks_enabled_on_mouse_enter", "Show previews when hovering over tabs", "", p.prefBool("a11y_peeks_enabled_on_mouse_enter", true), false)
	f.check("underline_links", "Underline links to websites", "", false, false)
	f.check("a11y_animations", "Allow animations", "Applies to emoji animation, GIFs, and reactions & effects in huddles and across Slack", true, false)
	f.muted("Simplified layout, alt text reminders and tab previews: "+naNote, false)
	f.hr()
	f.subheading("Keyboard")
	f.para("You can improve the way you use Slack by learning our handy keyboard shortcuts.")
	f.linkDo("shortcuts", "handy keyboard shortcuts", p.a.showShortcutsHelp)
	f.subheading("Press ↑ in the empty message field to:")
	// Slack keeps this client-side (localStorage composerUpArrowShortcutBehavior);
	// Clack keeps it in ui.json. The composer reads it (composer/editor_keys.go upArrow).
	up := p.a.prefs.ComposerUpArrow
	for _, o := range []struct {
		v           int
		label, hint string
	}{
		{1, "Move focus to the message list", "The most recent message in the list will be selected"},
		{2, "Move focus to the message list, only if the message field is empty", "The most recent message in the list will be selected"},
		{3, "Edit your last message", "The last message you sent will be selected and in editing mode"},
	} {
		v := o.v
		f.option("up."+strconv.Itoa(v), o.label, o.hint, up == v, true, func() { p.a.setComposerUpArrow(v) })
	}
	f.muted("Note: You can always press "+shortcut.Format("Mod+Up")+" to edit your last message.", false)
	f.subheading("Lists keyboard interactions")
	f.para("Choose how you want to use your keyboard with lists.")
	lk := p.prefStr("lists_tabbing_navigation_option_type")
	f.off("lists.tab_nav", "Data entry mode (default)", "Use Tab to move horizontally, Enter to move vertically between cells", lk != "arrow_nav", true)
	f.off("lists.arrow_nav", "Full keyboard mode (best for assistive technology)", "Use Tab to move between interactive elements, Enter to edit", lk == "arrow_nav", true)
	f.na()
	f.hr()
	f.subheading("Screen reader")
	f.para("Customize your screen reader experience")
	f.subheading("Message verbosity")
	f.para("Choose which parts of a message to include and the order in which they are announced.")
	f.button("verbosity_reset", "Reset to Default", false, nil)
	f.subheading("Screen reader announcements")
	f.off("a11y_dm_read_out", "Automatically read new messages in the current conversation", "", p.prefBool("a11y_dm_read_out", true), false)
	f.off("a11y_huddles_reactions_read_out_loud", "Read emoji reactions received in a huddle", "", p.prefBool("huddles_reactions_read_out_loud", false), false)
	f.off("a11y_huddles_reactions_play_sound", "Play a sound when an emoji is sent or received in a huddle", "", p.prefBool("huddles_reactions_play_sound", false), false)
	f.na()
	f.para("To change notification sounds, go to Notifications")
	f.linkDo("to_notifications", "Notifications", func() { p.selectTab("notifications") })
	f.hr()
	f.para("Looking to change the font of Slack? Check out Font under Appearance preferences.")
	f.linkDo("to_font", "Font", func() { p.selectTab("appearance") })
}

// ---- Mark as read ----

func (p *prefsModal) markReadPage(f *prefsForm) {
	f.para("Slack marks a channel read as soon as you view it. You can change this if you’d like.")
	f.subheading("When I view a channel:")
	imm, old := p.prefBool("mark_msgs_read_immediately", false), p.prefBool("start_scroll_at_oldest", false)
	state := "default"
	switch {
	case imm && old:
		state = "immediate_scroll"
	case imm:
		state = "immediate"
	}
	for _, o := range []struct{ v, label string }{
		{"immediate_scroll", "Start me where I left off, and mark the channel read"},
		{"immediate", "Start me at the newest message, and mark the channel read"},
		{"default", "Start me at the newest message, but leave unseen messages unread"},
	} {
		v := o.v
		f.option("read_state."+v, o.label, "", state == v, true, func() {
			p.setPrefs(map[string]any{"mark_msgs_read_immediately": v != "default", "start_scroll_at_oldest": v == "immediate_scroll"})
		})
	}
	f.gap()
	f.subheading("When I mark everything as read:")
	f.check("confirm_clear_all_unreads", "Prompt to confirm", "", true, false)
	f.hr()
	f.subheading("Keyboard shortcuts")
	f.para("Mark all messages in current channel as read   " + shortcut.Format("Escape"))
	f.para("Mark all messages as read   " + shortcut.Format("Shift+Escape"))
	f.para("Mark a message as unread   " + shortcut.Format("Alt") + " and click message")
	f.para("And that’s just the beginning. To view the full list of keyboard shortcuts, just press " + shortcut.Format("Mod+/"))
}

// ---- Audio & video ----

func (p *prefsModal) audioVideoPage(f *prefsForm) {
	f.selectBox("camera", "Camera", "Default", false, nil)
	f.selectBox("microphone", "Microphone", "Default", false, nil)
	f.off("agc", "Enable automatic gain control", "", true, false)
	f.off("ns", "Enable noise suppression", "", true, false)
	f.na()
	f.hr()
	f.subheading("When audio or video is playing…")
	f.off("clips_pip_auto_dock", "Automatically dock to corner upon scroll or when navigating away", "", p.prefBool("clips_pip_auto_dock", true), false)
	f.hr()
	// Set my status "In a huddle" is on Availability (availability_prefs_section)
	f.subheading("When joining a huddle…")
	f.check("huddles_mute_by_default", "Mute my microphone", "", false, false)
	// a joined huddle starts with captions shown (huddleui options → huddle.Options.Captions)
	f.check("huddles_cc_by_default", "Automatically turn on captions when you’re in a huddle", "", false, false)
	// "Start a huddle in this channel?" over 150 members (huddleui.Controller.shoutyRooster)
	f.check("huddles_show_shouty_rooster", "Send a warning if you’re starting a huddle in a channel with more than 150 members", "", true, false)
	f.off("video_background_filter", "Blur your video background when you’re in a huddle with your camera on", "", false, false)
	f.off("huddles_show_join_preview", "Show audio and video preview", "", p.prefBool("huddles_show_join_preview", true), false)
	f.na()
	f.hr()
	f.subheading("When Slack is in the background:")
	f.off("huddles_global_mute", "Allow keyboard shortcut to mute", "", p.prefBool("huddles_global_mute", false), false)
	f.subheading("When your screen saver starts or computer locks…")
	f.off("huddles_disconnect_on_lock", "Automatically leave huddles", "", p.prefBool("huddles_disconnect_on_lock", true), false)
	f.subheading("Huddle motion")
	f.off("huddles_reactions_effects", "Allow for reactions & effects", "", true, false)
	f.subheading("When alone in a huddle…")
	f.off("huddles_play_music_when_last", "Play music", "", p.prefBool("huddles_play_music_when_last", true), false)
	f.selectBox("huddles_music_delay", "", "After 1 minute", false, nil)
	f.na()
}

// ---- Salesforce ----

func (p *prefsModal) salesforcePage(f *prefsForm) {
	f.heading("Salesforce orgs")
	f.muted("Contact your admin to add or remove Salesforce orgs.", false)
	f.gap()
	f.heading("No Salesforce org")
	f.para("Once your admin set up Salesforce org connections, Salesforce orgs that you have access to will appear here.")
}

// ---- Connected accounts ----

func (p *prefsModal) connectedPage(f *prefsForm) {
	f.heading("Connected accounts")
	f.muted("You can connect your Slack account with other apps and services for faster sign-in, rich previews of links and more.", false)
	f.link("connected_help", "Learn more", slackHelp("218891278-Connect-to-other-services-using-your-Slack-account"))
	f.gap()
	f.subheading("Apps connected through workflows")
	f.muted("Accounts from third-parties that you setup while creating and using workflows, functions, and apps", false)
	p.connectedList(f) // apps.auth.external.userAuths.get (prefs_lists.go)
}

// ---- Privacy & visibility ----

func (p *prefsModal) privacyPage(f *prefsForm) {
	a := p.a
	team := a.model().TeamName
	if team == "" {
		team = "your organization"
	}
	f.subheading("Slack Connect discoverability")
	f.para("Choose who can find you via Slack search. They’ll only be able to see that you’re on Slack - no personal or workspace details will be shared.")
	f.link("discoverability_help", "Learn more", slackHelp("5534401382675"))
	email := ""
	if c := a.Core(); c != nil {
		if u, ok := c.User(a.model().SelfID); ok {
			email = u.Email
		}
	}
	anyone := "Anyone with your email address"
	if email != "" {
		anyone += " (" + email + ")"
	}
	f.radio("who_can_see_account_by_searching_email", "EVERYONE", anyone, "")
	f.radio("who_can_see_account_by_searching_email", "NO_ONE", "No one", "")
	f.subheading("Preferences only apply to this workspace")
	f.muted("To make sure no one can search for you, you’ll need to change this preference across all your workspaces.", false)
	f.button("find_workspaces", "Find my workspaces", true, func() { prefsOpenURL(findWorkspacesURL) })
	f.hr()
	f.subheading("Contact sharing")
	f.para("Choose who’s allowed to share your contact info — so they can introduce you to people outside " + team + ".")
	f.link("contact_help", "Learn more", slackHelp("4406144704659"))
	f.radio("who_can_share_contact_card", "EVERYONE", "All your contacts", "Includes people from "+team+" and any external people you’re using Slack Connect to work with.")
	f.radio("who_can_share_contact_card", "TEAM_MEMBERS", "Only people at "+team, "")
	f.radio("who_can_share_contact_card", "NO_ONE", "No one", "")
	f.hr()
	f.subheading("Invitations you’ve blocked")
	f.para("You’ve stopped receiving Slack Connect invitations from these people.")
	p.blockedSection(f)
	f.hr()
	f.subheading("People you’ve hidden")
	f.para("You won’t get notifications or see messages from these people.")
	f.link("hidden_help", "Learn more", slackHelp("16905395872019"))
	p.hiddenSection(f)
}

// ---- Advanced ----

func (p *prefsModal) advancedPage(f *prefsForm) {
	a := p.a
	f.subheading("Input options")
	if c := a.Core(); c != nil && c.Experiment("composer_smart_symbols") == "on" { // iZ.w gates the row
		// the composer's TextSubstitutions (composer/smart_symbols.go)
		f.check("disable_smart_symbols", "Enable smart quotes and symbols", "Type quotation marks \" \" to create curly ones “ ”. Type -- to create an em-dash —. Type ... to create an ellipsis …. Type - and >, or < and - to create arrows → ←", false, true)
	}
	f.check("enter_is_special_in_tbt", "When typing code with ```, Enter should not send the message.", "With this checked use Shift+Enter to send.", false, false)
	// the composer's own toggle (prefs.go): live markup follows it
	f.option("plaintext", "Format messages with markup", "The text formatting toolbar won’t show in the composer.", a.plainTextOn(), false, a.togglePlainText)
	f.off("disable_link_truncation", "Shorten external links", "When pasting links to places outside Slack, URLs will be shortened.", !p.prefBool("disable_link_truncation", false), false)
	f.muted("Shortened links: "+naNote, false)
	f.subheading("When writing a message, press Enter to…")
	// msg_input_send_btn: the composer sends on Mod+Enter (editor_keys.go)
	f.radio2("msg_input_send_btn", false, "Send the message", "")
	f.radio2("msg_input_send_btn", true, "Start a new line (use "+shortcut.Format("Mod+Enter")+" to send)", "")
	f.hr()
	f.subheading("Search options")
	// search_exclude_channels: the In suggestions drop them (core/search.go)
	f.channelPicker("Exclude these channels from search results:")
	f.subheading("Sort option default:")
	// stick_user_sort: a new search starts from search_sort (popups/search_view.go)
	sticky := p.prefBool("stick_user_sort", false)
	f.option("pref.stick_user_sort=false", "Most relevant", "", !sticky, true, func() { p.setPref("stick_user_sort", false) })
	f.option("pref.stick_user_sort=true", "Last used:", "Each new search defaults you to the option you last used", sticky, true, func() { p.setPref("stick_user_sort", true) })
	f.hr()
	f.subheading("Download location")
	f.off("download_ask", "Choose location for each download", "Note: this setting is for this computer only and affects all workspaces you are signed in to.", false, false)
	f.na()
	f.subheading("Confirmations and warnings")
	// Mod+Z in the empty composer unsends (unsend.go, popups.ConfirmUnsend)
	f.check("should_show_unsend_message_confirmation", "Show me a confirmation when I use "+shortcut.Format("Mod+Z")+" to unsend a message I just sent", "", true, false)
	// the "Set yourself to active?" prompt after connecting (backend.maybePresencePrompt)
	f.check("confirm_user_marked_away", "Ask if I want to toggle my away status when I log in after having set myself away", "", false, false)
	// "Double-check this link" on a suspicious link (popups.MaybeWarnLink)
	f.check("suppress_link_warning", "Warn me about potentially malicious links", "", false, true)
	f.off("hide_sc_files_sharing_speed_bump", "Warn me when sharing files with external organizations", "", !p.prefBool("hide_sc_files_sharing_speed_bump", false), false)
	f.off("hide_external_members_canvas_sharing_speed_bump", "Warn me when sharing canvases with external organizations", "", !p.prefBool("hide_external_members_canvas_sharing_speed_bump", false), false)
	f.na()
	f.subheading("Other options")
	f.check("overloaded_message_enabled", "Send me occasional channel suggestions", "", true, false)
	f.check("in_prod_surveys_enabled", "Send me occasional surveys", "We’re always working to make Slack better, and we’d love your thoughts.", true, false)
	if runtime.GOOS != "darwin" { // DesktopPref: Windows and Linux
		f.off("launchOnStartup", "Launch app on login", "", false, false)
		f.off("runFromTray", "Leave app running in notification area when the window is closed", "", false, false)
		f.na()
	}
	f.hr()
	f.heading("Clack")
	f.subheading("Live updates")
	rtHint := "Opens Slack's realtime connection. Off: messages refresh every 20 s"
	if a.opts.NoRealtime {
		rtHint = "Turned off with -no-realtime or CLACK_NO_REALTIME"
	}
	f.option("realtime", "Live updates (typing, presence, instant messages)", rtHint, a.realtimeOn(), false, a.toggleRealtime)
	f.note("Clack shares your browser's Slack session. On some Enterprise Grid workspaces Slack can sign that session out everywhere if it notices.", true, 2)
	p.updatesSection(f)         // update_check.go
	p.troubleshootingSection(f) // diagnostics.go
}
