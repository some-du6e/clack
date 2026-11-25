// message_actions.cjs: Slack's message actions for each case of
// internal/ui/popups/testdata/parity/message-actions.json, from Slack's own
// selectors and components, run in node over the web client's public CDN
// chunks (build 66e72b57ade1, captured 2026-11-24):
//
//   node tools/oracles/message_actions.cjs CASES.json > out.json
//
// SLACK_CHUNKS names a directory holding the gantry chunks (client-boot,
// gantry-v2-async-client-boot-shared/-render/-vendors/-deferred,
// gantry-v2-vendors; fetch them from https://a.slack-edge.com/bv1-13-br/
// with `curl --compressed`, no cookie), JSDOM a directory holding
// node_modules/jsdom (24.1.3). tools/parity.py --refresh message-actions
// runs this and rewrites the snapshot.
//
// Every module runs its own code (slackrt realAll): the whole import graph
// of the toolbar and the menus, 8,000-odd modules. Nothing Slack decides
// is stubbed. Three things are replaced:
//   - qqaQ.N, Slack's connect(): it keeps each mapStateToProps, so the
//     oracle calls them as React-Redux would.
//   - JuvA and RwAt export internals (the ⋮ menu's builder `ap`, the
//     toolbar's classes): text added to their export lists, nothing else.
//   - the store: each case's conversation, message, people, prefs and
//     experiments go in through Slack's own reducers (the reducer
//     manager's team map), Slack's own action creators, found by their
//     descriptions ("Add messages", "Bulk add members [proto]"...), and
//     Slack's Upsert channels thunk for the conversation.
//
// For each case it prints:
//   toolbar: null when actions_MessageActions.shouldRenderActions is
//     false, else the buttons MessageActions.renderMessageActions draws, in
//     order: "quick_reactions" when the quick reactions block is there,
//     each button's action id, "more" when MessageActionButtonEllipsis (⋮)
//     renders.
//   menu: the ⋮ menu (the ellipsis' MessageActionsOverflowMenu, then
//     MessageActionsMenu's actions), [] when there's no ⋮: its groups, each
//     action "id: name", a submenu's actions after " > ".
//   context: the right-click menu, MessageActionsContextMenu (DtN2) as the
//     lists render it: null when it opens none, else its groups.
'use strict';
const fs = require('fs');
const path = require('path');
const { makeRuntime, installBrowser } = require('./slackrt.cjs');

const SCRATCH = '/root/clack-ops/scratch/parity-actions-1';
const CHUNKS = process.env.SLACK_CHUNKS || SCRATCH + '/cdn';
const JSDOM_DIR = process.env.JSDOM || '/root/clack-ops/scratch/canvas-edit/npm';
const DEBUG = !!process.env.DEBUG;

installBrowser(JSDOM_DIR);
const quiet = () => {};
const out = console.log.bind(console);
const err = console.error.bind(console);
console.log = console.info = console.debug = console.warn = quiet; // modules log at load

const connected = [];
let hookState = null;
function withState(state, fn) {
  hookState = state;
  try {
    return fn();
  } finally {
    hookState = null;
  }
}
const rt = makeRuntime({
  dir: CHUNKS,
  realAll: true,
  patch: {
    JuvA: [['a.d(t,{A:()=>nb})', 'a.d(t,{A:()=>nb,__ap:()=>ap})']],
    RwAt: [['a.d(t,{A:()=>aZ})', 'a.d(t,{A:()=>aZ,__MessageActions:()=>MessageActions,__actions_MessageActions:()=>actions_MessageActions,__connectedActions:()=>aU,__connectedEllipsis:()=>eW,__Ellipsis:()=>MessageActionButtonEllipsis,__OverflowMenu:()=>MessageActionsOverflowMenu})']],
  },
  partial: {
    ...(globalThis.__parityPartial || {}), // a debugging hook
    // react-redux's useSelector, for the function components the oracle
    // calls (MessageActionsContextMenu, MessageActionsOverflowMenu): their
    // selectors read the case's state
    WjYQ: { d4: (req, orig) => (sel, eq) => hookState ? sel(hookState) : orig(sel, eq) },
    qqaQ: {
      N: () => (mapState, mapDispatch) => Inner => {
        const c = function ConnectedStandIn() {};
        c.mapState = mapState;
        c.Inner = Inner;
        connected.push(c);
        return c;
      },
    },
  },
});

const JuvA = rt.req('JuvA');
const RwAt = rt.req('RwAt');
const uAhD = rt.req('uAhD').D; // action ids
const Zhyr = rt.req('Zhyr').Y; // message container types
const bCH2 = rt.req('bCH2').x; // menu usage locations
const reducers = rt.req('4GYe').i().getReducerMapForStoreType('team');
const creators = rt.req('8Ifm').D9();

function action(desc) {
  const hits = Object.keys(creators).filter(t => t.slice(t.indexOf(']') + 1).trim() === desc);
  if (hits.length !== 1) throw new Error(`message_actions: ${hits.length} action creators named ${JSON.stringify(desc)}`);
  return creators[hits[0]];
}

function reduce(state, act) {
  const next = {};
  for (const [k, r] of Object.entries(reducers)) next[k] = r(state ? state[k] : undefined, act);
  return next;
}

// --- a case's store, through Slack's reducers and actions
//
// The snapshot's base and the case's `in` (merged over it):
//   self, users: users.info objects (self is the viewer)
//   team: {id, domain, enterprise_id}
//   conv: a conversations.info object; msg: the message as history has
//     it (its channel is conv.id; a saved one carries message.saved,
//     which also goes into the saved store as saved.list's item); root: a
//     reply's thread root, if any
//   where: "channel", "thread" or "threads" (the container)
//   subscribed: the viewer follows the thread (threadSub), default false
//   experiments: {name: group}; prefs: user prefs; team_prefs;
//     feature_access: {name: enabled}
//     (the featureAccess store); permissions: {team: {name: allowed},
//     channel: {name: allowed}} (permissions.info's answers for the
//     viewer, team-wide and in conv); reporting: client.extras'
//     is_content_reporting_enabled
const WHERE = { channel: Zhyr.MESSAGE_PANE, thread: Zhyr.THREAD, threads: Zhyr.THREADS_VIEW };
const threadSubId = rt.req('7Kqu').Iu;
const stores = rt.req('W+Gl');
const savedItem = rt.req('GDwF').x;
const upsertChannels = rt.req('4XO+').U;

function merge(base, inp) {
  const r = { ...base, ...inp };
  for (const k of ['experiments', 'prefs', 'team_prefs', 'feature_access']) r[k] = { ...(base[k] || {}), ...(inp[k] || {}) };
  r.users = [...(base.users || []), ...(inp.users || [])];
  r.permissions = { team: { ...(base.permissions?.team || {}), ...(inp.permissions?.team || {}) }, channel: { ...(base.permissions?.channel || {}), ...(inp.permissions?.channel || {}) } };
  return r;
}

function storeFor(c) {
  let s = reduce(undefined, { type: '@@INIT' });
  const store = {
    getState: () => s,
    // a selector's fetch for something unknown is a thunk: nothing here
    // answers it, and the case's state is all there is
    dispatch: a => { if (typeof a === 'function' || !a || !a.type) return undefined; s = reduce(s, a); return a; },
    subscribe: () => () => {},
  };
  const team = c.team.id;
  stores.Sf(team, store);
  if (c.team.enterprise_id) stores.Sf(c.team.enterprise_id, store);
  const d = a => store.dispatch(a);
  // the web client's boot data says it's the client app (gdQ+ TZ: pinning
  // needs it)
  d(action('Load boot data')({ app: 'client', user_id: c.self.id, team_id: team, ...(c.team.enterprise_id ? { enterprise_id: c.team.enterprise_id } : {}), isContentReportingEnabled: !!c.reporting }));
  d(action('Set the current user team (workspace or enterprise) id')({ id: team }));
  if (c.team.enterprise_id) d(action('Set the current user enterprise id (only for enterprise)')({ id: c.team.enterprise_id }));
  d(action('Bulk add experiment assignments to redux')(Object.fromEntries(Object.entries(c.experiments).map(([k, g]) => [k, { group: g }]))));
  d(action('Initialize team preferences')({ teamId: team, teamPrefs: { ...c.team_prefs } }));
  d(action('Initialize user preferences')({ ...c.prefs }));
  d(action('Update feature access store')(Object.fromEntries(Object.entries(c.feature_access).map(([k, on]) => [k, { is_enabled: !!on }]))));
  // permissions.info's answers for the viewer: the team's and the
  // conversation's (the only ones the toolbar and menus ask for)
  const perms = o => Object.entries(o || {}).map(([id, isAllowed]) => ({ id, isAllowed: !!isAllowed }));
  d(action('Sets team permission objects')({ memberId: c.self.id, teamId: team, permissions: perms(c.permissions.team) }));
  d(action('Sets channel permission objects')({ memberId: c.self.id, channelId: c.conv.id, permissions: perms(c.permissions.channel), timestamp: 1700000000 }));
  d(action('Bulk add members [proto]')({ objects: [c.self, ...c.users].map(u => JSON.parse(JSON.stringify(u))) }));
  // the conversation as Slack's client takes one in: Upsert channels
  // (4XO+), whose processChannel (340b) also keeps its read-only and
  // thread-only state (properties.posting_restricted_to,
  // threads_restricted_to) before it bulk-adds the channel
  const upserted = upsertChannels({ channels: [JSON.parse(JSON.stringify(c.conv))], skipRecentlyLeftGuard: true })(d, store.getState, () => ({}));
  if (!Array.isArray(upserted) || upserted.length !== 1) throw new Error('message_actions: Upsert channels gave ' + JSON.stringify(upserted));
  const msgs = [c.root, c.msg].filter(Boolean).map(m => ({ ...JSON.parse(JSON.stringify(m)), channel: c.conv.id }));
  d(action('Add messages')(msgs));
  // a saved message: history carries message.saved, and saved.list the same
  // item, which Slack's saved store keeps (GDwF savedApiToSavedItem)
  for (const m of msgs) {
    if (m.saved) d(action('Add saved item')(savedItem({ ...m.saved, item_type: 'message', item_id: c.conv.id, ts: m.ts })));
  }
  // the thread's subscription, under the root's key and the message's own
  // (the channel list's right-click keys a broadcast reply's follow row on
  // its own ts, whose fetch answers the thread's state)
  const rootTs = c.msg.thread_ts || c.msg.ts;
  const subs = [...new Set([rootTs, c.msg.ts])].map(ts => ({ id: threadSubId(c.conv.id, ts), subscribed: !!c.subscribed }));
  d(action('Bulk add threadSub [proto]')({ objects: subs }));
  return store;
}

// --- Slack's answers

function label(a) {
  return a.id + ': ' + a.name + (a.submenuActions && a.submenuActions.length ? ' > ' + a.submenuActions.map(x => x.id + ': ' + x.name).join(', ') : '');
}

function groups(gs) {
  return gs.filter(g => g && g.length).map(g => g.map(label));
}

// toolbar: what actions_MessageActions and MessageActions draw for a
// hovered message: null when shouldRenderActions says no toolbar.
function toolbar(state, msg, where) {
  const aMA = RwAt.__actions_MessageActions;
  const outer = connected.find(c => c.Inner === aMA);
  // the props jc/I's channel Message (and the thread rows) hand it,
  // hovered, and its own mapState's
  let own = { ...aMA.defaultProps, msg, messageContainerType: where, position: 'default', isLast: false, hover: true, viewContext: where, windowId: 'main' };
  own = { ...own, ...outer.mapState(state, own) };
  const el = aMA.prototype.renderActions.call({ props: own, shouldRenderActions: aMA.prototype.shouldRenderActions });
  if (!el) return null;
  let props = { ...el.props };
  props = { ...props, ...el.type.mapState(state, props), hasKeyboardFocus: false };
  const inst = new RwAt.__MessageActions(props);
  // a button's tooltip is its action's name (renderButtonForAction's label)
  const names = new Map((props.actionsForButtons || []).map(a => [a.id, a.name]));
  const found = [];
  let ellipsis = null;
  (function walk(n) {
    if (!n || typeof n !== 'object') return;
    if (Array.isArray(n)) return n.forEach(walk);
    if (n.type === RwAt.__connectedEllipsis) {
      const p = { ...n.props, ...n.type.mapState(state, n.props) };
      if (RwAt.__Ellipsis.prototype.render.call({ props: p, renderMoreActionsMenu() {}, handlePopOverOpen() {} })) {
        found.push('more');
        ellipsis = p;
      }
      return;
    }
    if (n.key && /_button$/.test(n.key)) {
      const id = n.key.replace(/_button$/, '');
      found.push(id + ': ' + names.get(id));
      return;
    }
    if (n.props && n.props.onEmojiSelected && n.type && n.type.mapState) {
      // the quick reactions container (f+F9 Q): its own mapState and
      // render decide whether the reactions show
      const p = { ...n.props, ...n.type.mapState(state, n.props) };
      if (n.type.Inner.prototype.render.call({ props: p })) found.push('quick_reactions');
      return;
    }
    if (n.props) walk(n.props.children);
  })(inst.renderMessageActions());
  return { buttons: found, ellipsis };
}

// fromMenu is MessageActionsMenu's actions for the props an element hands
// it: `ap` with them, as MessageActionsMenu calls it (its additionalActions
// come from the toolbar's width presentation, which no case has).
function fromMenu(state, el) {
  const p = el.props;
  if (p.additionalActions && p.additionalActions.length) throw new Error('message_actions: additionalActions are not modelled');
  return groups(JuvA.__ap(state, { channelId: p.channelId, ts: p.ts, threadTs: p.threadTs, messageContainerType: p.messageContainerType,
    menuUsageLocation: p.menuUsageLocation, showJumpActionInOverflow: p.showJumpActionInOverflow, rollup: p.rollup }));
}

// overflowMenu is the ⋮ menu: the ellipsis' renderMoreActionsMenu, then
// MessageActionsOverflowMenu (its location from persistActions), then
// MessageActionsMenu.
function overflowMenu(state, ellipsisProps) {
  const ov = RwAt.__Ellipsis.prototype.renderMoreActionsMenu.call({ props: ellipsisProps }, { onTriggerClose() {}, onMenuItemSelected() {}, targetBounds: undefined });
  const mm = withState(state, () => ov.type(ov.props));
  return fromMenu(state, mm);
}

// contextMenu is the right-click menu: MessageActionsContextMenu (DtN2)
// as each list renders it, its own guard included (no menu at all on a
// moderated, DLP tombstone, DSA-restricted or EKM-denied message: null).
// The channel list (AEjl renderContextMenuForItem) passes no threadTs;
// the thread pane (6xF9) passes the thread's, as does the Threads view.
function contextMenu(state, msg, where) {
  const props = { ts: msg.ts, channelId: msg.channel, messageContainerType: where };
  if (where !== Zhyr.MESSAGE_PANE) props.threadTs = msg.thread_ts || msg.ts;
  const el = withState(state, () => rt.req('DtN2').A(props));
  if (!el) return null;
  return fromMenu(state, el.props.children);
}

function run(c) {
  const store = storeFor(c);
  const state = store.getState();
  const msg = state.messages[c.conv.id][c.msg.ts];
  const where = WHERE[c.where || 'channel'];
  if (!where) throw new Error('message_actions: where ' + c.where);
  const tb = toolbar(state, msg, where);
  return {
    toolbar: tb && tb.buttons,
    // the ⋮ menu opens from ⋮ alone
    menu: tb && tb.ellipsis ? overflowMenu(state, tb.ellipsis) : [],
    context: contextMenu(state, msg, where),
  };
}

// exports read while the answer was made, of modules not run: none may
// be on the decision path (each is listed with why it's harmless)
const STUB_OK = new Set([]);

function main() {
  const snap = JSON.parse(fs.readFileSync(process.argv[2], 'utf8'));
  const base = snap.base || {};
  const res = {};
  for (const k of snap.cases) {
    const before = new Set(rt.stubbed);
    res[k.name] = run(merge(base, k.in));
    const touched = [...rt.stubbed].filter(x => !before.has(x) && !STUB_OK.has(x));
    if (touched.length) throw new Error(`message_actions: ${k.name}: stubbed exports read: ${touched.join(' ')}`);
  }
  // synchronous: process.exit drops what a pipe hasn't taken yet
  fs.writeSync(1, JSON.stringify(res) + '\n');
}

if (require.main === module) {
  try {
    main();
  } catch (e) {
    err(e && e.stack || String(e));
    process.exit(1);
  }
  process.exit(0);
}

module.exports = { rt, JuvA, RwAt, uAhD, Zhyr, bCH2, reducers, creators, action, reduce, connected, storeFor, merge, run, toolbar, overflowMenu, contextMenu, out, err };
