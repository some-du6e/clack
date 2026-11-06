// channel.js: channel view (header, tabs, message list, composer), history cache, live updates (ui-core).
import { esc, h, avatarFor, debounce, tsCmp, fmtDay, plural } from './util.js';
import { icon } from './icons.js';
import { state, on, emit, user, conv, convName, putUsers, ensureUsers, userName } from './state.js';
import { api } from './api.js';
import { MessageList, messageHTML, hydrateTimestamps } from './messages.js';
import { Composer, uploadFiles } from './composer.js';
import { ext, navigate, openThread, openSecondary, viewCtx } from './app.js';

const MAX_LIVE_VIEWS = 10;
const views = new Map(); // channel id -> ChannelView (LRU: most recent last)
let current = null;

// ---------------------------------------------------------------- data

const inflight = new Map();
export function fetchHistory(id, { fresh = false } = {}) {
  const key = id + (fresh ? ':f' : '');
  if (inflight.has(key)) return inflight.get(key);
  const p = api.get('history', { channel: id, fresh: fresh ? 1 : undefined }).then((r) => {
    putUsers(r.users);
    if (r.conv) { state.convs.set(r.conv.id, Object.assign(state.convs.get(r.conv.id) || {}, r.conv)); }
    const prev = state.history.get(id);
    const msgs = r.messages || [];
    if (prev) {
      // keep the same array (live views hold it), merge fresh data in
      const pending = prev.messages.filter((m) => m.pending);
      const older = prev.messages.filter((m) => !m.pending && msgs.length && tsCmp(m.ts, msgs[0].ts) < 0);
      prev.messages.splice(0, prev.messages.length, ...older, ...msgs, ...pending);
      prev.hasMore = older.length ? prev.hasMore : r.has_more;
      prev.lastRead = r.last_read || prev.lastRead;
      prev.fetchedAt = Date.now();
      return prev;
    }
    const entry = { messages: msgs, hasMore: r.has_more, lastRead: r.last_read, fetchedAt: Date.now() };
    state.history.set(id, entry);
    return entry;
  }).finally(() => inflight.delete(key));
  inflight.set(key, p);
  return p;
}

export function prefetch(id) {
  if (!id || state.history.has(id) || inflight.has(id)) return;
  fetchHistory(id).catch(() => {});
}

// ---------------------------------------------------------------- view

function isStarred(id) {
  return state.sections.some((s) => s.type === 'stars' && s.channels.includes(id));
}

function placeholderFor(c) {
  if (!c) return 'Message';
  if (c.kind === 'im') return `Message ${convName(c)}`;
  if (c.kind === 'mpim') return `Message ${convName(c)}`;
  return `Message #${convName(c)}`;
}

function typingText(names) {
  if (!names.length) return '';
  if (names.length === 1) return `${names[0]} is typing…`;
  if (names.length === 2) return `${names[0]} and ${names[1]} are typing…`;
  return 'Several people are typing…';
}

export class ChannelView {
  constructor(id) {
    this.id = id;
    this.el = h(`<div class="p-channel_view" data-channel="${esc(id)}">
<div class="p-view_header_container"></div>
<div class="p-message_pane">
  <div class="p-message_pane__scroller"><div class="p-message_pane__top"></div><div class="p-message_pane__list"></div></div>
  <button class="p-message_pane__jump" hidden></button>
</div>
<div class="p-composer_footer"></div>
</div>`);
    this.headerEl = this.el.querySelector('.p-view_header_container');
    this.scroller = this.el.querySelector('.p-message_pane__scroller');
    this.jump = this.el.querySelector('.p-message_pane__jump');
    this.footer = this.el.querySelector('.p-composer_footer');
    this.list = new MessageList(this.scroller, {
      channel: id,
      getView: (ts) => this.list.msgs.find((m) => m.ts === ts),
      loadOlder: (before) => this.loadOlder(before),
      onBottom: () => this.markRead(),
      onNewBelow: (n) => this.showJump(n),
      introHTML: () => this.introHTML(),
    });
    this.jump.addEventListener('click', () => { this.list.stick = true; this.list.scrollToBottom(); this.showJump(0); });
    this.markRead = debounce(() => this.doMarkRead(), 400);
    this.renderHeader();
    this.renderComposer();
    this.loaded = false;
    this.pos = null;
  }

  get conv() { return conv(this.id); }

  // ---- header

  renderHeader() {
    const c = this.conv || { id: this.id, kind: 'channel', name: this.id };
    const name = convName(c);
    let title;
    if (c.kind === 'im') {
      const u = user(c.user);
      title = `<img class="p-view_header__avatar" src="${esc(avatarFor(u))}" alt=""><span class="p-view_header__channel_title p-view_header__truncated_text">${esc(name)}</span>${u && u.status_emoji_url ? `<img class="c-emoji p-view_header__status" src="${esc(u.status_emoji_url)}" alt="">` : ''}`;
    } else if (c.kind === 'mpim') {
      title = `<span class="p-view_header__channel_title_icon">${icon('users', 18)}</span><span class="p-view_header__channel_title p-view_header__truncated_text">${esc(name)}</span>`;
    } else {
      title = `<span class="p-view_header__channel_title_icon">${icon(c.kind === 'private' ? 'lock' : 'hash', 18)}</span><span class="p-view_header__channel_title p-view_header__truncated_text">${esc(name)}</span>`;
    }
    const starred = isStarred(this.id);
    const topic = c.kind === 'im' || c.kind === 'mpim' ? '' : (c.topic_html || esc(c.topic || ''));
    const members = c.kind === 'im' ? '' : this.facepileHTML(c);
    const tabs = this.tabsHTML(c);
    this.headerEl.innerHTML = `<div class="p-view_header">
<button class="p-view_header__star${starred ? ' p-view_header__star--on' : ''}" data-hact="star" aria-label="${starred ? 'Remove from Starred' : 'Star channel'}" data-tip="${starred ? 'Remove from Starred' : 'Star channel'}">${icon(starred ? 'starFilled' : 'star', 20)}</button>
<div class="p-view_header__text"><button class="p-view_header__big_button" data-hact="details"><div class="p-view_header__title">${title}</div></button>
<div class="p-view_header__topic_container">${topic ? `<button class="p-view_header__topic" data-hact="details" data-tip="${esc(c.topic || '')}">${topic}</button>` : ''}</div></div>
<div class="p-view_header__actions">
${members}
<div class="p-view_header__huddle"><button class="p-view_header__huddle_main" data-hact="huddle" data-tip="Start huddle">${icon('headphones', 20)}<span class="p-view_header__huddle_label">Huddle</span></button><button class="p-view_header__huddle_more" data-hact="huddle-menu" aria-label="Huddle options">${icon('chevronDown', 20)}</button></div>
<button class="p-view_header__notifications" data-hact="notifications" data-tip="Notification settings">${icon('bell', 20)}</button>
<button class="p-view_header__icon" data-hact="search" data-tip="Search in conversation">${icon('search', 20)}</button>
<button class="p-view_header__icon p-view_header__icon--kebab" data-hact="more" data-tip="More actions">${icon('kebab', 16)}</button>
</div></div>${tabs}`;
    hydrateTimestamps(this.headerEl);
    if (!this.headerBound) {
      this.headerBound = true;
      this.headerEl.addEventListener('click', (e) => {
        const b = e.target.closest('[data-hact]');
        if (b) this.headerAction(b.dataset.hact, b, e);
      });
    }
  }

  facepileHTML(c) {
    const ids = (this.memberIds || (c.kind === 'mpim' ? c.members : null) || []).filter((x) => x).slice(0, 3);
    const n = c.num_members || (c.members ? c.members.length : 0);
    const faces = ids.map((id) => `<span class="p-avatar_stack__avatar"><img src="${esc(avatarFor(user(id) || { user: id }))}" alt=""></span>`).join('');
    return `<button class="p-avatar_stack--details" data-hact="members" data-tip="View all members of this channel"><span class="p-avatar_stack__stack__avatars">${faces}</span><span class="p-avatar_stack__count">${n || ''}</span></button>`;
  }

  tabsHTML(c) {
    const tabs = [`<button class="p-view_tab p-view_tab--active" data-hact="tab-messages">${icon('messages', 16)}<span>Messages</span></button>`];
    for (const b of this.bookmarks || []) {
      const iconHTML = b.icon_url ? `<img class="p-view_tab__favicon" src="${esc(b.icon_url)}" alt="">` : icon(b.type === 'canvas' ? 'canvas' : (b.type === 'folder' ? 'folder' : 'link'), 16);
      tabs.push(`<button class="p-view_tab" data-hact="bookmark" data-url="${esc(b.link || '')}" data-tip="${esc(b.link || b.title)}">${iconHTML}<span>${esc(b.title || b.link)}</span></button>`);
    }
    if (this.pinCount) tabs.push(`<button class="p-view_tab" data-hact="pins">${icon('pin', 16)}<span>Pins</span><span class="p-view_tab__count">${this.pinCount}</span></button>`);
    tabs.push(`<button class="p-view_tab__add" data-hact="add-tab" data-tip="Add a tab">${icon('plusSmall', 16)}</button>`);
    return `<div class="p-view_tabs" role="tablist">${tabs.join('')}</div>`;
  }

  async loadHeaderExtras() {
    const c = this.conv;
    if (!c) return;
    const jobs = [];
    if (c.kind !== 'im') {
      jobs.push(api.call('conversations.members', { channel: this.id, limit: '8' }).then((r) => {
        this.memberIds = (r.members || []).filter((id) => id !== state.self?.id).concat(r.members?.includes(state.self?.id) ? [state.self.id] : []);
        return ensureUsers(this.memberIds.slice(0, 3));
      }).catch(() => {}));
    }
    jobs.push(api.call('bookmarks.list', { channel_id: this.id }).then((r) => { this.bookmarks = (r.bookmarks || []).slice(0, 12); }).catch(() => {}));
    jobs.push(api.call('pins.list', { channel: this.id }).then((r) => { this.pins = r.items || []; this.pinCount = this.pins.length; }).catch(() => {}));
    await Promise.all(jobs);
    this.renderHeader();
  }

  async headerAction(a, el, e) {
    const c = this.conv;
    switch (a) {
      case 'star': {
        const on = !isStarred(this.id);
        try {
          await api.call(on ? 'stars.add' : 'stars.remove', { channel: this.id });
          const sec = state.sections.find((s) => s.type === 'stars');
          if (sec) { sec.channels = on ? [...sec.channels, this.id] : sec.channels.filter((x) => x !== this.id); }
          emit('sections_local');
          this.renderHeader();
          ext.popups?.toast?.(on ? 'Starred' : 'Removed from Starred');
        } catch (err) { ext.popups?.toast?.(`Couldn't update star: ${err.code || err.message}`); }
        break;
      }
      case 'details': case 'members': openDetails(this.id, a === 'members' ? 'members' : 'about'); break;
      case 'search': ext.search?.openSearch ? ext.search.openSearch(`in:${c && c.kind !== 'im' ? '#' + convName(c) : '@' + (user(c?.user)?.handle || convName(c))} `) : navigate('#/search?q=' + encodeURIComponent(`in:#${convName(c)} `)); break;
      case 'more': if (ext.menus?.channelMenu) ext.menus.channelMenu(el, c); else channelMenuFallback(el, c); break;
      case 'huddle': case 'huddle-menu': ext.popups?.toast?.('Huddles aren’t supported in yoloslack yet'); break;
      case 'notifications': ext.popups?.openMenu?.(el, [{ label: 'Notifications' , hint: '' }, 'sep', { label: 'All new posts' }, { label: 'Mentions', hint: '' }, { label: 'Nothing' }, 'sep', { label: 'Mute channel' }]); break;
      case 'bookmark': if (el.dataset.url) window.open(el.dataset.url, '_blank', 'noopener'); break;
      case 'pins': this.showPins(); break;
      case 'add-tab': ext.popups?.toast?.('Adding tabs isn’t supported yet'); break;
      default: break;
    }
  }

  showPins() {
    const items = (this.pins || []).filter((p) => p.message);
    const body = h(`<div class="p-flexpane"><div class="p-flexpane_header"><span class="p-flexpane_header__title">Pins</span><span class="p-flexpane_header__sub">#${esc(convName(this.conv))}</span><span class="p-flexpane_header__spacer"></span><button class="p-flexpane_header__close" data-close aria-label="Close">${icon('close', 20)}</button></div><div class="p-flexpane_body p-pins_list"></div></div>`);
    body.querySelector('[data-close]').onclick = () => openSecondary(null);
    const list = body.querySelector('.p-pins_list');
    // pins.list returns raw Slack messages: render their text through /api/render
    list.innerHTML = items.length ? '' : '<div class="p-empty_state">No pinned messages</div>';
    openSecondary(body);
    for (const p of items) {
      const m = p.message;
      const cached = this.list.msgs.find((x) => x.ts === m.ts);
      if (cached) { list.insertAdjacentHTML('beforeend', messageHTML(cached, { hideThreadBar: true })); continue; }
      api.get('render', { text: m.text || '' }).then((r) => {
        list.insertAdjacentHTML('beforeend', messageHTML({ ts: m.ts, channel: this.id, user: m.user, html: `<div class="c-message_kit__blocks"><div class="c-message__message_blocks"><div class="p-rich_text_block">${r.html}</div></div></div>` }, { hideThreadBar: true }));
      }).catch(() => {});
    }
  }

  introHTML() {
    const c = this.conv;
    if (!c) return '';
    if (c.kind === 'im') {
      const u = user(c.user);
      const self = c.user === state.self?.id;
      return `<div class="p-channel_intro p-channel_intro--im"><img class="p-channel_intro__avatar" src="${esc(u?.avatar192 || avatarFor(u))}" alt=""><h2 class="p-channel_intro__title">${esc(convName(c))}${self ? ' <span class="p-channel_intro__you">(you)</span>' : ''}</h2><div class="p-channel_intro__body">${self ? '<b>This is your space.</b> Draft messages, list your to-dos, or keep links and files handy. You can also talk to yourself here, but please bear in mind you’ll have to supply both sides of the conversation.' : `This conversation is just between <a class="c-member_slug c-member_slug--link" data-member-id="${esc(c.user)}" href="#/u/${esc(c.user)}">@${esc(convName(c))}</a> and you. Check out their profile to learn more about them.`}</div></div>`;
    }
    const creator = c.creator ? `<a class="c-member_slug c-member_slug--link" data-member-id="${esc(c.creator)}" href="#/u/${esc(c.creator)}">@${esc(userName(c.creator))}</a>` : 'Someone';
    const when = c.created ? ` on ${fmtDay(String(c.created)).replace(/^(Today|Yesterday)$/, (x) => x.toLowerCase())}` : '';
    return `<div class="p-channel_intro"><h2 class="p-channel_intro__title">${icon(c.kind === 'private' ? 'lock' : 'hash', 28)}${esc(convName(c))}</h2><div class="p-channel_intro__body">${creator} created this channel${when}. This is the very beginning of the <b>${c.kind === 'private' ? '' : '#'}${esc(convName(c))}</b> channel.${c.purpose ? ' ' + esc(c.purpose) : ''}</div></div>`;
  }

  // ---- composer

  renderComposer() {
    const c = this.conv;
    if (c?.is_archived) {
      this.footer.innerHTML = '<div class="p-composer_footer__archived">You are viewing <b>#' + esc(convName(c)) + '</b>, an archived channel.</div>';
      return;
    }
    this.composer?.destroy();
    this.composer = new Composer({
      channel: this.id,
      placeholder: placeholderFor(c),
      draftKey: this.id,
      onSend: (v) => sendMessage({ channel: this.id }, v, this.list),
      onEditLast: () => this.editLast(),
    });
    this.footer.replaceChildren(this.composer.el);
  }

  editLast() {
    const self = state.self?.id;
    for (let i = this.list.msgs.length - 1; i >= 0; i--) {
      const m = this.list.msgs[i];
      if (m.user === self && !m.pending && !m.system && !m.files?.length) { startEdit(this.list, m); return; }
    }
  }

  // ---- data

  async load() {
    const t0 = performance.now();
    const cached = state.history.get(this.id);
    if (cached) {
      this.renderMessages(cached);
      if (Date.now() - cached.fetchedAt > 60000) fetchHistory(this.id).then((e) => this.renderMessages(e, true)).catch(() => {});
    } else {
      this.list.list.innerHTML = '<div class="p-message_pane__loading"><span class="c-spinner"></span></div>';
      try {
        const e = await fetchHistory(this.id);
        this.renderMessages(e);
      } catch (err) {
        this.list.list.innerHTML = `<div class="p-message_pane__error">Couldn’t load messages: ${esc(err.code || err.message)}</div>`;
      }
    }
    this.renderHeader();
    this.loadHeaderExtras();
    this.loaded = true;
    return performance.now() - t0;
  }

  renderMessages(entry, keepPos) {
    const pos = keepPos ? this.list.savePos() : null;
    const count = state.counts.get(this.id);
    this.lastRead = entry.lastRead || count?.last_read;
    this.list.render(entry.messages, { lastRead: this.lastRead, hasMore: entry.hasMore });
    hydrateTimestamps(this.list.list);
    if (pos) this.list.restorePos(pos);
    else if (this.pendingJump) { if (!this.list.highlight(this.pendingJump)) this.list.scrollToBottom(); this.pendingJump = null; }
    else this.list.scrollToBottom();
    if (entry.messages.length && !entry.hasMore) this.list.renderTop();
  }

  async loadOlder(before) {
    if (!before) return null;
    const r = await api.get('history', { channel: this.id, before });
    putUsers(r.users);
    const entry = state.history.get(this.id);
    if (entry) entry.hasMore = r.has_more;
    return r;
  }

  doMarkRead() {
    if (current !== this || !document.hasFocus()) return;
    const latest = [...this.list.msgs].reverse().find((m) => !m.pending);
    if (!latest) return;
    const c = state.counts.get(this.id);
    if (c && tsCmp(c.last_read || '0', latest.ts) >= 0 && !c.unread && !c.mentions) return;
    api.post('mark', { channel: this.id, ts: latest.ts }).catch(() => {});
    const entry = { ...(c || {}), last_read: latest.ts, unread: false, mentions: 0 };
    state.counts.set(this.id, entry);
    const h = state.history.get(this.id);
    if (h) h.lastRead = latest.ts;
    emit('counts_local', { channel: this.id });
  }

  showJump(n) {
    this.jump.hidden = !n;
    this.jump.innerHTML = n ? `${icon('arrowDown', 14)}<span>${plural(n, 'new message')}</span>` : '';
  }

  // ---- lifecycle

  show(container) {
    container.replaceChildren(this.el);
    if (this.pos) this.list.restorePos(this.pos);
    this.list.onScroll();
  }

  hide() {
    this.pos = this.list.savePos();
    this.composer && (this.composerFocus = document.activeElement === this.composer.editor);
  }

  destroy() {
    this.list.destroy();
    this.composer?.destroy();
    this.el.remove();
  }

  // ---- live events

  onEvent(type, d) {
    switch (type) {
      case 'message': this.list.add(d.msg); hydrateTimestamps(this.list.list); if (current === this && this.list.stick) this.markRead(); break;
      case 'update': this.list.update(d.msg); break;
      case 'delete': this.list.remove(d.ts); break;
      case 'typing': this.renderTyping(); break;
      default: break;
    }
  }

  renderTyping() {
    const t = state.typing.get(this.id);
    const now = Date.now();
    const names = [];
    if (t) for (const [id, x] of t) { if (x.until > now && !x.thread_ts) names.push(x.name || userName(id)); }
    this.composer?.setTyping(typingText(names));
  }
}

// ---------------------------------------------------------------- sending and editing

let tempSeq = 0;
export async function sendMessage({ channel, threadTs, broadcast }, v, list) {
  if (v.files?.length) {
    const ok = await uploadFiles(v.files, { channel, threadTs, text: v.text, blocks: v.blocks });
    if (!ok) return;
    return;
  }
  const tempTs = (Date.now() / 1000).toFixed(6).slice(0, -2) + String(++tempSeq % 100).padStart(2, '0');
  const pending = { ts: tempTs, channel, user: state.self.id, html: v.html || `<div class="c-message_kit__blocks"><div class="c-message__message_blocks"><div class="p-rich_text_block"><div class="p-rich_text_section">${esc(v.text)}</div></div></div></div>`, text: v.text, pending: true, thread_ts: threadTs, broadcast: !!(v.broadcast || broadcast) };
  pending._req = { channel, text: v.text, thread_ts: threadTs, broadcast: !!(v.broadcast || broadcast), blocks: v.blocks };
  list.add(pending);
  list.stick = true;
  list.scrollToBottom();
  await doSend(pending, list);
}

async function doSend(pending, list) {
  try {
    const r = await api.post('send', pending._req);
    list.replacePending(pending.ts, r.msg);
    if (!pending.thread_ts || pending.broadcast) emit('sent', { channel: pending.channel, msg: r.msg });
  } catch (e) {
    const i = list.indexOf(pending.ts);
    if (i >= 0) { list.msgs[i] = { ...pending, pending: false, failed: true }; list.replaceRow(i); }
    ext.popups?.toast?.(`Message not sent: ${e.code || e.message}`);
  }
}

on('retry_send', (v) => {
  const view = views.get(v.channel);
  const list = view?.list;
  if (!list || !v._req) return;
  const i = list.indexOf(v.ts);
  if (i < 0) return;
  list.msgs[i] = { ...v, failed: false, pending: true };
  list.replaceRow(i);
  doSend(list.msgs[i], list);
});

// startEdit swaps a row's body for an inline editor (Up arrow, or "Edit message").
export function startEdit(list, v) {
  const row = list.rowFor(v.ts);
  if (!row) return;
  const right = row.querySelector('.c-message_kit__gutter__right');
  const saved = right.innerHTML;
  row.classList.add('c-message_kit__background--editing');
  row.querySelector('.c-message_actions__container')?.remove();
  const done = () => { comp.destroy(); row.classList.remove('c-message_kit__background--editing'); const i = list.indexOf(v.ts); if (i >= 0) list.replaceRow(i); else right.innerHTML = saved; };
  const comp = new Composer({
    channel: v.channel, mode: 'edit', initialHTML: v.html || '',
    onCancel: done,
    onSend: async (nv) => {
      const i = list.indexOf(v.ts);
      if (i >= 0) list.msgs[i] = { ...list.msgs[i], html: nv.html.replace(/<\/div><\/div><\/div>$/, '<span class="c-message__edited_label">(edited)</span></div></div></div>'), text: nv.text, edited: true };
      done();
      try { await api.post('edit', { channel: v.channel, ts: v.ts, text: nv.text, blocks: nv.blocks }); } catch (e) { ext.popups?.toast?.(`Couldn't edit: ${e.code || e.message}`); if (i >= 0) { list.msgs[i] = v; list.replaceRow(i); } }
    },
  });
  // keep the header (name/time) for non-compact rows, replace the body
  right.innerHTML = '';
  right.appendChild(comp.el);
  comp.focus();
  row.scrollIntoView({ block: 'nearest' });
}

on('edit_request', (v) => {
  const view = views.get(v.channel);
  if (view && view.list.indexOf(v.ts) >= 0) { startEdit(view.list, v); return; }
  emit('thread_edit_request', v);
});

// ---------------------------------------------------------------- details / fallbacks

export async function openDetails(id, tab) {
  const c = conv(id);
  let mod = null;
  try { mod = await import('./views/channel_details.js'); } catch { /* not there yet */ }
  if (mod?.render && ext.popups?.openModal) {
    const body = document.createElement('div');
    body.className = 'p-channel_details_modal';
    let close = () => {};
    close = ext.popups.openModal({ title: (c?.kind === 'im' ? '' : '#') + convName(c), body, wide: true, buttons: [] }) || close;
    mod.render(body, { ...viewCtx(), channel: id, tab, close });
    return;
  }
  if (ext.popups?.openModal) {
    ext.popups.openModal({
      title: (c?.kind === 'channel' || c?.kind === 'private' ? '#' : '') + convName(c),
      body: `<div class="p-details_fallback"><p><b>Topic</b><br>${c?.topic_html || esc(c?.topic || 'Add a topic')}</p><p><b>Description</b><br>${esc(c?.purpose || '')}</p><p>${c?.num_members ? plural(c.num_members, 'member') : ''}</p></div>`,
      buttons: [{ label: 'Close' }],
    });
  }
}

function channelMenuFallback(anchor, c) {
  ext.popups?.openMenu?.(anchor, [
    { label: 'Open channel details', onClick: () => openDetails(c.id) },
    { label: 'Copy link', onClick: () => navigator.clipboard?.writeText(`https://${state.team?.domain || 'app'}.slack.com/archives/${c.id}`) },
    'sep',
    { label: 'Mark as read', onClick: () => views.get(c.id)?.doMarkRead() },
  ]);
}

// ---------------------------------------------------------------- show / cache / routing glue

export function showChannel(container, id, { ts } = {}) {
  const t0 = performance.now();
  if (current && current.id !== id) current.hide();
  let v = views.get(id);
  const wasCached = !!v && v.loaded;
  if (v) { views.delete(id); views.set(id, v); }
  else {
    v = new ChannelView(id);
    views.set(id, v);
    while (views.size > MAX_LIVE_VIEWS) {
      const [oldId, old] = views.entries().next().value;
      if (old === current) break;
      views.delete(oldId);
      old.destroy();
    }
  }
  current = v;
  if (ts) v.pendingJump = ts;
  v.show(container);
  let p;
  if (!wasCached) p = v.load();
  else {
    v.renderHeader();
    if (ts && !v.list.highlight(ts)) { /* older than loaded: ignore */ }
    const e = state.history.get(id);
    if (e && Date.now() - e.fetchedAt > 60000) fetchHistory(id).then(() => v.list.rerender()).catch(() => {});
  }
  const syncMs = performance.now() - t0;
  const report = (total) => {
    const rec = { channel: id, cached: wasCached || state.history.has(id), syncMs: +syncMs.toFixed(2), totalMs: +total.toFixed(2) };
    (window.__yolo ||= { switches: [] }).switches.push(rec);
    console.debug(`[switch] ${id} ${rec.cached ? 'cached' : 'cold'} sync ${rec.syncMs}ms total ${rec.totalMs}ms`);
  };
  if (p) p.then(() => requestAnimationFrame(() => report(performance.now() - t0)));
  else requestAnimationFrame(() => report(performance.now() - t0));
  v.markRead();
  if (!ts) setTimeout(() => { if (current === v && !document.activeElement?.closest('.p-flexpane, .c-sk-modal')) v.composer?.focus(false); }, 0);
  return v;
}

export function currentChannelView() { return current; }
export function hideCurrentChannel() { if (current) { current.hide(); current = null; } }
export function channelView(id) { return views.get(id); }

// Apply /ws events to cached history and live views.
function applyHistory(channel, fn) {
  const v = views.get(channel);
  if (v) { fn(v, null); return; }
  const e = state.history.get(channel);
  if (e) fn(null, e);
}

on('message', (d) => {
  if (d.reply && !d.msg.broadcast) return;
  applyHistory(d.channel, (v, e) => {
    if (v) v.onEvent('message', d);
    else if (!e.messages.some((m) => m.ts === d.msg.ts)) e.messages.push(d.msg);
  });
});
for (const t of ['update', 'local_update']) {
  on(t, (d) => {
    applyHistory(d.channel, (v, e) => {
      if (v) v.onEvent('update', d);
      else { const i = e.messages.findIndex((m) => m.ts === d.msg.ts); if (i >= 0) e.messages[i] = { ...e.messages[i], ...d.msg }; }
    });
  });
}
on('delete', (d) => {
  applyHistory(d.channel, (v, e) => {
    if (v) v.onEvent('delete', d);
    else { const i = e.messages.findIndex((m) => m.ts === d.ts); if (i >= 0) e.messages.splice(i, 1); }
  });
});
on('typing_changed', (d) => { views.get(d.channel)?.renderTyping(); });
on('conv', (d) => { const v = views.get(d.conv.id); if (v) { v.renderHeader(); } });
on('users', () => { if (current) { current.renderHeader(); } });
on('sections_local', () => current?.renderHeader());
on('emoji_loaded', () => { for (const v of views.values()) if (v.loaded) v.list.rerender(); });
window.addEventListener('focus', () => current?.markRead());
