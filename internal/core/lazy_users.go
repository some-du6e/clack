package core

import (
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
