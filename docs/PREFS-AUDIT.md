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
| Home | **fixed**. Added Channel organization and Show activity; views in Slack's order (Unreads, Huddles, Threads, Saved, Drafts & sent, Directories) with Slack's defaults (on); custom filter/sort hints; External Connections row; "Hide muted channels from your sidebar" (improve_mute_pref: the box is the pref's negation) with its helper text | **newly applied:** enable_threads_view, enable_huddles_home_view, enable_sent_view, enable_saved_view, enable_unified_directory_view hide the sidebar links; home_unread_dot (Slack's real key; its input is named enable_home_unread_dot): off, the Home tab has no dot or badge; show_sidebar_avatars: off, DM rows show the presence badge; should_show_connect_section: externally shared conversations go to the slack_connect section. Server: disable_auto_channel_sort (the server files joined channels; the client only reads auto_sort upserts). Applied: sidebar_behavior, channel_sort, separate_private_channels, undo_channel_intermix, boost_mentions, show_muted_items_outside_sidebar_menus. **2026-11-09, newly applied:** enable_unread_view (true shows Slack's Unreads link first in the sidebar and the Unreads page, see below), disable_channel_join_banner (the box is its negation; on, the channel join banner shows over the composer, see below). Greyed: none |
| Appearance | **fixed**, then part A. Font row; Color Mode (Light / Dark / System, desktop) plus the focus-mode dark row; **Slack themes \| Custom theme** tabs. Custom theme has Slack's four hex fields (typed hex, or the pencil's "Pick a color" popover: HSV square, hue slider, hex field), Share, **Import** (Slack's Import theme modal: 4 hexes or a legacy 8/10-colour string, createPayloadFromHexStrings) and **Surprise me** (randomizeTheme). A custom primary sets the brightness (getBrightnessFromCustomHex). The "Celebrate the music of Miles Davis" presets show first under miles_davis_themes | ia_theme (applied), window_background_gradients (applied), colour mode (device). **2026-11-09:** font (device, ui.json `client_typeface`; applied to the whole UI, see "Font" below). Greyed: focus_mode_force_dark_mode_enabled (Clack has no focus mode) |
| Messages & media | **fixed**, then part A. Default Skin Tone as Slack's six ✋ radios (:wave: on hover), "your profile" link, JUMBOMOJI subtext, legends as Slack's | **newly applied:** preferred_skin_tone, emoji_mode as_text (emoji draw as :name:, no JUMBOMOJI; turning it on writes convert_emoticons 0), convert_emoticons (the composer converts the emoticon before Space/Enter, Slack's dq table), obey_inline_img_limit (linked images over 20MB stay collapsed), one-click reactions (user_customized_quick_reactions_display_feature 1/0, frequent / custom slots; the hover toolbar shows them), Bring emails into Slack (users.emailtoken.get / create / delete, Copy). Applied: display_real_names_override, show_typing, time24, hide_hex_swatch, jumbomoji, expand_internal_inline_imgs, expand_inline_imgs, expand_non_media_attachments. **2026-11-09, newly applied:** messages_theme (Clean writes light_with_avatars, Compact dense; MessageThemePref), with Slack's "Here’s an example:" pillows under Theme and Names (CompactMessageExample / MessageWithAvatarExample); see [Compact](#compact-messages_theme-dense-2026-11-09) |
| Language & region | **fixed**, then part A. Keyboard layout row, Slack's spellcheck label. The time zone select lists Slack's 124 labelled zones ("(UTC-08:00) Pacific Time (US and Canada)") | **newly applied:** set_tz_automatically (on: the detected zone is written at once and after every sign-in, maybeUpdateTimezone with Slack's zone tables). Server: tz. Greyed: locale (no translated UI), webapp_spellcheck (no spellchecker) |
| Accessibility | **fixed**. Alt text reminders legend, tab previews, Slack's animations subtext, the three up-arrow options with descriptions and the Ctrl+↑ note, Lists keyboard interactions, Screen reader (Message verbosity, announcements), links to Notifications and Font. Removed invented "Limit message line length". **2026-11-09:** the up-arrow choice is real. Slack keeps it client-side (localStorage composerUpArrowShortcutBehavior, uMEH LIST=1 / LIST_IF_EMPTY=2 / EDIT=3), so Clack keeps it in ui.json, and the composer follows MessageInput.onUpArrow. LIST focuses the message list at the start of the first line, LIST_IF_EMPTY only in an empty field, EDIT (or unset without up_to_browse_kb_shortcut) edits your last message, and Mod+↑ in an empty field always edits. The list then selects its newest message: ↑/↓ move, ↓ past the end or Esc go back, Enter/E edit yours. The Lists and huddle rows now read Slack's real keys (lists_tabbing_navigation_option_type, huddles_reactions_read_out_loud, huddles_reactions_play_sound). **2026-11-09, lists-keyboard:** Lists keyboard interactions is wired. The section shows only with the lists_tabbing_navigation experiment (ListsTabbingPref returns null without it). Data entry is checked while the pref is unset, and a pick writes tab_nav or arrow_nav with setUserPrefByApi. The Lists grid follows it: cell keyboard navigation in both modes (docs/LISTS.md, Keyboard) | applied: underline_links, a11y_animations, composerUpArrowShortcutBehavior (device), lists_tabbing_navigation_option_type. Greyed: Simplified layout mode (a whole second layout), a11y_show_alt_text_reminders (Clack's uploads have no alt text field yet; Slack's reminder opens the edit-file alt text dialog), a11y_peeks_enabled_on_mouse_enter (Clack's rail has no hover peeks), a11y_dm_read_out and huddles_reactions_read_out_loud (screen-reader announcements; Clack has no accessibility bridge), huddles_reactions_play_sound (the sound lives in the huddle chunks, not fetched yet) |
| Mark as read | **fixed**. Removed the invented hint under Prompt to confirm; added Keyboard shortcuts | applied: mark_msgs_read_immediately + start_scroll_at_oldest (one users.prefs.set), confirm_clear_all_unreads |
| Audio & video | **fixed**. Camera, Microphone, gain control, noise suppression, auto-dock, 150-member warning, blur, Huddle motion, music delay. Removed "Set my status In a huddle" (Slack shows it under Availability) and "Turn off my video" (gated off in the live dialog) | applied: huddles_mute_by_default, **huddles_cc_by_default** (a joined huddle starts with captions shown), **huddles_show_shouty_rooster** (starting, not joining, a huddle in a channel of more than 150 members asks "Start a huddle in this channel?", HuddleShoutyRoosterModal, with its "Don’t show this message again." box writing the pref). Greyed: video_background_filter (needs person segmentation on camera frames), huddles_show_join_preview (Slack's pre-join preview window), huddles_global_mute (an OS-wide Mod+Shift+Space hotkey), huddles_disconnect_on_lock (screen-lock events per OS), huddles_play_music_when_last (the lounge music and delay), huddles_reactions_effects, clips_pip_auto_dock (Clack has no clip picture-in-picture), agc/ns (Clack's microphone path has no WebRTC audio processing) |
| Salesforce | **fixed**. Slack's exact copy; removed Clack's invented sentence | none |
| Connected accounts | **fixed**. Slack's copy, Learn more, "Apps connected through workflows". **2026-11-09:** the list is fetched like ConnectedWorkflowAccountsList (fetchUserAuth: apps.auth.external.userAuths.get, reason fetch_user_auth): one row per app, or "No accounts found". Still different: no Manage button (Slack's manage-accounts modal isn't ported) | none |
| Privacy & visibility | **fixed**. Learn more links, "Preferences only apply to this workspace" note, the workspace name in Contact sharing, People you've hidden. Removed the invented Privacy Mode section. **2026-11-09:** Find my workspaces opens Slack's get-started find page (Rgd9); Invitations you’ve blocked asks users.emailVerificationStatus.get and, verified, lists users.listIgnoredUsers with each email's unblock button (UnblockConfirmationModal, users.removeUserBlock), or "You haven’t blocked any invitations from anyone yet."; unverified, Slack's 👋 copy. People you’ve hidden lists hidden_users with unhide (unhideUserApi writes the list without them), or "You haven’t hidden anyone yet." Still different: "Send Verification Email" is greyed (Slack's code-entry flow goes through signin.confirmCode); Clack doesn't hide hidden people's messages | server: who_can_see_account_by_searching_email, who_can_share_contact_card, hidden_users |
| Advanced | **fixed**. Slack's Input options order and copy, Enter-key radios, Search options, Download location, Confirmations and warnings, Other options, Launch on login / notification-area rows (Linux, Windows). Removed "Copy message text as Markdown" (gated off in the live dialog). Clack's own Live updates / Updates / Troubleshooting section kept | **newly applied:** msg_input_send_btn (Enter adds a line, Mod+Enter sends). Applied: enter_is_special_in_tbt, plain_text_mode. **2026-11-09:** disable_smart_symbols (behind composer_smart_symbols, as iZ.w gates the row: TextSubstitutionsModule's "-- " → "— ", -> ← ↔, ... → …, type-time curly quotes, none in code, each its own undo), search_exclude_channels (Slack's channel multi-select; the In suggestions drop them as parseSuggestions does, Slack's servers drop the results), stick_user_sort (a new search starts from search_sort; the sort menu says "Most relevant" and writes search_sort, "Most relevant (default)" otherwise, plus "Edit default"), should_show_unsend_message_confirmation (Mod+Z in the empty composer within 15 s of a send unsends it and puts it back, behind Slack's Unsend message dialog), confirm_user_marked_away (after connecting while manually away, "Set yourself to active?"), suppress_link_warning (isLinkSuspicious and the "Double-check this link" dialog on message links). Server: overloaded_message_enabled, in_prod_surveys_enabled. Greyed: disable_link_truncation (its reader isn't in the public client bundles checked), hide_sc_files_sharing_speed_bump and hide_external_members_canvas_sharing_speed_bump (the send-time Slack Connect "Is it OK to send this?" dialog isn't ported), download location, launch on login, notification area |

## Still greyed after part A, and the plan for each

enable_unread_view and disable_channel_join_banner are done: see "Unreads view and channel join banner" below.

- ~~**messages_theme** (Messages & media)~~: done 2026-11-09, see "Compact" below.
- ~~**font** (Appearance)~~: done 2026-11-09, see "Font" below.
- **focus_mode_force_dark_mode_enabled** (Appearance): only means something
  once Clack has Slack's focus mode.
- **locale** (Language & region): the whole UI's strings through a catalog
  for Slack's 12 locales (Slack's i18n bundles are per-locale chunks), plus
  the localized time zone and sound lists Slack already ships (sbu9 has 11).
- **webapp_spellcheck**: a spellchecker in the composer (NSSpellChecker on
  macOS, ISpellChecker on Windows, hunspell dictionaries on Linux) with the
  red underline and suggestions menu.

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

Still different: Slack's letter-spacing variables for the system and Roboto
Mono / Noto Sans fonts (`--custom-font-tracking-*`, e.g. 0.06pt on body
text) aren't applied, because Clack's labels have no letter spacing; Chrome's
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
