package composer

import (
	"sync/atomic"
	"time"

	"github.com/some-du6e/clack/internal/taut"
)

// The local draft plugins read as you type. Slack's MessageInput keeps
// state.drafts.unifiedDrafts[draftId] current with the Quill ops long
// before the draft syncs to the server: fastLaneDraftUpdate calls
// setLocalDraftAction debounced 150 ms, leading and trailing (modern.vendor,
// MessageInput). HaikuWarning's banner reads those ops for the composer's
// BaseContextBarV2. Clack does the same while the plugin runtime runs; with
// no runtime this is one atomic load per edit.

// liveDraftWait is fastLaneDraftUpdate's debounce.
const liveDraftWait = 150 * time.Millisecond

// liveDraft is a composer's debounce state (UI goroutine, except gen).
type liveDraft struct {
	gen   atomic.Uint64 // the newest edit's timer; older timers do nothing
	armed bool          // a leading call ran and its wait isn't over
	dirty bool          // edits since that call: the trailing call sends them
}

// liveDraftSoon reports the edit: at once when the composer was idle
// (leading), else 150 ms after the last edit (trailing).
func (c *Composer) liveDraftSoon() {
	t := currentTaut()
	if t == nil || c.noDraft() || !t.Started() {
		return
	}
	if !c.live.armed {
		c.live.armed, c.live.dirty = true, false
		c.pushLiveDraft(t)
	} else {
		c.live.dirty = true
	}
	g := c.live.gen.Add(1)
	time.AfterFunc(liveDraftWait, func() {
		if c.live.gen.Load() != g {
			return // a newer edit restarted the wait
		}
		c.later(func() {
			if c.live.gen.Load() != g {
				return
			}
			c.live.armed = false
			if c.live.dirty {
				c.live.dirty = false
				c.pushLiveDraft(currentTaut())
			}
		})
	})
}

// pushLiveDraft hands the composer's content to the mirror.
func (c *Composer) pushLiveDraft(t *taut.Taut) {
	if t == nil {
		return
	}
	d := c.ed.Doc()
	ops, _ := d.Delta()["ops"].([]any)
	_, caret := c.ed.Selection()
	t.SetComposerDraft(taut.ComposerDraft{DraftID: c.draftID(), Channel: c.ch, ThreadTs: c.ts,
		Broadcast: c.broadcast && c.ts != "", Ops: ops, FileIDs: c.att.draftFileIDs(), Cursor: caret})
}

// clearLiveDraft drops the local draft once the message is sent (Slack's
// clearDrafts from InputContainer's sendMessage).
func (c *Composer) clearLiveDraft() {
	c.live.gen.Add(1)
	c.live.armed, c.live.dirty = false, false
	if t := currentTaut(); t != nil && !c.noDraft() {
		t.ClearComposerDraft(c.draftID())
	}
}
