package ui

import (
	"fmt"
	"image"
	"slices"
	"strings"

	"gioui.org/io/key"
	"gioui.org/layout"
	"gioui.org/op"

	"github.com/some-du6e/clack/internal/ui/inspect"
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
// session, Esc or Hyper+D again ends it, and a click copies a report of
// the element under the pointer. A Shift+click (or a drag, for the region
// dragged out) captures it for a review where rects can be blurred out
// before Enter copies the report.

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
	a.Toast("Inspect tool off")
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

// inspectBegin follows the Konami code, reads the shortcut before
// anything else reads keys (it works over a modal too), and while a
// session runs returns the hover-only context the window lays out with.
func (a *App) inspectBegin(gtx layout.Context) layout.Context {
	// every key press, whichever widget takes it (the composer takes the
	// arrows and the letters): Gio patch 28. Once per frame: a frame
	// laid out again reads none.
	for _, e := range gtx.Source.KeyPresses() {
		if a.konami.press(e) {
			a.setInspectTool(a.insp == nil)
		}
	}
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

func (h inspectHost) Theme() *theme.Theme  { return h.a.th }
func (h inspectHost) Toast(text string)    { h.a.Toast(text) }
func (h inspectHost) CopyText(text string) { h.a.copyText(text) }
func (h inspectHost) Post(f func())        { h.a.Post(f) }
func (h inspectHost) Invalidate()          { h.a.Invalidate() }
func (h inspectHost) ConfigDir() string    { return h.a.opts.ConfigDir }
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
	return out
}

// Describe fills the report's window and page fields.
func (h inspectHost) Describe(r *inspect.Report, red *inspect.Redactor) {
	a := h.a
	r.Version = buildVersion
	s := a.pxPerDp
	if s <= 0 {
		s = 1
	}
	r.Scale = s
	r.Window = image.Pt(int(float32(a.winSize.X)/s+.5), int(float32(a.winSize.Y)/s+.5))
	r.Zoom = a.prefs.Zoom
	r.Dark = a.th.Dark
	r.Theme = a.inspectThemeName()
	r.Font = a.font.applied
	if r.Font == "" {
		r.Font = "lato"
	}
	cur := a.router.Current()
	r.Page = red.Route(string(cur))
	if ch := cur.Channel(); ch != "" {
		if k := h.ConvKind(ch); k != "" {
			r.Page += " (" + k + ")"
		}
	}
	if a.secondary.open() {
		r.Page += " · pane " + secKindName(a.secondary.kind)
	}
	r.Patches = slackc.PatchedNames()
	var open []string
	for _, o := range a.overlays {
		if o != nil && !o.Dismissed() {
			open = append(open, strings.TrimPrefix(fmt.Sprintf("%T", o), "*"))
		}
	}
	r.Overlays = open
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
	}
	return fmt.Sprintf("kind %d", k)
}
