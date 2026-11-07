package views

// Slack component boundaries Slick plugins patch in the views
// (docs/PLUGINS-components.md §9: BetterBlock, CallTimer, LetMeSee,
// BetterHuddles). Same rules as slackc.go: one atomic load when nothing
// is patched, props built only under a patch, and natives that honour the
// props a plugin hands to <Original>. A plugin that renders null in place
// of a boundary hides that part of the view (BetterBlock's blocked people).

import (
	"gioui.org/layout"

	"github.com/some-du6e/yoloslackgoclient/internal/core"
	"github.com/some-du6e/yoloslackgoclient/internal/ui/slackc"
)

// ---- Activity: the parts of an ActivityItem ----
//
//	ActivityItem
//	└ ActivityThread (thread items) | ActivityDM (DM items) | —
//	  └ ActivityRowContentMultiLine (Detailed) | ActivityRowContentSingleLine (Dense)
//	    └ ActivityRowContentDetailed | ActivityRowContentDense
//	      └ ActivityRowContentSenderAvatarLayout, ActivityPrivacyBlurredName, …

// msg is the message the row previews: an ActivityThread override, or the
// item's own.
func (r *actRow) msg() *core.Message {
	if r.pm != nil {
		return r.pm
	}
	return r.it.Msg
}

// isThread reports a thread item (Slack's thread / thread_v2 rows).
func (r *actRow) isThread() bool {
	return r.it.ThreadTS != "" && (r.it.Type == "thread_v2" || r.it.Type == "thread")
}

func (r *actRow) rowKind(gtx C) D {
	switch {
	case r.isThread():
		return slackc.Component(gtx, "ActivityThread", r, (*actRow).threadProps, (*actRow).layoutThread)
	case r.d.dm:
		return slackc.Component(gtx, "ActivityDM", r, (*actRow).dmProps, (*actRow).layoutDM)
	}
	return r.lines(gtx)
}

// rowBase is the props every inner activity boundary gets.
func (r *actRow) rowBase(part string) slackc.Props {
	return slackc.Props{"$key": r.key + "/" + part, "activityItem": r.itemShape(), "msg": msgProps(r.msg()),
		"density": r.density(), "isDmTab": r.v.dmOnly()}
}

// threadProps: ActivityThread {activityItem, msg, messageTs, minUnreadTs,
// unreadCount}.
func (r *actRow) threadProps() slackc.Props {
	p := r.rowBase("thread")
	p["messageTs"] = r.it.TS
	p["threadTs"] = r.it.ThreadTS
	p["unreadCount"] = r.it.Count
	return p
}

// layoutThread honours BetterBlock's projection of a thread item: msg
// replaced by the latest visible message of the thread (its sender and
// preview), and unreadCount without the hidden replies.
func (r *actRow) layoutThread(gtx C, p slackc.Props) D {
	if p != nil {
		r.threadOverride(p)
	}
	return r.lines(gtx)
}

func (r *actRow) threadOverride(p slackc.Props) {
	if n, ok := p["unreadCount"].(float64); ok {
		r.count = max(0, int(n))
	} else if ai := p.Map("activityItem"); ai != nil {
		if n, ok := ai["unreadCount"].(float64); ok {
			r.count = max(0, int(n))
		}
	}
	mp := p.Map("msg")
	it := r.it
	if mp == nil || it.Msg == nil || r.d.invite || it.Reaction != nil {
		return
	}
	src := it.Msg
	if ts := mp.StrOr("ts", src.TS); ts != src.TS {
		if m := r.threadMsg(ts); m != nil {
			src, r.pm = m, m
		}
	}
	om := msgOverride(src, mp)
	r.pd = *r.cur
	if n, av := r.v.authorOf(&om); n != "" {
		r.pd.name, r.pd.avatar = n, av
	}
	r.cur = &r.pd
}

// dmProps: ActivityDM {channelId, activityItem, msg, isDmTab}.
func (r *actRow) dmProps() slackc.Props {
	p := r.rowBase("dm")
	p["channelId"] = r.it.Channel
	return p
}

func (r *actRow) layoutDM(gtx C, _ slackc.Props) D { return r.lines(gtx) }

// lines is the row layout for the density: Slack's MultiLine row holds
// the Detailed content, its SingleLine row the Dense one.
func (r *actRow) lines(gtx C) D {
	if r.v.dense {
		return slackc.Component(gtx, "ActivityRowContentSingleLine", r, (*actRow).singleProps, (*actRow).layoutSingle)
	}
	return slackc.Component(gtx, "ActivityRowContentMultiLine", r, (*actRow).multiProps, (*actRow).layoutMulti)
}

// multiProps: ActivityRowContentMultiLine {activityItem, itemId, index,
// hasFocus, isFirstEntry, precedesHeader}.
func (r *actRow) multiProps() slackc.Props {
	p := r.rowBase("multi")
	p["itemId"], p["hasFocus"], p["isFirstEntry"], p["precedesHeader"] = r.key, false, false, false
	return p
}

func (r *actRow) singleProps() slackc.Props {
	p := r.multiProps()
	p["$key"] = r.key + "/single"
	return p
}

func (r *actRow) layoutMulti(gtx C, _ slackc.Props) D {
	return slackc.Component(gtx, "ActivityRowContentDetailed", r, (*actRow).detailedProps, (*actRow).layoutDetailed)
}

func (r *actRow) layoutSingle(gtx C, _ slackc.Props) D {
	return slackc.Component(gtx, "ActivityRowContentDense", r, (*actRow).denseProps, (*actRow).layoutDense)
}

// detailedProps / denseProps: ActivityRowContentDetailed|Dense
// {activityItem, density, msg}.
func (r *actRow) detailedProps() slackc.Props { return r.rowBase("detailed") }
func (r *actRow) denseProps() slackc.Props    { return r.rowBase("dense") }

func (r *actRow) layoutDetailed(gtx C, _ slackc.Props) D { return r.v.detailedContent(gtx, r) }
func (r *actRow) layoutDense(gtx C, _ slackc.Props) D    { return r.v.denseContent(gtx, r) }

// threadMsg is the thread message ts an ActivityThread override points
// at: the root, or a cached reply (converted once, kept on the row).
func (r *actRow) threadMsg(ts string) *core.Message {
	it := r.it
	if it.Root != nil && it.Root.TS == ts {
		return it.Root
	}
	if r.tm != nil && r.tm.TS == ts && r.tm.Channel == it.Channel {
		return r.tm
	}
	a := r.v.h.Core()
	sm, ok := a.S.Find(it.Channel, ts)
	if !ok {
		return nil
	}
	m := a.MessageFromSlack(it.Channel, &sm)
	r.tm = &m
	return r.tm
}

// ---- DMs page: DmsList › DmsListItem › DmsChannel ----

// dmChannel is a DM's conversation in Slack's channel shape (what
// BetterBlock reads when the channel isn't in the store).
func dmChannel(dm *core.DM) slackc.Props {
	im := dm.Conv.Kind == "im" || dm.Conv.Kind == "" && dm.Conv.User != ""
	c := slackc.Props{"id": dm.Channel, "is_im": im, "is_mpim": dm.Conv.Kind == "mpim"}
	if dm.Conv.User != "" {
		c["user"] = dm.Conv.User
	}
	if dm.Conv.Name != "" {
		c["name"] = dm.Conv.Name
	}
	return c
}

// listProps: DmsList {channelIds, dms: [{channel, latest}]}, the DMs in
// list order.
func (d *DMs) listProps() slackc.Props {
	ids := make([]any, 0, len(d.view))
	dms := make([]any, 0, len(d.view))
	for _, e := range d.view {
		if e.dm < 0 {
			continue
		}
		dm := &d.items[e.dm]
		ids = append(ids, dm.Channel)
		r := slackc.Props{"channel": dmChannel(dm), "unread": dmsUnread(dm)}
		if dm.Latest != nil {
			r["latest"] = dm.Latest.TS
			r["message"] = msgProps(dm.Latest)
		}
		dms = append(dms, r)
	}
	return slackc.Props{"$key": "dms-list", "channelIds": ids, "dms": dms, "selectedChannelId": d.sel}
}

// listOverride is the view with only the DMs the plugin's channelIds and
// dms still list (headings and directory people stay).
func (d *DMs) listOverride(p slackc.Props) []dmsEntry {
	var ids, dms map[string]bool
	if p.Has("channelIds") {
		ids = map[string]bool{}
		for _, x := range p.List("channelIds") {
			if s, ok := x.(string); ok {
				ids[s] = true
			}
		}
	}
	if p.Has("dms") {
		dms = map[string]bool{}
		for _, x := range p.List("dms") {
			var r slackc.Props
			switch x := x.(type) {
			case slackc.Props:
				r = x
			case map[string]any:
				r = slackc.Props(x)
			}
			if c := r.Map("channel"); c != nil {
				dms[c.StrOr("id", "")] = true
			}
		}
	}
	d.pview = d.pview[:0]
	for _, e := range d.view {
		if e.dm >= 0 {
			ch := d.items[e.dm].Channel
			if ids != nil && !ids[ch] || dms != nil && !dms[ch] {
				continue
			}
		}
		d.pview = append(d.pview, e)
	}
	return d.pview
}

// visible is the list as last drawn.
func (d *DMs) visible() []dmsEntry {
	if d.pviewOn {
		return d.pview
	}
	return d.view
}

// itemProps: DmsListItem {channelId, result: {channel}, isSelected}.
func (r *dmsRow) itemProps() slackc.Props {
	dm := r.dm
	return slackc.Props{"$key": dm.Channel, "channelId": dm.Channel, "result": slackc.Props{"channel": dmChannel(dm)},
		"isSelected": dm.Channel == r.v.sel, "isUnread": dmsUnread(dm)}
}

func (r *dmsRow) layoutItem(gtx C, _ slackc.Props) D { return r.v.dmRow(gtx, r.dm) }

// channelProps: DmsChannel {channelId, channel}, the row's content.
func (r *dmsRow) channelProps() slackc.Props {
	dm := r.dm
	return slackc.Props{"$key": dm.Channel + "/channel", "channelId": dm.Channel, "channel": dmChannel(dm)}
}

func (r *dmsRow) layoutChannel(gtx C, _ slackc.Props) D { return r.content(gtx) }

// ---- Directories: PeopleGrid ----

// peopleProps: PeopleGrid {users: Slack members, query, total}.
func (d *Directories) peopleProps() slackc.Props {
	st, _, _ := d.people.get()
	users := make([]any, len(st.list))
	for i := range st.list {
		users[i] = memberProps(&st.list[i])
	}
	return slackc.Props{"$key": "people-grid", "users": users, "query": st.q, "total": st.total}
}

// memberProps is a core.User in Slack's member shape (the fields Clack
// has).
func memberProps(u *core.User) slackc.Props {
	prof := slackc.Props{"display_name": u.Name, "real_name": u.RealName}
	set := func(k, v string) {
		if v != "" {
			prof[k] = v
		}
	}
	set("title", u.Title)
	set("pronouns", u.Pronouns)
	set("image_72", u.Avatar)
	set("image_192", u.Avatar192)
	set("image_512", u.Avatar512)
	set("status_text", u.StatusText)
	set("status_emoji", u.StatusEmoji)
	m := slackc.Props{"id": u.ID, "name": u.Handle, "real_name": u.RealName, "profile": prof,
		"is_bot": u.IsBot, "deleted": u.Deleted}
	if u.Presence != "" {
		m["presence"] = u.Presence
	}
	return m
}

// peopleOverride keeps the people whose ids the plugin's users list
// still has, in Clack's order.
func (d *Directories) peopleOverride(p slackc.Props, us []core.User) []core.User {
	if !p.Has("users") {
		return us
	}
	keep := map[string]bool{}
	for _, x := range p.List("users") {
		switch x := x.(type) {
		case slackc.Props:
			keep[x.StrOr("id", "")] = true
		case map[string]any:
			keep[slackc.Props(x).StrOr("id", "")] = true
		}
	}
	d.pusers = d.pusers[:0]
	for _, u := range us {
		if keep[u.ID] {
			d.pusers = append(d.pusers, u)
		}
	}
	return d.pusers
}

// ---- Huddles ----
//
// The live call's components (HuddleMvaWindowTitle, HuddleBottomBarContent,
// HuddleRecordingIndicator, BaseDrawRenderer, HuddleMiniPanel) are patch
// points in huddleui's call window and sidebar bar. A live huddle's row on
// the Huddles page keeps the join point:
//
//	HuddleInviteWindowWithPreviewContent the Join button (onHuddleJoin)

func roomChannel(r huddleRoom) string {
	if len(r.Channels) > 0 {
		return r.Channels[0]
	}
	return ""
}

// huddleBase is the props every huddle point gets.
func huddleBase(r huddleRoom, part string) slackc.Props {
	return slackc.Props{"$key": r.ID + "/" + part, "huddleId": r.ID, "channelId": roomChannel(r), "isActive": r.Active}
}

// huddleTitle is the live row's title. Slack's huddle window components
// (HuddleMvaWindowTitle, HuddleBottomBarContent, HuddleRecordingIndicator,
// BaseDrawRenderer, HuddleMiniPanel) are patch points in huddleui's real
// call window and sidebar bar; this list row only joins.
func (v *Huddles) huddleTitle(gtx C, r huddleRoom) D {
	return v.label(gtx, 15, wBold, v.p.Text, v.roomTitle(r))
}

// huddleBar is the row's right side: faces, then the Join point.
func (v *Huddles) huddleBar(gtx C, r huddleRoom) D {
	return row(gtx, layout.Middle,
		layout.Rigid(func(gtx C) D { return v.faces(gtx, r) }),
		layout.Rigid(func(gtx C) D { return v.joinPoint(gtx, r) }),
	)
}

// joinPoint: HuddleInviteWindowWithPreviewContent {channelId, huddleId,
// onHuddleJoin}. The row's click joins through the onHuddleJoin a plugin
// left (BetterHuddles notes the preview's devices, then calls Clack's).
func (v *Huddles) joinPoint(gtx C, r huddleRoom) D {
	return slackc.RenderLazy(gtx, "HuddleInviteWindowWithPreviewContent", func() slackc.Props {
		p := huddleBase(r, "join")
		id := r.ID
		p["onHuddleJoin"] = func() { v.join(id) }
		return p
	}, func(gtx C, p slackc.Props) D {
		if f, ok := p.Func("onHuddleJoin"); ok {
			if jf, ok := f.(slackc.JSFunc); ok {
				if v.joinFn == nil {
					v.joinFn = map[string]slackc.JSFunc{}
				}
				v.joinFn[r.ID] = jf
			} else {
				delete(v.joinFn, r.ID)
			}
		} else {
			delete(v.joinFn, r.ID)
		}
		// drawn, not a separate clickable: the whole row joins
		return layout.Inset{Left: 12}.Layout(gtx, func(gtx C) D { return v.joinPill(gtx) })
	})
}

// joinClicked is a click on a huddle row: through the plugin's
// onHuddleJoin when the Join point is patched, else straight to join.
func (v *Huddles) joinClicked(id string) {
	if f, ok := v.joinFn[id]; ok && f.ID != 0 {
		f.Call()
		return
	}
	v.join(id)
}

// join starts Slack's startHuddle for the room (from its join preview
// when the huddle is live).
func (v *Huddles) join(id string) {
	r, ok := v.roomByID(id)
	if !ok {
		huddleAttempt(v.h, v.tt, startHuddleArgs{HuddleID: id})
		return
	}
	startHuddle(v.h, v.tt, startHuddleArgs{ChannelID: roomChannel(r), HuddleID: r.ID, FromJoinPreview: r.Active})
}

func (v *Huddles) roomByID(id string) (huddleRoom, bool) {
	v.mu.Lock()
	r, ok := v.live[id]
	v.mu.Unlock()
	if ok {
		return r, true
	}
	recent, _, _ := v.recent.get()
	for _, r := range recent {
		if r.ID == id {
			return r, true
		}
	}
	return huddleRoom{}, false
}
