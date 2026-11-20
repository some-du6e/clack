package richtext

// The share_shortcut block (a shared workflow link), as Slack's web client
// draws it (build 031dbe6a7400, boot-shared): ShareShortcut (KrN7) takes
// the block's own fields as the workflow's shortcut info
// (getFreshShortcutInfoForShareShortcutBlock D1NF, createShortcutFromBlock
// 1BgL) and draws WorkflowEntity: Slack Kit's entity card (VLgN on aJif),
// 420 wide, with the workflow's icon (ShortcutIcon mp5R: the trigger's
// workflow app), its title, the trigger type's byline
// (WorkflowTriggerTypeLabel b2Ud: none for a link trigger), the "Uses AI"
// badge (WorkflowAiBadge 0Odw), its description and, for a link trigger,
// the Start Workflow button (JYoX). The workflow's bot member decides the
// card's state: not loaded yet, the loading skeleton; deleted, the
// compact "This workflow no longer exists." card.

import (
	"regexp"
	"strings"
)

// ShortcutCard is what a share_shortcut card shows.
type ShortcutCard struct {
	TriggerID, Title, Description string
	// URL is a link trigger's url: Start Workflow trips it ("" for other
	// trigger types, which have no Start button).
	URL string
	// TriggerType is shortcut, event, scheduled, webhook, external or
	// service; Byline and BylineIcon are WorkflowTriggerTypeLabel's for
	// it ("" for a link trigger).
	TriggerType, Byline, BylineIcon string
	// Icon is the workflow app's icon from the trigger lookup ("" while
	// it isn't known: Slack's default app icon).
	Icon       string
	ShareURL   string
	WorkflowID string
	BotUserID  string
	// AI: contains_ai_steps, the "Uses AI" badge after the title.
	AI bool
	// State is the bot member's: "enabled", "loading" (not loaded yet,
	// or no bot at all) or "deleted".
	State string
}

// MemberStater is an optional Resolver extension: whether a member is
// deleted. known is false while a member-shaped id is still being fetched
// (the resolver asks for it and the Doc is rebuilt when it comes); a
// member users.info didn't find is known, and not deleted (Slack's
// non-existent member).
type MemberStater interface {
	MemberDeleted(id string) (deleted, known bool)
}

var memberIDRe = regexp.MustCompile(`^@?(U|W)[A-Z0-9]{8,}$`)

// LooksLikeMemberID is Slack's looksLikeMemberId (5oKS): Slackbot and
// Slack's fixed members (USLACKBOT, USEARCH, UCHANNELEXPERT, USLACK,
// UTBD), or "U" or "W" and eight or more of A-Z0-9, an "@" before it
// allowed. getMemberById fetches only ids that look like this; any other
// is a non-existent member at once.
func LooksLikeMemberID(id string) bool {
	switch id {
	case "USLACKBOT", "USEARCH", "UCHANNELEXPERT", "USLACK", "UTBD":
		return true
	}
	return memberIDRe.MatchString(id)
}

// bkShortcut is a share_shortcut block's fields (bk embeds it).
type bkShortcut struct {
	TriggerType     string `json:"trigger_type"`
	TriggerSubtype  string `json:"trigger_subtype"`
	BotUserID       string `json:"bot_user_id"`
	WorkflowID      string `json:"workflow_id"`
	ShareURL        string `json:"share_url"`
	ContainsAISteps bool   `json:"contains_ai_steps"`
	OwningTeamID    string `json:"owning_team_id"`
	AppID           string `json:"app_id"`
	ServiceName     string `json:"service_name"`
}

// triggerIDRe is looksLikeTriggerId (7GJl).
var triggerIDRe = regexp.MustCompile(`^Ft[A-Z0-9]{7,}$`)

// shortcutBlock converts a share_shortcut block. ShareShortcut (KrN7)
// draws a function_trigger_id that isn't a trigger id as the text
// "UNKNOWN SHORTCUT TYPE", no card (a notification still reads the
// block's title: convertStringTitledBlockToPlainText).
func (c *builder) shortcutBlock(b *bk) Block {
	if !triggerIDRe.MatchString(b.FunctionTriggerID) && !c.notify {
		return Block{Kind: Paragraph, BlockID: b.BlockID, Spans: []Span{{Kind: Text, Text: "UNKNOWN SHORTCUT TYPE"}}}
	}
	sc := &ShortcutCard{TriggerID: b.FunctionTriggerID, TriggerType: b.TriggerType, ShareURL: SafeURL(b.ShareURL),
		WorkflowID: b.WorkflowID, BotUserID: b.BotUserID, AI: b.ContainsAISteps, State: "enabled"}
	if sc.TriggerType == "" { // createShortcutFromBlock: no trigger_type is a link trigger
		sc.TriggerType = "shortcut"
	}
	if b.Title != nil {
		sc.Title = b.Title.Text
	}
	if b.Description != nil {
		sc.Description = b.Description.Text
	}
	if sc.TriggerType == "shortcut" {
		sc.URL = SafeURL(b.URL)
	}
	sc.Byline, sc.BylineIcon = triggerTypeLabel(sc.TriggerType, b.TriggerSubtype, b.ServiceName)
	if f := workflowSlugs.Load(); f != nil && sc.TriggerID != "" {
		if s, known := (*f)(sc.TriggerID); known && s.Error == "" {
			sc.Icon = SafeImage(s.Icon)
		}
	}
	// WorkflowEntity's bot member (KrN7: getMemberById, then
	// isUnknownObject and isDeleted). No id, or one that isn't
	// member-shaped, is a non-existent member at once, as is a member
	// users.info didn't find: the enabled card. Only a member-shaped id
	// still being fetched is the unknown object, the loading card; a
	// deleted member is the "no longer exists" one.
	switch ms, ok := c.r.(MemberStater); {
	case sc.BotUserID == "" || !LooksLikeMemberID(sc.BotUserID):
	case ok:
		del, known := ms.MemberDeleted(sc.BotUserID)
		switch {
		case !known:
			sc.State = "loading"
		case del:
			sc.State = "deleted"
		}
	}
	return Block{Kind: ShortcutBlock, BlockID: b.BlockID, Shortcut: sc}
}

// eventTriggers are the event triggers WorkflowTriggerTypeLabel words
// (8hZq Bx), with their icons (getTriggerTypeIcon).
var eventTriggers = map[string][2]string{
	"app_mentioned":                  {"Starts when an app is mentioned", "mentions"},
	"channel_archived":               {"Starts when a channel is archived", "channel"},
	"channel_created":                {"Starts when a channel is created", "channel"},
	"channel_deleted":                {"Starts when a channel is deleted", "channel"},
	"channel_renamed":                {"Starts when a channel is renamed", "channel"},
	"channel_shared":                 {"Starts when a channel is shared with an external workspace", "shared-channel"},
	"channel_unarchived":             {"Starts when a channel is unarchived", "channel"},
	"channel_unshared":               {"Starts when a Slack Connect channel is disconnected", "shared-channel"},
	"dnd_updated":                    {"Starts when Do Not Disturb status is changed", "notifications-dnd"},
	"emoji_changed":                  {"Starts when an emoji is changed", "emoji"},
	"list_record_created":            {"Starts when a list item is added", "lists"},
	"list_record_updated":            {"Starts when a list item is updated", "lists"},
	"message_posted":                 {"Starts when a message is posted with keywords", "message"},
	"pin_added":                      {"Starts when a pin is added", "pin"},
	"pin_removed":                    {"Starts when a pin is removed", "pin"},
	"reaction_added":                 {"Starts when a reaction is used", "add-reaction"},
	"reaction_removed":               {"Starts when a reaction is removed", "emoji"},
	"shared_channel_invite_accepted": {"Starts when someone accepts a Slack Connect invitation", "shared-channel"},
	"shared_channel_invite_approved": {"Starts when a Slack Connect invitation is approved", "shared-channel"},
	"shared_channel_invite_received": {"Starts when someone receives a Slack Connect invitation", "shared-channel"},
	"shared_channel_invite_declined": {"Starts when someone declines a Slack Connect invitation", "shared-channel"},
	"user_joined_channel":            {"Starts when someone joins a channel", "user-add"},
	"user_joined_team":               {"Starts when someone joins a team", "user-add"},
	"user_left_channel":              {"Starts when someone leaves a channel", "user-add"},
	"message_metadata_posted":        {"Starts with a custom event", "code"},
	"huddle_started":                 {"Starts when a huddle begins", "headphones"},
}

// triggerTypeLabel is getTriggerTypeLabel and getTriggerTypeIcon for a
// shortcut info made from a block: a link trigger has neither, nor does
// an event trigger whose subtype isn't one of Slack's (1YeR G_ strips a
// "slack#/events/" prefix and reads the first path part). The
// WorkflowTriggerTypeLabel needs both, so either missing is no byline. A
// service trigger names the block's service_name (createShortcutFromBlock
// keeps it as serviceName), else "a third party".
func triggerTypeLabel(typ, subtype, service string) (label, icon string) {
	switch typ {
	case "service":
		if service != "" {
			return "Starts with a " + service + " event", "code"
		}
		return "Starts with a third party event", "code"
	case "scheduled":
		return "Starts on a schedule", "clock"
	case "webhook":
		return "Starts with a Webhook event", "webhook"
	case "external": // no subtype on a block's info
		return "Starts with a Salesforce event", "code"
	case "event":
		ev := strings.TrimPrefix(subtype, "slack#/events/")
		ev, _, _ = strings.Cut(ev, "/")
		if e, ok := eventTriggers[ev]; ok {
			return e[0], e[1]
		}
	}
	return "", ""
}
