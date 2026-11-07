# yoloslack native architecture (supersedes the web-UI parts of ARCHITECTURE.md)

The user's requirement (2026-11-06): **no JS, no web UI, no Electron. Native and fast.** yoloslack is a single Go binary with a GPU-rendered native window built on **Gio** (`gioui.org`, immediate mode). Every widget is custom-drawn to look like the 2026 Slack desktop client. The HTTP server, `web/` and the HTML renderer `internal/render` will be deleted once the native UI lands.

> **Rename (2026-11-07): the app is now Clack.** The binary is `cmd/clack`. CI artifacts are `clack-linux-amd64` (file `clack-linux-amd64`), `clack-windows-amd64` and `clack-darwin-arm64`; `yoloslack-linux-amd64` is no longer built, so update your download scripts. Config lives in `~/.config/clack`, and the first start copies over `~/.config/yoloslack`, which stays in place for older tools. Env vars are `CLACK_HOME`, `CLACK_TIMING`, `CLACK_FRAMELOG`, `CLACK_NOEMBOLDEN` and `CLACK_TYPINGLOG`; the `YOLOSLACK_*` names still work. The window title ends in ` - Clack`. The Go module path is unchanged.

## Scraping policy (Hack Club), non-negotiable
Message contents are memory only. Never write message text, files or attachments to disk, logs or fixtures from real messages; test fixtures must be synthetic. Only metadata may be cached on disk: users, channel names, custom emoji map, and avatar/emoji images.

## Packages and owners
| package | owner (branch) | purpose |
|---|---|---|
| `internal/slack` | backend (`backend`) | HTTP client, realtime gateway, typed methods, login |
| `internal/store` | backend | in-memory state, metadata disk cache |
| `internal/core` | backend | **in-process service API** for the UI (replaces `internal/server`): boot, reads, writes, typed events. No HTTP. |
| `internal/richtext` | richtext (`richtext`) | Slack message → structured document (`Doc`) for native layout. Replaces `internal/render`. |
| `internal/ui/...`, `internal/imgcache`, `cmd/clack`, `assets/fonts`, `.github/workflows` | ui (`ui`) | the Gio app |
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
