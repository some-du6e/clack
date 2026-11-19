package messages

// What the lists' copies and the plugins' views read changes as Slack's
// would: prefs every Doc reads, a change taut's views took inside a row's
// 250 ms, a mention in a broadcast's preamble. Synthetic data on
// internal/fakeslack.

import (
	"context"
	"encoding/json"
	"fmt"
	"slices"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/some-du6e/clack/internal/core"
	"github.com/some-du6e/clack/internal/richtext"
	"github.com/some-du6e/clack/internal/slack"
	"github.com/some-du6e/clack/internal/store"
	"github.com/some-du6e/clack/internal/ui/richview"
	"github.com/some-du6e/clack/internal/ui/slackc"
	"github.com/some-du6e/clack/internal/ui/theme"
)

// docHas reports whether d has a span of kind (and, with style, that
// style) whose text or id contains sub.
func docHas(d richtext.Doc, kind richtext.SpanKind, style richtext.Style, sub string) bool {
	var walk func(bs []richtext.Block) bool
	walk = func(bs []richtext.Block) bool {
		for _, b := range bs {
			for _, sp := range b.Spans {
				if sp.Kind == kind && (style == 0 || sp.Style&style != 0) && (strings.Contains(sp.Text, sub) || sp.ID == sub) {
					return true
				}
			}
			if walk(b.Children) {
				return true
			}
		}
		return false
	}
	return walk(d.Blocks)
}

// "Display emoji as plain text" and a new highlight word re-convert the
// messages a list holds, as Slack's next render reads them; both are
// baked into every Doc. They converted nothing: a list kept its emoji
// images and old highlights until it loaded again.
func TestDocPrefsConvertLoadedMessages(t *testing.T) {
	app, _, _ := shotsApp(t)
	app.S.PutConv(slack.Conversation{ID: refsChan, Name: "launch", IsChannel: true, IsMember: true, NumMembers: 12})
	ts := fmt.Sprintf("%d.000100", time.Now().Add(-time.Hour).Unix())
	app.S.SetLatest(refsChan, []slack.Message{{Type: "message", TS: ts, User: "UANA00001", Text: "the launch is today :rocket:"}}, false)
	v := NewChannelView(coreHost{app: app, th: theme.New(false)}, refsChan)
	v.Preview = true
	frame := frameFn(v)
	doc := func() richtext.Doc {
		if r := v.s.rows[ts]; r != nil {
			return r.m.Doc
		}
		return richtext.Doc{}
	}
	until(t, 8*time.Second, "the row with its emoji", frame, func() bool { return docHas(doc(), richtext.Emoji, 0, "rocket") })
	ctx := context.Background()
	if err := app.SetMultiplePrefs(ctx, map[string]any{"emoji_mode": "as_text"}); err != nil {
		t.Fatal(err)
	}
	until(t, 8*time.Second, "the emoji as text", frame, func() bool {
		return !docHas(doc(), richtext.Emoji, 0, "rocket") && docHas(doc(), richtext.Text, 0, ":rocket:")
	})
	if err := app.SetMultiplePrefs(ctx, map[string]any{"all_notifications_prefs": `{"global":{"global_keywords":"launch"}}`}); err != nil {
		t.Fatal(err)
	}
	until(t, 8*time.Second, "the highlight word highlighted", frame, func() bool { return docHas(doc(), richtext.Text, richtext.Highlight, "launch") })
}

// askingHost counts the redraws the hub asks for.
type askingHost struct {
	coreHost
	asked *atomic.Int64
}

func (h askingHost) Invalidate() { h.asked.Add(1) }

// A plugin's view changed inside the row's 250 ms (a rename only the
// plugin's copy of a message reads): the frame taut asks for re-reads it
// (ViewEpoch), and the row shows the new name with no other frame. The
// frame came, the row's viewRefresh skipped the read, and an idle window
// drew no other: the old name stayed.
func TestPatchedRowTakesChangeWithoutAnotherFrame(t *testing.T) {
	app, _, _ := shotsApp(t)
	app.S.PutConv(slack.Conversation{ID: refsChan, Name: "launch", IsChannel: true, IsMember: true, NumMembers: 12})
	ts := fmt.Sprintf("%d.000100", time.Now().Add(-time.Hour).Unix())
	app.S.SetLatest(refsChan, []slack.Message{{Type: "message", TS: ts, User: "UBEN00001", Text: "Notes from the sync"}}, false)
	var asked atomic.Int64
	u := &pluginUI{trees: map[uint64][]byte{}}
	hooks := u.hooks()
	hooks.Invalidate = func() { asked.Add(1) }
	tt := newTaut(t, app, hooks)
	SetTaut(tt)
	slackc.SetBridge(tt.Bridge())
	t.Cleanup(func() { SetTaut(nil); slackc.SetBridge(nil); slackc.Reset(); slackc.SetPatched(nil) })
	notes := `{"id":"F0NOTES001","name":"notes.pdf","title":"Sync notes","mimetype":"application/pdf","filetype":"pdf","pretty_type":"PDF","mode":"hosted","user":"UCARA0001","size":1024}`
	install(t, tt, pluginJS(`
class ShareNotes extends TautPlugin {
  static id = 'ShareNotes'
  static pluginName = 'Share notes (test)'
  static description = 'test: its copy of a message shares a file the message does not'
  static authors = ['clack']
  static category = 'messages'
  static defaultConfig = { enabled: true }
  start() {
    const notes = `+notes+`
    const share = (ts, m) => (ts === '`+ts+`' && m ? { ...m, files: [notes] } : m)
    const { patchSlice, mapEntries } = this.api.redux
    patchSlice('messages', (ch, bucket) => (ch === '`+refsChan+`' && bucket && typeof bucket === 'object' ? mapEntries(bucket, share) : bucket))
  }
}
return ShareNotes`))
	until(t, 5*time.Second, "the messages slice patched", func() {}, func() bool { return tt.SlicePatched("messages") })
	v := NewChannelView(askingHost{coreHost{app: app, th: theme.New(false)}, &asked}, refsChan)
	v.Preview = true
	frame := frameFn(v)
	text := func() string { return rowText(v.s.rows[ts]) }
	until(t, 8*time.Second, "the plugin's file in the row", frame, func() bool { return strings.Contains(text(), "cara | ") })
	// a frame that reads the view (past its 250 ms), then at once the rename
	time.Sleep(viewRefresh + 20*time.Millisecond)
	frame()
	last := asked.Load()
	refsUserChange(app, "kit.l")
	for dl := time.Now().Add(3 * time.Second); time.Now().Before(dl); {
		if n := asked.Load(); n != last { // a frame only when one is asked for
			last = n
			frame()
			if strings.Contains(text(), "kit.l | ") {
				return
			}
			continue
		}
		time.Sleep(5 * time.Millisecond)
	}
	t.Errorf("with frames only when asked for, the row still draws %q; want the plugin's file under kit.l", text())
}

// A broadcast's preamble ("replied to a thread: …") draws the root's
// mentions as the store names the members now: mentions aren't
// conversion refs, so a rename converts no root again, and the preview
// kept the old @name.
func TestBroadcastPreambleReadsMentionsAsItDraws(t *testing.T) {
	names := map[string]string{"UCARA0001": "cara"}
	richview.SetMemberRead(func(id string) (string, richview.MemberMiss) {
		if n, ok := names[id]; ok {
			return n, richview.MemberLoaded
		}
		return "", richview.MemberLoading
	})
	t.Cleanup(func() { richview.SetMemberRead(nil) })
	root := core.Message{TS: "1700000001.000100", ThreadTS: "1700000001.000100", Doc: richtext.Doc{Blocks: []richtext.Block{{Kind: richtext.Paragraph,
		Spans: []richtext.Span{{Kind: richtext.User, ID: "UCARA0001", Text: "@cara"}, {Kind: richtext.Text, Text: " can you review?"}}}}}}
	s := &listState{}
	s.snap.msgs = []entry{{m: root, rev: 1}}
	r := &row{m: core.Message{TS: "1700000002.000200", ThreadTS: root.TS, Broadcast: true}}
	rc := &rowCtx{list: s, usersRev: 1}
	if got := r.rootPreview(rc); got != "@cara can you review?" {
		t.Fatalf("the preamble's preview: %q", got)
	}
	names["UCARA0001"] = "kit.l" // she renamed herself: the users revision moves
	rc.usersRev = 2
	if got := r.rootPreview(rc); got != "@kit.l can you review?" {
		t.Errorf("after the rename the preamble's preview is %q; want the name as the mention draws it now", got)
	}
}

// storeMemberRead reads mentions' names from app's store, as the ui
// root's lazy_users does (getMemberById: store.DisplayName).
func storeMemberRead(t *testing.T, app *core.App) {
	richview.SetMemberRead(func(id string) (string, richview.MemberMiss) {
		if u, ok := app.S.User(id); ok {
			return store.DisplayName(u), richview.MemberLoaded
		}
		return "", richview.MemberLoading
	})
	t.Cleanup(func() { richview.SetMemberRead(nil) })
}

// drawWhile draws frames back to back, as a window does while anything
// animates, until cond holds or d passes.
func drawWhile(frame func(), d time.Duration, cond func() bool) bool {
	for dl := time.Now().Add(d); time.Now().Before(dl); {
		frame()
		if cond() {
			return true
		}
		time.Sleep(time.Millisecond)
	}
	return false
}

// Through the hub, with nothing else on screen naming her: the broadcast's
// root (in the thread, not the channel) mentions Cara, and she renames
// herself while the window draws frame after frame. The preview reads the
// names its mentions draw with each frame, as Slack's mentions read their
// members on every render, and reads its text again when one changed:
// the rename, or a plugin's nickname. Before, it waited for the users
// revision, which moves only when the hub thinks a list drew the member in
// its latest frame: a frame drawn between her change and the hub hearing
// of it made that false, and the old @name stayed.
func TestBroadcastPreambleFollowsARename(t *testing.T) {
	app, _, _ := shotsApp(t)
	storeMemberRead(t, app)
	const ch, cara = "C0PREAMB01", "UCARA0001"
	app.S.PutConv(slack.Conversation{ID: ch, Name: "launch", IsChannel: true, IsMember: true, NumMembers: 9})
	app.S.PutUsers([]slack.User{{ID: cara, Name: "cara", Profile: slack.Profile{DisplayName: "cara", RealName: "Cara Lee"}}})
	base := time.Now().Add(-2 * time.Hour).Unix()
	rootTS, bTS := fmt.Sprintf("%d.000100", base), fmt.Sprintf("%d.000300", base+600)
	mention := json.RawMessage(`[{"type":"rich_text","block_id":"r","elements":[{"type":"rich_text_section","elements":[` +
		`{"type":"user","user_id":"` + cara + `"},{"type":"text","text":" can you review?"}]}]}]`)
	root := slack.Message{Type: "message", TS: rootTS, User: "UANA00001", Text: "<@" + cara + "> can you review?", Blocks: mention}
	root.ThreadTS, root.ReplyCount, root.LatestReply = rootTS, 1, bTS
	bcast := slack.Message{Type: "message", Subtype: "thread_broadcast", TS: bTS, ThreadTS: rootTS, User: "UBEN00001",
		Text: "on it", Blocks: shotRich(shotSection("on it"))}
	rootCopy := root
	bcast.Root = &rootCopy
	app.S.SetLatest(ch, []slack.Message{bcast}, false) // the root isn't in the channel's history: only the thread has it
	app.S.SetThread(ch, rootTS, []slack.Message{root, bcast})
	v := NewChannelView(coreHost{app: app, th: theme.New(false)}, ch)
	frame := frameFn(v)
	preview := func() string {
		if r := v.s.rows[bTS]; r != nil {
			return r.rootPrev
		}
		return ""
	}
	until(t, 5*time.Second, "the broadcast's preview naming Cara", frame, func() bool { return strings.Contains(preview(), "@cara") })
	u := map[string]any{"id": cara, "name": "cara", "profile": map[string]any{"display_name": "kit.l", "real_name": "Cara Lee"}}
	raw, _ := json.Marshal(map[string]any{"type": "user_change", "user": u})
	app.Inject(slack.Event{Type: "user_change", Raw: raw})
	if !drawWhile(frame, 5*time.Second, func() bool { return strings.Contains(preview(), "@kit.l") }) {
		t.Errorf("the preview, drawn frame after frame, still reads %q after Cara's rename to kit.l", preview())
	}
}

// A row's sender, renamed while the window draws frame after frame: the
// hub counts the member as drawn by the frames that drew her (store's
// last access), not only by the access the event carries from before it.
// A frame drawn between her change and the hub hearing of it was newer
// than that access, so her change looked undrawn: the users revision
// stayed and the row kept her old name.
func TestSenderRenameWhileDrawing(t *testing.T) {
	app, _, _ := shotsApp(t)
	const ch, cara = "C0SENDER01", "UCARA0002"
	app.S.PutConv(slack.Conversation{ID: ch, Name: "ops", IsChannel: true, IsMember: true, NumMembers: 9})
	app.S.PutUsers([]slack.User{{ID: cara, Name: "cara", Profile: slack.Profile{DisplayName: "cara", RealName: "Cara Lee"}}})
	ts := fmt.Sprintf("%d.000100", time.Now().Add(-time.Hour).Unix())
	app.S.SetLatest(ch, []slack.Message{{Type: "message", TS: ts, User: cara, Text: "deploying now", Blocks: shotRich(shotSection("deploying now"))}}, false)
	v := NewChannelView(coreHost{app: app, th: theme.New(false)}, ch)
	frame := frameFn(v)
	name := func() string {
		if r := v.s.rows[ts]; r != nil {
			return r.name
		}
		return ""
	}
	until(t, 5*time.Second, "Cara's row", frame, func() bool { return name() == "cara" })
	var mu sync.Mutex
	var drawn []string
	labelHook = func(txt string, _ textStyle) { mu.Lock(); drawn = append(drawn, txt); mu.Unlock() }
	t.Cleanup(func() { labelHook = nil })
	u := map[string]any{"id": cara, "name": "cara", "profile": map[string]any{"display_name": "kit.l", "real_name": "Cara Lee"}}
	raw, _ := json.Marshal(map[string]any{"type": "user_change", "user": u})
	app.Inject(slack.Event{Type: "user_change", Raw: raw})
	ok := drawWhile(frame, 5*time.Second, func() bool {
		mu.Lock()
		defer mu.Unlock()
		got := slices.Contains(drawn, "kit.l")
		drawn = drawn[:0]
		return got
	})
	if !ok {
		t.Errorf("the row, drawn frame after frame, still draws %q after Cara's rename to kit.l", name())
	}
}
