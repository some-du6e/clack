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
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"gioui.org/gpu/headless"

	"github.com/some-du6e/clack/internal/fakeslack"
	"github.com/some-du6e/clack/internal/richtext"
	"github.com/some-du6e/clack/internal/slack"
	"github.com/some-du6e/clack/internal/ui/canvasview"
	"github.com/some-du6e/clack/internal/ui/listview"
	"github.com/some-du6e/clack/internal/ui/messages"
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

// Every direct toMessage into a canvas's file channel is that thread
// view: Later's View message in the column, and the split view's
// c/<C>/m/<ts>, where the thread replaces nothing of the split view's back
// stack. The canvas opens in neither.
func TestCanvasMentionMessageOpensThread(t *testing.T) {
	ch, ts := fakeslack.CanvasMentionChannel, fakeslack.CanvasMentionTS
	t.Run("later", func(t *testing.T) {
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

// A thread on a canvas's file channel names the canvas: a comment's thread
// opened from Activity's column, and the split view's even beside the
// canvas (Split never hides it). Beside the canvas in the primary view,
// any thread's header names nothing (hideChannelName). A list's file
// channel is the channel entity with the lists icon.
func TestFileChannelThreadHeader(t *testing.T) {
	e := newActShell(t)
	cmSeedComments(e)
	s := e.ca.S
	s.PutConv(slack.Conversation{ID: fakeslack.FileChanKitchen, Name: "Launch tracker", IsChannel: true, IsGroup: true,
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
	e.a.OpenThread(fakeslack.ViewsChannel, fakeslack.ThreadRootTS) // any thread beside a canvas
	e.frames(10)
	if tv := crThread(e, fakeslack.ViewsChannel, fakeslack.ThreadRootTS, false); tv != nil {
		if sub, _, ok := tv.HeaderName(); ok {
			t.Errorf("beside the canvas a channel thread's header names %+v, want nothing", sub)
		}
	}
	e.a.OpenSplit("c/" + ch + "/t/" + root)
	if sub, _ := crHeader(e, ch, root, true, false); sub.Icon != "canvas-content" || sub.Text != "Launch plan" {
		t.Errorf("the split view's comment thread names %+v, want the canvas", sub)
	}
	e.a.CloseSplit()

	e.a.Navigate("activity")
	e.frames(3)
	e.a.OpenThread(fakeslack.FileChanKitchen, fakeslack.ListKitchenThread)
	if sub, _ := crHeader(e, fakeslack.FileChanKitchen, fakeslack.ListKitchenThread, false, false); sub.Icon != "lists" {
		t.Errorf("a list's thread names %+v, want the channel entity with the lists icon", sub)
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
}

// A comment link clicked inside the split view is jumpToMessage, which
// opens the canvas with the comment thread wherever it starts
// (openDocWithThreadTs has no split routing): the canvas's full view and
// the thread in the column, the split view keeping its own view. Only the
// split modifier click opens the thread in the split view.
func TestCanvasCommentLinkInSplitOpensCanvas(t *testing.T) {
	e := newActShell(t)
	cmSeedComments(e)
	e.a.Navigate("activity")
	e.frames(3)
	e.a.OpenSplit("c/" + fakeslack.ViewsChannel)
	e.frames(3)
	route := "c/" + fakeslack.FileChanLaunch + "/t/" + fakeslack.CommentUnread
	e.a.inPane(paneSplit, func() { e.a.Navigate(route) })
	fullCanvas(e, fakeslack.CanvasChannel)
	e.until("the comment thread in the column", crSecThread(e, fakeslack.FileChanLaunch, fakeslack.CommentUnread))
	if st := e.a.split.stack; len(st) != 1 || st[0] != Route("c/"+fakeslack.ViewsChannel) {
		t.Errorf("the split view holds %v, want its own c/%s", st, fakeslack.ViewsChannel)
	}
}

// A split view navigation that waits for files.info and ends in the
// conversation (no file, as files.info never answers) still goes on the
// split view's back stack: Back returns to the view it started from.
func TestSplitFileWaitKeepsBackStack(t *testing.T) {
	e := newActShell(t)
	cmSeedConv(e)
	e.fs.SetFault("files.info", fakeslack.Fault{HangUp: true})
	e.ca.ServeLookups()
	t.Cleanup(e.ca.StopTraffic)
	e.a.Navigate("activity")
	e.frames(3)
	e.a.OpenSplit("c/" + fakeslack.ViewsChannel)
	e.frames(3)
	route := Route("c/" + fakeslack.CanvasMentionChannel)
	e.a.inPane(paneSplit, func() { e.a.OpenSplit(string(route)) })
	e.frames(2)
	if n := len(e.a.split.stack); n != 1 {
		t.Fatalf("the split view moved on before files.info: %v", e.a.split.stack)
	}
	deadline := time.Now().Add(canvasWaitLimit + 3*time.Second)
	for len(e.a.split.stack) == 1 {
		if time.Now().After(deadline) {
			t.Fatalf("the split view still holds %v after the wait", e.a.split.stack)
		}
		e.frame()
		time.Sleep(20 * time.Millisecond)
	}
	if st := e.a.split.stack; len(st) != 2 || st[0] != Route("c/"+fakeslack.ViewsChannel) || st[1] != route {
		t.Errorf("the split view holds %v, want [c/%s %s]", st, fakeslack.ViewsChannel, route)
	}
}

// crCommentReply opens the comment reply's item (AddCanvasCommentActivity:
// a reply in CommentOpen that mentions the viewer), the jump reading its
// thread's root, which the store doesn't hold.
func crCommentReply(e *actShellEnv) {
	e.t.Helper()
	if _, held := e.ca.S.FindHeld(fakeslack.FileChanLaunch, fakeslack.CommentOpen); held {
		e.t.Fatalf("precondition: the store holds the comment's root already")
	}
	e.activity().StepItem(1, false)
	e.until("the reply opened", func() bool { return e.activity().OpenedItem() == fakeslack.CanvasCommentKey })
}

// crCommentsActivity is the Activity page with the persistent panel over
// the canvas comment items, filtered to the reply.
func crCommentsActivity(t *testing.T, seed func(e *actShellEnv)) *actShellEnv {
	e := newActShell(t)
	e.fs.AddCanvasCommentActivity()
	seed(e)
	crPersistent(e)
	cmOpenActivity(e)
	e.activity().Search("keep it on Wednesday")
	e.frames(5)
	return e
}

// With the persistent detail panel, while a click's jump resolves (the
// comment's root still on its way) the pane keeps what Primary showed, as
// Slack's openDoc navigates Primary only once the root is in: for a first
// click the empty view, never the item's preview card. Then the canvas,
// with the comment thread beside it.
func TestActivityPaneKeepsViewWhileJumpResolves(t *testing.T) {
	e := crCommentsActivity(t, cmSeedComments)
	e.fs.SetFault("messages.list", fakeslack.Fault{Delay: 800 * time.Millisecond})
	crCommentReply(e)
	until := time.Now().Add(400 * time.Millisecond)
	empty := false
	for time.Now().Before(until) {
		drawn := cmLabels(e)
		if drawn["Open in channel"] {
			t.Fatalf("the pane drew the item's preview while its jump resolved")
		}
		empty = empty || drawn["Select a notification to view the details."]
		time.Sleep(20 * time.Millisecond)
	}
	if !empty {
		t.Errorf("the pane didn't keep the empty view while the jump resolved")
	}
	e.until("the canvas in the pane", func() bool {
		v := e.a.canvasJumpsOf().pane
		return v != nil && v.ID() == fakeslack.CanvasChannel && e.a.paneCanvasLive() != nil
	})
	e.until("the comment thread beside it", crSecThread(e, fakeslack.FileChanLaunch, fakeslack.CommentOpen))
}

// A pane jump still out drops once another item is opened: the canvas
// never replaces the newer item's view, and its comment thread never
// opens.
func TestActivityPaneJumpDroppedByAnotherItem(t *testing.T) {
	e := crCommentsActivity(t, cmSeedComments)
	e.fs.SetFault("messages.list", fakeslack.Fault{Delay: 800 * time.Millisecond})
	crCommentReply(e)
	e.frames(2)
	e.activity().Search("")
	e.frames(5)
	e.activity().StepItem(-1, false) // a newer item, meanwhile
	next := e.activity().OpenedItem()
	if next == fakeslack.CanvasCommentKey || next == "" {
		t.Fatalf("precondition: opened %q, want another item", next)
	}
	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		e.frame()
		time.Sleep(20 * time.Millisecond)
	}
	if f, _ := e.a.PaneFile(); f == fakeslack.CanvasChannel {
		t.Errorf("the reply's canvas opened in the pane over the next item %s", next)
	}
	if crSecThread(e, fakeslack.FileChanLaunch, fakeslack.CommentOpen)() {
		t.Errorf("the reply's comment thread opened over the next item %s", next)
	}
	if k := e.activity().OpenedItem(); k != next {
		t.Errorf("opened %q, want the next item %q", k, next)
	}
}

// With the persistent panel, an item of a canvas's file channel whose
// files.info hasn't answered waits for it: no canvas in the pane before
// it does, then the canvas with the comment thread beside it.
func TestActivityPaneWaitsForFilesInfo(t *testing.T) {
	e := crCommentsActivity(t, func(e *actShellEnv) {
		e.ca.S.PutConv(slack.Conversation{ID: fakeslack.FileChanLaunch, Name: "Launch plan", IsChannel: true, IsGroup: true,
			IsPrivate: true, IsFile: true, IsMember: true, NumMembers: 4})
		e.a.data.fetched[fakeslack.FileChanLaunch] = time.Now()
		e.fs.SetFault("files.info", fakeslack.Fault{Delay: 2 * time.Second})
		e.ca.ServeLookups()
		t.Cleanup(e.ca.StopTraffic)
		crNames(e)
	})
	crCommentReply(e)
	if _, known := e.ca.S.KnownCanvasRef(fakeslack.CanvasChannel); known {
		t.Fatalf("precondition: files.info answered before the click")
	}
	if _, waits := e.a.canvasJumpsOf().waiting[fakeslack.CanvasChannel]; !waits {
		t.Errorf("the jump doesn't wait for the canvas's files.info")
	}
	for {
		e.frame()
		_, known := e.ca.S.KnownCanvasRef(fakeslack.CanvasChannel)
		if known {
			break
		}
		if e.a.paneCanvasLive() != nil {
			t.Fatalf("the pane drew the canvas before files.info answered")
		}
		time.Sleep(20 * time.Millisecond)
	}
	e.until("the canvas in the pane", func() bool {
		v := e.a.canvasJumpsOf().pane
		return v != nil && v.ID() == fakeslack.CanvasChannel && e.a.paneCanvasLive() != nil
	})
	e.until("the comment thread beside it", crSecThread(e, fakeslack.FileChanLaunch, fakeslack.CommentOpen))
}
