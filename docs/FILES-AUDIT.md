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
| Images in a channel/thread | `FileGallery` + `seRe` gallery v3 (`isMediaGalleryViewContext`): no file name above images; 1 file: `getDimensions` box (breakpoint caps 235/354/314 …, min 108); 2: shared height, widths grown from 108 (`getDimensions`, `isContainer`); ≥3: one row, widths ∝ 4/3, 3/4 or 1 (`getProportionalWidthV3`); flex width `min(width, 936)` | "name.png ▾" row above every image and video; 2+ images as 180px squares that wrap | Same algorithm (richview `mediaGallery`), no name row |
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

## Not changed, or still open

- **Viewer (lightbox):** I didn't change it. The header, actions, zoom, rotate and paging were measured on app.slack.com in an earlier round (popups/lightbox.go), and the shot shows them working over the gallery.
- **Files rail tab:** not re-audited in this pass. The Files page was measured in fidelity round 2 (commit 71b7424).
- **Upload cookie:** Slack's XHR to `upload_url` has no `withCredentials`, so a browser sends no cookie there. Clack still sends the session cookie, because the session tests (`TestPastedSessionUnchanged`, `TestDesktopSessionJar`) require cookies on every request type.
- **`files.edit` args:** the method and the thunk (`fileId, title`) are in the source, but the fetcher's argument names are in a chunk I didn't fetch. Clack sends `file` and `title`.
- **Snippet syntax colours:** Slack renders `preview_highlight` (CodeMirror HTML). Clack draws the plain `preview`. Also not done: the hover "Click to expand" pill and expanding the snippet.
- **Hover actions on files** (`c-file__actions`: the top-right button group on images and pillows) and the hover swap of the pillow's meta to "View **PDF** in Slack" / "Download **Zip**" (`getActionText`): not done. The menu is on right-click.
- **View file details, external links, folders, sections, transcripts, edit image details:** not in the menu, because Clack has no screens for them.
- **`attachments_and_unfurls_max_width`:** with this experiment on, Slack caps the flex gallery at 716. Clack always uses 936.
- **PDF first-page thumbnail:** the PDF pillow uses the icon tile. I didn't check whether Slack's message pillow shows `thumb_pdf` (the `p-file_card__preview` card that does is used on the Files page).
- **Drag-and-drop overlay, paste preview, upload progress row:** I didn't touch these. They are composer work (internal/ui/composer/attach*.go).
