package huddleui

import (
	"image"
	"image/color"
	"strings"
	"sync"
	"time"

	"gioui.org/app"
	"gioui.org/f32"
	"gioui.org/font"
	"gioui.org/io/system"
	"gioui.org/op"
	"gioui.org/op/clip"
	"gioui.org/op/paint"
	"gioui.org/unit"
	"gioui.org/widget"

	"github.com/some-du6e/clack/internal/i18n"
	"github.com/some-du6e/clack/internal/ui/icons"
	"github.com/some-du6e/clack/internal/ui/theme"
)

// The invite window: Slack's HuddleInviteWindow (render ce74), opened by
// openHuddleInviteWindow as its own 380×280 window titled "Slack - Huddle"
// ("Clack - Huddle" here). client-boot-styles' rules:
//
//   - .p-huddle_invite_window_titlebar: 32px, base-inv-pry, the headphones
//     and "Invite to huddle" (13px bold) centered.
//   - .p-huddle_invite_window_content__body: centered, 8px padding; the
//     facepile (HuddleInviteWindowAvatar: up to 4 faces, each
//     min((width-8·(n-1))/n, 80) px, 12px corners, 8px apart; a rung
//     person at 50% with a paper-plane or clock corner) and 4px under it
//     the text, 18px bold in content-inv-pry.
//   - the footer: 0 4px 4px padding; the buttons 24px apart, 8px off the
//     bottom: 48px circles in surf-inv-pry with a 20px content-inv-sec icon
//     (Join: base-pry with a content-ter icon and a 1px otl-inv-pry ring),
//     each labelled under it, 4px down.
//   - "Join when you're ready!" (and the error) is a toast at the top.

// invInk is the invite surfaces' theme tokens (rollup-style-plastic:
// the aubergine theme palette, per mode).
type invInk struct {
	bg, text, sec, btn, btnHov, join, joinHov, joinInk, joinRing, surfSec color.NRGBA
}

func rgba(r, g, b uint8, a float32) color.NRGBA {
	return color.NRGBA{R: r, G: g, B: b, A: uint8(a*255 + .5)}
}

var invInks = [2]invInk{
	{ // light: aubergine-0 (249,237,255), -70 (131,56,138), -80 (97,31,105), -100 (57,6,58)
		bg: rgba(249, 237, 255, 1), text: rgba(57, 6, 58, 1), sec: rgba(57, 6, 58, .8),
		btn: rgba(57, 6, 58, .13), btnHov: rgba(57, 6, 58, .2),
		join: rgba(131, 56, 138, 1), joinHov: rgba(112, 41, 118, 1), joinInk: rgba(249, 237, 255, 1), joinRing: rgba(97, 31, 105, .13),
		surfSec: rgba(57, 6, 58, .06),
	},
	{ // dark: aubergine-0 (32,2,33), -70 (207,161,222), -80 (216,186,225), -100 (239,225,245)
		bg: rgba(32, 2, 33, 1), text: rgba(239, 225, 245, 1), sec: rgba(239, 225, 245, .8),
		btn: rgba(239, 225, 245, .13), btnHov: rgba(239, 225, 245, .2),
		join: rgba(207, 161, 222, 1), joinHov: rgba(222, 186, 233, 1), joinInk: rgba(32, 2, 33, 1), joinRing: rgba(216, 186, 225, .13),
		surfSec: rgba(239, 225, 245, .06),
	},
}

// invInkFor is the palette for th (a Slick theme's tokens win).
func invInkFor(th *theme.Theme) invInk {
	if th == nil {
		return invInks[0]
	}
	k := invInks[0]
	if th.Dark {
		k = invInks[1]
	}
	t := th.Tokens
	theme.SetColor(t, &k.bg, "var(--dt_color-theme-base-inv-pry)")
	theme.SetColor(t, &k.text, "var(--dt_color-theme-content-inv-pry)")
	theme.SetColor(t, &k.sec, "var(--dt_color-theme-content-inv-sec)")
	theme.SetColor(t, &k.btn, "var(--dt_color-theme-surf-inv-pry)")
	theme.SetColor(t, &k.join, "var(--dt_color-theme-base-pry)")
	theme.SetColor(t, &k.joinInk, "var(--dt_color-theme-content-ter)")
	theme.SetColor(t, &k.joinRing, "var(--dt_color-theme-otl-inv-pry)")
	theme.SetColor(t, &k.surfSec, "var(--dt_color-theme-surf-sec)")
	return k
}

// inviteView is what the invite window and card draw (a snapshot).
type inviteView struct {
	Channel, Sender, Text  string
	Faces                  []inviteFace
	JoiningSoon, Busy      bool
	Note                   string
	NoteErr                bool
	NoteUntil              time.Time // when the note goes (a redraw then)
	CardText               []span    // the sidebar card's sentence
	SenderAvatar, Overflow int
}

type inviteFace struct {
	User, Status string // Status: "", pending, joining_soon
}

// view snapshots inv for drawing.
func (c *Controller) view(inv *incoming) inviteView {
	c.mu.Lock()
	v := inviteView{Channel: inv.Channel, Sender: inv.Sender, JoiningSoon: inv.status == "joining_soon", Busy: inv.busy != ""}
	if inv.note != "" && time.Since(inv.noteAt) < 6*time.Second {
		v.Note, v.NoteErr, v.NoteUntil = inv.note, inv.noteErr, inv.noteAt.Add(6*time.Second)
	}
	live := append([]string(nil), c.live[inv.Channel].Participants...)
	c.mu.Unlock()
	self := c.selfID()
	var active []string // activeMemberIdsWithoutSender
	for _, u := range live {
		if u != inv.Sender && u != self {
			active = append(active, u)
		}
	}
	sender := c.displayName(inv.Sender)
	kind := ""
	var mpimOthers []string
	if app := c.h.Core(); app != nil {
		if conv, ok := app.Conv(inv.Channel); ok {
			kind = conv.Kind
			for _, m := range conv.Members {
				if m != inv.Sender && m != self {
					mpimOthers = append(mpimOthers, c.displayName(m))
				}
			}
		}
	}
	// HuddleInviteWindowText
	switch kind {
	case "im":
		v.Text = sender
	case "mpim":
		switch len(active) {
		case 0:
			v.Text = sender
		case 1: // HuddleInviteWindowText
			v.Text = nsHuddles.T("{inviteSenderMemberName} and {otherMemberName}", "inviteSenderMemberName", sender, "otherMemberName", c.displayName(active[0]))
		default:
			v.Text = nsHuddles.T("{inviteSenderMemberName}, {firstOtherMemberName}, and {numRemaining, plural, =1 {{numRemaining} other} other {{numRemaining} others}}",
				"inviteSenderMemberName", sender, "firstOtherMemberName", c.displayName(active[0]), "numRemaining", len(active)-1)
		}
	default:
		v.Text = sender + " in " + c.channelTitle(inv.Channel)
		if i18n.Locale() != i18n.EnUS {
			v.Text = nsHuddles.T("Huddle in {channelName}", "channelName", c.channelTitle(inv.Channel)) // no "{sender} in {channel}" string: the place
		}
	}
	// the facepile: you (joining soon), the sender, the others in it
	if v.JoiningSoon && self != "" {
		v.Faces = append(v.Faces, inviteFace{User: self, Status: "joining_soon"})
	}
	if inv.Sender != self {
		v.Faces = append(v.Faces, inviteFace{User: inv.Sender})
	}
	for _, u := range active {
		if len(v.Faces) == 4 {
			break
		}
		v.Faces = append(v.Faces, inviteFace{User: u})
	}
	v.Overflow = max(0, len(active)+1-len(v.Faces))
	if v.JoiningSoon {
		v.Overflow = max(0, len(active)+2-len(v.Faces))
	}
	// HuddleInvite's sentence (the sidebar card)
	switch kind { // useHuddleInviteProps' strings, the names bold
	case "im":
		v.CardText = boldSpans("Join huddle with {senderName}", "senderName", sender)
	case "mpim":
		v.CardText = boldSpans("Join huddle with {allMemberNames}", "allMemberNames", joinNames(append([]string{sender}, mpimOthers...)))
		v.SenderAvatar = len(mpimOthers) + 1
	default:
		v.CardText = boldSpans("{senderName} is inviting you to a huddle in {channelName}", "senderName", sender, "channelName", c.channelTitle(inv.Channel))
	}
	return v
}

// inviteWindow runs the invite window's own Gio loop (like window.go's).
type inviteWindow struct {
	c   *Controller
	inv *incoming
	th  *theme.Theme

	mu       sync.Mutex
	w        *app.Window
	closed   bool // closed by the controller (answered or cleared)
	decline  widget.Clickable
	soon     widget.Clickable
	join     widget.Clickable
	lastNote string
}

// openInviteWindow shows inv's window.
func (c *Controller) openInviteWindow(inv *incoming) {
	if c.NoWindow {
		return
	}
	c.mu.Lock()
	if c.inv != inv || c.iwin != nil {
		c.mu.Unlock()
		return
	}
	w := &inviteWindow{c: c, inv: inv}
	c.iwin = w
	c.mu.Unlock()
	go w.run()
}

// InviteWindowOpen reports whether the invite window is showing.
func (c *Controller) InviteWindowOpen() bool {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.iwin != nil
}

func (w *inviteWindow) close() {
	w.mu.Lock()
	w.closed = true
	ww := w.w
	w.mu.Unlock()
	if ww != nil {
		go ww.Perform(system.ActionClose) // not from a frame (see window.close)
	}
}

func (w *inviteWindow) invalidate() {
	w.mu.Lock()
	ww := w.w
	w.mu.Unlock()
	if ww != nil {
		go ww.Invalidate() // this window's own loop, never the main window's
	}
}

func (w *inviteWindow) run() {
	ww := new(app.Window)
	ww.Option(app.Title(AppName+" - Huddle"), app.Size(unit.Dp(380), unit.Dp(280)), app.MinSize(unit.Dp(380), unit.Dp(280)))
	main := w.c.h.Theme()
	dark := main != nil && main.Dark
	th := theme.Bare(dark)
	sh := theme.NewShaperFor(dark)
	th.SetShapers(sh, sh)
	if main != nil {
		th.Images, th.EmojiStyle, th.Tokens = main.Images, main.EmojiStyle, main.Tokens
	}
	w.th = th
	w.mu.Lock()
	w.w = ww
	closed := w.closed
	w.mu.Unlock()
	if closed {
		ww.Perform(system.ActionClose)
	}
	tick := time.NewTicker(500 * time.Millisecond) // the toast's timeout, avatars
	done := make(chan struct{})
	go func() {
		for {
			select {
			case <-tick.C:
				ww.Invalidate()
			case <-done:
				return
			}
		}
	}()
	var ops op.Ops
	for {
		switch e := ww.Event().(type) {
		case app.DestroyEvent:
			tick.Stop()
			close(done)
			w.mu.Lock()
			answered := w.closed
			w.mu.Unlock()
			if !answered {
				w.c.inviteWindowClosed(w)
			}
			return
		case app.FrameEvent:
			gtx := app.NewContext(&ops, e)
			w.layout(gtx)
			e.Frame(gtx.Ops)
		}
	}
}

func (w *inviteWindow) layout(gtx C) {
	c := w.c
	if w.decline.Clicked(gtx) {
		c.post(c.DeclineInvite)
	}
	if w.soon.Clicked(gtx) {
		c.post(c.JoiningSoon)
	}
	if w.join.Clicked(gtx) {
		c.post(func() { c.JoinInvite(EntryInviteWindow) })
	}
	layoutInviteWindow(gtx, w.th, c, c.view(w.inv), &w.decline, &w.soon, &w.join)
}

// layoutInviteWindow draws the invite window's content in
// gtx.Constraints.Max.
func layoutInviteWindow(gtx C, th *theme.Theme, c *Controller, v inviteView, decline, soon, join *widget.Clickable) {
	sz := gtx.Constraints.Max
	k := invInkFor(th)
	theme.Fill(gtx, image.Rectangle{Max: sz}, k.bg)

	// the title bar
	tb := gtx.Dp(32)
	title := nsHuddles.T("Invite to huddle")
	ts := labelSize(gtx, th, 13, theme.Bold, title)
	is := gtx.Dp(18)
	tx := (sz.X - (is + gtx.Dp(4) + ts.X)) / 2
	at(gtx, image.Pt(tx, (tb-is)/2), image.Pt(is, is), func(gtx C) D { return icons.Get("headphones")(gtx, 18, k.text) })
	at(gtx, image.Pt(tx+is+gtx.Dp(4), (tb-ts.Y)/2), ts, func(gtx C) D { return th.Label(gtx, 13, theme.Bold, k.text, title) })

	// the footer buttons
	type btn struct {
		cl          *widget.Clickable
		icon, label string
		join        bool
	}
	btns := []btn{{decline, "close", nsHuddles.T("Decline"), false}}
	if !v.JoiningSoon {
		btns = append(btns, btn{soon, "hourglass", nsHuddles.T("Be there soon"), false})
	}
	btns = append(btns, btn{join, "headphones", nsHuddles.T("Join"), true})
	bd := gtx.Dp(48)
	gap := gtx.Dp(24)
	lh := labelSize(gtx, th, 13, theme.Regular, nsHuddles.T("Join")).Y
	widths := make([]int, len(btns))
	total := 0
	for i, b := range btns {
		widths[i] = max(bd, labelSize(gtx, th, 13, theme.Regular, b.label).X)
		total += widths[i]
	}
	total += gap * (len(btns) - 1)
	footH := bd + gtx.Dp(4) + lh + gtx.Dp(8) + gtx.Dp(4)
	fy := sz.Y - footH
	x := (sz.X - total) / 2
	for i, b := range btns {
		cw := widths[i]
		bx := x + (cw-bd)/2
		b := b
		at(gtx, image.Pt(bx, fy), image.Pt(bd, bd), func(gtx C) D {
			return button(gtx, b.cl, image.Pt(bd, bd), func(gtx C, hov bool) {
				r := image.Rectangle{Max: image.Pt(bd, bd)}
				switch {
				case b.join:
					bg := k.join
					if hov {
						bg = k.joinHov
					}
					theme.Circle(gtx, image.Pt(bd/2, bd/2), bd, k.joinRing)
					theme.Circle(gtx, image.Pt(bd/2, bd/2), bd-2*max(1, gtx.Dp(1)), bg)
				default:
					bg := k.btn
					if hov {
						bg = k.btnHov
					}
					theme.RRect(gtx, r, bd/2, bg)
				}
			}, func(gtx C) D {
				col := k.sec
				if b.join {
					col = k.joinInk
				}
				if v.Busy && !b.join {
					col.A /= 2
				}
				s := gtx.Dp(20)
				at(gtx, image.Pt((bd-s)/2, (bd-s)/2), image.Pt(s, s), func(gtx C) D {
					if b.icon == "hourglass" {
						return hourglass(gtx, 20, col)
					}
					return icons.Get(b.icon)(gtx, 20, col)
				})
				return D{Size: image.Pt(bd, bd)}
			})
		})
		ls := labelSize(gtx, th, 13, theme.Regular, b.label)
		at(gtx, image.Pt(x+(cw-ls.X)/2, fy+bd+gtx.Dp(4)), ls, func(gtx C) D {
			return th.Label(gtx, 13, theme.Regular, k.text, b.label)
		})
		x += cw + gap
	}

	// the body: the facepile and the text, centered between
	body := image.Rect(gtx.Dp(8), tb+gtx.Dp(8), sz.X-gtx.Dp(8), fy-gtx.Dp(8))
	n := max(1, len(v.Faces))
	fg := gtx.Dp(8)
	face := min((body.Dx()-fg*(n-1))/n, gtx.Dp(80))
	txt := v.Text
	tsz := labelSize(gtx, th, 18, theme.Bold, txt)
	tw := min(tsz.X, body.Dx())
	blockH := face + gtx.Dp(4) + tsz.Y
	y := body.Min.Y + (body.Dy()-blockH)/2
	fx := (sz.X - (face*n + fg*(n-1))) / 2
	for i, f := range v.Faces {
		f := f
		over := 0
		if i == len(v.Faces)-1 {
			over = v.Overflow
		}
		at(gtx, image.Pt(fx+i*(face+fg), y), image.Pt(face, face), func(gtx C) D {
			inviteAvatar(gtx, th, c, k, f, face, over)
			return D{Size: image.Pt(face, face)}
		})
	}
	ty := y + face + gtx.Dp(4)
	func() {
		defer clip.Rect(image.Rect(body.Min.X, ty, body.Max.X, ty+tsz.Y)).Push(gtx.Ops).Pop()
		at(gtx, image.Pt((sz.X-tw)/2, ty), image.Pt(max(tw, tsz.X), tsz.Y), func(gtx C) D {
			return th.Label(gtx, 18, theme.Bold, k.text, ellipsize(gtx, th, 18, theme.Bold, txt, body.Dx()))
		})
	}()

	// the toast
	if v.Note != "" {
		inviteToast(gtx, th, v.Note, v.NoteErr, image.Pt(sz.X/2, tb+gtx.Dp(4)))
	}
}

// inviteAvatar is HuddleInviteWindowAvatar.
func inviteAvatar(gtx C, th *theme.Theme, c *Controller, k invInk, f inviteFace, size, overflow int) {
	rad := gtx.Dp(12)
	func() {
		if f.Status != "" { // ..._container--pending: opacity .5
			defer paint.PushOpacity(gtx.Ops, 0.5).Pop()
		}
		avatar(gtx, th, c.h.Images(), c.avatarURL(f.User, size), image.Pt(size, size), rad)
	}()
	if f.Status != "" { // __status_overlay: base-inv-pry, 2px padding, top left
		icon := "paper-plane"
		if f.Status == "joining_soon" {
			icon = "clock"
		}
		s := gtx.Dp(20)
		p := gtx.Dp(2)
		theme.RRect(gtx, image.Rect(0, 0, s+2*p, s+2*p), gtx.Dp(2), k.bg)
		at(gtx, image.Pt(p, p), image.Pt(s, s), func(gtx C) D { return icons.GetSlack(icon)(gtx, 20, k.text) })
	}
	if overflow > 0 { // __additional_overlay: "+N" at the bottom right
		txt := "+" + itoa(overflow)
		ls := labelSize(gtx, th, 13, theme.Bold, txt)
		h := gtx.Dp(22)
		w := ls.X + gtx.Dp(8)
		r := image.Rect(size-w, size-h, size, size)
		theme.RRect(gtx, r, gtx.Dp(2), k.bg)
		at(gtx, image.Pt(r.Min.X+gtx.Dp(4), r.Min.Y+(h-ls.Y)/2), ls, func(gtx C) D {
			return th.Label(gtx, 13, theme.Bold, k.text, txt)
		})
	}
}

// inviteToast is the top toast (c-toast: Slack's dark pill).
func inviteToast(gtx C, th *theme.Theme, note string, isErr bool, top image.Point) {
	icon := "clock"
	if isErr {
		icon = "warning"
	}
	ls := labelSize(gtx, th, 13, theme.Bold, note)
	is := gtx.Dp(20)
	pad := gtx.Dp(12)
	h := max(ls.Y, is) + gtx.Dp(16)
	w := pad + is + gtx.Dp(4) + ls.X + pad
	r := image.Rect(top.X-w/2, top.Y, top.X+w/2, top.Y+h)
	theme.RRect(gtx, r, gtx.Dp(8), color.NRGBA{R: 0x1d, G: 0x1c, B: 0x1d, A: 0xf2})
	at(gtx, image.Pt(r.Min.X+pad, r.Min.Y+(h-is)/2), image.Pt(is, is), func(gtx C) D {
		return icons.GetSlack(icon)(gtx, 20, hudWhite)
	})
	at(gtx, image.Pt(r.Min.X+pad+is+gtx.Dp(4), r.Min.Y+(h-ls.Y)/2), ls, func(gtx C) D {
		return th.Label(gtx, 13, theme.Bold, hudWhite, note)
	})
}

// ellipsize cuts txt to fit w px with "…".
func ellipsize(gtx C, th *theme.Theme, size unit.Sp, wt font.Weight, txt string, w int) string {
	if labelSize(gtx, th, size, wt, txt).X <= w {
		return txt
	}
	r := []rune(txt)
	for len(r) > 1 {
		r = r[:len(r)-1]
		s := strings.TrimSpace(string(r)) + "…"
		if labelSize(gtx, th, size, wt, s).X <= w {
			return s
		}
	}
	return "…"
}

// hourglass is Slack's hourglass glyph (the Be there soon button), drawn
// here on the icon set's 20×20 grid and 1.5px stroke.
func hourglass(gtx C, size unit.Dp, col color.NRGBA) D {
	px := float32(gtx.Dp(size))
	u := px / 20
	pt := func(x, y float32) f32.Point { return f32.Pt(x*u, y*u) }
	var p clip.Path
	p.Begin(gtx.Ops)
	p.MoveTo(pt(4.75, 2.75))
	p.LineTo(pt(15.25, 2.75))
	p.MoveTo(pt(4.75, 17.25))
	p.LineTo(pt(15.25, 17.25))
	p.MoveTo(pt(6.25, 2.75))
	p.LineTo(pt(6.25, 5.5))
	p.CubeTo(pt(6.25, 7.6), pt(9.25, 8.9), pt(9.25, 10))
	p.CubeTo(pt(9.25, 11.1), pt(6.25, 12.4), pt(6.25, 14.5))
	p.LineTo(pt(6.25, 17.25))
	p.MoveTo(pt(13.75, 2.75))
	p.LineTo(pt(13.75, 5.5))
	p.CubeTo(pt(13.75, 7.6), pt(10.75, 8.9), pt(10.75, 10))
	p.CubeTo(pt(10.75, 11.1), pt(13.75, 12.4), pt(13.75, 14.5))
	p.LineTo(pt(13.75, 17.25))
	paint.FillShape(gtx.Ops, col, clip.Stroke{Path: p.End(), Width: 1.5 * u}.Op())
	// the sand
	var q clip.Path
	q.Begin(gtx.Ops)
	q.MoveTo(pt(8, 15.75))
	q.LineTo(pt(12, 15.75))
	q.LineTo(pt(10, 13.5))
	q.Close()
	paint.FillShape(gtx.Ops, col, clip.Outline{Path: q.End()}.Op())
	return D{Size: image.Pt(int(px), int(px))}
}
