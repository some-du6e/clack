package core

// Slash commands, as Slack's web client runs them (gantry-v2, 2026-11):
//
//   - chatCommandApi (application.js "Send a slash command to the API"):
//     chat.command {command, disp, text, blocks?, channel, client_token,
//     thread_ts?, team_id?, app_id?}. client_token comes from
//     clientToken.generateAndEnqueue, the same queue blocks.actions and
//     apps.actions.v2.execute use. An app answering the command with
//     views.open(trigger_id) makes Slack push view_opened {client_token}
//     on the socket, and viewOpened shows the modal only when that token is
//     one this client queued (and is younger than timeout_range). Without
//     the token on chat.command the app's modal never opens: that was
//     "/se anchor didn't show me the modal".
//   - executeCommand ("Run a local or server slash command"): the API's
//     error codes become RESTRICTED_COMMAND (restricted_action),
//     UNKNOWN_COMMAND (unknown_command), DLP_WARNING (dlp_user_warning),
//     UNSUPPORTED_IN_THREAD (restricted_in_threads), else UNKNOWN_API_ERROR
//     with feedback = the error code (or the exception's message). An error
//     whose data has silence_error is dropped. With the
//     slash_command_failed_dedupe experiment an unknown command that failed
//     with unknown_command in the same channel within 30 s fails again
//     locally without calling the API. keep_input puts the text back.
//   - handleSlashCommand ("Intercept and handle a slash commands when
//     sending a message"): first removes the earlier command feedback
//     (ephemeralType temp_slash_cmd_feedback, threads_ in a thread), then
//     shows the response, or the failure text (CommandFailureText), as
//     Slackbot's temporary ephemeral (addEphemeralFeedback,
//     slackbot_feels sad_surprise for failures). The composer is not
//     restored on failure.
//   - Ephemeral replies an app sends later through response_url arrive on
//     the socket as message events with is_ephemeral (interactive.go).

import (
	"context"
	"encoding/json"
	"errors"
	"net/url"
	"strings"
	"sync"
	"time"

	"github.com/some-du6e/clack/internal/slack"
)

// Command failure kinds (Slack's nK.gu error names).
const (
	CmdRestricted          = "RESTRICTED_COMMAND"
	CmdUnknown             = "UNKNOWN_COMMAND"
	CmdUnknownAPIError     = "UNKNOWN_API_ERROR"
	CmdDLPWarning          = "DLP_WARNING"
	CmdUnsupportedInThread = "UNSUPPORTED_IN_THREAD"
)

// CommandError is a failed server command, classified like executeCommand.
type CommandError struct {
	Command  string // as typed ("/se")
	Kind     string // Cmd* above
	Feedback string // UNKNOWN_API_ERROR: the API's error code
	Silent   bool   // the error data asked for silence_error
	Err      error
}

func (e *CommandError) Error() string {
	if e.Feedback != "" {
		return e.Command + ": " + e.Kind + " (" + e.Feedback + ")"
	}
	return e.Command + ": " + e.Kind
}

func (e *CommandError) Unwrap() error { return e.Err }

// CommandFailureText is handleSlashCommand's Slackbot text for a failed
// command ("" when nothing is shown: silence_error, or no error).
func CommandFailureText(err error, command string) string {
	if err == nil {
		return ""
	}
	var ce *CommandError
	if !errors.As(err, &ce) {
		ce = &CommandError{Command: command, Kind: CmdUnknownAPIError, Feedback: err.Error()}
	}
	if ce.Silent {
		return ""
	}
	cmd := command
	if cmd == "" {
		cmd = ce.Command
	}
	switch {
	case ce.Kind == CmdRestricted:
		return "*" + cmd + "* failed because you are not allowed to perform that action. Talk to your Workspace Owner."
	case ce.Kind == CmdUnknown:
		return "*" + cmd + "* is not a valid command. In Slack, all messages that start with the \"/\" character are interpreted as commands.\n\n" +
			"If you are trying to send a message and not run a command, try preceding the \"/\" with an empty space."
	case ce.Kind == CmdUnsupportedInThread:
		return "*" + cmd + "* is not supported in threads. Sorry!"
	case ce.Kind == CmdDLPWarning:
		return "This message may include sensitive info that your admins don’t want shared. Make sure everything looks okay before sending."
	case ce.Kind == CmdUnknownAPIError && ce.Feedback == "dispatch_failed":
		return "*" + cmd + "* failed because the app did not respond. Please try again or contact the app developer."
	case ce.Kind == CmdUnknownAPIError && ce.Feedback != "":
		return "*" + cmd + "* failed with the error \"" + ce.Feedback + "\""
	}
	return "*" + cmd + "* failed with an unknown error."
}

// classifyCommandError is executeCommand's catch.
func classifyCommandError(cmd string, err error) *CommandError {
	ce := &CommandError{Command: cmd, Err: err}
	code := err.Error()
	var se *slack.Error
	if errors.As(err, &se) {
		code = se.Code
		var data struct {
			SilenceError bool `json:"silence_error"`
		}
		if json.Unmarshal(se.Body, &data) == nil && data.SilenceError {
			ce.Silent = true
		}
	}
	switch code {
	case "restricted_action":
		ce.Kind = CmdRestricted
	case "unknown_command":
		ce.Kind = CmdUnknown
	case "dlp_user_warning":
		ce.Kind = CmdDLPWarning
	case "restricted_in_threads":
		ce.Kind = CmdUnsupportedInThread
	default:
		ce.Kind, ce.Feedback = CmdUnknownAPIError, code
	}
	return ce
}

// failedCommands is executeCommand's recently-failed map (channel:command
// → when it failed with unknown_command).
type failedCommands struct {
	mu sync.Mutex
	m  map[string]time.Time
}

const failedCommandTTL = 30 * time.Second

func (f *failedCommands) recent(ch, cmd string) bool {
	f.mu.Lock()
	defer f.mu.Unlock()
	t, ok := f.m[ch+":"+cmd]
	if !ok {
		return false
	}
	if time.Since(t) > failedCommandTTL {
		delete(f.m, ch+":"+cmd)
		return false
	}
	return true
}

func (f *failedCommands) mark(ch, cmd string, failed bool) {
	f.mu.Lock()
	defer f.mu.Unlock()
	if !failed {
		delete(f.m, ch+":"+cmd)
		return
	}
	if f.m == nil {
		f.m = map[string]time.Time{}
	}
	f.m[ch+":"+cmd] = time.Now()
}

// knownCommand reports whether cmd is in the cached commands.list (false
// before the list loaded), and its app.
func (a *App) knownCommand(cmd string) (slack.Command, bool) {
	a.mu.Lock()
	defer a.mu.Unlock()
	name := strings.ToLower(cmd)
	for _, c := range a.cmdCache {
		if strings.ToLower(c.Name) == name {
			return c, true
		}
	}
	return slack.Command{}, false
}

// threadCommands and threadChannelCommands are isSlashCommandEnabled's
// F and D (boot-shared yX/v): the commands that run in a thread, and
// those that do outside DMs and huddle channels.
var (
	threadCommands = map[string]bool{"/away": true, "/collapse": true, "/color": true, "/colour": true, "/dm": true,
		"/expand": true, "/feedback": true, "/keys": true, "/me": true, "/msg": true, "/open": true, "/s": true,
		"/search": true, "/shortcuts": true, "/shrug": true, "/slack_diagnostic_report": true, "/slackdevtools": true,
		"/star": true, "/gif": true, "/active": true, "/giphy": true, "/iamcal": true, "/mute": true, "/remind": true,
		"/tableflip": true}
	threadChannelCommands = map[string]bool{"/archive": true, "/remove": true, "/rename": true, "/topic": true, "/who": true}
)

// threadCommandOK is isSlashCommandEnabled's thread rule for a command
// Slack knows (executeCommand checks only those): an allowed one, a
// channel one outside a DM, /invite under invite_slash_command_in_threads,
// or one marked is_runnable_from_thread.
func (a *App) threadCommandOK(ch, cmd string, known slack.Command) bool {
	name := strings.ToLower(cmd)
	switch {
	case threadCommands[name]:
		return true
	case threadChannelCommands[name] && a.kindOf(ch) != "im":
		return true
	case name == "/invite" && a.experiment("invite_slash_command_in_threads") == "on":
		return true
	}
	return known.RunnableFromThread
}

// appCommand is executeCommand's lookup by the picked app with
// ia4_slash_command_fe on: the command of that name from app.
func (a *App) appCommand(cmd, app string) (slack.Command, bool) {
	a.mu.Lock()
	defer a.mu.Unlock()
	name := strings.ToLower(cmd)
	for _, c := range a.cmdCache {
		if strings.ToLower(c.Name) == name && c.AppID == app {
			return c, true
		}
	}
	return slack.Command{}, false
}

// serverCommand is chatCommandApi + executeCommand's server half.
func (a *App) serverCommand(ctx context.Context, cmd string, o CommandOpts) (CommandResult, error) {
	known, isKnown := a.knownCommand(cmd)
	ia4 := a.experiment("ia4_slash_command_fe") == "on"
	if ia4 && o.AppID != "" {
		// the slash command store keys commands by app and name: the one
		// the autocomplete picked
		if c, ok := a.appCommand(cmd, o.AppID); ok {
			known, isKnown = c, true
		}
	}
	if ia4 && isKnown && len(known.Teams) == 1 {
		o.TeamID = known.Teams[0] // teamId: $.teams[0] for a single workspace
	}
	if isKnown && o.ThreadTS != "" && !a.threadCommandOK(o.Channel, cmd, known) {
		// executeCommand: isSlashCommandEnabled says no, before the API
		return CommandResult{}, &CommandError{Command: cmd, Kind: CmdUnsupportedInThread}
	}
	dedupe := a.experiment("slash_command_failed_dedupe") == "on"
	if dedupe && !isKnown && a.cmdFailed.recent(o.Channel, cmd) {
		return CommandResult{}, &CommandError{Command: cmd, Kind: CmdUnknown}
	}
	name := cmd
	if isKnown {
		name = known.Name
	}
	p := url.Values{"command": {name}, "disp": {cmd}, "text": {o.Text}, "channel": {o.Channel},
		"client_token": {a.Interactive().ClientToken()}}
	if o.ThreadTS != "" {
		p.Set("thread_ts", o.ThreadTS)
	}
	if o.TeamID != "" {
		p.Set("team_id", o.TeamID)
	}
	if o.AppID != "" {
		p.Set("app_id", o.AppID)
	}
	var res struct {
		Response  string `json:"response"`
		KeepInput bool   `json:"keep_input"`
	}
	if err := a.C.Call(ctx, "chat.command", p, &res); err != nil {
		ce := classifyCommandError(cmd, err)
		if dedupe && ce.Kind == CmdUnknown {
			a.cmdFailed.mark(o.Channel, cmd, true)
		}
		return CommandResult{}, ce
	}
	if dedupe {
		a.cmdFailed.mark(o.Channel, cmd, false)
	}
	return CommandResult{Response: res.Response, ResponseDoc: a.Mrkdwn(res.Response), KeepInput: res.KeepInput}, nil
}

// Command feedback ephemeral types (Slack's ephemeral message types).
const (
	feedbackType       = "temp_slash_cmd_feedback"
	threadFeedbackType = "threads_temp_slash_cmd_feedback"
)

// ClearCommandFeedback is handleSlashCommand's removeEphemeralMessages
// ({ephemeralType}) before a command runs: the earlier commands' replies
// and failures go away (in threads, the thread ones).
func (a *App) ClearCommandFeedback(thread bool) {
	typ := feedbackType
	if thread {
		typ = threadFeedbackType
	}
	ix := a.Interactive()
	ix.mu.Lock()
	keys := ix.feedback[typ]
	delete(ix.feedback, typ)
	ix.mu.Unlock()
	for _, k := range keys {
		ch, ts, _ := strings.Cut(k, "/")
		if ix.Ephemeral(ch, ts) {
			ix.DismissEphemeral(ch, ts)
		}
	}
}

// CommandFeedback is addEphemeralFeedback for a command: Slackbot's
// temporary "Only visible to you" message in ch (or the thread), removed
// by the next command's ClearCommandFeedback.
func (a *App) CommandFeedback(ch, threadTS, text string) string {
	ts := a.LocalEphemeral(ch, threadTS, text)
	typ := feedbackType
	if threadTS != "" {
		typ = threadFeedbackType
	}
	ix := a.Interactive()
	ix.mu.Lock()
	if ix.feedback == nil {
		ix.feedback = map[string][]string{}
	}
	ix.feedback[typ] = append(ix.feedback[typ], ch+"/"+ts)
	ix.mu.Unlock()
	return ts
}
