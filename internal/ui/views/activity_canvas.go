package views

// Canvas mentions in Activity (Karim, α.102: "kinda messed up on canvas
// pings"; a click "put me in a limbo esque channel where the canvas
// contents are messages??"). Slack's item is an @-mention whose message
// (subtype document_mention, document_mention {file_id, section_id,
// mentioning_user_ids}) sits in the canvas's hidden file channel (core
// canvas_channel.go). Build 2c0d02ca130f:
//
//   - the row's destination (VeYL, every layout: ActivityRowContentDense
//     and -Detailed, ActivityRowContentSingleLine and -MultiLine) is the
//     file channel's InlineChannelEntity: the canvas icon and the canvas's
//     title from its title_blocks, emoji as images (chanent.Canvas), not
//     the channel's own ":shortcode:" name behind a private channel's lock;
//   - a jump to the message (wc0P jumpToMessage, isSpaceship) opens the
//     canvas at document_mention.section_id (lFpV openDocWithThreadTs →
//     zsuR openDoc, which focuses the section when the canvas is already
//     open). Clack opens it where the inbox opens an item (the secondary
//     column beside the inbox, or Primary with the persistent detail
//     panel), never the file channel's messages.

import (
	"github.com/some-du6e/clack/internal/core"
	"github.com/some-du6e/clack/internal/slack"
	"github.com/some-du6e/clack/internal/ui/chanent"
)

// CanvasOpener is optionally implemented by the Host: the canvas viewer
// on a canvas, focused on a section ("" for the top), in the secondary
// column or, with full, in the full view; and whether the secondary column
// shows that canvas now.
type CanvasOpener interface {
	OpenCanvasAt(fileID, sectionID string, full bool)
	CanvasShown(fileID string) bool
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
// one core knows as a canvas's file channel.
func (v *Activity) canvasChannel(it *core.ActivityItem) (string, bool) {
	if it == nil || it.Channel == "" {
		return "", false
	}
	if dm := actDocMention(it); dm != nil && core.FileOfChannel(it.Channel) == dm.FileID {
		return dm.FileID, true
	}
	return v.h.Core().CanvasOfChannel(it.Channel)
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

// selCanvas is the chosen item's canvas and the section a click opens it
// at (a canvas mention's document_mention.section_id).
func (v *Activity) selCanvas() (fileID, sectionID string, ok bool) {
	if !v.selOK {
		return "", "", false
	}
	fileID, ok = v.canvasChannel(&v.sel)
	if !ok {
		return "", "", false
	}
	if dm := actDocMention(&v.sel); dm != nil {
		sectionID = dm.SectionID
	}
	return fileID, sectionID, true
}

// openCanvas opens the chosen item's canvas instead of its conversation:
// in the secondary column beside the inbox, or (full) in Primary as the
// persistent detail panel's click does. ok false when the item isn't a
// canvas's or the Host can't open canvases.
func (v *Activity) openCanvas(full bool) bool {
	fid, sec, ok := v.selCanvas()
	if !ok {
		return false
	}
	co, ok := v.h.(CanvasOpener)
	if !ok {
		return false
	}
	co.OpenCanvasAt(fid, sec, full)
	v.selOut = true
	v.selSec = !full && co.CanvasShown(fid)
	return true
}
