package popups

import (
	"image"
	"strings"

	"gioui.org/f32"

	"github.com/some-du6e/clack/internal/core"
	"github.com/some-du6e/clack/internal/ui/richview"
)

// ContentMenu is the menu for a right-click at local (v's coordinates) in
// message m's body, wherever Slack's message lists draw a message
// (the channel's, the thread pane's, the Threads view's: AEjl and duay,
// both behind should_show_message_contextmenu, Ay9+), and at is the
// pointer in window coordinates. open opens a link the way a click
// would. ok is false where the list opens the message's own actions menu
// (text, empty space, Block Kit's image element). popups/actions_context.go
// has the sources of each menu:
//
//   - a v3 link preview: renderV3Attachment's own menu, the whole card;
//   - inline code or a code block: Copy this code / Copy this code block;
//   - a link: the desktop shell's link menu (with an emoji or image in
//     it, its image items too);
//   - a user mention: the member menu; a channel: the channel menu; a
//     user group: the link menu for its admin link;
//   - an image block's or an attachment's image (AttachmentImage, the
//     background of an <a href>): the link menu; an animated one under
//     its play button, or an <img>: the image menu;
//   - a file: Slack's file menu; an emoji: the image menu.
func ContentMenu(h Host, at image.Point, v *richview.View, local f32.Point, m core.Message, open func(string)) (Overlay, bool) {
	if v == nil {
		return nil, false
	}
	if ah, ok := v.AttachmentAt(local); ok && ah.Att != nil {
		return NewAttachmentMenu(h, at, AttachmentMenu{CanRemove: ah.CanRemove,
			Channel: m.Channel, TS: m.TS, ID: ah.Att.ID, CopyURL: ah.CopyURL}), true
	}
	hit := v.HitAt(local)
	if code, block, ok := v.CodeAt(local); ok && hit.Kind == richview.HitNone && hit.EmojiURL == "" {
		return NewCodeMenu(h, at, code, block), true
	}
	openFn := func(u string) func() {
		if open == nil {
			return nil
		}
		return func() { open(u) }
	}
	switch hit.Kind {
	case richview.HitLink:
		return NewLinkMenu(h, at, LinkMenu{URL: hit.URL, Image: hit.EmojiURL, Open: openFn(hit.URL)}), true
	case richview.HitUser:
		if isMember(hit.ID) {
			return NewMemberMenu(h, at, hit.ID), true
		}
	case richview.HitChannel:
		if hit.ID != "" {
			return NewChannelContextMenu(h, at, hit.ID), true
		}
	case richview.HitUsergroup:
		return NewLinkMenu(h, at, LinkMenu{URL: UsergroupLink(h)}), true
	case richview.HitImage:
		if hit.Link != "" {
			return NewLinkMenu(h, at, LinkMenu{URL: hit.Link, Open: openFn(hit.Link)}), true
		}
		if hit.Image != nil && hit.Image.URL != "" {
			return NewImageMenu(h, at, hit.Image.URL, hit.Image.Proxy), true
		}
	case richview.HitFile:
		if hit.File != nil {
			return NewFileMenu(h, at, m, hit.File), true
		}
	case richview.HitNone:
		if hit.EmojiURL != "" {
			return NewImageMenu(h, at, hit.EmojiURL, false), true
		}
	}
	return nil, false
}

// isMember reports whether id is a user (not a broadcast: here, channel,
// everyone).
func isMember(id string) bool {
	return len(id) > 1 && (strings.HasPrefix(id, "U") || strings.HasPrefix(id, "W"))
}
