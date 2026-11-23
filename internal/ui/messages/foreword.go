package messages

// The DM foreword, as Slack's BaseImForeword draws it (boot-shared VHud,
// build 9f450b8ad3db; BaseForeword picks it for every DM but Slackbot's,
// which is SlackbotForeword, the Slack Security bot's and a Salesforce
// app's): div.p-message_pane__foreword (margin 48px 20px 16px) holding
//
//   - IMForewordMemberEntity in .p-message_pane__foreword__avatars (16px
//     under it). BaseEntity (client-boot xgHk), medium, with an avatar: a
//     row centred on the 102px avatar button (UnstyledButton; the avatar
//     at zvVA xxx_large, radius 9 from .p-new_im_foreword-entity__avatar,
//     no hover card) and, 12px on, the text column, -4px top and bottom:
//   - c-member__primary_content, a flex row, centred: the name button
//     (UnstyledButton, display flex) with MemberName (yT2g: strong,
//     .p-new_im_foreword-entity__name 18px/27px Black, one line; "(you)"
//     for yourself, "(deactivated)" and the entity dimmed, content-sec,
//     for a deleted member, the APP badge for an app or a bot that isn't
//     the Slack system user), 4px (.c-member_name), the presence
//     (ConnectedPresence Bh6F: BasePresence's 20px glyph, margin 4px 4px 0
//     0) unless the member is deactivated, then the name recording's
//     NamePronunciationAudioPlayer (BG0H) when the header section has one;
//   - the title (BaseEntityMetadata, .p-new_im_foreword-entity__subtext:
//     15px/22px, wrapping, the entity's colour), when there is one;
//   - PronounsAndNamePronunciation (oGjw Yw): the pronouns and the header
//     section's pronunciation text, " · " between.
//     The avatar and the name are the click targets (onMemberClick: the
//     member's profile in the secondary pane, showMemberProfile; a bot's
//     About tab, openAboutAppInIm), with the hand cursor and nothing else
//     on hover (.c-button-unstyled has no hover style).
//   - The description (.p-message_pane__foreword__description: 1.1rem
//     with line-height 1.5, content-pry, 16px under it): a member's
//     "This conversation is just between {name} and you. Check out their
//     profile to learn more about them." with their MemberSlug (nWQ1:
//     c-member_slug--link, padding 1px 2px, radius 3, the profile on a
//     click), yours, the Slack system user's, or a bot's "This is the very
//     beginning of your direct message history with {name}".
//   - A member's View Profile (an outline medium Kit button,
//     .p-message_pane__foreword__profile-button-container, 16px under
//     it). In your own DM, when you may change your photo
//     (showUploadPhotoCta: EDIT_PROFILE_PHOTO), Edit Profile (Edit profile
//     on Full name) and, without a photo of your own, Upload Profile Photo
//     8px on (margin_left_50); without it nothing. A bot's
//     "How does {name} work?" (AppHelpButton, a link button with the
//     lightbulb, in .p-message_pane__foreword__buttons, 8px down).
//
// Not drawn: the MemberSlug's tooltip (showTooltip), an assistant app's
// suggested prompts (ImForewordSuggestedPrompts), an agent DM's
// AgentDmForeword, the messaging max width column, a hidden member's
// "Name hidden", the dimmed entity's grayscale filter. The MemberSlug
// keeps to one line.

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
	"github.com/some-du6e/clack/internal/i18n"
	"github.com/some-du6e/clack/internal/ui/icons"
	"github.com/some-du6e/clack/internal/ui/popups"
	"github.com/some-du6e/clack/internal/ui/slackc/css"
	"github.com/some-du6e/clack/internal/ui/theme"
)

var (
	nsForeword     = i18n.NS("message_pane_foreword")
	nsInlineMember = i18n.NS("inline_member")
)

// fwType is im_foreword_mapStateToProps' type.
type fwType int

const (
	fwMember fwType = iota
	fwSelf          // the DM with yourself
	fwSystem        // the Slack system user (USLACK): system_notification
	fwBot           // isAppOrBot: is_app_user or is_bot
)

func (t fwType) String() string {
	return [...]string{"member", "self", "system_notification", "bot"}[t]
}

// imForeword is the DM foreword's widgets (UI goroutine).
type imForeword struct {
	avatar, name, slug, view, edit, photo, help widget.Clickable
	grantsAsked                                 bool // permissions/info asked for the self DM's buttons
	// rects are the buttons' boxes in the foreword (tests): avatar, name,
	// slug, view, edit, photo, help
	rects map[string]image.Rectangle
}

// fwTypeOf is the type of the DM with u.
func fwTypeOf(app *core.App, u core.User) fwType {
	switch {
	case u.ID == app.Self().ID:
		return fwSelf
	case u.ID == "USLACK":
		return fwSystem
	case u.IsBot || u.AppUser:
		return fwBot
	}
	return fwMember
}

// fwRun is a run of the description: plain, bold (<b>) or the MemberSlug.
type fwRun struct {
	text       string
	bold, slug bool
}

// imForewordLayout draws the foreword of the DM cv with its member, or
// reports false when BaseImForeword isn't the DM's foreword (Slackbot's
// DM, a member the store doesn't hold yet): the generic intro draws then.
func (s *listState) imForewordLayout(gtx layout.Context, h Host, c *colors, w int, cv core.Conv) (layout.Dimensions, bool) {
	app := h.Core()
	if app == nil || cv.User == "" || cv.User == "USLACKBOT" {
		return layout.Dimensions{}, false
	}
	u, ok := app.User(cv.User)
	if !ok {
		return layout.Dimensions{}, false
	}
	th := h.Theme()
	fw := &s.fw
	if fw.rects == nil {
		fw.rects = map[string]image.Rectangle{}
	}
	clear(fw.rects)
	typ := fwTypeOf(app, u)
	var ph popups.Host = h
	onMember := func() { // onMemberClick
		if typ == fwBot && popups.OpenAppAbout != nil {
			popups.OpenAppAbout(u.ID) // openAboutAppInIm
			return
		}
		h.OpenProfile(image.Point{}, u.ID) // showMemberProfile
	}
	if fw.avatar.Clicked(gtx) || fw.name.Clicked(gtx) || fw.view.Clicked(gtx) {
		onMember()
	}
	if fw.help.Clicked(gtx) && popups.OpenAppAbout != nil {
		popups.OpenAppAbout(u.ID)
	}
	if fw.slug.Clicked(gtx) {
		h.OpenProfile(image.Point{}, u.ID) // MemberSlug's toProfile (an app's App details)
	}
	if fw.edit.Clicked(gtx) {
		popups.OpenEditProfile(ph, popups.EditProfileOpts{Focus: "real_name"})
	}
	if fw.photo.Clicked(gtx) {
		popups.OpenUploadProfilePhoto(ph)
	}

	x0, y0 := dp(gtx, 20), dp(gtx, 48)
	av, gap := dp(gtx, 102), dp(gtx, 12)
	tx := x0 + av + gap
	tw := max(1, w-x0-dp(gtx, 20)-av-gap)
	dimmed := u.Deleted
	ink := c.Text
	if dimmed {
		ink = c.Text2 // .c-base_entity--dim: --dt_color-content-sec
	}

	// the name row's parts
	nameSt := textStyle{size: 18, weight: theme.Black, col: ink, lineH: 27, maxLines: 1}
	name := u.Name
	if name == "" {
		name = u.RealName
	}
	var marks []string // c-member_name__indicator, 4px before each
	if typ == fwSelf {
		marks = append(marks, nsInlineMember.T("(you)"))
	}
	if dimmed {
		marks = append(marks, nsInlineMember.T("(deactivated)"))
	}
	badge := ""
	if typ == fwBot {
		badge = "APP"
		switch {
		case u.Workflow:
			badge = "WORKFLOW"
		case u.Agentforce:
			badge = "AGENTFORCE"
		}
	}
	presence := !u.Deleted
	pron, rec, ctl := s.introHeader(h, u.ID)
	rowH := dp(gtx, 27)
	if presence {
		rowH = max(rowH, dp(gtx, 24)) // the 20px glyph and its 4px margin-top
	}
	if rec != nil {
		rowH = max(rowH, dp(gtx, 30))
	}

	// the title and PronounsAndNamePronunciation, wrapping in the column
	subSt := textStyle{size: 15, col: ink, lineH: 22}
	wrapH := func(txt string) int {
		if txt == "" {
			return 0
		}
		g := gtx
		g.Constraints = layout.Constraints{Max: image.Pt(tw, 1<<20)}
		m := op.Record(g.Ops)
		d := label(g, th, subSt, txt)
		m.Stop()
		return d.Size.Y
	}
	title := strings.TrimSpace(u.Title)
	var pp []string
	if p := strings.TrimSpace(u.Pronouns); p != "" {
		pp = append(pp, p)
	}
	if p := strings.TrimSpace(pron); p != "" {
		pp = append(pp, p)
	}
	meta := strings.Join(pp, " · ")
	titleH, metaH := wrapH(title), wrapH(meta)
	contentH := rowH + titleH + metaH
	outer := contentH - dp(gtx, 8)
	e := max(av, outer)
	colTop := y0 + (e-outer)/2 - dp(gtx, 4)
	avTop := y0 + (e-av)/2

	// the avatar button
	fw.rects["avatar"] = image.Rect(x0, avTop, x0+av, avTop+av)
	at(gtx, x0, avTop, func(gtx layout.Context) layout.Dimensions {
		gtx.Constraints = layout.Exact(image.Pt(av, av))
		return fw.avatar.Layout(gtx, func(gtx layout.Context) layout.Dimensions {
			url := u.Avatar192
			if av > 192 && u.Avatar512 != "" {
				url = u.Avatar512
			}
			if url == "" {
				url = u.Avatar
			}
			paintAvatarRadius(gtx, h.Images(), url, image.Rect(0, 0, av, av), c, dp(gtx, 9))
			pointerCursor(gtx, image.Rect(0, 0, av, av))
			return layout.Dimensions{Size: image.Pt(av, av)}
		})
	})

	// the name row
	x := tx
	right := tx + tw
	nameW := measure(gtx, th, nameSt, name).X
	markW := 0
	for _, m := range marks {
		markW += dp(gtx, 4) + measure(gtx, th, nameSt, m).X
	}
	badgeW := 0
	if badge != "" {
		badgeW = dp(gtx, 4) + measure(gtx, th, textStyle{size: 10, weight: theme.Bold}, badge).X + dp(gtx, 6)
	}
	extra := dp(gtx, 4) // .c-member_name's margin-right
	if presence {
		extra += dp(gtx, 24)
	}
	if rec != nil {
		extra += dp(gtx, 30)
	}
	if maxName := right - x - markW - badgeW - extra; nameW > maxName {
		nameW = max(1, maxName) // MemberName truncates (o.CG)
	}
	btnW := nameW + markW + badgeW
	ny := colTop + (rowH-dp(gtx, 27))/2
	fw.rects["name"] = image.Rect(x, ny, x+btnW, ny+dp(gtx, 27))
	at(gtx, x, ny, func(gtx layout.Context) layout.Dimensions {
		gtx.Constraints = layout.Exact(image.Pt(btnW, dp(gtx, 27)))
		return fw.name.Layout(gtx, func(gtx layout.Context) layout.Dimensions {
			at(gtx, 0, 0, func(gtx layout.Context) layout.Dimensions {
				gtx.Constraints = layout.Constraints{Max: image.Pt(nameW, dp(gtx, 27))}
				return label(gtx, th, nameSt, name)
			})
			mx := nameW
			for _, m := range marks {
				mx += dp(gtx, 4)
				d := at(gtx, mx, 0, func(gtx layout.Context) layout.Dimensions { return label(gtx, th, nameSt, m) })
				mx += d.Size.X
			}
			if badge != "" {
				fwAppBadge(gtx, th, c, mx+dp(gtx, 4), (dp(gtx, 27)-dp(gtx, 14))/2, badge)
			}
			pointerCursor(gtx, image.Rect(0, 0, btnW, dp(gtx, 27)))
			return layout.Dimensions{Size: image.Pt(btnW, dp(gtx, 27))}
		})
	})
	x += btnW + dp(gtx, 4)
	if presence {
		box := dp(gtx, 20)
		py := colTop + (rowH-dp(gtx, 24))/2 + dp(gtx, 4)
		dot := dp(gtx, 9)
		at(gtx, x+(box-dot)/2, py+(box-dot)/2, func(gtx layout.Context) layout.Dimensions {
			return popups.MemberPresence(gtx, ph, u, 9)
		})
		x += box + dp(gtx, 4)
	}
	if rec != nil {
		at(gtx, x, colTop+(rowH-dp(gtx, 30))/2, func(gtx layout.Context) layout.Dimensions {
			return ctl.LayoutNameSound(gtx, &s.irec.btn, "dm-foreword", u.ID, rec)
		})
	}
	y := colTop + rowH
	for _, t := range []string{title, meta} {
		if t == "" {
			continue
		}
		d := at(gtx, tx, y, func(gtx layout.Context) layout.Dimensions {
			gtx.Constraints = layout.Constraints{Max: image.Pt(tw, 1<<20)}
			return label(gtx, th, subSt, t)
		})
		y += d.Size.Y
	}

	// the description
	y = y0 + e + dp(gtx, 16)
	slugText := "@" + name
	var src string
	switch typ {
	case fwSelf:
		src = "<b>This is your space.</b> Draft messages, list your to-dos, or keep links and files handy. You can also talk to yourself here, but please bear in mind you’ll have to supply both sides of the conversation."
	case fwSystem:
		src = "Slack notifications deliver messages about channel management, membership, and more — the things that happen in the background of your work day, but are still good to know about."
	case fwBot:
		src = "This is the very beginning of your direct message history with {name}"
	default:
		src = "This conversation is just between {name} and you. Check out their profile to learn more about them."
	}
	var runs []fwRun
	for _, p := range nsForeword.RT(src, "name", slugText) {
		runs = append(runs, fwRun{text: p.Text, bold: p.Tag == "b", slug: p.Arg == "name"})
	}
	dy := y
	d := at(gtx, x0, y, func(gtx layout.Context) layout.Dimensions {
		return s.fwDescription(gtx, h, th, c, runs, max(1, w-2*x0))
	})
	if r, ok := fw.rects["slug"]; ok {
		fw.rects["slug"] = r.Add(image.Pt(x0, dy))
	}
	y += d.Size.Y + dp(gtx, 16)

	// the buttons
	switch {
	case typ == fwMember:
		bd := at(gtx, x0, y, func(gtx layout.Context) layout.Dimensions {
			return popups.Button(gtx, th, &fw.view, popups.BtnOutline, popups.BtnMedium, nsForeword.T("View Profile"), "", 0)
		})
		fw.rects["view"] = image.Rectangle{Max: bd.Size}.Add(image.Pt(x0, y))
		y += bd.Size.Y + dp(gtx, 16)
	case typ == fwSelf && s.fwCanEditPhoto(h, app):
		bd := at(gtx, x0, y, func(gtx layout.Context) layout.Dimensions {
			return popups.Button(gtx, th, &fw.edit, popups.BtnOutline, popups.BtnMedium, nsForeword.T("Edit Profile"), "", 0)
		})
		fw.rects["edit"] = image.Rectangle{Max: bd.Size}.Add(image.Pt(x0, y))
		if !app.HasCustomPhoto(u.ID) {
			px := x0 + bd.Size.X + dp(gtx, 8)
			pd := at(gtx, px, y, func(gtx layout.Context) layout.Dimensions {
				return popups.Button(gtx, th, &fw.photo, popups.BtnOutline, popups.BtnMedium, nsForeword.T("Upload Profile Photo"), "", 0)
			})
			fw.rects["photo"] = image.Rectangle{Max: pd.Size}.Add(image.Pt(px, y))
		}
		y += bd.Size.Y
	case typ == fwBot:
		y += dp(gtx, 8)
		hd := at(gtx, x0, y, func(gtx layout.Context) layout.Dimensions {
			return fwHelpButton(gtx, th, c, &fw.help, nsForeword.T("How does {name} work?", "name", name))
		})
		fw.rects["help"] = image.Rectangle{Max: hd.Size}.Add(image.Pt(x0, y))
		y += hd.Size.Y
	}
	y += dp(gtx, 16)
	return layout.Dimensions{Size: image.Pt(w, y)}, true
}

// fwCanEditPhoto is showUploadPhotoCta (1fUZ): the viewer holds
// EDIT_PROFILE_PHOTO. Until permissions/info answers (asked once here)
// the buttons wait.
func (s *listState) fwCanEditPhoto(h Host, app *core.App) bool {
	if g, ok := app.CachedProfileGrants(); ok {
		return g.Photo
	}
	if !s.fw.grantsAsked {
		s.fw.grantsAsked = true
		go func() {
			ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
			defer cancel()
			app.LoadProfileGrants(ctx)
			h.Invalidate()
		}()
	}
	return false
}

// fwDescription lays the description's runs out word by word in w: 1.1rem
// (17.6px) on 26.4px lines in content-pry, <b> bold, the MemberSlug as
// c-member_slug--link on one line (padding 1px 2px, radius 3; hovered,
// sk_highlight_hover on rgba(sk_highlight_accent, .2)).
func (s *listState) fwDescription(gtx layout.Context, h Host, th *theme.Theme, c *colors, runs []fwRun, w int) layout.Dimensions {
	pal := styleForTheme(th, h.Images()).Palette
	lh := dp(gtx, 26.4)
	st := textStyle{size: 17.6, col: c.Text, lineH: 26.4}
	space := measure(gtx, th, st, " ").X
	x, line := 0, 0
	for _, r := range runs {
		rs := st
		if r.bold {
			rs.weight = theme.Bold
		}
		var words []string
		lead, trail := false, false
		if r.slug {
			words = []string{r.text} // one line
		} else {
			words = strings.Fields(r.text)
			lead = strings.HasPrefix(r.text, " ")
			trail = strings.HasSuffix(r.text, " ")
		}
		for i, wd := range words {
			if (i > 0 || lead) && x > 0 {
				x += space
			}
			pad := 0
			if r.slug {
				pad = dp(gtx, 2)
			}
			ww := measure(gtx, th, rs, wd).X + 2*pad
			if x > 0 && x+ww > w {
				x, line = 0, line+1
			}
			px, py := x, line*lh
			if r.slug {
				hov := s.fw.slug.Hovered()
				bg, fg := pal.MentionBg, pal.MentionText
				if hov {
					bg = pal.MentionHoverBg
					if v, ok := css.ParseColor("rgb(" + css.SlackVars(th.Dark)["--sk_highlight_hover"] + ")"); ok {
						fg = v
					}
				}
				box := image.Rect(px, py+(lh-dp(gtx, 22)-2*dp(gtx, 1))/2, px+ww, py+(lh+dp(gtx, 22)+2*dp(gtx, 1))/2)
				s.fw.rects["slug"] = image.Rect(px, py, px+ww, py+lh) // in the description; moved to the foreword's by the caller
				rrect(gtx.Ops, box, dp(gtx, 3), bg)
				rs.col = fg
				at(gtx, px, py, func(gtx layout.Context) layout.Dimensions {
					gtx.Constraints = layout.Exact(image.Pt(ww, lh))
					return s.fw.slug.Layout(gtx, func(gtx layout.Context) layout.Dimensions {
						at(gtx, pad, 0, func(gtx layout.Context) layout.Dimensions { return label(gtx, th, rs, wd) })
						pointerCursor(gtx, image.Rect(0, 0, ww, lh))
						return layout.Dimensions{Size: image.Pt(ww, lh)}
					})
				})
			} else {
				at(gtx, px, py, func(gtx layout.Context) layout.Dimensions { return label(gtx, th, rs, wd) })
			}
			x += ww
		}
		if trail && x > 0 {
			x += space
		}
	}
	return layout.Dimensions{Size: image.Pt(w, (line+1)*lh)}
}

// fwAppBadge is MemberName's c-member_name__indicator BaseAppBadge: 14px
// tall, 10px bold content-sec on --dt_color-surf-pry, padding 1px 3px,
// radius 2.
func fwAppBadge(gtx layout.Context, th *theme.Theme, c *colors, x, y int, label0 string) {
	st := textStyle{size: 10, weight: theme.Bold, col: c.Text2, lineH: 12}
	tw := measure(gtx, th, st, label0).X
	bg := color.NRGBA{R: 0x1d, G: 0x1c, B: 0x1d, A: 0x0f}
	if th.Dark {
		bg = color.NRGBA{R: 0xf8, G: 0xf8, B: 0xf8, A: 0x0f}
	}
	r := image.Rect(x, y, x+tw+dp(gtx, 6), y+dp(gtx, 14))
	rrect(gtx.Ops, r, dp(gtx, 2), bg)
	at(gtx, x+dp(gtx, 3), y+dp(gtx, 1), func(gtx layout.Context) layout.Dimensions { return label(gtx, th, st, label0) })
}

// fwHelpButton is AppHelpButton: a link button (.p-message_pane__foreword__app_help_button:
// .9rem, no underline on hover) with the lightbulb, a space and the label.
func fwHelpButton(gtx layout.Context, th *theme.Theme, c *colors, b *widget.Clickable, txt string) layout.Dimensions {
	st := textStyle{size: 14.4, col: c.Link, lineH: 21.6}
	is := dp(gtx, 14.4)
	tw := measure(gtx, th, st, " "+txt).X
	sz := image.Pt(is+tw, dp(gtx, 21.6))
	return b.Layout(gtx, func(gtx layout.Context) layout.Dimensions {
		at(gtx, 0, (sz.Y-is)/2, func(gtx layout.Context) layout.Dimensions {
			return icons.Get("lightbulb")(gtx, unit.Dp(14.4), c.Link)
		})
		at(gtx, is, 0, func(gtx layout.Context) layout.Dimensions { return label(gtx, th, st, " "+txt) })
		pointerCursor(gtx, image.Rectangle{Max: sz})
		return layout.Dimensions{Size: sz}
	})
}
