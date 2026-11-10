package ui

// A group DM's channel header, from Slack's ViewHeader (view-header chunk
// W58+, build 4d0c78085232) and the components it renders for an mpdm:
//
//   - the title is MPDMTitle (boot-shared m6yC, shouldTruncate): a named
//     group DM (core.MpdmCustomName, tpcx JM) shows its name after
//     BaseChannelPrefixMpim (kn1I: the first other member's 16px avatar
//     with the bottom-right corner cut away, and a counter of the others,
//     max(members-1, 2), variant "secondary"); otherwise the other
//     members' names sorted (3gAC Z), all of them up to three ("A, B, C",
//     listify noConj), else "A, B, N others". There is no # or lock glyph.
//   - under mpdm_naming the title button is .p-view_header__big_button--mpdm
//     in .p-view_header__text--mpdm-naming: padding 3px 8px, the prefix
//     and the name 8px apart (gap) with the prefix's own 0 8px 0 -4px
//     margins;
//   - EditMpdmNameButton (core.CanNameMpdm): a small IconButton "edit"
//     (28px, base-pry, radius 4) in .p-view_header__big_button_wrapper
//     (padding 2px 8px 2px 0, radius 4, base-sec on hover). It only shows
//     while the wrapper is hovered, 4px after the title, and the wrapper
//     keeps its idle width (max-width: var(--title-idle-width)), so the
//     title gives up the room. Its tooltip (below) is "Edit conversation
//     name" for a group DM with a custom name, else "Name this
//     conversation"; a click opens RenameMpdmDialog (entrypoint header);
//   - the topic slot: a named group DM without a topic shows
//     MpdmMemberNamesSubtitle (0bGD) there, the same member list in
//     .p-dms_channel__mpdm_members (13px, content.sec).

import (
	"image"
	"image/color"
	"sort"
	"strconv"
	"strings"

	"gioui.org/layout"

	"github.com/some-du6e/clack/internal/ui/popups"
	"github.com/some-du6e/clack/internal/ui/richview"
	"github.com/some-du6e/clack/internal/ui/theme"
)

// headerExtra is the channel header's state beyond its clickables.
type headerExtra struct {
	nameTip popups.TipState // EditMpdmNameButton's tooltip
	// the wide topic's hover card (channel_topic.go)
	card      *richview.View
	cardKey   string
	cardStyle *richview.Style
	cardDark  bool
	cardFg    color.NRGBA
}

func (a *App) hdrX() *headerExtra {
	if a.hdrx == nil {
		a.hdrx = &headerExtra{}
	}
	return a.hdrx
}

// mpdmOthers are a group DM's members other than the viewer, sorted by
// display name (3gAC Z), with those names.
func (a *App) mpdmOthers(conv *convInfo) (ids, names []string) {
	c := a.Core()
	if c == nil || conv == nil {
		return nil, nil
	}
	self := c.Self().ID
	type m struct{ id, name string }
	var ms []m
	for _, id := range conv.Members {
		if id == self {
			continue
		}
		u, ok := c.User(id)
		if !ok || u.Name == "" {
			c.S.UserName(id) // queues the member for the resolve loop ("names" redraws)
			continue
		}
		ms = append(ms, m{id, u.Name})
	}
	sort.SliceStable(ms, func(i, j int) bool { return strings.ToLower(ms[i].name) < strings.ToLower(ms[j].name) })
	for _, x := range ms {
		ids, names = append(ids, x.id), append(names, x.name)
	}
	return ids, names
}

// mpdmMemberList is MPDMTitle's (and MpdmMemberNamesSubtitle's) member
// text: every name up to three, else the first two and how many others.
func mpdmMemberList(names []string) string {
	switch {
	case len(names) == 0:
		return ""
	case len(names) <= 3:
		return strings.Join(names, ", ")
	}
	n := len(names) - 2
	if n == 1 {
		return names[0] + ", " + names[1] + ", 1 other"
	}
	return names[0] + ", " + names[1] + ", " + strconv.Itoa(n) + " others"
}

// mpdmHeaderTitle is MPDMTitle's text and whether it is a custom name.
func (a *App) mpdmHeaderTitle(conv *convInfo) (string, bool) {
	if c := a.Core(); c != nil && conv != nil {
		if n, ok := c.MpdmCustomName(conv.ID); ok {
			return n, true
		}
	}
	_, names := a.mpdmOthers(conv)
	if t := mpdmMemberList(names); t != "" {
		return t, false
	}
	return a.convTitle(conv, ""), false
}

// mpdmNamingOn is p-view_header__text--mpdm-naming (experimentMpdmNamingGroupOn).
func (a *App) mpdmNamingOn() bool {
	c := a.Core()
	return c != nil && c.Experiment("mpdm_naming") == "on"
}

// mpdmPrefixCount is getMpimMemberCount: max(members-1, 2).
func mpdmPrefixCount(conv *convInfo) int {
	return max(len(conv.Members)-1, 2)
}

// layoutMpdmTitle draws a group DM's title button (and, when the viewer
// may name it, the edit-name button inside the wrapper) at x in a header
// of height h, at most avail wide. It returns the width it took.
func (a *App) layoutMpdmTitle(gtx layout.Context, ch string, conv *convInfo, x, avail, h int) int {
	c := &a.th.C
	title, named := a.mpdmHeaderTitle(conv)
	naming := a.mpdmNamingOn()
	core := a.Core()
	canName := core != nil && core.CanNameMpdm(ch)
	pad := gtx.Dp(4) // .p-view_header__big_button: padding 2px 4px
	if naming {
		pad = gtx.Dp(8) // --mpdm under mpdm-naming: 3px 8px
	}
	lead := 0 // what comes before the name
	if named {
		lead = gtx.Dp(-4) + gtx.Dp(20) + gtx.Dp(8) + gtx.Dp(8) // the prefix's margins and the 8px gap
	}
	nw := a.th.Measure(gtx, 18, theme.Black, title)
	btnW := pad + lead + nw // its right padding is 0 inside the wrapper
	if !canName {
		btnW += pad
	}
	wrapPad := 0
	if canName {
		wrapPad = gtx.Dp(8) // the wrapper's padding-right
	}
	idle := min(max(btnW+wrapPad, gtx.Dp(88)), avail)
	hdrMpdmTitleRect = image.Rect(x, (h-gtx.Dp(32))/2, x+idle, (h+gtx.Dp(32))/2)
	hdrMpdmEditShown = false
	wrapHov := a.clicks.get("hdr.mpdmwrap")
	editW := gtx.Dp(4) + gtx.Dp(28)
	at(gtx, image.Pt(x, (h-gtx.Dp(32))/2), image.Pt(idle, gtx.Dp(32)), func(gtx layout.Context) layout.Dimensions {
		return wrapHov.Layout(gtx, func(gtx layout.Context) layout.Dimensions {
			th := gtx.Dp(32)
			showEdit := canName && wrapHov.Hovered()
			if canName && wrapHov.Hovered() { // the wrapper's hover; the button inside stays clear
				theme.RRect(gtx, image.Rect(0, 0, idle, th), gtx.Dp(4), c.BgHover)
			}
			room := idle - wrapPad
			if showEdit {
				room -= editW
			}
			at(gtx, image.Point{}, image.Pt(room, th), func(gtx layout.Context) layout.Dimensions {
				return clickArea(gtx, a.clicks.get("hdr.title"), func(gtx layout.Context, s image.Point, hov bool) {
					if hov && !canName {
						theme.RRect(gtx, image.Rectangle{Max: s}, gtx.Dp(6), c.BgHover)
					}
				}, func(gtx layout.Context) layout.Dimensions {
					tx := pad
					if named {
						at(gtx, image.Pt(pad-gtx.Dp(4), (th-gtx.Dp(20))/2), image.Pt(gtx.Dp(24), gtx.Dp(24)), func(gtx layout.Context) layout.Dimensions {
							return a.mpdmPrefix(gtx, conv)
						})
						tx += lead
					}
					w := max(room-tx, 0)
					at(gtx, image.Pt(tx, 0), image.Pt(w, th), func(gtx layout.Context) layout.Dimensions {
						return vcenter(gtx, th, func(gtx layout.Context) layout.Dimensions {
							return a.th.Layout(gtx, theme.TextStyle{Size: 18, Weight: theme.Black, Color: c.Fg, MaxLines: 1}, title)
						})
					})
					return layout.Dimensions{Size: image.Pt(room, th)}
				})
			})
			if showEdit {
				hdrMpdmEditShown = true
				bx := room + gtx.Dp(4)
				at(gtx, image.Pt(bx, (th-gtx.Dp(28))/2), image.Pt(gtx.Dp(28), gtx.Dp(28)), func(gtx layout.Context) layout.Dimensions {
					theme.RRect(gtx, image.Rect(0, 0, gtx.Dp(28), gtx.Dp(28)), gtx.Dp(4), c.Bg) // base-pry
					d := iconButton(gtx, a.clicks.get("hdr.mpdmname"), "edit", 28, 20, c.FgMuted, c.BgHover, 4)
					tip := "Name this conversation"
					if core.MpdmHasCustomName(ch) {
						tip = "Edit conversation name"
					}
					popups.Tooltip(gtx, a.th, &a.hdrX().nameTip, a.clicks.get("hdr.mpdmname").Hovered(), d.Size, popups.Below, tip, "")
					return d
				})
			}
			return layout.Dimensions{Size: image.Pt(idle, th)}
		})
	})
	if clicked(a.clicks.get("hdr.mpdmname")) {
		a.openRenameMpdm(ch, conv)
	}
	return idle
}

// hdrMpdmTitleRect is the last group DM title wrapper (header
// coordinates) and hdrMpdmEditShown whether its edit-name button was
// drawn (tests).
var (
	hdrMpdmTitleRect image.Rectangle
	hdrMpdmEditShown bool
)

// openRenameMpdm opens RenameMpdmDialog for ch.
func (a *App) openRenameMpdm(ch string, conv *convInfo) {
	_, names := a.mpdmOthers(conv)
	a.ShowOverlay(popups.NewRenameMpdmDialog(a, ch, strings.Join(names, ", ")))
}

// mpdmPrefix is BaseChannelPrefixMpim at 16px, variant secondary
// (popups.MpimPrefix), with the first other member's avatar.
func (a *App) mpdmPrefix(gtx layout.Context, conv *convInfo) layout.Dimensions {
	ids, _ := a.mpdmOthers(conv)
	var av layout.Widget
	if len(ids) > 0 {
		url := ""
		if u, ok := a.Core().User(ids[0]); ok {
			url = u.Avatar
		}
		av = func(gtx layout.Context) layout.Dimensions {
			s := gtx.Constraints.Max.X
			return a.avatar(gtx, url, "", s, gtx.Dp(4))
		}
	}
	return popups.MpimPrefix(gtx, a.th, 16, mpdmPrefixCount(conv), av)
}

// layoutMpdmSubtitle is MpdmMemberNamesSubtitle in the topic slot r.
func (a *App) layoutMpdmSubtitle(gtx layout.Context, conv *convInfo, r image.Rectangle) {
	_, names := a.mpdmOthers(conv)
	t := mpdmMemberList(names)
	if t == "" {
		return
	}
	c := &a.th.C
	at(gtx, r.Min, r.Size(), func(gtx layout.Context) layout.Dimensions {
		return vcenter(gtx, r.Dy(), func(gtx layout.Context) layout.Dimensions {
			return a.th.Layout(gtx, theme.TextStyle{Size: 13, Weight: theme.Regular, Color: c.FgMuted, MaxLines: 1}, t)
		})
	})
}
