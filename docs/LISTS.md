# Lists

How Slack's web client loads, follows and edits Slack Lists, and what Clack does. Sources: Slack's web client on the public CDN (build a84662d2d135, 2026-11-08: `modern.vendor`, `application`, `gantry-v2-async-client-boot-*` and the `gantry-v2-async-client-list-*` chunks), the method and Redux catalogues in IzieStratt/slack-datamine-plus, and read-only shape probes of Hack Club lists. The probes printed key names, value types and enum values only (column types, view types, operators). No list content was printed or saved.

Module ids below are `modern.vendor`'s.

## What a list is

A list is a Slack file with `filetype: "list"`, `mode: "list"`, `pretty_type: "List"`. `files.info` returns the usual file fields plus:

- `list_metadata`: `schema` (columns), `subtask_schema`, `views`, `icon` (`:emoji:`), `description`, `description_blocks` (rich text), `default_view`, `todo_mode`, `is_trial`, `integrations`, `creation_source {type, reference_id}`.
- `list_limits`: `row_count`, `row_count_limit`, `column_count`, `column_count_limit`, `view_count`, `view_count_limit`, `archived_row_count`, `max_attachments_per_cell`, `over_row_maximum`, `over_column_maximum`, `over_view_maximum`.
- `list_csv_download_url`, `editable`, `access`, `editors`, `last_editor`, `is_restricted_sharing_enabled`, `is_ai_suggested`, `is_modified_by_ai`.

Ids: columns `Col…`, select options `Opt…`, views `View…`, records `Rec…` (`mjHr`: `/^Rec[A-Z0-9]{8,}$/`, `/^View[A-Z0-9]{8,}$/`). The primary column has `key: "name"`; other keys are usually the column id. To-do lists use the keys `todo_completed`, `todo_assignee`, `todo_due_date`.

Channel tabs: `conversations.info` → `properties.tabs[]` has `{type: "list", id, label, data: {file_id}}`. The Files browser lists them with `search.modules.files` and `query: "type:lists"` (the thunk `fetchLists`). A list link is `https://<team>.slack.com/lists/<T…>/<F…>`. An item link adds `?record_id=Rec…`.

### Columns

`{id, key, name, type, is_primary_column, options}`. Types (`tuyK`): `text`, `rich_text`, `message`, `number`, `select`, `multi_select`, `date`, `user`, `attachment`, `checkbox`, `email`, `phone`, `channel`, `currency`, `rating`, `created_by`, `last_edited_by`, `created_time`, `last_edited_time`, `vote`, `assignee`, `due_date`, `completed`, `canvas`, `todo_assignee`, `todo_due_date`, `todo_completed`, `reference`, `link`.

These options were seen in real lists:

| type | options |
|---|---|
| text | `format: text` |
| select | `format: single_select \| multi_select`, `choices: [{value, label, color}]`, `default_value`, `default_value_typed {select}` |
| user, todo_assignee | `format: single_entity \| multi_entity`, `show_member_name`, `notify_users`, `default_value_typed {user}` |
| channel, canvas | `format: single_entity \| multi_entity`, `show_member_name` |
| date, todo_due_date | `format: single` (or `range`), `date_format` (moment.js; `default` = "MMM D, YYYY") |
| number | `format: standard`, `precision` |
| rating | `max` (3 or 5), `emoji`, `emoji_team_id`, `emoji_url` |
| vote | `emoji`, `emoji_team_id`, `emoji_url` |
| checkbox | `mark_as_done_when_checked` |

Option colours (`zl6T`) are `gray brown orange yellow green blue indigo purple pink red cyan`. Slack names them Gray, Horchata, Honeycomb, Sunflower, Grass, Lagoon, Indigo, Aubergine, Flamingo, Tomato and Jade, and paints them from the `--dt_color-plt-*` ramps of the same names.

### Views

`{id, name, type, position, columns: [{id, key, visible, position, width, should_wrap_text}], sorts: [{key, column_id, ascending}], filters: [{key, column_id, operator, values, typed_values}], grouping: {group_by, group_by_column_id}, is_all_items_view, is_locked, show_completed_items, stick_column_left, info_column_filters, created_by, date_created, default_view_key}`.

View types (`LIzA`): `table`, `board`, `timeline`, `list`, `gallery`, `calendar`, `map`, `record`, `canvas_inline`. Real lists had only `table`, `board` and `record`. Every list has one `record` view, which holds the item pane's column order. `record` and `canvas_inline` are never tabs. The view switcher (`q1RT`) offers Table (icon `spreadsheet`) and Board (icon `kanban`).

### Records

`lists.records.list {list_id, include_subtasks, archived, include_suggested, exclude_deprecated_values, cursor}` → `{records, response_metadata: {next_cursor}, limits}`. A record is `{id, list_id, date_created, created_by, updated_by, updated_timestamp, position, parent_record_id, thread_ts, view_positions {View…: position}, platform_refs, archived, is_subscribed, saved, fields}`. Each field is `{key, column_id, value: null, <typed>}`, with one array for its type:

| column | field |
|---|---|
| text | `text` (plain), `rich_text` (blocks) |
| select | `select: [Opt…]` |
| user, vote, created_by, last_edited_by, todo_assignee | `user: [U…]` |
| channel | `channel: [C…]` |
| date, todo_due_date | `date: ["YYYY-MM-DD"]`, `timestamp: [unix]` |
| created_time, last_edited_time | `timestamp: [unix]` |
| number | `number: [n]` |
| rating | `rating: [n]` |
| checkbox, todo_completed | `checkbox: bool` |
| email, phone | `email: [..]`, `phone: [..]` |
| attachment, canvas | `attachment: [F…]` |
| link | `link: [{originalUrl, displayAsUrl, displayName?, attachment: {url: unfurl}}]`. These keys are camelCase in responses. |
| message | `message: [{value: permalink, channel_id, ts, thread_ts?}]` |

The grid flattens a field to one value (`mjHr.getTypedFieldValue`). Multi-values are joined with `|` (selects, people, channels), `,` (attachments) or `:` (date ranges). Filters, sorts and search all read that flat value.

### Comments

An item's comments are a message thread in the list's companion channel, which is the file id with `C` for `F` (`7/Dp`). The thread's root is the record's `thread_ts`. An item that never had a comment has no `thread_ts`; the client then reads `conversations.replies {channel, ts: "<list id>-<record id>"}`, which answers with the root message (subtype `list_record_comment`). Comments are posted as replies to that root. `lists.getThreadsForRecord {list_id, record_id}` lists other threads that mention the item. The pane's empty state reads "Every item has a thread" / "Add comments to the thread to collaborate with your coworkers. Comments stay with the item no matter where it is shared."

## How Slack's client shows a list

- **Loading** (`gO15 fetchRecords`): `files.info` and `lists.records.list` in parallel. `nIb2` refetches single items with `lists.records.infoBatch` / `lists.records.info`.
- **Open** (`f/Ia fetchListsOpen`): `lists.open {list_id}` → `{pinned_view_ids}`.
- **Order** (`mjHr.sortRecordsByDefault`): the record's `view_positions[view]`, else `position` (fractional strings compared as strings), then `date_created`.
- **Filters** (`AhXZ.applyFiltersToRecords`; every filter must match). Operators (`pNWq`): `equal not_equal greater_than less_than greater_than_or_equal less_than_or_equal contains does_not_contain is_empty is_not_empty is_me is_not_me`. The operators offered per type are in `BFYq.UE`, their labels in `BFYq.R8`: "includes", "is after", "is not checked"…
  - Text matches case-insensitively on the plain text.
  - Selects and people compare option values or user ids. `viewer` means the viewer, and single-value columns compare only their first value.
  - Numbers never match an empty cell.
  - Ratings compare as strings.
  - Dates take `YYYY-MM-DD` or a dynamic value (`+haa`): `today yesterday tomorrow this_week next_week last_week upcoming overdue later since_i_last_viewed`. Weeks are ISO weeks, `upcoming` is after today+2 and `overdue` is before now.
  - Checkboxes and links filter only on empty/not empty. A false checkbox counts as empty.
- **Sorts** (`AhXZ.applySortToRecords`): lodash `orderBy` over the sort keys. Selects sort by option order, people by real name, text case-insensitively, votes by count, and dates by time. Empty values sort last either way.
- **Search**: case- and accent-insensitive over text, email, phone, number, rating, select labels, people and channel names, and vote counts.
- **Grouping**: the board's lanes and the table's group headers. Lane headers drop the pill background. The empty group is "Not Rated" (rating), "No date set", "No due date set", or "Ungrouped" (`getEmptyGroupLabel`). Counts read "{n} item(s)".
- **Grid metrics** (CSS `p-grid*`): header cells 32px with 6px margins, 13px gray-70 names (to-do completed shows a `check-circle` icon instead). Cells are 48px with a 1px gray-5 top border (gray-10 in dark), gray-10 outer edges, gray-0 row hover and a bold primary column. Default widths are in `use_get_column_width`: primary 384, date 200, checkbox 128, select 128, people 230, vote 96, rating 32·max+32, others 250. "Add item" is 44px with 13px bold gray-70 text. The grid always uses `p-grid--saturated-colors`, so select pills are ramp-5 (aubergine-10 for purple).
- **Board** (`p-board_*`): lanes are 340px with a ramp-5 background, radius 12 and a 15px bold title. Cards have a white background, a 1px ramp-20 border, radius 8, padding 12 16 16, and a bold primary field.
- **Header** (`p-list_header`): 50px. Icon, 18px black title, "Last edited …", actions. In the body, `p-list_title_header` shows the title at 28px black 36px from the left, with the description in 15px content-ter.

## Live updates

The gateway sends these events (`application` RTM handlers), each with `list_id`:

| event | payload | client |
|---|---|---|
| `list_record_created` | `record` | upsert |
| `list_record_updated` | `record`, or `deleted: true` + `id` | upsert / remove |
| `list_record_removed` | `id` | remove |
| `list_records_archived` | `ids`, `restore` | archive / restore |
| `list_cells_updated` | `cells: [{row_id, column_id, …typed}]`, `column?` (a new schema column) | update cells |
| `list_contents_merged` | | refetch records |

`file_change` / `file_shared` / `file_deleted` cover title, schema and view changes. Activity has `list_record_assigned`, `list_record_edited`, `list_user_mentioned`, `list_todo_notification`, `list_approval_request` and `list_approval_reviewed`, each with a `list_*_payload {list_id, row_id, …}`.

## Editing

Every fetcher snake-cases its arguments deeply (`MBHb`, `lpND.snakeCaseDeep`), so values sent are snake_case, link entries included.

| action | call |
|---|---|
| cells | `lists.cells.update {list_id, cells: [{row_id, column_id, <typed>}]}` → `{cells}`. Typed values (`xWHc`): `rich_text: [blocks]`, `number: [n]`, `select: [Opt…]`, `date: [YYYY-MM-DD]`, `user: [U…]` (people and votes), `channel: [C…]`, `attachment: [F…]` (canvas, files), `checkbox: bool`, `email: [s]`, `phone: [s]`, `rating: [n]`, `link: [{original_url, display_as_url, display_name?}]`. Plain text becomes one `rich_text_section` with a `text` element, or a `link` element when the whole text is a URL (`AhXZ.u1`). |
| new item | `lists.records.create {list_id, initial_fields: [{column_id, <typed>}], position, parent_record_id?, updated_timestamp, duplicated_record_id?}` → `{record}`. `updated_timestamp` is a hybrid logical clock stamp (`HGm0`): `<ms>-<counter, 4 hex>-<node, 16>`. |
| delete | `lists.records.delete {list_id, id}`; also `deleteMultiple`, `archive`, `restore`, `reorder`, `updateDeletion` |
| new list | `lists.create {name, description?, description_blocks?, schema?, icon?, template_id?, todo_mode?, context_channel_id?, include_canvas_inline_view, include_copied_list_records}` → `{list}` (a file). Templates come from `lists.templates` → `{templates, starter_templates}` (`template_files` / `starter_template_files` in some configurations). |
| schema, views | `lists.columns.create/update/delete/revert`, `lists.views.create/update/delete`, `lists.views.pins.add/remove`, `lists.update`, `lists.setDefaultView` |
| other | `lists.addToList` (a message into a list), `lists.duplicate`, `lists.history.*`, `lists.edits.*`, `lists.records.subscriptions.*`, `lists.getMyItems`, `lists.setupTodos`, `lists.import` |

The public `slackLists.*` methods aren't what the client uses.

## What Clack does

**Reading** (`internal/core/lists.go`, `internal/lists`):
- `List` makes Slack's two calls and keeps the result cached. `OpenList` sends `lists.open` and applies the realtime events above to the cached list (coalescing refetches to one per 400 ms). With realtime off, the view polls `files.info` every `core.PollInterval`.
- `internal/lists` is the model plus Slack's ordering, filter, sort, search and grouping rules, ported from the modules cited above.

**Drawing** (`internal/ui/listview`):
- Three modes: the full view (`lists/<F…>`), the side panel, and a channel's list tab.
- Header: icon, title, description and "Last edited".
- Toolbar: the view switcher, the view's saved filter, sort and group pills, search, and the Filter and Sort buttons, which list the view's own filters and sorts.
- Table: sticky header and primary column, horizontal and vertical scrolling, group headers, the row hover's "Open", and "Add item".
- Board: lanes, cards and "Add item" per lane.
- Every column type has a renderer: text, number and currency, select pills, people with avatars, dates in the column's format, created/edited times, checkboxes, to-do circles, ratings and votes as emoji, links, email, phone, channels, file and canvas chips (titles from `files.info`), and message links.
- The item pane (`Item`) lives in the secondary panel. It shows the item's name and its record-view fields, editable in place, then the comments thread and an "Add a comment…" box.

**Editing**:
- Checkboxes and votes toggle. Ratings take the star clicked.
- Selects and people get a menu: the options; for people, the viewer, people already in the list and DM partners. Multi-value fields toggle.
- Text, number, email, phone, link and date open an inline editor. Enter or clicking away saves, Escape cancels.
- "Add item" (table and lanes) creates an item with its name, in the group it was added to.
- Comments post to the item's thread.
- Create → List makes an "Untitled list". Files → Lists "+ New List" offers a blank list or a template.

**The fidelity guard** (`lists.Editable`, enforced in core before anything is sent):
- Columns Slack computes (created/edited by and time) are never written.
- Message, file, canvas and reference cells need pickers Clack doesn't have, so clicking one says "This field can't be edited in Clack yet (it has …)".
- Text is editable only while its rich text is plain text and bare links. Formatting, mentions, emoji, dates, lists, quotes, code and named links all refuse, and Clack checks that what it would write back reads as the same text.
- Date ranges and cells with several links stay read-only.
- New items take only the name and a group value.

**Not supported yet**:
- Editing views: saving filters, sorts, groups and columns, plus resizing and reordering.
- Schema changes, item reordering by drag, and subtasks (loaded but not drawn).
- Archive, history, CSV export, subscriptions and approvals.
- `lists.addToList` from a message, adding a list tab to a channel, and timeline, gallery and calendar views (none were found in real lists).

## Plugins

None of the Taut/Slick library plugins touch list UI.

## Code

- `internal/lists`: the model, the rules, cell encoding and the guard.
- `internal/core/lists.go`: load, cache, live events, and edits (`UpdateListCells`, `CreateListRecord`, `DeleteListRecord`, `CommentOnListRecord`, `CreateList`, `ListTemplates`).
- `internal/ui/listview`: the view, grid, board, cells, item pane and editing.
- `internal/ui/mount_features_lists.go`: the route, side panel, item pane, tab body, links and create. Hooks elsewhere: the channel tab body and click (`channel.go`), `messages.ListOpener` (list files and links in messages), Files → Lists (`views/files.go`), Create → List (`create_channel.go`), and Activity's list items (`views/activity.go`, `core/activity_items.go`).
- `internal/fakeslack/lists.go`: the "Launch tracker" fixture has every column type, a table, a board and a filtered and sorted view. A to-do list is `C0FAKE00001`'s tab. The fake implements the methods above and pushes their realtime events.
- `.github/workflows/lists-shots.yml`: screenshots in light and dark.
