package composer

import (
	"image"
	"strconv"

	"gioui.org/layout"

	"github.com/some-du6e/clack/internal/slack"
	"github.com/some-du6e/clack/internal/ui/slackc"
)

// attach_slack.go: the attachments' Slack boundaries.
//
//	MultiFileUploadItem      one pending file (the chip)
//	FileFieldset             Slack's rename dialog body, at the chip's
//	└─ EditFileDialogTitle   edit spot while hovered; both empty in Clack
//
// AnonymiseFileNames patches all three. The name it gives a file reaches
// the upload through uploadName (plugins.go), which reads it from a real
// File in the plugin runtime; the props carry that name. Its toggle
// renames the file's title (updatePendingFile, attach_pending.go): the
// chip shows the title and the file is shared under it, while file.name
// stays the name the bytes were uploaded under.

// attItem is one chip at a boundary: what the props and natives need.
type attItem struct {
	a  *attachments
	v  attView
	sz image.Point
}

// attFileType is a chip's File type as Slack's client holds it, which its
// pending file's mimetype is: a clip's own (the recorder's audio/mp4), else
// the type Chromium gives a file of that name (slack.FileType: its lists
// and this machine's OS). Go's mime.TypeByExtension isn't it: it gave a .js
// "text/javascript; charset=utf-8".
func attFileType(name, clipMime string) string {
	if clipMime != "" {
		return clipMime
	}
	return slack.FileType(name)
}

// file is Slack's file object for the pending upload.
func (it attItem) file() slackc.Props {
	v := it.v
	// a draft's file the store doesn't have yet is Slack's unknown object,
	// one that never came its non-existent one (draft_meta.go): a plugin's
	// MessagePane gets no name made of the id
	switch {
	case v.loading:
		return slackc.Props{"id": it.fileID(), "isUnknown": true}
	case v.gone:
		return slackc.Props{"id": it.fileID(), "isNonExistent": true}
	}
	return slackc.Props{"name": v.name, "title": v.title, "size": v.size, "mimetype": v.typ, "type": v.typ}
}

// fileID is the chip's Slack file id ("" before its upload gave it one).
func (it attItem) fileID() string {
	it.a.mu.Lock()
	defer it.a.mu.Unlock()
	if p := it.v.c.prepared; p != nil {
		return p.ID
	}
	return ""
}

func (it attItem) pendingID() string { return attPendingID(it.v.c.id) }

// attPendingID is a chip's pendingFileId.
func attPendingID(id int) string { return "clack_pending_" + strconv.Itoa(id) }

// MultiFileUploadItem's props: Slack's pendingFile {pendingFileId, title,
// file}.
func (it attItem) props() slackc.Props {
	return slackc.Props{
		"$key": it.pendingID(),
		"pendingFile": slackc.Props{
			"pendingFileId": it.pendingID(),
			"title":         it.shownTitle(),
			"file":          it.file(),
		},
		"isUploading": it.v.preparing,
	}
}

func (it attItem) layout(gtx layout.Context, _ slackc.Props) layout.Dimensions {
	it.a.layoutChip(gtx, it.v, it.sz)
	return layout.Dimensions{Size: it.sz}
}

// FileFieldset's props: the file being edited, and the title field
// (EditFileDialogTitle) as its child.
func (it attItem) fieldsetProps() slackc.Props {
	return slackc.Props{
		"$key":     it.pendingID() + "/fieldset",
		"file":     it.file(),
		"children": []any{slackc.Child("EditFileDialogTitle", it.titleProps(), it.layoutTitle)},
	}
}

func (it attItem) layoutFieldset(gtx layout.Context, p slackc.Props) layout.Dimensions {
	if e, patched := kid(p, "EditFileDialogTitle"); patched {
		return slackc.LayoutElement(gtx, e)
	}
	return slackc.Component(gtx, "EditFileDialogTitle", it, attItem.titleProps, attItem.layoutTitle)
}

func (it attItem) titleProps() slackc.Props {
	return slackc.Props{"$key": it.pendingID() + "/title", "file": it.file(), "title": it.shownTitle()}
}

// shownTitle is the chip's title, none while its file is unknown.
func (it attItem) shownTitle() string {
	if it.v.loading || it.v.gone {
		return ""
	}
	return it.v.title
}

// layoutTitle: Clack has no rename dialog, so the title draws nothing.
func (it attItem) layoutTitle(layout.Context, slackc.Props) layout.Dimensions {
	return layout.Dimensions{}
}
