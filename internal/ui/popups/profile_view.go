package popups

import (
	"image"
	"image/color"
	"strings"
	"time"

	"gioui.org/layout"
	"gioui.org/widget"

	"github.com/some-du6e/yoloslackgoclient/internal/core"
	"github.com/some-du6e/yoloslackgoclient/internal/ui/slackc"
	"github.com/some-du6e/yoloslackgoclient/internal/ui/theme"
)

// Measured from app.slack.com (2026-11, full profile in the secondary
// pane): header 49 tall, "Profile" 18/900, close 28x28 radius 8. Avatar
// 256x256 radius 8 centered 16px below the header; name 22/900 with an
// "Edit" link (15/700 #1264a3) on the right for yourself; title 18px;
// link/info rows 38 apart (presence dot + "Active", clock + local time);
// buttons 36 tall radius 8 15/700 sharing the row ([Message] [Huddle ▾]
// [VIP] [⋮], own: [Set a status] [View as ▾] [⋮]); then the profile
// sections (profile_sections.go).
//
// The bio follows Slack's RimetoProfileBio (module DqK0): Name is the
// member's real_name only (getMemberRealName: no display-name fallback and
// no presence dot, so a real name of a lone zero-width space draws an
// empty line with just Edit), Title (18px, line-height 1.5), pronouns ·
// name pronunciation, the add links for yourself, then presence, status
// and local time, then the action row.

// ProfileView is the full profile shown in the secondary pane.
type ProfileView struct {
	h    Host
	id   string
	list layout.List

	asked   bool
	missing bool

	closeBtn                     widget.Clickable
	btn1, btn2, btn3, moreBtn    widget.Clickable // Message|Set a status, Huddle|View as, VIP, ⋮
	editBtn, pronBtn, nameSndBtn widget.Clickable

	paneW   int             // last laid-out width (px)
	moreBox image.Rectangle // kebab rect in pane coords (last frame)
	hudBox  image.Rectangle // Huddle button rect in pane coords (last frame)

	editUntil time.Time // EditProfileFormPane is mounted until then

	secs pfSecState // users.profile.getSections (profile_sections.go)
}

// NewProfileView builds the full profile pane for userID.
func NewProfileView(h Host, userID string) *ProfileView {
	v := &ProfileView{h: h, id: userID}
	v.list.Axis = layout.Vertical
	pfSubscribe(h, userID)
	return v
}

const pfEditToast = "Editing your profile isn't supported in yoloslack yet"

// Layout fills the constraints with the profile.
func (v *ProfileView) Layout(gtx layout.Context) layout.Dimensions {
	drain()
	th := v.h.Theme()
	cl := colorsOf(th)
	sz := gtx.Constraints.Max
	v.paneW = sz.X
	u, ok := lookupUser(v.h, v.id)
	if !ok && !v.asked && v.h.Core() != nil && v.id != "" {
		v.asked = true
		pfEnsure(v.h, v.id, func(found bool) { v.missing = !found })
	}
	self := ok && pfIsSelf(v.h, u.ID)
	v.clicks(gtx, u, ok, self)
	if self {
		v.editPane(gtx)
	}
	if ok {
		pfTick(gtx)
		v.fetchSections()
	}

	// header
	hh := dp(gtx, 49)
	pad := dp(gtx, 16)
	at(gtx, image.Pt(pad, 0), func(gtx layout.Context) layout.Dimensions {
		gtx.Constraints = layout.Constraints{Max: image.Pt(maxInt(1, sz.X-pad-dp(gtx, 52)), hh)}
		return vcenter(gtx, hh, func(gtx layout.Context) layout.Dimensions {
			return text(gtx, th, 18, theme.Black, cl.Fg, "Profile")
		})
	})
	at(gtx, image.Pt(sz.X-dp(gtx, 12)-dp(gtx, 28), (hh-dp(gtx, 28))/2), func(gtx layout.Context) layout.Dimensions {
		return iconButton(gtx, &v.closeBtn, "close", 28, 20, cl.Fg2, cl.BgHover, 8)
	})

	// body
	bodyH := maxInt(0, sz.Y-hh)
	at(gtx, image.Pt(0, hh), func(gtx layout.Context) layout.Dimensions {
		gtx.Constraints = layout.Exact(image.Pt(sz.X, bodyH))
		defer clipRRect(gtx, image.Rect(0, 0, sz.X, bodyH), 0).Pop()
		return v.list.Layout(gtx, 1, func(gtx layout.Context, _ int) layout.Dimensions {
			gtx.Constraints.Min = image.Point{}
			if !ok {
				return v.body(gtx, th, cl, u, ok, self, sz.X)
			}
			return v.content(gtx, th, cl, u, self, sz.X)
		})
	})
	return layout.Dimensions{Size: sz}
}

func (v *ProfileView) clicks(gtx layout.Context, u core.User, ok, self bool) {
	h := v.h
	if v.closeBtn.Clicked(gtx) {
		h.CloseSecondary()
	}
	if !ok {
		return
	}
	if v.btn1.Clicked(gtx) {
		if self {
			pfSetStatus(h)
		} else {
			pfMessage(h, u.ID)
		}
	}
	if v.btn2.Clicked(gtx) {
		if self {
			h.Toast("Viewing your profile as someone else isn't supported yet")
		} else if !v.hudBox.Empty() {
			h.ShowOverlay(&pfPaneMenu{h: h, box: v.hudBox, paneW: v.paneW, member: u.ID, call: true})
		}
	}
	if v.btn3.Clicked(gtx) {
		h.Toast("VIPs aren't supported yet")
	}
	if v.editBtn.Clicked(gtx) || v.pronBtn.Clicked(gtx) || v.nameSndBtn.Clicked(gtx) {
		v.editUntil = gtx.Now.Add(pfEditPaneFor)
		pfEditProfile(h)
	}
	if v.moreBtn.Clicked(gtx) {
		items := pfMoreItems(h, u, false, nil)
		h.ShowOverlay(&pfPaneMenu{h: h, items: items, box: v.moreBox, paneW: v.paneW, member: u.ID, app: pfAppID(h, u)})
	}
}

// body is the scrolling content (one list item); w is the pane width.
func (v *ProfileView) body(gtx layout.Context, th *theme.Theme, cl *Colors, u core.User, ok, self bool, w int) layout.Dimensions {
	pad := dp(gtx, 16)
	inner := maxInt(1, w-2*pad)
	rowH := dp(gtx, 38)
	y := pad

	// avatar
	avDp := float32(256)
	if lim := float32(inner) / gtx.Metric.PxPerDp; lim < avDp {
		avDp = lim
	}
	av := dp(gtx, avDp)
	at(gtx, image.Pt((w-av)/2, y), func(gtx layout.Context) layout.Dimensions {
		if !ok {
			rrect(gtx, image.Rect(0, 0, av, av), dp(gtx, 8), cl.Selected)
			return layout.Dimensions{Size: image.Pt(av, av)}
		}
		return pfImage(gtx, v.h, pfAvatarURL(u, true), avDp, 8)
	})
	y += av + pad

	if !ok {
		d := at(gtx, image.Pt(pad, y), func(gtx layout.Context) layout.Dimensions {
			gtx.Constraints = layout.Constraints{Max: image.Pt(inner, 1<<20)}
			return pfSkeleton(gtx, th, cl, v.missing, v.id, inner)
		})
		y += d.Size.Y + pad
		return layout.Dimensions{Size: image.Pt(w, y)}
	}

	// name (real_name) and Edit for yourself
	editW := 0
	if self { // Name's Edit link: !preview && isSelf
		editW = textW(gtx, th, 15, theme.Bold, "Edit")
		at(gtx, image.Pt(w-pad-editW, y), func(gtx layout.Context) layout.Dimensions {
			return clickBox(gtx, &v.editBtn, nil, func(gtx layout.Context) layout.Dimensions {
				return vcenter(gtx, dp(gtx, 30), func(gtx layout.Context) layout.Dimensions {
					return text(gtx, th, 15, theme.Bold, cl.Link, "Edit")
				})
			})
		})
		editW += dp(gtx, 12)
	}
	at(gtx, image.Pt(pad, y), func(gtx layout.Context) layout.Dimensions {
		gtx.Constraints = layout.Constraints{Max: image.Pt(maxInt(1, inner-editW), dp(gtx, 30))}
		return para(gtx, th, 22, 30, theme.Black, cl.Fg, u.RealName, 1)
	})
	y += dp(gtx, 30)

	// title (.p-r_member_profile__subtitle: 18px, line-height 1.5)
	if !pfBlank(u.Title) {
		d := at(gtx, image.Pt(pad, y), func(gtx layout.Context) layout.Dimensions {
			gtx.Constraints = layout.Constraints{Max: image.Pt(inner, 1<<20)}
			return para(gtx, th, 18, 27, theme.Regular, cl.Fg, u.Title, 0)
		})
		y += d.Size.Y
	}
	y += dp(gtx, 4)

	// pronouns · name pronunciation, then the add links (yourself). The
	// pronunciation link needs the workspace's Name Pronunciation (or
	// Name Recording) field, from the header section.
	hd, hdOK := v.secs.header()
	hdata := pfHeaderDataOf(hd)
	var pron []string
	if !pfBlank(u.Pronouns) {
		pron = append(pron, u.Pronouns)
	}
	if !pfBlank(hdata.pronunciation) {
		pron = append(pron, hdata.pronunciation)
	}
	addSound := hdOK && (hdata.hasPronunciation && hdata.editable || hdata.recording) && hdata.recordingFile == "" && pfBlank(hdata.pronunciation)
	if v.secs.done && v.secs.err != nil {
		addSound = true // no sections (RimetoProfileLegacyData): offer it
	}
	if self && addSound {
		d := at(gtx, image.Pt(pad, y), func(gtx layout.Context) layout.Dimensions {
			return pfLinkRow(gtx, th, cl, &v.nameSndBtn, "Add name pronunciation", rowH)
		})
		if x := pad + d.Size.X + dp(gtx, 8); x < pad+inner {
			at(gtx, image.Pt(x, y), func(gtx layout.Context) layout.Dimensions {
				gtx.Constraints = layout.Constraints{Max: image.Pt(pad+inner-x, rowH)}
				return slackc.RenderLazy(gtx, "EditAudioButton", v.audioProps, pfNoNative)
			})
		}
		y += rowH
	}
	if len(pron) > 0 {
		at(gtx, image.Pt(pad, y), func(gtx layout.Context) layout.Dimensions {
			gtx.Constraints = layout.Constraints{Max: image.Pt(inner, rowH)}
			return vcenter(gtx, rowH, func(gtx layout.Context) layout.Dimensions {
				return para(gtx, th, 15, 22, theme.Regular, cl.Fg2, strings.Join(pron, " · "), 1)
			})
		})
		y += rowH
	}
	if self && pfBlank(u.Pronouns) {
		at(gtx, image.Pt(pad, y), func(gtx layout.Context) layout.Dimensions {
			return pfLinkRow(gtx, th, cl, &v.pronBtn, "Add pronouns", rowH)
		})
		y += rowH
	}

	// presence, then status (RimetoProfileBio's BioRows)
	if !u.IsBot { // unknown presence reads as Away, like Slack
		at(gtx, image.Pt(pad, y), func(gtx layout.Context) layout.Dimensions {
			gtx.Constraints = layout.Constraints{Max: image.Pt(inner, rowH)}
			return v.presence(gtx, th, u, rowH)
		})
		y += rowH
	}

	// status
	if pfHasStatus(u, gtx.Now) {
		y += dp(gtx, 8)
		d := at(gtx, image.Pt(pad, y), func(gtx layout.Context) layout.Dimensions {
			gtx.Constraints = layout.Constraints{Max: image.Pt(inner, 1<<20)}
			return pfStatusLine(gtx, v.h, th, cl, u, 15, 22, 20)
		})
		y += d.Size.Y + dp(gtx, 8)
	}

	// local time
	if pfHasClock(u) {
		at(gtx, image.Pt(pad, y), func(gtx layout.Context) layout.Dimensions {
			gtx.Constraints = layout.Constraints{Max: image.Pt(inner, rowH)}
			return v.localTime(gtx, th, cl, u, rowH)
		})
		y += rowH
	}

	// buttons
	y += dp(gtx, 12)
	var specs []pfBtnSpec
	if self {
		specs = []pfBtnSpec{
			{c: &v.btn1, label: "Set a status"},
			{c: &v.btn2, label: "View as", caret: true},
		}
	} else {
		specs = []pfBtnSpec{
			{c: &v.btn1, label: "Message", lead: "threads"},
			{c: &v.btn2, label: "Huddle", lead: "headphones", caret: true, rect: &v.hudBox},
			{c: &v.btn3, label: "VIP", lead: "user-plus", vip: true},
		}
	}
	by := y
	v.hudBox = image.Rectangle{}
	d := at(gtx, image.Pt(pad, y), func(gtx layout.Context) layout.Dimensions {
		rd, kr := pfBtnRow(gtx, th, BtnMedium, specs, &v.moreBtn, inner, true)
		v.moreBox = kr.Add(image.Pt(pad, by+dp(gtx, 49)-v.list.Position.Offset))
		if !v.hudBox.Empty() {
			v.hudBox = v.hudBox.Add(image.Pt(pad, by+dp(gtx, 49)-v.list.Position.Offset))
		}
		return rd
	})
	y += d.Size.Y + pad

	// sections: Contact information, People, About me, …
	if !v.secs.done {
		y += pad
	}
	y = v.sections(gtx, th, cl, u, self, y, w)
	return layout.Dimensions{Size: image.Pt(w, y)}
}

// presence is the presence row: RimetoProfilePresence{member} around
// BasePresence{showText} (LastSeen's context reaches the BasePresence).
func (v *ProfileView) presence(gtx layout.Context, th *theme.Theme, u core.User, rowH int) layout.Dimensions {
	pp, native := pfPresenceParts(v.h, th, u, 8, true, rowH)
	return slackc.RenderLazy(gtx, "RimetoProfilePresence", func() slackc.Props {
		return slackc.Props{
			"$key":     "profile-presence:" + u.ID,
			"member":   slackMember(u),
			"children": slackc.Child("Presence", pp(), native),
		}
	}, func(gtx layout.Context, p slackc.Props) layout.Dimensions {
		return childrenOr(gtx, p, "Presence", pp, native)
	})
}

// localTime is the local time row: LocalTimeRow{member} around Slack's
// LocalTime (Slick's LocalTime draws its own row there and hides
// LocalTime; LastSeen's card line reads the same LocalTime).
func (v *ProfileView) localTime(gtx layout.Context, th *theme.Theme, cl *Colors, u core.User, rowH int) layout.Dimensions {
	lt := func() slackc.Props {
		return slackc.Props{"$key": "profile-localtime:" + u.ID, "member": slackMember(u)}
	}
	native := func(gtx layout.Context, _ slackc.Props) layout.Dimensions {
		return vcenter(gtx, rowH, func(gtx layout.Context) layout.Dimensions {
			return pfIconLine(gtx, th, cl, "clock", localTime(u, gtx.Now)+" local time", 15, 22)
		})
	}
	return slackc.RenderLazy(gtx, "LocalTimeRow", func() slackc.Props {
		return slackc.Props{
			"$key":     "profile-localtime-row:" + u.ID,
			"member":   slackMember(u),
			"children": slackc.Child("LocalTime", lt(), native),
		}
	}, func(gtx layout.Context, p slackc.Props) layout.Dimensions {
		return childrenOr(gtx, p, "LocalTime", lt, native)
	})
}

// audioProps are EditAudioButton's (the name-recording control; Clack
// draws none, plugins append their own).
func (v *ProfileView) audioProps() slackc.Props {
	h := v.h
	return slackc.Props{
		"$key": "edit-audio:" + v.id,
		"onChangeFile": func(fileID any) {
			if s, ok := fileID.(string); ok && s != "" {
				h.Toast("Name recording uploaded")
			}
		},
		"onFileUploadStart": func() {},
		"onFileUploadEnd":   func() {},
	}
}

// pfNoNative draws nothing (boundaries Clack has no widget for).
func pfNoNative(layout.Context, slackc.Props) layout.Dimensions { return layout.Dimensions{} }

// pfFieldLabel is the profile field label color rgba(29,28,29,.7).
func pfFieldLabel(th *theme.Theme) color.NRGBA {
	return theme.WithAlpha(colorsOf(th).Fg, 178)
}

// pfLinkRow draws "+ label" in link color, vertically centered in h.
func pfLinkRow(gtx layout.Context, th *theme.Theme, cl *Colors, c *widget.Clickable, label string, h int) layout.Dimensions {
	return clickBox(gtx, c, nil, func(gtx layout.Context) layout.Dimensions {
		at(gtx, image.Pt(0, (h-dp(gtx, 18))/2), func(gtx layout.Context) layout.Dimensions {
			return icon(gtx, "plus", 18, cl.Link)
		})
		d := at(gtx, image.Pt(dp(gtx, 18)+dp(gtx, 4), 0), func(gtx layout.Context) layout.Dimensions {
			return vcenter(gtx, h, func(gtx layout.Context) layout.Dimensions {
				return text(gtx, th, 15, theme.Regular, cl.Link, label)
			})
		})
		return layout.Dimensions{Size: image.Pt(dp(gtx, 22)+d.Size.X, h)}
	})
}

// pfPaneMenu opens a Menu for a control inside the secondary pane. The pane
// only knows its own coordinates, so the window anchor is derived on the
// first frame from the window size, assuming the ui root's layout: the
// secondary pane is right-aligned in the workspace (theme.PanelInset from
// the window's right edge) and starts below the top nav (theme.TopNavH).
type pfPaneMenu struct {
	h      Host
	items  []MenuItem
	box    image.Rectangle // control rect in pane coords
	paneW  int             // pane width in px
	member string          // whose profile (RimetoMemberProfileOverflowMenu)
	app    string          // the app's id on an app's profile (AppDetailsOverflow)
	call   bool            // the Huddle button's call menu (pfHuddleMenu)
	menu   *Menu
	done   bool
}

func (p *pfPaneMenu) Layout(gtx layout.Context) layout.Dimensions {
	if p.done {
		return layout.Dimensions{}
	}
	if p.menu == nil {
		win := gtx.Constraints.Max
		off := image.Pt(win.X-dp(gtx, theme.PanelInset)-p.paneW, dp(gtx, theme.TopNavH))
		a := p.box.Add(off)
		if p.call {
			if p.menu = pfHuddleMenuAt(gtx, p.h, p.member, a, nil); p.menu == nil {
				p.done = true
				return layout.Dimensions{}
			}
			return p.menu.Layout(gtx)
		}
		o := MenuOpts{Anchor: a, Side: Below, AlignRight: true}
		if p.app != "" {
			o = pfAppMenuOpts(p.app, p.member, o)
		} else {
			o = pfMenuOpts(p.member, o)
		}
		p.menu = NewMenuOpts(p.h, p.items, o)
	}
	return p.menu.Layout(gtx)
}

func (p *pfPaneMenu) Dismissed() bool { return p.done || (p.menu != nil && p.menu.Dismissed()) }

// Dismiss closes the menu (idempotent).
func (p *pfPaneMenu) Dismiss() {
	p.done = true
	if p.menu != nil {
		p.menu.closeAll()
	}
}
