# Message rows audit: Clack vs Slack

2026-11-10, branch `audit-messages` from `ui`@b508b79. One row per element of a channel message row, plus the thread pane where Slack draws it differently.

How it was checked:

- **Slack, source.** Build 407aff01646b, the slack-datamine-plus capture of 2026-11-10. I fetched these public CDN chunks without a cookie: `gantry-v2-async-client-boot-shared.a9a60ec7e2e80dee`, `gantry-v2-async-client-boot-render.c02799ae8f8de665` and `client-boot.1c8b2fa630ae65f2`. The CSS is `client-boot-styles.cf726c4f2eaecc74` and `slack-kit-gantry-v2-styles.f859766a17f82b2a`, the same files compact-shots and TestRowHeightsMatchSlack use. The page HTML of that build gives the light and dark token values. Label icons are glyphs of Slack's icon font `slack-icons-v2-6509901.woff`, read with fontTools. Module ids are boot-shared's unless a row names another chunk.
- **Slack, live.** Not opened for this audit. Nothing on the real account was read or written, and no screenshots of Slack exist.
- **Clack.** The code at ui@b508b79, and the synthetic #release channel of `internal/ui/messages/compact_shots_test.go` on internal/fakeslack (made-up people and messages). Screenshots come from CI: the new `messages-audit-shots` workflow (artifact `messages-audit-shots`: `audit-thread-labels-{light,dark}.png`, `audit-reaction-tip-{light,dark}.png`) and `compact-shots`, which puts Clack's rows next to the same rows in Slack's markup under Slack's CSS and checks every height against Chrome.
- **Ownership.** `messages/list.go` and `messages/host.go` belong to the list agent, and the menus live in `internal/ui/popups`. Rows in those files say what Slack does and what to change. This branch doesn't edit them.

Status: **matched** (no change needed), **fixed** (on this branch), **open** (still different, with the reason).

No row that TestRowHeightsMatchSlack measures changes height. The new labels only appear on pinned or broadcast thread rows, and its fixture has none.

## Hover toolbar

Slack's MessageActions (L2dv) draws the HOVER list of the actions selector kctM, keeps the ids `hasButtonForAction` allows, and ends with MessageActionButtonEllipsis.

| Feature | Slack (evidence) | Clack before | Status |
|---|---|---|---|
| Buttons and order, channel message | Quick reactions when on, Add reaction… (rds8), Reply in thread (t8wB), Forward message… (6bWJ), Add to-do (Yoyn, with the to-dos experiment; Save for later, oMi4, without it), Add to list… (6r+G), then More actions. AI accounts add Summarize and Explain, and the jump action only shows outside the message pane | quick reactions, then the same six | **matched** (icons per FIDELITY #6) |
| Button box | `.c-message_actions__button`: 32px, radius 8, 18px icon in sk_foreground_max_solid; hover rgba(29,28,29,.04), the icon at scale 1.2 | same | **matched** |
| Position | `c-message_kit__actions--default`; `--above` puts it at top -28 for grouped rows and every Compact channel row | same rules | **matched** |
| Reply button tooltip | t8wB: "View thread" in the Threads view or when you can't reply and it has replies; "Reply to thread" when it has replies or is a reply; else "Reply in thread" | always "Reply in thread" (`tbTips`) | **open**: list.go. The context menu already uses this rule (popups `ctxHoverItems`) |
| Reply button in the thread pane | t8wB returns nothing for THREAD, MEDIA_VIEWER_THREAD and SAVED_FOR_LATER_PAGE containers | shown | **open**: list.go |
| Forward button | 6bWJ: "Forward message…" when private-message forwarding is allowed (Db0x), else "Share message…"; "Forward file…" / "Share file…" for file messages, "Share canvas…", "Share list…"; none for system messages (with system_messages_redesign_m1), unshareable messages (canBeShared) or document comment roots | always "Forward message…" | **open**: list.go; Clack doesn't read the forwarding setting |
| Add to-do on a to-do | Yoyn: a message whose `saved.todo_state` is `to_do` gets "Complete to-do" with check-circle-filled (`c-message_actions__button--complete_todo`); a saved bookmark still gets "Add to-do" | any saved item gets "Remove from to-dos", and the click removes it | **open**: `core.Message.Saved` merges the saved and to-do lists (`loadSaved` reads both filters), so a row can't tell a to-do from a bookmark. Needs core to keep `todo_state`, then list.go |
| Add to list… | dispatches `add_to_list`, Slack's add-to-list flow (thunk t9) | toast "Lists aren't supported yet" | **open**: a stub. Clack has no list picker; it needs one plus a `slackLists` record write. list.go |
| More actions button and menu anchor | "More actions", tooltip top-right, the menu's top 32px above the button's centre | same | **matched** (FIDELITY #26) |
| When the toolbar shows | `shouldRenderActions`: not while editing, and not on temp messages (isTempMsg, so pending and failed sends), unknown, hidden, `thread_hidden`, DLP-tombstone or restricted messages | not while editing, nor on tombstones, pending or failed sends | **matched** |

## The ⋯ menu

MessageActionsMenu (2rcC) builds groups in this order: P (context menu only), F (jump, overflow only), Edit (5Ech), then mark unread / to-do / reminder / follow (eJ), copy (eR), quote reply, create a code channel (GUK3), the AI items, the submenus (Organize, List, Connect to apps), broadcast and workflow items, View message activity, Slackbot feedback, and last flag / report / remove response / delete (3eXV).

| Feature | Slack (evidence) | Clack before | Status |
|---|---|---|---|
| Your own message | groups split by ·. Edit message E · Mark unread U, Add to saved ⇧⌘S (with to-dos), Add a reminder M, the follow item · Copy link L, Copy message ⌘C · Organize ▸ (Pin to channel P), Connect to apps ▸ · Delete message… (delete, danger) | same rows and keys | **matched** |
| Someone else's message | no Edit or Delete for a member. "Report to Slack" (tH) when `isContentReportingEnabled` and you didn't send it; "Flag this message…" (ed) when the org has content review | no report or flag rows | **open**: Clack doesn't read `isContentReportingEnabled` or `content_review_enabled` (popups) |
| Bot and app messages | as above, plus "View workflow details" (t7) for a `trigger_id` from a workflow bot and "Copy workflow ID" (q) for `workflow_id` | no workflow rows | **open**: popups |
| Follow item | ojBT picks the label from the thread subscription (`.subscribed`): "Turn off notifications for replies" with notifications-off, else "Get notified about new replies"; hidden when you're neither a member nor subscribed | label from authorship: your own message says "Turn off…" | **open**: popups. `core.Message.FollowingThread` already carries `subscribed` |
| Mark unread in the thread pane | jFAp only runs in the message pane and the unreads view; in a thread JsXu's "Mark unread" marks the thread unread | marks the channel unread from that reply | **open**: popups |
| Broadcast replies | the sender gets "Remove reply from channel…" ("…from conversation…" in a DM); a thread reply gets "Send reply to this channel" (T, thread containers) | neither | **open**: popups |
| Pin label in DMs | getPinActionLabel: "Pin to this conversation" / "Un-pin from this conversation" outside channels | always "…channel" | **open**: popups |
| Reminder label on a to-do | ih0g: "Edit reminder" when the message is a to-do, else "Add a reminder" | always "Add a reminder" | **open**: same core gap as Add to-do |

## Right-click

| Feature | Slack (evidence) | Clack before | Status |
|---|---|---|---|
| Message menu | MessageActionsContextMenu (8ToL): kctM's CONTEXT branch first (Add reaction…, the reply action outside the thread pane, Forward message…, Add to-do or Save for later), then the ⋯ menu | popups `NewMessageContextMenu`, same group and labels | **matched** |
| Links, mentions, images, files, code, previews | each opens its own menu (docs/EMBEDS.md for previews) | `contextMenuFor` routes the same way | **matched** |

## Reactions

| Feature | Slack (evidence) | Clack before | Status |
|---|---|---|---|
| Pill | `.c-reaction`: 24px, padding-inline 8, gap 4, radius 9999, 12px count, margin 0 4px 4px 0; the bar `margin: 8px 0 -5px` | same; heights pinned by TestRowHeightsMatchSlack | **matched** |
| Your reaction, light | `.c-reaction--reacted`: base-hgl-1 #e3f8ff, content-hgl-1 #1264a3 bold, a 1px base-inv-hgl-1 ring; `:hover` is the same | #e3f8ff, but #d5f3ff under the pointer | **fixed** |
| Your reaction, dark | `.sk-client-theme--dark .c-reaction--reacted`: solid base-inv-hgl-1 #0a77a7, no ring, content-pry #f8f8f8 | 20% #1d9bd1 with a #1d9bd1 ring and blue count | **fixed** (themes still recolour the light ring and text through the link token) |
| Count | `formatNumber`: en-US grouping, 1,234 | 1234 | **fixed** |
| Add reaction button | ReactionAddButton after the pills when you can react | always drawn | **matched** (FIDELITY #25) |
| Many reactions | ReactionBar (Ltiy) collapses past 5 reactions that don't fit two lines: the pills that fit, then "N more" (tooltip "Show more reactions", bold when one of the rest is yours); expanded, "Show less" | every pill, wrapping on | **open**: changes the height of busy messages and needs a per-message expand state; not done here |
| Skin tones | getReactionGroups (hzIJ) puts every skin tone of one emoji in one pill | a pill per name | **open** |
| Tooltip text | getTip (Qvn6): every reactor by name, joined like listify ("A and B", "A, B, and C"), you as "You" first or "you" after, "others" past 50; only you: "You (click to remove)"; then "reacted with :name:" as a TooltipSubtitle (regular weight, inverse.secondary) | at most three names then "and N others", one bold run | **fixed** |
| Tooltip box | ReactionTip in a Tooltip: padding 8×12, radius 8, 13px/1.25 bold, centred, max-width 200 (400 past 9 reactions); the emoji LARGER in `.c-reaction__tip--emoji` (64px white tile, radius 4, 56px emoji, margin-bottom 4); 8px over the pill with the arrow (a 9px square turned -45°, 6px of it under the box); delay 300 | 44px tile, radius 8, 36px emoji, max-width 220, no arrow, 6px over the pill | **fixed** |
| Tooltip in dark mode | the page's dark tokens give base-inv-pry #212428 and content-inv-pry #f8f8f8, with a 1px otl-pry drop shadow | #e8e8e8 with dark text, like every Clack tooltip | **open**: the light dark-mode tooltip is shared by theme, popups, messages and richview; it should change in one place. The subtitle keeps the text colour in dark until then |
| Pill aria-label | Reaction (9FoR): "{n} reaction(s), react with {name} emoji", `_` and `-` read as spaces | the tooltip text | **fixed** |

## Edited and deleted

| Feature | Slack (evidence) | Clack before | Status |
|---|---|---|---|
| "(edited)" | EditedLabel (Y5SQ): a no-break space then "(edited)", 13px, sk_foreground_high_solid #868686, nowrap, after the last text block | same | **matched** (TestEditedLabel) |
| "(edited)" tooltip | Tooltip, delay 300: "{date} at {time}", the date from toCalendarDateOrNameDay with shortenMonth. That is Today, Yesterday or Tomorrow, else "Nov 7th", and ", 2025" unless shouldExcludeYear (this year, or last year within 31 days) | none | **fixed** (richview `Options.EditedTS`, `editedTip`) |
| Edited by someone else | "{dateTime} by {name}" when the element has `editedByUserId` | Clack keeps no editor id | **open**: core doesn't keep `edited.user` apart from the author |
| Deleted message | MessageTombstone, 2X8J in build a84662d2d135 (PREFS-AUDIT): "This message was deleted.", the 20px trash, the reply bar under it | same | **matched** (heights 86 and 90) |

## Labels and broadcasts

MessageLabels (meLQ) draws one primary label: ephemeral, then broadcast, then pinned, then saved. Message (NnKv), ThreadRoot (3ZeZ) and ThreadReplyGeneric (oDUT) all wrap the row in it, and only ThreadReplyGeneric passes `showBroadcast`.

| Feature | Slack (evidence) | Clack before | Status |
|---|---|---|---|
| Pinned, channel | thumb-tack-filled (U+E547) in #e8912d, "Pinned by you" / "Pinned by {member}" / "Pinned", 13px at 70%, the row tinted rgba(242,199,68,.1) | same | **matched** (height 71.06) |
| Pinned, thread pane | the same label on the root and on replies, at padding-left 16 (`.c-message_kit__thread_message .c-message_kit__labels__label`) | tint only, no label | **fixed** |
| "Pinned by {member}" link | MemberLink (25Rg): the name opens the profile and has the profile hover card | plain text | **open** |
| Broadcast, thread pane | a reply also sent to the conversation: the jump glyph (U+E084) in sk_highlight, "Also sent to the channel" ("…as direct message" in a DM, "…to the group" in a group DM, 2D+h) as a ButtonLink in the label's colour, underlined under the pointer, opening the message in the conversation; pinned too: "{broadcast} ∙ {pinned}"; the label tint | nothing | **fixed** |
| Broadcast, channel | ThreadBroadcastPreamble (UsKL): "replied to a thread:" and the root's preview | same | **matched** (PREFS-AUDIT, height 74) |
| Ephemeral | "Only visible to you" with eye-filled (U+E584) in the label's own colour (no rule colours it) | an orange 14px SVG eye | **fixed** |
| Saved and to-dos | LaterLabel (wRj5): "Saved for later" with bookmark-filled, or "Saved for later · View Saved" under the saved rename; with to-dos and a due date, "To-do added · View to-dos" or "To-do due {date} · View to-dos" with check-circle-filled; the text blue and bold (`.c-message_kit__labels--later`), the row tinted #1d9bd11a; nothing for completed (`date_completed`) or archived items | nothing | **open**: needs each message's `saved` metadata (`date_due`, `todo_state`, `date_completed`, `is_archived`); core keeps one bool |
| Custom response | MessageLabels draws no label for `custom_response`, only the #1d9bd11a tint | "Custom response" with a pencil when reskin_custom_responses is off; no tint | **open**: custom_response.go's owner; its test expects the label |

## Bot and app rows

| Feature | Slack (evidence) | Clack before | Status |
|---|---|---|---|
| APP / AGENT / WORKFLOW tag | BaseAppBadge after the name | same | **matched** (FIDELITY #40) |
| Section | `.p-section_block`: 15px/1.46668, margin 8px 0 4px (0 on top as the first block), accessory margin 0 0 4px 8px, an 88px image, fields at most 600 wide with 12px above | same | **matched** (bk-elements, bk-surfaces) |
| Context | `.p-context_block`: a wrapping row, margin 4px 0; text 13px/1.38463 in sk_foreground_max_solid | same | **matched** |
| Divider | `.p-divider_block`: 1px otl-sec, margin 8px 0 | same | **matched** |
| Header | `.p-header_block`: 15px weight 900, margin 1em 0 4px, 4px on top as the first block | same | **matched** |
| Buttons with no live handler | Slack posts the block action | `richHandler.Button` toasts "Interactive buttons aren't supported yet" when there's no URL | **open**: host.go, a stub; live Block Kit elements go through blockkit, this is the fallback |

## Attachments, unfurls and link previews

docs/EMBEDS.md has the full rules, read from build a84662d2d135. I didn't re-read them for this audit. I only checked that AttachmentDeleteButton, renderV2Attachment and MessageAttachmentGeneric are still in today's chunks.

| Feature | Slack (evidence) | Clack before | Status |
|---|---|---|---|
| Classic attachment colour bar | MessageAttachmentGeneric v1: 4px `__border`, radius 8, `color` or base-ter | same | **matched** |
| Block Kit attachment | renderV2Attachment: the bar, margin-right 12, the blocks, "Added by <app>" | same | **matched** |
| Link preview card | MessageAttachmentDefaultV3: the 106px row card, fixed-layout service cards, the image card | same | **matched** |
| Remove-preview x | AttachmentDeleteButton (qrgA): a 20px circle at -9/-9 on v3 cards, the rail on v1/v2, "Remove preview?" / "Remove", chat.deleteAttachment | same | **matched** |
| Admin options | "Also block previews from this website", removing others' previews | not offered | **open**: the test account can't see them (EMBEDS.md) |

## List lines (list.go, the list agent's)

| Feature | Slack (evidence) | Clack before | Status |
|---|---|---|---|
| Day divider | `.c-message_list__day_divider` under ia4: padding 20px 0, a 1px sk_foreground_low_solid line, a 28px pill (13px bold, padding 0 16, the 1px ring and shadow), sticky at the top | same | **matched** (FIDELITY #11) |
| Day pill menu | `--jump_to_date` pills open Jump to… (Most recent, Last week, Last month, The very beginning, Jump to a specific date) | the chevron is drawn, a click does nothing | **open**: list.go |
| "New" line | UnreadDivider (VxYf): a 1px sk_raspberry_red rule and "New" 13px bold, margin-right 15, padding 0 4 | same | **matched** |
| Back to the newest | ArchivesBanner (boot-render bU1n): only when the loaded history stops short of the newest message (`reachedEnd === false`), a 28px #1264a3 pill, arrow-down and "Latest messages" (tooltip "Jump to latest messages" with AI summaries) | "Jump to recent" whenever the list is scrolled up a few rows | **open**: list.go |
| Unread banner | the message pane's top banner: "N new messages" ("New messages", "+N" when the count is a guess), arrow up or down, and an x "Mark as read (esc)"; a #1264a3 pill 28px tall, radius 14 | none | **open**: list.go and host.go |

## Counts

59 rows: 25 matched, 10 fixed, 24 open. The open ones, by reason:

- Another agent's files (14). list.go and host.go have 7: the reply tooltip, the reply button in the thread pane, the forward label, the day pill menu, the back-to-newest pill, the unread banner and the Block Kit button fallback. popups has 6: report and flag, the workflow rows, the follow label, mark unread in a thread, the broadcast reply items and the DM pin label. custom_response.go has the custom response label.
- Data Clack doesn't keep (4). Each message's saved metadata is behind Complete to-do, Edit reminder and the saved label, and the editor's id is behind "by {name}".
- Not built yet (5): the add-to-list picker, collapsing many reactions, skin-tone groups, the member link in "Pinned by", and the theme-wide dark tooltip.
- Not visible to the test account (1): the admin preview options.

## Tests

- `TestReactionTipText`: Slack's names, serial comma, "others", "You (click to remove)", the aria-label and the grouped count.
- `TestReactionTooltipDraws`: the 64px tile, the arrow, 8px over the pill, inside the window.
- `TestCSSReactionTooltip`: row rules don't reach the tooltip, tooltip rules do.
- `TestEditedTipLikeSlack`, `TestEditedLabelTooltipTarget` (richview): the date rules, and the "(edited)" word as the tooltip's target.
- `TestMessagesAuditShots`: the screenshots above.
