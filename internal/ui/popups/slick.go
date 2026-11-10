package popups

// Boundaries Slick plugins patch in popups (docs/PLUGINS-components.md §9,
// owner pop): the profile card and full profile (ProfileHoverCard,
// MemberProfileBaseEntity, Presence, LocalTimeRow, EditProfileFormPane,
// AppDetailsOverflow, Content, AboutPage), menus (Menu, MessageActionsMenu,
// ContextMenuTriggerWithKeyboardFocus), search (BaseSearch,
// SearchInputBox, SearchAutocomplete) and the GIF picker (GifPicker,
// GifListItem). Like the Taut ones in taut.go they cost nothing while no
// plugin patches the name.

import (
	"context"
	"image"
	"sync/atomic"
	"time"

	"gioui.org/layout"
	"gioui.org/op"

	"github.com/some-du6e/clack/internal/core"
	"github.com/some-du6e/clack/internal/taut"
	"github.com/some-du6e/clack/internal/ui/slackc"
	"github.com/some-du6e/clack/internal/ui/theme"
)

// ---- profile ----

// pfEditPaneFor is how long EditProfileFormPane stays mounted after "Edit"
// (Clack has no edit-profile form; the pane is an empty boundary that
// lives as long as the toast standing in for it, so plugins that track the
// form's mount, like Slick's Nicknames, see it open and close).
const pfEditPaneFor = 4 * time.Second

// pfEditProfile is Slack's "Edit profile": it dispatches the
// openEditProfileModal thunk (Nicknames wraps it) and, when the native
// runs, shows Clack's stand-in (a toast).
func pfEditProfile(h Host) {
	t := tautFor("openEditProfileModal")
	if t == nil {
		h.Toast(pfEditToast)
		return
	}
	var native atomic.Bool
	run(h, func(ctx context.Context) (struct{}, error) {
		return taut.Dispatch(ctx, t, "openEditProfileModal", struct{}{}, func(context.Context, struct{}) (struct{}, error) {
			native.Store(true)
			return struct{}{}, nil
		})
	}, func(_ struct{}, err error) {
		if err != nil || native.Load() {
			h.Toast(pfEditToast)
		}
	})
}

// editPane mounts the empty EditProfileFormPane while the edit surface is up.
func (v *ProfileView) editPane(gtx layout.Context) {
	if !gtx.Now.Before(v.editUntil) {
		return
	}
	id := v.id
	slackc.RenderLazy(gtx, "EditProfileFormPane", func() slackc.Props {
		return slackc.Props{"$key": "edit-profile:" + id, "memberId": id}
	}, pfNoNative)
	gtx.Execute(op.InvalidateCmd{At: v.editUntil})
}

// pfAppID is the app id of an app's bot user ("" for people).
func pfAppID(h Host, u core.User) string {
	if !u.IsBot {
		return ""
	}
	a := h.Core()
	if a == nil || a.S == nil {
		return ""
	}
	if su, ok := a.S.User(u.ID); ok && su != nil {
		return su.Profile.APIAppID
	}
	return ""
}

// pfAppMenuOpts puts an app's ⋮ menu inside AppDetailsOverflow{appId,
// appUserId} (Nicknames marks that subtree and appends "Set nickname…").
func pfAppMenuOpts(appID, userID string, o MenuOpts) MenuOpts {
	o.Within = "AppDetailsOverflow"
	o.WithinProps = slackc.Props{"appId": appID, "appUserId": userID}
	return o
}

// content is the full profile's body as Slack's tree: Content{children:
// the profile flexpane{uiComponentName, profileId, children: [the pane
// (data-qa member_profile_pane), AppDetailsOverflow on apps]}}.
// InstalledApps appends its block to the pane (people) or the flexpane
// (apps); both come back as extra children and draw below the profile.
func (v *ProfileView) content(gtx layout.Context, th *theme.Theme, cl *Colors, u core.User, self bool, w int) layout.Dimensions {
	app := pfAppID(v.h, u)
	pane := func(gtx layout.Context, p slackc.Props) layout.Dimensions {
		return v.pane(gtx, th, cl, u, self, w, app, p)
	}
	paneProps := func() slackc.Props {
		return slackc.Props{"$key": "profile-pane:" + u.ID, "data-qa": "member_profile_pane", "children": []any{}}
	}
	flexProps := func() slackc.Props {
		kind := "rimeto_member_profile_flexpane"
		kids := []any{slackc.Child("MemberProfilePane", paneProps(), pane)}
		if app != "" {
			kind = "app_profile_flexpane"
			kids = append(kids, slackc.Child("AppDetailsOverflow", slackc.Props{"appId": app, "appUserId": u.ID}, pfNoNative))
		}
		return slackc.Props{"$key": "profile-flexpane:" + u.ID, "uiComponentName": kind, "profileId": u.ID, "children": kids}
	}
	flex := func(gtx layout.Context, p slackc.Props) layout.Dimensions {
		return childrenOr(gtx, p, "MemberProfilePane", paneProps, pane)
	}
	return slackc.RenderLazy(gtx, "Content", func() slackc.Props {
		return slackc.Props{
			"$key":                "profile-content:" + u.ID,
			"isContentFullHeight": false,
			"width":               float64(w) / float64(max(gtx.Metric.PxPerDp, 1)),
			"children":            slackc.Child("MemberProfileFlexpane", flexProps(), flex),
		}
	}, func(gtx layout.Context, p slackc.Props) layout.Dimensions {
		return childrenOr(gtx, p, "MemberProfileFlexpane", flexProps, flex)
	})
}

// pane draws the profile body, then an app's AboutPage (an empty point:
// Clack has no app About tab; InstalledApps appends who installed the
// app), then any children a plugin added to the pane.
func (v *ProfileView) pane(gtx layout.Context, th *theme.Theme, cl *Colors, u core.User, self bool, w int, app string, p slackc.Props) layout.Dimensions {
	d := v.body(gtx, th, cl, u, true, self, w)
	y := d.Size.Y
	sub := func(gtx layout.Context) layout.Context {
		gtx.Constraints = layout.Constraints{Max: image.Pt(w, 1<<20)}
		return gtx
	}
	if app != "" {
		ad := at(gtx, image.Pt(0, y), func(gtx layout.Context) layout.Dimensions {
			return slackc.RenderLazy(sub(gtx), "AboutPage", func() slackc.Props {
				return slackc.Props{"$key": "about:" + app, "appId": app, "botUserId": u.ID}
			}, pfNoNative)
		})
		y += ad.Size.Y
	}
	if e, ok := slackc.PropChildren(p); ok {
		ed := at(gtx, image.Pt(0, y), func(gtx layout.Context) layout.Dimensions {
			return slackc.LayoutElement(sub(gtx), e)
		})
		y += ed.Size.Y
	}
	return layout.Dimensions{Size: image.Pt(w, y)}
}
