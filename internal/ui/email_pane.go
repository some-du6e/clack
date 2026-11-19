package ui

import (
	"context"
	"image"
	"strings"
	"time"

	"gioui.org/layout"
	"gioui.org/widget"

	"github.com/some-du6e/clack/internal/i18n"
	"github.com/some-du6e/clack/internal/richtext"
	"github.com/some-du6e/clack/internal/ui/messages"
	"github.com/some-du6e/clack/internal/ui/popups"
	"github.com/some-du6e/clack/internal/ui/richview"
	"github.com/some-du6e/clack/internal/ui/theme"
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
//
// The references footer (FileActions with isFileDetails: Share, Copy
// link and the rest as outline buttons) and the Shared in section aren't
// drawn yet.

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
	atExact(gtx, image.Pt(0, hh), image.Pt(sz.X, sz.Y-hh), func(gtx layout.Context) layout.Dimensions {
		return p.list.Layout(gtx, 1, func(gtx layout.Context, _ int) layout.Dimensions {
			gtx.Constraints.Min.X = gtx.Constraints.Max.X
			return p.view.Layout(gtx, messages.RichStyle(a), emailPaneHandler{a})
		})
	})
	return layout.Dimensions{Size: sz}
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
