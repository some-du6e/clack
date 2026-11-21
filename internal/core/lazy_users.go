package core

import (
	"regexp"
	"sync/atomic"

	"github.com/some-du6e/clack/internal/slack"
	"github.com/some-du6e/clack/internal/store"
)

// Slack fetches the members a loaded page mentions only as its rows
// render: the history and replies fetchers (client-boot QJKZ fetchHistory
// and fetchReplies, both with no_user_profile) hand the messages to
// processIncomingMessages (udbq), which upserts messages and never asks
// for a member, and the legacy API's ensureModelForLegacy skips the member
// fetch for activity.mentions, conversations.history, pins.list and
// unread.history (shouldSkipMemberFetch). What asks is getMemberById
// (gIKW) in the component that shows the member: the sender, a mention
// (MemberSlug), the reply bar's avatars, a pin's "Pinned by", and a
// reaction's users only in its tooltip (ReactionTip Qvn6's
// TooltipMemberName NZA6), each a miss for the UnknownFetchManager
// (Gx04, members.go). ensureMembersArePresent itself is called only by
// surfaces that need members before they draw (the unread threads
// popover's rows, autocomplete's affinity pool, agents, user groups,
// meeting headers), none of them a message list.
//
// So Clack's message loads store what they got and fetch nobody: the rows
// look their people up as they draw (store.User), mentions included
// (msgNames, richview's MemberRead), and "names" draws them again.

// What a view draws while it waits (Slack's client, the CDN build of
// 2026-11-14, chunks read without a session):
//
//   - getMemberById (gIKW) hands back the UnknownFetchManager's stub for a
//     member the store doesn't hold (makeUnknownMember, MA/Q: isUnknown,
//     no display or real name), and a non-existent member once users/info
//     didn't return the id (makeNonExistentMember). vrX/ tells them apart
//     (isUnknownMember, isNonExistentMember).
//   - BaseMessageSender (/OeL), MemberSlug (ld4x, a user mention),
//     MemberLink (25Rg) and TooltipMemberName (NZA6, the reaction tip) draw
//     BaseMissingMember (FToV) for both: MissingText's bar (sSUQ, medium:
//     120px), "Loading…" or "Private user info" in its tooltip. The sender
//     checks the stub before an override of the name ("Name hidden");
//     a huddle message's and a custom response's senders are usernames
//     (getMsgSenderEntity, 33X/), no member. MemberSlug draws a
//     non-existent member whose id doesn't look like a member's
//     (looksLikeMemberId) as "@id".
//   - Names in text (getPrefCompliantMemberNameById, Emmo; RollupName,
//     quVX, in system messages) are the stub's name: "".
//
// None of them shows the id, and all draw again when the fetch upserts the
// member: Clack's "names" event.

// MemberState is a member as getMemberById answers a view: in the store,
// the unknown stub (users/info is due or running), or non-existent.
type MemberState uint8

const (
	MemberKnown       MemberState = iota // in the store
	MemberUnknown                        // the unknown stub: BaseMissingMember "Loading…"
	MemberNonExistent                    // users/info didn't return it: "Private user info"
)

// MissingMember is the state of member id once User found no member for
// it: non-existent when users/info said so, else the unknown stub (the
// lookup that missed asked for it, and "names" follows the answer).
func (a *App) MissingMember(id string) MemberState {
	if a.S.Nonexistent(id) {
		return MemberNonExistent
	}
	return MemberUnknown
}

// MemberStateOf is member id's state, read as getMemberById reads it (a
// hit is an access, a miss is asked for).
func (a *App) MemberStateOf(id string) MemberState {
	if _, ok := a.S.User(id); ok {
		return MemberKnown
	}
	return a.MissingMember(id)
}

// MissingBot is bot id's state for a sender core can't name otherwise (a
// user-less message with no username or bot_profile), as getBotById
// (client-boot Emf2 AF) answers the connected MessageSender: a bot the
// store holds is known; one nothing returned is the bots
// UnknownFetchManager's non-existent stub; any other id that looks like a
// bot's (looksLikeBotId, 13v0) is asked for (senderOf noted it) and is
// the unknown stub until bots.info answers. Slack's own bots
// (slackInternalBots) and ids that don't look like a bot's are never
// fetched: no bot at all, so no bar (and no name).
func (a *App) MissingBot(id string) MemberState {
	if _, ok := a.S.Bot(id); ok {
		return MemberKnown
	}
	if a.S.BotNonexistent(id) {
		return MemberNonExistent
	}
	if slackInternalBots[id] || !botIDRe.MatchString(id) {
		return MemberKnown
	}
	return MemberUnknown
}

// botIDRe is looksLikeBotId (13v0): "B", eight or more of A-Z0-9, an "@"
// before it allowed.
var botIDRe = regexp.MustCompile(`^@?B[A-Z0-9]{8,}$`)

// slackInternalBots are the bot ids getBotById never fetches (Emf2's d):
// Slack's own.
var slackInternalBots = map[string]bool{"B01": true, "BSLACKBOT": true, "B03": true, "BSLACKSECURITY": true, "BCHANNELEXPERT": true}

// LooksLikeMemberID is Slack's looksLikeMemberId (5oKS): Slackbot and
// Slack's fixed members (USLACKBOT, USEARCH, UCHANNELEXPERT, USLACK,
// UTBD), or "U" or "W" and eight or more of A-Z0-9, an "@" before it
// allowed. getMemberById fetches only ids that look like this; any other
// is a non-existent member at once.
func LooksLikeMemberID(id string) bool {
	switch id {
	case "USLACKBOT", "USEARCH", "UCHANNELEXPERT", "USLACK", "UTBD":
		return true
	}
	return navMemberID.MatchString(id)
}

// msgNames is the resolver message docs are built with: a user mention
// takes the member's name if the store holds it, without asking for a
// missing one; the mention asks when a view draws it (richview
// MemberRead), as Slack's MemberSlug does when it renders.
type msgNames struct{ *store.Store }

// MentionName is richtext.MentionNamer.
func (r msgNames) MentionName(id string) string {
	u, _ := r.RawUser(id)
	return store.DisplayName(u)
}

// rowUsers are the members a message's row shows: its sender, the
// members its text mentions, the reply bar's people and a broadcast's
// root sender. Not its reactions' users (only the reaction tooltip names
// them) nor its editor (edited.user: drawn only by a workflow's "Edited
// by" label, which looks it up itself). Only what must name people before
// any row draws fetches them first: a socket message's notification.
func rowUsers(msgs []slack.Message) []string {
	var ids []string
	for i := range msgs {
		m := &msgs[i]
		ids = append(ids, m.User)
		ids = append(ids, m.ReplyUsers...)
		for _, mm := range mentionRe.FindAllStringSubmatch(m.Text, -1) {
			ids = append(ids, mm[1])
		}
		if m.Root != nil {
			ids = append(ids, m.Root.User)
		}
	}
	return ids
}

// usersAsked counts the ids sent to users/info (EnsureUsers and the
// UnknownFetchManager), retries not counted again.
type usersAsked struct{ atomic.Int64 }

// UsersAsked is how many member ids this session asked users/info for:
// perf's scale jobs report it per stage (app_users_asked).
func (a *App) UsersAsked() int64 { return a.usersAsked.Load() }
