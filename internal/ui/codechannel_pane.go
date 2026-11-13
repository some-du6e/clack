package ui

// The "Code channel details" pane: the views a code channel's agent
// published, in the secondary pane, 1:1 with Slack's web client (build
// a84662d2d135, client-code-channel-details-view ECTe and
// code-channel-view-renderer iuvb). docs/CODE-CHANNELS.md has the evidence.
//
// Slack opens it (toCodeChannelDetails: {id "code_channel_details",
// params {channelId, viewId}}) from a channel-tab slug: a rich_text
// channel element with a tab_id, a /archives/<C>/tabs/<id> link, or the
// inline cards of an agent_view_added message (navigateToChannelTabSlug
// → resolveCodeChannelViewIdForTab → navigateToCodeChannelView), when
// views are on and artifact tabs are off (biaa.PH).
//
// The pane is a flexpane whose title is a tab per view (labelForView,
// svgIconForView; the Code diff first) and whose header has the
// expand/collapse toggle; each tab draws CodeChannelViewRenderer:
// the diff (codechannel_diff.go), html views (Slack's sandboxed iframe;
// Clack shows Slack's "not available in this environment" state), canvases
// (canvasview), Block Kit (blockkit) and pull requests.

import (
	"context"
	"encoding/json"
	"image"
	"image/color"
	"net/url"
	"strings"
	"sync"
	"time"

	"gioui.org/layout"
	"gioui.org/op"
	"gioui.org/op/clip"
	"gioui.org/unit"

	"github.com/some-du6e/clack/internal/core"
	"github.com/some-du6e/clack/internal/ui/blockkit"
	"github.com/some-du6e/clack/internal/ui/canvasview"
	"github.com/some-du6e/clack/internal/ui/icons"
	"github.com/some-du6e/clack/internal/ui/messages"
	"github.com/some-du6e/clack/internal/ui/slackc/css"
	"github.com/some-du6e/clack/internal/ui/theme"
)

// ---- design tokens ----

var (
	ccTokMu sync.Mutex
	ccToks  = map[bool]map[string]color.NRGBA{}
)

// ccTok is a Slack design token (--dt_color-…) in light or dark.
func ccTok(dark bool, name string) color.NRGBA {
	ccTokMu.Lock()
	defer ccTokMu.Unlock()
	m := ccToks[dark]
	if m == nil {
		m = map[string]color.NRGBA{}
		ccToks[dark] = m
	}
	if c, ok := m[name]; ok {
		return c
	}
	c, _ := css.ParseColor(css.SlackVars(dark)["--dt_color-"+name])
	m[name] = c
	return c
}

// ---- opening ----

var _ messages.ChannelTabOpener = (*App)(nil)

// OpenCodeChannelView opens the details pane on ch's view (viewID "" =
// the first). Slack's navigateToCodeChannelView.
func (a *App) OpenCodeChannelView(ch, viewID string) {
	if a.secondary.kind == secCode && a.secondary.channel == ch && !a.secondary.hidden {
		if p, ok := a.secondary.view.(*codePane); ok {
			p.selectView(viewID)
			a.Invalidate()
			return
		}
	}
	w := a.secondary.width
	if w < 520 {
		w = 520
	}
	a.secondary = secondaryState{kind: secCode, channel: ch, width: w}
	p := &codePane{a: a, ch: ch, bodies: map[string]codeViewBody{}, clicks: clickables{}}
	p.selectView(viewID)
	a.secondary.view = p
	a.Invalidate()
}

// codeViewsOn is the pane's gate (CodeChannelViewsOn) on a live core.
func (a *App) codeViewsOn() bool {
	return a.data != nil && a.data.core != nil && a.data.core.CodeChannelViewsOn()
}

// OpenChannelTabLink is navigateToChannelTabSlug for a link: a code
// channel's view tab (/archives/<C>/tabs/<id>, absolute or not, or
// slack://channel?id=<C>&tab=<id>) opens the details pane; other tabs
// open the channel. False when u isn't a channel-tab link.
func (a *App) OpenChannelTabLink(u string) bool {
	ch, tab := channelTabOfLink(u)
	if ch == "" {
		return false
	}
	a.openChannelTab(ch, tab)
	return true
}

func (a *App) openChannelTab(ch, tab string) {
	if a.codeViewsOn() {
		if v := a.data.core.CodeViewForTab(ch, tab); v != "" {
			if a.router.Current().Channel() != ch {
				a.Navigate("c/" + ch)
			}
			a.OpenCodeChannelView(ch, v)
			return
		}
	}
	a.Navigate("c/" + ch)
	a.wantTabLink(ch, tab) // the tab opens once the tab bar has it (files_tab.go)
}

// channelTabOfLink parses getChannelTabPermalink's /archives/<C>/tabs/<id>.
func channelTabOfLink(u string) (ch, tab string) {
	p, err := url.Parse(u)
	if err != nil {
		return "", ""
	}
	if p.Scheme == "slack" {
		q := p.Query()
		if p.Host == "channel" && q.Get("tab") != "" {
			return q.Get("id"), q.Get("tab")
		}
		return "", ""
	}
	if p.Host != "" && !strings.HasSuffix(p.Host, ".slack.com") && p.Host != "slack.com" {
		return "", ""
	}
	parts := strings.Split(strings.Trim(p.Path, "/"), "/")
	if len(parts) == 4 && parts[0] == "archives" && parts[2] == "tabs" && parts[1] != "" && parts[3] != "" {
		return parts[1], parts[3]
	}
	return "", ""
}

// ---- the pane ----

type codeViewBody interface {
	Layout(gtx layout.Context) layout.Dimensions
}

type codePane struct {
	a      *App
	ch     string
	want   string // the view asked for (viewId)
	cur    string // the view shown
	clicks clickables
	bodies map[string]codeViewBody
	keys   map[string]string // view id → the body's identity (type + content), to rebuild on change
}

func (p *codePane) selectView(id string) { p.want, p.cur = id, id }

// paneTab is one tab: a view.
type paneTab struct {
	id, label, icon string
	view            core.CodeChannelView
}

// tabs is CodeChannelDetails' tab list (eb): a tab per view. Slack adds a
// "Files and links" tab (canvas-browser icon) when the channel's tabz has
// a files tab; Clack has no channel files view to put in it yet.
func (p *codePane) tabs() []paneTab {
	a := p.a
	if a.data == nil || a.data.core == nil {
		return nil
	}
	ca := a.data.core
	cc := ca.CodeChannelOf(p.ch)
	var out []paneTab
	for _, v := range ca.CodeChannelViews(cc) {
		title := ""
		if b, ok := p.bodies[v.ViewID].(*codeCanvasBody); ok && v.Type == core.ViewCanvas && v.CanvasID != "" {
			title = b.v.Title() // useCanvasViewTitles: the canvas's title once it's loaded
		}
		out = append(out, paneTab{id: v.ViewID, label: core.CodeViewLabel(v, title), icon: core.CodeViewIcon(v.Type), view: v})
	}
	return out
}

func (p *codePane) Layout(gtx layout.Context) layout.Dimensions {
	a := p.a
	c := &a.th.C
	sz := gtx.Constraints.Max
	tabs := p.tabs()
	cur := -1
	for i, t := range tabs {
		if t.id == p.cur {
			cur = i
		}
	}
	if cur < 0 && len(tabs) > 0 { // ev: the asked view, else the first
		cur = 0
		p.cur = tabs[0].id
	}
	hh := gtx.Dp(48) // .header__KXeRZ .p-flexpane_header__primary{height:48px}
	bs := gtx.Dp(28)
	// the header's buttons, right to left from 12px: close, then the
	// expand toggle (artifacts_expand_toggle), 6px apart
	cx := sz.X - gtx.Dp(12) - bs
	ex := cx - gtx.Dp(6) - bs
	closeBtn, expBtn := p.clicks.get("close"), p.clicks.get("expand")
	at(gtx, image.Pt(cx, (hh-bs)/2), image.Pt(bs, bs), func(gtx layout.Context) layout.Dimensions {
		return iconButton(gtx, closeBtn, "close", 28, 18, c.FgMuted, c.BgHover, 8)
	})
	expIcon := "expand" // Slack's expand-diagonal
	if a.secondary.expanded {
		expIcon = "collapse" // reduce-diagonal
	}
	at(gtx, image.Pt(ex, (hh-bs)/2), image.Pt(bs, bs), func(gtx layout.Context) layout.Dimensions {
		return iconButton(gtx, expBtn, expIcon, 28, 16, c.FgMuted, c.BgHover, 8)
	})
	if clicked(closeBtn) {
		a.secondary.expanded = false
		a.CloseSecondary()
		return layout.Dimensions{Size: sz}
	}
	if clicked(expBtn) {
		a.secondary.expanded = !a.secondary.expanded // "Expand view" / "Collapse view"
		a.Invalidate()
	}
	// the title: the tabs (TabMenu, icons left), else the view's label,
	// else "Code"
	tw := ex - gtx.Dp(8) - gtx.Dp(12)
	if len(tabs) == 0 {
		at(gtx, image.Pt(gtx.Dp(16), 0), image.Pt(tw, hh), func(gtx layout.Context) layout.Dimensions {
			return vcenter(gtx, hh, func(gtx layout.Context) layout.Dimensions {
				return a.th.Label(gtx, 18, theme.Black, c.Fg, "Code")
			})
		})
	} else {
		at(gtx, image.Pt(gtx.Dp(12), 0), image.Pt(tw, hh), func(gtx layout.Context) layout.Dimensions {
			return p.layoutTabs(gtx, tabs, cur)
		})
	}
	theme.Fill(gtx, image.Rect(0, hh, sz.X, hh+gtx.Dp(1)), c.Border)
	body := image.Rect(0, hh+gtx.Dp(1), sz.X, sz.Y)
	atExact(gtx, body.Min, body.Size(), func(gtx layout.Context) layout.Dimensions {
		defer clip.Rect{Max: gtx.Constraints.Max}.Push(gtx.Ops).Pop()
		if cur < 0 {
			return layout.Dimensions{Size: gtx.Constraints.Max}
		}
		return p.body(tabs[cur]).Layout(gtx)
	})
	return layout.Dimensions{Size: sz}
}

// layoutTabs draws the view tabs: .tab__Fz4BC (49px tall, 8px side
// padding, 4px apart), a 16px icon left of a title cut at 100px.
func (p *codePane) layoutTabs(gtx layout.Context, tabs []paneTab, cur int) layout.Dimensions {
	a := p.a
	c := &a.th.C
	h := gtx.Constraints.Max.Y
	x := 0
	maxTitle := gtx.Dp(100)
	for i, t := range tabs {
		lw := min(a.th.Measure(gtx, 13, theme.Bold, t.label), maxTitle)
		w := gtx.Dp(8) + gtx.Dp(16) + gtx.Dp(4) + lw + gtx.Dp(8)
		if x+w > gtx.Constraints.Max.X {
			break
		}
		btn := p.clicks.get("tab." + t.id)
		i, t := i, t
		at(gtx, image.Pt(x, 0), image.Pt(w, h), func(gtx layout.Context) layout.Dimensions {
			return clickArea(gtx, btn, func(gtx layout.Context, s image.Point, hov bool) {
				if hov && i != cur {
					theme.RRect(gtx, image.Rect(0, gtx.Dp(8), s.X, s.Y-gtx.Dp(8)), gtx.Dp(6), c.BgHover)
				}
				if i == cur {
					theme.Fill(gtx, image.Rect(0, s.Y-gtx.Dp(2), s.X, s.Y), c.Accent)
				}
			}, func(gtx layout.Context) layout.Dimensions {
				col := c.FgMuted
				if i == cur {
					col = ccTok(a.th.Dark, "content-pry")
				}
				at(gtx, image.Pt(gtx.Dp(8), (h-gtx.Dp(16))/2), image.Pt(gtx.Dp(16), gtx.Dp(16)), func(gtx layout.Context) layout.Dimensions {
					return icons.Get(codeTabIcon(t.icon))(gtx, 16, col)
				})
				at(gtx, image.Pt(gtx.Dp(28), 0), image.Pt(lw+1, h), func(gtx layout.Context) layout.Dimensions {
					return vcenter(gtx, h, func(gtx layout.Context) layout.Dimensions {
						return a.th.Label(gtx, 13, theme.Bold, col, t.label)
					})
				})
				return layout.Dimensions{Size: image.Pt(w, h)}
			})
		})
		if clicked(btn) && t.id != p.cur {
			p.cur = t.id
			a.Invalidate()
		}
		x += w + gtx.Dp(4)
	}
	return layout.Dimensions{Size: image.Pt(x, h)}
}

// body is the tab's CodeChannelViewRenderer, kept while the view's type
// and content stay the same (content_version bumps rebuild it, as Slack
// refetches then).
func (p *codePane) body(t paneTab) codeViewBody {
	v := t.view
	key := v.Type + "|" + v.FileID + "|" + v.CanvasID + "|" + v.PRURL + "|" + itoa(v.ContentVersion)
	if v.Type == core.ViewDiff {
		if cc := p.a.data.core.CodeChannelOf(p.ch); cc != nil {
			key += "|" + itoa(cc.DiffVersion)
		}
	}
	if p.keys == nil {
		p.keys = map[string]string{}
	}
	if b := p.bodies[t.id]; b != nil && p.keys[t.id] == key {
		return b
	}
	var b codeViewBody
	switch {
	case v.Type == core.ViewDiff:
		b = newCodeDiffView(p.a, p.ch, "")
	case v.Type == core.ViewPullRequest:
		b = newCodeDiffView(p.a, p.ch, v.PRURL)
	case v.Type == core.ViewHTML:
		b = newCodeHTMLView(p.a, p.ch, v, t.label)
	case v.Type == core.ViewCanvas:
		id := v.CanvasID
		if id == "" {
			id = v.FileID
		}
		b = &codeCanvasBody{v: p.a.newCanvasView(id, canvasview.Tab)}
	case v.Type == core.ViewBlockKit:
		b = newCodeBlockKitView(p.a, p.ch, v.ViewID)
	default:
		b = &codeStatusBody{a: p.a}
	}
	p.bodies[t.id] = b
	p.keys[t.id] = key
	return b
}

// codeTabIcon is a view icon (svgIconForView's Slack name) in Clack's set:
// canvas-content has no own glyph, the canvas one stands in.
func codeTabIcon(name string) string {
	if name == "canvas-content" {
		return "canvas"
	}
	return icons.Slack(name)
}

// ---- view bodies ----

// codeStatusBody is a .statusPane: centered text (or nothing).
type codeStatusBody struct {
	a       *App
	text    string
	loading bool
}

func (b *codeStatusBody) Layout(gtx layout.Context) layout.Dimensions {
	sz := gtx.Constraints.Max
	if b.loading {
		// <Spinner size="jumbo"/>: 40px
		d := gtx.Dp(40)
		defer op.Offset(image.Pt((sz.X-d)/2, (sz.Y-d)/2)).Push(gtx.Ops).Pop()
		codeSpinner(gtx, d, b.a.th.C.FgMuted)
		b.a.Invalidate()
		return layout.Dimensions{Size: sz}
	}
	if b.text != "" {
		a := b.a
		w := min(a.th.Measure(gtx, 15, theme.Regular, b.text), sz.X-gtx.Dp(32))
		at(gtx, image.Pt((sz.X-w)/2, sz.Y/2-gtx.Dp(11)), image.Pt(w+1, gtx.Dp(44)), func(gtx layout.Context) layout.Dimensions {
			return a.th.Para(gtx, 15, 22, theme.Regular, a.th.C.Fg, b.text)
		})
	}
	return layout.Dimensions{Size: sz}
}

// codeCanvasBody is CodeChannelCanvasView: the canvas, full height.
type codeCanvasBody struct{ v *canvasview.View }

func (b *codeCanvasBody) Layout(gtx layout.Context) layout.Dimensions { return b.v.Layout(gtx) }

// codeBlockKitView is CodeChannelBlockKitView: "Loading view" (a jumbo
// spinner), "Unable to load this view. Please try again later.", else the
// blocks in a scroller with 16px padding.
type codeBlockKitView struct {
	a       *App
	ch, id  string
	done    bool
	err     error
	surf    *blockkit.Surface
	blocks  json.RawMessage
	sid     string
	appID   string
	list    layout.List
	started bool
}

func newCodeBlockKitView(a *App, ch, id string) *codeBlockKitView {
	return &codeBlockKitView{a: a, ch: ch, id: id, list: layout.List{Axis: layout.Vertical}}
}

func (v *codeBlockKitView) load() {
	v.started = true
	a := v.a
	if a.data == nil || a.data.core == nil {
		v.done, v.err = true, context.Canceled
		return
	}
	ca := a.data.core
	go func() {
		ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
		defer cancel()
		blocks, sid, app, err := ca.CodeChannelBlockKit(ctx, v.ch, v.id)
		a.Post(func() {
			v.done, v.err, v.blocks, v.sid, v.appID = true, err, blocks, sid, app
			a.Invalidate()
		})
	}()
}

func (v *codeBlockKitView) Layout(gtx layout.Context) layout.Dimensions {
	if !v.started {
		v.load()
	}
	if !v.done {
		return (&codeStatusBody{a: v.a, loading: true}).Layout(gtx)
	}
	if v.err != nil {
		return (&codeStatusBody{a: v.a, text: "Unable to load this view. Please try again later."}).Layout(gtx)
	}
	if v.surf == nil {
		v.surf = blockkit.NewCodeChannelSurface(v.a, v.blocks, v.ch, v.id, v.sid, v.appID)
	}
	sz := gtx.Constraints.Max
	return v.list.Layout(gtx, 1, func(gtx layout.Context, _ int) layout.Dimensions {
		return layout.UniformInset(unit.Dp(16)).Layout(gtx, func(gtx layout.Context) layout.Dimensions {
			gtx.Constraints = layout.Constraints{Max: image.Pt(sz.X-gtx.Dp(32), 1<<20)}
			return v.surf.Layout(gtx)
		})
	})
}

// codeDetailsLabel is the pane's view label (core.CodeChannelDetailsLabel:
// "Code channel details" under session_channel_views and
// code_channels_naming), which Slack's breadcrumbs fall back to.
func (a *App) codeDetailsLabel() string {
	if a.data == nil || a.data.core == nil {
		return "Session channel details"
	}
	return a.data.core.CodeChannelDetailsLabel()
}
