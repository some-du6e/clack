# Plugin compatibility in Clack

Clack runs Taut's and Slick's plugins natively, from the published plugin Library (98 plugins: 40 from Taut, 58 from Slick). This page is how far each one works in Clack v0.1.0-alpha.148.

| | Plugins |
|---|---:|
| ✅ Works fully | 73 |
| ⚠️ Partly works | 16 |
| ❌ Doesn't work yet | 9 |

A plugin both systems ship (under the same name or not) is one row in Preferences › Plugins › Browse; here each version has its own line.
Compatibility follows the newest Clack release. Each row comes from `internal/pluginlib/compat.go`, which `TestCompatMatchesDocs` keeps in step with the per-system pages, [PLUGINS-live-taut.md](PLUGINS-live-taut.md) and [PLUGINS-live-slick.md](PLUGINS-live-slick.md).

## ❌ Doesn't work yet

| Plugin | From | What it does | In Clack |
|---|---|---|---|
| Better Huddles | Slick | A lighter huddle client with its own window, optional in-page controls, and a direct Chime connection | needs browser WebRTC |
| Browser Pop-outs | Taut | Enables Slack's pop-out windows in the browser | needs pop-out windows |
| Click to Load | Slick | Replaces third-party media embeds with privacy-preserving click-to-load placeholders | Clack shows no iframes for it to hold back |
| Custom Fonts | Slick | Use any installed system font or upload your own font file. | Clack can't load web fonts |
| Custom Name Recording | Taut | Uploads an audio file as your name recording, instead of recording one | can't decode audio |
| Custom Sounds | Slick | Override Slack notification sounds with your own audio file. | plugins can't play sounds in Clack |
| Online Count | Slick | Show how many of a channel's members are online, next to its member count | needs Slack's presence query |
| Quiet Spotify | Slick | Customize the volume of Spotify embeds so they are not stupidly loud | Clack has no Spotify players to quiet |
| Slick MCP | Slick | Connect local AI agents to Slack with search, messaging, and desktop controls. | its MCP server needs Node's http module |

## ⚠️ Partly works

| Plugin | From | What it does | In Clack |
|---|---|---|---|
| Account Switcher | Slick | Switch between saved accounts from the profile menu | switching accounts goes through Clack's sign-in |
| Anonymise File Names | Slick | Replace uploaded file names with a random string. Toggle individual names from the attachment preview. | renames uploads, but not in the edit-file dialog |
| Better Message Menu | Slick | excuse me laddie but i organized your menu | no Vim keys; big buttons stay on top above the cursor |
| Chat Tabs | Slick | Browser-like tabs for the real power users of Slack. Open channels, DMs, and threads in new tabs to keep organized. | thread tabs open beside their channel |
| Clear URLs | Slick | Automatically removes tracking elements from URLs you send | cleans links when you send, not when you paste |
| Click to Load | Taut | Holds Spotify, SoundCloud and other music embeds until you click to load them | Load shows Clack's media box, not a player |
| Custom Name Recording | Slick | Upload custom audio as your Slack name recording | can't decode audio, so nothing uploads |
| Installed Apps | Slick | Show apps people authorized, who authorized an app, and its channel memberships. | the app marketplace page asks you to sign in |
| Let Me See | Slick | Adds a Hide drawings button to the floating huddle bar shown during a screenshare | Hide drawings does nothing |
| oneko | Slick | cat follow mouse (real) | the cat runs but isn't drawn |
| Private Channel | Taut | Lets you see and mention private channels you aren't in (uses the <https://flaron.halceon.dev\|flaron> index) | unknown channel mentions aren't looked up |
| Private Channel Mapper | Slick | Name the private channels you can’t see, and mention the ones you are not in | the missing-channel pill never shows |
| Self Care | Slick | Take a break from Slack and pause notifications for a while. | a break pauses notifications, not the app badge |
| Show Real User | Slick | Show who actually sent a message when a relay bot posts on their behalf | forwarded copies aren't matched |
| Slack Fixes | Taut | Fixes small bugs in Slack's UI | two of its fixes have nothing to fix in Clack |
| Streamer Mode | Taut | Blurs private information while others may be able to see your screen | doesn't hide VIP badges or screen shares |

## ✅ Works fully

| Plugin | From | What it does |
|---|---|---|
| Account Switcher | Taut | Switches between saved accounts from the profile menu |
| Admin Backend | Slick | Open profiles in Hack Club admin tools |
| Admin Backend | Taut | Adds buttons to open a member in Hack Club tools |
| Anonymize Filenames | Taut | Randomizes file names before uploading to prevent metadata leakage |
| bChannel | Slick | Type @channel or @here and send, no permission hassles, even as a channel manager |
| Better Blocking | Slick | Make people disappear locally: messages, reactions, typing, and more. Huddles are untouched. |
| Better GIFs | Slick | Enhances Slack’s GIF picker with a better search experience, favorites, and more. |
| Better Typing Indicator | Slick | Show as many of the people typing as fit, with their profile pictures, and open their profiles from the indicator |
| Better Typing Indicator | Taut | Shows as many typing people as possible and lets you view their profiles |
| Bring Back Slackbot | Taut | Shows Slackbot's custom responses as sent by Slackbot again |
| Call Timer | Slick | Show how long you’ve been in a huddle |
| Censorship | Slick | There is no war in Ba Sing Se. |
| Censorship | Taut | Masks words you choose in messages, only on your screen |
| Channel List Typing | Slick | Show who’s typing next to each channel and DM in the sidebar |
| Character Counter | Slick | Show how many characters your message has next to the send button |
| Clear URLs | Taut | Strips tracking parameters from links you paste into messages (rules from <https://github.com/ClearURLs/Rules\|ClearURLs>) |
| Copy Reacted | Slick | Copy the list of people who reacted to a message |
| Copy Reacted | Taut | Copies the list of people who reacted to a message |
| Custom Slackbot | Slick | Customize the name and avatar used for workspace custom responses |
| DeArrow | Slick | Replace clickbait YouTube titles and thumbnails with crowdsourced ones. Sends the IDs of YouTube videos you see to DeArrow (sponsor.ajay.app). |
| Experiments | Slick | Enable Access to Experiments & other dev-only features in Slack! |
| Experiments | Taut | Adds a tab for trying Slack's unreleased features |
| Faster Slack | Taut | Makes Slack faster and smoother |
| Haiku Warning | Slick | Warn before sending a message that Orpheus would quote back as a haiku |
| Haiku Warning | Taut | Warns before you send a haiku that Orpheus would repeat in the thread |
| HCA Status | Slick | Flag users who have not completed identity verification. Sends Slack user IDs to auth.hackclub.com for verification and age-category checks. |
| Hide Resolved Tickets | Slick | Hide resolved questions in help channels so only the open ones are left |
| Huddle Activity | Taut | Logs who did what in a huddle, including mutes and settings changes |
| Huddle Mute Detector | Slick | Show who muted you in a huddle |
| Human Count | Slick | Leave bots and apps out of the channel header's member count. |
| Human Count | Taut | Leaves apps, and optionally guests, out of the channel header's member count |
| IDV Status | Taut | Underlines people who aren't IDV eligible in red, and IDV over 18 in orange |
| Ignore Time | Slick | Makes Slack not copy that stupid little timestamp |
| Invisible Forward | Taut | Makes Slack links at the start of your messages invisible, like a forwarded message |
| Last Seen | Slick | Show a "last seen" time for other people, based on what your client can observe. |
| Last Seen | Taut | Shows on someone's profile when they were last seen |
| Local Time | Slick | Show each person's local time beside their messages and in their profile, even while they're away. |
| Mention Profiles | Slick | Show profile pictures inside @mentions |
| Message Logger | Slick | Keep deleted and edited messages visible |
| Native At Channel | Taut | Sends @channel and @here through at-channel where you can't ping yourself |
| Neutral Pronouns | Slick | Rewrites he/she pronouns in messages you send to they/them, adjusting nearby verbs. |
| Nicknames | Slick | Set local nicknames for users and apps |
| Nicknames | Taut | Renames members across Slack |
| No Track | Slick | Disable Slack's built-in tracking and telemetry |
| No Tracking | Taut | Blocks Slack's built-in tracking and analytics requests |
| Not Shit Markdown | Slick | Make the composer use normal Markdown instead of Slack markup |
| NoUpsell | Slick | Hide promotions, trial offers, and Slack bot AI nudges. |
| Office Hours | Slick | Set schedules for out-of-office and invisible status |
| Office Hours | Taut | Sets you out of office or away on a weekly schedule |
| Oneko | Taut | Adds a cute cat that chases your cursor, based on <https://github.com/adryd325/oneko.js\|oneko.js> |
| Owl Mode | Taut | Lets you reply and react anonymously as the author of your owl confessions |
| Quick Join | Slick | Join channels straight from a link to them, without opening each one first |
| Quick Join | Taut | Adds "Join channel" to the right-click menu of channels you're not in |
| Real Markdown | Taut | Formats standard Markdown as you type, instead of Slack's markup |
| Restricted Channel Warning | Slick | Warn before posting in channels where only admins can post |
| Restricted Channel Warning | Taut | Shows admins the "Only certain people can post" box where only admins can post |
| Shinigami Eyes | Taut | Displays Hackatime trust level indicators next to user names in Slack |
| Show Real User | Taut | Shows the user who sent a message via a bot like at-channel |
| Show Sending Bot | Slick | Show which bot or app sent a message |
| Show Sending Bot | Taut | Shows the bot used to send a user message |
| Shut Up Slackbot | Slick | Mark Slackbot slash-command registration DMs as read and silence their notifications. |
| Shut Up Slackbot | Taut | Marks Slackbot's "an app took over your slash command" DMs as read and silences them |
| Silent Typing | Slick | Never show the "is typing…" indicator to anyone else |
| Silent Typing | Taut | Adds a button to hide your typing indicator, so others can't see when you're typing |
| Slim Message Box | Slick | ozempic for your message box! |
| Slim Message Box | Taut | Fits the message box on one line and hides the buttons you don't use |
| Snappy | Slick | Make Slack feel more responsive by disabling animations and other slow features |
| Streamer Mode | Slick | Blur private information while others may be able to see your screen |
| Thread Jump | Taut | Lets you jump through long threads to the top, bottom or a date, or 100 replies at a time |
| User Pronouns | Slick | Display users' pronouns next to their messages |
| User Pronouns | Taut | Shows people's pronouns next to the timestamp on their messages |
| Who Reacted | Slick | Show the avatars of everyone who reacted next to each reaction |
| Who Reacted | Taut | Shows the avatars of everyone who reacted, next to each reaction |
