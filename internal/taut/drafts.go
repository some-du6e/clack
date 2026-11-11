package taut

import (
	"encoding/json"
	"sort"
	"strconv"
	"time"

	"github.com/some-du6e/clack/internal/core"
)

// The drafts slice, as Slack's store keeps it (modern.vendor nFzq, the
// setLocalDraft reducer, and r3Gk/7awP, the draft helpers):
//
//   - unifiedDrafts is keyed by client_draft_id. A draft attached to a
//     conversation is keyed by encodeDraftKey({channelId, threadTs}):
//     "C…" or "C…-<threadTs>", the composer's draftId. A draft written
//     on the Drafts page (is_from_composer) keeps its client_msg_id
//     (getDraftClientIdFromServerDraft). The server id is the entry's id.
//   - The composer keeps its entry current as you type: MessageInput's
//     fastLaneDraftUpdate calls setLocalDraftAction (debounced 150 ms,
//     leading and trailing) with the Quill ops, long before the draft
//     syncs to the server. Plugins read those ops (HaikuWarning's banner:
//     unifiedDrafts[draftId].ops for the composer's BaseContextBarV2).
//   - channelIds are the conversations with an unthreaded draft;
//     withheldDraftIds the composers' drafts not stored before.
//
// Clack's composers report their live drafts with SetComposerDraft; the
// server drafts come from core's draft events.

// localDraft is a composer's live draft (setLocalDraft's payload).
type localDraft struct {
	ch, ts    string
	broadcast bool
	ops       json.RawMessage
	files     []string
	cursor    int
	created   int64 // ms
	updated   int64 // ms
}

// ComposerDraft is what a composer has in it now, for SetComposerDraft.
type ComposerDraft struct {
	// DraftID is Slack's client_draft_id for the composer: the channel id,
	// or "<channel>-<threadTs>" in a thread (encodeDraftKey).
	DraftID   string
	Channel   string
	ThreadTs  string
	Broadcast bool
	// Ops are the composer's Quill delta ops (Slack's local draft keeps
	// them as typed).
	Ops     []any
	FileIDs []string
	// Cursor is the caret's index in the ops' text.
	Cursor int
}

// draftKey is Slack's encodeDraftKey: the channel id and thread ts joined
// with "-".
func draftKey(ch, ts string) string {
	if ts == "" {
		return ch
	}
	if ch == "" {
		return ts
	}
	return ch + "-" + ts
}

// clientDraftID is getDraftClientIdFromServerDraft: a Drafts-page draft
// keeps its client_msg_id, an attached one is keyed by its destination
// (getAttachedDraftDestination: a scheduled thread reply counts as
// attached even from the Drafts page).
func clientDraftID(d *core.Draft) string {
	attached := !d.FromComposer || d.ThreadTS != "" && d.DateScheduled > 0
	if !attached || d.Channel == "" {
		if d.ClientMsgID != "" {
			return d.ClientMsgID
		}
		return d.ID
	}
	return draftKey(d.Channel, d.ThreadTS)
}

// SetComposerDraft keeps a composer's local draft (state.drafts
// .unifiedDrafts[DraftID]) current, as Slack's composer does while you
// type. Cheap and non-blocking; it does nothing until the plugin runtime
// runs. Call it at most every 150 ms (Slack's debounce).
func (t *Taut) SetComposerDraft(d ComposerDraft) {
	if t == nil || d.DraftID == "" || !t.Started() {
		return
	}
	ops, err := json.Marshal(d.Ops)
	if err != nil || len(d.Ops) == 0 {
		ops = json.RawMessage(`[{"insert":"\n"}]`) // an empty Quill document
	}
	now := time.Now().UnixMilli()
	m := t.mirror
	m.mu.Lock()
	prev, had := m.local[d.DraftID]
	ld := localDraft{ch: d.Channel, ts: d.ThreadTs, broadcast: d.Broadcast, ops: ops, files: append([]string(nil), d.FileIDs...),
		cursor: d.Cursor, created: now, updated: now}
	if had {
		ld.created = prev.created
	}
	m.local[d.DraftID] = ld
	m.mu.Unlock()
	m.invalidate("drafts")
}

// ClearComposerDraft drops a composer's local draft (Slack's clearDrafts
// when the composer sends).
func (t *Taut) ClearComposerDraft(draftID string) {
	if t == nil || draftID == "" || !t.Started() {
		return
	}
	m := t.mirror
	m.mu.Lock()
	_, had := m.local[draftID]
	delete(m.local, draftID)
	m.mu.Unlock()
	if had {
		m.invalidate("drafts")
	}
}

func (m *mirror) noteDraft(ev core.Event) {
	m.mu.Lock()
	if ev.Name == "delete" || ev.Name == "send" || ev.Draft == nil {
		delete(m.drafts, ev.ID)
	} else {
		m.drafts[ev.Draft.ID] = *ev.Draft
	}
	m.mu.Unlock()
	m.invalidate("drafts")
}

// SetDrafts gives the mirror the drafts the UI loaded (core.Drafts), so
// plugins see `drafts.unifiedDrafts`.
func (t *Taut) SetDrafts(ds []core.Draft) {
	t.mirror.mu.Lock()
	t.mirror.drafts = make(map[string]core.Draft, len(ds))
	for i := range ds {
		t.mirror.drafts[ds[i].ID] = ds[i]
	}
	t.mirror.mu.Unlock()
	t.mirror.invalidate("drafts")
}

// draftsSlice is the drafts store: the server drafts keyed by
// client_draft_id with the composers' live drafts over them.
func (m *mirror) draftsSlice() map[string]any {
	m.mu.Lock()
	defer m.mu.Unlock()
	unified := make(map[string]map[string]any, len(m.drafts)+len(m.local))
	for _, d := range m.drafts {
		unified[clientDraftID(&d)] = serverDraftEntry(&d)
	}
	withheld := []string{}
	for key, ld := range m.local {
		e, stored := unified[key]
		if !stored {
			e = map[string]any{"client_draft_id": key, "is_from_composer": false, "date_created": ld.created,
				"file_ids": []string{}, "temp_attached_draft_unique_id": "clack-" + strconv.FormatInt(ld.created, 36) + "-" + key}
			withheld = append(withheld, key)
			unified[key] = e
		}
		dest := map[string]any{"channel_id": ld.ch}
		if ld.ts != "" {
			dest["thread_ts"] = ld.ts
		}
		if ld.broadcast {
			dest["broadcast"] = true
		}
		e["destinations"] = []any{dest}
		e["ops"] = ld.ops
		e["cursor_index"] = ld.cursor
		e["last_updated"] = ld.updated
		if len(ld.files) > 0 {
			e["file_ids"] = ld.files
		}
	}
	chans := []string{}
	seen := map[string]bool{}
	for key, e := range unified {
		dest, _ := e["destinations"].([]any)
		if len(dest) == 0 {
			continue
		}
		d0, _ := dest[0].(map[string]any)
		ch, _ := d0["channel_id"].(string)
		if _, thread := d0["thread_ts"]; ch == "" || thread || seen[ch] || key != ch {
			continue
		}
		seen[ch] = true
		chans = append(chans, ch)
	}
	sort.Strings(chans)
	sort.Strings(withheld)
	return map[string]any{"unifiedDrafts": unified, "channelIds": chans, "withheldDraftIds": withheld,
		"isEditingDraftPage": false, "selectedDraftIds": []string{}}
}

// serverDraftEntry is a server draft as Slack stores it.
func serverDraftEntry(d *core.Draft) map[string]any {
	dest := []any{}
	if d.Channel != "" {
		e := map[string]any{"channel_id": d.Channel}
		if d.ThreadTS != "" {
			e["thread_ts"] = d.ThreadTS
		}
		if d.Broadcast {
			e["broadcast"] = true
		}
		dest = append(dest, e)
	} else if len(d.Users) > 0 {
		dest = append(dest, map[string]any{"user_ids": d.Users})
	}
	var blocks any = []any{}
	if len(d.Blocks) > 0 {
		blocks = d.Blocks
	}
	fileIDs := d.FileIDs
	if fileIDs == nil {
		fileIDs = []string{}
	}
	return map[string]any{
		"id": d.ID, "client_draft_id": clientDraftID(d), "client_msg_id": d.ClientMsgID, "destinations": dest, "blocks": blocks,
		"ops": []map[string]any{{"insert": d.Text + "\n"}}, "file_ids": fileIDs, "date_created": d.DateCreated,
		"date_scheduled": d.DateScheduled, "last_updated_ts": d.LastUpdated, "is_from_composer": d.FromComposer,
	}
}
