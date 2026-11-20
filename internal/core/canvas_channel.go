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

// FileChannelKind is the file a file channel ch belongs to and what it
// is, "canvas" or "list", as Slack's RedirectFileChannel middleware and
// jumpToMessage read it from the file in the store (Tac1 uz(getFileById)
// with fileIsCanvas, fileIsList). kind is "" for a conversation that is no
// file channel, or one whose file is gone or another kind: Slack mounts
// the conversation then. A held message ts whose own document_mention
// names the file is a canvas's. wait is true while files.info hasn't
// answered for a stored file channel (the store asked): Slack's prefetch
// has the file by then, Clack waits for it.
func (a *App) FileChannelKind(ch, ts string) (fileID, kind string, wait bool) {
	if !strings.HasPrefix(ch, "C") {
		return "", "", false
	}
	fileID = FileOfChannel(ch)
	if ts != "" {
		if m, held := a.S.FindHeld(ch, ts); held && m.DocumentMention != nil && m.DocumentMention.FileID == fileID {
			if ref, known := a.S.KnownCanvasRef(fileID); !known || ref.Kind == "canvas" {
				return fileID, "canvas", false
			}
		}
	}
	c, stored := a.S.Conv(ch)
	if !stored {
		c, stored = a.S.ExtConv(ch)
	}
	if stored && !IsFileChannel(c) {
		return "", "", false
	}
	var ref richtext.CanvasRef
	var known bool
	if stored {
		ref, known = a.S.CanvasRef(fileID) // a miss is asked for (resolveCanvasRef)
	} else {
		ref, known = a.S.KnownCanvasRef(fileID)
	}
	switch {
	case known && (ref.Kind == "canvas" || ref.Kind == "list"):
		return fileID, ref.Kind, false
	case known || !stored:
		return "", "", false
	}
	return fileID, "", true
}

// CanvasBlocked reports whether canvas fileID is one Slack Connect keeps
// from the viewer (files.info's slack_connect_*_blocked): a jump opens it
// (isBlockedExternalCanvas), RedirectFileChannel doesn't (fileIsCanvas
// reads mode quip).
func (a *App) CanvasBlocked(fileID string) bool {
	ref, known := a.S.KnownCanvasRef(fileID)
	return known && ref.Blocked
}

// CanvasOfJump is FileChannelKind for a canvas: the canvas a jump to
// message ts of ch opens (Slack's jumpToMessage, isSpaceship), or wait.
func (a *App) CanvasOfJump(ch, ts string) (fileID string, ok, wait bool) {
	fid, kind, wait := a.FileChannelKind(ch, ts)
	switch {
	case kind == "canvas":
		return fid, true, false
	case wait:
		return fid, false, true
	}
	return "", false, false
}

// ListRecordOf is the record of list file channel ch whose comment
// thread holds message ts (its thread root's ts thread, "" for a top-level
// message): the slack_list.list_record_id of the root the store holds
// (Slack's jumpToMessage reads it from the held root, wc0P isList), else
// the loaded list's record with that thread_ts. "" while neither knows:
// ListRecordFetch reads the root first, as Slack's List view does
// ("load-list-record-root-message").
func (a *App) ListRecordOf(ch, thread, ts string) string {
	root := thread
	if root == "" {
		root = ts
		// a reply names its thread (jumpToMessage's getThreadTsToOpen:
		// isReply → thread_ts)
		if m, held := a.S.FindHeld(ch, ts); held && m.ThreadTS != "" && m.ThreadTS != ts {
			root = m.ThreadTS
		}
	}
	if root == "" {
		return ""
	}
	if m, held := a.S.FindHeld(ch, root); held && m.SlackList != nil && m.SlackList.ListRecordID != "" {
		return m.SlackList.ListRecordID
	}
	return a.listRecordByThread(FileOfChannel(ch), root)
}

// ListRecordFetch is ListRecordOf for a root the store doesn't hold:
// fetched by id first (messages.list). rec is "" for a root without a
// slack_list, and err says the root couldn't be had.
func (a *App) ListRecordFetch(ctx context.Context, ch, thread, ts string) (rec string, err error) {
	root := thread
	if root == "" {
		root = ts
	}
	got, answered := a.hydrateOK(ctx, map[string][]string{ch: {root}})
	m, ok := got[ch+"/"+root]
	if ok && thread == "" && m.ThreadTS != "" && m.ThreadTS != m.TS {
		// a reply: its thread's root names the record
		root = m.ThreadTS
		if rec := a.ListRecordOf(ch, root, ""); rec != "" {
			return rec, nil
		}
		got, answered = a.hydrateOK(ctx, map[string][]string{ch: {root}})
		m, ok = got[ch+"/"+root]
	}
	switch {
	case ok && m.SlackList != nil:
		return m.SlackList.ListRecordID, nil
	case ok:
		return a.listRecordByThread(FileOfChannel(ch), root), nil
	case answered[ch]:
		return "", fmt.Errorf("%s/%s: no such message", ch, root)
	}
	return "", fmt.Errorf("%s/%s: messages.list didn't answer", ch, root)
}

// listRecordByThread is the loaded list's record whose comment thread
// is root ("" if the list isn't loaded or has none).
func (a *App) listRecordByThread(listID, root string) string {
	st := a.lists()
	st.mu.Lock()
	defer st.mu.Unlock()
	if d := st.docs[listID]; d != nil {
		for i := range d.Records {
			if d.Records[i].ThreadTS == root {
				return d.Records[i].ID
			}
		}
	}
	return ""
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
