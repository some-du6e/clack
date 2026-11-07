// The composer's Slack shapes on the plugin runtime (plugins.go installs
// this as a taut extension prelude). A classic script: runs once per
// runtime, after the TautAPI is up and before any plugin starts.
;(() => {
  const G = globalThis
  const T = G.__taut
  if (!T || G.__clackComposer) return

  // ---- TextyInput: the editor's length and selection ----
  // taut gives TextyInput Slack's shape (memo(class TextyInput), a ref with
  // a Quill: internal/taut/js/04_react.js). The composer feeds its Quill:
  // Go passes the editor's state as $texty ({length, index, selected},
  // characters), which the native itself doesn't need.
  T.feedNative?.('TextyInput', (props) => {
    const { $texty, ...rest } = props
    return { state: $texty || null, props: rest }
  })

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
