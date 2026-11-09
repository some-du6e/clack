package core

import (
	"context"
	"encoding/json"
	"strings"
	"testing"

	"github.com/some-du6e/yoloslackgoclient/internal/fakeslack"
	"github.com/some-du6e/yoloslackgoclient/internal/slack"
	"github.com/some-du6e/yoloslackgoclient/internal/store"
)

// chat.command carries Slack's chatCommandApi fields, the client_token
// among them, and the modal the app opens with that token is ours.
func TestCommandSendsClientToken(t *testing.T) {
	a, f := testApp(t)
	f.reply["chat.command"] = `{"ok":true}`
	ix := a.Interactive()
	ch, stop := ix.Subscribe()
	defer stop()
	if _, err := a.Command(context.Background(), CommandOpts{Channel: "C1", Command: "/se", Text: "anchor", ThreadTS: "1.0", AppID: "A9"}); err != nil {
		t.Fatal(err)
	}
	p := f.last("chat.command")
	tok := p.Get("client_token")
	if p.Get("command") != "/se" || p.Get("disp") != "/se" || p.Get("text") != "anchor" || p.Get("channel") != "C1" ||
		p.Get("thread_ts") != "1.0" || p.Get("app_id") != "A9" || !strings.HasPrefix(tok, "web-") {
		t.Fatalf("chat.command params: %v", p)
	}
	ix.handle(rtm("view_opened", map[string]any{"view_id": "VS", "view_type": "modal", "client_token": tok, "timeout_range": 10000,
		"view": map[string]any{"id": "VS", "type": "modal", "title": map[string]any{"type": "plain_text", "text": "Anchor"}, "blocks": []any{}}}))
	if ev := nextView(t, ch, "opened"); ev.View.ID != "VS" || ev.ClientToken != tok {
		t.Errorf("opened: %+v", ev)
	}
}

// executeCommand's error classes and handleSlashCommand's Slackbot text.
func TestCommandFailureText(t *testing.T) {
	a, f := testApp(t)
	cases := []struct{ reply, want string }{
		{`{"ok":false,"error":"unknown_command"}`, "*/nope* is not a valid command. In Slack, all messages that start with the \"/\" character are interpreted as commands.\n\nIf you are trying to send a message and not run a command, try preceding the \"/\" with an empty space."},
		{`{"ok":false,"error":"restricted_action"}`, "*/nope* failed because you are not allowed to perform that action. Talk to your Workspace Owner."},
		{`{"ok":false,"error":"restricted_in_threads"}`, "*/nope* is not supported in threads. Sorry!"},
		{`{"ok":false,"error":"dlp_user_warning"}`, "This message may include sensitive info that your admins don\u2019t want shared. Make sure everything looks okay before sending."},
		{`{"ok":false,"error":"dispatch_failed"}`, "*/nope* failed because the app did not respond. Please try again or contact the app developer."},
		{`{"ok":false,"error":"invalid_arguments"}`, "*/nope* failed with the error \"invalid_arguments\""},
		{`{"ok":false,"error":"oops","silence_error":true}`, ""},
	}
	for _, c := range cases {
		f.reply["chat.command"] = c.reply
		_, err := a.Command(context.Background(), CommandOpts{Channel: "C1", Command: "/nope"})
		if err == nil {
			t.Fatalf("%s: no error", c.reply)
		}
		if got := CommandFailureText(err, "/nope"); got != c.want {
			t.Errorf("%s:\n got %q\nwant %q", c.reply, got, c.want)
		}
	}
	if got := CommandFailureText(&CommandError{Command: "/x", Kind: CmdUnknownAPIError}, "/x"); got != "*/x* failed with an unknown error." {
		t.Errorf("no feedback: %q", got)
	}
}

// Command feedback is Slackbot's temporary ephemeral; the next command
// removes it.
func TestCommandFeedbackReplaced(t *testing.T) {
	a, _ := testApp(t)
	ix := a.Interactive()
	ts := a.CommandFeedback("C1", "", "first")
	if !ix.Ephemeral("C1", ts) {
		t.Fatal("feedback isn't ephemeral")
	}
	tts := a.CommandFeedback("C1", "9.0", "in the thread")
	a.ClearCommandFeedback(false)
	if ix.Ephemeral("C1", ts) {
		t.Error("channel feedback survived the next command")
	}
	if !ix.Ephemeral("C1", tts) {
		t.Error("a channel command removed the thread's feedback")
	}
	a.ClearCommandFeedback(true)
	if ix.Ephemeral("C1", tts) {
		t.Error("thread feedback survived the next thread command")
	}
}

// The whole flow against fakeslack's Deploy Bot: /ticket opens the app's
// modal (view_opened for our token), the app's views.update follows,
// /ticket status replies through response_url, and the failures map to
// Slack's errors.
func TestTicketCommandFakeslack(t *testing.T) {
	fs, err := fakeslack.Start(fakeslack.Gallery)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = fs.Close() })
	a := New(slack.New(fs.Creds()), store.New(""))
	fs.Emit = func(ev map[string]any) {
		b, _ := json.Marshal(ev)
		typ, _ := ev["type"].(string)
		a.Inject(slack.Event{Type: typ, Raw: b})
	}
	ix := a.Interactive()
	ch, stop := ix.Subscribe()
	defer stop()
	ctx := context.Background()
	cmds, err := a.Commands(ctx, "tick")
	if err != nil || len(cmds) != 1 || cmds[0].AppID != fakeslack.AppAppID {
		t.Fatalf("commands.list: %+v %v", cmds, err)
	}
	gal := fakeslack.BlockKitGalleryID
	if _, err := a.Command(ctx, CommandOpts{Channel: gal, Command: fakeslack.TicketCommand, Text: "anchor"}); err != nil {
		t.Fatal(err)
	}
	ev := nextView(t, ch, "opened")
	if ev.View.ID != fakeslack.SurfTicketView {
		t.Fatalf("opened: %+v", ev.View)
	}
	ev = nextView(t, ch, "updated")
	if !strings.Contains(string(ev.View.Blocks), `"initial_value":"anchor"`) {
		t.Errorf("views.update: %s", ev.View.Blocks)
	}
	calls := fs.CommandApp().Snapshot()
	if len(calls) != 1 || !strings.HasPrefix(calls[0].Get("client_token"), "web-") || calls[0].Get("disp") != fakeslack.TicketCommand {
		t.Errorf("chat.command: %v", calls)
	}

	if _, err := a.Command(ctx, CommandOpts{Channel: gal, Command: fakeslack.TicketCommand, Text: "status"}); err != nil {
		t.Fatal(err)
	}
	if ev = nextView(t, ch, "ephemeral"); ev.Channel != gal || !ix.Ephemeral(gal, ev.TS) {
		t.Errorf("response_url ephemeral: %+v", ev)
	}

	_, err = a.Command(ctx, CommandOpts{Channel: gal, Command: fakeslack.TicketCommand, Text: "timeout"})
	if got := CommandFailureText(err, "/ticket"); !strings.Contains(got, "the app did not respond") {
		t.Errorf("timeout: %q (%v)", got, err)
	}
	_, err = a.Command(ctx, CommandOpts{Channel: gal, Command: "/nope"})
	if got := CommandFailureText(err, "/nope"); !strings.Contains(got, "is not a valid command") {
		t.Errorf("unknown: %q (%v)", got, err)
	}
}
