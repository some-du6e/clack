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
| 12 | P2 | channel tab icons | glyphs | Messages: filled bubble when active; list: checklist card; canvas: rounded sheet with folded corner | outline bubble; bullets; plain document | glyph | icons + ui (root) | Messages + list fixed (ae92fb0); unread dot on tabs fixed ed1a27bb (round 2, row 37) |
| 13 | P2 | sidebar icons | glyphs | Directories: contact card with lines on the left; Add channels: "#" plus a small person in a rounded tile; DM section: two round bubbles (as the rail) | people icon; "#+"; square bubbles | glyph | icons | fixed ac4c59a |
| 14 | P2 | channel tab labels | weight/ink | 700 13px/16.25, active #1D1C1D (dark **#F8F8F8**), inactive #454447 | same spec; ink −9% with embolden; dark active #D1D2D3 | ink, colour | theme + ui (root) | fixed 52b26ed (weight), 2b1831c (dark colour) |
| 15 | P3 | rail labels | y | label box top = tile top + 40 (Home label y=149) | 1px lower | 1px | ui (root) | no change: same math as Slack (button 101 + 48); 1px is glyph placement |
| 16 | P3 | message header | name y vs avatar | name ink top = avatar top − 4 | −6 | 2px | messages | fixed (round 2, row 38: name ink 4px above the avatar) |
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

## Text on Windows (branch `text-render`, 2026-11-11)

Karim, Windows 11, dark mode: text looked washed out. font-shots' `text-render-linux` and `text-render-windows` jobs draw Slack's message (15px), sidebar (13px) and meta (12px) text, 400 and 700, both modes, at 1, 1.25, 1.5 and 2×, through Gio (Mesa llvmpipe; D3D11 on the Windows runner) and through headless Chrome 156 (and Edge 156) in the same embedded Lato (`TestTextRenderShots`, `.github/scripts/text_render_compare.py`). Per cell they measure the total ink and the peaks of strokes along rows (stems) and columns (bars), in sRGB-space ink (0 at the background, 1 at the text colour). Ink alone hides the problem: the earlier tuning (the section above) matched it.

What differs on Windows:

- **Not the framebuffer.** D3D11 renders in linear light into an sRGB target like the other backends: half-alpha white over black is 188 in the headless target and in a real window's swapchain read back from the screen (`TestD3D11WindowSRGB`). Gio's D3D11 (WARP) and llvmpipe pixels differ by at most 13 levels on a few pixels.
- **Gamma and contrast.** Chromium's Windows build sets `SK_GAMMA_SRGB` and `SK_GAMMA_CONTRAST=1.0` (`skia/BUILD.gn`). Skia's mask-gamma table adds `contrast·toLuma(1-src)·a·(1-a)` to dark text's coverage, then blits it in sRGB (`SkMaskGamma.cpp`). DirectWrite masks go through it, grayscale and ClearType alike. On Linux, grayscale FreeType masks skip the table (`ignorePreBlend` without `SK_GAMMA_APPLY_TO_A8`), so Clack's legacy fade and embolden, tuned against Linux Chrome, were fitted to a different browser.
- **Hinting.** Lato's `gasp` (v1, flags 15) makes DirectWrite grid-fit it vertically (`NATURAL_SYMMETRIC`), so Chrome's horizontal strokes are crisper. Gio doesn't hint.
- **Smaller ones.** The runner's Chrome draws ClearType (Edge draws identically); `--disable-lcd-text` gives 1.5–3% more ink. Slack's own CDN Lato (re-hinted by ttfautohint 1.8.1) differs from the embedded one by about ±3% on Windows and not at all on Linux.

Clack as shipped vs Chrome on Windows (ClearType), per mode means; stem and bar are peak differences:

| | dark 1× | dark 1.5× | light 1× | light 1.5× |
|---|---|---|---|---|
| legacy model (fade, embolden) | ink 95.8%, stem −0.111, bar −0.107 | 100.1%, −0.100, −0.057 | 95.7%, −0.046, −0.085 | 97.5%, −0.050, −0.028 |
| Skia's model (now on Windows) | 108.1%, −0.030, −0.022 | 106.3%, −0.045, +0.000 | 100.1%, −0.019, −0.047 | 100.3%, −0.036, −0.008 |

Dark timestamps (`#ababad`) were the worst under the legacy model: 83% of Chrome's ink with stems 0.20 below at 1×. Under Skia's model they are at 112% with stems −0.021.

The Windows model (`theme/textgamma.go`): no fade and no embolden. Skia's contrast is a second paint of the same glyph coverage at alpha k, which is Skia's `apply_contrast` exactly in Gio's linear blend. In light mode, dark ink's k is fitted to Chrome's sRGB blit of the table over white, because the table assumes a grey background for grey text. With Skia's own k, `#616061` timestamps came out at 87% of Chrome's ink and stems −0.074; fitted, 99.8% and −0.003. Linux and macOS keep the legacy model, byte for byte.

Left over on Windows: dark-mode light text carries 2–11% more ink than Chrome's, with stems 0.03–0.045 lower. Gio's unhinted edges spread wider, and the table's guessed background is lighter than Slack's `#1a1d21`, which a second paint can't undo. Bars at 1× are 0.02–0.05 lower (hinting). On Linux the legacy model shows the same pattern, milder: dark-mode stems −0.06 to −0.08 on average and timestamps −0.13 to −0.14. Skia's model would track Linux Chrome's stems better there too (worst cell −0.035 to −0.047). macOS wasn't measured.

`TestTextMatchesChrome` holds the Windows model to Chrome-on-Windows screenshots at 1× and 1.5×, and the legacy model to Chrome-on-Linux at 1× (`theme/testdata/text-render`). It runs in ci's pixel step and in both text-render jobs.

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
| 30 | P2 | Huddles page | banner text / cards | HeroBanner's CSS: the text column 60% (padding 44px 0, 44px left up to 1124 wide), Slack's 360px picture; DM cards 360 wide (136px header); live and suggested cards 200 tall in a 1/2/3-column grid (docs/HUDDLES-AUDIT.md, "The Huddles page") | +48/+48; 258 tall | matches | views |
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

- **Search results:** compared on 2026-11-08 (search-audit), see docs/SEARCH.md. Don't press Enter in Slack's search box: it opens the top suggestion, once a stranger's DM. Click "Show results for:" instead.
- **Block Kit modals:** not compared; they only open from an app interaction (a write).
- **Image viewer:** Slack measured (1384×844 card at 28,28, r 8, 70% black over a blurred copy, 35×32 zoom controls bottom-left); Clack's not opened (no image near the bottom of the test channel).
- **Profile:** a click opens Slack's full profile pane (nested below the tabs, like a thread); not compared field by field. The hover card is from round 1.
- **Profile hover card:** resting on a message author's name or avatar (threads included) or an @mention opens it after Slack's 350ms; leaving closes it after 250ms unless the pointer reaches the card; its bottom-left sits 12px above the trigger, flipped below near the top (internal/ui/hovercard, from Slack's HoverCardTrigger/ProfileHoverTrigger/Popover source).
  - **Where:** Slack registers ProfileHoverTrigger as `renderProfileHoverCard` (client-boot QvaD `yN`, boot-render 26v2 `CZ.yN.register`). Its callers, read from the CDN chunks (2026-11-08): MessageSender (/OeL, also search results), BaseAvatar when `showCard`, interactive and ≥ 36px (message and search-result avatars, the DM foreword's 56px avatars), MemberSlug mentions, MediumMemberEntity with `showHoverCard` (the name only: channel details Members, channel managers, the apps tab), Activity's reaction headers (ActivityItemMember), system-message rollups, and file/canvas/list owners. Clack wires: message names and avatars (and a bot-only sender, through its `bot_id`), mentions, search-result names and avatars, channel details Members names (the card draws above the dialog), and Activity reaction rows. Slack has none on reaction pills or their tooltips, the sidebar's DMs, thread reply-bar facepiles (24px, not interactive) or huddle participants, so neither does Clack. Clack's conversation intro has no avatars, and it doesn't draw join rollups.
  - **Apps and bots** get Slack's AppHoverCard (SmYd/f0+8, internal/ui/popups/app_card.go) from `apps.profile.get`, mapped like Slack's ToML. It has a 320px card with the app's color and gradient header, a 64px icon, the name with an inverse APP badge, the tagline, then About this app, Configuration, Add this app to a channel… and Add to VIP. Up to three slash commands follow (a click types the command into the composer), then the installation summary in Slack's wording ("X is installed on <team>." when the home team is known, else "@installer added X to this workspace."), then Go to App. Not done: "Remove this app from #channel" and hiding Go to App inside the app's own DM. Add to a channel and VIP only show a toast.
  - **Slack Connect:** core.User carries Slack's computeLocality (TeamID, External) plus `is_stranger`, `bot_id` and `api_app_id`. An external member's card shows AppExternalTeamBanner (team icon with a white ring, bold team name, from `team.info`) above the owner/admin banner, and their presence is the guest square. Another org's app shows the dark banner across its header and loses Configuration and slash commands.
  - **Slackbot's presence:** core marks USLACKBOT as a bot, and the card hid presence for every bot. Slack's ConnectedPresence draws Slackbot's presence with `isSystemUser` (the `status-slackbot-filled` heart, colored by its presence), so Clack now does too. Also, `core` never read the user object's own `presence` field, which is why the fixture's "active" never showed.
- **Emoji picker anchor:** Slack's composer picker sits at x = composer − 3 with its bottom 31px above the button; Clack's is ~55px higher and 18px left.
- **Huddle system rows:** Slack deletes the `huddle_thread` message after the huddle ends, so there was nothing to measure without starting a call.
- **Agent launcher** in the top bar (see above); the Files sort uses "Last updated" (Slack: "Recently viewed", needs view times); Slack's empty-state illustrations are SVGs.

## Round 3: Taut's and Slick's settings pages (2026-11-08, branch `plugin-prefs` from `ui`@3d0cb21)

Karim said Preferences > Taut and Preferences > Slick "don't look very native". Neither page exists in Slack, so the reference is each project's own settings UI: Taut's React components and CSS at `jeremy46231/taut@6fe5123` (Clack's `TAUT_PIN`, files under `app/settings/`) and Slick's `src/app/settings.tsx` at `3kh0/slick@29498a0` (main; its settings UI is the same at the `SLICK_PIN` f52d55d for everything cited here). Where those UIs use Slack's own components (`elements.Checkbox`, `FilterPill`, `Button`, `FormTextInput`, `MrkdwnElement`, `c-switch`), Clack uses its existing native equivalent: the 16px checkbox and FilterPill in `taut_prefs.go`, `popups.Button`, Slick's `c-switch--small` in `slick_prefs.go`, and richview (Clack's MrkdwnElement, the one messages use) for mrkdwn text. Colours are Slack's tokens, light and dark, as before: `--dt_color-content-sec/ter`, `--dt_color-otl-*`, `--dt_color-surf-pry`, and the member-mention pill from richview's palette (`#1264A3` on `rgba(54,197,240,.13)` light, `#1D9BD1` on `rgba(29,155,209,.1)` dark).

Checked against the ui-taut screenshots (`ui-taut-shots` artifact; synthetic data). Most of the Slick pages already matched their source; the gaps were on Taut's sub-screens and in the Library, which neither upstream has.

**Authors.** Neither upstream draws its own author pills. Taut writes authors as mrkdwn and lets Slack's `MrkdwnElement` draw them: in the Hack Club Slack each author is a member mention (`<@slackId>`, which Slack draws as its blue mention pill), anywhere else a name, linked when the author has a page, joined with ", " (`app/settings/common.tsx:122-166`, used at `pluginDetail.tsx:88-106` and `screens.tsx:663-668`). Slick prints `By {info.authors}` as plain text in its 13px tertiary meta row (`src/app/settings.tsx:1202`, style `:368-376`). Clack now does exactly that: mentions (as Slack's own pill, through richview) only in the Hack Club Slack on Taut's pages, names and links elsewhere, plain names on Slick's.

| # | screen | element | upstream (file:line) | Clack before (ui@3d0cb21) | after |
|---|---|---|---|---|---|
| 45 | Taut, plugin page | meta line | one `MrkdwnElement` in `taut-settings__small` (13px, content-ter): `Category · by <authors> · user plugin \`Id\`` (`pluginDetail.tsx:97-107`, `common.tsx:46-49`) | plain 13px text; authors "Jeremy and Rowan", never linked; id as plain text; an extra "· Hack Club only" | richview mrkdwn at 13/18 in content-ter: Taut's author rule (above), `Id` as inline code, no Hack Club item |
| 46 | Taut, About | credits | `*Name* by <authors>` per plugin, `MrkdwnElement` at body size in content-pry, 6px apart, every plugin (`screens.tsx:660-670`, `:371-380`) | "Name by a" in content-sec, no gap, only plugins with authors | bold name + authors through richview, content-pry, 6px gap, every plugin |
| 47 | Taut, About | intro | `SlackText` (content-pry) with a mention and a channel link (`screens.tsx:656-659`) | content-sec plain paragraph | content-pry mrkdwn: the Taut link, Jeremy (a mention in the Hack Club Slack, labelled "Jeremy" until the member loads, like Taut's `authorBySlackId`; a link elsewhere), #taut; still says Clack runs Taut plugins on its own implementation |
| 48 | Taut, Appearance | editor | storage note "Saved to \`…\`." (muted, 20px below), editor 420px tall, `Button type="outline" size="small"` "Save user.css", disabled until edited, "Discard" while edited, "Unsaved changes" / "Saved" at the right (`screens.tsx:420-441`, `common.tsx:347-391`, bar `:84-90`) | 320px editor, primary "Save" always on, "Saved." note beside it | 420px, outline small "Save user.css" (off until edited), Discard, Saved / Unsaved changes at the right; note with the path as inline code |
| 49 | Taut, Advanced | config.json | storage note, 420px DocumentEditor, "Save config.json" outline small with the same bar (`screens.tsx:609-641`) | 300px editor, primary "Save config.json" | 420px, Taut's bar; Clack's extra "Reload from disk" stays as an outline button in it |
| 50 | Taut, Advanced / About | sections | stacked `FieldSet`s, no rules between them (`screens.tsx:530-642`, `:655-671`) (the plugin page does use `<hr />`, `pluginDetail.tsx:140,267`) | `<hr>` between sections | 28px between FieldSets (Slack's FieldSet margin not measured), the plugin page keeps its rules |
| 51 | Taut, main | row caret | `SvgIcon name="caret-right"` (`pluginList.tsx:298-300`, `index.tsx:295-297`) | chevron | caret-right, the same glyph Slick's rows use |
| 52 | Taut, main | Appearance icon | `paintbrush` (`index.tsx:257-262`) | pencil (`edit`) | paintbrush |
| 53 | Taut, main | "User plugins" filter | shown only when some plugin is a user plugin (`pluginList.tsx:214-218`) | shown whenever any plugin is installed | only with a user plugin |
| 54 | Slick, plugin page | back button | `slick-settings__back`: 700, link colour, 6px icon gap (`settings.tsx:335-346`) | Taut's 400 link, 4px gap | 700, 6px |
| 55 | Library | screen | (neither upstream has one) | two copies: Taut > Library (Taut's link-row look, legends, plain 12px "Hack Club only · disabled upstream" text tags) and Slick > Library (the same view under Slick's tabs) | one "Plugin library" tab drawn with Slick's parts, see below |

Measured equal before and left alone: Taut's header, nav rows, search box, filter pills, group titles and plugin rows (`index.tsx:56-76`, `pluginList.tsx:15-96`); Taut's plugin page title, back link, Turn on checkbox, Settings and Data sections; every Slick tab: the hero (`settings.tsx:94-132`), cards and fields (`:141-186`, `:377-410`), the sticky toolbar, search and chips with counts (`:187-246`), plugin rows with the `User` badge, the switch and the caret (`:247-299`), the detail header with its badges (`:347-376`), notices, empty states, theme cards (`:433-521`) and the install panel (`:1262-1356`).

**The Library.** It's Clack's own (see PLUGINS.md), so it borrows the parts of the UI it sits beside: Slick's section head with the actions at the right ("Official plugins" | Update all (n) | Refresh, like Themes' "Theme" | Import theme JSON…, `settings.tsx:1955-1961`), a 13px secondary intro, Slick's sticky-style toolbar (search with the 16px icon at 10px, 34px inset; chips All / Taut / Slick / Installed / Updates with counts, `:199-246`), and one card per category with Slick's category icons (`:766-776`) in Taut's and Slick's category order. Each row is a Slick plugin row (`:247-277`) with the switch replaced by the action button: name 15/700 with a `Taut` or `Slick` badge (Slick's 20px 11/700 badge, `:286-299`) plus `Hack Club only` / `Off upstream` (warn) badges, the description clamped to two lines, "By …" in 13px tertiary, the last result in Slick's 12/700 status style (`:306-314`), and Install / Update (primary small) or Settings (outline small), which opens the plugin in its own system's tab.

## Drop to upload (branch `gap-w1-drop`, 2026-11-11)

Files dragged onto the window from Explorer, Finder or a file manager.
Sources: CDN build 4d0c78085232, fetched without a cookie: boot-shared
`oCSS` (`FileDragDrop`), `cCrv` (`MessagePaneDragOverlay`), `6Uf7`
(threads `DragOverlay`), `HgMH` (`KeyboardKey`), `Tmmu`/`lVUj` (what counts
as a file drag), `qwL3` (`disable_file_uploads`), and client-boot-styles'
`.p-message_pane_drag_overlay*`, `.p-thread_drag_overlay*` and their
keyframes. The sprites are Slack's (`dragdrop_sprites@2x-ca59764.png`,
`dragdrop_threads_sprites@2x-f3aaf0a.png`,
`emptystate-blockedfiles@1x-6b3ea84.png`), embedded. Clack's events come
from Gio patch 11 (third_party/gioui.org/CLACK_PATCHES.md §11).

| element | Slack | Clack before | Clack now |
|---|---|---|---|
| drop target | one `FileDragDrop` per pane: MESSAGE_PANE around the primary view body (list and composer), THREAD around the thread flexpane body (not its header); each its own drag state | none (Gio had no OS drop events) | each convo `layoutConvo` lays out (channel, split card, thread) registers its window rectangle, a thread's from under its 49px header; the one under the pointer shows its overlay |
| disabled pane | `disabled: G\|\|$` on the message pane: no overlay, no drop | n/a | a preview or archived channel (its footer replaces the composer) registers no zone |
| overlay box | z-index 200, `inset: -1px 0 0`, `rgba(var(--sk_primary_background), .95)`, flex-centred | n/a | the same, drawn as the pane's last layer (white light, #1a1d21 dark at .95) |
| entrance | message pane: `messagePaneDragOverlayOverlayTransition` .12s ease (background 0 → .95, opacity 0 → 1); thread: `threadDragOverlayFadeIn`, which the CSS never defines (no fade) | n/a | the same; reduced motion draws the last frame |
| message pane cards | 171×171 `__anim`: `__anim_circle` (#00000003, scale .5 → 1), `card_03` (92×77 at 35,50; scale .5 rotate 5° → translateY(-30)), `card_02` (92×77 at 25,55; rotate 15° → translate(-20)), `card_01` (100×89 at 35,62; rotate -10° → translate(30)); .12s ease-in-out; the night `card_01` (0 -96) from 20:00 to 08:00 | n/a | the same pieces cut from the @2x sprite, CSS's `scale() rotate() translate()` about each box's centre |
| thread cards | 110×155: `text_01` (top 3), `card_01` (top 30 left 10, night 0 -125), `text_02` (top 112); threadTextAnim scale .85 → 1 and 10px down, threadCard1Anim .5 → 1 and 10px down, fading in | n/a | the same |
| title | 28px / 1.2143, weight 900, 16px above (thread 8), 8px below; "Upload to {teamName}" (`getTeamDisplayName`), thread "Upload to thread" | n/a | the same |
| subtitle | 15px / 1.46668, `--dt_color-content-sec` (thread `rgb(var(--sk_foreground_max_solid))`), wraps; "Hold [Shift] to share immediately to {channelName}" (`#name`, a private channel bare, "with {userName}" in DMs), thread "Hold [Shift] to share immediately" | n/a | the same, word-wrapped around the chip; padding 0 16px (thread 0 50px) |
| Shift chip | `KeyboardKey` slim: 20px tall, min 20 wide, 4px inside, margins 0 4px 4px, `base.tertiary` fill, 1px `tertiary` bottom border, radius 4, caption text | n/a | the same (13px text, as views/shortcuts.go measured the slim chip) |
| read only | "Read only" / "Only selected team members can post in this channel." (thread: "… in this thread."), the circle and `emoji-objects` at 50px; the drop does nothing | n/a | the same, from the convo's posting restriction (read-only, thread-only, non-threadable) |
| uploads disabled | `disable_file_uploads`: "disable_all" shows "Uploads disabled" with the blocked-files picture and drops nothing; "disable_all_except_images" says "Uploads disabled" over the cards | n/a | the same, from the team pref (`allow_box_cfs` allows all) |
| drop | `uploadDraftFiles` (the pane's draft); with Shift `uploadFiles` with `shareData` (shared at once, draft untouched) | n/a | the pane's composer `AttachPaths` (the + menu's attach path: folders and over-limit files refused with its toasts); Shift `SharePaths`: `core.Upload` to the conversation |
| dropEffect | "copy" on dragenter/dragover | n/a | the window answers copy (DROPEFFECT_COPY, NSDragOperationCopy, XdndActionCopy, wl copy) |
| Esc | the OS ends the drag: dragleave | n/a | the same (DragLeave / draggingExited / XdndLeave / leave) |

Not done: the huddle window's `HuddleDragOverlay`; Slack Connect's
`can_upload_files` ("Can't upload file", "Your admins have disabled file
upload in this Slack Connect conversation."); locked threads ("Locked
thread"); Block Kit drags (`dragKind` "block-kit"); a drop on a message
being edited (`messageEditUploadNewFiles`, the files go to the draft
instead); the Later, Files and canvas drop targets. Tests:
`internal/ui/drop_overlay_test.go` (the overlay follows the pane, leaves,
the pane's composer gets the files, Shift shares them, an archived
channel takes nothing), `third_party/gioui.org/app/clack_drop_test.go`,
the windows-applog job's real OLE drag from another process
(`clack_drop_windows_test.go`) and the linux job's XDND drag on Xvfb
(`clack_drop_x11_test.go`). Screenshots: files-shots' `drop-*.png`.

