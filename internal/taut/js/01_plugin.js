// The `$taut` module: TautPlugin, opt, labelFromKey, PLUGIN_CATEGORIES,
// WEEKDAYS, and how a plugin's defaultConfig turns into settings and config.

const PLUGIN_CATEGORIES = Object.freeze({
  messages: 'Messages',
  messageBox: 'Message Box',
  people: 'People',
  privacy: 'Privacy',
  app: 'App',
  fun: 'Fun',
})

const WEEKDAYS = Object.freeze(['sun', 'mon', 'tue', 'wed', 'thu', 'fri', 'sat'])

/** "keepFirstLetter" → "Keep first letter", "max2Lines" → "Max 2 lines", "snake_case" → "Snake case" */
function labelFromKey(key) {
  let s = String(key)
  s = s.replace(/([a-z0-9])([A-Z])/g, '$1 $2')
  s = s.replace(/([a-zA-Z])([0-9])/g, '$1 $2')
  s = s.replace(/[_-]+/g, ' ').trim().toLowerCase()
  return s ? s[0].toUpperCase() + s.slice(1) : s
}

/** one option of a plugin's defaultConfig: its default plus how settings shows it */
class Opt {
  constructor(value, comment, options) {
    this.value = value
    this.comment = comment
    this.label = options?.label
    this.editor = options?.editor
    this.check = options?.check
    this.kind = options?.kind
  }
}

function baseOpt(value, comment, options) {
  return new Opt(value, comment, options)
}

const opt = Object.assign(baseOpt, {
  select(choices, value, comment, options) {
    const normalized = [...choices].map((c) => (typeof c === 'string' ? { value: c, label: labelFromKey(c) } : { value: c.value, label: c.label }))
    return new Opt(value, comment, { ...options, kind: { type: 'select', options: normalized } })
  },
  list(value, comment, options) {
    return new Opt([...value], comment, { ...options, kind: { type: 'list' } })
  },
  color(value, comment, options) {
    return new Opt(value, comment, { ...options, kind: { type: 'color' } })
  },
  number(value, comment, options) {
    const { min, max, ...rest } = options || {}
    return new Opt(value, comment, { ...rest, kind: { type: 'number', min, max } })
  },
  secret(comment, options) {
    return new Opt('', comment, { ...options, kind: { type: 'secret' } })
  },
})

class TautPlugin {
  constructor(api, config) {
    this.api = api
    this.config = config
    this.log = this._log.bind(this)
  }
  start() {}
  stop() {}
  _log(...args) {
    console.log(`[Taut] [${this.constructor.pluginName}]`, ...args)
  }
}

// ---- defaultConfig → entries, checks, resolution ----

function inferKind(value, hasEditor) {
  switch (typeof value) {
    case 'boolean':
      return { type: 'boolean' }
    case 'number':
      return { type: 'number' }
    case 'string':
      return { type: 'string' }
  }
  const strings = !hasEditor && Array.isArray(value) && value.length > 0 && value.every((x) => typeof x === 'string')
  return { type: strings ? 'list' : 'json' }
}

function defaultEntries(defaults) {
  return Object.keys(defaults).map((key) => {
    const raw = defaults[key]
    const o = raw instanceof Opt ? raw : null
    const value = o ? o.value : raw
    return {
      key,
      value,
      comment: o?.comment,
      label: o?.label ?? labelFromKey(key),
      kind: o?.kind ?? inferKind(value, o?.editor !== undefined),
      editor: o?.editor,
      check: o?.check,
    }
  })
}

const isSecretEntry = (e) => e.kind.type === 'secret'

function checkJSONValue(v, path) {
  if (v === null) return
  switch (typeof v) {
    case 'string':
    case 'boolean':
      return
    case 'number':
      if (!Number.isFinite(v)) throw new Error(`defaultConfig.${path} is not a finite number`)
      return
    case 'object':
      if (Array.isArray(v)) {
        v.forEach((x, i) => checkJSONValue(x, `${path}[${i}]`))
        return
      }
      if (isPlainObject(v)) {
        for (const k of Object.keys(v)) checkJSONValue(v[k], `${path}.${k}`)
        return
      }
  }
  throw new Error(`defaultConfig.${path} must be JSON (string, number, boolean, null, array, or object)`)
}

function validateDefaultConfig(defaults) {
  if (!isPlainObject(defaults)) throw new Error('defaultConfig must be a plain object')
  for (const key of Object.keys(defaults)) {
    if (!key.trim()) throw new Error('defaultConfig has an empty key')
    const raw = defaults[key]
    if (raw instanceof Opt && typeof raw.comment !== 'string') throw new Error(`defaultConfig.${key} has a non-string comment`)
    checkJSONValue(raw instanceof Opt ? raw.value : raw, key)
  }
  const en = defaults.enabled instanceof Opt ? defaults.enabled.value : defaults.enabled
  if (typeof en !== 'boolean') throw new Error('defaultConfig.enabled must be a boolean')
  for (const e of defaultEntries(defaults)) {
    const p = checkOption(e, e.value)
    if (p) throw new Error(`defaultConfig.${e.key}: ${p}`)
  }
}

const show = (v) => (v === undefined ? 'nothing' : JSON.stringify(v))

function checkKind(entry, value) {
  const kind = entry.kind
  const found = show(value)
  switch (kind.type) {
    case 'boolean':
      return typeof value === 'boolean' ? null : `expected true or false, found ${found}`
    case 'number':
      return typeof value === 'number' && Number.isFinite(value) ? null : `expected a number, found ${found}`
    case 'string':
      return typeof value === 'string' ? null : `expected text, found ${found}`
    case 'secret':
      return typeof value === 'string' ? null : 'expected text'
    case 'select':
      return kind.options.some((o) => o.value === value) ? null : `expected one of ${kind.options.map((o) => JSON.stringify(o.value)).join(', ')}, found ${found}`
    case 'list':
      return Array.isArray(value) && value.every((x) => typeof x === 'string') ? null : `expected a list of text, found ${found}`
    case 'color':
      if (typeof value !== 'string') return `expected a color, found ${found}`
      return isCSSColor(value) ? null : `${found} isn't a CSS color`
    case 'json': {
      const d = entry.value
      if (d === null) return null
      if (Array.isArray(d)) return Array.isArray(value) ? null : `expected a list, found ${found}`
      if (typeof d === 'object') return isPlainObject(value) ? null : `expected an object, found ${found}`
      return null
    }
  }
  return null
}

function checkOption(entry, value) {
  const p = checkKind(entry, value)
  if (p) return p
  if (!entry.check) return null
  const r = entry.check(value)
  return r ?? null
}

function unwrapDefaults(defaults) {
  const out = {}
  for (const e of defaultEntries(defaults)) out[e.key] = e.value
  return out
}

/** the saved block over the defaults; an invalid value keeps its default and is reported in `problems` */
function resolveConfig(defaults, block) {
  const config = clone(unwrapDefaults(defaults))
  const problems = {}
  if (!isPlainObject(block)) return { config, problems }
  const entries = new Map(defaultEntries(defaults).map((e) => [e.key, e]))
  for (const key of Object.keys(block)) {
    const e = entries.get(key)
    let p = null
    try {
      p = e ? checkOption(e, block[key]) : null
    } catch (err) {
      p = `check failed: ${errorText(err)}`
    }
    if (p) {
      problems[key] = p
      continue
    }
    config[key] = block[key]
  }
  return { config, problems }
}

// ---- CSS colors (CSS.supports('color', v) without a CSS engine) ----
const NAMED_COLORS = new Set(
  ('aliceblue antiquewhite aqua aquamarine azure beige bisque black blanchedalmond blue blueviolet brown burlywood cadetblue chartreuse ' +
    'chocolate coral cornflowerblue cornsilk crimson cyan darkblue darkcyan darkgoldenrod darkgray darkgreen darkgrey darkkhaki darkmagenta ' +
    'darkolivegreen darkorange darkorchid darkred darksalmon darkseagreen darkslateblue darkslategray darkslategrey darkturquoise darkviolet ' +
    'deeppink deepskyblue dimgray dimgrey dodgerblue firebrick floralwhite forestgreen fuchsia gainsboro ghostwhite gold goldenrod gray green ' +
    'greenyellow grey honeydew hotpink indianred indigo ivory khaki lavender lavenderblush lawngreen lemonchiffon lightblue lightcoral lightcyan ' +
    'lightgoldenrodyellow lightgray lightgreen lightgrey lightpink lightsalmon lightseagreen lightskyblue lightslategray lightslategrey ' +
    'lightsteelblue lightyellow lime limegreen linen magenta maroon mediumaquamarine mediumblue mediumorchid mediumpurple mediumseagreen ' +
    'mediumslateblue mediumspringgreen mediumturquoise mediumvioletred midnightblue mintcream mistyrose moccasin navajowhite navy oldlace olive ' +
    'olivedrab orange orangered orchid palegoldenrod palegreen paleturquoise palevioletred papayawhip peachpuff peru pink plum powderblue purple ' +
    'rebeccapurple red rosybrown royalblue saddlebrown salmon sandybrown seagreen seashell sienna silver skyblue slateblue slategray slategrey snow ' +
    'springgreen steelblue tan teal thistle tomato turquoise violet wheat white whitesmoke yellow yellowgreen transparent currentcolor ' +
    'canvas canvastext linktext visitedtext activetext buttonface buttontext buttonborder field fieldtext highlight highlighttext selecteditem ' +
    'selecteditemtext mark marktext graytext accentcolor accentcolortext'
  ).split(' ')
)
const NUM = '[+-]?(?:\\d+\\.?\\d*|\\.\\d+)(?:e[+-]?\\d+)?'
const COLOR_FN = new RegExp(`^(rgba?|hsla?|hwb|lab|lch|oklab|oklch|color|color-mix|light-dark)\\((.*)\\)$`, 'i')
function isCSSColor(value) {
  const v = String(value).trim().toLowerCase()
  if (!v) return false
  if (NAMED_COLORS.has(v)) return true
  if (/^#([0-9a-f]{3}|[0-9a-f]{4}|[0-9a-f]{6}|[0-9a-f]{8})$/.test(v)) return true
  if (/^var\(--[\w-]+(\s*,.*)?\)$/.test(v)) return true
  const m = COLOR_FN.exec(v)
  if (!m) return false
  const fn = m[1]
  const body = m[2].trim()
  if (fn === 'color-mix' || fn === 'light-dark' || fn === 'color') return body.length > 0
  const parts = body.split(/[\s,/]+/).filter(Boolean)
  const tok = new RegExp(`^(${NUM}(%|deg|rad|grad|turn)?|none|from|r|g|b|h|s|l|w|alpha)$`)
  return parts.length >= 3 && parts.length <= 5 && parts.every((p) => tok.test(p) || /^var\(/.test(p))
}

// Taut's bundles read these globals (build:user-plugin shims `$taut` to them)
G.TautPlugin = TautPlugin
G.TautOpt = opt

T.$taut = {
  TautPlugin,
  default: TautPlugin,
  opt,
  Opt,
  labelFromKey,
  PLUGIN_CATEGORIES,
  WEEKDAYS,
}
