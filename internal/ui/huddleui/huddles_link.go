package huddleui

import (
	"slices"
	"sort"
	"time"

	"github.com/some-du6e/clack/internal/ui/theme"
)

// The sidebar's Huddles link, as Slack's client draws it (build
// fa938a086569):
//
//   - Its page config (boot-shared dQqU, the Huddles page's sidebar
//     entry): hasUnreads and badgeCount (+!!) are getActiveInviteForCurrentTeam
//     (b0mi jP), so an invite showing makes the link bold
//     (p-channel_sidebar__link--unread, not under focus mode's
//     isSilentSidebarActive) and gives it --has-badge's 6px right padding.
//     The link's own mention badge isn't drawn: its renderLinkContent
//     ignores the badge it's handed.
//   - renderLinkContent is HuddlesSidebarLink (boot-shared YCI0 d):
//     .sidebarLink__w8QIz (flex, gap 8px, width 100%) holds the icon, the
//     name (margin-right auto) and, when getChannelIdForHuddleLinkFacepile
//     (b0mi Pu) names a channel, .facepile__RJWDO (flex, min-width 80px,
//     20px tall). Inside it, absolutely at its right edge, either the
//     count badge (Badge with the unread invites' count, class
//     huddleBadge__PcXv3: theme-base-imp / theme-content-imp, the c-badge
//     pill) or the channel's HuddleAvatarStack (qgIY i: maxMembers 3,
//     excludeIcon, excludeMemberOverflowCount, withTooltip,
//     includeChannelNameInTooltip; nothing when nobody's in it, and
//     hidden while your huddle there starts or ends).
//   - The badge replaces the faces only under huddles_link_v2 (pSxm $A)
//     and with an unread invite for this team (M5rs WG: clientHuddles.
//     unreadInvites with this teamId).
//   - getChannelIdForHuddleLinkFacepile: the active invite's channel
//     (this team's), else getChannelIdWithActiveHuddle over every huddle
//     with people in it (eS: huddles.activeChannels, your own read from
//     the client store).
//   - unreadInvites (ZRDf): upsertInvite (every huddle_invite that isn't
//     to your huddle or to the invite already showing) adds {invite,
//     receivedTimestampMs}; clearInviteForChannelId drops the channel's;
//     markInvitesSeenForTeam drops this team's received up to markBeforeMs.
//   - useMarkHuddlesNotifications (YCI0 m), run by the Huddles page
//     (Igs2): with huddles_link_v2 on and unread invites, any call it
//     hasn't seen since it mounted makes it dispatch markInvitesReadOnDelay
//     (delay 600): 600 ms on, markInvitesSeenForTeam with markBeforeMs =
//     now − 600, so invites that came in during the wait stay.
//
// Not drawn: the "New" tag and its glow (tD6Z q, /48f c: external
// huddles' awareness, which Clack has no external huddles for).

// ExpHuddlesLinkV2 is pSxm $A.
const ExpHuddlesLinkV2 = "huddles_link_v2"

// markInvitesDelay is useMarkHuddlesNotifications' delay.
const markInvitesDelay = 600 * time.Millisecond

// linkMaxFaces is the link's HuddleAvatarStack maxMembers.
const linkMaxFaces = 3

// linkFacepileW is .facepile__RJWDO's min-width; its children are
// absolutely placed, so it's the box's width.
const linkFacepileW = 80

// unreadInvite is one of clientHuddles.unreadInvites.
type unreadInvite struct {
	Team, Channel, Call string
	At                  time.Time // receivedTimestampMs
}

// unreadInvitesLocked is WG: team's unread invites. c.mu held.
func (c *Controller) unreadInvitesLocked(team string) []unreadInvite {
	var out []unreadInvite
	for _, u := range c.unread {
		if u.Team == team {
			out = append(out, u)
		}
	}
	return out
}

// UnreadInvites is how many unread huddle invites this team has (WG's
// length). Any goroutine.
func (c *Controller) UnreadInvites() int {
	team := c.teamID()
	c.mu.Lock()
	defer c.mu.Unlock()
	return len(c.unreadInvitesLocked(team))
}

// markInvitesSeen is markInvitesSeenForTeam.
func (c *Controller) markInvitesSeen(team string, before time.Time) {
	c.mu.Lock()
	n := len(c.unread)
	c.unread = slices.DeleteFunc(c.unread, func(u unreadInvite) bool { return u.Team == team && !u.At.After(before) })
	dropped := len(c.unread) != n
	c.mu.Unlock()
	if dropped {
		c.changed()
	}
}

// HuddlesPage runs the Huddles page's useMarkHuddlesNotifications each
// frame: shown is whether the page is up (false forgets the calls it
// saw, as its unmount does). Main window goroutine.
func (c *Controller) HuddlesPage(shown bool) {
	if !shown {
		c.mu.Lock()
		c.pageSeen = nil
		c.mu.Unlock()
		return
	}
	on := c.on(ExpHuddlesLinkV2)
	team := c.teamID()
	c.mu.Lock()
	if c.pageSeen == nil {
		c.pageSeen = map[string]bool{}
	}
	if team == "" || !on {
		c.mu.Unlock()
		return
	}
	fresh := false
	for _, u := range c.unreadInvitesLocked(team) {
		if !c.pageSeen[u.Call] {
			fresh = true
		}
		c.pageSeen[u.Call] = true
	}
	c.mu.Unlock()
	if fresh { // markInvitesReadOnDelay
		time.AfterFunc(markInvitesDelay, func() { c.markInvitesSeen(team, time.Now().Add(-markInvitesDelay)) })
	}
}

// HuddleLink is what the sidebar's Huddles link shows.
type HuddleLink struct {
	// Unread is hasUnreads / badgeCount: an invite is showing (bold,
	// --has-badge).
	Unread bool
	// Channel is getChannelIdForHuddleLinkFacepile: the facepile box is
	// there when it's set.
	Channel string
	// Badge is the unread invites' count when it replaces the faces
	// (huddles_link_v2), else 0.
	Badge int
}

// Link is the Huddles link's state. Main window goroutine.
func (c *Controller) Link() HuddleLink {
	on := c.on(ExpHuddlesLinkV2)
	team := c.teamID()
	c.mu.Lock()
	inv := c.inv
	n := len(c.unreadInvitesLocked(team))
	c.mu.Unlock()
	l := HuddleLink{Unread: inv != nil}
	if inv != nil {
		l.Channel = inv.Channel // getActiveInviteForCurrentTeam
	} else {
		l.Channel = c.withActiveHuddle(c.activeHuddles())
	}
	if on && n > 0 {
		l.Badge = n
	}
	return l
}

// activeHuddles is eS: the channels whose huddle has people in it, yours
// included, by date_start (Slack keeps huddles.activeChannels' insertion
// order; Clack's live rooms are a map, so ties fall to the channel id).
func (c *Controller) activeHuddles() []string {
	var have []string
	c.mu.Lock()
	for ch, r := range c.live {
		if len(r.Participants) > 0 {
			have = append(have, ch)
		}
	}
	if c.s != nil && !slices.Contains(have, c.s.Channel()) {
		have = append(have, c.s.Channel())
	}
	start := map[string]int64{}
	for _, ch := range have {
		start[ch] = c.live[ch].Start
	}
	c.mu.Unlock()
	have = slices.DeleteFunc(have, func(ch string) bool { return len(c.activeMembers(ch)) == 0 })
	sort.SliceStable(have, func(i, j int) bool {
		if start[have[i]] != start[have[j]] {
			return start[have[i]] < start[have[j]]
		}
		return have[i] < have[j]
	})
	return have
}

// LinkFacepileWidth is .facepile__RJWDO's width.
func LinkFacepileWidth(gtx C) int { return gtx.Dp(linkFacepileW) }

// LinkFaces draws the Huddles link's HuddleAvatarStack for ch ending at
// right (row coordinates, rowH tall) with HuddleChannelTooltip (the
// channel's name in it), and returns its left edge (right when nothing
// shows). Main window goroutine.
func (c *Controller) LinkFaces(gtx C, th *theme.Theme, ch string, right, rowH int) int {
	t := &c.hdrState("\x00huddles-link").sbFaces
	people := c.activeMembers(ch)
	if _, live := c.Live(ch); !live && !c.In(ch) || len(people) == 0 || c.transitioning(ch) {
		*t = sbTip{}
		return right
	}
	return c.sbStack(gtx, th, t, ch, people, linkMaxFaces, right, rowH, true)
}

// LinkFacesHovered reports whether the pointer is over the link's faces.
func (c *Controller) LinkFacesHovered(gtx C) bool {
	hs := c.hdr["\x00huddles-link"]
	return hs != nil && hs.sbFaces.hover.Update(gtx.Source)
}
