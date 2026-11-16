package ui

import (
	"image"
	"image/png"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"slices"
	"sort"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"
	"weak"

	"gioui.org/f32"
	"gioui.org/io/key"
	"gioui.org/io/pointer"

	"github.com/some-du6e/clack/internal/demo"
	"github.com/some-du6e/clack/internal/emoji"
	"github.com/some-du6e/clack/internal/ui/slackc"
	"github.com/some-du6e/clack/internal/ui/theme/pixeltest"
)

// introPreviewTestCommand runs the preview renderer as this test binary
// (TestIntroPreviewChild), the way the app runs `clack intro-preview`.
func introPreviewTestCommand(args ...string) (*exec.Cmd, error) {
	cmd := exec.Command(os.Args[0], append([]string{"-test.run=^TestIntroPreviewChild$", "-test.count=1", "--"}, args...)...)
	cmd.Env = append(os.Environ(), "CLACK_INTRO_PREVIEW_CHILD=1")
	return cmd, nil
}

// TestIntroPreviewChild is the renderer's process in tests.
func TestIntroPreviewChild(t *testing.T) {
	if os.Getenv("CLACK_INTRO_PREVIEW_CHILD") == "" {
		t.Skip("the preview renderer's process (introPreviewTestCommand)")
	}
	args := os.Args
	for i, a := range args {
		if a == "--" {
			args = args[i+1:]
			break
		}
	}
	os.Exit(RunIntroPreview(args, os.Stdout, os.Stderr))
}

// usePreviewRenderer turns the renderer on for this test; its reports
// arrive on the channel (nil: no reports asked for).
func usePreviewRenderer(t *testing.T, reports bool) chan *introPreviewReport {
	t.Helper()
	oldCmd, oldHook := introPreviewCommand, introPreviewReportHook
	introPreviewCommand = introPreviewTestCommand
	var ch chan *introPreviewReport
	if reports {
		ch = make(chan *introPreviewReport, 4)
		introPreviewReportHook = func(r *introPreviewReport) { ch <- r }
	}
	t.Cleanup(func() { introPreviewCommand, introPreviewReportHook = oldCmd, oldHook })
	return ch
}

// waitReal draws frames without moving the virtual clock (the beat stays
// where it is) until ok, for at most d of real time: the renderer works
// in real time.
func waitReal(t *testing.T, e *introEnv, what string, d time.Duration, ok func() bool) {
	t.Helper()
	dl := time.Now().Add(d)
	for !ok() {
		if time.Now().After(dl) {
			t.Fatalf("timed out after %v waiting for %s", d, what)
		}
		e.frame(0)
		time.Sleep(5 * time.Millisecond)
	}
}

// writeShot saves img (cropped to r unless r is empty) as dir/name.png.
func writeShot(t *testing.T, dir, name string, img *image.RGBA, r image.Rectangle) {
	t.Helper()
	var out image.Image = img
	if !r.Empty() {
		out = img.SubImage(r.Intersect(img.Bounds()))
	}
	f, err := os.Create(filepath.Join(dir, name+".png"))
	if err != nil {
		t.Fatal(err)
	}
	defer f.Close()
	if err := png.Encode(f, out); err != nil {
		t.Fatal(err)
	}
}

// previewReads are the fakeslack methods the renderer may call: reads,
// and the client's own session bookkeeping. Anything else (a mark, a
// draft, a post, a pref write) fails the test.
var previewReads = []string{
	"admin.roles.entity.listAssignments", "api.features", "apps.profile.get", "auth.test", "bookmarks.list", "bots.info", "canvases.sections.lookup",
	"channels.prefs.get", "chat.getPermalink", "client.boot", "client.counts", "client.userBoot", "conversations.history",
	"conversations.info", "conversations.list", "conversations.members", "conversations.replies", "conversations.view",
	"dnd.info", "dnd.teamInfo", "drafts.list", "edge/huddles/list", "edge/permissions/info", "edge/users/counts",
	"edge/users/info", "edge/users/list", "enterprise.prefs.get", "megaphone.notifications.list", "sidebar.dms", "emoji.adminList", "emoji.list", "experiments.getByUser", "files.info",
	"files.list", "lists.items.list", "pins.list", "reminders.list", "saved.list", "search.modules", "stars.list",
	"subscriptions.thread.get", "subscriptions.thread.getView", "team.info", "team.prefs.get", "users.channelSections.list",
	"users.conversations", "users.counts", "users.getPresence", "users.info", "users.list", "users.prefs.get",
	"users.profile.get", "workflows.featured.list",
}

// "It's your Slack, 1:1." shows the real app: the preview renderer, its
// own process, boots the demo workspace and draws the shell with the
// sidebar's sections, #general's header, messages and composer and a
// thread, in the window's theme, and the card draws its frame. The window
// gives nothing of itself: its emoji set, plugin patches and Dock badge
// are untouched while the renderer works and the card shows, clicks and
// keys on the card do nothing, the renderer calls only reads, and after
// the introduction the preview is gone: unreachable, its goroutines ended,
// its folder removed (and on quit mid-renderer too). With LOGIN_SHOTS_DIR
// set (the login-shots workflow) the beat is saved, light and dark:
// intro-slack-<mode>.png and its card, -card.png.
func TestIntroSlackPreview(t *testing.T) {
	dir := os.Getenv("LOGIN_SHOTS_DIR")
	sz := image.Pt(1440, 900)
	for _, dark := range []bool{false, true} {
		mode := "light"
		if dark {
			mode = "dark"
		}
		t.Run(mode, func(t *testing.T) {
			reports := usePreviewRenderer(t, true)
			a, _, _ := fakePrefsApp(t) // the window (its own core sets the emoji set: before the sentinel)
			a.motionOverride = -1
			if dark {
				a.th.SetDark(true)
			}
			// the window's process-wide state, set to what the renderer would
			// change if it reached it
			v17 := !emoji.V17()
			emoji.SetV17(v17)
			t.Cleanup(func() { emoji.SetV17(!v17) })
			slackc.SetPatched([]string{"IntroSentinel"})
			t.Cleanup(func() { slackc.SetPatched(nil) })
			var badges atomic.Int32
			oldAny, oldBadge := dockAnyOS, dockBadge
			dockAnyOS, dockBadge = true, func(string) { badges.Add(1) }
			t.Cleanup(func() { dockAnyOS, dockBadge = oldAny, oldBadge })
			// a background goroutine, as a plugin dispatch would, sees the
			// window's patches the whole time
			var unpatched atomic.Int32
			stop := make(chan struct{})
			var bg sync.WaitGroup
			bg.Add(1)
			go func() {
				defer bg.Done()
				for {
					select {
					case <-stop:
						return
					default:
					}
					if !slackc.Patched("IntroSentinel") {
						unpatched.Add(1)
					}
					time.Sleep(time.Millisecond)
				}
			}()

			e := newIntroEnv(t, a, sz)
			e.run(300 * time.Millisecond)
			runtime.GC()
			base := runtime.NumGoroutine()
			a.replayIntro()
			e.frame(time.Second / 60)
			e.started()
			in := a.intro
			e.key(key.NameRightArrow)
			e.key(key.NameRightArrow)
			if in.cur != beatSlack {
				t.Fatalf("beat %d, want your Slack (%d)", in.cur, beatSlack)
			}
			waitReal(t, e, "the preview's frame", 90*time.Second, func() bool { return in.preview.ready() || in.preview.failed() })
			if in.preview.failed() {
				if w, err := pixeltest.NewWindow(64, 64); err != nil {
					t.Skipf("no GPU for the renderer: %v", err)
				} else {
					w.Release()
				}
				t.Fatal("the preview renderer failed (its log is above)")
			}
			var rep *introPreviewReport
			select {
			case rep = <-reports:
			case <-time.After(10 * time.Second):
				t.Fatal("no report from the renderer")
			}
			e.run(1200 * time.Millisecond) // the card has risen, the image faded in

			var rec []introDrawn
			in.rec = &rec
			e.frame(time.Second / 60)
			in.rec = nil
			full := false
			for _, d := range rec {
				if d.name == "slack-preview" && d.a > 0.99 {
					full = true
				}
			}
			if !full {
				t.Errorf("the preview isn't fully drawn: %+v", rec)
			}

			// what the renderer drew: the app's own components
			t.Logf("renderer: %.1fs, timeout %v, route %q, sections %q, %d rows, thread %v, list %s, composer %s, unread %v",
				rep.Elapsed, rep.Timeout, rep.Route, rep.Sections, rep.Rows, rep.Thread, rep.List, rep.Composer, rep.Unread)
			t.Logf("drawn: %q", rep.Drawn)
			if rep.Timeout {
				t.Error("the renderer never settled")
			}
			if rep.Team != demo.TeamName || !slices.Contains(rep.Sections, "Sales floor") || rep.Rows == 0 {
				t.Errorf("the sidebar: team %q, sections %q, %d rows", rep.Team, rep.Sections, rep.Rows)
			}
			if !rep.Thread || rep.List != "ui.channelList" || rep.Composer != "ui.composerBox" {
				t.Errorf("#general: thread %v, list %s, composer %s (want the real list, composer and a thread)", rep.Thread, rep.List, rep.Composer)
			}
			for _, want := range []string{demo.TeamName, "general", "Sales floor", "Thread", "Reply to thread…"} {
				if !slices.Contains(rep.Drawn, want) {
					t.Errorf("%q isn't drawn", want)
				}
			}
			for _, s := range rep.Drawn {
				if strings.Contains(s, "new message") {
					t.Errorf("#general opens with a %q pill", s)
				}
			}
			if rep.Unread {
				t.Error("#general starts unread")
			}
			if bg := a.th.C.Bg; rep.Dark != dark || rep.Bg != [3]uint8{bg.R, bg.G, bg.B} {
				t.Errorf("the renderer's theme: dark %v, panel %v; the window's: dark %v, panel %v", rep.Dark, rep.Bg, dark, bg)
			}
			if len(rep.Dock) != 0 {
				t.Errorf("the renderer asked for Dock badges %q", rep.Dock)
			}
			var calls []string
			for m, n := range rep.Calls {
				calls = append(calls, m)
				if n > 0 && !slices.Contains(previewReads, m) {
					t.Errorf("the renderer called %s ×%d (not a read the preview may make)", m, n)
				}
			}
			sort.Strings(calls)
			t.Logf("calls: %q", calls)

			if dir != "" {
				win, err := pixeltest.NewWindow(sz.X, sz.Y)
				if err != nil {
					t.Fatalf("headless window: %v", err)
				}
				e.win = win
				e.frame(time.Second / 60)
				e.win = nil
				win.Release()
				writeShot(t, dir, "intro-slack-"+mode, e.img, image.Rectangle{})
				writeShot(t, dir, "intro-slack-"+mode+"-card", e.img, in.preview.card)
			}

			// inert: clicks on the card, keys and text change nothing
			card := in.preview.card
			for _, pt := range []image.Point{
				{card.Min.X + card.Dx()*45/100, card.Max.Y - card.Dy()/14},
				{card.Min.X + card.Dx()*12/100, card.Min.Y + card.Dy()/2},
				{card.Max.X - card.Dx()/8, card.Min.Y + card.Dy()/3},
			} {
				fp := f32.Pt(float32(pt.X), float32(pt.Y))
				e.r.Queue(
					pointer.Event{Kind: pointer.Move, Source: pointer.Mouse, Position: fp},
					pointer.Event{Kind: pointer.Press, Source: pointer.Mouse, Buttons: pointer.ButtonPrimary, Position: fp},
					pointer.Event{Kind: pointer.Release, Source: pointer.Mouse, Position: fp},
				)
				e.frame(time.Second / 60)
			}
			e.r.Queue(key.EditEvent{Text: "hello from the introduction"})
			e.r.Queue(key.Event{Name: "K", Modifiers: key.ModShortcut, State: key.Press}, key.Event{Name: "K", Modifiers: key.ModShortcut, State: key.Release})
			e.frame(time.Second / 60)
			e.frame(time.Second / 60)
			if len(a.overlays) != 0 || in.cur != beatSlack {
				t.Errorf("input on the card: %d overlays, beat %d", len(a.overlays), in.cur)
			}

			// the window's own state, untouched
			if emoji.V17() != v17 {
				t.Error("the window's emoji set changed")
			}
			if n := badges.Load(); n != 0 {
				t.Errorf("the Dock badge was set %d times", n)
			}
			if n := unpatched.Load(); n != 0 {
				t.Errorf("a background reader saw the window's plugin patches gone %d times", n)
			}

			// gone with the introduction
			wp := weak.Make(in.preview)
			e.key(key.NameEscape)
			e.run(1500 * time.Millisecond)
			e.key(key.NameReturn)
			e.run(time.Second)
			if a.intro != nil {
				t.Fatal("the introduction is still up")
			}
			in = nil
			dl := time.Now().Add(15 * time.Second)
			for {
				runtime.GC()
				n := runtime.NumGoroutine()
				if wp.Value() == nil && n <= base {
					break
				}
				if time.Now().After(dl) {
					buf := make([]byte, 1<<20)
					t.Fatalf("after the introduction: preview reachable %v, %d goroutines (%d before):\n%s",
						wp.Value() != nil, n, base, buf[:runtime.Stack(buf, true)])
				}
				e.frame(0)
				time.Sleep(20 * time.Millisecond)
			}
			close(stop)
			bg.Wait()
			if ms, _ := filepath.Glob(filepath.Join(os.TempDir(), previewDirPrefix+"*")); len(ms) != 0 {
				t.Errorf("renderer folders left: %q", ms)
			}
		})
	}
}

// Quitting while the renderer still works stops it and removes its
// folder before the window goes (DestroyEvent's shutdownIntro).
func TestIntroPreviewQuit(t *testing.T) {
	usePreviewRenderer(t, false)
	a, _, _ := fakePrefsApp(t)
	a.motionOverride = -1
	e := newIntroEnv(t, a, image.Pt(1280, 800))
	e.run(200 * time.Millisecond)
	a.replayIntro()
	e.frame(time.Second / 60)
	e.started()
	p := a.intro.preview
	var dir string
	waitReal(t, e, "the renderer running", 60*time.Second, func() bool {
		p.mu.Lock()
		defer p.mu.Unlock()
		dir = p.dir
		return p.proc != nil || p.fail
	})
	if p.fail {
		t.Skip("the renderer didn't start")
	}
	a.shutdownIntro()
	p.mu.Lock()
	proc := p.proc
	p.mu.Unlock()
	if proc != nil {
		t.Error("the renderer is still running after shutdown")
	}
	if _, err := os.Stat(dir); !os.IsNotExist(err) {
		t.Errorf("the renderer's folder %s is still there (%v)", dir, err)
	}
}
