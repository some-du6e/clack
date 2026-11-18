package core

import (
	"strings"

	"github.com/some-du6e/clack/internal/richtext"
)

// A canvas's hidden file channel (sidebar_docs.go: the file's id with F
// swapped for C, is_file) holds its comments and its mention messages
// (subtype document_mention). Slack never shows it as a channel where a
// canvas belongs (build 2c0d02ca130f):
//
//   - its channel entity (client-boot X3qV InlineChannelEntity, which
//     Activity's destination draws, VeYL): the icon is getChannelIconType's
//     file channel case (/HhV: "canvas" for a canvas, "lists" for a list,
//     "file" for a file it doesn't know yet), and the name is ChannelName's
//     title swap (g5tH: renderQuipTitle over the canvas's title_blocks,
//     caption size, when the file is a canvas), never the channel's own
//     name with its :shortcodes: or a private channel's lock;
//   - a jump to one of its messages (wc0P jumpToMessage) is isSpaceship:
//     the canvas opens (lFpV openDocWithThreadTs → zsuR openDoc), at the
//     message's document_mention.section_id for a mention.

// CanvasOfChannel is the canvas whose file channel ch is: a stored file
// channel (or one the store doesn't hold) whose file files.info said is a
// canvas. ok is false for any other conversation, a list's file channel,
// and one whose file isn't known yet (the store asks files.info for it).
func (a *App) CanvasOfChannel(ch string) (fileID string, ok bool) {
	if !strings.HasPrefix(ch, "C") {
		return "", false
	}
	c, stored := a.S.Conv(ch)
	if !stored {
		c, stored = a.S.ExtConv(ch)
	}
	if stored && !IsFileChannel(c) {
		return "", false
	}
	fileID = FileOfChannel(ch)
	var ref richtext.CanvasRef
	var known bool
	if stored {
		ref, known = a.S.CanvasRef(fileID) // a miss is asked for (resolveCanvasRef)
	} else {
		ref, known = a.S.KnownCanvasRef(fileID)
	}
	if !known || ref.Kind != "canvas" {
		return "", false
	}
	return fileID, true
}

// CanvasTitleOf is a canvas's title as Slack's channel entity draws it
// (renderQuipTitle: title_blocks with their emoji as images, else the
// title string's :emoji:; richtext.CanvasTitle) and the file's kind
// ("canvas", "list", ""). known is false until files.info answered: the
// store asks, and "names" with the file id follows.
func (a *App) CanvasTitleOf(fileID string) (spans []richtext.Span, kind string, known bool) {
	ref, ok := a.S.CanvasRef(fileID)
	if !ok {
		return nil, "", false
	}
	return richtext.CanvasTitle(a.S, ref.Title, ref.TitleBlocks), ref.Kind, true
}

// MentionSection is the section a canvas mention points at: the stored
// message ch/ts's document_mention.section_id, as Slack's
// openDocWithThreadTs reads it from the message ("" for another message,
// or one the store doesn't hold).
func (a *App) MentionSection(ch, ts string) string {
	if ts == "" {
		return ""
	}
	m, ok := a.S.Find(ch, ts)
	if !ok || m.DocumentMention == nil {
		return ""
	}
	return m.DocumentMention.SectionID
}
