# Hover tooltips: Slack's sites and Clack's

Every hover tooltip the desktop client shows in the surfaces Clack draws,
from Slack's web client (CDN build 36d315d11944, captured 2026-11-22:
client-boot, boot-shared, boot-render; every `tip:` prop of a Tooltip,
1,535 sites in those chunks, filtered to these surfaces), checked in
Clack natively and with a plugin patching the component.

## What every tooltip shares

Slack's `Tooltip` is `BaseTooltip` (client-boot `HzHE`):

- **Delay:** 150 ms by default, deferred only for the mouse (keyboard
  focus shows it at once). A site passes its own: ComposerButton 500, a
  message's time and reactions 300, the hover bar 60, a sidebar name 600.
  Clack: `popups.TipDelay` is 150 ms (it was 450, a placeholder from the
  first overlay primitives); every site that passes its own sets
  `TipState.Delay`. A plugin's Slack Kit `Tooltip` (`slackc/kit`) takes
  the same default.
- **Placement:** `position` (top by default), `tryOppositePosition`,
  allowance 3 px: `theme.PlaceTooltip`.
- **Dismissal:** mouse-leave hides it. Content scrolling under a still
  pointer is a mouse-leave in Chromium; Gio re-hit-tests the still pointer
  every frame, so the same happens. A press doesn't hide it by itself.
  `TooltipManager` (`hideVisibleTooltip`) hides the tooltip up when a
  modal or a menu opens, and BaseTooltip hides itself on Escape while
  hovered (`hoverEscHide`). Clack: `theme.HideTooltips` (popups'
  `HideTooltips`) on every Escape press, whoever takes it
  (`inspect_wire.go`, Gio patch 28's key log), and on `ShowOverlay` for
  anything but a hover peek. A hidden tooltip comes back only after the
  pointer leaves and returns. `popups.TipState` tooltips, the message list's reaction, status and hover-bar tips, richview's emoji, edited, copy and file-action tips, the Threads, Unreads and Activity views' reaction pills and a plugin's Slack Kit `Tooltip` all honour it. The huddle window's toolbar tips (a window of its own, its tips drawn at once by `hudTip`) don't: its Escape and menus aren't the shell's.
- **Shortcut hints:** `KeyboardShortcut` chips through `shortcut.Format`
  (never ⌘ literals: `TestNoHardCodedHints`).

## The sites

Native: Clack draws the tooltip without plugins. Patched: with a plugin
patching the component (Taut or Slick; "n/a" where the component isn't
rendered through slackc).

| Site | Slack module (component, id) | Slack: text, delay, position | Native | Patched | Test |
|---|---|---|---|---|---|
| Message time (header, gutter, preamble) | Timestamp, `JO00` | TimestampTooltip (full date, seconds), 300, top | yes | yes | `TestTimestampTooltip`, `TestGutterTimeTipEndsOnWindowsMouse` |
| Reaction pill | Reaction, `Zpvd` (ReactionTip) | who reacted, 300, top | yes | yes | `TestPluginPatchedReactionBarTip`, `TestReactionTipNamesFollowPrefs` |
| Add reaction | ReactionAddButton, `stYk` | "Add reaction…", 300 | yes | yes | `TestPluginReactionAddButtonTooltip` |
| Show more reactions | ReactionExpandButton, `uFSC` | "Show more reactions", 300 | yes | yes | — |
| Sender's status emoji | BaseCustomStatus | status text, "Until …", 150, top | yes | yes | `TestSenderStatusTooltip` |
| Hover bar buttons | MessageActionButton, `/rjK` | label (+ subtext), 60, top | yes | yes | `TestToolbarTipTimingLikeSlack` |
| Edited mark | EditedLabel, `J825` | "{date} at {time}" (by …), 300 | yes | n/a | `TestEditedLabelTooltipTarget` |
| Emoji in a message | Emoji renderTooltip, EmojiTooltipEmoji | the emoji at LARGER on a 64px white tile, then :name:, 300 | yes | yes (EmojiTooltipEmoji is a component) | `TestEmojiTooltip`, `TestEmojiTooltipShowsTheEmojiLikeSlack` |
| Attachment remove button | AttachmentDeleteButton | "Remove preview" ("Remove contact info": no contact cards in Clack), 150, top | **yes (new)** | n/a | `TestRemoveButtonTooltip` |
| Image actions: Expand | AttachmentImageActions, `UvHM` | "Expand", 300, left-bottom | yes | n/a | — |
| Image actions: Download, Open in new window, More actions | AttachmentImageActions, `UvHM` | 300 | not drawn (Clack draws Expand only) | n/a | — |
| File actions bar | FileViewerActions / FileActions | per button, 300 | yes (texts not rechecked in this sweep) | n/a | — |
| Thread replies bar | ViewThreadButton, `T9mV` | a tip for meeting and discussion threads ("View thread about this meeting", "Open discussion") | none (Clack has no meeting or discussion threads) | n/a | — |
| Missing member | MissingText | "This person…", 150 | yes | n/a | — |
| Unread banner | UnreadStopPreview, `AEjl` | "Jump" (⌘J), "Mark as read" (Esc), 300, bottom | yes | n/a | — |
| Thread jump to bottom | ThreadScrollJumpButton, `duay` | "Jump to bottom", 300, left | not drawn | n/a | — |
| Sidebar: truncated name | BaseChannelName, `C80D` | the whole name, 600, top, only when truncated | **yes (new)** | yes (the row's native) | `TestSidebarTruncatedNameTooltip` |
| Sidebar: shared / code channel | BaseChannelName | team list / code sentence, 600, top | yes | yes | — |
| Sidebar: section heading ⋯ and + | (none) | aria-label only | none | n/a | — |
| Sidebar: draft pencil, outbox | SidebarDraftIcon, outbox | per site | yes (texts not rechecked in this sweep) | n/a | — |
| Header: notifications | NotificationsButton via NotificationsButtonMenu (`8xXK`) | "Edit notifications" + "Current: …" (+ mobile line); DM "Mute / Unmute conversation", 150, bottom | **yes (new)** | n/a | `TestNotifyButtonTip` |
| Header: huddle, its caret | HuddleHeaderButton | per state, with the leave shortcut | yes | n/a | — |
| Header: kebab, search, members | (none found) | aria-label only | none | n/a | — |
| Composer: Aa, emoji, mention, shortcuts, video, overflow, formatting buttons, attach | ComposerButton (`aiGo`), EmojiToolbarButton (`hzkp`), FileMenuButton | label + shortcut, **500**, top (attach top-right) | yes (now 500; was 450) | yes (OwlMode) | `TestComposerButtonTipDelay` |
| Composer: Send, Schedule | SendButton, `2LvR` | "Send now" + Return, 150, top-right | yes | yes | — |
| Composer: too long | TextyLengthWarning, `X/Xq` | count, 150, top-right | yes | n/a | — |
| Thread pane: close | ExpandSecondaryViewButton, `VWbZ` | "Close", 300, bottom-right | yes | n/a | — |
| Thread pane: search | ThreadSearchButton, `tTq3` | "Search in thread", 300, bottom | yes | n/a | — |
| Thread pane: title | ThreadTitleLabel, `CiO7` | the title, "Edit thread name" | yes | n/a | — |
| Split view: close | CloseSplitViewButton, `SSfk` | "Close", 300, bottom-right | yes | n/a | — |
| Threads, Unreads, Activity: reaction pill | Reaction (ReactionTip) | who reacted, 300 | yes | yes | `TestViewsReactionPillTip` |
| Threads, Unreads, Activity: add reaction | ReactionAddButton | "Add reaction…", 300 | yes | yes | — |
| Activity: tabs, filters, bulk actions, row marks | ActivityInboxSavedViewTabs, ActivityFilter*, ActivityRowContent* | per site, 150 | yes (texts not rechecked in this sweep; now 150 ms) | n/a | — |
| Emoji picker: emoji | EmojiListEmojiButton, `+y5+` | "Right click to customize skin tones", 500, top, only for an emoji with skin tones | not drawn (Clack's picker has no right-click skin-tone customiser to point at) | n/a | — |
| Huddle toolbar buttons | HuddleToolbar* (mute, camera, screenshare, leave, …) | per state, 150 | yes in the dock, bar and window (texts not rechecked in this sweep; now 150 ms) | n/a | — |

Not drawn means Clack doesn't draw the control at all; that's a missing
feature rather than a missing tooltip.

## A message's time is a link

Slack's `Timestamp` with a link (boot-shared `JO00`) is an `<a>` (`CFnj`)
to the message's permalink (`getPermalinkForMessage`, `1PIM`), class
`c-timestamp`: the hand cursor, the label underlined under the pointer,
TimestampTooltip after 300 ms. `MessageTimestamp` (`azta`) makes its click
`handleTimestampClick` (`Gi27`): with Cmd (Ctrl off macOS, `isCmdKey`) on
a reply, or on a message the view isn't showing, the message's thread
opens in a tile, highlighted; any other click focuses the message list at
it and opens the permalink, which routes to the message and highlights
it. A right-click is a link's: the shell's link menu. Clack:
`popups.TimeLink`, `App.OpenMessageTime` (jump.go), the menu through
`popups.NewLinkMenu` (`TimeLinkMenu`).

| Where | Slack | Clack | Test |
|---|---|---|---|
| Channel: header time, gutter time | MessageTimestamp, link + handleTimestampClick | yes | `TestMessageTimeIsAPermalink` |
| Thread pane: a reply's time | same: the thread at the reply; highlighted per jumpToThreadReply (`6IaZ`: not under the nested view when the navigation came from the thread's own conversation) | yes | `TestMessageTimeIsAPermalink` |
| Threads view | ThreadSenderAndTimestampGeneric's MessageTimestamp | yes | `TestThreadsTimeIsALink` |
| Search results | SearchMessage's renderTimestamp: link, `full` (no tooltip), onTimestampClick only prevents the navigation (the card's click opens the result) | yes | `TestSearchCardTimeIsALink` |
| Activity rows | ActivityTimestamp (`Jsmq`): a plain `<span>`, no link, no tooltip | unchanged (plain) | — |
| Activity's opened message | the channel or thread pane's | yes (those panes') | — |

Keyboard: the `<a>` takes Tab focus and Enter in Slack. Clack's message
list has no per-link tab stops yet, so the time is reached with the
pointer only.
