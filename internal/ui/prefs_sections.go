package ui

import (
	"context"
	"encoding/json"
	"errors"
	"image"
	"math"
	"time"

	"gioui.org/gesture"
	"gioui.org/io/pointer"
	"gioui.org/layout"
	"gioui.org/op"
	"gioui.org/op/clip"

	"github.com/some-du6e/yoloslackgoclient/internal/ui/popups"
	"github.com/some-du6e/yoloslackgoclient/internal/ui/shortcut"
	"github.com/some-du6e/yoloslackgoclient/internal/ui/theme"
)

// Preferences sections Slack has beyond Clack's original five: each row
// either reads and writes the users.prefs key Slack's dialog uses (and
// drives Clack's behaviour where Clack has it), or shows the way Slack
// does, disabled, with naNote. Keys and calls are from Slack's
// gantry-v2-async-prefs-dialog chunk (2026-11).

var errNoCore = errors.New("not signed in")

// ---- writes (the functions the rows call) ----

// setSlackPref writes one users.prefs key through Slack's setUserPrefByApi
// thunk, so plugins see (and may change) it; the native is core.SetPref
// (users.prefs.set name/value). done runs on the UI goroutine.
func (a *App) setSlackPref(name string, value any, done func(error)) {
	a.dispatch("setUserPrefByApi", map[string]any{"pref": name, "value": value},
		func(ctx context.Context, args map[string]any) (any, error) {
			n := argStr(args, "pref")
			if n == "" {
				n = name
			}
			err := errNoCore
			if c := a.Core(); c != nil {
				err = c.SetPref(ctx, n, prefValue(args["value"]))
			}
			a.prefWritten(err, done)
			return nil, err
		})
}

// setSlackPrefs writes several keys in one users.prefs.set (Slack's
// setMultipleUserPrefsByApi: Mark as read's two read-state prefs).
func (a *App) setSlackPrefs(kv map[string]any, done func(error)) {
	go func() {
		ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
		defer cancel()
		err := errNoCore
		if c := a.Core(); c != nil {
			err = c.SetMultiplePrefs(ctx, kv)
		}
		a.prefWritten(err, done)
	}()
}

// setNotifyPref writes a global notification pref
// (users.prefs.setNotifications global=true, like Slack's
// NotificationsGlobalPrefCheckbox).
func (a *App) setNotifyPref(name string, value any, done func(error)) {
	go func() {
		ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
		defer cancel()
		err := errNoCore
		if c := a.Core(); c != nil {
			err = c.SetGlobalNotifyPref(ctx, name, value)
		}
		a.prefWritten(err, done)
	}()
}

// removeVIP takes a user off the VIP list (users.priority.remove).
func (a *App) removeVIP(user string, done func(error)) {
	go func() {
		ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
		defer cancel()
		err := errNoCore
		if c := a.Core(); c != nil {
			err = c.RemoveVIP(ctx, user)
		}
		a.prefWritten(err, done)
	}()
}

func (a *App) prefWritten(err error, done func(error)) {
	a.Post(func() {
		if err != nil {
			a.Toast("Couldn't save that preference: " + err.Error())
		}
		if done != nil {
			done(err)
		}
		a.Invalidate()
	})
}

// prefValue is a thunk's value as core.SetPref takes it (plugins hand
// numbers back as json.Number or float64).
func prefValue(v any) any {
	switch x := v.(type) {
	case json.Number:
		if i, err := x.Int64(); err == nil {
			return int(i)
		}
		return x.String()
	case float64:
		if x == math.Trunc(x) {
			return int(x)
		}
	}
	return v
}

// ---- reads, with the value just written shown until the write lands ----

func (p *prefsModal) pending(k string) (any, bool) {
	v, ok := p.pend[k]
	return v, ok
}

func (p *prefsModal) prefBool(name string, def bool) bool {
	if v, ok := p.pending(name); ok {
		if b, ok := v.(bool); ok {
			return b
		}
	}
	if c := p.a.Core(); c != nil {
		return c.PrefBool(name, def)
	}
	return def
}

func (p *prefsModal) prefStr(name string) string {
	if v, ok := p.pending(name); ok {
		if s, ok := v.(string); ok {
			return s
		}
	}
	if c := p.a.Core(); c != nil {
		return c.PrefString(name)
	}
	return ""
}

func (p *prefsModal) prefInt(name string, def int) int {
	if v, ok := p.pending(name); ok {
		if n, ok := v.(int); ok {
			return n
		}
	}
	if c := p.a.Core(); c != nil {
		return c.PrefInt(name, def)
	}
	return def
}

func (p *prefsModal) notifyBool(name string, def bool) bool {
	if v, ok := p.pending("notify:" + name); ok {
		if b, ok := v.(bool); ok {
			return b
		}
	}
	if c := p.a.Core(); c != nil {
		return c.GlobalNotifyBool(name, def)
	}
	return def
}

func (p *prefsModal) notifyStr(name string) string {
	if v, ok := p.pending("notify:" + name); ok {
		if s, ok := v.(string); ok {
			return s
		}
	}
	if c := p.a.Core(); c != nil {
		return c.GlobalNotifyString(name)
	}
	return ""
}

func (p *prefsModal) hold(k string, v any) func(error) {
	if p.pend == nil {
		p.pend = map[string]any{}
	}
	p.pend[k] = v
	return func(error) { delete(p.pend, k) }
}

// setPref is what a pref row calls (setUserPrefByApi).
func (p *prefsModal) setPref(name string, v any) { p.a.setSlackPref(name, v, p.hold(name, v)) }

// setPrefs writes several prefs at once.
func (p *prefsModal) setPrefs(kv map[string]any) {
	var dones []func(error)
	for k, v := range kv {
		dones = append(dones, p.hold(k, v))
	}
	p.a.setSlackPrefs(kv, func(err error) {
		for _, d := range dones {
			d(err)
		}
	})
}

// setNotify writes a global notification pref.
func (p *prefsModal) setNotify(name string, v any) {
	p.a.setNotifyPref(name, v, p.hold("notify:"+name, v))
}

// ---- scrolling (pages and the nav outgrow the 624px dialog) ----

type prefsScroll struct {
	sc       gesture.Scroll
	y        int
	contentH int
}

// layout draws w (laid out with unbounded height) scrolled and clipped to
// gtx's max size.
func (s *prefsScroll) layout(gtx layout.Context, w layout.Widget) layout.Dimensions {
	vh := gtx.Constraints.Max.Y
	sr := pointer.ScrollRange{Min: -s.y, Max: max(0, s.contentH-vh-s.y)}
	if d := s.sc.Update(gtx.Metric, gtx.Source, gtx.Now, gesture.Vertical, pointer.ScrollRange{}, sr); d != 0 {
		s.y += d
	}
	s.y = min(max(s.y, 0), max(0, s.contentH-vh))
	size := gtx.Constraints.Max
	cl := clip.Rect{Max: size}.Push(gtx.Ops)
	s.sc.Add(gtx.Ops)
	t := op.Offset(image.Pt(0, -s.y)).Push(gtx.Ops)
	g := gtx
	g.Constraints = layout.Constraints{Max: image.Pt(size.X, 1<<20)}
	d := w(g)
	t.Pop()
	cl.Pop()
	s.contentH = d.Size.Y
	return layout.Dimensions{Size: size}
}

// ---- pages ----

// pageFns are Clack's own pages by id (Appearance lives in
// prefs_appearance.go; Availability is popups' page).
var pageFns = map[string]func(p *prefsModal, f *prefsForm){
	"notifications":      (*prefsModal).notificationsPage,
	"vip":                (*prefsModal).vipPage,
	"navigation":         (*prefsModal).navigationPage,
	"sidebar":            (*prefsModal).homePage,
	"messages_media":     (*prefsModal).messagesPage,
	"language_region":    (*prefsModal).languagePage,
	"accessibility":      (*prefsModal).accessibilityPage,
	"mark_as_read":       (*prefsModal).markReadPage,
	"video_audio":        (*prefsModal).audioVideoPage,
	"salesforce":         (*prefsModal).salesforcePage,
	"connected_accounts": (*prefsModal).connectedPage,
	"privacy_visibility": (*prefsModal).privacyPage,
	"advanced":           (*prefsModal).advancedPage,
}

func (p *prefsModal) notificationsPage(f *prefsForm) {
	a := p.a
	f.heading("Messaging defaults")
	levels := []struct{ v, label string }{
		{"everything", "All new messages"},
		{"mentions_dms", "Direct messages, mentions & keywords"},
		{"nothing", "Nothing"},
	}
	sel := func(name string) {
		cur := p.notifyStr(name)
		label := cur
		var items []MenuItem
		for _, l := range levels {
			l := l
			if l.v == cur {
				label = l.label
			}
			items = append(items, MenuItem{Label: l.label, Do: func() { p.setNotify(name, l.v) }})
		}
		title := "Desktop notifications"
		if name == "global_mobile" {
			title = "Mobile notifications"
		}
		f.selectBox(name, title, label, true, items)
	}
	sel("global_desktop")
	sel("global_mobile")
	f.subheading("Also notify you about:")
	on := p.notifyBool("threads_everything", true)
	f.option("notify.threads_everything", "Thread replies", "Replies in threads you follow", on, false, func() { p.setNotify("threads_everything", !on) })
	f.check("priority_dnd_override", "Messages from VIPs, even if notifications are paused", "", false, false)
	f.gap()
	f.heading("Sounds & appearance")
	prev := !p.notifyBool("no_text_in_notifications", false)
	f.option("notify.no_text", "Include a preview of the message in each notification", "Disable this for extra privacy", prev, false,
		func() { p.setNotify("no_text_in_notifications", prev) })
	f.off("mute_sounds", "Mute all messaging sounds from Slack", "", p.prefBool("mute_sounds", false), false)
	f.off("desktop_sound", "Notification sound (messages)", "", false, false)
	f.na()
	f.gap()
	f.heading("On this computer")
	f.option("notify", "Show desktop notifications", "Clack's own switch for notifications on this computer", a.notifyOn(), false, a.toggleNotify)
	f.gap()
	f.heading("Channel keywords")
	f.muted("These act like mentions but can be set for topics that are important to you.", false)
	if kw := p.notifyStr("global_keywords"); kw != "" {
		f.para(kw)
	}
	f.off("keywords", "Edit keywords", "", false, false)
	f.na()
}

func (p *prefsModal) vipPage(f *prefsForm) {
	a := p.a
	f.heading("VIP Notifications")
	f.check("priority_dnd_override", "Always allow notifications from VIPs",
		"Still get message notifications from your VIPs, even when notifications are paused", false, false)
	f.off("priority_sidebar_section", "Create VIP unreads section", "Appears at the top of your sidebar", p.prefBool("priority_sidebar_section", false), false)
	f.na()
	f.gap()
	f.heading("VIP List")
	f.muted("Decide who or what you want prioritized at the top of your Home, DMs and Activity.", false)
	c := a.Core()
	var ids []string
	if c != nil {
		ids = c.VIPs()
	}
	if len(ids) == 0 {
		f.subheading("Added to your VIP list")
		f.muted("No VIPs added yet", false)
	} else {
		f.subheading("Added to your VIP list")
		for _, id := range ids {
			id := id
			name, av := id, ""
			if u, ok := c.User(id); ok {
				name, av = u.Name, u.Avatar
				if u.RealName != "" {
					name = u.RealName
				}
			} else {
				ensureUserAsync(a, id)
			}
			f.listRow("vip."+id, av, name, "Remove", func() { a.removeVIP(id, nil) })
		}
	}
	f.muted("Adding people to your VIP list isn't available in Clack yet: add them from Slack, or from a profile in Slack.", false)
}

// ensureUserAsync loads a user Clack hasn't seen (VIP rows).
func ensureUserAsync(a *App, id string) {
	c := a.Core()
	if c == nil {
		return
	}
	go func() {
		ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
		defer cancel()
		c.EnsureUsers(ctx, []string{id})
		a.Invalidate()
	}()
}

func (p *prefsModal) navigationPage(f *prefsForm) {
	a := p.a
	f.heading("Sidebar")
	f.option("sidebar", "Show the sidebar", shortcut.Format("Mod+Shift+D")+" toggles it", !a.sidebar.hidden, false, func() {
		a.sidebar.hidden = !a.sidebar.hidden
		a.savePrefs()
	})
	f.option("sbreset", "Reset sidebar width", "Back to the default 232px", a.sidebar.width == theme.SidebarW, false, func() {
		a.sidebar.width = theme.SidebarW
		a.savePrefs()
	})
}

func (p *prefsModal) homePage(f *prefsForm) {
	f.heading("Filter conversations by:")
	f.radio("sidebar_behavior", "hide_inactive_channels", "Active only", "New activity within the last 30 days")
	f.radio("sidebar_behavior", "hide_read_channels", "Unreads", "")
	f.radio("sidebar_behavior", "show_mentions_only", "Mentions", "")
	f.radio("sidebar_behavior", "", "All", "")
	f.gap()
	f.heading("Sort conversations by:")
	// core's sidebar rules sort by channel_sort (sectionSort)
	sort := p.prefStr("channel_sort")
	f.option("pref.channel_sort=default", "A–Z", "", sort == "" || sort == "default", true, func() { p.setPref("channel_sort", "default") })
	f.radio("channel_sort", "recent", "Most recent", "")
	f.radio("channel_sort", "priority", "Priority", "")
	f.gap()
	f.heading("Always show in the sidebar:")
	for _, v := range []struct{ k, label string }{
		{"enable_unread_view", "Unreads"}, {"enable_threads_view", "Threads"}, {"enable_huddles_home_view", "Huddles"},
		{"enable_sent_view", "Drafts & sent"}, {"enable_saved_view", "Saved"}, {"enable_unified_directory_view", "Directories"},
	} {
		f.off(v.k, v.label, "", p.prefBool(v.k, false), false)
	}
	f.na()
	f.gap()
	f.heading("Sidebar Organization")
	// core's sidebar rules apply these (internal/core/sidebar_rules.go)
	f.off("show_sidebar_avatars", "Show profile photos next to DMs", "", p.prefBool("show_sidebar_avatars", true), false)
	f.check("separate_private_channels", "Separate private channels from public ones in sidebar", "", false, false)
	f.check("undo_channel_intermix", "Separate direct messages and apps from channels in sidebar", "", false, false)
	f.off("boost_unreads", "Move unread items to top of sections", "", p.prefBool("boost_unreads", false), false)
	f.check("boost_mentions", "Move items with unread mentions to top of sections", "", false, false)
	f.off("online_first_dms_on", "Move online users to the top of the DM section", "", p.prefBool("online_first_dms_on", false), false)
	f.check("show_muted_items_outside_sidebar_menus", "Display muted items outside of sidebar menus", "", false, false)
	f.muted("Profile photos, unreads first and online first: "+naNote, false)
}

func (p *prefsModal) messagesPage(f *prefsForm) {
	f.heading("Theme")
	dense := p.prefStr("messages_theme") == "dense"
	f.off("messages_theme.clean", "Clean", "", !dense, true)
	f.off("messages_theme.dense", "Compact", "", dense, true)
	f.na()
	f.gap()
	f.heading("Names")
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
	f.gap()
	f.heading("Additional options")
	f.check("show_typing", "Display information about who is currently typing a message", "", true, false)
	f.check("time24", "Show times with 24-hour clock", "", false, false)
	f.check("hide_hex_swatch", "Display color swatches next to hexadecimal values", "", false, true)
	f.gap()
	f.heading("Emoji")
	f.off("emoji_mode", "Display emoji as plain text", "", p.prefStr("emoji_mode") == "as_text", false)
	f.check("jumbomoji", "Show JUMBOMOJI", "Display the jumbo versions of emoji in messages without text", true, false)
	f.off("convert_emoticons", "Convert my typed emoticons to emoji, so :D becomes 😃", "", p.prefBool("convert_emoticons", true), false)
	f.off("quick_reactions", "Show one-click reactions on messages", "", p.prefBool("user_customized_quick_reactions_display_feature", true), false)
	f.na()
	f.gap()
	f.heading("Inline media & links")
	f.check("expand_internal_inline_imgs", "Show images and files uploaded to Slack", "", true, false)
	f.check("expand_inline_imgs", "Show images and files from linked websites", "", true, false)
	f.off("obey_inline_img_limit", "Even if they’re larger than 20MB", "", !p.prefBool("obey_inline_img_limit", true), false)
	f.check("expand_non_media_attachments", "Show text previews of linked websites", "", true, false)
}

func (p *prefsModal) languagePage(f *prefsForm) {
	f.heading("Language")
	loc := p.prefStr("locale")
	if loc == "" {
		loc = "en-US"
	}
	f.selectBox("locale", "", localeName(loc), false, nil)
	f.muted("Choose the language you’d like to use with Slack. Clack is in English only, so changing it isn’t available in Clack yet.", false)
	f.gap()
	f.heading("Time zone")
	tz := p.prefStr("tz")
	if tz == "" {
		tz = "Not set"
	}
	f.off("set_tz_automatically", "Set time zone automatically", "", p.prefBool("set_tz_automatically", true), false)
	f.selectBox("tz", "", tz, false, nil)
	f.muted("Slack uses your time zone to send summary and notification emails, for times in your activity feeds, and for reminders.", false)
	f.na()
	f.gap()
	f.heading("Spellcheck")
	f.off("webapp_spellcheck", "Enable spellcheck on your messages", "", p.prefBool("webapp_spellcheck", true), false)
	f.na()
}

// localeName is Slack's language label for a locale code.
func localeName(code string) string {
	if n, ok := map[string]string{
		"en-US": "English (US)", "en-GB": "English (UK)", "en-IN": "English (India)", "de-DE": "Deutsch (Deutschland)",
		"es-ES": "Español (España)", "es-LA": "Español (Latinoamérica)", "fr-FR": "Français (France)", "it-IT": "Italiano",
		"ja-JP": "日本語", "ko-KR": "한국어", "pt-BR": "Português (Brasil)", "zh-CN": "简体中文", "zh-TW": "繁體中文",
	}[code]; ok {
		return n
	}
	return code
}

func (p *prefsModal) accessibilityPage(f *prefsForm) {
	f.heading("Display")
	f.subheading("Simplified layout mode")
	f.muted("All of Slack’s functionality in a compact, more predictable layout designed to be easier to navigate and promote focus.", false)
	f.off("a11y_show_alt_text_reminders", "Remind me to add an image description on image uploads", "", p.prefBool("a11y_show_alt_text_reminders", false), false)
	f.na()
	f.subheading("Other display options")
	f.check("underline_links", "Underline links to websites", "", false, false)
	f.check("a11y_animations", "Allow animations", "Applies to animated GIFs", true, false)
	f.gap()
	f.heading("Keyboard")
	f.subheading("Press ↑ in the empty message field to:")
	f.off("up.list", "Move focus to the message list", "", false, true)
	f.off("up.edit", "Edit your last message", "", true, true)
	f.na()
	f.gap()
	f.heading("Screen reader")
	f.off("a11y_line_length", "Limit message line length", "", p.prefBool("a11y_line_length", false), false)
	f.na()
}

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
	f.check("confirm_clear_all_unreads", "Prompt to confirm", "Shift+Esc marks every channel read", true, false)
}

func (p *prefsModal) audioVideoPage(f *prefsForm) {
	f.heading("When joining a huddle…")
	f.check("huddles_mute_by_default", "Mute my microphone", "", false, false)
	f.check("huddles_set_status", "Set my status to “In a huddle”", "", true, false)
	f.off("huddles_video_off_by_default", "Turn off my video", "", p.prefBool("huddles_video_off_by_default", false), false)
	f.off("huddles_cc_by_default", "Automatically turn on captions when you’re in a huddle", "", p.prefBool("huddles_cc_by_default", false), false)
	f.na()
	f.gap()
	f.heading("When your screen saver starts or computer locks…")
	f.off("huddles_disconnect_on_lock", "Automatically leave huddles", "", p.prefBool("huddles_disconnect_on_lock", true), false)
	f.heading("When alone in a huddle…")
	f.off("huddles_play_music_when_last", "Play music", "", p.prefBool("huddles_play_music_when_last", true), false)
	f.heading("When Slack is in the background:")
	f.off("huddles_show_join_preview", "Show audio and video preview", "", p.prefBool("huddles_show_join_preview", true), false)
	f.off("huddles_global_mute", "Allow keyboard shortcut to mute", "", p.prefBool("huddles_global_mute", false), false)
	f.na()
}

func (p *prefsModal) salesforcePage(f *prefsForm) {
	f.heading("Salesforce orgs")
	f.para("Now you can view, edit and collaborate on your Salesforce data — right in Slack.")
	f.subheading("No Salesforce org")
	f.muted("Once your admin sets up Salesforce org connections, Salesforce orgs that you have access to will appear here. Connecting one needs Salesforce sign-in, which isn’t available in Clack yet.", false)
}

func (p *prefsModal) connectedPage(f *prefsForm) {
	f.heading("Connected accounts")
	f.para("Apps you’ve connected to your Slack account (calendars, Google Drive, GitHub…) appear here in Slack.")
	f.muted("Connecting and disconnecting accounts signs you in to each app with OAuth, which isn’t available in Clack yet. Manage them from Slack.", false)
}

func (p *prefsModal) privacyPage(f *prefsForm) {
	f.heading("Slack Connect discoverability")
	f.muted("Choose who can find you via Slack search. They’ll only be able to see that you’re on Slack - no personal or workspace details will be shared.", false)
	email := ""
	if c := p.a.Core(); c != nil {
		if u, ok := c.User(p.a.model().SelfID); ok {
			email = u.Email
		}
	}
	anyone := "Anyone with your email address"
	if email != "" {
		anyone += " (" + email + ")"
	}
	f.radio("who_can_see_account_by_searching_email", "EVERYONE", anyone, "")
	f.radio("who_can_see_account_by_searching_email", "NO_ONE", "No one", "")
	f.gap()
	f.heading("Contact sharing")
	f.muted("Choose who’s allowed to share your contact info — so they can introduce you to people outside your organization.", false)
	f.radio("who_can_share_contact_card", "EVERYONE", "All your contacts", "Includes your organization and external people you work with in Slack Connect")
	f.radio("who_can_share_contact_card", "TEAM_MEMBERS", "Only people in your organization", "")
	f.radio("who_can_share_contact_card", "NO_ONE", "No one", "")
	f.gap()
	f.heading("Privacy Mode")
	f.muted("Automatically activate Privacy Mode when sharing your screen in a huddle or recording a clip.", false)
	f.off("privacy_mode_auto_detect", "Enable auto-detection", "", p.prefBool("privacy_mode_auto_detect", false), false)
	f.off("privacy_mode_shortcut_enabled", "Enable the keyboard shortcut", "", p.prefBool("privacy_mode_shortcut_enabled", false), false)
	f.na()
	f.gap()
	f.heading("Invitations you’ve blocked")
	f.muted("Blocked Slack Connect invitations and people you’ve hidden are listed in Slack. Managing them isn’t available in Clack yet.", false)
}

func (p *prefsModal) advancedPage(f *prefsForm) {
	a := p.a
	f.heading("Input options")
	f.check("enter_is_special_in_tbt", "When typing code with ```, Enter should not send the message", "With this checked, use Shift+Enter to send", false, false)
	// the composer's own toggle (prefs.go): live markup follows it
	f.option("plaintext", "Format messages with markup", "The text formatting toolbar won’t show in the composer.", a.plainTextOn(), false, a.togglePlainText)
	f.off("copy_messages_as_markdown", "Copy message text as Markdown", "", p.prefBool("copy_messages_as_markdown", false), false)
	f.off("disable_smart_symbols", "Enable smart quotes and symbols", "", !p.prefBool("disable_smart_symbols", false), false)
	f.na()
	f.gap()
	f.heading("Clack")
	f.muted("Plugin settings appear as their own tabs.", false)
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

// confirmMarkAllRead is Shift+Esc: with confirm_clear_all_unreads (Mark
// as read > "Prompt to confirm", on by default) it asks first, like Slack.
func (a *App) confirmMarkAllRead() {
	if a.data == nil {
		return
	}
	if c := a.Core(); c != nil && !c.PrefBool("confirm_clear_all_unreads", true) {
		a.data.markAllRead()
		return
	}
	a.ShowOverlay(popups.Confirm(a, "Mark all messages as read?", "Every channel will be marked read. This can’t be undone.",
		"Mark as read", false, nil, func() { a.data.markAllRead() }))
}
