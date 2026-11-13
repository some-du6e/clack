package core

import (
	"context"
	"slices"
	"strconv"
	"strings"
	"time"

	"github.com/some-du6e/clack/internal/slack"
)

// What Slack's ChannelHoverCard (boot-shared 7+JF, build 3889f0c93d30,
// read from the public CDN without a cookie) reads about a channel. The
// card itself is popups.ChannelHoverCard; these are its selectors and
// fetches:
//
//   - the channel (6Z4k getChannelById), fetched by Slack's unknown
//     channel manager when missing: ResolveConv (conversations.info).
//     isChannelViewable (HY4m): existent, and public, a record channel or
//     one the viewer is in. Anything else (a private channel the viewer
//     isn't in comes back channel_not_found) gets the no-access text.
//   - the member count (552O getMembershipCountsByChannelId, which asks
//     PBfs to refetch a stale entry: fetchAndUpsertMembershipCountsForChannel,
//     Gpq2, the edge users/counts call with as_admin): member_count is
//     `people`. ChannelPeopleCount / LoadPeopleCount.
//   - the last message (ChannelHoverCardBodyItemLastMessage): the newest
//     loaded message of the channel's history whose subtype isn't one of
//     isAutomatedSubtype's (KC8h), after useFetchChannelHistory (4iWy)
//     fetched conversations.history {channel, limit 1} (reason
//     "channel-hover-card/latest-timestamp"; Clack's calls carry no
//     _x_reason) unless the history slice has reached its end.
//     ChannelLatestTS.
//   - the external teams (0tCs getConnectedTeamIdsForModelObjectIds →
//     tebx/t7jr): Conv.ConnectedTeams.
//   - the mention badge (Z0JO): a channel's unreadHighlightCnt from
//     client.counts, Count.Mentions.

// IsAutomatedSubtype is KC8h isAutomatedSubtype with its default list
// (automatedSubtypes, channel_agents.go).
func IsAutomatedSubtype(subtype string) bool { return automatedSubtypes[subtype] }

// isMemberConv is JUrK isMember for a stored conversation: is_member, or
// one of the viewer's conversations (users.conversations, the sidebar
// store); DMs and group DMs are always the viewer's.
func (a *App) isMemberConv(c *slack.Conversation) bool {
	if c == nil {
		return false
	}
	if c.IsIM || c.IsMPIM || c.IsMember {
		return true
	}
	_, in := a.S.Conv(c.ID)
	return in
}

// connectedTeams is setConnectedTeamIds: connected_team_ids without the
// viewer's top-level team (mAu1 omitCurrentTeamId, K5IA Kc: the org on
// Enterprise Grid, else the workspace), each once.
func (a *App) connectedTeams(c *slack.Conversation) []string {
	if c == nil || len(c.ConnectedTeamIDs) == 0 {
		return nil
	}
	top := ""
	if a.C != nil {
		top = firstNonEmpty(a.C.Creds.EnterpriseID, a.C.Creds.Team)
	}
	var out []string
	for _, t := range c.ConnectedTeamIDs {
		if t == "" || t == top || slices.Contains(out, t) {
			continue
		}
		out = append(out, t)
	}
	return out
}

// ChannelPeopleCount is the card's member count: membershipCounts'
// member_count (edge users/counts `people`), ok false until it loads.
func (a *App) ChannelPeopleCount(ch string) (int, bool) { return a.MemberCountCached(ch) }

// LoadPeopleCount is getMembershipCountsByChannelId's maybeRefetch for
// a channel whose counts aren't known: fetchAndUpsertMembershipCountsForChannel
// (edge users/counts). A DM is Slack's fixed {member_count: 2} and isn't
// fetched.
func (a *App) LoadPeopleCount(ctx context.Context, ch string) error { // a failure leaves the count unknown
	if ch == "" || strings.HasPrefix(ch, "D") {
		return nil
	}
	if _, ok := a.MemberCountCached(ch); ok {
		return nil
	}
	a.loadMemberCount(ctx, ch)
	return nil
}

// latestShown is the card's findLast: the newest message of msgs (any
// order) that isn't an automated subtype, "" for none.
func latestShown(msgs []slack.Message) string {
	best := ""
	for _, m := range msgs {
		if m.TS == "" || IsAutomatedSubtype(m.Subtype) {
			continue
		}
		if best == "" || tsLess(best, m.TS) {
			best = m.TS
		}
	}
	return best
}

// ChannelLatestTS is ChannelHoverCardBodyItemLastMessage's timestamp:
// the newest loaded message that isn't automated ("" for none). A
// channel whose history core holds (kept current by the realtime feed)
// answers from it; any other is read the way useFetchChannelHistory
// reads it, conversations.history {channel, limit: 1, inclusive}, and the
// card looks only at that message, as Slack's store then holds only it.
func (a *App) ChannelLatestTS(ctx context.Context, ch string) (string, error) {
	if t := a.S.Timeline(ch); t != nil {
		return latestShown(t.Msgs), nil
	}
	p, err := a.C.History(ctx, ch, slack.HistoryOpts{Limit: 1, Inclusive: true})
	if err != nil {
		return "", err
	}
	return latestShown(p.Messages), nil
}

// LastMessageText is getLatestChannelMessageDateText for ts at now:
// "No messages", "Last message today", "… yesterday", "Last message on
// {MMM Do}" this year, else "Last message in {YYYY}". tr translates in
// Slack's "team" namespace (the caller's i18n).
func LastMessageText(ts string, now time.Time, tr func(src string, args ...any) string) string {
	if ts == "" {
		return tr("No messages")
	}
	sec, _, _ := strings.Cut(ts, ".")
	n, err := strconv.ParseInt(sec, 10, 64)
	if err != nil {
		return tr("No messages")
	}
	t := time.Unix(n, 0).In(now.Location())
	y, m, d := t.Date()
	ny, nm, nd := now.Date()
	if y == ny && m == nm && d == nd {
		return tr("Last message today")
	}
	if py, pm, pd := now.AddDate(0, 0, -1).Date(); y == py && m == pm && d == pd {
		return tr("Last message yesterday")
	}
	if y == ny {
		return tr("Last message on {createdDate}", "createdDate", t.Format("Jan ")+ordinalDay(d))
	}
	return tr("Last message in {createdDate}", "createdDate", t.Format("2006"))
}

// ordinalDay is moment's "Do" in English: 1st, 2nd, 3rd, 4th … 11th,
// 12th, 13th, 21st.
func ordinalDay(d int) string {
	s := strconv.Itoa(d)
	switch {
	case d%100 >= 11 && d%100 <= 13:
		return s + "th"
	case d%10 == 1:
		return s + "st"
	case d%10 == 2:
		return s + "nd"
	case d%10 == 3:
		return s + "rd"
	}
	return s + "th"
}
