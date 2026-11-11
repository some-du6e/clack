# Slick plugins and themes, live in Clack

Live verification of every Slick plugin (https://github.com/3kh0/slick at `f52d55d`), its 10 bundled themes and custom CSS, against the real Hack Club Slack (2026-11-07). Companion to [PLUGINS-slick.md](PLUGINS-slick.md), which covers the API and the CI acceptance suite.

## How it was tested

- **Bundles:** the `slick` workflow's `slick-plugins` artifact: 53 renderers built by Slick's own build, plus 17 `<id>.main.js` halves. Every file was imported unmodified through **Preferences > Slick > Install plugins > From a URL**, served from `127.0.0.1` (allowed: https or localhost). Each installed file is byte-identical to the artifact (`cmp`).
- **Plugin Library:** the `plugins` release that live-taut's `plugin-library` workflow builds. All 72 Slick assets match their `index.json` sha256. The 70 that are also in the artifact are byte-identical to it; `SelfCare` (two files) is new upstream (`2896741`).
- **App:** the `clack-linux-amd64` artifact of `ui`, then of `live-slick` after each fix. One instance, a private Xvfb, `CLACK_NOEMBOLDEN=1`, `CLACK_HOME` scratch copy, `xset r off`, driven with xdotool. Screenshots stayed in /tmp and were deleted.
- **Slack:** only channel C0B4DEFKFCJ. Composer tests typed there without sending; the synced draft was cleared afterwards.
- **Not run live, by policy:**
  - MessageLogger: it stores deleted message contents on disk, which the Hack Club scraping policy forbids.
  - OfficeHours: it changes your own presence and status.
  - bChannel: it posts and invites.

  These rely on the `slick` acceptance suite (CI) and source review.
- **Source review:** each plugin's source was read against Clack's API, patch points, CSS engine and mirror before testing, to know what to look for.

Legend: ✅ works as in Slick · ⚠️ partly (what's missing is listed) · ❌ no visible effect in Clack (why). **Verified** says how: *live* is the 2026-11-07 run on real Slack; *CI* is the `slick` acceptance suite (internal/slick) or the `ui-slick` view tests (`TestSlickView*`, the real native views over a synthetic workspace) on `plugins-slick-pass` (2026-11-11), with no live run since.

**Status:** live testing stopped on 2026-11-07 (Slack revoked sessions of an Enterprise Grid account using Clack). The 2026-11-11 pass (`plugins-slick-pass`) re-checked every ⚠️/❌ row against its source and Clack's code and marks what CI now proves; nothing below was re-run live.

## Plugins

| Plugin | Result | Verified | Notes |
|---|---|---|---|
| AccountSwitcher | ⚠️ | CI (`TestSlickViewAccountSwitcher`) | "Switch account" and "Add another account" sit before Sign out in the user menu. Switching moves Slack session cookies between accounts; Clack has one session, so it goes through Clack's sign-in. |
| AdminBackend | ✅ | live | "Open in Identity / Telescreen / Fire Engine / Attend" in the profile ⋮ menu. |
| AnonymiseFileNames | ⚠️ | CI (`TestSlickViewAnonymiseFileNames`) | A file attached in the composer uploads under a random name (extension kept), and the chip's toggle switches it between its original and anonymous name. Its `FileFieldset` and `EditFileDialogTitle` patches have nothing to wrap: Clack has no edit-file dialog. |
| bChannel | ✅ | CI (`TestSlickViewbChannel*`) | Its /bchannel relay, the @ rows offering every broadcast keyword, its confirm step and the relayed send. Not run live (it posts and invites). |
| BetterBlock | ✅ | CI (messages, popups, composer, views, shell `TestSlickViewBetterBlock*`) | Blocked people's messages, thread replies, reply-bar avatars, mentions, reactions, profile card, search, @ rows, Activity, DMs, People and sidebar DMs. Notifications go through its `showNotification` patch. |
| BetterGifs | ✅ | CI (`TestSlickViewBetterGifs`) | Favourite stars on Giphy items in the GIF grid (GifListItem inside the GifPicker boundary). |
| BetterHuddles | ❌ | — | Out of scope (huddle media): it replaces Slack's call with its own Chime/WebRTC client, which jsrt can't run. Its join-preview wrapper applies (`TestSlickViewBetterHuddles`). |
| BetterTypingIndicator | ✅ | CI (`TestSlickViewBetterTypingIndicator`) | Names on one 10.5px line in the typing bar. |
| CallTimer | ✅ | CI (`TestSlickViewCallTimer`) | The timer in the huddle window's title and the bottom bar. Not run live (no live huddles). |
| Censorship | ✅ | CI (acceptance check; `TestSlickViewCensorshipActivity`) | Masked in channels, threads and the Activity feed. Search cards now show its masked copy too (`MessageListItem`'s result, `search_view.go` `overText`), not yet covered by a view test. Notifications go through its `showNotification` patch. |
| ChannelListTyping | ✅ | CI (`TestSlickViewChannelListTyping`) | The typing dots at the right of a sidebar row while someone types there. |
| CharacterCounter | ✅ | live | "43/40,000" beside the send button, following the text. |
| ChatTabs | ✅ | CI (`TestSlickViewChatTabs`, `TestSlickViewHaikuWarningWithChatTabs`, `TestSlickViewMissedFrameChatTabs`) | Its tab bar inside the primary view, "Open in new Slick tab" in the channel menu, Ctrl+W through its main half. The send it blocked with HaikuWarning on is fixed (`815933f`). |
| ClearURLs | ⚠️ | CI (acceptance check) | Tracking parameters go on send (its main half fetches the rules). Its paste-time cleaning patches Quill's Clipboard module, which Clack's native composer doesn't have. |
| Click2Load | ❌ | — | It gates iframes; Clack never creates any. |
| CopyReacted | ⚠️ | CI (`TestSlickViewCopyReacted`) | In channels its copy button follows the add-reaction button (ReactionAddButton inside ReactionBar's tree). The Threads, Unreads and Activity views have Slack's Reaction boundary now but no ReactionBar or add-reaction button, so its button isn't there. |
| CustomFonts | ❌ | — | Its stylesheet installs, but Clack draws its bundled Lato and doesn't load `@font-face` fonts. |
| CustomNameRecording | ⚠️ | — | Its button shows on your own profile; `decodeAudioData` isn't supported, so nothing uploads. |
| CustomSlackbot | ✅ | CI (`TestSlickViewCustomSlackbot`) | Its avatar and name on Slackbot's custom responses (`subtype: slackbot_response`) and in the reply bar. Clack draws no custom-response label, so its rule hiding that badge has nothing to hide. |
| CustomSounds | ❌ | — | Clack plays no notification sounds through JS; its `load()` returns for `loader === 'electron'`. |
| DeArrow | ✅ | CI (`TestSlickViewDeArrow`) | Crowdsourced title and thumbnail on YouTube unfurls, with its toggle. |
| Experiments | ✅ | CI (`experiments` workflow `TestExperimentsPluginsDriveClack`, acceptance check) | A group forced in its tab drives Clack (sidebar_section_search gates the section menu's "Search in this section"); turning it off gives the server's group back. Never set on the real account. |
| HaikuWarning | ✅ | live (alone), CI (with ChatTabs) | "Found a haiku in your message!"; Keep editing returns the text. |
| HcaStatus | ⚠️ | CI (acceptance check) | The auth.hackclub.com lookup works and its class reaches the sender. Whether its name underline draws on Clack's native sender isn't covered by a test. |
| HuddleMuteDetector | ❌ | — | Out of scope (huddle media). |
| HumanCount | ✅ | live | The header count (17, same as Slack). |
| IgnoreTime | ✅ | CI (`TestIgnoreTimeLeavesTimesOutOfCopies`) | Its `.c-timestamp__label {user-select: none}` now keeps the times out of a multi-message copy's header lines, as in Slack. |
| InstalledApps | ⚠️ | CI (`TestSlickViewInstalledApps`) | Its block sits in a person's full profile and around an app's AboutPage. Its main half's `session.fetch` sends no Slack cookie (security policy), so the marketplace page asks to sign in. |
| LastSeen | ✅ | CI (`TestSlickViewLastSeen`) | "Last seen …" on the profile card and in the full profile's presence row. |
| LetMeSee | ⚠️ | CI (`TestSlickViewLetMeSee`) | Out of scope for live (huddles). Its points render; "Hide drawings" clones Slack's mini-panel DOM, which a native panel doesn't have. |
| LocalTime | ✅ | live (messages), CI (`TestSlickViewLocalTime`) | The sender's local time after the timestamp; the profile card and the full profile's clock row. |
| MentionProfiles | ✅ | CI (`TestSlickViewMentionProfiles`) | Pictures in @mention pills (MemberSlug); without the @ when its setting says so. |
| MessageLogger | ✅ | CI (`TestSlickViewMessageLogger*`) | Deleted messages stay, marked; edits are marked; "Accept deletion" in the ⋮ menu. Not run live (it keeps message contents on disk). |
| Nicknames | ✅ | live | "Set nickname…" → the row's sender and popups show the nickname. |
| NoTrack | ✅ | CI | Telemetry fetches are blocked. Nothing visible, as in Slack. |
| NotShitMarkdown | ✅ | CI | Pasted Markdown is converted on send. |
| NoUpsell | ✅ | live | Clack draws no upsells. |
| OfficeHours | — | CI (acceptance check) | Not run live (it changes your presence). |
| oneko | ⚠️ | CI (acceptance check) | The cat element and its mousemove listener work; the cat isn't drawn (its sprite is a `data:` URL moved with pixel `background-position`). |
| OnlineCount | ❌ | — | Needs Slack's flannel `query` export and presence subscriptions for up to 950 users. |
| PrivateChannelMapper | ⚠️ | — | Autocomplete rows keep their `.promise` tier (`d69711c`); the missing-channel pill never triggers. |
| QuickJoin | ✅ | CI (`TestSlickViewQuickJoin`) | A Join button under a channel you can join. |
| QuietSpotify | ❌ | — | No Spotify embeds or frames in Clack. |
| RestrictedChannelWarning | ✅ | CI (acceptance check) | "Post anyway" in the roadblock of an admin-only channel. Not testable live (needs such a channel). |
| SelfCare | — | — | New upstream (`2896741`), after the pinned Slick; not reviewed in this pass. |
| ShowRealUser | ⚠️ | — | Direct relay posts work; forwarded copies carry a numeric `ts`. |
| ShowSendingBot | ✅ | CI (`TestSlickViewShowSendingBot`, messages and shell) | The bot's badge on the avatar of a message a bot posted for a person. |
| ShutUpSlackbot | ✅ | CI | Slash-command notices are dropped, other notifications shown. |
| SilentTyping | ✅ | live | Its composer toggle. |
| SlimMessageBox | ✅ | CI (`TestSlickViewSlimMessageBoxOneLine`, `…HidesEmoji`) | The hide toggles, and now the Discord-style one-line layout: its row-flex `.c-basic_container__body` puts the footer beside the editor (composer `layoutBox`). Unlike Slack, pending attachments don't stack it again (Clack's natives answer `:has()` with false), and the editor's 450px min-width alone decides when it wraps (`@container` queries always apply). |
| Snappy | ✅ | CI (`TestSnappyInClack`, `TestSlickViewSnappy`) | See [Snappy in Clack](#snappy-in-clack). |
| StreamerMode | ⚠️ | CI (acceptance check) | Notifications are silenced and its redaction stylesheet installs. Its blur (`filter: blur`) applies to natives that wrap their CSS, but no view test checks the sidebar, Activity or DM rows yet. |
| UserPronouns | ✅ | CI (`TestSlickViewUserPronouns`, acceptance check) | Pronouns after the timestamp in channel rows (BaseBroadcastPreamble), once per run of grouped messages, and in thread headers. |
| WhoReacted | ✅ | CI (`TestSlickViewWhoReacted`, `TestSlickViewWhoReactedInViews`) | Reactors' avatars in reaction pills, in channels and now in the Threads, Unreads and Activity views (their pills are Slack's Reaction → UnstyledButton). |

## Snappy in Clack

Snappy (src/plugins/Snappy) makes Slack feel faster by collapsing CSS transitions and turning off slow features. Each part does the same in Clack (CI: `TestSnappyInClack`, `TestSlickViewSnappy`):

| Snappy does in Slack | Clack does |
|---|---|
| **Always:** scans `document.styleSheets` and overrides every rule that declares a transition with `transition-duration: .01ms; transition-delay: 0s` (transitions.ts). Keyframe animations aren't touched. | The page has Slack's stylesheet for the transitions Clack draws natively (`slackc.NativeTransitionsCSS`, Slack's own rules, also in the CSS engine's base sheet), with a real CSSOM (jsrt). Snappy's override reaches the CSS engine, and the natives read their timing from the cascade (`Styled.Transition`): the thread pane's read lines dim at once instead of 3 s later, the channel join banner and agent plan/task blocks stop animating. |
| `optimizeSelectors`: rewrites rightmost `:is()` selectors so Chromium can index them. | Nothing to do: Clack's engine already indexes a rightmost `:is()` of classes under each class. Said in its Preferences description. |
| `optimizeDuplicateStyles`: disables duplicate `<link>` stylesheets. | Nothing to do: the page has one copy of Slack's stylesheet. Said in Preferences. |
| `optimizeResize`: holds Slack's `resize` listeners and ResizeObservers while a window edge is dragged, then replays once. | Slick's `deferResizeWork` now holds ResizeObservers too, not only `resize` events. Clack's own layout is native and follows the window every frame. |
| `optimizeStateTracking`: opts Slack's Redux code into two fast paths at boot. | Nothing to do: Clack's views never run Slack's Redux code. Said in Preferences. |
| `disableSpellcheck` (main half): `session.setSpellCheckerEnabled(false)`, live. | The composer stops spellchecking (Electron's session spellchecker is Clack's), and checks again when it's off or Snappy stops. |
| `disableCrashReporter` (default on, main half): Chromium's `disable-crashpad` and `disable-crash-reporter` at boot. | Clack's main halves' switches are kept for the next start (`slick/switches.json`); with these, Clack doesn't capture crashes into crash.log. |
| `ignoreGpuBlocklist` (main half): Chromium's `ignore-gpu-blocklist`. | Gio's one GPU blocklist is X11's Vulkan (NVIDIA drivers): at the next start an X11 window tries Vulkan first (`app.IgnoreGPUBlocklist`, CLACK_PATCHES §4). |


## Themes and custom CSS

Clack converts theme JSON exactly as Slick's `themeToCss` does. Until `279f419` + `a4aa0ac` the native views ignored the tokens, so only the composer, menus and hovered rows changed. Now chrome, sidebar, header, message list, text, links and hover follow the stylesheets' Slack tokens. With no theme set, the palette is unchanged.

| Theme | Result | Measured live on `6da798a` (pane background / sidebar / window) |
|---|---|---|
| Catppuccin Frappé | ⚠️ | `#303446` / `#292c3c` / `#232634`: all as in the JSON |
| Catppuccin Macchiato | ⚠️ | `#24273a` / `#1e2030` / `#181926` |
| Catppuccin Mocha | ⚠️ | `#1e1e2e` / `#181825` / `#11111b` |
| Gruvbox Dark | ⚠️ | `#282828` / `#32302f` / `#1d2021` |
| AMOLED | ⚠️ | black chrome, sidebar and pane |
| Hot Dog Stand | ⚠️ | red chrome, yellow sidebar and header. Message list pending a re-shot after `a4aa0ac`. |
| i hate myself | ⚠️ | its raw CSS restyles Preferences (borders, tilted Done button). Message list pending a re-shot after `a4aa0ac` (before it, dark text sat on Slack's dark list). |
| Material, Nord, Verdant | pending live | covered by `internal/ui/theme_tokens_test.go` (Nord) |
| Custom CSS (user.css) | ✅ (CI), live pending | A user rule recolours a native widget over an equally specific `!important` theme rule, `display:none` hides a native element, and removing it restores the theme (`TestUserCSSStylesNativeWidgets`, `2865f2c`). Theme → user.css order fixed (`00d5efe`). A user.css token override recolours the native palette (`TestThemeTokensUserCSS`). |

On `6da798a` the pane background, sidebar and window matched every measured theme exactly, but the message list itself still painted Slack's `#1a1d21`. `a4aa0ac` makes the list follow the tokens (unit test `TestMessageColorsFollowTokens`). Re-shooting the themes on that build is pending live, and each ⚠️ above becomes ✅ when it shows the list themed.

**"Preferences still has that Slack gray" (Hot Dog Stand, macOS, `theme-overlays`).** Slick themes are document-wide CSS, so in Slack every dialog and menu takes them. All bundled themes repaint overlays with one raw rule, `[role=menu],[role=dialog],[role=listbox],.c-menu,.p-ia4_floating_window,.c-popover__content{background-color:…;border:1px solid …}`, plus `.c-tabs__tab_menu--vertical .c-tabs__tab.c-tabs__tab--active{…}` for the Preferences tab. Everything else comes from the tokens Slack's stylesheet paints overlays with: `.c-menu` uses `--dt_color-ctr-pry`, `.c-sk-modal` uses `--dt_color-base-pry`, the menu highlight uses `--sk_menu-item_highlight-*` over `--dt_color-base-inv-hgl-1`, and tooltips use `--dt_color-base-inv-pry`. Material and i hate myself also target `.p-prefs_dialog__modal`, `__menu` and `__panel`. Clack's overlays had used a fixed palette (`popups.colorsOf`, blockkit's, kit's) and painted their native card over the rule's background. Now the palettes follow the tokens, and cards (`popups.styledCard`) let the rule's background and border show, rounded like Slack's own (`Styled.WrapRounded`). The Preferences menu carries Slack's `c-tabs__tab_menu--vertical`, and the dialog carries `p-prefs_dialog__modal`. CI: `TestThemeTokensOverlaysHotDogStand` and `TestThemeOverlayRulesReachNatives` (ui), plus `TestSlickViewThemeOverlays` (ui-slick), which draws Preferences and a menu under Hot Dog Stand, Nord, Catppuccin Mocha, Gruvbox Dark and AMOLED and under Slack's own look. Its `overlay-*` shots are in `ui-slick-theme-shots`.

**"AMOLED doesn't theme anything" (alpha.7, macOS, `slick-themes-fix`).** The stylesheet path was fine. AMOLED, like every bundled theme but Hot Dog Stand and i hate myself, is `"mode": "dark"`, so Slick installs it only in dark mode, and Clack was in light. alpha.7 had no OS colour scheme on macOS. In alpha.8, "Sync with OS" was on only while ui.json had no `dark`, but `savePrefs` writes `dark` on every save, so the first route change turned sync off for every later start. Fixes: `theme_sync` is pinned at start (an old `dark:false` is the old default, so sync comes on; `dark:true` was a choice and stays), Ctrl+Shift+Y ends sync like Appearance does, and picking a dark-only theme in light mode switches to dark. Downloaded themes load without a restart (`ReloadThemes`). Material's pane follows its `.p-view_contents--primary` background. CI: `TestSlickViewThemes` and `TestSlickViewEveryTheme` in ui-slick draw the whole window with every upstream theme over a Slack account theme (Ocean), check chrome, sidebar, header, pane, composer, border and text pixels, and upload `ui-slick-theme-shots` (light and dark).

Related theme fixes: importing selects the theme (`fcc7ac9`); pausing Slick keeps the theme and user.css (`142b738`); validation matches `isThemeJson` (`528dd0a`); `document.body` carries `sk-client-theme--dark/--light` (`a1e88f5`).

## Bugs found and fixed (live-slick)

| Commit | Package | Bug |
|---|---|---|
| `7362936` | slackc | document.body's root box shielded the whole window: any plugin element in the body (oneko) made every click do nothing |
| `a4c04df` | messages, popups | plugins' member patches (Nicknames) never reached message senders or popups |
| `a4aa0ac` | messages | the message list ignored theme tokens |
| `38afc61` | composer | emoji button `data-qa` was `texty_emoji_button`, not Slack's `emoji_toolbar_button` |
| `b985c8d` | slackc | children a patched component's Original lays out didn't see the elements it entered (CSS ancestors) |
| `4c35299`, `a738093` | jsrt | no CompressionStream / DecompressionStream |
| `8163fec` | jsrt | `pipeTo` dropped the chunk of an already-closed source: `blob.stream().pipeThrough(…)` produced nothing |
| `b28043a` | jsrt | atob/btoa in JS were slow enough to run tasks into the 5 s interrupt |
| `ac3ef8a` … `a1e88f5` | slick, jsrt | fix-slick: main-half sender, theme import/pause/order/validation, dark class |
| `5aac9fb` … `e898996` | slick, taut, jsrt, slack | fix-taut: thunk timeout, `.promise` tier, guest count, component sources, media/iframe prototypes, `status_text_canonical`, local timezone |
| `279f419`, `f801d44` | ui, theme, views, richview, css | fix-theme: native palettes follow Slack design tokens |

| `815933f`, `da3c2d4`, `4e3fca4`, `aafbd3e` | ui, slackc, jsrt, taut | fix-dialog: plugin window key listeners swallowed the composer's Enter (ChatTabs); modal `<dialog>`; a send check whose task the watchdog killed can't hold a message forever (ignored after 10 s with nothing open for the user) |
| `slick-chattabs` | ui, slackc, taut | Slick ChatTabs' `f52f03a` tab bar never showed: no `RenderedViewContents` in Clack's tree, the primary view wasn't a host element (`:has(> .slick-chat-tabs)` can't match natives), and a `display: contents` element at a tree's top broke percentage heights below it |

## Fixed in plugins-slick-pass (2026-11-11, CI only)

| Commit | Package | What |
|---|---|---|
| `97e33367`, `d27aac7d` | css, slackc, messages, ui, blockkit, jsrt, slick, cmd/clack, gioui.org/app | Snappy did nothing in Clack: transitions computed by the CSS engine and the natives' own transitions read from the cascade; Slack's stylesheet and shell mirrors in the page with a real CSSOM; Electron's session spellchecker drives the composer spellcheck; main halves' switches kept for the next start (crash capture, X11 Vulkan); `deferResizeWork` holds ResizeObservers; Preferences notes per setting |
| `8c2f7943` | composer | SlimMessageBox's one-line layout: a row-flex `.c-basic_container__body` puts the footer beside the editor |
| `ecb7940b` | views | Reaction pills in Threads, Unreads and Activity at Slack's Reaction / UnstyledButton boundaries (WhoReacted) |
| `76205972`, `e375e310` | views, popups | A plugin's copy of a message's text is what Activity rows and search cards show (Censorship) |
| `c7d05fe4` | messages | A multi-message copy leaves times out when the timestamp label is unselectable (IgnoreTime) |

Still open: StreamerMode's blur and HcaStatus's underline on natives have no view test; CopyReacted in the views (no ReactionBar or ReactionAddButton there); SlimMessageBox's `:has()`/`@container` cases; ClearURLs' paste-time cleaning (Quill's Clipboard); CustomFonts, CustomSounds, oneko's sprite, OnlineCount, PrivateChannelMapper's pill, ShowRealUser's forwarded copies; huddle media (out of scope); and every *CI*-only row's live re-check.
