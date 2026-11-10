package popups

import (
	"gioui.org/layout"

	"github.com/some-du6e/clack/internal/ui/theme"
)

// FileUploadError is Slack's FileUploadErrorDialog after a message with
// files couldn't be sent (sendDraftFiles' catch: goToDrafts true): the
// placeholder is gone and the message and its files are back in a draft.
// Title "Couldn’t send message", the drafts copy, "Got it" and "See
// Drafts" (onDrafts opens Drafts & sent).
func FileUploadError(h Host, onDrafts func()) *Modal {
	th := h.Theme()
	m := NewModal(h, ModalOpts{
		Title:        "Couldn’t send message",
		EnterSubmits: true,
		Body: func(gtx layout.Context) layout.Dimensions {
			return para(gtx, th, 15, 22, theme.Regular, colorsOf(th).Fg,
				"Your message, along with any files and attachments, has been saved to your drafts.", 0)
		},
		Buttons: []*ModalButton{
			{Label: "See Drafts", Kind: BtnOutline, Do: func() bool {
				if onDrafts != nil {
					onDrafts()
				}
				return true
			}},
			{Label: "Got it", Kind: BtnPrimary, Do: func() bool { return true }},
		},
	})
	h.ShowOverlay(m)
	return m
}
