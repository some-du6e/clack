# Emoji picker and frecency

Sources: app.slack.com web client, 2026-11 (public CDN chunks, no cookie):
`gantry-v2-async-client-boot-shared` modules `Zu56` (EmojiPicker), `bEcp`
(getFrequentlyUsedEmoji), `2rPc`/`W3UT` (recordEmojiFrecencyEvent /
recordFrecencyEvent), `client-boot` `RI+6` (the frecency store), `1TmA`
(setFrecencyByApi), `j8FV`/`y+lP`/`X46X` (searcher sorters and the
`emojiFrequentlyUsed` model), `client-boot-styles` CSS (`.p-emoji_picker*`,
`.p-emoji_and_gif_pickers_container`). A live read-only look at the
picker in the test workspace's #C0B4DEFKFCJ on 2026-11-09 confirmed the
geometry and the Frequently Used shape.

## Frequently Used (internal/frecency, core/frecency.go)

The `frecency` user pref (a JSON object, sent by users.prefs.get as a
string) keys records `{id, count, visits[≤10 ms timestamps], _reduced?}` by
item id and by the normalized text typed before the pick. Emoji ids are
`"E" + unicode` (`E1f44d`) or `"E" + name` for custom emoji.

- **Score** (calculateScore): mean visit weight × count. Weights by age: 4 h
  100, 1 d 80, 3 d 60, 1 w 40, 30 d 20, 90 d 10, older 0; a `_reduced`
  record counts half.
- **List** (getFrequentlyUsedEmoji, max 36, min 5): the searcher's
  empty-query emoji sort. Records are scored as hit×weight + item×weight,
  decayed by 0.9755 per day since the last visit, normalized to 100 (+1
  for `thumbsup`/`point_up`). The `emojiFrequentlyUsed` linear model,
  log1p(itemCount), then re-ranks them stably, and only emoji with a
  record stay. If fewer than 36, `emoji_use` is used: `{name: count}`
  summed per canonical name, by count then name, and padded with the
  defaults when it has under 5. `ensureMinResults` appends as many
  `emoji_use` names as that list is longer than the frecency one. With
  neither pref the list is the defaults `slightly_smiling_face, heart, +1,
  100, bug`. The experiment `autocomplete_frequently_used_emojis_frecency`
  sorts by record count instead.
- **Cache**: a computed list is reused for 4 h, and a read within 15 min of
  expiry pushes it out to now + 15 min. Reloading the frecency prefs (the
  `search_frecency` "updated" RTM event: users.prefs.get
  `prefs=emoji_use,frecency`) drops it.
- **Recording**: a picker pick records `{emojiName, text: search query}`.
  A reaction added from a message (not from the picker) records `text: ""`.
  The composer's `:` autocomplete records `{id, text: matched text}`.
  Sending a message records nothing. A record bumps the id key (a new one
  is `_reduced`) and the text key (a record, or an array once a second id
  shares the text). The pref changes locally at once and is saved with
  users.prefs.set `name=frecency` behind a 60 s leading+trailing throttle.
  The client never writes `emoji_use`.
- Quick reactions "use frequently used" take the first three of the same
  list.

## Look

- Card 361×518, radius 6. Background `rgb(var(--sk_primary_background))`
  (#fff / dark #1a1d21). Shadow `0 0 0 1px var(--dt_color-otl-ter), 0 4px
  6px -4px #00000019, 0 10px 15px -3px #00000019`, the same literals in
  dark.
- The Emoji | GIFs tabs are Slack's own when the GIF picker pref is on
  (`p-emoji_and_gif_pickers_container`, GIFs from Giphy), not a plugin.
- Category tabs 32×28 at x 10 + 34i. The active tile is
  `sk_foreground_low_solid`. Inactive icons are `sk_foreground_high_solid`
  (#868686 / #818385); hover only darkens the icon.
- Rows (getRowHeight): per group a 1 px empty-header-row and a 27 px
  heading (13/700 at x 21, 24 px line), then 33 px emoji rows, the last
  one 43. The sticky heading is 26 tall, with its text at x 20.
- The hovered or keyboard-selected cell is tinted by `data-color-index` =
  (9 × screen row + column) mod 6: #9bdf58 #95d3fe #f7e922 #eea6fc #ffd58f
  #d3d1ff at .7, with no dark override. Only that one cell is lit.
- Footer `sk_foreground_min_solid` (#f8f8f8 / #222529), border-top
  #00000026 in both modes. "Add Emoji" sits at left 14, bottom 14.5. The
  preview is a 32 px image at (12.8, 17.1), the name 14.4/700 at
  (57.2, 13.5) and the aliases 12.8 at y 28.9. The Skin Tone toggle (22 px
  hand, 14.4/700 label) sits at right 19, bottom 14.5, with no hover style.
- Search: "Search all emoji", 15 px, radius 8. On focus the border clears
  and a 1 px ring plus a 5 px ring at 30 % sit outside it.
- Scrollbar (supports_custom_scrollbar): an 8 px thumb, radius 4, min
  36 px, primary foreground at .52, shown only while the pointer is over
  the list.
- Apple vs Google images follow the platform (`getEmojiMode`: Apple on
  macOS, Google elsewhere); the `emoji_mode` pref only switches to text.
  Clack still picks the set from `emoji_mode` (store.ParsePrefs), app-wide.
  Slack's own sprites are as drawn: the saluting face is cropped at its
  right edge (16.0 and 17.0 sets), and the dotted-line face is a dashed
  outline.
