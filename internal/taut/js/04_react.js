// Slack's React components, by name. Every Slack component a plugin patches
// or renders is Clack's native widget behind a JS component: it renders the
// host node {"t":"@","n":<name>,"p":props} (jsrt's __clack.native), which
// taut-ui lays out with the native widget. patchComponent wraps those
// components exactly like Taut wraps Slack's: replacers compose around the
// original, the result is cached per (replacer, original) so hooks keep their
// state, and Go learns which names are patched so unpatched widgets never
// touch JS.

const ORIGINAL = Symbol.for('taut.originalComponent')
const MEMO = Symbol.for('react.memo')
const FORWARD_REF = Symbol.for('react.forward_ref')

/** source markers for components plugins pick out by their code */
const COMPONENT_SOURCES = {
  Button: '"c-button"',
  Tooltip: '"data-sk":"tooltip"',
  Label: '"c-label"',
  Legend: '"c-legend"',
  Popover: '"c-popover"',
  MessageKitGutter: '"c-message_kit__gutter__left"',
  HuddleLinkJoinGate: 'canJoinByExternalHuddleLink',
}
/**
 * code a component's source mentions, as Slack's own does, for plugins that
 * pick components out by it: Slick's BetterGifs `isGifPicker` (a one-argument
 * function mentioning these) and ChannelListTyping's `isSidebarChannel`
 */
const REAL_SOURCES = {
  GifPicker: '"gif-picker-host", e.onGifSelected, e.emojiSearchQuery',
  // Slick's ChannelListTyping: {displayName: 'Channel', filter: isSidebarChannel}
  Channel: '"getSidebarChannelUiInfo"',
  // Slack's message actions menu renderer (boot-shared 2rcC): Slick's
  // ChatTabs before #473 finds it with a filter on its render's source
  MAMenu: 'renderActionGroup, dispatchMessageAction',
}

/**
 * The source Clack's stand-ins for Slack's code show. In Slack the source is
 * the code, so `fn.toString()` and `Function.prototype.toString.call(fn)`
 * agree; plugins use both (Slick's filters call the latter to skip
 * overrides), so the shown source is served by Function.prototype.toString.
 */
const shownSources = new WeakMap()
const realFnToString = Function.prototype.toString
const fnToString = {
  toString() {
    const src = shownSources.get(this)
    return src !== undefined ? src : realFnToString.call(this)
  },
}.toString
shownSources.set(fnToString, realFnToString.call(realFnToString))
Object.defineProperty(Function.prototype, 'toString', { value: fnToString, writable: true, configurable: true, enumerable: false })
/** makes `fn`'s source `src` for both toString paths */
const showSource = (fn, src) => void shownSources.set(fn, src)

// ---- shapes: Slack components plugins pick out by more than their name ----

/**
 * Slack's TextyInput is memo(class TextyInput). Slick's CharacterCounter
 * finds it with a filter on that type (a memo whose type's prototype has
 * getNumCharsOverLimit) and reads the editor through the ref it passes:
 * ref.instance.quill (getLength, getSelection, on/off for 'editor-change',
 * 'text-change', 'selection-change'). The editor is Clack's native widget,
 * so the Quill here is a model of it, fed by whoever draws the native
 * (feedNative; the composer passes its length and selection).
 */
class TextyInput {
  getNumCharsOverLimit() {
    return 0
  }
}

/** a Quill-like view of a native editor's {length, index, selected} (characters) */
function quillModel() {
  const listeners = new Map()
  let s = { length: 0, index: 0, selected: 0 }
  const emit = (ev, ...args) => {
    const set = listeners.get(ev)
    if (!set) return
    for (const fn of [...set]) {
      try {
        fn(...args)
      } catch (err) {
        error(`a quill ${ev} listener threw`, err)
      }
    }
  }
  const quill = {
    getLength: () => s.length + 1, // Quill counts the trailing newline
    getSelection: () => ({ index: s.index, length: s.selected }),
    on(ev, fn) {
      if (!listeners.has(ev)) listeners.set(ev, new Set())
      listeners.get(ev).add(fn)
      return quill
    },
    off(ev, fn) {
      listeners.get(ev)?.delete(fn)
      return quill
    },
  }
  return {
    quill,
    get: () => s,
    set(next) {
      const text = next.length !== s.length
      const sel = next.index !== s.index || next.selected !== s.selected
      if (!text && !sel) return
      s = next
      if (text) emit('text-change')
      if (sel) emit('selection-change')
      emit('editor-change', text ? 'text-change' : 'selection-change')
    },
  }
}

const finite = (v) => (typeof v === 'number' && Number.isFinite(v) ? v : 0)

/** TextyInput's render: hands its ref Slack's {instance: {quill}} and draws the native with the rest */
function textyInputRender(render) {
  return function (props) {
    const React = G.React
    const { state, props: rest } = fedProps('TextyInput', props)
    const { ref, ...pass } = rest
    const q = React.useMemo(quillModel, [])
    const max = React.useRef(0)
    max.current = finite(pass.maxLength)
    const handle = React.useMemo(
      () => ({ instance: { quill: q.quill, getNumCharsOverLimit: () => (max.current > 0 ? Math.max(0, q.get().length - max.current) : 0) } }),
      [q]
    )
    React.useLayoutEffect(() => {
      if (state) q.set({ length: finite(state.length), index: finite(state.index), selected: finite(state.selected) })
    })
    React.useLayoutEffect(() => {
      if (!ref) return
      if (typeof ref === 'function') {
        const cleanup = ref(handle)
        return () => (typeof cleanup === 'function' ? cleanup() : ref(null))
      }
      ref.current = handle
      return () => {
        ref.current = null
      }
    }, [ref, handle])
    return render(pass)
  }
}

/** Slack's own layout for the view wrapper, in a layer so any plugin or theme rule beats it */
const VIEW_CONTENTS_CSS =
  '@layer clack-slack { .p-view_contents { position: relative; display: flex; flex-direction: column; box-sizing: border-box; height: 100%; min-width: 0; min-height: 0 } }'
let viewContentsStyled = false

/**
 * RenderedViewContents' render, as Slack's: a div with the view's classes
 * (p-view_contents p-view_contents--<viewType> plus className) and
 * containerRef around its children. Clack's view body comes in as a child
 * slot, so whatever a patch puts beside it is inside the view element
 * (`.p-view_contents--primary:has(> …)` sees it) and positioned against it.
 */
function viewContentsRender() {
  return function RenderedViewContents(props) {
    if (!viewContentsStyled) {
      viewContentsStyled = true
      setStyle(VIEW_CONTENTS_CSS, 'clack:view-contents')
    }
    const { className, containerRef, viewType, children } = props || {}
    const classes = ['p-view_contents']
    if (typeof viewType === 'string' && viewType) classes.push(`p-view_contents--${viewType}`)
    for (const c of String(className || '').split(/\s+/)) if (c && !classes.includes(c)) classes.push(c)
    return G.React.createElement('div', { className: classes.join(' '), ref: containerRef }, children)
  }
}

/**
 * name → how its native is shaped: `render` wraps the native's render,
 * `memoOf` makes the component look like memo(memoOf) (React still sees a
 * function component: it checks typeof before $$typeof)
 */
const SHAPES = {
  TextyInput: { render: textyInputRender, memoOf: TextyInput },
  RenderedViewContents: { render: viewContentsRender },
}

/** name → feed(props) → {state, props}: the data a shaped native shows, from whoever draws it */
const nativeFeeds = new Map()
/** sets (or with null removes) the feed of shaped native `name`; read at every render, so order doesn't matter */
function feedNative(name, feed) {
  if (typeof feed === 'function') nativeFeeds.set(name, feed)
  else nativeFeeds.delete(name)
}
function fedProps(name, props) {
  const feed = nativeFeeds.get(name)
  if (!feed) return { state: null, props }
  try {
    const out = feed(props)
    if (out && out.props && typeof out.props === 'object') return out
  } catch (err) {
    error(`the ${name} feed threw`, err)
  }
  return { state: null, props }
}

/** Slack components that are anonymous memos in the web client, found by `filter` on their source */
const ANONYMOUS = new Set(['MessageKitGutter', 'HuddleLinkJoinGate'])
/** every Slack component Clack draws at a boundary (docs/PLUGINS-components.md), so filter-only patches can match them */
const KNOWN_COMPONENTS = ('MessageBackground;MessageWrapper;MessageListItem;SearchResult;MessageAvatar;ConnectedBaseAvatar;MessageKitGutter;' +
  'ThreadCompactGutterGeneric;MessageSender;BaseMessageSender;BaseBroadcastPreamble;ThreadSenderAndTimestampGeneric;ThreadRootGeneric;' +
  'MessageBlocks;Blocks;ViewBlocks;MessageAttachment;BaseMessageAttachmentSlackMessage;AttachmentMedia;MessageAttachmentHuddleLink;' +
  'HuddleLinkJoinGate;ReactionBar;Reaction;UnstyledButton;ReactionAddButton;MessageActions;MessageActionButtonReaction;EmojiPickerPopover;' +
  'EmojiPicker;MessagePaneInput;InputContainer;MessageInput;WysiwygContainer;TextyButtons;TextyButtonOverflow;ShortcutsMenuTriggerButton;' +
  'BaseContextBarV2;MessageInputRoadblock;ThreadTypingIndicator;TypingNames;texty_input_with_autocomplete;TextyInput;CharacterCount;ComposerLinkModal;' +
  'Serialized(ThreadsFlexpane);ThreadSearchButton;ThreadFlexpaneAdminTools;BaseAvatarStack;HelpButton;ListChannelEntity;BaseMrkdwnChannel;' +
  'MemberLink;ActivityItem;ActivityRowContentSenderAvatarLayout;ActivityPrivacyBlurredName;RimetoProfilePresence;MemberProfileHoverCard;' +
  'BasePresence;LocalTime;RimetoMemberProfileOverflowMenu;EditAudioButton;CaptionsSubmenu;CaptionsItemsMenu;HuddleMetaEvent;HuddleBaseEvent;' +
  'HuddlesAlertBannerBase;Tabs;MenuFromTemplate;MenuTrigger;withKeyboardFocusContext(ContextMenuTrigger);SvgIcon;ProfileHoverTrigger;' +
  'MrkdwnElement;Button;Tooltip;KeyboardKeysTooltip;IconButtonBase;ConfirmationModal;InlineAlert;Label;FormTextInput;Checkbox;FilterPill;' +
  'Tag;DateRangePicker;DatePickerTrigger;TimePicker;FieldSet;Legend;Hint;BasicSelect;Connect(EmojiMenuTrigger);Popover;RichTextInput;' +
  // names Slick's plugins patch (docs/PLUGINS-components.md §9)
  'AboutPage;ActivityDM;ActivityRowContentDense;ActivityRowContentDetailed;ActivityRowContentMultiLine;ActivityRowContentSingleLine;' +
  'ActivityThread;AppDetailsOverflow;AttachmentTitle;BaseChannelSuffix;BaseDrawRenderer;BaseMessageAvatar;BaseSearch;BaseVisuallyHiddenMessageText;' +
  'ChannelContextMenu;ChannelSidebar;ClientPrimaryView;RenderedViewContents;CodeChannelEntityCard;ConnectedChannel;Channel;Content;ContextMenuTriggerWithKeyboardFocus;' +
  'DmsChannel;DmsList;DmsListItem;EditFileDialogTitle;EditProfileFormPane;FileFieldset;GifListItem;GifPicker;HuddleBottomBarContent;' +
  'HuddleInviteWindowWithPreviewContent;HuddleMiniPanel;HuddleMvaWindowTitle;HuddleRecordingIndicator;LocalTimeRow;MemberProfileBaseEntity;' +
  'MemberSlug;Menu;Message;MessageActionsMenu;MessageAttachmentYouTube;MessageFile;MessageFiles;MessageKitAttachments;MessageText;' +
  'MultiFileUploadItem;PeopleGrid;Presence;ProfileHoverCard;ProgressiveDisclosureSuccessCoachmarkWrapper;ReplyBarAvatar;SearchAutocomplete;' +
  'SearchInputBox;SendButton;TextyAutocomplete;ThreadReplies;ThreadReplyGeneric;MessageAvatar;MessageKitGutter;' +
  // Slick's api.menu.addMessageActions (since #473) and BetterMessageMenu;
  // the message actions submenus, each with a MAMenu of its own
  'MAMenu;OrganizeMessageActionsSubmenu;ConnectToAppsMessageActionsSubmenu').split(';')

const rawNatives = new Map() // name → the unpatched native component
const wrappers = new Map() // name → the patch-following component plugins get
const componentNames = new WeakMap()

let slotCtx
const slotContext = () => (slotCtx ??= G.React.createContext(null))

function nativeFor(name) {
  let c = rawNatives.get(name)
  if (c) return c
  const React = G.React
  // The host native, memoized: a patch that renders again with what it
  // passed before stops here, so the native's host node, and the root
  // drawing it, don't change. Every patch calling usePatchVersion renders
  // again on each api.redux.refresh (Message Logger's, on every edit and
  // delete it logs): about 1,000 of them in a long channel, each
  // re-rendering its native, which re-serialized its props (a message's
  // blocks and all) and sent its root's same tree to Go again.
  const native = typeof host.native === 'function' ? React.memo(host.native(name)) : null
  const render = function SlackNative(props) {
    const view = React.useContext(stateViewContext())
    const el = native ? React.createElement(native, view ? { ...props, $view: view.id } : props) : React.createElement('clack-native', { name, props })
    return el
  }
  const shape = SHAPES[name]
  const inner = shape?.render ? shape.render(render) : render
  const marker = COMPONENT_SOURCES[name]
  const real = REAL_SOURCES[name] ? `void [${REAL_SOURCES[name]}];` : ''
  showSource(
    inner,
    `function(e){${real}let{className:t,...n}=e;return(0,r.jsx)("div",{className:(0,a.A)(${marker || `"c-${name.replace(/[A-Z]/g, (m, i) => (i ? '_' : '') + m.toLowerCase())}"`},t),...n})}`
  )
  if (ANONYMOUS.has(name)) {
    c = React.memo(inner)
    named('', inner)
  } else {
    named(name, inner)
    inner.displayName = name
    c = inner
  }
  if (shape?.memoOf) {
    c.$$typeof = MEMO
    c.type = shape.memoOf
  }
  componentNames.set(c, name)
  rawNatives.set(name, c)
  return c
}

function componentName(c) {
  if (!c) return null
  if (typeof c === 'object') {
    if (c.$$typeof === MEMO) return componentNames.get(c) && !ANONYMOUS.has(componentNames.get(c)) ? componentNames.get(c) : componentName(c.type) || null
    if (c.$$typeof === FORWARD_REF) return c.displayName || c.render?.displayName || c.render?.name || null
    if (c.$$typeof === ORIGINAL) return c.displayName || null
  }
  if (typeof c === 'function') return c.displayName || c.name || null
  return null
}

// ---- patching ----

const replacements = new Map() // matcher → replacer
const replacerCache = new WeakMap() // replacer → Map(original → replaced)
const composedStores = new Map() // name → Store(composed component)
let patchedNames = new Set()

function applyReplacer(replacer, original) {
  let m = replacerCache.get(replacer)
  if (!m) replacerCache.set(replacer, (m = new Map()))
  if (m.has(original)) return m.get(original)
  const out = replacer(original)
  if (typeof out === 'function' && !Object.prototype.hasOwnProperty.call(out, 'displayName')) {
    try {
      out.displayName = `Patched(${componentName(original) || 'Component'})`
    } catch {}
  }
  m.set(original, out)
  return out
}

const originalObjects = new Map()
function originalObject(name) {
  let o = originalObjects.get(name)
  if (!o) {
    const raw = nativeFor(name)
    // what replacers get as `Original`, a component they render like any other
    o = function Original(props) {
      // a slot's <Original> rendered with new props (BetterTypingIndicator's
      // <Original firstTyper={…}/>) keeps its slot, so Go still draws the native
      const slot = G.React.useContext(slotContext())
      if (slot && slot.name === name && (props == null || props.$slot === undefined)) props = { ...props, $slot: slot.slot }
      return G.React.createElement(raw, props)
    }
    o.displayName = name
    o.originalComponent = raw
    originalObjects.set(name, o)
  }
  return o
}

function matchers(name) {
  const raw = nativeFor(name)
  const out = []
  for (const [m, r] of replacements) {
    let ok = false
    try {
      ok = m(raw, name)
    } catch {}
    if (ok) out.push(r)
  }
  return out
}

function compose(name) {
  const reps = matchers(name)
  const original = originalObject(name)
  if (!reps.length) return original
  return reps.reduce((cur, r) => applyReplacer(r, cur), original)
}

// ---- a patch that throws draws the original ----

/**
 * Every patched component Go draws is a React root of its own, and React
 * unmounts a whole root on an error nothing catches: one throw anywhere in
 * a patched subtree, while rendering or in an effect, emptied the root's
 * host tree and Go drew nothing for the component. A composer that Better
 * Typing Indicator patches (it wraps MessagePaneInput, and its TypingNames
 * renders again whenever someone types) vanished that way and stayed gone
 * until a restart. So each patched render runs under a PatchGuard: what a
 * patch throws is logged and the component draws as though no plugin
 * patched it (its Original; the patched parts inside keep their own
 * guards). New props try the patches again.
 */
let PatchGuard
function patchGuard() {
  if (PatchGuard) return PatchGuard
  const React = G.React
  PatchGuard = class PatchGuard extends React.Component {
    constructor(props) {
      super(props)
      this.state = { failed: false }
    }
    static getDerivedStateFromError() {
      return { failed: true }
    }
    componentDidCatch(err, info) {
      notePatchError(this.props.name, err, info)
    }
    componentDidUpdate(prev) {
      // after the commit, not in getDerivedStateFromProps: a reset there
      // would also run in the render that shows the fallback, render the
      // throwing patch again, and send the error on up to the root
      if (this.state.failed && prev.props !== this.props.props) this.setState({ failed: false })
    }
    render() {
      return this.state.failed ? React.createElement(originalObject(this.props.name), this.props.props) : this.props.children
    }
  }
  return PatchGuard
}

/** component name → {at, n}: the last line about its patches throwing, and the throws since */
const patchErrors = new Map()
const PATCH_ERROR_EVERY = 60e3
/** logs a patch's error: the first at once, then a line a minute per component with the count */
function notePatchError(name, err, info) {
  const now = Date.now()
  let s = patchErrors.get(name)
  if (!s) patchErrors.set(name, (s = { at: -Infinity, n: 0 }))
  s.n++
  if (now - s.at < PATCH_ERROR_EVERY) return
  const stack = String(info?.componentStack ?? '')
    .split('\n')
    .map((l) => l.trim())
    .filter(Boolean)
    .slice(0, 6)
    .join(' < ')
  error(`a plugin's patch of ${name} threw${s.n > 1 ? ` (${s.n} times)` : ''}; drawing ${name} without it:`, err, stack ? `[${stack}]` : '')
  s.at = now
  s.n = 0
}

/** the component a plugin gets for Slack's `name`: it follows patches as they come and go */
function slackComponent(name) {
  let w = wrappers.get(name)
  if (w) return w
  const composed = new Store(null)
  composedStores.set(name, composed)
  const React = G.React
  w = function Wrapped(props) {
    const C = composed.use() || compose(name)
    // `__original` opts one render out of patching
    let el
    if (props && props.__original) {
      const { __original, ...rest } = props
      el = React.createElement(nativeFor(name), rest)
    } else if (C !== originalObject(name)) {
      el = React.createElement(patchGuard(), { name, props }, React.createElement(C, props))
    } else {
      el = React.createElement(C, props)
    }
    // a Go slot (decode's $slot) tells its Original the slot even if a patch
    // drops the prop; any other instance clears it for what it renders
    const outer = React.useContext(slotContext())
    const slotId = props?.$slot
    const mine = React.useMemo(() => (slotId !== undefined ? { name, slot: slotId } : null), [slotId])
    if (mine || outer) el = React.createElement(slotContext().Provider, { value: mine }, el)
    return el
  }
  w.displayName = name
  componentNames.set(w, name)
  wrappers.set(name, w)
  composed.set(compose(name))
  return w
}

function applyPatches() {
  const names = new Set()
  for (const name of new Set([...rawNatives.keys(), ...wrappers.keys(), ...patchTargetNames])) {
    if (matchers(name).length) names.add(name)
    const s = composedStores.get(name)
    if (s) {
      const next = compose(name)
      if (next !== s.get()) s.set(next)
    }
  }
  if (!sameSet(names, patchedNames)) {
    patchedNames = names
    goOr(null, 'patchedComponents', [...names])
  }
}
const sameSet = (a, b) => a.size === b.size && [...a].every((x) => b.has(x))

const patchTargetNames = new Set()

/**
 * patchComponent's patch placed innermost, next to the component itself,
 * whenever it comes and goes: a runtime's own patch that plugins' patches
 * wrap (Slick's MAMenu patch, which Slick loads before the plugin whose
 * menu API asks for it starts, so every plugin's MAMenu patch sits outside
 * it; Clack installs it only while it has a message action to add).
 */
function patchComponentInner(matcher, replacement) {
  return addPatch(matcher, replacement, true)
}

/** a plugin's patch: outside the ones before it */
function patchComponent(matcher, replacement) {
  return addPatch(matcher, replacement, false)
}

function addPatch(matcher, replacement, inner) {
  const displayName = typeof matcher === 'string' ? matcher : matcher?.displayName
  const filter = typeof matcher === 'string' ? undefined : matcher?.filter
  const component = typeof matcher === 'string' ? undefined : matcher?.component
  const fn = (comp, name) => {
    if (component && (comp === component || wrappers.get(name) === component || rawNatives.get(name) === component)) return true
    if (displayName === undefined && !filter) return false
    if (displayName !== undefined && name !== displayName) return false
    if (filter && !filter(comp)) return false
    return true
  }
  if (displayName !== undefined) {
    patchTargetNames.add(displayName)
    nativeFor(displayName)
  }
  // a filter-only spec matches any component its filter accepts, so offer it every one Clack draws
  if (filter) for (const n of KNOWN_COMPONENTS) nativeFor(n)
  if (inner && replacements.size) {
    // compose() applies patches in insertion order, the first innermost
    const rest = [...replacements]
    replacements.clear()
    replacements.set(fn, replacement)
    for (const [m, r] of rest) replacements.set(m, r)
  } else {
    replacements.set(fn, replacement)
  }
  applyPatches()
  return () => {
    replacements.delete(fn)
    applyPatches()
  }
}

/** Go → JS: the element Go's root renders for patched component `name` */
T.componentElement = (name, props) => {
  const React = G.React
  const p = decode(props || {})
  const C = name === 'TautComposerButtons' ? ComposerButtons : slackComponent(name)
  let el = React.createElement(C, p)
  // components inside a thread pane get it from Clack's thread view as $pane
  if (p && p.$pane) el = React.createElement(PaneContext().Provider, { value: paneFrom(p.$pane) }, el)
  return el
}
/** Go → JS: a render-prop root (MenuTrigger.renderMenu, Tooltip tip fns): fn called with args */
T.fnElement = (fnId, args) => {
  const fn = fnId >= FN_BASE ? jsFns.get(fnId) : typeof host.fnById === 'function' ? host.fnById(fnId) : null
  if (typeof fn !== 'function') return null
  const Rendered = () => fn(...decode(args || []))
  return G.React.createElement(Rendered)
}
/** Go → JS: calls a function prop jsrt serialized as {"$fn"} (not taut's), awaiting its result */
T.callHostFn = (id, args) => {
  const fn = typeof host.fnById === 'function' ? host.fnById(id) : null
  if (typeof fn !== 'function') throw new Error(`[Taut] function ${id} is gone`)
  const [a, events] = T.decodeArgs(args)
  const r = fn(...a)
  // a synthetic event's handler: what it did with it, at once (00_core.js)
  if (events) return T.eventAnswer(events)
  return Promise.resolve(r).then(encode)
}
/**
 * Go → JS: calls a function prop the way Slack calls a handler whose
 * return value decides what runs next (MAMenu's dispatchMessageAction:
 * `onSelectAction(…) || native(…)`): whether it returned something truthy,
 * right away (a returned Promise is truthy and isn't awaited), or threw
 * (then nothing else runs, as when Slack's handler throws)
 */
function truthyCall(fn, args) {
  if (typeof fn !== 'function') return { gone: true } // released with its tree: no handler to decide
  const [a] = T.decodeArgs(args)
  try {
    return { truthy: !!fn(...a) }
  } catch (err) {
    error('a handler threw', err)
    return { truthy: false, threw: true }
  }
}
T.callHostFnTruthy = (id, args) => truthyCall(typeof host.fnById === 'function' ? host.fnById(id) : null, args)
T.callFnTruthy = (id, args) => truthyCall(jsFns.get(id), args)
T.patchedComponents = () => [...patchedNames]
/** component name → how many patches match it (introspection: which patches a plugin left behind) */
T.patchCounts = () => {
  const out = {}
  for (const name of patchedNames) out[name] = matchers(name).length
  return out
}

// ---- lookups (getComponent, lazyComponent, …) ----

function lookup(name, filter) {
  const c = slackComponent(name)
  if (filter) {
    let ok = false
    try {
      ok = filter(nativeFor(name))
    } catch {}
    if (!ok) return undefined
  }
  return c
}

function getComponent(name, all = false, filter) {
  const c = lookup(name, filter)
  if (all) return c ? [c] : []
  if (!c) throw new Error(`[Taut] Could not find component: ${name}`)
  return c
}
const waitForComponent = (name, filter) => {
  const c = lookup(name, filter)
  return c ? Promise.resolve(c) : new Promise(() => {})
}
const getRenderedComponent = (name) => lookup(name)
const waitForRenderedComponent = (name) => Promise.resolve(lookup(name))
function lazyComponent(name, filter) {
  const c = lookup(name, filter)
  const React = G.React
  const L = function LazyComponent(props) {
    return c ? React.createElement(c, props) : null
  }
  L.displayName = `Lazy(${name})`
  return L
}
function getComponentSource(component) {
  if (typeof component === 'string') throw new Error(`[Taut] "${component}" is a host element, not a component`)
  const name = componentNames.get(component) ?? componentName(component)
  const raw = name && rawNatives.get(name)
  if (raw) return String(raw.$$typeof === MEMO ? raw.type : raw)
  if (typeof component === 'function') return component.toString()
  if (component?.$$typeof === MEMO) return String(component.type)
  throw new Error('[Taut] Could not find source for component')
}

// ---- api.elements ----

const elementNames = {
  SvgIcon: 'SvgIcon',
  Avatar: 'ConnectedBaseAvatar',
  ProfileHoverTrigger: 'ProfileHoverTrigger',
  MrkdwnElement: 'MrkdwnElement',
  Button: 'Button',
  Tooltip: 'Tooltip',
  IconButtonBase: 'IconButtonBase',
  ConfirmationModal: 'ConfirmationModal',
  InlineAlert: 'InlineAlert',
  Label: 'Label',
  FormTextInput: 'FormTextInput',
  Checkbox: 'Checkbox',
  FilterPill: 'FilterPill',
  Tag: 'Tag',
  DateRangePicker: 'DateRangePicker',
  DatePickerTrigger: 'DatePickerTrigger',
  FieldSet: 'FieldSet',
  Legend: 'Legend',
  Hint: 'Hint',
  BasicSelect: 'BasicSelect',
  Blocks: 'Blocks',
  MenuTrigger: 'MenuTrigger',
  MenuFromTemplate: 'MenuFromTemplate',
  ContextMenuTrigger: 'withKeyboardFocusContext(ContextMenuTrigger)',
  TimePicker: 'TimePicker',
  EmojiMenuTrigger: 'Connect(EmojiMenuTrigger)',
  Popover: 'Popover',
  RichTextInput: 'RichTextInput',
}
let elementsAPI
function elementsApi() {
  if (!elementsAPI) {
    elementsAPI = {}
    for (const [k, n] of Object.entries(elementNames)) elementsAPI[k] = lazyComponent(n)
  }
  return elementsAPI
}

// ---- overlays: modals, toasts, menus opened from JS ----

const overlays = new Map() // id → {id, kind, root, …}
let nextOverlay = 1
const tellGoOverlays = batcher(() => goOr(null, 'overlays', [...overlays.values()].map(({ element, ...o }) => o)))

/** renders `element` into a root of its own (a JS root, bit 52 set) and tells Go about it */
function openOverlay(kind, element, info = {}) {
  const id = nextOverlay++
  const root = typeof host.createRoot === 'function' ? host.createRoot() : id
  if (typeof host.render === 'function') host.render(root, element)
  overlays.set(id, { id, kind, root, element, ...info })
  tellGoOverlays()
  return {
    id,
    update(next) {
      const o = overlays.get(id)
      if (!o) return
      o.element = next
      if (typeof host.render === 'function') host.render(root, next)
    },
    close() {
      if (!overlays.delete(id)) return
      if (typeof host.unmount === 'function') host.unmount(root)
      tellGoOverlays()
    },
  }
}
T.overlays = () => [...overlays.values()].map(({ element, ...o }) => o)

const MODAL_TEXT_INPUTS = new Set(['text', 'search', 'email', 'url', 'tel', 'password', 'number'])

function baseOpenModal(options) {
  const React = G.React
  const els = elementsApi()
  let handle
  let closed = false
  const close = () => {
    if (closed) return
    closed = true
    handle?.close()
  }
  const submit = () => {
    options.onSubmit?.()
    close()
  }
  const body = React.createElement(
    'div',
    {
      onKeyDown: (event) => {
        const field = event.target
        if (
          event.key === 'Enter' &&
          !event.defaultPrevented &&
          !event.nativeEvent?.isComposing &&
          !(event.shiftKey || event.altKey || event.ctrlKey || event.metaKey) &&
          String(field?.tagName).toUpperCase() === 'INPUT' &&
          MODAL_TEXT_INPUTS.has(field.type || 'text') &&
          (options.showSubmitButton ?? true)
        ) {
          event.preventDefault()
          submit()
        }
      },
    },
    options.body
  )
  const element = React.createElement(
    els.ConfirmationModal,
    {
      title: options.title,
      submitButtonText: options.submitText ?? 'Save',
      cancelButtonText: options.cancelText ?? 'Cancel',
      submitButtonType: options.danger ? 'danger' : 'primary',
      showCancelButton: options.showCancelButton ?? true,
      showSubmitButton: options.showSubmitButton ?? true,
      onSubmit: submit,
      onCancel: () => {
        options.onCancel?.()
        close()
      },
      onClose: () => {
        options.onClose?.()
        close()
      },
    },
    body
  )
  handle = openOverlay('modal', element, { title: typeof options.title === 'string' ? options.title : 'modal' })
  return { close }
}

function dialogHelpersFor(openModal) {
  function confirm(options) {
    return new Promise((resolve) => {
      let settled = false
      const settle = (v) => {
        if (!settled) {
          settled = true
          resolve(v)
        }
      }
      const h = openModal({
        title: options.title,
        body: options.body ?? null,
        submitText: options.confirmText ?? 'Confirm',
        cancelText: options.cancelText ?? 'Cancel',
        danger: options.danger,
        onSubmit: () => settle(true),
        onCancel: () => settle(false),
        onClose: () => settle(false),
      })
      if (!h) settle(false)
    })
  }
  function alert(options) {
    return new Promise((resolve) => {
      let settled = false
      const settle = () => {
        if (!settled) {
          settled = true
          resolve()
        }
      }
      const h = openModal({
        title: options.title,
        body: options.body ?? null,
        submitText: options.closeText ?? 'OK',
        showCancelButton: false,
        onSubmit: settle,
        onClose: settle,
      })
      if (!h) settle()
    })
  }
  return { confirm, alert }
}

let modalAPI
function modalApi() {
  if (modalAPI) return modalAPI
  const React = G.React
  const els = elementsApi()
  function TextInput({ autoFocus, inputRef, ...props }) {
    const input = React.useRef(null)
    React.useEffect(() => {
      if (autoFocus) input.current?.focus?.()
    }, [])
    return React.createElement(els.FormTextInput, {
      ...props,
      autoFocus,
      inputRef: (node) => {
        input.current = node
        if (typeof inputRef === 'function') inputRef(node)
        else if (inputRef) inputRef.current = node
      },
    })
  }
  modalAPI = { openModal: baseOpenModal, ...dialogHelpersFor(baseOpenModal), Label: els.Label, TextInput }
  return modalAPI
}

let menuAPI
function menuApi() {
  if (menuAPI) return menuAPI
  const React = G.React
  const els = elementsApi()
  function Menu({ template, position, children }) {
    return React.createElement(els.MenuTrigger, { position, renderMenu: (menuProps) => React.createElement(els.MenuFromTemplate, { ...menuProps, template }) }, children)
  }
  function ContextMenu({ template, children }) {
    return React.createElement(els.ContextMenuTrigger, { getTemplate: () => template, desktopAppOnly: false, showSlackKitContextMenuOnDesktop: true }, children)
  }
  menuAPI = { Menu, ContextMenu }
  return menuAPI
}

// Slack's openModal action, for plugins that dispatch it themselves
const openModalAction = withMeta('openModal', ({ element, name } = {}) => ({ type: 'OPEN_MODAL', payload: { element, name } }))
onAction('OPEN_MODAL', (action) => {
  const h = openOverlay('modal', action.payload.element, { title: action.payload.name || 'modal' })
  return { close: h.close, render: (props) => h.update(G.React.cloneElement(action.payload.element, props)) }
})
defineModule('modal', { openModal: openModalAction })

// ---- preferences tabs ----

const preferencesTabs = new Store([])
let lastTab = 0
function addTab(tab) {
  const added = { ...tab, id: `taut-tab-${++lastTab}` }
  preferencesTabs.update((tabs) => [...tabs, added].sort((a, b) => String(a.label).localeCompare(String(b.label))))
  tellGoTabs()
  return () => {
    preferencesTabs.update((tabs) => tabs.filter((t) => t !== added))
    tellGoTabs()
  }
}
const tellGoTabs = batcher(() => goOr(null, 'preferencesTabs', preferencesTabs.get().map(({ id, label, icon }) => ({ id, label, icon }))))
T.preferencesTabs = () => preferencesTabs.get().map(({ id, label, icon }) => ({ id, label, icon }))
/** Go → JS: the element a preferences tab shows */
T.preferencesTabElement = (id) => {
  const tab = preferencesTabs.get().find((t) => t.id === id)
  if (!tab) return null
  const Tab = () => tab.render()
  return G.React.createElement(Tab)
}
const preferences = { addTab }

Object.assign(T, { patchComponent, patchComponentInner, slackComponent, nativeFor, feedNative, getComponent, waitForComponent, getRenderedComponent, waitForRenderedComponent, getComponentSource, componentName, lazyComponent, elementsApi, modalApi, menuApi, dialogHelpersFor, openOverlay })
