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
| Every start path | the startHuddle thunk with Slack's entry points (DF8l's enum); startEventLog is the transcription (B3Qp), not the join | same (Begin), with the media join wrapped in startEventLog | **fixed** (exp-huddles): the join is no longer a startEventLog; startHuddle carries isMultiDevice |
| Large-channel warning | HuddleShoutyRoosterModal over 150 members, `Start a huddle in this channel?` | same | **matched** |
| Start a Huddle picker | HuddleInviteStartModal from the Create menu and the Huddles page | same | **matched** |
| Join preview | the join preview with mic, camera and device pickers (huddles_show_join_preview), only under huddles_preview_v2; no mic or camera permission forces it (computeShouldShowJoinPreview, kLPq gC); the pref row is aa02's, under the same experiment | preview.go, whatever the experiment | **fixed** (exp-huddles): shouldPreview and the pref row follow huddles_preview_v2, and missing permissions force it. The camera self-preview stays the background-blur work |
| Joining | HuddleLoading (`render ce74`): Slack's spinner over `Joining huddle…` | the text `Joining the huddle…` | **fixed** |
| Join or start failure | HuddleErrorModal (`boot OAp3`, isJoinError): `Can’t join the huddle`, the reason's body (permission_denied, rooms_call_ended, else `You’ll have to go back and give it another try.`), OK | toasts `Couldn't join the huddle: <code>` and `Couldn't start the huddle: <code>` | **fixed** (errmodal.go) |
| A full huddle | room_max_participants: `This huddle already has {maxAmount} people, which is the maximum allowed.`, Got It | the code in a toast | **open**: the modal shows Slack's default body with Got It. Clack isn't sent the team's limit, so it can't fill `{maxAmount}` |
| Huddles page start and join failures | the same modal | toasts `Huddles need Slack's audio/video stack (Amazon Chime), which yoloslack doesn't support yet`, `Open a channel or DM and click Huddle in its header to start one`, `Couldn't start the huddle: …` | **fixed**: views calls the ui root's HuddleJoinFailed; New huddle without a conversation opens Start a Huddle |

## Huddle window

| Feature | Slack (evidence) | Clack before | Status |
|---|---|---|---|
| Title | HuddleMvaWindowTitle (`render zuQH`): headphones, 8px, `Huddle in {channel}` with no `#` (publicChannelIncludeHashSign false), `Huddle with {names}` in a DM or group DM, then the subtitle `{n} people` / `{n} person` in regular weight (not in DMs), at `.p-multi_view_window_title`'s .8 opacity | `Huddle in #channel`, a `Connecting… ` or `Reconnecting… ` prefix, no subtitle | **fixed** (windowTitle; the subtitle is also a `subtitle` prop for plugins) |
| Title from a huddle topic | HuddleMvaWindowTitle shows the topic (the call's name) in place of "Huddle in …" | none | **fixed** (exp-huddles, topic.go): windowTitle uses the room's name |
| Toolbar frame | HuddleToolbar (`shared 9d6D`), expanded mode: 56px tall, center buttons in one row, HuddleToolbarLeaveButton in rightButtons, 2px from the edge | 60px, Leave in the centered row | **fixed** |
| Toolbar buttons | `.button`: 40px, 4px margin, radius 8; center order mute, camera, screen share, sticker pair, emoji, canvas, thread, Meeting Assistant, ⋯ | mic, deafen, camera, screen, hand, caret, react, captions, thread | **fixed**: mic, camera, sticker pair, emoji, thread, ⋯. Screen share is the row below; canvas and Meeting Assistant aren't drawn because Clack has no huddle canvases or Slack AI |
| Deafen | no such control: the word doesn't occur in boot-shared, boot-render, client-boot or the huddle chunks | a deafen button in the window and in the sidebar bar | **fixed**: removed from both |
| Captions button | captions live in ⋯ › Show captions (CaptionsSubmenu); the toolbar has none | a CC button and the note `Captions show when someone in the huddle turns on transcription` | **fixed**: removed, the menu has them |
| Mic button | HuddleToolbarMuteButton (`shared Q9S9`): tooltip `Mute mic` / `Unmute mic` with Mod+Shift+Space; under huddles_av_button_permission_handling a denied OS permission greys it (micDisabled), shows the warning-filled badge and `Microphone permission denied`, and a click opens DeviceWarningModal (`Microphone access required`) | tooltips; the badge for every microphone problem | **fixed** (exp-huddles, micalerts.go): the badge, grey and tooltip only for a denied permission under the experiment; the click opens DeviceWarningModal; the icon follows the mute state |
| Mic device menu | in the window and the bottom bar the mute button is segmented (36px plus a 28px `Audio settings` caret, Q9S9): HuddleAVDevicesMenu (LmPT): `Microphone` (gmEJ radio rows), `Speaker` (QdEk rows, a pick plays Test Speaker), `Test Speaker` (complete_quest_requirement), `Enable noise suppression` under huddles_noise_suppression_awareness while it's off, `Audio & video preferences` (yBJd); a pick is setNewPreferredDevice | a single 40px button; internal/huddle opened the sound card once | **fixed** (exp-huddles, audiomenu.go): the segmented button and the menu in the window and the dock; Session.SwitchAudio reopens the chosen speakers or microphone mid-huddle |
| Camera button | HuddleToolbarCameraButton (`shared LnqA`): segmented with a `Video settings` caret; `Turn on camera` / `Turn off camera` | window_camera.go's button and the tooltip `Camera isn't supported on this platform yet` | **open**: camera devices and backgrounds are the camera and background-blur work; window_camera.go wasn't touched. The unsupported tooltip isn't Slack's |
| Screen share | HuddleToolbarScreenshareButton (`shared Q9Yv`): `screen` / `screen-off`, `Share your screen`, `Stop sharing screen`, `Screen sharing limit reached` (getRemotePeersWithScreenshareEnabledCount > 1, no click); lit while sharing or picking; the click is 0S5z's useHandleScreenShareButtonClicked | a button whose click said `Clack can't share your screen yet` | **fixed** (branch screenshare: huddleui/share.go): the button after the camera in the window and the dock with Slack's icons, tooltips, lit state and limit; the click follows Slack's order (Screen Recording check, limit, stop while sharing, else the picker). See "Sharing your screen" below |
| Sticker pair | HuddleToolbarSegmentedStickerPickerButton (`shared zeVD`): a 36px main half and a 28px caret, a 1px otl-inv-pry divider 20px tall; `Raise hand`, `Lower hand`, `Remove sticker`, `Choose sticker` | 40px plus a 20px caret 8px apart | **fixed** (segFill paints each half once) |
| Emoji button | HuddleToolbarEmojiPickerButton (`shared dltD`): `emoji`, `emoji-filled` on hover or while open, `Add reaction` | `emoji`, no tooltip | **fixed** |
| Emoji picker | HuddleQuickEmojiPicker (`shared Vw0B`): tabs Reactions (the emoji picker), Effects, Stickers, GIFs, and a close button | a row of 8 quick emoji over the stage | **open**: Effects need Slack's Lottie packs and GIFs need GIPHY; the full emoji picker isn't available inside the huddle window |
| Thread button | HuddleToolbarThreadButton (`shared ZMXj`): `threads` (`threads-filled` while open), `Open thread` / `Close thread` | `threads`, no tooltip | **fixed** (tooltip) |
| Thread view | the huddle thread opens in the huddle window's side panel (client-huddle-thread-view) | opens the thread in the main window | **open**: no flexpane in the huddle window |
| ⋯ button | HuddleToolbarMoreActionsButton (`shared RXWR`): `ellipsis-vertical-filled`, `More actions`, its menu above it | missing | **fixed** |
| ⋯ menu rows | `shared a+g1`, expanded window: Invite people, Copy huddle link, `Go to #channel` / `Go to direct message`, divider, Add topic, Choose a huddle theme, Layouts ›, Play music ›, Huddle language ›, divider, Show captions › (`Display as…`, Closed captions, Side-by-side), Hide self-view, divider, Huddle preferences | none | **fixed**: Copy huddle link, Go to…, Show captions › Display as… / Closed captions, Hide self-view (with someone else there), Huddle preferences (opens Preferences › Audio & video) |
| ⋯ rows Clack lacks | as above | none | **fixed**: Add topic / Edit topic (HuddleEditTopicMenuItem, HuddleTopicDialog: `Add a topic` / `Edit topic`, Add / Save, `What is this huddle about?`, 250 characters; screenhero.rooms.rename). **open**: Invite people (inviteMembers is rooms.notifyMember per person; its `Invite people to your huddle` picker isn't built), Choose a huddle theme (no huddle themes), Huddle language (no transcription language), Side-by-side captions (no side panel). Layouts and Play music need huddles_new_grids and huddles_music, which this account isn't in, so Slack hides them too |
| Hide self-view | isHidingSelf: your tile leaves the gallery; the banner `Self-view is hidden.` with Undo | missing | **fixed** (shared with the docked bar) |
| Leave button | `c-button--medium c-button--danger`, min-width 80px, `Leave`; tooltip `Leave Huddle` with Mod+Shift+H / Mod+Alt+Shift+H | 84px, no tooltip | **fixed** |
| Leave options | HuddleLeaveOptionsMenu (`shared OsUf`): a caret with `End huddle for all` where you may end it | none | **open**: Clack can't end a huddle for everyone |
| Participant tiles | HuddlePeerTile (`shared pluj`): 12px corners, a 1px ring, cover video or avatar, mic pill, name pill on hover, sticker pill, reaction pill, invite status | same (tiles-* shots) | **matched** |
| Speaking ring | `.p-huddle_peer_tile__active_speaker`: 3px white outline and white glow; the active_speaker bars in the mic pill | same | **matched** |
| Gallery layout | HuddlePeopleGridView and GridPicker: Slack's grid templates and pagination; the order (S5fM): you, raised hands (by field_timestamp_ms, or the raised hand queue), screen sharers, cameras, the rest | Clack's own fit of square tiles up to 220px, you first | **fixed**: the order (galleryOrder). **open**: the grid templates and pagination |
| Reaction effects | HuddleReactionEffects, the explosion and float-up layers | the tile's pill only | **open**: Lottie animations |
| Screen share view | HuddleScreenshareView with its tile list, zoom control and top toolbar (fullscreen, pop-out under huddles_screenshare_popout: HuddleScreensharePopoutView with the presenter, `Go full screen`, `Pop back into huddle window`, `Close screen share window` and the huddle toolbar) | the share on the stage, people in a column, `Loading screen share…` while the first frame comes | **fixed** (exp-huddles, popout.go): `Open in new window` and the popout window. **open**: zoom, the drawing toggle, Slack's share layout; the loading text isn't Slack's |
| Closed captions | HuddleClosedCaptions (`shared PWaJ`, events from `boot mvRv`): a speaker's words within 2 s join one event, the last 5 show; each its own black box (radius 6, padding 4px 8px, 4px apart) with the bold name and a colon, 8px, then the words, both inline so wrapped words start at the box's left edge; at the bottom left, half the width at most; an event fades 5 s after its text last changed, hover shows them again | the last 2 lines in one centered bold box | **fixed** (captions.go) |
| Caption start | `Captions have been turned on.` (HuddleClosedCaptionStartEvent) is startEventLog's START_TRANSCRIPTION, pushed after rooms.startTranscription | none | **fixed** (exp-huddles, captions.go): the start event shows first, without a name |
| Turning captions on | toggleClosedCaptions runs startEventLog (rooms.startTranscription with the call id, once more after 5 s on permission_denied) or stopEventLog (rooms.stopTranscription); allowHuddleTranscriptions gates it; only an enabled event log keeps transcripts; huddles_cc_by_default runs it as your audio connects; stopHuddle runs stopEventLog first | shows only what someone else's transcription sends | **fixed** (exp-huddles, transcribe.go, captions_log.go). Grid org-level transcription prefs aren't sent to Clack: before team prefs load (or without the pref) Clack allows it |
| Mute request | HuddlesInformationalBanner (`shared 7tWB`) `user_was_muted`: a neutral c-banner `Someone muted you.` with microphone-slash and a close button | toast `<name> muted you` | **fixed** (banner) |
| Speaking while muted | useHuddleMutedVolumeTracker (owyz): muted, a level (computeStrength/6) of .065 or more four updates in a row raises SpeakingWhenMuted; HuddlesInformationalBanner (7tWB) shows `You’re muted.` (microphone-slash) unless closed in this call, and hides it for good after 60 s | none | **fixed** (exp-huddles, devices.go, micalerts.go). Clack samples the level every 300 ms (AudioLevelMonitor's cadence); the in-call SDK's own cadence isn't in the build |
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
| Huddles page | huddles-page (`a29464bf75cbb4a4`, Igs2): a list of the hero, the live huddles (HuddlesActiveSection, `Show more live huddles`) or DM suggestions, `Upcoming` (calendar), `Recent huddles` with Filters (Date, With, In, Has, Huddle type), ended huddles only, day groups (Today … Last month and older) under huddle_page_enhancements, `Loading older huddles…`, channel suggestions when there's no history | `Happening now` (`Huddles in your channels and DMs`), `Recent` (`Huddles you were part of`), `New Huddle`, `Started …`, a green `Join` pill | **open**: not rewritten on exp-huddles; the composition above is mapped from Slack's code, the rows and suggestions live in modules this branch didn't port |
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
| huddle_thread rows | HuddleMsgText (`shared EKdW`) in `c-message__body--automated` (sk_foreground_max_solid), the body from getHuddleMsgText (`shared dbqw`): `{a} is already there.`, `{a} and {b} are here too.`, `You’re the only one here. Enjoy the tranquility, or invite someone.`, `{user} has received a notification to join`, `You and {a} were in the huddle for {duration}.`, `You were in the huddle by yourself for {duration}. A nice, meditative session :tea:`, `{a} and {b} were in the huddle for {duration}.` ({h}h {m}m / {m}m), the long forms under system_messages_redesign_m1; a `Join Huddle` button (outline, plt-jade-20 border, three faces) while it's live and you're not in it (startHuddle, huddle_message_button) | a card: `Huddle in progress` or `A huddle happened`, `Lasted 10m · 2 participants`, the names | **fixed** (exp-huddles: richtext/huddle.go, richview/huddle.go). **open**: the header's LIVE / MISSED badge (the messages package's sender line), the status prefix of redesigned system rows, the `and N others` tooltip, the AI notes lines |
| huddle_thread row around it | getMsgSenderEntity (`client-boot 33X/`): a huddle message's sender is `{username: IQT5's status, isHuddleMsg, huddleActivityStatus}`, never its poster (Slackbot). With system_messages_redesign_m1 on (the account's group) Message (`shared NnKv`) and ThreadRootGeneric (`3ZeZ`) make it a redesigned system row: no sender line; HuddleSystemMessageIcon (`vXN3`, 16px headphones, content-hgl-2 while live); SystemMessage (`I0jQ`) with `{status}. {sentence}` (the status bold in a thread root), the time, and the compact Join Huddle (`c-button--small c-button--primary`, no faces) 4px under it. Off: the status as a span sender (`huddle_message_user_name`, no APP badge), renderStatusBadge's LIVE / MISSED (`K+HP`), HuddleMsgAvatar's 36px tile (`vjFA`). Huddle messages never group | Slackbot as the sender over the card | **fixed** (branch karim-bugs-2-msgs: messages/huddle_msg.go, system.go, row.go; TestHuddleMessageRowLikeSlack, huddle-rows-* shots in messages-audit-shots) |
| Sender in a huddle | BaseHuddleStatus: the headphones by the name (qeNM) | same | **matched** |

## Sharing your screen (branch screenshare)

Slack build `4d0c78085232` (2026-11-11 cache). Nothing real was shared or joined: the fake Chime (`huddle.NewFake`, `fake_content.go`) and the synthetic test screen (`CLACK_HUDDLE_TESTSCREEN`) only.

| Feature | Slack (evidence) | Clack before | Status |
|---|---|---|---|
| Permission check | useHandleScreenShareButtonClicked: maybeCheckSystemMediaAccess `screen` (`sGNa e$`) denied raises the `noSystemScreenShare` alert (`vAUZ`, priority 300) and runs getScreenThumbnails, which makes macOS ask | none | **fixed** (share.go): CGPreflightScreenCaptureAccess, the alert, CGRequestScreenCaptureAccess. Other systems have no such permission, as in Slack |
| Permission alert | useWarningBannerAlertText (`NoKD`): macOS desktop `<strong>Allow screen recording access in <a>system preferences</a></strong>. If you’ve already granted permissions, quit and reopen Slack.`, else `Please check your system preferences to give screen share access to Slack.`; `warning` icon, dismissible | none | **fixed**: the huddle window's warning banner and the dock's bottom_bar alert, the link opening Privacy › Screen Recording; Clack says its own name |
| Busy capture | ContentShareManager's NotReadableError → `ScreenShareNotReadableError`: `You may be screensharing in another app. Try closing other apps or restarting Slack.` | none | **fixed** (the same banner). screenShareError and ScreenShareNotFoundError have no text in NoKD, so nothing shows, as in Slack |
| Picker | ScreenPickerModal (`0S5z`): maxWidth 800, no title, the header's SourceSegmentedButton (`D7xu`: `Share entire screen`, `Window`; .item 165px, base-sec, selected base-pry with otl-ter), content 400px | none | **fixed** (screenpicker.go, popups.Modal's new Header) |
| Picker grid | ScreenPicker (`cXsV`): getColumnCount (2 for screens, 3 for windows, 2 under 800px), legacy spacing with app groups off, tiles 1px border, radius 4, padding 4/4/8, 140px thumbnails (205 for wide two-column screens), 13px title; hover base-hgl-1-hover; selected otl-hgl-1, surf-hgl-1, content-hgl-1, bold; a second click unselects | none | **fixed**. Arrow-key navigation between tiles isn't ported |
| Picker sources | calls.getScreenPreviewThumbnails `{width: 346, height: 260}` (`2kEl`, `mTT/`): screens first, ids normalized to `:0`, Stage Manager's pseudo-windows filtered (`aOZQ`); no screens closes it with noSystemScreenShare | none | **fixed** (internal/huddle share.go): macOS ScreenCaptureKit (thumbnails on 14+), Windows monitors and WebRTC's window rules, X11 RandR monitors and the window manager's clients |
| Share and Cancel | Share (primary, disabled until a pick) runs toggleScreenShare with `mediaSourceId` and `shareAudio`; Cancel and the close button close it; the border goes when it closes unless Share closed it | none | **fixed** |
| Border | BIqb: createBorderWindow, width 6, colour (0, 255/265, 0.2, 1), showBorder on a pick and while sharing, hideBorder after; desktop only, never Linux | none | **fixed** on macOS (a click-through window above everything, out of captures, following a shared window) and Windows (a layered topmost window, WDA_EXCLUDEFROMCAPTURE); none on Linux, as Slack |
| Content attendee | DefaultContentShareController: `<attendee>#content`, join token + `#content`, its own JOIN, INDEX, SUBSCRIBE (DUPLEX, the audio stream, the screen as stream 2 / group 2, 15 fps, 1500 kbps), LEAVE on stop; JOIN's disablePeriodicKeyframeRequestOnContentSender (m8Tb) | none | **fixed** (content.go): H.264 through videosend.go's encoder (now shared with the camera), keyframes on PLI/FIR, REMB; system audio or silence. Content attendees no longer make tiles (realtimeAttendeeIdPresenceCallback) |
| Capture | ContentShareManager: 15 fps, at most 2560px a side, content hint `text`, system audio with echo cancellation | none | **fixed**: ScreenCaptureKit (macOS 12.3+, cursor, system audio on 13+), GDI BitBlt / PrintWindow (Windows, cursor), MIT-SHM (X11, cursor from XFixes) |
| Encoding | Chromium's encoders (content prefers AV1, then H.264) | VideoToolbox for the camera only | **fixed**: VideoToolbox (macOS), Media Foundation's encoder MFT (Windows), OpenH264 when installed (Linux, its screen-content mode); without one, the pure-Go encoder at 640×360 |
| Peer messages | startScreenShare: ContentShareDetails `{sourceType: desktop:screen / desktop:window, itemId}` and selfDrawEnabled (Capabilities.isDrawEnabled) | none | **fixed**: isDrawEnabled is sent false (Clack shows a presenter no drawings; Slack's Linux app sends false too) |
| Your share on the stage | the SDK binds your stream to a content tile (contentShareTile) | none | **fixed**: State().Videos has `self#content`, unmirrored |
| Mini panel | HuddleMiniPanel (`render zuQH`, HuddleMiniPanelMounter): 620×60, frameless, on top (`screen-saver`, relative level 1), on every desktop, content protection, bottom centre of the shared screen 100px up, sk-client-theme--dark; opened hidden and shown with `showInactive`, `acceptFirstMouse`, on macOS `type: "panel"` and hiddenInMissionControl; `.p-huddle_mini_panel` is `-webkit-app-region: drag` (buttons `no-drag`) with onDoubleClick focusing the main window; HuddleActivity, mute, video, host draw, Stop Sharing | none | **fixed** (minipanel.go, Gio's `app.Panel`: third_party/gioui.org/CLACK_PATCHES.md §7): all but the host draw button. It shows without taking the focus; on macOS a click doesn't activate Clack and the first one presses the button; the background moves it (X11 through `_NET_WM_MOVERESIZE`, new in Clack's Gio), so a double click reaches the page only on a button, as in Slack. X11 places it and keeps it above; Wayland's compositor places and focuses it |
| Presenter drawing | HostDrawWindow and the mini panel's `Draw on your own screen` (macOS, Windows) | none | **open**: no host draw window |
| Wayland | `window.slackPlatform.isWayland` skips the picker for the desktop portal | none | **open**: no portal capture; a Wayland session gets the no-screens alert |

## Huddle experiments (branch exp-huddles)

The account's huddle experiments (docs/EXPERIMENTS.md), against Slack build `4d0c78085232` (2026-11-10). Each test sets the group itself, as the Experiments plugins can, and checks both sides.

| Feature | Slack (evidence) | Clack before | Status |
|---|---|---|---|
| huddles_alone_timeout | `M3Ec` HuddleAloneTimeoutTimer (every 5 s and on peer joins and leaves), `iKHp` getHuddleAloneTimeoutStep: 10 minutes with no remote peer (30 s under huddles_alone_timeout_shortened) opens HuddleStillHereModal: `Are you still there?`, `You’ve been in this huddle by yourself for a while. You’ll leave it in {seconds} seconds unless you’re still here.`, `I’m Still Here` / `Leave Huddle`; at the 60 s (15 s) deadline you leave | none | **fixed** (alone.go): the modal opens in the huddle window when it's open (winHost overlays), else the main window |
| huddles_multi_device | `e5et` startHuddleWithConfirmation and `6e8N` HuddleMultiDeviceModal: in a huddle you're already in on another device, your faces (one per device) and the others' (56px, six at most), `You’re in this huddle on a different device` / `… on multiple devices`, `Use both devices` / `Switch to this device` (rooms.join multidevice); `T3hK` keeps awareness per device | rooms.join always multidevice=false; stickers per tile | **fixed** (multidevice.go, awareness.go). The modal's footer is right-aligned (Slack centres it): popups.Modal has no centred footer |
| huddles_switch_microphone_helper | `owyz`: with two or more microphones and someone else there, 20 one-second checks of an unmuted microphone that hasn't sent sound raise NoAudioDetected (`NoKD`: `We are not detecting any audio from your microphone. Click here to switch to a different microphone`, microphone-slash) | none | **fixed** (micalerts.go): the link opens the Audio settings menu |
| huddles_missed_notification | `zuQH` handleHuddleMissedCallNotification (`Huddle missed`, 30 s on, only with it on); `zq6z` getDidUserMissHuddleInvite | the notification always | **fixed** (invite.go) |
| huddles_miles_davis | `IeZG` getSongsMetadata: So What, Freddie Freeloader, All Blues enabled only with it on; off, a pick of one plays Smooth Jazz | always Smooth Jazz for them | **fixed** (avprefs.go). Its effects, NEW badges and coachmark aren't drawn: Clack has no music menu or effects |
| huddle_join_from_preview | `zq6z` doesChannelSupportHuddlesWithReason: a channel you're previewing supports huddles only with it on | the button in every channel | **fixed** (SupportsHuddles; channel.go passes it to headerFit) |
| huddles_awareness_timestamp | `T3hK` ChimeChannelBroadcaster sends field_timestamp_ms with it on; `OGIR` xP sorts raised hands by it | no timestamps | **fixed** (awareness.go; galleryOrder) |
| huddles_link_v2 | `YCI0`: the sidebar Huddles link's count badge of unviewed huddle invites, cleared 600 ms after the page opens | none | **open**: Clack's sidebar link has no facepile and Clack keeps no huddle invite notifications |
| huddles_discovery_calls | `GPML` CallMenu's `New: Try a huddle for quick informal discussions with your team.` (treatment) | none | **open**: no call apps menu |
| huddles_mva_window | the huddle window as a multi-view window (secondary views) vs the legacy window's flexpanes (~30 modules) | MVA only | **open**: Clack's window has neither secondary views nor flexpanes, so off and on look the same |
| huddles_screenshare_share_audio | `0S5z` ScreenPickerModal's `Share system audio` (shown when on, in the desktop app on macOS or Windows) | none | **fixed** (screenpicker.go): the checkbox when it's on and the capture can take system audio without Clack's own sound (macOS 13+: ScreenCaptureKit's excludesCurrentProcessAudio; Windows 10 build 20348+: WASAPI process loopback leaving Clack's process tree out); the share's content attendee then sends it as stereo Opus. Older Windows gets no checkbox (Slack captures all system audio there and cancels the echo) |

## Counts

105 rows: 17 matched, 68 fixed, 20 open. Fixed rows that still name an open remainder: the caret's `More call options` wording, the facepile inside the live tooltip, system audio on older Windows, custom account themes for the sidebar badge, the ⋯ menu (Invite people, themes, language, side-by-side), the gallery's grid templates, the screen share view's zoom and drawing, the huddle message's header badge and tooltip, Grid's org-level transcription prefs, and the in-call level cadence.

The open rows and why:

| Open row | Reason |
|---|---|
| Call apps and calendar rows | no call apps or Google Calendar in Clack |
| A full huddle | Clack isn't sent the team's participant limit for `{maxAmount}` |
| Camera button | camera devices, backgrounds and window_camera.go's unsupported tooltip are the camera and background-blur work |
| Emoji picker | Effects need Slack's Lottie packs, GIFs need GIPHY, the emoji picker isn't available inside the huddle window |
| Thread view | the huddle window has no side panel |
| Leave options | Clack can't end a huddle for everyone |
| Reaction effects | Lottie animations |
| Lounge music | the banner and its controls aren't drawn |
| Responsive center | the dock doesn't hide buttons in narrow windows |
| Sticker caret | the dock opens a menu of stickers, not Slack's grid popover |
| Face crescent | the second face isn't clipped |
| Suffix tooltip | no tooltip on the row's faces and badge |
| Collapsed section | SectionHuddleIndicator isn't drawn |
| Huddles page | views/huddles.go follows an older page; porting the 2026 page is its own job |
| Missed invites on the page | part of the Huddles page |
| huddles_link_v2 | no huddle invite notifications or link facepile in the sidebar |
| huddles_discovery_calls | no call apps menu |
| huddles_mva_window | no secondary views or flexpanes in the huddle window |
| Presenter drawing | no host draw window, so no "Draw on your own screen" and isDrawEnabled stays false |
| Wayland | the desktop portal (ScreenCast over PipeWire) isn't built; Wayland sessions have no capture |

## What changed

- huddleui/window.go: the toolbar (no deafen, captions or screen-share stub; Leave at the right; the segmented sticker pair; Slack's tooltips), the title and its subtitle, the 56px bar, Slack's loading state.
- huddleui/window_menu.go: the ⋯ menu, drawn with popups.Menu through a window-local host (its own theme and invalidation).
- huddleui/captions.go: closed captions, the informational banner, Joining huddle…, the window title.
- huddleui/errmodal.go: HuddleErrorModal for join failures and ended huddles.
- huddleui/dock.go, huddle_wire.go, app.go, bbm.go: the docked bar in the client's bottom row; bar.go keeps only the sidebar's invite card.
- huddleui/sidebar_suffix.go, sidebar.go: the row's huddle faces and badge.
- channel.go, huddleui/bar.go: the idle button's tooltips; the shortcut hints go through shortcut.Format.
- views/taut.go, views/huddles.go: the Huddles page's failures use the modal.

On branch exp-huddles (2026-11-10, Slack build `4d0c78085232`):

- internal/huddle: transcribe.go (startEventLog / stopEventLog: rooms.startTranscription, rooms.stopTranscription, the START_TRANSCRIPTION event, screenhero.rooms.rename for topics), devices.go (SwitchAudio, Slack's level and the speaking-while-muted rule, hasSelfAudioBeenDetected), awareness.go (field_timestamp_ms, per-device keying), rooms.go (rooms.join multidevice), the fake's new answers and SoloRoom.
- huddleui: experiments.go, alone.go, audiomenu.go, micalerts.go, captions_log.go, topic.go, popout.go, multidevice.go; the window's overlays (winHost.ShowOverlay), the segmented mute button in the window and the dock, galleryOrder, the topic in the title, the join no longer wrapped in startEventLog.
- richtext/huddle.go and richview/huddle.go: huddle_thread messages as HuddleMsgText with the Join Huddle button (richview.HuddleJoiner, messages' richHandler, the ui root's JoinHuddleFrom); store.HuddleTextOpts / UserAvatar and core's store.ExperimentGroup hook feed it.
- prefs_pages.go (the join preview row under huddles_preview_v2), channel.go (SupportsHuddles into headerFit), huddle_av.go (SetHuddleAudioPrefs).

On branch screenshare (2026-11-11, Slack build `4d0c78085232`; see "Sharing your screen"):

- internal/huddle: share.go (sources, the share's state, the test screen), content.go and fake_content.go (the `#content` attendee and the fake's side of it), screenimg.go (scaling and NV12), videosend.go (the H.264 publisher shared by the camera and the share), videoenc.go and h264enc_* (VideoToolbox's screen mode, Media Foundation, OpenH264), share_darwin.m/.go (ScreenCaptureKit, the border, the panel), share_windows.go and sysaudio_windows.c (GDI capture, the border, WASAPI process loopback), share_x11_linux.c/.go (XShm, RandR, XFixes); session.go (State().Share, `self#content`, content attendees out of the tiles), frames.go (JOIN field 13).
- huddleui: share.go (the button, the click, the alerts), screenpicker.go (ScreenPickerModal), minipanel*.go (HuddleMiniPanel); window.go, dock.go and controller.go wire them; popups/modal.go gained a header slot; icons: `screen-off`, `stop-screen-sharing`.
- third_party/gioui.org: `app.Panel` and X11 move areas (CLACK_PATCHES.md §7).
