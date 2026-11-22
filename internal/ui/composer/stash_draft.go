package composer

import (
	"errors"
	"sync"

	"github.com/some-du6e/clack/internal/core"
)

// Picking a draft from the sidebar's "Jump to draft…" menu, as Slack does
// under draft_management_in_composer (boot-render PZNh: the menu item calls
// selectStashedDraft, boot-shared pXlZ, unless it's the channel's attached
// draft, then opens the channel):
//
//   - The channel composer's slot holds one draft at a time (the attached
//     one, Slack's attached client draft id). Picking another one swaps
//     it in: what the slot held stays a draft of its own (stashed): its
//     unsaved edits are flushed to Slack first (select-stashed-draft-flush-
//     outgoing), and a slot that was never synced is saved
//     (…-stash-outgoing); an empty slot is just dropped.
//   - The picked draft then is the slot's: the composer shows its text and
//     files and later edits update it, not the one it replaced.
//
// Clack's slot is the channel key's local doc, its attachments and its
// server draft id (drafts_sync.go). Saves for a key run in order, so the
// flush runs under the old id and the swap after it. A composer that was
// closed with a save still pending must not write over the new draft: its
// attach generation is behind (attachGen) and its saves are dropped.

// attachGens counts each key's swaps (draftKey → uint64).
var attachGens sync.Map

func attachGenOf(key string) uint64 {
	if v, ok := attachGens.Load(key); ok {
		return v.(uint64)
	}
	return 0
}

// AttachedDraftID is the server draft ch's composer slot is attached to
// ("" none yet: the composer restores the newest when it opens).
func AttachedDraftID(ch string) string { return attachedDraftID(draftKey(ch, "")) }

func attachedDraftID(key string) string {
	if v, ok := serverDraftIDs.Load(key); ok {
		return v.(string)
	}
	return ""
}

// SelectStashedDraft puts d in the composer slot of ch (ts "") or of the
// thread ts in it (live: that slot's composer, else the one open for it).
// It reports false when d is already there.
//
// What the slot held stays a draft of its own, so it must be on Slack
// first: Slack's drafts store keeps it locally and syncs it later, and
// nothing typed is ever dropped. When the slot holds text or files Slack
// may not have (a save failed or is still out, it changed since its last
// save, or it never synced), the pick saves that first and takes the slot
// once Slack has it; if Slack refuses, the slot keeps its text and a toast
// says the draft couldn't be saved.
func SelectStashedDraft(h Host, live *Composer, ch, ts string, d core.Draft) bool {
	key := draftKey(ch, ts)
	if attachedDraftID(key) == d.ID {
		return false
	}
	if live == nil {
		live = liveComposer(key)
	}
	if live != nil && (live.edit != nil || live.closed) {
		live = nil
	}
	switch {
	case live != nil && live.unsaved():
		live.saveThenPick(d)
		return true
	case live == nil && slotUnsaved(h, ch, ts):
		saveSlotThenPick(h, ch, ts, d)
		return true
	}
	takeSlot(h, live, ch, ts, d)
	return true
}

// unsaved reports text or files in the composer that Slack may not have.
func (c *Composer) unsaved() bool {
	if c.ed.Empty() && c.att.Len() == 0 && c.att.gifCount() == 0 {
		return false
	}
	saves, dirty := draftKeyBusy(c.key())
	return saves > 0 || dirty || c.ed.Version() != c.dsync.synced || c.attVer != c.dsync.syncedAtt ||
		attachedDraftID(c.key()) == ""
}

// saveThenPick saves what the composer holds, then picks d once Slack has
// it (anything typed meanwhile is saved too, by picking again). Refused,
// the pick is dropped and the text stays. A pick made while one waits
// replaces it.
func (c *Composer) saveThenPick(d core.Draft) {
	waiting := c.dsync.pickWant != nil
	c.dsync.pickWant = &d
	if waiting {
		return
	}
	if c.dsync.timer != nil {
		c.dsync.timer.Stop()
	}
	c.queueSave(func(err error) {
		want := c.dsync.pickWant
		c.dsync.pickWant = nil
		switch {
		case want == nil || c.closed:
		case errors.Is(err, errDraftUploading):
			c.h.Toast(draftsHangTight())
		case err != nil:
			c.h.Toast("Couldn't save your draft: " + err.Error())
		default:
			SelectStashedDraft(c.h, c, c.ch, c.ts, *want)
		}
	})
}

// slotUnsaved reports text or files in a slot with no composer open that
// Slack may not have: a save failed or is still out, or it never synced.
func slotUnsaved(h Host, ch, ts string) bool {
	key := draftKey(ch, ts)
	doc := loadDraft(ch, ts)
	if (doc == nil || doc.Empty()) && attachmentsFor(key, h).Len() == 0 {
		return false
	}
	saves, dirty := draftKeyBusy(key)
	return saves > 0 || dirty || attachedDraftID(key) == ""
}

// saveSlotThenPick saves a slot with no composer open, then picks d once
// Slack has it.
func saveSlotThenPick(h Host, ch, ts string, d core.Draft) {
	key := draftKey(ch, ts)
	args := draftSyncArgs{DraftID: ch, ChannelID: ch, ThreadTs: ts}
	if doc := loadDraft(ch, ts); doc != nil {
		args.Blocks, args.Text = doc.RichTextBlocks(), doc.Mrkdwn()
		if ops, ok := doc.Delta()["ops"].([]any); ok {
			args.Ops = ops
		}
	}
	att := attachmentsFor(key, h)
	args.FileIDs = att.draftFileIDs()
	api := coreAPI{h.Core()}
	if len(args.Blocks) == 0 && len(args.FileIDs) == 0 {
		laterFor(api.a, h, func() { h.Toast(draftsHangTight()) }) // only files still uploading
		return
	}
	queueDraftOp(key, draftOp{api: api, save: &args, done: func(err error) {
		laterFor(api.a, h, func() {
			if err != nil {
				h.Toast("Couldn't save your draft: " + err.Error())
				return
			}
			SelectStashedDraft(h, nil, ch, ts, d)
		})
	}})
}

// takeSlot swaps d into the slot: what the slot held is on Slack already
// (or is nothing).
func takeSlot(h Host, live *Composer, ch, ts string, d core.Draft) {
	key := draftKey(ch, ts)
	api := coreAPI{h.Core()}
	if live != nil {
		// an emptied slot's draft goes (a synced one: nothing to send)
		if live.dsync.timer != nil {
			live.dsync.timer.Stop()
		}
		live.syncDraftNow()
	}
	id := d.ID
	queueDraftOp(key, draftOp{api: api, attach: &id})
	gen := attachGenOf(key) + 1
	attachGens.Store(key, gen)

	// the slot's content is d's
	doc := DocFromRichtext(d.Doc.Blocks)
	if (doc == nil || doc.Len() == 0) && d.Text != "" {
		doc = NewDoc()
		doc.Insert(0, d.Text, Attr{})
	}
	drafts.Lock()
	if drafts.m == nil {
		drafts.m = map[string]*Doc{}
	}
	if doc != nil && doc.Len() > 0 {
		drafts.m[key] = doc.Clone()
	} else {
		delete(drafts.m, key)
	}
	drafts.Unlock()
	att := attachmentsFor(key, h)
	att.replaceFiles(d.FileInfo)
	if live == nil {
		return
	}
	live.attachGen = gen
	if doc == nil {
		doc = NewDoc()
	}
	live.ed.SetDoc(doc)
	n := live.ed.Doc().Len()
	live.ed.SetSelection(n, n) // the caret at the draft's end
	live.lastVer = live.ed.Version()
	live.dsync.synced, live.dsync.syncedAtt = live.lastVer, live.attVer
	if live.ts != "" {
		live.broadcast = d.Broadcast // the draft's own "Also send to #channel"
	}
	live.touched = true
	live.h.Invalidate()
}

// replaceFiles makes the attachments a draft's files only (the stashed
// draft keeps its own on Slack). It doesn't notify: no save follows.
func (a *attachments) replaceFiles(fs []core.DraftFile) {
	a.mu.Lock()
	a.chips = nil
	a.mu.Unlock()
	a.restoreFiles(fs)
	a.h.Invalidate()
	pendingChanged()
}
