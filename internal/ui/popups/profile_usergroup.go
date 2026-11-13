package popups

import (
	"context"
	"image"
	"strconv"
	"strings"
	"time"

	"gioui.org/layout"
	"gioui.org/op"
	"gioui.org/unit"
	"gioui.org/widget"

	"github.com/some-du6e/clack/internal/core"
	"github.com/some-du6e/clack/internal/ui/theme"
)

// UserGroupView is Slack's user group profile: a @group mention's
// UserGroupElement (boot-shared U14y) navigates to toUserGroup (client-boot
// jpad: {id: "user_groups", viewType: UserGroup, params: {userGroupId}}),
// the flexpane of client-channel-user-group-view. Its BaseUserGroupView
// picks one of two UserGroupDetails by the group's kind (9tRe d,
// core.Usergroup.OrgLevel: an E… team_id):
//
//   - the pane header "User Group";
//   - UserGroupHeader (.p-user_group_details__section, margin 12px 16px):
//     the name (18/900; "<name> (Deactivated)" for a disabled group), the
//     description (15px), the @handle (13/700, secondary, margin-top 4);
//   - the tabs (only for an active group; margin-top 12, tab menu
//     padding 0 16): "Members" and "Channels" with their counts
//     (user_count, which a workspace group's fetch resets to the number
//     of members; the group's channels).
//
// A workspace group (the classic class component): its members come
// from fetchAndUpsertUserGroupMembers on mount (usergroups.users.list
// {usergroup, team_id}; core.UsergroupUsers), sorted by
// getPrefCompliantMemberNameLowerCase, as rows that open the profile
// (the ⋮ menu is for editors only); no search box; empty: "Nobody’s in
// this group" / "Contact a workspace administrator to add members." (the
// link opens the people browser filtered to admins); no channels: "No
// default channels" / "All members of a user group are automatically
// added to its default channels.".
//
// An org-level group (AssignedGroupMembers): "Find members" (padding 16)
// once there is someone or a search, the members from
// searchAndUpsertUserGroupMembers (search.enterprise, 100 a page, 300 ms
// debounced search; core.UsergroupMembers) as 60px rows whose ⋮ menu has
// "View profile"; empty: "Nobody’s in this group" / "Contact an org
// administrator to add members." ("No results found" for a search); no
// channels: "No channels are connected to this group" / "Contact an org
// administrator to add channels.".
//
// Both lists start from the members last fetched (Slack keeps them on the
// userGroups record). A workspace group with none of those shows the
// empty state, as Slack's classic UserGroupDetails does while its
// usergroups.users.list is in flight and after it fails (no error state;
// the boot list has no users). An org group's list stays blank then.
//
// The group itself is the boot list's, refreshed by usergroups.search
// (keyword: the handle, count 1; core.UsergroupProfile), which also
// brings team_id when the boot list's lacked it.
type UserGroupView struct {
	h    Host
	id   string
	g    core.UsergroupProfile
	list layout.List

	asked, loaded bool
	// started: the members fetch began (it waits for team_id); org: the
	// group is OrgLevel (decided when it starts)
	started, org bool
	// failed: the last members fetch failed and there is nothing to show
	failed bool

	tab               int // 0 Members, 1 Channels
	tabBtns           [2]widget.Clickable
	closeBtn          widget.Clickable
	adminLink         widget.Clickable
	search            widget.Editor
	searchQ, lastQ    string
	searchAt          time.Time
	members           []core.User
	cursor            string
	membersDone       bool
	loading, fetching bool
	seq               int
	rows              map[string]*widget.Clickable
	paneW             int

	// the editor parts (usergroup_edit.go): the viewer's rights for the
	// group's team, and the store generation and record last seen
	rights       core.UsergroupRights
	rightsAsked  bool
	rightsOK     bool
	ugGen        uint64
	snap         ugSnap
	snapSet      bool
	editMembers  widget.Clickable
	editChannels widget.Clickable
	editDetails  widget.Clickable
	copyID       widget.Clickable
	options      widget.Clickable
	optionsTip   TipState
	enableBtn    widget.Clickable
	addMembers   widget.Clickable
	addChannels  widget.Clickable
	dashLink     widget.Clickable
	rowTips      map[string]*TipState
	// hits are the editor buttons' last rects in pane coordinates (tests
	// click them through the router)
	hits map[string]image.Rectangle
}

// ugSnap is what of a stored group makes the classic pane refetch its
// members when it changes (componentDidUpdate compares the userGroup
// record without its member fetch state): everything but the members and
// their count.
type ugSnap struct {
	g        core.Usergroup
	disabled bool
}

func ugSnapOf(p core.UsergroupProfile) ugSnap {
	g := p.Usergroup
	g.UserCount = 0
	return ugSnap{g: g, disabled: p.Disabled}
}

// NewUserGroupView builds the pane for group id.
func NewUserGroupView(h Host, id string) *UserGroupView {
	v := &UserGroupView{h: h, id: id, rows: map[string]*widget.Clickable{}}
	v.list.Axis = layout.Vertical
	v.search.SingleLine = true
	if c := h.Core(); c != nil {
		v.g.Usergroup = c.StoredUsergroup(id)
	}
	return v
}

func (v *UserGroupView) row(key string) *widget.Clickable {
	c := v.rows[key]
	if c == nil {
		c = new(widget.Clickable)
		v.rows[key] = c
	}
	return c
}

// fetch refreshes the group and loads its members once: right away when
// the boot list gave team_id, else once usergroups.search has.
func (v *UserGroupView) fetch() {
	c := v.h.Core()
	if c == nil || v.asked {
		return
	}
	v.asked = true
	id, h := v.id, v.h
	run(h, func(ctx context.Context) (core.UsergroupProfile, error) { return c.UsergroupProfile(ctx, id) },
		func(g core.UsergroupProfile, err error) {
			if err == nil {
				n := v.g.UserCount
				v.g = g
				if v.started && !v.org && !v.loading && !v.failed {
					v.g.UserCount = n // the members fetch already counted them
				}
			}
			v.loaded = true
			v.startMembers()
			h.Invalidate()
		})
	v.startMembers()
}

// startMembers picks the group's members path once team_id is known
// (BaseUserGroupView) and starts it from the members last fetched.
func (v *UserGroupView) startMembers() {
	c := v.h.Core()
	if c == nil || v.started {
		return
	}
	if v.g.TeamID == "" {
		if v.loaded { // fetchAndUpsertUserGroupMembers: no team_id, no fetch
			v.started, v.failed = true, true
		}
		return
	}
	v.started, v.org = true, v.g.OrgLevel()
	v.loadRights()
	v.ugGen = c.S.UsergroupGen()
	if p, ok := c.StoredUsergroupProfile(v.id); ok {
		v.snap, v.snapSet = ugSnapOf(p), true
	}
	if us, ok := c.CachedUsergroupUsers(v.id, !v.org); ok {
		v.members = v.shown(us)
	}
	if v.org {
		v.loadMembers("", "")
	} else {
		v.loadUsers()
	}
}

// loadRights reads the viewer's rights for the group's team (core.
// UsergroupRights: canEditUserGroupsInWorkspace and its kin).
func (v *UserGroupView) loadRights() {
	c := v.h.Core()
	if c == nil || v.rightsAsked {
		return
	}
	v.rightsAsked = true
	team, h := v.g.TeamID, v.h
	if r, ok := c.CachedUsergroupRights(team); ok {
		v.rights, v.rightsOK = r, true
		return
	}
	run(h, func(ctx context.Context) (core.UsergroupRights, error) { return c.UsergroupRights(ctx, team), nil },
		func(r core.UsergroupRights, _ error) {
			v.rights, v.rightsOK = r, true
			h.Invalidate()
		})
}

// watch follows the stored group (subteam_updated, an edit's answer):
// its members and count, its default channels, and, when the record
// itself changed, the classic pane's member refetch (componentDidUpdate's
// fetchUserGroupMembers).
func (v *UserGroupView) watch() {
	c := v.h.Core()
	if c == nil || !v.started {
		return
	}
	gen := c.S.UsergroupGen()
	if gen == v.ugGen {
		return
	}
	v.ugGen = gen
	p, ok := c.StoredUsergroupProfile(v.id)
	if !ok {
		return
	}
	if chans, known := c.S.GroupChannels(v.id); known {
		_ = chans
		v.g.Channels, v.g.Groups = p.Channels, p.Groups
	}
	if v.org {
		return
	}
	if us, ok := c.CachedUsergroupUsers(v.id, true); ok && !v.loading {
		v.members = us
		v.g.UserCount = p.UserCount
	}
	snap := ugSnapOf(p)
	if !v.snapSet || snap == v.snap {
		v.snap, v.snapSet = snap, true
		return
	}
	v.snap = snap
	n, chans, groups, extra := v.g.UserCount, v.g.Channels, v.g.Groups, v.g.AdditionalChannels
	v.g = p
	v.g.UserCount, v.g.Channels, v.g.Groups, v.g.AdditionalChannels = n, chans, groups, extra
	if !p.Disabled {
		v.loadUsers()
	}
}

// shown drops deactivated members from an org group's list (MembersList
// filters them); the classic list shows whom usergroups.users.list gave.
func (v *UserGroupView) shown(us []core.User) []core.User {
	if !v.org {
		return us
	}
	out := make([]core.User, 0, len(us))
	for _, u := range us {
		if !u.Deleted {
			out = append(out, u)
		}
	}
	return out
}

type ugPage struct {
	us   []core.User
	next string
	n    int
}

// loadUsers is the classic pane's componentDidMount fetch: one
// usergroups.users.list call (no limit, so no pages), whose count
// becomes the Members tab's (bulkAddMembersToGroup's user_count).
func (v *UserGroupView) loadUsers() {
	c := v.h.Core()
	if c == nil {
		return
	}
	v.seq++
	seq, g, h := v.seq, v.g.Usergroup, v.h
	v.loading, v.fetching = true, true
	run(h, func(ctx context.Context) (ugPage, error) {
		us, _, err := c.UsergroupUsers(ctx, g, "")
		n := len(us)
		if ids, ok := c.S.GroupUsers(g.ID); ok {
			n = len(ids)
		}
		return ugPage{us: us, n: n}, err
	}, func(r ugPage, err error) {
		if seq != v.seq {
			return
		}
		v.loading, v.fetching, v.membersDone = false, false, true
		if err != nil {
			v.failed = len(v.members) == 0
		} else {
			v.members, v.failed = r.us, false
			v.g.UserCount = r.n
		}
		h.Invalidate()
	})
}

// loadMembers fetches an org group's members page for term from cursor
// ("" first).
func (v *UserGroupView) loadMembers(term, cursor string) {
	c := v.h.Core()
	if c == nil {
		return
	}
	v.seq++
	seq, id, h := v.seq, v.id, v.h
	if cursor == "" {
		v.loading = true
	}
	v.fetching = true
	run(h, func(ctx context.Context) (ugPage, error) {
		us, next, _, err := c.UsergroupMembers(ctx, id, term, cursor)
		return ugPage{us: us, next: next}, err
	}, func(r ugPage, err error) {
		if seq != v.seq {
			return
		}
		v.loading, v.fetching = false, false
		if err != nil { // the list keeps what it had
			v.membersDone = true
			v.failed = len(v.members) == 0
			h.Invalidate()
			return
		}
		if cursor == "" {
			v.members = nil
		}
		v.members = append(v.members, v.shown(r.us)...)
		v.failed = false
		v.cursor, v.membersDone = r.next, r.next == ""
		h.Invalidate()
	})
}

// Layout fills the constraints with the pane.
func (v *UserGroupView) Layout(gtx layout.Context) layout.Dimensions {
	drain()
	th := v.h.Theme()
	cl := colorsOf(th)
	sz := gtx.Constraints.Max
	v.paneW = sz.X
	v.fetch()
	v.watch()
	v.editorClicks(gtx)
	if v.closeBtn.Clicked(gtx) {
		v.h.CloseSecondary()
	}
	for i := range v.tabBtns {
		if v.tabBtns[i].Clicked(gtx) {
			v.tab = i
		}
	}
	// "Find members": a 300 ms debounce (useDebounce), then a fresh search
	for {
		ev, ok := v.search.Update(gtx)
		if !ok {
			break
		}
		if _, ok := ev.(widget.ChangeEvent); ok {
			v.searchQ, v.searchAt = v.search.Text(), gtx.Now
		}
	}
	if v.searchQ != v.lastQ {
		if wait := v.searchAt.Add(300 * time.Millisecond).Sub(gtx.Now); wait > 0 {
			gtx.Execute(op.InvalidateCmd{At: gtx.Now.Add(wait)})
		} else {
			v.lastQ = v.searchQ
			v.loadMembers(v.lastQ, "")
		}
	}

	hh := dp(gtx, 49)
	pad := dp(gtx, 16)
	at(gtx, image.Pt(pad, 0), func(gtx layout.Context) layout.Dimensions {
		gtx.Constraints = layout.Constraints{Max: image.Pt(maxInt(1, sz.X-pad-dp(gtx, 52)), hh)}
		return vcenter(gtx, hh, func(gtx layout.Context) layout.Dimensions {
			return text(gtx, th, 18, theme.Black, cl.Fg, "User Group")
		})
	})
	at(gtx, image.Pt(sz.X-dp(gtx, 12)-dp(gtx, 28), (hh-dp(gtx, 28))/2), func(gtx layout.Context) layout.Dimensions {
		return iconButton(gtx, &v.closeBtn, "close", 28, 20, cl.Fg2, cl.BgHover, 8)
	})
	bodyH := maxInt(0, sz.Y-hh)
	at(gtx, image.Pt(0, hh), func(gtx layout.Context) layout.Dimensions {
		gtx.Constraints = layout.Exact(image.Pt(sz.X, bodyH))
		defer clipRRect(gtx, image.Rect(0, 0, sz.X, bodyH), 0).Pop()
		return v.list.Layout(gtx, 1, func(gtx layout.Context, _ int) layout.Dimensions {
			gtx.Constraints.Min = image.Point{}
			return v.body(gtx, th, cl, sz.X, bodyH)
		})
	})
	return layout.Dimensions{Size: sz}
}

func (v *UserGroupView) body(gtx layout.Context, th *theme.Theme, cl *Colors, w, viewH int) layout.Dimensions {
	pad := dp(gtx, 16)
	inner := maxInt(1, w-2*pad)
	y := dp(gtx, 12)
	name := v.g.Name
	if name == "" {
		name = v.g.Handle
	}
	if v.g.Disabled {
		name += " (Deactivated)"
	}
	put := func(x, y int, wd layout.Widget) int {
		d := at(gtx, image.Pt(x, y), func(gtx layout.Context) layout.Dimensions {
			gtx.Constraints = layout.Constraints{Max: image.Pt(inner, 1<<20)}
			return wd(gtx)
		})
		return d.Size.Y
	}
	y += put(pad, y, func(gtx layout.Context) layout.Dimensions {
		return para(gtx, th, 18, 24, theme.Black, cl.Fg, name, 0)
	})
	if !pfBlank(v.g.Description) {
		y += put(pad, y, func(gtx layout.Context) layout.Dimensions {
			return para(gtx, th, 15, 22, theme.Regular, cl.Fg, v.g.Description, 0)
		})
	}
	if v.g.Handle != "" {
		y += dp(gtx, 4)
		y += put(pad, y, func(gtx layout.Context) layout.Dimensions {
			return para(gtx, th, 13, 18, theme.Bold, cl.Fg2, "@"+v.g.Handle, 0)
		})
	}
	if v.org && v.rights.OrgDash { // ManageUserGroupLink (the Groups dashboard)
		y += dp(gtx, 12)
		y += v.dashAlert(gtx, th, cl, pad, y, inner)
	}
	y += dp(gtx, 12)
	if v.g.Disabled { // UserGroupTabs: isActive
		if !v.org && v.rights.Create { // renderEnableGroupButton
			y += v.enableSection(gtx, th, cl, w, y)
		}
		return layout.Dimensions{Size: image.Pt(w, y)}
	}
	if v.editable() { // renderActions: UserGroupsDetailsActionBar
		y += v.actionBar(gtx, th, cl, w, y)
	}
	// the tab menu
	y += dp(gtx, 12)
	tabs := [2]struct {
		label string
		n     int
	}{{"Members", v.g.UserCount}, {"Channels", len(v.g.Channels)}}
	tabH := dp(gtx, 40)
	x := pad
	for i, t := range tabs {
		label, count := t.label, strconv.Itoa(t.n)
		lw := textW(gtx, th, 15, theme.Bold, label)
		cw := textW(gtx, th, 13, theme.Regular, count)
		tw := lw + dp(gtx, 6) + cw
		sel := v.tab == i
		v.hit("tab:"+strconv.Itoa(i), image.Rect(x, y, x+tw, y+tabH).Add(image.Pt(0, dp(gtx, 49)-v.list.Position.Offset)))
		at(gtx, image.Pt(x, y), func(gtx layout.Context) layout.Dimensions {
			return clickBox(gtx, &v.tabBtns[i], nil, func(gtx layout.Context) layout.Dimensions {
				fg := cl.Fg2
				if sel {
					fg = cl.Fg
				}
				at(gtx, image.Pt(0, 0), func(gtx layout.Context) layout.Dimensions {
					return vcenter(gtx, tabH, func(gtx layout.Context) layout.Dimensions {
						return text(gtx, th, 15, theme.Bold, fg, label)
					})
				})
				at(gtx, image.Pt(lw+dp(gtx, 6), 0), func(gtx layout.Context) layout.Dimensions {
					return vcenter(gtx, tabH, func(gtx layout.Context) layout.Dimensions {
						return text(gtx, th, 13, theme.Regular, cl.Fg2, count)
					})
				})
				if sel {
					fill(gtx, image.Rect(0, tabH-dp(gtx, 2), tw, tabH), cl.Link)
				}
				return layout.Dimensions{Size: image.Pt(tw, tabH)}
			})
		})
		x += tw + dp(gtx, 24)
	}
	y += tabH
	fill(gtx, image.Rect(0, y, w, y+1), cl.Ring)
	y++
	if v.tab == 0 {
		return v.membersTab(gtx, th, cl, w, y, viewH)
	}
	return v.channelsTab(gtx, th, cl, w, y)
}

func (v *UserGroupView) membersTab(gtx layout.Context, th *theme.Theme, cl *Colors, w, y, viewH int) layout.Dimensions {
	pad := dp(gtx, 16)
	if v.org && (len(v.members) > 0 || v.searchQ != "" || v.loading) {
		y += pad
		box := image.Rect(pad, y, w-pad, y+dp(gtx, 36))
		actFocusBox(gtx, cl, box, gtx.Focused(&v.search))
		at(gtx, image.Pt(box.Min.X+dp(gtx, 32), box.Min.Y), func(gtx layout.Context) layout.Dimensions {
			fw := maxInt(1, box.Dx()-dp(gtx, 44))
			return vcenter(gtx, box.Dy(), func(gtx layout.Context) layout.Dimensions {
				gtx.Constraints = layout.Constraints{Min: image.Pt(fw, 0), Max: image.Pt(fw, box.Dy())}
				if v.search.Len() == 0 {
					call, _ := record(gtx, func(gtx layout.Context) layout.Dimensions {
						return text(gtx, th, 15, theme.Regular, cl.Placeholder, "Find members")
					})
					call.Add(gtx.Ops)
				}
				return th.Editor(gtx, &v.search, unit.Sp(15), cl.Fg, theme.WithAlpha(cl.Focus, 0x55))
			})
		})
		at(gtx, image.Pt(box.Min.X+dp(gtx, 10), box.Min.Y+(box.Dy()-dp(gtx, 16))/2), func(gtx layout.Context) layout.Dimensions {
			return icon(gtx, "search", 16, cl.Fg2)
		})
		y = box.Max.Y + pad
	}
	if len(v.members) == 0 && v.org && (!v.started || v.loading || v.failed) {
		return layout.Dimensions{Size: image.Pt(w, y+dp(gtx, 60))}
	}
	if len(v.members) == 0 {
		if !v.org && v.rights.Edit { // renderEmptyMemberState for an editor
			return v.emptyAction(gtx, th, cl, w, y, "Nobody’s in this group",
				ugWords(nsUG.T("You can add a few people with the button below.")), nsUG.T("Add Members"), &v.addMembers)
		}
		if !v.org { // the classic renderEmptyMemberState (no edit rights)
			if v.adminLink.Clicked(gtx) { // openPeopleBrowser({filters: {accountType: admins}})
				v.h.Navigate("directories?tab=people&account=admins")
			}
			return v.empty(gtx, th, cl, w, y, "Nobody’s in this group",
				[]ugWord{{s: "Contact"}, {s: "a"}, {s: "workspace administrator", link: &v.adminLink}, {s: "to"}, {s: "add"}, {s: "members."}})
		}
		if v.searchQ != "" {
			return v.empty(gtx, th, cl, w, y, "No results found", nil)
		}
		if v.rights.OrgDash { // EmptyMembersState with canUserAccessGroupsInOrgDash
			return v.empty(gtx, th, cl, w, y, "Nobody’s in this group", v.dashWords())
		}
		return v.empty(gtx, th, cl, w, y, "Nobody’s in this group", ugWords("Contact an org administrator to add members."))
	}
	rowH := dp(gtx, 60)
	for _, u := range v.members {
		u := u
		c := v.row("member:" + u.ID)
		more := v.row("more:" + u.ID)
		moreClicked := more.Clicked(gtx)                       // the ⋮ sits in the row: its click isn't the row's
		if c.Clicked(gtx) && !moreClicked && !more.Pressed() { // onViewMemberProfile
			pfOpenFull(v.h, u.ID)
		}
		ry := y
		at(gtx, image.Pt(0, ry), func(gtx layout.Context) layout.Dimensions {
			return clickBox(gtx, c, func(gtx layout.Context, sz image.Point, hovered bool) {
				if hovered {
					fill(gtx, image.Rectangle{Max: sz}, cl.BgAlt)
				}
			}, func(gtx layout.Context) layout.Dimensions {
				return layout.Dimensions{Size: image.Pt(w, rowH)}
			})
		})
		at(gtx, image.Pt(pad, ry+(rowH-dp(gtx, 36))/2), func(gtx layout.Context) layout.Dimensions {
			return pfImage(gtx, v.h, pfAvatarURL(u, false), 36, 6)
		})
		tx := pad + dp(gtx, 36) + dp(gtx, 12)
		tw := maxInt(1, w-tx-pad-dp(gtx, 36))
		sub := u.Title
		if pfBlank(sub) {
			sub = u.RealName
		}
		ny := ry + (rowH-dp(gtx, 22))/2
		if !pfBlank(sub) {
			ny = ry + (rowH-dp(gtx, 40))/2
		}
		at(gtx, image.Pt(tx, ny), func(gtx layout.Context) layout.Dimensions {
			return pfNameLine(gtx, v.h, th, nil, u, 15, 22, 9, tw)
		})
		if !pfBlank(sub) {
			at(gtx, image.Pt(tx, ny+dp(gtx, 22)), func(gtx layout.Context) layout.Dimensions {
				gtx.Constraints = layout.Constraints{Max: image.Pt(tw, dp(gtx, 18))}
				return para(gtx, th, 13, 18, theme.Regular, cl.Fg2, sub, 1)
			})
		}
		y += rowH
		fill(gtx, image.Rect(0, y-1, w, y), cl.Ring)
		if !v.org && !v.editable() {
			continue // the classic row's ⋮ is isContextButtonVisible: editors only
		}
		// the row's ⋮ (MemberRow's context button): "View profile"; the
		// classic pane's renderListItemOptionsMenu adds "Remove from
		// group…" when canRemoveItem
		if moreClicked {
			r := image.Rect(w-pad-dp(gtx, 28), ry+(rowH-dp(gtx, 28))/2, w-pad, ry+(rowH+dp(gtx, 28))/2)
			v.h.ShowOverlay(&pfPaneMenu{h: v.h, plain: true, paneW: v.paneW,
				box:   r.Add(image.Pt(0, dp(gtx, 49)-v.list.Position.Offset)),
				items: v.memberItems(u)})
		}
		v.hit("more:"+u.ID, image.Rect(w-pad-dp(gtx, 28), ry+(rowH-dp(gtx, 28))/2, w-pad, ry+(rowH+dp(gtx, 28))/2).Add(image.Pt(0, dp(gtx, 49)-v.list.Position.Offset)))
		at(gtx, image.Pt(w-pad-dp(gtx, 28), ry+(rowH-dp(gtx, 28))/2), func(gtx layout.Context) layout.Dimensions {
			gtx.Constraints = layout.Constraints{Max: image.Pt(dp(gtx, 28), dp(gtx, 28))}
			d := Button(gtx, th, more, BtnOutline, BtnSmall, "", "kebab", 0)
			if !v.org { // the classic row's tooltip, renderTooltip: "More actions"
				Tooltip(gtx, th, v.rowTip("more:"+u.ID), more.Hovered(), d.Size, Above, nsUG.T("More actions"), "")
			}
			return d
		})
	}
	// loadPost: the next page once the end is in view
	if v.org && !v.membersDone && !v.fetching && y-v.list.Position.Offset < viewH+dp(gtx, 200) {
		v.loadMembers(v.lastQ, v.cursor)
	}
	return layout.Dimensions{Size: image.Pt(w, y)}
}

func (v *UserGroupView) channelsTab(gtx layout.Context, th *theme.Theme, cl *Colors, w, y int) layout.Dimensions {
	chans := v.g.Channels
	if !v.org {
		chans = v.g.DefaultChannels() // getUserGroupDefaultChannels: channels, then the private ones
	}
	if len(chans) == 0 {
		if !v.org && v.rights.Edit { // renderEmptyChannelState for an editor
			return v.emptyAction(gtx, th, cl, w, y, "No default channels",
				ugWords("All members of a user group are automatically added to its default channels."), nsUG.T("Add Default Channels"), &v.addChannels)
		}
		if !v.org { // the classic renderEmptyChannelState (no edit rights)
			return v.empty(gtx, th, cl, w, y, "No default channels", ugWords("All members of a user group are automatically added to its default channels."))
		}
		if v.rights.OrgDash {
			return v.empty(gtx, th, cl, w, y, "No channels are connected to this group", v.dashWords())
		}
		return v.empty(gtx, th, cl, w, y, "No channels are connected to this group", ugWords("Contact an org administrator to add channels."))
	}
	pad := dp(gtx, 16)
	rowH := dp(gtx, 60)
	c := v.h.Core()
	for _, id := range chans {
		cv := core.Conv{ID: id, Name: id, Kind: "channel"}
		if c != nil {
			if r := c.ConvRef(id); r.Name != "" {
				cv.Name, cv.Kind = r.Name, r.Kind
			}
		}
		b := v.row("channel:" + id)
		more := v.row("chmore:" + id)
		moreClicked := more.Clicked(gtx)
		if b.Clicked(gtx) && !moreClicked && !more.Pressed() { // ChannelRow: Open channel
			v.h.Navigate("c/" + id)
		}
		ry := y
		at(gtx, image.Pt(0, ry), func(gtx layout.Context) layout.Dimensions {
			return clickBox(gtx, b, func(gtx layout.Context, sz image.Point, hovered bool) {
				if hovered {
					fill(gtx, image.Rectangle{Max: sz}, cl.BgAlt)
				}
			}, func(gtx layout.Context) layout.Dimensions {
				at(gtx, image.Pt(pad, 0), func(gtx layout.Context) layout.Dimensions {
					gtx.Constraints = layout.Constraints{Max: image.Pt(maxInt(1, w-2*pad), rowH)}
					return vcenter(gtx, rowH, func(gtx layout.Context) layout.Dimensions {
						return pfChannelLabel(gtx, v.h, th, cl.Fg, cv)
					})
				})
				return layout.Dimensions{Size: image.Pt(w, rowH)}
			})
		})
		y += rowH
		fill(gtx, image.Rect(0, y-1, w, y), cl.Ring)
		if v.org || !v.editable() {
			continue
		}
		// the classic channel row's ⋮: "Open channel", "Remove from group…"
		if moreClicked {
			r := image.Rect(w-pad-dp(gtx, 28), ry+(rowH-dp(gtx, 28))/2, w-pad, ry+(rowH+dp(gtx, 28))/2)
			v.h.ShowOverlay(&pfPaneMenu{h: v.h, plain: true, paneW: v.paneW,
				box:   r.Add(image.Pt(0, dp(gtx, 49)-v.list.Position.Offset)),
				items: v.channelItems(cv)})
		}
		v.hit("chmore:"+id, image.Rect(w-pad-dp(gtx, 28), ry+(rowH-dp(gtx, 28))/2, w-pad, ry+(rowH+dp(gtx, 28))/2).Add(image.Pt(0, dp(gtx, 49)-v.list.Position.Offset)))
		at(gtx, image.Pt(w-pad-dp(gtx, 28), ry+(rowH-dp(gtx, 28))/2), func(gtx layout.Context) layout.Dimensions {
			gtx.Constraints = layout.Constraints{Max: image.Pt(dp(gtx, 28), dp(gtx, 28))}
			d := Button(gtx, th, more, BtnOutline, BtnSmall, "", "kebab", 0)
			Tooltip(gtx, th, v.rowTip("chmore:"+id), more.Hovered(), d.Size, Above, nsUG.T("More actions"), "")
			return d
		})
	}
	return layout.Dimensions{Size: image.Pt(w, y)}
}

// ugWord is a word of an empty state's line; link makes it a link.
type ugWord struct {
	s    string
	link *widget.Clickable
}

func ugWords(s string) []ugWord {
	var out []ugWord
	for _, f := range strings.Fields(s) {
		out = append(out, ugWord{s: f})
	}
	return out
}

// empty is the panes' EmptyState (gkKy): the 18/900 header (margin-bottom
// 16) and the centred 15px description, both wrapping at words; a link
// word is drawn in the link colour and clicks.
func (v *UserGroupView) empty(gtx layout.Context, th *theme.Theme, cl *Colors, w, y int, head string, sub []ugWord) layout.Dimensions {
	pad := dp(gtx, 16)
	y += dp(gtx, 40)
	inner := maxInt(1, w-2*pad)
	d := at(gtx, image.Pt(pad, y), func(gtx layout.Context) layout.Dimensions {
		return paraCenteredW(gtx, th, 18, 24, theme.Black, cl.Fg, head, inner)
	})
	y += d.Size.Y + pad
	if len(sub) == 0 {
		return layout.Dimensions{Size: image.Pt(w, y)}
	}
	space := textW(gtx, th, 15, theme.Regular, " ")
	lh := dp(gtx, 22)
	var lines [][]ugWord
	lineW := 0
	for _, wd := range sub {
		ww := textW(gtx, th, 15, theme.Regular, wd.s)
		if n := len(lines); n > 0 && lineW+space+ww <= inner {
			lines[n-1] = append(lines[n-1], wd)
			lineW += space + ww
			continue
		}
		lines = append(lines, []ugWord{wd})
		lineW = ww
	}
	for _, ln := range lines {
		lw := 0
		for i, wd := range ln {
			if i > 0 {
				lw += space
			}
			lw += textW(gtx, th, 15, theme.Regular, wd.s)
		}
		x := pad + maxInt(0, (inner-lw)/2)
		for _, wd := range ln {
			wd := wd
			ww := textW(gtx, th, 15, theme.Regular, wd.s)
			at(gtx, image.Pt(x, y), func(gtx layout.Context) layout.Dimensions {
				draw := func(gtx layout.Context) layout.Dimensions {
					gtx.Constraints = layout.Constraints{Max: image.Pt(ww, lh)}
					return vcenter(gtx, lh, func(gtx layout.Context) layout.Dimensions {
						if wd.link != nil {
							return text(gtx, th, 15, theme.Regular, cl.Link, wd.s)
						}
						return text(gtx, th, 15, theme.Regular, cl.Fg, wd.s)
					})
				}
				if wd.link != nil {
					return clickBox(gtx, wd.link, nil, func(gtx layout.Context) layout.Dimensions {
						draw(gtx)
						return layout.Dimensions{Size: image.Pt(ww, lh)}
					})
				}
				return draw(gtx)
			})
			x += ww + space
		}
		y += lh
	}
	return layout.Dimensions{Size: image.Pt(w, y+pad)}
}
