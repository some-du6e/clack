// Plugin data: api.storage / api.secrets (ScopedStorage), api.Cache,
// api.SharedStore and api.sharedFrom. Blobs live in Go (storage.go), which
// keeps them in memory and writes them under ~/.config/clack/taut/ (secrets
// in a separate 0600 file). Calls are synchronous in Go but every blob method
// returns a Promise, like Taut's bridge.

/** a namespace's blob store in Go */
function goBlob(namespace) {
  return {
    list: async () => go('kv', 'list', namespace) || [],
    read: async (key) => go('kv', 'read', namespace, key) ?? null,
    write: async (key, value) => !!go('kv', 'write', namespace, key, value),
    delete: async (key) => !!go('kv', 'delete', namespace, key),
    clear: async () => !!go('kv', 'clear', namespace),
  }
}

/** in-process stand-in for the BroadcastChannel tabs use to keep stores in step */
const storageChannels = new Map() // namespace → Set(listener)
function announceStorageChange(namespace, key, from) {
  for (const l of [...(storageChannels.get(namespace) || [])]) {
    if (l === from) continue
    try {
      l(key)
    } catch {}
  }
}

class ScopedStorage {
  #stores = new Map()
  #listener = null
  constructor(blob, namespace, signal) {
    this.blob = blob
    this.namespace = namespace
    this.signal = signal
  }
  store(key, defaultValue) {
    let s = this.#stores.get(key)
    if (!s) {
      s = this.#open(key, defaultValue)
      this.#stores.set(key, s)
    }
    return s
  }
  async get(key, fallback) {
    return this.#read(key, fallback)
  }
  async set(key, value) {
    try {
      await this.#write(key, value, () => value)
      return true
    } catch {
      return false
    }
  }
  update(key, fallback, change) {
    return this.#write(key, fallback, change)
  }
  keys() {
    return this.blob.list()
  }
  async clear() {
    const ok = await this.blob.clear()
    if (ok) {
      for (const s of this.#stores.values()) await s.refresh()
      this.#broadcast()
    }
    return ok
  }
  async delete(key) {
    const ok = await withLock(this.#lock(key), () => this.blob.delete(key))
    if (ok) {
      await this.#stores.get(key)?.refresh()
      this.#broadcast(key)
    }
    return ok
  }
  #lock(key) {
    return `taut:${this.namespace}:${key}`
  }
  async #read(key, fallback) {
    const raw = await this.blob.read(key)
    if (raw === null || raw === undefined) return fallback
    try {
      return JSON.parse(raw)
    } catch {
      return fallback
    }
  }
  async #write(key, fallback, change) {
    const next = await withLock(this.#lock(key), async () => {
      const v = change(await this.#read(key, fallback))
      if (!(await this.blob.write(key, JSON.stringify(v)))) throw new Error(`Couldn't save ${key}`)
      return v
    })
    await this.#stores.get(key)?.refresh()
    this.#broadcast(key)
    return next
  }
  #open(key, defaultValue) {
    this.#listen()
    const local = new Store(defaultValue)
    const lock = this.#lock(key)
    const refresh = async () => {
      const saved = await withLock(lock, () => this.#read(key, defaultValue))
      if (!deepEqual(saved, local.get())) local.set(saved)
      return saved
    }
    const update = async (change) => {
      local.update(change)
      try {
        return await withLock(lock, async () => {
          const next = change(await this.#read(key, defaultValue))
          if (!(await this.blob.write(key, JSON.stringify(next)))) throw new Error(`Couldn't save ${key}`)
          if (!deepEqual(next, local.get())) local.set(next)
          this.#broadcast(key)
          return next
        })
      } catch (err) {
        await refresh().catch(() => {})
        throw err
      }
    }
    return {
      get: local.get,
      subscribe: local.subscribe,
      use: local.use,
      ready: refresh().then(
        () => {},
        (err) => error(`Couldn't load ${this.namespace} ${key}:`, err)
      ),
      set: (value) => update(() => value),
      update,
      refresh,
    }
  }
  #broadcast(key) {
    announceStorageChange(this.namespace, key, this.#listener)
  }
  #listen() {
    if (this.#listener) return
    const l = (key) => {
      for (const [k, s] of this.#stores) if (key === undefined || key === k) s.refresh()
    }
    this.#listener = l
    let set = storageChannels.get(this.namespace)
    if (!set) storageChannels.set(this.namespace, (set = new Set()))
    set.add(l)
    const close = () => set.delete(l)
    if (this.signal?.aborted) close()
    else this.signal?.addEventListener?.('abort', close, { once: true })
  }
}

// ---- Cache ----

const CACHE_WRITE_DELAY = 2000
class Cache {
  #memory = new Map()
  #pending = new Map()
  #generation = 0
  #writes = Promise.resolve()
  #timer = null
  constructor(blob, storageKey, options = {}) {
    this.blob = blob
    this.storageKey = storageKey
    this.options = options || {}
  }
  #fresh(e) {
    const { ttl } = this.options
    return ttl === undefined || Date.now() - e.ts < ttl
  }
  #queue(w) {
    this.#writes = this.#writes.catch(() => {}).then(w)
  }
  #persist() {
    if (this.#timer) return
    this.#timer = setTimeout(() => {
      this.#timer = null
      const value = JSON.stringify(Object.fromEntries(this.#memory))
      this.#queue(() => this.blob.write(this.storageKey, value))
    }, CACHE_WRITE_DELAY)
  }
  #evict() {
    const { maxSize } = this.options
    if (maxSize === undefined) return
    for (const k of this.#memory.keys()) {
      if (this.#memory.size <= maxSize) return
      this.#memory.delete(k)
    }
  }
  async load() {
    const gen = this.#generation
    let saved
    try {
      await this.#writes.catch(() => {})
      saved = JSON.parse((await this.blob.read(this.storageKey)) ?? '{}')
    } catch {
      return
    }
    if (gen !== this.#generation || !saved || typeof saved !== 'object') return
    const loaded = Object.entries(saved)
      .filter(([k, e]) => typeof e?.ts === 'number' && !this.#memory.has(k) && (this.options.staleIfError || this.#fresh(e)))
      .sort((a, b) => a[1].ts - b[1].ts)
    this.#memory = new Map([...loaded, ...this.#memory])
    this.#evict()
  }
  get(key) {
    const e = this.#memory.get(key)
    return e && this.#fresh(e) ? e.value : undefined
  }
  set(key, value) {
    this.#memory.delete(key)
    this.#memory.set(key, { value, ts: Date.now() })
    this.#evict()
    this.#persist()
  }
  async fetch(key, fetcher) {
    const e = this.#memory.get(key)
    if (e && this.#fresh(e)) return e.value
    const existing = this.#pending.get(key)
    if (existing) return existing
    const gen = this.#generation
    const p = Promise.resolve()
      .then(fetcher)
      .then(
        (v) => {
          if (gen === this.#generation) this.set(key, v)
          return v
        },
        (err) => {
          const stale = this.#memory.get(key)
          if (this.options.staleIfError && stale) return stale.value
          throw err
        }
      )
      .finally(() => {
        if (this.#pending.get(key) === p) this.#pending.delete(key)
      })
    this.#pending.set(key, p)
    return p
  }
  clear() {
    this.#generation++
    this.#memory.clear()
    this.#pending.clear()
    if (this.#timer) clearTimeout(this.#timer)
    this.#timer = null
    this.#queue(() => this.blob.delete(this.storageKey))
  }
}
function bindCache(blob) {
  return class BoundCache extends Cache {
    constructor(key, options) {
      super(blob, key, options)
    }
  }
}

// ---- SharedStore ----

const sharedSlots = new Map()
const sharedOwners = new Map()
function sharedSlot(name) {
  let s = sharedSlots.get(name)
  if (!s) sharedSlots.set(name, (s = new Store(undefined)))
  return s
}
function sharedFrom(pluginId, key) {
  const { get, subscribe, use } = sharedSlot(`${pluginId}:${key}`)
  return { get, subscribe, use }
}
class SharedStore {
  #name
  #slot
  constructor(name, initial) {
    if (sharedOwners.has(name)) throw new Error(`Shared store "${name}" already has an owner`)
    this.#name = name
    this.#slot = sharedSlot(name)
    sharedOwners.set(name, this)
    this.#slot.set(initial)
  }
  get = () => this.#slot.get()
  set = (next) => {
    if (sharedOwners.get(this.#name) !== this) return
    this.#slot.set(next)
  }
  update = (fn) => this.set(fn(this.get()))
  use = () => this.#slot.use()
  dispose = () => {
    if (sharedOwners.get(this.#name) !== this) return
    sharedOwners.delete(this.#name)
    this.#slot.set(undefined)
  }
}
function bindSharedStore(pluginId, signal) {
  return class BoundSharedStore extends SharedStore {
    constructor(key, initial) {
      super(`${pluginId}:${key}`, initial)
      if (signal.aborted) this.dispose()
      else signal.addEventListener('abort', this.dispose, { once: true })
    }
  }
}

Object.assign(T, { ScopedStorage, Cache, bindCache, SharedStore, bindSharedStore, sharedFrom, goBlob, announceStorageChange })
