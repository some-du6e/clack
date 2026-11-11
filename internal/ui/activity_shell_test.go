package ui

import (
	"context"
	"errors"
	"fmt"
	"image"
	"image/png"
	"os"
	"path/filepath"
	"testing"
	"time"

	"gioui.org/f32"
	"gioui.org/gpu/headless"
	"gioui.org/io/input"
	"gioui.org/io/key"
	"gioui.org/io/pointer"
	"gioui.org/op"

	"github.com/some-du6e/clack/internal/core"
	"github.com/some-du6e/clack/internal/fakeslack"
	"github.com/some-du6e/clack/internal/imgcache"
	"github.com/some-du6e/clack/internal/slack"
	"github.com/some-du6e/clack/internal/store"
	"github.com/some-du6e/clack/internal/ui/slackc"
	"github.com/some-du6e/clack/internal/ui/theme"
	"github.com/some-du6e/clack/internal/ui/views"
)

// The Activity tab in the whole client, on internal/fakeslack's activity
// fixture (Karim, alpha.33: the rows took the left half of the page
// beside an empty half, and ⌘K did nothing there). Synthetic content
// only.

type actShellEnv struct {
	t   *testing.T
	a   *App
	ca  *core.App
	fs  *fakeslack.Server
	rt  input.Router
	ops op.Ops
	sz  image.Point
}

func newActShell(t *testing.T) *actShellEnv {
	t.Helper()
	t.Setenv("CLACK_HOME", t.TempDir())
	sz := fakeslack.Small
	sz.Views = true
	fs, err := fakeslack.Start(sz)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = fs.Close() })
	s := store.New("")
	s.Lock()
	s.TeamID, s.TeamName = fakeslack.TeamID, "Synthetic Club"
	s.Unlock()
	s.PutUsers([]slack.User{
		{ID: fakeslack.SelfID, Name: "self", RealName: "Test Self"},
		{ID: "U0FAKE00001", Name: "ada", RealName: "Ada Example"},
		{ID: "U0FAKE00002", Name: "grace", RealName: "Grace Example"},
	})
	s.PutConv(slack.Conversation{ID: fakeslack.ViewsChannel, Name: "views-fixtures", IsChannel: true, IsMember: true, NumMembers: 3})
	s.PutConv(slack.Conversation{ID: "D0FAKE00000", IsIM: true, User: "U0FAKE00001", IsMember: true})
	s.PutConv(slack.Conversation{ID: fakeslack.ActivitySlackbotDM, IsIM: true, User: "USLACKBOT", IsMember: true})
	ca := core.New(slack.New(fs.Creds()), s)
	if u, ok := s.User(fakeslack.SelfID); ok {
		ca.S.Self = *u
	}
	// the account's Activity: the inbox with an opened item beside it
	// (core's defaults have these; fakeslack's experiments.getByUser
	// doesn't list them)
	on := "on"
	ca.SetExperimentGroups(map[string]*string{
		"activity_inbox": &on, "nested_secondary_view": &on, "secondary_view_default_width": &on,
		"activity_inbox_remove_persistent_detail_panel": &on, "activity_entry_layout_revisions_2": &on,
		"activity_global_settings": &on,
	})
	ca.ServeOffline()
	images := imgcache.New(func(ctx context.Context, url string) ([]byte, error) { return nil, errors.New("offline") }, "", nil)
	a := New(Options{ConfigDir: t.TempDir(), NoRealtime: true}, images)
	sh := theme.NewShaper()
	a.th.SetShapers(sh, sh)
	b := newBackend(ca, "")
	b.app = a
	b.ctx, b.cancel = context.WithCancel(context.Background())
	t.Cleanup(b.cancel)
	for _, ch := range []string{fakeslack.ViewsChannel, "D0FAKE00000", fakeslack.ActivitySlackbotDM} {
		b.fetched[ch] = time.Now() // no tab / facepile fetches
	}
	a.data = b
	b.apply(a, ca.Snapshot())
	return &actShellEnv{t: t, a: a, ca: ca, fs: fs, sz: image.Pt(1347, 853)}
}

func (e *actShellEnv) frame() {
	e.ops.Reset()
	gtx := ttContext(&e.ops, e.sz)
	gtx.Source = e.rt.Source()
	e.a.drain()
	slackc.Pump()
	e.a.title = e.a.windowTitle()
	e.a.frame(gtx)
	e.rt.Frame(&e.ops)
}

func (e *actShellEnv) frames(n int) {
	for i := 0; i < n; i++ {
		e.frame()
		time.Sleep(5 * time.Millisecond)
	}
}

func (e *actShellEnv) until(what string, ok func() bool) {
	e.t.Helper()
	deadline := time.Now().Add(15 * time.Second)
	for !ok() {
		if time.Now().After(deadline) {
			e.t.Fatalf("timed out waiting for %s", what)
		}
		e.frame()
		time.Sleep(10 * time.Millisecond)
	}
}

func (e *actShellEnv) click(p image.Point) {
	pos := f32.Pt(float32(p.X), float32(p.Y))
	e.rt.Queue(
		pointer.Event{Kind: pointer.Move, Source: pointer.Mouse, Position: pos},
		pointer.Event{Kind: pointer.Press, Source: pointer.Mouse, Buttons: pointer.ButtonPrimary, Position: pos},
		pointer.Event{Kind: pointer.Release, Source: pointer.Mouse, Position: pos},
	)
	e.frames(3)
}

func (e *actShellEnv) key(name key.Name, mods key.Modifiers) {
	e.rt.Queue(key.Event{Name: name, Modifiers: mods, State: key.Press}, key.Event{Name: name, Modifiers: mods, State: key.Release})
	e.frames(3)
}

func (e *actShellEnv) activity() *views.Activity {
	v, _ := e.a.views[Route("activity")].(*views.Activity)
	return v
}

// openRow clicks down the list until a row opens beside the inbox in the
// app's secondary column.
func (e *actShellEnv) openRow() {
	e.t.Helper()
	x := e.sz.X / 3
	for y := 200; y < e.sz.Y-40; y += 30 {
		e.click(image.Pt(x, y))
		if e.a.secondary.open() && (e.a.secondary.kind == secChannel || e.a.secondary.kind == secThread) {
			return
		}
	}
	e.t.Fatalf("no row opened in the secondary column (secondary %+v)", e.a.secondary.kind)
}

// GMkG with activity_inbox_remove_persistent_detail_panel: an item opens
// as a secondary view beside the inbox, which keeps the rest of the
// width (on the Activity tab the column is half the region); the
// view's own split never applies, so no half of the page is left blank.
func TestActivityItemOpensBesideTheInbox(t *testing.T) {
	e := newActShell(t)
	e.a.Navigate("activity")
	e.until("the activity view", func() bool { return e.activity() != nil })
	e.until("the activity feed", func() bool { return e.fs.CallCount("activity.feed") > 0 })
	e.frames(20)
	e.openRow()
	panel := e.sz.X
	if w := e.a.secondary.drawnW; w < panel/4 || w > panel*3/5 {
		t.Errorf("the column is %d px of a %d px window, want about half the region", w, panel)
	}
	ch := e.a.secondary.channel
	if !e.a.SecondaryShows(ch, e.a.secondary.ts) {
		t.Error("SecondaryShows misses the open item")
	}
	if e.a.router.Current().Page() != "activity" {
		t.Errorf("the route left Activity: %s", e.a.router.Current())
	}
	// ⌘. on the Activity tab closes it (N8Zu mod+.)
	e.key(".", key.ModShortcut)
	if e.a.secondary.open() {
		t.Error("⌘. didn't close the item beside the inbox")
	}
	// every window width: the column never covers the inbox
	for _, w := range []int{900, 1100, 1347, 1700, 2200} {
		e.sz = image.Pt(w, 853)
		e.frames(3)
		e.openRow()
		if dw := e.a.secondary.drawnW; dw <= 0 || dw >= w-300 {
			t.Errorf("window %d: column %d px leaves the inbox under 300", w, dw)
		}
		e.key(".", key.ModShortcut)
	}
}

// Slack's global keymap (PG1Q) with the Activity list focused: ⌘K the
// quick switcher, ⌘G search, ⌘/ the shortcuts pane; Esc closes what's on
// top, then the item beside the inbox (N8Zu esc); ⌥↓ is the inbox's next
// item, never the next conversation.
func TestActivityGlobalShortcutsLikeSlack(t *testing.T) {
	e := newActShell(t)
	e.a.Navigate("c/" + fakeslack.ViewsChannel)
	e.frames(5)
	e.a.Navigate("activity")
	e.until("the activity view", func() bool { return e.activity() != nil })
	e.until("the activity feed", func() bool { return e.fs.CallCount("activity.feed") > 0 })
	e.frames(20)
	e.openRow() // the press on a row also focuses the list
	n := len(e.a.overlays)
	e.key("K", key.ModShortcut)
	if len(e.a.overlays) != n+1 {
		t.Fatalf("⌘K on Activity: %d overlays, want %d", len(e.a.overlays), n+1)
	}
	if got := fmt.Sprintf("%T", e.a.overlays[len(e.a.overlays)-1]); got != "*popups.SearchPopover" {
		t.Errorf("⌘K opened %s", got)
	}
	e.key(key.NameEscape, 0)
	if len(e.a.overlays) != n {
		t.Fatalf("Esc left %d overlays", len(e.a.overlays))
	}
	if !e.a.secondary.open() {
		t.Error("the first Esc closed the item too")
	}
	e.key("G", key.ModShortcut)
	if len(e.a.overlays) != n+1 {
		t.Errorf("⌘G on Activity: %d overlays", len(e.a.overlays))
	}
	e.key(key.NameEscape, 0)
	// back on the list: Esc closes the item beside the inbox
	e.click(image.Pt(e.sz.X/4, 400))
	if !e.a.secondary.open() {
		e.openRow()
	}
	e.key(key.NameEscape, 0)
	if e.a.secondary.open() && !e.a.secondary.keyFocus {
		t.Error("Esc on the Activity list didn't close the item beside the inbox")
	}
	e.key("/", key.ModShortcut)
	if e.a.secondary.kind != secShortcuts || !e.a.secondary.open() {
		t.Errorf("⌘/ on Activity: secondary %v", e.a.secondary.kind)
	}
	e.key("/", key.ModShortcut)
	route := e.a.router.Current()
	e.key(key.NameDownArrow, key.ModAlt)
	if e.a.router.Current() != route {
		t.Errorf("⌥↓ on Activity left for %s (the shell's next conversation)", e.a.router.Current())
	}
	// ⌘[ goes back to the channel Activity was opened from
	e.key("[", key.ModShortcut)
	if got := e.a.router.Current(); got.Page() != "c" {
		t.Errorf("⌘[ on Activity: %s", got)
	}
}

// shot draws a frame into win and saves it as a PNG at path.
func (e *actShellEnv) shot(win *headless.Window, path string) *image.RGBA {
	e.t.Helper()
	e.frame()
	if err := win.Frame(&e.ops); err != nil {
		e.t.Fatal(err)
	}
	img := image.NewRGBA(image.Rectangle{Max: e.sz})
	if err := win.Screenshot(img); err != nil {
		e.t.Fatal(err)
	}
	f, err := os.Create(path)
	if err != nil {
		e.t.Fatal(err)
	}
	defer f.Close()
	if err := png.Encode(f, img); err != nil {
		e.t.Fatal(err)
	}
	return img
}

// actShotColors counts the distinct colors in r (4 bits a channel): 1 or
// 2 for a blank area, dozens wherever text is drawn.
func actShotColors(img *image.RGBA, r image.Rectangle) int {
	seen := map[[3]uint8]bool{}
	r = r.Intersect(img.Bounds())
	for y := r.Min.Y; y < r.Max.Y; y++ {
		for x := r.Min.X; x < r.Max.X; x++ {
			c := img.RGBAAt(x, y)
			seen[[3]uint8{c.R >> 4, c.G >> 4, c.B >> 4}] = true
		}
	}
	return len(seen)
}

// With ACTIVITY_SHOTS_DIR set (the activity-shots workflow): the Activity
// tab in the whole window at Karim's 1347×853, Detailed and Dense, light
// and dark, with nothing open and with an item open beside the inbox.
// The column draws the item (alpha.33 left that half of the page blank)
// and the inbox's rows reach it.
func TestActivityShellShots(t *testing.T) {
	dir := os.Getenv("ACTIVITY_SHOTS_DIR")
	if dir == "" {
		t.Skip("ACTIVITY_SHOTS_DIR is unset (the activity-shots workflow sets it)")
	}
	for _, dark := range []bool{false, true} {
		for _, dense := range []bool{false, true} {
			name := map[bool]string{false: "detailed", true: "dense"}[dense] + "-" + map[bool]string{false: "light", true: "dark"}[dark]
			t.Run(name, func(t *testing.T) {
				e := newActShell(t)
				if dark {
					e.a.setDark(true)
				}
				if dense { // the layout toggle's write (activity.prefs.update), before the view loads
					if err := e.ca.SetActivityPrefs(context.Background(), "", "dense"); err != nil {
						t.Fatal(err)
					}
				}
				win, err := headless.NewWindow(e.sz.X, e.sz.Y)
				if err != nil {
					t.Fatalf("headless window: %v", err)
				}
				t.Cleanup(win.Release)
				e.a.Navigate("activity")
				e.until("the activity view", func() bool { return e.activity() != nil })
				e.until("the activity feed", func() bool { return e.fs.CallCount("activity.feed") > 0 })
				e.frames(30)
				e.shot(win, filepath.Join(dir, "shell-inbox-"+name+".png"))
				e.openRow()
				e.frames(40)
				img := e.shot(win, filepath.Join(dir, "shell-open-"+name+".png"))
				o, w := e.a.secondaryOrigin, e.a.secondary.drawnW
				col := image.Rect(o.X+8, o.Y+8, o.X+w-8, e.sz.Y-16)
				if n := actShotColors(img, col); n < 12 {
					t.Errorf("the column beside the inbox (%v) draws %d colors: blank", col, n)
				}
				// the rows' right ends (times, locations, dividers) sit by the column
				edge := image.Rect(o.X-100, 200, o.X-12, 640)
				if n := actShotColors(img, edge); n < 3 {
					t.Errorf("nothing drawn in %v, just left of the column: the inbox stops short of it", edge)
				}
			})
		}
	}
}
