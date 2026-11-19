package views

// FileShares is a file's Shared in section: FileDetails' FileShareContext
// (boot-shared 2TKv, build 031dbe6a7400), which the email FileDetails
// draws under the email (FkQ0 EmailFileDetailsContent's
// renderSharedInSection; ui/email_pane.go). Its data is files.getShares
// (core.FileShares):
//
//   - While it runs (the fileShares object isn't fresh): in
//     .shareContextContainer (16px all round) .shareDividerEmpty (16px
//     over: a small MissingText, 10px before the otl-ter rule), then 8px
//     under (margin_top_50) three lines each holding a MissingText, large,
//     medium and medium, and an empty line. A MissingText is an 8px pill
//     in base-ter: small 80px wide, medium 120, large 160.
//   - No shares at all: .shareDividerEmpty's rule, then the empty state
//     (.emptyState: centred, at least 70% of the window tall, 16px in):
//     ShareContextEmptyState-0e52ceb.png 150px wide (448×232), 16px over
//     "No references yet" (18px Bold, 8px over) and "When this file
//     appears in a channel or conversation, it’ll also appear here."
//     (15px), both content-pry.
//   - Else in .shareContextContainer: .shareDivider (8px 0 12px:
//     "Conversations {count}" in 13px Bold sk_foreground_max_solid, 10px
//     before the rule; with files_info_pane_count_style on only the word
//     is bold) and a FileShareDetailsMessageEntity per share (0MIe: the
//     link details' card, link_details.go; padding_bottom_75, 12px), for
//     a share whose message and channel the client has
//     (core.FileShareCard). A click opens the thread at it (threadTs ??
//     shareTs).
//   - The Tabs and Files sections (tab_shares, file_channel_shares: a
//     file shared as a channel tab or into a file's channel) aren't drawn;
//     a fetch that fails draws nothing, as FileShareContext does with no
//     data.

import (
	"context"
	"image"
	"strconv"
	"strings"

	"gioui.org/layout"
	"gioui.org/op"
	"gioui.org/text"
	"gioui.org/unit"
	"gioui.org/widget"

	"github.com/some-du6e/clack/internal/core"
	"github.com/some-du6e/clack/internal/i18n"
	"github.com/some-du6e/clack/internal/ui/theme"
)

// fileSharesEmptyArt is renderEmptyState's image (EvDM), 448×232.
const fileSharesEmptyArt = "https://a.slack-edge.com/bv1-13-br/ShareContextEmptyState-0e52ceb.png"

// FileShares is a file's Shared in section.
type FileShares struct {
	base
	fileID string
	data   async[core.FileShareInfo]
	cards  map[string]*widget.Clickable
	// got are the shares' cards the store could give, read again after
	// any message changes (seq: core's MessageChangeSeq)
	got map[string]core.LinkShare
	seq uint64
}

// NewFileShares starts reading file id's shares.
func NewFileShares(h Host, id string) *FileShares {
	v := &FileShares{base: newBase(h), fileID: id, cards: map[string]*widget.Clickable{}}
	if a := h.Core(); a != nil && id != "" {
		v.data.load(h, false, func(ctx context.Context) (core.FileShareInfo, error) { return a.FileShares(ctx, id) })
	}
	return v
}

// Shares is what files.getShares said, and whether it answered.
func (v *FileShares) Shares() (core.FileShareInfo, bool) {
	s, ok, err := v.data.get()
	return s, ok && err == nil && !v.data.busy()
}

// Layout draws the section in the width given; vh is the window's height
// (the empty state's 70vh).
func (v *FileShares) Layout(gtx C, vh int) D {
	v.frame()
	info, loaded, err := v.data.get()
	app := v.h.Core()
	var cards []core.LinkShare
	var shares []core.FileShare
	if loaded && err == nil && app != nil {
		if seq := app.MessageChangeSeq(); v.got == nil || seq != v.seq {
			v.got, v.seq = map[string]core.LinkShare{}, seq
		}
		for _, s := range info.Conversations {
			key := s.Channel + "/" + s.TS
			ls, ok := v.got[key]
			if !ok {
				if ls, ok = app.FileShareCard(s); ok {
					v.got[key] = ls
				}
			}
			if ok {
				cards, shares = append(cards, ls), append(shares, s)
			}
		}
	}
	for i, s := range shares {
		if c := v.cards[s.Channel+"/"+s.TS]; c != nil && c.Clicked(gtx) {
			ts := s.ThreadTS
			if ts == "" {
				ts = cards[i].Message.ThreadTS
			}
			if ts == "" {
				ts = s.TS
			}
			v.h.OpenThread(s.Channel, ts)
		}
	}
	gtx.Constraints.Min = image.Point{}
	switch {
	case v.data.busy() || !loaded && v.data.started():
		return layout.UniformInset(16).Layout(gtx, v.loading)
	case !loaded || err != nil:
		return D{}
	case info.ConversationCount == 0 && info.TabCount == 0 && info.FileChannelCount == 0:
		return v.empty(gtx, vh)
	}
	return layout.UniformInset(16).Layout(gtx, func(gtx C) D {
		if info.ConversationCount == 0 { // only tab or file-channel shares: their sections aren't drawn
			return D{Size: image.Pt(gtx.Constraints.Max.X, 0)}
		}
		ch := []layout.FlexChild{layout.Rigid(func(gtx C) D { return v.divider(gtx, info.ConversationCount, app) })}
		for i := range cards {
			s := &cards[i]
			key := shares[i].Channel + "/" + shares[i].TS
			c := v.cards[key]
			if c == nil {
				c = new(widget.Clickable)
				v.cards[key] = c
			}
			ch = append(ch, layout.Rigid(func(gtx C) D {
				return layout.Inset{Bottom: 12}.Layout(gtx, func(gtx C) D { return v.shareCard(gtx, c, s) })
			}))
		}
		return col(gtx, ch...)
	})
}

// missingText is a MissingText pill w wide.
func (v *FileShares) missingText(gtx C, x, y, w int) {
	h := gtx.Dp(8)
	rrect(gtx, image.Rect(x, y, x+gtx.Dp(unit.Dp(w)), y+h), h/2, v.lt(rgb(0xf8f8f8), rgb(0x2c2d30))) // dt_color-base-ter
}

// rule is the divider's hr from x to the right edge, centred on y.
func (v *FileShares) rule(gtx C, x, y int) {
	w := gtx.Constraints.Max.X
	if x < w {
		fill(gtx, image.Rect(x, y, w, y+max(1, gtx.Dp(1))), theme.Over(v.p.CardLine, v.p.Bg)) // otl-ter
	}
}

// loading is the loading state: the empty divider with a small pill, then
// three lines of pills.
func (v *FileShares) loading(gtx C) D {
	w := gtx.Constraints.Max.X
	y := gtx.Dp(16) // .shareDividerEmpty: padding-top 16px
	v.missingText(gtx, 0, y, 80)
	v.rule(gtx, gtx.Dp(80)+gtx.Dp(10), y+gtx.Dp(4))
	y += gtx.Dp(8) + gtx.Dp(8) // margin_top_50
	line := gtx.Dp(22)
	for _, pw := range []int{160, 120, 120} {
		v.missingText(gtx, 0, y+gtx.Dp(9), pw) // on the line's baseline
		y += line
	}
	y += line // the last <br>
	return D{Size: image.Pt(w, y)}
}

// divider is .shareDivider: "Conversations {count}" and the rule.
func (v *FileShares) divider(gtx C, n int, app *core.App) D {
	return layout.Inset{Top: 8, Bottom: 12}.Layout(gtx, func(gtx C) D {
		ink := v.lt(rgb(0x616061), rgb(0xababad)) // sk_foreground_max_solid
		ns := i18n.NS("file-details")
		var d D
		x := 0
		if app != nil && app.Experiment(core.ExpFilesInfoPaneCountStyle) == "on" {
			// <strong>Conversations</strong> {count}
			s := ns.T("<strong>Conversations</strong> {count}", "count", strconv.Itoa(n))
			bold, rest, _ := strings.Cut(strings.TrimPrefix(s, "<strong>"), "</strong>")
			d = v.label(gtx, 13, wBold, ink, bold)
			x = v.th.Measure(gtx, 13, wBold, bold)
			dirAt(gtx, image.Pt(x, 0), func(gtx C) D { return v.label(gtx, 13, wReg, ink, rest) })
			x += v.th.Measure(gtx, 13, wReg, rest)
		} else {
			label := ns.T("Conversations {count}", "count", strconv.Itoa(n))
			d = v.label(gtx, 13, wBold, ink, label)
			x = v.th.Measure(gtx, 13, wBold, label)
		}
		v.rule(gtx, x+gtx.Dp(10), d.Size.Y/2)
		return D{Size: image.Pt(gtx.Constraints.Max.X, d.Size.Y)}
	})
}

// empty is renderEmptyState.
func (v *FileShares) empty(gtx C, vh int) D {
	w := gtx.Constraints.Max.X
	y := gtx.Dp(16)
	v.rule(gtx, 0, y)
	y += max(1, gtx.Dp(1))
	// .emptyState: a centred column, min-height 70vh, padding 16px
	pad := gtx.Dp(16)
	iw := min(gtx.Dp(150), max(w-2*pad, gtx.Dp(100)))
	ih := iw * 232 / 448
	tw := max(w-2*pad, 0)
	ns := i18n.NS("file-details")
	title := ns.T("No references yet")
	desc := ns.T("When this file appears in a channel or conversation, it’ll also appear here.")
	// the title and the description, centred in the column's width
	measure := func(st theme.TextStyle, s string) (D, op.CallOp) {
		m := gtx
		m.Constraints = layout.Constraints{Min: image.Pt(tw, 0), Max: image.Pt(tw, 1<<20)}
		macro := op.Record(gtx.Ops)
		d := v.th.Layout(m, st, s)
		return d, macro.Stop()
	}
	td, drawT := measure(theme.TextStyle{Size: 18, Weight: wBold, Color: v.p.Text, Align: text.Middle, LineHeight: 26}, title)
	dd, drawD := measure(theme.TextStyle{Size: 15, Weight: wReg, Color: v.p.Text, Align: text.Middle, LineHeight: 22}, desc)
	content := ih + gtx.Dp(16) + td.Size.Y + gtx.Dp(8) + dd.Size.Y
	h := max(content+2*pad, vh*70/100)
	cy := y + pad + (h-2*pad-content)/2
	if ic := v.h.Images(); ic != nil {
		if img, ok := ic.Get(fileSharesEmptyArt, iw*2); ok {
			dirAt(gtx, image.Pt((w-iw)/2, cy), func(gtx C) D { return themeImage(gtx, img, image.Pt(iw, ih), 0) })
		}
	}
	cy += ih + gtx.Dp(16)
	dirAt(gtx, image.Pt(pad, cy), func(gtx C) D { drawT.Add(gtx.Ops); return td })
	cy += td.Size.Y + gtx.Dp(8)
	dirAt(gtx, image.Pt(pad, cy), func(gtx C) D { drawD.Add(gtx.Ops); return dd })
	return D{Size: image.Pt(w, y+h)}
}
