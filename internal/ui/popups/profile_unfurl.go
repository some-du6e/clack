package popups

import (
	"image/color"
	"time"

	"gioui.org/layout"
	"gioui.org/widget"

	"github.com/some-du6e/clack/internal/core"
	"github.com/some-du6e/clack/internal/ui/theme"
)

// UserProfileUnfurl's parts (client-boot-shared texo), the pieces search's
// person card draws with, for the canvas's member link card
// (canvasview linkcard_member.go).

// ProfileUnfurlName is MemberProfileBaseEntity's name line: the real name
// in 15px Black at 22, its badges and presence dot, maxW wide; c (nil:
// none) makes it a link to the profile.
func ProfileUnfurlName(gtx layout.Context, h Host, th *theme.Theme, c *widget.Clickable, u core.User, maxW int) layout.Dimensions {
	return pfNameLine(gtx, h, th, c, u, pfRealName(u), 15, 22, 10, maxW)
}

// ProfileUnfurlAvatar is the member's avatar at size dp.
func ProfileUnfurlAvatar(gtx layout.Context, h Host, u core.User, size float32) layout.Dimensions {
	return avatar(gtx, h, pfAvatarURL(u, false), size)
}

// ProfileUnfurlHasClock: the member has a local time to show (a tz, or
// a tz offset).
func ProfileUnfurlHasClock(u core.User) bool { return pfHasClock(u) }

// ProfileUnfurlLocalTime is LocalTime's label: "{time} local time" and its
// relative variants.
func ProfileUnfurlLocalTime(h Host, u core.User, now time.Time) string {
	return pfLocalTimeLabel(h, u, now)
}

// ProfileUnfurlBanner is MemberProfileHoverBanner's label ("Workspace
// Owner", "Workspace Admin", "Deactivated account"; "" none).
func ProfileUnfurlBanner(h Host, u core.User) (label string, deactivated bool) {
	return pfRoleBanner(h, u)
}

// LayoutProfileUnfurlBanner draws that banner w wide over base.
func LayoutProfileUnfurlBanner(gtx layout.Context, th *theme.Theme, label string, deactivated bool, w int, base color.NRGBA) layout.Dimensions {
	return srBanner(gtx, th, label, deactivated, w, base)
}
