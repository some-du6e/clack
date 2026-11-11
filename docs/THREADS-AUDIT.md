# Thread pane audit: Clack vs Slack (2026-11-10, branch `audit-threads`)

The thread pane row by row: the header and its menu, the parent and the "N replies" separator, the composer, the replies and the unread marker, thread notifications, the channel's reply bar, and the pane's width and scroll position.

How it was checked:

- **Slack, source.** Build 407aff01646b (slack-datamine-plus captured it on 2026-11-10), fetched from the public CDN without a cookie: `client-boot` 1c8b2fa630ae65f2, `gantry-v2-async-client-boot-shared` a9a60ec7e2e80dee, `client-boot-render` c02799ae8f8de665, `client-boot-vendors` 7b2d23cc68bf0a91, `client-thread-view` 893b1396be8bd8b5 and the stylesheet `client-boot-styles` cf726c4f2eaecc74. The module ids below are from those files. The pane is `puxw` (Thread, ThreadsFlexpane, getThreadRowsAndKeys, getThreadStartPosition), its frame `XVla` (Flexpane, the header), the separator `EWXh`, the unread divider `VxYf`, the banners `ntqA` and `JwHt`, the ⋮ menu `IO7k`, the bell `5mp4`, the reply bar `v8ac` with its text in `BZhr`, the notification rules `gMhK` and `gaJC`, and the column widths `aTtQ` with the resizer `7M48`.
- **Slack, live.** The agent-browser `whatever` profile (the test account) in a tab of my own, only in C0B4DEFKFCJ, read-only. Before Slack loaded, network routes aborted `conversations.mark/join/leave/open/close`, `subscriptions.thread.mark/add/remove`, `chat.*`, `reactions.*`, `users.prefs.set`, `users.setPresence`, `drafts.*`, `activity.mark*`, `activity.clear*`, `saved.*` writes, `stars.*`, `pins.*`, `bookmarks.*`, `rooms.*`, `calls.*`, `huddles.*`, `dnd.*` and the old `*.mark` methods. I read class names, `data-qa`, aria labels and boxes, plus the reply bars' own text ("1 reply", "1 day ago"). One thread was opened from its reply bar. No screenshots were taken and no message text was saved. The first route set also aborted `subscriptions.thread.get`, a read the pane waits for, and after a reload with narrower routes every request in the tab failed, so the thread's rows, its composer and the ⋮ menu were never seen live. The tab was closed and the routes removed. What the live look settled: the client root carries `p-ia4_client--nested-secondary-view`, the pane is 484px wide in a 1666px window, its header is `p-flexpane_header--smaller` with only More actions and Close, the resizer is an 8px `p-resizer--default` strip, and the reply bars' description holds only the hover span.
- **Clack.** internal/fakeslack's `#th-audit` (`Size.ThreadsAudit`, internal/fakeslack/threads_audit.go) holds one thread per case: read to the viewer's own reply, every reply unread, the first unread reply the viewer's, 40 replies not followed, read, `reply_count` 7 with three replies served, and seven repliers. The tests are in internal/ui/messages/thread_audit_test.go, internal/ui/threads_audit_test.go and internal/core/thread_read_test.go. The `threads-audit` workflow saves the pane's states, light and dark, plus Slack's markup for the same states under Slack's CSS (.github/scripts/threads_ref.py), as the `threads-audit-shots` artifact.

The account's experiments the pane depends on are now in core's defaults and fakeslack, as seen live: `nested_secondary_view`, `secondary_view_default_width`, `smaller_secondary_headers` and `unread_thread_indication_in_replybar` on, `thread_header_mute_button` off. Clack reads them through `core.App.Experiment`, so it follows the account when experiments.getByUser answers.

Status: **matched** (no change needed), **fixed** (on this branch), **open** (still different, with the reason).

## Header, close and the ⋮ menu

| Feature | Slack (evidence) | Clack before | Status |
|---|---|---|---|
| Title | "Thread" (renderThreadTitle) in `.p-flexpane__title_container`, 18px/24px Black. Under `p-flexpane_header--smaller` (smaller_secondary_headers, on live) it is 15px/22px Black | 18px/24px | **fixed**, follows the experiment |
| Header box | 49px (`.p-flexpane--ia_details_popover .p-flexpane_header__primary`), padding 0 12px 0 16px; live 49px | same | **matched** |
| Conversation name | ThreadSubtitle (NCqL) 13px/18px at 70%, 8px after the title, hidden when the primary view shows that conversation; a channel is InlineChannelEntity (X3qV: className `p-threads_flexpane__header_channel_name`, truncate) with InlineChannelIcon (`data-inline-channel-type-icon`) and the name in `c-channel_entity__name` | same; plugin CSS saw no elements in it | **matched** (branch thread-header); the entity's elements are Slack's for plugin CSS (plugins-leftovers: StreamerMode blurs a private channel's name) |
| Controls | Close, More actions, the bell when shown, then ThreadSearchButton's plugin content, 28px buttons 4px apart under `--smaller`. Live: More actions at x 412 and Close at 444 in the 484px pane | 6px apart | **fixed**; a plugin's own button there (ThreadJump's `c-icon_button c-icon_button--size_medium`) draws with Slack's Slack Kit rules as an item of `.p-flexpane_header__primary`'s flex row, left of More actions; ThreadSearchButton's own null takes no gap (plugins-leftovers, `thread-jump-button-taut.png`). ThreadSearchButton itself (under `find_in_thread`) is **open**: Clack has no find-in-thread bar |
| Bell | `thread-mute-button` (5mp4) only under thread_header_mute_button once the root is loaded (XVla `eX`). Live: no bell, so Follow sits in More actions | always drawn | **fixed**, gated on the experiment |
| Tooltips | the bell's 300ms below, More actions below at BaseTooltip's 150ms, Close "Close" bottom-right at 300ms (`eK` is 300 when the header has extra buttons) | 450ms each, no Close tooltip | **fixed** (popups.Tooltip gained `BelowRight`) |
| More actions rows | SecondaryHeaderMore for a Thread: the subscribe action when the header has no bell (ojBT: "Get notified about new replies" / "Turn off notifications for replies"), then Copy link (L) | the same two rows | **matched** |
| Copy link | copyLink (5FS0) writes the permalink to the clipboard and shows nothing | a "Link copied" toast | **fixed** |
| Summarize thread | needs Slack AI thread summaries (7KpF) | none | **open**: Clack has no Slack AI |
| Start huddle in thread | canUserPostInChannel and doesChannelSupportHuddles (o/IU, zq6z) | none | **open**: Clack starts huddles in channels and DMs only |
| Create a code channel from thread | its own menu group when the thread can become one | none | **open**: Clack doesn't create code channels |
| Open in split view, Open in new window | the surface group (eXbF, HWmf) | none | Open in split view **fixed** (split-view, 3c9bc068: `popups/actions_thread.go` adds Slack's row outside the split view, docs/SPLIT-VIEW.md). Open in new window **open**: Clack has no second window |
| Menu position | Popover "bottom-left", moved inside the window with its 16px allowance (L1PI keepInBounds) | right-aligned to the button | **open**: never seen live (the pane didn't load) |
| Expand view | ExpandSecondaryViewButton only under channel_resources_remove_tabs | none | **matched** (the account's channels have tabs) |
| Find in thread | ThreadSearchButton and ThreadSearchBar under find_in_thread | the boundary, empty natively | **matched** (off for the account; ThreadJump still draws there) |

## Parent, "N replies" and "Also send to"

| Feature | Slack (evidence) | Clack before | Status |
|---|---|---|---|
| The count | ThreadSeparatorRowGeneric (EWXh) says `Math.max(1, reply_count)` of the root (getReplyCount), not how many replies loaded | the replies loaded | **fixed** |
| Separator box | 24px row, 13px/18px at 70%, padding-left 16, the rule to width − 22 at y 13 | same | **matched** |
| No separator without replies | getThreadRowsAndKeys adds it only with replies | same | **matched** |
| The separator as the marker | separatorType "unread" when the first reply is past initialLastRead and isn't the viewer's: the rule turns raspberry red (#de4e2b in dark) and ends at "New", 12px bold, line-height 8, padding 2px 4px 4px. Once read it is "read", the ghost, its rule at half opacity 3s later | the plain separator | **fixed** |
| "Also send to" | BroadcastControls (63o2): "Also send to" and the channel in bold, "Also send as direct message" in a DM, "Also send to the group" in a group DM | a group DM said "Also send as direct message" | **fixed** |

The parent row itself is the message row's (ThreadRoot through MessageKit); the message row audit covers it.

## Composer

| Feature | Slack (evidence) | Clack before | Status |
|---|---|---|---|
| Placeholder | MessageInput: "Reply to thread…" under nested_secondary_view (live: the client root has `p-ia4_client--nested-secondary-view`), else "Reply…" | "Reply…" | **fixed**, reads the experiment |
| Where it sits | the list's last row, so a short thread has it right under the last reply | same | **matched** (docs/FIDELITY.md #4) |
| Focus on open | the reply bar's openThread sets focusInput and the input focuses on mount | not focused | **fixed** |
| Escape | the secondary view is a dialog (RenderedViewContents): Escape goes to the pane's handler first, onEsc marks the thread read when shouldMarkThreadAsRead, and otherwise the view closes | marked the channel read | **fixed** for the reply composer. From an edit, the list's keyboard focus or an open autocomplete, Escape still goes Clack's old way (the shell's binding marks the channel read) |

## Replies, the "New" line, jumping and following

| Feature | Slack (evidence) | Clack before | Status |
|---|---|---|---|
| Read state | the thread subscription's lastRead. getDerivedStateFromProps gives a followed thread an initialLastRead (that lastRead) when its newest real message (maxRealTs) is past it, once per open | none: core dropped the root's `last_read` | **fixed**: core keeps it from conversations.replies, thread_marked, thread_subscribed and its own marks |
| "New" divider | UnreadDivider (VxYf) before the first reply past initialLastRead that isn't the viewer's or a temp message: a 1px rule and "New" 13px bold 15px from the right edge, #de4e2b in dark. The reply after it keeps its group (prevTs) | none in threads | **fixed** |
| After reading | "unread_marked_read": the divider becomes the ghost, its rule at half opacity 3s later. Esc drops it, and the next open decides afresh | n/a | **fixed** |
| Marking read | checkUnreads: 350ms after the pane opens, with the window focused and no overlay up, a followed thread is marked to maxMarkableTs (subscriptions.thread.mark) | the pane never marked a thread | **fixed**, through markLastRead with threadTs so plugins see it |
| "N new replies" banner | ThreadUnreadBannerGeneric (ntqA, JwHt): "↓ N new replies" with a close, 28px, #1264a3, at the bottom (8px) or "↑" at the top (5px) while the marker is out of view and lastRead is short of maxMarkableTs. The label scrolls the marker 100px down, the close marks the thread read | none | **fixed**. It lasts only until the pane marks the thread read |
| "j" | onKeyDown: j with the desktop modifier jumps to firstUnreadTs or announces "No unread messages" | none | **open**: not ported |
| "Jump to recent" | the message pane's own pill, not the thread pane's | drawn in threads too | **fixed**: channels only |
| "Latest messages", "Back to top" | ThreadJumpToLatestBanner under fe_long_threads_jump_to_latest | none | **open**: the experiment's state wasn't seen |
| Follow and unfollow | ojBT's names, subscriptions.thread.add / remove, in More actions while the bell is off | same | **matched** |
| Subscription events | thread_subscribed and thread_unsubscribed carry last_read | the flag only | **fixed** |
| Loading | "Loading thread…" on the first load, a "Loading replies…" row (13px italic, 96px) while paging | skeleton rows | **open**: not built in this pass |
| Load error | ThreadsError: :mag: at 64px, "Couldn’t load thread" 22px Black, "If the problem persists, please drop us a line.", Try Again. thread_not_found says "This message was deleted." | the list's "Couldn't load messages · Retry" | **open**: not built in this pass |

## Thread notifications

| Feature | Slack (evidence) | Clack before | Status |
|---|---|---|---|
| A reply on the message path | shouldShowNotificationForChannelMessage lets a reply through only when checkForMentionInMessage finds the viewer, without highlight words; a reply's @channel and @everyone don't count, @here does. shouldShowNotificationForDirectMessage never shows a DM's reply | every reply under "All new posts", every DM reply | **fixed** |
| A user group in a reply | counts when the viewer is in the group | not counted | **open**: core doesn't know the viewer's groups per message |
| Followed threads | the server's desktop_notification event (maybeShowNotification): a reply in a known conversation, quiet while that thread is open with the window focused or already read to the reply; the body is the event's content | the event was dropped | **fixed** |
| Title and body | getNotificationTitle with one account signed in: "New message from {name}" in a DM, else "New message in #channel" (a private channel's bare name, a group DM's members). The body outside DMs is "{sender}: {text}" | "{name} in #channel" | **fixed** |
| "New thread message in …" | the title under grouped_notifications | n/a | **open**: the experiment's state wasn't seen |
| Sound | desktop_sound with the banner | same | **matched** |

## Opening a thread from the reply bar

| Feature | Slack (evidence) | Clack before | Status |
|---|---|---|---|
| Faces | renderFaces: reply_users without B00, up to five. Past five, four and a fifth with "+N" (reply_users_count − 5, "9+" past nine) over rgba(0,0,0,.5), 15px bold white | the first five, no "+N" | **fixed** (core keeps reply_users_count) |
| Count | "1 reply" / "N replies", 13px bold link | same | **matched** |
| Last reply text | getThreadLatestReplyText (BZhr): "Last reply today at 3:23 PM" ("Today at 3:23 PM" for one reply), else "Last reply" and toTimeAgo, the time ago alone for one reply. Live: "1 day ago", "24 hours ago" | "Last reply yesterday at …", day counts truncated | **fixed**, on i18n's TimeAgo |
| At rest and on hover | unread_thread_indication_in_replybar (on live): the description holds only `reply_bar_hover_last_reply`, so nothing shows at rest and the last reply takes "View thread"'s place on hover | "Last reply …" at rest, "View thread" on hover | **fixed**, follows the experiment |
| "Unread" chip | at rest for a thread with unread activity (getUnreadThreadsCountForMessage over Activity's channel lists) | none | **open**: Clack doesn't load Activity's per-channel lists |
| Drafts and scheduled replies | "N replies & 1 draft", "… & N scheduled" with their icons | none | **open**: thread drafts aren't wired to the bar |
| Hover look | white with a 1px 13% border, radius 6, at most 600px, 34px tall, caret-right 18px at #868686. Live: 34px, radius 6 | same | **matched** |
| Click | openThread opens the thread and focuses its input | opened it unfocused | **fixed** |
| Modifier click | opens the thread in split view (not in BBM) | opens the pane | **fixed** (split-view: `App.OpenThread` takes the modifier, ZRE2/v8ac; docs/SPLIT-VIEW.md Entries) |
| Faces' clicks | not interactive (no profile card) | part of the bar | **matched** |

## Width, resizing and scroll position

| Feature | Slack (evidence) | Clack before | Status |
|---|---|---|---|
| Default width | getIdealSecondaryWidth (aTtQ): (region − sidebar) × .4 under secondary_view_default_width. Live: 484px = .4 × 1210 (1666 − 70 rail − 4 − 382 sidebar) | a fixed 400px | **fixed** |
| Limits | at least 300; the primary keeps 300 (maxWidth region − 300 with the nested view) | 300, the primary keeping 360 | **fixed** |
| Resizer | BaseResizer (7M48): an 8px strip on the pane's edge (live 8px), snapping to the default within 15px (mode "auto"); two presses within 500ms reset it | a 6px strip, no snap, no reset | **fixed** |
| Resizer keys | a focusable range input: arrows move it 10px, Home to the minimum | none | **open**: Clack's resizer can't take the focus |
| Remembered width | secondaryViewWidthIntent with its mode, manual or auto | `secondary_w` in ui.json, the 400 default saved as if dragged | **fixed**: 0 is auto, and the old 400 reads as auto |
| Start position | getThreadStartPosition: a saved scrollmark first; a thread you don't follow at the root; a followed one at its first unread reply 100px below the top, or at the reply it is read to | always at the newest reply | **fixed** |
| Scroll restore | maybeSaveThreadScrollmarks saves the place on close ("input" when stuck to the bottom) except in a public channel you aren't in, and the next open starts there | every pane kept its place | **fixed**: a preview starts over |
| Reply permalinks | the reply becomes startTs, 100px down when past initialLastRead | the reply two rows down, highlighted | **open**: not changed (a few pixels of offset) |

## Counts

60 rows: 13 matched, 31 fixed, 16 open.

## Live session side effects

None I know of. Every write and mark endpoint was blocked while the tab was open, the only clicks were the reply bar (which opens the thread) and the pane's Close, and nothing was typed.

## Not compared

- The parent row and the reply rows themselves (sender line, body, toolbar): the message row audit.
- The thread's composer formatting bar and attachments: the composer audit.
- The Threads rail view: docs/VIEWS-AUDIT.md.
