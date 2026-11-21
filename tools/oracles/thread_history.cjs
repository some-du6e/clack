// thread_history.cjs: Slack's channelHistory reducer (client-boot 6Yed)
// over om2Z's slices and dTxY's selectors, build 35b42de8b870 (2026-11-19),
// run on scripted pages: internal/store/testdata/thread_history_slack.json,
// which TestThreadHistoryLikeSlack replays through the store.
//
//   MODS=<dir with om2Z.js and 6Yed.js, beautified> node tools/oracles/thread_history.cjs \
//     > internal/store/testdata/thread_history_slack.json
//
// The modules come from the public CDN's client-boot chunk (no cookie),
// cut by module id and run through js-beautify; the lodash pieces they
// import are stubbed below (their gantry-v2-vendors ids are noted).
const fs = require('fs');
const MODS = (process.env.MODS || '/root/clack-ops/scratch/thread-load-speed/mods').replace(/\/?$/, '/');
function load(file) {
  let src = fs.readFileSync(MODS + file, 'utf8').trim();
  src = src.replace(/^"?[^"]+"?:\s*/, '').replace(/,\s*$/, '');
  return eval('(' + src + ')');
}
// lodash stand-ins (the vendors modules confirmed by id: 7RVK head, DuX+
// last, MqQN max, wKWa min, z69U isBoolean, 7+j2 isUndefined, LyRa
// sortedIndexOf, lXQd get, RRWn sortedLastIndex)
const _ = {
  head: a => a && a.length ? a[0] : undefined,
  last: a => a && a.length ? a[a.length - 1] : undefined,
  isBoolean: v => v === true || v === false,
  isUndefined: v => v === undefined,
  sortedIndexOf: (a, v) => { let lo = 0, hi = a ? a.length : 0; while (lo < hi) { const m = (lo + hi) >> 1; if (a[m] < v) lo = m + 1; else hi = m; } return a && lo < a.length && a[lo] === v ? lo : -1; },
  sortedLastIndex: (a, v) => { let lo = 0, hi = a.length; while (lo < hi) { const m = (lo + hi) >> 1; if (v < a[m]) hi = m; else lo = m + 1; } return lo; },
  get: (o, p, d) => { let v = o; for (const k of String(p).split('.')) { v = v == null ? undefined : v[k]; } return v === undefined ? d : v; },
  pickBy: (o, f) => { const r = {}; for (const k of Object.keys(o)) if (f(o[k], k)) r[k] = o[k]; return r; },
  startsWith: (s, p) => String(s).startsWith(p),
  omit: (o, k) => { const r = { ...o }; delete r[k]; return r; },
  mapValues: (o, f) => { const r = {}; for (const k of Object.keys(o)) r[k] = f(o[k], k); return r; },
  sumBy: (a, f) => a.reduce((s, x) => s + f(x), 0),
  map: (a, f) => a.map(f),
  isEmpty: v => v == null || (Array.isArray(v) ? v.length === 0 : Object.keys(v).length === 0),
  compact: a => a.filter(Boolean),
  isString: v => typeof v === 'string',
};
function req(stubs) {
  const r = id => {
    if (!(id in stubs)) throw new Error('unstubbed ' + id);
    return stubs[id];
  };
  return r;
}
function instantiate(fn, stubs) {
  const exports = {};
  const n = req(stubs);
  n.d = (t, getters, extra) => {
    for (const [k, g] of Object.entries(getters || {})) Object.defineProperty(exports, k, { get: g, enumerable: true });
    for (const [k, v] of Object.entries(extra || {})) exports[k] = v;
  };
  fn({}, exports, n);
  return exports;
}
const om2Z = instantiate(load('om2Z.js'), { '7RVK': { A: _.head }, 'DuX+': { A: _.last }, 'RRWn': { A: _.sortedLastIndex } });
// 6Yed: actions are tokens, the reducer builder records handlers
const handlers = new Map();
let tok = 0;
const createAction = () => ({ id: ++tok });
const builder = (a, b, c) => ({ on(act, f) { handlers.set(act, f); return this; } });
const sixYed = instantiate(load('6Yed.js'), {
  'z69U': { A: _.isBoolean }, '7+j2': { A: _.isUndefined }, 'LyRa': { A: _.sortedIndexOf }, 'lXQd': { A: _.get },
  'DuX+': { A: _.last }, 'TOaN': { A: _.pickBy }, 'Ifqe': { A: _.startsWith }, 'ybnO': { A: _.omit }, 'XzID': { A: _.mapValues },
  'gtmE': { A: _.sumBy }, 'ck13': { A: _.map }, 'AkOy': { A: _.isEmpty }, '7RVK': { A: _.head }, '2oHs': { A: _.compact },
  'dUDN': { A: ts => !ts || ts.includes('x') }, '8Ifm': { Ay: createAction }, '/XL8': { A: builder }, 'om2Z': om2Z,
});
const H = name => handlers.get(sixYed[name]);
const addTimestamps = H('VG'), removeTimestamp = H('Np'), resetReachedEnd = H('Jt'), truncate = H('CD');
// dTxY's selectors (getTimestampsByChannelId tp, getPotentiallyOutdatedTimestamps r7), as written there
const f = { start: undefined, end: undefined, timestamps: [], reachedStart: false, reachedEnd: false, prevReachedEnd: false };
function tp(hist, n) {
  if (!hist) return f;
  const r = hist.slices; if (!r || !r.length) return f;
  const s = r[0], o = r[r.length - 1];
  if (!n) return hist.reachedEnd ? { ...o, reachedStart: r.length === 1 && !!hist.reachedStart, reachedEnd: true } : f;
  if (n === '0000000000.000000') return hist.reachedStart ? { ...s, reachedStart: true, reachedEnd: r.length === 1 && !!hist.reachedEnd } : f;
  const l = om2Z.Z0([n]);
  for (let e = 0; e < r.length; e++) { const t = r[e]; if (om2Z.HY(t, l)) return { ...t, reachedStart: e === 0 && !!hist.reachedStart, reachedEnd: e === r.length - 1 && !!hist.reachedEnd }; }
  return hist.reachedEnd && o.end < n ? { ...o, reachedStart: r.length === 1 && !!hist.reachedStart, reachedEnd: true } :
    hist.reachedStart && _.isString(s.start) && s.start > n ? { ...s, reachedStart: true, reachedEnd: r.length === 1 && !!hist.reachedEnd } : f;
}
function r7(hist, n) {
  if (!hist) return f;
  const i = hist.slices; if (!i || !i.length) return f;
  const r = tp(hist, n);
  return hist.prevReachedEnd ? (n || r !== f ? { ...r, prevReachedEnd: r?.end === _.last(i)?.end } :
    { ..._.last(i), reachedStart: !!(i.length === 1 && hist.reachedStart), reachedEnd: !!hist.reachedEnd, prevReachedEnd: true }) : r;
}
const K = 'C1-1700000000.000100';
const ts = i => (1700000000 + i) + '.000100'; // i 0 is the root
const range = (a, b) => { const o = []; for (let i = a; i <= b; i++) o.push(i); return o; };
const T = a => a.map(ts);
const B = b => { const o = {}; for (const [k, v] of Object.entries(b || {})) o[k] = (k === 'oldest' || k === 'latest') ? ts(v) : v; return o; };
const cases = {
  root_open_then_pages: [
    ['add', range(0, 27), { oldest: 0, hasMore: true }],
    ['add', [0], { latest: 0, hasMore: false }],
    ['add', range(27, 54), { oldest: 27, hasMore: true }],
    ['add', range(54, 70), { oldest: 54, hasMore: false }],
    ['live', [71]],
    ['live', [72]],
  ],
  around_then_older_then_start: [
    ['add', range(500, 554), { oldest: 500, latest: 554, hasMore: { hasMoreStart: true, hasMoreEnd: true } }],
    ['live', [900]],
    ['add', range(473, 500), { latest: 500, hasMore: true }],
    ['add', range(1, 473).slice(-28), { latest: 473, hasMore: true }],
    ['add', [0, ...range(1, 445).slice(0, 27)], { latest: 446, hasMore: false }],
  ],
  two_jumps_bridged: [
    ['add', range(100, 154), { oldest: 100, latest: 154, hasMore: { hasMoreStart: true, hasMoreEnd: true } }],
    ['add', range(300, 354), { oldest: 300, latest: 354, hasMore: { hasMoreStart: true, hasMoreEnd: true } }],
    ['add', range(154, 181), { oldest: 154, hasMore: true }],
    ['add', range(181, 300), { oldest: 181, hasMore: true }],
    ['add', range(354, 381), { oldest: 354, hasMore: false }],
  ],
  perf_quality_newest: [
    ['add', range(980, 1000), { latest: 1000, hasMore: { hasMoreStart: true, hasMoreEnd: false } }],
    ['reset'],
    ['add', range(1000, 1003), { oldest: 1000, hasMore: false }],
  ],
  removes: [
    ['add', range(0, 10), { oldest: 0, hasMore: false }],
    ['add', [0], { latest: 0, hasMore: false }],
    ['add', range(50, 60), { oldest: 50, latest: 60, hasMore: { hasMoreStart: true, hasMoreEnd: true } }],
    ['remove', 5],
    ['remove', 55],
    ['add', range(60, 61), { oldest: 60, hasMore: false }],
    ['live', [62]],
    ['remove', 62],
  ],
  truncate: [
    ['add', range(0, 600), { oldest: 0, hasMore: true }],
    ['add', [0], { latest: 0, hasMore: false }],
    ['add', range(2000, 2600), { oldest: 2000, latest: 2600, hasMore: { hasMoreStart: true, hasMoreEnd: true } }],
    ['truncate'],
  ],
};
const selectAt = [undefined, '0000000000.000000', ts(0), ts(5), ts(27), ts(60), ts(150), ts(320), ts(473), ts(520), ts(990), ts(1001), ts(5000)];
const selectKeys = selectAt.map(n => n === undefined ? '' : n);
const out = [];
for (const [name, ops] of Object.entries(cases)) {
  let st = {};
  const states = [];
  for (const op of ops) {
    const [k, a, b] = op;
    if (k === 'add') st = addTimestamps(st, { channelId: K, timestamps: T(a), ...B(b) });
    else if (k === 'live') st = addTimestamps(st, { channelId: K, timestamps: T(a) });
    else if (k === 'remove') st = removeTimestamp(st, { channelId: K, timestamp: ts(a) });
    else if (k === 'reset') st = resetReachedEnd(st, { channelId: K });
    else if (k === 'truncate') st = truncate(st, { channelId: K });
    const h = st[K];
    const sel = [];
    for (const n of selectAt) { const v = r7(h, n); sel.push(v === f ? null : [v.start || '', v.end || '', v.timestamps.length, v.timestamps[0] || '', _.last(v.timestamps) || '', !!v.reachedStart, !!v.reachedEnd, !!v.prevReachedEnd]); }
    states.push({ slices: (h?.slices || []).map(s => [s.start || '', s.end || '', s.timestamps.length, s.timestamps[0] || '', _.last(s.timestamps) || '']),
      rs: h?.reachedStart === undefined ? 'unset' : h.reachedStart === null ? 'null' : String(h.reachedStart),
      re: h?.reachedEnd === undefined ? 'unset' : h.reachedEnd === null ? 'null' : String(h.reachedEnd),
      pre: !!h?.prevReachedEnd, sel });
  }
  out.push({ name, ops, states });
}
process.stdout.write(JSON.stringify({about: 'Slack client-boot 6Yed channelHistory reducer over om2Z slices and dTxY selectors (build 35b42de8b870, 2026-11-19), run by oracle.cjs: each case is ops on one thread key (reply index i is ts 1700000000+i.000100), with the history after each op: slices [start, end, count, first, last], reachedStart/reachedEnd (true, false, null, unset), prevReachedEnd, and getPotentiallyOutdatedTimestamps at each of select (null: the empty slice) as [start, end, count, first, last, reachedStart, reachedEnd, prevReachedEnd].', select: selectKeys, cases: out}));
