# Files audit: Clack vs Slack's web client (2026-11-09)

Sources: Slack's public CDN build a84662d2d135 (2026-11-08, chunk names
from slack-datamine-plus), fetched without a cookie: `client-boot`,
`gantry-v2-async-client-boot-shared`, `gantry-v2-async-gantry-v2-shared-boot-async`,
`gantry-v2-async-client-boot-vendors` and `client-boot-styles.css`. The
module ids below are webpack ids in those files. Nothing here came from a
live workspace: the real client wasn't driven, and no real file was
downloaded.

Fixtures: `fakeslack` `Size.Files` adds `#files-gallery`, one message for
each way Slack draws a shared file (internal/fakeslack/files.go). The
`files-shots` workflow renders it light and dark (`TestFilesShots`).

## Comparison

| Area | Slack (source) | Clack before | Clack now |
|---|---|---|---|
| Images in a channel/thread | `FileGallery` + `seRe` gallery v3 (`isMediaGalleryViewContext`): no file name above images; 1 file: `getDimensions` box (breakpoint caps 235/354/314 …, min 108); 2: shared height, widths grown from 108 (`getDimensions`, `isContainer`); ≥3: one row, widths ∝ 4/3, 3/4 or 1 (`getProportionalWidthV3`); flex width `min(width, 936)` | "name.png ▾" row above every image and video; 2+ images as 180px squares that wrap | Same algorithm (richview `mediaGallery`), under the title line (below) |
| Overflow | `getGalleryV3MaxVisibleFiles`: 4 at ≥600, 3 at ≥448, 2 at ≥224, else 1; the last tile shows `+ N` (N = files − visible + 1), 22px Black, `sk_primary_background` on `sk_foreground_high` at .5; clicks fall through to the tile | none (every image tiled) | Same; the viewer pages through every file ("2 of 6") |
| Tile look | `c-file_gallery__flex`: radius 8, `base-ter` fill, inset 1px `otl-ter`; cover, or contained at its own size when smaller than the tile | 1px border, cover | Same |
| Video | `FileGalleryVideoFile`/`VideoMessageFile`: sized like an image (original or thumb_video dims), poster contained; at rest the bottom-left pill (8+4px in, 28 tall, min 56, `#1d1c1d` .8, 1px `otl-inv-ter`, play-filled 16 + 13px Bold duration) | a 48px centered play circle plus a 12px badge, 480×360 max, name row | Pill, gallery sizing, no circle |
| Image + video | One flex gallery | the video on its own block | One gallery |
| Generic file | `c-pillow_file`: 12px padding, radius 12, 36px icon, name 16px Bold, `FileSize` = `pretty_type` only ("Arugula" → "Post") | `PDF · 2 KB` | `PDF` |
| Several documents | `c-file_gallery__mixed_file_format`: 40px columns, each file spans 7 (328px); `--compact` (≤664) one column ≤426; 8px gaps | stacked block by block | Grid |
| Audio file | `AudioPlayer` in a pillow: 36px play `#e6eff6`/`#1264a3`, name 15px Bold, "2:34 (900 KB)" 13px .7; 12px down the scrubber (4px `#ddd` track, 12px paused thumb) and the duration | 64px card, link-blue circle, "Audio" | Same as Slack |
| Voice clip (`slack_audio`) | flat `c-audio_player--audio_message`: 34px blue play, waveform (3px dots, 6px pitch, `min(6+34·s,34)`), 13px Bold time | same as audio | Same as Slack |
| Snippet | `FileSnippet`: pillow header, then CodeMirror at 13px/1.5 with a 32px numbered gutter; 4/8/18px edge padding; collapsed at 200px with a 40px fade; no max width | 560 wide, name + "N lines", 12px code, no gutter | Same as Slack except syntax colours (below) |
| File ⋯ / right-click menu | `buildFileContextMenuItems` templates: image `[download] [share_file, copy_link_to_file, copy_image, save_for_later] [open_in, edit_image_details, view_file_details, create_external_link] [add_to_folder, add_to_section] [delete_file]`; video `[generate_transcript, expand] [download, share_file, copy_link, save_for_later] [open_in, edit_thumbnail, view_file_details] [folder, section] [delete_file]`; other `[share_file, copy_image, copy_link_to_file, download, save_for_later]` | no Save for later, no Delete file | + Save for later / Remove from Later (`saveFile`/`removeFile`: `saved.add`/`saved.delete` `{item_type: file, item_id}`, file items tracked from `saved.list` and `saved_*` events); + Delete file on your own image/video files, through `DeleteFileDialog` ("Delete file", "Are you sure you want to delete this file permanently?", the pillow with "You • time", "Yes, Delete This File") → `files.delete {file}` |
| Upload: attach | `UploadQueue` eager upload: `files.getUploadURL {filename, length}` → `{file, upload_url}`; multipart POST of one `file` part (XHR); `files.completeUpload {files: [{id,title}]}`, no channel | `files.getUploadURLExternal` → POST → nothing until send | Slack's sequence |
| Upload: send | `shareFile` (`send_composer_message`) → `files.share {files: "F1,F2", channel, thread_ts, broadcast, blocks, client_msg_id, resharing_aware: true, skip_dlp_user_warning: false, from_share_modal: false}`; `comment` only without blocks (`getFormData` joins string arrays with commas) | `files.completeUploadExternal {files, channel_id, thread_ts, initial_comment}` | Slack's call, with the composer's rich_text blocks |
| Renamed pending file | `maybePersistPendingFileData` → `editFileApi` → `files.edit` | the title went in completeUploadExternal | `files.edit {file, title}` before the share |
| Block Kit file_input | uploads without sharing | `completeUploadExternal` with no channel | `PrepareUpload` then `files.info` for the file object |

## Sending files (image-send, 2026-11-09)

Karim: "sending an image by itself feels, wierd" and "the message gets 'sent' while the image stays behind a sec". Sources: the same CDN build (`gantry-v2-async-client-boot-shared`, `gantry-v2-async-gantry-v2-shared-boot-async`, `client-boot-styles.css`), plus Karim's two screenshots of the same message in Slack and in Clack.

| Step | Slack (source) | Clack before | Clack now |
|---|---|---|---|
| Attach | `UploadQueue` eager upload, then **PROCESSING** until `file_created` (`filesCreated` → `maybeResolveUploadInQueueWithFileId` → files.info). After 60 s (`_setProcessingTimeout`), or with no realtime connection, it polls (`pollForFileProcessing`: 7.5 s doubling to 30 s, for up to 10 min). The first failure retries once (`_retryUpload`). | done right after completeUpload, no retry | Same (`core.PrepareUpload`, `awaitProcessed`) |
| Tile | `MultiFileUploadItem`, `--medium`: 62px tiles, gallery padding 12, 12px apart. `RemoveFileButton` (`p-multi_file_upload__file_status_btn`): a 22px primary-background circle at top/right −10px. It holds an `InfiniteSpinner` medium (16px, blue `content-hgl-1`, a .25 `content-ter` track, 1.2s) while IDLE/UPLOADING/PROCESSING, `warning-filled` 20 on ERROR, and the 20px `base-inv-pry` × while the button is hovered (the tile hover shows × once uploaded). Tooltip "Remove file". No progress bar. | an invented progress bar along the bottom, × at −6 | Slack's button; padding 12, gap 12 |
| Send button | enabled with only a file. Enter sends, Mod+Enter with "Enter starts a new line". | same | unchanged; tested |
| What goes out | `prepareAndSendMessage` → `sendDraftFiles` → `files.share {files, channel, client_msg_id, resharing_aware, skip_dlp_user_warning, from_share_modal}` after **every** upload promise resolves. `convertDeltaToBlocks` returns nothing for an empty delta, so a text-less send has no `blocks` and no `comment`. The message's `upload`/`display_as_bot` are the server's; no client parameter sets them. | the same args, but the share went out before processing, so the echo carried a stub (FILE card, then the image) | the share waits for processing |
| Placeholder | `appendPlaceholderMessage`: `{…msgParams (blocks), client_msg_id, user, is_pending: true}`, with `files` when every upload is done or `pendingFiles` when some are still running. `PendingFilesAtom` draws the meta line (`PendingFilesMetaTextWrapper`: "Waiting to upload…", "Uploading…", "Processing…", then "Sending…"). Under it each pending tile is the File's own bytes at .5 (`c-pending_files__thumbnail--pending`) with a white jumbo (52px) spinner centred. `syncPlaceholderWithUploads` swaps in the real ids. The row isn't faded (no CSS for pending messages). | none: nothing until the echo | Same. The row holds the text and the image from the first frame; the previews sit in memory only (`internal/localimg`) |
| Swap | the echo with the same client_msg_id replaces the placeholder | n/a | `confirmPending`, keeping the processed file objects if the echo carries stubs. Without a socket, history is refetched after 5 s (`dispatchDegradedModeEventsForFileUpload`). |
| Failure | `sendDraftFiles`' catch: the placeholder goes (`cleanUpPlaceholder`), the text and files are saved as a draft, and `FileUploadErrorDialog` opens: "Couldn’t send message" / "Your message, along with any files and attachments, has been saved to your drafts." / "See Drafts", "Got it". | a toast; the text was gone | Same. The files and text go back into the composer, which is the conversation's draft. If the share's reply is lost, history is checked for the client_msg_id first, so resending can't post twice (Clack's addition). |
| Final row | `MessageFiles` wraps the gallery in `FilesMetaTextWrapper` unless `shouldUseCompactStack`, which the message pane and threads don't set. Its line holds the file title (one image, snippet or video, or any collapsed file), `pretty_type` (another single file), or "N files", then nbsp and the collapse button (`caret-down-filled` / `caret-right-filled`, 1em, the text color). `.c-message_kit__file__meta`: 13px, foreground_max .7, 4px under it. Measured on Karim's screenshot: a 22px line, 4px under the text. The toggle is an expandable keyed by ts + file ids + view, kept for the session. It starts collapsed for images when `expand_internal_inline_imgs` is off. | no title line (the earlier audit read gallery v3 as dropping it) | Same; images with the pref off are collapsed rather than drawn as cards |
| Composer after send | emptied at once, the draft deleted (`skip_file_deletion`), focus kept | the tiles stayed dimmed with a bar until the share returned | emptied at Enter; failure puts them back |

Every upload step goes to clack.log with the file id, sizes, timings and the error class only (`upload: F… slot for N bytes`, `POST … in …`, `processed (file_created) in …`, `sharing 1 file(s) as 1a2b3c4d`, `share … confirmed`, `… failed (files.share internal_error)`). It never logs file names or text. fakeslack has fault injection (`SetFault`: delays, HTTP statuses, API errors, dropped connections before or after the call takes effect, `DuplicateEvents`). `TestImageSendFaults` checks that every fault ends in exactly one message with both the text and the image, or in the visible failure, and never in the text alone.

Not done: the meta line's remove × (`renderRemove`), "Download all" (2+ downloadable files), the uploader prefix ("Name | ") for files shared by someone else, the title's truncation tooltip, and the pending tile's "Uploading… N%" tooltip. FileTitle's own hover is in a chunk I didn't fetch, so the title is drawn as plain text.

## Unprocessed uploads (image-click, 2026-11-09)

A pasted screenshot's `message` event often arrives before Slack has processed the upload. Its file has `name`, `url_private` and `permalink`, but `mimetype`, `filetype` and `pretty_type` are empty, `size` is 0, `media_display_type` is `"unknown"` and there are no thumbs. Slack's `fileIsImage` (`mimetype` contains `image/` and an image filetype or a `thumb_360`) says no, so for a moment Slack draws a file card too. The `file_change` that follows (`filesChanged` in boot-render) refetches the file, and the card turns into the image. Clack used to fill only files with no `url_private`, and it ignored `file_change` for messages. Such pastes stayed "FILE image.png" cards that opened the permalink instead of the viewer. Core now:

- treats a hosted file with no mimetype and no filetype (or `file_access: check_file_info`) as a stub, and retries `files.info` for about 20 s while Slack processes it;
- refetches every loaded message's copy on `file_change` / `file_shared` / `file_public`;
- keeps the complete file when a `message_changed` copy carries the stub again;
- also fills the files of the viewer's own echoed messages.

`media_display_type` appears in Slack's client only in its example fixtures and the file schema. Rendering doesn't branch on it.

## Not changed, or still open

- **Viewer (lightbox):** I didn't change it. The header, actions, zoom, rotate and paging were measured on app.slack.com in an earlier round (popups/lightbox.go), and the shot shows them working over the gallery.
- **Files rail tab:** the file actions are in "Files view" below. The layout was measured in fidelity round 2 (commit 71b7424) and wasn't re-measured.
- **Upload cookie:** Slack's XHR to `upload_url` has no `withCredentials`, so a browser sends no cookie there. Clack still sends the session cookie, because the session tests (`TestPastedSessionUnchanged`, `TestDesktopSessionJar`) require cookies on every request type.
- **`files.edit` args:** the method and the thunk (`fileId, title`) are in the source, but the fetcher's argument names are in a chunk I didn't fetch. Clack sends `file` and `title`.
- **Snippet syntax colours:** Slack renders `preview_highlight` (CodeMirror HTML). Clack draws the plain `preview`. Also not done: the hover "Click to expand" pill and expanding the snippet.
- **Hover actions on files** (`c-file__actions`: the top-right button group on images and pillows) and the hover swap of the pillow's meta to "View **PDF** in Slack" / "Download **Zip**" (`getActionText`): not done. The menu is on right-click.
- **Audio and video in place:** clicking the audio player's play button, or a video tile, opens the file's permalink. Clack has no in-message player, and the viewer only shows images.
- **View file details, external links, folders, sections, transcripts, edit image details:** not in the menu, because Clack has no screens for them.
- **`attachments_and_unfurls_max_width`:** with this experiment on, Slack caps the flex gallery at 716. Clack always uses 936.
- **PDF first-page thumbnail:** the PDF pillow uses the icon tile. I didn't check whether Slack's message pillow shows `thumb_pdf` (the `p-file_card__preview` card that does is used on the Files page).
- **Drag-and-drop overlay:** done on branch gap-w1-drop. Gio patch 11 adds OS file-drop events (Windows, macOS, X11, Wayland), and the message and thread panes show Slack's drop overlays and attach what's dropped (Shift shares at once). Paste, the + menu and drop all end in the same attach path. The huddle's `HuddleDragOverlay` and Slack Connect's blocked state are still open (docs/FIDELITY.md "Drop to upload").

## Files view (files-view, 2026-11-10)

The Files rail tab's last "not supported yet" toasts, replaced with what Slack does. Sources: the same CDN build, fetched without a cookie: `gantry-v2-async-client-unified-files-sidebar-view`, `gantry-v2-async-client-browse-unified-files-view`, `gantry-v2-async-client-boot-shared` and `client-boot`. The writes are tested against fakeslack only (`internal/fakeslack/files_view.go`, `TestFilesView*` in core and views). The `files-view` workflow runs them and saves screenshots.

| Area | Slack (source) | Clack before | Clack now |
|---|---|---|---|
| Sidebar gear | `UnifiedFilesSidebarView`: the "More options" settings button (`unified-files-sidebar-settings-menu`) renders only when its menu has items. Its one item, "Bulk manage files", needs `custom_file_sections` | a gear that toasted | no gear: Clack has no file sections, so the menu would be empty |
| Star button | `FileActions.renderFavoriteCanvas` / `renderFavoriteList`, with `custom_file_sections` off: canvases and lists only, "Add to starred" / "Unstar canvas" / "Unstar list". Starred uses `c-file__unstar_button` (sunflower-0 fill, sunflower-60 star) | a star on every row that toasted | on canvases and lists only, filled sunflower when starred |
| Starring | `addCanvasToFavorites` / `addListToFavorites`: the file is marked at once (`AP`), then `files.favorites.add {file_id}` (`createApiMethodFetcher` snake-cases `fileId` and keeps `reason` out); a failure unmarks it (`f_`). Removing is `files.favorites.remove {file_id}`. There's no toast in the Files tab (it toasts only outside UnifiedFiles) | none | the same (`core.StarFile`) |
| Starred section | `maybeFetchStarredUnifiedFiles`: `files.favorites.list {type: all}`, `file_ids` reversed. Rows are `browse-sidebar-item`s; the empty row says "Click the ⭐ star on any canvas or list to add it here for later." ("on any file" with `unified_files_3p_primary_custom_sections`) | always the "any file" hint | the starred files (type icon and title, click opens), or the hint for the viewer's experiment |
| ⋯ menu | `getFileActionsMenuItems` (hYuM) as `unified-file-result` calls it: `isUnifiedFiles`, `showMoreListBrowserActions`, `includeExternalLinkAction: false`, the root download button on so no overflow Download. Permissions from `getFileActions` (KfB/): `edit_file` is your own `hosted`/`email` file, `delete_file` your own (or an admin's) | a toast | Add to saved / Remove from Saved (Save for later / Remove from Later without `later_todos`), Add to starred / Unstar …, Copy link to canvas / list / file (`getCopyLinkLabel`), Edit file details / Edit image details, Open in split view (canvases, lists), Delete canvas / list / file |
| Delete | `DeleteFileDialog`: "Delete canvas" / "Are you sure you want to delete this canvas?" / "Delete canvas", lists alike, other files "Delete file" / "… delete this file permanently?" / "Yes, Delete This File". The file sits under the text with "You • time"; then `files.delete`. Lists leave at once (`shouldOptimisticallyDeleteFile`). A failure toasts "Oops! We couldn’t delete that file" | none | the same |
| Edit file details | `EditFileDialog`: "Edit file details", a "File name" field (required, at most 255 characters, placeholder "big-sur-trip-aerial.png") and Done. `editFileApi` sets the title at once, `editFile` sends `files.edit {file, title, is_canvas_template: false}`, and a failure puts the old title back with no toast | none | the same, name only |
| Templates | `ExplorerActionsTemplates`: on All files the canvas templates (search `type:canvas_template from:<@USLACKBOT>`) interleaved with the list templates ProjectTracker, FeedbackTracker and QuarterlyPlan (`PTs7.op` ids from `lists.templates`). Canvases shows 3 canvas templates, Lists the 3 list templates. "See all" isn't on All files. A canvas tile opens `openCanvasTemplateGalleryModal`, a list tile `ListTemplatePreviewModal` | six hard-coded tiles that toasted | the three list templates from `lists.templates` by Slack's ids. A tile makes the list (`lists.create {template_id}`) and opens it. "See all" shows on Lists only and opens Clack's `lists.templates` menu |
| Opening | `handleFileContainerClick` by `getMainFileAction`: `open` (canvases in the main view, lists), `view` (the media viewer), `remote` / `external` (the link), `detail` (snippets, posts and emails go to file details), or a download | canvases and lists opened; images went to the viewer; anything shared in a channel opened that channel; the rest toasted | canvases and lists open in full view and images in the viewer. Everything else opens its permalink in the browser, as message files already did. Message attachments (`views/adapt.go`) use the same opener |

Not done, and why (updated by exp-media, 2026-11-10; see the next section
for what it built):

- **Canvas templates:** the strip's canvas tiles and Canvases' "See all" open
  `openCanvasTemplateGalleryModal`, whose gallery and create call are in a
  lazily loaded chunk that hasn't been ported, so Clack still draws no canvas
  template tiles.
- **List template preview:** Slack's tile opens ListTemplatePreviewModal
  (boot-shared iRq6 → djIA ListTemplatePreview): a 1280-wide modal with the
  template's sample records in a read-only list grid, the name, description,
  "By Slack" and "Use Template" (lists.create from the template, then the
  list opens). Clack's `lists.templates` read keeps only id, name and
  description, and the read-only grid belongs to the Lists view, so Clack
  still makes the list straight away. Lists' "See all" opens Clack's
  `lists.templates` menu, not Slack's templates modal.
- **Menu items Clack has no screen for:** Ask Slackbot, Add to-do, Share canvas / Share list, View file details, Open in new window and Report to Slack. ("Open in split view" now has Slack's icon and leaves the simplified layout, from the split-view branch.)
- **custom_file_sections:** ported by files-view-2 (2026-11-11): the
  section picker, Bulk manage files, Move to section and the sidebar's
  file collections. See "Sections, Recently viewed and the rest" below.
- **Image alt text:** Slack's image dialog adds a preview and an alt text field. Clack edits the name only.
- **PDFs, video, audio, snippets, posts:** getMainFileAction (boot-shared
  C5YW) sends video, audio and PDFs to Slack's media viewer (pdf.js,
  `<video>`, `<audio>`), Markdown and HTML files there too under
  native_markdown_rendering / native_html_rendering, and snippets, posts and
  emails to the file details view. Clack's viewer shows images only; it has
  no PDF renderer, no MP4/AAC decoder and no file details view, and it never
  renders untrusted HTML in-process. They still open in the browser.

## Deleted, the menu and the Files experiments (exp-media, 2026-11-10)

Sources: CDN build 4d0c78085232 without a cookie:
`gantry-v2-async-client-deleted-unified-files-view`,
`gantry-v2-async-client-unified-files-sidebar-view`, `client-boot` (L1PI
Popover), `gantry-v2-async-client-boot-shared` (gaA3 FileActions, 6e8N
UndeleteFileDialog, afR1 undeleteFile) and `client-boot-styles.css`. Tested
against fakeslack (`TestFilesViewRestore` in core and views,
`TestFilesViewDeletedNav`, `TestSlackPlace`).

| Area | Slack (source) | Clack before | Clack now |
|---|---|---|---|
| ⋯ menu position | FileActions' trigger is a Popover at `right-bottom`, offsetX 4, tryOppositePosition (gaA3). L1PI's getInitialPosition puts the menu's top-left at the button's top-right; keepInBounds shifts it 16px (x) / 10px (y) inside the window instead of flipping, and takes `right-top` when the top had to move and that fits | anchored at the pointer | the same (popups `MenuOpts.Popover`, slack_place.go); the button's rect comes from the press position |
| Deleted page | DeletedFilesPage: "Deleted" (18px, 900) and, with rows, "You can restore deleted canvases or lists within 24 hours of deletion." (13px, 12px after it); no search or filters | the general Files page with a search field | the same |
| Deleted rows | DeletedFilesList: files.recentlyDeleted, isDeletedFile's canvases ("quip") and lists, the latest deletion first; DeletedFileEntity: the trash icon in c-file_entity__view_only_icon (32px, radius 4), title, "Deleted {ago}" (toTimeAgo), the always-shown MessageActionButton (undo 18, tooltip "Restore canvas" / "Restore list") at top 12, right 12 | rows without the star or ⋯ and no Restore | the same |
| Restore | UndeleteFileDialog: "Restore canvas" / "Are you sure you want to restore this canvas?", the row with the canvas or lists icon, Cancel and "Restore canvas"; files.undelete {file_id}; the row goes and the toast says "{title} restored." with View; a failure: "Oops! We couldn’t restore that canvas" | none | the same (App.ToastAction for View) |
| Empty | "Nothing to see here" / "You haven’t deleted any canvases or lists recently." | an invented "No deleted files" | the same |
| Deleted in the sidebar (file_trash_updates) | the sidebar loads files.recentlyDeleted when it mounts; the divider and Deleted show while that has rows, or always with file_trash_updates on | always shown | the same |

Under file_trash_updates the list also follows the socket (boot-render
filesDeleted / filesUndeleted): file_deleted refetches it
(invalidateDeletedFiles) and file_undeleted drops the rows
(removeRecentlyDeletedFilesByIds); core emits them as `file_trash` events
(`TestFileTrashEvents`, `TestFilesViewTrashEvents`).

Not done: Slackbot's trash notification (attachment type
`slackbot_file_trash_notification`) gets FileTrashNotificationButtons
("Restore file" through files.undelete, "View all deleted files",
"Restored"). Those buttons live in the legacy attachment renderer, which is
the messages area's, so they aren't ported here.

## Empty states, the sidebar's Starred part and the controls (karim-bugs-2-chrome, 2026-11-10)

Karim: the Files page is "so half-assed" (the Starred empty row had the ⭐
crammed against "star"). Sources: CDN build 4d0c78085232 without a cookie:
`gantry-v2-async-client-unified-files-sidebar-view` (cNNe
UnifiedFilesSidebarView), `…find-tab_browse-sidebar` (tpiL BrowseSidebar),
`gantry-v2-async-client-boot-render` (Fl3U BrowseSidebarItem,
FileCollectionHeaderRow), `gantry-v2-async-client-boot-shared` (gkKy
EmptyState, 31Gm explorer rows and ExplorerControls, b2qR the lists NUX
header), `gantry-v2-async-client-browse-unified-files-view`
(useUnifiedFilesExplorer), `…explorer_hooks_use-hosted-browser…` (jnm4
useUnifiedFilesFilters) and `client-boot-styles.css`. The account's team
has `custom_file_sections`, `unified_files_3p_primary_custom_sections` and
`file_trash_updates` on.

| Area | Slack (source) | Clack before | Clack now |
|---|---|---|---|
| Starred's empty row | an `empty` browse-sidebar-item: EmptyState (isExtended) in the 8px item container with `padding_left_150 padding_right_50 margin_0 align_left padding_top_0`, so the text starts 32px from the list's edge and has 8 + 20px under it; the description is `eD.t("Click the ⭐ star on any file …")`, the ⭐ a character with a space on each side | the star image laid over spaces with no space after it, 40px in | Slack's sentence through the theme's emoji slot (the image 1.2em in its own 1.27em slot, the spaces kept), 32px in, 28px under |
| Starred, `custom_file_sections` on | "file-collections-divider", then the starred collection's FileCollectionHeaderRow: 28px, padding 0 8px, 15px, the star that becomes caret-down-filled / caret-right-filled on hover, the theme-surf-inv-ter hover fill; a click collapses it (the client store: `custom_file_sections_collapsed_persist` is off) | a divider and a plain "Starred" label | the same, minus the row's ⋮ (below) |
| Starred, `custom_file_sections` off | spacer-2 (12px), then the `sub-header` (padding 0 8px 0 20px, `.p-browse_sidebar` makes it 15px weight 400) | a divider | the spacer and the sub-header, 20px in |
| Starred files | type canvas / list rows: `p-browse_sidebar__item--collection_indent`, 24px left padding | 8px | 24px |
| While files.favorites.list is out | five `shimmer` rows (list / canvas icon, a bar of random width) | nothing | five shimmer rows |
| Dividers | `.p-browse_sidebar__divider`: margin 12px all round | 8px from the edges | 12px |
| Empty list, All files | every file type on (isAllFileTypesSelected): "No files yet" / "Your recently viewed or updated files will be shown here." (with a search: "No results", same line); fewer types: "No files found" / "It may be that you don’t have any files yet, or that there aren’t any that fit your current filters." (with a search: "No results" / "You may want to try adjusting your filters."), both with an outline "Clear Filters" that turns every type back on | "No files yet" / "Files you share or view will show up here."; "No results" / "Try different keywords or remove filters." | Slack's four states and Clear Filters |
| Empty list, Canvases and Lists | "No canvases yet" / "Your recently viewed or updated canvases will show up here." and the lists NUX "No lists yet" / "Your recently viewed or updated lists will be shown here.", each with an outline + New Canvas / New List; with a search "No results" / "You may want to try adjusting your filters." | a link "+ New Canvas"; "… will show up here." for lists | the same copy and outline buttons with the plus icon |
| Empty state layout | `.p-unified_files_browser_empty_state` / `large-top-padding`: padding-top 16vh; EmptyState centred, max-width 380, the title 18px/24 Black, 8px, the description 15px/22, 8px, then the action 8px down | a 32px icon, a 16px Bold title, centred in a 300px box | Slack's |
| Types control | `filter_file_types_recents`: "{n} Types" (the selected count) with the refine icon, active (content-hgl-1 fill) only off the default selection | "All types" when all were on; always the active fill | "{n} Types"; active only off the default six |
| Sort control | active only off its default (Recently viewed) | always the active fill | active only when Last updated is picked |

Not done then: Recently viewed, the collection header's ⋮, the gear, the
"+" and "New" menus, the canvas-browser / canvas-content icons, paging and
the search sorts. files-view-2 ported them (below), all but "Assigned to
you".

## Sections, Recently viewed and the rest (files-view-2, 2026-11-11)

Sources: CDN build 4d0c78085232 without a cookie:
`gantry-v2-async-client-unified-files-sidebar-view` (cNNe
UnifiedFilesSidebarView), `gantry-v2-async-client-boot-render` (Fl3U
BrowseSidebarItem and FileCollectionHeaderRow, Q1Ql
useFileCollectionMenuTemplate, R3yk sortCollectionFiles, 3cwA / 0A31),
`gantry-v2-async-client-boot-shared` (QLxC FileSectionMoveToMenu, ofyN
FileSectionDropdownButton, vBP0 FileSectionCreateModal, ICbw / MgtY,
GIC/ maybeFetchCollections, KUDA / u7AE / d9Vx / oKEb / k+Nh / I0On the
open-files store, hYuM, gaA3, ZIeL Pagination, 31Gm, 7JHD),
`client-boot` (mSgJ / wbGe the unifiedFilesBrowser store, DdYY
generateKeyBetween, mAu1 isOnFreePlan, ogf8),
`…explorer_helpers_unified-files-create-menu` (n+Lz
UnifiedFilesCreateMenu), `gantry-v2-async-client-browse-unified-files-view`
(useUnifiedFilesExplorer) and `client-boot-styles.css` /
`slack-kit-gantry-v2-styles.css` (Panda tokens through toHash). The
account has `custom_file_sections`, `unified_files_3p_primary_custom_sections`,
`file_trash_updates`, `lists_grid_todos` and `later_todos` on, and no
`custom_file_sections_collapsed_persist`, `custom_file_sections_recents_limit_ab`
or `shareable_folders`. Tested against fakeslack only
(`internal/fakeslack/files_sections.go`: files.collections.* and
files.favorites.* with a collection_id; `TestFilesView*` in core and views,
`TestFilesSidebarStarred`, `TestFilesRecentlyViewedRoutes` in ui); the
`files-view` workflow saves the shots.

| Area | Slack (source) | Clack before | Clack now |
|---|---|---|---|
| Sidebar icons | the views' iconName: All files `canvas-browser`, Canvases `canvas-content`, Lists `lists`; canvas rows `canvas-content`, list rows `lists` (7JHD) | `files`, `canvas`, `list-view` | Slack's glyphs as centrelines (IoU against Slack's outline 0.999 for both new ones), held by TestIconsMatchSlack |
| Recently viewed, the store | filesOpenInSessionStore (KUDA): new files go to the top of the display order, reopened ones keep their place, twenty kept; getRecentlyViewedFileIds (u7AE) shows the display order of the ten most recently opened. FileOpenSubscriber (I0On) adds a canvas or list the canvas and list views open (maybeAddToRecentIfNotStarred: not from a channel tab, not a starred file, with sections not one in a section); moving a file into a section takes it out. It's kept in local storage per team (d9Vx) | no store | the same (core `FileOpened`, fed by docs/ and lists/ routes and the side panel); kept in ui.json by team, file ids only |
| Recently viewed, the rows | without sections: spacer-1, the "Recently viewed" sub-header (clock), the files (starred ones left out); with them: a file_collection header (clock, the caret on hover, collapses: isRecentlyViewedCollapsed), the files, recently-viewed-collections-divider. A row's × is removeOpenFileAndPersist | none | the same |
| Sections (file collections) | maybeFetchCollections: files.collections.list once custom_file_sections is on (no files.favorites.list then), again after a day; names unescaped; sorted by position, highest first; the client's collapsed state kept over a reload. The file-collections-divider, five shimmer rows while it loads, then each FileCollectionHeaderRow (.container radius 4, theme-surf-inv-ter hover, 12px above all but the first; the 28px row with the emoji, the star or `section`, the caret on hover) and its files in the collection's sort (sortCollectionFiles: last_read, updated, title A-Z without emoji, else position), the starred collection's empty row | the starred collection, from files.favorites.list, its header without ⋮ | the same |
| Collapsing | toggleCollectionCollapse in the client store (custom_file_sections_collapsed_persist is off); Option-click expands or collapses all, Recently viewed staying open | the starred header only | the same |
| The header's ⋮ | useFileCollectionMenuTemplate: "Create…" New canvas (createCanvasInCollection) and New list (createListInCollection), Sidebar section (not on a free plan); the starred collection: a divider and Sort by (Last viewed / Last updated / A-Z / Date added radios, files.collections.update {collection, sort}); a custom one: Rename section, Sort by, Delete section (danger). The 20px ellipsis-vertical-filled ("More actions") on hover or while open; a Popover at right-bottom, offsetX -4, offsetY 4, 300 wide; a right-click on the header opens the same menu | not drawn | the same |
| Section modal | FileSectionCreateModal: "Create a sidebar section" / "Rename this section", "Name your section", the 44px emoji picker inside the 36px input (placeholder "Enter a section name"), Clear all (close-filled) once there's a name or an emoji, Close and Save (off without a name): files.collections.create {name, emoji} / files.collections.update {collection, name, emoji} | none | the same |
| Delete section | DeleteFileCollectionModal: "Delete sidebar section?", "Are you sure you want to delete **{name}**?", Cancel and Confirm (the primary default, focused), then files.collections.delete {collection} | none | the same |
| Move to section | the results' FileSectionDropdownButton ("Move file") in the star's place: canvases and lists, every file with unified_files_3p_primary_custom_sections; star-filled on the filled star button in the starred collection, the section's emoji, `section` in another, `star` in none. FileSectionMoveToMenu (320): "Move to…", the collections as radio rows (FileSectionName), Create section (not on a free plan), Remove from {section}. moveFileToCollection: out of the old collection, to the top of the new (generateKeyBetween), out of Recently viewed, files.favorites.add {file_id, collection_id}, "Added {file} to {section}." with Undo; removeFileFromCollectionThunk: files.favorites.remove {file_id, collection_id}, back to Recently viewed when asked (addToRecentsOnRemove). The ⋯ menu has no Add to starred / Unstar under custom_file_sections (hYuM `!ta`) | the star on canvases and lists only | the same |
| Sidebar file rows | BrowseSidebarItem canvas / list: 24px in, the product icon, the × on hover (.64, 1 under the pointer); CanvasSidebarContextMenu / ListSidebarContextMenu / FileSidebarContextMenu: Move to section ▸ (with sections), Copy link to …, Open in split view, Unstar canvas / list (Starred without sections), Remove from recently viewed | no ×, no menu | the same, without View canvas / list details, Share and Open in new window (as in the ⋯ menu) |
| Bulk manage | the gear ("More options", shown while its menu has items): Bulk manage files with custom_file_sections off a free plan. Edit mode (setIsFileEditMode): checkboxes, a click picks, no ×; FileOrganizeModeMenu under the list (otl-ter top border, 16/16/24 padding): "{n} selected", then "Clear selection" after a bar, and Done; Move to… (BulkFileSectionMoveMenu above it), Remove from {section} / Remove from all sections, or Create New Section with nothing picked; one "Moved {n} file(s)." / "Removed {n} file(s) from their sections." toast with Undo | no gear | the same |
| "+" | "Create new" (bottom-left): Canvas "Create and share content", List "Track and manage projects" (lists.create from the blank template, to-do mode under lists_grid_todos), Sidebar section "Group and organize your files" (custom_file_sections) | made a canvas (or Clack's list menu) | the same; Folder, Connect cloud app and New surface follow experiments and banners that are off (shareable_folders) or not ported |
| "New" | UnifiedFilesCreateMenu (plus-filled, bottom-right, 250 wide): All files New canvas, New list, Templates › Canvas templates, List templates, Import CSV as a list; Canvases New canvas, Start from a template; Lists New list, Start from a template, Import CSV | made a canvas or opened Clack's list menu | New canvas and New list (below) |
| Paging | useBrowser shows results[page]; the "pagination" row when pageCount > 1: Pagination (caption text, 4px apart, 8px around; 20px circle back / forward carets; five page numbers, 20px tall, min 20 wide, radius 4; the current one in base-inv-hgl-2) | more pages on scroll, up to eight to fill the client-side filters | one page at a time and Slack's row |
| Types filter | the browser's fileType filter: search type: modifiers (`lists`, `quip` + `documents`, `spreadsheets`, `presentations`, `pdfs`, `folder_skill_set`, `audio`, `videos`, `images`, `snippets`, `emails`) | a client-side match over loaded pages | type: modifiers, a new search on each change |
| Sorts | Recently viewed (last_engaged) / Last updated (Created by you: Last updated only); with a search Most relevant (score, picked when the search starts) / Last updated | Recently viewed / Last updated, sorted client-side | the search's sort |

Not done, and why:

- **Assigned to you.** Slack draws the row (check-circle) while
  lists_grid_todos is on and lists.getMyItems returns items. It opens
  AssignedTasksPage, `client-assigned-tasks-view`, about 2,300 lines
  beautified: the refine header's All / Open / Completed items, Sort and
  Group by, the group headers, and TaskRow, whose to-do checkbox writes the
  list cell next to the reminder date and the record reference. I haven't
  ported that page, so Clack doesn't draw the row yet.
- **The "New" menu's other rows.** Canvas templates and Canvases' Start
  from a template open openCanvasTemplateGalleryModal. List templates and
  Lists' Start from a template open ListTemplateBrowseModal, boot-shared
  1OTT with djIA's preview grid. Create a template and Import CSV open
  their own modals, `…lists_subcomponents_import-modal` for the CSV. None
  of those modals is ported, so Clack leaves the rows out.
- **Dragging.** Slack reorders sections by drag (files.collections.reorder)
  and takes files dropped onto a section. Clack has neither.
- **CustomFileSectionsNuxBubble.** "Organize it your way" with "Create a
  Section" shows while a coachmark is open, and Clack doesn't read
  coachmark state.
- **Selected file rows.** Slack keeps the Files sidebar next to an open
  canvas or list and marks its row. Clack's docs/ and lists/ routes belong
  to Home, so a file row never shows as selected.


## Files & links tab (files-links-tab, 2026-11-12)

Karim: "files and links arent implemented?" The channel header had the
tab, but clicking it toasted "This tab opens in Slack". Sources: CDN build
0e695dd18302 (boot-shared 0fb3750ed9646c84), fetched without a cookie:
`gantry-v2-async-client-boot-shared` (3ztk files_tab_view, JG8Q
useChannelAttachmentsExplorer, WiAq, yKEr Explorer, 31Gm explorer rows,
uahl ContextualFile, UZv6 / ae8D / XWLGa the pillow's detail line, YnzN
getActionText, gaA3 FileActions, hYuM, 6kKb handleFileContainerClick,
jAOO the grid), `client-boot` (XjMX fetchBrowserEntity /
fetchChannelLinks, rZ6M fetchSearchModule, d4lr / d81O the query tokens,
ogf8 the browser constants, biaa, 6qU2), the tab-view chunk (5Itp
useTabView, ResourcesTabView), `client-channel-files-view` (All files &
links) and `client-boot-styles.css`. The account's groups came from
experiments.getByUser (read only): `resources_channel_tab` and
`files_and_artifacts_from_meetings` unassigned (off),
`channel_links_in_files_tab` and `desktop_channel_links_in_files_tab` on.
Tested against fakeslack only: `internal/fakeslack/channel_files.go` puts a
files tab in #files-gallery's tab bar and answers the tab's reads from the
gallery's files, a canvas, a list and 14 links (`TestChannelFiles*` in core
and views, `TestFilesTabOpens` in ui). The `files-view` workflow runs them
and saves the shots (`chfiles-*`).

| Area | Slack (source) | Clack before | Clack now |
|---|---|---|---|
| Which view | useTabView: Xy.Files is ResourcesTabView with `resources_channel_tab` on, else files_tab_view (3ztk) | a toast | files_tab_view (the account's group) |
| Tab label and icon | ChannelTabLabel: "Resources" with resources_channel_tab, else "Files & links"; useTabIconProps: `files` / `files-filled` with it, else `canvas-browser` / `canvas-browser-filled` (selected) | "Files & links", the `files` glyph | "Files & links", canvas-browser; canvas-browser-filled while selected (a new glyph, held to Slack's by TestIconsMatchSlack). The Manage tabs card uses canvas-browser too |
| Reads | three browsers: media (`channel-files`, {in, fileType: images, videos}), files ({in, excludedFileType: images, videos, folder}), links (`channel-links`). The file ones are search.modules.files: the filters as modifiers after the words (`in:<#C|name> type:images type:videos`, `-type:images -type:videos -type:folder`), search_context desktop_files_channel_tab, count 50, highlight/extracts on, getSortParams' sort / sort_dir. Links: conversations.searchLinks {channel_id, page, query, sort, sort_dir} | none | the same calls (`core.ChannelFiles`, `core.ChannelLinks`), with Clack's usual omissions (no `_x_reason`, no `team`, as the Files browser) |
| Most relevant for links | fetchChannelLinks' switch matches QX.NEWEST / OLDEST and n5.RELEVANT ("relevant"); the browser's Most relevant is QX.RELEVANT ("score"), so it falls to timestamp desc | n/a | the same |
| Layout | .files_tab_view: base-sec, 20px down; ExplorerSearch "Search" (16 under); results at most 968 wide in 16px gutters | n/a | the same |
| Controls | "Attachment type" radios All / Files / Media / Links (+ From huddles under files_and_artifacts_from_meetings), Sort right: Newest / Oldest, Most relevant first with a search outside All; active off Newest. Back to All with Most relevant puts the files browser on Newest | n/a | the same, without From huddles (off) |
| All | without a search: "Photos and videos" + See all, one row of the media grid (140px cells, 16 apart, columns floor(w / 156)), 16px; then files and links in one list by share time (with a search the media join it); "More results by type" cards (Files / Media / Links, 42 tall, radius 12) for browsers with more pages | n/a | the same |
| Media / Files / Links | that browser alone, every grid row or list row, ExplorerLoadMoreButton "Load More" (outline, medium) while pages remain | n/a | the same |
| Empty | "No files or links found for this channel." / `No results for "{query}" in this channel.`; per type "No media found…", "No files found…", "No links found…" and their `No … results for "{query}"` forms, in .p-explorer_empty_state (200 tall, 18px Black) | n/a | the same |
| Loading | listLoadingFragment: seven result skeletons | n/a | seven skeleton rows |
| File rows | DraggableExplorerFile → ContextualFile: the pillow (12px padding, shared 1px otl-ter lines, radius 12 at the ends), title, "Shared by {name} on {date}" / "today" / "yesterday" (FileEntityUserTimestamp, displaySharedBy); the action text on hover (getActionText) | n/a | the same; the action text is the action Clack takes: Open canvas / Open list, View {type} in Slack for images, Open in browser otherwise |
| Link rows | LinkEntity: favicon 24 (else link 20), title or host, then the host (titled) or path + query; "Open in browser" on hover | n/a | the same |
| Clicks | handleFileContainerClick, source CHANNEL_TAB: canvases and lists open (openCanvas / openList: the side panel), images in the media viewer; links window.open | n/a | the same; PDFs, video, audio and the rest open their page in the browser (openFile, as the Files view) |
| Row ⋯ | FileActions' hover group: Download (hosted files), Share, the canvas / list star, More actions (getFileActionsMenuItems); LinkEntity: View link details (info) and More actions: Copy link ("Link copied to clipboard"), Add to folder ▸ | n/a | Download and More actions (the Files view's menu items); links: More actions with Copy link |
| Keyboard | the explorer's virtual list (↑/↓, Home/End, PageUp/PageDown, Enter) and the grid's arrow keys | n/a | vlist_keys.go over the rows; the media cells are rows of their own (↑/↓ walk them in order) |
| Tab permalinks | getChannelTabPermalink /archives/<C>/tabs/<id> opens that tab | the channel, on Messages | the channel, then the tab once the tab bar has it (canvas, list, Workflows and Files & links tabs) |
| Search | ExplorerSearch: Enter on the type-query completion sets the query on all three browsers (All keeps the sort, others take Most relevant); clearing resets them | n/a | Enter searches, clearing the text resets |

Not done, and why:

- **ResourcesTabView** (`resources_channel_tab`: "Files & links" with View
  all, "Media", "Recent huddles", and the All files & links page). The
  account doesn't have it; Clack reads no experiment for it and always
  draws files_tab_view.
- **From huddles** (`files_and_artifacts_from_meetings`, off): the radio and
  its huddles.history-style list aren't ported.
- **The search's autocomplete.** Done in round 2 (below).
- **Hover actions Clack has no screen for:** Share (the file share dialog),
  the canvas / list star in the hover group, View link details (the link
  details flexpane) and Add to folder (the folder tabs themselves are in
  docs/CHANNELS-AUDIT.md "Pins tab and bookmark folders"). The file ⋯ menu
  is the Files view's subset, in that view's order, not hYuM's non-unified
  order.
- **The media viewer's paging.** Slack passes fileChannelMediaInfo so the
  viewer pages through the channel's media; Clack's opens the one image.
  Videos open their page (no player).
- **The grid's 2-D arrow keys.** ↑/↓ step through the cells one by one.
- **Dragging** files and links into folders (DraggableExplorerFile/Link).
- **Manage tabs:** reordering and hiding the tab aren't wired (the card
  lists the tabs only).
- **Real pages.** conversations.searchLinks' page size isn't visible from
  the client; fakeslack uses 10 so Load More is exercised.

## Files & links tab, round 2 (files-links-2, 2026-11-13)

The round-1 list above, worked through. Sources: CDN build 0e695dd18302,
fetched without a cookie: `gantry-v2-async-client-boot-shared` (JG8Q
useChannelAttachmentsExplorer's searcherOptions and onSearch, l7jR
ExplorerSearch, drV9 Omniswitcher, 4l6z / OXCx the suggestion builder,
tGcK the query echo, 2uBe useFileClick, gaA3 FileActions' renderShare /
onShare / renderFavoriteCanvas / renderFavoriteList, sZ6G
addCanvasToFavorites, XJ82 addListToFavorites, H8G4 shareFile, Zp0h
ShareDialogCopyLinkButton, 31Gm LinkEntity / DraggableExplorerFile /
ExplorerExpandableGrid, jAOO Grid, byyT useContainerKeyboardNavigation,
NjKd FileShareDetailsMessageEntity, 8HEe AddOrMoveToFolderMenuItems, qSP5
AddBookmarkModal, plzS useRemoveTab, allT removeTab, AIhO updateTabOrder,
hm7X addTabToChannel), `client-boot` (QcwJ the searcher's getRemoteFiles,
NQkX fetchFilesWithQuery, mwX5 the tab helpers and canEditTabs (Ln), YQEj
the bookmarks fetchers), `gantry-v2-vendors` (Bfb3), boot-vendors (08G8
conversations.removeTab, IdoR addTab, XLhM moveTab), the share-dialog
chunk (zUYy), client-link-details-view (DEJt), the canvas-share-dialog
chunk (ut+Q files.getOverview), manage-channel-tabs (Wg5J), the
bookmarks-store chunk (H9Rh useTabSchemaToTabItems), media-viewer (viWS)
and client-boot-styles.css. Tested against fakeslack only
(`internal/fakeslack/channel_files_more.go`): core
`TestChannelFileSuggestions`, `TestShareFileTo`, `TestLinkShares`,
`TestChannelTabWrites`, `TestMoveTabAfter`, `TestCanEditTabsOnGrid`;
popups `TestShareFileDialogLikeSlack`, `TestFileAccessText`,
`TestFileGallery`; views `TestChannelFilesSuggestions`, `…HoverActions`,
`…LinkDetails`, `…Gallery`, `…GridKeys`, `…AddToFolder`, `…ManageTabs`;
ui `TestFilesTabHideAndLinkDetails`. The `files-view` workflow runs them
and saves `chfiles-suggest`, `-hover-star`, `-hover-share`,
`-link-hover`, `-share`, `-viewer`, `-link-details`, `-add-to-folder`,
`-add-to-folder-none` and `-manage-tabs`.

| Area | Slack (source) | Clack before | Clack now |
|---|---|---|---|
| Search suggestions | ExplorerSearch is the Omniswitcher with searcherOptions {entities.files {channel}, limit 30, tiered, frecency sort (channel-tab:suggestions-from-searcher), the type filter: All every file, Media images and videos, Files the rest, Links none} and hideAutocompleteWithEmptyQuery. The searcher's remote files are getRemoteFiles → fetchFilesWithQuery with channelId: `search.autocomplete.files {query: "in:<#C> q", include_shares: true}` (the prefix cache is skipped with a channel). The omniswitcher's post-processing puts the query echo ("Show results for:", queryUser) after the top hit. onSearch: the echo is the type-query completion (the search of round 1), a file row is useFileClick's openFile | Enter searched | the list under the box while it has text: remote files after 250 ms (Clack's omniswitcher wait for its file rows) and the tab's loaded files whose words start with the query's, by the filter, the top hit, the echo, the rest (15 rows at most); 40px rows, pseudo-selection with the Enter key, ↑/↓ (from none the first or the last, past an end none), Enter, Escape, blur closes; a file opens as a row click does |
| Share | renderShare (share-message, "Share file…"): onShare opens openShareDialog for a file, openShareQuipModal for a canvas, openShareListsModal for a list. The dialog (zUYy, shareType file): "Share this file", one destination (selectedChannelId), the note, ShareDialogPreview over renderFileAccessMetadata (files.getOverview {file_id} on mount; "{n} users and {n} channels have access to this file.", users at least 1, "{count}+" when max-counted), Copy Link as a link (renderAsLink: the link icon, "You can also copy a link to this file. To share, paste it in any channel or conversation.", "Copied!"), Share (Forward with private_message_forwarding): shareFile = files.share {files, channel, blocks, resharing_aware, skip_dlp_user_warning false, from_share_modal false}, then navigate to the destination at ts / file_msg_ts | none | the same for files (popups/share_file.go on the forward dialog's picker and note). **Open:** a canvas's and a list's Share (their share modals aren't in Clack, so the button isn't drawn on them), the dialog's Save Draft for a file (drafts don't carry files here) |
| Star | renderFavoriteCanvas / renderFavoriteList: star / star-filled (sunflower), "Add to starred" / "Unstar canvas" / "Unstar list"; with custom_file_sections FileSectionDropdownButton. addCanvasToFavorites toasts "Added {title} to Starred." with View outside the Files view; addListToFavorites toasts only in a child window | none | the same (the Files view's toggleStar and section menu) |
| View link details | LinkEntity's info button: toLinkDetails → LinkDetailsView in the secondary view: "Link", the favicon and the title (h2), the URL, "First shared by {name}", "Shared {date}" / "Shared on {date}"; search.modules.messages {query: url, sort timestamp, sort_dir asc, search_context desktop_link_details} (fetchSearchModule's base args); "Conversations {count}" and one card per conversation ("Thread in {channel}", the time, the sender, the message); "No conversations found" | none | the same (views/link_details.go, ui/files_tab_more.go). The card's message is plain text with its links as labels, not the full message renderer; its replies bar isn't drawn |
| Add to folder | LinkEntity's More actions: "Add to folder", a submenu row whose submenu (AddOrMoveToFolderMenuItems) renders nothing for a link (it needs a file in the store); its click opens AddBookmarkModal "Add link to folder" with showSetFolder: the link and title, "Folder" over a select of the channel's folders; with none the banner "We’ll add this link to a Bookmarks folder for safekeeping." and the folder made first (addBookmarkFolderForChannel: bookmarks.add {channel, title, type: folder}); then bookmarks.add {…, parent_id} and "Added {fileName} to folder {folderName}" | Copy link only | the same on pins-folders' link modal (its BookmarkLinkModal grows the Folder field). the select's default folder tab (SwCK) and, with useCanEditTabs, a divider and "New folder" (Folder Name, made first), the banner's "Edit folder name" (tabs-3). **Open:** the submenu caret |
| Media viewer | handleFileContainerClick passes fileChannelMediaInfo (the media browser's loaded files); the viewer's queue is those fileIsViewableInMediaViewer keeps (images, PDFs, playable video), opened at the clicked file | one image | the channel's loaded images, ←/→ and the arrows ("1 of 28"). **Open:** video in the viewer: Slack plays it there, Clack has no video decoder, so a video still opens its page |
| Grid keys | jAOO Grid with byyT: ↑ the row above, ↓ the row below when it has the cell, ← / → with wrapping across rows; an unmatched key falls through to the list. All's collapsed strip keeps every media file as a key with the later rows display:none, so a move into them is taken and focus stays | ↑/↓ cell by cell | the same (vkOpts.Key) |
| Hide / reorder tabs | the tab bar's right-click (getTabContextMenu): "Hide tab" (danger) for a built-in tab with useCanEditTabs; ManageChannelTabs: hovered rows show the visibility eye (Hide tab / Show tab, debounced 300 ms) for editable system tabs and Move tab up / down, aria-disabled where the neighbour can't move; removeTab = conversations.removeTab {id, channel_id}, addTabToChannel = conversations.addTab {type, channel_id}, updateTabOrder = conversations.moveTab {channel_id, id, after: the last tab before the new place}; a failure: "Something’s gone wrong. Mind trying again in a moment?". canEditTabs (mwX5 Ln): a member who may post, not archived, channel_managers → managers / who may assign them (a free team: the creator or an admin), on Grid without a setting the MANAGE_CHANNEL_TABS grant (edge permissions/info) | the card listed tabs | the same (ui/files_tab_more.go, views/details_tabs_manage.go); one core.CanEditTabs for the bar, the card, More, folders and Pins (tabs-3) |
| Rename / remove tabs (tabs-3) | getTabContextMenu by type: a folder's Add link (canUserPostInChannel), then with useCanEditTabs Rename tab / Rename folder for the rename table's types (AkE0 i2: canvas, folder, list…) and, for a tab that isn't static, Hide tab (built in), Delete folder, or Remove tab (danger). RenameTabModal (O1Cc): "Tab name" / "Folder name", the label (or the file's title) selected, the clear button, a canvas's "Clear text to default to file name" and its empty name, 256 at most, updateTabLabel = conversations.updateTab {id, channel_id, label}. useRemoveTab (plzS): a folder with items asks (TabDeleteConfirmationModal); a canvas or list is OH3f: the knowledge canvas asks (Remove: ai.channelKnowledge.disable), else files.hasShares {file_id, channel_id, exclude_channel_tab_shares}: shared nowhere else and not with the org asks "Also remove access to this canvas?" / "…list?" (Remove; Keep = keep_file_share). Manage tabs: edit (Rename tab) on canvas and folder rows, trash "Delete tab" on non-system editable rows | Hide tab only | the same (views/tab_edit.go, ui/files_tab_more.go, views/details_tabs_manage.go). **Open:** Add file (AddFileModal), Copy link to canvas / list, Add or Move to folder, Canvas options, the folder's post-updates toggle, Change default list view, split / secondary view and new window items; maybeShowCannotDeleteCkaFolder |
| Drag tabs (tabs-3) | DraggableDroppableTabs (E/Yw): with useCanEditTabs non-static tabs drag (slop, source at .5, a .channel_tab_dragging copy at the pointer); drop positions left / right on the last tab, top / bottom on More's last row, a folder's centre for a canvas or list; the 2px base-inv-hgl-1 indicator, base-ter on a folder's centre or the hovered More; calculateDropIndex → updateTabOrderByIndex → conversations.moveTab; More opens 500 ms into a hover and takes a drop to the end; More's rows drag and take drops; a folder's centre adds the file's bookmark and removes the tab ("Added {itemName} to <link>{folderName}</link>") | no drag | the same (ui/tab_drag.go). Attachment / link drops from messages aren't ported |
| Dragging | DraggableExplorerFile / Link are react-dnd sources (FOLDER_FILE_BOOKMARK / FOLDER_LINK_BOOKMARK) that drop onto bookmark folders; no OS drag-out | none | **open**: Clack's folder tabs (pins-folders) have no drop targets for them; Add to folder is the way in |
