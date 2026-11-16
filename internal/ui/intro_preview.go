package ui

import (
	"context"
	"errors"
	"image"
	"log"
	"os"
	"strings"
	"time"

	"gioui.org/io/event"
	"gioui.org/io/input"
	"gioui.org/io/pointer"
	"gioui.org/layout"
	"gioui.org/op"
	"gioui.org/op/clip"
	"gioui.org/unit"

	"github.com/some-du6e/clack/internal/core"
	"github.com/some-du6e/clack/internal/demo"
	"github.com/some-du6e/clack/internal/imgcache"
	"github.com/some-du6e/clack/internal/slack"
	"github.com/some-du6e/clack/internal/store"
	"github.com/some-du6e/clack/internal/ui/composer"
	"github.com/some-du6e/clack/internal/ui/messages"
	"github.com/some-du6e/clack/internal/ui/popups"
	"github.com/some-du6e/clack/internal/ui/richview"
	"github.com/some-du6e/clack/internal/ui/slackc"
	"github.com/some-du6e/clack/internal/ui/theme"
)

// The "your Slack, 1:1" beat shows Clack itself, not a drawing of it: a
// second App signed in to the demo workspace (internal/demo: Quillfeather
// Paper Co., a fakeslack in this process on loopback), laid out live in
// the beat's card at a smaller scale. Its rail, top bar, sidebar sections,
// channel header, messages, thread pane and composer are the app's own
// components; it draws them in the window's theme (the account's colours
// and mode) with the window's shaper and Slack prefs, and shares nothing
// else with the signed-in session:
//
//   - its own core, store (a temporary folder) and image cache, over the
//     demo's loopback fake: it never sees the account's data or network,
//     and the account never sees its data
//   - no ui.json (ConfigDir ""), no plugins, no Block Kit runtime, and the
//     backend never subscribes to its core, so it posts no notification,
//     badge, sound or toast, and never touches the window's globals
//   - it reads a private input.Router that nothing is ever queued to, and a
//     shield over the card takes the window's pointer, so clicks, keys and
//     typing never reach it and it never sends anything
//   - while it lays out, the window's plugins are out of the way (slackc
//     boundaries draw native, the Taut thunks and mention labels are
//     detached, its mentions read its own store) and the message lists'
//     last host stays the window's
//
// The demo session starts with the introduction (it takes a moment to
// come up) and goes with it; the App is laid out only while the beat is on
// screen, and a few times beforehand so its history and avatars are in,
// always at previewW×previewH dp.

// previewW, previewH are the preview window's size in its own dp: the
// card shows it at cardW/previewW of the window's scale.
const (
	previewW = 1200
	previewH = 600
	cardW    = 660 // the card in stage units
	cardH    = 330
)

// paneIntro is the preview's view container: its lists keep their own
// state, apart from the window's ("" and paneSplit).
const paneIntro = "intro"

type introPreview struct {
	host   *App
	app    *App          // nil until the demo session is up
	sess   *demo.Session //
	dir    string        // the preview store's temporary folder
	closed bool
	fail   bool // the demo session didn't start (or CLACK_NO_INTRO_PREVIEW)

	r      input.Router // never fed: the preview's widgets get no events
	empty  op.Ops       // the router's frames
	scrap  op.Ops       // warm-up layouts (not drawn)
	shield int          // the pointer tag over the card

	readyAt  time.Time // the window's frame time when the sidebar came in
	warmAt   time.Time // the last warm-up layout
	warmDone bool
	thread   bool            // the thread pane went up
	card     image.Rectangle // where draw put it last (device px; tests)
	thGen    uint64
	thDark   bool
	thSet    bool
}

// startIntroPreview starts the demo session behind the beat's preview
// (off the UI goroutine). CLACK_NO_INTRO_PREVIEW=1 keeps it off.
func startIntroPreview(host *App) *introPreview {
	p := &introPreview{host: host}
	if envFlag("NO_INTRO_PREVIEW") {
		p.fail = true
		return p
	}
	go func() {
		var (
			s   *demo.Session
			c   *slack.Client
			cc  *core.App
			dir string
			err error
		)
		s, err = demo.Start(demo.Options{Quiet: true, Seed: 1})
		if err == nil {
			c, err = s.NewClient()
		}
		if err == nil {
			dir, err = os.MkdirTemp("", "clack-intro-preview-")
		}
		if err == nil {
			cc = core.New(c, store.New(dir)) // the store reads its folder: not on the UI goroutine
		}
		host.Post(func() {
			switch {
			case err != nil:
				log.Printf("ui: introduction preview: %v", err)
				p.fail = true
				if s != nil {
					go s.Close()
				}
				removePreviewDir(dir)
			case p.closed:
				go func() {
					_ = cc.Close()
					s.Close()
					removePreviewDir(dir)
				}()
			default:
				p.sess, p.dir = s, dir
				p.app = newPreviewApp(host, s, cc, dir)
			}
		})
	}()
	return p
}

// removePreviewDir deletes the preview store's folder: only one
// os.MkdirTemp made under the temp directory with the preview's prefix.
func removePreviewDir(dir string) {
	if dir == "" || !strings.HasPrefix(dir, os.TempDir()) || !strings.Contains(dir, "clack-intro-preview-") {
		return
	}
	_ = os.RemoveAll(dir)
}

// newPreviewApp is the preview's App over demo session s: New's fields
// without anything New installs in the process (theme.SetPrimary, the
// composer's and tooltips' window hooks, spellcheck, locale, downloads,
// ui.json), over core cc (the demo's client), which it boots. UI
// goroutine.
func newPreviewApp(host *App, s *demo.Session, cc *core.App, dir string) *App {
	images := imgcache.New(s.Fetch, "", nil)
	a := &App{
		opts:   Options{Started: time.Now(), NoRealtime: true},
		images: images,
		clicks: clickables{},
		views:  map[Route]View{},
		// its redraws are the window's: Invalidate reaches the window
		// through host's invalidator (never Window.Invalidate here)
		inval:          host.inval,
		updNow:         make(chan struct{}, 1),
		updWake:        make(chan struct{}, 1),
		pane:           paneIntro,
		motionOverride: 1, // a still picture: no list or hover animations
	}
	a.th = new(theme.Theme)
	a.sidebar.width = theme.SidebarW
	a.keys = a.defaultShortcuts()
	a.syncDownloadPrefs() // its own (empty) download folder, no popups hooks
	images.SetInvalidate(a.Invalidate)
	messages.Isolate(cc) // no plugin thunk ever sees its lists, off the UI goroutine too
	b := newBackend(cc, dir)
	a.data = b
	// never b.start: no event subscription (notifications, badges, the
	// account's prefs), no plugins, no Block Kit runtime, no drafts
	b.started, b.app = true, a
	b.ctx, b.cancel = context.WithCancel(context.Background())
	cc.SetRealtime(false)
	go func() {
		if err := cc.Boot(b.ctx); err != nil {
			if !errors.Is(err, context.Canceled) {
				log.Printf("ui: introduction preview: boot: %v", err)
			}
			return
		}
		snap := cc.Snapshot()
		a.Post(func() {
			if b.ctx.Err() != nil {
				return
			}
			b.applyModel(a, snap)
			b.booted = time.Now()
			ch := demo.GeneralID
			a.lastChannel = ch
			a.router.Push(Route("c/" + ch)) // not Navigate: no recent places, no Slick route
			cc.SetSelected(ch)
			b.opened(ch) // the header: tabs, bookmarks, members
		})
	}()
	return a
}

// ready reports whether the preview has its sidebar (UI goroutine).
func (p *introPreview) ready() bool {
	return p != nil && p.app != nil && !p.closed && !p.app.data.booted.IsZero()
}

// failed reports whether the preview will never come up (nil-safe).
func (p *introPreview) failed() bool { return p == nil || p.fail }

// close tears the preview down (idempotent; UI goroutine).
func (p *introPreview) close() {
	if p == nil || p.closed {
		return
	}
	p.closed = true
	a, s, dir := p.app, p.sess, p.dir
	p.app, p.sess = nil, nil
	if a == nil {
		return
	}
	b := a.data
	b.cancel()
	b.core.StopTraffic()
	messages.Release(b.core)
	go func() {
		_ = b.core.Close()
		s.Close()
		removePreviewDir(dir)
	}()
}

// pump runs the preview's posted work (its boot, the header's reads):
// every window frame while the introduction is up, drawn or not.
func (p *introPreview) pump(now time.Time) {
	if p == nil || p.app == nil || p.closed {
		return
	}
	p.app.drain()
	if p.ready() {
		if p.readyAt.IsZero() {
			p.readyAt = now
		}
		p.openThread()
	}
}

// update, until the beat shows the preview, lays it out unseen a few
// times so its history, thread and avatars are in by then. Every frame
// that draws the beats.
func (p *introPreview) update(f *introFrame, showing bool) {
	if !p.ready() {
		return
	}
	a := p.app
	if showing {
		p.warmDone = true // from here on it is laid out only on screen
	}
	if p.warmDone {
		return
	}
	// unseen layouts: every 250 ms of real time for 3 s
	now := time.Now()
	if now.Sub(p.warmAt) < 250*time.Millisecond {
		return
	}
	p.warmAt = now
	if now.Sub(a.data.booted) > 3*time.Second {
		p.warmDone = true
	}
	r := p.cardRect(f)
	p.scrap.Reset()
	g := f.gtx
	g.Ops = &p.scrap
	p.layout(g, r.Size(), f)
}

// openThread opens the newest thread of #general's history in the
// secondary pane, once (the copy says threads).
func (p *introPreview) openThread() {
	if p.thread {
		return
	}
	a := p.app
	tl := a.data.core.S.Timeline(demo.GeneralID)
	if tl == nil {
		return
	}
	for i := len(tl.Msgs) - 1; i >= 0; i-- {
		if m := tl.Msgs[i]; m.ReplyCount > 0 && m.ThreadTS == m.TS {
			p.thread = true
			a.OpenThread(demo.GeneralID, m.TS)
			return
		}
	}
}

// cardRect is the card in device px: centred in the stage, cardW×cardH
// stage units.
func (p *introPreview) cardRect(f *introFrame) image.Rectangle {
	k := f.kit(f.in.geom(f))
	return k.rect((k.w-cardW)/2, 0, cardW, cardH)
}

// syncTheme copies the window's theme (the account's colours, mode,
// tokens and shaper) into the preview's, keeping its own image cache.
func (p *introPreview) syncTheme() {
	h, a := p.host, p.app
	if p.thSet && p.thGen == h.th.Gen && p.thDark == h.th.Dark {
		return
	}
	*a.th = *h.th
	a.th.Images = a.images
	a.th.Chrome = false
	a.acct = h.acct
	p.thGen, p.thDark, p.thSet = h.th.Gen, h.th.Dark, true
}

// layout lays the preview App out at size device px (its previewW×previewH
// dp) at gtx's origin, on the preview's own router. The window's plugin
// hooks are swapped out meanwhile (see the top of the file).
func (p *introPreview) layout(gtx layout.Context, size image.Point, f *introFrame) {
	a := p.app
	p.syncTheme()
	k := float32(size.X) / previewW // device px per preview dp
	spdp := float32(1)
	if gtx.Metric.PxPerDp > 0 {
		spdp = gtx.Metric.PxPerSp / gtx.Metric.PxPerDp
	}
	pg := layout.Context{
		Ops:         gtx.Ops,
		Now:         gtx.Now,
		Locale:      gtx.Locale,
		Metric:      unit.Metric{PxPerDp: k, PxPerSp: k * spdp},
		Constraints: layout.Exact(size),
		Source:      p.r.Source(),
	}
	a.pxPerDp, a.winSize = k, size
	a.frameSeq.Add(1)
	mt := messages.SwapTaut(nil)
	ct := composer.SwapTaut(nil)
	pt := popups.SwapTaut(nil)
	lh := messages.SwapLastHost(nil)
	mr := richview.SwapMemberRead(memberReadFor(a.data.core))
	ml := richview.SwapMemberLabel(nil)
	defer func() {
		messages.SwapTaut(mt)
		composer.SwapTaut(ct)
		popups.SwapTaut(pt)
		messages.SwapLastHost(lh)
		richview.SwapMemberRead(mr)
		richview.SwapMemberLabel(ml)
	}()
	slackc.Unpatched(func() layout.Dimensions {
		// the window's ancestor chain for stylesheets, as frame has it
		client := clsClient.Enter(pg, 0, 0)
		ia := clsIA.Enter(pg, 0, 0)
		a.layoutShell(pg)
		ia.Leave()
		client.Leave()
		return layout.Dimensions{Size: size}
	})
	// the router saw no input and never will; its frame only drops what
	// the layout asked of it (filters, focus) so nothing piles up
	p.empty.Reset()
	p.r.Frame(&p.empty)
}

// draw lays the preview out in r (device px) under the card's rounded
// clip, then the shield over it: the window's pointer stops at the card.
func (p *introPreview) draw(f *introFrame, r image.Rectangle, rad int) {
	ops := f.gtx.Ops
	p.card = r
	func() {
		defer clip.UniformRRect(r, rad).Push(ops).Pop()
		defer op.Offset(r.Min).Push(ops).Pop()
		p.layout(f.gtx, r.Size(), f)
	}()
	area := clip.Rect(r).Push(ops)
	event.Op(ops, &p.shield)
	pointer.CursorDefault.Add(ops)
	area.Pop()
}
