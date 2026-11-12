package ui

import (
	"context"
	"encoding/json"
	"image"
	"strings"
	"time"

	"gioui.org/layout"
	"gioui.org/widget"

	"github.com/some-du6e/clack/internal/core"
	"github.com/some-du6e/clack/internal/i18n"
	"github.com/some-du6e/clack/internal/richtext"
	"github.com/some-du6e/clack/internal/slack"
	"github.com/some-du6e/clack/internal/ui/icons"
	"github.com/some-du6e/clack/internal/ui/messages"
	"github.com/some-du6e/clack/internal/ui/richview"
	"github.com/some-du6e/clack/internal/ui/theme"
	"github.com/some-du6e/clack/internal/workobject"
)

// The work object flexpane: Slack's WorkObjectFlexpane (gantry-v2-async
// WIP_work-objects chunks yqt8, bjxP, i4MO; build 4d0c78085232), which a
// work object card opens (ExternalWorkObjectEntity's viewDetails,
// openFlexpane HFuw P):
//
//   - the flexpane header (.workObjectFlexpane .p-flexpane_header: 0 8px
//     0 20px): the title row (flexpaneTitleRow: baseline, 8px apart) of
//     the full layout's header_title or the display type, capitalized
//     (18px/1.33334 black, one line), and the caption (header_subtitle,
//     else the product or app name; 13px content-ter); then
//     WorkObjectFlexpaneAdditionalButtons' Sync data (refresh: a
//     force_refetch of workObjects.getEntity, while refreshable) and the
//     close button
//   - the entity header (header__i5UwZ: 20px padding, a 1.5px otl-ter
//     line under it): the title (22px/1.36365 black, three lines) and
//     WorkObjectFlexpaneDocLink (13px bold link with the new-window
//     glyph, 4px above: the full subtitle, else the product or app name,
//     to app_unfurl_url or the subtitle's URL)
//   - WorkObjectFlexpaneBody's tabs (c-tabs, full width, padding 0 12px):
//     Details; Comments (wo_comment_sync_client_xp and an entity that
//     carries comments: the count after the title); Conversations
//     (entities.refs.list: the count)
//   - Details: WorkObjectDetails (richview's WorkObjectDetails mode)
//   - Comments: WorkObjectComments: the comments (commentItem: 8px 16px,
//     the 36px avatar, the name in black and the relative time, the body
//     4px under), the read-only banner without can_post_comment, the
//     empty state ("Synced with {productName}") without comments, and the
//     composer ("Comment on {productName} {displayType}…")
//   - Conversations: the messages that shared the item, or "It’s quiet.
//     Too quiet…"
//   - the footer (Details): WorkObjectFlexpaneFooter: the full layout's
//     primary actions as outline buttons
//
// The data is the workObjects store's (core.WorkObject): the card's own
// unfurl until entity_details_present brings the live details.

// woPaneNS are the namespaces the flexpane's strings come from.
var (
	woNS      = i18n.NS("work-objects")
	woNSUnder = i18n.NS("work_objects") // the comments' items (EPHX, l7JR)
)

var _ messages.WorkObjectHost = (*App)(nil)

// OpenWorkObject opens the flexpane on o's tab (messages.WorkObjectHost).
func (a *App) OpenWorkObject(o messages.WorkObjectOpen) {
	if o.Entity == nil || o.Entity.EntityID == "" {
		return
	}
	tab := o.Tab
	if tab == "" {
		tab = "details"
	}
	if p, ok := a.secondary.view.(*woPane); ok && a.secondary.kind == secWorkObject && a.secondary.open() && p.o.Entity.EntityID == o.Entity.EntityID {
		p.setTab(tab)
		a.Invalidate()
		return
	}
	p := &woPane{a: a, o: o}
	p.list.Axis = layout.Vertical
	p.comments.Axis = layout.Vertical
	p.editor.Submit = true
	a.secondary = secondaryState{kind: secWorkObject, channel: o.Channel, ts: o.TS, width: a.secondary.width}
	a.secondary.view = p
	if c := a.Core(); c != nil {
		e := o.Entity
		c.FetchWorkObject(core.WorkObjectRequest{EntityID: e.EntityID, EntityURL: e.ExternalURL, Channel: o.Channel, TS: o.TS,
			Unfurl: e.Raw(), Reason: "work-objects-open-flexpane"})
	}
	p.setTab(tab)
	a.Invalidate()
}

// OpenWorkObjectEntity opens the flexpane for a search result
// (popups.WorkObjectOpener).
func (a *App) OpenWorkObjectEntity(e *workobject.Entity, appUnfurlURL, ch, ts, tab string) {
	a.OpenWorkObject(messages.WorkObjectOpen{Entity: e, AppUnfurlURL: appUnfurlURL, Channel: ch, TS: ts, Tab: tab})
}

// ShareWorkObject is the card's Share…: Clack has no ShareWorkObjectModal
// yet (Slack's share dialog with the link going into the chosen
// conversation's composer), so there is nothing to open and the card and
// its menu leave Share out.
func (a *App) ShareWorkObject(o messages.WorkObjectOpen) bool { return false }

// CanShareWorkObjects is messages.WorkObjectSharer: not yet (see
// ShareWorkObject).
func (a *App) CanShareWorkObjects() bool { return false }

// woPane is WorkObjectFlexpane in the secondary pane.
type woPane struct {
	a   *App
	o   messages.WorkObjectOpen
	tab string

	close, sync, send widget.Clickable
	tabs              [3]widget.Clickable
	footer            []widget.Clickable
	refs              []widget.Clickable
	list, comments    layout.List
	editor            widget.Editor

	head, details   *richview.View
	headKey, detKey string
	bodies          map[string]*richview.View // comment bodies by id
	seenOnLeave     float64                   // the card's latest comment when the Comments tab opened
}

func (p *woPane) setTab(tab string) {
	c := p.a.Core()
	if p.tab == "comments" && tab != "comments" && c != nil { // onCommentsTabDismissed
		c.SeeWorkObjectComments(p.o.Entity.EntityID, p.seenOnLeave)
	}
	p.tab = tab
	if c == nil {
		return
	}
	e := p.entity()
	switch tab {
	case "comments":
		if e.Comments != nil {
			p.seenOnLeave = workobject.Number(e.Comments.LatestTimestamp)
		}
		c.FetchWorkObjectComments(e.EntityID, e.ExternalURL)
	case "threads":
		c.FetchWorkObjectRefs(firstNonEmptyStr(e.RelatedConvoID, e.EntityID), e.ExternalURL)
	}
}

// entity is the store's live details, else the card's unfurl.
func (p *woPane) entity() *workobject.Entity {
	if c := p.a.Core(); c != nil {
		if d := c.WorkObject(p.o.Entity.EntityID); d != nil && d.Entity != nil {
			return d.Entity
		}
	}
	return p.o.Entity
}

// title is tq: the full layout's header_title, else the display type
// (or "item"), capitalized.
func (p *woPane) title(e *workobject.Entity) string {
	t := ""
	if f := e.Full(); f != nil {
		t = f.HeaderTitle
	}
	if t == "" {
		t = firstNonEmptyStr(e.DisplayType, i18n.T("solutions", "item")) // tK.t("item", {ns: "solutions"})
	}
	if t == "" {
		return t
	}
	return strings.ToUpper(t[:1]) + t[1:]
}

// caption is tJ: the full header_subtitle, else the product or app name.
func (p *woPane) caption(e *workobject.Entity) string {
	if f := e.Full(); f != nil && f.HeaderSubtitle != "" {
		return f.HeaderSubtitle
	}
	return firstNonEmptyStr(e.ProductName, e.AppName)
}

func (p *woPane) Layout(gtx layout.Context) layout.Dimensions {
	a := p.a
	th := a.th
	c := &th.C
	sz := gtx.Constraints.Max
	e := p.entity()
	co := a.Core()
	var det *core.WorkObjectDetails
	if co != nil {
		det = co.WorkObject(e.EntityID)
	}
	// header
	hh := gtx.Dp(theme.HeaderH)
	bs := gtx.Dp(28)
	cx := sz.X - gtx.Dp(8) - bs
	sx := cx - gtx.Dp(6) - bs
	tx := gtx.Dp(20)
	titleW := sx - gtx.Dp(8) - tx
	title, cap := p.title(e), p.caption(e)
	tw := min(th.Measure(gtx, 18, theme.Black, title), titleW)
	at(gtx, image.Pt(tx, 0), image.Pt(tw+1, hh), func(gtx layout.Context) layout.Dimensions {
		return vcenter(gtx, hh, func(gtx layout.Context) layout.Dimensions {
			return th.Label(gtx, 18, theme.Black, c.Fg, title)
		})
	})
	if cap != "" && tw+gtx.Dp(8) < titleW {
		at(gtx, image.Pt(tx+tw+gtx.Dp(8), gtx.Dp(2)), image.Pt(titleW-tw-gtx.Dp(8), hh), func(gtx layout.Context) layout.Dimensions {
			return vcenter(gtx, hh, func(gtx layout.Context) layout.Dimensions {
				return th.Label(gtx, 13, theme.Regular, ccTok(th.Dark, "content-ter"), cap)
			})
		})
	}
	if e.IsRefreshable() {
		loading := det != nil && det.Loading
		at(gtx, image.Pt(sx, (hh-bs)/2), image.Pt(bs, bs), func(gtx layout.Context) layout.Dimensions {
			col := c.FgMuted
			if loading {
				col = c.FgSubtle
			}
			return iconButton(gtx, &p.sync, "refresh", 28, 18, col, c.BgHover, 8)
		})
	}
	at(gtx, image.Pt(cx, (hh-bs)/2), image.Pt(bs, bs), func(gtx layout.Context) layout.Dimensions {
		return iconButton(gtx, &p.close, "close", 28, 18, c.FgMuted, c.BgHover, 8)
	})
	theme.Fill(gtx, image.Rect(0, hh-gtx.Dp(1), sz.X, hh), c.Border)
	if clicked(&p.close) {
		p.setTab("") // leaving the Comments tab counts as seen
		a.CloseSecondary()
		return layout.Dimensions{Size: sz}
	}
	if clicked(&p.sync) && co != nil { // the flexpane's Sync data: getEntity with force_refetch
		co.FetchWorkObject(core.WorkObjectRequest{EntityID: e.EntityID, EntityURL: e.ExternalURL, Channel: p.o.Channel, TS: p.o.TS,
			Force: true, Reason: "work-objects-flexpane-refresh"})
		if p.tab == "comments" {
			co.FetchWorkObjectComments(e.EntityID, e.ExternalURL)
		}
	}
	// the footer: the full layout's primary actions (Details)
	footH := 0
	var acts []workobject.Button
	if f := e.Full(); f != nil && f.Actions != nil && p.tab == "details" {
		acts = f.Actions.Primary
	}
	if len(acts) > 0 {
		footH = gtx.Dp(36) + 2*gtx.Dp(12)
	}
	body := image.Pt(sz.X, sz.Y-hh-footH)
	atExact(gtx, image.Pt(0, hh), body, func(gtx layout.Context) layout.Dimensions {
		return p.body(gtx, e)
	})
	if footH > 0 {
		atExact(gtx, image.Pt(0, sz.Y-footH), image.Pt(sz.X, footH), func(gtx layout.Context) layout.Dimensions {
			return p.layoutFooter(gtx, e, acts)
		})
	}
	return layout.Dimensions{Size: sz}
}

// body lays out the entity header, the tabs and the tab's content.
func (p *woPane) body(gtx layout.Context, e *workobject.Entity) layout.Dimensions {
	parts := []layout.Widget{
		func(gtx layout.Context) layout.Dimensions { return p.entityHeader(gtx, e) },
		func(gtx layout.Context) layout.Dimensions { return p.layoutTabs(gtx, e) },
	}
	switch p.tab {
	case "comments":
		parts = append(parts, func(gtx layout.Context) layout.Dimensions { return p.layoutComments(gtx, e) })
	case "threads":
		parts = append(parts, func(gtx layout.Context) layout.Dimensions { return p.layoutRefs(gtx, e) })
	default:
		parts = append(parts, func(gtx layout.Context) layout.Dimensions { return p.layoutDetails(gtx, e) })
	}
	return p.list.Layout(gtx, len(parts), func(gtx layout.Context, i int) layout.Dimensions {
		gtx.Constraints.Min.X = gtx.Constraints.Max.X
		return parts[i](gtx)
	})
}

// woDoc is a one-block doc of the entity as a work object attachment.
func (p *woPane) woDoc(e *workobject.Entity) richtext.Doc {
	var r richtext.Resolver
	if c := p.a.Core(); c != nil {
		r = c.S
	}
	if r == nil {
		return richtext.Doc{}
	}
	wo := richtext.WorkObjectFromEntity(r, e, p.o.AppUnfurlURL)
	return richtext.Doc{Blocks: []richtext.Block{{Kind: richtext.AttachmentBlock, Att: &richtext.Attachment{WorkObject: wo}}}}
}

// richStyle is the message rows' style (palette, text model, the work
// object flags).
func (p *woPane) richStyle() *richview.Style { return messages.RichStyle(p.a) }

// entityHeader is header__i5UwZ: the title and the doc link.
func (p *woPane) entityHeader(gtx layout.Context, e *workobject.Entity) layout.Dimensions {
	a := p.a
	th := a.th
	c := &th.C
	pad := gtx.Dp(20)
	w := gtx.Constraints.Max.X
	title := ""
	if f := e.Full(); f != nil {
		title = f.Title
	} else if l := e.UnfurlLayout(); l != nil {
		title = l.Title
	}
	y := pad
	d := at(gtx, image.Pt(pad, y), image.Pt(w-2*pad, gtx.Dp(30)*3), func(gtx layout.Context) layout.Dimensions {
		return th.Layout(gtx, theme.TextStyle{Size: 22, Weight: theme.Black, Color: c.Fg, LineHeight: 30, MaxLines: 3}, title)
	})
	y += d.Size.Y
	// WorkObjectFlexpaneDocLink
	link, label := "", ""
	if f := e.Full(); f != nil {
		label = f.Subtitle
		link = f.SubtitleURL
	}
	label = firstNonEmptyStr(label, e.ProductName, e.AppName)
	if p.o.AppUnfurlURL != "" && e.AppID != "A012G7WPK1Q" && e.AppID != "A2RPP3NFR" {
		link = p.o.AppUnfurlURL
	}
	link = firstNonEmptyStr(link, e.ExternalURL)
	if link != "" && label != "" {
		y += gtx.Dp(4)
		lw := min(th.Measure(gtx, 13, theme.Bold, label), w-2*pad-gtx.Dp(20))
		lh := gtx.Dp(18)
		btn := p.footerClick(99)
		at(gtx, image.Pt(pad, y), image.Pt(lw+gtx.Dp(4)+gtx.Dp(13), lh), func(gtx layout.Context) layout.Dimensions {
			return clickArea(gtx, btn, nil, func(gtx layout.Context) layout.Dimensions {
				at(gtx, image.Point{}, image.Pt(lw+1, lh), func(gtx layout.Context) layout.Dimensions {
					return vcenter(gtx, lh, func(gtx layout.Context) layout.Dimensions {
						return th.Label(gtx, 13, theme.Bold, c.Link, label)
					})
				})
				at(gtx, image.Pt(lw+gtx.Dp(4), (lh-gtx.Dp(13))/2), image.Pt(gtx.Dp(13), gtx.Dp(13)), func(gtx layout.Context) layout.Dimensions {
					return icons.Get("new-window")(gtx, 13, c.Link)
				})
				return layout.Dimensions{Size: image.Pt(lw+gtx.Dp(17), lh)}
			})
		})
		if clicked(btn) {
			openURL(link)
		}
		y += lh
	}
	y += pad
	line := max(1, gtx.Dp(1.5))
	theme.Fill(gtx, image.Rect(0, y, w, y+line), ccTok(th.Dark, "otl-ter"))
	return layout.Dimensions{Size: image.Pt(w, y+line)}
}

// woTab is one of the flexpane's tabs.
type woTab struct{ id, title, count string }

// tabList is WorkObjectFlexpaneBody's tabs.
func (p *woPane) tabList(e *workobject.Entity) []woTab {
	co := p.a.Core()
	tabs := []woTab{{id: "details", title: woNS.T("Details")}}
	if co != nil && co.WorkObjectFlags().CommentSync && e.Comments != nil {
		n := e.Comments.Count
		if cs := co.WorkObjectCommentsOf(e.EntityID); cs != nil && cs.Comments != nil {
			n = len(cs.Comments)
		}
		t := woTab{id: "comments", title: woNS.T("Comments")}
		if n > 0 {
			t.count = itoa(min(n, 9))
			if n > 9 {
				t.count += "+"
			}
		}
		tabs = append(tabs, t)
	}
	t := woTab{id: "threads", title: woNS.T("Conversations")}
	if co != nil {
		if n := len(co.WorkObjectRefs(firstNonEmptyStr(e.RelatedConvoID, e.EntityID))); n > 0 {
			t.count = itoa(n)
		}
	}
	return append(tabs, t)
}

// layoutTabs is the c-tabs menu: 13px bold content-sec tabs (padding
// 8px 0, margin 0 8px, 20px apart), the active one content-pry over a
// 2px theme underline, a 1px otl-ter line under them all; the menu has
// 12px side padding.
func (p *woPane) layoutTabs(gtx layout.Context, e *workobject.Entity) layout.Dimensions {
	a := p.a
	th := a.th
	c := &th.C
	w := gtx.Constraints.Max.X
	h := gtx.Dp(8)*2 + gtx.Dp(16.25)
	x := gtx.Dp(12) + gtx.Dp(8)
	for i, t := range p.tabList(e) {
		if i >= len(p.tabs) {
			break
		}
		tw := th.Measure(gtx, 13, theme.Bold, t.title)
		cw := 0
		if t.count != "" {
			cw = gtx.Dp(4) + th.Measure(gtx, 13, theme.Regular, t.count)
		}
		btn := &p.tabs[i]
		t := t
		at(gtx, image.Pt(x, 0), image.Pt(tw+cw, h), func(gtx layout.Context) layout.Dimensions {
			return clickArea(gtx, btn, nil, func(gtx layout.Context) layout.Dimensions {
				col := ccTok(th.Dark, "content-sec")
				if p.tab == t.id || p.tab == "" && t.id == "details" {
					col = ccTok(th.Dark, "content-pry")
					theme.Fill(gtx, image.Rect(0, h-gtx.Dp(2), tw+cw, h), c.Accent)
				}
				at(gtx, image.Point{}, image.Pt(tw+1, h), func(gtx layout.Context) layout.Dimensions {
					return vcenter(gtx, h, func(gtx layout.Context) layout.Dimensions {
						return th.Label(gtx, 13, theme.Bold, col, t.title)
					})
				})
				if t.count != "" {
					at(gtx, image.Pt(tw+gtx.Dp(4), 0), image.Pt(cw, h), func(gtx layout.Context) layout.Dimensions {
						return vcenter(gtx, h, func(gtx layout.Context) layout.Dimensions {
							return th.Label(gtx, 13, theme.Regular, col, t.count)
						})
					})
				}
				return layout.Dimensions{Size: image.Pt(tw+cw, h)}
			})
		})
		if clicked(btn) && p.tab != t.id {
			p.setTab(t.id)
			a.Invalidate()
		}
		x += tw + cw + gtx.Dp(8) + gtx.Dp(20)
	}
	theme.Fill(gtx, image.Rect(0, h-gtx.Dp(1), w, h), ccTok(th.Dark, "otl-ter"))
	return layout.Dimensions{Size: image.Pt(w, h)}
}

// layoutDetails is the Details tab: WorkObjectDetails in richview.
func (p *woPane) layoutDetails(gtx layout.Context, e *workobject.Entity) layout.Dimensions {
	key := string(e.Raw())
	if p.details == nil || p.detKey != key {
		p.details = richview.New(p.woDoc(e), richview.Options{WorkObjectDetails: true, Channel: p.o.Channel, TS: p.o.TS})
		p.detKey = key
	}
	return p.details.Layout(gtx, p.richStyle(), woPaneHandler{p.a})
}

// layoutFooter is WorkObjectFlexpaneFooter's buttons: each primary action
// an outline button sharing the row (display_flex flex_one), 8px apart
// inside 12px/20px padding.
func (p *woPane) layoutFooter(gtx layout.Context, e *workobject.Entity, acts []workobject.Button) layout.Dimensions {
	a := p.a
	th := a.th
	c := &th.C
	sz := gtx.Constraints.Max
	theme.Fill(gtx, image.Rect(0, 0, sz.X, max(1, gtx.Dp(1))), c.Border)
	pad := gtx.Dp(20)
	gap := gtx.Dp(8)
	bh := gtx.Dp(36)
	n := len(acts)
	bw := (sz.X - 2*pad - gap*(n-1)) / max(1, n)
	for i := range acts {
		b := acts[i]
		btn := p.footerClick(i)
		x := pad + i*(bw+gap)
		label := b.Label()
		if label == "" && b.Type == "open-in-app" {
			label = woNS.T("Open in {productName}", "productName", firstNonEmptyStr(e.ProductName, e.AppName))
		}
		at(gtx, image.Pt(x, gtx.Dp(12)), image.Pt(bw, bh), func(gtx layout.Context) layout.Dimensions {
			return clickArea(gtx, btn, func(gtx layout.Context, s image.Point, hov bool) {
				bg := c.Bg
				if hov {
					bg = c.BgHover
				}
				theme.RRect(gtx, image.Rect(0, 0, s.X, s.Y), gtx.Dp(8), bg)
				theme.Border(gtx, image.Rect(0, 0, s.X, s.Y), gtx.Dp(8), max(1, gtx.Dp(1)), c.BorderStrong)
			}, func(gtx layout.Context) layout.Dimensions {
				lw := min(th.Measure(gtx, 15, theme.Bold, label), bw-gtx.Dp(24))
				at(gtx, image.Pt((bw-lw)/2, 0), image.Pt(lw+1, bh), func(gtx layout.Context) layout.Dimensions {
					return vcenter(gtx, bh, func(gtx layout.Context) layout.Dimensions {
						return th.Label(gtx, 15, theme.Bold, c.Fg, label)
					})
				})
				return layout.Dimensions{Size: image.Pt(bw, bh)}
			})
		})
		if clicked(btn) {
			p.footerAction(e, b)
		}
	}
	return layout.Dimensions{Size: sz}
}

// footerAction is a footer button: an open-in-app opens the item, a URL
// opens, a block action goes to the app (blocks.actions in an
// entity_detail container).
func (p *woPane) footerAction(e *workobject.Entity, b workobject.Button) {
	switch {
	case b.Type == "open-in-app":
		openURL(firstNonEmptyStr(p.o.AppUnfurlURL, e.ExternalURL))
	case b.URL != "":
		openURL(richtext.SafeURL(b.URL))
	}
	if b.ActionID == "" || b.BlockID == "" {
		return
	}
	co := p.a.Core()
	if co == nil || co.Interactive() == nil {
		return
	}
	f := e.Full()
	if f == nil || f.Actions == nil {
		return
	}
	act, _ := json.Marshal(b)
	bp := f.Actions.BlockPayload
	go func() {
		ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
		defer cancel()
		ix := co.Interactive()
		_, _ = ix.BlockActionSend(ctx, core.BlockAction{ServiceID: bp.ServiceID, AppID: e.AppID, ServiceTeamID: bp.ServiceTeamID,
			Action: act, Container: core.ActionContainer{Type: firstNonEmptyStr(bp.Container.Type, "message_attachment"),
				ChannelID: p.o.Channel, MessageTS: p.o.TS}})
	}()
}

// footerClick is the i-th footer (or doc link: 99) clickable.
func (p *woPane) footerClick(i int) *widget.Clickable {
	if i == 99 {
		i = 0
		if len(p.footer) == 0 {
			p.footer = make([]widget.Clickable, 1)
		}
		return &p.footer[0]
	}
	for len(p.footer) < i+2 {
		p.footer = append(p.footer, widget.Clickable{})
	}
	return &p.footer[i+1]
}

// ---- Comments ----

// layoutComments is WorkObjectComments.
func (p *woPane) layoutComments(gtx layout.Context, e *workobject.Entity) layout.Dimensions {
	a := p.a
	th := a.th
	c := &th.C
	w := gtx.Constraints.Max.X
	co := a.Core()
	if co == nil {
		return layout.Dimensions{}
	}
	cs := co.WorkObjectCommentsOf(e.EntityID)
	y := gtx.Dp(8) // container: padding-top 8px
	product := firstNonEmptyStr(e.ProductName, e.AppName, "app")
	dtype := firstNonEmptyStr(e.DisplayType, "item")
	switch {
	case cs == nil || cs.Loading && cs.Comments == nil:
		// WorkObjectCommentSkeleton while getComments runs
		for i := 0; i < 3; i++ {
			theme.RRect(gtx, image.Rect(gtx.Dp(16), y+gtx.Dp(8), gtx.Dp(52), y+gtx.Dp(44)), gtx.Dp(8), c.Skeleton)
			theme.RRect(gtx, image.Rect(gtx.Dp(60), y+gtx.Dp(10), gtx.Dp(180), y+gtx.Dp(18)), gtx.Dp(4), c.Skeleton)
			theme.RRect(gtx, image.Rect(gtx.Dp(60), y+gtx.Dp(26), w-gtx.Dp(40), y+gtx.Dp(34)), gtx.Dp(4), c.Skeleton)
			y += gtx.Dp(52)
		}
		return layout.Dimensions{Size: image.Pt(w, y)}
	case cs.Failed:
		// WorkObjectCommentsErrorState
		d := at(gtx, image.Pt(gtx.Dp(20), y+gtx.Dp(40)), image.Pt(w-gtx.Dp(40), gtx.Dp(80)), func(gtx layout.Context) layout.Dimensions {
			return th.Para(gtx, 15, 22, theme.Regular, c.FgMuted, woNS.T("Couldn’t load comments."))
		})
		return layout.Dimensions{Size: image.Pt(w, y+gtx.Dp(40)+d.Size.Y)}
	}
	if len(cs.Comments) == 0 {
		// WorkObjectCommentsEmptyState: "Synced with {productName}"
		y += gtx.Dp(80)
		d := at(gtx, image.Pt(gtx.Dp(20), y), image.Pt(w-gtx.Dp(40), gtx.Dp(30)), func(gtx layout.Context) layout.Dimensions {
			return th.Label(gtx, 18, theme.Black, c.Fg, woNS.T("Synced with {productName}", "productName", product))
		})
		y += d.Size.Y + gtx.Dp(8)
		d = at(gtx, image.Pt(gtx.Dp(20), y), image.Pt(w-gtx.Dp(40), gtx.Dp(80)), func(gtx layout.Context) layout.Dimensions {
			return th.Para(gtx, 15, 22, theme.Regular, c.FgMuted,
				woNS.T("Comments posted on this {itemType} will sync back to {productName}.", "itemType", dtype, "productName", product))
		})
		y += d.Size.Y + gtx.Dp(36)
	}
	if p.bodies == nil {
		p.bodies = map[string]*richview.View{}
	}
	for i := range cs.Comments {
		y += p.layoutComment(gtx, &cs.Comments[i], y, w)
	}
	// the composer, or the read-only banner
	y += gtx.Dp(8)
	if !cs.CanPostComment {
		d := at(gtx, image.Pt(gtx.Dp(16), y), image.Pt(w-gtx.Dp(32), gtx.Dp(80)), func(gtx layout.Context) layout.Dimensions {
			return th.Para(gtx, 13, 18, theme.Regular, c.FgMuted,
				woNSUnder.T("Your {productName} account doesn’t have permission to comment on this {displayType}.", "productName", product, "displayType", dtype))
		})
		return layout.Dimensions{Size: image.Pt(w, y+d.Size.Y+gtx.Dp(16))}
	}
	y += p.layoutComposer(gtx, e, y, w, product, dtype, cs.Posting)
	return layout.Dimensions{Size: image.Pt(w, y)}
}

// layoutComment is WorkObjectCommentItem (commentItem: 8px 16px; the
// message gutter's 36px avatar; the header's name in black and the
// relative time 8px apart on a baseline; the body 4px under).
func (p *woPane) layoutComment(gtx layout.Context, cm *core.WorkObjectComment, y, w int) int {
	a := p.a
	th := a.th
	c := &th.C
	co := a.Core()
	name, avatar := cm.Sender.Text, ""
	if cm.Sender.UserType == "slack" && co != nil {
		if n, ok := co.S.MemberName(cm.Sender.UserID); ok {
			name = n
		}
		avatar = co.S.MemberAvatar(cm.Sender.UserID)
	} else if cm.Sender.Image != nil {
		avatar = cm.Sender.Image.ImageURL
	}
	padX, padY := gtx.Dp(16), gtx.Dp(8)
	av := gtx.Dp(36)
	if avatar != "" && a.images != nil {
		if img, ok := a.images.Get(avatar, av); ok {
			atExact(gtx, image.Pt(padX, y+padY), image.Pt(av, av), func(gtx layout.Context) layout.Dimensions {
				richview.DrawImage(gtx.Ops, img, image.Rect(0, 0, av, av), gtx.Dp(8), true)
				return layout.Dimensions{Size: image.Pt(av, av)}
			})
		} else {
			theme.RRect(gtx, image.Rect(padX, y+padY, padX+av, y+padY+av), gtx.Dp(8), c.Skeleton)
		}
	} else {
		theme.RRect(gtx, image.Rect(padX, y+padY, padX+av, y+padY+av), gtx.Dp(8), c.Skeleton)
	}
	tx := padX + av + gtx.Dp(8)
	tw := w - tx - padX
	nw := min(th.Measure(gtx, 15, theme.Black, name), tw)
	lh := gtx.Dp(22)
	at(gtx, image.Pt(tx, y+padY), image.Pt(nw+1, lh), func(gtx layout.Context) layout.Dimensions {
		return vcenter(gtx, lh, func(gtx layout.Context) layout.Dimensions { return th.Label(gtx, 15, theme.Black, c.Fg, name) })
	})
	if t, ok := workobject.ToTime(workobject.Number(cm.Timestamp)); ok {
		ago := i18n.TimeAgo(i18n.Locale(), t, time.Now(), false, true)
		at(gtx, image.Pt(tx+nw+gtx.Dp(8), y+padY+gtx.Dp(2)), image.Pt(max(tw-nw-gtx.Dp(8), 1), lh), func(gtx layout.Context) layout.Dimensions {
			return vcenter(gtx, lh, func(gtx layout.Context) layout.Dimensions { return th.Label(gtx, 12, theme.Regular, c.FgMuted, ago) })
		})
	}
	v := p.bodies[cm.ID]
	if v == nil {
		v = richview.New(p.commentDoc(cm), richview.Options{})
		p.bodies[cm.ID] = v
	}
	by := y + padY + lh + gtx.Dp(4)
	d := at(gtx, image.Pt(tx, by), image.Pt(tw, gtx.Dp(2000)), func(gtx layout.Context) layout.Dimensions {
		return v.Layout(gtx, p.richStyle(), woPaneHandler{a})
	})
	h := max(by+d.Size.Y, y+padY+av) - y + padY
	return h
}

// commentDoc is a comment's body: its rich_text block, or its text value.
func (p *woPane) commentDoc(cm *core.WorkObjectComment) richtext.Doc {
	co := p.a.Core()
	if co == nil {
		return richtext.Doc{}
	}
	if len(cm.Comment.Blocks) > 0 && string(cm.Comment.Blocks) != "null" {
		raw := cm.Comment.Blocks
		if raw[0] != '[' {
			raw = json.RawMessage("[" + string(raw) + "]")
		}
		return richtext.FromMessage(co.S, &slack.Message{Blocks: raw})
	}
	return richtext.FromMessage(co.S, &slack.Message{Text: cm.Comment.Value})
}

// layoutComposer is WorkObjectCommentComposer: an input under the
// comments ("Comment on {productName} {displayType}…"); Enter sends the
// text as a rich_text block (workObjects.postComment).
func (p *woPane) layoutComposer(gtx layout.Context, e *workobject.Entity, y, w int, product, dtype string, posting bool) int {
	a := p.a
	th := a.th
	c := &th.C
	pad := gtx.Dp(16)
	h := gtx.Dp(44)
	r := image.Rect(pad, y, w-pad, y+h)
	theme.RRect(gtx, r, gtx.Dp(8), c.Bg)
	theme.Border(gtx, r, gtx.Dp(8), max(1, gtx.Dp(1)), c.BorderStrong)
	ph := woNS.T("Comment on {productName} {displayType}…", "productName", product, "displayType", dtype)
	in := image.Pt(r.Min.X+gtx.Dp(12), r.Min.Y)
	iw := r.Dx() - gtx.Dp(24)
	if p.editor.Len() == 0 {
		at(gtx, in, image.Pt(iw, h), func(gtx layout.Context) layout.Dimensions {
			return vcenter(gtx, h, func(gtx layout.Context) layout.Dimensions { return th.Label(gtx, 15, theme.Regular, c.FgSubtle, ph) })
		})
	}
	at(gtx, image.Pt(in.X, in.Y+(h-gtx.Dp(22))/2), image.Pt(iw, gtx.Dp(22)), func(gtx layout.Context) layout.Dimensions {
		gtx.Constraints.Min.X = iw
		return th.Editor(gtx, &p.editor, 15, c.Fg, c.Accent)
	})
	for {
		ev, ok := p.editor.Update(gtx)
		if !ok {
			break
		}
		if _, ok := ev.(widget.SubmitEvent); ok && !posting {
			p.post(e)
		}
	}
	return h + gtx.Dp(16)
}

// post sends the composer's text as a comment.
func (p *woPane) post(e *workobject.Entity) {
	text := strings.TrimSpace(p.editor.Text())
	co := p.a.Core()
	if text == "" || co == nil {
		return
	}
	block, _ := json.Marshal(map[string]any{"type": "rich_text", "elements": []any{
		map[string]any{"type": "rich_text_section", "elements": []any{map[string]any{"type": "text", "text": text}}}}})
	co.PostWorkObjectComment(e.EntityID, e.ExternalURL, block)
	p.editor.SetText("")
}

// ---- Conversations ----

// layoutRefs is WorkObjectThreads: the messages that shared the item, or
// its empty state.
func (p *woPane) layoutRefs(gtx layout.Context, e *workobject.Entity) layout.Dimensions {
	a := p.a
	th := a.th
	c := &th.C
	w := gtx.Constraints.Max.X
	co := a.Core()
	if co == nil {
		return layout.Dimensions{}
	}
	refs := co.WorkObjectRefs(firstNonEmptyStr(e.RelatedConvoID, e.EntityID))
	y := gtx.Dp(16)
	if len(refs) == 0 {
		y += gtx.Dp(40)
		d := at(gtx, image.Pt(gtx.Dp(20), y), image.Pt(w-gtx.Dp(40), gtx.Dp(30)), func(gtx layout.Context) layout.Dimensions {
			return th.Label(gtx, 18, theme.Black, c.Fg, woNS.T("It’s quiet. Too quiet…"))
		})
		y += d.Size.Y + gtx.Dp(8)
		d = at(gtx, image.Pt(gtx.Dp(20), y), image.Pt(w-gtx.Dp(40), gtx.Dp(80)), func(gtx layout.Context) layout.Dimensions {
			return th.Para(gtx, 15, 22, theme.Regular, c.FgMuted,
				woNS.T("When this {type} is shared in other Slack messages, they will show up here.", "type", strings.ToLower(firstNonEmptyStr(e.DisplayType, "item"))))
		})
		return layout.Dimensions{Size: image.Pt(w, y+d.Size.Y+gtx.Dp(20))}
	}
	for len(p.refs) < len(refs) {
		p.refs = append(p.refs, widget.Clickable{})
	}
	for i, r := range refs {
		btn := &p.refs[i]
		name := "#" + strings.TrimPrefix(co.S.ChannelName(r.ChannelID), "#")
		text := ""
		if m, ok := co.S.Find(r.ChannelID, r.TS); ok {
			text = m.Text
		}
		rh := gtx.Dp(64)
		at(gtx, image.Pt(0, y), image.Pt(w, rh), func(gtx layout.Context) layout.Dimensions {
			return clickArea(gtx, btn, func(gtx layout.Context, s image.Point, hov bool) {
				if hov {
					theme.Fill(gtx, image.Rect(0, 0, s.X, s.Y), c.BgHover)
				}
			}, func(gtx layout.Context) layout.Dimensions {
				at(gtx, image.Pt(gtx.Dp(20), gtx.Dp(10)), image.Pt(w-gtx.Dp(40), gtx.Dp(18)), func(gtx layout.Context) layout.Dimensions {
					return th.Label(gtx, 13, theme.Bold, c.FgMuted, name)
				})
				at(gtx, image.Pt(gtx.Dp(20), gtx.Dp(30)), image.Pt(w-gtx.Dp(40), gtx.Dp(22)), func(gtx layout.Context) layout.Dimensions {
					return th.Label(gtx, 15, theme.Regular, c.Fg, text)
				})
				return layout.Dimensions{Size: image.Pt(w, rh)}
			})
		})
		if clicked(btn) {
			a.OpenThread(r.ChannelID, r.TS)
		}
		y += rh
	}
	return layout.Dimensions{Size: image.Pt(w, y+gtx.Dp(16))}
}

// woPaneHandler routes the flexpane's rich text clicks.
type woPaneHandler struct{ a *App }

func (h woPaneHandler) OpenURL(u string)                       { openURL(u) }
func (h woPaneHandler) OpenUser(id string, anchor image.Point) { h.a.OpenProfile(anchor, id) }
func (h woPaneHandler) OpenChannel(id string)                  { h.a.Navigate("c/" + id) }
func (h woPaneHandler) OpenUsergroup(string, image.Point)      {}
func (h woPaneHandler) OpenImage(img richtext.Image)           { openURL(img.URL) }
func (h woPaneHandler) OpenFile(f *richtext.File)              { openURL(firstNonEmptyStr(f.Permalink, f.URL)) }
func (h woPaneHandler) Button(b richtext.Button) {
	if b.URL != "" {
		openURL(b.URL)
	}
}
