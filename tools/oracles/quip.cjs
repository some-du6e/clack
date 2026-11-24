// quip.cjs: Slack's own Quip modules (web client build 5718bdb1b28b,
// 2026-11-23) on internal/quip's parity snapshots: the protobuf encoder and
// decoder (p8gD.toBinary / fromBinary over QkBk's syncer.* and o3+F's
// proto.section.* classes), order keys (0fa5.generate), RTML text
// (W77Y.escapeText, isSafeUri), RTML parsing (fce5.getTextAndMarks) and
// writing (the editor's getCompactRtml).
//
//   QUIP_CHUNKS=<webapp chunk>,<editor chunk> JSDOM=<dir with node_modules/jsdom> \
//     node tools/oracles/quip.cjs KIND CASES.json > SLACK.json
//
// KIND is a snapshot's slack.refresh (quip-wire-encode, quip-wire-decode,
// quip-order, quip-rtml-text, quip-rtml-parse, quip-rtml-write). CASES.json
// is the snapshot's cases; the output maps each case name to Slack's
// value. tools/parity.py --refresh runs it.
//
// The chunks are gantry-v2-async-quip-collab-webapp.b0a4464d07be5505.min.js
// and gantry-v2-async-quip-collab-editor.2b9ea24acdaf8782.min.js from the
// public CDN (https://a.slack-edge.com/bv1-13-br/), fetched without a
// cookie; this repo doesn't redistribute them. jsdom 24.1.3 gives the RTML
// parser its DOM (W77Y walks a real document, as in the browser).
'use strict';
const fs = require('fs');
const path = require('path');

const SCRATCH = '/root/clack-ops/scratch/canvas-edit';
const CHUNKS = (process.env.QUIP_CHUNKS || [
  SCRATCH + '/b2/gantry-v2-async-quip-collab-webapp.b0a4464d07be5505.min.js',
  SCRATCH + '/b2/gantry-v2-async-quip-collab-editor.2b9ea24acdaf8782.min.js',
].join(',')).split(',');
const JSDOM_DIR = process.env.JSDOM || SCRATCH + '/npm';
// Modules the collab chunks import but don't hold, taken from the client's
// boot chunk (gantry-v2-async-client-boot-shared.29563e274bf72a20.min.js,
// same CDN): e9rp is the emoji-name pattern 2U5W.parseSlackEmojiId tests an
// emoji control's id with. Only these ids come from it; the rest stay stubs.
const BOOT = process.env.QUIP_BOOT_CHUNK || SCRATCH + '/cdn-boot/gantry-v2-async-client-boot-shared.29563e274bf72a20.min.js';
const BOOT_MODULES = ['e9rp'];

// --- a browser for the modules: jsdom's window as the global scope
const { JSDOM } = require(path.join(JSDOM_DIR, 'node_modules', 'jsdom'));
const dom = new JSDOM('<!DOCTYPE html><html><head></head><body></body></html>', { url: 'https://app.slack.com/client/T0/C0' });
const win = dom.window;
for (const k of Object.getOwnPropertyNames(win)) {
  if (k in globalThis) continue;
  try { Object.defineProperty(globalThis, k, { value: win[k], configurable: true, writable: true }); } catch (e) { /* not copyable */ }
}
for (const k of ['window', 'self', 'document', 'navigator', 'location', 'localStorage', 'sessionStorage']) {
  try { Object.defineProperty(globalThis, k, { value: k === 'window' || k === 'self' ? globalThis : win[k], configurable: true, writable: true }); } catch (e) { /* keep node's */ }
}
for (const k of ['addEventListener', 'removeEventListener', 'dispatchEvent', 'getComputedStyle', 'getSelection']) {
  if (typeof win[k] === 'function') globalThis[k] = win[k].bind(win);
}
globalThis.requestAnimationFrame = f => setTimeout(f, 0);
globalThis.matchMedia = () => ({ matches: false, addListener() {}, removeListener() {}, addEventListener() {}, removeEventListener() {} });
const quiet = console.warn;
console.warn = () => {}; // Quip's logger warns on bad input (order.generate_), as in the browser
console.log = console.info = console.debug = () => {}; // and its diagnostics log at load; results go to stdout below

// --- an rspack runtime over the chunks: a module runs its own factory
const START = /[{,](?:"([^"]{1,12})"|([A-Za-z0-9_$]{1,12})):\(function\(/g;
const texts = CHUNKS.map(f => fs.readFileSync(f, 'utf8'));
const where = new Map();
const starts = texts.map((t, fi) => {
  const st = [];
  START.lastIndex = 0;
  let m;
  while ((m = START.exec(t))) st.push([m.index, m[1] || m[2]]);
  st.forEach(([, id], si) => { if (!where.has(id)) where.set(id, [fi, si]); });
  return st;
});
{
  const t = fs.readFileSync(BOOT, 'utf8'), st = [];
  START.lastIndex = 0;
  let m;
  while ((m = START.exec(t))) st.push([m.index, m[1] || m[2]]);
  texts.push(t);
  starts.push(st);
  st.forEach(([, id], si) => { if (BOOT_MODULES.includes(id) && !where.has(id)) where.set(id, [texts.length - 1, si]); });
  for (const id of BOOT_MODULES) if (!where.has(id)) throw new Error('the boot chunk has no module ' + id);
}
function factoryOf(id) {
  const w = where.get(id);
  if (!w) return null;
  const [fi, si] = w;
  const t = texts[fi], st = starts[fi];
  const begin = st[si][0] + 1;
  // the module ends where a later module starts; a match inside the module
  // doesn't parse, so try the following starts in turn
  for (let j = si + 1; j < st.length && j < si + 400; j++) {
    try {
      const o = (0, eval)('({' + t.slice(begin, st[j][0]) + '})');
      const ks = Object.keys(o);
      if (ks.length === 1) return o[ks[0]];
    } catch (e) { /* not the end yet */ }
  }
  throw new Error('cannot cut module ' + id);
}
const cache = new Map();
// A module the chunks don't have is a stub. The flat stub's exports are
// functions returning undefined (what the codec kinds have always run
// with); deepStubs (the save kinds, whose modules import React and the
// editor's views) makes it a callable proxy that returns more of itself,
// so code around the path being run can construct and call what it likes.
let deepStubs = process.env.QUIP_DEEP_STUBS === '1' || /^quip-save/.test(process.argv[2] || '');
function deepStub(name) {
  const fn = function () {};
  return new Proxy(fn, {
    get: (t, k) => {
      if (k === '__esModule') return true;
      if (k === 'then') return undefined;
      if (k === Symbol.toPrimitive) return () => '';
      if (typeof k === 'symbol') return undefined;
      if (k === 'prototype') return t.prototype;
      return deepStub(name + '.' + String(k));
    },
    apply: () => deepStub(name + '()'),
    construct: () => deepStub('new ' + name),
    set: () => true,
  });
}
function stub(id) {
  if (deepStubs) return deepStub(id);
  const fn = function () { return undefined; };
  return new Proxy({}, { get: (_, k) => (k === '__esModule' ? true : typeof k === 'symbol' || k === 'then' ? undefined : fn) });
}
function req(id) {
  if (cache.has(id)) return cache.get(id).exports;
  if (!where.has(id)) {
    const m = { exports: stub(id) };
    cache.set(id, m);
    return m.exports;
  }
  const m = { id, exports: {}, loaded: false };
  cache.set(id, m);
  factoryOf(id).call(m.exports, m, m.exports, req);
  m.loaded = true;
  return m.exports;
}
req.d = (ex, getters, values) => {
  for (const k in getters || {}) if (!Object.prototype.hasOwnProperty.call(ex, k)) Object.defineProperty(ex, k, { enumerable: true, get: getters[k] });
  for (const k in values || {}) if (!Object.prototype.hasOwnProperty.call(ex, k)) Object.defineProperty(ex, k, { enumerable: true, value: values[k], writable: true });
};
req.r = ex => { Object.defineProperty(ex, '__esModule', { value: true }); };
req.n = m => { const g = m && m.__esModule ? () => m.default : () => m; req.d(g, { a: g }); return g; };
req.o = (o, p) => Object.prototype.hasOwnProperty.call(o, p);
req.g = globalThis;
req.p = '';
req.u = id => id + '.js';
req.nmd = m => m;
req.hmd = m => m;

// --- message classes by full name, from QkBk and o3+F and every type
// their fields reach
const classes = new Map();
function addClass(c) {
  let d;
  try { d = c.getDescriptor(); } catch (e) { return; }
  const name = d.getFullName();
  if (classes.has(name)) return;
  classes.set(name, c);
  for (const f of d.getFields()) {
    if (f.isCompositeType()) {
      const nt = f.getNativeType();
      if (nt && typeof nt.getDescriptor === 'function') addClass(nt);
    }
  }
}
function walk(o, seen) {
  if (!o || (typeof o !== 'object' && typeof o !== 'function') || seen.has(o)) return;
  seen.add(o);
  if (typeof o === 'function' && typeof o.getDescriptor === 'function') addClass(o);
  for (const k of Object.getOwnPropertyNames(o)) {
    if (['prototype', 'caller', 'arguments', 'descriptor_'].includes(k)) continue;
    let v;
    try { v = o[k]; } catch (e) { continue; }
    if (v && (typeof v === 'object' || typeof v === 'function') && !Array.isArray(v)) walk(v, seen);
  }
}
const seen = new Set();
for (const id of ['QkBk', 'o3+F', '5SNU']) walk(req(id), seen);
const pbu = req('p8gD');

const INT64 = new Set([3, 4, 6, 16, 18]);
const BYTES = 12;
function hexOf(b) { return Buffer.from(b).toString('hex'); }
function classOf(type) {
  const c = classes.get(type);
  if (!c) throw new Error('no message ' + type);
  return c;
}

// build makes a message from a tree keyed by field number: a message is
// an object, a repeated field an array, bytes a hex string.
function build(cls, tree) {
  const msg = new cls();
  const d = cls.getDescriptor();
  for (const [k, v] of Object.entries(tree)) {
    const f = d.findFieldByTag(Number(k));
    if (!f) throw new Error(d.getFullName() + ' has no field ' + k);
    const one = x => {
      if (f.isCompositeType()) return build(f.getNativeType(), x);
      if (f.getFieldType() === BYTES) return new Uint8Array(Buffer.from(x, 'hex'));
      if (INT64.has(f.getFieldType()) && f.getNativeType() === String) return String(x);
      return x;
    };
    if (f.isRepeated()) {
      if (!Array.isArray(v)) throw new Error(d.getFullName() + '.' + k + ' is repeated');
      for (const x of v) msg.add$Value(Number(k), one(x));
    } else msg.set$Value(Number(k), one(v));
  }
  return msg;
}

const order = req('0fa5');
const W77Y = req('W77Y');
const fce5 = req('fce5');

// getCompactRtml lives inside the editor's 1 MB module; it is cut out as
// written and run with the two names it reads (dHCX for sortBy, a logger).
function compactRtml() {
  const t = texts.find(x => x.includes('function getCompactRtml('));
  const i = t.indexOf('function getCompactRtml(');
  const j = t.indexOf('function getMarkOrNull(', i);
  const src = t.slice(i, j);
  const names = [...new Set([...src.matchAll(/\b([A-Za-z_$][A-Za-z0-9_$]{0,3})\.(sortBy)\b/g)].map(m => m[1]))];
  const logs = [...new Set([...src.matchAll(/\b([A-Za-z_$][A-Za-z0-9_$]{0,3})\.S\.error\b/g)].map(m => m[1]))];
  if (names.length !== 1 || logs.length !== 1) throw new Error('getCompactRtml reads ' + names + ' ' + logs);
  return new Function(names[0], logs[0], src + '\nreturn getCompactRtml;')(req('dHCX'), { S: { error() {} } });
}

const KINDS = {
  'quip-wire-encode': c => hexOf(pbu.Rm(build(classOf(c.in.type), c.in.fields))),
  'quip-wire-decode': c => {
    let msg;
    try {
      msg = pbu.Er(new Uint8Array(Buffer.from(c.in.hex, 'hex')), classOf(c.in.type));
    } catch (e) {
      return { error: String(e.message || e) };
    }
    return { hex: hexOf(pbu.Rm(msg)) };
  },
  'quip-order': c => order.cM(c.in.prev, c.in.next),
  'quip-rtml-text': c => (c.in.fn === 'escapeText' ? W77Y.X1(c.in.s, c.in.flag) : c.in.fn === 'isSafeUri' ? W77Y.MG(c.in.s, c.in.flag) : (() => { throw new Error('fn ' + c.in.fn); })()),
  'quip-rtml-parse': c => fce5.N(c.in.rtml),
  'quip-rtml-write': (() => {
    let f;
    return c => {
      f = f || compactRtml();
      // a round trip case is Slack's own parse, then its writer
      const tm = c.in.rtml !== undefined ? fce5.N(c.in.rtml) : { text: c.in.text, marks: c.in.marks.map(m => ({ ...m })) };
      return f(tm);
    };
  })(),
};


// ---- saves (deep stubs): Slack's own section entity (EKJU's class over
// ZRIn's) builds the update bundle. The entity is made from its prototype
// and given the state its toPb and toUpdateBundle read, from the server's
// section as Slack's update() would load it; the editor's DOM is what this
// can't run, so the content the editor would hand toPb is the case's.
// Which fields an edit dirties is Slack's setters' (EKJU): typing
// setDirty([CONTENT, STATUS]) (the input path), a checkbox
// setSectionAttribute -> [STATUS] then [ATTRIBUTES], deleteSection [STATUS]
// with deleteEntity [DELETED].
// A restyle is setSectionStyle: [STYLE] (TYPE only when the content's
// type changes, never between text styles); a list level is
// setSectionAttribute("indentation"): [STATUS], then [ATTRIBUTES]. A case
// with several (typing and a restyle, say) names its dirty fields.
const DIRTY = { text: [12, 15], check: [15, 16], delete: [3, 15], block: [] };
function saveRuntime() {
  if (saveRuntime.r) return saveRuntime.r;
  const EKJU = req('EKJU'), nTyX = req('nTyX'), u = req('7yDq'), WJQ2 = req('WJQ2');
  saveRuntime.r = { Sec: EKJU.wn, nTyX, u, J: WJQ2.J };
  return saveRuntime.r;
}
function setSaveCounter(u, session, saveSeq) {
  u.mY(session);
  if (u.Zv() > saveSeq) throw new Error('save sequence ' + saveSeq + ' after ' + u.Zv() + ': order the cases by saveSeq');
  while (u.Zv() < saveSeq) u.K();
}
function sectionEntity(rt, c) {
  const S = classOf('syncer.Section');
  const pb = build(S, c.in.server);
  const e = Object.create(rt.Sec.prototype);
  const stub = deepStub('section');
  Object.assign(e, {
    id_: pb.getId(), isBeingDeleted_: false, isReadOnly_: pb.getReadOnlyOrDefault(), dirtyFields: new Set(c.in.edit.dirty || DIRTY[c.in.edit.kind]),
    pendingFields: new Set(), sequence_: Number(pb.getSequenceOrDefault()), localSequence_: c.in.localSeq, transientSequence_: 0,
    isProgrammaticUpdate_: false, isDeleted_: pb.getDeletedOrDefault() || c.in.edit.kind === 'delete', dirtyState_: 2,
    document: stub, contentOrigin_: pb.getContentOriginOrDefault(), knownFields: rt.nTyX.Ps.concat(rt.nTyX.fP || [], rt.nTyX.mV || []),
    position_: pb.getPositionOrDefault(), sectionStyle_: c.in.edit.style ?? pb.getStyleOrDefault(), parents_: [], missingParentIds_: [],
    hasLayoutParent_: pb.hasHasLayoutParent() ? pb.getHasLayoutParent() : undefined, savedDefaultContent_: null,
    persistOnNextChange_: false, layoutParent_: null, sectionContent_: stub,
  });
  // update() loads attributes with Slack's own conversion
  e.attributes_ = rt.Sec.prototype.convertAttributesPbToMap.call(e, pb.getAttributesOrDefault());
  if (c.in.edit.kind === 'check' || 'checked' in c.in.edit) e.attributes_.set('checked', c.in.edit.checked);
  if ('indent' in c.in.edit) e.attributes_.set('indentation', c.in.edit.indent);
  for (const id of pb.getParentsOrDefault().idsArray()) {
    e.parents_.push({ id: () => id, sectionStyle: () => c.in.parentStyle });
  }
  let content = pb.getContent();
  if (c.in.edit.kind === 'text' || c.in.edit.rtml !== undefined) {
    // the content toPb makes: the title's (createPbFn of the RTML, then
    // the cover when there is one), a text section's (its RTML, and the
    // code language the content holds)
    const Content = classOf('proto.section.Section.Content');
    const old = pb.getContentOrDefault();
    content = new Content();
    if (pb.getTypeOrDefault() === 64) {
      const t = new (classOf('proto.section.Section.ContentTitle'))().setText(c.in.edit.rtml);
      if (old.getTitleOrDefault().hasHeaderImage()) t.setHeaderImage(old.getTitle().getHeaderImage());
      content.setTitle(t);
    } else {
      const t = new (classOf('proto.section.Section.ContentText'))().setText(c.in.edit.rtml);
      if (old.getTextOrDefault().hasLanguage()) t.setLanguage(old.getText().getLanguage());
      content.setText(t);
    }
  }
  e.toContentPb = () => content;
  e.type = () => pb.getTypeOrDefault(); // the section content's type (its class, made from the server's type)
  e.positionPath = () => pb.getPositionPathOrDefault();
  // plain content, as Clack saves: no CRDT copy (otherwise toPb's deleted
  // comes from crdt.currentSaveDeleted, which the stubs would make up)
  e.isCrdtEnabled = () => false;
  e.checkDirtyFieldInvariants = () => {}; // metrics only
  e.validateFsck_ = () => {};
  e.validateCrdt_ = () => {};
  return { e, pb };
}
KINDS['quip-save-section'] = c => {
  const rt = saveRuntime();
  setSaveCounter(rt.u, c.in.session, c.in.saveSeq);
  const { e, pb } = sectionEntity(rt, c);
  const b = e.toUpdateBundle();
  // createUpdateBundle: the activity row, against the server's copy
  const get = rt.J.get;
  rt.J.get = () => pb;
  try {
    const a = rt.J.shouldAddToActivityStack(b, c.in.team);
    if (a !== null) b.setActivityStackUpdate(a);
  } finally {
    rt.J.get = get;
  }
  return { hex: hexOf(pbu.Rm(b)), localSeq: e.localSequence_ };
};
// A new section, as Slack's editor makes one after another section
// (K2FO.createNewSection): a fresh entity (sequence 0, the constructor's
// content origin, USER_EDIT), every field dirty (setDirty(nTyX.mA)),
// vertical_margin false through EKJU's own setSectionAttribute, then
// K2FO.moveSiblingTo's initSibling from the section it follows (its
// parents, indentation, paragraph indentation; text alignment under the
// canvas editor's suppressTextAlignment, on by default: !autoTextAlignment).
// The editor hands toPb the new section's content; a ticked checklist item
// gets its box through setSectionAttribute too. Then toUpdateBundle, and
// the activity row against no server copy.
KINDS['quip-save-new-section'] = c => {
  const rt = saveRuntime();
  setSaveCounter(rt.u, c.in.session, c.in.saveSeq);
  const K2FO = req('K2FO');
  const S = classOf('syncer.Section');
  const origin = classOf('proto.section.Section').ContentOrigin.USER_EDIT;
  const after = build(S, c.in.after);
  const doc = new Proxy(deepStub('document'), {
    get: (t, k) => k === 'editorConfig' ? () => ({ suppressTextAlignment: true }) : t[k],
  });
  const parentStub = id => ({ id: () => id, sectionStyle: () => c.in.parentStyle, position: () => c.in.parentPosition, parents_: [],
    sectionContent: () => ({ allowsNewChild: () => true }) });
  // the section it follows: its attributes as update() loads them
  const a = Object.create(rt.Sec.prototype);
  Object.assign(a, { id_: after.getId(), document: doc, parents_: after.getParentsOrDefault().idsArray().map(parentStub),
    layoutParent_: null, position_: after.getPositionOrDefault() });
  a.attributes_ = rt.Sec.prototype.convertAttributesPbToMap.call(a, after.getAttributesOrDefault());
  a.parents = () => a.parents_;
  a.layoutParent = () => null;
  // the new one
  const e = Object.create(rt.Sec.prototype);
  const stub = deepStub('section');
  Object.assign(e, {
    id_: c.in.id, isBeingDeleted_: false, isReadOnly_: false, dirtyFields: new Set(rt.nTyX.mA),
    pendingFields: new Set(), sequence_: 0, localSequence_: 0, transientSequence_: 0,
    isProgrammaticUpdate_: false, isDeleted_: false, dirtyState_: 2,
    document: doc, contentOrigin_: origin, knownFields: rt.nTyX.Ps.concat(rt.nTyX.fP || [], rt.nTyX.mV || []),
    position_: '', sectionStyle_: c.in.style, parents_: [], missingParentIds_: [],
    hasLayoutParent_: undefined, savedDefaultContent_: null,
    persistOnNextChange_: false, layoutParent_: null, sectionContent_: stub,
  });
  e.attributes_ = new Map();
  e.fireAttributeChangeListeners_ = () => {};
  e.setDirty = f => f.forEach(x => e.dirtyFields.add(x));
  e.setPosition = p => { e.position_ = p; };
  e.setParents = ps => { e.parents_ = ps.slice(); };
  e.layoutParent = () => null;
  e.sectionStyle = () => c.in.style;
  e.setSectionAttribute('vertical_margin', false); // createNewSectionAtPosition, no id given
  K2FO.j8(a, e, c.in.position); // moveSiblingTo: setPosition, initSibling
  if (c.in.checked) e.setSectionAttribute('checked', true);
  const Content = classOf('proto.section.Section.Content');
  const content = new Content().setText(new (classOf('proto.section.Section.ContentText'))().setText(c.in.rtml));
  e.toContentPb = () => content;
  e.type = () => classOf('proto.section.Section').Type.TEXT_TYPE;
  e.positionPath = () => (c.in.parentPosition ? c.in.parentPosition + '-' : '') + e.position_;
  // EKJU: null !== sectionContent().overrideThreadId(), which a text
  // section's content never has (only a copied live app's)
  e.shouldSaveCopyThreadData = () => false;
  // plain content, as Clack saves: no CRDT copy (otherwise toPb's deleted
  // comes from crdt.currentSaveDeleted, which the stubs would make up)
  e.isCrdtEnabled = () => false;
  e.checkDirtyFieldInvariants = () => {};
  e.validateFsck_ = () => {};
  e.validateCrdt_ = () => {};
  const b = e.toUpdateBundle();
  const get = rt.J.get;
  rt.J.get = () => null; // no server copy: a new section
  try {
    const r = rt.J.shouldAddToActivityStack(b, c.in.team);
    if (r !== null) b.setActivityStackUpdate(r);
  } finally {
    rt.J.get = get;
  }
  return { hex: hexOf(pbu.Rm(b)), localSeq: e.localSequence_ };
};
// The title metadata extractTitleMetadata(true) gives every save, run as
// Slack's document model has it (the method cut from the webapp chunk's
// nZa/ module, over its own imports: W77Y, u21E, 30B3, fmyk, the class's
// MAX_TITLE_LENGTH): the title section exported (CITi's lE over
// W77Y.parse, as getRtmlContent_ runs it on the title's DOM; the RTML
// stands for that DOM), its text, the title rich text
// (30B3.convertSectionsToRichTextApiPb) and the emoji info of the title's
// controls (referencedControls: W77Y's control id collector over the
// RTML), each as Slack's protobuf encoder writes it.
const titleFns = (() => {
  let f;
  return () => {
    if (f) return f;
    const web = texts[0], ed = texts[1];
    const i = web.indexOf('extractTitleMetadata(e,t){let n=e;');
    if (i < 0) throw new Error('extractTitleMetadata moved');
    let d = 0, k = web.indexOf('{', i);
    for (; k < web.length; k++) {
      if (web[k] === '{') d++;
      else if (web[k] === '}' && --d === 0) break;
    }
    const max = /static\{this\.MAX_TITLE_LENGTH=(\d+)\}/.exec(web);
    if (!max) throw new Error('MAX_TITLE_LENGTH moved');
    const method = 'function ' + web.slice(i, k + 1);
    const extract = new Function('ew', 'P', 'nB', 'c', 'il', 'ii', 'R', 'v', 'return ' + method)(
      req('W77Y'), req('u21E'), req('30B3'), req('fmyk'), { MAX_TITLE_LENGTH: +max[1] }, { t: s => s }, null, null);
    // the editor chunk's emojiInfoOf (esJb), over o3+F
    const j = ed.indexOf('function emojiInfoOf(e,t,n){');
    const je = ed.indexOf('return r&&i.setDisplayUrl(r),i}', j);
    if (j < 0 || je < 0) throw new Error('emojiInfoOf moved');
    const emojiInfoOf = new Function('i8', 'return ' + ed.slice(j, je + 'return r&&i.setDisplayUrl(r),i}'.length))(req('o3+F'));
    f = { extract, emojiInfoOf };
    return f;
  };
})();
// A title's controls (c.in.controls, by id): stand-ins for the editor's
// control objects with what extractTitleMetadata and 30B3 read of each,
// their altRtml as the editor's control contents write it (user and
// channel: the rendered link, its label "user" / "channel" / "private
// channel" when not for an external paste; emoji ":name:" of its stored
// name; date dJkV.lC's text; broadcast "<!range>"). An emoji's k.info is
// the emoji store's model of it (uu.U), absent while not loaded;
// getEmojiInfo is emojiInfoOf over it, as the editor's emoji control does.
function titleControl(id, k) {
  const kind = k.kind === 'usergroup' ? 'user' : k.kind;
  const is = want => () => kind === want;
  const model = k.info && {
    getTeamIdOrDefault: () => k.info.teamId || '', getDisplayName: () => k.info.displayName,
    getUnicode: () => k.info.unicode, getDisplayUrl: () => k.info.displayUrl,
  };
  const content = {
    altRtml(e, t, n) {
      switch (kind) {
        case 'user': return '<a href="https://example.slack.com/team/' + k.id + '">@user</a>';
        case 'channel': return '<a href="https://example.slack.com/archives/' + k.id + '">#' + (k.private ? 'private channel' : 'channel') + '</a>';
        case 'emoji': return k.name ? ':' + k.name + ':' : '';
        case 'date': {
          const txt = req('dJkV').lC(k.ms, { verbose: false, dynamic: false, useExcludeYearHelper: true });
          return (n || e) && k.ms ? `<span data-slack-date-timestamp='${k.ms}'>${txt}</span>` : txt;
        }
        case 'broadcast': return k.range ? `<!${k.range}>` : '';
      }
      return '';
    },
    getSyncerObject() {
      return { slackUserId: () => k.id, slackChannelId: () => k.id, id: () => k.slackEmojiId || '' };
    },
    chipLabel() { return undefined; },
    getEmojiInfo() { return model ? titleFns().emojiInfoOf(model, k.name, k.fallbackUrl) : null; },
    getHeldEmojiInfo() { return null; },
    getUnloadableEmojiName() { return null; },
    getPlaceholderText() { return null; },
    getUtcDateMs() { return k.ms; },
    getBroadcastRange() { return k.range; },
  };
  const none = () => false;
  return {
    isControl: () => true, id: () => id, controlContent: () => content,
    isSlackUser: is('user'), isSlackChannel: is('channel'), isSlackEmoji: is('emoji'), isSlackDate: is('date'), isSlackBroadcast: is('broadcast'),
    isSlackTemplate: none, isSlackSearchFilter: none, isSlackSalesforceDataMention: none, isSlackUnfurl: none, isSlackDocument: none,
    isSlackFile: none, isSlackList: none, isSlackListRecord: none,
  };
}

KINDS['quip-save-title'] = c => {
  const CITi = req('CITi'), W = req('W77Y');
  const controls = {};
  for (const [id, k] of Object.entries(c.in.controls || {})) controls[id] = titleControl(id, k);
  const lookup = { lookupControl: (node, id) => (controls[id] ? { id, control: controls[id] } : null) };
  const exportOf = target => {
    const o = { fromLocalDisplay: true, forExternalPaste: target === 1 || target === 6, dropEmojiControls: target === 6, forRichText: target === 5 };
    const h = new CITi.lE(o, false, null);
    W.qg(c.in.rtml, h, lookup);
    return '<h1>' + h.output() + '</h1>';
  };
  const ids = new W.lz();
  W.qg(c.in.rtml, ids, null);
  const content = {
    exportToHtml: o => exportOf(o && o.exportTarget),
    referencedControls: () => ids.controlIds.map(id => controls[id]).filter(Boolean),
  };
  const section = { isDeleted: () => false, isTitle: () => true, sectionContent: () => content };
  const doc = { sections: () => [section], getControlForId: id => controls[id] || null };
  const m = titleFns().extract.call(doc, true);
  return { title: m.title, richText: hexOf(pbu.Rm(m.titleRichText)), emojiInfo: m.emojiInfo.map(x => hexOf(pbu.Rm(x))) };
};
// Slack's save writer over a section's RTML: CITi.F0 (renderToString,
// the DI handler over W77Y's parse of the DOM the RTML builds). A case
// holds when the RTML Clack wrote comes back unchanged: Slack's editor,
// loading it and saving it as it is, would send those bytes.
KINDS['quip-apply-runs'] = c => {
  if (c.in.rtml === '(refused)') return '(refused)';
  return req('CITi').F0(c.in.rtml, null, {});
};
// A code block's RTML of typed text, and its text again: 32AJ's
// plainTextToCodeRtml and rtmlToPlainText (with its W77Y handler), module
// private, so taken from the webapp chunk as written and run over 32AJ's
// own modules (/XUt, W77Y).
const codeFns = (() => {
  let f;
  return () => {
    if (f) return f;
    const src = fs.readFileSync(CHUNKS[0], 'utf8');
    const i = src.indexOf('"32AJ":(function(e,t,n){');
    const a = src.indexOf('class u extends r.mM{', i);
    const b = src.indexOf('function canEdit(e)', a);
    if (i < 0 || a < 0 || b < 0) throw new Error('32AJ: the code functions moved');
    const body = src.slice(a, b) + '; return {plainTextToCodeRtml, rtmlToPlainText};';
    f = new Function('r', 's', body)(req('W77Y'), req('/XUt'));
    return f;
  };
})();
KINDS['quip-code'] = c => {
  const { plainTextToCodeRtml, rtmlToPlainText } = codeFns();
  const rtml = plainTextToCodeRtml(c.in.text);
  return { rtml, text: rtmlToPlainText(rtml) }; // a code section's getContent() is its RTML
};
module.exports = { req, classes, classOf, build, hexOf, pbu, KINDS };
if (require.main === module) {
  const [kind, file] = process.argv.slice(2);
  if (!KINDS[kind] || !file) {
    quiet('usage: quip.cjs KIND CASES.json, KIND one of ' + Object.keys(KINDS).join(' '));
    process.exit(2);
  }
  const out = {};
  for (const c of JSON.parse(fs.readFileSync(file, 'utf8'))) out[c.name] = KINDS[kind](c);
  process.stdout.write(JSON.stringify(out) + '\n');
}
