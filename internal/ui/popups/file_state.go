package popups

import (
	"context"
	"image"
	"image/color"
	"strings"
	"time"

	"gioui.org/font"
	"gioui.org/io/pointer"
	"gioui.org/layout"
	"gioui.org/op"
	"gioui.org/unit"
	"gioui.org/widget"

	"github.com/some-du6e/clack/internal/i18n"
	"github.com/some-du6e/clack/internal/ui/theme"
)

// FileState is the layout Slack's states for a file it doesn't show share:
// QuipDeletedState (.p-quip_deleted_state, a canvas) and
// BaseListErrorState (.p-list_error_state, a list), from
// client-boot-styles.cf726c4f2eaecc74:
//
//   - the container is a flex box centring the state both ways over the
//     whole body; the state is text-align centre with margin-top -90px,
//     so its box sits 45px above the centre (flex centres the margin box);
//   - __icon: an inline-block at font-size 36px (an Icon of size inherit,
//     or a 32px .c-emoji__large), in a 15px/1.46668 line;
//   - .margin_bottom_100 (16px below) holds the h1 __title, 18px Lato
//     Black at line-height 1.33334 (the canvas's with padding 0 16px), and
//     the h2 __subtitle, 15px regular at 1.46668, 8px below it (the list's
//     with min-height 22px);
//   - then the canvas's .actions__cyRuI row (centred, gap 16px) or the
//     list's lone button: FileRequestAccessButton, a medium primary
//     c-button.
type FileState struct {
	Icon  string // the 36px icon's name, when there is no emoji
	Emoji string // an emoji image URL (EmojiURL), drawn at 32px
	Title string
	Sub   string
	// Link is a part of Sub drawn as a link (BaseListErrorState's
	// "<link>Restore this list</link>"), LinkBtn its click.
	Link    string
	LinkBtn *widget.Clickable
	// Button is the primary button's label ("" for none), ButtonBtn its
	// click, ButtonOff whether it is disabled ("Request Sent").
	Button    string
	ButtonBtn *widget.Clickable
	ButtonOff bool
	// List is BaseListErrorState: no padding beside the title, a 22px
	// minimum subtitle.
	List bool
}

// Layout draws the state over gtx's whole box. h draws the emoji.
func (s FileState) Layout(gtx layout.Context, h Host) layout.Dimensions {
	th := h.Theme()
	sz := gtx.Constraints.Max
	fg := colorsOf(th).Fg
	iconLineH := gtx.Sp(unit.Sp(36 * 1.46668)) // the 36px font's line
	pad := 16
	if s.List {
		pad = 0
	}
	titleLines := epWrap(gtx, th, 18, theme.Black, s.Title, max(1, sz.X-dp(gtx, float32(2*pad))))
	titleLH := dp(gtx, 24) // 18px × 1.33334
	subLH := dp(gtx, 22)   // 15px × 1.46668
	var subLines []string
	oneLine := false
	if s.Sub != "" {
		if s.Link != "" && s.LinkBtn != nil && textW(gtx, th, 15, theme.Regular, s.Sub) <= sz.X {
			oneLine = true
		} else {
			subLines = epWrap(gtx, th, 15, theme.Regular, s.Sub, max(1, sz.X))
		}
	}
	subH := 0
	switch {
	case oneLine:
		subH = subLH
	case len(subLines) > 0:
		subH = len(subLines) * subLH
	}
	if s.List && s.Sub != "" {
		subH = max(subH, dp(gtx, 22))
	}
	bh := 0
	if s.Button != "" && s.ButtonBtn != nil {
		bh = dp(gtx, 36)
	}
	h0 := iconLineH + len(titleLines)*titleLH + dp(gtx, 16) + bh
	gap := 0 // the h2's margin-top; FileStorageWarning's span has none
	if s.Title != "" {
		gap = dp(gtx, 8)
	}
	if s.Sub != "" {
		h0 += gap + subH
	}
	// flex centres the margin box: margin-top -90px lifts the box by 45
	y := (sz.Y-h0)/2 - dp(gtx, 45)
	if s.Emoji != "" {
		es := dp(gtx, 32)
		at(gtx, image.Pt((sz.X-es)/2, y+(iconLineH-es)/2), func(gtx layout.Context) layout.Dimensions {
			if img, ok := h.Images().Get(s.Emoji, es); ok {
				return containImage(gtx, img, image.Pt(es, es))
			}
			return layout.Dimensions{Size: image.Pt(es, es)}
		})
	} else if s.Icon != "" {
		is := dp(gtx, 36)
		at(gtx, image.Pt((sz.X-is)/2, y+(iconLineH-is)/2), func(gtx layout.Context) layout.Dimensions {
			return icon(gtx, s.Icon, 36, fg)
		})
	}
	y += iconLineH
	line := func(y, size int, wt font.Weight, col color.NRGBA, lh int, l string) {
		w := textW(gtx, th, float32(size), wt, l)
		at(gtx, image.Pt((sz.X-w)/2, y), func(gtx layout.Context) layout.Dimensions {
			gtx.Constraints = layout.Constraints{Max: image.Pt(w+2, lh)}
			return vcenter(gtx, lh, func(gtx layout.Context) layout.Dimensions {
				return text(gtx, th, float32(size), wt, col, l)
			})
		})
	}
	for _, l := range titleLines {
		line(y, 18, theme.Black, fg, titleLH, l)
		y += titleLH
	}
	if s.Sub != "" {
		y += gap
		if oneLine {
			s.layoutLinkLine(gtx, th, y, sz.X)
		} else {
			for i, l := range subLines {
				line(y+i*subLH, 15, theme.Regular, fg, subLH, l)
			}
		}
		y += subH
	}
	y += dp(gtx, 16)
	if bh > 0 {
		kind := BtnPrimary
		if s.ButtonOff {
			kind = BtnDisabled
		}
		m := op.Record(gtx.Ops)
		g := gtx
		g.Constraints = layout.Constraints{Max: image.Pt(sz.X, bh)}
		d := Button(g, th, s.ButtonBtn, kind, BtnMedium, s.Button, "", 0)
		call := m.Stop()
		at(gtx, image.Pt((sz.X-d.Size.X)/2, y), func(gtx layout.Context) layout.Dimensions {
			call.Add(gtx.Ops)
			return d
		})
	}
	return layout.Dimensions{Size: sz}
}

// layoutLinkLine draws the one-line subtitle at y, centred across w, its
// Link part a .c-link (the link colour, underlined on hover).
func (s FileState) layoutLinkLine(gtx layout.Context, th *theme.Theme, y, w int) {
	i := strings.Index(s.Sub, s.Link)
	if i < 0 {
		i = len(s.Sub)
	}
	pre, lnk, post := s.Sub[:i], s.Sub[i:min(len(s.Sub), i+len(s.Link))], s.Sub[min(len(s.Sub), i+len(s.Link)):]
	cl := colorsOf(th)
	pw := textW(gtx, th, 15, theme.Regular, pre)
	lw := textW(gtx, th, 15, theme.Regular, lnk)
	qw := textW(gtx, th, 15, theme.Regular, post)
	lh := dp(gtx, 22)
	x := (w - pw - lw - qw) / 2
	piece := func(x, pw int, col color.NRGBA, txt string) {
		if txt == "" {
			return
		}
		at(gtx, image.Pt(x, y), func(gtx layout.Context) layout.Dimensions {
			gtx.Constraints = layout.Constraints{Max: image.Pt(pw+2, lh)}
			return vcenter(gtx, lh, func(gtx layout.Context) layout.Dimensions {
				return text(gtx, th, 15, theme.Regular, col, txt)
			})
		})
	}
	piece(x, pw, cl.Fg, pre)
	hovered := s.LinkBtn.Hovered()
	at(gtx, image.Pt(x+pw, y), func(gtx layout.Context) layout.Dimensions {
		gtx.Constraints = layout.Exact(image.Pt(lw, lh))
		return s.LinkBtn.Layout(gtx, func(gtx layout.Context) layout.Dimensions {
			pointer.CursorPointer.Add(gtx.Ops)
			return layout.Dimensions{Size: image.Pt(lw, lh)}
		})
	})
	piece(x+pw, lw, th.C.Link, lnk)
	if hovered {
		one := max(1, dp(gtx, 1))
		base := y + lh - dp(gtx, 4)
		theme.Fill(gtx, image.Rect(x+pw, base, x+pw+lw, base+one), th.C.Link)
	}
	piece(x+pw+lw, qw, cl.Fg, post)
}

// EmojiURL is an emoji name's image URL (core's custom map, aliases, the
// standard CDN), "" when it has none.
func EmojiURL(h Host, name string) string { return emojiURL(h, name) }

// RequestFileAccess is FileRequestAccessButton's click:
// requestAccessModalThunk with skipConfirmation, which sends requestType
// "access" with reason "DirectRequestAccessModal", then opens Slack's
// done dialog (ZlLL) or, when the request fails, its error dialog (e5OL).
// sent runs (on the UI goroutine) once the request was made, ok or not:
// the button reads "Request Sent" from the click on.
func RequestFileAccess(h Host, id string, post func(func())) {
	a := h.Core()
	if a == nil {
		return
	}
	go func() {
		ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
		defer cancel()
		err := a.RequestFileAccess(ctx, id, "DirectRequestAccessModal", "access")
		post(func() {
			if err != nil {
				openAccessError(h)
				return
			}
			openAccessDone(h)
		})
	}()
}

var nsRequestDone = i18n.NS("request_access_done")

// openAccessDone is the Request Access Done modal (ZlLL).
func openAccessDone(h Host) *Modal {
	m := NewModal(h, ModalOpts{
		Title: nsRequestDone.T("Request sent"),
		Body: func(gtx layout.Context) layout.Dimensions {
			th := h.Theme()
			return para(gtx, th, 15, 22, theme.Regular, colorsOf(th).Fg, nsRequestDone.T("You’ll be notified once it’s been shared with you"), 0)
		},
		Buttons: []*ModalButton{{Label: nsRequestDone.T("Done"), Kind: BtnPrimary}},
	})
	h.ShowOverlay(m)
	return m
}

// openAccessError is the Request Access error modal (e5OL). Its "let us
// know" is Slack's feedback link, drawn as text: Clack has no feedback
// form to open.
func openAccessError(h Host) *Modal {
	m := NewModal(h, ModalOpts{
		Title: nsRequestDone.T("Error"),
		Body: func(gtx layout.Context) layout.Dimensions {
			th := h.Theme()
			return para(gtx, th, 15, 22, theme.Regular, colorsOf(th).Fg, i18n.Plain(nsRequestDone.RT("Something’s gone wrong. If the problem persists, <a>let us know</a>.")), 0)
		},
		Buttons: []*ModalButton{{Label: nsRequestDone.T("Close"), Kind: BtnOutline}},
	})
	h.ShowOverlay(m)
	return m
}
