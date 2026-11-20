package huddleui

import (
	"fmt"
	"image"
	"image/color"
	"strings"
	"sync"
	"time"

	"gioui.org/app"
	"gioui.org/io/key"
	"gioui.org/layout"
	"gioui.org/op"

	"github.com/some-du6e/clack/internal/ui/inspect"
	"github.com/some-du6e/clack/internal/ui/popups"
	"github.com/some-du6e/clack/internal/ui/slackc"
	"github.com/some-du6e/clack/internal/ui/theme"
)

// The inspect tool (internal/ui/inspect, docs/INSPECT.md) in huddleui's
// windows: the huddle window, a screen share popped out of it, the
// presenter's panel, an invite window and the join preview. Karim,
// 2026-11-20, on Windows (α.112) in a huddle: "no dev tools" / "they dont
// appear": the tool ran only in the main window. Each of these windows
// runs its own event loop and theme, so each runs a tool of its own while
// the main window's is on (InspectHost), with the same shortcut, picks,
// regions, freeze and review. Its frames are recorded under slackc's frame
// lock (the inspect record is slackc's: one window's frame at a time), its
// popups are layers of the record as the main window's are, its toasts
// are drawn in the window, and its reports are saved and copied as the
// main window's are.

// InspectHost is the ui root's inspect tool as huddleui's windows run it
// (optional on Host: a host without it gives them none).
type InspectHost interface {
	// InspectOn reports that the tool is on (any goroutine).
	InspectOn() bool
	// InspectKey is its shortcut; InspectConfigDir is where reports are
	// saved, InspectVersion the app's version for them.
	InspectKey() key.Filter
	InspectConfigDir() string
	InspectVersion() string
	// InspectHold holds the animated images while a window is frozen (on),
	// or lets that window's hold go: one hold per frozen window, the main
	// window's included (any goroutine).
	InspectHold(on bool)
}

// winInspect is one window's inspect tool. Its goroutine runs it (frame);
// Post, Invalidate and Toast may come from any goroutine.
type winInspect struct {
	h         InspectHost // nil: the host has no tool
	kind      string      // the window, as the report's page and its outermost region
	themeName string      // what the window draws with, for the report
	th        *theme.Theme
	inval     func()
	// regions are the window's areas inside it this frame (px), outermost
	// first; popups lists its open popups (both nil: none)
	regions func(sz image.Point, pxPerDp float32) []inspect.Region
	popups  func() []any

	key   key.Filter
	tool  *inspect.Tool // while the tool is on
	size  image.Point   // the last frame's
	scale float32
	held  bool // a freeze of this window holds the animated images

	mu     sync.Mutex
	posts  []func()
	reset  bool // a freeze ended: the next frame starts the pointer state over
	note   string
	noteAt time.Time
}

// newWinInspect makes the tool of a window of kind (its theme th, and
// inval asking it for a frame from any goroutine).
func (c *Controller) newWinInspect(kind, themeName string, th *theme.Theme, inval func()) *winInspect {
	wi := &winInspect{kind: kind, themeName: themeName, th: th, inval: inval}
	wi.h, _ = c.h.(InspectHost)
	return wi
}

// frame runs one of the window's frames (its FrameEvent; tests drive it
// with a router's): frozen, the frame on screen is drawn again under the
// tool's drawing with nothing laid out; otherwise lay lays the window out
// between the tool's Begin and End, with the hover-only context while a
// session runs. slackc's frame lock is held until the frame is complete.
func (wi *winInspect) frame(e app.FrameEvent, ops *op.Ops, lay func(gtx layout.Context)) {
	slackc.LockFrame()
	if fops := wi.first(frameContext(e, ops)); fops != nil {
		slackc.UnlockFrame()
		e.Frame(fops)
		wi.after(fops, e.Size)
		return
	}
	gtx := app.NewContext(ops, e)
	lay(wi.begin(gtx))
	wi.end(gtx)
	slackc.UnlockFrame()
	e.Frame(gtx.Ops)
	wi.after(gtx.Ops, e.Size)
}

// frameContext is app.NewContext's context for e with ops as the last
// frame left them (not reset): what the tool's Frame reads, and draws a
// frozen frame into.
func frameContext(e app.FrameEvent, ops *op.Ops) layout.Context {
	size := e.Size
	if e.Insets != (app.Insets{}) {
		size.X -= e.Metric.Dp(e.Insets.Left) + e.Metric.Dp(e.Insets.Right)
		size.Y -= e.Metric.Dp(e.Insets.Top) + e.Metric.Dp(e.Insets.Bottom)
	}
	return layout.Context{Ops: ops, Now: e.Now, Source: e.Source, Metric: e.Metric, Constraints: layout.Exact(size)}
}

// first starts a frame, before the window's ops are reset: the tool is
// made or let go of as the main window's is turned on or off, and in a
// session it reads the frame's input (inspect.Tool.Frame), returning the
// frozen frame's ops to submit when frozen.
func (wi *winInspect) first(gtx layout.Context) *op.Ops {
	if wi == nil {
		return nil
	}
	wi.runPosts()
	if wi.h == nil {
		return nil
	}
	switch on := wi.h.InspectOn(); {
	case !on && wi.tool != nil:
		wi.tool.Close()
		wi.tool = nil
	case on && wi.tool == nil:
		wi.key = wi.h.InspectKey()
		wi.tool = inspect.New(wi, wi.key)
	}
	var ops *op.Ops
	if t := wi.tool; t != nil && t.Active() {
		ops = t.Frame(gtx)
	}
	wi.mu.Lock()
	reset := wi.reset
	wi.reset = false
	wi.mu.Unlock()
	if reset {
		gtx.Source.ResetPointers() // no widget read its events while frozen (Gio patch 49)
	}
	return ops
}

// begin reads the shortcut before anything in the window reads keys, and
// in a session returns the hover-only context to lay out with.
func (wi *winInspect) begin(gtx layout.Context) layout.Context {
	if wi == nil {
		return gtx
	}
	wi.size, wi.scale = gtx.Constraints.Max, gtx.Metric.PxPerDp
	t := wi.tool
	if t == nil {
		return gtx
	}
	if !t.Active() {
		for {
			e, ok := gtx.Event(wi.key)
			if !ok {
				break
			}
			if ke, ok := e.(key.Event); ok && ke.State == key.Press {
				t.Toggle()
			}
		}
	}
	return t.Begin(gtx)
}

// end draws the session over everything (gtx: the frame's own context),
// then the tool's toast. The toast comes after the frame a freeze holds.
func (wi *winInspect) end(gtx layout.Context) {
	if wi == nil {
		return
	}
	if t := wi.tool; t != nil {
		t.End(gtx)
	}
	wi.drawNote(gtx)
}

// after shoots a pick's frame once its ops were submitted.
func (wi *winInspect) after(ops *op.Ops, size image.Point) {
	if wi == nil {
		return
	}
	if t := wi.tool; t != nil {
		t.AfterFrame(ops, size)
	}
}

// close: the window is gone (its DestroyEvent): its session ends, and a
// freeze's hold on the images with it.
func (wi *winInspect) close() {
	if wi == nil {
		return
	}
	if t := wi.tool; t != nil {
		t.Close()
		wi.tool = nil
	}
	if wi.held {
		wi.held = false
		wi.h.InspectHold(false)
	}
}

// active reports a running session in the window (tests).
func (wi *winInspect) active() bool { return wi != nil && wi.tool != nil && wi.tool.Active() }

func (wi *winInspect) runPosts() {
	wi.mu.Lock()
	ps := wi.posts
	wi.posts = nil
	wi.mu.Unlock()
	for _, f := range ps {
		f()
	}
}

// The tool's Host: the window as the tool sees it.

func (wi *winInspect) Theme() *theme.Theme { return wi.th }

// Describer fills a report's window and page fields: the window's size and
// scale, its mode and what it draws with, the window as the page.
func (wi *winInspect) Describer() func(r *inspect.Report, red *inspect.Redactor) {
	s := wi.scale
	if s <= 0 {
		s = 1
	}
	window := image.Pt(int(float32(wi.size.X)/s+.5), int(float32(wi.size.Y)/s+.5))
	dark := wi.th != nil && wi.th.Dark
	kind, themeName, version := wi.kind, wi.themeName, wi.h.InspectVersion()
	patches := slackc.PatchedNames()
	var open []string
	if wi.popups != nil {
		for _, o := range wi.popups() {
			open = append(open, strings.TrimPrefix(fmt.Sprintf("%T", o), "*"))
		}
	}
	return func(r *inspect.Report, _ *inspect.Redactor) {
		r.Version = version
		r.Scale, r.Window = s, window
		r.Dark, r.Theme, r.Font = dark, themeName, "lato"
		r.Page = kind
		r.Patches = append([]string(nil), patches...)
		r.Overlays = append([]string(nil), open...)
	}
}

// ConvKind: these windows' reports name no conversation's kind.
func (wi *winInspect) ConvKind(string) string { return "" }

// Regions are the window, then its areas, then its popups (where the
// pointer is over one, it is the region), outermost first.
func (wi *winInspect) Regions() []inspect.Region {
	out := []inspect.Region{{Name: wi.kind, Rect: image.Rectangle{Max: wi.size}}}
	if wi.regions != nil {
		out = append(out, wi.regions(wi.size, wi.scale)...)
	}
	if wi.popups != nil {
		for _, o := range wi.popups() {
			if r := popupRect(o); !r.Empty() {
				out = append(out, inspect.Region{Name: popupName(o), Rect: r})
			}
		}
	}
	return out
}

func (wi *winInspect) Toast(text string) {
	wi.mu.Lock()
	wi.note, wi.noteAt = text, time.Now()
	wi.mu.Unlock()
	wi.Invalidate()
}

func (wi *winInspect) Post(f func()) {
	wi.mu.Lock()
	wi.posts = append(wi.posts, f)
	wi.mu.Unlock()
	wi.Invalidate()
}

func (wi *winInspect) Invalidate() {
	if wi.inval != nil {
		wi.inval()
	}
}

func (wi *winInspect) ConfigDir() string { return wi.h.InspectConfigDir() }

// Frozen holds the animated images while the window is frozen and, when
// the freeze ends, has the next frame start the pointer state over
// (first).
func (wi *winInspect) Frozen(on bool) {
	if on != wi.held {
		wi.held = on
		wi.h.InspectHold(on)
	}
	if !on {
		wi.mu.Lock()
		wi.reset = true
		wi.mu.Unlock()
	}
	wi.Invalidate()
}

// noteFor is how long the tool's toast shows.
const noteFor = 4 * time.Second

// drawNote draws the tool's toast ("Copied the inspect report…") at the
// bottom of the window, as the main window's toasts sit.
func (wi *winInspect) drawNote(gtx layout.Context) {
	wi.mu.Lock()
	text, at := wi.note, wi.noteAt
	if text != "" && time.Since(at) >= noteFor {
		wi.note, text = "", ""
	}
	wi.mu.Unlock()
	if text == "" || wi.th == nil {
		return
	}
	gtx.Execute(op.InvalidateCmd{At: at.Add(noteFor)}) // it goes
	sz := gtx.Constraints.Max
	pad := gtx.Dp(10)
	lg := gtx
	lg.Constraints = layout.Constraints{Max: image.Pt(max(0, sz.X-4*pad), sz.Y)}
	m := op.Record(gtx.Ops)
	d := wi.th.Label(lg, 13, theme.Bold, hudWhite, text)
	call := m.Stop()
	bottom := sz.Y - gtx.Dp(12)
	r := image.Rect((sz.X-d.Size.X)/2-pad, bottom-d.Size.Y-pad, (sz.X+d.Size.X)/2+pad, bottom)
	theme.RRect(gtx, r, gtx.Dp(8), color.NRGBA{R: 0x1d, G: 0x1c, B: 0x1d, A: 0xee})
	defer op.Offset(image.Pt(r.Min.X+pad, r.Min.Y+pad/2)).Push(gtx.Ops).Pop()
	call.Add(gtx.Ops)
}

// inspectLayer records popup o, about to be laid out over a window, as a
// layer of the inspect record in a session, as the main window's
// drawOverlay records its own: a pick on it reports the popup, boxed at
// its Rect where nothing of its own was recorded. Call the func it returns
// after o's Layout.
func inspectLayer(gtx layout.Context, o any) func() {
	if !slackc.Inspecting() {
		return func() {}
	}
	lm := slackc.InspectOverlay(popupName(o))
	return func() { lm.EndCardOr(gtx, popupRect(o)) }
}

// popupName names a popup for the inspect tool: what it is and its Go
// type, as the main window names its overlays ("menu (popups.Menu)").
func popupName(o any) string {
	typ := strings.TrimPrefix(fmt.Sprintf("%T", o), "*")
	role := "overlay"
	switch x := o.(type) {
	case interface{ InspectRole() string }:
		role = x.InspectRole()
	case *popups.Menu:
		role = "menu"
	case *popups.Modal:
		role = "modal"
	case *popups.Popover:
		role = "popover"
	case *quickPicker:
		role = "emoji picker"
	case *stickerGrid:
		role = "sticker picker"
	}
	return role + " (" + typ + ")"
}

// popupRect is a popup's card as it says (window px), empty when it
// doesn't.
func popupRect(o any) image.Rectangle {
	if r, ok := o.(interface{ Rect() image.Rectangle }); ok {
		return r.Rect()
	}
	return image.Rectangle{}
}
