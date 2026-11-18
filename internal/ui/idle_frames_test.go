package ui

import (
	"fmt"
	"image"
	"os"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"gioui.org/f32"
	"gioui.org/io/key"
	"gioui.org/io/pointer"
	"gioui.org/widget"

	"github.com/some-du6e/clack/internal/core"
	"github.com/some-du6e/clack/internal/fakeslack"
	"github.com/some-du6e/clack/internal/imgcache"
	"github.com/some-du6e/clack/internal/slack"
	"github.com/some-du6e/clack/internal/store"
	"github.com/some-du6e/clack/internal/ui/idleprobe"
	"github.com/some-du6e/clack/internal/ui/views"
)

// TestIdleFrames lets the whole client sit for 10 s with no input on each
// surface and counts the frames it draws (CLACK_IDLE_FRAMES=1; ci's linux
// and windows-applog jobs run it). Slack's Chromium draws nothing when
// nothing changes: in 10 s that's a focused caret's blinks
// (FrameCaret's repeating timer, every LayoutTheme::CaretBlinkInterval,
// 500 ms by default) and at most a clock-minute tick (relative times,
// the sidebar's "x minutes ago"). So a surface may draw
//
//   - one frame per 500 ms caret toggle, only while the window is focused
//     (FrameSelection::FocusedOrActiveStateChanged disables the caret in
//     an inactive window), and
//   - idleOtherFrames more, for minute ticks;
//
// and every frame's requester is logged.
//
// The window lifecycle is App.Run's (idleprobe.Run): op.InvalidateCmd
// wake-ups at their time, App.Invalidate at once, router events pending
// after a frame at the next vsync. Realtime is on (the fake's socket
// idles after hello), so presence and typing paths are live.
func TestIdleFrames(t *testing.T) {
	if os.Getenv("CLACK_IDLE_FRAMES") == "" {
		t.Skip("CLACK_IDLE_FRAMES not set")
	}
	p := idleprobe.New()
	defer p.Close()
	// only the App being measured: an earlier one's goroutines may still
	// finish work after its shutdown
	var measured atomic.Pointer[App]
	invalidateHook = func(a *App) {
		if a == measured.Load() {
			p.Invalidated()
		}
	}
	defer func() { invalidateHook = nil }()

	for _, motion := range []struct {
		name     string
		override int
	}{{"motion", -1}, {"reduced motion", 1}} {
		t.Run(motion.name, func(t *testing.T) {
			// the main view, a thread, Activity, Preferences
			sz := fakeslack.Small
			sz.Unread = 0
			sz.ThreadHeader = true
			e := newIdleEnv(t, sz, motion.override)
			measured.Store(e.a)
			ch := fakeslack.ThreadPublicID
			e.openChannel(ch)
			e.until("history", 20*time.Second, func() bool { return e.loadedMessages(ch) > 0 })
			e.focusComposer(ch, "")
			e.idle(t, p, "channel", true)
			e.blur()
			e.idle(t, p, "channel, window in the background", false)
			e.unblur()
			e.a.OpenThread(ch, fakeslack.ThreadRootTS)
			e.frame()
			e.focusComposer(ch, fakeslack.ThreadRootTS)
			e.idle(t, p, "thread", true)
			e.a.CloseSecondary()
			e.a.Navigate("activity")
			e.frame()
			e.idle(t, p, "activity", false)
			e.openChannel(ch)
			e.frame()
			e.a.openPrefs()
			e.frame()
			e.idle(t, p, "preferences", false)
			for e.a.dismissTop() {
			}
			e.shutdown()

			// the sidebar with unread channels and mentions
			sz = fakeslack.Small
			sz.Unreads = true
			e = newIdleEnv(t, sz, motion.override)
			measured.Store(e.a)
			ch = e.fs.ChannelIDs()[0]
			e.openChannel(ch)
			e.until("history", 20*time.Second, func() bool { return e.loadedMessages(ch) > 0 })
			e.focusComposer(ch, "")
			e.idle(t, p, "sidebar with unreads", true)
			e.shutdown()

			// the rail's and the sidebar's views, with their fixtures
			sz = fakeslack.Small
			sz.Views = true
			sz.DMsTab = true
			sz.Unreads = true
			sz.ThreadHeader = true
			e = newIdleEnv(t, sz, motion.override)
			measured.Store(e.a)
			ch = fakeslack.ThreadPublicID
			e.openChannel(ch)
			e.until("history", 20*time.Second, func() bool { return e.loadedMessages(ch) > 0 })
			for _, page := range []string{"threads", "later", "dms", "files", "huddles", "unreads", "drafts", "saved"} {
				e.a.Navigate(page)
				e.frame()
				e.idle(t, p, page, false)
			}
			// Activity on the fixture's feed with its search open on a
			// query and the first row it lists open beside the inbox
			// (Alt+Down): the search's rows were described again every
			// frame once an item opened. The inbox's experiments as the
			// account has them (fakeslack's experiments.getByUser doesn't
			// list them: newActShellOn forces the same)
			on := "on"
			inbox := map[string]*string{"activity_inbox": &on, "nested_secondary_view": &on, "secondary_view_default_width": &on,
				"activity_inbox_remove_persistent_detail_panel": &on, "activity_entry_layout_revisions_2": &on, "activity_global_settings": &on}
			e.a.Core().SetExperimentGroups(inbox)
			e.a.Navigate("activity")
			e.frame()
			e.until("the activity feed", 20*time.Second, func() bool { return e.fs.CallCount("activity.feed") > 0 })
			e.settle(time.Second)
			act, _ := e.a.views[Route("activity")].(*views.Activity)
			if act == nil {
				t.Fatal("no Activity view")
			}
			act.Search("e")
			e.frame()
			// Alt+Down until a row opens: on a slow runner the search may
			// list nothing yet a frame after it opened
			e.until("an Activity item open with the search open", 20*time.Second, func() bool {
				if !e.a.secondary.open() {
					act.StepItem(1, false)
				}
				return e.a.secondary.open()
			})
			e.idle(t, p, "activity, searching, an item open", true)
			e.a.CloseSecondary()
			for k := range inbox {
				inbox[k] = nil // as experiments.getByUser had them again
			}
			e.a.Core().SetExperimentGroups(inbox)
			e.a.Navigate("directories")
			e.frame()
			e.idle(t, p, "directories", true) // Slack focuses its search field
			if motion.override >= 0 {
				return // the rest with full motion only
			}
			e.shutdown()

			// a pointer resting over the window: hover states (a row's
			// background and actions, the sidebar's row, tooltips after
			// their delays) settle, and nothing keeps animating
			sz = fakeslack.Small
			sz.Unread = 0
			sz.ThreadHeader = true
			e = newIdleEnv(t, sz, motion.override)
			measured.Store(e.a)
			ch = fakeslack.ThreadPublicID
			e.openChannel(ch)
			e.until("history", 20*time.Second, func() bool { return e.loadedMessages(ch) > 0 })
			for _, at := range []struct {
				name string
				x, y float32
			}{
				{"pointer on the newest message", 900, 720},
				{"pointer on the message list", 900, 400},
				{"pointer on a sidebar row", 160, 260},
				{"pointer on the channel header", 800, 64},
				{"pointer on the top nav", 700, 18},
				{"pointer on the rail", 34, 180},
			} {
				e.r.Queue(pointer.Event{Kind: pointer.Move, Source: pointer.Mouse, Position: f32.Pt(at.x, at.y)})
				e.frame()
				e.idle(t, p, at.name, true) // opening the channel focused its composer
			}
			e.r.Queue(pointer.Event{Kind: pointer.Leave, Source: pointer.Mouse, Position: f32.Pt(-1, -1)})
			e.frame()

			// a busy workspace's socket about nothing on screen: people
			// Clack never drew changing their profiles and presence, new
			// custom emoji, typing in another channel (Hack Club's stream
			// while the client sits in one channel); ten events a second
			other := ""
			for _, id := range e.fs.ChannelIDs() {
				if id != ch {
					other = id
					break
				}
			}
			stop := make(chan struct{})
			go func() {
				select { // idle's settle
				case <-time.After(2500 * time.Millisecond):
				case <-stop:
					return
				}
				tick := time.NewTicker(100 * time.Millisecond)
				defer tick.Stop()
				for i := 0; ; i++ {
					select {
					case <-stop:
						return
					case <-tick.C:
					}
					u := fmt.Sprintf("U0IDLETR%03d", i%40)
					switch i % 4 {
					case 0:
						e.fs.Push(map[string]any{"type": "user_change", "user": map[string]any{"id": u, "name": "idle" + u, "real_name": "Idle " + u,
							"profile": map[string]any{"display_name": "idle" + u, "status_emoji": ":wave:", "status_text": fmt.Sprintf("away %d", i)}}})
					case 1:
						pr := "active"
						if i%8 == 1 {
							pr = "away"
						}
						e.fs.Push(map[string]any{"type": "presence_change", "user": u, "presence": pr})
					case 2:
						e.fs.Push(map[string]any{"type": "emoji_changed", "subtype": "add", "name": fmt.Sprintf("idle-emoji-%d", i),
							"value": "https://emoji.slack-edge.com/T0/idle/1.png", "event_ts": fmt.Sprintf("%d.000100", time.Now().Unix())})
					case 3:
						e.fs.Push(map[string]any{"type": "user_typing", "channel": other, "user": u})
					}
				}
			}()
			e.idle(t, p, "realtime events about nothing on screen", true)
			close(stop)

			// a huddle on in the open channel and in another (the header's
			// button, the sidebar's Huddles link and rows)
			if users := e.fs.GroupDMUsers(); len(users) >= 4 {
				now := time.Now().Unix()
				e.fs.PushHuddleRoom("sh_room_join", ch, "R0IDLE0001", []string{users[0].ID, users[1].ID, users[2].ID}, now-120, nil)
				e.fs.PushHuddleRoom("sh_room_join", other, "R0IDLE0002", []string{users[3].ID}, now-60, nil)
				hud := e.a.huddles()
				e.untilSoft(10*time.Second, func() bool { _, on := hud.Live(ch); return on })
				e.idle(t, p, "huddles on in the channel and another", true)
			}
			e.shutdown()

			// every fixture gallery's messages laid out at once (a window
			// tall enough for most of them): Block Kit, link previews,
			// files, an agent's thread, work objects, workflows, pins, bot
			// and app senders
			sz = fakeslack.Small
			sz.Unread = 0
			sz.Gallery, sz.Embeds, sz.Files, sz.AgentThread = true, true, true, true
			sz.WorkObjects, sz.Workflows, sz.PinsFolders, sz.Identity = true, true, true, true
			e = newIdleEnv(t, sz, motion.override)
			measured.Store(e.a)
			e.size = image.Pt(1440, 4000)
			for _, g := range []struct{ name, ch string }{
				{"#blockkit-gallery", fakeslack.GalleryChannelID},
				{"#embeds-gallery", fakeslack.EmbedsChannelID},
				{"#files-gallery", fakeslack.FilesChannelID},
				{"the agent's DM", fakeslack.AgentDMID},
				{"#work-objects", fakeslack.WorkObjectsChannelID},
				{"#workflows-gallery", fakeslack.WorkflowsChannelID},
				{"#pins-gallery", fakeslack.PinsChannelID},
				{"#identity-fixtures", fakeslack.IdentityChannelID},
			} {
				e.openChannel(g.ch)
				if !e.untilSoft(20*time.Second, func() bool { return e.loadedMessages(g.ch) > 0 }) {
					t.Logf("%s: no history loaded", g.name)
				}
				e.idle(t, p, g.name, true)
			}
			e.shutdown()

			// a code channel whose agent works: the sidebar's dot is
			// Slack's slow spinner, pulsing, which turns while the window
			// is active and holds still in the background (indicatorFrame;
			// it drew 30 frames a second there)
			sz = fakeslack.Small
			sz.Unread = 0
			sz.CodeChannels = true
			e = newIdleEnv(t, sz, motion.override)
			measured.Store(e.a)
			ch = e.fs.ChannelIDs()[0]
			e.openChannel(ch)
			e.untilSoft(20*time.Second, func() bool { return e.loadedMessages(ch) > 0 })
			e.idle(t, p, "a working code channel in the sidebar", true, "ui.codeSpinner")
			e.blur()
			e.idle(t, p, "a working code channel in the sidebar, window in the background", false)
			e.unblur()
		})
	}
}

// untilSoft runs frames until cond holds or d passes, and reports which.
func (e *perfEnv) untilSoft(d time.Duration, cond func() bool) bool {
	for dl := time.Now().Add(d); time.Now().Before(dl); time.Sleep(time.Millisecond) {
		if cond() {
			e.frame()
			return true
		}
		e.frame()
	}
	return false
}

// idleOtherFrames is what a surface may draw in idleFor besides caret
// blinks: a minute tick, with room for two more. indicatorEvery is an
// indicator's frame interval while the window is active (indicatorFrame).
const (
	idleFor         = 10 * time.Second
	idleOtherFrames = 3
	indicatorEvery  = 33 * time.Millisecond
)

// newIdleEnv is startEnv with live updates on and motion set (-1 full
// motion, 1 reduced), on its own fake.
func newIdleEnv(t *testing.T, sz fakeslack.Size, motion int) *perfEnv {
	fs, err := fakeslack.Start(sz)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { fs.Close() })
	home := t.TempDir()
	e := &perfEnv{tb: t, fs: fs, home: home, size: image.Pt(1440, 900), now: time.Now(), start: time.Now()}
	images := imgcache.New(fs.Fetch, "", nil)
	e.a = New(Options{ConfigDir: home, Started: e.start}, images)
	on := true
	e.a.prefs.Realtime = &on // the first-run banner's "Turn on"
	e.a.motionOverride = motion
	e.a.focused = true // App.Run: the first ConfigEvent of a shown window
	cache := filepath.Join(home, "cache")
	svc := core.New(slack.New(fs.Creds()), store.New(cache))
	e.a.Attach(svc, cache)
	images.SetInvalidate(e.a.Invalidate) // what Run does once the window exists
	e.a.th.Images = images
	if installHooks != nil {
		installHooks(e.a)
	}
	evs, stop := svc.Subscribe()
	e.a.data.start(e.a)
	t.Cleanup(e.shutdown)
	e.waitReady()
	dl := time.After(20 * time.Second)
	for up := false; !up; {
		select {
		case ev := <-evs:
			up = ev.Type == "connection" && ev.Connected
		case <-dl:
			t.Fatal("no realtime connection")
		}
	}
	stop()
	return e
}

// blur is the window losing the focus as Gio reports it: a pointer
// Cancel and a FocusEvent{Focus: false} to the focused tag (Window's
// ConfigEvent), then App.Run's a.focused.
func (e *perfEnv) blur() {
	e.r.Queue(pointer.Event{Kind: pointer.Cancel}, key.FocusEvent{Focus: false})
	e.a.focused = false
	e.frame()
}

func (e *perfEnv) unblur() {
	e.r.Queue(key.FocusEvent{Focus: true})
	e.a.focused = true
	e.frame()
}

// focusComposer puts the key focus in a conversation's composer, as
// Slack does when a conversation opens (and the reply bar does for a
// thread, FocusThreadComposer).
func (e *perfEnv) focusComposer(ch, ts string) {
	e.frame()
	c := e.a.convoFor(ch, ts)
	if c == nil || c.comp == nil {
		e.tb.Fatalf("no composer for %s/%s", ch, ts)
	}
	c.comp.Focus()
	e.frame()
	e.frame()
}

// idleFrame is one App.Run FrameEvent at now (layout and the router; no
// GPU).
func (e *perfEnv) idleFrame(now time.Time) {
	a := e.a
	a.drain()
	e.now = now
	gtx := e.context()
	before := a.router.Current()
	a.frame(gtx)
	if a.router.Current() != before {
		gtx = e.context()
		a.frame(gtx)
	}
	a.revealFrame()
	e.r.Frame(&e.ops)
}

// idle settles the surface (loads, opening animations), then counts 10 s
// of frames with no input and checks them against the budget. caret is
// whether a text field has the focus (Slack focuses the composer when a
// conversation opens). indicators name the callers of an indicator that
// turns as long as its state holds (a working agent's spinner): their
// frames count apart, at most one per indicatorEvery, and only in an
// active window.
func (e *perfEnv) idle(t *testing.T, p *idleprobe.Probe, name string, caret bool, indicators ...string) {
	t.Helper()
	e.settle(2 * time.Second)
	// core's deferred boot loads (usergroups, client extras: a.later 5 s
	// after boot) are start-up, not idle
	if d := 8*time.Second - time.Since(e.a.data.booted); d > 0 {
		e.settle(d)
	}
	res := p.Run(name, idleFor, &e.r, e.idleFrame)
	res.Log(t)
	blinks := res.CountMatching("Editor).Layout")
	anim := 0
	if len(indicators) > 0 {
		anim = res.CountMatching(append([]string{"Editor).Layout"}, indicators...)...) - blinks
	}
	other := res.Count() - blinks - anim
	maxAnim := 0
	if e.a.focused && len(indicators) > 0 {
		maxAnim = int(idleFor/indicatorEvery) + 2
	}
	if anim > maxAnim {
		t.Errorf("%s: %d indicator frames in %v idle, want at most %d (focused window: %v)", name, anim, idleFor, maxAnim, e.a.focused)
	}
	maxBlinks := 0
	if blink := widget.CaretBlinkInterval(); caret && e.a.focused && blink > 0 {
		// 500 ms, or Windows' GetCaretBlinkTime once a window opened
		// in this process (Gio patch 13); none when blinking is off
		maxBlinks = int(idleFor/blink) + 1
	}
	if blinks > maxBlinks {
		t.Errorf("%s: %d caret frames in %v idle, want at most %d (focused window: %v)", name, blinks, idleFor, maxBlinks, e.a.focused)
	}
	if other > idleOtherFrames {
		t.Errorf("%s: %d frames in %v idle besides the caret's %d, want at most %d:\n%s", name, other, idleFor, blinks, idleOtherFrames, strings.Join(firstCauses(res, 8), "\n"))
	}
}

// firstCauses lists the first n distinct non-caret causes.
func firstCauses(r idleprobe.Result, n int) []string {
	seen := map[string]bool{}
	var out []string
	for _, f := range r.Frames {
		for _, c := range f.Causes {
			if !strings.Contains(c, "Editor).Layout") && !seen[c] && len(out) < n {
				seen[c] = true
				out = append(out, "\t"+c)
			}
		}
	}
	return out
}
