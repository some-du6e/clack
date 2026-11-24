package richview

// Block Kit buttons that write to Slack: a share_prompt's Send (and, on the
// cards that have them, an event's RSVP options and Copy to Your
// Calendar). Slack disables each while its call is out (LoadingButton's
// disableWhenLoading, BaseButton U4rH dropping clicks while aria-disabled),
// so a second click does nothing. popups keeps the writes in flight, one
// per key (a card's); richview draws a busy key's buttons in Slack's
// loading state: the spinner over the button that sent it, the others
// disabled.

import (
	"image"
	"image/color"

	"gioui.org/layout"
	"gioui.org/op/clip"
	"gioui.org/op/paint"

	"github.com/some-du6e/clack/internal/richtext"
)

// Busy reports whether the write keyed key is in flight and which button
// sent it (its act). Set by popups, which keeps them.
var Busy func(key string) (act string, busy bool)

// busyBtn is a laid out writing button: its box and corner radius, its
// card's write key and its own act, and the fill its loading state paints
// over its label.
type busyBtn struct {
	r        image.Rectangle
	radius   int
	key, act string
	bg       color.NRGBA
}

// SharePromptKey is a share_prompt's Send write: its block, prompt and
// agent.
func SharePromptKey(p *richtext.SharePrompt) string {
	return "prompt\x00" + p.BlockID + "\x00" + p.PromptID + "\x00" + p.BotUserID
}

// drawBusy draws the busy keys' buttons: the one that sent the write
// covered by its fill and a white spinner (LoadingButton's loading), the
// card's others at half opacity (disabled).
func (v *View) drawBusy(gtx layout.Context, st *Style) {
	if Busy == nil {
		return
	}
	for i := range v.d.busyBtns {
		b := &v.d.busyBtns[i]
		act, busy := Busy(b.key)
		if !busy {
			continue
		}
		if act != b.act {
			veil := st.Palette.Panel
			veil.A = 0x80
			paint.FillShape(gtx.Ops, veil, clip.UniformRRect(b.r, b.radius).Op(gtx.Ops))
			continue
		}
		paint.FillShape(gtx.Ops, b.bg, clip.UniformRRect(b.r, b.radius).Op(gtx.Ops))
		d := min(v.px(20), b.r.Dy())
		c := b.r.Min.Add(b.r.Max).Div(2)
		sp := spinPrim{r: image.Rect(c.X-d/2, c.Y-d/2, c.X-d/2+d, c.Y-d/2+d), white: b.bg.A > 0 && !nearWhite(b.bg)}
		sp.draw(gtx, st)
	}
}

// nearWhite reports a light fill (its spinner is the content colour's).
func nearWhite(c color.NRGBA) bool { return int(c.R)+int(c.G)+int(c.B) > 600 }
