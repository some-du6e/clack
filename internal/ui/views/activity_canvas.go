package views

// Canvas mentions in Activity (Karim, α.102: "kinda messed up on canvas
// pings"; a click "put me in a limbo esque channel where the canvas
// contents are messages??"). Slack's item is an @-mention whose message
// (subtype document_mention, document_mention {file_id, section_id,
// mentioning_user_ids}) sits in the canvas's hidden file channel (core
// canvas_channel.go), as do its comments' threads. Build 2c0d02ca130f:
//
//   - the row's destination (VeYL, every layout: ActivityRowContentDense
//     and -Detailed, ActivityRowContentSingleLine and -MultiLine) is the
//     file channel's InlineChannelEntity: the canvas icon and the canvas's
//     title from its title_blocks, emoji as images (chanent.Canvas), not
//     the channel's own ":shortcode:" name behind a private channel's lock.
//     That holds for every item of the file channel, mentions and
//     comments alike;
//   - the click (GMkG handleOnClickActivityItem): in the inbox a mention
//     opens its message (jpad toMessage) in the file channel's Channel
//     view, which has no canvas case, so Clack opens the canvas there
//     instead (openMention). A comment's thread_v2 item opens its comment
//     thread (toQuipCommentThread) and another reply opens its thread
//     (toThread), in the column as any thread does. With the persistent
//     detail panel any other item jumps to its message (wc0P
//     jumpToMessage), which for a canvas's file channel opens the canvas
//     in Primary at the mention's section, or with the comment's thread
//     beside it (isSpaceship → lFpV openDocWithThreadTs → zsuR openDoc):
//     the view's own pane, the inbox staying in the Sidebar
//     (paneCanvasJump).

import (
	"gioui.org/layout"

	"github.com/some-du6e/clack/internal/core"
	"github.com/some-du6e/clack/internal/slack"
	"github.com/some-du6e/clack/internal/ui/chanent"
)

// CanvasWhere is the container a jump into a canvas's file channel opens
// the canvas in.
type CanvasWhere int

const (
	// CanvasSide is the secondary column (beside the inbox: Slack's
	// Secondary).
	CanvasSide CanvasWhere = iota
	// CanvasFull is the full view, in place of the page: a link, Open in
	// home.
	CanvasFull
	// CanvasPane is the Activity persistent detail panel's own pane:
	// Slack's Primary, the inbox staying in the Sidebar.
	CanvasPane
)

// CanvasOpener is optionally implemented by the Host: Slack's
// jumpToMessage into a canvas's file channel (wc0P isSpaceship → lFpV
// openDocWithThreadTs → zsuR openDoc), which opens the canvas at a
// mention's section, with a comment's thread beside it (ui/canvas_open.go).
type CanvasOpener interface {
	// OpenCanvasJump opens the jump to message ts (or thread) of canvas
	// file channel ch, the canvas where says.
	OpenCanvasJump(ch, thread, ts string, where CanvasWhere)
	// CanvasShown reports whether the secondary column shows canvas
	// fileID.
	CanvasShown(fileID string) bool
	// CanvasPane draws canvas fileID in the persistent detail panel's
	// pane (after a CanvasPane jump).
	CanvasPane(fileID string) layout.Widget
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

// selMention is the chosen item's canvas when the item is a canvas
// mention: its own document_mention, in the canvas's file channel.
func (v *Activity) selMention() (fileID string, ok bool) {
	if !v.selOK {
		return "", false
	}
	dm := actDocMention(&v.sel)
	if dm == nil || core.FileOfChannel(v.sel.Channel) != dm.FileID {
		return "", false
	}
	return dm.FileID, true
}

// openMention opens the chosen canvas mention's canvas in the secondary
// column beside the inbox, at the mention's section. Slack's inbox opens
// the mention's message there (toMessage), in the file channel's Channel
// view, which has no canvas case: the one place Clack opens the canvas
// instead. ok is false for any other item, which opens as Slack opens it.
func (v *Activity) openMention() bool {
	fid, ok := v.selMention()
	if !ok {
		return false
	}
	co, ok := v.h.(CanvasOpener)
	if !ok {
		return false
	}
	co.OpenCanvasJump(v.sel.Channel, "", v.selJumpTS(), CanvasSide)
	v.selOut = true
	v.selSec = co.CanvasShown(fid)
	return true
}

// paneCanvasJump is the persistent detail panel's click on an item of a
// canvas's file channel: Slack's jump to the item's message, the canvas
// in Primary at a mention's section or with a comment's thread beside it
// (core CanvasJump), here the view's own pane. A thread_v2 item opens its
// comment thread instead (toQuipCommentThread), in the pane as any thread
// does (JumpPane). ok is false for an item of any other conversation.
func (v *Activity) paneCanvasJump() bool {
	if !v.selOK || v.sel.Type == "thread_v2" {
		return false
	}
	fid, ok, pending := v.canvasChannel(&v.sel)
	if !ok && !pending {
		return false
	}
	co, ok := v.h.(CanvasOpener)
	if !ok {
		return false
	}
	co.OpenCanvasJump(v.sel.Channel, v.selDesc.thread, v.selJumpTS(), CanvasPane)
	v.paneCanvas, v.paneCanvasKey = fid, v.selKey
	return true
}

// paneCanvasOf is the canvas the pane shows for the chosen item
// (paneCanvasJump), "" for none.
func (v *Activity) paneCanvasOf() string {
	if v.selOK && v.paneCanvasKey == v.selKey {
		return v.paneCanvas
	}
	return ""
}
