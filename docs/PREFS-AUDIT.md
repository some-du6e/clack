# Preferences audit (2026-11-08)

Every Slack Preferences page in Clack, compared against:

- **Slack's live desktop dialog**: the Hack Club workspace in a signed-in
  browser. It was read-only: tabs were clicked, nothing was toggled or typed.
  The DOM was dumped per tab (labels, input names and values, checked state).
- **Slack's web client source**: `gantry-v2-async-prefs-dialog` (build
  a84662d2d135) for the desktop-only branches (`isDesktop`, macOS and
  Windows/Linux rows), the Audio & video module in
  `gantry-v2-async-client-boot-shared`, and `modern.vendor` KlX6 for the
  theme shade tables.

Plugins / Themes / Plugin settings and Advanced > Updates / Troubleshooting
were out of scope and left as they were.

**Legend:** *applied* means Clack acts on the pref. *Server* means Slack's
servers act on it (mobile push, email, privacy), so writing it works for
real. *Greyed* means the row is drawn the way Slack draws it, disabled, with
"Not available in Clack yet".

| Page | Result | Prefs (users.prefs / all_notifications_prefs.global) |
| --- | --- | --- |
| Availability | matched (no change). Still different: "VIPs" in the Working hours paragraph is plain text, not a link to the VIP tab | dnd_* / working hours (applied, popups) |
| Notifications | **fixed**. Slack's order: Messaging defaults + About notifications · How to notify you (Desktop / Mobile) · What to notify you about (Everything / Mentions and direct messages, mobile follows unless overridden) · Also notify you about (Thread replies, VIPs, New huddles, Set up mobile overrides → mobile select) · What to show in Activity (activity_inbox only) · Channel keywords · Reminders · When you're not active on desktop · Sounds & appearance (macOS dock badge / bounce). Removed Clack's invented "On this computer" switch | **newly applied:** global_desktop_push_enabled (off: no desktop notifications). Applied: global_desktop, no_text_in_notifications. Server: global_mobile, push_idle_wait, threads_everything, priority_dnd_override, huddles_allow_smart_notif, inactive_user_summary_notification, activity_dms_opt_out, global_channels_in_activity, global_reminders_in_activity. Greyed: global_keywords editing, reminder time, desktop_sound, huddle_invite_sound_v2, mute_sounds, mute_huddle_sounds, mac_ssb_bullet, mac_ssb_bounce |
| VIP | **fixed** labels, subtexts and order; search box stand-in. Still different: no search/Add, no Recommended list | priority_dnd_override (server), vip_users + users.priority.remove (applied), priority_sidebar_section (greyed) |
| Navigation | **fixed**. Replaced Clack's invented "Show the sidebar" / "Reset sidebar width" with Slack's tab checkboxes, Move up / Move down, Icons & text / Icons only, and App agents & assistants. Still different: no drag reordering; Move up/down act on the tab last toggled | **newly written** (the rail already applied them): tab_rail_customization + tab_rail_customization_reasons (setTabRailCustomizationByTabName), tab_rail_customization_order, ia4_compact_mode. Greyed: show_ai_apps, hide_agents_in_dm_list |
| Home | **fixed**. Added Channel organization and Show activity; views in Slack's order (Unreads, Huddles, Threads, Saved, Drafts & sent, Directories) with Slack's defaults (on); custom filter/sort hints; External Connections row; "Hide muted channels from your sidebar" (improve_mute_pref: the box is the pref's negation) with its helper text | **newly applied:** enable_threads_view, enable_huddles_home_view, enable_sent_view, enable_saved_view, enable_unified_directory_view hide the sidebar links. Applied: sidebar_behavior, channel_sort, separate_private_channels, undo_channel_intermix, boost_mentions, show_muted_items_outside_sidebar_menus. Greyed: enable_unread_view, show_sidebar_avatars, should_show_connect_section, disable_channel_join_banner, disable_auto_channel_sort, enable_home_unread_dot |
| Appearance | **fixed**. Font row; Color Mode (Light / Dark / System, desktop) plus the focus-mode dark row; **Slack themes \| Custom theme** tabs. Custom theme now has Slack's four hex fields (System navigation, Selected items, Presence indication, Notifications) showing Slack's values (#611F69 / #F9EDFF / #4CC894 / #EABDFB for Aubergine) and taking typed hex (Enter applies it as a useCustomHex ia_theme, like Slack). Share copies the hexes. Window gradient and Darker sidebars moved into Custom theme, as in Slack. Removed Clack's invented palette-swatch rows, Brightness 1–6 buttons and "Emoji set" (Apple/Google). Still different: no Miles Davis preset group (seasonal, presets not ported); no colour square / hue slider popover; Import and Surprise me greyed | ia_theme (applied), window_background_gradients (applied), colour mode (device). Greyed: font, focus_mode_force_dark_mode_enabled |
| Messages & media | **fixed**. Default Skin Tone radios, "your profile" link, JUMBOMOJI subtext, Bring emails into Slack, legends as Slack's. Still different: no example message previews; skin tones are text rows, not hand emoji | **newly written:** preferred_skin_tone (the emoji picker and reactions use it when no tone was picked locally). Applied: display_real_names_override, show_typing, time24, hide_hex_swatch, jumbomoji, expand_internal_inline_imgs, expand_inline_imgs, expand_non_media_attachments. Greyed: messages_theme, emoji_mode, convert_emoticons, quick reactions, obey_inline_img_limit |
| Language & region | **fixed**. Keyboard layout row, Slack's spellcheck label, removed Clack's invented copy. Still different: time zone shows the IANA id, not Slack's "(UTC-08:00) Pacific Time (US and Canada)" | greyed: locale, set_tz_automatically, tz, webapp_spellcheck |
| Accessibility | **fixed**. Alt text reminders legend, tab previews, Slack's animations subtext, the three up-arrow options with descriptions and the Ctrl+↑ note, Lists keyboard interactions, Screen reader (Message verbosity, announcements), links to Notifications and Font. Removed invented "Limit message line length" | applied: underline_links, a11y_animations. Greyed: a11y_show_alt_text_reminders, a11y_peeks_enabled_on_mouse_enter, composer_up_arrow_shortcut, lists_keyboard_interaction, a11y_dm_read_out, a11y_huddles_reactions_* |
| Mark as read | **fixed**. Removed the invented hint under Prompt to confirm; added Keyboard shortcuts | applied: mark_msgs_read_immediately + start_scroll_at_oldest (one users.prefs.set), confirm_clear_all_unreads |
| Audio & video | **fixed**. Camera, Microphone, gain control, noise suppression, auto-dock, 150-member warning, blur, Huddle motion, music delay. Removed "Set my status In a huddle" (Slack shows it under Availability) and "Turn off my video" (gated off in the live dialog) | applied: huddles_mute_by_default. Greyed: huddles_cc_by_default, huddles_show_shouty_rooster, video_background_filter, huddles_show_join_preview, huddles_global_mute, huddles_disconnect_on_lock, huddles_play_music_when_last, clips_pip_auto_dock, agc/ns |
| Salesforce | **fixed**. Slack's exact copy; removed Clack's invented sentence | none |
| Connected accounts | **fixed**. Slack's copy, Learn more, "Apps connected through workflows". Still different: the list isn't fetched (stand-in) | none |
| Privacy & visibility | **fixed**. Learn more links, "Preferences only apply to this workspace" note + Find my workspaces (greyed), the workspace name in Contact sharing, People you've hidden. Removed the invented Privacy Mode section | server: who_can_see_account_by_searching_email, who_can_share_contact_card |
| Advanced | **fixed**. Slack's Input options order and copy, Enter-key radios, Search options, Download location, Confirmations and warnings, Other options, Launch on login / notification-area rows (Linux, Windows). Removed "Copy message text as Markdown" (gated off in the live dialog). Clack's own Live updates / Updates / Troubleshooting section kept | **newly applied:** msg_input_send_btn (Enter adds a line, Mod+Enter sends). Applied: enter_is_special_in_tbt, plain_text_mode. Server: overloaded_message_enabled, in_prod_surveys_enabled. Greyed: disable_smart_symbols, disable_link_truncation, search_exclude_channels, stick_user_sort, should_show_unsend_message_confirmation, confirm_user_marked_away, suppress_link_warning, hide_sc_files_sharing_speed_bump, hide_external_members_canvas_sharing_speed_bump |

## Tests

- `TestPrefsPagesMatchSlack` (internal/ui/prefs_pages_test.go) draws every
  page against fakeslack. It checks each page's controls, headings and order
  against Slack's, and which rows are enabled or greyed. It also checks that
  none of the removed invented rows came back, and checks the Custom theme
  fields' default hexes.
- `TestPrefsAuditAppliedRoundTrip` and `TestPrefsMobileOverrides` cover the
  newly applied prefs. For each one, the row writes Slack's key and value to
  fakeslack, core reads it back, and Clack acts on it (no notification, rail,
  sidebar links, ia_theme).
- `TestSendOnModEnter` (composer) and `TestRoleHexAubergine` (theme).
- `TestPrefsAuditShots` with the `prefs-audit-shots` workflow: every page,
  one shot per screenful, light and dark, uploaded as the `prefs-audit-shots`
  artifact.
