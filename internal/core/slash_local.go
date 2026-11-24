package core

import (
	"context"
	"errors"
	"net/url"

	"github.com/some-du6e/clack/internal/slack"
)

// Slack's client-side slash commands (boot-render Zrd3 getCommands, build
// 9f450b8ad3db): the slash command store lists them beside the server's
// (type "client", with their aliases), so is_match_for_command finds them by
// name or alias, and executeCommand runs the local action of the canonical
// name it matched (Zrd3 XU getCommandByName, keyed by canonical_name ??
// name). The slack* developer commands and the multiselect previews are
// left out: Clack has nothing behind them.
var clientCommands = []slack.Command{
	{Name: "/archive"}, {Name: "/collapse"}, {Name: "/color", Aliases: []string{"/colour"}}, {Name: "/expand"},
	{Name: "/invite"}, {Name: "/invite_people"}, {Name: "/leave", Aliases: []string{"/close", "/part"}},
	{Name: "/msg", Aliases: []string{"/dm"}}, {Name: "/open", Aliases: []string{"/join"}},
	{Name: "/remove", Aliases: []string{"/kick"}}, {Name: "/rename"}, {Name: "/search", Aliases: []string{"/s"}},
	{Name: "/shortcuts", Aliases: []string{"/keys"}}, {Name: "/topic"}, {Name: "/feedback"}, {Name: "/gif"},
}

// ClientCommand is the canonical name of the client command typed (the
// command word as parseSlashCmdArgsFromDelta gives it): "/keys" is
// "/shortcuts". false when it names none.
func ClientCommand(typed string) (string, bool) {
	for _, c := range clientCommands {
		if commandMatches(c, typed) {
			return canonicalCommand(c), true
		}
	}
	return "", false
}

// LocalCommand is the canonical name executeCommand looks a typed command
// up by: the matched command's in the slash command store (the server's
// commands.list, then the client commands), else the word itself.
func (a *App) LocalCommand(typed string) string {
	if k, ok := a.knownCommand(typed); ok {
		return canonicalCommand(k)
	}
	if c, ok := ClientCommand(typed); ok {
		return c
	}
	return typed
}

// CanCreateChannels is 8cBv MM (the channel browser's and /open's
// canCreateChannel): CREATE_PUBLIC_CHANNEL or CREATE_PRIVATE_CHANNEL
// somewhere (permissions/info), and not a guest.
func (a *App) CanCreateChannels(ctx context.Context) bool {
	body := map[string]any{"user_id": a.S.SelfID(), "permissions": []string{"CREATE_PUBLIC_CHANNEL", "CREATE_PRIVATE_CHANNEL"}}
	if a.C.Creds.EnterpriseID != "" {
		body["team_ids"] = a.selfWorkspaceIDs()
	} else {
		body["as_admin"] = true
	}
	var r struct {
		Allowed        []string            `json:"allowed"`
		AllowedPerTeam map[string][]string `json:"allowed_per_team"`
	}
	if err := a.C.Edge(ctx, "permissions/info", body, &r); err != nil {
		return false
	}
	has := len(r.Allowed) > 0
	for _, ps := range r.AllowedPerTeam {
		has = has || len(ps) > 0
	}
	return has && !a.viewerRestricted()
}

// SubmitUserFeedback is the /feedback modal's Submit
// (feedbackSubmitUserFeedbackFetcher, VhHZ): feedback.submitUserFeedback
// {product_area, product_feedback, source: slash_feedback} (the fetcher
// sends its camelCase arguments in snake case).
func (a *App) SubmitUserFeedback(ctx context.Context, area, feedback string) error {
	return a.C.Call(ctx, "feedback.submitUserFeedback", url.Values{"product_area": {area}, "product_feedback": {feedback},
		"source": {"slash_feedback"}}, nil)
}

// RateLimited reports a failure the feedback modal calls the rate limit:
// HTTP 429, or Slack's ratelimited answer with 200
// (HTTP_429_TOO_MANY_REQUESTS, HTTP_200_RATELIMITED).
func RateLimited(err error) bool {
	if errors.Is(err, slack.RateLimited) {
		return true
	}
	var he *slack.HTTPError
	if errors.As(err, &he) && he.Status == 429 {
		return true
	}
	var se *slack.Error
	return errors.As(err, &se) && (se.Code == "ratelimited" || se.Status == 429)
}

// LocalTarget is where /open or /msg takes the viewer.
type LocalTarget struct {
	// Browse: no arguments (/open's channel browser, /msg's DMs)
	Browse bool
	// Create: /open found no channel and the viewer may create one
	// (rejectChannelNotFound's openChannelCreateModal, entryPoint
	// slash_command), with Name filled in
	Create bool
	Name   string
	// Conv is the conversation to switch to: the channel joined, or the
	// person's DM opened
	Conv string
}

// OpenCommand is /open (/join; slash-commands-local 8FiW openCommand) for
// the parsed arguments; name is the first argument as typed (a channel
// mention's text or the first text, "#" and spaces trimmed):
//
//   - no arguments: the channel browser;
//   - a first argument that is no mention, or a channel Slack doesn't
//     know: rejectChannelNotFound, the create-channel modal when the
//     viewer may create channels (8cBv canCreateChannel), else "I
//     couldn’t find a channel named “{channel_name}”, sorry!";
//   - a member: their DM (startImByMemberId), "A valid member name is
//     required." when that fails;
//   - another workspace's channel: "*{cmd}* can’t open or join channels
//     outside this workspace.";
//   - a channel: joinAndSwitchToChannel (KcgN: joined unless the viewer is
//     in it, an archived one shown anyway), "There was a problem joining
//     “{channel_name}”, sorry!" when the join fails.
//
// isSlashCommandEnabled (P+cE mE) turns /open down for a guest first:
// UNKNOWN_COMMAND.
func (a *App) OpenCommand(ctx context.Context, cmd string, params []CommandParam, name string) (LocalTarget, error) {
	if a.Self().Guest {
		return LocalTarget{}, &CommandError{Command: cmd, Kind: CmdUnknown}
	}
	if len(params) == 0 {
		return LocalTarget{Browse: true}, nil
	}
	notFound := func() (LocalTarget, error) {
		if a.CanCreateChannels(ctx) {
			return LocalTarget{Create: true, Name: name}, nil
		}
		return LocalTarget{}, customErr(cmd, "I couldn’t find a channel named “"+name+"”, sorry!")
	}
	if params[0].Type != "mention" || params[0].Value == "" {
		return notFound()
	}
	id := params[0].Value
	if LooksLikeMemberID(id) {
		c, err := a.OpenDM(ctx, id)
		if err != nil || c.ID == "" {
			return LocalTarget{}, customErr(cmd, "A valid member name is required.")
		}
		return LocalTarget{Conv: c.ID}, nil
	}
	c, ok := a.convInfo(id)
	if !ok {
		return notFound()
	}
	if c.otherTeam {
		return LocalTarget{}, customErr(cmd, "*"+cmd+"* can’t open or join channels outside this workspace.")
	}
	if err := a.joinToSwitch(ctx, id, c); err != nil {
		return LocalTarget{}, customErr(cmd, "There was a problem joining “"+name+"”, sorry!")
	}
	return LocalTarget{Conv: id}, nil
}

// MsgCommand is /msg (/dm; slash-commands-local 6bdk msgCommand) for the
// parsed arguments; hasText reports a message after the first argument
// (trimDelta of the rest, not empty):
//
//   - no arguments: the DMs (openDmBrowser);
//   - a first argument that is no mention: "A valid member name is
//     required."; a user group: "*{cmd}* can’t send message to user
//     groups.";
//   - a member Slack doesn't hold: "A valid member name is required.", a
//     deactivated one with a message "That user has been deactivated.";
//     else their DM, opened;
//   - a channel Slack doesn't know: "A valid channel name is required.",
//     another workspace's "*{cmd}* can’t send message to channels outside
//     this workspace.", an archived one with a message "That channel has
//     been archived." ("That private channel has been archived."); else
//     the channel, joined (joinAndSwitchToChannel).
//
// A failed call is "A valid member name is required." for user_not_found,
// "A valid channel name is required." for channel_not_found, else "Hmm,
// something went wrong. Try again?". The composer sends the message.
func (a *App) MsgCommand(ctx context.Context, cmd string, params []CommandParam, hasText bool) (LocalTarget, error) {
	if len(params) == 0 {
		return LocalTarget{Browse: true}, nil
	}
	invalidMember := customErr(cmd, "A valid member name is required.")
	invalidChannel := customErr(cmd, "A valid channel name is required.")
	fallback := customErr(cmd, "Hmm, something went wrong. Try again?")
	if params[0].Type != "mention" || params[0].Value == "" {
		return LocalTarget{}, invalidMember
	}
	id := params[0].Value
	failed := func(err error) error {
		var se *slack.Error
		if errors.As(err, &se) {
			switch se.Code {
			case "user_not_found":
				return invalidMember
			case "channel_not_found":
				return invalidChannel
			}
		}
		return fallback
	}
	if looksLikeUsergroupID(id) {
		return LocalTarget{}, customErr(cmd, "*"+cmd+"* can’t send message to user groups.")
	}
	if LooksLikeMemberID(id) {
		// getMemberById without a fetch: the member the store holds
		u, ok := a.RawUser(id)
		if !ok {
			return LocalTarget{}, invalidMember
		}
		if u.Deleted && hasText {
			return LocalTarget{}, customErr(cmd, "That user has been deactivated.")
		}
		c, err := a.OpenDM(ctx, id)
		if err != nil {
			return LocalTarget{}, failed(err)
		}
		if c.ID == "" {
			return LocalTarget{}, fallback
		}
		return LocalTarget{Conv: c.ID}, nil
	}
	c, ok := a.convInfo(id)
	if !ok {
		return LocalTarget{}, invalidChannel
	}
	if c.otherTeam {
		return LocalTarget{}, customErr(cmd, "*"+cmd+"* can’t send message to channels outside this workspace.")
	}
	if c.archived && hasText {
		if c.private {
			return LocalTarget{}, customErr(cmd, "That private channel has been archived.")
		}
		return LocalTarget{}, customErr(cmd, "That channel has been archived.")
	}
	if err := a.joinToSwitch(ctx, id, c); err != nil {
		return LocalTarget{}, failed(err)
	}
	return LocalTarget{Conv: id}, nil
}

// joinToSwitch is joinAndSwitchToChannel's join (KcgN over yOEk
// joinChannel): conversations.join unless the viewer is in the channel;
// an archived channel (is_archived) is shown anyway.
func (a *App) joinToSwitch(ctx context.Context, id string, c inviteConv) error {
	if c.member {
		return nil
	}
	_, err := a.Join(ctx, id)
	var se *slack.Error
	if errors.As(err, &se) && se.Code == "is_archived" {
		return nil
	}
	return err
}
