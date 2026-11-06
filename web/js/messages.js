// messages.js: message rows, the list controller and message interactions (ui-core).
import { esc, fmtTime, fmtTimeShort, fmtDay, fmtFull, dayKey, avatarFor, relTime, plural, tsCmp, h, cdnEmoji } from './util.js';
import { icon } from './icons.js';
import { state, user, userName, ensureUsers, emit, conv, convName } from './state.js';
import { api } from './api.js';
import { ext, openThread, navigate } from './app.js';

const GROUP_WINDOW = 300; // seconds: Slack groups consecutive messages by the same author within 5 minutes

// ---------------------------------------------------------------- rendering

function authorKey(v) {
  if (v.user && !v.name) return 'u:' + v.user;
  return 'b:' + (v.user || '') + ':' + (v.bot_id || '') + ':' + (v.name || '') + ':' + (v.avatar || '');
}

export function canGroup(prev, v) {
  if (!prev || !v) return false;
  if (prev.system || v.system || v.broadcast) return false;
  if (authorKey(prev) !== authorKey(v)) return false;
  const dt = parseFloat(v.ts) - parseFloat(prev.ts);
  if (dt < 0 || dt > GROUP_WINDOW) return false;
  return dayKey(prev.ts) === dayKey(v.ts);
}

export function authorName(v) {
  if (v.name) return v.name;
  const u = user(v.user);
  if (u) return u.name || u.real_name || u.handle;
  return v.user || 'Unknown';
}

function isBotView(v) {
  if (v.is_bot) return true;
  const u = user(v.user);
  return !!(u && u.is_bot && v.user !== 'USLACKBOT');
}

function emojiURL(name) {
  try { return ext.emoji?.emojiURL?.(name) || null; } catch { return null; }
}

function emojiTag(name, url, cls = 'c-emoji') {
  url = url || emojiURL(name);
  if (!url) return `<span class="${cls} c-emoji--text">:${esc(name)}:</span>`;
  return `<img class="${cls}" src="${esc(url)}" alt=":${esc(name)}:" draggable="false" loading="lazy">`;
}

function reactionTip(r) {
  const self = state.self?.id;
  const names = (r.users || []).map((id) => (id === self ? 'you' : userName(id)));
  let who;
  if (!names.length) who = `${r.count} ${r.count === 1 ? 'person' : 'people'}`;
  else {
    const extra = r.count - names.length;
    if (extra > 0) names.push(`${extra} other${extra === 1 ? '' : 's'}`);
    who = names.length === 1 ? names[0] : names.slice(0, -1).join(', ') + ' and ' + names[names.length - 1];
  }
  return `${who.charAt(0).toUpperCase() + who.slice(1)} reacted with :${r.name}:`;
}

function reactionsHTML(v) {
  if (!v.reactions || !v.reactions.length) return '';
  let s = '<div class="c-reaction_bar">';
  for (const r of v.reactions) {
    s += `<button class="c-reaction${r.self ? ' c-reaction--reacted' : ''}" data-action="toggle-reaction" data-reaction="${esc(r.name)}" data-tip="${esc(reactionTip(r))}" aria-pressed="${r.self ? 'true' : 'false'}">${emojiTag(r.name, r.url, 'c-reaction__emoji c-emoji')}<span class="c-reaction__count">${r.count}</span></button>`;
  }
  s += `<button class="c-reaction_add" data-action="add-reaction" aria-label="Add reaction" data-tip="Add reaction...">${icon('addReaction', 16)}</button></div>`;
  return s;
}

function replyBarHTML(v) {
  if (!v.reply_count) return '';
  const ids = (v.reply_users || []).slice(0, 5);
  const avatars = ids.map((id) => `<img src="${esc(avatarFor(user(id) || { user: id }))}" alt="" loading="lazy">`).join('');
  const last = v.latest_reply ? `Last reply ${relTime(v.latest_reply)}` : '';
  return `<div class="c-message__reply_bar" data-action="open-thread" role="button" tabindex="0"><span class="c-message__reply_bar__avatars">${avatars}</span><span class="c-message__reply_count">${plural(v.reply_count, 'reply', 'replies')}</span><span class="c-message__reply_bar__description"><span class="c-message__reply_bar__last">${esc(last)}</span><span class="c-message__reply_bar__view">View thread</span></span><span class="c-message__reply_bar__chevron">${icon('chevronRight', 16)}</span></div>`;
}

// System messages (joins, topic changes…) read like Slack: "joined #channel." in the author's row.
function systemBody(v) {
  const c = conv(v.channel);
  const cname = c ? (c.kind === 'channel' || c.kind === 'private' ? '#' + convName(c) : convName(c)) : 'the channel';
  switch (v.subtype) {
    case 'channel_join': case 'group_join':
      return `joined ${esc(cname)}.`;
    case 'channel_leave': case 'group_leave':
      return `left ${esc(cname)}.`;
    default: {
      // Drop the leading "@name " mention the backend renders from Slack's fallback text.
      return (v.html || '').replace(/(<div class="p-rich_text_section">)\s*<a class="c-member_slug[^>]*>[^<]*<\/a>\s*/, '$1');
    }
  }
}

function tsLink(v, short) {
  const label = short ? fmtTimeShort(v.ts) : fmtTime(v.ts);
  return `<a class="c-link c-timestamp${short ? ' c-message_kit__gutter_ts' : ''}" href="#/c/${esc(v.channel)}${v.thread_ts && v.thread_ts !== v.ts ? '/t/' + esc(v.thread_ts) : ''}" data-action="timestamp" data-tip="${esc(fmtFull(v.ts))}"><span class="c-timestamp__label">${esc(label)}</span></a>`;
}

// messageHTML renders one message row. opts: {compact, inThread, showChannel, isParent, unreadMark}
export function messageHTML(v, opts = {}) {
  const compact = !!opts.compact;
  const bot = isBotView(v);
  const u = user(v.user);
  const cls = ['c-message_kit__background', 'c-message_kit__message'];
  if (compact) cls.push('c-message_kit__message--compact');
  if (v.pinned) cls.push('c-message_kit__background--pinned');
  if (v.pending) cls.push('c-message_kit__background--pending');
  if (v.failed) cls.push('c-message_kit__background--failed');
  if (v.system) cls.push('c-message--system');
  if (v.subtype === 'message_deleted' || v.subtype === 'tombstone') cls.push('c-message--tombstone');

  let left;
  if (compact) left = tsLink(v, true);
  else left = `<button class="c-message_kit__avatar" data-action="profile" data-user="${esc(v.user || '')}" tabindex="-1"><img src="${esc(avatarFor(v.avatar ? v : (u || v)))}" alt="" loading="lazy" draggable="false"></button>`;

  let top = '';
  if (v.pinned) top += `<div class="c-message_kit__labels c-message_kit__labels--pinned">${icon('pin', 12)}<span>Pinned</span></div>`;
  if (opts.showChannel && v.channel) {
    const c = conv(v.channel);
    if (c) top += `<div class="c-message_kit__labels c-message_kit__labels--channel"><a href="#/c/${esc(v.channel)}">${c.kind === 'im' || c.kind === 'mpim' ? '' : '#'}${esc(convName(c))}</a></div>`;
  }

  let header = '';
  if (!compact) {
    let status = '';
    if (u && u.status_emoji && !bot) {
      const surl = u.status_emoji_url || emojiURL(u.status_emoji.replace(/:/g, ''));
      if (surl) status = `<img class="c-message__status_emoji c-emoji" src="${esc(surl)}" alt="${esc(u.status_emoji)}" data-tip="${esc(u.status_text || u.status_emoji)}" loading="lazy">`;
    }
    const badge = bot ? '<span class="c-message__app_badge">APP</span>' : '';
    header = `<span class="c-message__sender c-message_kit__sender"><button class="c-message__sender_button" data-action="profile" data-user="${esc(v.user || '')}">${esc(authorName(v))}</button></span>${status}${badge}${tsLink(v, false)}<br>`;
  }

  let broadcast = '';
  if (v.broadcast && v.thread_ts && v.thread_ts !== v.ts) {
    if (opts.inThread) broadcast = '<div class="c-message_kit__broadcast_preamble">Also sent to the channel</div>';
    else {
      const parent = findCached(v.channel, v.thread_ts);
      const snippet = parent ? textSnippet(parent) : 'View thread';
      broadcast = `<div class="c-message_kit__broadcast_preamble">replied to a thread: <a href="#" data-action="open-broadcast-thread">${esc(snippet)}</a></div>`;
    }
  }

  let body = v.system ? `<div class="c-message_kit__blocks"><div class="c-message__message_blocks"><div class="p-rich_text_block"><div class="p-rich_text_section">${systemBody(v)}</div></div></div></div>` : (v.html || '');
  if (v.pending) body += '';
  if (v.failed) body += `<div class="c-message__failed">${icon('info', 14)} Message failed to send. <button class="c-link--button" data-action="retry">Retry</button> · <button class="c-link--button" data-action="discard">Delete</button></div>`;

  const thread = opts.inThread || opts.hideThreadBar ? '' : replyBarHTML(v);
  return `<div class="${cls.join(' ')}" data-ts="${esc(v.ts)}" data-channel="${esc(v.channel || '')}"${v.user ? ` data-user="${esc(v.user)}"` : ''}><div class="c-message_kit__gutter"><div class="c-message_kit__gutter__left">${left}</div><div class="c-message_kit__gutter__right">${top}${broadcast}${header}${body}${reactionsHTML(v)}${thread}</div></div></div>`;
}

function textSnippet(v) {
  const t = (v.text || '').replace(/<@(\w+)(\|[^>]*)?>/g, (_, id) => '@' + userName(id)).replace(/<#(\w+)\|?([^>]*)>/g, (_, id, n) => '#' + (n || id)).replace(/<([^|>]+)\|([^>]+)>/g, '$2').replace(/<([^>]+)>/g, '$1');
  return t.length > 80 ? t.slice(0, 80) + '…' : t || 'View thread';
}

function findCached(channel, ts) {
  const hist = state.history.get(channel);
  return hist?.messages.find((m) => m.ts === ts) || state.threads.get(channel + ':' + ts)?.messages[0];
}

function dayOpenHTML(ts) {
  return `<div class="c-message_list__day" data-day="${dayKey(ts)}"><div class="c-message_list__day_divider" role="separator"><div class="c-message_list__day_divider__line"></div></div><div class="c-message_list__day_divider__label"><button class="c-message_list__day_divider__label__pill" data-action="day-menu" data-ts="${esc(ts)}">${esc(fmtDay(ts))}${icon('chevronDown', 13)}</button></div>`;
}
const UNREAD_HTML = '<div class="c-message_list__unread_divider" role="separator"><span class="c-message_list__unread_divider__label">New</span></div>';

// listHTML renders messages (ascending) into day sections with grouping and the "New" divider.
export function listHTML(msgs, opts = {}) {
  let out = '', prev = null, day = null, unreadDone = !opts.lastRead;
  for (const v of msgs) {
    const dk = dayKey(v.ts);
    if (dk !== day) {
      if (day !== null) out += '</div>';
      out += dayOpenHTML(v.ts);
      day = dk;
      prev = null;
    }
    let breakGroup = false;
    if (!unreadDone && tsCmp(v.ts, opts.lastRead) > 0 && v.user !== state.self?.id && !v.pending) {
      out += UNREAD_HTML;
      unreadDone = true;
      breakGroup = true;
    }
    const compact = !breakGroup && !opts.noGroup && canGroup(prev, v);
    out += messageHTML(v, { compact, inThread: opts.inThread, showChannel: opts.showChannel });
    prev = v;
  }
  if (day !== null) out += '</div>';
  return out;
}

// ---------------------------------------------------------------- list controller

export class MessageList {
  // ctx: {channel, threadTs, inThread, loadOlder(): Promise<{messages, has_more}>, onBottom(latestTs)}
  constructor(scroller, ctx) {
    this.scroller = scroller;
    this.ctx = ctx;
    this.msgs = [];
    this.hasMore = false;
    this.loadingOlder = false;
    this.stick = true;
    this.newBelow = 0;
    this.list = scroller.querySelector('.p-message_pane__list') || scroller;
    this.top = scroller.querySelector('.p-message_pane__top');
    this.scroller.addEventListener('scroll', () => this.onScroll(), { passive: true });
    this.ro = new ResizeObserver(() => { if (this.stick) this.scrollToBottom(); });
    this.ro.observe(this.list);
    bindMessageEvents(scroller, ctx);
  }

  get atBottom() {
    const s = this.scroller;
    return s.scrollHeight - s.scrollTop - s.clientHeight < 4;
  }

  render(msgs, { lastRead, hasMore } = {}) {
    this.msgs = msgs;
    this.hasMore = !!hasMore;
    this.lastRead = lastRead;
    this.list.innerHTML = listHTML(msgs, { lastRead, inThread: this.ctx.inThread });
    this.renderTop();
    ensureMissingUsers(msgs);
  }

  renderTop() {
    if (!this.top) return;
    this.top.innerHTML = this.hasMore ? '<div class="p-message_pane__loading_older"><span class="c-spinner"></span></div>' : (this.ctx.introHTML ? this.ctx.introHTML() : '');
  }

  scrollToBottom() {
    this.scroller.scrollTop = this.scroller.scrollHeight;
  }

  // Remember position as distance from the bottom, which survives prepends and re-renders.
  savePos() {
    const s = this.scroller;
    return { fromBottom: s.scrollHeight - s.scrollTop - s.clientHeight, stick: this.stick };
  }
  restorePos(p) {
    if (!p || p.stick) { this.stick = true; this.scrollToBottom(); return; }
    const s = this.scroller;
    this.stick = false;
    s.scrollTop = s.scrollHeight - s.clientHeight - p.fromBottom;
  }

  rowFor(ts) { return this.list.querySelector(`.c-message_kit__background[data-ts="${CSS.escape(ts)}"]`); }
  indexOf(ts) {
    for (let i = this.msgs.length - 1; i >= 0; i--) if (this.msgs[i].ts === ts) return i;
    return -1;
  }

  // add inserts a live message in ts order (usually at the end).
  add(v) {
    const existing = this.indexOf(v.ts);
    if (existing >= 0) { this.update(v); return; }
    const wasBottom = this.atBottom || this.stick;
    let i = this.msgs.length;
    while (i > 0 && tsCmp(this.msgs[i - 1].ts, v.ts) > 0 && !this.msgs[i - 1].pending) i--;
    // pending rows always stay last; real messages go before them
    while (i > 0 && this.msgs[i - 1].pending && !v.pending) i--;
    this.msgs.splice(i, 0, v);
    if (i !== this.msgs.length - 1) { this.rerender(); return; }
    const prev = this.msgs[i - 1];
    const lastDay = this.list.lastElementChild;
    const html = messageHTML(v, { compact: canGroup(prev, v), inThread: this.ctx.inThread });
    if (lastDay && lastDay.dataset.day === String(dayKey(v.ts))) lastDay.insertAdjacentHTML('beforeend', html);
    else this.list.insertAdjacentHTML('beforeend', dayOpenHTML(v.ts) + html + '</div>');
    ensureMissingUsers([v]);
    if (wasBottom) { this.stick = true; this.scrollToBottom(); }
    else if (!v.pending && v.user !== state.self?.id) { this.newBelow++; this.ctx.onNewBelow?.(this.newBelow); }
  }

  update(v) {
    const i = this.indexOf(v.ts);
    if (i < 0) return;
    const old = this.msgs[i];
    // updates from /ws may lack fields the history copy had (e.g. pending flags): merge
    this.msgs[i] = Object.assign({}, old, v, { pending: v.pending || false });
    this.replaceRow(i);
  }

  replaceRow(i) {
    const v = this.msgs[i];
    const row = this.rowFor(v.ts);
    if (!row) return;
    const compact = row.classList.contains('c-message_kit__message--compact') && canGroup(this.msgs[i - 1], v);
    const el = h(messageHTML(v, { compact, inThread: this.ctx.inThread }));
    if (row.contains(hoverBar)) detachHoverBar();
    row.replaceWith(el);
  }

  // replacePending swaps an optimistic row for the real message.
  replacePending(tempTs, v) {
    const i = this.indexOf(tempTs);
    if (i < 0) { this.add(v); return; }
    const dup = this.indexOf(v.ts);
    if (dup >= 0 && dup !== i) { this.msgs.splice(i, 1); this.rerender(); return; }
    this.msgs[i] = v;
    const row = this.rowFor(tempTs);
    if (row) {
      const el = h(messageHTML(v, { compact: row.classList.contains('c-message_kit__message--compact'), inThread: this.ctx.inThread }));
      row.replaceWith(el);
    }
  }

  remove(ts) {
    const i = this.indexOf(ts);
    if (i < 0) return;
    this.msgs.splice(i, 1);
    this.rerender();
  }

  rerender() {
    const pos = this.savePos();
    this.list.innerHTML = listHTML(this.msgs, { lastRead: this.lastRead, inThread: this.ctx.inThread });
    this.restorePos(pos);
  }

  prepend(older, hasMore) {
    const known = new Set(this.msgs.map((m) => m.ts));
    older = older.filter((m) => !known.has(m.ts));
    this.hasMore = hasMore;
    if (!older.length) { this.renderTop(); return; }
    const pos = this.savePos();
    this.msgs.unshift(...older);
    const html = listHTML(older, { lastRead: null, inThread: this.ctx.inThread });
    const frag = h(`<div>${html}</div>`);
    const first = this.list.firstElementChild;
    const olderSections = [...frag.children];
    const lastOlder = olderSections[olderSections.length - 1];
    if (first && lastOlder && first.dataset.day === lastOlder.dataset.day) {
      // merge the boundary day: move older rows after the existing section's label
      const label = first.querySelector(':scope > .c-message_list__day_divider__label');
      const rows = [...lastOlder.querySelectorAll(':scope > .c-message_kit__background')];
      label.after(...rows);
      lastOlder.remove();
      // the first previously-rendered row may now group with the last older one
      const firstOld = this.msgs[older.length];
      const i = older.length;
      if (canGroup(this.msgs[i - 1], firstOld)) {
        const row = this.rowFor(firstOld.ts);
        if (row) row.replaceWith(h(messageHTML(firstOld, { compact: true, inThread: this.ctx.inThread })));
      }
    }
    this.list.prepend(...frag.children);
    this.renderTop();
    this.restorePos(pos);
    ensureMissingUsers(older);
  }

  async onScroll() {
    const s = this.scroller;
    const bottom = this.atBottom;
    this.stick = bottom;
    if (bottom) {
      if (this.newBelow) { this.newBelow = 0; this.ctx.onNewBelow?.(0); }
      this.ctx.onBottom?.();
    }
    if (s.scrollTop < 1200 && this.hasMore && !this.loadingOlder && this.ctx.loadOlder) {
      this.loadingOlder = true;
      try {
        const r = await this.ctx.loadOlder(this.msgs.find((m) => !m.pending)?.ts);
        if (r) this.prepend(r.messages || [], r.has_more);
      } catch (e) { console.warn('load older', e); }
      this.loadingOlder = false;
    }
  }

  highlight(ts) {
    const row = this.rowFor(ts);
    if (!row) return false;
    this.stick = false;
    row.scrollIntoView({ block: 'center' });
    row.classList.add('c-message_kit__background--highlight');
    setTimeout(() => row.classList.remove('c-message_kit__background--highlight'), 3000);
    return true;
  }

  destroy() { this.ro.disconnect(); }
}

function ensureMissingUsers(msgs) {
  const ids = new Set();
  for (const m of msgs) {
    if (m.user && !state.users.has(m.user)) ids.add(m.user);
    for (const id of m.reply_users || []) if (!state.users.has(id)) ids.add(id);
  }
  if (ids.size) ensureUsers([...ids]);
}

// ---------------------------------------------------------------- hover actions

let hoverBar = null;
let hoverRow = null;
const DEFAULT_QUICK = [
  { name: 'white_check_mark', url: cdnEmoji('2705') },
  { name: 'eyes', url: cdnEmoji('1f440') },
  { name: 'raised_hands', url: cdnEmoji('1f64c') },
];

function quickReactions() {
  let names = [];
  try { names = ext.emoji?.frequentEmoji?.() || []; } catch { /* ignore */ }
  const out = [];
  for (const n of names) {
    const url = emojiURL(n);
    if (url) out.push({ name: n, url });
    if (out.length === 3) break;
  }
  for (const d of DEFAULT_QUICK) if (out.length < 3 && !out.some((o) => o.name === d.name)) out.push(d);
  return out;
}

function hoverBarHTML(inThread, isReply) {
  const q = quickReactions().map((r) => `<button class="c-message_actions__button c-message_actions__emoji" data-action="quick-react" data-reaction="${esc(r.name)}" data-tip=":${esc(r.name)}:"><img src="${esc(r.url)}" alt=":${esc(r.name)}:"></button>`).join('');
  return `<div class="c-message_actions__container c-message__actions" role="toolbar" aria-label="Message actions"><div class="c-message_actions__group">${q}<button class="c-message_actions__button" data-action="add-reaction" data-tip="Find another reaction">${icon('addReaction', 18)}</button>${inThread && isReply ? '' : `<button class="c-message_actions__button" data-action="reply" data-tip="Reply in thread">${icon('reply', 18)}</button>`}<button class="c-message_actions__button" data-action="forward" data-tip="Forward message...">${icon('forward', 18)}</button><button class="c-message_actions__button" data-action="save" data-tip="Save for later">${icon('bookmark', 18)}</button><button class="c-message_actions__button" data-action="more" data-tip="More actions">${icon('kebab', 18)}</button></div></div>`;
}

function detachHoverBar() {
  if (hoverBar && hoverBar.parentNode) hoverBar.remove();
  hoverRow?.classList.remove('c-message_kit__background--hovered');
  hoverRow = null;
}

function attachHoverBar(row, ctx) {
  if (hoverRow === row || row.classList.contains('c-message_kit__background--pending') || row.classList.contains('c-message_kit__background--editing')) return;
  if (document.body.classList.contains('menu-open')) return;
  detachHoverBar();
  const isReply = !!(ctx.inThread && row.dataset.ts !== ctx.threadTs);
  hoverBar = h(hoverBarHTML(ctx.inThread, isReply));
  row.appendChild(hoverBar);
  hoverRow = row;
  row.classList.add('c-message_kit__background--hovered');
}

// ---------------------------------------------------------------- interactions

export function viewFor(row, ctx) {
  const ts = row.dataset.ts, ch = row.dataset.channel || ctx.channel;
  if (ctx.getView) { const v = ctx.getView(ts); if (v) return v; }
  return findCached(ch, ts) || state.threads.get(ch + ':' + (ctx.threadTs || ''))?.messages.find((m) => m.ts === ts);
}

export async function toggleReaction(v, name, add) {
  if (!v) return;
  const self = state.self.id;
  const rs = (v.reactions || []).map((r) => ({ ...r, users: [...(r.users || [])] }));
  let r = rs.find((x) => x.name === name);
  if (add === undefined) add = !(r && r.self);
  if (add) {
    if (r && r.self) return;
    if (r) { r.count++; r.self = true; r.users.push(self); }
    else rs.push({ name, count: 1, users: [self], self: true, url: emojiURL(name) || '' });
    try { ext.emoji?.recordEmojiUse?.(name); } catch { /* ignore */ }
  } else {
    if (!r || !r.self) return;
    r.count--; r.self = false; r.users = r.users.filter((u) => u !== self);
    if (r.count <= 0) rs.splice(rs.indexOf(r), 1);
  }
  const updated = { ...v, reactions: rs };
  emit('local_update', { channel: v.channel, msg: updated });
  try {
    await api.post('react', { channel: v.channel, ts: v.ts, name, add });
  } catch (e) {
    emit('local_update', { channel: v.channel, msg: v });
    ext.popups?.toast?.(`Couldn't ${add ? 'add' : 'remove'} reaction: ${e.code || e.message}`);
  }
}

export async function deleteMessage(v) {
  const doIt = async () => {
    try { await api.post('delete', { channel: v.channel, ts: v.ts }); emit('delete', { channel: v.channel, ts: v.ts }); } catch (e) { ext.popups?.toast?.(`Couldn't delete: ${e.code || e.message}`); }
  };
  if (ext.popups?.openModal) {
    ext.popups.openModal({
      title: 'Delete message',
      body: `<p>Are you sure you want to delete this message? This cannot be undone.</p><div class="c-message_preview">${messageHTML(v, { hideThreadBar: true })}</div>`,
      buttons: [{ label: 'Cancel' }, { label: 'Delete', danger: true, onClick: doIt }],
    });
  } else if (confirm('Delete this message? This cannot be undone.')) doIt();
}

function fallbackMessageMenu(anchor, v) {
  const mine = v.user === state.self?.id;
  const items = [
    { label: 'Copy link', onClick: () => copyLink(v) },
    { label: 'Copy text', onClick: () => navigator.clipboard?.writeText(v.text || '') },
    { label: 'Mark unread', onClick: () => markUnread(v) },
  ];
  if (mine) items.push('sep', { label: 'Edit message', hint: 'E', onClick: () => emit('edit_request', v) }, { label: 'Delete message...', danger: true, onClick: () => deleteMessage(v) });
  ext.popups?.openMenu?.(anchor, items);
}

export function copyLink(v) {
  const dom = state.team?.domain || 'app';
  const url = `https://${dom}.slack.com/archives/${v.channel}/p${v.ts.replace('.', '')}${v.thread_ts && v.thread_ts !== v.ts ? `?thread_ts=${v.thread_ts}&cid=${v.channel}` : ''}`;
  navigator.clipboard?.writeText(url).then(() => ext.popups?.toast?.('Link copied'), () => {});
}

export async function markUnread(v) {
  // Marking read at the message before this one makes it the first unread.
  const prev = (parseFloat(v.ts) - 0.000001).toFixed(6);
  try { await api.post('mark', { channel: v.channel, ts: prev }); emit('marked', { channel: v.channel, ts: prev, unread: true, mentions: state.counts.get(v.channel)?.mentions || 0, local: true }); } catch (e) { ext.popups?.toast?.(e.code || e.message); }
}

async function saveForLater(v) {
  try {
    await api.call('saved.add', { item_type: 'message', item_id: v.channel, ts: v.ts });
    ext.popups?.toast?.('Saved for later');
  } catch (e) { ext.popups?.toast?.(`Couldn't save: ${e.code || e.message}`); }
}

function openPicker(anchor, onPick) {
  if (ext.emoji?.openEmojiPicker) ext.emoji.openEmojiPicker(anchor, onPick, {});
}

// bindMessageEvents wires hover toolbars and clicks for every message row inside container.
// ctx: {channel, threadTs, inThread, getView(ts)}
export function bindMessageEvents(container, ctx) {
  if (container.__msgBound) return;
  container.__msgBound = true;
  container.addEventListener('mouseover', (e) => {
    const row = e.target.closest('.c-message_kit__background');
    if (row && container.contains(row)) attachHoverBar(row, ctx);
  });
  container.addEventListener('mouseleave', () => { if (!hoverBar?.classList.contains('c-message_actions__container--open')) detachHoverBar(); });

  container.addEventListener('click', (e) => {
    const t = e.target;
    const row = t.closest('.c-message_kit__background');
    const actEl = t.closest('[data-action]');
    // links inside message bodies
    const slug = t.closest('.c-member_slug');
    if (slug && slug.dataset.memberId) { e.preventDefault(); ext.profile?.openProfileCard?.(slug, slug.dataset.memberId); return; }
    const zoom = t.closest('[data-zoom]');
    if (zoom && row) {
      e.preventDefault();
      const src = zoom.dataset.zoom || zoom.getAttribute('src') || zoom.getAttribute('href');
      const v = viewFor(row, ctx);
      const imgs = [...row.querySelectorAll('[data-zoom]')];
      const items = imgs.map((z) => ({ src: z.dataset.zoom || z.getAttribute('src'), name: z.querySelector('img')?.alt || z.getAttribute('alt') || '' }));
      if (ext.lightbox?.openLightbox) ext.lightbox.openLightbox({ src, name: items[imgs.indexOf(zoom)]?.name, user: v?.user, ts: v?.ts, items, index: imgs.indexOf(zoom) });
      else window.open(src, '_blank');
      return;
    }
    if (!actEl || !row) return;
    const v = viewFor(row, ctx);
    const a = actEl.dataset.action;
    switch (a) {
      case 'toggle-reaction': toggleReaction(v, actEl.dataset.reaction); break;
      case 'quick-react': toggleReaction(v, actEl.dataset.reaction, true); break;
      case 'add-reaction': {
        const keep = hoverBar && hoverBar.contains(actEl);
        if (keep) hoverBar.classList.add('c-message_actions__container--open');
        openPicker(actEl, (name) => toggleReaction(v, name, true));
        if (!ext.emoji?.openEmojiPicker) fallbackPicker(actEl, (name) => toggleReaction(v, name, true));
        if (keep) setTimeout(() => hoverBar?.classList.remove('c-message_actions__container--open'), 300);
        break;
      }
      case 'reply': case 'open-thread':
        openThread(v.channel || ctx.channel, v.thread_ts && v.thread_ts !== v.ts && !v.reply_count ? v.thread_ts : v.ts);
        break;
      case 'open-broadcast-thread': e.preventDefault(); openThread(v.channel, v.thread_ts); break;
      case 'profile': if (actEl.dataset.user) ext.profile?.openProfileCard?.(actEl, actEl.dataset.user); break;
      case 'save': saveForLater(v); break;
      case 'forward': ext.menus?.forwardMessage ? ext.menus.forwardMessage(v) : copyLink(v); break;
      case 'more':
        if (ext.menus?.messageMenu) ext.menus.messageMenu(actEl, v);
        else fallbackMessageMenu(actEl, v);
        break;
      case 'timestamp': e.preventDefault(); copyLink(v); break;
      case 'retry': emit('retry_send', v); break;
      case 'discard': emit('delete', { channel: v.channel, ts: v.ts }); break;
      case 'day-menu': break;
      default: break;
    }
  });
  container.addEventListener('keydown', (e) => {
    if ((e.key === 'Enter' || e.key === ' ') && e.target.matches('.c-message__reply_bar')) { e.preventDefault(); e.target.click(); }
  });
}

// Tiny emoji chooser used only until emoji.js (ui-features) lands.
function fallbackPicker(anchor, onPick) {
  const name = prompt('Emoji name (without colons):', 'thumbsup');
  if (name) onPick(name.replace(/:/g, '').trim());
}

// Fill client-side formatted timestamp tokens that the backend left empty.
export function hydrateTimestamps(root) {
  for (const el of root.querySelectorAll('.c-timestamp-token:empty')) {
    const ts = el.dataset.ts;
    if (ts) el.textContent = fmtDay(ts) + ' at ' + fmtTime(ts);
  }
}

export { navigate };
