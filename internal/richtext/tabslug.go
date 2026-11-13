package richtext

// Channel-tab slugs, 1:1 with Slack's web client (boot-shared NY16
// MrkdwnChannelTab / ResolvedChannelTabSlug, build 3889f0c93d30,
// 2026-11-13). A rich_text channel element with a tab_id (and no
// from_llm) is a RichTextSlug (Pc7Q) when its tab resolves: the tab's
// icon (useTabIconProps, /LFd) and the label
//
//	{tabLabel} in {channelName}
//
// (R.rt, the mrkdwn-channel-tab namespace). The tab label is
// useTabLabel's (83sY). channelName is formatChannelName (ZuZy): "#name"
// for a channel whose icon is the plain channel one, else the channel's
// icon (getChannelIconType with showCodeIcon, /HhV: lock, code, archive,
// …) then a space and the name. In the slug's own channel (the message
// pane's channelId), or when the channel has no name, the label is the
// tab alone. A slug whose tab doesn't resolve stays the plain channel
// mention (BaseMrkdwnChannel).

// TabSlug is a resolved channel-tab slug's parts (core's ChannelTabSlug).
type TabSlug struct {
	Label  string // useTabLabel
	Icon   string // useTabIconProps' name ("" when Slack has none)
	Name   string // getChannelDisplayName, without "#" ("" when unknown)
	ChIcon string // getChannelIconType's name when it isn't "channel": drawn instead of "#"
}

// ChannelTabResolver is an optional Resolver extension (store.Store):
// MrkdwnChannelTab's resolution of a slug's tab, false when Slack falls
// back to BaseMrkdwnChannel.
type ChannelTabResolver interface {
	ChannelTabSlug(channelID, tabID string) (TabSlug, bool)
}

// TabSlugText is ResolvedChannelTabSlug's label for t: in its own channel
// (own) or without a channel name, the tab label alone; else "{tabLabel}
// in {channelName}". pre is the bytes before the channel's name, where
// its icon goes when t.ChIcon is set.
func TabSlugText(t TabSlug, own bool) (text string, pre int) {
	if own || t.Name == "" {
		return t.Label, 0
	}
	head := t.Label + " in "
	if t.ChIcon != "" {
		return head + t.Name, len(head)
	}
	return head + "#" + t.Name, len(head)
}

// channelTabSpan is the ChannelTab span for a tab slug, false when it
// doesn't resolve.
func (c *builder) channelTabSpan(e *bk) (Span, bool) {
	tr, ok := c.r.(ChannelTabResolver)
	if !ok {
		return Span{}, false
	}
	t, ok := tr.ChannelTabSlug(e.ChannelID, e.TabID)
	if !ok {
		return Span{}, false
	}
	text, pre := TabSlugText(t, c.chID != "" && c.chID == e.ChannelID)
	return Span{Kind: ChannelTab, ID: e.ChannelID, URL: "/archives/" + e.ChannelID + "/tabs/" + e.TabID,
		Text: text, Pre: pre, Icon: t.Icon, ChIcon: t.ChIcon, Chan: t.Name}, true
}
