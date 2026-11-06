// util.js: small shared helpers (ui-core). See docs/ARCHITECTURE.md for the contract.

const ESC = { '&': '&amp;', '<': '&lt;', '>': '&gt;', '"': '&quot;', "'": '&#39;' };
export function esc(s) {
  if (s == null) return '';
  return String(s).replace(/[&<>"']/g, (c) => ESC[c]);
}

const tpl = document.createElement('template');
// h parses an HTML string into one Element (or a DocumentFragment when there are several roots).
export function h(html) {
  tpl.innerHTML = String(html).trim();
  const c = tpl.content;
  if (c.childNodes.length === 1) return c.firstChild;
  const f = document.createDocumentFragment();
  f.append(...c.childNodes);
  return f;
}

export function tsDate(ts) {
  return new Date(parseFloat(ts) * 1000);
}

const timeFmt = new Intl.DateTimeFormat(undefined, { hour: 'numeric', minute: '2-digit' });
// "11:25 AM"
export function fmtTime(ts) {
  return timeFmt.format(typeof ts === 'object' ? ts : tsDate(ts));
}
// "11:25" (compact gutter timestamp: no meridiem)
export function fmtTimeShort(ts) {
  return fmtTime(ts).replace(/\s?[AP]\.?M\.?$/i, '');
}

function ordinal(n) {
  const s = ['th', 'st', 'nd', 'rd'], v = n % 100;
  return n + (s[(v - 20) % 10] || s[v] || s[0]);
}
const MONTHS = ['January', 'February', 'March', 'April', 'May', 'June', 'July', 'August', 'September', 'October', 'November', 'December'];
const DAYS = ['Sunday', 'Monday', 'Tuesday', 'Wednesday', 'Thursday', 'Friday', 'Saturday'];

export function dayKey(d) {
  if (typeof d !== 'object') d = tsDate(d);
  return d.getFullYear() * 10000 + (d.getMonth() + 1) * 100 + d.getDate();
}

// "Today", "Yesterday", "Monday, November 3rd" (and ", 2025" for other years)
export function fmtDay(ts) {
  const d = typeof ts === 'object' ? ts : tsDate(ts);
  const now = new Date();
  const k = dayKey(d), today = dayKey(now);
  if (k === today) return 'Today';
  const y = new Date(now); y.setDate(now.getDate() - 1);
  if (k === dayKey(y)) return 'Yesterday';
  let s = `${DAYS[d.getDay()]}, ${MONTHS[d.getMonth()]} ${ordinal(d.getDate())}`;
  if (d.getFullYear() !== now.getFullYear()) s += `, ${d.getFullYear()}`;
  return s;
}

// "November 3rd at 11:25 AM" style used in tooltips.
export function fmtFull(ts) {
  const d = tsDate(ts);
  const day = fmtDay(d);
  return `${/^(Today|Yesterday)$/.test(day) ? day : `${MONTHS[d.getMonth()]} ${ordinal(d.getDate())}${d.getFullYear() !== new Date().getFullYear() ? ', ' + d.getFullYear() : ''}`} at ${fmtTime(d)}`;
}

// "just now", "5 minutes ago", "2 hours ago", "3 days ago"
export function relTime(ts) {
  const s = Math.max(0, Date.now() / 1000 - parseFloat(ts));
  if (s < 60) return 'just now';
  const m = Math.floor(s / 60);
  if (m < 60) return `${m} minute${m === 1 ? '' : 's'} ago`;
  const h = Math.floor(m / 60);
  if (h < 24) return `${h} hour${h === 1 ? '' : 's'} ago`;
  const d = Math.floor(h / 24);
  if (d < 30) return `${d} day${d === 1 ? '' : 's'} ago`;
  return fmtDay(ts);
}

export function plural(n, one, many) {
  return `${n} ${n === 1 ? one : (many || one + 's')}`;
}

const DEFAULT_AVATAR = 'https://a.slack-edge.com/80588/img/avatars/ava_0001-72.png';
// avatarFor accepts a message View or a UserView.
export function avatarFor(v) {
  if (!v) return DEFAULT_AVATAR;
  if (v.avatar) return v.avatar;
  if (v.user && typeof v.user === 'string' && userLookup) {
    const u = userLookup(v.user);
    if (u?.avatar) return u.avatar;
  }
  return DEFAULT_AVATAR;
}
let userLookup = null;
export function setUserLookup(fn) { userLookup = fn; }

export function debounce(fn, ms) {
  let t;
  const d = (...a) => { clearTimeout(t); t = setTimeout(() => fn(...a), ms); };
  d.cancel = () => clearTimeout(t);
  return d;
}

export function throttleRAF(fn) {
  let queued = false;
  return () => {
    if (queued) return;
    queued = true;
    requestAnimationFrame(() => { queued = false; fn(); });
  };
}

export function tsCmp(a, b) {
  const x = parseFloat(a), y = parseFloat(b);
  return x < y ? -1 : x > y ? 1 : 0;
}

// Standard emoji image on Slack's CDN, by unified codepoint image name.
export function cdnEmoji(image, size = 'medium') {
  return `https://a.slack-edge.com/production-standard-emoji-assets/16.0/apple-${size}/${image}.png`;
}

export function isMac() { return /Mac|iPhone|iPad/.test(navigator.platform); }

export function clamp(v, lo, hi) { return Math.max(lo, Math.min(hi, v)); }
