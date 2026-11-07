// dom.js: a small, fast DOM for Taut plugins, mapped onto Clack's host tree.
//
// React (react-host.js) renders into these Elements; plugins may also build
// DOM by hand (document.createElement + document.body.appendChild). Each
// element has a stable id `_k`. Root containers (React roots) and the
// document (body overlays + <style> CSS) are serialized to the JSON host tree
// of docs/PLUGINS.md when they change, at most once per JS task.
(() => {
  'use strict';
  const G = globalThis, jr = __jr, H = jr.host, def = jr.def;
  const kListeners = jr.kListeners, kStop = jr.kStop, kImmediate = jr.kImmediate, kCanceled = jr.kCanceled;

  // ---- ids, dirty tracking ----
  let nextK = 1;
  const kmap = new Map(); // k → WeakRef(node)
  const kfree = new FinalizationRegistry((k) => { const w = kmap.get(k); if (w && !w.deref()) kmap.delete(k); });
  const byK = (k) => { const w = kmap.get(k); return w ? w.deref() : undefined; };
  jr.byK = byK;
  const dirtyRoots = new Set();
  let docDirty = false, flushQueued = false;
  const queueFlush = () => { if (!flushQueued) { flushQueued = true; H.dirty(); } };
  // markDirty walks up to the owning root container or document part
  const markDirty = (node) => {
    for (let n = node; n; n = n.parentNode) {
      n._j = undefined;
      if (n._rootId !== undefined) { dirtyRoots.add(n); queueFlush(); return; }
      if (n === docBody || n === docHead || n === docHtml) { docDirty = true; queueFlush(); return; }
    }
  };
  jr.markDirty = markDirty;

  // ---- mutation observers (zero cost while none exist) ----
  let moCount = 0;
  const moRecord = (target, rec) => {
    if (!moCount) return;
    for (let n = target, direct = true; n; n = n.parentNode, direct = false) {
      const regs = n._mo;
      if (!regs) continue;
      for (const r of regs) {
        const o = r.options;
        if (!direct && !o.subtree) continue;
        if (rec.type === 'childList' && !o.childList) continue;
        if (rec.type === 'attributes') {
          if (!o.attributes) continue;
          if (o.attributeFilter && !o.attributeFilter.includes(rec.attributeName)) continue;
        }
        if (rec.type === 'characterData' && !o.characterData) continue;
        const copy = Object.assign({}, rec);
        if (!(rec.type === 'attributes' && o.attributeOldValue) && !(rec.type === 'characterData' && o.characterDataOldValue)) copy.oldValue = null;
        r.observer._queue(copy);
      }
    }
  };
  const emptyList = Object.freeze([]);

  // ---- name mapping: DOM attribute ↔ React prop ----
  const attrToProp = {
    class: 'className', for: 'htmlFor', tabindex: 'tabIndex', readonly: 'readOnly', maxlength: 'maxLength', minlength: 'minLength',
    colspan: 'colSpan', rowspan: 'rowSpan', contenteditable: 'contentEditable', spellcheck: 'spellCheck', autocomplete: 'autoComplete',
    autofocus: 'autoFocus', crossorigin: 'crossOrigin', srcset: 'srcSet', enterkeyhint: 'enterKeyHint', inputmode: 'inputMode',
    accesskey: 'accessKey', autocapitalize: 'autoCapitalize', 'http-equiv': 'httpEquiv', referrerpolicy: 'referrerPolicy',
    novalidate: 'noValidate', formaction: 'formAction', datetime: 'dateTime', frameborder: 'frameBorder', allowfullscreen: 'allowFullScreen',
    'accept-charset': 'acceptCharset', cellpadding: 'cellPadding', cellspacing: 'cellSpacing', usemap: 'useMap', 'xlink:href': 'xlinkHref',
    'xml:space': 'xmlSpace', 'xmlns:xlink': 'xmlnsXlink',
  };
  const propToAttr = {};
  for (const [a, p] of Object.entries(attrToProp)) propToAttr[p] = a;
  const camel = (s) => s.replace(/-([a-z])/g, (_, c) => c.toUpperCase());
  const kebab = (s) => s.replace(/[A-Z]/g, (c) => '-' + c.toLowerCase());
  // SVG presentation attributes are hyphenated in the DOM and camelCase in React
  const svgHyphen = new Set(['accent-height', 'alignment-baseline', 'arabic-form', 'baseline-shift', 'cap-height', 'clip-path', 'clip-rule', 'color-interpolation', 'color-interpolation-filters', 'color-profile', 'color-rendering', 'dominant-baseline', 'enable-background', 'fill-opacity', 'fill-rule', 'flood-color', 'flood-opacity', 'font-family', 'font-size', 'font-size-adjust', 'font-stretch', 'font-style', 'font-variant', 'font-weight', 'glyph-name', 'glyph-orientation-horizontal', 'glyph-orientation-vertical', 'horiz-adv-x', 'horiz-origin-x', 'image-rendering', 'letter-spacing', 'lighting-color', 'marker-end', 'marker-mid', 'marker-start', 'overline-position', 'overline-thickness', 'paint-order', 'panose-1', 'pointer-events', 'rendering-intent', 'shape-rendering', 'stop-color', 'stop-opacity', 'strikethrough-position', 'strikethrough-thickness', 'stroke-dasharray', 'stroke-dashoffset', 'stroke-linecap', 'stroke-linejoin', 'stroke-miterlimit', 'stroke-opacity', 'stroke-width', 'text-anchor', 'text-decoration', 'text-rendering', 'underline-position', 'underline-thickness', 'unicode-bidi', 'unicode-range', 'units-per-em', 'v-alphabetic', 'v-hanging', 'v-ideographic', 'v-mathematical', 'vector-effect', 'vert-adv-y', 'vert-origin-x', 'vert-origin-y', 'word-spacing', 'writing-mode', 'x-height']);
  // React prop name → DOM attribute name
  const reactToAttr = (name, svg) => {
    const a = propToAttr[name];
    if (a) return a;
    if (svg) { const h = kebab(name); if (svgHyphen.has(h)) return h; return name; }
    return name.toLowerCase() === name || name.startsWith('data-') || name.startsWith('aria-') ? name : name.toLowerCase();
  };
  jr.reactToAttr = reactToAttr;
  // DOM attribute name → React prop name (for serialization)
  const attrName = (a, svg) => {
    const p = attrToProp[a];
    if (p) return p;
    if (svg && svgHyphen.has(a)) return camel(a);
    return a;
  };

  // ---- CSS style declarations ----
  const unitless = new Set(['animationIterationCount', 'aspectRatio', 'borderImageOutset', 'borderImageSlice', 'borderImageWidth', 'boxFlex', 'boxFlexGroup', 'boxOrdinalGroup', 'columnCount', 'columns', 'flex', 'flexGrow', 'flexPositive', 'flexShrink', 'flexNegative', 'flexOrder', 'gridArea', 'gridRow', 'gridRowEnd', 'gridRowSpan', 'gridRowStart', 'gridColumn', 'gridColumnEnd', 'gridColumnSpan', 'gridColumnStart', 'fontWeight', 'lineClamp', 'lineHeight', 'opacity', 'order', 'orphans', 'scale', 'tabSize', 'widows', 'zIndex', 'zoom', 'fillOpacity', 'floodOpacity', 'stopOpacity', 'strokeDasharray', 'strokeDashoffset', 'strokeMiterlimit', 'strokeOpacity', 'strokeWidth']);
  const cssName = (p) => p.startsWith('--') ? p : p.startsWith('ms') ? '-' + kebab(p) : /^(webkit|moz|o)[A-Z]/.test(p) ? '-' + kebab(p) : kebab(p);
  const propName = (css) => css.startsWith('--') ? css : camel(css.replace(/^-ms-/, 'ms-').replace(/^-(webkit|moz|o)-/, '$1-'));
  const cssValue = (p, v) => {
    if (v == null || typeof v === 'boolean' || v === '') return '';
    if (typeof v === 'number' && v !== 0 && !unitless.has(p) && !p.startsWith('--')) return v + 'px';
    return String(v).trim();
  };
  const declMethods = {
    setProperty(name, value, priority) {
      const p = propName(String(name));
      if (value == null || value === '') return this._owner._styleDel(p);
      this._owner._styleSet(p, String(value) + (priority === 'important' ? ' !important' : ''));
    },
    getPropertyValue(name) { const p = propName(String(name)); return cssValue(p, this._s()[p]).replace(/ !important$/, ''); },
    getPropertyPriority(name) { return / !important$/.test(String(this._s()[propName(String(name))] || '')) ? 'important' : ''; },
    removeProperty(name) { const p = propName(String(name)); const old = this.getPropertyValue(name); this._owner._styleDel(p); return old; },
    item(i) { const k = Object.keys(this._s())[i]; return k ? cssName(k) : ''; },
  };
  const parseCssText = (text) => {
    const out = {};
    for (const decl of String(text).split(';')) {
      const i = decl.indexOf(':');
      if (i < 0) continue;
      const n = decl.slice(0, i).trim(), v = decl.slice(i + 1).trim();
      if (n && v) out[propName(n.startsWith('--') ? n : n.toLowerCase())] = v;
    }
    return out;
  };
  const serializeCss = (s) => Object.keys(s).map((p) => cssName(p) + ': ' + cssValue(p, s[p]) + ';').join(' ');
  const makeStyle = (owner) => {
    const target = { _owner: owner, _s: () => owner._st || emptyObj };
    return new Proxy(target, {
      get(t, k) {
        if (typeof k === 'symbol') return undefined;
        if (k in declMethods) return declMethods[k];
        if (k === 'cssText') return serializeCss(t._s());
        if (k === 'length') return Object.keys(t._s()).length;
        if (k === 'parentRule') return null;
        if (k === '_owner' || k === '_s') return t[k];
        if (k === 'cssFloat') k = 'float';
        if (/^\d+$/.test(k)) return declMethods.item.call(this.receiver || t, +k);
        return cssValue(k, t._s()[k]).replace(/ !important$/, '');
      },
      set(t, k, v) {
        if (typeof k === 'symbol') return true;
        if (k === 'cssText') { owner._styleReplace(parseCssText(v)); return true; }
        if (k === 'cssFloat') k = 'float';
        if (v == null || v === '') owner._styleDel(k);
        else owner._styleSet(k, typeof v === 'number' ? v : String(v));
        return true;
      },
      has(t, k) { return typeof k === 'string' && (k in declMethods || k in t._s() || /^[a-zA-Z-]+$/.test(k)); },
      ownKeys(t) { return Object.keys(t._s()); },
      getOwnPropertyDescriptor(t, k) { const s = t._s(); return k in s ? { value: cssValue(k, s[k]), enumerable: true, configurable: true, writable: true } : undefined; },
      deleteProperty(t, k) { owner._styleDel(k); return true; },
    });
  };
  const emptyObj = Object.freeze({});

  // ---- DOMTokenList (classList) ----
  class DOMTokenList {
    constructor(el, attr) { this._el = el; this._attr = attr; }
    _get() { const v = this._el.getAttribute(this._attr); return v ? v.split(/\s+/).filter(Boolean) : []; }
    _set(list) { this._el.setAttribute(this._attr, list.join(' ')); }
    get length() { return this._get().length; }
    get value() { return this._el.getAttribute(this._attr) || ''; }
    set value(v) { this._el.setAttribute(this._attr, v); }
    item(i) { return this._get()[i] ?? null; }
    contains(t) { return this._get().includes(String(t)); }
    add(...ts) { const l = this._get(); let ch = false; for (const t of ts) { const s = String(t); if (!s || /\s/.test(s)) throw new DOMException('The token provided contains HTML space characters, which are not valid in tokens.', 'InvalidCharacterError'); if (!l.includes(s)) { l.push(s); ch = true; } } if (ch || !this._el.hasAttribute(this._attr)) this._set(l); }
    remove(...ts) { const l = this._get(); const n = l.filter((x) => !ts.map(String).includes(x)); if (n.length !== l.length) this._set(n); }
    toggle(t, force) {
      const s = String(t), has = this.contains(s);
      if (has && force !== true) { this.remove(s); return false; }
      if (!has && force !== false) { this.add(s); return true; }
      return has;
    }
    replace(a, b) { const l = this._get(); const i = l.indexOf(String(a)); if (i < 0) return false; l[i] = String(b); this._set([...new Set(l)]); return true; }
    supports() { return true; }
    forEach(fn, thisArg) { this._get().forEach((v, i) => fn.call(thisArg, v, i, this)); }
    entries() { return this._get().entries(); }
    keys() { return this._get().keys(); }
    values() { return this._get().values(); }
    [Symbol.iterator]() { return this._get()[Symbol.iterator](); }
    toString() { return this.value; }
  }

  // node lists: arrays with item()
  const nodeList = (arr) => { def(arr, 'item', (i) => arr[i] ?? null); def(arr, 'namedItem', (n) => arr.find((e) => e.id === n || e.getAttribute && e.getAttribute('name') === n) ?? null); return arr; };

  // ---- Node ----
  const ELEMENT_NODE = 1, TEXT_NODE = 3, COMMENT_NODE = 8, DOCUMENT_NODE = 9, DOCUMENT_FRAGMENT_NODE = 11;
  class Node extends EventTarget {
    constructor() {
      super();
      this.parentNode = null;
      this._c = null; // children array (lazily created)
      this._k = nextK++;
    }
    get childNodes() { return nodeList(this._c ? this._c.slice() : []); }
    get firstChild() { return this._c && this._c.length ? this._c[0] : null; }
    get lastChild() { return this._c && this._c.length ? this._c[this._c.length - 1] : null; }
    get parentElement() { const p = this.parentNode; return p && p.nodeType === ELEMENT_NODE ? p : null; }
    get nextSibling() { const p = this.parentNode; if (!p) return null; const c = p._c, i = c.indexOf(this); return c[i + 1] ?? null; }
    get previousSibling() { const p = this.parentNode; if (!p) return null; const c = p._c, i = c.indexOf(this); return i > 0 ? c[i - 1] : null; }
    get ownerDocument() { return this.nodeType === DOCUMENT_NODE ? null : document; }
    get isConnected() { for (let n = this; n; n = n.parentNode) { if (n === document || n._rootId !== undefined) return true; } return false; }
    getRootNode() { let n = this; while (n.parentNode) n = n.parentNode; return n._rootId !== undefined ? document : n; }
    hasChildNodes() { return !!(this._c && this._c.length); }
    contains(other) { for (let n = other; n; n = n.parentNode) if (n === this) return true; return false; }
    get textContent() {
      if (this.nodeType === TEXT_NODE || this.nodeType === COMMENT_NODE) return this._v;
      let s = '';
      const walk = (n) => { if (!n._c) return; for (const c of n._c) { if (c.nodeType === TEXT_NODE) s += c._v; else if (c.nodeType === ELEMENT_NODE) walk(c); } };
      walk(this);
      return s;
    }
    set textContent(v) {
      if (this.nodeType === TEXT_NODE || this.nodeType === COMMENT_NODE) { this.data = v; return; }
      this._removeAll();
      const s = v == null ? '' : String(v);
      if (s) this.appendChild(document.createTextNode(s));
    }
    get nodeValue() { return this.nodeType === TEXT_NODE || this.nodeType === COMMENT_NODE ? this._v : null; }
    set nodeValue(v) { if (this.nodeType === TEXT_NODE || this.nodeType === COMMENT_NODE) this.data = v; }
    _check(child) {
      if (!(child instanceof Node)) throw new TypeError("Failed to execute 'appendChild' on 'Node': parameter 1 is not of type 'Node'.");
      if (child.contains(this)) throw new DOMException("Failed to execute 'appendChild' on 'Node': The new child element contains the parent.", 'HierarchyRequestError');
    }
    _detach(child) {
      const p = child.parentNode;
      if (!p) return;
      const i = p._c.indexOf(child);
      if (i >= 0) p._c.splice(i, 1);
      child.parentNode = null;
      markDirty(p);
      if (moCount) moRecord(p, { type: 'childList', target: p, addedNodes: emptyList, removedNodes: [child], previousSibling: p._c[i - 1] ?? null, nextSibling: p._c[i] ?? null, attributeName: null, oldValue: null });
      if (child._onDisconnect) child._onDisconnect();
    }
    insertBefore(child, ref) {
      if (ref === undefined) ref = null;
      if (child.nodeType === DOCUMENT_FRAGMENT_NODE) {
        for (const c of child._c ? child._c.slice() : []) this.insertBefore(c, ref);
        return child;
      }
      this._check(child);
      if (ref && ref.parentNode !== this) throw new DOMException("Failed to execute 'insertBefore' on 'Node': The node before which the new node is to be inserted is not a child of this node.", 'NotFoundError');
      if (child === ref) return child;
      this._detach(child);
      const c = this._c || (this._c = []);
      const i = ref ? c.indexOf(ref) : c.length;
      c.splice(i, 0, child);
      child.parentNode = this;
      markDirty(this);
      if (moCount) moRecord(this, { type: 'childList', target: this, addedNodes: [child], removedNodes: emptyList, previousSibling: c[i - 1] ?? null, nextSibling: c[i + 1] ?? null, attributeName: null, oldValue: null });
      if (child._onConnect && child.isConnected) child._onConnect();
      return child;
    }
    appendChild(child) { return this.insertBefore(child, null); }
    removeChild(child) {
      if (!child || child.parentNode !== this) throw new DOMException("Failed to execute 'removeChild' on 'Node': The node to be removed is not a child of this node.", 'NotFoundError');
      this._detach(child);
      return child;
    }
    replaceChild(nc, old) { this.insertBefore(nc, old); this.removeChild(old); return old; }
    _removeAll() { if (this._c) for (const c of this._c.slice()) this._detach(c); }
    cloneNode(deep) {
      let n;
      if (this.nodeType === TEXT_NODE) n = document.createTextNode(this._v);
      else if (this.nodeType === COMMENT_NODE) n = document.createComment(this._v);
      else if (this.nodeType === DOCUMENT_FRAGMENT_NODE) n = document.createDocumentFragment();
      else {
        n = this._svg ? document.createElementNS(SVG_NS, this.localName) : document.createElement(this.localName);
        if (this._a) n._a = new Map(this._a);
        if (this._st) n._st = Object.assign({}, this._st);
        if (this._val !== undefined) n._val = this._val;
        if (this._chk !== undefined) n._chk = this._chk;
      }
      if (deep && this._c) for (const c of this._c) n.appendChild(c.cloneNode(true));
      return n;
    }
    isEqualNode(o) { return !!o && o.nodeType === this.nodeType && (this.outerHTML ?? this.textContent) === (o.outerHTML ?? o.textContent); }
    isSameNode(o) { return o === this; }
    compareDocumentPosition(o) {
      if (o === this) return 0;
      if (this.contains(o)) return 20; // CONTAINED_BY | FOLLOWING
      if (o.contains(this)) return 10; // CONTAINS | PRECEDING
      const pa = [], pb = [];
      for (let n = this; n; n = n.parentNode) pa.unshift(n);
      for (let n = o; n; n = n.parentNode) pb.unshift(n);
      if (pa[0] !== pb[0]) return 1 | 32 | 2;
      let i = 0;
      while (pa[i] === pb[i]) i++;
      const c = pa[i - 1]._c;
      return c.indexOf(pa[i]) < c.indexOf(pb[i]) ? 4 : 2;
    }
    normalize() {}
    get baseURI() { return location.href; }
    // EventTarget path parent
    _parentForEvents() { return this.parentNode || (this._rootId !== undefined ? document : null); }
  }
  for (const [k, v] of Object.entries({ ELEMENT_NODE, ATTRIBUTE_NODE: 2, TEXT_NODE, CDATA_SECTION_NODE: 4, PROCESSING_INSTRUCTION_NODE: 7, COMMENT_NODE, DOCUMENT_NODE, DOCUMENT_TYPE_NODE: 10, DOCUMENT_FRAGMENT_NODE, DOCUMENT_POSITION_DISCONNECTED: 1, DOCUMENT_POSITION_PRECEDING: 2, DOCUMENT_POSITION_FOLLOWING: 4, DOCUMENT_POSITION_CONTAINS: 8, DOCUMENT_POSITION_CONTAINED_BY: 16, DOCUMENT_POSITION_IMPLEMENTATION_SPECIFIC: 32 })) {
    def(Node, k, v);
    def(Node.prototype, k, v);
  }

  class CharacterData extends Node {
    constructor(v) { super(); this._v = String(v); }
    get data() { return this._v; }
    set data(v) {
      const old = this._v;
      this._v = v == null ? '' : String(v);
      if (this.parentNode) markDirty(this.parentNode);
      if (moCount) moRecord(this, { type: 'characterData', target: this, addedNodes: emptyList, removedNodes: emptyList, previousSibling: null, nextSibling: null, attributeName: null, oldValue: old });
    }
    get length() { return this._v.length; }
    appendData(s) { this.data = this._v + s; }
    deleteData(o, n) { this.data = this._v.slice(0, o) + this._v.slice(o + n); }
    insertData(o, s) { this.data = this._v.slice(0, o) + s + this._v.slice(o); }
    replaceData(o, n, s) { this.data = this._v.slice(0, o) + s + this._v.slice(o + n); }
    substringData(o, n) { return this._v.substr(o, n); }
    remove() { if (this.parentNode) this.parentNode.removeChild(this); }
    before(...n) { insertNodes(this.parentNode, this, n); }
    after(...n) { insertNodes(this.parentNode, this.nextSibling, n); }
    replaceWith(...n) { const p = this.parentNode; if (!p) return; insertNodes(p, this, n); p.removeChild(this); }
    get nextElementSibling() { let n = this.nextSibling; while (n && n.nodeType !== ELEMENT_NODE) n = n.nextSibling; return n; }
    get previousElementSibling() { let n = this.previousSibling; while (n && n.nodeType !== ELEMENT_NODE) n = n.previousSibling; return n; }
  }
  class Text extends CharacterData {
    get nodeType() { return TEXT_NODE; }
    get nodeName() { return '#text'; }
    get wholeText() { return this._v; }
    splitText(o) { const t = document.createTextNode(this._v.slice(o)); this.data = this._v.slice(0, o); if (this.parentNode) this.parentNode.insertBefore(t, this.nextSibling); return t; }
  }
  class Comment extends CharacterData {
    get nodeType() { return COMMENT_NODE; }
    get nodeName() { return '#comment'; }
  }
  const toNodes = (items) => items.map((x) => x instanceof Node ? x : document.createTextNode(String(x)));
  const insertNodes = (parent, ref, items) => { if (!parent) return; for (const n of toNodes(items)) parent.insertBefore(n, ref); };

  class DocumentFragment extends Node {
    get nodeType() { return DOCUMENT_FRAGMENT_NODE; }
    get nodeName() { return '#document-fragment'; }
    get children() { return nodeList(this._c ? this._c.filter((c) => c.nodeType === ELEMENT_NODE) : []); }
    get childElementCount() { return this.children.length; }
    get firstElementChild() { return this.children[0] ?? null; }
    get lastElementChild() { const c = this.children; return c[c.length - 1] ?? null; }
    append(...n) { insertNodes(this, null, n); }
    prepend(...n) { insertNodes(this, this.firstChild, n); }
    replaceChildren(...n) { this._removeAll(); insertNodes(this, null, n); }
    querySelector(s) { return qs(this, s, true)[0] ?? null; }
    querySelectorAll(s) { return nodeList(qs(this, s, false)); }
    getElementById(id) { return qs(this, '#' + CSS.escape(id), true)[0] ?? null; }
  }

  // ---- Element ----
  const SVG_NS = 'http://www.w3.org/2000/svg', HTML_NS = 'http://www.w3.org/1999/xhtml';
  const boolAttrs = new Set(['disabled', 'checked', 'selected', 'hidden', 'readonly', 'required', 'multiple', 'autofocus', 'open', 'novalidate', 'autoplay', 'controls', 'loop', 'muted', 'default', 'reversed', 'async', 'defer', 'inert', 'allowfullscreen', 'playsinline', 'itemscope', 'formnovalidate', 'nomodule', 'draggable']);
  class Element extends Node {
    constructor(tag, svg) {
      super();
      this.localName = tag;
      this._svg = !!svg;
      this._a = null; // Map attr → raw value
      this._st = null; // style object (camelCase → raw)
      this._h = null; // React handlers: lowercased name ('click', 'clickcapture') → fn
      this._r = null; // [x, y, w, h] from Go layout
    }
    get nodeType() { return ELEMENT_NODE; }
    get tagName() { return this._svg || this.localName.startsWith('@') ? this.localName : this.localName.toUpperCase(); }
    get nodeName() { return this.tagName; }
    get namespaceURI() { return this._svg ? SVG_NS : HTML_NS; }
    get prefix() { return null; }
    // attributes
    getAttribute(n) {
      if (!this._a) return null;
      n = this._svg ? String(n) : String(n).toLowerCase();
      if (!this._a.has(n)) return null;
      const v = this._a.get(n);
      return v === true ? '' : String(v);
    }
    getAttributeNS(ns, n) { return this.getAttribute(n); }
    hasAttribute(n) { return !!this._a && this._a.has(this._svg ? String(n) : String(n).toLowerCase()); }
    hasAttributes() { return !!this._a && this._a.size > 0; }
    getAttributeNames() { return this._a ? [...this._a.keys()] : []; }
    setAttribute(n, v) {
      n = this._svg ? String(n) : String(n).toLowerCase();
      if (n === 'style') { this._styleReplace(parseCssText(v)); return; }
      this._setAttr(n, String(v));
    }
    setAttributeNS(ns, n, v) { this.setAttribute(n, v); }
    removeAttribute(n) {
      n = this._svg ? String(n) : String(n).toLowerCase();
      if (n === 'style') { this._styleReplace(null); return; }
      if (!this._a || !this._a.has(n)) return;
      const old = this.getAttribute(n);
      this._a.delete(n);
      this._attrChanged(n, old);
    }
    removeAttributeNS(ns, n) { this.removeAttribute(n); }
    toggleAttribute(n, force) {
      const has = this.hasAttribute(n);
      if (has && force !== true) { this.removeAttribute(n); return false; }
      if (!has && force !== false) { this.setAttribute(n, ''); return true; }
      return has;
    }
    get attributes() {
      const list = this.getAttributeNames().map((name) => ({ name, localName: name, value: this.getAttribute(name), namespaceURI: null, prefix: null, specified: true, ownerElement: this }));
      if (this._st && Object.keys(this._st).length) list.push({ name: 'style', localName: 'style', value: serializeCss(this._st), ownerElement: this });
      def(list, 'getNamedItem', (n) => list.find((a) => a.name === n) ?? null);
      def(list, 'item', (i) => list[i] ?? null);
      return list;
    }
    // raw set (React host config passes raw values: numbers, booleans)
    _setAttr(n, v) {
      const a = this._a || (this._a = new Map());
      const old = moCount ? this.getAttribute(n) : null;
      if (a.get(n) === v) return;
      a.set(n, v);
      this._attrChanged(n, old);
    }
    _attrChanged(n, old) {
      markDirty(this);
      if (moCount) moRecord(this, { type: 'attributes', target: this, attributeName: n, attributeNamespace: null, oldValue: old, addedNodes: emptyList, removedNodes: emptyList, previousSibling: null, nextSibling: null });
    }
    // style
    get style() { return this._style || (this._style = makeStyle(this)); }
    set style(v) { if (typeof v === 'string') this.style.cssText = v; }
    _styleSet(p, v) {
      const s = this._st || (this._st = {});
      if (s[p] === v) return;
      const old = moCount ? serializeCss(s) : null;
      s[p] = v;
      this._attrChanged('style', old);
    }
    _styleDel(p) {
      if (!this._st || !(p in this._st)) return;
      const old = moCount ? serializeCss(this._st) : null;
      delete this._st[p];
      this._attrChanged('style', old);
    }
    _styleReplace(obj) {
      const old = moCount && this._st ? serializeCss(this._st) : null;
      this._st = obj && Object.keys(obj).length ? obj : null;
      this._attrChanged('style', old);
    }
    // reflected attributes
    get id() { return this.getAttribute('id') ?? ''; }
    set id(v) { this.setAttribute('id', v); }
    get className() { return this.getAttribute('class') ?? ''; }
    set className(v) { this.setAttribute('class', v); }
    get classList() { return this._cl || (this._cl = new DOMTokenList(this, 'class')); }
    get dataset() {
      return this._ds || (this._ds = new Proxy({}, {
        get: (t, k) => typeof k === 'string' ? this.getAttribute('data-' + kebab(k)) ?? undefined : undefined,
        set: (t, k, v) => { this.setAttribute('data-' + kebab(String(k)), v); return true; },
        deleteProperty: (t, k) => { this.removeAttribute('data-' + kebab(String(k))); return true; },
        has: (t, k) => this.hasAttribute('data-' + kebab(String(k))),
        ownKeys: () => this.getAttributeNames().filter((n) => n.startsWith('data-')).map((n) => camel(n.slice(5))),
        getOwnPropertyDescriptor: (t, k) => this.hasAttribute('data-' + kebab(String(k))) ? { value: this.getAttribute('data-' + kebab(String(k))), enumerable: true, configurable: true, writable: true } : undefined,
      }));
    }
    get title() { return this.getAttribute('title') ?? ''; }
    set title(v) { this.setAttribute('title', v); }
    get lang() { return this.getAttribute('lang') ?? ''; }
    set lang(v) { this.setAttribute('lang', v); }
    get dir() { return this.getAttribute('dir') ?? ''; }
    set dir(v) { this.setAttribute('dir', v); }
    get hidden() { return this.hasAttribute('hidden'); }
    set hidden(v) { if (v) this._setAttr('hidden', true); else this.removeAttribute('hidden'); }
    get tabIndex() { const v = this.getAttribute('tabindex'); return v == null ? (/^(a|button|input|select|textarea)$/.test(this.localName) ? 0 : -1) : parseInt(v) || 0; }
    set tabIndex(v) { this._setAttr('tabindex', Number(v)); }
    get role() { return this.getAttribute('role'); }
    set role(v) { this.setAttribute('role', v); }
    get slot() { return this.getAttribute('slot') ?? ''; }
    get draggable() { return this.getAttribute('draggable') === 'true'; }
    set draggable(v) { this.setAttribute('draggable', v ? 'true' : 'false'); }
    get contentEditable() { return this.getAttribute('contenteditable') ?? 'inherit'; }
    set contentEditable(v) { this.setAttribute('contenteditable', v); }
    get isContentEditable() { for (let n = this; n && n.getAttribute; n = n.parentNode) { const v = n.getAttribute('contenteditable'); if (v != null) return v !== 'false'; } return false; }
    get inert() { return this.hasAttribute('inert'); }
    set inert(v) { this.toggleAttribute('inert', !!v); }
    // tree helpers
    get children() { return nodeList(this._c ? this._c.filter((c) => c.nodeType === ELEMENT_NODE) : []); }
    get childElementCount() { let n = 0; if (this._c) for (const c of this._c) if (c.nodeType === ELEMENT_NODE) n++; return n; }
    get firstElementChild() { if (this._c) for (const c of this._c) if (c.nodeType === ELEMENT_NODE) return c; return null; }
    get lastElementChild() { if (this._c) for (let i = this._c.length - 1; i >= 0; i--) if (this._c[i].nodeType === ELEMENT_NODE) return this._c[i]; return null; }
    get nextElementSibling() { let n = this.nextSibling; while (n && n.nodeType !== ELEMENT_NODE) n = n.nextSibling; return n; }
    get previousElementSibling() { let n = this.previousSibling; while (n && n.nodeType !== ELEMENT_NODE) n = n.previousSibling; return n; }
    append(...n) { insertNodes(this, null, n); }
    prepend(...n) { insertNodes(this, this.firstChild, n); }
    before(...n) { insertNodes(this.parentNode, this, n); }
    after(...n) { insertNodes(this.parentNode, this.nextSibling, n); }
    replaceWith(...n) { const p = this.parentNode; if (!p) return; insertNodes(p, this, n); p.removeChild(this); }
    replaceChildren(...n) { this._removeAll(); insertNodes(this, null, n); }
    remove() { if (this.parentNode) this.parentNode.removeChild(this); }
    insertAdjacentElement(pos, el) {
      switch (String(pos).toLowerCase()) {
        case 'beforebegin': if (!this.parentNode) return null; this.parentNode.insertBefore(el, this); break;
        case 'afterbegin': this.insertBefore(el, this.firstChild); break;
        case 'beforeend': this.appendChild(el); break;
        case 'afterend': if (!this.parentNode) return null; this.parentNode.insertBefore(el, this.nextSibling); break;
        default: throw new DOMException("The value provided ('" + pos + "') is not one of 'beforeBegin', 'afterBegin', 'beforeEnd', or 'afterEnd'.", 'SyntaxError');
      }
      return el;
    }
    insertAdjacentText(pos, text) { this.insertAdjacentElement(pos, document.createTextNode(text)); }
    insertAdjacentHTML(pos, html) { const f = parseHTML(String(html), this._svg); for (const c of f._c ? f._c.slice() : []) this.insertAdjacentElement(pos, c); }
    get innerHTML() { return this._c ? this._c.map(serializeHTML).join('') : ''; }
    set innerHTML(html) { this._removeAll(); const f = parseHTML(String(html ?? ''), this._svg); this.appendChild(f); }
    get outerHTML() { return serializeHTML(this); }
    set outerHTML(html) { const p = this.parentNode; if (!p) return; const f = parseHTML(String(html), this._svg); p.insertBefore(f, this); p.removeChild(this); }
    get innerText() { return this.textContent; }
    set innerText(v) { this.textContent = v; }
    get outerText() { return this.textContent; }
    // selectors
    matches(s) { return compile(s)(this, this); }
    webkitMatchesSelector(s) { return this.matches(s); }
    closest(s) { const m = compile(s); for (let n = this; n && n.nodeType === ELEMENT_NODE; n = n.parentNode) if (m(n, this)) return n; return null; }
    querySelector(s) { return qs(this, s, true)[0] ?? null; }
    querySelectorAll(s) { return nodeList(qs(this, s, false)); }
    getElementsByClassName(c) { const sel = String(c).trim().split(/\s+/).filter(Boolean).map((x) => '.' + CSS.escape(x)).join(''); return sel ? this.querySelectorAll(sel) : nodeList([]); }
    getElementsByTagName(t) { return this.querySelectorAll(t === '*' ? '*' : CSS.escape(t)); }
    getElementsByTagNameNS(ns, t) { return this.getElementsByTagName(t); }
    // geometry (from Go layout via SetRects / event rects)
    getBoundingClientRect() { const r = this._r; return r ? new DOMRect(r[0], r[1], r[2], r[3]) : new DOMRect(0, 0, 0, 0); }
    getClientRects() { const r = this.getBoundingClientRect(); return r.width || r.height ? [r] : []; }
    get offsetWidth() { return this._r ? Math.round(this._r[2]) : 0; }
    get offsetHeight() { return this._r ? Math.round(this._r[3]) : 0; }
    get offsetLeft() { const p = this.offsetParent; return this._r ? Math.round(this._r[0] - (p && p._r ? p._r[0] : 0)) : 0; }
    get offsetTop() { const p = this.offsetParent; return this._r ? Math.round(this._r[1] - (p && p._r ? p._r[1] : 0)) : 0; }
    get offsetParent() { return this.parentElement; }
    get clientWidth() { return this._r ? Math.round(this._r[2]) : 0; }
    get clientHeight() { return this._r ? Math.round(this._r[3]) : 0; }
    get clientTop() { return 0; }
    get clientLeft() { return 0; }
    get scrollWidth() { return this.clientWidth; }
    get scrollHeight() { return this.clientHeight; }
    scrollTo(x, y) { if (typeof x === 'object' && x) { y = x.top; x = x.left; } if (x != null) this.scrollLeft = +x || 0; if (y != null) this.scrollTop = +y || 0; }
    scroll(x, y) { this.scrollTo(x, y); }
    scrollBy(x, y) { if (typeof x === 'object' && x) { y = x.top; x = x.left; } this.scrollTo(this.scrollLeft + (+x || 0), this.scrollTop + (+y || 0)); }
    scrollIntoView() {}
    scrollIntoViewIfNeeded() {}
    // focus
    focus(opts) { focusElement(this); }
    blur() { if (document.activeElement === this) focusElement(null); }
    click() {
      if (this.disabled && /^(button|input|select|textarea)$/.test(this.localName)) return;
      const ev = makeEvent('click', { bubbles: true, cancelable: true, composed: true, button: 0, view: G });
      if (this.dispatchEvent(ev)) defaultAction(this, ev); // activation behavior, like el.click() in browsers
    }
    animate(keyframes, opts) { return new Animation(this, keyframes, opts); }
    getAnimations() { return []; }
    attachShadow() { const f = document.createDocumentFragment(); def(this, 'shadowRoot', f); f.host = this; return f; }
    requestFullscreen() { return Promise.reject(new DOMException('Fullscreen is not supported', 'NotSupportedError')); }
    requestPointerLock() {}
    setPointerCapture() {}
    releasePointerCapture() {}
    hasPointerCapture() { return false; }
    checkVisibility() { return true; }
    computedStyleMap() { return new Map(); }
  }

  // form-ish elements share value / checked / selection state
  class HTMLElement extends Element {
    get accessKey() { return this.getAttribute('accesskey') ?? ''; }
    get offsetParent() { return this.parentElement; }
    get spellcheck() { return this.getAttribute('spellcheck') !== 'false'; }
    set spellcheck(v) { this.setAttribute('spellcheck', v ? 'true' : 'false'); }
    // value-bearing elements
    get value() {
      if (this._val !== undefined) return this._val;
      if (this.localName === 'textarea') return this.textContent;
      if (this.localName === 'select') { const o = this.querySelector('option[selected]') || this.querySelector('option'); return o ? o.value : ''; }
      if (this.localName === 'option') return this.getAttribute('value') ?? this.textContent;
      const v = this.getAttribute('value');
      if (v != null) return v;
      return this.localName === 'input' && (this.type === 'checkbox' || this.type === 'radio') ? 'on' : '';
    }
    set value(v) {
      v = v == null ? '' : String(v);
      if (this.localName === 'option' || this.localName === 'button' || this.localName === 'li' || this.localName === 'data' || this.localName === 'param' || this.localName === 'meter' || this.localName === 'progress') { this.setAttribute('value', v); return; }
      if (this._val === v) return;
      this._val = v;
      this._selStart = this._selEnd = v.length;
      markDirty(this);
    }
    get defaultValue() { return this.localName === 'textarea' ? this.textContent : this.getAttribute('value') ?? ''; }
    set defaultValue(v) { if (this.localName === 'textarea') this.textContent = v; else this.setAttribute('value', v); }
    get checked() { return this._chk !== undefined ? this._chk : this.hasAttribute('checked'); }
    set checked(v) { v = !!v; if (this._chk === v) return; this._chk = v; markDirty(this); }
    get defaultChecked() { return this.hasAttribute('checked'); }
    set defaultChecked(v) { this.toggleAttribute('checked', !!v); }
    get indeterminate() { return !!this._ind; }
    set indeterminate(v) { this._ind = !!v; markDirty(this); }
    get selected() { return this._sel !== undefined ? this._sel : this.hasAttribute('selected'); }
    set selected(v) { this._sel = !!v; markDirty(this); }
    get disabled() { return this.hasAttribute('disabled'); }
    set disabled(v) { if (v) this._setAttr('disabled', true); else this.removeAttribute('disabled'); }
    get readOnly() { return this.hasAttribute('readonly'); }
    set readOnly(v) { if (v) this._setAttr('readonly', true); else this.removeAttribute('readonly'); }
    get required() { return this.hasAttribute('required'); }
    set required(v) { this.toggleAttribute('required', !!v); }
    get multiple() { return this.hasAttribute('multiple'); }
    set multiple(v) { this.toggleAttribute('multiple', !!v); }
    get type() {
      const t = this.getAttribute('type');
      if (this.localName === 'button') return t && /^(submit|reset|button|menu)$/i.test(t) ? t.toLowerCase() : 'submit';
      if (this.localName === 'select') return this.multiple ? 'select-multiple' : 'select-one';
      if (this.localName === 'textarea') return 'textarea';
      return t ? t.toLowerCase() : 'text';
    }
    set type(v) { this.setAttribute('type', v); }
    get name() { return this.getAttribute('name') ?? ''; }
    set name(v) { this.setAttribute('name', v); }
    get placeholder() { return this.getAttribute('placeholder') ?? ''; }
    set placeholder(v) { this.setAttribute('placeholder', v); }
    get maxLength() { const v = this.getAttribute('maxlength'); return v == null ? -1 : +v; }
    set maxLength(v) { this._setAttr('maxlength', Number(v)); }
    get min() { return this.getAttribute('min') ?? ''; }
    get max() { return this.getAttribute('max') ?? ''; }
    get step() { return this.getAttribute('step') ?? ''; }
    get accept() { return this.getAttribute('accept') ?? ''; }
    set accept(v) { this.setAttribute('accept', v); }
    get autofocus() { return this.hasAttribute('autofocus'); }
    set autofocus(v) { this.toggleAttribute('autofocus', !!v); }
    get valueAsNumber() { const n = parseFloat(this.value); return isNaN(n) ? NaN : n; }
    set valueAsNumber(n) { this.value = String(n); }
    get files() { return this._files || (this._files = Object.assign([], { item(i) { return this[i] ?? null; } })); }
    set files(v) { this._files = v; }
    get form() { return this.closest('form'); }
    get labels() { return nodeList([]); }
    get options() { return this.querySelectorAll('option'); }
    get selectedIndex() { return this.options.findIndex((o) => o.selected); }
    set selectedIndex(i) { this.options.forEach((o, j) => { o.selected = i === j; }); }
    get selectedOptions() { return nodeList(this.options.filter((o) => o.selected)); }
    get selectionStart() { return this._selStart ?? this.value.length; }
    set selectionStart(v) { this._selStart = v; }
    get selectionEnd() { return this._selEnd ?? this.value.length; }
    set selectionEnd(v) { this._selEnd = v; }
    get selectionDirection() { return 'none'; }
    setSelectionRange(a, b) { this._selStart = a; this._selEnd = b; markDirty(this); }
    setRangeText(text, start = this.selectionStart, end = this.selectionEnd) { const v = this.value; this.value = v.slice(0, start) + text + v.slice(end); }
    select() { this._selStart = 0; this._selEnd = this.value.length; markDirty(this); }
    checkValidity() { return true; }
    reportValidity() { return true; }
    setCustomValidity() {}
    get validity() { return { valid: true, valueMissing: false, typeMismatch: false, patternMismatch: false, tooLong: false, tooShort: false, rangeUnderflow: false, rangeOverflow: false, stepMismatch: false, badInput: false, customError: false }; }
    get validationMessage() { return ''; }
    get willValidate() { return false; }
    stepUp() {}
    stepDown() {}
    showPicker() {}
    // anchors / images / media
    get href() { const v = this.getAttribute('href'); if (v == null) return ''; try { return new URL(v, location.href).href; } catch { return v; } }
    set href(v) { this.setAttribute('href', v); }
    get target() { return this.getAttribute('target') ?? ''; }
    set target(v) { this.setAttribute('target', v); }
    get rel() { return this.getAttribute('rel') ?? ''; }
    set rel(v) { this.setAttribute('rel', v); }
    get relList() { return new DOMTokenList(this, 'rel'); }
    get download() { return this.getAttribute('download') ?? ''; }
    set download(v) { this.setAttribute('download', v); }
    get src() { const v = this.getAttribute('src'); if (v == null) return ''; try { return new URL(v, location.href).href; } catch { return v; } }
    set src(v) { this.setAttribute('src', v); if (this._onSrc) this._onSrc(); }
    get alt() { return this.getAttribute('alt') ?? ''; }
    set alt(v) { this.setAttribute('alt', v); }
    get width() { const v = this.getAttribute('width'); return v != null ? +v : this._r ? Math.round(this._r[2]) : 0; }
    set width(v) { this._setAttr('width', Number(v)); }
    get height() { const v = this.getAttribute('height'); return v != null ? +v : this._r ? Math.round(this._r[3]) : 0; }
    set height(v) { this._setAttr('height', Number(v)); }
    get htmlFor() { return this.getAttribute('for') ?? ''; }
    set htmlFor(v) { this.setAttribute('for', v); }
    get action() { return this.getAttribute('action') ?? location.href; }
    get method() { return (this.getAttribute('method') || 'get').toLowerCase(); }
    get elements() { return this.querySelectorAll('input,select,textarea,button,fieldset,output,object'); }
    submit() { dispatchSynthetic(this, 'submit', { bubbles: true, cancelable: true }); }
    requestSubmit() { this.submit(); }
    reset() { for (const e of this.elements) { delete e._val; delete e._chk; } markDirty(this); }
    get open() { return this.hasAttribute('open'); }
    set open(v) { this.toggleAttribute('open', !!v); }
    show() { this.open = true; }
    showModal() { this.open = true; }
    close(rv) { this.open = false; if (rv !== undefined) this.returnValue = rv; dispatchSynthetic(this, 'close', {}); }
    showPopover() { this._popover = true; markDirty(this); }
    hidePopover() { this._popover = false; markDirty(this); }
    togglePopover(f) { this._popover = f === undefined ? !this._popover : !!f; markDirty(this); return this._popover; }
    get popover() { return this.getAttribute('popover'); }
    set popover(v) { this.setAttribute('popover', v); }
    // <style> / <script> / <template>
    get sheet() { return this.localName === 'style' ? styleSheetOf(this) : null; }
    get content() { if (this.localName !== 'template') return undefined; return this._tpl || (this._tpl = document.createDocumentFragment()); }
    get media() { return this.getAttribute('media') ?? ''; }
    set media(v) { this.setAttribute('media', v); }
    get text() { return this.textContent; }
    set text(v) { this.textContent = v; }
    get label() { return this.getAttribute('label') ?? this.textContent; }
    get index() { const s = this.closest('select'); return s ? s.options.indexOf(this) : 0; }
    // images
    get complete() { return this._loaded !== false; }
    get naturalWidth() { return this._nw || 0; }
    get naturalHeight() { return this._nh || 0; }
    get currentSrc() { return this.src; }
    decode() { return Promise.resolve(); }
    get loading() { return this.getAttribute('loading') ?? 'auto'; }
    set loading(v) { this.setAttribute('loading', v); }
    // canvas
    getContext(type) { if (this.localName !== 'canvas') return null; return this._ctx2d || (this._ctx2d = new CanvasRenderingContext2D(this)); }
    toDataURL() { return 'data:,'; }
    toBlob(cb) { setTimeout(() => cb(new Blob([], { type: 'image/png' })), 0); }
    captureStream() { throw new DOMException('captureStream is not supported', 'NotSupportedError'); }
    transferControlToOffscreen() { return new OffscreenCanvas(this.width, this.height); }
    // media elements
    play() { return Promise.reject(new DOMException('play() failed because media playback is not available.', 'NotSupportedError')); }
    pause() { this._paused = true; }
    load() {}
    canPlayType() { return ''; }
    get paused() { return this._paused !== false; }
    get currentTime() { return this._t || 0; }
    set currentTime(v) { this._t = +v || 0; }
    get duration() { return NaN; }
    get volume() { return this._vol ?? 1; }
    set volume(v) { this._vol = +v; }
    get muted() { return !!this._muted; }
    set muted(v) { this._muted = !!v; }
    get ended() { return false; }
    get readyState() { return 0; }
    get srcObject() { return this._srcObj ?? null; }
    set srcObject(v) { this._srcObj = v; }
    get dataset() { return super.dataset; }
  }
  class SVGElement extends Element {
    constructor(tag) { super(tag, true); }
    get ownerSVGElement() { return this.closest('svg'); }
    get viewportElement() { return this.ownerSVGElement; }
    getBBox() { const r = this.getBoundingClientRect(); return { x: 0, y: 0, width: r.width, height: r.height }; }
    getTotalLength() { return 0; }
    getPointAtLength() { return { x: 0, y: 0 }; }
    getScreenCTM() { return null; }
    get className() { return { baseVal: this.getAttribute('class') ?? '', animVal: this.getAttribute('class') ?? '' }; }
    set className(v) { this.setAttribute('class', v); }
  }
  // Slack component placeholder created by __clack.native(name)
  class NativeElement extends HTMLElement {
    constructor(name) { super('@' + name); this._n = name; this._np = null; }
  }

  // named element classes for instanceof checks
  const htmlClasses = {};
  const tagClass = { a: 'HTMLAnchorElement', area: 'HTMLAreaElement', audio: 'HTMLAudioElement', br: 'HTMLBRElement', base: 'HTMLBaseElement', body: 'HTMLBodyElement', button: 'HTMLButtonElement', canvas: 'HTMLCanvasElement', dl: 'HTMLDListElement', data: 'HTMLDataElement', datalist: 'HTMLDataListElement', details: 'HTMLDetailsElement', dialog: 'HTMLDialogElement', div: 'HTMLDivElement', embed: 'HTMLEmbedElement', fieldset: 'HTMLFieldSetElement', form: 'HTMLFormElement', h1: 'HTMLHeadingElement', h2: 'HTMLHeadingElement', h3: 'HTMLHeadingElement', h4: 'HTMLHeadingElement', h5: 'HTMLHeadingElement', h6: 'HTMLHeadingElement', hr: 'HTMLHRElement', head: 'HTMLHeadElement', html: 'HTMLHtmlElement', iframe: 'HTMLIFrameElement', img: 'HTMLImageElement', input: 'HTMLInputElement', li: 'HTMLLIElement', label: 'HTMLLabelElement', legend: 'HTMLLegendElement', link: 'HTMLLinkElement', map: 'HTMLMapElement', meta: 'HTMLMetaElement', meter: 'HTMLMeterElement', del: 'HTMLModElement', ins: 'HTMLModElement', ol: 'HTMLOListElement', object: 'HTMLObjectElement', optgroup: 'HTMLOptGroupElement', option: 'HTMLOptionElement', output: 'HTMLOutputElement', p: 'HTMLParagraphElement', picture: 'HTMLPictureElement', pre: 'HTMLPreElement', progress: 'HTMLProgressElement', q: 'HTMLQuoteElement', blockquote: 'HTMLQuoteElement', script: 'HTMLScriptElement', select: 'HTMLSelectElement', slot: 'HTMLSlotElement', source: 'HTMLSourceElement', span: 'HTMLSpanElement', style: 'HTMLStyleElement', caption: 'HTMLTableCaptionElement', td: 'HTMLTableCellElement', th: 'HTMLTableCellElement', col: 'HTMLTableColElement', table: 'HTMLTableElement', tr: 'HTMLTableRowElement', tbody: 'HTMLTableSectionElement', thead: 'HTMLTableSectionElement', tfoot: 'HTMLTableSectionElement', template: 'HTMLTemplateElement', textarea: 'HTMLTextAreaElement', time: 'HTMLTimeElement', title: 'HTMLTitleElement', track: 'HTMLTrackElement', ul: 'HTMLUListElement', video: 'HTMLVideoElement' };
  for (const n of new Set(Object.values(tagClass).concat(['HTMLUnknownElement', 'HTMLMediaElement']))) {
    const base = n === 'HTMLAudioElement' || n === 'HTMLVideoElement' ? null : HTMLElement;
    htmlClasses[n] = base ? { [n]: class extends HTMLElement {} }[n] : null;
  }
  htmlClasses.HTMLMediaElement = class HTMLMediaElement extends HTMLElement {};
  htmlClasses.HTMLAudioElement = class HTMLAudioElement extends htmlClasses.HTMLMediaElement {};
  htmlClasses.HTMLVideoElement = class HTMLVideoElement extends htmlClasses.HTMLMediaElement {};
  for (const [k, v] of Object.entries({ HAVE_NOTHING: 0, HAVE_METADATA: 1, HAVE_CURRENT_DATA: 2, HAVE_FUTURE_DATA: 3, HAVE_ENOUGH_DATA: 4 })) def(htmlClasses.HTMLMediaElement, k, v);
  const svgClasses = {};
  for (const n of ['SVGSVGElement', 'SVGPathElement', 'SVGGElement', 'SVGCircleElement', 'SVGRectElement', 'SVGGraphicsElement', 'SVGUseElement', 'SVGTextElement']) svgClasses[n] = { [n]: class extends SVGElement {} }[n];
  const svgTag = { svg: 'SVGSVGElement', path: 'SVGPathElement', g: 'SVGGElement', circle: 'SVGCircleElement', rect: 'SVGRectElement', use: 'SVGUseElement', text: 'SVGTextElement' };
  const newElement = (tag, svg) => {
    if (svg) { const C = svgClasses[svgTag[tag]] || SVGElement; return new C(tag); }
    const C = htmlClasses[tagClass[tag]] || (tag.includes('-') ? HTMLElement : htmlClasses.HTMLUnknownElement);
    const el = new C(tag);
    if (tag === 'img') initImage(el);
    return el;
  };
  jr.newElement = newElement;
  jr.NativeElement = NativeElement;

  // images: report load asynchronously (Clack's UI loads the pixels)
  const initImage = (img) => {
    img._loaded = true;
    img._onSrc = () => {
      img._loaded = false;
      setTimeout(() => { img._loaded = true; dispatchSynthetic(img, 'load', {}); }, 0);
    };
  };

  // ---- Document ----
  class Document extends Node {
    get nodeType() { return DOCUMENT_NODE; }
    get nodeName() { return '#document'; }
    get documentElement() { return docHtml; }
    get head() { return docHead; }
    get body() { return docBody; }
    get children() { return nodeList([docHtml]); }
    get firstElementChild() { return docHtml; }
    get defaultView() { return G; }
    get location() { return G.location; }
    set location(v) { G.location.href = v; }
    get URL() { return G.location.href; }
    get documentURI() { return G.location.href; }
    get domain() { return G.location.hostname; }
    get referrer() { return ''; }
    get readyState() { return 'complete'; }
    get compatMode() { return 'CSS1Compat'; }
    get characterSet() { return 'UTF-8'; }
    get charset() { return 'UTF-8'; }
    get contentType() { return 'text/html'; }
    get hidden() { return !!jr.docHidden; }
    get visibilityState() { return jr.docHidden ? 'hidden' : 'visible'; }
    get fullscreenElement() { return null; }
    get fullscreenEnabled() { return false; }
    get pointerLockElement() { return null; }
    get scrollingElement() { return docHtml; }
    get activeElement() { return activeEl && activeEl.isConnected ? activeEl : docBody; }
    get title() { const t = docHead.querySelector('title'); return t ? t.textContent : ''; }
    set title(v) { let t = docHead.querySelector('title'); if (!t) { t = this.createElement('title'); docHead.appendChild(t); } t.textContent = v; }
    get cookie() { return [...cookies].map(([k, v]) => k + '=' + v).join('; '); }
    set cookie(s) {
      const [pair, ...attrs] = String(s).split(';');
      const i = pair.indexOf('=');
      const k = (i < 0 ? '' : pair.slice(0, i)).trim(), v = (i < 0 ? pair : pair.slice(i + 1)).trim();
      if (attrs.some((a) => /^\s*max-age\s*=\s*(0|-)/i.test(a) || (/^\s*expires\s*=/i.test(a) && new Date(a.split('=')[1]) < new Date()))) cookies.delete(k);
      else cookies.set(k, v);
    }
    get fonts() { return fonts; }
    get styleSheets() { return [...qs(this, 'style', false)].map(styleSheetOf); }
    get adoptedStyleSheets() { return this._adopted || []; }
    set adoptedStyleSheets(v) { this._adopted = v; docDirty = true; queueFlush(); }
    get forms() { return this.querySelectorAll('form'); }
    get images() { return this.querySelectorAll('img'); }
    get links() { return this.querySelectorAll('a[href]'); }
    get scripts() { return this.querySelectorAll('script'); }
    get currentScript() { return null; }
    get implementation() { return { createHTMLDocument: () => document, hasFeature: () => true }; }
    get doctype() { return { name: 'html', nodeType: 10, publicId: '', systemId: '' }; }
    createElement(tag, opts) {
      tag = String(tag).toLowerCase();
      if (!/^[a-z][a-z0-9._-]*$|^[a-z][\w.-]*-[\w.-]*$/.test(tag)) throw new DOMException("Failed to execute 'createElement' on 'Document': The tag name provided ('" + tag + "') is not a valid name.", 'InvalidCharacterError');
      const el = newElement(tag, false);
      if (opts && opts.is) el._setAttr('is', opts.is);
      return el;
    }
    createElementNS(ns, tag) { return newElement(String(tag), ns === SVG_NS); }
    createTextNode(s) { return new Text(s); }
    createComment(s) { return new Comment(s); }
    createDocumentFragment() { return new DocumentFragment(); }
    createEvent(type) {
      const C = { mouseevent: MouseEvent, mouseevents: MouseEvent, keyboardevent: KeyboardEvent, keyboardevents: KeyboardEvent, focusevent: FocusEvent, uievent: UIEvent, uievents: UIEvent, customevent: CustomEvent, inputevent: InputEvent }[String(type).toLowerCase()] || Event;
      const e = new C('');
      return e;
    }
    createRange() { return new Range(); }
    createTreeWalker(root, what = 0xffffffff, filter) { return new TreeWalker(root, what, filter); }
    createNodeIterator(root, what = 0xffffffff, filter) { return new TreeWalker(root, what, filter); }
    importNode(n, deep) { return n.cloneNode(deep); }
    adoptNode(n) { if (n.parentNode) n.parentNode.removeChild(n); return n; }
    getElementById(id) { return qs(this, '#' + CSS.escape(String(id)), true)[0] ?? null; }
    getElementsByClassName(c) { return docHtml.getElementsByClassName(c); }
    getElementsByTagName(t) { return t === 'html' ? nodeList([docHtml]) : docHtml.getElementsByTagName(t); }
    getElementsByName(n) { return this.querySelectorAll('[name="' + CSS.escape(String(n)) + '"]'); }
    querySelector(s) { return qs(this, s, true)[0] ?? null; }
    querySelectorAll(s) { return nodeList(qs(this, s, false)); }
    elementFromPoint(x, y) { return hitTest(x, y)[0] ?? null; }
    elementsFromPoint(x, y) { return hitTest(x, y); }
    hasFocus() { return !jr.docHidden; }
    getSelection() { return selection; }
    execCommand() { return false; }
    queryCommandSupported() { return false; }
    queryCommandState() { return false; }
    exitFullscreen() { return Promise.resolve(); }
    exitPointerLock() {}
    startViewTransition(cb) { const p = Promise.resolve().then(() => cb && cb()); return { finished: p, ready: p, updateCallbackDone: p, skipTransition() {} }; }
    open() { return this; }
    close() {}
    write() {}
    writeln() {}
    append(...n) {}
    _parentForEvents() { return G; }
  }
  const cookies = new Map();
  const document = new Document();
  const docHtml = newElement('html'), docHead = newElement('head'), docBody = newElement('body');
  docHtml.parentNode = document;
  document._c = [docHtml];
  docHtml.appendChild(docHead);
  docHtml.appendChild(docBody);
  docHtml._setAttr('lang', (H.language || 'en-US'));
  def(G, 'document', document);
  jr.docBody = docBody;
  jr.docHead = docHead;

  // fonts (FontFaceSet): everything is "loaded"
  const fonts = new EventTarget();
  Object.assign(fonts, { status: 'loaded', ready: Promise.resolve(fonts), check: () => true, load: () => Promise.resolve([]), add() {}, delete: () => false, clear() {}, has: () => false, size: 0, forEach() {}, [Symbol.iterator]: function* () {} });
  def(G, 'FontFace', class FontFace { constructor(family, source, desc) { this.family = family; this.status = 'loaded'; this.loaded = Promise.resolve(this); Object.assign(this, desc || {}); } load() { return Promise.resolve(this); } });

  // ---- selection / range / tree walker (minimal) ----
  class Range {
    constructor() { this.startContainer = document; this.endContainer = document; this.startOffset = 0; this.endOffset = 0; this.collapsed = true; this.commonAncestorContainer = document; }
    setStart(n, o) { this.startContainer = n; this.startOffset = o; }
    setEnd(n, o) { this.endContainer = n; this.endOffset = o; this.collapsed = false; }
    setStartBefore(n) { this.startContainer = n.parentNode; }
    setStartAfter(n) { this.startContainer = n.parentNode; }
    setEndBefore(n) { this.endContainer = n.parentNode; }
    setEndAfter(n) { this.endContainer = n.parentNode; }
    selectNode(n) { this.startContainer = this.endContainer = n; }
    selectNodeContents(n) { this.startContainer = this.endContainer = n; }
    collapse() { this.collapsed = true; }
    cloneRange() { return Object.assign(new Range(), this); }
    cloneContents() { return document.createDocumentFragment(); }
    extractContents() { return document.createDocumentFragment(); }
    deleteContents() {}
    insertNode(n) { if (this.startContainer && this.startContainer.appendChild) this.startContainer.appendChild(n); }
    surroundContents(n) {}
    getBoundingClientRect() { return new DOMRect(); }
    getClientRects() { return []; }
    toString() { return ''; }
    createContextualFragment(html) { return parseHTML(String(html), false); }
    detach() {}
  }
  const selection = { rangeCount: 0, isCollapsed: true, type: 'None', anchorNode: null, focusNode: null, anchorOffset: 0, focusOffset: 0, getRangeAt: () => new Range(), addRange() {}, removeAllRanges() {}, removeRange() {}, collapse() {}, collapseToEnd() {}, collapseToStart() {}, selectAllChildren() {}, setBaseAndExtent() {}, extend() {}, modify() {}, containsNode: () => false, deleteFromDocument() {}, empty() {}, toString: () => '' };
  def(G, 'getSelection', () => selection);
  class TreeWalker {
    constructor(root, what, filter) { this.root = root; this.whatToShow = what; this.filter = filter; this.currentNode = root; this._list = null; }
    _all() {
      if (this._list) return this._list;
      const out = [], show = this.whatToShow;
      const ok = (n) => (show & (1 << (n.nodeType - 1))) && (!this.filter || (typeof this.filter === 'function' ? this.filter(n) : this.filter.acceptNode(n)) === 1);
      const walk = (n) => { if (n !== this.root && ok(n)) out.push(n); if (n._c) for (const c of n._c) walk(c); };
      walk(this.root);
      return (this._list = out);
    }
    nextNode() { const l = this._all(); const i = l.indexOf(this.currentNode); const n = l[i + 1]; if (n) this.currentNode = n; return n ?? null; }
    previousNode() { const l = this._all(); const i = l.indexOf(this.currentNode); const n = i > 0 ? l[i - 1] : null; if (n) this.currentNode = n; return n; }
    firstChild() { const n = this.currentNode.firstChild; if (n) this.currentNode = n; return n; }
    lastChild() { const n = this.currentNode.lastChild; if (n) this.currentNode = n; return n; }
    parentNode() { const n = this.currentNode === this.root ? null : this.currentNode.parentNode; if (n) this.currentNode = n; return n; }
    nextSibling() { const n = this.currentNode.nextSibling; if (n) this.currentNode = n; return n; }
    previousSibling() { const n = this.currentNode.previousSibling; if (n) this.currentNode = n; return n; }
    get referenceNode() { return this.currentNode; }
    detach() {}
  }
  def(G, 'NodeFilter', { FILTER_ACCEPT: 1, FILTER_REJECT: 2, FILTER_SKIP: 3, SHOW_ALL: 0xffffffff, SHOW_ELEMENT: 1, SHOW_ATTRIBUTE: 2, SHOW_TEXT: 4, SHOW_COMMENT: 128, SHOW_DOCUMENT: 256, SHOW_DOCUMENT_FRAGMENT: 1024 });

  // ---- DOMRect ----
  class DOMRectReadOnly {
    constructor(x = 0, y = 0, width = 0, height = 0) { this.x = +x; this.y = +y; this.width = +width; this.height = +height; }
    get left() { return Math.min(this.x, this.x + this.width); }
    get right() { return Math.max(this.x, this.x + this.width); }
    get top() { return Math.min(this.y, this.y + this.height); }
    get bottom() { return Math.max(this.y, this.y + this.height); }
    toJSON() { return { x: this.x, y: this.y, width: this.width, height: this.height, top: this.top, right: this.right, bottom: this.bottom, left: this.left }; }
    static fromRect(r = {}) { return new this(r.x, r.y, r.width, r.height); }
  }
  class DOMRect extends DOMRectReadOnly {}
  class DOMPoint { constructor(x = 0, y = 0, z = 0, w = 1) { Object.assign(this, { x, y, z, w }); } static fromPoint(p = {}) { return new DOMPoint(p.x, p.y, p.z, p.w); } }
  class DOMMatrix { constructor() { Object.assign(this, { a: 1, b: 0, c: 0, d: 1, e: 0, f: 0, m11: 1, m12: 0, m21: 0, m22: 1, m41: 0, m42: 0, is2D: true, isIdentity: true }); } translate() { return new DOMMatrix(); } scale() { return new DOMMatrix(); } multiply() { return new DOMMatrix(); } inverse() { return new DOMMatrix(); } transformPoint(p) { return DOMPoint.fromPoint(p); } toString() { return 'matrix(1, 0, 0, 1, 0, 0)'; } }

  // ---- UI events ----
  class UIEvent extends Event { constructor(t, i = {}) { super(t, i); this.view = i.view ?? G; this.detail = i.detail ?? 0; this.which = i.which ?? 0; } }
  const modInit = (e, i) => { e.altKey = !!i.altKey; e.ctrlKey = !!i.ctrlKey; e.shiftKey = !!i.shiftKey; e.metaKey = !!i.metaKey; };
  class MouseEvent extends UIEvent {
    constructor(t, i = {}) {
      super(t, i);
      modInit(this, i);
      this.screenX = i.screenX ?? i.clientX ?? 0; this.screenY = i.screenY ?? i.clientY ?? 0;
      this.clientX = i.clientX ?? 0; this.clientY = i.clientY ?? 0;
      this.button = i.button ?? 0; this.buttons = i.buttons ?? 0;
      this.relatedTarget = i.relatedTarget ?? null;
      this.movementX = i.movementX ?? 0; this.movementY = i.movementY ?? 0;
      if (i.detail == null && /click$/.test(t)) this.detail = t === 'dblclick' ? 2 : 1;
    }
    get pageX() { return this.clientX; }
    get pageY() { return this.clientY; }
    get x() { return this.clientX; }
    get y() { return this.clientY; }
    get offsetX() { const r = this.target && this.target._r; return r ? this.clientX - r[0] : this.clientX; }
    get offsetY() { const r = this.target && this.target._r; return r ? this.clientY - r[1] : this.clientY; }
    getModifierState(k) { return ({ Alt: this.altKey, Control: this.ctrlKey, Shift: this.shiftKey, Meta: this.metaKey })[k] || false; }
  }
  class PointerEvent extends MouseEvent {
    constructor(t, i = {}) { super(t, i); this.pointerId = i.pointerId ?? 1; this.pointerType = i.pointerType ?? 'mouse'; this.isPrimary = i.isPrimary ?? true; this.width = i.width ?? 1; this.height = i.height ?? 1; this.pressure = i.pressure ?? (this.buttons ? 0.5 : 0); this.tiltX = 0; this.tiltY = 0; this.twist = 0; }
    getCoalescedEvents() { return [this]; }
    getPredictedEvents() { return []; }
  }
  class WheelEvent extends MouseEvent { constructor(t, i = {}) { super(t, i); this.deltaX = i.deltaX ?? 0; this.deltaY = i.deltaY ?? 0; this.deltaZ = i.deltaZ ?? 0; this.deltaMode = i.deltaMode ?? 0; } }
  WheelEvent.DOM_DELTA_PIXEL = 0; WheelEvent.DOM_DELTA_LINE = 1; WheelEvent.DOM_DELTA_PAGE = 2;
  class KeyboardEvent extends UIEvent {
    constructor(t, i = {}) {
      super(t, i);
      modInit(this, i);
      this.key = i.key ?? ''; this.code = i.code ?? ''; this.location = i.location ?? 0;
      this.repeat = !!i.repeat; this.isComposing = !!i.isComposing;
      this.charCode = i.charCode ?? (t === 'keypress' && this.key.length === 1 ? this.key.charCodeAt(0) : 0);
      this.keyCode = i.keyCode ?? keyCodeOf(this.key, this.code);
      this.which = i.which ?? (t === 'keypress' ? this.charCode : this.keyCode);
    }
    getModifierState(k) { return ({ Alt: this.altKey, Control: this.ctrlKey, Shift: this.shiftKey, Meta: this.metaKey })[k] || false; }
  }
  KeyboardEvent.DOM_KEY_LOCATION_STANDARD = 0; KeyboardEvent.DOM_KEY_LOCATION_LEFT = 1; KeyboardEvent.DOM_KEY_LOCATION_RIGHT = 2; KeyboardEvent.DOM_KEY_LOCATION_NUMPAD = 3;
  const keyCodes = { Backspace: 8, Tab: 9, Enter: 13, Shift: 16, Control: 17, Alt: 18, Pause: 19, CapsLock: 20, Escape: 27, ' ': 32, PageUp: 33, PageDown: 34, End: 35, Home: 36, ArrowLeft: 37, ArrowUp: 38, ArrowRight: 39, ArrowDown: 40, Insert: 45, Delete: 46, Meta: 91, ContextMenu: 93, ';': 186, '=': 187, ',': 188, '-': 189, '.': 190, '/': 191, '`': 192, '[': 219, '\\': 220, ']': 221, "'": 222 };
  const keyCodeOf = (key, code) => {
    if (keyCodes[key]) return keyCodes[key];
    if (/^F(\d{1,2})$/.test(key)) return 111 + +key.slice(1);
    if (/^Key[A-Z]$/.test(code)) return code.charCodeAt(3);
    if (/^Digit\d$/.test(code)) return code.charCodeAt(5);
    if (key.length === 1) { const c = key.toUpperCase().charCodeAt(0); if ((c >= 65 && c <= 90) || (c >= 48 && c <= 57)) return c; }
    return 0;
  };
  class FocusEvent extends UIEvent { constructor(t, i = {}) { super(t, i); this.relatedTarget = i.relatedTarget ?? null; } }
  class InputEvent extends UIEvent { constructor(t, i = {}) { super(t, i); this.data = i.data ?? null; this.inputType = i.inputType ?? ''; this.isComposing = !!i.isComposing; this.dataTransfer = i.dataTransfer ?? null; } getTargetRanges() { return []; } }
  class CompositionEvent extends UIEvent { constructor(t, i = {}) { super(t, i); this.data = i.data ?? ''; } }
  class ClipboardEvent extends Event { constructor(t, i = {}) { super(t, i); this.clipboardData = i.clipboardData ?? new DataTransfer(); } }
  class DragEvent extends MouseEvent { constructor(t, i = {}) { super(t, i); this.dataTransfer = i.dataTransfer ?? null; } }
  class TouchEvent extends UIEvent { constructor(t, i = {}) { super(t, i); this.touches = i.touches || []; this.targetTouches = i.targetTouches || []; this.changedTouches = i.changedTouches || []; modInit(this, i); } }
  class SubmitEvent extends Event { constructor(t, i = {}) { super(t, i); this.submitter = i.submitter ?? null; } }
  class AnimationEvent extends Event { constructor(t, i = {}) { super(t, i); this.animationName = i.animationName ?? ''; this.elapsedTime = i.elapsedTime ?? 0; this.pseudoElement = ''; } }
  class TransitionEvent extends Event { constructor(t, i = {}) { super(t, i); this.propertyName = i.propertyName ?? ''; this.elapsedTime = i.elapsedTime ?? 0; this.pseudoElement = ''; } }
  class ToggleEvent extends Event { constructor(t, i = {}) { super(t, i); this.oldState = i.oldState ?? ''; this.newState = i.newState ?? ''; } }
  class StorageEvent extends Event { constructor(t, i = {}) { super(t, i); Object.assign(this, { key: i.key ?? null, oldValue: i.oldValue ?? null, newValue: i.newValue ?? null, url: i.url ?? '', storageArea: i.storageArea ?? null }); } }
  class HashChangeEvent extends Event { constructor(t, i = {}) { super(t, i); this.oldURL = i.oldURL ?? ''; this.newURL = i.newURL ?? ''; } }
  class PopStateEvent extends Event { constructor(t, i = {}) { super(t, i); this.state = i.state ?? null; } }
  class BeforeUnloadEvent extends Event {}
  class DataTransfer {
    constructor() { this._d = new Map(); this.dropEffect = 'none'; this.effectAllowed = 'all'; this.files = []; this.items = []; }
    get types() { return [...this._d.keys()]; }
    getData(t) { return this._d.get(t) ?? ''; }
    setData(t, v) { this._d.set(t, String(v)); }
    clearData(t) { if (t) this._d.delete(t); else this._d.clear(); }
    setDragImage() {}
  }

  // React-style synthetic additions on every Event (React passes the event itself)
  Object.assign(Event.prototype, {
    persist() {},
    isDefaultPrevented() { return this.defaultPrevented; },
    isPropagationStopped() { return this[kStop]; },
  });
  Object.defineProperty(Event.prototype, 'nativeEvent', { get() { return this; }, configurable: true });

  // ---- dispatch with capture/bubble through the DOM and React handlers ----
  // DOM type → React handler key(s) (lowercased prop name without "on")
  const reactKeys = (type, el) => {
    switch (type) {
      case 'dblclick': return ['doubleclick'];
      case 'input': return isTextInput(el) ? ['input', 'change', 'beforeinput'] : ['input'];
      case 'change': return isTextInput(el) ? [] : ['change'];
      case 'focusin': return ['focus'];
      case 'focusout': return ['blur'];
      case 'focus': case 'blur': return [];
      case 'mouseenter': case 'mouseleave': case 'pointerenter': case 'pointerleave': return [type];
      case 'compositionstart': case 'compositionend': case 'compositionupdate': return [type];
      default: return [type];
    }
  };
  const isTextInput = (el) => el && (el.localName === 'textarea' || (el.localName === 'input' && !/^(checkbox|radio|file|submit|button|reset|image|range|color)$/.test(el.type)) || el.isContentEditable);
  const nonBubbling = new Set(['focus', 'blur', 'mouseenter', 'mouseleave', 'pointerenter', 'pointerleave', 'load', 'error', 'scroll', 'resize', 'abort', 'loadstart', 'progress', 'loadend', 'toggle', 'close', 'cancel', 'invalid']);
  const callReact = (node, ev, capture) => {
    const h = node._h;
    if (!h) return;
    const keys = reactKeys(ev.type, node);
    for (const k of keys) {
      const fn = h[capture ? k + 'capture' : k];
      if (typeof fn !== 'function') continue;
      ev.currentTarget = node;
      try { fn.call(undefined, ev); } catch (e) { jr.reportError(e); }
      if (ev[kImmediate]) return;
    }
  };
  const eventPath = (target) => {
    const path = [];
    for (let n = target; n; n = n._parentForEvents ? n._parentForEvents() : null) {
      path.push(n);
      if (n === G) break;
    }
    if (path[path.length - 1] !== G && target !== G) path.push(G);
    return path;
  };
  jr.dispatch = (target, ev) => {
    const path = eventPath(target);
    ev.target = ev.srcElement = target;
    ev._path = path;
    const bubbles = ev.bubbles;
    const reactBubbles = !nonBubbling.has(ev.type) || ev.type === 'scroll';
    // capture: window → … → parent of target
    ev.eventPhase = 1;
    for (let i = path.length - 1; i > 0 && !ev[kStop]; i--) {
      jr.invoke(path[i], ev, 1);
      if (!ev[kStop] && path[i]._h && reactBubbles) callReact(path[i], ev, true);
    }
    // at target: capture listeners, React capture, then bubble listeners, React bubble
    if (!ev[kStop]) {
      ev.eventPhase = 2;
      jr.invoke(target, ev, 1);
      if (!ev[kStop] && target._h) callReact(target, ev, true);
      if (!ev[kStop]) jr.invoke(target, ev, 3);
      if (!ev[kStop] && target._h) callReact(target, ev, false);
    }
    // bubble (React handlers bubble for focus/blur via focusin/out, and scroll)
    ev.eventPhase = 3;
    for (let i = 1; i < path.length && !ev[kStop]; i++) {
      if (bubbles) jr.invoke(path[i], ev, 3);
      if (!ev[kStop] && path[i]._h && reactBubbles && (bubbles || ev.type === 'scroll' ? false : false)) callReact(path[i], ev, false);
      if (!ev[kStop] && path[i]._h && bubbles) callReact(path[i], ev, false);
    }
    ev.eventPhase = 0;
    ev.currentTarget = null;
    return !ev[kCanceled];
  };
  // invoke: listeners on a target for a phase (1 capture, 3 bubble), env.js's
  // generic version handles on<type> properties too.

  const makeEvent = (type, init) => {
    let C = Event;
    if (/^(click|dblclick|auxclick|contextmenu|mouse(down|up|move|over|out|enter|leave))$/.test(type)) C = MouseEvent;
    else if (/^pointer/.test(type)) C = PointerEvent;
    else if (type === 'wheel') C = WheelEvent;
    else if (/^key/.test(type)) C = KeyboardEvent;
    else if (/^(focus|blur|focusin|focusout)$/.test(type)) C = FocusEvent;
    else if (type === 'input' || type === 'beforeinput') C = InputEvent;
    else if (/^composition/.test(type)) C = CompositionEvent;
    else if (type === 'submit') C = SubmitEvent;
    else if (/^(copy|cut|paste)$/.test(type)) C = ClipboardEvent;
    else if (/^drag|^drop$/.test(type)) C = DragEvent;
    return new C(type, init);
  };
  const dispatchSynthetic = (target, type, init) => {
    const ev = makeEvent(type, Object.assign({ bubbles: !nonBubbling.has(type), cancelable: true, composed: true }, init));
    return target.dispatchEvent(ev);
  };
  jr.dispatchSynthetic = dispatchSynthetic;

  // ---- focus ----
  let activeEl = null;
  const focusElement = (el) => {
    const prev = activeEl;
    if (prev === el) return;
    activeEl = el;
    if (prev) {
      dispatchSynthetic(prev, 'blur', { relatedTarget: el, cancelable: false });
      dispatchSynthetic(prev, 'focusout', { relatedTarget: el, cancelable: false });
      markDirty(prev);
    }
    if (el) {
      dispatchSynthetic(el, 'focus', { relatedTarget: prev, cancelable: false });
      dispatchSynthetic(el, 'focusin', { relatedTarget: prev, cancelable: false });
      markDirty(el);
    }
  };
  jr.focusElement = focusElement;
  jr.activeElement = () => activeEl;

  // ---- events from Go ----
  // target: host node k (0 = document); ev: parsed event JSON
  let hovered = null;
  __clack.dispatch = (k, e) => {
    const target = k ? byK(k) : document;
    if (!target) return { defaultPrevented: false, propagationStopped: false };
    if (e.rects) for (const key in e.rects) { const n = byK(+key); if (n) n._r = e.rects[key]; }
    const type = e.type;
    if (e.target && target.nodeType === ELEMENT_NODE) {
      if ('value' in e.target && e.target.value != null) { target._val = String(e.target.value); if (e.target.selectionStart != null) { target._selStart = e.target.selectionStart; target._selEnd = e.target.selectionEnd ?? e.target.selectionStart; } }
      if ('checked' in e.target && e.target.checked != null) target._chk = !!e.target.checked;
      markDirty(target);
    }
    if (type === 'focus') { focusElement(target === document ? null : target); return { defaultPrevented: false, propagationStopped: false }; }
    if (type === 'blur') { if (activeEl === target || target === document) focusElement(null); return { defaultPrevented: false, propagationStopped: false }; }
    if (type === 'mouseenter') hovered = target;
    const init = Object.assign({}, e, { bubbles: !nonBubbling.has(type), cancelable: !/^(mouseenter|mouseleave|pointerenter|pointerleave|scroll|input)$/.test(type), composed: true, view: G });
    delete init.target; delete init.rects; delete init.type;
    if (type === 'scroll' && e.scrollTop != null) { target.scrollTop = e.scrollTop; target.scrollLeft = e.scrollLeft ?? target.scrollLeft; }
    const ev = makeEvent(type, init);
    def(ev, 'isTrusted', true);
    // keyboard events go to the focused element when Go targets the document
    const t = (k === 0 && /^key/.test(type) && activeEl && activeEl.isConnected) ? activeEl : target;
    t.dispatchEvent(ev);
    const res = { defaultPrevented: ev.defaultPrevented, propagationStopped: !!ev[kStop] };
    if (!ev.defaultPrevented) defaultAction(t, ev);
    return res;
  };
  // <input type=file>: Options.PickFiles shows the native picker
  const pickFiles = (input) => {
    if (!H.pickFiles) return;
    H.pickFiles(input.accept || '', !!input.multiple).then((list) => {
      if (!list) { dispatchSynthetic(input, 'cancel', { bubbles: false, cancelable: false }); return; }
      const files = list.map((f) => new File([f.data], f.name, { type: f.type || '', lastModified: f.lastModified || Date.now() }));
      input._files = Object.assign(files, { item(i) { return this[i] ?? null; } });
      input._val = files.length ? 'C:\\fakepath\\' + files[0].name : '';
      markDirty(input);
      dispatchSynthetic(input, 'input', { cancelable: false });
      dispatchSynthetic(input, 'change', { cancelable: false });
    }, (e) => jr.reportError(e));
  };
  const defaultAction = (t, ev) => {
    if (ev.type === 'click' && ev.button === 0) {
      const el = t.nodeType === ELEMENT_NODE ? t : t.parentElement;
      if (!el) return;
      if (el.localName === 'input' && el.type === 'file' && !el.disabled) { pickFiles(el); return; }
      const a = el.closest('a[href]');
      if (a) { const href = a.href; if (href && !/^javascript:/i.test(href)) { if (href.startsWith(location.origin + '/') && a.target !== '_blank') G.location.assign(href); else H.openURL(href); } return; }
      const label = el.closest('label');
      if (label) { const ctl = label.htmlFor ? document.getElementById(label.htmlFor) : label.querySelector('input,select,textarea,button'); if (ctl && ctl !== el && !ctl.contains(el)) { if (ctl.type === 'checkbox' || ctl.type === 'radio') { ctl._chk = ctl.type === 'radio' ? true : !ctl.checked; dispatchSynthetic(ctl, 'click', {}); dispatchSynthetic(ctl, 'input', {}); dispatchSynthetic(ctl, 'change', {}); } else ctl.focus(); } }
      const btn = el.closest('button,input[type=submit],input[type=image]');
      if (btn && !btn.disabled && (btn.type === 'submit' || btn.localName === 'input')) { const f = btn.form; if (f) dispatchSynthetic(f, 'submit', { submitter: btn }); }
      else if (btn && btn.type === 'reset' && btn.form) btn.form.reset();
    } else if (ev.type === 'keydown' && ev.key === 'Enter' && t.localName === 'input' && isTextInput(t)) {
      const f = t.form;
      if (f) dispatchSynthetic(f, 'submit', {});
    }
  };

  // ---- layout from Go ----
  const resizeObs = new Set();
  __clack.setRects = (rects) => {
    for (const key in rects) { const n = byK(+key); if (n) n._r = rects[key]; }
    for (const ro of resizeObs) ro._check();
  };
  let vw = 1280, vh = 800, dpr = 1, dark = false;
  const mqls = new Set();
  __clack.setViewport = (w, h, d, isDark) => {
    const resized = w !== vw || h !== vh;
    vw = w; vh = h; dpr = d || 1;
    const themed = !!isDark !== dark;
    dark = !!isDark;
    docHtml._r = [0, 0, vw, vh];
    docBody._r = [0, 0, vw, vh];
    if (resized) G.dispatchEvent(new UIEvent('resize'));
    for (const m of mqls) m._update();
    for (const ro of resizeObs) ro._check();
  };
  __clack.setHidden = (hidden) => {
    if (!!hidden === !!jr.docHidden) return;
    jr.docHidden = !!hidden;
    document.dispatchEvent(new Event('visibilitychange', { bubbles: true }));
    G.dispatchEvent(new FocusEvent(hidden ? 'blur' : 'focus'));
  };
  docHtml._r = [0, 0, vw, vh];
  docBody._r = [0, 0, vw, vh];

  // ---- window ----
  const winProps = {
    get innerWidth() { return vw; }, get innerHeight() { return vh; },
    get outerWidth() { return vw; }, get outerHeight() { return vh; },
    get devicePixelRatio() { return dpr; },
    get scrollX() { return 0; }, get scrollY() { return 0; }, get pageXOffset() { return 0; }, get pageYOffset() { return 0; },
    get screenX() { return 0; }, get screenY() { return 0; }, get screenLeft() { return 0; }, get screenTop() { return 0; },
  };
  for (const k of Object.keys(winProps)) Object.defineProperty(G, k, Object.assign(Object.getOwnPropertyDescriptor(winProps, k), { configurable: true }));
  def(G, 'screen', { get width() { return vw; }, get height() { return vh; }, get availWidth() { return vw; }, get availHeight() { return vh; }, colorDepth: 24, pixelDepth: 24, orientation: { type: 'landscape-primary', angle: 0, addEventListener() {}, removeEventListener() {} } });
  def(G, 'visualViewport', Object.assign(new EventTarget(), { get width() { return vw; }, get height() { return vh; }, scale: 1, offsetLeft: 0, offsetTop: 0, pageLeft: 0, pageTop: 0 }));
  for (const k of ['scrollTo', 'scroll', 'scrollBy', 'moveTo', 'moveBy', 'resizeTo', 'resizeBy', 'print', 'stop', 'focus', 'blur', 'captureEvents', 'releaseEvents']) def(G, k, () => {});
  def(G, 'open', (url) => { if (url) { try { H.openURL(new URL(String(url), location.href).href); } catch { H.openURL(String(url)); } } return null; });
  def(G, 'close', () => {});
  def(G, 'alert', (m) => console.warn('alert: ' + m));
  def(G, 'confirm', (m) => { console.warn('confirm: ' + m); return false; });
  def(G, 'prompt', (m) => { console.warn('prompt: ' + m); return null; });
  def(G, 'postMessage', (data, origin) => { const d = structuredClone(data); jr.immediate(() => G.dispatchEvent(new MessageEvent('message', { data: d, origin: location.origin, source: G }))); });
  for (const k of ['parent', 'top', 'frames']) def(G, k, G);
  def(G, 'opener', null);
  def(G, 'frameElement', null);
  def(G, 'name', '');
  def(G, 'closed', false);
  def(G, 'isSecureContext', true);
  def(G, 'crossOriginIsolated', false);
  def(G, 'length', 0);
  if (!G.origin) def(G, 'origin', 'https://app.slack.com');
  def(G, 'customElements', { define() {}, get: () => undefined, whenDefined: () => Promise.resolve(), upgrade() {} });
  def(G, 'CSS', {
    supports: () => true,
    escape: (s) => String(s).replace(/([\0-\x1f\x7f]|^-?\d)|^-$|[^\0-\x1f\x7f-￿\w-]/g, (m, ctl) => ctl ? (m === '\0' ? '�' : '\\' + m.charCodeAt(m.length - 1).toString(16) + ' ').replace(/^(-?)\\/, '$1\\') : '\\' + m),
    registerProperty() {},
    px: (n) => n + 'px',
  });

  // matchMedia
  const evalMedia = (q) => {
    q = String(q).toLowerCase().trim();
    if (!q || q === 'all' || q === 'screen') return true;
    if (q === 'print') return false;
    return q.split(',').some((part) => part.split(/\band\b/).every((c) => {
      c = c.trim().replace(/^only\s+/, '').replace(/^screen\s*/, '');
      if (!c) return true;
      const not = c.startsWith('not ');
      if (not) c = c.slice(4).trim();
      const m = c.match(/^\(\s*([a-z-]+)\s*(?::\s*([^)]+))?\)$/);
      if (!m) return not;
      const [, f, v0] = m, v = (v0 || '').trim();
      const px = (s) => s.endsWith('em') || s.endsWith('rem') ? parseFloat(s) * 16 : parseFloat(s);
      let r;
      switch (f) {
        case 'prefers-color-scheme': r = v === (dark ? 'dark' : 'light'); break;
        case 'prefers-reduced-motion': r = v === 'no-preference'; break;
        case 'prefers-contrast': r = v === 'no-preference'; break;
        case 'prefers-reduced-transparency': r = v === 'no-preference'; break;
        case 'forced-colors': r = v === 'none'; break;
        case 'min-width': r = vw >= px(v); break;
        case 'max-width': r = vw <= px(v); break;
        case 'min-height': r = vh >= px(v); break;
        case 'max-height': r = vh <= px(v); break;
        case 'width': r = vw === px(v); break;
        case 'orientation': r = v === (vw >= vh ? 'landscape' : 'portrait'); break;
        case 'hover': r = v ? v === 'hover' : true; break;
        case 'any-hover': r = v ? v === 'hover' : true; break;
        case 'pointer': case 'any-pointer': r = v ? v === 'fine' : true; break;
        case 'min-resolution': r = dpr * 96 >= parseFloat(v) * (v.endsWith('dppx') || v.endsWith('x') ? 96 : 1); break;
        case 'max-resolution': r = dpr * 96 <= parseFloat(v) * (v.endsWith('dppx') || v.endsWith('x') ? 96 : 1); break;
        case '-webkit-min-device-pixel-ratio': case 'min-device-pixel-ratio': r = dpr >= parseFloat(v); break;
        case 'display-mode': r = v === 'browser'; break;
        case 'color': r = true; break;
        default: r = false;
      }
      return not ? !r : r;
    }));
  };
  class MediaQueryList extends EventTarget {
    constructor(q) { super(); this.media = String(q); this.matches = evalMedia(q); this.onchange = null; }
    _update() { const m = evalMedia(this.media); if (m !== this.matches) { this.matches = m; const ev = new Event('change'); ev.matches = m; ev.media = this.media; this.dispatchEvent(ev); } }
    addListener(fn) { this.addEventListener('change', fn); }
    removeListener(fn) { this.removeEventListener('change', fn); }
    addEventListener(t, fn, o) { super.addEventListener(t, fn, o); mqls.add(this); }
  }
  def(G, 'matchMedia', (q) => { const m = new MediaQueryList(q); mqls.add(m); return m; });
  jr.isDark = () => dark;

  // getComputedStyle: inline style over Slack-ish defaults
  const defaults = { display: 'block', position: 'static', fontFamily: 'Slack-Lato, Slack-Fractions, appleLogo, sans-serif', fontSize: '15px', fontWeight: '400', fontStyle: 'normal', lineHeight: '22px', letterSpacing: 'normal', color: 'rgb(29, 28, 29)', backgroundColor: 'rgba(0, 0, 0, 0)', visibility: 'visible', opacity: '1', overflow: 'visible', boxSizing: 'border-box', textAlign: 'start', whiteSpace: 'normal', zIndex: 'auto', transform: 'none', cursor: 'auto', pointerEvents: 'auto' };
  const inlineTags = new Set(['span', 'a', 'b', 'i', 'em', 'strong', 'code', 'img', 'button', 'input', 'label', 'svg', 'select', 'small', 'sub', 'sup', 'u', 's', 'kbd', 'abbr', 'time']);
  const inherited = new Set(['fontFamily', 'fontSize', 'fontWeight', 'fontStyle', 'lineHeight', 'letterSpacing', 'color', 'visibility', 'textAlign', 'whiteSpace', 'cursor', 'direction', 'wordSpacing', 'textTransform']);
  def(G, 'getComputedStyle', (el, pseudo) => {
    const get = (p) => {
      for (let n = el; n && n.nodeType === ELEMENT_NODE; n = n.parentNode) {
        if (n._st && n._st[p] != null && n._st[p] !== '') return cssValue(p, n._st[p]);
        if (!inherited.has(p)) break;
      }
      if (p === 'display') return el.hidden ? 'none' : inlineTags.has(el.localName) ? 'inline' : 'block';
      if (p === 'width') return el._r ? el._r[2] + 'px' : 'auto';
      if (p === 'height') return el._r ? el._r[3] + 'px' : 'auto';
      if (p === 'colorScheme') return dark ? 'dark' : 'light';
      if (p === 'color' && dark) return 'rgb(209, 210, 211)';
      return defaults[p] ?? '';
    };
    return new Proxy({}, {
      get(t, k) {
        if (typeof k === 'symbol') return undefined;
        if (k === 'getPropertyValue') return (n) => get(propName(String(n)));
        if (k === 'getPropertyPriority') return () => '';
        if (k === 'length') return 0;
        if (k === 'item') return () => '';
        if (k === 'cssText') return '';
        return get(k === 'cssFloat' ? 'float' : k);
      },
      set() { throw new DOMException("Failed to set an indexed property on 'CSSStyleDeclaration': These styles are computed, and therefore read-only.", 'NoModificationAllowedError'); },
    });
  });

  // ---- observers ----
  class MutationObserver {
    constructor(cb) { if (typeof cb !== 'function') throw new TypeError("Failed to construct 'MutationObserver': parameter 1 is not of type 'MutationCallback'."); this._cb = cb; this._records = []; this._targets = new Set(); this._queued = false; }
    observe(target, options = {}) {
      const o = Object.assign({}, options);
      if (o.attributeOldValue || o.attributeFilter) o.attributes = o.attributes ?? true;
      if (o.characterDataOldValue) o.characterData = o.characterData ?? true;
      if (!o.childList && !o.attributes && !o.characterData) throw new TypeError("Failed to execute 'observe' on 'MutationObserver': The options object must set at least one of 'attributes', 'characterData', or 'childList' to true.");
      const regs = target._mo || (target._mo = []);
      const ex = regs.find((r) => r.observer === this);
      if (ex) ex.options = o; else { regs.push({ observer: this, options: o }); moCount++; }
      this._targets.add(target);
    }
    disconnect() {
      for (const t of this._targets) { const i = t._mo.findIndex((r) => r.observer === this); if (i >= 0) { t._mo.splice(i, 1); moCount--; } }
      this._targets.clear();
      this._records = [];
    }
    takeRecords() { const r = this._records; this._records = []; return r; }
    _queue(rec) {
      this._records.push(rec);
      if (!this._queued) {
        this._queued = true;
        queueMicrotask(() => { this._queued = false; const r = this.takeRecords(); if (r.length) { try { this._cb(r, this); } catch (e) { jr.reportError(e); } } });
      }
    }
  }
  class ResizeObserver {
    constructor(cb) { this._cb = cb; this._els = new Map(); }
    observe(el, opts) { if (!this._els.has(el)) { this._els.set(el, null); resizeObs.add(this); queueMicrotask(() => this._check()); } }
    unobserve(el) { this._els.delete(el); if (!this._els.size) resizeObs.delete(this); }
    disconnect() { this._els.clear(); resizeObs.delete(this); }
    _check() {
      const entries = [];
      for (const [el, last] of this._els) {
        const r = el._r || [0, 0, 0, 0];
        if (last && last[2] === r[2] && last[3] === r[3]) continue;
        this._els.set(el, r.slice());
        const size = [{ inlineSize: r[2], blockSize: r[3] }];
        entries.push({ target: el, contentRect: new DOMRectReadOnly(0, 0, r[2], r[3]), borderBoxSize: size, contentBoxSize: size, devicePixelContentBoxSize: [{ inlineSize: r[2] * dpr, blockSize: r[3] * dpr }] });
      }
      if (entries.length) { try { this._cb(entries, this); } catch (e) { jr.reportError(e); } }
    }
  }
  class IntersectionObserver {
    constructor(cb, opts = {}) { this._cb = cb; this.root = opts.root ?? null; this.rootMargin = opts.rootMargin ?? '0px'; this.thresholds = [].concat(opts.threshold ?? 0); this._els = new Set(); }
    observe(el) {
      if (this._els.has(el)) return;
      this._els.add(el);
      // everything Clack renders is treated as visible
      queueMicrotask(() => {
        if (!this._els.has(el)) return;
        const r = el.getBoundingClientRect();
        try { this._cb([{ target: el, isIntersecting: true, intersectionRatio: 1, boundingClientRect: r, intersectionRect: r, rootBounds: new DOMRectReadOnly(0, 0, vw, vh), time: performance.now() }], this); } catch (e) { jr.reportError(e); }
      });
    }
    unobserve(el) { this._els.delete(el); }
    disconnect() { this._els.clear(); }
    takeRecords() { return []; }
  }

  // ---- animation (Element.animate): finishes immediately ----
  class Animation extends EventTarget {
    constructor(el, kf, opts) {
      super();
      this.effect = { target: el, getKeyframes: () => [].concat(kf || []), getTiming: () => (typeof opts === 'number' ? { duration: opts } : opts || {}) };
      this.playState = 'running';
      this.currentTime = 0;
      this.playbackRate = 1;
      this.onfinish = null;
      this.oncancel = null;
      let resolve;
      this.finished = new Promise((r) => { resolve = r; });
      this.ready = Promise.resolve(this);
      const dur = typeof opts === 'number' ? opts : (opts && opts.duration) || 0;
      this._t = setTimeout(() => { this.playState = 'finished'; resolve(this); this.dispatchEvent(new Event('finish')); }, Math.min(+dur || 0, 10000));
    }
    play() { this.playState = 'running'; }
    pause() { this.playState = 'paused'; }
    finish() { clearTimeout(this._t); this.playState = 'finished'; this.dispatchEvent(new Event('finish')); }
    cancel() { clearTimeout(this._t); this.playState = 'idle'; this.dispatchEvent(new Event('cancel')); }
    reverse() {}
    commitStyles() {}
    persist() {}
    updatePlaybackRate(r) { this.playbackRate = r; }
  }

  // ---- canvas (text metrics only) ----
  const parseFont = (font) => {
    const m = String(font).match(/(\d+(?:\.\d+)?)px/);
    return { size: m ? +m[1] : 10, bold: /\b(bold|[6-9]00)\b/.test(font) };
  };
  const measure = (text, font, letterSpacing) => {
    if (H.measureText) { const w = H.measureText(String(font), String(text)); if (typeof w === 'number') return w; }
    const { size, bold } = parseFont(font);
    let w = 0;
    for (const ch of String(text)) {
      const c = ch.codePointAt(0);
      w += c < 0x80 ? (/[ilI.,;:'|!]/.test(ch) ? 0.27 : /[mwMW]/.test(ch) ? 0.85 : /[A-Z]/.test(ch) ? 0.65 : ch === ' ' ? 0.26 : 0.52) : c >= 0x1100 ? 1 : 0.6;
    }
    const ls = parseFloat(letterSpacing) || 0;
    return w * size * (bold ? 1.05 : 1) + ls * [...String(text)].length;
  };
  class CanvasRenderingContext2D {
    constructor(canvas) { this.canvas = canvas; this.font = '10px sans-serif'; this.letterSpacing = '0px'; this.fillStyle = '#000000'; this.strokeStyle = '#000000'; this.lineWidth = 1; this.globalAlpha = 1; this.textAlign = 'start'; this.textBaseline = 'alphabetic'; this.direction = 'inherit'; this.imageSmoothingEnabled = true; this.globalCompositeOperation = 'source-over'; this._stack = []; }
    measureText(text) {
      const w = measure(text, this.font, this.letterSpacing), size = parseFont(this.font).size;
      return { width: w, actualBoundingBoxLeft: 0, actualBoundingBoxRight: w, actualBoundingBoxAscent: size * 0.8, actualBoundingBoxDescent: size * 0.2, fontBoundingBoxAscent: size * 0.9, fontBoundingBoxDescent: size * 0.25, emHeightAscent: size * 0.8, emHeightDescent: size * 0.2, alphabeticBaseline: 0, hangingBaseline: size * 0.8, ideographicBaseline: -size * 0.2 };
    }
    save() { this._stack.push({ font: this.font, fillStyle: this.fillStyle, strokeStyle: this.strokeStyle, globalAlpha: this.globalAlpha }); }
    restore() { const s = this._stack.pop(); if (s) Object.assign(this, s); }
    getImageData(x, y, w, h) { return { width: w, height: h, data: new Uint8ClampedArray(Math.max(0, w * h * 4)), colorSpace: 'srgb' }; }
    createImageData(w, h) { if (typeof w === 'object') { h = w.height; w = w.width; } return { width: w, height: h, data: new Uint8ClampedArray(Math.max(0, w * h * 4)), colorSpace: 'srgb' }; }
    createLinearGradient() { return { addColorStop() {} }; }
    createRadialGradient() { return { addColorStop() {} }; }
    createConicGradient() { return { addColorStop() {} }; }
    createPattern() { return {}; }
    getTransform() { return new DOMMatrix(); }
    isPointInPath() { return false; }
    isPointInStroke() { return false; }
    getLineDash() { return []; }
    getContextAttributes() { return { alpha: true, desynchronized: false, colorSpace: 'srgb', willReadFrequently: false }; }
  }
  for (const m of ['fillRect', 'strokeRect', 'clearRect', 'fillText', 'strokeText', 'beginPath', 'closePath', 'moveTo', 'lineTo', 'bezierCurveTo', 'quadraticCurveTo', 'arc', 'arcTo', 'ellipse', 'rect', 'roundRect', 'fill', 'stroke', 'clip', 'drawImage', 'putImageData', 'translate', 'rotate', 'scale', 'transform', 'setTransform', 'resetTransform', 'setLineDash', 'drawFocusIfNeeded', 'reset']) CanvasRenderingContext2D.prototype[m] = function () {};
  class OffscreenCanvas extends EventTarget {
    constructor(w, h) { super(); this.width = w; this.height = h; this._ctx = null; }
    getContext(type) { return type === '2d' ? (this._ctx || (this._ctx = new CanvasRenderingContext2D(this))) : null; }
    convertToBlob(o) { return Promise.resolve(new Blob([], { type: (o && o.type) || 'image/png' })); }
    transferToImageBitmap() { return { width: this.width, height: this.height, close() {} }; }
  }

  // Image / Audio constructors
  const ImageCtor = function Image(w, h) { const img = document.createElement('img'); if (w != null) img.width = w; if (h != null) img.height = h; return img; };
  ImageCtor.prototype = htmlClasses.HTMLImageElement.prototype;
  const AudioCtor = function Audio(src) { const a = document.createElement('audio'); if (src != null) a.src = src; return a; };
  AudioCtor.prototype = htmlClasses.HTMLAudioElement.prototype;
  const OptionCtor = function Option(text = '', value, defSel, sel) { const o = document.createElement('option'); o.textContent = text; if (value !== undefined) o.value = value; if (defSel) o.defaultSelected = true; if (sel) o.selected = true; return o; };
  OptionCtor.prototype = htmlClasses.HTMLOptionElement.prototype;
  // Web Audio / media capture are not available natively; constructing
  // them works, using them fails like a browser without permission.
  const notSupported = (what) => () => Promise.reject(new DOMException(what + ' is not supported in Clack', 'NotSupportedError'));
  class AudioContext extends EventTarget {
    constructor() { super(); this.state = 'suspended'; this.sampleRate = 48000; this.currentTime = 0; this.destination = { maxChannelCount: 2 }; }
    resume() { return Promise.resolve(); } suspend() { return Promise.resolve(); } close() { this.state = 'closed'; return Promise.resolve(); }
    decodeAudioData(buf, ok, err) { const p = notSupported('decodeAudioData')(); if (err) p.catch(err); return p; }
    createGain() { return { gain: { value: 1, setValueAtTime() {} }, connect() {}, disconnect() {} }; }
    createOscillator() { return { frequency: { value: 440 }, connect() {}, disconnect() {}, start() {}, stop() {} }; }
    createBufferSource() { return { buffer: null, connect() {}, disconnect() {}, start() {}, stop() {}, addEventListener() {} }; }
    createMediaStreamDestination() { return { stream: new MediaStream(), connect() {}, disconnect() {} }; }
    createAnalyser() { return { connect() {}, disconnect() {}, getByteFrequencyData() {}, fftSize: 2048 }; }
  }
  class MediaStream extends EventTarget { constructor() { super(); this.id = crypto.randomUUID(); this.active = false; } getTracks() { return []; } getAudioTracks() { return []; } getVideoTracks() { return []; } addTrack() {} removeTrack() {} clone() { return new MediaStream(); } }
  class MediaRecorder extends EventTarget {
    constructor(stream, opts) { super(); this.stream = stream; this.state = 'inactive'; this.mimeType = (opts && opts.mimeType) || ''; }
    static isTypeSupported() { return false; }
    start() { throw new DOMException('MediaRecorder is not supported in Clack', 'NotSupportedError'); }
    stop() {} pause() {} resume() {} requestData() {}
  }

  // ---- CSSOM (enough for plugins that read <style>.sheet) ----
  const sheets = new WeakMap();
  const styleSheetOf = (el) => {
    let s = sheets.get(el);
    if (!s) { s = new CSSStyleSheet(); s.ownerNode = el; sheets.set(el, s); }
    s._text = el.textContent;
    return s;
  };
  class CSSStyleSheet {
    constructor() { this._text = ''; this.ownerNode = null; this.disabled = false; this.media = []; this.href = null; this.type = 'text/css'; }
    get cssRules() { return splitRules(this._text).map((cssText) => ({ cssText, selectorText: cssText.split('{')[0].trim(), style: {} })); }
    get rules() { return this.cssRules; }
    insertRule(rule, i) { const r = splitRules(this._text); i = i ?? 0; r.splice(i, 0, rule); this._set(r.join('\n')); return i; }
    deleteRule(i) { const r = splitRules(this._text); r.splice(i, 1); this._set(r.join('\n')); }
    addRule(sel, style, i) { return this.insertRule(sel + '{' + style + '}', i); }
    removeRule(i) { this.deleteRule(i ?? 0); }
    replace(t) { this._set(t); return Promise.resolve(this); }
    replaceSync(t) { this._set(t); }
    _set(t) { this._text = String(t); if (this.ownerNode) this.ownerNode.textContent = this._text; else { docDirty = true; queueFlush(); } }
  }
  const splitRules = (css) => {
    const out = [];
    let depth = 0, start = 0;
    for (let i = 0; i < css.length; i++) {
      const c = css[i];
      if (c === '{') depth++;
      else if (c === '}') { depth--; if (depth === 0) { out.push(css.slice(start, i + 1).trim()); start = i + 1; } }
    }
    return out.filter(Boolean);
  };

  // ---- hit testing (elementFromPoint) from reported rects ----
  const hitTest = (x, y) => {
    const out = [];
    const walk = (n) => {
      if (n._c) for (let i = n._c.length - 1; i >= 0; i--) { const c = n._c[i]; if (c.nodeType === ELEMENT_NODE) walk(c); }
      const r = n._r;
      if (r && x >= r[0] && y >= r[1] && x < r[0] + r[2] && y < r[1] + r[3]) out.push(n);
    };
    walk(docHtml);
    for (const root of roots.values()) walk(root);
    return out;
  };

  // ---- selectors ----
  const selCache = new Map();
  const compile = (sel) => {
    sel = String(sel);
    let f = selCache.get(sel);
    if (f) return f;
    f = compileList(sel);
    if (selCache.size > 500) selCache.clear();
    selCache.set(sel, f);
    return f;
  };
  const qs = (root, sel, first) => {
    const m = compile(sel), out = [];
    const scope = root.nodeType === ELEMENT_NODE ? root : null;
    const walk = (n) => {
      if (!n._c) return false;
      for (const c of n._c) {
        if (c.nodeType !== ELEMENT_NODE) continue;
        if (m(c, scope)) { out.push(c); if (first) return true; }
        if (walk(c)) return true;
      }
      return false;
    };
    walk(root);
    return out;
  };
  // tokenizer → list of complex selectors; each complex = [compound, comb, compound, …]
  const compileList = (src) => {
    let i = 0;
    const err = () => new DOMException("Failed to execute 'querySelectorAll' on 'Document': '" + src + "' is not a valid selector.", 'SyntaxError');
    const ws = () => { while (i < src.length && /\s/.test(src[i])) i++; };
    const ident = () => {
      let s = '';
      while (i < src.length) {
        const c = src[i];
        if (c === '\\') { i++; const hex = src.slice(i).match(/^[0-9a-fA-F]{1,6}\s?/); if (hex) { s += String.fromCodePoint(parseInt(hex[0], 16)); i += hex[0].length; } else s += src[i++]; }
        else if (/[\w-]/.test(c) || c.charCodeAt(0) > 127) { s += c; i++; }
        else break;
      }
      return s;
    };
    const str = () => { const q = src[i++]; let s = ''; while (i < src.length && src[i] !== q) { if (src[i] === '\\') i++; s += src[i++]; } i++; return s; };
    const parseCompound = () => {
      const tests = [];
      let any = false;
      for (;;) {
        const c = src[i];
        if (c === '*') { i++; any = true; continue; }
        if (c === '#') { i++; const id = ident(); tests.push((e) => e.getAttribute('id') === id); continue; }
        if (c === '.') { i++; const cls = ident(); tests.push((e) => { const v = e._a && e._a.get('class'); if (v == null) return false; const s = String(v); return s === cls || (' ' + s.replace(/\s+/g, ' ') + ' ').includes(' ' + cls + ' '); }); continue; }
        if (c === '[') {
          i++; ws();
          let name = ident();
          if (src[i] === '|') { i++; name = ident(); }
          ws();
          let op = null, val = null, ci = false;
          if (src[i] !== ']') {
            op = src[i] === '=' ? '=' : src.slice(i, i + 2);
            i += op.length;
            ws();
            val = src[i] === '"' || src[i] === "'" ? str() : ident();
            ws();
            if (/[iIsS]/.test(src[i] || '') && src[i + 1] !== undefined) { ci = /[iI]/.test(src[i]); i++; ws(); }
          }
          if (src[i] !== ']') throw err();
          i++;
          const lname = name.toLowerCase();
          tests.push((e) => {
            let v = e.getAttribute(e._svg ? name : lname);
            if (v == null) return false;
            if (op == null) return true;
            let w = val;
            if (ci) { v = v.toLowerCase(); w = w.toLowerCase(); }
            switch (op) {
              case '=': return v === w;
              case '~=': return v.split(/\s+/).includes(w);
              case '|=': return v === w || v.startsWith(w + '-');
              case '^=': return w !== '' && v.startsWith(w);
              case '$=': return w !== '' && v.endsWith(w);
              case '*=': return w !== '' && v.includes(w);
            }
            throw err();
          });
          continue;
        }
        if (c === ':') {
          i++;
          const pseudoEl = src[i] === ':';
          if (pseudoEl) i++;
          const name = ident().toLowerCase();
          let arg = null;
          if (src[i] === '(') {
            let depth = 1, start = ++i;
            while (i < src.length && depth) { if (src[i] === '(') depth++; else if (src[i] === ')') depth--; if (depth) i++; }
            arg = src.slice(start, i);
            i++;
          }
          if (pseudoEl) { tests.push(() => false); continue; }
          tests.push(pseudo(name, arg, err));
          continue;
        }
        if (c && /[\w-]/.test(c) || (c && c.charCodeAt(0) > 127) || c === '\\') {
          const tag = ident();
          const lt = tag.toLowerCase();
          tests.push((e) => e._svg ? e.localName === tag || e.localName.toLowerCase() === lt : e.localName === lt);
          continue;
        }
        break;
      }
      if (!tests.length && !any) throw err();
      if (tests.length === 1) return tests[0];
      return (e, scope) => { for (const t of tests) if (!t(e, scope)) return false; return true; };
    };
    const complexes = [];
    for (;;) {
      ws();
      const parts = [parseCompound()];
      for (;;) {
        const before = i;
        ws();
        const c = src[i];
        if (c === undefined || c === ',' || c === ')') break;
        let comb = ' ';
        if (c === '>' || c === '+' || c === '~') { comb = c; i++; ws(); }
        else if (before === i) throw err();
        parts.push(comb, parseCompound());
      }
      complexes.push(matchComplex(parts));
      if (src[i] === ',') { i++; continue; }
      if (i < src.length) throw err();
      break;
    }
    if (complexes.length === 1) return complexes[0];
    return (e, scope) => { for (const m of complexes) if (m(e, scope)) return true; return false; };
  };
  // match right-to-left with backtracking
  const matchComplex = (parts) => {
    const n = parts.length;
    const matchAt = (e, idx, scope) => {
      if (!parts[idx](e, scope)) return false;
      if (idx === 0) return true;
      const comb = parts[idx - 1];
      const prevIdx = idx - 2;
      if (comb === '>') { const p = e.parentNode; return !!p && p.nodeType === ELEMENT_NODE && matchAt(p, prevIdx, scope); }
      if (comb === ' ') { for (let p = e.parentNode; p && p.nodeType === ELEMENT_NODE; p = p.parentNode) if (matchAt(p, prevIdx, scope)) return true; return false; }
      if (comb === '+') { const s = e.previousElementSibling; return !!s && matchAt(s, prevIdx, scope); }
      if (comb === '~') { for (let s = e.previousElementSibling; s; s = s.previousElementSibling) if (matchAt(s, prevIdx, scope)) return true; return false; }
      return false;
    };
    return (e, scope) => matchAt(e, n - 1, scope);
  };
  const nth = (arg, err) => {
    arg = arg.trim().toLowerCase();
    let ofSel = null;
    const of = arg.match(/^(.*?)\s+of\s+(.+)$/);
    if (of) { arg = of[1]; ofSel = compile(of[2]); }
    if (arg === 'odd') return [2, 1, ofSel];
    if (arg === 'even') return [2, 0, ofSel];
    const m = arg.replace(/\s+/g, '').match(/^([+-]?\d*)n([+-]\d+)?$|^([+-]?\d+)$/);
    if (!m) throw err();
    if (m[3] !== undefined) return [0, +m[3], ofSel];
    const a = m[1] === '' || m[1] === '+' ? 1 : m[1] === '-' ? -1 : +m[1];
    return [a, m[2] ? +m[2] : 0, ofSel];
  };
  const nthMatch = (pos, a, b) => a === 0 ? pos === b : (pos - b) / a >= 0 && (pos - b) % a === 0;
  const siblings = (e) => e.parentNode ? e.parentNode._c.filter((c) => c.nodeType === ELEMENT_NODE) : [e];
  const pseudo = (name, arg, err) => {
    switch (name) {
      case 'scope': return (e, scope) => scope ? e === scope : e === docHtml;
      case 'root': return (e) => e === docHtml;
      case 'not': { const m = compile(arg); return (e, s) => !m(e, s); }
      case 'is': case 'matches': case 'where': case '-webkit-any': { const m = compile(arg); return (e, s) => m(e, s); }
      case 'has': {
        const rel = arg.trim();
        const direct = /^[>+~]/.test(rel);
        return (e) => {
          if (!direct) return qs(e, rel, true).length > 0;
          const m = compile(':scope ' + rel);
          if (rel[0] === '>') return qs(e, '*', false).some((c) => m(c, e));
          for (let s = e.nextElementSibling; s; s = s.nextElementSibling) if (m(s, e) || qs(s, '*', false).some((c) => m(c, e))) return true;
          return false;
        };
      }
      case 'first-child': return (e) => !e.previousElementSibling;
      case 'last-child': return (e) => !e.nextElementSibling;
      case 'only-child': return (e) => !e.previousElementSibling && !e.nextElementSibling;
      case 'first-of-type': return (e) => !siblings(e).slice(0, siblings(e).indexOf(e)).some((s) => s.localName === e.localName);
      case 'last-of-type': return (e) => { const s = siblings(e); return !s.slice(s.indexOf(e) + 1).some((x) => x.localName === e.localName); };
      case 'only-of-type': return (e) => siblings(e).filter((s) => s.localName === e.localName).length === 1;
      case 'nth-child': case 'nth-last-child': case 'nth-of-type': case 'nth-last-of-type': {
        const [a, b, ofSel] = nth(arg, err);
        return (e) => {
          let s = siblings(e);
          if (name.endsWith('of-type')) s = s.filter((x) => x.localName === e.localName);
          if (ofSel) { if (!ofSel(e)) return false; s = s.filter((x) => ofSel(x)); }
          if (name.includes('last')) s = s.reverse();
          return nthMatch(s.indexOf(e) + 1, a, b);
        };
      }
      case 'empty': return (e) => !e._c || !e._c.some((c) => c.nodeType === ELEMENT_NODE || (c.nodeType === TEXT_NODE && c._v));
      case 'checked': return (e) => !!e.checked || (e.localName === 'option' && e.selected);
      case 'disabled': return (e) => e.hasAttribute('disabled');
      case 'enabled': return (e) => /^(input|button|select|textarea|option|fieldset)$/.test(e.localName) && !e.hasAttribute('disabled');
      case 'focus': return (e) => e === activeEl;
      case 'focus-within': return (e) => !!activeEl && e.contains(activeEl);
      case 'focus-visible': return (e) => e === activeEl;
      case 'hover': return (e) => !!hovered && e.contains(hovered);
      case 'active': case 'visited': case 'target': case 'fullscreen': case 'modal': case 'popover-open': case 'autofill': case 'placeholder-shown': case 'invalid': case 'user-invalid': case 'indeterminate': return () => false;
      case 'link': case 'any-link': return (e) => /^(a|area)$/.test(e.localName) && e.hasAttribute('href');
      case 'required': return (e) => e.hasAttribute('required');
      case 'optional': return (e) => !e.hasAttribute('required');
      case 'read-only': return (e) => e.hasAttribute('readonly') || !isTextInput(e);
      case 'read-write': return (e) => isTextInput(e) && !e.hasAttribute('readonly');
      case 'valid': case 'defined': return () => true;
      case 'lang': return (e) => { const v = e.closest('[lang]'); return !!v && v.getAttribute('lang').toLowerCase().startsWith(arg.trim().toLowerCase()); };
      case 'dir': return () => arg.trim() === 'ltr';
      default: throw err();
    }
  };

  // ---- HTML parsing / serialization (innerHTML) ----
  const voidTags = new Set(['area', 'base', 'br', 'col', 'embed', 'hr', 'img', 'input', 'link', 'meta', 'param', 'source', 'track', 'wbr']);
  const rawText = new Set(['script', 'style', 'textarea', 'title']);
  const entities = { amp: '&', lt: '<', gt: '>', quot: '"', apos: "'", nbsp: ' ', copy: '©', reg: '®', hellip: '…', mdash: '—', ndash: '–', lsquo: '‘', rsquo: '’', ldquo: '“', rdquo: '”', bull: '•', middot: '·', times: '×', divide: '÷', deg: '°', trade: '™', euro: '€', pound: '£', yen: '¥', cent: '¢', sect: '§', para: '¶', laquo: '«', raquo: '»', larr: '←', rarr: '→', uarr: '↑', darr: '↓', zwj: '‍', zwnj: '‌', thinsp: ' ', ensp: ' ', emsp: ' ' };
  const decodeEntities = (s) => s.indexOf('&') < 0 ? s : s.replace(/&(#x[0-9a-f]+|#\d+|[a-z][a-z0-9]*);?/gi, (m, e) => {
    if (e[0] === '#') { const n = e[1] === 'x' || e[1] === 'X' ? parseInt(e.slice(2), 16) : parseInt(e.slice(1), 10); return n > 0 && n <= 0x10ffff ? String.fromCodePoint(n) : '�'; }
    const v = entities[e.toLowerCase()];
    return v !== undefined ? v : m;
  });
  const parseHTML = (html, svgCtx) => {
    const frag = document.createDocumentFragment();
    const stack = [frag];
    let i = 0;
    const top = () => stack[stack.length - 1];
    const inSvg = () => { for (let k = stack.length - 1; k > 0; k--) { if (stack[k]._svg) return true; if (stack[k].localName === 'foreignobject') return false; } return svgCtx; };
    while (i < html.length) {
      const lt = html.indexOf('<', i);
      if (lt < 0) { top().appendChild(document.createTextNode(decodeEntities(html.slice(i)))); break; }
      if (lt > i) top().appendChild(document.createTextNode(decodeEntities(html.slice(i, lt))));
      i = lt;
      if (html.startsWith('<!--', i)) { const end = html.indexOf('-->', i + 4); top().appendChild(document.createComment(html.slice(i + 4, end < 0 ? html.length : end))); i = end < 0 ? html.length : end + 3; continue; }
      if (html.startsWith('<!', i) || html.startsWith('<?', i)) { const end = html.indexOf('>', i); i = end < 0 ? html.length : end + 1; continue; }
      if (html[i + 1] === '/') {
        const m = html.slice(i).match(/^<\/\s*([^\s>]+)\s*>/);
        if (!m) { top().appendChild(document.createTextNode('<')); i++; continue; }
        const tag = m[1].toLowerCase();
        for (let k = stack.length - 1; k > 0; k--) if (stack[k].localName.toLowerCase() === tag) { stack.length = k; break; }
        i += m[0].length;
        continue;
      }
      const m = html.slice(i).match(/^<([a-zA-Z][^\s/>]*)/);
      if (!m) { top().appendChild(document.createTextNode('<')); i++; continue; }
      i += m[0].length;
      const svg = inSvg() || m[1].toLowerCase() === 'svg';
      const tag = svg ? m[1] : m[1].toLowerCase();
      const el = newElement(svg && tag.toLowerCase() === 'svg' ? 'svg' : tag, svg);
      // attributes
      for (;;) {
        const am = html.slice(i).match(/^\s*([^\s"'>\/=]+)(?:\s*=\s*(?:"([^"]*)"|'([^']*)'|([^\s>]+)))?/);
        if (!am) break;
        i += am[0].length;
        const v = am[2] ?? am[3] ?? am[4] ?? '';
        el.setAttribute(svg ? am[1] : am[1].toLowerCase(), decodeEntities(v));
      }
      const close = html.slice(i).match(/^\s*(\/?)>/);
      const selfClose = close && close[1] === '/';
      i += close ? close[0].length : 0;
      top().appendChild(el);
      if (voidTags.has(tag) || (selfClose && svg)) continue;
      if (rawText.has(tag)) {
        const end = html.toLowerCase().indexOf('</' + tag, i);
        const text = html.slice(i, end < 0 ? html.length : end);
        if (text) el.appendChild(document.createTextNode(tag === 'textarea' || tag === 'title' ? decodeEntities(text) : text));
        i = end < 0 ? html.length : html.indexOf('>', end) + 1;
        continue;
      }
      stack.push(el);
    }
    return frag;
  };
  const escText = (s) => s.replace(/&/g, '&amp;').replace(/</g, '&lt;').replace(/>/g, '&gt;').replace(/ /g, '&nbsp;');
  const escAttr = (s) => s.replace(/&/g, '&amp;').replace(/"/g, '&quot;').replace(/ /g, '&nbsp;');
  const serializeHTML = (n) => {
    if (n.nodeType === TEXT_NODE) return n.parentNode && rawText.has(n.parentNode.localName) && n.parentNode.localName !== 'textarea' ? n._v : escText(n._v);
    if (n.nodeType === COMMENT_NODE) return '<!--' + n._v + '-->';
    if (n.nodeType !== ELEMENT_NODE) return n._c ? n._c.map(serializeHTML).join('') : '';
    let s = '<' + n.localName;
    if (n._a) for (const [k, v] of n._a) { if (v === false || v == null) continue; s += ' ' + k + (v === true ? '=""' : '="' + escAttr(String(v)) + '"'); }
    if (n._st && Object.keys(n._st).length) s += ' style="' + escAttr(serializeCss(n._st)) + '"';
    s += '>';
    if (voidTags.has(n.localName)) return s;
    if (n._c) for (const c of n._c) s += serializeHTML(c);
    return s + '</' + n.localName + '>';
  };
  def(G, 'DOMParser', class DOMParser {
    parseFromString(str, type) {
      const frag = parseHTML(String(str), /xml|svg/.test(type));
      const doc = Object.create(Document.prototype);
      const html = frag.querySelector('html') || (() => { const h = newElement('html'); const b = newElement('body'); h.appendChild(newElement('head')); h.appendChild(b); b.appendChild(frag); return h; })();
      Object.defineProperties(doc, {
        documentElement: { value: html }, body: { value: html.querySelector('body') }, head: { value: html.querySelector('head') },
        querySelector: { value: (s) => html.matches(s) ? html : html.querySelector(s) }, querySelectorAll: { value: (s) => html.querySelectorAll(s) },
        getElementById: { value: (id) => html.querySelector('#' + CSS.escape(id)) },
      });
      return doc;
    }
  });
  def(G, 'XMLSerializer', class XMLSerializer { serializeToString(n) { return serializeHTML(n); } });

  // ---- serialization to the host tree ----
  // fn props of native ("@") nodes: stable ids per (node k, prop path)
  const fnTable = new Map(); // id → fn
  let fnN = 1;
  const fnId = (node, path, fn) => {
    const ids = node._fnIds || (node._fnIds = new Map());
    let id = ids.get(path);
    if (!id) { id = fnN++; ids.set(path, id); }
    fnTable.set(id, fn);
    return id;
  };
  __clack.callFn = (id, args) => {
    const fn = fnTable.get(id);
    if (typeof fn !== 'function') return undefined;
    return fn.apply(undefined, Array.isArray(args) ? args : args === undefined || args === null ? [] : [args]);
  };
  __clack.fnById = (id) => fnTable.get(id);
  const hasOwn = Object.prototype.hasOwnProperty;
  jr.releaseFns = (node) => { if (node._fnIds) { for (const id of node._fnIds.values()) fnTable.delete(id); node._fnIds = null; } };
  const isElement = (v) => v !== null && typeof v === 'object' && (v.$$typeof === Symbol.for('react.transitional.element') || v.$$typeof === Symbol.for('react.element'));
  // plain JSON-able copy of a native component's props
  const nativeProps = (node, v, path, depth) => {
    if (v === null || v === undefined) return v === null ? null : undefined;
    switch (typeof v) {
      case 'function':
        // Go funcs (taut tags them with an own numeric $gofn) and taut's own ids pass through
        if (hasOwn.call(v, '$gofn') && typeof v.$gofn === 'number') return { $gofn: v.$gofn };
        if (hasOwn.call(v, '$fn') && typeof v.$fn === 'number') return { $fn: v.$fn };
        return { $fn: fnId(node, path, v) };
      case 'string': case 'boolean': return v;
      case 'number': return Number.isFinite(v) ? v : null;
      case 'bigint': return String(v);
      case 'symbol': return undefined;
    }
    if (depth > 12) return null;
    if (v instanceof Node) return v.nodeType === ELEMENT_NODE ? { $node: v._k } : null;
    if (Array.isArray(v)) return v.map((x, i) => { const r = nativeProps(node, x, path + '.' + i, depth + 1); return r === undefined ? null : r; });
    if (v instanceof Date) return v.toISOString();
    if (v instanceof Map) return nativeProps(node, Object.fromEntries(v), path, depth + 1);
    if (v instanceof Set) return nativeProps(node, [...v], path, depth + 1);
    if (hasOwn.call(v, '$slot') || hasOwn.call(v, '$gofn') || hasOwn.call(v, '$el')) return v; // already host-tree shaped
    if (typeof v.toJSON === 'function') { try { return nativeProps(node, v.toJSON(), path, depth + 1); } catch { return null; } }
    const out = {};
    for (const k of Object.keys(v)) { const r = nativeProps(node, v[k], path + '.' + k, depth + 1); if (r !== undefined) out[k] = r; }
    return out;
  };
  const register = (n) => { if (!kmap.has(n._k)) { kmap.set(n._k, new WeakRef(n)); kfree.register(n, n._k); } };
  // Serialization produces JSON text directly. Each element caches its JSON
  // in _j; markDirty clears the caches up the ancestor path, so an update
  // re-serializes only the changed path and splices cached siblings.
  const J = JSON.stringify;
  const serChildren = (node) => {
    const c = node._c;
    if (!c || !c.length) return '';
    let out = '';
    for (let i = 0; i < c.length; i++) {
      const x = ser(c[i]);
      if (x) out = out ? out + ',' + x : x;
    }
    return out;
  };
  const ser = (n) => {
    const t = n.nodeType;
    if (t === TEXT_NODE) return n._v === '' ? '' : '{"t":"#","v":' + J(n._v) + '}';
    if (t !== ELEMENT_NODE) return '';
    if (n._j !== undefined) return n._j;
    register(n);
    let out;
    if (n._slot !== undefined) out = ''; // native component slots are folded into "$el" props
    else if (n instanceof NativeElement) {
      let pj = J(n._np ? nativeProps(n, n._np, '', 0) : {});
      let kids = '', slots = '';
      if (n._c) for (const c of n._c) {
        if (c._slot !== undefined) { register(c); slots += (slots ? ',' : '') + J(c._slot) + ':{"$el":[' + serChildren(c) + ']}'; continue; }
        const x = ser(c);
        if (x) kids = kids ? kids + ',' + x : x;
      }
      if (slots) pj = pj === '{}' ? '{' + slots + '}' : pj.slice(0, -1) + ',' + slots + '}';
      out = '{"t":"@","n":' + J(n._n) + ',"k":' + n._k + ',"p":' + pj + (kids ? ',"c":[' + kids + ']' : '') + (n === activeEl ? ',"focus":true' : '') + '}';
    } else {
      const p = {};
      if (n._a) for (const [a, v] of n._a) p[attrName(a, n._svg)] = v;
      if (n._st) p.style = n._st;
      const ln = n.localName;
      if (n._val !== undefined) p.value = n._val;
      if (n._chk !== undefined) p.checked = n._chk;
      if (n._sel !== undefined) p.selected = n._sel;
      if (n._ind) p.indeterminate = true;
      if ((ln === 'input' || ln === 'textarea') && n._selStart !== undefined && n === activeEl) { p.selectionStart = n._selStart; p.selectionEnd = n._selEnd; }
      if (n._sT) p.scrollTop = n._sT;
      if (n._sL) p.scrollLeft = n._sL;
      if (ln === 'textarea' && n._val === undefined) p.value = n.textContent;
      // handled events: React handlers, DOM listeners, on<type> properties
      let on = '';
      const seen = n._h || n[kListeners] || n._onp ? new Set() : null;
      const addOn = (k) => { if (!seen.has(k)) { seen.add(k); on += (on ? ',' : '') + J(k) + ':' + n._k; } };
      if (n._h) for (const k in n._h) addOn(k);
      const ls = n[kListeners];
      if (ls) for (const k of ls.keys()) addOn(k);
      if (n._onp) for (const k in n._onp) if (typeof n._onp[k] === 'function') addOn(k);
      const kids = ln === 'textarea' ? '' : serChildren(n);
      out = '{"t":' + J(ln) + ',"k":' + n._k + ',"p":' + J(p) + (on ? ',"on":{' + on + '}' : '') + (kids ? ',"c":[' + kids + ']' : '') + (n === activeEl ? ',"focus":true' : '') + '}';
    }
    n._j = out;
    return out;
  };
  // object form (tests / debugging)
  const serializeNode = (n) => { const x = ser(n); return x ? JSON.parse(x) : null; };
  // on<type> event handler properties on elements (el.onclick = fn)
  const onProps = ['onclick', 'ondblclick', 'onauxclick', 'oncontextmenu', 'onmousedown', 'onmouseup', 'onmouseenter', 'onmouseleave', 'onmousemove', 'onmouseover', 'onmouseout', 'onpointerdown', 'onpointerup', 'onpointermove', 'onpointerenter', 'onpointerleave', 'onpointerover', 'onpointerout', 'onpointercancel', 'onkeydown', 'onkeyup', 'onkeypress', 'oninput', 'onbeforeinput', 'onchange', 'onfocus', 'onblur', 'onfocusin', 'onfocusout', 'onsubmit', 'onreset', 'onwheel', 'onscroll', 'onload', 'onerror', 'onabort', 'oncancel', 'onclose', 'ontoggle', 'onselect', 'oncopy', 'oncut', 'onpaste', 'ondrag', 'ondragstart', 'ondragend', 'ondragenter', 'ondragleave', 'ondragover', 'ondrop', 'onanimationend', 'onanimationstart', 'ontransitionend', 'ontouchstart', 'ontouchend', 'ontouchmove', 'oncompositionstart', 'oncompositionend', 'oninvalid', 'onresize', 'onplay', 'onpause', 'onended', 'ontimeupdate', 'onloadeddata', 'onloadedmetadata', 'oncanplay'];
  for (const name of onProps) {
    const type = name.slice(2);
    Object.defineProperty(Node.prototype, name, {
      configurable: true, enumerable: false,
      get() { return (this._onp && this._onp[type]) || null; },
      set(fn) { const m = this._onp || (this._onp = Object.create(null)); if (typeof fn === 'function') m[type] = fn; else delete m[type]; markDirty(this); },
    });
  }
  for (const [name, field] of [['scrollTop', '_sT'], ['scrollLeft', '_sL']]) {
    Object.defineProperty(Element.prototype, name, {
      configurable: true, enumerable: false,
      get() { return this[field] || 0; },
      set(v) { v = +v || 0; if (v < 0) v = 0; if (this[field] === v) return; this[field] = v; markDirty(this); },
    });
  }
  // DOM listeners change a node's "on" set
  // window/document listeners are reported to Go (Runtime.WantsEvent), so
  // the UI only forwards global input (mousemove, keydown…) when JS listens
  jr.onListen = (target, type, added) => {
    if (target === G || target === document) H.listen(type, added ? 1 : -1);
    else if (target instanceof Node) markDirty(target);
  };
  jr.serializeNode = serializeNode;

  // roots: id → container element
  const roots = new Map();
  jr.roots = roots;
  jr.newContainer = (id) => {
    const c = newElement('div');
    c._rootId = id;
    roots.set(id, c);
    return c;
  };
  jr.dropContainer = (id) => {
    const c = roots.get(id);
    roots.delete(id);
    if (c) { dirtyRoots.delete(c); jr.releaseFns(c); }
  };
  const collectCss = () => {
    const css = [];
    for (const s of qs(document, 'style', false)) { if (!s.closest('[data-clack-root]')) css.push(s.textContent); }
    for (const sh of document.adoptedStyleSheets || []) if (sh && sh._text) css.push(sh._text);
    return css;
  };
  let lastBody = '[]', lastCss = '[]', lastHtmlP = '', lastBodyP = '';
  // attributes and inline style of <html> / <body> (classes, CSS variables)
  const rootProps = (n) => {
    const p = {};
    if (n._a) for (const [a, v] of n._a) p[attrName(a, false)] = v;
    if (n._st) p.style = n._st;
    return p;
  };
  // a new subscriber gets the current state on the next flush
  __clack.resendDocument = () => { lastBody = lastCss = lastHtmlP = lastBodyP = null; docDirty = true; queueFlush(); };
  __clack.flush = () => {
    flushQueued = false;
    if (dirtyRoots.size) {
      const list = [...dirtyRoots];
      dirtyRoots.clear();
      for (const c of list) {
        if (!roots.has(c._rootId)) continue;
        H.commit(c._rootId, '[' + serChildren(c) + ']');
      }
    }
    if (docDirty) {
      docDirty = false;
      const body = '[' + serChildren(docBody) + ']', css = JSON.stringify(collectCss());
      if (body !== lastBody || css !== lastCss) { lastBody = body; lastCss = css; H.document(body, css); }
      const hp = J(rootProps(docHtml)), bp = J(rootProps(docBody));
      if (hp !== lastHtmlP || bp !== lastBodyP) { lastHtmlP = hp; lastBodyP = bp; H.documentProps(hp, bp); }
    }
  };

  // ---- globals ----
  const globals = {
    Node, CharacterData, Text, Comment, DocumentFragment, Element, HTMLElement, SVGElement, Document, HTMLDocument: Document,
    DOMTokenList, DOMRect, DOMRectReadOnly, DOMPoint, DOMPointReadOnly: DOMPoint, DOMMatrix, DOMMatrixReadOnly: DOMMatrix,
    UIEvent, MouseEvent, PointerEvent, WheelEvent, KeyboardEvent, FocusEvent, InputEvent, CompositionEvent, ClipboardEvent, DragEvent, TouchEvent,
    SubmitEvent, AnimationEvent, TransitionEvent, ToggleEvent, StorageEvent, HashChangeEvent, PopStateEvent, BeforeUnloadEvent, DataTransfer,
    MutationObserver, WebKitMutationObserver: MutationObserver, ResizeObserver, IntersectionObserver, MediaQueryList, Animation,
    Range, Selection: Object, TreeWalker, NodeIterator: TreeWalker, CanvasRenderingContext2D, OffscreenCanvas, OffscreenCanvasRenderingContext2D: CanvasRenderingContext2D,
    Image: ImageCtor, Audio: AudioCtor, Option: OptionCtor, AudioContext, webkitAudioContext: AudioContext, OfflineAudioContext: AudioContext,
    MediaStream, MediaRecorder, CSSStyleSheet, CSSStyleDeclaration: Object, NodeList: Array, HTMLCollection: Array,
    ShadowRoot: DocumentFragment, Attr: Object,
  };
  for (const [k, v] of Object.entries(globals)) def(G, k, v);
  for (const [k, v] of Object.entries(htmlClasses)) if (v) def(G, k, v);
  for (const [k, v] of Object.entries(svgClasses)) def(G, k, v);
  // window is the end of every event path
  def(G, '_parentForEvents', () => null);
})();
