// The Redux mirror: a Slack-shaped store whose slices are served by Go
// (internal/taut/mirror.go, built from internal/core) and cached here until Go
// invalidates them, plus Taut's read-time patches (patchState, patchSlice,
// mapEntries), thunks (patchThunk, dispatchThunk, waitForThunkCreator) and
// the React hooks (useReduxState, usePatchVersion, StateView).

// ---- slices served by Go ----

const OBJ_KEYS = new Set(['toString', 'valueOf', 'hasOwnProperty', 'isPrototypeOf', 'propertyIsEnumerable', 'toLocaleString', 'constructor', '__proto__'])

/** an id-keyed slice whose entries are fetched from Go on first read */
class LazyMap {
  constructor(name) {
    this.name = name
    this.entries = new Map() // key → value, or null when Go has none
    this.keys = null
    this.target = {}
    this.overrides = new Map()
    const self = this
    this.handler = {
      get(_t, key) {
        if (typeof key === 'symbol') return undefined
        if (OBJ_KEYS.has(key)) return key === '__proto__' ? Object.prototype : Object.prototype[key]
        const v = self.entry(key)
        return v === null ? undefined : v
      },
      has(_t, key) {
        if (typeof key === 'symbol') return false
        return self.entry(key) !== null
      },
      ownKeys() {
        return self.allKeys()
      },
      getOwnPropertyDescriptor(_t, key) {
        if (typeof key === 'symbol') return undefined
        const v = self.entry(key)
        return v === null ? undefined : { value: v, writable: true, enumerable: true, configurable: true }
      },
      set(_t, key, value) {
        self.overrides.set(String(key), value)
        self.entries.set(String(key), value)
        return true
      },
      defineProperty(_t, key, desc) {
        self.overrides.set(String(key), desc.value)
        self.entries.set(String(key), desc.value)
        return true
      },
      deleteProperty(_t, key) {
        self.overrides.set(String(key), null)
        self.entries.set(String(key), null)
        return true
      },
      // Slack's store keeps a slice's real keys on its prototype too, and
      // plugins scan them there (LastSeen: Object.keys(Object.getPrototypeOf(messages)))
      getPrototypeOf() {
        return self.protoView()
      },
    }
    this.proxy = new Proxy(this.target, this.handler)
  }
  entry(key) {
    if (this.entries.has(key)) return this.entries.get(key)
    let v = null
    try {
      v = go('get', this.name, key) ?? null
    } catch (err) {
      error(`reading ${this.name}.${key} failed`, err)
    }
    if (v !== null && typeof v === 'object') v = harden(v)
    // a channel's messages keep their timestamps on the prototype too, like Slack's
    if (this.name === 'messages' && v !== null && typeof v === 'object' && !Array.isArray(v)) v = withKeyProto(v)
    this.entries.set(key, v)
    return v
  }
  allKeys() {
    if (!this.keys) {
      let ks = []
      try {
        ks = go('keys', this.name) || []
      } catch (err) {
        error(`listing ${this.name} failed`, err)
      }
      const set = new Set(ks.map(String))
      for (const [k, v] of this.overrides) {
        if (v === null) set.delete(k)
        else set.add(k)
      }
      this.keys = [...set]
    }
    return this.keys
  }
  /** an object whose own (lazy) properties are the slice's entries, served as its prototype */
  protoView() {
    const keys = this.allKeys()
    if (this.proto && this.protoKeys === keys) return this.proto
    const proto = Object.create(Object.prototype)
    for (const k of keys) {
      Object.defineProperty(proto, k, { get: () => this.handler.get(this.target, k), enumerable: true, configurable: true })
    }
    this.proto, this.protoKeys = proto, keys
    return proto
  }
  /** drops `keys` (or everything) so they're read again, and gives the slice a new identity */
  invalidate(keys) {
    if (!keys) {
      this.entries = new Map(this.overrides)
      this.keys = null
    } else {
      for (const k of keys) {
        if (this.overrides.has(k)) continue
        this.entries.delete(k)
      }
      this.keys = null
    }
    this.proxy = new Proxy(this.target, this.handler)
  }
}

/** o with a prototype holding the same entries (Slack's nested slices look like this) */
function withKeyProto(o) {
  if (Object.getPrototypeOf(o) !== Object.prototype) return o
  const proto = Object.create(Object.prototype)
  for (const k of Object.keys(o)) proto[k] = o[k]
  try {
    Object.setPrototypeOf(o, proto)
  } catch {}
  return o
}

/** entries are shared by every reader, so they're frozen like Slack's immutable state */
function harden(v) {
  return v
}

const lazyMaps = new Map()
const wholeSlices = new Map() // name → value (undefined = not fetched)
const localSlices = new Map() // JS-owned slices (modal, toast, plugin-dispatched actions)
let sliceSpec = null

function spec() {
  if (!sliceSpec) {
    try {
      sliceSpec = go('slices') || { maps: [], wholes: [] }
    } catch (err) {
      error('could not read the slice list', err)
      sliceSpec = { maps: [], wholes: [] }
    }
    for (const n of sliceSpec.maps) if (!lazyMaps.has(n)) lazyMaps.set(n, new LazyMap(n))
  }
  return sliceSpec
}

function wholeSlice(name) {
  if (wholeSlices.has(name)) return wholeSlices.get(name)
  let v
  try {
    v = go('get', name, null)
  } catch (err) {
    error(`reading ${name} failed`, err)
  }
  if (v === null) v = undefined
  wholeSlices.set(name, v)
  return v
}

let rawState = null
function buildRaw() {
  const s = spec()
  const root = {}
  for (const n of s.maps) {
    const m = lazyMaps.get(n)
    Object.defineProperty(root, n, { get: () => m.proxy, enumerable: true, configurable: true })
  }
  for (const n of s.wholes) {
    Object.defineProperty(root, n, { get: () => wholeSlice(n), enumerable: true, configurable: true })
  }
  for (const [n, v] of localSlices) {
    Object.defineProperty(root, n, { value: v, enumerable: true, configurable: true, writable: true })
  }
  return root
}
function getRawState() {
  if (!rawState) rawState = buildRaw()
  return rawState
}

/** Go → JS: [{slice, keys?}] changed */
T.invalidate = (changes) => {
  spec()
  let experiments = false
  for (const c of changes || []) {
    const m = lazyMaps.get(c.slice)
    if (m) m.invalidate(c.keys && c.keys.length ? c.keys.map(String) : null)
    else wholeSlices.delete(c.slice)
    experiments ||= c.slice === 'experiments'
  }
  rawState = null
  notifyStore()
  if (experiments) tellGoExperiments() // the server's groups moved under the patches
}

function setLocalSlice(name, value) {
  localSlices.set(name, value)
  rawState = null
  notifyStore()
}

// ---- the store ----

const storeListeners = new Set()
const notifyStore = batcher(() => {
  for (const l of [...storeListeners]) {
    try {
      l()
    } catch (err) {
      error('store listener threw', err)
    }
  }
  for (const w of [...stateWaiters]) w()
})

const actionHandlers = new Map()
/** handles plain actions dispatched to the mirror, `type` → (action) → result */
function onAction(type, fn) {
  actionHandlers.set(type, fn)
}

function dispatch(action) {
  if (typeof action === 'function') return action(dispatch, getState, undefined)
  if (!action || typeof action !== 'object') return action
  const h = actionHandlers.get(action.type)
  if (h) return h(action)
  if (typeof action.type === 'string' && !action.type.startsWith('@@')) {
    try {
      goOr(null, 'action', encode(action))
    } catch (err) {
      warn('action failed', action.type, err)
    }
  }
  notifyStore()
  return action
}

function getState() {
  const raw = getRawState()
  if (statePatches.size === 0) return raw
  if (raw === cachedRaw && cachedVersion === patchVersion) return cachedOut
  let out = raw
  for (const p of statePatches) out = guarded(p, out, () => p(out))
  cachedRaw = raw
  cachedVersion = patchVersion
  cachedOut = out
  return out
}
let cachedRaw, cachedOut
let cachedVersion = -1

function subscribe(listener) {
  const l = () => listener()
  storeListeners.add(l)
  return () => storeListeners.delete(l)
}

const store = { getState, dispatch, subscribe, subscribeImmediate: subscribe, replaceReducer() {} }
store.getState.__tautRawGetState = getRawState
const getStore = () => store
const waitForStore = () => Promise.resolve(store)

const stateWaiters = new Set()
function waitForState(read, signal) {
  return new Promise((resolve, reject) => {
    const done = (settle) => {
      stateWaiters.delete(check)
      signal?.removeEventListener?.('abort', check)
      settle()
    }
    const check = () => {
      if (signal?.aborted) return done(() => reject(signal.reason))
      let v
      try {
        v = read()
      } catch (err) {
        return done(() => reject(err))
      }
      if (v !== undefined) done(() => resolve(v))
    }
    stateWaiters.add(check)
    signal?.addEventListener?.('abort', check)
    check()
  })
}

// ---- read-time patches ----

const statePatches = new Set()
let patchVersion = 0
const patchListeners = new Set()
const failed = new WeakSet()
let running

function guarded(owner, fallback, run) {
  if (failed.has(owner)) return fallback
  const outer = running
  running = owner
  try {
    return run()
  } catch (err) {
    failed.add(owner)
    error('A redux patch threw and was switched off', err)
    queueMicrotask(refresh)
    return fallback
  } finally {
    running = outer
  }
}

const tellGoPatches = batcher(() => {
  goOr(null, 'statePatched', { version: patchVersion, all: statePatches.size > (sliceLayers.size ? 1 : 0), slices: [...sliceLayers.keys()] })
})

// The groups the patches force on Slack's experiments slice (any plugin's:
// Slick's Experiments patches it, Taut's api.experiments does too). Go
// forces them in core, so Clack's own experiment checks read them
// (taut/experiments.go); null = reads as not assigned.
let sentExperiments = '{}'
const tellGoExperiments = batcher(() => {
  const forced = {}
  if (statePatches.size) {
    const raw = getRawState()?.experiments ?? {}
    const view = getState()?.experiments ?? {}
    if (view !== raw) {
      for (const name of new Set([...Object.keys(raw), ...Object.keys(view)])) {
        const was = raw[name]?.group ?? null
        const now = view[name]?.group ?? null
        if (was !== now) forced[name] = now
      }
    }
  }
  const s = JSON.stringify(forced)
  if (s === sentExperiments) return
  sentExperiments = s
  goOr(null, 'experiments', forced)
})

function refresh() {
  patchVersion++
  try {
    dispatch({ type: '@@taut/PATCH_STATE' })
  } catch {}
  for (const l of [...patchListeners]) {
    try {
      l()
    } catch {}
  }
  tellGoPatches()
  tellGoExperiments()
}

function patchState(patch) {
  statePatches.add(patch)
  failed.delete(patch)
  refresh()
  return () => {
    statePatches.delete(patch)
    refresh()
  }
}

const hasOwn = (o, k) => typeof k !== 'symbol' && Object.prototype.hasOwnProperty.call(o, k)
const memos = new WeakMap()
function memoFor(fn) {
  let m = memos.get(fn)
  if (!m) {
    m = { cache: new Map(), added: new Set(), version: -1 }
    memos.set(fn, m)
  }
  return m
}

/** a view of an id-keyed object reading each entry through `mapEntry(key, entry)` */
function mapEntries(object, mapEntry, addedKeys) {
  const memo = memoFor(mapEntry)
  const owner = running ?? mapEntry
  const sync = () => {
    if (memo.version === patchVersion) return
    memo.cache = new Map()
    memo.added = new Set(addedKeys ? [...guarded(owner, [], addedKeys)].map(String) : [])
    memo.version = patchVersion
  }
  const run = (key, value) => {
    if (typeof key !== 'string') return value
    sync()
    const hit = memo.cache.get(key)
    if (hit && hit.input === value) return hit.output
    const output = guarded(owner, value, () => mapEntry(key, value))
    memo.cache.set(key, { input: value, output })
    return output
  }
  const describe = (target, key) => {
    const d = Reflect.getOwnPropertyDescriptor(target, key)
    if (d) {
      if (!('value' in d) || d.configurable === false) return d
      return { ...d, value: run(key, d.value) }
    }
    sync()
    if (typeof key === 'string' && memo.added.has(key) && Object.isExtensible(target)) {
      return { value: run(key, undefined), enumerable: true, configurable: true, writable: true }
    }
    return undefined
  }
  const ownKeys = (target) => {
    const ks = Reflect.ownKeys(target)
    if (!addedKeys || !Object.isExtensible(target)) return ks
    sync()
    const extra = [...memo.added].filter((k) => !ks.includes(k))
    return extra.length ? [...ks, ...extra] : ks
  }
  const protoProxies = new WeakMap()
  const proxyProto = (proto) => {
    let p = protoProxies.get(proto)
    if (!p) {
      p = new Proxy(proto, { get: (t, k) => run(k, t[k]), getOwnPropertyDescriptor: describe, ownKeys })
      protoProxies.set(proto, p)
    }
    return p
  }
  return new Proxy(object, {
    get(target, key) {
      if (typeof key === 'string' && !(key in target)) {
        sync()
        if (memo.added.has(key)) return run(key, undefined)
      }
      return run(key, target[key])
    },
    has(target, key) {
      if (key in target) return true
      sync()
      return typeof key === 'string' && memo.added.has(key)
    },
    getOwnPropertyDescriptor: describe,
    ownKeys,
    getPrototypeOf(target) {
      const proto = Object.getPrototypeOf(target)
      if (!proto || typeof proto !== 'object' || proto === Object.prototype) return proto
      return proxyProto(proto)
    },
  })
}

const sliceLayers = new Map()
let unpatchSlices

function composeView(patches) {
  return {
    mapEntry: (key, entry) => {
      let out = entry
      for (const p of patches) out = guarded(p, out, () => p.mapEntry(key, out))
      return out
    },
    addedKeys: patches.some((p) => p.addedKeys) ? () => patches.flatMap((p) => (p.addedKeys ? [...guarded(p, [], p.addedKeys)] : [])) : undefined,
    version: -1,
  }
}

function readView(view, slice) {
  if (!view.proxy || view.raw !== slice || view.version !== patchVersion) {
    view.raw = slice
    view.version = patchVersion
    view.proxy = mapEntries(slice, view.mapEntry, view.addedKeys)
  }
  return view.proxy
}

function patchSlices(state) {
  let out = state
  for (const [name, layer] of sliceLayers) {
    const slice = state?.[name]
    if (!slice || typeof slice !== 'object') continue
    if (out === state) out = { ...state }
    out[name] = readView(layer.all, slice)
  }
  return out
}

function patchSlice(sliceName, mapEntry, options) {
  const addedKeys = typeof options === 'function' ? options : options?.addedKeys
  const patch = { mapEntry, addedKeys }
  let layer = sliceLayers.get(sliceName)
  if (!layer) {
    layer = { patches: [], all: composeView([]) }
    sliceLayers.set(sliceName, layer)
  }
  layer.patches.push(patch)
  layer.all = composeView(layer.patches)
  if (unpatchSlices) refresh()
  else unpatchSlices = patchState(patchSlices)
  let disposed = false
  return () => {
    if (disposed) return
    disposed = true
    const cur = sliceLayers.get(sliceName)
    if (!cur) return
    cur.patches = cur.patches.filter((p) => p !== patch)
    if (cur.patches.length) cur.all = composeView(cur.patches)
    else sliceLayers.delete(sliceName)
    if (sliceLayers.size || !unpatchSlices) return refresh()
    const u = unpatchSlices
    unpatchSlices = undefined
    u()
  }
}

/** Go → JS: the patched value of `slice[key]` (or the slice) for the native UI */
T.view = (slice, key) => {
  const s = getState()[slice]
  if (key === null || key === undefined) return encode(s)
  if (!s || typeof s !== 'object') return null
  return encode(s[key])
}
/** Go → JS: several at once, [[slice, key], …] → values */
T.views = (pairs) => {
  const st = getState()
  return pairs.map(([slice, key]) => {
    const s = st[slice]
    if (key === null || key === undefined) return encode(s)
    return s && typeof s === 'object' ? encode(s[key]) ?? null : null
  })
}

// ---- thunks ----

const thunkWraps = new Set()
const thunkCreators = new Map()
const thunkWaiters = new Map()

function wrapCreator(original) {
  return new Proxy(original, {
    apply(target, thisArg, args) {
      let creator = target
      for (const { match, wrap } of thunkWraps) {
        let ok = false
        try {
          ok = match(target)
        } catch {}
        if (!ok) continue
        try {
          creator = wrap(creator)
        } catch (err) {
          error('a thunk patch threw', err)
        }
      }
      return Reflect.apply(creator, thisArg, args)
    },
  })
}

/** defines Slack thunk `name`; `impl(args, dispatch, getState)` runs when the thunk does */
function defineThunk(name, impl) {
  if (thunkCreators.has(name)) return thunkCreators.get(name)
  const base = withMeta(name, (...args) => (d, gs) => impl(args, d || dispatch, gs || getState))
  const creator = wrapCreator(base)
  thunkCreators.set(name, creator)
  defineModule(`thunk:${name}`, { [name]: creator })
  const waiting = thunkWaiters.get(name)
  if (waiting) {
    thunkWaiters.delete(name)
    for (const r of waiting) r(creator)
  }
  tellGoThunks()
  return creator
}

/** native thunks whose Go work changes slices: read again before the thunk resolves, so callers see the change */
const THUNK_CHANGES = {
  maybePersistPendingFileData: ['pendingFileUploads', 'files'],
  editFileApi: ['pendingFileUploads', 'files'],
}

/** a thunk whose work is Clack's native implementation, run in Go */
function nativeThunk(name) {
  const changes = THUNK_CHANGES[name]
  if (!changes) return defineThunk(name, async (args) => decode(await go('thunk', name, await encodeAsync(args))))
  return defineThunk(name, async (args) => {
    try {
      return decode(await go('thunk', name, await encodeAsync(args)))
    } finally {
      T.invalidate(changes.map((slice) => ({ slice })))
    }
  })
}

const getThunkCreator = (name) => thunkCreators.get(name)
function waitForThunkCreator(name) {
  const known = thunkCreators.get(name)
  if (known) return Promise.resolve(known)
  return new Promise((resolve) => {
    let w = thunkWaiters.get(name)
    if (!w) thunkWaiters.set(name, (w = new Set()))
    w.add(resolve)
  })
}
/** waitForThunkCreator that rejects with `message` after `ms` and drops its waiter (Slick gives up after 30 s; Taut waits) */
function waitForThunkWithin(name, ms, message) {
  const known = thunkCreators.get(name)
  if (known) return Promise.resolve(known)
  return new Promise((resolve, reject) => {
    let w = thunkWaiters.get(name)
    if (!w) thunkWaiters.set(name, (w = new Set()))
    const waiters = w
    const waiter = (creator) => {
      clearTimeout(timer)
      resolve(creator)
    }
    const timer = setTimeout(() => {
      waiters.delete(waiter)
      if (!waiters.size && thunkWaiters.get(name) === waiters) thunkWaiters.delete(name)
      reject(new Error(message))
    }, ms)
    waiters.add(waiter)
  })
}
async function dispatchThunk(name, ...args) {
  const creator = await waitForThunkCreator(name)
  return dispatch(creator(...args))
}

const thunkMatchNames = new WeakMap()
function patchThunk(match, wrap) {
  const matcher = typeof match === 'string' ? byMeta(match) : match
  if (typeof match === 'string') thunkMatchNames.set(matcher, match)
  const entry = { match: matcher, wrap }
  thunkWraps.add(entry)
  tellGoThunks()
  return () => {
    thunkWraps.delete(entry)
    tellGoThunks()
  }
}

/** which thunk names any patch matches, so Go only crosses into JS for those */
function patchedThunkNames() {
  const names = new Set()
  for (const { match } of thunkWraps) {
    const n = thunkMatchNames.get(match)
    if (n) names.add(n)
    for (const [name, creator] of thunkCreators) {
      try {
        if (match(creator)) names.add(name)
      } catch {}
    }
  }
  return [...names]
}
const tellGoThunks = batcher(() => goOr(null, 'patchedThunks', patchedThunkNames()))

/** Go → JS: runs thunk `name` through every patch, with Go's native work (callable `nativeId`) at the core */
T.dispatchThunk = async (name, args, nativeId, sync) => {
  const native = sync
    ? withMeta(name, (...a) => () => decode(go('callNativeSync', nativeId, encode(a))))
    : withMeta(name, (...a) => async () => decode(await go('callNative', nativeId, await encodeAsync(a))))
  let creator = native
  for (const { match, wrap } of thunkWraps) {
    let ok = false
    try {
      ok = match(native) || match(thunkCreators.get(name))
    } catch {}
    if (!ok) continue
    try {
      creator = wrap(creator)
    } catch (err) {
      error('a thunk patch threw', err)
    }
  }
  const result = await dispatch(creator(...decode(args || [])))
  return encode(await completeTier(result))
}

/**
 * Slack's autocomplete thunks return tiered results: the local rows, with the
 * complete tier as `.promise` (PrivateChannelMapper adds its rows there). Go
 * gets one answer and keeps its native rows meanwhile, so it gets the complete
 * tier, or the local rows when that tier fails or isn't a list.
 */
async function completeTier(result) {
  if (!Array.isArray(result) || typeof result.promise?.then !== 'function') return result
  try {
    const all = await result.promise
    return Array.isArray(all) ? all : result
  } catch (err) {
    warn('a thunk result tier failed', err)
    return result
  }
}

// ---- React hooks ----

function useReduxState(selector) {
  const React = G.React
  const ref = React.useRef(selector)
  ref.current = selector
  const getSnapshot = React.useCallback(() => ref.current(getState()), [])
  return React.useSyncExternalStore(subscribe, getSnapshot)
}

const subscribePatches = (fn) => {
  patchListeners.add(fn)
  return () => patchListeners.delete(fn)
}
const getPatchVersion = () => patchVersion
function usePatchVersion() {
  return G.React.useSyncExternalStore(subscribePatches, getPatchVersion)
}

/** state views are a context: Clack's own slice readers inside honour it, and native "@" nodes get its id */
let StateViewContext
const stateViews = new Map()
let nextStateView = 1
function StateView({ patch, children }) {
  const React = G.React
  const Ctx = stateViewContext()
  const parent = React.useContext(Ctx)
  const id = React.useMemo(() => nextStateView++, [])
  const view = React.useMemo(() => {
    let input, output
    return {
      id,
      parent,
      getState: () => {
        const st = parent ? parent.getState() : getState()
        if (st !== input) {
          input = st
          output = guarded(patch, st, () => patch(st))
        }
        return output
      },
    }
  }, [patch, parent])
  stateViews.set(id, view)
  React.useEffect(() => () => stateViews.delete(id), [id])
  return React.createElement(Ctx.Provider, { value: view }, children)
}
/** the context native components read to tell Go which StateView they render in */
function stateViewContext() {
  return (StateViewContext ??= G.React.createContext(null))
}
/** Go → JS: patched value inside a StateView (native nodes carry its id) */
T.viewIn = (viewId, slice, key) => {
  const v = stateViews.get(viewId)
  const st = v ? v.getState() : getState()
  const s = st[slice]
  if (key === null || key === undefined) return encode(s)
  return s && typeof s === 'object' ? encode(s[key]) ?? null : null
}

const redux = {
  getStore,
  waitForStore,
  getRawState,
  waitForState,
  useReduxState,
  usePatchVersion,
  StateView,
  patchState,
  patchSlice,
  mapEntries,
  patchThunk,
  getThunkCreator,
  waitForThunkCreator,
  dispatchThunk,
  refresh,
}

// Slack's store factories and the client store, as byName finds them
defineModule('redux-store', {
  getClientStoreInstance: named('getClientStoreInstance', () => store),
  tryGetClientStoreInstance: named('tryGetClientStoreInstance', () => store),
  getStoreInstanceByTeamId: named('getStoreInstanceByTeamId', () => store),
  getFocusedWorkspace: named('getFocusedWorkspace', () => getRawState().selfTeamIds?.teamId),
  createStore: named('createStore', () => store),
  legacy_createStore: named('legacy_createStore', () => store),
  exposeSelectorForDebug: named('exposeSelectorForDebug', (selector, name) => {
    nameSelector(selector, name)
    return selector
  }),
})

Object.assign(T, { redux, store, getState, getRawState, dispatch, defineThunk, nativeThunk, waitForThunkWithin, onAction, setLocalSlice, refresh })
