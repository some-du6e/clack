package popups

// EditAgentThreadNameModal (boot-shared n5nG, web client build
// f132f0e78468): the agent session menu's Rename. "Edit thread name", the
// field (labelled "Thread name" for screen readers only; at most 80
// characters, the current name selected, focused on open) with the hint
// "Make threads easier to find. This will be visible to all
// participants.", then Cancel and Save. Save is off while saving, for an
// empty or unchanged name, or one getAgentThreadNameError refuses (shown
// under the field): over 80 characters, only punctuation, or any of
// @#$%^*+/\:;<>[]{}~`. agents.threads.rename's error codes pick a toast
// and the modal stays open.

import (
	"context"
	"image"
	"strings"
	"unicode/utf8"

	"gioui.org/io/event"
	"gioui.org/io/key"
	"gioui.org/layout"
	"gioui.org/widget"

	"github.com/some-du6e/clack/internal/i18n"
	"github.com/some-du6e/clack/internal/ui/theme"
)

var nsThreads = i18n.NS("threads")

// AgentThreadNameModal is EditAgentThreadNameModal.
type AgentThreadNameModal struct {
	h       Host
	modal   *Modal
	ed      widget.Editor
	name0   string
	focused bool
	pending bool
	save    func(ctx context.Context, name string) error
}

// NewAgentThreadNameModal opens the modal on cur; save renames the thread.
func NewAgentThreadNameModal(h Host, cur string, save func(ctx context.Context, name string) error) *AgentThreadNameModal {
	m := &AgentThreadNameModal{h: h, name0: cur, save: save}
	m.ed.SingleLine, m.ed.Submit = true, true
	m.ed.SetText(cur)
	m.ed.SetCaret(len([]rune(cur)), 0) // forceSelectAll
	ok := &ModalButton{Label: nsThreads.T("Save"), Kind: BtnPrimary, Enabled: m.canSave, Do: func() bool { m.Save(); return false }}
	m.modal = NewModal(h, ModalOpts{Title: nsThreads.T("Edit thread name"), Body: m.layout,
		Buttons:    []*ModalButton{{Label: nsThreads.T("Cancel"), Kind: BtnOutline}, ok},
		FocusStops: func() []event.Tag { return []event.Tag{&m.ed} }})
	return m
}

func (m *AgentThreadNameModal) Layout(gtx layout.Context) layout.Dimensions {
	return m.modal.Layout(gtx)
}
func (m *AgentThreadNameModal) Dismissed() bool { return m.modal.Dismissed() }
func (m *AgentThreadNameModal) Dismiss()        { m.modal.Dismiss() }

// SetName types into the field (tests).
func (m *AgentThreadNameModal) SetName(s string) { m.ed.SetText(s) }

// AgentThreadNameError is getAgentThreadNameError's code for name ("" when
// it's fine).
func AgentThreadNameError(name string) string {
	if name == "" {
		return ""
	}
	if utf8.RuneCountInString(name) > 80 {
		return "name_too_long"
	}
	all := true
	for _, r := range name {
		if !strings.ContainsRune("-_.'‘’\",(){}[]/;", r) {
			all = false
			break
		}
	}
	if all {
		return "invalid_name_punctuation"
	}
	if strings.ContainsAny(name, "@#$%^*+/\\:;<>[]{}~`") {
		return "invalid_name_specials"
	}
	return ""
}

// agentThreadNameMessage is getAgentThreadNameErrorMessage.
func agentThreadNameMessage(code string) string {
	switch code {
	case "name_too_long":
		return nsThreads.T("That name is too long. Try a shorter one.")
	case "invalid_name_specials":
		return nsThreads.T("Thread names can’t include symbols like @, #, or :.")
	case "invalid_name_punctuation":
		return nsThreads.T("Thread names must include at least one letter or number.")
	case "invalid_name":
		return nsThreads.T("That name can’t be used. Try a different one.")
	}
	return nsThreads.T("Something went wrong. Please try again.")
}

func (m *AgentThreadNameModal) canSave() bool {
	n := strings.TrimSpace(m.ed.Text())
	return !m.pending && n != "" && n != strings.TrimSpace(m.name0) && AgentThreadNameError(n) == ""
}

// Save is renameThread with the trimmed field.
func (m *AgentThreadNameModal) Save() {
	if !m.canSave() {
		return
	}
	name := strings.TrimSpace(m.ed.Text())
	m.pending = true
	run(m.h, func(ctx context.Context) (struct{}, error) { return struct{}{}, m.save(ctx, name) }, func(_ struct{}, err error) {
		m.pending = false
		if err != nil {
			code := ""
			for _, c := range []string{"name_too_long", "invalid_name_specials", "invalid_name_punctuation", "invalid_name"} {
				if strings.Contains(err.Error(), c) {
					code = c
					break
				}
			}
			m.h.Toast(agentThreadNameMessage(code))
			return
		}
		m.modal.Dismiss()
	})
}

func (m *AgentThreadNameModal) layout(gtx layout.Context) layout.Dimensions {
	if !m.focused {
		gtx.Execute(key.FocusCmd{Tag: &m.ed})
		m.focused = true
	}
	for {
		e, ok := m.ed.Update(gtx)
		if !ok {
			break
		}
		if _, ok := e.(widget.SubmitEvent); ok {
			m.Save()
		}
	}
	th := m.h.Theme()
	cl := colorsOf(th)
	w := gtx.Constraints.Max.X
	ih := dp(gtx, 44)
	code := AgentThreadNameError(strings.TrimSpace(m.ed.Text()))
	at(gtx, image.Point{}, func(gtx layout.Context) layout.Dimensions {
		gtx.Constraints = layout.Exact(image.Pt(w, ih))
		d := field(gtx, th, &m.ed, "", "", 44, 18)
		if code != "" {
			stroke(gtx, image.Rect(0, 0, w, ih), dp(gtx, 8), border1(gtx), ccErrColor(th, cl))
		}
		return d
	})
	y := ih + dp(gtx, 4)
	if code != "" { // the inline alert
		d := at(gtx, image.Pt(0, y), func(gtx layout.Context) layout.Dimensions {
			gtx.Constraints = layout.Constraints{Max: image.Pt(w, 1<<20)}
			return para(gtx, th, 13, 18, theme.Regular, ccErrColor(th, cl), agentThreadNameMessage(code), 0)
		})
		y += d.Size.Y + dp(gtx, 4)
	}
	d := at(gtx, image.Pt(0, y), func(gtx layout.Context) layout.Dimensions {
		gtx.Constraints = layout.Constraints{Max: image.Pt(w, 1<<20)}
		return para(gtx, th, 13, 18, theme.Regular, cl.Fg2, nsThreads.T("Make threads easier to find. This will be visible to all participants."), 0)
	})
	y += d.Size.Y
	return layout.Dimensions{Size: image.Pt(w, y+dp(gtx, 20))}
}
