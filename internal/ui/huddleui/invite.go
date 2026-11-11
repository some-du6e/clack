package huddleui

import (
	"context"
	"encoding/json"
	"log"
	"net/url"
	"strings"
	"time"

	"github.com/some-du6e/clack/internal/huddle"
	"github.com/some-du6e/clack/internal/taut"
)

// Incoming huddle invites, as Slack's client handles them (bv1-13):
//
//   - huddle_invite {channel_id, call_id, sender_user_id, team_id,
//     free_willy} (huddleInviteRtmHandler, gantry-v2-async-client-boot-
//     render) is ignored for the huddle you're in and for the invite
//     already showing; otherwise it becomes the active invite (upsertInvite).
//   - The invite window (HuddleInviteWindowMounter → openHuddleInviteWindow,
//     render ce74): a separate small window, 380×280, "Slack - Huddle",
//     with the "Invite to huddle" title bar, the facepile, "{sender}" or
//     "{sender} in #channel", and three round buttons: Decline, Be there
//     soon, Join. It plays the invite sound once (playHuddleInviteSound:
//     the huddle_invite_sound_v2 pref's file, unless mute_huddle_sounds or
//     Do Not Disturb) unless you're in a huddle already.
//   - Under Do Not Disturb there is no window and no sound: the invite is a
//     card at the bottom of the sidebar instead (HuddleInvite, client-boot
//     FAXx: "Join huddle with {sender}" or "{sender} is inviting you to a
//     huddle in #channel", Join / Be there soon / Decline). Closing the
//     window without answering also leaves the card
//     (canRenderActiveInviteCardInSidebar).
//   - The OS notification (showHuddleNotification) shows when a huddle
//     notification may (shouldShowNotificationForHuddle: not in DND, the
//     conversation known and not muted, desktop notifications not
//     "nothing"): "{sender} is inviting you to a huddle [in #channel | with
//     …]", silent (the window rings), with Join and Decline.
//   - 30 s later (handleHuddleMissedCallNotification), if that invite is
//     still the active one and you're still "pending", a second
//     notification: "Huddle missed", "{sender} invited you to a huddle"
//     (only under huddles_missed_notification).
//   - Decline and Be there soon are respondToHuddleInvite: rooms.inviteResponse
//     {response: decline | joining_soon, channel_id, room_id}. Decline
//     clears the invite; Be there soon keeps it, says "Join when you're
//     ready!" and drops its button. Join is startHuddle (Begin) with
//     entryPoint huddle_invite_window (sidebar_huddle_invite from the card).
//   - The invite goes away on huddle_invite_cancel, when the room's
//     pending_invitees no longer list you (the ring timed out on Slack's
//     side), when the huddle empties or ends, and when you answer it.

// Slack's invite entry points (DF8l's su enum).
const (
	EntryInviteWindow  = "huddle_invite_window"
	EntrySidebarInvite = "sidebar_huddle_invite"
)

// inviteMissedAfter is handleHuddleMissedCallNotification's delay.
const inviteMissedAfter = 30 * time.Second

// incoming is the active invite (clientHuddles.activeInvite). Guarded by
// Controller.mu.
type incoming struct {
	Channel, Call, Sender, Team string
	At                          time.Time
	status                      string // your status in the room: pending (default), joining_soon
	window                      bool   // the invite window is up
	card                        bool   // the sidebar card shows
	notified                    bool   // the OS notification showed
	busy                        string // a response in flight
	note                        string // the window's toast
	noteErr                     bool
	noteAt                      time.Time
	timer                       *time.Timer
}

// IncomingInvite is the active invite as tests and the ui root see it.
type IncomingInvite struct {
	Channel, Call, Sender string
	Window, Card          bool // the invite window / the sidebar card
	JoiningSoon           bool
	Note                  string
}

// Incoming returns the active invite, if any. Safe from any goroutine.
func (c *Controller) Incoming() (IncomingInvite, bool) {
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.inv == nil {
		return IncomingInvite{}, false
	}
	i := c.inv
	return IncomingInvite{Channel: i.Channel, Call: i.Call, Sender: i.Sender, Window: i.window, Card: i.card,
		JoiningSoon: i.status == "joining_soon", Note: i.note}, true
}

// Notification is a desktop notification with Slack's action buttons.
type Notification struct {
	Title, Body string
	// PlainBody replaces Body when notifications hide message text
	// (shouldHideNotificationText).
	PlainBody string
	Actions   []NotifyAction
}

// NotifyAction is one of a notification's buttons.
type NotifyAction struct {
	Key, Label string
	Do         func()
}

// notifyHost is the ui root's desktop notifications.
type notifyHost interface{ NotifyActions(n Notification) }

func (c *Controller) notify(n Notification) {
	if h, ok := c.h.(notifyHost); ok {
		h.NotifyActions(n)
		return
	}
	if h, ok := c.h.(interface{ Notify(title, body string) }); ok {
		h.Notify(n.Title, n.Body)
	}
}

type inviteEvent struct {
	Channel string `json:"channel_id"`
	Call    string `json:"call_id"`
	Sender  string `json:"sender_user_id"`
	Team    string `json:"team_id"`
}

// onInvite is huddleInviteRtmHandler.
func (c *Controller) onInvite(raw json.RawMessage) {
	var ev inviteEvent
	if json.Unmarshal(raw, &ev) != nil || ev.Channel == "" || ev.Call == "" {
		return
	}
	c.mu.Lock()
	if c.s != nil && c.s.Channel() == ev.Channel && c.s.RoomID() == ev.Call {
		c.mu.Unlock()
		log.Printf("huddle: ignoring invite to active huddle")
		return
	}
	if c.joining == ev.Channel {
		c.mu.Unlock()
		return
	}
	if i := c.inv; i != nil && i.Channel == ev.Channel && i.Call == ev.Call {
		c.mu.Unlock()
		log.Printf("huddle: ignoring invite to active invite")
		return
	}
	old := c.inv
	inv := &incoming{Channel: ev.Channel, Call: ev.Call, Sender: ev.Sender, Team: ev.Team, At: time.Now(), status: "pending"}
	c.inv = inv
	c.gen++
	c.mu.Unlock()
	if old != nil {
		c.dropInvite(old)
	}
	go c.presentInvite(inv)
}

// presentInvite shows a new invite: window and sound (or the sidebar card
// under DND), and the notification.
func (c *Controller) presentInvite(inv *incoming) {
	app := c.h.Core()
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	dnd := false
	if app != nil {
		app.EnsureUsers(ctx, []string{inv.Sender})
		dnd = app.DNDActive(ctx)
	}
	c.mu.Lock()
	if c.inv != inv {
		c.mu.Unlock()
		return
	}
	inHuddle := c.s != nil
	if dnd {
		inv.card = true // HuddleInviteWindowMounter: no window under DND
	} else {
		inv.window = true
	}
	c.gen++
	c.mu.Unlock()

	if !dnd {
		c.openInviteWindow(inv)
		if !inHuddle {
			c.ringInvite()
		}
	}
	if c.mayNotifyHuddle(inv.Channel, dnd) {
		c.mu.Lock()
		inv.notified = true
		c.mu.Unlock()
		c.notify(c.inviteNotification(inv))
		after := c.MissedAfter
		if after <= 0 {
			after = inviteMissedAfter
		}
		t := time.AfterFunc(after, func() { c.inviteMissed(inv) })
		c.mu.Lock()
		inv.timer = t
		c.mu.Unlock()
	}
	c.changed()
}

// mayNotifyHuddle is shouldShowNotificationForHuddle == 0.
func (c *Controller) mayNotifyHuddle(ch string, dnd bool) bool {
	app := c.h.Core()
	if app == nil || dnd {
		return false
	}
	conv, ok := app.Conv(ch)
	if !ok || conv.Muted {
		return false
	}
	p := app.Prefs()
	level := p.GlobalDesktop
	if o := p.ChannelNotify[ch].Desktop; o != "" && o != "default" {
		level = o
	}
	return level != "nothing"
}

// inviteNotification is sharedHuddleNotificationOptions +
// desktopHuddleNotificationOptions.
func (c *Controller) inviteNotification(inv *incoming) Notification {
	who := c.displayName(inv.Sender)
	plain := who + " is inviting you to a huddle"
	body := plain
	if app := c.h.Core(); app != nil {
		if conv, ok := app.Conv(inv.Channel); ok {
			switch conv.Kind {
			case "mpim":
				var others []string
				self := c.selfID()
				for _, m := range conv.Members {
					if m != inv.Sender && m != self {
						if n := c.displayName(m); n != "" {
							others = append(others, n)
						}
					}
				}
				if len(others) > 0 {
					body = plain + " with " + joinNames(others)
				}
			case "im":
			default:
				body = plain + " in " + c.channelTitle(inv.Channel)
			}
		}
	}
	ch := inv.Channel
	return Notification{Title: c.workspaceName(), Body: body, PlainBody: plain, Actions: []NotifyAction{
		{Key: "join", Label: "Join", Do: func() { c.post(func() { c.joinInviteIn(ch, "") }) }},
		{Key: "decline", Label: "Decline", Do: func() { c.post(func() { c.respondInviteIn(ch, "decline") }) }},
	}}
}

// inviteMissed is handleHuddleMissedCallNotification, 30 s on.
func (c *Controller) inviteMissed(inv *incoming) {
	c.mu.Lock()
	cur := c.inv
	if cur != nil && cur != inv {
		c.mu.Unlock()
		return // another invite came in since
	}
	pending := cur == inv && inv.status == "pending"
	c.mu.Unlock()
	// (0,de.Z0)(r) && "pending" === c: only under huddles_missed_notification
	if !pending || !c.on(ExpMissedNotification) {
		return
	}
	title, body := "Huddle missed", c.displayName(inv.Sender)+" invited you to a huddle"
	if app := c.h.Core(); app != nil {
		if conv, ok := app.Conv(inv.Channel); ok && conv.Kind == "mpim" {
			body = c.displayName(inv.Sender) + " invited " + c.channelTitle(inv.Channel) + " to a huddle"
		}
	}
	ch := inv.Channel
	c.notify(Notification{Title: title, Body: body, PlainBody: body, Actions: []NotifyAction{
		{Key: "view", Label: "View", Do: func() { c.post(func() { c.h.Navigate("c/" + ch) }) }},
	}})
}

// clearInvite is clearInviteForChannelId: the window closes, the card
// goes, the sound stops.
func (c *Controller) clearInvite(ch string) {
	c.mu.Lock()
	inv := c.inv
	if inv == nil || (ch != "" && inv.Channel != ch) {
		c.mu.Unlock()
		return
	}
	c.inv = nil
	c.gen++
	c.mu.Unlock()
	c.dropInvite(inv)
	c.changed()
}

func (c *Controller) dropInvite(inv *incoming) {
	c.mu.Lock()
	if inv.timer != nil {
		inv.timer.Stop()
	}
	w := c.iwin
	if w != nil && w.inv == inv {
		c.iwin = nil
	} else {
		w = nil
	}
	c.mu.Unlock()
	c.stopRing()
	if w != nil {
		w.close()
	}
}

// inviteRoomEvent applies sh_room_* to the active invite
// (updateHuddleRtmHandler's invite half).
func (c *Controller) inviteRoomEvent(name string, raw json.RawMessage) {
	var ev struct {
		Huddle struct {
			ChannelID string `json:"channel_id"`
		} `json:"huddle"`
		Room struct {
			ID           string          `json:"id"`
			Channels     []string        `json:"channels"`
			HasEnded     bool            `json:"has_ended"`
			DateEnd      int64           `json:"date_end"`
			Participants json.RawMessage `json:"participants"`
			Pending      json.RawMessage `json:"pending_invitees"`
		} `json:"room"`
	}
	if json.Unmarshal(raw, &ev) != nil {
		return
	}
	ch := ev.Huddle.ChannelID
	if ch == "" && len(ev.Room.Channels) > 0 {
		ch = ev.Room.Channels[0]
	}
	c.mu.Lock()
	inv := c.inv
	if inv == nil || ch != inv.Channel || (ev.Room.ID != "" && ev.Room.ID != inv.Call) {
		c.mu.Unlock()
		return
	}
	self := c.selfID()
	clear := false
	switch {
	case ev.Room.HasEnded || ev.Room.DateEnd > 0:
		clear = true
	case name == "sh_room_leave" && ev.Room.Participants != nil && len(ids(ev.Room.Participants)) == 0:
		clear = true
	case name == "sh_room_update" && len(ev.Room.Pending) > 0 && string(ev.Room.Pending) != "null":
		st := pendingStatus(ev.Room.Pending, self)
		switch {
		case st == "joining_soon":
			inv.status = "joining_soon"
		case st != "":
			inv.status = st
		case inv.status != "joining_soon":
			clear = true // no longer rung: Slack stopped ringing you
		}
	}
	c.mu.Unlock()
	if clear {
		c.clearInvite(inv.Channel)
	}
}

// pendingStatus is user's entry in a room's pending_invitees (an object
// keyed by user, or a list of users): its status, "pending" for a
// non-string entry, "" when they aren't listed.
func pendingStatus(raw json.RawMessage, user string) string {
	var byUser map[string]json.RawMessage
	if json.Unmarshal(raw, &byUser) == nil {
		v, ok := byUser[user]
		if !ok {
			return ""
		}
		var st string
		if json.Unmarshal(v, &st) == nil && st != "" {
			return st
		}
		return "pending"
	}
	for _, u := range ids(raw) {
		if u == user {
			return "pending"
		}
	}
	return ""
}

// JoinInvite answers the active invite with Join (from the window, the
// card or the notification): startHuddle, through Begin.
func (c *Controller) JoinInvite(entry string) { c.joinInviteIn("", entry) }

func (c *Controller) joinInviteIn(ch, entry string) {
	c.mu.Lock()
	inv := c.inv
	c.mu.Unlock()
	if inv == nil || (ch != "" && inv.Channel != ch) {
		return
	}
	c.clearInvite(inv.Channel)
	c.Begin(inv.Channel, entry)
}

// DeclineInvite and JoiningSoon answer the active invite
// (respondToHuddleInvite).
func (c *Controller) DeclineInvite() { c.respondInviteIn("", "decline") }

// JoiningSoon is Be there soon.
func (c *Controller) JoiningSoon() { c.respondInviteIn("", "joining_soon") }

type respondArgs struct {
	ChannelID      string `json:"channelId"`
	InviteResponse string `json:"inviteResponse"`
	CallID         string `json:"callId"`
}

func (c *Controller) client() huddle.Caller {
	if c.Client != nil {
		return c.Client
	}
	if app := c.h.Core(); app != nil {
		return app.C
	}
	return nil
}

func (c *Controller) respondInviteIn(ch, resp string) {
	c.mu.Lock()
	inv := c.inv
	if inv == nil || (ch != "" && inv.Channel != ch) || inv.busy != "" {
		c.mu.Unlock()
		return
	}
	inv.busy = resp
	c.gen++
	c.mu.Unlock()
	c.stopRing()
	c.changed()
	cl, tt := c.client(), c.taut()
	go func() {
		ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
		defer cancel()
		_, err := taut.Dispatch(ctx, tt, "respondToHuddleInvite", respondArgs{ChannelID: inv.Channel, InviteResponse: resp, CallID: inv.Call},
			func(ctx context.Context, a respondArgs) (any, error) {
				if a.InviteResponse == "accept" || cl == nil {
					return nil, nil
				}
				p := url.Values{"response": {a.InviteResponse}, "channel_id": {a.ChannelID}, "room_id": {a.CallID}}
				return nil, cl.Call(ctx, "rooms.inviteResponse", p, nil)
			})
		c.mu.Lock()
		inv.busy = ""
		live := c.inv == inv
		if live {
			switch {
			case err != nil:
				inv.note, inv.noteErr, inv.noteAt = "Something went wrong responding to the invite.", true, time.Now()
			case resp == "joining_soon":
				inv.status = "joining_soon"
				inv.note, inv.noteErr, inv.noteAt = "Join when you’re ready!", false, time.Now()
			}
		}
		c.gen++
		c.mu.Unlock()
		if err != nil {
			log.Printf("huddle: rooms.inviteResponse %s: %v", resp, err)
		} else if resp == "decline" {
			c.clearInvite(inv.Channel)
		}
		c.changed()
	}()
}

// inviteWindowClosed is the window going away without an answer: the
// sidebar card takes over (beforeunload → canRenderActiveInviteCardInSidebar).
func (c *Controller) inviteWindowClosed(w *inviteWindow) {
	c.mu.Lock()
	if c.iwin == w {
		c.iwin = nil
	}
	if c.inv == w.inv {
		c.inv.window = false
		c.inv.card = true
	}
	c.gen++
	c.mu.Unlock()
	c.stopRing()
	c.h.Invalidate()
}

// ---- helpers ----

// displayName is how Slack names someone in an invite (getMemberDisplayName):
// their display name, else their full name.
func (c *Controller) displayName(id string) string {
	if u, ok := c.user(id); ok {
		if u.Name != "" { // core's Name is the display name as Slack shows it
			return u.Name
		}
		if u.RealName != "" {
			return u.RealName
		}
	}
	return "Someone"
}

func (c *Controller) workspaceName() string {
	if app := c.h.Core(); app != nil {
		app.S.RLock()
		n := app.S.TeamName
		app.S.RUnlock()
		if n != "" {
			return n
		}
	}
	return AppName
}

// joinNames is Slack's list format: "A", "A and B", "A, B, and C".
func joinNames(n []string) string {
	switch len(n) {
	case 0:
		return ""
	case 1:
		return n[0]
	case 2:
		return n[0] + " and " + n[1]
	}
	return strings.Join(n[:len(n)-1], ", ") + ", and " + n[len(n)-1]
}
