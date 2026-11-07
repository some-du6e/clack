// react-host.js: real React 19 + a react-reconciler host config that renders
// into dom.js's elements, the __clack root API, native Slack components and
// a ReactDOM shim. Evaluated last.
(() => {
  'use strict';
  const G = globalThis, jr = __jr, def = jr.def;
  const React = jr.require('react');
  const jsx = jr.require('react/jsx-runtime');
  const Scheduler = jr.require('scheduler');
  const createReconciler = jr.require('react-reconciler');
  const K = jr.require('react-reconciler/constants');
  def(G, 'React', React);
  def(G, '__clack_jsx', jsx);
  def(G, '__clack_scheduler', Scheduler);

  const SVG = 1, HTML = 0;
  const textTypes = new Set(['textarea', 'noscript']);
  const hasOwn = Object.prototype.hasOwnProperty;
  const isEl = React.isValidElement;

  // ---- props → DOM ----
  const eventKey = (name) => name.slice(2).toLowerCase(); // onClickCapture → clickcapture
  const reserved = new Set(['children', 'key', 'ref', 'suppressContentEditableWarning', 'suppressHydrationWarning', 'defaultValue', 'defaultChecked', 'innerHTML', 'autoFocus', '__np']);
  const setProp = (el, name, v, svg, isCreate) => {
    if (name.length > 2 && name[0] === 'o' && name[1] === 'n' && name[2] >= 'A' && name[2] <= 'Z') {
      const h = el._h || (el._h = Object.create(null));
      const k = eventKey(name);
      if (typeof v === 'function') h[k] = v; else delete h[k];
      jr.markDirty(el);
      return;
    }
    switch (name) {
      case 'style': return;
      case 'dangerouslySetInnerHTML': if (v && v.__html != null) el.innerHTML = v.__html; else el.innerHTML = ''; return;
      case 'value':
        if (el.localName === 'option' || el.localName === 'button' || el.localName === 'li' || el.localName === 'progress' || el.localName === 'meter' || el.localName === 'param' || el.localName === 'data') { if (v == null) el.removeAttribute('value'); else el._setAttr('value', v); return; }
        if (v == null) { if (!isCreate) { delete el._val; jr.markDirty(el); } return; }
        if (el._val !== String(v)) { el._val = String(v); jr.markDirty(el); }
        return;
      case 'checked': if (v == null) { delete el._chk; jr.markDirty(el); } else { el._chk = !!v; jr.markDirty(el); } return;
      case 'selected': el._sel = !!v; jr.markDirty(el); return;
      case 'className': if (v == null || v === false) el.removeAttribute('class'); else el._setAttr('class', svg ? v : String(v)); return;
    }
    if (reserved.has(name)) return;
    const attr = jr.reactToAttr(name, svg);
    if (v == null || typeof v === 'function' || typeof v === 'symbol' || (v === false && !(name.startsWith('aria-') || name.startsWith('data-')))) { el.removeAttribute(attr); return; }
    if (typeof v === 'boolean' && (name.startsWith('aria-') || name.startsWith('data-'))) v = String(v);
    el._setAttr(attr, v);
  };
  const setStyle = (el, prev, next) => {
    if (prev) for (const k in prev) if (!next || !hasOwn.call(next, k) || next[k] == null || next[k] === '' || typeof next[k] === 'boolean') el._styleDel(k);
    if (next) for (const k in next) {
      const v = next[k];
      if (v == null || v === '' || typeof v === 'boolean') continue;
      if (!prev || prev[k] !== v) el._styleSet(k, v);
    }
  };
  const applyProps = (el, prev, next, svg, isCreate) => {
    if (prev) for (const k in prev) if (!hasOwn.call(next, k) && k !== 'children') { if (k === 'style') setStyle(el, prev.style, null); else setProp(el, k, null, svg, false); }
    for (const k in next) {
      const v = next[k];
      if (k === 'children') {
        if (typeof v === 'string' || typeof v === 'number' || typeof v === 'bigint') { if (!prev || prev.children !== v) el.textContent = String(v); }
        else if (prev && (typeof prev.children === 'string' || typeof prev.children === 'number') && el.localName !== 'textarea') el.textContent = '';
        continue;
      }
      if (prev && prev[k] === v) continue;
      if (k === 'style') setStyle(el, prev && prev.style, v);
      else setProp(el, k, v, svg, isCreate);
    }
    if (isCreate) {
      if (next.defaultValue != null && el._val === undefined && el.localName !== 'select') { if (el.localName === 'textarea' || el.localName === 'input') el._val = String(next.defaultValue); else el._setAttr('value', next.defaultValue); }
      if (next.defaultChecked != null && el._chk === undefined) el._chk = !!next.defaultChecked;
      if (el.localName === 'textarea' && el._val === undefined && next.children != null && typeof next.children !== 'object') el._val = String(next.children);
    }
  };
  const rootHost = { svg: HTML };

  // ---- update priority from the event being dispatched ----
  let curEvent = null;
  const discrete = new Set(['click', 'dblclick', 'auxclick', 'contextmenu', 'keydown', 'keyup', 'keypress', 'input', 'beforeinput', 'change', 'focus', 'blur', 'focusin', 'focusout', 'mousedown', 'mouseup', 'pointerdown', 'pointerup', 'pointercancel', 'submit', 'reset', 'copy', 'cut', 'paste', 'compositionstart', 'compositionend', 'compositionupdate', 'dragstart', 'dragend', 'drop', 'touchstart', 'touchend', 'touchcancel', 'select', 'selectionchange', 'toggle', 'close', 'cancel', 'fullscreenchange', 'invalid']);
  const continuous = new Set(['mousemove', 'mouseover', 'mouseout', 'mouseenter', 'mouseleave', 'pointermove', 'pointerover', 'pointerout', 'pointerenter', 'pointerleave', 'wheel', 'scroll', 'touchmove', 'drag', 'dragenter', 'dragleave', 'dragover']);
  const eventPriority = (t) => discrete.has(t) ? K.DiscreteEventPriority : continuous.has(t) ? K.ContinuousEventPriority : K.DefaultEventPriority;
  let updatePriority = K.NoEventPriority;

  const hostTransition = React.createContext(null);
  const cfg = {
    rendererPackageName: 'clack',
    rendererVersion: React.version,
    extraDevToolsConfig: null,
    isPrimaryRenderer: true,
    supportsMutation: true,
    supportsPersistence: false,
    supportsHydration: false,
    supportsMicrotasks: true,
    supportsResources: false,
    supportsSingletons: false,
    supportsTestSelectors: false,
    noTimeout: -1,
    NotPendingTransition: null,
    HostTransitionContext: hostTransition,
    scheduleTimeout: (fn, ms) => setTimeout(fn, ms),
    cancelTimeout: (id) => clearTimeout(id),
    scheduleMicrotask: (fn) => queueMicrotask(fn),
    getRootHostContext: () => rootHost,
    getChildHostContext: (parent, type) => {
      if (type === 'svg') return parent.svg === SVG ? parent : { svg: SVG };
      if (type === 'foreignObject') return rootHost;
      return parent;
    },
    getPublicInstance: (i) => i,
    shouldSetTextContent: (type, props) => {
      if (type[0] === '@' || type === '#slot') return false;
      const c = props.children;
      return textTypes.has(type) || typeof c === 'string' || typeof c === 'number' || typeof c === 'bigint' || (props.dangerouslySetInnerHTML != null && props.dangerouslySetInnerHTML.__html != null);
    },
    createInstance(type, props, root, host, fiber) {
      let el;
      if (type[0] === '@') {
        el = new jr.NativeElement(type.slice(1));
        el._np = props.__np || null;
      } else if (type === '#slot') {
        el = jr.newElement('div', false);
        el._slot = String(props.name);
        return el;
      } else {
        const svg = host.svg === SVG || type === 'svg';
        el = jr.newElement(type, svg);
        applyProps(el, null, props, svg, true);
      }
      el.__reactFiber$clack = fiber;
      el.__reactProps$clack = props;
      return el;
    },
    createTextInstance: (text) => document.createTextNode(text),
    appendInitialChild: (p, c) => p.appendChild(c),
    finalizeInitialChildren: (el, type, props) => !!props.autoFocus && /^(button|input|select|textarea)$/.test(type),
    commitMount: (el, type, props) => { if (props.autoFocus) el.focus(); },
    commitUpdate(el, type, prev, next, fiber) {
      el.__reactProps$clack = next;
      el.__reactFiber$clack = fiber;
      if (type[0] === '@') { el._np = next.__np || null; jr.markDirty(el); return; }
      if (type === '#slot') { el._slot = String(next.name); jr.markDirty(el); return; }
      applyProps(el, prev, next, el._svg, false);
    },
    commitTextUpdate: (t, old, next) => { t.data = next; },
    resetTextContent: (el) => { el.textContent = ''; },
    appendChild: (p, c) => p.appendChild(c),
    appendChildToContainer: (c, child) => c.appendChild(child),
    insertBefore: (p, c, before) => p.insertBefore(c, before),
    insertInContainerBefore: (p, c, before) => p.insertBefore(c, before),
    removeChild: (p, c) => p.removeChild(c),
    removeChildFromContainer: (p, c) => p.removeChild(c),
    clearContainer: (c) => c._removeAll(),
    detachDeletedInstance(node) {
      jr.releaseFns(node);
      node._h = null;
      node.__reactFiber$clack = null;
      node.__reactProps$clack = null;
      if (jr.activeElement() === node) jr.focusElement(null);
    },
    prepareForCommit: () => null,
    resetAfterCommit: (container) => jr.markDirty(container),
    preparePortalMount: () => {},
    hideInstance: (el) => { el._hidden = true; el._styleSet('display', 'none'); },
    unhideInstance: (el, props) => { el._hidden = false; const d = props.style && props.style.display; if (d != null && d !== '') el._styleSet('display', d); else el._styleDel('display'); },
    hideTextInstance: (t) => { t._hiddenText = t.data; t.data = ''; },
    unhideTextInstance: (t, text) => { t.data = text; },
    getInstanceFromNode: (node) => node && node.__reactFiber$clack || null,
    beforeActiveInstanceBlur: () => {},
    afterActiveInstanceBlur: () => {},
    prepareScopeUpdate: () => {},
    getInstanceFromScope: () => null,
    setCurrentUpdatePriority: (p) => { updatePriority = p; },
    getCurrentUpdatePriority: () => updatePriority,
    resolveUpdatePriority: () => updatePriority !== K.NoEventPriority ? updatePriority : curEvent ? eventPriority(curEvent.type) : K.DefaultEventPriority,
    getCurrentEventPriority: () => curEvent ? eventPriority(curEvent.type) : K.DefaultEventPriority,
    resolveEventType: () => curEvent ? curEvent.type : null,
    resolveEventTimeStamp: () => curEvent ? curEvent.timeStamp : -1.1,
    trackSchedulerEvent: () => {},
    shouldAttemptEagerTransition: () => false,
    requestPostPaintCallback: (cb) => requestAnimationFrame((t) => cb(t)),
    maySuspendCommit: () => false,
    maySuspendCommitOnUpdate: () => false,
    maySuspendCommitInSyncRender: () => false,
    mayResourceSuspendCommit: () => false,
    preloadInstance: () => true,
    preloadResource: () => true,
    startSuspendingCommit: () => {},
    suspendInstance: () => {},
    suspendResource: () => {},
    waitForCommitToBeReady: () => null,
    getSuspendedCommitReason: () => null,
    resetFormInstance: (form) => form.reset && form.reset(),
    bindToConsole: (method, args) => Function.prototype.bind.call(console[method] || console.log, console, ...args),
    setFocusIfFocusable: (node) => { if (node && node.focus) { node.focus(); return jr.activeElement() === node; } return false; },
    getBoundingRect: (node) => node.getBoundingClientRect(),
    getTextContent: (node) => node.textContent,
    isHiddenSubtree: () => false,
    matchAccessibilityRole: (node, role) => node.getAttribute && node.getAttribute('role') === role,
    setupIntersectionObserver: (targets, cb) => { const o = new IntersectionObserver(cb); for (const t of targets) o.observe(t); return { disconnect: () => o.disconnect(), observe: (t) => o.observe(t), unobserve: (t) => o.unobserve(t) }; },
    findFiberRoot: () => null,
    isSingletonScope: () => false,
    isHostHoistableType: () => false,
    isHostSingletonType: () => false,
    getHoistableRoot: () => document,
    hasInstanceChanged: () => false,
    hasInstanceAffectedParent: () => false,
    wasInstanceInViewport: () => true,
    measureInstance: (node) => node.getBoundingClientRect(),
    measureClonedInstance: (node) => node.getBoundingClientRect(),
    getCurrentGestureOffset: () => 0,
  };
  // everything else the reconciler reads (hydration, resources, view
  // transitions, fragment refs …) is disabled above; default to no-ops.
  const noop = () => {};
  const config = new Proxy(cfg, { get: (t, k) => (k in t ? t[k] : typeof k === 'string' && /^(supports|is[A-Z])/.test(k) ? false : noop) });
  const R = createReconciler(config);
  jr.reconciler = R;

  const onUncaught = (error, info) => jr.reportError(error);
  const onCaught = (error, info) => { console.error('React caught an error:', error); };
  const onRecoverable = (error) => console.warn('React recoverable error:', error);

  // Wrap Go-dispatched events so React sees their priority and type.
  const dispatch = __clack.dispatch;
  __clack.dispatch = (k, e) => {
    const prev = curEvent;
    curEvent = { type: e.type, timeStamp: performance.now() };
    try { return dispatch(k, e); } finally { curEvent = prev; R.flushSyncWork(); }
  };
  // also when plugins or the DOM dispatch events themselves
  const jrDispatch = jr.dispatch;
  jr.dispatch = (target, ev) => {
    const prev = curEvent;
    curEvent = ev;
    try { return jrDispatch(target, ev); } finally { curEvent = prev; }
  };

  // ---- roots ----
  const rootsById = new Map(); // id → { container, fiberRoot }
  let jsRootN = 0;
  const JS_ROOT = 2 ** 52;
  const ensureRoot = (id) => {
    let r = rootsById.get(id);
    if (!r) {
      const container = jr.newContainer(id);
      const fiberRoot = R.createContainer(container, K.ConcurrentRoot, null, false, null, 'c' + id + '-', onUncaught, onCaught, onRecoverable, null);
      r = { container, fiberRoot };
      rootsById.set(id, r);
    }
    return r;
  };
  __clack.createRoot = () => { jsRootN++; return JS_ROOT + jsRootN; };
  __clack.render = (id, element) => {
    const r = ensureRoot(id);
    R.updateContainerSync(element === undefined ? null : element, r.fiberRoot, null, null);
    R.flushSyncWork();
  };
  __clack.unmount = (id) => {
    if (portalRootK.has(id)) { removePortal(id); return; }
    const r = rootsById.get(id);
    if (!r) return;
    R.updateContainerSync(null, r.fiberRoot, null, null);
    R.flushSyncWork();
    rootsById.delete(id);
    jr.dropContainer(id);
  };
  __clack.rootContainer = (id) => { const r = rootsById.get(id); return r ? r.container : null; };

  // ---- nested roots as portals ----
  // A patched Slack component that a native draws under another root's
  // native host node k (a message row's <Original> drawing MessageText) is
  // mounted as a portal inside that root's tree, at that native, so the
  // contexts and providers around it (plugin patches, Redux views) reach
  // it exactly as in Slack's single tree. Its own container delivers its
  // commits to its own root id.
  const portalsByK = new Map(); // host k → Map(root id → element)
  const portalRootK = new Map(); // root id → host k
  const portalSubs = new Map(); // host k → Set(rerender)
  const portalBoxes = new Map(); // root id → container
  const notifyPortal = (k) => {
    const subs = portalSubs.get(k);
    if (subs && subs.size) flushSync(() => { for (const f of subs) f(); });
  };
  const removePortal = (id) => {
    const k = portalRootK.get(id);
    portalRootK.delete(id);
    const m = portalsByK.get(k);
    if (m) { m.delete(id); if (!m.size) portalsByK.delete(k); }
    notifyPortal(k);
    portalBoxes.delete(id);
    jr.dropContainer(id);
  };
  __clack.portalInto = (k, id, element) => {
    k = Number(k);
    const was = portalRootK.get(id);
    if (was !== undefined && was !== k) removePortal(id);
    if (!portalBoxes.has(id)) portalBoxes.set(id, jr.newContainer(id));
    let m = portalsByK.get(k);
    if (!m) portalsByK.set(k, (m = new Map()));
    m.set(id, element === undefined ? null : element);
    portalRootK.set(id, k);
    notifyPortal(k);
    R.flushSyncWork();
  };
  __clack.portalCount = () => portalRootK.size;

  // ---- native Slack components ----
  const natives = new Map();
  __clack.native = (name) => {
    name = String(name);
    let C = natives.get(name);
    if (C) return C;
    const type = '@' + name;
    C = function (props) {
      const np = {};
      let slots = null;
      for (const k in props) {
        if (k === 'children' || k === 'ref') continue;
        const v = props[k];
        if (isEl(v) || (Array.isArray(v) && v.length && v.some(isEl))) (slots || (slots = [])).push(React.createElement('#slot', { key: '$' + k, name: k }, v));
        else np[k] = v;
      }
      // nested patched roots Go mounted under this native (portalInto)
      const ref = React.useRef(null);
      const [, rerender] = React.useReducer((x) => x + 1, 0);
      React.useLayoutEffect(() => {
        const hk = ref.current && ref.current._k;
        if (!hk) return undefined;
        let subs = portalSubs.get(hk);
        if (!subs) portalSubs.set(hk, (subs = new Set()));
        subs.add(rerender);
        if (portalsByK.has(hk)) rerender();
        return () => { subs.delete(rerender); if (!subs.size) portalSubs.delete(hk); };
      }, []);
      const hk = ref.current && ref.current._k;
      const pm = hk ? portalsByK.get(hk) : null;
      const host = slots ? React.createElement(type, { __np: np, ref }, props.children, slots) : React.createElement(type, { __np: np, ref }, props.children);
      if (!pm) return host;
      const portals = [];
      for (const [id, el] of pm) portals.push(R.createPortal(el, portalBoxes.get(id), null, 'p' + id));
      return React.createElement(React.Fragment, null, host, portals);
    };
    def(C, 'name', name);
    C.displayName = name;
    natives.set(name, C);
    return C;
  };

  // ---- ReactDOM ----
  const domRoots = new WeakMap();
  const domRoot = (container, opts = {}) => {
    let r = domRoots.get(container);
    if (r) return r;
    const fiberRoot = R.createContainer(container, K.ConcurrentRoot, null, !!opts.unstable_strictMode, null, opts.identifierPrefix || '', opts.onUncaughtError || onUncaught, opts.onCaughtError || onCaught, opts.onRecoverableError || onRecoverable, null);
    r = {
      _root: fiberRoot,
      render(children) { R.updateContainer(children, fiberRoot, null, null); },
      unmount() { R.updateContainerSync(null, fiberRoot, null, null); R.flushSyncWork(); domRoots.delete(container); },
    };
    domRoots.set(container, r);
    return r;
  };
  const flushSync = (fn) => {
    const prev = updatePriority;
    try {
      updatePriority = K.DiscreteEventPriority;
      return fn ? fn() : undefined;
    } finally {
      updatePriority = prev;
      R.flushSyncWork();
    }
  };
  const ReactDOM = {
    version: React.version,
    createPortal: (children, container, key) => R.createPortal(children, container, null, key == null ? null : '' + key),
    createRoot: (container, opts) => domRoot(container, opts),
    hydrateRoot: (container, children, opts) => { const r = domRoot(container, opts); r.render(children); return r; },
    flushSync,
    unstable_batchedUpdates: (fn, a) => R.batchedUpdates(fn, a),
    render(element, container, cb) {
      const r = domRoot(container);
      R.updateContainerSync(element, r._root, null, cb || null);
      R.flushSyncWork();
      return R.getPublicRootInstance(r._root);
    },
    unmountComponentAtNode(container) { const r = domRoots.get(container); if (!r) return false; r.unmount(); return true; },
    findDOMNode: (inst) => inst == null ? null : inst.nodeType ? inst : R.findHostInstance(inst),
    preconnect() {}, prefetchDNS() {}, preinit() {}, preinitModule() {}, preload() {}, preloadModule() {},
    requestFormReset() {},
    useFormStatus: () => ({ pending: false, data: null, method: null, action: null }),
    useFormState: (action, init) => React.useActionState(action, init),
  };
  ReactDOM.default = ReactDOM;
  def(G, 'ReactDOM', ReactDOM);
  jr.define('react-dom', (m) => { m.exports = ReactDOM; });
  jr.define('react-dom/client', (m) => { m.exports = ReactDOM; });
})();
