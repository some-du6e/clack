package core

import (
	"context"
	"testing"

	"github.com/some-du6e/clack/internal/slack"
)

// files.getShares (FileShareContext's data: boot-shared dc7t, c2wK
// formatShareData) against a fake API: asked with file_id; the counts as
// given; the conversations public and private together, newest first; a
// share at 0000000000.000000 with no date_last_shared is skipped. A card
// (0MIe) needs the share's message and channel in the store; an IM's is
// named for its other member.
func TestFileShares(t *testing.T) {
	a, f := testApp(t)
	f.reply["files.getShares"] = `{"ok":true,
		"conversation_shares":{"count":3,"shares":{
			"public":{"C1":[{"ts":"1700000000.000100","channel_name":"general","team_id":"T1","reply_count":0},
				{"ts":"0000000000.000000","channel_name":"general"}]},
			"private":{"D1":[{"ts":"1700000500.000100","thread_ts":"1700000400.000100","reply_count":2}]}}},
		"tab_shares":{"count":1,"shares":{}},
		"file_channel_shares":{"count":0,"shares":{}}}`
	info, err := a.FileShares(context.Background(), "F0EMAIL0001")
	if err != nil {
		t.Fatal(err)
	}
	if p := f.last("files.getShares"); p == nil || p.Get("file_id") != "F0EMAIL0001" {
		t.Errorf("files.getShares params %v, want file_id", p)
	}
	if info.ConversationCount != 3 || info.TabCount != 1 || info.FileChannelCount != 0 {
		t.Errorf("counts %d %d %d, want 3 1 0", info.ConversationCount, info.TabCount, info.FileChannelCount)
	}
	cs := info.Conversations
	if len(cs) != 2 || cs[0].Channel != "D1" || cs[0].ThreadTS != "1700000400.000100" || cs[0].Public ||
		cs[1].Channel != "C1" || cs[1].TS != "1700000000.000100" || !cs[1].Public {
		t.Fatalf("conversations %+v, want D1's newer share, then C1's (the zero ts skipped)", cs)
	}
	if _, ok := a.FileShareCard(cs[1]); ok {
		t.Error("a card for a share whose message the store lacks")
	}
	a.S.SetLatest("C1", []slack.Message{{TS: "1700000000.000100", User: "U1", Text: "the email"}}, false)
	a.S.SetLatest("D1", []slack.Message{{TS: "1700000500.000100", User: "USELF", ThreadTS: "1700000400.000100"}}, false)
	if ls, ok := a.FileShareCard(cs[1]); !ok || ls.Name != "general" || ls.IM || ls.Message.TS != "1700000000.000100" {
		t.Errorf("C1's card %+v %v", ls, ok)
	}
	if ls, ok := a.FileShareCard(cs[0]); !ok || !ls.IM || ls.Name != "Alice" {
		t.Errorf("D1's card %+v %v, want the IM named for Alice", ls, ok)
	}
	f.reply["files.getShares"] = `{"ok":false,"error":"file_not_found"}`
	if _, err := a.FileShares(context.Background(), "F0GONE"); err == nil {
		t.Error("a refused files.getShares read no error")
	}
}
