// env.js: core of the browser environment, evaluated first.
// Provides: __jr (private: CommonJS registry for vendored code, the host
// functions as __jr.host, timer/immediate dispatch), the event loop APIs,
// console, performance, EventTarget/Event/CustomEvent/DOMException,
// queueMicrotask, structuredClone and small language extras.
// Later files (web.js, intl.js, dom.js, react-host.js) build on __jr.
(() => {
  'use strict';
  const H = globalThis.__jrh;
  delete globalThis.__jrh;
  const G = globalThis;
  const def = (obj, name, value) =>
    Object.defineProperty(obj, name, { value, writable: true, configurable: true, enumerable: false });

  // ---- CommonJS registry (vendored React) ----
  const mods = new Map(), cache = new Map();
  const jr = {
    host: H,
    def,
    define(name, factory) { mods.set(name, factory); },
    require(name) {
      const c = cache.get(name);
      if (c) return c.exports;
      const f = mods.get(name);
      if (!f) throw new Error("Cannot find module '" + name + "'");
      const module = { exports: {} };
      cache.set(name, module);
      f(module, module.exports, jr.require);
      return module.exports;
    },
  };
  def(G, '__jr', jr);
  if (!G.__clack) def(G, '__clack', {});
  def(G, 'globalThis', G);
  def(G, 'window', G);
  def(G, 'self', G);
  def(G, 'global', G);

  // report an exception thrown by a callback without stopping the caller
  const reportError = (e) => {
    try {
      if (typeof G.dispatchEvent === 'function' && G.ErrorEvent) {
        const ev = new G.ErrorEvent('error', { error: e, message: e && e.message, cancelable: true });
        if (!G.dispatchEvent(ev)) return;
      }
    } catch {}
    H.log('error', 'Uncaught ' + fmtErr(e));
  };
  jr.reportError = reportError;
  def(G, 'reportError', reportError);

  // ---- console ----
  const fmtErr = (e) => {
    if (e && typeof e === 'object' && 'stack' in e && e.stack) {
      const head = (e.name || 'Error') + ': ' + e.message;
      return String(e.stack).startsWith(head) ? String(e.stack) : head + '\n' + e.stack;
    }
    if (e instanceof Error) return e.name + ': ' + e.message;
    return inspect(e, 2);
  };
  const inspect = (v, depth = 2, seen = new Set()) => {
    switch (typeof v) {
      case 'string': return seen.size ? JSON.stringify(v) : v;
      case 'number': case 'boolean': case 'undefined': case 'symbol': return String(v);
      case 'bigint': return v + 'n';
      case 'function': return '[Function: ' + (v.name || 'anonymous') + ']';
    }
    if (v === null) return 'null';
    if (v instanceof Error) return fmtErr(v);
    if (seen.has(v)) return '[Circular]';
    if (depth < 0) return Array.isArray(v) ? '[Array]' : '[Object]';
    seen.add(v);
    try {
      if (Array.isArray(v)) return '[ ' + v.slice(0, 100).map((x) => inspect(x, depth - 1, seen)).join(', ') + (v.length > 100 ? ', …' : '') + ' ]';
      if (v instanceof Map) return 'Map(' + v.size + ') { ' + [...v].slice(0, 50).map(([k, x]) => inspect(k, depth - 1, seen) + ' => ' + inspect(x, depth - 1, seen)).join(', ') + ' }';
      if (v instanceof Set) return 'Set(' + v.size + ') { ' + [...v].slice(0, 50).map((x) => inspect(x, depth - 1, seen)).join(', ') + ' }';
      if (typeof v.nodeType === 'number' && v.nodeName) return '<' + String(v.nodeName).toLowerCase() + '>';
      const name = v.constructor && v.constructor !== Object && v.constructor.name ? v.constructor.name + ' ' : '';
      const keys = Object.keys(v);
      return name + '{ ' + keys.slice(0, 50).map((k) => k + ': ' + inspect(v[k], depth - 1, seen)).join(', ') + (keys.length > 50 ? ', …' : '') + ' }';
    } catch (e) {
      return '[object]';
    } finally {
      seen.delete(v);
    }
  };
  jr.inspect = inspect;
  const format = (args) => {
    if (typeof args[0] === 'string' && args[0].includes('%')) {
      let i = 1;
      const s = args[0].replace(/%[sdifoOjc%]/g, (m) => {
        if (m === '%%') return '%';
        if (i >= args.length) return m;
        const a = args[i++];
        switch (m) {
          case '%s': return typeof a === 'string' ? a : inspect(a, 1);
          case '%d': case '%i': return String(parseInt(a));
          case '%f': return String(Number(a));
          case '%c': return '';
          default: return inspect(a, 2);
        }
      });
      return [s, ...args.slice(i).map((a) => inspect(a))].join(' ');
    }
    return args.map((a) => inspect(a)).join(' ');
  };
  const counts = new Map(), timersC = new Map();
  let group = '';
  const out = (level) => (...a) => H.log(level, group + format(a));
  def(G, 'console', {
    log: out('log'), info: out('info'), warn: out('warn'), error: out('error'), debug: out('debug'), trace: out('debug'),
    dir: (v) => H.log('log', group + inspect(v, 3)),
    dirxml: out('log'),
    table: (v) => H.log('log', group + inspect(v, 3)),
    assert: (c, ...a) => { if (!c) H.log('error', group + 'Assertion failed' + (a.length ? ': ' + format(a) : '')); },
    count: (l = 'default') => { const n = (counts.get(l) || 0) + 1; counts.set(l, n); H.log('log', group + l + ': ' + n); },
    countReset: (l = 'default') => counts.delete(l),
    time: (l = 'default') => timersC.set(l, H.now()),
    timeLog: (l = 'default', ...a) => H.log('log', group + l + ': ' + (H.now() - (timersC.get(l) || 0)).toFixed(3) + 'ms ' + format(a)),
    timeEnd: (l = 'default') => { H.log('log', group + l + ': ' + (H.now() - (timersC.get(l) || 0)).toFixed(3) + 'ms'); timersC.delete(l); },
    group: (...a) => { if (a.length) H.log('log', group + format(a)); group += '  '; },
    groupCollapsed: (...a) => { if (a.length) H.log('log', group + format(a)); group += '  '; },
    groupEnd: () => { group = group.slice(2); },
    clear: () => {},
    profile: () => {}, profileEnd: () => {}, timeStamp: () => {},
  });

  // ---- microtasks ----
  def(G, 'queueMicrotask', (fn) => {
    if (typeof fn !== 'function') throw new TypeError("Failed to execute 'queueMicrotask': parameter 1 is not of type 'Function'.");
    Promise.resolve().then(() => { try { fn(); } catch (e) { reportError(e); } });
  });

  // ---- timers (Go keeps the heap; ids are shared by all timer kinds) ----
  let nextId = 1;
  const timers = new Map(); // id → {fn, args, interval}
  const toMs = (ms) => { ms = Number(ms); return ms > 0 && ms < 2147483648 ? ms : 0; };
  const setTimer = (fn, ms, args, interval) => {
    const id = nextId++;
    if (typeof fn !== 'function') { const code = String(fn); fn = () => (0, eval)(code); }
    const t = { fn, args, interval: interval ? Math.max(toMs(ms), 1) : 0 };
    timers.set(id, t);
    H.timer(id, interval ? t.interval : toMs(ms));
    return id;
  };
  const clearTimer = (id) => {
    id = Number(id);
    if (timers.delete(id)) H.clearTimer(id);
  };
  jr.fire = (id) => {
    const t = timers.get(id);
    if (!t) return;
    if (t.interval) H.timer(id, t.interval);
    else timers.delete(id);
    try { t.fn.apply(G, t.args); } catch (e) { reportError(e); }
  };
  def(G, 'setTimeout', (fn, ms, ...args) => setTimer(fn, ms, args, false));
  def(G, 'setInterval', (fn, ms, ...args) => setTimer(fn, ms, args, true));
  def(G, 'clearTimeout', clearTimer);
  def(G, 'clearInterval', clearTimer);

  // setImmediate / MessageChannel: one Go macrotask runs every queued callback
  // that existed when it started (later ones run in the next macrotask).
  let imm = [], immScheduled = false;
  const immIds = new Map();
  jr.immediate = (fn) => {
    imm.push(fn);
    if (!immScheduled) { immScheduled = true; H.wake(); }
  };
  jr.runImmediates = () => {
    immScheduled = false;
    const q = imm;
    imm = [];
    for (let i = 0; i < q.length; i++) {
      try { q[i](); } catch (e) { reportError(e); }
    }
  };
  let immN = 1;
  def(G, 'setImmediate', (fn, ...args) => {
    const id = immN++;
    immIds.set(id, true);
    jr.immediate(() => { if (immIds.delete(id)) fn(...args); });
    return id;
  });
  def(G, 'clearImmediate', (id) => immIds.delete(id));

  // requestAnimationFrame: ~60 Hz batches; all callbacks of a frame get the same timestamp.
  let rafs = new Map(), rafN = 1, rafTimer = 0;
  const runFrame = () => {
    rafTimer = 0;
    const q = rafs;
    rafs = new Map();
    const now = H.now();
    for (const fn of q.values()) { try { fn(now); } catch (e) { reportError(e); } }
  };
  def(G, 'requestAnimationFrame', (fn) => {
    const id = rafN++;
    rafs.set(id, fn);
    if (!rafTimer) rafTimer = setTimer(runFrame, 16 - (H.now() % 16), [], false);
    return id;
  });
  def(G, 'cancelAnimationFrame', (id) => rafs.delete(id));
  def(G, 'requestIdleCallback', (fn, opts) => setTimer(() => {
    const end = H.now() + 50;
    fn({ didTimeout: false, timeRemaining: () => Math.max(0, end - H.now()) });
  }, 1, [], false));
  def(G, 'cancelIdleCallback', clearTimer);

  // ---- performance ----
  const marks = [];
  def(G, 'performance', {
    now: () => H.now(),
    timeOrigin: H.timeOrigin(),
    mark: (name, o) => { const m = { name, entryType: 'mark', startTime: o && o.startTime != null ? o.startTime : H.now(), duration: 0, detail: o && o.detail }; marks.push(m); return m; },
    measure: (name, a, b) => { const m = { name, entryType: 'measure', startTime: 0, duration: 0 }; marks.push(m); return m; },
    getEntries: () => marks.slice(),
    getEntriesByName: (n) => marks.filter((m) => m.name === n),
    getEntriesByType: (t) => marks.filter((m) => m.entryType === t),
    clearMarks: () => { marks.length = 0; },
    clearMeasures: () => {},
    toJSON: () => ({ timeOrigin: H.timeOrigin() }),
    memory: { usedJSHeapSize: 0, totalJSHeapSize: 0, jsHeapSizeLimit: 0 },
  });

  // ---- DOMException ----
  const codes = { IndexSizeError: 1, HierarchyRequestError: 3, WrongDocumentError: 4, InvalidCharacterError: 5, NoModificationAllowedError: 7, NotFoundError: 8, NotSupportedError: 9, InvalidStateError: 11, SyntaxError: 12, InvalidModificationError: 13, NamespaceError: 14, InvalidAccessError: 15, TypeMismatchError: 17, SecurityError: 18, NetworkError: 19, AbortError: 20, URLMismatchError: 21, QuotaExceededError: 22, TimeoutError: 23, InvalidNodeTypeError: 24, DataCloneError: 25 };
  class DOMException extends Error {
    constructor(message = '', name = 'Error') {
      super(message);
      def(this, 'name', String(name));
    }
    get code() { return codes[this.name] || 0; }
  }
  for (const [n, c] of Object.entries(codes)) def(DOMException, n.replace(/([a-z])([A-Z])/g, '$1_$2').toUpperCase().replace(/_ERROR$/, '_ERR'), c);
  def(G, 'DOMException', DOMException);

  // ---- Event / EventTarget ----
  const kStop = Symbol('stop'), kImmediate = Symbol('stopImmediate'), kCanceled = Symbol('canceled'), kPassive = Symbol('passive');
  class Event {
    constructor(type, init = {}) {
      if (arguments.length === 0) throw new TypeError("Failed to construct 'Event': 1 argument required, but only 0 present.");
      this.type = String(type);
      this.bubbles = !!init.bubbles;
      this.cancelable = !!init.cancelable;
      this.composed = !!init.composed;
      this.target = null;
      this.currentTarget = null;
      this.srcElement = null;
      this.eventPhase = 0;
      this.isTrusted = false;
      this.timeStamp = H.now();
      this.returnValue = true;
      this[kStop] = false;
      this[kImmediate] = false;
      this[kCanceled] = false;
      this[kPassive] = false;
    }
    get defaultPrevented() { return this[kCanceled]; }
    get cancelBubble() { return this[kStop]; }
    set cancelBubble(v) { if (v) this[kStop] = true; }
    preventDefault() { if (this.cancelable && !this[kPassive]) { this[kCanceled] = true; this.returnValue = false; } }
    stopPropagation() { this[kStop] = true; }
    stopImmediatePropagation() { this[kStop] = true; this[kImmediate] = true; }
    composedPath() { return this._path ? this._path.slice() : this.currentTarget ? [this.currentTarget] : []; }
    initEvent(type, bubbles, cancelable) { this.type = String(type); this.bubbles = !!bubbles; this.cancelable = !!cancelable; }
  }
  Event.NONE = 0; Event.CAPTURING_PHASE = 1; Event.AT_TARGET = 2; Event.BUBBLING_PHASE = 3;
  jr.kStop = kStop; jr.kImmediate = kImmediate; jr.kCanceled = kCanceled; jr.kPassive = kPassive;
  class CustomEvent extends Event {
    constructor(type, init = {}) { super(type, init); this.detail = init.detail === undefined ? null : init.detail; }
    initCustomEvent(type, b, c, detail) { this.initEvent(type, b, c); this.detail = detail; }
  }
  class ErrorEvent extends Event {
    constructor(type, init = {}) { super(type, init); Object.assign(this, { message: init.message || '', filename: init.filename || '', lineno: init.lineno || 0, colno: init.colno || 0, error: init.error }); }
  }
  class PromiseRejectionEvent extends Event {
    constructor(type, init = {}) { super(type, init); this.promise = init.promise; this.reason = init.reason; }
  }
  class MessageEvent extends Event {
    constructor(type, init = {}) { super(type, init); Object.assign(this, { data: init.data === undefined ? null : init.data, origin: init.origin || '', lastEventId: init.lastEventId || '', source: init.source || null, ports: init.ports || [] }); }
  }
  class ProgressEvent extends Event {
    constructor(type, init = {}) { super(type, init); Object.assign(this, { lengthComputable: !!init.lengthComputable, loaded: init.loaded || 0, total: init.total || 0 }); }
  }
  for (const [n, c] of Object.entries({ Event, CustomEvent, ErrorEvent, PromiseRejectionEvent, MessageEvent, ProgressEvent })) def(G, n, c);

  const kListeners = Symbol('listeners');
  jr.kListeners = kListeners;
  const flatOpts = (o) => typeof o === 'boolean' ? { capture: o } : (o || {});
  class EventTarget {
    constructor() { this[kListeners] = null; } // Symbol keys never show in Object.keys / for-in
    addEventListener(type, cb, opts) {
      if (cb == null) return;
      const o = flatOpts(opts);
      if (o.signal && o.signal.aborted) return;
      let m = this[kListeners];
      if (!m) { m = new Map(); this[kListeners] = m; }
      type = String(type);
      let l = m.get(type);
      if (!l) { l = []; m.set(type, l); }
      const capture = !!o.capture;
      if (l.some((x) => x.cb === cb && x.capture === capture)) return;
      const entry = { cb, capture, once: !!o.once, passive: !!o.passive, removed: false };
      l.push(entry);
      if (o.signal) o.signal.addEventListener('abort', () => this.removeEventListener(type, cb, { capture }), { once: true });
      jr.onListen && jr.onListen(this, type, true);
    }
    removeEventListener(type, cb, opts) {
      const m = this[kListeners];
      if (!m) return;
      const l = m.get(String(type));
      if (!l) return;
      const capture = !!flatOpts(opts).capture;
      const i = l.findIndex((x) => x.cb === cb && x.capture === capture);
      if (i >= 0) {
        l[i].removed = true;
        l.splice(i, 1);
        if (!l.length) m.delete(String(type));
        jr.onListen && jr.onListen(this, String(type), false);
      }
    }
    dispatchEvent(ev) {
      if (!(ev instanceof Event)) throw new TypeError("Failed to execute 'dispatchEvent' on 'EventTarget': parameter 1 is not of type 'Event'.");
      return jr.dispatch(this, ev);
    }
  }
  // invoke listeners of one target for one phase (capture: true/false/null = both, at target)
  jr.invoke = (target, ev, phase) => {
    const m = target[kListeners];
    // on<type> handler properties (el.onclick = fn, window.onerror …)
    const l = m && m.get(ev.type);
    const handlerProp = phase !== 1 && target['on' + ev.type];
    if (!l && typeof handlerProp !== 'function') return;
    ev.currentTarget = target;
    if (l) {
      for (const x of l.slice()) {
        if (x.removed) continue;
        if (phase === 1 && !x.capture) continue;
        if (phase === 3 && x.capture) continue;
        if (x.once) target.removeEventListener(ev.type, x.cb, { capture: x.capture });
        ev[kPassive] = x.passive;
        try {
          if (typeof x.cb === 'function') x.cb.call(target, ev);
          else if (x.cb && typeof x.cb.handleEvent === 'function') x.cb.handleEvent(ev);
        } catch (e) { reportError(e); }
        ev[kPassive] = false;
        if (ev[kImmediate]) return;
      }
    }
    if (typeof handlerProp === 'function' && !ev[kImmediate]) {
      try {
        const r = handlerProp.call(target, ev);
        if (r === false) ev.preventDefault();
      } catch (e) { reportError(e); }
    }
  };
  // default dispatch: no tree (dom.js replaces it with the DOM path walk)
  jr.dispatch = (target, ev) => {
    ev.target = ev.srcElement = target;
    ev._path = [target];
    ev.eventPhase = 2;
    jr.invoke(target, ev, 2);
    ev.eventPhase = 0;
    ev.currentTarget = null;
    return !ev[kCanceled];
  };
  def(G, 'EventTarget', EventTarget);
  // make the global object an EventTarget
  def(G, kListeners, null);
  for (const k of ['addEventListener', 'removeEventListener', 'dispatchEvent']) def(G, k, EventTarget.prototype[k]);

  // ---- structuredClone ----
  const clone = (v, seen) => {
    if (typeof v !== 'object' || v === null) {
      if (typeof v === 'function' || typeof v === 'symbol') throw new DOMException((typeof v === 'function' ? 'function' : 'Symbol') + ' could not be cloned.', 'DataCloneError');
      return v;
    }
    if (seen.has(v)) return seen.get(v);
    let out;
    const tag = Object.prototype.toString.call(v);
    switch (tag) {
      case '[object Date]': out = new Date(v.getTime()); break;
      case '[object RegExp]': out = new RegExp(v.source, v.flags); break;
      case '[object Boolean]': case '[object Number]': case '[object String]': case '[object BigInt]': out = Object(v.valueOf()); break;
      case '[object ArrayBuffer]': out = v.slice(0); break;
      case '[object Map]': out = new Map(); seen.set(v, out); for (const [k, x] of v) out.set(clone(k, seen), clone(x, seen)); return out;
      case '[object Set]': out = new Set(); seen.set(v, out); for (const x of v) out.add(clone(x, seen)); return out;
      case '[object Array]': out = new Array(v.length); seen.set(v, out); for (let i = 0; i < v.length; i++) if (i in v) out[i] = clone(v[i], seen); return out;
      case '[object Error]': {
        const C = G[v.name] && G[v.name].prototype instanceof Error ? G[v.name] : Error;
        out = new C(v.message); if (v.stack) out.stack = v.stack; if ('cause' in v) out.cause = clone(v.cause, seen); break;
      }
      default:
        if (ArrayBuffer.isView(v)) {
          if (v instanceof DataView) out = new DataView(clone(v.buffer, seen), v.byteOffset, v.byteLength);
          else out = new v.constructor(clone(v.buffer, seen), v.byteOffset, v.length);
          break;
        }
        if (G.Blob && v instanceof G.Blob) { out = v.slice(0, v.size, v.type); if (G.File && v instanceof G.File) out = new G.File([out], v.name, { type: v.type, lastModified: v.lastModified }); break; }
        if (tag === '[object WeakMap]' || tag === '[object WeakSet]' || tag === '[object Promise]' || tag === '[object WeakRef]' || (G.Node && v instanceof G.Node))
          throw new DOMException(tag.slice(8, -1) + ' object could not be cloned.', 'DataCloneError');
        out = {};
        seen.set(v, out);
        for (const k of Object.keys(v)) out[k] = clone(v[k], seen);
        return out;
    }
    seen.set(v, out);
    return out;
  };
  def(G, 'structuredClone', (v) => clone(v, new Map()));

  // ---- language extras not (yet) in QuickJS-NG ----
  if (!RegExp.escape) {
    def(RegExp, 'escape', (s) => {
      if (typeof s !== 'string') throw new TypeError('RegExp.escape requires a string');
      let out = '';
      for (let i = 0; i < s.length; i++) {
        const c = s[i], code = s.charCodeAt(i);
        if (i === 0 && /[0-9A-Za-z]/.test(c)) out += '\\x' + code.toString(16).padStart(2, '0');
        else if ('^$\\.*+?()[]{}|/'.includes(c)) out += '\\' + c;
        else if (',-=<>#&!%:;@~\'`"'.includes(c)) out += '\\x' + code.toString(16).padStart(2, '0');
        else if (/[\t\n\v\f\r ]/.test(c)) out += { '\t': '\\t', '\n': '\\n', '\v': '\\v', '\f': '\\f', '\r': '\\r', ' ': '\\x20' }[c];
        else if (/[\u00A0\u1680\u2000-\u200A\u2028\u2029\u202F\u205F\u3000\uFEFF]/.test(c) || (code >= 0xd800 && code <= 0xdfff && !(code <= 0xdbff && (s.charCodeAt(i + 1) & 0xfc00) === 0xdc00) && !((code & 0xfc00) === 0xdc00 && i > 0 && (s.charCodeAt(i - 1) & 0xfc00) === 0xd800)))
          out += '\\u' + code.toString(16).padStart(4, '0');
        else out += c;
      }
      return out;
    });
  }
  if (!Map.groupBy) def(Map, 'groupBy', (items, fn) => { const m = new Map(); let i = 0; for (const x of items) { const k = fn(x, i++); const a = m.get(k); if (a) a.push(x); else m.set(k, [x]); } return m; });
  if (!Object.groupBy) def(Object, 'groupBy', (items, fn) => { const o = Object.create(null); let i = 0; for (const x of items) { const k = fn(x, i++); (o[k] || (o[k] = [])).push(x); } return o; });
  if (!Promise.withResolvers) def(Promise, 'withResolvers', function () { let resolve, reject; const promise = new this((a, b) => { resolve = a; reject = b; }); return { promise, resolve, reject }; });
  if (!Promise.try) def(Promise, 'try', function (fn, ...a) { return new this((res) => res(fn(...a))); });
  if (!Error.captureStackTrace) def(Error, 'captureStackTrace', (o) => { if (o && typeof o === 'object') { try { o.stack = new Error().stack; } catch {} } });
  if (Error.stackTraceLimit === undefined) Error.stackTraceLimit = 10;

  // unhandled rejections are reported by Go; this hook lets dom.js fire
  // window 'unhandledrejection'.
  jr.unhandled = (promise, reason) => {
    if (G.dispatchEvent) {
      const ev = new PromiseRejectionEvent('unhandledrejection', { promise, reason, cancelable: true });
      return G.dispatchEvent(ev);
    }
    return true;
  };
})();
