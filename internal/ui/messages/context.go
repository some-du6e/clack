package messages

import (
	"context"
	"image"
	"strings"

	"gioui.org/f32"
	"gioui.org/io/pointer"
	"gioui.org/layout"

	"github.com/some-du6e/clack/internal/ui/ctxclick"
	"github.com/some-du6e/clack/internal/ui/popups"
	"github.com/some-du6e/clack/internal/ui/richview"
)

// Right-click menus on messages. The list (an ancestor of every row) sees
// each context press (ctxclick.Press: a secondary press, or Ctrl+click on
// macOS). Rows that open their own menu (the sender's name and avatar:
// the member menu) mark the press taken while they lay out; otherwise,
// once the rows have laid out, contextMenu opens what Slack opens for the
// spot (popups/actions_context.go has the sources):
//
//   - with text selected: the text selection menu (Copy text, Search in
//     Slack);
//   - a link: the link menu (Copy Link, Open Link, plus the image items
//     when the link holds an emoji or image);
//   - a user mention: the member menu; a channel link: the channel menu;
//     a user group: the link menu for its admin/user_groups link;
//   - an image: Copy Image / Copy Image URL; an emoji: the same for its
//     image;
//   - a file (card, gallery image, video): the file menu;
//   - a v3 link preview (row, social, video, music, image cards): the
//     attachment's menu (Remove preview for the sender, Copy link);
//   - inline code or a code block: Copy this code / Copy this code block
//     (the desktop shell's menu with BaseCopyCodeBlockContextMenuTrigger's
//     item);
//   - anywhere else on the message, text included: the message actions
//     menu (More actions with the hover toolbar's group on top).
//
// Slack's message list itself leaves a click on plain text to the
// desktop shell's menu, which is empty there off macOS; Clack opens the
// message's menu instead, as Slack does on the message's empty space.

// ctxPress is a context-menu press the list saw this frame.
type ctxPress struct {
	pos   f32.Point // list coordinates
	ok    bool
	taken bool // a row opened its own menu for it
}

// contextMenu opens the menu for this frame's context press, if any.
// origin is the list's top-left in window coordinates.
func (s *listState) contextMenu(h Host, origin image.Point) {
	p := s.ctx
	s.ctx = ctxPress{}
	if !p.ok || p.taken {
		return
	}
	i := s.itemAt(p.pos)
	if i < 0 {
		return
	}
	r := s.rows[s.items[i].key]
	if r == nil || r.edit != nil || r.src.TS == "" {
		return // the inline editor has its own (Cut/Copy/Paste) menu
	}
	at := s.anchor(h, origin, image.Point{})
	h.ShowOverlay(s.contextMenuFor(h, r, i, p.pos, at))
}

// itemAt is the visible message item at p (exactly; dividers and gaps
// get no menu), or the item whose hover toolbar is at p.
func (s *listState) itemAt(p f32.Point) int {
	pt := image.Pt(int(p.X), int(p.Y))
	if s.tbFor >= 0 && s.tbFor < len(s.items) && pt.In(s.tbRect) {
		return s.tbFor
	}
	for i := s.visible[0]; i < s.visible[1] && i < len(s.items); i++ {
		if s.items[i].kind == itMsg && s.heights[i] > 0 && pt.Y >= s.pos[i] && pt.Y < s.pos[i]+s.heights[i] {
			return i
		}
	}
	return -1
}

// contextMenuFor picks the menu for a press at p (list coordinates) on
// row r (item i); at is the pointer in window coordinates.
func (s *listState) contextMenuFor(h Host, r *row, i int, p f32.Point, at image.Point) popups.Overlay {
	m := r.src
	if s.sel.active {
		if t := s.selectedText(h); t != "" {
			return popups.NewTextSelectionMenu(h, at, t)
		}
	}
	local := p.Sub(f32.Pt(float32(r.bodyRect.Min.X), float32(s.pos[i]+r.bodyRect.Min.Y)))
	if local.X >= 0 && local.Y >= 0 && int(local.X) < r.bodyRect.Dx() && int(local.Y) < r.bodyRect.Dy() {
		if ah, ok := r.rv.AttachmentAt(local); ok && ah.Att != nil {
			// renderV3Attachment's onContextMenu: the attachment's own
			// menu, wherever in the card (popups.NewAttachmentMenu)
			return popups.NewAttachmentMenu(h, at, popups.AttachmentMenu{CanRemove: ah.CanRemove,
				Channel: m.Channel, TS: m.TS, ID: ah.Att.ID, CopyURL: ah.CopyURL})
		}
		hit := r.rv.HitAt(local)
		if code, block, ok := r.rv.CodeAt(local); ok && hit.Kind == richview.HitNone && hit.EmojiURL == "" {
			// BaseCopyCodeBlockContextMenuTrigger: the shell's menu with
			// Slack's one item (popups.NewCodeMenu)
			return popups.NewCodeMenu(h, at, code, block)
		}
		switch hit.Kind {
		case richview.HitLink:
			url := hit.URL
			return popups.NewLinkMenu(h, at, popups.LinkMenu{URL: url, Image: hit.EmojiURL,
				Open: func() { r.handler.OpenURL(url) }})
		case richview.HitUser:
			if isMemberID(hit.ID) {
				return popups.NewMemberMenu(h, at, hit.ID)
			}
		case richview.HitChannel:
			if hit.ID != "" {
				return popups.NewChannelContextMenu(h, at, hit.ID)
			}
		case richview.HitUsergroup:
			return popups.NewLinkMenu(h, at, popups.LinkMenu{URL: popups.UsergroupLink(h)})
		case richview.HitImage:
			if hit.Image != nil && hit.Image.URL != "" {
				return popups.NewImageMenu(h, at, hit.Image.URL, hit.Image.Proxy)
			}
		case richview.HitFile:
			if hit.File != nil {
				return popups.NewFileMenu(h, at, m, hit.File)
			}
		case richview.HitNone:
			if hit.EmojiURL != "" {
				return popups.NewImageMenu(h, at, hit.EmojiURL, false)
			}
		}
	}
	return s.messageMenu(h, r, at)
}

// messageMenu is the message actions menu for r, opened at the pointer,
// with the same inline hooks as the hover toolbar's More actions.
func (s *listState) messageMenu(h Host, r *row, at image.Point) *popups.Menu {
	m := r.src
	app := h.Core()
	where := popups.InChannel
	if s.thread {
		where = popups.InThread
	}
	return popups.NewMessageContextMenu(h, at, m, where, popups.MessageMenuOpts{
		Delete:  func(ctx context.Context, ch, ts string) error { return deleteMessage(ctx, app, ch, ts) },
		OnEdit:  func() { s.startEdit(h, m.TS) },
		OnReply: func() { h.OpenThread(m.Channel, threadRoot(&m)) },
		OnTodo:  func() { s.toggleSaved(h, app, m) },
	})
}

// isMemberID reports whether id is a user (not a broadcast: here,
// channel, everyone).
func isMemberID(id string) bool {
	return len(id) > 1 && (strings.HasPrefix(id, "U") || strings.HasPrefix(id, "W"))
}

// noMenuOn takes the context presses on tag without opening a menu: a
// Slackbot custom response's sender (the team's icon and name) is not
// interactive in Slack, so a right-click there opens neither the member
// menu nor the message's (custom_response.go). The body still gets the
// message menu.
func (r *row) noMenuOn(gtx layout.Context, tag *ctxTag) {
	for {
		ev, ok := gtx.Event(pointer.Filter{Target: tag, Kinds: pointer.Press})
		if !ok {
			return
		}
		if e, ok := ev.(pointer.Event); ok && ctxclick.Press(e) {
			if l := r.rc.list; l != nil {
				l.ctx.taken = true
			}
		}
	}
}
