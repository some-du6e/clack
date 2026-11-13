package core

import (
	"context"
	"encoding/json"
	"regexp"
	"strings"
	"sync"

	"github.com/some-du6e/clack/internal/richtext"
	"github.com/some-du6e/clack/internal/slack"
)

// What Slack's CodeChannelEntityCard (boot-shared ICrU, build
// 3889f0c93d30) reads for a code channel's hover card
// (popups.ChannelHoverCard draws it):
//
//   - the summary (useCodeChannelSummaryText, 04hH): for a channel the
//     viewer is in or a public one, code_channel.summary_message.message_ts
//     (a member's "pin as summary" localStorage fallback, ubsF, is set by a
//     message action Clack doesn't have), looked up in the message store
//     only (iR00 Ld: nothing fetches it). Its text is the message's last
//     rich_text block as plain text with unicode emoji, blank runs
//     collapsed (V74f fC), or, without a rich_text block, the message's
//     own text less the generated "This message contains interactive
//     elements." (getCodeChannelPreviewText); its author the sender's
//     display name (Emmo I).
//   - the facepile's people: the agents (encoded_agent_bot_user_ids),
//     then the channel's first members (membershipOrdered, XX9n $), which
//     rbiO's fetcher fills through fetchAndUpsertFirstNMembersForChannel:
//     edge users/list {channels: [ch], present_first, filter "everyone
//     AND NOT bots AND NOT apps", count 20}. ChannelTopMembers /
//     LoadChannelTopMembers.

// CodeSummary is a code channel's summary message as the card shows it.
type CodeSummary struct {
	TS     string // the message's ts (the attribution's time)
	Text   string
	Author string
}

var blankRuns = regexp.MustCompile(`\n{2,}`)

// normalizePreview is V74f's normalizePreviewText.
func normalizePreview(s string) string { return strings.TrimSpace(blankRuns.ReplaceAllString(s, "\n")) }

// CodeChannelSummary is useCodeChannelSummaryText for ch: ok false when
// the card has no summary (hasSummary: a stored message with text).
func (a *App) CodeChannelSummary(ch string) (CodeSummary, bool) {
	c, ok := a.S.Conv(ch)
	if !ok {
		if c, ok = a.S.ExtConv(ch); !ok {
			return CodeSummary{}, false
		}
	}
	cc := codeChannelOf(c)
	if cc == nil || cc.SummaryMessage == "" {
		return CodeSummary{}, false
	}
	public := c.IsChannel && !(c.IsPrivate || c.IsGroup) && !c.IsMPIM // aK5v.rW
	if !a.isMemberConv(c) && !public {
		return CodeSummary{}, false
	}
	t := a.S.Timeline(ch)
	if t == nil {
		return CodeSummary{}, false
	}
	var m *slack.Message
	for i := range t.Msgs {
		if t.Msgs[i].TS == cc.SummaryMessage {
			m = &t.Msgs[i]
			break
		}
	}
	if m == nil {
		return CodeSummary{}, false
	}
	text := codeSummaryText(a, m)
	if text == "" {
		return CodeSummary{}, false
	}
	author := ""
	switch {
	case m.User != "":
		author = a.S.UserName(m.User)
	case m.Username != "":
		author = m.Username
	case m.BotID != "":
		author = a.S.UserName(m.BotID)
	}
	return CodeSummary{TS: m.TS, Text: text, Author: author}, true
}

// codeSummaryText is the summary's text: the last rich_text block's plain
// text, else the message's (getCodeChannelPreviewText without the
// generated fallback).
func codeSummaryText(a *App, m *slack.Message) string {
	var blocks []json.RawMessage
	_ = json.Unmarshal(m.Blocks, &blocks)
	var last json.RawMessage
	for _, b := range blocks {
		var t struct {
			Type string `json:"type"`
		}
		if json.Unmarshal(b, &t) == nil && t.Type == "rich_text" {
			last = b
		}
	}
	if last != nil {
		one := *m
		one.Blocks, _ = json.Marshal([]json.RawMessage{last})
		one.Attachments, one.RawAttachments, one.Files = nil, nil, nil
		return normalizePreview(richtext.PlainText(richtext.FromMessage(a.S, &one)))
	}
	one := *m
	one.Attachments, one.RawAttachments, one.Files = nil, nil, nil
	s := normalizePreview(richtext.PlainText(richtext.FromMessage(a.S, &one)))
	if s == "This message contains interactive elements." {
		return ""
	}
	return s
}

// topMembers is the membershipOrdered store.
type topMembers struct {
	mu  sync.Mutex
	ids map[string][]string
}

// ChannelTopMembers is membershipOrdered for ch (the first 20 people, in
// edge's present-first order); ok false until it loads.
func (a *App) ChannelTopMembers(ch string) ([]string, bool) {
	a.topMem.mu.Lock()
	defer a.topMem.mu.Unlock()
	ids, ok := a.topMem.ids[ch]
	return ids, ok
}

// LoadChannelTopMembers is fetchAndUpsertFirstNMembersForChannel: edge
// users/list for the channel's first 20 people, kept as membershipOrdered.
func (a *App) LoadChannelTopMembers(ctx context.Context, ch string) error {
	us, _, err := a.C.ChannelMembers(ctx, ch, "", 20, true)
	if err != nil {
		return err
	}
	a.S.PutUsers(us)
	ids := make([]string, 0, len(us))
	for _, u := range us {
		ids = append(ids, u.ID)
	}
	a.topMem.mu.Lock()
	if a.topMem.ids == nil {
		a.topMem.ids = map[string][]string{}
	}
	a.topMem.ids[ch] = ids
	a.topMem.mu.Unlock()
	return nil
}
