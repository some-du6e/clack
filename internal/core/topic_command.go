package core

import (
	"context"
	"encoding/json"
	"errors"
	"net/url"
	"strconv"

	"github.com/some-du6e/clack/internal/slack"
)

// topicCommand is Slack's local /topic (slash-commands-local MH7n
// topicCommand, build 9f450b8ad3db): the topic is the arguments as the
// texty input's value (convertContentsToString of deltaWithoutCmd: the
// composer hands it over as Text, mentions as <@U…|label>). An empty one
// does nothing; one longer than 250 characters (80 in a group DM under
// mpdm_naming; channelTopicExceedsLimit) fails with the too-long text;
// else submitChannelTopic (s0VR) sends it cleaned (Submit, as the Edit
// topic dialog does: <@U…> for a mention, & < > escaped, phone numbers
// linked), its failure in getChannelTopicError's words. Both failures are
// CUSTOM_ERROR: Slackbot shows the text as is.
func (a *App) topicCommand(ctx context.Context, o CommandOpts, cmd string) (CommandResult, error) {
	if o.ThreadTS != "" && !a.threadCommandOK(o.Channel, cmd, slack.Command{}) {
		return CommandResult{}, &CommandError{Command: cmd, Kind: CmdUnsupportedInThread}
	}
	topic := o.Text
	if topic == "" {
		return CommandResult{}, nil
	}
	max := 250
	if a.experiment("mpdm_naming") == "on" && a.kindOf(o.Channel) == "mpim" {
		max = 80
	}
	if jsLen(topic) > max {
		return CommandResult{}, customErr(cmd, channelTopicError("too_long"))
	}
	// submitChannelTopic: the value cleaned (o.Submit), with its reason
	if o.Submit != "" {
		topic = o.Submit
	}
	if err := a.SubmitChannelTopic(ctx, o.Channel, topic); err != nil {
		code := ""
		var se *slack.Error
		if errors.As(err, &se) {
			code = se.Code
		}
		return CommandResult{}, customErr(cmd, channelTopicError(code))
	}
	return CommandResult{}, nil
}

// channelTopicError is getChannelTopicError (MgZJ Bw) without the group
// DM limit, as topicCommand calls it.
func channelTopicError(code string) string {
	switch code {
	case "too_long":
		return "Channel topics can only include up to 250 characters — please make this topic shorter."
	case "restricted_action", "user_is_restricted":
		return "It looks like you don’t have permission to change the channel topic. Please check with your workspace admin."
	case "method_not_supported_for_channel_type":
		return "It’s not possible to set a topic here, unfortunately."
	case "method_not_supported_for_dm":
		return "Regrettably, direct messages do not have topics."
	}
	return "It looks like setting the channel topic didn’t work, for some reason. Try again?"
}

// AddReminder is the reminder modal's save without a message
// (openLaterCustomModal isMsgContext false, WUPY addReminder →
// saveReminder): saved.add {item_type: reminder, description: the
// description's blocks as JSON, client_id, date_due}. A plain reminder
// (not a later to-do) has no todo_state (getTodoState is undefined).
func (a *App) AddReminder(ctx context.Context, description json.RawMessage, dueAt int64) error {
	if len(description) == 0 {
		return errors.New("LATER saveReminder() requires a description argument")
	}
	p := url.Values{"item_type": {"reminder"}, "description": {string(description)}, "client_id": {clientMsgID()},
		"date_due": {strconv.FormatInt(dueAt, 10)}}
	return a.C.Call(ctx, "saved.add", p, nil)
}
