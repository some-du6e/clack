package huddleui

import (
	"image"
	"image/color"
	"math"
	"time"

	"gioui.org/f32"
	"gioui.org/layout"
	"gioui.org/op"
	"gioui.org/op/clip"
	"gioui.org/op/paint"
	"gioui.org/unit"

	"github.com/some-du6e/clack/internal/huddle"
	"github.com/some-du6e/clack/internal/ui/icons"
	"github.com/some-du6e/clack/internal/ui/theme"
)

// A participant tile, as Slack's HuddlePeerTile draws one in the huddle
// window (gantry-v2-async-client-boot-shared, module pluj, and
// client-boot-styles' .p-huddle_peer_tile rules, bv1-13):
//
//   - the tile: 12px corners, a 1px #1d1c1d21 ring; the camera (cover) or
//     else the avatar filling it.
//   - speaking (the active speaker): .p-huddle_peer_tile__active_speaker,
//     a 3px white outline and a white glow (box-shadow 0 0 8px 5px #fff).
//     No coloured border.
//   - the mic pill (.p-huddle_peer_tile__mic_overlay, always shown while
//     muted or speaking): 28px tall, fully rounded, 8px from the bottom
//     left, padding 4px 8px around HuddleMicIcon (20px). Muted: the
//     struck-through mic in white on base-modal (#1d1c1db3). Speaking:
//     white with the active_speaker GIF (four green bars bouncing).
//   - the name pill (.p-huddle_peer_tile__name_overlay) only on hover, in
//     the same place: the mic icon when muted or speaking, then the name
//     (13px bold) on base-inv-pry (#1d1c1d), or white while speaking. Your
//     own tile shows your name too (no "(you)").
//   - a reaction: HuddlePopoverReaction, a white pill at the top left (8px
//     in, 32px tall, padding 4px 10px, 20px emoji) that springs in.
//   - a sticker (raise hand, Be right back…): the same white pill, kept
//     while it's on, with the sticker's label (13px bold) when the tile is
//     at least 100px; on a small tile a 32×28 pill with only the emoji.
//     Audio-only is the label alone, white on base-inv. Yours is a button
//     ("Click to remove sticker").
//   - not connected: Slack's white spinner over a 70% black dim.
//   - rung but not here (invite status): the avatar at 50% opacity and a
//     white pill at the top left with paper-plane ("Invited"), clock
//     ("Joining soon") or warning ("Invite Declined"); the label shows on
//     hover.

const (
	tileRadius = 12 // --canvas_peer_tile_border_radius
	pillH      = 28
)

// overlay colours by theme (rollup-style-plastic's tokens)
type tileInk struct {
	modal  color.NRGBA // --dt_color-base-modal
	invPry color.NRGBA // --dt_color-base-inv-pry
	invInk color.NRGBA // --dt_color-content-inv-pry
}

var tileInks = [2]tileInk{
	{modal: color.NRGBA{R: 0x1d, G: 0x1c, B: 0x1d, A: 0xb3}, invPry: color.NRGBA{R: 0x1d, G: 0x1c, B: 0x1d, A: 0xff}, invInk: hudWhite},
	{modal: color.NRGBA{R: 0x10, G: 0x12, B: 0x14, A: 0x99}, invPry: color.NRGBA{R: 0x21, G: 0x24, B: 0x28, A: 0xff}, invInk: color.NRGBA{R: 0xf8, G: 0xf8, B: 0xf8, A: 0xff}},
}

var (
	tileRing     = color.NRGBA{R: 0x1d, G: 0x1c, B: 0x1d, A: 0x21} // box-shadow 0 0 0 1px
	speakerInk   = color.NRGBA{R: 0x1d, G: 0x1c, B: 0x1d, A: 0xff} // --dt_color-brand-sec-text
	speakerGreen = color.NRGBA{R: 0x41, G: 0xb6, B: 0x58, A: 0xff} // active_speaker-46c9bb2.gif
	inviteInk    = color.NRGBA{R: 0x1d, G: 0x1c, B: 0x1d, A: 0xb3} // .p-huddle_peer_tile__invite_status_overlay
)

func (w *window) ink() tileInk {
	if th := w.c.h.Theme(); th != nil && th.Dark {
		return tileInks[1]
	}
	return tileInks[0]
}

func (w *window) tile(gtx C, s *huddle.Session, p huddle.Participant, cam string, r image.Rectangle, now time.Time) {
	w.tileR(gtx, s, p, cam, r, now, gtx.Dp(tileRadius))
}

// tileR is tile with rad corners (the compact audio pane's are 4px).
func (w *window) tileR(gtx C, s *huddle.Session, p huddle.Participant, cam string, r image.Rectangle, now time.Time, rad int) {
	sz := r.Size()
	speaking := p.Speaking(now)
	if speaking {
		activeSpeaker(gtx, r, rad)
	}
	key := "tile:" + p.UserID + "/" + p.AttendeeID
	at(gtx, r.Min, sz, func(gtx C) D {
		return button(gtx, w.click(key), sz, func(gtx C, hov bool) {
			tileEdge(gtx, sz, rad)
			theme.RRect(gtx, image.Rectangle{Max: sz}, rad, hudTileBg)
			drawn := false
			if cam != "" && s != nil {
				if img, ok := w.vids.get(s, cam); ok {
					theme.Image(gtx, img, sz, rad)
					drawn = true
				}
			}
			if !drawn {
				avatar(gtx, w.th, w.c.h.Images(), w.c.avatarURL(p.UserID, sz.X), sz, rad)
			}
			if !p.InMedia && !p.Self || p.Self && w.phase != huddle.Connected {
				// VideoSpinnerContainer: dimmed while not connected
				theme.RRect(gtx, image.Rectangle{Max: sz}, rad, color.NRGBA{A: 0xb3})
				d := gtx.Dp(32)
				at(gtx, image.Pt((sz.X-d)/2, (sz.Y-d)/2), image.Pt(d, d), func(gtx C) D { spinner(gtx, d, hudWhite); return D{} })
			}
			ink := w.ink()
			if p.Muted || speaking {
				w.micPill(gtx, sz, p.Muted, speaking, ink, now)
			}
			if hov {
				w.namePill(gtx, sz, w.c.userName(p.UserID), p.Muted, speaking, ink, now)
			}
			if p.Sticker != "" { // a sticker wins over a passing reaction (ta || te)
				w.stickerPill(gtx, p, sz)
			} else if p.Reaction != "" {
				if age := now.Sub(p.ReactedAt); age < 4*time.Second {
					w.reactionPill(gtx, p.Reaction, sz, age)
				}
			}
		}, nil)
	})
}

// activeSpeaker is .p-huddle_peer_tile__active_speaker around r: a 3px
// white outline outside the tile's edge and a white glow beyond it
// (box-shadow 0 0 8px 5px #fff: solid to 1px, half at 5px, gone by 9px).
func activeSpeaker(gtx C, r image.Rectangle, rad int) {
	for d := 9; d > 3; d-- {
		a := float32(9-d) / 8
		px := gtx.Dp(unit.Dp(float32(d)))
		theme.Border(gtx, r.Inset(-px), rad+px, max(1, gtx.Dp(1)), color.NRGBA{R: 0xff, G: 0xff, B: 0xff, A: uint8(a * 255)})
	}
	o := gtx.Dp(3)
	theme.Border(gtx, r.Inset(-o/2), rad+o/2, o, hudWhite)
}

// micPill is .p-huddle_peer_tile__mic_overlay.
func (w *window) micPill(gtx C, sz image.Point, muted, speaking bool, ink tileInk, now time.Time) {
	h := gtx.Dp(pillH)
	pw := gtx.Dp(8) + gtx.Dp(20) + gtx.Dp(8)
	r := image.Rect(gtx.Dp(8), sz.Y-gtx.Dp(8)-h, gtx.Dp(8)+pw, sz.Y-gtx.Dp(8))
	bg := ink.modal
	if speaking {
		bg = hudWhite
	}
	pillShadow(gtx, r)
	theme.RRect(gtx, r, h/2, bg)
	at(gtx, image.Pt(r.Min.X+gtx.Dp(8), r.Min.Y+gtx.Dp(4)), image.Pt(gtx.Dp(20), gtx.Dp(20)), func(gtx C) D {
		micIcon(gtx, muted, speaking, ink.invInk, now)
		return D{}
	})
}

// namePill is .p-huddle_peer_tile__name_overlay (hover).
func (w *window) namePill(gtx C, sz image.Point, name string, muted, speaking bool, ink tileInk, now time.Time) {
	h := gtx.Dp(pillH)
	icon := muted || speaking
	x := gtx.Dp(8)
	if icon {
		x += gtx.Dp(20) + gtx.Dp(4)
	}
	maxW := sz.X - gtx.Dp(8) - gtx.Dp(12) // left 8, margin-right 12
	ls := labelSize(gtx, w.th, 13, theme.Bold, name)
	pw := min(x+ls.X+gtx.Dp(8), maxW)
	if pw <= 0 {
		return
	}
	r := image.Rect(gtx.Dp(8), sz.Y-gtx.Dp(8)-h, gtx.Dp(8)+pw, sz.Y-gtx.Dp(8))
	bg, fg := ink.invPry, ink.invInk
	if speaking {
		bg, fg = hudWhite, speakerInk
	}
	pillShadow(gtx, r)
	theme.RRect(gtx, r, h/2, bg)
	if icon {
		at(gtx, image.Pt(r.Min.X+gtx.Dp(8), r.Min.Y+gtx.Dp(4)), image.Pt(gtx.Dp(20), gtx.Dp(20)), func(gtx C) D {
			micIcon(gtx, muted, speaking, fg, now)
			return D{}
		})
	}
	tw := max(0, r.Dx()-x-gtx.Dp(8))
	defer clip.Rect(image.Rect(r.Min.X+x, r.Min.Y, r.Min.X+x+tw, r.Max.Y)).Push(gtx.Ops).Pop()
	at(gtx, image.Pt(r.Min.X+x, r.Min.Y+(h-ls.Y)/2), image.Pt(max(tw, ls.X), ls.Y), func(gtx C) D {
		return w.th.Label(gtx, 13, theme.Bold, fg, name)
	})
}

// tileEdge is .p-huddle_peer_tile's box-shadow 0 0 0 1px #1d1c1d21: a
// 1px ring just outside the tile.
func tileEdge(gtx C, sz image.Point, rad int) {
	o := max(1, gtx.Dp(1))
	theme.Border(gtx, image.Rectangle{Max: sz}.Inset(-o/2-o%2), rad+o/2, o, tileRing)
}

// pillShadow is the overlays' box-shadow: a 1px #1d1c1d21 ring.
func pillShadow(gtx C, r image.Rectangle) {
	o := max(1, gtx.Dp(1))
	theme.RRect(gtx, r.Inset(-o), (r.Dy()+2*o)/2, tileRing)
}

// micIcon is HuddleMicIcon at 20px: the struck-through mic when muted,
// else (the active speaker, allowUnmutedAnimation) the speaking bars.
func micIcon(gtx C, muted, speaking bool, col color.NRGBA, now time.Time) {
	if muted || !speaking {
		icons.Get("mic-off")(gtx, 20, col)
		return
	}
	speakingBars(gtx, now)
}

// speakerFrames are active_speaker-46c9bb2.gif's four bar heights per
// frame (on its 60px grid; the GIF shows at 20px), 40-50ms a frame.
var speakerFrames = [...][4]uint8{
	{17, 11, 13, 14}, {15, 10, 12, 13}, {11, 9, 11, 13}, {11, 9, 11, 14}, {17, 26, 33, 33}, {23, 40, 52, 49}, {21, 36, 47, 41}, {16, 28, 37, 25}, {12, 22, 30, 15}, {8, 18, 25, 11},
	{7, 14, 20, 10}, {7, 11, 15, 9}, {7, 9, 11, 9}, {11, 10, 12, 8}, {15, 11, 14, 8}, {15, 11, 15, 7}, {15, 10, 15, 7}, {12, 9, 14, 7}, {10, 8, 14, 7}, {11, 8, 15, 8},
	{11, 9, 15, 9}, {11, 9, 15, 9}, {9, 7, 14, 9}, {9, 7, 14, 9}, {12, 12, 18, 19}, {17, 17, 25, 33}, {19, 25, 40, 41}, {19, 27, 47, 45}, {16, 24, 40, 37}, {11, 18, 26, 23},
	{7, 13, 13, 10}, {7, 11, 11, 9}, {8, 10, 10, 8}, {8, 9, 9, 8}, {9, 9, 10, 10}, {12, 8, 12, 12}, {14, 9, 13, 15}, {17, 9, 15, 17}, {13, 8, 13, 12}, {9, 7, 11, 8},
	{9, 9, 14, 13}, {9, 10, 19, 18}, {9, 11, 22, 23}, {10, 12, 18, 20}, {11, 12, 13, 17}, {11, 13, 9, 15}, {11, 20, 21, 23}, {11, 29, 36, 34}, {11, 35, 47, 43}, {11, 29, 38, 33},
	{10, 20, 26, 19}, {9, 14, 17, 9}, {9, 12, 14, 9}, {8, 9, 11, 8}, {7, 8, 9, 7}, {7, 7, 8, 7}, {9, 8, 11, 8}, {13, 9, 16, 10}, {17, 9, 20, 11}, {14, 9, 17, 12},
	{11, 8, 14, 13}, {9, 7, 11, 14}, {10, 11, 13, 16}, {11, 14, 15, 18}, {11, 15, 17, 20}, {11, 15, 11, 9}, {11, 16, 17, 17}, {11, 17, 24, 26}, {10, 18, 30, 34}, {14, 22, 41, 30},
	{17, 25, 47, 27}, {15, 24, 39, 20}, {11, 22, 25, 8}, {10, 26, 18, 8}, {9, 29, 11, 7}, {10, 21, 10, 8}, {11, 13, 8, 9}, {13, 15, 11, 10}, {15, 18, 13, 11}, {17, 20, 16, 11},
	{12, 15, 13, 10}, {7, 9, 10, 8}, {9, 11, 10, 9}, {11, 13, 11, 9}, {11, 14, 12, 10}, {11, 14, 13, 9}, {12, 19, 13, 11}, {13, 26, 14, 14}, {14, 31, 15, 17}, {13, 38, 33, 29},
	{13, 45, 51, 42}, {11, 35, 40, 32}, {9, 22, 26, 19}, {7, 13, 16, 9}, {8, 9, 13, 11}, {8, 10, 11, 11}, {7, 11, 9, 11}, {9, 10, 9, 11}, {11, 9, 9, 11}, {13, 8, 9, 11},
	{15, 7, 8, 11}, {13, 8, 8, 12}, {11, 8, 8, 14}, {9, 8, 7, 15}, {14, 10, 11, 14}, {17, 11, 13, 14},
}

// speakingBars draws the active_speaker GIF in a 20px box: four 2px
// rounded green bars, 4.33px apart, centred, bouncing (~42ms a frame).
func speakingBars(gtx C, now time.Time) D {
	const frameMs = 42
	f := speakerFrames[(now.UnixMilli()/frameMs)%int64(len(speakerFrames))]
	box := float32(gtx.Dp(20))
	u := box / 60 // the GIF's grid
	bw := 6 * u
	for i, h := range f {
		x := (7 + 13*float32(i)) * u
		hh := float32(h) * u
		y := 29.5*u - hh/2
		rr := image.Rect(int(x), int(y), int(x+bw+.5), int(y+hh+.5))
		theme.RRect(gtx, rr, rr.Dx()/2, speakerGreen)
	}
	gtx.Execute(op.InvalidateCmd{At: now.Add(frameMs * time.Millisecond)})
	return D{Size: image.Pt(gtx.Dp(20), gtx.Dp(20))}
}

// reactionPill is HuddlePopoverReaction in the tile's activity icons
// container: a white pill at the top left that springs in (scale 0→1).
func (w *window) reactionPill(gtx C, name string, sz image.Point, age time.Duration) {
	h := gtx.Dp(32)
	es := gtx.Dp(20)
	pw := gtx.Dp(10) + es + gtx.Dp(10)
	r := image.Rect(gtx.Dp(8), gtx.Dp(8), gtx.Dp(8)+pw, gtx.Dp(8)+h)
	if r.Max.X > sz.X-gtx.Dp(8) {
		return
	}
	// spring (stiffness 150, damping 8): a quick overshoot, settled by ~0.6 s
	t := float64(age) / float64(time.Second)
	sc := float32(1 - math.Exp(-8*t)*math.Cos(12*t))
	alpha := float32(min(1, t/0.2))
	if age > 3800*time.Millisecond { // exit: opacity and scale to 0
		k := float32(4*time.Second-age) / float32(200*time.Millisecond)
		sc, alpha = sc*k, alpha*k
	}
	c := layout.FPt(r.Min.Add(r.Max)).Div(2)
	defer op.Affine(f32.Affine2D{}.Scale(c, f32.Pt(sc, sc))).Push(gtx.Ops).Pop()
	defer paint.PushOpacity(gtx.Ops, alpha).Pop()
	pillShadow(gtx, r)
	bg, _ := w.pillInk()
	theme.RRect(gtx, r, h/2, bg)
	txt := w.emojiText(name)
	at(gtx, image.Pt(r.Min.X+gtx.Dp(10), r.Min.Y+(h-es)/2), image.Pt(es, es), func(gtx C) D {
		emojiAt(gtx, w.th, image.Pt(es, es), es, txt)
		return D{}
	})
	gtx.Execute(op.InvalidateCmd{At: gtx.Now.Add(33 * time.Millisecond)})
}

// stickerPill is HuddlePeerTile's p-huddle_peer_tile__sticky_emoji_container.
func (w *window) stickerPill(gtx C, p huddle.Participant, sz image.Point) {
	small := sz.X <= gtx.Dp(100)
	label := ""
	if !small {
		label = w.c.stickerLabel(p.Sticker)
	}
	audioOnly := stickerBase(p.Sticker) == "speaker" && !small
	es := gtx.Dp(20)
	h := gtx.Dp(32)
	x0 := gtx.Dp(8)
	var r image.Rectangle
	if small {
		h = gtx.Dp(28)
		x0 = gtx.Dp(4)
		r = image.Rect(x0, x0, x0+gtx.Dp(32), x0+h)
	} else {
		pw := gtx.Dp(10)*2 + es
		lw := 0
		if label != "" {
			lw = labelSize(gtx, w.th, 13, theme.Bold, label).X + gtx.Dp(8)
			pw += lw
		}
		if audioOnly {
			pw -= es + gtx.Dp(4) // the icon hides, the label loses its left padding
		}
		pw = min(pw, sz.X-gtx.Dp(24))
		r = image.Rect(x0, x0, x0+pw, x0+h)
	}
	if r.Dx() <= 0 {
		return
	}
	bg, fg := w.pillInk()
	if audioOnly { // p-huddle_tile_reaction__audio_only_mode--huddle_window: sk_inverted_*
		bg, fg = hudInk, hudWhite
	}
	draw := func(gtx C, hov bool) {
		rr := image.Rectangle{Max: r.Size()}
		pillShadow(gtx, rr)
		theme.RRect(gtx, rr, rr.Dy()/2, bg)
		x := gtx.Dp(10)
		if small {
			x = (rr.Dx() - es) / 2
		}
		if !audioOnly {
			txt := w.emojiText(p.Sticker)
			at(gtx, image.Pt(x, (rr.Dy()-es)/2), image.Pt(es, es), func(gtx C) D {
				emojiAt(gtx, w.th, image.Pt(es, es), es, txt)
				return D{}
			})
			x += es + gtx.Dp(4)
		}
		if label != "" {
			ls := labelSize(gtx, w.th, 13, theme.Bold, label)
			tw := max(0, rr.Dx()-x-gtx.Dp(10))
			defer clip.Rect(image.Rect(x, 0, x+tw, rr.Dy())).Push(gtx.Ops).Pop()
			// .p-huddle_tile_reaction__description: text-overflow ellipsis
			at(gtx, image.Pt(x, (rr.Dy()-ls.Y)/2), image.Pt(tw, ls.Y), func(gtx C) D { return w.th.Label(gtx, 13, theme.Bold, fg, label) })
		}
	}
	at(gtx, r.Min, r.Size(), func(gtx C) D {
		if p.Self { // "Click to remove sticker"
			cl := w.click("sticker:self")
			if cl.Clicked(gtx) {
				go func() { _ = w.c.ToggleHand() }()
			}
			return button(gtx, cl, r.Size(), draw, nil)
		}
		draw(gtx, false)
		return D{Size: r.Size()}
	})
}

// pillInk is the reaction and sticker pills' colours:
// .p-huddle_peer_tile__emoji_container / __sticky_emoji_container's
// background sk_primary_background and .p-huddle_tile_reaction__description's
// content-pry, so they follow the client's theme (dark in dark mode).
func (w *window) pillInk() (bg, fg color.NRGBA) {
	if th := w.c.h.Theme(); th != nil {
		return th.C.Bg, th.C.Fg
	}
	return hudWhite, hudInk
}

// inviteTile is a rung person who isn't in the huddle (yet).
func (w *window) inviteTile(gtx C, inv huddle.Invite, r image.Rectangle) {
	w.inviteTileR(gtx, inv, r, gtx.Dp(tileRadius))
}

// inviteTileR is inviteTile with rad corners.
func (w *window) inviteTileR(gtx C, inv huddle.Invite, r image.Rectangle, rad int) {
	sz := r.Size()
	key := "invite:" + inv.UserID
	icon, label := "paper-plane", nsHuddles.T("Invited")
	switch inv.Status {
	case "joining_soon":
		icon, label = "clock", nsHuddles.T("Joining soon")
	case "declined":
		icon, label = "warning", nsHuddles.T("Invite Declined")
	}
	at(gtx, r.Min, sz, func(gtx C) D {
		return button(gtx, w.click(key), sz, func(gtx C, hov bool) {
			tileEdge(gtx, sz, rad)
			func() { // .p-huddle_peer_tile__pending_user: opacity .5
				defer paint.PushOpacity(gtx.Ops, 0.5).Pop()
				theme.RRect(gtx, image.Rectangle{Max: sz}, rad, hudTileBg)
				avatar(gtx, w.th, w.c.h.Images(), w.c.avatarURL(inv.UserID, sz.X), sz, rad)
			}()
			h := gtx.Dp(26) // 18px icon, padding 4px
			pw := gtx.Dp(8) + gtx.Dp(18) + gtx.Dp(8)
			var ls image.Point
			if hov {
				ls = labelSize(gtx, w.th, 13, theme.Bold, label)
				pw += gtx.Dp(4) + ls.X
			}
			pr := image.Rect(gtx.Dp(8), gtx.Dp(8), gtx.Dp(8)+min(pw, sz.X-gtx.Dp(20)), gtx.Dp(8)+h)
			pillShadow(gtx, pr)
			theme.RRect(gtx, pr, h/2, hudWhite)
			at(gtx, image.Pt(pr.Min.X+gtx.Dp(8), pr.Min.Y+gtx.Dp(4)), image.Pt(gtx.Dp(18), gtx.Dp(18)), func(gtx C) D {
				return icons.GetSlack(icon)(gtx, 18, inviteInk)
			})
			if hov {
				x := pr.Min.X + gtx.Dp(8) + gtx.Dp(18) + gtx.Dp(4)
				tc := clip.Rect(image.Rect(x, pr.Min.Y, pr.Max.X-gtx.Dp(8), pr.Max.Y)).Push(gtx.Ops)
				at(gtx, image.Pt(x, pr.Min.Y+(h-ls.Y)/2), ls, func(gtx C) D {
					return w.th.Label(gtx, 13, theme.Bold, inviteInk, label)
				})
				tc.Pop()
			}
		}, nil)
	})
}

// spinner is Slack's white loading ring (a 3/4 arc turning once a second)
// in a d×d box.
func spinner(gtx C, d int, c color.NRGBA) {
	t := float64(gtx.Now.UnixMilli()%1000) / 1000
	r := float32(d)/2 - float32(gtx.Dp(2))
	ctr := f32.Pt(float32(d)/2, float32(d)/2)
	a0 := float32(t * 2 * math.Pi)
	var p clip.Path
	p.Begin(gtx.Ops)
	p.MoveTo(f32.Pt(ctr.X+r*float32(math.Cos(float64(a0))), ctr.Y+r*float32(math.Sin(float64(a0)))))
	p.ArcTo(ctr, ctr, 1.5*math.Pi)
	st := clip.Stroke{Path: p.End(), Width: float32(gtx.Dp(3))}.Op().Push(gtx.Ops)
	paint.ColorOp{Color: c}.Add(gtx.Ops)
	paint.PaintOp{}.Add(gtx.Ops)
	st.Pop()
	gtx.Execute(op.InvalidateCmd{At: gtx.Now.Add(33 * time.Millisecond)})
}
