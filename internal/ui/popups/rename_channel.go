package popups

// Renaming a channel: Slack's RenameChannelModal (boot-shared BYCL, web
// client 66e72b57ade1), which /rename opens when isSlashCommandEnabled
// allows it (slash-commands-local BasZ renameCommand: the arguments as
// convertContentsToStringForDisplay gives them, else the channel's name).
// A c-sk-modal titled "Rename this channel":
//
//   - the field (jJZ6 BaseChannelRenameField over psv2 ChannelNameInput):
//     "Channel name", the input with the channel's # or lock, placeholder
//     "e.g. marketing", 80 characters at most (the countdown), and the hint
//     "Names must be lowercase, without spaces or periods, and can’t be
//     longer than 80 characters."; it starts with the name given, as given.
//     Each edit is lowercased, loses its leading "#"s and turns spaces into
//     "-" (core.NormalizeChannelName), and is checked 500 ms later with
//     conversations.validateName (reason create-channel, as the input
//     always asks); a problem shows above the input, beside the label
//     (core.ChannelNameErrorText, no workspace name);
//   - Save Changes is disabled until the name changed (it has from the
//     start when the name given isn't the channel's) or while the check
//     found a problem; the same name closes. Otherwise renameChannelApi
//     (core.Rename: conversations.rename {channel, name, validate: true})
//     closes on success; a failure shows at the top: name_taken "{name} is
//     already taken by a channel, username, or user group.",
//     invalid_name_specials "Channel names can’t support special characters
//     (i.e. !#$%&).", anything else "Oops, something went wrong with
//     renaming the channel. Please try again.".
//
// Not here: the input's :emoji: completion (friendly channel names only)
// and the record channels' rules (Clack has no record channels).

import (
	"context"
	"image"
	"strconv"
	"strings"
	"time"
	"unicode/utf8"

	"gioui.org/io/event"
	"gioui.org/io/key"
	"gioui.org/layout"
	"gioui.org/widget"

	"github.com/some-du6e/clack/internal/core"
	"github.com/some-du6e/clack/internal/slack"
	"github.com/some-du6e/clack/internal/ui/theme"
)

// Rename strings (channel_details; the input's, channel_modal).
const (
	rnTitle       = "Rename this channel"
	rnLabel       = "Channel name"
	rnPlaceholder = "e.g. marketing"
	rnHint        = "Names must be lowercase, without spaces or periods, and can’t be longer than 80 characters."
	rnSave        = "Save Changes"
	rnTaken       = "{name} is already taken by a channel, username, or user group."
	rnSpecials    = "Channel names can’t support special characters (i.e. !#$%&)."
	rnFailed      = "Oops, something went wrong with renaming the channel. Please try again."
)

// rnDebounce is ChannelNameInput's validateName debounce.
var rnDebounce = 500 * time.Millisecond

// RenameChannelModal is "Rename this channel" for one channel.
type RenameChannelModal struct {
	h       Host
	ch      string
	current string // s: the channel's name
	private bool
	modal   *Modal
	ed      widget.Editor
	focused bool
	name    string // b: what Save sends
	last    string // the input's text as last read: a ChangeEvent without a new text is no edit
	changed bool   // f: hasChanges
	gen     int    // bumped by every edit: stale checks are dropped
	code    string // the check's problem (core channel name codes)
	saving  bool
	err     string // A: the rename's failure
}

// NewRenameChannelModal opens the modal for channel ch with name (the
// channel's own when empty) in the input; show it with h.ShowOverlay.
func NewRenameChannelModal(h Host, ch, name string) *RenameChannelModal {
	m := &RenameChannelModal{h: h, ch: ch}
	if a := h.Core(); a != nil {
		if c, ok := a.Conv(ch); ok {
			m.current, m.private = c.Name, c.Kind == "private"
		}
	}
	if name == "" {
		name = m.current
	}
	m.name, m.changed, m.last = name, name != m.current, name
	m.ed.SingleLine = true
	m.ed.SetText(name)
	n := utf8.RuneCountInString(name)
	m.ed.SetCaret(n, n)
	save := &ModalButton{Label: nsChDetails.T(rnSave), Kind: BtnPrimary, Enabled: m.CanSave, Do: func() bool { return m.Save() }}
	m.modal = NewModal(h, ModalOpts{Title: nsChDetails.T(rnTitle), Body: m.layout,
		Buttons:    []*ModalButton{{Label: nsChDetails.T("Cancel"), Kind: BtnOutline}, save},
		FocusStops: func() []event.Tag { return []event.Tag{&m.ed} }})
	return m
}

func (m *RenameChannelModal) Layout(gtx layout.Context) layout.Dimensions { return m.modal.Layout(gtx) }
func (m *RenameChannelModal) Dismissed() bool                             { return m.modal.Dismissed() }
func (m *RenameChannelModal) Dismiss()                                    { m.modal.Dismiss() }

// Title is the modal's title; Name is the name Save sends.
func (m *RenameChannelModal) Title() string { return m.modal.Title() }
func (m *RenameChannelModal) Name() string  { return m.name }

// SetName types s into the input (an edit: normalized, then checked).
func (m *RenameChannelModal) SetName(s string) {
	m.ed.SetText(s)
	m.onEdit()
}

// NameError is the check's problem as the label shows it; Error is the
// rename's failure at the top ("" for none).
func (m *RenameChannelModal) NameError() string { return core.ChannelNameErrorText(m.code, "") }
func (m *RenameChannelModal) Error() string     { return m.err }

// CanSave is Save Changes' `!f || S()` turned around: the name changed
// and the check found none of the input's problems.
func (m *RenameChannelModal) CanSave() bool {
	switch m.code {
	case core.NameRequired, core.NameMaxLength, core.NameSpecials, core.NamePunctuation, core.NameTaken:
		return false
	}
	return m.changed && !m.saving
}

// Save is Save Changes: the channel's own name closes; otherwise the
// rename, closing once Slack took it. It reports whether the modal
// closes now.
func (m *RenameChannelModal) Save() bool {
	if m.name == m.current {
		return true
	}
	a := m.h.Core()
	if a == nil || !m.CanSave() {
		return false
	}
	m.saving, m.err = true, ""
	ch, name := m.ch, m.name
	// the viewer's own Save always sends (never the guard's copy of the
	// last failure), as Slack's does
	run(m.h, func(ctx context.Context) (struct{}, error) {
		return struct{}{}, a.Rename(slack.NoBackoff(ctx), ch, name)
	},
		func(_ struct{}, err error) {
			m.saving = false
			if err == nil {
				m.modal.Dismiss()
				return
			}
			switch slackErrID(err) {
			case core.NameTaken:
				m.err = nsChDetails.T(rnTaken, "name", name)
			case core.NameSpecials:
				m.err = nsChDetails.T(rnSpecials)
			default:
				m.err = nsChDetails.T(rnFailed)
			}
			m.h.Invalidate()
		})
	return false
}

// onEdit is ChannelNameInput's onChange: the name normalized, the change
// noted, the check scheduled.
func (m *RenameChannelModal) onEdit() {
	raw := m.ed.Text()
	norm := core.NormalizeChannelName(raw)
	if norm != raw {
		caret, _ := m.ed.Selection()
		prefix := string([]rune(raw)[:min(caret, utf8.RuneCountInString(raw))])
		n := utf8.RuneCountInString(core.NormalizeChannelName(prefix))
		m.ed.SetText(norm)
		m.ed.SetCaret(n, n)
	}
	m.name, m.changed, m.last = norm, true, norm
	m.gen++
	gen := m.gen
	time.AfterFunc(rnDebounce, func() {
		post(m.h, func() {
			if gen != m.gen || m.modal.Dismissed() {
				return
			}
			name := m.name
			run(m.h, func(ctx context.Context) (string, error) {
				a := m.h.Core()
				if a == nil {
					return "", context.Canceled
				}
				return a.ValidateChannelName(ctx, name)
			}, func(_ string, err error) {
				if gen == m.gen {
					m.code = core.ChannelNameCode(err)
				}
			})
		})
	})
}

func (m *RenameChannelModal) layout(gtx layout.Context) layout.Dimensions {
	if !m.focused {
		m.focused = true // focusOnMount
		gtx.Execute(key.FocusCmd{Tag: &m.ed})
	}
	for {
		e, ok := m.ed.Update(gtx)
		if !ok {
			break
		}
		if _, ok := e.(widget.ChangeEvent); ok && m.ed.Text() != m.last {
			m.onEdit() // the editor reports SetText too: only a new text is an edit
		}
	}
	th := m.h.Theme()
	cl := colorsOf(th)
	w := gtx.Constraints.Max.X
	y := 0
	if m.err != "" { // the error InlineAlert, p-about_modal__section
		d := at(gtx, image.Pt(0, y), func(gtx layout.Context) layout.Dimensions {
			return renameAlert(gtx, th, cl, m.err, w)
		})
		y += d.Size.Y + dp(gtx, 16)
	}
	if e := m.NameError(); e != "" { // the label's warning, before its title
		d := at(gtx, image.Pt(0, y), func(gtx layout.Context) layout.Dimensions {
			return renameAlert(gtx, th, cl, e, w)
		})
		y += d.Size.Y + dp(gtx, 4)
	}
	lh := dp(gtx, 22)
	at(gtx, image.Pt(0, y), func(gtx layout.Context) layout.Dimensions {
		gtx.Constraints = layout.Constraints{Max: image.Pt(w, lh)}
		return vcenter(gtx, lh, func(gtx layout.Context) layout.Dimensions {
			return text(gtx, th, 15, theme.Bold, cl.Fg, nsChDetails.T(rnLabel))
		})
	})
	y += lh + dp(gtx, 8)
	ih := dp(gtx, 44) // c-input_text--large
	leading := "hash"
	if m.private {
		leading = "lock"
	}
	at(gtx, image.Pt(0, y), func(gtx layout.Context) layout.Dimensions {
		gtx.Constraints = layout.Exact(image.Pt(w, ih))
		d := field(gtx, th, &m.ed, nsCreateCh.T(rnPlaceholder), leading, 44, 18)
		// the characters left (c-input_character_count--large)
		left := core.MaxChannelNameLength - utf8.RuneCountInString(m.ed.Text())
		num := strconv.Itoa(left)
		col := cl.Fg3
		if left < 0 {
			col = ccErrColor(th, cl)
		}
		nw := textW(gtx, th, 18, theme.Regular, num)
		bx := w - dp(gtx, 12) - nw
		rrect(gtx, image.Rect(bx-dp(gtx, 6), dp(gtx, 2), w-dp(gtx, 2), ih-dp(gtx, 2)), dp(gtx, 6), cl.Bg)
		at(gtx, image.Pt(bx, 0), func(gtx layout.Context) layout.Dimensions {
			gtx.Constraints = layout.Constraints{Max: image.Pt(nw+dp(gtx, 2), ih)}
			return vcenter(gtx, ih, func(gtx layout.Context) layout.Dimensions {
				return text(gtx, th, 18, theme.Regular, col, num)
			})
		})
		return d
	})
	y += ih + dp(gtx, 4)
	hd := at(gtx, image.Pt(0, y), func(gtx layout.Context) layout.Dimensions {
		gtx.Constraints = layout.Constraints{Max: image.Pt(w, 1<<20)}
		return para(gtx, th, 13, 18, theme.Regular, cl.Fg2, strings.TrimSpace(nsCreateCh.T(rnHint)), 0)
	})
	y += hd.Size.Y
	return layout.Dimensions{Size: image.Pt(w, y)}
}
