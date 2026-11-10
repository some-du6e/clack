package huddleui

import (
	"image"
	"image/color"
	"sync"

	"gioui.org/app"
	"gioui.org/io/system"
	"gioui.org/widget"

	"github.com/some-du6e/clack/internal/huddle"
	"github.com/some-du6e/clack/internal/ui/theme"
)

// A shared screen in its own window (huddles_screenshare_popout),
// ported from gantry-v2-async-client-boot-shared PWaJ
// (HuddleToolbarScreensharePopoutButtonBase, openHuddlePrimaryPopoutWindowMva)
// and the client-huddle-screenshare-popout-view chunk (1u5c,
// HuddleScreensharePopoutView), build 4d0c78085232:
//
//   - with the experiment on, the share's top toolbar has "Open in new
//     window" (open-in-window); it opens the popout for that person's
//     share and the huddle window goes back to the people (the self
//     content view "none").
//   - the popout: a top toolbar (the presenter's avatar and name, "Screen
//     share"; "Go full screen" / "Exit full screen", "Pop back into huddle
//     window" (move-to-main-window), "Close screen share window"), the
//     share, and the huddle toolbar at the bottom (HuddleToolbar
//     isPopout). "No one is sharing their screen" when there's nothing.
//   - it closes itself when that share stops or another share replaces
//     it, when you leave or switch huddles; Pop back puts the share back
//     on the huddle window's stage and closes it.
//
// Clack doesn't draw Slack's zoom control or the drawing toggle (it has
// neither); Close puts the share back on the stage too, since Clack's
// gallery has no screen share tiles to view it from again.

type popoutState struct {
	mu   sync.Mutex
	wins map[string]*window // by video key
}

// popoutOn reports whether a share may pop out.
func (c *Controller) popoutOn() bool { return c.on(ExpScreensharePopout) }

// OpenPopout opens the popout for the share with video key (or brings
// it forward). Any goroutine.
func (c *Controller) OpenPopout(key, user string) {
	if c.NoWindow || !c.popoutOn() {
		return
	}
	p := &c.popout
	p.mu.Lock()
	if p.wins == nil {
		p.wins = map[string]*window{}
	}
	if w := p.wins[key]; w != nil {
		p.mu.Unlock()
		w.raise()
		return
	}
	w := &window{c: c, clicks: map[string]*widget.Clickable{}, popout: key, popoutUser: user}
	p.wins[key] = w
	p.mu.Unlock()
	c.changed()
	go w.run()
}

// popped reports whether the share with key is in its own window.
func (c *Controller) popped(key string) bool {
	p := &c.popout
	p.mu.Lock()
	defer p.mu.Unlock()
	return p.wins[key] != nil
}

// closePopout closes one (from its own buttons, or when its share went).
func (c *Controller) closePopout(key string) {
	p := &c.popout
	p.mu.Lock()
	w := p.wins[key]
	delete(p.wins, key)
	p.mu.Unlock()
	if w != nil {
		w.close()
	}
	c.changed()
}

// closePopouts closes them all (you left the huddle).
func (c *Controller) closePopouts() {
	p := &c.popout
	p.mu.Lock()
	ws := p.wins
	p.wins = nil
	p.mu.Unlock()
	for _, w := range ws {
		w.close()
	}
}

// popoutGone forgets w once its window is gone (the OS closed it).
func (c *Controller) popoutGone(w *window) {
	p := &c.popout
	p.mu.Lock()
	if p.wins[w.popout] == w {
		delete(p.wins, w.popout)
	}
	p.mu.Unlock()
	c.changed()
}

func (w *window) raise() {
	w.mu.Lock()
	ww := w.w
	w.mu.Unlock()
	if ww != nil {
		go ww.Perform(system.ActionRaise)
	}
}

// layoutPopout is HuddleScreensharePopoutView.
func (w *window) layoutPopout(gtx C, s *huddle.Session, st huddle.State) {
	sz := gtx.Constraints.Max
	var share *huddle.Video
	for i := range st.Videos {
		if st.Videos[i].Key == w.popout {
			share = &st.Videos[i]
		}
	}
	if share == nil { // the share stopped: screenshare_popout_auto_close
		w.c.closePopout(w.popout)
		centerLabel(gtx, w.th, sz, 15, theme.Regular, hudWhite, "No one is sharing their screen")
		return
	}
	if w.click("pop-full").Clicked(gtx) {
		w.full = !w.full
		if w.full {
			w.w.Option(app.Fullscreen.Option())
		} else {
			w.w.Option(app.Windowed.Option())
		}
	}
	if w.click("pop-back").Clicked(gtx) {
		w.c.closePopout(w.popout)
		w.c.openOrRaise() // the huddle window, with the share on its stage
		return
	}
	if w.click("pop-close").Clicked(gtx) {
		w.c.closePopout(w.popout)
		return
	}
	topH, barH := gtx.Dp(56), gtx.Dp(56)
	vr := image.Rect(0, topH, sz.X, max(topH, sz.Y-barH))
	if img, ok := w.vids.get(s, share.Key); ok && vr.Dx() > 0 && vr.Dy() > 0 {
		at(gtx, vr.Min, vr.Size(), func(gtx C) D { fit(gtx, img, vr.Size(), 0); return D{} })
	}
	// the top toolbar: the presenter, then the buttons at the right
	pad := gtx.Dp(12)
	av := gtx.Dp(32) // Avatar medium
	at(gtx, image.Pt(pad, (topH-av)/2), image.Pt(av, av), func(gtx C) D {
		avatar(gtx, w.th, w.c.h.Images(), w.c.avatarURL(w.popoutUser, av), image.Pt(av, av), gtx.Dp(8))
		return D{Size: image.Pt(av, av)}
	})
	name := w.c.displayName(w.popoutUser)
	ns := labelSize(gtx, w.th, 15, theme.Bold, name)
	ty := (topH - ns.Y) / 2
	at(gtx, image.Pt(pad+av+gtx.Dp(8), ty), ns, func(gtx C) D { return w.th.Label(gtx, 15, theme.Bold, hudWhite, name) })
	sub := "Screen share"
	ss := labelSize(gtx, w.th, 13, theme.Regular, sub)
	sx := pad + av + gtx.Dp(8) + ns.X + gtx.Dp(8)
	at(gtx, image.Pt(sx, (topH-ss.Y)/2), ss, func(gtx C) D {
		return w.th.Label(gtx, 13, theme.Regular, theme.WithAlpha(hudWhite, 0xb3), sub)
	})
	bs := gtx.Dp(36)
	full := "window-expand"
	fullTip := "Go full screen"
	if w.full {
		full, fullTip = "window-expand-filled", "Exit full screen"
	}
	btns := []struct{ id, icon, tip string }{
		{"pop-full", full, fullTip},
		{"pop-back", "move-to-main-window", "Pop back into huddle window"},
		{"pop-close", "close", "Close screen share window"},
	}
	x := sz.X - pad - len(btns)*(bs+gtx.Dp(4))
	tip, tipAt := "", image.Rectangle{}
	for _, b := range btns {
		b := b
		cl := w.click(b.id)
		bx := x
		if cl.Hovered() {
			tip, tipAt = b.tip, image.Rect(bx, (topH-bs)/2, bx+bs, (topH+bs)/2)
		}
		at(gtx, image.Pt(bx, (topH-bs)/2), image.Pt(bs, bs), func(gtx C) D {
			return button(gtx, cl, image.Pt(bs, bs), func(gtx C, hov bool) {
				if hov {
					theme.RRect(gtx, image.Rectangle{Max: image.Pt(bs, bs)}, gtx.Dp(8), hudBtnHover)
				}
			}, func(gtx C) D { iconAt(gtx, image.Pt(bs, bs), b.icon, 20, hudWhite); return D{} })
		})
		x += bs + gtx.Dp(4)
	}
	if tip != "" { // the top toolbar's tooltips sit under it
		ts := labelSize(gtx, w.th, 12, theme.Bold, tip)
		tp := gtx.Dp(8)
		r := image.Rect(0, 0, ts.X+2*tp, ts.Y+tp)
		r = r.Add(image.Pt(min((tipAt.Min.X+tipAt.Max.X-r.Dx())/2, sz.X-r.Dx()-gtx.Dp(4)), tipAt.Max.Y+gtx.Dp(6)))
		theme.RRect(gtx, r, gtx.Dp(6), color.NRGBA{R: 0x1d, G: 0x1c, B: 0x1d, A: 0xf2})
		at(gtx, image.Pt(r.Min.X+tp, r.Min.Y+tp/2), ts, func(gtx C) D { return w.th.Label(gtx, 12, theme.Bold, hudWhite, tip) })
	}
	// the huddle toolbar (isPopout)
	w.barTop = sz.Y - barH
	w.bottomBar(gtx, s, st, image.Rect(0, sz.Y-barH, sz.X, sz.Y))
	w.toolbarTip(gtx, image.Rect(0, sz.Y-barH, sz.X, sz.Y))
	w.layoutMore(gtx)
	w.layoutOverlays(gtx)
}

// popoutButton is HuddleToolbarScreensharePopoutButtonBase in the share's
// top toolbar (right controls): "Open in new window", open-in-window,
// only under huddles_screenshare_popout.
func (w *window) popoutButton(gtx C, share *huddle.Video, sr image.Rectangle) {
	if !w.c.popoutOn() || share == nil {
		return
	}
	cl := w.click("popout:" + share.Key)
	if cl.Clicked(gtx) {
		w.c.OpenPopout(share.Key, share.UserID)
	}
	bs := gtx.Dp(36)
	p := image.Pt(sr.Max.X-bs-gtx.Dp(8), sr.Min.Y+gtx.Dp(8))
	if cl.Hovered() {
		tip := "Open in new window"
		ts := labelSize(gtx, w.th, 12, theme.Bold, tip)
		tp := gtx.Dp(8)
		r := image.Rect(0, 0, ts.X+2*tp, ts.Y+tp)
		r = r.Add(image.Pt(min(p.X+(bs-r.Dx())/2, sr.Max.X-r.Dx()), p.Y+bs+gtx.Dp(6)))
		defer func() {
			theme.RRect(gtx, r, gtx.Dp(6), color.NRGBA{R: 0x1d, G: 0x1c, B: 0x1d, A: 0xf2})
			at(gtx, image.Pt(r.Min.X+tp, r.Min.Y+tp/2), ts, func(gtx C) D { return w.th.Label(gtx, 12, theme.Bold, hudWhite, tip) })
		}()
	}
	at(gtx, p, image.Pt(bs, bs), func(gtx C) D {
		return button(gtx, cl, image.Pt(bs, bs), func(gtx C, hov bool) {
			bg := hudBtn
			if hov {
				bg = hudBtnHover
			}
			theme.RRect(gtx, image.Rectangle{Max: image.Pt(bs, bs)}, gtx.Dp(8), bg)
		}, func(gtx C) D { iconAt(gtx, image.Pt(bs, bs), "open-in-window", 20, hudWhite); return D{} })
	})
}
