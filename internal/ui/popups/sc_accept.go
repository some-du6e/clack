package popups

import (
	"context"
	"image"
	"net/url"
	"strings"
	"time"
	"unicode/utf8"

	"gioui.org/io/event"
	"gioui.org/io/key"
	"gioui.org/layout"
	"gioui.org/widget"

	"github.com/some-du6e/clack/internal/core"
	"github.com/some-du6e/clack/internal/i18n"
	"github.com/some-du6e/clack/internal/ui/openurl"
	"github.com/some-du6e/clack/internal/ui/theme"
)

// Slack's accept modal for a Slack Connect channel invitation, opened by
// Activity's Accept on an external channel invite (gantry-v2 boot-shared
// dcZX, build d3de3a487b1c, public CDN, no cookie):
//
//   - SlackConnectAcceptChannelInviteModal → SlackConnectAcceptChannelInviteMultiPaneModal:
//     the panes WORKSPACE_SELECTION, CHANNEL_NAME, CHANNEL_PRIVACY, each
//     with AcceptInvitationModalHeader ("Accept your invitation", the
//     invite's channel as "#name" under it, ZuZy formatChannelName) and
//     the footer's FooterSteps ("Step {currentStep} of {totalSteps}").
//   - WorkspaceSelectionPane / SelectWorkspaceField: "Workspace where this
//     channel will live:" and the workspace (Clack signs in to one, so
//     the suggested-workspaces select never shows); off Grid the escape
//     hatch "You can also sign into a different workspace or create a new
//     workspace." (redirectToGetStartedPage in a new window); Next.
//   - ChannelNamePane: "Name this channel for your team", the channel name
//     input (45Rv, validated on load and on every edit, no # prefix), the
//     hint "This won’t impact the channel name for {invitingTeamName}.";
//     a name_taken error on the invite's own name doesn't count; Back,
//     Next (disabled while checking or invalid).
//   - ChannelPrivacyPane / ChannelPrivacyField: "Should this channel be
//     public or private?", "Public - Anyone in {selectedWorkspace} can
//     join" and "Private - Only specific people can join" ("Requires an
//     invitation"), the one the requirements rule out disabled; "{invitingTeamName}
//     may have different privacy settings for their team."; Back, "Accept
//     and Join Channel" ("Accept and Start Trial" with a trial). A failed
//     accept shows AcceptInvitationErrorModalContent ("Something went
//     wrong", "If the problem persists, please drop us a line") and the
//     footer becomes a danger Close.
//   - FreeTrialNote under the footer when the workspace needs a trial.
//   - After the accept, g7VB acceptSharedInviteAndMaybeOpenWelcomeModal
//     (connectInvites.list) and SBlm SharedInviteCreatorWelcomeModal:
//     NewSCWelcomeModal.

var (
	nsSlackConnect = i18n.NS("slack_connect")
	nsSetupPage    = i18n.NS("setup_page")
)

// SCAcceptOpts open the accept modal.
type SCAcceptOpts struct {
	Invite         core.ActivityInvite
	ChannelName    string // the invite's channel
	InvitingTeam   string
	RequiredAccess string // the requirements' channel_privacy ("public" | "private" | "")
	FreeTrial      bool
	// OnAccepted runs on the UI goroutine after conversations.acceptSharedInvite
	// succeeds (Activity reads the invite back and opens the welcome modal).
	OnAccepted func(name string, private bool)
}

// SCAcceptModal is the open accept modal.
type SCAcceptModal struct {
	h     Host
	o     SCAcceptOpts
	modal *Modal

	step      int // 0 workspace, 1 name, 2 privacy
	ed        widget.Editor
	focused   bool
	name      string
	gen       int
	checking  bool
	code      string
	private   bool
	pubBtn    widget.Clickable
	privBtn   widget.Clickable
	signIn    widget.Clickable
	create    widget.Clickable
	help      widget.Clickable
	trialMore widget.Clickable

	accepting  bool
	failed     bool
	workspace  string
	enterprise bool

	next, back, accept, closeBtn *ModalButton
}

// scSteps is the multi-pane modal's L: WORKSPACE_SELECTION, CHANNEL_NAME,
// CHANNEL_PRIVACY.
const scSteps = 3

// NewSCAcceptModal builds the modal; show it with h.ShowOverlay.
func NewSCAcceptModal(h Host, o SCAcceptOpts) *SCAcceptModal {
	c := &SCAcceptModal{h: h, o: o, private: o.RequiredAccess == "private"}
	c.ed.SingleLine = true
	c.ed.Submit = true
	if a := h.Core(); a != nil {
		select {
		case <-a.Ready():
			snap := a.Snapshot()
			c.workspace = strings.TrimSpace(snap.Team.Name)
			c.enterprise = snap.Self.EnterpriseID != ""
		default:
		}
	}
	c.next = &ModalButton{Label: nsSlackConnect.T("Next"), Kind: BtnPrimary, Enabled: c.canNext, Do: func() bool { c.Next(); return false }}
	c.back = &ModalButton{Label: nsSlackConnect.T("Back"), Kind: BtnOutline, Do: func() bool { c.Back(); return false }}
	c.accept = &ModalButton{Label: c.acceptLabel(), Kind: BtnPrimary, Enabled: func() bool { return !c.accepting }, Do: func() bool { c.Accept(); return false }}
	c.closeBtn = &ModalButton{Label: nsSlackConnect.T("Close"), Kind: BtnDanger, Enabled: func() bool { return !c.accepting }}
	c.modal = NewModal(h, ModalOpts{Title: nsSlackConnect.T("Accept your invitation"), Body: c.layout,
		Subtitle: "#" + o.ChannelName, FooterLeft: c.footerLeft, CloseGuard: func() bool { return !c.accepting },
		FocusStops: func() []event.Tag {
			switch c.step {
			case 1:
				return []event.Tag{&c.ed}
			case 2:
				if c.private {
					return []event.Tag{&c.privBtn}
				}
				return []event.Tag{&c.pubBtn}
			}
			return nil
		}})
	c.ed.SetText(o.ChannelName)
	c.name = core.NormalizeChannelName(o.ChannelName)
	c.check() // shouldValidateChannelNameOnLoad
	c.sync()
	return c
}

// Layout and Dismissed make it an Overlay.
func (c *SCAcceptModal) Layout(gtx layout.Context) layout.Dimensions { return c.modal.Layout(gtx) }
func (c *SCAcceptModal) Dismissed() bool                             { return c.modal.Dismissed() }

// Dismiss closes it.
func (c *SCAcceptModal) Dismiss() { c.modal.Dismiss() }

// Step is the pane shown: "workspace", "name" or "privacy".
func (c *SCAcceptModal) Step() string { return [...]string{"workspace", "name", "privacy"}[c.step] }

// Name is the channel name typed so far (normalized).
func (c *SCAcceptModal) Name() string { return c.name }

// SetName replaces the name as if typed.
func (c *SCAcceptModal) SetName(s string) { c.ed.SetText(s); c.onEdit() }

// Private reports the privacy picked.
func (c *SCAcceptModal) Private() bool { return c.private }

// SetPrivate picks a privacy, unless the requirements disable it.
func (c *SCAcceptModal) SetPrivate(p bool) {
	if p && c.o.RequiredAccess == "public" || !p && c.o.RequiredAccess == "private" {
		return
	}
	c.private = p
}

// Failed reports AcceptInvitationErrorModalContent.
func (c *SCAcceptModal) Failed() bool { return c.failed }

// CanNext reports whether Next would leave the pane.
func (c *SCAcceptModal) CanNext() bool { return c.canNext() }

func (c *SCAcceptModal) acceptLabel() string {
	if c.o.FreeTrial {
		return nsSlackConnect.T("Accept and Start Trial")
	}
	return nsSlackConnect.T("Accept and Join Channel")
}

func (c *SCAcceptModal) canNext() bool {
	switch c.step {
	case 0:
		return true
	case 1:
		return !c.checking && c.nameError() == "" && c.name != ""
	}
	return false
}

// nameError is the name's error, without the name_taken warning on the
// invite's own name (ChannelNamePane k).
func (c *SCAcceptModal) nameError() string {
	code := c.code
	if code == core.NameTaken && c.name == core.NormalizeChannelName(c.o.ChannelName) {
		code = ""
	}
	if code == "" {
		return ""
	}
	return core.ChannelNameErrorText(code, c.workspace)
}

// Next goes to the next pane.
func (c *SCAcceptModal) Next() {
	if !c.canNext() {
		return
	}
	c.step++
	c.focused = false
	c.sync()
}

// Back goes to the previous pane.
func (c *SCAcceptModal) Back() {
	if c.step > 0 {
		c.step--
	}
	c.focused = false
	c.sync()
}

// Accept is ChannelPrivacyPane's accept: conversations.acceptSharedInvite.
func (c *SCAcceptModal) Accept() {
	if c.accepting || c.step != 2 {
		return
	}
	c.accepting = true
	o := core.AcceptChannelInviteOpts{Sig: c.o.Invite.Sig, InviteID: c.o.Invite.ID, ChannelName: c.name, Private: c.private, FreeTrial: c.o.FreeTrial}
	run(c.h, func(ctx context.Context) (bool, error) {
		a := c.h.Core()
		if a == nil {
			return false, context.Canceled
		}
		return a.AcceptConnectChannelInvite(ctx, o)
	}, func(_ bool, err error) {
		c.accepting = false
		if err != nil {
			c.failed = true
			c.sync()
			return
		}
		c.modal.Close()
		if c.o.OnAccepted != nil {
			c.o.OnAccepted(o.ChannelName, o.Private)
		}
	})
	c.h.Invalidate()
}

func (c *SCAcceptModal) sync() {
	o := &c.modal.o
	switch {
	case c.step == 0:
		o.Buttons = []*ModalButton{c.next}
	case c.step == 1:
		o.Buttons = []*ModalButton{c.back, c.next}
	case c.failed:
		o.Buttons = []*ModalButton{c.closeBtn}
	default:
		o.Buttons = []*ModalButton{c.back, c.accept}
	}
}

// footerLeft is FooterSteps (p-slack_connect_accept_channel_invite_modal__footer_stepper,
// #1d1c1db3).
func (c *SCAcceptModal) footerLeft(gtx layout.Context) layout.Dimensions {
	th := c.h.Theme()
	return text(gtx, th, 15, theme.Regular, colorsOf(th).Fg2,
		nsSlackConnect.T("Step {currentStep} of {totalSteps}", "currentStep", c.step+1, "totalSteps", scSteps))
}

func (c *SCAcceptModal) onEdit() {
	raw := c.ed.Text()
	norm := core.NormalizeChannelName(raw)
	if norm != raw {
		caret, _ := c.ed.Selection()
		prefix := string([]rune(raw)[:min(caret, utf8.RuneCountInString(raw))])
		n := utf8.RuneCountInString(core.NormalizeChannelName(prefix))
		c.ed.SetText(norm)
		c.ed.SetCaret(n, n)
	}
	if norm == c.name {
		return
	}
	c.name = norm
	c.check()
}

// check asks conversations.validateName after 45Rv's debounce.
func (c *SCAcceptModal) check() {
	c.gen++
	gen := c.gen
	c.checking = true
	time.AfterFunc(ccDebounce, func() {
		post(c.h, func() {
			if gen != c.gen || c.modal.Dismissed() {
				return
			}
			name := c.name
			run(c.h, func(ctx context.Context) (string, error) {
				a := c.h.Core()
				if a == nil {
					return "", context.Canceled
				}
				return a.ValidateChannelName(ctx, name)
			}, func(_ string, err error) {
				if gen != c.gen {
					return
				}
				c.checking = false
				c.code = core.ChannelNameCode(err)
			})
		})
	})
}

func (c *SCAcceptModal) events(gtx layout.Context) {
	if !c.focused && c.step == 1 {
		gtx.Execute(key.FocusCmd{Tag: &c.ed})
		c.focused = true
	}
	for {
		e, ok := c.ed.Update(gtx)
		if !ok {
			break
		}
		switch e.(type) {
		case widget.ChangeEvent:
			c.onEdit()
		case widget.SubmitEvent:
			c.Next()
		}
	}
	if c.pubBtn.Clicked(gtx) {
		c.SetPrivate(false)
	}
	if c.privBtn.Clicked(gtx) {
		c.SetPrivate(true)
	}
	if c.signIn.Clicked(gtx) {
		c.getStarted(false)
	}
	if c.create.Clicked(gtx) {
		c.getStarted(true)
	}
	if c.help.Clicked(gtx) {
		_ = openurl.Open("https://slack.com/help/requests/new")
	}
	if c.trialMore.Clicked(gtx) {
		_ = openurl.Open("https://slack.com/help/articles/115004151203")
	}
}

// getStarted is redirectToGetStartedPage in a new window (isDm false):
// app.slack.com/get-started?sig&invite_id&redir=/share/{sig}, #/createnew
// for a new workspace; the modal closes.
func (c *SCAcceptModal) getStarted(create bool) {
	if c.accepting || c.o.Invite.Sig == "" {
		return
	}
	q := url.Values{"sig": {c.o.Invite.Sig}, "invite_id": {c.o.Invite.ID}, "redir": {"/share/" + c.o.Invite.Sig}}
	u := "https://app.slack.com/get-started?" + q.Encode()
	if create {
		u += "#/createnew"
	}
	_ = openurl.Open(u)
	c.modal.Close()
}

func (c *SCAcceptModal) layout(gtx layout.Context) layout.Dimensions {
	c.events(gtx)
	th := c.h.Theme()
	cl := colorsOf(th)
	w := gtx.Constraints.Max.X
	var d layout.Dimensions
	switch c.step {
	case 0:
		d = c.layoutWorkspace(gtx, th, cl, w)
	case 1:
		d = c.layoutName(gtx, th, cl, w)
	default:
		d = c.layoutPrivacy(gtx, th, cl, w)
	}
	if c.o.FreeTrial {
		d.Size.Y += c.trialNote(gtx, th, cl, w, d.Size.Y)
	}
	return d
}

// layoutWorkspace is SelectWorkspaceField without the select: the label,
// the workspace (target_workspace--with_steps: padding 2px 0 6px, 15px),
// and off Grid the escape hatch (13px, #1d1c1db3, padding-top 4px).
func (c *SCAcceptModal) layoutWorkspace(gtx layout.Context, th *theme.Theme, cl *Colors, w int) layout.Dimensions {
	y := 0
	lh := dp(gtx, 22)
	at(gtx, image.Pt(0, y), func(gtx layout.Context) layout.Dimensions {
		gtx.Constraints = layout.Constraints{Max: image.Pt(w, lh)}
		return vcenter(gtx, lh, func(gtx layout.Context) layout.Dimensions {
			return text(gtx, th, 15, theme.Bold, cl.Fg, nsSlackConnect.T("Workspace where this channel will live:"))
		})
	})
	y += lh + dp(gtx, 2)
	at(gtx, image.Pt(0, y), func(gtx layout.Context) layout.Dimensions {
		gtx.Constraints = layout.Constraints{Max: image.Pt(w, lh)}
		return vcenter(gtx, lh, func(gtx layout.Context) layout.Dimensions {
			return text(gtx, th, 15, theme.Regular, cl.Fg, c.workspace)
		})
	})
	y += lh + dp(gtx, 6)
	if !c.enterprise {
		y += dp(gtx, 4)
		d := at(gtx, image.Pt(0, y), func(gtx layout.Context) layout.Dimensions {
			return c.escapeHatch(gtx, th, cl, w)
		})
		y += d.Size.Y
	}
	y += dp(gtx, 8) // content_section:last-child padding-bottom 8px
	return layout.Dimensions{Size: image.Pt(w, y)}
}

// escapeHatch is "You can also {changeWorkspaceLink} or
// {createWorkspaceLink}", its links in the locale's order.
func (c *SCAcceptModal) escapeHatch(gtx layout.Context, th *theme.Theme, cl *Colors, w int) layout.Dimensions {
	lh := dp(gtx, 18)
	x, y := 0, 0
	put := func(s string, link *widget.Clickable) {
		for _, word := range strings.SplitAfter(s, " ") {
			if word == "" {
				continue
			}
			tw := textW(gtx, th, 13, theme.Regular, word)
			if x > 0 && x+tw > w {
				x, y = 0, y+lh
			}
			col := cl.Fg2
			if link != nil {
				col = cl.Link
			}
			at(gtx, image.Pt(x, y), func(gtx layout.Context) layout.Dimensions {
				gtx.Constraints = layout.Constraints{Max: image.Pt(tw, lh)}
				if link == nil {
					return vcenter(gtx, lh, func(gtx layout.Context) layout.Dimensions { return text(gtx, th, 13, theme.Regular, col, word) })
				}
				return clickBox(gtx, link, nil, func(gtx layout.Context) layout.Dimensions {
					return vcenter(gtx, lh, func(gtx layout.Context) layout.Dimensions { return text(gtx, th, 13, theme.Regular, col, word) })
				})
			})
			x += tw
		}
	}
	const sep = "\x00"
	s := nsSlackConnect.T("You can also {changeWorkspaceLink} or {createWorkspaceLink}", "changeWorkspaceLink", sep+"1"+sep, "createWorkspaceLink", sep+"2"+sep)
	for _, part := range strings.Split(s, sep) {
		switch part {
		case "1":
			put(nsSlackConnect.T("sign into a different workspace"), &c.signIn)
		case "2":
			put(nsSlackConnect.T("create a new workspace."), &c.create)
		default:
			put(part, nil)
		}
	}
	return layout.Dimensions{Size: image.Pt(w, y+lh)}
}

// layoutName is ChannelNamePane: the label (c-label, 15px bold), the
// medium input (36px, no channel prefix), the error (13px content-imp,
// margin-top 8px) or the hint (c-hint margin 8px 0 0).
func (c *SCAcceptModal) layoutName(gtx layout.Context, th *theme.Theme, cl *Colors, w int) layout.Dimensions {
	y := 0
	lh := dp(gtx, 22)
	at(gtx, image.Pt(0, y), func(gtx layout.Context) layout.Dimensions {
		gtx.Constraints = layout.Constraints{Max: image.Pt(w, lh)}
		return vcenter(gtx, lh, func(gtx layout.Context) layout.Dimensions {
			return text(gtx, th, 15, theme.Bold, cl.Fg, nsSlackConnect.T("Name this channel for your team"))
		})
	})
	y += lh + dp(gtx, 4) // c-label__text margin-bottom 4px
	ih := dp(gtx, 36)
	errText := c.nameError()
	at(gtx, image.Pt(0, y), func(gtx layout.Context) layout.Dimensions {
		gtx.Constraints = layout.Exact(image.Pt(w, ih))
		d := field(gtx, th, &c.ed, "", "", 36, 15)
		if errText != "" {
			stroke(gtx, image.Rect(0, 0, w, ih), dp(gtx, 8), maxInt(1, dp(gtx, 1)), ccErrColor(th, cl))
		}
		return d
	})
	y += ih + dp(gtx, 8)
	col, s := cl.Fg2, nsSlackConnect.T("This won’t impact the channel name for {invitingTeamName}.", "invitingTeamName", c.o.InvitingTeam)
	if errText != "" {
		col, s = ccErrColor(th, cl), errText
	}
	d := at(gtx, image.Pt(0, y), func(gtx layout.Context) layout.Dimensions {
		gtx.Constraints = layout.Constraints{Max: image.Pt(w, 1<<20)}
		return para(gtx, th, 13, 18, theme.Regular, col, s, 0)
	})
	y += d.Size.Y + dp(gtx, 8)
	return layout.Dimensions{Size: image.Pt(w, y)}
}

// layoutPrivacy is ChannelPrivacyField: the legend (c-legend margin 0 0
// 8px), the two lightweight radio options 8px apart (the one the
// requirements rule out content-ter and not clickable), the privacy
// warning (13px, #1d1c1db3, margin-top 20px); then the error alert after
// a failed accept.
func (c *SCAcceptModal) layoutPrivacy(gtx layout.Context, th *theme.Theme, cl *Colors, w int) layout.Dimensions {
	y := 0
	lh := dp(gtx, 22)
	at(gtx, image.Pt(0, y), func(gtx layout.Context) layout.Dimensions {
		gtx.Constraints = layout.Constraints{Max: image.Pt(w, lh)}
		return vcenter(gtx, lh, func(gtx layout.Context) layout.Dimensions {
			return text(gtx, th, 15, theme.Bold, cl.Fg, nsSlackConnect.T("Should this channel be public or private?"))
		})
	})
	y += lh + dp(gtx, 8)
	opt := func(btn *widget.Clickable, on, disabled bool, label, sub string) {
		fg, sfg := cl.Fg, cl.Fg2
		if disabled {
			fg, sfg = cl.Fg3, cl.Fg3
		}
		d := at(gtx, image.Pt(0, y), func(gtx layout.Context) layout.Dimensions {
			gtx.Constraints = layout.Constraints{Max: image.Pt(w, 1<<20)}
			draw := func(gtx layout.Context) layout.Dimensions {
				at(gtx, image.Pt(0, dp(gtx, 2)), func(gtx layout.Context) layout.Dimensions { return radio(gtx, cl, on) })
				tx := dp(gtx, 28)
				ld := at(gtx, image.Pt(tx, 0), func(gtx layout.Context) layout.Dimensions {
					gtx.Constraints = layout.Constraints{Max: image.Pt(w-tx, 1<<20)}
					return para(gtx, th, 15, 22, theme.Regular, fg, label, 0)
				})
				hgt := ld.Size.Y
				if sub != "" {
					sd := at(gtx, image.Pt(tx, hgt), func(gtx layout.Context) layout.Dimensions {
						gtx.Constraints = layout.Constraints{Max: image.Pt(w-tx, 1<<20)}
						return para(gtx, th, 13, 18, theme.Regular, sfg, sub, 0)
					})
					hgt += sd.Size.Y
				}
				return layout.Dimensions{Size: image.Pt(w, maxInt(hgt, lh))}
			}
			if disabled {
				return draw(gtx)
			}
			return clickBox(gtx, btn, nil, draw)
		})
		y += d.Size.Y
	}
	// {selectedWorkspace} is lFhh TeamName: the workspace's name
	opt(&c.pubBtn, !c.private, c.o.RequiredAccess == "private", nsSlackConnect.T("Public - Anyone in {selectedWorkspace} can join", "selectedWorkspace", c.workspace), "")
	y += dp(gtx, 8)
	opt(&c.privBtn, c.private, c.o.RequiredAccess == "public", nsSlackConnect.T("Private - Only specific people can join"), nsSlackConnect.T("Requires an invitation"))
	y += dp(gtx, 20)
	d := at(gtx, image.Pt(0, y), func(gtx layout.Context) layout.Dimensions {
		gtx.Constraints = layout.Constraints{Max: image.Pt(w, 1<<20)}
		return para(gtx, th, 13, 18, theme.Regular, cl.Fg2, nsSlackConnect.T("{invitingTeamName} may have different privacy settings for their team.", "invitingTeamName", c.o.InvitingTeam), 0)
	})
	y += d.Size.Y + dp(gtx, 8)
	if c.failed {
		// AcceptInvitationErrorModalContent: margin_top_150, an error
		// alert (VvO8 level error): the heading, then the help line
		y += dp(gtx, 12)
		d := at(gtx, image.Pt(0, y), func(gtx layout.Context) layout.Dimensions { return c.errorAlert(gtx, th, cl, w) })
		y += d.Size.Y + dp(gtx, 8)
	}
	return layout.Dimensions{Size: image.Pt(w, y)}
}

// errorAlert is the error Alert: a 1px content-imp-tinted box (radius 8,
// padding 12px 16px) with the warning icon, "Something went wrong" in
// bold and "If the problem persists, please drop us a line" (the link to
// the help request page).
func (c *SCAcceptModal) errorAlert(gtx layout.Context, th *theme.Theme, cl *Colors, w int) layout.Dimensions {
	ec := ccErrColor(th, cl)
	pad, ix := dp(gtx, 12), dp(gtx, 16)
	call, d := record(gtx, func(gtx layout.Context) layout.Dimensions {
		inner := w - 2*ix - dp(gtx, 28)
		at(gtx, image.Pt(ix, pad+dp(gtx, 2)), func(gtx layout.Context) layout.Dimensions { return icon(gtx, "warning", 18, ec) })
		hd := at(gtx, image.Pt(ix+dp(gtx, 28), pad), func(gtx layout.Context) layout.Dimensions {
			gtx.Constraints = layout.Constraints{Max: image.Pt(inner, 1<<20)}
			return para(gtx, th, 15, 22, theme.Bold, cl.Fg, nsSlackConnect.T("Something went wrong"), 0)
		})
		help := i18n.StripTags(nsSlackConnect.T("If the problem persists, please <a>drop us a line</a>"))
		bd := at(gtx, image.Pt(ix+dp(gtx, 28), pad+hd.Size.Y), func(gtx layout.Context) layout.Dimensions {
			gtx.Constraints = layout.Constraints{Max: image.Pt(inner, 1<<20)}
			return clickBox(gtx, &c.help, nil, func(gtx layout.Context) layout.Dimensions {
				return para(gtx, th, 15, 22, theme.Regular, cl.Fg, help, 0)
			})
		})
		return layout.Dimensions{Size: image.Pt(w, pad+hd.Size.Y+bd.Size.Y+pad)}
	})
	r := image.Rectangle{Max: d.Size}
	rrect(gtx, r, dp(gtx, 8), theme.WithAlpha(ec, 0x14))
	stroke(gtx, r, dp(gtx, 8), maxInt(1, dp(gtx, 1)), theme.WithAlpha(ec, 0x80))
	call.Add(gtx.Ops)
	return d
}

// trialNote is FreeTrialNote (13px, #1d1c1d0a, padding 16px 24px): "By
// accepting this invitation, your team will start a free trial. Learn
// more", under the pane.
func (c *SCAcceptModal) trialNote(gtx layout.Context, th *theme.Theme, cl *Colors, w, y int) int {
	s := i18n.StripTags(nsSlackConnect.T("By accepting this invitation, your team will start a free trial. <strong>{learnMoreLink}</strong>", "learnMoreLink", nsSlackConnect.T("Learn more")))
	d := at(gtx, image.Pt(0, y), func(gtx layout.Context) layout.Dimensions {
		call, d := record(gtx, func(gtx layout.Context) layout.Dimensions {
			return at(gtx, image.Pt(dp(gtx, 24), dp(gtx, 16)), func(gtx layout.Context) layout.Dimensions {
				gtx.Constraints = layout.Constraints{Max: image.Pt(w-dp(gtx, 48), 1<<20)}
				return clickBox(gtx, &c.trialMore, nil, func(gtx layout.Context) layout.Dimensions {
					return para(gtx, th, 13, 18, theme.Regular, cl.Fg, s, 0)
				})
			})
		})
		sz := image.Pt(w, d.Size.Y+dp(gtx, 32))
		rrect(gtx, image.Rectangle{Max: sz}, 0, theme.WithAlpha(cl.Fg, 0x0a))
		call.Add(gtx.Ops)
		return layout.Dimensions{Size: sz}
	})
	return d.Size.Y
}

// ---- SBlm SharedInviteCreatorWelcomeModal ----

// SCWelcome is what the welcome modal says after an accept (core
// ConnectInviteState plus the names it prints).
type SCWelcome struct {
	Connected      bool
	ChannelName    string
	InvitingTeam   string
	InvitingName   string // the inviter's real name (forceRealName)
	PendingTeams   []string
	InvitingTeamID string
}

// NewSCWelcomeModal is SharedInviteCreatorWelcomeModal's welcome step for
// a channel invite: "Welcome to the channel!" with "{channelName} is where
// you’ll work with {invitingMember} and others from {invitingTeam}." once
// connected, else "All set — admins are taking a look" and which admins
// still have to approve (Vd/3's pending teams); "Got It" closes.
func NewSCWelcomeModal(h Host, w SCWelcome) *Modal {
	title, parts := scWelcomeCopy(w)
	// the pending copy's {invitingMember} is bold (eU), the connected
	// copy's isn't (eH); <strong> is
	var runs []cnRun
	for _, p := range parts {
		runs = append(runs, cnRun{s: p.Text, bold: p.Tag == "strong" || !w.Connected && p.Arg == "invitingMember"})
	}
	return NewModal(h, ModalOpts{Title: title, Body: func(gtx layout.Context) layout.Dimensions {
		th := h.Theme()
		cl := colorsOf(th)
		d := cnRuns(gtx, th, cl, runs, 15, 22, cl.Fg, gtx.Constraints.Max.X)
		d.Size.Y += dp(gtx, 8)
		return d
	}, Buttons: []*ModalButton{{Label: nsSetupPage.T("Got It"), Kind: BtnPrimary}}, EnterSubmits: true})
}

// SCWelcomeText is the welcome modal's title and body as plain text.
func SCWelcomeText(w SCWelcome) (title, body string) {
	title, parts := scWelcomeCopy(w)
	return title, i18n.Plain(parts)
}

// scWelcomeCopy is SBlm's eO (the title) and eq (the body) for a channel
// invite without a trial.
func scWelcomeCopy(w SCWelcome) (string, []i18n.Part) {
	ch := "#" + w.ChannelName
	switch {
	case w.Connected:
		return nsSetupPage.T("Welcome to the channel!"),
			nsSetupPage.RT("<strong>{channelName}</strong> is where you’ll work with {invitingMember} and others from <strong>{invitingTeam}</strong>.",
				"channelName", ch, "invitingMember", w.InvitingName, "invitingTeam", w.InvitingTeam)
	}
	title := nsSetupPage.T("All set — admins are taking a look")
	switch {
	case len(w.PendingTeams) == 0:
		return title, nsSetupPage.RT("All organizations will need the approval of their admins to work together using Slack Connect. Once approved, you’ll be connected.")
	case len(w.PendingTeams) == 1 && w.PendingTeams[0] == w.InvitingTeamID:
		return title, nsSetupPage.RT("You’ve accepted this invite from {invitingMember}! To keep things secure, admins from {invitingTeam} need to approve it before you’re added to <strong>{channelName}</strong>.",
			"channelName", ch, "invitingMember", w.InvitingName, "invitingTeam", w.InvitingTeam)
	case len(w.PendingTeams) == 1:
		return title, nsSetupPage.RT("You’ve accepted this invite from {invitingMember}! To keep things secure, admins from your organization need to approve it before you’re added to <strong>{channelName}</strong>.",
			"channelName", ch, "invitingMember", w.InvitingName)
	}
	return title, nsSetupPage.RT("You’ve accepted this invite from {invitingMember}! To keep things secure, admins from your organization and {invitingTeam} need to approve it before you’re added to <strong>{channelName}</strong>.",
		"channelName", ch, "invitingMember", w.InvitingName, "invitingTeam", w.InvitingTeam)
}

// NewSCUpgradeModal is SlackConnectUpgradeModal: "Upgrade to join this
// channel", why, Not Now / See Upgrade Options (plans_page_cta: the plans
// page with entry_point slack_connect_received_invite and the invite, in
// the browser).
func NewSCUpgradeModal(h Host, inviteID string) *Modal {
	return NewModal(h, ModalOpts{Title: nsSlackConnect.T("Upgrade to join this channel"),
		Body: func(gtx layout.Context) layout.Dimensions {
			th := h.Theme()
			d := para(gtx, th, 15, 22, theme.Regular, colorsOf(th).Fg,
				nsSlackConnect.T("This is a Slack Connect channel, a paid feature that allows teams to work securely with external people. You’ll need to upgrade your team’s plan to accept this invite."), 0)
			d.Size.Y += dp(gtx, 8)
			return d
		},
		Buttons: []*ModalButton{
			{Label: nsSlackConnect.T("Not Now"), Kind: BtnOutline},
			{Label: nsSlackConnect.T("See Upgrade Options"), Kind: BtnPrimary, Do: func() bool {
				_ = openurl.Open(scPlansURL(h, inviteID))
				return true
			}},
		}})
}

// scPlansURL is kzxh getPlansPageUrl on the team's domain.
func scPlansURL(h Host, inviteID string) string {
	host := "app.slack.com"
	if a := h.Core(); a != nil {
		select {
		case <-a.Ready():
			if d := a.Snapshot().Team.Domain; d != "" {
				host = d + ".slack.com"
			}
		default:
		}
	}
	q := url.Values{"entry_point": {"slack_connect_received_invite"}, "invite_id": {inviteID}}
	return "https://" + host + "/plans?" + q.Encode()
}
