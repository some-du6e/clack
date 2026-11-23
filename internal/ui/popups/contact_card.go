package popups

// A contact_card block's actions (Slack's ContactCard, boot-shared zb3H;
// ShareContactModal in jz7l; CDN build 9f450b8ad3db):
//
//   - Message (contact_card_message): the DM with the member
//     (startImByMemberId) when the viewer can interact with them, else
//     contacts.add accepts the card first;
//   - Save contact (contact_card_save_info): contacts.add, then "Saved
//     contact" (SaveContactModal) unless its "Don't show me this again"
//     was ticked before (users.interactions SLACK_CONNECT_SAVE_CONTACT_
//     MODAL / DISMISS);
//   - either one failing: ContactActionErrorModal, "Something’s gone
//     wrong…" with "If the problem persists, please drop us a line.", or
//     for cannot_add_contact_card, user_is_blocked,
//     invalid_user_combination and user_not_found its sentence for the
//     action;
//   - Share contact…: ShareContactModal, the share dialog's picker with
//     the explanation over it, the note ("Add a message, if you’d like.")
//     and the card under it; Share posts the card (core.ShareContact) to
//     the conversation and opens it.
//
// Left out: the error modal's title and text are left-aligned (Slack
// centres them).

import (
	"context"
	"image"
	"strings"

	"gioui.org/layout"
	"gioui.org/widget"

	"github.com/some-du6e/clack/internal/core"
	"github.com/some-du6e/clack/internal/i18n"
	"github.com/some-du6e/clack/internal/richtext"
	"github.com/some-du6e/clack/internal/ui/richview"
	"github.com/some-du6e/clack/internal/ui/theme"
)

var (
	nsIntroductions = i18n.NS("introductions")
	nsSaveContact   = i18n.NS("slack_connect_save_contact")
	nsShareContact  = i18n.NS("share-contact")
)

// ContactCardAction runs a contact card's action act ("message",
// "share_contact", "save_info").
func ContactCardAction(h Host, act string, c *richtext.ContactCard) {
	if c == nil {
		return
	}
	a := h.Core()
	if a == nil {
		return
	}
	switch act {
	case "message":
		if c.Member.CanInteract {
			pfMessage(h, c.UserID)
			return
		}
		run(h, func(ctx context.Context) (struct{}, error) {
			return struct{}{}, a.AddContact(ctx, c.CardID, "contact_card_message")
		}, func(_ struct{}, err error) {
			if err != nil {
				h.ShowOverlay(NewContactErrorModal(h, c, act, core.ErrCode(err)))
				return
			}
			pfMessage(h, c.UserID)
		})
	case "save_info":
		run(h, func(ctx context.Context) (bool, error) {
			if err := a.AddContact(ctx, c.CardID, "contact_card_save_info"); err != nil {
				return false, err
			}
			// the modal only when its dismissal isn't recorded (and the
			// interactions loaded: a failed read shows nothing, as Slack's
			// loading flag stays up)
			ints, err := a.UserInteractions(ctx, [2]string{core.SaveContactModalComp, core.InteractionDismiss})
			if err != nil {
				return false, nil
			}
			_, dismissed := ints[core.InteractionKey(core.SaveContactModalComp, core.InteractionDismiss, "")]
			return !dismissed, nil
		}, func(show bool, err error) {
			if err != nil {
				h.ShowOverlay(NewContactErrorModal(h, c, act, core.ErrCode(err)))
				return
			}
			if show {
				h.ShowOverlay(NewSaveContactModal(h, c))
			}
		})
	case "share_contact":
		OpenShareContact(h, c)
	}
}

// contactErrorText is ContactActionErrorModal's sentence for act and the
// API's error (nil: the default with its link).
func contactErrorText(c *richtext.ContactCard, act, code string) []richtext.Span {
	member := c.Member.Name
	switch code {
	case core.ContactCannotAddCard, core.ContactUserIsBlocked, core.ContactInvalidCombo, core.ContactUserNotFound:
		switch act {
		case "save_info":
			return []richtext.Span{{Kind: richtext.Text, Text: nsIntroductions.T("So you can’t save {member}’s contact info. But you can DM them if you are both in the same channel.", "member", member)}}
		case "message":
			return []richtext.Span{{Kind: richtext.Text, Text: nsIntroductions.T("You need to be in a channel with {member} to message them.", "member", member)}}
		}
	}
	// "If the problem persists, please <a>drop us a line.</a>"
	before, link, after := i18nLinkParts(nsIntroductions.T("If the problem persists, please <a>drop us a line.</a>"))
	return []richtext.Span{{Kind: richtext.Text, Text: before}, {Kind: richtext.Link, Text: link, URL: "https://slack.com/help/contact"}, {Kind: richtext.Text, Text: after}}
}

// i18nLinkParts splits s around its <a>…</a>.
func i18nLinkParts(s string) (before, link, after string) {
	before, rest, ok := strings.Cut(s, "<a>")
	if !ok {
		return s, "", ""
	}
	link, after, _ = strings.Cut(rest, "</a>")
	return before, link, after
}

// NewContactErrorModal is ContactActionErrorModal for act failing with
// code.
func NewContactErrorModal(h Host, c *richtext.ContactCard, act, code string) *Modal {
	v := richview.New(richtext.Doc{Blocks: []richtext.Block{{Kind: richtext.Paragraph, Spans: contactErrorText(c, act, code)}}}, richview.Options{NoHoverCards: true})
	return NewModal(h, ModalOpts{Title: nsIntroductions.T("Something’s gone wrong…"), Body: func(gtx layout.Context) layout.Dimensions {
		if RichStyle == nil {
			return layout.Dimensions{}
		}
		st := RichStyle(h)
		if st == nil {
			return layout.Dimensions{}
		}
		return v.Layout(gtx, st, topicHandler{h})
	}, Buttons: []*ModalButton{{Label: nsIntroductions.T("Got It"), Kind: BtnOutline, Do: func() bool { return true }}}})
}

// saveContact is SaveContactModal's state: its checkbox.
type saveContact struct {
	h     Host
	c     *richtext.ContactCard
	never bool
}

// NewSaveContactModal is "Saved contact": the sentence, and in the footer
// the checkbox "Don’t show me this again" (left) and Got It, which records
// the dismissal when it's ticked.
func NewSaveContactModal(h Host, c *richtext.ContactCard) *Modal {
	s := &saveContact{h: h, c: c}
	check := &ModalButton{Label: nsSaveContact.T("Don’t show me this again"), Left: true, Draw: s.drawCheck, Do: func() bool {
		s.never = !s.never
		return false
	}}
	return NewModal(h, ModalOpts{Title: nsSaveContact.T("Saved contact"), Body: s.layout, Buttons: []*ModalButton{
		check, {Label: nsSaveContact.T("Got It"), Kind: BtnOutline, Do: s.gotIt},
	}})
}

func (s *saveContact) gotIt() bool {
	if a := s.h.Core(); a != nil && s.never {
		run(s.h, func(ctx context.Context) (struct{}, error) {
			return struct{}{}, a.RecordUserInteraction(ctx, core.SaveContactModalComp, core.InteractionDismiss)
		}, func(struct{}, error) {})
	}
	return true
}

// drawCheck is the footer's checkbox with its label inline after it
// (.p-save_contact_modal__footer_actions--label: CheckboxLabel inline).
func (s *saveContact) drawCheck(gtx layout.Context, th *theme.Theme, c *widget.Clickable, kind BtnKind, ring bool, grp grpSide) layout.Dimensions {
	cl := colorsOf(th)
	label := nsSaveContact.T("Don’t show me this again")
	h := dp(gtx, 36)
	return c.Layout(gtx, func(gtx layout.Context) layout.Dimensions {
		cb := dp(gtx, 16)
		at(gtx, image.Pt(0, (h-cb)/2), func(gtx layout.Context) layout.Dimensions {
			return Checkbox(gtx, th, s.never, false, false)
		})
		tx := cb + dp(gtx, 8)
		at(gtx, image.Pt(tx, 0), func(gtx layout.Context) layout.Dimensions {
			return vcenter(gtx, h, func(gtx layout.Context) layout.Dimensions {
				return text(gtx, th, 15, theme.Regular, cl.Fg, label)
			})
		})
		sz := image.Pt(tx+textW(gtx, th, 15, theme.Regular, label), h)
		if ring {
			focusRing(gtx, cl, image.Rectangle{Max: sz}, dp(gtx, 4))
		}
		pointerCursor(gtx, sz)
		return layout.Dimensions{Size: sz}
	})
}

// layout is the modal's body: "Now you can message {name} whenever you’d
// like — simply click compose and type their name." (margin-bottom 16px).
func (s *saveContact) layout(gtx layout.Context) layout.Dimensions {
	th := s.h.Theme()
	cl := colorsOf(th)
	msg := nsSaveContact.T("Now you can message {name} whenever you’d like — simply click <strong>compose</strong> and type their name.", "name", s.c.Member.Name)
	msg = strings.NewReplacer("<strong>", "", "</strong>", "").Replace(msg)
	d := para(gtx, th, 15, 22, theme.Regular, cl.Fg, msg, 0)
	return layout.Dimensions{Size: image.Pt(gtx.Constraints.Max.X, d.Size.Y+dp(gtx, 16))}
}

// OpenShareContact is ShareContactModal for card c's member.
func OpenShareContact(h Host, c *richtext.ContactCard) {
	h.ShowOverlay(newShareContact(h, c).modal)
}
