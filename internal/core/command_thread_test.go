package core

import (
	"context"
	"errors"
	"testing"

	"github.com/some-du6e/clack/internal/slack"
)

// isSlashCommandEnabled's thread rule (yX/v): in a thread a known command
// runs only when allowed there; /invite needs invite_slash_command_in_threads;
// is_runnable_from_thread commands always run. Refused ones fail locally
// with UNSUPPORTED_IN_THREAD, before chat.command.
func TestCommandsInThreads(t *testing.T) {
	a, f := testApp(t)
	f.reply["chat.command"] = `{"ok":true}`
	a.mu.Lock()
	a.cmdCache = []slack.Command{{Name: "/invite"}, {Name: "/poll", RunnableFromThread: true}, {Name: "/deploy"}, {Name: "/remind"}}
	a.mu.Unlock()
	run := func(cmd, ts string) error {
		_, err := a.Command(context.Background(), CommandOpts{Channel: "C1", Command: cmd, ThreadTS: ts})
		return err
	}
	unsupported := func(err error) bool {
		var ce *CommandError
		return errors.As(err, &ce) && ce.Kind == CmdUnsupportedInThread
	}
	for _, g := range []string{"off", ""} {
		a.SetExperimentGroup("invite_slash_command_in_threads", g)
		if err := run("/invite", "1.0"); !unsupported(err) {
			t.Errorf("group %q: /invite in a thread: %v", g, err)
		}
	}
	a.SetExperimentGroup("invite_slash_command_in_threads", "on")
	if err := run("/invite", "1.0"); unsupported(err) { // runs (locally in a channel: invite_command.go)
		t.Errorf("on: /invite in a thread: %v", err)
	}
	if err := run("/poll", "1.0"); err != nil {
		t.Errorf("a runnable-from-thread command: %v", err)
	}
	if err := run("/remind", "1.0"); err != nil {
		t.Errorf("an allowed command: %v", err)
	}
	if err := run("/deploy", "1.0"); !unsupported(err) {
		t.Errorf("/deploy in a thread: %v", err)
	}
	if err := run("/deploy", ""); err != nil {
		t.Errorf("/deploy in the channel: %v", err)
	}
	if got := CommandFailureText(&CommandError{Command: "/deploy", Kind: CmdUnsupportedInThread}, "/deploy"); got != "*/deploy* is not supported in threads. Sorry!" {
		t.Errorf("feedback %q", got)
	}
}

// ia4_slash_command_fe (5dbN executeCommand): the picked app's command is
// the one checked, and a command installed in one workspace sends that
// workspace as team_id; off, neither.
func TestIA4CommandTeam(t *testing.T) {
	a, f := testApp(t)
	f.reply["chat.command"] = `{"ok":true}`
	a.mu.Lock()
	a.cmdCache = []slack.Command{
		{Name: "/deploy", AppID: "A1", Teams: []string{"T9"}},
		{Name: "/deploy", AppID: "A2", Teams: []string{"T1", "T9"}, RunnableFromThread: true},
	}
	a.mu.Unlock()
	run := func(app, ts string) (string, error) {
		_, err := a.Command(context.Background(), CommandOpts{Channel: "C1", Command: "/deploy", ThreadTS: ts, AppID: app})
		if err != nil {
			return "", err
		}
		p := f.last("chat.command")
		if p.Get("app_id") != app {
			t.Errorf("app_id %q, want %q", p.Get("app_id"), app)
		}
		return p.Get("team_id"), nil
	}
	a.SetExperimentGroup("ia4_slash_command_fe", "on")
	if team, err := run("A1", ""); err != nil || team != "T9" {
		t.Errorf("on, one workspace: team %q err %v", team, err)
	}
	if team, err := run("A2", ""); err != nil || team != "" {
		t.Errorf("on, two workspaces: team %q err %v", team, err)
	}
	if _, err := run("A2", "1.0"); err != nil {
		t.Errorf("on: A2's thread-runnable /deploy refused in a thread: %v", err)
	}
	a.SetExperimentGroup("ia4_slash_command_fe", "off")
	if team, err := run("A1", ""); err != nil || team != "" {
		t.Errorf("off: team %q err %v", team, err)
	}
}
