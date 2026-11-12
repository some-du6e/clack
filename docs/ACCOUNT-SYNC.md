# What Slack keeps per account, and what it keeps per device (2026-11-12)

Karim asked whether the sidebar width syncs with the account, because their
Slack sidebar is about 420px wide and Clack's was about 230. **It doesn't
sync.** Slack keeps the width on the device. Their 420 is Slack's default:
Slack sizes the sidebar as a share of the window, and Clack used a fixed 232.

Source: Slack's web client, build `fa938a086569` (2026-11-12), from the public
CDN without a cookie (`client-boot`, `gantry-v2-async-client-boot-render`,
`-boot-shared`, `-boot-vendors`, `-prefs-dialog`, `modern.vendor`). Module ids
below are from that build. The pref inventory came from every
`ty(state, "<pref>")` read and every `pref:"<pref>"` write in those chunks
(318 names), checked against Clack's source. Nothing touched the real
account: the tests run against fakeslack.

## Where Slack keeps things

- **The account** means user prefs: `users.prefs.get`, `users.prefs.set`
  (`setUserPrefFetcher`, nfg+ `CI`, value serialised by Yl7S `Vc`), and the
  realtime `pref_change` event. JSON-typed prefs (`channel_sections`,
  `ia_theme`, `frecency`, `recents`, `emoji_use`) go over the wire as JSON
  text.
- **The device** means `localStorage`: `localConfig_v2` (ieP4: `getByKey`,
  `setForKey`, per-team `getForTeamByKey`), plus a few loose keys, plus the
  desktop app's own settings. Clack keeps these in `ui.json`.
- **The view-layout store** (QXGe, `storeKey: "viewLayout"`) has
  `persistenceEnabled: false`. Every column width and mode in it is read from
  and written to `localConfig_v2` under the `/fAs` keys:
  `viewLayoutSidebarWidthIntent`, `…SidebarWidthMode`,
  `…SidebarWideWidthIntent`/`Mode`, `…SidebarExtraWideWidthIntent`/`Mode`,
  `…SidebarLaterWidthIntentV2`/`ModeV2`, `…SecondaryWidthIntent`/`Mode`,
  `…ForActivity`, `…ForLater`, `…SplitWidthIntent`/`Mode`,
  `…IsSidebarVisible`, `…CompactModeEnabled`, `…IsBigModeSidebarVisible`,
  `…IsWorkspaceSwitcherVisible`, and `viewLayoutLastSidebarWidth`. The
  thunks (`setSidebarColumn`, QXGe's `setSecondaryColumnForActivity`
  in lJYS, `setSecondaryColumnForLater` in k0bF) write `localConfig_v2` and
  broadcast to the other workspaces in the window (`maybe_broadcast_state_to_workspaces`).
  They never call an API.

## Inventory

**Status:** *ported* means this branch added it. *Matches* means Clack already
did what Slack does. *Open* means it's a work package below.

| Item | Slack cite | Where Slack keeps it | Clack |
| --- | --- | --- | --- |
| Home sidebar width | QXGe, W2CY `b`, b2oS `useCalculateMainWindowClientLayout`, 7M48 `BaseResizer` / `calculateColumnWidth` | device: `viewLayoutSidebarWidthIntent` + `…Mode` | **ported**: see "Sidebar width" below |
| Boot placeholder width | XwtS `renderBootPlaceholder`: `viewLayoutLastSidebarWidth \|\| 260`, written by b2oS on each layout change | device | not needed: Clack's first frame is laid out from the window, with no HTML placeholder |
| Wide sidebar (Activity, DMs, To-dos) | zfm1 `tabHasWideSidebar`, W2CY `isWideWidth`: .38, 320 … 720 | device: `…SidebarWideWidthIntent`/`Mode` (To-dos: `…SidebarLaterWidthIntentV2`) | **ported** the clamps: the list was 300 … 640, now 320 … 720 (`views/dms.go`). Open: Slack's list column is resizable with its own intent, Clack's isn't (WP 4) |
| Extra-wide sidebar (Search, Activity inbox) | zfm1 `tabHasExtraWideSidebar`, W2CY (320, region less primary/secondary, .5) | device | Clack's search page lays out its own columns (docs/SEARCH.md) |
| Secondary pane width | QXGe `secondaryWidthIntent`/`Mode`, aTtQ `hW` | device | matches (`secondary.go`, ui.json `secondary_w`) |
| Secondary width in Activity and To-dos | lJYS / k0bF: `…SecondaryWidthIntentForActivity`, `…ForLater` | device, one width per tab | **ported**: Activity and To-dos keep their own width (ui.json `secondary_w_activity`, `secondary_w_later`). Before this, a drag in a channel resized Activity's pane too |
| Split view width | `viewLayoutSplitWidthIntent`/`Mode`, DUQ2 | device | matches (`split.go`) |
| Sidebar shown or hidden | `viewLayoutIsSidebarVisible` | device | matches (ui.json `sidebar_hidden`) |
| Compact mode (Icons only) | `ia4_compact_mode` (pref) and `viewLayoutCompactModeEnabled` | account | matches (Navigation, PREFS-AUDIT) |
| Collapsed sections | hJIH `setChannelSectionCollapsed`, tQkP `mM` = `channel_sections` | account: `channel_sections` `{<section id>: {c: "1"\|"0"}}` | **ported**: see "Collapsed sections" below. Before this, ui.json held them: a collapse never reached the account, and the account's state was read once per section, then ignored |
| Section filter and sort | `setChannelSectionPrefByApi` (`channel_sections` `sidebar`, `sort`) | account | matches (`core/section_menu.go` `SetSectionPref`), live on `pref_change` |
| Display-rule sections' collapse | hJIH, `collapseKey` path (zwyz `X`) | client store | no display-rule sections in Clack |
| `sidebar_behavior`, `channel_sort`, `separate_private_channels`, `undo_channel_intermix`, `boost_mentions`, `show_muted_items_outside_sidebar_menus`, `improve_mute_pref` | Home prefs page, lJBI | account | matches (PREFS-AUDIT Home) |
| `boost_unreads` | lJBI sort: `!!ty("boost_unreads") && eZ.eM` | account | open (WP 6): Clack doesn't read it |
| `separate_shared_channels`, `ent_org_wide_channels_sidebar` | written only by `BasePrefsSectionSidebar`, the old Sidebar prefs section | account | open (WP 7): the live dialog's Home page doesn't show them (PREFS-AUDIT), so they only matter if another client set them |
| `hidden_user_group_sections` | `hideUserGroupSections`, `RemoveUserGroupSectionConfirmationModal`, Preferences' `RemovedUserGroupSections` | account, comma list | open (WP 3) |
| Home view links (`enable_threads_view`, `enable_huddles_home_view`, `enable_sent_view`, `enable_saved_view`, `enable_unified_directory_view`, `enable_unread_view`, `home_unread_dot`) | Home prefs page | account | matches |
| `enable_later_view`, `enable_streamline_view`, `enable_mentions_and_reactions_view`, `enable_all_dms_view`, … | read only by `BasePrefsSectionSidebar` | account | not needed: the current client doesn't draw these links from them |
| Tab rail (`tab_rail_customization`, `…_order`, `…_reasons`) | Navigation prefs | account | matches |
| Activity's saved views | `activity_views`, realtime `activity_views_updated` | account | matches (`core/activity_views.go`) |
| `activity_view` ("flexpane" or page) | `maybeOpenActivityWindow`, `shouldUpdatePref` | account | open (WP 5): Clack always opens Activity as a page |
| `mentions_exclude_reactions` | `ActivityToast`, `ActivityReactionToast` | account | open (WP 5): Clack has no reaction toast |
| `activity_include_threads_in_badge_count`, `activity_dms_opt_out` | Notifications page | account | written; Slack's servers act on them |
| `ia_theme` (theme) | `setIaThemePrefByApi` | account | matches, live (`theme_account.go`) |
| Color mode (Light / Dark) | pPFL `setClientTheme`: `localStorage` and the desktop `userTheme` setting, "Set the current theme for the device" | device | matches (ui.json `dark`) |
| Sync with OS | `clientTheme.systemSync` (G4CN `FW`) | device | matches (ui.json `theme_sync`) |
| `sidebar_theme_custom_values`, `ia_top_nav_theme` | legacy theme readers | account | not needed: `ia_theme` replaced them |
| Font | `client-typeface` in localConfig | device | matches (`prefs_font.go`) |
| `preferred_skin_tone` | 46 readers | account | matches, live |
| Message density (`messages_theme`), names (`display_real_names_override`) | Messages & media | account | matches, live |
| `mentions_real_names_override` | the mention pill's name | account | open (WP 8) |
| Frecency (`frecency`, `emoji_use`) | `setUserPrefByApi`, `search_frecency` | account | matches (`core/frecency.go`) |
| Recent searches and places (`recents`) | `setRecentsByApi`, `recordVisitsByApi` | account | matches (`core/search_recents.go`) |
| Unread options (`mark_msgs_read_immediately`, `start_scroll_at_oldest`, `confirm_clear_all_unreads`) | Mark as read | account | matches |
| `locale` | `setLocaleForTeam` (+ localConfig `user_locale`) | account and device | matches, live (`locale_wire.go`) |
| Formatting toolbar (`msg_input_sticky_composer`) | `ComposerButton` | account | matches, written (`composer/composer.go`) |
| `msg_input_formatting_bar_state` | the same toggle under the floating toolbar (`getFormattingBarState`, `tQ`) | account | open (WP 9): read only in that layout, which Clack doesn't ship |
| Up-arrow behaviour | `composerUpArrowShortcutBehavior` | device | matches (ui.json) |
| Media player (`media_playback_speed`, `media_muted`, `media_volume`) | the inline and fullscreen players (`setPlaybackRate`, `setMuted`) | account | open (WP 2): Clack's player has no speed or mute control to keep |
| `huddles_video_off_by_default` | `useJoinPreviewCameraUI`, `HuddleJoinSettingsPref` | account | open (WP 10): the live dialog has the row gated off (PREFS-AUDIT Audio & video) |
| `lists_template_sidebar_collapsed`, `lists_default_view` | Lists | account | open (WP 11) |
| `search_results_card_density`, `result_count_toggle`, `search_minimize_sources` | search, behind `srp_card_explore` | account | open (WP 12): under an experiment |
| `today_*` (cards, sections, channels) | the Today tab | account | Clack has no Today tab |
| Onboarding, coachmarks, `dismissed_*`, `seen_*`, `has_*` (about 120) | first-run tips and banners | account | not ported: one-off tips |

## Sidebar width (ported)

`internal/ui/sidebar_width.go`. Slack's Home column is:

- **Auto (the default):** W2CY `clamp(availableWidth × .24, 180, 450)`.
  `availableWidth` is b2oS's window width less the workspace switcher (0
  unless shown), the tab rail (70) and the 4px `rightPadding`. Slack rounds
  the result. A 1440 window gets 328 (the feel-shots test already used 328),
  1820 gets 419 and 1920 gets 443. Karim's 420 is auto on a window about
  1820 wide.
- **Manual:** the dragged width, clamped to 180 … 450 at every layout
  (W2CY's `manualMinWidthOverride` is unset for Home, so the minimum is the
  same 180; BaseResizer's default `min` is 180 too).
- **The resizer** (7M48 `BaseResizer`): a drag within 15px of the default
  snaps to it and goes back to auto (`calculateColumnWidth`). Anywhere else
  it's the pointer's position, clamped, in manual mode. Two presses within
  500 ms reset it to auto (`onReset`).

Clack's ui.json keeps `sidebar_w` (the manual intent) and `sidebar_w_mode`.
Older builds saved `sidebar_w: 232`, the old fixed default, whether or not
it was dragged. A file with only `sidebar_w` reads 232 as auto and any other
value as a manual width. The old 160 … 480 clamp is gone.

Tests: `TestSidebarWidthLikeSlack` (the formula, the clamps, the ui.json
migration) and `TestSidebarResizeLikeSlack` (a drag sets manual and saves it,
the clamps, the snap, the double click, auto following the window).

## Collapsed sections (ported)

`internal/core/section_collapse.go`, `ui/sidebar_keys.go`, `ui/sidebar.go`.
hJIH `setChannelSectionCollapsed` works like this:

1. It copies the `channel_sections` object and sets `<id>.c` to `"1"` or
   `"0"`, keeping the other sections and keys (`sidebar`, `sort`).
2. It updates the local pref at once (n3H2 `rX`), so the sidebar redraws.
3. It cancels any pending write and debounces a new one (S6U4, 3 s). The
   write sends `users.prefs.set name=channel_sections value=<JSON>` with
   `reason=set_channel_section_collapse_in_home_sidebar`, in the Home
   sidebar's case (`onChannelSectionCollapseToggle` passes `"c"` there).
4. On teardown, `flushPendingChannelSectionCollapsedSync` sends the pending
   write by `sendBeacon`.

Clack does the same. Core's `SetSectionCollapsed` updates the local pref and
emits `prefs`, so the sidebar rebuilds. `FlushSectionCollapsed` runs when the
3 s timer fires, and the backend's `stop` (quit, sign-out) calls it too. A
toggle-all (Alt+click, the keyboard's toggleAll) is one call, so one write.

The sidebar reads the account's state. It takes the model's `Collapsed`
whenever that value changes: on first sight, on another client's
`pref_change`, and when our own write comes back. A model built before a
toggle can't undo it. While a write waits, a `pref_change` reload keeps the
local object (`sectionCollapsePending`), because the server's copy is older.
ui.json no longer has `collapsed`. Organize mode's collapse stays local,
as `setEditModeChannelSectionCollapsed` is.

Tests: `TestSectionCollapsedSyncsWithAccount` (core: read on `pref_change`,
one debounced write with the object merged, a reload during the debounce,
the live change after it), `TestSectionCollapsedDebounceFires`, and
`TestSectionCollapseSyncsInSidebar` (UI: a heading's toggle reaches the
fake account; the account's change over the realtime socket opens it
again).

## Work packages (open)

1. ~~Sidebar width~~, ~~collapsed sections~~, ~~secondary width per tab~~,
   ~~wide sidebar clamps~~: done above.
2. **Media player prefs.** `media_playback_speed`, `media_muted` and
   `media_volume` are written by the video player's speed menu
   (`setPlaybackRate`, then `AZ({pref:"media_playback_speed"})`) and its
   mute button, and read when a player mounts. This needs Clack's player to
   grow those controls first.
3. **Hide a user-group section.** The section menu's Remove opens
   `RemoveUserGroupSectionConfirmationModal` ("Conversations in this
   section will move back to the Channels section of your Home sidebar.").
   Confirming runs `hideUserGroupSections` (reason
   `sidebar-section-context-menu`): it appends the section id to
   `hidden_user_group_sections` (a comma list, setUserPrefByApi, with
   rollback on failure). Preferences' `RemovedUserGroupSections` lists them
   to restore. The sidebar drops those sections.
4. **Resizable wide sidebar.** Activity, DMs and To-dos have a resizer in
   Slack with its own intent (`setWideSidebarColumn`; To-dos has
   `setSidebarLaterColumn`) under the same snap and double-click rules.
   Clack's list column is fixed at the auto width.
5. **Activity as a flexpane.** `activity_view: "flexpane"` opens Activity
   in the secondary pane (`maybeOpenActivityFlexpane`,
   `maybeOpenActivityWindow` with `shouldUpdatePref`). This comes with
   `mentions_exclude_reactions` for the reaction toast.
6. **`boost_unreads`.** The sidebar's sort reads it (`!!ty("boost_unreads")
   && eZ.eM`) next to `boost_mentions`. Find what `eZ.eM` gates, then port
   it in `core/sidebar_rules.go`.
7. **`separate_shared_channels` / `ent_org_wide_channels_sidebar`.** Read
   them in the section rules if another client set them. No UI writes them
   now.
8. **`mentions_real_names_override`.** The mention pill's name follows it,
   as `display_real_names_override` does for message names.
9. **`msg_input_formatting_bar_state`.** "collapsed" or "expanded" for the
   floating formatting toolbar layout (`tQ`), if Clack ships that layout.
10. **`huddles_video_off_by_default`.** The join preview starts with the
    camera off. The row is gated off in the live dialog.
11. **Lists' `lists_template_sidebar_collapsed`, `lists_default_view`.**
12. **Search card prefs** behind `srp_card_explore`.
13. **The resizer's keyboard.** BaseResizer is focusable (`onKeyDown`):
    → / ↑ widen it by 10px and ← / ↓ narrow it, through
    `calculateColumnWidth`; Home collapses a collapsible column, or else
    goes to the minimum; End, → or ↑ show a collapsed one. Mod chords are
    left alone. Clack's sidebar and pane resizers have no key focus.
