package core

import (
	"context"
	"encoding/json"
	"log"
	"regexp"
	"strings"
)

// /color (alias /colour) is one of Slack's client commands (Zrd3
// getCommands; slash-commands-local qu1f colorCommand, web client
// 66e72b57ade1): it never reaches chat.command.
//
//   - The first argument must mention someone the store holds
//     (getMemberById without the fetch) who isn't a non-existent member:
//     else "A valid member name is required.".
//   - The second, its "#"s dropped, must be six hex digits when given:
//     else "A valid 6 character hex code is required, like `FF0000`.".
//   - setUserColorAndCallApi writes the user_colors pref (setUserPrefByApi:
//     users.prefs.set, the viewer's colors by member, this one set or
//     dropped); a failed write is only logged.
//   - Slackbot's temporary feedback says what was done, with the member's
//     real name: "You’ve set your custom color for {user} to #{color}", or
//     "You’ve removed your custom color for {user}." without a color.

// colorHexRe is JQxa T.
var colorHexRe = regexp.MustCompile(`^[0-9a-fA-F]{6}$`)

// ColorCommand is colorCommand for the parsed arguments in ch (threadTS:
// the thread it was typed in): its feedback, or the refusal.
func (a *App) ColorCommand(ctx context.Context, cmd, ch, threadTS string, params []CommandParam) error {
	member, hex := "", ""
	if len(params) > 0 && params[0].Type == "mention" {
		member = params[0].Value
	}
	if len(params) > 1 && params[1].Type == "string" {
		hex = strings.ReplaceAll(params[1].Value, "#", "")
	}
	var known bool
	var realName string
	if LooksLikeMemberID(member) && !a.S.Nonexistent(member) {
		if u, ok := a.S.RawUser(member); ok && u != nil {
			known = true
			realName = firstNonEmptyS(u.Profile.RealName, u.RealName) // getMemberRealName
		}
	}
	switch {
	case !known:
		return customErr(cmd, "A valid member name is required.")
	case hex != "" && !colorHexRe.MatchString(hex):
		return customErr(cmd, "A valid 6 character hex code is required, like `FF0000`.")
	}
	text := "You’ve removed your custom color for " + realName + "."
	if hex != "" {
		text = "You’ve set your custom color for " + realName + " to #" + hex
	}
	colors := a.userColors()
	if hex != "" {
		colors[member] = hex
	} else {
		delete(colors, member)
	}
	a.CommandFeedback(ch, threadTS, text)
	if err := a.SetPref(ctx, "user_colors", colors); err != nil {
		log.Printf("color: setting user_colors: %v", err) // "Error setting user color change"
	}
	return nil
}

// userColors is the user_colors pref (the viewer's custom colors by
// member), a copy; users.prefs.get answers it as JSON in a string.
func (a *App) userColors() map[string]string {
	out := map[string]string{}
	raw, ok := a.Pref("user_colors")
	if !ok || len(raw) == 0 {
		return out
	}
	var s string
	if json.Unmarshal(raw, &s) == nil {
		raw = json.RawMessage(s)
	}
	_ = json.Unmarshal(raw, &out)
	if out == nil {
		out = map[string]string{}
	}
	return out
}
