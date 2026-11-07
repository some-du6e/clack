// The composer's Slack shapes on the plugin runtime (plugins.go installs
// this as a taut extension prelude). A classic script: runs once per
// runtime, after the TautAPI is up and before any plugin starts.
;(() => {
  const G = globalThis
  const T = G.__taut
  const host = G.__clack
  const React = G.React
  if (!T || !host || !React || G.__clackComposer) return

  // ---- TextyInput: Slack's memo(class TextyInput) ----
  // Plugins find Slack's editor with {filter} on its type (CharacterCounter:
  // a memo whose type's prototype has getNumCharsOverLimit) and read it
  // through the ref they pass: ref.instance.quill (getLength, getSelection,
  // on/off for 'editor-change', 'text-change', 'selection-change'). The
  // native is the composer's editor; Go passes its state as $texty
  // ({length, index, selected}, characters).
  class TextyInput {
    getNumCharsOverLimit() {
      return 0
    }
  }

  function quillState() {
    const listeners = new Map()
    let s = { length: 0, index: 0, selected: 0 }
    const emit = (ev, ...args) => {
      const set = listeners.get(ev)
      if (!set) return
      for (const fn of [...set]) {
        try {
          fn(...args)
        } catch (e) {
          console.error(e)
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

  const num = (v) => (typeof v === 'number' && Number.isFinite(v) ? v : 0)
  const nativeOf = host.native
  if (typeof nativeOf === 'function') {
    const base = nativeOf('TextyInput')
    const TextyInputHost = function (props) {
      const { ref, $texty, ...rest } = props
      const q = React.useMemo(quillState, [])
      const max = React.useRef(0)
      max.current = num(props.maxLength)
      const handle = React.useMemo(
        () => ({ instance: { quill: q.quill, getNumCharsOverLimit: () => (max.current > 0 ? Math.max(0, q.get().length - max.current) : 0) } }),
        [q]
      )
      React.useLayoutEffect(() => {
        if ($texty) q.set({ length: num($texty.length), index: num($texty.index), selected: num($texty.selected) })
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
      return React.createElement(base, rest)
    }
    TextyInputHost.displayName = 'TextyInput'
    // taut's native for TextyInput renders TextyInputHost: swap it in while
    // taut creates the native, then put jsrt's lookup back.
    host.native = (name) => (name === 'TextyInput' ? TextyInputHost : nativeOf(name))
    try {
      T.nativeFor('TextyInput')
    } finally {
      host.native = nativeOf
    }
  }
  const raw = T.nativeFor('TextyInput')
  // Slack's type is memo(class TextyInput); React still sees a function
  // component (it checks typeof before $$typeof).
  raw.$$typeof = Symbol.for('react.memo')
  raw.type = TextyInput

  // ---- files: the name an attachment goes out under ----
  // Slack's upload reads File.name, which plugins may redefine
  // (AnonymiseFileNames patches File.prototype's name getter).
  G.__clackComposer = {
    fileName(name, type) {
      if (typeof File === 'undefined') return name
      return new File([], String(name), { type: type || '' }).name
    },
  }
})()
