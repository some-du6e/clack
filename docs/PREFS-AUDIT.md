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

Plugins / Themes / Plugin settings and Clack (updates, Replay introduction, live updates)
were out of scope and left as they were.

**Legend:** *applied* means Clack acts on the pref. *Server* means Slack's
servers act on it (mobile push, email, privacy), so writing it works for
real. *Greyed* means the row is drawn the way Slack draws it, disabled, with
"Not available in Clack yet".

| Page | Result | Prefs (users.prefs / all_notifications_prefs.global) |
| --- | --- | --- |
| Availability | matched, then **fixed** 2026-11-10 (see "Availability, status and focus mode" below): "VIPs" in the Working hours paragraph is a link that switches the dialog to the VIP section; Custom shows Slack's row per day; Weekdays shows its subtext; the selects have Slack's options (None, Midnight, wrap-around) and bump an end at or before its start | dnd_* / working hours (server; each change writes only what changed plus dnd_enabled), **dnd_before_/dnd_after_/dnd_enabled_&lt;day&gt;** (server), automatic statuses **huddles_set_status, focus_mode_status{show}, scheduled_dnd_status{show}** (server; Clack wrote focus_mode_set_status and outside_working_hours_set_status, which Slack doesn't read) |
| Notifications | **fixed**, then made real (part A, 2026-11-09). Slack's order: Messaging defaults + About notifications · How to notify you (Desktop / Mobile) · What to notify you about (Everything / Mentions and direct messages, mobile follows unless overridden) · Also notify you about (Thread replies, VIPs, New huddles, Set up mobile overrides → mobile select) · What to show in Activity (activity_inbox only) · Channel keywords · Reminders · When you're not active on desktop · Sounds & appearance (macOS dock badge / bounce). Removed Clack's invented "On this computer" switch | **newly applied:** global_desktop_push_enabled; global_keywords (an editable field, 1 s debounce, max 100; keywords count as mentions and are highlighted, lxEU port); desktop_sound + mute_sounds (a message notification plays Slack's sound unless muted or DND; picking one previews it); huddle_invite_sound_v2 + mute_huddle_sounds (the invite ring); mac_ssb_bullet / mac_ssb_bounce (Dock badge • and bounce). Applied: global_desktop, no_text_in_notifications. Server: global_mobile, push_idle_wait, threads_everything, priority_dnd_override, huddles_allow_smart_notif, inactive_user_summary_notification, activity_dms_opt_out, global_channels_in_activity, global_reminders_in_activity, reminder_notification_time (half-hour TimePicker, "H:mm"; Slack's servers send day-only reminders at it). Greyed: none |
| VIP | **fixed** labels, subtexts and order; then made real (part A) | priority_dnd_override (server), vip_users + users.priority.add/remove (applied: the member search adds, Remove removes), Recommended (users.priority.getRecommendations, up to six with Slack's reason lines), priority_sidebar_section (the row shows under vip_section_pref; core makes Slack's client-local "VIP unreads" section first, holding DMs/group DMs with a VIP that have unread mentions). Greyed: none |
| Navigation | **fixed**. Replaced Clack's invented "Show the sidebar" / "Reset sidebar width" with Slack's tab checkboxes, Icons & text / Icons only, and App agents & assistants. **2026-11-09:** the tab list is slack-kit's ReorderableOptionsList (OkAw, `reorderable-options-list` chunk): the active option follows the pointer and ↑/↓/Home/End, a click or Space/Enter toggles it, the Move up / Move down arrows float at the active row (and Alt+↑/↓), and a row dragged onto another takes its index with Slack's drop line. Still different: show_ai_apps (below) | **newly written** (the rail already applied them): tab_rail_customization + tab_rail_customization_reasons (setTabRailCustomizationByTabName), tab_rail_customization_order (setTabRailCustomizationOrder with the listbox's ids, after a move or a drop), ia4_compact_mode. **Newly applied:** hide_agents_in_dm_list (the row shows under agents_in_dm_sidebar and agents_in_dm_sidebar_pref, TabRail's Mq && n7; with agents_in_dm_sidebar an agent's DM is filed under Direct messages, uXg5 oK / isAgentInDmSidebar in getDefaultSectionType, and the pref puts it back under Apps). Greyed: show_ai_apps (Clack has no AI agents entry in the top bar to hide) |
| Home | **fixed**. Added Channel organization and Show activity; views in Slack's order (Unreads, Huddles, Threads, Saved, Drafts & sent, Directories) with Slack's defaults (on); custom filter/sort hints; External Connections row; "Hide muted channels from your sidebar" (improve_mute_pref: the box is the pref's negation) with its helper text | **newly applied:** enable_threads_view, enable_huddles_home_view, enable_sent_view, enable_saved_view, enable_unified_directory_view hide the sidebar links; home_unread_dot (Slack's real key; its input is named enable_home_unread_dot): off, the Home tab has no dot or badge; show_sidebar_avatars: off, DM rows show the presence badge; should_show_connect_section: externally shared conversations go to the slack_connect section. Server: disable_auto_channel_sort (the server files joined channels; the client only reads auto_sort upserts). Applied: sidebar_behavior, channel_sort, separate_private_channels, undo_channel_intermix, boost_mentions, show_muted_items_outside_sidebar_menus. **2026-11-09, newly applied:** enable_unread_view (true shows Slack's Unreads link first in the sidebar and the Unreads page, see below), disable_channel_join_banner (the box is its negation; on, the channel join banner shows over the composer, see below). Greyed: none |
| Appearance | **fixed**, then part A. Font row; Color Mode (Light / Dark / System, desktop) plus the focus-mode dark row; **Slack themes \| Custom theme** tabs. Custom theme has Slack's four hex fields (typed hex, or the pencil's "Pick a color" popover: HSV square, hue slider, hex field), Share, **Import** (Slack's Import theme modal: 4 hexes or a legacy 8/10-colour string, createPayloadFromHexStrings) and **Surprise me** (randomizeTheme). A custom primary sets the brightness (getBrightnessFromCustomHex). The "Celebrate the music of Miles Davis" presets show first under miles_davis_themes | ia_theme (applied), window_background_gradients (applied), colour mode (device). **2026-11-09:** font (device, ui.json `client_typeface`; applied to the whole UI, see "Font" below). **2026-11-10:** focus_mode_force_dark_mode_enabled (applied: the silent sidebar theme's sidebarInverted while focus mode is on; shown only under silent_sidebar in light mode, like ThemeColorModeContainer), see "Availability, status and focus mode" below |
| Messages & media | **fixed**, then part A. Default Skin Tone as Slack's six ✋ radios (:wave: on hover), "your profile" link, JUMBOMOJI subtext, legends as Slack's | **newly applied:** preferred_skin_tone, emoji_mode as_text (emoji draw as :name:, no JUMBOMOJI; turning it on writes convert_emoticons 0), convert_emoticons (the composer converts the emoticon before Space/Enter, Slack's dq table), obey_inline_img_limit (linked images over 20MB stay collapsed), one-click reactions (user_customized_quick_reactions_display_feature 1/0, frequent / custom slots; the hover toolbar shows them), Bring emails into Slack (users.emailtoken.get / create / delete, Copy). Applied: display_real_names_override, show_typing, time24, hide_hex_swatch, jumbomoji, expand_internal_inline_imgs, expand_inline_imgs, expand_non_media_attachments. **2026-11-09, newly applied:** messages_theme (Clean writes light_with_avatars, Compact dense; MessageThemePref), with Slack's "Here’s an example:" pillows under Theme and Names (CompactMessageExample / MessageWithAvatarExample); see [Compact](#compact-messages_theme-dense-2026-11-09) |
| Language & region | **fixed**, then part A. Keyboard layout row, Slack's spellcheck label. The time zone select lists Slack's 124 labelled zones ("(UTC-08:00) Pacific Time (US and Canada)") | **newly applied:** set_tz_automatically (on: the detected zone is written at once and after every sign-in, maybeUpdateTimezone with Slack's zone tables). Server: tz. **2026-11-10, newly applied:** webapp_spellcheck (the composer, thread replies, message edits, canvases and drafts are spellchecked; on Windows and Linux Slack's language multi-select follows, on macOS its "System Preferences > Keyboard > Text" hint; see [Spellcheck](#spellcheck-webapp_spellcheck-2026-11-10)). **2026-11-10, newly applied:** locale (see [Language](#language-locale-2026-11-10)) |
| Accessibility | **fixed**. Alt text reminders legend, tab previews, Slack's animations subtext, the three up-arrow options with descriptions and the Ctrl+↑ note, Lists keyboard interactions, Screen reader (Message verbosity, announcements), links to Notifications and Font. Removed invented "Limit message line length". **2026-11-09:** the up-arrow choice is real. Slack keeps it client-side (localStorage composerUpArrowShortcutBehavior, uMEH LIST=1 / LIST_IF_EMPTY=2 / EDIT=3), so Clack keeps it in ui.json, and the composer follows MessageInput.onUpArrow. LIST focuses the message list at the start of the first line, LIST_IF_EMPTY only in an empty field, EDIT (or unset without up_to_browse_kb_shortcut) edits your last message, and Mod+↑ in an empty field always edits. The list then selects its newest message: ↑/↓ move, ↓ past the end or Esc go back, Enter/E edit yours. The Lists and huddle rows now read Slack's real keys (lists_tabbing_navigation_option_type, huddles_reactions_read_out_loud, huddles_reactions_play_sound). **2026-11-09, lists-keyboard:** Lists keyboard interactions is wired. The section shows only with the lists_tabbing_navigation experiment (ListsTabbingPref returns null without it). Data entry is checked while the pref is unset, and a pick writes tab_nav or arrow_nav with setUserPrefByApi. The Lists grid follows it: cell keyboard navigation in both modes (docs/LISTS.md, Keyboard) | applied: underline_links, a11y_animations, composerUpArrowShortcutBehavior (device), lists_tabbing_navigation_option_type, Simplified layout mode (device: bbm, bbmSettings; see "Simplified layout mode" below). Greyed: a11y_show_alt_text_reminders (Clack's uploads have no alt text field yet; Slack's reminder opens the edit-file alt text dialog), a11y_peeks_enabled_on_mouse_enter (Clack's rail has no hover peeks), a11y_dm_read_out and huddles_reactions_read_out_loud (screen-reader announcements; Clack has no accessibility bridge), huddles_reactions_play_sound (the sound lives in the huddle chunks, not fetched yet) |
| Mark as read | **fixed**. Removed the invented hint under Prompt to confirm; added Keyboard shortcuts | applied: mark_msgs_read_immediately + start_scroll_at_oldest (one users.prefs.set), confirm_clear_all_unreads |
| Audio & video | **fixed**. Camera, Microphone, gain control, noise suppression, auto-dock, 150-member warning, blur, Huddle motion, music delay. Removed "Set my status In a huddle" (Slack shows it under Availability) and "Turn off my video" (gated off in the live dialog) | applied: huddles_mute_by_default, **huddles_cc_by_default** (a joined huddle starts with captions shown), **huddles_show_shouty_rooster** (starting, not joining, a huddle in a channel of more than 150 members asks "Start a huddle in this channel?", HuddleShoutyRoosterModal, with its "Don’t show this message again." box writing the pref). **2026-11-10, huddle-audio, newly applied:** agc and ns (device prefs in ui.json, like Slack's localStorage prefs.audio: gain control and RNNoise noise suppression before the Opus encoder), huddles_show_join_preview (the join preview window), huddles_global_mute (Mod+Shift+Space while in a huddle), huddles_disconnect_on_lock (leave when the screen locks), huddles_play_music_when_last and huddles_play_music_timeout (the lounge music); see "Audio & video (2026-11-10)" below. **2026-11-10, bg-blur:** video_background_filter ("Blur your video background…": Slack's prefs.video.effect, a device pref; the camera's background is blurred before the self view and the encoder, see "Background blur" below). Greyed: huddles_reactions_effects, clips_pip_auto_dock (Clack has no clip picture-in-picture) |
| Salesforce | **fixed**. Slack's exact copy; removed Clack's invented sentence | none |
| Connected accounts | **fixed**. Slack's copy, Learn more, "Apps connected through workflows". **2026-11-09:** the list is fetched like ConnectedWorkflowAccountsList (fetchUserAuth: apps.auth.external.userAuths.get, reason fetch_user_auth): one row per app, or "No accounts found". **2026-11-12 (gap-w16):** each row is Slack's ConnectedWorkflowAccountAppRow with its Manage button, which opens the manage-accounts modal and its Disconnect confirmation (apps.auth.external.delete), see "Connected accounts' Manage" below | none |
| Privacy & visibility | **fixed**. Learn more links, "Preferences only apply to this workspace" note, the workspace name in Contact sharing, People you've hidden. Removed the invented Privacy Mode section. **2026-11-09:** Find my workspaces opens Slack's get-started find page (Rgd9); Invitations you’ve blocked asks users.emailVerificationStatus.get and, verified, lists users.listIgnoredUsers with each email's unblock button (UnblockConfirmationModal, users.removeUserBlock), or "You haven’t blocked any invitations from anyone yet."; unverified, Slack's 👋 copy. People you’ve hidden lists hidden_users with unhide (unhideUserApi writes the list without them), or "You haven’t hidden anyone yet." **2026-11-12 (gap-w16):** Send Verification Email works: signup.confirmEmail emails the code, VerifyEmailModal takes it (signin.confirmCode), and the page then lists the blocked invitations, see "Send Verification Email" below. **2026-11-12 (gap-w8):** hidden people's messages are hidden (HiddenMessageBody with Show anyway, MESSAGES-AUDIT "Hidden people's messages"), and unhiding someone here also resets their messages shown anyway (unhideUserApi) | server: who_can_see_account_by_searching_email, who_can_share_contact_card, hidden_users |
| Advanced | **fixed**. Slack's Input options order and copy, Enter-key radios, Search options, Download location, Confirmations and warnings, Other options, Launch on login / notification-area rows (Linux, Windows). Removed "Copy message text as Markdown" (gated off in the live dialog). Clack's own rows moved to its own Clack tab (after Advanced, where a Taut tab lands; three rows: updates, Replay introduction, live updates; Troubleshooting is the macOS Help menu's) | **newly applied:** msg_input_send_btn (Enter adds a line, Mod+Enter sends). Applied: enter_is_special_in_tbt, plain_text_mode. **2026-11-09:** disable_smart_symbols (behind composer_smart_symbols, as iZ.w gates the row: TextSubstitutionsModule's "-- " → "— ", -> ← ↔, ... → …, type-time curly quotes, none in code, each its own undo), search_exclude_channels (Slack's channel multi-select; the In suggestions drop them as parseSuggestions does, Slack's servers drop the results), stick_user_sort (a new search starts from search_sort; the sort menu says "Most relevant" and writes search_sort, "Most relevant (default)" otherwise, plus "Edit default"), should_show_unsend_message_confirmation (Mod+Z in the empty composer within 15 s of a send unsends it and puts it back, behind Slack's Unsend message dialog), confirm_user_marked_away (after connecting while manually away, "Set yourself to active?"), suppress_link_warning (isLinkSuspicious and the "Double-check this link" dialog on message links). Server: overloaded_message_enabled, in_prod_surveys_enabled. **2026-11-10:** disable_link_truncation applied the way Slack's client applies it: InputOptionsPref writes !checked with setUserPrefByApi, and the prefs-dialog chunks are the only client code that reads it. Greyed: hide_sc_files_sharing_speed_bump and hide_external_members_canvas_sharing_speed_bump (the send-time Slack Connect "Is it OK to send this?" dialog isn't ported), download location, launch on login, notification area |

## Still greyed after part A, and the plan for each

enable_unread_view and disable_channel_join_banner are done: see "Unreads view and channel join banner" below.

- ~~**messages_theme** (Messages & media)~~: done 2026-11-09, see "Compact" below.
- ~~**font** (Appearance)~~: done 2026-11-09, see "Font" below.
- ~~**focus_mode_force_dark_mode_enabled** (Appearance)~~: done 2026-11-10,
  see "Availability, status and focus mode" below.
- **locale** (Language & region): applied 2026-11-10, in part. See
  "Language" below and docs/I18N.md.
- ~~**webapp_spellcheck**~~: done 2026-11-10, see "Spellcheck" below.

## No notes (prefs-cleanup, 2026-11-10)

No page says "Not available in Clack yet" any more. Each row that had the
note is now one of three things.

**Written the way Slack's dialog writes it** (setUserPrefByApi, Slack's
default and box sense). Slack's other clients act on these. Clack lacks the
part that reads them:

| Pref | What reads it in Slack | Why Clack doesn't act on it |
| --- | --- | --- |
| show_ai_apps | the top nav's AI apps container (boot-render) | Clack's top nav has no AI agents |
| a11y_show_alt_text_reminders | the upload flow's image description reminder (boot-shared, client-boot) | Clack's uploads have no image description field |
| a11y_peeks_enabled_on_mouse_enter | only the prefs-dialog chunks name it | Clack's rail has no hover peeks |
| a11y_dm_read_out | only the prefs-dialog chunks name it | no screen-reader bridge |
| huddles_reactions_read_out_loud | the screen-reader announcer (boot-shared) | no screen-reader bridge |
| huddles_reactions_play_sound | the huddle reaction sound (client-boot) | the sound isn't fetched yet |
| clips_pip_auto_dock | clips' picture-in-picture (boot-render) | Clack plays clips inline |
| huddles_reaction_explosions ("Allow for reactions & effects"; the old row used the wrong key) | the huddle's reaction effects | Clack's huddles draw none. As in HuddleReactionsPref, the row is greyed and unticked while a11y_animations is off, with Slack's "Animations are turned off in Accessibility preferences." |
| hide_sc_files_sharing_speed_bump, hide_external_members_canvas_sharing_speed_bump | the send-time Slack Connect speed bumps (boot-shared) | those dialogs aren't ported |

**Implemented:**

- Download location (DownloadLocationPref) has Slack's box, the folder
  in a read-only field, and Change… (greyed while the box is on), with
  Slack's note. With the box on, each download opens the system's save
  dialog (gioui.org/x/explorer: the xdg portal, Windows' and macOS'
  panels). Off, downloads go to the folder that Change… picked
  (`download_ask`, `download_path` in ui.json; ~/Downloads by default).
- Launch app on login (DesktopPref launchOnStartup, Linux and Windows)
  writes an XDG autostart entry (`$XDG_CONFIG_HOME/autostart/clack.desktop`,
  starting the AppImage when run from one) or the per-user Run key.

**Greyed the way Slack shows a disabled row, without a note:**

- Leave app running in notification area (runFromTray): Gio's window
  can't refuse a close, so it can't stay in the tray, and Linux has no
  tray.

## Language (locale, 2026-11-10)

Slack's model, with the evidence, is in docs/I18N.md. In short: per-locale
JSON bundles on the CDN, keyed by namespace and a SHA-1 prefix of the
English source, formatted with format-message, falling back to the source.

| What | Slack | Clack |
|---|---|---|
| Row | `LocalePref`: a large full-width select of the 12 locales, each label in its own language, sorted by label. Disabled offline with "Go online to change your language."; otherwise "Choose the language you’d like to use with Slack." | `localePref` (locale_wire.go), the same |
| Writing | `setLocaleForTeam`: setLocale (localConfig user_locale, ensure the bundle, `<html lang>`, re-render), then setUserPrefByApi `locale`, then refetchLocalizedApis. The desktop app does it all live | `i18n.SetLocale` loads the bundle on a goroutine (the cache, else the CDN), then Invalidate; users.prefs.set `locale`; ui.json `locale`, so the next start loads the bundle before users.prefs answers. Live |
| Time zones | `sbu9 getList(locale)`: 11 localized lists of 124 zones | `core.TimezonesFor` (tz_slack_locales.go, generated from sbu9) |
| Sounds | The sound map's labels in the `sounds` namespace ("Knock Brush" is "Pincelada") | `soundLabel` |
| Dates, times | dayjs locales, Azem tables, toTime, formatDateDayDivider | `i18n.DayDivider`, `Time`, `TimeAgo`… (uipref.Clock follows the locale) |

What's wired and what's left: see "Wiring" in docs/I18N.md.

## Availability, status and focus mode (2026-11-10)

Read from Slack's web client, build a84662d2d135 on the public CDN (no
cookie): `gantry-v2-async-prefs-dialog` (AvailabilitySection, DndPreferences,
DndSchedule, DndCustomPreferences, BaseDndTimeSelect, ThemeColorModeContainer),
`gantry-v2-async-client-boot-shared` (jDuV dnd constants, 06oL time options,
QBEZ pause labels), `gantry-v2-async-client-boot-render` (SilentSidebarTrigger,
FocusModePopoverContent, GTtr toggleFocusMode, the Vjk7 popover CSS, the
keymap) and `modern.vendor` (tHjK CustomStatusModal, 7Nma, OwtR, KQp3, /Ab5,
v4SM, OmIz, 1Hmm, T80I, Pm/a). Nothing was written to the real account; the
tests run against fakeslack.

**Working hours.** The first select is dnd_end_hour (notifications start when
DND ends), the second dnd_start_hour; with dnd_enabled off the page shows Every
day, 12:00 AM to Midnight. Options are the 48 half hours plus Midnight (23:59);
the second select only lists times after the first, wrapping past midnight on
Every day / Weekdays (never 00:00). Choosing a first time at or after the second
moves the second to the next half hour. Weekdays adds "You won't receive
notifications on Saturday or Sunday." Custom adds a row per day, Monday first
(36px, 16 apart, a 120px day label): the first select (dnd_before_&lt;day&gt;)
has a "None" option (dnd_enabled_&lt;day&gt; all_day, hiding the second
select) while fewer than six other days are None; a "disabled" day shows 12:00
AM to Midnight; picking a time makes the day "partial". Every change sends only
the prefs it changed plus dnd_enabled: true, as setMultiplePrefsByApi does
(users.prefs.set prefs=JSON). The hours come as "H:MM" strings, sometimes
unpadded ("8:00", like reminder_notification_time); BaseDndTimeSelect pads
them to five characters to find the option and labels it with
format_24_hour_time (time24: "08:00", else "8:00 AM"). An unset hour is
checked before padding: the select shows its "--:--" placeholder and gets
no option of its own (2026-11-10: Clack had padded "" to "00000" and shown
that). The automatic statuses write huddles_set_status
and the focus_mode_status / scheduled_dnd_status objects with show flipped
(users.prefs.set name/value). "VIPs" calls the dialog's section switch with
"vip" (Slack's setActiveSection).

**Set a status.** A status starting now goes through users.profile.set with
Slack's profile: status_emoji (`:speech_balloon:` for text only),
status_text, status_text_canonical (resolveTextCanonicalToSave: a preset's
canonical while its text is unchanged, Out of office always),
status_expiration, status_duration (30_minutes, all_day, week, custom…),
ooo_message, and status_is_dnd under the status_improvements experiment. A start
more than 30 s ahead (isScheduledImmediately) goes through
users.customStatus.schedule (emoji, text, text_canonical, date_scheduled,
date_expire, is_dnd, ooo_message, duration custom); "Status scheduled." The
modal lists users.customStatus.list's future statuses under "Scheduled"
(emoji, text, "Today, 3:00 PM – 5:00 PM"); a row edits it ("Edit status",
"Update", custom_status_id; "Scheduled status updated.") and its × asks
"Delete this scheduled status?" (Keep Status / Delete Status, then
users.customStatus.deleteScheduled, "Scheduled status removed."). Five
scheduled statuses block a sixth ("You can only have 5 scheduled statuses at
a time."), and an end in the past shows "End time must be in the future."
The automatic reply is sent as JSON rich_text blocks; it is prefilled from
the saved ooo_message (users.profile.get), else "I'm out of office until
{date}." in v4SM's wording (a time today; "tomorrow" / "next week" / a day
for an end at 11:59 PM; a date a week or more out), which follows the end
until edited; an unedited saved reply is left out so the server keeps it,
and any other status sends "" to clear it. "This week" ends Friday 11:59 PM,
Start defaults to the current minute, Vacationing pauses notifications by
itself and a Vacationing that doesn't clear becomes a start and end range.
Clear Status is clearUserStatus (status_text_canonical null, ending a snooze
that belonged to the status). Like Slack, saving shows no toast.

**Opening it, the tip and the calendar row** (exp-media, CDN build
4d0c78085232). Mod+Shift+Y is Slack's status shortcut (boot-render PG1Q:
openCustomStatusDialog with fromKeyboardShortcut); Clack had bound it to an
invented dark mode toggle, and Slack has no dark mode shortcut. The shortcut
first saves used_custom_status_kb_shortcut (setUserPrefByApi), and while that
pref is unset the modal, however it was opened, shows the keyboard hint
(D72a showKeyboardHint): a .p-pill 50px under the modal's bottom, centred on
it, "**Tip:** Open this faster with **⌘ + Shift + Y**" (Ctrl elsewhere), for
as long as the modal is open (Clack showed it only from the user menu, at the
window's bottom, for 4 s). The "Automatically updates" row was always "In a
meeting — Based on your Google Calendar" and toasted that calendar sync isn't
available. Slack builds it from getCurrentCalendarAndUser on mount
(calendar.getInstalledCalendars {}, then calendar.user.info {app_type} when
Google or Outlook is installed; the current calendar from mx_calendar_type) and
getAllCalendarPresets (aq7I), so there's no row without a manageable or
installed calendar app. The row has the app's logo and "Based on your
{Google} Calendar" or "{Google} Calendar updates are on". Pressing it swaps
the body for the calendar step (renderCalendarModalSyncText's line, the status
sync picture) with "ⓘ Learn more" and Back + Add / Connect / Turn On / Turn
Off: Add opens slack.com/interop/{gcal,ocal}app/install, Connect the calendar
user's provider_auth_url, Turn On/Off its turn_on_status_sync_url or
calendar.user.toggleStatusSync {calapp_user_id, enable} (the store first, back
on failure), then the modal closes.

**Focus mode** is real in this build: Slack calls it the silent sidebar, and
it exists under the silent_sidebar experiment (the live dialog's Appearance
row, which only ThemeColorModeContainer draws under that experiment, shows
the test account has it). It is not a DND preset or a status: turning it on
writes focus_mode_expiration_ts (now + 30 min / 1 h / 2 h, or 9999999999 for
"Don't turn off"); the server then pauses notifications and, with
Availability's "In focus mode" checked, sets that status. The control strip's
moon button toggles it (Clack had an invented light/dark toggle in that
slot); off, its tooltip is "Turn on focus mode / Pause notifications and
reduce badging and bolding."; on, hovering opens "You're in focus mode" with
the Availability gear, Slack's telescope (focus-d8ca34d.svg drawn natively),
a subtitle, "Notifications paused (until …)", the Duration select and
"Continue to get notifications from VIPs" (priority_dnd_override). While it
is on, unread rows aren't bold, mention badges are dots (the count on hover),
and the theme is the Silent Sidebar Gray theme, whose sidebarInverted is
focus_mode_force_dark_mode_enabled. Under focus_mode_custom_duration
"treatment" (exp-media, CDN build 4d0c78085232) the Duration select also has
Custom: picking it writes today's next half hour an hour on, and the row under
the select holds the date button ("Nov 11th, 2026", Slack's DatePicker from
today to a year on) and a 30-minute time select (later than now on today);
each change writes the pref, and turning focus mode off puts Custom back to
"Don't turn off". Not built: focus music (Slack's Miles
Davis soundtrack, under focus_mode_music), the calendar-started popover
(calendar_focus_sync, which also starts a Custom duration at the event's end),
the first-open Lottie animation and swirling blobs
(Clack draws the reduced-motion rendering), the recap
(focus_mode_recap, an AI summary), the button's glow coachmark, and the
other silent sidebar themes (only Gray is the default, and Slack's picker for
them isn't in the prefs dialog).

## Spellcheck (webapp_spellcheck, 2026-11-10)

Read from Slack's web client (build a84662d2d135 on the public CDN, fetched
without a cookie), the Slack 4.53.276 desktop shell (`app.asar`
`dist/main.bundle.cjs` and `preload.bundle.js`) and Chromium 140's sources
(`chromium.googlesource.com`, tag 140.0.7339.0). The real account's prefs
were never changed.

| Rule | Evidence | Clack |
| --- | --- | --- |
| The pref | Texty's props `enableSpellCheck: !!webapp_spellcheck`; `handlePropChanges` sets the contenteditable's `spellcheck` attribute; canvases get `spellcheckEnabled` (Quip `setSpellcheckEnabled`) | `spell.Service.SetEnabled`, from users.prefs and at once from the Preferences box |
| Where | Every Texty input: the message composer, thread replies, message edits, drafts; canvases. slack-kit TextInput defaults to `spellCheck: false` (modals' single-line fields aren't checked) | every `composer.Editor` (composer, edit, canvases, drafts, Block Kit rich_text_input); `Editor.SetSpellcheck(false)` opts one out |
| Not checked | Texty's `CodeBlockContainer` sets `spellcheck=false`; `SlackMention`, `c-slackslug`, `SlackTag`, `SearchFilter` do too; emoji are images. Inline `code` and link text keep the attribute, so Chromium checks them | code-block lines and atoms (mentions, channels, broadcasts, emoji, message slugs) are skipped; inline code and link text are checked |
| Words | `SpellcheckWordIterator` (Latin rules): letters of the language's script plus 0-9, chained through one MidLetter / MidNumLet / U+0027 between letters, NFKC; `IsValidContraction` accepts "hello.world" when both halves are words; > 64 bytes is correct, > 24 bytes gets no suggestions; several languages: correct if any says so | `spell.Tokenizer`, `Service.Lookup` |
| When | `IdleSpellCheckController`: a hot check after each change or caret move (the whole field ≤ 128 chars, the paragraph ≤ 1024, else 1024 around the caret), skipped while typing inside a word (that word's marker is removed); a cold check 1 s later: the whole field first, then ±64 around the caret, the whole field again after 1024 chars of change. Only the focused field | `composer/spellcheck.go` (`spellHot`, `spellCold`); answers come from a worker goroutine with a per-word cache, frames never wait |
| Marker | `text_decoration_info.cc` / `decoration_line_painter.cc`: non-Apple, a 1px stroke of cubic waves (wavelength 6, control points ±5, phase −¾ wavelength) in (255, 0, 0), at ascent + max(1, ⌈t/2⌉) + t + 1 + ½ below the top; Apple, a dotted 2px line (drawn as 2×2 dashes, 2 apart) in (255, 59, 48, 191), WebKit's colour | `paintSpellWave`, `paintSpellDots` (scaled by the zoom) |
| Right-click | Blink moves the caret outside the selection, selects the misspelling under the pointer (and on macOS the word). The shell's `buildMenuForTextInput`: suggestions (at most 5, `kMaxSuggestions`, minus Slack's 71-word offensive list `mf`), a separator, **Add to Dictionary**; with a selection **Search with Google** and a separator; **Cut**, **Copy**, **Paste** (enabled by editFlags). No "Ignore". `replaceMisspelling`, `addWordToSpellCheckerDictionary` | `Editor.contextMenu`; the menu opens when the suggestions arrive (≤ 300 ms); Add to Dictionary writes Chromium's `Custom Dictionary.txt` (with its `checksum_v1` line) in the config folder and the system dictionary on macOS / Windows |
| Engine | Electron: macOS NSSpellChecker (`checkSpellingOfString` with language nil, `guessesForWordRange`, `learnWord`); Windows Chromium's hybrid checker (ISpellChecker `ComprehensiveCheck` for languages Windows supports, Hunspell for the rest); Linux Hunspell. Slack sets the dictionary URL to `https://spellcheck.slack-edge.com/dictionaries_v2/` (gov/mil builds use their own); 5 retries of a failed download | `internal/spell`: cgo NSSpellChecker; ISpellChecker over COM (no cgo); a pure-Go Hunspell (`internal/spell/hunspell`) reading Chromium's `.bdic` files, downloaded from Slack's URL into `<user cache>/clack/Dictionaries` |
| Languages | `SpellcheckPrefs` → `renderSpellcheckLanguagesV3`: `isLanguageSelectionEnabled` is `!darwin`, so macOS shows "We spellcheck the languages specified in your system settings. You can find those under System Preferences > Keyboard > Text."; Windows and Linux a large full-width multi-select of `getAvailableLanguages` (the shell's 56 labels, `SM`, sorted by label), greyed while spellcheck is off, writing `setCurrentLanguages`, then "Choose the languages you’d like Slack to spellcheck as you type." Default: the OS locale's language | `prefs_spell.go`; the choice is per device (ui.json `spellcheck_languages`) like Electron's session |

Still different: the menu is drawn in Clack's menu style, like its other
shell menus (Copy Link, Copy this code), not as the OS's native menu
Electron pops up; macOS and Windows check word by word through Chromium's
iterator where Chromium hands them whole paragraphs; the Hunspell
implementation is new code, so its suggestion order can differ from
Hunspell's on some words; Inspect Element and Exit Full Screen (dev
builds, full screen) aren't in the menu; the language picker lists its
matches under the field rather than in slack-kit's dropdown.

Tests: `TestCheckAffixes`, `TestSuggest`, `TestBDicEnUS` (internal/spell/hunspell,
the last on Chromium's en-US dictionary in CI); `TestWords`,
`TestServiceCachesAndNotifies`, `TestServiceTwoLanguages`,
`TestAddToDictionaryPersists`, `TestFilterSuggestions`, `TestPlatformBackend`
(Hunspell on Linux, NSSpellChecker on the macOS runner, the Windows checker
on the Windows runner), `TestWindowsChecker`; `TestSpellcheckWhileTyping`,
`TestSpellcheckSkips`, `TestSpellMarkersFollowEdits`, `TestSpellContextMenu`,
`TestSpellcheckPrefOff`, `TestSpellcheckNeverBlocksFrame` (a 50 ms-per-word
engine), `TestSpellcheckShots` (the spellcheck workflow's
`spellcheck-shots` artifact: `spellcheck-{light,dark}-{wave,mac}.png` and
`spellcheck-menu-*.png`).

## Font (2026-11-09)

Appearance > Font is real. Read from Slack's web client (build a84662d2d135
on the public CDN `a.slack-edge.com/bv1-13-br`, fetched without a cookie;
the real account's font was never changed):

| Rule | Slack's code | Clack |
| --- | --- | --- |
| Options and labels | client-boot `wj/S` (label, value, fontFamily per font) and `ofdg` getAllFontOptions: Lato (Default), OpenDyslexic, Arial, Roboto Mono, Comic Sans, Georgia, Noto Sans, Verdana, Atkinson Hyperlegible Next, then "{font} (System)" | `uifont.Options` |
| Order | prefs-dialog ThemeFontTypefaceContainer passes them through `ofdg`.tw, a sort by label: Arial, Atkinson Hyperlegible Next, Comic Sans, Georgia, Lato (Default), Noto Sans, OpenDyslexic, Roboto Mono, [system], Verdana | same |
| System font | `wj/S`.qL: San Francisco Pro on macOS, Segoe UI on Windows, Ubuntu only when `+oeN`.OL isUbuntu() (navigator.userAgent contains "Ubuntu"; the desktop app's Electron UA doesn't), else no option. Its fontFamily is `-apple-system, BlinkMacSystemFont, "Segoe UI", "Ubuntu", "Noto Sans", Helvetica, Arial, sans-serif, …emoji` | 10 options on macOS and Windows, 9 on Linux |
| The select | a 250px slack-kit Select under a "Font" header, each option rendered in its own fontFamily (renderItem), then an `hr`. Hidden for ko-KR / zh-CN / zh-TW (`ofdg`.xu) | `selectBoxW` 250dp; the menu draws each label in its font once the previews have loaded (they load when the menu opens, as the browser loads @font-face files on first use) |
| Storage | setPersistentCustomTypeface: localConfig `client-typeface` (`4T1V`.C3) holds the whole option object; the client theme store (`G4CN`) reads it at boot. Per device, never users.prefs | ui.json `client_typeface` with the same `{label, value, fontFamily, default}` object; looked up by value on load, so a stored "system-font" gets this OS's label |
| Where it applies | boot-shared Theme: `:root{--font-family-default: <fontFamily>}` and `body.use-theme-font` when the label isn't Lato's (`UflI`.oo); slack-kit `body{font-family:var(--font-family-default)}` and `.use-theme-font{font-family:var(--font-family-default)}`. The whole UI: sidebar, header, messages, composer, menus, dialogs, huddle window (its body gets the class too) | the theme's UI family (`theme.Typeface`): every `theme.Font(w)` draws in it. Huddle windows opened later follow it (`theme.SetDefaultUIFont`) |
| Where it doesn't | code, pre and the composer's code blocks keep `--font-family-monospace` ("Slack-Roboto-Mono", Monaco, Menlo, Consolas, "Courier New", monospace); icons are the Slack v2 icon font; emoji are images; a few onboarding titles hard-code Slack-Lato | `theme.MonoTypeface` and the emoji slot face are untouched |
| Weights | Theme sets `--custom-font-weight-bold:600; --custom-font-weight-black:700` for San Francisco Pro (System), Segoe UI (System), Roboto Mono, Noto Sans, Meiryo, and Atkinson Hyperlegible Next. Bold and black text uses those variables (2,591 rules against ~50 literal 700/900) | `uifont.Weights`: Clack's Bold and Black slots get the 600 / 700 faces for those fonts, 700 / 900 for the others |
| Web fonts | slack-kit-gantry-v2-styles @font-face, `src:local(☺)` first (an installed copy is never used): Slack-Roboto-Mono `RobotoMono-Regular-6ddf6fb.woff2` / `-Italic-7937489` (weight 100–700, variable) plus Cyrillic and box-drawing companions; Open Dyslexic as eight OpenType files (400 / 700, italic, Cyrillic); Noto Sans `NotoSans-Regular-b20b6ba.woff2` / `-Italic-22a8c66` (100–700); Atkinson Hyperlegible Next `AtkinsonHyperlegibleNext-3795751.woff2` (200–800, no italic) | fetched from `bv1-13-br/` with a plain GET (no cookie, no token), kept in `<user cache>/clack/fonts`; woff2 decoded by `internal/woff2` (andybalholm/brotli); variable faces instanced at the slot's weight, clamped to the @font-face range |
| System fonts | Arial, Georgia, Verdana and "Comic Sans MS", cursive, sans-serif have no @font-face: the computer's | go-text fontscan over the OS font directories (macOS /System/Library/Fonts, /Library/Fonts, ~/Library/Fonts; Windows %WINDIR%\Fonts and %LOCALAPPDATA%\Microsoft\Windows\Fonts; Linux the XDG and fontconfig directories); -apple-system is /System/Library/Fonts/SFNS.ttf |
| Missing fonts | Chrome tries the next family of the list; on Linux it accepts only metric-compatible substitutes (Skia SkFontConfigInterface_direct kFontEquivMap: Arimo / Liberation Sans for Arial, Tinos / Liberation Serif for Times New Roman); generics are Chromium's defaults (locale_settings_*.grd, copied by Electron's font_defaults.cc: sans-serif Helvetica on macOS, Arial elsewhere; cursive Apple Chancery / Comic Sans MS; standard Times / Times New Roman); a list that runs out ends at the standard font, then the last resort (FontFallbackList::GetFontData) | the same chain (`uifont.Loader.Load`), then fontconfig's sans-serif preferences on Linux, then the embedded Lato. A CDN font that can't be fetched counts as missing |
| Matching | CSS Fonts 4 font matching within the family; no italic face means Chrome slants the upright one | the same weight and style rules per slot. Gio can't slant, so a font with an `ital` axis (Atkinson) uses it and one without stays upright |

The switch: picking a font saves it, then a goroutine loads the files (cache
or CDN, system files from disk), builds a shaper and posts it; the UI
goroutine installs it in one step (`Theme.SetUIShaper`: both mode shapers,
`theme.FontGen` and `Theme.Gen` bumped once). Every cache of laid-out text
starts over in that frame: richview styles and views (keyed by shaper),
the composer's text layout and shaped-run cache and its chrome space widths,
the prefs text styles, Block Kit text heights, the message list scrollbar's
remembered heights, and the CSS strut baseline (`theme.UIExtents`, the UI
face's hhea metrics instead of Lato's constants). At startup a saved font
whose files are all on this computer is built before the first frame; one
that still needs a download starts in Lato and switches when it arrives.

Letter spacing (gap-w16, 2026-11-12): Slack's `--custom-font-tracking-*`
for the system fonts, Roboto Mono, Noto Sans and Meiryo are applied, see
"Font tracking" below. Still different: Chrome's
synthetic oblique and synthetic bold aren't drawn; a unicode-range companion
(OpenDyslexic's Cyrillic file) only fills in what the main face lacks, while
Chrome prefers it inside its range; SF's optical size is fixed at 15px (Chrome sets
`opsz` per element size); the "/slackfont" custom font (its option only
shows when one is set) isn't ported.

Tests: `TestDecodeSlackRobotoMono`, `TestDecodeHmtxTransform`,
`TestDecodeRejectsGarbage` (internal/woff2: every glyph outline and advance
of Slack's file against fontTools' decoding, at the default instance and at
wght 700); `TestOptionsSlackOrder`, `TestWeights`, `TestSystemFontFallback`,
`TestSystemFontWindows`, `TestCDNFontsFetchCacheAndInstance`,
`TestMatchCSS` (internal/ui/uifont); `TestFontPrefRoundTrip`,
`TestFontPickLatestWins` (internal/ui: the select, the switch, ui.json,
the next start); `TestFontSwitchRelayout` (messages: one frame after the
switch every row matches a fresh row in the new font, the scrollbar
forgets old heights, nothing moves after). Screenshots: `TestFontShots`
with the `font-shots` workflow (a channel in every option, light, and the
open menu; `fonts.txt` lists what each option resolved to on the runner).

### Font tracking (gap-w16, 2026-11-12)

From CDN build d3de3a487b1c (fetched without cookies) and the CSS Slack
serves with it (client-boot-styles, the webapp's application CSS,
slack-kit-gantry-v2-styles, rollup-style-plastic).

| What | Slack | Clack |
| --- | --- | --- |
| Which fonts | boot-shared `oWug` Theme: `M = S \|\| T \|\| I \|\| j \|\| E`, the labels San Francisco Pro (System), Segoe UI (System), Roboto Mono (`wj/S`.RR), Noto Sans (`.Cp`) and value `meiryo`; Atkinson Hyperlegible Next (`.gw`) gets only the weights | `uifont.Tracks`; `theme.UIFont.Tracking` |
| Values | on `.use-theme-font`: headline -0.36pt, title -0.43pt, subtitle -0.15pt, large-body -0.15pt, body 0.06pt, caption 0.12pt, micro 0.17pt; unset otherwise, so `var(…, inherit)` is normal | `theme/tracking.go` |
| Where | `letter-spacing: var(--custom-font-tracking-X, inherit)` beside each type size: caption 13px (972 rules), body 15px (671, plus slack-kit's `body`), subtitle 18px (241), micro 12px (146), title 22px (120), headline 28px (41), large-body 18px (22); Slack Kit's `--dt_static_type-size-*` / `-tracking-*` agree. Other sizes inherit, mostly body's; code blocks set none | `theme.TrackingFor(font, size)`: the size's variable, body's off the scale and for monospace runs; none for the emoji slot (Slack's emoji are images) |
| How Chrome spaces | Blink `ShapeResult::ApplySpacingOrExpansion`: the spacing goes after each grapheme cluster's last glyph; `ShapeResultSpacing::ComputeSpacing` skips `TreatAsZeroWidthSpace` (CR, FF, U+FFFC, ZWNJ, ZWJ, default ignorables) and cursive scripts' non-space characters; `font_features.cc` turns liga, clig and calt off when letter-spacing isn't 0 | Gio patch 12 (`third_party/gioui.org/CLACK_PATCHES.md`): the same, in the shaper before wrapping, so measuring, carets and drawing agree |
| Who gets it | everything under body | every text the theme's painters, Gio's Label and Editor, richview and the composer shape; slackc (Clack's CSS engine for Slack-styled views) gets the variables at `:root` and spaces through its own letter-spacing |

Not done: when a tracked font can't load and Clack falls back to the
embedded Lato, Slack would still track the fallback; Clack draws plain
Lato.

Tests: `TestTrackingForSize`, `TestLetterSpacingShaper` (Blink's rules:
the per-cluster spacing, ZWSP, ligatures off, PxPerSp and the shaper's
scale, an explicit spacing), `TestTrackedLabelsAndEditors` (the painters,
`widget.Label`, `widget.Editor`'s caret) and `TestSetTrackingFollowsUIFont`
(internal/ui/theme). Shots: `font-shots` adds
`font-tracking-{notosans,robotomono}-{light,dark}.png` and the same font
untracked (`font-untracked-…`) to compare.

## Connected accounts' Manage (gap-w16, 2026-11-12)

From CDN build d3de3a487b1c, boot-shared modules:

| Part | Slack | Clack |
| --- | --- | --- |
| Row | `d40N` ConnectedWorkflowAccount → `BcGa` ConnectedWorkflowAccountAppRow: 32px app icon (12px gap), the name (h3, 15px bold in `.p-prefs_dialog`) and a check-circle-filled in content-hgl-2 ("Connected"), an outline medium "Manage" (aria "Manage {appName}"), rows 8px apart | `prefs_connected.go` connectedRow |
| Modal | `H7Jb` ConnectedWorkflowAccountManageModal, opened as `manage_accounts_modal` (stacked). From Preferences only the app id is passed. Title bar: the 32px icon in its 40px box, 16px gap, the app's name (padding 16px 28px). Per provider (`lsX5`.iN, in the response's order): "Connected {providerName} Accounts" (h3 18px black, 20px under), rows 16px apart: external_user_id (h4, ellipsized) with the check; a domain-wide token (`KYUQ`: dwa_user, dwa_app) says "Added by your administrator" and has no Disconnect; the others an outline medium Disconnect 16px right. Domain-wide rows, then sections with one, first; empty providers dropped; none: "No accounts found". Footer: Done | openManageAccounts, manageSections |
| Disconnect | `j8Vt` ConnectedWorkflowAccountConfirmationModal on `iIWJ` ConfirmationModal: the title centered (the 64px app icon 64px down, 20px gap, "Are you sure you want to disconnect this {providerName} account?", 8px gap, the account at 50%), WorkflowAlert (a default boxed c-alert without its icon: base-ter, otl-sec border in dark; "What will change:" in bold, three disable-icon items 12px apart, the 12px note at 50%), Cancel and a danger LoadingButton "Disconnect" (`oG6i`: the label hidden, a 16px spinner, disabled) | confirmDisconnect; the disable icon in `icons/redrawn.go` |
| Delete | `F/EF` deleteUserAuth: `apps.auth.external.delete` (external_token_id, reason delete-user-auth), then resetAppAuths and fetchUserAuth; a failure shows "Something went wrong. Please try again." in an error alert and keeps the confirmation open; success closes it, and the manage modal too when that was its only account (no domain-wide one) | `core.DeleteUserAuth`; reloadConnected |
| Data | apps.auth.external.userAuths.get `apps[id].providers[key] = {provider_name, valid_tokens: [{external_token_id, external_user_id, auth_model}]}` | `core.WorkflowAppAuth.Providers` (key order kept) |

Not ported: the search-connector, GBP ("Disable"), multi-auth ("Connect
Another Account"), disconnect-URL and misconfigured-connector variants of
the same modal, which Preferences never opens.

Tests: `TestPrefsConnectedManage` (the row, the modal's text, Disconnect,
a failed and then a good delete, the reload), `TestPrefsManageSections`
(internal/ui, fakeslack hooks). Shots: `prefs-connected-manage-{mode}`,
`prefs-connected-disconnect-{mode}`, and the page itself in
`prefs-connected_accounts-{mode}`.

## Send Verification Email (gap-w16, 2026-11-12)

From CDN build d3de3a487b1c:

| Part | Slack | Clack |
| --- | --- | --- |
| Section | prefs-dialog BlockedInvitesUnverifiedEmailSection: a foreground_min_solid box (radius 8, padding 24px 64px, centered): 👋 (8px under), "To see whose invitations you’ve blocked, you’ll need to verify your email address{memberEmail}." (16px under), a primary "Send Verification Email" | `prefs_verify_email.go` verifySection |
| Request | boot-shared `3+f0` useEmailVerificationHandlers.handleVerifyEmail: one request at a time, `signup.confirmEmail` (vendors `knqT`) with the member's email (left out when there's none), the client locale, reason request-new-email-verification-code; ok opens VerifyEmailModal (`verify-email-modal`, stacked); a failure shows nothing on this page | `core.RequestEmailCode` |
| Modal | `3+f0` VerifyEmailModal: "Enter the code we sent you", "We’ve sent a 6-character code to **{email}**. The code expires shortly, so please enter it soon.", `LJSk` SplitInput, `mFn7` CodeCheckStatus, Cancel (disabled, and closing refused, while checking) | verifyEmail.open, body |
| Split input | six cells, two groups of three around an em dash (`confirmation_code_span_cell`, margin 8px); `isNumbersOnly` false: letters and digits, uppercased; typing moves right, ← → move, Backspace in an empty cell moves left, a paste fills from the focused cell and focuses the next; a full code submits (componentDidUpdate). Desktop rules ((width>=1024px) and (hover:hover) and (pointer:fine)): the cells joined in a group with 4px outer corners, 92px tall, 50px/56 text; focused, a highlight border and glow | verifyEmail.split, cell, update, paste |
| Check | `signin.confirmCode` (vendors `wIr1`; code, email, reason "verify email modal code confirmation"). Errors (`i0DY`, `wIr1`.Mv): failed → invalid_code → "That code wasn't valid. Give it another go!"; ratelimited; expired → "That code’s no longer valid. <a>Request a new code</a> and try again." (the link requests one); ratelimited_blocked → "Temporarily blocked". The cells clear and the first takes the focus | `core.ConfirmEmailCode`; confirmed, statusLine |
| Success | "Your email has been verified", "You can now manage the Slack Connect invitations you’ve blocked.", Got It; `5kqq` isEmailVerified, so the page lists the blocked invitations | emailVerified |

Tests: `TestPrefsVerifyEmailFlow` (the enabled button and copy, the
request's form, the modal, typing through a real input.Router: uppercase,
a rejected dash, Backspace and →, Ctrl+V and a paste that submits, the
check's form, an invalid code clearing the cells, success and the blocked
list) and `TestPrefsVerifyEmailErrors` (internal/ui). Shots:
`prefs-verify-email-{,typing-,invalid-,expired-,ratelimited-,checking-,verified-}{mode}`,
`prefs-privacy-after-verify-{mode}`, and the section in
`prefs-privacy_visibility-{mode}`. All against fakeslack: no email was
sent, no account setting changed.

## Compact (messages_theme dense, 2026-11-09)

From the web client (build a84662d2d135; boot-shared and client-boot
modules named below) and client-boot-styles. The selector is client-boot
l4xu: `"dense" === messages_theme ? "dense" : "light"`.

| Part | Slack (module / CSS) | Clack |
| --- | --- | --- |
| Pref | MessageThemePref: radios messages_theme_set, Clean = light_with_avatars (checked for it and "default"), Compact = dense, setUserPrefByApi | prefs_pages.go writes the same; the list follows uipref Dense live |
| Examples | CompactMessageExample (time gutter, sender margin-left -8px, then the text) / MessageWithAvatarExample 4fBz (36px avatar, margin 0 10 0 5, sender + time, br, text) in .p-prefs_dialog__message_example_pillow; same under Names (Meg / Megan Anne, ":wave: Hi, I’m @Meg") | prefs_theme.go |
| Channel gutter | Message NnKv getCompactGutter for every row; .p-message_pane_message__compact_timestamp width 56px, margin -4px 0, right-aligned, meridiem on (`!(adjacent && light)`), --adjacent opacity 0 until hovered; MessageKitGutter R86e adds c-message_kit__gutter--compact | the 56px time box at 20; a group's first row always shows "9:02 AM", grouped rows on hover |
| Body | getCompactMessageBody: MessageIndent lFHZ (.c-message_kit__indent padding-left 8px) around BaseBroadcastPreamble {sender (.p-message_pane_message__sender--compact margin-left -8px), "\u00a0"} and the blocks; no header timestamp, no `<br>` | body at 92, sender at 84 on the text's first line (richview SetLead); one-line rows are 30px whether or not they start a group |
| Inline text | CompactBlocks GVID: inline only when every block is rich_text; .p-rich_text_block--inline / .p-rich_text_section inline, lists, quotes and code stay blocks | inlineBody: the first block a paragraph and msg.blocks all rich_text (or none); otherwise the sender has its own line |
| Grouping | isAdjacentMessage (boot-shared) doesn't read the theme; ThreadReply passes `adjacent && "light"` | channel groups as in Clean; thread replies never group |
| Thread pane | ThreadReply / ThreadRoot pass isCompact only for the Threads view, so MessageKitGutter draws only gutter__right | no avatar column: the column at 16, sender and time line kept |
| Toolbar | Message: position `w \|\| a ? "above" : "default"` | top -28 on every Compact channel row |
| Labels | MessageLabels meLQ c-message_kit__labels--dense; .c-message_kit__labels--dense.p-message_pane_message__message_label .c-message_kit__labels__icon width 56px | pin/eye icon right edge at 76, text at 84 |
| Broadcast | c-message_kit__broadcast_preamble--dense (display flex): sender, space, "replied to a thread" on one line, text below | same |
| Status emoji | .c-message_kit__sender--dense .c-custom_status border 1px rgba(fg_low,.13), radius 5, padding 1px 2px | drawn around the status emoji / huddle icon |
| System messages | Message: compact gutter (the time) instead of the icon; SystemMessage drops its `<time>` and uses containerCompact (margin-top 2px, padding 0 0 4px) | denseSystemLayout |
| Tombstone | MessageTombstone 2X8J: no icon under dense, c-message_kit__tombstone--compact, text --compact (height auto, margin-bottom 4px) | denseTombstoneLayout: text at the row padding, 4px from the top |
| Jumbomoji | client-boot Y9zX: jumbomoji && theme != dense | off under Compact (the prefs row already greys) |

Not changed: Clack's huddle card is MessageAttachmentHuddleLink, which has
no dense branch (the "small" join button belongs to HuddleMsgJoinButton,
the newer huddle message Clack doesn't draw); AttachmentBlocks' inline
rich_text inside attachments under dense (+Yt1) isn't ported; the Threads,
Activity and Unreads views draw their own rows. The compact-shots workflow
runs the geometry, idle/fling/scroll-gap and plugin tests in both layouts and
uploads Clack next to Slack's markup under Slack's CSS.

**Row heights against Chrome (2026-11-10).** compact_ref.py also measures
every row of Slack's markup in Chrome (light, both layouts, the channel at
1100px and the thread pane at 520px) and fails when Clack's row is more than
1px off, or when the checked-in table
`internal/ui/messages/testdata/slack-row-heights.json` (which
TestRowHeightsMatchSlack holds Clack to in ci) is stale. Fixed on the way,
from boot-shared and client-boot-styles:

| Row | Slack | Was | Now |
| --- | --- | --- | --- |
| System message (Clean) | SystemMessage I0jQ: .textRow__DEPF9 13px/1.5 in both layouts, the <time> (12px) on its baseline, .systemIcon__mZbDL a 36px box with the 16px glyph centred, padding-top 2 | 15px text, 38px | 13px, 36 (Chrome 35.5) |
| Tombstone (Clean) | 2X8J: .c-message_kit__tombstone__right margin -4px 0, __text min-height 44px flex align-items center, the reply bar under it (margin -4px 0 4px, 34px) | 80 | 86 |
| Block Kit section | iGDn adds c-message_kit__blocks--rich_text (margin-bottom 4px) only when the last block is rich_text; the section's own 4px (p-section_block 8px 0 4px, top 0 first) ends the body | 56 in both layouts | 52 |
| Reactions | ReactionBar Ltiy: div > span > inline-flex pills (24px, margin-bottom 4px, vertical-align top) = 28px lines, then c-message_kit__reaction_bar margin-bottom 4px | 4px short | 32 after the text |
| Broadcast (Clean) | ThreadBroadcastPreamble UsKL gets className c-message_kit__broadcast_preamble__inner (margin 0, so no 8px 0 -6px); the reference page had left it off, so the 76 it measured was its own | 74 | 74; the preamble now reads "replied to a thread:" and the root's one-line preview in the link when the root has text (canRenderSimpleMessagePreview), "replied to a thread" otherwise |

Activity: Gjwd _F gives invitations `paper-plane`, Slack's outline paper
plane; Clack aliased that name to its filled `send`, now to `drafts` (the
glyph TestIconsMatchSlack holds to Slack's paper-plane).

## Unreads view and channel join banner (2026-11-09)

From Slack's web client on the public CDN (build bv1-13): `page-unreads-view` (LlSi), the shared chunk `…unreads-view_catchup_hooks_use-catchup-activity-feed_ts-js_modern-28a78c` (2cdE UnreadsView / UnreadsList, F9zO Header, Ard6 ShowNewerButton, H6rY footer, JhsS EmptyState, gvf3 collapse, 2cdE sorts), `client-boot-render` (AllUnreadsHeader, the sort and filter menus, RefreshUnreadMessagesButton, the Unreads page's sidebar config), `client-channel-list-view` (bG, the sidebar's page links), `client-boot` (LbeF markLastRead / markManyLastRead, QJKZ fetchHistory, VllE, aK5v, 82Kz, NznF), `modern.vendor` / `gantry-v2-vendors` (megaphone XUhT, the fetchers), `megaphone-message-pane-input-channel-join-banner` (7BTH) with its shared button chunk (mzyA, QwCw) and both chunks' CSS. Nothing was opened in real Slack: opening Unreads marks things read, and the banner needs a fresh join.

**Unreads** (`internal/ui/views/unreads.go`, `unreads_model.go`; `core/unreads.go`):

| Element | Slack | Clack |
| --- | --- | --- |
| Sidebar link | bG: first page link (before Threads, Huddles, Drafts & sent, Saved) when `enable_unread_view === true` (shouldShowAllUnreadsLink); UnreadsSidebarIcon `mark-as-unread`; hasUnreadsPageUnreads makes it bold (totalUnsuppressedUnreadsInChannels > 0); no badge outside the catch-up experiment (getUnreadsPageBadgeCount) | the same (sidebar.go `sbLinks`, `model.unsuppressedUnreads`) |
| Which conversations | the unread counts' conversations the model counts (shouldCountUnreadsInModel: not muted, a member, not archived), with a last read | `Unreads.counted` |
| Order | getAllUnreadsSortOrder: a valid `all_unreads_sort_order`, else "priority" on a paid team, "sidebar" otherwise. sortScientifically (starred, then channels with mentions, by priority, then DMs, then group DMs), sortAlphabetically, sortNewest, sortOldest, sortSidebar | `unrSort` (TestUnreadsSortOrders) |
| Header | "Unreads" (18px 900), the section filter ("All of {org}" with the 16px team icon on an Enterprise org, radios: All, then the sidebar's sections; `all_unreads_section_filter`), the sort menu (Sorted like your sidebar / alphabetically / by recommended order / newest to oldest / oldest to newest; checkbox rows; `all_unreads_sort_order`), RefreshUnreadMessagesButton ("N new messages", sync icon, label hidden under 1070px) | the same |
| Frame header | 48px on foreground_min_solid, padding 0 32 0 16: the caret (20x28), the conversation's name (900, opens it), "N messages" / "N+ messages" (13px), "Press Esc to" on the selected header, small outline "Mark as Read". Marked: check, "N messages marked read", Undo. Collapsed or only newer: "New messages" / "N new messages" with the circle-fill dot | the same |
| Rows | getRowsAndKeys: 20px top spacer, or day dividers when the first unread message isn't today; the unread divider at the read cursor; the channel's message rows (grouped like the channel list); "Loading more messages…" (80px) while a frame isn't fetched, and the list stops there; "N new messages" (ShowNewerButton) or a 20px spacer with a bottom line; footer "Mark All Messages Read" (outline, 32px padding) and, with a section filter, "Jump to the {section} section" | the same; the frame header sticks to the top while its messages scroll |
| Fetching | fetchHistory: conversations.history channel, limit 28, oldest (the last read, then the slice end), inclusive, include_pin_count, no_user_profile, include_free_team_extra_messages; a page loads as the list nears its end (loadPost); a frame that ends up empty is removed | `core.UnreadHistory` |
| Marking | Mark as Read and Esc: conversations.mark at the frame's end (markLastRead, immediate); Undo: back to the initial cursor. Mark All Messages Read: unread.markRead markers={channel: ts}; its Undo the same with the initial cursors | `Mark`, `core.MarkMarkers` |
| Collapse | unread_collapsed_channels updated locally (setUserPref), unread.collapse / unread.expand {channel}; expanding shows up to the latest message | `core.SetUnreadCollapsed` |
| Empty state | one of Slack's 22 en-US line pairs with its emoji (preferred skin tone on hands; the octopus/oyster swap on click), "N New Messages" (primary) with "Enter ↵ to show" ("Return" on macOS) a second after new unreads, "You marked all your conversations as read. Undo"; with a section filter "You're caught up on {section}!", Show All Unreads, Jump to the next section | the same |
| Keys | Esc (global on the page): mark the selected (keyboard) or topmost frame read and move to the next header. R on a header, ←/→ collapse/expand (→ on a message opens its thread), Shift+PageUp/PageDown between headers, Enter on the footer (keyup) | the same; ↑/↓ move the keyboard row |
| Realtime | a new message in a frame's channel shows under ShowNewerButton; in another counted conversation it adds to NewMessagesUpdater's count (the refresh button, the empty state's button); a sort or filter change rebuilds the frames | the same |

Not ported: the catch-up experience (unified_global_triage: For you / Everything else tabs, `stacked-cards` icon, a badge), the card appearance, the XWS education card, the Activity inbox's channel-unreads variant, and the right-click menu's `unreads_view` container type (Clack's menu uses the channel's).

**Channel join banner** (`internal/ui/joinbanner.go`):

| Element | Slack | Clack |
| --- | --- | --- |
| When | Opening a channel (not a DM, group DM, preview, agent record channel or meeting channel) requests megaphone spaces: megaphone.notifications.list event={type: request_spaces, spaces: [message_input, message_input_invited_glow, message_input_channel_join_summary_glow]}, channel_id. It shows while message_input's notification is education_channel_join_banner | `core.MegaphoneSpaces` |
| Pref | isAutoSortRolloutActive (suggested_sections_auto_sort_v2 on, its killswitch off): hidden when disable_channel_join_banner is true; otherwise hidden unless channel_join_experience_enabled is true. The live dialog shows "Show suggestions for getting started in new channels", which Slack draws only under the rollout, so the experiment defaults on | `joinBannerHidden` |
| Card | 1px otl-ter, base-sec, radius 12, margin 8 20 0, padding 20; close (16px, content-sec) at top 20 right 40; ":wave: Welcome to #name" 15px 900 (margin-right 48); "Group this channel with similar conversations in your sidebar and choose when you want to be notified about new messages." 13px/18px | the same |
| Buttons | 28px outline, padding 0 8, 13px, 4px apart: the section selector ("★ Move to Section" in no section, else the section's emoji or icon and name) with a chevron, opening Move to…; NotificationsControl (its level's icon: notifications / notifications-all-new-posts / notifications-off; "All new posts" / "Just mentions" / "Nothing" / "Muted"; chevron) opening ChannelNotificationPrefMenu above | the same; Slack's notifications-all-new-posts glyph added to the icon set |
| Dismissal | the close button or Delete on the banner: slide down (.3s), megaphone.setNotificationAsSeen notification=education_channel_join_banner action=dismiss channel_id. A section or notification change made elsewhere closes it unrecorded. Leaving the channel records an impression and requests message_input again | the same |

Not ported: the auto-sort variant (needs the server's auto-sort placements), the Slackbot AI variants (ChannelJoinInvitedBanner, ChannelJoinSummaryBanner, ChannelJoinReasonBanner) and the agents line (agent_channel_welcome).

Tests: `TestUnreadsFramesLikeSlack`, `TestUnreadsMarkRead`, `TestUnreadsRealtime`, `TestUnreadsCollapse`, `TestUnreadsSortMenu`, `TestUnreadsSortOrders`, `TestUnreadsEmptyCopy` (internal/ui/views), `TestUnreadsSidebarLink`, `TestJoinBanner`, `TestJoinBannerPrefGate` (internal/ui), on fakeslack's `Size.Unreads` fixtures (internal/fakeslack/unreads.go). The `unreads-shots` workflow saves light and dark screenshots (`unreads-shots` artifact).

## Audio & video (2026-11-10)

Read from Slack's web client on the public CDN (bv1-13 build a84662d2d135, fetched without cookies: `gantry-v2-async-client-boot-shared`, `client-boot`, `gantry-v2-async-client-boot-render`, `client-boot-styles`) and the Linux desktop app's `main.bundle.cjs`. No huddle was started or joined: everything runs against the fake Chime.

| Row | Slack | Clack |
|---|---|---|
| Enable automatic gain control / Enable noise suppression (`agc`, `ns`) | Device prefs, not users.prefs: the A/V prefs component keeps `prefs.audio` in localStorage (`{enableAGC: true, enableNoiseSuppression: false}` by default; `toggleAGC`, `toggleNoiseSuppression`). The microphone opens with `getHuddleMicrophoneConstraints(enableAGC, ns)` (client-boot `8Di1`): `echoCancellation: {ideal: true}`, `autoGainControl: {ideal: enableAGC}` and, with noise suppression on, `noiseSuppression: {ideal: false}`. The browser's suppressor goes off because the Chime SDK's Voice Focus (`voiceFocusManager`, a proprietary Amazon WebAssembly model) takes over. Chromium's autoGainControl is WebRTC's AGC2. | ui.json `huddle_agc` (unset is on) and `huddle_noise_suppression`. `internal/huddle/apm.go` runs on the send goroutine before the Opus encoder, on every captured 10 ms frame. Noise suppression is RNNoise (`third_party/rnnoise-go`, pure Go, BSD-3), the open model closest to Voice Focus. Gain control ports AGC2's adaptive digital controller: speech level from runs of ≥12 voiced frames, the 12–25 dB saturation margin, 5 dB headroom, at most 50 dB of gain, starting at 15 dB, slewing 6 dB/s, the noise floor held under -50 dBFS, then a limiter. RNNoise's voice probability is its VAD, paired with the frame it describes (20 ms back). A change applies mid-huddle (`Session.SetMicProcessing`). Not ported: echo cancellation, and AGC1's OS input-volume control. |
| Show audio and video preview (`huddles_show_join_preview`) | `HuddleJoinPreviewPref` (unset is off: `ty(e, aI) \|\| !1`). `startHuddleWithConfirmation` → `computeShouldShowJoinPreview`: never from the invite window, only with the pref on, only for another huddle than yours. `showHuddlesJoinPreview` opens the invite-with-preview window (`HuddleInviteWindowWithPreviewContent`, CSS module `qki8`) before `startHuddle` runs with `fromJoinPreview`. The window is 524×760, "Slack - Huddle Preview". | `huddleui/preview.go`: the same gate after the 150-member warning; a 524×760 "Clack - Huddle Preview" window laid out from `qki8`/`tewJ`. The title bar reads "Start huddle in **#ch**", "Join huddle in **#ch**. **X** is already there." or "Start huddle with **Name**". The square video (mirrored camera, or your avatar) has the mic and camera toggles; talking lights a white border and the mic pill (`huddle.Preview` level > 0). The microphone, speaker and camera pickers list miniaudio's devices; a pick is used by later sessions and previews (`huddle.SelectAudioDevices`). Cancel, and Start Huddle / Join, which starts with the preview's mic and camera state. |
| Allow keyboard shortcut to mute (`huddles_global_mute`) | `HuddleBackgroundPref`, desktop only, "Use ⌘/Ctrl+Shift+Space to mute or unmute your mic." Main process: `toggle_mute` → Electron `globalShortcut` "CommandOrControl+Shift+Space"; when no Slack window has focus it shows and focuses the main window. Renderer: `globalMuteShortcut.enable` when the huddle is STARTED and the pref is on, disable on leaving; a press runs `toggleSelfMicrophone`. | `internal/ui/globalkey`: Carbon `RegisterEventHotKey` (Cmd+Shift+Space) on macOS, `RegisterHotKey` (Ctrl+Shift+Space, MOD_NOREPEAT) on Windows, `XGrabKey` on the X root window with every Caps/Num Lock combination on Linux. Registered while you're in a huddle with the pref on; a press toggles your mic and raises the main window. Wayland has no API for an app to grab keys outside its own windows (Electron's globalShortcut doesn't work there either), so under a Wayland session it returns `ErrWayland` and logs it. |
| Automatically leave huddles (`huddles_disconnect_on_lock`) | `HuddleLockPref`, desktop only (unset is off). `HuddleLocker` (boot-render) leaves with terminal state "DeviceLocked" when `desktop.app.systemLockedStatus` turns false→true and the pref is on. Slack's Linux build defines `SET_SYSTEM_LOCKED` but never dispatches it, so on Linux Slack never leaves. | `internal/ui/screenlock`: macOS `com.apple.screenIsLocked`/`screenIsUnlocked` distributed notifications; Windows `WM_WTSSESSION_CHANGE` (WTS_SESSION_LOCK/UNLOCK) to a message-only window; Linux logind's `Lock`/`Unlock` and `LockedHint` on this session plus `ActiveChanged` from org.freedesktop/org.gnome.ScreenSaver. A lock edge with the pref on leaves the huddle. On Linux Clack does what the row says, where Slack does nothing. |
| When alone in a huddle… Play music, and its select (`huddles_play_music_when_last`, `huddles_play_music_timeout`) | `HuddleLoungeMusicPref` (unset is off; the select shows only while it's on: "Right when I join" 1, "After 30 seconds" 30, "After 1 minute" 60). The huddle state machine starts INITIAL_ALONE with a timeout of `huddles_play_music_timeout` seconds (unset 60), or 10 s for IN_PROGRESS_ALONE after someone left (`resolveSubstateTimeoutMs`). When the alone notice goes active, `startHuddleLoungeMusic` plays `huddles_music_player_song_key` (default "jazz", `waiting_muzak-9187b04.mp3`), looped, on its own audio element. PEER_JOINED stops it (STOP_LOUNGE_MUSIC), and so does turning the pref off. | `huddleui/avprefs.go`: the same timers, and the song files from `getSongsMetadata`/BLe4 (the Miles Davis three fall back to "jazz", as when Slack doesn't enable them). The mp3 is fetched from `a.slack-edge.com/bv1-13-br/` without any cookie, cached, decoded and mixed into your speakers only (`Session.PlayLocal`: it never reaches the microphone or Chime, and plays even deafened, like Slack's separate element). Slack's music has no redistribution licence, so it's fetched at runtime and never shipped. |
| Blur your video background (`video_background_filter`) | A device pref, not users.prefs: localStorage `prefs.video.effect`, checked when it is "blur". Shown when huddle video is allowed and the browser isn't Firefox. | **Done** (bg-blur): ui.json `huddle_video_effect`; `internal/huddle/bgblur` runs Slack's mask and blur passes on the camera goroutine. See "Background blur" below. |

Tests (fake Chime and synthetic audio only): `TestMicAGC` (quiet speech -40.5 → -17 dBFS with the gap noise held at AGC2's -50 dBFS; loud speech not boosted, nothing clipped; off is bit-exact), `TestMicNoiseSuppression` (14 dB SNR: the gap noise falls -40 → -81 dBFS, the speech loses 3.4 dB), `TestMicProcessorToggle`, `TestPreviewDevices`, `TestPreviewMicDenied` (internal/huddle); `TestHuddleMicProcessingPrefs`, `TestHuddleGlobalMute`, `TestHuddleLeaveOnLock`, `TestHuddleLoungeMusic`, `TestJoinPreviewFlow`, `TestHuddleShotsJoinPreview` (internal/ui/huddleui); `TestWatchDBus` (a private dbus-daemon with a fake logind), `TestWatchWTS`, `TestWatchDarwin` (screenlock); `TestRegisterX11` (Ctrl+Shift+Space typed with xdotool under Xvfb, with Num Lock too), `TestRegisterWayland`, `TestRegisterWindows`, `TestRegisterDarwin` (globalkey). The `huddle` workflow's pictures step saves `join-preview-*-{light,dark}.png` in the `huddle-shots` artifact.


## Background blur (video_background_filter, 2026-11-10)

Read from Slack's web client on the public CDN (bv1-13: `gantry-v2-async-client-boot-shared`, `client-boot`, the `background.worker.3f3041fa685abac3` chunk and the `node_modules_mediapipe_tasks-vision_lazy_recursive-js_modern_utility_video-effects_model_self-c8097d` chunk it loads; all fetched without cookies). No huddle was started or joined, and the real account's prefs weren't touched. Module ids are in brackets.

**The pref.** The A/V prefs component keeps the effect in localStorage, like `prefs.audio`: `useState(() => getPrefs().video?.effect)`. The checkbox is `name="video_background_filter"`, `data-qa="video-background-filter-checkbox"`, checked when `"blur" === effect`. Its onChange sets `effect = effect === "blur" ? undefined : "blur"`, writes the prefs and dispatches `{effect}` to the client store, so the huddle you're in follows. The row needs `F7` (huddle video allowed) and `!gm()`, where `gm` is `isFirefox`. The call side reads `backgroundVideoFilteringSetting: isFirefox() ? "none" : "both"`.

**Values.** `effect` is unset (no effect), `"blur"`, or `"bg"` with `effectBgKey` (Slack's background images; a stored `"BEACH"` is migrated to `"LIVING_ROOM"`) or `customBgImg`. Image backgrounds are separate from this row: the huddle window's effects picker sets them, and the worker draws them with `MaskShader`: `mix(camera, image, mask)` on the bilateral mask, with no smoothstep. Clack has no picker and no images, so `"bg"` sends the camera unchanged (`huddle.VideoEffectImage`).

**Where it runs.** `setCameraVideoInputDevice` → `VideoProcessingManager.processVideoWithBackgroundBlur` → `BackgroundWorkerVideoTransformDevice`. That needs MediaStreamTrackProcessor/Generator, OffscreenCanvas and createImageBitmap; without them, or after a worker error, Slack sends the plain camera. The transformed track feeds both the self view and Chime. While an effect is on, `applyVideoEffectCaptureFrameRate(true)` asks the camera for 20 fps (Clack's camera stays at 15). The worker's `SegmentationEffectPipeline` runs MediaPipe Tasks' `ImageSegmenter` (GPU delegate, VIDEO mode, `outputConfidenceMasks`) on `selfie_multiclass_256x256-57ecd5e.tflite` [N6An]. That file is 16,371,837 bytes, the same size as Google's float32 `selfie_multiclass_256x256.tflite`. Slack keeps `confidenceMasks[0]`, the background class.

| Step | Slack | Clack |
|---|---|---|
| Mask cadence | `MaskCadenceController` [4Cqi]: segment every `[1,2,3,4][rung]`-th frame and reuse the mask in between. Starts on rung 1 (every 2nd frame). Two 2 s windows over 50 % busy climb a rung; three under 25 % step down. At most one change per 3 s, and the huddle worker's max rung is 3. | `cadence.go`, the same numbers. Busy is `Apply`'s time on the camera goroutine. |
| Temporal smoothing | None. The mask is held for `decimation` frames, then replaced; frames are never blended. | Same. |
| Mask smoothing | `MultiBilateralShader.fillMask` [thur]: 4 passes at 256×256 into RGBA8 textures. Each pass is a 15×15 kernel of Gaussian σ 4 with taps `4/u_iter` frame pixels apart (4, 2, 4/3, 1). Each tap is weighted by `(m − 0.5)²` and by `normpdf(distance(rgb, centre), BSIGMA)`; normpdf ignores its σ argument and uses σ 4, so that colour weight is `exp(−d²/32)`, nearly flat. Taps are `smoothstep(0, 1, m)`; texels whose smoothstep is over 0.95 become 1 and under 0.05 become 0. | `fillMask`: the same passes on the 256×144 mask, quantized to 8 bits after each pass. The colour is the frame on its blur grid, sampled bilinearly. |
| Blur and feathering | `BlurShader` [UVKF]: a separable 25-tap Gaussian, σ 12 frame pixels, with 13 precomputed weights (0.0473192 … 0.0287005). It blurs `c·I` and `c` with `c = smoothstep(0.4, 0.6, mask)` (LINEAR-sampled), then divides. A pixel with `c < 0.001` keeps the camera's picture; any other pixel takes `Σwc·I / Σwc`, or the camera's when `Σwc ≤ 1e-6`. There is no separate mix step: the edge is feathered only by the mask weighting. | `composite`: the same weights, at multiples of the grid cell (cells of 2 px up to 960 px wide, 4 px for 720p and 1080p, so 7 taps at 720p). The blurred premultiplied field is sampled back bilinearly and divided per pixel, in the frame's own NV12 (Y at full size, Cb/Cr at half). |

**The model.** Slack's multiclass vision transformer needs 4.2 G multiply-adds a frame, which only fits on a GPU (100 times Clack's 20 ms CPU budget). Clack embeds MediaPipe's `selfie_segmenter_landscape.tflite` (Apache-2.0, 250 KB, float16 weights, 256×144 RGB → person probability, 32.7 M multiply-adds), unmodified from `storage.googleapis.com/mediapipe-models`. Its weights are byte-for-byte those of `selfie_segmentation_landscape.tflite` (MediaPipe's asset bucket), the model the Chime SDK's `BackgroundBlurProcessor` (in Slack's vendor bundle) loads. Clack uses 1 − person as Slack's background confidence. `internal/tflite` is a new pure-Go interpreter: a flatbuffer parser and exactly the model's ops. Those are CONV_2D (43), DEPTHWISE_CONV_2D (11), ADD (14), MUL (10, squeeze-and-excite broadcasts), MEAN (10), RELU (22), HARD_SWISH (11), LOGISTIC (11), RESIZE_BILINEAR (3, half-pixel centres), DEQUANTIZE (110 float16 weights, folded at load) and MediaPipe's custom `Convolution2DTransposeBias` (1). The kernels are float32 NHWC loops over contiguous channels, with 1×1 convolutions as an 8-wide register-blocked GEMV (AVX2+FMA assembly on amd64). Rows are split over up to 4 goroutines.

**Accuracy and speed.** Against MediaPipe 0.10.35's runtime (Tasks ImageSegmenter: TFLite with XNNPack and the custom op) on the two fixed 256×144 inputs, the person probability is within max |Δ| 2.3e-5 (mean 1.4e-8) on the synthetic person and 1.8e-7 on noise, single- or multi-threaded, on all three OSes. On the synthetic person the final mask's IoU with the drawn silhouette is 0.986 at 1280×720, 0.985 at 640×480 and 0.977 at 320×180. Interpreter speed on CI runners (`BenchmarkInvoke`; `TestInvokeSpeed` asserts the best of three 20-run batch medians under 20 ms): Linux amd64 7.8 ms, Windows amd64 7.9 ms, macOS arm64 8.2 ms. On amd64, AVX2+FMA kernels (`simd_amd64.s`, CPUID-gated) run the GEMV and the depthwise multiply-add; without them the Linux runner took 24 ms. The whole effect at 1280×720 (`BenchmarkApply720p`, segmenting every second frame) costs 14.4 / 14.9 / 10.2 ms per frame: about 31 / 37 / 16 ms on segmented frames and 14 / 17 / 5 ms on the others.

Still different: the model (above); the blur and the bilateral colour run on a grid of 2–4 px cells instead of every pixel; capture stays at 15 fps; no background images.

Tests: `TestModelOps`, `TestConv2DMatchesNaive`, `TestDepthwiseMatchesNaive`, `TestResizeBilinear`, `TestTransposeConvBias`, `TestBroadcast`, `TestFloat16`, `TestGemvTail`, `TestParseRejects`, `TestKeepAllMatches`, `TestMediaPipeReference` (against `testdata/*.mediapipe.f32`, made by the bgblur workflow's `reference` job with `mediapipe_ref.py`), `TestInvokeSpeed` and `BenchmarkInvoke*` (internal/tflite); `TestMaskSyntheticPerson`, `TestBlurKeepsPerson`, `TestBlurWeights`, `TestCadence`, `TestFillMaskSnaps`, `TestOddSizes`, `TestApplySpeed`, `BenchmarkApply720p` (internal/huddle/bgblur); `TestVideoEffectToggle` (fake Chime: blur on at join, off mid-huddle frees the model, on again, `"bg"` unchanged), `TestVideoEffectPreview` (internal/huddle); `TestHuddleBlurPref`, `TestHuddleShotsBlur` (internal/ui/huddleui). Pictures: `blur-self-preview-{off,on}-{light,dark}.png` in the huddle workflow's `huddle-shots` artifact, and `bgblur-source.png` / `bgblur-blurred.png` (1280×720) in `bgblur-shots-*`.

## Colours (2026-11-09)

Read from Slack's CSS on the public CDN (bv1-13: `client-boot-styles.cf726c4f2eaecc74` for the rules, `slack-kit-gantry-v2-styles.f859766a17f82b2a` for the token values in `:root,.sk-client-theme--light` and `.sk-client-theme--dark`) and checked against `getComputedStyle` in the live dialog (read-only). Values are light / dark.

| Element | Slack | Value |
| --- | --- | --- |
| Dialog surface | `.c-sk-modal` `--dt_color-base-pry` | #fff / #1a1d21 (Clack drew #222529 in dark) |
| Header line | `.p-prefs_dialog__layout` border-top `rgba(var(--sk_foreground_low),.13)` | rgba(29,28,29,.13) / rgba(232,232,232,.13) |
| Nav row | `.c-tabs__tab_menu--vertical .c-tabs__tab` `--dt_color-content-pry` | #1d1c1d / #f8f8f8 |
| Nav icon | `.p-prefs_dialog__menu .c-tabs__tab:not(--active) .c-tabs__tab_icon--left` `rgba(var(--sk_foreground_max),.7)` | rgba(29,28,29,.7) / rgba(232,232,232,.7) |
| Selected row | `--active` `--dt_color-base-inv-hgl-1`, text `--dt_color-content-inv-pry` | #1264a3 + #fff / #0a77a7 + #f8f8f8 (Clack drew #1d9bd1 in dark) |
| Hover row | `--dt_color-base-pry-hover` over `--dt_color-base-pry` | #4544470f over #fff / #f8f8f814 over #1a1d21 |
| Headings, labels | body `rgb(var(--sk_primary_foreground))` | #1d1c1d / #d1d2d3 |
| Notes | `p` `rgba(var(--sk_foreground_max),.7)`; Working hours paragraph and `.c-label--disabled` `--dt_color-content-sec` | #454447 / #b9babd |
| Hints | `.c-label__subtext` `--dt_color-content-ter` | #5e5d60 / #9a9b9e |
| `hr` | `rgb(var(--sk_foreground_low_solid))` | #dddddd / #35373b |
| Checkbox, radio | `appearance:auto`, `accent-color:auto`: the browser's own 13px control in the mode's `color-scheme` | checked #0075ff / #99c8ff (check #fff / #3b3b3b), unchecked border #767676 / #858585, hover #005cc8 / #d1e6ff |
| Disabled box | `.c-input_checkbox--disabled` opacity .38 | the native box at 38% |
| Select | `.c-select_button` base-pry, border `--dt_color-otl-pry`, icon content-pry, no hover change | border #7c7a7f / #797c81 |
| Inputs | `.c-input_text` base-pry, otl-pry, `:focus` `--dt_color-otl-hgl-1` | focus #1264a3 / #2ba5ce |
| Links | `a`, `.c-link`, `.c-link--button` `--dt_color-content-hgl-1` | #1264a3 / #2ba5ce |
| Outline button | `.c-button--outline` base-pry, border `--dt_color-otl-sec`, hover `--dt_color-base-sec` | #5e5d6073 / #797c8180 |
| Primary button | `.c-button--primary` `--dt_color-base-inv-hgl-2`, white | #007a5a / #177e56 |
| Skin tones | the radio is hidden; the label box has border `rgb(var(--sk_foreground_low_solid))`, radius 6, padding 8; selected border `#1264a3` | |
| Appearance tabs | `.c-tabs__tab` content-sec, active content-pry, underline `--dt_color-theme-base-pry` | the dialog's one account-theme colour |

None of these except the Appearance tab underline is a `--dt_color-theme-*` token, and Slack's theme module (`getThemeVariables`, gantry-v2-vendors) writes only `--dt_color-theme-*` and the palette ramps. So every account theme (Aubergine, a dark sidebar, custom hexes) leaves the dialog in the same blue and grey in Slack. A stylesheet that overrides the base tokens (a Slick theme like Hot Dog Stand, user.css) recolours it, and Clack reads those overrides (`prefsColorsOf`). Clack's Plugins / Themes / Plugin settings rows and pages use the same tokens. Tests: `TestPrefsColorsMatchSlack`, `TestPrefsColorsIgnoreAccountTheme`, `TestPrefsColorsFollowStylesheetTokens`, `TestNativeCheckPixels`; screenshots from `TestPrefsColorShots` (prefs-colors workflow, `prefs-colors-shots` artifact).

## Simplified layout mode (2026-11-10)

Slack calls it "big button mode" (BBM). Everything below is from the web
client on the public CDN (build a84662d2d135, bv1-13-br): the GantryV2
chunks `client-boot`, `gantry-v2-async-client-boot-{shared,render}`,
`gantry-v2-async-prefs-dialog`, the three `gantry-v2-async-client-bbm-*-view`
chunks and `client-boot-styles`. Module ids are in brackets.

**The pref.** It isn't a users.prefs key, and nothing reaches the API. The
redux store `bbm` holds `{enabled}`: persisted, client and team stores
[TNXj]. localConfig keeps `bbmEnabled` per team [nXnr t2]. `bbmSettings`
[p/nG] holds three localStorage keys: `slack-zoom-bbm-setting`,
`slack-persistent-actions-bbm-setting` (default true) and
`slack-onboarded-setting`. `nonBBMSettings` holds the zoom and sidebar
visibility to restore when the mode ends. `isBBMUnsupported` is the
`agentic_ia` experiment [I/bR]. Clack keeps the same values in ui.json
(`bbm`, `bbm_hover_actions`, `bbm_onboarded`, `bbm_zoom`, `non_bbm_zoom`,
`non_bbm_sidebar_hidden`).

**Preferences** [AM7V AccessibilityBBM]. The section has the legend
"Simplified layout mode", its description and an outline button, "Turn On"
or "Turn Off". Clicking the button closes Preferences, then starts the mode
(step `accessibility_pref`) or stops it. While the mode is on, a fieldset
"Choose how to display message actions" offers "Always show collapsed menu"
(subtext "Doesn’t support one click reactions") or "Show full action menu
on hover only" (subtext "Supports one click reactions"). Under `agentic_ia`
the section greys, the button is aria-disabled and "Simplified Layout is
not yet supported in Slack 6" follows. "Show previews when hovering over
tabs" is disabled while the mode is on.

**Starting and stopping.**
- startBBM [HnPE]. The first time, it opens the welcome dialog, sets
  onboarded, and on the desktop app sets the mode's zoom to 3 (150%). Then
  it enables the store and fullscreen mode, navigates to the BBM tab
  (BBMMain), and applies setBBMSettings: the mode's zoom, the sidebar shown,
  tab peeks off.
- stopBBM [NoNg]. resetBBMSettings puts back the earlier zoom (0 when unset)
  and sidebar visibility. maybeNavigateAwayFromBBM [RxPj] goes to the
  latest non-BBM tab in history, else Home. Fullscreen mode is disabled.
- While the mode is on, a desktop zoom change is stored as the mode's zoom;
  outside it, as the normal zoom [2bwp updateFromDesktop].
- A cold boot with the mode on lands on the BBM tab [4P57
  RedirectColdBootToBBM]. Outside the mode, back/forward skip BBM-tab
  entries [B69+ getStackWithoutBBMEntries].

**Welcome dialog** [HnPE BBMNuxModal]. It has three panes.
- Welcome: the GIF `bbm-nux-transition-53381be.gif` above the title
  "Welcome to Simplified layout mode", the description, and Next.
- "Let’s set your zoom level: ": Slackbot's "We’re so glad you’re here"
  pillow, then radios for 70%–200% with "150% Default". A pick zooms at
  once. Go Back and Next.
- "Choose how to display message actions": two radio cards with
  `persistent-hover-actions-f6d14dc.png` and
  `full-message-actions-on-hover-9281d14.png`. Go Back and Done.

**Layout** [26v2 FullscreenContainer, TopNav; styles].
- The tab rail, control strip, sidebar popout and banners aren't drawn. The
  client shows the top bar, the breadcrumbs (on every tab but BBM) and one
  panel: `.p-ia4_client--fullscreen`, radius 12, inset 40px 4px 4px
  (72px from the top with breadcrumbs), max-width 1200px, centred.
- The panel shows one view container, `fullscreen.containers.main` [qIxd],
  never two side by side.
- No split view, and no close buttons on views [AaAa, M+MY, eXbF, 1331].

**Top bar.**
- The wrapper is centred, max 1200px, with 8px side margins below a 1208px
  window.
- Left: Back and Forward (16px icons) and History (clock). Each is a
  `.topNavButton`: 28px tall, padding-inline 8px, radius 4,
  theme-surf-inv-pry, hover surf-inv.
- Middle: the search, filling the space, min 192px, 28px tall.
- Right, 8px apart: the 28px avatar with presence (the user menu), Create
  (+), Help (16px), and Exit (sign-out, tip "Exit", label "Exit Simplified
  layout mode").
- The workspace switcher toggle, megaphone and agent buttons aren't shown.

**Breadcrumbs** [26v2 Breadcrumbs, WOop].
- The crumbs are the workspace (back to BBMMain, with a mention badge or an
  unread dot), the tab (its list, if the tab has one), the primary view (a
  channel's name with its icon and no #, a DM's person, or a page title,
  semibold when unread), then the secondary view ("Thread"; a code
  channel's details pane is "Code channel details", its view label from
  getAriaLabelForViewContent under session_channel_views and
  code_channels_naming, else "Session channel details").
- Each crumb shows only up to the container on screen
  (shouldIncludeBreadcrumb). Labels are underlined, carets are 16px, and
  the row is max 1200px wide with 12px padding.
- Clicking a crumb shows that container (navigateToContainerBBM) without
  closing what's after it.

**Navigation** [zluD getDestinationContainer, updateFullscreenContainer].
- A tab switch lands on the tab's list if it has one: Home's channel list,
  the DMs list, the Files nav, or BBMMain. Activity and To-dos are single
  primary views.
- Opening a conversation shows its primary view. Opening a thread or other
  pane shows the secondary view. Closing the pane goes back to the primary.
- Alt+↑/↓ don't step conversations while the list is on screen [N8Zu].
  Ctrl+Shift+D (toggle sidebar) is ignored in the mode [PG1Q].

**The BBM tab** [client-bbm-*-view chunks, WOop styles].
- BBMMain has a header with the workspace switcher button (margin 4px 4px
  4px 34px, padding 8px, radius 8; 34px team icon, the name at 22px black,
  a 20px mention badge). Below it, a ListViewer of the tabs the rail would
  show (useTabRailOrganizer).
- Each row has a 1px theme-otl-inv-pry border, radius 12, margin 1px 0 8px
  and padding 12px 16px. It holds the icon on a 36px theme-surf-inv-pry
  tile (radius 8) and the name at 22px, black with mentions. A badge or
  dot sits on the tile's corner.
- More opens BBMMore: its back header (caret-left-full, then "More" at
  22px, padding 13px 0 13px 38px), the rail's overflow tabs, then
  Preferences.
- The switcher opens BBMWorkspaceSwitcher, "Workspaces": the workspace row
  on base-inv-hgl-1 with a 44px ring around its icon, the name (15/700),
  the address and the ⌘1 shortcut (Slack writes "Ctrl 1" off macOS; Clack uses its own formatter, "Ctrl+1").

**Sidebar in the mode** [X4Y6, xnuR, Jbto, styles].
- `--sidebar_item_height` is 44px (space.275), up from 28px (space.175).
- Conversation and page rows are boxes: 1px theme-otl-inv-pry border,
  radius 4, theme-surf-ter-pressed fill. They lose the selected style. A
  12px SpacerRow follows each one.

**Message actions** [zgdY, L2dv MessageActions].
- With "Always show collapsed menu" (the default), every message shows one
  28px "Message actions" button. It is the ellipsis, with no quick
  reactions and no other buttons, at top min(50% − 14px, 12px), right 16px.
  It opens the context form of the message menu.
- The body column gives the button room: gutter__right gets padding-right
  44px (36px more in a channel, 44px in a thread).
- "On hover only" keeps the normal hover toolbar.

**Shortcuts.** Alt+= / Alt+Plus (start), Alt+− (stop) and Alt+Home (BBM
list) exist in PG1Q. Their ignoreIf requires `EjMV.Cu()` (env dev) or
`b3()` (a js_path debug URL), so production Slack has no shortcut for the
mode, and Clack doesn't either. The KEYBOARD_SHORTCUT step only happens in
dev.

**What Clack leaves out.**
- Screen-reader announcements ("Simplified layout mode enabled/disabled"),
  because Clack has no accessibility bridge.
- Megaphone clearing, because Clack has no megaphone.
- Tab peeks, because they're already greyed in Clack.
- The BBM help path (hW.s): Help opens Clack's help as it does outside the
  mode.
- The workspace switcher's "Add Workspace" row, and switching workspaces,
  because Clack signs in to one workspace.
- The Admin tab and its divider (Clack has none).
- Child windows' BBM title bar, because Clack has one window.
- Large GIFs don't animate in Clack's image cache, so the welcome GIF shows
  its first frame.
- Approximations: the row fill token (theme-surf-ter-pressed) has no
  sidebar-scope value in Clack's token table. It is drawn as the dark
  ramp's value over the sidebar's lilac (rows) and the hover as Clack's
  sidebar hover. The Create icon is 16px; Slack's is `size="inherit"`.

**Tests and shots.**
- `TestBBMPrefRoundTrip`: on and off through the start and stop paths,
  ui.json across a restart, no API write mentioning the mode, the welcome
  dialog and zoom 3 the first time only, and the agentic_ia state.
- `TestBBMLayoutAndNavigation` (light and dark): panel geometry, the BBM
  list, a real click on Home, 44+12px sidebar rows, the keys, conversation,
  thread, each breadcrumb, DMs list then conversation, Preferences, the
  dialog panes, and Exit followed by Back.
- messages `TestPersistActionsButtons` and `Test{IdleRowsStayPut,FlingScrollNoGaps,FlingPageArrivalEveryFrame}BBM`
  (plus Compact).
- The bbm-shots workflow uploads `bbm-*-{light,dark}.png` (main, more,
  workspaces, home-list, channel, thread, dms-list, dms-conversation,
  prefs, welcome-1..3).

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
  `TestPrefsListsKeyboard` (the Lists keyboard pref and its experiment gate),
  `TestListKeys*` (listview: every Lists binding in both modes),
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
- Availability, status and focus mode (2026-11-10): `TestAvTimeOptions`,
  `TestAvChangesWriteWhatSlackWrites`, `TestAvPageReadsAndWrites` (popups,
  prefs_availability_test.go); `TestActProfileSetFields`,
  `TestActScheduleStatusCall`, `TestActListAndDeleteScheduled`,
  `TestActClearStatusCall`, `TestActApplyDND`, `TestActStatusModalSaves`,
  `TestActStatusModalScheduledSection` (popups, actions_status_api_test.go);
  `TestFocusExpiration`, `TestFocusModeToggle` (ui, focus_mode_test.go). All
  write to fakeslack only.
- exp-media (2026-11-10): `TestStatusShortcut` (ui), `TestActStatusTipPill`,
  `TestActTipKeys`, `TestActCalendarRules`, `TestActStatusNoCalendar`,
  `TestActStatusCalendarStep` (popups, actions_status_calendar_test.go);
  `TestFocusCustomDuration`, `TestFocusCustomShots` (ui); `TestEmojiToneChoices`,
  `TestEmojiTonePopover` (popups); `TestDeferStreamingNotification` (ui) and
  `TestStreamDoneEvent`, `TestOpenStreams` (core).

## The panel's padding and focus rings (plugins-leftovers, 2026-11-11)

Slack's `.p-prefs_dialog__panel` (padding 20px 28px) is the page's scroll container, so it scrolls and clips at its padding box: what overflows a control shows in the padding, like the 5px box-shadow ring of `.c-input_text:focus`. Clack's panes clipped at the content box, at the controls' own edge, so the Plugins search field's focus ring and the All pill's left edge were cut (Karim, α.37 on Windows). Every pane now scrolls and clips at the padding box (`panelList` for the Plugins, Themes and Taut pages, `prefsScroll.panel` for Slack's sections), and a theme's panel background covers the padding as Slack's does. The search fields (Plugins, the library, Themes, Slick's settings, VIP's) draw Slack's focus ring (1px `otl-hgl-1` and 5px of it at 30%, outside the box, the border transparent), and the inputs', pills' and chips' borders sit inside their box as CSS borders do. CI: `TestPluginsPrefsShots` (plugins-shots) focuses the Plugins search with a click and checks the ring's pixels left of it (`plugins-search-focused-light.png`, `-dark`).
