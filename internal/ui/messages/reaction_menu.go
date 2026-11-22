package messages

import (
	"image"

	"gioui.org/f32"

	"github.com/some-du6e/clack/internal/ui/popups"
)

// A right-click on a reaction pill, as Slack's client and desktop shell
// answer it (gantry boot-shared Reaction, build a09b625d90aa):
//
//   - The pill's onContextMenu is maybeOpenImportExternalEmojiModal
//     (MDnx) for a custom emoji, which acts only on an emoji another
//     workspace owns (Slack Connect); otherwise nothing stops the event.
//   - The message list's should_show_message_contextmenu (Ay9+) leaves
//     an <img> (the emoji) and a text character (the count) to the
//     desktop shell: its image menu (Copy Image, Copy Image URL) for the
//     emoji, and for the count buildMenuForNonText, which has no items,
//     so no menu at all.
//   - Elsewhere in the pill (its padding, the gap between emoji) the
//     list opens the message actions menu.

// reactionMenu is the menu for a press at p (list coordinates) on row r
// (item i), when p is on one of r's reaction pills: handled is false
// when it isn't, and o is nil when Slack opens no menu there.
func (s *listState) reactionMenu(h Host, r *row, i int, p f32.Point, at image.Point) (o popups.Overlay, handled bool) {
	// a plugin's tree drew the pill: its emoji and count told their own
	// tags about the press
	for k := range r.pills {
		part := r.pills[k].ctxPart
		if !part.on {
			continue
		}
		for j := range r.pills {
			r.pills[j].ctxPart = pillPart{}
		}
		if part.count || part.url == "" {
			return nil, true
		}
		return popups.NewImageMenu(h, at, part.url, false), true
	}
	pt := image.Pt(int(p.X), int(p.Y)-s.pos[i]) // row-local, as reactRects
	for k, rect := range r.reactRects {
		if k >= len(r.pills) || !pt.In(rect) {
			continue
		}
		pl := &r.pills[k]
		in := pt.Sub(rect.Min)
		for _, e := range pl.emojiAt {
			if in.In(e.r) && e.url != "" {
				return popups.NewImageMenu(h, at, e.url, false), true
			}
		}
		if in.In(pl.countAt) {
			return nil, true
		}
		return s.messageMenu(h, r, at), true
	}
	return nil, false
}

// clearPillParts forgets the parts the last context press was on, once
// the list has answered it (or not: another row's menu, a taken press).
func (s *listState) clearPillParts() {
	for i := s.visible[0]; i < s.visible[1] && i < len(s.items); i++ {
		if r := s.rows[s.items[i].key]; r != nil {
			for k := range r.pills {
				r.pills[k].ctxPart = pillPart{}
			}
		}
	}
}
