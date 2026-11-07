package taut

import (
	"context"
	"encoding/json"
	"sort"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/some-du6e/yoloslackgoclient/internal/core"
	"github.com/some-du6e/yoloslackgoclient/internal/jsrt"
	"github.com/some-du6e/yoloslackgoclient/internal/slack"
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

func (b *Bridge) runtime() *runtime {
	rt, err := b.t.ensureRuntime()
	if err != nil {
		return nil
	}
	return rt
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
func (b *Bridge) mount(id uint64, fn string, args ...any) {
	r := b.runtime()
	if r == nil {
		return
	}
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
			b.t.logf("taut: %s: %v", fn, err)
			return
		}
		if err := jr.Render(js, el); err != nil {
			b.t.logf("taut: render %s: %v", fn, err)
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
	if r := b.runtime(); r != nil {
		r.rt.DispatchEvent(int(target), event)
	}
}

// Call calls a JS function prop; done (optional) gets its JSON result.
func (b *Bridge) Call(fn int64, args []byte, done func(result []byte, err error)) {
	r := b.runtime()
	if r == nil {
		if done != nil {
			done(nil, b.t.rtErr)
		}
		return
	}
	if done == nil && !IsTautFn(fn) {
		r.rt.CallFn(int(fn), args)
		return
	}
	go func() {
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

// SetRects reports laid-out host node boxes.
func (b *Bridge) SetRects(rects map[int64][4]float32) {
	r := b.runtime()
	if r == nil {
		return
	}
	m := make(map[int]jsrt.Rect, len(rects))
	for k, v := range rects {
		m[int(k)] = jsrt.Rect{X: v[0], Y: v[1], W: v[2], H: v[3]}
	}
	r.rt.SetRects(m)
}

// SetViewport reports the window size, scale and color scheme.
func (b *Bridge) SetViewport(w, h, dpr float32, dark bool) {
	if r := b.runtime(); r != nil {
		r.rt.SetViewport(w, h, dpr, dark)
	}
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
	var ok bool
	if err := rt.call(ctx, "sendCheck", &ok, c); err != nil {
		t.logf("taut: send check: %v", err)
		return true
	}
	return ok
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

// MarkupSpan is one piece of inline markup, offsets into the parsed text.
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
	return spans
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
// returns ok=false and the UI draws m as is.
func (t *Taut) MessageView(ch, ts string) (core.Message, bool) {
	if !t.SlicePatched("messages") {
		return core.Message{}, false
	}
	raw, ok := t.ui.views.get("messages", ch, ts)
	if !ok || len(raw) == 0 || string(raw) == "null" {
		return core.Message{}, false
	}
	var sm slack.Message
	if json.Unmarshal(raw, &sm) != nil {
		return core.Message{}, false
	}
	return t.app.MessageFromSlack(ch, &sm), true
}

// InjectedMessages lists messages plugins added to ch (injectMessages)
// that core doesn't have, as UI messages. Non-blocking like MessageView.
func (t *Taut) InjectedMessages(ch string) []core.Message {
	if !t.SlicePatched("messages") {
		return nil
	}
	raw, ok := t.ui.views.get("messages", ch, "")
	if !ok {
		return nil
	}
	var bucket map[string]slack.Message
	if json.Unmarshal(raw, &bucket) != nil {
		return nil
	}
	var out []core.Message
	for ts, sm := range bucket {
		if _, have := t.app.S.Find(ch, ts); have {
			continue
		}
		m := sm
		out = append(out, t.app.MessageFromSlack(ch, &m))
	}
	sort.Slice(out, func(i, j int) bool { return out[i].TS < out[j].TS })
	return out
}

// MemberView is member id as plugins present it (Nicknames,
// StreamerMode…): the Slack-shaped member JSON. Non-blocking.
func (t *Taut) MemberView(id string) (MemberFields, bool) {
	if !t.SlicePatched("members") {
		return MemberFields{}, false
	}
	raw, ok := t.ui.views.get("members", id, "")
	if !ok || len(raw) == 0 || string(raw) == "null" {
		return MemberFields{}, false
	}
	var u slack.User
	if json.Unmarshal(raw, &u) != nil {
		return MemberFields{}, false
	}
	return MemberFields{Name: firstNonEmpty(u.Profile.DisplayName, u.RealName, u.Name), RealName: firstNonEmpty(u.RealName, u.Profile.RealName),
		DisplayName: u.Profile.DisplayName, Avatar: firstNonEmpty(u.Profile.Image72, u.Profile.Image48), StatusText: u.Profile.StatusText,
		StatusEmoji: u.Profile.StatusEmoji, Raw: raw}, true
}

// MemberFields are the parts of a patched member the UI draws.
type MemberFields struct {
	Name, RealName, DisplayName, Avatar, StatusText, StatusEmoji string
	Raw                                                          json.RawMessage
}

// ChannelView is channel id as plugins present it (PrivateChannel…).
func (t *Taut) ChannelView(id string) (ChannelFields, bool) {
	if !t.SlicePatched("channels") {
		return ChannelFields{}, false
	}
	raw, ok := t.ui.views.get("channels", id, "")
	if !ok || len(raw) == 0 || string(raw) == "null" {
		return ChannelFields{}, false
	}
	var c struct {
		Name       string `json:"name"`
		IsPrivate  bool   `json:"is_private"`
		IsArchived bool   `json:"is_archived"`
		IsMember   bool   `json:"is_member"`
	}
	if json.Unmarshal(raw, &c) != nil {
		return ChannelFields{}, false
	}
	return ChannelFields{Name: c.Name, IsPrivate: c.IsPrivate, IsArchived: c.IsArchived, IsMember: c.IsMember, Raw: raw}, true
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
	ro, _ := t.ui.views.get("readOnlyChannels", ch, "")
	to, _ := t.ui.views.get("threadOnlyChannels", ch, "")
	var r struct {
		IsReadOnly   bool `json:"isReadOnly"`
		IsThreadOnly bool `json:"isThreadOnly"`
	}
	_ = json.Unmarshal(ro, &r)
	_ = json.Unmarshal(to, &r)
	return r.IsReadOnly, r.IsThreadOnly
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

// viewCache keeps patched values for the UI. A miss (or a stale entry)
// schedules one batched JS call; the UI gets the old value (or none) now
// and an Invalidate when the new one lands.
type viewKey struct{ slice, key, sub string }

type viewEntry struct {
	raw   json.RawMessage
	stale bool
}

type viewCache struct {
	t       *Taut
	mu      sync.Mutex
	entries map[viewKey]*viewEntry
	want    map[viewKey]bool
	timer   *time.Timer
}

func (v *viewCache) init(t *Taut) {
	v.t = t
	v.entries = map[viewKey]*viewEntry{}
	v.want = map[viewKey]bool{}
}

func (v *viewCache) get(slice, key, sub string) (json.RawMessage, bool) {
	k := viewKey{slice, key, sub}
	v.mu.Lock()
	defer v.mu.Unlock()
	e := v.entries[k]
	if e == nil || e.stale {
		v.want[k] = true
		if v.timer == nil {
			v.timer = time.AfterFunc(2*time.Millisecond, v.fetch)
		}
	}
	if e == nil {
		return nil, false
	}
	return e.raw, true
}

func (v *viewCache) staleAll() {
	v.mu.Lock()
	for _, e := range v.entries {
		e.stale = true
	}
	v.mu.Unlock()
	if f := v.t.opts.UI.Invalidate; f != nil {
		f()
	}
}

func (v *viewCache) stale(slice, key string) {
	v.mu.Lock()
	for k, e := range v.entries {
		if k.slice == slice && (key == "" || k.key == key) {
			e.stale = true
		}
	}
	v.mu.Unlock()
}

func (v *viewCache) fetch() {
	v.mu.Lock()
	v.timer = nil
	keys := make([]viewKey, 0, len(v.want))
	for k := range v.want {
		keys = append(keys, k)
	}
	v.want = map[viewKey]bool{}
	if len(v.entries) > 20000 {
		v.entries = map[viewKey]*viewEntry{}
	}
	v.mu.Unlock()
	if len(keys) == 0 {
		return
	}
	v.t.mu.Lock()
	rt := v.t.rt
	v.t.mu.Unlock()
	if rt == nil {
		return
	}
	// StateView reads go one by one through __taut.viewIn
	var plain []viewKey
	var viewed []viewKey
	for _, k := range keys {
		if strings.HasPrefix(k.sub, "view:") {
			viewed = append(viewed, k)
		} else {
			plain = append(plain, k)
		}
	}
	keys = plain
	for _, k := range viewed {
		id, _ := strconv.ParseInt(strings.TrimPrefix(k.sub, "view:"), 10, 64)
		var key any = k.key
		if k.key == "" {
			key = nil
		}
		raw, err := rt.rt.Call(context.Background(), "__taut.viewIn", float64(id), k.slice, key)
		if err == nil {
			v.mu.Lock()
			v.entries[k] = &viewEntry{raw: raw}
			v.mu.Unlock()
		}
	}
	// one JS read per (slice, key); message subs share their bucket
	type sk struct{ slice, key string }
	index := map[sk]int{}
	var pairs [][2]any
	for _, k := range keys {
		p := sk{k.slice, k.key}
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
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()
	var vals []json.RawMessage
	if err := rt.call(ctx, "views", &vals, pairs); err != nil || len(vals) != len(pairs) {
		return
	}
	v.mu.Lock()
	for _, k := range keys {
		raw := vals[index[sk{k.slice, k.key}]]
		if k.sub != "" {
			// one message of a bucket
			var bucket map[string]json.RawMessage
			_ = json.Unmarshal(raw, &bucket)
			raw = bucket[k.sub]
		}
		v.entries[k] = &viewEntry{raw: raw}
	}
	v.mu.Unlock()
	if f := v.t.opts.UI.Invalidate; f != nil {
		f()
	}
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
