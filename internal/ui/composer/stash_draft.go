package composer

import (
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
func AttachedDraftID(ch string) string {
	if v, ok := serverDraftIDs.Load(draftKey(ch, "")); ok {
		return v.(string)
	}
	return ""
}

// SelectStashedDraft puts d in ch's composer slot (live: that channel's
// composer when one exists). It reports false when d is already there.
func SelectStashedDraft(h Host, live *Composer, ch string, d core.Draft) bool {
	key := draftKey(ch, "")
	if AttachedDraftID(ch) == d.ID {
		return false
	}
	api := coreAPI{h.Core()}
	switch {
	case live != nil && live.edit == nil:
		// flush what the slot shows under its own id (or drop it when empty)
		if live.dsync.timer != nil {
			live.dsync.timer.Stop()
		}
		live.syncDraftNow()
	default:
		if doc := loadDraft(ch, ""); doc != nil && doc.Len() > 0 && AttachedDraftID(ch) == "" {
			// never synced: save it as a draft of its own before the swap
			args := draftSyncArgs{DraftID: ch, ChannelID: ch, Blocks: doc.RichTextBlocks(), Text: doc.Mrkdwn()}
			if ops, ok := doc.Delta()["ops"].([]any); ok {
				args.Ops = ops
			}
			queueDraftOp(key, draftOp{api: api, save: &args})
		}
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
	if live != nil && live.edit == nil {
		live.attachGen = gen
		if doc == nil {
			doc = NewDoc()
		}
		live.ed.SetDoc(doc)
		live.lastVer = live.ed.Version()
		live.dsync.synced, live.dsync.syncedAtt = live.lastVer, live.attVer
		live.touched = true
		live.h.Invalidate()
	}
	return true
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
