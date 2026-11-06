// state.js: client-side state and the event bus (ui-core).
import { api } from './api.js';
import { setUserLookup } from './util.js';

const PREFS_KEY = 'yoloslack.prefs';
function loadPrefs() {
  try { return JSON.parse(localStorage.getItem(PREFS_KEY)) || {}; } catch { return {}; }
}

export const state = {
  self: null,
  team: null,
  users: new Map(),
  convs: new Map(),
  counts: new Map(),
  sections: [],
  current: { route: '', channel: null, thread: null },
  prefs: Object.assign({ theme: 'light', sidebarWidth: 232, collapsed: {}, showFormatting: true }, loadPrefs()),
  connected: false,
  // message data, memory only (scraping policy): channel -> {messages:[View], hasMore, lastRead, fetchedAt}
  history: new Map(),
  // "channel:ts" -> {messages:[View] parent first, fetchedAt}
  threads: new Map(),
  // channel -> Map(userId -> {name, until, thread_ts})
  typing: new Map(),
  drafts: new Map(),
  enterpriseId: '',
};

export function savePrefs() {
  try { localStorage.setItem(PREFS_KEY, JSON.stringify(state.prefs)); } catch { /* quota */ }
}

const handlers = new Map();
export function on(event, fn) {
  if (!handlers.has(event)) handlers.set(event, new Set());
  handlers.get(event).add(fn);
  return () => off(event, fn);
}
export function off(event, fn) { handlers.get(event)?.delete(fn); }
export function emit(event, data) {
  const hs = handlers.get(event);
  if (hs) for (const fn of [...hs]) {
    try { fn(data); } catch (e) { console.error(`handler for ${event}`, e); }
  }
  const all = handlers.get('*');
  if (all) for (const fn of [...all]) { try { fn(event, data); } catch (e) { console.error(e); } }
}

export function user(id) { return state.users.get(id); }
setUserLookup(user);

export function putUsers(map) {
  if (!map) return;
  let n = 0;
  for (const [id, u] of Object.entries(map)) {
    if (!u) continue;
    state.users.set(id, u);
    n++;
  }
  if (n) emit('users', map);
}

// ensureUsers batches lookups of unknown ids into one /api/users request per tick.
let pendingIds = new Set();
let pendingP = null;
export function ensureUsers(ids) {
  const missing = (ids || []).filter((id) => id && !state.users.has(id));
  if (!missing.length) return Promise.resolve();
  missing.forEach((id) => pendingIds.add(id));
  if (!pendingP) {
    pendingP = new Promise((resolve) => setTimeout(async () => {
      const batch = [...pendingIds];
      pendingIds = new Set();
      pendingP = null;
      try {
        for (let i = 0; i < batch.length; i += 100) {
          const r = await api.get('users', { ids: batch.slice(i, i + 100).join(',') });
          putUsers(r.users);
        }
      } catch (e) { console.warn('ensureUsers', e); }
      resolve();
    }, 0));
  }
  return pendingP;
}

export function conv(id) { return state.convs.get(id); }

// Display name for a conversation the way the sidebar shows it.
export function convName(c) {
  if (!c) return '';
  if (c.kind === 'im') {
    const u = user(c.user);
    return u ? (u.name || u.real_name || u.handle) : (c.name || c.user);
  }
  if (c.kind === 'mpim') {
    const ids = (c.members || []).filter((id) => id !== state.self?.id);
    if (ids.length) return ids.map((id) => { const u = user(id); return u ? (u.name || u.real_name) : id; }).join(', ');
    return (c.name || '').replace(/^mpdm-/, '').replace(/-\d+$/, '').split('--').join(', ');
  }
  return c.name || c.id;
}

export function userName(id) {
  const u = user(id);
  return u ? (u.name || u.real_name || u.handle || id) : id;
}

export function isUnread(id) {
  const c = state.counts.get(id);
  return !!(c && (c.unread || c.mentions > 0));
}
