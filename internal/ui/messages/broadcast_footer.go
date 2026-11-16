package messages

import (
	"image"

	"gioui.org/io/pointer"
	"gioui.org/layout"
	"gioui.org/op/clip"

	"github.com/some-du6e/clack/internal/ui/slackc"
	"github.com/some-du6e/clack/internal/ui/slackc/css"
	"github.com/some-du6e/clack/internal/ui/theme"
)

// ThreadReplyOpener is optional: the ui root opens a thread at one of its
// replies (Slack's navigate(toThread({channelId, threadTs, replyTs}))).
type ThreadReplyOpener interface {
	OpenThreadReply(ch, thread, ts string)
}

// broadcastFooter is Slack's ThreadBroadcastFooter (boot-shared NnKv,
// build c07ab895506f) under a thread broadcast whose thread has newer
// replies (core.Message.NewerReplies, KYIV k): a ButtonLink
// (.c-link--button: --dt_color-content-hgl-1, underlined on hover)
// reading "View newer replies" with classes c-message__broadcast_footer
// (12px, line-height 1.50001, bold, display block) and
// c-message_kit__broadcast_footer (margin -4px 0 4px; the later rule, so
// its margin wins). Both message bodies put it last: after the reactions
// and the reply bar, outside the dense body's indent, and not under a
// hidden sender (isSenderHidden). The -4px takes back the body's or the
// reaction bar's 4px bottom margin, so the row grows by the line alone.
// A click opens the thread at the broadcast (replyTs).
func (r *row) broadcastFooter(gtx layout.Context, y, cw int) int {
	rc := r.rc
	m := &r.m
	if r.newerBtn.Clicked(gtx) {
		if o, ok := rc.h.(ThreadReplyOpener); ok {
			o.OpenThreadReply(m.Channel, m.ThreadTS, m.TS)
		} else {
			rc.h.OpenThread(m.Channel, m.ThreadTS)
		}
	}
	x := 0
	if rc.dense {
		x = -dp(gtx, mt.indent)
	}
	top := y - dp(gtx, 4)
	r.newerRect = image.Rectangle{}
	d := at(gtx, x, top, func(gtx layout.Context) layout.Dimensions {
		gtx.Constraints = layout.Constraints{Max: image.Pt(cw-x, 1<<20)}
		return slackc.Component(gtx, "BroadcastFooter", r, (*row).broadcastFooterProps, (*row).layoutBroadcastFooter)
	})
	if d.Size.Y <= 0 {
		return 0
	}
	if r.newerRect.Empty() {
		r.newerRect = image.Rectangle{Max: d.Size}
	}
	r.newerRect = r.newerRect.Add(image.Pt(x, top)).Add(r.col)
	return d.Size.Y // -4px + the line + 4px
}

func (r *row) broadcastFooterProps() slackc.Props {
	return r.pane(slackc.Props{"$key": r.ts, "msg": r.message()})
}

// layoutBroadcastFooter is BroadcastFooter's ThreadBroadcastFooter: the
// ButtonLink.
func (r *row) layoutBroadcastFooter(gtx layout.Context, _ slackc.Props) layout.Dimensions {
	rc := r.rc
	fs := broadcastFooterClass.Enter(gtx, btnState(gtx, &r.newerBtn), 0)
	defer fs.Leave()
	if fs.Hidden() {
		return layout.Dimensions{}
	}
	d := r.newerBtn.Layout(gtx, func(gtx layout.Context) layout.Dimensions {
		st := textStyle{size: 12, weight: theme.Bold, col: rc.c.Link, lineH: 18}
		d := styledLabel(gtx, rc.th, fs, st, msgNS.T("View newer replies"))
		if r.newerBtn.Hovered() && d.Size.X > 0 && fs.Style() == nil {
			slackc.DrawDecoration(gtx.Ops, css.DecoUnderline, st.col, 0, float32(d.Size.Y-d.Baseline), float32(d.Size.X), 12*gtx.Metric.PxPerSp)
		}
		defer clip.Rect{Max: d.Size}.Push(gtx.Ops).Pop()
		pointer.CursorPointer.Add(gtx.Ops)
		return d
	})
	r.newerRect = image.Rectangle{Max: d.Size}
	return d
}

// NewerRepliesRect is a broadcast's "View newer replies" on screen.
type NewerRepliesRect struct {
	TS string // the broadcast
	R  image.Rectangle
}

// NewerRepliesRects lists the visible broadcasts' "View newer replies"
// links as of the last frame, in window coordinates.
func (v *ChannelView) NewerRepliesRects() []NewerRepliesRect {
	s := v.s
	var out []NewerRepliesRect
	for i := s.visible[0]; i < s.visible[1] && i < len(s.items); i++ {
		if s.items[i].kind != itMsg || i >= len(s.heights) || s.heights[i] <= 0 {
			continue
		}
		r := s.rows[s.items[i].key]
		if r == nil || r.newerRect.Empty() {
			continue
		}
		out = append(out, NewerRepliesRect{TS: r.src.TS, R: r.newerRect.Add(s.origin.Add(image.Pt(0, s.pos[i])))})
	}
	return out
}
