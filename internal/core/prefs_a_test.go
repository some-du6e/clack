package core

import (
	"encoding/json"
	"strings"
	"testing"

	"github.com/some-du6e/yoloslackgoclient/internal/slack"
)

// should_show_connect_section: externally shared conversations outside a
// custom section go to the slack_connect section (uSoP); off, there is no
// such section and they stay in Channels.
func TestConnectSectionPref(t *testing.T) {
	a, _ := testApp(t)
	a.S.PutConv(slack.Conversation{ID: "CX", Name: "partners", IsChannel: true, IsExtShared: true})
	a.S.Sections = []slack.Section{
		{ID: "L1", Type: "channels", Next: "L2"}, {ID: "L2", Type: "slack_connect", Next: "L3"}, {ID: "L3", Type: "direct_messages"},
	}
	close(a.ready)
	layout := func(on bool) string {
		a.S.SetRawPrefs(map[string]json.RawMessage{"should_show_connect_section": json.RawMessage(map[bool]string{true: "true", false: "false"}[on])})
		var out []string
		for _, s := range a.Snapshot().Sections {
			if s.Type == "coding_channels" {
				continue
			}
			out = append(out, s.Type+"="+strings.Join(s.Channels, ","))
		}
		return strings.Join(out, " ")
	}
	if got := layout(true); got != "channels=C1 slack_connect=CX direct_messages=D1" {
		t.Errorf("on: %s", got)
	}
	if got := layout(false); got != "channels=C1,CX direct_messages=D1" {
		t.Errorf("off: %s", got)
	}
}

// Channel keywords count as mentions (checkForMentionInMessage), but not
// in link previews or inside an @mention.
func TestKeywordMention(t *testing.T) {
	kw := []string{"deploy", "ship it"}
	for _, c := range []struct {
		m    slack.Message
		want bool
	}{
		{slack.Message{Text: "Time to DEPLOY"}, true},
		{slack.Message{Text: "we ship it today"}, true},
		{slack.Message{Text: "redeployed"}, false},
		{slack.Message{Text: "hey <@U1|deploy>"}, false},
		{slack.Message{Attachments: []slack.Attachment{{Text: "deploy notes"}}}, true},
		{slack.Message{Attachments: []slack.Attachment{{Text: "deploy notes", FromURL: "https://x"}}}, false},
	} {
		if got := keywordMention(&c.m, kw); got != c.want {
			t.Errorf("%+v: %v, want %v", c.m, got, c.want)
		}
	}
	if keywordMention(&slack.Message{Text: "deploy"}, nil) {
		t.Error("no keywords matched")
	}
}
