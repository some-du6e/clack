// sidebar.js: the channel sidebar (ui-core).
import { esc, h, avatarFor, throttleRAF, clamp } from './util.js';
import { icon } from './icons.js';
import { state, on, emit, user, conv, convName, isUnread, savePrefs } from './state.js';
import { navigate, ext } from './app.js';
import { prefetch } from './channel.js';

let root, listEl, filterInput;
let filterText = '';

const STATIC = [
  { route: '#/threads', label: 'Threads', icon: 'threads' },
  { route: '#/huddles', label: 'Huddles', icon: 'headphones' },
  { route: '#/drafts', label: 'Drafts & sent', icon: 'drafts' },
  { route: '#/later', label: 'Saved', icon: 'saved' },
  { route: '#/directories', label: 'Directories', icon: 'directories' },
];

const SECTION_ICON = { stars: 'star', channels: 'hashBox', direct_messages: 'bubble', apps: 'appsDots', standard: 'folder', recent_apps: 'appsDots' };

export function mountSidebar(el) {
  root = el;
  root.innerHTML = `<div class="p-sidebar">
<div class="p-ia4_sidebar_header">
  <button class="p-ia4_home_header_menu__button" data-sact="team"><span class="p-ia4_home_header_menu__team_name"></span>${icon('chevronDown', 18)}</button>
  <div class="p-ia4_sidebar_header__controls">
    <button class="p-ia4_sidebar_header__button" data-sact="settings" aria-label="Edit sidebar" data-tip="Edit sidebar">${icon('gear', 20)}</button>
    <button class="p-ia4_sidebar_header__button" data-sact="compose" aria-label="New message" data-tip="New message">${icon('compose', 20)}</button>
  </div>
</div>
<div class="p-sidebar_text_filter_input_header"><label class="p-sidebar_filter">${icon('filter', 16)}<input class="p-sidebar_filter__input" type="text" placeholder="Find a conversation…" spellcheck="false" autocomplete="off"></label></div>
<div class="p-channel_sidebar" role="tree"></div>
</div>`;
  listEl = root.querySelector('.p-channel_sidebar');
  filterInput = root.querySelector('.p-sidebar_filter__input');
  filterInput.addEventListener('input', () => { filterText = filterInput.value.trim().toLowerCase(); render(); });
  filterInput.addEventListener('keydown', (e) => {
    if (e.key === 'Escape') { filterInput.value = ''; filterText = ''; render(); filterInput.blur(); }
    if (e.key === 'Enter') { const first = listEl.querySelector('[data-id]'); if (first) { navigate('#/c/' + first.dataset.id); filterInput.value = ''; filterText = ''; render(); } }
  });
  root.addEventListener('click', onClick);
  root.addEventListener('mousedown', (e) => { const r = e.target.closest('[data-id]'); if (r) prefetch(r.dataset.id); });
  let hoverT;
  listEl.addEventListener('mouseover', (e) => {
    const r = e.target.closest('[data-id]');
    clearTimeout(hoverT);
    if (r) hoverT = setTimeout(() => prefetch(r.dataset.id), 60);
  });
  listEl.addEventListener('contextmenu', (e) => {
    const r = e.target.closest('[data-id]');
    if (!r) return;
    e.preventDefault();
    const c = conv(r.dataset.id);
    if (ext.menus?.channelMenu) ext.menus.channelMenu({ x: e.clientX, y: e.clientY }, c);
  });
  render();
  const rr = throttleRAF(render);
  for (const ev of ['counts', 'counts_local', 'marked', 'conv', 'conv_removed', 'presence', 'users', 'sections_local', 'boot', 'message', 'sent']) on(ev, rr);
  on('route', updateSelection);
}

function selectedId() { return state.current.channel; }

function rowHTML(c, opts = {}) {
  const cnt = state.counts.get(c.id);
  const unread = isUnread(c.id) && c.id !== selectedId();
  const mentions = cnt?.mentions || 0;
  const sel = c.id === selectedId() && state.current.route.startsWith('#/c/');
  const cls = ['p-channel_sidebar__channel'];
  if (sel) cls.push('p-channel_sidebar__channel--selected');
  if (unread || (mentions && !sel)) cls.push('p-channel_sidebar__channel--unread');
  if (mentions && !sel) cls.push('p-channel_sidebar__channel--has-badge');
  let iconHTML, extra = '';
  if (c.kind === 'im') {
    const u = user(c.user);
    const self = c.user === state.self?.id;
    const pres = u?.presence === 'active' || self ? ' c-presence--active' : '';
    const bot = isAppDM(c);
    iconHTML = `<span class="p-channel_sidebar__user_avatar"><img src="${esc(avatarFor(u || { user: c.user }))}" alt="" loading="lazy">${bot ? '' : `<span class="c-presence${pres}"></span>`}</span>`;
    if (c.user === state.self?.id) extra = '<span class="p-channel_sidebar__member_label">you</span>';
    if (!pres && !bot) cls.push('p-channel_sidebar__channel--away');
  } else if (c.kind === 'mpim') {
    const n = (c.members || []).filter((id) => id !== state.self?.id).length;
    iconHTML = `<span class="p-channel_sidebar__channel_icon_prefix p-channel_sidebar__mpim_count">${n || ''}</span>`;
  } else {
    iconHTML = `<span class="p-channel_sidebar__channel_icon_prefix">${icon(c.kind === 'private' ? 'lock' : 'hashSmall', 16)}</span>`;
  }
  const badge = mentions && !sel ? `<span class="p-channel_sidebar__badge">${mentions}</span>` : '';
  return `<div class="${cls.join(' ')}" data-id="${esc(c.id)}" role="treeitem" tabindex="-1">${iconHTML}<span class="p-channel_sidebar__name"><span>${esc(convName(c))}</span>${extra}</span>${badge}</div>`;
}

export function isAppDM(c) {
  if (!c || c.kind !== 'im') return false;
  if (c.user === 'USLACK' || c.user === 'USLACKBOT') return true;
  return !!user(c.user)?.is_bot;
}

function hasActivity(id) {
  const c = state.counts.get(id);
  return !!(c && ((c.latest && !/^0+\.0+$/.test(c.latest)) || c.unread || c.mentions));
}

// effectiveSections moves app DMs into Apps (the way Slack files them) and hides idle app DMs.
function effectiveSections() {
  const apps = state.sections.find((s) => s.type === 'apps');
  const moved = [];
  const out = state.sections.map((s) => {
    if (s.type !== 'direct_messages' || !apps) return s;
    const keep = [];
    for (const id of s.channels) (isAppDM(conv(id)) ? moved : keep).push(id);
    return { ...s, channels: keep };
  });
  return out.map((s) => {
    if (s !== apps && s.id !== apps?.id) return s;
    const ids = [...new Set([...moved, ...s.channels])].filter((id) => hasActivity(id) || id === selectedId());
    return { ...s, channels: ids };
  });
}

function sectionHTML(s) {
  const collapsed = !!state.prefs.collapsed[s.id];
  const convs = s.channels.map(conv).filter((c) => c && !c.is_archived);
  const shown = collapsed ? convs.filter((c) => isUnread(c.id) || c.id === selectedId()) : convs;
  const ic = s.emoji ? emojiIcon(s.emoji) : icon(SECTION_ICON[s.type] || 'folder', 16);
  let body = shown.map((c) => rowHTML(c)).join('');
  if (!collapsed) {
    if (s.type === 'stars' && !convs.length) body += '<div class="p-channel_sidebar__empty">Drag and drop important stuff here</div>';
    if (s.type === 'channels') body += `<div class="p-channel_sidebar__link p-channel_sidebar__link--add-more-items" data-sact="add-channels"><span class="p-channel_sidebar__channel_icon_prefix p-channel_sidebar__channel_icon_prefix--bg">${icon('plusSmall', 14)}</span><span class="p-channel_sidebar__name">Add channels</span></div>`;
    if (s.type === 'direct_messages') body += `<div class="p-channel_sidebar__link p-channel_sidebar__link--add-more-items" data-sact="browse-people"><span class="p-channel_sidebar__channel_icon_prefix p-channel_sidebar__channel_icon_prefix--bg">${icon('directories', 14)}</span><span class="p-channel_sidebar__name">Browse all people</span></div>`;
  }
  return `<div class="p-channel_sidebar__section${collapsed ? ' p-channel_sidebar__section--collapsed' : ''}" data-section="${esc(s.id)}">
<div class="p-channel_sidebar__section_heading" data-sact="toggle-section" data-section="${esc(s.id)}" role="treeitem" aria-expanded="${!collapsed}"><span class="p-channel_sidebar__section_heading__toggle"><span class="p-channel_sidebar__section_heading__icon">${ic}</span><span class="p-channel_sidebar__section_heading__caret">${icon('caretDown', 16)}</span></span><span class="p-channel_sidebar__section_heading__label">${esc(s.name)}</span></div>
${body}</div>`;
}

function emojiIcon(e) {
  const name = e.replace(/:/g, '');
  const url = ext.emoji?.emojiURL?.(name);
  return url ? `<img class="c-emoji" src="${esc(url)}" alt="">` : icon('folder', 16);
}

function render() {
  if (!listEl) return;
  root.querySelector('.p-ia4_home_header_menu__team_name').textContent = state.team?.name || '';
  if (filterText) {
    const matches = [...state.convs.values()].filter((c) => !c.is_archived && convName(c).toLowerCase().includes(filterText));
    matches.sort((a, b) => convName(a).localeCompare(convName(b)));
    listEl.innerHTML = matches.length ? `<div class="p-channel_sidebar__section">${matches.slice(0, 60).map((c) => rowHTML(c)).join('')}</div>` : '<div class="p-channel_sidebar__empty p-channel_sidebar__empty--filter">No matches</div>';
    return;
  }
  const route = state.current.route;
  const statics = STATIC.map((s) => `<div class="p-channel_sidebar__link${route.startsWith(s.route) ? ' p-channel_sidebar__link--selected' : ''}" data-route="${s.route}">${'<span class="p-channel_sidebar__link__icon">' + icon(s.icon, 16) + '</span>'}<span class="p-channel_sidebar__name">${esc(s.label)}</span></div>`).join('');
  const scroll = listEl.scrollTop;
  listEl.innerHTML = `<div class="p-channel_sidebar__static_list">${statics}</div><div class="p-channel_sidebar__divider"></div>${effectiveSections().map(sectionHTML).join('')}`;
  listEl.scrollTop = scroll;
}

function updateSelection() {
  if (!listEl) return;
  render();
  const sel = listEl.querySelector('.p-channel_sidebar__channel--selected');
  if (sel) {
    const r = sel.getBoundingClientRect(), lr = listEl.getBoundingClientRect();
    if (r.top < lr.top || r.bottom > lr.bottom) sel.scrollIntoView({ block: 'nearest' });
  }
}

function onClick(e) {
  const row = e.target.closest('[data-id]');
  if (row) { navigate('#/c/' + row.dataset.id); if (filterText) { filterInput.value = ''; filterText = ''; render(); } return; }
  const link = e.target.closest('[data-route]');
  if (link) { navigate(link.dataset.route); return; }
  const a = e.target.closest('[data-sact]');
  if (!a) return;
  switch (a.dataset.sact) {
    case 'toggle-section': {
      const id = a.dataset.section;
      state.prefs.collapsed[id] = !state.prefs.collapsed[id];
      if (!state.prefs.collapsed[id]) delete state.prefs.collapsed[id];
      savePrefs();
      render();
      break;
    }
    case 'team': {
      const items = [{ label: state.team?.name || 'Workspace', hint: state.team?.domain ? state.team.domain + '.slack.com' : '' }, 'sep',
        { label: 'Preferences', onClick: () => toggleThemeFromMenu() }, 'sep',
        { label: `Sign out of ${state.team?.name || 'workspace'}`, onClick: () => ext.popups?.toast?.('Sign out from the yoloslack CLI') }];
      ext.popups?.openMenu?.(a, items);
      break;
    }
    case 'settings': ext.popups?.openMenu?.(a, [{ label: 'Create a section', onClick: () => ext.popups?.toast?.('Not supported yet') }, { label: 'Collapse all sections', onClick: () => { for (const s of state.sections) state.prefs.collapsed[s.id] = true; savePrefs(); render(); } }, { label: 'Expand all sections', onClick: () => { state.prefs.collapsed = {}; savePrefs(); render(); } }]); break;
    case 'compose': if (ext.switcher?.openQuickSwitcher) ext.switcher.openQuickSwitcher(); else filterInput.focus(); break;
    case 'add-channels': navigate('#/directories'); break;
    case 'browse-people': navigate('#/directories'); break;
    default: break;
  }
}

function toggleThemeFromMenu() { emit('toggle_theme'); }

export function focusSidebarFilter() { filterInput?.focus(); }

// Resizable sidebar: drag the handle between the sidebar and the primary view.
export function mountResizer(handle) {
  const apply = (w) => document.documentElement.style.setProperty('--sidebar-w', w + 'px');
  apply(state.prefs.sidebarWidth || 232);
  handle.addEventListener('pointerdown', (e) => {
    e.preventDefault();
    handle.setPointerCapture(e.pointerId);
    const startX = e.clientX, startW = state.prefs.sidebarWidth || 232;
    document.body.classList.add('is-resizing');
    const move = (ev) => { state.prefs.sidebarWidth = clamp(startW + ev.clientX - startX, 180, 520); apply(state.prefs.sidebarWidth); };
    const up = () => { handle.removeEventListener('pointermove', move); handle.removeEventListener('pointerup', up); document.body.classList.remove('is-resizing'); savePrefs(); };
    handle.addEventListener('pointermove', move);
    handle.addEventListener('pointerup', up);
  });
  handle.addEventListener('dblclick', () => { state.prefs.sidebarWidth = 232; apply(232); savePrefs(); });
}

export { h };
