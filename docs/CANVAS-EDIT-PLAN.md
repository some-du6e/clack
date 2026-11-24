# Canvas editing over Slack's edit protocol: the plan

Clack saves canvas edits with the public `canvases.edit`, which Slack refuses for session tokens (`not_allowed_token_type`), so every save fails on a real account. Slack's own web client saves through Quip's protocol: `/canvas/-/load-data`, `fetch-latest` and `edit-document` with binary protobuf bodies. [CANVASES.md](CANVASES.md) "Slack's edit protocol" describes that protocol from Slack's code (build 9685e6ce144b). This file is the plan for building it (round 2), after a look at the earlier attempt.

Nobody can test against a live account and a wrong save can damage a real canvas, so the plan leans on two things: Slack's own code as the oracle for every byte Clack writes, and a fakeslack that speaks the protocol strictly.

## The earlier attempt, canvases-2026-11-07

Remote branch `canvases-2026-11-07` (4e510d1a, `internal/core/canvas_quip.go`) spoke the protocol from a recording of the real client. Its notes say "shapes only". Compared with Slack's code:

**What it got right.**
- The three endpoints on the top-level host (enterprise host and token when there is one), `token` in the form and the `d` cookie.
- Base64 binary protobuf in `request_binary` and `data_binary`. It never was pblite.
- The load request: `{1: thread, 3: {1: 1, 2: thread}, 5: "editor", 6: 1}` on `/load-data/editor/1` is `object_ids`, a THREAD bulk load, and a user action with its sequence (`NzH8.loadData`).
- The answer path `{2: {2: {3, 6, 7}}}`: `ResponseWrapper.response.objects` with threads, documents and sections.
- Section fields 1 (id), 7 (document id), 8 (position), 10 (style), 12 (content), 21 (position path), content `{1: {1: rtml}}` for text and `{58: {1: …}}` for the title, field 13 as the list parent (`parents.ids[0]` and `container_style` 4), 16.1 as the list level, 29 for children of a layout.
- The edit form: `thread`, `document`, `session` (12 hex digits), `sequence`, `retry_count`, `title`, `data_binary` = `DocumentData{sections}`, and `trace_id` 33 = `<session>-<n>` with n one below the form's `sequence`, which is exactly `ZRIn.toUpdateBundle` and `i40t.save`.
- The answer: `changed` 1, `final_data` 2 with the stored sections.
- New ids: `temp:C:` + 3 characters + 25 hex digits, and attributes `{4: 0}` (vertical_margin false) on a new text section.
- RTML as escaped text with inline tags and `<control id>` placeholders, and editing it by splicing the changed middle into the old token stream (`rtmlEdit`), which keeps controls and comment anchors outside the edit. That idea survives in this plan as Slack's text-and-marks model.

**What it got wrong.**
- **The section sequence.** It sent `version + 1000`. Slack sends the last server sequence it saw, unchanged (`ZRIn.toPb`). Server sequences presumably go up 1000 per saved change, so a recording of a second save after a load would show load + 1000, which may be where the rule came from; the branch reloaded before every save and so sent a sequence the server hadn't issued yet.
- **Missing fields.** Slack always sends `parents` (even empty) and, whenever anything is dirty, `type`. Its new sections carry every known field: `section_class`, `type`, `deleted`, `read_only`, `content`, `parents`, `style`, `attributes`, `position`, `position_path`. The branch sent `local_sequence` as the session counter; it is a per-section count. It never sent `activity_stack_update`, `title_rich_text_api` or `title_slack_emoji_info`.
- **Wrong fields.** It put `thread_id` (6) and `document_id` (7) in new sections; Slack never does. It called field 9 the class; it is `type` (`section_class` is 11). The numbers it used (0, 1, 64, 71) are the right type values.
- **Positions.** It made order keys from a-z with its own midpoint rule and no `:temp` suffix. Slack's keys use A-Z then a-z and `0fa5.generate`, and new ones end in `:temp`. Shard characters come from the document (field 42, else the document id), not the thread.
- **Preserialized sections.** It read sections only from `objects.sections`; Slack also takes them from `preserialized_items`.
- **Headers.** It sent `Origin: https://app.slack.com` and a Chrome User-Agent. Clack sends neither any more ([ua.go](../internal/slack/ua.go), docs/SIGNOUT-AUDIT.md): a fake browser identity is what signed this account out everywhere.
- **Concurrency and errors.** It reloaded the whole document before each edit (a 2 s cache), had no fetch-latest, and overwrote anything another person changed between its load and its save. It turned any non-200 into an error string, with none of Slack's retry, backoff or banners.
- **RTML details.** Tabs and control characters went through unchanged (Slack turns tabs into spaces and drops control characters), and code blocks didn't get `&nbsp;` for runs of spaces.
- **Tests.** Only the HTML parser and a protobuf round trip were tested. Nothing ever saved against a server, fake or real, after the recording.

## Rules for round 2

- **Never touch a real canvas.** No agent runs load-data, fetch-latest, edit-document, `files.open` or `canvases.*` against real Slack, or opens a canvas in a browser. All tests use fakeslack. The first live save is Karim's, on a canvas they make for it (see "Karim's live check").
- **Slack's code is the oracle.** Every encoder, the position keys, the RTML writer and the merge get golden values from Slack's own modules run in node (the `tools/quip` scripts below), not from Clack's idea of them.
- **Plain content only.** Clack has no Automerge, so it saves text the way Slack does when its CRDT copy is unusable: `content.text`, no `crdt_session` (CANVASES.md "Concurrent edits"). It never writes a `crdt_session`, a lock (`status`), a transient section or `document_change`.
- **Refuse rather than guess.** A save that doesn't fit what Slack's client would send is refused before it leaves Clack (see "Validation").
- **Honest headers.** The canvas calls go through `slack.Client`'s transport: Clack's User-Agent, no Origin or Referer.
- **A gate.** The new path ships behind a switch. It was off by default through alpha.163 and is on from alpha.164 (Karim: "test in prod bud"). `ui.json`'s `"canvas_quip_edit": false` or `CLACK_CANVAS_QUIP_EDIT=0` turns it off, and `CLACK_CANVAS_QUIP_EDIT=1` turns it on; the environment wins over `ui.json`. While it's off, editing a real account's canvas can only fail, so the plan makes the canvas view-only then instead of offering edits that error. Both choices are Karim's.

## Code layout

| where | what |
|---|---|
| `internal/quip/wire.go` | protobuf wire reader and writer over the messages' `pb:"N"` tags (reflection): varints, fixed32/64, length-delimited; packed and unpacked repeated scalars on read, packed on write; size and depth caps; Slack's decoder's ways (a message seen twice is replaced, a number with wire type 2 is read packed, field 0 is skipped) |
| `internal/quip/messages.go`, `enums.go` | the messages below and their enums; a `pb:"extra"` field keeps the records of fields Slack's schema has and Clack doesn't name (its `slack:"…"` tag lists them), dropping the rest as Slack's decoder does, and writes them back in field order |
| `internal/quip/rtml.go` | RTML parse (`W77Y.parse`'s grammar) into text and marks (`fce5.getTextAndMarks`), the writer (`getCompactRtml`), `escapeText`, `isSafeUri`, and conversion to and from `richtext.Span`s |
| `internal/quip/position.go` | `0fa5`'s `generate`, `increment`, `decrement`, `position_path`, control paths |
| `internal/quip/ids.go` | new section ids, `su:`/`sc:`/`se:` syncer ids, the 12-hex session id |
| `internal/quip/merge.go` | applying an edit to RTML and the three-way merge (Slack's `withRtml` and `mergeRtml` semantics without Automerge) |
| `internal/quip/doc.go` | the document model: sections by id, display order, lists and controls; applies load-data answers and fetch-latest changes with Slack's sequence rules (save answers: B2) |
| `internal/quip/bundle.go` | builds one save's `DocumentData` by `ZRIn.toUpdateBundle`'s rules, and `Validate` |
| `internal/slack/canvas.go` | `(*Client).CanvasCall(ctx, path, []CanvasField)`: the transport; the form keeps its order and is written as Slack's encodeBody writes it (`encodeURIComponent`, the token last) |
| `internal/core/canvas_quip.go` | the read model: `LoadCanvasModel`, the follow on `document_update` and files.open, `CanvasModel` |
| `internal/core/canvas_edit.go` | per-canvas edit state: save queue, retries, error state, rebase (B2) |
| `internal/ui/canvasview/edit.go`, `banner.go` | the save path on sections, Slack's banners, the editing lock |
| `internal/fakeslack/quip.go`, `quip_endpoints.go` | the fake's Quip model (made from the canvas's blocks, written back into them after a save) and the three endpoints |
| `tools/oracles/quip.cjs` | Slack's Quip modules in node (jsdom for the RTML parser) over `internal/quip/testdata/parity/*.json`: `python3 -I tools/parity.py --refresh quip-codec` reruns it (the `quip-codec` parity dimension) |

## Go types

Field numbers and types are from CANVASES.md; the full dump of build 9685e6ce144b is /root/clack-ops/scratch/canvas-edit/meta/schema.txt and schema-section.txt (regenerate with /root/clack-ops/scratch/canvas-edit/tools/schema.cjs; build 5718bdb1b28b's dump, 2026-11-23, is the same). Presence is explicit, as in Slack's encoder: a field is written when it was set, even to its zero value, so `type: TEXT_TYPE` (0) and an empty `parents` are on the wire. As built (B1), a field is a pointer, nil when unset (`quip.Ptr(int32(0))` sets a zero); the sketch below used a generic:

```go
// Opt is a proto2 optional field: set or not, whatever the value.
type Opt[T any] struct {
	V   T
	Set bool
}

func Some[T any](v T) Opt[T] { return Opt[T]{V: v, Set: true} }

type Section struct { // syncer.Section
	ID                  Opt[string]  // 1
	Sequence            Opt[int64]   // 2
	Deleted             Opt[bool]    // 3
	Dirty, Pending      []int32      // 4, 5 (read only)
	ThreadID            Opt[string]  // 6 (read only: never in a bundle)
	DocumentID          Opt[string]  // 7 (read only)
	Position            Opt[string]  // 8
	Type                Opt[int32]   // 9  quip.Type*
	Style               Opt[int32]   // 10 quip.Style*
	SectionClass        Opt[int32]   // 11
	Content             *Content     // 12
	Parents             *Parents     // 13
	AltRTML             Opt[string]  // 14
	Attributes          *Attributes  // 16
	TempID              Opt[string]  // 17
	MergedState         Opt[int32]   // 18
	Modified            Opt[bool]    // 19
	LocalSequence       Opt[int64]   // 20
	PositionPath        Opt[string]  // 21
	ContentOrigin       Opt[int32]   // 25
	HasLayoutParent     Opt[bool]    // 29
	TraceID             Opt[string]  // 33
	ReadOnly            Opt[bool]    // 35
	IsProgrammatic      Opt[bool]    // 36
	CrdtSession         []byte       // 38, kept raw on read, never written
	ActivityStackUpdate Opt[int32]   // 39
	Unknown             []int        // field numbers seen but not modelled (diagnostics)
}
```

The other messages, with only the fields Clack reads or writes:

| Go type | message | fields |
|---|---|---|
| `LoadRequest` | `syncer.LoadData.Request` | 1 object_ids, 3 bulk_loads, 5 user_action, 6 user_action_sequence |
| `BulkLoad` | `.Request.BulkLoad` | 1 type, 2 arg0, 3 arg1 |
| `LoadWrapper` | `syncer.LoadData.ResponseWrapper` | 1 error, 2 response, 3 preserialized_items |
| `LoadResponse` | `syncer.LoadData.Response` | 1 success, 2 objects, 3 indexes, 6 failures |
| `Index`, `IndexEntry` | `.Response.Index` | 1 name, 2 entries {1 object_id, 2 sort_value}, 3 desc, 4 numeric |
| `Failure` | `.Response.Failure` | 1 reason, 3 organization_ids |
| `Preserialized` | `syncer.LoadData.PreserializedItems` | 1 preserialized {1 id_type, 2 value}, 2 crdt_sessions |
| `Payload` | `syncer.Payload` | 3 threads, 6 documents, 7 sections (the rest skipped and counted) |
| `Thread` | `syncer.Thread` | 1 id, 2 sequence, 3 deleted, 6 title, 10 document_id |
| `Document` | `syncer.Document` | 1 id, 2 sequence, 3 deleted, 6 thread_id, 7 title, 42 shard_chars, 48 max_section_sequence |
| `Content` | `proto.section.Section.Content` | 1 text, 17 list, 42 slack_channel, 43 horizontal_rule, 44 slack_user, 48 slack_emoji, 58 title, 62 slack_date, 65 layout_blockquote, 68 slack_broadcast; any other kind is kept as `Other int` (its field number) and makes the section read-only to Clack |
| `ContentText` | `.ContentText` | 1 text, 2 language |
| `ContentTitle` | `.ContentTitle` | 1 text, 4 emoji_info; 2 header_image and 3 kept raw and re-sent as loaded whenever the title is saved (Slack's title `toPb` includes the cover) |
| `Parents` | `.Parents` | 1 ids, 2 container_id, 4 container_style |
| `Attributes` | `.Attributes` | 1 indentation, 2 checked, 3 background_color, 4 vertical_margin, 11 list_position, 13 default_content, 26 text_alignment, 34 is_new_document_default_content, 47, 48, 51, 57; any other field is kept raw and re-sent as it came (Slack's `toPb` re-sends every attribute it holds) |
| `SlackUser`, `SlackChannel`, `SlackEmoji`, `SlackBroadcast`, `SlackDate` | the control contents | as in CANVASES.md |
| `FetchRequest`, `FetchThread` | `syncer.FetchLatest.Request` | 1 requests {1 thread_id, 2 client_thread_sequence, 3 client_max_section_sequence} |
| `FetchResponse` | `syncer.FetchLatest.Response` | 1 changes_data, 2 section_consistency_hash |
| `Changes` | `syncer.ChangesData` | 1 ids, 2 indexes {1 index_id, 4 changed_entries {1 id, 2 remove, 3 sort_value}, 5 reload}, 3 temp_ids, 7 data |
| `DocumentData` | `syncer.DocumentData` | 1 sections, 13 title_slack_emoji_info, 14 title_rich_text_api |
| `EmojiInfo` | `proto.section.SlackEmojiInfo` | 1 team_id, 2 display_name, 3 unicode, 4 display_url |
| `RichTextAPI` | `proto.threads.RichTextApi` | 1 type, 2 elements {1 type, 2 elements {1 type, 2 text, 3 channel_id, 4 user_id, 5 style {1-6}, 6 name, 7 url, 8 skin_tone, 20 unicode}} |
| `EditAnswer` | `syncer.EditDocumentResponseWrapper` | 1 changed, 2 final_data, 6 is_new_edit_session (3-5 skipped) |

Codec rules, each from `c7F4.serializeToWriter` and `H1Ej`:
- `Marshal` writes set fields in ascending field number, repeated messages and strings one record each, repeated numbers packed.
- `Unmarshal` accepts packed and unpacked numbers and skips unknown fields, groups included (recording their numbers). It rejects truncated input, a field number 0, a length past the end and nesting deeper than 32. Invalid UTF-8 is replaced as `TextDecoder` would, except in the strings Clack writes back (RTML, ids, positions), where it makes the section read-only.
- Answers are capped at 16 MiB, like the export.
- Base64 is `encoding/base64.StdEncoding`.

## The client calls

`CanvasCall(ctx, path, form)` in `internal/slack/canvas.go`:
- **Host and token.** The enterprise host and token when the creds have an enterprise token, else the team's (`XgZX.Kc`).
- **URL and body.** `POST <host>/canvas/-/<path>?_x_version_ts=noversion`, with `form` plus `token` as `application/x-www-form-urlencoded`. "noversion" is what Slack sends without a page version.
- **Transport.** `c.do`, so the session guard, the identical-failure backoff, the honest headers and the cookie apply as for `Call`. Allowed paths are exactly `load-data`, `fetch-latest` and `edit-document`; anything else returns `ErrNotAllowed`.
- **Answer.** On 200, the body. A JSON answer (`Content-Type` application/json, or a body that parses as a JSON object with `"ok": false`; a protobuf answer can start with `{`, byte 0x7b, so the first byte alone doesn't decide) is an error: an auth code trips the signed-out state as `Call` does, any other code returns an `*Error`. Any other status is an `*HTTPError{Status}`, which the edit state sorts like `getSaveErrorInfo_`.

Calls Clack makes:

```go
// open an edit session (the first edit of a canvas view)
w, err := c.CanvasCall(ctx, "load-data", url.Values{"request_binary": {b64(quip.LoadRequest{
	ObjectIDs: []string{thread},
	BulkLoads: []quip.BulkLoad{{Type: quip.Some(quip.BulkThread), Arg0: quip.Some(thread)}},
}.Marshal())}})

// follow: after document_update, after files.open, and right before each save
f, err := c.CanvasCall(ctx, "fetch-latest", url.Values{"request_binary": {b64(quip.FetchRequest{Requests: []quip.FetchThread{{
	ThreadID: quip.Some(thread), ClientThreadSequence: quip.Some(doc.ThreadSeq),
	ClientMaxSectionSequence: quip.Some(doc.MaxSectionSeq),
}}}.Marshal())}})

// save
a, err := c.CanvasCall(ctx, "edit-document", url.Values{
	"thread": {thread}, "document": {doc.ID}, "retry_count": {strconv.Itoa(fails)},
	"session": {session}, "sequence": {strconv.FormatInt(seq, 10)},
	"data_binary": {b64(bundle.Marshal())}, "title": {title}, // title only with a title section
})
```

No user action: Slack sends none when no tracer runs. `files.open` stays as it is; `should_subscribe_and_ping: false` makes the canvas static (the `static` banner, no editing), as in Slack.

## The save path

**The model.** `core` keeps one `quip.Doc` per open canvas from its first edit: load-data once, then fetch-latest on every `document_update` for the file and after each `files.open` (Slack's backlog fetch). It applies changes with Slack's rules (CANVASES.md "Concurrent edits"): an update with a lower `sequence` than the section has is dropped; an update to a section with an unsaved edit waits until that save is answered. The view still draws from the HTML export; the model is only for edits. Section ids join the two: the export's `id` attributes are the model's ids.

**Opening a block.** The view asks core for the block's section(s). A paragraph, heading, quote line, code block, list item or title maps to one section. Its RTML becomes text plus marks. The editor gets the text and the marks it can show (b, i, u, del, code, links, user/channel mentions, emoji, @here). The other marks (annotations, font colour, sub, sup, link attributes Clack doesn't edit) stay with core, attached to their characters. The base snapshot is that RTML and the section's sequence.

**Finishing.** `fromDoc`'s lines become section updates by `ZRIn.toUpdateBundle`'s rules:

| edit | sections |
|---|---|
| text changed | the section: `id`, `sequence` (base), `type`, `content.text` = the new RTML, `parents` (as loaded), `modified`, `local_sequence`, `trace_id`, `activity_stack_update` per `WJQ2.shouldAddToActivityStack` |
| more lines (Enter) | one new TEXT section per extra line, after the edited one: id from `ids.go`, sequence 0, every known field, `position` = `generate(this, next in display order)` with `:temp`, `position_path`, the list parent and level for a list item, `attributes {vertical_margin: false}` as `30B3` makes them |
| emptied | `deleted: true` |
| checkbox | `attributes` as loaded with `checked` flipped |
| `# ` and friends | `style` (and `type` when it leaves its kind) |
| new mention or emoji | a new control section (`section_class` CONTROL_CLASS, `type`, content, `alt_rtml` for emoji, position path per `positionPathForControl`) and `<control id="…"></control>` in the text |
| rename | the title section's `content.title`, plus the form's `title`, `title_rich_text_api` and `title_slack_emoji_info` |

The new RTML comes from the base's text and marks with the editor's diff applied, the way Slack's `withRtml` applies one: words first (`71JW.spliceWords`), characters inside a replaced run, marks moved with their characters, then `getCompactRtml`. Marks Clack doesn't edit keep covering the characters they covered. A mark whose characters are all deleted goes away, as Automerge's would.

**Before sending.** Core runs fetch-latest. When the section's sequence moved past the base, it rebases: `merge(base, ours, theirs)` with `mergeRtml`'s semantics (both sides' inserts kept, both sides' deletes applied, marks from both), and the update's `sequence` becomes the latest. When the section is gone (deleted, or no longer text), the edit is dropped and the block disappears, as in Slack, which shows nothing. The current "The block you were editing was deleted" toast goes.

**Sending.** One save in flight per canvas; later edits queue. `session` is 12 hex digits made at start (`tAAR.s4` × 3); `sequence` is one counter for all canvases, bumped before each send; `trace_id` uses the value before the bump; `retry_count` is the canvas's consecutive failures.

**The answer.** With `changed`, `final_data`'s sections replace the model's (new sequence, final position, `temp_id` mapping). The view keeps its optimistic text until the export re-read that `document_update` triggers. `changed: false` is fine.

**Failures.** The edit stays queued; nothing is thrown away. The retry follows `i40t` and `7DQG.getSaveBackoffDelay` (an injectable clock for tests):

| status | counts | error state after | then |
|---|---|---|---|
| 400 | yes | 2 | retry with backoff |
| 403 | no | at once | quipOffline banner, retry every 10 s, editing not locked |
| 409 | yes | 9 | fetch-latest, rebase, retry |
| 413 | yes | 2 | quipDocumentTooLarge banner |
| 429 | yes | 5 | retry with backoff, or after `Retry-After` when that's longer (Slack's `fetch` ignores the header; Clack's transport honours it everywhere) |
| 503 | yes | 2 | retry with backoff |
| other, network | yes | 2 | retry with backoff |

Backoff: 10 s for the first five consecutive failures, then 10 s × 1.5^(n-5), at most 10 min. In the error state the canvas shows Slack's banner and is read-only (`cant_sync`) until a save succeeds. An edit still unsaved after 2 min shows the `unresolved` banner; Slack's socket being down shows `offline`. Closing the view, or quitting, tries one last save with a 5 s timeout. The current "Couldn't save your change to the canvas: …" toast and reload go.

**The banner** is Slack's `QuipSystemMessage`: the warning banner with `p-quip_system_messages--<type>`, the icon and the text from CANVASES.md, no close button for these types. Its CSS comes from client-boot-styles (`.p-quip_system_messages`) and Slack Kit's banner; round 2 measures it there.

## Validation

`bundle.Validate` runs on every `DocumentData` before it's encoded, and a failure refuses the save: the editor stays open with Clack's existing "This part can't be edited in Clack yet" message, and nothing is sent. It checks:

- **Sections.** Each one is known to the model and sent with the model's current sequence for it, or is new (sequence 0, a fresh `temp:C:` id with the document's shard characters and 25 hex digits). No id appears twice.
- **Fields.** Only the fields `toUpdateBundle` would include for that change are set. Never `thread_id`, `document_id`, `crdt_session`, `status`, `transient`, `dirty`, `pending` or `merged_state`.
- **Type and style.** `type` and `style` are a pair from `nTyX.pW` (TEXT with plain, H1-H6, code, blockquote, pull quote or caption; TITLE with title; LIST with bullet, numbered or checklist; HORIZONTAL_RULE with its style), and Clack only writes TEXT, TITLE and controls of the five kinds it knows.
- **RTML.** `content.text`/`content.title` parse under the RTML grammar with only `a b i u del code f sub sup br control annotation`. Links are safe (`isSafeUri`). Every `control` id is a control section in the model or in this bundle, and the text has no raw control characters, tabs or newlines. The title has at most 2000 code points and `title` at most 255 characters.
- **Positions.** A new section's `position` ends in `:temp`, sorts strictly between its neighbours (compared without `:temp`), and its `position_path` is its ancestors' positions plus its own, `:temp` included, joined with "-".
- **Parents.** A list item's parent is a LIST section of the model, and `container_style` is that list's style.
- **Round trip.** The encoded bytes decode back to an equal `DocumentData` with no unknown fields, and the RTML written then parsed gives the same text and marks.

Opening a block is refused the same way when its section has a content kind Clack doesn't model, a type/style pair outside the table, or RTML that doesn't parse. `canvas.Editable` stays for the export side.

## The other writes that used canvases.edit

| feature | round 2 |
|---|---|
| rename (title click, More actions → Rename canvas) | the title section, as above |
| Remove mention (`canvasview/mention.go`) | the text section without the `<control>`; the control section stays, as Slack's `eju0.remove` leaves it |
| Insert → Divider | a new HORIZONTAL_RULE section (`horizontal_rule {}`) |
| Insert → Profile | a new text section holding one user control |
| Insert → Blockquote, Formatting menu | a style change or a new TEXT_BLOCKQUOTE section (a LAYOUT_BLOCKQUOTE container under `canvas_blockquote_layout`) |
| Insert → Image, File; Insert table | not in round 2: their sections (IMAGE_TYPE with `content.image`, a file control or unfurl, LAYOUT_TABLE_TYPE) need their own reading of Slack's code (`0ealK`'s image `upload`, `30B3.slackObjectControlType`, `TableLayout.initializeContent`). Until then they are drawn disabled, as Date and Callout already are |

Creating (`canvases.create`, `conversations.canvases.create`) and sharing (`canvases.access.set`) aren't edits and stay as they are; open question 10 is about them.

## canvases.edit is never sent

- The edit, rename, checkbox, Remove-mention and Insert paths stop calling it; `core.EditCanvas` and `CanvasChange` go. Tests that read fakeslack's `CanvasEdits()` move to its decoded edit-document log.
- `internal/slack` refuses `canvases.edit` for session tokens before any network traffic (`ErrNotForSessionTokens`, next to `forbiddenMethods`), so a plugin can't send it either.
- fakeslack answers `canvases.edit` with `not_allowed_token_type` for the xoxc tokens, as Slack does, and counts it.
- A source scan test (like ua_test's) fails on the string `canvases.edit` outside the refusal list, fakeslack and docs.

## fakeslack

**The model.** Each fixture canvas gets a Quip document built from its `canvas.Doc`: a thread (`quip_thread_id` as now), a document with shard characters, sections for each block (title, text, lists and their items, quote lines, code, dividers, and controls for mentions and emoji), sequences in thousands, positions from `generate`. The HTML export is written from the sections, so a save shows up in `url_private`, and `canvas.Doc` becomes a view of the sections. `EditCanvasAsOther` edits sections like another client's save would (new sequence, `document_update`).

**The endpoints** on the fake's one host, `/canvas/-/load-data`, `fetch-latest` and `edit-document`:
- **Requests.** It checks `token` (the org token), the form fields, and that `request_binary`/`data_binary` decode with no unknown fields, and records each request decoded for tests.
- **load-data.** The thread, document, sections and `Section/<doc>` index. With `PreserializeSections`, sections go in `preserialized_items` instead, so the decoder's second path is tested.
- **fetch-latest.** Sections above `client_max_section_sequence`, with the thread and document.
- **edit-document.** It applies the bundle as the server presumably would and answers `changed` with the stored sections. It is strict where the real server is unknown: a section `sequence` below the stored one is a 409, a session `sequence` that doesn't go up is a 409, an unknown section id or control reference is a 400, a type/style pair outside `nTyX.pW` is a 400. It pushes `document_update` to every socket.
- **Faults.** `SetFault("canvas/edit-document", …)` and friends reuse faults.go (status, hang-up, delay, `Times`).
- **canvases.edit.** Refused for xoxc tokens, counted.

## Tests

All run in GitHub Actions; locally only gofmt and `go vet` on one package.

**Golden values from Slack's code.** `tools/quip/vectors.cjs` runs Slack's modules in node and writes `internal/quip/testdata/*.json`. Synthetic values only.
- **Encoder.** Slack's bytes for a load request, a fetch request, and `DocumentData` for each row of the edit table (text, new section, delete, checkbox, style, mention, rename). The Go encoder must produce the same bytes; the decoder must read Slack-encoded answers.
- **Positions.** At least 50 `generate` cases, carries and bad input included.
- **RTML.** Parse and write cases through `W77Y`, `fce5` and `getCompactRtml` (W77Y needs a DOM: jsdom in the tools directory).
- **Merges.** Three-way cases through Slack's `mergeRtml` with the public Automerge wasm, including two inserts at one spot, a delete inside a link, an edit next to an annotation.
- **Title rich text.** Cases through `30B3.convertSectionsToRichTextApiPb`.

**Protocol tests against fakeslack** (core, with an injectable clock):
1. **Save round trip.** Edit a paragraph: exactly one load-data, one fetch-latest and one edit-document; the decoded bundle is the expected section; the export then shows the text.
2. **Every edit kind.** New block, delete, checkbox, style, list item, mention added, mention kept (control id unchanged), annotation kept, rename.
3. **Concurrent edit by another client.** Clack opens a paragraph, `EditCanvasAsOther` changes the same paragraph and another one, `document_update` arrives, Clack saves. The saved text has both edits, the sent `sequence` is the latest, the other paragraph is untouched.
4. **Stale save.** The other edit lands between Clack's fetch and its save. The fake answers 409; Clack fetches, rebases and succeeds on the retry.
5. **Error answers.** 400, 403, 409, 413, 429 (with `Retry-After`), 503, 500 and a hang-up, each with `Times`: retry times, `retry_count` values, the banner type after the threshold, the editing lock, and recovery clearing both.
6. **canvases.edit is never sent.** Every canvas test asserts the fake's count is 0; a unit test shows `slack.Client` refusing it without a request; the source scan.
7. **Refusals.** A section with a table or callout won't open; bundles made invalid on purpose (bad position, unknown control id, invalid UTF-8, a `javascript:` link, a duplicate id, a wrong type/style pair) are refused by `Validate` and the fake sees no edit-document.
8. **Following.** Another client's edit while Clack only views: `document_update`, fetch-latest, model updated, and no request touches the section.
9. **Load failures.** load-data answering 500, "Not found" or an AUTH failure: no editing, the quipOffline banner.

**UI tests** in canvasview: the banner and the lock (CI screenshots, as the other canvas tests do), and a full edit through the fake.

**Schema drift.** `.github/workflows/quip-schema-drift.yml` (workflow_dispatch and weekly): it fetches the current quip-collab chunks named by datamine's chunks.txt, dumps the schema with `tools/quip/schema.cjs`, and fails when a field Clack writes changed number, type or label. A failure means Slack shipped a new schema: refresh the golden values before trusting saves.

## Status

- **A** (canvas-cursor): Quip's cursors over a canvas. Done.
- **B1** (canvas-quip-codec): the codec, the read model, the strict fake. Done.
- **B2** (canvas-quip-save): saves through edit-document behind the switch (off), Slack's retries, backoff, banners and lock, the validator. Done; what it doesn't save yet is listed in CANVASES.md, "Saves through edit-document".
- **B3**: every part Slack's editor edits, through edit-document; the refusal list and the can't-edit toast go.
- **B4**: the other canvas writes through Slack's client paths; canvases.edit and friends gone, kept out by a source scan.

## Order of work

1. `internal/quip` codec, positions, ids, RTML, merge, with golden tests.
2. fakeslack's Quip model and endpoints, with their own tests.
3. `slack.CanvasCall`, `core` edit state, protocol tests 1-9.
4. canvasview on sections, the banner and lock, UI tests.
5. Remove `canvases.edit`, the refusal and the scan.
6. The gate, then Karim's live check.

## Karim's live check

Only Karim runs this, on a canvas they made for it, with Slack open on the same canvas in a browser:
1. Type a word in a paragraph in Clack; Slack shows it within a few seconds; the canvas's version history shows the edit.
2. Type in the same paragraph in Slack and Clack at once; both survive.
3. Add a block, delete one, tick a checkbox, rename the canvas, add and remove a mention.
4. Cut the network during a save; the banner shows; after reconnecting the edit saves.

## Open questions only a live account can answer

1. Do the `/canvas` endpoints accept Clack's honest request: no Origin, Clack's User-Agent? The web client always sends Origin. A CSRF check would refuse Clack, and nothing in the client code says whether one exists.
2. Is it the enterprise host and token in an org, as `XgZX.Kc` says? The orchestrator's `canvases.edit` test failed on both hosts for a different reason.
3. What does the server do with a plain-content save whose section `sequence` is stale: merge with the base version, overwrite, or 409? With a plain-content save to a section other people edit with CRDTs, what do their clients get?
4. Does load-data put a canvas's sections in `objects.sections` or in `preserialized_items`?
5. Does the server care about a missing user action, or a session id it didn't issue (`InitOptions.session_id` from `quip.getControllerInitData`)?
6. What does an error look like on the wire: an empty body with a status, or JSON? Which status means an expired session?
7. Is the HTML export regenerated by the time `document_update` arrives?
8. Is a control section that no text references any more deleted by the server, or left?
9. Code blocks: one section with `<br>`s, or one per line as the export's `p`s suggest?
10. Do `canvases.create`, `conversations.canvases.create` and `canvases.access.set` also refuse session tokens? If so, creating should move to `files.createCanvas` and sharing to `files.updatePermission`, as in Slack's client.
