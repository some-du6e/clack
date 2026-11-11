package canvasview

// QuipAccessIndicator (client-boot-shared CQK3): a canvas the viewer
// can't edit says so in its header, Slack's ViewOnlyPill (EgWz): an
// outline small button (28px, 13px bold, a 1px foreground-low .13 border,
// radius 4) with the 16px eye-open icon and "View only", or "Comment
// only" when canvas_comment_only_mode is on and the viewer has comment
// access (isCommentable: access comment, write or owner). In a channel
// tab it is borderless, inside the floating actions' tile before More
// options, and gone when the viewer can edit; in the side panel and the
// full view it sits in CanvasActionItems for a canvas the viewer can't
// edit (renderViewOnlyButton: access set and not isWriteable).
//
// A click opens its popover (position bottom-left, offsetX -12, offsetY
// 6; .popover__dsoBs: 300px, padding 16, a 1px otl-ter border, radius 4,
// 12px between a bold title, the content-sec description and the
// buttons). useQuipAccess asks the owner with files.requestPermission
// {file_id, reason, request_type}:
//
//   - not asked yet: "You only have permission to view." (to comment,
//     under the experiment with comment access), "To make changes, ask
//     the owner for edit access.", Request Edit Access ("upgrade"), and
//     Request Comment Access ("comment_upgrade") under the experiment
//     for a viewer who can't comment;
//   - asked before (most_recent_access_request_ts): "You’ve already asked
//     for access", "Still waiting for edit access? You can send the owner
//     a reminder.", Remind The Owner;
//   - sent: "Request sent", "You’ll be notified once it’s been shared
//     with you.";
//   - failed: "Error", "Something’s gone wrong. Please try again later.",
//     Try Again.

import (
	"context"
	"image"
	"image/color"
	"time"

	"gioui.org/io/event"
	"gioui.org/io/key"
	"gioui.org/io/pointer"
	"gioui.org/layout"
	"gioui.org/op"
	"gioui.org/op/clip"
	"gioui.org/widget"

	"github.com/some-du6e/clack/internal/core"
	"github.com/some-du6e/clack/internal/ui/icons"
	"github.com/some-du6e/clack/internal/ui/theme"
)

// access request states (useQuipAccess)
const (
	askNone = iota
	askAlready
	askSent
	askFailed
)

// accessState is the indicator's state (UI goroutine only).
type accessState struct {
	open     bool
	status   int
	statusOf string // the canvas id the status was read for
	loading  bool
	pill     widget.Clickable
	edit     widget.Clickable // Request Edit Access / Remind The Owner / Try Again
	comment  widget.Clickable // Request Comment Access
	anchor   image.Rectangle  // the pill, in the view
	scrim    int              // tag: a press outside the popover closes it
	keys     int              // tag: Escape closes it
	focused  bool             // the popover has asked for the key focus
}

// commentable is isCommentable.
func commentable(access string) bool {
	return access == "comment" || access == "write" || access == "owner"
}

// accessLabel is the pill's text.
func (v *View) accessLabel(info core.CanvasInfo) string {
	if v.exp("canvas_comment_only_mode") && commentable(info.Access) {
		return "Comment only"
	}
	return "View only"
}

// showsAccess: the header draws the indicator for this canvas.
func (v *View) showsAccess(d *core.CanvasDoc) bool {
	if d == nil || d.Info.Editable || d.Info.TemplateMod {
		return false
	}
	return v.mode == Tab || d.Info.Access != ""
}

// accessPillWidth is the pill's width for its label.
func (v *View) accessPillWidth(gtx layout.Context, label string) int {
	th := v.h.Theme()
	return max(gtx.Dp(56), gtx.Dp(12)+gtx.Dp(16)+gtx.Dp(4)+th.Measure(gtx, 13, theme.Bold, label)+gtx.Dp(12))
}

// layoutAccessPill draws the pill at r (in gtx), bordered or not.
func (v *View) layoutAccessPill(gtx layout.Context, d *core.CanvasDoc, r image.Rectangle, border bool, origin image.Point) {
	th := v.h.Theme()
	c := &th.C
	st := &v.access
	if st.statusOf != v.id {
		st.statusOf, st.status, st.open = v.id, askNone, false
		if d.Info.AccessRequested {
			st.status = askAlready
		}
	}
	label := v.accessLabel(d.Info)
	hit := st.pill.Clicked(gtx) // before Layout, which drains a Clickable's events
	st.anchor = r.Add(origin)
	at(gtx, r.Min, r.Size(), func(gtx layout.Context) layout.Dimensions {
		s := gtx.Constraints.Max
		bg := c.Bg
		if st.pill.Hovered() {
			bg = c.BgHover
		}
		theme.RRect(gtx, image.Rectangle{Max: s}, gtx.Dp(4), bg)
		if border {
			theme.Border(gtx, image.Rectangle{Max: s}, gtx.Dp(4), gtx.Dp(1), theme.WithAlpha(c.Fg, 0x21))
		}
		x := gtx.Dp(12)
		ip := gtx.Dp(16)
		at(gtx, image.Pt(x, (s.Y-ip)/2), image.Pt(ip, ip), func(gtx layout.Context) layout.Dimensions {
			return icons.Get("eye-open")(gtx, 16, c.Fg)
		})
		x += ip + gtx.Dp(4)
		at(gtx, image.Pt(x, 0), image.Pt(max(0, s.X-x), s.Y), func(gtx layout.Context) layout.Dimensions {
			return vcenter(gtx, s.Y, func(gtx layout.Context) layout.Dimensions {
				return th.Label(gtx, 13, theme.Bold, c.Fg, label)
			})
		})
		return st.pill.Layout(gtx, func(gtx layout.Context) layout.Dimensions { return layout.Dimensions{Size: s} })
	})
	if hit {
		st.open, st.focused = !st.open, false
		v.h.Invalidate()
	}
}

// accessCopy is the popover's title, description and buttons for the
// current state.
func (v *View) accessCopy(info core.CanvasInfo) (title, desc string, buttons []string) {
	exp := v.exp("canvas_comment_only_mode")
	switch v.access.status {
	case askAlready:
		return "You’ve already asked for access", "Still waiting for edit access? You can send the owner a reminder.", []string{"Remind The Owner"}
	case askSent:
		return "Request sent", "You’ll be notified once it’s been shared with you.", nil
	case askFailed:
		return "Error", "Something’s gone wrong. Please try again later.", []string{"Try Again"}
	}
	title = "You only have permission to view."
	if exp && commentable(info.Access) {
		title = "You only have permission to comment."
	}
	buttons = []string{"Request Edit Access"}
	if exp && !commentable(info.Access) {
		buttons = append(buttons, "Request Comment Access")
	}
	return title, "To make changes, ask the owner for edit access.", buttons
}

// requestAccess runs useQuipAccess's request.
func (v *View) requestAccess(reason, requestType string) {
	st := &v.access
	a := v.h.Core()
	if a == nil || st.loading {
		return
	}
	st.loading = true
	id := v.id
	go func() {
		ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
		defer cancel()
		err := a.RequestFileAccess(ctx, id, reason, requestType)
		v.post(func() {
			st.loading = false
			if v.id != id {
				return
			}
			if err != nil {
				st.status = askFailed
			} else {
				st.status = askSent
			}
		})
	}()
}

// layoutAccessPopover draws the open popover over the view (gtx is the
// whole view), closing on Escape or a press outside it.
func (v *View) layoutAccessPopover(gtx layout.Context, d *core.CanvasDoc) {
	st := &v.access
	if !st.open || d == nil || !v.showsAccess(d) {
		st.open = false
		return
	}
	th := v.h.Theme()
	c := &th.C
	title, desc, buttons := v.accessCopy(d.Info)

	for {
		ev, ok := gtx.Event(pointer.Filter{Target: &st.scrim, Kinds: pointer.Press})
		if !ok {
			break
		}
		if pe, ok := ev.(pointer.Event); ok && pe.Kind == pointer.Press {
			st.open = false
		}
	}
	for {
		ev, ok := gtx.Event(key.Filter{Focus: &st.keys, Name: key.NameEscape})
		if !ok {
			break
		}
		if ke, ok := ev.(key.Event); ok && ke.State == key.Press {
			st.open = false
		}
	}
	if !st.open {
		v.h.Invalidate()
		return
	}
	sz := gtx.Constraints.Max
	w := gtx.Dp(300)
	pad := gtx.Dp(16)
	inner := w - 2*pad - 2*gtx.Dp(1)
	// measure: the title, the description, then 36px buttons, 12px apart
	m := op.Record(gtx.Ops)
	g := gtx
	g.Constraints = layout.Constraints{Max: image.Pt(inner, sz.Y)}
	th1 := th.Text(g, theme.LabelOpts{Font: theme.Font(theme.Bold), Size: 15, Color: c.Fg}, title).Size.Y
	dh := th.Text(g, theme.LabelOpts{Font: theme.Font(theme.Regular), Size: 15, Color: c.FgMuted}, desc).Size.Y
	m.Stop()
	h := 2*gtx.Dp(1) + 2*pad + th1 + gtx.Dp(12) + dh
	for range buttons {
		h += gtx.Dp(12) + gtx.Dp(36)
	}
	// bottom-left of the pill, 12px further left, 6px below it
	x := st.anchor.Max.X - w - gtx.Dp(12)
	x = min(max(gtx.Dp(8), x), sz.X-w-gtx.Dp(8))
	y := st.anchor.Max.Y + gtx.Dp(6)
	r := image.Rect(x, y, x+w, y+h)

	rec := op.Record(gtx.Ops)
	// a press anywhere else closes it
	sc := clip.Rect{Max: sz}.Push(gtx.Ops)
	event.Op(gtx.Ops, &st.scrim)
	sc.Pop()
	if !st.focused { // once: a FocusCmd defers the rest of the frame's events
		st.focused = true
		gtx.Execute(key.FocusCmd{Tag: &st.keys})
	}
	// 0 2px 4px -2px #00000019, 0 4px 6px -1px #00000019
	theme.Shadow(gtx, r.Add(image.Pt(0, gtx.Dp(4))).Inset(gtx.Dp(1)), gtx.Dp(4), gtx.Dp(6), color.NRGBA{A: 0x19})
	theme.RRect(gtx, r, gtx.Dp(4), c.BgRaised)
	theme.Border(gtx, r, gtx.Dp(4), gtx.Dp(1), c.Border)
	pa := clip.Rect(r).Push(gtx.Ops) // the popover itself doesn't close it
	event.Op(gtx.Ops, &st.keys)
	pa.Pop()
	cy := y + gtx.Dp(1) + pad
	cx := x + gtx.Dp(1) + pad
	at(gtx, image.Pt(cx, cy), image.Pt(inner, th1), func(gtx layout.Context) layout.Dimensions {
		return th.Text(gtx, theme.LabelOpts{Font: theme.Font(theme.Bold), Size: 15, Color: c.Fg}, title)
	})
	cy += th1 + gtx.Dp(12)
	at(gtx, image.Pt(cx, cy), image.Pt(inner, dh), func(gtx layout.Context) layout.Dimensions {
		return th.Text(gtx, theme.LabelOpts{Font: theme.Font(theme.Regular), Size: 15, Color: c.FgMuted}, desc)
	})
	cy += dh
	for i, label := range buttons {
		cy += gtx.Dp(12)
		clk := &st.edit
		if i == 1 {
			clk = &st.comment
		}
		hit := clk.Clicked(gtx) // before Layout, which drains a Clickable's events
		at(gtx, image.Pt(cx, cy), image.Pt(inner, gtx.Dp(36)), func(gtx layout.Context) layout.Dimensions {
			s := gtx.Constraints.Max
			bg := c.Bg
			if clk.Hovered() && !st.loading {
				bg = c.BgHover
			}
			// c-button--outline, medium: 36px, radius 8, 15px bold, a 1px otl-sec border
			theme.RRect(gtx, image.Rectangle{Max: s}, gtx.Dp(8), bg)
			theme.Border(gtx, image.Rectangle{Max: s}, gtx.Dp(8), gtx.Dp(1), c.BorderStrong)
			col := c.Fg
			if st.loading {
				col = theme.WithAlpha(col, 0x80)
			}
			lw := th.Measure(gtx, 15, theme.Bold, label)
			at(gtx, image.Pt((s.X-lw)/2, 0), image.Pt(lw+2, s.Y), func(gtx layout.Context) layout.Dimensions {
				return vcenter(gtx, s.Y, func(gtx layout.Context) layout.Dimensions {
					return th.Label(gtx, 15, theme.Bold, col, label)
				})
			})
			return clk.Layout(gtx, func(gtx layout.Context) layout.Dimensions { return layout.Dimensions{Size: s} })
		})
		if hit && !st.loading {
			switch {
			case i == 1:
				v.requestAccess("IndicatorRequestCommentAccessButton", "comment_upgrade")
			default: // Request Edit Access, Remind The Owner, Try Again
				v.requestAccess("IndicatorRequestEditAccessButton", "upgrade")
			}
		}
		cy += gtx.Dp(36)
	}
	op.Defer(gtx.Ops, rec.Stop())
}
