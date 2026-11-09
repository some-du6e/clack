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

// VIP unreads (priority_sidebar_section): a DM or group DM with a VIP that
// has unread mentions moves to Slack's client-local section, first;
// with vip_section_pref on it needs the pref.
func TestPrioritySection(t *testing.T) {
	a, _ := testApp(t)
	a.S.PutConv(slack.Conversation{ID: "D2", IsIM: true, User: "U2"})
	a.S.Sections = []slack.Section{{ID: "L1", Type: "channels", Next: "L3"}, {ID: "L3", Type: "direct_messages"}}
	a.S.SetCounts(&slack.Counts{IMs: []slack.CountEntry{{ID: "D1", Latest: "20.0", LastRead: "10.0", HasUnreads: true, MentionCount: 1},
		{ID: "D2", Latest: "20.0", LastRead: "10.0", HasUnreads: true, MentionCount: 2}}})
	close(a.ready)
	prefs := func(kv string) {
		raw := map[string]json.RawMessage{}
		_ = json.Unmarshal([]byte(kv), &raw)
		a.S.SetRawPrefs(raw)
	}
	layout := func() string {
		var out []string
		for _, s := range a.Snapshot().Sections {
			if s.Type == "coding_channels" {
				continue
			}
			out = append(out, s.Type+"="+strings.Join(s.Channels, ","))
		}
		return strings.Join(out, " ")
	}
	prefs(`{}`)
	if got := layout(); strings.Contains(got, "priority") {
		t.Fatalf("a VIP unreads section without VIPs: %s", got)
	}
	prefs(`{"vip_users":"U1"}`)
	if got := layout(); !strings.HasPrefix(got, "priority=D1 ") || strings.Contains(got, "direct_messages=D1") {
		t.Fatalf("VIP DM not in VIP unreads: %s", got)
	}
	a.SetExperimentGroup("vip_section_pref", "on")
	if got := layout(); strings.Contains(got, "priority") {
		t.Fatalf("vip_section_pref on and the pref off: %s", got)
	}
	prefs(`{"vip_users":"U1","priority_sidebar_section":true}`)
	if got := layout(); !strings.HasPrefix(got, "priority=D1 ") {
		t.Fatalf("the pref on: %s", got)
	}
}
