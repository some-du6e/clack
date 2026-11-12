package composer

import (
	"context"
	"os"
	"path/filepath"
	"time"

	"github.com/some-du6e/clack/internal/core"
)

// Files dropped on a pane from the OS (Gio patch 11's FileDropEvent; the
// ui root's drop_overlay.go picks the pane). Slack's FileDragDrop.onDrop
// (boot-shared oCSS): a plain drop is uploadDraftFiles, the files join the
// pane's draft; with Shift held it is uploadFiles with shareData, the
// files go to the conversation at once and the draft stays as it is.

// AttachPaths attaches files from disk to the draft, as the + menu's
// "Upload from your computer" does with what it picked. The files are
// read off the UI goroutine.
func (c *Composer) AttachPaths(paths []string) {
	if len(paths) == 0 {
		return
	}
	go c.att.addPaths(paths)
	c.Focus()
}

// SharePaths uploads files from disk straight to the conversation (Shift
// held on the drop: "Hold Shift to share immediately"), with no message
// text. Files over Slack's limits are refused with the composer's toasts.
func (c *Composer) SharePaths(paths []string) {
	if len(paths) == 0 || c.api.a == nil {
		return
	}
	ch, ts := c.ch, c.ts
	go func() {
		files := dropReadFiles(paths, c.att.toast)
		if len(files) == 0 {
			return
		}
		ctx, cancel := context.WithTimeout(context.Background(), 10*time.Minute)
		defer cancel()
		if _, err := c.api.upload(ctx, core.UploadOpts{Channel: ch, ThreadTS: ts, Files: files}); err != nil {
			c.att.toast("Couldn't upload: " + err.Error())
		}
	}()
}

// dropReadFiles reads dropped files into memory with the attach limits
// (10 files, 1 GB each and together); toast says why a file is skipped.
func dropReadFiles(paths []string, toast func(string)) []core.UploadFile {
	var files []core.UploadFile
	var total int64
	for _, p := range paths {
		name := filepath.Base(p)
		st, err := os.Stat(p)
		if err != nil {
			toast("Couldn't read " + name + ": " + err.Error())
			continue
		}
		if st.IsDir() {
			toast(name + " is a folder. Only files can be uploaded.")
			continue
		}
		if err := attCheckAdd(len(files), total, st.Size()); err != nil {
			toast(attRejectText(name, err))
			continue
		}
		data, err := os.ReadFile(p)
		if err != nil {
			toast("Couldn't read " + name + ": " + err.Error())
			continue
		}
		name = attCleanName(name)
		files = append(files, core.UploadFile{Name: name, Title: name, Data: data})
		total += int64(len(data))
	}
	return files
}
