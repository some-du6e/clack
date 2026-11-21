package views

import (
	"image"
	"image/color"
	"strings"

	"gioui.org/font"
	"gioui.org/layout"
	"gioui.org/op"
	"gioui.org/unit"

	"github.com/some-du6e/clack/internal/core"
	"github.com/some-du6e/clack/internal/richtext"
	"github.com/some-du6e/clack/internal/ui/chanent"
	"github.com/some-du6e/clack/internal/ui/icons"
	"github.com/some-du6e/clack/internal/ui/theme"
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
	case len(e.Spans) > 0:
		// a canvas's file channel: the canvas icon, then its title
		// (chanent.Canvas)
		return row(gtx, layout.Middle,
			layout.Rigid(func(gtx C) D {
				return layout.Inset{Right: 2}.Layout(gtx, func(gtx C) D { return b.icon(gtx, icons.Slack(e.Icon), unit.Dp(size), col) })
			}),
			layout.Rigid(func(gtx C) D { return b.canvasTitle(gtx, e.Spans, size, w, col) }),
		)
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
// padding 0 2px 1px 0, in the surrounding font. The alphas are the
// browser's (sRGB) blend over the page (theme.LinearAlpha: Gio blends in
// linear light, which made the dark chip a solid grey).
func (b *base) privateChip(gtx C, e chanent.Ent, size unit.Sp, w font.Weight) D {
	ink := color.NRGBA{R: 29, G: 28, B: 29, A: 255} // --sk_foreground_max / _low, light
	if b.th.Dark {
		ink = color.NRGBA{R: 232, G: 232, B: 232, A: 255}
	}
	fg := theme.LinearAlpha(theme.WithAlpha(ink, 179), b.p.Bg) // .7
	bg := theme.LinearAlpha(theme.WithAlpha(ink, 33), b.p.Bg)  // .13
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

// canvasTitle draws a canvas title as the channel entity's
// renderQuipTitle does at caption size, on one line ellipsized at the
// max width: the text in col and weight w (a mention in it plain text:
// .p-quip_title__blocks_simple's c-member_slug inherits the colour and has
// no background), the emoji as 16px images (.p-quip_title__blocks__caption
// .c-emoji--inline) centred on the line. An emoji the store couldn't name
// when the title was converted is resolved now (Slack's Emoji component
// reads the emoji store on every render), else drawn as its ":name:".
func (b *base) canvasTitle(gtx C, spans []richtext.Span, size unit.Sp, w font.Weight, col color.NRGBA) D {
	gtx.Constraints.Min = image.Point{}
	maxW := gtx.Constraints.Max.X
	m := op.Record(gtx.Ops)
	line := b.label(gtx, size, w, col, "Ag") // the line box a label of this size has
	m.Stop()
	es := gtx.Dp(16)
	h := max(line.Size.Y, es)
	top := (h - line.Size.Y) / 2 // the text's place in a line an emoji makes taller
	x := 0
	app := b.h.Core()
	for _, s := range spans {
		if s.Kind == richtext.Break || x >= maxW {
			break
		}
		if s.Kind == richtext.Emoji {
			url := s.URL
			if s.KeptEmoji() && app != nil {
				url, _, _ = app.Emoji(s.ID)
			}
			if url != "" {
				if x+es > maxW {
					break
				}
				u := url
				dirAt(gtx, image.Pt(x, (h-es)/2), func(gtx C) D { return b.emojiImg(gtx, u, unit.Dp(float32(es)/gtx.Metric.PxPerDp)) })
				x += es
				continue
			}
		}
		txt := s.Text
		if s.Kind == richtext.Emoji && txt == "" {
			txt = ":" + s.ID + ":"
		}
		txt = strings.ReplaceAll(txt, "\n", " ")
		if txt == "" {
			continue
		}
		t := op.Offset(image.Pt(x, top)).Push(gtx.Ops)
		g := gtx
		g.Constraints = layout.Constraints{Max: image.Pt(maxW-x, line.Size.Y)}
		d := b.label(g, size, w, col, txt)
		t.Pop()
		x += d.Size.X
	}
	// Baseline is from the bottom: the gap under the text's box, then
	// the label's own
	return D{Size: image.Pt(min(x, maxW), h), Baseline: h - top - line.Size.Y + line.Baseline}
}
