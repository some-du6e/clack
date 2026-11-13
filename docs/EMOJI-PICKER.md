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
- **Loading** (frecency-grid, build 0e695dd18302: boot-render
  `loadFrecencyPrefs` and `fetchDeferredData`, client-boot `RI+6`
  getFrecencyCache, boot-shared `1TmA`; the grid account's
  users.prefs.get, read-only, 2026-11-12): Slack's store is one pref,
  `frecency` (tQkP `T0`), read through getUserPref and written with
  setUserPrefByApi `{pref: "frecency"}`. The client never names
  `frecency_jumper` or `frecency_ent_jumper`; the server picks the
  storage. On Enterprise Grid the account's frecency is stored as
  `frecency_ent_jumper`, and the plain `frecency` and `frecency_jumper`
  are `""`. A full users.prefs.get (no `prefs=`) returns every key as
  stored, so its `frecency` is `""`. A named request (`prefs=frecency` or
  `prefs=emoji_use,frecency`) returns `frecency_ent_jumper`'s value under
  the name `frecency`, byte for byte. Slack's client reads it only that way:
  loadFrecencyPrefs (`prefs=emoji_use,frecency`, reason
  `fetch-frecency-prefs`) runs in fetchDeferredData on every boot, on a slow
  reconnect, on `search_frecency` "updated", and in loadEmoji when the
  experiment below is on. Clack does the same on any workspace. The boot's
  prefs step sends that request after the full map, and a full map
  (boot, `pref_change` reload, RefreshPrefs) keeps the frecency in use and
  drops its own (core.keepFrecency). A `pref_change` for `frecency` or
  `emoji_use` sends the named request again. The store holds the jumper's
  records too (channels `C…`, members `U…`, menu actions `MA…`, search
  text keys), so the autocomplete and the jumper read the same pref.
  **α.48's defaults:** α.48 took the boot's full map as the account's
  frecency whenever it had both keys, so on the grid it took `""`. The
  account has `autocomplete_frequently_used_emojis_frecency` on, and that
  path (bEcp `M`) ranks only the frecency's own `E…` records and pads
  with the defaults. It never reads `emoji_use`, so the 41 `emoji_use`
  names couldn't help, and the list was the five defaults. Until the
  account's frecency is in, Frequently Used isn't cached, and a use is
  applied locally and queued, not saved: a save then would replace the
  synced frecency with a fresh one. Once the account's copy arrives the
  queue is replayed over it and saved (core.frecencyFromServer). A
  recorded use doesn't drop the 4 h list (bEcp's T stays). It changes
  when the save's `search_frecency` event reloads the prefs, as in Slack.
  With the experiment on, getFrequentlyUsedEmoji returns `M` (memoized on
  the frecency pref) before the 4 h cache, so that list follows every use
  at once.
- **Ids** (constructEmojiFrecencyId): `"E" + unicode` for a standard
  emoji (`E1f44d`), `"E" + name` for a custom one (`Ethumbs-up` on the
  grid account is Hack Club's custom `thumbs-up`). Channel and member ids
  are bare (`C…`, `U…`), menu actions `MA` + action (`MAcopy_link`).
- **Skin tone**: ids and `emoji_use` names are stored without the tone
  (recordEmojiFrecencyEvent resolves the picked name to its base emoji,
  W90V and Ak7A; getUsedEmojiFromPref strips `::skin-tone-N`), and the
  grid draws Frequently Used with the active tone, as every group.
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
- Category tabs 32×28 at x 10 + 34i. Grid cells (the list) are 36×32 every 37px (see "Against Slack's CSS"). The active tile is
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
- Skin tones (EmojiSkinTonePicker, boot-shared Zu56, build 4d0c78085232;
  popups/emoji_tone.go): the toggle shows "Skin Tone" only while
  preferred_skin_tone is unset. It opens a Popover at "top-right" (bottom on
  the toggle's top, right edges aligned): the radio group on the primary
  background, 1px base-ter border, radius 10. The hands are eG's order, or
  with a tone set the other five and then the active one. Without
  emoji_skin_tone_picker_improvement: padding 4 8, the label "Choose your
  default skin tone" (.68rem bold, 4 px under it), 22 px hands, and the
  focused hand gets the focus ring. With it: padding 4, no label, 36×32
  cells 1 px apart (radius 4) tinted on hover by the tone's color index,
  the ring on the cell from the keyboard only, the toggle's tooltip "Select
  default skin tone", and the arrows wrap from the selected tone (Enter or
  Space picks it). A press picks (setUserPrefByApi preferred_skin_tone) and
  closes; Escape closes only the popover; Tab stays.
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

## Against Slack's CSS (gap-w28)

The emoji-picker workflow draws Slack's light picker in Chrome and compares
it with Clack's (`TestEmojiPickerSlackPage`, `TestEmojiPickerVsSlack`,
artifact `slack-vs-clack-light*`). The page is Slack's markup rebuilt from
Zu56 (class names, nesting, screen rows) with Slack's own rules from
kit.css and client-boot-styles (`popups/testdata/slack-picker`), the same
emoji (the @2x images Slack loads on a 2x screen) and Lato. Slack's icon
font isn't in it: the category icons and the search glyph are masked.

Fixed from it (2026-11-13):

- `.c-input_text{border:1px solid var(--dt_color-otl-pry)}`: the search
  field's border is one CSS px of otl-pry (#7c7a7f light, #797c81 dark),
  not one device pixel of 30 % ink. This is popups' `fieldNative`, so every
  popups text field changed with it.
- `.c-button--outline`'s 1px border (Add Emoji, every outline button in
  popups): one CSS px.
- `.p-emoji_picker__footer{border-top:1px solid #00000026}`: composited in
  sRGB (211 over #f8f8f8). Gio's alpha blend is in linear light (231).
- `.p-emoji_picker__list_item{width:36px;margin:0 1px -1px 0}`: a cell
  every 37px (36 wide plus the 1px margin), not every 36. The ninth column
  sat 8px left of Slack's. The hit test skips the margin.
- `.p-emoji_picker__list_item{padding:.25rem 0 .2rem}` with `.c-emoji`
  `vertical-align:top`: the image sits 4px below the cell's top, not
  centred in its 32px.

Left as they are:

- The Emoji/GIFs tab underline is
  `inset 0 -2px 0 0 var(--dt_color-theme-base-pry, var(--dt_color-otl-hgl-1))`:
  the account theme's colour (Aubergine's #83388a in Clack). The page has no
  account theme, so Chrome falls back to otl-hgl-1's blue.
- The search field starts at 17.5px (`padding:14px 17.5px 7px`). Chrome
  draws that half pixel, Clack rounds it to 18.
- The sticky heading sits about 1.25px right of Slack's (`left:0`,
  `margin:0 6px`, `padding:0 14px`: 20px, which is what Clack asks for).
  The cause isn't found yet.
- Outside the picker, 35 more popups draw 1px borders as one device pixel
  (`r.Inset(1)`), thinner than Slack's at 125 %, 150 % and 2x. Not changed
  here.
