package core

// A file's references (FileDetails' Shared in section, boot-shared 2TKv
// FileShareContext, build 031dbe6a7400): the fileShares store's
// ConditionalFetchManager (dc7t) runs fetchAndUpsertFileShares, which
// asks files.getShares {file_id}; c2wK formatShareData turns its answer
// into what the section draws:
//
//	{"conversation_shares": {"count": 1, "shares": {"public": {"C…": [{"ts",
//	 "channel_name", "team_id", "share_user_id", "source",
//	 "is_silent_share", "reply_users", "reply_users_count",
//	 "reply_count"}]}}},
//	 "tab_shares": {"count": 0, "shares": {}},
//	 "file_channel_shares": {"count": 0, "shares": {}}}
//
// (the shape Hack Club answered for an email file, 2026-11-19, read
// without its values). formatShareData skips a share at
// "0000000000.000000" with no date_last_shared; the conversations are
// public then private, newest first.

import (
	"bytes"
	"context"
	"encoding/json"
	"net/url"
	"sort"
)

// ExpFilesInfoPaneCountStyle is files_info_pane_count_style: on, the
// Shared in section's "Conversations {count}" has only its word in bold
// (TestExperimentReadsListed).
const ExpFilesInfoPaneCountStyle = "files_info_pane_count_style"

func init() { experimentReads[ExpFilesInfoPaneCountStyle] = on }

// FileShareInfo is what files.getShares says of a file.
type FileShareInfo struct {
	// Conversations are the messages that share the file: public ones
	// first, then private, newest first.
	Conversations []FileShare
	// ConversationCount, TabCount and FileChannelCount are the answer's
	// counts (conversation_shares, tab_shares, file_channel_shares).
	ConversationCount, TabCount, FileChannelCount int
}

// FileShare is one conversation share (formatShareData).
type FileShare struct {
	Channel, TS, ThreadTS, ChannelName string
	ReplyCount                         int
	Public                             bool
}

type fileSharesGroup struct {
	Count  int `json:"count"`
	Shares struct {
		Public  map[string][]fileShareJSON `json:"public"`
		Private map[string][]fileShareJSON `json:"private"`
	} `json:"shares"`
}

type fileShareJSON struct {
	TS             string          `json:"ts"`
	ThreadTS       string          `json:"thread_ts"`
	ChannelName    string          `json:"channel_name"`
	ReplyCount     int             `json:"reply_count"`
	DateLastShared json.RawMessage `json:"date_last_shared"`
}

// FileShares asks files.getShares for file id's references.
func (a *App) FileShares(ctx context.Context, id string) (FileShareInfo, error) {
	var r struct {
		Conversation fileSharesGroup `json:"conversation_shares"`
		Tab          fileSharesGroup `json:"tab_shares"`
		FileChannel  fileSharesGroup `json:"file_channel_shares"`
	}
	if err := a.C.Call(ctx, "files.getShares", url.Values{"file_id": {id}}, &r); err != nil {
		return FileShareInfo{}, err
	}
	return fileShareInfo(r.Conversation, r.Tab.Count, r.FileChannel.Count), nil
}

// fileShareInfo is c2wK's formatting of the conversation shares.
func fileShareInfo(conv fileSharesGroup, tabs, fileChannels int) FileShareInfo {
	out := FileShareInfo{ConversationCount: conv.Count, TabCount: tabs, FileChannelCount: fileChannels}
	add := func(m map[string][]fileShareJSON, public bool) {
		chans := make([]string, 0, len(m))
		for ch := range m {
			chans = append(chans, ch)
		}
		sort.Strings(chans)
		for _, ch := range chans {
			for _, s := range m[ch] {
				if s.TS == "0000000000.000000" && !shareDated(s.DateLastShared) {
					continue
				}
				out.Conversations = append(out.Conversations, FileShare{Channel: ch, TS: s.TS, ThreadTS: s.ThreadTS,
					ChannelName: s.ChannelName, ReplyCount: s.ReplyCount, Public: public})
			}
		}
	}
	add(conv.Shares.Public, true)
	add(conv.Shares.Private, false)
	sort.SliceStable(out.Conversations, func(i, j int) bool { return out.Conversations[i].TS > out.Conversations[j].TS })
	return out
}

// shareDated reports whether date_last_shared is truthy.
func shareDated(raw json.RawMessage) bool {
	v := bytes.TrimSpace(raw)
	switch string(v) {
	case "", "null", "0", `""`, "false":
		return false
	}
	return true
}

// FileShareCard is share s as FileShareDetailsMessageEntity (0MIe) draws
// it: its message and its channel, when the client has both (Slack draws
// nothing for a share whose message the store lacks). An IM is named for
// its other member.
func (a *App) FileShareCard(s FileShare) (LinkShare, bool) {
	m, ok := a.S.Find(s.Channel, s.TS)
	if !ok {
		return LinkShare{}, false
	}
	c, ok := a.Conv(s.Channel)
	if !ok {
		return LinkShare{}, false
	}
	ls := LinkShare{Channel: s.Channel, Name: c.Name, IM: c.Kind == "im", MPIM: c.Kind == "mpim", Message: a.MessageFromSlack(s.Channel, &m)}
	if ls.IM {
		ls.Name = ""
		if u, ok := a.User(c.User); ok {
			ls.Name = firstNonEmptyName(u.Name, u.RealName, u.Handle)
		}
	}
	return ls, true
}

func firstNonEmptyName(ss ...string) string {
	for _, s := range ss {
		if s != "" {
			return s
		}
	}
	return ""
}
