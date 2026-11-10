package popups

import (
	"context"
	"image"
	"image/color"
	"sort"
	"strings"

	"gioui.org/io/key"
	"gioui.org/io/pointer"
	"gioui.org/layout"
	"gioui.org/op/clip"
	"gioui.org/unit"
	"gioui.org/widget"

	"github.com/some-du6e/clack/internal/core"
	"github.com/some-du6e/clack/internal/ui/theme"
)

// The app card's "Add this app to a channel…" (boot-render f0+8
// createAppProfileCardMenu channel_invite) dispatches
// openInviteMemberToChannelDialog (boot-shared qrpS):
//
//   - You a single-channel guest: "You are not allowed to add other
//     members to channels."; the member one: "<name> cannot be added to any
//     new channels."; the member a guest and you no admin: "Only a
//     Workspace Admin can add <name> into new channels." Each under the
//     title "Add <name> to a channel" with only OK.
//   - Else BaseInviteMemberToChannelDialog: "Add <name> to a channel", a
//     SearchSelect "Select a channel" over your channels (not archived,
//     by name; .p-invite_member_to_channel_dialog__body: min-height 64,
//     padding-top 4) and Add, disabled until a channel is picked. Picking
//     one asks getChannelMembershipInfoForUsers (edge channels/membership,
//     core.ChannelMembership; Add pending meanwhile); a member already
//     there keeps Add disabled under "<name> is already a member of this
//     channel." (inline error, margin-top 8).
//   - Add: a bot goes through addAppToChannel (boot-shared XcD2):
//     apps.permissions.internal.add unconfirmed (core.AddAppToChannel)
//     for the scope_info, then AppPermissionDialog (H+hS); a person
//     through submitInviteToChannel (conversations.invite, core.Invite).
//
// AppPermissionDialog: 420 wide, no header (the close × at the corner),
// body padding 40px 24px 0: the app's 48px icon centred, the h3 "<app>
// would like to do the following in <#channel>:" (normal weight, centred;
// "…in your direct message with …" for a DM), OauthScopeInfo (each scope's
// short description, padding 18px 16px between 1px rules; dangerous ones
// in a box with a 5px #f2c744 left border and the warning-filled icon),
// then the footer (1px #ddd rule, padding 24): "Don’t Allow" (cancel,
// #f8f8f8) and "Allow" side by side at full width, 16 apart. Allow sends
// the call confirmed; Don’t Allow sends apps.permissions.internal.denyAdd;
// the × only closes.

// pfOpenInviteToChannel is openInviteMemberToChannelDialog for memberID;
// it returns the picker dialog (nil when a notice or nothing opened).
func pfOpenInviteToChannel(h Host, memberID string) *pfInviteDialog {
	c := h.Core()
	if c == nil || memberID == "" {
		return nil
	}
	u, ok := lookupUser(h, memberID)
	if !ok {
		u = core.User{ID: memberID, IsBot: true} // the app card's bot user, not loaded yet
	}
	name := pfMemberName(u)
	title := "Add " + name + " to a channel"
	self := c.Self()
	_, selfUltra := pfRestriction(h, self.ID)
	restricted, ultra := pfRestriction(h, memberID)
	notice := func(msg string) {
		h.ShowOverlay(NewModal(h, ModalOpts{Title: title, EnterSubmits: true,
			Body: func(gtx layout.Context) layout.Dimensions {
				return para(gtx, h.Theme(), 15, 22, theme.Regular, colorsOf(h.Theme()).Fg, msg, 0)
			},
			Buttons: []*ModalButton{{Label: "OK", Kind: BtnPrimary, Do: func() bool { return true }}}}))
	}
	switch {
	case selfUltra:
		notice("You are not allowed to add other members to channels.")
	case ultra:
		notice(name + " cannot be added to any new channels.")
	case restricted && !self.IsAdmin && !self.IsOwner:
		notice("Only a Workspace Admin can add " + name + " into new channels.")
	default:
		d := &pfInviteDialog{h: h, member: u, name: name, chans: pfInviteChannels(c)}
		d.field.SingleLine, d.field.Submit = true, true
		d.focusNext = true // the select takes the focus on open
		add := &ModalButton{Label: "Add", Kind: BtnPrimary,
			Enabled: func() bool { return d.state == pfInviteEnabled },
			Do:      func() bool { return d.add() }}
		d.modal = NewModal(h, ModalOpts{Title: title, Body: d.layout, Buttons: []*ModalButton{add}})
		h.ShowOverlay(d.modal)
		return d
	}
	return nil
}

// pfMemberName is prefCompliantMemberName(member, false): the display
// name, else the real name.
func pfMemberName(u core.User) string {
	if n := pfMentionName(u); n != "" {
		return n
	}
	return pfName(u)
}

// pfRestriction reads a member's is_restricted and is_ultra_restricted
// (core.User merges them into Guest).
func pfRestriction(h Host, id string) (restricted, ultra bool) {
	c := h.Core()
	if c == nil || id == "" {
		return false, false
	}
	c.S.RLock()
	defer c.S.RUnlock()
	if u := c.S.Users[id]; u != nil {
		return u.IsRestricted || u.IsUltraRestricted, u.IsUltraRestricted
	}
	return false, false
}

// pfInviteChannels are the dialog's options: getMemberChannels (your
// public and private channels, not archived), sorted by name.
func pfInviteChannels(c *core.App) []core.Conv {
	var out []core.Conv
	for _, cv := range c.SnapshotNow().Convs {
		if (cv.Kind == "channel" || cv.Kind == "private") && !cv.IsArchived && !cv.Preview && cv.Code == nil {
			out = append(out, cv)
		}
	}
	sort.SliceStable(out, func(i, j int) bool { return strings.ToLower(out[i].Name) < strings.ToLower(out[j].Name) })
	return out
}

// Add button states (BaseInviteMemberToChannelDialog addButtonState).
const (
	pfInviteDisabled = iota
	pfInviteLoading
	pfInviteEnabled
)

// pfInviteDialog is BaseInviteMemberToChannelDialog's state.
type pfInviteDialog struct {
	h      Host
	modal  *Modal
	member core.User
	name   string
	chans  []core.Conv

	field    widget.Editor
	fieldBox widget.Clickable
	open     bool // the options list shows (the field focused)
	rows     []widget.Clickable
	matches  []int

	sel       string        // selectedChannelId
	perm      *pfPermDialog // the AppPermissionDialog Add opened
	state     int
	isMember  bool
	selSeq    int
	focusNext bool
}

// filter is the SearchSelect's matching: options whose name holds the
// typed text (without a leading #), at most eight.
func (d *pfInviteDialog) filter() {
	q := strings.ToLower(strings.TrimPrefix(strings.TrimSpace(d.field.Text()), "#"))
	d.matches = d.matches[:0]
	for i, cv := range d.chans {
		if q == "" || strings.Contains(strings.ToLower(cv.Name), q) {
			d.matches = append(d.matches, i)
			if len(d.matches) == 8 {
				break
			}
		}
	}
	if len(d.rows) < len(d.matches) {
		d.rows = make([]widget.Clickable, len(d.matches))
	}
}

// pick is onChannelSelected: the channel is selected and its membership
// asked for; Add waits (loading) until it answers.
func (d *pfInviteDialog) pick(id string) {
	h, c := d.h, d.h.Core()
	d.sel, d.state, d.isMember, d.open = id, pfInviteLoading, false, false
	d.field.SetText("")
	d.selSeq++
	seq, member := d.selSeq, d.member.ID
	run(h, func(ctx context.Context) (map[string]bool, error) {
		return c.ChannelMembership(ctx, id, []string{member})
	}, func(m map[string]bool, err error) {
		if seq != d.selSeq {
			return
		}
		if err != nil {
			d.state = pfInviteEnabled // Slack's catch keeps the dialog usable
			h.Invalidate()
			return
		}
		d.isMember = m[member]
		d.state = pfInviteEnabled
		if d.isMember {
			d.state = pfInviteDisabled
		}
		h.Invalidate()
	})
}

// add is onAddToChannel.
func (d *pfInviteDialog) add() bool {
	if d.sel == "" {
		return false
	}
	h, c := d.h, d.h.Core()
	ch, member := d.sel, d.member.ID
	if d.member.IsBot {
		pfAddAppToChannel(h, member, ch, func(p *pfPermDialog) { d.perm = p })
		return true
	}
	do(h, "", func(ctx context.Context) error { return c.Invite(ctx, ch, member) })
	return true
}

func (d *pfInviteDialog) layout(gtx layout.Context) layout.Dimensions {
	th := d.h.Theme()
	cl := colorsOf(th)
	w := gtx.Constraints.Max.X
	if d.fieldBox.Clicked(gtx) {
		d.focusNext = true
	}
	if d.focusNext {
		gtx.Execute(key.FocusCmd{Tag: &d.field})
		d.focusNext = false
	}
	for {
		ev, ok := d.field.Update(gtx)
		if !ok {
			break
		}
		switch ev.(type) {
		case widget.ChangeEvent:
			d.open = true
		case widget.SubmitEvent:
			d.filter()
			if len(d.matches) > 0 {
				d.pick(d.chans[d.matches[0]].ID)
			}
		}
	}
	focused := gtx.Focused(&d.field)
	if focused && !d.open && d.field.Len() == 0 {
		d.open = true
	}
	d.filter()
	y := dp(gtx, 4) // .p-invite_member_to_channel_dialog__body padding-top
	boxH := dp(gtx, 36)
	box := image.Rect(0, y, w, y+boxH)
	actFocusBox(gtx, cl, box, focused)
	at(gtx, box.Min, func(gtx layout.Context) layout.Dimensions {
		return d.fieldBox.Layout(exact(gtx, box.Size()), func(gtx layout.Context) layout.Dimensions {
			defer clip.Rect{Max: box.Size()}.Push(gtx.Ops).Pop()
			pointer.CursorText.Add(gtx.Ops)
			return layout.Dimensions{Size: box.Size()}
		})
	})
	padX := dp(gtx, 12)
	fw := w - 2*padX - dp(gtx, 24)
	at(gtx, image.Pt(box.Min.X+padX, box.Min.Y), func(gtx layout.Context) layout.Dimensions {
		return vcenter(gtx, boxH, func(gtx layout.Context) layout.Dimensions {
			gtx.Constraints = layout.Constraints{Min: image.Pt(fw, 0), Max: image.Pt(fw, boxH)}
			if d.field.Len() == 0 {
				call, _ := record(gtx, func(gtx layout.Context) layout.Dimensions {
					if d.sel != "" && !focused { // renderSelectedOption: the channel
						return pfChannelLabel(gtx, d.h, th, cl.Fg, d.channel(d.sel))
					}
					return text(gtx, th, 15, theme.Regular, cl.Placeholder, "Select a channel")
				})
				call.Add(gtx.Ops)
			}
			return th.Editor(gtx, &d.field, unit.Sp(15), cl.Fg, theme.WithAlpha(cl.Focus, 0x55))
		})
	})
	at(gtx, image.Pt(box.Max.X-padX-dp(gtx, 16), box.Min.Y+(boxH-dp(gtx, 16))/2), func(gtx layout.Context) layout.Dimensions {
		return icon(gtx, "chevron-down", 16, cl.Fg2)
	})
	y = box.Max.Y
	if d.open && focused {
		y += dp(gtx, 4)
		rowH := dp(gtx, 36)
		if len(d.matches) == 0 {
			at(gtx, image.Pt(padX, y), func(gtx layout.Context) layout.Dimensions {
				return vcenter(gtx, rowH, func(gtx layout.Context) layout.Dimensions {
					return text(gtx, th, 15, theme.Regular, cl.Fg2, "No matches")
				})
			})
			y += rowH
		}
		for i, ci := range d.matches {
			cv := d.chans[ci]
			c := &d.rows[i]
			if c.Clicked(gtx) {
				d.pick(cv.ID)
			}
			ry := y
			at(gtx, image.Pt(0, ry), func(gtx layout.Context) layout.Dimensions {
				return clickBox(gtx, c, func(gtx layout.Context, sz image.Point, hovered bool) {
					if hovered {
						fill(gtx, image.Rectangle{Max: sz}, cl.Highlight)
					}
				}, func(gtx layout.Context) layout.Dimensions {
					fg := cl.Fg
					if c.Hovered() {
						fg = cl.HighText
					}
					at(gtx, image.Pt(padX, 0), func(gtx layout.Context) layout.Dimensions {
						gtx.Constraints = layout.Constraints{Max: image.Pt(w-2*padX, rowH)}
						return vcenter(gtx, rowH, func(gtx layout.Context) layout.Dimensions {
							return pfChannelLabel(gtx, d.h, th, fg, cv)
						})
					})
					return layout.Dimensions{Size: image.Pt(w, rowH)}
				})
			})
			y += rowH
		}
	}
	if d.isMember {
		y += dp(gtx, 8)
		msg := d.name + " is already a member of this channel."
		ed := at(gtx, image.Pt(0, y), func(gtx layout.Context) layout.Dimensions {
			gtx.Constraints = layout.Constraints{Max: image.Pt(w, 1<<20)}
			return pfInlineError(gtx, th, msg)
		})
		y += ed.Size.Y
	}
	return layout.Dimensions{Size: image.Pt(w, maxInt(y, dp(gtx, 64)))}
}

// channel is the option with id (a stub with the id while unknown).
func (d *pfInviteDialog) channel(id string) core.Conv {
	for _, cv := range d.chans {
		if cv.ID == id {
			return cv
		}
	}
	return core.Conv{ID: id, Name: id, Kind: "channel"}
}

// pfChannelLabel is ChannelName with its icon: # (or the lock of a
// private channel) and the name.
func pfChannelLabel(gtx layout.Context, h Host, th *theme.Theme, fg color.NRGBA, cv core.Conv) layout.Dimensions {
	ic := "hash"
	if cv.Kind == "private" {
		ic = "lock"
	}
	isz := dp(gtx, 16)
	lh := dp(gtx, 22)
	at(gtx, image.Pt(0, (lh-isz)/2), func(gtx layout.Context) layout.Dimensions {
		return icon(gtx, ic, 16, fg)
	})
	x := isz + dp(gtx, 6)
	d := at(gtx, image.Pt(x, 0), func(gtx layout.Context) layout.Dimensions {
		gtx.Constraints = layout.Constraints{Max: image.Pt(maxInt(1, gtx.Constraints.Max.X-x), lh)}
		return para(gtx, th, 15, 22, theme.Regular, fg, cv.Name, 1)
	})
	return layout.Dimensions{Size: image.Pt(x+d.Size.X, lh)}
}

// pfInlineError is an inline error Alert (type inline, level error): the
// warning glyph in the danger colour and the 15px message.
func pfInlineError(gtx layout.Context, th *theme.Theme, msg string) layout.Dimensions {
	cl := colorsOf(th)
	w := gtx.Constraints.Max.X
	at(gtx, image.Pt(0, dp(gtx, 2)), func(gtx layout.Context) layout.Dimensions {
		return icon(gtx, "warning", 18, cl.Danger)
	})
	x := dp(gtx, 24)
	d := at(gtx, image.Pt(x, 0), func(gtx layout.Context) layout.Dimensions {
		gtx.Constraints = layout.Constraints{Max: image.Pt(maxInt(1, w-x), 1<<20)}
		return para(gtx, th, 15, 22, theme.Regular, cl.Fg, msg, 0)
	})
	return layout.Dimensions{Size: image.Pt(w, maxInt(d.Size.Y, dp(gtx, 22)))}
}

// pfAddAppToChannel is addAppToChannel: the unconfirmed
// apps.permissions.internal.add, then AppPermissionDialog with its
// scope_info.
func pfAddAppToChannel(h Host, appUser, ch string, shown func(*pfPermDialog)) {
	c := h.Core()
	if c == nil {
		return
	}
	run(h, func(ctx context.Context) ([]core.AppScope, error) {
		return c.AddAppToChannel(ctx, appUser, ch, false)
	}, func(scopes []core.AppScope, err error) {
		if err != nil {
			h.Toast("Couldn't do that: " + errText(err))
			return
		}
		p := newAppPermissionDialog(h, appUser, ch, scopes)
		h.ShowOverlay(p.modal)
		if shown != nil {
			shown(p)
		}
	})
}

// pfPermDialog is AppPermissionDialog's state.
type pfPermDialog struct {
	h             Host
	modal         *Modal
	app, ch       string
	scopes        []core.AppScope
	allow, deny   widget.Clickable
	more          []widget.Clickable
	expanded      []bool
	confirmInProg bool
}

func newAppPermissionDialog(h Host, appUser, ch string, scopes []core.AppScope) *pfPermDialog {
	d := &pfPermDialog{h: h, app: appUser, ch: ch, scopes: scopes,
		more: make([]widget.Clickable, len(scopes)), expanded: make([]bool, len(scopes))}
	d.modal = NewModal(h, ModalOpts{Width: 420, HeadH: 40, Flush: true, Body: d.layout, Class: "p-app_permission_dialog"})
	return d
}

// allowNow is Allow (onAuthorize): the call again, confirmed.
func (d *pfPermDialog) allowNow() {
	c := d.h.Core()
	if d.confirmInProg || c == nil {
		return
	}
	d.confirmInProg = true // goButtonIsPending; shouldCloseOnGo
	app, ch := d.app, d.ch
	d.modal.Dismiss()
	do(d.h, "", func(ctx context.Context) error {
		_, err := c.AddAppToChannel(ctx, app, ch, true)
		return err
	})
}

// denyNow is Don't Allow (onDismiss): apps.permissions.internal.denyAdd.
func (d *pfPermDialog) denyNow() {
	c := d.h.Core()
	if c == nil {
		return
	}
	app, ch := d.app, d.ch
	d.modal.Dismiss()
	do(d.h, "", func(ctx context.Context) error { return c.DenyAddAppToChannel(ctx, app, ch) })
}

func (d *pfPermDialog) layout(gtx layout.Context) layout.Dimensions {
	h := d.h
	th := h.Theme()
	cl := colorsOf(th)
	w := gtx.Constraints.Max.X
	if d.allow.Clicked(gtx) {
		d.allowNow()
	}
	if d.deny.Clicked(gtx) {
		d.denyNow()
	}
	pad := dp(gtx, 24)
	inner := maxInt(1, w-2*pad)
	y := 0
	u, ok := lookupUser(h, d.app)
	// the 48px app icon, centred
	av := dp(gtx, 48)
	at(gtx, image.Pt((w-av)/2, y), func(gtx layout.Context) layout.Dimensions {
		if !ok {
			rrect(gtx, image.Rect(0, 0, av, av), dp(gtx, 6), cl.Selected)
			return layout.Dimensions{Size: image.Pt(av, av)}
		}
		return pfImage(gtx, h, pfAvatarURL(u, false), 48, 6)
	})
	y += av + dp(gtx, 16)
	// the title (h3: normal weight, centred, margins 16)
	appName := pfName(u)
	cv, im := pfConvLabel(h, d.ch)
	pre := appName + " would like to do the following in "
	if im {
		pre = appName + " would like to do the following in your direct message with "
	}
	td := at(gtx, image.Pt(pad, y), func(gtx layout.Context) layout.Dimensions {
		gtx.Constraints = layout.Constraints{Max: image.Pt(inner, 1<<20)}
		return pfCenteredRuns(gtx, th, cl, inner, []pfRun{{s: pre}, {s: cv, mention: true}, {s: ":"}})
	})
	y += td.Size.Y + dp(gtx, 16)
	// OauthScopeInfo: safe scopes, then the dangerous ones boxed
	var safe, danger []int
	for i, s := range d.scopes {
		if s.Dangerous {
			danger = append(danger, i)
		} else {
			safe = append(safe, i)
		}
	}
	rule := func(y int) { fill(gtx, image.Rect(pad, y, pad+inner, y+1), cl.Ring) }
	scope := func(i int, x, y, sw int, dangerous bool) int {
		s := d.scopes[i]
		if d.more[i].Clicked(gtx) {
			d.expanded[i] = !d.expanded[i]
		}
		px, py := dp(gtx, 16), dp(gtx, 18)
		if dangerous {
			px, py = dp(gtx, 12), dp(gtx, 16)
		}
		tx := x + px
		if dangerous {
			at(gtx, image.Pt(tx, y+py+dp(gtx, 2)), func(gtx layout.Context) layout.Dimensions {
				return icon(gtx, "warning", 18, rgb(0xf2c744))
			})
			tx += dp(gtx, 24)
		}
		tw := maxInt(1, x+sw-dp(gtx, 16)-tx)
		if s.Long != "" {
			tw -= dp(gtx, 24)
		}
		dd := at(gtx, image.Pt(tx, y+py), func(gtx layout.Context) layout.Dimensions {
			gtx.Constraints = layout.Constraints{Max: image.Pt(tw, 1<<20)}
			return para(gtx, th, 15, 22, theme.Regular, cl.Fg, s.Short, 0)
		})
		hgt := dd.Size.Y
		if s.Long != "" {
			ic := "caret-right"
			if d.expanded[i] {
				ic = "caret-down"
			}
			at(gtx, image.Pt(tx+tw+dp(gtx, 4), y+py), func(gtx layout.Context) layout.Dimensions {
				return clickBox(gtx, &d.more[i], nil, func(gtx layout.Context) layout.Dimensions {
					return icon(gtx, ic, 20, cl.Fg2)
				})
			})
			if d.expanded[i] {
				ld := at(gtx, image.Pt(tx, y+py+hgt+dp(gtx, 4)), func(gtx layout.Context) layout.Dimensions {
					gtx.Constraints = layout.Constraints{Max: image.Pt(maxInt(1, x+sw-dp(gtx, 16)-tx), 1<<20)}
					return para(gtx, th, 15, 22, theme.Regular, cl.Fg2, s.Long, 0)
				})
				hgt += dp(gtx, 4) + ld.Size.Y
			}
		}
		return py + hgt + py
	}
	for k, i := range safe {
		if k > 0 {
			rule(y)
			y++
		}
		y += scope(i, pad, y, inner, false)
	}
	if len(safe) > 0 {
		rule(y)
		y++
	}
	if len(danger) > 0 {
		y0 := y
		yy := y + 1
		for k, i := range danger {
			if k > 0 {
				fill(gtx, image.Rect(pad+dp(gtx, 5), yy, pad+inner, yy+1), cl.Ring)
				yy++
			}
			yy += scope(i, pad+dp(gtx, 5), yy, inner-dp(gtx, 5), true)
		}
		box := image.Rect(pad, y0, pad+inner, yy+1)
		stroke(gtx, box, dp(gtx, 4), 1, cl.Ring)
		fill(gtx, image.Rect(box.Min.X, box.Min.Y, box.Min.X+dp(gtx, 5), box.Max.Y), rgb(0xf2c744))
		y = box.Max.Y + dp(gtx, 24)
	}
	y += pad
	// the footer: a full-width rule, padding 24, the two buttons
	fill(gtx, image.Rect(0, y, w, y+1), rgb(0xdddddd))
	y++
	y += pad
	bw := (inner - dp(gtx, 16)) / 2
	bwDp := float32(bw) / gtx.Metric.PxPerDp
	if gtx.Metric.PxPerDp == 0 {
		bwDp = float32(bw)
	}
	at(gtx, image.Pt(pad, y), func(gtx layout.Context) layout.Dimensions {
		return Button(gtx, th, &d.deny, BtnOutline, BtnMedium, "Don’t Allow", "", bwDp)
	})
	at(gtx, image.Pt(pad+bw+dp(gtx, 16), y), func(gtx layout.Context) layout.Dimensions {
		return Button(gtx, th, &d.allow, BtnPrimary, BtnMedium, "Allow", "", bwDp)
	})
	y += dp(gtx, 36) + pad
	return layout.Dimensions{Size: image.Pt(w, y)}
}

// pfConvLabel is ChannelName for ch: "#name" (a DM: the person's name)
// and whether ch is a DM or group DM.
func pfConvLabel(h Host, ch string) (string, bool) {
	c := h.Core()
	if c == nil {
		return ch, false
	}
	for _, cv := range c.SnapshotNow().Convs {
		if cv.ID != ch {
			continue
		}
		switch cv.Kind {
		case "im":
			if u, ok := lookupUser(h, cv.User); ok {
				return pfName(u), true
			}
			return cv.Name, true
		case "mpim":
			return cv.Name, true
		}
		return "#" + cv.Name, false
	}
	if r := c.ConvRef(ch); r.Name != "" {
		return "#" + r.Name, false
	}
	return ch, false
}

// pfCenteredRuns lays runs out as one centred paragraph (15/22; a mention
// run bold, ChannelName's className "bold").
func pfCenteredRuns(gtx layout.Context, th *theme.Theme, cl *Colors, w int, runs []pfRun) layout.Dimensions {
	type word struct {
		s    string
		bold bool
		w    int
	}
	var words []word
	for _, r := range runs {
		wt := theme.Regular
		if r.mention {
			wt = theme.Bold
		}
		for _, f := range strings.SplitAfter(r.s, " ") {
			if f == "" {
				continue
			}
			words = append(words, word{f, r.mention, textW(gtx, th, 15, wt, f)})
		}
	}
	lineH := dp(gtx, 22)
	var lines [][]word
	var cur []word
	cw := 0
	for _, wd := range words {
		if cw > 0 && cw+wd.w > w {
			lines = append(lines, cur)
			cur, cw = nil, 0
		}
		cur = append(cur, wd)
		cw += wd.w
	}
	if len(cur) > 0 {
		lines = append(lines, cur)
	}
	for i, ln := range lines {
		lw := 0
		for _, wd := range ln {
			lw += wd.w
		}
		x := maxInt(0, (w-lw)/2)
		for _, wd := range ln {
			wt := theme.Regular
			if wd.bold {
				wt = theme.Bold
			}
			s := wd.s
			at(gtx, image.Pt(x, i*lineH), func(gtx layout.Context) layout.Dimensions {
				return vcenter(gtx, lineH, func(gtx layout.Context) layout.Dimensions {
					return text(gtx, th, 15, wt, cl.Fg, s)
				})
			})
			x += wd.w
		}
	}
	return layout.Dimensions{Size: image.Pt(w, len(lines)*lineH)}
}
