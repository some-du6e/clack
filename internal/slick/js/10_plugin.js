// Clack's Slick runtime: the `$slick` module, each plugin's SlickAPI and the
// renderer lifecycle. Slick (https://github.com/3kh0/slick) is GPLv3; this is
// Clack's own implementation of its plugin contract, on top of the TautAPI
// internals (globalThis.__taut) that already run Slack-shaped state, patches,
// thunks and composer hooks against Clack's native UI. All files in js/ are
// concatenated in name order into one function scope (slick.go).

const G = globalThis
const T = G.__taut
const TA = G.TautAPI
const host = G.__clack || {}
const S = (G.__slick = G.__slick || {})

/** calls Go host function __clack.slick_<name> */
function go(name, ...args) {
  const fn = host['slick_' + name]
  if (typeof fn !== 'function') throw new Error(`[slick] host function slick_${name} is missing`)
  return fn(...args)
}

const errorText = (err) => (err instanceof Error ? err.message : String(err))
const clone = (v) => (v === undefined ? undefined : JSON.parse(JSON.stringify(v)))

// ---- $slick ----

const PLUGIN_APIS = Object.freeze([
  'redux',
  'rtm',
  'messages',
  'members',
  'channels',
  'blocks',
  'files',
  'elements',
  'menu',
  'modal',
  'react',
  'onMessageSendDelta',
  'onMessageSendBlocks',
  'addSendCheck',
])

class SlickPlugin {
  static authors = ''
  static defaultEnabled = false
  static settings = {}
  static requiredAPIs = PLUGIN_APIS
  static liveSettings = []
  static relaunchSettings = []

  log = (...args) => {
    console.log(`[slick] [${this.constructor.pluginName}]`, ...args)
  }

  constructor(api, config) {
    this.api = api
    this.config = config
  }

  stop() {}

  onSettingsChange(_changed) {}
}

const slickModule = Object.freeze({ __esModule: true, PLUGIN_APIS, SlickPlugin, default: SlickPlugin })
S.module = slickModule
S.SlickPlugin = SlickPlugin

// ---- loading user plugin files ----

/** runs a plugin file, which pushes one factory onto globalThis[queue] */
function factoryFrom(code, queue, what) {
  const previous = Object.getOwnPropertyDescriptor(G, queue)
  const found = []
  G[queue] = found
  try {
    new Function(code)()
  } finally {
    if (previous) Object.defineProperty(G, queue, previous)
    else delete G[queue]
  }
  if (found.length !== 1 || typeof found[0] !== 'function') throw new Error(`That file is not a Slick ${what}.`)
  return found[0]
}

const PLUGIN_ID = /^[A-Za-z0-9_.-]{1,100}$/

function rendererClass(code) {
  const factory = factoryFrom(code, '__slickPlugins', 'plugin')
  const PluginClass = factory((name) => {
    if (name === '$slick') return slickModule
    throw new Error(`user plugins can only import $slick, not ${name}`)
  })
  if (!PluginClass) throw new Error('the file has no default-exported plugin class')
  if (typeof PluginClass !== 'function' || !(PluginClass.prototype instanceof SlickPlugin)) {
    throw new Error('plugin does not extend SlickPlugin')
  }
  if (typeof PluginClass.id !== 'string' || !PLUGIN_ID.test(PluginClass.id)) {
    throw new Error(`plugin has an invalid id: ${String(PluginClass.id)}`)
  }
  return PluginClass
}

const strings = (v) => (Array.isArray(v) ? v.filter((x) => typeof x === 'string') : [])

function describe(PluginClass) {
  const settings = PluginClass.settings && typeof PluginClass.settings === 'object' ? PluginClass.settings : {}
  return {
    id: PluginClass.id,
    name: typeof PluginClass.pluginName === 'string' && PluginClass.pluginName ? PluginClass.pluginName : PluginClass.id,
    description: typeof PluginClass.description === 'string' ? PluginClass.description : '',
    category: typeof PluginClass.category === 'string' ? PluginClass.category : null,
    authors: typeof PluginClass.authors === 'string' ? PluginClass.authors : '',
    defaultEnabled: PluginClass.defaultEnabled === true,
    settings: clone(settings),
    liveSettings: strings(PluginClass.liveSettings),
    relaunchSettings: strings(PluginClass.relaunchSettings),
    requiredAPIs: Array.isArray(PluginClass.requiredAPIs) ? strings(PluginClass.requiredAPIs) : [...PLUGIN_APIS],
  }
}

/** id → {Class, instance, scope, config, running, main} */
const plugins = new Map()

/** Go → JS: evaluates a renderer file and returns its metadata */
S.load = (code) => {
  const PluginClass = rendererClass(code)
  const id = PluginClass.id
  const existing = plugins.get(id)
  if (existing?.instance) throw new Error(`${id} is running; stop it before loading new code`)
  plugins.set(id, { ...(existing || {}), Class: PluginClass, instance: null, scope: null })
  return describe(PluginClass)
}

/** Go → JS: just the metadata of a file, without keeping it */
S.describe = (code) => describe(rendererClass(code))

S.unload = async (id) => {
  await S.stop(id)
  await S.stopMain(id)
  plugins.delete(id)
}

// ---- scopes (Slick's: dispose aborts, then reverses registrations) ----

function createScope() {
  let active = true
  const controller = new AbortController()
  const cleanups = new Set()
  return {
    signal: controller.signal,
    track(cleanup) {
      let pending = typeof cleanup === 'function' ? cleanup : null
      const once = () => {
        const run = pending
        if (!run) return
        pending = null
        cleanups.delete(once)
        run()
      }
      if (active) cleanups.add(once)
      else once()
      return once
    },
    dispose() {
      if (!active) return
      active = false
      controller.abort()
      for (const cleanup of [...cleanups].reverse()) {
        try {
          cleanup()
        } catch (err) {
          console.error('[slick] error disposing plugin resource:', err)
        }
      }
    },
  }
}

const LIFECYCLE_MS = 5000
function withTimeout(id, phase, promise) {
  return new Promise((resolve, reject) => {
    const timer = setTimeout(() => reject(new Error(`[slick] ${id} ${phase} timed out after ${LIFECYCLE_MS}ms`)), LIFECYCLE_MS)
    Promise.resolve(promise).then(
      (v) => {
        clearTimeout(timer)
        resolve(v)
      },
      (e) => {
        clearTimeout(timer)
        reject(e)
      }
    )
  })
}

// ---- lifecycle (Go owns config, order and files; see manager.go) ----

/** Go → JS: start with resolved settings; null, or the error to show */
S.start = async (id, settings) => {
  const entry = plugins.get(id)
  if (!entry?.Class) return `${id} is not loaded`
  if (entry.instance) await S.stop(id)
  const scope = createScope()
  let instance
  try {
    const config = { ...settings }
    const api = createScopedAPI(entry, id, scope)
    instance = new entry.Class(api, config)
    entry.config = config
    entry.instance = instance
    entry.scope = scope
    await withTimeout(id, 'start', Promise.resolve().then(() => instance.start()))
    if (scope.signal.aborted) {
      await S.stop(id)
      return null
    }
    entry.running = true
    console.log(`[slick] started ${id}`)
    return null
  } catch (err) {
    console.error(`[slick] ${id} failed to start:`, err)
    entry.instance = instance ?? null
    entry.scope = scope
    await S.stop(id)
    return errorText(err)
  }
}

/** Go → JS: stop; registrations are disposed before stop() runs, like Slick */
S.stop = async (id) => {
  const entry = plugins.get(id)
  if (!entry) return null
  const { instance, scope } = entry
  entry.instance = null
  entry.scope = null
  entry.running = false
  scope?.dispose()
  if (!instance) return null
  try {
    await withTimeout(id, 'stop', Promise.resolve().then(() => instance.stop()))
  } catch (err) {
    console.error(`[slick] ${id} failed to stop cleanly:`, err)
    return errorText(err)
  } finally {
    console.log(`[slick] stopped ${id}`)
  }
  return null
}

/** Go → JS: live settings, every changed key in liveSettings: mutate config in place and call onSettingsChange */
S.live = async (id, settings, changed) => {
  const entry = plugins.get(id)
  if (!entry?.instance) return 'not running'
  Object.assign(entry.config, settings)
  try {
    await withTimeout(id, 'onSettingsChange', Promise.resolve().then(() => entry.instance.onSettingsChange(changed)))
    console.log(`[slick] ${id} applied live settings: ${changed.join(', ')}`)
  } catch (err) {
    console.error(`[slick] ${id} onSettingsChange failed:`, err)
  }
  mainSettingsChanged(id, settings)
  return null
}

S.running = () => [...plugins].filter(([, p]) => p.instance).map(([id]) => id)

/** Go → JS: route the UI shows, for location-based lookups (getCurrentChannelId, the active team) */
S.setRoute = (team, channel) => {
  const path = team ? `/client/${team}${channel ? '/' + channel : ''}` : '/client'
  try {
    if (G.location?.pathname !== path) G.history?.replaceState?.(G.history.state ?? null, '', path)
  } catch (err) {
    console.warn('[slick] could not set the route', err)
  }
}

S.introspect = () => ({
  running: S.running(),
  mains: [...plugins].filter(([, p]) => p.main?.running).map(([id]) => id),
  deltaTransforms: deltaTransforms.size,
  documentListeners: documentListeners.size,
  mainListeners: [...plugins].reduce((n, [, p]) => n + (p.mainListeners?.size ?? 0), 0),
  protocols: [...protocols.keys()],
  netRules: netRules.size,
  notificationFilters: notificationFilters.size,
  switches: [...switches],
})

// Slick's preload bridge (app/bridge.ts getBridge), for first-party plugins
// built with Slick's shared code bundled in (Snappy reads loader / settings)
const bridge = Object.freeze({
  loader: 'electron',
  loaderVersion: 'clack',
  bridgeVersion: 1,
  safeMode: false,
  paths: {},
  get initialSettings() {
    return go('settingsText')
  },
  readSettings: async () => go('settingsText'),
  writeSettings: async () => false,
  onSettingsChange: () => () => {},
  readUserCss: async () => '',
  writeUserCss: async () => false,
  onUserCssChange: () => () => {},
  openFile: async (title, accept) => (await go('openFile', title ?? '', accept ?? '')) ?? '',
  openCssEditor: async () => false,
  blobStore: (ns) => blobStore(ns),
  plugin: (id) => ({ call: (method, ...args) => callMain(id, method, args), on: (event, cb) => onMainEvent(id, event, cb) }),
  fetch: (url, init) => bridgeFetch(url, init),
})
if (!G.SlickBridge) Object.defineProperty(G, 'SlickBridge', { value: Object.freeze({ claim: () => bridge }), configurable: true })
