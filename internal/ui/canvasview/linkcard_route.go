package canvasview

// Where useEmbeddedUnfurlType (gZnA) sends a card before the public link
// and fallback steps: the ids each kind's card reads. Steps 7-21, in
// order; the notes are in /root/clack-ops/scratch/link-url-box-3/
// cards-spec.md §0.1.

import (
	"encoding/json"
	"strconv"

	"github.com/some-du6e/clack/internal/quip"
)

// cardRoute is the kind gZnA gives a card at steps 7-21 ("" past them)
// and what its card reads.
type cardRoute struct {
	kind                                                      string // canvas, list, file, user, message, channel, other
	channel, ts, threadTS, fileID, memberID, recordID, viewID string
}

// routeOf is gZnA's steps 7-21 for the card as refreshCards last read it.
func (c *linkCard) routeOf() cardRoute {
	if !c.modelOK || c.cc.Section == "" {
		return cardRoute{}
	}
	held := c.cc.Type == quip.CardSlackUnfurl && c.held
	var attTS, attCh, link string
	var isMsgUnfurl bool
	link = c.url
	if held && len(c.msg.Attachments) > 0 {
		a := &c.msg.Attachments[0]
		if a.OriginalURL != "" {
			link = a.OriginalURL
		}
		attTS, attCh, isMsgUnfurl = rawTS(a.TS), a.ChannelID, a.IsMsgUnfurl
	}
	pl, isPermalink := parsePermalink(link)
	switch t := c.cc.Type; {
	case t == quip.CardSlackDocument: // 7, 10: a DOCUMENT object
		return cardRoute{kind: "canvas", fileID: c.cc.ContentID}
	case isPermalink && pl.typ == "quip":
		return cardRoute{kind: "canvas", fileID: pl.fileID}
	case isPermalink && pl.typ == "canvas": // a channel's canvas: its properties.canvas.file_id
		return cardRoute{kind: "canvas", channel: pl.channel}
	case isPermalink && pl.typ == "list": // 8
		return cardRoute{kind: "list", fileID: pl.fileID, recordID: pl.recordID, viewID: pl.viewID}
	case held && len(c.msg.Files) > 0: // 9: the container's file
		return cardRoute{kind: "file", fileID: c.msg.Files[0].ID}
	case t == quip.CardSlackFile:
		return cardRoute{kind: "file", fileID: c.cc.ContentID}
	case isPermalink && pl.typ == "file":
		return cardRoute{kind: "file", fileID: pl.fileID}
	case t == quip.CardSlackUser: // 11
		return cardRoute{kind: "user", memberID: c.cc.ContentID}
	case isPermalink && pl.typ == "member":
		return cardRoute{kind: "user", memberID: pl.memberID}
	case t == quip.CardSlackGeneric, t == quip.CardSlackWidget, t == quip.CardSlackList, t == quip.CardSlackListRecord: // 12-15
		return cardRoute{kind: "other"}
	case isMsgUnfurl || isPermalink && pl.typ == "message": // 16, 17
		ts, ch := firstNonEmpty(pl.ts, attTS), firstNonEmpty(pl.channel, attCh)
		if ts != "" {
			return cardRoute{kind: "message", channel: ch, ts: ts, threadTS: pl.threadTS}
		}
		return cardRoute{kind: "channel", channel: ch}
	case t == quip.CardSlackSalesforceRecord, t == quip.CardSlackSalesforceDataBlock, t == quip.CardSlackInvitedUser: // 21
		return cardRoute{kind: "other"}
	}
	return cardRoute{}
}

// rawTS is an attachment's ts (a string or a number) as a Slack ts.
func rawTS(raw json.RawMessage) string {
	var s string
	if json.Unmarshal(raw, &s) == nil {
		return s
	}
	var f float64
	if json.Unmarshal(raw, &f) == nil && f > 0 {
		return strconv.FormatFloat(f, 'f', 6, 64)
	}
	return ""
}
