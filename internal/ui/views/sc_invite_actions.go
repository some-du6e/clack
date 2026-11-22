package views

// The Slack Connect invite actions Activity's rows and Directories ›
// Invitations share (SlackConnectInviteActions' received-invite actions,
// as Activity ported them; docs/ACTIVITY.md "Row actions"): Ignore (the
// forked modal, then Ignore this invitation or Block invitations), Accept
// for a DM (sharedInvites.canAccept, the trust modal, sharedInvites.accept)
// and for a channel (the eligibility, the accept modal or the join, then
// the welcome modal). The host runs the writes and reloads its list.

import (
	"context"
	"errors"
	"image"
	"strings"
	"sync"
	"time"

	"gioui.org/font"
	"gioui.org/layout"
	"gioui.org/op"
	"gioui.org/unit"
	"gioui.org/widget"

	"github.com/some-du6e/clack/internal/core"
	"github.com/some-du6e/clack/internal/i18n"
	"github.com/some-du6e/clack/internal/ui/popups"
	"github.com/some-du6e/clack/internal/ui/theme"
)

// scActHost is what the actions need from the view that shows the invite.
type scActHost interface {
	scBase() *base
	scWrite(fail string, f func(context.Context, *core.App) error, revert func())
	scPost(f func()) // on the UI goroutine, next frame
	scReload()       // read the invites again (on the UI goroutine)
	scForked() *[2]func()
	scFlights() *scFlights
}

// scFlights are the invites whose Accept flow is running: Slack's accept
// actions (SlackConnectAcceptDmInviteAction, …ChannelInviteAction) hold
// their button loading from the click until the flow settles (the
// eligibility read and the accept, or the modal it opens), so a second
// click sends nothing.
type scFlights struct {
	mu sync.Mutex
	m  map[string]bool
}

// start claims invite id; false when its flow is already running.
func (f *scFlights) start(id string) bool {
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.m[id] {
		return false
	}
	if f.m == nil {
		f.m = map[string]bool{}
	}
	f.m[id] = true
	return true
}

func (f *scFlights) done(id string) {
	f.mu.Lock()
	delete(f.m, id)
	f.mu.Unlock()
}

// busy reports invite id's flow running (the Accept button loading).
func (f *scFlights) busy(id string) bool {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.m[id]
}

// scSettle ends invite id's flow (the finally's setIsLoading(false)) and
// asks for the frame that draws Accept again.
func scSettle(x scActHost, id string) {
	x.scFlights().done(id)
	x.scBase().h.Invalidate()
}

// inviterName is the Ignore modals' {imMember} (forceRealName).
func scInviterName(b *base, it core.ActivityItem) string {
	v := b
	if u, ok := v.user(it.Actor); ok && u.RealName != "" {
		return u.RealName
	}
	return firstNonEmpty(it.Invite.InviterName, it.ActorName, v.userName(it.Actor))
}

// ignoreForked is IgnoreForkedModal ("What would you like to do?"): two
// IgnoreForkedModalButton rows, Ignore this invitation (times-circle) and
// Block invitations from this person (disable), each opening its modal.
func scIgnoreForked(x scActHost, ph popups.Host, it core.ActivityItem) {
	v := x.scBase()
	name := scInviterName(v, it)
	var m *popups.Modal
	var rows [2]widget.Clickable
	choices := []struct {
		icon, text, sub string
		next            func()
	}{
		{"times-circle-medium", nsIntro.T("Ignore this invitation"),
			nsIntro.T("This invite simply disappears, as if nothing ever happened. But {imMember} can reach out again.", "imMember", name),
			func() { scIgnoreThis(x, ph, it) }},
		{"disable-font", nsIntro.T("Block invitations from this person"),
			nsIntro.T("You’ll no longer receive Slack Connect invitations sent from {imMember}.", "imMember", name),
			func() { scIgnoreAll(x, ph, it) }},
	}
	// this modal's own row actions: a row runs its closure, never the
	// host's latest (an older modal under a newer one acts on its invite)
	var acts [2]func()
	for i := range choices {
		next := choices[i].next
		acts[i] = func() {
			if m != nil {
				m.Dismiss()
			}
			next()
		}
	}
	*x.scForked() = acts // the newest modal's, for the hosts' tests
	m = popups.NewModal(ph, popups.ModalOpts{
		Title: nsIntro.T("What would you like to do?"),
		Body: func(gtx C) D {
			for i := range rows {
				if rows[i].Clicked(gtx) {
					acts[i]()
				}
			}
			// p-ignore_forked_modal__content: padding 0 14px 24px, the
			// rows' 4px padding-top
			return layout.Inset{Top: 4, Bottom: 24}.Layout(gtx, func(gtx C) D {
				return col(gtx,
					layout.Rigid(func(gtx C) D { return scForkedRow(v, gtx, &rows[0], choices[0].icon, choices[0].text, choices[0].sub) }),
					layout.Rigid(func(gtx C) D { return scForkedRow(v, gtx, &rows[1], choices[1].icon, choices[1].text, choices[1].sub) }),
				)
			})
		},
		OnClose: func() { scForkedOpen.drop(m) },
	})
	scForkedOpen.put(m, acts)
	ph.ShowOverlay(m)
}

// scForkedOpen are the open forked modals' row actions, by modal (tests
// press a given modal's row; ForkedRowPress).
var scForkedOpen = forkedModals{m: map[*popups.Modal][2]func(){}}

type forkedModals struct {
	mu sync.Mutex
	m  map[*popups.Modal][2]func()
}

func (f *forkedModals) put(m *popups.Modal, acts [2]func()) {
	f.mu.Lock()
	f.m[m] = acts
	f.mu.Unlock()
}

func (f *forkedModals) drop(m *popups.Modal) {
	f.mu.Lock()
	delete(f.m, m)
	f.mu.Unlock()
}

// ForkedRowPress presses row i (0 Ignore this invitation, 1 Block
// invitations) of the open forked Ignore modal m, as a click on it does
// (tests); false when m isn't one.
func ForkedRowPress(m *popups.Modal, i int) bool {
	scForkedOpen.mu.Lock()
	acts, ok := scForkedOpen.m[m]
	scForkedOpen.mu.Unlock()
	if !ok || i < 0 || i > 1 {
		return false
	}
	acts[i]()
	return true
}

// forkedRow is IgnoreForkedModalButton: padding 12px 14px, radius 4px, a
// 36px round icon well (foreground-min .04), the bold text over its
// subtext (padding 0 16px), a chevron at the end.
func scForkedRow(v *base, gtx C, c *widget.Clickable, icon, text, sub string) D {
	return c.Layout(gtx, func(gtx C) D {
		pointerCursor(gtx)
		m := op.Record(gtx.Ops)
		d := layout.Inset{Top: 12, Bottom: 12, Left: 14, Right: 14}.Layout(gtx, func(gtx C) D {
			gtx.Constraints.Min.X = gtx.Constraints.Max.X
			return row(gtx, layout.Middle,
				layout.Rigid(func(gtx C) D {
					s := gtx.Dp(36)
					theme.Circle(gtx, image.Pt(s/2, s/2), s, theme4b(v, 0x0a))
					g := gtx
					g.Constraints = layout.Exact(image.Pt(s, s))
					return layout.Center.Layout(g, func(gtx C) D { return v.icon(gtx, icon, 20, v.p.Sec) })
				}),
				layout.Flexed(1, func(gtx C) D {
					return layout.Inset{Left: 16, Right: 16}.Layout(gtx, func(gtx C) D {
						return col(gtx,
							layout.Rigid(func(gtx C) D { return v.para(gtx, 15, 22, wBold, v.p.Text, 0, text) }),
							layout.Rigid(func(gtx C) D { return v.para(gtx, 15, 22, wReg, v.p.Sec, 0, sub) }),
						)
					})
				}),
				layout.Rigid(func(gtx C) D { return v.icon(gtx, "chevron-medium-right", 20, v.p.Sec) }),
			)
		})
		call := m.Stop()
		if c.Hovered() {
			rrect(gtx, image.Rectangle{Max: d.Size}, gtx.Dp(4), theme4b(v, 0x0a))
		}
		call.Add(gtx.Ops)
		return d
	})
}

// ignoreThis is IgnoreThisModal: "Ignore this invitation from {name}?",
// "{name} ({email}) won’t be notified you’ve done this." (plus the search
// hint for a DM introduction or channel invite), Cancel / Ignore Invitation
// (danger) → conversations.ignoreInvite or sharedInvites.ignoreInvite.
func scIgnoreThis(x scActHost, ph popups.Host, it core.ActivityItem) {
	v := x.scBase()
	name := scInviterName(v, it)
	email := ""
	if it.Invite.InviterEmail != "" {
		email = " (" + it.Invite.InviterEmail + ")"
	}
	channel := it.Invite.Kind == "channel"
	body := nsIntro.T("{imMember}{maybeImMemberEmail} won’t be notified you’ve done this.", "imMember", name, "maybeImMemberEmail", email)
	if channel {
		body = nsIntro.T("{imMember}{maybeImMemberEmail} won’t be notified you’ve done this. If you change your mind, you can search for their name to start the conversation.", "imMember", name, "maybeImMemberEmail", email)
	}
	id, key := it.Invite.ID, actKey(&it)
	ph.ShowOverlay(popups.NewModal(ph, popups.ModalOpts{
		Title: nsIntro.T("Ignore this invitation from {imMember}?", "imMember", name),
		Body:  func(gtx C) D { return v.para(gtx, 15, 22, wReg, v.p.Text, 0, body) },
		Buttons: []*popups.ModalButton{
			{Label: nsIntro.T("Cancel"), Kind: popups.BtnOutline},
			{Label: nsIntro.T("Ignore Invitation"), Kind: popups.BtnDanger, Do: func() bool {
				x.scWrite("Hmm, something went wrong. Try again? ", func(ctx context.Context, c *core.App) error {
					if err := c.IgnoreConnectInvite(ctx, id, channel); err != nil {
						return err
					}
					// the reload, on the UI goroutine (load reads the view's state)
					x.scPost(func() { x.scReload() })
					return nil
				}, func() {})
				_ = key
				return true
			}},
		},
	}))
}

// ignoreAll is IgnoreAllModal: "Block invitations from {name}?", Cancel /
// Block Invitations (danger) → conversations.ignoreUser or
// sharedInvites.ignoreUser, then IgnoreAllSuccessModal ("You won’t hear
// from {name} again", Got It).
func scIgnoreAll(x scActHost, ph popups.Host, it core.ActivityItem) {
	v := x.scBase()
	name := scInviterName(v, it)
	email := ""
	if it.Invite.InviterEmail != "" {
		email = " (" + it.Invite.InviterEmail + ")"
	}
	channel := it.Invite.Kind == "channel"
	id := it.Invite.ID
	ph.ShowOverlay(popups.NewModal(ph, popups.ModalOpts{
		Title: nsIntro.T("Block invitations from {imMember}?", "imMember", name),
		Body: func(gtx C) D {
			return col(gtx,
				layout.Rigid(func(gtx C) D {
					return v.para(gtx, 15, 22, wReg, v.p.Text, 0, nsIntro.T("You’ll no longer receive Slack Connect invitations from {imMember}{maybeImMemberEmail}.", "imMember", name, "maybeImMemberEmail", email))
				}),
				layout.Rigid(func(gtx C) D {
					return layout.Inset{Top: 8}.Layout(gtx, func(gtx C) D {
						return v.para(gtx, 15, 22, wReg, v.p.Text, 0, nsIntro.T("They won’t be notified you’ve done this."))
					})
				}),
				layout.Rigid(func(gtx C) D {
					return layout.Inset{Top: 8}.Layout(gtx, func(gtx C) D {
						return v.para(gtx, 15, 22, wReg, v.p.Text, 0, i18n.StripTags(nsIntro.T("To undo this, you can adjust the setting under <strong>privacy & visibility</strong> in your preferences.")))
					})
				}),
			)
		},
		Buttons: []*popups.ModalButton{
			{Label: nsIntro.T("Cancel"), Kind: popups.BtnOutline},
			{Label: nsIntro.T("Block Invitations"), Kind: popups.BtnDanger, Do: func() bool {
				x.scWrite("Hmm, something went wrong. Try again? ", func(ctx context.Context, c *core.App) error {
					if err := c.BlockConnectInvites(ctx, id, channel); err != nil {
						return err
					}
					x.scPost(func() {
						ph.ShowOverlay(popups.NewModal(ph, popups.ModalOpts{
							Title: nsIntro.T("You won\u2019t hear from {imMember} again", "imMember", name),
							Body: func(gtx C) D {
								return v.para(gtx, 15, 22, wReg, v.p.Text, 0, i18n.StripTags(nsIntro.T("If you change your mind, you can adjust the setting under <strong>privacy & visibility</strong> in your preferences.")))
							},
							Buttons: []*popups.ModalButton{{Label: nsIntro.T("Got It"), Kind: popups.BtnOutline}},
						}))
						x.scReload() // on the UI goroutine, as the modal
					})
					return nil
				}, func() {})
				return true
			}},
		},
	}))
}

// acceptDM is SlackConnectAcceptDmInviteAction: sharedInvites.canAccept
// {sig}; eligible and from a verified organisation, sharedInvites.accept
// and the DM with the inviter opens; eligible otherwise, the trust modal;
// not eligible, "Hmm, something went wrong" (hasConnectInviteAcceptanceFailed).
func scAcceptDM(x scActHost, ph popups.Host, it core.ActivityItem) {
	inv := *it.Invite
	inviter := it.Actor
	if !x.scFlights().start(inv.ID) {
		return
	}
	x.scWrite("Hmm, something went wrong. Try again? ", func(ctx context.Context, c *core.App) error {
		defer scSettle(x, inv.ID)
		ok, err := c.ConnectDMInviteEligible(ctx, inv.Sig)
		if err != nil {
			return err
		}
		if !ok {
			return errors.New("this invitation can’t be accepted")
		}
		if !inv.TeamVerified {
			x.scPost(func() { scTrustModal(x, ph, it) })
			return nil
		}
		if err := c.AcceptConnectDMInvite(ctx, inv.Sig, inv.ID); err != nil {
			return err
		}
		scOpenDMWith(x, inviter)
		return nil
	}, func() {})
}

// openDMWith opens your DM with user (Y1Qm openDmWithMember,
// shouldNavigateToChannel: conversations.open). Called off the UI
// goroutine.
func scOpenDMWith(x scActHost, user string) {
	v := x.scBase()
	c := v.h.Core()
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	defer cancel()
	conv, err := c.OpenDM(ctx, user)
	x.scPost(func() {
		if err == nil && conv.ID != "" {
			v.h.Navigate("c/" + conv.ID)
		}
		x.scReload()
	})
}

// trustModal is SlackConnectConfirmTrustModal: "Before you accept this
// invitation…" ("This invitation is from a new account" for a suspicious
// one isn't in the feed's invite), "Make sure you recognize the sender’s
// email address and other info.", the inviter's photo, "{name} from
// {team}.", the email, "This organization has used Slack since {MMM
// YYYY}", Ignore (the forked modal) / Accept Invitation.
func scTrustModal(x scActHost, ph popups.Host, it core.ActivityItem) {
	v := x.scBase()
	inv := *it.Invite
	name := scInviterName(v, it)
	av := ""
	if u, ok := v.user(it.Actor); ok {
		av = u.Avatar
	}
	inviter := it.Actor
	ph.ShowOverlay(popups.NewModal(ph, popups.ModalOpts{
		Title: nsSharedInvites.T("Before you accept this invitation…"), // SlackConnectConfirmTrustModal
		Body: func(gtx C) D {
			ln := func(s string, size, lh unit.Sp, w font.Weight, top unit.Dp) layout.FlexChild {
				return layout.Rigid(func(gtx C) D {
					return layout.Inset{Top: top}.Layout(gtx, func(gtx C) D {
						return layout.Center.Layout(gtx, func(gtx C) D { return v.para(gtx, size, lh, w, v.p.Text, 0, s) })
					})
				})
			}
			ch := []layout.FlexChild{
				ln(nsSharedInvites.T("Make sure you recognize the sender’s email address and other info."), 15, 22, wReg, 0),
				layout.Rigid(func(gtx C) D {
					return layout.Inset{Top: 24}.Layout(gtx, func(gtx C) D {
						gtx.Constraints.Min.X = gtx.Constraints.Max.X
						return layout.Center.Layout(gtx, func(gtx C) D { return v.avatar(gtx, av, 50) })
					})
				}),
				ln(i18n.StripTags(nsSharedInvites.T("<strong>{invitingUserName}</strong> from {invitingTeamName}.", "invitingUserName", name, "invitingTeamName", it.Team)), 15, 22, wReg, 8),
			}
			if inv.InviterEmail != "" {
				ch = append(ch, ln(inv.InviterEmail, 13, 18, wReg, 0))
			}
			if inv.TeamCreated > 0 {
				ch = append(ch, ln(nsSharedInvites.T("This organization has used Slack since {teamDuration}", "teamDuration", monthYear(time.Unix(inv.TeamCreated, 0))), 15, 22, wReg, 4))
			}
			return layout.Inset{Bottom: 8}.Layout(gtx, func(gtx C) D { return col(gtx, ch...) })
		},
		Buttons: []*popups.ModalButton{
			{Label: nsSharedInvites.T("Ignore"), Kind: popups.BtnOutline, Do: func() bool {
				scIgnoreForked(x, ph, it)
				return true
			}},
			{Label: nsSharedInvites.T("Accept Invitation"), Kind: popups.BtnPrimary, Do: func() bool {
				x.scWrite("Hmm, something went wrong. Try again? ", func(ctx context.Context, c *core.App) error {
					if err := c.AcceptConnectDMInvite(ctx, inv.Sig, inv.ID); err != nil {
						return err
					}
					scOpenDMWith(x, inviter)
					return nil
				}, func() {})
				return true
			}},
		},
	}))
}

// acceptChannel is SlackConnectAcceptChannelInviteAction's click (dcZX):
// the eligibility check (core ChannelInviteEligible), then the upgrade
// modal, the accept modal (popups/sc_accept.go; a trial adds its note
// and "Accept and Start Trial"), or the join_channel alternative's
// conversations.joinConnectedShared; anything else is
// hasConnectInviteAcceptanceFailed. Grid's "accept on another
// workspace" (SlackConnectConfirmWorkspaceModal), the start-trial modal
// before the accept modal and the join-when-connected and join-workspace
// alternatives aren't in Clack: Clack signs in to one workspace.
func scAcceptChannel(x scActHost, ph popups.Host, it core.ActivityItem) {
	v := x.scBase()
	inv := *it.Invite
	team, name := it.Team, strings.TrimPrefix(it.Title, "#")
	inviter := scInviterName(v, it)
	if !x.scFlights().start(inv.ID) {
		return
	}
	x.scWrite("Hmm, something went wrong. Try again? ", func(ctx context.Context, c *core.App) error {
		defer scSettle(x, inv.ID)
		el, err := c.ChannelInviteEligible(ctx, inv.Sig)
		if err != nil {
			return err
		}
		show := func(f func()) {
			x.scPost(f)
		}
		switch {
		case el.IsEligible && el.Requirements.Upgrade || el.Reason == core.SCReasonUpgradeButCannot:
			show(func() { ph.ShowOverlay(popups.NewSCUpgradeModal(ph, inv.ID)) })
		case el.IsEligible:
			show(func() {
				ph.ShowOverlay(popups.NewSCAcceptModal(ph, popups.SCAcceptOpts{Invite: inv, ChannelName: name, InvitingTeam: team,
					RequiredAccess: el.RequiredPrivacy(), FreeTrial: el.Requirements.Trial,
					OnAccepted: func(string, bool) { scAfterChannelAccept(x, ph, inv, inviter, team) }}))
			})
		case el.CanJoinChannel():
			if err := c.JoinConnectedChannel(ctx, inv.Sig, inv.ID); err != nil {
				return err
			}
			show(func() { scAfterChannelAccept(x, ph, inv, inviter, team) })
		default:
			return errors.New("this invitation can’t be accepted")
		}
		return nil
	}, func() {})
}

// afterChannelAccept is g7VB acceptSharedInviteAndMaybeOpenWelcomeModal:
// connectInvites.list for the invite; an approved acceptance opens the
// channel; then SharedInviteCreatorWelcomeModal; the feed reloads (the
// invite's row leaves it).
func scAfterChannelAccept(x scActHost, ph popups.Host, inv core.ActivityInvite, inviter, team string) {
	v := x.scBase()
	x.scWrite("", func(ctx context.Context, c *core.App) error {
		st, err := c.ConnectChannelInvite(ctx, inv.ID)
		x.scPost(func() {
			if err == nil && st.Found {
				if st.Approved && st.ChannelID != "" {
					v.h.Navigate("c/" + st.ChannelID)
				}
				ph.ShowOverlay(popups.NewSCWelcomeModal(ph, popups.SCWelcome{Connected: st.Approved, ChannelName: st.ChannelName,
					InvitingTeam: firstNonEmpty(st.InvitingTeam, team), InvitingTeamID: st.InvitingTeamID, InvitingName: inviter,
					PendingTeams: st.PendingTeams}))
			}
			x.scReload()
		})
		return err
	}, func() {})
}
