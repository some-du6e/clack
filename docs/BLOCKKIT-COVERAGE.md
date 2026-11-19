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

A message that has blocks draws them instead of its text (client-boot `iub5` shouldRenderBlocksInsteadOfText), even when they draw nothing. A condition with no case for the desktop, or a lone empty fallback, leaves an empty body. The text stands in only when the blocks array is empty or holds nothing but nulls and empty rich_text (`lby4`). In Clack it also stands in when a block that Clack can't draw yet drew nothing.

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
| call (slack.ts) | `FkcF` · `9CtN` PlatformCallSubtype | partial: a generic "Call" link card | **partial** | the ended icon's and faces' grayscale filter, renderCallIcon's phone-flat (ended), call-missed and call-declined glyphs (DM calls; they aren't in Clack's icon set yet, so it draws phone), the faces' name tooltips, Decline for an incoming DM call, `desktop_app_join_url` (Join opens `join_url`), stopping your huddle on Join, ava_0002 for an external avatar that fails to load (a face with no `avatar_url` draws it), and the times' clock (always 12-hour; Slack's follows the locale's default, 24-hour outside en-US, en-GB, ko-KR and zh) |
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
| image | `8plt` | full | full | a `slack_file` given by `id` alone (Slack reads the file's thumbs) |
| input | `h/Gw` InputBlock | full in modals and App Home; partial in messages (label + placeholder) | same | |
| loading (slack.ts) | `KSQB` | absent | **full** | |
| markdown | none in messages (server-translated) | Clack converts it itself | same | Slack's client would show the fallback card if one arrived untranslated. Kept, because the agent-thread fixture (Karim's report, 2026-11-12) stores one |
| message_embed (slack.ts) | `ACXQ` MessageEmbed | absent | absent | |
| plan | `5mjF` PlanBlock | full | full | (agent.go) |
| rich_text | `N5ZY` · `EAqS` → `qaNC` RichTextRenderer | full | full | see rich_text below |
| section | `bzDH` | full | full | |
| share_prompt (slack.ts) | `JcWK` | absent | absent | |
| share_shortcut (slack.ts) | `KrN7` ShareShortcut → WorkflowEntity on `VLgN`/`aJif` | absent (drew nothing) | **partial** | Forward workflow (Slack's share dialog for a workflow) and More actions' Add workflow to channel aren't in Clack, so neither is drawn; the other-org badge (`ISuZ`, hermes_next); a service trigger's byline; the Slackbot-message attachment variant (`7lAH`) |
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
| file (→ `rich_text_file`, `v1Jk`) | partial: its text | **partial**: Slack's File slug (getFileSlugAttributes: the file's title or name, "Audio clip" / "Video clip", getFileTypeSvgIconName, trash / eye-closed and disabled when the file isn't there to see) | eight type glyphs Clack's set lacks (transcript, gauge-filled, pdf-file, binary, disc-image, text, presentation, zip) draw Slack's generic `file`; a click follows the link (Slack opens its file viewer); the slug's Download menu |
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
- The gallery's dark pages are no longer shot with the loading thread open, and the duplicate end pages are gone: `bk_gallery_test.go` closes the thread after its shot.
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
   - Join opening `desktop_app_join_url` when the OS has a handler, and ending your huddle;
   - renderCallIcon's phone-flat, call-missed and call-declined glyphs.

Then the rest of the client-only blocks.
