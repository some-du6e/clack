package core

import (
	"context"
	"encoding/json"
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

// slugPeekTabs are the tab types with a peek (client-boot mwX5's P,
// isPreviewEnabled): under session_channels_slug_peek a slug to one of
// them shows the tab's peek (D3FY ChannelTabSlugPeekTrigger, useTabPeek)
// instead of the channel hover card.
var slugPeekTabs = map[string]bool{
	"agent_sessions": true, "agent_view": true, "automations": true, "bookmarks": true, "canvas": true, "channel": true,
	"code": true, "code_channel_pull_request": true, "external_files": true, "files": true, "folder": true, "list": true,
	"native_file": true, "pins": true, "record_overview": true, "record_summary": true, "workflows": true,
	"record_related_list": true, "record_related_conversations": true, "salesforce_list_view": true,
}

// ChannelTabSlugCard reports whether a channel-tab slug (a rich_text
// channel element with a tab_id) opens Slack's ChannelHoverCard, as
// boot-shared NY16 wires it (build 3889f0c93d30):
//
//   - MrkdwnChannelTab resolves the tab: the first of the channel's
//     properties.tabz whose id or type is the tab id
//     (findTabInChannelProperties), else a code channel's artifact tab
//     (getCodeChannelArtifactTabForTabId: artifact tabs aren't on for the
//     account, so none), else the Code tab for "code". With the channel
//     known and either public, joined or a record channel, and the tab
//     resolved, it's ResolvedChannelTabSlug; anything else falls back to
//     BaseMrkdwnChannel (lH2I), whose HoverCardTrigger opens the card.
//   - ResolvedChannelTabSlug renders renderChannelTabSlugPeek (zDOB),
//     which D3FY's ChannelTabSlugPeekTrigger fills: with
//     session_channels_slug_peek on and a tab type that has a peek
//     (slugPeekTabs), the tab's peek; otherwise the HoverCardTrigger
//     with the channel card (enableButtons).
//
// A slug that doesn't open the card shows the tab's peek
// (ChannelTabSlugPeek; ui/tab_slug_peek.go draws it).
func (a *App) ChannelTabSlugCard(ch, tab string) bool {
	_, card := a.resolveTabSlug(ch, tab)
	return card
}

// SlugTab is the channel tab a slug resolved to: the properties.tabz
// entry (findTabById), with its data the peeks read (Slack's tab.data:
// fileId, listsChannelTabDefaultRefineViewId).
type SlugTab struct {
	ID, Type, Label string
	FileID          string // data.file_id
	DefaultView     string // data.lists_channel_tab_default_refine_view_id
	FolderID        string // data.folder_bookmark_id
}

// ChannelTabSlugPeek is ChannelTabSlugPeekTrigger's choice: the slug's
// resolved tab when it shows the tab's peek (session_channels_slug_peek
// on and isPreviewEnabled), false when it opens the channel card (or
// nothing resolves).
func (a *App) ChannelTabSlugPeek(ch, tab string) (SlugTab, bool) {
	t, card := a.resolveTabSlug(ch, tab)
	if card {
		return SlugTab{}, false
	}
	return t, true
}

// resolveTabSlug is MrkdwnChannelTab + ChannelTabSlugPeekTrigger: the
// resolved tab, and whether the slug opens the channel card.
func (a *App) resolveTabSlug(ch, tab string) (SlugTab, bool) {
	c, ok := a.S.Conv(ch)
	if !ok {
		if c, ok = a.S.ExtConv(ch); !ok {
			return SlugTab{}, true // unknown: BaseMrkdwnChannel
		}
	}
	private := (c.IsPrivate || c.IsGroup) && !c.IsMPIM // aK5v.Zp
	record := recordID(c.Properties) != ""             // aK5v.rI
	if private && !a.isMemberConv(c) && !record {
		return SlugTab{}, true
	}
	t, found := tabzTab(c.Properties, tab)
	if !found && tab == "code" { // MrkdwnChannelTab's Code tab fallback: {id: "code", type: "code"}
		t, found = SlugTab{ID: "code", Type: "code"}, true
	}
	if !found {
		return SlugTab{}, true
	}
	if !(a.experiment("session_channels_slug_peek") == "on" && slugPeekTabs[t.Type]) {
		return t, true
	}
	return t, false
}

// tabzType is findTabInChannelProperties (mwX5.kt → findTabById): the
// type of the first of properties.tabz whose id or type is id.
func tabzType(props json.RawMessage, id string) (string, bool) {
	t, ok := tabzTab(props, id)
	return t.Type, ok
}

// tabzTab is findTabInChannelProperties: the first of properties.tabz
// whose id or type is id.
func tabzTab(props json.RawMessage, id string) (SlugTab, bool) {
	if id == "" || len(props) == 0 {
		return SlugTab{}, false
	}
	var p struct {
		Tabz []struct {
			ID    string `json:"id"`
			Type  string `json:"type"`
			Label string `json:"label"`
			Data  struct {
				FileID      string `json:"file_id"`
				DefaultView string `json:"lists_channel_tab_default_refine_view_id"`
				FolderID    string `json:"folder_bookmark_id"`
			} `json:"data"`
		} `json:"tabz"`
	}
	if json.Unmarshal(props, &p) != nil {
		return SlugTab{}, false
	}
	for _, t := range p.Tabz {
		if t.ID == id || t.Type == id {
			return SlugTab{ID: t.ID, Type: t.Type, Label: t.Label, FileID: t.Data.FileID, DefaultView: t.Data.DefaultView,
				FolderID: t.Data.FolderID}, true
		}
	}
	return SlugTab{}, false
}

// recordID is properties.record_channel.record_id (aK5v.rI
// isRecordChannel tests it).
func recordID(props json.RawMessage) string {
	var p struct {
		Record struct {
			ID string `json:"record_id"`
		} `json:"record_channel"`
	}
	if len(props) == 0 || json.Unmarshal(props, &p) != nil {
		return ""
	}
	return p.Record.ID
}
