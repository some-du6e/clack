package slackc

import (
	"sync"

	"gioui.org/layout"

	"github.com/some-du6e/clack/internal/ui/slackc/css"
)

// ChannelEntity is Slack's InlineChannelEntity (client-boot X3qV) for a
// native that draws a channel's icon and name itself: BaseInlineEntity's
// span (pQqe) with the call site's className, then
// c-inline_channel_entity c-base_inline_entity
// c-base_inline_entity--has_avatar (and --truncate_block when it
// truncates), data-qa inline_channel_entity and the channel's
// data-channel-id. Inside, under MaybeTruncate's span, the avatar slot is
// InlineChannelIcon (N4L8): span.c-base_inline_entity__avatar
// .c-inline_channel_icon.c-inline_channel_icon--space with
// data-inline-channel-type-icon set to getChannelIconType's name
// (/HhV: "channel", "lock-filled" for a private channel, "archive",
// "archive-filled"); then span.c-base_inline_entity__primary_content >
// span.c-inline_channel_entity__content > span.c-channel_entity__name
// (ChannelName, g5tH) holds the name. The icon and the name are declared
// kids (ClassDef.Kids), so a plugin's
// .c-inline_channel_entity:has([data-inline-channel-type-icon^="lock"])
// (StreamerMode) sees a private channel's lock, and Name is the name's
// style under the entity, so the rule's .c-channel_entity__name part
// (its blur, the :hover reveal) reaches what the native draws.
type ChannelEntity struct {
	classes string

	mu    sync.Mutex
	kinds map[string]*ClassDef // entity element by icon name

	wrap, primary, content, name *ClassDef
}

// NewChannelEntity declares an InlineChannelEntity whose className is
// classes (space-separated, Slack's call-site className first).
func NewChannelEntity(classes string, truncate bool) *ChannelEntity {
	cls := classes + " c-inline_channel_entity c-base_inline_entity c-base_inline_entity--has_avatar"
	if truncate {
		cls += " c-base_inline_entity--truncate_block"
	}
	e := &ChannelEntity{classes: cls, kinds: map[string]*ClassDef{},
		wrap:    Class("").Tag("span"),
		content: Class("c-inline_channel_entity__content").Tag("span"),
		name:    Class("c-channel_entity__name", "data-qa", "inline_channel_entity__name").Tag("span"),
	}
	e.primary = Class("c-base_inline_entity__primary_content").Tag("span")
	return e
}

// def is the entity element for a channel whose icon is icon.
func (e *ChannelEntity) def(icon string) *ClassDef {
	e.mu.Lock()
	defer e.mu.Unlock()
	if d := e.kinds[icon]; d != nil {
		return d
	}
	name := Class("c-channel_entity__name", "data-qa", "inline_channel_entity__name").Tag("span")
	content := Class("c-inline_channel_entity__content").Tag("span").Kids(name)
	primary := Class("c-base_inline_entity__primary_content").Tag("span").Kids(content)
	ic := Class("c-base_inline_entity__avatar c-inline_channel_icon c-inline_channel_icon--space", "data-inline-channel-type-icon", icon).Tag("span")
	wrap := Class("").Tag("span").Kids(ic, primary)
	d := Class(e.classes, "data-qa", "inline_channel_entity").Tag("span").Kids(wrap)
	e.kinds[icon] = d
	return d
}

// Enter tags the entity for channel id with icon (getChannelIconType's
// name); st is its hover state. Pair with Leave, like ClassDef.Enter.
func (e *ChannelEntity) Enter(gtx layout.Context, st css.State, id, icon string) Styled {
	if Styles.Empty() {
		return Styled{}
	}
	return e.def(icon).EnterAttr(gtx, st, 0, "data-channel-id", id)
}

// Name is the style of the channel name (span.c-channel_entity__name)
// inside the entity s returned, through Slack's elements around it.
func (e *ChannelEntity) Name(s Styled) Styled {
	return s.Child(e.wrap, 0).Child(e.primary, 0).Child(e.content, 0).Child(e.name, 0)
}
