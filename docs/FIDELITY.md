# Pixel fidelity audit: Clack vs Slack 2026

How this was measured (2026-11-07):

- **Slack:** the real web client in the agent-browser profile `whatever`. I used my own Chrome window, driven over CDP, with a 1440×900 viewport, the workspace's aubergine theme and a 328px sidebar (meclod's saved width). Values come from `getComputedStyle` and `getBoundingClientRect`.
- **Clack:** first the CI build of `ui`@4222be3, then each `fidelity` build, run on a private 1440×900 Xvfb with the same sidebar width and the text embolden on. Values come from pixel sampling: ink bounding boxes and ink coverage of the same glyphs, and edge scans.
- **Dark mode:** Slack's main-pane tokens come from `.sk-client-theme--dark` (`--dt_color-base-pry #1a1d21`, …). Slack's dark *chrome* colours (rail, top nav, sidebar gradient) are per-user theme variables. Swapping the class in a tab doesn't reproduce them: you get a lilac chrome. So the chrome was checked against Clack's dark palette tokens only, not against screenshots.
- **Screenshots:** none are committed, because they contain messages. Slack message text never went to disk.

Priorities: **P1** visible at a glance on the main screen, **P2** visible when you look for it, **P3** under 2px or a rare state.

## Differences

| # | P | element | property | Slack | Clack (before) | delta | package | status |
|---|---|---|---|---|---|---|---|---|
| 1 | P1 | all text (env) | `CLACK_NOEMBOLDEN=0` | n/a | disables the embolden (any non-empty value counts) | text ink −26% (tab labels 159 vs 215) | theme | fixed c0f0b4b |
| 2 | P1 | file image, single | display size | Slack media-gallery v3: `d=min(thumb1024W, galleryW)`, `h=floor(min(d/aspect, cap, thumbH))`, cap by breakpoint (gallery ≥636: landscape 235, portrait 354, square 314; ≥472: 154/276/206; ≥448: 115/206/206; else 115/206/154), min 108. Gallery width = content width − 26, max 1000. 1779×683 → **612×235** | 720-thumb pixel size, cap 360 tall: **720×276** | +108px wide, +41px tall; wide screenshots 934 vs 720 | richtext (thumb_1024 dims) + richview | fixed 9ed9e3f, c5f099a |
| 3 | P1 | thread / secondary pane | placement | nested in the channel view **below the header and tabs**: x=1021, y=127, 415×769, `box-shadow 0 4px 12px rgba(0,0,0,.1)` | full height from y=40, beside a squeezed header | header and tabs lose 400px; pane 87px taller | ui (root) | fixed ae92fb0 |
| 4 | P1 | thread pane | content alignment | top-aligned: parent, "N replies" divider, replies, then the composer right after the last reply | bottom-aligned, with the empty space above the parent | parent 360px lower | messages | fixed bf4d3bd, ae92fb0 |
| 5 | P1 | rail icons (inactive) | glyphs | outline: DMs = two overlapping round bubbles; Files = two stacked rounded sheets with a folded corner; Agents & tools = three small circles plus a 4-point sparkle; thin bell | heavier glyphs: square chat bubbles, single sheet, four big overlapping circles | shape; ink +20% (To-dos 110 vs 91, circle Ø15 vs Ø17) | icons | fixed ac4c59a, 6bc1dd2 |
| 6 | P1 | message hover toolbar | icons 2, 4, 5 | thread (bubble with two lines), add to To-dos (check-circle with +), save to list (list with arrow in) | reply arrow, plain check, plain list | glyph | icons + messages | fixed ac4c59a, bf4d3bd |
| 7 | P1 | channel header Huddle button | border | none (`p-huddle_channel_header_button__container--polish`): 94×28, r=8, transparent | 1px border | 1px outline | ui (root) | fixed ae92fb0 |
| 8 | P1 | channel header facepile | border | none: the avatars and the "17" sit on white | 1px rounded border box | 1px outline | ui (root) | fixed ae92fb0 |
| 9 | P1 | broadcast reply ("also sent to channel") | layout | under the header line: "replied to " 400 15px/22 #616061 + "a thread" 700 15px #1264a3, then the body | above the header: "replied to a thread: View thread" 13px | line moved, text and size differ (+2px font) | messages | fixed bf4d3bd |
| 10 | P2 | top nav search pill | width | `min(1000, ⅔·(W − 4 − sidebarEdge − 88))` → **633** at 1440 | `min(680, …)` → **680** | +47px | ui (root) | fixed ae92fb0 |
| 11 | P2 | sticky day divider pill | top | header bottom + 6 → y=**133** | y=**141** | +8px | messages | fixed bf4d3bd |
| 12 | P2 | channel tab icons | glyphs | Messages: filled bubble when active; list: checklist card; canvas: rounded sheet with folded corner | outline bubble; bullets; plain document | glyph | icons + ui (root) | Messages + list fixed (ae92fb0); unread dot on tabs open |
| 13 | P2 | sidebar icons | glyphs | Directories: contact card with lines on the left; Add channels: "#" plus a small person in a rounded tile; DM section: two round bubbles (as the rail) | people icon; "#+"; square bubbles | glyph | icons | fixed ac4c59a |
| 14 | P2 | channel tab labels | weight/ink | 700 13px/16.25, active #1D1C1D (dark **#F8F8F8**), inactive #454447 | same spec; ink −9% with embolden; dark active #D1D2D3 | ink, colour | theme + ui (root) | fixed 52b26ed (weight), 2b1831c (dark colour) |
| 15 | P3 | rail labels | y | label box top = tile top + 40 (Home label y=149) | 1px lower | 1px | ui (root) | no change: same math as Slack (button 101 + 48); 1px is glyph placement |
| 16 | P3 | message header | name y vs avatar | name ink top = avatar top − 4 | −6 | 2px | messages | open |
| 17 | P1 | dark message text | colour | names `--dt_color-content-pry` **#F8F8F8**, bodies `--dt_color-content-sec` **#B9BABD** | names #D1D2D3, bodies #D1D2D3 (with a 0.235 fade compensating) | +39 / +24 per channel | messages, richview | fixed 2b1831c, 2230420 |
| 18 | P1 | text weight (embedded Lato, outline embolden) | ink vs Chrome | see the next section | bodies +7–14%, 900 names −9%, grey 12px +11%, light chrome rows +8% | up to ±14% ink | theme, richview | fixed 52b26ed…c7daf40 |

## Text weight vs Chrome

Method: total ink in the same string, Clack vs Chrome, with glyph widths checked too (they match exactly since the embedded "Clack Lato" faces win over the system `fonts-lato`). Light text is measured against a fixed white reference, so alpha (the light-text fade) shows. 2× uses a 2880×1800 Xvfb with `Xft.dpi: 192` (run it with `-noreset`, or Xvfb drops the resource) against Chrome at DSF 2. Final constants: regular 0.3px, Bold ×1.1, Black ×1.7, bodies 0.33px, mid greys ramp to 0 by luminance .43, light text fades by surface (chrome k=0.15; dark main pane k=0.27+max(0,.73−y)·2; Bold ×0.75; Black none, +0.12px), dark body fade 0.

| text | light 1× | light 2× | dark 1× | dark 2× |
|---|---|---|---|---|
| message body 15px (richview) | −2.9…−4.2% | −2.9…−3.3% | +0.6…+1.7% | **−7.1…−7.4%** |
| sender name 900 15px | −1.6% | −6.2% | +6.8% | −2.3% |
| timestamp 12px grey | +0.8% | −0.9% | −1.2% | +2.8% |
| tab label 700 13px (active / inactive) | −1.8% / +3.6% | −0.6% / +0.9% | +0.7% / +1.5% | +2.3% / +1.5% |
| topic 400 13px | +1.3% | −2.0% | −0.1% | +0.1% |
| sidebar rows 400 15px (white on aubergine) | −3.5…−4.5% | −2.9…−3.3% | n/a¹ | n/a¹ |
| team name 900 18px | −2.5% | −3.4% | −0.6% | −3.4% |

These were measured before the last calibration commit (bodies 0.30→0.33, Black ×1.6→×1.7, chrome fade 0.17→0.15), which targets the light −3% bodies, the 2× names and the chrome rows. Dark 2× bodies stay ~7% light: light-on-dark ink drops with scale and richview has no light-text embolden, so matching 2× would mean a scale-dependent embolden.

¹ Slack's dark sidebar can't be reproduced by a class swap (see above).

Measured equal (within 1px), so no change is needed:

- Top nav: 40px; history buttons 26×26 at 299/331/363; search text 13px at +27.
- Rail: tiles 52×68 (Agents 80) starting at y=101; active tile 36×36 r=8 `rgba(249,237,255,.25)`; labels 700 11px/12.
- Sidebar: header 49px, team name 900 18px at (87, 53.5), buttons 28×28 at 328/362, filter 311×28 at (79, 90); rows 28px with 8px padding, 16px icons at x=87, labels at x=111; the selected row's radius is 6.
- Message rows: avatar 36 at (16, 8) r=8; name 900 15px/22 at +60; timestamp 400 12px #616061 7.7px after the name; body 15px/22 at +26; consecutive row stride 52.
- Composer: 998×118 at (418, 754), r=8, border `rgba(94,93,96,.45)`, toolbar 38px #F8F8F8, footer 40px, 28px buttons.
- Ctrl+K: 650px wide at x≈390, rows 40px, footer hint row.

Not fixed here, because the difference is data or a feature rather than a style:

- Slack's onboarding row ("2 tasks left") appears only in some sessions.
- The top-bar "avatar" is Slack's agent launcher ("Open 24Bot" + "More agents"), driven by the ai_apps prefs and an AI-apps fetch (round 2).
- Huddle system rows ("You joined the huddle…") are Slack's own render of `huddle_thread` messages (round 2).

## Region diff score (light, 1×, mean |Δ| 0–255 and % of pixels off by more than 24)

Same window, same channel, the chrome regions that don't depend on message content. Sidebar sections stay high because Slack's onboarding row shifts them 28px.

| region | before (ui@4222be3) | after (fidelity) |
|---|---|---|
| top nav | 3.21 / 2.3% | 4.20 / 2.9%² |
| rail | 8.69 / 6.8% | 7.07 / 6.1% |
| sidebar top | 4.38 / 4.4% | 4.41 / 4.5% |
| sidebar sections | 35.42 / 22.4% | 35.38 / 22.4% |
| header | 7.74 / 6.8% | 7.66 / 6.7% |
| tabs | 12.00 / 10.6% | 10.08 / 9.4% |

² The top nav got slightly worse even though the search pill now matches Slack's 633px: Clack still lacks Slack's top-bar avatar button, and the narrower pill exposes more gradient. Not investigated further.

## Round 2 (2026-11-08, branch `fidelity2` from `ui`@402a63d)

The same method (my own CDP window on Slack, the CI build on a private Xvfb). Screens: every view, the emoji picker, the profile card, Preferences, the team, channel and message menus, the details dialog, App Home, plus the round-1 leftovers.

| # | P | element | property | Slack | Clack before (ui@402a63d) | after | package |
|---|---|---|---|---|---|---|---|
| 21 | P1 | Activity, read rows | mentions | `@clod` pill (#1264A3 on rgba(255,198,0,.18), r 3) on read rows too | read rows rendered `Muted` (system-message mode): "clod", no @, no pill | pill kept (richview `Options.Secondary`: Text3 colour only) | views, richview |
| 22 | P1 | Activity | day grouping | no divider for today, then a 50px "Yesterday" / "Monday, November 3rd" row (700 13px/18 #454447 at 16,16) | none | dividers before each older day | views |
| 23 | P1 | Activity, detailed row | height / status | 97px (name at +14, avatar at +18, footer 9px under the body, 14px bottom); nothing on the right when read | 92px; grey check circle on every read row | 97px, no check | views |
| 24 | P1 | Files results | list | one list of `c-pillow_file` rows: 65px each (66 the last), shared 1px #E8E8E8 dividers, radius 12 on the outer corners only | separate 59px cards with 8px gaps | one 65px-row list | views |
| 25 | P1 | message reactions | add-reaction button | always after the pills: 34×24, rgba(29,28,29,.06), fully round | hover only | always | messages |
| 26 | P1 | message menu | items / anchor | no "Forward message…" (it's on the toolbar); top 32px above the kebab's centre | Forward item; top at the pointer | matches | popups, messages |
| 27 | P1 | channel menu | items / anchor | no "Mute channel" (the bell does it); 300 wide, right edge 6px inside the kebab, top y 74 | Mute row; at the pointer (10px high, 5px left) | matches | popups, ui |
| 28 | P1 | Preferences | frame | 800×700 at (320,100): 76px header (title 900 22/36 at 28,20) with a 1px line under it, no footer; nav 205×29 items every 33px at (16,17), 15px icons, selected r 4; page at (249,21), sections 700 18px | 70px header, no line, a Done footer, 184px nav 24px higher, 15px bold titles at x≈228 | matches | popups (`ModalOpts.Flush`), ui |
| 29 | P2 | team menu | position | 320 wide at (58, 91) | (86, 82) | (58, 91) | ui |
| 30 | P2 | Huddles page | banner text / cards | text block at +76/+66 in the banner; DM cards 301×239 | +48/+48; 258 tall | matches | views |
| 31 | P2 | Drafts & sent | banner | title 900 18px at (24,45), body right under it | title at y 20, 4px gap | matches | views |
| 32 | P2 | Drafts markers | sidebar | "✎ N" (13px pencil + 400 13px count, 8px from the edge) on Drafts & sent; 13px pencil on conversations with a draft | none | from drafts.list, refreshed on `draft` events | ui |
| 33 | P2 | To-dos / Saved empty | layout | 157×140 SVG illustration, 24px gap, 900 18/24 title, 15/22 text | 64px icon only, text 32px higher | the 157×140 box kept (SVG not drawn: no SVG decoder) | views |
| 34 | P2 | Saved | header | no line under "Saved" when empty | line | none when empty | views |
| 35 | P2 | Directories | People | no invite hero; filters Title, Location, Workspaces, More | "Invite your team" hero + Department | matches | views |
| 36 | P2 | Threads view | stamps / participants | "Today at 2:46 PM", "Yesterday at 4:26 PM"; "Shroud, Shroud, and you" | "2:46 PM", "Nov 6th at 4:26 PM"; no Oxford comma | matches | views |
| 37 | P2 | channel tabs | unread dot | 12px badge (blue dot in a 2px background ring) on the icon of a canvas the viewer never opened | none | from files.info (no `last_read`) | core, ui |
| 38 | P2 | message header | name/time y | name ink 4px above the avatar's top | 6px | 2px lower | messages |
| 39 | P2 | thread pane | empty thread | no "0 replies" divider | divider | hidden | messages |
| 40 | P2 | APP / AGENT badge | label | Slack's BaseAppBadge: AGENT when the bot user's `agent_status` is `consistently_an_agent` (edge users/info carries it) | APP always | AGENT for agents | slack, core, messages |
| 41 | P2 | rail, active To-dos | glyph | filled circle with the check cut out | outline | `todos-filled` | icons |
| 42 | P2 | App Home / app DMs | content / header | blocks at (24, 31) under the tabs, 969 wide; header keeps only bell + kebab; active Home tab icon filled | (20, 16); member count, Huddle, search; outline icon | matches | blockkit, ui |
| 43 | P3 | details dialog | Created by | name in plain text | blue link | plain | views |
| 44 | P2 | dark message bodies, 2× | ink vs Chrome | — | −5…−7% | −0.5…−1.3% (richview `LightEmbolden` 0.34·(1−1/scale): none at 1×) | richview, messages |

Measured equal or within 1–2px: DMs view, Ctrl+K, channel details frame (580×765, tabs, field cards), team menu items (28px rows), the message menu's items, icons and hints, the emoji picker card (361×518 with the Emoji | GIFs row: Slack shows it once the team prefs load, `allow_native_gif_picker`).

Not done in round 2:

- **Search results:** not compared. Slack's Enter in the search box opened the top suggestion's DM (a write; I closed it again), so I stopped there.
- **Block Kit modals:** not compared; they only open from an app interaction (a write).
- **Image viewer:** Slack measured (1384×844 card at 28,28, r 8, 70% black over a blurred copy, 35×32 zoom controls bottom-left); Clack's not opened (no image near the bottom of the test channel).
- **Profile:** a click opens Slack's full profile pane (nested below the tabs, like a thread); not compared field by field. The hover card is from round 1.
- **Emoji picker anchor:** Slack's composer picker sits at x = composer − 3 with its bottom 31px above the button; Clack's is ~55px higher and 18px left.
- **Huddle system rows:** Slack deletes the `huddle_thread` message after the huddle ends, so there was nothing to measure without starting a call.
- **Agent launcher** in the top bar (see above); the Files sort uses "Last updated" (Slack: "Recently viewed", needs view times); Slack's empty-state illustrations are SVGs.
