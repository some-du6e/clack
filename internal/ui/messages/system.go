package messages

import (
	"github.com/some-du6e/clack/internal/core"
	"github.com/some-du6e/clack/internal/richtext"
	"github.com/some-du6e/clack/internal/ui/icons"
)

// System messages and system_messages_redesign_m1, ported from Slack's
// client (build 4d0c78085232, boot-shared):
//
//   - The redesign draws a subtype as SystemMessage (I0jQ: the 16px glyph
//     in the avatar's place, 13px secondary text, the time inline; row.go
//     systemLayout) only when it is registered (BBB3
//     isRedesignedSystemMessageSubtypeActive: the kA8d components' pU
//     calls and BBB3's own table) and the experiment is on. Other automated
//     subtypes (pinned_item, bot_add, reminder_add…) and every subtype with
//     the experiment off or unassigned are ordinary message rows (avatar,
//     name, time) whose body is getMessageAutomatedSubtypeText (NaeI).
//   - Redesigned texts name the actor first, a RollupMemberButton (the
//     member's name in the text's colour, underlined under the pointer):
//     renderRedesignedMembershipText (OuZx) "{actor} joined #{channel}",
//     "… left #{channel}" ("the conversation" in a DM or group DM), "{actor}
//     was added to #{channel} by {inviter}" when an inviter is recorded;
//     renderRedesignedMetadataChangeText (EFF2) for topics and
//     descriptions, renderRedesignedRenameText (DeME), …ArchiveText (/EW+)
//     and …UnarchiveText (a1i9).
//   - NaeI keeps the message's own text for joins and leaves and writes
//     the topic, description, rename, archive and unarchive lines itself,
//     without the actor (the row's sender says who).
//   - The glyph: getRedesignedSystemMessageIcon (BBB3), "direct-messages"
//     for a group DM's rename, and "apps" for a join or leave by an app or
//     bot (BaseMessageAvatar's isAppOrBotJoiner). Clack's icon set has no
//     canvas-content, ai-huddle-summary or hourglass glyph yet: those rows
//     keep the generic info glyph.

// redesignedIcons is the registry: every redesigned subtype and its glyph.
var redesignedIcons = map[string]string{
	"channel_join": "user-add", "group_join": "user-add", "welcome_party": "user-add",
	"channel_leave": "user", "group_leave": "user",
	"channel_topic": "channel", "group_topic": "channel", "channel_purpose": "channel", "group_purpose": "channel",
	"channel_name": "channel", "group_name": "channel",
	"channel_archive": "archive", "group_archive": "archive", "channel_unarchive": "archive", "group_unarchive": "archive",
	"tabbed_canvas_updated": "canvas-content", "tabbed_canvas_updated_unread_ignore": "canvas-content",
	"channel_canvas_updated": "canvas-content", "channel_canvas_updated_unread_ignore": "canvas-content",
	"folders_organized": "folder", "folders_organized_unread_ignore": "folder",
	"folder_item_added": "folder", "folder_item_added_unread_ignore": "folder",
	"app_conversation_join": "apps", "app_conversation_leave": "apps",
	"automatic_ai_huddle_notes_enabled": "ai-huddle-summary", "automatic_ai_huddle_notes_disabled": "ai-huddle-summary",
	"agent_view_added": "canvas-content", "code_channel_review_submitted": "code", "code_channel_lifecycle": "code",
	"retention": "hourglass", "channel_posting_permissions": "channel",
	"channel_convert_to_private": "channel", "channel_convert_to_public": "channel",
	"channel_convert_to_linked_private": "channel", "channel_convert_to_linked_private_read": "channel",
	"channel_convert_to_linked_private_edit": "channel", "channel_convert_to_linked_private_transfer": "channel",
	"channel_convert_shared": "channel",
}

// systemRedesign reports the experiment's group: on.
func systemRedesign(h Host) bool {
	app := h.Core()
	return app != nil && app.Experiment("system_messages_redesign_m1") == "on"
}

// redesignedIcon is the glyph of a redesigned row.
func redesignedIcon(app *core.App, m *core.Message) string {
	sub := m.Subtype
	if app != nil {
		if c, ok := app.Conv(m.Channel); ok && c.Kind == "mpim" && (sub == "channel_name" || sub == "group_name") {
			return "direct-messages"
		}
		if joinKind(m) != 0 {
			if u, ok := app.User(m.User); ok && (u.IsBot || u.AppUser) {
				return "apps"
			}
		}
	}
	if ic := redesignedIcons[sub]; icons.Has(ic) {
		return ic
	}
	return "info"
}

// adjustSystem gives the snapshot's system messages Slack's look for the
// experiment's group: System (drawn as SystemMessage) and their text. It
// reports whether an actor's name wasn't loaded yet.
func (s *listState) adjustSystem(h Host, on bool) (missing bool) {
	app := h.Core()
	for i := range s.snap.msgs {
		m := &s.snap.msgs[i].m
		_, redesigned := redesignedIcons[m.Subtype]
		if !m.System && !redesigned && !isAutomatedSubtype(m.Subtype) {
			continue
		}
		m.System = on && redesigned
		if app == nil {
			continue
		}
		if spans, miss, ok := systemText(h, app, m, m.System); ok {
			m.Doc = richtext.Doc{Blocks: []richtext.Block{{Kind: richtext.Paragraph, Spans: spans}}}
			missing = missing || miss
		}
	}
	return missing
}

// systemText is the line Slack writes for m: the redesigned one, or
// NaeI's. ok false keeps the message's own text.
func systemText(h Host, app *core.App, m *core.Message, redesigned bool) (spans []richtext.Span, missing, ok bool) {
	conv, _ := app.Conv(m.Channel)
	isConversation := conv.Kind == "im" || conv.Kind == "mpim" // aK5v JM
	txt := func(t string) { spans = append(spans, richtext.Span{Kind: richtext.Text, Text: t}) }
	member := func(id string) {
		name := id
		if u, ok := app.User(id); ok && u.Name != "" {
			name = u.Name
		} else {
			ensureUser(h, id)
			missing = true
		}
		spans = append(spans, richtext.Span{Kind: richtext.User, ID: id, Text: name})
	}
	actor := func() {
		if redesigned {
			member(m.User)
		}
	}
	mrkdwn := func(t string) {
		for _, b := range app.Mrkdwn(t) {
			spans = append(spans, b.Spans...)
		}
	}
	// the channel as e5.SB names it: "#name" for a public channel, a
	// private channel's own name
	hashName := func() string {
		if conv.Kind == "private" {
			return conv.Name
		}
		return "#" + conv.Name
	}
	switch m.Subtype {
	case "channel_join", "group_join", "channel_leave", "group_leave":
		if !redesigned {
			return nil, false, false // NaeI: the message's text
		}
		join := joinKind(m) == 1
		switch {
		case join && m.Inviter != "": // MemberAddedMessage (renderRedesignedMemberAddedText)
			actor()
			if isConversation {
				txt(" was added to the conversation by ")
			} else {
				txt(" was added to #" + conv.Name + " by ")
			}
			member(m.Inviter)
		case join && isConversation:
			actor()
			txt(" joined the conversation")
		case join:
			actor()
			txt(" joined #" + conv.Name)
		case isConversation:
			actor()
			txt(" left the conversation")
		default:
			actor()
			txt(" left #" + conv.Name)
		}
	case "channel_topic", "group_topic":
		where := "channel"
		if isConversation {
			where = "conversation"
		}
		actor()
		pre := " "
		if !redesigned {
			pre = ""
		}
		if m.Topic == "" {
			txt(pre + "cleared the " + where + " topic")
			break
		}
		txt(pre + "set the " + where + " topic: ")
		mrkdwn(m.Topic)
	case "channel_purpose", "group_purpose":
		actor()
		pre := " "
		if !redesigned {
			pre = ""
		}
		if m.Purpose == "" {
			txt(pre + "cleared the channel description")
			break
		}
		txt(pre + "set the channel description: ")
		mrkdwn(m.Purpose)
	case "channel_name", "group_name":
		actor()
		pre := " "
		if !redesigned {
			pre = ""
		}
		dot := ""
		if redesigned {
			dot = "."
		}
		named := app.Experiment("mpdm_naming") == "on"
		switch {
		case conv.Kind == "mpim" && m.OldName == "" && m.NewName != "":
			txt(pre + "named the conversation “" + m.NewName + "”" + dot)
		case conv.Kind == "mpim" && m.OldName != "" && m.NewName == "":
			txt(pre + "removed the conversation name “" + m.OldName + "”" + dot)
		case conv.Kind == "mpim" && named:
			txt(pre + "renamed the conversation from “" + m.OldName + "” to “" + m.NewName + "”" + dot)
		default:
			txt(pre + "renamed the channel from “" + m.OldName + "” to “" + m.NewName + "”" + dot)
		}
	case "channel_archive", "group_archive":
		self := m.User != "" && m.User == app.Self().ID
		name := hashName()
		if !redesigned {
			if m.Subtype == "channel_archive" || self {
				name = "#" + conv.Name
			} else {
				name = conv.Name
			}
		}
		actor()
		pre := " "
		if !redesigned {
			pre = ""
		}
		if self {
			txt(pre + "archived " + name + ". The messages and files in this channel are still browsable and available in search. You can unarchive this channel anytime from Channel Details.")
		} else {
			txt(pre + "archived " + name + ". The contents will still be browsable and available in search.")
		}
	case "channel_unarchive", "group_unarchive":
		name := hashName()
		if !redesigned {
			if m.Subtype == "channel_unarchive" {
				name = "#" + conv.Name
			} else {
				name = conv.Name
			}
		}
		actor()
		pre := " "
		if !redesigned {
			pre = ""
		}
		txt(pre + "unarchived " + name)
	default:
		return nil, false, false
	}
	return spans, missing, true
}
