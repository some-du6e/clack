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
| Notifications | **fixed**, then made real (part A, 2026-11-09). Slack's order: Messaging defaults + About notifications · How to notify you (Desktop / Mobile) · What to notify you about (Everything / Mentions and direct messages, mobile follows unless overridden) · Also notify you about (Thread replies, VIPs, New huddles, Set up mobile overrides → mobile select) · What to show in Activity (activity_inbox only) · Channel keywords · Reminders · When you're not active on desktop · Sounds & appearance (macOS dock badge / bounce). Removed Clack's invented "On this computer" switch | **newly applied:** global_desktop_push_enabled; global_keywords (an editable field, 1 s debounce, max 100; keywords count as mentions and are highlighted, lxEU port); desktop_sound + mute_sounds (a message notification plays Slack's sound unless muted or DND; picking one previews it); huddle_invite_sound_v2 + mute_huddle_sounds (the invite ring); mac_ssb_bullet / mac_ssb_bounce (Dock badge • and bounce). Applied: global_desktop, no_text_in_notifications. Server: global_mobile, push_idle_wait, threads_everything, priority_dnd_override, huddles_allow_smart_notif, inactive_user_summary_notification, activity_dms_opt_out, global_channels_in_activity, global_reminders_in_activity, reminder_notification_time (half-hour TimePicker, "H:mm"; Slack's servers send day-only reminders at it). Greyed: none |
| VIP | **fixed** labels, subtexts and order; then made real (part A) | priority_dnd_override (server), vip_users + users.priority.add/remove (applied: the member search adds, Remove removes), Recommended (users.priority.getRecommendations, up to six with Slack's reason lines), priority_sidebar_section (the row shows under vip_section_pref; core makes Slack's client-local "VIP unreads" section first, holding DMs/group DMs with a VIP that have unread mentions). Greyed: none |
| Navigation | **fixed**. Replaced Clack's invented "Show the sidebar" / "Reset sidebar width" with Slack's tab checkboxes, Icons & text / Icons only, and App agents & assistants. **2026-11-09:** the tab list is slack-kit's ReorderableOptionsList (OkAw, `reorderable-options-list` chunk): the active option follows the pointer and ↑/↓/Home/End, a click or Space/Enter toggles it, the Move up / Move down arrows float at the active row (and Alt+↑/↓), and a row dragged onto another takes its index with Slack's drop line. Still different: show_ai_apps (below) | **newly written** (the rail already applied them): tab_rail_customization + tab_rail_customization_reasons (setTabRailCustomizationByTabName), tab_rail_customization_order (setTabRailCustomizationOrder with the listbox's ids, after a move or a drop), ia4_compact_mode. **Newly applied:** hide_agents_in_dm_list (the row shows under agents_in_dm_sidebar and agents_in_dm_sidebar_pref, TabRail's Mq && n7; with agents_in_dm_sidebar an agent's DM is filed under Direct messages, uXg5 oK / isAgentInDmSidebar in getDefaultSectionType, and the pref puts it back under Apps). Greyed: show_ai_apps (Clack has no AI agents entry in the top bar to hide) |
| Home | **fixed**. Added Channel organization and Show activity; views in Slack's order (Unreads, Huddles, Threads, Saved, Drafts & sent, Directories) with Slack's defaults (on); custom filter/sort hints; External Connections row; "Hide muted channels from your sidebar" (improve_mute_pref: the box is the pref's negation) with its helper text | **newly applied:** enable_threads_view, enable_huddles_home_view, enable_sent_view, enable_saved_view, enable_unified_directory_view hide the sidebar links; home_unread_dot (Slack's real key; its input is named enable_home_unread_dot): off, the Home tab has no dot or badge; show_sidebar_avatars: off, DM rows show the presence badge; should_show_connect_section: externally shared conversations go to the slack_connect section. Server: disable_auto_channel_sort (the server files joined channels; the client only reads auto_sort upserts). Applied: sidebar_behavior, channel_sort, separate_private_channels, undo_channel_intermix, boost_mentions, show_muted_items_outside_sidebar_menus. Greyed: enable_unread_view (no Unreads view yet), disable_channel_join_banner (no channel join banner yet) |
| Appearance | **fixed**, then part A. Font row; Color Mode (Light / Dark / System, desktop) plus the focus-mode dark row; **Slack themes \| Custom theme** tabs. Custom theme has Slack's four hex fields (typed hex, or the pencil's "Pick a color" popover: HSV square, hue slider, hex field), Share, **Import** (Slack's Import theme modal: 4 hexes or a legacy 8/10-colour string, createPayloadFromHexStrings) and **Surprise me** (randomizeTheme). A custom primary sets the brightness (getBrightnessFromCustomHex). The "Celebrate the music of Miles Davis" presets show first under miles_davis_themes | ia_theme (applied), window_background_gradients (applied), colour mode (device). Greyed: font (device-local; Clack draws Lato only), focus_mode_force_dark_mode_enabled (Clack has no focus mode) |
| Messages & media | **fixed**, then part A. Default Skin Tone as Slack's six ✋ radios (:wave: on hover), "your profile" link, JUMBOMOJI subtext, legends as Slack's | **newly applied:** preferred_skin_tone, emoji_mode as_text (emoji draw as :name:, no JUMBOMOJI; turning it on writes convert_emoticons 0), convert_emoticons (the composer converts the emoticon before Space/Enter, Slack's dq table), obey_inline_img_limit (linked images over 20MB stay collapsed), one-click reactions (user_customized_quick_reactions_display_feature 1/0, frequent / custom slots; the hover toolbar shows them), Bring emails into Slack (users.emailtoken.get / create / delete, Copy). Applied: display_real_names_override, show_typing, time24, hide_hex_swatch, jumbomoji, expand_internal_inline_imgs, expand_inline_imgs, expand_non_media_attachments. Greyed: messages_theme (Clean / Compact; no compact message layout yet, so no example previews) |
| Language & region | **fixed**, then part A. Keyboard layout row, Slack's spellcheck label. The time zone select lists Slack's 124 labelled zones ("(UTC-08:00) Pacific Time (US and Canada)") | **newly applied:** set_tz_automatically (on: the detected zone is written at once and after every sign-in, maybeUpdateTimezone with Slack's zone tables). Server: tz. Greyed: locale (no translated UI), webapp_spellcheck (no spellchecker) |
| Accessibility | **fixed**. Alt text reminders legend, tab previews, Slack's animations subtext, the three up-arrow options with descriptions and the Ctrl+↑ note, Lists keyboard interactions, Screen reader (Message verbosity, announcements), links to Notifications and Font. Removed invented "Limit message line length". **2026-11-09:** the up-arrow choice is real. Slack keeps it client-side (localStorage composerUpArrowShortcutBehavior, uMEH LIST=1 / LIST_IF_EMPTY=2 / EDIT=3), so Clack keeps it in ui.json, and the composer follows MessageInput.onUpArrow. LIST focuses the message list at the start of the first line, LIST_IF_EMPTY only in an empty field, EDIT (or unset without up_to_browse_kb_shortcut) edits your last message, and Mod+↑ in an empty field always edits. The list then selects its newest message: ↑/↓ move, ↓ past the end or Esc go back, Enter/E edit yours. The Lists and huddle rows now read Slack's real keys (lists_tabbing_navigation_option_type, huddles_reactions_read_out_loud, huddles_reactions_play_sound) | applied: underline_links, a11y_animations, composerUpArrowShortcutBehavior (device). Greyed: Simplified layout mode (a whole second layout), a11y_show_alt_text_reminders (Clack's uploads have no alt text field yet; Slack's reminder opens the edit-file alt text dialog), a11y_peeks_enabled_on_mouse_enter (Clack's rail has no hover peeks), lists_tabbing_navigation_option_type (Clack's Lists grid has no cell keyboard navigation in either mode), a11y_dm_read_out and huddles_reactions_read_out_loud (screen-reader announcements; Clack has no accessibility bridge), huddles_reactions_play_sound (the sound lives in the huddle chunks, not fetched yet) |
| Mark as read | **fixed**. Removed the invented hint under Prompt to confirm; added Keyboard shortcuts | applied: mark_msgs_read_immediately + start_scroll_at_oldest (one users.prefs.set), confirm_clear_all_unreads |
| Audio & video | **fixed**. Camera, Microphone, gain control, noise suppression, auto-dock, 150-member warning, blur, Huddle motion, music delay. Removed "Set my status In a huddle" (Slack shows it under Availability) and "Turn off my video" (gated off in the live dialog) | applied: huddles_mute_by_default, **huddles_cc_by_default** (a joined huddle starts with captions shown), **huddles_show_shouty_rooster** (starting, not joining, a huddle in a channel of more than 150 members asks "Start a huddle in this channel?", HuddleShoutyRoosterModal, with its "Don’t show this message again." box writing the pref). Greyed: video_background_filter (needs person segmentation on camera frames), huddles_show_join_preview (Slack's pre-join preview window), huddles_global_mute (an OS-wide Mod+Shift+Space hotkey), huddles_disconnect_on_lock (screen-lock events per OS), huddles_play_music_when_last (the lounge music and delay), huddles_reactions_effects, clips_pip_auto_dock (Clack has no clip picture-in-picture), agc/ns (Clack's microphone path has no WebRTC audio processing) |
| Salesforce | **fixed**. Slack's exact copy; removed Clack's invented sentence | none |
| Connected accounts | **fixed**. Slack's copy, Learn more, "Apps connected through workflows". **2026-11-09:** the list is fetched like ConnectedWorkflowAccountsList (fetchUserAuth: apps.auth.external.userAuths.get, reason fetch_user_auth): one row per app, or "No accounts found". Still different: no Manage button (Slack's manage-accounts modal isn't ported) | none |
| Privacy & visibility | **fixed**. Learn more links, "Preferences only apply to this workspace" note, the workspace name in Contact sharing, People you've hidden. Removed the invented Privacy Mode section. **2026-11-09:** Find my workspaces opens Slack's get-started find page (Rgd9); Invitations you’ve blocked asks users.emailVerificationStatus.get and, verified, lists users.listIgnoredUsers with each email's unblock button (UnblockConfirmationModal, users.removeUserBlock), or "You haven’t blocked any invitations from anyone yet."; unverified, Slack's 👋 copy. People you’ve hidden lists hidden_users with unhide (unhideUserApi writes the list without them), or "You haven’t hidden anyone yet." Still different: "Send Verification Email" is greyed (Slack's code-entry flow goes through signin.confirmCode); Clack doesn't hide hidden people's messages | server: who_can_see_account_by_searching_email, who_can_share_contact_card, hidden_users |
| Advanced | **fixed**. Slack's Input options order and copy, Enter-key radios, Search options, Download location, Confirmations and warnings, Other options, Launch on login / notification-area rows (Linux, Windows). Removed "Copy message text as Markdown" (gated off in the live dialog). Clack's own Live updates / Updates / Troubleshooting section kept | **newly applied:** msg_input_send_btn (Enter adds a line, Mod+Enter sends). Applied: enter_is_special_in_tbt, plain_text_mode. **2026-11-09:** disable_smart_symbols (behind composer_smart_symbols, as iZ.w gates the row: TextSubstitutionsModule's "-- " → "— ", -> ← ↔, ... → …, type-time curly quotes, none in code, each its own undo), search_exclude_channels (Slack's channel multi-select; the In suggestions drop them as parseSuggestions does, Slack's servers drop the results), stick_user_sort (a new search starts from search_sort; the sort menu says "Most relevant" and writes search_sort, "Most relevant (default)" otherwise, plus "Edit default"), should_show_unsend_message_confirmation (Mod+Z in the empty composer within 15 s of a send unsends it and puts it back, behind Slack's Unsend message dialog), confirm_user_marked_away (after connecting while manually away, "Set yourself to active?"), suppress_link_warning (isLinkSuspicious and the "Double-check this link" dialog on message links). Server: overloaded_message_enabled, in_prod_surveys_enabled. Greyed: disable_link_truncation (its reader isn't in the public client bundles checked), hide_sc_files_sharing_speed_bump and hide_external_members_canvas_sharing_speed_bump (the send-time Slack Connect "Is it OK to send this?" dialog isn't ported), download location, launch on login, notification area |

## Still greyed after part A, and the plan for each

- **enable_unread_view** (Home): Slack's Unreads view (gantry chunks
  page-unreads-view and the catch-up hooks): a sidebar link and a page
  listing unread conversations, newest first, with Mark as read per
  conversation. Clack has the counts and message lists; it needs the view
  in internal/ui/views (one more synthetic-view route like Threads) and the
  sidebar link the pref shows.
- **disable_channel_join_banner** (Home): the
  megaphone-message-pane-input-channel-join-banner chunk, shown over the
  composer after joining a channel (section and notification suggestions).
  Port the banner into the composer chrome, then enable the row.
- **messages_theme** (Messages & media): Compact ("dense") is a second row
  layout for every message (name and time on one line, no avatar column)
  plus Slack's CompactMessageExample / clean example in the dialog. It
  touches internal/ui/messages row layout, so it wants its own branch.
- **font** (Appearance): device-local (Slack keeps it in localStorage). The
  list is Lato, Arial, Comic Sans, Georgia, Noto Sans, OpenDyslexic, Roboto
  Mono, Verdana, Atkinson Hyperlegible Next and the system font. It needs
  per-OS font discovery and Slack's CDN web fonts (woff2, so a woff2
  decoder) loaded under theme.Typeface, then a shaper rebuild.
- **focus_mode_force_dark_mode_enabled** (Appearance): only means something
  once Clack has Slack's focus mode.
- **locale** (Language & region): the whole UI's strings through a catalog
  for Slack's 12 locales (Slack's i18n bundles are per-locale chunks), plus
  the localized time zone and sound lists Slack already ships (sbu9 has 11).
- **webapp_spellcheck**: a spellchecker in the composer (NSSpellChecker on
  macOS, ISpellChecker on Windows, hunspell dictionaries on Linux) with the
  red underline and suggestions menu.

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
- `TestPrefsAuditAppliedRoundTripB` covers part B (Navigation drag and
  Move down, smart symbols, search exclusions and sticky sort, link
  warning, unsend, the presence prompt, the up-arrow choice in ui.json,
  Connected accounts and the Privacy lists) against fakeslack, with hooks
  for the methods the fake doesn't serve. `TestSmartSymbols`,
  `TestUpArrowShortcut` and `TestUndoEmptyUnsends` (composer),
  `TestLinkSuspicious` (popups) and `TestHuddleJoinPrefs` (huddleui) check
  the behaviour.
- `TestSendOnModEnter` (composer) and `TestRoleHexAubergine` (theme).
- Part A: `TestPrefsAuditAppliedRoundTrip{Sounds,Keywords,Dock,Home,Messages,Time,VIP,ThemeButtons}`,
  `TestPrefsSkinToneHandsAndMilesDavis`, `TestPrefsEmailForwarding` and
  `TestColorPopover` (internal/ui/prefs_a_test.go); `TestConnectSectionPref`,
  `TestKeywordMention`, `TestPrioritySection` (core);
  `TestLocateHighlightWords`, `TestHighlightDocSplitsSpans` (richtext);
  `TestLastEmoticon`, `TestEmoticonOnSpace` (composer);
  `TestQuickReactionsFollowPrefs` (messages); `TestImportThemeString`,
  `TestSurpriseTheme` (theme).
- `TestPrefsAuditShots` with the `prefs-audit-shots` workflow: every page,
  one shot per screenful, light and dark, uploaded as the `prefs-audit-shots`
  artifact.
