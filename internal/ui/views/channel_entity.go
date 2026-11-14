package views

import (
	"image"
	"image/color"

	"gioui.org/font"
	"gioui.org/layout"
	"gioui.org/op"
	"gioui.org/unit"

	"github.com/some-du6e/clack/internal/core"
	"github.com/some-du6e/clack/internal/ui/chanent"
	"github.com/some-du6e/clack/internal/ui/icons"
)

// A channel as Slack's InlineChannelEntity draws it (internal/ui/chanent
// has the rules), which is how Activity rows name their channel (VeYL's
// destination: the Detailed footer, the Dense location).

// unknownChannelName is formatChannelName's name for a channel without
// one ("unknown-channel").
func unknownChannelName() string { return chanent.UnknownName() }

// channelEnt is channel id (not a DM or a group DM) as InlineChannelEntity
// draws it, from core's store: core.InlineChannel starts the lookup for a
// channel it can't name, whose "names" event redraws it.
func (b *base) channelEnt(id string) chanent.Ent { return b.refEnt(core.ConvRef{ID: id}) }

// refEnt is channelEnt for a conversation a page named: the store's
// answer, else the name the page came with, else the lookup's state.
func (b *base) refEnt(r core.ConvRef) chanent.Ent {
	app := b.h.Core()
	ic, ok := app.KnownChannel(r.ID)
	switch {
	case ok:
	case r.Name != "" && r.Name != r.ID:
		ic = core.InlineChannel{Name: r.Name, Private: r.Kind == "private", State: core.ChannelKnown}
	default:
		ic = app.InlineChannel(r.ID)
	}
	return chanent.Of(r.ID, ic, func(name string) string { return b.viewChannelName(r.ID, name) })
}

// chanEntity draws e on one line in size, weight and col, as
// InlineChannelEntity: the icon (1em) 2px before the name; the hash and
// the MissingText bar (an 8px base-ter pill, its bottom on the text's
// baseline) while it loads; the Private channel chip.
func (b *base) chanEntity(gtx C, e chanent.Ent, size unit.Sp, w font.Weight, col color.NRGBA) D {
	gtx.Constraints.Min = image.Point{}
	switch {
	case e.Chip:
		return b.privateChip(gtx, e, size, w)
	case e.Loading():
		d := b.label(gtx, size, w, col, e.Text)
		bw := min(gtx.Dp(e.Bar), max(0, gtx.Constraints.Max.X-d.Size.X))
		base := d.Size.Y - d.Baseline
		rrect(gtx, image.Rect(d.Size.X, base-gtx.Dp(8), d.Size.X+bw, base), gtx.Dp(4), b.p.Skel)
		return D{Size: image.Pt(d.Size.X+bw, d.Size.Y), Baseline: d.Baseline}
	case e.Icon == "":
		return b.label(gtx, size, w, col, e.Text)
	}
	return row(gtx, layout.Middle,
		layout.Rigid(func(gtx C) D {
			return layout.Inset{Right: 2}.Layout(gtx, func(gtx C) D { return b.icon(gtx, icons.Slack(e.Icon), unit.Dp(size), col) })
		}),
		layout.Rigid(func(gtx C) D { return b.label(gtx, size, w, col, e.Text) }),
	)
}

// privateChip is BaseMissingChannel's non-existent chip
// (c-missing_channel--private): the lock and the label 4px apart in
// rgba(foreground_max, .7) over rgba(foreground_low, .13), radius 3,
// padding 0 2px 1px 0, in the surrounding font.
func (b *base) privateChip(gtx C, e chanent.Ent, size unit.Sp, w font.Weight) D {
	ink := color.NRGBA{R: 29, G: 28, B: 29, A: 255} // --sk_foreground_max / _low, light
	if b.th.Dark {
		ink = color.NRGBA{R: 232, G: 232, B: 232, A: 255}
	}
	fg, bg := ink, ink
	fg.A, bg.A = 179, 33 // .7, .13
	m := op.Record(gtx.Ops)
	d := layout.Inset{Right: 2, Bottom: 1}.Layout(gtx, func(gtx C) D {
		return row(gtx, layout.Middle,
			layout.Rigid(func(gtx C) D { return b.icon(gtx, icons.Slack(e.Icon), unit.Dp(size), fg) }),
			layout.Rigid(func(gtx C) D {
				return layout.Inset{Left: 4}.Layout(gtx, func(gtx C) D { return b.label(gtx, size, w, fg, e.Text) })
			}),
		)
	})
	call := m.Stop()
	rrect(gtx, image.Rectangle{Max: d.Size}, gtx.Dp(3), bg)
	call.Add(gtx.Ops)
	return d
}
