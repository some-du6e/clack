# Taut plugins in Clack

Every Taut plugin (https://github.com/jeremy46231/taut at `6fe5123`, which is upstream `main`, `TAUT_PIN` in CI, and the Taut half of the Plugin Library's `plugins` release: same 40 files, same commit) checked against Clack on 2026-11-10. Companion to [PLUGINS-taut.md](PLUGINS-taut.md) (the API), [PLUGINS-state.md](PLUGINS-state.md) (the Redux mirror) and [PLUGINS-live-slick.md](PLUGINS-live-slick.md) (the same pass for Slick).

## How it was checked

- **Source review.** Each plugin's source (`plugins/<Name>.ts(x)`, cited as `File.tsx:line`) was read against Clack's TautAPI (`internal/taut/js`), its patch points (`slackc.Render`/`Component`/`Child` at Slack's component names), the CSS engine (`internal/ui/slackc/css`, what natives carry) and the mirror (`internal/taut/mirror.go`).
- **CI.** `taut` (every plugin built with Taut's `build:user-plugin`, imported unmodified, started, its hooks checked, `behaviour` checks for most), `composer-taut`, `popups-taut`, `messages-taut` and `ui-taut` (the real call sites), and the plugin bench (`docs/PLUGIN-BENCH.md`: each plugin both systems ship, run in the whole client over `internal/fakeslack`).
- **No live Slack runs** (policy for this pass). Nothing below was seen on real Slack.

Legend: ✅ does what it does in Slack · ⚠️ partly (what's missing is listed) · ❌ no visible effect in Clack (why) · — not applicable.

## Plugins

| Plugin | Result | What it does in Slack | What it does in Clack | Notes |
|---|---|---|---|---|
| AccountSwitcher | ✅ | "Switch account" submenu above Sign out in your profile menu; saved accounts, switch, add, forget (`AccountSwitcher.tsx:49`, `:85-130`) | The same submenu in Clack's user menu (its MenuFromTemplate has Slack's `sign-out` key); accounts in the secret store, switching writes creds.json and reconnects (`taut/accounts.go`) | Bench: "Switch account in the profile menu". A desktop session's saved accounts can't be checked before switching (their cookies rotate), so a dead one fails when picked. |
| AdminBackend | ✅ | "Open in Identity / Telescreen / Slack Admin…" in a member's profile ⋮ menu (`AdminBackend.tsx:120`, `:129`) | The same rows in the profile card's ⋮ menu (RimetoMemberProfileOverflowMenu around its MenuFromTemplate); the tools editor renders in its settings | Bench pass; the editor in `ui-taut`. |
| AnonymizeFilenames | ✅ | Files upload under a random name (`AnonymizeFilenames.ts:39`, `:48`) | The composer dispatches `addAndUploadPendingFile` with the File; it uploads under the plugin's name | `composer-taut` TestTautAnonymizeFilenames. |
| BetterTypingIndicator | ✅ | Names everyone typing, cut to fit, each a link to the profile (`BetterTypingIndicator.tsx:139-142`) | TypingNames gets the conversation from MessagePaneInput / ThreadTypingIndicator and draws every typer | Bench: "3 of 3 typists named". |
| BringBackSlackbot | ✅ | Forces `reskin_custom_responses` off: custom responses show as sent by Slackbot (`BringBackSlackbot.ts:19`) | Clack gates its reskin on that experiment, so custom responses are Slackbot's again | This Slack build no longer reads the experiment (docs/EXPERIMENTS.md), so today it changes nothing in Slack; in Clack it still does. |
| BrowserPopouts | ❌ | Turns on Slack's pop-out windows in a browser (`URLSearchParams.get('windowing')` → `'1'`, `BrowserPopouts.ts:18-28`) | Nothing: Clack has one window and no pop-outs | Needs multi-window support in Clack. |
| Censorship | ⚠️ | Masks the words you choose in messages, notifications and search (`Censorship.ts:67` patchMessageText) | Channel and thread rows show the masked text; notifications go through `showNotification` | `messages-taut` TestPluginCensorship. Search isn't masked: Clack's search view never hands its results to the mirror (`Taut.SetSearchResults` has no caller), so the patched `searchResults` slice isn't read. |
| ClearURLs | ✅ | Strips tracking parameters from links you paste (`ClearURLs.ts:53`) | Pasted links and the link dialog go through it | `composer-taut` TestTautClearURLs. |
| ClickToLoad | ❌ | Holds Spotify, SoundCloud, Apple Music and other audio embeds behind "Load …" until clicked (`ClickToLoad.tsx:44` AttachmentMedia) | Never triggers: Clack plays no embeds (no iframes), its AttachmentMedia props carry no `audioHtml`, and the patch point covers the whole unfurl card, not just the media | Making it Slack's would need richview to draw the media part as its own AttachmentMedia, with `audioHtml`/`audioHtmlHeight`. |
| CopyReacted | ⚠️ | A copy button after Add reaction: Everyone or one emoji's reactors, as mentions, names or handles (`CopyReacted.tsx:174`, `:191`) | The button and its menu work; the list goes to the clipboard | `popups-taut` CopyReacted, bench. The chip draws without Slack's `c-reaction_add` pill style (Clack has no Slack stylesheet for plugins' own elements but the context bar's). |
| CustomNameRecording | ❌ | "Upload audio" beside the name recording in Edit profile (`CustomNameRecording.tsx:25`, `:89`) | The button shows, but every upload fails: jsrt has no `AudioContext.decodeAudioData` or `MediaRecorder` | Needs audio decoding (and for non-MP3 files re-encoding) in jsrt. |
| Experiments | ✅ | A Preferences tab with every experiment assignment and overrides (`Experiments.tsx:147`) | The tab lists the account's assignments; an override reaches core and changes Clack at once | TestExperimentsPluginsDriveClack. |
| FasterSlack | ✅ | Pauses Slack's top-nav layout work while the window resizes (`FasterSlack.ts:27`, `:36`) | Nothing to pause: Clack's layout is native and has no lag; the plugin finds no `.p-ia4_top_nav__left_container` in the JS document and stands down | Nothing visible by design. |
| HaikuWarning | ✅ (fixed) | A yellow banner over the composer while your draft is a haiku Orpheus would repeat; or a pop-up when you send (`HaikuWarning.tsx:89`, `:146`) | Both: the banner over the composer (BaseContextBarV2), styled with Slack's context bar rules, and the send check | Was ❌: the drafts slice was keyed by server draft id and only updated after a sync, so the banner never saw your text. `taut` HaikuWarning/behaviour. |
| HuddleActivity | — | Logs who did what in a huddle (joins, mutes, settings) and toasts it (`HuddleActivity.tsx:108-563`) | Out of scope (huddle media): Clack's huddle has no event log or captions menus to patch | |
| HumanCount | ✅ | The header's member count leaves apps (and optionally guests) out (`HumanCount.tsx:24`) | Same (BaseAvatarStack memberCount from `membershipCounts`) | `taut` HumanCount/behaviour, bench. |
| IdvStatus | ✅ | Wavy red/orange underline under people who aren't IDV-eligible / are over 18 (`IdvStatus.tsx:43`, `:88`) | The class reaches the sender and the wavy underline is painted | `messages-taut` TestPluginIdvStatusSenderClick; asks identity.hackclub.com. |
| InvisibleForward | ✅ | Slack links at the start of a message are sent invisible (`InvisibleForward.tsx:19`) | Same, on every send | `composer-taut`, `taut`. |
| LastSeen | ✅ | "Last seen …" on profiles and hover cards (`LastSeen.tsx:83-140`) | Same | Bench: "Last seen on a profile". |
| NativeAtChannel | ✅ (fixed) | @channel/@here through at-channel where you can't ping; edits and deletes of your pings go through its shortcuts (`NativeAtChannel.tsx:123-452`) | Sends, the @ menu and confirmations as in Slack; edits and deletes run at-channel's shortcuts | `composer-taut` TestTautNativeAtChannel*. Deleting or editing a relayed ping used to fail: `executeAppAction` dropped the plugin's client_token, so its modal never came back to it. |
| Nicknames | ⚠️ | Your nicknames replace people's names across Slack (`Nicknames.tsx:52-112`) | Senders, profiles, hover cards and the member menu; "Set nickname…" in the profile ⋮ and right-click menus | `popups-taut`, bench. Mentions in messages keep the real name (Clack resolves them when the message is parsed). |
| NoTrack | ✅ | Blocks Slack's tracking requests (`NoTrack.ts:105`) | Clack makes none of these requests; the plugin's own blocking works on what plugins fetch | Nothing visible by design. `taut` NoTrack/behaviour. |
| OfficeHours | ✅ | Out of office or away on a weekly schedule (`OfficeHours.tsx:208-262`) | Same (users.profile.set, users.setPresence, dnd.setSnooze) | Bench: "inside an Away window, you're set away". Not run on the real account (it changes your status). |
| Oneko | ✅ (fixed) | A cat that chases your cursor (`Oneko.ts:90`) | Same | Was ⚠️: the cat followed the pointer but never changed frame, as `background-position` other than center was ignored. slackc now positions and tiles backgrounds and honours `image-rendering: pixelated` (TestBgPosition, TestImageRendering). |
| OwlMode | ⚠️ (fixed mostly) | Reply and react anonymously in your owl confession's thread; save confession keys from owl's DM or modal (`OwlMode.tsx:102-500`) | The thread composer's owl button, context bar and styling; replies and reactions go through owl's shortcuts; keys save from owl's DM | Was ❌ for replies and reactions: Clack had no `appActions` store (owl's shortcuts weren't found), and `executeAppAction` dropped the plugin's client_token. `taut` OwlMode/behaviour runs a reply end to end. Left: "Save in Taut" inside owl's modal needs a ViewBlocks patch point in Clack's Block Kit modals. |
| PrivateChannel | ⚠️ (fixed partly) | Names private channels you aren't in from the flaron index; mentions grayed with a lock; local names (`PrivateChannel.tsx:83-689`) | Mentions get Slack's props (isPrivate, isMember, isNonExistent, isUnknown) and the plugin's lock styling; a typed channel id resolves through `fetchAndUpsertChannelsById` | `taut` PrivateChannel/behaviour names a channel from flaron. Was: `fetchAndUpsertChannelsById` didn't exist, so typed ids hung for 5 s. Clack never dispatches `fetchRawChannelsById` for mentions it can't resolve, so those keep their id until typed. |
| QuickJoin | ✅ | "Join channel" in the right-click menu of channels you're not in (`QuickJoin.tsx:29`) | Same, in the channel menus (a channel link's right-click menu among them) | `popups-taut` QuickJoin, bench. |
| RealMarkdown | ✅ | Standard Markdown as you type (`RealMarkdown.ts:17`) | Same | `composer-taut` TestTautRealMarkdown. |
| RestrictedChannelWarning | ✅ | Admins see the "Only certain people can post" box, with Post anyway (`RestrictedChannelWarning.tsx:51-88`) | Same | Bench pass. |
| ShinigamiEyes | — | Disabled upstream (`.disabled.tsx`): broken since the Hackatime API changed | Loads and does nothing without an API token, as in Taut | |
| ShowRealUser | ✅ | Messages a relay bot posted for someone show that person (`ShowRealUser.tsx:175-212`) | Same, from the message's metadata | Bench: "a relay bot's message credits the person". |
| ShowSendingBot | ✅ | The bot that sent a person's message, as a badge on their avatar (`ShowSendingBot.tsx:256-444`) | Same, on rows and grouped rows | `messages-taut` TestPluginShowSendingBot, bench. |
| ShutUpSlackbot | ✅ | Marks Slackbot's "an app took over your slash command" DMs read and silences them (`ShutUpSlackbot.ts:32-78`) | Same | `messages-taut`, `taut`, bench. |
| SilentTyping | ✅ | A composer button that stops your typing indicator (`SilentTyping.tsx:41`, `:50`) | Same | `composer-taut`, `taut`, bench. |
| SlackFixes | ⚠️ | Keeps zalgo text in its message; shows thread admin tools on bot threads; fixes the Halloween sidebar row (`SlackFixes.tsx:26-52`) | Zalgo text already stays inside its message (Clack clips message text to its box, at the edge where Slack's rule leaves 4px) | Clack has no thread admin tools (ThreadFlexpaneAdminTools is an empty patch point) and no Halloween row, so those two options have nothing to fix. |
| SlimMessageBox | ⚠️ | Hides the composer buttons you don't use, a broadcast button, the formatting bar as its own setting, and a one-line layout (`SlimMessageBox.tsx:91-261`) | Hidden buttons, the broadcast button and the formatting-bar setting work | The one-line layout (on by default) does nothing: it reorders Slack's composer with CSS flex `order` rules, and Clack's composer parts are native, at fixed places. |
| StreamerMode | ⚠️ | Blurs private things (channel names, DM previews, activity, private threads), hides VIP badges, silences notifications; a top-bar button and Ctrl/Cmd+Shift+P (`StreamerMode.tsx:86-319`) | The button, the shortcut and silenced notifications; its blur rules target classes and attributes Clack's sidebar, DM list, activity and threads view carry | Bench: the blur stylesheet installs, notifications are silenced; the blurs aren't checked pixel by pixel. Not blurred: private channel mentions in messages (the rule needs `:has()` over a native's children and Slack's `data-inline-channel-type-icon`), VIP badges (no `priority_vip_badge` element). Screen shares don't turn it on (Clack's huddle doesn't use getDisplayMedia). The button draws without Slack's top-nav button style. |
| ThreadJump | ⚠️ | A Jump in thread button for threads of 50+ replies: Top, Bottom, ±100, a date; Slack's unreleased Search in thread (`ThreadJump.tsx:41-52`) | The button, its panel and the jumps | `taut` ThreadJump/behaviour (new). `findInThread` (on by default) does nothing: Clack has no find-in-thread UI (it doesn't read `find_in_thread`); nor the `banners` option's `fe_long_threads_jump_to_latest`. |
| UserPronouns | ✅ | Pronouns after the timestamp in channels and thread headers (`UserPronouns.tsx:57-107`) | Same | `messages-taut` TestPluginUserPronouns, bench. |
| WhoReacted | ✅ | The reactors' avatars inside each reaction pill (`WhoReacted.tsx:60-102`) | Same | `messages-taut` TestPluginWhoReacted, bench. |

## Fixed in this pass (branch plugins-taut-pass)

| Area | Fix | Plugins |
|---|---|---|
| taut mirror, composer | `drafts.unifiedDrafts` is keyed by Slack's `client_draft_id` (`encodeDraftKey(channel, thread)`, or `client_msg_id` for Drafts-page drafts), and the composer keeps its entry's ops current as you type (Slack's `fastLaneDraftUpdate`, 150 ms debounce, leading and trailing); a send clears it (`Taut.SetComposerDraft`, `ClearComposerDraft`) | HaikuWarning, NativeAtChannel |
| taut mirror, core | `appActions` store from apps.actions.v2.list's message actions, as Slack's `loadClientAppCommands` builds it | OwlMode |
| core, blockkit | `executeAppAction` from a plugin sends its own `client_token` (`Interactive.RunShortcutAs`), so the modal comes back to it in `view_opened` instead of opening | OwlMode, NativeAtChannel |
| taut thunks | `fetchAndUpsertChannelsById` (through `fetchRawChannelsById`, as Slack's) | PrivateChannel |
| slackc | `background-position` (lengths, percentages, keywords), `background-repeat` tiling, `image-rendering: pixelated` | Oneko |
| slackc | Slack's `.p-context_bar_v2` rules for plugins' own elements | HaikuWarning, OwlMode |
| Preferences | Preferences > Plugins is Taut's own tab (see below) | all |

New acceptance checks in `internal/taut` (workflow `taut`): HaikuWarning, OwlMode, PrivateChannel and ThreadJump behaviours.

## Preferences > Plugins

Karim: the plugin settings page read like a thank-you page written for Slick. It's now Taut's own Preferences tab (`app/settings`: `index.tsx`, `pluginList.tsx`, `screens.tsx`), over both systems:

- **Main screen.** Taut's rows to Appearance ("Custom CSS"), Advanced ("Installing plugins, config.json": Taut's, without safe mode, which Clack doesn't have) and About ("Credits"), and one Clack row to the plugin library. Then Taut's "Search plugins", its pills (All, Enabled, Disabled, Hack Club only, User plugins) and every installed plugin of both systems under Taut's groups (`pluginGroup`: its categories, else User plugins), each Taut's PluginRow (checkbox, name and description, a warning when it needs attention, a caret). Slick plugins are ordinary rows; a Slick category Taut doesn't have lists under User plugins, as Taut lists any user plugin.
- **No hits.** A search nothing installed matches suggests the library's plugins that do ("You don't have anything like that installed. Maybe you're looking for:"), each with the library's Install action, and links to all results in the library with the search kept. The plain empty state shows only when the library has nothing either.
- **Advanced.** Taut's sets: Installation (Clack's version, Taut's config directory), the loader's settings (Clack's single row: Run Slick plugins), Install a plugin (Taut's ImportControls; a Slick file goes to Slick) and config.json (follows the file until edited, like Taut's editor). Taut's Safe mode, What's new and Usage statistics aren't there: Clack has none of them.
- **Appearance** (Custom CSS) and **About** (Taut's text, then every plugin's credits) in Taut's words.
- Gone: the Plugin settings tab (its id and Slick's old tab open Advanced), the Installed/Browse strip, the install dialog, the cards and toggles, config-folder and URL-update rows, and Slick's thank-you cards.

## Left

- **Search masking** (Censorship): the search view would have to hand its results to the mirror and read the patched copy back.
- **AttachmentMedia** (ClickToLoad): the media part of an unfurl as its own patch point, with Slack's `audioHtml` props.
- **Audio in jsrt** (CustomNameRecording): `AudioContext.decodeAudioData` and `MediaRecorder`.
- **ViewBlocks** (OwlMode's Save in Taut in owl's modal): a patch point in Clack's Block Kit modals.
- **Mentions under Nicknames**: mention labels from the patched members slice.
- **CSS on natives** (StreamerMode's mention blur, SlimMessageBox's one-line layout): `:has()` over natives' children and flex `order` on native composer parts.
- **Slack's stylesheet for plugins' own elements** beyond the context bar (CopyReacted's chip, ThreadJump's and StreamerMode's header buttons draw unstyled).
- **Surfaces Clack doesn't have**: pop-out windows (BrowserPopouts), find in thread (ThreadJump's findInThread), thread admin tools and the Halloween row (SlackFixes), huddle event logs (HuddleActivity, out of scope).
