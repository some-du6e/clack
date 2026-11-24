package core

import (
	"context"
	"encoding/json"
	"strings"
	"testing"

	"github.com/some-du6e/clack/internal/slack"
)

// executeCommand's split between the server and the local commands
// (boot-render pymN, Zrd3 getCommands; build 9f450b8ad3db): /shrug and /me
// have no local action, so they go to chat.command with their blocks and
// thread like any server command; /topic is local (MH7n topicCommand).
func TestShrugAndMeAreServerCommands(t *testing.T) {
	a, f := testApp(t)
	f.reply["chat.command"] = `{"ok":true}`
	ctx := context.Background()
	blocks := json.RawMessage(`[{"type":"rich_text","elements":[{"type":"rich_text_section","elements":[{"type":"user","user_id":"U082DRXPMF1"}]}]}]`)
	for _, cmd := range []string{"/shrug", "/me"} {
		if _, err := a.Command(ctx, CommandOpts{Channel: "C1", ThreadTS: "1700000000.000100", Command: cmd, Disp: cmd,
			Blocks: blocks, Params: []CommandParam{{Type: "mention", Value: "U082DRXPMF1"}}}); err != nil {
			t.Fatalf("%s: %v", cmd, err)
		}
		p := f.last("chat.command")
		if p.Get("command") != cmd || p.Get("blocks") != string(blocks) || p.Get("thread_ts") != "1700000000.000100" || p.Has("text") {
			t.Errorf("%s: chat.command %v", cmd, p)
		}
	}
	if n := countCalls(f, "chat.postMessage") + countCalls(f, "chat.meMessage"); n != 0 {
		t.Errorf("%d local posts for /shrug or /me", n)
	}
}

// chatCommandApi's command is the matched command's name (the typed word,
// lowercased, when none matches) and disp the word as typed;
// is_match_for_command reads a full-width slash as "/" and the command's
// aliases and canonical name too.
func TestCommandNameAndDisp(t *testing.T) {
	a, f := testApp(t)
	f.reply["chat.command"] = `{"ok":true}`
	a.mu.Lock()
	a.cmdCache = []slack.Command{{Name: "/remind", Type: "core"}, {Name: "/giphy", Aliases: []string{"/gif"}}}
	a.mu.Unlock()
	ctx := context.Background()
	for _, c := range []struct{ cmd, disp, want string }{
		{"/remind", "/Remind", "/remind"},
		{"／remind", "／remind", "/remind"},
		{"/gif", "/gif", "/giphy"},
		{"/nope", "/NoPe", "/nope"},
	} {
		if _, err := a.Command(ctx, CommandOpts{Channel: "C1", Command: c.cmd, Disp: c.disp, Params: []CommandParam{}}); err != nil {
			t.Fatalf("%s: %v", c.cmd, err)
		}
		if p := f.last("chat.command"); p.Get("command") != c.want || p.Get("disp") != c.disp {
			t.Errorf("%s: command %q disp %q, want %q %q", c.cmd, p.Get("command"), p.Get("disp"), c.want, c.disp)
		}
	}
	if !commandMatches(slack.Command{Name: "/café"}, "/cafe") || !commandMatches(slack.Command{Name: "/remind"}, "/remind`") {
		t.Error("is_match_for_command: deburred names and a trailing accent mark")
	}
}

// topicCommand: the arguments' texty value is the topic; nothing for an
// empty one; Slack's too-long and error texts (CUSTOM_ERROR, shown as is).
func TestTopicCommandLocal(t *testing.T) {
	a, f := testApp(t)
	f.reply["conversations.setTopic"] = `{"ok":true,"channel":{"id":"C1","name":"general"}}`
	f.reply["info"] = `{"ok":true,"allowed":["SET_CHANNEL_TOPIC"]}` // edge permissions/info: isSlashCommandEnabled's check
	ctx := context.Background()
	if err := a.LoadChannelGrants(ctx, "C1"); err != nil { // as the channel's header asked when it opened
		t.Fatal(err)
	}
	run := func(text string) error {
		_, err := a.Command(ctx, CommandOpts{Channel: "C1", Command: "/topic", Disp: "/topic", Text: text, Params: []CommandParam{}})
		return err
	}
	if err := run("<@U1|@alice> owns the release"); err != nil {
		t.Fatal(err)
	}
	if p := f.last("conversations.setTopic"); p.Get("topic") != "<@U1|@alice> owns the release" || p.Get("channel") != "C1" ||
		p.Get("reason") != "submitChannelTopic" {
		t.Errorf("setTopic %v", p)
	}
	// the composer's Submit (submitChannelTopic's cleaning) is the topic
	// that goes out; the limit reads the value
	if _, err := a.Command(ctx, topicOpts("C1", "<@U1|@alice> & co", "<@U1> &amp; co")); err != nil {
		t.Fatal(err)
	}
	if p := f.last("conversations.setTopic"); p.Get("topic") != "<@U1> &amp; co" {
		t.Errorf("cleaned topic %q", p.Get("topic"))
	}
	if countCalls(f, "chat.command") != 0 {
		t.Error("/topic went to chat.command")
	}
	n := countCalls(f, "conversations.setTopic")
	if err := run(""); err != nil || countCalls(f, "conversations.setTopic") != n {
		t.Errorf("an empty /topic: %v, %d calls", err, countCalls(f, "conversations.setTopic")-n)
	}
	if got := feedbackOf(run(strings.Repeat("x", 251))); got != "Channel topics can only include up to 250 characters — please make this topic shorter." {
		t.Errorf("too long: %q", got)
	}
	f.reply["conversations.setTopic"] = `{"ok":false,"error":"restricted_action"}`
	if got := feedbackOf(run("x")); got != "It looks like you don’t have permission to change the channel topic. Please check with your workspace admin." {
		t.Errorf("restricted: %q", got)
	}
}

// The reminder modal's save for "/remind" alone: saved.add {item_type:
// reminder, description: the blocks, client_id, date_due}, no todo_state.
func TestAddReminder(t *testing.T) {
	a, f := testApp(t)
	f.reply["saved.add"] = `{"ok":true}`
	desc := json.RawMessage(`[{"type":"rich_text","elements":[{"type":"rich_text_section","elements":[{"type":"text","text":"stretch"}]}]}]`)
	if err := a.AddReminder(context.Background(), desc, 1700003600); err != nil {
		t.Fatal(err)
	}
	p := f.last("saved.add")
	if p.Get("item_type") != "reminder" || p.Get("description") != string(desc) || p.Get("date_due") != "1700003600" || p.Get("client_id") == "" || p.Has("todo_state") {
		t.Errorf("saved.add %v", p)
	}
	if err := a.AddReminder(context.Background(), nil, 1); err == nil {
		t.Error("a reminder without a description was saved")
	}
}
