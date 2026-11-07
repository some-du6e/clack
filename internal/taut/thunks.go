package taut

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/url"
	"sort"
	"sync"
	"sync/atomic"
)

// Slack's named thunks are where plugins hook actions: SilentTyping wraps
// currentUserStartedTyping, ShutUpSlackbot wraps markLastRead, QuickJoin
// wraps getContextMenuTemplate, and so on. In Clack the native UI goes
// through the same names with Dispatch: unpatched it calls the native
// implementation directly (one atomic load, no JS); patched, the plugins'
// wrappers run in JS around the native implementation, which may get
// changed args, be skipped, or have its result changed, exactly as with
// Slack's thunk creators.
//
// ThunkCatalogue lists every name plugins use, the args Slack passes (and
// Clack must pass), and which UI owner calls it.

// ThunkSpec describes one Slack thunk.
type ThunkSpec struct {
	Name  string
	Args  string // Slack's argument shape
	Owner string // the Clack package that dispatches it
	Note  string
}

// ThunkCatalogue is every thunk Taut's plugins (and the TautAPI) use.
var ThunkCatalogue = []ThunkSpec{
	{"currentUserStartedTyping", "{channelId, threadTs?}", "ui/composer", "sending the typing indicator (core.Typing)"},
	{"markLastRead", "{channelId, ts, threadTs?}", "ui/messages", "marking a conversation read (core.Mark)"},
	{"setUserPrefByApi", "{pref, value}", "ui (prefs)", "saving a user pref (users.prefs.set)"},
	{"setUserPrefFetcher", "{pref, value}", "ui (prefs)", "same, the fetcher form"},
	{"initializeUserPrefs", "{prefsData}", "taut (prefs event)", "prefs loading into the store; the result's prefsData is what plugins leave"},
	{"deleteDraftApi", "{draftId}", "ui/composer, ui/views (drafts)", "deleting a draft (core.DeleteDraft)"},
	{"syncDraftCreate", "{draftId, …draft}", "ui/composer", "saving a new draft (core.SaveDraft)"},
	{"syncDraftUpdates", "{draftId, …draft}", "ui/composer", "updating a draft (core.SaveDraft)"},
	{"clearDrafts", "{draftIds}", "ui/views (drafts)", "deleting drafts"},
	{"prepareAndSendMessage", "{channelId, replyToTs?, text, blocks, broadcast?}", "ui/composer", "sending (core.Send); after TransformSendBlocks"},
	{"sendMessage", "{channel, blocks, thread_ts?, reply_broadcast?, unfurl?}", "ui/composer", "the send itself, inside prepareAndSendMessage"},
	{"saveMessageEdit", "{channelId, ts, blocks, text}", "ui/messages", "saving an edit (core.Edit)"},
	{"commitMessageEdit", "{channelId, ts}", "ui/messages", "closing the editor after a save"},
	{"chatDeleteFetcher", "{channel, ts}", "ui/messages", "deleting a message (core.Delete)"},
	{"changeReactionFromUserAction", "{key: _rxn_key, name, add?}", "ui/messages", "toggling a reaction (core.React)"},
	{"submitAddOrRemoveReaction", "{reactionKey, name, add}", "ui/messages, ui/popups (emoji picker)", "adding/removing a reaction"},
	{"getContextMenuTemplate", "{id: channelId}", "ui/popups (channel menu)", "the channel context menu; returns []MenuItem"},
	{"getMemberContextMenuTemplate", "{id: userId}", "ui/popups (member menu)", "the member context menu; returns []MenuItem"},
	{"showNotification", "{message, channelId, title, …}", "ui (notifications)", "a desktop notification"},
	{"searchInline", "{query}", "ui/popups (search dropdown)", "recent-message suggestions; returns {items}"},
	{"autocompleteChannels", "{query, …}", "ui/composer (# autocomplete)", "returns results; NativeAtChannel and others filter them"},
	{"autocompleteMembers", "{query, channelId, …}", "ui/composer (@ autocomplete)", "returns results (broadcast keywords included)"},
	{"addAndUploadPendingFile", "{file: File, hideBanner?}", "ui/composer (attachments)", "uploading a file (core.PrepareUpload); returns {uploadPromise}"},
	{"uploadFiles", "{files: File[], channelId, …}", "ui/composer (attachments)", "uploading and sharing files (core.Upload)"},
	{"getChannelMembershipInfoForUsers", "{channelId, userIds}", "taut", "who is in a channel; fills membership"},
	{"ensureMembersArePresent", "{memberIds}", "taut", "loading users (core.EnsureUsers)"},
	{"fetchRawChannelsById", "{ids}", "taut", "loading conversations (core.ChannelInfo)"},
	{"joinChannel", "{channelId}", "ui (join button)", "joining (core.Join)"},
	{"openPrefsModal", "{tab?}", "ui (shell)", "opening Preferences"},
	{"enqueueHuddleToast", "{content, svgIconProps, duration}", "ui (toasts)", "a huddle toast"},
	{"maybeFetchBackgrounds", "{huddleId}", "ui/views (huddles)", "huddle themes; Clack has none"},
	{"startEventLog", "{}", "ui/views (huddles)", "huddle event log"},
	{"stopEventLog", "{}", "ui/views (huddles)", "huddle event log"},
	{"stopHuddle", "{}", "ui/views (huddles)", "leaving a huddle"},
	{"displayAlertMessage", "{alert, …}", "ui (alerts)", "an alert banner"},
	{"processAwsTranscription", "{type, …}", "ui/views (huddles)", "huddle captions"},
	{"logExperimentExposure", "{name}", "none", "Clack logs no exposures"},
	{"handleMessageImmediatelyWithoutPreprocessing", "rtm event", "taut (rtm)", "realtime events"},
	{"blocksFormatFetcher", "{blocks, reason}", "ui/composer (markdown mode)", "blocks.format"},
	{"executeAppAction", "{actionId, appId, channelId, …}", "ui/popups (shortcuts)", "running an app shortcut"},
	{"submitView", "{viewId, values, …}", "ui/popups (app modals)", "submitting an app modal"},
	{"apiCall", "{method, args}", "taut", "a Slack API call"},
	{"apiCallExpedited", "{method, args}", "taut", "a Slack API call"},
}

// NativeFunc is a thunk's native implementation: Clack's own code for what
// Slack's thunk does. args is the (possibly plugin-changed) JSON argument
// list Slack passes (usually one object).
type NativeFunc func(ctx context.Context, args []json.RawMessage) (any, error)

type thunkTable struct {
	t       *Taut
	patched atomic.Pointer[map[string]bool]

	mu      sync.Mutex
	natives map[string]NativeFunc // defaults for plugin-initiated dispatches
	calls   map[int64]NativeFunc  // in-flight native cores of a Dispatch
	next    int64
}

func newThunkTable(t *Taut) *thunkTable {
	tt := &thunkTable{t: t, natives: map[string]NativeFunc{}, calls: map[int64]NativeFunc{}}
	tt.installDefaults()
	return tt
}

// Patched reports whether a plugin wraps thunk name (cheap).
func (t *Taut) ThunkPatched(name string) bool {
	if t == nil {
		return false
	}
	p := t.thunks.patched.Load()
	return p != nil && (*p)[name]
}

func (tt *thunkTable) setPatched(names []string) {
	if len(names) == 0 {
		tt.patched.Store(nil)
		return
	}
	m := make(map[string]bool, len(names))
	for _, n := range names {
		m[n] = true
	}
	tt.patched.Store(&m)
}

// RegisterThunk sets (or replaces) the native implementation used when a
// plugin dispatches name itself (dispatchThunk) or a wrapper calls the
// original outside a UI Dispatch. UI owners register the ones they own.
func (t *Taut) RegisterThunk(name string, fn NativeFunc) {
	t.thunks.mu.Lock()
	t.thunks.natives[name] = fn
	t.thunks.mu.Unlock()
}

func (tt *thunkTable) native(name string) NativeFunc {
	tt.mu.Lock()
	defer tt.mu.Unlock()
	return tt.natives[name]
}

func (tt *thunkTable) names() []string {
	seen := map[string]bool{}
	for _, s := range ThunkCatalogue {
		seen[s.Name] = true
	}
	tt.mu.Lock()
	for n := range tt.natives {
		seen[n] = true
	}
	tt.mu.Unlock()
	out := make([]string, 0, len(seen))
	for n := range seen {
		out = append(out, n)
	}
	sort.Strings(out)
	return out
}

// Dispatch runs Slack thunk name for the native UI. With no plugin wrapping
// name it is native(ctx, args) and nothing else. With one, args go to JS,
// each plugin's wrapper runs (outermost last-registered), and native runs
// where the wrappers call the original, with the args they pass.
//
// Call it where the UI would call core for that action, off the UI
// goroutine (it may wait for JS and for native's network call).
func Dispatch[A, R any](ctx context.Context, t *Taut, name string, args A, native func(ctx context.Context, args A) (R, error)) (R, error) {
	if !t.ThunkPatched(name) {
		return native(ctx, args)
	}
	var zero R
	core := func(ctx context.Context, raw []json.RawMessage) (any, error) {
		var a A
		if len(raw) > 0 {
			if err := json.Unmarshal(raw[0], &a); err != nil {
				return nil, fmt.Errorf("taut: %s args: %w", name, err)
			}
		}
		return native(ctx, a)
	}
	out, err := t.dispatchJS(ctx, name, []any{args}, core)
	if err != nil {
		return zero, err
	}
	if len(out) == 0 || string(out) == "null" {
		return zero, nil
	}
	var r R
	if err := json.Unmarshal(out, &r); err != nil {
		return zero, fmt.Errorf("taut: %s result: %w", name, err)
	}
	return r, nil
}

// DispatchJSON is Dispatch with JSON in and out.
func (t *Taut) DispatchJSON(ctx context.Context, name string, args []any, native NativeFunc) (json.RawMessage, error) {
	if !t.ThunkPatched(name) {
		v, err := native(ctx, rawArgs(args))
		if err != nil {
			return nil, err
		}
		return json.Marshal(v)
	}
	return t.dispatchJS(ctx, name, args, native)
}

func rawArgs(args []any) []json.RawMessage {
	out := make([]json.RawMessage, len(args))
	for i, a := range args {
		out[i], _ = json.Marshal(a)
	}
	return out
}

func (t *Taut) dispatchJS(ctx context.Context, name string, args []any, native NativeFunc) (json.RawMessage, error) {
	rt, err := t.ensureRuntime()
	if err != nil {
		return nil, err
	}
	tt := t.thunks
	tt.mu.Lock()
	tt.next++
	id := tt.next
	tt.calls[id] = native
	tt.mu.Unlock()
	defer func() {
		tt.mu.Lock()
		delete(tt.calls, id)
		tt.mu.Unlock()
	}()
	return rt.rt.Call(ctx, "__taut.dispatchThunk", name, args, id)
}

// callNative runs the native core of an in-flight Dispatch (JS → Go).
func (tt *thunkTable) callNative(ctx context.Context, id int64, args []json.RawMessage) (any, error) {
	tt.mu.Lock()
	fn := tt.calls[id]
	tt.mu.Unlock()
	if fn == nil {
		return nil, errors.New("taut: that dispatch already finished")
	}
	return fn(ctx, args)
}

// runThunk is a plugin-initiated dispatch of a native thunk (JS → Go).
func (tt *thunkTable) runThunk(ctx context.Context, name string, args []json.RawMessage) (any, error) {
	fn := tt.native(name)
	if fn == nil {
		return nil, fmt.Errorf("Clack has no native %s", name)
	}
	return fn(ctx, args)
}

// ---- default native implementations, over core ----

type obj map[string]json.RawMessage

func argObj(args []json.RawMessage) obj {
	o := obj{}
	if len(args) > 0 {
		_ = json.Unmarshal(args[0], &o)
	}
	return o
}

func (o obj) str(k string) string {
	var s string
	_ = json.Unmarshal(o[k], &s)
	return s
}

func (o obj) strs(k string) []string {
	var s []string
	_ = json.Unmarshal(o[k], &s)
	return s
}

func (tt *thunkTable) installDefaults() {
	t := tt.t
	app := func() *appRef { return &appRef{t} }
	n := map[string]NativeFunc{
		"currentUserStartedTyping": func(ctx context.Context, a []json.RawMessage) (any, error) {
			o := argObj(a)
			return nil, t.app.Typing(o.str("channelId"), o.str("threadTs"))
		},
		"markLastRead": func(ctx context.Context, a []json.RawMessage) (any, error) {
			o := argObj(a)
			ch, ts := o.str("channelId"), o.str("ts")
			if th := o.str("threadTs"); th != "" {
				return nil, t.app.MarkThread(ctx, ch, th, ts)
			}
			_, err := t.app.Mark(ctx, ch, ts)
			return nil, err
		},
		"setUserPrefByApi":    app().setPref,
		"setUserPrefFetcher":  app().setPref,
		"initializeUserPrefs": func(ctx context.Context, a []json.RawMessage) (any, error) { return argObj(a), nil },
		"deleteDraftApi": func(ctx context.Context, a []json.RawMessage) (any, error) {
			return nil, t.app.DeleteDraft(ctx, argObj(a).str("draftId"))
		},
		"clearDrafts": func(ctx context.Context, a []json.RawMessage) (any, error) {
			for _, id := range argObj(a).strs("draftIds") {
				if err := t.app.DeleteDraft(ctx, id); err != nil {
					return nil, err
				}
			}
			return nil, nil
		},
		"getChannelMembershipInfoForUsers": app().membershipInfo,
		"ensureMembersArePresent": func(ctx context.Context, a []json.RawMessage) (any, error) {
			t.app.EnsureUsers(ctx, argObj(a).strs("memberIds"))
			return nil, nil
		},
		"fetchRawChannelsById": func(ctx context.Context, a []json.RawMessage) (any, error) {
			o := argObj(a)
			ids := o.strs("ids")
			if len(ids) == 0 {
				ids = o.strs("channelIds")
			}
			for _, id := range ids {
				if _, err := t.app.ChannelInfo(ctx, id); err != nil {
					return nil, err
				}
				t.mirror.invalidate("channels", id)
			}
			return nil, nil
		},
		"joinChannel": func(ctx context.Context, a []json.RawMessage) (any, error) {
			_, err := t.app.Join(ctx, argObj(a).str("channelId"))
			return nil, err
		},
		"chatDeleteFetcher": func(ctx context.Context, a []json.RawMessage) (any, error) {
			o := argObj(a)
			return nil, t.app.Delete(ctx, o.str("channel"), o.str("ts"))
		},
		"enqueueHuddleToast": func(ctx context.Context, a []json.RawMessage) (any, error) {
			t.toast(argObj(a))
			return nil, nil
		},
		"apiCall":          app().apiCall,
		"apiCallExpedited": app().apiCall,
		"openPrefsModal": func(ctx context.Context, a []json.RawMessage) (any, error) {
			if f := t.opts.UI.OpenPreferences; f != nil {
				f(argObj(a).str("tab"))
			}
			return nil, nil
		},
	}
	// Slack bookkeeping with nothing to do in Clack: they resolve, so
	// plugins awaiting them continue.
	for _, name := range []string{"maybeFetchBackgrounds", "startEventLog", "stopEventLog", "stopHuddle", "displayAlertMessage",
		"processAwsTranscription", "logExperimentExposure", "handleMessageImmediatelyWithoutPreprocessing", "commitMessageEdit"} {
		n[name] = func(ctx context.Context, a []json.RawMessage) (any, error) { return nil, nil }
	}
	tt.natives = n
}

type appRef struct{ t *Taut }

func (r *appRef) setPref(ctx context.Context, a []json.RawMessage) (any, error) {
	o := argObj(a)
	name := o.str("pref")
	var value any
	_ = json.Unmarshal(o["value"], &value)
	v := ""
	switch x := value.(type) {
	case string:
		v = x
	case bool:
		v = fmt.Sprint(x)
	case float64:
		v = fmt.Sprint(x)
	default:
		b, _ := json.Marshal(x)
		v = string(b)
	}
	_, err := r.t.app.C.Raw(ctx, "users.prefs.set", url.Values{"name": {name}, "value": {v}})
	if err == nil {
		r.t.mirror.invalidate("prefs")
		r.t.mirror.invalidate("userPrefs")
	}
	return nil, err
}

// membershipInfo answers whether userIds are in channelId, remembered in
// the membership slice like Slack's thunk does.
func (r *appRef) membershipInfo(ctx context.Context, a []json.RawMessage) (any, error) {
	o := argObj(a)
	ch := o.str("channelId")
	want := o.strs("userIds")
	found := map[string]bool{}
	for _, u := range want {
		found[u] = false
	}
	cursor := ""
	for page := 0; page < 10 && len(want) > 0; page++ {
		ids, next, err := r.t.app.Members(ctx, ch, cursor, false)
		if err != nil {
			return nil, err
		}
		for _, id := range ids {
			if _, ok := found[id.ID]; ok {
				found[id.ID] = true
			}
		}
		all := true
		for _, v := range found {
			if !v {
				all = false
			}
		}
		if all || next == "" {
			break
		}
		cursor = next
	}
	r.t.mirror.noteMembership(ch, found)
	return found, nil
}

func (r *appRef) apiCall(ctx context.Context, a []json.RawMessage) (any, error) {
	o := argObj(a)
	method := o.str("method")
	var args map[string]any
	_ = json.Unmarshal(o["args"], &args)
	vals := url.Values{}
	for k, v := range args {
		switch x := v.(type) {
		case string:
			vals.Set(k, x)
		default:
			b, _ := json.Marshal(x)
			vals.Set(k, string(b))
		}
	}
	raw, err := r.t.app.C.Raw(ctx, method, vals)
	if err != nil {
		return nil, err
	}
	return json.RawMessage(raw), nil
}
