package popups

import (
	"context"
	"image"

	"gioui.org/layout"
	"gioui.org/widget"

	"github.com/some-du6e/clack/internal/core"
	"github.com/some-du6e/clack/internal/ui/openurl"
	"github.com/some-du6e/clack/internal/ui/theme"
)

// Dialogs Slack shows behind a "Don’t …again" checkbox that writes a
// users.prefs boolean (setUserPrefByApi): Unsend message
// (should_show_unsend_message_confirmation) and "Set yourself to active?"
// (confirm_user_marked_away).

// prefCheckRow is the dialog's checkbox row: on reads the box, toggle
// flips it.
func prefCheckRow(h Host, btn *widget.Clickable, label string, on func() bool) layout.Widget {
	return func(gtx layout.Context) layout.Dimensions {
		th := h.Theme()
		cl := colorsOf(th)
		return btn.Layout(gtx, func(gtx layout.Context) layout.Dimensions {
			bx := Checkbox(gtx, th, on(), false, false)
			lw := at(gtx, image.Pt(bx.Size.X+dp(gtx, 8), 0), func(gtx layout.Context) layout.Dimensions {
				return text(gtx, th, 15, theme.Regular, cl.Fg, label)
			})
			pointerCursor(gtx, image.Pt(bx.Size.X+dp(gtx, 8)+lw.Size.X, max(bx.Size.Y, lw.Size.Y)))
			return layout.Dimensions{Size: image.Pt(bx.Size.X+dp(gtx, 8)+lw.Size.X, max(bx.Size.Y, lw.Size.Y))}
		})
	}
}

// setPrefAsync writes one users.prefs key, toasting a failure.
func setPrefAsync(h Host, name string, v any) {
	c := h.Core()
	if c == nil {
		return
	}
	run(h, func(ctx context.Context) (struct{}, error) { return struct{}{}, c.SetPref(ctx, name, v) },
		func(_ struct{}, err error) {
			if err != nil {
				h.Toast("Couldn't save that preference: " + errText(err))
			}
		})
}

// UnsendHelp is the dialog's "Learn more" (Edit or delete messages,
// #unsend-a-message).
const UnsendHelp = "https://slack.com/help/articles/202395258-Edit-or-delete-messages#unsend-a-message"

// ConfirmUnsend is UndoSendLastMessage (msg_edit): "Unsend message", the
// explanation and Learn more, the file note, the message, "Don’t show me
// this again" (unchecking keeps should_show_unsend_message_confirmation
// on), Cancel / Unsend (danger).
func ConfirmUnsend(h Host, m core.Message, onOK func()) *Modal {
	th := h.Theme()
	var dontShow bool
	var box, learn widget.Clickable
	note := unsendFileNote(m)
	return Confirm(h, "Unsend message", "", "Unsend", true, func(gtx layout.Context) layout.Dimensions {
		cl := colorsOf(th)
		if box.Clicked(gtx) {
			dontShow = !dontShow
			setPrefAsync(h, "should_show_unsend_message_confirmation", !dontShow)
		}
		if learn.Clicked(gtx) {
			if err := openurl.Open(UnsendHelp); err != nil {
				h.Toast("Couldn't open the link: " + err.Error())
			}
		}
		return layout.Flex{Axis: layout.Vertical}.Layout(gtx,
			layout.Rigid(func(gtx layout.Context) layout.Dimensions {
				return para(gtx, th, 15, 22, theme.Regular, cl.Fg, "After sending a message, you have a few moments to cancel it. This will delete your original message and drop it back in the composer for you.", 0)
			}),
			layout.Rigid(func(gtx layout.Context) layout.Dimensions {
				return learn.Layout(gtx, func(gtx layout.Context) layout.Dimensions {
					d := text(gtx, th, 15, theme.Regular, cl.Link, "Learn more")
					pointerCursor(gtx, d.Size)
					return d
				})
			}),
			layout.Rigid(func(gtx layout.Context) layout.Dimensions {
				if note == "" {
					return layout.Dimensions{}
				}
				return layout.Inset{Top: 8}.Layout(gtx, func(gtx layout.Context) layout.Dimensions {
					return para(gtx, th, 15, 22, theme.Regular, cl.Fg, note, 0)
				})
			}),
			layout.Rigid(func(gtx layout.Context) layout.Dimensions {
				return layout.Inset{Top: 16}.Layout(gtx, func(gtx layout.Context) layout.Dimensions {
					return actMsgPreview(gtx, h, m, true)
				})
			}),
			layout.Rigid(func(gtx layout.Context) layout.Dimensions {
				return layout.Inset{Top: 16}.Layout(gtx, prefCheckRow(h, &box, "Don’t show me this again", func() bool { return dontShow }))
			}),
		)
	}, onOK)
}

// unsendFileNote is getBodyText: what deleting the message leaves behind
// (by its subtype; a plain message has none).
func unsendFileNote(m core.Message) string {
	switch m.Subtype {
	case "file_upload":
		return "Note that deleting this message will not delete the file that was uploaded."
	case "file_share":
		return "Note that deleting this message will not unshare the file."
	case "file_comment":
		return "Note that deleting this message will not delete the comment."
	}
	return ""
}

// ConfirmAway is PresencePromptDialog (presence): shown on connecting
// while the viewer is manually away and confirm_user_marked_away is on.
// "Don’t ask again" writes confirm_user_marked_away=false (checked =
// !pref); No Thanks / Set to Active (toggleUserPresence).
func ConfirmAway(h Host, setActive func()) *Modal {
	var dontAsk bool
	var box widget.Clickable
	return confirm2(h, "Set yourself to active?", "You’re currently set to away, but it looks like you’re back. Want to update your availability?",
		"No Thanks", "Set to Active", func(gtx layout.Context) layout.Dimensions {
			if box.Clicked(gtx) {
				dontAsk = !dontAsk
				setPrefAsync(h, "confirm_user_marked_away", !dontAsk)
			}
			return prefCheckRow(h, &box, "Don’t ask again", func() bool { return dontAsk })(gtx)
		}, setActive)
}

// confirm2 is Confirm with Slack's own cancel label.
func confirm2(h Host, title, textBody, cancel, action string, body layout.Widget, onOK func()) *Modal {
	th := h.Theme()
	m := NewModal(h, ModalOpts{
		Title:        title,
		EnterSubmits: true,
		Body: func(gtx layout.Context) layout.Dimensions {
			cl := colorsOf(th)
			return layout.Flex{Axis: layout.Vertical}.Layout(gtx,
				layout.Rigid(func(gtx layout.Context) layout.Dimensions {
					return para(gtx, th, 15, 22, theme.Regular, cl.Fg, textBody, 0)
				}),
				layout.Rigid(func(gtx layout.Context) layout.Dimensions {
					return layout.Inset{Top: 16}.Layout(gtx, body)
				}),
			)
		},
		Buttons: []*ModalButton{
			{Label: cancel, Kind: BtnOutline},
			{Label: action, Kind: BtnPrimary, Do: func() bool {
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

// ConfirmPrefModal is a confirmation whose "Don’t show …" box is the
// negation of the boolean pref, written as it changes (Slack's
// HuddleShoutyRoosterModal pattern).
func ConfirmPrefModal(h Host, title, body, cancel, action, dontShow, pref string, onOK func()) *Modal {
	var off bool
	var box widget.Clickable
	return confirm2(h, title, body, cancel, action, func(gtx layout.Context) layout.Dimensions {
		if box.Clicked(gtx) {
			off = !off
			setPrefAsync(h, pref, !off)
		}
		return prefCheckRow(h, &box, dontShow, func() bool { return off })(gtx)
	}, onOK)
}

// PressDefault presses the modal's last enabled button (what Enter does
// with EnterSubmits): the confirmation's action.
func (m *Modal) PressDefault() {
	for i := len(m.o.Buttons) - 1; i >= 0; i-- {
		if b := m.o.Buttons[i]; b.enabled() {
			m.press(b)
			return
		}
	}
}
