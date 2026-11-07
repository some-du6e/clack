// The plugin runtime half of the manager: evaluating plugin code, scoping
// each running plugin's TautAPI (everything it registers is undone when it
// stops), and start/stop with Taut's 5 second timeout. Go (manager.go) owns
// the files, config.json, secrets and the order of operations.

const LIFECYCLE_MS = 5000

function withLifecycleTimeout(id, phase, promise) {
  return new Promise((resolve, reject) => {
    const t = setTimeout(() => reject(new Error(`Plugin ${id} ${phase} timed out after 5 seconds`)), LIFECYCLE_MS)
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

function createScope() {
  let active = true
  let tail = Promise.resolve()
  const controller = new AbortController()
  const pending = new Set()
  const cleanups = []
  const track = (cleanup) => {
    let done = false
    const wrapped = () => {
      if (done) return
      done = true
      cleanup?.()
    }
    if (active) cleanups.push(wrapped)
    else wrapped()
    return wrapped
  }
  const mutation = (run, rejected) => {
    if (!active) return Promise.resolve(rejected)
    const p = tail.then(run)
    tail = p.then(
      () => {},
      () => {}
    )
    pending.add(p)
    p.finally(() => pending.delete(p)).catch(() => {})
    return p
  }
  return {
    signal: controller.signal,
    abort: () => controller.abort(),
    track,
    wrap: (blob) => ({
      list: () => blob.list(),
      read: (k) => blob.read(k),
      write: (k, v) => mutation(() => blob.write(k, v), false),
      delete: (k) => mutation(() => blob.delete(k), false),
      clear: () => mutation(() => blob.clear(), false),
    }),
    async dispose() {
      if (!active) return
      active = false
      controller.abort()
      for (let i = cleanups.length - 1; i >= 0; i--) {
        try {
          cleanups[i]()
        } catch (err) {
          error('Error disposing plugin resource:', err)
        }
      }
      await Promise.allSettled([...pending])
    },
  }
}

let baseAPI
/** the TautAPI with nothing scoped to a running plugin (what editors get) */
function makeBaseAPI() {
  if (baseAPI) return baseAPI
  baseAPI = {
    setStyle,
    byName,
    byMeta,
    waitForExport,
    within: (work, ms, what, signal) => within(work, ms, what, signal),
    waitForComponent: (name, filter) => T.slackComponent && waitForComponentImpl(name, filter),
    waitForRenderedComponent: (name) => Promise.resolve(T.slackComponent(name)),
    lazyComponent: T.lazyComponent,
    getExport,
    getByProps,
    getModuleSources,
    findModuleId,
    getComponent: T.getComponent,
    getRenderedComponent: (name) => T.slackComponent(name),
    patchComponent: T.patchComponent,
    redux,
    experiments: new ForcedExperiments(),
    members,
    messages,
    channels,
    blocks,
    files,
    huddles,
    profile,
    rtm,
    threads,
    workspace,
    fetch: bridgeFetch,
    userAPI,
    RateLimit,
    cookies,
    accounts: new AccountSwitcher(),
    modal: modalApi(),
    menu: menuApi(),
    elements: elementsApi(),
    preferences,
    commonModules: { react: G.React },
    onMessageSendBlocks,
    composer,
    deferResizeWork,
    scheduleEnd,
    Store,
    SharedStore,
    sharedFrom,
  }
  G.TautAPI = baseAPI
  return baseAPI
}
function waitForComponentImpl(name, filter) {
  try {
    return Promise.resolve(T.getComponent(name, false, filter))
  } catch {
    return new Promise(() => {})
  }
}

/** a plugin's this.api: registrations undone when it stops, storage scoped to its id */
function createScopedAPI(id, scope) {
  const base = makeBaseAPI()
  const tracked =
    (fn) =>
    (...args) =>
      scope.track(fn(...args))
  const experiments = new ForcedExperiments()
  scope.track(experiments.dispose)
  const openModal = (options) => {
    if (scope.signal.aborted) return null
    let open = true
    const closing =
      (cb = () => {}) =>
      () => {
        open = false
        cb()
      }
    const handle = base.modal.openModal({
      ...options,
      onSubmit: closing(options.onSubmit),
      onCancel: closing(options.onCancel),
      onClose: closing(options.onClose),
    })
    const dismiss = scope.track(() => {
      handle.close()
      if (open) closing(options.onClose)()
    })
    return { ...handle, close: closing(dismiss) }
  }
  const storageNs = `plugin:${id}:storage`
  const secretsNs = `plugin:${id}:secrets`
  const cacheNs = `plugin:${id}:cache`
  return {
    ...base,
    signal: scope.signal,
    within: (work, ms, what) => base.within(work, ms, what && `${id}: ${what}`, scope.signal),
    patchComponent: tracked(base.patchComponent),
    redux: {
      ...base.redux,
      patchState: tracked(base.redux.patchState),
      patchSlice: tracked(base.redux.patchSlice),
      patchThunk: tracked(base.redux.patchThunk),
    },
    experiments,
    rtm: { ...base.rtm, on: tracked(base.rtm.on) },
    huddles: { ...base.huddles, onPeerMessage: tracked(base.huddles.onPeerMessage), onActiveHuddle: tracked(base.huddles.onActiveHuddle) },
    workspace: { ...base.workspace, onChange: tracked(base.workspace.onChange) },
    messages: {
      ...base.messages,
      injectMessages: tracked(base.messages.injectMessages),
      patchMessageText: tracked(base.messages.patchMessageText),
      patchActionableMessage: tracked(base.messages.patchActionableMessage),
    },
    onMessageSendBlocks: tracked(base.onMessageSendBlocks),
    composer: {
      ...base.composer,
      patchInlineMarkup: tracked(base.composer.patchInlineMarkup),
      addButton: tracked(base.composer.addButton),
      addSendCheck: tracked(base.composer.addSendCheck),
      addLinkTransform: tracked(base.composer.addLinkTransform),
    },
    deferResizeWork: tracked(base.deferResizeWork),
    setStyle: tracked((css, key) => base.setStyle(css, key === undefined ? undefined : `plugin:${id}:${key}`)),
    modal: { ...base.modal, openModal, ...dialogHelpersFor(openModal) },
    preferences: { ...base.preferences, addTab: tracked(base.preferences.addTab) },
    sharedFrom: (pluginId, key) => {
      const h = base.sharedFrom(pluginId, key)
      return { ...h, subscribe: tracked(h.subscribe) }
    },
    storage: new ScopedStorage(scope.wrap(goBlob(storageNs)), storageNs, scope.signal),
    secrets: new ScopedStorage(scope.wrap(goBlob(secretsNs)), secretsNs, scope.signal),
    Cache: bindCache(scope.wrap(goBlob(cacheNs))),
    SharedStore: bindSharedStore(id, scope.signal),
  }
}

// ---- plugins ----

/** id → {Class, instance, scope} */
const plugins = new Map()

function describeEntries(defaults) {
  return defaultEntries(defaults).map((e) => ({
    key: e.key,
    value: e.value,
    comment: e.comment ?? null,
    label: e.label,
    kind: e.kind,
    hasEditor: typeof e.editor === 'function' || (e.editor && typeof e.editor === 'object'),
    hasCheck: typeof e.check === 'function',
  }))
}

function pluginMeta(Class) {
  const authors = Class.authors
  return {
    id: Class.id,
    name: typeof Class.pluginName === 'string' ? Class.pluginName : Class.id,
    description: typeof Class.description === 'string' ? Class.description : '',
    authors: typeof authors === 'string' ? authors : Array.isArray(authors) ? authors.map((a) => (typeof a === 'string' ? a : { name: a?.name, slackId: a?.slackId, url: a?.url })) : [],
    category: typeof Class.category === 'string' ? Class.category : null,
    hackClubOnly: Class.hackClubOnly === true,
    hasMigrate: typeof Class.migrateConfig === 'function',
    hasDataPanel: typeof Class.prototype?.dataPanel === 'function',
    options: describeEntries(Class.defaultConfig),
  }
}

/** Go → JS: evaluates a plugin bundle (an IIFE giving the class, or a module with it as default), returns its metadata */
T.load = (code) => {
  const result = new Function(`return ${code}`)()
  const Class = result?.prototype instanceof TautPlugin ? result : result?.default
  if (typeof Class !== 'function' || !(Class.prototype instanceof TautPlugin)) throw new Error('Plugin class does not extend TautPlugin')
  const id = Class.id
  if (typeof id !== 'string' || id === '.' || id === '..' || !id.length || id.length > 100 || !/^[A-Za-z0-9_.-]+$/.test(id)) {
    throw new Error(`Plugin has an invalid static id "${id}" (allowed: letters, numbers, "_", ".", "-")`)
  }
  try {
    validateDefaultConfig(Class.defaultConfig)
  } catch (err) {
    throw new Error(`Plugin ${id}: ${errorText(err)}`)
  }
  const existing = plugins.get(id)
  if (existing?.instance) throw new Error(`Plugin ${id} is running; stop it before loading new code`)
  plugins.set(id, { Class, instance: null, scope: null })
  return pluginMeta(Class)
}

const classOf = (id) => {
  const p = plugins.get(id)
  if (!p) throw new Error(`Plugin ${id} is not loaded`)
  return p.Class
}

/** Go → JS: runs migrateConfig on a copy of the saved block, the result or null if unchanged */
T.migrate = (id, saved) => {
  const Class = classOf(id)
  if (typeof Class.migrateConfig !== 'function' || !isPlainObject(saved)) return null
  const migrated = clone(saved)
  Class.migrateConfig(migrated)
  return deepEqual(migrated, saved) ? null : migrated
}

/** Go → JS: {config, problems} for a saved block (secrets are added by Go) */
T.resolve = (id, saved) => resolveConfig(classOf(id).defaultConfig, saved)

/** Go → JS: why `value` can't be used for option `key`, or null */
T.check = (id, key, value) => {
  const e = defaultEntries(classOf(id).defaultConfig).find((x) => x.key === key)
  if (!e) return null
  try {
    return checkOption(e, value)
  } catch (err) {
    return `check failed: ${errorText(err)}`
  }
}

/** Go → JS: starts the plugin with its resolved config; null, or the error to show */
T.start = async (id, config) => {
  const entry = plugins.get(id)
  if (!entry) return `Plugin ${id} is not loaded`
  if (entry.instance) await T.stop(id)
  const scope = createScope()
  let instance = null
  try {
    const api = createScopedAPI(id, scope)
    instance = new entry.Class(api, config)
    entry.instance = instance
    entry.scope = scope
    await withLifecycleTimeout(id, 'start', Promise.resolve().then(() => instance.start()))
    log(`Plugin ${id} started successfully`)
    return null
  } catch (err) {
    error(`Plugin ${id} failed to start:`, err)
    await stopRuntime(id, { instance, scope })
    entry.instance = null
    entry.scope = null
    return `Failed to start: ${errorText(err)}`
  }
}

async function stopRuntime(id, rt) {
  rt.scope?.abort()
  try {
    if (rt.instance) await withLifecycleTimeout(id, 'stop', Promise.resolve().then(() => rt.instance.stop()))
  } catch (err) {
    error(`Error stopping plugin ${id}:`, err)
    return errorText(err)
  } finally {
    await rt.scope?.dispose()
  }
  return null
}

/** Go → JS: stops a running plugin; null or the stop error (it's stopped either way) */
T.stop = async (id) => {
  const entry = plugins.get(id)
  if (!entry?.instance) return null
  const rt = { instance: entry.instance, scope: entry.scope }
  entry.instance = null
  entry.scope = null
  return stopRuntime(id, rt)
}

T.unload = async (id) => {
  await T.stop(id)
  plugins.delete(id)
}

T.running = () => [...plugins].filter(([, p]) => p.instance).map(([id]) => id)

// ---- settings: editors and data panels render through host trees ----

/** Go → JS: the element for option `key`'s custom editor, with value/onChange from Go */
T.editorElement = (id, key, value, onChangeId) => {
  const e = defaultEntries(classOf(id).defaultConfig).find((x) => x.key === key)
  if (!e?.editor) return null
  return G.React.createElement(e.editor, {
    value: decode(value),
    onChange: (v) => go('callGo', onChangeId, encode([v])),
    label: e.label,
    api: makeBaseAPI(),
  })
}
/** Go → JS: a running plugin's data panel */
T.dataPanelElement = (id) => {
  const p = plugins.get(id)
  if (!p?.instance || typeof p.instance.dataPanel !== 'function') return null
  const Panel = () => p.instance.dataPanel()
  return G.React.createElement(Panel)
}

/** Go → JS: once per runtime, after the host functions exist */
T.init = () => {
  makeBaseAPI()
  // Clack's native thunks: the work happens in Go (thunks.go)
  for (const name of go('thunkNames') || []) nativeThunk(name)
  return { version: 1 }
}

/** Go → JS: what plugins have registered, for settings diagnostics and tests */
T.introspect = () => ({
  running: T.running(),
  components: T.patchedComponents(),
  thunks: patchedThunkNames(),
  rtm: [...rtmListeners.keys()],
  buttons: composerButtons.get().map((b) => b.id),
  sendChecks: sendChecks.size,
  linkTransforms: linkTransforms.size,
  inlineMarkup: !!activeMarkup,
  sendTransforms: sendTransforms.size,
  actionable: actionableTransforms.size,
  styles: [...sheets.keys()],
  slices: [...sliceLayers.keys()],
  statePatches: statePatches.size,
  tabs: preferencesTabs.get().map((t) => t.label),
  overlays: [...overlays.values()].map((o) => o.kind + ':' + o.title),
  experiments: Object.fromEntries(forcedExperiments),
  resize: resizeRegs.size,
  peers: peerListeners.size,
})
