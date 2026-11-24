// slackrt.cjs: a small rspack runtime over Slack's public web-client chunks,
// for the parity oracles (docs/PARITY.md) that run Slack's own decision
// functions in node.
//
// Slack's chunks come from its CDN (https://a.slack-edge.com/bv1-13-br/,
// fetched with `curl --compressed` and no cookie; datamine-plus's
// build/chunks.txt names them). This repo doesn't carry them: a refresh
// points SLACK_CHUNKS (or the oracle's own default) at a directory of
// them. Only the gantry chunks are read; modern.vendor is another build
// whose module ids mean other things.
//
// Only the modules listed in `real` run their own factory. A module in
// `overrides` is the given exports instead (a store read, say). Every
// other module is a stub: each export it's asked for is a function that
// returns undefined, and the runtime logs the export (`stubbed`), so a
// run shows exactly which outside pieces its answer touched. A stub is
// a silent wrong answer waiting to happen (a stubbed predicate is
// falsy, a stubbed constant undefined): the oracles print the list and
// fail when an export outside their allowed list was read.
'use strict';
const fs = require('fs');
const path = require('path');

// installBrowser(jsdomDir, {userAgent}): jsdom's window as the global
// scope, for the modules' top levels (location, document, storage,
// observers). Timers they start never keep node alive. Slack reads the
// user agent (bowser) for what the client is: the desktop app's (DESKTOP_UA)
// says "Slack" (isDesktopApp) and a Chrome new enough to copy
// (canCopyLink's clipboard check).
const DESKTOP_UA = 'Mozilla/5.0 (Macintosh; Intel Mac OS X 10_15_7) AppleWebKit/537.36 (KHTML, like Gecko) Slack/4.53.287 Chrome/142.0.7444.59 Electron/39.1.2 Safari/537.36 Sonic Slack_SSB/4.53.287';
function installBrowser(jsdomDir, { userAgent = DESKTOP_UA } = {}) {
  const g = globalThis;
  const { JSDOM, ResourceLoader } = require(path.join(jsdomDir, 'node_modules', 'jsdom'));
  const dom = new JSDOM('<!DOCTYPE html><html><head></head><body></body></html>', { url: 'https://app.slack.com/client/T0/C0', resources: new ResourceLoader({ userAgent }) });
  const win = dom.window;
  for (const k of Object.getOwnPropertyNames(win)) {
    if (k in g) continue;
    try { Object.defineProperty(g, k, { value: win[k], configurable: true, writable: true }); } catch (e) { /* not copyable */ }
  }
  for (const k of ['window', 'self', 'top', 'parent', 'document', 'navigator', 'location', 'localStorage', 'sessionStorage']) {
    try { Object.defineProperty(g, k, { value: ['window', 'self', 'top', 'parent'].includes(k) ? g : win[k], configurable: true, writable: true }); } catch (e) { /* keep node's */ }
  }
  for (const k of ['addEventListener', 'removeEventListener', 'dispatchEvent', 'getComputedStyle', 'getSelection']) {
    if (typeof win[k] === 'function') g[k] = win[k].bind(win);
  }
  g.requestAnimationFrame = f => setTimeout(f, 0); g.cancelAnimationFrame = clearTimeout;
  g.requestIdleCallback = f => setTimeout(f, 0); g.cancelIdleCallback = clearTimeout;
  g.matchMedia = () => ({ matches: false, addListener() {}, removeListener() {}, addEventListener() {}, removeEventListener() {} });
  g.ResizeObserver = class { observe() {} disconnect() {} unobserve() {} };
  g.IntersectionObserver = class { observe() {} disconnect() {} unobserve() {} };
  g.Worker = class { postMessage() {} terminate() {} addEventListener() {} };
  g.BroadcastChannel = class { postMessage() {} close() {} addEventListener() {} removeEventListener() {} };
  g.TS = g.TS || {};
  g.boot_data = g.boot_data || {};
  for (const k of ['setTimeout', 'setInterval']) {
    const f = g[k];
    g[k] = (...a) => { const t = f(...a); if (t && t.unref) t.unref(); return t; };
  }
}

const START = /[{,](?:"([^"]{1,12})"|([A-Za-z0-9_$]{1,12})):\(function\(/g;

function chunkFiles(dir) {
  return fs.readdirSync(dir)
    .filter(f => f.endsWith('.min.js') && !f.startsWith('modern.vendor'))
    .sort()
    .map(f => path.join(dir, f));
}

// Index: module id -> its chunk text and where it starts. A module id
// can sit in two chunks (the same code); the first file in sort order
// wins.
function makeIndex(files) {
  const where = new Map();
  const texts = [];
  const starts = [];
  files.forEach((f, fi) => {
    const t = fs.readFileSync(f, 'utf8');
    texts[fi] = t;
    const st = [];
    START.lastIndex = 0;
    let m;
    while ((m = START.exec(t))) st.push([m.index, m[1] || m[2]]);
    starts[fi] = st;
    st.forEach(([, id], si) => { if (!where.has(id)) where.set(id, [fi, si]); });
  });
  return { where, texts, starts, files };
}

function tryParse(body) {
  try {
    const o = (0, eval)('({' + body + '})');
    const ks = Object.keys(o);
    return ks.length === 1 ? o[ks[0]] : null;
  } catch (e) {
    return null;
  }
}

function applyPatch(id, body, edits) {
  for (const [from, to] of edits || []) {
    const n = body.split(from).length - 1;
    if (n !== 1) throw new Error(`slackrt: patch for ${id} matches ${n} times: ${from}`);
    body = body.replace(from, () => to);
  }
  return body;
}

function makeFactories(ix, patch) {
  const factories = new Map();
  return function factoryOf(id) {
    if (factories.has(id)) return factories.get(id);
    const w = ix.where.get(id);
    if (!w) return null;
    const [fi, si] = w;
    const t = ix.texts[fi], st = ix.starts[fi];
    const begin = st[si][0] + 1;
    let f = null, body = null;
    // The module ends right before the comma that starts the next one. A
    // match inside the module (an inline "{x:(function(") doesn't parse,
    // so try the following starts in turn.
    for (let j = si + 1; j < st.length && j < si + 400 && !f; j++) {
      body = t.slice(begin, st[j][0]);
      f = tryParse(body);
    }
    if (!f && si + 400 >= st.length) {
      // a chunk's last module: it ends at one of the last few ")"s, before
      // the chunk's entry function or its closing "}]);"
      const all = t.slice(begin);
      for (let end = all.length; end > 0 && end > all.length - 4096 && !f; end--) {
        if (all[end - 1] !== ')') continue;
        body = all.slice(0, end);
        f = tryParse(body);
      }
    }
    if (!f) throw new Error('slackrt: cannot parse module ' + id);
    if (patch[id]) {
      f = tryParse(applyPatch(id, body, patch[id]));
      if (!f) throw new Error('slackrt: patched module ' + id + ' does not parse');
    }
    factories.set(id, f);
    return f;
  };
}

// A stub export read while modules load (a module's top level, as in
// connect(mapState)(Component) or a styled() call) is a chain: callable,
// constructible, every property the same chain. Read once the modules
// have loaded, by the code an oracle runs, it is a function returning
// undefined and the read is logged in `stubbed`; a chain made at load
// time and called later logs itself the same way and returns undefined.
function makeStubs(isLoading) {
  const stubbed = new Set(), atLoad = new Set();
  function chain(name) {
    const target = function () {};
    const p = new Proxy(target, {
      get(_, k) {
        if (k === '__esModule') return true;
        if (typeof k === 'symbol' || k === 'then' || k === 'prototype') return k === 'prototype' ? {} : undefined;
        return chain(name + '.' + String(k));
      },
      apply() {
        if (isLoading()) return chain(name + '()');
        stubbed.add(name + '() [made at load]');
        return undefined;
      },
      construct() { return chain('new ' + name); },
    });
    return p;
  }
  function makeStub(id) {
    return new Proxy({}, {
      get(_, k) {
        if (k === '__esModule') return true;
        if (typeof k === 'symbol' || k === 'then') return undefined;
        const name = id + '.' + String(k);
        if (isLoading()) {
          atLoad.add(name);
          return chain(name);
        }
        stubbed.add(name);
        return function () { return undefined; };
      },
    });
  }
  return { makeStub, stubbed, atLoad };
}

// makeRuntime({dir, real, overrides, partial, patch}) -> {req, stubbed,
// atLoad, has}.
//   overrides: {id: req => exports}, the module replaced whole.
//   partial: {id: {name: (req, original) => value}}, a real module with
//     some of its exports replaced (a store read next to the logic around
//     it); `original` is the export the module made.
//   patch: {id: [[from, to], ...]}, text replaced in a real module's
//     source before it runs, to export an internal (never to change what
//     it decides).
//   realAll: every module real but those in `stub` (an exploring aid, to
//     see what a path loads; an oracle lists its real modules).
function makeRuntime({ dir, real = [], overrides = {}, partial = {}, patch = {}, realAll = false, stub = [], onLoad = null }) {
  const ix = makeIndex(chunkFiles(dir));
  const factoryOf = makeFactories(ix, patch);
  const realSet = new Set(real);
  const stubSet = new Set(stub);
  const isReal = id => realAll ? !stubSet.has(id) && ix.where.has(id) : realSet.has(id);
  const loaded = [];
  const cache = new Map();
  let loading = 0;
  const { makeStub, stubbed, atLoad } = makeStubs(() => loading > 0);
  function req(id) {
    if (cache.has(id)) return cache.get(id).exports;
    if (overrides[id]) {
      const m = { exports: overrides[id](req) };
      cache.set(id, m);
      return m.exports;
    }
    if (!isReal(id)) {
      const m = { exports: makeStub(id) };
      cache.set(id, m);
      return m.exports;
    }
    const f = factoryOf(id);
    if (!f) throw new Error('slackrt: module not found: ' + id);
    const m = { id, exports: {}, loaded: false };
    cache.set(id, m);
    loaded.push(id);
    if (onLoad) onLoad(id, loaded.length);
    loading++;
    try {
      f.call(m.exports, m, m.exports, req);
    } finally {
      loading--;
    }
    for (const [k, make] of Object.entries(partial[id] || {})) {
      if (!Object.prototype.hasOwnProperty.call(m.exports, k)) throw new Error(`slackrt: ${id} has no export ${k} to replace`);
      const v = make(req, m.exports[k]);
      Object.defineProperty(m.exports, k, { enumerable: true, configurable: true, get: () => v });
    }
    m.loaded = true;
    return m.exports;
  }
  req.d = (ex, getters, values) => {
    for (const k in getters || {}) if (!Object.prototype.hasOwnProperty.call(ex, k)) Object.defineProperty(ex, k, { enumerable: true, configurable: true, get: getters[k] });
    for (const k in values || {}) if (!Object.prototype.hasOwnProperty.call(ex, k)) Object.defineProperty(ex, k, { enumerable: true, configurable: true, value: values[k], writable: true });
  };
  req.r = ex => { Object.defineProperty(ex, '__esModule', { value: true }); };
  req.n = m => { const g = m && m.__esModule ? () => m.default : () => m; req.d(g, { a: g }); return g; };
  req.o = (o, p) => Object.prototype.hasOwnProperty.call(o, p);
  // webpack's create-fake-namespace (import * of a CommonJS module)
  req.t = function (value, mode) {
    if (mode & 1) value = req(value);
    if (mode & 8) return value;
    if (typeof value === 'object' && value) {
      if (mode & 4 && value.__esModule) return value;
      if (mode & 16 && typeof value.then === 'function') return value;
    }
    const ns = Object.create(null);
    req.r(ns);
    const def = {};
    for (let cur = mode & 2 && value; (typeof cur === 'object' || typeof cur === 'function') && cur && cur !== Object.prototype && cur !== Array.prototype && cur !== Function.prototype; cur = Object.getPrototypeOf(cur)) {
      for (const k of Object.getOwnPropertyNames(cur)) if (!(k in def)) def[k] = () => value[k];
    }
    def.default = () => value;
    req.d(ns, def);
    return ns;
  };
  req.g = globalThis;
  req.p = '';
  req.u = id => id + '.js';
  // webpack's chunk load (n.e("slash-commands-local")): every chunk in
  // dir is indexed already, so a lazy import's modules are there to
  // require (as stubs when its chunk isn't in dir)
  req.e = () => Promise.resolve();
  req.b = 'file:///';
  req.nmd = m => m;
  req.hmd = m => m;
  return { req, stubbed, atLoad, loaded, has: id => ix.where.has(id), files: ix.files };
}

module.exports = { makeRuntime, installBrowser, DESKTOP_UA };
