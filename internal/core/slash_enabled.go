package core

// What happens to a typed command before anything runs it, as Slack's web
// client does it (web client 66e72b57ade1, 2026-11-24):
//
//   - handleSlashCommand (boot-render vHCW) looks for a server command the
//     typed word matches that is installed in one of the channel's
//     workspaces (getRelevantWorkspaceIdsForChannel, UaAK f: without
//     unified users, Clack's case, the signed-in workspace); with
//     ia4_slash_command_fe and an app picked from the autocomplete, that
//     app's. Found, maybeShowShortcutsWorkspacePickerSpeedbump (Po0S)
//     decides the workspace: one runs it there, with ia4 as the app of
//     the first matching command installed there
//     (h.find(t => t.teams.includes(e)).app); none runs it with neither;
//     more would ask with its picker, which one workspace never needs.
//     Not found: the team and app the send was given.
//   - executeCommand (pymN) matches the command in the slash command
//     store, whose client commands (Zrd3 e4) come before the server's
//     (yrZ7 Qe), takes the picked app's command of that name instead with
//     ia4, and asks isSlashCommandEnabled (P+cE mE) before it runs any of
//     it, the reminder dialog and the client commands included: a refusal
//     rejects with its kind and text, which handleSlashCommand shows as
//     Slackbot's (CommandFailureText). A word the store doesn't hold isn't
//     checked: chat.command decides.
//
// isSlashCommandEnabled, in its order, for the parts Clack's commands
// reach (its developer and desktop-only lists, B and U, name commands
// Clack leaves out; the shortcuts menu's list isn't the composer's):
//
//   - in a file's conversation (gxWy jT, is_file) /leave, /close, /part,
//     /archive and /topic are unknown;
//   - in a thread only O's commands run, D's in a channel that isn't a
//     huddle's (zdO/ cb, properties.huddles.is_huddle_channel) and isn't a
//     file's, /invite under invite_slash_command_in_threads, and a command
//     marked is_runnable_from_thread (ZkUx Lf), by canonical name;
//   - a custom or service command is unknown to a guest (p+Mq Ez) under
//     commands_only_regular;
//   - a command with teams needs one of the channel's workspaces: "This
//     command is not available in this channel";
//   - /open and /feed are unknown to a guest;
//   - /topic needs canUserSetTopicForChannelWithReasonById (iJOl X), its
//     reason in getChannelTopicError's words (MgZJ Bw);
//   - /rename needs canUserRenameChannelWithReasonById (kfQA be), its
//     reason in the check's own words;
//   - /archive needs canUserArchiveChannel (2Uru Jz), else it's unknown;
//   - /feedback is unknown under feedback_disabled_orgs' treatment.
//
// The permissions these read are getPermissionForChannel's
// (permissions/info, chperms.go): Slack's store has them from the
// channel's own views by the time a command is typed, and Clack's command
// asks when it has none yet (channelGrantNow). The /invite rules are
// inviteCommand's own (invite_command.go).

import (
	"context"
	"encoding/json"
	"slices"
	"strings"

	"github.com/some-du6e/clack/internal/slack"
)

// fileChannelUnknown is isSlashCommandEnabled's W: the commands a file's
// conversation doesn't know.
var fileChannelUnknown = map[string]bool{"/leave": true, "/close": true, "/part": true, "/archive": true, "/topic": true}

// CheckCommand is handleSlashCommand's workspace pick and executeCommand's
// isSlashCommandEnabled for o, before anything runs the command: o's
// TeamID and AppID become the ones it runs with, and Checked is set; a
// refusal is the CommandError executeCommand rejects with. Command runs
// it itself unless o.Checked.
func (a *App) CheckCommand(ctx context.Context, o *CommandOpts) error {
	cmd := commandWord(o.Command)
	o.TeamID, o.AppID = a.commandWorkspace(cmd, o.Channel, o.TeamID, o.AppID)
	o.Checked = true
	z, ok := a.storeCommand(cmd, o.AppID)
	if !ok {
		return nil
	}
	return a.slashEnabled(ctx, z, cmd, o.Channel, o.ThreadTS != "")
}

// commandWord is the command as executeCommand reads it: with its slash
// (a full-width one stays: executeCommand's N).
func commandWord(cmd string) string {
	if !strings.HasPrefix(cmd, "/") && !strings.HasPrefix(cmd, "\uff0f") {
		return "/" + cmd
	}
	return cmd
}

// clientCommand is the slash command store's client command typed
// matches (Zrd3 formatCommand: type "client").
func clientCommand(typed string) (slack.Command, bool) {
	for _, c := range clientCommands {
		if commandMatches(c, typed) {
			c.Type = "client"
			return c, true
		}
	}
	return slack.Command{}, false
}

// storeCommand is executeCommand's Z: the first command of the slash
// command store typed matches, the client commands before the server's;
// with ia4_slash_command_fe and app, the app's command of that name
// (e.app === x && e.name === Y, Y the matched name, else the word).
func (a *App) storeCommand(typed, app string) (slack.Command, bool) {
	z, ok := clientCommand(typed)
	if !ok {
		z, ok = a.knownCommand(typed)
	}
	if app != "" && a.experiment("ia4_slash_command_fe") == "on" {
		y := typed
		if ok {
			y = z.Name
		}
		if c, found := a.appCommand(y, app); found {
			z, ok = c, true
		}
	}
	return z, ok
}

// slashEnabled is isSlashCommandEnabled (P+cE mE) for store command z,
// typed as cmd in ch.
func (a *App) slashEnabled(ctx context.Context, z slack.Command, cmd, ch string, thread bool) error {
	unknown := &CommandError{Command: cmd, Kind: CmdUnknown}
	v := canonicalCommand(z)
	conv, _ := a.S.Conv(ch) // H: getChannelById
	file := conv != nil && conv.IsFile
	if file && fileChannelUnknown[v] {
		return unknown
	}
	if thread && !a.threadCommandOK(v, conv) && !z.RunnableFromThread {
		return &CommandError{Command: cmd, Kind: CmdUnsupportedInThread}
	}
	if (z.Type == "custom" || z.Type == "service") && a.teamPrefOn("commands_only_regular") && a.selfRestricted() {
		return unknown
	}
	if z.Teams != nil { // `if (t.teams)`: an empty list too
		ws := a.channelWorkspaces(conv)
		if !slices.ContainsFunc(z.Teams, func(t string) bool { return slices.Contains(ws, t) }) {
			return customErr(cmd, "This command is not available in this channel")
		}
	}
	grant := func(p string) bool { return a.channelGrantNow(ctx, ch, p) }
	switch v {
	case "/open", "/feed":
		if a.selfRestricted() {
			return unknown
		}
	case "/topic":
		if r := a.topicRefusal(conv, grant); r != "" {
			return customErr(cmd, channelTopicError(r))
		}
	case "/rename":
		if r := renameRefusal(conv, grant); r != renameOK {
			return customErr(cmd, renameRefusalText(r))
		}
	case "/archive":
		if !a.canArchive(conv, grant) {
			return unknown
		}
	case "/feedback":
		return a.FeedbackCommandErr(cmd)
	}
	return nil
}

// selfRestricted is p+Mq Ez for the viewer: is_restricted (a guest; an
// ultra restricted one is restricted too).
func (a *App) selfRestricted() bool {
	u, ok := a.S.User(a.S.SelfID())
	return ok && u.IsRestricted
}

// channelWorkspaces is getRelevantWorkspaceIdsForChannel (UaAK f) without
// unified users: the signed-in workspace, none for a channel the store
// doesn't hold.
func (a *App) channelWorkspaces(c *slack.Conversation) []string {
	if c == nil {
		return nil
	}
	return a.selfWorkspaceIDs()
}

// cmdProps are the properties the checks read.
type cmdProps struct {
	Huddles struct {
		IsHuddleChannel bool `json:"is_huddle_channel"`
	} `json:"huddles"`
	MeetingsChannel struct {
		IsMeetingChannel bool `json:"is_meeting_channel"`
	} `json:"meetings_channel"`
	RecordChannel struct {
		RecordID   string `json:"record_id"`
		RecordType string `json:"record_type"`
	} `json:"record_channel"`
}

func propsOf(c *slack.Conversation) cmdProps {
	var p cmdProps
	if c != nil && len(c.Properties) > 0 {
		_ = json.Unmarshal(c.Properties, &p)
	}
	return p
}

// knownChannel is zdO/ jq: a channel-shaped id of a known type (an IM, a
// group DM, a private or a public channel).
func knownChannel(c *slack.Conversation) bool {
	if c == nil || !looksLikeChannelID(c.ID) {
		return false
	}
	private := (c.IsPrivate || c.IsGroup) && !c.IsMPIM
	return c.IsIM || c.IsMPIM || private || c.IsChannel
}

// commandWorkspace is handleSlashCommand's workspace pick for a command
// typed in ch (vHCW's lookup, then Po0S with isSlashCommand): the team
// and app executeCommand runs it with.
func (a *App) commandWorkspace(cmd, ch, team, app string) (string, string) {
	conv, _ := a.S.Conv(ch)
	ws := a.channelWorkspaces(conv)
	ia4 := a.experiment("ia4_slash_command_fe") == "on"
	installed := func(c slack.Command) bool {
		return slices.ContainsFunc(c.Teams, func(t string) bool { return slices.Contains(ws, t) })
	}
	a.mu.Lock()
	cmds := slices.Clone(a.cmdCache) // the store's server commands (UrAp p)
	a.mu.Unlock()
	i := slices.IndexFunc(cmds, func(c slack.Command) bool {
		return commandMatches(c, cmd) && installed(c) && (app == "" || !ia4 || c.AppID == app)
	})
	if i < 0 {
		return team, app // executeSlashCommandCallback(h)
	}
	y := cmds[i]
	var picked []string // d
	var from []slack.Command
	if ia4 {
		for _, c := range cmds {
			if commandMatches(c, y.Name) && installed(c) && (app == "" || c.AppID == app) {
				from = append(from, c)
				for _, t := range commandTeams(c, ws) {
					if !slices.Contains(picked, t) {
						picked = append(picked, t)
					}
				}
			}
		}
	} else {
		picked = commandTeams(y, ws)
	}
	switch len(picked) {
	case 0:
		return "", app // onConfirm(): no team, the app picked
	case 1:
		if ia4 {
			for _, c := range from {
				if slices.Contains(c.Teams, picked[0]) {
					return picked[0], c.AppID
				}
			}
		}
		return picked[0], app
	}
	// several: Slack's picker asks; one workspace never gets here
	return team, app
}

// commandTeams is P+cE tY: the command's workspaces among the channel's
// (none for a workspace-agnostic command; tY's "Slack" app name is the
// client and core commands', which have no teams).
func commandTeams(c slack.Command, ws []string) []string {
	if c.Teams == nil || len(ws) == 0 || c.WorkspaceAgnostic {
		return nil
	}
	var out []string
	for _, t := range c.Teams {
		if slices.Contains(ws, t) {
			out = append(out, t)
		}
	}
	return out
}

// Rename refusal reasons (kfQA a_), and none.
const (
	renameOK                 = ""
	renameInvalidParams      = "invalid_params"
	renameInvalidDM          = "invalid_dm"
	renameInvalidChannelType = "invalid_channel_type"
	renameNoPermission       = "user_has_no_permissions"
)

// renameRefusal is canUserRenameChannelWithReasonById (kfQA be) in the
// client (bootData app "client", TZ: the permission branch): a
// conversation the store holds, of a known type, not a DM or group DM,
// not a meeting's or a record's (a Salesforce or agent record channel
// may), with RENAME_CHANNEL.
func renameRefusal(c *slack.Conversation, grant func(string) bool) string {
	if c == nil || !knownChannel(c) {
		return renameInvalidParams
	}
	p := propsOf(c)
	rec := p.RecordChannel.RecordID != "" // zdO/ rI
	salesforce := rec && salesforceRecordID(p.RecordChannel.RecordID)
	agent := rec && (p.RecordChannel.RecordType == "agent_channel" || strings.HasPrefix(p.RecordChannel.RecordID, "AC:"))
	switch {
	case c.IsIM || c.IsMPIM:
		return renameInvalidDM
	case p.MeetingsChannel.IsMeetingChannel || strings.HasPrefix(c.Name, "mc:"), // zdO/ Dh
		rec && !salesforce && !agent:
		return renameInvalidChannelType
	case grant(PermRenameChannel):
		return renameOK
	}
	return renameNoPermission
}

// salesforceRecordID is OHyo V: three ":"-parts, the first "S".
func salesforceRecordID(id string) bool {
	parts := strings.Split(id, ":")
	return len(parts) == 3 && parts[0] == "S"
}

// renameRefusalText is isSlashCommandEnabled's /rename feedback for a
// refusal (its switch with no second argument).
func renameRefusalText(reason string) string {
	switch reason {
	case renameInvalidDM:
		return "Direct messages don’t even have a name! Ergo, regrettably, you can’t change it."
	case renameInvalidChannelType:
		return "This channel can’t be renamed."
	}
	return "You don’t have permission to rename."
}

// canArchive is canUserArchiveChannel (2Uru Jz) over
// canUserSeeArchiveChannelOptionWithReasons (HT3j iz): a conversation of
// a known type that isn't a file's; an externally shared one only when
// the viewer may disconnect it; then not a DM or group DM, not #general
// or org-mandatory, not archived, not hosted by another organization
// (yAjA b), the viewer in it or a workspace or org admin (JM7a S4), and
// ARCHIVE_CHANNEL.
func (a *App) canArchive(c *slack.Conversation, grant func(string) bool) bool {
	if c == nil || !knownChannel(c) || c.IsFile {
		return false
	}
	if c.IsExtShared && !a.canDisconnectShared(c) {
		return false
	}
	switch {
	case c.IsIM || c.IsMPIM, c.IsGeneral || c.IsOrgMandatory, c.IsArchived, a.hostedAway(c):
		return false
	case !c.IsMember && !a.actsAsAdmin(c):
		return false
	}
	return grant(PermArchiveChannel)
}

// hostedAway is yAjA b: the channel's conversation_host_id is neither the
// signed-in org nor its workspace.
func (a *App) hostedAway(c *slack.Conversation) bool {
	h := c.ConversationHostID
	if h == "" || a.C == nil {
		return false
	}
	own := a.C.Creds.EnterpriseID // XgZX Kc: the enterprise, else the team
	if own == "" {
		own = a.C.Creds.Team
	}
	return h != own && h != a.C.Creds.Team
}

// actsAsAdmin is JM7a S4 for the viewer: a workspace of the channel's
// where the viewer is an org admin (ltQJ n5) or a workspace admin (ltQJ
// LF, is_admin).
func (a *App) actsAsAdmin(c *slack.Conversation) bool {
	if len(a.channelWorkspaces(c)) == 0 {
		return false
	}
	u, ok := a.S.User(a.S.SelfID())
	return ok && (u.IsAdmin || a.selfIsEnterpriseAdmin())
}

// canDisconnectShared is canUserDisconnectExternalSharedChannel (SL8x R):
// an externally shared channel that isn't a DM or #general, which the
// viewer created (DT3Q M), or on a free plan (rUVB tc), or where the
// org's who_can_manage_shared_channels lets the viewer manage it (f0pa
// I). Clack doesn't read that org pref, so the last is Slack's answer
// while the pref has no value: no.
func (a *App) canDisconnectShared(c *slack.Conversation) bool {
	if c.IsIM || c.IsMPIM || !c.IsExtShared || c.IsGeneral {
		return false
	}
	if c.Creator != "" && c.Creator == a.S.SelfID() {
		return true
	}
	return a.OnFreePlan()
}
