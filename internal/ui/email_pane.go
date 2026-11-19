package ui

import (
	"context"
	"image"
	"strings"
	"time"

	"gioui.org/layout"
	"gioui.org/widget"

	"github.com/some-du6e/clack/internal/core"
	"github.com/some-du6e/clack/internal/i18n"
	"github.com/some-du6e/clack/internal/richtext"
	"github.com/some-du6e/clack/internal/ui/messages"
	"github.com/some-du6e/clack/internal/ui/popups"
	"github.com/some-du6e/clack/internal/ui/richview"
	"github.com/some-du6e/clack/internal/ui/theme"
	"github.com/some-du6e/clack/internal/ui/views"
)

// An email file's FileDetails in the secondary view (boot-shared eg5B,
// FkQ0; new_email_rendering, on for Hack Club): the email's header
// (MessageFileHeader's onEmailHeaderClick, toFileDetails) and its View
// file details open it.
//
//   - FileDetails' flexpane (p-file_flexpane--detail): the title "Email"
//     (titleContent for an email under new_email_rendering) and the close
//     button; the body scrolls.
//   - EmailFileDetailsContent: the file entity with the email's time,
//     then From, To, Cc, Body (the whole email: files.info's
//     simplified_html, else the message's preview) and Attachments
//     (richview's EmailDetails, email.go).
//   - A link in the body asks first when it is http(s)
//     (openLinkRedirectModal's "Open Link"); a mailto opens.
//   - Under the email, the Shared in section (renderSharedInSection,
//     2TKv FileShareContext: views.FileShares).
//   - The flexpane's footer (.p-flexpane__footer: a top rule, 16px 20px
//     in, its buttons 8px apart) is renderReferencesFooter, FileActions
//     with isFileDetails (v1+y): for an email only renderShare(true),
//     "Share File…" with share-message as an outline button that fills
//     the row (display_flex flex_one: the icon 8px before the label), and
//     More actions, ellipsis-vertical-filled at 20 in a 36px button
//     (p-file_details__references_more_actions). Share opens the share
//     dialog and More actions the file's menu, as the message's
//     FileActions do (messages' FileAction). Not ported: the
//     showDownloadAndShareActions gate (enable_new_channel_settings'
//     channel download settings), as the message's FileActions.

var emailDetailsNS = i18n.NS("file-details")

// emailPaneTitle is FileDetails' titleContent for an email.
func emailPaneTitle() string { return emailDetailsNS.T("Email") }

var _ messages.EmailOpener = (*App)(nil)

// OpenEmailFile opens email file f, shared in message ch/ts, in the
// secondary view (messages.EmailOpener).
func (a *App) OpenEmailFile(f *richtext.File, ch, ts string) {
	if f == nil || f.Email == nil || f.ID == "" {
		return
	}
	if p, ok := a.secondary.view.(*emailPane); ok && a.secondary.kind == secEmail && a.secondary.open() && p.file().ID == f.ID {
		return
	}
	p := &emailPane{a: a, msg: f, ch: ch, ts: ts}
	p.list.Axis = layout.Vertical
	a.secondary = secondaryState{kind: secEmail, channel: ch, ts: ts, width: a.secondary.width}
	a.secondary.view = p
	if c := a.Core(); c != nil {
		go func() { // the whole email (fetchAndUpsertFileById)
			ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
			defer cancel()
			sf, err := c.EmailFile(ctx, f.ID)
			if err != nil || sf == nil {
				return
			}
			full := richtext.EmailFile(sf, c.S)
			a.Post(func() {
				if full.Email != nil {
					p.full = full
					p.view = nil
				}
				a.Invalidate()
			})
		}()
	}
	a.Invalidate()
}

// emailPane is FileDetails for an email.
type emailPane struct {
	a      *App
	msg    *richtext.File // the message's copy
	full   *richtext.File // files.info's, once read
	ch, ts string

	close widget.Clickable
	list  layout.List
	view  *richview.View

	shares      *views.FileShares // the Shared in section
	share, more widget.Clickable  // the footer's buttons
	moreTip     popups.TipState
	org         originTracker   // the pane's window origin
	foot        image.Point     // the footer in the pane
	shareR      image.Rectangle // the footer's buttons in the window
	moreR       image.Rectangle
}

func (p *emailPane) file() *richtext.File {
	if p.full != nil {
		return p.full
	}
	return p.msg
}

func (p *emailPane) Layout(gtx layout.Context) layout.Dimensions {
	a := p.a
	th := a.th
	c := &th.C
	sz := gtx.Constraints.Max
	hh := gtx.Dp(theme.HeaderH)
	bs := gtx.Dp(28)
	cx := sz.X - gtx.Dp(8) - bs
	tx := gtx.Dp(20)
	title := emailPaneTitle()
	tw := min(th.Measure(gtx, 18, theme.Black, title), cx-gtx.Dp(8)-tx)
	at(gtx, image.Pt(tx, 0), image.Pt(tw+1, hh), func(gtx layout.Context) layout.Dimensions {
		return vcenter(gtx, hh, func(gtx layout.Context) layout.Dimensions {
			return th.Label(gtx, 18, theme.Black, c.Fg, title)
		})
	})
	at(gtx, image.Pt(cx, (hh-bs)/2), image.Pt(bs, bs), func(gtx layout.Context) layout.Dimensions {
		return iconButton(gtx, &p.close, "close", 28, 18, c.FgMuted, c.BgHover, 8)
	})
	if clicked(&p.close) {
		a.CloseSecondary()
		return layout.Dimensions{Size: sz}
	}
	if p.view == nil {
		doc := richtext.Doc{Blocks: []richtext.Block{{Kind: richtext.FileBlock, File: p.file()}}}
		p.view = richview.New(doc, richview.Options{EmailDetails: true, CompactFiles: true, Channel: p.ch, TS: p.ts})
	}
	if p.shares == nil {
		p.shares = views.NewFileShares(a, p.file().ID)
	}
	fh := 2*gtx.Dp(16) + gtx.Dp(36) + max(1, gtx.Dp(1)) // the footer
	bh := max(sz.Y-hh-fh, 0)
	atExact(gtx, image.Pt(0, hh), image.Pt(sz.X, bh), func(gtx layout.Context) layout.Dimensions {
		return p.list.Layout(gtx, 2, func(gtx layout.Context, i int) layout.Dimensions {
			gtx.Constraints.Min.X = gtx.Constraints.Max.X
			if i == 1 {
				return p.shares.Layout(gtx, a.winSize.Y)
			}
			return p.view.Layout(gtx, messages.RichStyle(a), emailPaneHandler{a})
		})
	})
	p.foot = image.Pt(0, hh+bh)
	at(gtx, p.foot, image.Pt(sz.X, fh), func(gtx layout.Context) layout.Dimensions { return p.footer(gtx, sz) })
	p.org.track(gtx, a) // last: over everything (pass-through), for the menu's anchor
	return layout.Dimensions{Size: sz}
}

// emailShareLabel is getShareLabel(true) for an email file.
func emailShareLabel() string { return i18n.T("file", "Share File\u2026") }

// footer is the flexpane's footer: the top rule, then Share File… filling
// the row and More actions.
func (p *emailPane) footer(gtx layout.Context, pane image.Point) layout.Dimensions {
	a := p.a
	th := a.th
	w := gtx.Constraints.Max.X
	share := p.share.Clicked(gtx) // popups.Button's Layout drains its clicks
	// border-top
	theme.Fill(gtx, image.Rect(0, 0, w, max(1, gtx.Dp(1))), theme.OtlTer(th))
	y := max(1, gtx.Dp(1)) + gtx.Dp(16)
	x, bh := gtx.Dp(20), gtx.Dp(36)
	mx := w - gtx.Dp(20) - bh // More actions at the right
	sw := max(mx-gtx.Dp(8)-x, 0)
	at(gtx, image.Pt(x, y), image.Pt(sw, bh), func(gtx layout.Context) layout.Dimensions {
		gtx.Constraints.Min.X = sw
		return popups.Button(gtx, th, &p.share, popups.BtnOutline, popups.BtnMedium, emailShareLabel(), "share-message", float32(sw)/gtx.Metric.PxPerDp)
	})
	at(gtx, image.Pt(mx, y), image.Pt(bh, bh), func(gtx layout.Context) layout.Dimensions {
		d := iconButton(gtx, &p.more, "ellipsis-vertical-filled", 36, 20, th.C.FgMuted, th.C.BgHover, 8)
		p.moreTip.Delay = 60 * time.Millisecond
		popups.Tooltip(gtx, th, &p.moreTip, p.more.Hovered(), d.Size, popups.Above, i18n.T("file", "More actions"), "")
		return d
	})
	// where they are in the window (the pane sits at the window's right
	// until the pointer has crossed it)
	org := p.org.get(image.Pt(a.winSize.X-pane.X, a.winSize.Y-pane.Y)).Add(p.foot)
	p.shareR = image.Rect(x, y, x+sw, y+bh).Add(org)
	p.moreR = image.Rect(mx, y, mx+bh, y+bh).Add(org)
	if share {
		if m, ok := p.message(); ok {
			a.ShowOverlay(popups.NewForwardModal(a, m))
		}
	}
	if clicked(&p.more) {
		if m, ok := p.message(); ok {
			if mn := popups.NewFileActionsMenu(a, p.moreR, m, p.file(), false); mn != nil {
				a.ShowOverlay(mn)
			}
		}
	}
	return layout.Dimensions{Size: image.Pt(w, gtx.Constraints.Max.Y)}
}

// message is the message the email was shared in.
func (p *emailPane) message() (core.Message, bool) {
	c := p.a.Core()
	if c == nil {
		return core.Message{}, false
	}
	m, ok := c.S.Find(p.ch, p.ts)
	if !ok {
		return core.Message{}, false
	}
	return c.MessageFromSlack(p.ch, &m), true
}

// emailPaneHandler is the pane's links: members open their profile, an
// address mails it, the body's links ask first (EmailLinkHandler).
type emailPaneHandler struct{ a *App }

func (h emailPaneHandler) OpenURL(u string)                       { openURL(u) }
func (h emailPaneHandler) OpenUser(id string, anchor image.Point) { h.a.OpenProfile(anchor, id) }
func (h emailPaneHandler) OpenChannel(id string)                  { h.a.Navigate("c/" + id) }
func (h emailPaneHandler) OpenUsergroup(string, image.Point)      {}
func (h emailPaneHandler) OpenImage(img richtext.Image)           { openURL(img.URL) }
func (h emailPaneHandler) OpenFile(f *richtext.File)              { openURL(firstNonEmptyStr(f.Permalink, f.URL)) }
func (h emailPaneHandler) Button(b richtext.Button) {
	if b.URL != "" {
		openURL(b.URL)
	}
}
func (h emailPaneHandler) OpenEmailLink(u string) { h.a.OpenEmailLink(u) }

// OpenEmailLink is EmailIframe's link click: an http(s) link opens
// LinkRedirectModal (wwJm: "Open Link", the URL, Cancel and Open Link),
// any other opens (window.open).
func (a *App) OpenEmailLink(u string) {
	lu := strings.ToLower(u)
	if !strings.HasPrefix(lu, "http://") && !strings.HasPrefix(lu, "https://") {
		openURL(u)
		return
	}
	ns := i18n.NS("hide-user")
	th := a.th
	popups.Confirm(a, ns.T("Open Link"), ns.T("You\u2019re opening a link you received in an email. Make sure the following URL is where you want to go."),
		ns.T("Open Link"), false, func(gtx layout.Context) layout.Dimensions {
			// c-pillow_file__link_redirect_link: the URL under the text
			return th.Layout(gtx, theme.TextStyle{Size: 15, Weight: theme.Bold, Color: th.C.Fg, LineHeight: 22}, u)
		}, func() { openURL(u) })
}
