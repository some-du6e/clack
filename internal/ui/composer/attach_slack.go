package composer

import (
	"image"
	"mime"
	"path/filepath"
	"strconv"

	"gioui.org/layout"

	"github.com/some-du6e/yoloslackgoclient/internal/ui/slackc"
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

// attMime is a file's type from its extension ("" when unknown).
func attMime(name string) string { return mime.TypeByExtension(filepath.Ext(name)) }

// file is Slack's file object for the pending upload.
func (it attItem) file() slackc.Props {
	v := it.v
	m := attMime(v.name)
	return slackc.Props{"name": v.name, "title": v.title, "size": len(v.c.data), "mimetype": m, "type": m}
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
			"title":         it.v.title,
			"file":          it.file(),
		},
		"isUploading": it.v.sending || it.v.preparing,
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
	return slackc.Props{"$key": it.pendingID() + "/title", "file": it.file(), "title": it.v.title}
}

// layoutTitle: Clack has no rename dialog, so the title draws nothing.
func (it attItem) layoutTitle(layout.Context, slackc.Props) layout.Dimensions {
	return layout.Dimensions{}
}
