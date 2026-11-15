package composer

import (
	"image"
	"strings"
	"sync"
	"testing"
	"time"

	"gioui.org/io/input"
	"gioui.org/layout"
	"gioui.org/op"
	"gioui.org/unit"

	"github.com/some-du6e/clack/internal/ui/slackc"
)

// throwingTyping is shaped like Better Typing Indicator (the plugin Karim's
// composer vanished under): MessagePaneInput wrapped in a context provider,
// and a TypingNames patch inside the composer's tree, which throws the way
// a member lookup on a missing profile does once someone types.
const throwingTyping = `(() => {
const TautPlugin = globalThis.TautPlugin
class ThrowingTyping extends TautPlugin {
  static id = 'ThrowingTyping'
  static pluginName = 'Throwing Typing'
  static description = 'a typing line that throws (test)'
  static authors = ['test']
  static defaultConfig = { enabled: true }
  start() {
    const R = globalThis.React
    const Conv = R.createContext(null)
    this.api.patchComponent('MessagePaneInput', (Original) => (props) =>
      R.createElement(Conv.Provider, { value: props.channelId },
        R.createElement('div', { className: 'bti-wrap' }, R.createElement(Original, props))))
    this.api.patchComponent('TypingNames', (Original) => (props) => {
      if (R.useContext(Conv)) throw new TypeError("Cannot read properties of undefined (reading 'profile')")
      return R.createElement(Original, props)
    })
  }
  stop() {}
}
return ThrowingTyping
})()`

// The whole path, through Taut's runtime: someone types, the plugin's
// typing line throws inside the composer's React tree. Before PatchGuard
// React unmounted the composer's root and its tree came back "[]"; now
// only the typing line falls back to Clack's own, the composer keeps the
// plugin's tree around its box, and the error is logged.
func TestTautThrowingTypingLineKeepsComposer(t *testing.T) {
	var mu sync.Mutex
	var jsErrs []string
	e := startTautCode(t, []byte(throwingTyping), nil, func(level, msg string) {
		mu.Lock()
		jsErrs = append(jsErrs, msg)
		mu.Unlock()
	})
	e.until(10*time.Second, "the plugin's patches", nil, func() bool {
		return slackc.Patched("MessagePaneInput") && slackc.Patched("TypingNames")
	})
	var lines []string
	OnVanish = func(l string) { lines = append(lines, l) }
	t.Cleanup(func() { OnVanish = nil })

	clearDraft(tChGen, "")
	c := New(e.h, tChGen, "")
	t.Cleanup(func() { c.Close(); clearDraft(tChGen, "") })
	ops, r := new(op.Ops), new(input.Router)
	frame := func() {
		ops.Reset()
		gtx := layout.Context{Ops: ops, Metric: unit.Metric{PxPerDp: 1, PxPerSp: 1},
			Constraints: layout.Constraints{Min: image.Pt(900, 0), Max: image.Pt(900, 500)}, Now: time.Now(), Source: r.Source()}
		slackc.Frame(gtx)
		c.Layout(gtx)
		r.Frame(ops)
		if c.BoxRect().Empty() {
			t.Fatalf("a frame without the message box (plugin trees: %s)", slackc.KeyedTrees(c.key()))
		}
	}
	// the composer's tree as the plugin last drew it
	tree := func() string {
		e.mu.Lock()
		defer e.mu.Unlock()
		for _, b := range e.trees {
			if s := string(b); strings.Contains(s, "bti-wrap") {
				return s
			}
		}
		return ""
	}
	e.until(10*time.Second, "the composer through the plugin", frame, func() bool {
		return strings.Contains(tree(), `"n":"MessagePaneInput"`)
	})

	// someone types in the channel
	hub := typingHub(e.app)
	hub.mu.Lock()
	hub.m[draftKey(tChGen, "")] = map[string]typer{"UALICE": {name: "alice", until: time.Now().Add(time.Minute)}}
	hub.mu.Unlock()
	e.until(10*time.Second, "the typing line in the composer's tree", frame, func() bool {
		return strings.Contains(tree(), `"n":"TypingNames"`)
	})
	for i := 0; i < 5; i++ {
		frame()
	}
	s := tree()
	if !strings.Contains(s, `"n":"MessagePaneInput"`) || !strings.Contains(s, `"n":"MessageInputRoadblock"`) {
		t.Errorf("the composer's tree lost its parts when the typing line threw: %s", s)
	}
	if c.BoxVanished() || len(lines) > 0 {
		t.Errorf("the Go guard had to step in (the JS root emptied?): %q", lines)
	}
	if c.TypingLine() == "" {
		t.Error("no typing line in the composer state")
	}
	mu.Lock()
	defer mu.Unlock()
	found := false
	for _, l := range jsErrs {
		if strings.Contains(l, "a plugin's patch of TypingNames threw") {
			found = true
		}
	}
	if !found {
		t.Errorf("the throw wasn't logged: %q", jsErrs)
	}
}
