package canvasview

// UserProfileUnfurl (client-boot-shared texo), the card Slack draws in a
// canvas for a member (a SLACK_USER card, a /team/U… link):
// MemberProfileBaseEntity, the local time and primary location, the role
// banner. Its parts are popups' port of the same component (search's
// person card). Build 889ac449a32d; the notes are in
// /root/clack-ops/scratch/link-url-box-3/cards-spec.md §4.

import (
	"context"
	"image"
	"image/color"
	"strings"
	"time"

	"gioui.org/layout"
	"gioui.org/op"
	"gioui.org/unit"
	"gioui.org/widget"

	"github.com/some-du6e/clack/internal/core"
	"github.com/some-du6e/clack/internal/ui/icons"
	"github.com/some-du6e/clack/internal/ui/popups"
	"github.com/some-du6e/clack/internal/ui/theme"
)

// memberCardData is what UserProfileUnfurl reads: the member (users.info
// when the store misses it) and the profile's sections
// (users.profile.getSections, for the primary location).
type memberCardData struct {
	u                 core.User
	known, asked      bool
	nameBtn, avatarBt widget.Clickable
}

// profileSecs is a member's sections as the view fetched them: done once
// users.profile.getSections answered (Slack's skeleton waits for it).
type profileSecs struct {
	done   bool
	locale string // "City, State, Country"
}

// popHost is canvasview's host as popups' (the parts the profile pieces
// draw with; the rest do nothing in a canvas card).
type popHost struct{ Host }

func (p popHost) OpenThread(ch, ts string)                  { p.Navigate("c/" + ch + "/t/" + ts) }
func (p popHost) CloseSecondary()                           {}
func (p popHost) ShowOverlay(popups.Overlay)                {}
func (p popHost) OpenEmojiPicker(image.Point, func(string)) {}
func (p popHost) OpenMessageMenu(image.Point, core.Message) {}

// readMember reads the route's member; it reports whether to fetch it.
func (c *linkCard) readMember(a *core.App) bool {
	d := &c.mb
	if c.rt.kind != "user" || c.rt.memberID == "" {
		return false
	}
	d.u, d.known = a.User(c.rt.memberID)
	return !d.known && !d.asked
}

// memberSections is the view's sections of member id, fetching them once.
func (v *View) memberSections(id string) *profileSecs {
	if v.profiles == nil {
		v.profiles = map[string]*profileSecs{}
	}
	if p := v.profiles[id]; p != nil {
		return p
	}
	p := &profileSecs{}
	v.profiles[id] = p
	a := v.h.Core()
	if a == nil {
		return p
	}
	go func() {
		ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
		defer cancel()
		ps, err := a.ProfileSections(ctx, id)
		if err != nil { // Slack's data stays undefined: the skeleton stays
			return
		}
		loc := profileLocale(ps)
		v.post(func() {
			p.done, p.locale = true, loc
			v.h.Invalidate()
		})
	}()
	return p
}

// profileLocale is PrimaryLocation's label: the HEADER section's
// locality, region and country text elements, joined by ", ".
func profileLocale(ps core.ProfileSections) string {
	for _, s := range ps.Sections {
		if s.Type != "HEADER" {
			continue
		}
		var parts []string
		for _, key := range []string{"locality", "region", "country"} {
			for _, e := range s.Elements {
				if e.ElementKey == key && e.Typename == "ProfileTextElement" && strings.TrimSpace(e.Text) != "" {
					parts = append(parts, strings.TrimSpace(e.Text))
					break
				}
			}
		}
		return strings.Join(parts, ", ")
	}
	return ""
}

// decideMember is UserProfileUnfurl's rule: the skeleton until the
// sections answer (at once for a deleted member), then the card.
func (v *View) decideMember(c *linkCard) cardKind {
	d := &c.mb
	if !d.known {
		return cardMemberLoading // Slack draws nothing until users.info answers
	}
	if d.u.Deleted || v.memberSections(d.u.ID).done {
		return cardMember
	}
	return cardMemberLoading
}

// layoutMember draws UserProfileUnfurl w wide and returns its height: the
// base entity padded 12 (the 72px avatar, 12 from the name line, the
// title in 15px and the pronouns in 13px), the secondary block under a
// 1px rule (the local time after the 20px clock, the primary location
// after the 20px building, 4px apart), then the role banner.
func (v *View) layoutMember(gtx layout.Context, c *linkCard, w int) int {
	d := &c.mb
	u := d.u
	th := v.h.Theme()
	ph := popHost{v.h}
	ink := cardInks(th.Dark)
	fgMax70, fgLow13 := color.NRGBA{29, 28, 29, 179}, color.NRGBA{29, 28, 29, 33}
	if th.Dark {
		fgMax70, fgLow13 = color.NRGBA{232, 232, 232, 179}, color.NRGBA{232, 232, 232, 33}
	}
	for d.avatarBt.Clicked(gtx) || d.nameBtn.Clicked(gtx) { // the avatar and the name open the profile
		v.h.OpenProfile(image.Pt(0, 0), u.ID)
	}
	pad, av := gtx.Dp(12), gtx.Dp(72)
	func() {
		defer op.Offset(image.Pt(pad, pad)).Push(gtx.Ops).Pop()
		g := gtx
		g.Constraints = layout.Exact(image.Pt(av, av))
		d.avatarBt.Layout(g, func(gtx layout.Context) layout.Dimensions {
			return popups.ProfileUnfurlAvatar(gtx, ph, u, 72)
		})
	}()
	colX := pad + av + gtx.Dp(12)
	colW := max(1, w-colX-pad)
	title, pron := strings.TrimSpace(u.Title), strings.TrimSpace(u.Pronouns)
	textH := float32(22)
	if title != "" {
		textH += 22 + 4
	}
	if pron != "" {
		textH += 19
	}
	y := pad + gtx.Dp(unit.Dp((72-(textH-8))/2-4)) // centred on the avatar, as the base entity's -4px margins leave it
	at := func(x, y int, f func(gtx layout.Context) layout.Dimensions) {
		defer op.Offset(image.Pt(x, y)).Push(gtx.Ops).Pop()
		f(gtx)
	}
	at(colX, y, func(gtx layout.Context) layout.Dimensions {
		return popups.ProfileUnfurlName(gtx, ph, th, &d.nameBtn, u, colW)
	})
	lineH := gtx.Dp(22)
	y += lineH
	if title != "" {
		at(colX, y, func(gtx layout.Context) layout.Dimensions {
			gtx.Constraints = layout.Constraints{Max: image.Pt(colW, lineH)}
			return th.Layout(gtx, theme.TextStyle{Size: 15, Weight: theme.Regular, Color: ink.primary, LineHeight: 22, MaxLines: 1}, title)
		})
		y += lineH + gtx.Dp(4)
	}
	if pron != "" {
		at(colX, y, func(gtx layout.Context) layout.Dimensions {
			gtx.Constraints = layout.Constraints{Max: image.Pt(colW, gtx.Dp(19))}
			return th.Layout(gtx, theme.TextStyle{Size: 13, Weight: theme.Regular, Color: fgMax70, LineHeight: 19, MaxLines: 1}, pron)
		})
	}
	by := pad + av + pad
	local := ""
	if popups.ProfileUnfurlHasClock(u) && !u.IsBot {
		local = popups.ProfileUnfurlLocalTime(ph, u, time.Now())
	}
	locale := ""
	if p := v.profiles[u.ID]; p != nil {
		locale = p.locale
	}
	if local != "" || locale != "" {
		rule := max(1, gtx.Dp(1))
		theme.Fill(gtx, image.Rect(0, by, w, by+rule), fgLow13)
		ly := by + rule + pad
		row := func(glyph, label string) {
			at(pad, ly+(lineH-gtx.Dp(20))/2, func(gtx layout.Context) layout.Dimensions {
				gtx.Constraints = layout.Exact(image.Pt(gtx.Dp(20), gtx.Dp(20)))
				return icons.GetSlack(glyph)(gtx, unit.Dp(20), ink.primary)
			})
			tx := pad + gtx.Dp(20+8)
			at(tx, ly, func(gtx layout.Context) layout.Dimensions {
				gtx.Constraints = layout.Constraints{Max: image.Pt(max(1, w-tx-pad), lineH)}
				return th.Layout(gtx, theme.TextStyle{Size: 15, Weight: theme.Regular, Color: ink.primary, LineHeight: 22, MaxLines: 1}, label)
			})
			ly += lineH
		}
		if local != "" {
			row("clock", local)
		}
		if locale != "" {
			if local != "" {
				ly += gtx.Dp(4) // __locale's margin-top
			}
			row("buildings", locale) // PrimaryLocation's "building" glyph
		}
		by = ly + pad
	}
	if label, deact := popups.ProfileUnfurlBanner(ph, u); label != "" {
		func() {
			defer op.Offset(image.Pt(0, by)).Push(gtx.Ops).Pop()
			by += popups.LayoutProfileUnfurlBanner(gtx, th, label, deact, w, v.styles.card.Palette.Panel).Size.Y
		}()
	}
	return by
}

// layoutMemberSkeleton is LoadingUserProfileUnfurl: a 72px MissingIcon
// and two bars (120 and 80) beside it, padded 12, then under a 1px rule two
// 30% bars, shimmering.
func (v *View) layoutMemberSkeleton(gtx layout.Context, w int, now time.Time) int {
	col := v.skeletonInk(gtx, now)
	pad, av, lh := gtx.Dp(12), gtx.Dp(72), gtx.Dp(8)
	theme.RRect(gtx, image.Rect(pad, pad, pad+av, pad+av), gtx.Dp(12), col)
	tx := pad + av + gtx.Dp(12)
	gap := (av - 2*lh) / 3 // justify-content: space-evenly
	theme.RRect(gtx, image.Rect(tx, pad+gap, tx+gtx.Dp(120), pad+gap+lh), lh/2, col)
	theme.RRect(gtx, image.Rect(tx, pad+2*gap+lh, tx+gtx.Dp(80), pad+2*gap+2*lh), lh/2, col)
	y := pad + av + pad
	rule := color.NRGBA{29, 28, 29, 33}
	if v.h.Theme().Dark {
		rule = color.NRGBA{232, 232, 232, 33}
	}
	theme.Fill(gtx, image.Rect(0, y, w, y+max(1, gtx.Dp(1))), rule)
	y += max(1, gtx.Dp(1))
	bw := (w - 2*pad) * 30 / 100
	for i := 0; i < 2; i++ {
		y += gtx.Dp(8)
		theme.RRect(gtx, image.Rect(pad, y, pad+bw, y+lh), lh/2, col)
		y += lh + gtx.Dp(8)
	}
	return y + pad
}
