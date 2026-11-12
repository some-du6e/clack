package views

import (
	"context"
	"image"
	"image/color"

	"gioui.org/layout"

	"github.com/some-du6e/clack/internal/core"
	"github.com/some-du6e/clack/internal/ui/theme"
)

// The page's suggestions (Igs2), shown before you have any huddle
// history:
//
//   - HuddlesPageDmSuggestions: "Direct messages — Talk privately 1:1
//     with someone" over up to three HuddlesSuggestionTile cards (360
//     wide, 12 apart, shrinking to fit; the third hidden in a window up to
//     1000 wide): the People browser's first page without you (useBrowser
//     PEOPLE, the directory's search.modules.people), each card a
//     huddles.listBackgrounds picture (136 tall) with your face and theirs
//     (60px, 8 apart), their name with their status emoji (or presence),
//     their status text, and "Start Huddle" on hover. With exactly one
//     person in the browser, HuddlesPageInviteTeammates instead;
//   - HuddlesPageChannelSuggestions: "Channels — Meet with a whole team,
//     or just let people drop in and out" over up to three 56px rows
//     (getChannelHuddleSuggestionsForNUX: the first conversations you're
//     in that support huddles), each with "Start Huddle" on hover.
//
// A card or row opens HuddleInviteStartModal with that person or
// conversation picked — only when it has a conversation id (Slack checks
// isValidChannelId of the person's DM).

// hdTeammatePNGs are HuddlesPageInviteTeammates' pictures (512×512).
var hdTeammatePNGs = [...]string{
	"https://a.slack-edge.com/bv1-13-br/new_teammate_1-a8410a2.png",
	"https://a.slack-edge.com/bv1-13-br/new_teammate_2-65c8752.png",
	"https://a.slack-edge.com/bv1-13-br/new_teammate_3-47cda13.png",
}

// peopleSuggestions is the People browser's first page (as the
// directory loads it), minus you.
func (v *Huddles) peopleSuggestions() (all []core.User, ok bool) {
	if !v.people.started() {
		app := v.h.Core()
		v.people.load(v.h, false, func(ctx context.Context) ([]core.User, error) {
			res, err := app.BrowsePeople(ctx, core.PeopleBrowseOpts{Page: 1})
			if err != nil {
				return nil, err
			}
			return res.Users, nil
		})
	}
	us, loaded, _ := v.people.get()
	return us, loaded
}

// imFor is the person's DM (getImChannelIdForUser), "" without one.
func (v *Huddles) imFor(uid string) string {
	for _, c := range v.h.Core().Snapshot().Convs {
		if c.Kind == "im" && c.User == uid {
			return c.ID
		}
	}
	return ""
}

// background is the i-th huddles.listBackgrounds picture, else the first.
func (v *Huddles) background(i int) string {
	v.mu.Lock()
	defer v.mu.Unlock()
	if len(v.bgs) == 0 {
		return ""
	}
	if i < len(v.bgs) {
		return v.bgs[i].URL
	}
	return v.bgs[0].URL
}

// sectionTitle is the suggestions' title row: the bold title (15px
// content-pry), " — " (the separator's bold "\2014 ", 5px each side) and
// the subtitle.
func (v *Huddles) sectionTitle(gtx C, title, sub string) D {
	fg := v.tok("dt_color-content-pry")
	return row(gtx, layout.Baseline,
		layout.Rigid(func(gtx C) D { return v.label(gtx, 15, wBold, fg, title) }),
		layout.Rigid(func(gtx C) D {
			return layout.Inset{Left: 5, Right: 5}.Layout(gtx, func(gtx C) D { return v.label(gtx, 15, wBold, fg, "— ") })
		}),
		layout.Flexed(1, func(gtx C) D { return v.label(gtx, 15, wReg, fg, sub) }),
	)
}

func (v *Huddles) layoutDMSuggestions(gtx C) D {
	w := gtx.Constraints.Max.X
	x, cw := hdColumn(gtx, w)
	all, loaded := v.peopleSuggestions()
	if !loaded {
		return D{Size: image.Pt(w, 0)}
	}
	top := gtx.Dp(20) // .p-huddles-page-content: margin 20px auto
	if len(all) == 1 {
		d := func() D {
			defer opOffset(gtx, image.Pt(x, top)).Pop()
			return v.inviteTeammates(gtx)
		}()
		return D{Size: image.Pt(w, top+d.Size.Y+gtx.Dp(20))}
	}
	self := v.selfID()
	var ids []string
	for _, u := range all {
		if u.ID != self && len(ids) < 3 {
			ids = append(ids, u.ID)
		}
	}
	defer opOffset(gtx, image.Pt(x, top)).Pop()
	g := gtx
	g.Constraints = layout.Constraints{Max: image.Pt(cw, gtx.Constraints.Max.Y)}
	d := v.sectionTitle(g, nsHuddles.T("Direct messages"), nsHuddles.T("Talk privately 1:1 with someone"))
	y := d.Size.Y + gtx.Dp(12)
	n := len(ids)
	if n == 3 && w <= gtx.Dp(1000) { // @media (width<=1000px): the last card hides
		n = 2
	}
	if n > 0 {
		gap := gtx.Dp(12)
		tw := min(gtx.Dp(360), (cw-(n-1)*gap)/n)
		tileH := 0
		for i := 0; i < n; i++ {
			func() {
				defer opOffset(gtx, image.Pt(i*(tw+gap), y)).Pop()
				g := gtx
				g.Constraints = layout.Constraints{Min: image.Pt(tw, 0), Max: image.Pt(tw, 1<<20)}
				td := v.suggestionTile(g, ids[i], v.background(i))
				tileH = max(tileH, td.Size.Y)
			}()
		}
		y += tileH
	}
	return D{Size: image.Pt(w, top+y+gtx.Dp(20))}
}

// suggestionTile is HuddlesSuggestionTile.
func (v *Huddles) suggestionTile(gtx C, uid, bgURL string) D {
	w := gtx.Constraints.Max.X
	c := v.clk("person:" + uid)
	im := v.imFor(uid)
	if c.Clicked(gtx) && im != "" {
		v.openStart("huddles_page_nux_member_suggestion", "", uid)
	}
	u, _ := v.user(uid)
	hov := c.Hovered()
	return c.Layout(gtx, func(gtx C) D {
		pointerCursor(gtx)
		hh := gtx.Dp(136)
		// the content row, measured first
		fg := v.tok("dt_color-content-pry")
		bg := v.tok("dt_color-base-pry")
		m := recordTo(gtx, func(gtx C) D {
			gtx.Constraints = layout.Constraints{Min: image.Pt(w, 0), Max: image.Pt(w, 1<<20)}
			return layout.Inset{Top: 12, Right: 12, Bottom: 8, Left: 12}.Layout(gtx, func(gtx C) D {
				return row(gtx, layout.Middle,
					layout.Flexed(1, func(gtx C) D {
						return col(gtx,
							layout.Rigid(func(gtx C) D {
								return row(gtx, layout.Middle,
									layout.Flexed(1, func(gtx C) D { return v.label(gtx, 15, wBold, fg, displayName(u)) }),
									layout.Rigid(func(gtx C) D {
										if u.StatusEmojiURL != "" {
											return layout.Inset{Left: 6}.Layout(gtx, func(gtx C) D { return v.emojiImg(gtx, u.StatusEmojiURL, 16) })
										}
										return dirPresence(&v.base, gtx, u.Presence)
									}),
								)
							}),
							layout.Rigid(func(gtx C) D {
								if u.StatusText == "" {
									return D{} // the presence text is there at opacity 0, width 0
								}
								return row(gtx, layout.Middle,
									layout.Rigid(func(gtx C) D {
										if u.StatusEmojiURL == "" {
											return D{}
										}
										return layout.Inset{Right: 8}.Layout(gtx, func(gtx C) D { return v.emojiImg(gtx, u.StatusEmojiURL, 16) })
									}),
									layout.Flexed(1, func(gtx C) D { return v.label(gtx, 13, wReg, fg, u.StatusText) }),
								)
							}),
						)
					}),
					layout.Rigid(func(gtx C) D {
						return layout.Inset{Left: 12}.Layout(gtx, func(gtx C) D { return v.startDisplay(gtx, hov, bg) })
					}),
				)
			})
		})
		h := hh + m.d.Size.Y
		r := image.Rect(0, 0, w, h)
		rad := gtx.Dp(8)
		v.cardShadow(gtx, r, rad)
		func() {
			defer clipRR(gtx, r, rad, rad, rad, rad).Pop()
			fill(gtx, r, bg)
			if bgURL != "" {
				v.cdnImage(gtx, bgURL, image.Pt(w, hh))
			}
			v.facepile(gtx, []string{v.selfID(), uid}, 60, 2, image.Rect(0, 0, w, hh))
			func() {
				defer opOffset(gtx, image.Pt(0, hh)).Pop()
				m.call.Add(gtx.Ops)
			}()
		}()
		return D{Size: image.Pt(w, h)}
	})
}

// cardShadow is the tiles' box-shadow: 0 0 0 1px rgba(foreground_low,
// .13) and 0 1px 3px #00000014.
func (v *Huddles) cardShadow(gtx C, r image.Rectangle, rad int) {
	theme.Shadow(gtx, r.Add(image.Pt(0, gtx.Dp(1))), rad, gtx.Dp(3), argb(0x14000000))
	ring := flatten(theme.WithAlpha(v.tok("dt_color-content-pry"), 0x21), v.tok("dt_color-base-sec"))
	border(gtx, r.Inset(-max(1, gtx.Dp(1))), rad+max(1, gtx.Dp(1)), max(1, gtx.Dp(1)), ring)
}

// startDisplay is HuddlesPageStartDisplay: "Start Huddle" (15px bold,
// padding 6px 12px, radius 6, base-pry), shown only while its card or row
// is hovered, with a ring and a soft 18px shadow.
func (v *Huddles) startDisplay(gtx C, shown bool, bg color.NRGBA) D {
	label := nsHuddles.T("Start Huddle")
	tw := v.th.Measure(gtx, 15, wBold, label)
	sz := image.Pt(tw+2*gtx.Dp(12), gtx.Dp(22)+2*gtx.Dp(6))
	if !shown {
		return D{Size: sz}
	}
	r := image.Rectangle{Max: sz}
	rad := gtx.Dp(6)
	theme.Shadow(gtx, r.Add(image.Pt(0, gtx.Dp(6))), rad, gtx.Dp(12), argb(0x14000000))
	rrect(gtx, r, rad, v.tok("dt_color-base-pry"))
	border(gtx, r.Inset(-max(1, gtx.Dp(1))), rad+1, max(1, gtx.Dp(1)), flatten(theme.WithAlpha(v.tok("dt_color-content-pry"), 0x21), bg))
	g := gtx
	g.Constraints = layout.Exact(sz)
	layout.Center.Layout(g, func(gtx C) D { return v.label(gtx, 15, wBold, v.tok("dt_color-content-pry"), label) })
	return D{Size: sz}
}

// inviteTeammates is HuddlesPageInviteTeammates: a 346px card, its 136px
// header (the first background) holding Slack's three teammate pictures
// and the user-add tile (60px each, 12 apart), then "Invite teammates",
// "Add teammates to huddle 1:1" and the outline medium "Add coworkers"
// (Slack's invite modal, source huddles_page; Clack has no invite flow).
func (v *Huddles) inviteTeammates(gtx C) D {
	w := gtx.Dp(346)
	hh := gtx.Dp(136)
	fg := v.tok("dt_color-content-pry")
	bg := v.tok("dt_color-base-pry")
	m := recordTo(gtx, func(gtx C) D {
		gtx.Constraints = layout.Constraints{Min: image.Pt(w, 0), Max: image.Pt(w, 1<<20)}
		return layout.Inset{Top: 12, Right: 12, Bottom: 8, Left: 12}.Layout(gtx, func(gtx C) D {
			return row(gtx, layout.Middle,
				layout.Flexed(1, func(gtx C) D {
					return col(gtx,
						layout.Rigid(func(gtx C) D { return v.label(gtx, 15, wBold, fg, nsHuddles.T("Invite teammates")) }),
						layout.Rigid(func(gtx C) D { return v.label(gtx, 13, wReg, fg, nsHuddles.T("Add teammates to huddle 1:1")) }),
					)
				}),
				layout.Rigid(func(gtx C) D { return v.outlineButton(gtx, v.clk("invite"), "", nsHuddles.T("Add coworkers"), false) }),
			)
		})
	})
	h := hh + m.d.Size.Y
	r := image.Rect(0, 0, w, h)
	rad := gtx.Dp(8)
	v.cardShadow(gtx, r, rad)
	func() {
		defer clipRR(gtx, r, rad, rad, rad, rad).Pop()
		fill(gtx, r, bg)
		if u := v.background(0); u != "" {
			v.cdnImage(gtx, u, image.Pt(w, hh))
		}
		s, gap := gtx.Dp(60), gtx.Dp(12)
		x0 := (w - (4*s + 3*gap)) / 2
		y0 := (hh - s) / 2
		for i, u := range hdTeammatePNGs {
			func() {
				defer opOffset(gtx, image.Pt(x0+i*(s+gap), y0)).Pop()
				v.cdnImage(gtx, u, image.Pt(s, s))
			}()
		}
		func() {
			defer opOffset(gtx, image.Pt(x0+3*(s+gap), y0)).Pop()
			rrect(gtx, image.Rect(0, 0, s, s), gtx.Dp(6), v.tok("dt_color-base-sec"))
			g := gtx
			g.Constraints = layout.Exact(image.Pt(s, s))
			layout.Center.Layout(g, func(gtx C) D { return v.icon(gtx, "user-add", 20, fg) })
		}()
		func() {
			defer opOffset(gtx, image.Pt(0, hh)).Pop()
			m.call.Add(gtx.Ops)
		}()
	}()
	return D{Size: image.Pt(w, h)}
}

// channelSuggestions is getChannelHuddleSuggestionsForNUX: the first
// three conversations you're in (DMs, group DMs, channels of this
// workspace that aren't archived) that support huddles. Slack walks its
// channel store in insertion order; Clack's store has none, so it walks
// the snapshot's (by name).
func (v *Huddles) channelSuggestions() []core.Conv {
	var out []core.Conv
	hs, _ := v.h.(interface{ SupportsHuddles(ch string) bool })
	for _, c := range v.h.Core().Snapshot().Convs {
		if len(out) >= 3 {
			break
		}
		in := c.Kind == "im" || c.Kind == "mpim" || (!c.Preview && !c.IsArchived && (c.Kind == "channel" || c.Kind == "private"))
		if !in {
			continue
		}
		if hs != nil && !hs.SupportsHuddles(c.ID) {
			continue
		}
		out = append(out, c)
	}
	return out
}

func (v *Huddles) layoutChannelSuggestions(gtx C) D {
	w := gtx.Constraints.Max.X
	x, cw := hdColumn(gtx, w)
	chans := v.channelSuggestions()
	if len(chans) == 0 {
		return D{Size: image.Pt(w, 0)}
	}
	top := gtx.Dp(20)
	defer opOffset(gtx, image.Pt(x, top)).Pop()
	g := gtx
	g.Constraints = layout.Constraints{Max: image.Pt(cw, gtx.Constraints.Max.Y)}
	d := v.sectionTitle(g, nsHuddles.T("Channels"), nsHuddles.T("Meet with a whole team, or just let people drop in and out"))
	y := d.Size.Y + gtx.Dp(10)
	for i, c := range chans {
		func() {
			defer opOffset(gtx, image.Pt(0, y)).Pop()
			g := gtx
			g.Constraints = layout.Exact(image.Pt(cw, gtx.Dp(56)))
			v.channelSuggestion(g, c, i == 0, i == len(chans)-1)
		}()
		y += gtx.Dp(56)
	}
	return D{Size: image.Pt(w, top+y+gtx.Dp(10)+gtx.Dp(20))}
}

// channelSuggestion is HuddlesPageChannelSuggestion: 56px, padding 10px
// 12px, sk_primary_background between #0000001a sides and an inset line
// below (the first row's top line and corners 12, the last's), the 36px
// tile (foreground_soft .06, radius 8) with the channel icon, the name
// (15px bold, 8px after), "Start Huddle" on hover at the right.
func (v *Huddles) channelSuggestion(gtx C, c core.Conv, first, last bool) D {
	sz := gtx.Constraints.Max
	cl := v.clk("chan:" + c.ID)
	if cl.Clicked(gtx) && c.ID != "" {
		v.openStart("huddles_page_nux_channel_suggestion", c.ID, "")
	}
	hov := cl.Hovered()
	return cl.Layout(gtx, func(gtx C) D {
		pointerCursor(gtx)
		bg := v.tok("dt_color-base-pry")
		line := flatten(argb(0x1a000000), bg)
		rad := gtx.Dp(12)
		var nw, se int
		if first {
			nw = rad
		}
		if last {
			se = rad
		}
		r := image.Rectangle{Max: sz}
		px1 := max(1, gtx.Dp(1))
		func() {
			defer clipRR(gtx, r, nw, nw, se, se).Pop()
			fill(gtx, r, line)
			in := image.Rect(px1, 0, sz.X-px1, sz.Y-px1)
			if first {
				in.Min.Y = px1
			}
			defer clipRR(gtx, in, max(0, nw-px1), max(0, nw-px1), max(0, se-px1), max(0, se-px1)).Pop()
			fill(gtx, in, bg)
		}()
		g := gtx
		g.Constraints = layout.Exact(sz)
		layout.Inset{Left: 12, Right: 12}.Layout(g, func(gtx C) D {
			return row(gtx, layout.Middle,
				layout.Rigid(func(gtx C) D {
					s := gtx.Dp(36)
					rrect(gtx, image.Rect(0, 0, s, s), gtx.Dp(8), flatten(argb(0x0f1d1c1d), bg))
					gg := gtx
					gg.Constraints = layout.Exact(image.Pt(s, s))
					layout.Center.Layout(gg, func(gtx C) D { return v.icon(gtx, "channel", 20, v.tok("dt_color-content-pry")) })
					return D{Size: image.Pt(s, s)}
				}),
				hgap(8),
				layout.Flexed(1, func(gtx C) D {
					return v.label(gtx, 15, wBold, v.tok("dt_color-content-pry"), v.channelDisplayName(c))
				}),
				layout.Rigid(func(gtx C) D { return v.startDisplay(gtx, hov, bg) }),
			)
		})
		return D{Size: sz}
	})
}

// channelDisplayName is getChannelDisplayName (W+Ke SB): #name for a
// public channel, the person or people for a DM.
func (v *Huddles) channelDisplayName(c core.Conv) string {
	if c.Kind == "channel" {
		return "#" + c.Name
	}
	return v.convTitle(c.ID, nil)
}
