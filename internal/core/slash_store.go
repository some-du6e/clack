package core

import (
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
