package huddleui

import (
	"context"
	"errors"
	"image"
	"log"
	"net/url"
	"time"

	"gioui.org/widget"

	"github.com/some-du6e/clack/internal/huddle"
	"github.com/some-du6e/clack/internal/taut"
	"github.com/some-du6e/clack/internal/ui/popups"
	"github.com/some-du6e/clack/internal/ui/theme"
)

// Leave options (gantry-v2-async-client-boot-shared, build d3de3a487b1c):
// HuddleToolbarLeaveButton (4tmx) is segmented when
// canEndExternalHuddleForAll (8ksj j) holds: the red Leave
// (.segmentedLeaveButtonMain, radius 8px 0 0 8px) and a 28px caret
// (.segmentedLeaveButtonCaret: radius 0 8px 8px 0, caret-up, caret-down
// while open, a 20px theme-otl-inv-pry line at its left; aria-label
// "Leave options", no tooltip). The caret opens HuddleLeaveOptionsMenu
// (OsUf) above it, one danger row: "End huddle for all"
// (huddle_toolbar__end_huddle_for_all_menu_item), which runs
// endExternalHuddleForAll: huddles.external.end {channel_id} (PdsW
// huddlesExternalEndFetcher, reason end-external-huddle-for-all), then
// stopHuddle; when the call fails, the toast "Can’t end the huddle for
// everyone. Try again." for 10 s (huddleToastType
// end_huddle_for_all_failed) and you stay. In the compact window Slack
// shows the menu as a full-window view with a "Leave options" header;
// Clack opens it above the caret like the window's other toolbar menus.

const extHuddles = "external_huddles" // pSxm a_: isExternalHuddlesEnabled

// leaveCaretW is .segmentedLeaveButtonCaret's width (and what the toolbar
// adds to the screen share button's minimum width, ek).
const leaveCaretW = 28

// canEndForAll is canEndExternalHuddleForAll (8ksj j): external huddles on
// (pSxm a_), you aren't a guest (QLH5 DM, isCurrentUserRestricted), an
// external guest is in the huddle (dWSn O: a remote peer whose member is
// is_restricted with restricted_user_type external_guest, 7zi0 I), and
// you're the huddle's host: the calendar huddle's host (AUOM ZU, which
// Clack has no calendar huddles to set) or else the channel's creator
// (Opi6 getChannelCreator).
func (c *Controller) canEndForAll(st huddle.State) bool {
	app := c.h.Core()
	if app == nil || !c.on(extHuddles) || st.Channel == "" {
		return false
	}
	me := c.selfID()
	if u, ok := app.S.User(me); ok && u.IsRestricted {
		return false
	}
	guest := false
	for _, p := range st.Participants {
		if p.Self || p.UserID == me {
			continue
		}
		if u, ok := app.S.User(p.UserID); ok && u.IsRestricted && u.RestrictedUserType == "external_guest" {
			guest = true
			break
		}
	}
	if !guest {
		return false
	}
	conv, ok := app.Conv(st.Channel)
	return ok && conv.Creator != "" && conv.Creator == me
}

// leaveOptionsMenu is HuddleLeaveOptionsMenu's row.
func (c *Controller) leaveOptionsMenu(s *huddle.Session) []popups.MenuItem {
	return []popups.MenuItem{{Key: "end-huddle-for-all", Label: nsHuddles.T("End huddle for all"), Danger: true,
		Do: func() { go c.EndForAll(s) }}}
}

// EndForAll is endExternalHuddleForAll ("End an external huddle for every
// participant"): the same checks, huddles.external.end, then stopHuddle.
// Off the UI goroutine.
func (c *Controller) EndForAll(s *huddle.Session) {
	if s == nil {
		return
	}
	st := s.State()
	if !c.on(extHuddles) {
		return
	}
	if app := c.h.Core(); app != nil {
		if u, ok := app.S.User(c.selfID()); ok && u.IsRestricted {
			log.Printf("huddle: not ending the huddle for all: the current user is a guest")
			return
		}
	}
	ch := st.Channel
	if ch == "" {
		log.Printf("huddle: not ending the huddle for all: no active huddle channel")
		return
	}
	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()
	_, err := taut.Dispatch(ctx, c.taut(), "endExternalHuddleForAll", huddleArgs{HuddleID: s.RoomID(), ChannelID: ch},
		func(ctx context.Context, a huddleArgs) (any, error) {
			cl := c.client()
			if cl == nil {
				return nil, errors.New("not signed in")
			}
			var res struct{}
			return nil, cl.Call(ctx, "huddles.external.end", url.Values{"channel_id": {a.ChannelID}}, &res)
		})
	if err != nil {
		log.Printf("huddle: ending the huddle for all failed: %v", err)
		c.toastFor(nsHuddles.T("Can’t end the huddle for everyone. Try again."), "", 10000)
		return
	}
	c.leave(s)
}

// toggleLeaveOptions opens (or closes) Leave options above its caret.
func (w *window) toggleLeaveOptions(s *huddle.Session) {
	if w.leaveMenu != nil {
		w.leaveMenu.Dismiss()
		w.leaveMenu = nil
		return
	}
	w.closeMenus()
	anchor := w.leaveBtn.Add(image.Pt(0, w.barTop))
	w.leaveMenu = popups.NewMenuOpts(winHost{w}, w.c.leaveOptionsMenu(s), popups.MenuOpts{Anchor: anchor, Side: popups.Above,
		MenuClass: "p-huddle_more_actions_menu"})
}

// leaveCaret draws .segmentedLeaveButtonCaret in sz.
func leaveCaret(gtx C, cl *widget.Clickable, sz image.Point, open bool) D {
	return button(gtx, cl, sz, func(gtx C, hov bool) {
		bg := hudLeave
		if hov {
			bg = hudLeaveHov
		}
		segFill(gtx, image.Rectangle{Max: sz}, segCaret, gtx.Dp(8), bg)
	}, func(gtx C) D {
		// :before, border-left 1px theme-otl-inv-pry, 20px tall
		theme.Fill(gtx, image.Rect(0, (sz.Y-gtx.Dp(20))/2, max(1, gtx.Dp(1)), (sz.Y+gtx.Dp(20))/2), segDivider)
		icon := "caret-up"
		if open {
			icon = "caret-down"
		}
		iconAt(gtx, sz, icon, 20, hudWhite)
		return D{}
	})
}
