package ui

// A canvas's or a list's file channel as Slack's RedirectFileChannel
// (client-boot 548618a2169f2a90, AOkh aW) and jumpToMessage open it
// (canvas_open.go): a direct toMessage is the thread view on the message,
// toChannel the canvas or the list, a jump the canvas or the list at the
// record from wherever it starts; the thread header names the file
// (messages/thread_file.go); the Activity pane keeps its view while a
// jump resolves. internal/fakeslack, made-up content only.

import (
	"image"
	"net/url"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"sync"
	"testing"
	"time"

	"gioui.org/gpu/headless"
	"gioui.org/io/key"

	"github.com/some-du6e/clack/internal/fakeslack"
	"github.com/some-du6e/clack/internal/richtext"
	"github.com/some-du6e/clack/internal/slack"
	"github.com/some-du6e/clack/internal/ui/canvasview"
	"github.com/some-du6e/clack/internal/ui/listview"
	"github.com/some-du6e/clack/internal/ui/messages"
	"github.com/some-du6e/clack/internal/ui/shortcut"
	"github.com/some-du6e/clack/internal/ui/views"
)

// crThread is the thread view on ch/ts in the main window's views, or
// with split the split view's.
func crThread(e *actShellEnv, ch, ts string, split bool) *messages.ThreadView {
	var tv *messages.ThreadView
	get := func() {
		if c := e.a.convos[ch+"/"+ts]; c != nil {
			if l, ok := c.list.(threadList); ok {
				tv = l.ThreadView
			}
		}
	}
	if split {
		e.a.inPane(paneSplit, get)
	} else {
		get()
	}
	return tv
}

// crHeader waits until the thread view on ch/ts names its conversation
// (with link, as a link) and returns the name and where it is.
func crHeader(e *actShellEnv, ch, ts string, split, link bool) (messages.HeaderSubtitle, image.Rectangle) {
	e.t.Helper()
	var sub messages.HeaderSubtitle
	var r image.Rectangle
	e.until("the thread header's subtitle", func() bool {
		tv := crThread(e, ch, ts, split)
		if tv == nil {
			return false
		}
		var ok bool
		sub, r, ok = tv.HeaderName()
		return ok && (!link || sub.Link)
	})
	return sub, r
}

// crSecThread reports whether the column shows the thread on ch/ts.
func crSecThread(e *actShellEnv, ch, ts string) func() bool {
	return func() bool {
		s := e.a.secondary
		return s.open() && s.kind == secThread && s.channel == ch && s.ts == ts
	}
}

// crNames sends core's "names" to the App as the backend would (the
// shell's backend isn't started): what files.info's answer resumes.
func crNames(e *actShellEnv) {
	evs, unsub := e.ca.Subscribe()
	e.t.Cleanup(unsub)
	go func() {
		for ev := range evs {
			if ev.Type == "names" {
				ev := ev
				e.a.Post(func() { e.a.data.handle(e.a, ev) })
			}
		}
	}()
}

// crPersistent turns the Activity page's persistent detail panel on.
func crPersistent(e *actShellEnv) {
	off := "off"
	e.ca.SetExperimentGroups(map[string]*string{"activity_inbox_remove_persistent_detail_panel": &off, "nested_secondary_view": &off})
}

// crShot saves the window as name for the activity-shots workflow.
func crShot(t *testing.T, e *actShellEnv, name string) {
	t.Helper()
	dir := os.Getenv("ACTIVITY_SHOTS_DIR")
	if dir == "" || t.Failed() {
		return
	}
	win, err := headless.NewWindow(e.sz.X, e.sz.Y)
	if err != nil {
		t.Fatalf("headless window: %v", err)
	}
	t.Cleanup(win.Release)
	e.frames(20)
	e.until("the pictures", func() bool { return e.a.images.Pending() == 0 })
	e.shot(win, filepath.Join(dir, name))
}

// The inbox's click on a canvas mention is GMkG's toMessage in the
// column, which RedirectFileChannel makes the thread view on the mention
// (toQuipCommentThreadFromThreadIds without a comment thread): the thread
// pane on the file channel at the mention, the mention its highlighted
// root, the canvas its subtitle (SmallFileEntity: the canvas icon and the
// title), never a lock and the channel's ":shortcode:" name, and never the
// canvas itself. The subtitle links to the canvas at the thread
// (focus_thread_ts), which opens at the mention's section.
func TestActivityCanvasMentionOpensThread(t *testing.T) {
	e := newActShellPictures(t)
	cmSeed(e)
	cmOpenActivity(e)
	ch, ts := fakeslack.CanvasMentionChannel, fakeslack.CanvasMentionTS
	e.activity().StepItem(1, false) // the newest row: the canvas mention
	e.until("the thread view on the mention in the column", crSecThread(e, ch, ts))
	if k := e.activity().OpenedItem(); k != fakeslack.CanvasMentionKey {
		t.Errorf("opened %q, want the canvas mention %s", k, fakeslack.CanvasMentionKey)
	}
	if r := e.a.router.Current(); r.Channel() == ch {
		t.Errorf("the route is the file channel's: %s", r)
	}
	e.until("the mention highlighted as the thread's root", func() bool {
		tv := crThread(e, ch, ts, false)
		return tv != nil && tv.Highlighted() == ts
	})
	sub, r := crHeader(e, ch, ts, false, true)
	if sub.Icon != "canvas-content" || !strings.HasPrefix(sub.Text, "Pet roster ") {
		t.Errorf("the header names %+v, want the canvas Pet roster… (its entity, the canvas icon)", sub)
	}
	crShot(t, e, "shell-canvas-mention-thread-light.png")
	e.click(r.Min.Add(r.Size().Div(2)))
	cmScrolledTo(e, fullCanvas(e, fakeslack.CanvasMentionFile))
}

// The inbox row follows the mention's thread view in the column: closing
// the column lets the row go (SecondaryShows matches the thread view).
func TestActivityCanvasMentionThreadCloses(t *testing.T) {
	e := newActShell(t)
	cmSeed(e)
	cmOpenActivity(e)
	ch, ts := fakeslack.CanvasMentionChannel, fakeslack.CanvasMentionTS
	e.activity().StepItem(1, false)
	e.until("the thread view on the mention in the column", crSecThread(e, ch, ts))
	e.frames(3)
	if k := e.activity().OpenedItem(); k != fakeslack.CanvasMentionKey {
		t.Fatalf("opened %q, want the mention", k)
	}
	e.a.CloseSecondary()
	e.until("the row let go", func() bool { return e.activity().OpenedItem() == "" })
}

// Every direct toMessage into a canvas's file channel is that thread
// view: LaterLabel's View to-dos in the column (wRj5's R9), Later's menu's
// Open in Home (e6's R9 with forceTab Home) and the split view's
// c/<C>/m/<ts>, where the thread replaces nothing of the split view's back
// stack. The canvas opens in none.
func TestCanvasMentionMessageOpensThread(t *testing.T) {
	ch, ts := fakeslack.CanvasMentionChannel, fakeslack.CanvasMentionTS
	t.Run("label", func(t *testing.T) {
		e := newActShell(t)
		cmSeed(e)
		e.a.ViewTodo(ch, ts, "")
		e.until("the thread view on the mention beside Later", crSecThread(e, ch, ts))
		if p := e.a.router.Current().Page(); p != "later" {
			t.Errorf("the page is %q, want later", p)
		}
		if e.a.secondary.at != ts {
			t.Errorf("the thread opened at %q, want the mention %s", e.a.secondary.at, ts)
		}
	})
	t.Run("home", func(t *testing.T) {
		e := newActShell(t)
		cmSeed(e)
		e.a.Navigate("later")
		e.frames(3)
		if !e.a.OpenSavedInHome(ch, ts) {
			t.Fatalf("Open in Home didn't take the file channel's message")
		}
		e.until("the thread view on the mention", crSecThread(e, ch, ts))
		if r := e.a.router.Current(); r.Page() == "later" || r.Channel() == ch {
			t.Errorf("the main view is %q, want the Home tab's", r)
		}
		if cv, ok := e.a.secondary.view.(*canvasview.View); ok {
			t.Errorf("the column shows canvas %s, want the thread view", cv.ID())
		}
	})
	t.Run("split", func(t *testing.T) {
		e := newActShell(t)
		cmSeed(e)
		e.a.Navigate("activity")
		e.frames(3)
		e.a.OpenSplit("c/" + fakeslack.ViewsChannel)
		e.frames(3)
		e.a.inPane(paneSplit, func() { e.a.OpenSplit("c/" + ch + "/m/" + ts) })
		e.frames(5)
		want := Route("c/" + ch + "/t/" + ts)
		if st := e.a.split.stack; len(st) != 2 || st[0] != Route("c/"+fakeslack.ViewsChannel) || st[1] != want {
			t.Errorf("the split view holds %v, want [c/%s %s]", st, fakeslack.ViewsChannel, want)
		}
		if cv, ok := e.a.secondary.view.(*canvasview.View); ok && e.a.secondary.open() {
			t.Errorf("the side panel opened canvas %s", cv.ID())
		}
		e.until("the mention highlighted in the split view's thread", func() bool {
			tv := crThread(e, ch, ts, true)
			return tv != nil && tv.Highlighted() == ts
		})
	})
}

// A To-dos row on a saved canvas mention is Slack's jumpToSavedMessage
// (vh7T → jumpToMessage, isSpaceship), not a toMessage: the canvas beside
// the list (getTargetContainer: Secondary on the Later tab), at the
// mention's section, never the thread view or the file channel's
// messages, the route staying To-dos.
func TestLaterRowOpensSavedCanvasMention(t *testing.T) {
	e := newActShell(t)
	cmSeed(e)
	for _, x := range []string{"later_todos", "nested_secondary_view", "secondary_view_default_width"} {
		e.ca.SetExperimentGroup(x, "on")
	}
	now := time.Now().Unix()
	items := []map[string]any{{"item_id": fakeslack.CanvasMentionChannel, "item_type": "message", "ts": fakeslack.CanvasMentionTS,
		"todo_state": "to_do", "date_created": now - 100}}
	e.fs.SetHook(func(method string, f url.Values) (any, bool) {
		if method != "saved.list" {
			return nil, false
		}
		if !strings.HasPrefix(f.Get("filter"), "todo") {
			return map[string]any{"ok": true, "saved_items": []any{}}, true
		}
		return map[string]any{"ok": true, "saved_items": items, "counts": map[string]any{"todo_count": len(items)}}, true
	})
	e.a.Navigate("later")
	e.until("the to-do", func() bool {
		l, ok := e.a.views[Route("later")].(*views.Later)
		return ok && l.PeekCount() == len(items)
	})
	e.frames(10)
	side := func() *canvasview.View {
		if cv, ok := e.a.secondary.view.(*canvasview.View); ok && e.a.secondary.open() && cv.ID() == fakeslack.CanvasMentionFile {
			return cv
		}
		return nil
	}
	o := e.a.primaryOrigin
	for y := o.Y + 60; y < e.sz.Y-40 && side() == nil; y += 20 {
		e.click(image.Pt(o.X+8, y))
		if crSecThread(e, fakeslack.CanvasMentionChannel, fakeslack.CanvasMentionTS)() {
			t.Fatalf("the row opened the thread view on the mention, want the canvas")
		}
	}
	v := side()
	if v == nil {
		t.Fatalf("no row click opened the canvas beside the list (the column shows %v %s/%s)", e.a.secondary.kind, e.a.secondary.channel, e.a.secondary.ts)
	}
	if r := string(e.a.router.Current()); r != "later" {
		t.Errorf("the route is %q, want later", r)
	}
	e.until("the mention's section focused", func() bool {
		pending, at := v.Focus()
		return pending == fakeslack.CanvasMentionSection || at == fakeslack.CanvasMentionSection
	})
}

// A thread on a canvas's file channel names the canvas: a comment's thread
// opened from Activity's column, and the split view's even beside the
// canvas (Split never hides it). Beside the canvas in the primary view,
// any thread's header names nothing (hideChannelName). A list's file
// channel is the channel entity with the lists icon.
func TestFileChannelThreadHeader(t *testing.T) {
	e := newActShell(t)
	cmSeedComments(e)
	s := e.ca.S
	// a list's file channel named after its title, an emoji in it
	s.CustomEmoji.Put(fakeslack.CanvasMentionEmoji, e.fs.CanvasMentionEmojiURL())
	s.PutConv(slack.Conversation{ID: fakeslack.FileChanKitchen, Name: ":" + fakeslack.CanvasMentionEmoji + ": Launch tracker", IsChannel: true, IsGroup: true,
		IsPrivate: true, IsFile: true, IsMember: true, NumMembers: 3})
	s.PutCanvasRef(fakeslack.ListKitchen, richtext.CanvasRef{Title: "Launch tracker", Status: "normal", Kind: "list"})
	e.a.data.fetched[fakeslack.FileChanKitchen] = time.Now()
	ch, root := fakeslack.FileChanLaunch, fakeslack.CommentOpen

	e.a.Navigate("activity")
	e.frames(3)
	e.a.OpenThread(ch, root)
	sub, _ := crHeader(e, ch, root, false, false)
	if sub.Icon != "canvas-content" || sub.Text != "Launch plan" {
		t.Errorf("the comment thread's header names %+v, want the canvas Launch plan", sub)
	}

	e.a.OpenCanvasFull(fakeslack.CanvasChannel)
	fullCanvas(e, fakeslack.CanvasChannel)
	e.a.OpenCommentThread(fakeslack.CanvasChannel, ch, root)
	e.frames(10)
	if tv := crThread(e, ch, root, false); tv == nil {
		t.Errorf("no comment thread beside the canvas")
	} else if sub, _, ok := tv.HeaderName(); ok {
		t.Errorf("beside the canvas the header names %+v, want nothing", sub)
	}
	e.a.OpenThread(fakeslack.ViewsChannel, fakeslack.ViewsThreadUnread) // any thread beside a canvas
	e.frames(10)
	if tv := crThread(e, fakeslack.ViewsChannel, fakeslack.ViewsThreadUnread, false); tv == nil {
		t.Errorf("no channel thread beside the canvas")
	} else if sub, _, ok := tv.HeaderName(); ok {
		t.Errorf("beside the canvas a channel thread's header names %+v, want nothing", sub)
	}
	e.a.OpenSplit("c/" + ch + "/t/" + root)
	if sub, _ := crHeader(e, ch, root, true, false); sub.Icon != "canvas-content" || sub.Text != "Launch plan" {
		t.Errorf("the split view's comment thread names %+v, want the canvas", sub)
	}
	e.a.CloseSplit()

	e.a.Navigate("activity")
	e.frames(3)
	e.a.OpenThread(fakeslack.FileChanKitchen, fakeslack.ListKitchenThread)
	if sub, _ := crHeader(e, fakeslack.FileChanKitchen, fakeslack.ListKitchenThread, false, false); sub.Icon != "lists" ||
		!slices.ContainsFunc(sub.Spans, func(sp richtext.Span) bool { return sp.Kind == richtext.Emoji && sp.ID == fakeslack.CanvasMentionEmoji }) {
		t.Errorf("a list's thread names %+v, want the channel entity with the lists icon and its name's emoji", sub)
	}

	// the split view names its thread's channel even when the main view
	// shows that channel (ThreadSubtitle: never hidden in Split)
	e.a.Navigate("c/" + fakeslack.ViewsChannel)
	e.frames(3)
	e.a.OpenSplit("c/" + fakeslack.ViewsChannel + "/t/" + fakeslack.ViewsThreadUnread)
	if sub, _ := crHeader(e, fakeslack.ViewsChannel, fakeslack.ViewsThreadUnread, true, false); sub.Text == "" {
		t.Errorf("the split view's thread on the primary's channel names %+v, want the channel", sub)
	}
	e.a.CloseSplit()
}

// A canvas whose title starts with an emoji draws it as the header's
// icon (SmallFileEntity's useLeadingEmojiAsIcon), the title without it.
func TestFileChannelThreadHeaderLeadingEmoji(t *testing.T) {
	e := newActShell(t)
	cmSeedComments(e)
	url := e.fs.CanvasMentionEmojiURL()
	e.ca.S.CustomEmoji.Put(fakeslack.CanvasMentionEmoji, url)
	blocks := `[{"type":"rich_text","block_id":"title","elements":[{"type":"rich_text_section","elements":[` +
		`{"type":"emoji","name":"` + fakeslack.CanvasMentionEmoji + `"},{"type":"text","text":" Launch plan"}]}]}]`
	e.ca.S.PutCanvasRef(fakeslack.CanvasChannel, richtext.CanvasRef{Title: ":" + fakeslack.CanvasMentionEmoji + ": Launch plan",
		TitleBlocks: []byte(blocks), Status: "normal", Kind: "canvas"})
	e.a.Navigate("activity")
	e.frames(3)
	e.a.OpenThread(fakeslack.FileChanLaunch, fakeslack.CommentOpen)
	sub, _ := crHeader(e, fakeslack.FileChanLaunch, fakeslack.CommentOpen, false, false)
	if sub.Emoji != url || strings.TrimSpace(sub.Text) != "Launch plan" {
		t.Errorf("the header names %+v, want the emoji %s as its icon and the title Launch plan", sub, url)
	}
}

// Beside the Activity persistent panel's canvas (openDoc's Primary) a
// thread names nothing: ThreadSubtitle hides it beside any canvas in
// Primary.
func TestCommentThreadBesidePaneCanvas(t *testing.T) {
	e := newActShell(t)
	e.fs.AddCanvasCommentActivity()
	cmSeedComments(e)
	crPersistent(e)
	cmOpenActivity(e)
	e.activity().Search("keep it on Wednesday")
	e.frames(5)
	e.activity().StepItem(1, false)
	e.until("the canvas in the pane, the comment thread beside it", func() bool {
		return e.a.paneCanvasLive() != nil && crSecThread(e, fakeslack.FileChanLaunch, fakeslack.CommentOpen)()
	})
	e.frames(10)
	tv := crThread(e, fakeslack.FileChanLaunch, fakeslack.CommentOpen, false)
	if tv == nil {
		t.Fatalf("no comment thread view")
	}
	if sub, _, ok := tv.HeaderName(); ok {
		t.Errorf("beside the pane's canvas the header names %+v, want nothing", sub)
	}
}

// A list's file channel is never its conversation once the file is
// known: toChannel is the list (the main view's lists/<F>, the column's
// side panel), toMessage the list at the record whose thread the message
// is (its root's slack_list, read with messages.list), a jump (a link, a
// notification) the list with the record's item pane.
func TestListFileChannelOpensList(t *testing.T) {
	e := newActShell(t)
	s := e.ca.S
	ch := fakeslack.FileChanKitchen
	s.PutConv(slack.Conversation{ID: ch, Name: "Launch tracker", IsChannel: true, IsGroup: true,
		IsPrivate: true, IsFile: true, IsMember: true, NumMembers: 3})
	s.PutCanvasRef(fakeslack.ListKitchen, richtext.CanvasRef{Title: "Launch tracker", Status: "normal", Kind: "list"})
	e.a.data.fetched[ch] = time.Now()
	item := func() bool {
		it, ok := e.a.secondary.view.(*listview.Item)
		return e.a.secondary.open() && ok && it.ListID() == fakeslack.ListKitchen && it.RecordID() == fakeslack.ListKitchenRecord
	}

	e.a.Navigate("c/" + ch)
	e.until("the list's full view", func() bool { return string(e.a.router.Current()) == "lists/"+fakeslack.ListKitchen })

	e.a.Navigate("activity")
	e.frames(3)
	e.a.OpenChannelSecondary(ch)
	e.until("the list in the side panel", func() bool {
		lv, ok := e.a.secondary.view.(*listview.View)
		return e.a.secondary.open() && ok && lv.ID() == fakeslack.ListKitchen
	})
	e.a.OpenMessageSecondary(ch, fakeslack.ListKitchenThread)
	e.until("the record's item pane", item)
	if p := e.a.router.Current().Page(); p != "activity" {
		t.Errorf("the column's toMessage left Activity for %q", p)
	}

	e.a.CloseSecondary()
	e.a.Navigate("c/" + ch + "/t/" + fakeslack.ListKitchenThread)
	e.until("the list with the record's item pane", func() bool {
		return string(e.a.router.Current()) == "lists/"+fakeslack.ListKitchen && item()
	})
	for _, r := range e.a.router.stack {
		if r.Channel() == ch {
			t.Errorf("the history holds the list's file channel: %v", e.a.router.stack)
		}
	}

	// a link to a reply in the record's thread, without the thread: the
	// reply names its thread (getThreadTsToOpen), whose root names the
	// record
	e.a.CloseSecondary()
	e.a.Navigate("activity")
	e.frames(3)
	const reply = "1762500100.000100" // the kitchen list's first record's thread's first reply (fakeslack lists.go)
	e.a.Navigate("c/" + ch + "/m/" + reply)
	e.until("the record from its reply's link", func() bool {
		return string(e.a.router.Current()) == "lists/"+fakeslack.ListKitchen && item()
	})
}

// A toast's click on a canvas mention (a file channel's message without
// a thread) opens the canvas itself, unfocused: Slack's
// notificationClickedInVisitOrigin calls openDoc with no section before
// any jumpToMessage.
func TestNotificationOpensCanvasUnfocused(t *testing.T) {
	e := newActShell(t)
	cmSeed(e)
	e.a.Navigate("activity")
	e.frames(3)
	e.a.notificationClicked(fakeslack.CanvasMentionChannel, fakeslack.CanvasMentionTS, "", "")
	v := fullCanvas(e, fakeslack.CanvasMentionFile)
	e.frames(10)
	if pending, at := v.Focus(); pending != "" || at != "" {
		t.Errorf("the toast's canvas focuses %q/%q, want no section (openDoc without sectionId)", pending, at)
	}
}

// A navigation into a file channel still waiting for files.info drops
// once the user goes elsewhere: Back (whose forward history then stays),
// a thread opened meanwhile, another canvas opened meanwhile. Slack never
// waits, so nothing of it may land late.
func TestNavigationWaitDropped(t *testing.T) {
	ch := fakeslack.CanvasMentionChannel
	known := func(e *actShellEnv) func() bool {
		return func() bool {
			_, ok := e.ca.S.KnownCanvasRef(fakeslack.CanvasMentionFile)
			return ok
		}
	}
	lateEnv := func(t *testing.T) *actShellEnv {
		e := newActShell(t)
		cmSeedConv(e)
		cmSeedComments(e)
		e.fs.SetFault("files.info", fakeslack.Fault{Delay: 2 * time.Second})
		e.ca.ServeLookups()
		t.Cleanup(e.ca.StopTraffic)
		crNames(e)
		e.a.Navigate("threads")
		e.frames(2)
		e.a.Navigate("c/" + fakeslack.ViewsChannel)
		e.frames(2)
		return e
	}
	t.Run("back", func(t *testing.T) {
		e := lateEnv(t)
		e.a.Navigate("c/" + ch) // toChannel: waits for the file
		e.frames(2)
		if r := e.a.router.Current(); r != Route("c/"+fakeslack.ViewsChannel) {
			t.Fatalf("precondition: the navigation didn't wait (at %s)", r)
		}
		e.a.Back()
		e.until("files.info's answer", known(e))
		e.frames(20)
		if r := string(e.a.router.Current()); r != "threads" {
			t.Errorf("after Back the route is %q, want threads: the dropped navigation landed", r)
		}
		e.a.Forward()
		if r := e.a.router.Current(); r != Route("c/"+fakeslack.ViewsChannel) {
			t.Errorf("Forward went to %q, want the c/%s Back left: the forward history was cut", r, fakeslack.ViewsChannel)
		}
	})
	t.Run("thread", func(t *testing.T) {
		e := lateEnv(t)
		e.a.Navigate("c/" + ch + "/m/" + fakeslack.CanvasMentionTS) // a jump: waits for the file
		e.frames(2)
		e.a.OpenThread(fakeslack.ViewsChannel, fakeslack.ThreadRootTS)
		e.until("files.info's answer", known(e))
		e.frames(20)
		if r := e.a.router.Current(); canvasRouteID(r) != "" {
			t.Errorf("the dropped jump opened %s over the thread", r)
		}
	})
	t.Run("canvas", func(t *testing.T) {
		e := lateEnv(t)
		e.a.Navigate("c/" + ch + "/m/" + fakeslack.CanvasMentionTS)
		e.frames(2)
		e.a.OpenCanvas(fakeslack.CanvasChannel) // another canvas, in the side panel
		e.until("files.info's answer", known(e))
		e.frames(20)
		if r := e.a.router.Current(); canvasRouteID(r) != "" {
			t.Errorf("the dropped jump opened %s", r)
		}
		if cv, ok := e.a.secondary.view.(*canvasview.View); !ok || cv.ID() != fakeslack.CanvasChannel {
			t.Errorf("the side panel no longer shows the canvas opened meanwhile")
		}
	})
}

// A file the viewer can't read is still a known file: files.info's
// not_visible names a canvas by the error's filetype (fetchFileById's
// make_not_visible), which the redirect opens; hidden_by_limit is no
// canvas, so the conversation opens. Neither waits on the next
// navigation, nor asks files.info again.
func TestHiddenFilesDontWait(t *testing.T) {
	for _, c := range []struct {
		name string
		body map[string]any
		want string
	}{
		{"not_visible", map[string]any{"ok": false, "error": "not_visible", "filetype": "quip", "title": "A hidden plan"}, "docs/" + fakeslack.CanvasMentionFile},
		{"hidden_by_limit", map[string]any{"ok": false, "error": "hidden_by_limit"}, "c/" + fakeslack.CanvasMentionChannel},
	} {
		t.Run(c.name, func(t *testing.T) {
			e := newActShell(t)
			cmSeedConv(e)
			e.fs.SetHook(func(method string, f url.Values) (any, bool) {
				if method == "files.info" && f.Get("file") == fakeslack.CanvasMentionFile {
					return c.body, true
				}
				return nil, false
			})
			e.ca.ServeLookups()
			t.Cleanup(e.ca.StopTraffic)
			crNames(e)
			e.a.Navigate("c/" + fakeslack.ViewsChannel)
			e.frames(2)
			e.a.Navigate("c/" + fakeslack.CanvasMentionChannel)
			e.until("the first navigation's end", func() bool { return string(e.a.router.Current()) == c.want })
			if _, known := e.ca.S.KnownCanvasRef(fakeslack.CanvasMentionFile); !known {
				t.Fatalf("files.info's error left the file unknown")
			}
			e.a.Navigate("c/" + fakeslack.ViewsChannel)
			e.frames(2)
			e.a.Navigate("c/" + fakeslack.CanvasMentionChannel)
			if r := string(e.a.router.Current()); r != c.want {
				t.Errorf("the second navigation is at %q, want %q at once (the file is known)", r, c.want)
			}
			if c.name == "not_visible" {
				// a thread there names the canvas the viewer can't see:
				// SmallFileEntity's eye-closed icon
				e.a.Navigate("activity")
				e.frames(2)
				e.a.OpenThread(fakeslack.CanvasMentionChannel, fakeslack.CanvasMentionTS)
				if sub, _ := crHeader(e, fakeslack.CanvasMentionChannel, fakeslack.CanvasMentionTS, false, false); sub.Icon != "eye-closed" || sub.Text != "A hidden plan" {
					t.Errorf("the hidden canvas's thread names %+v, want the eye-closed icon and the error's title", sub)
				}
			}
		})
	}
}

// The column's toChannel into a canvas's file channel is the canvas in
// the side panel (toCanvas), and a toast's click on a reply in a list
// record's thread, while the list's files.info is out, opens the list
// with the record once it answers.
func TestFileChannelOpeners(t *testing.T) {
	t.Run("column toChannel", func(t *testing.T) {
		e := newActShell(t)
		cmSeedComments(e)
		e.a.Navigate("activity")
		e.frames(3)
		e.a.OpenChannelSecondary(fakeslack.FileChanLaunch)
		e.until("the canvas in the side panel", func() bool {
			cv, ok := e.a.secondary.view.(*canvasview.View)
			return e.a.secondary.open() && ok && cv.ID() == fakeslack.CanvasChannel
		})
	})
	t.Run("list toast", func(t *testing.T) {
		e := newActShell(t)
		ch := fakeslack.FileChanKitchen
		e.ca.S.PutConv(slack.Conversation{ID: ch, Name: "Launch tracker", IsChannel: true, IsGroup: true,
			IsPrivate: true, IsFile: true, IsMember: true, NumMembers: 3})
		e.a.data.fetched[ch] = time.Now()
		e.fs.SetFault("files.info", fakeslack.Fault{Delay: 800 * time.Millisecond})
		e.ca.ServeLookups()
		t.Cleanup(e.ca.StopTraffic)
		crNames(e)
		e.a.Navigate("activity")
		e.frames(3)
		e.a.notificationClicked(ch, "1762500100.000100", fakeslack.ListKitchenThread, "")
		e.frames(2)
		if r := e.a.router.Current(); r.Channel() == ch {
			t.Fatalf("the toast opened the list's file channel (%s) before files.info answered", r)
		}
		e.until("the list with the record", func() bool {
			it, ok := e.a.secondary.view.(*listview.Item)
			return string(e.a.router.Current()) == "lists/"+fakeslack.ListKitchen && ok && it.RecordID() == fakeslack.ListKitchenRecord
		})
	})
}

// A jump clicked inside the split view stays there (getTargetContainer:
// shouldForceOpenInSplitView for a Split source): a comment link's
// thread goes on the split view's back stack, through Navigate or
// jumpToMessage, and a mention link's canvas goes in the side panel at
// the mention's section, Clack's split view holding conversations only.
// The split modifier click on a file channel's link is the plain jump
// (openPermalinkInClient's "message" case drops shouldOpenInSplitView).
func TestFileLinkInSplitStaysInSplit(t *testing.T) {
	base := Route("c/" + fakeslack.ViewsChannel)
	comment := Route("c/" + fakeslack.FileChanLaunch + "/t/" + fakeslack.CommentUnread)
	splitEnv := func(t *testing.T) *actShellEnv {
		e := newActShell(t)
		cmSeed(e)
		cmSeedComments(e)
		e.a.Navigate("activity")
		e.frames(3)
		e.a.OpenSplit(string(base))
		e.frames(3)
		return e
	}
	onStack := func(e *actShellEnv) func() bool {
		return func() bool {
			st := e.a.split.stack
			return len(st) == 2 && st[0] == base && st[1] == comment
		}
	}
	noCanvas := func(t *testing.T, e *actShellEnv) {
		t.Helper()
		if r := e.a.router.Current(); canvasRouteID(r) != "" {
			t.Errorf("the main view went to the canvas %s", r)
		}
	}
	t.Run("comment link", func(t *testing.T) {
		e := splitEnv(t)
		e.a.inPane(paneSplit, func() { e.a.Navigate(string(comment)) })
		e.until("the comment thread on the split view's stack", onStack(e))
		e.frames(5)
		noCanvas(t, e)
	})
	t.Run("comment jump", func(t *testing.T) {
		e := splitEnv(t)
		e.a.inPane(paneSplit, func() {
			e.a.jumpToMessage(fakeslack.FileChanLaunch, fakeslack.CommentUnread, fakeslack.CommentUnread)
		})
		e.until("the comment thread on the split view's stack", onStack(e))
		e.frames(5)
		noCanvas(t, e)
	})
	t.Run("mention link", func(t *testing.T) {
		e := splitEnv(t)
		e.a.inPane(paneSplit, func() {
			e.a.Navigate("c/" + fakeslack.CanvasMentionChannel + "/m/" + fakeslack.CanvasMentionTS)
		})
		var v *canvasview.View
		e.until("the canvas in the side panel", func() bool {
			cv, ok := e.a.secondary.view.(*canvasview.View)
			v = cv
			return e.a.secondary.open() && ok && cv.ID() == fakeslack.CanvasMentionFile
		})
		cmScrolledTo(e, v)
		if st := e.a.split.stack; len(st) != 1 || st[0] != base {
			t.Errorf("the split view holds %v, want its own %s", st, base)
		}
		noCanvas(t, e)
	})
	t.Run("split click", func(t *testing.T) {
		e := splitEnv(t)
		e.a.WithClickMods(shortcut.Mod()|key.ModAlt, func() { e.a.Navigate(string(comment)) })
		fullCanvas(e, fakeslack.CanvasChannel)
		e.until("the comment thread in the column", crSecThread(e, fakeslack.FileChanLaunch, fakeslack.CommentUnread))
		if st := e.a.split.stack; len(st) != 1 || st[0] != base {
			t.Errorf("the split click put %v on the split view, want the plain jump", st)
		}
	})
}

// A split view navigation that waits for files.info lands on the split
// view's back stack, whichever way it ends: the conversation when
// files.info never answers, the thread view on the message when it does.
// A split view closed meanwhile drops it: nothing reopens it, nothing
// opens elsewhere.
func TestSplitFileWaitKeepsBackStack(t *testing.T) {
	base := Route("c/" + fakeslack.ViewsChannel)
	splitEnv := func(t *testing.T, fault fakeslack.Fault) *actShellEnv {
		e := newActShell(t)
		cmSeedConv(e)
		e.fs.SetFault("files.info", fault)
		e.ca.ServeLookups()
		t.Cleanup(e.ca.StopTraffic)
		crNames(e)
		e.a.Navigate("activity")
		e.frames(3)
		e.a.OpenSplit(string(base))
		e.frames(3)
		return e
	}
	top := func(e *actShellEnv) Route {
		if n := len(e.a.split.stack); n > 0 {
			return e.a.split.stack[n-1]
		}
		return ""
	}
	t.Run("never answers", func(t *testing.T) {
		e := splitEnv(t, fakeslack.Fault{HangUp: true})
		route := Route("c/" + fakeslack.CanvasMentionChannel)
		e.a.inPane(paneSplit, func() { e.a.OpenSplit(string(route)) })
		e.frames(2)
		if top(e) != base {
			t.Fatalf("the split view moved on before files.info: %v", e.a.split.stack)
		}
		deadline := time.Now().Add(canvasWaitLimit + 3*time.Second)
		for top(e) != route {
			if time.Now().After(deadline) {
				t.Fatalf("the split view holds %v after the wait, want %s on top", e.a.split.stack, route)
			}
			e.frame()
			time.Sleep(20 * time.Millisecond)
		}
		if st := e.a.split.stack; len(st) != 2 || st[0] != base {
			t.Errorf("the split view holds %v: the late conversation replaced its back stack", st)
		}
	})
	t.Run("answers late", func(t *testing.T) {
		e := splitEnv(t, fakeslack.Fault{Delay: 600 * time.Millisecond})
		ch, ts := fakeslack.CanvasMentionChannel, fakeslack.CanvasMentionTS
		e.a.inPane(paneSplit, func() { e.a.OpenSplit("c/" + ch + "/m/" + ts) })
		want := Route("c/" + ch + "/t/" + ts)
		e.until("the thread view on the split view's stack", func() bool { return top(e) == want })
		if st := e.a.split.stack; len(st) != 2 || st[0] != base {
			t.Errorf("the split view holds %v: the late thread view replaced its back stack", st)
		}
	})
	t.Run("closed meanwhile", func(t *testing.T) {
		e := splitEnv(t, fakeslack.Fault{Delay: 600 * time.Millisecond})
		e.a.inPane(paneSplit, func() { e.a.OpenSplit("c/" + fakeslack.CanvasMentionChannel) })
		e.frames(2)
		e.a.CloseSplit()
		e.until("files.info's answer", func() bool {
			_, known := e.ca.S.KnownCanvasRef(fakeslack.CanvasMentionFile)
			return known
		})
		e.frames(20)
		if st := e.a.split.stack; len(st) != 0 {
			t.Errorf("the closed split view reopened on %v", st)
		}
		if cv, ok := e.a.secondary.view.(*canvasview.View); ok && e.a.secondary.open() {
			t.Errorf("the dropped navigation opened canvas %s in the side panel", cv.ID())
		}
	})
}

// crCommentReply opens the comment reply's item (AddCanvasCommentActivity:
// a reply in CommentOpen that mentions the viewer).
func crCommentReply(e *actShellEnv) {
	e.t.Helper()
	e.activity().StepItem(1, false)
	e.until("the reply opened", func() bool { return e.activity().OpenedItem() == fakeslack.CanvasCommentKey })
}

// crLateCanvas is the Activity page with the persistent panel over the
// canvas comment items (and with lists the list comment's), filtered to
// the reply, with the Launch plan canvas's file channel known and its
// files.info held until release: Activity hydrates every item's message
// and thread root (core activityItems), so what a pane jump waits on is
// the file. The hold starts when the rows ask, the jump's wait at the
// click, so the test decides when the wait ends.
func crLateCanvas(t *testing.T, lists bool) (e *actShellEnv, release func()) {
	e = newActShell(t)
	e.fs.AddCanvasCommentActivity()
	e.ca.S.PutConv(slack.Conversation{ID: fakeslack.FileChanLaunch, Name: "Launch plan", IsChannel: true, IsGroup: true,
		IsPrivate: true, IsFile: true, IsMember: true, NumMembers: 4})
	e.a.data.fetched[fakeslack.FileChanLaunch] = time.Now()
	if lists {
		e.fs.AddListCommentActivity()
		e.ca.S.PutConv(slack.Conversation{ID: fakeslack.FileChanKitchen, Name: "Launch tracker", IsChannel: true, IsGroup: true,
			IsPrivate: true, IsFile: true, IsMember: true, NumMembers: 3})
		e.ca.S.PutCanvasRef(fakeslack.ListKitchen, richtext.CanvasRef{Title: "Launch tracker", Status: "normal", Kind: "list"})
		e.a.data.fetched[fakeslack.FileChanKitchen] = time.Now()
	}
	hold := make(chan struct{})
	var once sync.Once
	release = func() { once.Do(func() { close(hold) }) }
	t.Cleanup(release)
	e.fs.SetHook(func(method string, f url.Values) (any, bool) {
		if method == "files.info" && f.Get("file") == fakeslack.CanvasChannel {
			<-hold
		}
		return nil, false
	})
	e.ca.ServeLookups()
	t.Cleanup(e.ca.StopTraffic)
	crNames(e)
	crPersistent(e)
	cmOpenActivity(e)
	e.activity().Search("keep it on Wednesday")
	e.frames(5)
	return e, release
}

// crCanvasKnown reports whether the held files.info has answered.
func crCanvasKnown(e *actShellEnv) bool {
	_, known := e.ca.S.KnownCanvasRef(fakeslack.CanvasChannel)
	return known
}

// With the persistent detail panel, a click whose jump waits for the
// canvas's files.info draws no canvas until files.info answers, and every
// frame meanwhile the pane keeps what Primary showed, as Slack's openDoc
// navigates Primary only once it has run: for a first click the empty
// view, never the item's preview card ("View thread" for the reply).
// Then the canvas, with the comment thread beside it.
func TestActivityPaneKeepsViewWhileJumpResolves(t *testing.T) {
	e, release := crLateCanvas(t, false)
	crCommentReply(e)
	if crCanvasKnown(e) {
		t.Fatalf("precondition: files.info answered before the click")
	}
	if _, waits := e.a.canvasJumpsOf().waiting[fakeslack.CanvasChannel]; !waits {
		t.Fatalf("the jump doesn't wait for the canvas's files.info")
	}
	for i := 0; i < 25; i++ {
		drawn := cmLabels(e)
		if drawn["View thread"] || drawn["Open in channel"] {
			t.Fatalf("frame %d: the pane drew the item's preview while its jump resolved", i)
		}
		if !drawn["Select a notification to view the details."] {
			t.Fatalf("frame %d: the pane didn't keep the empty view while the jump resolved", i)
		}
		if e.a.paneCanvasLive() != nil {
			t.Fatalf("frame %d: the pane drew the canvas before files.info answered", i)
		}
		time.Sleep(20 * time.Millisecond)
	}
	release()
	e.until("the canvas in the pane", func() bool {
		v := e.a.canvasJumpsOf().pane
		return v != nil && v.ID() == fakeslack.CanvasChannel && e.a.paneCanvasLive() != nil
	})
	if f, done := e.a.PaneFile(); f != fakeslack.CanvasChannel || !done {
		t.Errorf("the pane's file %q (done %v), want the canvas", f, done)
	}
	e.until("the comment thread beside it", crSecThread(e, fakeslack.FileChanLaunch, fakeslack.CommentOpen))
}

// crNothingLands checks, once files.info has answered, that a dropped
// pane jump opened nothing: no canvas in the pane, no comment thread.
func crNothingLands(t *testing.T, e *actShellEnv, release func(), why string) {
	t.Helper()
	release()
	e.until("files.info's answer", func() bool { return crCanvasKnown(e) })
	for i := 0; i < 30; i++ {
		e.frame()
		if v := e.a.paneCanvasLive(); v != nil && v.ID() == fakeslack.CanvasChannel {
			t.Fatalf("%s: the dropped jump's canvas opened in the pane", why)
		}
		time.Sleep(15 * time.Millisecond)
	}
	if crSecThread(e, fakeslack.FileChanLaunch, fakeslack.CommentOpen)() {
		t.Errorf("%s: the dropped jump's comment thread opened", why)
	}
}

// A pane jump still waiting drops once another item is opened (here the
// comment's thread_v2, which opens its thread in the pane), or once the
// item is let go without opening anything (an Activity tab switch: no
// navigation, so the jump's still() is what drops it).
func TestActivityPaneJumpDropped(t *testing.T) {
	t.Run("another item", func(t *testing.T) {
		e, release := crLateCanvas(t, false)
		crCommentReply(e)
		e.activity().Search("Booked it") // the comment's thread_v2, meanwhile
		e.frames(5)
		e.activity().StepItem(1, false)
		if next := e.activity().OpenedItem(); next != fakeslack.CanvasCommentThreadKey {
			t.Fatalf("precondition: opened %q, want the thread_v2 %s", next, fakeslack.CanvasCommentThreadKey)
		}
		if crCanvasKnown(e) {
			t.Fatalf("precondition: files.info answered before the second click")
		}
		crNothingLands(t, e, release, "another item")
		if k := e.activity().OpenedItem(); k != fakeslack.CanvasCommentThreadKey {
			t.Errorf("opened %q, want the thread_v2 still", k)
		}
	})
	t.Run("tab switch", func(t *testing.T) {
		e, release := crLateCanvas(t, false)
		crCommentReply(e)
		gen := e.a.canvasJumpsOf().gen
		e.activity().ShowTab(1)
		e.frames(3)
		if k := e.activity().OpenedItem(); k != "" {
			t.Fatalf("precondition: the tab switch left %q open", k)
		}
		if g := e.a.canvasJumpsOf().gen; g != gen {
			t.Fatalf("precondition: the tab switch navigated (gen %d → %d); still() is what this checks", gen, g)
		}
		crNothingLands(t, e, release, "tab switch")
	})
}

// While a pane jump waits, a pane that last drew a list record keeps
// drawing that record: no canvas viewer is made on the list's file id.
func TestActivityPaneKeepsListWhileJumpWaits(t *testing.T) {
	e, release := crLateCanvas(t, true)
	e.activity().Search("can you add the screenshots")
	e.frames(5)
	e.activity().StepItem(1, false)
	e.until("the list record in the pane", func() bool {
		st := e.a.canvasJumpsOf()
		return st.paneKind == "list" && st.paneFile == fakeslack.ListKitchen && st.paneRec == fakeslack.ListKitchenRecord && st.list != nil
	})
	e.frames(5)
	e.activity().Search("keep it on Wednesday")
	e.frames(5)
	e.activity().StepItem(1, false)
	if k := e.activity().OpenedItem(); k != fakeslack.CanvasCommentKey {
		t.Fatalf("precondition: opened %q, want the canvas comment's reply", k)
	}
	if crCanvasKnown(e) {
		t.Fatalf("precondition: files.info answered before the click")
	}
	for i := 0; i < 20; i++ {
		e.frame()
		if v := e.a.canvasJumpsOf().pane; v != nil && v.ID() == fakeslack.ListKitchen {
			t.Fatalf("frame %d: the pane made a canvas viewer on the list %s", i, fakeslack.ListKitchen)
		}
		time.Sleep(15 * time.Millisecond)
	}
	release()
	e.until("the canvas in the pane", func() bool {
		v := e.a.paneCanvasLive()
		return v != nil && v.ID() == fakeslack.CanvasChannel
	})
}
