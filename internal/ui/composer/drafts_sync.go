package composer

import (
	"context"
	"sync"
	"time"

	"github.com/some-du6e/clack/internal/core"
	"github.com/some-du6e/clack/internal/taut"
)

// Server draft sync, like Slack's composer: a pause in typing saves the
// draft to Slack (syncDraftCreate the first time, syncDraftUpdates after),
// and an emptied or sent composer deletes it (deleteDraftApi). Each goes
// through taut.Dispatch so plugins can veto it (OwlMode keeps owl replies
// out of synced drafts). Edit composers never sync.
//
// Drafts live on Slack's servers, as in Slack; Clack still writes no
// message contents to disk.

// draftSyncDelay is the typing pause before a draft is saved.
var draftSyncDelay = 1500 * time.Millisecond

// draftSync is one composer's server-draft state (UI goroutine).
type draftSync struct {
	timer     *time.Timer
	synced    uint64 // editor version last saved
	syncedAtt uint64 // Composer.attVer last saved
}

// serverDraftIDs keeps each composer key's server draft id across composer
// instances (a channel switch drops and rebuilds composers).
var serverDraftIDs sync.Map // draftKey → string

func (c *Composer) serverDraftID() string {
	if v, ok := serverDraftIDs.Load(c.key()); ok {
		return v.(string)
	}
	return ""
}

func (c *Composer) setServerDraftID(id string) {
	if id == "" {
		serverDraftIDs.Delete(c.key())
	} else {
		serverDraftIDs.Store(c.key(), id)
	}
}

// draftOps runs each draft key's saves and deletes one at a time, in
// order. Each op reads the server draft id when it runs, so a save queued
// while the first drafts.create is still out becomes an update of that
// draft (not a second one), and a delete queued meanwhile deletes it (not
// nothing, leaving it orphaned). Saves queued behind a save coalesce:
// only the newest content is sent.
var draftOps sync.Map // draftKey → *draftQueue

type draftQueue struct {
	mu      sync.Mutex
	ops     []draftOp
	running bool
}

type draftOp struct {
	api  coreAPI
	save *draftSyncArgs   // drafts.create/update with this content
	del  *deleteDraftArgs // or delete (Draft.ID filled in when it runs)
}

func queueDraftOp(key string, op draftOp) {
	v, _ := draftOps.LoadOrStore(key, &draftQueue{})
	q := v.(*draftQueue)
	q.mu.Lock()
	if n := len(q.ops); n > 0 && q.ops[n-1].save != nil && op.save != nil {
		q.ops[n-1] = op
	} else {
		q.ops = append(q.ops, op)
	}
	if q.running {
		q.mu.Unlock()
		return
	}
	q.running = true
	q.mu.Unlock()
	go q.run(key)
}

func (q *draftQueue) run(key string) {
	for {
		q.mu.Lock()
		if len(q.ops) == 0 {
			q.running = false
			q.mu.Unlock()
			return
		}
		op := q.ops[0]
		q.ops = q.ops[1:]
		q.mu.Unlock()
		runDraftOp(key, op)
	}
}

func runDraftOp(key string, op draftOp) {
	cur := ""
	if v, ok := serverDraftIDs.Load(key); ok {
		cur = v.(string)
	}
	ctx := context.Background()
	if op.del != nil {
		if cur == "" {
			return
		}
		serverDraftIDs.Delete(key)
		args := *op.del
		args.Draft.ID = cur
		_ = deleteDraft(ctx, op.api, args)
		return
	}
	args := *op.save
	args.ID = cur
	id, err := syncDraft(ctx, op.api, args)
	if err == nil && id != "" {
		serverDraftIDs.Store(key, id)
	}
}

// syncDraftSoon (re)starts the save timer after a change (UI goroutine).
func (c *Composer) syncDraftSoon() {
	if c.edit != nil || c.h == nil || c.h.Core() == nil {
		return
	}
	if c.dsync.timer == nil {
		c.dsync.timer = time.AfterFunc(draftSyncDelay, func() { c.later(c.syncDraftNow) })
		return
	}
	c.dsync.timer.Reset(draftSyncDelay)
}

// syncDraftNow saves (or deletes, when empty) the server draft for what is
// in the editor now (UI goroutine; the network work runs in draftOps).
func (c *Composer) syncDraftNow() {
	v := c.ed.Version()
	if v == c.dsync.synced && c.attVer == c.dsync.syncedAtt {
		return
	}
	c.dsync.synced, c.dsync.syncedAtt = v, c.attVer
	files := c.att.draftFileIDs()
	gifs := c.att.gifCount()
	if c.ed.Empty() && c.att.Len() == 0 && gifs == 0 {
		c.deleteServerDraft("composer_cleared")
		return
	}
	if c.ed.Empty() && len(files) == 0 && gifs == 0 {
		return // only files still uploading: saved once they have ids
	}
	d := c.ed.Doc()
	args := draftSyncArgs{
		DraftID:   c.draftID(),
		ChannelID: c.ch,
		ThreadTs:  c.ts,
		Broadcast: c.broadcast && c.ts != "",
		Blocks:    d.RichTextBlocks(),
		Text:      d.Mrkdwn(),
		FileIDs:   files,
		// 53Wp / iLF4: the GIFs go with the draft as its attachments
		Attachments: c.att.draftGifAttachments(gifPickerOn(c.api.a)),
	}
	if ops, ok := d.Delta()["ops"].([]any); ok {
		args.Ops = ops
	}
	queueDraftOp(c.key(), draftOp{api: c.api, save: &args})
}

// deleteServerDraft removes the server draft, if there is one (after a
// send, or when the composer is emptied); one still being created is
// deleted once it exists.
func (c *Composer) deleteServerDraft(reason string) {
	if c.dsync.timer != nil {
		c.dsync.timer.Stop()
	}
	c.dsync.synced, c.dsync.syncedAtt = c.ed.Version(), c.attVer
	queueDraftOp(c.key(), draftOp{api: c.api, del: &deleteDraftArgs{Draft: draftRef{ClientDraftID: c.draftID()}, Reason: reason}})
}

// syncDraft creates (args.ID == "") or updates a server draft and returns
// its id. A plugin may skip it (OwlMode): then the id is unchanged.
func syncDraft(ctx context.Context, api coreAPI, args draftSyncArgs) (string, error) {
	name := thDraftUpdate
	if args.ID == "" {
		name = thDraftCreate
	}
	if t := currentTaut(); t.ThunkPatched(name) {
		r, err := taut.Dispatch(ctx, t, name, args, func(ctx context.Context, a draftSyncArgs) (draftResult, error) {
			id, err := saveDraftNative(ctx, api, a)
			return draftResult{ID: id}, err
		})
		if err != nil {
			return "", err
		}
		return r.ID, nil
	}
	return saveDraftNative(ctx, api, args)
}

func saveDraftNative(ctx context.Context, api coreAPI, a draftSyncArgs) (string, error) {
	d, err := api.saveDraft(ctx, core.DraftOpts{ID: a.ID, Channel: a.ChannelID, ThreadTS: a.ThreadTs, Broadcast: a.Broadcast,
		Text: a.Text, Blocks: a.Blocks, FileIDs: a.FileIDs, Attachments: a.Attachments})
	if err != nil {
		return "", err
	}
	return d.ID, nil
}

// deleteDraft is deleteDraftApi ({draft: {id}, reason}).
func deleteDraft(ctx context.Context, api coreAPI, args deleteDraftArgs) error {
	if t := currentTaut(); t.ThunkPatched(thDraftDelete) {
		_, err := taut.Dispatch(ctx, t, thDraftDelete, args, func(ctx context.Context, a deleteDraftArgs) (any, error) {
			if a.Draft.ID == "" {
				return nil, nil
			}
			return nil, api.deleteDraft(ctx, a.Draft.ID)
		})
		return err
	}
	return api.deleteDraft(ctx, args.Draft.ID)
}
