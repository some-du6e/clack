package messages

// Canvas comment threads in the thread pane (docs/CANVASES.md
// "Comments"; Slack's modern.vendor build 93ac48dd3c752f09, boot-shared
// f9df0f12, read from the public CDN without a cookie). A comment thread
// is a thread in the canvas's file channel whose root is a
// document_comment_root message (core/canvas_comments.go).
//
//   - The flexpane header's ArchiveCommentButton (WanD): when
//     canArchiveComment (+IQ1+: the canvas is writeable and not locked,
//     the root has a Quip thread id and a reply), a 28px header button
//     before the ⋮: check "Resolve thread", or rotate "Re-open thread" on
//     a resolved one, its tooltip bottom-right after 300ms. A click is
//     updateArchiveState (quip.thread.archive / unarchive).
//   - The root is ThreadRootCanvas → QuipCommentRoot (thrK): no avatar
//     gutter; .p-quip_comment_root (padding 12px 20px 8px 12px, a flex
//     row) with the 4px c-message_attachment__border (base-ter, radius
//     8), then .p-quip_comment_root__body (padding 0 12px, margin-bottom
//     -4px): in a thread, when the root names its most_recent_editor, the
//     heading (the editor's xx_small avatar, 8px, their name), then the
//     highlighted text; "This content was deleted." when Quip no longer
//     shows the highlight (isDocumentCommentVisible false).

import (
	"context"
	"image"
	"sync"
	"time"

	"gioui.org/layout"
	"gioui.org/op"
	"gioui.org/unit"
	"gioui.org/widget"

	"github.com/some-du6e/clack/internal/core"
	"github.com/some-du6e/clack/internal/i18n"
	"github.com/some-du6e/clack/internal/ui/icons"
	"github.com/some-du6e/clack/internal/ui/popups"
	"github.com/some-du6e/clack/internal/ui/slackc"
	"github.com/some-du6e/clack/internal/ui/slackc/css"
	"github.com/some-du6e/clack/internal/ui/theme"
)

// nsDocs is ArchiveCommentButton's I18n("slack_docs"); nsQuipRoot is
// QuipCommentRoot's I18n("quip-comment-root").
var (
	nsDocs     = i18n.NS("slack_docs")
	nsQuipRoot = i18n.NS("quip-comment-root")
)

// commentThread is the comment thread's canvas (files.info, read once)
// and the header button.
type commentThread struct {
	mu        sync.Mutex
	info      core.CanvasInfo
	asked     bool
	got       bool
	btn       widget.Clickable
	tip       popups.TipState
	busy      bool
	resolving *bool // the state clicked, until the root has it
	at        time.Time
	rect      image.Rectangle // the button, window coordinates (last frame)
}

// canvasInfo is the thread's canvas, fetched on first use.
func (v *ThreadView) canvasInfo() (core.CanvasInfo, bool) {
	t := &v.cmt
	t.mu.Lock()
	defer t.mu.Unlock()
	if t.got {
		return t.info, true
	}
	app := v.h.Core()
	if t.asked || app == nil {
		return core.CanvasInfo{}, false
	}
	t.asked = true
	file, h := core.FileOfChannel(v.ch), v.h
	go func() {
		ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
		defer cancel()
		info, err := app.CanvasInfo(ctx, file)
		t.mu.Lock()
		if err == nil {
			t.info, t.got = info, true
		}
		t.mu.Unlock()
		h.Invalidate()
	}()
	return core.CanvasInfo{}, false
}

// archiveShown is canArchiveComment for the thread's root.
func (v *ThreadView) archiveShown(root core.Message, ok bool) bool {
	if !ok || !root.IsCommentRoot() {
		return false
	}
	info, got := v.canvasInfo()
	return got && core.CanArchiveComment(info, root)
}

// resolved is the root's is_archived, or the state just clicked until the
// store has it.
func (v *ThreadView) resolved(root core.Message) bool {
	t := &v.cmt
	if t.resolving != nil {
		if *t.resolving == root.CommentResolved() || time.Since(t.at) > 10*time.Second {
			t.resolving = nil
		} else {
			return *t.resolving
		}
	}
	return root.CommentResolved()
}

// toggleResolved is updateArchiveState.
func (v *ThreadView) toggleResolved(root core.Message) {
	app := v.h.Core()
	if app == nil || v.cmt.busy {
		return
	}
	to := !v.resolved(root)
	v.cmt.resolving, v.cmt.at, v.cmt.busy = &to, time.Now(), true
	ch, ts, h := v.ch, root.TS, v.h
	go func() {
		ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
		defer cancel()
		err := app.SetCommentResolved(ctx, ch, ts, to)
		v.cmt.mu.Lock()
		v.cmt.busy = false
		v.cmt.mu.Unlock()
		if err != nil {
			msg := "Couldn't do that: " + err.Error()
			v.failed.Store(&msg)
		}
		h.Invalidate()
	}()
}

// archiveButton draws ArchiveCommentButton at pos.
func (v *ThreadView) archiveButton(gtx layout.Context, th *theme.Theme, c *colors, st slackc.Styled, pos image.Point, bs int, root core.Message) {
	icon, label := "check", nsDocs.T("Resolve thread")
	if v.resolved(root) {
		icon, label = "rotate", nsDocs.T("Re-open thread")
	}
	v.cmt.tip.Delay = 300 * time.Millisecond
	v.cmt.rect = image.Rect(pos.X, pos.Y, pos.X+bs, pos.Y+bs).Add(v.Origin)
	at(gtx, pos.X, pos.Y, func(gtx layout.Context) layout.Dimensions {
		gtx.Constraints = layout.Exact(image.Pt(bs, bs))
		d := v.cmt.btn.Layout(gtx, func(gtx layout.Context) layout.Dimensions {
			if v.cmt.btn.Hovered() {
				rrect(gtx.Ops, image.Rectangle{Max: image.Pt(bs, bs)}, dp(gtx, 8), st.Background(c.CloseHover))
			}
			is := dp(gtx, 20)
			at(gtx, (bs-is)/2, (bs-is)/2, func(gtx layout.Context) layout.Dimensions {
				return icons.Get(icon)(gtx, unit.Dp(20), st.Color(c.Text3))
			})
			return layout.Dimensions{Size: image.Pt(bs, bs)}
		})
		popups.Tooltip(gtx, th, &v.cmt.tip, v.cmt.btn.Hovered(), d.Size, popups.BelowRight, label, "")
		return d
	})
}

// ResolveShown reports whether the header shows Resolve thread /
// Re-open thread (tests).
func (v *ThreadView) ResolveShown() bool {
	root, ok := v.root()
	return v.archiveShown(root, ok)
}

// ResolveRect is that button as last drawn (window coordinates), and
// whether it says Re-open thread.
func (v *ThreadView) ResolveRect() (image.Rectangle, bool) {
	root, _ := v.root()
	return v.cmt.rect, v.resolved(root)
}

// commentRootLayout is ThreadRootCanvas: QuipCommentRoot in the thread.
func (r *row) commentRootLayout(gtx layout.Context, rc *rowCtx) layout.Dimensions {
	c := rc.c
	m := &r.m
	w := gtx.Constraints.Max.X
	vars := css.SlackVars(rc.th.Dark)
	barCol, _ := css.ParseColor(vars["--dt_color-base-ter"])
	padL, padT, padR, padB := dp(gtx, 12), dp(gtx, 12), dp(gtx, 20), dp(gtx, 8)
	bar := dp(gtx, 4)
	bx := padL + bar + dp(gtx, 12) // the body's left edge (its 12px padding)
	cw := max(w-bx-dp(gtx, 12)-padR, dp(gtx, 80))
	rec := op.Record(gtx.Ops)
	y := padT
	dc := m.DocumentComment
	visible := dc == nil || dc.IsVisible
	if dc != nil && dc.MostRecentEditor != "" && visible {
		// the heading: xx_small (16px) avatar, 8px, MessageSender
		lh := dp(gtx, 22)
		name, url := dc.MostRecentEditor, ""
		if app := rc.h.Core(); app != nil {
			if u, ok := app.User(dc.MostRecentEditor); ok {
				name, url = cmtFirst(u.Name, u.RealName, u.Handle), u.Avatar
			}
		}
		av := dp(gtx, 16)
		drawAvatarAt(gtx, rc, url, image.Rect(bx, y+(lh-av)/2, bx+av, y+(lh-av)/2+av))
		at(gtx, bx+av+dp(gtx, 8), y, func(gtx layout.Context) layout.Dimensions {
			gtx.Constraints = layout.Constraints{Max: image.Pt(max(0, cw-av-dp(gtx, 8)), lh)}
			return label(gtx, rc.th, textStyle{size: 15, weight: theme.Black, col: c.Text, lineH: 22, maxLines: 1}, name)
		})
		y += lh
	}
	if visible {
		g := gtx
		g.Constraints = layout.Constraints{Max: image.Pt(cw, 1<<20)}
		r.handler.h, r.handler.m, r.handler.anchor = rc.h, m, rc.origin
		rec2 := op.Record(gtx.Ops)
		body := r.rv.Layout(g, rc.st, &r.handler)
		call := rec2.Stop()
		placeCall(gtx, call, bx, y)
		r.bodyRect = image.Rectangle{Min: image.Pt(bx, y), Max: image.Pt(bx+body.Size.X, y+body.Size.Y)}
		y += body.Size.Y
	} else { // MessageTombstone's customDeletedText
		d := at(gtx, bx, y, func(gtx layout.Context) layout.Dimensions {
			gtx.Constraints = layout.Constraints{Max: image.Pt(cw, 1<<20)}
			return label(gtx, rc.th, textStyle{size: 15, col: c.Text2, lineH: 22}, nsQuipRoot.T("This content was deleted."))
		})
		y += d.Size.Y
	}
	y -= dp(gtx, 4) // .p-quip_comment_root__body margin-bottom -4px
	content := rec.Stop()
	rrect(gtx.Ops, image.Rect(padL, padT, padL+bar, max(y, padT+dp(gtx, 8))), dp(gtx, 8), barCol)
	content.Add(gtx.Ops)
	r.reactRects = r.reactRects[:0]
	r.addRect = image.Rectangle{}
	h := y + padB
	r.height = h
	return layout.Dimensions{Size: image.Pt(w, h)}
}

// reactable is canAddReaction for r's toolbar: a canvas comment's file
// channel follows the canvas's access (core.CanAddReaction), and a
// comment thread's root in its thread (ThreadRootCanvas) has
// showAddReactionButton and showQuickReactions off.
func reactable(app *core.App, r *row, thread bool) bool {
	if app == nil {
		return true
	}
	if thread && r.src.IsCommentRoot() {
		return false
	}
	return app.CanAddReaction(&r.src)
}

func drawAvatarAt(gtx layout.Context, rc *rowCtx, url string, rect image.Rectangle) {
	t := op.Offset(rect.Min).Push(gtx.Ops)
	drawAvatar(gtx, rc.h, url, image.Rectangle{Max: rect.Size()}, rc.c)
	t.Pop()
}

func cmtFirst(s ...string) string {
	for _, x := range s {
		if x != "" {
			return x
		}
	}
	return ""
}
