package huddleui

import (
	"context"
	"errors"
	"image"
	"log"
	"net/url"
	"time"

	"gioui.org/unit"

	"github.com/some-du6e/clack/internal/huddle"
	"github.com/some-du6e/clack/internal/taut"
	"github.com/some-du6e/clack/internal/ui/popups"
	"github.com/some-du6e/clack/internal/ui/theme"
)

// The huddle menus, as Slack's web client builds them (client-boot-shared
// on the public CDN, bv1-13):
//
//   - the channel header caret (HuddleChannelHeaderButton's options button,
//     "More Huddles options") opens HuddleButtonMenu (module uw9r): Start /
//     Join Huddle (HuddleButtonMenuItem, hidden while you're in it on this
//     device), Copy huddle link, and with the scheduled_huddles experiment
//     grouped under "Huddle" and "Schedule for later" headings with
//     Schedule Huddle (not in channels shared with other organizations,
//     not for frontline workers). Menu width "auto", position bottom,
//     offsetX 5, offsetY -5.
//   - the profile card's Huddle button is WithCallMenu (CXZJ) around
//     CallMenu (GPML, 305px, "Start a call"): HuddleButtonMenuItem for the
//     member, and Copy huddle link when your DM with them exists. Position
//     bottom, offsetY 10.
//
// Huddle-team call apps (Zoom and friends, "Video meetings" and "Phone"
// groups) and the Google Calendar "Schedule meeting" item
// (channel_meeting_invite) aren't in Clack.

// menuHost is what the menus need from the ui root beyond Host (the App
// implements it in huddle_wire.go).
type menuHost interface {
	// CopyText puts text on the clipboard (at the next frame).
	CopyText(s string)
	// ScheduleHuddle opens Slack's Schedule Huddle modal for channel.
	ScheduleHuddle(channel string)
}

// ScheduledHuddles reports whether the viewer has Slack's
// scheduled_huddles experiment (Schedule Huddle and the grouped menu).
func (c *Controller) ScheduledHuddles() bool {
	app := c.h.Core()
	return app != nil && app.Experiment("scheduled_huddles") == "on"
}

// huddleItemLabel is HuddleButtonMenuItem's label for channel: Start
// Huddle with nothing running, else Join Huddle, or Leave Huddle when
// you're in it here.
func (c *Controller) huddleItemLabel(channel string) string {
	if channel == "" {
		return nsHuddles.T("Start Huddle")
	}
	if c.In(channel) {
		return nsHuddles.T("Leave Huddle")
	}
	if _, live := c.Live(channel); live {
		return nsHuddles.T("Join Huddle")
	}
	return nsHuddles.T("Start Huddle")
}

// huddleItem is HuddleButtonMenuItem: leave when you're in channel's
// huddle, else start or join it (with the member when there's no DM yet:
// startHuddleByMemberId). entry is the startHuddle entry point.
func (c *Controller) huddleItem(channel, member, entry string) popups.MenuItem {
	return popups.MenuItem{Key: "huddle_button_menu_item", Label: c.huddleItemLabel(channel), Icon: "headphones", Do: func() {
		switch {
		case channel != "" && c.In(channel):
			c.Leave()
		case member != "":
			c.StartWithFrom(member, entry)
		case channel != "":
			c.Begin(channel, entry)
		}
	}}
}

func (c *Controller) copyItem(channel string) popups.MenuItem {
	return popups.MenuItem{Key: "copy-huddle-link", Label: nsHuddles.T("Copy huddle link"), Icon: "link", Do: func() { c.CopyLink(channel) }}
}

// MenuItems is the header caret's menu for channel (HuddleButtonMenu).
func (c *Controller) MenuItems(channel string) []popups.MenuItem {
	in := c.In(channel)
	other := !in && contains(c.headerPeople(channel), c.selfID())
	showHuddle := !in || other // isCurrentUserInHuddle && !isUserOnAnotherDevice hides it
	var items []popups.MenuItem
	if !c.ScheduledHuddles() {
		if showHuddle {
			items = append(items, c.huddleItem(channel, "", EntryHeaderMenu))
		}
		return append(items, c.copyItem(channel))
	}
	items = append(items, popups.MenuItem{Header: nsHuddles.T("Huddle")})
	if showHuddle {
		items = append(items, c.huddleItem(channel, "", EntryHeaderMenu))
	}
	items = append(items, c.copyItem(channel))
	if c.canSchedule(channel) {
		items = append(items, popups.Sep(), popups.MenuItem{Header: nsHuddles.T("Schedule for later")},
			popups.MenuItem{Key: "external-huddle", Label: nsHuddles.T("Schedule Huddle"), Icon: "calendar", Do: func() {
				if mh, ok := c.h.(menuHost); ok {
					mh.ScheduleHuddle(channel)
				}
			}})
	}
	return items
}

// canSchedule is ExternalHuddleMenuItem's condition (the experiment
// aside): not a channel shared with another organization.
func (c *Controller) canSchedule(channel string) bool {
	app := c.h.Core()
	if app == nil {
		return false
	}
	if cv, ok := app.Conv(channel); ok && cv.IsShared {
		return false
	}
	return true
}

// MemberMenuItems is the profile card's call menu for a person: the
// huddle item, and Copy huddle link when your DM with them exists.
func (c *Controller) MemberMenuItems(member string) []popups.MenuItem {
	return c.MemberMenuItemsFrom(member, EntryMemberCard)
}

// MemberMenuItemsFrom is MemberMenuItems for an entry point (the full
// profile's button is member_profile_huddle_button).
func (c *Controller) MemberMenuItemsFrom(member, entry string) []popups.MenuItem {
	dm := c.dmWith(member)
	items := []popups.MenuItem{c.huddleItem(dm, member, entry)}
	if dm != "" {
		items = append(items, c.copyItem(dm))
	}
	return items
}

// MenuWidth is a menu's width in dp for Slack's width "auto": the widest
// row (24px padding each side, the 20+8px icon column, 15px labels, 13px
// headings), at least .c-menu's min-width of 200px.
func MenuWidth(gtx C, th *theme.Theme, items []popups.MenuItem) float32 {
	w := 200
	for _, it := range items {
		var n int
		switch {
		case it.Header != "":
			n = th.Measure(gtx, 13, theme.Bold, it.Header)
		case it.Label != "":
			n = th.Measure(gtx, 15, theme.Regular, it.Label)
			if it.Icon != "" {
				n += gtx.Dp(28)
			}
		default:
			continue
		}
		if d := int(float32(n+gtx.Dp(48))/gtx.Metric.PxPerDp + .999); d > w {
			w = d
		}
	}
	return float32(w)
}

// MenuAnchor is the popups anchor that puts a w dp menu where Slack's
// MenuTrigger (position "bottom", offsetX 5, offsetY -5) puts the header
// caret's: centered on the caret (window coordinates), its top 5px above
// the caret's bottom, shifted 5px right.
func MenuAnchor(gtx C, caret image.Rectangle, w float32) image.Rectangle {
	wpx := gtx.Dp(unit.Dp(w))
	x := caret.Min.X + (caret.Dx()-wpx)/2 + gtx.Dp(5)
	return image.Rect(x, caret.Min.Y, x+wpx, caret.Max.Y-gtx.Dp(5))
}

// dmWith is your open DM with member ("" if there's none yet).
func (c *Controller) dmWith(member string) string {
	app := c.h.Core()
	if app == nil || member == "" {
		return ""
	}
	app.S.RLock()
	defer app.S.RUnlock()
	for id, cv := range app.S.Convs {
		if cv.IsIM && cv.User == member {
			return id
		}
	}
	return ""
}

type memberArgs struct {
	MemberID   string `json:"memberId"`
	EntryPoint string `json:"entryPoint,omitempty"`
}

// StartWith starts (or joins) a huddle in your DM with member, opening
// the DM first when there's none (Slack's startHuddleByMemberId:
// conversations.open, then startHuddle in it).
func (c *Controller) StartWith(member string) { c.StartWithFrom(member, EntryMemberCard) }

// StartWithFrom is StartWith for an entry point.
func (c *Controller) StartWithFrom(member, entry string) {
	app := c.h.Core()
	if app == nil || member == "" {
		return
	}
	tt := c.taut()
	go func() {
		ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
		defer cancel()
		_, err := taut.Dispatch(ctx, tt, "startHuddleByMemberId", memberArgs{MemberID: member, EntryPoint: entry},
			func(ctx context.Context, a memberArgs) (any, error) {
				ch := c.dmWith(a.MemberID)
				if ch == "" {
					cv, err := app.OpenDM(ctx, a.MemberID)
					if err != nil {
						return nil, err
					}
					if ch = cv.ID; ch == "" {
						return nil, errors.New("could not open conversation")
					}
				}
				c.Begin(ch, a.EntryPoint)
				return nil, nil
			})
		if err != nil {
			log.Printf("huddle: start with %s: %v", member, err)
			c.showErrModal(joinErrorModal(err))
		}
	}()
}

type copyLinkArgs struct {
	ChannelID string `json:"channelId"`
}

// Slack's copy toasts (copyHuddleLinkByChannelId).
const (
	linkCopied     = "Huddle link copied"
	linkCopyFailed = "Couldn’t copy huddle link. Try again."
)

// CopyLink is Slack's copyHuddleLinkByChannelId: rooms.getLink for the
// channel, the link on the clipboard, and "Huddle link copied" (or
// "Couldn’t copy huddle link. Try again.") for 3 seconds.
func (c *Controller) CopyLink(channel string) {
	if channel == "" {
		return
	}
	tt := c.taut()
	go func() {
		ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cancel()
		_, _ = taut.Dispatch(ctx, tt, "copyHuddleLinkByChannelId", copyLinkArgs{ChannelID: channel},
			func(ctx context.Context, a copyLinkArgs) (any, error) {
				link, err := c.huddleLink(ctx, a.ChannelID)
				mh, ok := c.h.(menuHost)
				if err != nil || link == "" || !ok {
					if err != nil {
						log.Printf("huddle: rooms.getLink %s: %v", a.ChannelID, err)
					}
					c.toastFor(nsHuddles.T(linkCopyFailed), "", 3000)
					return nil, err
				}
				mh.CopyText(link)
				c.toastFor(nsHuddles.T(linkCopied), "check", 3000)
				return nil, nil
			})
	}()
}

// huddleLink asks rooms.getLink (reason "copy") for channel's huddle link.
func (c *Controller) huddleLink(ctx context.Context, channel string) (string, error) {
	var client huddle.Caller
	if c.Client != nil {
		client = c.Client
	} else if app := c.h.Core(); app != nil {
		client = app.C
	}
	if client == nil {
		return "", errors.New("not signed in")
	}
	var res struct {
		Link string `json:"link"`
	}
	if err := client.Call(ctx, "rooms.getLink", url.Values{"channel_id": {channel}}, &res); err != nil {
		return "", err
	}
	return res.Link, nil
}
