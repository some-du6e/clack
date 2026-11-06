// rail.js: the 70px tab rail on the left (ui-core).
import { esc, avatarFor, throttleRAF } from './util.js';
import { icon } from './icons.js';
import { state, on, emit } from './state.js';
import { navigate, ext } from './app.js';
import { isAppDM } from './sidebar.js';

let root;

const TABS = [
  { id: 'home', label: 'Home', icon: 'home', active: 'homeFilled', route: '#/home' },
  { id: 'dms', label: 'DMs', icon: 'dms', active: 'dmsFilled', route: '#/dms' },
  { id: 'activity', label: 'Activity', icon: 'activity', active: 'activityFilled', route: '#/activity' },
  { id: 'files', label: 'Files', icon: 'files', active: 'filesFilled', route: '#/files' },
  { id: 'later', label: 'To-dos', icon: 'todos', active: 'todosFilled', route: '#/later' },
  { id: 'more', label: 'Agents & tools', icon: 'more', active: 'more', route: null },
];

export function activeTab(route) {
  if (route.startsWith('#/dms')) return 'dms';
  if (route.startsWith('#/activity')) return 'activity';
  if (route.startsWith('#/files')) return 'files';
  if (route.startsWith('#/later')) return 'later';
  return 'home';
}

function counts() {
  let dms = 0, mentions = 0, homeUnread = false;
  for (const [id, c] of state.counts) {
    const cv = state.convs.get(id);
    if (!cv) continue;
    if (cv.kind === 'im' || cv.kind === 'mpim') { if (isAppDM(cv)) continue; if (c.unread || c.mentions) dms += Math.max(1, c.mentions || 0); }
    else { mentions += c.mentions || 0; if (c.unread) homeUnread = true; }
  }
  return { dms, mentions, homeUnread };
}

export function mountRail(el) {
  root = el;
  render();
  const rr = throttleRAF(render);
  for (const ev of ['counts', 'counts_local', 'marked', 'route', 'user', 'presence', 'boot', 'message', 'theme']) on(ev, rr);
  root.addEventListener('click', (e) => {
    const b = e.target.closest('[data-tab]');
    if (b) {
      const t = TABS.find((x) => x.id === b.dataset.tab);
      if (t.id === 'home') { navigate(state.lastHomeRoute || (state.current.channel ? '#/c/' + state.current.channel : '#/home')); return; }
      if (t.route) navigate(t.route);
      else ext.popups?.openMenu?.(b, [
        { label: 'Huddles', onClick: () => navigate('#/huddles') },
        { label: 'Directories', onClick: () => navigate('#/directories') },
        { label: 'Threads', onClick: () => navigate('#/threads') },
        { label: 'Drafts & sent', onClick: () => navigate('#/drafts') },
      ], { placement: 'right' });
      return;
    }
    const a = e.target.closest('[data-ract]');
    if (!a) return;
    if (a.dataset.ract === 'theme') emit('toggle_theme');
    if (a.dataset.ract === 'create') ext.popups?.openMenu?.(a, [
      { label: 'Message', onClick: () => ext.switcher?.openQuickSwitcher?.() },
      { label: 'Channel', onClick: () => navigate('#/directories') },
    ], { placement: 'right' });
    if (a.dataset.ract === 'me') ext.profile?.openProfileCard?.(a, state.self.id);
    if (a.dataset.ract === 'team') navigate('#/home');
  });
}

function render() {
  if (!root || !state.self) return;
  const at = activeTab(state.current.route || '');
  const { dms, mentions, homeUnread } = counts();
  const team = state.team || {};
  const teamIcon = team.icon ? `<span class="p-tab_rail__team_icon" style="background-image:url('${esc(team.icon)}')"></span>` : `<span class="p-tab_rail__team_icon p-tab_rail__team_icon--text">${esc((team.name || 'S').slice(0, 1))}</span>`;
  const tabs = TABS.map((t) => {
    const active = t.id === at;
    let badge = '';
    if (t.id === 'dms' && dms) badge = `<span class="p-tab_rail__badge">${dms}</span>`;
    if (t.id === 'activity' && mentions) badge = `<span class="p-tab_rail__badge">${mentions}</span>`;
    if (t.id === 'home' && homeUnread && at !== 'home') badge = '<span class="p-tab_rail__dot"></span>';
    return `<div class="p-tab_rail__tab_container"><button class="p-tab_rail__button${active ? ' p-tab_rail__button--active' : ''}" data-tab="${t.id}" aria-label="${esc(t.label)}"><span class="p-tab_rail__button__icon">${icon(active ? t.active : t.icon, 20)}</span>${badge}</button><span class="p-tab_rail__button__label">${esc(t.label)}</span></div>`;
  }).join('');
  const me = state.users.get(state.self.id) || state.self;
  const dark = document.documentElement.classList.contains('dark');
  root.innerHTML = `<button class="p-tab_rail__team" data-ract="team" aria-label="${esc(team.name || '')}" data-tip="${esc(team.name || '')}">${teamIcon}</button>
<div class="p-tab_rail__tabs">${tabs}</div>
<div class="p-tab_rail__spacer"></div>
<div class="p-control_strip">
  <button class="p-control_strip__circle_button p-control_strip__create_button" data-ract="create" aria-label="Create new" data-tip="Create new">${icon('plus', 20)}</button>
  <button class="p-control_strip__circle_button" data-ract="theme" aria-label="Toggle dark mode" data-tip="${dark ? 'Switch to light mode' : 'Switch to dark mode'}">${icon(dark ? 'sun' : 'moon', 20)}</button>
  <button class="p-control_strip__avatar" data-ract="me" aria-label="Your profile"><img src="${esc(avatarFor(me))}" alt=""><span class="c-presence${me.presence === 'active' || !me.presence ? ' c-presence--active' : ''}"></span></button>
</div>`;
}
