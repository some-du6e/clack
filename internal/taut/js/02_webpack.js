// Slack-equivalent "webpack" exports. In the web client Taut finds Slack's
// functions by name in the bundle. Clack has no bundle, so the runtime
// registers its own native-backed modules here under Slack's names and
// plugins find them through the same byName / byMeta / waitForExport calls.

/** module id → exports object */
const modules = new Map()
/** export value → owning module id */
const exportOwners = new WeakMap()
const moduleSources = new Map()
const pendingExportMatchers = new Set()
const moduleLoadedCallbacks = []

const byName = (name) => (exp) => typeof exp === 'function' && exp.name === name

const selectorNames = new WeakMap()
function nameSelector(selector, name) {
  selectorNames.set(selector, name)
}
const byMeta = (name) => (exp) => typeof exp === 'function' && (exp.meta?.name ?? selectorNames.get(exp)) === name

function candidatesOf(exports) {
  const out = [exports]
  if (exports && typeof exports === 'object') {
    for (const k of Object.keys(exports)) {
      try {
        out.push(exports[k])
      } catch {}
    }
  }
  return out
}

function matchIn(exports, matcher) {
  for (const c of candidatesOf(exports)) {
    try {
      if (matcher(c)) return { hit: true, value: c }
    } catch {}
  }
  return { hit: false }
}

/** names a function so byName finds it, even when it was made anonymously */
function named(name, fn) {
  try {
    Object.defineProperty(fn, 'name', { value: name, configurable: true })
  } catch {}
  return fn
}

/** registers (or extends) a module; `source` is what getModuleSources yields for it */
function defineModule(id, exports, source) {
  const key = String(id)
  const existing = modules.get(key)
  const target = existing || {}
  Object.assign(target, exports)
  modules.set(key, target)
  for (const k of Object.keys(exports)) {
    const v = exports[k]
    if (v && (typeof v === 'object' || typeof v === 'function')) exportOwners.set(v, key)
  }
  exportOwners.set(target, key)
  const src =
    source ??
    `function(e,t,n){"use strict";${Object.keys(target)
      .map((k) => `n.d(t,{${JSON.stringify(k)}:()=>${typeof target[k] === 'function' ? String(target[k]).slice(0, 2000) : JSON.stringify(k)}})`)
      .join(';')}}`
  moduleSources.set(key, src)
  for (const p of [...pendingExportMatchers]) {
    const m = matchIn(target, p.matcher)
    if (m.hit) {
      pendingExportMatchers.delete(p)
      p.resolve(m.value)
    }
  }
  for (const cb of moduleLoadedCallbacks) {
    try {
      cb(target)
    } catch {}
  }
  return target
}

function getExport(matcher, all = false) {
  const results = new Set()
  for (const exports of modules.values()) {
    for (const c of candidatesOf(exports)) {
      let ok = false
      try {
        ok = matcher(c)
      } catch {}
      if (!ok) continue
      if (!all) return c
      results.add(c)
    }
  }
  return all ? [...results] : undefined
}

function waitForExport(matcher) {
  const found = getExport(matcher)
  if (found !== undefined) return Promise.resolve(found)
  return new Promise((resolve) => pendingExportMatchers.add({ matcher, resolve }))
}

function forEachExport(matcher, cb) {
  for (const v of getExport(matcher, true)) cb(v)
  moduleLoadedCallbacks.push((exports) => {
    for (const c of candidatesOf(exports)) {
      try {
        if (matcher(c)) cb(c)
      } catch {}
    }
  })
}

function getByProps(props, all = false) {
  const m = (exp) => exp && typeof exp === 'object' && props.every((p) => p in exp)
  return all ? getExport(m, true) : getExport(m)
}

function* getModuleSources() {
  for (const [id, src] of moduleSources) yield [id, src]
}
function getModuleSource(id) {
  const s = moduleSources.get(String(id))
  if (s === undefined) throw new Error(`[Taut] No module found with id: ${String(id)}`)
  return s
}
function findModuleId(value) {
  if (!value || (typeof value !== 'object' && typeof value !== 'function')) return undefined
  return exportOwners.get(value)
}
function getValueSource(value) {
  const id = findModuleId(value)
  if (id !== undefined) return getModuleSource(id)
  if (typeof value === 'function') return value.toString()
  throw new Error('[Taut] Could not find a module or source for value')
}

/** Slack's `createThunk`-made creators carry `meta.name`; Clack's are made here */
function withMeta(name, fn) {
  named(name, fn)
  fn.meta = { name }
  return fn
}

Object.assign(T, { defineModule, named, withMeta, getExport, waitForExport, forEachExport, getByProps, getModuleSources, getModuleSource, findModuleId, getValueSource, byName, byMeta })
