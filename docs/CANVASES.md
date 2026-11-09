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
- **Opening a block for editing:** clicking a paragraph, heading, list item or code block (not on a link or mention) turns it into the composer's rich editor. A list item keeps its gutter marker at its level.
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

**Not supported yet:** editing tables, images, columns, callouts and embeds (they render, but don't open in the editor), inserting images or files, comments, version history, templates, deleting a canvas, Lists, and sending cursors (`document_change`). The link button only links selected text that is itself an address (there's no link dialog yet).

## How it looks

Slack's evidence: the Quip editor's stylesheet `canvas-controller-d97a8e501e2ab778.css` (on the CDN; `quip-collab-webapp` loads it through `setControllerCssUrl`), the canvas chunks above, `client-boot-styles` (the toolbars), `client-boot`'s `zk` (the floating bar's buttons), and read-only DOM measurements of a canvas tab in the test channel (2026-11-09; only layout numbers and computed styles were read).

**Page.**
- **Column and background:** `.parts-screen-children-wrapper` is at most 990px wide, centred, with `padding: 0 94px 120px 96px`, which gives an 800px column (Quip's `MAX_DOCUMENT_WIDTH`). Narrower panes use Quip's smaller paddings (51px under 500px, 28px under 350px, 16px under 200px). Above the title is the 100px `.header-image-container`, which stays empty without a cover image. The background is `--sk_primary_background` (`.p-quip_embed`).
- **Title:** the title section (`data-section-style="48"`) is 28px/34px at `--doc-theme-header-weight` (900 under `use-slack-font`), with a 20px margin below it. Slack draws no byline under the title: no avatar, no "Created by", no "Last edited".
- **Text colour:** `rgb(var(--quipUserTextPrimary))`, which is #333333 in light and #e6e6e6 in dark, not Slack's message ink.
- **Body:** Lato 15px/22px. Paragraph margins are 4px/4px.
- **Headings:** H1 22/30, H2 18/27, H3 15/22, all weight 900. Their margins are 28/6, 24/2 and 20/2. Margins collapse, and the first section has no top margin.
- **Lists:** a list section has 8px margins, with 6px between items and 30px of indent per level. The marker sits in a box at least 24px wide plus 4px padding, so the text starts 28px in. Bullets go ● ○ ■ □ by level; numbers go 1. a. i.
- **Checklists:** a 14px box 5px in, radius 2, with a 1px #7c7a7f border (#bbbabb in dark) on `--dt_color-base-sec`. Checked boxes are `--dt_color-base-inv-hgl-2` with a 2px tick.
- **Quote:** padding `3px 0 3px 18px` and a 4px bar (radius 3, inset 4px top and bottom) in `--dt_color-otl-pry` (`--dt_color-content-sec` in dark). Margins are 4/12.
- **Code block:** 12px monospace on a 20px line, padding 8, a 1px `--dt_color-otl-ter` border on `--dt_color-base-sec`, radius 4.
- **Divider:** a 20px `hr` with a 1px line in `rgba(var(--quipUiColorGray10), .2)`.
- **Other blocks** take the default 20/20 section margins.

**Placeholder.** Slack fills the empty canvas with an overlay (Quip's `TextSectionOverlay`), never with document text. The overlay is shown to editors only, when the document is its title plus one plain paragraph that is empty or holds unedited default content. Its text is `textPlaceholder`, picked by `getIndexFromFileId` from "Go ahead, start writing!", "What's on the docket for today?", "Once upon a time…", "Time to document your genius!" and "Words go here " (`client-boot-shared`; with AI access a random AI prompt is used instead). It is 15px in #77757a (#9a9b9e in dark). Once the canvas has other text, default content is invisible.

**Header.**
- **Channel tab:** no bar. The More options ⋮ floats at the top right in a 32px tile (radius 8, a 1px ring, a 1px/3px shadow), 24px in. Its menu starts with Copy link and Share this canvas and ends with Open in split view. Clack shows only those three; Slack's other items (View file details, Add to saved, cover image, version history, templates, print, …) aren't implemented.
- **Side panel and full view:** these keep Clack's bar.

**Bottom insertion toolbar** (`.p-quip_insertion_toolbar`, `InsertionToolbarButtons`).
- **Shape:** centred, 18px up from the bottom (24px in the side panel), on the primary background with `0 0 0 1px` foreground at .13 and `0 4px 12px #0000001a`, radius 8, padding 2. Buttons are 30px with 20px icons, with 5px around the first and 2px around the rest.
- **Buttons, in order:** the green `+` (Insert), `Aa` (Formatting), emoji, Insert file, Insert checklist, Insert table, Layouts.
- **Insert menu:** Divider, then Date, Profile, Callout, Canvas, Image and File, which Clack draws disabled.
- **Formatting menu:** Blockquote, Code block, ─, Bulleted list, Ordered list, Check list, ─, Small heading, Medium heading, Big heading, Paragraph. It is disabled until a block is being edited.
- **Disabled in Clack:** Insert file, table and layouts aren't implemented, so they are drawn in Slack's disabled state.

**Floating formatting bar** (`QuipFloatingFormattingBar`).
- **When:** only while a non-empty range is selected.
- **Shape:** #1a1d21 in both themes, radius 8, padding 2, a `#e8e8e840` ring. Buttons are 28px with radius 4 and white icons, hover `#ffffff13`, pressed `#e8e8e821`. Separators are 1px `#ffffff40` lines, 20px tall.
- **Buttons, from `zk`:** the block-style menu (the current format's icon and a caret: Paragraph, Big/Medium/Small heading, ─, Check list, Ordered list, Bulleted list, ─, Code block, Blockquote) | bold, italic, underline, strike | code | link, callout. The comment button is left out because Clack has no comments, which is Slack's own `quipThreadButtonType: "NONE"`.
- **Disabled:** underline (canvas markdown has none) and callout.

## Plugins

In the library, only Slick's NoUpsell touches canvas UI: it hides `CanvasAskSlackbotButton` / `[data-qa="canvas_ask_slackbot_button"]`. Clack draws no Ask Slackbot button, so there is nothing to hide. HaikuWarning's comment about canvas links (sent as the file id) is about the composer. HuddleActivity's `huddlesCanvas` and BetterHuddles' `canvas_thread_ts` are huddle state. None of them break.

## Code

- `internal/canvas`: the model, the export parser, markdown in and out, an HTML writer in the export's shape (the fake uses it).
- `internal/slack/canvas.go`: `DownloadCanvasHTML`.
- `internal/core/canvas.go`: `Canvas`, `CanvasInfo`, `OpenCanvas`/`CloseCanvas`, the event watcher, `EditCanvas`, `RenameCanvas`, `CreateCanvas`, `ShareCanvas`.
- `internal/ui/canvasview`: the view (header, blocks, the three modes, live updates, editing).
- `internal/ui/mount_features_canvas.go`: the `docs/<F…>` route, the side panel, the channel tab body. Hooks elsewhere: the channel tab click and body (`channel.go`), `messages.CanvasOpener` (canvas files and canvas links in messages), Files → Canvases (`views/files.go`), and the canvas tile in file cards (`richview/cards.go`).
- `internal/fakeslack/canvas.go`: fixture canvases (`canvases/kitchen.html` covers every block type) and the endpoints above, with `document_update` pushed over the fake socket.
