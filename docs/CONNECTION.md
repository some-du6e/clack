# Connection states (branch connection-trouble, Slack build 889ac449a32d)

Karim, 2026-11-24: "you should put a warning or smth that says that your internet is dogshit so ppl dont blame clack". Slack's client has no slow-network notice. The only Slack text that blames a slow network is the desktop app's "Slack is having trouble loading" dialog, shown when the window hasn't loaded for 4 minutes, and Clack has no page load to time. What Slack has are two connection states, and Clack now shows both as Slack does: the realtime socket that keeps failing (troubleConnecting), and offline (wasRecentlyOnline, isClientOnline).

This branch is part (a). Part (b) is Slack's offline detection itself: counting API attempts with no answer, the CDN pixel probe and the api.test rounds. Until then clientOnline comes only from the interface watcher (core/online.go).

## Trouble connecting (the realtime socket)

- **The flag.** Slack's socket store (client-boot `5rVH`) `troubleConnecting` is set by the socket manager (boot-render `RpHI` `_maybeDispatchTroubleConnectingAction`), counting a connect attempt that ends in ERROR (`_leaveState`). Before any connection said hello it is the first failure; after one, the third failure in a row once 30 s have passed since the first of them (`_getConnectionTroubleThreshold` `3e4 * !!_hasConnected`, `_getConnectionAttemptCountTroubleThreshold` `_hasConnected ? 3 : 1`). CONNECTED clears it and the count, and so does the drop of a connected socket (`_handleDisconnectedSocket`); a failed connect doesn't. Clack: `slack.RTM.Trouble` and `OnTrouble`, core's `connection_trouble` event and `App.ConnectionTrouble`. Run's end clears it. With realtime off (Clack's default until the user opts in) there's no socket, so no trouble and no banner.
- **WAIT_FOR_CONNECTIVITY** (`RpHI`). After a failure's backoff the socket connects only while isClientOnline; otherwise it waits for the client to be online again, and the wait clears the failures' count (not the flag). So with no network trouble can't newly turn on, and the offline states show alone, never beside the DegradedBanner. Clack: `RTM.AwaitOnline`, which core sets to wait for clientOnline (part (b) makes it the network wait itself).
- **reconnectImmediately** (`zNCe`): a socket waiting out its backoff connects now. Clack: `RTM.ConnectNow`, which keeps the reconnect_url and does nothing while a socket connects or is up (`RTM.Reconnect` drops both).
- **The DegradedBanner** (boot-shared `7JQN`, namespace ms) in the message pane's top banners, the Threads view's and the Unreads view's (`messages.DegradedBanner`):
  - It reads "Last updated less than a minute ago…" and "Load new messages". From its mount, every 60 s it reads "Last updated {timeAgo}…" with toTimeAgo of the mount. Slack renders it only while troubleConnecting, so each trouble period mounts it afresh (`RTM.TroubleSince`), and so does a view entered again (its remount): a view off screen when the last period ended doesn't carry that period's time into the next.
  - "Load new messages" hides it for 10 s and runs loadNewMessages (core `DegradedLoad`). That reads the open conversation's newest history (`wy9s` requestOfflineHistory: not while the socket is up, and not for a conversation the store doesn't hold) and the replies of the thread open beside it (the thread pane drawn this frame or the last: the click lands before the thread pane lays out), each at most once a second. In the Threads view it reloads the threads. Then client.counts, at most once a minute, and reconnectImmediately. When that's done the time starts over.
  - The CSS under IA4: 28px, radius 14, padding 0 16px, 13px bold, sk_foreground_low_solid behind rgba(sk_foreground_max, .7), the link 8px after the text and underlined. In the message pane (14px down) and Threads (5px under the header) the top banners are centred shrink-to-fit boxes (`left: 50%; transform: translate(-50%)`), so the banner is a pill as wide as its text. Unreads' have no IA4 rule: 16px in from each side, 23px down, under a 27px sk_foreground_min_solid band, and the text ellipsizes ("…" after whole glyphs).
  - While it shows, the unread pill hides (`.p-message_pane__banner + .p-message_pane__banner`). While there's trouble the day pills don't stick (useStickyDayDividers' `!isDegradedMode`, the flag, so also through the 10 s hide).
  - Screen readers get "… Press F6 to navigate to the status banner and click load new messages" (the desktop app's wording). Clack records it as `DegradedBanner.Aria`; it has no announcer.

## Offline

- **The send gate** (boot-shared `CPUF` MessageInput.onEnter). While not recently online (`wasRecentlyOnline`), after the isSendingDisabled checks, Enter sends nothing, keeps the draft and toasts the bold "No connection." and "You’ll be able to send messages as soon as you’re back online." (msg_input). The toast is top-right, cloud-offline at 20, 6 s. SendButton (`Bv+q`) disables send and the schedule caret. An edit still saves: BaseEditMessage (`+rDF` maybeSave) reads no connectivity selector, and the API holds the call. Karim's 00:43Z report ("you can still attempt to send messages to slack, even tho it doesnt owrk") was this.
- **Top-right toasts** sit 14px from the window's right and 70 + ci() from its top: 22 on macOS (`p-client_desktop--frameless-macos`), 41 elsewhere (ToastConfigProvider `wO1/`, nWRK). Clack: `ToastPartsTopRight`.
- **OfflineChannelToast** (boot-render `RpHI`, offline_mode). A conversation opened in the primary view while !isClientOnline (onChannelChange at every setActiveChannel: from a page or another conversation, not a thread or permalink inside it) toasts "No connection." and "Last updated {clientLastOnline}." with toTimeAgo of lastClientConnection. When the store holds none of its history it says "Go online to get the latest messages." instead.
- **The indicators** (boot-shared `7yfC` InlineOfflineIndicator and `z2vV` LargeOfflineIndicator) take their view's caption (`messages.InlineOfflineIndicator`, `messages.LargeOfflineIndicator`):
  - The message list's and a thread's history loader rows show the inline one: "No connection." and "Go online to load more messages.".
  - An empty conversation shows the large one: "No connection" and "Go online to get the latest messages.".
  - The Threads view's paging row says "Go online to load more threads.", and the view with no threads says "Go online to get the latest threads.".
  - Unreads shows the large indicator before its channels are known, and the inline one on its loading rows.
  - The thread pane's own large indicator now uses the shared widget.
- **Slow loads** (the message-kit chunk's `q3Mq` MessageContainerLoading, namespace unread). The Threads view's first load shows "Loading threads…" and Unreads' shows "Loading unread messages…", each over Slack's uv-loader GIF at 200px (embedded in internal/slackasset). Each GIF frame is composed and scaled once at the drawn size and kept while the loader shows (3 s after its last draw), so a loop re-uses its textures. Five seconds after the load started they read "Still loading… sorry for the long wait!". The Threads paging row (`aQNV` LoadingMoreThreadsIndicator) reads "Loading more messages…", then the same after 5 s, each page from its own start.

## Not here

- Part (b): API attempt counting, the CDN pixel probe, api.test. Then holding every API call while offline (`7quX` _tick).
- The relatives:
  - the canvas offline and degraded banners;
  - presence away while not recently online;
  - the "No connection" tooltip lines;
  - the huddle button's "Go online to start a huddle";
  - the section menu and language pref reading core's online state;
  - Activity's row loader whenever offline;
  - the Searcher's local-only mode;
  - App details' and the dialogs' offline alerts;
  - the boot's "Connecting to Slack…" and WorkspaceBootFailure;
  - the composer line's 30 s announcer;
  - Slack's decorrelated socket backoff and stuck-state timers;
  - the day pill's whole useStickyDayDividers rule.
- Clack sends none of Slack's `_x_*` request metadata (`_x_mode` online, degraded or offline included), on purpose.
- Huddles' own network notices are out of scope.
