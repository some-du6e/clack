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
//     section, "Remove from <section>". Choosing a section is
//     performBulkChannelMove: insert there, remove from where it was.
//   - The header ⋮ menu's "Move channel" opens the same choice; with no
//     custom section to offer, Slack goes straight to "Create a section"
//     (C0CFT52NEGM, 2026-11-08).
//   - None of this shows for a channel the viewer isn't in.

import (
	"image"

	"gioui.org/layout"

	"github.com/some-du6e/clack/internal/core"
	"github.com/some-du6e/clack/internal/i18n"
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
					if img, ok := a.images.Get(emoji, es); ok {
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
		o := org.get(image.Pt(a.paneRight()-gtx.Constraints.Max.X+pos.X, gtx.Dp(theme.TopNavH)+pos.Y))
		a.openMoveToMenu(ch, image.Rectangle{Min: o, Max: o.Add(image.Pt(box, box))}, false)
	}
}

// moveToItems is ChannelSectionMoveToMenu for ch (the header's star,
// the ⋮ menu's Move channel).
func (a *App) moveToItems(ch string, _ chSectionState) []MenuItem {
	return a.moveToMenuItems([]string{ch}, moveMenuOpts{})
}

// moveMenuOpts are what ChannelSectionMoveToMenu's callers set.
type moveMenuOpts struct {
	// edit is showEditButton (a sidebar row's menu: onEnterEditModeClick
	// was passed): "Edit all sections" ends the menu and enters organize
	// mode at section, the row's.
	edit    bool
	section string
}

// moveToMenuItems is ChannelSectionMoveToMenu (boot-shared EaYn, build
// 4d0c78085232) for the conversations ids (a row's one, or organize
// mode's picks: getChannels):
//
//   - "Move to…", then Starred (a stand-in first when the sidebar has
//     none) and the custom sections in sidebar order as radios: checked
//     when any of ids is there (Starred: starred), indeterminate when not
//     all are; the section's emoji, else its type's glyph (star /
//     star-filled when checked, section for a custom one). Picking a
//     section that has all of them takes them out of it
//     (performBulkChannelMove remove); any other moves them there
//     (performMoveManyChannelsToSection).
//   - a separator, then "Create your first section" without a custom
//     section, else "Create new section" (Create a section with ids
//     picked), "Remove from {section}" / "Remove from all sections" while
//     they are in a custom section or Starred, and for a sidebar row a
//     separator and "Edit all sections".
func (a *App) moveToMenuItems(ids []string, o moveMenuOpts) []MenuItem {
	ns := i18n.NS("menu")
	m := a.model()
	var list []*sbSection
	var stars *sbSection
	custom := false
	for i := range m.Sections {
		s := &m.Sections[i]
		switch s.Type {
		case "stars":
			stars = s
			list = append(list, s)
		case "standard":
			custom = true
			list = append(list, s)
		}
	}
	starred := func(id string) bool { return a.channelSections(id).starred }
	in := func(s *sbSection, id string) bool {
		if s == nil || s.Type == "stars" {
			return starred(id) || s != nil && secHas(s, id)
		}
		return secHas(s, id)
	}
	item := func(s *sbSection) MenuItem {
		has, all := false, len(ids) > 0
		for _, id := range ids {
			h := in(s, id)
			has, all = has || h, all && h
		}
		label, key, icon, url := ns.T("Starred"), "section-stars", "star", ""
		if s != nil {
			label, key = sectionLabel(s), "section-"+s.ID
			icon = sectionIcon("standard") // Slack's "section" glyph
			if s.Type == "stars" {
				icon = "star"
			}
			if s.EmojiURL != "" {
				icon, url = "", s.EmojiURL
			}
		}
		if icon == "star" && has {
			icon = "star-filled"
		}
		to := s
		return MenuItem{Key: key, Label: label, Icon: icon, IconURL: url, Radio: true, Checked: has, Indeterminate: has && !all,
			Do: func() {
				if !all {
					a.moveManyTo(ids, to)
					return
				}
				if to != nil { // all of them there: out of it (…-deselect)
					a.moveMany(ids, nil, []core.SectionItems{{Section: to.ID, Channels: ids}}, nil)
				}
			}}
	}
	items := []MenuItem{{Header: true, Label: ns.T("Move to…")}}
	if stars == nil {
		items = append(items, item(nil))
	}
	for _, s := range list {
		items = append(items, item(s))
	}
	items = append(items, MenuItem{Divider: true})
	if !custom {
		return append(items, MenuItem{Key: "channel_section_select_menu_create_new_section", Label: ns.T("Create your first section"),
			Icon: "plus", Do: func() { a.openCreateSectionWithIDs(ids) }})
	}
	items = append(items, MenuItem{Key: "channel_section_select_menu_new_section", Label: ns.T("Create new section"),
		Icon: "plus", Do: func() { a.openCreateSectionWithIDs(ids) }})
	if rm, ok := a.removeFromSectionItem(ids, stars); ok {
		items = append(items, rm)
	}
	if o.edit {
		sec := o.section
		items = append(items, MenuItem{Divider: true},
			MenuItem{Key: "edit_mode_enter", Label: ns.T("Edit all sections"), Do: func() { a.sbEnterEditMode(sec) }})
	}
	return items
}

// removeFromSectionItem is renderRemoveFromSection: the custom sections
// and Starred holding ids, "Remove from {section}" for one, "Remove from
// all sections" for several.
func (a *App) removeFromSectionItem(ids []string, stars *sbSection) (MenuItem, bool) {
	m := a.model()
	var secs []*sbSection
	per := map[string][]string{}
	add := func(s *sbSection, id string) {
		if _, ok := per[s.ID]; !ok {
			secs = append(secs, s)
		}
		per[s.ID] = append(per[s.ID], id)
	}
	for _, id := range ids {
		if a.channelSections(id).starred {
			if stars != nil {
				add(stars, id)
			}
			continue
		}
		for i := range m.Sections {
			if s := &m.Sections[i]; s.Type == "standard" && secHas(s, id) {
				add(s, id)
			}
		}
	}
	if len(secs) == 0 {
		return MenuItem{}, false
	}
	ns := i18n.NS("menu")
	label := ns.T("Remove from all sections")
	if len(secs) == 1 {
		label = ns.T("Remove from {section}", "section", sectionLabel(secs[0]))
	}
	var rem []core.SectionItems
	for _, s := range secs {
		rem = append(rem, core.SectionItems{Section: s.ID, Channels: per[s.ID]})
	}
	return MenuItem{Key: "channel_section_select_menu_remove_from_section", Label: label, Do: func() {
		a.moveMany(ids, nil, rem, nil)
	}}, true
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

// moveChannelTo is ChannelSectionMoveToMenu's pick for one conversation:
// performMoveManyChannelsToSection into to (nil: the synthetic Starred).
func (a *App) moveChannelTo(ch string, to *sbSection, _ chSectionState) {
	a.moveManyTo([]string{ch}, to)
}

// openCreateSectionWith is openCreateChannelSectionModal({channelIds:
// [ch]}): "Create a section" with ch already picked on step 2.
func (a *App) openCreateSectionWith(ch string) { a.openCreateSectionWithIDs([]string{ch}) }

// openCreateSectionWithIDs is openCreateChannelSectionModal({channelIds}).
func (a *App) openCreateSectionWithIDs(ids []string) {
	if a.Core() == nil {
		a.Toast("Sign in first")
		return
	}
	o := popups.CreateSectionOpts{Candidates: a.sidebarConvs(), Preselect: append([]string(nil), ids...), Moved: a.bulkMoveToast}
	switch s := a.Core().Prefs().ChannelSort; s {
	case "recent", "priority":
		o.DefaultSort = s
	default:
		o.DefaultSort = "default"
	}
	a.ShowOverlay(popups.NewCreateSectionModal(a, o))
}
