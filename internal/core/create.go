package core

import (
	"context"
	"encoding/json"
	"errors"
	"net/url"
	"strings"
	"unicode"
	"unicode/utf8"

	"github.com/some-du6e/yoloslackgoclient/internal/slack"
)

// Creating a channel, as Slack's web client does it (build a84662d2d135,
// 2026-11-08):
//
//   - The name input (boot-shared 45Rv ChannelNameInput) rewrites what the
//     user types: lowercase, leading "#"s dropped, every whitespace rune
//     turned into "-" (PvOQ replaceSpaces). That is the whole client-side
//     rule while the friendly_channel_names experiment is off (it is for
//     Hack Club: the server answers invalid_name_specials for capitals).
//   - Every change is checked, debounced 500 ms, with
//     conversations.validateName {name, team_id, reason=create-channel}
//     (CazK). It only checks: the modal calls it on each keystroke and
//     creates with a separate method. Its errors: name_taken,
//     invalid_name_required, invalid_name_punctuation,
//     invalid_name_maxlength, invalid_name_specials (verified read-only
//     against Hack Club, 2026-11-08: ok answers {channel_name}).
//   - Create is conversations.create {name, validate_name=true,
//     is_private (private only), team_id} (6ndL submitCreate); a public
//     channel the user can't create is restricted_action before any call.
//   - Afterwards (C0Nd configureCreatedChannel) a channel created from a
//     custom section goes into it with users.channelSections.channels.
//     bulkUpdate insert=[{channel_section_id, channel_ids}] (2A1t), one
//     created from Starred is starred; anything else lands in Channels,
//     where Slack files every channel no section claims.

// MaxChannelNameLength is PvOQ getMaxLength.
const MaxChannelNameLength = 80

// NormalizeChannelName is what Slack's channel name input does to each
// edit (45Rv): lowercase, no leading "#", whitespace becomes "-".
func NormalizeChannelName(s string) string {
	s = strings.ToLower(s)
	s = strings.TrimLeft(s, "#")
	return strings.Map(func(r rune) rune {
		if unicode.IsSpace(r) {
			return '-'
		}
		return r
	}, s)
}

// Channel name problems, as Slack's channelNameErrors keys them.
const (
	NameRequired    = "invalid_name_required"
	NameMaxLength   = "invalid_name_maxlength"
	NameSpecials    = "invalid_name_specials"
	NamePunctuation = "invalid_name_punctuation"
	NameTaken       = "name_taken"
	NameRestricted  = "restricted_action"
)

// ChannelNameCode maps a validateName/create error to the code Slack's
// modal shows a message for (qJpn); anything else is "unknown".
func ChannelNameCode(err error) string {
	if err == nil {
		return ""
	}
	code := err.Error()
	var se *slack.Error
	if errors.As(err, &se) {
		code = se.Code
	}
	switch code {
	case NameRequired, NameMaxLength, NameSpecials, NamePunctuation, NameTaken, NameRestricted:
		return code
	}
	return "unknown"
}

// ChannelNameErrorText is PvOQ getErrorStringFromChannelNameErrors for
// code ("" for none). workspace may be empty.
func ChannelNameErrorText(code, workspace string) string {
	switch code {
	case "":
		return ""
	case NameRequired:
		return "Give your channel a name to continue. You can always change the name later."
	case NameMaxLength:
		return "Channel names can’t be longer than 80 characters."
	case NameSpecials, NamePunctuation:
		return "Channel names can’t contain spaces or punctuation. Use dashes to separate words."
	case NameTaken:
		if workspace != "" {
			return "That name is already taken by a channel, username, or user group in " + workspace + "."
		}
		return "That name is already taken by a channel, username, or user group in this workspace."
	case NameRestricted:
		return "Your organization’s settings have changed and may limit who can create channels. Contact an Admin or channel Owner to add people."
	}
	return "For some weird reason, that didn’t work. Please try again to continue."
}

// teamID is the workspace a new channel belongs to (Slack's modal passes
// the selected workspace; Clack signs in to one).
func (a *App) teamID() string {
	a.S.RLock()
	defer a.S.RUnlock()
	return a.S.TeamID
}

// ValidateChannelName asks Slack whether name is free and valid
// (conversations.validateName; read-only). It returns the name Slack
// would use.
func (a *App) ValidateChannelName(ctx context.Context, name string) (string, error) {
	if name == "" { // Slack's server answers this too; skip the round trip
		return "", &slack.Error{Method: "conversations.validateName", Code: NameRequired}
	}
	if utf8.RuneCountInString(name) > MaxChannelNameLength {
		return "", &slack.Error{Method: "conversations.validateName", Code: NameMaxLength}
	}
	p := url.Values{"name": {name}, "reason": {"create-channel"}}
	if t := a.teamID(); t != "" {
		p.Set("team_id", t)
	}
	var res struct {
		ChannelName string `json:"channel_name"`
	}
	if err := a.C.Call(ctx, "conversations.validateName", p, &res); err != nil {
		return "", err
	}
	if res.ChannelName == "" {
		res.ChannelName = name
	}
	return res.ChannelName, nil
}

// CreateChannelOpts is what the create modal collected.
type CreateChannelOpts struct {
	Name    string
	Private bool
	// Section is the sidebar section the modal was opened from ("" for
	// none): a custom section or Starred keeps the channel.
	Section string
}

// CreateChannel creates a channel (conversations.create), adds it to the
// sidebar and files it in o.Section when that is a custom or Starred
// section. Slack's errors (name_taken, restricted_action, …) come back as
// *slack.Error.
func (a *App) CreateChannel(ctx context.Context, o CreateChannelOpts) (Conv, error) {
	if o.Name == "" {
		return Conv{}, &slack.Error{Method: "conversations.create", Code: NameRequired}
	}
	p := url.Values{"name": {o.Name}, "validate_name": {"true"}}
	if o.Private {
		p.Set("is_private", "true")
	}
	if t := a.teamID(); t != "" {
		p.Set("team_id", t)
	}
	var res struct {
		Channel slack.Conversation `json:"channel"`
	}
	if err := a.C.Call(ctx, "conversations.create", p, &res); err != nil {
		return Conv{}, err
	}
	c := res.Channel
	if c.ID == "" {
		return Conv{}, errors.New("core: conversations.create answered no channel")
	}
	c.IsMember = true
	a.S.PutConv(c)
	if o.Section != "" {
		a.fileNewChannel(ctx, c.ID, o.Section)
	}
	v := a.conv(&c)
	a.emit(Event{Type: "conv", Channel: c.ID, Conv: &v})
	return v, nil
}

// fileNewChannel is C0Nd configureCreatedChannel: Standard sections get
// the channel through channelSections.channels.bulkUpdate, Starred stars
// it; Channels (and anything else) needs nothing. Failures leave the
// channel in Channels, as in Slack.
func (a *App) fileNewChannel(ctx context.Context, ch, section string) {
	a.S.RLock()
	typ := ""
	for _, s := range a.S.Sections {
		if s.ID == section {
			typ = s.Type
		}
	}
	a.S.RUnlock()
	switch typ {
	case "stars":
		_ = a.Star(ctx, ch, true)
	case "standard":
		ins, _ := json.Marshal([]map[string]any{{"channel_section_id": section, "channel_ids": []string{ch}}})
		p := url.Values{"insert": {string(ins)}, "remove": {"[]"}}
		if a.C.Call(ctx, "users.channelSections.channels.bulkUpdate", p, nil) != nil {
			return
		}
		a.S.Lock()
		for i := range a.S.Sections {
			if s := &a.S.Sections[i]; s.ID == section {
				s.ChannelIDsPage.ChannelIDs = append([]string{ch}, s.ChannelIDsPage.ChannelIDs...)
				s.ChannelIDsPage.Count++
			}
		}
		a.S.Unlock()
	}
}
