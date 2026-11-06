// app.js: entry point (ui-core). Boot, router, history, /ws, the shell, and the loader for
// ui-features / ui-views modules (dynamic import with fallbacks so the app runs before they land).
import { api } from './api.js';
import { state, on, emit, putUsers, savePrefs, conv, convName, ensureUsers } from './state.js';
import { esc, h, avatarFor, debounce } from './util.js';
import { icon } from './icons.js';

const T0 = performance.now();

// ---------------------------------------------------------------- extension modules + fallbacks

const fallbackPopups = (() => {
  let open = [];
  const host = () => document.getElementById('popups') || document.body;
  function closeAll() { for (const c of [...open]) c(); open = []; document.body.classList.remove('menu-open'); }
  function place(el, anchor, opts = {}) {
    const r = anchor instanceof Element ? anchor.getBoundingClientRect() : { left: anchor.x, right: anchor.x, top: anchor.y, bottom: anchor.y, width: 0, height: 0 };
    el.style.visibility = 'hidden';
    host().appendChild(el);
    const w = el.offsetWidth, hh = el.offsetHeight;
    let x, y;
    if (opts.placement === 'right') { x = r.right + 8; y = r.top; }
    else { x = r.left; y = r.bottom + 4; if (y + hh > innerHeight - 8) y = r.top - hh - 4; }
    x = Math.max(8, Math.min(x, innerWidth - w - 8));
    y = Math.max(8, Math.min(y, innerHeight - hh - 8));
    el.style.left = x + 'px'; el.style.top = y + 'px'; el.style.visibility = '';
  }
  function openPopover(anchor, el, opts = {}) {
    const back = h('<div class="c-popover__backdrop"></div>');
    const pop = h('<div class="c-popover"></div>');
    pop.appendChild(el);
    host().appendChild(back);
    place(pop, anchor, opts);
    document.body.classList.add('menu-open');
    const close = () => { back.remove(); pop.remove(); open = open.filter((c) => c !== close); if (!open.length) document.body.classList.remove('menu-open'); document.removeEventListener('keydown', key, true); opts.onClose?.(); };
    const key = (e) => { if (e.key === 'Escape') { e.stopPropagation(); close(); } };
    back.addEventListener('mousedown', close);
    document.addEventListener('keydown', key, true);
    open.push(close);
    return close;
  }
  function openMenu(anchor, items, opts = {}) {
    const menu = h('<div class="c-menu" role="menu"></div>');
    let close = () => {};
    for (const it of items) {
      if (it === 'sep') { menu.appendChild(h('<div class="c-menu_separator"></div>')); continue; }
      const b = h(`<button class="c-menu_item${it.danger ? ' c-menu_item--danger' : ''}" role="menuitem">${it.icon ? icon(it.icon, 18) : ''}<span class="c-menu_item__label">${esc(it.label)}</span>${it.hint ? `<span class="c-menu_item__hint">${esc(it.hint)}</span>` : ''}</button>`);
      if (!it.onClick && !it.submenu) b.classList.add('c-menu_item--static');
      b.addEventListener('click', () => { close(); it.onClick?.(); });
      menu.appendChild(b);
    }
    close = openPopover(anchor, menu, opts);
    return close;
  }
  function openModal({ title, body, buttons = [], wide }) {
    const ov = h(`<div class="c-sk-overlay"><div class="c-sk-modal${wide ? ' c-sk-modal--wide' : ''}" role="dialog"><div class="c-sk-modal_header"><h1 class="c-sk-modal_title">${esc(title || '')}</h1><button class="c-sk-modal_close" aria-label="Close">${icon('close', 20)}</button></div><div class="c-sk-modal_content"></div>${buttons.length ? '<div class="c-sk-modal_footer"></div>' : ''}</div></div>`);
    const content = ov.querySelector('.c-sk-modal_content');
    if (typeof body === 'string') content.innerHTML = body; else if (body) content.appendChild(body);
    const close = () => { ov.remove(); document.removeEventListener('keydown', key, true); };
    const key = (e) => { if (e.key === 'Escape') { e.stopPropagation(); close(); } };
    for (const b of buttons) {
      const el = h(`<button class="c-button${b.primary ? ' c-button--primary' : ''}${b.danger ? ' c-button--danger' : ''}">${esc(b.label)}</button>`);
      el.onclick = async () => { const r = await b.onClick?.(); if (r !== false) close(); };
      ov.querySelector('.c-sk-modal_footer').appendChild(el);
    }
    ov.querySelector('.c-sk-modal_close').onclick = close;
    ov.addEventListener('mousedown', (e) => { if (e.target === ov) close(); });
    document.addEventListener('keydown', key, true);
    host().appendChild(ov);
    return close;
  }
  let toastEl, toastT;
  function toast(text) {
    if (!toastEl) { toastEl = h('<div class="c-toast" role="status"></div>'); document.body.appendChild(toastEl); }
    toastEl.textContent = text;
    toastEl.classList.add('c-toast--visible');
    clearTimeout(toastT);
    toastT = setTimeout(() => toastEl.classList.remove('c-toast--visible'), 3500);
  }
  const tooltip = {
    init() {
      const tip = document.getElementById('tooltip');
      if (!tip || tooltip.done) return;
      tooltip.done = true;
      let cur = null, t;
      document.addEventListener('mouseover', (e) => {
        const el = e.target.closest?.('[data-tip]');
        if (el === cur) return;
        cur = el;
        clearTimeout(t);
        tip.hidden = true;
        if (!el) return;
        t = setTimeout(() => {
          if (!document.contains(el)) return;
          tip.textContent = el.dataset.tip;
          tip.hidden = false;
          const r = el.getBoundingClientRect();
          const x = Math.max(8, Math.min(r.left + r.width / 2 - tip.offsetWidth / 2, innerWidth - tip.offsetWidth - 8));
          let y = r.top - tip.offsetHeight - 8;
          if (y < 4) y = r.bottom + 8;
          tip.style.left = x + 'px'; tip.style.top = y + 'px';
        }, 350);
      });
      document.addEventListener('mousedown', () => { clearTimeout(t); tip.hidden = true; }, true);
    },
  };
  return { openMenu, openPopover, openModal, toast, tooltip, closeAll };
})();

const fallbackEmoji = (() => {
  const std = new Map(); // name -> image
  let custom = null; // name -> url | alias:x
  let loading = null;
  const FREQ_KEY = 'yoloslack.emoji.freq';
  async function loadEmoji() {
    if (loading) return loading;
    loading = (async () => {
      try {
        const j = await (await fetch('/static/emoji.json')).json();
        for (const e of j.emoji || []) for (const n of e.n) std.set(n, e.i);
      } catch { /* ignore */ }
      emit('emoji_loaded');
      try { custom = (await api.get('emoji')).emoji || {}; emit('emoji_loaded'); } catch { custom = {}; }
    })();
    return loading;
  }
  function emojiURL(name, depth = 0) {
    if (!name) return null;
    name = name.replace(/^:|:$/g, '');
    const skin = name.match(/^(.*)::skin-tone-(\d)$/);
    if (skin) name = skin[1];
    if (custom && custom[name]) {
      const v = custom[name];
      if (v.startsWith('alias:')) return depth < 3 ? emojiURL(v.slice(6), depth + 1) : null;
      return v;
    }
    const img = std.get(name);
    return img ? `https://a.slack-edge.com/production-standard-emoji-assets/16.0/apple-medium/${img}.png` : null;
  }
  function emojiImg(name, cls = 'c-emoji') {
    const u = emojiURL(name);
    return u ? `<img class="${cls}" src="${esc(u)}" alt=":${esc(name)}:">` : `:${esc(name)}:`;
  }
  function searchEmoji(q, limit = 10) {
    q = (q || '').toLowerCase();
    const out = [], seen = new Set();
    const add = (n) => { if (seen.has(n)) return; const u = emojiURL(n); if (u) { seen.add(n); out.push({ name: n, url: u }); } };
    for (const n of std.keys()) { if (n.startsWith(q)) add(n); if (out.length >= limit) return out; }
    if (custom) for (const n in custom) { if (n.startsWith(q)) add(n); if (out.length >= limit) return out; }
    for (const n of std.keys()) { if (n.includes(q)) add(n); if (out.length >= limit) return out; }
    return out;
  }
  function freq() { try { return JSON.parse(localStorage.getItem(FREQ_KEY)) || {}; } catch { return {}; } }
  function frequentEmoji() { const f = freq(); return Object.keys(f).sort((a, b) => f[b] - f[a]).slice(0, 24); }
  function recordEmojiUse(name) { const f = freq(); f[name] = (f[name] || 0) + 1; localStorage.setItem(FREQ_KEY, JSON.stringify(f)); }
  function openEmojiPicker(anchor, onPick) {
    const el = h(`<div class="p-emoji_picker p-emoji_picker--mini"><div class="p-emoji_picker__header"><input class="p-emoji_picker__search" placeholder="Search all emoji"></div><div class="p-emoji_picker__grid"></div></div>`);
    const grid = el.querySelector('.p-emoji_picker__grid');
    const input = el.querySelector('input');
    const draw = () => {
      const q = input.value.trim().replace(/:/g, '');
      const list = q ? searchEmoji(q, 80) : [...new Set([...frequentEmoji(), 'thumbsup', 'heart', 'joy', 'eyes', 'white_check_mark', 'raised_hands', 'tada', 'fire', 'pray', '100', 'smile', 'thinking_face'])].map((n) => ({ name: n, url: emojiURL(n) })).filter((x) => x.url);
      grid.innerHTML = list.map((e) => `<button class="p-emoji_picker__item" data-name="${esc(e.name)}" data-tip=":${esc(e.name)}:"><img src="${esc(e.url)}" alt=""></button>`).join('');
    };
    input.addEventListener('input', draw);
    input.addEventListener('keydown', (e) => { if (e.key === 'Enter') { const b = grid.querySelector('[data-name]'); if (b) { close(); onPick(b.dataset.name); } } });
    grid.addEventListener('click', (e) => { const b = e.target.closest('[data-name]'); if (b) { close(); recordEmojiUse(b.dataset.name); onPick(b.dataset.name); } });
    draw();
    const close = fallbackPopups.openPopover(anchor, el);
    setTimeout(() => input.focus(), 0);
    return close;
  }
  return { loadEmoji, emojiURL, emojiImg, searchEmoji, frequentEmoji, recordEmojiUse, openEmojiPicker };
})();

const fallbackProfile = {
  async openProfileCard(anchor, id) {
    await ensureUsers([id]);
    const u = state.users.get(id);
    if (!u) return;
    const el = h(`<div class="p-member_profile_card"><img class="p-member_profile_card__avatar" src="${esc(u.avatar192 || avatarFor(u))}" alt=""><div class="p-member_profile_card__body"><div class="p-member_profile_card__name">${esc(u.real_name || u.name)}</div>${u.title ? `<div class="p-member_profile_card__title">${esc(u.title)}</div>` : ''}${u.status_text ? `<div class="p-member_profile_card__status">${u.status_emoji_url ? `<img class="c-emoji" src="${esc(u.status_emoji_url)}" alt="">` : ''}${esc(u.status_text)}</div>` : ''}${u.tz_label ? `<div class="p-member_profile_card__tz">${esc(u.tz_label)}</div>` : ''}<button class="c-button c-button--small" data-dm>Message</button></div></div>`);
    let close = () => {};
    el.querySelector('[data-dm]').onclick = async () => {
      close();
      const existing = [...state.convs.values()].find((c) => c.kind === 'im' && c.user === id);
      if (existing) { navigate('#/c/' + existing.id); return; }
      try { const r = await api.call('conversations.open', { users: id }); if (r.channel?.id) navigate('#/c/' + r.channel.id); } catch (e) { ext.popups.toast(e.code || e.message); }
    };
    close = ext.popups.openPopover(anchor, el, {});
  },
};

const fallbackSwitcher = {
  openQuickSwitcher() {
    const body = h('<div class="p-quick_switcher"><input class="c-input_text" placeholder="Jump to a conversation…"><div class="p-quick_switcher__list"></div></div>');
    const input = body.querySelector('input'), list = body.querySelector('.p-quick_switcher__list');
    let items = [], sel = 0, close;
    const draw = () => {
      const q = input.value.trim().toLowerCase();
      items = [...state.convs.values()].filter((c) => !c.is_archived && (!q || convName(c).toLowerCase().includes(q))).sort((a, b) => (convName(a).toLowerCase().startsWith(q) ? -1 : 0) - (convName(b).toLowerCase().startsWith(q) ? -1 : 0) || convName(a).localeCompare(convName(b))).slice(0, 12);
      sel = Math.min(sel, Math.max(0, items.length - 1));
      list.innerHTML = items.map((c, i) => `<div class="c-menu_item${i === sel ? ' c-menu_item--highlighted' : ''}" data-i="${i}">${c.kind === 'im' ? '' : (c.kind === 'private' ? icon('lock', 16) : icon('hash', 16))}<span class="c-menu_item__label">${esc(convName(c))}</span></div>`).join('');
    };
    input.addEventListener('input', () => { sel = 0; draw(); });
    input.addEventListener('keydown', (e) => {
      if (e.key === 'ArrowDown') { sel = Math.min(items.length - 1, sel + 1); draw(); e.preventDefault(); }
      if (e.key === 'ArrowUp') { sel = Math.max(0, sel - 1); draw(); e.preventDefault(); }
      if (e.key === 'Enter' && items[sel]) { close(); navigate('#/c/' + items[sel].id); }
    });
    list.addEventListener('click', (e) => { const it = e.target.closest('[data-i]'); if (it) { close(); navigate('#/c/' + items[+it.dataset.i].id); } });
    draw();
    close = ext.popups.openModal({ title: 'Jump to…', body });
    setTimeout(() => input.focus(), 0);
  },
};

export const ext = {
  popups: fallbackPopups,
  emoji: fallbackEmoji,
  switcher: fallbackSwitcher,
  search: { openSearch: (q) => ext.popups.toast('Search isn’t available yet') },
  profile: fallbackProfile,
  lightbox: { openLightbox: ({ src }) => window.open(src, '_blank', 'noopener') },
  menus: null,
};
const fallbacks = { ...ext };

async function loadExtensions() {
  const names = ['popups', 'emoji', 'switcher', 'search', 'profile', 'lightbox', 'menus'];
  await Promise.all(names.map(async (n) => {
    try {
      const m = await import(`./${n}.js`);
      ext[n] = Object.assign({}, fallbacks[n] || {}, m);
    } catch (e) {
      console.info(`[ext] ${n}.js not loaded (${e.message.split('\n')[0]}); using fallback`);
    }
  }));
  try { ext.popups.tooltip?.init?.(); } catch (e) { console.warn(e); fallbackPopups.tooltip.init(); }
  Promise.resolve(ext.emoji.loadEmoji?.()).then(() => emit('emoji_loaded')).catch(() => {});
}

// ---------------------------------------------------------------- routing and history

let navStack = [];
let navIndex = -1;
let navigatingHistory = 0;

export function navigate(route) {
  if (!route.startsWith('#')) route = '#' + route;
  if (location.hash === route) { handleRoute(); return; }
  location.hash = route;
}

export function openThread(channel, ts) {
  const route = `#/c/${channel}/t/${ts}`;
  if (state.current.route.startsWith('#/c/') && state.current.channel === channel) navigate(route);
  else if (!state.current.route.startsWith('#/c/')) {
    // from a view (activity, search…): open the thread pane over that view without leaving it
    import('./thread.js').then((m) => m.openThreadPane(channel, ts));
  } else navigate(route);
}

let secondaryClose = null;
export function openSecondary(el, opts = {}) {
  const pane = document.getElementById('secondary');
  if (secondaryClose) { const c = secondaryClose; secondaryClose = null; c(); }
  if (!el) {
    pane.hidden = true;
    pane.replaceChildren();
    document.getElementById('client').classList.remove('p-client--secondary');
    if (state.current.thread) {
      state.current.thread = null;
      if (state.current.route.includes('/t/')) {
        navigatingHistory++;
        history.replaceState(null, '', '#/c/' + state.current.channel);
        state.current.route = '#/c/' + state.current.channel;
        navStack[navIndex] = state.current.route;
        navigatingHistory--;
      }
    }
    return;
  }
  pane.replaceChildren(el);
  pane.hidden = false;
  document.getElementById('client').classList.add('p-client--secondary');
  secondaryClose = opts.onClose || null;
}

export function viewCtx() {
  return { api, state, navigate, openThread, openSecondary, ext, messageHTML: modules.messages?.messageHTML, bindMessageEvents: modules.messages?.bindMessageEvents };
}

const modules = {};
let viewCleanup = null;

const VIEW_ROUTES = ['activity', 'dms', 'later', 'files', 'threads', 'huddles', 'drafts', 'directories', 'search'];
const VIEW_TITLES = { activity: 'Activity', dms: 'Direct messages', later: 'Later', files: 'Files', threads: 'Threads', huddles: 'Huddles', drafts: 'Drafts & sent', directories: 'Directories', search: 'Search' };
const FULL_WIDTH_VIEWS = new Set(['activity', 'dms', 'files', 'later']);

function parseRoute(hash) {
  const m = hash.match(/^#\/c\/([A-Z0-9]+)(?:\/t\/([\d.]+))?(?:\/m\/([\d.]+))?/);
  if (m) return { kind: 'channel', channel: m[1], thread: m[2] || null, ts: m[3] || null };
  const v = hash.match(/^#\/(\w+)/);
  if (v && VIEW_ROUTES.includes(v[1])) return { kind: 'view', view: v[1] };
  const u = hash.match(/^#\/u\/(\w+)/);
  if (u) return { kind: 'user', user: u[1] };
  return { kind: 'home' };
}

function defaultChannel() {
  const last = state.prefs.lastChannel;
  if (last && state.convs.has(last)) return last;
  for (const s of state.sections) if (s.type === 'channels' && s.channels.length) return s.channels[0];
  const general = [...state.convs.values()].find((c) => c.is_general) || state.convs.values().next().value;
  return general?.id;
}

async function handleRoute() {
  const hash = location.hash || '#/home';
  const r = parseRoute(hash);
  if (r.kind === 'user') {
    // profile links: show the card and go back to where we were
    const anchor = document.querySelector(`a[href="${CSS.escape ? hash : hash}"]`) || { x: innerWidth / 2, y: 120 };
    history.replaceState(null, '', state.current.route || '#/home');
    ext.profile?.openProfileCard?.(anchor, r.user);
    return;
  }
  if (r.kind === 'home') {
    const id = defaultChannel();
    if (id) { history.replaceState(null, '', '#/c/' + id); return handleRoute(); }
  }
  // history bookkeeping
  if (!navigatingHistory) {
    if (navStack[navIndex - 1] === hash) navIndex--;
    else if (navStack[navIndex + 1] === hash) navIndex++;
    else if (navStack[navIndex] !== hash) { navStack = navStack.slice(0, navIndex + 1); navStack.push(hash); navIndex = navStack.length - 1; if (navStack.length > 100) { navStack.shift(); navIndex--; } }
  }
  updateHistoryButtons();
  const prev = state.current;
  state.current = { route: hash, channel: r.channel || (r.kind === 'view' ? prev.channel : null), thread: r.thread || null, view: r.view || null };
  const primary = document.getElementById('primary');
  const client = document.getElementById('client');
  client.classList.toggle('p-client--full_width_view', r.kind === 'view' && FULL_WIDTH_VIEWS.has(r.view));
  if (r.kind === 'channel') {
    state.lastHomeRoute = '#/c/' + r.channel;
    if (viewCleanup) { try { viewCleanup(); } catch { /* ignore */ } viewCleanup = null; }
    const ch = modules.channel;
    ch.showChannel(primary, r.channel, { ts: r.ts });
    state.prefs.lastChannel = r.channel;
    savePrefsSoon();
    if (r.thread) { const t = modules.thread; t.openThreadPane(r.channel, r.thread); }
    else if (prev.thread && prev.channel === r.channel) openSecondary(null);
    else if (document.getElementById('secondary').querySelector('.p-thread_view')) openSecondary(null);
    updateTitle();
  } else if (r.kind === 'view') {
    modules.channel.hideCurrentChannel();
    if (viewCleanup) { try { viewCleanup(); } catch { /* ignore */ } viewCleanup = null; }
    await showView(primary, r.view, hash);
    updateTitle();
  }
  emit('route', state.current);
}

async function showView(primary, name, hash) {
  if (name === 'search') {
    const q = decodeURIComponent((hash.split('?q=')[1] || ''));
    if (ext.search?.openSearch && ext.search !== fallbacks.search) { ext.search.openSearch(q); }
  }
  const box = h(`<div class="p-view_contents--view p-view_${name}"></div>`);
  primary.replaceChildren(box);
  let mod = null;
  try { mod = await import(`./views/${name === 'search' ? 'search' : name}.js`); } catch { /* not there yet */ }
  if (state.current.route !== hash) return;
  if (mod?.render) {
    try { viewCleanup = mod.render(box, { ...viewCtx(), route: hash }) || null; } catch (e) { console.error(e); box.innerHTML = errorState(name, e); }
  } else {
    box.innerHTML = `<div class="p-view_list_header"><h1>${esc(VIEW_TITLES[name] || name)}</h1></div><div class="p-empty_state"><h2>${esc(VIEW_TITLES[name] || name)}</h2><p>This view is on its way.</p></div>`;
  }
}

function errorState(name, e) {
  return `<div class="p-empty_state"><h2>Couldn’t open ${esc(VIEW_TITLES[name] || name)}</h2><p>${esc(e.message)}</p></div>`;
}

const savePrefsSoon = debounce(savePrefs, 500);

function updateHistoryButtons() {
  const back = document.getElementById('nav-back'), fwd = document.getElementById('nav-fwd');
  if (back) back.disabled = navIndex <= 0;
  if (fwd) fwd.disabled = navIndex >= navStack.length - 1;
}

function goBack() { if (navIndex > 0) history.back(); }
function goForward() { if (navIndex < navStack.length - 1) history.forward(); }

function routeLabel(route) {
  const r = parseRoute(route);
  if (r.kind === 'channel') { const c = conv(r.channel); return (c && (c.kind === 'channel' || c.kind === 'private') ? '#' : '') + convName(c || { id: r.channel, name: r.channel }) + (r.thread ? ' (thread)' : ''); }
  if (r.kind === 'view') return VIEW_TITLES[r.view] || r.view;
  return route;
}

function updateTitle() {
  const r = parseRoute(state.current.route);
  let unread = false;
  for (const [id, c] of state.counts) if ((c.unread || c.mentions) && state.convs.has(id) && id !== state.current.channel) { unread = true; break; }
  let base;
  if (r.kind === 'channel') {
    const c = conv(r.channel);
    base = `${convName(c) || r.channel} (${c?.kind === 'im' || c?.kind === 'mpim' ? 'DM' : 'Channel'})`;
  } else base = VIEW_TITLES[r.view] || 'Slack';
  document.title = `${unread ? '* ' : ''}${base} - ${state.team?.name || 'Slack'} - Slack`;
}

// ---------------------------------------------------------------- /ws

let ws, wsRetry = 0, wsWasConnected = false;
function connectWS() {
  const url = (location.protocol === 'https:' ? 'wss://' : 'ws://') + location.host + '/ws';
  ws = new WebSocket(url);
  ws.onopen = () => {
    if (wsRetry > 0 || wsWasConnected) resync();
    wsRetry = 0;
    wsWasConnected = true;
    setBanner(null);
  };
  ws.onmessage = (e) => {
    let d;
    try { d = JSON.parse(e.data); } catch { return; }
    try { handleEvent(d); } catch (err) { console.error('ws event', d.type, err); }
  };
  ws.onclose = () => {
    const delay = Math.min(10000, 500 * 2 ** wsRetry++);
    if (wsRetry > 1) setBanner('Reconnecting…');
    setTimeout(connectWS, delay);
  };
}

async function resync() {
  try {
    const b = await api.get('boot');
    applyBoot(b, true);
    emit('counts', { counts: b.counts });
    const ch = modules.channel?.currentChannelView();
    if (ch) modules.channel.fetchHistory(ch.id, { fresh: true }).then(() => ch.list.rerender()).catch(() => {});
  } catch { /* ignore */ }
}

function setBanner(text) {
  let b = document.getElementById('connection-banner');
  if (!text) { b?.remove(); return; }
  if (!b) { b = h('<div id="connection-banner" class="p-connection_banner"></div>'); document.getElementById('workspace').prepend(b); }
  b.textContent = text;
}

const typingTimers = new Map();
function handleEvent(d) {
  switch (d.type) {
    case 'message': {
      const m = d.msg;
      if (d.user) putUsers({ [d.user.id]: d.user });
      const self = m.user === state.self?.id;
      const isCurrent = state.current.channel === d.channel && state.current.route.startsWith('#/c/') && document.hasFocus();
      if ((!d.reply || m.broadcast) && !self) {
        const c = state.counts.get(d.channel) || { last_read: '0', latest: m.ts, unread: false, mentions: 0 };
        c.latest = m.ts;
        const view = modules.channel?.currentChannelView();
        if (!(isCurrent && view?.list.stick)) {
          c.unread = true;
          const kind = d.conv_kind || conv(d.channel)?.kind;
          if (d.mention || kind === 'im' || kind === 'mpim') c.mentions = (c.mentions || 0) + 1;
        }
        state.counts.set(d.channel, c);
      }
      // a message from someone clears their typing indicator
      const t = state.typing.get(d.channel);
      if (t?.delete(m.user)) emit('typing_changed', { channel: d.channel });
      emit('message', d);
      updateTitle();
      break;
    }
    case 'marked': {
      const c = state.counts.get(d.channel) || {};
      c.last_read = d.ts; c.unread = !!d.unread; c.mentions = d.mentions || 0;
      state.counts.set(d.channel, c);
      const hh = state.history.get(d.channel);
      if (hh) hh.lastRead = d.ts;
      emit('marked', d);
      updateTitle();
      break;
    }
    case 'counts':
      if (d.counts) for (const [id, c] of Object.entries(d.counts)) state.counts.set(id, c);
      emit('counts', d);
      updateTitle();
      break;
    case 'presence':
      for (const id of d.users || []) { const u = state.users.get(id); if (u) u.presence = d.presence; }
      emit('presence', d);
      break;
    case 'user': if (d.user) putUsers({ [d.user.id]: d.user }); emit('user', d); break;
    case 'conv': if (d.conv) { state.convs.set(d.conv.id, Object.assign(state.convs.get(d.conv.id) || {}, d.conv)); ensureSectionFor(d.conv); } emit('conv', d); break;
    case 'conv_removed': state.convs.delete(d.channel); for (const s of state.sections) s.channels = s.channels.filter((x) => x !== d.channel); emit('conv_removed', d); break;
    case 'typing': {
      if (d.user === state.self?.id) break;
      if (!state.typing.has(d.channel)) state.typing.set(d.channel, new Map());
      state.typing.get(d.channel).set(d.user, { name: d.name, until: Date.now() + 6000, thread_ts: d.thread_ts || '' });
      emit('typing', d);
      emit('typing_changed', d);
      const k = d.channel + d.user;
      clearTimeout(typingTimers.get(k));
      typingTimers.set(k, setTimeout(() => { state.typing.get(d.channel)?.delete(d.user); emit('typing_changed', d); }, 6100));
      break;
    }
    case 'connection':
      state.connected = d.connected;
      setBanner(d.connected ? null : 'Connecting to Slack…');
      emit('connection', d);
      break;
    case 'sidebar_changed':
      api.get('boot').then((b) => { state.sections = b.sections || state.sections; emit('sections_local'); }).catch(() => {});
      emit('sidebar_changed', d);
      break;
    case 'emoji_changed':
      emit('emoji_changed', d);
      break;
    default:
      emit(d.type, d);
  }
}

function ensureSectionFor(c) {
  if (state.sections.some((s) => s.channels.includes(c.id))) return;
  const type = c.kind === 'im' || c.kind === 'mpim' ? 'direct_messages' : 'channels';
  const s = state.sections.find((x) => x.type === type);
  if (s) s.channels.push(c.id);
}

// ---------------------------------------------------------------- shell

function applyTheme() {
  document.documentElement.classList.toggle('dark', state.prefs.theme === 'dark');
  emit('theme');
}

function mountTopNav() {
  const back = document.getElementById('nav-back'), fwd = document.getElementById('nav-fwd'), hist = document.getElementById('nav-history');
  back.onclick = goBack;
  fwd.onclick = goForward;
  hist.onclick = () => {
    const items = [];
    for (let i = navStack.length - 1; i >= 0 && items.length < 12; i--) {
      if (i === navIndex) continue;
      const route = navStack[i];
      items.push({ label: routeLabel(route), onClick: () => navigate(route) });
    }
    if (!items.length) items.push({ label: 'No recent history' });
    ext.popups.openMenu(hist, items);
  };
  const search = document.getElementById('top-search');
  search.querySelector('.p-top_nav__search__text').textContent = `Search ${state.team?.name || ''}`.trim();
  search.onclick = () => {
    if (ext.search?.openSearch && ext.search !== fallbacks.search) ext.search.openSearch('');
    else navigate('#/search');
  };
  updateHistoryButtons();
}

function bindShortcuts() {
  document.addEventListener('keydown', (e) => {
    const mod = e.metaKey || e.ctrlKey;
    const inField = e.target.closest?.('input, textarea, [contenteditable="true"]');
    if (mod && e.key.toLowerCase() === 'k' && !e.shiftKey) { e.preventDefault(); ext.switcher?.openQuickSwitcher?.(); return; }
    if (mod && e.key === '[') { e.preventDefault(); goBack(); return; }
    if (mod && e.key === ']') { e.preventDefault(); goForward(); return; }
    if (mod && e.shiftKey && e.key.toLowerCase() === 'k') { e.preventDefault(); navigate('#/dms'); return; }
    if (mod && e.key.toLowerCase() === 'g') { e.preventDefault(); document.getElementById('top-search').click(); return; }
    if (e.altKey && (e.key === 'ArrowUp' || e.key === 'ArrowDown')) {
      e.preventDefault();
      const rows = [...document.querySelectorAll('.p-channel_sidebar [data-id]')];
      const i = rows.findIndex((r) => r.classList.contains('p-channel_sidebar__channel--selected'));
      let j = i + (e.key === 'ArrowUp' ? -1 : 1);
      if (e.shiftKey) { // next unread
        const dir = e.key === 'ArrowUp' ? -1 : 1;
        for (j = i + dir; j >= 0 && j < rows.length; j += dir) if (rows[j].classList.contains('p-channel_sidebar__channel--unread')) break;
      }
      if (rows[j]) navigate('#/c/' + rows[j].dataset.id);
      return;
    }
    if (e.key === 'Escape' && !inField && !document.querySelector('.c-popover, .c-sk-overlay')) {
      if (!document.getElementById('secondary').hidden) { openSecondary(null); return; }
    }
    if (e.key === 'Escape' && inField && e.target.closest('.p-thread_view') && !document.querySelector('.c-popover, .c-sk-overlay, .c-autocomplete')) { openSecondary(null); return; }
    // typing anywhere focuses the composer
    if (!inField && !mod && !e.altKey && e.key.length === 1 && !document.querySelector('.c-popover, .c-sk-overlay')) {
      const ch = modules.channel?.currentChannelView();
      if (ch?.composer && state.current.route.startsWith('#/c/')) ch.composer.focus();
    }
  }, false);
  // profile links anywhere (topics, intros): open cards instead of routing
  document.addEventListener('click', (e) => {
    const a = e.target.closest?.('a[href^="#/u/"]');
    if (!a) return;
    e.preventDefault();
    ext.profile?.openProfileCard?.(a, a.getAttribute('href').slice(4));
  });
}

function applyBoot(b, refresh) {
  state.self = b.self;
  state.team = b.team;
  state.enterpriseId = b.enterprise_id;
  state.connected = b.connected;
  putUsers(b.users);
  if (!refresh) state.convs.clear();
  for (const c of b.convs || []) state.convs.set(c.id, Object.assign(state.convs.get(c.id) || {}, c));
  state.sections = b.sections || [];
  for (const [id, c] of Object.entries(b.counts || {})) state.counts.set(id, c);
  if (b.self) state.users.set(b.self.id, Object.assign(state.users.get(b.self.id) || {}, b.self));
}

async function boot() {
  applyTheme();
  const bootEl = document.getElementById('boot');
  const extP = loadExtensions();
  let b;
  try {
    b = await api.get('boot');
  } catch (e) {
    bootEl.innerHTML = `<div class="p-boot__error"><h2>Couldn’t connect to Slack</h2><p>${esc(e.code || e.message)}</p><button class="c-button" onclick="location.reload()">Try again</button></div>`;
    return;
  }
  const tBoot = performance.now();
  applyBoot(b);
  const [rail, sidebar, channel, thread, messages] = await Promise.all([
    import('./rail.js'), import('./sidebar.js'), import('./channel.js'), import('./thread.js'), import('./messages.js'),
  ]);
  Object.assign(modules, { rail, sidebar, channel, thread, messages });
  rail.mountRail(document.getElementById('tab-rail'));
  sidebar.mountSidebar(document.getElementById('sidebar-pane'));
  sidebar.mountResizer(document.getElementById('sidebar-resizer'));
  mountTopNav();
  bindShortcuts();
  on('toggle_theme', () => { state.prefs.theme = state.prefs.theme === 'dark' ? 'light' : 'dark'; savePrefs(); applyTheme(); });
  on('counts_local', updateTitle);
  window.addEventListener('hashchange', handleRoute);
  // Prefetch the history of the default channel while the shell paints.
  const first = parseRoute(location.hash).channel || defaultChannel();
  if (first) channel.prefetch(first);
  await handleRoute();
  connectWS();
  emit('boot');
  requestAnimationFrame(() => {
    bootEl.classList.add('p-boot--done');
    setTimeout(() => bootEl.remove(), 250);
    const ready = performance.now();
    const rec = { bootApiMs: +(tBoot - T0).toFixed(1), firstPaintMs: +(ready - T0).toFixed(1), navStart: +performance.timeOrigin.toFixed(0) };
    (window.__yolo ||= { switches: [] }).boot = rec;
    console.debug('[boot]', rec);
  });
  await extP;
  // warm the other unread channels in the background so switching to them is instant
  setTimeout(() => {
    let n = 0;
    for (const [id, c] of state.counts) if ((c.unread || c.mentions) && state.convs.has(id) && n++ < 6) channel.prefetch(id);
  }, 1500);
}

window.__yolo ||= { switches: [] };
window.__yolo.state = state;
window.__yolo.navigate = navigate;
boot();
