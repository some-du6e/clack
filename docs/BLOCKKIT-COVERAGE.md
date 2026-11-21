# Block Kit coverage

What Slack can put in a Block Kit payload, what Slack's own client draws for it, and what Clack draws, item by item. The checklist is Karim's pointer (2026-11-18): [jollyroger182/slack.ts `src/types/blocks/blocks.ts`](https://github.com/jollyroger182/slack.ts/blob/main/src/types/blocks/blocks.ts) with its `elements.ts` and `objects.ts`. It's filled out with `@slack/types` 3.3.0 (which slack.ts extends) and with Slack's web client, which decides how each item draws.

**Sources**

- **Slack's web client**, build `846030e1ce97` (captured 2026-11-17, public CDN chunks via slack-datamine-plus): the SUI renderables in `gantry-v2-async-client-boot-shared` and `client-boot`, and `client-boot-styles.cf726c4f2eaecc74`. Module ids are given as `boot-shared XXXX` or `client-boot XXXX`. The next build, `031dbe6a7400` (2026-11-19), has the same renderable list.
- **slack.ts** at `main` (2026-11-18) and **@slack/types 3.3.0** (`dist/block-kit/*.d.ts`).
- **Real use**: block and element types counted in the probe account's 16 conversations (5 public, 1 private, 10 DMs): 7,248 messages and their thread replies, and the one App Home view the account can see (2026-11-18). Only shapes and counts were kept, never message text; `count.py` and `shape1.py` in `/root/clack-ops/scratch/blockkit-coverage/tools`.

**Status words**

- **full**: drawn and handled the way Slack's component does, with tests.
- **partial**: drawn, with what's missing named.
- **absent**: Clack has no renderer. Until this round such a block drew its text object if it had one, else nothing. That's still the rule for absent blocks whose type Slack knows.
- **fallback**: Slack has no renderable for it either, so the fallback chain below decides.

"Before" is `ui` at `e93a5e70`, "after" is this branch.

## How Slack decides what draws

Every surface (messages through `InteractiveBlocks` boot-shared `VoyB` and `ReadonlyBlocks` `+Yyz`, App Home and modals through `WSwB`, the AI surfaces, the share dialog, workflow previews) does two things:

1. **Expands `condition` blocks** (`useExpandedConditionBlocks`, boot-shared `CRur`). A condition block is replaced by the blocks of its first case whose `conditions` all hold, else by its `default`, else by nothing. `client` conditions compare with `"desktop"`, `team` conditions with the viewer's team or enterprise id (`=` / `!=`), and `content_type` matches `condition_demo` whatever the op. Any other type or op never holds.
2. **Looks each block up in the surface's renderables** (`SUIRendererItem`, boot-shared `Q5CE`). Messages use `jBv3`'s list. Blocks, elements, rich_text pieces and inline elements share one type-keyed map, so an element type at the top level renders too. A type with no renderable goes to `resolveFallback`:
   - a `fallback` list draws each of its payload items (an object whose `type` is a string), each through the same lookup, so fallbacks nest. An empty list draws nothing.
   - a single payload item draws itself.
   - anything else draws the surface's fallback component, `UnsupportedBlockFallback` (boot-shared `1G65`): an entity card reading "This message can’t be fully displayed."

Types in Slack's own block enum (client-boot `WP`) still fall back when no renderable exists: `fallback_canary`, `expandable`, `group`, `error`. `markdown` isn't in the enum and has no renderable: Slack's server translates markdown blocks before a client sees them.

Child blocks differ by parent:
- A callout's render through `createRenderBlocks` (boot-shared `a+Oj`) with no fallback component, so an unknown child with no fallback draws nothing there.
- A container's are its `childBlocks` slot (`yWQF`). Those SUIRendererItems keep the container's own fallback component: in a message that's the card, and nothing when the container sits in a callout.
- Neither kind has block wrappers, so the children's margins collapse.

A message that has blocks draws them instead of its text (client-boot `iub5` shouldRenderBlocksInsteadOfText), even when they draw nothing. A condition with no case for the desktop, or a lone empty fallback, leaves an empty body. Blocks that are all null or rich_text without elements leave an empty body too: Blocks (`lby4`) draws nothing for them, and the text still doesn't draw. The text stands in only when the blocks array is empty. In Clack it also stands in when a block that Clack can't draw yet drew nothing.

A rich_text piece (`rich_text_section`, `_list`, `_quote`, `_preformatted`, `_header`, `_divider`) or an inline element at the top level, or as a fallback payload item, draws through its own renderable, as it would inside a rich_text.

A block Clack can't decode draws KfF5's per-block line, "This content could not be displayed.". Slack has no such case: its blocks are plain JS objects, so a field of an unexpected type never fails a block. The nearest Slack outcome is a block that throws while rendering, which MessageBlocks' error boundary replaces with AR99's "We can’t fully display this message." (u33y MessageErrorFallback). Clack's lenient field types (`bkStr`, `bkLevel`, `bkRows`, raw `icon` and `width`) keep these failures rare.

Two other readers skip the chain:
- **Notifications** read a message through `convertMessageToPlainText` (client-boot `2xQY`, from boot-shared `Eley` getContentForNotification). It doesn't expand conditions (that's for message summaries) and has no fallback chain, so a condition, a `fallback_canary` or a type it can't convert adds nothing, and an empty result is the message text: `richtext.NotificationText`.
- **Search previews** give MessageBlocks `NoUnsupportedBlockNotice` as their fallback (boot-shared `vVRo`), so the card adds nothing to a result's text: `richtext.SearchText`.

Clack before: only types missing from the enum fell back. Known types it couldn't draw showed their text object or nothing, a fallback list skipped nested unknown types, an empty list drew the card, and conditions drew nothing. After: `richtext/fallback.go` implements the chain above, and condition expansion runs on every surface (`richtext.ExpandConditions`, `blockkit.expandConditions`).

The elements inside child blocks and fallback payloads are live, as in Slack, where the surface's `onAction` reaches them (Container `yWQF` provides the blockKit context, and Callout's `a+Oj` inherits it). Blockkit keeps the tree richtext draws (`Block.Nested`: a container's or a callout's `child_blocks`, and `richtext.FallbackItems`). Its lookups and the view state walk that tree, so a nested button sends `block_actions` with its own block's id. In modals and App Home, the Surface is the Elements host of the richview that draws those blocks. One gap remains there: an input block nested in a container draws as a section with its element (no hint, no error line), and the submit's checks skip it.

## Use in the probe account's conversations

Top-level blocks; "in attachments" counts attachment `blocks`:

| type | top-level | in attachments | notes |
|---|---:|---:|---|
| rich_text | 6,757 | 225 | rich_text_section 8,100 · list 298 (`border` 261, `offset` 27) · quote 72 · preformatted 17; inline text 18,873 · emoji 5,550 · link 1,681 · user 877 · channel 573 · message_mention 96 · broadcast 59 · usergroup 26 · color 8 · canvas 5 · file 1 |
| section | 69 | 224 | `fields` 227, `text` 70, `accessory` 5 |
| header | 9 | 223 | `level` 3 |
| actions | 11 | 229 | buttons 430 (`url` 416, `value` 416, `style` 16) |
| plan | 85 | 0 | tasks with `details` rich_text, `status` |
| context | 12 | 23 | |
| image | 18 | 9 | `is_animated` and `fallback` (server-added; the client reads neither) |
| divider | 7 | 0 | |
| call | 2 | 0 | ended Calls API calls with a `display_id` |
| share_shortcut | 0 | 1 | a workflow card |
| App Home | | | header, section, context |

So the blocks Karim's workspaces use were already drawn. The gaps that showed up in real use were `call` (a generic card before), `share_shortcut` (nothing) and the inline `file` element (its text). The rest of the list has no use in this sample, so it was done in the order slack.ts and the docs list it.

## Blocks

| block | Slack (renderable · component) | before | after | what's missing |
|---|---|---|---|---|
| actions | `/jJF` | full | full; `visible_to_user_ids` (the one block that reads it) hides a button or checkboxes from members it doesn't list, and the hidden element still counts for "+ N more" and takes one of the first five places | modals and App Home draw every element: Slack's Actions block truncates there too (the first five and "+ N more" from seven) |
| alert | `MyNk` AlertRenderable · client-boot `MlDl` Alert | the per-block error line ("This content could not be displayed.": a string `level` failed the decode) | **full** | the optimal-line-length pref's 75ch cap |
| auth_provider (@slack/types only) | `zTn9` AuthProviderBlockLazy | the per-block error line (its `name` text object failed the decode) | absent | connect/disconnect card |
| call (slack.ts) | `FkcF` · `9CtN` PlatformCallSubtype | partial: a generic "Call" link card | **partial** | the ended icon's and faces' grayscale filter, the faces' name tooltips, Decline for an incoming DM call, `desktop_app_join_url` (Join opens `join_url`), stopping your huddle on Join, and the times' clock (always 12-hour; Slack's follows the locale's default, 24-hour outside en-US, en-GB, ko-KR and zh) |
| callout (slack.ts) | `zMv1` · `Xhau` Callout | absent | **full** | a callout inside a container lays its children out as a flex column with an 8px gap |
| canvas_table (slack.ts) | `8Rt0` CanvasTable | the per-block error line (its object `rows` failed the decode) | absent | |
| card | `ARtW` · `7Xkb` Card on Slack Kit's entity card (`aJif`) | absent | **partial**: the 180–340px card with its hero image (4:3, cover), its header (slack_icon glyph or icon image, title, subtitle), body, subtext and actions (partitionActions `fWi9`: three or more put the danger ones left; getButtonType `szY5`), live in messages, modals and App Home | the hero image's load-error picture (`Ec61`), the buttons' label truncation and the ghost style (drawn as the outline button), the explore-my-trial dark hero images |
| carousel | `ZDqH` CarouselBlock | absent | absent | horizontally scrolling cards |
| channel_section / channel_set (slack.ts) | `865t` | absent | absent | |
| condition (slack.ts) | expanded first (`CRur`), no renderable | absent (drew nothing) | **full** | |
| contact_card (slack.ts) | `zb3H` ContactCard | absent | absent | |
| container | `yWQF` ContainerBlock | absent | **full** | the collapse's 0.16s height animation |
| context | `0v+p` | full | full | |
| context_actions | `fYf1` | full | full | (agent.go) |
| data_table | `6dWo` DataTableBlock | absent | absent | pagination, sorting, raw/rich cells |
| data_visualization (slack.ts) | `vxqh` (lazy chunk) | absent | absent | pie/bar/line/area charts |
| divider | `Iu9o` | full | full | |
| event (slack.ts) | `0lYF` | absent | absent | calendar event card |
| fallback_canary (slack.ts) | none: always its fallback | absent (drew nothing) | **full** | |
| file | `QRSm` FileLazy | partial | partial | remote file cards (not re-audited) |
| header | `nN7E` | full | full | |
| image | `8plt` | full | full; its image loads like Slack's (AttachmentImage `UvHM`: getImgProxyUrl `d0up`): directly only from https on Slack's hosts, through the image proxy (`slack-imgs.com/?c=1&o1=ro&url=`) from anywhere else, plain http included, and its box stays empty while the image loads or if it fails | a `slack_file` given by `id` alone (Slack reads the file's thumbs); a deleted or unreadable `slack_file`'s unable-to-load picture (`Ec61`); with no `image_width`/`image_height` (the API adds both) Slack's AspectBox draws the image 0×0, where Clack keeps a 360×240 box |
| input | `h/Gw` InputBlock | full in modals and App Home; partial in messages (label + placeholder) | same | |
| loading (slack.ts) | `KSQB` | absent | **full** | |
| markdown | none in messages (server-translated) | Clack converts it itself | same | Slack's client would show the fallback card if one arrived untranslated. Kept, because the agent-thread fixture (Karim's report, 2026-11-12) stores one |
| message_embed (slack.ts) | `ACXQ` MessageEmbed | absent | absent | |
| plan | `5mjF` PlanBlock | full | full | (agent.go) |
| rich_text | `N5ZY` · `EAqS` → `qaNC` RichTextRenderer | full | full | see rich_text below |
| section | `bzDH` | full | full | |
| share_prompt (slack.ts) | `JcWK` | absent | absent | |
| share_shortcut (slack.ts) | `KrN7` ShareShortcut → WorkflowEntity on `VLgN`/`aJif` | absent (drew nothing) | **partial** | Forward workflow (Slack's share dialog for a workflow: the hover bar's button and More actions' item, `Tw8O`), More actions' Feature workflow and Add workflow to channel aren't in Clack, so none is drawn; the other-org badge (`ISuZ`, hermes_next); the VIP badge after the title (`LA2n`, for a bot that is one of your VIPs); ShortcutIcon's app and workflow icon fallbacks (`mp5R`: the app for `app_id`, the workflow's `icons.image_96`); the hover shadow; the Slackbot-message attachment variant (`7lAH`) |
| table | `POW9` Table | partial: a `raw_number` cell's numeric `value` failed the whole block ("This content could not be displayed."), `cell_text` cells drew empty | **full** | |
| task_card | `1+vf` TaskCard | full | full | (agent.go) |
| timeline (slack.ts) | `9B8s` Timeline | absent | absent | |
| video | `u8h8` VideoBlock | partial (drawn as an unfurl card) | same | not re-audited |
| share_skill, share_workflow, salesforce_record_card, search_top_results, mcp_app, page, layout, prototype, related_automation_decorator, attachment_reference, canvas_mermaid, message (client only) | `1Ine`, `ZiEp`, `bgQL`, `jBv3`, `bwbq`, `eMXm`, `+42+`, `b3P2`, `9H1Y`, `jBv3`, `iToP`, `jBv3` | absent (layout: the per-block error line, its object `rows` failed the decode) | absent | |
| expandable, group, error (client enum) | no renderable | drew nothing | **fallback** (the card) | |
| a type nobody knows | no renderable | the card; nested unknown fallback items skipped | **fallback** (Slack's chain exactly) | |

## Elements

| element | Slack | before | after | what's missing |
|---|---|---|---|---|
| button | `pPpA` | full | full; in an actions block `visible_to_user_ids` hides it from members it doesn't list (`/jJF` maybeRenderActionsElement). A section accessory or an input element draws for everyone, since neither the section, the input nor the button reads the list | `agent_prompt` / `agent_prompt_display` (send a prompt to Slackbot in an agent DM) |
| checkboxes | `JSDD` | full | full; `visible_to_user_ids` as for button | |
| channels_select, conversations_select, users_select, static_select, external_select | `rlGy`, `6bah`, `1hVl`, `2RVQ`, `v36g` | full | full | |
| multi_* (5) | `xjrl`, `bgmB`, `RBm/`, `pMe8`, `yIlz` | full | full | |
| datepicker, timepicker, datetimepicker | `KoX7`, `MUoz`, `UXHZ` | full | full | |
| plain_text_input, email_text_input, url_text_input, number_input | `5/Mo`, `EZY9`, `MjMC`, `kdgU` | full | full | |
| rich_text_input | `5gdY` | full | full | |
| file_input | `ISYo` | full (modals) | full | |
| overflow | `tZYQ` | full | full | |
| radio_buttons | `DNgy` | full | full | |
| workflow_button | `wDHu` | full | full | |
| icon_button | `RVG0` | full | full; it hides itself from members its `visible_to_user_ids` doesn't list, wherever it is (`X2Vm`). In an actions block its empty action box stays (8px) | a hidden icon_button accessory (outside @slack/types, which allows icon_button only in context_actions) takes no room; Slack keeps its empty 8px accessory box |
| feedback_buttons | `1ou8` | full | full | (context_actions) |
| image (element) | in section/context/card | full | full | |
| split_button, password (client only) | `jHw9`, `nvsC` | absent | absent | |

## Composition objects

| object | before | after | notes |
|---|---|---|---|
| plain_text (`emoji`) | full | full | a top-level text object now draws (Slack's `plain_text`/`mrkdwn` renderables) |
| mrkdwn (`verbatim`) | full | full | |
| option (`text`, `value`, `description`, `url`) | full | full | |
| option group | full | full | |
| confirmation dialog | full | full | |
| conversation filter | full | full | |
| dispatch_action_config | full | full | |
| slack_file (`url` / `id`) | partial | partial | `id` alone |
| raw_text / raw_number / cell_text (table cells) | raw_text only | **full** | |
| url source (task_card `sources`) | full | full | |

## rich_text

| piece | before | after | what's missing |
|---|---|---|---|
| rich_text_section, _list (`style`, `indent`, `offset`, `border`), _quote (`border`), _preformatted (`border`) | full | full | |
| rich_text_header (`level`) | absent (drew nothing) | **full** | |
| rich_text_divider | absent (drew nothing) | **full** | |
| check lists (`style: "check"` / `checklist`, `checked`) | absent (bullets) | **full**: RichTextList's 16px `checkbox-filled` / `square` icon (each item's `checked`, else the list's), 8px before the text, the item 4px in at indent 0 and 28px per step deeper, its next lines wrapping back under the icon; bullets where the rich_text renderable doesn't allow checkboxes (sS: anything but a message, a thread and a Slackbot message). The two glyphs are in Clack's icon set, held to Slack's by icon-shots | |
| preformatted `language` | absent | absent | Slack highlights it (`VKeu`) |
| text, link (`unsafe`, `truncated`), user, channel, usergroup, broadcast, emoji (`unicode`, `skin_tone`), date (`format`, `url`, `fallback`), color, message_mention, canvas, list_record, workflow_mention | full | full | |
| file (→ `rich_text_file`, `v1Jk`) | partial: its text | **partial**: Slack's File slug (getFileSlugAttributes: getFileDisplayName's label (title, name, the status's string, "Untitled"), "Audio clip" / "Video clip", getFileTypeSvgIconName, trash / eye-closed and disabled when getFileStatus says the file isn't there to see; a normal file's Name with its emoji, a canvas's title_blocks, in the element's style) | eight type glyphs Clack's set lacks (transcript, gauge-filled, pdf-file, binary, disc-image, text, presentation, zip) draw Slack's generic `file`; a list's label from the lists store; getFileStatus's canvas and list blocks read from file_access, and DSA restrictions; a click follows the link (Slack opens its file viewer); the slug's Download menu |
| tag, variable, workflowtoken, skill, citation, folder_mention, salesforce_data_field, canvas_user_mention, work_object_mention, attachment_mention | partial: their text | partial: their text or link. A citation without `details.citation_type` draws nothing: Slack's Citation doesn't take it, and the last branches' gate leaves it empty | their own components |
| team | its id | **full**: Slack has no renderer for it, so the rule below applies (a team element has no text: nothing) | |
| an inline type nobody knows | its text | **full**: RichTextRenderer's (`J825`) last branches. A null `fallback` draws nothing; an object `fallback` draws in its place. Otherwise UnknownElement (`Dvg3`) gets its `text` and `url` only when each is a string and the type isn't one of Slack's rich text types (`BeP8`, `tlJF` against N8wa `Lv`): a link to the url, else the text, else nothing (inside preformatted text too) | |

## Shots and tests

The gallery's `12-coverage` part (`internal/fakeslack/gallery_coverage.go`) holds a fixture for each block this round built: calls (ended, running with a facepile, waiting), alerts at every level, callouts in several colours, containers (fixed header with a divider, collapsible, collapsed by default), rich_text headers and dividers, the fallback chain, and a condition block. The loading block sits in a thread reply, because its spinner turns for ever as Slack's does and `TestIdleFrames` holds the gallery channel to no idle frames. `bk-gallery-shots` saves the channel page by page in light and dark, and the loading thread as `gallery-thread-loading-*`.

Tests that fail on `e93a5e70`: `TestBlockFallbacks`, `TestConditionBlocks`, `TestCallBlock`, `TestBoxedBlocks`, `TestRichTextHeaderDivider`, `TestTableTextCells`, `TestUnknownInlineElement`, `TestVisibleToUserIDs` (richtext) and `TestCallCardLayout`, `TestBoxedBlocksLayout`, `TestRichHeaderDividerLayout`, `TestContainerLayout` (richview). The tests that reach new API do it through `*_api_test.go` files, which the red-evidence branch stubs.

Round 1's review fixes, with tests that fail on `8126e895`:

- `visible_to_user_ids` is read only by the actions block. A hidden element keeps its place, and section accessories and input elements draw: `TestVisibleToUserIDs` (rewritten), `TestActionsHiddenElements` (richview), `TestMessageVisibleTo` and `TestSurfaceVisibleTo` (blockkit, modal and App Home).
- Buttons in a container, a callout and a fallback payload draw and send `block_actions` with their own block's id: `TestNestedBlockElements` (clicks, on the elements gallery's new `nested` family), `TestNestedElementsRichview` (richview's own layout with the message host), `TestSurfaceNestedElements` (modal and App Home).
- A message's alert shrinks to its text: it's the flex item of its block wrapper, and `.c-alert` sets no width. It wraps only at the column. Inside a callout or a container, and in a shared message's text, it fills the width: `TestAlertWidth`.
- The loading block's spinner sits centred (`.infiniteSpinner{margin:0 auto}`). Its ring fills the 52px box, because `noMargin` gives it a 78-unit viewBox. The track is content-ter at .25 and the arc content-hgl-1: `TestBoxedBlocksLayout`.
- The fallback chain's edges:
  - A container's unknown child is the card, a callout's draws nothing (`TestBlockFallbacks`).
  - Blocks that draw nothing leave an empty body, not the message text (`TestBlockFallbacks`, `TestConditionBlocks`).
  - Notifications read the raw blocks as `convertMessageToPlainText` does (`TestNotificationText`), and search results drop the card (`TestSearchText`).
  - Unknown inline elements take RichTextRenderer's last branches (`TestUnknownInlineElement`).
- A field shape that only some blocks use no longer fails the decode into the per-block error line: auth_provider's text-object `name`, and canvas_table's and layout's object `rows`. Those blocks now follow the absent rule (`TestBlockFallbacks`).
- The gallery's dark pages are no longer shot with the loading thread open: `bk_gallery_test.go` closes the thread after its shot. (The duplicate end pages weren't gone, though: see round 3.)
- Tests for round-1 code that had none: blockkit's condition expansion in a message, a modal and App Home (`TestConditionElements`), and the facepile's "+N" over its fifth face (`TestCallFacepileOverflow`). A mutation run proves them: the same tests with the expansion and the overlay's layer removed.
- The cards' texts take Slack's tokens, tested by `TestKitCardTokens`:
  - a container's title is content-pry and its subtitle content-sec;
  - an alert's text is content-pry, with its links in that colour and always underlined (`.c-alert :any-link`);
  - a call card's title and status are `sk_primary_foreground`, and its Meeting ID is `sk_foreground_max` at .7;
  - a container's caret is 1em (15px), with 12+8px before it;
  - a callout's border sits over its own fill.
- Call cards, tested by `TestCallCardEdges` (richtext and richview):
  - a DM call names the other member, through the call's D channel (getOtherMemberNameInDmCall);
  - a missed or declined DM call that has a `join_url` and a hidden facepile draws no footer band;
  - an external participant without an avatar shows ava_0002;
  - odd `app_icon_urls`, channel and participant values no longer fail the card.
- An inline file slug draws its label beside its icon. Round 2's first cut drew the icon alone, which the gallery shot showed: `TestFileSlugLabel` (richview).
- A container's or a call card's toggle survives the message changing (a reaction, a reply, `chat.update`). The View keys it by the container's block_id (else its place among the message's containers) and by the call id. A change to `is_collapsible` or `default_collapsed` resets it, as ContainerBlock's `useEffect` does: `TestCollapseSurvivesUpdates`. A container `width` Slack lacks, or `null`, now draws full width, since "standard" is only the default for an absent field: `TestBoxedBlocks`.

Round 3's fixes, with tests that fail on `f4a38c2c` (Karim's icon's on `f3fd8e49`, the first three's fix):

- Alerts in modals and App Home fill the view. A view's block wrappers aren't flex (WSwB's wrapViewBlock and wrapAppHomeBlock), so `.c-alert` is full width there, as in a callout or a container: `TestSurfaceAlertFills` (blockkit), `TestAlertFillsViews` (richview).
- A button in a container, callout or fallback inside an attachment sends the attachment's container, `{type: message_attachment, attachment_id}` (AttachmentBlocks' containerActionsPayload), not the message's: `TestNestedBlockElements`.
- `share_shortcut` settles. With no bot, a bot id that isn't member-shaped, or one users.info didn't find, the bot is a non-existent member (getMemberById `DOS4`) and the card is enabled. Only a member-shaped id still being fetched is the loading card, and that one opens the details and has its actions bar: `TestShareShortcutBlock`, `TestShortcutCardLayout`, `TestMemberDeletedStates`.
- Karim's weather icon (#clack, α.112): an image block on a plain-http host drew an empty box. Clack's image fetcher refused anything but https before it got to Slack's image proxy, which loads http as well. Such an image now loads through the proxy, and never from its own host. The gallery's `coverage/http image` is a message unfurl like Karim's (a 64×64 icon on `http://icons.example.net`, which only the fake's proxy serves): `TestHTTPImageBlockLoads` (richview, with the session's fetcher as cmd/clack builds it, `imgcache.SessionFetch`), `TestPublicFetchProxiesHTTP` and `TestSessionFetch` (imgcache). The box is empty while the image loads or if it fails: Slack draws the image as the box's CSS background, with only the 1px ring showing (`TestImageBlockEmptyUntilLoaded`, a pixel test in ci's list).

Round 3's other fixes (the review's MINORs and NITs), with tests that fail on `7da8f94e`, Karim's icon's fix. The exceptions are the ones the bullets call tests for claims that had none, `TestCardActionSlots`, and the ui and messages wiring tests, which hold existing behaviour. Two more runs back the round's evidence:
- `fallback_test.go` as at round 3's head fails on `8126e895` at 2c3cbf21's case ("a rich_text piece or an inline element at the top level draws as inside a rich_text").
- 12685e07's header guard fails when the bk `level` decodes only strings (a mutation run): `TestBoxedBlocks` "header level" and "header beside an alert".

- A notification's body follows convertMessageToPlainText's own table (2xQY, in Eley's default verbosity), not the drawing path. Alerts, callouts, cards, calls, loading, inputs and files add nothing. A container gives its title, a share_shortcut its workflow's title, an image block its title (not its alt text), a table its rows with cells joined by ", ". A list item is "• item." with ◦ and ■ (or a., i.) for nesting, and a check list is a plain list there. When the blocks or the message text give anything, that is the whole body; only when both are empty do the attachments speak: `TestNotificationText`.
- Blocks that are all null or rich_text without elements leave an empty body. The text doesn't stand in, since the array isn't empty (`iub5`); `lby4` draws nothing for them: `TestBlockFallbacks`.
- Inline file slugs take getFileStatus (`SRWA`, `richtext.FileStatusOf`) over a message file's or a files.info answer's mode, file_access, is_deleted, is_hidden_by_limit and is_tombstoned. Their label is getFileDisplayName: the title, the name, the status's string ("Deleted file", "Hidden file", "Deleted canvas"…), else "Untitled". A file not fetched yet is the store's unknown object, an enabled "Untitled" slug. A normal file's label draws its emoji (a canvas its title_blocks) in the element's style. A skill canvas draws no slug, and a skill set folder takes the book icon: `TestRichTextFileSlug` (the gallery's tombstone among its cases).
- A share_shortcut whose function_trigger_id isn't a trigger id (`^Ft[A-Z0-9]{7,}$`) is the text "UNKNOWN SHORTCUT TYPE", as ShareShortcut draws it. A service trigger's byline names its service_name, else "a third party": `TestShareShortcutBlock`.
- The workflow card's details:
  - the byline is content-sec (WorkflowTriggerTypeLabel), the deleted card's caption tertiary;
  - the loading card's skeletons are surface.secondary, and the 86px content one is clipped to the card's corners;
  - the Uses AI badge sits 20px after the wider label, in every state;
  - the description's newlines collapse, and the title and caption take Panda's 22.5px and 16.25px lines;
  - Start Workflow sits 16px from the card's outer edge;
  - the ⋮ turns content-pry while hovered, and its menu opens from the button;
  - the deleted card gets the otl-sec border while hovered, as every state does (withHover), with no click: `TestShortcutCardDetails`, `TestShortcutCardLayout`.
- A card's title and subtitle take Panda's body and caption lines, 22.5px and 16.25px (StackedLabels): `TestCardLabelLines`.
- More actions puts Start workflow first for a link trigger (not for a guest; WjTy), and shows the VIP item only on a paid team (isPriorityVipAvailableForCurrentTeam).
- Start Workflow ignores clicks while its trigger's trip is in flight (isTriggerRunning: `blockkit.TripRunning`), so a double click no longer trips twice. Its spinner and disabled look aren't drawn yet.
- share_shortcut's ui wiring has tests. Through a row's richHandler, the card opens the details view for its trigger, the ⋮ opens More actions at the button (window coordinates), and Start Workflow trips the link from the message, once while that trip is in flight (`TestWorkflowCardHandler`, messages). Through the ui root, More actions' items and order, its Start workflow's trip, and the details view are covered by `TestShareShortcutCardWiring`; `TestMessageActionsHooksWired` checks the More actions hook too.
- A toast's body for a message with blocks is converted with a resolver that asks for no one (getContentForNotification runs after ensureModelsArePresent). A member the toast's wait brought is named; one users/info answered too late for reads as nothing and isn't asked for again: `TestNotifyNamesABlockMention` (flake-check's toast-names job).
- The gallery shots end at the message list's end (`AtBottom`). They used to end at an unchanged frame, but past the end the frames still differed by the composer's blinking caret, which left duplicate end pages (light 20 to 25, dark 20 to 23 on f4a38c2c).
- Call cards: without an app icon the card draws renderCallIcon's font icon (9CtN). That's phone, phone-flat once the call ended, and call-missed or call-declined for a DM call the viewer missed or declined. Those three glyphs are icon-font glyphs (U+E122, U+E168, U+E167) drawn on their outlines' centrelines and held to the font by icon-shots (`fontRef`): `TestCallGlyph` (richtext), `TestCallIconGlyph` (richview). An external participant whose avatar_url fails to load shows ava_0002 (Facepile's renderError, renderExternalAvatarFallback): `TestCallFaceLoadError`.
- Tests for round-1 claims that had none:
  - In a modal, a fallback payload's button and a container child's button send block_actions with the child block's id and the view's container (`TestSurfaceNestedClick`).
  - A message row's rich text allows check lists' checkboxes (`TestRowAllowsCheckLists`, messages).
- A container the viewer collapsed in a modal or App Home stays collapsed through a views.update or an App Home publish. The surface keeps each block's view by Slack's React key (Q5CE: block_id, else type-index), as ViewBlocks' SUIRenderer keeps its items; a new view starts over: `TestSurfaceCollapseSurvivesUpdate`.
- A failed action from an actions block nested in a container or callout of a modal or App Home draws its error line under the buttons, as a top-level actions block does (`/jJF` renderBlock): `TestSurfaceNestedActionError`.
- A card's buttons in a message are Card's one row (`.actionsLeft`/`.actionsRight`): every action shows and they shrink rather than wrap. They get no "+ N more" and no status icons; those belong to the Actions block. The right group ends at the content's right edge and the danger group starts at its left, 12px under the body: `TestCardButtonsOneRow` (blockkit), `TestCardActionSlots` (richview, the Elements-host path).
- A check list in an attachment's blocks or in a forwarded message is a bullet list. That rich text renders in the message_attachment context, and RichTextList's checkboxes come only in a message, a thread or a Slackbot message (s3Wf sS): `TestCheckListInAttachments`.
- A card's icon image sits on ThumbnailContainer's surface.secondary box (radius 8) and is never scaled up (object-fit contain at max 100%), so a small icon keeps its size, centred: `TestCardIconBox`.
- An alert's max-content measure numbers its mention pills after the message's own, so in pill mode (CSS or patched mentions) each reads its own component state: `TestAlertMaxContentPills`.

## Next

Round 2 built `share_shortcut`, the inline file slug, `card` and check lists. What's left, in order, with what each needs:

1. `carousel` (`ZDqH`): LSo8's horizontal scroller of 340px cards 16px apart, with arrows and shadows inside and paging by whole cards. Richview has no horizontal scrolling or clipping yet, and the partly shown card needs both.
2. `data_table` (`6dWo`): pagination, sorting, raw and rich cells.
3. `contact_card` (`zb3H`): Slack Connect's introductions card. Its Save contact info and Start DM actions call the API and open their own modals, and fakeslack needs both endpoints first.
4. `timeline` (`9B8s`) and `event` (`0lYF`, a calendar event card).
5. `message_embed` (`ACXQ`): the referenced message drawn as a message unfurl. richtext needs a resolver for a stored message by channel and ts.
6. `share_prompt` (`JcWK`): the prompt card, CvC8, or 6HeA for Slackbot's.
7. The call card's open items:
   - the ended card's grayscale icon and faces (richview has no image filter);
   - the faces' name tooltips;
   - Decline on an incoming DM call;
   - Join opening `desktop_app_join_url` when the OS has a handler, and ending your huddle.

Then the rest of the client-only blocks.
