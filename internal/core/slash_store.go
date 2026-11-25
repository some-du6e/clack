package core

import (
	"context"
	"strings"

	"github.com/some-du6e/clack/internal/slack"
)

// The slash command store's server half, as Slack's web client fills it
// (web client 66e72b57ade1, 2026-11-24). loadClientAppCommands
// (modern.vendor Sg6t) reads client.appCommands, clears the store
// (resetCommands) and hands the answer's commands to
// bulkAddServerCommands (client-boot kvac), which:
//
//   - keys the list by app and name first (keyBy `${app}-${name}`: a later
//     command of the same app and name replaces an earlier one, in its
//     place);
//   - skips a name that doesn't start with "/";
//   - keeps no alias as a command: a command with alias_of goes to the
//     aliases of the stored command of that name, once every command is
//     in;
//   - drops every command whose canonical name (canonical_name ?? name)
//     is one of the client's own commands' (doesLocalCommandExistForName,
//     k09K): the server's /remove, /leave, /topic, /invite, /archive and
//     the rest never reach the store, so they run only as the client's;
//   - stores the rest by canonical name, or with ia4_slash_command_fe by
//     app and canonical name (a core command has no app: by name), a
//     later one of the same key replacing the earlier in its place.
//
// executeCommand's lookup then reads the client commands before these
// (yrZ7 Qe; LocalCommand, storeCommand).

// localCommandNames is k09K's list: the names doesLocalCommandExistForName
// knows (each command's canonical name, or its name when it has none).
// It is wider than the client commands Clack runs (slash_local.go): /call
// and Slack's developer commands are on it too.
var localCommandNames = map[string]bool{
	"/archive": true, "/call": true, "/collapse": true, "/color": true, "/expand": true, "/invite": true,
	"/invite_people": true, "/gif": true, "/leave": true, "/msg": true, "/open": true, "/remove": true,
	"/rename": true, "/search": true, "/shortcuts": true, "/slackaccessibility": true, "/slackperf": true,
	"/slackdebugcanvas": true, "/slackdebuglag": true, "/slackdebugunreads": true, "/slackdebugdegrade": true,
	"/slackdebugdegraderecover": true, "/slackdevtools": true, "/slackdesktopsettings": true, "/slackgpuinfo": true,
	"/slackfont": true, "/slackserviceworkerinfo": true, "/slackgov": true, "/slackjspath": true, "/slackstaging": true,
	"/slackmediainfo": true, "/slacknetlog": true, "/slacktrace": true, "/slackenabletracing": true,
	"/slackrecordtraffic": true, "/slackforceversion": true, "/slackversion": true, "/topic": true, "/feedback": true,
}

// serverCommandStore is bulkAddServerCommands over cmds (client.appCommands'
// list, in its order) into an empty store, as the store's commands in
// their order (the store's insertion order: executeCommand's first match
// is the first stored).
func serverCommandStore(cmds []slack.Command, ia4 bool) []slack.Command {
	// keyBy `${app}-${name}`: a JS object, so a repeated key keeps its
	// first place and takes the later command
	type keyed struct {
		key string
		cmd slack.Command
	}
	var in []keyed
	at := map[string]int{}
	for _, c := range cmds {
		k := c.AppID + "-" + c.Name
		if c.AppID == "" { // `${undefined}-${name}`
			k = "undefined-" + c.Name
		}
		if i, ok := at[k]; ok {
			in[i].cmd = c
			continue
		}
		at[k] = len(in)
		in = append(in, keyed{k, c})
	}
	var out []slack.Command
	stored := map[string]int{}
	aliases := map[string][]string{} // alias_of → the alias names, in order
	var aliasOrder []string
	for _, kc := range in {
		c := kc.cmd
		if !strings.HasPrefix(c.Name, "/") {
			continue
		}
		if c.AliasOf != "" {
			if _, ok := aliases[c.AliasOf]; !ok {
				aliasOrder = append(aliasOrder, c.AliasOf)
			}
			aliases[c.AliasOf] = append(aliases[c.AliasOf], c.Name)
			continue
		}
		canon := canonicalCommand(c)
		if localCommandNames[canon] {
			continue
		}
		key := canon
		if ia4 && c.AppID != "" {
			key = c.AppID + "-" + canon
		}
		if i, ok := stored[key]; ok {
			out[i] = c
			continue
		}
		stored[key] = len(out)
		out = append(out, c)
	}
	// keyBy(store, name): the last stored command of a name takes the
	// aliases (a.aliases = t, over any the server sent with it)
	byName := map[string]int{}
	for i, c := range out {
		byName[c.Name] = i
	}
	for _, target := range aliasOrder {
		if i, ok := byName[target]; ok {
			out[i].Aliases = aliases[target]
		}
	}
	return out
}

// clientMenuCommand is one of Slack's client commands as formatCommand
// gives it to the slash command store's list (Zrd3 e4): its description
// (descShared: in an externally shared channel), usage and aliases.
type clientMenuCommand struct {
	name, desc, descShared, usage string
	aliases                       []string
}

// clientMenu is Zrd3 getCommands with autocomplete on, in its order:
// /color's autocomplete is false, the developer commands' only for a
// dogfood user, and the multiselect previews are hidden.
var clientMenu = []clientMenuCommand{
	{name: "/archive", desc: "Archive the current channel"},
	{name: "/collapse", desc: "Collapse all files in the current channel or thread (opposite of /expand)"},
	{name: "/expand", desc: "Expand all files in the current channel or thread (opposite of /collapse)"},
	{name: "/invite", desc: "Invite another member to a channel", usage: "@user [channel]"},
	{name: "/invite_people", desc: "Invite people to your workspace", usage: "name@example.com, …"},
	{name: "/leave", desc: "Leave a channel", usage: "[channel]", aliases: []string{"/close", "/part"}},
	{name: "/msg", desc: "Send a DM message to another user", usage: "@user your message", aliases: []string{"/dm"}},
	{name: "/open", desc: "Open a channel", usage: "channel", aliases: []string{"/join"}},
	{name: "/remove", desc: "Remove a person from the current channel", usage: "@user", aliases: []string{"/kick"}},
	{name: "/rename", desc: "Rename a channel", descShared: "Rename this channel for your workspace"},
	{name: "/search", desc: "Perform a search", usage: "[your text]", aliases: []string{"/s"}},
	{name: "/shortcuts", desc: "Open the keyboard shortcuts dialog", aliases: []string{"/keys"}},
	{name: "/topic", desc: "Set the channel topic", descShared: "Set the channel topic for your workspace", usage: "[new topic]"},
	{name: "/feedback"},
	{name: "/gif", desc: "A GIF is worth a thousand words", usage: "[search terms]"},
}

// SlashCommands is the slash command store's list as executeCommand and
// the "/" menu read it (yrZ7 Qe: [...the client commands, ...the
// server's]): Slack's client commands the menu lists (clientMenu, their
// descriptions for ch: an externally shared channel's for /rename and
// /topic), then the server's (Commands: client.appCommands through the
// ingest, which keeps none of the client commands' names).
func (a *App) SlashCommands(ctx context.Context, ch string) ([]Command, error) {
	server, err := a.Commands(ctx, "")
	shared := false
	if c, ok := a.S.Conv(ch); ok && c.IsExtShared {
		shared = true
	}
	out := make([]Command, 0, len(clientMenu)+len(server))
	for _, c := range clientMenu {
		desc := c.desc
		if shared && c.descShared != "" {
			desc = c.descShared
		}
		out = append(out, Command{Name: c.name, Desc: desc, Usage: c.usage, Type: "client"})
	}
	return append(out, server...), err
}
