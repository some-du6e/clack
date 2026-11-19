package core

import (
	"context"
	"fmt"
	"strings"

	"github.com/some-du6e/clack/internal/richtext"
	"github.com/some-du6e/clack/internal/slack"
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
//     message's document_mention.section_id for a mention, with its
//     comment thread for a comment (CanvasJump).

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

// IsFileChannelID is Slack's isFileChannelById (gxWy OR): the stored
// conversation's is_file, or for one the store doesn't hold, whether the
// file its id names (C swapped for F) is known. Activity's item menu
// calls such an item canvas-related (ITY6 isCanvasRelated) and leaves out
// Open in home and Remind me about this.
func (a *App) IsFileChannelID(ch string) bool {
	if !strings.HasPrefix(ch, "C") {
		return false
	}
	if c, ok := a.S.Conv(ch); ok {
		return IsFileChannel(c)
	}
	if c, ok := a.S.ExtConv(ch); ok {
		return IsFileChannel(c)
	}
	_, known := a.S.KnownCanvasRef(FileOfChannel(ch))
	return known
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

// CanvasOfJump is the canvas a jump to message ts of ch opens (Slack's
// jumpToMessage, isSpaceship): CanvasOfChannel's. While files.info hasn't
// answered for a stored file channel, it is the canvas the message's own
// document_mention names. Otherwise wait is true: the store asked
// files.info, and the jump waits for the "names" that brings the file id
// (fileID) instead of opening the file channel.
func (a *App) CanvasOfJump(ch, ts string) (fileID string, ok, wait bool) {
	if fid, ok := a.CanvasOfChannel(ch); ok {
		return fid, true, false
	}
	if !strings.HasPrefix(ch, "C") {
		return "", false, false
	}
	fileID = FileOfChannel(ch)
	// the message names its canvas itself (a document_mention), whatever
	// the store holds of the channel
	if ts != "" {
		if m, held := a.S.FindHeld(ch, ts); held && m.DocumentMention != nil && m.DocumentMention.FileID == fileID {
			if ref, known := a.S.KnownCanvasRef(fileID); !known || ref.Kind == "canvas" {
				return fileID, true, false
			}
		}
	}
	c, stored := a.S.Conv(ch)
	if !stored {
		c, stored = a.S.ExtConv(ch)
	}
	if !stored || !IsFileChannel(c) {
		return "", false, false
	}
	if _, known := a.S.KnownCanvasRef(fileID); known {
		return "", false, false // a list's file channel, or a file that's gone
	}
	a.S.CanvasRef(fileID) // asked for (resolveCanvasRef), if CanvasOfChannel hadn't
	return fileID, false, true
}

// CanvasJump is where Slack opens a jump into canvas file channel ch.
// lFpV openDocWithThreadTs reads the message at the jump's thread, else at
// its message (wc0P "eo && !ed && (ed = i)"):
//
//   - a mention (document_mention): the canvas at its section_id;
//   - a comment's root (document_comment_root) with replies: the canvas
//     with that comment thread (openDoc's commentThreadId →
//     showQuipCommentThread). A root without replies opens just the
//     canvas (suppressCommentThread);
//   - any other message (a reply): just the canvas.
//
// known is false while the store doesn't hold that message:
// CanvasJumpFetch reads it first, as lFpV does.
func (a *App) CanvasJump(ch, thread, ts string) (section, comment string, known bool) {
	at := thread
	if at == "" {
		at = ts
	}
	if at == "" {
		return "", "", true
	}
	m, ok := a.S.FindHeld(ch, at)
	if !ok {
		return "", "", false
	}
	section, comment = canvasJumpOf(&m)
	return section, comment, true
}

// CanvasJumpFetch is CanvasJump for a message the store doesn't hold:
// fetched by id first (lFpV's fetchMessagesByIds, reason
// "quip-root-comment-not-loaded", is messages.list). When the message
// can't be had nothing opens, as in Slack.
func (a *App) CanvasJumpFetch(ctx context.Context, ch, thread, ts string) (section, comment string, err error) {
	at := thread
	if at == "" {
		at = ts
	}
	got, answered := a.hydrateOK(ctx, map[string][]string{ch: {at}})
	m, ok := got[ch+"/"+at]
	switch {
	case ok:
		section, comment = canvasJumpOf(&m)
		return section, comment, nil
	case answered[ch]:
		return "", "", fmt.Errorf("%s/%s: no such message", ch, at)
	}
	return "", "", fmt.Errorf("%s/%s: messages.list didn't answer", ch, at)
}

// canvasJumpOf is what openDocWithThreadTs opens for message m.
func canvasJumpOf(m *slack.Message) (section, comment string) {
	switch {
	case m.DocumentMention != nil:
		return m.DocumentMention.SectionID, ""
	case m.Subtype == "document_comment_root" && m.ReplyCount > 0:
		return "", m.TS
	}
	return "", ""
}
