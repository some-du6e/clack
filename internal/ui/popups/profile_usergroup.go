package popups

import (
	"context"
	"image"
	"image/color"
	"strconv"
	"time"

	"gioui.org/font"
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
// the flexpane of client-channel-user-group-view's UserGroupDetails:
//
//   - the pane header "User Group";
//   - UserGroupHeader (.p-user_group_details__section, margin 12px 16px):
//     the name (18/900; "<name> (Deactivated)" for a disabled group), the
//     description (15px), the @handle (13/700, secondary, margin-top 4);
//   - UserGroupTabs (only for an active group; margin-top 12, tab menu
//     padding 0 16): "Members" and "Channels" with their counts
//     (user_count, the group's channels);
//   - Members (AssignedGroupMembers): "Find members" (padding 16) once
//     there is someone or a search, the members from
//     searchAndUpsertUserGroupMembers (search.enterprise, 100 a page,
//     300 ms debounced search; core.UsergroupMembers) as 60px rows whose ⋮
//     menu has "View profile"; empty: "Nobody’s in this group" / "Contact
//     an org administrator to add members." ("No results found" for a
//     search);
//   - Channels (ConnectedChannels): the group's channels (Open channel);
//     empty: "No channels are connected to this group" / "Contact an org
//     administrator to add channels.".
//
// The group itself is the boot list's, refreshed by usergroups.search
// (keyword: the handle, count 1; core.UsergroupProfile).
type UserGroupView struct {
	h    Host
	id   string
	g    core.UsergroupProfile
	list layout.List

	asked, loaded bool

	tab               int // 0 Members, 1 Channels
	tabBtns           [2]widget.Clickable
	closeBtn          widget.Clickable
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
}

// NewUserGroupView builds the pane for group id.
func NewUserGroupView(h Host, id string) *UserGroupView {
	v := &UserGroupView{h: h, id: id, rows: map[string]*widget.Clickable{}}
	v.list.Axis = layout.Vertical
	v.search.SingleLine = true
	if c := h.Core(); c != nil {
		c.S.RLock()
		for _, g := range c.S.Groups {
			if g.ID == id {
				v.g.Usergroup = core.Usergroup{ID: g.ID, Handle: g.Handle, Name: g.Name, Description: g.Description, UserCount: g.UserCount}
			}
		}
		c.S.RUnlock()
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

// fetch refreshes the group and loads the first members page once.
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
				v.g = g
			}
			v.loaded = true
			h.Invalidate()
		})
	v.loadMembers("", "")
}

// loadMembers fetches a members page for term from cursor ("" first).
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
	run(h, func(ctx context.Context) (struct {
		us   []core.User
		next string
	}, error) {
		us, next, _, err := c.UsergroupMembers(ctx, id, term, cursor)
		return struct {
			us   []core.User
			next string
		}{us, next}, err
	}, func(r struct {
		us   []core.User
		next string
	}, err error) {
		if seq != v.seq {
			return
		}
		v.loading, v.fetching = false, false
		if cursor == "" {
			v.members = nil
		}
		if err == nil {
			for _, u := range r.us {
				if !u.Deleted {
					v.members = append(v.members, u)
				}
			}
		}
		v.cursor, v.membersDone = r.next, err != nil || r.next == ""
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
	y += dp(gtx, 12)
	if v.g.Disabled { // UserGroupTabs: isActive
		return layout.Dimensions{Size: image.Pt(w, y)}
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
	if len(v.members) > 0 || v.searchQ != "" || v.loading {
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
	if v.loading && len(v.members) == 0 {
		return layout.Dimensions{Size: image.Pt(w, y+dp(gtx, 60))}
	}
	if len(v.members) == 0 {
		head, sub := "Nobody’s in this group", "Contact an org administrator to add members."
		if v.searchQ != "" {
			head, sub = "No results found", ""
		}
		return v.empty(gtx, th, cl, w, y, head, sub)
	}
	rowH := dp(gtx, 60)
	for _, u := range v.members {
		u := u
		c := v.row("member:" + u.ID)
		if c.Clicked(gtx) {
			pfOpenFull(v.h, u.ID)
		}
		more := v.row("more:" + u.ID)
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
		// the row's ⋮ (UserGroupBaseRow context button): "View profile"
		if more.Clicked(gtx) {
			r := image.Rect(w-pad-dp(gtx, 28), ry+(rowH-dp(gtx, 28))/2, w-pad, ry+(rowH+dp(gtx, 28))/2)
			id := u.ID
			v.h.ShowOverlay(&pfPaneMenu{h: v.h, plain: true, paneW: v.paneW,
				box:   r.Add(image.Pt(0, dp(gtx, 49)-v.list.Position.Offset)),
				items: []MenuItem{{Label: "View profile", Do: func() { pfOpenFull(v.h, id) }}}})
		}
		at(gtx, image.Pt(w-pad-dp(gtx, 28), ry+(rowH-dp(gtx, 28))/2), func(gtx layout.Context) layout.Dimensions {
			gtx.Constraints = layout.Constraints{Max: image.Pt(dp(gtx, 28), dp(gtx, 28))}
			return Button(gtx, th, more, BtnOutline, BtnSmall, "", "kebab", 0)
		})
		y += rowH
		fill(gtx, image.Rect(0, y-1, w, y), cl.Ring)
	}
	// loadPost: the next page once the end is in view
	if !v.membersDone && !v.fetching && y-v.list.Position.Offset < viewH+dp(gtx, 200) {
		v.loadMembers(v.lastQ, v.cursor)
	}
	return layout.Dimensions{Size: image.Pt(w, y)}
}

func (v *UserGroupView) channelsTab(gtx layout.Context, th *theme.Theme, cl *Colors, w, y int) layout.Dimensions {
	if len(v.g.Channels) == 0 {
		return v.empty(gtx, th, cl, w, y, "No channels are connected to this group", "Contact an org administrator to add channels.")
	}
	pad := dp(gtx, 16)
	rowH := dp(gtx, 60)
	c := v.h.Core()
	for _, id := range v.g.Channels {
		cv := core.Conv{ID: id, Name: id, Kind: "channel"}
		if c != nil {
			if r := c.ConvRef(id); r.Name != "" {
				cv.Name, cv.Kind = r.Name, r.Kind
			}
		}
		b := v.row("channel:" + id)
		if b.Clicked(gtx) { // ChannelRow: Open channel
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
	}
	return layout.Dimensions{Size: image.Pt(w, y)}
}

// empty is EmptyMembersState / EmptyChannelsState: the 18/900 header
// (margin-bottom 16) and the centred 15px line.
func (v *UserGroupView) empty(gtx layout.Context, th *theme.Theme, cl *Colors, w, y int, head, sub string) layout.Dimensions {
	pad := dp(gtx, 16)
	y += dp(gtx, 40)
	inner := maxInt(1, w-2*pad)
	d := at(gtx, image.Pt(pad, y), func(gtx layout.Context) layout.Dimensions {
		return pfCenteredLine(gtx, th, 18, 24, theme.Black, cl.Fg, head, inner)
	})
	y += d.Size.Y + pad
	if sub != "" {
		d = at(gtx, image.Pt(pad, y), func(gtx layout.Context) layout.Dimensions {
			return pfCenteredLine(gtx, th, 15, 22, theme.Regular, cl.Fg, sub, inner)
		})
		y += d.Size.Y + pad
	}
	return layout.Dimensions{Size: image.Pt(w, y)}
}

// pfCenteredLine draws s centred in w (one line, cut to fit).
func pfCenteredLine(gtx layout.Context, th *theme.Theme, size, lh float32, wt font.Weight, c color.NRGBA, s string, w int) layout.Dimensions {
	tw := minInt(textW(gtx, th, size, wt, s), w)
	at(gtx, image.Pt((w-tw)/2, 0), func(gtx layout.Context) layout.Dimensions {
		gtx.Constraints = layout.Constraints{Max: image.Pt(tw, dp(gtx, lh))}
		return para(gtx, th, size, lh, wt, c, s, 1)
	})
	return layout.Dimensions{Size: image.Pt(w, dp(gtx, lh))}
}
