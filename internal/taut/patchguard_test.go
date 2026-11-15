package taut

import (
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/some-du6e/clack/internal/jsrt"
)

// A plugin's patch that throws must not empty its component's root. Every
// patched component Go draws is a React root of its own, and React
// unmounts a root on an error nothing catches: the host tree came back
// "[]" and Go drew nothing for the component. That was the composer going
// away from a channel until a restart (Better Typing Indicator patches
// MessagePaneInput, and its typing line renders again whenever someone
// types). PatchGuard draws the original instead and logs the error.

// composerProps is the channel composer's boundary as Go passes it: its
// parts are slots down to the typing line, as in composer/slack.go.
func composerProps(n int) map[string]any {
	return map[string]any{"$key": chGen + "/", "channelId": chGen, "n": n, "children": []any{
		map[string]any{"$slot": 1, "n": "BaseContextBarV2", "p": map[string]any{"$key": chGen + "/"}},
		map[string]any{"$slot": 2, "n": "MessageInputRoadblock", "p": map[string]any{"$key": chGen + "/", "children": []any{
			map[string]any{"$slot": 3, "n": "MessageInput", "p": map[string]any{"$key": chGen + "/", "children": []any{
				map[string]any{"$slot": 4, "n": "WysiwygContainer", "p": map[string]any{"$key": chGen + "/"}},
				map[string]any{"$slot": 5, "n": "TypingNames", "p": map[string]any{"$key": chGen + "/"}},
			}}},
		}}},
	}}
}

// hasNatives reports which of names have no "@" node in the tree.
func missingNatives(nodes []any, names ...string) []string {
	var out []string
	for _, n := range names {
		if findNative(nodes, n) == nil {
			out = append(out, n)
		}
	}
	return out
}

func hasClass(nodes []any, class string) bool {
	return findHost(nodes, func(m map[string]any) bool {
		p, _ := m["p"].(map[string]any)
		c, _ := p["className"].(string)
		return strings.Contains(" "+c+" ", " "+class+" ")
	}) != nil
}

func (h *harness) loggedPatchError(name string) bool {
	for _, l := range h.jsLogged() {
		if strings.Contains(l, "a plugin's patch of "+name+" threw") {
			return true
		}
	}
	return false
}

// A patch of the composer's own boundary that throws while rendering: the
// root still draws MessagePaneInput's native, with every part inside it.
func TestPatchThrowDrawsOriginal(t *testing.T) {
	h := newHarness(t)
	h.js(`
		api.patchComponent('MessagePaneInput', (Original) => (props) => { throw new Error('synthetic render failure') })
		return 1`)
	nodes := h.render(5101, "MessagePaneInput", composerProps(1))
	if miss := missingNatives(nodes, "MessagePaneInput", "MessageInputRoadblock", "MessageInput", "WysiwygContainer"); len(miss) > 0 {
		t.Fatalf("the composer's tree lacks %v after its patch threw: %v", miss, nodes)
	}
	eventually(t, 2*time.Second, "the error logged", func() bool { return h.loggedPatchError("MessagePaneInput") })
}

// Better Typing Indicator's shape: MessagePaneInput wrapped in a context
// provider, the typing line (nested inside its tree) patched too. The
// typing line's patch throws: only the typing line falls back, and the
// composer around it keeps its tree.
func TestNestedPatchThrowKeepsParent(t *testing.T) {
	h := newHarness(t)
	h.js(`
		const R = globalThis.React
		const Conv = R.createContext(null)
		api.patchComponent('MessagePaneInput', (Original) => (props) =>
			R.createElement(Conv.Provider, { value: props.channelId }, R.createElement('div', { className: 'bti-wrap' }, R.createElement(Original, props))))
		api.patchComponent('TypingNames', (Original) => (props) => {
			const ch = R.useContext(Conv)
			if (ch) throw new TypeError("Cannot read properties of undefined (reading 'profile')")
			return R.createElement(Original, props)
		})
		return 1`)
	nodes := h.render(5102, "MessagePaneInput", composerProps(1))
	if miss := missingNatives(nodes, "MessagePaneInput", "MessageInputRoadblock", "MessageInput", "WysiwygContainer", "TypingNames"); len(miss) > 0 {
		t.Fatalf("the composer's tree lacks %v after the typing line's patch threw: %v", miss, nodes)
	}
	if !hasClass(nodes, "bti-wrap") {
		t.Errorf("MessagePaneInput's own patch was dropped too (only TypingNames threw): %v", nodes)
	}
	eventually(t, 2*time.Second, "the error logged", func() bool { return h.loggedPatchError("TypingNames") })
	if h.loggedPatchError("MessagePaneInput") {
		t.Error("MessagePaneInput's patch was blamed for TypingNames' error")
	}
}

// A patch whose layout effect throws: the commit had already gone out
// with the patch's output; the next one draws the original.
func TestPatchEffectThrowDrawsOriginal(t *testing.T) {
	h := newHarness(t)
	h.js(`
		const R = globalThis.React
		api.patchComponent('MessagePaneInput', (Original) => (props) => {
			R.useLayoutEffect(() => { throw new Error('synthetic effect failure') }, [])
			return R.createElement('div', { className: 'patched' }, R.createElement(Original, props))
		})
		return 1`)
	h.render(5103, "MessagePaneInput", composerProps(1))
	var nodes []any
	eventually(t, renderWait(), "the original drawn after the effect threw", func() bool {
		nodes = h.tree(5103)
		return findNative(nodes, "MessagePaneInput") != nil && !hasClass(nodes, "patched")
	})
	if miss := missingNatives(nodes, "MessageInputRoadblock", "MessageInput", "WysiwygContainer"); len(miss) > 0 {
		t.Fatalf("the composer's tree lacks %v: %v", miss, nodes)
	}
	eventually(t, 2*time.Second, "the error logged", func() bool { return h.loggedPatchError("MessagePaneInput") })
}

// The guard tries the patch again when Go sends new props: one bad render
// doesn't turn a plugin off for good.
func TestPatchRetriedOnNewProps(t *testing.T) {
	h := newHarness(t)
	h.js(`
		const R = globalThis.React
		globalThis.__fail = true
		api.patchComponent('MessagePaneInput', (Original) => (props) => {
			if (globalThis.__fail) throw new Error('synthetic transient failure')
			return R.createElement('div', { className: 'patched' }, R.createElement(Original, props))
		})
		return 1`)
	nodes := h.render(5104, "MessagePaneInput", composerProps(1))
	if findNative(nodes, "MessagePaneInput") == nil || hasClass(nodes, "patched") {
		t.Fatalf("first render: want the original alone, got %v", nodes)
	}
	h.js(`globalThis.__fail = false; return 1`)
	h.render(5104, "MessagePaneInput", composerProps(2))
	eventually(t, renderWait(), "the patch drawn again after new props", func() bool {
		nodes = h.tree(5104)
		return hasClass(nodes, "patched") && findNative(nodes, "MessagePaneInput") != nil
	})
}

// Without a patch nothing changes: the native is drawn as before, and a
// patch that renders fine is drawn with no guard in the host tree.
func TestPatchGuardInvisible(t *testing.T) {
	h := newHarness(t)
	nodes := h.render(5105, "MessagePaneInput", composerProps(1))
	if findNative(nodes, "MessagePaneInput") == nil {
		t.Fatalf("unpatched: %v", nodes)
	}
	h.js(`
		const R = globalThis.React
		api.patchComponent('MessagePaneInput', (Original) => (props) => R.createElement('div', { className: 'patched' }, R.createElement(Original, props)))
		return 1`)
	nodes = h.render(5106, "MessagePaneInput", composerProps(1))
	if len(nodes) != 1 || !hasClass(nodes, "patched") || findNative(nodes, "MessagePaneInput") == nil {
		t.Fatalf("patched: want div.patched around the native, got %v", nodes)
	}
	if h.loggedPatchError("MessagePaneInput") {
		t.Errorf("a patch that renders fine was logged as throwing: %q", h.jsLogged())
	}
}

// The mechanism PatchGuard is for, in Clack's runtime: an error no
// boundary catches unmounts the React root, and the root commits an empty
// host tree. That was the composer's tree when a plugin inside it threw.
func TestUncaughtRenderErrorEmptiesRoot(t *testing.T) {
	h := newHarness(t)
	r := h.rt()
	h.js(`
		const R = globalThis.React
		globalThis.__boomEl = (fail) => R.createElement(function Boom() {
			if (fail) throw new Error('synthetic uncaught render error')
			return R.createElement('div', { className: 'boom-ok' })
		})
		return 1`)
	var mu sync.Mutex
	var last string
	root := r.rt.NewRoot(func(tree []byte) {
		mu.Lock()
		last = string(tree)
		mu.Unlock()
	})
	render := func(fail bool) error {
		done := make(chan error, 1)
		r.rt.Post(func(js *jsrt.Ctx) {
			el, err := js.Global().Get("__boomEl").Call(fail)
			if err == nil {
				err = root.Render(js, el)
			}
			done <- err
		})
		return <-done
	}
	tree := func() string {
		mu.Lock()
		defer mu.Unlock()
		return last
	}
	if err := render(false); err != nil {
		t.Fatal(err)
	}
	eventually(t, renderWait(), "the first tree", func() bool { return strings.Contains(tree(), "boom-ok") })
	if err := render(true); err != nil {
		t.Logf("the throwing render returned %v", err) // React reports it as uncaught instead
	}
	eventually(t, renderWait(), "the root's tree emptied", func() bool { return tree() == "[]" })
}
