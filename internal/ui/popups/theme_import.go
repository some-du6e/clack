package popups

import (
	"gioui.org/io/key"
	"gioui.org/layout"
	"gioui.org/widget"

	"github.com/some-du6e/yoloslackgoclient/internal/ui/theme"
)

// ThemeImportModal is Slack's ThemeImportHexModalV2 ("Import theme"): a
// theme-string field, Cancel and Apply. apply gets the string and reports
// whether it took it; if not the modal stays with "Invalid theme string".
type ThemeImportModal struct {
	h       Host
	modal   *Modal
	ed      widget.Editor
	err     string
	focused bool
	apply   func(s string) bool
}

// NewThemeImportModal opens it.
func NewThemeImportModal(h Host, apply func(s string) bool) *ThemeImportModal {
	m := &ThemeImportModal{h: h, apply: apply}
	m.ed.SingleLine, m.ed.Submit = true, true
	m.modal = NewModal(h, ModalOpts{Title: "Import theme", Body: m.layout, EnterSubmits: true, Buttons: []*ModalButton{
		{Label: "Cancel", Kind: BtnOutline},
		{Label: "Apply", Kind: BtnPrimary, Do: func() bool { return m.Apply() }},
	}})
	return m
}

func (m *ThemeImportModal) Layout(gtx layout.Context) layout.Dimensions { return m.modal.Layout(gtx) }
func (m *ThemeImportModal) Dismissed() bool                             { return m.modal.Dismissed() }
func (m *ThemeImportModal) Dismiss()                                    { m.modal.Dismiss() }

// SetText types into the field (tests).
func (m *ThemeImportModal) SetText(s string) { m.ed.SetText(s); m.err = "" }

// Apply is the Apply button: true when the theme was taken (the modal
// closes).
func (m *ThemeImportModal) Apply() bool {
	m.err = ""
	if m.apply != nil && m.apply(m.ed.Text()) {
		return true
	}
	m.err = "Invalid theme string"
	return false
}

// Error is the field's error ("" for none).
func (m *ThemeImportModal) Error() string { return m.err }

func (m *ThemeImportModal) layout(gtx layout.Context) layout.Dimensions {
	if !m.focused {
		gtx.Execute(key.FocusCmd{Tag: &m.ed})
		m.focused = true
	}
	for {
		e, ok := m.ed.Update(gtx)
		if !ok {
			break
		}
		switch e.(type) {
		case widget.ChangeEvent:
			m.err = ""
		case widget.SubmitEvent:
			if m.Apply() {
				m.modal.Dismiss()
			}
		}
	}
	th := m.h.Theme()
	cl := colorsOf(th)
	return layout.Flex{Axis: layout.Vertical}.Layout(gtx,
		layout.Rigid(func(gtx layout.Context) layout.Dimensions {
			return layout.Inset{Bottom: 8}.Layout(gtx, func(gtx layout.Context) layout.Dimensions {
				return para(gtx, th, 15, 22, theme.Bold, cl.Fg, "Paste in a theme string", 0)
			})
		}),
		layout.Rigid(func(gtx layout.Context) layout.Dimensions {
			return field(gtx, th, &m.ed, "#124426, #350D36, #EEB317, #392104", "", 36, 15)
		}),
		layout.Rigid(func(gtx layout.Context) layout.Dimensions {
			return layout.Inset{Top: 6}.Layout(gtx, func(gtx layout.Context) layout.Dimensions {
				if m.err != "" {
					return para(gtx, th, 13, 18, theme.Regular, cl.Danger, m.err, 0)
				}
				return para(gtx, th, 13, 18, theme.Regular, cl.Fg2, "Import Slack theme strings. We’ll adapt the colors as best we can to preserve contrast. ", 0)
			})
		}),
	)
}
