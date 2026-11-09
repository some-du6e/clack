package composer

import (
	"context"
	"sync"
	"time"

	"github.com/some-du6e/yoloslackgoclient/internal/core"
)

// Files in drafts, like Slack's composer: a draft keeps its attached files.
//
//   - Attaching uploads the bytes at once (attachments.prepare, Slack's
//     eager upload: files.getUploadURL, the multipart POST,
//     files.completeUpload; no share), which gives the
//     file its id. The server draft then carries it: drafts.create /
//     drafts.update file_ids=["F…"] (createDraftApi sends
//     file_ids: JSON.stringify(fileIds)), next to the blocks; a draft with
//     files and no text sends blocks=[]. drafts.list answers file_ids and
//     the file objects (files), so Drafts & sent and other devices see them.
//   - Locally the chips (with their bytes, memory only) belong to the
//     channel or thread, not to one composer: switching conversations and
//     back finds them again (attStore).
//   - A composer that opens empty and untouched restores the server draft
//     for its conversation (core.DraftFor): the text, and the files as
//     chips that are already uploaded (no bytes; Slack's thumb_360 for the
//     thumbnail). That covers a restart, another device and Drafts & sent.
//   - Sending shares the files (files.share with the blocks) and deletes
//     the draft; removing the last file from an empty composer deletes it
//     too. drafts.delete keeps the files (skip_file_deletion=true), as
//     Slack's clearDrafts does on send and on an emptied composer.

// attStore keeps each conversation's attachments across composer
// instances. Memory only.
var attStore struct {
	sync.Mutex
	m map[string]*attachments // draftKey
}

// attachmentsFor is the attachments of key's conversation, created on
// first use. A different session (core) starts afresh.
func attachmentsFor(key string, h Host) *attachments {
	attStore.Lock()
	defer attStore.Unlock()
	if a := attStore.m[key]; a != nil && a.core == h.Core() {
		return a
	}
	a := newAttachments(h)
	if attStore.m == nil {
		attStore.m = map[string]*attachments{}
	}
	attStore.m[key] = a
	return a
}

// storedFiles reports whether key's conversation has files attached
// (HasDraft).
func storedFiles(key string) bool {
	attStore.Lock()
	a := attStore.m[key]
	attStore.Unlock()
	return a != nil && a.Len() > 0
}

// setOnChange makes f run (on any goroutine) after the file list or a
// file's id changes; the composer showing the attachments owns it.
func (a *attachments) setOnChange(f func()) {
	a.mu.Lock()
	a.onChange = f
	a.mu.Unlock()
}

// notify runs onChange. Call without a.mu held.
func (a *attachments) notify() {
	a.mu.Lock()
	f := a.onChange
	a.mu.Unlock()
	if f != nil {
		f()
	}
}

// draftFileIDs are the ids of the files a draft carries now: uploaded
// (prepared) and not being sent. Files still uploading join when their id
// arrives.
func (a *attachments) draftFileIDs() []string {
	a.mu.Lock()
	defer a.mu.Unlock()
	var ids []string
	for _, c := range a.chips {
		if c.prepared != nil {
			ids = append(ids, c.prepared.ID)
		}
	}
	return ids
}

// restoreFiles adds a server draft's files as chips that are already
// uploaded. Files already attached are skipped. It doesn't notify: the
// draft it came from already has them.
func (a *attachments) restoreFiles(fs []core.DraftFile) {
	a.mu.Lock()
	have := map[string]bool{}
	for _, c := range a.chips {
		if c.prepared != nil {
			have[c.prepared.ID] = true
		}
	}
	added := false
	for _, f := range fs {
		if f.ID == "" || have[f.ID] || len(a.chips) >= attMaxFiles {
			continue
		}
		name := attCleanName(firstNonEmpty(f.Name, f.Title, f.ID))
		title := firstNonEmpty(f.Title, name)
		c := &attChip{id: int(attNextID.Add(1)), name: name, title: title, kind: attKindOf(name, nil), thumbURL: f.Thumb,
			prepared: &core.PreparedUpload{ID: f.ID, Name: name, Title: title, Size: f.Size}}
		if f.Thumb != "" {
			c.kind.Image = true // Slack made a thumbnail: draw it like the image it is
		}
		a.chips = append(a.chips, c)
		have[f.ID] = true
		added = true
	}
	a.mu.Unlock()
	if added {
		a.h.Invalidate()
		pendingChanged()
	}
}

// size is the file's size: its bytes, or what Slack said for a file
// restored from a draft.
func (c *attChip) size() int64 {
	if c.data == nil && c.prepared != nil {
		return c.prepared.Size
	}
	return int64(len(c.data))
}

// thumbBytes is what a thumbnail is decoded from: the file, or Slack's
// thumbnail of a restored file (core.Fetch: memory only).
func (a *attachments) thumbBytes(c *attChip) []byte {
	if len(c.data) > 0 || c.thumbURL == "" || a.core == nil {
		return c.data
	}
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	b, _, err := a.core.Fetch(ctx, c.thumbURL)
	if err != nil {
		return nil
	}
	return b
}

// ---- composer side ----

// attChanged runs when the composer's files change: save the draft
// (locally and to Slack).
func (c *Composer) attChanged() {
	c.later(func() {
		c.attVer++
		c.touched = true
		c.syncDraftSoon()
		c.h.Invalidate()
	})
}

// maybeRestoreServerDraft fills an empty, untouched composer from the
// server draft for its conversation once drafts are known (UI goroutine,
// every frame; a counter compare when nothing changed).
func (c *Composer) maybeRestoreServerDraft() {
	if c.edit != nil || c.touched || c.api.a == nil {
		return
	}
	g := c.api.a.DraftsGen()
	if g == c.srvGen {
		return
	}
	c.srvGen = g
	if !c.ed.Empty() || c.att.Len() > 0 {
		c.touched = true // local content wins
		return
	}
	d, ok := c.api.a.DraftFor(c.ch, c.ts)
	if !ok {
		return
	}
	c.applyServerDraft(d)
}

// applyServerDraft puts a server draft's text and files in the composer.
func (c *Composer) applyServerDraft(d core.Draft) {
	doc := DocFromRichtext(d.Doc.Blocks)
	if (doc == nil || doc.Len() == 0) && d.Text != "" {
		doc = NewDoc()
		doc.Insert(0, d.Text, Attr{})
	}
	if doc != nil && doc.Len() > 0 {
		c.ed.SetDoc(doc)
	}
	c.lastVer = c.ed.Version()
	c.dsync.synced = c.lastVer
	if c.ts != "" {
		c.broadcast = d.Broadcast
	}
	if c.serverDraftID() == "" {
		c.setServerDraftID(d.ID)
	}
	c.att.restoreFiles(d.FileInfo)
	c.dsync.syncedAtt = c.attVer
	c.touched = true
	c.saveDraft()
	c.h.Invalidate()
}

func firstNonEmpty(s ...string) string {
	for _, v := range s {
		if v != "" {
			return v
		}
	}
	return ""
}
