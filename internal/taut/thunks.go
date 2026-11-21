package taut

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/url"
	"sort"
	"strings"
	"sync"
	"sync/atomic"

	"github.com/some-du6e/clack/internal/core"
	"github.com/some-du6e/clack/internal/jsrt"
	"github.com/some-du6e/clack/internal/slack"
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
	{"markLastRead", "{id: channelId, ts, threadTs?, reason?, immediate?}", "ui/messages", "marking a conversation read (core.Mark)"},
	{"setUserPrefByApi", "{pref, value}", "ui (prefs)", "saving a user pref (users.prefs.set)"},
	{"setUserPrefFetcher", "{pref, value}", "ui (prefs)", "same, the fetcher form"},
	{"initializeUserPrefs", "{prefsData}", "taut (prefs event)", "prefs loading into the store; the result's prefsData is what plugins leave"},
	{"deleteDraftApi", "{draft: {id, …}, reason?}", "ui/composer, ui/views (drafts)", "deleting a draft (core.DeleteDraft)"},
	{"syncDraftCreate", "{draftId, …draft}", "ui/composer", "saving a new draft (core.SaveDraft)"},
	{"syncDraftUpdates", "{draftId, …draft}", "ui/composer", "updating a draft (core.SaveDraft)"},
	{"clearDrafts", "{ids, syncDelete?}", "ui/views (drafts)", "deleting drafts"},
	{"prepareAndSendMessage", "{channelId, replyToTs?, text, blocks, broadcast?}", "ui/composer", "sending (core.Send); after TransformSendBlocks"},
	{"sendMessage", "{channel, blocks, thread_ts?, reply_broadcast?, unfurl?}", "ui/composer", "the send itself, inside prepareAndSendMessage"},
	{"commitMessageEdit", "{channelId, ts, blocks, text?}", "ui/messages", "submitting an edit; its native dispatches saveMessageEdit"},
	{"saveMessageEdit", "{channelId, ts, blocks, text?}", "ui/messages", "saving an edit (core.Edit, after TransformSendBlocks)"},
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
	{"maybePersistPendingFileData", "{pendingFileId}", "taut (PersistPendingFile, set by ui/composer)", "keeping a pending file's edited title for its upload"},
	{"editFileApi", "{fileId, title}", "taut (EditFile, set by ui/composer)", "renaming an uploaded file; state.files[fileId].title follows"},
	{"getChannelMembershipInfoForUsers", "{channelId, userIds}", "taut", "who is in a channel; fills membership"},
	{"ensureMembersArePresent", "{memberIds}", "taut", "loading users (core.EnsureUsers)"},
	{"fetchRawChannelsById", "{ids}", "taut", "loading conversations (core.ChannelInfo); returns {missing}"},
	{"fetchAndUpsertChannelsById", "{ids, reason?}", "taut", "loading conversations through fetchRawChannelsById, as Slack's; returns {channels, resolved, missing, failedIds}"},
	{"joinChannel", "{channelId}", "ui (join button)", "joining (core.Join)"},
	{"openPrefsModal", "{activeSection?}", "ui (shell)", "opening Preferences"},
	{"enqueueHuddleToast", "{content, svgIconProps, duration}", "ui (toasts)", "a huddle toast"},
	{"maybeFetchBackgrounds", "{huddleId}", "ui/huddleui (themes)", "huddle themes (huddles.listBackgrounds) into huddlesCanvas.backgrounds"},
	{"startEventLog", "{}", "ui/views (huddles)", "huddle event log"},
	{"stopEventLog", "{}", "ui/views (huddles)", "huddle event log"},
	{"stopHuddle", "{}", "ui/views (huddles)", "leaving a huddle"},
	{"displayAlertMessage", "{alert, …}", "ui (alerts)", "an alert banner"},
	{"processAwsTranscription", "{type, …}", "ui/views (huddles)", "huddle captions"},
	{"roomsStartTranscriptionFetcher", "{callId, reason}", "ui/huddleui (the session)", "rooms.startTranscription for the call"},
	{"roomsStopTranscriptionFetcher", "{callId, reason}", "ui/huddleui (the session)", "rooms.stopTranscription for the call"},
	{"logExperimentExposure", "{name}", "none", "Clack logs no exposures"},
	{"handleMessageImmediatelyWithoutPreprocessing", "rtm event", "taut (rtm)", "realtime events"},
	{"blocksFormatFetcher", "{blocks, reason}", "ui/composer (markdown mode)", "blocks.format"},
	{"executeAppAction", "{actionId, appId, channelId, …}", "ui/popups (shortcuts)", "running an app shortcut"},
	{"submitView", "{viewId, values, …}", "ui/popups (app modals)", "submitting an app modal"},
	{"apiCall", "{method, args, …}", "taut", "a Slack API call, settled as Slack's SlackApiRequest settles (05_slack.js apiCallThunk)"},
	{"apiCallExpedited", "{method, args, …}", "taut", "apiCall with EXPEDITED, through apiCall's creator"},
	// used by Slick's plugins (docs/PLUGINS-slick.md)
	{"handleDeepLink", "{uri}", "ui (shell)", "opening a slack:// deep link (UIHooks.DeepLink)"},
	{"navigate", "{id, viewType, params?, uiState?, options?}", "ui (shell)", "Slack's multi-view navigation: a Channel, Thread or Profile view through its deep link (UIHooks.DeepLink); resolves undefined, as Slack's"},
	{"currentUserEndedTyping", "{channelId, threadTs?}", "ui/composer", "the typing indicator ending (Clack sends none)"},
	{"openEditProfileModal", "{}", "ui/popups (profile)", "opening the edit-profile dialog"},
	{"showSavedItemNotification", "{message, …}", "ui (notifications)", "a saved-item reminder notification"},
	{"startHuddle", "{channelId, …}", "ui/views (huddles)", "starting a huddle (Clack has no huddle media)"},
	{"addMemberToPresenceSubscription", "memberId", "taut", "Slack's presence manager (client-boot 4hlf): counts a subscriber of the member's presence (core.AddPresenceSubscription)"},
	{"removeMemberFromPresenceSubscription", "memberId", "taut", "one subscriber fewer (core.RemovePresenceSubscription)"},
}

// SyncThunks return their result synchronously in Slack (plugins call the
// original thunk and use its value right away, e.g. QuickJoin appends to
// the menu template). Their natives run on the JS thread, so they must be
// quick and never touch the network.
var SyncThunks = map[string]bool{
	"getContextMenuTemplate": true, "getMemberContextMenuTemplate": true,
	"autocompleteChannels": true, "autocompleteMembers": true,
}

// NativeFunc is a thunk's native implementation: Clack's own code for what
// Slack's thunk does. args is the (possibly plugin-changed) JSON argument
// list Slack passes (usually one object).
type NativeFunc func(ctx context.Context, args []json.RawMessage) (any, error)

type thunkTable struct {
	t       *Taut
	patched atomic.Pointer[map[string]bool]

	mu      sync.Mutex
	natives map[string]NativeFunc  // defaults for plugin-initiated dispatches
	calls   map[int64]NativeFunc   // in-flight native cores of a Dispatch
	seq     map[int64]bool         // the calls whose natives run in JS's order (DispatchSeq)
	checks  map[int64]*atomic.Bool // CheckThenDispatch's: its checks passed (checksPassed)
	next    int64
	// seqGo runs DispatchSeq's natives with no plugin, in the order they
	// came
	seqGo seqRunner
}

// seqRunner runs funcs one after another on a goroutine of its own, in the
// order they came.
type seqRunner struct {
	mu sync.Mutex
	q  []func()
	on bool
}

func (s *seqRunner) run(f func()) {
	s.mu.Lock()
	s.q = append(s.q, f)
	start := !s.on
	s.on = true
	s.mu.Unlock()
	if !start {
		return
	}
	go func() {
		for {
			s.mu.Lock()
			if len(s.q) == 0 {
				s.on, s.q = false, nil
				s.mu.Unlock()
				return
			}
			f := s.q[0]
			s.q[0] = nil
			s.q = s.q[1:]
			s.mu.Unlock()
			f()
		}
	}()
}

// noTautSeq is seqGo for DispatchSeq with no plugin system at all.
var noTautSeq seqRunner

func newThunkTable(t *Taut) *thunkTable {
	tt := &thunkTable{t: t, natives: map[string]NativeFunc{}, calls: map[int64]NativeFunc{}, seq: map[int64]bool{}}
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
	// a runtime that started first defined its creators already (T.init):
	// define this one now, so its waiters resolve and a dispatch finds it
	// (JS rejects a thunk it doesn't know at once)
	if r := t.liveRuntime(); r != nil {
		r.post("nativeThunk", name)
	}
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
	out, err := t.dispatchJS(ctx, name, []any{args}, nativeCore(name, native))
	if err != nil {
		return zero, err
	}
	return decodeDispatch[R](name, out)
}

// nativeCore is Dispatch's native as JS calls it back (callNative): its
// args decoded from JSON.
func nativeCore[A, R any](name string, native func(ctx context.Context, args A) (R, error)) NativeFunc {
	return func(ctx context.Context, raw []json.RawMessage) (any, error) {
		var a A
		if len(raw) > 0 {
			if err := json.Unmarshal(raw[0], &a); err != nil {
				return nil, fmt.Errorf("taut: %s args: %w", name, err)
			}
		}
		return native(ctx, a)
	}
}

// decodeDispatch is a dispatch's JSON result as R.
func decodeDispatch[R any](name string, out json.RawMessage) (R, error) {
	var r R
	if len(out) == 0 || string(out) == "null" {
		return r, nil
	}
	if err := json.Unmarshal(out, &r); err != nil {
		var zero R
		return zero, fmt.Errorf("taut: %s result: %w", name, err)
	}
	return r, nil
}

// DispatchThen is Dispatch without the wait, for the user's input (a
// keystroke's typing report): with the runtime running its JS call is
// queued before DispatchThen returns, on the caller's goroutine, in order
// with what the caller queues after it (the send at Enter: the typing
// report a keystroke dispatched from a goroutine of its own could come
// after the message). done (nil for none) hears the result on another
// goroutine; unpatched, native runs on one.
func DispatchThen[A, R any](ctx context.Context, t *Taut, name string, args A, native func(ctx context.Context, args A) (R, error), done func(R, error)) {
	dispatchThen(ctx, t, name, args, native, done, false)
}

// DispatchSeq is DispatchThen for dispatches that each go out without
// waiting for the ones before (Slack's messageReceived dispatches
// showNotification for every message without awaiting the last), whose
// natives run in the order their dispatches call them: in JS's order, one
// after another, as Slack runs them in its one JS thread, and in the order
// DispatchSeq was called with no plugin. A goroutine each could run two
// in reverse. For natives that never wait long.
func DispatchSeq[A, R any](ctx context.Context, t *Taut, name string, args A, native func(ctx context.Context, args A) (R, error), done func(R, error)) {
	dispatchThen(ctx, t, name, args, native, done, true)
}

func dispatchThen[A, R any](ctx context.Context, t *Taut, name string, args A, native func(ctx context.Context, args A) (R, error), done func(R, error), seq bool) {
	if done == nil {
		done = func(R, error) {}
	}
	if !t.ThunkPatched(name) {
		run := func() { done(native(ctx, args)) }
		switch {
		case seq && t != nil:
			t.thunks.seqGo.run(run)
		case seq:
			noTautSeq.run(run)
		default:
			go run()
		}
		return
	}
	rt := t.liveRuntime()
	if rt == nil {
		run := func() { done(Dispatch(ctx, t, name, args, native)) }
		if seq {
			// in order with the dispatches around it while the runtime
			// is down too: a goroutine each could show two toasts in
			// reverse (the dispatch waits for a runtime that starts)
			t.thunks.seqGo.run(run)
		} else {
			go run()
		}
		return
	}
	ctx = inOrder(ctx)
	tt := t.thunks
	id := tt.add(ctx, nativeCore(name, native))
	if seq {
		tt.mu.Lock()
		tt.seq[id] = true
		tt.mu.Unlock()
	}
	// the call waits for its JS whatever the caller's deadline (a toast
	// behind a 30 s backlog still shows), and the native stays registered
	// until that JS has run: one dropped at the deadline was gone when JS
	// called it
	rt.rt.CallThen(context.WithoutCancel(ctx), "__taut.dispatchThunk", func(out json.RawMessage, err error) {
		tt.drop(id)
		if err != nil {
			var zero R
			done(zero, err)
			return
		}
		done(decodeDispatch[R](name, out))
	}, name, []any{args}, id, SyncThunks[name])
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
	ctx = inOrder(ctx)
	tt := t.thunks
	id := tt.add(ctx, native)
	defer tt.drop(id)
	return rt.rt.Call(ctx, "__taut.dispatchThunk", name, args, id, SyncThunks[name])
}

// inOrder is ctx for a dispatch: the user's (UserAction) as it is, any
// other in order with the user's later input (jsrt.InOrder). What the
// native UI dispatches is the user's doing (a reaction, a draft sync, a
// menu), and a plugin click after it went ahead of it when it waited in
// the queue: OwlMode reads its mode in the reaction's patch, and a
// reaction meant for Owl went out under the user's name.
func inOrder(ctx context.Context) context.Context {
	if jsrt.IsUserBlocking(ctx) || jsrt.IsInOrder(ctx) {
		return ctx
	}
	return jsrt.InOrder(ctx)
}

// add registers an in-flight dispatch's native core for JS to call back
// (callNative) and returns its id. JS calls it with a context of its own:
// with a user-blocking ctx, the native's JS work (a send's transforms, the
// thunks it dispatches in turn) is the user's too, ahead of the renders
// queued meanwhile; with an in-order one, in order with the user's later
// input.
func (tt *thunkTable) add(ctx context.Context, native NativeFunc) int64 {
	// the native's work is the user's (a post, a reaction): it outlives the
	// plugin runtime that called it, which may close meanwhile (plugins
	// turned off, the watchdog), keeping the chain it goes on with
	inner := native
	mark := func(c context.Context) context.Context { return c }
	switch {
	case jsrt.IsUserChain(ctx):
		noHold := jsrt.IsNoChain(ctx)
		mark = func(c context.Context) context.Context {
			c = jsrt.UserChain(c)
			if noHold {
				c = jsrt.NoChain(c)
			}
			return c
		}
	case jsrt.IsUserBlocking(ctx):
		mark = jsrt.UserBlocking
	case jsrt.IsInOrder(ctx):
		mark = jsrt.InOrder
	}
	native = func(c context.Context, raw []json.RawMessage) (any, error) {
		return inner(mark(context.WithoutCancel(c)), raw)
	}
	tt.mu.Lock()
	tt.next++
	id := tt.next
	tt.calls[id] = native
	tt.mu.Unlock()
	return id
}

// drop forgets a finished dispatch's native core: JS calling it after
// that fails ("that dispatch already finished").
func (tt *thunkTable) drop(id int64) {
	tt.mu.Lock()
	delete(tt.calls, id)
	delete(tt.seq, id)
	delete(tt.checks, id)
	tt.mu.Unlock()
}

// watchChecks is the flag checksPassed sets for dispatch id (a
// CheckThenDispatch's call): its send checks let the message go, and what
// follows is the dispatch.
func (tt *thunkTable) watchChecks(id int64) *atomic.Bool {
	f := new(atomic.Bool)
	tt.mu.Lock()
	if tt.checks == nil {
		tt.checks = map[int64]*atomic.Bool{}
	}
	tt.checks[id] = f
	tt.mu.Unlock()
	return f
}

// checksPassed is JS saying dispatch id's checks passed (JS thread).
func (tt *thunkTable) checksPassed(id int64) {
	tt.mu.Lock()
	f := tt.checks[id]
	tt.mu.Unlock()
	if f != nil {
		f.Store(true)
	}
}

// inSeq reports whether dispatch id's native runs in JS's order
// (DispatchSeq; JS thread).
func (tt *thunkTable) inSeq(id int64) bool {
	tt.mu.Lock()
	defer tt.mu.Unlock()
	return tt.seq[id]
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

// navigateLink is the slack:// deep link of navigate's view (o is its
// {id, viewType, params}), "" for a view Clack doesn't route to.
func navigateLink(o obj) string {
	var params struct {
		ThreadID string `json:"threadId"`
	}
	_ = json.Unmarshal(o["params"], &params)
	id := o.str("id")
	q := url.Values{}
	switch o.str("viewType") {
	case "Channel":
		if id == "" {
			return ""
		}
		q.Set("id", id)
		return "slack://channel?" + q.Encode()
	case "Thread":
		// threadId is "<channel>-<thread ts>" (ChatTabs' splitProps, Slack's
		// thread views)
		i := strings.LastIndex(params.ThreadID, "-")
		if i <= 0 || i == len(params.ThreadID)-1 {
			return ""
		}
		q.Set("id", params.ThreadID[:i])
		q.Set("thread_ts", params.ThreadID[i+1:])
		return "slack://channel?" + q.Encode()
	case "Profile":
		if id == "" {
			return ""
		}
		q.Set("id", id)
		return "slack://user?" + q.Encode()
	}
	return ""
}

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
			ch, ts := firstNonEmpty(o.str("id"), o.str("channelId")), o.str("ts")
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
			o := argObj(a)
			id := o.str("draftId")
			if id == "" {
				var d struct {
					ID string `json:"id"`
				}
				_ = json.Unmarshal(o["draft"], &d)
				id = d.ID
			}
			return nil, t.app.DeleteDraft(ctx, id)
		},
		"addAndUploadPendingFile": func(ctx context.Context, a []json.RawMessage) (any, error) {
			var args struct {
				File File `json:"file"`
			}
			if len(a) > 0 {
				_ = json.Unmarshal(a[0], &args)
			}
			if args.File.Name == "" && len(args.File.Data) == 0 {
				return nil, errors.New("no file to upload")
			}
			up, err := t.app.PrepareUpload(ctx, core.UploadFile{Name: args.File.Name, Title: args.File.Name, Data: args.File.Data}, nil)
			if err != nil {
				return nil, err
			}
			return map[string]any{"fileIds": []string{up.ID}}, nil
		},
		"maybePersistPendingFileData": func(ctx context.Context, a []json.RawMessage) (any, error) {
			return nil, persistPendingFile(ctx, argObj(a).str("pendingFileId"))
		},
		"editFileApi": func(ctx context.Context, a []json.RawMessage) (any, error) {
			o := argObj(a)
			return t.editFile(ctx, firstNonEmpty(o.str("fileId"), o.str("id")), o.str("title"))
		},
		"clearDrafts": func(ctx context.Context, a []json.RawMessage) (any, error) {
			o := argObj(a)
			ids := o.strs("ids")
			if len(ids) == 0 {
				ids = o.strs("draftIds")
			}
			for _, id := range ids {
				if err := t.app.DeleteDraft(ctx, id); err != nil {
					return nil, err
				}
			}
			return nil, nil
		},
		"getChannelMembershipInfoForUsers": app().membershipInfo,
		"ensureMembersArePresent": func(ctx context.Context, a []json.RawMessage) (any, error) {
			ids := argObj(a).strs("memberIds")
			t.app.EnsureUsers(ctx, ids)
			// the upsert is a store change: the JS side reads what it held
			// as absent again (Taut's getMember waits on it), as Slack's
			// selectors see the members ensureMembersArePresent upserted
			if len(ids) > 0 {
				t.mirror.invalidate("members", ids...)
				t.mirror.send() // ahead of the thunk's answer on the JS thread
			}
			return nil, nil
		},
		"fetchRawChannelsById": t.fetchRawChannels,
		// Slack's dispatches fetchRawChannelsById (PrivateChannel patches that
		// one to name what's missing), then upserts what came back
		"fetchAndUpsertChannelsById": func(ctx context.Context, a []json.RawMessage) (any, error) {
			o := argObj(a)
			ids := o.strs("ids")
			reason := o.str("reason")
			if reason == "" {
				reason = "fetchAndUpsertChannelsById"
			}
			raw, err := t.DispatchJSON(ctx, "fetchRawChannelsById", []any{map[string]any{"ids": ids, "fetchConditionally": true, "reason": reason}}, t.fetchRawChannels)
			if err != nil {
				return nil, err
			}
			var res struct {
				Missing []string `json:"missing"`
			}
			_ = json.Unmarshal(raw, &res)
			gone := map[string]bool{}
			for _, id := range res.Missing {
				gone[id] = true
			}
			resolved := []string{}
			for _, id := range ids {
				if !gone[id] {
					resolved = append(resolved, id)
				}
			}
			if res.Missing == nil {
				res.Missing = []string{}
			}
			return map[string]any{"channels": []any{}, "resolved": resolved, "missing": res.Missing, "failedIds": []string{}}, nil
		},
		"commitMessageEdit": func(ctx context.Context, a []json.RawMessage) (any, error) {
			if len(a) == 0 {
				return nil, errors.New("commitMessageEdit needs {channelId, ts, blocks}")
			}
			return t.DispatchJSON(ctx, "saveMessageEdit", []any{a[0]}, func(ctx context.Context, a []json.RawMessage) (any, error) {
				return nil, t.saveEdit(ctx, argObj(a))
			})
		},
		"saveMessageEdit": func(ctx context.Context, a []json.RawMessage) (any, error) {
			return nil, t.saveEdit(ctx, argObj(a))
		},
		// Slack's presence manager (client-boot 4hlf): the argument is the
		// member's id; presence_sub follows a member's first subscriber
		// and its last one leaving (core/presence_subs.go)
		"addMemberToPresenceSubscription": func(ctx context.Context, a []json.RawMessage) (any, error) {
			t.app.AddPresenceSubscription(strArg(a, 0))
			return nil, nil
		},
		"removeMemberFromPresenceSubscription": func(ctx context.Context, a []json.RawMessage) (any, error) {
			t.app.RemovePresenceSubscription(strArg(a, 0))
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
				o := argObj(a)
				f(firstNonEmpty(o.str("activeSection"), o.str("tab")))
			}
			return nil, nil
		},
	}
	// Slack bookkeeping with nothing to do in Clack: they resolve, so
	// plugins awaiting them continue.
	n["handleDeepLink"] = func(ctx context.Context, a []json.RawMessage) (any, error) {
		if f := t.opts.UI.DeepLink; f != nil {
			return f(argObj(a).str("uri")), nil
		}
		return false, nil
	}
	// Slack's navigate thunk (client-boot pf/G): navigateInMainWindow
	// (qBXZ) for the main window, whose promise resolves undefined whether
	// the view set was committed or the middleware terminated it. Clack
	// opens the views it has through the deep links they share (ChatTabs'
	// thread tabs: {id: "thread", viewType: "Thread", params: {threadId:
	// "C…-ts"}} opens the thread beside its channel, where Slack's
	// forceRenderInPrimary draws it alone); others navigate nowhere.
	n["navigate"] = func(ctx context.Context, a []json.RawMessage) (any, error) {
		if f := t.opts.UI.DeepLink; f != nil {
			if uri := navigateLink(argObj(a)); uri != "" {
				f(uri)
			}
		}
		return nil, nil
	}
	// the transcription fetchers reach the native huddle (huddle.go)
	n["roomsStartTranscriptionFetcher"] = func(ctx context.Context, a []json.RawMessage) (any, error) {
		return t.huddleTranscription(ctx, a, true)
	}
	n["roomsStopTranscriptionFetcher"] = func(ctx context.Context, a []json.RawMessage) (any, error) {
		return t.huddleTranscription(ctx, a, false)
	}
	n["maybeFetchBackgrounds"] = func(ctx context.Context, a []json.RawMessage) (any, error) { return t.huddleBackgrounds(ctx) }
	for _, name := range []string{"startEventLog", "stopEventLog", "stopHuddle", "displayAlertMessage", "currentUserEndedTyping", "startHuddle",
		"processAwsTranscription", "logExperimentExposure", "handleMessageImmediatelyWithoutPreprocessing"} {
		n[name] = func(ctx context.Context, a []json.RawMessage) (any, error) { return nil, nil }
	}
	tt.natives = n
}

// saveEdit is core.Edit with the send transforms applied to the blocks.
func (t *Taut) saveEdit(ctx context.Context, o obj) error {
	blocks := o["blocks"]
	if len(blocks) == 0 || string(blocks) == "null" {
		blocks = nil
	} else {
		blocks = t.TransformSendBlocks(ctx, blocks)
	}
	return t.app.Edit(ctx, firstNonEmpty(o.str("channelId"), o.str("channel")), o.str("ts"), o.str("text"), blocks)
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

// apiCall sends the request of Slack's apiCall thunk, which 05_slack.js
// (apiCallThunk) builds as Slack's does (client-boot 0hZc, build
// 35b42de8b870): the form fields getFormData makes of its args, its file
// parts, and BaseRequest's maxAttempts (1 for the methods apiCall never
// retries). It is tried the way Slack's API queue tries a SlackApiRequest
// (slack.SlackRetriesUpTo), and the answer is the last try, whole, for JS
// to settle the thunk from as Slack's request settles (the response with
// its request ids, or Slack's rejection with the answer's JSON as
// err.data): {attempts, status, reqId, retryAfter, body}, status being
// the HTTP status, "TIMEOUT" (no answer within RequestTimeout) or 0 (no
// answer). A request Clack won't send (the session ended, a method that
// would sign you out) rejects with Clack's reason.
func (r *appRef) apiCall(ctx context.Context, a []json.RawMessage) (any, error) {
	var req struct {
		Method string            `json:"method"`
		Fields map[string]string `json:"fields"`
		Files  []struct {
			Field string `json:"field"`
			File  File   `json:"file"`
		} `json:"files"`
		MaxAttempts int `json:"maxAttempts"`
	}
	if len(a) > 0 {
		if err := json.Unmarshal(a[0], &req); err != nil {
			return nil, fmt.Errorf("taut: apiCall: %w", err)
		}
	}
	if req.MaxAttempts == 0 {
		req.MaxAttempts = slack.SlackMaxAttempts
	}
	if len(req.Files) > 1 {
		return nil, errors.New("taut: apiCall: Clack sends one file part per call")
	}
	vals := url.Values{}
	for k, v := range req.Fields {
		vals.Set(k, v)
	}
	var last *slack.Attempt
	tries := 0
	ctx = slack.ObserveAttempts(slack.SlackRetriesUpTo(ctx, req.MaxAttempts), func(at slack.Attempt) {
		if last != nil && last.URL != at.URL {
			tries = 0 // Call sent it on to the other host: a request of its own
		}
		tries++
		last = &at
	})
	var err error
	if len(req.Files) == 0 {
		_, err = r.t.app.C.Raw(ctx, req.Method, vals)
	} else {
		f := req.Files[0]
		var out json.RawMessage
		err = r.t.app.C.CallMultipartFile(ctx, req.Method, vals, slack.MultipartFile{Field: f.Field, Name: f.File.Name, Type: f.File.Type, Data: f.File.Data}, &out)
	}
	if last == nil {
		// nothing went out: the backoff guard answered with the failure
		// it holds (Slack would have sent it again, and been answered the
		// same), or Clack refused to send it
		at, ok := slack.AttemptOf(err)
		if !ok {
			if err == nil {
				err = errors.New("taut: apiCall: no request was sent")
			}
			return nil, err
		}
		last, tries = &at, 1
	}
	var status any = last.Status
	switch {
	case last.Timeout:
		status = "TIMEOUT"
	case last.Err != nil:
		status = 0
	}
	// XHR's text decodes the answer as UTF-8, dropping a byte order mark
	body := bytes.TrimPrefix(last.Body, []byte("\xef\xbb\xbf"))
	return map[string]any{"attempts": tries, "status": status, "reqId": last.ReqID, "retryAfter": last.RetryAfter, "body": string(body)}, nil
}

// fetchRawChannels is fetchRawChannelsById's native: core loads each
// conversation (conversations.info); what it can't is missing.
func (t *Taut) fetchRawChannels(ctx context.Context, a []json.RawMessage) (any, error) {
	o := argObj(a)
	ids := o.strs("ids")
	if len(ids) == 0 {
		ids = o.strs("channelIds")
	}
	missing := []string{}
	for _, id := range ids {
		if _, err := t.app.ChannelInfo(ctx, id); err != nil {
			missing = append(missing, id)
			continue
		}
		t.mirror.invalidate("channels", id)
	}
	return map[string]any{"missing": missing}, nil
}
