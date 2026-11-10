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

## Keyboard

Slack's table grid is a fork of react-datasheet-grid. Read from build a84662d2d135 (`modern.vendor`): `8dY2` (the grid, `useKeyCommands`, `GridField`), `3VkJ` (`GridView`, `BaseFieldInputRenderer`), `W077` (the text editor), `b2eO` (rating), `mhiL` (`LinkColumn`), `x8/8` (aria labels), `jPF5` (the experiment), `Y3LT` (Mousetrap). The CSS is `client-boot-styles`. Clack's port is `internal/ui/listview/keys.go`, with one router test per binding per mode in `keys_test.go`.

### The two modes

The pref is `lists_tabbing_navigation_option_type`. The prefs dialog's `ListsTabbingPref` writes `tab_nav` (Data entry mode) or `arrow_nav` (Full keyboard mode) with the normal user-pref save. It checks Data entry while the pref is unset. It renders nothing unless the experiment `lists_tabbing_navigation` is `on`.

- `enableTabbingNavigation` (3VkJ `tg`) is the experiment on and the pref unset or `tab_nav`. With it, ↑/↓ cross group boundaries. Without it (Full keyboard mode), they stop at the group's first and last row (`useKeyCommands` eJ/eX: `J === em - 1 || ex`).
- Tab and Shift+Tab move between cells only when tabbing is on and the pref was written (`e3`/`e4` return on `!eP`). They wrap to the next or previous row's first or last cell. At the end of the list or group they call `onFocusLeave` and let the browser move focus. In every other case Tab is the browser's, so it leaves the grid. Clack doesn't ask Gio for Tab then, and Gio moves the focus.
- `nextRowOnSave` is `!arrow_nav` (`BaseFieldInputRenderer`). In Data entry mode Enter commits a text, number, link, message or default cell, or a single-select pick, then moves down a row in the same column. On the group's last row the primary column adds an item (`GridField` `tS` calls `onAddRecordKeyboardEvent`). Full keyboard mode stays on the cell. Dates (`DateSelectComponent`), people and multi-selects never move.
- Full keyboard mode also takes Add item out of the tab order (`AddButton` `tabIndex: -1`). It traps Tab inside non-primary text editors (`W077` `e1`, hidden tab stops). Clack's editors have no buttons to trap Tab between, so a Tab there moves the focus away and saves.

### Bindings (a cell active, not editing)

| keys | Slack | source |
|---|---|---|
| ←/→ | previous/next column, stop at the edges | eQ, eZ |
| ↑/↓ | previous/next row; a row out of view scrolls to the top (`scrollToKey`) | eJ, eX |
| Shift+arrows | extend the selection (`selection.max`); a plain move collapses it | eY |
| Ctrl/⌘+Shift+↑/↓ | stretch the selection to the first/last row, of the group when grouped | t_, tp |
| Home/End | the row's first/last cell | e7, e6 |
| PageUp/PageDown | the first/last row in view, or one more row when already there; within the group | e9, tm, `getNextFirstFocusableItemInView` |
| Tab/Shift+Tab | see the modes | e3, e4 |
| Enter, Space, F2 | edit the cell, or its custom action (below) | `GridField` onKeyDown |
| a printable ASCII key (no Ctrl/⌘) | start editing; the editor opens with the cursor at the end (`setCursorAtEnd`), so the key lands after the value | `GridField`, `W077` |
| Esc | leave the cell (no ring); focus coming back restores it (`previousActiveCell`) | `clearSelection`, aD, aO |
| Backspace/Delete | clear the selection with each column's `deleteValue`; a selection across every column deletes those items ("Item deleted"/"Items deleted") | th, eD, `deleteRecords` |
| Shift+Enter | add an item below and edit its name | e2 |
| Alt+Enter | open the item | e1, `onRowOpenKeyboardEvent` |
| ⌘/Super+Enter | on a link: open it | `LinkColumn` (`metaKey`) |
| Ctrl/⌘+C, X, V | copy (toast "Cell copied"/"Cells copied"), cut, paste | eL, eB, eV |
| Ctrl/⌘+Z, Shift+Z | undo/redo | e0 |
| Ctrl+M, Shift+F10 | the cell's context menu | e5 |

Mousetrap only prevents the default when a handler returns false, and none do. So Chrome still pages the list on PageUp/PageDown. Clack moves the cell, then scrolls by 7/8 of the view, Chromium's paging step. Chrome would also scroll the list to its top or bottom on Home/End. Clack doesn't, because the active row would leave the screen.

### Per column type

`skipEditMode` (3VkJ `t2`) picks a custom action over the editor. Context 0 is a click and 1 is a key.

- Created/edited by and time: "Not editable" (announced, nothing opens).
- Checkbox (without `markAsDoneWhenChecked`), to-do completed, vote: the key toggles (`onCustomClickCell`; to-dos announce "Completed"/"Not completed"). Printable keys count too, so typing a letter on a checkbox toggles it.
- Attachment: Enter/F2 focus the attach button (`FileAttachmentsComponent`). Clack can't attach files to cells, so it shows the guard's toast.
- Link with a value: Enter/F2 open "Link options": Open link, Edit link, Copy link, Display as title/URL (when the link has a title), Remove link. Space does nothing. An empty link edits.
- Message with a value: Enter opens its menu. Clack can't edit message cells, so it shows the guard's toast.
- Rating: a click picks the star. A key opens the range editor: ←/↓ −1, →/↑ +1, Home 0, End max, a digit sets that value, Enter keeps it, Esc restores it. Blur saves.
- Text, number, email, phone, link: the inline editor. Enter commits, Esc cancels (`RecordTextInput` restores the value), blur commits.
- Select, people, channel: the cell's menu, at the cell. A single-select pick moves down in Data entry mode. Typing a letter opens it too (Slack's search field would take the letter; Clack's menu has no search).
- Date: the editor. Enter sets the date and stays. Esc cancels.

Clack differs in three places. Completed columns and mark-as-done checkboxes toggle on Enter/Space in Clack. In Slack they open the checkbox "editor". Shift+Enter, and Enter on the last row, open Clack's new-item editor at the end of the group, because Clack creates items without a position. And Clack only edits rows that exist, so a paste stops at the last row.

### Copy and paste

Copy writes `text/plain` as tab-separated rows of each column's `copyValue` (8dY2 `t2`). Text becomes plain text. Selects become their labels, joined with `|`. Checkboxes become YES/NO. Votes become the voter count. Ratings, numbers and dates stay raw. People, channels and files stay as raw ids. Created/edited times are the raw timestamp. Slack also writes `slack/texty` and an HTML table.

Paste parses with `qtAi` `to`: tabs, newlines and quoted cells. Plain text with several rows from another app becomes one cell, its non-empty pieces joined. Only Slack's own `slack/texty` copy keeps rows. Clack's clipboard has text only, so Clack treats text equal to its last copy as its own. Each column's `pasteValue` applies. Text joins its lines with spaces and an empty value clears. A select takes the label's option, or clears when nothing matches. A checkbox is checked unless the text is one of `false no off disabled 0 n f unchecked undefined null wrong negative` or empty. A rating is `parseInt`. Clack's guard skips cells it can't write.

### The focus ring, selection and edit state

- The active cell (`p-grid_cell--cell_focus`) has a 2px `lagoon-50` border on `base-hgl-1`: `#e3f8ff` light, `#001a2d` dark, or the theme's own.
- A selection (`p-grid_multi_cell--selected`) is `base-hgl-1` with 2px `lagoon-50` on its outer edges.
- Editors (`p-grid_field__input--is_editing`, the rating and select editors) have a 2px `lagoon-50` border on the base background.

Screenshots: `lists-kb-ring`, `lists-kb-selection`, `lists-kb-edit` and `lists-kb-rating`, light and dark, in the `lists-shots` artifact.

### Screen readers

Primary cells are `rowheader` and the rest `gridcell`, with `aria-colindex`. The label (`x8/8`) is "{emoji}, N vote(s)" for votes, "{v} of {max}, {emoji}" for ratings, and "A, B" for several select options. Checkbox inputs say "Yes"/"No". Other cells read their text. The description (3VkJ `nE`) is "Read only" for computed columns and "Including you"/"No vote from you" for votes. Primary cells also say "Has N comments"/"Has new comment". Clack's grid doesn't load reply counts, so it leaves that out. Announcements go through `sendMessageToAnnouncerAPI`: "Not editable", "Completed"/"Not completed", "Yes"/"No".

Clack puts each drawn cell in Gio's semantic tree: the label, the description, and `Selected` on the active cell. The last announcement is appended to the active cell's description. Gio has no grid roles. Clack's desktop builds have no screen-reader bridge yet, so for now only tests read this tree.

### Not ported

- Undo/redo: Clack has no list history.
- The cell context menu (Ctrl+M, Shift+F10): Clack's grid has no item context menu.
- Mouse selection (drag, Shift+click).
- Deleting highlighted text inside a cell.
- New rows created by a paste.

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
- The keyboard: the active cell, the selection, and every binding in both modes of the Lists keyboard pref (see Keyboard above).
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
