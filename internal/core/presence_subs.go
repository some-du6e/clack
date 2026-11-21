package core

import (
	"log"
	"regexp"
	"time"
)

// Slack's presence manager (web client build 35b42de8b870, client-boot
// 4hlf): per team, a count for each member of the subscribers that want
// its presence. addMemberToPresenceSubscription counts one more
// (ignoring an empty id, USLACKBOT and anything looksLikeMemberId, kflk,
// rejects); removeMemberFromPresenceSubscription one fewer. A member's
// first subscriber and its last one leaving each send presence_sub,
// 100 ms after the last change (sendPresenceSubEvent: lodash debounce,
// trailing), when the socket is up. Clack's own presence set (presenceIDs:
// the viewer, DM partners, users recently shown) takes in every counted
// member, so presence_sub carries them as long as they are counted.

// presenceSubDelay is sendPresenceSubEvent's debounce.
const presenceSubDelay = 100 * time.Millisecond

// memberIDLike is looksLikeMemberId's pattern (kflk).
var memberIDLike = regexp.MustCompile(`^@?(U|W)[A-Z0-9]{8,}$`)

// looksLikeMemberID is kflk's looksLikeMemberId: Slack's special member
// ids (ZOnG: USLACKBOT, USEARCH, UCHANNELEXPERT), USLACK and UTBD, and
// anything shaped like a user id.
func looksLikeMemberID(id string) bool {
	switch id {
	case "USLACKBOT", "USEARCH", "UCHANNELEXPERT", "USLACK", "UTBD":
		return true
	}
	return id != "" && memberIDLike.MatchString(id)
}

// AddPresenceSubscription is Slack's addMemberToPresenceSubscription.
func (a *App) AddPresenceSubscription(id string) {
	if id == "" || id == "USLACKBOT" || !looksLikeMemberID(id) {
		return
	}
	a.mu.Lock()
	n := a.presSubs[id]
	a.presSubs[id] = n + 1
	a.mu.Unlock()
	if n == 0 {
		a.sendPresenceSubSoon()
	}
}

// RemovePresenceSubscription is Slack's removeMemberFromPresenceSubscription.
func (a *App) RemovePresenceSubscription(id string) {
	if id == "" {
		return
	}
	a.mu.Lock()
	n := a.presSubs[id]
	switch {
	case n == 0:
		a.mu.Unlock()
		return
	case n > 1:
		a.presSubs[id] = n - 1
		a.mu.Unlock()
		return
	}
	delete(a.presSubs, id)
	a.mu.Unlock()
	a.sendPresenceSubSoon()
}

// PresenceSubscriptions is the presence manager's counts
// (getPresenceSubscriptionByTeamId), a copy.
func (a *App) PresenceSubscriptions() map[string]int {
	a.mu.Lock()
	defer a.mu.Unlock()
	out := make(map[string]int, len(a.presSubs))
	for id, n := range a.presSubs {
		out[id] = n
	}
	return out
}

// sendPresenceSubSoon is sendPresenceSubEvent: while the socket is up,
// presence_sub with every member, once the changes have paused 100 ms.
func (a *App) sendPresenceSubSoon() {
	if a.RTM == nil || !a.RTM.Connected() {
		return // Slack only logs it: the next hello subscribes everyone
	}
	a.mu.Lock()
	defer a.mu.Unlock()
	if a.presSubTimer != nil {
		a.presSubTimer.Stop()
	}
	a.presSubTimer = time.AfterFunc(presenceSubDelay, func() {
		if err := a.RTM.SubscribePresence(a.presenceIDs(nil)); err != nil {
			log.Printf("rtm: presence_sub: %v", err)
		}
	})
}
