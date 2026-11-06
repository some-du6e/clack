package popups

import (
	"image"
	"image/color"

	"gioui.org/layout"
	"gioui.org/widget"

	"github.com/some-du6e/yoloslackgoclient/internal/core"
	"github.com/some-du6e/yoloslackgoclient/internal/ui/theme"
)

// Measured from app.slack.com (2026-11, full profile in the secondary
// pane): header 49 tall, "Profile" 18/900, close 28x28 radius 8. Avatar
// 256x256 radius 8 centered 16px below the header; name 22/900 with an
// "Edit" link (15/700 #1264a3) on the right for yourself; title 18px;
// link/info rows 38 apart (presence dot + "Active", clock + local time);
// buttons 36 tall radius 8 15/700 sharing the row ([Message] [Huddle ▾]
// [VIP] [⋮], own: [Set a status] [View as ▾] [⋮]); a full-width divider;
// "Contact information" 15/900 with 36px icon tiles, labels 13/700
// rgba(29,28,29,.7) and 15px values.

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
	handleBtn                    widget.Clickable

	paneW   int             // last laid-out width (px)
	moreBox image.Rectangle // kebab rect in pane coords (last frame)
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
	if ok {
		pfTick(gtx)
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
			return v.body(gtx, th, cl, u, ok, self, sz.X)
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
		} else {
			h.Toast("Huddles aren't supported in yoloslack yet")
		}
	}
	if v.btn3.Clicked(gtx) {
		h.Toast("VIPs aren't supported yet")
	}
	if v.editBtn.Clicked(gtx) || v.pronBtn.Clicked(gtx) || v.nameSndBtn.Clicked(gtx) {
		h.Toast(pfEditToast)
	}
	if v.handleBtn.Clicked(gtx) && !pfBlank(u.Handle) {
		copyText(gtx, "@"+u.Handle)
		h.Toast("Username copied")
	}
	if v.moreBtn.Clicked(gtx) {
		items := pfMoreItems(h, u, false, nil)
		h.ShowOverlay(&pfPaneMenu{h: h, items: items, box: v.moreBox, paneW: v.paneW})
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

	// name (+ presence) and Edit for yourself
	editW := 0
	if self {
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
		return pfNameLine(gtx, th, nil, u, 22, 30, 12, maxInt(1, inner-editW))
	})
	y += dp(gtx, 30)

	// title
	if !pfBlank(u.Title) {
		d := at(gtx, image.Pt(pad, y), func(gtx layout.Context) layout.Dimensions {
			gtx.Constraints = layout.Constraints{Max: image.Pt(inner, 1<<20)}
			return para(gtx, th, 18, 26, theme.Regular, cl.Fg, u.Title, 0)
		})
		y += d.Size.Y
	}
	y += dp(gtx, 4)

	// pronunciation / pronouns
	if self {
		at(gtx, image.Pt(pad, y), func(gtx layout.Context) layout.Dimensions {
			return pfLinkRow(gtx, th, cl, &v.nameSndBtn, "Add name pronunciation", rowH)
		})
		y += rowH
	}
	if !pfBlank(u.Pronouns) {
		at(gtx, image.Pt(pad, y), func(gtx layout.Context) layout.Dimensions {
			gtx.Constraints = layout.Constraints{Max: image.Pt(inner, rowH)}
			return vcenter(gtx, rowH, func(gtx layout.Context) layout.Dimensions {
				return para(gtx, th, 15, 22, theme.Regular, cl.Fg2, u.Pronouns, 1)
			})
		})
		y += rowH
	} else if self {
		at(gtx, image.Pt(pad, y), func(gtx layout.Context) layout.Dimensions {
			return pfLinkRow(gtx, th, cl, &v.pronBtn, "Add pronouns", rowH)
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

	// presence
	if u.Presence != "" {
		label := "Away"
		if u.Presence == "active" {
			label = "Active"
		}
		at(gtx, image.Pt(pad, y), func(gtx layout.Context) layout.Dimensions {
			dot := dp(gtx, 8)
			at(gtx, image.Pt((dp(gtx, 20)-dot)/2, (rowH-dot)/2), func(gtx layout.Context) layout.Dimensions {
				return presenceDot(gtx, th, u.Presence == "active", 8)
			})
			return at(gtx, image.Pt(dp(gtx, 28), 0), func(gtx layout.Context) layout.Dimensions {
				gtx.Constraints = layout.Constraints{Max: image.Pt(maxInt(1, inner-dp(gtx, 28)), rowH)}
				return vcenter(gtx, rowH, func(gtx layout.Context) layout.Dimensions {
					return text(gtx, th, 15, theme.Regular, cl.Fg, label)
				})
			})
		})
		y += rowH
	}

	// local time
	if pfHasClock(u) {
		at(gtx, image.Pt(pad, y), func(gtx layout.Context) layout.Dimensions {
			gtx.Constraints = layout.Constraints{Max: image.Pt(inner, rowH)}
			return vcenter(gtx, rowH, func(gtx layout.Context) layout.Dimensions {
				return pfIconLine(gtx, th, cl, "clock", localTime(u, gtx.Now)+" local time", 15, 22)
			})
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
			{c: &v.btn2, label: "Huddle", lead: "headphones", caret: true},
			{c: &v.btn3, label: "VIP", lead: "user-plus"},
		}
	}
	by := y
	d := at(gtx, image.Pt(pad, y), func(gtx layout.Context) layout.Dimensions {
		rd, kr := pfBtnRow(gtx, th, BtnMedium, specs, &v.moreBtn, inner, true)
		v.moreBox = kr.Add(image.Pt(pad, by+dp(gtx, 49)-v.list.Position.Offset))
		return rd
	})
	y += d.Size.Y + pad

	// contact information
	fill(gtx, image.Rect(0, y, w, y+1), cl.Ring)
	y++
	y += dp(gtx, 20)
	y += v.section(gtx, th, cl, y, w, "Contact information")
	y += dp(gtx, 12)
	if !pfBlank(u.Handle) {
		y += v.field(gtx, th, cl, y, inner, "at", "Username", "@"+u.Handle, &v.handleBtn)
		y += dp(gtx, 12)
	}
	if u.TZLabel != "" || u.TZ != "" {
		tz := u.TZLabel
		if tz == "" {
			tz = u.TZ
		}
		y += v.field(gtx, th, cl, y, inner, "globe", "Timezone", tz, nil)
		y += dp(gtx, 12)
	}
	y += dp(gtx, 4)

	// about me
	if !pfBlank(u.Title) {
		fill(gtx, image.Rect(0, y, w, y+1), cl.Ring)
		y++
		y += dp(gtx, 20)
		y += v.section(gtx, th, cl, y, w, "About me")
		y += dp(gtx, 12)
		lbl := pfFieldLabel(th)
		at(gtx, image.Pt(pad, y), func(gtx layout.Context) layout.Dimensions {
			return text(gtx, th, 13, theme.Bold, lbl, "Title")
		})
		y += dp(gtx, 18)
		d := at(gtx, image.Pt(pad, y), func(gtx layout.Context) layout.Dimensions {
			gtx.Constraints = layout.Constraints{Max: image.Pt(inner, 1<<20)}
			return para(gtx, th, 15, 22, theme.Regular, cl.Fg, u.Title, 0)
		})
		y += d.Size.Y + pad
	}
	y += pad
	return layout.Dimensions{Size: image.Pt(w, y)}
}

// section draws a 15/900 section title at y and returns its height.
func (v *ProfileView) section(gtx layout.Context, th *theme.Theme, cl *Colors, y, w int, title string) int {
	pad := dp(gtx, 16)
	d := at(gtx, image.Pt(pad, y), func(gtx layout.Context) layout.Dimensions {
		gtx.Constraints = layout.Constraints{Max: image.Pt(maxInt(1, w-2*pad), dp(gtx, 22))}
		return para(gtx, th, 15, 22, theme.Black, cl.Fg, title, 1)
	})
	return d.Size.Y
}

// field draws an icon tile + label + value row at (16, y); value is a link
// when c is set. Returns its height.
func (v *ProfileView) field(gtx layout.Context, th *theme.Theme, cl *Colors, y, inner int, ic, label, value string, c *widget.Clickable) int {
	pad := dp(gtx, 16)
	tile := dp(gtx, 36)
	at(gtx, image.Pt(pad, y), func(gtx layout.Context) layout.Dimensions {
		r := image.Rect(0, 0, tile, tile)
		rrect(gtx, r, dp(gtx, 8), cl.Ring)
		rrect(gtx, r.Inset(1), dp(gtx, 7), cl.BgAlt)
		at(gtx, image.Pt((tile-dp(gtx, 20))/2, (tile-dp(gtx, 20))/2), func(gtx layout.Context) layout.Dimensions {
			return icon(gtx, ic, 20, cl.Fg2)
		})
		return layout.Dimensions{Size: r.Max}
	})
	x := pad + tile + dp(gtx, 8)
	tw := maxInt(1, inner+pad-x)
	lbl := pfFieldLabel(th)
	at(gtx, image.Pt(x, y), func(gtx layout.Context) layout.Dimensions {
		gtx.Constraints = layout.Constraints{Max: image.Pt(tw, dp(gtx, 18))}
		return para(gtx, th, 13, 18, theme.Bold, lbl, label, 1)
	})
	at(gtx, image.Pt(x, y+dp(gtx, 18)), func(gtx layout.Context) layout.Dimensions {
		gtx.Constraints = layout.Constraints{Max: image.Pt(tw, dp(gtx, 22))}
		if c == nil {
			return para(gtx, th, 15, 22, theme.Regular, cl.Fg, value, 1)
		}
		return clickBox(gtx, c, nil, func(gtx layout.Context) layout.Dimensions {
			return para(gtx, th, 15, 22, theme.Regular, cl.Link, value, 1)
		})
	})
	return maxInt(tile, dp(gtx, 40))
}

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
	h     Host
	items []MenuItem
	box   image.Rectangle // control rect in pane coords
	paneW int             // pane width in px
	menu  *Menu
	done  bool
}

func (p *pfPaneMenu) Layout(gtx layout.Context) layout.Dimensions {
	if p.done {
		return layout.Dimensions{}
	}
	if p.menu == nil {
		win := gtx.Constraints.Max
		off := image.Pt(win.X-dp(gtx, theme.PanelInset)-p.paneW, dp(gtx, theme.TopNavH))
		a := p.box.Add(off)
		p.menu = NewMenuOpts(p.h, p.items, MenuOpts{Anchor: a, Side: Below, AlignRight: true})
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
