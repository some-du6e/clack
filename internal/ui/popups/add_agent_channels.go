package popups

import (
	"context"
	"image"
	"strings"

	"gioui.org/io/event"
	"gioui.org/layout"
	"gioui.org/widget"

	"github.com/some-du6e/clack/internal/core"
	"github.com/some-du6e/clack/internal/i18n"
	"github.com/some-du6e/clack/internal/ui/theme"
)

// AddAgentToChannelsModal (boot-shared gWR0, build c07ab895506f) is what
// App details' Add to channel opens under flexpane_content_parity: the
// main button "Add to Channel" and AppDetailsOverflow's "Add to channel"
// (openAddAgentToChannelsDialog). Off, the overflow's item opens the
// single-channel dialog (pfOpenInviteToChannel) and there is no main
// button for it.
//
//   - A Slack Kit modal, centered, maxWidth 520: the title "Add {name} to
//     channels" (name: prefCompliantMemberName, vUCu sE), the body
//     (.body__ukQzq: a column, gap 16, padding 8px 0) and the footer's
//     Cancel (outline) and Add (primary; disabled with nothing picked,
//     loading while it runs).
//   - The body: the error Alert after a failed Add, then the multi-select
//     (o17d MultiSelect, placeholder "Select channels", aria "Channels")
//     over the channels the app can be added to (bD9l dX:
//     core.AppInvitableChannels, by name), with the current channel
//     picked when App details offered it (addToChannelPrefillId: the
//     channel the app isn't in yet). Ten at most: a pick past that is
//     refused (shouldOptionBeSelected) and the hint "You can add {name} to
//     up to 10 channels at a time." (c-hint: 13px content-ter, margin-top
//     4) shows under the select.
//   - Add: conversations.invite for each channel (core.AddMemberToChannels,
//     reason add-agent-to-channels-modal). Any channel added, or already a
//     member, toasts with check-circle ("Added {name} to # channel(s)." or
//     "{name} is already in that channel / those channels."). With no
//     failure the modal closes; else it stays with the failure's message
//     (buildFailureMessage, one wording for one shared reason) and only
//     the failed channels picked.
//
// Not drawn: Slack's option rows are ConversationListEntity (SMALL); the
// rows here are ugSelect's channel rows (the user group picker's), and
// the Add button's loading state is its spinner over the hidden label.
type addAgentChannels struct {
	h      Host
	modal  *Modal
	member string
	name   string
	opts   []ugOption
	sel    *ugSelect
	add    *ModalButton

	busy bool
	err  string
}

// aacMax is AddAgentToChannelsModal's cap: ten channels at a time.
const aacMax = 10

// nsAppDetails is gWR0's namespace (new Logger("app_details")).
var nsAppDetails = i18n.NS("app_details")

// CurrentView is the primary view's conversation and, for an app DM, its
// open tab ("home", "messages" or "about"; "" for any other
// conversation): useAppPrimaryActionFacts' getPrimaryView. The ui root
// sets it; nil reads as no conversation.
var CurrentView func() (channel, tab string)

// OpenAddAgentToChannels is openAddAgentToChannelsDialog for the bot user
// memberID; prefill is the channel picked on open ("" for none).
func OpenAddAgentToChannels(h Host, memberID, prefill string) *addAgentChannels {
	c := h.Core()
	if c == nil || memberID == "" {
		return nil
	}
	u, ok := lookupUser(h, memberID)
	if !ok {
		u = core.User{ID: memberID, IsBot: true}
	}
	d := &addAgentChannels{h: h, member: memberID, name: pfMemberName(u)}
	for _, cv := range c.AppInvitableChannels(memberID) {
		d.opts = append(d.opts, ugOption{ID: cv.ID, Label: cv.Name, Conv: cv})
	}
	d.sel = newUGSelect(h, false, nsAppDetails.T("Select channels"))
	d.sel.allowEmpty = true
	d.sel.canPick = func() bool { return len(d.sel.picked) < aacMax }
	opts := d.opts
	d.sel.search = func(_ context.Context, q string) ([]ugOption, error) {
		q = strings.ToLower(strings.TrimPrefix(strings.TrimSpace(q), "#"))
		var out []ugOption
		for _, o := range opts {
			if q == "" || strings.Contains(strings.ToLower(o.Label), q) {
				out = append(out, o)
			}
		}
		return out, nil
	}
	if prefill != "" {
		for _, o := range d.opts {
			if o.ID == prefill {
				d.sel.SetPicked([]ugOption{o})
			}
		}
	}
	d.add = &ModalButton{Label: nsAppDetails.T("Add"), Kind: BtnPrimary,
		Enabled: func() bool { return len(d.sel.picked) > 0 && !d.busy },
		Draw:    d.drawAdd,
		Do:      func() bool { d.submit(); return false }}
	d.modal = NewModal(h, ModalOpts{
		Title: nsAppDetails.T("Add {name} to channels", "name", d.name),
		Body:  d.layout,
		Buttons: []*ModalButton{{Label: nsAppDetails.T("Cancel"), Kind: BtnOutline,
			Do: func() bool { return true }}, d.add},
		FocusStops: func() []event.Tag { return []event.Tag{&d.sel.ed} },
		OnEscape:   ugEscape(d.sel), // MultiSelectInput onEscape: the list first
	})
	h.ShowOverlay(d.modal)
	return d
}

// Picked are the picked channels' ids (tests).
func (d *addAgentChannels) Picked() []string { return d.sel.IDs() }

// Options are the channels on offer (tests).
func (d *addAgentChannels) Options() []ugOption { return d.opts }

// Error is the failure Alert's message ("" for none).
func (d *addAgentChannels) Error() string { return d.err }

// Modal is the dialog.
func (d *addAgentChannels) Modal() *Modal { return d.modal }

// Pick picks the channel with id the way a click on its row does.
func (d *addAgentChannels) Pick(id string) bool {
	for _, o := range d.opts {
		if o.ID == id {
			n := len(d.sel.picked)
			d.sel.Pick(o)
			return len(d.sel.picked) > n
		}
	}
	return false
}

// atCap is shouldOptionBeSelected's limit: the hint shows.
func (d *addAgentChannels) atCap() bool { return len(d.sel.picked) >= aacMax }

// capHint is the hint at ten channels.
func (d *addAgentChannels) capHint() string {
	return nsAppDetails.T("You can add {name} to up to {max} channels at a time.", "name", d.name, "max", aacMax)
}

// submit is et: invite to every picked channel, then the toast, the close
// or the failure.
func (d *addAgentChannels) submit() {
	c := d.h.Core()
	if c == nil || d.busy || len(d.sel.picked) == 0 {
		return
	}
	d.busy, d.err = true, ""
	member, chans := d.member, d.sel.IDs()
	run(d.h, func(ctx context.Context) (core.AddToChannelsResult, error) {
		return c.AddMemberToChannels(ctx, member, chans), nil
	}, func(r core.AddToChannelsResult, _ error) {
		d.busy = false
		if len(r.NewlyAdded) > 0 || len(r.AlreadyMember) > 0 {
			toastIcon(d.h, aacResultLabel(d.name, r), "check-circle")
		}
		if len(r.Failed) == 0 {
			d.modal.Close()
			return
		}
		var failed []ugOption
		var names []string
		for _, f := range r.Failed {
			o := ugOption{ID: f.Channel, Label: f.Channel, Conv: core.Conv{ID: f.Channel, Name: f.Channel, Kind: "channel"}}
			for _, p := range d.opts {
				if p.ID == f.Channel {
					o = p
				}
			}
			failed = append(failed, o)
			names = append(names, "#"+o.Label)
		}
		d.err = aacFailureMessage(d.name, r.Failed, names)
		d.sel.SetPicked(failed) // O(r), W(r) and the select's remount
		d.h.Invalidate()
	})
}

// aacResultLabel is buildResultLabel.
func aacResultLabel(name string, r core.AddToChannelsResult) string {
	if n := len(r.NewlyAdded); n > 0 {
		return nsAppDetails.T("{count, plural, one {Added {name} to # channel.} other {Added {name} to # channels.}}", "name", name, "count", n)
	}
	return nsAppDetails.T("{name} is already in {count, plural, one {that channel} other {those channels}}.", "name", name, "count", len(r.AlreadyMember))
}

// aacFailureMessage is buildFailureMessage: the shared reason's wording
// ("other" when the channels failed for different reasons) over the
// listified channel names.
func aacFailureMessage(name string, failed []core.AddToChannelFailure, names []string) string {
	reason := core.AddToChannelOther
	if len(failed) > 0 {
		reason = failed[0].Reason
		for _, f := range failed[1:] {
			if f.Reason != reason {
				reason = core.AddToChannelOther
				break
			}
		}
	}
	chans := i18n.Listify(names, false)
	n := len(failed)
	switch reason {
	case core.AddToChannelNotInWorkspace:
		return nsAppDetails.T("Can\u2019t add {name} to {channels}. A Slack admin hasn\u2019t added {name} to the workspace for {count, plural, one {that channel} other {those channels}} yet.", "name", name, "channels", chans, "count", n)
	case core.AddToChannelNotAllowed:
		return nsAppDetails.T("Can\u2019t add {name} to {channels}. {name} isn\u2019t allowed in {count, plural, one {that channel} other {those channels}}.", "name", name, "channels", chans, "count", n)
	case core.AddToChannelNoPermission:
		return nsAppDetails.T("Can\u2019t add {name} to {channels}. Channel settings don\u2019t allow adding members.", "name", name, "channels", chans)
	}
	return nsAppDetails.T("Can\u2019t add {name} to {channels}. Try again, or contact a Slack admin.", "name", name, "channels", chans)
}

// layout is the body: the Alert, the select and its hint.
func (d *addAgentChannels) layout(gtx layout.Context) layout.Dimensions {
	th := d.h.Theme()
	cl := colorsOf(th)
	w := gtx.Constraints.Max.X
	y := dp(gtx, 8) // padding 8px 0
	if d.err != "" {
		ad := at(gtx, image.Pt(0, y), func(gtx layout.Context) layout.Dimensions {
			return ugAlert(gtx, th, cl, "error", d.err, w)
		})
		y += ad.Size.Y + dp(gtx, 16) // gap 16
	}
	sd := at(gtx, image.Pt(0, y), func(gtx layout.Context) layout.Dimensions {
		gtx.Constraints = layout.Constraints{Max: image.Pt(w, 1<<20)}
		return d.sel.layout(gtx, th, cl)
	})
	y += sd.Size.Y
	if d.atCap() {
		y += dp(gtx, 4) // .c-hint margin-top
		hd := at(gtx, image.Pt(0, y), func(gtx layout.Context) layout.Dimensions {
			gtx.Constraints = layout.Constraints{Max: image.Pt(w, 1<<20)}
			return para(gtx, th, 13, 18, theme.Regular, cl.Fg3, d.capHint(), 0)
		})
		y += hd.Size.Y
	}
	y += dp(gtx, 8)
	return layout.Dimensions{Size: image.Pt(w, y)}
}

// drawAdd is the Add button; loading, its label hides under the spinner
// at the same width (Slack's loading Button).
func (d *addAgentChannels) drawAdd(gtx layout.Context, th *theme.Theme, c *widget.Clickable, kind BtnKind, ring bool, grp grpSide) layout.Dimensions {
	if !d.busy {
		return buttonGrp(gtx, th, c, kind, BtnMedium, d.add.Label, "", 80, ring, grp)
	}
	_, full := record(gtx, func(gtx layout.Context) layout.Dimensions {
		return buttonGrp(gtx, th, c, BtnPrimary, BtnMedium, d.add.Label, "", 80, false, grp)
	})
	call, bd := record(gtx, func(gtx layout.Context) layout.Dimensions {
		return buttonGrp(gtx, th, c, BtnPrimary, BtnMedium, " ", "", float32(full.Size.X)/gtx.Metric.PxPerDp, false, grp)
	})
	call.Add(gtx.Ops)
	s := dp(gtx, 16)
	at(gtx, image.Pt((bd.Size.X-s)/2, (bd.Size.Y-s)/2), func(gtx layout.Context) layout.Dimensions {
		return abSpinner(gtx, s, colorsOf(th).PrimaryText)
	})
	return bd
}
