package blockkit

import (
	"context"
	"encoding/json"
	"image"
	"sync"
	"time"

	"gioui.org/io/event"
	"gioui.org/io/key"
	"gioui.org/io/pointer"
	"gioui.org/layout"
	"gioui.org/op"
	"gioui.org/op/clip"
	"gioui.org/unit"

	"github.com/some-du6e/clack/internal/core"
	"github.com/some-du6e/clack/internal/ui/popups"
	"github.com/some-du6e/clack/internal/ui/refocus"
	"github.com/some-du6e/clack/internal/ui/shortcut"
	"github.com/some-du6e/clack/internal/ui/theme"
)

// Stack is the modal overlay: a stack of views (views.push adds one,
// back pops it), like Slack's BlockKitModal.
//
// Slack's rules (BlockKitModal, 2026-11):
//   - Submit: client-side validation first; then views.submit. Answer
//     update/push: the new view (from the answer or view_updated/
//     view_opened); errors: messages under the inputs; clear: close the
//     whole stack; none: back to the previous view, or close.
//     toast_message shows a toast. Other failures show a banner with
//     Retry.
//   - Close (footer) / Escape / back: pop to the previous view (the
//     whole stack with clear_on_close), and views.close the view.
//   - The × closes the whole stack (views.close with root_view_id).
type Stack struct {
	h     Host
	views []*Surface
	done  bool

	busy   bool
	banner string
	// bannerRetry: the banner is BlockKitModalError's default ("We had
	// some trouble connecting. Try again?" with the link); an app's
	// view_error shows alone.
	bannerRetry bool
	closeTap    tap
	backTap     tap
	cancel      tap
	submit      tap
	retry       tap
	list        layout.List
	tag         int
	scrim       int
	focused     bool
	dialog      *core.Dialog
	dlgAppID    string
	// keyboard (popups' modal model): the ring shows on a focus reached
	// by Tab; prev had the focus before the stack opened
	ring  bool
	prev  event.Tag
	ptr   int
	wasUp bool
}

func newStack(h Host, v *core.View) *Stack {
	s := &Stack{h: h}
	s.list.Axis = layout.Vertical
	s.views = []*Surface{s.surface(v)}
	return s
}

func (s *Stack) surface(v *core.View) *Surface {
	sf := newSurface(s.h, v)
	sf.onAction = func() { s.banner, s.bannerRetry = "", false }
	return sf
}

func (s *Stack) top() *Surface { return s.views[len(s.views)-1] }

func (s *Stack) root() string {
	if len(s.views) == 0 {
		return ""
	}
	return s.views[0].view.RootViewID
}

func (s *Stack) has(id string) bool {
	for _, v := range s.views {
		if v.view.ID == id {
			return true
		}
	}
	return false
}

func (s *Stack) push(v *core.View) {
	if s.has(v.ID) {
		s.update(v)
		return
	}
	s.views = append(s.views, s.surface(v))
	s.list.Position = layout.Position{}
	s.busy, s.banner, s.bannerRetry = false, "", false
	s.h.Invalidate()
}

// update replaces a view in place (views.update), keeping typed values.
func (s *Stack) update(v *core.View) {
	for i, sf := range s.views {
		if sf.view.ID == v.ID {
			if v.Hash != "" && v.Hash == sf.view.Hash && v.Blocks == nil {
				return
			}
			s.views[i].set(v, true)
			if i == len(s.views)-1 {
				s.busy = false
			}
			s.h.Invalidate()
			return
		}
	}
}

func (s *Stack) Dismissed() bool { return s.done }

// Dismiss is Escape from the root (an input had the focus): back out
// of the top view. The root drops the overlay right after Dismiss, so the
// stack shows itself again first; if it closes, the copy is just a
// dismissed overlay the root prunes. Any speedbump goes above it.
func (s *Stack) Dismiss() {
	if s.done {
		return
	}
	s.h.ShowOverlay(s)
	s.userBack()
}

// Slack's speedbump (BlockKitModal getShouldShowSpeedBump): leaving a
// view that has a submit button and stateful inputs whose values changed
// asks first. Backing out of a pushed view says "Continue", otherwise
// "Leave" (danger); "Keep Editing" stays.
func (s *Stack) changed() bool {
	if len(s.views) == 0 {
		return false
	}
	sf := s.top()
	if sf.view.Submit == nil && s.dialog == nil {
		return false
	}
	return sf.dirty()
}

func (s *Stack) speedbump(pushed bool, leave func()) {
	if !s.changed() {
		leave()
		return
	}
	cont, style := "Leave", "danger"
	if pushed {
		cont, style = "Continue", "primary"
	}
	showConfirm(s.h, &Confirm{
		Title:   &Text{Type: "plain_text", Text: "Are you sure you want to leave?"},
		Text:    &Text{Type: "plain_text", Text: "It looks like you’re still working on this. You may lose the info you’ve entered so far if you leave now."},
		Confirm: &Text{Type: "plain_text", Text: cont},
		Deny:    &Text{Type: "plain_text", Text: "Keep Editing"},
		Style:   style,
	}, leave, nil)
}

// userBack / userCloseAll are the user's Close, Escape, back and ×.
func (s *Stack) userBack() {
	pushed := len(s.views) > 1 && !s.top().view.ClearOnClose
	s.speedbump(pushed, s.back)
}

func (s *Stack) userCloseAll() { s.speedbump(false, s.closeAll) }

// back pops the top view (closing the stack at the root, or entirely
// with clear_on_close) and tells Slack.
func (s *Stack) back() {
	if s.dialog != nil {
		d := s.dialog
		s.close()
		go func() {
			ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
			defer cancel()
			_ = s.h.Core().Interactive().DialogCancel(ctx, d)
		}()
		return
	}
	t := s.top().view
	if t.ClearOnClose || len(s.views) == 1 {
		s.closeViews(t.ID, t.RootViewID)
		s.close()
		return
	}
	s.closeViews(t.ID, "")
	s.views = s.views[:len(s.views)-1]
	s.busy, s.banner, s.bannerRetry = false, "", false
	s.list.Position = layout.Position{}
}

// closeAll is the ×: the whole stack goes.
func (s *Stack) closeAll() {
	if s.dialog != nil {
		s.back()
		return
	}
	t := s.top().view
	s.closeViews(t.ID, t.RootViewID)
	s.close()
}

func (s *Stack) close() {
	s.done = true
	if s.prev != nil { // useRestoreFocus
		refocus.Restore(s.prev, &s.tag, &s.closeTap, &s.backTap, &s.cancel, &s.submit)
		s.prev = nil
	}
	s.h.Invalidate()
}

// stops is the tab order: the header (back, close) first, as in Slack's
// c-dialog, then the footer's Close and Submit. The body's inputs take
// the focus with the mouse.
func (s *Stack) stops() []event.Tag {
	var out []event.Tag
	if len(s.views) > 1 {
		out = append(out, &s.backTap)
	}
	out = append(out, &s.closeTap, &s.cancel)
	if v := s.top().view; (v.Submit != nil || s.dialog != nil) && !v.SubmitDisabled && !s.busy {
		out = append(out, &s.submit)
	}
	return out
}

// keys runs Tab / Shift+Tab (trapped, wrapping) and the initial focus:
// BlockKitModal transitions the focus to MODAL_ACTIVE_FORM, whose
// focusOnFormElements picks the first form element in the body, else the
// submit button, else cancel; with no autoFocus there is no ring.
func (s *Stack) keys(gtx C) {
	for _, t := range []*tap{&s.closeTap, &s.backTap, &s.cancel, &s.submit} {
		t.keys = true
	}
	if !s.focused {
		s.focused = true
		s.prev = refocus.Last(gtx.Now)
		st := s.stops()
		gtx.Execute(key.FocusCmd{Tag: st[len(st)-1]})
	}
	for {
		e, ok := gtx.Event(key.Filter{Name: key.NameTab, Optional: key.ModShift})
		if !ok {
			break
		}
		ke, ok := e.(key.Event)
		if !ok || ke.State != key.Press {
			continue
		}
		st := s.stops()
		cur := -1
		for i, t := range st {
			if gtx.Focused(t) {
				cur = i
			}
		}
		back := ke.Modifiers.Contain(key.ModShift)
		next := 0
		switch {
		case cur < 0 && back:
			next = len(st) - 1
		case back:
			next = (cur - 1 + len(st)) % len(st)
		case cur >= 0:
			next = (cur + 1) % len(st)
		}
		gtx.Execute(key.FocusCmd{Tag: st[next]})
		s.ring = true
		s.h.Invalidate()
	}
}

// pointerSeen hides the ring after a mouse press anywhere (pass-through,
// registered over the card).
func (s *Stack) pointerSeen(gtx C) {
	for {
		e, ok := gtx.Event(pointer.Filter{Target: &s.ptr, Kinds: pointer.Press})
		if !ok {
			break
		}
		if pe, ok := e.(pointer.Event); ok && pe.Kind == pointer.Press {
			s.ring = false
		}
	}
	defer pointer.PassOp{}.Push(gtx.Ops).Pop()
	defer clip.Rect{Max: gtx.Constraints.Max}.Push(gtx.Ops).Pop()
	event.Op(gtx.Ops, &s.ptr)
}

// ringAt draws the focus ring around a focused button at r.
func (s *Stack) ringAt(gtx C, t *tap, r image.Rectangle) {
	if s.ring && gtx.Focused(t) {
		popups.FocusRing(gtx, s.h.Theme(), r, dp(gtx, 8))
	}
}

// FocusedButton names the stack's focused button ("" for none) and
// whether it shows the ring (tests).
func (s *Stack) FocusedButton(gtx C) (string, bool) {
	for name, t := range map[string]*tap{"close": &s.closeTap, "back": &s.backTap, "cancel": &s.cancel, "submit": &s.submit} {
		if gtx.Focused(t) {
			return name, s.ring
		}
	}
	return "", false
}

func (s *Stack) closeViews(id, root string) {
	h := s.h
	go func() {
		ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cancel()
		_ = h.Core().Interactive().CloseView(ctx, id, root)
	}()
}

// doSubmit validates and submits the top view.
func (s *Stack) doSubmit() {
	if s.busy {
		return
	}
	sf := s.top()
	if !sf.validate() {
		return
	}
	s.busy, s.banner, s.bannerRetry = true, "", false
	if s.dialog != nil {
		s.submitDialog(sf)
		return
	}
	v := sf.view
	values := sf.submitValues()
	run(s.h, 15*time.Second, func(ctx context.Context) (core.SubmitResult, error) {
		return dispatchSubmit(ctx, s.h, v.ID, values)
	}, func(r core.SubmitResult, err error) {
		if s.done {
			return
		}
		s.busy = false
		if err != nil {
			s.failed("")
			return
		}
		if r.Toast != "" {
			s.h.Toast(r.Toast)
		}
		switch r.ResponseAction {
		case "errors":
			for k, m := range r.Errors {
				sf.errs[k] = m
			}
			sf.changed()
			if r.Code != "" && len(r.Errors) == 0 {
				s.failed(r.ViewError)
			}
			return
		case "update":
			if r.View != nil {
				if r.View.ID == v.ID || r.View.ID == "" {
					sf.set(r.View, true)
				} else {
					s.views[len(s.views)-1] = s.surface(r.View)
				}
			} else {
				s.busy = true // the new view arrives as view_updated
			}
			return
		case "push":
			if r.View != nil {
				s.push(r.View)
			}
			return
		case "clear":
			s.close()
			return
		}
		if r.Code != "" {
			s.failed(r.ViewError)
			return
		}
		// no response_action (handleSubmitResolved): back to the previous
		// view if this one was pushed, else close
		if len(s.views) > 1 {
			s.views = s.views[:len(s.views)-1]
			s.list.Position = layout.Position{}
			return
		}
		s.close()
	})
}

// ---- layout ----

func (s *Stack) Layout(gtx C) D {
	drain()
	if s.done || len(s.views) == 0 {
		return D{}
	}
	th := s.h.Theme()
	cl := colorsOf(th)
	win := gtx.Constraints.Max
	gtx.Constraints.Min = image.Point{}

	// backdrop: rgba(29,28,29,.7); presses on it do nothing (Slack)
	rrect(gtx, image.Rectangle{Max: win}, 0, cl.Backdrop)
	for {
		if _, ok := gtx.Event(pointer.Filter{Target: &s.scrim, Kinds: pointer.Press | pointer.Scroll,
			ScrollY: pointer.ScrollRange{Min: -1 << 20, Max: 1 << 20}}); !ok {
			break
		}
	}
	func() {
		defer clip.Rect{Max: win}.Push(gtx.Ops).Pop()
		event.Op(gtx.Ops, &s.scrim)
	}()

	s.keys(gtx)
	// command+enter submits (BlockKitModal's Mousetrap binding: the meta
	// key, ⌘ on macOS)
	cmd := key.ModSuper
	if shortcut.GOOS == "darwin" {
		cmd = key.ModCommand
	}
	for {
		e, ok := gtx.Event(key.Filter{Focus: &s.tag, Name: key.NameEscape},
			key.Filter{Focus: &s.closeTap, Name: key.NameEscape}, key.Filter{Focus: &s.backTap, Name: key.NameEscape},
			key.Filter{Focus: &s.cancel, Name: key.NameEscape}, key.Filter{Focus: &s.submit, Name: key.NameEscape},
			key.Filter{Name: key.NameReturn, Required: cmd})
		if !ok {
			break
		}
		if ke, ok := e.(key.Event); ok && ke.State == key.Press {
			if ke.Name == key.NameEscape {
				s.userBack()
				return D{}
			}
			if s.top().view.Submit != nil {
				s.doSubmit()
			}
		}
	}

	sf := s.top()
	v := sf.view
	if s.closeTap.clicked(gtx, s.h) {
		s.userCloseAll()
	}
	if s.backTap.clicked(gtx, s.h) || s.cancel.clicked(gtx, s.h) {
		s.userBack()
	}
	if s.done {
		return D{}
	}
	if s.submit.clicked(gtx, s.h) || s.retry.clicked(gtx, s.h) {
		s.doSubmit()
	}

	mw := min(dp(gtx, 520), win.X-dp(gtx, 32))
	headH := dp(gtx, 68)
	pad := dp(gtx, 24)
	hasFooter := v.Submit != nil || v.Close != nil || true
	footH := 0
	if hasFooter {
		footH = dp(gtx, 36) + 2*dp(gtx, 20)
	}
	bannerH := 0
	if s.banner != "" {
		bannerH = dp(gtx, 44)
	}
	// .c-dialog__content: max-height 640px (96% under 657px tall),
	// centered; .p-block_kit_modal .c-dialog__body: min-height 90px,
	// padding 8px 20px 16px 24px
	maxH := dp(gtx, 640)
	if win.Y < dp(gtx, 657) {
		maxH = win.Y * 96 / 100
	}
	maxBody := maxH - headH - footH - bannerH
	padT, padB := dp(gtx, 8), dp(gtx, 16)
	// measure the body
	bw := mw - pad - dp(gtx, 20)
	m := op.Record(gtx.Ops)
	g := gtx
	g.Constraints = layout.Constraints{Max: image.Pt(bw, 1<<20)}
	bd := sf.Layout(g)
	m.Stop()
	content := bd.Size.Y + padT + padB
	bodyH := min(max(content, dp(gtx, 90)), max(maxBody, dp(gtx, 90)))
	mh := headH + bannerH + bodyH + footH
	pos := image.Pt((win.X-mw)/2, max((win.Y-mh)/2, 0))

	// the card: Slack's c-sk-modal (role dialog), so a theme's
	// [role=dialog] background and border land on it as in Slack
	defer s.pointerSeen(gtx)
	st := popups.ModalClass.Enter(gtx, 0, 0)
	defer st.Leave()
	if st.Hidden() {
		return D{Size: win}
	}
	shadow := func(gtx C, r image.Rectangle, rad int) { theme.Shadow(gtx, r, rad, dp(gtx, 24), cl.ModalShadow) }
	popups.StyledCard(gtx, st, cl.Bg, cl.Ring, shadow, pos, image.Pt(mw, mh), dp(gtx, 8), func(gtx C) {
		r := image.Rect(0, 0, mw, mh)
		func() {
			defer clip.Rect(r).Push(gtx.Ops).Pop()
			event.Op(gtx.Ops, &s.tag)
		}()
		// header
		s.header(gtx, mw, headH, v)
		y := headH
		if s.banner != "" {
			at(gtx, image.Pt(pad, y), func(gtx C) D {
				w := mw - 2*pad
				rr := image.Rect(0, 0, w, dp(gtx, 36))
				rrect(gtx, rr, dp(gtx, 8), rgba(0xe01e5a, .1))
				at(gtx, image.Pt(dp(gtx, 12), dp(gtx, 10)), func(gtx C) D { return icon(gtx, "warning", 16, cl.ErrorText) })
				at(gtx, image.Pt(dp(gtx, 36), 0), func(gtx C) D {
					gtx.Constraints = layout.Constraints{Max: image.Pt(w-dp(gtx, 48), rr.Dy())}
					return vcenter(gtx, rr.Dy(), func(gtx C) D { return label(gtx, th, 15, theme.Regular, cl.Text, s.banner) })
				})
				if s.bannerRetry { // "We had some trouble connecting. <a>Try again?</a>"
					tw := measure(gtx, th, 15, theme.Regular, s.banner)
					rw := measure(gtx, th, 15, theme.Regular, "Try again?")
					at(gtx, image.Pt(dp(gtx, 36)+tw, 0), func(gtx C) D {
						vcenter(gtx, rr.Dy(), func(gtx C) D { return label(gtx, th, 15, theme.Regular, cl.Link, "Try again?") })
						s.retry.add(gtx, image.Pt(rw, rr.Dy()))
						return D{}
					})
				}
				return D{}
			})
			y += bannerH
		}
		// body (scrolls)
		at(gtx, image.Pt(0, y), func(gtx C) D {
			defer clip.Rect{Max: image.Pt(mw, bodyH)}.Push(gtx.Ops).Pop()
			gtx.Constraints = layout.Exact(image.Pt(mw, bodyH))
			d := s.list.Layout(gtx, 1, func(gtx C, _ int) D {
				return layout.Inset{Left: unit.Dp(24), Right: unit.Dp(20), Top: unit.Dp(8), Bottom: unit.Dp(16)}.Layout(gtx, func(gtx C) D {
					gtx.Constraints = layout.Constraints{Max: image.Pt(bw, 1<<20)}
					return sf.Layout(gtx)
				})
			})
			// the surface's window origin as drawn (ModalFields; a
			// theme's card border isn't counted)
			sf.org = pos.Add(image.Pt(dp(gtx, 24), y+dp(gtx, 8)-s.list.Position.Offset))
			if s.busy {
				rrect(gtx, image.Rect(0, 0, mw, bodyH), 0, theme.WithAlpha(cl.Bg, 0xb0))
				sp := dp(gtx, 32)
				at(gtx, image.Pt((mw-sp)/2, (bodyH-sp)/2), func(gtx C) D { spinner(gtx, sp, cl.Text2); return D{} })
			}
			return d
		})
		y += bodyH
		// footer
		if hasFooter {
			// .p-block_kit_modal .c-dialog__footer: always a 1px top border
			rrect(gtx, image.Rect(0, y, mw, y+max(1, dp(gtx, 1))), 0, cl.FooterBorder)
			s.footer(gtx, image.Pt(0, y), mw, footH, v)
		}
	})
	return D{Size: win}
}

func (s *Stack) header(gtx C, mw, hh int, v *core.View) {
	th := s.h.Theme()
	cl := colorsOf(th)
	x := dp(gtx, 24)
	if len(s.views) > 1 {
		bs := dp(gtx, 36)
		at(gtx, image.Pt(dp(gtx, 16), (hh-bs)/2), func(gtx C) D {
			s.ringAt(gtx, &s.backTap, image.Rect(0, 0, bs, bs))
			if s.backTap.g.Hovered() {
				rrect(gtx, image.Rect(0, 0, bs, bs), dp(gtx, 8), cl.BgHover)
			}
			at(gtx, image.Pt((bs-dp(gtx, 20))/2, (bs-dp(gtx, 20))/2), func(gtx C) D { return icon(gtx, "back", 20, cl.Text2) })
			s.backTap.add(gtx, image.Pt(bs, bs))
			return D{}
		})
		x = dp(gtx, 16) + bs + dp(gtx, 8)
	}
	is := dp(gtx, 36)
	if u := s.appIcon(v); u != "" {
		if img, ok := s.h.Images().Get(u, is); ok {
			at(gtx, image.Pt(x, (hh-is)/2), func(gtx C) D { return theme.Image(gtx, img, image.Pt(is, is), dp(gtx, 4)) })
		} else {
			at(gtx, image.Pt(x, (hh-is)/2), func(gtx C) D { return th.Placeholder(gtx, image.Pt(is, is), dp(gtx, 4)) })
		}
		x += is + dp(gtx, 16)
	}
	title := titleOf(v.Title)
	if s.dialog != nil {
		title = s.dialog.Title
	}
	cs := dp(gtx, 36)
	at(gtx, image.Pt(x, 0), func(gtx C) D {
		gtx.Constraints = layout.Constraints{Max: image.Pt(mw-x-dp(gtx, 24)-cs-dp(gtx, 8), hh)}
		return vcenter(gtx, hh, func(gtx C) D { return label(gtx, th, 22, theme.Black, cl.Text, title) })
	})
	at(gtx, image.Pt(mw-dp(gtx, 16)-cs, (hh-cs)/2), func(gtx C) D {
		s.ringAt(gtx, &s.closeTap, image.Rect(0, 0, cs, cs))
		if s.closeTap.g.Hovered() {
			rrect(gtx, image.Rect(0, 0, cs, cs), dp(gtx, 8), cl.BgHover)
		}
		at(gtx, image.Pt((cs-dp(gtx, 20))/2, (cs-dp(gtx, 20))/2), func(gtx C) D { return icon(gtx, "close", 20, cl.Text2) })
		s.closeTap.add(gtx, image.Pt(cs, cs))
		return D{}
	})
}

// appIcons caches app icons by app id (apps.profile.get; memory only).
var (
	appIconsMu sync.Mutex
	appIcons   = map[string]string{}
)

func (s *Stack) appIcon(v *core.View) string {
	c := s.h.Core()
	if c == nil {
		return ""
	}
	if v.BotID != "" {
		if b, ok := c.S.Bot(v.BotID); ok && b.Icon != "" {
			return b.Icon
		}
	}
	if v.AppID == "" {
		return ""
	}
	appIconsMu.Lock()
	u, known := appIcons[v.AppID]
	if !known {
		appIcons[v.AppID] = "" // one fetch per app
	}
	appIconsMu.Unlock()
	if !known {
		h, bot, app := s.h, v.BotID, v.AppID
		go func() {
			ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
			defer cancel()
			if p, err := c.Interactive().AppProfileOf(ctx, bot, app); err == nil && p.Icon != "" {
				appIconsMu.Lock()
				appIcons[app] = p.Icon
				appIconsMu.Unlock()
				h.Invalidate()
			}
		}()
	}
	return u
}

func titleOf(raw json.RawMessage) string {
	var t Text
	if json.Unmarshal(raw, &t) == nil {
		return unescape(t.Text)
	}
	return ""
}

func (s *Stack) footer(gtx C, org image.Point, mw, fh int, v *core.View) {
	th := s.h.Theme()
	right := mw - dp(gtx, 24)
	bh := dp(gtx, 36)
	y := org.Y + (fh-bh)/2
	btn := func(t *tap, txt, style string, disabled bool) {
		w := max(measure(gtx, th, 15, theme.Bold, txt)+2*dp(gtx, 16), dp(gtx, 80))
		right -= w
		at(gtx, image.Pt(right, y), func(gtx C) D {
			sz := image.Pt(w, bh)
			if !disabled {
				s.ringAt(gtx, t, image.Rectangle{Max: sz})
			}
			drawButton(gtx, th, sz, style, txt, 8, 15, t.g.Hovered(), false, disabled)
			if !disabled {
				t.add(gtx, sz)
			}
			return D{}
		})
		right -= dp(gtx, 12)
	}
	if v.Submit != nil || s.dialog != nil {
		txt := titleOf(v.Submit)
		if s.dialog != nil {
			txt = firstNonEmpty(s.dialog.SubmitLabel, "Submit")
		}
		btn(&s.submit, firstNonEmpty(txt, "Submit"), "primary", v.SubmitDisabled || s.busy)
	}
	// renderBackButton: the view's close text, else "Close"
	closeTxt := firstNonEmpty(titleOf(v.Close), "Close")
	btn(&s.cancel, closeTxt, "", false)
}

// ---- legacy dialogs ----

// openDialog loads a dialog.open dialog and shows it as a modal.
func openDialog(h Host, d *core.Dialog) {
	if d == nil {
		return
	}
	run(h, 10*time.Second, func(ctx context.Context) (*core.Dialog, error) {
		return d, h.Core().Interactive().DialogGet(ctx, d)
	}, func(d *core.Dialog, err error) {
		if err != nil {
			h.Toast("Couldn't open the app's dialog")
			return
		}
		v := &core.View{ID: d.ID, RootViewID: d.ID, Type: "dialog", AppID: d.AppID,
			Title: mustJSON(Text{Type: "plain_text", Text: d.Title}), Submit: mustJSON(Text{Type: "plain_text", Text: firstNonEmpty(d.SubmitLabel, "Submit")}),
			Blocks: dialogBlocks(d.Elements)}
		st := newStack(h, v)
		st.dialog = d
		h.ShowOverlay(st)
	})
}

func mustJSON(v any) json.RawMessage { b, _ := json.Marshal(v); return b }

// dialogBlocks converts dialog elements (text, textarea, select) into
// input blocks keyed by the element name.
func dialogBlocks(raw json.RawMessage) json.RawMessage {
	var els []struct {
		Type        string `json:"type"`
		Subtype     string `json:"subtype"`
		Label       string `json:"label"`
		Name        string `json:"name"`
		Value       string `json:"value"`
		Placeholder string `json:"placeholder"`
		Optional    bool   `json:"optional"`
		Hint        string `json:"hint"`
		MaxLength   int    `json:"max_length"`
		MinLength   int    `json:"min_length"`
		DataSource  string `json:"data_source"`
		Options     []struct {
			Label string `json:"label"`
			Value string `json:"value"`
		} `json:"options"`
	}
	_ = json.Unmarshal(raw, &els)
	pt := func(s string) map[string]any { return map[string]any{"type": "plain_text", "text": s} }
	var blocks []map[string]any
	for _, e := range els {
		el := map[string]any{"action_id": e.Name}
		switch e.Type {
		case "select":
			switch e.DataSource {
			case "users":
				el["type"] = "users_select"
			case "channels":
				el["type"] = "channels_select"
			case "conversations":
				el["type"] = "conversations_select"
			default:
				el["type"] = "static_select"
				var opts []map[string]any
				for _, o := range e.Options {
					opt := map[string]any{"text": pt(o.Label), "value": o.Value}
					opts = append(opts, opt)
					if o.Value == e.Value {
						el["initial_option"] = opt
					}
				}
				el["options"] = opts
			}
		default:
			el["type"] = "plain_text_input"
			switch e.Subtype {
			case "email":
				el["type"] = "email_text_input"
			case "url":
				el["type"] = "url_text_input"
			case "number":
				el["type"] = "number_input"
				el["is_decimal_allowed"] = true
			}
			if e.Type == "textarea" {
				el["multiline"] = true
			}
			if e.Value != "" {
				el["initial_value"] = e.Value
			}
			if e.MaxLength > 0 {
				el["max_length"] = e.MaxLength
			}
			if e.MinLength > 0 {
				el["min_length"] = e.MinLength
			}
		}
		if e.Placeholder != "" {
			el["placeholder"] = pt(e.Placeholder)
		}
		b := map[string]any{"type": "input", "block_id": e.Name, "label": pt(e.Label), "optional": e.Optional, "element": el}
		if e.Hint != "" {
			b["hint"] = pt(e.Hint)
		}
		blocks = append(blocks, b)
	}
	out, _ := json.Marshal(blocks)
	return out
}

func (s *Stack) submitDialog(sf *Surface) {
	sub := map[string]string{}
	for bid, acts := range sf.vals {
		for _, v := range acts {
			switch {
			case v.SelectedOption != nil:
				sub[bid] = v.SelectedOption.Value
			case v.SelectedUser != "":
				sub[bid] = v.SelectedUser
			case v.SelectedChan != "":
				sub[bid] = v.SelectedChan
			case v.SelectedConv != "":
				sub[bid] = v.SelectedConv
			default:
				sub[bid] = v.Text
			}
		}
	}
	d := s.dialog
	run(s.h, 15*time.Second, func(ctx context.Context) (map[string]string, error) {
		return s.h.Core().Interactive().DialogSubmit(ctx, d, sub)
	}, func(errs map[string]string, err error) {
		s.busy = false
		if len(errs) > 0 {
			for k, m := range errs {
				sf.errs[k] = m
			}
			sf.changed()
			return
		}
		if err != nil {
			s.failed("")
			return
		}
		s.close()
	})
}

// failed shows BlockKitModalError: the app's view_error, else Slack's
// "We had some trouble connecting. Try again?" (the link resubmits).
func (s *Stack) failed(viewError string) {
	s.banner, s.bannerRetry = viewError, false
	if viewError == "" {
		s.banner, s.bannerRetry = "We had some trouble connecting. ", true
	}
}
