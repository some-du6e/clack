package ui

// Canvas mentions open the canvas, never the canvas's hidden file channel
// (Karim, α.102: a click "put me in a limbo esque channel where the canvas
// contents are messages??"), and canvas titles draw their emoji (the
// channel's canvas tab read "clack testing :cat-slackbot:"). Slack's
// jumpToMessage opens a canvas's file channel message as the canvas at the
// mention's section (with a comment's thread beside it); its tab label
// renders title_blocks from the emoji store each time. internal/fakeslack,
// made-up content only.

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"gioui.org/f32"
	"gioui.org/gpu/headless"
	"gioui.org/io/key"
	"gioui.org/io/pointer"

	"github.com/some-du6e/clack/internal/fakeslack"
	"github.com/some-du6e/clack/internal/richtext"
	"github.com/some-du6e/clack/internal/slack"
	"github.com/some-du6e/clack/internal/ui/canvasview"
	"github.com/some-du6e/clack/internal/ui/shortcut"
	"github.com/some-du6e/clack/internal/ui/theme"
)

// cmSeedConv gives the shell's store the mention's file channel as the
// private conversation it is, and the title's emoji, without the
// canvas's files.info.
func cmSeedConv(e *actShellEnv) {
	e.fs.AddCanvasMention()
	s := e.ca.S
	s.PutConv(slack.Conversation{ID: fakeslack.CanvasMentionChannel, Name: fakeslack.CanvasMentionTitle, IsChannel: true, IsGroup: true,
		IsPrivate: true, IsFile: true, IsMember: true, NumMembers: 2})
	s.CustomEmoji.Put(fakeslack.CanvasMentionEmoji, e.fs.CanvasMentionEmojiURL())
	e.a.data.fetched[fakeslack.CanvasMentionChannel] = time.Now() // no header reads for it
}

// cmSeed is cmSeedConv with the canvas's files.info, as core has it once
// the mention's message arrived.
func cmSeed(e *actShellEnv) {
	cmSeedConv(e)
	blocks, _ := json.Marshal(fakeslack.CanvasMentionTitleBlocks())
	e.ca.S.PutCanvasRef(fakeslack.CanvasMentionFile, richtext.CanvasRef{Title: fakeslack.CanvasMentionTitle, TitleBlocks: blocks,
		Status: "normal", Kind: "canvas"})
}

// newActShellPictures is newActShell drawing the fake's pictures
// (avatars, the title's emoji), for a shot.
func newActShellPictures(t *testing.T) *actShellEnv {
	t.Helper()
	sz := fakeslack.Small
	sz.Views = true
	fs, err := fakeslack.Start(sz)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = fs.Close() })
	return newActShellOn(t, fs, fs.Fetch)
}

// cmOpenActivity shows the Activity page with its feed loaded.
func cmOpenActivity(e *actShellEnv) {
	e.a.Navigate("activity")
	e.until("the activity view", func() bool { return e.activity() != nil })
	e.until("the activity feed", func() bool { return e.fs.CallCount("activity.feed") > 0 })
	e.frames(20)
}

// cmShot saves the window as name (ACTIVITY_SHOTS_DIR, the activity-shots
// workflow) once the canvas v has loaded and the pictures are in.
func cmShot(t *testing.T, e *actShellEnv, v *canvasview.View, name string) {
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
	e.until("the canvas's content", func() bool { _, known := v.Empty(); return known })
	e.frames(20)
	e.until("the pictures", func() bool { return e.a.images.Pending() == 0 })
	e.shot(win, filepath.Join(dir, name))
}

// cmScrolledTo waits until canvas v scrolled to the mention's section.
func cmScrolledTo(e *actShellEnv, v *canvasview.View) {
	e.t.Helper()
	e.until("the mention's section scrolled into view", func() bool {
		_, at := v.Focus()
		return at == fakeslack.CanvasMentionSection
	})
}

// cmLabels is every label the next frame draws.
func cmLabels(e *actShellEnv) map[string]bool {
	var mu sync.Mutex
	set := map[string]bool{}
	theme.DrawHook = func(s string) { mu.Lock(); set[s] = true; mu.Unlock() }
	e.frame()
	theme.DrawHook = nil
	mu.Lock()
	defer mu.Unlock()
	return set
}

// With the persistent detail panel the mention's click opens the canvas
// in the Activity view's own pane, at the mention's section. The inbox
// stays beside it: Slack's openDoc replaces Primary, and the inbox sits
// in the Sidebar.
func TestActivityCanvasMentionPersistentPane(t *testing.T) {
	e := newActShellPictures(t)
	cmSeed(e)
	off := "off"
	e.ca.SetExperimentGroups(map[string]*string{"activity_inbox_remove_persistent_detail_panel": &off, "nested_secondary_view": &off})
	cmOpenActivity(e)
	e.activity().StepItem(1, false)
	var v *canvasview.View
	e.until("the canvas in the Activity pane", func() bool {
		v = e.a.canvasJumpsOf().pane
		return v != nil && v.ID() == fakeslack.CanvasMentionFile
	})
	if r := string(e.a.router.Current()); r != "activity" {
		t.Errorf("the click left Activity for %q", r)
	}
	cmScrolledTo(e, v) // the pane draws it: the scroll happens in its layout
	// the fixture's reminder rows, which neither the rail nor the canvas draws
	if drawn := cmLabels(e); !drawn["Reminder"] {
		t.Errorf("the inbox's rows aren't drawn beside the canvas")
	}
	if e.a.secondary.open() && e.a.secondary.channel == fakeslack.CanvasMentionChannel {
		t.Errorf("the column opened the canvas's file channel")
	}
	cmShot(t, e, v, "shell-canvas-mention-pane-light.png")
}

// fullCanvas waits for canvas file's full view and returns it.
func fullCanvas(e *actShellEnv, file string) *canvasview.View {
	e.t.Helper()
	var v *canvasview.View
	e.until("the canvas's full view", func() bool {
		cv, ok := e.a.views[Route("docs/"+file)].(*canvasview.View)
		v = cv
		return string(e.a.router.Current()) == "docs/"+file && ok
	})
	return v
}

// A jump to the mention's message (a link, a search result, a
// notification: c/<file channel>/m/<ts>) opens the canvas in the full
// view at the mention's section; the file channel's route never shows. A
// thread link at the mention reads the same message (openDocWithThreadTs
// reads the message at the thread): its section, no comment thread.
func TestCanvasMentionMessageJumpOpensCanvas(t *testing.T) {
	e := newActShell(t)
	cmSeed(e)
	e.a.Navigate("activity")
	e.frames(5)
	ch, ts := fakeslack.CanvasMentionChannel, fakeslack.CanvasMentionTS
	e.a.Navigate("c/" + ch + "/m/" + ts)
	cmScrolledTo(e, fullCanvas(e, fakeslack.CanvasMentionFile))

	e.a.Navigate("activity")
	e.frames(3)
	e.a.Navigate("c/" + ch + "/t/" + ts)
	cmScrolledTo(e, fullCanvas(e, fakeslack.CanvasMentionFile))
	if sec := e.a.secondary; sec.open() && sec.channel == ch {
		t.Errorf("the thread link opened %v %s/%s beside the canvas, want none (a mention has no comment thread)", sec.kind, sec.channel, sec.ts)
	}
	// a toast's click on the mention opens the canvas unfocused
	// (TestNotificationOpensCanvasUnfocused)
}

// cmSeedComments gives the shell's store the Launch plan canvas's file
// channel, as the private conversation it is, and the canvas's files.info.
func cmSeedComments(e *actShellEnv) {
	s := e.ca.S
	s.PutConv(slack.Conversation{ID: fakeslack.FileChanLaunch, Name: "Launch plan", IsChannel: true, IsGroup: true,
		IsPrivate: true, IsFile: true, IsMember: true, NumMembers: 4})
	s.PutCanvasRef(fakeslack.CanvasChannel, richtext.CanvasRef{Title: "Launch plan", Status: "normal", Kind: "canvas"})
	e.a.data.fetched[fakeslack.FileChanLaunch] = time.Now() // no header reads for it
}

// A ⌘⌥ click on a canvas mention's row is Slack's maybeOpenInSurface:
// toChannel in the split view, which the RedirectFileChannel middleware
// turns into the canvas there too. Clack's split view holds conversations
// only, so the canvas goes in the side panel; the file channel never
// shows, and the click doesn't open the row in the inbox.
func TestCanvasMentionSplitClickOpensCanvas(t *testing.T) {
	e := newActShell(t)
	cmSeed(e)
	cmOpenActivity(e)
	pos := f32.Pt(float32(e.sz.X/3), 232) // the newest row (the mention), right of its sender and time
	mods := shortcut.Mod() | key.ModAlt
	e.rt.Queue(
		pointer.Event{Kind: pointer.Move, Source: pointer.Mouse, Position: pos, Modifiers: mods},
		pointer.Event{Kind: pointer.Press, Source: pointer.Mouse, Buttons: pointer.ButtonPrimary, Position: pos, Modifiers: mods},
		pointer.Event{Kind: pointer.Release, Source: pointer.Mouse, Position: pos, Modifiers: mods},
	)
	e.frames(3)
	for _, r := range e.a.split.stack {
		if r.Channel() == fakeslack.CanvasMentionChannel {
			t.Fatalf("the split view opened the canvas's file channel: %s", r)
		}
	}
	e.until("the canvas in the side panel", func() bool {
		cv, ok := e.a.secondary.view.(*canvasview.View)
		return e.a.secondary.open() && ok && cv.ID() == fakeslack.CanvasMentionFile
	})
	if k := e.activity().OpenedItem(); k != "" {
		t.Errorf("the split click opened %q in the inbox; it goes to the split view's place only", k)
	}
	if r := string(e.a.router.Current()); r != "activity" {
		t.Errorf("the route is %q, want activity", r)
	}
}

// Open in split view (and the ⌘⌥ click) on a comment's item is toThread
// in the split view: the comment thread stays a thread there, as the
// RedirectFileChannel middleware leaves Thread views alone, not the canvas
// in the side panel.
func TestCanvasCommentSplitOpensThread(t *testing.T) {
	e := newActShell(t)
	cmSeedComments(e)
	cmOpenActivity(e)
	route := Route("c/" + fakeslack.FileChanLaunch + "/t/" + fakeslack.CommentUnread)
	e.a.OpenSplit(string(route))
	e.frames(5)
	if n := len(e.a.split.stack); n == 0 || e.a.split.stack[n-1] != route {
		t.Errorf("the split view holds %v, want the comment thread %s", e.a.split.stack, route)
	}
	if cv, ok := e.a.secondary.view.(*canvasview.View); ok && e.a.secondary.open() {
		t.Errorf("the side panel opened canvas %s instead", cv.ID())
	}
}

// A canvas comment's items in the App. In the inbox a reply that
// mentions the viewer opens its thread in the column (GMkG: toThread).
// With the persistent panel it jumps to its message: the canvas in the
// Activity pane with the comment thread in the column (jumpToMessage →
// openDocWithThreadTs), the inbox staying; the comment's thread_v2 opens
// its comment thread in the pane.
func TestActivityCanvasCommentClicks(t *testing.T) {
	open := func(e *actShellEnv, text string) {
		e.activity().Search(text)
		e.frames(5)
		e.activity().StepItem(1, false)
	}
	commentThread := func(e *actShellEnv, ts string) func() bool {
		return func() bool {
			sec := e.a.secondary
			return sec.open() && sec.kind == secThread && sec.channel == fakeslack.FileChanLaunch && sec.ts == ts
		}
	}
	t.Run("inbox", func(t *testing.T) {
		e := newActShell(t)
		e.fs.AddCanvasCommentActivity()
		cmSeedComments(e)
		cmOpenActivity(e)
		open(e, "keep it on Wednesday")
		e.until("the reply's thread in the column", commentThread(e, fakeslack.CommentOpen))
	})
	t.Run("persistent", func(t *testing.T) {
		e := newActShell(t)
		e.fs.AddCanvasCommentActivity()
		cmSeedComments(e)
		off := "off"
		e.ca.SetExperimentGroups(map[string]*string{"activity_inbox_remove_persistent_detail_panel": &off, "nested_secondary_view": &off})
		cmOpenActivity(e)
		open(e, "keep it on Wednesday")
		e.until("the canvas in the Activity pane", func() bool {
			v := e.a.canvasJumpsOf().pane
			return v != nil && v.ID() == fakeslack.CanvasChannel
		})
		e.until("the comment thread beside it", commentThread(e, fakeslack.CommentOpen))
		if r := string(e.a.router.Current()); r != "activity" {
			t.Errorf("the click left Activity for %q", r)
		}
		open(e, "Booked it")
		e.until("the thread_v2's comment thread in the pane", func() bool {
			return e.a.convos[fakeslack.FileChanLaunch+"/"+fakeslack.CommentUnread] != nil
		})
	})
}

// A jump whose message the store lacks reads it with messages.list
// first. When that fails nothing opens, as in Slack (openDocWithThreadTs
// rejects and Primary stays as it was).
func TestCanvasJumpFetchFailureOpensNothing(t *testing.T) {
	e := newActShell(t)
	cmSeed(e)
	e.a.Navigate("c/" + fakeslack.ViewsChannel)
	e.frames(5)
	e.fs.SetFault("messages.list", fakeslack.Fault{Error: "internal_error"})
	before := e.a.router.Current()
	e.a.Navigate("c/" + fakeslack.CanvasMentionChannel + "/m/" + fakeslack.CanvasMentionTS)
	e.until("messages.list", func() bool { return e.fs.CallCount("messages.list") > 0 })
	e.frames(30)
	if r := e.a.router.Current(); r != before {
		t.Errorf("the route moved to %q, want %q", r, before)
	}
	if _, ok := e.a.views[Route("docs/"+fakeslack.CanvasMentionFile)]; ok {
		t.Errorf("the canvas's full view opened")
	}
}

// A slow jump drops once the user moved on: another view opened in the
// column first, not only a navigation.
func TestCanvasJumpDroppedByAnotherOpen(t *testing.T) {
	e := newActShell(t)
	cmSeed(e)
	e.a.Navigate("c/" + fakeslack.ViewsChannel)
	e.frames(5)
	e.fs.SetFault("messages.list", fakeslack.Fault{Delay: 800 * time.Millisecond})
	e.a.Navigate("c/" + fakeslack.CanvasMentionChannel + "/m/" + fakeslack.CanvasMentionTS) // reads its message first
	e.frames(2)
	e.a.OpenChannelSecondary(fakeslack.ViewsChannel) // meanwhile
	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		e.frame()
		time.Sleep(20 * time.Millisecond)
	}
	if r := string(e.a.router.Current()); r == "docs/"+fakeslack.CanvasMentionFile {
		t.Errorf("the canvas opened after the user had opened something else")
	}
}

// A jump that waits for files.info and never hears back (a transport
// error brings no "names") opens the conversation after canvasWaitLimit,
// instead of dropping the click.
func TestCanvasJumpWaitEnds(t *testing.T) {
	e := newActShell(t)
	cmSeedConv(e)
	e.fs.SetFault("files.info", fakeslack.Fault{HangUp: true})
	e.ca.ServeLookups()
	t.Cleanup(e.ca.StopTraffic)
	route := "c/" + fakeslack.CanvasMentionChannel + "/m/" + fakeslack.CanvasMentionTS
	e.a.Navigate(route)
	e.frames(2)
	if r := string(e.a.router.Current()); r == route {
		t.Fatalf("the jump didn't wait for files.info")
	}
	deadline := time.Now().Add(canvasWaitLimit + 3*time.Second)
	for string(e.a.router.Current()) != route {
		if time.Now().After(deadline) {
			t.Fatalf("still at %q %v after files.info hung up; want the conversation %s", e.a.router.Current(), canvasWaitLimit+3*time.Second, route)
		}
		e.frame()
		time.Sleep(20 * time.Millisecond)
	}
}

// A jump into a canvas's file channel before files.info named its canvas
// (canvas refs live in memory, so a fresh session's link or toast meets
// it) waits for files.info instead of opening the file channel. Then the
// canvas opens, at the mention's section messages.list brings.
func TestCanvasJumpWaitsForFilesInfo(t *testing.T) {
	e := newActShell(t)
	cmSeedConv(e)
	e.fs.SetFault("files.info", fakeslack.Fault{Delay: 400 * time.Millisecond})
	e.ca.ServeLookups()
	t.Cleanup(e.ca.StopTraffic)
	// the shell's backend isn't started: its "names" go to the App as
	// backend.start would send them
	evs, unsub := e.ca.Subscribe()
	t.Cleanup(unsub)
	go func() {
		for ev := range evs {
			if ev.Type == "names" {
				ev := ev
				e.a.Post(func() { e.a.data.handle(e.a, ev) })
			}
		}
	}()
	e.a.Navigate("c/" + fakeslack.CanvasMentionChannel + "/m/" + fakeslack.CanvasMentionTS)
	e.frames(2)
	if r := e.a.router.Current(); r.Channel() == fakeslack.CanvasMentionChannel {
		t.Fatalf("the jump opened the canvas's file channel (%s) before files.info answered", r)
	}
	cmScrolledTo(e, fullCanvas(e, fakeslack.CanvasMentionFile))
	if n := e.fs.CallCount("files.info"); n == 0 {
		t.Errorf("files.info was never asked")
	}
}

// A comment's notification (a reply in one of a canvas's comment threads:
// jumpToMessage with the thread) opens the canvas with that comment
// thread in the pane beside it, as Slack's openDocWithThreadTs does once
// it read the thread's root, never the file channel with the thread open.
func TestCanvasCommentJumpOpensCanvas(t *testing.T) {
	e := newActShell(t)
	s := e.ca.S
	s.PutConv(slack.Conversation{ID: fakeslack.FileChanLaunch, Name: "Launch plan", IsChannel: true, IsGroup: true,
		IsPrivate: true, IsFile: true, IsMember: true, NumMembers: 2})
	s.PutCanvasRef(fakeslack.CanvasChannel, richtext.CanvasRef{Title: "Launch plan", Status: "normal", Kind: "canvas"})
	e.a.data.fetched[fakeslack.FileChanLaunch] = time.Now() // no header reads for it
	e.a.Navigate("activity")
	e.frames(3)
	const reply = "1762510200.000300" // the viewer's reply in CommentOpen
	e.a.notificationClicked(fakeslack.FileChanLaunch, reply, fakeslack.CommentOpen, "")
	fullCanvas(e, fakeslack.CanvasChannel)
	e.until("the comment thread beside the canvas", func() bool {
		sec := e.a.secondary
		return sec.kind == secThread && sec.channel == fakeslack.FileChanLaunch && sec.ts == fakeslack.CommentOpen
	})
}

// A canvas tab whose title emoji the emoji map didn't have when the tab
// bar was read (a warm start's map still loading, an emoji newer than the
// cached map) draws it as an image once the map has it: Karim's channel
// tab read "clack testing :cat-slackbot:". A leading emoji is the tab's
// icon (getLeadingTitleEmoji), once the map has it too.
func TestCanvasTabTitleEmojiLate(t *testing.T) {
	fs, err := fakeslack.Start(fakeslack.Small)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { fs.Close() })
	const ch, file, name = "C0FAKE00001", "F0TABEMOJ01", "late-emoji-fixture"
	const lead, leadName = "F0TABEMOJ02", "late-lead-fixture"
	e := startEnv(t, fs, t.TempDir())
	e.waitReady()
	section := func(els ...any) []any {
		return []any{map[string]any{"type": "rich_text", "block_id": "t", "elements": []any{
			map[string]any{"type": "rich_text_section", "elements": els}}}}
	}
	fs.AddCanvas(fakeslack.FakeCanvas{ID: file, Channel: ch, Title: "clack testing :" + name + ":",
		TitleBlocks: section(map[string]any{"type": "text", "text": "clack testing "}, map[string]any{"type": "emoji", "name": name})})
	fs.AddCanvas(fakeslack.FakeCanvas{ID: lead, Channel: ch, Title: ":" + leadName + ": lead notes",
		TitleBlocks: section(map[string]any{"type": "emoji", "name": leadName}, map[string]any{"type": "text", "text": " lead notes"})})
	e.openChannel(ch)
	tab := func(id string) (chTab, bool) {
		for _, tb := range e.a.channelTabs(ch) {
			if tb.fileID == id {
				return tb, true
			}
		}
		return chTab{}, false
	}
	var spans []richtext.Span
	e.until("the canvas tabs", 15*time.Second, func() bool {
		if !e.a.data.tabsDone[ch] {
			return false
		}
		a, ok1 := tab(file)
		_, ok2 := tab(lead)
		spans = a.spans
		return ok1 && ok2
	})
	for _, s := range spans {
		if s.Kind == richtext.Emoji && s.URL != "" {
			t.Fatalf("precondition: the emoji map had %s already (%+v)", name, spans)
		}
	}
	if tb, _ := tab(lead); tb.emojiIcon != "" {
		t.Fatalf("precondition: the leading emoji's image was known already (%q)", tb.emojiIcon)
	}
	url := fs.URL + "img/64x64/" + fakeslack.TeamID + "/" + name + "/00000000000000ee.png"
	leadURL := fs.URL + "img/64x64/" + fakeslack.TeamID + "/" + leadName + "/00000000000000ef.png"
	e.a.Core().S.CustomEmoji.Put(name, url) // emoji_changed, or the map's refresh
	e.a.Core().S.CustomEmoji.Put(leadName, leadURL)
	e.until("the tab title's emoji as an image", 15*time.Second, func() bool {
		tb, _ := tab(file)
		for _, s := range tb.spans {
			if s.Kind == richtext.Emoji && s.URL == url {
				return true
			}
		}
		return false
	})
	if tb, _ := tab(file); !strings.Contains(richtext.PlainSpans(tb.spans), "clack testing") {
		t.Errorf("the tab's title lost its text: %+v", tb.spans)
	}
	e.until("the leading emoji as the tab's icon", 15*time.Second, func() bool {
		tb, _ := tab(lead)
		return tb.emojiIcon == leadURL
	})
	if tb, _ := tab(lead); strings.Contains(tb.label, ":"+leadName+":") || !strings.Contains(tb.label, "lead notes") {
		t.Errorf("the leading emoji's tab reads %q, want its title without the emoji", tb.label)
	}
	e.frame()
}

// The tab bar resolves late only the emoji TitleBlocks kept (Text
// ":name:"); a hidden emoji (Unicode 17 with emoji_v17 off) keeps its
// characters even when a custom emoji has its name.
func TestResolveEmojiKeepsHiddenEmoji(t *testing.T) {
	char := string(rune(0x1FAE8))
	in := []richtext.Span{{Kind: richtext.Emoji, ID: "hidden-fixture", Text: char}, {Kind: richtext.Emoji, ID: "kept-fixture", Text: ":kept-fixture:"}}
	out := resolveEmoji(in, func(name string) (string, string, bool) { return "https://emoji.example/" + name + ".png", "", true })
	if out[0].URL != "" || out[0].Text != char {
		t.Errorf("the hidden emoji became %+v, want its characters", out[0])
	}
	if out[1].URL == "" {
		t.Errorf("the kept emoji wasn't resolved: %+v", out[1])
	}
}
