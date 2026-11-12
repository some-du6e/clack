package ui

import (
	"context"
	"image"
	"image/color"
	"time"

	"gioui.org/layout"
	"gioui.org/op"
	"gioui.org/text"
	"gioui.org/widget"

	"github.com/some-du6e/clack/internal/core"
	"github.com/some-du6e/clack/internal/i18n"
	"github.com/some-du6e/clack/internal/ui/icons"
	"github.com/some-du6e/clack/internal/ui/popups"
	"github.com/some-du6e/clack/internal/ui/slackc/css"
	"github.com/some-du6e/clack/internal/ui/theme"
)

// Preferences > Connected accounts > "Apps connected through workflows":
// each app's row and its Manage button, the manage-accounts modal and the
// disconnect confirmation. Ported from Slack's web client (build
// d3de3a487b1c, boot-shared modules named below):
//
//   - d40N ConnectedWorkflowAccount: the row for one app id, cta "Manage"
//     (aria-label "Manage {appName}"); its onClick opens
//     manage_accounts_modal (isStackable). From Preferences the row gets
//     only the app id, so the modal's other props (target provider,
//     disconnect URL, multi-auth, search account, GBP) are unset.
//   - BcGa ConnectedWorkflowAccountAppRow: a 32px app icon (12px gap), the
//     name (an h3, 15px bold in .p-prefs_dialog) with a check-circle-filled
//     in content-hgl-2 (aria-label "Connected"), and an outline medium
//     button; rows 8px apart (.p-connected_workflow_accounts_list--dwa).
//   - H7Jb ConnectedWorkflowAccountManageModal: a title bar with the 32px
//     icon and the app's name (padding 16px 28px), then one section per
//     provider (lsX5.iN, in the response's order), "Connected
//     {providerName} Accounts" (an h3: 18px black), each valid token's
//     external_user_id (an h4, 15px, ellipsized) with the check, "Added by
//     your administrator" under a domain-wide one (KYUQ: dwa_user,
//     dwa_app), which has no Disconnect; the others an outline medium
//     Disconnect 16px to the right. Domain-wide rows and sections come
//     first. No section: "No accounts found". Footer: Done (primary).
//     The --dwa modifier: section headers 20px above the rows, rows 16px
//     apart, sections flush.
//   - j8Vt ConnectedWorkflowAccountConfirmationModal on iIWJ
//     ConfirmationModal: a centered title (the app's 64px icon 64px down
//     and 20px above "Are you sure you want to disconnect this
//     {providerName} account?", then the account at 50%), WorkflowAlert (a
//     default boxed c-alert without its icon: "What will change:" and three
//     disable-icon items, then the 12px note), Cancel and a danger
//     LoadingButton "Disconnect". Submit runs deleteUserAuth (F/EF:
//     apps.auth.external.delete, then resetAppAuths and fetchUserAuth); an
//     error shows "Something went wrong. Please try again." in an error
//     alert above; success closes it, and the manage modal too when that
//     was its only account.

var (
	cwaNS     = i18n.NS("connected-workflow-account")
	cwaRowNS  = i18n.NS("connected-workflow-account-app-row")
	cwmNS     = i18n.NS("connected-workflow-manage-modal")
	cwcNS     = i18n.NS("connected-workflow-account-confirmation-modal")
	cwListNS  = i18n.NS("connected-workflow-accounts-list")
	cwContent = "content-hgl-2" // the check's color.content.hgl.2
)

// connectedRow is ConnectedWorkflowAccount → ConnectedWorkflowAccountAppRow.
func (p *prefsModal) connectedRow(f *prefsForm, ap core.WorkflowAppAuth) {
	gtx, a := f.gtx, f.p.a
	th := a.th
	label := cwaNS.T("Manage")
	f.record("list", "row.conn."+ap.AppID, ap.Name+" · "+label, false, true)
	b := p.btn("row.conn." + ap.AppID)
	rh := gtx.Dp(36) // the row's tallest child, the medium button
	bw := popupsButtonWidth(gtx, th, label)
	is := gtx.Dp(32)
	at(gtx, image.Pt(0, f.y+(rh-is)/2), image.Pt(is, is), func(gtx layout.Context) layout.Dimensions {
		return a.avatar(gtx, ap.Icon, ap.Name, is, 0)
	})
	x := is + gtx.Dp(12)
	nameW := f.w - x - bw - gtx.Dp(8) - gtx.Dp(15+8)
	at(gtx, image.Pt(x, f.y), image.Pt(max(1, nameW), rh), func(gtx layout.Context) layout.Dimensions {
		return vcenter(gtx, rh, func(gtx layout.Context) layout.Dimensions {
			d := th.Label(gtx, 15, theme.Bold, th.C.Fg, ap.Name)
			at(gtx, image.Pt(d.Size.X+gtx.Dp(8), (d.Size.Y-gtx.Dp(15))/2), image.Pt(gtx.Dp(15), gtx.Dp(15)), func(gtx layout.Context) layout.Dimensions {
				icons.Get("check-circle-filled")(gtx, 15, cardTok(th, cwContent))
				return layout.Dimensions{Size: image.Pt(gtx.Dp(15), gtx.Dp(15))}
			})
			return d
		})
	})
	if b.Clicked(gtx) { // before Layout drains it
		p.openManageAccounts(ap.AppID)
		a.Invalidate()
	}
	at(gtx, image.Pt(f.w-bw, f.y), image.Pt(bw, rh), func(gtx layout.Context) layout.Dimensions {
		return popups.Button(gtx, th, b, popups.BtnOutline, popups.BtnMedium, label, "", 80)
	})
	f.y += rh + gtx.Dp(8)
}

// popupsButtonWidth is a medium c-button's width for label: 12px padding
// each side, 80px at least.
func popupsButtonWidth(gtx layout.Context, th *theme.Theme, label string) int {
	return max(gtx.Dp(80), th.Measure(gtx, 15, theme.Bold, label)+gtx.Dp(24)+2)
}

// connectedApp is the app's entry in the fetched list (the store), or false
// once it's gone.
func (p *prefsModal) connectedApp(id string) (core.WorkflowAppAuth, bool) {
	for _, ap := range p.lists.conn {
		if ap.AppID == id {
			return ap, true
		}
	}
	return core.WorkflowAppAuth{}, false
}

// manageRow is one account row of the manage modal.
type manageRow struct {
	provider string // providerName
	tok      core.WorkflowToken
}

type manageSection struct {
	key, name string
	rows      []manageRow
}

// manageSections is H7Jb's section builder for the Preferences case (no
// target provider, no domain-wide auth id, no app token, no search
// account): every provider, domain-wide rows first, empty providers
// dropped, and the sections with a domain-wide row first.
func manageSections(ap core.WorkflowAppAuth) []manageSection {
	var out []manageSection
	anyDwa := false
	for _, pr := range ap.Providers {
		s := manageSection{key: pr.Key, name: pr.Name}
		for _, t := range pr.Tokens {
			if t.DWA() {
				s.rows = append(s.rows, manageRow{pr.Name, t})
				anyDwa = true
			}
		}
		for _, t := range pr.Tokens {
			if !t.DWA() {
				s.rows = append(s.rows, manageRow{pr.Name, t})
			}
		}
		if len(s.rows) > 0 {
			out = append(out, s)
		}
	}
	if !anyDwa {
		return out
	}
	var dwa, rest []manageSection
	for _, s := range out {
		has := false
		for _, r := range s.rows {
			has = has || r.tok.DWA()
		}
		if has {
			dwa = append(dwa, s)
		} else {
			rest = append(rest, s)
		}
	}
	return append(dwa, rest...)
}

// manageModal is manage_accounts_modal's state.
type manageModal struct {
	p      *prefsModal
	appID  string
	modal  *popups.Modal
	clicks map[string]*widget.Clickable
}

func (m *manageModal) btn(k string) *widget.Clickable {
	c := m.clicks[k]
	if c == nil {
		c = new(widget.Clickable)
		m.clicks[k] = c
	}
	return c
}

// openManageAccounts is d40N's onClick: manage_accounts_modal, stacked.
func (p *prefsModal) openManageAccounts(appID string) *manageModal {
	a := p.a
	ap, ok := p.connectedApp(appID)
	if !ok {
		return nil
	}
	m := &manageModal{p: p, appID: appID, clicks: map[string]*widget.Clickable{}}
	m.modal = popups.NewModal(a, popups.ModalOpts{
		HeadH:   16 + 40 + 16, // .p-connected_workflow_accounts_manage_modal_title_bar: padding 16 28, the 40px icon box
		Header:  func(gtx layout.Context) layout.Dimensions { return m.header(gtx, ap) },
		Body:    m.body,
		Buttons: []*popups.ModalButton{{Label: cwmNS.T("Done"), Kind: popups.BtnPrimary}},
	})
	p.manage = m
	a.ShowOverlay(m.modal)
	return m
}

// header is the title bar: the 32px icon in its 40px box (16px right
// margin) and the app's name (an only-child h1, 22px black).
func (m *manageModal) header(gtx layout.Context, ap core.WorkflowAppAuth) layout.Dimensions {
	th := m.p.a.th
	sz := gtx.Constraints.Max
	x := gtx.Dp(28)
	is := gtx.Dp(32)
	at(gtx, image.Pt(x, (sz.Y-is)/2), image.Pt(is, is), func(gtx layout.Context) layout.Dimensions {
		return m.p.a.avatar(gtx, ap.Icon, ap.Name, is, 0)
	})
	x += is + gtx.Dp(16)
	lh := gtx.Dp(30)
	at(gtx, image.Pt(x, (sz.Y-lh)/2), image.Pt(max(1, sz.X-x-gtx.Dp(76)), lh), func(gtx layout.Context) layout.Dimensions {
		return th.Layout(gtx, theme.TextStyle{Size: 22, LineHeight: 30, Weight: theme.Black, Color: th.C.Fg, MaxLines: 1}, ap.Name)
	})
	return layout.Dimensions{Size: sz}
}

// body is renderContent.
func (m *manageModal) body(gtx layout.Context) layout.Dimensions {
	p := m.p
	th := p.a.th
	w := gtx.Constraints.Max.X
	ap, ok := p.connectedApp(m.appID)
	secs := manageSections(ap)
	if !ok || len(secs) == 0 {
		return th.Layout(gtx, theme.TextStyle{Size: 15, LineHeight: 22, Color: th.C.Fg}, cwmNS.T("No accounts found"))
	}
	nonDwa := 0
	for _, s := range secs {
		for _, r := range s.rows {
			if !r.tok.DWA() {
				nonDwa++
			}
		}
	}
	hasDwa := len(secs) > 0 && secs[0].rows[0].tok.DWA()
	label := cwmNS.T("Disconnect")
	bw := popupsButtonWidth(gtx, th, label)
	y := 0
	for _, s := range secs {
		d := at(gtx, image.Pt(0, y), image.Pt(w, 1<<20), func(gtx layout.Context) layout.Dimensions {
			return th.Layout(gtx, theme.TextStyle{Size: 18, LineHeight: 24, Weight: theme.Black, Color: th.C.Fg},
				cwmNS.T("Connected {providerName} Accounts", "providerName", s.name))
		})
		y += d.Size.Y + gtx.Dp(20)
		for _, r := range s.rows {
			r := r
			rowW := w
			if !r.tok.DWA() {
				rowW = w - bw - gtx.Dp(16)
			}
			nameH := gtx.Dp(22)
			subH := 0
			if r.tok.DWA() {
				subH = gtx.Dp(22)
			}
			rh := max(nameH+subH, gtx.Dp(36))
			if r.tok.DWA() {
				rh = nameH + subH
			}
			top := y + (rh-nameH-subH)/2
			at(gtx, image.Pt(0, top), image.Pt(max(1, rowW-gtx.Dp(15+8)), nameH), func(gtx layout.Context) layout.Dimensions {
				d := th.Layout(gtx, theme.TextStyle{Size: 15, LineHeight: 22, Color: th.C.Fg, MaxLines: 1}, r.tok.ExternalUserID)
				at(gtx, image.Pt(d.Size.X+gtx.Dp(8), (nameH-gtx.Dp(15))/2), image.Pt(gtx.Dp(15), gtx.Dp(15)), func(gtx layout.Context) layout.Dimensions {
					icons.Get("check-circle-filled")(gtx, 15, cardTok(th, cwContent))
					return layout.Dimensions{Size: image.Pt(gtx.Dp(15), gtx.Dp(15))}
				})
				return d
			})
			if r.tok.DWA() {
				at(gtx, image.Pt(0, top+nameH), image.Pt(rowW, subH), func(gtx layout.Context) layout.Dimensions {
					return th.Layout(gtx, theme.TextStyle{Size: 15, LineHeight: 22, Color: skForeground(th, "sk_foreground_high", 1)}, cwmNS.T("Added by your administrator"))
				})
			} else {
				b := m.btn("disconnect." + r.tok.ID)
				if b.Clicked(gtx) {
					closeAfter := nonDwa == 1 && !hasDwa
					p.confirmDisconnect(m, ap, r.provider, r.tok, closeAfter)
				}
				at(gtx, image.Pt(w-bw, y+(rh-gtx.Dp(36))/2), image.Pt(bw, gtx.Dp(36)), func(gtx layout.Context) layout.Dimensions {
					return popups.Button(gtx, th, b, popups.BtnOutline, popups.BtnMedium, label, "", 80)
				})
			}
			y += rh + gtx.Dp(16)
		}
	}
	return layout.Dimensions{Size: image.Pt(w, max(0, y-gtx.Dp(16)))}
}

// skForeground is rgba(var(--<name>), alpha) of Slack's stylesheet.
func skForeground(th *theme.Theme, name string, alpha float32) color.NRGBA {
	c := skRGB(th, name)
	c.A = uint8(float32(c.A)*alpha + .5)
	return c
}

// disconnectConfirm is j8Vt's state.
type disconnectConfirm struct {
	submitting, failed bool
}

// confirmDisconnect is eh: ConnectedWorkflowAccountConfirmationModal for
// one account, stacked over the manage modal.
func (p *prefsModal) confirmDisconnect(m *manageModal, ap core.WorkflowAppAuth, providerName string, tok core.WorkflowToken, closeAfter bool) *popups.Modal {
	a := p.a
	th := a.th
	st := &disconnectConfirm{}
	var md *popups.Modal
	submit := &popups.ModalButton{Label: cwcNS.T("Disconnect"), Kind: popups.BtnDanger,
		Enabled: func() bool { return !st.submitting },
		Draw: func(gtx layout.Context, th *theme.Theme, c *widget.Clickable, kind popups.BtnKind, ring bool, _ popups.GrpSide) layout.Dimensions {
			if !st.submitting {
				return popups.FocusButton(gtx, th, c, kind, popups.BtnMedium, cwcNS.T("Disconnect"), "", 80, ring)
			}
			// LoadingButton: the label hidden, a medium spinner (16px) in
			// its place, the button disabled
			d := popups.Button(gtx, th, c, popups.BtnDisabled, popups.BtnMedium, "", "", 80)
			s := gtx.Dp(16)
			at(gtx, image.Pt((d.Size.X-s)/2, (d.Size.Y-s)/2), image.Pt(s, s), func(gtx layout.Context) layout.Dimensions {
				codeSpinner(gtx, s, cardTok(th, "content-hgl-1"))
				return layout.Dimensions{Size: image.Pt(s, s)}
			})
			return d
		},
		Do: func() bool {
			st.submitting, st.failed = true, false
			c := a.Core()
			go func() {
				var err error
				if c == nil {
					err = errNoCore
				} else {
					ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
					err = c.DeleteUserAuth(ctx, tok.ID)
					cancel()
				}
				a.Post(func() {
					if err != nil {
						st.submitting, st.failed = false, true
						a.Invalidate()
						return
					}
					st.submitting = false
					md.Close()
					if closeAfter && m != nil && m.modal != nil {
						m.modal.Close() // closeNamed manage_accounts_modal
					}
					p.reloadConnected() // resetAppAuths, fetchUserAuth
					a.Invalidate()
				})
			}()
			return false
		}}
	md = popups.NewModal(a, popups.ModalOpts{
		HeadH:          20, // the centered title bar's top padding
		HeaderBelow:    func(gtx layout.Context) layout.Dimensions { return confirmTitle(gtx, a, ap, providerName, tok) },
		HeaderBelowPad: 20,
		Body:           func(gtx layout.Context) layout.Dimensions { return confirmBody(gtx, th, st) },
		Buttons:        []*popups.ModalButton{{Label: cwcNS.T("Cancel"), Kind: popups.BtnOutline}, submit},
		AutoFocus:      submit, // ConfirmationModal: autoFocus on submit
	})
	a.ShowOverlay(md)
	return md
}

// confirmTitle is the confirmation's title node, centered in the title
// bar (which keeps 76px each side for the close button,
// c-sk-modal_title_bar--center--close_button_visible).
func confirmTitle(gtx layout.Context, a *App, ap core.WorkflowAppAuth, providerName string, tok core.WorkflowToken) layout.Dimensions {
	th := a.th
	w := gtx.Constraints.Max.X
	in := gtx.Dp(76 - 28)
	tw := max(1, w-2*in)
	y := gtx.Dp(64)
	is := gtx.Dp(64)
	at(gtx, image.Pt((w-is)/2, y), image.Pt(is, is), func(gtx layout.Context) layout.Dimensions {
		return a.avatar(gtx, ap.Icon, ap.Name, is, 0)
	})
	y += is + gtx.Dp(20)
	desc := i18n.Plain(cwcNS.RT("Are you sure you want to disconnect this {providerName} account?", "providerName", providerName))
	d := at(gtx, image.Pt(in, y), image.Pt(tw, 1<<20), func(gtx layout.Context) layout.Dimensions {
		gtx.Constraints.Min.X = tw
		return th.Layout(gtx, theme.TextStyle{Size: 22, LineHeight: 30, Weight: theme.Black, Color: th.C.Fg, Align: textMiddle}, desc)
	})
	y += d.Size.Y + gtx.Dp(8)
	d = at(gtx, image.Pt(in, y), image.Pt(tw, 1<<20), func(gtx layout.Context) layout.Dimensions {
		gtx.Constraints.Min.X = tw
		return th.Layout(gtx, theme.TextStyle{Size: 15, LineHeight: 22, Color: skForeground(th, "sk_foreground_high", .5), Align: textMiddle}, tok.ExternalUserID)
	})
	y += d.Size.Y
	return layout.Dimensions{Size: image.Pt(w, y)}
}

// confirmBody is the error alert (after a failed delete) and WorkflowAlert.
func confirmBody(gtx layout.Context, th *theme.Theme, st *disconnectConfirm) layout.Dimensions {
	w := gtx.Constraints.Max.X
	y := 0
	if st.failed {
		d := alertBoxAt(gtx, th, image.Pt(0, y), w, true, func(gtx layout.Context) layout.Dimensions {
			return th.Layout(gtx, theme.TextStyle{Size: 15, LineHeight: 22, Color: th.C.Fg}, cwcNS.T("Something went wrong. Please try again."))
		})
		y += d.Size.Y + gtx.Dp(16) // the alert's own p margin
	}
	d := alertBoxAt(gtx, th, image.Pt(0, y), w, false, func(gtx layout.Context) layout.Dimensions {
		return workflowAlert(gtx, th)
	})
	y += d.Size.Y
	return layout.Dimensions{Size: image.Pt(w, y)}
}

// workflowAlert is WorkflowAlert's message: the bold header, the three
// items (a 15px disable icon 4px down, 12px before the text, 12px between
// items) and the note (12px/18 at 50%).
func workflowAlert(gtx layout.Context, th *theme.Theme) layout.Dimensions {
	w := gtx.Constraints.Max.X
	y := 0
	d := at(gtx, image.Pt(0, y), image.Pt(w, 1<<20), func(gtx layout.Context) layout.Dimensions {
		return th.Layout(gtx, theme.TextStyle{Size: 15, LineHeight: 22, Weight: theme.Bold, Color: th.C.Fg}, cwcNS.T("What will change:"))
	})
	y += d.Size.Y + gtx.Dp(16) // p{margin:0 0 1rem}
	for _, it := range []string{
		"If you’ve built a workflow using this account, those workflows will stop running.",
		"Workflows that use this account for a data source will stop running.",
		"You will need to sign in again when you run a workflow that uses this account.",
	} {
		ix := gtx.Dp(15 + 12)
		at(gtx, image.Pt(0, y+gtx.Dp(4)), image.Pt(gtx.Dp(15), gtx.Dp(15)), func(gtx layout.Context) layout.Dimensions {
			icons.Get("disable")(gtx, 15, th.C.Fg)
			return layout.Dimensions{Size: image.Pt(gtx.Dp(15), gtx.Dp(15))}
		})
		d := at(gtx, image.Pt(ix, y), image.Pt(max(1, w-ix), 1<<20), func(gtx layout.Context) layout.Dimensions {
			return th.Layout(gtx, theme.TextStyle{Size: 15, LineHeight: 22, Color: th.C.Fg}, cwcNS.T(it))
		})
		y += d.Size.Y + gtx.Dp(12)
	}
	y += gtx.Dp(16 - 12) // the ul's 1rem bottom margin under the last item's 12px
	d = at(gtx, image.Pt(0, y), image.Pt(w, 1<<20), func(gtx layout.Context) layout.Dimensions {
		return th.Layout(gtx, theme.TextStyle{Size: 12, LineHeight: 18, Color: skForeground(th, "sk_foreground_high", .5)},
			cwcNS.T("Note: This will not revoke existing tokens, only remove them from Slack systems. You might be able to revoke them from the provider’s site."))
	})
	y += d.Size.Y
	return layout.Dimensions{Size: image.Pt(w, y)}
}

// alertBoxAt draws a boxed c-alert at pos, w wide, with no icon: level
// default (base-ter, a transparent border in light and otl-sec in dark) or
// error (surf-imp), radius 8, padding 12px 16px.
func alertBoxAt(gtx layout.Context, th *theme.Theme, pos image.Point, w int, errLevel bool, msg layout.Widget) layout.Dimensions {
	px, py := gtx.Dp(16), gtx.Dp(12)
	b := max(1, gtx.Dp(1))
	call, d := recordWidget(gtx, image.Pt(max(1, w-2*px-2*b), 1<<20), msg)
	h := d.Size.Y + 2*py + 2*b
	r := image.Rectangle{Min: pos, Max: pos.Add(image.Pt(w, h))}
	bg := cardTok(th, "base-ter")
	if errLevel {
		bg = cardTok(th, "surf-imp")
	}
	if !errLevel && th.Dark {
		theme.RRect(gtx, r, gtx.Dp(8), cardTok(th, "otl-sec"))
		theme.RRect(gtx, r.Inset(b), gtx.Dp(8)-b, bg)
	} else {
		theme.RRect(gtx, r, gtx.Dp(8), bg)
	}
	at(gtx, pos.Add(image.Pt(px+b, py+b)), d.Size, func(gtx layout.Context) layout.Dimensions {
		call.Add(gtx.Ops)
		return d
	})
	return layout.Dimensions{Size: r.Size()}
}

// reloadConnected is resetAppAuths then fetchUserAuth: the list loads
// again (the rows and an open manage modal follow it).
func (p *prefsModal) reloadConnected() {
	p.lists.connStarted = false
	p.loadConnected()
}

// recordWidget records w laid out within max (min zero).
func recordWidget(gtx layout.Context, max image.Point, w layout.Widget) (op.CallOp, layout.Dimensions) {
	m := op.Record(gtx.Ops)
	gtx.Constraints = layout.Constraints{Max: max}
	d := w(gtx)
	return m.Stop(), d
}

// skRGB is a --sk_* triple of Slack's stylesheet ("29, 28, 29") as an
// opaque color.
func skRGB(th *theme.Theme, name string) color.NRGBA {
	c, _ := css.ParseColor("rgb(" + css.SlackVars(th.Dark)["--"+name] + ")")
	return c
}

const textMiddle = text.Middle
