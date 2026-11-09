package popups

import (
	"image"

	"gioui.org/io/event"
	"gioui.org/io/key"
	"gioui.org/layout"
	"gioui.org/widget"

	"github.com/some-du6e/yoloslackgoclient/internal/ui/theme"
)

// Measured from app.slack.com (c-sk-modal, "Set a status" / "Delete
// message"): width 520, radius 8, ring 1px rgba(29,28,29,.13) + 0 18px 48px
// rgba(0,0,0,.35); backdrop rgba(29,28,29,.7). Header 70px: title 22/900
// (line 30) at 28,20; close button 36x36 radius 8 at the right. Body
// padding 0 28, 15px text. Footer buttons right-aligned, 36px, radius 8,
// 15/700, gap 12; padding 24 28 with a top border when the body scrolls,
// 20 28 without.

// ModalButton is a footer button.
type ModalButton struct {
	Label   string
	Kind    BtnKind // BtnOutline, BtnPrimary, BtnDanger
	Enabled func() bool
	// Do runs on click; return true to close the modal.
	Do    func() bool
	click widget.Clickable
}

// ModalOpts configure a modal.
type ModalOpts struct {
	Title string
	Width float32 // dp, default 520
	// Body draws the content; constraints carry the body width.
	Body    func(gtx layout.Context) layout.Dimensions
	Buttons []*ModalButton
	// Border draws a footer top border (long, scrolling bodies).
	Border bool
	// EnterSubmits makes Enter activate the last enabled button.
	EnterSubmits bool
	OnClose      func()
	// NoClose hides the header close button.
	NoClose bool
	// PadX and HeadH override the side padding (default 28) and header
	// height (default 70) in dp; Slack's link dialog uses 24 / 68.
	PadX, HeadH float32
	// Flush draws the body across the card's full width (from x 0) with no
	// bottom padding: bodies that lay out their own columns (Preferences).
	Flush bool
	// Class adds Slack classes to the modal element (.c-sk-modal), e.g.
	// Preferences' p-prefs_dialog__modal, which themes style.
	Class string
	// TitleIcon and TitleAfter follow Title on its line (Slack's "Add
	// people to 🔒 plans": a channel icon and name).
	TitleIcon, TitleAfter string
	// Subtitle is a 15px line under the title, after SubtitleIcon (the
	// create-channel modal's "# name" on its visibility step).
	Subtitle, SubtitleIcon string
	// FooterNote sits at the footer's left ("Step 1 of 2").
	FooterNote string
}

// Modal is a centered dialog over a dimmed backdrop.
type Modal struct {
	base
	h     Host
	o     ModalOpts
	scrim scrim
	block blocker
	close widget.Clickable
	list  layout.List
	rect  image.Rectangle
	focus bool
}

// NewModal builds a modal; show it with h.ShowOverlay.
func NewModal(h Host, o ModalOpts) *Modal {
	m := &Modal{h: h, o: o}
	m.onClose = o.OnClose
	m.list.Axis = layout.Vertical
	return m
}

func (b *ModalButton) enabled() bool { return b.Enabled == nil || b.Enabled() }

func (m *Modal) press(b *ModalButton) {
	if !b.enabled() {
		return
	}
	if b.Do == nil || b.Do() {
		m.Dismiss()
	}
}

// Layout draws the modal.
func (m *Modal) Layout(gtx layout.Context) layout.Dimensions {
	drain()
	if m.done {
		return layout.Dimensions{}
	}
	stacked(gtx, m)
	th := m.h.Theme()
	cl := colorsOf(th)
	win := gtx.Constraints.Max
	gtx.Constraints.Min = image.Point{} // robust to roots passing Exact constraints
	if !m.focus {
		gtx.Execute(key.FocusCmd{Tag: m})
		m.focus = true
	}
	// keys: Escape anywhere, Enter when asked.
	for {
		var e event.Event
		var ok bool
		if isTop(m) {
			e, ok = gtx.Event(key.Filter{Name: key.NameEscape}, key.Filter{Focus: m, Name: key.NameReturn}, key.Filter{Focus: m, Name: key.NameEnter})
		} else {
			e, ok = gtx.Event(key.Filter{Focus: m, Name: key.NameReturn}, key.Filter{Focus: m, Name: key.NameEnter})
		}
		if !ok {
			break
		}
		ke, ok := e.(key.Event)
		if !ok || ke.State != key.Press {
			continue
		}
		switch ke.Name {
		case key.NameEscape:
			m.Dismiss()
			return layout.Dimensions{}
		default:
			if m.o.EnterSubmits {
				for i := len(m.o.Buttons) - 1; i >= 0; i-- {
					if b := m.o.Buttons[i]; b.enabled() {
						m.press(b)
						break
					}
				}
			}
		}
	}
	if m.done {
		return layout.Dimensions{}
	}
	// backdrop
	fill(gtx, image.Rectangle{Max: win}, cl.Backdrop)
	if m.scrim.Layout(gtx) {
		m.Dismiss()
		return layout.Dimensions{}
	}
	for _, b := range m.o.Buttons {
		if b.click.Clicked(gtx) {
			m.press(b)
		}
	}
	if m.close.Clicked(gtx) {
		m.Dismiss()
	}
	if m.done {
		return layout.Dimensions{}
	}

	wdp := m.o.Width
	if wdp == 0 {
		wdp = 520
	}
	w := dp(gtx, wdp)
	if mw := win.X - 2*dp(gtx, 28); w > mw {
		w = mw
	}
	padX := dp(gtx, 28)
	if m.o.PadX > 0 {
		padX = dp(gtx, m.o.PadX)
	}
	headH := dp(gtx, 70)
	if m.o.HeadH > 0 {
		headH = dp(gtx, m.o.HeadH)
	}
	subH := 0
	if m.o.Subtitle != "" {
		subH = dp(gtx, 22)
		headH += subH
	}
	footPad := dp(gtx, 20)
	if m.o.Border {
		footPad = dp(gtx, 24)
	}
	footH := 0
	if len(m.o.Buttons) > 0 {
		footH = 2*footPad + dp(gtx, 36)
		if m.o.Border {
			footH++
		}
	}
	bodyW, bodyX := w-2*padX, padX
	if m.o.Flush {
		bodyW, bodyX = w, 0
	}
	// measure the body
	var bodyH int
	if m.o.Body != nil {
		_, d := record(loose(gtx, image.Pt(bodyW, 1<<20)), m.o.Body)
		bodyH = d.Size.Y
	}
	maxBody := win.Y - 2*dp(gtx, 28) - headH - footH
	if maxBody < dp(gtx, 60) {
		maxBody = dp(gtx, 60)
	}
	scroll := bodyH > maxBody
	if scroll {
		bodyH = maxBody
	}
	h := headH + bodyH + footH
	if len(m.o.Buttons) == 0 && !m.o.Flush {
		h += dp(gtx, 28)
	}
	pos := image.Pt((win.X-w)/2, (win.Y-h)/2)
	m.rect = image.Rectangle{Min: pos, Max: pos.Add(image.Pt(w, h))}
	keyTarget(gtx, m, m.rect)
	st := modalClass.EnterClass(gtx, 0, 0, m.o.Class)
	defer st.Leave()
	if !st.Hidden() {
		styledCard(gtx, st, cl, pos, m.rect.Size(), dp(gtx, 8), true, func(gtx layout.Context) {
			{
				pos := image.Point{}
				local := image.Rectangle{Max: m.rect.Size()}
				m.block.Layout(gtx, local)

				// header
				ty := (headH - subH - dp(gtx, 30)) / 2
				at(gtx, pos.Add(image.Pt(padX, ty)), func(gtx layout.Context) layout.Dimensions {
					maxW := w - padX - dp(gtx, 64)
					gtx.Constraints = layout.Constraints{Max: image.Pt(maxW, dp(gtx, 30))}
					d := para(gtx, th, 22, 30, theme.Black, cl.Fg, m.o.Title, 1)
					if m.o.TitleIcon == "" && m.o.TitleAfter == "" {
						return d
					}
					x := d.Size.X
					if m.o.TitleIcon != "" {
						at(gtx, image.Pt(x, dp(gtx, 6)), func(gtx layout.Context) layout.Dimensions {
							return icon(gtx, m.o.TitleIcon, 20, cl.Fg)
						})
						x += dp(gtx, 22)
					}
					at(gtx, image.Pt(x, 0), func(gtx layout.Context) layout.Dimensions {
						gtx.Constraints = layout.Constraints{Max: image.Pt(maxInt(1, maxW-x), dp(gtx, 30))}
						return para(gtx, th, 22, 30, theme.Black, cl.Fg, m.o.TitleAfter, 1)
					})
					return d
				})
				if m.o.Subtitle != "" {
					at(gtx, pos.Add(image.Pt(padX, ty+dp(gtx, 30))), func(gtx layout.Context) layout.Dimensions {
						x := 0
						if m.o.SubtitleIcon != "" {
							at(gtx, image.Pt(0, (subH-dp(gtx, 15))/2), func(gtx layout.Context) layout.Dimensions {
								return icon(gtx, m.o.SubtitleIcon, 15, cl.Fg2)
							})
							x = dp(gtx, 17)
						}
						return at(gtx, image.Pt(x, 0), func(gtx layout.Context) layout.Dimensions {
							gtx.Constraints = layout.Constraints{Max: image.Pt(maxInt(1, w-2*padX-x), subH)}
							return para(gtx, th, 15, 22, theme.Regular, cl.Fg2, m.o.Subtitle, 1)
						})
					})
				}
				if !m.o.NoClose {
					at(gtx, pos.Add(image.Pt(w-dp(gtx, 20)-dp(gtx, 36), (headH-dp(gtx, 36))/2)), func(gtx layout.Context) layout.Dimensions {
						return iconButton(gtx, &m.close, "close", 36, 20, cl.Fg2, cl.BgHover, 8)
					})
				}
				// body
				if m.o.Body != nil {
					at(gtx, pos.Add(image.Pt(bodyX, headH)), func(gtx layout.Context) layout.Dimensions {
						gtx.Constraints = layout.Constraints{Min: image.Pt(bodyW, 0), Max: image.Pt(bodyW, bodyH)}
						if !scroll {
							gtx.Constraints.Min.X = 0
							return m.o.Body(gtx)
						}
						defer clipRRect(gtx, image.Rect(0, 0, bodyW, bodyH), 0).Pop()
						return m.list.Layout(gtx, 1, func(gtx layout.Context, _ int) layout.Dimensions {
							gtx.Constraints.Min.X = 0
							return m.o.Body(gtx)
						})
					})
				}
				// footer
				if len(m.o.Buttons) > 0 {
					fy := pos.Y + headH + bodyH
					if m.o.Border {
						fill(gtx, image.Rect(pos.X, fy, pos.X+w, fy+1), cl.Ring)
						fy++
					}
					if m.o.FooterNote != "" {
						at(gtx, image.Pt(pos.X+padX, fy+footPad), func(gtx layout.Context) layout.Dimensions {
							bh := dp(gtx, 36)
							gtx.Constraints = layout.Constraints{Max: image.Pt(w/2, bh)}
							return vcenter(gtx, bh, func(gtx layout.Context) layout.Dimensions {
								return text(gtx, th, 15, theme.Regular, cl.Fg3, m.o.FooterNote)
							})
						})
					}
					x := pos.X + w - padX
					for i := len(m.o.Buttons) - 1; i >= 0; i-- {
						b := m.o.Buttons[i]
						kind := b.Kind
						if !b.enabled() {
							kind = BtnDisabled
						}
						call, d := record(gtx, func(gtx layout.Context) layout.Dimensions {
							return Button(gtx, th, &b.click, kind, BtnMedium, b.Label, "", 80)
						})
						x -= d.Size.X
						macroAt(gtx, call, image.Pt(x, fy+footPad))
						x -= dp(gtx, 12)
					}
				}
			}
		})
	}
	return layout.Dimensions{Size: win}
}

// Confirm opens a two-button confirmation modal (Cancel + action). body may
// be nil; text is shown above it.
func Confirm(h Host, title, textBody, action string, danger bool, body layout.Widget, onOK func()) *Modal {
	th := h.Theme()
	kind := BtnPrimary
	if danger {
		kind = BtnDanger
	}
	m := NewModal(h, ModalOpts{
		Title:        title,
		EnterSubmits: true,
		Body: func(gtx layout.Context) layout.Dimensions {
			cl := colorsOf(th)
			return layout.Flex{Axis: layout.Vertical}.Layout(gtx,
				layout.Rigid(func(gtx layout.Context) layout.Dimensions {
					if textBody == "" {
						return layout.Dimensions{}
					}
					return para(gtx, th, 15, 22, theme.Regular, cl.Fg, textBody, 0)
				}),
				layout.Rigid(func(gtx layout.Context) layout.Dimensions {
					if body == nil {
						return layout.Dimensions{}
					}
					return layout.Inset{Top: 12}.Layout(gtx, body)
				}),
			)
		},
		Buttons: []*ModalButton{
			{Label: "Cancel", Kind: BtnOutline},
			{Label: action, Kind: kind, Do: func() bool {
				if onOK != nil {
					onOK()
				}
				return true
			}},
		},
	})
	h.ShowOverlay(m)
	return m
}
