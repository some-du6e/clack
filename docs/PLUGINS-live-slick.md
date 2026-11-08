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

Legend: ✅ works as in Slick on real Slack · ⚠️ partly (what's missing is listed) · ❌ no visible effect in Clack (why).

**Status (2026-11-07, `live-slick` at `b28043a`):** live testing is paused. ui-shell is investigating Slack revoking whole sessions of an Enterprise Grid account that uses Clack (suspected: the realtime websocket's client identification). Rows marked *pending live* are verified in CI and by source review only.

## Plugins

| Plugin | Result | Live in Clack | Notes |
|---|---|---|---|
| AccountSwitcher | ⚠️ | not run (switching moves session cookies) | Menu entry registers. Clack has one session, so switching goes through Clack's sign-in. |
| AdminBackend | ✅ | "Open in Identity / Telescreen / Fire Engine / Attend" in the profile ⋮ menu | |
| AnonymiseFileNames | ❌ | — | Clack uploads from Go, never as JS `File`s. Its 3 components have no patch point. |
| bChannel | ❌ | not run (it posts and invites) | The composer drops a patched send's replaced delta (`prepareAndSend` honours only `shouldBroadcast`). |
| BetterBlock | ⚠️ | "Block karim" in the profile ⋮ menu | Messages, threads, reactions and the profile card follow the block (CI). Search hits, typing, notifications and sidebar DMs still show the user. |
| BetterGifs | ⚠️ | pending live | Source filter fixed (`94efea3`). The GIF picker patch points come from taut-ui. |
| BetterHuddles | ❌ | — | Clack has no huddle media, so its surfaces never render. |
| BetterTypingIndicator | ⚠️ | pending live (needs a second person typing) | Its `<Original firstTyper>` slot was fixed on live-taut (`550dd92`). |
| CallTimer | ❌ | — | No huddles. |
| Censorship | ⚠️ | pending live | Channel and thread bodies are masked (CI). Search cards and Activity rows aren't. Notifications go through live-taut's `02c9fbb`. |
| ChannelListTyping | ⚠️ | pending live | Source filter fixed (`94efea3`). The sidebar row has no `Channel` patch point. |
| CharacterCounter | ✅ | "43/40,000" beside the send button, following the text | |
| ChatTabs | ⚠️ | blocked every send while HaikuWarning was on (bisected live) | Fixed in `815933f`: its main half's `before-input-event` added window key listeners, and Clack fed them the composer's Enter. Plugins' window key listeners now get only the keys no widget took, as on Slack's page. Its main half no longer throws (`ac3ef8a`). No tab-bar patch point. Live re-check pending. |
| ClearURLs | ⚠️ | pending live (the one test send was cut short by the pause) | Strips tracking parameters on send in CI. Paste-time cleaning needs a Quill-style composer. |
| Click2Load | ❌ | — | Clack never creates iframes. It now starts past its prototype check (`6451875`). |
| CopyReacted | ⚠️ | not seen in the Threads view; channel view pending | |
| CustomFonts | ❌ | — | The stylesheet installs, but Clack renders only its bundled Lato (no `@font-face`). |
| CustomNameRecording | ⚠️ | pending live | Button on your own profile. `decodeAudioData` isn't supported, so nothing uploads. |
| CustomSlackbot | ❌ | — | Needs `hasSlackbotCustomResponseInThread` and the custom-response classes. |
| CustomSounds | ❌ | — | Clack plays no notification sounds through JS. It starts now, then `load()` returns for `loader === 'electron'`. |
| DeArrow | ⚠️ | pending live | Works through taut-ui's YouTube card patch points (CI `TestSlickViewDeArrow`). |
| Experiments | ⚠️ | its Preferences tab renders and saves | Empty catalogue: Clack has no Slack experiments. |
| HaikuWarning | ✅ (live alone; combined with ChatTabs in CI) | "Found a haiku in your message!", Keep editing returns the text, nothing sent | The dialog was drawn top-left. `showModal()` is now a real modal (`da3c2d4`): top layer, centred over a `::backdrop` that blocks the page, focus inside, Escape fires `cancel` then `close`. The ChatTabs combination is fixed (`815933f`). Needed native Compression Streams, `pipeTo` and `atob`. Centred-dialog look: live re-check pending. |
| HcaStatus | ⚠️ | pending live | The auth.hackclub.com lookup works and its class reaches the sender. The name underline (`text-decoration`) isn't drawn. |
| HuddleMuteDetector | ❌ | — | No huddles. |
| HumanCount | ✅ | header count shows (17, same as Slack) | Guest exclusion fixed (`8086322`). |
| IgnoreTime | ❌ | — | Clack's copy writes times itself; no native `.c-timestamp__label`. |
| InstalledApps | ❌ | — | No `Content`/`AboutPage` patch points. Its main half runs now (`ac3ef8a`), but `session.fetch` sends no Slack cookie (security policy), so the marketplace page needs sign-in. |
| LastSeen | ⚠️ | inconclusive (the user was online) | Hover-card line works. The profile pane draws `BasePresence`, not `Presence`. |
| LetMeSee | ❌ | — | No huddle media. |
| LocalTime | ✅ in messages | "3:56 PM • 4:37 PM": the sender's current local time, ticking each minute | Name-to-time gap fixed (`8316a3a`). Profiles have no `LocalTimeRow` patch point (⚠️). |
| MentionProfiles | ❌ | — | Mention pills are `MemberLink`; there's no `MemberSlug` patch point. |
| MessageLogger | — | not run (it stores message contents on disk, against the scraping policy) | CI acceptance passes. |
| Nicknames | ✅ | "Set nickname…" → dialog → the row's sender shows the nickname; profile popups too | Fixed in `a4c04df`. Mentions keep the real name (they're resolved when the message is parsed). |
| NoTrack | ✅ | nothing visible by design | Telemetry fetch blocked (CI). |
| NotShitMarkdown | ✅ (CI) | not distinguishable by typing: Clack's composer formats `**x**` itself | Applies on send to pasted Markdown (CI). |
| NoUpsell | ✅ | nothing to hide: Clack draws no upsells | |
| OfficeHours | — | not run (it changes your own presence) | `status_text_canonical` added (`51b8338`). JS local time is now the host zone (`7dbe2c1`). |
| oneko | ⚠️ | broke every mouse click until `7362936`; the cat isn't drawn | Its sprite is a `data:` URL, and pixel `background-position` isn't supported. |
| OnlineCount | ❌ | — | Needs Slack's flannel `query` export and presence subscriptions for up to 950 users. Not enabled on the shared account. |
| PrivateChannelMapper | ⚠️ | pending live | Autocomplete rows now keep their `.promise` tier (`d69711c`). The missing-channel pill never triggers. |
| QuickJoin | ⚠️ | pending live | The Join button comes from taut-ui. Its fallback no longer hangs (30 s thunk timeout, `5aac9fb`). |
| QuietSpotify | ❌ | — | No frames or Spotify embeds (by design). |
| RestrictedChannelWarning | ⚠️ | not testable (needs an admin-only channel) | |
| ShowRealUser | ⚠️ | pending live | Direct relay posts work. Forwarded copies carry a numeric `ts`. |
| ShowSendingBot | ⚠️ | pending live | Badge on header rows (CI). |
| ShutUpSlackbot | ✅ (CI) | needs a real Slackbot notice | |
| SilentTyping | ✅ | composer button toggles ("Disable silent typing") | |
| SlimMessageBox | ⚠️ | Hide emoji, mention, video, audio: buttons gone | Emoji fixed in `38afc61` + `b985c8d` (children of a patched component's Original now see its ancestors in CSS). The Discord-style one-line layout does nothing. |
| Snappy | ✅ | nothing visible by design | |
| StreamerMode | ⚠️ | pending live | Its main half no longer throws (`ac3ef8a`). |
| UserPronouns | ⚠️ | pending live | Channel headers work (CI). Name gap fixed. |
| WhoReacted | ⚠️ | not seen in the Threads view; channel view pending | CI `TestSlickViewWhoReacted` draws the reactors' avatars. |

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

Still open: the *pending live* rows, the Plugin Library's Slick entries, and origin/taut-ui's patch points, once the session issue is resolved.
