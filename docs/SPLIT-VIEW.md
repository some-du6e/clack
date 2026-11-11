# Split view

Slack's split view shows a second channel, DM or thread in a card to the
right of the workspace, beside the primary view. Clack's is in
`internal/ui/split.go`. Everything below was read from Slack's web client
(public CDN, build a84662d2d135, 2026-11-08). Module ids are
boot-render / boot-shared / client-boot / modern.vendor ones.

## Opening

- `openInSplitView` (client-boot `Fndw`, modern.vendor `9ciC`) checks the
  view type (`AjmF` isValidSplitViewType: Channel, Thread, Canvas, List,
  Profile, ActivityInbox and others), then navigates with
  `options.forceRenderInSplitView` (`XUuN` inSplitView).
- The window has one Split container (`getTargetContainer` returns
  `mq.Split`). Opening from outside it replaces its view with no back
  stack. Navigating from inside it keeps the old view as `view.prev`
  (`maybeGenerateSplitBackStack`).
- Inside the split view, channels, DMs and threads stay in it:
  `shouldForceOpenInSplitView` is true when the source container is Split.
  So a thread opened from the split channel replaces it there, and a back
  button returns to the channel.
- The modifier click `shouldSplit` (`5Nta` / `+YLv`) is the platform's Cmd
  key (⌘ on macOS, Ctrl elsewhere) with Alt. `maybeOpenInSurface`
  (`ZRE2`) uses it on sidebar rows and threads, the tab rail, permalinks,
  activity rows (`GMkG`) and the reply bar (`v8ac`).
- Simplified layout mode has none of it: `isBBMEnabled` hides every entry
  and `shouldForceOpenInSplitView` is false.

### Entries

| Slack | Where in Slack's code | Clack |
|---|---|---|
| Channel ⋮, header title and mention right-click: "Open in split view", icon `move-to-split-view`, hint "⌘ Opt Click" / "Ctrl Alt Click" (or "Open in…" › "Split view" with `channel_ia_header_refinements`) | boot-render 26v2 `getTilesTemplate` (`channel_ctx_open_split`, for ids `looksLikeChannelId`) | `popups/split.go` `channelSplitItem` |
| Sidebar row right-click, the same row before the Leave group | 26v2 `getContextMenuTemplate` | `slick_shell.go` `openSidebarMenu` and the plugin template |
| Thread pane ⋮ "Open in split view", not inside the split view | boot-shared IO7k `SecondaryHeaderMore`, eXbF | `popups/actions_thread.go` |
| Activity item menu "Open in split view" with the hint | boot-render wrqs `renderMenuPlaceholderItem` | `views/activity_slack.go` |
| Modifier click on sidebar rows, the reply bar, activity rows, channel links | ZRE2, v8ac, GMkG | `App.Navigate`, `App.OpenThread`, `views/activity_split.go` |

## Layout

- `ClientSplitView` (boot-render `B5vr`) renders the Split container as
  `.p-view_contents--split--card`: margin-left 4px, radius 6, box-shadow
  -4px -4px 16px rgba(0,0,0,.09). In dark mode it has no shadow and a 1px
  `--dt_color-otl-ter` border. The workspace (sidebar + primary) keeps its
  own card to the left (`layoutSubgridSplit`: tabpanel | split).
- Width (`useCalculateMainWindowClientLayout` b2oS, `pyGT`): half of the
  space beside the sidebar, or 30% of the whole workspace in a window wider
  than 1200px. It is clamped between 300 and min(that space − 300, 1000).
  Then the squish (`xeD9`): the primary view keeps 300, and the sidebar
  gives way down to 180.
- Width modes (`DUQ2`): "auto" becomes "sticky" at the width it lays out
  at. A window resize turns "sticky" back to "auto". A dragged width is
  "manual" and stays. Saved per device as localStorage
  `viewLayoutSplitWidthIntent` / `viewLayoutSplitWidthMode` (client-boot
  `puDW` setSplitColumn, `/fAs`). Clack saves `split_w` and `split_w_mode`
  in ui.json.
- Resizer (`DFUN`, BaseResizer): min 300, max 1000. Within 15px of the
  default it snaps there ("auto"). A second press within 500ms resets it.
  The element is `.p-resizer` (8px) with `--split:after` (2px), and its
  hover line is the highlight accent with a 1px ring.
- Minimum window width (b2oS `minWindowWidth`): rail + 320 + 300 + 300 +
  4 while the split view shows (994 at 100% zoom). The desktop app sets
  it and widens a narrower window (`W6f`, `qL1`). Clack does the same with
  `app.MinSize` / `app.Size` and puts its own 800×500 back on close.

## The view

- `ChannelView` renders the full channel (header, tabs, messages,
  composer) in both Primary and Split.
- `ViewHeader` (view-header chunk) adds a close button after the actions
  in Split: `close_view`, a 28px interaction button with icon `close`
  and tooltip "Close" (bottom-right, 300ms). It also shows
  `FlexpaneBackButton` (`DG1E`, `caret-left-full`, 28px, margin-right 6)
  while the view has a back stack. A thread view's flexpane header has
  the same back and close buttons.
- Narrow columns follow `useActionsToMoveToMenu` (view-header chunk,
  module `1DLG`) with `channel_ia_header_refinements` off. Under 550px
  the header drops its search button, and under 500px it drops the bell
  too. The Slackbot DM only ever loses the bell. `getOverflowingActions`
  then keeps floor((width - 340) / 30) of the other movable actions, so
  Huddle goes under 370px. The ⋮ menu lists moved actions only with the
  experiment on, so here it stays the same. Under 600px (`isColumnNarrow`)
  a container query hides `.p-view_header__topic_container` and the title
  runs up to the actions. Clack's `headerFit` measures the split view's
  whole column, or the primary header, which spans a nested thread pane.
- Focus: navigating to the split view moves the focus into it (its
  composer), and closing it hands the focus back to the primary view.
  Escape in a closable container closes it (`4zmr` RenderedViewContents)
  once the view has nothing to mark read (ThreadsFlexpane `onEsc`).
- Clack draws the split view through the same channel and thread code as
  the main view, with its own buttons, conversations (message list and
  composer), tab choice and menu anchors (`paneState`). Menus opened from
  it act in it. Message lists keep per-container state
  (`messages.NewChannelViewIn`), so the same channel can be open in both
  views.

## Persistence

- The width persists. The view does not: `maybeSaveWindowRestorationData`
  skips the main window (`j/WZ`), and the URL holds only the primary view.
  The split view also stays out of main-window back/forward (`onPopState`
  restores the tabs' view sets only), so Clack's history doesn't touch it.

## Not in Clack

- Drag and drop onto the split view (`SplitDropTarget`, "Replace split
  view"), the floating view and Slackbot's split views.
- Canvases, lists, files, profiles and pages (Activity, Later, the tab
  rail's "Open in split view") in the split view: Clack's split view shows
  channels, DMs and threads. The canvas ⋮ "Open in split view" still
  opens Clack's side panel.
- Search results' "Open in split view" message action (Pult): Clack's
  search menu follows the captured menu, which has no such row.
- `ClientSplitView` / `RenderedViewContents` as plugin component
  boundaries: plugins patch the channel's own components in both views,
  not the split container.
- Keyboard resizing of the split column (BaseResizer's range input).
- `channel_ia_header_refinements` on: Slack moves search and the bell
  into the ⋮ menu at every width. Clack keeps them in the header, since
  those rows would go in the channel menu, which isn't this code's.
