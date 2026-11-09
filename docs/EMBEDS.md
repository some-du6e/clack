# Link previews (unfurls): Slack's rules

How Clack draws message attachments and link unfurls (`internal/ui/richview/unfurl.go`, `cards.go`), and where each rule comes from.

**Sources.** Slack's web client, build `a84662d2d135` (2026-11-08), public CDN chunks (no cookie): `gantry-v2-async-gantry-v2-shared-boot-async.080032942d9a3cd0.min.js` for the components, `client-boot-styles.cf726c4f2eaecc74.min.css` for the CSS. Module ids are webpack ids in that chunk. One live check on 2026-11-09 in the signed-in web client: read-only, test channel only. It recorded classes and boxes with `getBoundingClientRect` and `getComputedStyle`, and saved no message content.

## Which component

`MessageAttachment` renders `MessageAttachmentDefaultV3` when the `unfurl_attachments_redesign` experiment is on (`vYpC`: `_Z(e,"unfurl_attachments_redesign")==="on"`). The test account is in that group: the live props capture (docs/PLUGINS-props-live.md) has `MessageAttachment` with outer host `c-message_attachment_v3--hover_wrapper`, and `ConnectedMessageAttachmentTwitterX` with `c-message_attachment__body--social_media_card --fixed_layout --twitter_x`. Clack draws that group's components. The experiment-off path (`MessageAttachmentDefault`, a color bar on every card) is not modelled.

MessageAttachmentDefaultV3, in order:

1. An attachment with Block Kit `blocks` → `renderV2Attachment` (color bar and blocks only).
2. Services override that: `service_name` matching `/twitter/i` or `"bluesky"` → `MessageAttachmentTwitterX` (useRedesign); `"Threads"` → `MessageAttachmentImageCard`; `"Flickr"`; `/wistia/i`, `"YouTube"`, `/vimeo/i`, `"Loom"` → video service cards; `/apple\s?music/i`, `"Spotify"` → music cards.
3. With none of those, an image-only link (`iR.Wx`: no actions, no title (or a title on a .gif), no text, no `video_html`, has `original_url`) → `MessageAttachmentImageCard`.
4. `iR.Id` true → `renderV1Attachment(MessageAttachmentGeneric v1)`: the classic card with the 4px color bar. Id is true for: no title and no text, no author and no title, no `original_url`, a `bot_id` (the attachment's, or the message's: `ax2i` passes `attachment.botId || msg.bot_id`), `type` org_chart, a Grafana URL.
5. Otherwise → `MessageAttachmentGeneric` v3, the 106px row card.

Shares and message unfurls keep their own cards (`MessageAttachmentSlackMessage`, docs elsewhere).

## Which previews show (`GMrh`, `djIX` shouldShow)

`hidden` attachments never show. Shares, message unfurls, attachments without `from_url`, and those whose `from_url` isn't in the message (its text, with `&` as `&amp;`, or a link element in its blocks) always show. Otherwise a preview shows only with **expand_non_media_attachments** on, unless it is media with no text: `audio_html`, `other_html`, `video_html`, `image_url`, or service `twitter`.

**expand_inline_imgs** off collapses attachment media (`maUm isAttachmentExpanded`). The redesign has no media toggle (`ed = eo && !$`: no `AttachmentMediaTrigger` with the experiment on). The v3 card's right box stays empty, and the fixed cards show only their backup background.

## The v3 row card (MessageAttachmentGeneric v3)

CSS `.c-message_attachment_v3*`, measured live (716×108 card, 106px side):

- Wrapper: width 100%, max-width 716, max-height 114 (88 for `--short`). Inside it the card has `margin-bottom: 8px`, so 6px of that margin shows. Consecutive v3 wrappers share one wrapping flex row (`.c-message_kit__attachments:has(v3)`: max-width 720, `justify-content: space-between`).
- Card: base-pry, 1px otl-ter border (otl-sec on hover), radius 12, and the whole card is one link (`iR.Rb`: every service except YouTube, Vimeo, Apple Music, Spotify and Loom, when there is a `title_link` or `original_url`).
- Left box: padding 12, the content centered vertically in 106px, a 1px otl-ter border on the right.
  - Title row: 15px bold content-pry, line-height 1.25, one line with an ellipsis, margin-bottom 4. If the title is the URL itself (`iR.C0`), the host is shown instead and the URL becomes the text.
  - Text: 13px content-sec, line-height 1.25. `AttachmentText` with `shouldUseTruncate`, `truncateLineLength` 2 (Truncate clamps it to 2 lines), `maxCharacters` 700, no newline limit, no "Show more" (`shouldAllowExpand` is false for v3).
  - Footer: padding-top 4 after text (0 straight under the title row). The text is `footer || service_name || author_name` at 13px content-ter. The icon is `footer_icon || author_icon || service_icon`, 16px, radius 4, 8px before the text. The date is hidden (`shouldHideUnfurlDate`).
  - v3 draws no fields and no author row.
- Right box, 106×106, `background-size: cover`, filled with the first that applies: the `image_url` image, the poster of a `video_html` (its buttons hidden in this card), the `thumb_url` alone (`AttachmentThumb`), or, with no media at all, Slack's `no-image-c399e8e.png` (#eaeaea with a gray picture glyph; redrawn, not copied).
- `--short` (no text and no media): 80px.
- Container ≤475px (`@container v3-attachment`): the title clamps to 2 lines, the text is hidden, wrappers go 100% wide.

## The fixed-layout cards (X, YouTube…)

`.c-message_attachment__body--fixed_layout`: wrapper max-width 358 (375 for video, music and Flickr cards in a container ≤475px), max-height 408. Layout is a column: the left box (padding 12, max-height 208), then the media at 100%×200, cover, radius 0 0 12 12.

**X / Bluesky** (`MessageAttachmentTwitterX`, useRedesign). Measured live: a 358-wide card, the title at +13, the text 8px under the title row, the footer 8px under the text, the image 356×200 under the left box.
- The title is `title || service_name` (`iR.zy`). The author, after a "|", is `author_name || author_subname` (`iR.IJ`; Bluesky: `author_subname`).
- Text: margin-top 8, clamped to 6 lines (`truncateLineLength: 6`), 700 characters, no "Show more".
- Footer: padding-top 8, Slack's `brand-x-filled` glyph and `x.com` (`bsky.app` with `brand-bluesky-filled` for Bluesky). `service_icon` is not used.
- Media only when there is `image_url`, `video_html` or `audio_html`. A `thumb_url` alone gives no media box.

**Video service cards** (YouTube, Vimeo, Loom, Wistia): `video_service_card`.
- The title (linked to `title_link || original_url`) and the author stack in a column; the author is 13px content-ter with no bar.
- Text: padding-top 4, 6 lines.
- Footer: `service_icon`, or else `brand-youtube-filled` (YouTube) or `play-filled` (Vimeo, Loom).
- The media has a base-ter hairline above it.

**Play button.** Only `video_html` makes one (`AttachmentMedia.renderVideoMaybe` → `AttachmentVideo`). `image_url` wins over video (`renderImageMaybe || renderVideoMaybe`). The poster is `thumb_url`. Over it sit `.c-message_attachment__video_buttons`: the legacy `play` glyph and, when there is a `from_url`, `external_link`. Each is 3.4rem, white at opacity .7 with a `0 1px 1px rgba(0,0,0,.5)` shadow and padding `0 10%`. A thumb under 120×60 gets no buttons (`--small`). The buttons show in video, social and music cards and are hidden in the row card. Slack swaps in the `video_html` iframe when you click play. Clack can't embed the player, so it opens the link instead.

**Image card** (`MessageAttachmentImageCard`): `AttachmentMedia` at its defaults, `getAdjustedImageDimensions` (`T7Yx`) with max 360×360, contain, floored, inside a 358px card. The body has no border; the right box has a 1px otl-sec border and radius 12.

## The v1 card (bots, apps, links without original_url)

`MessageAttachmentGeneric` v1 inside `.c-message_attachment`: max-width 600, the 4px `__border` (radius 8, `color` or base-ter), the body padded 0 12px at 15px/1.5.
- Author row: icon `service_icon || author_icon`; name `service_name || author_name`, linked to `service_url || author_link`; then the other name or `author_subname`.
- Title, then text: `AttachmentText` with `maxCharacters` 700 and `maxNewlines` 5, then " Show more" / "Show less" (`gZUk`: `K=700`, `Y=5` when the caller gives none).
- Fields, footer and date as before.
- Image: `getAdjustedImageDimensions` 360×360, floored, 5px over it.
- `thumb_url` alone: the 80px thumb on the right.

## v2 (Block Kit attachments)

`renderV2Attachment`: `.c-message_attachment_v2`, the color bar (`.c-message_attachment_v2__border` margin-right 12), then `.c-message_attachment_v2__body` (padding-top 2) with the blocks. The attachment's own title, text and fields are not drawn. Not drawn yet: Slack's "Added by <app>" attribution, which needs the bot's name.

## Clicking an attachment image

Sources: `AttachmentMedia` (`PgfZ`) and `AttachmentImage` (`NANx`) in `gantry-v2-async-client-boot-shared`, `AttachmentThumb` and `renderV3Attachment` in `gantry-v2-async-client-boot-render`, and Slack's media viewer (`gantry-v2-async-media-viewer`, `MediaViewer`, `MediaViewerActions`, `MediaViewerModalHeader`; `7f4G openInNewWindow`, `kIXo getImgProxyUrl`).

- **Every `image_url` opens the image viewer.** `AttachmentMedia.renderImageMaybe` draws it with `AttachmentImage`, whose `<a class="c-message_attachment__image">` has an `onClick` that calls `preventDefault()` and then `openFileViewer({mediaItem: {image: {src, width, height}, botId, title}, mediaId: ts + media index})`. The media viewer context's `mediaViewerEnabled` is only on in huddle threads, so in a conversation this is always the modal media viewer. This covers the image card (`MessageAttachmentImageCard`, a bare image link), the X/Bluesky, Flickr, video and music cards' media, the row card's right box, the v1 card's image (and its `media_aside`), and Block Kit `image` blocks in an unfurl (the Block Kit `Image` also renders `AttachmentImage`).
- The v3 card is an `<a href={title_link || original_url}>` (`iR.Rb`), but the image's `preventDefault()` stops it from navigating, and the card's `onClick` (`tT`) only logs. Clack registers the card's hit before the hits inside it, so the image (and a video's poster) sits on top of the card. A right-click on the image is still the card's: the image has no context menu of its own.
- `thumb_url` alone is `AttachmentThumb`: a `<span>` with no handler. In a v3 card the click goes to the card's link. A v1 card isn't a link, so there nothing happens.
- `video_html`'s poster is `AttachmentVideo`: Slack plays the iframe in place. Clack has no player, so the poster opens `from_url`.

**The viewer for an image that isn't a file.** `openViewer` passes no `fileId`, so the hide flags are all off, but `MediaViewerActions` renders the file's actions (Download, More actions with Copy link) only for a file id. Without one it renders only its own buttons, in this order: the thread toggle (when there is a channel and thread), then "Open in new window" (`open-in-window`). That button calls `openInNewWindow`, which opens `image.src` in the browser. `image.src` is the image through Slack's image proxy (`getImgProxyUrl({url, rotate: true})`: `https://slack-imgs.com/?c=1&o1=ro&url=…`; URLs on Slack's own hosts stay as they are). Clack opens `slack.ProxyImage(url)`, which builds the same URL. There is no zoom control or Alt button: both need `fileIsImage`, which needs a mimetype. The header is the message's sender (avatar, name) and "time in #channel", with no "– title": `MediaViewerModalHeader` adds the title for file ids only. The image itself loads through Clack's image cache, the same path the inline unfurl uses: no credentials, through slack-imgs.com for third-party hosts.

## Remove preview

`AttachmentDeleteButton` (`qrgA`). v3 cards use the `overlay` style: a 20px base-inv-pry circle at top −9px, right −9px of the card, holding the `close-circle-filled` glyph in content-inv-pry. It shows while the card is hovered (`:is(.c-message_attachment_v3:hover) > .c-message_attachment__delete_container--overlay {opacity:1}`). Measured live: 20×20 at (705,−9) on a 716 card, with a 12px glyph.

Who gets it (`4BNQ canDelete`): the message exists, the viewer sent it (or is a channel manager on a channel that allows it; Clack doesn't model that branch), the subtype isn't `pinned_item`, and the attachment has an `id` and a `from_url` (or the message has text). Shares and message unfurls never get it.

Clicking it opens `BaseDeleteMessageAttachmentDialog` (`khoF`): title "Remove preview?" and the danger button "Remove". Admins also get an "Also block previews from this website in the future" form, which Clack doesn't offer. Confirming runs `removeAttachmentsIncludingChildren`: the attachment, and the others unfurled from the same `from_url`, leave the message at once, then `chat.deleteAttachment {attachment, channel, ts}` goes out for each (`OV4v deleteSingleAttachment`). If that fails, Slack shows "Attachment removing failed!". Slack's own path is chat.deleteAttachment; it doesn't use chat.update or chat.unfurl.

Not drawn yet: the v3 hover card (`MessageAttachmentDefaultV3HoverCard`, the full preview in a popover on hover), the right-click menu ("Remove preview", "Copy link"), and the v1/v2 rail-style remove button left of the card.

## Fixtures and screenshots

`internal/fakeslack/embeds.go` (`Size.Embeds`, #embeds-gallery) has an X post in the shape Slack's unfurler sends, a YouTube video (`video_html`, `thumb_url`), a GitHub app unfurl (author, fields, footer, color, `bot_id`), a site with a small thumb, one with a large image, a text-only site, an image link, a Block Kit unfurl and "image links": a message linking two PNGs, each unfurled as an image-only attachment (`from_url` = `original_url` = `image_url`, 1440×900, `image_bytes`, `media_type: "image"`, a `fallback`). The `embeds-shots` workflow uploads `embeds-<case>-{light,dark}.png`, `embeds-x-post-{light,dark}-hover.png` (the remove button), `embeds-channel-{light,dark}.png`, `embeds-bottom-*`, `embeds-remove-dialog-*` and `embeds-viewer-{1,2}-{light,dark}.png` (the viewer opened by a click on each image of "image links"). `TestAttachmentImageClicks` (richview) clicks each attachment kind and checks viewer vs link vs nothing; `TestEmbedsImageViewer` clicks the fixture's images through the app and checks that the viewer opened.
