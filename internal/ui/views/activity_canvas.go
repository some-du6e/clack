package views

// Canvas mentions in Activity (Karim, α.102: "kinda messed up on canvas
// pings"; a click "put me in a limbo esque channel where the canvas
// contents are messages??"). Slack's item is an @-mention whose message
// (subtype document_mention, document_mention {file_id, section_id,
// mentioning_user_ids}) sits in the canvas's hidden file channel (core
// canvas_channel.go), as do its comments' threads. Builds 2c0d02ca130f
// and 548618a2169f2a90:
//
//   - the row's destination (VeYL, every layout: ActivityRowContentDense
//     and -Detailed, ActivityRowContentSingleLine and -MultiLine) is the
//     file channel's InlineChannelEntity: the canvas icon and the canvas's
//     title from its title_blocks, emoji as images (chanent.Canvas), not
//     the channel's own ":shortcode:" name behind a private channel's lock.
//     That holds for every item of the file channel, mentions and
//     comments alike;
//   - the click (GMkG handleOnClickActivityItem): in the inbox a mention
//     opens its message (jpad toMessage) in the column, which Slack's
//     RedirectFileChannel middleware turns into the thread view on the
//     mention (ui canvas_open.go): the thread pane, the mention its
//     highlighted root, the canvas its subtitle. A comment's thread_v2
//     item opens its comment thread (toQuipCommentThread) and another
//     reply opens its thread (toThread), in the column as any thread
//     does. With the persistent detail panel any other item jumps to its
//     message (wc0P jumpToMessage), which for a canvas's file channel
//     opens the canvas in Primary at the mention's section, or with the
//     comment's thread beside it (isSpaceship → lFpV openDocWithThreadTs →
//     zsuR openDoc): the view's own pane, the inbox staying in the Sidebar
//     (paneFileJump);
//   - a list's file channel holds its records' comment threads: a reply
//     there whose root names its record (slack_list) opens the record,
//     its thread shown (GMkG's el.C, before any thread rule:
//     selListRecord), and a jump opens the list at the record (isList).

import (
	"gioui.org/layout"

	"github.com/some-du6e/clack/internal/core"
	"github.com/some-du6e/clack/internal/slack"
	"github.com/some-du6e/clack/internal/ui/chanent"
)

// FileWhere is the container a navigation into a canvas's or a list's
// file channel opens the canvas or list in.
type FileWhere int

const (
	// FileSide is the secondary column (beside the inbox: Slack's
	// Secondary).
	FileSide FileWhere = iota
	// FileFull is the full view, in place of the page: a link, Open in
	// home.
	FileFull
	// FileInPane is the Activity persistent detail panel's own pane:
	// Slack's Primary, the inbox staying in the Sidebar.
	FileInPane
	// FileInSplit is the split view, where a jump that starts there stays
	// (getTargetContainer: shouldForceOpenInSplitView for a Split source):
	// a comment's thread on its back stack; the canvas and the list in the
	// side panel, as Clack's split view holds conversations only.
	FileInSplit
)

// FileOpener is optionally implemented by the Host: Slack's jumpToMessage
// into a canvas's or a list's file channel (wc0P isSpaceship → lFpV
// openDocWithThreadTs → zsuR openDoc, which opens the canvas at a
// mention's section, with a comment's thread beside it; isList, the list
// at the record), and what its column shows of one (ui/canvas_open.go).
type FileOpener interface {
	// OpenFileJump opens the jump to message ts (or thread) of file
	// channel ch, the canvas or list where says.
	OpenFileJump(ch, thread, ts string, where FileWhere)
	// FileShown reports whether the secondary column shows canvas or list
	// fileID.
	FileShown(fileID string) bool
	// FilePane draws file fileID in the persistent detail panel's pane
	// (after a FileInPane jump), as the jump left it.
	FilePane(fileID string) layout.Widget
	// PaneFile is the file the last FileInPane jump opened, and whether
	// it resolved: a canvas or a list, or "" for none (the pane shows the
	// item's conversation instead).
	PaneFile() (fileID string, done bool)
}

// actDocMention is an item's canvas mention (nil for other items).
func actDocMention(it *core.ActivityItem) *slack.MessageDocumentMention {
	if it == nil || it.Msg == nil || it.Msg.DocumentMention == nil || it.Msg.DocumentMention.FileID == "" {
		return nil
	}
	return it.Msg.DocumentMention
}

// canvasChannel is the canvas whose file channel the item's conversation
// is: a canvas mention's own (its message lives in the file channel), else
// one core knows as a canvas's file channel. pending is true for a file
// channel whose files.info hasn't answered yet (core asked; the row waits
// for it).
func (v *Activity) canvasChannel(it *core.ActivityItem) (fileID string, ok, pending bool) {
	if it == nil || it.Channel == "" {
		return "", false, false
	}
	if dm := actDocMention(it); dm != nil && core.FileOfChannel(it.Channel) == dm.FileID {
		return dm.FileID, true, false
	}
	return v.h.Core().CanvasOfJump(it.Channel, "")
}

// canvasEnt is a canvas's file channel as the row's InlineChannelEntity
// draws it once files.info is known (chanent.Canvas); a file that isn't a
// canvas (a list's) keeps the channel's own entity. known is false while
// files.info hasn't answered.
func (v *Activity) canvasEnt(fileID, ch string) (chanent.Ent, bool) {
	spans, kind, known := v.h.Core().CanvasTitleOf(fileID)
	if !known {
		return chanent.Ent{}, false
	}
	if kind != "canvas" {
		return v.channelEnt(ch), true
	}
	return chanent.Canvas(spans), true
}

// fileChannel is the canvas or list whose file channel the item's
// conversation is, and the file's kind ("canvas", "list"): a canvas
// mention's own, else what core knows. pending is true while files.info
// hasn't answered for a file channel (core asked).
func (v *Activity) fileChannel(it *core.ActivityItem) (fileID, kind string, pending bool) {
	if it == nil || it.Channel == "" {
		return "", "", false
	}
	if dm := actDocMention(it); dm != nil && core.FileOfChannel(it.Channel) == dm.FileID {
		return dm.FileID, "canvas", false
	}
	return v.h.Core().FileChannelKind(it.Channel, "")
}

// selListRecord is the list record the chosen item opens: a reply in a
// list's file channel whose thread's root names its record (GMkG reads
// the held root's slack_list, before any thread rule, and opens the
// record with its thread: el.C, defaultContentType "thread"). rec is ""
// for any other item.
func (v *Activity) selListRecord() (listID, rec string) {
	ch, thread := v.sel.Channel, v.selDesc.thread
	if !v.selOK || ch == "" || thread == "" || !v.h.Core().IsFileChannelID(ch) {
		return "", ""
	}
	if rec = v.h.Core().ListRecordOf(ch, thread, ""); rec == "" {
		return "", ""
	}
	return core.FileOfChannel(ch), rec
}

// paneFileJump is the persistent detail panel's click on an item of a
// canvas's or a list's file channel: Slack's jump to the item's message,
// the canvas in Primary at a mention's section or with a comment's thread
// beside it (core CanvasJump), the list at the record, here the view's
// own pane. A thread_v2 item opens its comment thread instead
// (toQuipCommentThread), in the pane as any thread does (JumpPane), unless
// it is a list record's. ok is false for an item of any other
// conversation.
func (v *Activity) paneFileJump() bool {
	if !v.selOK {
		return false
	}
	if _, rec := v.selListRecord(); v.sel.Type == "thread_v2" && rec == "" {
		return false
	}
	if _, kind, pending := v.fileChannel(&v.sel); kind == "" && !pending {
		return false
	}
	fo, ok := v.h.(FileOpener)
	if !ok {
		return false
	}
	v.paneFileKey = v.selKey
	fo.OpenFileJump(v.sel.Channel, v.selDesc.thread, v.selJumpTS(), FileInPane)
	return true
}

// paneFileOf is the canvas or list the pane shows for the chosen item
// once its jump resolved (paneFileJump), "" for none; wait is true while
// the jump resolves (the pane keeps what it showed, as Slack's Primary
// does until openDoc runs: Activity.pane).
func (v *Activity) paneFileOf() (fileID string, wait bool) {
	if !v.selOK || v.paneFileKey != v.selKey {
		return "", false
	}
	fo, ok := v.h.(FileOpener)
	if !ok {
		return "", false
	}
	fileID, done := fo.PaneFile()
	return fileID, !done
}
