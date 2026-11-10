package popups

import (
	"context"

	"github.com/some-du6e/clack/internal/core"
)

// VIPs on the profile surfaces: MemberProfileButtons' renderVipButton
// (boot-shared Bp41: label "VIP", tooltip "Add to VIP" / "Remove from
// VIP", icon user-add, or user-hide on a VIP with the
// --vip_selected background) and the app card's toggle_vip item
// (boot-render f0+8 createAppProfileCardMenu). Both dispatch
// togglePriorityUserById (boot-shared 77my), which Preferences > VIP
// shares through the same users.priority.add/remove (core.AddVIP,
// core.RemoveVIP): the VIP list changes at once, then the call, then
// "Added <name> to VIPs" / "Removed <name> from VIPs" (3 s). A failed add
// puts the list back and shows PriorityErrorToast when the error is an
// API answer (core.VIPError).

// pfVipPending is the optimistic VIP state of members whose write is in
// flight (UI goroutine only).
var pfVipPending = map[string]bool{}

// pfIsVIP is isUserVIP: the member is on users.prefs vip_users, or an
// add/remove of it is in flight.
func pfIsVIP(h Host, id string) bool {
	if v, ok := pfVipPending[id]; ok {
		return v
	}
	c := h.Core()
	if c == nil || id == "" {
		return false
	}
	for _, v := range c.VIPs() {
		if v == id {
			return true
		}
	}
	return false
}

// pfCanToggleVIP is canMemberToggleVip (client-boot f/hH): VIPs are a paid
// team's feature (core treats the workspace as paid, like its VIP
// unreads section), and never for yourself, a member you hid (Clack hides
// none), a deactivated member or an Agentforce agent (Clack has none).
func pfCanToggleVIP(h Host, u core.User) bool {
	return h.Core() != nil && u.ID != "" && !pfIsSelf(h, u.ID) && !u.Deleted
}

// pfToggleVIP is togglePriorityUserById for u.
func pfToggleVIP(h Host, u core.User) {
	c := h.Core()
	if c == nil || u.ID == "" {
		return
	}
	id := u.ID
	name := pfMentionName(u) // prefCompliantMemberName: display name, else real name
	if name == "" {
		name = pfName(u)
	}
	was := pfIsVIP(h, id)
	pfVipPending[id] = !was
	h.Invalidate()
	run(h, func(ctx context.Context) (struct{}, error) {
		if was {
			return struct{}{}, c.RemoveVIP(ctx, id)
		}
		return struct{}{}, c.AddVIP(ctx, id)
	}, func(_ struct{}, err error) {
		delete(pfVipPending, id)
		h.Invalidate()
		switch {
		case err == nil && was:
			h.Toast("Removed " + name + " from VIPs")
		case err == nil:
			h.Toast("Added " + name + " to VIPs")
		case !was:
			if s := core.VIPError(err, name); s != "" {
				h.Toast(s)
			}
		}
	})
}

// pfVipSpec is the VIP button of a member's button row.
func pfVipSpec(h Host, u core.User, spec pfBtnSpec) pfBtnSpec {
	spec.label, spec.vip, spec.lead = "VIP", true, "user-add"
	if pfIsVIP(h, u.ID) {
		spec.lead, spec.selected = "user-hide", true
	}
	return spec
}
