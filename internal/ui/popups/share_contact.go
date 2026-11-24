package popups

// ShareContactModal (jz7l; CDN build 9f450b8ad3db) on the share dialog
// (actions_forward.go): the title "Share contact", the explanation over
// the picker ("When you share someone’s contact info, …", margin-bottom
// 16px), one destination, the note ("Add a message, if you’d like.") and
// under it the card (SimpleContactCard in a c-pillow_file_container, no
// actions); Share opens the conversation (openOrCreate for a person),
// posts the card with the note (core.ShareContact) and navigates there;
// while it runs the button is loading, and a failure keeps the modal open
// with the error Alert over the picker ("Sorry, something went wrong.
// Try again in a moment.").

import (
	"context"
	"image"
	"strings"

	"gioui.org/layout"

	"github.com/some-du6e/clack/internal/richtext"
	"github.com/some-du6e/clack/internal/ui/richview"
	"github.com/some-du6e/clack/internal/ui/theme"
)

func newShareContact(h Host, c *richtext.ContactCard) *actForward {
	f := &actForward{h: h, focusTo: true, hov: -1, contact: c}
	f.to.SingleLine = true
	f.to.Submit = true
	f.sugList.Axis = layout.Vertical
	f.all = actFwdTargets(h)
	// the preview: the card without its id (no actions) and no click
	pc := *c
	pc.CardID = ""
	pc.Member.CanInteract, pc.Member.ProfileOnly = false, false
	f.contactPreview = richview.New(richtext.Doc{Blocks: []richtext.Block{{Kind: richtext.ContactCardBlock, Kit: true, Contact: &pc}}}, richview.Options{NoHoverCards: true})
	v2 := func() bool { return expOn(h, ExpSlackKitV2Button) }
	f.fwd = &ModalButton{Label: nsShareContact.T("Share"), Kind: BtnPrimary, Enabled: func() bool { return f.canGo() && !f.contactBusy }, V2: v2,
		Loading: func() bool { return f.contactBusy }, Do: func() bool {
			f.shareContact()
			return false
		}}
	f.modal = NewModal(h, ModalOpts{
		TitleFunc: f.title,
		Body:      f.layout,
		Buttons:   []*ModalButton{f.fwd},
		FooterPad: 24,
	})
	return f
}

// contactIntro is the modal's first paragraph, and the error Alert after
// a failed Share.
func (f *actForward) contactIntro(gtx layout.Context, th *theme.Theme, cl *Colors) layout.Dimensions {
	s := nsShareContact.T("When you share someone’s contact info, others can message them directly — even if they’re from another company. {name} will see you’ve introduced them.", "name", f.contact.Member.Name)
	w := gtx.Constraints.Max.X
	d := para(gtx, th, 15, 22, theme.Regular, cl.Fg, s, 0)
	y := d.Size.Y + dp(gtx, 16)
	if f.contactErr {
		y += actAlert(gtx, th, cl, 0, y, w, nsShareContact.T("Sorry, something went wrong. Try again in a moment."))
	}
	return layout.Dimensions{Size: image.Pt(w, y)}
}

// contactCardPreview draws the card under the note.
func (f *actForward) contactCardPreview(gtx layout.Context) layout.Dimensions {
	if RichStyle == nil || f.contactPreview == nil {
		return layout.Dimensions{}
	}
	st := RichStyle(f.h)
	if st == nil {
		return layout.Dimensions{}
	}
	return f.contactPreview.Layout(gtx, st, topicHandler{f.h})
}

// shareContact is Share: the conversation (a person's DM opened first),
// the card posted there with the note, then the conversation.
func (f *actForward) shareContact() {
	if len(f.chips) == 0 {
		return
	}
	dest, user := f.chips[0].t.id, f.contact.UserID
	note := strings.TrimSpace(f.note.Text())
	a := f.h.Core()
	if a == nil || f.contactBusy {
		return
	}
	f.contactBusy, f.contactErr = true, false
	run(f.h, func(ctx context.Context) (string, error) {
		ch := dest
		if strings.HasPrefix(dest, "U") || strings.HasPrefix(dest, "W") {
			cv, err := a.OpenDM(ctx, dest)
			if err != nil {
				return "", err
			}
			ch = cv.ID
		}
		_, err := a.ShareContact(ctx, ch, user, note)
		return ch, err
	}, func(ch string, err error) {
		f.contactBusy = false
		if err != nil {
			f.contactErr = true
			f.h.Invalidate()
			return
		}
		f.modal.Dismiss()
		f.h.Navigate("c/" + ch)
	})
}
