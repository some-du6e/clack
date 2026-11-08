// web.js: the Web platform APIs, in plain JS over env.js and the Go host
// functions (__jr.host). Evaluated after env.js, before intl.js/dom.js.
// URL/URLSearchParams, TextEncoder/TextDecoder, atob/btoa, Blob/File/
// FileReader/FormData, streams, Headers/Request/Response/fetch,
// AbortController/AbortSignal, XMLHttpRequest, crypto, Storage,
// BroadcastChannel, MessageChannel, navigator, location/history,
// Notification and WebSocket/EventSource stubs.
(() => {
  'use strict';
  const G = globalThis, jr = G.__jr, H = jr.host, def = jr.def;
  const { DOMException, EventTarget, Event, MessageEvent, ProgressEvent } = G;
  const reportError = jr.reportError;
  const kToken = Symbol('internal');
  const illegal = () => new TypeError('Illegal constructor');
  const setTag = (C, name) => Object.defineProperty(C.prototype, Symbol.toStringTag, { value: name, configurable: true });
  const expose = (name, C) => { if (typeof C === 'function' && C.prototype) setTag(C, name); def(G, name, C); };
  const need = (args, n, method, iface) => {
    if (args.length < n) {
      const where = method ? `Failed to execute '${method}' on '${iface}'` : `Failed to construct '${iface}'`;
      throw new TypeError(`${where}: ${n} argument${n > 1 ? 's' : ''} required, but only ${args.length} present.`);
    }
  };
  const isObj = (x) => (typeof x === 'object' && x !== null) || typeof x === 'function';
  const usv = (x) => { const s = String(x); return s.isWellFormed === undefined || s.isWellFormed() ? s : s.toWellFormed(); };
  const constants = (C, o) => {
    for (const k in o) {
      Object.defineProperty(C, k, { value: o[k], enumerable: true });
      Object.defineProperty(C.prototype, k, { value: o[k], enumerable: true });
    }
  };
  // on<event> handler attributes, stored per instance
  const kOn = Symbol('handlers');
  const handlers = (proto, names) => {
    for (const n of names) {
      Object.defineProperty(proto, 'on' + n, {
        get() { const h = this[kOn]; return (h && h[n]) || null; },
        set(v) {
          let h = this[kOn];
          if (!h) { h = {}; def(this, kOn, h); }
          h[n] = typeof v === 'function' || isObj(v) ? v : null;
        },
        enumerable: true, configurable: true,
      });
    }
  };
  const domErr = (e, dflt) => e instanceof DOMException ? e : new DOMException((e && e.message) || String(e), (e && e.name && e.name !== 'Error') ? e.name : dflt);

  // ---- bytes ----
  const EMPTY = new Uint8Array(0);
  const toBytes = (x) => {
    if (x instanceof Uint8Array) return x;
    if (ArrayBuffer.isView(x)) return new Uint8Array(x.buffer, x.byteOffset, x.byteLength);
    if (x instanceof ArrayBuffer || (typeof SharedArrayBuffer === 'function' && x instanceof SharedArrayBuffer)) return new Uint8Array(x);
    return null;
  };
  const concat = (chunks, total) => {
    if (total === undefined) { total = 0; for (const c of chunks) total += c.length; }
    const out = new Uint8Array(total);
    let o = 0;
    for (const c of chunks) { out.set(c, o); o += c.length; }
    return out;
  };
  // bytes → string, one char per byte
  const latin1 = (u8) => {
    const n = u8.length;
    if (n <= 8192) return String.fromCharCode.apply(null, u8);
    const parts = [];
    for (let i = 0; i < n; i += 8192) parts.push(String.fromCharCode.apply(null, u8.subarray(i, i + 8192)));
    return parts.join('');
  };
  const fromLatin1 = (s) => {
    const n = s.length, out = new Uint8Array(n);
    for (let i = 0; i < n; i++) out[i] = s.charCodeAt(i);
    return out;
  };

  // ---- UTF-8 ----
  const utf8Len = (s) => {
    let n = 0;
    for (let i = 0, l = s.length; i < l; i++) {
      const c = s.charCodeAt(i);
      if (c < 0x80) n++;
      else if (c < 0x800) n += 2;
      else if (c >= 0xd800 && c <= 0xdbff && i + 1 < l && (s.charCodeAt(i + 1) & 0xfc00) === 0xdc00) { n += 4; i++; }
      else n += 3;
    }
    return n;
  };
  const utf8Write = (s, out, p) => {
    for (let i = 0, l = s.length; i < l; i++) {
      let c = s.charCodeAt(i);
      if (c < 0x80) { out[p++] = c; continue; }
      if (c < 0x800) { out[p++] = 0xc0 | (c >> 6); out[p++] = 0x80 | (c & 63); continue; }
      if (c >= 0xd800 && c <= 0xdfff) {
        if (c <= 0xdbff && i + 1 < l) {
          const d = s.charCodeAt(i + 1);
          if ((d & 0xfc00) === 0xdc00) {
            i++;
            c = 0x10000 + ((c - 0xd800) << 10) + (d - 0xdc00);
            out[p++] = 0xf0 | (c >> 18); out[p++] = 0x80 | ((c >> 12) & 63); out[p++] = 0x80 | ((c >> 6) & 63); out[p++] = 0x80 | (c & 63);
            continue;
          }
        }
        c = 0xfffd;
      }
      out[p++] = 0xe0 | (c >> 12); out[p++] = 0x80 | ((c >> 6) & 63); out[p++] = 0x80 | (c & 63);
    }
    return p;
  };
  const utf8Encode = (s) => {
    const l = s.length;
    let i = 0;
    while (i < l && s.charCodeAt(i) < 0x80) i++;
    if (i === l) {
      const out = new Uint8Array(l);
      for (let j = 0; j < l; j++) out[j] = s.charCodeAt(j);
      return out;
    }
    const out = new Uint8Array(utf8Len(s));
    utf8Write(s, out, 0);
    return out;
  };
  const badData = (enc) => new TypeError(`Failed to execute 'decode' on 'TextDecoder': The encoded data was not valid for encoding ${enc}`);
  const CH = 8192;
  const obuf = new Uint16Array(CH + 4);
  const newUtf8State = () => ({ need: 0, seen: 0, cp: 0, lo: 0x80, hi: 0xbf });
  // WHATWG UTF-8 decoder; st carries a partial sequence between stream chunks
  const utf8Decode = (b, st, fatal, flush) => {
    const parts = [];
    let n = 0, need = st.need, seen = st.seen, cp = st.cp, lo = st.lo, hi = st.hi;
    const l = b.length;
    let i = 0;
    const fail = () => { st.need = st.seen = st.cp = 0; st.lo = 0x80; st.hi = 0xbf; return badData('utf-8'); };
    while (i < l) {
      if (n >= CH) { parts.push(String.fromCharCode.apply(null, obuf.subarray(0, n))); n = 0; }
      const x = b[i];
      if (need === 0) {
        if (x < 0x80) {
          // ASCII run: convert straight from the input
          let j = i + 1;
          while (j < l && b[j] < 0x80 && j - i < CH) j++;
          if (j - i > 32) {
            if (n) { parts.push(String.fromCharCode.apply(null, obuf.subarray(0, n))); n = 0; }
            parts.push(String.fromCharCode.apply(null, b.subarray(i, j)));
          } else {
            for (; i < j; i++) obuf[n++] = b[i];
          }
          i = j;
          continue;
        }
        if (x >= 0xc2 && x <= 0xdf) { need = 1; cp = x & 0x1f; }
        else if (x >= 0xe0 && x <= 0xef) { if (x === 0xe0) lo = 0xa0; else if (x === 0xed) hi = 0x9f; need = 2; cp = x & 0xf; }
        else if (x >= 0xf0 && x <= 0xf4) { if (x === 0xf0) lo = 0x90; else if (x === 0xf4) hi = 0x8f; need = 3; cp = x & 7; }
        else { if (fatal) throw fail(); obuf[n++] = 0xfffd; }
        i++;
        continue;
      }
      if (x < lo || x > hi) {
        need = seen = cp = 0; lo = 0x80; hi = 0xbf;
        if (fatal) throw fail();
        obuf[n++] = 0xfffd;
        continue; // reprocess x
      }
      lo = 0x80; hi = 0xbf;
      cp = (cp << 6) | (x & 0x3f);
      i++;
      if (++seen === need) {
        if (cp > 0xffff) { cp -= 0x10000; obuf[n++] = 0xd800 + (cp >> 10); obuf[n++] = 0xdc00 + (cp & 0x3ff); }
        else obuf[n++] = cp;
        need = seen = cp = 0;
      }
    }
    if (flush && need) {
      need = seen = cp = 0; lo = 0x80; hi = 0xbf;
      if (fatal) throw fail();
      obuf[n++] = 0xfffd;
    }
    st.need = need; st.seen = seen; st.cp = cp; st.lo = lo; st.hi = hi;
    if (n) parts.push(String.fromCharCode.apply(null, obuf.subarray(0, n)));
    return parts.length === 1 ? parts[0] : parts.join('');
  };
  // UTF-8 decode (lossy, BOM stripped), as Body.text() and Blob.text() do
  const utf8Text = (b) => {
    const s = utf8Decode(b, newUtf8State(), false, true);
    return s.charCodeAt(0) === 0xfeff ? s.slice(1) : s;
  };
  const W1252 = [0x20ac, 0x81, 0x201a, 0x192, 0x201e, 0x2026, 0x2020, 0x2021, 0x2c6, 0x2030, 0x160, 0x2039, 0x152, 0x8d, 0x17d, 0x8f, 0x90, 0x2018, 0x2019, 0x201c, 0x201d, 0x2022, 0x2013, 0x2014, 0x2dc, 0x2122, 0x161, 0x203a, 0x153, 0x9d, 0x17e, 0x178];
  const decode1252 = (b) => {
    const parts = [];
    for (let i = 0; i < b.length; i += CH) {
      const end = Math.min(b.length, i + CH);
      let n = 0;
      for (let j = i; j < end; j++) { const x = b[j]; obuf[n++] = x >= 0x80 && x < 0xa0 ? W1252[x - 0x80] : x; }
      parts.push(String.fromCharCode.apply(null, obuf.subarray(0, n)));
    }
    return parts.join('');
  };
  // UTF-16: st = {lead: -1|byte, hs: 0|high surrogate}
  const decode16 = (b, st, fatal, flush, be) => {
    const parts = [];
    let n = 0, i = 0;
    const emit = (u) => {
      if (n >= CH) { parts.push(String.fromCharCode.apply(null, obuf.subarray(0, n))); n = 0; }
      if (st.hs) {
        const h = st.hs;
        if (u >= 0xdc00 && u <= 0xdfff) { st.hs = 0; obuf[n++] = h; obuf[n++] = u; return; }
        st.hs = 0;
        if (fatal) throw badData(be ? 'utf-16be' : 'utf-16le');
        obuf[n++] = 0xfffd;
      }
      if (u >= 0xd800 && u <= 0xdbff) st.hs = u;
      else if (u >= 0xdc00 && u <= 0xdfff) { if (fatal) throw badData(be ? 'utf-16be' : 'utf-16le'); obuf[n++] = 0xfffd; }
      else obuf[n++] = u;
    };
    if (st.lead >= 0 && b.length) { emit(be ? (st.lead << 8) | b[0] : st.lead | (b[0] << 8)); st.lead = -1; i = 1; }
    for (; i + 1 < b.length; i += 2) emit(be ? (b[i] << 8) | b[i + 1] : b[i] | (b[i + 1] << 8));
    if (i < b.length) st.lead = b[i];
    if (flush && (st.lead >= 0 || st.hs)) {
      st.lead = -1; st.hs = 0;
      if (fatal) throw badData(be ? 'utf-16be' : 'utf-16le');
      obuf[n++] = 0xfffd;
    }
    if (n) parts.push(String.fromCharCode.apply(null, obuf.subarray(0, n)));
    return parts.join('');
  };
  const LABELS = new Map();
  for (const [enc, labels] of [
    ['utf-8', 'unicode-1-1-utf-8 unicode11utf8 unicode20utf8 utf-8 utf8 x-unicode20utf8'],
    ['utf-16le', 'csunicode iso-10646-ucs-2 ucs-2 unicode unicodefeff utf-16 utf-16le'],
    ['utf-16be', 'unicodefffe utf-16be'],
    ['windows-1252', 'ansi_x3.4-1968 ascii cp1252 cp819 csisolatin1 ibm819 iso-8859-1 iso-ir-100 iso8859-1 iso88591 iso_8859-1 iso_8859-1:1987 l1 latin1 us-ascii windows-1252 x-cp1252'],
  ]) for (const l of labels.split(' ')) LABELS.set(l, enc);
  const encodingFor = (label) => LABELS.get(String(label).replace(/^[\t\n\f\r ]+|[\t\n\f\r ]+$/g, '').toLowerCase());

  class TextEncoder {
    get encoding() { return 'utf-8'; }
    encode(input = '') { return utf8Encode(String(input)); }
    encodeInto(src, dest) {
      need(arguments, 2, 'encodeInto', 'TextEncoder');
      if (!(dest instanceof Uint8Array)) throw new TypeError("Failed to execute 'encodeInto' on 'TextEncoder': parameter 2 is not of type 'Uint8Array'.");
      const s = String(src), cap = dest.length;
      let read = 0, p = 0;
      for (let i = 0, l = s.length; i < l; i++) {
        let c = s.charCodeAt(i), units = 1;
        if (c >= 0xd800 && c <= 0xdfff) {
          const d = c <= 0xdbff && i + 1 < l ? s.charCodeAt(i + 1) : 0;
          if ((d & 0xfc00) === 0xdc00) { c = 0x10000 + ((c - 0xd800) << 10) + (d - 0xdc00); units = 2; }
          else c = 0xfffd;
        }
        const len = c < 0x80 ? 1 : c < 0x800 ? 2 : c < 0x10000 ? 3 : 4;
        if (p + len > cap) break;
        if (len === 1) dest[p++] = c;
        else if (len === 2) { dest[p++] = 0xc0 | (c >> 6); dest[p++] = 0x80 | (c & 63); }
        else if (len === 3) { dest[p++] = 0xe0 | (c >> 12); dest[p++] = 0x80 | ((c >> 6) & 63); dest[p++] = 0x80 | (c & 63); }
        else { dest[p++] = 0xf0 | (c >> 18); dest[p++] = 0x80 | ((c >> 12) & 63); dest[p++] = 0x80 | ((c >> 6) & 63); dest[p++] = 0x80 | (c & 63); }
        read += units;
        i += units - 1;
      }
      return { read, written: p };
    }
  }
  class TextDecoder {
    #enc; #fatal; #ignoreBOM; #st = null; #bomSeen = false; #streaming = false;
    constructor(label = 'utf-8', options = undefined) {
      const enc = encodingFor(label);
      if (!enc) throw new RangeError(`Failed to construct 'TextDecoder': The encoding label provided ('${label}') is invalid.`);
      this.#enc = enc;
      this.#fatal = !!(options && options.fatal);
      this.#ignoreBOM = !!(options && options.ignoreBOM);
    }
    get encoding() { return this.#enc; }
    get fatal() { return this.#fatal; }
    get ignoreBOM() { return this.#ignoreBOM; }
    decode(input = undefined, options = undefined) {
      let b = EMPTY;
      if (input !== undefined && input !== null) {
        b = toBytes(input);
        if (!b) throw new TypeError("Failed to execute 'decode' on 'TextDecoder': The provided value is not of type '(ArrayBuffer or ArrayBufferView)'.");
      }
      if (!this.#streaming || !this.#st) {
        this.#st = this.#enc === 'utf-8' ? newUtf8State() : { lead: -1, hs: 0 };
        this.#bomSeen = false;
      }
      const stream = !!(options && options.stream);
      this.#streaming = stream;
      let s;
      switch (this.#enc) {
        case 'utf-8': s = utf8Decode(b, this.#st, this.#fatal, !stream); break;
        case 'utf-16le': s = decode16(b, this.#st, this.#fatal, !stream, false); break;
        case 'utf-16be': s = decode16(b, this.#st, this.#fatal, !stream, true); break;
        default: s = decode1252(b);
      }
      if (!this.#bomSeen && s.length) {
        this.#bomSeen = true;
        if (!this.#ignoreBOM && this.#enc !== 'windows-1252' && s.charCodeAt(0) === 0xfeff) s = s.slice(1);
      }
      if (!stream) this.#st = null;
      return s;
    }
  }
  expose('TextEncoder', TextEncoder);
  expose('TextDecoder', TextDecoder);

  // ---- base64 ----
  const B64 = 'ABCDEFGHIJKLMNOPQRSTUVWXYZabcdefghijklmnopqrstuvwxyz0123456789+/';
  const B64C = new Uint8Array(64);
  const B64R = new Int8Array(128).fill(-1);
  for (let i = 0; i < 64; i++) { B64C[i] = B64.charCodeAt(i); B64R[B64.charCodeAt(i)] = i; }
  const b64encode = (u8) => {
    const n = u8.length, out = new Uint8Array(Math.ceil(n / 3) * 4);
    let o = 0, i = 0;
    for (; i + 2 < n; i += 3) {
      const v = (u8[i] << 16) | (u8[i + 1] << 8) | u8[i + 2];
      out[o++] = B64C[v >> 18]; out[o++] = B64C[(v >> 12) & 63]; out[o++] = B64C[(v >> 6) & 63]; out[o++] = B64C[v & 63];
    }
    if (i < n) {
      const v = (u8[i] << 16) | (i + 1 < n ? u8[i + 1] << 8 : 0);
      out[o++] = B64C[v >> 18]; out[o++] = B64C[(v >> 12) & 63];
      out[o++] = i + 1 < n ? B64C[(v >> 6) & 63] : 61;
      out[o++] = 61;
    }
    return latin1(out);
  };
  // forgiving-base64 decode; null on failure
  const b64decode = (s) => {
    let l = s.length;
    for (let i = 0; i < l; i++) {
      const c = s.charCodeAt(i);
      if (c === 0x20 || c === 9 || c === 10 || c === 12 || c === 13) { s = s.replace(/[\t\n\f\r ]+/g, ''); l = s.length; break; }
    }
    if (l % 4 === 0 && l) { if (s.charCodeAt(l - 1) === 61) { l--; if (s.charCodeAt(l - 1) === 61) l--; } }
    if (l % 4 === 1) return null;
    const out = new Uint8Array((l * 3) >> 2);
    let acc = 0, bits = 0, o = 0;
    for (let i = 0; i < l; i++) {
      const c = s.charCodeAt(i);
      const v = c < 128 ? B64R[c] : -1;
      if (v < 0) return null;
      acc = (acc << 6) | v; bits += 6;
      if (bits >= 8) { bits -= 8; out[o++] = (acc >> bits) & 255; }
    }
    return o === out.length ? out : out.subarray(0, o);
  };
  def(G, 'btoa', function btoa(data) {
    need(arguments, 1, 'btoa', 'Window');
    const r = H.btoa(String(data));
    if (typeof r !== 'string') throw new DOMException("Failed to execute 'btoa' on 'Window': The string to be encoded contains characters outside of the Latin1 range.", 'InvalidCharacterError');
    return r;
  });
  def(G, 'atob', function atob(data) {
    need(arguments, 1, 'atob', 'Window');
    const r = H.atob(String(data));
    if (typeof r !== 'string') throw new DOMException("Failed to execute 'atob' on 'Window': The string to be decoded is not correctly encoded.", 'InvalidCharacterError');
    return r;
  });

  // ---- percent-encoding ----
  const C0 = 1, FRAG = 2, QUERY = 4, SQUERY = 8, PATH = 16, USERINFO = 32, COMPONENT = 64, FORM = 128;
  const PE = new Uint8Array(128);
  for (let c = 0; c < 128; c++) {
    if (c < 0x20 || c === 0x7f) { PE[c] = 0xff; continue; }
    const ch = String.fromCharCode(c);
    let m = 0;
    if (' "<>`'.includes(ch)) m |= FRAG;
    if (' "#<>'.includes(ch)) m |= QUERY | SQUERY;
    if (ch === "'") m |= SQUERY;
    if (' "#<>?^`{}'.includes(ch)) m |= PATH;
    if (' "#<>?^`{}/:;=@[\\]|'.includes(ch)) m |= USERINFO;
    if (' "#<>?^`{}/:;=@[\\]|$%&+,'.includes(ch)) m |= COMPONENT;
    if (' "#<>?^`{}/:;=@[\\]|$%&+,!\'()~'.includes(ch)) m |= FORM;
    PE[c] = m;
  }
  const HEX = '0123456789ABCDEF';
  const pct = (b) => '%' + HEX[b >> 4] + HEX[b & 15];
  const pctEncode = (s, mask, spacePlus) => {
    const l = s.length;
    let i = 0;
    for (; i < l; i++) { const c = s.charCodeAt(i); if (c >= 0x80 || (PE[c] & mask)) break; }
    if (i === l) return s;
    let out = s.slice(0, i);
    for (; i < l; i++) {
      let c = s.charCodeAt(i);
      if (c < 0x80) {
        if (PE[c] & mask) out += spacePlus && c === 0x20 ? '+' : pct(c);
        else out += s[i];
        continue;
      }
      if (c >= 0xd800 && c <= 0xdfff) {
        const d = c <= 0xdbff && i + 1 < l ? s.charCodeAt(i + 1) : 0;
        if ((d & 0xfc00) === 0xdc00) { c = 0x10000 + ((c - 0xd800) << 10) + (d - 0xdc00); i++; }
        else c = 0xfffd;
      }
      if (c < 0x800) out += pct(0xc0 | (c >> 6)) + pct(0x80 | (c & 63));
      else if (c < 0x10000) out += pct(0xe0 | (c >> 12)) + pct(0x80 | ((c >> 6) & 63)) + pct(0x80 | (c & 63));
      else out += pct(0xf0 | (c >> 18)) + pct(0x80 | ((c >> 12) & 63)) + pct(0x80 | ((c >> 6) & 63)) + pct(0x80 | (c & 63));
    }
    return out;
  };
  const hexVal = (c) => c >= 0x30 && c <= 0x39 ? c - 0x30 : (c | 0x20) >= 0x61 && (c | 0x20) <= 0x66 ? (c | 0x20) - 0x57 : -1;
  const pctDecodeBytes = (s) => {
    const b = utf8Encode(s);
    if (b.indexOf(0x25) < 0) return b;
    const out = new Uint8Array(b.length);
    let n = 0;
    for (let i = 0; i < b.length; i++) {
      const x = b[i];
      if (x === 0x25 && i + 2 < b.length) {
        const h = hexVal(b[i + 1]), lo = hexVal(b[i + 2]);
        if (h >= 0 && lo >= 0) { out[n++] = h * 16 + lo; i += 2; continue; }
      }
      out[n++] = x;
    }
    return out.subarray(0, n);
  };
  const pctDecode = (s) => s.indexOf('%') < 0 ? s : utf8Decode(pctDecodeBytes(s), newUtf8State(), false, true);

  // ---- URL parsing (WHATWG URL Standard) ----
  const defaultPort = (s) => s === 'http' || s === 'ws' ? 80 : s === 'https' || s === 'wss' ? 443 : s === 'ftp' ? 21 : null;
  const isSpecial = (s) => s === 'http' || s === 'https' || s === 'ws' || s === 'wss' || s === 'file' || s === 'ftp';
  const isAlpha = (c) => (c | 0x20) >= 0x61 && (c | 0x20) <= 0x7a;
  const isDigit = (c) => c >= 0x30 && c <= 0x39;
  const lowerC = (c) => String.fromCharCode(c >= 0x41 && c <= 0x5a ? c + 32 : c);
  const isWinLetter = (s, normalized) => s.length === 2 && isAlpha(s.charCodeAt(0)) && (s[1] === ':' || (!normalized && s[1] === '|'));
  const startsWithWinLetter = (s, p) => {
    if (s.length - p < 2 || !isAlpha(s.charCodeAt(p))) return false;
    const c1 = s.charCodeAt(p + 1);
    if (c1 !== 0x3a && c1 !== 0x7c) return false;
    if (s.length - p === 2) return true;
    const c2 = s.charCodeAt(p + 2);
    return c2 === 0x2f || c2 === 0x5c || c2 === 0x3f || c2 === 0x23;
  };
  const shortenPath = (u) => {
    const p = u.path;
    if (u.scheme === 'file' && p.length === 1 && isWinLetter(p[0], true)) return;
    p.pop();
  };
  const isSingleDot = (s) => s === '.' || (s.length === 3 && s.toLowerCase() === '%2e');
  const isDoubleDot = (s) => {
    switch (s.length) {
      case 2: return s === '..';
      case 4: s = s.toLowerCase(); return s === '.%2e' || s === '%2e.';
      case 6: return s.toLowerCase() === '%2e%2e';
    }
    return false;
  };
  const FORBIDDEN_HOST = new Uint8Array(128);
  for (const ch of '\0\t\n\r #/:<>?@[\\]^|') FORBIDDEN_HOST[ch.charCodeAt(0)] = 1;
  const FORBIDDEN_DOMAIN = FORBIDDEN_HOST.slice();
  for (let c = 0; c < 0x20; c++) FORBIDDEN_DOMAIN[c] = 1;
  FORBIDDEN_DOMAIN[0x25] = 1; FORBIDDEN_DOMAIN[0x7f] = 1;

  const ipv4Num = (s) => {
    if (s === '') return NaN;
    let r = 10;
    if (s.length >= 2 && s[0] === '0' && (s[1] === 'x' || s[1] === 'X')) { r = 16; s = s.slice(2); }
    else if (s.length >= 2 && s[0] === '0') { r = 8; s = s.slice(1); }
    if (s === '') return 0;
    for (let i = 0; i < s.length; i++) {
      const c = s.charCodeAt(i);
      if (r === 10 ? !isDigit(c) : r === 8 ? !(c >= 0x30 && c <= 0x37) : hexVal(c) < 0) return NaN;
    }
    return parseInt(s, r);
  };
  const endsInNumber = (s) => {
    const parts = s.split('.');
    if (parts[parts.length - 1] === '') { if (parts.length === 1) return false; parts.pop(); }
    const last = parts[parts.length - 1];
    if (last !== '' && /^[0-9]+$/.test(last)) return true;
    return !Number.isNaN(ipv4Num(last));
  };
  const parseIPv4 = (s) => {
    const parts = s.split('.');
    if (parts[parts.length - 1] === '' && parts.length > 1) parts.pop();
    if (parts.length > 4) return null;
    const nums = [];
    for (const p of parts) { const n = ipv4Num(p); if (Number.isNaN(n)) return null; nums.push(n); }
    for (let i = 0; i < nums.length - 1; i++) if (nums[i] > 255) return null;
    if (nums[nums.length - 1] >= 256 ** (5 - nums.length)) return null;
    let ip = nums[nums.length - 1];
    for (let i = 0; i < nums.length - 1; i++) ip += nums[i] * 256 ** (3 - i);
    return [ip >>> 24, (ip >>> 16) & 255, (ip >>> 8) & 255, ip & 255].join('.');
  };
  const parseIPv6 = (s) => {
    const a = [0, 0, 0, 0, 0, 0, 0, 0];
    let piece = 0, compress = null, p = 0;
    const len = s.length;
    const at = (i) => i < len ? s.charCodeAt(i) : -1;
    if (at(0) === 0x3a) {
      if (at(1) !== 0x3a) return null;
      p = 2; piece = 1; compress = 1;
    }
    while (at(p) !== -1) {
      if (piece === 8) return null;
      if (at(p) === 0x3a) {
        if (compress !== null) return null;
        p++; piece++; compress = piece;
        continue;
      }
      let v = 0, n = 0;
      while (n < 4 && at(p) !== -1 && hexVal(at(p)) >= 0) { v = v * 16 + hexVal(at(p)); p++; n++; }
      if (at(p) === 0x2e) {
        if (n === 0) return null;
        p -= n;
        if (piece > 6) return null;
        let seen = 0;
        while (at(p) !== -1) {
          let v4 = null;
          if (seen > 0) { if (at(p) === 0x2e && seen < 4) p++; else return null; }
          if (!isDigit(at(p))) return null;
          while (isDigit(at(p))) {
            const d = at(p) - 0x30;
            if (v4 === null) v4 = d; else if (v4 === 0) return null; else v4 = v4 * 10 + d;
            if (v4 > 255) return null;
            p++;
          }
          a[piece] = a[piece] * 256 + v4;
          seen++;
          if (seen === 2 || seen === 4) piece++;
        }
        if (seen !== 4) return null;
        break;
      } else if (at(p) === 0x3a) {
        p++;
        if (at(p) === -1) return null;
      } else if (at(p) !== -1) return null;
      a[piece] = v;
      piece++;
    }
    if (compress !== null) {
      let swaps = piece - compress;
      piece = 7;
      while (piece !== 0 && swaps > 0) {
        const t = a[piece]; a[piece] = a[compress + swaps - 1]; a[compress + swaps - 1] = t;
        piece--; swaps--;
      }
    } else if (piece !== 8) return null;
    return a;
  };
  const ipv6Str = (a) => {
    let best = -1, bestLen = 1;
    for (let i = 0; i < 8;) {
      if (a[i] !== 0) { i++; continue; }
      let j = i;
      while (j < 8 && a[j] === 0) j++;
      if (j - i > bestLen) { best = i; bestLen = j - i; }
      i = j;
    }
    let out = '', ignore0 = false;
    for (let i = 0; i < 8; i++) {
      if (ignore0 && a[i] === 0) continue;
      ignore0 = false;
      if (best === i) { out += i === 0 ? '::' : ':'; ignore0 = true; continue; }
      out += a[i].toString(16);
      if (i !== 7) out += ':';
    }
    return out;
  };
  // punycode (RFC 3492) for IDNA labels
  const punyDigit = (d) => String.fromCharCode(d + 22 + (d < 26 ? 75 : 0));
  const punyAdapt = (delta, num, first) => {
    delta = first ? Math.floor(delta / 700) : delta >> 1;
    delta += Math.floor(delta / num);
    let k = 0;
    while (delta > 455) { delta = Math.floor(delta / 35); k += 36; }
    return k + Math.floor((36 * delta) / (delta + 38));
  };
  const punycode = (input) => {
    const cps = Array.from(input, (c) => c.codePointAt(0));
    let n = 128, delta = 0, bias = 72, out = '';
    for (const c of cps) if (c < 128) out += String.fromCharCode(c);
    let h = out.length;
    const b = h;
    if (b) out += '-';
    while (h < cps.length) {
      let m = Infinity;
      for (const c of cps) if (c >= n && c < m) m = c;
      delta += (m - n) * (h + 1);
      n = m;
      for (const c of cps) {
        if (c < n) delta++;
        if (c === n) {
          let q = delta;
          for (let k = 36; ; k += 36) {
            const t = k <= bias ? 1 : k >= bias + 26 ? 26 : k - bias;
            if (q < t) break;
            out += punyDigit(t + ((q - t) % (36 - t)));
            q = Math.floor((q - t) / (36 - t));
          }
          out += punyDigit(q);
          bias = punyAdapt(delta, h + 1, h === b);
          delta = 0;
          h++;
        }
      }
      delta++; n++;
    }
    return out;
  };
  const domainToASCII = (d) => {
    let ascii = true;
    for (let i = 0; i < d.length; i++) if (d.charCodeAt(i) >= 0x80) { ascii = false; break; }
    if (ascii) return d.toLowerCase();
    d = d.normalize('NFKC').toLowerCase().replace(/[。．｡]/g, '.');
    return d.split('.').map((l) => /[^\x00-\x7f]/.test(l) ? 'xn--' + punycode(l) : l).join('.');
  };
  const parseHost = (input, opaque) => {
    if (input.charCodeAt(0) === 0x5b) {
      if (input.charCodeAt(input.length - 1) !== 0x5d) return null;
      const a = parseIPv6(input.slice(1, -1));
      return a ? '[' + ipv6Str(a) + ']' : null;
    }
    if (opaque) {
      for (let i = 0; i < input.length; i++) { const c = input.charCodeAt(i); if (c < 128 && FORBIDDEN_HOST[c]) return null; }
      return pctEncode(input, C0);
    }
    const ascii = domainToASCII(pctDecode(input));
    if (ascii === '') return null;
    for (let i = 0; i < ascii.length; i++) { const c = ascii.charCodeAt(i); if (c < 128 && FORBIDDEN_DOMAIN[c]) return null; }
    if (endsInNumber(ascii)) return parseIPv4(ascii);
    return ascii;
  };
  const cleanInput = (s, trim) => {
    let start = 0, end = s.length;
    if (trim) {
      while (start < end && s.charCodeAt(start) <= 0x20) start++;
      while (end > start && s.charCodeAt(end - 1) <= 0x20) end--;
      if (start || end !== s.length) s = s.slice(start, end);
    }
    for (let i = 0; i < s.length; i++) {
      const c = s.charCodeAt(i);
      if (c === 9 || c === 10 || c === 13) return s.replace(/[\t\n\r]/g, '');
    }
    return s;
  };
  const S_SCHEME_START = 1, S_SCHEME = 2, S_NO_SCHEME = 3, S_SPECIAL_REL_OR_AUTH = 4, S_PATH_OR_AUTH = 5, S_RELATIVE = 6,
    S_RELATIVE_SLASH = 7, S_SPECIAL_AUTH_SLASHES = 8, S_SPECIAL_AUTH_IGNORE = 9, S_AUTHORITY = 10, S_HOST = 11, S_HOSTNAME = 12,
    S_PORT = 13, S_FILE = 14, S_FILE_SLASH = 15, S_FILE_HOST = 16, S_PATH_START = 17, S_PATH = 18, S_OPAQUE = 19, S_QUERY = 20, S_FRAGMENT = 21;
  const newRecord = () => ({ scheme: '', username: '', password: '', host: null, port: null, path: [], query: null, fragment: null });
  const copyRecord = (u) => ({ scheme: u.scheme, username: u.username, password: u.password, host: u.host, port: u.port, path: typeof u.path === 'string' ? u.path : u.path.slice(), query: u.query, fragment: u.fragment });
  // basic URL parser: returns the record (url, when given with a state
  // override) or null on failure
  const parseURL = (input, base, url, override) => {
    input = cleanInput(input, !url);
    if (!url) url = newRecord();
    let state = override || S_SCHEME_START;
    let buffer = '', atSign = false, inBrackets = false, pwSeen = false;
    let sp = isSpecial(url.scheme);
    const len = input.length;
    let p = 0;
    for (;;) {
      const c = p < len ? input.charCodeAt(p) : -1;
      switch (state) {
        case S_SCHEME_START:
          if (c !== -1 && isAlpha(c)) { buffer += lowerC(c); state = S_SCHEME; }
          else if (!override) { state = S_NO_SCHEME; p--; }
          else return null;
          break;
        case S_SCHEME:
          if (c !== -1 && (isAlpha(c) || isDigit(c) || c === 0x2b || c === 0x2d || c === 0x2e)) buffer += lowerC(c);
          else if (c === 0x3a) {
            if (override) {
              if (sp !== isSpecial(buffer)) return url;
              if ((url.username !== '' || url.password !== '' || url.port !== null) && buffer === 'file') return url;
              if (url.scheme === 'file' && url.host === '') return url;
            }
            url.scheme = buffer;
            sp = isSpecial(buffer);
            if (override) {
              if (url.port === defaultPort(url.scheme)) url.port = null;
              return url;
            }
            buffer = '';
            if (url.scheme === 'file') state = S_FILE;
            else if (sp && base && base.scheme === url.scheme) state = S_SPECIAL_REL_OR_AUTH;
            else if (sp) state = S_SPECIAL_AUTH_SLASHES;
            else if (input.charCodeAt(p + 1) === 0x2f) { state = S_PATH_OR_AUTH; p++; }
            else { url.path = ''; state = S_OPAQUE; }
          } else if (!override) { buffer = ''; state = S_NO_SCHEME; p = -1; }
          else return null;
          break;
        case S_NO_SCHEME:
          if (!base || (typeof base.path === 'string' && c !== 0x23)) return null;
          if (typeof base.path === 'string') {
            url.scheme = base.scheme; sp = isSpecial(url.scheme);
            url.path = base.path; url.query = base.query; url.fragment = '';
            state = S_FRAGMENT;
          } else if (base.scheme !== 'file') { state = S_RELATIVE; p--; }
          else { state = S_FILE; p--; }
          break;
        case S_SPECIAL_REL_OR_AUTH:
          if (c === 0x2f && input.charCodeAt(p + 1) === 0x2f) { state = S_SPECIAL_AUTH_IGNORE; p++; }
          else { state = S_RELATIVE; p--; }
          break;
        case S_PATH_OR_AUTH:
          if (c === 0x2f) state = S_AUTHORITY;
          else { state = S_PATH; p--; }
          break;
        case S_RELATIVE:
          url.scheme = base.scheme; sp = isSpecial(url.scheme);
          if (c === 0x2f || (sp && c === 0x5c)) state = S_RELATIVE_SLASH;
          else {
            url.username = base.username; url.password = base.password; url.host = base.host; url.port = base.port;
            url.path = base.path.slice(); url.query = base.query;
            if (c === 0x3f) { url.query = ''; state = S_QUERY; }
            else if (c === 0x23) { url.fragment = ''; state = S_FRAGMENT; }
            else if (c !== -1) { url.query = null; shortenPath(url); state = S_PATH; p--; }
          }
          break;
        case S_RELATIVE_SLASH:
          if (sp && (c === 0x2f || c === 0x5c)) state = S_SPECIAL_AUTH_IGNORE;
          else if (c === 0x2f) state = S_AUTHORITY;
          else {
            url.username = base.username; url.password = base.password; url.host = base.host; url.port = base.port;
            state = S_PATH; p--;
          }
          break;
        case S_SPECIAL_AUTH_SLASHES:
          state = S_SPECIAL_AUTH_IGNORE;
          if (c === 0x2f && input.charCodeAt(p + 1) === 0x2f) p++;
          else p--;
          break;
        case S_SPECIAL_AUTH_IGNORE:
          if (c !== 0x2f && c !== 0x5c) { state = S_AUTHORITY; p--; }
          break;
        case S_AUTHORITY:
          if (c === 0x40) {
            if (atSign) buffer = '%40' + buffer;
            atSign = true;
            if (pwSeen) url.password += pctEncode(buffer, USERINFO);
            else {
              const i = buffer.indexOf(':');
              if (i < 0) url.username += pctEncode(buffer, USERINFO);
              else {
                pwSeen = true;
                url.username += pctEncode(buffer.slice(0, i), USERINFO);
                url.password += pctEncode(buffer.slice(i + 1), USERINFO);
              }
            }
            buffer = '';
          } else if (c === -1 || c === 0x2f || c === 0x3f || c === 0x23 || (sp && c === 0x5c)) {
            if (atSign && buffer === '') return null;
            p -= buffer.length + 1;
            buffer = '';
            state = S_HOST;
          } else buffer += input[p];
          break;
        case S_HOST:
        case S_HOSTNAME:
          if (override && url.scheme === 'file') { p--; state = S_FILE_HOST; }
          else if (c === 0x3a && !inBrackets) {
            if (buffer === '') return null;
            if (override === S_HOSTNAME) return null;
            const h = parseHost(buffer, !sp);
            if (h === null) return null;
            url.host = h; buffer = ''; state = S_PORT;
          } else if (c === -1 || c === 0x2f || c === 0x3f || c === 0x23 || (sp && c === 0x5c)) {
            p--;
            if (sp && buffer === '') return null;
            if (override && buffer === '' && (url.username !== '' || url.password !== '' || url.port !== null)) return null;
            const h = parseHost(buffer, !sp);
            if (h === null) return null;
            url.host = h; buffer = ''; state = S_PATH_START;
            if (override) return url;
          } else {
            if (c === 0x5b) inBrackets = true;
            else if (c === 0x5d) inBrackets = false;
            buffer += input[p];
          }
          break;
        case S_PORT:
          if (c !== -1 && isDigit(c)) buffer += input[p];
          else if (c === -1 || c === 0x2f || c === 0x3f || c === 0x23 || (sp && c === 0x5c) || override) {
            if (buffer !== '') {
              const port = parseInt(buffer, 10);
              if (port > 65535) return null;
              url.port = port === defaultPort(url.scheme) ? null : port;
              buffer = '';
              if (override) return url;
            }
            if (override) return null;
            state = S_PATH_START; p--;
          } else return null;
          break;
        case S_FILE:
          url.scheme = 'file'; sp = true; url.host = '';
          if (c === 0x2f || c === 0x5c) state = S_FILE_SLASH;
          else if (base && base.scheme === 'file') {
            url.host = base.host; url.path = base.path.slice(); url.query = base.query;
            if (c === 0x3f) { url.query = ''; state = S_QUERY; }
            else if (c === 0x23) { url.fragment = ''; state = S_FRAGMENT; }
            else if (c !== -1) {
              url.query = null;
              if (!startsWithWinLetter(input, p)) shortenPath(url);
              else url.path = [];
              state = S_PATH; p--;
            }
          } else { state = S_PATH; p--; }
          break;
        case S_FILE_SLASH:
          if (c === 0x2f || c === 0x5c) state = S_FILE_HOST;
          else {
            if (base && base.scheme === 'file') {
              url.host = base.host;
              if (!startsWithWinLetter(input, p) && base.path.length && isWinLetter(base.path[0], true)) url.path.push(base.path[0]);
            }
            state = S_PATH; p--;
          }
          break;
        case S_FILE_HOST:
          if (c === -1 || c === 0x2f || c === 0x5c || c === 0x3f || c === 0x23) {
            p--;
            if (!override && isWinLetter(buffer, false)) state = S_PATH;
            else if (buffer === '') {
              url.host = '';
              if (override) return url;
              state = S_PATH_START;
            } else {
              let h = parseHost(buffer, !sp);
              if (h === null) return null;
              if (h === 'localhost') h = '';
              url.host = h;
              if (override) return url;
              buffer = '';
              state = S_PATH_START;
            }
          } else buffer += input[p];
          break;
        case S_PATH_START:
          if (sp) { state = S_PATH; if (c !== 0x2f && c !== 0x5c) p--; }
          else if (!override && c === 0x3f) { url.query = ''; state = S_QUERY; }
          else if (!override && c === 0x23) { url.fragment = ''; state = S_FRAGMENT; }
          else if (c !== -1) { state = S_PATH; if (c !== 0x2f) p--; }
          else if (override && url.host === null) url.path.push('');
          break;
        case S_PATH: {
          const delim = (d) => d === -1 || d === 0x2f || (sp && d === 0x5c) || (!override && (d === 0x3f || d === 0x23));
          if (!delim(c)) {
            const st = p;
            while (p + 1 < len && !delim(input.charCodeAt(p + 1))) p++;
            buffer += input.slice(st, p + 1);
            break;
          }
          const slash = c === 0x2f || (sp && c === 0x5c);
          if (isDoubleDot(buffer)) {
            shortenPath(url);
            if (!slash) url.path.push('');
          } else if (isSingleDot(buffer)) {
            if (!slash) url.path.push('');
          } else {
            if (url.scheme === 'file' && url.path.length === 0 && isWinLetter(buffer, false)) buffer = buffer[0] + ':';
            url.path.push(pctEncode(buffer, PATH));
          }
          buffer = '';
          if (c === 0x3f) { url.query = ''; state = S_QUERY; }
          else if (c === 0x23) { url.fragment = ''; state = S_FRAGMENT; }
          break;
        }
        case S_OPAQUE:
          if (c === 0x3f) { url.query = ''; state = S_QUERY; }
          else if (c === 0x23) { url.fragment = ''; state = S_FRAGMENT; }
          else if (c !== -1) {
            const st = p;
            while (p + 1 < len) { const d = input.charCodeAt(p + 1); if (d === 0x3f || d === 0x23) break; p++; }
            let seg = pctEncode(input.slice(st, p + 1), C0);
            // a space right before ? or # is encoded so it survives serialization
            if (p + 1 < len && seg.charCodeAt(seg.length - 1) === 0x20) seg = seg.slice(0, -1) + '%20';
            url.path += seg;
          }
          break;
        case S_QUERY:
          if ((!override && c === 0x23) || c === -1) {
            url.query += pctEncode(buffer, sp ? SQUERY : QUERY);
            buffer = '';
            if (c === 0x23) { url.fragment = ''; state = S_FRAGMENT; }
          } else {
            const st = p;
            while (p + 1 < len && (override || input.charCodeAt(p + 1) !== 0x23)) p++;
            buffer += input.slice(st, p + 1);
          }
          break;
        case S_FRAGMENT:
          if (c !== -1) {
            url.fragment += pctEncode(input.slice(p), FRAG);
            p = len - 1;
          }
          break;
      }
      if (p >= len) break;
      p++;
    }
    return url;
  };
  const pathStr = (u) => typeof u.path === 'string' ? u.path : u.path.length ? '/' + u.path.join('/') : '';
  const serialize = (u, noFrag) => {
    let out = u.scheme + ':';
    if (u.host !== null) {
      out += '//';
      if (u.username !== '' || u.password !== '') {
        out += u.username;
        if (u.password !== '') out += ':' + u.password;
        out += '@';
      }
      out += u.host;
      if (u.port !== null) out += ':' + u.port;
    } else if (typeof u.path !== 'string' && u.path.length > 1 && u.path[0] === '') out += '/.';
    out += pathStr(u);
    if (u.query !== null) out += '?' + u.query;
    if (!noFrag && u.fragment !== null) out += '#' + u.fragment;
    return out;
  };
  const originOf = (u) => {
    switch (u.scheme) {
      case 'blob': {
        if (typeof u.path === 'string') {
          const inner = parseURL(u.path, null);
          if (inner && (inner.scheme === 'http' || inner.scheme === 'https')) return originOf(inner);
        }
        return 'null';
      }
      case 'ftp': case 'http': case 'https': case 'ws': case 'wss':
        return u.scheme + '://' + u.host + (u.port !== null ? ':' + u.port : '');
      default:
        return 'null';
    }
  };
  const cannotHaveCreds = (u) => u.host === null || u.host === '' || u.scheme === 'file';
  jr.parseURL = parseURL;
  jr.serializeURL = serialize;

  // application/x-www-form-urlencoded
  const formDecode = (s) => pctDecode(s.indexOf('+') >= 0 ? s.replace(/\+/g, ' ') : s);
  const parseForm = (s) => {
    const out = [];
    const l = s.length;
    let i = 0;
    while (i < l) {
      let j = s.indexOf('&', i);
      if (j < 0) j = l;
      if (j > i) {
        const eq = s.indexOf('=', i);
        if (eq < 0 || eq >= j) out.push([formDecode(s.slice(i, j)), '']);
        else out.push([formDecode(s.slice(i, eq)), formDecode(s.slice(eq + 1, j))]);
      }
      i = j + 1;
    }
    return out;
  };
  const serializeForm = (list) => {
    let out = '';
    for (let i = 0; i < list.length; i++) {
      if (i) out += '&';
      out += pctEncode(list[i][0], FORM, true) + '=' + pctEncode(list[i][1], FORM, true);
    }
    return out;
  };

  let spLink, spReset;
  class URLSearchParams {
    #list = [];
    #update = null;
    static {
      spLink = (sp, fn) => { sp.#update = fn; };
      spReset = (sp, q) => { sp.#list = q ? parseForm(q) : []; };
    }
    constructor(init = undefined) {
      if (init === undefined) return;
      if (isObj(init)) {
        if (#list in init) { this.#list = init.#list.map((p) => [p[0], p[1]]); return; }
        const it = init[Symbol.iterator];
        if (it !== undefined && it !== null) {
          if (typeof it !== 'function') throw new TypeError("Failed to construct 'URLSearchParams': The provided value cannot be converted to a sequence.");
          for (const pair of init) {
            if (!isObj(pair) || typeof pair[Symbol.iterator] !== 'function') throw new TypeError("Failed to construct 'URLSearchParams': The provided value cannot be converted to a sequence.");
            const a = Array.from(pair);
            if (a.length !== 2) throw new TypeError("Failed to construct 'URLSearchParams': Sequence initializer must only contain pair elements");
            this.#list.push([usv(a[0]), usv(a[1])]);
          }
        } else {
          for (const k of Object.keys(init)) this.#list.push([usv(k), usv(init[k])]);
        }
        return;
      }
      let s = usv(init);
      if (s.charCodeAt(0) === 0x3f) s = s.slice(1);
      this.#list = parseForm(s);
    }
    #changed() { if (this.#update) this.#update(serializeForm(this.#list)); }
    get size() { return this.#list.length; }
    append(name, value) {
      need(arguments, 2, 'append', 'URLSearchParams');
      this.#list.push([usv(name), usv(value)]);
      this.#changed();
    }
    delete(name, value = undefined) {
      need(arguments, 1, 'delete', 'URLSearchParams');
      name = usv(name);
      if (value !== undefined) { value = usv(value); this.#list = this.#list.filter((p) => p[0] !== name || p[1] !== value); }
      else this.#list = this.#list.filter((p) => p[0] !== name);
      this.#changed();
    }
    get(name) {
      need(arguments, 1, 'get', 'URLSearchParams');
      name = usv(name);
      for (const p of this.#list) if (p[0] === name) return p[1];
      return null;
    }
    getAll(name) {
      need(arguments, 1, 'getAll', 'URLSearchParams');
      name = usv(name);
      const out = [];
      for (const p of this.#list) if (p[0] === name) out.push(p[1]);
      return out;
    }
    has(name, value = undefined) {
      need(arguments, 1, 'has', 'URLSearchParams');
      name = usv(name);
      if (value !== undefined) { value = usv(value); return this.#list.some((p) => p[0] === name && p[1] === value); }
      return this.#list.some((p) => p[0] === name);
    }
    set(name, value) {
      need(arguments, 2, 'set', 'URLSearchParams');
      name = usv(name); value = usv(value);
      const l = this.#list;
      const i = l.findIndex((p) => p[0] === name);
      if (i < 0) l.push([name, value]);
      else { l[i] = [name, value]; this.#list = l.filter((p, j) => j <= i || p[0] !== name); }
      this.#changed();
    }
    sort() {
      this.#list.sort((a, b) => a[0] < b[0] ? -1 : a[0] > b[0] ? 1 : 0);
      this.#changed();
    }
    forEach(cb, thisArg = undefined) {
      if (typeof cb !== 'function') throw new TypeError("Failed to execute 'forEach' on 'URLSearchParams': The callback provided as parameter 1 is not a function.");
      for (let i = 0; i < this.#list.length; i++) { const p = this.#list[i]; cb.call(thisArg, p[1], p[0], this); }
    }
    *entries() { for (let i = 0; i < this.#list.length; i++) { const p = this.#list[i]; yield [p[0], p[1]]; } }
    *keys() { for (let i = 0; i < this.#list.length; i++) yield this.#list[i][0]; }
    *values() { for (let i = 0; i < this.#list.length; i++) yield this.#list[i][1]; }
    toString() { return serializeForm(this.#list); }
  }
  def(URLSearchParams.prototype, Symbol.iterator, URLSearchParams.prototype.entries);
  expose('URLSearchParams', URLSearchParams);

  const blobURLs = new Map();
  let urlRecOf;
  class URL {
    #u;
    #sp = null;
    static { urlRecOf = (x) => (isObj(x) && #u in x) ? x.#u : null; }
    constructor(url, base = undefined) {
      if (url === kToken) { this.#u = base; return; }
      need(arguments, 1, null, 'URL');
      let b = null;
      if (base !== undefined) {
        b = urlRecOf(base) || parseURL(String(base), null);
        if (!b) throw new TypeError("Failed to construct 'URL': Invalid base URL");
      }
      const u = parseURL(String(url), b);
      if (!u) throw new TypeError("Failed to construct 'URL': Invalid URL");
      this.#u = u;
    }
    static canParse(url, base = undefined) {
      need(arguments, 1, 'canParse', 'URL');
      let b = null;
      if (base !== undefined) { b = parseURL(String(base), null); if (!b) return false; }
      return parseURL(String(url), b) !== null;
    }
    static parse(url, base = undefined) {
      need(arguments, 1, 'parse', 'URL');
      let b = null;
      if (base !== undefined) { b = parseURL(String(base), null); if (!b) return null; }
      const u = parseURL(String(url), b);
      return u ? new URL(kToken, u) : null;
    }
    static createObjectURL(obj) {
      need(arguments, 1, 'createObjectURL', 'URL');
      if (!(obj instanceof G.Blob)) throw new TypeError("Failed to execute 'createObjectURL' on 'URL': Overload resolution failed.");
      const url = 'blob:clack/' + G.crypto.randomUUID();
      blobURLs.set(url, obj);
      return url;
    }
    static revokeObjectURL(url) {
      need(arguments, 1, 'revokeObjectURL', 'URL');
      blobURLs.delete(String(url));
    }
    get href() { return serialize(this.#u); }
    set href(v) {
      const u = parseURL(String(v), null);
      if (!u) throw new TypeError("Failed to set the 'href' property on 'URL': Invalid URL");
      this.#u = u;
      if (this.#sp) spReset(this.#sp, u.query);
    }
    get origin() { return originOf(this.#u); }
    get protocol() { return this.#u.scheme + ':'; }
    set protocol(v) { parseURL(String(v) + ':', null, this.#u, S_SCHEME_START); }
    get username() { return this.#u.username; }
    set username(v) { const u = this.#u; if (!cannotHaveCreds(u)) u.username = pctEncode(usv(v), USERINFO); }
    get password() { return this.#u.password; }
    set password(v) { const u = this.#u; if (!cannotHaveCreds(u)) u.password = pctEncode(usv(v), USERINFO); }
    get host() { const u = this.#u; return u.host === null ? '' : u.port === null ? u.host : u.host + ':' + u.port; }
    set host(v) { const u = this.#u; if (typeof u.path !== 'string') parseURL(String(v), null, u, S_HOST); }
    get hostname() { return this.#u.host === null ? '' : this.#u.host; }
    set hostname(v) { const u = this.#u; if (typeof u.path !== 'string') parseURL(String(v), null, u, S_HOSTNAME); }
    get port() { return this.#u.port === null ? '' : String(this.#u.port); }
    set port(v) {
      const u = this.#u;
      if (cannotHaveCreds(u)) return;
      v = String(v);
      if (v === '') u.port = null;
      else parseURL(v, null, u, S_PORT);
    }
    get pathname() { return pathStr(this.#u); }
    set pathname(v) {
      const u = this.#u;
      if (typeof u.path === 'string') return;
      u.path = [];
      parseURL(String(v), null, u, S_PATH_START);
    }
    get search() { const q = this.#u.query; return q === null || q === '' ? '' : '?' + q; }
    set search(v) {
      const u = this.#u;
      v = String(v);
      if (v === '') {
        u.query = null;
        if (this.#sp) spReset(this.#sp, '');
        return;
      }
      const input = v.charCodeAt(0) === 0x3f ? v.slice(1) : v;
      u.query = '';
      parseURL(input, null, u, S_QUERY);
      if (this.#sp) spReset(this.#sp, input);
    }
    get searchParams() {
      if (!this.#sp) {
        const sp = new URLSearchParams(this.#u.query || '');
        spLink(sp, (q) => {
          const u = this.#u;
          u.query = q === '' ? null : q;
        });
        this.#sp = sp;
      }
      return this.#sp;
    }
    get hash() { const f = this.#u.fragment; return f === null || f === '' ? '' : '#' + f; }
    set hash(v) {
      const u = this.#u;
      v = String(v);
      if (v === '') { u.fragment = null; return; }
      u.fragment = '';
      parseURL(v.charCodeAt(0) === 0x23 ? v.slice(1) : v, null, u, S_FRAGMENT);
    }
    toString() { return serialize(this.#u); }
    toJSON() { return serialize(this.#u); }
  }
  for (const k of ['href', 'origin', 'protocol', 'username', 'password', 'host', 'hostname', 'port', 'pathname', 'search', 'searchParams', 'hash']) {
    Object.defineProperty(URL.prototype, k, { ...Object.getOwnPropertyDescriptor(URL.prototype, k), enumerable: true });
  }
  expose('URL', URL);
  def(G, 'webkitURL', URL);
  jr.blobURLs = blobURLs;

  // the document's URL (location / history keep it current)
  let loc = parseURL('https://app.slack.com/client/T0266FRGM', null);
  const locHref = () => serialize(loc);
  const parseRelative = (s) => parseURL(String(s), loc);

  // ---- AbortController / AbortSignal ----
  let asAbort, asOn, asOff, asNew, asFollow;
  const liveSignals = new Set(); // dependent signals with listeners stay alive
  const abortReason = () => new DOMException('signal is aborted without reason', 'AbortError');
  class AbortSignal extends EventTarget {
    #aborted = false;
    #reason = undefined;
    #algos = null; // internal abort steps (fetch, pipeTo …)
    #deps = null; // Set<WeakRef<AbortSignal>> of dependents
    #dependent = false;
    static {
      asNew = () => new AbortSignal(kToken);
      asAbort = (s, r) => s.#abort(r);
      asOn = (s, fn) => { (s.#algos || (s.#algos = new Set())).add(fn); };
      asOff = (s, fn) => { if (s.#algos) s.#algos.delete(fn); };
      // a new signal that aborts when any of srcs does
      asFollow = (srcs) => {
        const s = new AbortSignal(kToken);
        for (const x of srcs) if (x.#aborted) { s.#aborted = true; s.#reason = x.#reason; return s; }
        s.#dependent = true;
        const ref = new WeakRef(s);
        for (const x of srcs) {
          const deps = x.#deps || (x.#deps = new Set());
          if (deps.size >= 32 && (deps.size & 31) === 0) for (const w of deps) if (!w.deref()) deps.delete(w);
          deps.add(ref);
        }
        return s;
      };
    }
    constructor(t) {
      if (t !== kToken) throw illegal();
      super();
    }
    get aborted() { return this.#aborted; }
    get reason() { return this.#reason; }
    throwIfAborted() { if (this.#aborted) throw this.#reason; }
    addEventListener(type, cb, opts) {
      super.addEventListener(type, cb, opts);
      if (this.#dependent && !this.#aborted && String(type) === 'abort' && cb) liveSignals.add(this);
    }
    #abort(reason) {
      if (this.#aborted) return;
      if (reason === undefined) reason = abortReason();
      const all = [this];
      this.#aborted = true;
      this.#reason = reason;
      for (let i = 0; i < all.length; i++) {
        const deps = all[i].#deps;
        if (!deps) continue;
        all[i].#deps = null;
        for (const w of deps) {
          const d = w.deref();
          if (d && !d.#aborted) { d.#aborted = true; d.#reason = reason; all.push(d); }
        }
      }
      for (const s of all) s.#run();
    }
    #run() {
      liveSignals.delete(this);
      const a = this.#algos;
      this.#algos = null;
      if (a) for (const f of a) { try { f(this.#reason); } catch (e) { reportError(e); } }
      this.dispatchEvent(new Event('abort'));
    }
    static abort(reason = undefined) {
      const s = new AbortSignal(kToken);
      s.#aborted = true;
      s.#reason = reason === undefined ? abortReason() : reason;
      return s;
    }
    static timeout(ms) {
      need(arguments, 1, 'timeout', 'AbortSignal');
      ms = Number(ms);
      if (!Number.isFinite(ms) || ms < 0) throw new TypeError("Failed to execute 'timeout' on 'AbortSignal': Value is outside the 'unsigned long long' value range.");
      const s = new AbortSignal(kToken);
      setTimeout(() => s.#abort(new DOMException('signal timed out', 'TimeoutError')), ms);
      return s;
    }
    static any(signals) {
      need(arguments, 1, 'any', 'AbortSignal');
      const list = Array.from(signals);
      for (const x of list) if (!(isObj(x) && #aborted in x)) throw new TypeError("Failed to execute 'any' on 'AbortSignal': Failed to convert value to 'AbortSignal'.");
      return asFollow(list);
    }
  }
  Object.defineProperty(AbortSignal.prototype, 'onabort', {
    get() { const h = this[kOn]; return (h && h.abort) || null; },
    set(v) {
      let h = this[kOn];
      if (!h) { h = {}; def(this, kOn, h); }
      h.abort = typeof v === 'function' || isObj(v) ? v : null;
      if (h.abort && !this.aborted) liveSignals.add(this);
    },
    enumerable: true, configurable: true,
  });
  class AbortController {
    #signal = asNew();
    get signal() { return this.#signal; }
    abort(reason = undefined) { asAbort(this.#signal, reason); }
  }
  expose('AbortSignal', AbortSignal);
  expose('AbortController', AbortController);
  const isSignal = (x) => x instanceof AbortSignal;

  // ---- Blob / File ----
  let blobBytes, blobMake;
  const normType = (t) => {
    t = String(t);
    for (let i = 0; i < t.length; i++) { const c = t.charCodeAt(i); if (c < 0x20 || c > 0x7e) return ''; }
    return t.toLowerCase();
  };
  const clampInt = (x) => { const n = Math.trunc(Number(x)); return n !== n ? 0 : n; };
  class Blob {
    #b = EMPTY;
    #t = '';
    static {
      blobBytes = (b) => b.#b;
      blobMake = (u8, type) => { const o = new Blob(); o.#b = u8; o.#t = type; return o; };
    }
    constructor(parts = undefined, options = undefined) {
      if (parts !== undefined) {
        if (!isObj(parts) || typeof parts[Symbol.iterator] !== 'function') throw new TypeError("Failed to construct 'Blob': The provided value cannot be converted to a sequence.");
        let endings = 'transparent';
        if (options && options.endings !== undefined) {
          endings = String(options.endings);
          if (endings !== 'transparent' && endings !== 'native') throw new TypeError(`Failed to construct 'Blob': Failed to read the 'endings' property from 'BlobPropertyBag': The provided value '${endings}' is not a valid enum value of type EndingType.`);
        }
        const chunks = [];
        let total = 0, reuse = false;
        for (const p of parts) {
          let u;
          if (isObj(p) && #b in p) { u = p.#b; reuse = true; } // blobs are immutable
          else if ((u = toBytes(p)) !== null) reuse = false; // caller-owned: copy
          else {
            let s = usv(p);
            if (endings === 'native') s = s.replace(/\r\n?/g, '\n');
            u = utf8Encode(s);
            reuse = true;
          }
          chunks.push(u);
          total += u.length;
        }
        if (chunks.length === 1) this.#b = reuse ? chunks[0] : chunks[0].slice();
        else if (chunks.length) this.#b = concat(chunks, total);
      }
      if (options && options.type !== undefined) this.#t = normType(options.type);
    }
    get size() { return this.#b.length; }
    get type() { return this.#t; }
    slice(start = undefined, end = undefined, contentType = undefined) {
      const size = this.#b.length;
      let s = start === undefined ? 0 : clampInt(start);
      s = s < 0 ? Math.max(size + s, 0) : Math.min(s, size);
      let e = end === undefined ? size : clampInt(end);
      e = e < 0 ? Math.max(size + e, 0) : Math.min(e, size);
      return blobMake(this.#b.subarray(s, Math.max(s, e)), contentType === undefined ? '' : normType(contentType));
    }
    text() { return Promise.resolve(utf8Text(this.#b)); }
    arrayBuffer() { return Promise.resolve(this.#b.slice().buffer); }
    bytes() { return Promise.resolve(this.#b.slice()); }
    stream() { return rsFromBytes(this.#b.slice()); }
  }
  class File extends Blob {
    #name;
    #lm;
    constructor(bits, name, options = undefined) {
      need(arguments, 2, null, 'File');
      super(bits, options);
      this.#name = usv(name);
      this.#lm = options && options.lastModified !== undefined ? clampInt(options.lastModified) : Date.now();
    }
    get name() { return this.#name; }
    get lastModified() { return this.#lm; }
    get webkitRelativePath() { return ''; }
  }
  expose('Blob', Blob);
  expose('File', File);

  // ---- FileReader ----
  class FileReader extends EventTarget {
    #state = 0;
    #result = null;
    #error = null;
    #gen = 0;
    get readyState() { return this.#state; }
    get result() { return this.#result; }
    get error() { return this.#error; }
    #fire(type, loaded, total) { this.dispatchEvent(new ProgressEvent(type, { lengthComputable: true, loaded, total })); }
    #read(blob, kind, enc, method) {
      if (!(blob instanceof Blob)) throw new TypeError(`Failed to execute '${method}' on 'FileReader': parameter 1 is not of type 'Blob'.`);
      if (this.#state === 1) throw new DOMException(`Failed to execute '${method}' on 'FileReader': The object is already busy reading Blobs.`, 'InvalidStateError');
      this.#state = 1;
      this.#result = null;
      this.#error = null;
      const gen = ++this.#gen;
      const bytes = blobBytes(blob), total = bytes.length, type = blob.type;
      jr.immediate(() => {
        if (gen !== this.#gen) return;
        this.#fire('loadstart', 0, total);
        jr.immediate(() => {
          if (gen !== this.#gen) return;
          let r;
          try {
            switch (kind) {
              case 'buffer': r = bytes.slice().buffer; break;
              case 'binary': r = latin1(bytes); break;
              case 'url': r = 'data:' + (type || 'application/octet-stream') + ';base64,' + b64encode(bytes); break;
              default: {
                let d;
                try { d = new TextDecoder(enc === undefined ? 'utf-8' : enc); } catch { d = new TextDecoder(); }
                r = d.decode(bytes);
              }
            }
          } catch (e) {
            this.#state = 2;
            this.#error = new DOMException(String(e && e.message), 'NotReadableError');
            this.#fire('error', 0, total);
            if (gen === this.#gen && this.#state === 2) this.#fire('loadend', 0, total);
            return;
          }
          this.#fire('progress', total, total);
          if (gen !== this.#gen) return;
          this.#state = 2;
          this.#result = r;
          this.#fire('load', total, total);
          if (gen === this.#gen && this.#state === 2) this.#fire('loadend', total, total);
        });
      });
    }
    readAsArrayBuffer(blob) { need(arguments, 1, 'readAsArrayBuffer', 'FileReader'); this.#read(blob, 'buffer', undefined, 'readAsArrayBuffer'); }
    readAsBinaryString(blob) { need(arguments, 1, 'readAsBinaryString', 'FileReader'); this.#read(blob, 'binary', undefined, 'readAsBinaryString'); }
    readAsDataURL(blob) { need(arguments, 1, 'readAsDataURL', 'FileReader'); this.#read(blob, 'url', undefined, 'readAsDataURL'); }
    readAsText(blob, encoding = undefined) { need(arguments, 1, 'readAsText', 'FileReader'); this.#read(blob, 'text', encoding, 'readAsText'); }
    abort() {
      if (this.#state !== 1) { this.#result = null; return; }
      this.#gen++;
      this.#state = 2;
      this.#result = null;
      this.#fire('abort', 0, 0);
      if (this.#state === 2) this.#fire('loadend', 0, 0);
    }
  }
  constants(FileReader, { EMPTY: 0, LOADING: 1, DONE: 2 });
  handlers(FileReader.prototype, ['loadstart', 'progress', 'load', 'abort', 'error', 'loadend']);
  expose('FileReader', FileReader);

  // ---- FormData ----
  let fdList;
  const fdEntry = (name, value, filename, nargs, method) => {
    name = usv(name);
    if (value instanceof Blob) {
      if (!(value instanceof File) || filename !== undefined) {
        value = new File([value], filename !== undefined ? usv(filename) : value instanceof File ? value.name : 'blob',
          { type: value.type, lastModified: value instanceof File ? value.lastModified : undefined });
      }
      return [name, value];
    }
    if (nargs > 2) throw new TypeError(`Failed to execute '${method}' on 'FormData': parameter 2 is not of type 'Blob'.`);
    return [name, usv(value)];
  };
  class FormData {
    #l = [];
    static { fdList = (f) => f.#l; }
    constructor(form = undefined, submitter = undefined) {
      if (form === undefined) return;
      if (!isObj(form) || !form.elements) throw new TypeError("Failed to construct 'FormData': parameter 1 is not of type 'HTMLFormElement'.");
      for (const el of Array.from(form.elements)) {
        const name = el.name, type = String(el.type || '').toLowerCase();
        if (!name || el.disabled || type === 'submit' || type === 'button' || type === 'reset' || type === 'image' || type === 'file') continue;
        if ((type === 'checkbox' || type === 'radio') && !el.checked) continue;
        this.#l.push([usv(name), usv(el.value == null ? (type === 'checkbox' || type === 'radio' ? 'on' : '') : el.value)]);
      }
    }
    append(name, value, filename = undefined) {
      need(arguments, 2, 'append', 'FormData');
      this.#l.push(fdEntry(name, value, filename, arguments.length, 'append'));
    }
    delete(name) { need(arguments, 1, 'delete', 'FormData'); name = usv(name); this.#l = this.#l.filter((e) => e[0] !== name); }
    get(name) {
      need(arguments, 1, 'get', 'FormData');
      name = usv(name);
      for (const e of this.#l) if (e[0] === name) return e[1];
      return null;
    }
    getAll(name) { need(arguments, 1, 'getAll', 'FormData'); name = usv(name); return this.#l.filter((e) => e[0] === name).map((e) => e[1]); }
    has(name) { need(arguments, 1, 'has', 'FormData'); name = usv(name); return this.#l.some((e) => e[0] === name); }
    set(name, value, filename = undefined) {
      need(arguments, 2, 'set', 'FormData');
      const e = fdEntry(name, value, filename, arguments.length, 'set');
      const l = this.#l;
      const i = l.findIndex((x) => x[0] === e[0]);
      if (i < 0) l.push(e);
      else { l[i] = e; this.#l = l.filter((x, j) => j <= i || x[0] !== e[0]); }
    }
    forEach(cb, thisArg = undefined) {
      if (typeof cb !== 'function') throw new TypeError("Failed to execute 'forEach' on 'FormData': The callback provided as parameter 1 is not a function.");
      for (let i = 0; i < this.#l.length; i++) cb.call(thisArg, this.#l[i][1], this.#l[i][0], this);
    }
    *entries() { for (let i = 0; i < this.#l.length; i++) yield [this.#l[i][0], this.#l[i][1]]; }
    *keys() { for (let i = 0; i < this.#l.length; i++) yield this.#l[i][0]; }
    *values() { for (let i = 0; i < this.#l.length; i++) yield this.#l[i][1]; }
  }
  def(FormData.prototype, Symbol.iterator, FormData.prototype.entries);
  expose('FormData', FormData);

  const ALNUM = 'ABCDEFGHIJKLMNOPQRSTUVWXYZabcdefghijklmnopqrstuvwxyz0123456789';
  const randomToken = (n) => { const r = randBytes(n); let s = ''; for (let i = 0; i < n; i++) s += ALNUM[r[i] % 62]; return s; };
  const CRLF = new Uint8Array([13, 10]);
  const escName = (s) => s.replace(/\r\n|\r|\n/g, '\r\n').replace(/[\n\r"]/g, (c) => c === '\n' ? '%0A' : c === '\r' ? '%0D' : '%22');
  const multipart = (fd) => {
    const boundary = '----ClackFormBoundary' + randomToken(16);
    const parts = [];
    for (const [name, v] of fdList(fd)) {
      let head = '--' + boundary + '\r\nContent-Disposition: form-data; name="' + escName(name) + '"';
      if (typeof v === 'string') parts.push(utf8Encode(head + '\r\n\r\n' + v.replace(/\r\n|\r|\n/g, '\r\n') + '\r\n'));
      else {
        head += '; filename="' + escName(v.name) + '"\r\nContent-Type: ' + (v.type || 'application/octet-stream') + '\r\n\r\n';
        parts.push(utf8Encode(head), blobBytes(v), CRLF);
      }
    }
    parts.push(utf8Encode('--' + boundary + '--\r\n'));
    return { bytes: concat(parts), type: 'multipart/form-data; boundary=' + boundary };
  };
  const parseMultipart = (bytes, ct, iface) => {
    const bad = () => new TypeError(`Failed to execute 'formData' on '${iface}': Failed to parse body as FormData.`);
    const m = /;\s*boundary=(?:"([^"]+)"|([^;\s]+))/i.exec(ct);
    if (!m) throw bad();
    const delim = '--' + (m[1] || m[2]);
    const s = latin1(bytes), fd = new FormData();
    const u8 = (x) => utf8Decode(fromLatin1(x), newUtf8State(), false, true);
    let pos = s.indexOf(delim);
    if (pos < 0) throw bad();
    for (;;) {
      pos += delim.length;
      if (s.startsWith('--', pos)) break;
      if (s.startsWith('\r\n', pos)) pos += 2;
      const next = s.indexOf('\r\n' + delim, pos);
      if (next < 0) throw bad();
      const part = s.slice(pos, next);
      const he = part.indexOf('\r\n\r\n');
      if (he < 0) throw bad();
      let name = null, filename, type = '';
      for (const line of part.slice(0, he).split('\r\n')) {
        const c = line.indexOf(':');
        if (c < 0) continue;
        const hn = line.slice(0, c).trim().toLowerCase(), hv = line.slice(c + 1).trim();
        if (hn === 'content-disposition') {
          const nm = /;\s*name="([^"]*)"/i.exec(hv) || /;\s*name=([^;]*)/i.exec(hv);
          const fm = /;\s*filename="([^"]*)"/i.exec(hv) || /;\s*filename=([^;]*)/i.exec(hv);
          if (nm) name = u8(nm[1]).replace(/%0A/g, '\n').replace(/%0D/g, '\r').replace(/%22/g, '"');
          if (fm) filename = u8(fm[1]).replace(/%0A/g, '\n').replace(/%0D/g, '\r').replace(/%22/g, '"');
        } else if (hn === 'content-type') type = hv;
      }
      if (name === null) throw bad();
      const body = part.slice(he + 4);
      if (filename !== undefined) fd.append(name, new File([fromLatin1(body)], filename, { type }));
      else fd.append(name, u8(body));
      pos = next + 2;
    }
    return fd;
  };

  // ---- Streams ----
  const strategyOf = (s, dflt, iface) => {
    let hwm = dflt, size = null;
    if (s !== undefined && s !== null) {
      if (s.highWaterMark !== undefined) {
        hwm = Number(s.highWaterMark);
        if (hwm !== hwm || hwm < 0) throw new RangeError(`Failed to construct '${iface}': Cannot construct a ${iface} with a negative or NaN high water mark.`);
      }
      if (s.size !== undefined) {
        if (typeof s.size !== 'function') throw new TypeError(`Failed to construct '${iface}': Failed to read the 'size' property from 'QueuingStrategy': The provided value is not a function.`);
        size = s.size;
      }
    }
    return { hwm, size };
  };
  const settleable = () => {
    const d = { settled: false, p: null, res: null, rej: null };
    d.p = new Promise((a, b) => {
      d.res = (v) => { if (!d.settled) { d.settled = true; a(v); } };
      d.rej = (e) => { if (!d.settled) { d.settled = true; b(e); } };
    });
    d.p.catch(() => {});
    return d;
  };
  let rsOf, wsOf;
  const rsDesired = (s) => s.state === 'errored' ? null : s.state === 'closed' ? 0 : s.hwm - s.qsize;
  const rsFinishClose = (s) => {
    if (s.state !== 'readable') return;
    s.state = 'closed';
    const r = s.reader;
    if (r) {
      const reqs = r.reqs;
      r.reqs = [];
      for (const q of reqs) q.close();
      r.closed.res();
    }
  };
  const rsError = (s, e) => {
    if (s.state !== 'readable') return;
    s.state = 'errored';
    s.storedError = e;
    s.queue = [];
    s.qsize = 0;
    const r = s.reader;
    if (r) {
      const reqs = r.reqs;
      r.reqs = [];
      for (const q of reqs) q.error(e);
      r.closed.rej(e);
    }
  };
  const rsPull = (s) => {
    if (!s.started || s.state !== 'readable' || s.closeRequested || !s.pullFn) return;
    const r = s.reader;
    if (!((r && r.reqs.length > 0) || s.hwm - s.qsize > 0)) return;
    if (s.pulling) { s.pullAgain = true; return; }
    s.pulling = true;
    let p;
    try { p = s.pullFn.call(s.src, s.controller); } catch (e) { rsError(s, e); return; }
    Promise.resolve(p).then(() => {
      s.pulling = false;
      if (s.pullAgain) { s.pullAgain = false; rsPull(s); }
    }, (e) => rsError(s, e));
  };
  // hand a chunk to a read request; byob requests take at most q.max bytes
  const rsDeliver = (s, q, c, n) => {
    if (q.max !== undefined && c.length > q.max) {
      s.queue.unshift({ c: c.subarray(q.max), n: c.length - q.max });
      s.qsize += c.length - q.max;
      c = c.subarray(0, q.max);
    }
    q.chunk(c);
  };
  const rsEnqueue = (s, chunk, iface) => {
    if (s.closeRequested || s.state !== 'readable') throw new TypeError(`Failed to execute 'enqueue' on '${iface || 'ReadableStreamDefaultController'}': Cannot enqueue a chunk into a readable stream that is closed or has been requested to be closed`);
    const r = s.reader;
    if (r && r.reqs.length && !s.queue.length) rsDeliver(s, r.reqs.shift(), chunk);
    else {
      let n = 1;
      if (s.bytes) n = chunk.length;
      else if (s.size) {
        try { n = Number(s.size.call(undefined, chunk)); } catch (e) { rsError(s, e); throw e; }
        if (!(n >= 0) || n === Infinity) { const e = new RangeError('The return value of a queuing strategy\'s size function must be a finite, non-NaN, non-negative number'); rsError(s, e); throw e; }
      }
      s.queue.push({ c: chunk, n });
      s.qsize += n;
    }
    rsPull(s);
  };
  const rsClose = (s, iface) => {
    if (s.closeRequested || s.state !== 'readable') throw new TypeError(`Failed to execute 'close' on '${iface || 'ReadableStreamDefaultController'}': Unexpected call to close`);
    s.closeRequested = true;
    if (!s.queue.length) rsFinishClose(s);
  };
  const rsRead = (s, q) => {
    s.disturbed = true;
    if (s.state === 'closed') q.close();
    else if (s.state === 'errored') q.error(s.storedError);
    else if (s.queue.length) {
      const e = s.queue.shift();
      s.qsize -= e.n;
      if (s.qsize < 1e-9) s.qsize = 0;
      rsDeliver(s, q, e.c);
      if (s.closeRequested && !s.queue.length) rsFinishClose(s);
      else rsPull(s);
    } else {
      s.reader.reqs.push(q);
      rsPull(s);
    }
  };
  const rsCancel = (s, reason) => {
    s.disturbed = true;
    if (s.state === 'closed') return Promise.resolve();
    if (s.state === 'errored') return Promise.reject(s.storedError);
    s.queue = [];
    s.qsize = 0;
    rsFinishClose(s);
    let r;
    try { r = s.cancelFn ? s.cancelFn.call(s.src, reason) : undefined; } catch (e) { return Promise.reject(e); }
    return Promise.resolve(r).then(() => undefined);
  };
  const attachReader = (s) => {
    const r = { reqs: [], closed: settleable() };
    if (s.state === 'closed') r.closed.res();
    else if (s.state === 'errored') r.closed.rej(s.storedError);
    s.reader = r;
    return r;
  };
  const detachReader = (s, r) => {
    const e = new TypeError('Releasing reader');
    if (r.closed.settled) r.closed = settleable();
    r.closed.rej(e);
    const reqs = r.reqs;
    r.reqs = [];
    for (const q of reqs) q.error(e);
    s.reader = null;
  };
  const released = (m, iface) => new TypeError(`Failed to execute '${m}' on '${iface}': This readable stream reader has been released and cannot be used to read from its previous owner stream`);

  class ReadableStreamDefaultController {
    #s;
    constructor(t, s) { if (t !== kToken) throw illegal(); this.#s = s; }
    get desiredSize() { return rsDesired(this.#s); }
    enqueue(chunk = undefined) { rsEnqueue(this.#s, chunk); }
    close() { rsClose(this.#s); }
    error(e = undefined) { rsError(this.#s, e); }
  }
  class ReadableByteStreamController {
    #s;
    constructor(t, s) { if (t !== kToken) throw illegal(); this.#s = s; }
    get byobRequest() { return null; }
    get desiredSize() { return rsDesired(this.#s); }
    enqueue(chunk) {
      need(arguments, 1, 'enqueue', 'ReadableByteStreamController');
      if (!ArrayBuffer.isView(chunk)) throw new TypeError("Failed to execute 'enqueue' on 'ReadableByteStreamController': parameter 1 is not of type 'ArrayBufferView'.");
      if (chunk.byteLength === 0) throw new TypeError("Failed to execute 'enqueue' on 'ReadableByteStreamController': chunk is empty");
      rsEnqueue(this.#s, new Uint8Array(chunk.buffer.slice(chunk.byteOffset, chunk.byteOffset + chunk.byteLength)), 'ReadableByteStreamController');
    }
    close() { rsClose(this.#s, 'ReadableByteStreamController'); }
    error(e = undefined) { rsError(this.#s, e); }
  }
  class ReadableStreamDefaultReader {
    #s = null;
    #r = null;
    constructor(stream) {
      need(arguments, 1, null, 'ReadableStreamDefaultReader');
      const s = rsOf(stream);
      if (!s) throw new TypeError("Failed to construct 'ReadableStreamDefaultReader': parameter 1 is not of type 'ReadableStream'.");
      if (s.reader) throw new TypeError("Failed to construct 'ReadableStreamDefaultReader': ReadableStreamDefaultReader constructor can only accept readable streams that are not yet locked to a reader");
      this.#s = s;
      this.#r = attachReader(s);
    }
    get closed() { return this.#r.closed.p; }
    read() {
      const s = this.#s;
      if (!s) return Promise.reject(released('read', 'ReadableStreamDefaultReader'));
      return new Promise((resolve, reject) => rsRead(s, {
        chunk: (c) => resolve({ value: c, done: false }),
        close: () => resolve({ value: undefined, done: true }),
        error: reject,
      }));
    }
    releaseLock() { const s = this.#s; if (!s) return; detachReader(s, this.#r); this.#s = null; }
    cancel(reason = undefined) {
      const s = this.#s;
      if (!s) return Promise.reject(released('cancel', 'ReadableStreamDefaultReader'));
      return rsCancel(s, reason);
    }
  }
  class ReadableStreamBYOBReader {
    #s = null;
    #r = null;
    constructor(stream) {
      need(arguments, 1, null, 'ReadableStreamBYOBReader');
      const s = rsOf(stream);
      if (!s) throw new TypeError("Failed to construct 'ReadableStreamBYOBReader': parameter 1 is not of type 'ReadableStream'.");
      if (!s.bytes) throw new TypeError("Failed to construct 'ReadableStreamBYOBReader': Cannot use a BYOB reader with a non-byte stream");
      if (s.reader) throw new TypeError("Failed to construct 'ReadableStreamBYOBReader': ReadableStreamBYOBReader constructor can only accept readable streams that are not yet locked to a reader");
      this.#s = s;
      this.#r = attachReader(s);
    }
    get closed() { return this.#r.closed.p; }
    read(view, options = undefined) {
      const s = this.#s;
      if (!s) return Promise.reject(released('read', 'ReadableStreamBYOBReader'));
      if (!ArrayBuffer.isView(view)) return Promise.reject(new TypeError("Failed to execute 'read' on 'ReadableStreamBYOBReader': parameter 1 is not of type 'ArrayBufferView'."));
      if (view.byteLength === 0) return Promise.reject(new TypeError("Failed to execute 'read' on 'ReadableStreamBYOBReader': This readable stream reader cannot be used to read as the view has byte length equal to 0"));
      const es = view.BYTES_PER_ELEMENT || 1;
      const mk = (n) => new view.constructor(view.buffer, view.byteOffset, view instanceof DataView ? n : n / es);
      return new Promise((resolve, reject) => rsRead(s, {
        max: view.byteLength - (view.byteLength % es),
        chunk: (c) => {
          const n = c.length - (c.length % es);
          new Uint8Array(view.buffer, view.byteOffset, view.byteLength).set(c.subarray(0, n));
          if (n < c.length) { s.queue.unshift({ c: c.subarray(n), n: c.length - n }); s.qsize += c.length - n; }
          resolve({ value: mk(n), done: false });
        },
        close: () => resolve({ value: mk(0), done: true }),
        error: reject,
      }));
    }
    releaseLock() { const s = this.#s; if (!s) return; detachReader(s, this.#r); this.#s = null; }
    cancel(reason = undefined) {
      const s = this.#s;
      if (!s) return Promise.reject(released('cancel', 'ReadableStreamBYOBReader'));
      return rsCancel(s, reason);
    }
  }
  const AsyncIteratorProto = Object.getPrototypeOf(Object.getPrototypeOf((async function* () {}).prototype));
  const RSIterProto = Object.create(AsyncIteratorProto, { [Symbol.toStringTag]: { value: 'ReadableStream AsyncIterator', configurable: true } });
  const newRS = (stream, bytes, hwm, size) => ({
    stream, bytes, hwm, size, queue: [], qsize: 0, state: 'readable', storedError: undefined, reader: null,
    disturbed: false, started: false, pulling: false, pullAgain: false, closeRequested: false,
    pullFn: null, cancelFn: null, src: null, controller: null,
  });

  class ReadableStream {
    #s;
    static { rsOf = (x) => (isObj(x) && #s in x) ? x.#s : null; }
    constructor(source = undefined, strategy = undefined) {
      if (source === kToken) {
        // a closed byte stream holding one chunk (Blob.stream(), bodies)
        const s = this.#s = newRS(this, true, 0, null);
        s.controller = new ReadableByteStreamController(kToken, s);
        if (strategy.length) { s.queue.push({ c: strategy, n: strategy.length }); s.qsize = strategy.length; }
        s.closeRequested = true;
        s.started = true;
        if (!s.queue.length) s.state = 'closed';
        return;
      }
      if (source === null) throw new TypeError("Failed to construct 'ReadableStream': The provided value is not of type 'UnderlyingSource'.");
      const src = source === undefined ? {} : source;
      let bytes = false;
      if (src.type !== undefined) {
        if (String(src.type) !== 'bytes') throw new TypeError(`Failed to construct 'ReadableStream': The provided value '${src.type}' is not a valid enum value of type ReadableStreamType.`);
        bytes = true;
      }
      const { hwm, size } = strategyOf(strategy, bytes ? 0 : 1, 'ReadableStream');
      if (bytes && size) throw new RangeError("Failed to construct 'ReadableStream': Cannot create a ReadableStream with type 'bytes' and a size function");
      const s = this.#s = newRS(this, bytes, hwm, size);
      for (const k of ['start', 'pull', 'cancel']) {
        if (src[k] !== undefined && typeof src[k] !== 'function') throw new TypeError(`Failed to construct 'ReadableStream': Failed to read the '${k}' property from 'UnderlyingSource': The provided value is not a function.`);
      }
      s.src = src;
      s.pullFn = src.pull || null;
      s.cancelFn = src.cancel || null;
      s.controller = bytes ? new ReadableByteStreamController(kToken, s) : new ReadableStreamDefaultController(kToken, s);
      const r = src.start ? src.start.call(src, s.controller) : undefined;
      Promise.resolve(r).then(() => { s.started = true; rsPull(s); }, (e) => rsError(s, e));
    }
    get locked() { return this.#s.reader !== null; }
    cancel(reason = undefined) {
      if (this.#s.reader) return Promise.reject(new TypeError("Failed to execute 'cancel' on 'ReadableStream': Cannot cancel a locked stream"));
      return rsCancel(this.#s, reason);
    }
    getReader(options = undefined) {
      const mode = options && options.mode !== undefined ? String(options.mode) : undefined;
      if (mode === 'byob') return new ReadableStreamBYOBReader(this);
      if (mode !== undefined) throw new TypeError(`Failed to execute 'getReader' on 'ReadableStream': Failed to read the 'mode' property from 'ReadableStreamGetReaderOptions': The provided value '${mode}' is not a valid enum value of type ReadableStreamReaderMode.`);
      if (this.#s.reader) throw new TypeError("Failed to execute 'getReader' on 'ReadableStream': ReadableStreamDefaultReader constructor can only accept readable streams that are not yet locked to a reader");
      return new ReadableStreamDefaultReader(this);
    }
    tee() {
      const src = this.#s;
      const reader = this.getReader();
      let reading = false, readAgain = false, canceled1 = false, canceled2 = false, reason1, reason2;
      let b1, b2;
      const cancelDone = settleable();
      const pull = () => {
        if (reading) { readAgain = true; return; }
        reading = true;
        reader.read().then(({ value, done }) => {
          reading = false;
          const s1 = rsOf(b1), s2 = rsOf(b2);
          if (done) {
            if (!canceled1 && s1.state === 'readable' && !s1.closeRequested) rsClose(s1);
            if (!canceled2 && s2.state === 'readable' && !s2.closeRequested) rsClose(s2);
            cancelDone.res();
            return;
          }
          const v2 = src.bytes && !canceled1 ? value.slice() : value;
          if (!canceled1) rsEnqueue(s1, value);
          if (!canceled2) rsEnqueue(s2, v2);
          if (readAgain) { readAgain = false; pull(); }
        }, () => { reading = false; });
      };
      const cancel = (which) => (reason) => {
        if (which === 1) { canceled1 = true; reason1 = reason; } else { canceled2 = true; reason2 = reason; }
        if (canceled1 && canceled2) reader.cancel([reason1, reason2]).then(() => cancelDone.res(), (e) => cancelDone.rej(e));
        return cancelDone.p;
      };
      const type = src.bytes ? 'bytes' : undefined;
      b1 = new ReadableStream({ type, pull, cancel: cancel(1) });
      b2 = new ReadableStream({ type, pull, cancel: cancel(2) });
      reader.closed.catch((e) => { rsError(rsOf(b1), e); rsError(rsOf(b2), e); cancelDone.res(); });
      return [b1, b2];
    }
    pipeTo(dest, options = undefined) {
      const s = this.#s;
      const ws = wsOf(dest);
      if (!ws) return Promise.reject(new TypeError("Failed to execute 'pipeTo' on 'ReadableStream': parameter 1 is not of type 'WritableStream'."));
      const o = options || {};
      const preventClose = !!o.preventClose, preventAbort = !!o.preventAbort, preventCancel = !!o.preventCancel, signal = o.signal;
      if (signal !== undefined && !isSignal(signal)) return Promise.reject(new TypeError("Failed to execute 'pipeTo' on 'ReadableStream': Failed to read the 'signal' property from 'StreamPipeOptions': Failed to convert value to 'AbortSignal'."));
      if (s.reader) return Promise.reject(new TypeError("Failed to execute 'pipeTo' on 'ReadableStream': Cannot pipe a locked stream"));
      if (ws.writer) return Promise.reject(new TypeError("Failed to execute 'pipeTo' on 'ReadableStream': Cannot pipe to a locked stream"));
      const reader = this.getReader(), writer = dest.getWriter();
      return new Promise((resolve, reject) => {
        let shutting = false, lastWrite = Promise.resolve(), pendingRead = Promise.resolve();
        const finish = (isErr, err) => {
          if (signal) signal.removeEventListener('abort', onAbort);
          writer.releaseLock();
          reader.releaseLock();
          if (isErr) reject(err); else resolve(undefined);
        };
        const shutdown = (action, isErr, err) => {
          if (shutting) return;
          shutting = true;
          const go = () => {
            let p;
            try { p = action ? Promise.resolve(action()) : Promise.resolve(); } catch (e) { p = Promise.reject(e); }
            p.then(() => finish(isErr, err), (e) => finish(true, e));
          };
          // Chunks already read are still written before the action (spec:
          // "if any chunks have been read but not yet written, write them"):
          // a pre-closed source (Blob.stream()) closes while its only read
          // is still resolving.
          const flushed = () => lastWrite.then(go, go);
          pendingRead.then(flushed, flushed);
        };
        const onAbort = () => {
          const err = signal.reason;
          shutdown(() => Promise.all([
            !preventAbort && ws.state === 'writable' ? writer.abort(err) : undefined,
            !preventCancel && s.state === 'readable' ? reader.cancel(err) : undefined,
          ]), true, err);
        };
        if (signal) {
          if (signal.aborted) { onAbort(); return; }
          signal.addEventListener('abort', onAbort);
        }
        reader.closed.then(
          () => shutdown(preventClose ? null : () => writer.close(), false),
          (e) => shutdown(preventAbort ? null : () => writer.abort(e), true, e));
        writer.closed.then(() => {
          const e = new TypeError('the destination writable stream closed before all data could be piped to it');
          shutdown(preventCancel ? null : () => reader.cancel(e), true, e);
        }, (e) => shutdown(preventCancel ? null : () => reader.cancel(e), true, e));
        const pump = () => {
          if (shutting) return;
          writer.ready.then(() => {
            if (shutting) return;
            pendingRead = reader.read().then(({ value, done }) => {
              if (done || ws.state !== 'writable') return;
              lastWrite = writer.write(value);
              lastWrite.catch(() => {});
              if (!shutting) pump();
            });
            return pendingRead;
          }).catch(() => {});
        };
        pump();
      });
    }
    pipeThrough(transform, options = undefined) {
      const t = transform || {};
      if (!wsOf(t.writable)) throw new TypeError("Failed to execute 'pipeThrough' on 'ReadableStream': Failed to read the 'writable' property from 'ReadableWritablePair': Required member is undefined.");
      if (!rsOf(t.readable)) throw new TypeError("Failed to execute 'pipeThrough' on 'ReadableStream': Failed to read the 'readable' property from 'ReadableWritablePair': Required member is undefined.");
      if (this.#s.reader) throw new TypeError("Failed to execute 'pipeThrough' on 'ReadableStream': Cannot pipe a locked stream");
      if (wsOf(t.writable).writer) throw new TypeError("Failed to execute 'pipeThrough' on 'ReadableStream': Cannot pipe to a locked stream");
      this.pipeTo(t.writable, options).catch(() => {});
      return t.readable;
    }
    values(options = undefined) {
      const reader = this.getReader();
      const preventCancel = !!(options && options.preventCancel);
      let done = false;
      const it = Object.create(RSIterProto);
      def(it, 'next', () => {
        if (done) return Promise.resolve({ value: undefined, done: true });
        return reader.read().then((r) => {
          if (r.done) { done = true; reader.releaseLock(); }
          return r;
        }, (e) => { done = true; reader.releaseLock(); throw e; });
      });
      def(it, 'return', (value) => {
        if (done) return Promise.resolve({ value, done: true });
        done = true;
        if (!preventCancel) {
          const p = reader.cancel(value);
          reader.releaseLock();
          return p.then(() => ({ value, done: true }));
        }
        reader.releaseLock();
        return Promise.resolve({ value, done: true });
      });
      return it;
    }
    static from(iterable) {
      need(arguments, 1, 'from', 'ReadableStream');
      if (iterable === null || iterable === undefined) throw new TypeError("Failed to execute 'from' on 'ReadableStream': The provided value is not iterable.");
      let it;
      const am = iterable[Symbol.asyncIterator];
      if (am !== undefined && am !== null) it = am.call(iterable);
      else {
        const sm = iterable[Symbol.iterator];
        if (typeof sm !== 'function') throw new TypeError("Failed to execute 'from' on 'ReadableStream': The provided value is not iterable.");
        const sit = sm.call(iterable);
        it = {
          next: () => { const r = sit.next(); return Promise.resolve(r.value).then((value) => ({ value, done: !!r.done })); },
          return: typeof sit.return === 'function' ? (v) => Promise.resolve(sit.return(v)) : undefined,
        };
      }
      return new ReadableStream({
        pull: (c) => Promise.resolve(it.next()).then((r) => {
          if (!isObj(r)) throw new TypeError('The iterator result is not an object');
          if (r.done) c.close(); else c.enqueue(r.value);
        }),
        cancel: (reason) => {
          const ret = it.return;
          if (ret === undefined || ret === null) return;
          return Promise.resolve(ret.call(it, reason)).then((r) => { if (!isObj(r)) throw new TypeError('The iterator result is not an object'); });
        },
      }, { highWaterMark: 0 });
    }
  }
  def(ReadableStream.prototype, Symbol.asyncIterator, ReadableStream.prototype.values);
  const rsFromBytes = (u8) => new ReadableStream(kToken, u8);
  const rsDisturb = (st) => { const s = rsOf(st); s.disturbed = true; s.queue = []; s.qsize = 0; rsFinishClose(s); };
  const readAll = async (st, what) => {
    const reader = new ReadableStreamDefaultReader(st);
    const chunks = [];
    let total = 0;
    for (;;) {
      const { value, done } = await reader.read();
      if (done) break;
      const u = toBytes(value);
      if (!u) { reader.cancel(); throw new TypeError(`Failed to execute '${what || 'read'}': Received non-Uint8Array chunk`); }
      chunks.push(u);
      total += u.length;
    }
    return chunks.length === 1 ? chunks[0].slice() : concat(chunks, total);
  };

  // WritableStream
  const wsRejectClosed = (s, e) => { if (s.writer) s.writer.closed.rej(e); };
  const wsFinishErroring = (s) => {
    s.state = 'errored';
    const e = s.storedError;
    for (const w of s.queue) w.reject(e);
    s.queue = [];
    s.qsize = 0;
    const pa = s.pendingAbort;
    s.pendingAbort = null;
    const after = () => {
      if (s.closeReq) { s.closeReq.reject(e); s.closeReq = null; }
      wsRejectClosed(s, e);
    };
    if (!pa) { after(); return; }
    if (pa.already) { pa.reject(e); after(); return; }
    let r;
    try { r = s.sink.abort ? s.sink.abort.call(s.sink, pa.reason) : undefined; } catch (x) { pa.reject(x); after(); return; }
    Promise.resolve(r).then(() => { pa.resolve(); after(); }, (x) => { pa.reject(x); after(); });
  };
  const wsStartErroring = (s, reason) => {
    s.state = 'erroring';
    s.storedError = reason;
    if (s.writer) { const w = s.writer; if (w.ready.settled) w.ready = settleable(); w.ready.rej(reason); }
    if (!s.inFlight && s.started) wsFinishErroring(s);
  };
  const wsDealWithError = (s, e) => {
    if (s.state === 'writable') wsStartErroring(s, e);
    else if (s.state === 'erroring' && !s.inFlight) wsFinishErroring(s);
  };
  const wsUpdateBackpressure = (s) => {
    const bp = s.hwm - s.qsize <= 0;
    if (bp === s.backpressure) return;
    s.backpressure = bp;
    const w = s.writer;
    if (!w) return;
    if (bp) { if (w.ready.settled) w.ready = settleable(); }
    else w.ready.res();
  };
  const wsAdvance = (s) => {
    if (!s.started || s.inFlight) return;
    if (s.state === 'erroring') { wsFinishErroring(s); return; }
    if (s.state !== 'writable') return;
    if (s.queue.length) {
      const w = s.queue[0];
      s.inFlight = w;
      let r;
      try { r = s.sink.write ? s.sink.write.call(s.sink, w.chunk, s.controller) : undefined; } catch (e) { r = Promise.reject(e); }
      Promise.resolve(r).then(() => {
        s.inFlight = null;
        s.queue.shift();
        s.qsize -= w.n;
        if (s.qsize < 1e-9) s.qsize = 0;
        w.resolve();
        if (s.state === 'erroring') { wsFinishErroring(s); return; }
        if (s.state === 'writable' && !s.closeReq) wsUpdateBackpressure(s);
        wsAdvance(s);
      }, (e) => {
        s.inFlight = null;
        s.queue.shift();
        w.reject(e);
        wsDealWithError(s, e);
      });
    } else if (s.closeReq) {
      const c = s.closeReq;
      s.inFlight = c;
      let r;
      try { r = s.sink.close ? s.sink.close.call(s.sink) : undefined; } catch (e) { r = Promise.reject(e); }
      Promise.resolve(r).then(() => {
        s.inFlight = null;
        s.closeReq = null;
        if (s.state === 'erroring') {
          s.storedError = undefined;
          if (s.pendingAbort) { s.pendingAbort.resolve(); s.pendingAbort = null; }
        }
        s.state = 'closed';
        c.resolve();
        if (s.writer) s.writer.closed.res();
      }, (e) => {
        s.inFlight = null;
        s.closeReq = null;
        c.reject(e);
        if (s.pendingAbort) { s.pendingAbort.reject(e); s.pendingAbort = null; }
        wsDealWithError(s, e);
      });
    }
  };
  const wsWrite = (s, chunk) => {
    let n = 1;
    if (s.size) {
      try { n = Number(s.size.call(undefined, chunk)); } catch (e) { wsDealWithError(s, e); return Promise.reject(e); }
    }
    if (s.state === 'errored' || s.state === 'erroring') return Promise.reject(s.storedError);
    if (s.closeReq || s.state === 'closed') return Promise.reject(new TypeError("Failed to execute 'write' on 'WritableStreamDefaultWriter': Cannot write to a closing writable stream"));
    return new Promise((resolve, reject) => {
      s.queue.push({ chunk, n, resolve, reject });
      s.qsize += n;
      wsUpdateBackpressure(s);
      wsAdvance(s);
    });
  };
  const wsClose = (s, m) => {
    if (s.state === 'closed' || s.state === 'errored' || s.closeReq) return Promise.reject(new TypeError(`Failed to execute 'close' on '${m}': Cannot close a writable stream that has already been requested to be closed`));
    return new Promise((resolve, reject) => {
      s.closeReq = { resolve, reject };
      if (s.writer && s.backpressure && s.state === 'writable') s.writer.ready.res();
      wsAdvance(s);
    });
  };
  const wsAbort = (s, reason) => {
    if (s.state === 'closed' || s.state === 'errored') return Promise.resolve();
    asAbort(s.signal, reason);
    if (s.state === 'closed' || s.state === 'errored') return Promise.resolve();
    if (s.pendingAbort) return s.pendingAbort.promise;
    const already = s.state === 'erroring';
    const pa = { reason: already ? undefined : reason, already, promise: null, resolve: null, reject: null };
    pa.promise = new Promise((a, b) => { pa.resolve = a; pa.reject = b; });
    s.pendingAbort = pa;
    if (!already) wsStartErroring(s, reason);
    return pa.promise;
  };
  class WritableStreamDefaultController {
    #s;
    constructor(t, s) { if (t !== kToken) throw illegal(); this.#s = s; }
    get signal() { return this.#s.signal; }
    error(e = undefined) { if (this.#s.state === 'writable') wsStartErroring(this.#s, e); }
  }
  class WritableStreamDefaultWriter {
    #s = null;
    #w = null;
    constructor(stream) {
      need(arguments, 1, null, 'WritableStreamDefaultWriter');
      const s = wsOf(stream);
      if (!s) throw new TypeError("Failed to construct 'WritableStreamDefaultWriter': parameter 1 is not of type 'WritableStream'.");
      if (s.writer) throw new TypeError("Failed to construct 'WritableStreamDefaultWriter': Cannot create writer when WritableStream is locked");
      const w = this.#w = { ready: settleable(), closed: settleable() };
      if (s.state === 'writable') { if (s.closeReq || !s.backpressure) w.ready.res(); }
      else if (s.state === 'erroring') w.ready.rej(s.storedError);
      else if (s.state === 'closed') { w.ready.res(); w.closed.res(); }
      else { w.ready.rej(s.storedError); w.closed.rej(s.storedError); }
      s.writer = w;
      this.#s = s;
    }
    get closed() { return this.#w.closed.p; }
    get ready() { return this.#w.ready.p; }
    get desiredSize() {
      const s = this.#s;
      if (!s) throw new TypeError("Failed to read the 'desiredSize' property from 'WritableStreamDefaultWriter': This writable stream writer has been released and cannot be used to monitor the stream's state");
      if (s.state === 'errored' || s.state === 'erroring') return null;
      if (s.state === 'closed') return 0;
      return s.hwm - s.qsize;
    }
    write(chunk = undefined) {
      const s = this.#s;
      if (!s) return Promise.reject(new TypeError("Failed to execute 'write' on 'WritableStreamDefaultWriter': This writable stream writer has been released and cannot be used to write"));
      return wsWrite(s, chunk);
    }
    close() {
      const s = this.#s;
      if (!s) return Promise.reject(new TypeError("Failed to execute 'close' on 'WritableStreamDefaultWriter': This writable stream writer has been released and cannot be closed"));
      return wsClose(s, 'WritableStreamDefaultWriter');
    }
    abort(reason = undefined) {
      const s = this.#s;
      if (!s) return Promise.reject(new TypeError("Failed to execute 'abort' on 'WritableStreamDefaultWriter': This writable stream writer has been released and cannot be aborted"));
      return wsAbort(s, reason);
    }
    releaseLock() {
      const s = this.#s;
      if (!s) return;
      const w = this.#w;
      const e = new TypeError("This writable stream writer has been released and cannot be used to monitor the stream's state");
      if (w.ready.settled) w.ready = settleable();
      w.ready.rej(e);
      if (w.closed.settled) w.closed = settleable();
      w.closed.rej(e);
      s.writer = null;
      this.#s = null;
    }
  }
  class WritableStream {
    #s;
    static { wsOf = (x) => (isObj(x) && #s in x) ? x.#s : null; }
    constructor(sink = undefined, strategy = undefined) {
      if (sink === null) throw new TypeError("Failed to construct 'WritableStream': The provided value is not of type 'UnderlyingSink'.");
      const snk = sink === undefined ? {} : sink;
      if (snk.type !== undefined) throw new RangeError("Failed to construct 'WritableStream': Invalid type is specified");
      const { hwm, size } = strategyOf(strategy, 1, 'WritableStream');
      const s = this.#s = {
        stream: this, state: 'writable', storedError: undefined, writer: null, queue: [], qsize: 0, inFlight: null,
        closeReq: null, started: false, hwm, size, sink: snk, signal: asNew(), backpressure: hwm <= 0, pendingAbort: null, controller: null,
      };
      s.controller = new WritableStreamDefaultController(kToken, s);
      const r = typeof snk.start === 'function' ? snk.start.call(snk, s.controller) : undefined;
      Promise.resolve(r).then(() => { s.started = true; wsAdvance(s); }, (e) => { s.started = true; wsDealWithError(s, e); });
    }
    get locked() { return this.#s.writer !== null; }
    abort(reason = undefined) {
      if (this.#s.writer) return Promise.reject(new TypeError("Failed to execute 'abort' on 'WritableStream': Cannot abort a locked stream"));
      return wsAbort(this.#s, reason);
    }
    close() {
      if (this.#s.writer) return Promise.reject(new TypeError("Failed to execute 'close' on 'WritableStream': Cannot close a locked stream"));
      return wsClose(this.#s, 'WritableStream');
    }
    getWriter() { return new WritableStreamDefaultWriter(this); }
  }

  // TransformStream
  class TransformStreamDefaultController {
    #f;
    constructor(t, f) { if (t !== kToken) throw illegal(); this.#f = f; }
    get desiredSize() { return this.#f.desired(); }
    enqueue(chunk = undefined) { this.#f.enqueue(chunk); }
    error(e = undefined) { this.#f.error(e); }
    terminate() { this.#f.terminate(); }
  }
  class TransformStream {
    #r;
    #w;
    constructor(transformer = undefined, writableStrategy = undefined, readableStrategy = undefined) {
      const t = transformer === undefined || transformer === null ? {} : transformer;
      if (t.readableType !== undefined) throw new RangeError("Failed to construct 'TransformStream': Invalid readableType is specified");
      if (t.writableType !== undefined) throw new RangeError("Failed to construct 'TransformStream': Invalid writableType is specified");
      let bp = true, bpGate = settleable();
      const setBP = (v) => {
        if (bp === v) return;
        bp = v;
        if (v) bpGate = settleable(); else bpGate.res();
      };
      let readable, writable;
      const rs = () => rsOf(readable), ws = () => wsOf(writable);
      const errorWritable = (e) => {
        const w = ws();
        if (w && w.state === 'writable') wsStartErroring(w, e);
        if (bp) setBP(false);
      };
      const tc = new TransformStreamDefaultController(kToken, {
        desired: () => rsDesired(rs()),
        enqueue: (chunk) => {
          const s = rs();
          if (s.state !== 'readable' || s.closeRequested) throw new TypeError("Failed to execute 'enqueue' on 'TransformStreamDefaultController': Readable side is not in a state that permits enqueue");
          try { rsEnqueue(s, chunk); } catch (e) { errorWritable(e); throw s.storedError !== undefined ? s.storedError : e; }
          if (s.hwm - s.qsize <= 0 && !bp) setBP(true);
        },
        error: (e) => { rsError(rs(), e); errorWritable(e); },
        terminate: () => {
          const s = rs();
          if (s.state === 'readable' && !s.closeRequested) rsClose(s);
          errorWritable(new TypeError('The transform stream has been terminated'));
        },
      });
      const rsStrat = { highWaterMark: readableStrategy && readableStrategy.highWaterMark !== undefined ? readableStrategy.highWaterMark : 0, size: readableStrategy ? readableStrategy.size : undefined };
      const wsStrat = { highWaterMark: writableStrategy && writableStrategy.highWaterMark !== undefined ? writableStrategy.highWaterMark : 1, size: writableStrategy ? writableStrategy.size : undefined };
      readable = new ReadableStream({
        pull: () => { setBP(false); },
        cancel: (reason) => {
          let r;
          try { r = t.cancel ? t.cancel.call(t, reason) : undefined; } catch (e) { errorWritable(e); return Promise.reject(e); }
          return Promise.resolve(r).then(() => errorWritable(reason));
        },
      }, rsStrat);
      const start = Promise.resolve(typeof t.start === 'function' ? t.start.call(t, tc) : undefined);
      writable = new WritableStream({
        start: () => start,
        write: (chunk) => {
          const go = () => {
            const w = ws();
            if (w.state === 'erroring') throw w.storedError;
            if (!t.transform) { tc.enqueue(chunk); return; }
            let r;
            try { r = t.transform.call(t, chunk, tc); } catch (e) { rsError(rs(), e); errorWritable(e); throw e; }
            return Promise.resolve(r).catch((e) => { rsError(rs(), e); errorWritable(e); throw e; });
          };
          return bp ? bpGate.p.then(go) : go();
        },
        close: () => {
          const s = rs();
          let r;
          try { r = t.flush ? t.flush.call(t, tc) : undefined; } catch (e) { rsError(s, e); return Promise.reject(e); }
          return Promise.resolve(r).then(() => {
            if (s.state === 'errored') throw s.storedError;
            if (s.state === 'readable' && !s.closeRequested) rsClose(s);
          }, (e) => { rsError(s, e); throw e; });
        },
        abort: (reason) => {
          let r;
          try { r = t.cancel ? t.cancel.call(t, reason) : undefined; } catch (e) { rsError(rs(), e); return Promise.reject(e); }
          return Promise.resolve(r).then(() => { rsError(rs(), reason); });
        },
      }, wsStrat);
      this.#r = readable;
      this.#w = writable;
    }
    get readable() { return this.#r; }
    get writable() { return this.#w; }
  }
  class TextEncoderStream {
    #t;
    constructor() {
      let pending = '';
      this.#t = new TransformStream({
        transform(chunk, c) {
          let s = String(chunk);
          if (pending) { s = pending + s; pending = ''; }
          if (s.length) {
            const last = s.charCodeAt(s.length - 1);
            if (last >= 0xd800 && last <= 0xdbff) { pending = s[s.length - 1]; s = s.slice(0, -1); }
          }
          if (s) c.enqueue(utf8Encode(s));
        },
        flush(c) { if (pending) c.enqueue(new Uint8Array([0xef, 0xbf, 0xbd])); },
      });
    }
    get encoding() { return 'utf-8'; }
    get readable() { return this.#t.readable; }
    get writable() { return this.#t.writable; }
  }
  class TextDecoderStream {
    #t;
    #d;
    constructor(label = 'utf-8', options = undefined) {
      const d = this.#d = new TextDecoder(label, options);
      this.#t = new TransformStream({
        transform(chunk, c) {
          if (!toBytes(chunk)) throw new TypeError("Failed to execute 'decode' on 'TextDecoder': The provided value is not of type '(ArrayBuffer or ArrayBufferView)'.");
          const s = d.decode(chunk, { stream: true });
          if (s) c.enqueue(s);
        },
        flush(c) { const s = d.decode(); if (s) c.enqueue(s); },
      });
    }
    get encoding() { return this.#d.encoding; }
    get fatal() { return this.#d.fatal; }
    get ignoreBOM() { return this.#d.ignoreBOM; }
    get readable() { return this.#t.readable; }
    get writable() { return this.#t.writable; }
  }
  // CompressionStream / DecompressionStream: the chunks are collected and
  // (de)compressed natively in Go when the writable side closes.
  const zFormat = (f, who) => {
    const s = String(f);
    if (s !== 'gzip' && s !== 'deflate' && s !== 'deflate-raw') throw new TypeError(`Failed to construct '${who}': Unsupported compression format: '${s}'`);
    return s;
  };
  const zStream = (mode, format) => {
    const parts = [];
    let n = 0;
    return new TransformStream({
      transform(chunk) {
        const b = toBytes(chunk);
        if (!b) throw new TypeError("Failed to execute 'write' on 'WritableStreamDefaultWriter': The provided value is not of type '(ArrayBuffer or ArrayBufferView)'.");
        parts.push(b.slice());
        n += b.length;
      },
      flush(c) {
        const all = new Uint8Array(n);
        let o = 0;
        for (const p of parts) { all.set(p, o); o += p.length; }
        parts.length = 0;
        const out = toBytes(H.zlib(mode, format, all));
        if (out && out.length) c.enqueue(new Uint8Array(out));
      },
    });
  };
  class CompressionStream {
    #t;
    constructor(format) {
      need(arguments, 1, null, 'CompressionStream');
      this.#t = zStream('c', zFormat(format, 'CompressionStream'));
    }
    get readable() { return this.#t.readable; }
    get writable() { return this.#t.writable; }
  }
  class DecompressionStream {
    #t;
    constructor(format) {
      need(arguments, 1, null, 'DecompressionStream');
      this.#t = zStream('d', zFormat(format, 'DecompressionStream'));
    }
    get readable() { return this.#t.readable; }
    get writable() { return this.#t.writable; }
  }
  const countSize = function size() { return 1; };
  const byteSize = function size(chunk) { return chunk.byteLength; };
  class CountQueuingStrategy {
    #h;
    constructor(init) {
      need(arguments, 1, null, 'CountQueuingStrategy');
      if (!init || init.highWaterMark === undefined) throw new TypeError("Failed to construct 'CountQueuingStrategy': Failed to read the 'highWaterMark' property from 'QueuingStrategyInit': Required member is undefined.");
      this.#h = Number(init.highWaterMark);
    }
    get highWaterMark() { return this.#h; }
    get size() { return countSize; }
  }
  class ByteLengthQueuingStrategy {
    #h;
    constructor(init) {
      need(arguments, 1, null, 'ByteLengthQueuingStrategy');
      if (!init || init.highWaterMark === undefined) throw new TypeError("Failed to construct 'ByteLengthQueuingStrategy': Failed to read the 'highWaterMark' property from 'QueuingStrategyInit': Required member is undefined.");
      this.#h = Number(init.highWaterMark);
    }
    get highWaterMark() { return this.#h; }
    get size() { return byteSize; }
  }
  for (const [n, C] of Object.entries({
    ReadableStream, ReadableStreamDefaultReader, ReadableStreamBYOBReader, ReadableStreamDefaultController, ReadableByteStreamController,
    WritableStream, WritableStreamDefaultWriter, WritableStreamDefaultController, TransformStream, TransformStreamDefaultController,
    TextEncoderStream, TextDecoderStream, CountQueuingStrategy, ByteLengthQueuingStrategy,
    CompressionStream, DecompressionStream,
  })) expose(n, C);

  // ---- Headers ----
  const TOKEN = new Uint8Array(128);
  for (const ch of "!#$%&'*+-.^_`|~") TOKEN[ch.charCodeAt(0)] = 1;
  for (let c = 0x30; c <= 0x39; c++) TOKEN[c] = 1;
  for (let c = 0x41; c <= 0x5a; c++) { TOKEN[c] = 1; TOKEN[c + 32] = 1; }
  const isToken = (s) => {
    if (!s.length) return false;
    for (let i = 0; i < s.length; i++) { const c = s.charCodeAt(i); if (c >= 128 || !TOKEN[c]) return false; }
    return true;
  };
  const isHWS = (c) => c === 0x20 || c === 9 || c === 10 || c === 13;
  const normHV = (v) => {
    let s = 0, e = v.length;
    while (s < e && isHWS(v.charCodeAt(s))) s++;
    while (e > s && isHWS(v.charCodeAt(e - 1))) e--;
    return s || e !== v.length ? v.slice(s, e) : v;
  };
  const validHV = (v) => {
    for (let i = 0; i < v.length; i++) { const c = v.charCodeAt(i); if (c === 0 || c === 10 || c === 13 || c > 255) return false; }
    return true;
  };
  const hWhere = (m) => m ? `Failed to execute '${m}' on 'Headers'` : "Failed to construct 'Headers'";
  const H_INIT_ERR = "Failed to construct 'Headers': The provided value is not of type '(record<ByteString, ByteString> or sequence<sequence<ByteString>>)'.";
  let hAppendRaw, hGuard, hGetGuard, hPairs;
  class Headers {
    #m = new Map(); // lowercased name → values
    #guard = '';
    #sorted = null;
    static {
      hAppendRaw = (h, n, v) => { const l = h.#m.get(n); if (l) l.push(v); else h.#m.set(n, [v]); h.#sorted = null; };
      hGuard = (h, g) => { h.#guard = g; };
      hGetGuard = (h) => h.#guard;
      hPairs = (h) => { const out = []; for (const [n, vs] of h.#m) for (const v of vs) out.push([n, v]); return out; };
    }
    constructor(init = undefined) {
      if (init === undefined) return;
      if (isObj(init) && #m in init) { for (const [n, vs] of init.#m) this.#m.set(n, vs.slice()); return; }
      if (!isObj(init)) throw new TypeError(H_INIT_ERR);
      const it = init[Symbol.iterator];
      if (it !== undefined && it !== null) {
        for (const pair of init) {
          if (!isObj(pair) || typeof pair[Symbol.iterator] !== 'function') throw new TypeError(H_INIT_ERR);
          const a = Array.from(pair);
          if (a.length !== 2) throw new TypeError("Failed to construct 'Headers': Invalid value");
          this.#add(a[0], a[1], null);
        }
      } else for (const k of Object.keys(init)) this.#add(k, init[k], null);
    }
    #name(name, m) {
      name = String(name);
      if (!isToken(name)) throw new TypeError(`${hWhere(m)}: Invalid name`);
      return name.toLowerCase();
    }
    #value(value, m) {
      value = normHV(String(value));
      if (!validHV(value)) throw new TypeError(`${hWhere(m)}: Invalid value`);
      return value;
    }
    #mutable(m) { if (this.#guard === 'immutable') throw new TypeError(`${hWhere(m)}: Headers are immutable`); }
    #add(name, value, m) {
      const n = this.#name(name, m), v = this.#value(value, m);
      this.#mutable(m);
      const l = this.#m.get(n);
      if (l) l.push(v); else this.#m.set(n, [v]);
      this.#sorted = null;
    }
    #list() {
      if (this.#sorted) return this.#sorted;
      const out = [];
      for (const n of [...this.#m.keys()].sort()) {
        const vs = this.#m.get(n);
        if (n === 'set-cookie') for (const v of vs) out.push([n, v]);
        else out.push([n, vs.join(', ')]);
      }
      return (this.#sorted = out);
    }
    append(name, value) { need(arguments, 2, 'append', 'Headers'); this.#add(name, value, 'append'); }
    delete(name) {
      need(arguments, 1, 'delete', 'Headers');
      const n = this.#name(name, 'delete');
      this.#mutable('delete');
      if (this.#m.delete(n)) this.#sorted = null;
    }
    get(name) {
      need(arguments, 1, 'get', 'Headers');
      const vs = this.#m.get(this.#name(name, 'get'));
      return vs ? vs.join(', ') : null;
    }
    getSetCookie() { const vs = this.#m.get('set-cookie'); return vs ? vs.slice() : []; }
    has(name) { need(arguments, 1, 'has', 'Headers'); return this.#m.has(this.#name(name, 'has')); }
    set(name, value) {
      need(arguments, 2, 'set', 'Headers');
      const n = this.#name(name, 'set'), v = this.#value(value, 'set');
      this.#mutable('set');
      this.#m.set(n, [v]);
      this.#sorted = null;
    }
    forEach(cb, thisArg = undefined) {
      if (typeof cb !== 'function') throw new TypeError("Failed to execute 'forEach' on 'Headers': The callback provided as parameter 1 is not a function.");
      for (let i = 0; ; i++) { const l = this.#list(); if (i >= l.length) return; cb.call(thisArg, l[i][1], l[i][0], this); }
    }
    *entries() { for (let i = 0; ; i++) { const l = this.#list(); if (i >= l.length) return; yield [l[i][0], l[i][1]]; } }
    *keys() { for (let i = 0; ; i++) { const l = this.#list(); if (i >= l.length) return; yield l[i][0]; } }
    *values() { for (let i = 0; ; i++) { const l = this.#list(); if (i >= l.length) return; yield l[i][1]; } }
  }
  def(Headers.prototype, Symbol.iterator, Headers.prototype.entries);
  expose('Headers', Headers);

  // ---- bodies ----
  const NULL_BODY = (s) => s === 101 || s === 103 || s === 204 || s === 205 || s === 304;
  const extractBody = (x, W) => {
    if (typeof x === 'string') return { bytes: utf8Encode(x), type: 'text/plain;charset=UTF-8' };
    if (x instanceof URLSearchParams) return { bytes: utf8Encode(x.toString()), type: 'application/x-www-form-urlencoded;charset=UTF-8' };
    if (x instanceof Blob) return { bytes: blobBytes(x), type: x.type || null };
    if (x instanceof FormData) return multipart(x);
    const u = toBytes(x);
    if (u) return { bytes: u.slice(), type: null };
    const s = rsOf(x);
    if (s) {
      if (s.disturbed || s.reader) throw new TypeError(`${W}: Response body object should not be disturbed or locked`);
      return { stream: x, type: null };
    }
    return { bytes: utf8Encode(usv(x)), type: 'text/plain;charset=UTF-8' };
  };
  const cloneBody = (b) => {
    if (!b) return null;
    if (b.bytes) return { bytes: b.bytes, stream: null, used: false };
    const [s1, s2] = b.stream.tee();
    b.stream = s1;
    return { bytes: null, stream: s2, used: false };
  };
  const bodyUsed = (b) => !!b && (b.used || (!!b.stream && rsOf(b.stream).disturbed));
  const mixBody = (C, iface, st, hd) => {
    Object.defineProperty(C.prototype, 'body', {
      get() {
        const b = st(this);
        if (!b) return null;
        if (!b.stream) b.stream = rsFromBytes(b.bytes.slice());
        return b.stream;
      },
      enumerable: true, configurable: true,
    });
    Object.defineProperty(C.prototype, 'bodyUsed', { get() { return bodyUsed(st(this)); }, enumerable: true, configurable: true });
    const consume = (self, m) => {
      let b;
      try { b = st(self); } catch (e) { return Promise.reject(e); }
      if (!b) return Promise.resolve(EMPTY);
      if (bodyUsed(b)) return Promise.reject(new TypeError(`Failed to execute '${m}' on '${iface}': body stream already read`));
      if (b.stream && rsOf(b.stream).reader) return Promise.reject(new TypeError(`Failed to execute '${m}' on '${iface}': body stream is locked`));
      b.used = true;
      if (b.bytes) { if (b.stream) rsDisturb(b.stream); return Promise.resolve(b.bytes); }
      return readAll(b.stream, `${m}' on '${iface}`);
    };
    const ctype = (self) => hd(self).get('content-type') || '';
    def(C.prototype, 'arrayBuffer', function arrayBuffer() { return consume(this, 'arrayBuffer').then((u) => u.slice().buffer); });
    def(C.prototype, 'bytes', function bytes() { return consume(this, 'bytes').then((u) => u.slice()); });
    def(C.prototype, 'blob', function blob() { const ct = ctype(this); return consume(this, 'blob').then((u) => blobMake(u, normType(ct))); });
    def(C.prototype, 'text', function text() { return consume(this, 'text').then(utf8Text); });
    def(C.prototype, 'json', function json() { return consume(this, 'json').then((u) => JSON.parse(utf8Text(u))); });
    def(C.prototype, 'formData', function formData() {
      const ct = ctype(this);
      return consume(this, 'formData').then((u) => {
        const essence = ct.split(';')[0].trim().toLowerCase();
        if (essence === 'multipart/form-data') return parseMultipart(u, ct, iface);
        if (essence === 'application/x-www-form-urlencoded') {
          const fd = new FormData();
          for (const [k, v] of parseForm(utf8Text(u))) fd.append(k, v);
          return fd;
        }
        throw new TypeError(`Failed to execute 'formData' on '${iface}': Invalid MIME type`);
      });
    });
  };

  // ---- Request ----
  const NORM_METHODS = new Set(['DELETE', 'GET', 'HEAD', 'OPTIONS', 'POST', 'PUT']);
  const ENUMS = {
    mode: ['RequestMode', 'same-origin', 'no-cors', 'cors', 'navigate'],
    credentials: ['RequestCredentials', 'omit', 'same-origin', 'include'],
    cache: ['RequestCache', 'default', 'no-store', 'reload', 'no-cache', 'force-cache', 'only-if-cached'],
    redirect: ['RequestRedirect', 'follow', 'error', 'manual'],
    referrerPolicy: ['ReferrerPolicy', '', 'no-referrer', 'no-referrer-when-downgrade', 'same-origin', 'origin', 'strict-origin', 'origin-when-cross-origin', 'strict-origin-when-cross-origin', 'unsafe-url'],
    priority: ['RequestPriority', 'high', 'low', 'auto'],
    duplex: ['RequestDuplex', 'half'],
  };
  const noFrag = (u) => { const i = u.indexOf('#'); return i < 0 ? u : u.slice(0, i); };
  let reqParts;
  class Request {
    #url = '';
    #method = 'GET';
    #h = null;
    #b = null;
    #signal = null;
    #o = null;
    static { reqParts = (r) => ({ url: r.#url, method: r.#method, headers: r.#h, body: r.#b, signal: r.#signal, o: r.#o }); }
    constructor(input, init = undefined) {
      if (input === kToken) return;
      need(arguments, 1, null, 'Request');
      const W = "Failed to construct 'Request'";
      if (init === null || init === undefined) init = {};
      const src = isObj(input) && #url in input ? input : null;
      let url, method, o, signal = null;
      if (src) { url = src.#url; method = src.#method; o = { ...src.#o }; signal = src.#signal; }
      else {
        const u = parseRelative(input);
        if (!u) throw new TypeError(`${W}: Failed to parse URL from ${input}`);
        if (u.username !== '' || u.password !== '') throw new TypeError(`${W}: Request cannot be constructed from a URL that includes credentials: ${input}`);
        url = serialize(u);
        method = 'GET';
        o = { mode: 'cors', credentials: 'same-origin', cache: 'default', redirect: 'follow', referrer: 'about:client', referrerPolicy: '', integrity: '', keepalive: false, priority: 'auto', duplex: 'half' };
      }
      for (const k in ENUMS) {
        if (init[k] === undefined) continue;
        const v = String(init[k]), e = ENUMS[k];
        if (e.indexOf(v, 1) < 0) throw new TypeError(`${W}: Failed to read the '${k}' property from 'RequestInit': The provided value '${v}' is not a valid enum value of type ${e[0]}.`);
        o[k] = v;
      }
      if (init.mode === 'navigate') throw new TypeError(`${W}: Cannot construct a Request with a RequestInit whose mode member is set as 'navigate'.`);
      if (o.mode === 'navigate') o.mode = 'same-origin';
      if (init.integrity !== undefined) o.integrity = String(init.integrity);
      if (init.keepalive !== undefined) o.keepalive = !!init.keepalive;
      if (init.referrer !== undefined) {
        const r = String(init.referrer);
        if (r === '') o.referrer = '';
        else {
          const ru = parseRelative(r);
          if (!ru) throw new TypeError(`${W}: Failed to parse referrer URL ${r}`);
          const rs = serialize(ru);
          o.referrer = rs === 'about:client' || originOf(ru) !== originOf(loc) ? 'about:client' : rs;
        }
      }
      if (init.method !== undefined) {
        const m = String(init.method);
        if (!isToken(m)) throw new TypeError(`${W}: '${m}' is not a valid HTTP method.`);
        const up = m.toUpperCase();
        if (up === 'CONNECT' || up === 'TRACE' || up === 'TRACK') throw new TypeError(`${W}: '${m}' HTTP method is unsupported.`);
        method = NORM_METHODS.has(up) ? up : m;
      }
      if (init.signal !== undefined) {
        if (init.signal !== null && !isSignal(init.signal)) throw new TypeError(`${W}: Failed to read the 'signal' property from 'RequestInit': Failed to convert value to 'AbortSignal'.`);
        signal = init.signal;
      }
      const h = new Headers(init.headers !== undefined ? init.headers : src ? src.#h : undefined);
      const initBody = init.body !== undefined && init.body !== null;
      if ((initBody || (src && src.#b)) && (method === 'GET' || method === 'HEAD')) throw new TypeError(`${W}: Request with GET/HEAD method cannot have body.`);
      let body = null;
      if (initBody) {
        const ex = extractBody(init.body, W);
        if (ex.type && !h.has('content-type')) h.append('content-type', ex.type);
        if (ex.stream) {
          if (o.keepalive) throw new TypeError(`${W}: keepalive cannot be set for a request with a streaming body`);
          if (init.duplex === undefined) throw new TypeError(`${W}: The \`duplex\` member must be specified for a request with a streaming body`);
        }
        body = { bytes: ex.bytes || null, stream: ex.stream || null, used: false };
      } else if (src && src.#b) {
        const sb = src.#b;
        if (bodyUsed(sb) || (sb.stream && rsOf(sb.stream).reader)) throw new TypeError(`${W}: Cannot construct a Request with a Request object that has already been used.`);
        body = { bytes: sb.bytes, stream: sb.stream, used: false };
        sb.used = true;
      }
      this.#url = url;
      this.#method = method;
      this.#h = h;
      this.#b = body;
      this.#o = o;
      this.#signal = signal ? asFollow([signal]) : asNew();
    }
    get method() { return this.#method; }
    get url() { return this.#url; }
    get headers() { return this.#h; }
    get destination() { return ''; }
    get referrer() { return this.#o.referrer; }
    get referrerPolicy() { return this.#o.referrerPolicy; }
    get mode() { return this.#o.mode; }
    get credentials() { return this.#o.credentials; }
    get cache() { return this.#o.cache; }
    get redirect() { return this.#o.redirect; }
    get integrity() { return this.#o.integrity; }
    get keepalive() { return this.#o.keepalive; }
    get isReloadNavigation() { return false; }
    get isHistoryNavigation() { return false; }
    get signal() { return this.#signal; }
    get duplex() { return this.#o.duplex; }
    clone() {
      if (bodyUsed(this.#b)) throw new TypeError("Failed to execute 'clone' on 'Request': Request body is already used");
      const r = new Request(kToken);
      r.#url = this.#url;
      r.#method = this.#method;
      r.#h = new Headers(this.#h);
      r.#b = cloneBody(this.#b);
      r.#o = { ...this.#o };
      r.#signal = asFollow([this.#signal]);
      return r;
    }
  }
  mixBody(Request, 'Request', (r) => reqParts(r).body, (r) => reqParts(r).headers);
  expose('Request', Request);

  // ---- Response ----
  let respMake, respBody;
  class Response {
    #status = 200;
    #statusText = '';
    #h = null;
    #b = null;
    #type = 'default';
    #url = '';
    #redirected = false;
    static {
      respMake = (o) => {
        const r = new Response(kToken);
        r.#status = o.status; r.#statusText = o.statusText || ''; r.#h = o.headers; r.#b = o.body || null;
        r.#type = o.type || 'default'; r.#url = o.url || ''; r.#redirected = !!o.redirected;
        return r;
      };
      respBody = (r) => r.#b;
    }
    constructor(body = null, init = undefined) {
      if (body === kToken) return;
      const W = "Failed to construct 'Response'";
      if (init === null || init === undefined) init = {};
      let status = 200;
      if (init.status !== undefined) status = (Math.trunc(Number(init.status)) || 0) & 0xffff;
      if (status < 200 || status > 599) throw new RangeError(`${W}: The status provided (${status}) is outside the range [200, 599].`);
      const st = init.statusText !== undefined ? String(init.statusText) : '';
      if (!validHV(st)) throw new TypeError(`${W}: Invalid statusText`);
      const h = new Headers(init.headers);
      this.#status = status;
      this.#statusText = st;
      this.#h = h;
      if (body !== null && body !== undefined) {
        if (NULL_BODY(status)) throw new TypeError(`${W}: Response with null body status cannot have body`);
        const ex = extractBody(body, W);
        if (ex.type && !h.has('content-type')) h.append('content-type', ex.type);
        this.#b = { bytes: ex.bytes || null, stream: ex.stream || null, used: false };
      }
    }
    static error() {
      const h = new Headers();
      hGuard(h, 'immutable');
      return respMake({ status: 0, headers: h, type: 'error' });
    }
    static redirect(url, status = 302) {
      need(arguments, 1, 'redirect', 'Response');
      const u = parseRelative(url);
      if (!u) throw new TypeError(`Failed to execute 'redirect' on 'Response': Failed to parse URL from ${url}`);
      status = Number(status);
      if (status !== 301 && status !== 302 && status !== 303 && status !== 307 && status !== 308) throw new RangeError("Failed to execute 'redirect' on 'Response': Invalid status code");
      const h = new Headers();
      hAppendRaw(h, 'location', serialize(u));
      hGuard(h, 'immutable');
      return respMake({ status, headers: h });
    }
    static json(data, init = undefined) {
      need(arguments, 1, 'json', 'Response');
      const s = JSON.stringify(data);
      if (s === undefined) throw new TypeError("Failed to execute 'json' on 'Response': The data is not JSON serializable");
      const r = new Response(null, init);
      if (NULL_BODY(r.#status)) throw new TypeError("Failed to execute 'json' on 'Response': Response with null body status cannot have body");
      r.#b = { bytes: utf8Encode(s), stream: null, used: false };
      if (!r.#h.has('content-type')) r.#h.append('content-type', 'application/json');
      return r;
    }
    get type() { return this.#type; }
    get url() { return this.#url; }
    get redirected() { return this.#redirected; }
    get status() { return this.#status; }
    get ok() { return this.#status >= 200 && this.#status <= 299; }
    get statusText() { return this.#statusText; }
    get headers() { return this.#h; }
    clone() {
      if (bodyUsed(this.#b)) throw new TypeError("Failed to execute 'clone' on 'Response': Response body is already used");
      const h = new Headers(this.#h);
      hGuard(h, hGetGuard(this.#h));
      return respMake({ status: this.#status, statusText: this.#statusText, headers: h, body: cloneBody(this.#b), type: this.#type, url: this.#url, redirected: this.#redirected });
    }
  }
  mixBody(Response, 'Response', (r) => respBody(r), (r) => r.headers);
  expose('Response', Response);

  // ---- data: URLs ----
  const parseMime = (s) => {
    const parts = s.split(';');
    const ess = parts[0].replace(/^[\t\n\f\r ]+|[\t\n\f\r ]+$/g, '').toLowerCase();
    const sl = ess.indexOf('/');
    if (sl <= 0 || !isToken(ess.slice(0, sl)) || !isToken(ess.slice(sl + 1))) return null;
    let out = ess;
    for (let i = 1; i < parts.length; i++) {
      const p = parts[i].replace(/^[\t\n\f\r ]+/, '');
      const eq = p.indexOf('=');
      if (eq <= 0) continue;
      const n = p.slice(0, eq).toLowerCase(), v = p.slice(eq + 1);
      if (!isToken(n) || !v) continue;
      out += ';' + n + '=' + v;
    }
    return out;
  };
  const dataURL = (href) => {
    let input = noFrag(href).slice(5);
    const comma = input.indexOf(',');
    if (comma < 0) return null;
    let mime = input.slice(0, comma).replace(/^[\t\n\f\r ]+|[\t\n\f\r ]+$/g, '');
    let body = pctDecodeBytes(input.slice(comma + 1));
    const m = /;[\x20]*base64$/i.exec(mime);
    if (m) {
      body = b64decode(latin1(body));
      if (!body) return null;
      mime = mime.slice(0, m.index);
    }
    if (mime.startsWith(';')) mime = 'text/plain' + mime;
    return { mime: parseMime(mime) || 'text/plain;charset=US-ASCII', body };
  };

  // ---- fetch ----
  let fetchSeq = 0;
  const pendingFetches = new Map();
  const fetchFailed = (cause) => cause === undefined ? new TypeError('Failed to fetch') : new TypeError('Failed to fetch', { cause });
  const localResponse = (url, type, bytes, head) => {
    const h = new Headers();
    hAppendRaw(h, 'content-type', type);
    hAppendRaw(h, 'content-length', String(bytes.length));
    hGuard(h, 'immutable');
    return respMake({ status: 200, statusText: 'OK', headers: h, body: head ? null : { bytes, stream: null, used: false }, type: 'basic', url });
  };
  const fetch = function fetch(input, init = undefined) {
    let req;
    try { req = new Request(input, init); } catch (e) { return Promise.reject(e); }
    const r = reqParts(req);
    const signal = r.signal;
    if (signal.aborted) return Promise.reject(signal.reason);
    return new Promise((resolve, reject) => {
      const url = noFrag(r.url);
      const scheme = url.slice(0, url.indexOf(':'));
      if (scheme === 'data') {
        const d = dataURL(url);
        if (!d) reject(fetchFailed());
        else resolve(localResponse(url, d.mime, d.body, r.method === 'HEAD'));
        return;
      }
      if (scheme === 'blob') {
        const blob = blobURLs.get(url);
        if (!blob || r.method !== 'GET') reject(fetchFailed());
        else resolve(localResponse(url, blob.type, blobBytes(blob), false));
        return;
      }
      if (scheme !== 'http' && scheme !== 'https') { reject(fetchFailed()); return; }
      const id = ++fetchSeq;
      const onAbort = () => {
        if (!pendingFetches.delete(id)) return;
        try { H.fetchAbort(id); } catch {}
        reject(signal.reason);
      };
      const start = (bytes) => {
        if (signal.aborted) { reject(signal.reason); return; }
        pendingFetches.set(id, { resolve, reject, signal, onAbort, redirect: r.o.redirect, method: r.method });
        asOn(signal, onAbort);
        try {
          H.fetch(id, JSON.stringify({ method: r.method, url, headers: hPairs(r.headers), redirect: r.o.redirect }), bytes);
        } catch (e) {
          pendingFetches.delete(id);
          asOff(signal, onAbort);
          reject(fetchFailed(e));
        }
      };
      const b = r.body;
      if (!b) start(null);
      else {
        b.used = true;
        if (b.bytes) start(b.bytes);
        else readAll(b.stream, 'fetch').then(start, (e) => reject(fetchFailed(e)));
      }
    });
  };
  def(G, 'fetch', fetch);
  jr.fetchDone = (id, meta, body, errName, errMsg) => {
    const p = pendingFetches.get(id);
    if (!p) return;
    pendingFetches.delete(id);
    asOff(p.signal, p.onAbort);
    if (meta === null || meta === undefined) {
      if (p.signal.aborted) p.reject(p.signal.reason);
      else if (errName === 'AbortError') p.reject(new DOMException('The user aborted a request.', 'AbortError'));
      else p.reject(fetchFailed(errMsg ? new Error(String(errMsg)) : undefined));
      return;
    }
    let m;
    try { m = JSON.parse(meta); } catch (e) { p.reject(fetchFailed(e)); return; }
    if (p.redirect === 'error' && m.redirected) { p.reject(fetchFailed()); return; }
    const h = new Headers();
    for (const kv of m.headers || []) hAppendRaw(h, String(kv[0]).toLowerCase(), String(kv[1]));
    hGuard(h, 'immutable');
    const status = m.status | 0;
    const ru = parseURL(String(m.url || ''), null);
    p.resolve(respMake({
      status, statusText: m.statusText || '', headers: h,
      body: NULL_BODY(status) || p.method === 'HEAD' ? null : { bytes: (body && toBytes(body)) || EMPTY, stream: null, used: false },
      type: ru && originOf(ru) !== originOf(loc) ? 'cors' : 'basic',
      url: ru ? serialize(ru, true) : String(m.url || ''),
      redirected: !!m.redirected,
    }));
  };

  // ---- XMLHttpRequest (over fetch) ----
  class XMLHttpRequestEventTarget extends EventTarget {
    constructor(t) { if (t !== kToken) throw illegal(); super(); }
  }
  handlers(XMLHttpRequestEventTarget.prototype, ['loadstart', 'progress', 'abort', 'error', 'load', 'timeout', 'loadend']);
  class XMLHttpRequestUpload extends XMLHttpRequestEventTarget {}
  const XHR_TYPES = ['', 'arraybuffer', 'blob', 'document', 'json', 'text'];
  const xhrErr = (m, msg, name) => new DOMException(`Failed to execute '${m}' on 'XMLHttpRequest': ${msg}`, name);
  class XMLHttpRequest extends XMLHttpRequestEventTarget {
    #state = 0;
    #method = 'GET';
    #url = '';
    #headers = []; // [name, value], combined case-insensitively
    #send = false;
    #resp = null;
    #bytes = null;
    #type = '';
    #timeout = 0;
    #cred = false;
    #ctl = null;
    #gen = 0;
    #upload;
    #mime = null;
    #text = null;
    #cache = undefined;
    constructor() {
      super(kToken);
      this.#upload = new XMLHttpRequestUpload(kToken);
    }
    get readyState() { return this.#state; }
    get upload() { return this.#upload; }
    get timeout() { return this.#timeout; }
    set timeout(v) { this.#timeout = Math.max(0, Math.trunc(Number(v)) || 0); }
    get withCredentials() { return this.#cred; }
    set withCredentials(v) {
      if ((this.#state !== 0 && this.#state !== 1) || this.#send) throw new DOMException("Failed to set the 'withCredentials' property on 'XMLHttpRequest': The value may only be set if the object's state is UNSENT or OPENED.", 'InvalidStateError');
      this.#cred = !!v;
    }
    get responseType() { return this.#type; }
    set responseType(v) {
      v = String(v);
      if (!XHR_TYPES.includes(v)) return;
      if (this.#state === 3 || this.#state === 4) throw new DOMException("Failed to set the 'responseType' property on 'XMLHttpRequest': The response type cannot be set if the object's state is LOADING or DONE.", 'InvalidStateError');
      this.#type = v;
    }
    get status() { return this.#resp && this.#state >= 2 ? this.#resp.status : 0; }
    get statusText() { return this.#resp && this.#state >= 2 ? this.#resp.statusText : ''; }
    get responseURL() { return this.#resp ? this.#resp.url : ''; }
    get responseXML() {
      if (this.#type !== '' && this.#type !== 'document') throw new DOMException(`Failed to read the 'responseXML' property from 'XMLHttpRequest': The value is only accessible if the object's 'responseType' is '' or 'document' (was '${this.#type}').`, 'InvalidStateError');
      return null;
    }
    get responseText() {
      if (this.#type !== '' && this.#type !== 'text') throw new DOMException(`Failed to read the 'responseText' property from 'XMLHttpRequest': The value is only accessible if the object's 'responseType' is '' or 'text' (was '${this.#type}').`, 'InvalidStateError');
      if (this.#state < 3 || !this.#bytes) return '';
      if (this.#text === null) {
        const cs = /;\s*charset="?([^";\s]+)/i.exec(this.#mime || (this.#resp && this.#resp.headers.get('content-type')) || '');
        let d;
        try { d = new TextDecoder(cs ? cs[1] : 'utf-8'); } catch { d = new TextDecoder(); }
        this.#text = d.decode(this.#bytes);
      }
      return this.#text;
    }
    get response() {
      const t = this.#type;
      if (t === '' || t === 'text') return this.responseText;
      if (this.#state !== 4 || !this.#bytes) return null;
      if (this.#cache !== undefined) return this.#cache;
      let v = null;
      if (t === 'arraybuffer') v = this.#bytes.slice().buffer;
      else if (t === 'blob') v = blobMake(this.#bytes, normType(this.#mime || this.#resp.headers.get('content-type') || ''));
      else if (t === 'json') { try { v = JSON.parse(utf8Text(this.#bytes)); } catch { v = null; } }
      return (this.#cache = v);
    }
    #fire(type, loaded = 0, total = 0, target = this) {
      target.dispatchEvent(type === 'readystatechange' ? new Event(type) : new ProgressEvent(type, { lengthComputable: total > 0, loaded, total }));
    }
    open(method, url, async = true, username = undefined, password = undefined) {
      need(arguments, 2, 'open', 'XMLHttpRequest');
      const m = String(method);
      if (!isToken(m)) throw xhrErr('open', `'${m}' is not a valid HTTP method.`, 'SyntaxError');
      const up = m.toUpperCase();
      if (up === 'CONNECT' || up === 'TRACE' || up === 'TRACK') throw xhrErr('open', `'${m}' HTTP method is unsupported.`, 'SecurityError');
      const u = parseRelative(url);
      if (!u) throw xhrErr('open', 'Invalid URL', 'SyntaxError');
      if (arguments.length > 2 && !async) throw xhrErr('open', 'Synchronous requests are not supported.', 'InvalidAccessError');
      if (u.host !== null && u.host !== '' && u.scheme !== 'file') {
        if (username !== undefined && username !== null) u.username = pctEncode(String(username), USERINFO);
        if (password !== undefined && password !== null) u.password = pctEncode(String(password), USERINFO);
      }
      if (this.#ctl) { this.#ctl.abort(); this.#ctl = null; }
      this.#gen++;
      this.#method = NORM_METHODS.has(up) ? up : m;
      this.#url = serialize(u);
      this.#headers = [];
      this.#send = false;
      this.#resp = null;
      this.#bytes = null;
      this.#text = null;
      this.#cache = undefined;
      if (this.#state !== 1) { this.#state = 1; this.#fire('readystatechange'); }
    }
    setRequestHeader(name, value) {
      need(arguments, 2, 'setRequestHeader', 'XMLHttpRequest');
      if (this.#state !== 1 || this.#send) throw xhrErr('setRequestHeader', "The object's state must be OPENED.", 'InvalidStateError');
      name = String(name);
      value = normHV(String(value));
      if (!isToken(name)) throw xhrErr('setRequestHeader', `'${name}' is not a valid HTTP header field name.`, 'SyntaxError');
      if (!validHV(value)) throw xhrErr('setRequestHeader', `'${value}' is not a valid HTTP header field value.`, 'SyntaxError');
      const n = name.toLowerCase();
      const e = this.#headers.find((x) => x[0].toLowerCase() === n);
      if (e) e[1] += ', ' + value;
      else this.#headers.push([name, value]);
    }
    send(body = null) {
      if (this.#state !== 1 || this.#send) throw xhrErr('send', "The object's state must be OPENED.", 'InvalidStateError');
      if (this.#method === 'GET' || this.#method === 'HEAD') body = null;
      if (body !== null && body !== undefined && typeof body === 'object' && body.nodeType === 9) body = null; // Document bodies
      const hasBody = body !== null && body !== undefined;
      this.#send = true;
      const gen = this.#gen;
      const ok = () => gen === this.#gen;
      this.#fire('loadstart');
      if (hasBody && ok()) this.#fire('loadstart', 0, 0, this.#upload);
      if (!ok() || this.#state !== 1 || !this.#send) return;
      const ctl = this.#ctl = new AbortController();
      let timedOut = false, timer = 0;
      if (this.#timeout > 0) timer = setTimeout(() => { timedOut = true; ctl.abort(); }, this.#timeout);
      fetch(this.#url, {
        method: this.#method, headers: this.#headers, body: hasBody ? body : undefined,
        signal: ctl.signal, credentials: this.#cred ? 'include' : 'same-origin',
      }).then((res) => {
        clearTimeout(timer);
        if (!ok()) return;
        const b = respBody(res);
        const bytes = b ? b.bytes : EMPTY;
        this.#ctl = null;
        this.#resp = res;
        this.#bytes = bytes;
        if (hasBody) {
          for (const t of ['progress', 'load', 'loadend']) { if (!ok()) return; this.#fire(t, 0, 0, this.#upload); }
        }
        const n = bytes.length;
        this.#state = 2; this.#fire('readystatechange'); if (!ok()) return;
        this.#state = 3; this.#fire('readystatechange'); if (!ok()) return;
        this.#fire('progress', n, n); if (!ok()) return;
        this.#state = 4; this.#send = false;
        this.#fire('readystatechange'); if (!ok()) return;
        this.#fire('load', n, n); if (!ok()) return;
        this.#fire('loadend', n, n);
      }, () => {
        clearTimeout(timer);
        if (!ok()) return;
        this.#ctl = null;
        this.#netError(timedOut ? 'timeout' : 'error', hasBody);
      });
    }
    #netError(type, hasBody) {
      const gen = this.#gen;
      this.#state = 4;
      this.#send = false;
      this.#resp = null;
      this.#bytes = null;
      this.#fire('readystatechange');
      if (hasBody) for (const t of [type, 'loadend']) { if (gen !== this.#gen) return; this.#fire(t, 0, 0, this.#upload); }
      if (gen !== this.#gen) return;
      this.#fire(type);
      if (gen !== this.#gen) return;
      this.#fire('loadend');
    }
    abort() {
      if (this.#ctl) { this.#ctl.abort(); this.#ctl = null; }
      const gen = ++this.#gen;
      if ((this.#state === 1 && this.#send) || this.#state === 2 || this.#state === 3) {
        this.#state = 4;
        this.#send = false;
        this.#resp = null;
        this.#bytes = null;
        this.#fire('readystatechange');
        if (gen === this.#gen) this.#fire('abort');
        if (gen === this.#gen) this.#fire('loadend');
      }
      if (this.#state === 4) { this.#state = 0; this.#resp = null; this.#bytes = null; }
    }
    getResponseHeader(name) {
      need(arguments, 1, 'getResponseHeader', 'XMLHttpRequest');
      if (!this.#resp || this.#state < 2) return null;
      const n = String(name).toLowerCase();
      if (n === 'set-cookie' || n === 'set-cookie2' || !isToken(n)) return null;
      return this.#resp.headers.get(n);
    }
    getAllResponseHeaders() {
      if (!this.#resp || this.#state < 2) return '';
      let out = '';
      for (const [k, v] of this.#resp.headers) if (k !== 'set-cookie' && k !== 'set-cookie2') out += k + ': ' + v + '\r\n';
      return out;
    }
    overrideMimeType(mime) {
      need(arguments, 1, 'overrideMimeType', 'XMLHttpRequest');
      if (this.#state === 3 || this.#state === 4) throw xhrErr('overrideMimeType', "MimeType cannot be overridden when the state is LOADING or DONE.", 'InvalidStateError');
      this.#mime = parseMime(String(mime)) || 'application/octet-stream';
    }
  }
  handlers(XMLHttpRequest.prototype, ['readystatechange']);
  constants(XMLHttpRequest, { UNSENT: 0, OPENED: 1, HEADERS_RECEIVED: 2, LOADING: 3, DONE: 4 });
  expose('XMLHttpRequestEventTarget', XMLHttpRequestEventTarget);
  expose('XMLHttpRequestUpload', XMLHttpRequestUpload);
  expose('XMLHttpRequest', XMLHttpRequest);

  // ---- crypto ----
  let pool = null, poolPos = 0;
  const randBytes = (n) => {
    if (n > 256) return H.random(n);
    if (!pool || poolPos + n > pool.length) { pool = H.random(4096); poolPos = 0; }
    const out = pool.subarray(poolPos, poolPos + n);
    poolPos += n;
    return out;
  };
  const INT_ARRAYS = ['Int8Array', 'Uint8Array', 'Uint8ClampedArray', 'Int16Array', 'Uint16Array', 'Int32Array', 'Uint32Array', 'BigInt64Array', 'BigUint64Array'];
  const DIGESTS = { 'SHA-1': 'SHA-1', 'SHA-256': 'SHA-256', 'SHA-384': 'SHA-384', 'SHA-512': 'SHA-512' };
  class CryptoKey { constructor() { throw illegal(); } }
  class SubtleCrypto {
    constructor(t) { if (t !== kToken) throw illegal(); }
    digest(algorithm, data) {
      try {
        need(arguments, 2, 'digest', 'SubtleCrypto');
        const name = typeof algorithm === 'string' ? algorithm : isObj(algorithm) && algorithm.name !== undefined ? String(algorithm.name) : null;
        if (name === null) throw new TypeError("Failed to execute 'digest' on 'SubtleCrypto': Algorithm: name: Missing or not a string");
        const alg = DIGESTS[name.toUpperCase()];
        if (!alg) throw new DOMException("Failed to execute 'digest' on 'SubtleCrypto': Algorithm: Unrecognized name", 'NotSupportedError');
        const b = toBytes(data);
        if (!b) throw new TypeError("Failed to execute 'digest' on 'SubtleCrypto': 2nd argument is not instance of ArrayBuffer, Buffer, TypedArray, or DataView.");
        const copy = b.slice();
        return Promise.resolve().then(() => {
          const out = toBytes(H.digest(alg, copy));
          return out.byteOffset === 0 && out.byteLength === out.buffer.byteLength ? out.buffer : out.slice().buffer;
        });
      } catch (e) {
        return Promise.reject(e);
      }
    }
  }
  for (const m of ['encrypt', 'decrypt', 'sign', 'verify', 'generateKey', 'deriveKey', 'deriveBits', 'importKey', 'exportKey', 'wrapKey', 'unwrapKey']) {
    def(SubtleCrypto.prototype, m, { [m]() { return Promise.reject(new DOMException(`Failed to execute '${m}' on 'SubtleCrypto': Algorithm: Unrecognized name`, 'NotSupportedError')); } }[m]);
  }
  const subtle = new SubtleCrypto(kToken);
  class Crypto {
    constructor(t) { if (t !== kToken) throw illegal(); }
    get subtle() { return subtle; }
    getRandomValues(array) {
      need(arguments, 1, 'getRandomValues', 'Crypto');
      if (!ArrayBuffer.isView(array) || array instanceof DataView) throw new TypeError("Failed to execute 'getRandomValues' on 'Crypto': parameter 1 is not of type 'ArrayBufferView'.");
      const tn = Object.prototype.toString.call(array).slice(8, -1);
      if (!INT_ARRAYS.includes(tn)) throw new DOMException(`Failed to execute 'getRandomValues' on 'Crypto': The provided ArrayBufferView is of type '${tn}', which is not an integer array type.`, 'TypeMismatchError');
      const n = array.byteLength;
      if (n > 65536) throw new DOMException(`Failed to execute 'getRandomValues' on 'Crypto': The ArrayBufferView's byte length (${n}) exceeds the number of bytes of entropy available via this API (65536).`, 'QuotaExceededError');
      if (n) new Uint8Array(array.buffer, array.byteOffset, n).set(randBytes(n));
      return array;
    }
    randomUUID() {
      const b = randBytes(16);
      let s = '';
      for (let i = 0; i < 16; i++) {
        let x = b[i];
        if (i === 6) x = (x & 0x0f) | 0x40;
        else if (i === 8) x = (x & 0x3f) | 0x80;
        s += (x < 16 ? '0' : '') + x.toString(16);
        if (i === 3 || i === 5 || i === 7 || i === 9) s += '-';
      }
      return s;
    }
  }
  expose('Crypto', Crypto);
  expose('SubtleCrypto', SubtleCrypto);
  expose('CryptoKey', CryptoKey);
  def(G, 'crypto', new Crypto(kToken));
  if (typeof Math.random !== 'function') def(Math, 'random', () => { const b = randBytes(8); return ((b[0] & 0x1f) * 2 ** 48 + b[1] * 2 ** 40 + b[2] * 2 ** 32 + b[3] * 2 ** 24 + b[4] * 2 ** 16 + b[5] * 2 ** 8 + b[6]) / 2 ** 53; });

  // ---- Storage ----
  const storageNS = new WeakMap();
  const nsOf = (o) => { const n = storageNS.get(o); if (!n) throw new TypeError('Illegal invocation'); return n; };
  const sKeys = (ns) => H.storage(ns, 'keys') || [];
  const sGet = (ns, k) => { const v = H.storage(ns, 'get', k); return v === null || v === undefined ? null : v; };
  class Storage {
    constructor(t) { if (t !== kToken) throw illegal(); }
    get length() { return sKeys(nsOf(this)).length; }
    key(index) {
      need(arguments, 1, 'key', 'Storage');
      const k = sKeys(nsOf(this))[Number(index) >>> 0];
      return k === undefined ? null : k;
    }
    getItem(key) { need(arguments, 1, 'getItem', 'Storage'); return sGet(nsOf(this), String(key)); }
    setItem(key, value) { need(arguments, 2, 'setItem', 'Storage'); H.storage(nsOf(this), 'set', String(key), String(value)); }
    removeItem(key) { need(arguments, 1, 'removeItem', 'Storage'); H.storage(nsOf(this), 'del', String(key)); }
    clear() { H.storage(nsOf(this), 'clear'); }
  }
  expose('Storage', Storage);
  const makeStorage = (ns) => {
    const target = new Storage(kToken);
    const proto = Storage.prototype;
    const hasOwn = Object.prototype.hasOwnProperty;
    // named properties are hidden by own and inherited properties
    const named = (t, k) => typeof k === 'string' && !hasOwn.call(t, k) && !(k in proto);
    const p = new Proxy(target, {
      get(t, k, r) {
        if (!named(t, k)) return Reflect.get(t, k, r);
        const v = sGet(ns, k);
        return v === null ? undefined : v;
      },
      set(t, k, v, r) {
        if (typeof k !== 'string') return Reflect.set(t, k, v, r);
        H.storage(ns, 'set', k, String(v));
        return true;
      },
      has(t, k) { return k in t || (typeof k === 'string' && sGet(ns, k) !== null); },
      deleteProperty(t, k) {
        if (named(t, k) && sGet(ns, k) !== null) { H.storage(ns, 'del', k); return true; }
        return Reflect.deleteProperty(t, k);
      },
      ownKeys(t) {
        const out = sKeys(ns).slice();
        for (const k of Reflect.ownKeys(t)) if (!out.includes(k)) out.push(k);
        return out;
      },
      getOwnPropertyDescriptor(t, k) {
        if (named(t, k)) {
          const v = sGet(ns, k);
          if (v !== null) return { value: v, writable: true, enumerable: true, configurable: true };
        }
        return Reflect.getOwnPropertyDescriptor(t, k);
      },
      defineProperty(t, k, d) {
        if (typeof k !== 'string') return Reflect.defineProperty(t, k, d);
        if (!('value' in d)) return false;
        H.storage(ns, 'set', k, String(d.value));
        return true;
      },
    });
    storageNS.set(p, ns);
    storageNS.set(target, ns);
    return p;
  };
  def(G, 'localStorage', makeStorage('localStorage'));
  def(G, 'sessionStorage', makeStorage('sessionStorage'));

  // ---- BroadcastChannel ----
  const channels = new Map(); // name → Set<BroadcastChannel>
  class BroadcastChannel extends EventTarget {
    #name;
    #closed = false;
    constructor(name) {
      need(arguments, 1, null, 'BroadcastChannel');
      super();
      this.#name = String(name);
      let set = channels.get(this.#name);
      if (!set) channels.set(this.#name, (set = new Set()));
      set.add(this);
    }
    get name() { return this.#name; }
    postMessage(message) {
      need(arguments, 1, 'postMessage', 'BroadcastChannel');
      if (this.#closed) throw new DOMException("Failed to execute 'postMessage' on 'BroadcastChannel': Channel is closed", 'InvalidStateError');
      const data = structuredClone(message);
      const origin = originOf(loc);
      for (const t of channels.get(this.#name) || []) {
        if (t === this) continue;
        jr.immediate(() => {
          if (t.#closed) return;
          let d;
          try { d = structuredClone(data); } catch { t.dispatchEvent(new MessageEvent('messageerror', { origin })); return; }
          t.dispatchEvent(new MessageEvent('message', { data: d, origin }));
        });
      }
    }
    close() {
      if (this.#closed) return;
      this.#closed = true;
      const set = channels.get(this.#name);
      if (set) { set.delete(this); if (!set.size) channels.delete(this.#name); }
    }
  }
  handlers(BroadcastChannel.prototype, ['message', 'messageerror']);
  expose('BroadcastChannel', BroadcastChannel);

  // ---- MessageChannel / MessagePort ----
  let mpEntangle;
  class MessagePort extends EventTarget {
    #other = null;
    #queue = [];
    #started = false;
    #closed = false;
    #onmessage = null;
    static { mpEntangle = (a, b) => { a.#other = b; b.#other = a; }; }
    constructor(t) { if (t !== kToken) throw illegal(); super(); }
    #deliverOne() {
      if (this.#closed || !this.#queue.length) return;
      const [data, ports] = this.#queue.shift();
      this.dispatchEvent(new MessageEvent('message', { data, ports }));
    }
    postMessage(message, transfer = undefined) {
      need(arguments, 1, 'postMessage', 'MessagePort');
      const list = Array.isArray(transfer) ? transfer : isObj(transfer) && Array.isArray(transfer.transfer) ? transfer.transfer : [];
      const ports = list.filter((x) => x instanceof MessagePort);
      const data = structuredClone(message);
      const o = this.#other;
      if (this.#closed || !o || o.#closed) return;
      o.#queue.push([data, ports]);
      if (o.#started) jr.immediate(() => o.#deliverOne());
    }
    start() {
      if (this.#started) return;
      this.#started = true;
      for (let i = 0; i < this.#queue.length; i++) jr.immediate(() => this.#deliverOne());
    }
    close() {
      this.#closed = true;
      this.#queue = [];
      const o = this.#other;
      this.#other = null;
      if (o) o.#other = null;
    }
    get onmessage() { return this.#onmessage; }
    set onmessage(v) {
      this.#onmessage = typeof v === 'function' || isObj(v) ? v : null;
      if (this.#onmessage) this.start();
    }
  }
  handlers(MessagePort.prototype, ['messageerror', 'close']);
  class MessageChannel {
    #p1 = new MessagePort(kToken);
    #p2 = new MessagePort(kToken);
    constructor() { mpEntangle(this.#p1, this.#p2); }
    get port1() { return this.#p1; }
    get port2() { return this.#p2; }
  }
  expose('MessagePort', MessagePort);
  expose('MessageChannel', MessageChannel);

  // ---- navigator ----
  const platform = String(H.platform || 'Linux x86_64');
  const uaOS = platform.startsWith('Mac') ? 'Macintosh; Intel Mac OS X 10_15_7' : platform.startsWith('Win') ? 'Windows NT 10.0; Win64; x64' : 'X11; Linux x86_64';
  const uaPlatform = platform.startsWith('Mac') ? 'macOS' : platform.startsWith('Win') ? 'Windows' : 'Linux';
  const CHROME = '142';
  const userAgent = `Mozilla/5.0 (${uaOS}) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/${CHROME}.0.0.0 Safari/537.36`;
  const language = String(H.language || 'en-US');
  const languages = Object.freeze(language.includes('-') ? [language, language.split('-')[0]] : [language]);
  const brands = Object.freeze([
    Object.freeze({ brand: 'Chromium', version: CHROME }),
    Object.freeze({ brand: 'Google Chrome', version: CHROME }),
    Object.freeze({ brand: 'Not_A Brand', version: '99' }),
  ]);
  class NavigatorUAData {
    constructor(t) { if (t !== kToken) throw illegal(); }
    get brands() { return brands; }
    get mobile() { return false; }
    get platform() { return uaPlatform; }
    getHighEntropyValues(hints = []) {
      const all = { architecture: 'x86', bitness: '64', model: '', platformVersion: uaPlatform === 'macOS' ? '15.0.0' : uaPlatform === 'Windows' ? '15.0.0' : '6.8.0', uaFullVersion: CHROME + '.0.0.0', fullVersionList: brands.map((b) => ({ brand: b.brand, version: b.version === '99' ? '99.0.0.0' : CHROME + '.0.0.0' })), wow64: false, formFactors: ['Desktop'] };
      const out = { brands: brands.slice(), mobile: false, platform: uaPlatform };
      for (const h of Array.from(hints)) if (h in all) out[h] = all[h];
      return Promise.resolve(out);
    }
    toJSON() { return { brands: brands.slice(), mobile: false, platform: uaPlatform }; }
  }
  class ClipboardItem {
    #items;
    constructor(items, options = undefined) {
      need(arguments, 1, null, 'ClipboardItem');
      if (!isObj(items) || !Object.keys(items).length) throw new TypeError("Failed to construct 'ClipboardItem': Empty dictionary argument");
      this.#items = { ...items };
      this.presentationStyle = options && options.presentationStyle || 'unspecified';
    }
    get types() { return Object.freeze(Object.keys(this.#items)); }
    getType(type) {
      if (!(type in this.#items)) return Promise.reject(new DOMException("Failed to execute 'getType' on 'ClipboardItem': The type was not found", 'NotFoundError'));
      return Promise.resolve(this.#items[type]).then((v) => v instanceof Blob ? v : new Blob([String(v)], { type }));
    }
    static supports(type) { return ['text/plain', 'text/html', 'image/png'].includes(String(type)); }
  }
  class Clipboard extends EventTarget {
    constructor(t) { if (t !== kToken) throw illegal(); super(); }
    writeText(data) {
      try { need(arguments, 1, 'writeText', 'Clipboard'); H.clipboardWrite(String(data)); return Promise.resolve(); } catch (e) { return Promise.reject(domErr(e, 'NotAllowedError')); }
    }
    readText() {
      try { return Promise.resolve(String(H.clipboardRead() ?? '')); } catch (e) { return Promise.reject(domErr(e, 'NotAllowedError')); }
    }
    write(items) {
      need(arguments, 1, 'write', 'Clipboard');
      const list = Array.from(items);
      const item = list.find((x) => x instanceof ClipboardItem && x.types.includes('text/plain'));
      if (!item) return Promise.reject(new DOMException("Failed to execute 'write' on 'Clipboard': Only text/plain is supported", 'NotAllowedError'));
      return item.getType('text/plain').then((b) => b.text()).then((t) => this.writeText(t));
    }
    read() { return this.readText().then((t) => [new ClipboardItem({ 'text/plain': new Blob([t], { type: 'text/plain' }) })]); }
  }

  // Web Locks
  class Lock {
    #name; #mode;
    constructor(t, name, mode) { if (t !== kToken) throw illegal(); this.#name = name; this.#mode = mode; }
    get name() { return this.#name; }
    get mode() { return this.#mode; }
  }
  const lockQueues = new Map(); // name → { held: [req], pending: [req] }
  const lockQ = (name) => { let q = lockQueues.get(name); if (!q) lockQueues.set(name, (q = { held: [], pending: [] })); return q; };
  const lockGrantable = (q, req) => {
    if (q.pending[0] !== req) return false;
    return req.mode === 'exclusive' ? q.held.length === 0 : !q.held.some((h) => h.mode === 'exclusive');
  };
  const lockRelease = (req) => {
    const q = lockQueues.get(req.name);
    if (!q) return;
    const i = q.held.indexOf(req);
    if (i >= 0) q.held.splice(i, 1);
    lockProcess(req.name);
  };
  const lockGrant = (q, req) => {
    q.held.push(req);
    if (req.signal) req.signal.removeEventListener('abort', req.onAbort);
    const lock = new Lock(kToken, req.name, req.mode);
    Promise.resolve().then(() => req.cb(lock)).then(
      (v) => { lockRelease(req); req.resolve(v); },
      (e) => { lockRelease(req); req.reject(e); });
  };
  const lockProcess = (name) => {
    const q = lockQueues.get(name);
    if (!q) return;
    while (q.pending.length && lockGrantable(q, q.pending[0])) lockGrant(q, q.pending.shift());
    if (!q.held.length && !q.pending.length) lockQueues.delete(name);
  };
  const lockErr = (msg) => new DOMException(`Failed to execute 'request' on 'LockManager': ${msg}`, 'NotSupportedError');
  class LockManager {
    constructor(t) { if (t !== kToken) throw illegal(); }
    request(name, options, callback = undefined) {
      try {
        need(arguments, 2, 'request', 'LockManager');
        if (arguments.length < 3) { callback = options; options = {}; }
        options = options || {};
        if (typeof callback !== 'function') throw new TypeError("Failed to execute 'request' on 'LockManager': parameter 3 is not of type 'LockGrantedCallback'.");
        name = String(name);
        const mode = options.mode === undefined ? 'exclusive' : String(options.mode);
        if (mode !== 'exclusive' && mode !== 'shared') throw new TypeError(`Failed to execute 'request' on 'LockManager': Failed to read the 'mode' property from 'LockOptions': The provided value '${mode}' is not a valid enum value of type LockMode.`);
        const ifAvailable = !!options.ifAvailable, steal = !!options.steal, signal = options.signal;
        if (signal !== undefined && signal !== null && !isSignal(signal)) throw new TypeError("Failed to execute 'request' on 'LockManager': Failed to read the 'signal' property from 'LockOptions': Failed to convert value to 'AbortSignal'.");
        if (name.startsWith('-')) throw lockErr("Names cannot start with '-'.");
        if (steal && ifAvailable) throw lockErr("The 'steal' and 'ifAvailable' options cannot be used together.");
        if (steal && mode !== 'exclusive') throw lockErr("The 'steal' option may only be used with 'exclusive' locks.");
        if (signal && steal) throw lockErr("The 'signal' and 'steal' options cannot be used together.");
        if (signal && ifAvailable) throw lockErr("The 'signal' and 'ifAvailable' options cannot be used together.");
        if (signal && signal.aborted) return Promise.reject(signal.reason);
        return new Promise((resolve, reject) => {
          const req = { name, mode, cb: callback, resolve, reject, signal, onAbort: null };
          const q = lockQ(name);
          if (steal) {
            const stolen = q.held;
            q.held = [];
            for (const h of stolen) h.reject(new DOMException("Lock broken by another request with the 'steal' option.", 'AbortError'));
            q.pending.unshift(req);
          } else if (ifAvailable) {
            const ok = !q.pending.length && (mode === 'exclusive' ? !q.held.length : !q.held.some((h) => h.mode === 'exclusive'));
            if (!ok) {
              if (!q.held.length && !q.pending.length) lockQueues.delete(name);
              Promise.resolve().then(() => callback(null)).then(resolve, reject);
              return;
            }
            q.pending.push(req);
          } else q.pending.push(req);
          if (signal) {
            req.onAbort = () => {
              const i = q.pending.indexOf(req);
              if (i < 0) return;
              q.pending.splice(i, 1);
              reject(signal.reason);
              lockProcess(name);
            };
            signal.addEventListener('abort', req.onAbort, { once: true });
          }
          lockProcess(name);
        });
      } catch (e) {
        return Promise.reject(e);
      }
    }
    query() {
      const held = [], pending = [];
      for (const [name, q] of lockQueues) {
        for (const r of q.held) held.push({ name, mode: r.mode, clientId: 'clack' });
        for (const r of q.pending) pending.push({ name, mode: r.mode, clientId: 'clack' });
      }
      return Promise.resolve({ held, pending });
    }
  }
  class PermissionStatus extends EventTarget {
    #name;
    constructor(t, name) { if (t !== kToken) throw illegal(); super(); this.#name = name; }
    get name() { return this.#name; }
    get state() { return this.#name === 'notifications' && notifPermission === 'denied' ? 'denied' : 'prompt'; }
  }
  handlers(PermissionStatus.prototype, ['change']);
  class Permissions {
    constructor(t) { if (t !== kToken) throw illegal(); }
    query(desc) {
      if (!isObj(desc) || desc.name === undefined) return Promise.reject(new TypeError("Failed to execute 'query' on 'Permissions': Failed to read the 'name' property from 'PermissionDescriptor': Required member is undefined."));
      return Promise.resolve(new PermissionStatus(kToken, String(desc.name)));
    }
  }
  class StorageManager {
    constructor(t) { if (t !== kToken) throw illegal(); }
    estimate() { return Promise.resolve({ quota: 2 ** 31, usage: 0, usageDetails: {} }); }
    persist() { return Promise.resolve(false); }
    persisted() { return Promise.resolve(false); }
  }
  class MediaDevices extends EventTarget {
    constructor(t) {
      if (t !== kToken) throw illegal();
      super();
      const denied = (m) => () => Promise.reject(new DOMException(`Failed to execute '${m}' on 'MediaDevices': Permission denied`, 'NotAllowedError'));
      // own writable properties so plugins can wrap them
      for (const m of ['getUserMedia', 'getDisplayMedia']) Object.defineProperty(this, m, { value: denied(m), writable: true, configurable: true, enumerable: true });
      Object.defineProperty(this, 'enumerateDevices', { value: () => Promise.resolve([]), writable: true, configurable: true, enumerable: true });
      Object.defineProperty(this, 'getSupportedConstraints', { value: () => ({ deviceId: true, groupId: true, width: true, height: true, aspectRatio: true, frameRate: true, facingMode: true, echoCancellation: true, autoGainControl: true, noiseSuppression: true, sampleRate: true, sampleSize: true, channelCount: true }), writable: true, configurable: true, enumerable: true });
    }
  }
  handlers(MediaDevices.prototype, ['devicechange']);
  class NetworkInformation extends EventTarget {
    constructor(t) { if (t !== kToken) throw illegal(); super(); }
    get effectiveType() { return '4g'; }
    get rtt() { return 50; }
    get downlink() { return 10; }
    get saveData() { return false; }
  }
  handlers(NetworkInformation.prototype, ['change']);
  const nav = {
    clipboard: new Clipboard(kToken), locks: new LockManager(kToken), permissions: new Permissions(kToken),
    storage: new StorageManager(kToken), mediaDevices: new MediaDevices(kToken), userAgentData: new NavigatorUAData(kToken),
    connection: new NetworkInformation(kToken),
  };
  class Navigator {
    constructor(t) { if (t !== kToken) throw illegal(); }
    get userAgent() { return userAgent; }
    get appVersion() { return userAgent.slice(8); }
    get appName() { return 'Netscape'; }
    get appCodeName() { return 'Mozilla'; }
    get product() { return 'Gecko'; }
    get productSub() { return '20030107'; }
    get vendor() { return 'Google Inc.'; }
    get vendorSub() { return ''; }
    get platform() { return platform; }
    get language() { return language; }
    get languages() { return languages; }
    get onLine() { return true; }
    get cookieEnabled() { return true; }
    get hardwareConcurrency() { return 4; }
    get deviceMemory() { return 8; }
    get maxTouchPoints() { return 0; }
    get doNotTrack() { return null; }
    get webdriver() { return false; }
    get pdfViewerEnabled() { return false; }
    get clipboard() { return nav.clipboard; }
    get locks() { return nav.locks; }
    get permissions() { return nav.permissions; }
    get storage() { return nav.storage; }
    get mediaDevices() { return nav.mediaDevices; }
    get userAgentData() { return nav.userAgentData; }
    get connection() { return nav.connection; }
    javaEnabled() { return false; }
    vibrate() { return false; }
    sendBeacon(url, data = null) {
      need(arguments, 1, 'sendBeacon', 'Navigator');
      const u = parseRelative(url);
      if (!u) throw new TypeError(`Failed to execute 'sendBeacon' on 'Navigator': The URL argument is ill-formed or unsupported.`);
      if (u.scheme !== 'http' && u.scheme !== 'https') throw new TypeError(`Failed to execute 'sendBeacon' on 'Navigator': Beacons are only supported over HTTP(S).`);
      if (data !== null && data !== undefined) {
        const ex = extractBody(data, "Failed to execute 'sendBeacon' on 'Navigator'");
        if (ex.bytes && ex.bytes.length > 65536) return false;
      }
      fetch(serialize(u), { method: 'POST', body: data === undefined ? null : data, keepalive: true, credentials: 'include', mode: 'no-cors' }).catch(() => {});
      return true;
    }
  }
  expose('Navigator', Navigator);
  for (const [n, C] of Object.entries({ Clipboard, ClipboardItem, Lock, LockManager, Permissions, PermissionStatus, StorageManager, MediaDevices, NavigatorUAData, NetworkInformation })) expose(n, C);
  def(G, 'navigator', new Navigator(kToken));

  // ---- location / history ----
  let notifPermission = 'default';
  const hist = { entries: [{ url: locHref(), state: null }], index: 0, scroll: 'auto' };
  const setLocation = (u, replace, state) => {
    loc = u;
    const e = { url: serialize(u), state: state === undefined ? null : state };
    if (replace) hist.entries[hist.index] = e;
    else {
      hist.entries.length = hist.index + 1;
      hist.entries.push(e);
      hist.index++;
    }
  };
  const navigate = (v, replace, method) => {
    const u = parseRelative(v);
    if (!u) throw new DOMException(`Failed to execute '${method}' on 'Location': '${v}' is not a valid URL.`, 'SyntaxError');
    if (u.scheme === 'javascript') return;
    if (originOf(u) !== originOf(loc)) { H.openURL(serialize(u)); return; }
    setLocation(u, replace, null);
  };
  const withLoc = (f) => { const u = new URL(kToken, copyRecord(loc)); f(u); navigate(u.href, false, 'assign'); };
  class Location {
    constructor(t) { if (t !== kToken) throw illegal(); }
    get href() { return serialize(loc); }
    set href(v) { navigate(String(v), false, 'href'); }
    get origin() { return originOf(loc); }
    get protocol() { return loc.scheme + ':'; }
    set protocol(v) { withLoc((u) => { u.protocol = v; }); }
    get host() { return loc.host === null ? '' : loc.port === null ? loc.host : loc.host + ':' + loc.port; }
    set host(v) { withLoc((u) => { u.host = v; }); }
    get hostname() { return loc.host || ''; }
    set hostname(v) { withLoc((u) => { u.hostname = v; }); }
    get port() { return loc.port === null ? '' : String(loc.port); }
    set port(v) { withLoc((u) => { u.port = v; }); }
    get pathname() { return pathStr(loc); }
    set pathname(v) { withLoc((u) => { u.pathname = v; }); }
    get search() { return loc.query ? '?' + loc.query : ''; }
    set search(v) { withLoc((u) => { u.search = v; }); }
    get hash() { return loc.fragment ? '#' + loc.fragment : ''; }
    set hash(v) { withLoc((u) => { u.hash = v; }); }
    get ancestorOrigins() { return Object.freeze({ length: 0, item: () => null, contains: () => false }); }
    assign(url) { need(arguments, 1, 'assign', 'Location'); navigate(String(url), false, 'assign'); }
    replace(url) { need(arguments, 1, 'replace', 'Location'); navigate(String(url), true, 'replace'); }
    reload() { G.console.debug('location.reload() ignored'); }
    toString() { return serialize(loc); }
  }
  for (const k of ['href', 'origin', 'protocol', 'host', 'hostname', 'port', 'pathname', 'search', 'hash']) {
    Object.defineProperty(Location.prototype, k, { ...Object.getOwnPropertyDescriptor(Location.prototype, k), enumerable: true });
  }
  const location = new Location(kToken);
  class History {
    constructor(t) { if (t !== kToken) throw illegal(); }
    get length() { return hist.entries.length; }
    get state() { return hist.entries[hist.index].state; }
    get scrollRestoration() { return hist.scroll; }
    set scrollRestoration(v) { v = String(v); if (v === 'auto' || v === 'manual') hist.scroll = v; }
    #change(m, state, url, replace) {
      let u = loc;
      if (url !== undefined && url !== null) {
        u = parseRelative(url);
        if (!u) throw new DOMException(`Failed to execute '${m}' on 'History': A history state object with URL '${url}' cannot be created in a document with origin '${originOf(loc)}' and URL '${serialize(loc)}'.`, 'SecurityError');
        if (originOf(u) !== originOf(loc)) throw new DOMException(`Failed to execute '${m}' on 'History': A history state object with URL '${serialize(u)}' cannot be created in a document with origin '${originOf(loc)}' and URL '${serialize(loc)}'.`, 'SecurityError');
      } else u = copyRecord(loc);
      setLocation(u, replace, structuredClone(state));
    }
    pushState(state, unused, url = undefined) { need(arguments, 2, 'pushState', 'History'); this.#change('pushState', state, url, false); }
    replaceState(state, unused, url = undefined) { need(arguments, 2, 'replaceState', 'History'); this.#change('replaceState', state, url, true); }
    back() {}
    forward() {}
    go() {}
  }
  expose('Location', Location);
  expose('History', History);
  Object.defineProperty(G, 'location', { get: () => location, set: (v) => { location.href = v; }, configurable: true, enumerable: false });
  def(G, 'history', new History(kToken));
  Object.defineProperty(G, 'origin', { get: () => originOf(loc), set: (v) => { Object.defineProperty(G, 'origin', { value: v, writable: true, configurable: true }); }, configurable: true, enumerable: false });
  def(G, 'isSecureContext', true);
  def(G, 'crossOriginIsolated', false);
  if (typeof G.open !== 'function') {
    def(G, 'open', function open(url = '') {
      const s = String(url);
      if (s) { const u = parseRelative(s); if (u && u.scheme !== 'javascript') H.openURL(serialize(u)); }
      return null;
    });
  }
  jr.locationURL = () => serialize(loc);

  // ---- Notification ----
  let notifOf;
  class Notification extends EventTarget {
    #o;
    static { notifOf = (n) => n.#o; }
    constructor(title, options = undefined) {
      need(arguments, 1, null, 'Notification');
      super();
      const o = options || {};
      this.#o = {
        title: String(title), body: o.body === undefined ? '' : String(o.body), dir: o.dir || 'auto', lang: o.lang === undefined ? '' : String(o.lang),
        tag: o.tag === undefined ? '' : String(o.tag), icon: o.icon === undefined ? '' : String(o.icon), badge: o.badge === undefined ? '' : String(o.badge),
        image: o.image === undefined ? '' : String(o.image), data: o.data === undefined ? null : structuredClone(o.data),
        requireInteraction: !!o.requireInteraction, silent: o.silent === undefined ? null : !!o.silent, renotify: !!o.renotify,
        timestamp: o.timestamp === undefined ? Date.now() : Number(o.timestamp), actions: Object.freeze([]), vibrate: Object.freeze([]),
      };
      if (notifPermission !== 'granted') jr.immediate(() => this.dispatchEvent(new Event('error')));
    }
    static get permission() { return notifPermission; }
    static get maxActions() { return 2; }
    static requestPermission(cb = undefined) {
      if (notifPermission === 'default') notifPermission = 'denied';
      const p = Promise.resolve(notifPermission);
      if (typeof cb === 'function') p.then(cb);
      return p;
    }
    close() {}
  }
  for (const k of ['title', 'body', 'dir', 'lang', 'tag', 'icon', 'badge', 'image', 'data', 'requireInteraction', 'silent', 'renotify', 'timestamp', 'actions', 'vibrate']) {
    Object.defineProperty(Notification.prototype, k, { get() { return notifOf(this)[k]; }, enumerable: true, configurable: true });
  }
  handlers(Notification.prototype, ['click', 'show', 'error', 'close']);
  expose('Notification', Notification);

  // ---- WebSocket / EventSource stubs (no sockets: they fail asynchronously) ----
  class CloseEvent extends Event {
    constructor(type, init = {}) {
      super(type, init);
      this.wasClean = !!(init && init.wasClean);
      this.code = (init && init.code) || 0;
      this.reason = init && init.reason !== undefined ? String(init.reason) : '';
    }
  }
  expose('CloseEvent', CloseEvent);
  class WebSocket extends EventTarget {
    #url;
    #state = 0;
    #binaryType = 'blob';
    constructor(url, protocols = undefined) {
      need(arguments, 1, null, 'WebSocket');
      super();
      const u = parseRelative(url);
      if (!u) throw new DOMException(`Failed to construct 'WebSocket': The URL '${url}' is invalid.`, 'SyntaxError');
      if (u.scheme === 'http') u.scheme = 'ws';
      else if (u.scheme === 'https') u.scheme = 'wss';
      else if (u.scheme !== 'ws' && u.scheme !== 'wss') throw new DOMException(`Failed to construct 'WebSocket': The URL's scheme must be either 'http', 'https', 'ws', or 'wss'. '${u.scheme}' is not allowed.`, 'SyntaxError');
      if (u.fragment !== null) throw new DOMException(`Failed to construct 'WebSocket': The URL contains a fragment identifier ('${u.fragment}'). Fragment identifiers are not allowed in WebSocket URLs.`, 'SyntaxError');
      this.#url = serialize(u);
      setTimeout(() => {
        if (this.#state === 3) return;
        this.#state = 3;
        this.dispatchEvent(new Event('error'));
        this.dispatchEvent(new CloseEvent('close', { code: 1006, reason: '', wasClean: false }));
      }, 0);
    }
    get url() { return this.#url; }
    get readyState() { return this.#state; }
    get bufferedAmount() { return 0; }
    get extensions() { return ''; }
    get protocol() { return ''; }
    get binaryType() { return this.#binaryType; }
    set binaryType(v) { v = String(v); if (v === 'blob' || v === 'arraybuffer') this.#binaryType = v; }
    send(data) {
      need(arguments, 1, 'send', 'WebSocket');
      if (this.#state === 0) throw new DOMException("Failed to execute 'send' on 'WebSocket': Still in CONNECTING state.", 'InvalidStateError');
    }
    close(code = undefined, reason = undefined) {
      if (code !== undefined && code !== 1000 && !(code >= 3000 && code <= 4999)) throw new DOMException(`Failed to execute 'close' on 'WebSocket': The close code must be either 1000, or between 3000 and 4999. ${code} is neither.`, 'InvalidAccessError');
      if (this.#state === 0 || this.#state === 1) this.#state = 2;
    }
  }
  constants(WebSocket, { CONNECTING: 0, OPEN: 1, CLOSING: 2, CLOSED: 3 });
  handlers(WebSocket.prototype, ['open', 'message', 'error', 'close']);
  expose('WebSocket', WebSocket);
  class EventSource extends EventTarget {
    #url;
    #state = 0;
    #cred;
    constructor(url, init = undefined) {
      need(arguments, 1, null, 'EventSource');
      super();
      const u = parseRelative(url);
      if (!u) throw new DOMException(`Failed to construct 'EventSource': Cannot open an EventSource to '${url}'. The URL is invalid.`, 'SyntaxError');
      this.#url = serialize(u);
      this.#cred = !!(init && init.withCredentials);
      setTimeout(() => {
        if (this.#state === 2) return;
        this.#state = 2;
        this.dispatchEvent(new Event('error'));
      }, 0);
    }
    get url() { return this.#url; }
    get readyState() { return this.#state; }
    get withCredentials() { return this.#cred; }
    close() { this.#state = 2; }
  }
  constants(EventSource, { CONNECTING: 0, OPEN: 1, CLOSED: 2 });
  handlers(EventSource.prototype, ['open', 'message', 'error']);
  expose('EventSource', EventSource);
})();
