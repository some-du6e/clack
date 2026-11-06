# yoloslack architecture and contracts

A fast Slack client. A Go binary (`cmd/yoloslack`) talks to Slack with a session token (xoxc + `d` cookie). It keeps state in memory and serves a UI that copies Slack's 2026 web client to a local browser window (`-app` opens a chromeless Chrome window). The UI is plain ES modules and CSS with no build step, embedded via `web/embed.go`. Run with `-dev ./web` to serve the files from disk so UI edits apply without a rebuild.

## Scraping policy (Hack Club), non-negotiable
- Message contents live in memory only. Never write message text, files or rendered HTML to disk, logs or test fixtures taken from real messages. Use synthetic fixtures in tests.
- Only metadata is cached on disk: the user directory, custom emoji map, channel names (`~/.config/yoloslack/cache/meta.json`).
- See https://news.hackclub.com/news/scraping-use-policy/

## Go packages
- `internal/slack`: HTTP client (`Call`, `Raw`, `Edge`, `Download`). Methods that need the org token go to the enterprise host (`entMethods`). Also typed helpers (`methods.go`), the realtime gateway (`rtm.go`, wss-primary flannel), and `Login` from a `d` cookie.
- `internal/store`: in-memory state (users, convs, timelines, threads, counts, presence, custom emoji). It also implements `render.Resolver`.
- `internal/render`: Slack message → HTML. Covers mrkdwn, rich_text, Block Kit, attachments/unfurls and files. Output uses Slack's own class names (`c-message_kit__*`, `p-rich_text_*`, `c-mrkdwn__*`, `c-member_slug`, …) so the CSS can follow Slack's.
- `internal/emoji`: standard emoji table (iamcal emoji-data, `standard.json`; images come from Slack's CDN `a.slack-edge.com/production-standard-emoji-assets/16.0/<style>-<size>/<image>.png`) plus custom emoji resolution.
- `internal/server`: boot, the JSON API, RTM event handling, and the push hub at `/ws`.

## HTTP API (server → UI)
All JSON. Errors: `{"ok":false,"error":"<slack error code>"}` with HTTP 502.

| route | method | body / query | response |
|---|---|---|---|
| `/api/boot` | GET | | `{self:UserView, team:{id,name,icon,domain}, convs:[ConvView], sections:[SectionView], counts:{chanId:CountView}, users:{id:UserView}, connected, emoji_count, enterprise_id}` |
| `/api/history` | GET | `channel`, optional `before=<ts>` (older page), `fresh=1` | `{messages:[View] ascending, has_more, users:{id:UserView}, conv:ConvView, last_read}` |
| `/api/replies` | GET | `channel`, `ts`, `fresh=1` | `{messages:[View] (parent first), users}` |
| `/api/send` | POST | `{channel, text, thread_ts?, broadcast?, blocks?}` | `{msg:View}` |
| `/api/edit` | POST | `{channel, ts, text, blocks?}` | `{ok}` |
| `/api/delete` | POST | `{channel, ts}` | `{ok}` |
| `/api/react` | POST | `{channel, ts, name, add:bool}` | `{ok}` |
| `/api/mark` | POST | `{channel, ts}` | `{ok}` |
| `/api/typing` | POST | `{channel, thread_ts?}` | 204 |
| `/api/users` | GET | `ids=U1,U2` | `{users:{id:UserView}}` |
| `/api/emoji` | GET | | `{emoji:{name:url \| "alias:other"}}` (custom, ~65k on Hack Club) |
| `/static/emoji.json` | GET | | `{categories:[...], emoji:[{n:[names], i:image, u:unified, c:catIdx, s?:{skinCode:image}}]}` |
| `/api/render` | GET | `text` | `{html}` (mrkdwn → HTML) |
| `/api/call` | POST | `{method, params:{k:v}}` or `{edge:"users/list", body:{}}` | raw Slack JSON (allow-listed methods in `http.go: allowedCalls`) |
| `/file` | GET | `u=<files.slack.com url>` | authenticated proxy (memory-only LRU) |
| `/ws` | WS | | push events, below |

### Shapes
- `View` (message): `{ts, channel, user, bot_id, name?, avatar?, is_bot, subtype, html, text, edited, thread_ts, reply_count, reply_users, latest_reply, reactions:[{name,count,users,url,self}], pinned, broadcast, system, files:[{id,name,mime,url}], huddle?}`. `html` is the rendered body (blocks/text plus files and attachments). The UI draws the chrome around it: avatar, name, time, reactions, thread bar.
- `UserView`: `{id, name (display), real_name, handle, avatar (72px), avatar192, title, pronouns, status_text, status_emoji, status_emoji_url, status_exp, tz, tz_offset, tz_label, is_bot, deleted, is_admin, is_owner, guest, color, presence, huddle}`
- `ConvView`: `{id, name, kind: channel|private|im|mpim, user (im partner), members (mpim), topic, topic_html, purpose, num_members, is_archived, is_general, is_shared, created, creator, priority}`
- `SectionView`: `{id, name, type: stars|channels|direct_messages|standard|apps|..., emoji, channels:[ids]}` in display order
- `CountView`: `{last_read, latest, unread:bool, mentions:int}`

### `/ws` push events (`type` field)
`message {channel, msg:View, reply:bool, user:UserView, mention:bool, conv_kind}` ·
`update {channel, msg:View}` (edits, reactions, reply counts, pins) ·
`delete {channel, ts}` ·
`typing {channel, user, name, thread_ts}` ·
`marked {channel, ts, unread, mentions}` ·
`presence {users:[ids], presence}` ·
`user {user:UserView}` ·
`conv {conv:ConvView}` · `conv_removed {channel}` ·
`counts {counts}` · `connection {connected}` · `emoji_changed` · `sidebar_changed` ·
`huddle {event, raw}` · passthroughs: `thread_marked`, `update_thread_state`, `dnd_updated`, `pref_change`, `badge_counts_updated` with `raw`.

## Frontend module contracts (`web/js`)
Each module has exactly one owner. Never edit a module you don't own; if you need something from it, code against the contract below and stub it locally if it isn't there yet.

**ui-core owns:** `app.js` (entry: boot, router, ws, shell), `api.js`, `state.js`, `icons.js`, `util.js`, `sidebar.js`, `rail.js`, `messages.js`, `composer.js`, `thread.js`, `channel.js`, plus `css/app.css` and `css/messages.css`.
**ui-features owns:** `popups.js`, `emoji.js`, `switcher.js`, `search.js`, `profile.js`, `lightbox.js`, `menus.js` (message/channel context menus, modals), plus `css/popups.css`.
**ui-views owns:** `views/*.js` (home-less views: `activity.js`, `dms.js`, `later.js`, `files.js`, `threads.js`, `huddles.js`, `drafts.js`, `directories.js`, `channel_details.js`) plus `css/views.css`.

```js
// api.js
export const api = {
  get(path, params) -> Promise<json>,        // GET /api/<path>?params
  post(path, body) -> Promise<json>,         // POST JSON
  call(method, params) -> Promise<json>,     // POST /api/call
  edge(path, body) -> Promise<json>,
};
// state.js
export const state = { self, team, users: Map<id,UserView>, convs: Map<id,ConvView>, counts: Map<id,CountView>,
  sections: SectionView[], current: {route, channel, thread}, prefs: {...} };
export function on(event, fn) / off / emit(event, data)   // events include every /ws type, plus 'route', 'users'
export function user(id) -> UserView|undefined; export function ensureUsers(ids) -> Promise
// util.js
export function esc(s); export function h(html) -> Element (from an HTML string);
export function fmtTime(ts) /* "11:25 AM" */; export function fmtDay(ts) /* "Today", "Yesterday", "Monday, November 3rd" */;
export function tsDate(ts) -> Date; export function avatarFor(view) -> url
// icons.js
export const icons = { name: '<svg…>' }   // 20x20 viewBox SVG strings
// app.js
export function navigate(route)   // routes: '#/c/<id>', '#/c/<id>/t/<ts>', '#/activity', '#/dms', '#/later', '#/files', '#/threads', '#/huddles', '#/drafts', '#/directories', '#/search?q='
export function openThread(channel, ts); export function openSecondary(el|null)  // right-hand pane
// messages.js
export function messageHTML(view, {compact, inThread, showChannel}) -> string  // one row; reuse in views/search
export function bindMessageEvents(container, ctx)                              // hover actions, reactions, thread bars

// popups.js  (ui-features)
export function openMenu(anchor /*Element|{x,y}*/, items /*[{label, hint, icon, danger, onClick, submenu}|'sep']*/, opts) -> close()
export function openPopover(anchor, el, opts) -> close();  export function closeAll()
export function openModal({title, body /*Element|string*/, buttons:[{label, primary, danger, onClick}], wide}) -> close()
export function toast(text); export function tooltip.init()   // auto tooltips for [data-tip]
// emoji.js
export async function loadEmoji(); export function emojiURL(name) -> url|null; export function emojiImg(name, cls) -> html
export function searchEmoji(query, limit) -> [{name, url}]; export function openEmojiPicker(anchor, onPick(name), opts)
export function frequentEmoji() -> names[]; export function recordEmojiUse(name)
// switcher.js / search.js / profile.js / lightbox.js / menus.js
export function openQuickSwitcher(); export function openSearch(query); export function openProfileCard(anchor, userId)
export function openLightbox({src, name, user, ts, items, index}); export function messageMenu(anchor, view); export function channelMenu(anchor, conv)
// views/*.js  (ui-views): each exports
export function render(container, ctx) -> optional cleanup()  // ctx = {api, state, navigate, openThread, messageHTML}
```
