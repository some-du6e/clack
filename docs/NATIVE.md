# yoloslack native architecture (supersedes the web-UI parts of ARCHITECTURE.md)

The user's requirement (2026-11-06): **no JS, no web UI, no Electron. Native and fast.** yoloslack is a single Go binary with a GPU-rendered native window built on **Gio** (`gioui.org`, immediate mode). Every widget is custom-drawn to look like the 2026 Slack desktop client. The HTTP server, `web/` and the HTML renderer `internal/render` will be deleted once the native UI lands.

> **Rename (2026-11-07): the app is now Clack.** The binary is `cmd/clack`. CI artifacts are `clack-linux-amd64` (file `clack-linux-amd64`), `clack-windows-amd64` and `clack-darwin-arm64`; `yoloslack-linux-amd64` is no longer built, so update your download scripts. Config lives in `~/.config/clack`, and the first start copies over `~/.config/yoloslack`, which stays in place for older tools. Env vars are `CLACK_HOME`, `CLACK_TIMING`, `CLACK_FRAMELOG`, `CLACK_NOEMBOLDEN` and `CLACK_TYPINGLOG`; the `YOLOSLACK_*` names still work. The window title ends in ` - Clack`. The Go module path is unchanged.

## Scraping policy (Hack Club), non-negotiable
Message contents are memory only. Never write message text, files or attachments to disk, logs or fixtures from real messages; test fixtures must be synthetic. Only metadata may be cached on disk (the account's cache directory, `internal/store/cachedir.go`: 0600, deleted on sign-out): users, channel names, custom emoji map, prefs, and avatar/emoji images. (alpha.28's boot snapshot, which kept the last channel's messages, was replaced by a fast cold start; see docs/STARTUP.md.)

## Packages and owners
| package | owner (branch) | purpose |
|---|---|---|
| `internal/slack` | backend (`backend`) | HTTP client, realtime gateway, typed methods, login (pasted sessions; desktop sign-in with a magic link: `magic.go`, cookie jar `desktop.go`, `dpop.go`) |
| `internal/store` | backend | in-memory state, metadata disk cache |
| `internal/core` | backend | **in-process service API** for the UI (replaces `internal/server`): boot, reads, writes, typed events. No HTTP. |
| `internal/richtext` | richtext (`richtext`) | Slack message → structured document (`Doc`) for native layout. Replaces `internal/render`. |
| `internal/ui/...`, `internal/imgcache`, `cmd/clack`, `assets/fonts`, `.github/workflows` | ui (`ui`) | the Gio app |
| `internal/desktop`, `assets/icon`, `packaging/`, `.github/workflows/release.yml` | packaging (`packaging`) | single instance, update check, window/tray icons, slack:// registration, installers and the release workflow (see below) |
| `internal/huddle`, `internal/ui/huddleui`, `cmd/huddle-demo` | huddles (`huddles`) | native huddles: `rooms.join` → Amazon Chime signaling (hand-written protobuf) + pion WebRTC, Opus in/out (pure-Go libopus, miniaudio via cgo for the sound card), H.264 video in (pure-Go decoder), peer messages, captions; the huddle bar, header pill and huddle window. Tests run against `huddle.NewFake` (a loopback Chime); `internal/huddle/probe` joins a real huddle only with `-live` (it rings people). |

## `internal/richtext` contract
```go
package richtext

type Style uint16
const (Bold Style = 1 << iota; Italic; Strike; Code; Underline; Highlight)

type SpanKind uint8
const (Text SpanKind = iota; Link; User; Channel; Usergroup; Broadcast /* @here @channel @everyone */; Emoji; Date; Break /* hard line break */;
	ColorSwatch /* rich_text color: Text is "#RRGGBB", draw a swatch */; InlineImage /* context-block image: URL, Text=alt */)

type Span struct {
	Kind  SpanKind
	Text  string // display text, already unescaped and resolved ("@karim", "#general", "@here", formatted date); never contains "\n" (lines are split by Break spans)
	Style Style
	URL   string // Link: target. Emoji: image URL (custom or Slack CDN standard).
	ID    string // User/Channel/Usergroup id; Emoji: name (with ::skin-tone-N); Broadcast: here|channel|everyone; Date: unix seconds
	Self  bool   // User mention of the viewer, or a broadcast (yellow highlight)
}

type BlockKind uint8
// ImageBlock/AttachmentBlock/FileBlock: Go can't have a const and a type both named Image/Attachment/File.
const (Paragraph BlockKind = iota; Preformatted; Quote; List; Header; Divider; Context; Section; ImageBlock; Actions; AttachmentBlock; FileBlock; Tombstone)

type Block struct {
	Kind     BlockKind
	Spans    []Span   // inline content (Paragraph, Preformatted, Quote, Header, Context, Section text)
	Items    [][]Span // List items
	Ordered  bool; Indent int; Offset int; Border bool // List
	Fields   [][]Span // Section fields / attachment fields (Short flag in FieldShort)
	FieldShort []bool
	Image    *Image   // Image block, section accessory, attachment image/thumb
	Buttons  []Button // Actions block / section button accessory
	Att      *Attachment
	File     *File
	Children []Block  // nested blocks: attachment files; Section text with quotes/pre (then Spans is nil)
}
// Preformatted spans carry no Code style (the block itself is monospace). Emoji with URL=="" (unknown to the Resolver
// but with rich_text unicode) render as text. Tombstone: deleted message/file, Spans hold the notice.
type Image struct { URL string; Alt string; W, H int; Title string; Proxy bool /* needs the session cookie (files.slack.com) */ }
type Button struct { Text string; URL string; Style string /* primary|danger|"" */; ActionID, Value, BlockID string
	Kind string /* button|overflow|select|datepicker|timepicker|datetimepicker|input|checkboxes|radio_buttons */ }
type Attachment struct {
	Color string; Pretext []Span; ServiceName, ServiceIcon string; AuthorName, AuthorIcon, AuthorLink string
	Title []Span; TitleLink string; Text []Block; Footer []Span; FooterIcon string; TS int64
	Image *Image; Thumb *Image; IsUnfurl bool; IsShare bool; ShareChannel string /* name, no "#" */; FromURL string
	AuthorSubname string; ShareChannelID string; VideoURL string /* video block */
}
// Attachment blocks carry fields in Block.Fields/FieldShort (bold title, Break, value), legacy action buttons in
// Block.Buttons, and attachment files in Block.Children. Video blocks, call blocks and huddle_thread messages are
// also AttachmentBlocks (ServiceName "Call"/"Huddle"; huddle participants are User spans in Footer).
type File struct {
	ID, Name, Title, Mime, Type, PrettyType string; Size int64; URL, Download, Permalink string
	Thumb *Image /* best thumb for images/videos/pdfs */; IsImage, IsVideo, IsAudio, IsSnippet bool; Preview string; Lines int; DurationMS int
}
type Doc struct { Blocks []Block; Jumbo bool /* emoji-only, render emoji 32px */ }

type Resolver interface {
	UserName(id string) string; ChannelName(id string) string; UsergroupName(id string) string
	Emoji(name string) (url, text string, ok bool); SelfID() string
}
func FromMessage(r Resolver, m *slack.Message) Doc   // blocks if rich, else mrkdwn text; plus files and attachments
func FromMrkdwn(r Resolver, text string) []Block
func PlainText(d Doc) string                         // for copy, previews, notifications
// helpers: SpansText([]Span) string; FormatDate(t, "{date_short} at {time}") string (re-render Date spans: ID is unix secs);
//          SafeURL(u) string; SafeImage(u) string; NeedsProxy(u) bool (files.slack.com etc: fetch via core.Fetch)
```

## `internal/core` contract
The in-process service the UI calls (no HTTP). Every method is safe for concurrent use; call network methods off the UI goroutine. Types are in `internal/core/types.go` (plus `browse.go`, `manage.go`, `drafts.go`). The full exported surface is generated from the source:

```go
func (*App) ActiveHuddles(ctx context.Context) ([]Huddle, error)
func (*App) Activity(ctx context.Context, o ActivityOpts) (ActivityPage, error)
func (*App) AddTodo(ctx context.Context, ch, ts string, dateDue int64) error
func (*App) Archive(ctx context.Context, ch string) error
func (*App) Bookmarks(ctx context.Context, ch string) ([]Bookmark, error)
func (*App) Boot(ctx context.Context) error
func (*App) BootErr() error
func (*App) BrowseChannels(ctx context.Context, query string, page int, sort string) (ChannelPage, error)
func (*App) BrowseChannelsWith(ctx context.Context, o ChannelBrowseOpts) (ChannelPage, error)
func (*App) BrowsePeople(ctx context.Context, o PeopleBrowseOpts) (PeoplePage, error)
func (*App) ChannelInfo(ctx context.Context, ch string) (ChannelDetails, error)
func (*App) ClearActivity(ctx context.Context, items ...ActivityItem) error
func (*App) Close() error { return a.S.SaveCache() }
func (*App) Command(ctx context.Context, o CommandOpts) (CommandResult, error)
func (*App) Commands(ctx context.Context, query string) ([]Command, error)
func (*App) CompleteTodo(ctx context.Context, ch, ts string) error
func (*App) Conv(id string) (Conv, bool)
func (*App) ConvRef(id string) ConvRef
func (*App) Count(ch string) Count
func (*App) Counts() map[string]Count
func (*App) CustomEmoji() map[string]string { return a.S.CustomEmoji.Snapshot() }
func (*App) Delete(ctx context.Context, ch, ts string) error { return a.C.Delete(ctx, ch, ts) }
func (*App) DeletedFiles(ctx context.Context) (FilePage, error)
func (*App) DeleteDraft(ctx context.Context, id string) error
func (*App) DeleteScheduled(ctx context.Context, ch, id string) error
func (*App) DiscardPending(ch, clientMsgID string)
func (*App) DMs(ctx context.Context, cursor string) (DMPage, error)
func (*App) DND(ctx context.Context) (json.RawMessage, error)
func (*App) Drafts(ctx context.Context) ([]Draft, error)
func (*App) Edit(ctx context.Context, ch, ts, text string, blocks json.RawMessage) error
func (*App) Emoji(name string) (url, text string, ok bool) { return a.S.Emoji(name) }
func (*App) EnsureUsers(ctx context.Context, ids []string)
func (*App) Fetch(ctx context.Context, rawURL string) ([]byte, string, error)
func (*App) Files(ctx context.Context, o FilesOpts) (FilePage, error)
func (*App) History(ctx context.Context, ch, before string) (Page, error)
func (*App) HuddleHistory(ctx context.Context, cursor string) ([]Huddle, string, error)
func (*App) Invite(ctx context.Context, ch string, users ...string) error
func (*App) Join(ctx context.Context, ch string) (Conv, error)
func (*App) Leave(ctx context.Context, ch string) error
func (*App) Managers(ctx context.Context, ch string) ([]string, error)
func (*App) MarkActivityRead(ctx context.Context, items ...ActivityItem) error
func (*App) MarkActivityUnread(ctx context.Context, items ...ActivityItem) error
func (*App) MarkAllActivityRead(ctx context.Context, view string) error
func (*App) Mark(ctx context.Context, ch, ts string) (Count, error)
func (*App) MarkThread(ctx context.Context, ch, threadTS, ts string) error
func (*App) MarkUnread(ctx context.Context, ch, ts string) (Count, error)
func (*App) MemberCounts(ctx context.Context, ch string) (MemberCounts, error)
func (*App) Members(ctx context.Context, ch, marker string, peopleOnly bool) ([]User, string, error)
func (*App) MoveToSaved(ctx context.Context, ch, ts string) error
func (*App) MoveToTodos(ctx context.Context, ch, ts string, dateDue int64) error
func (*App) Mrkdwn(text string) []richtext.Block { return richtext.FromMrkdwn(a.S, text) }
func (*App) Mute(ctx context.Context, ch string, muted bool) error
func (*App) NotifyPrefs(ch string) ChannelNotify
func (*App) OpenDM(ctx context.Context, users ...string) (Conv, error)
func (*App) Pin(ctx context.Context, ch, ts string) error
func (*App) Pins(ctx context.Context, ch string) ([]Pin, error)
func (*App) Prefs() Prefs
func (*App) PrepareUpload(ctx context.Context, f UploadFile, progress func(sent, total int64)) (PreparedUpload, error)
func (*App) Presence(ctx context.Context, ids []string) map[string]string
func (*App) React(ctx context.Context, ch, ts, name string, add bool) error
func (*App) Ready() <-chan struct{} { return a.ready }
func (*App) Refresh(ctx context.Context, ch string) (Page, error)
func (*App) RefreshPrefs(ctx context.Context) (Prefs, error)
func (*App) RefreshReplies(ctx context.Context, ch, ts string) ([]Message, error)
func (*App) RemoveTab(ctx context.Context, ch, tabID string) error
func (*App) Rename(ctx context.Context, ch, name string) error
func (*App) Replies(ctx context.Context, ch, ts string) ([]Message, error)
func (*App) RescheduleMessage(ctx context.Context, id string, postAt int64) (ScheduledMessage, error)
func (*App) Resend(ctx context.Context, ch, clientMsgID string) (Message, error)
func (*App) Save(ctx context.Context, ch, ts string, dateDue int64) error
func (*App) Saved(ctx context.Context, filter, cursor string) (SavedPage, error)
func (*App) SaveDraft(ctx context.Context, o DraftOpts) (Draft, error)
func (*App) ScheduledMessages(ctx context.Context, ch string) ([]ScheduledMessage, error)
func (*App) ScheduleMessage(ctx context.Context, o SendOpts, postAt int64) (ScheduledMessage, error)
func (*App) SearchChannels(ctx context.Context, query string, count int) ([]ChannelHit, error)
func (*App) SearchMessages(ctx context.Context, o SearchOpts) (SearchResult, error)
func (*App) SearchUsergroups(ctx context.Context, o UsergroupOpts) ([]Usergroup, error)
func (*App) SearchUsers(ctx context.Context, query string, count int) ([]User, error)
func (*App) Self() User
func (*App) Send(ctx context.Context, o SendOpts) (Message, error)
func (*App) SendDraft(ctx context.Context, id string) (Message, error)
func (*App) Sent(ctx context.Context, cursor string, page int) (SentPage, error)
func (*App) SetDND(ctx context.Context, minutes int) error
func (*App) SetNotifyPrefs(ctx context.Context, ch string, n ChannelNotify) error
func (*App) SetPresence(ctx context.Context, presence string) error
func (*App) SetPurpose(ctx context.Context, ch, purpose string) error
func (*App) SetStatus(ctx context.Context, text, emoji string, expiration int64) error
func (*App) SetTopic(ctx context.Context, ch, topic string) error
func (*App) Snapshot() Snapshot
func (*App) Star(ctx context.Context, ch string, star bool) error
func (*App) Starred(ch string) bool
func (*App) Subscribe() (<-chan Event, func())
func (*App) SubscribePresence(ids []string)
func (*App) SubscribeThread(ctx context.Context, ch, threadTS string, follow bool) error
func (*App) Tabs(ctx context.Context, ch string) ([]Tab, error)
func (*App) Threads(ctx context.Context, cursor string, limit int) (ThreadsPage, error)
func (*App) ThreadsState() ThreadsState
func (*App) Typing(ch, threadTS string) error { return a.RTM.Typing(ch, threadTS) }
func (*App) UnclearActivity(ctx context.Context, items ...ActivityItem) error
func (*App) Unpin(ctx context.Context, ch, ts string) error
func (*App) Unsave(ctx context.Context, ch, ts string) error
func (*App) UnscheduleDraft(ctx context.Context, id string) (Draft, error)
func (*App) UpdateSaved(ctx context.Context, ch, ts string, u SavedUpdate) error
func (*App) Upload(ctx context.Context, o UploadOpts) ([]UploadedFile, error)
func (*App) Usergroups(ctx context.Context, query string, limit int) ([]Usergroup, error)
func (*App) User(id string) (User, bool)
func (*App) Users(ids []string) map[string]User { return a.users(ids) }
func New(c *slack.Client, s *store.Store) *App
func ParseHuddle(raw json.RawMessage) (Huddle, bool)
```

### Events (`Subscribe()`)
`Event.Type` is one of the following:

- **Messages.** `message`, plus `Count` for the conversation's new unread state. Own optimistic sends start as `message` with `Msg.Pending` and a local TS.
  - `sent {ReplacesTS, TS, ClientMsgID, Msg}` swaps the pending row for the confirmed message. The realtime echo of your own send never produces a second `message`.
  - A failed send arrives as `update` with `Msg.Failed`. Recover with `Resend` or `DiscardPending`; `DiscardPending` emits `delete`.
  - `update` covers edits, reactions, reply counts, pins, saved and follow flags, and filled file stubs. `delete` removes a message.
- **Conversations and sidebar.** `typing`, `marked {Count}`, `presence {Users, Presence}`, `user`, `conv`, `conv_removed`, `counts`, `connection`, `emoji_changed`, `sidebar_changed` (sections and stars), `prefs`, `huddle {Name: sh_* type, Raw}`.
- **Views.** `saved {Name: add|update|delete, Saved}`, `draft {Name: create|update|delete|send, ID, Draft}`, `activity {Name: subtype, ID: key, Activity}`, `tabs {Channel}` (re-read `Tabs` and `Bookmarks`), `thread_subscription {Channel, ThreadTS, Name: follow|unfollow}`.
- **Overflow.** `resync` means the subscriber fell behind and dropped events: re-read what you show.
- **Raw passthroughs.** `thread_marked`, `update_thread_state`, `dnd_updated`, `dnd_updated_user`, `pref_change`, `badge_counts_updated`, `channel_sections_*`.

### Facts verified against the web client (2026-11)
- **Tabs.** The channel tab bar comes from `conversations.info` → `properties.tabs`, with titles from `files.info`.
  - A `file_not_found` file shows as "Deleted file".
  - A folder tab is "Bookmarks", filled from `bookmarks.list`.
- **Members.** The header member count is edge `users/counts` → `people`, not `num_members`, which includes bots.
- **Stars and mute.**
  - Starred membership comes from `stars.list` on the team host: the Starred section from `users.channelSections.list` stays empty. Star and unstar use `stars.add`/`stars.remove`.
  - Mute and the per-channel notification prefs use `users.prefs.setNotifications`; they are read from `all_notifications_prefs`.
- **Preferences.** The dialog's sections, ids and keys come from Slack's `gantry-v2-async-prefs-dialog` chunk.
  - One pref is `users.prefs.set name=&value=` (`setUserPrefByApi`); Mark as read writes `mark_msgs_read_immediately` and `start_scroll_at_oldest` together as `prefs={…}`. Global notification prefs are `users.prefs.setNotifications global=true`, read from `all_notifications_prefs.global`. VIPs are read from `vip_users` and removed with `users.priority.remove user=`.
  - `display_real_names_override` is 1 (full names), -1 (display names) or 0 (the team's `display_real_names`).
  - Render paths read the prefs through `internal/ui/uipref` (a snapshot set on every `prefs` event); `uipref.Gen` changes when a pref that changes text layout does, and richview relays out.
- **Scheduling.** Scheduled messages are drafts (`drafts.update date_scheduled`), because `chat.scheduleMessage` rejects session tokens.
- **Saved and To-dos.** Both are `saved.list`, with `filter=saved_with_archived` for Saved and `todo_with_completed` for To-dos, split by `todo_state`.
- **Sent.** The Sent list is `messages.sent.list`, which returns ids only, hydrated with `messages.list`.
- **Files.** The Files browser uses `search.modules.files` with `search_context=desktop_files_browser`.
- **Gateway subscriptions.** `user_change` subscriptions take at most 100 ids; `presence_sub` takes 500.
- **Account prefs.** `core.Pref`/`PrefString`/`PrefBool`/`PrefInt` read users.prefs.get's raw map (memory only; the parsed `store.Prefs` is what the disk cache keeps). `core.SetPref(ctx, name, value)` writes like Slack's `setUserPrefByApi`: `users.prefs.set name=…&value=…`, JSON-typed prefs (`ia_theme`) JSON-encoded, everything else as plain form values; it updates the local copy and emits `prefs`. fakeslack's users.prefs.get/set keep a mutable map (`Server.Pref`, `SetPrefs`).
- **Theme.** Slack's 2026 theme is the account pref `ia_theme` (`{primary, highlight1, highlight2, important: {palette, custom}, brightness, sidebarInverted, useCustomHex}`) plus `window_background_gradients`. `theme.ApplyIATheme` turns it into the chrome gradient, sidebar, badge, presence and accent colours the way Slack's theme module does (its shade table, gradient rules, the plt ramps in `plt_ramps.go`, Material HCT ramps for custom hexes in `hct.go`). It applies at boot (from the cached prefs on the first frame) and on `prefs` events, under the stylesheet tokens, so Slick themes and user CSS still win. Appearance writes it back with users.prefs.set, as Slack's `setIaThemePrefByApi` does. Light/dark and "System" are per device in Slack (localStorage `userTheme`, `systemThemeSyncEnabled`), so Clack keeps them in ui.json (`dark`, `theme_sync`). The OS scheme is an atomic (`osscheme.go`) that background watchers update: AppKit's `effectiveAppearance` (KVO) and `AppleInterfaceThemeChangedNotification` on macOS, the XDG portal's `SettingChanged` on Linux, the registry on Windows, each with a slow poll. The theme's colours are cached per (theme, mode, gradients) and computed off the UI goroutine (`theme.FieldsAsync`).

## Desktop integration (`internal/desktop`, `packaging/`)
- **Single instance.** `cmd/clack` calls `desktop.Forward(configDir, Request{URL})` before opening a window. If a Clack for the same user and config dir answers on its socket (`$XDG_RUNTIME_DIR/clack-<hash>.sock`, or the temp dir on macOS and Windows), the new process exits. Otherwise `desktop.Listen` makes it the primary, and every later launch calls `App.Raise` plus `App.OpenURL(url)`. Different `CLACK_HOME`s run side by side, which keeps test instances apart. A socket left by a crash is replaced.
- **Deep links.** The OS runs `clack "slack://…"`: the Linux `.desktop` `Exec=clack %u`, and the Windows HKCU `Software\Classes\slack` keys written by the installer or `clack -register-url-handler`. macOS sends the link to the running app as a Gio `app.URLEvent` (`main` ranges over `app.Events` instead of calling `app.Main`). Every path ends in `App.OpenURL`, which dispatches Slack's `handleDeepLink` thunk; plugins can patch it, and the native is the shell's `openDeepLink` (`UIHooks.DeepLink`).
- **Sign-in links.** A `slack://<TEAM>/magic-login/<tok>` or `slack://login-v2/?0.host=…` link (`slack.IsMagicLink`) isn't a navigation. `cmd/clack`'s `openLink` sends it to `App.SignInWithLink` instead of `App.OpenURL`, on every path: the launch argument, a forwarded one (`desktop.Listen`) and macOS `URLEvent`s. It signs in only from the sign-in screen and shows a toast otherwise. The login callback runs `slack.ParseMagicLink` + `slack.LoginMagic` (DPoP keys next to `creds.json`). Clack never registers itself as the `slack://` handler. `-register-url-handler` and the Windows installer's task (unchecked by default) do, on request. Slack desktop re-registers `slack://` on every start, so with both installed the last one to register wins.
- **Update check.** Release builds only (`main.version` is a semver). The first check is 15 s after launch. After that, `updateLoop` (internal/ui/update_check.go) wakes every 5 min and on window focus, and checks when an hour of wall-clock time (`time.Now().Round(0)`, so time asleep counts) has passed since GitHub last answered. A failed check is retried after 5, 10, 20, 40, then 60 min. A 429, or a 403 with the rate limit used up, waits until `Retry-After` or `X-RateLimit-Reset` (10 min if neither is sent). The last answer and the rate limit persist in ui.json (`update_checked_at`, `update_limited_until`), so a restart doesn't ask again. Check now (Preferences > Advanced > Updates), Clack > Check for Updates… on macOS and the workspace menu's Check for updates… on Linux and Windows always check, even with "Check for updates" off. The one exception is an active rate limit, when they say how long to wait instead of asking. A release build calls GitHub's `releases/latest`, which skips drafts and pre-releases. A pre-release build (`v0.1.0-alpha.8`) lists recent releases and takes the newest non-draft one, because `releases/latest` never returns a pre-release. Versions compare by semver. Preferences > Advanced > "Check for updates" turns it off (`no_update_check` in ui.json). For testing, `CLACK_UPDATE_URL` points the check at another JSON file (one release, or a list) and lets downloads come from that host too, plain http on loopback included. `CLACK_UPDATE_AS` sets the version it compares against.
- **Auto-update** (`internal/desktop/autoupdate.go`, `install.go`, `cmd/clack/update.go`). With "Download updates automatically" on (the default; `no_update_download` in ui.json), a newer release downloads in the background to `<config>/cache/updates/<tag>/`. It picks the one package that matches this copy (`desktop.DetectInstall`): the universal `.dmg` (or `.zip`) for a `Clack.app`, `setup.exe` when Inno's `unins*.exe` sits next to `Clack.exe`, the portable `.zip` otherwise, the AppImage when `$APPIMAGE` is set, the `.deb`/`.rpm` when `dpkg-query -S`/`rpm -qf` own the binary, and the AppImage for any other Linux binary. Downloads are https only, from github.com and its two asset hosts (every redirect checked), at most 400 MB and never past the size the release declares. They resume with `Range` and get 5 tries. A package is kept only if it matches its line in the release's `SHA256SUMS`. A missing entry or a mismatch deletes it, and Preferences → Advanced falls back to **Download** and **View release** (the menus' item reads **An Update Is Available** and opens it). The UI is Slack desktop's (`internal/ui/update_ui.go`, from Slack 4.53.276's app.asar and web client): no banner. While downloading, the menu item reads **Downloading Update**, greyed. Once verified, the top nav's Help button gets a dot, Help opens its flexpane with a **Take action** card ("Clack X", "Click to update to a new version of the app."), and the card opens "Restart and update Clack?" with **Not Now** and **Restart Clack** (plus a warning while you're in a huddle). The menu item reads **Restart to Apply Update** and restarts right away. Nothing is snoozed: Not Now only closes the dialog. Nothing installs before the user chooses to restart. The click re-checks the hash, then:
  - macOS: mounts the dmg (`hdiutil attach -nobrowse -readonly`), `ditto`s the new `Clack.app` beside the installed one, and detaches. The installed bundle's path stays as it is. A translocated or unwritable bundle opens the dmg in Finder instead.
  - Windows installer: runs `setup.exe /VERYSILENT /CURRENTUSER /SUPPRESSMSGBOXES /NORESTART` (release.yml's install test flags) after Clack exits. Portable: `Clack/Clack.exe` from the zip replaces the running exe after exit.
  - Linux AppImage: the new file takes the old one's path (launchers keep working). A loose binary gets the AppImage in `~/Applications`. `.deb`/`.rpm`: `pkexec apt-get install -y` (or `dnf`, `zypper`, `rpm -U`), which asks for the password. When pkexec is missing or has no polkit agent, `xdg-open` hands the package to the software installer and a toast says to finish there. Nothing runs as root without a prompt.

  Swaps and the Windows installer run in a helper: `desktop.StartHelper` copies the running executable to `$TMP/clack-update-helper-*` and starts `clack-updater -apply-update plan.json`, and Clack quits. The helper waits for Clack's pid to exit (2 min at most; otherwise it changes nothing), renames the old copy to `*.clack-old`, moves the new one in, puts the old one back if that fails, and relaunches (`open Clack.app` on macOS). Its log is `update.log` beside the plan. The next start removes helper directories older than an hour.
- **Icons.** `assets/icon` holds the SVG masters (`gen_svg.py`) and the rendered PNG, .ico and .icns files (`build.py`). The window icon depends on the platform:
  - X11: `_NET_WM_ICON`, set from the view event.
  - Wayland: the compositor matches app id `clack` to `clack.desktop`.
  - Windows: icon resource `#1`, which `go-winres` embeds from `packaging/windows/winres.json` (Gio and the tray both load it).
  - macOS: the bundle's `clack.icns`; a bare binary sets the Dock icon itself.
- **macOS menu bar** (`internal/ui/menubar`, `internal/ui/macmenu.go`). Clack replaces Gio's two-item app menu with Slack desktop 4.53.276's menus: Clack, File, Edit, View, Go, History, Window and Help. They come from the Electron shell's AppMenu template in `app.asar` (`dist/main.bundle.cjs`) plus the items the web client adds (`desktop-app-menu`: Preferences, New Message, the Go menu, Hide Sidebar, Find, Search, Sign Out and History's recent views). Slack's order numbers decide where the separators go. Items Clack has no feature for are left out, not shown greyed out. `TestMacMenus` pins the tree, and the macOS CI job's `TestNativeMenuBarBuilds` checks that AppKit builds it and that every action has an implementation.
  - Threading: AppKit runs menu actions on the main thread, where `Window.Perform` deadlocks. Each action runs on its own goroutine and reaches the UI through `App.Post`. The callbacks AppKit calls synchronously (validation, titles, the recent list) only read the `menubar.State` that `syncMenuBar` stores once per frame. `TestCallbacksOnlyHandOff` enforces this.
  - Shortcuts that Clack's window already binds (`keys.go`: ⌘K, ⌘,, ⇧⌘A, ⌘[, …) are `Forward` items. A key press reaches the menu first and is handed to the focused Gio view unchanged, so modals and focused widgets behave as before. Choosing the item with the mouse runs the same action.
  - Edit (Undo, Redo, Cut, Copy, Paste, Paste and Match Style, Delete, Select All) uses AppKit's standard actions through the responder chain. Clack adds them to Gio's `GioView`, which sends Gio the key its editors bind (⌘C, ⇧⌘V, …), so Gio text fields work from the menu and native panels keep their own handling.
  - View > Actual Size / Zoom In / Zoom Out are Slack's zoom levels −3…5 (×0.7 to ×2, `zoom` in ui.json). They scale the main window's `unit.Metric` and are also bound to Cmd/Ctrl+0, +/=, and − on every OS. File > Close hides the main window as Slack does, and the Dock icon brings it back. Other windows close.
- **Windows tray.** A Win32 notification-area icon on its own thread. A click or **Open Clack** raises the window, and **Quit Clack** closes it. The icon comes back after an Explorer restart. Gio can't intercept the window's close button, so closing the window still quits Clack instead of hiding it to the tray.

## Sessions (`internal/slack`)

`Creds.Session` tells the two kinds apart. Both live in `creds.json`.

- **Pasted** (`Session` empty). The browser's own `xoxc` token and `d` cookie. Every request sends `Cookie: d=…` only, never `d-s`, with Clack's own User-Agent (below). `Set-Cookie` is ignored. `Creds.UserAgent` is legacy: read from old files, never sent.
- **Desktop** (`Session: "desktop"`, from `LoginMagic`). The `d` cookie (`Cookie`) and every other cookie Slack vended (`Cookies`, `name=value; …`) belong to Clack. `New` wraps the transport (`desktopTransport`). It sends the jar on every slack.com request, takes in `Set-Cookie` from every slack.com response (redirect hops and the socket handshake too), and adds DPoP proofs when `Creds.DPoP` names a bound domain. `Client.PersistSession(save)` gets the rotated creds, debounced (1 s when `d` or the set of cookies changes, 30 s otherwise), and `FlushSession` at exit. `cmd/clack` saves only while that client is the live one. The User-Agent is Clack's own, as for pasted sessions. Callers that need the current cookie use `SessionCookie`/`SetSessionCookie`/`SessionCreds`, not `Creds.Cookie`.
- **User-Agent, both kinds.** `slack.UserAgent()` is `Clack/<version> (+https://github.com/some-du6e/clack)` (`cmd/clack` calls `slack.SetVersion`). `slack.HonestHeaders` sets it and strips `sec-ch-*`, `Sec-Fetch-*`, `Origin` and `Referer`; `honestTransport` applies it again under `Client.HTTP`, the sign-in exchange's client and `PublicHTTP`. Never send a browser or Slack desktop identity: Grid signs a session out everywhere for it (docs/SIGNOUT-AUDIT.md "Result"). `TestNoBrowserDisguise` and the ua.go source scan enforce it.
- **DPoP-bound** desktop sessions send nothing until `EnableDPoP(configDir)` loads the key. A missing key, or a proof Slack rejects, trips the signed-out guard with a `DPoPError`, so traffic stops at once instead of retrying.

## Connections to Slack (`internal/slack/tlsfp`)

Grid signs a session out everywhere when it sees the session's credentials on Go `crypto/tls`'s ClientHello (docs/SIGNOUT-AUDIT.md "Root cause"). So every TLS connection to a Slack host (`tlsfp.SlackHost`: slack.com, slack-edge.com, slack-imgs.com, slack-files.com and subdomains) presents the hello Python urllib's OpenSSL 3.0.13 sent, which survived. It is embedded as `tlsfp/openssl-3.0.13.bin`, parsed by uTLS's `Fingerprinter` per connection, given the host's SNI, and handshaken with `crypto/tls`'s own verification (system roots, host name).

- `tlsfp.NewTransport(*Dialer)` is the only `http.Transport` constructor: HTTP/1.1 only (no h2 in ALPN, empty `TLSNextProto`), keep-alive, 16 connections and 16 idle per host, 64 idle total, 5 min idle timeout. `Dialer.Dial` is the TCP dial (a guard goes there, under TLS); `Dialer.Proxy` picks an `HTTPS_PROXY`, which gets Clack's own CONNECT so the handshake with Slack runs inside the tunnel.
- `slack.newTransport` (API dialer, `HTTPS_PROXY`) carries everything with credentials: `Client.HTTP` (Web API, edgeapi, `Download`, `UploadBytes`, multipart, and the realtime socket, whose `websocket.Dial` uses `Client.HTTP` as its dial hook) and the sign-in exchange (`magicClient`).
- `slack.publicTransport` (public dialer: public addresses only, no proxy) carries `PublicHTTP`.
- `tlsfp.Mixed`/`tlsfp.Client` route per request: Slack's hosts through a tlsfp transport, everything else through Go's default transport unchanged. Taut's fetch, Slick downloads, jsrt's fallback fetch, the plugin library, Slick theme downloads and the update check use it.
- Huddle signalling (Amazon Chime, no Slack credentials) keeps Go's default client.
- Performance on HTTP/1.1: one request per connection at a time, so the boot fan-out (up to `MaxConcurrent` = 12 API calls in flight) opens up to 12 connections to the team host in parallel. The handshakes overlap, so boot pays about one extra round trip, not twelve. Idle connections are kept, so later bursts reuse them. Beyond 16 per host, Go queues requests instead of opening more.
- `TestOnlyTLSFPDials` (tlsfp/scan_test.go) parses every non-test file and fails on a `crypto/tls` or uTLS import outside tlsfp, an `http.Transport` built elsewhere, `http.DefaultClient`/`DefaultTransport`, an `http.Client` without a `Transport`, `http.Get` and friends, `websocket.Dial` without an `HTTPClient` (huddle's Chime socket excepted), an `x/net/http2` import, or code setting a transport's TLS or dial fields.

## UI (Gio) principles
- **Never block the UI goroutine.** On macOS Gio handles every window event and waits for every frame on the AppKit main thread, so any wait in layout or in work posted with `App.Post` beachballs the whole app (2026-11-08: the Appearance page ran `defaults read` on every frame). That rules out exec, file I/O, network, D-Bus, waiting on channels or `core.Snapshot` (it waits for Boot), sleeps, locks held across slow work, `dispatch_sync`, and calling another window's `Option`/`Perform`/`Run` from a frame (on macOS that deadlocks). Do the work on a goroutine and `Post` the result. Functions that may block call `uiguard.OffUI(what)` first, which logs and records a call made on the UI goroutine. `TestNoExecOnUIPaths` allows `os/exec` only in a short list of files, `TestOSPrefersDarkOffUI` keeps the OS-scheme read out of layout, and `TestStartupStatesNeverBlock` starts the client in dozens of saved states (ui.json and account prefs) and fails on any blocking call or slow frame. In the field, a frame or task over 2 s writes a redacted report to `<config>/freeze-reports/` (`watchdog.go`; README "If Clack freezes").
- Fast: immediate mode with virtualized `layout.List`s, no allocations in hot paths where avoidable, async image decode (`internal/imgcache`: memory LRU plus a disk cache for avatars/emoji only, decoded off the UI goroutine, invalidate the window on arrival). Instant channel switch from cached history.
- Look: copy the Slack 2026 desktop client (measurements are taken from the real client in the agent-browser profile `whatever`). Lato TTF embedded (OFL). SVG-like icons drawn with `clip.Path` / vector icons; no Slack proprietary assets committed. Emoji images come from Slack's CDN at runtime.
- Builds: Linux needs cgo with `libwayland-dev libx11-dev libx11-xcb-dev libxkbcommon-x11-dev libgles2-mesa-dev libegl1-mesa-dev libffi-dev libxcursor-dev libvulkan-dev`. Windows cross-compiles without cgo. macOS builds on a `macos-latest` runner. Gio needs Go ≥ 1.24.

## UI package ownership (Gio). Each agent edits only its own packages.
| package | owner agent | contents |
|---|---|---|
| `internal/ui/theme` | ui-shell | `Theme` struct: colors (light + dark), `Shaper *text.Shaper` with embedded Lato TTF (300/400/700/900, italics), sizes, helpers `Label(gtx, size, weight, color, text)`; fonts in `assets/fonts` |
| `internal/ui/icons` | ui-shell | vector icons as `func(gtx, size unit.Dp, col color.NRGBA) layout.Dimensions`, a named registry `icons.Get("hash")` |
| `internal/imgcache` | ui-shell | `New(fetch func(ctx, url) ([]byte, error), diskDir string, invalidate func()) *Cache`; `(*Cache).Get(url string, sizePx int) (paint.ImageOp, bool)`: non-blocking, decodes off the UI goroutine, disk caching only for avatars/emoji (`DiskOK(url)` decides; never files.slack.com) |
| `internal/ui` (root) + `cmd/clack` | ui-shell | window/app loop, core event pump, `Host` implementation, shell layout (top nav, rail, sidebar, header + tabs, split panes, resizing), routing/history, keyboard shortcut registry, dark mode, window title/unread badge, desktop notifications, login screen, CI (cgo deps, per-OS builds) |
| `internal/ui/richview` | ui-messages | layout engine for `richtext.Doc`: wrapped styled spans, inline emoji images, mention pills, code/pre/quote/lists, clickable links/mentions, text selection + copy |
| `internal/ui/messages` | ui-messages | virtualized message list, rows (grouping, avatars, names, APP badge, status emoji, timestamps), day dividers (sticky), unread line, reactions, thread bar, hover action bar, files/images/video/audio cards, attachments/unfurls, infinite scroll, jump-to-bottom, thread pane |
| `internal/ui/composer` | ui-composer | rich editor (formatting toolbar, inline styles, lists/quote/code), Enter-to-send, Up-to-edit, autocomplete (@ # : /), attachments (pick/paste/drop) with upload, drafts, typing indicator send, edit mode, "also send to channel" |
| `internal/ui/popups` | ui-popups | overlay system (menu, popover, modal, tooltip, toast), emoji picker (virtualized 65k custom), quick switcher (Ctrl+K), search (dropdown + results view), profile card + full profile, lightbox, message/channel/user/team menus with actions |
| `internal/ui/views` | ui-views | Activity, DMs, Later/Saved, Files, Threads, Huddles, Drafts & sent, Directories, channel details |
| `internal/ui/blockkit` (+ `internal/core/interactive*.go`) | blockkit | interactive apps: Block Kit elements in messages, legacy attachment actions, modals (view stack), App Home tabs, app shortcuts, workflow links, ephemeral messages |

### Cross-package interface: `Host`
To avoid import cycles, every feature package defines the small interface it needs **in its own package** (e.g. `messages.Host`). The `internal/ui` root implements all of them on one `*ui.App`. Shared things every Host offers (use these exact names so one implementation satisfies all):
```go
Core() *core.App; Theme() *theme.Theme; Images() *imgcache.Cache; Invalidate()
Navigate(route string)            // "c/<id>", "c/<id>/t/<ts>" (open thread), "c/<id>/m/<ts>" (scroll to + flash a message: ChannelView.Highlight(ts); a reply in a thread is "c/<id>/t/<root>" plus Highlight on the ThreadView later), "activity", "dms", "later", "files", "threads", "huddles", "drafts", "directories", "search?q="
OpenThread(channel, ts string); CloseSecondary()
ShowOverlay(o Overlay)            // popups.Overlay (interface below) — ui root renders the overlay stack on top
OpenEmojiPicker(anchor image.Point, onPick func(name string))
OpenProfile(anchor image.Point, userID string)
OpenMessageMenu(anchor image.Point, m core.Message)
OpenLightbox(img richtext.Image, m core.Message)
Toast(text string)
```
`popups.Overlay`: `interface{ Layout(gtx layout.Context) layout.Dimensions; Dismissed() bool }` (popups agent owns the concrete types; ui root just stacks and draws them, and routes Escape/outside clicks).
Each feature package exports a constructor and a widget with `Layout(gtx layout.Context) layout.Dimensions`, e.g. `messages.NewChannelView(h Host, channelID string) *ChannelView`, `messages.NewThreadView(h, ch, ts)`, `composer.New(h Host, channelID, threadTS string) *Composer`, `views.NewActivity(h)` …, and `popups.NewEmojiPicker(h, onPick) Overlay` etc.

### Context menus (right-click)
A context click is a secondary press, a two-finger tap on a Mac trackpad (AppKit sends it as a right click), or **Ctrl+click on macOS**, which AppKit sends as a primary press with Control held. `internal/ui/ctxclick` decides: `Press(e)` for a context press, `Primary(e)` for an ordinary primary press, and `Clicked(gtx, btn)` for a `widget.Clickable` click that ignores macOS Ctrl+clicks. `clickArea`, richview's links and selection, the message list's selection, the sidebar rows and slackc's `ContextMenuTrigger` all use it. As in Chromium on a Mac, a Ctrl+click opens the menu and doesn't click. The menus come from Slack's web client and desktop shell (build a84662d2d135, 2026-11-08). popups/actions_context.go cites the modules.

| where | menu |
|---|---|
| a message (body text, gutter, time, reactions, toolbar) | `popups.NewMessageContextMenu`: Slack's MessageActionsContextMenu > MessageActionsMenu (`menuUsageLocation: "context"`). It has the hover toolbar's group (Add reaction… R, Reply in thread / Reply to thread / View thread T, Forward message… F, Add to-do / Complete to-do A), then More actions |
| text selected in the list | `NewTextSelectionMenu`: Copy text (⌘C / Ctrl+C), Search in Slack |
| inline code or a code block (nothing selected) | `NewCodeMenu`: the desktop shell's menu with BaseCopyCodeBlockContextMenuTrigger's one item, **Copy this code** / **Copy this code block** (the code run's or block's text, trailing newlines dropped). Code blocks also have Slack's hover **Copy** button (CopyCodeBlockButton, `richview/codecopy.go`), and a triple-click selects the whole code run or block |
| a link | `NewLinkMenu`, the desktop shell's link menu: Copy Link (Copy Email Address for `mailto:`), Open Link, plus Copy Image / Copy Image URL when the link holds an image |
| an image or emoji | `NewImageMenu`: Copy Image, Copy Image URL. `internal/ui/clipimg` writes the image with wl-copy/xclip, NSPasteboard or CF_DIB+PNG |
| a file card, gallery image or video | `NewFileMenu`, Slack's FileContextMenuTrigger groups (media gallery). Clack has no Save for later, file details, external link, folders, sections, transcripts, edit or delete for files, so those rows are left out |
| a user mention, the sender's name or avatar | `NewMemberMenu` (MemberContextMenu) |
| a Slackbot custom response's sender (team icon and name) | nothing: Slack draws it non-interactive (`noMenuOn` takes the press); its body still gets the message menu |
| a channel link, the header title | `NewChannelContextMenu` (ChannelContextMenu{id}): the channel menu at the pointer |
| a user group mention | the link menu for `<team>.slack.com/admin/user_groups` (Slack's href) |
| a sidebar conversation row | the shell's sidebar menu (`openSidebarMenu`) |
| a Threads view message | `NewMessageContextMenu` with `InThreadsView` |
| the composer, a message edit, a canvas being edited | the shell's `buildMenuForTextInput` (`composer/spellcheck.go`): spelling suggestions, Add to Dictionary, Search with Google (with a selection), Cut / Copy / Paste (docs/PREFS-AUDIT.md, Spellcheck) |

The message list sees every context press in its ancestor handler. Rows with their own menu (the name and avatar) mark the press taken; otherwise `messages/context.go` opens the menu after the rows lay out, picked from `richview.View.HitAt`. One difference from Slack: Slack's list leaves a click on plain text to the shell, whose menu is empty off macOS (on macOS Chromium selects the word and shows the selection menu), while Clack opens the message's menu.

### Rich paste (`internal/ui/composer/paste*.go`)
Ctrl/Cmd+V keeps formatting like Slack's composer. Gio v0.10.3's `clipboard.ReadCmd` only delivers plain text on every OS (X11 asks for `UTF8_STRING`, Wayland for `text/plain`, macOS reads the string type, Windows `CF_UNICODETEXT`), so the key press also starts an HTML read off the UI goroutine. The editor waits up to 300 ms for it after the text arrives, checking once per frame without blocking, so Clack can still answer selection requests when it owns the clipboard. Then it pastes plain text.
- **macOS** (`paste_darwin.go`, cgo): NSPasteboard `public.html` (browsers, Notes, Word); if absent, `public.rtf` / `com.apple.flat-rtfd` (TextEdit, Pages) converted to HTML by AppKit's `NSAttributedString` writer.
- **Windows** (`paste_windows.go`): the registered `HTML Format` (CF_HTML) via user32; the header's offsets are parsed by `cfHTML`.
- **Linux** (`paste_linux.go`): `text/html` via `wl-paste` (Wayland) or `xclip` (X11), the same tools as image paste. Without them: plain text only.
- **Others** (Android, iOS, BSDs, macOS without cgo): plain text only.

`docFromHTML` converts to the composer `Doc`: `ul`/`ol`/`li` (nested, Slack's `data-indent`, Word's `mso-list` paragraphs), `b`/`strong`, `i`/`em`, `s`/`del`, `u`, `code`, `pre`, `a[href]`, `blockquote`, `br`/`p`/block elements, plus CSS `font-weight`/`font-style`/`text-decoration` (Google Docs, AppKit classes). Headings become bold lines. Emoji images keep their shortcode or unicode alt text; other images, scripts, styles and hidden elements are dropped. Untrusted input: links must be http(s) with a host (no userinfo) or `mailto:`, at most 4 KiB; over 2 MiB of HTML or 40,000 runes, the paste falls back to plain text. Plain text with list lines (`- `, `* `, `+ `, `•`, `◦`, `1.`, `1)`) becomes a list (`docFromPlainText`), nested by indentation. Ctrl/Cmd+Shift+V (and Cmd+Opt+Shift+V) is paste and match style: plain text, no list detection. Code blocks and inline code always take plain text.

## Interactive apps (`internal/ui/blockkit`, `core.Interactive`)

`a.Interactive()` (internal/core/interactive.go) is the service; blockkit draws. Every call shape was read from Slack's web client (gantry-v2, 2026-11) and the ones marked live were tested on Hack Club:

| what | Slack call | notes |
|---|---|---|
| block action (live) | `blocks.actions {service_id: bot_id, app_id, service_team_id, actions: [element + block_id + selection], container, client_token: web-<ms>, state: {values}}` | enterprise token (team token: `team_is_restricted`); container `message {message_ts, channel_id, is_ephemeral, thread_ts only for ephemerals}`, `message_attachment {…, attachment_id}`, `view {view_id}`; errors carry Slack's text (`post_error` "App took too long…") |
| external select | `blocks.suggestions {service_id, app_id, service_team_id, container, action_id, block_id, value}` | `min_query_length` default 3, 200 ms debounce |
| legacy attachment action (live) | `chat.attachmentAction {payload: {actions:[raw action (+selected_options)], attachment_id, callback_id, channel_id, is_ephemeral, message_ts, team_id}, client_token, service_id, app_id, bot_user_id}` | |
| modal opens (live) | RTM `view_opened {view, view_id, view_type, previous_view_id, client_token, timeout_range, …}` (older payloads flat) then `view_updated {view_id, view}` | shown only when `client_token` is one this client issued, within `timeout_range` (a click in the browser must not open it in Clack too) |
| submit (live) | `views.submit {view_id, client_token, state: {values}}` → `{view, response_action, toast_message}`; `ok:false error=validation_failed errors{block_id:msg}` | `slack.Error.Body` keeps the body for the errors |
| close (live) | `views.close {view_id, client_token, root_view_id?}` | footer Close / Escape pop one view (root_view_id with clear_on_close); × closes the stack |
| shortcuts (live) | `apps.actions.v2.list {resource_id: team, resource_type: team}`; `apps.actions.v2.execute {action_id, app_id, team_id: current workspace, context: {channel_id, message_ts?}, client_token}` | the list's per-app `team_id` gets `internal_error`; message shortcuts in the message menu's Connect to apps, global ones in the composer's + menu |
| App Home (live) | `conversations.info {channel, return_app_home}` → `app_home` + `home_view`; `apps.home.dispatchOpenEvent {id, type: home/messages}`; RTM `view_opened/updated` type home; About: `apps.profile.get {bot, app}` | |
| workflow links | `workflows.triggers.trip {url, context: {location: message, channel_id, message_ts}, client_token, run_precheck}` | `pretrip_ack_required` → "Start workflow?" confirm, trip again without precheck (not live-tested: workflows can post) |
| legacy dialogs | RTM `dialog_opened` → `dialog.get`, `dialog.submit {dialog_id, submission}`, `dialog.notifyCancel` | |
| ephemerals (live) | RTM `message` with `is_ephemeral` | `Interactive().Ephemeral(ch, ts)`; rows get "Only visible to you" + Slack's .04 background |

UI wiring (minimal wraps): richview `Options.Elements` gives each run of elements (actions block, section accessory, attachment buttons) a slot in the cached layout that blockkit sizes and draws per frame; messages' rows pass `blockkit.MessageElements`; the ui root calls `blockkit.Start` after boot, `blockkit.SetTaut`, `AppDMFor` for app DM tabs, and sets `popups.MessageShortcuts` / `composer.ShortcutItems`. Taut thunks: `sendActions`, `executeAppAction`, `submitView` are dispatched with Slack's args; natives for those and `blocksFormatFetcher` are registered.

Not done yet: `rich_text_input` edits as plain text; `file_input` toasts; the read-only Messages tab of some apps doesn't replace the composer (composer owns it: `AppDM.ReadOnly()`); speedbump on closing a changed modal; workflow unfurl cards.

