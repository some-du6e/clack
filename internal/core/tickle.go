package core

import (
	"encoding/json"
	"log"
	"sync/atomic"
	"time"
)

// The viewer's presence kept current while they use Clack, as Slack's
// client keeps it (build 5718bdb1b28b, desktop 4.53.276;
// docs/NOTIFY-RULES.md §10):
//
//   - maybeTickleMS (client-boot lWLC, "Ping the message server if
//     necessary") sends the socket {type: "tickle", reason} when the socket
//     is connected (isConnected), the viewer isn't manually away
//     (getMemberManualPresence) and nothing that counts as activity went
//     out over the socket for 19 s: a tickle, a typing indicator, a
//     user_typing (lastSocketActivity, which the socket manager's send
//     moves for those). One that fails because the socket closed is sent
//     once more when it's back ("will try again the next time we get
//     reconnected").
//   - Its reasons: the window gaining the focus (handleWindowFocusEvent),
//     a press of the mouse and of a key anywhere in it
//     (handleWindowMouseDownEvent, handleWindowKeyDownEvent), and the
//     desktop app's polling: every 10 s, while the system had input in the
//     last 20 s and the screen isn't locked (main process
//     systemIdleStatusEpic: getSystemIdleTime() <= 20 and
//     systemLockedStatus; tickleMessageServerEpic sends the webapp
//     TICKLE_MESSAGE_SERVER, which dispatches maybeTickleMS with reason
//     "polling"). ui/presence_activity.go feeds them.
//   - Without them the server's auto-away marked the viewer away while
//     they read in Clack: @here stopped reaching them (NotifyMention, the
//     sidebar's badge) and others saw them away.
//   - set_active: the modern apiCall adds it only for a SET_ACTIVE caller
//     (none in this build) or a caller's own set_active (admin search.team,
//     enterprise.teams.directory, enterprise.users.profile.set, the admin
//     terms of service), none of them a call Clack makes; the legacy
//     interop.api bridge that sets it by default has no caller.

// tickleEvery is maybeTickleMS's 19 s (tests shorten it).
var tickleEvery atomic.Int64

func init() { tickleEvery.Store(int64(19 * time.Second)) }

// Tickle is maybeTickleMS with reason ("focus", "mousedown", "keydown",
// "polling"). Any goroutine; it never blocks: the frame goes out aside.
func (a *App) Tickle(reason string) {
	r := a.RTM
	if r == nil || !r.Connected() || a.manuallyAway() {
		return
	}
	if !r.ClaimActivity(time.Duration(tickleEvery.Load())) {
		return
	}
	go a.sendTickle(reason, true)
}

// sendTickle writes the tickle; one that fails is kept for the socket's
// next connect when retry (the first try).
func (a *App) sendTickle(reason string, retry bool) {
	err := a.RTM.Send(map[string]any{"type": "tickle", "reason": reason})
	switch {
	case err == nil && !retry:
		log.Printf("rtm: the tickle that failed earlier went out (%s)", reason)
	case err == nil:
	case retry:
		log.Printf("rtm: sending a tickle failed (%v); it goes again once the socket is back", err)
		a.tickleAgain.Store(&reason)
	default:
		log.Printf("rtm: the tickle that failed earlier failed again: %v", err)
	}
}

// retryTickle sends the tickle that failed, once, as the socket comes
// back (OnState).
func (a *App) retryTickle() {
	if p := a.tickleAgain.Swap(nil); p != nil {
		go a.sendTickle(*p, false)
	}
}

// manuallyAway is getMemberManualPresence(currentUser) == away.
func (a *App) manuallyAway() bool {
	a.S.RLock()
	defer a.S.RUnlock()
	return a.S.Self.ManualPresence == "away"
}

// onManualPresence is manualPresenceChange (boot-render): the viewer set
// themselves away or back, in any client. updateCurrentMemberPresence
// sets the viewer's manual_presence and presence to it.
func (a *App) onManualPresence(raw json.RawMessage) {
	var e struct {
		Presence string `json:"presence"`
	}
	if json.Unmarshal(raw, &e) != nil || e.Presence != "away" && e.Presence != "active" {
		log.Printf("core: an unknown presence in a manual_presence_change: %q", e.Presence)
		return
	}
	self := a.S.SelfID()
	if self == "" {
		return
	}
	a.S.Lock()
	a.S.Self.ManualPresence, a.S.Self.Presence = e.Presence, e.Presence
	a.S.Presence[self] = e.Presence
	if u, ok := a.S.Users[self]; ok {
		u.ManualPresence, u.Presence = e.Presence, e.Presence
	}
	a.S.Unlock()
	a.presenceArrived()
	a.emit(Event{Type: "presence", Users: []string{self}, Presence: e.Presence})
}

// SetTickleEvery sets maybeTickleMS's 19 s throttle (tests) and returns
// the undo.
func SetTickleEvery(d time.Duration) func() {
	old := tickleEvery.Swap(int64(d))
	return func() { tickleEvery.Store(old) }
}
