package messages

// The thread pane header's title (srGL renderThreadTitle, CDN build
// fa938a086569; core.ThreadHeaderTitle picks it):
//
//   - EditableThreadTitle (boot-shared bCNd) under agent_channel_thread_naming
//     (a channel thread whose root has an agent_session) or
//     agent_dm_thread_naming (an agent app's DM): .titleRow__LNySH
//     (inline-flex, max-width 100%, Black, align center, gap 8) holding the
//     title in Truncate lines 1 withTooltip (TruncationTooltip: the whole
//     title, only while it's cut) or "Thread", then .editButton__k1SY7: on
//     a pointer device clipped away (1px, absolute) until the row is
//     hovered. The button is IconButton size small (28px, the 16px edit
//     glyph, content-sec, content-pry and the hover fill on hover), its
//     tooltip "Edit thread name" on top; it opens EditAgentThreadNameModal
//     (n5nG) on agent_session?.title ?? assistant_app_thread?.title.
//     Saving upserts the root's agent_session.title at once
//     (useRenameAgentThread) and puts the old one back if
//     agents.threads.rename fails.
//   - AgentThreadTitle (5RBz) in a DM whose root is an assistant thread:
//     AssistantThreadTitleLabel, assistant_app_thread.title (or "Thread")
//     in one truncated line, Black.
//   - otherwise "Thread".
//
// The title container and the subtitle (.p-flexpane__title_container and
// .p-flexpane__subtitle, both min-width 0 in the header's row) shrink in
// proportion to their widths when the row is too narrow for both.

import (
	"context"
	"image"
	"time"

	"gioui.org/gesture"
	"gioui.org/io/pointer"
	"gioui.org/layout"
	"gioui.org/op/clip"
	"gioui.org/unit"
	"gioui.org/widget"

	"github.com/some-du6e/clack/internal/core"
	"github.com/some-du6e/clack/internal/ui/icons"
	"github.com/some-du6e/clack/internal/ui/popups"
	"github.com/some-du6e/clack/internal/ui/theme"
)

// threadTitleUI is the title row's widgets (UI goroutine only).
type threadTitleUI struct {
	hover   gesture.Hover
	hovered bool
	edit    widget.Clickable
	editTip popups.TipState
	textTip popups.TipState
	asked   string // the DM partner whose app profile was asked for
	last    core.ThreadTitle
	rect    image.Rectangle
	editR   image.Rectangle // the edit button, view coordinates (last frame; zero when hidden)
}

// headerTitle is what the header's title shows now; it asks once for the
// DM partner's app profile when that can still make the DM an agent
// app's (isAgentAppChannel).
func (v *ThreadView) headerTitle(root core.Message, rootOK bool) core.ThreadTitle {
	app := v.h.Core()
	if app == nil {
		return core.ThreadTitle{}
	}
	if u, pending := app.AgentAppChannelPending(v.ch); pending && v.tt.asked != u {
		v.tt.asked = u
		h := v.h
		go func() {
			ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
			defer cancel()
			if _, err := app.AppCardOf(ctx, u, ""); err == nil {
				h.Invalidate()
			}
		}()
	}
	var r *core.Message
	if rootOK {
		r = &root
	}
	return app.ThreadHeaderTitle(v.ch, v.ts, r)
}

// titleWidth is the title row's natural width: the text, and the edit
// button (8px after it) while it shows.
func (v *ThreadView) titleWidth(gtx layout.Context, th *theme.Theme, ts textStyle, t core.ThreadTitle) int {
	w := measure(gtx, th, ts, threadTitleText(t)).X
	if t.Editable && v.tt.hovered {
		w += dp(gtx, 8) + dp(gtx, 28)
	}
	return w
}

func threadTitleText(t core.ThreadTitle) string {
	if t.Text == "" {
		return threadsNS.T("Thread")
	}
	return t.Text
}

// layoutTitle draws the title at (x, y) in at most maxW.
func (v *ThreadView) layoutTitle(gtx layout.Context, th *theme.Theme, c *colors, x, y, maxW int, ts textStyle, t core.ThreadTitle) layout.Dimensions {
	if v.tt.edit.Clicked(gtx) && t.Editable {
		v.openRename()
	}
	v.tt.editR = image.Rectangle{}
	v.tt.last = t
	return at(gtx, x, y, func(gtx layout.Context) layout.Dimensions {
		txt := threadTitleText(t)
		lh := gtx.Sp(unit.Sp(ts.lineH))
		bs := dp(gtx, 28)
		showEdit := t.Editable && v.tt.hovered
		textMax := maxW
		if showEdit {
			textMax = max(0, maxW-dp(gtx, 8)-bs)
		}
		natural := measure(gtx, th, ts, txt).X
		g := gtx
		g.Constraints = layout.Constraints{Max: image.Pt(textMax, lh)}
		td := lineBox(g, ts, func(gtx layout.Context) layout.Dimensions { return label(gtx, th, ts, txt) })
		td.Size.X = min(td.Size.X, textMax)
		h := td.Size.Y
		w := td.Size.X
		if showEdit {
			w += dp(gtx, 8) + bs
			h = max(h, bs)
		}
		// the row's hover (.titleRow:hover) covers the text and the button;
		// the button is drawn inside it, so both see the pointer
		row := image.Rect(0, min(0, (td.Size.Y-h)/2), w, min(0, (td.Size.Y-h)/2)+h)
		if !t.Editable {
			row = image.Rectangle{Max: td.Size}
		}
		area := clip.Rect(row).Push(gtx.Ops)
		v.tt.hovered = v.tt.hover.Update(gtx.Source)
		v.tt.hover.Add(gtx.Ops)
		if natural > textMax { // TruncationTooltip: only while cut
			popups.Tooltip(gtx, th, &v.tt.textTip, v.tt.hovered && !v.tt.edit.Hovered(), td.Size, popups.Above, txt, "")
		}
		if showEdit {
			bx := td.Size.X + dp(gtx, 8)
			by := (td.Size.Y - bs) / 2
			v.tt.editR = image.Rect(bx, by, bx+bs, by+bs).Add(image.Pt(x, y)).Add(v.Origin)
			at(gtx, bx, by, func(gtx layout.Context) layout.Dimensions {
				gtx.Constraints = layout.Exact(image.Pt(bs, bs))
				d := v.tt.edit.Layout(gtx, func(gtx layout.Context) layout.Dimensions {
					col := c.Text2 // c-icon_button--default: content-sec, content-pry on hover
					if v.tt.edit.Hovered() {
						col = c.Text
						rrect(gtx.Ops, image.Rectangle{Max: image.Pt(bs, bs)}, dp(gtx, 8), c.CloseHover)
					}
					is := dp(gtx, 16)
					at(gtx, (bs-is)/2, (bs-is)/2, func(gtx layout.Context) layout.Dimensions {
						return icons.Get(icons.Slack("edit"))(gtx, unit.Dp(16), col)
					})
					defer clip.Rect{Max: image.Pt(bs, bs)}.Push(gtx.Ops).Pop()
					pointer.CursorPointer.Add(gtx.Ops)
					return layout.Dimensions{Size: image.Pt(bs, bs)}
				})
				popups.Tooltip(gtx, th, &v.tt.editTip, v.tt.edit.Hovered(), d.Size, popups.Above, threadsNS.T("Edit thread name"), "")
				return d
			})
		}
		area.Pop()
		v.tt.rect = image.Rectangle{Max: image.Pt(w, td.Size.Y)}.Add(image.Pt(x, y))
		return layout.Dimensions{Size: image.Pt(w, td.Size.Y), Baseline: td.Baseline}
	})
}

// openRename is EditableThreadTitle's onClick: EditAgentThreadNameModal
// on the root's current name.
func (v *ThreadView) openRename() {
	app := v.h.Core()
	ph, ok := v.h.(popups.Host)
	if app == nil || !ok {
		return
	}
	root, rootOK := v.root()
	var r *core.Message
	if rootOK {
		r = &root
	}
	cur := core.AgentThreadNameOf(r)
	old, hadSession := "", false
	if r != nil && r.AgentSession != nil {
		old, hadSession = r.AgentSession.SessionTitle(), true
	}
	ch, ts := v.ch, v.ts
	ph.ShowOverlay(popups.NewAgentThreadNameModal(ph, cur, func(ctx context.Context, name string) error {
		if hadSession {
			app.SetAgentSessionTitle(ch, ts, name)
		}
		err := app.RenameAgentThread(ctx, ch, ts, name)
		if err != nil && hadSession {
			app.SetAgentSessionTitle(ch, ts, old)
		}
		return err
	}))
}

// HeaderTitle is the title the header drew last (tests).
func (v *ThreadView) HeaderTitle() core.ThreadTitle { return v.tt.last }

// TitleRect is the header's title row in view coordinates (tests).
func (v *ThreadView) TitleRect() image.Rectangle { return v.tt.rect.Add(v.Origin) }

// EditTitleRect is the title's edit button in window coordinates while it
// shows (tests).
func (v *ThreadView) EditTitleRect() image.Rectangle { return v.tt.editR }
