package ui

import (
	"context"
	"image"
	"strings"
	"time"

	"gioui.org/layout"
	"gioui.org/unit"

	"github.com/some-du6e/clack/internal/i18n"
	"github.com/some-du6e/clack/internal/ui/popups"
	"github.com/some-du6e/clack/internal/ui/theme"
)

// The Starred section's empty states (web client build 2c0d02ca130f):
//
//   - The banner (boot-render FmLP EmptyStarredSectionRow): while core's
//     showEmptyStarredSectionBanner holds (m1KW l, core sidebar_rules.go
//     starredBanner) and the text filter is empty,
//     use_home_channel_sidebar_visible_rows puts a 44px row under an open
//     Starred heading with no rows, outside organize mode: a 4px spacer
//     (.spacer__euZCI), then .banner__kYRUH (40px, padding 0 12px 0 24px,
//     align-items start) with "Drag and drop important stuff here" in
//     .label__H84QF (13px regular, two lines at most, ellipsized), in the
//     sidebar's colour (.p-channel_sidebar: theme-content-inv-sec). Its
//     drop-target looks (isHovered) follow a drag, and Clack's sidebar has
//     no drag and drop; the Truncate tooltip only shows on a cut label,
//     and the label fits two lines at every sidebar width.
//   - The "New" badge (boot-render MGQu, iNkd SectionMenuNewInternal):
//     with the banner's condition and the megaphone's generic_new_badge
//     listing starred_section_new_badge, the Starred heading carries
//     EducationTag's micro "New" (7ux/, style education) in .callout__MHCtX
//     (margin-left 8px, padding-top 1px, max-width 70px) after its label,
//     unless the sidebar is under 220px, and 0Eub's getChannelSectionPref
//     reads the section as collapsed ("c" "1"). Clicking the heading
//     toggles it (onCollapseToggleClick), then onBadgeClick: the hold ends
//     at once (MGQu's local state) and 500 ms later recordAndClearNewBadge
//     clears the badge (core DismissNewBadge).

// sbStarredBannerKey is EmptyStarredSectionRow's id and key.
const sbStarredBannerKey = "emptyStarredSectionBanner"

// starredNewBadge is MGQu's badge name (l).
const starredNewBadge = "starred_section_new_badge"

// starredNewLabel is EducationTag's "New".
func starredNewLabel() string { return i18n.T("education", "New") }

// sbStarredBadge is MGQu Q for the model: the Starred section whose
// heading carries the "New" badge, and the one the badge holds collapsed
// (until its heading is clicked); "" for none.
func (a *App) sbStarredBadge(m *model) (badge, forced string) {
	c := a.Core()
	if c == nil {
		return "", ""
	}
	for i := range m.Sections {
		sec := &m.Sections[i]
		if sec.Type != "stars" || !sec.StarredBanner {
			continue
		}
		if !c.MegaphoneNewBadge(starredNewBadge) {
			return "", ""
		}
		if a.sidebar.starClicked {
			return sec.ID, ""
		}
		return sec.ID, sec.ID
	}
	return "", ""
}

// sbStarredTagWidth is the room the "New" callout takes after r's label
// (its 8px margin included), 0 when r doesn't carry it (eC && !ei).
func (a *App) sbStarredTagWidth(gtx layout.Context, r *sbRow) int {
	if r.kind != sbHeading || r.key == "" || r.key != a.sidebar.starBadgeID || a.sidebar.width < 220 {
		return 0
	}
	return gtx.Dp(8) + min(popups.MicroTagWidth(gtx, a.th, starredNewLabel()), gtx.Dp(70))
}

// layoutStarredTag draws the "New" callout after a heading label ending
// at x, in a row rowH tall: the micro Tag centred under the callout's 1px
// top padding.
func (a *App) layoutStarredTag(gtx layout.Context, x, rowH int) {
	t := &popups.MenuTag{Label: starredNewLabel(), Style: "education"}
	w := min(popups.MicroTagWidth(gtx, a.th, t.Label), gtx.Dp(70))
	at(gtx, image.Pt(x+gtx.Dp(8), gtx.Dp(1)), image.Pt(w, rowH-gtx.Dp(1)), func(gtx layout.Context) layout.Dimensions {
		return popups.DrawMicroTag(gtx, a.th, t)
	})
}

// sbStarredHeadingClicked is handleSectionHeaderClick's onBadgeClick, after
// the toggle: on the heading carrying the badge the hold ends now and the
// badge is cleared 500 ms later, as MGQu does on every such click.
func (a *App) sbStarredHeadingClicked(r *sbRow) {
	s := &a.sidebar
	if r.key == "" || r.key != s.starBadgeID {
		return
	}
	s.starClicked = true
	s.builtFor = -1
	if c := a.Core(); c != nil {
		time.AfterFunc(500*time.Millisecond, func() { _ = c.DismissNewBadge(context.Background(), starredNewBadge) })
	}
}

// sbBannerLineH is the label's line box: 13px at the body's line-height
// 1.46668.
const sbBannerLineH = 13 * 1.46668

// layoutStarredBanner draws EmptyStarredSectionRow, w wide; pad is the
// list's inset (the rows' 8px).
func (a *App) layoutStarredBanner(gtx layout.Context, r *sbRow, w, pad int) layout.Dimensions {
	label := r.label
	if r.tr != "" {
		label = r.tr.T(label)
	}
	x := pad + gtx.Dp(24)
	tw := max(0, w-pad*2-gtx.Dp(24)-gtx.Dp(12))
	lh := gtx.Dp(sbBannerLineH)
	first, rest := a.sbBannerLines(gtx, label, tw)
	for i, line := range []string{first, rest} {
		if line == "" {
			continue
		}
		y := gtx.Dp(unit.Dp(4 + float32(i)*sbBannerLineH))
		at(gtx, image.Pt(x, y), image.Pt(tw, lh), func(gtx layout.Context) layout.Dimensions {
			return vcenter(gtx, lh, func(gtx layout.Context) layout.Dimensions {
				return a.th.Label(gtx, 13, theme.Regular, a.sbPal.SidebarText, line)
			})
		})
	}
	return layout.Dimensions{Size: image.Pt(w, gtx.Dp(44))}
}

// sbBannerLines wraps the label the way Chrome does there (white-space
// normal, no word breaking): the words that fit the first line, and the
// rest for the second, which the two-line clamp ellipsizes.
func (a *App) sbBannerLines(gtx layout.Context, label string, w int) (first, rest string) {
	words := strings.Fields(label)
	for i, wd := range words {
		try := wd
		if first != "" {
			try = first + " " + wd
		}
		if first != "" && a.th.Measure(gtx, 13, theme.Regular, try) > w {
			return first, strings.Join(words[i:], " ")
		}
		first = try
	}
	return first, ""
}
