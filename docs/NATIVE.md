# yoloslack native architecture (supersedes the web-UI parts of ARCHITECTURE.md)

The user's requirement (2026-11-06): **no JS, no web UI, no Electron. Native and fast.** yoloslack is a single Go binary with a GPU-rendered native window built on **Gio** (`gioui.org`, immediate mode). Every widget is custom-drawn to look like the 2026 Slack desktop client. The HTTP server, `web/` and the HTML renderer `internal/render` will be deleted once the native UI lands.

> **Rename (2026-11-07): the app is now Clack.** The binary is `cmd/clack`. CI artifacts are `clack-linux-amd64` (file `clack-linux-amd64`), `clack-windows-amd64` and `clack-darwin-arm64`; `yoloslack-linux-amd64` is no longer built, so update your download scripts. Config lives in `~/.config/clack`, and the first start copies over `~/.config/yoloslack`, which stays in place for older tools. Env vars are `CLACK_HOME`, `CLACK_TIMING`, `CLACK_FRAMELOG`, `CLACK_NOEMBOLDEN` and `CLACK_TYPINGLOG`; the `YOLOSLACK_*` names still work. The window title ends in ` - Clack`. The Go module path is unchanged.

## Scraping policy (Hack Club), non-negotiable
Message contents are memory only. Never write message text, files or attachments to disk, logs or fixtures from real messages; test fixtures must be synthetic. Only metadata may be cached on disk: users, channel names, custom emoji map, and avatar/emoji images.

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
- **Scheduling.** Scheduled messages are drafts (`drafts.update date_scheduled`), because `chat.scheduleMessage` rejects session tokens.
- **Saved and To-dos.** Both are `saved.list`, with `filter=saved_with_archived` for Saved and `todo_with_completed` for To-dos, split by `todo_state`.
- **Sent.** The Sent list is `messages.sent.list`, which returns ids only, hydrated with `messages.list`.
- **Files.** The Files browser uses `search.modules.files` with `search_context=desktop_files_browser`.
- **Gateway subscriptions.** `user_change` subscriptions take at most 100 ids; `presence_sub` takes 500.

## Desktop integration (`internal/desktop`, `packaging/`)
- **Single instance.** `cmd/clack` calls `desktop.Forward(configDir, Request{URL})` before opening a window. If a Clack for the same user and config dir answers on its socket (`$XDG_RUNTIME_DIR/clack-<hash>.sock`, or the temp dir on macOS and Windows), the new process exits. Otherwise `desktop.Listen` makes it the primary, and every later launch calls `App.Raise` plus `App.OpenURL(url)`. Different `CLACK_HOME`s run side by side, which keeps test instances apart. A socket left by a crash is replaced.
- **Deep links.** The OS runs `clack "slack://…"`: the Linux `.desktop` `Exec=clack %u`, and the Windows HKCU `Software\Classes\slack` keys written by the installer or `clack -register-url-handler`. macOS sends the link to the running app as a Gio `app.URLEvent` (`main` ranges over `app.Events` instead of calling `app.Main`). Every path ends in `App.OpenURL`, which dispatches Slack's `handleDeepLink` thunk; plugins can patch it, and the native is the shell's `openDeepLink` (`UIHooks.DeepLink`).
- **Sign-in links.** A `slack://<TEAM>/magic-login/<tok>` or `slack://login-v2/?0.host=…` link (`slack.IsMagicLink`) isn't a navigation. `cmd/clack`'s `openLink` sends it to `App.SignInWithLink` instead of `App.OpenURL`, on every path: the launch argument, a forwarded one (`desktop.Listen`) and macOS `URLEvent`s. It signs in only from the sign-in screen and shows a toast otherwise. The login callback runs `slack.ParseMagicLink` + `slack.LoginMagic` (DPoP keys next to `creds.json`). Clack never registers itself as the `slack://` handler. `-register-url-handler` and the Windows installer's task (unchecked by default) do, on request. Slack desktop re-registers `slack://` on every start, so with both installed the last one to register wins.
- **Update check.** Release builds only (`main.version` is a semver). The check starts 15 s after launch and repeats every 12 h. It calls GitHub's `releases/latest`, which skips drafts and pre-releases, compares versions by semver, and shows a blue "A new version of Clack is available" banner above the workspace with **View release**, which opens the release page in the browser. Nothing is downloaded. Preferences > Advanced > "Check for updates" turns it off (`no_update_check` in ui.json). Closing the banner hides it until a newer tag comes out (`update_dismissed`). For testing, `CLACK_UPDATE_URL` points the check at another JSON file and `CLACK_UPDATE_AS` sets the version it compares against.
- **Icons.** `assets/icon` holds the SVG masters (`gen_svg.py`) and the rendered PNG, .ico and .icns files (`build.py`). The window icon depends on the platform:
  - X11: `_NET_WM_ICON`, set from the view event.
  - Wayland: the compositor matches app id `clack` to `clack.desktop`.
  - Windows: icon resource `#1`, which `go-winres` embeds from `packaging/windows/winres.json` (Gio and the tray both load it).
  - macOS: the bundle's `clack.icns`; a bare binary sets the Dock icon itself.
- **Windows tray.** A Win32 notification-area icon on its own thread. A click or **Open Clack** raises the window, and **Quit Clack** closes it. The icon comes back after an Explorer restart. Gio can't intercept the window's close button, so closing the window still quits Clack instead of hiding it to the tray.

## Sessions (`internal/slack`)

`Creds.Session` tells the two kinds apart. Both live in `creds.json`.

- **Pasted** (`Session` empty). The browser's own `xoxc` token and `d` cookie. Every request sends `Cookie: d=…` only, never `d-s`, with Clack's own User-Agent (below). `Set-Cookie` is ignored. `Creds.UserAgent` is legacy: read from old files, never sent.
- **Desktop** (`Session: "desktop"`, from `LoginMagic`). The `d` cookie (`Cookie`) and every other cookie Slack vended (`Cookies`, `name=value; …`) belong to Clack. `New` wraps the transport (`desktopTransport`). It sends the jar on every slack.com request, takes in `Set-Cookie` from every slack.com response (redirect hops and the socket handshake too), and adds DPoP proofs when `Creds.DPoP` names a bound domain. `Client.PersistSession(save)` gets the rotated creds, debounced (1 s when `d` or the set of cookies changes, 30 s otherwise), and `FlushSession` at exit. `cmd/clack` saves only while that client is the live one. The User-Agent is Clack's own, as for pasted sessions. Callers that need the current cookie use `SessionCookie`/`SetSessionCookie`/`SessionCreds`, not `Creds.Cookie`.
- **User-Agent, both kinds.** `slack.UserAgent()` is `Clack/<version> (+https://github.com/some-du6e/yoloslackgoclient)` (`cmd/clack` calls `slack.SetVersion`). `slack.HonestHeaders` sets it and strips `sec-ch-*`, `Sec-Fetch-*`, `Origin` and `Referer`; `honestTransport` applies it again under `Client.HTTP`, the sign-in exchange's client and `PublicHTTP`. Never send a browser or Slack desktop identity: Grid signs a session out everywhere for it (docs/SIGNOUT-AUDIT.md "Result"). `TestNoBrowserDisguise` and the ua.go source scan enforce it.
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

