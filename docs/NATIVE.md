# yoloslack native architecture (supersedes the web-UI parts of ARCHITECTURE.md)

The user's requirement (2026-11-06): **no JS, no web UI, no Electron. Native and fast.** yoloslack is a single Go binary with a GPU-rendered native window built on **Gio** (`gioui.org`, immediate mode). Every widget is custom-drawn to look like the 2026 Slack desktop client. The HTTP server, `web/` and the HTML renderer `internal/render` will be deleted once the native UI lands.

## Scraping policy (Hack Club), non-negotiable
Message contents are memory only. Never write message text, files or attachments to disk, logs or fixtures from real messages; test fixtures must be synthetic. Only metadata may be cached on disk: users, channel names, custom emoji map, and avatar/emoji images.

## Packages and owners
| package | owner (branch) | purpose |
|---|---|---|
| `internal/slack` | backend (`backend`) | HTTP client, realtime gateway, typed methods, login |
| `internal/store` | backend | in-memory state, metadata disk cache |
| `internal/core` | backend | **in-process service API** for the UI (replaces `internal/server`): boot, reads, writes, typed events. No HTTP. |
| `internal/richtext` | richtext (`richtext`) | Slack message → structured document (`Doc`) for native layout. Replaces `internal/render`. |
| `internal/ui/...`, `internal/imgcache`, `cmd/yoloslack`, `assets/fonts`, `.github/workflows` | ui (`ui`) | the Gio app |

## `internal/richtext` contract
```go
package richtext

type Style uint16
const (Bold Style = 1 << iota; Italic; Strike; Code; Underline; Highlight)

type SpanKind uint8
const (Text SpanKind = iota; Link; User; Channel; Usergroup; Broadcast /* @here @channel @everyone */; Emoji; Date; Break /* hard line break */)

type Span struct {
	Kind  SpanKind
	Text  string // display text, already unescaped and resolved ("@karim", "#general", "here", formatted date)
	Style Style
	URL   string // Link: target. Emoji: image URL (custom or Slack CDN standard).
	ID    string // User/Channel/Usergroup id; Emoji: name (with ::skin-tone-N)
	Self  bool   // User mention of the viewer, or a broadcast (yellow highlight)
}

type BlockKind uint8
const (Paragraph BlockKind = iota; Preformatted; Quote; List; Header; Divider; Context; Section; Image; Actions; Attachment; File; Tombstone)

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
	Children []Block  // nested blocks (e.g. attachment body)
}
type Image struct { URL string; Alt string; W, H int; Title string; Proxy bool /* needs the session cookie (files.slack.com) */ }
type Button struct { Text string; URL string; Style string /* primary|danger|"" */; ActionID, Value, BlockID string }
type Attachment struct {
	Color string; Pretext []Span; ServiceName, ServiceIcon string; AuthorName, AuthorIcon, AuthorLink string
	Title []Span; TitleLink string; Text []Block; Footer []Span; FooterIcon string; TS int64
	Image *Image; Thumb *Image; IsUnfurl bool; IsShare bool; ShareChannel string; FromURL string
}
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
```

## `internal/core` contract
```go
package core
func New(c *slack.Client, s *store.Store) *App
func (a *App) Boot(ctx context.Context) error            // parallel boot; starts realtime
func (a *App) Subscribe() (<-chan Event, func())         // buffered; UI drains and invalidates the window
func (a *App) Snapshot() Snapshot                        // Self User, Team, Convs []Conv, Sections []Section, Counts map[string]Count, Connected bool, Prefs
func (a *App) User(id string) (User, bool); func (a *App) EnsureUsers(ctx, ids []string)
func (a *App) History(ctx, ch, before string) (Page, error)   // Page{Messages []Message ascending, HasMore, LastRead, Conv}
func (a *App) Replies(ctx, ch, ts string) ([]Message, error)
// writes: Send(ctx, SendOpts{Channel, Text, ThreadTS, Broadcast, Blocks}) (Message, error), Edit, Delete, React(ctx,ch,ts,name,add), Mark, Typing,
//         Upload(ctx, UploadOpts), Pin/Unpin, Save/Unsave, MarkUnread, Join/Leave, OpenDM, SetStatus, SetPresence, Command
// reads:  SearchMessages, SearchUsers (directory, for @-autocomplete and DMs), SearchChannels, Members, Pins, Bookmarks,
//         Activity, Threads, Saved, Drafts, DMs, Usergroups, CustomEmoji() map, Commands
// files:  Fetch(ctx, url) ([]byte, mime, error)  // authenticated download for files.slack.com (memory only)

type Message struct { // former render.View, minus HTML
	TS, Channel, User, BotID, Name, Avatar, Subtype, Text, ThreadTS, LatestReply string
	IsBot, Edited, Pinned, Broadcast, System, Pending bool
	ReplyCount int; ReplyUsers []string; Reactions []Reaction /* {Name, Count, Users, URL, Self} */
	Doc richtext.Doc
}
type User struct{ /* former UserView fields */ }
type Conv struct{ /* former ConvView fields */ }; type Section struct{...}; type Count struct{ LastRead, Latest string; Unread bool; Mentions int }
type Event struct {
	Type string // message, update, delete, typing, marked, presence, user, conv, conv_removed, counts, connection, emoji_changed, sidebar_changed, huddle, thread_marked, ...
	Channel, TS, ThreadTS, UserID string; Msg *Message; User *User; Conv *Conv; Reply, Mention bool; Connected bool; Raw json.RawMessage
}
```

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
| `internal/ui` (root) + `cmd/yoloslack` | ui-shell | window/app loop, core event pump, `Host` implementation, shell layout (top nav, rail, sidebar, header + tabs, split panes, resizing), routing/history, keyboard shortcut registry, dark mode, window title/unread badge, desktop notifications, login screen, CI (cgo deps, per-OS builds) |
| `internal/ui/richview` | ui-messages | layout engine for `richtext.Doc`: wrapped styled spans, inline emoji images, mention pills, code/pre/quote/lists, clickable links/mentions, text selection + copy |
| `internal/ui/messages` | ui-messages | virtualized message list, rows (grouping, avatars, names, APP badge, status emoji, timestamps), day dividers (sticky), unread line, reactions, thread bar, hover action bar, files/images/video/audio cards, attachments/unfurls, infinite scroll, jump-to-bottom, thread pane |
| `internal/ui/composer` | ui-composer | rich editor (formatting toolbar, inline styles, lists/quote/code), Enter-to-send, Up-to-edit, autocomplete (@ # : /), attachments (pick/paste/drop) with upload, drafts, typing indicator send, edit mode, "also send to channel" |
| `internal/ui/popups` | ui-popups | overlay system (menu, popover, modal, tooltip, toast), emoji picker (virtualized 65k custom), quick switcher (Ctrl+K), search (dropdown + results view), profile card + full profile, lightbox, message/channel/user/team menus with actions |
| `internal/ui/views` | ui-views | Activity, DMs, Later/Saved, Files, Threads, Huddles, Drafts & sent, Directories, channel details |

### Cross-package interface: `Host`
To avoid import cycles, every feature package defines the small interface it needs **in its own package** (e.g. `messages.Host`). The `internal/ui` root implements all of them on one `*ui.App`. Shared things every Host offers (use these exact names so one implementation satisfies all):
```go
Core() *core.App; Theme() *theme.Theme; Images() *imgcache.Cache; Invalidate()
Navigate(route string)            // "c/<id>", "c/<id>/t/<ts>", "activity", "dms", "later", "files", "threads", "huddles", "drafts", "directories", "search?q="
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
