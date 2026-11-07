// Privileged halves (a plugin's main.ts, SlickMainPlugin): Slick runs them in
// Electron's main process. Clack runs them here, with a MainCtx whose
// capabilities are Go host functions (privileged HTTP, the secret store,
// files, the OS browser) and Clack's own equivalents (notification filters
// over the showNotification thunk, protocol handlers and request rules over
// fetch). Renderer ↔ main traffic is cloned like IPC.

const CAPABILITIES = new Set(['protocol', 'requests', 'frames', 'notifications', 'switches', 'shell', 'net', 'media', 'dialog', 'cookies', 'secrets'])

// ---- what main halves can require ----

const pathShim = (() => {
  const normalize = (p) => {
    const abs = p.startsWith('/')
    const out = []
    for (const part of p.split('/')) {
      if (!part || part === '.') continue
      if (part === '..') out.pop()
      else out.push(part)
    }
    return (abs ? '/' : '') + out.join('/') || (abs ? '/' : '.')
  }
  const basename = (p, ext) => {
    const b = String(p).replace(/\/+$/, '').split('/').pop() ?? ''
    return ext && b.endsWith(ext) ? b.slice(0, -ext.length) : b
  }
  const extname = (p) => {
    const b = basename(p)
    const i = b.lastIndexOf('.')
    return i > 0 ? b.slice(i) : ''
  }
  const shim = {
    sep: '/',
    delimiter: ':',
    normalize,
    join: (...parts) => normalize(parts.filter((x) => x !== '').join('/')),
    resolve: (...parts) => {
      let p = ''
      for (const part of parts) p = part.startsWith('/') ? part : p ? p + '/' + part : part
      return normalize(p.startsWith('/') ? p : go('os', 'cwd') + '/' + p)
    },
    dirname: (p) => {
      const s = String(p).replace(/\/+$/, '')
      const i = s.lastIndexOf('/')
      return i < 0 ? '.' : i === 0 ? '/' : s.slice(0, i)
    },
    basename,
    extname,
    isAbsolute: (p) => String(p).startsWith('/'),
    relative: (from, to) => {
      const a = normalize(from).split('/').filter(Boolean)
      const b = normalize(to).split('/').filter(Boolean)
      let i = 0
      while (i < a.length && a[i] === b[i]) i++
      return [...a.slice(i).map(() => '..'), ...b.slice(i)].join('/')
    },
  }
  shim.posix = shim
  return shim
})()

class FileStream {
  constructor(path) {
    this.path = path
  }
}

const statOf = (path) => {
  const st = go('fs', 'stat', String(path))
  if (!st) {
    const err = new Error(`ENOENT: no such file or directory, stat '${path}'`)
    err.code = 'ENOENT'
    throw err
  }
  return { ...st, isFile: () => st.file, isDirectory: () => st.dir, mtime: new Date(st.mtimeMs) }
}
const readBytes = (path) => {
  const b64 = go('fs', 'read', String(path))
  if (b64 === null || b64 === undefined) {
    const err = new Error(`ENOENT: no such file or directory, open '${path}'`)
    err.code = 'ENOENT'
    throw err
  }
  return Uint8Array.from(atob(b64), (c) => c.charCodeAt(0))
}
const unsupported = (what) => () => {
  throw new Error(`[slick] ${what} is not available to plugins in Clack`)
}
const fsShim = {
  existsSync: (p) => !!go('fs', 'stat', String(p)),
  statSync: statOf,
  realpathSync: (p) => {
    const r = go('fs', 'realpath', String(p))
    if (!r) {
      const err = new Error(`ENOENT: no such file or directory, realpath '${p}'`)
      err.code = 'ENOENT'
      throw err
    }
    return r
  },
  readFileSync: (p, enc) => {
    const bytes = readBytes(p)
    return enc ? new TextDecoder().decode(bytes) : bytes
  },
  createReadStream: (p) => new FileStream(String(p)),
  writeFileSync: unsupported('fs.writeFileSync'),
  mkdirSync: unsupported('fs.mkdirSync'),
}
fsShim.realpathSync.native = fsShim.realpathSync
fsShim.promises = {
  stat: async (p) => statOf(p),
  readFile: async (p, enc) => fsShim.readFileSync(p, typeof enc === 'object' ? enc?.encoding : enc),
  realpath: async (p) => fsShim.realpathSync(p),
  access: async (p) => void statOf(p),
  mkdtemp: unsupported('fs.mkdtemp'),
  writeFile: unsupported('fs.writeFile'),
  rm: unsupported('fs.rm'),
  copyFile: unsupported('fs.copyFile'),
}
const streamShim = {
  Readable: {
    toWeb(stream) {
      const bytes = stream instanceof FileStream ? readBytes(stream.path) : new Uint8Array()
      return new ReadableStream({
        start(c) {
          c.enqueue(bytes)
          c.close()
        },
      })
    },
  },
}
const osShim = {
  homedir: () => go('os', 'home'),
  tmpdir: () => go('os', 'tmp'),
  platform: () => go('os', 'platform'),
  EOL: '\n',
}
const electronShim = {
  app: { isReady: () => true, whenReady: () => Promise.resolve(), getPath: (name) => go('os', name === 'home' ? 'home' : 'tmp'), getVersion: () => '0.0.0' },
  session: { defaultSession: { setSpellCheckerEnabled() {}, setSpellCheckerLanguages() {}, webRequest: { onBeforeRequest() {} } } },
  shell: { openExternal: async (url) => void G.open?.(url) },
  webContents: { getAllWebContents: () => [] },
  BrowserWindow: { getAllWindows: () => [] },
}

// the rest of Node's builtins: loadable, and a clear error when a main half
// actually uses a part Clack doesn't offer (CustomSounds' macOS afconvert)
const notInClack = (what) => (...args) => {
  const err = new Error(`[slick] ${what} is not available to plugins in Clack`)
  err.code = 'ENOSYS'
  const cb = args.findLast?.((a) => typeof a === 'function')
  if (cb) {
    queueMicrotask(() => cb(err))
    return undefined
  }
  throw err
}
const stubModule = (name) =>
  new Proxy(
    {},
    {
      get: (_t, key) => (key === '__esModule' ? false : key === 'default' ? stubModule(name) : typeof key === 'symbol' ? undefined : notInClack(`${name}.${String(key)}`)),
    }
  )
const promisify = (fn) => (...args) =>
  new Promise((resolve, reject) => fn(...args, (err, ...out) => (err ? reject(err) : resolve(out.length > 1 ? out : out[0]))))
const utilShim = { promisify, inspect: (v) => (typeof v === 'string' ? v : JSON.stringify(v)), format: (...a) => a.join(' '), TextEncoder, TextDecoder }
class Hash {
  #alg
  #parts = []
  constructor(alg) {
    this.#alg = String(alg).toLowerCase()
  }
  update(data) {
    this.#parts.push(typeof data === 'string' ? new TextEncoder().encode(data) : new Uint8Array(data.buffer ?? data, data.byteOffset ?? 0, data.byteLength ?? data.length))
    return this
  }
  digest(enc) {
    let bin = ''
    for (const p of this.#parts) for (let i = 0; i < p.length; i += 0x8000) bin += String.fromCharCode(...p.subarray(i, i + 0x8000))
    const hex = go('hash', this.#alg, btoa(bin))
    if (enc === 'hex') return hex
    const bytes = Uint8Array.from(hex.match(/../g) ?? [], (h) => parseInt(h, 16))
    if (enc === 'base64') return btoa(String.fromCharCode(...bytes))
    return bytes
  }
}
const cryptoShim = { createHash: (alg) => new Hash(alg), randomUUID: () => crypto.randomUUID(), randomBytes: (n) => crypto.getRandomValues(new Uint8Array(n)), webcrypto: crypto }
class EventEmitter {
  #l = new Map()
  on(e, f) {
    if (!this.#l.has(e)) this.#l.set(e, [])
    this.#l.get(e).push(f)
    return this
  }
  addListener(e, f) {
    return this.on(e, f)
  }
  once(e, f) {
    const g = (...a) => {
      this.off(e, g)
      f(...a)
    }
    return this.on(e, g)
  }
  off(e, f) {
    this.#l.set(e, (this.#l.get(e) ?? []).filter((x) => x !== f))
    return this
  }
  removeListener(e, f) {
    return this.off(e, f)
  }
  emit(e, ...a) {
    const ls = this.#l.get(e) ?? []
    for (const f of [...ls]) f(...a)
    return ls.length > 0
  }
}
const urlShim = {
  URL,
  URLSearchParams,
  fileURLToPath: (u) => decodeURIComponent(new URL(String(u)).pathname),
  pathToFileURL: (p) => new URL('file://' + encodeURI(pathShim.resolve(p))),
}

const processShim = {
  platform: 'linux',
  arch: 'x64',
  env: {},
  argv: [],
  versions: { node: '24.0.0', electron: '0.0.0-clack' },
  version: 'v24.0.0',
  cwd: () => go('os', 'cwd'),
  nextTick: (fn, ...args) => queueMicrotask(() => fn(...args)),
  on() {
    return processShim
  },
  off() {
    return processShim
  },
  once() {
    return processShim
  },
  emitWarning: (w) => console.warn(String(w)),
}
let processReady = false
function processFor() {
  if (!processReady) {
    processReady = true
    try {
      processShim.platform = go('os', 'platform')
    } catch {}
  }
  return processShim
}
const bufferShim = {
  Buffer: {
    from: (v, enc) => (typeof v === 'string' ? (enc === 'base64' ? Uint8Array.from(atob(v), (c) => c.charCodeAt(0)) : new TextEncoder().encode(v)) : new Uint8Array(v)),
    isBuffer: (v) => v instanceof Uint8Array,
    byteLength: (s) => new TextEncoder().encode(String(s)).length,
  },
}

function requireMain(name) {
  switch (name.replace(/^node:/, '')) {
    case 'process':
      return processFor()
    case 'buffer':
      return bufferShim
    case 'util':
      return utilShim
    case 'crypto':
      return cryptoShim
    case 'events':
      return Object.assign(EventEmitter, { EventEmitter, default: EventEmitter })
    case 'url':
      return urlShim
    case 'child_process':
    case 'zlib':
    case 'http':
    case 'https':
    case 'net':
    case 'tls':
    case 'dgram':
    case 'worker_threads':
    case 'readline':
    case 'vm':
      return stubModule(name.replace(/^node:/, ''))
    case 'fs':
      return fsShim
    case 'fs/promises':
      return fsShim.promises
    case 'path':
    case 'path/posix':
      return pathShim
    case 'os':
      return osShim
    case 'stream':
      return streamShim
    case 'electron':
      return electronShim
    case '$slick':
      return slickModule
  }
  throw new Error(`[slick] main halves can't require ${name} in Clack`)
}

// ---- request rules and protocols, applied to fetch ----

const protocols = new Map() // scheme → {id, handler}
const netRules = new Set() // {id, patterns, block?, handler?}
const switches = new Set()

/** Chrome match patterns ("*://*.example.com/*", "<all_urls>") */
function matchPattern(pattern, url) {
  if (pattern === '<all_urls>') return true
  const m = /^(\*|[a-z][a-z0-9+.-]*):\/\/([^/]*)(\/.*)?$/i.exec(pattern)
  if (!m) return false
  let u
  try {
    u = new URL(url)
  } catch {
    return false
  }
  const [, scheme, hostPat, pathPat = '/*'] = m
  const proto = u.protocol.slice(0, -1)
  if (scheme === '*' ? proto !== 'http' && proto !== 'https' : scheme.toLowerCase() !== proto) return false
  if (hostPat !== '*') {
    if (hostPat.startsWith('*.')) {
      const base = hostPat.slice(2).toLowerCase()
      if (u.hostname !== base && !u.hostname.endsWith('.' + base)) return false
    } else if (u.hostname !== hostPat.toLowerCase().replace(/:\d+$/, '')) return false
  }
  const re = new RegExp('^' + pathPat.split('*').map((s) => s.replace(/[.+?^${}()|[\]\\]/g, '\\$&')).join('.*') + '$')
  return re.test(u.pathname + u.search)
}

const pageFetch = G.fetch
async function ruledFetch(input, init) {
  const url = typeof input === 'string' ? input : input instanceof URL ? input.href : input?.url
  if (typeof url === 'string') {
    const scheme = url.slice(0, Math.max(0, url.indexOf(':')))
    const proto = protocols.get(scheme)
    if (proto) {
      const req = input instanceof Request ? input : new Request(url, init)
      return proto.handler(req)
    }
    for (const rule of netRules) {
      if (!rule.patterns.some((p) => matchPattern(p, url))) continue
      if (rule.block) throw new TypeError('Failed to fetch')
      const out = rule.handler?.({ url, method: init?.method ?? 'GET', resourceType: 'xhr' })
      if (out?.cancel) throw new TypeError('Failed to fetch')
      if (out?.redirectURL && out.redirectURL !== url) return ruledFetch(out.redirectURL, init)
    }
  }
  return pageFetch(input, init)
}
function syncFetchHook() {
  G.fetch = protocols.size || netRules.size ? ruledFetch : pageFetch
}

// ---- notifications: one filter chain over Slack's showNotification thunk ----

const notificationFilters = new Set()
let unpatchNotifications
function syncNotificationPatch() {
  if (notificationFilters.size && !unpatchNotifications) {
    unpatchNotifications = TA.redux.patchThunk('showNotification', (original) => (...args) => {
      const opts = args[0] || {}
      const message = opts.message
      const body = typeof message === 'string' ? message : (message?.text ?? opts.body ?? '')
      // Electron's NotificationConstructorOptions, from Slack's showNotification args
      const options = { ...opts, title: opts.title ?? '', subtitle: opts.subtitle, body, silent: opts.silent }
      for (const f of [...notificationFilters]) {
        let ok = true
        try {
          ok = f.predicate(options) !== false
        } catch (err) {
          console.error(`[slick] ${f.id} notification filter threw:`, err)
        }
        if (!ok) return () => undefined
      }
      return original(...args)
    })
  } else if (!notificationFilters.size && unpatchNotifications) {
    unpatchNotifications()
    unpatchNotifications = undefined
  }
}

// ---- the renderer side of main.call / main.on ----

const fakeSender = Object.freeze({ id: 1, isDestroyed: () => false, send() {}, session: {}, executeJavaScript: async (code) => (0, eval)(code) })

async function callMain(id, method, args) {
  const entry = plugins.get(id)
  const main = entry?.main
  if (!main) throw new Error(`[slick] no main module for ${id}`)
  if (!main.running) throw new Error(`[slick] ${id} is disabled`)
  const rpc = main.plugin.rpc ?? {}
  if (!Object.hasOwn(rpc, method)) throw new Error(`[slick] unknown method: ${id}.${method}`)
  return clone(await rpc[method](main.ctx, clone(args) ?? [], fakeSender))
}

function onMainEvent(id, event, cb) {
  const entry = plugins.get(id)
  if (!entry) return () => {}
  entry.mainListeners ??= new Map()
  let set = entry.mainListeners.get(event)
  if (!set) entry.mainListeners.set(event, (set = new Set()))
  set.add(cb)
  return () => {
    set.delete(cb)
    if (!set.size) entry.mainListeners.delete(event)
  }
}

function emitToRenderer(id, event, payload) {
  const set = plugins.get(id)?.mainListeners?.get(event)
  if (!set) return
  const copy = clone(payload)
  queueMicrotask(() => {
    for (const cb of [...set]) {
      try {
        cb(copy)
      } catch (err) {
        console.error(`[slick] ${id} main event listener threw:`, err)
      }
    }
  })
}

// ---- MainCtx ----

function createMainCtx(id, main) {
  const need = (cap) => {
    if (!main.plugin.capabilities?.includes(cap)) throw new Error(`[slick] ${id} used "${cap}" without declaring it`)
  }
  const disposers = (set, item, after) => {
    set.add(item)
    after?.()
    return () => {
      set.delete(item)
      after?.()
    }
  }
  const blob = blobStore(`plugin:${id}`)
  const secrets = blobStore(`plugin:${id}:secrets`)
  return {
    id,
    get settings() {
      return main.settings
    },
    onSettingsChange(cb) {
      main.settingsListeners.add(cb)
      return () => void main.settingsListeners.delete(cb)
    },
    log: (...args) => console.log(`[slick] [${id}]`, ...args),
    emit: (event, payload) => emitToRenderer(id, event, payload),
    emitTo: (_contents, event, payload) => emitToRenderer(id, event, payload),
    storage: {
      list: () => blob.list(),
      read: (key) => blob.read(key),
      readAll: (prefix) => blob.readAll(prefix),
      write: (key, value) => blob.write(key, value),
      delete: (key) => blob.delete(key),
      clear: () => blob.clear(),
    },
    protocol: {
      register(scheme, _privileges, handler) {
        need('protocol')
        if (main.booted) throw new Error(`[slick] ${id}: protocols must be declared during boot`)
        main.protocols.set(scheme, handler)
      },
    },
    net: {
      block(patterns) {
        need('requests')
        return disposers(netRules, { id, patterns: [...patterns], block: true }, syncFetchHook)
      },
      intercept(patterns, handler) {
        need('requests')
        return disposers(netRules, { id, patterns: [...patterns], handler }, syncFetchHook)
      },
      async fetch(url, init) {
        need('net')
        const res = await pageFetch(url, init)
        return { status: res.status, body: await res.text() }
      },
    },
    frames: {
      onFrame() {
        need('frames')
        return () => {}
      },
    },
    notifications: {
      filter(predicate) {
        need('notifications')
        return disposers(notificationFilters, { id, predicate }, syncNotificationPatch)
      },
    },
    media: {
      onDisplayMediaRequest() {
        need('media')
        return () => {}
      },
    },
    switches: {
      append(name, value) {
        need('switches')
        if (main.booted) throw new Error(`[slick] ${id}: switches can only be set during boot`)
        switches.add(value === undefined ? name : `${name}=${value}`)
      },
    },
    shell: {
      async openExternal(url) {
        need('shell')
        const parsed = new URL(url)
        if (!['https:', 'http:', 'mailto:', 'slack:'].includes(parsed.protocol)) throw new Error(`[slick] ${id}: refusing to open ${parsed.protocol} externally`)
        go('openURL', parsed.href)
      },
    },
    dialog: {
      async openFile(options) {
        need('dialog')
        return (await go('openFile', options?.title ?? '', (options?.filters ?? []).flatMap((f) => f.extensions ?? []).join(','))) ?? ''
      },
    },
    sessions: {
      clients: () => [fakeSender],
      async navigate(_sender, url, _pending, mutate) {
        await mutate?.()
        await go('navigate', id, String(url))
      },
      onSignIn: () => () => {},
    },
    cookies: {
      get: async (details) => {
        need('cookies')
        return TA.cookies.get(details).then((c) => c ?? null)
      },
      set: async (details) => {
        need('cookies')
        await TA.cookies.set(details)
      },
      remove: async (url, name) => {
        need('cookies')
        await TA.cookies.remove({ url, name })
      },
      onChanged: () => {
        need('cookies')
        return () => {}
      },
    },
    secrets: {
      read: (key) => {
        need('secrets')
        return secrets.read(key)
      },
      write: (key, value) => {
        need('secrets')
        return secrets.write(key, value)
      },
      delete: (key) => {
        need('secrets')
        return secrets.delete(key)
      },
    },
  }
}

function mainPluginFrom(code) {
  const factory = factoryFrom(code, '__slickMainPlugins', 'main half')
  const plugin = factory(requireMain)
  if (!plugin || typeof plugin !== 'object' || typeof plugin.id !== 'string') throw new Error('the file has no default-exported SlickMainPlugin')
  for (const cap of plugin.capabilities ?? []) if (!CAPABILITIES.has(cap)) throw new Error(`unknown capability ${cap}`)
  return plugin
}
const mainMeta = (plugin) => ({ id: plugin.id, capabilities: [...(plugin.capabilities ?? [])], rpc: Object.keys(plugin.rpc ?? {}) })

/** Go → JS: a main-half file's declarations, without keeping it */
S.describeMain = (code) => mainMeta(mainPluginFrom(code))

/** Go → JS: evaluates a main-half file and keeps it for startMain */
S.loadMain = (code) => {
  const plugin = mainPluginFrom(code)
  let entry = plugins.get(plugin.id)
  if (!entry) plugins.set(plugin.id, (entry = { Class: null, instance: null, scope: null }))
  if (entry.main?.running) throw new Error(`${plugin.id}'s main half is running; stop it before loading new code`)
  entry.main = { plugin, ctx: null, settings: null, settingsListeners: new Set(), protocols: new Map(), running: false, booted: false, dispose: null }
  return mainMeta(plugin)
}

/** Go → JS: boot (once per runtime) and ready the main half */
S.startMain = async (id, settings) => {
  const main = plugins.get(id)?.main
  if (!main) return null
  main.settings = { ...settings }
  if (!main.ctx) main.ctx = createMainCtx(id, main)
  if (!main.booted) {
    try {
      main.plugin.boot?.(main.ctx)
    } catch (err) {
      console.error(`[slick] ${id} boot failed:`, err)
    }
    main.booted = true
  }
  if (main.running) return null
  try {
    for (const [scheme, handler] of main.protocols) protocols.set(scheme, { id, handler })
    syncFetchHook()
    const dispose = await withTimeout(id, 'ready', Promise.resolve().then(() => main.plugin.ready?.(main.ctx)))
    main.dispose = typeof dispose === 'function' ? dispose : null
    main.running = true
    try {
      main.plugin.window?.(main.ctx, { webContents: fakeSender, isDestroyed: () => false, on() {}, once() {} })
    } catch (err) {
      console.error(`[slick] ${id} window hook failed:`, err)
    }
    return null
  } catch (err) {
    for (const [scheme] of main.protocols) protocols.delete(scheme)
    syncFetchHook()
    console.error(`[slick] ${id} ready failed:`, err)
    return errorText(err)
  }
}

S.stopMain = async (id) => {
  const main = plugins.get(id)?.main
  if (!main?.running) return null
  main.running = false
  let problem = null
  try {
    await withTimeout(id, 'dispose', Promise.resolve().then(() => main.dispose?.()))
  } catch (err) {
    console.error(`[slick] ${id} dispose failed:`, err)
    problem = errorText(err)
  }
  main.dispose = null
  main.settingsListeners.clear()
  for (const [scheme] of main.protocols) if (protocols.get(scheme)?.id === id) protocols.delete(scheme)
  syncFetchHook()
  return problem
}

function mainSettingsChanged(id, settings) {
  const main = plugins.get(id)?.main
  if (!main?.running) return
  const before = JSON.stringify(main.settings)
  main.settings = { ...settings }
  if (JSON.stringify(main.settings) === before) return
  for (const l of [...main.settingsListeners]) {
    try {
      l(main.settings)
    } catch (err) {
      console.error(`[slick] ${id} settings listener threw:`, err)
    }
  }
}
S.mainSettings = (id, settings) => mainSettingsChanged(id, settings)
S.hasMain = (id) => !!plugins.get(id)?.main
S.callMain = callMain
S.onMain = onMainEvent
