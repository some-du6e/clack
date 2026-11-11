// A plugin's this.api: Slick's SlickAPI over the TautAPI internals. Every
// registration goes through scope.track and is undone when the plugin stops;
// integrations a plugin didn't list in requiredAPIs throw when touched.

// ---- storage (Slick's blobStore(`plugin:<id>`) in Go: Taut's data store) ----

function blobStore(namespace) {
  return {
    list: async () => go('blob', 'list', namespace) || [],
    read: async (key) => go('blob', 'read', namespace, key) ?? null,
    readAll: async (prefix = '') => go('blob', 'readAll', namespace, prefix) || {},
    write: async (key, value) => !!go('blob', 'write', namespace, key, value),
    delete: async (key) => !!go('blob', 'delete', namespace, key),
    clear: async () => !!go('blob', 'clear', namespace),
  }
}

class ScopedStorage {
  #blob
  constructor(blob) {
    this.#blob = blob
  }
  keys() {
    return this.#blob.list().catch(() => [])
  }
  async entries(prefix = '') {
    const raw = await this.#blob.readAll(prefix).catch(() => ({}))
    const out = new Map()
    for (const [key, value] of Object.entries(raw)) {
      try {
        out.set(key, JSON.parse(value))
      } catch {}
    }
    return out
  }
  async get(key, fallback) {
    const raw = await this.#blob.read(key).catch(() => null)
    if (raw === null) return fallback
    try {
      return JSON.parse(raw)
    } catch {
      return fallback
    }
  }
  set(key, value) {
    let json
    try {
      json = JSON.stringify(value)
    } catch (err) {
      console.error('[slick] value is not serializable:', err)
      return Promise.resolve(false)
    }
    if (json === undefined) return Promise.resolve(false)
    return this.#blob.write(key, json).catch(() => false)
  }
  delete(key) {
    return this.#blob.delete(key).catch(() => false)
  }
  clear() {
    return this.#blob.clear().catch(() => false)
  }
}

/** Slick's TTL cache over plugin storage, de-duplicating in-flight fetches */
class Cache {
  #memory = new Map()
  #inflight = new Map()
  #swept = false
  #sinceSweep = 0
  constructor(storage, name, ttlMs = 60 * 60 * 1000, maxEntries = 5000) {
    this.storage = storage
    this.name = name
    this.ttlMs = ttlMs
    this.maxEntries = maxEntries
  }
  #key(key) {
    return `cache:${this.name}:${key}`
  }
  #prune() {
    if (++this.#sinceSweep < 512) return
    this.#sinceSweep = 0
    const now = Date.now()
    for (const [key, entry] of this.#memory) if (entry.expires <= now) this.#memory.delete(key)
  }
  #remember(key, entry) {
    this.#memory.set(key, entry)
    for (const oldest of this.#memory.keys()) {
      if (this.#memory.size <= this.maxEntries) break
      this.#memory.delete(oldest)
    }
  }
  #sweepOnce() {
    if (this.#swept) return
    this.#swept = true
    const prefix = `cache:${this.name}:`
    void this.storage
      .entries(prefix)
      .then((stored) => {
        const now = Date.now()
        for (const [key, entry] of stored) {
          if (entry && typeof entry.expires === 'number' && entry.expires > now) continue
          void this.storage.delete(key)
        }
      })
      .catch((err) => console.error(`[slick] cache sweep failed (${this.name}):`, err))
  }
  async get(key, fetcher) {
    const now = Date.now()
    this.#sweepOnce()
    this.#prune()
    const hit = this.#memory.get(key)
    if (hit && hit.expires > now) return hit.value
    const pending = this.#inflight.get(key)
    if (pending) return pending
    const load = (async () => {
      const stored = await this.storage.get(this.#key(key), null)
      if (stored && stored.expires > now) {
        this.#remember(key, stored)
        return stored.value
      }
      const value = await fetcher(key)
      const ttl = typeof this.ttlMs === 'function' ? this.ttlMs(value) : this.ttlMs
      const entry = { value, expires: Date.now() + ttl }
      this.#remember(key, entry)
      void this.storage.set(this.#key(key), entry)
      return value
    })()
    this.#inflight.set(key, load)
    try {
      return await load
    } finally {
      this.#inflight.delete(key)
    }
  }
  peek(key) {
    const hit = this.#memory.get(key)
    return hit && hit.expires > Date.now() ? hit.value : undefined
  }
  forget(key) {
    this.#memory.delete(key)
    void this.storage.delete(this.#key(key))
  }
}

// Slick's stored-file format (browser file settings): file:<setting> meta and base64 chunks
const STORED_PREFIX = 'stored:'
async function readStoredFile(blob, setting) {
  let meta
  try {
    meta = JSON.parse((await blob.read(`file:${setting}`)) ?? 'null')
  } catch {
    meta = null
  }
  if (!Number.isInteger(meta?.chunks) || typeof meta.type !== 'string') return null
  const parts = []
  for (let i = 0; i < meta.chunks; i++) {
    const part = await blob.read(`file:${setting}:${i}`)
    if (part === null) return null
    const bin = atob(part)
    const bytes = new Uint8Array(bin.length)
    for (let j = 0; j < bin.length; j++) bytes[j] = bin.charCodeAt(j)
    parts.push(bytes)
  }
  return new Blob(parts, { type: meta.type })
}

// ---- integrations ----

const getMessageBotId = (msg) => msg?.bot_id ?? msg?.bot_profile?.id ?? msg?.slick_bot_id
const getRawMessage = (channel, ts) => T.getRawState()?.messages?.[channel]?.[ts]
const asRawMessage = (msg) => (typeof msg?.channel === 'string' && msg.ts && getRawMessage(msg.channel, msg.ts)) || msg

function modifyMessageObject(message, edits) {
  const next = { ...message }
  if (edits?.sentBy === undefined) return next
  next.user = edits.sentBy
  const bot = getMessageBotId(message)
  if (bot) next.slick_bot_id = bot
  for (const key of ['bot_id', 'app_id', 'username', 'icons', 'bot_profile', 'display_as_bot']) delete next[key]
  if (next.subtype === 'bot_message') delete next.subtype
  return next
}

function useMessageBot(msg) {
  const bot = T.redux.useReduxState((state) => {
    const raw = asRawMessage(msg)
    const botId = getMessageBotId(raw)
    return botId ? (state.bots?.[botId] ?? raw?.bot_profile ?? botId) : undefined
  })
  return typeof bot === 'string' ? { id: bot } : bot
}

const messageDiagnostics = { historyShapeFailures: 0 }

const messagesBase = {
  getRawMessage,
  asRawMessage,
  getMessageBotId,
  injectMessages: T.messages.injectMessages,
  diagnostics: messageDiagnostics,
  modifyMessageObject,
  useActivityMessage: T.messages.useActivityMessage,
  useMessageBot,
}

const channelsBase = {
  getCachedChannel: (id) => T.getState().channels?.[id],
  getCurrentChannelId: () => G.location?.pathname?.match(/^\/client\/[^/]+\/([CDG][A-Z0-9]+)/)?.[1],
  makeChannelObject: T.channels.makeChannelObject,
}

const membersBase = {
  getCachedMember: T.members.getCachedMember,
  getCurrentMemberId: T.members.getCurrentMemberId,
  getMember: T.members.getMember,
  useMember: T.members.useMember,
  modifyMemberObject: T.members.modifyMemberObject,
}

const toDelta = (content) => (Array.isArray(content) ? new T.Delta(content) : content instanceof T.Delta ? content : new T.Delta(content?.ops ?? []))
const blocksBase = {
  makeDelta: async (ops) => new T.Delta(ops),
  fromDelta: async (content, options) => T.deltaToBlocks(toDelta(content), options),
  toPlainText: (blocks) => TA.blocks.toPlainText(blocks),
  toMarkdown: (blocks) => TA.blocks.toMarkdown(blocks),
}

let elementsBase
function elements() {
  if (!elementsBase) {
    const taut = T.elementsApi()
    elementsBase = {}
    for (const name of ['SvgIcon', 'Avatar', 'ProfileHoverTrigger', 'MrkdwnElement', 'Button', 'Tooltip', 'IconButtonBase', 'ConfirmationModal', 'InlineAlert', 'Label', 'FormTextInput', 'FieldSet', 'Legend', 'Hint', 'BasicSelect', 'Blocks', 'MenuTrigger', 'MenuFromTemplate']) {
      elementsBase[name] = taut[name]
    }
    elementsBase.MemberLink = T.lazyComponent('MemberLink')
  }
  return elementsBase
}

// onMessageSendDelta: Slack hands the composer's Delta to the send; Clack's
// composer sends Block Kit, so the transforms run over the Delta of the
// blocks, and only a changed Delta is turned back into blocks.
const deltaTransforms = new Map()
let unhookDelta

function isUsableDelta(value, input) {
  if (!value || typeof value !== 'object' || !Array.isArray(value.ops)) return false
  const InputDelta = input.constructor
  if (typeof InputDelta !== 'function' || !(value instanceof InputDelta)) return false
  return value.ops.every((op) => {
    if (!op || typeof op !== 'object' || Array.isArray(op)) return false
    const kinds = ['insert', 'delete', 'retain'].filter((k) => k in op)
    if (kinds.length !== 1) return false
    if ('insert' in op && typeof op.insert !== 'string' && (typeof op.insert !== 'object' || !op.insert)) return false
    if ('delete' in op && (typeof op.delete !== 'number' || !Number.isFinite(op.delete) || op.delete < 0)) return false
    if ('retain' in op && (typeof op.retain !== 'number' || !Number.isFinite(op.retain) || op.retain < 0)) return false
    return !('attributes' in op) || (typeof op.attributes === 'object' && op.attributes !== null)
  })
}

const label = (fn) => fn?.name || 'anonymous'

function sendThroughDeltas(blocks) {
  if (!deltaTransforms.size || !Array.isArray(blocks)) return blocks
  const input = T.blocksToDelta(blocks)
  // transforms may edit the Delta in place (ClearURLs does) or return a new one
  const before = JSON.stringify(input.ops)
  let result = input
  for (const transform of [...deltaTransforms.keys()]) {
    const current = result
    try {
      const next = transform(current)
      if (isUsableDelta(next, current)) result = next
      else console.error(`[slick] message transform ${label(transform)} returned an invalid Delta; ignoring it`)
    } catch (err) {
      console.error(`[slick] message transform ${label(transform)} failed; ignoring it:`, err)
    }
  }
  if (JSON.stringify(result.ops) === before) return blocks
  return T.deltaToBlocks(result)
}

function onMessageSendDelta(transform) {
  deltaTransforms.set(transform, true)
  unhookDelta ??= TA.onMessageSendBlocks(sendThroughDeltas)
  return () => {
    deltaTransforms.delete(transform)
    if (!deltaTransforms.size && unhookDelta) {
      const u = unhookDelta
      unhookDelta = undefined
      u()
    }
  }
}

// addSendCheck(check(blocks, {channelId, threadTs})): Taut's send checks have
// the same signature (the composer's outgoing blocks and context).
function addSendCheck(check) {
  return TA.composer.addSendCheck(async (blocks, context) => {
    try {
      return (await check(Array.isArray(blocks) ? blocks : [], context ?? {})) !== false
    } catch (err) {
      console.error(`[slick] send check ${label(check)} failed; ignoring it:`, err)
      return true
    }
  })
}

// RTM: Slick emits each event under its type and its subtype
function onRtm(key, listener) {
  const offType = TA.rtm.on(key, listener)
  if (key === '*' || key === 'message') return offType
  const offSub = TA.rtm.on('message', (ev) => {
    if (ev?.subtype === key && ev.type !== key) listener(ev)
  })
  return () => {
    offType()
    offSub()
  }
}

// pop-out documents: Clack has none, so listeners are kept and never called
const documentListeners = new Set()
function onDocument(cb) {
  documentListeners.add(cb)
  return () => void documentListeners.delete(cb)
}

/** api.fetch: the privileged bridge fetch ({status, body}, no CORS) */
async function bridgeFetch(url, init) {
  const res = await G.fetch(url, init)
  return { status: res.status, body: await res.text() }
}

// Slick's redux.ts: a thunk Slack never defines rejects after THUNK_WAIT_MS instead of waiting forever
S.thunkWaitMs = 30_000
const waitForThunkCreator = (name) =>
  T.waitForThunkWithin(name, S.thunkWaitMs, `[slick] timed out waiting for Slack thunk: ${name}`)
async function dispatchThunk(name, ...args) {
  const creator = await waitForThunkCreator(name)
  return T.dispatch(creator(...args))
}

function createScopedAPI(entry, id, scope) {
  const tracked =
    (fn) =>
    (...args) =>
      scope.track(fn(...args))
  const allowed = new Set(entry.Class.requiredAPIs ?? PLUGIN_APIS)
  const storage = new ScopedStorage(blobStore(`plugin:${id}`))
  const fileBlob = blobStore(`plugin:${id}`)
  const integrations = {
    redux: () => ({ ...TA.redux, waitForThunkCreator, dispatchThunk, patchState: tracked(TA.redux.patchState), patchSlice: tracked(TA.redux.patchSlice), patchThunk: tracked(TA.redux.patchThunk) }),
    rtm: () => ({ on: tracked(onRtm) }),
    messages: () => ({ ...messagesBase, injectMessages: tracked(messagesBase.injectMessages) }),
    members: () => membersBase,
    channels: () => channelsBase,
    blocks: () => blocksBase,
    files: () => ({ upload: TA.files.upload }),
    elements: () => elements(),
    menu: () => ({ Menu: T.menuApi().Menu }),
    modal: () => {
      const m = T.modalApi()
      return { openModal: m.openModal, confirm: m.confirm, alert: m.alert }
    },
    react: () => G.React,
    onMessageSendDelta: () => tracked(onMessageSendDelta),
    onMessageSendBlocks: () => tracked(TA.onMessageSendBlocks),
    addSendCheck: () => tracked(addSendCheck),
  }
  const made = new Map()
  const integration = (name) => {
    if (!allowed.has(name)) throw new Error(`Plugin must declare requiredAPIs: ${name}`)
    if (!made.has(name)) made.set(name, integrations[name]())
    return made.get(name)
  }
  const api = {
    getExport: T.getExport,
    getByProps: T.getByProps,
    waitForExport: T.waitForExport,
    getComponent: T.getComponent,
    waitForComponent: T.waitForComponent,
    getRenderedComponent: T.getRenderedComponent,
    waitForRenderedComponent: T.waitForRenderedComponent,
    getComponentSource: T.getComponentSource,
    getValueSource: T.getValueSource,
    findModuleId: T.findModuleId,
    moduleSources: T.getModuleSources,
    getFiberFromNode: () => null,
    Store: TA.Store,
    fetch: bridgeFetch,
    loader: 'electron',
    userAPI: T.userAPI,
    id,
    signal: scope.signal,
    isActive: () => !scope.signal.aborted && plugins.get(id)?.scope === scope,
    patchComponent: tracked(T.patchComponent),
    onDocument: tracked(onDocument),
    deferResizeWork: tracked(deferResizeWork),
    setStyle: tracked((css, key = 'default') => TA.setStyle(css, `plugin:${id}:${key}`)),
    storage,
    storedFileUrl: async (setting) => {
      let file = await readStoredFile(fileBlob, setting).catch(() => null)
      if (!file) {
        // desktop file settings hold a path: Clack reads it (Slick's main half serves it)
        const read = await go('readFile', id, setting)
        if (read) file = new Blob([Uint8Array.from(atob(read.data), (c) => c.charCodeAt(0))], { type: read.type })
      }
      if (!file || scope.signal.aborted) return null
      const url = URL.createObjectURL(file)
      scope.track(() => URL.revokeObjectURL(url))
      return url
    },
    Cache: (name, ttlMs, maxEntries) => new Cache(storage, name, ttlMs, maxEntries),
    settings: {
      set: (key, value) => go('setSetting', id, key, value === undefined ? null : value),
      addTab: tracked((tab) => TA.preferences.addTab({ ...tab, id: `slick-plugin-${id}` })),
    },
    main: {
      call: (method, ...args) => callMain(id, method, args),
      on: tracked((event, cb) => onMainEvent(id, event, cb)),
    },
  }
  for (const name of PLUGIN_APIS) {
    Object.defineProperty(api, name, { enumerable: true, get: () => integration(name) })
  }
  return api
}

// ---- deferResizeWork (src/app/api/resize.ts) ----
// Taut's gate holds the window's resize events while a window edge is
// dragged and replays one when it has been still for quietMs. Slick's also
// holds ResizeObserver callbacks over the same hold: the latest entry per
// target is kept and delivered when it ends (a delivered notification is
// never sent again). Observers made after the prelude get the hold, as on
// Slack's page, where Slick installs it before Slack's bundle.
let resizeHolding = false
const heldObservers = new Set()
function releaseObservers() {
  const pending = [...heldObservers]
  heldObservers.clear()
  for (const held of pending) {
    const entries = [...held.entries.values()]
    held.entries.clear()
    if (!entries.length) continue
    try {
      held.callback(entries, held.observer)
    } catch (err) {
      console.error('[slick] held ResizeObserver callback threw:', err)
    }
  }
}
if (typeof G.ResizeObserver === 'function') {
  const Native = G.ResizeObserver
  class SlickResizeObserver extends Native {
    constructor(callback) {
      let held
      super((entries, observer) => {
        if (!resizeHolding) return callback(entries, observer)
        held ??= { callback, observer, entries: new Map() }
        for (const e of entries) held.entries.set(e.target, e)
        heldObservers.add(held)
      })
      const disconnect = this.disconnect.bind(this)
      this.disconnect = () => {
        if (held) {
          heldObservers.delete(held)
          held.entries.clear()
        }
        disconnect()
      }
      const unobserve = this.unobserve.bind(this)
      this.unobserve = (target) => {
        held?.entries.delete(target)
        unobserve(target)
      }
    }
  }
  Object.defineProperty(SlickResizeObserver, 'name', { value: 'ResizeObserver' })
  G.ResizeObserver = SlickResizeObserver
}
const holdingRegs = new Set() // registrations Taut's gate is holding for
function deferResizeWork({ quietMs, onHoldChange } = {}) {
  const reg = {}
  return TA.deferResizeWork({
    quietMs,
    onHoldChange: (holding) => {
      const was = resizeHolding
      if (holding) holdingRegs.add(reg)
      else holdingRegs.delete(reg)
      resizeHolding = holdingRegs.size > 0
      // after the flag flips, so a callback that resizes something observes live
      if (was && !resizeHolding) releaseObservers()
      onHoldChange?.(holding)
    },
  })
}

Object.assign(S, { blobStore, ScopedStorage, Cache, readStoredFile, STORED_PREFIX })
