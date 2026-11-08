// Clack's Taut runtime: core helpers shared by every part of the TautAPI.
// Clack implements Taut's plugin contract independently (Taut is GPLv3; no
// Taut code is copied). All files in js/ are concatenated in name order into
// one classic script that runs once per JS runtime; see runtime.go.

const G = globalThis
const host = G.__clack || {}

// Fallbacks for web APIs every plugin needs, used only where the JS
// environment doesn't provide them yet (jsrt does; these keep the TautAPI
// usable regardless).
if (typeof G.AbortSignal === 'undefined' && typeof G.EventTarget === 'function') {
  class AbortSignalImpl extends G.EventTarget {
    constructor() {
      super()
      this.aborted = false
      this.reason = undefined
      this.onabort = null
    }
    throwIfAborted() {
      if (this.aborted) throw this.reason
    }
    static abort(reason) {
      const c = new G.AbortController()
      c.abort(reason)
      return c.signal
    }
    static timeout(ms) {
      const c = new G.AbortController()
      setTimeout(() => c.abort(abortError('The operation timed out.', 'TimeoutError')), ms)
      return c.signal
    }
    static any(signals) {
      const c = new G.AbortController()
      for (const s of signals) {
        if (s.aborted) {
          c.abort(s.reason)
          return c.signal
        }
      }
      for (const s of signals) s.addEventListener('abort', () => c.abort(s.reason), { once: true })
      return c.signal
    }
  }
  const abortError = (message, name) => {
    if (typeof G.DOMException === 'function') return new G.DOMException(message, name)
    const e = new Error(message)
    e.name = name
    return e
  }
  class AbortControllerImpl {
    constructor() {
      this.signal = new AbortSignalImpl()
    }
    abort(reason) {
      const s = this.signal
      if (s.aborted) return
      s.aborted = true
      s.reason = reason === undefined ? abortError('signal is aborted without reason', 'AbortError') : reason
      const ev = new G.Event('abort')
      try {
        s.onabort?.(ev)
      } catch {}
      s.dispatchEvent(ev)
    }
  }
  G.AbortSignal = AbortSignalImpl
  G.AbortController = AbortControllerImpl
}
if (typeof G.navigator === 'undefined') {
  G.navigator = { language: 'en-US', languages: ['en-US'], platform: 'Linux x86_64', userAgent: 'Clack', onLine: true }
}
const T = (G.__taut = G.__taut || {})

/** calls a Go host function registered as __clack.taut_<name> */
function go(name, ...args) {
  const fn = host['taut_' + name]
  if (typeof fn !== 'function') throw new Error(`[Taut] host function taut_${name} is missing`)
  return fn(...args)
}
/** like go, but a missing host function returns `fallback` */
function goOr(fallback, name, ...args) {
  const fn = host['taut_' + name]
  return typeof fn === 'function' ? fn(...args) : fallback
}

const log = (...args) => console.log('[Taut]', ...args)
const warn = (...args) => console.warn('[Taut]', ...args)
const error = (...args) => console.error('[Taut]', ...args)
const errorText = (err) => (err instanceof Error ? err.message : String(err))

function deepEqual(a, b) {
  if (a === b) return true
  if (a === null || b === null || typeof a !== 'object' || typeof b !== 'object') return false
  if (Array.isArray(a) !== Array.isArray(b)) return false
  const ka = Object.keys(a)
  const kb = Object.keys(b)
  if (ka.length !== kb.length) return false
  for (const k of ka) {
    if (!Object.prototype.hasOwnProperty.call(b, k) || !deepEqual(a[k], b[k])) return false
  }
  return true
}

const clone = (v) => (v === undefined ? undefined : typeof structuredClone === 'function' ? structuredClone(v) : JSON.parse(JSON.stringify(v)))

const isPlainObject = (v) => {
  if (v === null || typeof v !== 'object') return false
  const p = Object.getPrototypeOf(v)
  return p === Object.prototype || p === null
}

const sleep = (ms) => new Promise((r) => setTimeout(r, ms))

function abortableSleep(ms, signal) {
  return new Promise((resolve, reject) => {
    if (signal?.aborted) return reject(signal.reason)
    const onAbort = () => {
      clearTimeout(t)
      reject(signal.reason)
    }
    const t = setTimeout(() => {
      signal?.removeEventListener?.('abort', onAbort)
      resolve()
    }, ms)
    signal?.addEventListener?.('abort', onAbort, { once: true })
  })
}

function anySignal(signals) {
  if (typeof AbortSignal.any === 'function') return AbortSignal.any(signals)
  const c = new AbortController()
  for (const s of signals) {
    if (s.aborted) {
      c.abort(s.reason)
      break
    }
    s.addEventListener('abort', () => c.abort(s.reason), { once: true })
  }
  return c.signal
}

const unavailableReported = new Set()
/** `work` (a promise, or a function given an AbortSignal) settled within `ms`, else undefined and one log naming `what` */
function within(work, ms, what, signal) {
  const deadline = new AbortController()
  let promise
  try {
    promise =
      typeof work === 'function'
        ? work(signal ? anySignal([deadline.signal, signal]) : deadline.signal)
        : work
  } catch (err) {
    return Promise.reject(err)
  }
  return new Promise((resolve, reject) => {
    const t = setTimeout(() => {
      if (what && !unavailableReported.has(what)) {
        unavailableReported.add(what)
        error(`${what} is unavailable`)
      }
      deadline.abort()
      resolve(undefined)
    }, ms)
    Promise.resolve(promise).then(
      (v) => {
        clearTimeout(t)
        resolve(v)
      },
      (e) => {
        clearTimeout(t)
        reject(e)
      }
    )
  })
}

async function retry(attempt, { tries = 3, baseMs = 1000, maxMs = 30000 } = {}) {
  for (let i = 0; ; i++) {
    const v = await attempt()
    if (v !== undefined || i >= tries - 1) return v
    await sleep(Math.random() * Math.min(maxMs, baseMs * 2 ** i))
  }
}

/** a value with change listeners, readable in a render as a hook */
class Store {
  #value
  #listeners = new Set()
  constructor(initial) {
    this.#value = initial
  }
  get = () => this.#value
  set = (next) => {
    this.#value = next
    this.#notify()
  }
  update = (fn) => this.set(fn(this.#value))
  subscribe = (fn) => {
    const l = () => fn()
    this.#listeners.add(l)
    return () => this.#listeners.delete(l)
  }
  use = () => G.React.useSyncExternalStore(this.subscribe, this.get)
  #notify() {
    for (const l of [...this.#listeners]) {
      try {
        l()
      } catch (err) {
        error('store listener threw', err)
      }
    }
  }
}

/** an async mutex by name, the in-process stand-in for navigator.locks */
const lockTails = new Map()
function withLock(name, fn) {
  const prev = lockTails.get(name) || Promise.resolve()
  const run = prev.then(() => fn())
  const tail = run.then(
    () => {},
    () => {}
  )
  lockTails.set(name, tail)
  tail.then(() => {
    if (lockTails.get(name) === tail) lockTails.delete(name)
  })
  return run
}

/** calls `cb` once after the current JS task, coalescing repeated requests */
function batcher(cb) {
  let queued = false
  return () => {
    if (queued) return
    queued = true
    queueMicrotask(() => {
      queued = false
      cb()
    })
  }
}

// ---- values crossing to Go: functions become ids, Go functions become callables ----

/** ids handed out from JS for taut payloads; bit 51 marks them as taut's (jsrt's own fn ids leave it clear) */
const FN_BASE = 2 ** 51
let nextFnId = 1
const jsFns = new Map()
const fnIds = new WeakMap()
function fnRef(fn) {
  let id = fnIds.get(fn)
  if (!id) {
    id = FN_BASE + nextFnId++
    fnIds.set(fn, id)
    jsFns.set(id, fn)
  }
  return { $fn: id }
}
T.callFn = (id, args) => {
  const fn = jsFns.get(id)
  if (!fn) throw new Error(`[Taut] function ${id} was released`)
  return fn(...decode(args || []))
}
T.releaseFns = (ids) => {
  for (const id of ids || []) {
    const fn = jsFns.get(id)
    if (fn) fnIds.delete(fn)
    jsFns.delete(id)
  }
}

const goFnCache = new Map()
function goFn(id) {
  let f = goFnCache.get(id)
  if (!f) {
    f = (...args) => go('callGo', id, encode(args))
    f.$gofn = id
    goFnCache.set(id, f)
  }
  return f
}

/** a JSON-safe copy of `v` for Go, functions as {$fn} and Go callables back as {$gofn} */
function encode(v, seen = new Set()) {
  if (v === null || v === undefined) return v ?? null
  switch (typeof v) {
    case 'function':
      return v.$gofn ? { $gofn: v.$gofn } : fnRef(v)
    case 'object': {
      if (seen.has(v)) return null
      if (G.React && G.React.isValidElement && G.React.isValidElement(v)) return { $el: elementRef(v) }
      if (typeof v.toJSON === 'function' && !(v instanceof Date)) return encode(v.toJSON(), seen)
      if (v instanceof Date) return v.toISOString()
      seen.add(v)
      let out
      if (Array.isArray(v)) out = v.map((x) => encode(x, seen))
      else {
        out = {}
        for (const k in v) {
          const x = v[k]
          if (x === undefined) continue
          out[k] = encode(x, seen)
        }
      }
      seen.delete(v)
      return out
    }
    case 'bigint':
      return String(v)
    case 'symbol':
      return null
    default:
      return v
  }
}

/** encode, with Blobs and Files read into {$file: {name, type, data}} (base64), for natives that need the bytes */
async function encodeAsync(v) {
  const files = []
  const walk = (x, seen = new Set()) => {
    if (!x || typeof x !== 'object' || seen.has(x)) return
    seen.add(x)
    if (typeof Blob !== 'undefined' && x instanceof Blob) return files.push(x)
    for (const k in x) walk(x[k], seen)
  }
  walk(v)
  if (!files.length) return encode(v)
  const read = new Map()
  for (const f of files) {
    const bytes = new Uint8Array(await f.arrayBuffer())
    let bin = ''
    for (let i = 0; i < bytes.length; i += 0x8000) bin += String.fromCharCode(...bytes.subarray(i, i + 0x8000))
    read.set(f, { $file: { name: f.name ?? 'blob', type: f.type || 'application/octet-stream', size: bytes.length, data: btoa(bin) } })
  }
  const swap = (x, seen = new Set()) => {
    if (!x || typeof x !== 'object') return x
    if (read.has(x)) return read.get(x)
    if (seen.has(x)) return null
    seen.add(x)
    if (Array.isArray(x)) return x.map((y) => swap(y, seen))
    const o = {}
    for (const k in x) o[k] = swap(x[k], seen)
    return o
  }
  return encode(swap(v))
}

/** turns {$gofn} into callables and {$fn} back into the JS functions they name */
function decode(v) {
  if (v === null || typeof v !== 'object') return v
  if (Array.isArray(v)) return v.map(decode)
  if (typeof v.$gofn === 'number' && Object.keys(v).length === 1) return goFn(v.$gofn)
  if (typeof v.$fn === 'number' && Object.keys(v).length === 1) return jsFns.get(v.$fn) ?? (() => {})
  // a file from Go (composer attachments): a real File, which plugins like AnonymizeFilenames rebuild
  if (v.$file && typeof v.$file === 'object' && typeof File !== 'undefined') {
    const f = v.$file
    const bin = atob(f.data || '')
    const bytes = new Uint8Array(bin.length)
    for (let i = 0; i < bin.length; i++) bytes[i] = bin.charCodeAt(i)
    return new File([bytes], f.name || 'file', { type: f.type || '' })
  }
  // a native sub-part Go passes as a child or prop: a patchable Slack component carrying its slot id
  if (v.$slot !== undefined && typeof v.n === 'string') return G.React.createElement(slackComponent(v.n), { ...decode(v.p || {}), $slot: v.$slot, key: v.k ?? v.$slot })
  const out = {}
  for (const k in v) out[k] = k === 'delta' ? reviveDelta(decode(v[k])) : decode(v[k])
  return out
}
/**
 * Slack hands composer deltas around as Quill Delta instances, and plugins
 * build on them (`new delta.constructor().insert('/channel ')` in
 * NativeAtChannel, `new (delta.constructor)(ops)` in bChannel), so a
 * `delta` coming from Go becomes the runtime's Delta (05_slack.js).
 */
function reviveDelta(d) {
  const D = T.Delta
  return D && d && typeof d === 'object' && Array.isArray(d.ops) && !(d instanceof D) ? new D(d.ops) : d
}

/** React elements inside payloads (menu labels, toast content) are kept in JS and rendered on request */
const elements = new Map()
let nextElementId = 1
function elementRef(el) {
  const id = FN_BASE + nextElementId++
  elements.set(id, el)
  return id
}
T.takeElement = (id) => elements.get(id)
T.releaseElements = (ids) => {
  for (const id of ids || []) elements.delete(id)
}

Object.assign(T, { deepEqual, within, retry, Store, encode, decode })
