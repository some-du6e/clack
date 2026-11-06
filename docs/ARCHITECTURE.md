# yoloslack architecture and contracts

A fast Slack client. A Go binary (`cmd/yoloslack`) talks to Slack with a session token (xoxc + `d` cookie). It keeps state in memory and serves a UI that copies Slack's 2026 web client to a local browser window (`-app` opens a chromeless Chrome window). The UI is plain ES modules and CSS with no build step, embedded via `web/embed.go`. Run with `-dev ./web` to serve the files from disk so UI edits apply without a rebuild.

## Scraping policy (Hack Club), non-negotiable
- Message contents live in memory only. Never write message text, files or rendered HTML to disk, logs or test fixtures taken from real messages. Use synthetic fixtures in tests.
- Only metadata is cached on disk: the user directory, custom emoji map, channel names (`~/.config/yoloslack/cache/meta.json`).
- See https://news.hackclub.com/news/scraping-use-policy/

## Go packages
- `internal/slack`: HTTP client (`Call`, `Raw`, `Edge`, `Download`, `UploadBytes`). Methods that need the org token go to the enterprise host (`entMethods`); `Call` also learns routes at runtime (`team_is_restricted` → enterprise host, `enterprise_is_restricted` → team host). At most 12 requests are in flight; 429s are retried with `Retry-After`; calls slower than 1.5s are logged by method name. Typed helpers live in `methods.go` / `more.go`, the realtime gateway in `rtm.go`, and `Login` (from a `d` cookie) in `login.go`.
- Realtime gateway (verified against app.slack.com, 2026-11): `wss://wss-primary.slack.com/?token=<org token>&gateway_server=<routing_context from client.getWebSocketURL>&enterprise_id=E…&start_args=?agent=client&…&agent_version=<unix ts>…&flannel=3&lazy_channels=1&batch_presence_aware=1`. A bogus `agent_version` (e.g. `1`) makes the gateway close the socket without a `hello`. After `hello` the server sends `presence_sub` (≤500 ids, replaces the set) and `subscription_update/user_change_subscribe_request` (≤200 ids) for self, DM partners and users the UI recently showed.
- `internal/store`: in-memory state (users, convs, timelines, threads, counts, presence, custom emoji, usergroups, prefs). It also implements `render.Resolver`. Unread bookkeeping: `NoteMessage` (new message: unread, mentions++, DMs badge every message, own messages read the conversation) and `MarkRead` (earlier ts = mark unread).
- `internal/render`: Slack message → HTML. Covers mrkdwn, rich_text, Block Kit, attachments/unfurls and files. Output uses Slack's own class names (`c-message_kit__*`, `p-rich_text_*`, `c-mrkdwn__*`, `c-member_slug`, …) so the CSS can follow Slack's. Every `href` goes through `SafeURL` (http/https/mailto/tel/slack only) and every `img src` through `SafeImg`, so bot-supplied `javascript:` links render as plain text. Only Slack's literal `<…>` tokens are links; typed `&lt;…&gt;` stays text.
- `internal/emoji`: standard emoji table (iamcal emoji-data, `standard.json`; images come from Slack's CDN `a.slack-edge.com/production-standard-emoji-assets/16.0/<style>-<size>/<image>.png`) plus custom emoji resolution.
- `internal/server`: boot, the JSON API, RTM event handling, and the push hub at `/ws`.

## HTTP API (server → UI)
All JSON. Errors: `{"ok":false,"error":"<slack error code>"}` with HTTP 502 (Slack said no) or 400 (bad request to us). Responses over 4 KB are gzipped when the browser accepts it. Requests whose `Host` is not loopback/the listen address get 403 (DNS-rebinding guard); non-GET requests with a foreign `Origin` get 403.

| route | method | body / query | response |
|---|---|---|---|
| `/api/boot` | GET | | `{self:UserView, team:{id,name,icon,domain}, convs:[ConvView], sections:[SectionView], counts:{chanId:CountView}, threads:ThreadsView, users:{id:UserView}, prefs:Prefs, connected, emoji_count, usergroup_count, enterprise_id}` |
| `/api/history` | GET | `channel`, optional `before=<ts>` (older page), `fresh=1` | `{messages:[View] ascending, has_more, users:{id:UserView}, conv:ConvView, last_read}` |
| `/api/replies` | GET | `channel`, `ts`, `fresh=1` | `{messages:[View] (parent first), users}` |
| `/api/send` | POST | `{channel, text, thread_ts?, broadcast?, blocks?}` | `{msg:View}` |
| `/api/edit` | POST | `{channel, ts, text, blocks?}` | `{ok}` |
| `/api/delete` | POST | `{channel, ts}` | `{ok}` |
| `/api/react` | POST | `{channel, ts, name, add:bool}` | `{ok}` |
| `/api/mark` | POST | `{channel, ts}`; an earlier `ts` marks unread | `{ok, count:CountView}` (also pushes `marked`) |
| `/api/typing` | POST | `{channel, thread_ts?}` | 204 |
| `/api/users` | GET | `ids=U1,U2` | `{users:{id:UserView}}` |
| `/api/emoji` | GET | | `{emoji:{name:url \| "alias:other"}}` (custom, ~65k on Hack Club) |
| `/static/emoji.json` | GET | | `{categories:[...], emoji:[{n:[names], i:image, u:unified, c:catIdx, s?:{skinCode:image}}]}` |
| `/api/render` | GET | `text` | `{html}` (mrkdwn → HTML) |
| `/api/call` | POST | `{method, params:{k:v}}` or `{edge:"users/list", body:{}}` | raw Slack JSON (allow-listed methods in `http.go: allowedCalls`) |
| `/file` | GET | `u=<files.slack.com url>` | authenticated proxy (memory-only LRU) |
| `/api/prefs` | GET | `fresh=1` | `{prefs:Prefs}` |
| `/api/search` | GET | `q`, `page`, `count` (20), `sort=score\|timestamp\|oldest` | `{messages:[View + permalink], total, page, pages, users, convs:{id:ConvRef}}` (search.messages) |
| `/api/search/people` | GET | `q`, `count` (20), `deleted=1` | `{users:[UserView]}` ranked (edge `users/search`, ~80 ms); subscribes their presence |
| `/api/search/channels` | GET | `q`, `count` (20) | `{channels:[ChannelHit]}` (edge `channels/search`, ~90 ms) |
| `/api/browse/channels` | GET | `q` (may be empty), `page`, `sort=name\|created\|members` | `{channels:[ChannelHit], total, pages, page}` (search.modules.channels) |
| `/api/usergroups` | GET | `q`, `limit`, `fresh=1` | `{usergroups:[{id,handle,name,description,user_count}]}` (prefix matches first) |
| `/api/members` | GET | `channel`, `marker`, `count` (100), `people=1` (no bots/apps) | `{members:[ids], users, next}` (edge `users/list`; pass `next` back as `marker`) |
| `/api/pins` | GET | `channel` | `{items:[{type, created, created_by, msg:View}], users}` |
| `/api/pin` | POST | `{channel, ts, add:bool}` | `{ok}` |
| `/api/bookmarks` | GET | `channel` | `{bookmarks:[{id,title,link,emoji,emoji_url,icon,type,entity_id,created,rank,parent_id}]}` sorted by rank (channel tab bar) |
| `/api/channel` | GET | `channel` | `{conv:ConvView (+purpose_html), info:{is_member, topic_set_by, topic_set_at, purpose_set_by, purpose_set_at, last_read, …}, users}` |
| `/api/activity` | GET | `view=all\|mentions\|threads\|reactions\|invites\|apps` or `types=`, `unread=1`, `cursor`, `limit` (30) | `{items:[ActivityItem], next, users, convs}` (activity.feed + messages.list) |
| `/api/threads` | GET | `cursor` (= previous `next`), `limit` (10) | `{threads:[{channel, root:View, replies:[View], unread_count}], has_more, next, total_unread_replies, threads_state:ThreadsView, users, convs}` (subscriptions.thread.getView) |
| `/api/thread/subscribe` | POST | `{channel, thread_ts, add:bool}` or `{channel, thread_ts, read_ts}` (mark read) | `{ok}` |
| `/api/saved` | GET | `filter=saved\|completed\|archived`, `cursor`, `limit` (50) | `{items:[{item_id, item_type, channel, ts, state, date_created, date_due, date_completed, archived, msg:View}], counts, next, users, convs}` |
| `/api/save` | POST | `{channel, ts, add:true\|false}` or `{channel, ts, completed:bool}` / `{…, archived:bool}` / `{…, date_due}` | `{ok}` (saved.add / delete / update) |
| `/api/drafts` | GET | `limit` | `{drafts:[{id, channel, users, thread_ts, broadcast, html, blocks, date_created, date_scheduled, last_updated, files}], users, convs}` |
| `/api/dms` | GET | `count` (50), `cursor`, `bots=0` | `{dms:[{channel, conv:ConvView, latest:View, count:CountView}] newest first, next, users}` (client.dms; includes closed DMs, which are not added to the sidebar) |
| `/api/presence` | GET | `ids=U1,U2` | `{presence:{id:"active"\|"away"}}`; unknown ids are subscribed and awaited ≤1.5 s |
| `/api/presence/sub` | POST | `{ids:[…]}` | `{presence}` (adds ids to the realtime subscription; updates arrive as `presence` events) |
| `/api/presence/set` | POST | `{presence:"auto"\|"away"}` | `{ok}` |
| `/api/status` | POST | `{text, emoji, expiration}` | `{ok}` (users.profile.set) |
| `/api/dnd` | GET / POST | POST `{minutes}` (0 ends) | `{dnd:<dnd.info>}` |
| `/api/commands` | GET | `q` | `{commands:[{name, desc, usage, type, app}]}` (cached 30 min) |
| `/api/command` | POST | `{channel, command:"/x", text, thread_ts?}` | `{ok, msg?, response?, response_html?}`; `/shrug`, `/me`, `/topic` run locally, the rest via chat.command |
| `/api/join` | POST | `{channel}` | `{conv}` (pushes `conv`) |
| `/api/leave` | POST | `{channel}` (D… closes the DM) | `{ok}` (pushes `conv_removed`) |
| `/api/open` | POST | `{users:[ids]}` | `{channel, conv, users}` (conversations.open; pushes `conv`) |
| `/api/upload` | POST | multipart: `file` (1+), `channel`, `thread_ts`, `initial_comment`, `title` | `{files:[{id,name,title}]}`; held in memory (≤512 MB), never spooled to disk; the message arrives over `/ws` |
| `/ws` | WS | | push events, below |

### Shapes
- `View` (message): `{ts, channel, user, bot_id, name?, avatar?, is_bot, subtype, html, text, edited, thread_ts, reply_count, reply_users, latest_reply, reactions:[{name,count,users,url,self}], pinned, broadcast, system, files:[{id,name,title,mime,filetype,url,download,thumb,w,h,size,permalink}], huddle?}`. File URLs are already proxied (`/file?u=…`). Fresh uploads can arrive as stubs without `url`; the server fills them via files.info and pushes an `update`. `html` is the rendered body (blocks/text plus files and attachments). The UI draws the chrome around it: avatar, name, time, reactions, thread bar.
- `UserView`: `{id, name (display), real_name, handle, avatar (72px), avatar192, title, pronouns, status_text, status_emoji, status_emoji_url, status_exp, tz, tz_offset, tz_label, is_bot, deleted, is_admin, is_owner, guest, color, presence, huddle}`
- `ConvView`: `{id, name, kind: channel|private|im|mpim, user (im partner), members (mpim), topic, topic_html, purpose, purpose_html?, num_members, is_archived, is_general, is_shared, created, creator, priority, muted}`
- `ConvRef`: `{id, name, kind, user?}`, a light descriptor for conversations in search/activity/saved results that may not be in the sidebar.
- `ChannelHit`: `{id, name, kind: channel|private, is_member, is_archived, num_members, purpose, topic, created}`
- `ActivityItem`: `{key, type: at_user|at_user_group|at_channel|at_everyone|keyword|thread_v2|message_reaction|bot_dm_bundle|internal_channel_invite|…, feed_ts, unread, channel, ts, thread_ts, author, msg?:View, root?:View (thread parent), reaction?:{user,name,url}, count?}`
- `ThreadsView`: `{unread:bool, mentions:int}` (Threads sidebar item)
- `Prefs`: `{emoji_style: apple|google, emoji_mode, skin_tone, time24, display_real_names, jumbomoji, convert_emoticons, sidebar_behavior, hide_inactive, channel_sort, locale, theme, dark_mode, sidebar_theme, messages_theme, enter_is_special_in_tbt, show_typing, color_names_in_list, frequent_emoji:[names], muted:[conv ids]}`
- `SectionView`: `{id, name, type: stars|channels|direct_messages|standard|apps|..., emoji, channels:[ids]}` in display order
- `CountView`: `{last_read, latest, unread:bool, mentions:int, muted?}`. DMs count every unread message as a mention (Slack's badge).

### `/ws` push events (`type` field)
`message {channel, msg:View, reply:bool, user:UserView, mention:bool, conv_kind, count:CountView}` ·
`update {channel, msg:View}` (edits, reactions, reply counts, pins) ·
`delete {channel, ts}` ·
`typing {channel, user, name, thread_ts}` ·
`marked {channel, ts, unread, mentions, count:CountView}` ·
`presence {users:[ids], presence}` ·
`user {user:UserView}` ·
`conv {conv:ConvView}` · `conv_removed {channel}` ·
`counts {counts, threads:ThreadsView}` · `connection {connected}` (also sent once right after `/ws` connects) · `emoji_changed` · `sidebar_changed` · `prefs {prefs}` ·
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
