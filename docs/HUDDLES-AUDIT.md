# Huddles audit: Clack vs Slack, row by row

Branch `audit-huddles`, cut from `ui`@b508b79 on 2026-11-10.

How this was checked:

- **Slack source.** Build `407aff01646b` (2026-11-10), fetched from the public CDN without a cookie: `gantry-v2-async-client-boot-shared.a9a60ec7e2e80dee`, `gantry-v2-async-client-boot-render.c02799ae8f8de665`, `client-boot.1c8b2fa630ae65f2`, the `huddles-page_tsx` chunk `a29464bf75cbb4a4` and the small `gantry-v2-async-*huddle*` chunks named in slack-datamine-plus's `chunks.txt`. Module ids are cited below as chunk-id pairs, for example `shared 9d6D` for HuddleToolbar. CSS rules come from `client-boot-styles.cf726c4f2eaecc74` (build `a84662d2`, 2026-11-08). Its hashed class names, such as `toolbarContainer__eE_VC`, are the ones today's modules export, so the rules carry over. Theme token values come from the default theme tables in `gantry-v2-vendors.6085a6e7dffbbb12`: module `858H` is light mode, `jmNE` the light client's inverted sidebar scope, `7uI9` dark mode.
- **Slack live.** Not used. No huddle button, huddle window or Huddles page was opened in real Slack, and nothing was joined, started or rung.
- **Clack.** The fake Chime (`huddle.NewFake`) and internal/fakeslack only. `TestHuddleShotsAudit` (internal/ui/huddleui/audit_shots_test.go) renders the window, its ⋯ menu, the captions, the banners, the docked bar and the sidebar badge. The `huddles-audit-shots` workflow (workflow_dispatch) uploads them with the header, invite and mic pictures as `huddles-audit-shots`; `huddle.yml` keeps its own `huddle-shots`.

Status: **matched** means no change was needed, **fixed** means changed on this branch, **open** means still different, with the reason.

## Channel header button and menu

| Feature | Slack (evidence) | Clack before | Status |
|---|---|---|---|
| Idle button | HuddleChannelHeaderButton (`shared jMei`), the `--polish` container: headphones, `Huddle`, a 20px caret, 28px tall, hover fills only | same, measured in docs/FIDELITY.md row 7 | **matched** |
| Idle button tooltip | getStartHuddleTip (`shared 7zD7`): `Start huddle in {channel}`, `Start huddle with {name}` in a DM, plus addJoinLeaveKeyboardShortcuts (Mod+Shift+H on macOS, Mod+Alt+Shift+H elsewhere) | none | **fixed**: `StartTip`, hint through shortcut.Format |
| Idle caret tooltip | `More Huddles options`, or `More call options` with call apps installed | none | **fixed** (`CaretTip`). The call-apps wording is open: Clack has no call apps |
| Live button | jade-5 fill, up to 3 faces, headphones and a bare count; in the huddle a jade-50 fill with headphones that turns into close on hover | same (TestHeaderShots) | **matched** |
| Live tooltip | getTipForUserJoinActiveHuddle: a facepile, `Join huddle in {channel}` or `Join huddle with {name}`, `{n} in the huddle`, the shortcut | the two text lines, no shortcut | **fixed** (shortcut). The facepile inside the tooltip is **open**: Clack's tooltips are text only |
| In-huddle and other-device tooltips | `Leave huddle`; `Joined on another device` / `Join on this device` | same | **matched** |
| Caret menu | HuddleButtonMenu (`shared uw9r`) with HuddleButtonMenuItem (`shared XLpf`): Start Huddle / Join Huddle, Copy huddle link, and with `scheduled_huddles` the Huddle and Schedule for later groups with Schedule Huddle | same (menu_test.go) | **matched** |
| Copy huddle link | copyHuddleLinkByChannelId: rooms.getLink, `Huddle link copied` or `Couldn’t copy huddle link. Try again.` for 3 s | same | **matched** |
| Call apps and calendar rows | `Video meetings` and `Phone` groups from installed call apps; `Schedule meeting` (channel_meeting_invite) | none | **open**: no call apps or Google Calendar in Clack |

## Pre-join and starting a huddle

| Feature | Slack (evidence) | Clack before | Status |
|---|---|---|---|
| Every start path | the startHuddle thunk with Slack's entry points (DF8l's enum) | same (Begin) | **matched** |
| Large-channel warning | HuddleShoutyRoosterModal over 150 members, `Start a huddle in this channel?` | same | **matched** |
| Start a Huddle picker | HuddleInviteStartModal from the Create menu and the Huddles page | same | **matched** |
| Join preview | the join preview with mic, camera and device pickers (huddles_show_join_preview) | preview.go | **open**: not re-audited. The camera self-preview belongs to the background-blur work on `bg-blur`, so preview.go was left alone |
| Joining | HuddleLoading (`render ce74`): Slack's spinner over `Joining huddle…` | the text `Joining the huddle…` | **fixed** |
| Join or start failure | HuddleErrorModal (`boot OAp3`, isJoinError): `Can’t join the huddle`, the reason's body (permission_denied, rooms_call_ended, else `You’ll have to go back and give it another try.`), OK | toasts `Couldn't join the huddle: <code>` and `Couldn't start the huddle: <code>` | **fixed** (errmodal.go) |
| A full huddle | room_max_participants: `This huddle already has {maxAmount} people, which is the maximum allowed.`, Got It | the code in a toast | **open**: the modal shows Slack's default body with Got It. Clack isn't sent the team's limit, so it can't fill `{maxAmount}` |
| Huddles page start and join failures | the same modal | toasts `Huddles need Slack's audio/video stack (Amazon Chime), which yoloslack doesn't support yet`, `Open a channel or DM and click Huddle in its header to start one`, `Couldn't start the huddle: …` | **fixed**: views calls the ui root's HuddleJoinFailed; New huddle without a conversation opens Start a Huddle |

## Huddle window

| Feature | Slack (evidence) | Clack before | Status |
|---|---|---|---|
| Title | HuddleMvaWindowTitle (`render zuQH`): headphones, 8px, `Huddle in {channel}` with no `#` (publicChannelIncludeHashSign false), `Huddle with {names}` in a DM or group DM, then the subtitle `{n} people` / `{n} person` in regular weight (not in DMs), at `.p-multi_view_window_title`'s .8 opacity | `Huddle in #channel`, a `Connecting… ` or `Reconnecting… ` prefix, no subtitle | **fixed** (windowTitle; the subtitle is also a `subtitle` prop for plugins) |
| Title from a huddle topic | the topic replaces the title when set | none | **open**: Clack has no huddle topics |
| Toolbar frame | HuddleToolbar (`shared 9d6D`), expanded mode: 56px tall, center buttons in one row, HuddleToolbarLeaveButton in rightButtons, 2px from the edge | 60px, Leave in the centered row | **fixed** |
| Toolbar buttons | `.button`: 40px, 4px margin, radius 8; center order mute, camera, screen share, sticker pair, emoji, canvas, thread, Meeting Assistant, ⋯ | mic, deafen, camera, screen, hand, caret, react, captions, thread | **fixed**: mic, camera, sticker pair, emoji, thread, ⋯. Screen share is the row below; canvas and Meeting Assistant aren't drawn because Clack has no huddle canvases or Slack AI |
| Deafen | no such control: the word doesn't occur in boot-shared, boot-render, client-boot or the huddle chunks | a deafen button in the window and in the sidebar bar | **fixed**: removed from both |
| Captions button | captions live in ⋯ › Show captions (CaptionsSubmenu); the toolbar has none | a CC button and the note `Captions show when someone in the huddle turns on transcription` | **fixed**: removed, the menu has them |
| Mic button | HuddleToolbarMuteButton (`shared Q9S9`): tooltip `Mute mic` / `Unmute mic` with Mod+Shift+Space, `Microphone permission denied` and a warning-filled badge when denied | no tooltip | **fixed** (tooltips; the badge was already there) |
| Mic device menu | in the window the mic is segmented (36px plus a 28px `Audio settings` caret): `Microphone` and `Speaker` device lists, noise suppression, Audio & video preferences (`shared LmPT`) | a single 40px button | **open**: internal/huddle opens the sound card when you join and can't switch devices mid-huddle, so the lists would only take effect next time |
| Camera button | HuddleToolbarCameraButton (`shared LnqA`): segmented with a `Video settings` caret; `Turn on camera` / `Turn off camera` | window_camera.go's button and the tooltip `Camera isn't supported on this platform yet` | **open**: camera devices and backgrounds are the camera and background-blur work; window_camera.go wasn't touched. The unsupported tooltip isn't Slack's |
| Screen share | HuddleToolbarScreenshareButton (`shared Q9Yv`): `Share your screen`, `Stop sharing screen`, `Screen sharing limit reached` | a button whose click said `Clack can't share your screen yet` | **fixed**: the stub is gone. Sharing your own screen stays **open**: internal/huddle has no screen capture |
| Sticker pair | HuddleToolbarSegmentedStickerPickerButton (`shared zeVD`): a 36px main half and a 28px caret, a 1px otl-inv-pry divider 20px tall; `Raise hand`, `Lower hand`, `Remove sticker`, `Choose sticker` | 40px plus a 20px caret 8px apart | **fixed** (segFill paints each half once) |
| Emoji button | HuddleToolbarEmojiPickerButton (`shared dltD`): `emoji`, `emoji-filled` on hover or while open, `Add reaction` | `emoji`, no tooltip | **fixed** |
| Emoji picker | HuddleQuickEmojiPicker (`shared Vw0B`): tabs Reactions (the emoji picker), Effects, Stickers, GIFs, and a close button | a row of 8 quick emoji over the stage | **open**: Effects need Slack's Lottie packs and GIFs need GIPHY; the full emoji picker isn't available inside the huddle window |
| Thread button | HuddleToolbarThreadButton (`shared ZMXj`): `threads` (`threads-filled` while open), `Open thread` / `Close thread` | `threads`, no tooltip | **fixed** (tooltip) |
| Thread view | the huddle thread opens in the huddle window's side panel (client-huddle-thread-view) | opens the thread in the main window | **open**: no flexpane in the huddle window |
| ⋯ button | HuddleToolbarMoreActionsButton (`shared RXWR`): `ellipsis-vertical-filled`, `More actions`, its menu above it | missing | **fixed** |
| ⋯ menu rows | `shared a+g1`, expanded window: Invite people, Copy huddle link, `Go to #channel` / `Go to direct message`, divider, Add topic, Choose a huddle theme, Layouts ›, Play music ›, Huddle language ›, divider, Show captions › (`Display as…`, Closed captions, Side-by-side), Hide self-view, divider, Huddle preferences | none | **fixed**: Copy huddle link, Go to…, Show captions › Display as… / Closed captions, Hide self-view (with someone else there), Huddle preferences (opens Preferences › Audio & video) |
| ⋯ rows Clack lacks | as above | none | **open**: Invite people (no in-huddle invite call), Add topic (no topic API), Choose a huddle theme (no huddle themes), Layouts (no layout modes), Play music (no jukebox), Huddle language (no transcription language), Side-by-side captions (no side panel) |
| Hide self-view | isHidingSelf: your tile leaves the gallery; the banner `Self-view is hidden.` with Undo | missing | **fixed** (shared with the docked bar) |
| Leave button | `c-button--medium c-button--danger`, min-width 80px, `Leave`; tooltip `Leave Huddle` with Mod+Shift+H / Mod+Alt+Shift+H | 84px, no tooltip | **fixed** |
| Leave options | HuddleLeaveOptionsMenu (`shared OsUf`): a caret with `End huddle for all` where you may end it | none | **open**: Clack can't end a huddle for everyone |
| Participant tiles | HuddlePeerTile (`shared pluj`): 12px corners, a 1px ring, cover video or avatar, mic pill, name pill on hover, sticker pill, reaction pill, invite status | same (tiles-* shots) | **matched** |
| Speaking ring | `.p-huddle_peer_tile__active_speaker`: 3px white outline and white glow; the active_speaker bars in the mic pill | same | **matched** |
| Gallery layout | HuddlePeopleGridView and GridPicker: Slack's grid templates and pagination | Clack's own fit of square tiles up to 220px | **open**: Slack's grid templates aren't ported |
| Reaction effects | HuddleReactionEffects, the explosion and float-up layers | the tile's pill only | **open**: Lottie animations |
| Screen share view | HuddleScreenshareView with its tile list, zoom control and top toolbar (fullscreen, pop-out) | the share on the stage, people in a column, `Loading screen share…` while the first frame comes | **open**: zoom, pop-out and Slack's layout aren't ported; the loading text isn't Slack's |
| Closed captions | HuddleClosedCaptions (`shared PWaJ`, events from `boot mvRv`): a speaker's words within 2 s join one event, the last 5 show; each its own black box (radius 6, padding 4px 8px, 4px apart) with the bold name and a colon, 8px, then the words, both inline so wrapped words start at the box's left edge; at the bottom left, half the width at most; an event fades 5 s after its text last changed, hover shows them again | the last 2 lines in one centered bold box | **fixed** (captions.go) |
| Caption start | `Captions have been turned on.` when transcription starts | none | **open**: internal/huddle doesn't report the transcription start |
| Turning captions on | toggleCaptions starts the huddle's transcription | shows only what someone else's transcription sends | **open**: internal/huddle can't start transcription |
| Mute request | HuddlesInformationalBanner (`shared 7tWB`) `user_was_muted`: a neutral c-banner `Someone muted you.` with microphone-slash and a close button | toast `<name> muted you` | **fixed** (banner) |
| Speaking while muted | the same banner's `You’re muted.` | none | **open**: needs a speaking-while-muted signal internal/huddle doesn't give |
| Huddle ended or dropped | HuddleErrorModal: `This huddle has ended` / `This huddle has ended for everyone who was in it.` (Got It); otherwise `Something went wrong with your huddle` | toasts `The huddle ended` and `You were disconnected from the huddle` | **fixed** |
| Leaving on lock | HuddleErrorModal `DeviceLocked`: `You’re no longer in a huddle`, `When your computer locks, you automatically leave any huddle you’re in. You can change this in <a>Preferences</a>.`, OK | left silently | **fixed**. The `Preferences` link is plain text: Clack's modal body has no links |
| Other notices | none | `Couldn't update your sticker`, `Couldn't send the reaction` | **fixed**: removed |
| Lounge music | HuddleLoungeMusicBanner (`shared 4ahz`, `fHBk`): `You’re the only one in this huddle. Enjoy the tranquility.`, `Turn off music`, the song menu | the music plays (avprefs.go) without the banner or its controls | **open** |
| Window palette | `.p-huddle_window` is theme-base-inv-pry; buttons surf-inv-pry, hover surf-inv-ter, toggled base-pry | the palette measured on the 2026 web client by the earlier huddles work | **matched** (kept). In the light table (`858H`) theme-base-inv-pry is the light lilac rgb(249,237,255), so whether the window carries the inverted scope needs a live look, which this audit didn't take |

## Docked bar

Slack 2026 (IA4) docks the huddle at the bottom of the client while you're in one: HuddleBottomBar (`render wjeI`) in `.p-client__huddle_bottom_bar`, the client's bottom row under the tab rail and the workspace (zuQH's `huddleBottomBar` slot). The old sidebar footer card is gone from that layout.

| Feature | Slack (evidence) | Clack before | Status |
|---|---|---|---|
| Placement | the bottom row, padding 0 4px 4px; the rail and the workspace end above it | a card at the bottom of the sidebar | **fixed**: dock.go, wired into layoutShell and layoutBBM |
| Card | `.toolbarMainBar`: theme-surf-inv-sec, radius 12, three columns (1fr, 2fr, max-content), gap 16px, padding 8px 8px 8px 12px, 56px | a translucent card with a title row, faces and four buttons | **fixed** |
| Left, window closed | your 40px tile and the active speaker's or sharer's; the title (13px bold) over the subtitle (13px, content-inv-sec): `{a} and {b} are screensharing`, `{name} is screensharing`, `{name} is speaking`, `{n} people here`, `You’re the only one here`; none in a DM | `Huddle in #channel`, an elapsed timer and up to 7 faces | **fixed**. Slack's bar has no timer (Slick's CallTimer adds one) |
| Center | HuddleToolbarCenterButtons: mute, camera, screen share, sticker pair, emoji, canvas, thread, ⋯ | mic, deafen, open-window | **fixed**: mic, camera, sticker pair, emoji (opens the emoji picker), thread, ⋯ |
| Responsive center | useResponsiveHuddleToolbarButtons hides buttons in narrow windows | none | **open** |
| Sticker caret | the HuddleStickerGrid popover | none | **open**: Clack opens a menu of the same stickers, not Slack's grid |
| Right | `Open Huddle in new window` (open-in-window), Leave | an expand button and a 64px Leave | **fixed** |
| Collapsed, window open | `.toolbarMainBarControlsCollapsed`, 48px: headphones and the title in 15px bold, `Bring huddle to front`, Leave, 32px tall | the same card as above | **fixed** |
| Clicking the bar | opens the window, or brings it to the front | the title opened the window | **fixed** |
| Alerts | HuddlesAlertBanner with entryPoint bottom_bar, radius 12, 4px above the card | the mic warning row inside the card | **fixed**: Clack's mic warning sits above the card in that shape |

## Sidebar rows and the Huddles view

| Feature | Slack (evidence) | Clack before | Status |
|---|---|---|---|
| Row suffix | BaseChannelSuffixHuddleIcon (`render PZNh`): HuddleAvatarStackContent (`qgIY`: two 20px faces, 25% corners, each after the first at margin-left -2.235% of the stack's width and clipped by the stack's `--huddles-mask` clipPath, 8px after the name; hidden only while your huddle there is starting or ending: `activeHuddleChannelId === channelId && isHuddleTransitionState` (STARTING, PENDING, COMPLETING_PRE_JOIN, ENDING), so it stays while you're in it), HuddlesBadgeFakeMask (its own 2×20 path), HuddleSidebarLinkPeerCountBadge (`lw/3`: headphones and the count 2px apart, 13px bold, min-width 36px, 20px tall, padding 2px 6px, corners 0 4px 4px 0, 4px before what follows) | a 15px headphones icon and the count in the row's text colour | **fixed** (sidebar_suffix.go). karim-bugs-2-chrome: the faces were dropped for any huddle you were in (Karim's "# fallen" showed only the badge); now Slack's rule, the mask and the -2.235% step, and an unloaded face is the base-ter tile |
| Badge colours | theme-base-inv-hgl-1 with content-inv-pry: rgb(97,31,105) and white in the light client's sidebar (`jmNE`), rgb(125,57,134) and #f8f8f8 in dark (`7uI9`); on the selected row surf-inv with theme-content-hgl-1, or base-pry with content-pry for a huddle you're not in | the row's text colour | **fixed**. A stylesheet that sets the token wins. Custom account themes stay **open**: account.go doesn't derive theme-base-inv-hgl-1 |
| Face crescent | `--huddles-mask` clips the second face where it overlaps | plain overlap | **open** |
| Suffix tooltip | the participants, with the channel name | none | **open** |
| Collapsed section | SectionHuddleIndicator (`render 2LNF`): a huddle avatar stack on a collapsed section | none | **open** |
| Huddles link | enable_huddles_home_view | same | **matched** |
| Huddles page | huddles-page (`a29464bf75cbb4a4`): `Huddles` header and actions, the live huddles section (`Show more live huddles`), `Upcoming`, `Recent huddles` with Date / With / In / Has / Huddle type filters, groups Today, Yesterday, This week, Last week, Last month and older, rows like `Huddle for {n} minutes`, `Channel suggestions` and `Direct message suggestions` | `Happening now` (`Huddles in your channels and DMs`), `Recent` (`Huddles you were part of`), `New Huddle`, `Started …`, a green `Join` pill | **open**: views/huddles.go follows an older page; porting the 2026 page is its own job |
| Missed invites on the page | MissedHuddleInvite rows | none | **open** (the page) |

## Notifications and ringing

| Feature | Slack (evidence) | Clack before | Status |
|---|---|---|---|
| Invite window | HuddleInviteWindow (`render ce74`): its own 380×280 window, `Invite to huddle`, the facepile, `{sender}` or `{sender} in {channel}`, Decline / Be there soon / Join | same (invite-window shots) | **matched** |
| Invite sound | playHuddleInviteSound: huddle_invite_sound_v2 once, not with mute_huddle_sounds or Do Not Disturb, not while you're in a huddle | same | **matched** |
| Sidebar invite card | HuddleInvite under Do Not Disturb, or after the window closes unanswered | same | **matched** (it stays in the sidebar; the docked bar doesn't replace it) |
| OS notification and missed call | showHuddleNotification with Join and Decline; `Huddle missed` 30 s later | same | **matched** |
| Responses | rooms.inviteResponse; `Join when you’re ready!`, `Something went wrong responding to the invite.` | same | **matched** |

## Huddle message rows

| Feature | Slack (evidence) | Clack before | Status |
|---|---|---|---|
| huddle_thread rows | HuddleMsgText (`shared EKdW`) in `c-message__body--automated`: `{status}. {body}`, the body from getHuddleMsgText (`shared dbqw`) such as `{a} and {b} are here too.`, `{a} is already there.`, `You and {a} were in the huddle for {duration}.`, `You were in the huddle by yourself for {duration}. A nice, meditative session.`; a `Join Huddle` button with an avatar stack while it's live | a card: `Huddle in progress` or `A huddle happened`, `Lasted 10m · 2 participants`, the names | **open**: the row is internal/richtext's huddleBlock and richview's card, outside the huddle UI; porting Slack's plural sentences, the status line and the Join Huddle button needs those packages |
| Sender in a huddle | BaseHuddleStatus: the headphones by the name (qeNM) | same | **matched** |

## Counts

78 rows: 18 matched, 35 fixed, 25 open. Four fixed rows still name an open remainder in their cell: the caret's `More call options` wording, the facepile inside the live tooltip, sharing your own screen, and custom account themes for the sidebar badge.

The open rows and why:

| Open row | Reason |
|---|---|
| Call apps and calendar rows | no call apps or Google Calendar in Clack |
| Join preview | not re-audited: the camera self-preview belongs to the background-blur work on `bg-blur` |
| A full huddle | Clack isn't sent the team's participant limit for `{maxAmount}` |
| Title from a huddle topic | no huddle topic API |
| Mic device menu | internal/huddle can't switch the sound card mid-huddle |
| Camera button | camera devices, backgrounds and window_camera.go's unsupported tooltip are the camera and background-blur work |
| Emoji picker | Effects need Slack's Lottie packs, GIFs need GIPHY, the emoji picker isn't available inside the huddle window |
| Thread view | the huddle window has no side panel |
| ⋯ rows Clack lacks | no in-huddle invite, topics, huddle themes, layouts, jukebox, transcription language or side-by-side captions |
| Leave options | Clack can't end a huddle for everyone |
| Gallery layout | Slack's grid templates and pagination aren't ported |
| Reaction effects | Lottie animations |
| Screen share view | zoom, pop-out and Slack's share layout aren't ported; `Loading screen share…` isn't Slack's |
| Caption start | internal/huddle doesn't report the transcription start |
| Turning captions on | internal/huddle can't start transcription |
| Speaking while muted | no speaking-while-muted signal from internal/huddle |
| Lounge music | the banner and its controls aren't drawn |
| Responsive center | the dock doesn't hide buttons in narrow windows |
| Sticker caret | the dock opens a menu of stickers, not Slack's grid popover |
| Face crescent | the second face isn't clipped |
| Suffix tooltip | no tooltip on the row's faces and badge |
| Collapsed section | SectionHuddleIndicator isn't drawn |
| Huddles page | views/huddles.go follows an older page; porting the 2026 page is its own job |
| Missed invites on the page | part of the Huddles page |
| huddle_thread rows | richtext's huddleBlock and richview's card, outside the huddle UI |

## What changed

- huddleui/window.go: the toolbar (no deafen, captions or screen-share stub; Leave at the right; the segmented sticker pair; Slack's tooltips), the title and its subtitle, the 56px bar, Slack's loading state.
- huddleui/window_menu.go: the ⋯ menu, drawn with popups.Menu through a window-local host (its own theme and invalidation).
- huddleui/captions.go: closed captions, the informational banner, Joining huddle…, the window title.
- huddleui/errmodal.go: HuddleErrorModal for join failures and ended huddles.
- huddleui/dock.go, huddle_wire.go, app.go, bbm.go: the docked bar in the client's bottom row; bar.go keeps only the sidebar's invite card.
- huddleui/sidebar_suffix.go, sidebar.go: the row's huddle faces and badge.
- channel.go, huddleui/bar.go: the idle button's tooltips; the shortcut hints go through shortcut.Format.
- views/taut.go, views/huddles.go: the Huddles page's failures use the modal.
