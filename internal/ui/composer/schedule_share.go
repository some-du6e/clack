package composer

import (
	"image"
	"time"

	"github.com/some-du6e/clack/internal/ui/popups"
	"github.com/some-du6e/clack/internal/ui/uipref"
)

// The share dialog's schedule caret uses the composer's ScheduleSendMenu
// and ScheduleMessageDialog (Slack's BYKP and z1vG, both shared by every
// surface): the ui root hands these to popups (popups.ScheduleSendMenu,
// popups.OpenScheduleDialog). The share dialog opens the menu with
// draftId "" and the destination as channelId, so:
//
//   - getScheduledSendMenuOptions has no draft member, hence no
//     member-specific row; the previously scheduled time comes back under
//     updated_scheduled_sends_desktop as in the composer;
//   - a row's description is getScheduledChannelTimeZoneDetails (Mluw uA)
//     under updated_scheduled_sends_desktop ("{memberTime} for
//     {memberName}" for a DM's other member, or a group DM's only other
//     one, in another time zone), else the draft's details, which a
//     missing draft doesn't have;
//   - Custom time saves the forward as a draft and opens the dialog for
//     it, whose time zone line and default time follow the draft's
//     member (getMemberFromDraftForTimeZoneDetails: the DM's).

// ShareScheduleMenu is ScheduleSendMenu for the share dialog, above anchor
// and right-aligned to it (the caret's "top-right"). pick gets a preset's
// time when chosen; custom is Custom time.
func ShareScheduleMenu(h Host, anchor image.Rectangle, ch string, pick func(time.Time), custom func()) {
	a := h.Core()
	now := time.Now()
	h24 := uipref.Get().Time24
	exp := a != nil && a.Experiment("updated_scheduled_sends_desktop") == "on"
	var desc *schedMemberInfo
	if exp {
		desc = shareChannelMember(h, ch, true)
	}
	items := []popups.MenuItem{{Key: popups.LabelKey("Schedule message"), Header: draftsNS.T("Schedule message")}}
	for _, r := range scheduleRows(now, h24, exp, nil) {
		r := r
		if r.opt == schedCustom {
			items = append(items, popups.Sep(), popups.MenuItem{Key: r.key, Label: r.label, Do: custom})
			continue
		}
		sub := ""
		if desc != nil && !desc.sameTZ {
			sub = draftsNS.T("{memberTime} for {memberName}", "memberTime", schedClock(r.at.In(desc.loc), h24), "memberName", desc.name)
		}
		items = append(items, popups.MenuItem{Key: r.key, Label: r.label, Subtitle: sub, Do: func() {
			at := r.at
			if r.opt < schedMember { // the time for the option when it is picked
				at = schedStandardTime(r.opt, time.Now())
			}
			pick(at)
		}})
	}
	h.ShowOverlay(popups.NewMenuOpts(h, items, popups.MenuOpts{Width: 260, Anchor: anchor, Side: popups.Above, AlignRight: true}))
}

// ShareScheduleDialog is ScheduleMessageDialog for a forward's draft to
// ch: it opens at getDefaultCustomTime for the draft's member, says when
// that is for them, and pick gets the time chosen (remembered as the last
// custom time, as the composer's is).
func ShareScheduleDialog(h Host, ch string, pick func(time.Time)) {
	m := shareChannelMember(h, ch, false)
	openSchedulePicker(h, schedCustomDefault(time.Now(), m), m, func(t time.Time) {
		lastCustomSchedule = t
		pick(t)
	})
}

// shareChannelMember is the member a conversation's time zone details
// name: a DM's other member, or with group DMs (getScheduledChannel-
// TimeZoneDetails) one with exactly one other member. nil when there is
// none or Clack doesn't know their time zone.
func shareChannelMember(h Host, ch string, groups bool) *schedMemberInfo {
	a := h.Core()
	if a == nil {
		return nil
	}
	c, ok := a.Conv(ch)
	if !ok {
		return nil
	}
	self := a.Self()
	uid := ""
	switch {
	case c.Kind == "im":
		uid = c.User
	case c.Kind == "mpim" && groups:
		var others []string
		for _, id := range c.Members {
			if id != self.ID {
				others = append(others, id)
			}
		}
		if len(others) == 1 {
			uid = others[0]
		}
	}
	if uid == "" {
		return nil
	}
	u, ok := a.User(uid)
	if !ok || u.TZ == "" && u.TZOffset == 0 {
		return nil
	}
	return &schedMemberInfo{name: u.Name, loc: schedMemberLoc(u), sameTZ: u.TZ == "" || u.TZ == self.TZ, offset: u.TZOffset != 0}
}
