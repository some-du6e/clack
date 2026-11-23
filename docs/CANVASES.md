# Canvases

How Slack's web client loads, follows and edits canvases, and what Clack does instead. Sources: Slack's web client on the public CDN (build a84662d2d135, 2026-11-08; chunks `gantry-v2-async-quip-collab-webapp`, `-quip-collab-editor`, `-client-boot-shared`, `-client-canvas-view`; build 9685e6ce144b, 2026-11-18, for "Slack's edit protocol"), the method list from IzieStratt/slack-datamine-plus, docs.slack.dev for the public `canvases.*` methods, and read-only structure probes of Hack Club canvases. The probes printed only tag names, class names and attribute names; no canvas content was printed or saved.

## What a canvas is

A canvas is a Slack file with `filetype: "quip"`, `mode: "quip"`, `pretty_type: "Canvas"`. Behind it is a Quip thread (`quip_thread_id`). `files.info` on a canvas returns, besides the usual file fields: `title`, `title_blocks` (rich text, for emoji in titles), `canvas_creator_id`, `edit_timestamp`, `editable`, `access` (`read`/`write`/`owner`), `editors` (user ids), `canvas_readtime`, `show_badge` (the viewer never opened it), `is_starred`, `dm_mpdm_users_with_file_access`, `org_or_workspace_access`, `canvas_printing_enabled`, `has_rich_preview`, `source_canvas_id`, `quip_thread_id`. `url_static_preview` and `permalink` are Slack web pages (`/docs/<T>/<F>`); Clack never loads them.

Channel canvases are channel tabs. `conversations.info` → `channel.properties.tabs[]` has `{type: "canvas", id, label, data: {file_id, shared_ts, mute_edit_updates?}}`. The old `channel_canvas` tab type is still listed with `is_disabled: true`. `properties.canvas` is `{file_id, is_empty, quip_thread_id, is_migrated, is_open_by_default}`. DMs carry canvas tabs the same way.

## How Slack's web client does it

**Document model.** The web client embeds Quip's collaborative editor ("quip-collab"). It talks to the top-level team's host (the enterprise host in an org) under `/canvas`:

| endpoint | what |
|---|---|
| `POST https://<host>/canvas/-/load-data?_x_version_ts=…` | loads the thread and its sections. The form field `request_binary` is a base64 binary protobuf (`syncer.LoadData.Request`); the answer is a binary `syncer.LoadData.ResponseWrapper` |
| `POST /canvas/-/fetch-latest` | `FetchLatest.Request{ThreadRequest{thread_id, client_thread_sequence, client_max_section_sequence}}` → the changes since those sequences (`ChangesData`) |
| `POST /canvas/-/edit-document` | saves sections: form fields plus `data_binary`, a base64 `syncer.DocumentData` |
| `POST /canvas/-/save-diagnostics-report` | diagnostics |

`buildCanvasTarget` puts the path on that host. `applyCanvasAuth` sends the token as `Authorization: Bearer` on the same origin, or as a `token=` form field cross-origin, which is the web client's case. The schema is Quip's own and changes with Quip's builds; "Slack's edit protocol" below has it, with the field numbers of build 9685e6ce144b.

Around it, Web API methods: `files.info`, `files.open` (`{file_id, supports_read_only_check, last_updated}` → `{viewers: [{user_id, is_focused, …}], should_subscribe_and_ping}`), `files.close`, `files.ping`, `files.createCanvas` (`{title, markdown, reason}` → `{file_id}`), `quip.getControllerInitData`, `quip.thread.synchronize`, `quip.getMentions`, `quip.lookupFileId`, `quip.lookupThreadIds`, `quip.history.getVersions`/`restoreVersion`, `quip.cloneCanvas`, `canvases.listHeaders`, `canvases.getTemplates`, `canvases.notificationSections`.

**Live updates.** Opening a canvas runs the `openFile` thunk: `files.open`, then it records the viewers and subscribes the Quip delegate to the canvas's document events. The realtime gateway sends:

- `document_update {file_id, payload}`: an edit was saved. The client looks up the Quip thread id and calls `fetchThreadChanges`, which is `fetch-latest`.
- `document_change {channel: <file id>, payload: base64}`: transient sections (cursors, selections, presence). The client also sends its own as `{type: "document_change", user, channel, payload}`.
- `file_opened` / `file_closed`: viewers.
- The message subtypes `channel_canvas_updated` / `tabbed_canvas_updated` and the event `file_channel_canvas_reset`.

**Title.** A canvas has no title apart from its document. Quip derives it (`extractTitleMetadata` in `quip-collab-webapp`): a TITLE-style section if there is one, else the first non-empty heading, else the first five words of the first text section (new-document default content skipped), else "Untitled". `files.info`'s `title` is that projection, and the export has no separate title element: #clack's canvas opens with a plain `<h1 id>`, then a `<p class="default-content line">`. So Clack draws an opening heading once, as the title, with the byline under it (it was also drawn as the first block, so the title showed twice). A canvas that opens with anything else keeps the file's title above it. Editing that heading is what renames the canvas in Slack.

**Header.** The canvas header toolbar has "Open in side panel" (full view), "More actions" (…), the comments button, "Ask Slackbot" and "Ask Agentforce" (both behind experiments). Strings: "Open in full view", "Open in side panel", "Open in new window", "Copy link to canvas", "Created by {name}", "Last edited", "Edited by {memberName} {date}".

## Slack's edit protocol

Everything here is from Slack's web client build 9685e6ce144b (captured 2026-11-18), read from the public CDN without a cookie:

- `gantry-v2-async-quip-collab-webapp.8c42ff1ca93bfb04`: Quip's syncer, document model, schema;
- `gantry-v2-async-quip-collab-editor.991e15af640943df`: the editor, one 1 MB concatenated module (`0ealK`), cited by function name;
- `client-boot.548618a2169f2a90`, `gantry-v2-async-client-boot-shared.1ea0c0b850226339`, `-client-boot-render.9358ec6724e1b096`: Slack's side of the embed and the realtime handlers.

Module ids are rspack ids in those chunks. Field numbers come from Slack's own descriptors: a node script loaded `QkBk` (the `syncer.*` messages) and `o3+F` (`proto.section.*`) and called `getDescriptor()` on every message class, so they are exact for that build. The example bytes were produced by Slack's own serializer (`p8gD.toBinary`) on made-up values. No canvas endpoint was called and no canvas was opened to write this. The scripts are in /root/clack-ops/scratch/canvas-edit/tools (`schema.cjs` dumps the schema, `vectors.cjs` the examples).

### Transport

Three POSTs carry an edit: `load-data`, `fetch-latest` and `edit-document`. Module `zRL+` builds them.

- **URL.** `buildCanvasTarget(path, teamId)` gives `https://<host>/canvas/-/<path>?_x_version_ts=<v>`. The team is the syncer's `topLevelTeamId`, which is `XgZX.Kc`: the enterprise id when the workspace is in an org, else the team id. The host is `getHostForTeamId` (client-boot `jfry`), the host of that team's `url`. So in Hack Club the calls go to the enterprise host with the enterprise token. `<v>` is the page's `data-version-ts` (`xIyL.getVersion`), or "noversion".
- **Auth.** The web client runs on app.slack.com, so the canvas host is cross-origin and `applyCanvasAuth` appends `token=<xoxc…>` to the form, with no Authorization header. The request is `credentials: "include"`, so the `d` cookie goes with it. (Same-origin, it would send `Authorization: Bearer`.)
- **Body.** `application/x-www-form-urlencoded`; `encodeBody` writes each value as `encodeURIComponent(String(v))`.
- **Requests.** load-data and fetch-latest go through `tk` and client-boot's request class (`OKwY` on `Umoo`: response type arraybuffer, 60 s timeout). edit-document is a plain `fetch` (`s3`), with `keepalive` when the page unloads.
- **Not needed by Clack.** `/-/run-document-fixers` and `/-/save-diagnostics-report` use the same target and auth.

**The bodies are binary protobuf, not pblite.** `request_binary` and `data_binary` are `p8gD.toBinaryString`: the standard protobuf wire format in standard base64 with `=` padding. `c7F4.serializeToWriter` writes fields in tag order, packs repeated numeric fields, and writes every field that was set, so an explicit 0 or an empty message is on the wire (proto2 presence). Answers are the same binary, not base64. Decoding drops unknown fields, so a client never echoes fields it doesn't know. pblite (Closure's JSON-array form, `p8gD.toPbLite`) shows up only in Slack's diagnostics (`syncer_load_data_request_pblite`) and in `call-handler`'s `request_pblite`, which canvases don't use.

The Quip session token that `quip.getControllerInitData` hands out (`7yDq.setSessionToken`) is only for blob URLs (`/collab-slack-blob/…`). None of the three calls sends it.

### load-data

| | |
|---|---|
| URL | `/canvas/-/load-data`, or `/canvas/-/load-data/<user_action>/<n>` while a user-action tracer is active (`NzH8.loadData` then also sets fields 5 and 6). Without a tracer Slack sends the bare path and logs a debug warning |
| form | `request_binary` = base64 `syncer.LoadData.Request`, `token` |
| answer | `syncer.LoadData.ResponseWrapper` |

To open a canvas, the syncer loads its thread (files.info `quip_thread_id`): the thread as an object id plus one bulk load of type THREAD (`srhK.reloadThreadBulkLoad`: `markAsThreadBulkLoad`, then `loadObject`). For thread `ABCdEfGhIjK` Slack's encoder writes `0a0b 4142…4b 1a0f 0801 120b 4142…4b`.

```proto
message syncer.LoadData.Request {
  repeated string object_ids = 1;          // [thread id]
  repeated string index_ids = 2;
  repeated BulkLoad bulk_loads = 3;        // [{type: THREAD, arg0: thread id}]
  repeated syncer.Parcel parcels = 4;
  string user_action = 5;
  int32 user_action_sequence = 6;
  // 7 fragment_collection_ids, 8 secret_paths, 9 authorize_pending_ids,
  // 10 cell_section_loads, 11 mobile, 13 object_id_and_sequences,
  // 14 context_thread_or_slack_file_ids
  message BulkLoad {
    enum type = 1;                         // INBOX 0, THREAD 1, FOLDER 8, THREAD_COPY 9, ...
    string arg0 = 2;
    string arg1 = 3;
    bool mobile = 4; bool minimal = 5; bool skip_sfdc_cdo_cache = 6;
    bool is_preload = 7; bool skip_indexed_db_cache = 8;
  }
}
message syncer.LoadData.ResponseWrapper {
  string error = 1;
  syncer.LoadData.Response response = 2;
  syncer.LoadData.PreserializedItems preserialized_items = 3;
}
message syncer.LoadData.Response {
  bool success = 1;
  syncer.Payload objects = 2;
  repeated Index indexes = 3;              // {string name = 1; repeated {string object_id = 1; string sort_value = 2} entries = 2;
                                           //  bool desc = 3; bool numeric = 4; int32 deleted_entry_count = 5; bool chunked = 6}
  syncer.LoadData.Request followup_request = 4;
  repeated syncer.Parcel parcels = 5;
  repeated Failure failures = 6;           // {enum reason = 1; Request request_subset = 2; repeated string organization_ids = 3}
                                           // reason: AUTH 0, TRANSIENT 1, NOT_FOUND 2, NOT_IMPLEMENTED 3,
                                           // AUTH_DISABLE_REQUEST_ACCESS 4, NOT_ON_ELEMENT_ALLOWLIST 5,
                                           // INTERNAL_ERROR 6, EXCEEDS_PRELOAD_ALLOWANCE 7, UNSUPPORTED_OBJECT_TYPE 8
  // 7 fragment_collections, 8 guest_listener_params
  bool from_cache = 9;
}
message syncer.LoadData.PreserializedItems {
  repeated Preserialized preserialized = 1; // {enum id_type = 1; repeated bytes value = 2}
  repeated bytes crdt_sessions = 2;         // each a syncer.CrdtSessionState
}
message syncer.Payload {                    // the parts a canvas uses
  repeated syncer.User users = 1;
  repeated syncer.Thread threads = 3;       // id 1, sequence 2, deleted 3, title 6, author_id 7, document_id 10, secret_path 106
  repeated syncer.Document documents = 6;   // id 1, sequence 2, deleted 3, thread_id 6, title 7, updated_usec 8,
                                            // shard_chars 42, max_section_sequence 48, title_rich_text_api 69
  repeated syncer.Section sections = 7;
  repeated syncer.SlackChannel slack_channels = 56;
  repeated syncer.SlackUser slack_users = 57;
  repeated syncer.SlackEmoji slack_emojis = 58;
  repeated syncer.SlackFile slack_files = 60;
  repeated syncer.SlackDocument slack_documents = 62;
  // and many more (the dump has them)
}
```

`handleLoadDataResponseBinary_` (`NzH8`) reads the answer:

- **No `response`.** The load fails with "Not found" (`g47m`).
- **`error` set.** The load fails with that string.
- **`preserialized_items` set.** `M455.updateResponseWithPreserialized` decodes each item: id type 2 values are `syncer.Section` bytes, appended to `response.objects.sections`, each given the `CrdtSessionState` from `crdt_sessions` with its `section_id`; id type 79 is spreadsheet cells. A decoder has to read sections from both places.
- **HTTP failures** (class `R` in `NzH8`). Status 0 is "offline", retryable after 1 s; a status under 500 isn't retried; 5xx is retryable after 30 s.

A canvas's answer, as far as the client reads it: the thread (with its `sequence` and `document_id`), the document (`id`, `sequence`, `max_section_sequence`, maybe `shard_chars`), its sections, and the index `Section/<document id>`, whose entries are `{object_id: section id, sort_value: position_path}`.

### Sections

A document is a flat list of `syncer.Section`s. Nesting is in fields, not in the message tree.

```proto
message syncer.Section {
  string id = 1;                 // "temp:C:" + 3 shard chars + 25 hex (see below)
  int64 sequence = 2;            // the server's version; 0 for a section the client just made
  bool deleted = 3;
  repeated int32 dirty = 4;      // field numbers; also seen in save answers
  repeated int32 pending = 5;
  string thread_id = 6;
  string document_id = 7;
  string position = 8;           // order key; new ones end in ":temp"
  enum type = 9;                 // proto.section.Section.Type
  enum style = 10;               // proto.section.Section.Style
  enum section_class = 11;       // SECTION_CLASS 0, CONTROL_CLASS 1
  proto.section.Section.Content content = 12;
  proto.section.Section.Parents parents = 13;
  string alt_rtml = 14;          // a control's text form (":smile:")
  proto.section.Section.Status status = 15;   // section locks (classic Quip)
  proto.section.Section.Attributes attributes = 16;
  string temp_id = 17;
  enum merged_state = 18;        // NETWORK 0, LOCAL 1, MERGED_FROM_LOCAL 2
  bool modified = 19;
  int64 local_sequence = 20;
  string position_path = 21;     // ancestors' positions and its own, joined with "-"
  bool transient = 22;
  string copy_thread_id = 23; string copy_secret_path = 24;
  enum content_origin = 25;      // USER_EDIT 0, NEW_DOCUMENT 1, TEMPLATE 2, COPY_DOCUMENT 3, PASTE 4, SLACK 5, ...
  int64 created_usec = 26; int64 updated_usec = 27; string remote_editor_id = 28;
  bool has_layout_parent = 29;   // a child of a callout/blockquote/table layout
  string author_id = 30; repeated string edited_by_user_ids = 31; int64 display_edited_usec = 32;
  string trace_id = 33;          // "<session>-<save sequence>"
  bool is_antimatter_section = 34;
  bool read_only = 35;
  bool is_programmatic_update = 36;
  syncer.CrdtSessionState crdt_session = 38;
  enum activity_stack_update = 39; // ADD_SECTION_ROW 0, REMOVE_SECTION_ROW 1, ADD_CHECKBOX_ROW 2, REMOVE_CHECKBOX_ROW 3
  // 102 root_id, 115 overlay_sequence, 200-215 table/formula fields, 300 source [default 2]
}
message proto.section.Section.Content {   // one of these is set
  ContentText text = 1;                   // {string text = 1 (RTML); string language = 2}
  ContentImage image = 2;
  ContentControlHighlight highlight = 7;  // comment anchors
  ContentList list = 17;                  // empty message: a list container
  ContentLayoutTable layout_table = 30;
  ContentControlSlackChannel slack_channel = 42; // {slack_channel_id = 1 ("sc:C…"), name = 2, is_private = 3}
  ContentHorizontalRule horizontal_rule = 43;    // {bool as_page_break = 1}
  ContentControlSlackUser slack_user = 44;       // {slack_user_id = 1 ("su:U…"), display_name = 2, creator_user_id = 3,
                                                 //  profile_url = 4, slack_team_id = 5, from_same_document_paste = 6}
  ContentControlSlackEmoji slack_emoji = 48;     // {emoji_name = 2, slack_emoji_id = 3 ("se:<name>/<team>"),
                                                 //  custom_emoji_url = 4, emoji_size = 5, non_animated_url = 6}
  ContentLinkUnfurlCard link_unfurl = 50;
  ContentControlSlackFile slack_file = 52;
  ContentTitle title = 58;                // {string text = 1 (RTML); ContentImage header_image = 2;
                                          //  string header_image_slack_file_upload_user_id = 3; repeated SlackEmojiInfo emoji_info = 4}
  ContentControlSlackDate slack_date = 62;      // {int64 utc_date_ms = 1; string date_format = 2}
  ContentLayoutCallout layout_callout = 63;
  ContentLayoutBlockquote layout_blockquote = 65; // {repeated string section_ids = 1}
  ContentControlSlackBroadcast slack_broadcast = 68; // {string range = 1} (@here, @channel)
  // 58 kinds in all
}
message proto.section.Section.Parents { repeated string ids = 1; string container_id = 2; enum container_style = 4; }
message proto.section.Section.Attributes {  // the ones text sections use
  int32 indentation = 1;               // list level
  bool checked = 2;
  string background_color = 3;
  bool vertical_margin = 4;            // [default = true]
  int32 list_position = 11;
  enum default_content = 13;           // NONE 0, HIDE_ON_FOCUS 1, SELECT_ON_FOCUS 2
  enum text_alignment = 26;
  bool is_new_document_default_content = 34;
  bool continue_previous_list_numbering = 47;
  int32 restart_list_numbering_at = 48;
  int32 paragraph_indentation = 51;
  bool is_customized_default_content = 57;
  // 54 fields in all (the dump has them)
}
```

The kinds a canvas's text blocks use, from `nTyX.pW` (the type each style belongs to) and the rich-text importer `30B3`:

| block | type | style | content |
|---|---|---|---|
| title | TITLE_TYPE 64 | TEXT_TITLE_STYLE 48 | `title` (RTML, at most 2000 code points: `trimToMaxLength_`) |
| paragraph | TEXT_TYPE 0 | TEXT_PLAIN_STYLE 0 | `text` |
| heading | TEXT_TYPE 0 | TEXT_H1_STYLE 1, H2 2, H3 3 (H4-H6 39-41) | `text` |
| code block | TEXT_TYPE 0 | TEXT_CODE_STYLE 4 | `text`, lines joined with `<br>`, `language` |
| quote paragraph | TEXT_TYPE 0 | TEXT_BLOCKQUOTE_STYLE 16 | `text` |
| list | LIST_TYPE 1 | LIST_BULLET_STYLE 5, NUMBERED 6, CHECKLIST 7 | `list` (empty) |
| list item | TEXT_TYPE 0 | TEXT_PLAIN_STYLE 0 | `text`; `parents {ids: [list], container_style: the list's style}`; `attributes {indentation, checked, vertical_margin: false}` |
| divider | HORIZONTAL_RULE_TYPE 16 | HORIZONTAL_RULE_STYLE 18 | `horizontal_rule` |
| blockquote layout | LAYOUT_BLOCKQUOTE_TYPE 71 | LAYOUT_BLOCKQUOTE_STYLE 50 | `layout_blockquote`; its children have `has_layout_parent` |
| callout | LAYOUT_CALLOUT_TYPE 69 | LAYOUT_CALLOUT_STYLE 49 | `layout_callout` |
| mention, emoji, @here, date | CONTROL_SLACK_USER_TYPE 50 (users and groups), CHANNEL 49, EMOJI 54, BROADCAST 73, DATE 68 | none | `slack_user`, `slack_channel`, `slack_emoji` (+ `alt_rtml` ":name:"), `slack_broadcast`, `slack_date`; `section_class` CONTROL_CLASS |

Tables, images, columns (LAYOUT_FLEXBOX 26/21), unfurls and files have their own types; the full enums are in the dump.

**Ids.** A new section's id is `temp:C:` + shard chars + 25 hex digits (`ZRIn.generateEntityId`; `s6aU.uuid` takes `crypto.randomUUID()` without dashes and cuts it to 25). The shard chars are the document's `shard_chars` (field 42) or the first three characters of the document id (`7DQG.shardChars`). The server keeps `temp:C:` ids as they are: they're "new section ids", not temp ids (`2U5W.isNewSectionId`, `isTempId`), and the export's `id` attributes are the same strings.

**Positions.** `position` is an order key over the alphabet A-Z then a-z (`0fa5`). `generate(prev, next)` returns a key strictly between its neighbours with `:temp` appended, which marks a key the server hasn't confirmed (`stripTemp`, `isTemp`); the stored key presumably drops it. It tries, in order: `prev` incremented (the last letter that isn't z goes up one, Z to a; each trailing z becomes B), `next` decremented, `prev + "aaaa"`, and `next` minus its final B plus "Aaaaaaa"; anything else is "aaa". Slack's own outputs:

| prev, next | key |
|---|---|
| none, none | `aaa:temp` |
| `aaa`, none | `aab:temp` |
| none, `aaa` | `aaZ:temp` |
| `aaa`, `aab` | `aaaaaaa:temp` |
| `aaz`, none | `abB:temp` |
| `aab`, `aac` | `aabaaaa:temp` |
| `zzz`, none | `zzzaaaa:temp` |
| `aaa`, `aaB` (out of order) | `aaa:temp`, with a warning |

`position_path` is the positions from the outermost container down to the section, joined with "-" (`EKJU.positionPath`). A control's path is its section's ancestors plus `generate(previous sibling's position, its section's position)` (`EKJU.positionPathForControl`); a control with no section is "zzzzzz-orphaned-m".

**Controls.** A mention, emoji, @here or date is a section of its own (CONTROL_CLASS), and the text that shows it holds `<control id="…"></control>`. `30B3` turns a rich_text element into one: `user`/`usergroup` into CONTROL_SLACK_USER_TYPE with `slack_user_id` `su:<id>` (`2U5W.syncerIdForSlackUser`), `channel` into `sc:<id>`, `emoji` into `slack_emoji_id` `se:<name>/<topLevelTeamId>` (and `emoji_name` when that isn't a valid emoji id) with `alt_rtml` `:<name>:`, `broadcast` into `slack_broadcast {range}`. Removing a mention from the text (`eju0.remove`) only changes its section's text; the control section isn't deleted there.

### RTML

A text or title section's content is RTML, an HTML subset. The grammar is `W77Y.parse`:

- **Tags.** `a` (`href`, `class`, `data-nolink="true"`, `data-slack-file-id`), `b`, `i`, `u`, `del`, `code`, `f` (font colour; its attributes carry it), `sub`, `sup`, `br`, `control` (`id` or `data-id`, empty), `annotation` (`id`; wraps the text a comment thread anchors to), `cl`/`cli` (cell lists). `p`, `spacer` and `input` are dropped with their contents; any other element is unwrapped.
- **Empty elements.** A formatting element with no text and no control inside is ignored.
- **Text.** `escapeText`: tab, CR and LF become spaces; other control characters (U+0000-U+001F, U+007F-U+009F) are removed; `&`, `<` and `>` are escaped. Quotes aren't. A line break is `<br>`. Code blocks write leading spaces and runs of two or more as `&nbsp;` (`32AJ.plainTextToCodeRtml`; the importer `30B3` turns every space into `&nbsp;`).
- **Links.** `isSafeUri`: http, https and Quip's own schemes (`gOda.kT`), plus ftp, mailto and tel. `javascript:` and `vbscript:` are never kept.

The canonical writer is the editor's `getCompactRtml({text, marks})`: it is how Slack writes RTML out of an Automerge document (`getRtmlWithMarks`), so it's what `mergeRtml` returns. `fce5.getTextAndMarks` turns RTML into text plus marks: the text keeps escaped characters, `<br>` and `<control id="…"></control>` as they are written, and each mark is `{name, value, start, end}` over that string. `getCompactRtml` applies the marks to a tree in descending `getMarkDensity` (annotation 3, `a` 2, `f` 1, the rest 0; `dHCX.sortBy` is stable, so ties keep their order), and a later mark nests inside an earlier one: annotations end up outermost, then links, then font colour, then b/i/u/del/code/sub/sup. `getAttributes` writes a link as `href="…"` with `"` as `&quot;`, then `data-nolink="true"` and `data-slack-file-id="…"` when set, and an annotation as `id="…"`. The importer (`30B3`) writes controls as `<control id=temp:C:…></control>`, unquoted; both forms parse the same.

### edit-document

| | |
|---|---|
| URL | `/canvas/-/edit-document` |
| form (`getSavePayloadAndOptions`) | `thread` (the thread id), `document` (the document id), `retry_count` (consecutive failed saves), `session`, `sequence`, `data_binary` (base64 `syncer.DocumentData`), `title` (when `extractTitleMetadata(true)` finds one, which is always with a title section), `activity_log=false` (only when every section is a programmatic update), `token` |
| answer | `syncer.EditDocumentResponseWrapper` |

```proto
message syncer.DocumentData {
  repeated syncer.Section sections = 1;    // the update bundle
  int32 omitted_deleted_entities = 2;
  bool partial = 3;
  string max_position_path = 4;
  // 5-12 miniapp, annotation and paste metadata
  repeated proto.section.SlackEmojiInfo title_slack_emoji_info = 13; // {team_id 1, display_name 2, unicode 3, display_url 4}
  proto.threads.RichTextApi title_rich_text_api = 14;
  string document_id = 15;
}
message syncer.EditDocumentResponseWrapper {
  bool changed = 1;
  syncer.DocumentData final_data = 2;      // the server's version of the saved sections
  syncer.ChangesData cell_data = 3;        // spreadsheets
  syncer.DocumentData import_sections = 4;
  syncer.MountPoints mount_points = 5;
  bool is_new_edit_session = 6;
}
message proto.threads.RichTextApi {        // rich_text as protobuf
  string type = 1;                         // "rich_text"
  repeated RichTextSection elements = 2;   // {string type = 1 ("rich_text_section"); repeated RichTextBlock elements = 2}
}
// RichTextBlock: type 1, text 2, channel_id 3, user_id 4,
//   style 5 {highlight 1, bold 2, italic 3, strike 4, code 5, underline 6},
//   name 6, url 7, skin_tone 8, ..., timestamp 17, ..., unicode 20, ...
```

**The counters.**

- **`session`** is `7yDq.sessionId()`: 12 hex digits made at load (`tAAR.s4` three times), replaced by `InitOptions.session_id` when controller init data has one (`agks`).
- **`sequence`** is the session's save counter (`7yDq.sessionSaveSequence`). It's module-wide, it starts at 0, and `i40t.save` bumps it right before each send (`nextSessionSaveSequence`), so the first save sends 1.
- **`trace_id`** on each section is `<session>-<save counter>`, read while the bundle is built, before the bump: the first save's sections say `…-0`.
- **A section's `sequence`** is the server sequence the client last saw for it (`ZRIn.toPb`: `setSequence(this.sequence_)`), 0 for a new one. The client never bumps it. `toServerSequence_` rounds a sequence down to a multiple of 1000 and 1-999 mark transient sections (`isNewlyCreatedTransient`), so saved sections presumably move in steps of 1000.
- **A section's `local_sequence`** counts the bundles that entity has gone into (`toUpdateBundle` starts with `localSequence_ += 1`).

**The update bundle.** `7DQG.createUpdateBundle` adds every entity that `shouldUpdate()` (dirty; under CRDTs, with saveable CRDT changes) as `toUpdateBundle()`, then the title's emoji and rich text. `ZRIn.toUpdateBundle` builds the full section with `toPb` and copies only these fields:

- the dirty fields (field numbers of `syncer.Section`);
- always: `id` 1, `sequence` 2, `local_sequence` 20, `modified` 19, `parents` 13, `crdt_session` 38, `activity_stack_update` 39;
- when anything is dirty: `is_programmatic_update` 36, and `type` 9 unless only `status` is dirty;
- `position_path` 21 when `position` is dirty;
- `content_origin` 25 for a new section whose origin isn't USER_EDIT;
- then `trace_id` 33.

A field on the list is copied only when `toPb` set it: `is_programmatic_update` only for programmatic changes, `crdt_session` only under CRDTs, `activity_stack_update` only when one applies.

`EKJU.toPb` always sets `parents`, even an empty one, and sets `style`, `position`, `attributes` and `has_layout_parent`; `ZRIn.toPb` sets `section_class`, `type`, `content`, `modified` (= dirty), `deleted`, `read_only`, `id`, `sequence`, `local_sequence`, `position_path`, `content_origin`. A new entity has every known field dirty (`setDirty(nTyX.mA)`: class, type, deleted, read_only, content, parents, style, status, attributes, has_layout_parent, overwrite_formula_user_id, position, alt_rtml). `thread_id` and `document_id` are never in a bundle; the form names them.

`activity_stack_update` (`WJQ2.shouldAddToActivityStack`, text sections only) is ADD_SECTION_ROW for a new section with text or a section whose stored text was empty, REMOVE_SECTION_ROW when the text is emptied, ADD_CHECKBOX_ROW and REMOVE_CHECKBOX_ROW when `checked` flips. It compares with the section as the server last sent it.

**The title.** With `updateDocumentTitle` (on for canvases), `extractTitleMetadata(true)` gives the title section's text, trimmed and cut to 255 characters (`MAX_TITLE_LENGTH`), for the `title` field; up to 10 of its emoji controls' `SlackEmojiInfo`; and `title_rich_text_api`, the section exported to HTML and converted to rich_text (`30B3.convertSectionsToRichTextApiPb`, options `maxLength 2000, flattenLists, inlineHeadings, useTextTypeForLinks, omitBreakNewlines, normalizeSpaces, escapeText`). A document with a title section sends all three with every save. Typing in the title also updates the header at once (`checkAndSendUpdatedTitle` → QuipDelegate `updateTitle`, a local store action).

An edited paragraph, encoded by Slack (an existing section at sequence 7000, first save of the session, with a minimal `title_rich_text_api`):

```
0a59                                       sections, 89 bytes
  0a21 "temp:C:ABC0123456789abcdef0123456"   id
  10 d836                                  sequence 7000
  48 00                                    type TEXT_TYPE, written although 0
  62 16 0a14 0a12 "Hello <b>world</b>"     content.text.text
  6a 00                                    parents, empty but written
  9801 01                                  modified
  a001 01                                  local_sequence 1
  8a02 0e "0123456789ab-0"                 trace_id
720b 0a09 "rich_text"                      title_rich_text_api.type
```

### One edit, as Slack saves it

| the user | the bundle |
|---|---|
| types in a paragraph | that section: content dirty (plus `activity_stack_update` when it goes from empty to text) |
| presses Enter at the end | a new section after it: sequence 0, every field, `position` from `generate(this, next)`, `position_path`; the old one too if its text changed |
| deletes a block | the section with `deleted: true` (and `type`) |
| ticks a checklist item | the item: `attributes` dirty (`checked`), ADD_CHECKBOX_ROW or REMOVE_CHECKBOX_ROW |
| makes a paragraph a heading | the section: `style` dirty (and `type` when it changes kind) |
| @-mentions someone | a new control section (CONTROL_CLASS, `slack_user`) and the text section whose RTML now has `<control id="…">` |
| renames the canvas | the title section (`content.title`), and the `title` field |

Every dirty entity of the document goes into one save. The background timer (`7DQG.backgroundProcess_`, `scheduleBackground`) saves 200 ms to 10 s after an edit; the defaults (`qfZ7`) are `backgroundMinDelay` 200 ms, `backgroundMaxDelay` 10 s, `backgroundSaveDelay` 3 s, `backgroundAttributeSaveDelay` 100 ms, `backgroundTitleSaveDelay` 200 ms, `backgroundNoViewersSaveDelay` 10 s, and the embed can override them. One save runs at a time: another waits, retrying every 200 ms (`i40t.shouldDeferSave_`). Editing a section also clears the "new edits" highlights and moves the canvas's read stamp to now (`EKJU` → `removeAllNewEditHighlights` → `updateFileReadTs`, QuipDelegate `updateFileReadTsBeforeSave`), and a successful save records `lastSaved` (`didSaveDoc`). Both are local store updates, not API calls.

### The answer

`handleEditResponseBinary` reads `changed`, `final_data`, `cell_data`, `import_sections`, `is_new_edit_session` and applies `mount_points`. On success (`i40t.successCallback_`), when `changed` is set, the client applies `final_data`'s sections as a save answer (`document.update(…, "POST response")`, flag 1), then `didApplyUpdatesForSave`: it maps `temp_id` to the real id when the server renamed a section, and updates the `Section/<document id>` index with `{id, remove: deleted, sort_value: position_path}`. `changed: false` is a success that changed nothing. `merged_state` MERGED_FROM_LOCAL is legal only in a save answer (the update pass logs "MERGED_FROM_LOCAL set outside of save response" anywhere else). The client renders such a section as a merge of its own change (`mergedFromLocal` in `EKJU.updateFromContent`) and holds it as a pending update when it still lists `dirty` fields or its `local_sequence` differs from the entity's.

### Concurrent edits

**Canvases merge typing with Automerge.** Slack turns CRDTs on for canvases unless the experiment `canvas_crdts_client_team` is "off" (boot-shared `11gR.isCrdtsClientEnabled`, `1E/J.isCrdtsClientEnabledForCanvas`; the collab init in `agks` then calls `f/mI.enableCrdts` and loads Automerge's wasm, `E2lw`). Each text section then carries an Automerge document of its RTML (the text and marks above) in `crdt_session`:

```proto
message syncer.CrdtSessionState {
  string section_id = 1; int64 base_sequence = 2; int64 last_saved_sequence = 3; int64 expiration_usec = 4;
  bytes automerge_doc = 5; repeated string processed_heads = 6; bool server_validation_failed = 7;
  string base_head = 8; MergeInfo merge_info = 9; repeated SplitInfo split_infos = 10;
  repeated SequenceHead sequence_heads = 11; string current_head = 12;
}
```

The client sends it with each save (`EKJU.toPb` → `crdt.createCrdtState`) and merges the sessions others save (`ZRIn.updateCrdtState_` → `crdt.update`). Two people typing in one paragraph both keep their text.

**Plain content is Slack's own fallback, and the only save Clack can make.** A text section whose Automerge document can't be read, that lacks live text, or that was saved without a session saves as plain content: `savesAsPlainContent` in the editor's CRDT class, and `EKJU.toPb` then leaves `crdt_session` out. So the server takes a text section update that has only `content.text`. When a save answer or a remote update carries a `crdt_session` without `base_sequence`, CRDT clients start a new session from its RTML (`startClientSession`) and merge their unsaved typing into it (`mergePendingSaveAnswer`). Clack has no Automerge, so every Clack save is this case.

**Slack's three-way merge.** `mergeRtml(base, ours, theirs)` (editor `0ealK`) builds an Automerge document from `base` as actor "00", applies `ours` as actor "02" and `theirs` as "01", merges, and writes RTML with `getCompactRtml`. `withRtml` diffs by words first (`71JW.spliceWords`), then by characters inside a replaced run. Every insert from both sides survives and every delete applies. Two inserts at the same spot are ordered by Automerge by actor id; which comes first is best taken from running Slack's `mergeRtml` itself. `crdt.mergeIntoSaveAnswer` uses it to keep typing that happened while a save was in flight. Automerge's wasm is public too (`automerge_wasm_bg-ea55425.wasm`, `092942ae1cb66d62.module.wasm`).

**Without CRDTs, sections lock.** In classic Quip, a section someone is typing in is locked for 60 s (`qFI8`, `DEFAULT_LOCK_LEASE`), renewed while they type, and sent around as transient sections; CRDT sections ignore locks marked `from_crdt`.

**Remote updates while a section has unsaved changes.** The document's update pass (`7DQG.update`) holds a remote update to an entity with unsaved changes (under CRDTs, only while its save is in flight) and replays it after the save. It drops any update whose `sequence` is lower than the entity's. So in plain content, a held update older than this client's save answer is dropped when it replays, and the save answer decides what the section says.

**There is no conflict state.** The client sends each section with the last server `sequence` it saw and never shows a conflict. HTTP 409 is SEQUENCE_OUT_OF_ORDER, which it counts like other failures (with a threshold of 9 instead of 2) and retries. What the server does with a stale section `sequence` under plain content (merge with the base version, keep the newest, or refuse) isn't visible from the client.

### Failures and what the user sees

`i40t.getSaveErrorInfo_` sorts a failed save by HTTP status:

| status | error type | failures before the error state |
|---|---|---|
| 400 | OPERATION_NOT_PERMITTED | 2 |
| 403 | FORBIDDEN | none: it isn't counted; `setError(1, 7, 15000)` shows the error at once (reason "Thread closed") without locking editing |
| 409 | SEQUENCE_OUT_OF_ORDER | 9 |
| 413 | PAYLOAD_TOO_LARGE | 2 |
| 429 | TOO_MANY_REQUESTS | 5 |
| 503 | SERVICE_UNAVAILABLE | 2, and the save counts as stuck |
| anything else, and a network failure | UNKNOWN | 2 |

A network failure never reaches the server's status: `s3`'s `onFetchFailure` hands the error callback a made-up `new Response(null, {statusText: <message>})`, which falls into UNKNOWN. After any failure the saved sections go from pending back to dirty (`finishSave_` → `revertFromPendingToDirty`), so nothing is lost, and the next save waits `getSaveBackoffDelay`: 10 s (`backgroundMaxDelay`) for the first five consecutive failures, then 10 s × 1.5^(n-5), at most 10 min. `retry_count` tells the server how many failed in a row. A success clears the count. Two more things start the error state: more than 25 saves attempted since the last one that succeeded (`saveIdDifferenceBeyondThreshold`), and a save still in flight after 150 deferrals, about 30 s (`setSaveStuckInProgress`). The reason the error carries follows the cause: offline when `navigator.onLine` says so ("Connection offline"), else "Server not reachable"; "Document too large" for 413; "Unknown error" for a save that threw.

In the error state the document calls `updateErrorState` → `setError(1, …)`, and because Slack's platform delegate says `disableEditingDuringErrorState()` (true in `agks`), the canvas turns read-only ("cant_sync") until a save succeeds. Typing then shows nothing more (`indicateEditsDisabled` is a no-op in every delegate here). The editor's `setError` (editor `hs.setError`) hands Slack's `errorUiProvider` (QuipEmbed, boot-shared `LlXB`) a reason: "Document failed to load", "Unknown error", "Early exit due to previous failures", "Connection offline", "Server not reachable", "Document too large", "Session expired" or "Thread closed". Slack logs `[QuipSaveError] <reason>` and shows a banner: "Document too large" gives `quipDocumentTooLarge`, any other reason `quipOffline`. Clearing the error removes it.

The banners are `QuipSystemMessage` (`p-quip_system_messages p-quip_system_messages--<type>`, Slack's warning banner, `isFixedPosition: false`), above the canvas:

| banner | when | icon | text | close button |
|---|---|---|---|---|
| quipOffline | a save error state, a load that failed | cloud-offline-filled | "We’re experiencing issues saving edits to this canvas." | no |
| quipDocumentTooLarge | 413 twice | warning-filled | "This canvas reached its size limit and can’t save edits. Copy any unsaved changes before closing it." | no |
| offline | Slack's own connection is down (`quipSystemMessages.isOffline`) | cloud-offline-filled | "Slack is trying to connect. Edits cannot be saved right now." | no |
| degraded | Slack's connection is degraded | cloud-offline-filled | "Your connection to Slack is unstable. Your edits will be saved, but it might take a while to show changes made by others." | yes |
| unresolved | edits unsaved for over 2 min (`useEmbedDirtyWarning` polls `getIsDirty` every 60 s), or an unresolved document id | cloud-offline-filled | "We’re experiencing issues creating a new canvas." | no |
| static | `files.open` said `should_subscribe_and_ping: false` | none | "This canvas is popular! Unfortunately, you can’t edit this canvas right now, and it might take a while to show changes made by others." for a writer (`vITP.isWriteable`), else "This canvas is popular! Unfortunately, it might take a while to show changes made by others." | yes |

QuipEmbed's own message (a save error, else unresolved) is shown first; otherwise the first of the store's messages in `ZEA4`'s order: offline, quipDocumentTooLarge, quipOffline, static, degraded, then the rest. On unload Slack saves once more with `keepalive` (only when the form is under 64 KiB, `zRL+.Su`). A failed fetch-latest is only logged ("fetch-latest failed"), with no retry and no UI; the next `document_update` fetches again.

### Following other people's edits

- **`document_update {file_id, payload}`** (boot-render `documentUpdated`): Slack looks up the Quip thread and calls `onReceiveDocumentEvent(thread, payload, "update")` (`/zCZ`), which runs `fetchThreadChanges`. The payload isn't read.
- **`document_change {channel: <file id>, payload: base64}`**: a `syncer.TransientSections` (cursors, live typing, locks), passed to `receiveTransientSections`. A client sends its own as `{type: "document_change", user, channel, payload}` (`/zCZ.sendDocumentEvent`).
- **Opening** (boot-shared `3uCk`, the `openFile` thunk): `files.open`, then `onReceiveDocumentEvent(thread, "", "backlog")`, the same catch-up fetch. If `should_subscribe_and_ping` is false (and `XLew.s` doesn't override it), it puts the canvas in static mode (`o4o5.setReadOnly`, store key `canvasReadOnly`): the `static` banner above.

`HVRg.fetchLatest_` builds the fetch:

| | |
|---|---|
| URL | `/canvas/-/fetch-latest` |
| form | `request_binary` = base64 `syncer.FetchLatest.Request`, `token` |
| answer | `syncer.FetchLatest.Response` |

```proto
message syncer.FetchLatest.Request { repeated ThreadRequest requests = 1; }
message syncer.FetchLatest.ThreadRequest {
  string thread_id = 1;
  int64 client_thread_sequence = 2;       // the thread's sequence (minus 1 to force)
  int64 client_max_section_sequence = 3;  // the document's max_section_sequence
}
message syncer.FetchLatest.Response {
  syncer.ChangesData changes_data = 1;
  int32 section_consistency_hash = 2;
}
message syncer.ChangesData {
  repeated string ids = 1;
  repeated Index indexes = 2;             // {index_id 1, in_place 2, ids 3,
                                          //  changed_entries 4 {id 1, remove 2, sort_value 3}, reload 5}
  repeated TempId temp_ids = 3;           // {temp_id 1, real_id 2}
  repeated TempId index_temp_ids = 4;
  repeated string presence_ids = 5;
  bool unread_count_changed = 6;
  syncer.Payload data = 7;                // the changed threads, documents, sections
  // 10 fragment_collections, 11 leveldb_write_error, 12 cell_sections
}
```

For thread `ABCdEfGhIjK` at sequence 42000 with sections up to 41000, Slack writes `0a15 0a0b 4142…4b 1090c802 18a8c002`. The answer goes to `onChangesReceived_` → `updateWithChanges`. When `data.backlog_reload_model_data` is set, the thread is reloaded with load-data instead.

### What a server answers

What the client code expects of Slack's server, which is what fakeslack has to do:

- **load-data.** A `ResponseWrapper` with `response.success`, the thread, the document and every section in `objects`, the `Section/<document id>` index, and HTTP 200. A thread the token can't read: `failures` (AUTH or NOT_FOUND), or no `response`.
- **fetch-latest.** `changes_data.data`: the thread and document with their new sequences, and the sections whose `sequence` is above `client_max_section_sequence` (deleted ones with `deleted: true`); `indexes` with `Section/<document id>` changes. Nothing changed: an empty `ChangesData`.
- **edit-document.** It applies each section's fields, gives every changed section a new sequence (above the document's `max_section_sequence`, which moves with it; presumably a multiple of 1000), drops `:temp` from new positions, and answers `changed: true` with those sections in `final_data` (full sections, with `thread_id` and `document_id`), and `is_new_edit_session` when the save starts a new editing session on the server's side (the client then logs `edit_document`; the server's rule for that is unknown). The `title` field names the thread; files.info's `title` is derived from the document (see "Title" above), so the two agree. Then it sends `document_update` to everyone with the canvas open. Errors are bare HTTP statuses (the table above); the client reads no error body. A session `sequence` that doesn't go up is, by its name, SEQUENCE_OUT_OF_ORDER (409).
- **Unknown.** The real server's choices for a stale section `sequence`, a control that no text references any more, and section ids it doesn't know (see the open questions in docs/CANVAS-EDIT-PLAN.md).

## What Clack does

Clack reads the HTML export and, for now, edits through Slack's documented `canvases.*` methods. That edit path doesn't work for any real account (see "Edit" below); the replacement speaks Slack's edit protocol above and is planned in [CANVAS-EDIT-PLAN.md](CANVAS-EDIT-PLAN.md).

**Quip's model (round 2, B1).** `internal/quip` speaks the protocol above: the messages and Slack's encoder and decoder, RTML parsing and writing, order keys and ids, each held to Slack's own modules by the `quip-codec` parity snapshots. `core.LoadCanvasModel` loads a canvas's document with load-data (`slack.Client.CanvasCall`), and core keeps it current as Slack's syncer does: fetch-latest on every `document_update` and after files.open, a reload when the answer asks for one. Nothing calls it on a real account yet: the view still draws from the export, and saves move to edit-document in B2, behind a switch that is off by default. fakeslack serves the three calls strictly (`internal/fakeslack/quip_endpoints.go`): every request has to be what Slack's client sends, or the fake refuses it.

**Saves through edit-document (round 2, B2).** With the switch on (ui.json's `canvas_quip_edit`, or `CLACK_CANVAS_QUIP_EDIT=1`; off by default), the canvas view saves through `core.SaveCanvas` instead of `canvases.edit`:
- **What it saves.** Typed plain text in an existing paragraph, heading, list item or the title (a rename), a ticked or unticked box (the `checked` attribute alone, whatever the item's text holds), and a top-level paragraph emptied (its section deleted). A pending edit of a section takes the next one in, as Slack's dirty fields do: typing and a tick in the same item before a save are one bundle section with the text and the box. An edit of a section whose save is out joins it too, and is what's left of the section once that save succeeds. Each is one bundle section with exactly the fields `ZRIn.toUpdateBundle` gives that edit (`quip.UpdateSection`; the `quip-save-section` snapshot holds it byte for byte to Slack's own entity), in the model's insertion order, with the session's trace id and each section's local sequence; the form is `getSavePayloadAndOptions`' (`thread`, `document`, `retry_count`, `session`, `sequence`, `data_binary`, `title`), and the title metadata is `extractTitleMetadata(true)`'s (`quip-save-title`).
- **Before it goes out.** `quip.Doc.Validate` checks every save on its own, against the model and the edits: one section per edit, the exact fields, the model's sequence, type and parents, the trace and local sequence, the activity row Slack would add, RTML Slack's save writer writes (it parses, only `a`, `annotation`, `b`, `i`, `u`, `del` and `code`, safe links, no raw control characters, every control in the model), the section's mentions, emoji, dates and comment anchors kept as they were, valid UTF-8, a byte-for-byte round trip and the title metadata the model gives. Anything else is refused, and nothing is sent. It also refuses an empty mark: the editor's `getCompactRtml` (the merge path) loses the text after one. The only empty mark Slack's own writer keeps is a comment anchor whose text is all gone (`ab<annotation id="z"></annotation>cd` comes out of `CITi.DI` as it went in; an empty `<b>` or `<a>` is dropped), so a section holding one is the one case where Clack refuses a save Slack's client would make.
- **Failures, as Slack's.** A failed save stays pending and goes again after `getSaveBackoffDelay` (10 s for up to five failures in a row, then ×1.5 up to 10 minutes), with `retry_count` the failures in a row, the model caught up first (fetch-latest). A failed catch-up fails the attempt, which goes again after the backoff: nothing is sent on stale state, as how the server takes a stale section sequence isn't known. The model's load is part of the attempt too: an edit made while it can't be loaded (offline, a failed or oversized answer) waits for it, counted and retried as a failed save is, and is built against the model once a load succeeds. `getSaveErrorInfo_` sorts the failures: two in a row of most kinds (nine 409s, five 429s), more than 25 saves without a success, or a save stuck 30 s is the error state. Then QuipEmbed's banner shows over the canvas ("We’re experiencing issues saving edits to this canvas.", or the size-limit one after 413s) and editing is locked (`cant_sync`) until a save succeeds, or until nothing is left to save. A block that was open keeps its text, unsent: Escape says the canvas can't be saved to right now and leaves the block open, and saves it once the lock lifts. Slack keeps text typed before `cant_sync` and saves it later, and has no way to throw it away. A conflict or refusal found by an attempt nobody waited on (a retry, or one that waited behind another save) is a notice the view shows as it shows a direct one: the toast, then a reload. A 403 shows the banner for 15 s without counting. Edits unsaved for two minutes show "We’re experiencing issues creating a new canvas." (`useEmbedDirtyWarning`, polled each minute). Closing the canvas makes one last attempt.
- **Not yet (B3).** New blocks and inserts, text with formatting, links, mentions, emoji, dates or line breaks, a block's style, level or box changed by typing, a list item emptied, Quip's default content typed over, a quote's paragraph, mention removal, and a title with a cover or anything but plain text. These keep the can't-edit toast, and the editor stays open with what was typed. B3 saves most of them now (below).

**Rich text, new blocks, styles and levels (round 2, B3).** With the switch on, more of what Slack's editor edits saves through edit-document:
- **A block's own text** with formatting (bold, italic, underline, strike, code), links and line breaks, and its mentions and emoji (`core.SaveCanvasSpans`). `quip.ApplyRuns` writes the editor's runs onto the section's RTML the way `getCompactRtml` writes text and marks. Each mention or emoji stays the section's own control, wherever the editor puts it. Taking one out leaves the text, as Slack's editor does (`eju0.remove`). A canvas's editor is pre-wrap (its config's `improvedWordWrapping` puts `white-space: pre-wrap` on the document), so the browser keeps typed spaces as they are, and they're saved so. A character the edit kept is written as `ParseRTML` gives it, which is `escapeText` of the DOM's character, the form Slack's save writer gives: an RTML's `&nbsp;` or `&quot;` comes out as the character. Zero-width spaces and byte order marks go, as the writer drops them, and no mark is left over them alone. Comment anchors, and a link's `nolink` and `slackFileId`, move with their characters, and a mark whose characters are all gone goes. Every RTML Clack writes is a fixed point of Slack's save writer (`CITi.DI`, the `quip-apply-runs` snapshot).
- **Code blocks** (`SaveCanvasCode`). A code block's text saves line by line through the same writer, `<br>` between the lines, a tab as a space (as DI writes it), its language kept.
- **A quote's paragraph**, a section of style 16, saves its own text as a text edit: its style and parents stay, so it stays in the quote. Restyling it, or Enter in it, comes later.
- **Deletes.** A block emptied is deleted as `deleteSection` deletes it. A list item goes alone while its list keeps items; the last one takes its list with it (`notifyDescendantDeleted_`), unless a new item for that list is waiting to be saved. Not deleted yet: a block between two neighbours `maybeMergeAdjacentSections` would merge (two lists of a style, two code blocks or quote paragraphs, two quote layouts), a block in a layout (the layout's `onChildRemoved` changes it too), and a block holding mentions, emoji or dates. The canvas's last block isn't deleted but emptied, its text deleted, as Slack's editor leaves it: `deleteEntity` would make a new section in place of a section with no sibling. A canvas hides its title (mode `canvas` sets `skipUntitledOnNewDoc`, so `omitBuiltInHeadingOrTitleSection`, and `isHiddenTitle` makes the title unreachable), so the title is no sibling (`Doc.TopLevel`).
- **List depth.** The canvas editor's config sets `maxListIndentationLevel` 4: Tab stops at level 4 and a paste is capped to it (`capEntityIndentation_`), so a block's editor takes `composer.Editor.MaxIndent` 4, and core and the validator refuse a deeper level (`quip.MaxListLevel`).
- **New blocks.** Enter in a block, or typing under the last one (`SaveCanvasSplit`, `SaveCanvasNewBlocks`), makes the sections Slack's `K2FO.createNewSection` does. Each is a fresh entity at sequence 0 with every field dirty, its position `generatePositionAfter` gives (a `0fa5` key between it and the next entity), `vertical_margin` false, and what `initSibling` copies from the block it follows: its parents, indentation and paragraph indentation. The canvas editor suppresses text alignment, so no alignment is copied. The bundle holds Slack's field set byte for byte (`quip-save-new-section`). A new block's id is made when the editor closes, so the next edit of it reaches its section. An edit made while its save is out joins it, and the real id the server gives later is followed (`temp_id`, `Doc.RealID`). A new block has to keep the list, and the level, of the block it follows; a new list, leaving a list, and a mention or emoji in a new block come later.
- **Styles, levels and boxes** (`SaveCanvasBlock`). Between a paragraph and the headings, `setSectionStyle` marks STYLE alone, with no content when the text didn't change. A list item's level and a checklist item's box go through `setSectionAttribute`, which marks STATUS and ATTRIBUTES. A list made another kind, or a block moved into or out of a list, comes later.

**Read.** `files.info`, then the file's `url_private`, which answers with Quip's HTML export of the document (`text/html`, starting with `<div class="quip-canvas-content">`). `slack.Client.Download` refuses every HTML answer, because a file URL answering with a page is usually a redirect to sign-in. So `slack.DownloadCanvasHTML` is the one exception, and it is narrow: the URL and every hop that keeps the cookie must be a Slack file path (`/files-pri/…`, `/files-tmb/…`) on an https slack.com host (`slack.FileURL`); a hop to Slack's other hosts loses the cookie, and the final URL's path must still be a file path; the type must be `text/html`/`text/plain`, the body must contain `quip-canvas-content`, and the size is capped at 16 MiB. The export is never cached and never written to disk. Embedded images and files are resolved with `files.info` on their ids (at most 24 per canvas, 4 at a time); Clack never fetches Quip's relative blob URLs.

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
| `control > a` "#C…" / "@U…" / "@S…" / "@here" | mentions. The export writes the id the control holds, never the name and no link (a shape probe of seven Hack Club canvases, 2026-11-18: every mention had this form). Quip's control patterns: `#([CDG][A-Z0-9]{8,})` channels, `@([UW][A-Z0-9]{8,})` people; a group is a user control with an S id. Older exports' "@name" / "#channel" are matched by name (`ResolveCanvasMention`) |
| `control > img[alt=name, src=slack-imgs.com proxy]` | emoji (Clack uses the workspace's own image) |
| `control` with text | a date |
| `.default-content` (without `customized-default-content`) | Quip's default content the user never edited (`Block.Default`). Slack draws it transparent (`.default-content:not(.customized-default-content)>.content{opacity:0}`): the line keeps its height, no text |

The `id` attributes (`temp:C:…`) are Quip section ids. They are the same ids `canvases.sections.lookup` returns, `canvases.edit` takes and load-data's sections carry. The export has no control ids (a mention is `<control><a>@U…</a></control>`), so an edit that keeps a mention needs load-data's RTML, where it is `<control id="temp:C:…"></control>`.

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

**This fails on every real account.** Slack answers `canvases.edit` with `not_allowed_token_type` for session (xoxc) tokens, on the workspace host and on the enterprise host alike (seen 2026-11-17 with this account's token), and Clack only ever has session tokens. fakeslack accepts the call, so no test noticed. So editing a block, ticking a checkbox, renaming and Remove mention all fail with a toast. Slack's own client never calls `canvases.edit`, `canvases.create`, `conversations.canvases.create` or `canvases.access.set` (none of them is in build 9685e6ce144b's boot, Quip or share-dialog chunks): it edits through edit-document, creates canvases with `files.createCanvas`, and shares with `files.updatePermission` / `files.revokePermission`. Create and share weren't tried with a session token, so whether they fail the same way is open. [CANVAS-EDIT-PLAN.md](CANVAS-EDIT-PLAN.md) replaces the edit path.

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
- *Mentions:* the export gives each mention its id (`#C…`, `@U…`, `@S…`), and the view names it from the store, as Slack's canvas draws every mention control from its model (`core.NameCanvasMention`; Quip's channel view hI and user view hu in `quip-collab-editor`): "#name" for a channel the viewer can see, member or not; the lock and the name for a private one they're in; the dimmed lock and "Private channel" (not a link) for one neither flannel's `channels/info` nor `conversations.genericInfo` knows; "@name" for a person, "@handle" for a group, "@unknown group" for a group that doesn't exist. While a lookup is out the mention is Quip's `slack-control-missing-content` skeleton (a 12px square and a 120 by 8 bar in `--quipBackgroundInverse` at .15), never the id. The store's reads ask for what they miss (Slack's quip delegate: `fetchAndFormatChannel`, then `ensureChannelsArePresent` → `fetchAndUpsertChannelsById`), and the block lays out again in the first frame after the answer lands (`mentionsMoved`). The table of contents and the editor read the same named spans. An older export's names resolve from the user and channel caches (`ResolveCanvasMention`): users by display name, then username, then real name, case-insensitively; channels by name. A name is accepted only when exactly one user or channel has it at the first tier that matches, so an ambiguous name stays unresolved.
- *Fidelity check:* a block opens in the editor only if `canvas.Editable` passes. Its kind must be one canvas markdown writes, and its markdown must parse back to the same inlines: text and links by text, style and target; mentions and emoji by id.
- *What fails the check:* underline and highlight (not in canvases.edit's supported list), dates, inline images, @here/@channel/@everyone and user groups (no documented form), unresolved mentions, a canvas, file or message mention (Quip's control around a link: the span keeps `Control`, since markdown would write it as a plain link), hard line breaks inside a block (supported, but the syntax isn't documented), a link styled as code, and a code block that contains a fence.
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
- **Comment wording:** a message in a canvas's file channel is a canvas comment (isCanvasComment, `C3jk`/`z1wc`). Its share action says "Share comment…" ("Forward comment…" under private_message_forwarding; `5t6x`), and deleting it asks "Delete comment" / "Are you sure you want to delete this comment? This cannot be undone." (`6Qou`).

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

**Where the channel card sits** (HovercardImpl and BubbleImpl in `quip-collab-webapp`; `hovercard/hovercard.go`'s package comment). The bubble's anchor is the mention's element, measured again by the window's positioner registry after every DOM change, scroll, resize, transition end and font load. So the card goes with its mention when a collaborator's edit moves it, whether the pointer is on the mention or resting on the card. It is dismissed only when the element leaves the document, or measures 0×0 for 30 tries. A mention that moves or scrolls out of the canvas's view keeps its card, placed by the mention and shifted into the window only when neither side fits. Once the card has slid out from under a resting pointer, the bubble's mouseleave starts the 250ms dismiss. A wheel gesture that begins on the card scrolls nothing, because the bubble is in the document's body, outside the canvas's scroller. One that began on the canvas keeps scrolling it when the card slides under the pointer, because Chromium latches a wheel gesture, momentum included, to the scroller it began on until 500ms pass without an event (MouseWheelPhaseHandler). Clack reads the mention's window position from its layout (Gio patches 53 and 57: the area's bounds as the last frame put it, and a frame when a frame moved it), not from pointer events, less the frame's own origin (Gio's title bar on Wayland without server-side decorations). Clack lays out only the rows it shows, so a mention whose row scrolls out entirely unmounts and closes its card at once.

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

**Title emoji** (Karim: the channel's canvas tab read "clack testing :cat-slackbot:"). Slack renders a canvas title from its title_blocks with the emoji store on every render (QuipTitleParsedId, GF/w: the tab label, the canvas header, Activity's destination at caption size, 16px emoji). Clack converted title_blocks once, when the tab bar or the canvas was read. An emoji the map didn't have yet (a warm start's map still loading, an emoji newer than the cached map) became its ":name:" text for good, and the header cache kept it across sessions. Now `richtext.TitleBlocks` keeps such an emoji as an Emoji span with no image. The tab bar builds itself again from the same reads when the emoji map's version changes, so the emoji draws and a leading one becomes the tab's icon (ui backend `tabs`). The canvas header resolves it when the map changes (canvasview `nameMentions`), Activity's destination and search's canvas results resolve it as they draw, and a search result names a canvas by its title_blocks (`core.File.TitleBlocks`; 22px emoji beside its 15px bold title, `p-quip_title__blocks__base`). Only the emoji TitleBlocks kept resolve late (`Span.KeptEmoji`: no image, its Text ":name:"); a hidden emoji (Unicode 17 with emoji_v17 off, which richEmoji leaves as its characters) stays text even when a custom emoji has its name.

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
