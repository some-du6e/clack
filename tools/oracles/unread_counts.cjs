// unread_counts.cjs: what Slack makes of each case's client.counts answer
// (internal/core/testdata/parity/unread-counts.json), from Slack's own code
// run in node over the web client's public CDN chunks (build 66e72b57ade1):
//
//   node tools/oracles/unread_counts.cjs SNAPSHOT.json > out.json
//
// SLACK_CHUNKS and JSDOM as for message_actions.cjs. tools/parity.py
// --refresh unread-counts runs it.
//
// The answer goes through fetchClientCounts' transformClientCountsOutput and
// applyCountsToChannels (client-boot mtwS, "Apply
// counts to channel objects and update stores"), which converts it,
// dispatches the raw counts and refreshFilteredUnreadChannelCounts (9dui):
// every module runs its own code (slackrt realAll), the store is built
// through Slack's reducers and actions, and nothing on the path is
// replaced. Then, for each conversation of the answer:
//   badge: the sidebar's badge count (EmXR TJ getBadgeCount);
//   unread: the filtered counts' unreadCnt > 0 (what the sidebar and the
//     totals count; a muted or left conversation isn't in them);
// as {rows: {id: {badge, unread}}}.
'use strict';
const fs = require('fs');
const { makeRuntime, installBrowser } = require('./slackrt.cjs');

const CHUNKS = process.env.SLACK_CHUNKS || '/root/clack-ops/scratch/parity-actions-1/cdn';
const JSDOM_DIR = process.env.JSDOM || '/root/clack-ops/scratch/canvas-edit/npm';
installBrowser(JSDOM_DIR);
const err = console.error.bind(console);
console.log = console.info = console.debug = console.warn = () => {}; // modules log at load

const rt = makeRuntime({ dir: CHUNKS, realAll: true });
const applyCounts = rt.req('mtwS');
const EmXR = rt.req('EmXR');
const appBadge = rt.req('9OWn').ER;
const totals = rt.req('vuTZ');
const inbox = rt.req('SWeb').Lh;
const userPref = rt.req('4iNl').ty;
const reducers = rt.req('4GYe').i().getReducerMapForStoreType('team');
const creators = rt.req('8Ifm').D9();
const stores = rt.req('W+Gl');

function action(desc) {
  const hits = Object.keys(creators).filter(t => t.slice(t.indexOf(']') + 1).trim() === desc);
  if (hits.length !== 1) throw new Error(`unread_counts: ${hits.length} action creators named ${JSON.stringify(desc)}`);
  return creators[hits[0]];
}

function reduce(state, act) {
  const next = {};
  for (const [k, r] of Object.entries(reducers)) next[k] = r(state ? state[k] : undefined, act);
  return next;
}

function merge(base, inp) {
  const r = { ...base, ...inp };
  for (const k of ['experiments', 'prefs']) r[k] = { ...(base[k] || {}), ...(inp[k] || {}) };
  r.users = [...(base.users || []), ...(inp.users || [])];
  return r;
}

// mtwS's export o: applyCountsToChannels
function applyThunk() {
  const v = applyCounts.o;
  if (typeof v !== 'function' || !v.meta || v.meta.name !== 'applyCountsToChannels') throw new Error('unread_counts: no applyCountsToChannels in mtwS');
  return v;
}

async function run(c) {
  let state = reduce(undefined, { type: '@@INIT' });
  const getState = () => state;
  const dispatch = a => {
    if (typeof a === 'function') return a(dispatch, getState, ctx => ctx && ctx.defaultValue);
    if (a && a.type) state = reduce(state, a);
    return a;
  };
  const store = { getState, dispatch, subscribe: () => () => {} };
  const team = c.team.id;
  const ent = c.team.enterprise_id;
  stores.Sf(team, store);
  if (ent) stores.Sf(ent, store);
  dispatch(action('Load boot data')({ app: 'client', user_id: c.self.id, team_id: team, ...(ent ? { enterprise_id: ent } : {}) }));
  dispatch(action('Set the current user team (workspace or enterprise) id')({ id: team }));
  if (ent) dispatch(action('Set the current user enterprise id (only for enterprise)')({ id: ent }));
  dispatch(action('Bulk add experiment assignments to redux')(Object.fromEntries(Object.entries(c.experiments).map(([k, g]) => [k, { group: g }]))));
  dispatch(action('Initialize user preferences')({ ...c.prefs }));
  dispatch(action('Bulk add members [proto]')({ objects: [c.self, ...c.users].map(u => JSON.parse(JSON.stringify(u))) }));
  dispatch(action('Bulk add channels [proto]')({ objects: JSON.parse(JSON.stringify(c.convs)) }));
  for (const id of c.muted || []) dispatch(action('Set a channel as muted or unmuted')({ id, muted: true }));
  // fetchClientCounts' own transformClientCountsOutput (OKlm b): DM entries
  // carry is_im, group DMs' is_mpim
  const counts = rt.req('OKlm').b(JSON.parse(JSON.stringify(c.counts || {})));
  const entries = [...(counts.channels || []), ...(counts.mpims || []), ...(counts.ims || [])];
  await dispatch(applyThunk()({ counts: JSON.parse(JSON.stringify(entries)), skipMissingChannelFetch: true }));
  const filtered = state.filteredUnreadChannelCounts || {};
  const rows = {};
  for (const e of entries) {
    const ch = EmXR.OL(state, e.id, false);
    const f = filtered[e.id];
    rows[e.id] = {
      badge: ch ? EmXR.TJ(state, ch) : 0,
      unread: !!(f && f.unreadCnt > 0),
    };
  }
  // the totals (unreadCounts' totalUnreads, the app badge, the dock's
  // triple) come next, with the check that compares them
  void appBadge; void totals; void inbox; void userPref;
  if (process.env.UNREAD_DEBUG) {
    const raw = rt.req('oyvW').w0;
    for (const e of entries) {
      const o = EmXR.OL(state, e.id, false);
      rows[e.id].debug = { raw: raw(state, e.id), filtered: filtered[e.id] || null, conv: o ? { is_im: o.is_im, is_channel: o.is_channel, keys: Object.keys(o).length } : null, inStore: !!(state.channels && state.channels[e.id]) };
    }
  }
  return { rows };
}

async function main() {
  const snap = JSON.parse(fs.readFileSync(process.argv[2], 'utf8'));
  const base = snap.base || {};
  const res = {};
  for (const k of snap.cases) {
    const before = new Set(rt.stubbed);
    res[k.name] = await run(merge(base, k.in));
    const touched = [...rt.stubbed].filter(x => !before.has(x));
    if (touched.length) throw new Error(`unread_counts: ${k.name}: stubbed exports read: ${touched.join(' ')}`);
  }
  // synchronous: process.exit drops what a pipe hasn't taken yet
  fs.writeSync(1, JSON.stringify(res) + '\n');
}

if (require.main === module) {
  main().then(() => process.exit(0), e => {
    err(e && e.stack || String(e));
    process.exit(1);
  });
}

module.exports = { rt, run, merge };
