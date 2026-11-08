// The Slack-facing parts of the TautAPI over the mirror and Go: members,
// channels, messages, rtm, workspace, profile, threads, huddles, files,
// blocks, experiments, composer hooks, message send transforms, userAPI,
// RateLimit, accounts, cookies, setStyle, scheduleEnd, deferResizeWork and
// the Slack-equivalent named exports (I18n, getPrefCompliantMemberName, …).

const deburr = (s) => String(s).normalize('NFKD').replace(/[̀-ͯ]/g, '')
const lc = (s) => String(s).toLowerCase()

// ---- members ----

const loadedMember = (m) => (!m || m.isUnknown === true || m.isNonExistent === true ? undefined : m)
const getCachedMember = (id) => loadedMember(getState().members?.[id])
const getCurrentMemberId = () => getRawState().bootData?.user_id

const memberRequests = new Map()
let memberBatch
function fetchMembers(id) {
  if (!memberBatch) {
    const ids = new Set()
    const done = sleep(5).then(async () => {
      memberBatch = undefined
      try {
        await dispatchThunk('ensureMembersArePresent', { memberIds: [...ids], reason: 'taut' })
      } catch {}
    })
    memberBatch = { ids, done }
  }
  memberBatch.ids.add(id)
  return memberBatch.done
}
function getMember(id) {
  const cached = getCachedMember(id)
  if (cached) return Promise.resolve(cached)
  const pending = memberRequests.get(id)
  if (pending) return pending
  const req = retry(
    async () => {
      await fetchMembers(id)
      return getCachedMember(id)
    },
    { tries: 4, baseMs: 3000 }
  ).finally(() => memberRequests.delete(id))
  memberRequests.set(id, req)
  return req
}
function useMember(id) {
  const React = G.React
  const member = useReduxState((s) => loadedMember(s?.members?.[id]))
  const missing = !member
  React.useEffect(() => {
    if (id && missing) getMember(id)
  }, [id, missing])
  return member
}
function modifyMemberObject(member, edits) {
  const profile = { ...member.profile }
  const next = { ...member, profile }
  const { name, displayName = name, realName = name } = edits || {}
  if (displayName !== undefined) {
    profile.display_name = displayName
    profile.display_name_normalized = deburr(displayName)
    next._display_name_lc = lc(displayName)
    next._display_name_normalized_lc = deburr(lc(displayName))
  }
  if (realName !== undefined) {
    next.real_name = realName
    profile.real_name = realName
    profile.real_name_normalized = deburr(realName)
    next._real_name_lc = lc(realName)
    next._real_name_normalized_lc = deburr(lc(realName))
  }
  return next
}
const members = { getCachedMember, getCurrentMemberId, getMember, useMember, modifyMemberObject }

// ---- channels ----

function makeChannelObject({ id, name, isPrivate = false, isMember = false, isArchived = false, previousNames = [] }) {
  return {
    id,
    name,
    name_normalized: deburr(name),
    _name_lc: deburr(lc(name)),
    is_channel: !isPrivate,
    is_group: isPrivate,
    is_im: false,
    is_mpim: false,
    is_private: isPrivate,
    is_member: isMember,
    is_archived: isArchived,
    is_general: false,
    previous_names: previousNames,
    isNonExistent: false,
    isUnknown: false,
  }
}
const getCachedChannel = (id) => getState().channels?.[id]
const channels = { getCachedChannel, makeChannelObject }

// ---- messages ----

const getMessageBotId = (m) => m?.bot_id ?? m?.bot_profile?.id ?? m?.taut_bot_id
const getRawMessage = (ch, ts) => getRawState().messages?.[ch]?.[ts]
const asRawMessage = (m) => (typeof m?.channel === 'string' && m.ts && getRawMessage(m.channel, m.ts)) || m

const TEXT_FIELDS = ['text', 'blocks', 'blocksProcessed', 'attachments', 'extracts', 'blocks_extracts', 'blocks_extracts_typed']
const TEXT_KEYS = new Set(['text', 'fallback', 'title', 'pretext', 'footer'])
const LITERAL_TYPES = new Set(['text', 'link', 'plain_text'])
const MRKDWN_TOKEN = /<([^<>|]*)(?:\|([^<>]*))?>|:[\w+'-]+:|&(?:amp|lt|gt);/g

function transformMrkdwn(text, fn) {
  let out = ''
  let last = 0
  for (const m of text.matchAll(MRKDWN_TOKEN)) {
    const [tok, target, label] = m
    out += fn(text.slice(last, m.index))
    out += label !== undefined && !/^[@#!]/.test(target) ? `<${target}|${transformMrkdwn(label, fn)}>` : tok
    last = m.index + tok.length
  }
  return last === 0 ? fn(text) : out + fn(text.slice(last))
}
function transformDeep(v, fn) {
  if (Array.isArray(v)) {
    let changed = false
    const next = v.map((x) => {
      const o = transformDeep(x, fn)
      if (o !== x) changed = true
      return o
    })
    return changed ? next : v
  }
  if (!v || typeof v !== 'object') return v
  const literal = LITERAL_TYPES.has(v.type)
  let next = null
  for (const [k, x] of Object.entries(v)) {
    const o = TEXT_KEYS.has(k) && typeof x === 'string' ? (literal ? fn(x) : transformMrkdwn(x, fn)) : transformDeep(x, fn)
    if (o === x) continue
    next ??= { ...v }
    next[k] = o
  }
  return next ?? v
}
function withMessageText(msg, fn) {
  let next = null
  for (const f of TEXT_FIELDS) {
    const v = msg[f]
    const o = typeof v === 'string' ? transformMrkdwn(v, fn) : transformDeep(v, fn)
    if (o === v) continue
    next ??= { ...msg }
    next[f] = o
  }
  return next ?? msg
}

const FINISHING_EDIT_SECONDS = 3
function sectionsMrkdwn(blocks) {
  if (!Array.isArray(blocks) || !blocks.length) return undefined
  const texts = []
  for (const b of blocks) {
    const keys = Object.keys(b ?? {}).filter((k) => k !== 'block_id' && k !== 'blockId')
    if (b?.type !== 'section' || b.text?.type !== 'mrkdwn' || typeof b.text.text !== 'string' || keys.some((k) => k !== 'type' && k !== 'text')) return undefined
    texts.push(b.text.text)
  }
  return texts.join('\n')
}
function modifyMessageObject(message, edits) {
  const next = { ...message }
  if (edits?.sentBy !== undefined) {
    next.user = edits.sentBy
    const bot = getMessageBotId(message)
    if (bot) next.taut_bot_id = bot
    for (const k of ['bot_id', 'app_id', 'username', 'icons', 'bot_profile', 'display_as_bot']) delete next[k]
    if (next.subtype === 'bot_message') delete next.subtype
    const edited = next.edited?.ts
    if (edited && Number(edited) - Number(next.ts) < FINISHING_EDIT_SECONDS) delete next.edited
  }
  if (edits?.typed) {
    const text = sectionsMrkdwn(next.blocks)
    if (text !== undefined) {
      next.text = text
      delete next.blocks
      delete next.blocksProcessed
    }
  }
  return next
}

const actionableTransforms = new Set()
let actionableMemo = new WeakMap()
let actionableVersion = -1
function asActionable(m) {
  if (!actionableTransforms.size || !m || typeof m !== 'object') return m
  if (actionableVersion !== patchVersion) {
    actionableVersion = patchVersion
    actionableMemo = new WeakMap()
  }
  const known = actionableMemo.get(m)
  if (known) return known
  let out = m
  for (const t of actionableTransforms) {
    try {
      out = t(out) ?? out
    } catch (err) {
      error('Actionable message transform failed:', err)
    }
  }
  actionableMemo.set(m, out)
  return out
}
const tellGoActionable = batcher(() => goOr(null, 'actionablePatched', actionableTransforms.size > 0))
function patchActionableMessage(t) {
  actionableTransforms.add(t)
  actionableMemo = new WeakMap()
  tellGoActionable()
  return () => {
    actionableTransforms.delete(t)
    actionableMemo = new WeakMap()
    tellGoActionable()
  }
}
/** Go → JS: the message Slack's edit/delete checks and the editor see */
T.actionable = (ch, ts) => {
  const m = getRawMessage(ch, ts)
  return m ? encode(asActionable(m)) : null
}

const metadataKnown = new Map()
const metadataPending = new Map()
/** a mirrored message carries its metadata already (core loads history with include_all_metadata) */
function storedMetadata(ch, ts) {
  const m = getRawMessage(ch, ts)
  if (!m) return undefined
  if (m.is_ephemeral) return null
  return m.metadata ?? null
}
function fetchMessageMetadata(ch, ts, signal) {
  const key = `${ch}:${ts}`
  if (metadataKnown.has(key)) return Promise.resolve(metadataKnown.get(key))
  const stored = storedMetadata(ch, ts)
  if (stored !== undefined) return Promise.resolve(stored)
  if (signal?.aborted) return Promise.reject(signal.reason)
  let p = metadataPending.get(key)
  if (!p) {
    p = Promise.resolve(go('messageMetadata', ch, ts)).then(
      (md) => {
        metadataKnown.set(key, md ?? null)
        metadataPending.delete(key)
        refreshSoon()
        return md ?? null
      },
      (err) => {
        metadataPending.delete(key)
        metadataFailed.add(key)
        throw err
      }
    )
    metadataPending.set(key, p)
  }
  if (!signal) return p
  return new Promise((resolve, reject) => {
    const onAbort = () => reject(signal.reason)
    signal.addEventListener('abort', onAbort, { once: true })
    p.then(resolve, reject).finally(() => signal.removeEventListener('abort', onAbort))
  })
}
const metadataFailed = new Set()
let refreshTimer
const refreshSoon = () => {
  clearTimeout(refreshTimer)
  refreshTimer = setTimeout(refresh, 100)
}
function getMessageMetadata(ch, ts, signal) {
  const key = `${ch}:${ts}`
  if (metadataKnown.has(key) || metadataFailed.has(key)) return metadataKnown.get(key)
  const stored = storedMetadata(ch, ts)
  if (stored !== undefined) return stored
  fetchMessageMetadata(ch, ts, signal).catch((err) => {
    if (!signal?.aborted) warn('Could not look up metadata for', key, err)
  })
  return undefined
}

const historyChannel = (k) => k.split('-')[0]
const historyThread = (k) => (k.includes('-') ? k.slice(k.indexOf('-') + 1) : undefined)
function inHistory(m, thread) {
  const parent = typeof m.thread_ts === 'string' ? m.thread_ts : undefined
  if (thread !== undefined) return parent === thread
  return parent === undefined || parent === m.ts || m.subtype === 'thread_broadcast'
}
function withTimestamps(history, added) {
  const slices = history.slices ?? []
  const last = slices.length - 1
  let changed = false
  const next = slices.map((slice, pos) => {
    const { timestamps, start, end } = slice
    if (!Array.isArray(timestamps)) return slice
    const covers = (ts) =>
      (start == null || ts >= start || (pos === 0 && history.reachedStart === true)) && (end == null || ts <= end || (pos === last && history.reachedEnd === true))
    const missing = added.filter((ts) => covers(ts) && !timestamps.includes(ts))
    if (!missing.length) return slice
    changed = true
    return { ...slice, timestamps: [...timestamps, ...missing].sort() }
  })
  return changed ? next : slices
}
function injectMessages(getMessages) {
  let at = -1
  let byChannel = new Map()
  const index = () => {
    if (at === patchVersion) return byChannel
    at = patchVersion
    byChannel = new Map()
    for (const m of getMessages()) {
      if (typeof m?.channel !== 'string' || typeof m.ts !== 'string') continue
      let b = byChannel.get(m.channel)
      if (!b) byChannel.set(m.channel, (b = new Map()))
      b.set(m.ts, m)
    }
    return byChannel
  }
  const unMessages = patchSlice(
    'messages',
    (ch, bucket) => {
      const injected = index().get(ch)
      if (!injected?.size) return bucket
      return mapEntries(bucket ?? {}, (ts, m) => injected.get(ts) ?? m, () => injected.keys())
    },
    () => index().keys()
  )
  const unHistory = patchSlice('channelHistory', (key, entry) => {
    const slices = entry?.slices
    if (!entry || !Array.isArray(slices)) return entry
    const injected = index().get(historyChannel(key))
    if (!injected?.size) return entry
    const thread = historyThread(key)
    const added = [...injected.values()].filter((m) => inHistory(m, thread)).map((m) => m.ts)
    if (!added.length) return entry
    const next = withTimestamps(entry, added)
    return next === slices ? entry : { ...entry, slices: next }
  })
  goOr(null, 'injecting', true)
  return () => {
    unMessages()
    unHistory()
  }
}

function mapAt(v, path, fn) {
  if (!v || typeof v !== 'object') return v
  if (!path.length) return fn(v)
  const [head, ...rest] = path
  const keys = head === '*' ? Object.keys(v) : [head]
  let next = null
  for (const k of keys) {
    const item = v[k]
    const o = mapAt(item, rest, fn)
    if (o === item) continue
    next ??= Array.isArray(v) ? [...v] : { ...v }
    next[k] = o
  }
  return next ?? v
}
function editingMessages(state) {
  const w = state?.messageEditWindow
  const keys = new Set()
  if (!w || typeof w !== 'object') return keys
  for (const e of Object.values(w)) if (e?.channelId && e.ts) keys.add(`${e.channelId}:${e.ts}`)
  return keys
}
function patchMessageText(transform) {
  const withText = (m) => withMessageText(m, transform)
  let editing = new Set()
  let editWindows
  const stop = subscribe(() => {
    const w = getRawState()?.messageEditWindow
    if (w === editWindows) return
    editWindows = w
    editing = editingMessages(getRawState())
    refresh()
  })
  const byChannel = new Map()
  const mapperFor = (ch) => {
    let m = byChannel.get(ch)
    if (!m) {
      m = (ts, msg) => (msg && !editing.has(`${ch}:${ts}`) ? withText(msg) : msg)
      byChannel.set(ch, m)
    }
    return m
  }
  const unMessages = patchSlice('messages', (ch, bucket) => (bucket && typeof bucket === 'object' ? mapEntries(bucket, mapperFor(ch)) : bucket))
  const SEARCH = ['response', 'results', 'messages', 'items', '*', '*', 'messages', '*']
  const unSearch = patchSlice('searchResults', (_i, e) => mapAt(e, SEARCH, withText))
  const unSuggest = patchThunk('searchInline', (original) => (...params) => {
    const thunk = original(...params)
    return (...a) => Promise.resolve(thunk(...a)).then((r) => mapAt(r, ['items', '*'], withText))
  })
  const unNotify = patchThunk('showNotification', (original) => (args) => original(args?.message ? { ...args, message: withText(args.message) } : args))
  return () => {
    stop()
    unMessages()
    unSearch()
    unSuggest()
    unNotify()
  }
}

function useActivityMessage(item) {
  const msg = useReduxState(() => (item?.channelId && item.messageTs ? getRawMessage(item.channelId, item.messageTs) : undefined))
  const sender = item?.senderType === 'app' ? undefined : (item?.senderId ?? item?.userId ?? msg?.user)
  return msg && { ...msg, user: sender }
}
function useMessageBot(msg) {
  const botId = useReduxState(() => getMessageBotId(asRawMessage(msg)))
  const stored = useReduxState((s) => (botId ? s.bots?.[botId] : undefined))
  if (!botId) return undefined
  return stored ?? asRawMessage(msg)?.bot_profile ?? { id: botId }
}

const messages = {
  getRawMessage,
  asRawMessage,
  getMessageBotId,
  injectMessages,
  patchMessageText,
  modifyMessageObject,
  fetchMessageMetadata,
  getMessageMetadata,
  patchActionableMessage,
  useActivityMessage,
  useMessageBot,
}

// ---- rtm ----

const rtmListeners = new Map()
function rtmEmit(type, ev) {
  if (!type || !ev) return
  for (const k of [type, '*']) {
    const set = rtmListeners.get(k)
    if (!set) continue
    for (const l of [...set]) {
      try {
        l(ev)
      } catch (err) {
        error(`RTM listener for ${k} threw`, err)
      }
    }
  }
}
/** Go → JS: one raw realtime event, as Slack's socket delivered it */
T.rtmEvent = (ev) => rtmEmit(ev?.type, ev)
T.rtmTypes = () => [...rtmListeners.keys()]
const tellGoRtm = batcher(() => goOr(null, 'rtmTypes', [...rtmListeners.keys()]))
function onRtm(type, listener) {
  let set = rtmListeners.get(type)
  if (!set) rtmListeners.set(type, (set = new Set()))
  set.add(listener)
  tellGoRtm()
  return () => {
    set.delete(listener)
    if (!set.size) rtmListeners.delete(type)
    tellGoRtm()
  }
}
const rtm = { on: onRtm }

// ---- workspace ----

const HACK_CLUB_IDS = new Set(['E09V59WQY1E', 'T0266FRGM'])
let hcFor
let hcResult = false
function isHackClub() {
  const self = getRawState().selfTeamIds
  if (self && self === hcFor) return hcResult
  const ids = self ? [self.teamId, self.enterpriseId, ...(self.internalWorkspaceIds ?? [])] : []
  const r = ids.some((id) => typeof id === 'string' && HACK_CLUB_IDS.has(id))
  if (self) {
    hcFor = self
    hcResult = r
  }
  return r
}
function onWorkspaceChange(listener) {
  let last = workspace.isHackClub()
  return subscribe(() => {
    const next = workspace.isHackClub()
    if (next === last) return
    last = next
    listener()
  })
}
const workspace = {
  isHackClub,
  onChange: onWorkspaceChange,
  useIsHackClub: () => G.React.useSyncExternalStore(workspace.onChange, () => workspace.isHackClub()),
}

// ---- userAPI, RateLimit, fetch, cookies ----

async function userAPI(method, params = {}, options = {}) {
  const fields = {}
  const files = {}
  for (const [k, v] of Object.entries(params)) {
    if (typeof Blob !== 'undefined' && v instanceof Blob) {
      files[k] = { name: v.name ?? 'blob', type: v.type, data: Array.from(new Uint8Array(await v.arrayBuffer())) }
    } else fields[k] = String(v)
  }
  const retries = options.rateLimitRetries ?? 0
  for (let attempt = 0; ; attempt++) {
    options.signal?.throwIfAborted?.()
    const res = await go('api', method, fields, files)
    if (res && res.status === 429) {
      if (attempt >= retries) throw new Error(`[Taut] userAPI ${method} rate limited`)
      await abortableSleep((res.retryAfter > 0 ? res.retryAfter : 2) * 1000, options.signal)
      continue
    }
    if (res && res.status && res.status !== 200) throw new Error(`[Taut] userAPI ${method} HTTP ${res.status}: ${res.text ?? ''}`)
    const json = res?.json ?? res
    if (!json?.ok) {
      if ((json?.error === 'ratelimited' || json?.error === 'rate_limited') && attempt < retries) {
        await abortableSleep(2000, options.signal)
        continue
      }
      throw new Error(`[Taut] userAPI ${method} failed: ${JSON.stringify(json)}`)
    }
    return json
  }
}

const rateWindows = new Map()
class RateLimit {
  constructor(key, options) {
    this.key = key
    this.options = options
  }
  take() {
    const { max, window } = this.options
    const lock = `taut:rate-limit:${getRawState().selfTeamIds?.teamId ?? ''}:${this.key}`
    return withLock(lock, () => {
      const now = Date.now()
      const sent = (rateWindows.get(lock) || []).filter((at) => at > now - window)
      if (sent.length >= max) {
        rateWindows.set(lock, sent)
        return sent[sent.length - max] + window - now
      }
      sent.push(now)
      rateWindows.set(lock, sent)
      return 0
    })
  }
  async wait(signal) {
    signal?.throwIfAborted?.()
    let w = await this.take()
    while (w) {
      await abortableSleep(w, signal)
      w = await this.take()
    }
  }
}

/** Taut's privileged fetch: no CORS, cookies only when asked for in headers */
function bridgeFetch(url, init) {
  return G.fetch(url, init)
}

const cookies = {
  get: (q) => Promise.resolve(go('cookie', 'get', q)),
  set: (c) => Promise.resolve(go('cookie', 'set', c)),
  remove: (q) => Promise.resolve(go('cookie', 'remove', q)),
  getAll: (q) => Promise.resolve(go('cookie', 'getAll', q || {})),
}

class AccountSwitcher {
  constructor() {
    this.supported = !!goOr(false, 'accountsSupported')
  }
  list() {
    return Promise.resolve(go('accounts', 'list'))
  }
  forget(userId) {
    return Promise.resolve(go('accounts', 'forget', userId))
  }
  validate() {
    return Promise.resolve(go('accounts', 'validate'))
  }
  captureCurrent() {
    return Promise.resolve(go('accounts', 'captureCurrent'))
  }
  switchTo(userId) {
    return Promise.resolve(go('accounts', 'switchTo', userId))
  }
  addAccount() {
    return Promise.resolve(go('accounts', 'addAccount'))
  }
}

// ---- profile ----

const selfMember = () => {
  const id = getCurrentMemberId()
  return id ? getCachedMember(id) : undefined
}
const profile = {
  getStatus() {
    const p = selfMember()?.profile
    if (!p) return undefined
    return {
      text: String(p.status_text ?? ''),
      emoji: String(p.status_emoji ?? ''),
      expiration: Number(p.status_expiration ?? 0) * 1000,
      canonical: String(p.status_text_canonical ?? ''),
    }
  },
  async setStatus(status) {
    const { text = '', emoji = '', expiration = 0, canonical = '' } = status || {}
    const p = { status_text: text, status_emoji: emoji, status_expiration: Math.ceil(expiration / 1000), status_text_canonical: canonical }
    if (status?.oooMessage !== undefined) p.ooo_message = status.oooMessage.length ? JSON.stringify(status.oooMessage) : ''
    await userAPI('users.profile.set', { profile: JSON.stringify(p) }, { rateLimitRetries: 2 })
  },
  getPresence() {
    const manual = selfMember()?.manual_presence
    if (manual === undefined) return undefined
    return manual === 'away' ? 'away' : 'auto'
  },
  async setPresence(presence) {
    await userAPI('users.setPresence', { presence }, { rateLimitRetries: 2 })
  },
  getSnooze() {
    const s = getRawState().dndV2?.currentMemberSnooze
    return s?.snoozeEnabled && s.snoozeEndTs ? s.snoozeEndTs * 1000 : null
  },
  async setSnooze(until) {
    if (until === null) {
      await userAPI('dnd.endSnooze', {}, { rateLimitRetries: 2 })
      return
    }
    const minutes = Math.max(1, Math.ceil((until - Date.now()) / 60000))
    await userAPI('dnd.setSnooze', { num_minutes: String(minutes) }, { rateLimitRetries: 2 })
  },
  getTimeZone() {
    const tz = selfMember()?.tz
    return typeof tz === 'string' && tz ? tz : undefined
  },
}

// ---- threads ----

let paneContext
const PaneContext = () => (paneContext ??= G.React.createContext(null))
/** the pane native thread views pass in props as $pane: {channelId, threadTs, windowId, loaded} */
function paneFrom(info) {
  if (!info?.channelId || !info.threadTs) return null
  return {
    channelId: info.channelId,
    threadTs: info.threadTs,
    windowId: info.windowId ?? 'main',
    loaded: () => goOr(info.loaded ?? [], 'threadPane', 'loaded', info.channelId, info.threadTs) ?? [],
    topReply: () => goOr(undefined, 'threadPane', 'topReply', info.channelId, info.threadTs) ?? undefined,
    jumpToReply: (ts) => Promise.resolve(go('threadPane', 'jumpToReply', info.channelId, info.threadTs, ts)).then(Boolean),
  }
}
const threads = { usePane: () => G.React.useContext(PaneContext()) }

// ---- huddles ----

const peerListeners = new Set()
const activeHuddle = new Store(undefined)
T.peerMessage = (event) => {
  for (const l of [...peerListeners]) {
    try {
      l(event)
    } catch (err) {
      error('Huddle peer message listener threw', err)
    }
  }
}
T.activeHuddle = (h) => activeHuddle.set(h || undefined)
const huddles = {
  onPeerMessage(l) {
    peerListeners.add(l)
    return () => peerListeners.delete(l)
  },
  getAttendeeUserId(attendeeId) {
    const st = getState()
    const peers = st?.peers
    return (peers?.byId?.[attendeeId] ?? peers?.leftById?.[attendeeId])?.userId
  },
  getCall: (roomId) => getState().allCalls?.byId?.[roomId],
  onActiveHuddle(l) {
    l(activeHuddle.get())
    return activeHuddle.subscribe(() => l(activeHuddle.get()))
  },
  getActiveHuddle: () => activeHuddle.get(),
  async sendReaction(reaction) {
    if (!activeHuddle.get()) return false
    return !!(await go('huddleReaction', reaction))
  },
}

// ---- files ----

async function uploadFile(file) {
  const pending = await dispatchThunk('addAndUploadPendingFile', { file, hideBanner: true })
  // Slack's thunk gives {uploadPromise}; Clack's native gives the result itself
  const id = (await (pending?.uploadPromise ?? pending))?.fileIds?.[0]
  if (!id) throw new Error('[Taut] Slack rejected the upload')
  return id
}
const files = { upload: uploadFile }

// ---- blocks (Block Kit and Quill deltas) ----

const isContainer = (v) => !!v && typeof v === 'object' && Array.isArray(v.elements)
function mapContainer(c, map) {
  const next = []
  let changed = false
  for (const el of c.elements ?? []) {
    if (isContainer(el)) {
      const m = mapContainer(el, map)
      if (m !== el) changed = true
      next.push(m)
      continue
    }
    const m = map(el, { inCode: c.type === 'rich_text_preformatted' || !!el?.style?.code, container: c.type ?? '' })
    if (Array.isArray(m)) {
      changed = true
      next.push(...m)
    } else {
      if (m !== el) changed = true
      next.push(m)
    }
  }
  return changed ? { ...c, elements: next } : c
}
function mapRichTextElements(blocks, map) {
  let changed = false
  const next = blocks.map((b) => {
    if (b?.type !== 'rich_text' || !isContainer(b)) return b
    const m = mapContainer(b, map)
    if (m !== b) changed = true
    return m
  })
  return changed ? next : blocks
}
function mapRichTextSections(blocks, map) {
  let index = 0
  const visit = (c, parent) => {
    const els = c.elements ?? []
    if (c.type !== 'rich_text' && !els.some(isContainer)) {
      const m = map(els, { container: c.type ?? '', parent, index: index++ })
      return m === els ? c : { ...c, elements: m }
    }
    let changed = false
    const next = els.map((el) => {
      if (!isContainer(el)) return el
      const m = visit(el, c.type ?? '')
      if (m !== el) changed = true
      return m
    })
    return changed ? { ...c, elements: next } : c
  }
  let changed = false
  const next = blocks.map((b) => {
    if (b?.type !== 'rich_text' || !isContainer(b)) return b
    const m = visit(b, '')
    if (m !== b) changed = true
    return m
  })
  return changed ? next : blocks
}

/** a Quill-style delta (the ops Slack's composer keeps drafts in) */
class Delta {
  constructor(ops) {
    this.ops = Array.isArray(ops) ? ops : ops?.ops ?? []
  }
  insert(arg, attributes) {
    const op = { insert: arg }
    if (attributes && Object.keys(attributes).length) op.attributes = attributes
    this.ops.push(op)
    return this
  }
  retain(n, attributes) {
    const op = { retain: n }
    if (attributes && Object.keys(attributes).length) op.attributes = attributes
    this.ops.push(op)
    return this
  }
  delete(n) {
    this.ops.push({ delete: n })
    return this
  }
  concat(other) {
    return new Delta([...this.ops, ...other.ops])
  }
  length() {
    return this.ops.reduce((n, op) => n + (typeof op.insert === 'string' ? op.insert.length : op.insert ? 1 : op.retain ?? op.delete ?? 0), 0)
  }
  compose(other) {
    // applies `other` to this document (inserts, deletes and attribute retains)
    const out = []
    const chars = []
    for (const op of this.ops) {
      if (typeof op.insert === 'string') for (const ch of op.insert) chars.push({ insert: ch, attributes: op.attributes })
      else if (op.insert) chars.push({ insert: op.insert, attributes: op.attributes })
    }
    let i = 0
    for (const op of other.ops) {
      if (op.retain) {
        for (let k = 0; k < op.retain && i < chars.length; k++, i++) {
          if (op.attributes) chars[i] = { ...chars[i], attributes: cleanAttrs({ ...chars[i].attributes, ...op.attributes }) }
        }
      } else if (op.delete) chars.splice(i, op.delete)
      else if (op.insert !== undefined) {
        const ins = typeof op.insert === 'string' ? [...op.insert].map((ch) => ({ insert: ch, attributes: op.attributes })) : [{ insert: op.insert, attributes: op.attributes }]
        chars.splice(i, 0, ...ins)
        i += ins.length
      }
    }
    for (const c of chars) {
      const last = out[out.length - 1]
      if (last && typeof last.insert === 'string' && typeof c.insert === 'string' && deepEqual(last.attributes ?? null, c.attributes ?? null)) last.insert += c.insert
      else out.push(c.attributes && Object.keys(c.attributes).length ? { insert: c.insert, attributes: c.attributes } : { insert: c.insert })
    }
    return new Delta(out)
  }
}
const cleanAttrs = (a) => {
  const o = {}
  for (const [k, v] of Object.entries(a || {})) if (v !== null && v !== undefined && v !== false) o[k] = v
  return Object.keys(o).length ? o : undefined
}

const STYLE_ATTRS = { bold: 'bold', italic: 'italic', strike: 'strike', code: 'code', underline: 'underline' }
function styleOf(attrs) {
  if (!attrs) return undefined
  const s = {}
  for (const [a, k] of Object.entries(STYLE_ATTRS)) if (attrs[a]) s[k] = true
  return Object.keys(s).length ? s : undefined
}
/** one line's inline ops as rich_text elements */
function inlineElements(ops) {
  const out = []
  for (const op of ops) {
    const a = op.attributes || {}
    const style = styleOf(a)
    if (typeof op.insert === 'string') {
      if (!op.insert) continue
      if (a.link) out.push({ type: 'link', url: a.link, text: op.insert, ...(style ? { style } : {}) })
      else {
        const last = out[out.length - 1]
        if (last?.type === 'text' && deepEqual(last.style, style)) last.text += op.insert
        else out.push({ type: 'text', text: op.insert, ...(style ? { style } : {}) })
      }
      continue
    }
    const e = op.insert || {}
    if (e.slackemoji || e.emoji) {
      const em = e.slackemoji || e.emoji
      const name = typeof em === 'string' ? em : em.name
      const el = { type: 'emoji', name: String(name).replace(/^:|:$/g, '') }
      if (em.unicode) el.unicode = em.unicode
      if (em.skin_tone) el.skin_tone = em.skin_tone
      out.push(el)
    } else if (e.slackuser || e.user) {
      const u = e.slackuser || e.user
      out.push({ type: 'user', user_id: u.id ?? u })
    } else if (e.slackchannel || e.channel) {
      const c = e.slackchannel || e.channel
      out.push({ type: 'channel', channel_id: c.id ?? c })
    } else if (e.slackusergroup || e.usergroup) {
      const g = e.slackusergroup || e.usergroup
      out.push({ type: 'usergroup', usergroup_id: g.id ?? g })
    } else if (e.slackbroadcast || e.broadcast) {
      const b = e.slackbroadcast || e.broadcast
      out.push({ type: 'broadcast', range: String(b.range ?? b.id ?? b).replace(/^@/, '') })
    } else if (e.slackdate || e.date) {
      const d = e.slackdate || e.date
      out.push({ type: 'date', timestamp: d.timestamp, format: d.format ?? '{date_short_pretty}', fallback: d.fallback })
    }
  }
  return out
}
/** Quill ops (Slack draft `ops`) → rich_text blocks, like Slack's convertDeltaToBlocks */
function deltaToBlocks(delta, options = {}) {
  const ops = delta?.ops ?? delta ?? []
  // split into lines; a line's block format lives on its trailing "\n"
  const lines = []
  let cur = []
  for (const op of ops) {
    if (typeof op.insert !== 'string') {
      cur.push(op)
      continue
    }
    const parts = op.insert.split('\n')
    parts.forEach((p, i) => {
      if (p) cur.push({ ...op, insert: p })
      if (i < parts.length - 1) {
        lines.push({ ops: cur, attrs: op.attributes || {} })
        cur = []
      }
    })
  }
  if (cur.length) lines.push({ ops: cur, attrs: {} })
  const elements = []
  const push = (type, els, extra) => {
    const last = elements[elements.length - 1]
    if (type === 'rich_text_section' && last?.type === 'rich_text_section') {
      last.elements.push({ type: 'text', text: '\n' }, ...els)
      return
    }
    if ((type === 'rich_text_preformatted' || type === 'rich_text_quote') && last?.type === type) {
      last.elements.push({ type: 'text', text: '\n' }, ...els)
      return
    }
    elements.push({ type, elements: els, ...extra })
  }
  for (const line of lines) {
    const els = inlineElements(line.ops)
    const a = line.attrs
    if (a['code-block']) push('rich_text_preformatted', els.map((e) => (e.style ? { ...e, style: undefined } : e)).map(cleanEl), { border: 0 })
    else if (a.blockquote) push('rich_text_quote', els)
    else if (a.list) {
      const style = a.list === 'ordered' ? 'ordered' : 'bullet'
      const indent = Number(a.indent || 0)
      const last = elements[elements.length - 1]
      const item = { type: 'rich_text_section', elements: els }
      if (last?.type === 'rich_text_list' && last.style === style && last.indent === indent) last.elements.push(item)
      else elements.push({ type: 'rich_text_list', style, indent, border: 0, elements: [item] })
    } else push('rich_text_section', els)
  }
  if (options.trimEndingWhitespace) trimEnd(elements)
  if (!elements.length && !options.convertEmpty) return []
  return [{ type: 'rich_text', block_id: randomBlockId(), elements: elements.length ? elements : [{ type: 'rich_text_section', elements: [] }] }]
}
const cleanEl = (e) => {
  if (e.style === undefined) delete e.style
  return e
}
function trimEnd(elements) {
  const last = elements[elements.length - 1]
  const t = last?.elements?.[last.elements.length - 1]
  if (t?.type === 'text') t.text = t.text.replace(/\s+$/, '')
}
const randomBlockId = () => Math.random().toString(36).slice(2, 7).padEnd(5, 'x')

/** rich_text blocks → Quill ops (the reverse of deltaToBlocks) */
function blocksToDelta(blocks) {
  const d = new Delta()
  const inline = (els) => {
    for (const e of els ?? []) {
      const attrs = { ...(e.style || {}) }
      switch (e.type) {
        case 'text':
          d.insert(e.text ?? '', cleanAttrs(attrs))
          break
        case 'link':
          d.insert(e.text || e.url, cleanAttrs({ ...attrs, link: e.url }))
          break
        case 'emoji':
          d.insert({ slackemoji: { name: e.name, unicode: e.unicode } })
          break
        case 'user':
          d.insert({ slackuser: { id: e.user_id } })
          break
        case 'channel':
          d.insert({ slackchannel: { id: e.channel_id } })
          break
        case 'usergroup':
          d.insert({ slackusergroup: { id: e.usergroup_id } })
          break
        case 'broadcast':
          d.insert({ slackbroadcast: { range: e.range } })
          break
        case 'date':
          d.insert({ slackdate: { timestamp: e.timestamp, format: e.format, fallback: e.fallback } })
          break
      }
    }
  }
  for (const b of blocks ?? []) {
    if (b?.type !== 'rich_text') continue
    for (const el of b.elements ?? []) {
      if (el.type === 'rich_text_list') {
        for (const item of el.elements ?? []) {
          inline(item.elements)
          d.insert('\n', { list: el.style === 'ordered' ? 'ordered' : 'bullet', ...(el.indent ? { indent: el.indent } : {}) })
        }
      } else if (el.type === 'rich_text_preformatted' || el.type === 'rich_text_quote') {
        const fmt = el.type === 'rich_text_preformatted' ? { 'code-block': true } : { blockquote: true }
        const sub = new Delta()
        const saved = d.ops
        d.ops = sub.ops
        inline(el.elements)
        d.ops = saved
        for (const op of sub.ops) {
          if (typeof op.insert === 'string') {
            const parts = op.insert.split('\n')
            parts.forEach((p, i) => {
              if (p) d.insert(p, op.attributes)
              if (i < parts.length - 1) d.insert('\n', fmt)
            })
          } else d.ops.push(op)
        }
        d.insert('\n', fmt)
      } else {
        inline(el.elements)
        d.insert('\n')
      }
    }
  }
  return d
}

function blocksText(blocks, md) {
  const st = getState()
  const out = []
  const inline = (els, inCode) =>
    (els ?? [])
      .map((e) => {
        switch (e.type) {
          case 'text': {
            let t = e.text ?? ''
            if (md && !inCode && e.style) {
              if (e.style.code) t = '`' + t + '`'
              if (e.style.bold) t = '*' + t + '*'
              if (e.style.italic) t = '_' + t + '_'
              if (e.style.strike) t = '~' + t + '~'
            }
            return t
          }
          case 'link':
            return md && e.text ? `[${e.text}](${e.url})` : e.text || e.url
          case 'emoji':
            return e.unicode ? String.fromCodePoint(...e.unicode.split('-').map((h) => parseInt(h, 16))) : `:${e.name}:`
          case 'user': {
            const m = st.members?.[e.user_id]
            return '@' + (m?.profile?.display_name || m?.real_name || m?.name || e.user_id)
          }
          case 'channel':
            return '#' + (st.channels?.[e.channel_id]?.name || e.channel_id)
          case 'usergroup':
            return '@' + (st.userGroups?.[e.usergroup_id]?.handle || e.usergroup_id)
          case 'broadcast':
            return '@' + e.range
          case 'date':
            return e.fallback || new Date(e.timestamp * 1000).toLocaleString()
          default:
            return e.text ?? ''
        }
      })
      .join('')
  for (const b of blocks ?? []) {
    if (b?.type === 'rich_text') {
      for (const el of b.elements ?? []) {
        if (el.type === 'rich_text_list') {
          ;(el.elements ?? []).forEach((it, i) => out.push(`${'  '.repeat(el.indent || 0)}${el.style === 'ordered' ? `${i + 1}.` : '•'} ${inline(it.elements)}`))
        } else if (el.type === 'rich_text_preformatted') out.push(md ? '```\n' + inline(el.elements, true) + '\n```' : inline(el.elements, true))
        else if (el.type === 'rich_text_quote') out.push(inline(el.elements).split('\n').map((l) => (md ? '> ' + l : l)).join('\n'))
        else out.push(inline(el.elements))
      }
    } else if (b?.text?.text) out.push(b.text.text)
  }
  return out.join('\n')
}

const blocks = {
  fromDraft: async (ops, options) => deltaToBlocks(new Delta(ops), options),
  toPlainText: async (b) => blocksText(b, false),
  toMarkdown: async (b) => blocksText(b, true),
  mapRichTextElements,
  mapRichTextSections,
}

// ---- experiments ----

const experimentSets = new Set()
let forcedExperiments = new Map()
function combinedExperiments() {
  const out = new Map()
  for (const s of [...experimentSets].sort((a, b) => a.priority - b.priority)) for (const [n, g] of s) out.set(n, g)
  return out
}
let experimentsScheduled = false
let unpatchExperiments
function updateExperiments() {
  experimentsScheduled = false
  const next = combinedExperiments()
  if (deepEqual([...next].sort(), [...forcedExperiments].sort())) return
  forcedExperiments = next
  goOr(null, 'experiments', Object.fromEntries(next))
  if (next.size && !unpatchExperiments) unpatchExperiments = patchSlice('experiments', (name, a) => experimentAssignment(name, a), () => forcedExperiments.keys())
  else if (!next.size && unpatchExperiments) {
    const u = unpatchExperiments
    unpatchExperiments = undefined
    u()
  } else refresh()
}
const experimentMemo = new Map()
function experimentAssignment(name, stored) {
  if (!forcedExperiments.has(name)) return stored
  const group = forcedExperiments.get(name)
  if (group === null) return undefined
  const m = experimentMemo.get(name)
  if (m && m.stored === stored && m.group === group) return m.out
  const { schedule_ts: _, ...base } = stored ?? { type: 'user', experiment_id: 'from-override', exposure_id: 0, trigger: 'force_request' }
  const out = { ...base, group, log_exposures: false }
  experimentMemo.set(name, { stored, group, out })
  return out
}
const scheduleExperiments = () => {
  if (experimentsScheduled) return
  experimentsScheduled = true
  queueMicrotask(updateExperiments)
}
class ForcedExperiments {
  #groups = new Map()
  #order = 0
  #disposed = false
  constructor() {
    experimentSets.add(this)
  }
  get priority() {
    return this.#order
  }
  set priority(p) {
    if (p === this.#order) return
    this.#order = p
    scheduleExperiments()
  }
  get size() {
    return this.#groups.size
  }
  get(n) {
    return this.#groups.get(n)
  }
  has(n) {
    return this.#groups.has(n)
  }
  set(n, g) {
    if (g !== null && typeof g !== 'string') throw new TypeError('Experiment group must be a string or null')
    if (this.#disposed || this.#groups.get(n) === g) return this
    this.#groups.set(n, g)
    scheduleExperiments()
    return this
  }
  delete(n) {
    const had = this.#groups.delete(n)
    if (had) scheduleExperiments()
    return had
  }
  clear() {
    if (!this.#groups.size) return
    this.#groups.clear()
    scheduleExperiments()
  }
  keys() {
    return this.#groups.keys()
  }
  entries() {
    return this.#groups.entries()
  }
  [Symbol.iterator]() {
    return this.#groups.entries()
  }
  dispose = () => {
    this.#disposed = true
    this.#groups.clear()
    experimentSets.delete(this)
    scheduleExperiments()
  }
}
const getAllExperimentAssignments = withMeta('getAllExperimentAssignments', (state) => state?.experiments ?? {})
const getGroupForUser = named('getGroupForUser', (state, name) => state?.experiments?.[name]?.group)
const getGroupForTeam = named('getGroupForTeam', (state, name) => state?.experiments?.[name]?.group)
defineModule('experiments', { getAllExperimentAssignments, getGroupForUser, getGroupForTeam, getExperimentGroup: named('getExperimentGroup', (state, name) => state?.experiments?.[name]?.group) })
nameSelector(getAllExperimentAssignments, 'getAllExperimentAssignments')

// ---- composer ----

const composerButtons = new Store([])
function addButton(added) {
  if (composerButtons.get().some((b) => b.id === added.id)) throw new Error(`[Taut] Composer button "${added.id}" already exists`)
  composerButtons.update((bs) => [...bs, added].sort((a, b) => a.id.localeCompare(b.id)))
  tellGoComposer()
  return () => {
    composerButtons.update((bs) => bs.filter((b) => b !== added))
    tellGoComposer()
  }
}
const sendChecks = new Set()
function addSendCheck(check) {
  sendChecks.add(check)
  tellGoComposer()
  return () => {
    sendChecks.delete(check)
    tellGoComposer()
  }
}
const linkTransforms = new Set()
function addLinkTransform(t) {
  linkTransforms.add(t)
  tellGoComposer()
  return () => {
    linkTransforms.delete(t)
    tellGoComposer()
  }
}
let activeMarkup
function patchInlineMarkup(markup) {
  if (activeMarkup) throw new Error('[Taut] Inline markup is already patched')
  activeMarkup = markup
  tellGoComposer()
  return () => {
    if (activeMarkup === markup) activeMarkup = undefined
    tellGoComposer()
  }
}
const sendTransforms = new Set()
function onMessageSendBlocks(t) {
  sendTransforms.add(t)
  tellGoComposer()
  return () => {
    sendTransforms.delete(t)
    tellGoComposer()
  }
}
const tellGoComposer = batcher(() =>
  goOr(null, 'composerHooks', {
    buttons: composerButtons.get().map((b) => ({ id: b.id, placement: b.placement, kinds: b.kinds ?? ['channel', 'thread'] })),
    sendChecks: sendChecks.size,
    linkTransforms: linkTransforms.size,
    inlineMarkup: !!activeMarkup,
    sendTransforms: sendTransforms.size,
  })
)

/** Go → JS: blocks after every send transform (idempotent by contract) */
T.transformSendBlocks = (blocks) => {
  let result = blocks
  for (const t of sendTransforms) {
    try {
      const next = t(result)
      if (Array.isArray(next)) result = next
    } catch (err) {
      error('Message send transform failed:', err)
    }
  }
  return result
}
/** the outgoing message's blocks: the composer passes them; else read the registered input */
function sendCheckBlocks(c) {
  if (Array.isArray(c.blocks)) return c.blocks
  try {
    const where = { channelId: c.channelId, ...(c.threadTs ? { viewContext: 'THREADS_FLEXPANE' } : {}) }
    const contents = getInputProxy(where)?.getContents()
    return contents ? deltaToBlocks(contents) : []
  } catch {
    return []
  }
}
/** Go → JS: false keeps the message in the composer. Checks are Taut's (blocks, {channelId, threadTs}) */
T.sendCheck = async (ctx) => {
  if (!sendChecks.size) return true
  const c = decode(ctx) || {}
  const blocks = sendCheckBlocks(c)
  const context = { channelId: c.channelId, ...(c.threadTs ? { threadTs: c.threadTs } : {}) }
  for (const check of [...sendChecks]) {
    try {
      if ((await check(blocks, context)) === false) return false
    } catch (err) {
      error('Send check failed:', err)
    }
  }
  return true
}
/** Go → JS: whether a dialog a pending send check may be waiting on is open */
T.sendCheckWaiting = () => !!G.document?.querySelector?.('dialog[open], [role=dialog], [role=alertdialog], [aria-modal=true]')
T.transformLink = (url, ctx) => {
  let r = url
  for (const t of linkTransforms) {
    try {
      const n = t(r, ctx)
      if (typeof n === 'string' && n) r = n
    } catch (err) {
      error('Link transform failed:', err)
    }
  }
  return r
}
T.inlineMarkup = (op, arg) => {
  if (!activeMarkup) return null
  if (op === 'parse') return activeMarkup.parse(arg)
  if (op === 'serialize') return activeMarkup.serialize(arg)
  if (op === 'escape') return activeMarkup.escape(arg)
  return null
}

/** the composer toolbar: Clack's composer renders "TautComposerButtons" with {placement, context} */
function ComposerButtons({ placement, context }) {
  const React = G.React
  const all = composerButtons.use()
  const ctx = decode(context || {})
  const kind = ctx.kind || 'channel'
  const list = all.filter((b) => b.placement === placement && (b.kinds ?? ['channel', 'thread']).includes(kind))
  return React.createElement(React.Fragment, null, ...list.map((b) => React.createElement(ComposerButtonHost, { key: b.id, button: b, context: ctx })))
}
function ComposerButtonHost({ button, context }) {
  const React = G.React
  const display = button.render(context)
  if (!display) return null
  const native = nativeFor('ComposerButton')
  const icon = typeof display.icon === 'string' ? display.icon : undefined
  return React.createElement(
    native,
    { id: button.id, icon, label: display.label, pressed: display.pressed, onClick: () => display.onClick(), tooltip: display.tooltip ?? display.label },
    typeof display.icon === 'string' ? undefined : display.icon
  )
}
ComposerButtons.displayName = 'TautComposerButtons'

const composer = { patchInlineMarkup, addButton, addSendCheck, addLinkTransform }

// ---- setStyle (forwarded to taut-ui's CSS engine) ----

const sheets = new Map() // key → css
let anonSheet = 0
function setStyle(css, key) {
  const k = key === undefined ? `anon:${++anonSheet}` : key
  if (css === null) {
    if (sheets.delete(k)) goOr(null, 'style', k, null)
    return () => {}
  }
  sheets.set(k, css)
  goOr(null, 'style', k, css)
  let dropped = false
  return () => {
    if (dropped) return
    dropped = true
    if (sheets.get(k) === css || key === undefined) {
      sheets.delete(k)
      goOr(null, 'style', k, null)
    }
  }
}

// ---- deferResizeWork ----

const resizeRegs = new Set()
let resizeTimer
let resizeHolding = false
let replayingResize = false
const quietMs = () => Math.max(...[...resizeRegs].map((r) => r.quietMs))
function setResizeHolding(next) {
  if (resizeHolding === next) return
  resizeHolding = next
  for (const r of resizeRegs) {
    try {
      r.onHoldChange?.(next)
    } catch (err) {
      error(err)
    }
  }
}
function flushResize() {
  resizeTimer = undefined
  setResizeHolding(false)
  replayingResize = true
  try {
    G.dispatchEvent?.(new (G.UIEvent || G.Event)('resize'))
  } finally {
    replayingResize = false
  }
}
let resizeGateInstalled = false
function installResizeGate() {
  if (resizeGateInstalled || typeof G.addEventListener !== 'function') return
  resizeGateInstalled = true
  G.addEventListener(
    'resize',
    (event) => {
      if (!resizeRegs.size || replayingResize) return
      setResizeHolding(true)
      clearTimeout(resizeTimer)
      resizeTimer = setTimeout(flushResize, quietMs())
      event.stopImmediatePropagation?.()
    },
    true
  )
}
function deferResizeWork({ quietMs, onHoldChange }) {
  installResizeGate()
  const reg = { quietMs, onHoldChange }
  resizeRegs.add(reg)
  return () => {
    if (!resizeRegs.delete(reg)) return
    try {
      reg.onHoldChange?.(false)
    } catch {}
    if (resizeRegs.size || !resizeHolding) return
    clearTimeout(resizeTimer)
    flushResize()
  }
}

// ---- scheduleEnd (Go does the time zone math) ----

function scheduleEnd(windows, timeZone) {
  const r = go('scheduleEnd', windows, timeZone ?? '', Date.now())
  return r === null || r === undefined ? null : r
}

// ---- Slack-equivalent named exports ----

/** Slack's I18n: English strings are their own keys, formatted ICU-style */
class I18n {
  constructor(namespace) {
    this.namespace = namespace
  }
  t(text, values) {
    return formatICU(String(text), values || {})
  }
  static t(text, values) {
    return formatICU(String(text), values || {})
  }
}
named('I18n', I18n)
function formatICU(text, values, hash) {
  let out = ''
  let i = 0
  while (i < text.length) {
    const c = text[i]
    if (c === "'" && text[i + 1] === "'") {
      out += "'"
      i += 2
      continue
    }
    if (c === '#' && hash !== undefined) {
      out += String(hash)
      i++
      continue
    }
    if (c !== '{') {
      out += c
      i++
      continue
    }
    // find the matching brace
    let depth = 0
    let j = i
    for (; j < text.length; j++) {
      if (text[j] === '{') depth++
      else if (text[j] === '}' && --depth === 0) break
    }
    const body = text.slice(i + 1, j)
    i = j + 1
    const parts = splitTop(body)
    const name = parts[0].trim()
    const v = values[name]
    if (parts.length === 1) {
      out += v === undefined ? `{${name}}` : String(v)
      continue
    }
    const type = parts[1].trim()
    const rest = parts.slice(2).join(',')
    const cases = parseCases(rest)
    if (type === 'plural' || type === 'selectordinal') {
      const n = Number(v)
      const exact = cases.get(`=${n}`)
      const cat = typeof Intl !== 'undefined' && Intl.PluralRules ? new Intl.PluralRules('en', type === 'selectordinal' ? { type: 'ordinal' } : undefined).select(n) : n === 1 ? 'one' : 'other'
      const msg = exact ?? cases.get(cat) ?? cases.get('other') ?? ''
      out += formatICU(msg, values, n)
    } else if (type === 'select') {
      out += formatICU(cases.get(String(v)) ?? cases.get('other') ?? '', values, hash)
    } else if (type === 'number') {
      out += typeof Intl !== 'undefined' && Intl.NumberFormat ? new Intl.NumberFormat('en').format(Number(v)) : String(v)
    } else out += v === undefined ? '' : String(v)
  }
  return out
}
function splitTop(s) {
  const out = []
  let depth = 0
  let start = 0
  for (let k = 0; k < s.length; k++) {
    if (s[k] === '{') depth++
    else if (s[k] === '}') depth--
    else if (s[k] === ',' && depth === 0 && out.length < 2) {
      out.push(s.slice(start, k))
      start = k + 1
    }
  }
  out.push(s.slice(start))
  return out
}
function parseCases(s) {
  const m = new Map()
  let k = 0
  while (k < s.length) {
    while (k < s.length && /\s/.test(s[k])) k++
    let key = ''
    while (k < s.length && s[k] !== '{' && !/\s/.test(s[k])) key += s[k++]
    while (k < s.length && s[k] !== '{') k++
    if (k >= s.length) break
    let depth = 0
    const st = k
    for (; k < s.length; k++) {
      if (s[k] === '{') depth++
      else if (s[k] === '}' && --depth === 0) break
    }
    m.set(key, s.slice(st + 1, k))
    k++
  }
  return m
}

/** the name Slack shows for a member, by the display-name pref */
const getPrefCompliantMemberName = named('getPrefCompliantMemberName', (state, member) => {
  if (!member) return ''
  const realNames = state?.prefs?.user?.display_real_names_override ?? state?.userPrefs?.display_real_names_override
  const display = member.profile?.display_name || member.display_name
  const real = member.profile?.real_name || member.real_name
  if (realNames === 1 || realNames === true) return real || display || member.name || ''
  return display || real || member.name || ''
})

/** @channel/@here/@everyone the viewer may use in a conversation */
const getBroadcastKeywordsForUser = named('getBroadcastKeywordsForUser', (state, channelId) => {
  const ch = state?.channels?.[channelId]
  if (!ch) return []
  const props = ch.properties || {}
  const out = []
  const isAdmin = (() => {
    const m = state?.members?.[state?.bootData?.user_id]
    return !!(m?.is_admin || m?.is_owner)
  })()
  const restricted = props.at_channel_restricted && !isAdmin
  const hereRestricted = props.at_here_restricted && !isAdmin
  if (!restricted) out.push({ id: 'BKchannel', name: 'channel', is_broadcast_keyword: true })
  if (!hereRestricted) out.push({ id: 'BKhere', name: 'here', is_broadcast_keyword: true })
  if (ch.is_general && !restricted) out.push({ id: 'BKeveryone', name: 'everyone', is_broadcast_keyword: true })
  return out
})

/** composers Clack's native UI registered, by Slack's {viewContext, channelId} */
const inputs = new Map()
const inputKey = (w) => `${w?.viewContext ?? ''}|${w?.channelId ?? ''}`
T.registerInput = (where, has) => {
  if (has) inputs.set(inputKey(where), where)
  else inputs.delete(inputKey(where))
}
const getInputProxy = named('getInputProxy', (where) => {
  const w = inputs.get(inputKey(where)) || [...inputs.values()].find((x) => x.channelId === where?.channelId && (!where.viewContext || x.viewContext === where.viewContext))
  if (!w) return undefined
  return {
    isEmpty: () => !!go('input', 'isEmpty', w),
    getContents: () => new Delta(go('input', 'getContents', w)),
    setContents: ({ contents }) => go('input', 'setContents', w, encode(contents?.ops ?? contents)),
    focus: () => go('input', 'focus', w),
    insertText: (t) => go('input', 'insertText', w, t),
  }
})
const isInputRegistered = named('isInputRegistered', (where) => [...inputs.values()].some((x) => x.channelId === where?.channelId && (!where.viewContext || x.viewContext === where.viewContext)))

const viewContexts = Object.freeze({
  MESSAGE_PANE: 'MESSAGE_PANE',
  THREADS_FLEXPANE: 'THREADS_FLEXPANE',
  THREADS_VIEW: 'THREADS_VIEW',
  HUDDLE_THREAD: 'HUDDLE_THREAD',
  MESSAGE_EDIT: 'MESSAGE_EDIT',
  COMPOSER_PAGE: 'COMPOSER_PAGE',
})
const THREAD_CONTEXTS = new Set(['THREADS_FLEXPANE', 'THREADS_VIEW', 'HUDDLE_THREAD'])
const isThreadViewContext = named('isThreadViewContext', (v) => THREAD_CONTEXTS.has(String(v).toUpperCase()))

/** Slack's link detector the composer uses */
const detectLinks = named('detectLinks', ({ input, ignoreEmailMatches, ignorePhoneMatches } = {}) => {
  const out = []
  const re = /\b((?:https?:\/\/|www\.)[^\s<>"]+[^\s<>".,;:!?)\]'])|([\w.+-]+@[\w-]+\.[\w.-]+)/gi
  for (const m of String(input ?? '').matchAll(re)) {
    if (m[2] && ignoreEmailMatches) continue
    const text = m[0]
    const url = m[2] ? `mailto:${text}` : /^www\./i.test(text) ? `http://${text}` : text
    out.push({ offset: m.index, text, url })
  }
  void ignorePhoneMatches
  return out
})

const canEditMessage = named('canEditMessage', (state, memberId, message) => {
  const m = asActionable(message)
  return !!m && m.user === memberId && !m.subtype
})
const canDeleteMessageHelper = named('canDeleteMessageHelper', (state, args) => {
  const m = asActionable(args?.message)
  return !!m && m.user === (state?.bootData?.user_id ?? getCurrentMemberId())
})
const getLastMessageByMember = named('getLastMessageByMember', (msgs, memberId) => {
  const views = (msgs || []).map(asActionable)
  for (let i = views.length - 1; i >= 0; i--) if (views[i]?.user === memberId && !views[i].subtype) return msgs[i]
  return undefined
})
const getDeltaForMessageObject = named('getDeltaForMessageObject', (state, message) => {
  const m = asActionable(message)
  return m?.blocks ? blocksToDelta(m.blocks) : new Delta([{ insert: (m?.text ?? '') + '\n' }])
})

// telemetry: Clack sends none, these exist so plugins that switch it off find them
class GenericTracer {
  shouldSample() {
    return false
  }
  startSpan() {
    return { end() {}, setAttribute() {} }
  }
}
class NoopTelemeter {
  clearMarks() {}
  clearMeasures() {}
  emptyMetrics() {}
  measureAndClear() {}
  mark() {}
  measure() {}
  histogram() {}
  count() {}
  store() {}
}
class GenericTelemeter extends NoopTelemeter {}
const tracer = new GenericTracer()
const telemeter = new GenericTelemeter()
const noopTelemeter = new NoopTelemeter()

// huddle events (Clack does not join huddles' media; the log thunks keep state; Go feeds activeHuddle, peers and peer messages from the native huddle)
const enableEventLog = withMeta('enableEventLog', () => ({ type: 'ENABLE_EVENT_LOG' }))
const disableEventLog = withMeta('disableEventLog', () => ({ type: 'DISABLE_EVENT_LOG' }))
onAction('ENABLE_EVENT_LOG', (a) => (goOr(null, 'huddleEventLog', true), a))
onAction('DISABLE_EVENT_LOG', (a) => (goOr(null, 'huddleEventLog', false), a))
const getActiveHuddleCallId = withMeta('getActiveHuddleCallId', () => activeHuddle.get()?.callId)
const getActiveHuddleChannelId = withMeta('getActiveHuddleChannelId', () => activeHuddle.get()?.channelId)
const sendReactionMessage = withMeta('sendReactionMessage', (payload) => ({ type: 'SEND_REACTION_MESSAGE', payload }))
onAction('SEND_REACTION_MESSAGE', (a) => (goOr(null, 'huddleReaction', a.payload), a))
const jumpToReply = withMeta('jumpToReply', (args) => ({ type: 'JUMP_TO_REPLY', payload: args }))
onAction('JUMP_TO_REPLY', (a) => {
  const [ch, ts] = String(a.payload?.threadKey ?? '').split('-')
  goOr(null, 'threadPane', 'jumpToReply', ch, ts, a.payload?.ts)
  return a
})
const setLocalDraft = withMeta('setLocalDraft', (draft) => ({ type: 'SET_LOCAL_DRAFT', payload: draft }))
onAction('SET_LOCAL_DRAFT', (a) => (goOr(null, 'localDraft', encode(a.payload)), a))

// ---- pending files (the composer's attachments) ----
// state.pendingFileUploads[pendingFileId] is served from Go (the composer's
// pending files, mirror.go); state.files has the ones whose bytes are up.
// updatePendingFile is Slack's plain action creator: renaming a file here
// renames it in the composer (its chip and the name it is shared under).
// maybePersistPendingFileData and editFileApi are native thunks (thunks.go).
const updatePendingFile = withMeta('updatePendingFile', (payload) => ({ type: 'UPDATE_PENDING_FILE', payload }))
onAction('UPDATE_PENDING_FILE', (a) => {
  const { pendingFileId, title } = a.payload || {}
  if (pendingFileId != null && typeof title === 'string') {
    goOr(false, 'updatePendingFile', String(pendingFileId), title)
    // read again now, so selectors see the new title in this task
    T.invalidate([{ slice: 'pendingFileUploads' }, { slice: 'files' }])
  }
  return a
})

class AudioVideoFacadeSubscriber {
  handlePeerMessageFromDataMessage() {}
}

// Slack's selector (state, channelId, threadTs, userId): did userId
// (Slackbot) answer with one of the workspace's custom responses
// (subtype slackbot_response) in this thread? Core answers from its cached
// replies (no JSON built); once a plugin patches the state, the state
// passed in is read instead, as Slack's selector would.
function hasSlackbotCustomResponseInThread(state, channelId, threadTs, userId) {
  if (!channelId || !threadTs) return false
  if (statePatches.size === 0) return goOr(false, 'slackbotCustomResponseInThread', String(channelId), String(threadTs), userId ? String(userId) : '') === true
  const bucket = state?.messages?.[channelId]
  if (!bucket || typeof bucket !== 'object') return false
  for (const ts of Object.keys(bucket)) {
    const m = bucket[ts]
    if (m && m.thread_ts === threadTs && m.subtype === 'slackbot_response' && (!userId || m.user === userId)) return true
  }
  return false
}

// Slack's ClientStoreContext: a React context holding {store}, which
// Slack provides at its app root. Clack's patched components are separate
// roots, so the store is the context's default value and every component
// sees it (CallTimer reads clientHuddles through it).
if (G.React) {
  const ClientStoreContext = G.React.createContext({ store: getStore() })
  ClientStoreContext.displayName = 'ClientStoreContext'
  defineModule('slack-client-store', { ClientStoreContext })
}

defineModule('slack-exports', {
  hasSlackbotCustomResponseInThread,
  I18n,
  getPrefCompliantMemberName,
  getBroadcastKeywordsForUser,
  getInputProxy,
  isInputRegistered,
  isThreadViewContext,
  detectLinks,
  canEditMessage,
  canDeleteMessageHelper,
  getLastMessageByMember,
  getDeltaForMessageObject,
  convertDeltaToBlocks: named('convertDeltaToBlocks', ({ delta, options } = {}) => ({ blocks: deltaToBlocks(delta, options) })),
  convertBlocksToDelta: named('convertBlocksToDelta', (_s, b) => blocksToDelta(b)),
  convertBlocksToPlainText: named('convertBlocksToPlainText', (_s, b) => blocksText(b, false)),
  convertBlocksToMarkdown: named('convertBlocksToMarkdown', (_s, b) => blocksText(b, true)),
  convertBlocksToMrkdwnDelta: named('convertBlocksToMrkdwnDelta', (_s, b) => new Delta([{ insert: blocksText(b, true) + '\n' }])),
  convertContentsToStringForDisplay: named('convertContentsToStringForDisplay', (c) => (typeof c === 'string' ? c : (c?.ops ?? []).map((o) => (typeof o.insert === 'string' ? o.insert : '')).join(''))),
  buildMarkdownChangeDelta: named('buildMarkdownChangeDelta', () => new Delta()),
  copyDocumentStylesheets: named('copyDocumentStylesheets', () => {}),
  getGenericTracer: named('getGenericTracer', () => tracer),
  getGenericTelemeter: named('getGenericTelemeter', () => telemeter),
  getNoopTelemeter: named('getNoopTelemeter', () => noopTelemeter),
  AudioVideoFacadeSubscriber: named('AudioVideoFacadeSubscriber', AudioVideoFacadeSubscriber),
  Delta,
  enableEventLog,
  disableEventLog,
  getActiveHuddleCallId,
  getActiveHuddleChannelId,
  sendReactionMessage,
  jumpToReply,
  setLocalDraft,
})
defineModule('pending-files', { updatePendingFile })
defineModule('view-contexts', viewContexts)

Object.assign(T, {
  members,
  channels,
  messages,
  rtm,
  workspace,
  profile,
  threads,
  huddles,
  files,
  blocks,
  composer,
  ForcedExperiments,
  RateLimit,
  AccountSwitcher,
  userAPI,
  bridgeFetch,
  cookies,
  setStyle,
  deferResizeWork,
  scheduleEnd,
  onMessageSendBlocks,
  ComposerButtons,
  PaneContext,
  paneFrom,
  Delta,
  deltaToBlocks,
  blocksToDelta,
  formatICU,
})
