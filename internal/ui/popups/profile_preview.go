package popups

import (
	"image"

	"gioui.org/io/event"
	"gioui.org/io/pointer"
	"gioui.org/layout"
	"gioui.org/op/clip"

	"github.com/some-du6e/clack/internal/ui/theme"
)

// "View as" on your own full profile, from Slack's web client (boot-shared,
// build a84662d2d135):
//
//   - DqK0 MemberProfilePreviewButton: "View as ▾" in the action row (you,
//     not previewing) opens LEYP ProfilePreviewMenu, a 320-wide menu of two
//     radio items: "A coworker at <org>" ("A coworker" without one, the
//     team's 24px icon) and "A contact from other organizations" (the
//     shared-channel icon); N6Hz usePreviewModeLabels.
//   - 9xzW RimetoMemberProfileFlexpane keeps previewMode (Internal or
//     External, PreviewModeContext) and while it is set draws PreviewMenu
//     above the profile: .p-r_member_profile__preview-menu (#1d9bd11a,
//     padding 12px 16px), a small select button "Viewing As Coworker" /
//     "Viewing as External" (white on #1264a3, #0b4c8c on hover; it opens
//     the same menu, bottom-left) and a small "Exit Preview". The profile
//     underneath is .interaction_disabled (pointer-events none), the rest
//     of the window gets .p-flexpane__preview-mode-overlay (#1d1c1d at 30%;
//     a click on it exits), and Escape exits.
//   - In preview the profile is the one others see (useIsProfilePreview):
//     no Edit or add links, the action row is MemberProfileButtons with
//     isProfilePreview (Message, Huddle, ⋮ with others' items), and the
//     External preview draws the presence as an external member's
//     (FakeExternalPreviewPresence). What limited Slack Connect
//     visibility hides (title, pronouns) needs team prefs Clack doesn't
//     read, so the External preview shows them.

// Preview modes (lUcP $_): none, Internal (a coworker), External.
const (
	pfPreviewNone = iota
	pfPreviewInternal
	pfPreviewExternal
)

// pfPreviewLabels is usePreviewModeLabels: the menu items' labels.
func pfPreviewLabels(h Host) (teammate, external string) {
	teammate = "A coworker"
	if org := pfOrgName(h); org != "" {
		teammate = "A coworker at " + org
	}
	return teammate, "A contact from other organizations"
}

// pfOrgName is the viewer's organization (the enterprise's name on a
// Grid, else the team's).
func pfOrgName(h Host) string {
	c := h.Core()
	if c == nil {
		return ""
	}
	snap := c.Snapshot()
	return snap.Team.Name
}

// previewItems is ProfilePreviewMenu's template.
func (v *ProfileView) previewItems() []MenuItem {
	h := v.h
	teammate, external := pfPreviewLabels(h)
	row := func(label string, mode int) MenuItem {
		return MenuItem{Key: "preview-" + label, Label: label, Radio: true, Checked: v.preview == mode,
			Widget: func(gtx layout.Context, hl bool) layout.Dimensions {
				return pfPreviewRow(gtx, h, label, mode, v.preview == mode, hl)
			},
			Do: func() { v.setPreview(mode) }}
	}
	return []MenuItem{row(teammate, pfPreviewInternal), row(external, pfPreviewExternal)}
}

// pfPreviewRow draws one ProfilePreviewMenu item: the radio check, the
// 24px icon (margin-top 2) and the label.
func pfPreviewRow(gtx layout.Context, h Host, label string, mode int, checked, hl bool) layout.Dimensions {
	th := h.Theme()
	cl := colorsOf(th)
	w := gtx.Constraints.Max.X
	hh := dp(gtx, 36)
	fg := cl.Fg
	if hl {
		fill(gtx, image.Rect(0, 0, w, hh), cl.Highlight)
		fg = cl.HighText
	} else if checked {
		fg = cl.Link
	}
	x := dp(gtx, 16)
	if checked {
		at(gtx, image.Pt(x, (hh-dp(gtx, 16))/2), func(gtx layout.Context) layout.Dimensions {
			return icon(gtx, "check", 16, fg)
		})
	}
	x += dp(gtx, 24)
	iy := (hh-dp(gtx, 24))/2 + dp(gtx, 1)
	if mode == pfPreviewInternal {
		name := pfOrgName(h)
		url := ""
		if c := h.Core(); c != nil {
			url = c.Snapshot().Team.Icon
		}
		pfTeamIcon(gtx, h, th, name, url, image.Pt(x, iy), 24, false)
	} else {
		// .p-r_member_profile__preview-view-as-icon.external: the text
		// colour, the highlight colour when selected
		ic := cl.Fg
		if checked {
			ic = cl.Link
		}
		if hl {
			ic = cl.HighText
		}
		at(gtx, image.Pt(x, iy), func(gtx layout.Context) layout.Dimensions {
			return icon(gtx, "shared-channel", 24, ic)
		})
	}
	x += dp(gtx, 24) + dp(gtx, 8)
	at(gtx, image.Pt(x, 0), func(gtx layout.Context) layout.Dimensions {
		gtx.Constraints = layout.Constraints{Max: image.Pt(maxInt(1, w-x-dp(gtx, 16)), hh)}
		return vcenter(gtx, hh, func(gtx layout.Context) layout.Dimensions {
			return para(gtx, th, 15, 22, theme.Regular, fg, label, 1)
		})
	})
	return layout.Dimensions{Size: image.Pt(w, hh)}
}

// setPreview enters or leaves a preview (mode pfPreviewNone exits).
func (v *ProfileView) setPreview(mode int) {
	if v.preview == mode {
		return
	}
	v.preview = mode
	if mode != pfPreviewNone && (v.dim == nil || v.dim.Dismissed()) {
		v.dim = &pfPreviewDim{v: v}
		v.h.ShowOverlay(v.dim)
	}
	v.h.Invalidate()
}

// previewBar is PreviewMenu across the pane's top at y (pane coords);
// it returns its height.
func (v *ProfileView) previewBar(gtx layout.Context, th *theme.Theme, cl *Colors, w, y int) int {
	pad := dp(gtx, 16)
	padY := dp(gtx, 12)
	bh := dp(gtx, 28)
	h := padY + bh + padY
	bg := rgba(0x1d9bd1, 0x1a/255.0)
	fill(gtx, image.Rect(0, y, w, y+h), bg)
	label := "Viewing As Coworker"
	if v.preview == pfPreviewExternal {
		label = "Viewing as External"
	}
	// the select button: label (margin-right 4) and the dropdown caret
	m := pfMetrics(BtnSmall)
	lw := textW(gtx, th, m.fs, theme.Bold, label)
	sw := dp(gtx, m.pad) + lw + dp(gtx, 4) + dp(gtx, m.csz) + dp(gtx, m.pad)
	sel := image.Rect(pad, y+padY, pad+sw, y+padY+bh)
	v.viewingBox = sel.Add(image.Pt(0, dp(gtx, 49)-v.list.Position.Offset)) // pane coords
	at(gtx, sel.Min, func(gtx layout.Context) layout.Dimensions {
		return v.viewingBtn.Layout(gtx, func(gtx layout.Context) layout.Dimensions {
			sz := sel.Size()
			c := rgb(0x1264a3)
			if v.viewingBtn.Hovered() {
				c = rgb(0x0b4c8c)
			}
			rrect(gtx, image.Rectangle{Max: sz}, dp(gtx, m.rad), c)
			at(gtx, image.Pt(dp(gtx, m.pad), 0), func(gtx layout.Context) layout.Dimensions {
				return vcenter(gtx, sz.Y, func(gtx layout.Context) layout.Dimensions {
					return text(gtx, th, m.fs, theme.Bold, rgb(0xffffff), label)
				})
			})
			at(gtx, image.Pt(sz.X-dp(gtx, m.pad)-dp(gtx, m.csz), (sz.Y-dp(gtx, m.csz))/2), func(gtx layout.Context) layout.Dimensions {
				return icon(gtx, "chevron-down", m.csz, rgb(0xffffff))
			})
			pointerCursor(gtx, sz)
			return layout.Dimensions{Size: sz}
		})
	})
	ew := pfBtnNatural(gtx, th, m, pfBtnSpec{label: "Exit Preview"})
	at(gtx, image.Pt(w-pad-ew, y+padY), func(gtx layout.Context) layout.Dimensions {
		return pfBtn(gtx, th, BtnSmall, pfBtnSpec{c: &v.exitBtn, label: "Exit Preview"}, ew)
	})
	return h
}

// shield is .interaction_disabled: a pointer area over r (pane coords)
// that takes the presses meant for the profile underneath.
func (v *ProfileView) shield(gtx layout.Context, r image.Rectangle) {
	if r.Empty() {
		return
	}
	defer clip.Rect(r).Push(gtx.Ops).Pop()
	event.Op(gtx.Ops, &v.shieldTag)
	for {
		if _, ok := gtx.Event(pointer.Filter{Target: &v.shieldTag, Kinds: pointer.Press | pointer.Release}); !ok {
			break
		}
	}
}

// pfPreviewDim is .p-flexpane__preview-mode-overlay: the window outside
// the secondary pane dimmed (#1d1c1d at 30%) while a preview is on; a
// click on it, or Escape (the root dismisses the top overlay), exits the
// preview.
type pfPreviewDim struct {
	v    *ProfileView
	done bool
	tag  int
}

func (d *pfPreviewDim) Layout(gtx layout.Context) layout.Dimensions {
	if d.done || d.v.preview == pfPreviewNone {
		d.done = true
		return layout.Dimensions{}
	}
	if !d.v.lastFrame.Equal(gtx.Now) {
		// the pane went away (closed, replaced, hidden): the flexpane's
		// unmount drops the preview with its overlays
		d.done = true
		d.v.preview = pfPreviewNone
		return layout.Dimensions{}
	}
	win := gtx.Constraints.Max
	off := image.Pt(win.X-dp(gtx, theme.PanelInset)-d.v.paneW, dp(gtx, theme.TopNavH))
	pane := image.Rectangle{Min: off, Max: image.Pt(off.X+d.v.paneW, win.Y-dp(gtx, theme.PanelInset))}
	parts := []image.Rectangle{
		{Max: image.Pt(win.X, pane.Min.Y)},
		{Min: image.Pt(0, pane.Min.Y), Max: image.Pt(pane.Min.X, win.Y)},
		{Min: image.Pt(pane.Max.X, pane.Min.Y), Max: win},
		{Min: image.Pt(pane.Min.X, pane.Max.Y), Max: image.Pt(pane.Max.X, win.Y)},
	}
	for {
		ev, ok := gtx.Event(pointer.Filter{Target: &d.tag, Kinds: pointer.Press})
		if !ok {
			break
		}
		if e, ok := ev.(pointer.Event); ok && e.Kind == pointer.Press {
			d.v.setPreview(pfPreviewNone)
		}
	}
	for _, r := range parts {
		if r.Empty() {
			continue
		}
		fill(gtx, r, rgba(0x1d1c1d, 0.3))
		st := clip.Rect(r).Push(gtx.Ops)
		event.Op(gtx.Ops, &d.tag)
		st.Pop()
	}
	return layout.Dimensions{Size: win}
}

func (d *pfPreviewDim) Dismissed() bool { return d.done || d.v.preview == pfPreviewNone }

// Dismiss is Escape: Slack's keyup listener exits the preview.
func (d *pfPreviewDim) Dismiss() {
	d.done = true
	d.v.setPreview(pfPreviewNone)
}
