package ui

import (
	"fmt"
	"image"
	"slices"
	"strings"

	"gioui.org/io/input"
	"gioui.org/io/key"
	"gioui.org/layout"
	"gioui.org/op"

	"github.com/some-du6e/clack/internal/ui/blockkit"
	"github.com/some-du6e/clack/internal/ui/inspect"
	"github.com/some-du6e/clack/internal/ui/popups"
	"github.com/some-du6e/clack/internal/ui/shortcut"
	"github.com/some-du6e/clack/internal/ui/slackc"
	"github.com/some-du6e/clack/internal/ui/theme"
)

// The inspect tool (internal/ui/inspect): a Clack developer shortcut for
// bug reports, not a Slack feature, so nothing about it shows until it
// runs (no Preferences row, no menu item). The Konami code typed in the
// main window, ↑ ↑ ↓ ↓ ← → ← → B A, turns it on at once and keeps it on in
// ui.json ("inspect_tool": true); the code again turns it off.
// CLACK_INSPECT=1 turns it on too, read at launch like ui.json. Then
// Hyper+D (⌃⌥⇧⌘D; Ctrl+Alt+Shift+D on Linux and Windows) starts a
// session, Esc or Hyper+D again ends it, and a click captures the element
// under the pointer for a review where rects can be blurred out before
// Enter copies the report (a drag does for the region dragged out; a
// Shift+click copies at once). F freezes the window on the frame on
// screen, hover states included, and the picker and the captures work on
// that frame (inspect/freeze.go): inspectFirst draws the frozen frames,
// with nothing laid out.

// inspectFilter is the tool's shortcut, Hyper+D: all four modifiers (off
// macOS Mod is Ctrl, so three). Super may be held as well: a Linux Hyper
// key can add it to the other four. inspectHint shows the same combo.
func inspectFilter() key.Filter {
	n, m := shortcut.Parse("Mod+Alt+Ctrl+Shift+D")
	return key.Filter{Name: n, Required: m, Optional: key.ModSuper}
}

// inspectHint is the shortcut as this platform writes it: ⌃⌥⇧⌘D on
// macOS, Ctrl+Alt+Shift+D elsewhere.
func inspectHint() string { return shortcut.Format("Mod+Alt+Ctrl+Shift+D") }

// inspectGated reports the tool's gate.
func (a *App) inspectGated() bool { return envFlag("INSPECT") || a.prefs.InspectTool }

// initInspect makes the tool when it's gated on and not made yet (after
// loadPrefs, and when the Konami code turns it on).
func (a *App) initInspect() {
	if !a.inspectGated() || a.insp != nil {
		return
	}
	a.inspKey = inspectFilter()
	a.insp = inspect.New(inspectHost{a}, a.inspKey)
	a.inspOn.Store(true) // huddleui's windows run theirs (InspectOn)
}

// setInspectTool turns the tool on or off at once, and for the next
// launches (ui.json; CLACK_INSPECT=1 still turns it on at launch).
func (a *App) setInspectTool(on bool) {
	a.prefs.InspectTool = on
	a.savePrefs()
	if on {
		a.initInspect()
		a.Toast("Inspect tool on: " + inspectHint() + " to inspect")
		return
	}
	if a.insp != nil {
		a.insp.Close() // a session, a pick's included, ends
		a.insp = nil
	}
	a.inspOn.Store(false) // and huddleui's windows' (their next frame)
	a.Toast("Inspect tool off")
}

// The tool in huddleui's windows (huddleui/inspect.go: the huddle window,
// a screen share popped out, the presenter's panel, an invite window, the
// join preview): each runs its own event loop, so each runs a tool of its
// own, on while the main window's is. huddleui.InspectHost.

// InspectOn reports that the tool is on (any goroutine).
func (a *App) InspectOn() bool { return a.inspOn.Load() }

// InspectKey is the tool's shortcut (inspectFilter).
func (a *App) InspectKey() key.Filter { return inspectFilter() }

// InspectConfigDir is where reports are saved.
func (a *App) InspectConfigDir() string { return a.opts.ConfigDir }

// InspectVersion is Clack's version for the reports.
func (a *App) InspectVersion() string { return buildVersion }

// InspectHold holds the animated images while a window is frozen (on) or
// lets one window's hold go: the cache holds them while any window's
// freeze does, the main window's included (any goroutine).
func (a *App) InspectHold(on bool) {
	h := &a.liveHolds
	h.Lock()
	defer h.Unlock()
	switch {
	case on:
		if h.n++; h.n == 1 {
			a.images.HoldLive(true)
		}
	case h.n > 0:
		if h.n--; h.n == 0 {
			a.images.HoldLive(false)
		}
	}
}

// konamiCode turns the inspect tool on, and again off.
var konamiCode = [...]key.Name{
	key.NameUpArrow, key.NameUpArrow, key.NameDownArrow, key.NameDownArrow,
	key.NameLeftArrow, key.NameRightArrow, key.NameLeftArrow, key.NameRightArrow,
	"B", "A",
}

// konami follows the Konami code through the window's key presses.
type konami struct {
	n int // the code's first n keys are the last ones pressed
}

// press takes the next key press (Gio patch 28 keeps modifier keys' own
// out) and reports the code complete. Shift may be held (a capital B);
// with any other modifier the press is a shortcut and starts it over.
func (k *konami) press(e key.Event) bool {
	if e.Modifiers&^key.ModShift != 0 {
		k.n = 0
		return false
	}
	// the longest start of the code the presses end with: after ↑ ↑ a
	// third ↑ still leaves ↑ ↑
	for m := min(k.n+1, len(konamiCode)); m > 0; m-- {
		if konamiCode[m-1] == e.Name && slices.Equal(konamiCode[:m-1], konamiCode[k.n-m+1:k.n]) {
			if m == len(konamiCode) {
				k.n = 0
				return true
			}
			k.n = m
			return false
		}
	}
	k.n = 0
	return false
}

// inspectFirst runs first in every frame of the main window, App.Run's
// and the tests', after the posted work and before the window's ops are
// reset (gtx: the frame's context, its ops unused; frameContext). In a
// session it reads the frame's input (inspect.Tool.Frame), and frozen it
// draws the frame on screen again under the tool's drawing: then it
// returns the ops to submit for the frame, with nothing laid out (call
// inspectAfterFrame after them), and nil when the window lays out as
// usual. When a freeze ended (inspectHost.Frozen) it first starts every
// widget's pointer state over where the pointer is (Gio patch 49): no
// widget read its events while the window was frozen.
func (a *App) inspectFirst(gtx layout.Context) *op.Ops {
	var ops *op.Ops
	if a.login != nil {
		// the sign-in screen: no session outlives the workspace it was
		// inspecting (showLogin, signOut and switchAccount end it; this
		// is for any other way there)
		a.closeInspectSession()
	}
	if a.insp != nil && a.insp.Active() {
		// the frame's key presses for the Konami code, read now: a frozen
		// frame lays out nothing (inspectBegin reads the rest)
		a.inspPresses = append(a.inspPresses, gtx.Source.KeyPresses()...)
		ops = a.insp.Frame(gtx)
		if ops != nil {
			a.konamiKeys(nil)
			if a.insp == nil {
				ops = nil // the code turned the tool off: lay the window out
			}
		}
	}
	if a.inspReset {
		a.inspReset = false
		gtx.Source.ResetPointers()
	}
	if ops != nil {
		// the frozen frame's images are drawn again (quiet.go)
		a.images.FrameRedrawn()
	}
	return ops
}

// konamiKeys follows the Konami code through the presses inspectFirst
// read and src's since (src nil: none).
func (a *App) konamiKeys(src *input.Source) {
	presses := a.inspPresses
	a.inspPresses = nil
	if src != nil {
		presses = append(presses, src.KeyPresses()...)
	}
	for _, e := range presses {
		if a.konami.press(e) {
			a.setInspectTool(a.insp == nil)
		}
	}
}

// closeInspectSession ends a running session, a frozen one included, and
// forgets the key presses read for the Konami code in it: the sign-in
// screen is coming up (signed out, the session ended, another account),
// and the tool would otherwise read its keys (Return, Tab, the arrows)
// before the card with no banner drawn.
func (a *App) closeInspectSession() {
	a.inspPresses = nil
	if a.insp != nil && a.insp.Active() {
		a.insp.Close()
	}
}

// inspectNotShown says the frame just laid out didn't go to the screen
// (the boot placeholder did): no freeze holds it.
func (a *App) inspectNotShown() {
	if a.insp != nil {
		a.insp.NotShown()
	}
}

// inspectBegin follows the Konami code, reads the shortcut before
// anything else reads keys (it works over a modal too), and while a
// session runs returns the hover-only context the window lays out with.
func (a *App) inspectBegin(gtx layout.Context) layout.Context {
	// every key press, whichever widget takes it (the composer takes the
	// arrows and the letters): Gio patch 28. Once per frame: a frame
	// laid out again reads none.
	a.konamiKeys(&gtx.Source)
	if a.insp == nil {
		return gtx
	}
	if !a.insp.Active() {
		for {
			e, ok := gtx.Event(a.inspKey)
			if !ok {
				break
			}
			if ke, ok := e.(key.Event); ok && ke.State == key.Press {
				if a.ptrOK {
					a.insp.SeedPointer(a.ptrWin)
				}
				a.insp.Toggle()
			}
		}
	}
	return a.insp.Begin(gtx)
}

// inspecting reports a running inspect session. While one runs the
// window lays out no system.ActionInputOp areas (titlebar.go): the
// system acts on a press over one before any handler sees it (on macOS,
// X11 and Windows a press on the top nav's drag area starts moving the
// window, a caption button minimizes or closes it), so the picker could
// never pick the top nav's bare parts and a region dragged from there
// would move the window instead.
func (a *App) inspecting() bool { return a.insp != nil && a.insp.Active() }

// inspectEnd draws the session's outline, label and banner over
// everything (gtx: the frame's own context).
func (a *App) inspectEnd(gtx layout.Context) {
	if a.insp != nil {
		a.insp.End(gtx)
	}
}

// inspectAfterFrame shoots a pick's frame once its ops are complete.
func (a *App) inspectAfterFrame(ops *op.Ops, size image.Point) {
	if a.insp != nil {
		a.insp.AfterFrame(ops, size)
	}
}

// inspectHost is the App as the tool sees it.
type inspectHost struct{ a *App }

func (h inspectHost) Theme() *theme.Theme { return h.a.th }
func (h inspectHost) Toast(text string)   { h.a.Toast(text) }
func (h inspectHost) Post(f func())       { h.a.Post(f) }
func (h inspectHost) Invalidate()         { h.a.Invalidate() }
func (h inspectHost) ConfigDir() string   { return h.a.opts.ConfigDir }

// Frozen holds the animated images while the window is frozen (the
// frozen frame's would play on) and, when the freeze ends, asks the next
// frame to start the pointer state over (inspectFirst).
func (h inspectHost) Frozen(on bool) {
	h.a.InspectHold(on)
	if !on {
		h.a.inspReset = true
	}
	h.a.Invalidate()
}

func (h inspectHost) ConvKind(id string) string {
	c := h.a.model().conv(id)
	if c == nil {
		return ""
	}
	switch c.Kind {
	case "channel":
		return "public"
	case "im":
		return "dm"
	case "mpim":
		return "mpdm"
	}
	return c.Kind // private, app
}

// Regions are the shell's columns this frame (px), outermost first.
func (h inspectHost) Regions() []inspect.Region {
	a := h.a
	sz := a.winSize
	px := func(dp float32) int { return int(dp*a.pxPerDp + .5) }
	navH, railW, inset := px(float32(theme.TopNavH)), px(float32(theme.RailW)), px(float32(theme.PanelInset))
	out := []inspect.Region{
		{Name: "window", Rect: image.Rectangle{Max: sz}},
		{Name: "top nav", Rect: image.Rect(0, 0, sz.X, navH)},
		{Name: "tab rail", Rect: image.Rect(0, navH, railW, sz.Y)},
	}
	if a.bbmOn() {
		return append(out, inspect.Region{Name: "simplified layout", Rect: image.Rect(railW, navH, sz.X-inset, sz.Y-inset)})
	}
	if !a.sidebar.hidden && !pageOwnsWorkspace(a.router.Current()) && a.sidebar.origin.X < a.primaryOrigin.X {
		out = append(out, inspect.Region{Name: "sidebar", Rect: image.Rect(a.sidebar.origin.X, a.sidebar.origin.Y, a.primaryOrigin.X, sz.Y-inset)})
	}
	right := sz.X - inset
	if !a.split.col.Empty() {
		right = a.split.col.Min.X
	}
	out = append(out, inspect.Region{Name: "primary", Rect: image.Rect(a.primaryOrigin.X, a.primaryOrigin.Y, right, sz.Y-inset)})
	if a.secondary.open() && a.secondaryOrigin.X > a.primaryOrigin.X {
		out = append(out, inspect.Region{Name: "secondary (" + secKindName(a.secondary.kind) + ")", Rect: image.Rect(a.secondaryOrigin.X, a.secondaryOrigin.Y, right, sz.Y-inset)})
	}
	if !a.split.col.Empty() {
		out = append(out, inspect.Region{Name: "split view", Rect: a.split.col})
	}
	// the popups over the shell, bottom first: where the pointer is over
	// one, it is the region (what a dragged region takes in, and what the
	// picker outlines where nothing of the popup's was recorded)
	for _, o := range a.overlays {
		if o == nil || o.Dismissed() || invisible(o) {
			continue
		}
		if rr, ok := o.(interface{ Rect() image.Rectangle }); ok {
			if r := rr.Rect(); !r.Empty() {
				out = append(out, inspect.Region{Name: overlayLayerName(o), Rect: r})
			}
		}
	}
	return out
}

// overlayLayerName names overlay o for the inspect tool, as the region a
// pick in it reports: what it is ("menu", "modal", "popover", "emoji
// picker" …) and its Go type, as the report's overlays row names it
// ("menu (popups.Menu)", "modal (blockkit.Stack)").
func overlayLayerName(o Overlay) string {
	o = unwrapOverlay(o)
	typ := strings.TrimPrefix(fmt.Sprintf("%T", o), "*")
	return overlayRole(o, typ) + " (" + typ + ")"
}

// unwrapOverlay is what o shows: through a wrapper (Block Kit's watch),
// what it wraps.
func unwrapOverlay(o Overlay) Overlay {
	for {
		w, ok := o.(interface{ Unwrap() popups.Overlay })
		if !ok {
			return o
		}
		o = w.Unwrap()
	}
}

func overlayRole(o Overlay, typ string) string {
	if r, ok := o.(interface{ InspectRole() string }); ok {
		return r.InspectRole() // an overlay naming itself (popups' date picker, time picker, select list)
	}
	switch o.(type) {
	case *popups.EmojiPicker:
		return "emoji picker"
	case *popups.ProfileCard:
		return "profile card"
	case *popups.Menu:
		return "menu"
	case *popups.Popover:
		return "popover"
	case *popups.Modal, *blockkit.Stack:
		return "modal"
	}
	name := typ[strings.LastIndex(typ, ".")+1:]
	switch {
	case strings.Contains(name, "Modal"), strings.Contains(name, "Dialog"):
		return "modal"
	case strings.Contains(name, "Menu"):
		return "menu"
	case strings.Contains(name, "Picker"):
		return "picker"
	case strings.Contains(name, "Card"):
		return "card"
	case strings.Contains(name, "Popover"), strings.Contains(name, "Peek"):
		return "popover"
	case strings.Contains(name, "Lightbox"):
		return "lightbox"
	}
	return "overlay"
}

// Describer reads the report's window and page fields now (a frozen
// capture's report describes the frozen frame, whatever the window does
// meanwhile) and returns what fills them in, the route through the
// report's redactor.
func (h inspectHost) Describer() func(r *inspect.Report, red *inspect.Redactor) {
	a := h.a
	s := a.pxPerDp
	if s <= 0 {
		s = 1
	}
	window := image.Pt(int(float32(a.winSize.X)/s+.5), int(float32(a.winSize.Y)/s+.5))
	zoom, dark, theme := a.prefs.Zoom, a.th.Dark, a.inspectThemeName()
	font := a.font.applied
	if font == "" {
		font = "lato"
	}
	cur := a.router.Current()
	kind := ""
	if ch := cur.Channel(); ch != "" {
		kind = h.ConvKind(ch)
	}
	pane := ""
	if a.secondary.open() {
		pane = secKindName(a.secondary.kind)
	}
	patches := slackc.PatchedNames()
	var open []string
	for _, o := range a.overlays {
		if o != nil && !o.Dismissed() {
			open = append(open, strings.TrimPrefix(fmt.Sprintf("%T", o), "*"))
		}
	}
	return func(r *inspect.Report, red *inspect.Redactor) {
		r.Version = buildVersion
		r.Scale, r.Window, r.Zoom = s, window, zoom
		r.Dark, r.Theme, r.Font = dark, theme, font
		r.Page = red.Route(string(cur))
		if kind != "" {
			r.Page += " (" + kind + ")"
		}
		if pane != "" {
			r.Page += " · pane " + pane
		}
		r.Patches = append([]string(nil), patches...)
		r.Overlays = append([]string(nil), open...)
	}
}

// inspectThemeName is the account theme as Slack names its palette
// ("aubergine", "custom #…"), "default" without one.
func (a *App) inspectThemeName() string {
	if !a.acct.set {
		return "default"
	}
	t := a.acct.t
	name := t.Primary.Palette
	if t.UseCustomHex && t.Primary.Custom != "" {
		name = "custom " + t.Primary.Custom
	}
	if name == "" {
		name = "account"
	}
	if t.SidebarInverted {
		name += ", inverted sidebar"
	}
	if t.HighContrast {
		name += ", high contrast"
	}
	if !a.acct.gradients {
		name += ", no gradients"
	}
	return name
}

// secKindName names the secondary pane's content for the report.
func secKindName(k secKind) string {
	switch k {
	case secThread:
		return "thread"
	case secDetails:
		return "details"
	case secProfile:
		return "profile"
	case secHelp:
		return "help"
	case secSearch:
		return "search"
	case secCode:
		return "code"
	case secWorkflow:
		return "workflow"
	case secWorkObject:
		return "work object"
	case secEmail:
		return "email"
	}
	return fmt.Sprintf("kind %d", k)
}
