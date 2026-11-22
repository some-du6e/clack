package views

// Directories › Invitations: Slack's SlackConnectPage (gmwg) and its
// InvitationsExplorer (useInvitationsExplorer, Agv4; build a09b625d90aa):
//
//   - the large spinner until connectInvites.list answers; when it fails
//     "Something’s fishy! Slack can’t seem to load your invitations." with
//     Try again;
//   - with no invitation at all SlackConnectInvitationsEmptyPage: the
//     picture, "Track your invitations", "You’ll see the status of
//     invitations you’ve sent and received here.", Learn more, the admin's
//     "Enable Slack Connect to work with other companies" or everyone
//     else's "Contact an admin to use Slack Connect", and the three steps
//     for a viewer who may invite external people;
//   - else the "Filter by" select (Show all, Sent, Received) and the Sent
//     and Received sections of SlackConnectCards: a sent email invite (to
//     a channel or a DM), a sent channel or DM link, a received channel or
//     DM invite, each with its header, icon, name, context, the state
//     footer and the actions F5qO FI allows (Copy link, Revoke or Disable
//     link, Ignore, Accept). Sent with none sent is the empty page without
//     steps; Received with none received SlackConnectTabEmptyState.

import (
	"context"
	"image"
	"strings"
	"sync"
	"time"

	"gioui.org/layout"
	"gioui.org/op"
	"gioui.org/text"
	"gioui.org/unit"
	"gioui.org/widget"

	"github.com/some-du6e/clack/internal/core"
	"github.com/some-du6e/clack/internal/i18n"
	"github.com/some-du6e/clack/internal/slack"
	"github.com/some-du6e/clack/internal/slackasset"
	"github.com/some-du6e/clack/internal/ui/popups"
	"github.com/some-du6e/clack/internal/ui/theme"
)

// scInvData is the invites connectInvites.list answered.
type scInvData struct {
	in, out []core.ConnectInvite
}

// scInvActs are an invite row's action buttons.
type scInvActs struct {
	copy, revoke, ignore, accept widget.Clickable
	tipCopy, tipRevoke           popups.TipState
	copiedAt                     time.Time
	wk                           wake // Link copied's end, the tooltips' delay
}

type scInvites struct {
	d  *Directories
	wk wake // the spinner's frames

	hub     async[core.SCHub]
	data    async[scInvData]
	filter  dirDropdown
	list    widget.List
	retry   widget.Clickable
	learn   widget.Clickable
	admin   widget.Clickable
	acts    map[string]*scInvActs
	forked  [2]func()
	flights scFlights // the invites whose Accept flow runs

	notesMu sync.Mutex
	notes   []func()
	wmu     sync.Mutex
}

var scFilterLabels = []string{"Show all", "Sent", "Received"}

func newSCInvites(d *Directories) *scInvites {
	v := &scInvites{d: d, acts: map[string]*scInvActs{}}
	v.list.Axis = layout.Vertical
	v.filter.labels, v.filter.ns = scFilterLabels, nsSCHub
	return v
}

func (v *scInvites) start() {
	c := v.d.h.Core()
	if c == nil || v.data.started() {
		return
	}
	v.hub.load(v.d.h, true, func(ctx context.Context) (core.SCHub, error) { return c.LoadSlackConnectHub(ctx), nil })
	v.fetch()
}

// fetch is useConnectInvitations' fetchInvitations: fetchAndResetAllConnectInvites.
// Try again (retry) always sends it, past the client's backoff of a
// request that just failed the same way (slack.NoBackoff).
func (v *scInvites) fetch() { v.fetchWith(false) }

func (v *scInvites) fetchWith(retry bool) {
	c := v.d.h.Core()
	if c == nil {
		return
	}
	v.data.load(v.d.h, false, func(ctx context.Context) (scInvData, error) {
		if retry {
			ctx = slack.NoBackoff(ctx)
		}
		in, out, err := c.ConnectInvites(ctx)
		return scInvData{in: in, out: out}, err
	})
}

// scActHost.
func (v *scInvites) scBase() *base { return &v.d.base }
func (v *scInvites) scPost(f func()) {
	v.notesMu.Lock()
	v.notes = append(v.notes, f)
	v.notesMu.Unlock()
	v.d.h.Invalidate()
}
func (v *scInvites) scReload()             { v.fetch() }
func (v *scInvites) scForked() *[2]func()  { return &v.forked }
func (v *scInvites) scFlights() *scFlights { return &v.flights }
func (v *scInvites) scWrite(fail string, f func(context.Context, *core.App) error, revert func()) {
	c := v.d.h.Core()
	go func() {
		v.wmu.Lock()
		defer v.wmu.Unlock()
		ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
		defer cancel()
		if err := f(ctx, c); err != nil {
			v.scPost(func() {
				if fail != "" {
					v.d.h.Toast(fail + friendlyErr(err))
				}
				if revert != nil {
					revert()
				}
			})
		}
	}()
}

func (v *scInvites) runNotes() {
	v.notesMu.Lock()
	fns := v.notes
	v.notes = nil
	v.notesMu.Unlock()
	for _, f := range fns {
		f()
	}
}

// Invitations reads (tests).

// SCInvitesLoaded reports the list in (or failed), not loading.
func (d *Directories) SCInvitesLoaded() bool {
	_, ok, _ := d.inv.data.get()
	_, h, _ := d.inv.hub.get()
	return ok && h && !d.inv.data.busy()
}

// SCInvitesState is what the tab shows: "error", "empty" (the empty page),
// "list".
func (d *Directories) SCInvitesState() string { return d.inv.state() }

// SCInviteRows are the list's rows: "Sent" / "Received" section titles,
// and each invite as "<id>:<footer state>:<actions>".
func (d *Directories) SCInviteRows() []string {
	var out []string
	for _, r := range d.inv.rows() {
		out = append(out, r.key)
	}
	return out
}

// SCInviteFilter picks Filter by i (0 Show all, 1 Sent, 2 Received).
func (d *Directories) SCInviteFilter(i int) { d.inv.filter.sel = i }

// SCInviteAct runs invite id's action (core.InviteAct*).
func (d *Directories) SCInviteAct(id, act string) {
	ci, ok := d.inv.byID(id)
	if !ok {
		return
	}
	d.inv.act(ci, act)
}

// SCForkedRowPress presses row i of Invitations' open forked Ignore
// modal m (ForkedRowPress).
func (d *Directories) SCForkedRowPress(m *popups.Modal, i int) bool { return ForkedRowPress(m, i) }

// SCInviteBusy reports invite id's Accept flow running (its button
// loading).
func (d *Directories) SCInviteBusy(id string) bool { return d.inv.flights.busy(id) }

// SCInvitesRetry is the error state's Try again.
func (d *Directories) SCInvitesRetry() { d.inv.fetchWith(true) }

func (v *scInvites) state() string {
	st, loaded, err := v.data.get()
	switch {
	case !loaded:
		return "loading"
	case err != nil:
		return "error"
	case len(st.in)+len(st.out) == 0:
		return "empty"
	}
	return "list"
}

func (v *scInvites) byID(id string) (core.ConnectInvite, bool) {
	st, _, _ := v.data.get()
	for _, x := range append(append([]core.ConnectInvite{}, st.out...), st.in...) {
		if x.ID == id {
			return x, true
		}
	}
	return core.ConnectInvite{}, false
}

func (v *scInvites) actsOf(id string) *scInvActs {
	a, ok := v.acts[id]
	if !ok {
		a = &scInvActs{}
		a.tipCopy.Wake, a.tipRevoke.Wake = tipWake(v.d.h, &a.wk), tipWake(v.d.h, &a.wk)
		v.acts[id] = a
	}
	return a
}

// act runs an invite's action.
func (v *scInvites) act(ci core.ConnectInvite, act string) {
	ph, ok := v.d.h.(popups.Host)
	if !ok {
		return
	}
	it := core.ActivityItem{Invite: &ci.ActivityInvite, Actor: ci.InviterID, Team: ci.InvitingTeam, Title: ci.ChannelName}
	switch act {
	case core.InviteActIgnore:
		scIgnoreForked(v, ph, it)
	case core.InviteActAcceptDM:
		scAcceptDM(v, ph, it)
	case core.InviteActAcceptChannel:
		scAcceptChannel(v, ph, it)
	case core.InviteActRevoke:
		v.revokeModal(ph, ci)
	}
}

// revokeModal is SlackConnectRevokeInviteModal: "Revoke this
// invitation?" (an email invite) or "Disable this link?", the sentence for
// its kind, Cancel / Revoke | Disable (danger) →
// conversations.revokeSharedInvite or sharedInvites.revoke, then the
// outgoing invites read again.
func (v *scInvites) revokeModal(ph popups.Host, ci core.ConnectInvite) {
	link := !ci.IsEmail()
	title, btn := nsSC.T("Revoke this invitation?"), nsSC.T("Revoke")
	if link {
		title, btn = nsSC.T("Disable this link?"), nsSC.T("Disable")
	}
	ch := "#" + ci.ChannelName
	var body string
	switch {
	case ci.Kind == "im" && link:
		body = nsSC.T("This link will no longer let people direct message you. You can always generate a new link in the future.")
	case ci.Kind == "im":
		body = nsSC.T("<strong>{recipientEmail}</strong> won’t be able to direct message you. You can always invite them again in the future.", "recipientEmail", ci.RecipientEmail)
	default:
		var pending []core.InviteAcceptor
		for _, a := range ci.Acceptors {
			if a.Status == "pending_approval" {
				pending = append(pending, a)
			}
		}
		switch {
		case len(pending) == 0 && link:
			body = nsSC.T("This link will no longer let people join {channelEntity}. You can always generate a new link in the future.", "channelEntity", ch)
		case len(pending) == 0:
			body = nsSC.T("<strong>{email}</strong> won’t be able to join {channelEntity}. You can always invite them again in the future.", "email", ci.RecipientEmail, "channelEntity", ch)
		case len(pending) == 1 && !link:
			body = nsSC.T("{acceptingUser} from {acceptingUserTeam} won’t be able to join {channelName}. You can always invite them again in the future.",
				"acceptingUser", pending[0].UserName, "acceptingUserTeam", pending[0].TeamName, "channelName", ch)
		case len(pending) == 1:
			body = nsSC.T("This link will no longer let people join {channelName}. Also, any pending invitations will be revoked, including {acceptingUser}.", "channelName", ch, "acceptingUser", pending[0].UserName)
		case len(pending) == 2:
			body = nsSC.T("This link will no longer let people join {channelName}. Also, any pending invitations will be revoked, including {acceptingUser1} and {acceptingUser2}.",
				"channelName", ch, "acceptingUser1", pending[0].UserName, "acceptingUser2", pending[1].UserName)
		default:
			body = nsSC.T("This link will no longer let people join {channelName}. Also, any pending invitations will be revoked, including {acceptingUser1}, {acceptingUser2}, and <strong>{numOtherAcceptingUsers} more</strong>.",
				"channelName", ch, "acceptingUser1", pending[0].UserName, "acceptingUser2", pending[1].UserName, "numOtherAcceptingUsers", len(pending)-2)
		}
	}
	body = i18n.StripTags(body)
	ph.ShowOverlay(popups.NewModal(ph, popups.ModalOpts{
		Title: title,
		Body:  func(gtx C) D { return v.d.para(gtx, 15, 22, wReg, v.d.p.Text, 0, body) },
		Buttons: []*popups.ModalButton{
			{Label: nsSC.T("Cancel"), Kind: popups.BtnOutline},
			{Label: btn, Kind: popups.BtnDanger, Do: func() bool {
				v.scWrite("Hmm, something went wrong. Try again? ", func(ctx context.Context, c *core.App) error {
					if err := c.RevokeConnectInvite(ctx, ci); err != nil {
						return err
					}
					out, err := c.ConnectInvitesOutgoing(ctx) // fetchAndResetOutboundConnectInvites (McFF)
					if err != nil {
						// the revoke went through: the card shows it revoked
						// (no Revoke or Copy link) though the re-read failed
						v.data.mutate(func(d *scInvData) {
							for i := range d.out {
								if d.out[i].ID == ci.ID {
									d.out[i].Status = "revoked"
								}
							}
						})
						v.d.h.Invalidate()
						return nil
					}
					v.data.mutate(func(d *scInvData) { d.out = out })
					v.d.h.Invalidate()
					return nil
				}, nil)
				return true
			}},
		},
	}).OldDialog())
}

// ---- drawing ----

func (v *scInvites) layout(gtx C) D {
	v.start()
	v.runNotes()
	d := v.d
	fill(gtx, image.Rectangle{Max: gtx.Constraints.Max}, d.p.ReadFill)
	switch v.state() {
	case "loading":
		return layout.Inset{Top: 40}.Layout(gtx, func(gtx C) D {
			gtx.Constraints.Min.X = gtx.Constraints.Max.X
			return layout.N.Layout(gtx, func(gtx C) D { return actSpinnerVia(gtx, gtx.Dp(32), d.p.Faint, tipWake(d.h, &v.wk)) }) // xRE+ Spinner large, margin_top_250
		})
	case "error":
		if v.retry.Clicked(gtx) {
			v.fetchWith(true)
		}
		return layout.Center.Layout(gtx, func(gtx C) D {
			return layout.Flex{Axis: layout.Vertical, Alignment: layout.Middle}.Layout(gtx,
				layout.Rigid(func(gtx C) D {
					return d.centered(gtx, 15, wReg, d.p.Text, nsSCHub.T("Something’s fishy! Slack can’t seem to load your invitations."))
				}),
				layout.Rigid(func(gtx C) D {
					return layout.Inset{Top: 16}.Layout(gtx, func(gtx C) D { return d.button(gtx, &v.retry, btnOutline, false, nsSCHub.T("Try again")) })
				}),
			)
		})
	case "empty":
		return v.emptyPage(gtx, false)
	}
	rows := v.rows()
	return layout.Inset{Top: 16}.Layout(gtx, func(gtx C) D { // invitationsView: padding-top 16
		return d.scrollList(gtx, &v.list, len(rows)+1, func(gtx C, i int) D {
			gtx.Constraints.Min.X = gtx.Constraints.Max.X
			if i == len(rows) {
				return D{Size: image.Pt(gtx.Constraints.Max.X, gtx.Dp(24))}
			}
			r := rows[i]
			if r.key == "empty-state-sent" || r.key == "empty-state-received" {
				return r.draw(gtx)
			}
			return layout.Inset{Left: dirPad, Right: dirPad}.Layout(gtx, r.draw)
		})
	})
}

func (v *scInvites) rows() []scRow {
	d := v.d
	st, _, _ := v.data.get()
	var rows []scRow
	add := func(k string, f func(gtx C) D) { rows = append(rows, scRow{key: k, draw: f}) }
	add("controls", func(gtx C) D {
		if v.filter.update(gtx) {
			v.list.Position = layout.Position{}
		}
		return layout.Inset{Bottom: 16}.Layout(gtx, func(gtx C) D {
			dims := v.filter.layoutSortButton(&d.base, gtx, v.filter.text(v.filter.sel)) // the menu once, left-aligned
			if v.filter.open {
				v.filter.menu(&d.base, gtx, dims.Size, false)
			}
			return dims
		})
	})
	self := d.selfID()
	now := time.Now()
	invite := func(ci core.ConnectInvite) {
		key := ci.ID + ":" + scFooterState(ci, now) + ":" + strings.Join(ci.HubActions(self, now), ",")
		add(key, func(gtx C) D { return v.card(gtx, ci) })
	}
	switch v.filter.sel {
	case 0:
		if len(st.out) > 0 {
			add("Sent", func(gtx C) D { return v.sectionTitle(gtx, nsSCHub.T("Sent")) })
			for _, ci := range st.out {
				invite(ci)
			}
		}
		if len(st.in) > 0 {
			add("Received", func(gtx C) D { return v.sectionTitle(gtx, nsSCHub.T("Received")) })
			for _, ci := range st.in {
				invite(ci)
			}
		}
	case 1:
		if len(st.out) == 0 {
			add("empty-state-sent", func(gtx C) D {
				return layout.Inset{Top: 48}.Layout(gtx, func(gtx C) D { return v.emptyPage(gtx, true) })
			})
		}
		for _, ci := range st.out {
			invite(ci)
		}
	case 2:
		if len(st.in) == 0 {
			add("empty-state-received", func(gtx C) D { return layout.Inset{Top: 48}.Layout(gtx, v.receivedEmpty) })
		}
		for _, ci := range st.in {
			invite(ci)
		}
	}
	return rows
}

// sectionTitle is the section-title row (ExplorerSectionTitle,
// isUnifiedDirectories: content-ter, 400, 4px in).
func (v *scInvites) sectionTitle(gtx C, t string) D {
	return layout.Inset{Top: 4, Bottom: 8, Left: 4}.Layout(gtx, func(gtx C) D { return v.d.label(gtx, 15, wReg, v.d.p.Faint, t) })
}

// emptyPage is SlackConnectInvitationsEmptyPage (hideSteps for Sent).
func (v *scInvites) emptyPage(gtx C, hideSteps bool) D {
	d := v.d
	hub, _, _ := v.hub.get()
	if v.learn.Clicked(gtx) {
		scOpenHelp(d.h, "7158164899") // 0x1ac6c815a3
	}
	if v.admin.Clicked(gtx) {
		if hub.AdminContent {
			if o, ok := d.h.(interface{ OpenURL(string) }); ok {
				o.OpenURL(scAdminURL(d.h.Core(), "manage/slack-connect/settings"))
			}
		} else {
			scOpenHelp(d.h, "360035280511") // 0x53d1e2002c
		}
	}
	link := nsSCHub.T("Contact an admin to use Slack Connect")
	if hub.AdminContent {
		link = nsSCHub.T("Enable Slack Connect to work with other companies")
	}
	steps := hub.Invite.OK && hub.Paid && !hideSteps
	return layout.Center.Layout(gtx, func(gtx C) D {
		return layout.Flex{Axis: layout.Vertical, Alignment: layout.Middle}.Layout(gtx,
			layout.Rigid(func(gtx C) D {
				return dirImage(&d.base, gtx, slackasset.SCInvitesEmpty, image.Pt(gtx.Dp(123), gtx.Dp(120)), 0)
			}),
			layout.Rigid(func(gtx C) D { return d.centered(gtx, 18, wBlack, d.p.Text, nsSCHub.T("Track your invitations")) }),
			layout.Rigid(func(gtx C) D {
				gtx.Constraints.Max.X = min(gtx.Constraints.Max.X, gtx.Dp(371))
				return d.th.Layout(gtx, theme.TextStyle{Size: 15, LineHeight: 22, Color: d.p.Sec, Align: text.Middle},
					nsSCHub.T("You’ll see the status of invitations you’ve sent and received here."))
			}),
			layout.Rigid(func(gtx C) D {
				return layout.Inset{Top: 16}.Layout(gtx, func(gtx C) D { return d.button(gtx, &v.learn, btnOutline, false, nsSCHub.T("Learn more")) })
			}),
			layout.Rigid(func(gtx C) D {
				return layout.Inset{Top: 8, Bottom: 8, Left: 4, Right: 4}.Layout(gtx, func(gtx C) D { return d.linkButton(gtx, &v.admin, 15, link) })
			}),
			layout.Rigid(func(gtx C) D {
				if !steps {
					return D{}
				}
				return layout.Flex{Axis: layout.Vertical, Alignment: layout.Middle}.Layout(gtx,
					layout.Rigid(func(gtx C) D {
						return layout.Inset{Top: 32, Bottom: 32}.Layout(gtx, func(gtx C) D {
							fill(gtx, image.Rect(0, 0, gtx.Dp(169), max(1, gtx.Dp(1))), d.p.Border)
							return D{Size: image.Pt(gtx.Dp(169), max(1, gtx.Dp(1)))}
						})
					}),
					layout.Rigid(func(gtx C) D {
						return d.centered(gtx, 13, wReg, d.p.Faint, nsSCHub.T("1. Invite someone to join a channel via email or link."))
					}),
					layout.Rigid(func(gtx C) D {
						return d.centered(gtx, 13, wReg, d.p.Faint, nsSCHub.T("2. Depending on your company, admins may need to sign off."))
					}),
					layout.Rigid(func(gtx C) D {
						return d.centered(gtx, 13, wReg, d.p.Faint, nsSCHub.T("3. They’re in! And they can add their coworkers, too."))
					}),
				)
			}),
		)
	})
}

// receivedEmpty is SlackConnectTabEmptyState for Received: "No new
// invitations — yet", "But you’re in the right place to accept them.
// We’ll let you know when a new one comes in." (no footer).
func (v *scInvites) receivedEmpty(gtx C) D {
	d := v.d
	gtx.Constraints.Min.X = gtx.Constraints.Max.X
	return layout.Flex{Axis: layout.Vertical, Alignment: layout.Middle}.Layout(gtx,
		layout.Rigid(func(gtx C) D { return d.centered(gtx, 18, wBlack, d.p.Text, nsSCHub.T("No new invitations — yet")) }),
		layout.Rigid(func(gtx C) D {
			gtx.Constraints.Max.X = min(gtx.Constraints.Max.X, gtx.Dp(371))
			return d.th.Layout(gtx, theme.TextStyle{Size: 15, LineHeight: 22, Color: d.p.Sec, Align: text.Middle},
				nsSCHub.T("But you’re in the right place to accept them. We’ll let you know when a new one comes in."))
		}),
	)
}

// scOpenHelp opens a help center article.
func scOpenHelp(h Host, id string) {
	if o, ok := h.(interface{ OpenURL(string) }); ok {
		o.OpenURL("https://slack.com/help/articles/" + id)
	}
}

// ---- the card ----

// scFooterState is the card's footer state (data-qa-invite-state):
// rejected, requested, denied, approved, pending_approval, revoked,
// expired, or "" none.
func scFooterState(ci core.ConnectInvite, now time.Time) string {
	var acc *core.InviteAcceptor
	if len(ci.Acceptors) > 0 {
		acc = &ci.Acceptors[0]
	}
	accExpired := acc != nil && (acc.Status == "expired" || acc.Status == "pending_approval" && acc.DateInvalid != 0 && acc.DateInvalid < now.Unix())
	expired := acc != nil && accExpired || acc == nil && !ci.Active(now) && ci.Status != "revoked"
	revoked := ci.Status == "revoked" && (acc == nil || accExpired)
	requested := ci.Status == "requested"
	switch {
	case acc != nil && acc.Status == "rejected":
		return "rejected"
	case requested && ci.RequestDenied == 0 && !expired:
		return "requested"
	case requested && ci.RequestDenied != 0:
		return "denied"
	case acc != nil && acc.Status == "approved":
		return "approved"
	case acc != nil && acc.Status == "pending_approval" && !accExpired:
		return "pending_approval"
	case revoked:
		return "revoked"
	case expired:
		return "expired"
	}
	return ""
}

// card is SlackConnectCard: the header (13/18, padding 8px 16px 4px, the
// top border), the body (the icon, the bold primary, the 13px secondary,
// the actions at the right), the footer (13/18, padding 8px 16px; green on
// approved, red on rejected, gray when inactive); 8px apart.
func (v *scInvites) card(gtx C, ci core.ConnectInvite) D {
	d := v.d
	now := time.Now()
	state := scFooterState(ci, now)
	a := v.actsOf(ci.ID)
	acts := ci.HubActions(d.selfID(), now)
	for _, x := range acts {
		var c *widget.Clickable
		switch x {
		case core.InviteActCopyLink:
			c = &a.copy
		case core.InviteActRevoke:
			c = &a.revoke
		case core.InviteActIgnore:
			c = &a.ignore
		default:
			c = &a.accept
		}
		if (x == core.InviteActAcceptDM || x == core.InviteActAcceptChannel) && v.flights.busy(ci.ID) {
			for c.Clicked(gtx) { // loading: not pressable (clicks while it runs send nothing later)
			}
			continue
		}
		if c.Clicked(gtx) {
			for c.Clicked(gtx) { // one flow per frame: a double click's second lands on the modal it opened
			}
			if x == core.InviteActCopyLink {
				cdCopy(gtx, ci.Link)
				a.copiedAt = gtx.Now
				continue
			}
			v.act(ci, x)
		}
	}
	left, when, tile, primary, secondary, foot, footText := v.cardText(ci, state, now)
	line := dirRing(&d.base, d.p.Surface)
	w := gtx.Constraints.Max.X
	return layout.Inset{Top: 8, Bottom: 8}.Layout(gtx, func(gtx C) D {
		m := op.Record(gtx.Ops)
		y := 0
		// header
		hd := dirAt(gtx, image.Pt(0, 0), func(gtx C) D {
			gtx.Constraints = layout.Constraints{Min: image.Pt(w, 0), Max: image.Pt(w, 1<<20)}
			return layout.Inset{Top: 8, Bottom: 4, Left: 16, Right: 16}.Layout(gtx, func(gtx C) D {
				gtx.Constraints.Min.X = gtx.Constraints.Max.X
				return row(gtx, layout.Baseline,
					layout.Flexed(2, func(gtx C) D {
						return dirFill(gtx, func(gtx C) D { return d.para(gtx, 13, 18, wReg, d.p.Muted, 1, left) })
					}),
					layout.Rigid(func(gtx C) D { return d.label(gtx, 13, wReg, d.p.Muted, when) }),
				)
			})
		})
		y += hd.Size.Y
		bd := dirAt(gtx, image.Pt(0, y), func(gtx C) D {
			gtx.Constraints = layout.Constraints{Min: image.Pt(w, 0), Max: image.Pt(w, 1<<20)}
			return layout.Inset{Left: 16, Right: 16, Bottom: 12}.Layout(gtx, func(gtx C) D {
				gtx.Constraints.Min.X = gtx.Constraints.Max.X
				return row(gtx, layout.Middle,
					layout.Rigid(func(gtx C) D {
						return layout.Inset{Top: 2, Bottom: 2, Right: 12}.Layout(gtx, func(gtx C) D { return v.cardIcon(gtx, ci, tile, state) })
					}),
					layout.Flexed(1, func(gtx C) D {
						return layout.Inset{Right: 16}.Layout(gtx, func(gtx C) D {
							return col(gtx,
								layout.Rigid(func(gtx C) D { return d.para(gtx, 15, 22, wBold, d.p.Text, 1, primary) }),
								layout.Rigid(func(gtx C) D {
									if secondary == "" {
										return D{}
									}
									return d.para(gtx, 13, 18, wReg, d.p.Muted, 1, secondary)
								}),
							)
						})
					}),
					layout.Rigid(func(gtx C) D { return v.cardActions(gtx, ci, acts, a) }),
				)
			})
		})
		y += bd.Size.Y
		topH := y
		if footText != "" {
			fd := dirAt(gtx, image.Pt(0, y), func(gtx C) D {
				gtx.Constraints = layout.Constraints{Min: image.Pt(w, 0), Max: image.Pt(w, 1<<20)}
				return layout.Inset{Top: 8, Bottom: 8, Left: 16, Right: 16}.Layout(gtx, func(gtx C) D {
					fg := d.p.Muted
					switch foot {
					case "approved":
						fg = rgb(0x007a5a)
					case "rejected":
						fg = d.p.Red
					}
					return row(gtx, layout.Middle,
						layout.Rigid(func(gtx C) D {
							return layout.Inset{Right: 4}.Layout(gtx, func(gtx C) D { return d.icon(gtx, scFooterIcon(state), 13, fg) })
						}),
						layout.Flexed(1, func(gtx C) D { return d.para(gtx, 13, 18, wReg, d.p.Muted, 1, footText) }),
					)
				})
			})
			y += fd.Size.Y
		}
		call := m.Stop()
		rad := gtx.Dp(8)
		rrect(gtx, image.Rect(0, 0, w, y), rad, line)
		inner := image.Rect(1, 1, w-1, y-1)
		rrect(gtx, inner, rad-1, d.p.Surface)
		if footText != "" {
			fbg := d.p.Surface
			switch foot {
			case "approved":
				fbg = flatten(argb(0x0d007a5a), d.p.Surface)
			case "rejected":
				fbg = flatten(argb(0x0de01e5a), d.p.Surface)
			case "inactive":
				fbg = flatten(theme4b(&d.base, 0x0a), d.p.Surface)
			}
			r := image.Rect(1, topH, w-1, y-1)
			fill(gtx, r, fbg)
			fill(gtx, image.Rect(0, topH, w, topH+max(1, gtx.Dp(1))), line)
		}
		call.Add(gtx.Ops)
		return D{Size: image.Pt(w, y)}
	})
}

func scFooterIcon(state string) string {
	switch state {
	case "rejected", "denied":
		return "close"
	case "requested", "pending_approval":
		return "hourglass"
	case "approved":
		return "check-filled"
	case "revoked":
		return "disable"
	case "expired":
		return "clock"
	}
	return ""
}

// cardText is the rows' texts for invite ci in state.
func (v *scInvites) cardText(ci core.ConnectInvite, state string, now time.Time) (left, when, tile, primary, secondary, foot, footText string) {
	d := v.d
	when = scWhen(time.Unix(ci.DateCreated, 0), now)
	ch := "#" + ci.ChannelName
	var acc *core.InviteAcceptor
	if len(ci.Acceptors) > 0 {
		acc = &ci.Acceptors[0]
	}
	expiry := ""
	if ci.DateInvalid > 0 && state != "expired" && state != "revoked" && state != "denied" && (acc == nil || acc.Status != "approved") {
		expiry = nsSC.T("Expires {calendarDateOrNamedDay}", "calendarDateOrNamedDay", scNamedDay(time.Unix(ci.DateInvalid, 0), now))
	}
	if ci.Direction == "outgoing" {
		switch {
		case ci.IsEmail() && ci.Kind == "channel":
			left = nsSC.T("{headerChannelCopy} {channel}:", "headerChannelCopy", nsSC.T("Invited to"), "channel", ch)
			if ci.Status == "requested" {
				left = nsSC.T("{headerChannelCopy} {channel}:", "headerChannelCopy", nsSC.T("Invitation not yet sent to"), "channel", ch)
				if ci.RequestDenied != 0 || state == "expired" {
					left = nsSC.T("{headerChannelCopy} {channel}:", "headerChannelCopy", nsSC.T("Invitation not sent to"), "channel", ch)
				}
			}
			tile = "channel"
		case ci.IsEmail():
			left, tile = nsSC.T("Invited to direct message:"), "dm"
		case ci.Kind == "channel":
			left, tile = nsSC.T("Created invite link to:"), "channel"
		default:
			left, tile = nsSC.T("Created invite link to:"), "dm"
		}
		switch {
		case acc != nil && acc.UserName != "":
			primary, secondary, tile = acc.UserName, acc.TeamName, "user:"+acc.UserID
		case ci.IsEmail():
			primary, secondary = ci.RecipientEmail, expiry
		case ci.Kind == "channel":
			primary, secondary = ch, expiry
		default:
			primary, secondary = nsSC.T("Direct message"), expiry
		}
	} else {
		inviter := scInviterName(&d.base, core.ActivityItem{Actor: ci.InviterID, Invite: &ci.ActivityInvite})
		if ci.Kind == "channel" {
			left = nsSC.T("{user} from {team} invited you to join:", "user", inviter, "team", ci.InvitingTeam)
			primary, tile = ch, "team"
		} else {
			left = nsSC.T("{user} from {team} invited you to join:", "user", inviter, "team", ci.InvitingTeam)
			primary, tile = nsSC.T("Direct message"), "dm-received"
		}
		secondary = ci.Message
		if secondary == "" {
			secondary = expiry
		}
	}
	switch state {
	case "rejected":
		foot, footText = "rejected", nsSC.T("Denied by your admin")
	case "requested":
		footText = nsSC.T("Awaiting admin approval")
	case "denied":
		foot, footText = "rejected", nsSC.T("Denied by your admin")
	case "approved":
		foot, footText = "approved", nsSC.T("Joined")
	case "pending_approval":
		if acc != nil && acc.TeamName != "" {
			footText = nsSC.T("Awaiting admin approval from {team}", "team", acc.TeamName)
		} else {
			footText = nsSC.T("Awaiting admin approval")
		}
	case "revoked":
		foot, footText = "inactive", nsSC.T("Invitation has been revoked")
	case "expired":
		foot, footText = "inactive", nsSC.T("Invitation has expired")
	}
	return
}

// cardIcon is the card's 36px icon: an email invite's channel or DM tile
// (#11a489 on #11a4891a; a received DM's #007a5a), the person who took it
// up, the inviting team's avatar; grayscale-looking when inactive.
func (v *scInvites) cardIcon(gtx C, ci core.ConnectInvite, tile, state string) D {
	d := v.d
	s := gtx.Dp(36)
	sz := image.Pt(s, s)
	inactive := state == "expired" || state == "revoked"
	switch {
	case strings.HasPrefix(tile, "user:"):
		url := ""
		if u, ok := d.user(strings.TrimPrefix(tile, "user:")); ok {
			url = u.Avatar
		}
		return dirImage(&d.base, gtx, url, sz, gtx.Dp(4))
	case tile == "team":
		t, _ := d.h.Core().ExternalTeamByID(ci.TeamID)
		if t.ID == "" {
			t = core.ExternalTeam{ID: ci.TeamID, Name: ci.InvitingTeam, Icon: ci.TeamIcon}
		}
		return scTeamAvatar(&d.base, gtx, t, 36)
	}
	fg, bg := rgb(0x11a489), argb(0x1a11a489)
	name := "shared-channel"
	switch tile {
	case "dm":
		name = "comment"
	case "dm-received":
		name, fg, bg = "comment", rgb(0x007a5a), argb(0x1a007a5a)
	}
	if inactive {
		fg, bg = d.p.Faint, theme4b(&d.base, 0x14)
	}
	rrect(gtx, image.Rectangle{Max: sz}, gtx.Dp(4), flatten(bg, d.p.Surface))
	g := gtx
	g.Constraints = layout.Exact(sz)
	layout.Center.Layout(g, func(gtx C) D { return d.icon(gtx, name, 20, fg) })
	return D{Size: sz}
}

// cardActions is SlackConnectInviteActions: the text actions (Ignore small
// outline, Accept small primary; 4px apart) and the icon actions (Copy
// link, Revoke invitation / Disable link: smedium icon buttons in a ringed
// group, with their tooltips).
func (v *scInvites) cardActions(gtx C, ci core.ConnectInvite, acts []string, a *scInvActs) D {
	d := v.d
	var ch []layout.FlexChild
	for _, x := range acts {
		switch x {
		case core.InviteActIgnore:
			ch = append(ch, layout.Rigid(func(gtx C) D {
				return layout.Inset{Left: 4}.Layout(gtx, func(gtx C) D { return d.button(gtx, &a.ignore, btnOutline, true, nsSC.T("Ignore")) })
			}))
		case core.InviteActAcceptDM, core.InviteActAcceptChannel:
			ch = append(ch, layout.Rigid(func(gtx C) D {
				return layout.Inset{Left: 4}.Layout(gtx, func(gtx C) D {
					if v.flights.busy(ci.ID) { // loading while its flow runs (c-button--small: min-width 56px)
						return popups.LoadingButtonWake(gtx, d.th, &a.accept, popups.BtnSmall, 56, tipWake(d.h, &a.wk))
					}
					return d.button(gtx, &a.accept, btnPrimary, true, nsSC.T("Accept"))
				})
			}))
		case core.InviteActCopyLink:
			ch = append(ch, layout.Rigid(func(gtx C) D {
				tip := nsSC.T("Copy link")
				if !a.copiedAt.IsZero() && gtx.Now.Sub(a.copiedAt) < 2*time.Second {
					tip = nsSC.T("Link copied")
					a.wk.schedule(d.h, gtx.Now, a.copiedAt.Add(2*time.Second))
				}
				bd := layout.Inset{Left: 4}.Layout(gtx, func(gtx C) D { return d.iconButton(gtx, &a.copy, "link", 28, 16, d.p.Sec) })
				popups.Tooltip(gtx, d.th, &a.tipCopy, a.copy.Hovered(), bd.Size, popups.Above, tip, "")
				return bd
			}))
		case core.InviteActRevoke:
			ch = append(ch, layout.Rigid(func(gtx C) D {
				tip := nsSC.T("Revoke invitation")
				if !ci.IsEmail() {
					tip = nsSC.T("Disable link")
				}
				bd := layout.Inset{Left: 4}.Layout(gtx, func(gtx C) D { return d.iconButton(gtx, &a.revoke, "disable", 28, 16, d.p.Sec) })
				popups.Tooltip(gtx, d.th, &a.tipRevoke, a.revoke.Hovered(), bd.Size, popups.Above, tip, "")
				return bd
			}))
		}
	}
	if len(ch) == 0 {
		return D{}
	}
	return row(gtx, layout.Middle, ch...)
}

// scWhen is the cards' when (s32x QE → toHumanReadableDateAndTime): "Today
// at 3:04 PM", "Yesterday at …", else "Nov 3rd at 3:04 PM" (with the year
// when it isn't this year).
func scWhen(t, now time.Time) string {
	if t.Unix() <= 0 {
		return ""
	}
	day := scNamedDay(t, now)
	if len(day) > 0 {
		day = strings.ToUpper(day[:1]) + day[1:]
	}
	return day + " at " + clock(t)
}

// scNamedDay is calendarDateOrNamedDay (uncapitalized): today, tomorrow,
// yesterday, else "Nov 3rd" ("Nov 3rd, 2024" in another year).
func scNamedDay(t, now time.Time) string {
	switch {
	case sameDay(t, now):
		return "today"
	case sameDay(t, now.AddDate(0, 0, 1)):
		return "tomorrow"
	case sameDay(t, now.AddDate(0, 0, -1)):
		return "yesterday"
	}
	s := t.Format("Jan 2") + ordinal(t.Day())
	if t.Year() != now.Year() {
		s += ", " + t.Format("2006")
	}
	return s
}

var _ = unit.Dp(0)
