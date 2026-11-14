package ui

import (
	"fmt"
	"image"
	"strings"

	"gioui.org/io/key"
	"gioui.org/layout"
	"gioui.org/op"

	"github.com/some-du6e/clack/internal/ui/inspect"
	"github.com/some-du6e/clack/internal/ui/slackc"
	"github.com/some-du6e/clack/internal/ui/theme"
)

// The inspect tool (internal/ui/inspect): a Clack developer shortcut for
// bug reports, not a Slack feature, so nothing about it shows until it
// runs. It's off unless CLACK_INSPECT=1 is set or ui.json has
// "inspect_tool": true (no Preferences row); then ⌘⌥⇧D (Ctrl+Alt+Shift+D)
// starts a session, Esc or the shortcut again ends it, and a click copies
// a report of the element under the pointer.

// inspectCombo is the tool's shortcut (internal/ui/shortcut syntax).
const inspectCombo = "Mod+Alt+Shift+D"

// inspectGated reports the tool's gate.
func (a *App) inspectGated() bool { return envFlag("INSPECT") || a.prefs.InspectTool }

// initInspect makes the tool when it's gated on (after loadPrefs).
func (a *App) initInspect() {
	if !a.inspectGated() {
		return
	}
	n, m := parseCombo(inspectCombo)
	a.inspKey = key.Filter{Name: n, Required: m}
	a.insp = inspect.New(inspectHost{a}, a.inspKey)
}

// inspectBegin reads the shortcut before anything else reads keys (it
// works over a modal too), and while a session runs returns the
// hover-only context the window lays out with.
func (a *App) inspectBegin(gtx layout.Context) layout.Context {
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
