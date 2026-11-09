package composer

import (
	"context"
	"errors"

	"github.com/some-du6e/yoloslackgoclient/internal/core"
)

// runCommand is Slack's handleSlashCommand around executeCommand: the
// earlier command feedback goes away, the command runs (chat.command
// with a client_token, so the modal an app opens with the trigger shows
// here), and its response, or Slack's text for why it failed ("*/se*
// failed with the error …"), shows as Slackbot's temporary ephemeral in
// the conversation (core/command.go). The composer isn't restored on a
// failure; keep_input asks for the text back.
func runCommand(ctx context.Context, api coreAPI, o core.CommandOpts) (core.CommandResult, error) {
	api.clearCommandFeedback(o.ThreadTS != "")
	res, err := api.command(ctx, o)
	if err != nil {
		if txt := core.CommandFailureText(err, o.Command); txt != "" {
			api.commandFeedback(o.Channel, o.ThreadTS, txt)
		}
		return res, &commandFailed{err}
	}
	if res.Response != "" {
		api.commandFeedback(o.Channel, o.ThreadTS, res.Response)
	}
	return res, nil
}

// commandFailed is a command whose failure Slackbot already explained.
type commandFailed struct{ err error }

func (e *commandFailed) Error() string { return e.err.Error() }
func (e *commandFailed) Unwrap() error { return e.err }

func isCommandFailed(err error) bool {
	var cf *commandFailed
	return errors.As(err, &cf)
}
