package quip

import "strings"

// LinkUnfurlCard is proto.section.Section.ContentLinkUnfurlCard (Content
// field 50): a card section's (TypeLinkUnfurlCard) link and what Slack's
// card view draws for it (quip-collab-editor's link-unfurl-card, the
// webapp's CardViewImpl). Type is unset for SLACK_FILE, the schema's
// default (CardType reads it).
type LinkUnfurlCard struct {
	URL           *string      `pb:"1"`
	Type          *int32       `pb:"2"`
	CardContent   *CardContent `pb:"3"`
	IsUnfurled    *bool        `pb:"6"`
	Incomplete    *bool        `pb:"7"`
	IsPlaceholder *bool        `pb:"8"`
	// Other: upload_status, snapshots_paused, placeholder_metadata.
	Other []byte `pb:"extra" slack:"4,5,9"`
}

// CardContent is ContentLinkUnfurlCard.CardContent: one of its kinds is set.
type CardContent struct {
	SlackFile     *CardFile   `pb:"2"`
	SlackUnfurl   *CardUnfurl `pb:"3"`
	SlackDocument *CardID     `pb:"4"` // slack_document_id
	SlackUser     *CardID     `pb:"5"` // slack_user_id
	// Other: slack_generic, slack_widget, slack_list, slack_invited_user,
	// slack_list_record and the Salesforce kinds.
	Other []byte `pb:"extra" slack:"6,8-13"`
}

// CardUnfurl is CardContent.SlackUnfurlContent: the unfurl container
// message a SLACK_UNFURL card draws (ParseUnfurlID).
type CardUnfurl struct {
	SlackUnfurlID *string `pb:"1"` // "suo:<team>/<channel>/<ts>"
	RenderType    *int32  `pb:"2"`
}

// CardFile is CardContent.SlackFileContent.
type CardFile struct {
	SlackFileID *string `pb:"1"`
	RenderType  *int32  `pb:"2"`
}

// CardID is a card content with one id field (SlackDocumentContent,
// SlackUserContent).
type CardID struct {
	ID *string `pb:"1"`
}

// ContentLinkUnfurlCard.Type (gantry-v2-async-quip-collab-webapp's enum,
// i[i.SLACK_UNFURL=3]).
const (
	CardSlackFile                int32 = 2 // the schema's default
	CardSlackUnfurl              int32 = 3
	CardSlackDocument            int32 = 4
	CardSlackPendingURL          int32 = 5
	CardSlackUser                int32 = 6
	CardSlackGeneric             int32 = 7
	CardSlackWidget              int32 = 8
	CardSlackList                int32 = 9
	CardSlackInvitedUser         int32 = 10
	CardSlackListRecord          int32 = 11
	CardSlackSalesforceRecord    int32 = 12
	CardSlackSalesforceDataBlock int32 = 13
)

// CardType is the card's type, SLACK_FILE when unset (the schema's
// default).
func (c *LinkUnfurlCard) CardType() int32 {
	if c == nil || c.Type == nil {
		return CardSlackFile
	}
	return *c.Type
}

// UnfurlID is the card's slack_unfurl_id ("" when it has none).
func (c *LinkUnfurlCard) UnfurlID() string {
	if c == nil || c.CardContent == nil || c.CardContent.SlackUnfurl == nil {
		return ""
	}
	return str(c.CardContent.SlackUnfurl.SlackUnfurlID)
}

// unfurlIDPrefix is the webapp's T in parseSlackUnfurlId and
// syncerIdForSlackUnfurl.
const unfurlIDPrefix = "suo:"

// ParseUnfurlID splits a slack_unfurl_id, "suo:<team>/<channel>/<ts>",
// into the unfurl container message's team, channel and ts (Quip's
// parseSlackUnfurlId); ok is false for anything else.
func ParseUnfurlID(id string) (team, channel, ts string, ok bool) {
	rest, found := strings.CutPrefix(id, unfurlIDPrefix)
	if !found {
		return "", "", "", false
	}
	parts := strings.Split(rest, "/")
	if len(parts) != 3 || parts[0] == "" || parts[1] == "" || parts[2] == "" {
		return "", "", "", false
	}
	return parts[0], parts[1], parts[2], true
}

// UnfurlID is the slack_unfurl_id of the container message channel/ts of
// team (syncerIdForSlackUnfurl).
func UnfurlID(team, channel, ts string) string {
	return unfurlIDPrefix + team + "/" + channel + "/" + ts
}

// LinkCards are the document's card sections (TypeLinkUnfurlCard) in
// document order, the ones the export draws as link cards first: Slack's
// SLACK_FILE cards are type 56 as well (the export's p.embedded-file), so
// a caller pairs the export's links by URL (PairLinkCards), not by count.
func (d *Doc) LinkCards() []*Section {
	var out []*Section
	for _, s := range d.Sections() {
		if s.Type != nil && *s.Type == TypeLinkUnfurlCard && s.Content != nil && s.Content.LinkUnfurl != nil {
			out = append(out, s)
		}
	}
	return out
}

// PairLinkCards pairs each of the export's link cards (urls, in document
// order) with the next unused card section whose url is the same: the
// export carries no section id. out[i] is nil where none matches.
func PairLinkCards(cards []*Section, urls []string) []*Section {
	out := make([]*Section, len(urls))
	used := make([]bool, len(cards))
	for i, u := range urls {
		if u == "" {
			continue
		}
		for j, c := range cards {
			if !used[j] && str(c.Content.LinkUnfurl.URL) == u {
				used[j] = true
				out[i] = c
				break
			}
		}
	}
	return out
}
