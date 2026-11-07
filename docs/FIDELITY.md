# Pixel fidelity audit: Clack vs Slack 2026

How this was measured (2026-11-07):

- **Slack:** the real web client in the agent-browser profile `whatever`. I used my own Chrome window, driven over CDP, with a 1440×900 viewport, the workspace's aubergine theme and a 328px sidebar (meclod's saved width). Values come from `getComputedStyle` and `getBoundingClientRect`.
- **Clack:** the CI build of `ui`@4222be3, run on a private 1440×900 Xvfb with the same sidebar width and the text embolden on. Values come from pixel sampling: ink bounding boxes and ink coverage of the same glyphs, and edge scans.
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
| 14 | P2 | channel tab labels | weight/ink | 700 13px/16.25, active #1D1C1D, inactive #454447 | same spec; ink −9% with embolden | <1px | theme | |
| 15 | P3 | rail labels | y | label box top = tile top + 40 (Home label y=149) | 1px lower | 1px | ui (root) | no change: same math as Slack (button 101 + 48); 1px is glyph placement |
| 16 | P3 | message header | name y vs avatar | name ink top = avatar top − 4 | −6 | 2px | messages | |

Measured equal (within 1px), so no change is needed:

- Top nav: 40px; history buttons 26×26 at 299/331/363; search text 13px at +27.
- Rail: tiles 52×68 (Agents 80) starting at y=101; active tile 36×36 r=8 `rgba(249,237,255,.25)`; labels 700 11px/12.
- Sidebar: header 49px, team name 900 18px at (87, 53.5), buttons 28×28 at 328/362, filter 311×28 at (79, 90); rows 28px with 8px padding, 16px icons at x=87, labels at x=111; the selected row's radius is 6.
- Message rows: avatar 36 at (16, 8) r=8; name 900 15px/22 at +60; timestamp 400 12px #616061 7.7px after the name; body 15px/22 at +26; consecutive row stride 52.
- Composer: 998×118 at (418, 754), r=8, border `rgba(94,93,96,.45)`, toolbar 38px #F8F8F8, footer 40px, 28px buttons.
- Ctrl+K: 650px wide at x≈390, rows 40px, footer hint row.

Not fixed here, because the difference is data or a feature rather than a style:

- Slack's onboarding row ("2 tasks left") and the top-bar avatar/status button appear only in some sessions.
- Huddle system rows ("You joined the huddle…") are Slack's own render of `huddle_thread` messages; that belongs to the huddles agent.
- The AGENT badge on agent bots: Clack labels every bot APP.
