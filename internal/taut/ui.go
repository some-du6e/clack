package taut

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"sort"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/some-du6e/clack/internal/core"
	"github.com/some-du6e/clack/internal/jsrt"
	"github.com/some-du6e/clack/internal/slack"
)

// UIHooks connect plugins to Clack's native UI. The ui root fills them in
// (wiring slackc, popups, composer); every field is optional. They are
// called from any goroutine (often the JS thread), so they must not block.
type UIHooks struct {
	// Attached is called once the JS runtime is up, with the slackc.Bridge
	// implementation (ui root: slackc.SetBridge(b)).
	Attached func(b *Bridge)
	// Deliver installs a host tree for a root (slackc.Deliver).
	Deliver func(root uint64, tree []byte)
	// SetPatched is the full set of patched component names (slackc.SetPatched).
	SetPatched func(names []string)
	// CallGo runs a slackc Go func that crossed into JS as {"$gofn":id}.
	CallGo func(id int64, args []byte) ([]byte, error)
	// SetStyle / RemoveStyle feed slackc's CSS engine (api.setStyle, user CSS "user").
	SetStyle    func(key, css string)
	RemoveStyle func(key string)
	// Overlays is every open plugin modal/overlay; their roots' trees come
	// through Deliver (ids with bit 52 set).
	Overlays func(o []Overlay)
	// PreferencesTabs lists tabs plugins added to Preferences.
	PreferencesTabs func(tabs []PreferencesTab)
	// Invalidate asks for a redraw (patched views changed, settings changed).
	Invalidate func()
	// Toast shows a short notice (enqueueHuddleToast); el is a root to
	// render when the content is a React element (0 = text only).
	Toast func(text string, icon string, d time.Duration)
	// OpenPreferences opens Preferences (on a tab when given).
	OpenPreferences func(tab string)
	// DeepLink opens a slack:// URI inside Clack (Slack's handleDeepLink
	// thunk: slack://channel?team=…&id=…, slack://user?…, slack://app?…);
	// true if it was handled. nil: handleDeepLink resolves false.
	DeepLink func(uri string) bool
	// ThreadPane answers api.threads.usePane(): op loaded | topReply |
	// jumpToReply for the pane showing ch/threadTs.
	ThreadPane func(op, ch, threadTs, arg string) any
	// Input is Slack's input proxy for a composer: op isEmpty | getContents
	// | setContents | focus | insertText.
	Input func(op string, where InputWhere, arg json.RawMessage) any
	// SetLocalDraft puts a draft (Slack's setLocalDraft action) into its composer.
	SetLocalDraft func(draft json.RawMessage)
	// SwitchAccount signs in as creds (empty: show the sign-in screen).
	SwitchAccount func(c slack.Creds)
	// OnDocument gets the DOM plugins append to document.body (a host
	// tree) and every <style> in document.head (slackc.SetDocument).
	OnDocument func(body []byte, css []string)
	// OnDocumentProps gets <html>'s and <body>'s own props: classes,
	// inline style vars (slackc.SetDocumentProps).
	OnDocumentProps func(html, body []byte)
}

// InputWhere names a composer the way Slack does.
type InputWhere struct {
	ViewContext string `json:"viewContext"`
	ChannelID   string `json:"channelId"`
}

// RegisterInput tells plugins a composer exists (Slack's isInputRegistered).
func (t *Taut) RegisterInput(w InputWhere, has bool) {
	if !t.Started() {
		return
	}
	if rt, err := t.ensureRuntime(); err == nil {
		rt.post("registerInput", w, has)
	}
}

// Overlay is a plugin modal (or other floating layer) to draw above the shell.
type Overlay struct {
	ID    int64  `json:"id"`
	Kind  string `json:"kind"` // modal
	Root  uint64 `json:"root"`
	Title string `json:"title"`
}

// PreferencesTab is a tab a plugin added with api.preferences.addTab.
type PreferencesTab struct {
	ID    string `json:"id"`
	Label string `json:"label"`
	Icon  string `json:"icon"`
}

type statePatchInfo struct {
	Version int64    `json:"version"`
	All     bool     `json:"all"`
	Slices  []string `json:"slices"`
}

type composerHooks struct {
	Buttons []struct {
		ID        string   `json:"id"`
		Placement string   `json:"placement"`
		Kinds     []string `json:"kinds"`
	} `json:"buttons"`
	SendChecks     int  `json:"sendChecks"`
	LinkTransforms int  `json:"linkTransforms"`
	InlineMarkup   bool `json:"inlineMarkup"`
	SendTransforms int  `json:"sendTransforms"`
}

type uiState struct {
	t *Taut

	mu         sync.Mutex
	components []string
	composer   composerHooks
	state      statePatchInfo
	overlays   []Overlay
	tabs       []PreferencesTab
	rtmAll     bool
	rtmTypes   map[string]bool

	patchedSlices atomic.Pointer[statePatchInfo]
	hooks         atomic.Pointer[composerHooks]
	actionable    atomic.Bool

	roots   map[uint64]*jsrt.Root // slackc root → jsrt root
	rootsMu sync.Mutex

	views viewCache
}

func newUIState(t *Taut) *uiState {
	u := &uiState{t: t, roots: map[uint64]*jsrt.Root{}}
	u.views.init(t)
	return u
}

func (u *uiState) publishPatched() {
	u.mu.Lock()
	names := append([]string(nil), u.components...)
	if len(u.composer.Buttons) > 0 {
		names = append(names, "TautComposerButtons")
	}
	u.mu.Unlock()
	sort.Strings(names)
	if f := u.t.opts.UI.SetPatched; f != nil {
		f(names)
	}
}

func (u *uiState) setPatchedComponents(names []string) {
	u.mu.Lock()
	u.components = names
	u.mu.Unlock()
	u.publishPatched()
}

func (u *uiState) setComposerHooks(h composerHooks) {
	u.mu.Lock()
	u.composer = h
	u.mu.Unlock()
	u.hooks.Store(&h)
	u.publishPatched()
}

func (u *uiState) setStatePatched(s statePatchInfo) {
	u.mu.Lock()
	u.state = s
	u.mu.Unlock()
	if !s.All && len(s.Slices) == 0 {
		u.patchedSlices.Store(nil)
	} else {
		u.patchedSlices.Store(&s)
	}
	u.views.staleAll()
}

func (u *uiState) setRTMTypes(types []string) {
	m := map[string]bool{}
	all := false
	for _, t := range types {
		if t == "*" {
			all = true
		}
		m[t] = true
	}
	u.mu.Lock()
	u.rtmTypes, u.rtmAll = m, all
	u.mu.Unlock()
}

func (u *uiState) setOverlays(o []Overlay) {
	u.mu.Lock()
	u.overlays = o
	u.mu.Unlock()
	if f := u.t.opts.UI.Overlays; f != nil {
		f(o)
	}
}

func (u *uiState) setTabs(tabs []PreferencesTab) {
	u.mu.Lock()
	u.tabs = tabs
	u.mu.Unlock()
	if f := u.t.opts.UI.PreferencesTabs; f != nil {
		f(tabs)
	}
	u.t.changed()
}

// Overlays returns the open plugin overlays.
func (t *Taut) Overlays() []Overlay {
	t.ui.mu.Lock()
	defer t.ui.mu.Unlock()
	return append([]Overlay(nil), t.ui.overlays...)
}

// PreferencesTabs returns the tabs plugins added.
func (t *Taut) PreferencesTabs() []PreferencesTab {
	t.ui.mu.Lock()
	defer t.ui.mu.Unlock()
	return append([]PreferencesTab(nil), t.ui.tabs...)
}

// onRaw forwards a realtime frame to rtm.on listeners (only the types some
// plugin listens for).
func (t *Taut) onRaw(ev slack.Event) {
	t.ui.mu.Lock()
	want := t.ui.rtmAll || t.ui.rtmTypes[ev.Type]
	t.ui.mu.Unlock()
	if !want || len(ev.Raw) == 0 {
		return
	}
	t.mu.Lock()
	rt := t.rt
	t.mu.Unlock()
	if rt != nil {
		rt.post("rtmEvent", json.RawMessage(ev.Raw))
	}
}

func (t *Taut) toast(o obj) {
	f := t.opts.UI.Toast
	if f == nil {
		return
	}
	text := o.str("content")
	var icon struct {
		Name string `json:"name"`
	}
	_ = json.Unmarshal(o["svgIconProps"], &icon)
	var ms float64
	_ = json.Unmarshal(o["duration"], &ms)
	if ms <= 0 {
		ms = 5000
	}
	f(text, icon.Name, time.Duration(ms)*time.Millisecond)
}

// ---- the slackc bridge ----

// Bridge implements slackc.Bridge over the JS runtime (the ui root passes
// it to slackc.SetBridge; internal/taut doesn't import Gio).
type Bridge struct{ t *Taut }

// Bridge returns the slackc bridge.
func (t *Taut) Bridge() *Bridge { return &Bridge{t: t} }

// with runs f with the runtime. slackc calls the Bridge from the UI
// goroutine, which must never wait for a runtime to start (hundreds of ms
// cold, up to the init timeout) or to restart after a crash: without a
// live one the start happens in the background and f runs once it's up
// (not at all if it fails).
func (b *Bridge) with(f func(r *runtime)) {
	if r := b.t.liveRuntime(); r != nil {
		f(r)
		return
	}
	go func() {
		if r, err := b.t.ensureRuntime(); err == nil {
			f(r)
		}
	}()
}

func (b *Bridge) root(r *runtime, id uint64) *jsrt.Root {
	u := b.t.ui
	u.rootsMu.Lock()
	defer u.rootsMu.Unlock()
	jr := u.roots[id]
	if jr == nil {
		deliver := b.t.opts.UI.Deliver
		jr = r.rt.NewRoot(func(tree []byte) {
			if deliver != nil {
				deliver(id, tree)
			}
		})
		u.roots[id] = jr
	}
	return jr
}

// mount renders the element __taut.<fn>(args…) returns into root id.
func (b *Bridge) mount(id uint64, fn string, args ...any) { b.mountAt(id, 0, fn, args...) }

// mountAt is mount; with k > 0 the root is a portal inside the tree that
// holds host node k (jsrt Root.RenderPortal). It never starts the runtime
// on the caller's (UI) goroutine: see with.
func (b *Bridge) mountAt(id uint64, k int64, fn string, args ...any) {
	b.with(func(r *runtime) { b.mountIn(r, id, k, fn, args...) })
}

func (b *Bridge) mountIn(r *runtime, id uint64, k int64, fn string, args ...any) {
	jr := b.root(r, id)
	r.rt.Post(func(js *jsrt.Ctx) {
		f := js.Global().Get("__taut").Get(fn)
		vals := make([]any, len(args))
		for i, a := range args {
			switch x := a.(type) {
			case []byte:
				v, err := js.ParseJSON(x)
				if err != nil {
					b.t.logf("taut: %s: bad JSON", fn)
					return
				}
				vals[i] = v
			default:
				vals[i] = x
			}
		}
		el, err := f.CallThis(js.Global().Get("__taut"), vals...)
		if err != nil {
			b.t.logf("taut: %s %v: %s", fn, logArg(args), errWithStack(err))
			return
		}
		if k > 0 {
			err = jr.RenderPortal(js, k, el)
		} else {
			err = jr.Render(js, el)
		}
		if err != nil {
			b.t.logf("taut: render %s %v: %s", fn, logArg(args), errWithStack(err))
		}
	})
}

// Render mounts or updates root with the (patched) Slack component name.
func (b *Bridge) Render(root uint64, name string, props []byte) {
	if len(props) == 0 {
		props = []byte("{}")
	}
	b.mount(root, "componentElement", name, props)
}

// RenderIn is Render for a patched component a native draws inside the
// patched root parent, under its host node parentK (slackc.NestedBridge):
// it mounts as a portal there, so the contexts and providers that root's
// patches put around that native reach it, as in Slack's single tree.
func (b *Bridge) RenderIn(root, parent uint64, parentK int64, name string, props []byte) {
	if len(props) == 0 {
		props = []byte("{}")
	}
	if parentK <= 0 {
		b.mount(root, "componentElement", name, props)
		return
	}
	b.mountAt(root, parentK, "componentElement", name, props)
}

// RenderFn mounts root rendering the JS function fn called with args.
func (b *Bridge) RenderFn(root uint64, fn int64, args []byte) {
	if len(args) == 0 {
		args = []byte("[]")
	}
	b.mount(root, "fnElement", float64(fn), args)
}

// Unmount unmounts root.
func (b *Bridge) Unmount(root uint64) {
	u := b.t.ui
	u.rootsMu.Lock()
	jr := u.roots[root]
	delete(u.roots, root)
	u.rootsMu.Unlock()
	if jr != nil {
		jr.Unmount()
	}
}

// Dispatch fires a DOM event at host node target.
func (b *Bridge) Dispatch(target int64, event []byte) {
	b.with(func(r *runtime) { r.rt.DispatchEvent(int(target), event) })
}

// DispatchResult is Dispatch telling done (any goroutine) whether a JS
// listener stopped the event's propagation (slackc.ResultBridge: a focused
// plugin element's Escape keydown goes on to Clack's overlay layers unless
// one did, as a DOM keydown bubbles up to react-modal). With no runtime to
// dispatch to, or one that shuts down first (its queued tasks never run),
// done hears false.
func (b *Bridge) DispatchResult(target int64, event []byte, done func(propagationStopped bool)) {
	b.withAnswer(done, func(r *runtime, answer func(bool)) {
		r.rt.DispatchEventThen(int(target), event, func(res jsrt.EventResult, err error) {
			answer(err == nil && res.PropagationStopped)
		})
	})
}

// CallEvent calls JS function prop fn with args that carry a DOM event
// ({"$event": fields}, which the TautAPI makes React's synthetic event:
// the handler can call stopPropagation and preventDefault) and tells done
// (any goroutine) whether it stopped the event (slackc.EventBridge: kit's
// FormTextInput onKeyDown). One JS task, queued in order with Dispatch's
// (the same field's DOM keydown comes after it). done hears false with no
// runtime, one that shuts down first, or a call that fails.
func (b *Bridge) CallEvent(fn int64, args []byte, done func(stopped bool)) {
	path := "callHostFn"
	if IsTautFn(fn) {
		path = "callFn"
	}
	b.withAnswer(done, func(r *runtime, answer func(bool)) {
		r.rt.Post(func(js *jsrt.Ctx) {
			stopped := false
			defer func() { answer(stopped) }()
			a, err := js.ParseJSON(args)
			if err != nil {
				return
			}
			v, err := js.Global().Get("__taut").Get(path).Call(float64(fn), a)
			if err != nil {
				return
			}
			res, err := v.JSON()
			if err != nil {
				return
			}
			var out struct {
				Event struct {
					Stopped bool `json:"stopped"`
				} `json:"$event"`
			}
			if json.Unmarshal(res, &out) == nil {
				stopped = out.Event.Stopped
			}
		})
	})
}

// ErrFnGone is CallTruthy's answer for a function JS no longer has (its
// tree re-rendered or unmounted): no handler is there to decide.
var ErrFnGone = errors.New("taut: the JS function is gone")

// CallTruthy calls JS function prop fn with args the way Slack calls a
// handler whose return value decides what runs next (slackc.TruthyBridge;
// MAMenu's dispatchMessageAction: `onSelectAction(…) || native(…)`): done
// hears, on any goroutine, whether it returned something truthy as JS
// sees it, right away (a Promise is truthy and isn't awaited), or threw
// (a handler the runtime interrupted, past its task timeout, counts as
// one); err when no handler answered: no runtime could start, it shut
// down first, or the function is gone (ErrFnGone). Like a click in
// Slack's page, the call waits its turn on the JS thread. Args carrying
// {"$event": fields} give the handler React's synthetic event.
func (b *Bridge) CallTruthy(fn int64, args []byte, done func(truthy, threw bool, err error)) {
	path := "callHostFnTruthy"
	if IsTautFn(fn) {
		path = "callFnTruthy"
	}
	var once sync.Once
	answered := make(chan struct{})
	answer := func(truthy, threw bool, err error) {
		once.Do(func() {
			close(answered)
			done(truthy, threw, err)
		})
	}
	run := func(r *runtime) {
		go func() {
			select {
			case <-r.rt.Done():
				answer(false, false, jsrt.ErrClosed)
			case <-answered:
			}
		}()
		r.rt.Post(func(js *jsrt.Ctx) {
			a, err := js.ParseJSON(args)
			if err != nil {
				answer(false, false, err)
				return
			}
			v, err := js.Global().Get("__taut").Get(path).Call(float64(fn), a)
			if err != nil {
				answer(false, true, nil) // the handler failed as it ran (interrupted): a throw
				return
			}
			res, err := v.JSON()
			if err != nil {
				answer(false, false, err)
				return
			}
			var out struct {
				Truthy bool `json:"truthy"`
				Threw  bool `json:"threw"`
				Gone   bool `json:"gone"`
			}
			if err := json.Unmarshal(res, &out); err != nil {
				answer(false, false, err)
				return
			}
			if out.Gone {
				answer(false, false, ErrFnGone)
				return
			}
			answer(out.Truthy, out.Threw, nil)
		})
	}
	if r := b.t.liveRuntime(); r != nil {
		run(r)
		return
	}
	go func() {
		r, err := b.t.ensureRuntime()
		if err != nil {
			answer(false, false, err)
			return
		}
		run(r)
	}()
}

// withAnswer runs f on the live runtime (starting one if need be) with an
// answer that reaches done once: f's, or false if the runtime shuts down
// before f answers (its queued tasks never run) or can't start.
func (b *Bridge) withAnswer(done func(bool), f func(r *runtime, answer func(bool))) {
	run := func(r *runtime) {
		var once sync.Once
		answered := make(chan struct{})
		answer := func(v bool) {
			once.Do(func() {
				close(answered)
				done(v)
			})
		}
		go func() {
			select {
			case <-r.rt.Done():
				answer(false)
			case <-answered:
			}
		}()
		f(r, answer)
	}
	if r := b.t.liveRuntime(); r != nil {
		run(r)
		return
	}
	go func() {
		r, err := b.t.ensureRuntime()
		if err != nil {
			done(false)
			return
		}
		run(r)
	}()
}

// Call calls a JS function prop; done (optional) gets its JSON result.
func (b *Bridge) Call(fn int64, args []byte, done func(result []byte, err error)) {
	if done == nil && !IsTautFn(fn) {
		b.with(func(r *runtime) { r.rt.CallFn(int(fn), args) })
		return
	}
	go func() {
		r, err := b.t.ensureRuntime()
		if err != nil {
			if done != nil {
				done(nil, err)
			}
			return
		}
		var a json.RawMessage = args
		if len(a) == 0 {
			a = json.RawMessage("[]")
		}
		path := "__taut.callHostFn"
		if IsTautFn(fn) {
			path = "__taut.callFn"
		}
		res, err := r.rt.Call(context.Background(), path, float64(fn), a)
		if done != nil {
			done(res, err)
		}
	}()
}

// WantsEvent reports whether any JS listens for window/document-level
// events of type typ (Oneko's mousemove, StreamerMode's keydown), so slackc
// forwards global input at target 0. False while no runtime runs; never
// starts one.
func (b *Bridge) WantsEvent(typ string) bool {
	b.t.mu.Lock()
	r := b.t.rt
	b.t.mu.Unlock()
	return r != nil && r.rt.WantsEvent(typ)
}

// SetRects reports laid-out host node boxes.
func (b *Bridge) SetRects(rects map[int64][4]float32) {
	m := make(map[int]jsrt.Rect, len(rects))
	for k, v := range rects {
		m[int(k)] = jsrt.Rect{X: v[0], Y: v[1], W: v[2], H: v[3]}
	}
	b.with(func(r *runtime) { r.rt.SetRects(m) })
}

// SetViewport reports the window size, scale and color scheme.
func (b *Bridge) SetViewport(w, h, dpr float32, dark bool) {
	b.with(func(r *runtime) { r.rt.SetViewport(w, h, dpr, dark) })
}

// RenderPreferencesTab mounts a plugin's Preferences tab into root.
func (b *Bridge) RenderPreferencesTab(root uint64, tabID string) {
	b.mount(root, "preferencesTabElement", tabID)
}

// RenderEditor mounts option key's custom editor into root; onChange gets
// each value the editor saves (after Taut's checks, see SetOption).
func (b *Bridge) RenderEditor(root uint64, pluginID, key string, value json.RawMessage, onChange func(json.RawMessage)) *GoHandle {
	h := b.t.Func(func(args []json.RawMessage) (any, error) {
		if len(args) > 0 && onChange != nil {
			onChange(args[0])
		}
		return nil, nil
	})
	if len(value) == 0 {
		value = json.RawMessage("null")
	}
	b.mount(root, "editorElement", pluginID, key, []byte(value), float64(h.ID))
	return h
}

// RenderDataPanel mounts a running plugin's dataPanel into root.
func (b *Bridge) RenderDataPanel(root uint64, pluginID string) {
	b.mount(root, "dataPanelElement", pluginID)
}

// Mount renders into root the element globalThis.__taut[fn](args…)
// returns. Extensions (Extension.Prelude) add their element factories to
// __taut and mount them with this, as RenderDataPanel does Taut's: Slick's
// plugin panels (api.settings.panel). args are JSON values; []byte is
// parsed as JSON.
func (b *Bridge) Mount(root uint64, fn string, args ...any) {
	b.mount(root, fn, args...)
}

// ---- composer and message hooks (UI owners call these) ----

func (t *Taut) composer() *composerHooks { return t.ui.hooks.Load() }

// ComposerButtons reports whether plugins added composer buttons; when
// true, ui-composer renders slackc "TautComposerButtons" with props
// {placement: "native"|"end", context: ComposerContext}.
func (t *Taut) ComposerButtons() bool {
	h := t.composer()
	return h != nil && len(h.Buttons) > 0
}

// ComposerContext is Taut's ComposerContext: kind channel | thread | edit
// | draft | other, with Broadcast for a thread composer's checkbox.
type ComposerContext struct {
	Kind      string             `json:"kind"`
	ChannelID string             `json:"channelId,omitempty"`
	ThreadTs  string             `json:"threadTs,omitempty"`
	Broadcast *ComposerBroadcast `json:"broadcast,omitempty"`
	// Blocks and Text are the message being sent (for send checks that
	// read it, like Slick's addSendCheck(blocks, context)).
	Blocks json.RawMessage `json:"blocks,omitempty"`
	Text   string          `json:"text,omitempty"`
}

// ComposerBroadcast is the "Also send to #channel" checkbox; Set is a Go
// func handle (see Taut.Func) JS calls with the new state.
type ComposerBroadcast struct {
	Active      bool      `json:"active"`
	ChannelType string    `json:"channelType"`
	Set         *GoHandle `json:"set"`
}

// HasSendChecks reports whether SendCheck must run before a send.
func (t *Taut) HasSendChecks() bool { h := t.composer(); return h != nil && h.SendChecks > 0 }

// SendCheck runs every plugin send check (which may ask the user, e.g. a
// confirm modal); false keeps the message in the composer. Call it off the
// UI goroutine before sending, when HasSendChecks.
func (t *Taut) SendCheck(ctx context.Context, c ComposerContext) bool {
	if !t.HasSendChecks() {
		return true
	}
	rt, err := t.ensureRuntime()
	if err != nil {
		return true
	}
	type result struct {
		ok  bool
		err error
	}
	done := make(chan result, 1)
	cctx, cancel := context.WithCancel(ctx)
	defer cancel()
	go func() {
		var ok bool
		err := rt.call(cctx, "sendCheck", &ok, c)
		done <- result{ok, err}
	}()
	// A check may wait for the user as long as they take (HaikuWarning's
	// dialog). But when the runtime's watchdog interrupts a task, QuickJS
	// leaves the promises that task would have settled pending forever, so
	// a check it hit (or a promise checks share, like HaikuWarning's
	// dictionary) never answers. Once that has happened, a check that stays
	// pending with nothing open for the user fails like a check that threw:
	// it is ignored.
	tick := time.NewTicker(time.Second)
	defer tick.Stop()
	var idle time.Duration
	for {
		select {
		case r := <-done:
			if r.err != nil {
				t.logf("taut: send check: %v", r.err)
				return true
			}
			return r.ok
		case <-tick.C:
			if rt.rt.Interrupts() == 0 || t.sendCheckWaiting(ctx, rt) {
				idle = 0
				continue
			}
			idle += time.Second
			if idle >= sendCheckGrace {
				t.logf("taut: send check: pending %v with nothing open for the user after the JS runtime interrupted a task (an interrupted promise never settles); ignoring it", idle)
				return true
			}
		}
	}
}

// sendCheckGrace is how long a send check may stay pending with nothing
// open for the user once the runtime has interrupted a task.
var sendCheckGrace = 10 * time.Second

// sendCheckWaiting reports whether something a pending send check may be
// waiting on is open: a plugin overlay (api.modal's confirm, alert,
// openModal) or a dialog in the document (an open <dialog>, role=dialog).
// A runtime too busy to answer counts as waiting.
func (t *Taut) sendCheckWaiting(ctx context.Context, rt *runtime) bool {
	if len(t.Overlays()) > 0 {
		return true
	}
	pctx, cancel := context.WithTimeout(ctx, 2*time.Second)
	defer cancel()
	var open bool
	if err := rt.call(pctx, "sendCheckWaiting", &open); err != nil {
		return true
	}
	return open
}

// HasSendTransforms reports whether TransformSendBlocks changes anything.
func (t *Taut) HasSendTransforms() bool { h := t.composer(); return h != nil && h.SendTransforms > 0 }

// TransformSendBlocks applies onMessageSendBlocks transforms to the Block
// Kit of a message being sent, edited or scheduled (idempotent). core's
// Send/Edit/ScheduleMessage callers pass their blocks through it.
func (t *Taut) TransformSendBlocks(ctx context.Context, blocks json.RawMessage) json.RawMessage {
	if !t.HasSendTransforms() || len(blocks) == 0 {
		return blocks
	}
	rt, err := t.ensureRuntime()
	if err != nil {
		return blocks
	}
	out, err := rt.rt.Call(ctx, "__taut.transformSendBlocks", blocks)
	if err != nil || len(out) == 0 || string(out) == "null" {
		return blocks
	}
	return out
}

// HasLinkTransforms reports whether pasted / dialog links go through TransformLink.
func (t *Taut) HasLinkTransforms() bool { h := t.composer(); return h != nil && h.LinkTransforms > 0 }

// TransformLink rewrites a link coming into the composer (source "paste"
// or "link-dialog"); ui-composer applies the paste with the result.
func (t *Taut) TransformLink(ctx context.Context, url, source string) string {
	if !t.HasLinkTransforms() {
		return url
	}
	rt, err := t.ensureRuntime()
	if err != nil {
		return url
	}
	var out string
	if err := rt.call(ctx, "transformLink", &out, url, map[string]string{"source": source}); err != nil || out == "" {
		return url
	}
	return out
}

// HasInlineMarkup reports whether a plugin replaced the composer's inline
// formatting shortcuts (RealMarkdown).
func (t *Taut) HasInlineMarkup() bool { h := t.composer(); return h != nil && h.InlineMarkup }

// MarkupSpan is one piece of inline markup. Offsets are runes into the
// parsed line (JS counts UTF-16 units; ParseInlineMarkup converts).
type MarkupSpan struct {
	Start        int             `json:"start"`
	End          int             `json:"end"`
	ContentStart int             `json:"contentStart"`
	ContentEnd   int             `json:"contentEnd"`
	Format       json.RawMessage `json:"format"` // "bold"|"italic"|"strike"|"code"|{"link":url}|null
}

// ParseInlineMarkup runs the plugin's parse on one line of composer text.
func (t *Taut) ParseInlineMarkup(ctx context.Context, line string) []MarkupSpan {
	if !t.HasInlineMarkup() {
		return nil
	}
	rt, err := t.ensureRuntime()
	if err != nil {
		return nil
	}
	var spans []MarkupSpan
	_ = rt.call(ctx, "inlineMarkup", &spans, "parse", line)
	toRune := utf16ToRune(line)
	for i := range spans {
		sp := &spans[i]
		sp.Start, sp.End, sp.ContentStart, sp.ContentEnd = toRune(sp.Start), toRune(sp.End), toRune(sp.ContentStart), toRune(sp.ContentEnd)
	}
	return spans
}

// utf16ToRune maps UTF-16 offsets into s (what JS strings count) to rune offsets.
func utf16ToRune(s string) func(int) int {
	idx := []int{0} // idx[utf16 offset] = rune offset
	r := 0
	for _, c := range s {
		r++
		idx = append(idx, r)
		if c >= 0x10000 {
			idx = append(idx, r) // the low surrogate lands on the same rune boundary
		}
	}
	return func(u int) int {
		if u < 0 {
			return 0
		}
		if u >= len(idx) {
			return r
		}
		return idx[u]
	}
}

// SerializeInlineMarkup gives the markers for a format ("bold", …).
func (t *Taut) SerializeInlineMarkup(ctx context.Context, format any) (open, close string) {
	rt, err := t.ensureRuntime()
	if err != nil || !t.HasInlineMarkup() {
		return "", ""
	}
	var pair []string
	_ = rt.call(ctx, "inlineMarkup", &pair, "serialize", format)
	if len(pair) == 2 {
		return pair[0], pair[1]
	}
	return "", ""
}

// EscapeInlineMarkup makes literal text read back as itself.
func (t *Taut) EscapeInlineMarkup(ctx context.Context, text string) string {
	rt, err := t.ensureRuntime()
	if err != nil || !t.HasInlineMarkup() {
		return text
	}
	var out string
	if rt.call(ctx, "inlineMarkup", &out, "escape", text) != nil {
		return text
	}
	return out
}

// ActionablePatched reports whether ActionableMessage may differ from the stored message.
func (t *Taut) ActionablePatched() bool { return t.ui.actionable.Load() }

// ActionableMessage is the message Slack's edit/delete checks, the up-arrow
// and the editor see (patchActionableMessage), e.g. a relayed message
// counted as yours. ok is false when unchanged or unknown.
func (t *Taut) ActionableMessage(ctx context.Context, ch, ts string) (slack.Message, bool) {
	if !t.ActionablePatched() {
		return slack.Message{}, false
	}
	rt, err := t.ensureRuntime()
	if err != nil {
		return slack.Message{}, false
	}
	raw, err := rt.rt.Call(ctx, "__taut.actionable", ch, ts)
	if err != nil || len(raw) == 0 || string(raw) == "null" {
		return slack.Message{}, false
	}
	var m slack.Message
	if json.Unmarshal(raw, &m) != nil {
		return slack.Message{}, false
	}
	return m, true
}

// ---- patched views for native rendering ----

// SlicePatched reports whether plugins change how slice reads (cheap).
func (t *Taut) SlicePatched(slice string) bool {
	if t == nil {
		return false
	}
	s := t.ui.patchedSlices.Load()
	if s == nil {
		return false
	}
	if s.All {
		return true
	}
	for _, x := range s.Slices {
		if x == slice {
			return true
		}
	}
	return false
}

// MessageView is message ch/ts as plugins present it (patchMessageText,
// patchSlice('messages'), injectMessages). Never blocks: until the patched
// value has been computed (a moment, then the UI is invalidated) it
// returns ok=false and the UI draws m as is. The fetch worker decodes the
// value off the UI goroutine, and its conversion to a core.Message is
// made once and kept until the value, the message in core, or what the
// conversion reads (names, prefs, emoji) changes: it used to be decoded
// and converted (highlighting and all) on every call, every frame on the
// Activity page.
func (t *Taut) MessageView(ch, ts string) (core.Message, bool) {
	m, _, ok := t.MessageViewConv(ch, ts, false)
	return m, ok
}

// MessageViewChanged is MessageView for a caller whose own copy of the
// message just changed (a new revision from core's events): the view is
// converted again now, so it never shows the message's previous state
// while the mirror is still on the event that changed it.
func (t *Taut) MessageViewChanged(ch, ts string) (core.Message, bool) {
	m, _, ok := t.MessageViewConv(ch, ts, true)
	return m, ok
}

// MessageViewConv is MessageView (fresh: MessageViewChanged) with the
// serial of the conversion it returns, which changes whenever the view is
// converted again: when its value changed, or what the conversion read (a
// member's name, a channel's tabs: bumpChange) did, though nothing else a
// caller compares may have.
func (t *Taut) MessageViewConv(ch, ts string, fresh bool) (core.Message, uint64, bool) {
	if !t.SlicePatched("messages") {
		return core.Message{}, 0, false
	}
	e := t.ui.views.entry("messages", ch, ts)
	if e == nil || e.sm == nil {
		return core.Message{}, 0, false
	}
	c := t.ui.views.conv(&e.conv, ch, e.sm, fresh)
	return c.msg, c.id, true
}

// InjectedMessages lists messages plugins added to ch (injectMessages)
// that core doesn't have, as UI messages. Non-blocking like MessageView,
// and cheap: the fetch worker decodes the channel's bucket once per value
// and keeps (converted) only the messages core didn't have when it came;
// here they're checked against core again. (Every call used to decode
// the whole bucket, 124 KB in a long channel, and look up each of its
// messages in core, copying the timeline once per message.)
func (t *Taut) InjectedMessages(ch string) []core.Message {
	if !t.SlicePatched("messages") {
		return nil
	}
	e := t.ui.views.entry("messages", ch, "")
	if e == nil || len(e.inj) == 0 {
		return nil
	}
	// The bucket is the mirror of core's messages as plugins left them,
	// and it may be a frame or two stale: a pending send's local copy
	// (swapped for the confirmed message since) or a message core already
	// has under another ts must not come back as "injected" (the message
	// showed twice after every send with Message Logger on).
	var out []core.Message
	for i := range e.inj {
		in := &e.inj[i]
		if t.app.S.Holds(ch, in.ts, in.cid) {
			continue
		}
		if _, local := t.app.LocalTS(in.ts); local {
			continue
		}
		out = append(out, t.ui.views.convert(&in.conv, ch, in.sm, false))
	}
	return out // the worker sorted them by ts
}

// MemberView is member id as plugins present it (Nicknames,
// StreamerMode…): the Slack-shaped member JSON. Non-blocking; decoded
// once per value by the fetch worker.
func (t *Taut) MemberView(id string) (MemberFields, bool) {
	if !t.SlicePatched("members") {
		return MemberFields{}, false
	}
	e := t.ui.views.entry("members", id, "")
	if e == nil || e.mem == nil {
		return MemberFields{}, false
	}
	return *e.mem, true
}

func decodeMember(raw json.RawMessage) *MemberFields {
	if len(raw) == 0 || string(raw) == "null" {
		return nil
	}
	var u slack.User
	if json.Unmarshal(raw, &u) != nil {
		return nil
	}
	return &MemberFields{Name: firstNonEmpty(u.Profile.DisplayName, u.RealName, u.Name), RealName: firstNonEmpty(u.RealName, u.Profile.RealName),
		DisplayName: u.Profile.DisplayName, Avatar: firstNonEmpty(u.Profile.Image72, u.Profile.Image48), StatusText: u.Profile.StatusText,
		StatusEmoji: u.Profile.StatusEmoji, Raw: raw}
}

// MemberFields are the parts of a patched member the UI draws.
type MemberFields struct {
	Name, RealName, DisplayName, Avatar, StatusText, StatusEmoji string
	Raw                                                          json.RawMessage
}

// ChannelView is channel id as plugins present it (PrivateChannel…).
// Non-blocking; decoded once per value by the fetch worker.
func (t *Taut) ChannelView(id string) (ChannelFields, bool) {
	if !t.SlicePatched("channels") {
		return ChannelFields{}, false
	}
	e := t.ui.views.entry("channels", id, "")
	if e == nil || e.ch == nil {
		return ChannelFields{}, false
	}
	return *e.ch, true
}

func decodeChannel(raw json.RawMessage) *ChannelFields {
	if len(raw) == 0 || string(raw) == "null" {
		return nil
	}
	var c struct {
		Name       string `json:"name"`
		IsPrivate  bool   `json:"is_private"`
		IsArchived bool   `json:"is_archived"`
		IsMember   bool   `json:"is_member"`
	}
	if json.Unmarshal(raw, &c) != nil {
		return nil
	}
	return &ChannelFields{Name: c.Name, IsPrivate: c.IsPrivate, IsArchived: c.IsArchived, IsMember: c.IsMember, Raw: raw}
}

// ChannelFields are the parts of a patched channel the UI draws.
type ChannelFields struct {
	Name                            string
	IsPrivate, IsArchived, IsMember bool
	Raw                             json.RawMessage
}

// ChannelRestriction is whether the viewer may post in ch at the top level
// and in threads, as plugins present it (readOnlyChannels /
// threadOnlyChannels, e.g. RestrictedChannelWarning). Without patches it
// comes straight from the mirror's own rules.
func (t *Taut) ChannelRestriction(ch string) (readOnly, threadOnly bool) {
	if !t.SlicePatched("readOnlyChannels") && !t.SlicePatched("threadOnlyChannels") {
		c, ok := t.app.S.Conv(ch)
		if !ok {
			return false, false
		}
		post, thread := t.mirror.restrictions(c)
		return post && thread, post && !thread
	}
	// both values decoded in turn into one struct, the second's fields
	// winning where it has them
	for _, s := range []string{"readOnlyChannels", "threadOnlyChannels"} {
		if e := t.ui.views.entry(s, ch, ""); e != nil && e.restr != nil {
			if e.restr.IsReadOnly != nil {
				readOnly = *e.restr.IsReadOnly
			}
			if e.restr.IsThreadOnly != nil {
				threadOnly = *e.restr.IsThreadOnly
			}
		}
	}
	return readOnly, threadOnly
}

type restrFields struct {
	IsReadOnly   *bool `json:"isReadOnly"`
	IsThreadOnly *bool `json:"isThreadOnly"`
}

// ViewIn reads slice[key] the way a plugin's redux.StateView presents it
// to the components inside it: native "@" nodes rendered inside one carry
// its id as the prop "$view" (a number). Non-blocking like View.
func (t *Taut) ViewIn(viewID int64, slice, key string) (json.RawMessage, bool) {
	if viewID == 0 {
		return t.View(slice, key)
	}
	return t.ui.views.get(slice, key, "view:"+strconv.FormatInt(viewID, 10))
}

// View is the generic form: getState()[slice][key] (key "" = the slice),
// as JSON. Non-blocking (ok=false until computed).
func (t *Taut) View(slice, key string) (json.RawMessage, bool) {
	if !t.SlicePatched(slice) {
		return nil, false
	}
	return t.ui.views.get(slice, key, "")
}

// viewCache keeps patched values for the UI (MessageView, View…). A miss
// or a stale entry queues its key for the one fetch worker; the UI gets
// the last value (or none) now and an Invalidate when the new one lands.
//
// The UI goroutine holds mu for map lookups only. The worker calls JS and
// decodes with mu released, and takes it again just to swap results in.
// It used to decode a channel's whole bucket under mu once for every
// message asked of it, while each frame's lookups waited on mu, and each
// frame that missed while JS was busy started another fetch of the same
// keys (Karim's 2.5 s freezes on the Activity page, 2026-11-15).
//
// Staleness is by sequence number: marking a slice or key stale is O(1)
// (it walked every entry, twice per typing event), and a value asked of JS
// before an invalidation that lands after it stays stale.
type viewKey struct{ slice, key, sub string }

type viewSK struct{ slice, key string }

type viewEntry struct {
	// raw is the value as JS sent it, except for a message of a bucket
	// (slice messages, sub ts): there it's decoded into sm and let go,
	// and sum and rawLen tell a refetch of the same bytes
	raw    json.RawMessage
	sum    uint64
	rawLen int
	size   int    // bytes it holds, about (the cache's budget)
	ver    uint64 // seq when the worker asked JS for it (mu)

	// decoded by the worker, read-only once installed
	sm    *slack.Message // a message (slice messages, sub ts)
	inj   []viewInj      // a channel's bucket (messages, key ch, sub ""): what core didn't have
	mem   *MemberFields  // a member (members)
	ch    *ChannelFields // a channel (channels)
	restr *restrFields   // readOnlyChannels / threadOnlyChannels

	conv atomic.Pointer[viewConv] // sm converted (MessageView)
}

// viewInj is a message of a bucket that core didn't have.
type viewInj struct {
	ts, cid string
	sm      *slack.Message
	conv    atomic.Pointer[viewConv]
}

// viewConv is a message converted for the UI, at conversion generation
// gen; id is the conversion's serial (convSerial).
type viewConv struct {
	gen, id uint64
	msg     core.Message
}

var convSerial atomic.Uint64

// viewGroup is the cached values under one (slice, key): a message bucket's
// messages share theirs.
type viewGroup struct {
	ver  uint64 // seq of its last invalidation
	subs map[string]*viewEntry
}

type viewCache struct {
	t *Taut

	mu       sync.Mutex
	groups   map[viewSK]*viewGroup
	sliceVer map[string]uint64 // seq of each slice's last whole invalidation
	sliceN   map[string]int    // groups per slice
	allVer   uint64            // seq of the last staleAll
	seq      uint64
	n        int // entries
	want     map[viewKey]bool
	flying   map[viewKey]bool // asked of JS, not back yet
	busy     bool             // the worker runs or is about to
	fetches  atomic.Int64     // worker batches (tests)
	maxSubs  int              // entries before everything is dropped (20000)
	bytes    int              // what the entries hold, about
	maxBytes int              // bytes before everything is dropped (64 MiB)
	failN    int              // fetches that failed in a row (retry backoff)

	// conversion generations (MessageView): the mirror bumps a message's
	// on core's events about it or about a member or conversation its
	// conversion read (refBy), and the global one on events about prefs,
	// emoji and the rest core.App.message reads
	gmu    sync.Mutex
	gseq   uint64
	global uint64
	msgGen map[viewSK]uint64 // channel, ts → gseq
	// what each conversion read (core.Message.Refs): member,
	// conversation or file id → the messages whose conversion read it,
	// and back (gmu), so a change to one converts just those again
	// (bumpChange). refSeq is each id's last change (gseq): a conversion
	// that was under way as it changed converts again (indexRefs).
	refBy  map[string]map[viewSK]struct{}
	refOf  map[viewSK]core.MsgRefs
	refSeq map[string]uint64
}

// maxConvKeys bounds the per-message generations and the conversions'
// index: past it they start over with a global bump.
const maxConvKeys = 50000

func (v *viewCache) init(t *Taut) {
	v.t = t
	v.groups = map[viewSK]*viewGroup{}
	v.sliceVer = map[string]uint64{}
	v.sliceN = map[string]int{}
	v.want = map[viewKey]bool{}
	v.flying = map[viewKey]bool{}
	v.msgGen = map[viewSK]uint64{}
	v.refBy = map[string]map[viewSK]struct{}{}
	v.refOf = map[viewSK]core.MsgRefs{}
	v.refSeq = map[string]uint64{}
	v.maxSubs = 20000
	v.maxBytes = 64 << 20
}

// viewRetry is how soon a batch JS didn't answer is asked again (doubling
// with each failure in a row, up to viewRetryMax).
const (
	viewRetry    = 250 * time.Millisecond
	viewRetryMax = 5 * time.Second
)

// kickLocked starts the worker if it isn't running (mu held).
func (v *viewCache) kickLocked() {
	if !v.busy {
		v.busy = true
		time.AfterFunc(2*time.Millisecond, v.work) // a frame's misses in one batch
	}
}

// groupLocked returns (creating) the group of (slice, key). Groups exist
// from the first request on, so an invalidation while it's in flight
// marks what comes back stale.
func (v *viewCache) groupLocked(sk viewSK) *viewGroup {
	g := v.groups[sk]
	if g == nil {
		g = &viewGroup{subs: map[string]*viewEntry{}}
		v.groups[sk] = g
		v.sliceN[sk.slice]++
	}
	return g
}

func (v *viewCache) staleLocked(e *viewEntry, g *viewGroup, slice string) bool {
	return e.ver < g.ver || e.ver < v.sliceVer[slice] || e.ver < v.allVer
}

// entry returns slice[key][sub]'s cached value (nil until the first one
// arrives), queueing a fetch when it's missing or stale. Never waits on JS.
func (v *viewCache) entry(slice, key, sub string) *viewEntry {
	k := viewKey{slice, key, sub}
	v.mu.Lock()
	g := v.groupLocked(viewSK{slice, key})
	e := g.subs[sub]
	// a key in flight isn't asked twice: if it was invalidated meanwhile,
	// the worker asks again when it lands (work)
	if (e == nil || v.staleLocked(e, g, slice)) && !v.want[k] && !v.flying[k] {
		v.want[k] = true
		v.kickLocked()
	}
	v.mu.Unlock()
	return e
}

func (v *viewCache) get(slice, key, sub string) (json.RawMessage, bool) {
	e := v.entry(slice, key, sub)
	if e == nil {
		return nil, false
	}
	return e.raw, true
}

func (v *viewCache) staleAll() {
	v.mu.Lock()
	v.seq++
	v.allVer = v.seq
	v.mu.Unlock()
	if f := v.t.opts.UI.Invalidate; f != nil {
		f()
	}
}

// stale marks the cached views of slice (key "" = all) stale, reporting
// whether it held any.
func (v *viewCache) stale(slice, key string) bool {
	v.mu.Lock()
	defer v.mu.Unlock()
	v.seq++
	if key == "" {
		v.sliceVer[slice] = v.seq
		return v.sliceN[slice] > 0
	}
	g := v.groups[viewSK{slice, key}]
	if g == nil {
		return false
	}
	g.ver = v.seq
	return len(g.subs) > 0
}

// work is the fetch worker: one batch at a time until nothing is wanted,
// so a busy runtime gets each key asked once, not once per frame.
func (v *viewCache) work() {
	for {
		v.mu.Lock()
		if len(v.want) == 0 {
			v.busy = false
			v.mu.Unlock()
			return
		}
		keys := make([]viewKey, 0, len(v.want))
		for k := range v.want {
			keys = append(keys, k)
			v.flying[k] = true
		}
		clear(v.want)
		ver := v.seq
		v.mu.Unlock()

		got, failed := v.fetch(keys) // JS and decoding, mu released
		v.fetches.Add(1)

		v.mu.Lock()
		if v.n > v.maxSubs || len(v.groups) > v.maxSubs || v.bytes > v.maxBytes {
			// bounded: drop everything (what's on screen comes back), and
			// count what's in flight as stale
			clear(v.groups)
			clear(v.sliceN)
			v.n, v.bytes = 0, 0
			v.seq++
			v.allVer = v.seq
			v.gmu.Lock() // no conversion is left: nothing they read is anyone's
			clear(v.refOf)
			clear(v.refBy)
			v.gmu.Unlock()
		}
		changed := false
		var retry []viewKey
		for _, k := range keys {
			delete(v.flying, k)
			e := got[k]
			if e == nil {
				if failed {
					retry = append(retry, k)
				}
				continue
			}
			g := v.groupLocked(viewSK{k.slice, k.key})
			cur := g.subs[k.sub]
			if cur != nil && cur.inj == nil && e.inj == nil && cur.sameValue(e) {
				// the same value: keep what was decoded and converted (not
				// a bucket's injected list, which also depends on what core
				// had when it came: a message core deleted since that a
				// logger keeps is injected now)
				cur.ver = ver
			} else {
				if cur == nil {
					v.n++
				} else {
					v.bytes -= cur.size
				}
				v.bytes += e.size
				e.ver = ver
				g.subs[k.sub] = e
				cur = e
				changed = true
			}
			// Invalidated while JS answered (the mirror marks a change again
			// once it has posted it to JS, behind this call): the value may
			// be from before the change, and a frame that asked meanwhile
			// was turned away (flying). Ask again: that call comes after
			// the change. Without it an idle window kept the plugins'
			// pre-edit value for good.
			if v.staleLocked(cur, g, k.slice) {
				v.want[k] = true
			}
		}
		delay := time.Duration(0)
		if len(retry) > 0 {
			delay = min(viewRetry<<min(v.failN, 5), viewRetryMax)
			v.failN++
		} else if !failed {
			v.failN = 0
		}
		v.mu.Unlock()
		if len(retry) > 0 {
			// JS didn't answer (busy past the timeout, or the call failed):
			// ask again in a moment, not on the next frame, which may never
			// come to an idle window
			time.AfterFunc(delay, func() {
				v.mu.Lock()
				for _, k := range retry {
					if !v.flying[k] {
						v.want[k] = true
					}
				}
				v.kickLocked()
				v.mu.Unlock()
			})
		}
		if f := v.t.opts.UI.Invalidate; changed && f != nil {
			f()
		}
	}
}

// sameValue reports whether e holds the bytes x does.
func (x *viewEntry) sameValue(e *viewEntry) bool {
	if x.raw != nil || e.raw != nil {
		return bytes.Equal(x.raw, e.raw) && x.rawLen == e.rawLen
	}
	return x.sum == e.sum && x.rawLen == e.rawLen
}

// drop forgets slice's cached values under keys (all of slice with none).
func (v *viewCache) drop(slice string, keys ...string) {
	v.mu.Lock()
	defer v.mu.Unlock()
	if len(keys) == 0 {
		for sk, g := range v.groups {
			if sk.slice == slice {
				v.dropGroupLocked(sk, g)
			}
		}
		return
	}
	// a lookup per key: the members pass lets thousands go at once, and
	// scanning every group for each of them held mu for tens of ms
	for _, k := range keys {
		sk := viewSK{slice, k}
		if g := v.groups[sk]; g != nil {
			v.dropGroupLocked(sk, g)
		}
	}
}

func (v *viewCache) dropGroupLocked(sk viewSK, g *viewGroup) {
	for _, e := range g.subs {
		v.n--
		v.bytes -= e.size
	}
	delete(v.groups, sk)
	v.sliceN[sk.slice]--
	if sk.slice == "messages" { // its conversions went: what they read is no one's
		var keys []viewSK
		for ts, e := range g.subs {
			if ts != "" {
				keys = append(keys, viewSK{sk.key, ts})
			}
			for i := range e.inj {
				keys = append(keys, viewSK{sk.key, e.inj[i].ts})
			}
		}
		v.unindex(keys) // gmu inside mu, as everywhere
	}
}

// dropEvicted forgets the views of ch's messages core's cache pass let go
// (its "evicted" event): those core no longer holds. Not those older than
// the event's oldest kept: the pass keeps a thread on screen whole while
// it trims its channel, and dropping that thread's root and replies made
// them flash back to their unpatched text until JS answered again.
func (v *viewCache) dropEvicted(ch string) {
	sk := viewSK{"messages", ch}
	v.mu.Lock()
	var subs []string
	if g := v.groups[sk]; g != nil {
		for ts := range g.subs {
			if ts != "" {
				subs = append(subs, ts)
			}
		}
	}
	v.mu.Unlock()
	// asked of core with mu released: the UI goroutine's lookups wait on it
	var gone []string
	for _, ts := range subs {
		if !v.t.app.S.Holds(ch, ts, "") {
			gone = append(gone, ts)
		}
	}
	if len(gone) == 0 {
		return
	}
	v.mu.Lock()
	defer v.mu.Unlock()
	g := v.groups[sk]
	if g == nil {
		return
	}
	injected := map[string]bool{} // what the bucket's injected list still converts
	if b := g.subs[""]; b != nil {
		for i := range b.inj {
			injected[b.inj[i].ts] = true
		}
	}
	var keys []viewSK
	for _, ts := range gone {
		if e := g.subs[ts]; e != nil {
			delete(g.subs, ts)
			v.n--
			v.bytes -= e.size
			if !injected[ts] {
				keys = append(keys, viewSK{ch, ts})
			}
		}
	}
	v.unindex(keys)
}

// fetch asks JS for keys and decodes what comes back: one JS read per
// (slice, key), each message bucket decoded once for all of its messages
// asked for. failed: a call to JS failed (the keys it didn't answer are
// worth asking again); with no runtime nothing is asked or failed.
func (v *viewCache) fetch(keys []viewKey) (out map[viewKey]*viewEntry, failed bool) {
	v.t.mu.Lock()
	rt := v.t.rt
	v.t.mu.Unlock()
	if rt == nil {
		return nil, false
	}
	out = make(map[viewKey]*viewEntry, len(keys))
	// StateView reads go one by one through __taut.viewIn
	var plain []viewKey
	for _, k := range keys {
		if !strings.HasPrefix(k.sub, "view:") {
			plain = append(plain, k)
			continue
		}
		id, _ := strconv.ParseInt(strings.TrimPrefix(k.sub, "view:"), 10, 64)
		var key any = k.key
		if k.key == "" {
			key = nil
		}
		if raw, err := rt.rt.Call(context.Background(), "__taut.viewIn", float64(id), k.slice, key); err == nil {
			out[k] = &viewEntry{raw: raw, rawLen: len(raw), size: len(raw) + 64}
		} else {
			failed = true
		}
	}
	index := map[viewSK]int{}
	var pairs [][2]any
	for _, k := range plain {
		p := viewSK{k.slice, k.key}
		if _, ok := index[p]; ok {
			continue
		}
		index[p] = len(pairs)
		var key any = k.key
		if k.key == "" {
			key = nil
		}
		pairs = append(pairs, [2]any{k.slice, key})
	}
	if len(pairs) == 0 {
		return out, failed
	}
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()
	var vals []json.RawMessage
	if err := rt.call(ctx, "views", &vals, pairs); err != nil || len(vals) != len(pairs) {
		return out, true
	}
	buckets := make(map[int]map[string]json.RawMessage)
	bucket := func(i int) map[string]json.RawMessage {
		b, ok := buckets[i]
		if !ok {
			_ = json.Unmarshal(vals[i], &b)
			buckets[i] = b
		}
		return b
	}
	for _, k := range plain {
		i := index[viewSK{k.slice, k.key}]
		raw := vals[i]
		if k.sub != "" {
			raw = bucket(i)[k.sub] // one message of a bucket
		}
		e := &viewEntry{raw: raw, rawLen: len(raw), size: len(raw) + 64}
		switch k.slice {
		case "messages":
			if k.sub != "" {
				// decoded and converted, the bytes aren't needed again:
				// a hash tells a refetch of the same value
				e.sm = decodeMessage(raw)
				e.raw, e.sum = nil, viewSum(raw)
				e.size = 3*len(raw) + 256 // the decoded message and its conversion
			} else if k.key != "" {
				e.inj = v.injected(k.key, bucket(i))
				e.size += 3 * 1024 * len(e.inj)
			}
		case "members":
			e.mem = decodeMember(raw)
			e.size += 256
		case "channels":
			e.ch = decodeChannel(raw)
			e.size += 128
		case "readOnlyChannels", "threadOnlyChannels":
			var r restrFields
			if json.Unmarshal(raw, &r) == nil {
				e.restr = &r
			}
		}
		out[k] = e
	}
	return out, failed
}

// viewSum is FNV-1a of b.
func viewSum(b []byte) uint64 {
	h := uint64(14695981039346656037)
	for _, c := range b {
		h ^= uint64(c)
		h *= 1099511628211
	}
	return h
}

func decodeMessage(raw json.RawMessage) *slack.Message {
	if len(raw) == 0 || string(raw) == "null" {
		return nil
	}
	var sm slack.Message
	if json.Unmarshal(raw, &sm) != nil {
		return nil
	}
	return &sm
}

// injected is the messages of ch's bucket that core doesn't have, by ts,
// converted (worker goroutine).
func (v *viewCache) injected(ch string, bucket map[string]json.RawMessage) []viewInj {
	if len(bucket) == 0 {
		return nil
	}
	have, cids := v.t.app.S.MessageKeys(ch)
	var out []viewInj
	for ts, raw := range bucket {
		if have[ts] {
			continue
		}
		sm := decodeMessage(raw)
		if sm == nil || sm.ClientMsgID != "" && cids[sm.ClientMsgID] {
			continue
		}
		out = append(out, viewInj{ts: ts, cid: sm.ClientMsgID, sm: sm})
	}
	sort.Slice(out, func(i, j int) bool { return out[i].ts < out[j].ts })
	for i := range out {
		v.convert(&out[i].conv, ch, out[i].sm, false)
	}
	return out
}

// convert is sm (a decoded view, never written) as a UI message: made
// again only when the conversion generation of ch/sm.TS moved since, or
// when fresh.
func (v *viewCache) convert(slot *atomic.Pointer[viewConv], ch string, sm *slack.Message, fresh bool) core.Message {
	return v.conv(slot, ch, sm, fresh).msg
}

// conv is convert's conversion itself.
func (v *viewCache) conv(slot *atomic.Pointer[viewConv], ch string, sm *slack.Message, fresh bool) *viewConv {
	gen, at := v.convGenAt(ch, sm.TS)
	if c := slot.Load(); c != nil && !fresh && c.gen == gen {
		return c
	}
	m := *sm
	c := &viewConv{gen: gen, id: convSerial.Add(1), msg: v.t.app.MessageFromSlack(ch, &m)}
	slot.Store(c)
	if v.indexRefs(viewSK{ch, sm.TS}, c.msg.Refs, at) {
		// something it read changed while it was being made (bumpChange
		// came before it was indexed): the next read makes it again
		if f := v.t.opts.UI.Invalidate; f != nil {
			f()
		}
	}
	return c
}

// convGenAt is convGen, and the sequence then (indexRefs' at).
func (v *viewCache) convGenAt(ch, ts string) (gen, at uint64) {
	v.gmu.Lock()
	defer v.gmu.Unlock()
	return max(v.global, v.msgGen[viewSK{ch, ts}]), v.gseq
}

// indexRefs records what the conversion of k read, in place of what its
// last conversion read. A conversion begun at sequence at that read
// something bumpChange changed since is stale at once: it reports true
// with k's generation moved on.
func (v *viewCache) indexRefs(k viewSK, r core.MsgRefs, at uint64) (stale bool) {
	v.gmu.Lock()
	defer v.gmu.Unlock()
	for _, l := range [][]string{r.Members, r.Channels, r.Files} {
		for _, id := range l {
			if v.refSeq[id] > at {
				stale = true
			}
		}
	}
	if len(r.Members) > 0 && v.refSeq[anyMember] > at {
		stale = true
	}
	if stale {
		v.gseq++
		v.msgGen[k] = v.gseq
	}
	old, had := v.refOf[k]
	n := len(r.Members) + len(r.Channels) + len(r.Files)
	if n == 0 && !had {
		return stale
	}
	v.unindexLocked(k, old)
	if n == 0 {
		return stale
	}
	if !had && len(v.refOf) >= maxConvKeys {
		// start over: every conversion is made (and indexed) again
		clear(v.refOf)
		clear(v.refBy)
		clear(v.refSeq)
		clear(v.msgGen)
		v.gseq++
		v.global = v.gseq
	}
	v.refOf[k] = r
	for _, l := range [][]string{r.Members, r.Channels, r.Files} {
		for _, id := range l {
			set := v.refBy[id]
			if set == nil {
				set = map[viewSK]struct{}{}
				v.refBy[id] = set
			}
			set[k] = struct{}{}
		}
	}
	return stale
}

// unindexLocked forgets that k's conversion read refs (gmu held).
func (v *viewCache) unindexLocked(k viewSK, refs core.MsgRefs) {
	for _, l := range [][]string{refs.Members, refs.Channels, refs.Files} {
		for _, id := range l {
			if set := v.refBy[id]; set != nil {
				delete(set, k)
				if len(set) == 0 {
					delete(v.refBy, id)
				}
			}
		}
	}
	delete(v.refOf, k)
}

// unindex forgets the conversions of keys (their views went).
func (v *viewCache) unindex(keys []viewSK) {
	if len(keys) == 0 {
		return
	}
	v.gmu.Lock()
	for _, k := range keys {
		if refs, ok := v.refOf[k]; ok {
			v.unindexLocked(k, refs)
		}
	}
	v.gmu.Unlock()
}

// anyMember is refSeq's key for a change to every member's names
// (RefChange.AnyMember): a conversion that read a member while it came is
// stale (indexRefs).
const anyMember = "*"

// bumpChange makes the conversions that read what c changed be made
// again, and only those: a profile change converted every message again,
// or none. It redraws only when one of them is still cached (a view's
// conversion that went keeps nothing on screen), and forgets the others.
func (v *viewCache) bumpChange(c core.RefChange) {
	if c.Empty() {
		return
	}
	ids := c.IDs()
	if c.AnyMember {
		ids = append(ids, anyMember)
	}
	v.gmu.Lock()
	v.gseq++
	seq := v.gseq
	if len(v.refSeq)+len(ids) > maxConvKeys {
		clear(v.refSeq)
	}
	for _, id := range ids {
		v.refSeq[id] = seq
	}
	var keys []viewSK
	for _, id := range ids {
		for k := range v.refBy[id] {
			keys = append(keys, k)
		}
	}
	if c.AnyMember {
		for k, refs := range v.refOf {
			if len(refs.Members) > 0 {
				keys = append(keys, k)
			}
		}
	}
	if len(keys) == 0 {
		v.gmu.Unlock()
		return
	}
	if len(v.msgGen)+len(keys) >= maxConvKeys {
		clear(v.msgGen)
		v.global = seq
	} else {
		for _, k := range keys {
			v.msgGen[k] = seq
		}
	}
	v.gmu.Unlock()
	cached, gone := v.cachedKeys(keys)
	v.unindex(gone)
	if f := v.t.opts.UI.Invalidate; cached && f != nil {
		f()
	}
}

// cachedKeys reports whether any of keys has a conversion cached, and
// lists those that have none.
func (v *viewCache) cachedKeys(keys []viewSK) (cached bool, gone []viewSK) {
	v.mu.Lock()
	defer v.mu.Unlock()
	for _, k := range keys {
		if v.convCachedLocked(k.slice, k.key) {
			cached = true
		} else {
			gone = append(gone, k)
		}
	}
	return cached, gone
}

// convGen is the generation of what converting ch/ts reads.
func (v *viewCache) convGen(ch, ts string) uint64 {
	v.gmu.Lock()
	defer v.gmu.Unlock()
	return max(v.global, v.msgGen[viewSK{ch, ts}])
}

// bumpConv makes conversions of ch/ts (with ts "", of every message) be
// made again, and redraws when one of them is cached: the UI hears core's
// event on its own and may have drawn, converting with the old
// generation, before the mirror got here (a mention resolved by "names",
// new highlight words), and nothing else would redraw for it.
func (v *viewCache) bumpConv(ch, ts string) {
	v.gmu.Lock()
	v.gseq++
	global := ch == "" || ts == "" || len(v.msgGen) >= maxConvKeys
	if global {
		clear(v.msgGen)
		v.global = v.gseq
	} else {
		v.msgGen[viewSK{ch, ts}] = v.gseq
	}
	v.gmu.Unlock()
	if global {
		ch, ts = "", ""
	}
	if f := v.t.opts.UI.Invalidate; f != nil && v.convCached(ch, ts) {
		f()
	}
}

// convCached reports whether a conversion of ch/ts is cached (with ch "",
// of any message).
func (v *viewCache) convCached(ch, ts string) bool {
	v.mu.Lock()
	defer v.mu.Unlock()
	return v.convCachedLocked(ch, ts)
}

func (v *viewCache) convCachedLocked(ch, ts string) bool {
	if ch == "" {
		return v.sliceN["messages"] > 0
	}
	g := v.groups[viewSK{"messages", ch}]
	if g == nil {
		return false
	}
	if e := g.subs[ts]; e != nil && e.conv.Load() != nil {
		return true
	}
	if b := g.subs[""]; b != nil {
		for i := range b.inj {
			if b.inj[i].ts == ts {
				return true
			}
		}
	}
	return false
}

// File is a file crossing to JS as a real File (thunk args like
// addAndUploadPendingFile): {"$file": {name, type, data}}.
type File struct {
	Name string
	Type string
	Data []byte
}

func (f File) MarshalJSON() ([]byte, error) {
	return json.Marshal(map[string]any{"$file": map[string]any{"name": f.Name, "type": f.Type, "size": len(f.Data), "data": f.Data}})
}

func (f *File) UnmarshalJSON(b []byte) error {
	var v struct {
		F *struct {
			Name string `json:"name"`
			Type string `json:"type"`
			Data []byte `json:"data"`
		} `json:"$file"`
	}
	if err := json.Unmarshal(b, &v); err != nil || v.F == nil {
		return nil
	}
	f.Name, f.Type, f.Data = v.F.Name, v.F.Type, v.F.Data
	return nil
}

// logArg is the first mount argument when it is a component name; props are
// never logged (they can carry message contents).
func logArg(args []any) string {
	if len(args) > 0 {
		if s, ok := args[0].(string); ok {
			return s
		}
	}
	return ""
}

// errWithStack formats a JS error with the top of its stack (function names
// and positions only), so plugin failures in the log say where they threw.
func errWithStack(err error) string {
	var je *jsrt.JSError
	if !errors.As(err, &je) || je.Stack == "" {
		return err.Error()
	}
	lines := strings.Split(strings.TrimSpace(je.Stack), "\n")
	if len(lines) > 6 {
		lines = lines[:6]
	}
	for i, l := range lines {
		lines[i] = strings.TrimSpace(l)
	}
	return err.Error() + " [" + strings.Join(lines, " | ") + "]"
}
