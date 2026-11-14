# Canvases

How Slack's web client loads, follows and edits canvases, and what Clack does instead. Sources: Slack's web client on the public CDN (build a84662d2d135, 2026-11-08; chunks `gantry-v2-async-quip-collab-webapp`, `-quip-collab-editor`, `-client-boot-shared`, `-client-canvas-view`), the method list from IzieStratt/slack-datamine-plus, docs.slack.dev for the public `canvases.*` methods, and read-only structure probes of Hack Club canvases. The probes printed only tag names, class names and attribute names; no canvas content was printed or saved.

## What a canvas is

A canvas is a Slack file with `filetype: "quip"`, `mode: "quip"`, `pretty_type: "Canvas"`. Behind it is a Quip thread (`quip_thread_id`). `files.info` on a canvas returns, besides the usual file fields: `title`, `title_blocks` (rich text, for emoji in titles), `canvas_creator_id`, `edit_timestamp`, `editable`, `access` (`read`/`write`/`owner`), `editors` (user ids), `canvas_readtime`, `show_badge` (the viewer never opened it), `is_starred`, `dm_mpdm_users_with_file_access`, `org_or_workspace_access`, `canvas_printing_enabled`, `has_rich_preview`, `source_canvas_id`, `quip_thread_id`. `url_static_preview` and `permalink` are Slack web pages (`/docs/<T>/<F>`); Clack never loads them.

Channel canvases are channel tabs. `conversations.info` → `channel.properties.tabs[]` has `{type: "canvas", id, label, data: {file_id, shared_ts, mute_edit_updates?}}`. The old `channel_canvas` tab type is still listed with `is_disabled: true`. `properties.canvas` is `{file_id, is_empty, quip_thread_id, is_migrated, is_open_by_default}`. DMs carry canvas tabs the same way.

## How Slack's web client does it

**Document model.** The web client embeds Quip's collaborative editor ("quip-collab"). It talks to the team host under `/canvas`:

| endpoint | what |
|---|---|
| `POST https://<team-host>/canvas/-/load-data?_x_version_ts=…` | loads the thread and its sections. The form field `request_binary` is a Quip protobuf (`pblite`); the answer is a binary `ResponseWrapper` |
| `POST /canvas/-/fetch-latest` | `Request{ThreadRequest{thread_id, client_thread_sequence, client_max_section_sequence}}` → the changes since that sequence (`ChangesData`) |
| `POST /canvas/-/edit-document` | sends edit transactions (binary) |
| `POST /canvas/-/save-diagnostics-report` | diagnostics |

`buildCanvasTarget` puts the path on the team's host. `applyCanvasAuth` sends the token as `Authorization: Bearer` on the same origin, or as a `token=` form field cross-origin. The sections and edits are Quip's internal protobuf schema, which isn't documented and changes with Quip's builds.

Around it, Web API methods: `files.info`, `files.open` (`{file_id, supports_read_only_check, last_updated}` → `{viewers: [{user_id, is_focused, …}], should_subscribe_and_ping}`), `files.close`, `files.ping`, `files.createCanvas` (`{title, markdown, reason}` → `{file_id}`), `quip.getControllerInitData`, `quip.thread.synchronize`, `quip.getMentions`, `quip.lookupFileId`, `quip.lookupThreadIds`, `quip.history.getVersions`/`restoreVersion`, `quip.cloneCanvas`, `canvases.listHeaders`, `canvases.getTemplates`, `canvases.notificationSections`.

**Live updates.** Opening a canvas runs the `openFile` thunk: `files.open`, then it records the viewers and subscribes the Quip delegate to the canvas's document events. The realtime gateway sends:

- `document_update {file_id, payload}`: an edit was saved. The client looks up the Quip thread id and calls `fetchThreadChanges`, which is `fetch-latest`.
- `document_change {channel: <file id>, payload: base64}`: transient sections (cursors, selections, presence). The client also sends its own as `{type: "document_change", user, channel, payload}`.
- `file_opened` / `file_closed`: viewers.
- The message subtypes `channel_canvas_updated` / `tabbed_canvas_updated` and the event `file_channel_canvas_reset`.

**Title.** A canvas has no title apart from its document. Quip derives it (`extractTitleMetadata` in `quip-collab-webapp`): a TITLE-style section if there is one, else the first non-empty heading, else the first five words of the first text section (new-document default content skipped), else "Untitled". `files.info`'s `title` is that projection, and the export has no separate title element: #clack's canvas opens with a plain `<h1 id>`, then a `<p class="default-content line">`. So Clack draws an opening heading once, as the title, with the byline under it (it was also drawn as the first block, so the title showed twice). A canvas that opens with anything else keeps the file's title above it. Editing that heading is what renames the canvas in Slack.

**Header.** The canvas header toolbar has "Open in side panel" (full view), "More actions" (…), the comments button, "Ask Slackbot" and "Ask Agentforce" (both behind experiments). Strings: "Open in full view", "Open in side panel", "Open in new window", "Copy link to canvas", "Created by {name}", "Last edited", "Edited by {memberName} {date}".

## What Clack does

Reimplementing Quip's binary sync protocol would mean reverse engineering an undocumented, versioned protobuf schema and its operational-transform rules. Clack uses the documented surface instead:

**Read.** `files.info`, then the file's `url_private`, which answers with Quip's HTML export of the document (`text/html`, starting with `<div class="quip-canvas-content">`). `slack.Client.Download` refuses every HTML answer, because a file URL answering with a page is usually a redirect to sign-in. So `slack.DownloadCanvasHTML` is the one exception, and it is narrow: the URL and every redirect hop must be a file URL (`files.slack.com`, `/files-pri/…`), the type must be `text/html`/`text/plain`, the body must contain `quip-canvas-content`, and the size is capped at 16 MiB. The export is never cached and never written to disk. Embedded images and files are resolved with `files.info` on their ids (at most 24 per canvas, 4 at a time); Clack never fetches Quip's relative blob URLs.

**The export's vocabulary** (`internal/canvas/parse.go`, from probes of 200 canvases):

| HTML | block |
|---|---|
| `h1` `h2` `h3` (`id` = section id) | headings |
| `p.line` | paragraph (empty = an empty line) |
| `div[data-section-style=5]` > `ul` > `li` > `span` | bullets; nested `ul` = next level; the wrapper's `style="--indent0: N"` is the base level |
| `data-section-style=6`, `li[value]` | numbered (value restarts the count) |
| `data-section-style=7`, `li.checked` | checklist |
| `blockquote` | quote (holds paragraphs and lists) |
| consecutive `p.prettyprint.line` | one code block, one `p` per line |
| `hr` | divider |
| `table[data-header]` > `tr` > `th`/`td` > blocks | table |
| `div[data-section-style=11]` > `img[src=/collab-slack-blob/<thread>/<F…>?size_name=…, width, height, alt]` | image (`div.empty` = placeholder) |
| `div.flexbox` > `div.flexbox-column` | columns |
| `p.embedded-link` | embedded link (unfurl) |
| `p.embedded-file` (`lnk[data-slack-file-id=sf:F…]`) | embedded file |
| `div.callout` | callout (Quip has a callout layout; no export of one was found, so this shape is a guess) |
| `b` `i` `del` `u` `code`, `span[style=background-color]` | inline styles |
| `lnk[href]` | link |
| `control > a` "@name" / "#channel" / "@here" | mentions. The export has the name only, no id |
| `control > img[alt=name, src=slack-imgs.com proxy]` | emoji (Clack uses the workspace's own image) |
| `control` with text | a date |
| `.default-content` (without `customized-default-content`) | Quip's default content the user never edited (`Block.Default`). Slack draws it transparent (`.default-content:not(.customized-default-content)>.content{opacity:0}`): the line keeps its height, no text |

The `id` attributes (`temp:C:…`) are Quip section ids. They are the same ids `canvases.sections.lookup` returns and `canvases.edit` takes.

**Live.** `files.open` when a view appears, `files.close` once no view has drawn it for 6 s (the shell has no unmount hook). Core listens to the raw gateway events through `SubscribeRaw` (`document_update`, `file_change`, `file_shared`, `file_channel_canvas_reset`, and the `channel_canvas_updated` / `tabbed_canvas_updated` messages). It coalesces bursts into one `canvas` event per 400 ms, and the view re-reads the export. The header's facepile shows the other viewers: `files.open`'s `viewers`, then `file_opened` / `file_closed` (`{file: {id}, viewer: {user_id}, event_ts}`) add and remove people. Clack doesn't send `document_change`, so other people don't see a cursor from Clack. With realtime off, the view polls `files.info` every `core.PollInterval` and refetches when `edit_timestamp` moves.

**Edit** (the public methods, all form-encoded with JSON values):

| action | call |
|---|---|
| change a block | `canvases.edit {canvas_id, changes: [{operation: "replace", section_id, document_content: {type: "markdown", markdown}}]}` |
| new block after one | `insert_after` (also `insert_before`, `insert_at_start`, `insert_at_end`) |
| delete a block | `{operation: "delete", section_id}` |
| rename | `{operation: "rename", title_content: {type: "markdown", markdown}}` |
| new standalone canvas | `canvases.create {title, document_content}` → `canvas_id` |
| new channel canvas | `conversations.canvases.create {channel_id, title?, document_content?}` → `canvas_id` (adds a tab) |
| share | `canvases.access.set {canvas_id, access_level: read|write, channel_ids: [...]}`, then the canvas's permalink is posted to each conversation (Slack unfurls it as the canvas card) |

Canvas markdown (`internal/canvas/markdown.go`): `#`/`##`/`###`, `- `, `1. `, `- [ ] `/`- [x] `, two spaces per nesting level, `> `, fenced code, `---`, pipe tables, `**bold**`, `_italic_`, `~~strike~~`, `` `code` ``, `[text](url)`, `![](@U…)` / `![](#C…)` mentions, `:emoji:`, `@here`.

**Editing in the UI** (`internal/ui/canvasview/edit.go`, `picker.go`). Only canvases whose `files.info` says `editable` can be edited.
- **Opening a block for editing:** Quip's document is one contenteditable, so a press anywhere on a block's text puts the caret where the browser's `caretPositionFromPoint` would (Quip's `caretRangeFromPoint` helper in `quip-collab-webapp`). Clack does the same: pressing on a paragraph, heading, the title, a list item, a quote's or callout's paragraph or a code block (not on a link or mention) opens that block in the composer's rich editor with the caret at the nearest character boundary under the pointer. The editor lays the text out once before its first frame, so the caret is never drawn anywhere else first. Dragging selects from the press, and a double or triple click selects the word or the line (the opening click is passed to the editor with `SeedClick`). A drag can't run across blocks the way Quip's can, because Clack edits one section at a time (one `canvases.edit` replace each), so it is clamped to the block. Headings are edited in Lato Black (`EditorStyle.Weight`), as they are drawn, so the text doesn't shift under the caret. A list item keeps its gutter marker at its level. Table cells don't open in the editor.
- **Quote paragraphs:** each paragraph of a quote is a Quip section of style 16. Editing one saves it as `> …`, so it stays in the quote, and Enter adds another quote paragraph. A block quoted from the toolbars, or by typing `> `, saves the same way: the fidelity check reads the quote's paragraph (it used to read the quote itself, and refused the save).
- **Toolbars:** as in Slack, there is no bar while a block is merely being edited. Anyone who can edit gets the bottom insertion toolbar, and selecting text shows the floating formatting bar over the selection. Both are described under "How it looks".
- **Enter** adds a line below in the same editor; a list stays a list, a checklist adds an unticked item.
- **Saving:** Escape, Ctrl/Cmd+Enter, or a click elsewhere saves the whole edit as one `canvases.edit`. That is a `replace` of the section with the lines' markdown, or a `delete` if the block was emptied; an unchanged block sends nothing. The view updates at once, and a failed save shows a toast and reloads.
- **Adding a block:** clicking the empty space under the last block types a new one (`insert_after` the last section).
- **Markdown shortcuts at the start of a block:** `# ` `## ` `### ` headings, `[] ` / `[x] ` checklist, plus the composer's own `- `, `1. `, `> `, ` ``` ` and *bold* _italic_ ~strike~ `code`.
- **Mentions:** `@name` searches people (core's directory search; `@here`, `@channel`, `@everyone` too) and `#name` channels. Up/Down/Enter/Tab or a click inserts a pill, which saves as `![](@U…)` / `![](#C…)`. The emoji button opens the shell's emoji picker.
- **Checklists:** ticking a box is its own `replace` of that item (`- [x] …`).
- **Rename:** click the title, or More actions → Rename canvas. Enter or clicking away saves (`rename`); Escape cancels.
- **Create:** the channel tab bar's "+" → Canvas (`conversations.canvases.create`; the new tab opens once the tab bar reloads), and Files' "+ New Canvas" (`canvases.create`, opened in full view).
- **Share:** in full view, Share or More actions → Share canvas lists the sidebar's conversations, then "Can edit" / "Can view" (`canvases.access.set`, then the link is posted).

**Editing never degrades a canvas.**
- *Mentions:* the export names mentions but carries no ids. Core resolves each one from the user and channel caches (`ResolveCanvasMention`): users by display name, then username, then real name, case-insensitively; channels by name. A name is accepted only when exactly one user or channel has it at the first tier that matches, so an ambiguous name stays unresolved.
- *Fidelity check:* a block opens in the editor only if `canvas.Editable` passes. Its kind must be one canvas markdown writes, and its markdown must parse back to the same inlines: text and links by text, style and target; mentions and emoji by id.
- *What fails the check:* underline and highlight (not in canvases.edit's supported list), dates, inline images, @here/@channel/@everyone and user groups (no documented form), unresolved mentions, hard line breaks inside a block (supported, but the syntax isn't documented), a link styled as code, and a code block that contains a fence.
- *What happens instead:* clicking such a block shows "This part can't be edited in Clack yet (it has …)", and nothing is sent. Ticking a checklist item that fails the check is refused the same way.
- *Saving:* a save whose new content would fail the check (say, Ctrl+U underline) is refused with a toast, and the editor stays open; a second Escape discards the edit.

The editor draws its text with richview's 0.45px embolden (`composer.EditorStyle.Embolden`), so a block doesn't look lighter while it's being edited.

**Not supported yet:** editing tables, images, columns and embeds (they render, but don't open in the editor), a drag selection across blocks, Insert > Date / Callout / Canvas, layouts, starting comment threads and section reactions (see "Comments"), version history, templates, deleting a canvas, Lists, and sending cursors (`document_change`). The link button only links selected text that is itself an address (there's no link dialog yet).

## Comments

Sources: Slack's web client on the public CDN, read without a cookie: the `application.js` + `modern.vendor` build 93ac48dd3c752f09 (requestQuipComments `xiJX`, updateArchiveState `XuFq`, QuipCommentsDataProvider `YPJx`, the id codecs `neAw`, ArchiveCommentButton in the flexpane header `WanD`, QuipCommentRoot `thrK`, canAddReaction `vtEK`, canArchiveComment `+IQ1+`, the embedded menu `Kda0`), the comment stream's lazy chunk `quip-popout_quip-comment-stream-view` 0893e737, boot-shared f9df0f12 (ThreadRoot `3ZeZ`, the stream reducer `pkMj`), `quip-collab-webapp`/`-editor` b10cb414/54e67774, and client-boot-styles cf726c4f.

**What a comment is.** A canvas's comments live in its file channel, the file id with F → C (`7/Dp`). A comment thread is a thread there. Its root has subtype `document_comment_root` and `document_comment {thread_id, authors, is_archived, is_visible, last_seen (µs), most_recent_editor}`; its text is the highlighted part of the canvas. The comments are the root's replies. Slack's thread key for a comment thread before it knows the root is `<channel>-Qpc:<thread id with "temp:" shortened to "t:">`, and `conversations.replies` takes that `Qpc:` form as `ts`.

**Where the anchor lives.** Quip makes the thread. Highlighting text and choosing Comment creates a `CONTROL_HIGHLIGHT_TYPE` control in the section (`highlightRange`/`highlightSection`, `quip-collab-webapp`), saved through Quip's binary `/canvas/-/edit-document`. `didCreateAnnotation` then calls `startCommentsThread`, and Quip's server writes the root message. A section reaction is the same kind of annotation, with the emoji on the root (`openReaction`, ReactionsEmojiPicker). The highlight's position, the annotation gutter and its message and reaction counts (`getAnnotationMessageCount`, `getAnnotationReactionCount`) exist only in Quip's document model. The HTML export Clack reads has no trace of them. canvases.edit has no form for a highlight, and Clack doesn't write Quip's protobuf (see "What Clack does"). So Clack can't start a thread or react to a section, and it can't draw the margin indicators. It reads, answers, resolves and reacts to the threads that exist.

**API** (all Web API, form-encoded):

| what | call |
|---|---|
| the comment stream (documentHistoryFetcher) | `conversations.history {channel: <file channel>, limit: 20, include_pin_count, inclusive, no_user_profile, include_free_team_extra_messages, include_date_joined: false, document_stream: latest_reply \| unread \| archived, cursor}`. The roots are the `document_comment_root` messages and the other messages are their latest replies. `channel_not_found` means no threads |
| a thread (requestQuipComments → fetchReplies) | `conversations.replies {channel, ts, inclusive, limit}` |
| reply, edit, delete, react | `chat.postMessage` (thread_ts = the root), `chat.update`, `chat.delete`, `reactions.add/remove`, as in any thread |
| Resolve thread / Re-open thread (updateArchiveState) | `quip.thread.archive` / `quip.thread.unarchive {channel_id, thread_ts}`. createApiMethodFetcher snake-cases its camelCase input. The answer's `message` replaces the root |
| Resolve all threads (archiveAllThreads) | `quip.thread.archiveAll {channel_id}` |

**The stream** (`internal/ui/views/canvas_comments.go`, QuipCommentStreamView). The canvas's More actions has "Show all threads", or "Hide all threads" while the stream shows (key `comments`). It is hidden only on a template the viewer can't write. The item opens `toQuipCommentStream` in the secondary pane. With the canvas itself in the side panel, the stream takes the pane and the canvas becomes its back stack (the header's Back).
- **Header:** `.p-quip_popout__flexpane_header`, 50px with padding 4px 12px, 18px Lato Black, and a 1px line under it. The title is "All threads", "Unread comments" or "Resolved threads". On the right are the Filter button (refine, "Filter" tooltip) and Close.
- **Filter menu:** "Filter by", the radios Most recent / Unread / Resolved, then Resolve all threads when the canvas is writeable and not locked. That item asks first (QuipArchiveAllModal: "Resolve all threads and reactions?", danger).
- **Rows** (getRows): a date row per day of the latest reply (13px bold, padding 12px 16px, `{day_divider_pretty}`). Each thread is a card (base-pry, 1px otl-ter, radius 12, 16px under it) holding:
  - the root, drawn as ThreadRootCanvas → QuipCommentRoot: padding 12px 20px 8px 12px, the 4px base-ter attachment bar, the highlighted text;
  - "Show N more replies" (32px) when the thread has more than one reply;
  - the "New" line when `last_seen` isn't the latest reply;
  - the latest reply.
- **Clicks:** every row opens the thread (showQuipCommentThread) with the stream as its back stack.
- **Empty states:** "Nobody's added any comments" / "There aren't any resolved threads" / "No unread comments", each with Slack's description.

**The thread** (`internal/ui/messages/thread_comment.go`). It is the ordinary thread pane on the file channel.
- **Root:** ThreadRootCanvas. There is no avatar gutter; QuipCommentRoot shows, in a thread, a heading with the most recent editor's 16px avatar and name, then the highlighted text. When Quip no longer shows the highlight it says "This content was deleted.".
- **Header:** ArchiveCommentButton sits before the ⋮, shown when canArchiveComment (the canvas writeable and not locked, a Quip thread id, at least one reply). It is check "Resolve thread", or rotate "Re-open thread" on a resolved thread, with its tooltip bottom-right after 300ms.
- **The root's own reactions:** the root takes no add-reaction button or quick reactions there (showAddReactionButton and showQuickReactions off).

**Reactions on comments** (canAddReaction, `vtEK`). In a canvas's file channel, a viewer who can't edit the canvas may react only when it is commentable (access comment, write or owner) and `canvas_comment_only_mode` is on. Clack applies this to the toolbar's add-reaction button and quick reactions and to the context menu's "Add reaction…" once the canvas's files.info is known (`core.CanAddReaction`).

**canvas_comment_only_mode** (on for the account) changes four things in Slack:
- the access indicator's "Comment only" and Request Comment Access (`access.go`, ported);
- reactions on comments with comment access (above, ported);
- the embedded menus' comment rows for commentable viewers (`fileIsCommentable` in Kda0, which needs annotations);
- the share dialog's Can comment level (useGetAccessOptions). That one can't be ported: canvases.access.set takes read, write and owner only.

**The channel card's rows** (`popups/canvas_channel_card.go`; Kda0 EmbeddedMenu in the HOVERCARD context). After View channel details and Copy name:
- **Report to Slack** (getReportSectionMenuTemplate): shown with `is_content_reporting_enabled`. It opens the team's `/help/requests/report-content?u=<the section's anchor link>&type=canvas_section`. The anchor link is the canvas permalink with `focus_section_id` appended (Quip's getUrl uses `URL.searchParams.append`).
- **Remove mention** (getRemoveSectionMenuTemplate). Slack deletes the control (performAction 1). Clack saves the section without the mention (canvases.edit replace), so the row is offered only on a top-level section that passes the fidelity check with and without it (`canvasview/mention.go`).
- **Not offered:** Add a comment / View comments, Add a reaction / View reactions and Display as (Mention / URL / Card). Each writes the control or an annotation through edit-document.

## How it looks

Slack's evidence: the Quip editor's stylesheet `canvas-controller-d97a8e501e2ab778.css` (on the CDN; `quip-collab-webapp` loads it through `setControllerCssUrl`), the canvas chunks above, `client-boot-styles` (the toolbars), `client-boot`'s `zk` (the floating bar's buttons), and read-only DOM measurements of a canvas tab in the test channel (2026-11-09; only layout numbers and computed styles were read).

**Page.**
- **Column and background:** `.parts-screen-children-wrapper` is at most 990px wide, centred, with `padding: 0 94px 120px 96px`, which gives an 800px column (Quip's `MAX_DOCUMENT_WIDTH`). The 94px is Quip's fixed padding (`horizontalPaddingOverrides`: fixed 94, medium 51); the left 96 is the table of contents' gutter (`canvas_table_of_contents`: `setExternalGutterReservedWidth(64)`, and `canvasDocumentMargins` makes the left margin `max(padding, 64 + 32)`). Without the experiment both sides are 94. Narrower panes use Quip's smaller paddings (51px under 500px, 28px under 350px, 16px under 200px), the left one still at least 96 under the experiment. Above the title is the 100px `.header-image-container`, which stays empty without a cover image. The background is `--sk_primary_background` (`.p-quip_embed`).
- **Title:** the title section (`data-section-style="48"`) is 28px/34px at `--doc-theme-header-weight` (900 under `use-slack-font`), with a 20px margin below it. Slack draws no byline under the title: no avatar, no "Created by", no "Last edited".
- **Text colour:** `rgb(var(--quipUserTextPrimary))`, which is #333333 in light and #e6e6e6 in dark, not Slack's message ink.
- **Body:** Lato 15px/22px. Paragraph margins are 4px/4px.
- **Headings:** H1 22/30, H2 18/27, H3 15/22, all weight 900. Their margins are 28/6, 24/2 and 20/2. Margins collapse, and the first section has no top margin.
- **Lists:** a list section has 8px margins, with 6px between items and 30px of indent per level. The marker sits in a box at least 24px wide plus 4px padding, so the text starts 28px in. Bullets go ● ○ ■ □ by level; numbers go 1. a. i.
- **Checklists:** a 14px box 5px in, radius 2, with a 1px #7c7a7f border (#bbbabb in dark) on `--dt_color-base-sec`. Checked boxes are `--dt_color-base-inv-hgl-2` with a 2px tick. That is Slack's `li.slackified-checkbox` label, which Quip adds under `slackified_checkbox` (feature 178, `useSlackCheckboxes`). Without it the item has only the browser's `<input type="checkbox">`: Chromium's 13px checkbox (radius 2, a 1px #767676 border on white, checked #0075ff with a white tick; under the document's `color-scheme: dark`, #3b3b3b, a #858585 border, checked #99c8ff with a #3b3b3b tick), centred in the 24px marker box, 2px down.
- **Quote:** each quote paragraph is its own section (`data-section-style="16"`), and the bar is that section's `.content:before`. So Slack draws one bar per paragraph, not one per quote: padding `3px 0 3px 18px`, a 4px bar (radius 3, inset 4px top and bottom) in `--dt_color-otl-pry` (`--dt_color-content-sec` in dark). Section margins are 4/12, so two paragraphs are 12px apart, each with its own bar.
- **Tables:** Quip's `TableContainer` (`quip-collab-editor`) and the `.table-container` CSS. `.table-element` is `table-layout: fixed` with collapsed borders. A column is as wide as the table stores it (`<col style="width:…">`, parsed into `Block.ColWidths`), else `containerWidth / numColumns`, so a plain table fills the column evenly. Cells have a 1px `--dt_color-otl-sec` border, padding 8px 16px, a 46px minimum height, and 4px before the first section and after the last. The header row is `--dt_color-surf-pry` at weight 700. `.table-scroller` scrolls sideways when stored widths overflow the page; its 16px padding is cancelled by -16px margins, and a 20px gradient from the background marks a side that can scroll.
- **Code block:** 12px monospace on a 20px line, padding 8, a 1px `--dt_color-otl-ter` border on `--dt_color-base-sec`, radius 4.
- **Divider:** a 20px `hr` with a 1px line in `rgba(var(--quipUiColorGray10), .2)`.
- **Other blocks** take the default 20/20 section margins.

**Title placeholder.** Quip's `TitleSectionOverlay` (`quip-collab-editor`) draws `titlePlaceholder`, "Your canvas title" (`client-boot-shared`), over the title section while it is empty, even when unfocused. Its `isTitleSection` takes the first section when it is an H1, so an empty opening `<h1>` is the title, not the file name. The placeholder is in the title's own type (28/34, 900) in the overlay colour (#77757a, #9a9b9e in dark). Viewers who can't edit see an empty title line.

**Table of contents** (`canvas_table_of_contents`; Quip's feature 188 `allowSlackOutline`, client-boot-shared `tshn` TableOfContentsDrawer, mounted by `foOi` QuipEmbed).
- **Entries** (quip-collab-editor `computeTableOfContentsInfo`): the title section and every heading with text, in document order. The title is level 0; the headings' levels are renumbered from 0 in the order of the levels present (`normalizeHeadingLevels`), so H2s and H3s indent like H1s and H2s. With fewer than two entries nothing is drawn.
- **Collapsed:** the drawer sits 12px from the embed's left edge, beside the column. Up to 24 lines, 2px tall and 10px apart, 16px under the drawer's top. They are 20, 14 or 8px wide by indent, in `--dt_color-otl-sec`, the current section's in `--dt_color-otl-hgl-1`. The drawer follows the first entry down the page and sticks 58px from the top (0 when the embed is under 546px tall).
- **Expanded:** hovering the lines for 250ms opens a 244px panel: 12px padding, a 1px `otl-ter` border, radius 12, `0 4px 6px -4px` and `0 10px 15px -3px` shadows of #00000019. Its entries are 28px tall in 13px text, indented 16px per level; the title is bold, the current entry bold in `content-hgl-1`. It scrolls when it is longer than the room under it. Leaving the drawer closes it, though not within 150ms of opening.
- **Clicking an entry** scrolls its section to the top of the view (`focusSection`). It stays current until the page scrolls on; otherwise the current entry is the one starting in the top half of the view, else the last one above it (`computeActiveSectionId`).
- **Clack:** `internal/ui/canvasview/toc.go`. Clack knows only the rows on screen, so an entry above or below the view counts as above or below its top, as Slack's model-only sections do.

**Placeholder.** Slack fills the empty canvas with an overlay (Quip's `TextSectionOverlay`), never with document text. The overlay is shown to editors only, when the document is its title plus one plain paragraph that is empty or holds unedited default content. Its text is `textPlaceholder`, picked by `getIndexFromFileId` from "Go ahead, start writing!", "What's on the docket for today?", "Once upon a time…", "Time to document your genius!" and "Words go here " (`client-boot-shared`; with AI access a random AI prompt is used instead). It is 15px in #77757a (#9a9b9e in dark). Once the canvas has other text, default content is invisible. The index comes from the Quip document's id (QuipEmbed passes `doc.id`; files.info `quip_thread_id`), not the file id.

**Default content when editing** (`quip-collab-webapp` Section, `quip-collab-editor` TextSectionOverlay). The new-document line is `DefaultContent.HIDE_ON_FOCUS`. Focusing it (a click on it, or a click under the last section when it is the last one) runs `hideDefaultContent`: the line opens empty with the caret at 0, so its text can't be selected. Typing makes it an ordinary section (`onSelectionChange` → `clearDefaultContent`), saved as a `replace`. Leaving it empty restores the default content and sends nothing (`unfocusDefaultContent_`). On an empty canvas the placeholder stays while the line is focused. In a canvas with other text, Slack's `slashShortcutCommandsNuxEnabled` secondary overlay shows the saved default text as the hint.

**Add cover** (`EmbeddedQuipHeaderImageEmptyState`, canvas-controller CSS). For editors, the 100px band above the title holds an 80px strip with an outline small button, bottom-aligned at the text column (the image-input icon, "Add cover", 8px side padding). It fades in over .25s after 30ms while the pointer is over the band or the title section, or the button has focus. A key pressed over the title hides it until the pointer moves. Slack's click adds a random header from `canvases.listHeaders` through Quip's `docEditor.setHeaderImage`. That is an edit-document write, which Clack doesn't make, so Clack says it can't save a cover yet. The picker (Library / Upload tabs, `QuipHeaderImagePicker`) belongs to an existing cover, which Clack can't detect: files.info and the export don't carry it.

**Links to canvases** (`internal/richtext/canvasslug.go`). A rich_text `canvas` element is Slack's canvas slug (boot-shared `rlp/`, labels from client-boot `Apl0`): the canvas's name, "{canvasName}: {sectionText}" for a section (25 characters, from `quip.getCanvasBulkSectionIdsData`), "Deleted canvas" / "Private canvas". A `document_section` attachment is `MessageAttachmentQuipAnchorLink`: the section's blocks, then the footer with the title linked to the section, "View section" (unless `private_message_forwarding`) and "Canvas". It has no author row. Both open the canvas focused on the section (`focus_section_id` → `View.FocusSection`).

**Canvases shared as message files** (`internal/richtext/canvasfile.go`, `internal/ui/richview/canvasfile.go`). The title line names the owner when someone other than the sender owns the file ("{owner} | Canvas"). The card is `p-message_file--quip`, at most 594px: QuipIcon's IA4 tile (canvas-content-filled on lagoon), the title from `title_blocks`, "Canvas". Under it is FileQuip's preview, cut at 240px with no fade (no `allowExpand`). Slack's preview blocks are Quip's own Block Kit thumbnail of the canvas, read through Quip's client. Clack builds equivalent blocks from the HTML export: the first 11 sections, the title dropped.

**Header.**
- **Channel tab:** no bar. The More options ⋮ floats at the top right in a 32px tile (radius 8, a 1px ring, a 1px/3px shadow), 24px in. For a canvas the viewer can't edit the access indicator (below) shares the tile, borderless, before the ⋮. Its menu starts with Copy link and Share this canvas and ends with Open in split view. Clack shows only those three; Slack's other items (View file details, Add to saved, cover image, version history, templates, print, …) aren't implemented.
- **Side panel and full view:** Slack's canvas flexpane header. It is a `.p-view_header` (49px, padding `0 16px 0 18px`, the title in 18px Lato Black, one line), with no bottom shadow (`.p-quip_flexpane .p-view_header{box-shadow:none}`). `CanvasActionItems` (`client-boot-shared`) sits on the right, 6px apart (`--gap`). In the full view (the primary container) these are the `QuipLastUpdatedReminder` ("Edited {time}", always relative, "minutes" shortened to "min", 13px at foreground .7, once the canvas was edited), the facepile, the outline small Share button (`CanvasSharePrimaryButton`), and More actions (the 28px `.p-quip_flexpane__menu_trigger`). In the side panel (the secondary container) Slack hides the reminder, Share and the star. Clack draws the facepile, its Open in full view where Slack's Open in new window is, More actions, and the panel's close. Open in side panel lives in More actions, as in Slack. Add to starred isn't implemented.
- **Access indicator** (QuipAccessIndicator, client-boot-shared `CQK3`): a canvas the viewer can't edit says so with Slack's ViewOnlyPill (`EgWz`). It is an outline small button: 28px, 13px bold, a 1px foreground-low .13 border, radius 4, with the 16px eye icon and "View only". Under `canvas_comment_only_mode`, a viewer with comment access (`isCommentable`: access comment, write or owner) sees "Comment only". It sits in `CanvasActionItems` before Share (`renderViewOnlyButton`: access set and not `isWriteable`).
  - Its popover (bottom-left, offset -12/6; `.popover__dsoBs`: 300px, padding 16, a 1px `otl-ter` border, radius 4) runs `useQuipAccess`. It says "You only have permission to view." (or "…to comment." under the experiment with comment access), then "To make changes, ask the owner for edit access.", with Request Edit Access (`files.requestPermission {file_id, reason: IndicatorRequestEditAccessButton, request_type: upgrade}`).
  - Under the experiment a viewer without comment access also gets Request Comment Access (`comment_upgrade`).
  - A canvas already asked about (`most_recent_access_request_ts`) offers "You’ve already asked for access" and Remind The Owner. A sent request says "Request sent" / "You’ll be notified once it’s been shared with you." A failure says "Error" with Try Again.
  - Slack's Can comment access level isn't offered: Clack shares through `canvases.access.set`, which takes read, write and owner only.

**Bottom insertion toolbar** (`.p-quip_insertion_toolbar`, `InsertionToolbarButtons`).
- **Shape:** centred, 18px up from the bottom (24px in the side panel), on the primary background with `0 0 0 1px` foreground at .13 and `0 4px 12px #0000001a`, radius 8, padding 2. Buttons are 30px with 20px icons, with 5px around the first and 2px around the rest.
- **Buttons, in order:** the green `+` (Insert), `Aa` (Formatting), emoji, Insert file, Insert checklist, Insert table, Layouts.
- **Insert menu:** Divider, then Date, Profile, Callout, (Blockquote), Canvas, Image and File. Blockquote is there under `canvas_blockquote_layout` (BLOCKQUOTE_LAYOUT, Quip's `insertSection("blockquote-layout")`): a new quote line after the insert point, saved as `> …`. These go through `canvases.edit`'s documented markdown:
  - Divider is `---`.
  - Profile opens a new line with the @ picker. The person saves as `![](@U…)` alone in its block, which Slack unfurls as a profile card.
  - Image uploads the picked file (`files.getUploadURL`, the bytes, `files.completeUpload`, shared nowhere) and inserts `![name](permalink)`, the form docs.slack.dev gives for images.
  - File does the same with `![](permalink)`, a file unfurl.
  - Date and Callout have no canvas markdown. Canvas needs Slack's canvas search, which Clack doesn't have yet. Those three are drawn in Slack's disabled state (`.c-menu_item__button--disabled`, opacity .5).
- **Formatting menu:** Blockquote, Code block, ─, Bulleted list, Ordered list, Check list, ─, Small heading, Medium heading, Big heading, Paragraph. Under `canvas_blockquote_layout` the Blockquote and the separator after Code block go (InsertionToolbarFormattingMenu). It is disabled until a block is being edited.
- **Insert file** is the menu's File. **Insert table** is Quip's new table (`TableLayout.initializeContent`: `insertRows({count: 3})`, `insertColumns({count: 2})`) as a canvas markdown table, inserted after the block being edited, else at the end. **Layouts** (columns) has no canvas markdown, so it is drawn disabled: `.c-icon_button.c-button--disabled`, the icon at opacity .5.
- **Inserts never fetch a page:** an image block whose URL is a Slack page (a permalink) is only drawn from its file's thumbnail. Clack never loads `*.slack.com` pages as images.

**Floating formatting bar** (`QuipFloatingFormattingBar`).
- **When:** only while a non-empty range is selected.
- **Shape:** #1a1d21 in both themes, radius 8, padding 2, a `#e8e8e840` ring. Buttons are 28px with radius 4 and white icons, hover `#ffffff13`, pressed `#e8e8e821`. Separators are 1px `#ffffff40` lines, 20px tall.
- **Buttons, from `zk`:** the block-style menu (the current format's icon and a caret: Paragraph, Big/Medium/Small heading, ─, Check list, Ordered list, Bulleted list, ─, Code block, Blockquote) | bold, italic, underline, strike | code | link, callout. Under `canvas_blockquote_layout` the blockquote leaves the menu for a button of its own after strike (`[blockquote-layout]`, the quote icon, pressed in a quote): `performBlockquoteConversion` quotes the block or takes it out of the quote. The comment button is left out because Clack can't start a comment thread (a highlight control, see "Comments"), which is Slack's own `quipThreadButtonType: "NONE"`.
- **Disabled:** underline (canvas markdown has none) and callout (no canvas markdown either), in the floating bar's disabled colour (`#ffffff4d`).

## Plugins

In the library, only Slick's NoUpsell touches canvas UI: it hides `CanvasAskSlackbotButton` / `[data-qa="canvas_ask_slackbot_button"]`. Clack draws no Ask Slackbot button, so there is nothing to hide. HaikuWarning's comment about canvas links (sent as the file id) is about the composer. HuddleActivity's `huddlesCanvas` and BetterHuddles' `canvas_thread_ts` are huddle state. None of them break.

## Code

- `internal/canvas`: the model, the export parser, markdown in and out, an HTML writer in the export's shape (the fake uses it).
- `internal/slack/canvas.go`: `DownloadCanvasHTML`.
- `internal/core/canvas.go`: `Canvas`, `CanvasInfo`, `OpenCanvas`/`CloseCanvas`, the event watcher, `EditCanvas`, `RenameCanvas`, `CreateCanvas`, `ShareCanvas`.
- `internal/ui/canvasview`: the view (header, blocks, the three modes, live updates, editing); `toc.go` the table of contents, `access.go` the access indicator, `experiments.go` the canvas experiments it follows.
- `internal/ui/mount_features_canvas.go`: the `docs/<F…>` route, the side panel, the channel tab body. Hooks elsewhere: the channel tab click and body (`channel.go`), `messages.CanvasOpener` (canvas files and canvas links in messages), Files → Canvases (`views/files.go`), and the canvas tile in file cards (`richview/cards.go`).
- `internal/core/canvas_comments.go`: the comment stream, Resolve / Re-open / Resolve all, canArchiveComment, canAddReaction's file channel rule. `internal/ui/views/canvas_comments.go` the stream, `internal/ui/messages/thread_comment.go` the thread's root and header button, `internal/ui/canvas_comments.go` the pane wiring, `internal/ui/canvasview/mention.go` Remove mention.
- `internal/fakeslack/canvas.go`: fixture canvases (`canvases/kitchen.html` covers every block type) and the endpoints above, with `document_update` pushed over the fake socket. `canvas_comments.go`: the Launch plan canvas's comment threads (open, unread, resolved, one with a reaction) on its file channel, with the stream's filters and the `quip.thread.*` methods.
