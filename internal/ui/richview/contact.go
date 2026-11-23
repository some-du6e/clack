package richview

// A contact_card block: Slack's ContactCard (boot-shared zb3H) around
// SimpleContactCard (alkx; CDN build 9f450b8ad3db), from client-boot-
// styles:
//
//   - .c-pillow_file_container.p-contact_card__container: width 100%,
//     min-width 200px, max-width 328px, cursor pointer; the c-pillow_file
//     in it as a file's pillow draws (12px padding, the 1px frame);
//   - .c-pillow_file__header: the 36px avatar (Avatar medium, the
//     ConnectedBaseAvatar's 8px corners) with 12px after it, then
//     .c-pillow_file__description: the name (.c-pillow_file__title: 16px
//     bold, one line with an ellipsis) over the team
//     (.c-pillow_file__meta__entry: 13px, 4px under it): TeamName, not
//     bold, or BaseMissingTeam's bar while the team isn't loaded;
//   - .c-file__actions at top 12px, right 12px while the card is hovered:
//     Message (direct-messages), Share contact… (share-message) and Save
//     contact (user-directory-add) as richtext.ContactActions lists them;
//   - a click on the card opens the member's profile when the viewer can
//     interact with them or they're profile only (ContactCard's Q).
//
// Left out: user-directory-add draws Clack's user-add glyph.

import (
	"image"

	"gioui.org/font"

	"github.com/some-du6e/clack/internal/i18n"
	"github.com/some-du6e/clack/internal/richtext"
)

var nsAttachmentMedia = i18n.NS("attachment_media")

// ContactCardHandler is optional on a Handler: a contact card's action
// (act: "message", "share_contact" or "save_info") was clicked; btn is
// the button, in window coordinates when window, else in the view's plus
// Origin (the owner maps it).
type ContactCardHandler interface {
	ContactCardAction(act string, c *richtext.ContactCard, btn image.Rectangle, window bool)
}

// contactCard lays out a contact_card block.
func (l *lay) contactCard(b *richtext.Block, x, y, w int) int {
	cc := b.Contact
	if cc == nil {
		return 0
	}
	p := &l.st.Palette
	cw := min(w, max(l.dp(200), min(w, l.dp(328))))
	pad := l.dp(12)
	ic := l.dp(36)
	h := ic + 2*pad + l.dp(2)
	var tg int16 = -1
	if richtext.ContactOpensProfile(cc) {
		tg = l.d.addTarget(target{kind: tContact, id: cc.UserID})
	}
	l.cardFrame(x, y, cw, h, tg)
	ar := image.Rect(x+pad, y+pad, x+pad+ic, y+pad+ic)
	rad := l.dp(8)
	if cc.Member.Avatar != "" {
		l.d.imgs = append(l.d.imgs, imgPrim{r: ar, url: cc.Member.Avatar, radii: [4]int{rad, rad, rad, rad}, target: tg, px: ic, onFail: DefaultAvatar})
	} else {
		l.d.boxes = append(l.d.boxes, box{kind: bFill, r: ar, radius: rad, fill: p.Placeholder, target: tg})
	}
	tx := x + pad + ic + pad
	tw := x + cw - tx - pad
	fs := l.bodyStyle()
	fs.face.Weight = font.Bold
	fs.size, fs.lineH = l.sp(16), l.dp(20)
	fs.maxLines, fs.ellipsis = 1, true
	l.plainAdd("\n")
	l.plainFlow(cc.Member.Name, tx, y+pad-l.dp(1), tw, &fs, tg)
	my := y + pad + l.dp(20)
	if cc.Member.TeamUnknown {
		// BaseMissingTeam: the unknown team's bar (c-missing_team--unknown)
		bh := l.dp(13)
		l.d.boxes = append(l.d.boxes, box{kind: bFill, r: image.Rect(tx, my+l.dp(2), tx+min(tw, l.dp(120)), my+l.dp(2)+bh), radius: l.dp(4), fill: p.Placeholder, target: -1})
	} else {
		ms := l.small(p.Text2)
		ms.maxLines, ms.ellipsis = 1, true
		l.plainAdd("\n")
		l.plainFlow(cc.Member.Team, tx, my, tw, &ms, tg)
	}
	if acts := richtext.ContactActions(cc); len(acts) > 0 && !l.opts.NoHoverCards && !l.opts.Canvas {
		var btns []barBtn
		for _, a := range acts {
			bt := barBtn{cact: a, w: 32, h: 32}
			switch a {
			case "message":
				bt.icon, bt.tip = "direct-messages", nsAttachmentMedia.T("Message")
			case "share_contact":
				bt.icon, bt.tip = "share-message", nsAttachmentMedia.T("Share contact…")
			case "save_info":
				bt.icon, bt.tip = "user-add", nsAttachmentMedia.T("Save contact") // Slack's user-directory-add
			}
			btns = append(btns, bt)
		}
		l.d.bars = append(l.d.bars, fileBar{card: image.Rect(x, y, x+cw, y+h), contact: cc, inset: 12, group: true, btns: btns})
	}
	return h
}

// ContactCardRect is a laid out contact card (tests): its card and its
// actions' buttons, view-local.
type ContactCardRect struct {
	Card    *richtext.ContactCard
	R       image.Rectangle
	Actions map[string]image.Rectangle
}

// ContactCardRects lists the view's contact cards' bars (tests): only
// cards with actions have one.
func (v *View) ContactCardRects() []ContactCardRect {
	var out []ContactCardRect
	for i := range v.d.bars {
		b := &v.d.bars[i]
		if b.contact == nil {
			continue
		}
		_, rs := v.barRects(i)
		c := ContactCardRect{Card: b.contact, R: b.card, Actions: map[string]image.Rectangle{}}
		for k, bt := range b.btns {
			c.Actions[bt.cact] = rs[k]
		}
		out = append(out, c)
	}
	return out
}
