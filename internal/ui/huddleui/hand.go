package huddleui

import (
	"image"
	"image/color"
	"strconv"
	"strings"
	"time"

	"gioui.org/io/event"
	"gioui.org/io/key"
	"gioui.org/io/pointer"
	"gioui.org/op/clip"
	"gioui.org/unit"
	"gioui.org/widget"

	"github.com/some-du6e/clack/internal/huddle"
	"github.com/some-du6e/clack/internal/ui/popups"
	"github.com/some-du6e/clack/internal/ui/slackc"
	"github.com/some-du6e/clack/internal/ui/slackc/css"
	"github.com/some-du6e/clack/internal/ui/theme"
)

// Raise hand and the other stickers (bv1-13): the huddle toolbar's
// HuddleToolbarSegmentedStickerPickerButton (boot-shared zeVD). Its main
// half shows your sticker, or the raised hand when you have none, and
// toggles: "Raise hand" puts raised_hand (with your preferred_skin_tone,
// getSkinToneModifiedName: raised_hand::skin-tone-N) on your tile, and
// with a sticker on it reads "Lower hand" (or "Remove sticker") and takes
// it off. The caret half opens the sticker picker. A sticker is an
// awareness entry (huddle.Session.SetSticker); HuddlePeerTile draws it at
// the tile's top left with its label (eAbZ).
//
// Under the huddles_raised_hand_queue experiment the label reads "Raised
// hand", and a raised hand lowers itself after 5 s as the active speaker
// (useAutoLowerRaisedHand).

// stickers are useHuddleReactionsConfig's stickers (boot-shared eAbZ,
// build 2cd9270e786c), in its order, with their labels. The tile names
// any of them; the grid offers gridStickers.
var stickers = []struct{ Emoji, Label string }{
	{"raised_hand", "Raise hand"},
	{"coffee", "Be right back"},
	{"headphones", "DJ’ing"},
	{"speaker", "Audio-only"},
	{"+1", "Yes"},
	{"-1", "No"},
	{"white_check_mark", "Done"},
	{"iphone", "On mobile"},
}

// stickerBase is a sticker's emoji without its skin tone.
func stickerBase(e string) string {
	if i := strings.Index(e, "::"); i >= 0 {
		return e[:i]
	}
	return e
}

// isHand reports whether a sticker is a raised hand (any skin tone).
func isHand(e string) bool { return stickerBase(e) == "raised_hand" }

// stickerLabel is the tile's description of a sticker ("" for none).
func (c *Controller) stickerLabel(e string) string {
	b := stickerBase(e)
	if b == "raised_hand" && c.handQueue() {
		return nsHuddles.T("Raised hand") // activeStateLabelOverrides
	}
	for _, s := range stickers {
		if s.Emoji == b {
			if b == "headphones" && !c.on(ExpMusic) {
				return s.Label // eAbZ: t ? o.t("DJ’ing") : "DJ’ing"
			}
			return nsHuddles.T(s.Label) // the picker's labels are huddles strings
		}
	}
	return ""
}

func (c *Controller) handQueue() bool {
	app := c.h.Core()
	return app != nil && app.Experiment("huddles_raised_hand_queue") == "on"
}

// handEmoji is raised_hand in your skin tone.
func (c *Controller) handEmoji() string { return c.withSkinTone("raised_hand") }

// withSkinTone is getSkinToneModifiedName for the stickers that take one.
func (c *Controller) withSkinTone(e string) string {
	if e != "raised_hand" && e != "+1" && e != "-1" {
		return e
	}
	app := c.h.Core()
	if app == nil {
		return e
	}
	t := app.PrefString("preferred_skin_tone")
	if t == "" {
		if n := app.PrefInt("preferred_skin_tone", 0); n > 0 {
			t = strconv.Itoa(n)
		}
	}
	if n, err := strconv.Atoi(t); err == nil && n >= 2 && n <= 6 {
		return e + "::skin-tone-" + t
	}
	return e
}

// ToggleHand is the sticker button's main half: take your sticker off,
// or raise your hand.
func (c *Controller) ToggleHand() error {
	s := c.Session()
	if s == nil {
		return nil
	}
	if s.Sticker() != "" {
		return s.SetSticker("")
	}
	return s.SetSticker(c.handEmoji())
}

// PickSticker is a sticker picked in the picker (on, or off when it's
// the one you have on).
func (c *Controller) PickSticker(e string) error {
	s := c.Session()
	if s == nil {
		return nil
	}
	return s.ToggleSticker(c.withSkinTone(e))
}

// handTip is the main half's tooltip.
func handTip(sticker string) string {
	switch {
	case sticker == "":
		return nsHuddles.T("Raise hand")
	case isHand(sticker):
		return nsHuddles.T("Lower hand")
	}
	return nsHuddles.T("Remove sticker")
}

type handState struct {
	speakingSince time.Time
	lastSpoke     time.Time
}

// autoLowerHand is useAutoLowerRaisedHand: under the raised-hand queue,
// 5 s as the active speaker with your hand up lowers it. Runs on session
// changes.
func (c *Controller) autoLowerHand(s *huddle.Session, st huddle.State) {
	if !c.handQueue() || !isHand(s.Sticker()) {
		c.mu.Lock()
		c.hand = handState{}
		c.mu.Unlock()
		return
	}
	now := time.Now()
	speaking := false
	for _, p := range st.Participants {
		if p.Self {
			speaking = p.Speaking(now)
		}
	}
	c.mu.Lock()
	h := &c.hand
	if speaking {
		h.lastSpoke = now
		if h.speakingSince.IsZero() {
			h.speakingSince = now
		}
	} else if now.Sub(h.lastSpoke) > time.Second { // a pause hands the floor back
		h.speakingSince = time.Time{}
	}
	lower := !h.speakingSince.IsZero() && now.Sub(h.speakingSince) >= 5*time.Second
	if lower {
		c.hand = handState{}
	}
	c.mu.Unlock()
	if lower {
		_ = s.SetSticker("")
	}
}

// The caret half's popover: HuddleEmojiMenuTrigger with onlyShowStickers
// (boot-shared Vw0B, build 2cd9270e786c). It is HuddleQuickEmojiPicker's
// c-menu card (.p-huddle_quick_emoji_picker: 360px wide,
// sk_primary_background) without the tabs: the close button at the top
// right (.p-huddle_quick_emoji_picker__right_controls: top 2px, right 0)
// and HuddleStickerGrid:
//
//   - "Stickers stay on until you remove them." (.stickerGridSubtitle:
//     12px/1.5, content-ter, margin-left 16px, padding 12px 0 4px);
//   - the stickers two to a row (.stickerGrid: flex-wrap, margin-bottom
//     8px; .stickerButton: width 50%, padding 8px), each a 56px
//     ctr-sec tile with radius 8 (.stickerEmojiWrapper: 13px bold, the
//     interaction blend of ctr-sec-hover / -pressed on hover and press),
//     its 24px emoji (margin 0 8px 0 16px) and its label;
//   - the sticker you have on (modifiedName === stickyEmoji, skin tone
//     included) is .stickerEmojiWrapperSelected: content-inv-pry on
//     base-inv-hgl-2.
//
// The grid leaves out "iphone", "speaker" and "network", and "headphones"
// (DJ'ing) unless huddles_music is on (pSxm lV). A pick sends a sticker
// reaction (useSendHuddleReaction, reactionType "sticker") and closes the
// popover; the popover sits above the caret (position "top", offsetY -5).

const (
	sgW         = 360 // .p-huddle_quick_emoji_picker width
	sgCloseW    = 36  // c-icon_button--size_medium
	sgCloseTop  = 2   // __right_controls top
	sgSubPadTop = 12  // .stickerGridSubtitle padding-top
	sgSubLine   = 18  // 12px × 1.50001
	sgSubPadBot = 4   // its padding-bottom
	sgSubLeft   = 16  // its margin-left
	sgCellPad   = 8   // .stickerButton padding
	sgTileH     = 56  // .stickerEmojiWrapper height
	sgEmoji     = 24  // .stickerEmoji
	sgEmojiL    = 16  // its margin-left
	sgEmojiR    = 8   // its margin-right
	sgGridBot   = 8   // .stickerGrid margin-bottom
	sgOffsetY   = 5   // the popover's offsetY -5
)

// gridStickers are the stickers HuddleStickerGrid offers.
func (c *Controller) gridStickers() []struct{ Emoji, Label string } {
	music := c.on(ExpMusic)
	var out []struct{ Emoji, Label string }
	for _, s := range stickers {
		switch s.Emoji {
		case "iphone", "speaker", "network":
			continue
		case "headphones":
			if !music {
				continue
			}
		}
		out = append(out, s)
	}
	return out
}

// stickerGrid is the sticker caret's popover: an overlay over the window
// it opens in (the huddle window, or the main window for the docked bar).
type stickerGrid struct {
	c      *Controller
	h      popups.Host
	anchor image.Rectangle // the caret, window px
	picks  map[string]*widget.Clickable
	close  widget.Clickable
	scrim  int
	card   image.Rectangle // last frame's card, window px
	done   bool
	focus  bool // took the keyboard focus (Escape closes it)
}

func newStickerGrid(c *Controller, h popups.Host, anchor image.Rectangle) *stickerGrid {
	return &stickerGrid{c: c, h: h, anchor: anchor, picks: map[string]*widget.Clickable{}}
}

func (g *stickerGrid) Dismiss()        { g.done = true }
func (g *stickerGrid) Dismissed() bool { return g.done }

func (g *stickerGrid) pick(e string) *widget.Clickable {
	cl := g.picks[e]
	if cl == nil {
		cl = new(widget.Clickable)
		g.picks[e] = cl
	}
	return cl
}

// size is the card's size in px for n stickers.
func (g *stickerGrid) size(gtx C, n int) image.Point {
	rows := (n + 1) / 2
	h := sgSubPadTop + sgSubLine + sgSubPadBot + rows*(sgTileH+2*sgCellPad) + sgGridBot
	return image.Pt(gtx.Dp(sgW), gtx.Dp(unit.Dp(h)))
}

// place puts the card above the caret, centred on it and kept in the
// window; below it when there's no room above (Popover's flip).
func (g *stickerGrid) place(gtx C, sz, win image.Point) image.Point {
	m := gtx.Dp(8)
	x := (g.anchor.Min.X+g.anchor.Max.X)/2 - sz.X/2
	x = max(m, min(x, win.X-m-sz.X))
	y := g.anchor.Min.Y - gtx.Dp(sgOffsetY) - sz.Y
	if y < m {
		y = g.anchor.Max.Y + gtx.Dp(sgOffsetY)
		if y+sz.Y > win.Y-m {
			y = max(0, win.Y-m-sz.Y)
		}
	}
	return image.Pt(x, y)
}

var sgClass = slackc.Class("c-menu p-huddle_quick_emoji_picker")

// dtColor is a Slack design token (--dt_color-NAME) in th's mode.
func dtColor(th *theme.Theme, name string) color.NRGBA {
	c, _ := css.ParseColor(css.SlackVars(th.Dark)["--dt_color-"+name])
	return c
}

// Layout draws the popover over the whole window (an outside press or
// Escape closes it).
func (g *stickerGrid) Layout(gtx C) D {
	if g.done {
		return D{}
	}
	win := gtx.Constraints.Max
	gtx.Constraints.Min = image.Point{}
	th := g.h.Theme()
	cl := popups.ColorsOf(th)
	list := g.c.gridStickers()
	if g.close.Clicked(gtx) {
		g.Dismiss()
		return D{}
	}
	for _, s := range list {
		if g.pick(s.Emoji).Clicked(gtx) {
			g.Dismiss()
			e := s.Emoji
			go func() { _ = g.c.PickSticker(e) }()
			return D{}
		}
	}
	for {
		ev, ok := gtx.Event(pointer.Filter{Target: &g.scrim, Kinds: pointer.Press}, key.Filter{Focus: &g.scrim, Name: key.NameEscape})
		if !ok {
			break
		}
		switch ev := ev.(type) {
		case pointer.Event:
			if ev.Kind == pointer.Press && !g.card.Empty() && !image.Pt(int(ev.Position.X), int(ev.Position.Y)).In(g.card) {
				g.Dismiss()
				return D{}
			}
		case key.Event:
			if ev.State == key.Press {
				g.Dismiss()
				return D{}
			}
		}
	}
	sc := clip.Rect{Max: win}.Push(gtx.Ops)
	event.Op(gtx.Ops, &g.scrim)
	sc.Pop()
	if !g.focus {
		gtx.Execute(key.FocusCmd{Tag: &g.scrim})
		g.focus = true
	}

	sz := g.size(gtx, len(list))
	pos := g.place(gtx, sz, win)
	g.card = image.Rectangle{Min: pos, Max: pos.Add(sz)}
	st := sgClass.Enter(gtx, 0, 0)
	defer st.Leave()
	if st.Hidden() {
		return D{Size: win}
	}
	mine := ""
	if s := g.c.Session(); s != nil {
		mine = s.Sticker()
	}
	shadow := func(gtx C, r image.Rectangle, rad int) { theme.Shadow(gtx, r, rad, gtx.Dp(12), cl.Shadow) }
	popups.StyledCard(gtx, st, cl.EmojiBg, cl.Ring, shadow, pos, sz, gtx.Dp(6), func(gtx C) {
		defer clip.UniformRRect(image.Rectangle{Max: sz}, gtx.Dp(6)).Push(gtx.Ops).Pop()
		cc := clip.Rect{Max: sz}.Push(gtx.Ops) // the card takes its own presses
		event.Op(gtx.Ops, &g.card)
		cc.Pop()
		g.layoutGrid(gtx, th, cl, sz, list, mine)
	})
	return D{Size: win}
}

// layoutGrid is HuddleStickerGrid and the close button inside the card.
func (g *stickerGrid) layoutGrid(gtx C, th *theme.Theme, cl *popups.Colors, sz image.Point, list []struct{ Emoji, Label string }, mine string) {
	sub := nsHuddles.T("Stickers stay on until you remove them.")
	ts := theme.TextStyle{Size: 12, Weight: theme.Regular, LineHeight: sgSubLine, MaxLines: 1, Color: dtColor(th, "content-ter")}
	at(gtx, image.Pt(gtx.Dp(sgSubLeft), gtx.Dp(sgSubPadTop)), image.Pt(sz.X-gtx.Dp(sgSubLeft+sgCloseW), gtx.Dp(sgSubLine)), func(gtx C) D {
		return th.Layout(gtx, ts, sub)
	})
	bg := dtColor(th, "ctr-sec")
	selBg := dtColor(th, "base-inv-hgl-2")
	selFg := dtColor(th, "content-inv-pry")
	fg := dtColor(th, "content-pry")
	cellW := sz.X / 2
	top := gtx.Dp(sgSubPadTop + sgSubLine + sgSubPadBot)
	cellH := gtx.Dp(sgTileH + 2*sgCellPad)
	for i, s := range list {
		name := g.c.withSkinTone(s.Emoji) // getSkinToneModifiedName
		on := name == mine
		label := nsHuddles.T(s.Label)
		p := image.Pt((i%2)*cellW+gtx.Dp(sgCellPad), top+(i/2)*cellH+gtx.Dp(sgCellPad))
		tile := image.Pt(cellW-2*gtx.Dp(sgCellPad), gtx.Dp(sgTileH))
		pk := g.pick(s.Emoji)
		emoji := g.c.emojiText(name)
		at(gtx, p, tile, func(gtx C) D {
			return button(gtx, pk, tile, func(gtx C, hov bool) {
				b, over := bg, "ctr-sec"
				if on {
					b, over = selBg, "base-inv-hgl-2"
				}
				switch {
				case pk.Pressed():
					b = theme.BlendInteraction(b, dtColor(th, over+"-pressed"), th.Dark)
				case hov:
					b = theme.BlendInteraction(b, dtColor(th, over+"-hover"), th.Dark)
				}
				theme.RRect(gtx, image.Rectangle{Max: tile}, gtx.Dp(8), b)
			}, func(gtx C) D {
				es := gtx.Dp(sgEmoji)
				at(gtx, image.Pt(gtx.Dp(sgEmojiL), (tile.Y-es)/2), image.Pt(es, es), func(gtx C) D {
					return emojiAt(gtx, th, image.Pt(es, es), es, emoji)
				})
				col := fg
				if on {
					col = selFg
				}
				lx := gtx.Dp(sgEmojiL + sgEmoji + sgEmojiR)
				txt := ellipsize(gtx, th, 13, theme.Bold, label, max(0, tile.X-lx-gtx.Dp(8)))
				ls := labelSize(gtx, th, 13, theme.Bold, txt)
				at(gtx, image.Pt(lx, (tile.Y-ls.Y)/2), ls, func(gtx C) D { return th.Label(gtx, 13, theme.Bold, col, txt) })
				return D{Size: tile}
			})
		})
	}
	cw := gtx.Dp(sgCloseW)
	at(gtx, image.Pt(sz.X-cw, gtx.Dp(sgCloseTop)), image.Pt(cw, cw), func(gtx C) D {
		return button(gtx, &g.close, image.Pt(cw, cw), func(gtx C, hov bool) {
			if hov {
				theme.RRect(gtx, image.Rectangle{Max: image.Pt(cw, cw)}, gtx.Dp(8), cl.BtnHover)
			}
		}, func(gtx C) D {
			col := cl.Fg2
			if g.close.Hovered() {
				col = cl.Fg
			}
			iconAt(gtx, image.Pt(cw, cw), "close", 20, col)
			return D{}
		})
	})
}
