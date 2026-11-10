package ui

// The channel header's star and "Move channel": Slack 2026 turned the
// header star into ViewHeaderEntitySectionSelector (boot-shared, CDN
// build 4f8632cd9602d1fa): a section picker, not a toggle.
//
//   - Icon (JEcC $): the section's emoji when it has one; otherwise
//     "section" in a custom (standard) section, "star-filled" when
//     starred, "star" otherwise. Tooltip/aria: "Currently in Starred",
//     "Currently in <section>", or "Add to section".
//   - Click: ChannelSectionMoveToMenu, 280px, under the star: a "Move to…"
//     header, the Starred section (synthesized when the sidebar has none)
//     and every custom section as radios (checked: where the channel is),
//     a separator, then "Create your first section" (no custom sections
//     yet) or "Create new section" and, for a channel in a custom
//     section, "Remove from section". Choosing a section is
//     performBulkChannelMove: insert there, remove from where it was.
//   - The header ⋮ menu's "Move channel" opens the same choice; with no
//     custom section to offer, Slack goes straight to "Create a section"
//     (C0CFT52NEGM, 2026-11-08).
//   - None of this shows for a channel the viewer isn't in.

import (
	"context"
	"image"

	"gioui.org/layout"

	"github.com/some-du6e/clack/internal/core"
	"github.com/some-du6e/clack/internal/ui/popups"
	"github.com/some-du6e/clack/internal/ui/theme"
)

// chSectionState is where a conversation sits in the sidebar: the
// Starred section and/or the custom section that holds it.
type chSectionState struct {
	starred  bool
	stars    *sbSection // the Starred section (nil: the sidebar has none yet)
	custom   *sbSection // the custom (standard) section holding it
	sections []*sbSection
}

func secHas(s *sbSection, ch string) bool {
	for _, id := range s.All {
		if id == ch {
			return true
		}
	}
	for _, id := range s.Rows {
		if id == ch {
			return true
		}
	}
	return false
}

// channelSections reads the sidebar model for ch.
func (a *App) channelSections(ch string) chSectionState {
	m := a.model()
	var st chSectionState
	if c := m.conv(ch); c != nil {
		st.starred = c.Starred
	}
	for i := range m.Sections {
		s := &m.Sections[i]
		switch s.Type {
		case "stars":
			st.stars = s
			if secHas(s, ch) {
				st.starred = true
			}
			st.sections = append(st.sections, s)
		case "standard":
			if st.custom == nil && secHas(s, ch) {
				st.custom = s
			}
			st.sections = append(st.sections, s)
		}
	}
	return st
}

// headerStarIcon is Slack's JEcC $ choice (plus the section emoji).
func headerStarIcon(st chSectionState) (icon, emojiURL string) {
	cur := st.custom
	if st.starred {
		cur = st.stars
	}
	if cur != nil && cur.EmojiURL != "" {
		return "", cur.EmojiURL
	}
	switch {
	case st.custom != nil && !st.starred:
		// Slack's "section" glyph is the one its sidebar draws on custom
		// section headings; Clack draws those with sectionIcon's
		return sectionIcon("standard"), ""
	case st.starred:
		return "star-filled", ""
	}
	return "star", ""
}

// layoutHeaderStar draws the 28px star at pos (header coordinates) and
// opens the Move to… menu under it.
func (a *App) layoutHeaderStar(gtx layout.Context, ch string, conv *convInfo, pos image.Point) {
	c := &a.th.C
	st := a.channelSections(ch)
	if conv != nil && conv.Starred {
		st.starred = true
	}
	icon, emoji := headerStarIcon(st)
	org := a.starOrigin[ch]
	if org == nil {
		if a.starOrigin == nil {
			a.starOrigin = map[string]*originTracker{}
		}
		org = &originTracker{}
		a.starOrigin[ch] = org
	}
	box := gtx.Dp(28)
	at(gtx, pos, image.Pt(box, box), func(gtx layout.Context) layout.Dimensions {
		var d layout.Dimensions
		if emoji != "" {
			d = clickArea(gtx, a.clicks.get("hdr.star"), func(gtx layout.Context, s image.Point, hov bool) {
				if hov {
					theme.RRect(gtx, image.Rectangle{Max: s}, gtx.Dp(8), c.BgHover)
				}
			}, func(gtx layout.Context) layout.Dimensions {
				es := gtx.Dp(16)
				at(gtx, image.Pt((box-es)/2, (box-es)/2), image.Pt(es, es), func(gtx layout.Context) layout.Dimensions {
					if img, ok := a.images.Get(emoji, es*2); ok {
						return theme.Image(gtx, img, image.Pt(es, es), 0)
					}
					return layout.Dimensions{Size: image.Pt(es, es)}
				})
				return layout.Dimensions{Size: image.Pt(box, box)}
			})
		} else {
			col := c.Fg
			if icon == "star-filled" && st.starred {
				col = c.Warning
			}
			d = iconButton(gtx, a.clicks.get("hdr.star"), icon, 28, 20, col, c.BgHover, 8)
		}
		org.track(gtx, a)
		return d
	})
	if clicked(a.clicks.get("hdr.star")) {
		o := org.get(image.Pt(a.winSize.X-gtx.Constraints.Max.X-gtx.Dp(theme.PanelInset)+pos.X, gtx.Dp(theme.TopNavH)+pos.Y))
		a.openMoveToMenu(ch, image.Rectangle{Min: o, Max: o.Add(image.Pt(box, box))}, false)
	}
}

// moveToItems is ChannelSectionMoveToMenu for ch.
func (a *App) moveToItems(ch string, st chSectionState) []MenuItem {
	items := []MenuItem{{Header: true, Label: "Move to…"}}
	if st.stars == nil {
		items = append(items, MenuItem{Key: "channel_section_select_menu_stars", Label: "Starred", Icon: "star", Radio: true,
			Checked: st.starred, Do: func() { a.moveChannelTo(ch, nil, st) }})
	}
	standard := false
	for _, s := range st.sections {
		s := s
		icon := ""
		cur := st.custom == s && !st.starred
		if s.Type == "stars" {
			icon = "star"
			cur = st.starred
		} else {
			standard = true
		}
		items = append(items, MenuItem{Key: "channel_section_select_menu_" + s.ID, Label: s.Name, Icon: icon, Radio: true, Checked: cur,
			Do: func() {
				if !cur {
					a.moveChannelTo(ch, s, st)
				}
			}})
	}
	items = append(items, MenuItem{Divider: true})
	if !standard {
		return append(items, MenuItem{Key: "channel_section_select_menu_create_new_section", Label: "Create your first section",
			Icon: "plus", Do: func() { a.openCreateSectionWith(ch) }})
	}
	items = append(items, MenuItem{Key: "channel_section_select_menu_new_section", Label: "Create new section",
		Icon: "plus", Do: func() { a.openCreateSectionWith(ch) }})
	if st.custom != nil && !st.starred {
		id := st.custom.ID
		items = append(items, MenuItem{Key: "channel_section_select_menu_remove_from_section", Label: "Remove from section",
			Do: func() {
				a.sectionRun("Couldn’t move the channel", func(ctx context.Context, c *core.App) error {
					return c.MoveToSection(ctx, id, nil, []string{ch})
				}, nil)
			}})
	}
	return items
}

// openMoveToMenu shows the Move to… menu under anchor (window
// coordinates). fromKebab: the header ⋮ menu's "Move channel", which goes
// straight to "Create a section" when there is no custom section yet.
func (a *App) openMoveToMenu(ch string, anchor image.Rectangle, fromKebab bool) {
	st := a.channelSections(ch)
	if fromKebab {
		standard := false
		for _, s := range st.sections {
			standard = standard || s.Type == "standard"
		}
		if !standard {
			a.openCreateSectionWith(ch)
			return
		}
	}
	// Slack: the 280px menu's left edge 66px left of the star, 8px below
	// it (x 350 for the star at 416, y 87; C0CFT52NEGM at 1440×900).
	r := anchor
	if !fromKebab {
		// (live check against Slack: the menu sat 10px high otherwise)
		r.Min.X -= a.px(66)
		r = r.Add(image.Pt(0, a.px(10)))
	}
	a.menuAt(r, 0, 280, a.moveToItems(ch, st))
}

// moveChannelTo is performBulkChannelMove: into to (nil: the synthetic
// Starred), out of the section ch was in.
func (a *App) moveChannelTo(ch string, to *sbSection, st chSectionState) {
	toStars := to == nil || to.Type == "stars"
	toID := ""
	if to != nil {
		toID = to.ID
	}
	starsID := ""
	if st.stars != nil {
		starsID = st.stars.ID
	}
	customID := ""
	if st.custom != nil {
		customID = st.custom.ID
	}
	wasStarred := st.starred
	a.sectionRun("Couldn’t move the channel", func(ctx context.Context, c *core.App) error {
		if toStars {
			if toID == "" {
				if err := c.Star(ctx, ch, true); err != nil {
					return err
				}
			} else if err := c.MoveToSection(ctx, toID, []string{ch}, nil); err != nil {
				return err
			}
			if customID != "" {
				return c.MoveToSection(ctx, customID, nil, []string{ch})
			}
			return nil
		}
		// into a custom section (core drops it from the other custom ones)
		if err := c.MoveToSection(ctx, toID, []string{ch}, nil); err != nil {
			return err
		}
		if wasStarred {
			if starsID != "" {
				return c.MoveToSection(ctx, starsID, nil, []string{ch})
			}
			return c.Star(ctx, ch, false)
		}
		return nil
	}, nil)
}

// openCreateSectionWith is openCreateChannelSectionModal({channelIds:
// [ch]}): "Create a section" with ch already picked on step 2.
func (a *App) openCreateSectionWith(ch string) {
	if a.Core() == nil {
		a.Toast("Sign in first")
		return
	}
	o := popups.CreateSectionOpts{Candidates: a.sidebarConvs(), Preselect: []string{ch}}
	switch s := a.Core().Prefs().ChannelSort; s {
	case "recent", "priority":
		o.DefaultSort = s
	default:
		o.DefaultSort = "default"
	}
	a.ShowOverlay(popups.NewCreateSectionModal(a, o))
}
