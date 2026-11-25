// slash_commands.cjs: what Slack's composer does with each case's typed
// slash command (internal/ui/composer/testdata/parity/slash-commands.json),
// from Slack's own send path run in node over the web client's public CDN
// chunks (build 66e72b57ade1):
//
//   node tools/oracles/slash_commands.cjs SNAPSHOT.json > out.json
//
// SLACK_CHUNKS and JSDOM as for message_actions.cjs (its header says
// where they come from), plus the two chunks the client commands load
// lazily (Zrd3's n.e calls): gantry-v2-async-slash-commands-local and
// gantry-v2-async-shared-js_modern_redux_stores_slack-connect-prefs-store_selectors_ts
// of the same build. tools/parity.py --refresh slash-commands runs it.
//
// The composer hands a typed message to handleSlashCommand (vHCW, "Intercept
// and handle a slash commands when sending a message"), which runs Slack's
// slash test, the s/old/new/ edit of the last message, the parse
// (parseSlashCmdArgsFromDelta) and executeCommand (pymN): the command's
// lookup, isSlashCommandEnabled (P+cE), a bare /remind, the client's own
// commands (Zrd3) and chat.command. Every module runs its own code
// (slackrt realAll). What's replaced, all of it at the path's exits, so
// that what's decided can be read rather than done:
//   - the API call (0hZc H): its method and args are the answer, and it
//     resolves {ok: true};
//   - Slackbot's ephemeral message (w0M8 A): its text is the answer;
//   - the reminder dialog (E8e8 A), a navigation (aYa+ o, "Navigate to
//     an ID ... multi-view architecture": its id), and the edit of the
//     last message (vHCW's own N: its {prev, next, ...});
//   - a client command's action (Zrd3 XU's entry) is marked with its
//     canonical name, then runs: what it does next (an API call, a
//     dialog, Slackbot's feedback) comes out at the exits above;
//   - executeCommand's injection slot (XFqE RS), which boot fills with
//     pymN's thunk: filled with it here.
//
// For each case it prints {route, ...}:
//   message: not a command (sent as a message);
//   edit: the s/old/new/ edit, with prev, next, replaceAll, ignoreCase;
//   server: chat.command's args (command, disp, channel, thread_ts, team_id,
//     app_id; blocks as their count);
//   local: a client command ran (an API call, a dialog), by its canonical
//     name;
//   remind: the reminder dialog;
//   feedback: Slackbot's ephemeral text: a command refused, or a client
//     command whose only answer was that text (/msg's "A valid member
//     name is required.", /collapse's note).
'use strict';
const fs = require('fs');
const { makeRuntime, installBrowser } = require('./slackrt.cjs');

const CHUNKS = process.env.SLACK_CHUNKS || '/root/clack-ops/scratch/slash-parity/cdn';
const JSDOM_DIR = process.env.JSDOM || '/root/clack-ops/scratch/canvas-edit/npm';
installBrowser(JSDOM_DIR);
const err = console.error.bind(console);
console.log = console.info = console.debug = console.warn = () => {}; // modules log at load

const exit = (kind, o) => ({ __exit: kind, ...o });
// The client commands whose action runs past the mark, by canonical name:
// the ones whose answer comes from the store and the command's own rules
// (Clack ports each as a decision: MsgCommand, OpenCommand, LeaveCommand,
// /rename and /archive's dialogs, topicCommand, ExpandCommand). The others
// stop at the mark: /invite and /invite_people go on through
// conversations.invite's answers, which the API exit doesn't model;
// /search needs the Sonic client's search store; the rest open UI only.
const FOLLOW = new Set(['/msg', '/open', '/leave', '/rename', '/archive', '/topic', '/collapse', '/expand']);
let kindSet;
const kinds = () => kindSet || (kindSet = new Set(Object.values(rt.req('ZOnG').gu)));
const rt = makeRuntime({
  dir: CHUNKS,
  realAll: true,
  patch: {
    vHCW: [['e(N({channelId:i,threadTs:r,...w}))', 'e({__exit:"edit",...w})']],
  },
  partial: {
    ...(globalThis.__parityPartial || {}), // a debugging hook
    '0hZc': { H: () => ({ method, args }) => exit('api', { method, args }) },
    w0M8: { A: () => o => exit('ephemeral', { text: o && o.message ? o.message.text : undefined }) },
    E8e8: { A: () => () => exit('remind') },
    // a navigation needs the Sonic client's store: where it goes is read
    'aYa+': { o: () => p => exit('navigate', { id: p && p.id }) },
    Zrd3: {
      // executeCommand dispatches action(params): the mark, then (for the
      // commands FOLLOW lists) the command's own thunk
      XU: (req, orig) => (state, name) => {
        const x = orig(state, name);
        if (!x || !x.action) return x;
        return { ...x, action: p => async dispatch => {
          dispatch(exit('local', { name }));
          if (!FOLLOW.has(name)) return undefined;
          try {
            return await dispatch(x.action(p));
          } catch (e) {
            // Slack's own refusals (CUSTOM_ERROR with its feedback and
            // the other ZOnG gu kinds) go on to handleSlashCommand's
            // catch; anything else is code node can't run (a router, the
            // Sonic client store): the answer stops at the mark
            if (e && kinds().has(e.message)) throw e;
            dispatch(exit('unfollowed', { name, error: String(e && e.message || e) }));
            return undefined;
          }
        } };
      },
    },
    XFqE: { RS: req => req('pymN').R },
  },
});

const vHCW = rt.req('vHCW');
const Delta = rt.req('mM05');
const DeltaClass = Delta.default || Delta;
const reducers = rt.req('4GYe').i().getReducerMapForStoreType('team');
const creators = rt.req('8Ifm').D9();
const stores = rt.req('W+Gl');

function action(desc) {
  const hits = Object.keys(creators).filter(t => t.slice(t.indexOf(']') + 1).trim() === desc);
  if (hits.length !== 1) throw new Error(`slash_commands: ${hits.length} action creators named ${JSON.stringify(desc)}`);
  return creators[hits[0]];
}

function reduce(state, act) {
  const next = {};
  for (const [k, r] of Object.entries(reducers)) next[k] = r(state ? state[k] : undefined, act);
  return next;
}

function merge(base, inp) {
  const r = { ...base, ...inp };
  for (const k of ['experiments', 'team_prefs']) r[k] = { ...(base[k] || {}), ...(inp[k] || {}) };
  r.users = [...(base.users || []), ...(inp.users || [])];
  r.commands = { ...(base.commands || {}), ...(inp.commands || {}) };
  r.permissions = { team: { ...(base.permissions?.team || {}), ...(inp.permissions?.team || {}) }, channel: { ...(base.permissions?.channel || {}), ...(inp.permissions?.channel || {}) } };
  return r;
}

// run gives a case's answer: its store built through Slack's own reducers
// and actions, then handleSlashCommand on its delta.
async function run(c, name = '') {
  let state = reduce(undefined, { type: '@@INIT' });
  const exits = [];
  const getState = () => state;
  const dispatch = a => {
    if (typeof a === 'function') return a(dispatch, getState, ctx => ctx && ctx.defaultValue);
    if (a && a.__exit) {
      exits.push(a);
      return Promise.resolve(a.__exit === 'api' ? { ok: true } : undefined);
    }
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
  dispatch(action('Initialize team preferences')({ teamId: team, teamPrefs: { ...c.team_prefs } }));
  const perms = o => Object.entries(o || {}).map(([id, isAllowed]) => ({ id, isAllowed: !!isAllowed }));
  dispatch(action('Sets team permission objects')({ memberId: c.self.id, teamId: team, permissions: perms(c.permissions && c.permissions.team) }));
  dispatch(action('Sets channel permission objects')({ memberId: c.self.id, channelId: c.conv.id, permissions: perms(c.permissions && c.permissions.channel), timestamp: 1700000000 }));
  dispatch(action('Bulk add members [proto]')({ objects: [c.self, ...c.users].map(u => JSON.parse(JSON.stringify(u))) }));
  dispatch(action('Bulk add channels [proto]')({ objects: [JSON.parse(JSON.stringify(c.conv))] }));
  dispatch(action('Adds an array of slash commands from the server')({
    commands: JSON.parse(JSON.stringify(c.commands)), cacheTs: 1700000000,
    experimentIa4SlashCommandFeGroupOn: c.experiments.ia4_slash_command_fe === 'on',
  }));
  const delta = new DeltaClass(JSON.parse(JSON.stringify(c.ops)));
  let threw;
  let ret;
  try {
    ret = await dispatch(vHCW.A({ delta, channelId: c.conv.id, threadTs: c.thread_ts || undefined, viewContext: c.thread_ts ? 'Thread' : 'Channel', windowId: 'main' }));
  } catch (e) {
    threw = e;
  }
  if (threw && process.env.SLASH_DEBUG) err(`${name}: threw`, threw && threw.stack || threw, JSON.stringify(exits));
  const at = exits.findIndex(x => x.__exit === 'local');
  if (at >= 0) {
    // a client command ran; what it did first decides (executeCommand's
    // frecency write comes before the mark): Slackbot's feedback (a
    // refusal of its own, /collapse's note) is that feedback; an API call,
    // a dialog or nothing observable is the command running
    const next = exits[at + 1];
    if (next && next.__exit === 'unfollowed') err(`slash_commands: ${name}: ${next.name} not followed past: ${next.error}`);
    if (next && next.__exit === 'ephemeral') return { route: 'feedback', text: next.text };
    return { route: 'local', name: exits[at].name };
  }
  const first = exits.find(x => x.__exit === 'api' && x.method === 'chat.command') ||
    exits.find(x => x.__exit !== 'ephemeral' && x.__exit !== 'api') || exits.find(x => x.__exit === 'api') || exits[0];
  if (!first) {
    if (threw) throw threw;
    return ret === null ? { route: 'message' } : { route: 'none' };
  }
  switch (first.__exit) {
    case 'edit':
      return { route: 'edit', prev: first.prev, next: first.next, replaceAll: !!first.replaceAll, ignoreCase: !!first.ignoreCase };
    case 'api': {
      const a = first.args || {};
      const o = { route: 'server', method: first.method, command: a.command, disp: a.disp };
      for (const k of ['channel', 'thread_ts', 'team_id', 'app_id']) if (a[k] !== undefined) o[k] = a[k];
      if (a.blocks !== undefined) o.blocks = Array.isArray(a.blocks) ? a.blocks.length : typeof a.blocks;
      if (a.text !== undefined) o.text = a.text;
      return o;
    }
    case 'local':
      return { route: 'local', name: first.name };
    case 'remind':
      return { route: 'remind' };
    case 'ephemeral':
      return { route: 'feedback', text: first.text };
  }
  throw new Error('slash_commands: exit ' + first.__exit);
}

async function main() {
  const snap = JSON.parse(fs.readFileSync(process.argv[2], 'utf8'));
  const base = snap.base || {};
  const res = {};
  for (const k of snap.cases) {
    const before = new Set(rt.stubbed);
    res[k.name] = await run(merge(base, k.in), k.name);
    const touched = [...rt.stubbed].filter(x => !before.has(x));
    if (touched.length) throw new Error(`slash_commands: ${k.name}: stubbed exports read: ${touched.join(' ')}`);
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
