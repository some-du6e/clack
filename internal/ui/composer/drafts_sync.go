package composer

import (
	"context"
	"errors"
	"sync"
	"time"
	"weak"

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
	synced    uint64 // editor version last saved (or being saved)
	syncedAtt uint64 // Composer.attVer last saved
	// A refused save leaves the composer unsynced and tries again (retry,
	// backing off by retryIn), as Slack's drafts store keeps a draft and
	// syncs it later: nothing typed is dropped because Slack didn't answer.
	retry   *time.Timer
	retryIn time.Duration
	// stashing: a "Save as draft" is out (the composer empties once Slack
	// has it, draft_button.go); pickWant: the draft a pick waits to put in
	// the slot while the slot's own text is saved first (stash_draft.go)
	stashing bool
	pickWant *core.Draft
}

// unsyncedVer marks a composer whose last save failed: no editor version
// equals it, so the next sync sends what the composer holds.
const unsyncedVer = ^uint64(0)

// The retries after a refused save: draftRetryMin, doubling up to
// draftRetryMax (vars for tests).
var (
	draftRetryMin = 5 * time.Second
	draftRetryMax = time.Minute
)

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
	// saves is how many saves and stashes are queued or running; dirty,
	// whether the last of them failed: then Slack doesn't have the slot's
	// text, and nothing may replace it before it's saved (draftKeyBusy)
	saves int
	dirty bool
}

type draftOp struct {
	api  coreAPI
	save *draftSyncArgs   // drafts.create/update with this content
	del  *deleteDraftArgs // or delete (Draft.ID filled in when it runs)
	// or attach the key's slot to this server draft (selectStashedDraft,
	// stash_draft.go): the saves after it update that one
	attach *string
	// or save this content as a composer draft of its own ("Save as
	// draft", draft_button.go): keep updates the key's server draft
	// (stashed under it, a conversation's), else a new one is made (a
	// thread's: the composer deletes the one it held once it empties).
	// After a kept stash Slack took, the key holds no draft; a refused
	// stash changes nothing (the composer keeps its text).
	stash *draftSyncArgs
	keep  bool
	// done hears how the op went, on the queue's goroutine
	done func(error)
}

func queueDraftOp(key string, op draftOp) {
	v, _ := draftOps.LoadOrStore(key, &draftQueue{})
	q := v.(*draftQueue)
	q.mu.Lock()
	if n := len(q.ops); n > 0 && q.ops[n-1].save != nil && op.save != nil {
		// the newer content goes in the older one's place; both hear how
		// it went
		if prev, next := q.ops[n-1].done, op.done; prev != nil {
			op.done = func(err error) {
				prev(err)
				if next != nil {
					next(err)
				}
			}
		}
		q.ops[n-1] = op
	} else {
		q.ops = append(q.ops, op)
		if op.save != nil || op.stash != nil {
			q.saves++
		}
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
		// the slice's array kept every op run (and the session its api is),
		// for as long as the key's queue: for good
		q.ops[0] = draftOp{}
		q.ops = q.ops[1:]
		if len(q.ops) == 0 {
			q.ops = nil
		}
		q.mu.Unlock()
		err := runDraftOp(key, op)
		q.mu.Lock()
		switch {
		case op.save != nil || op.stash != nil:
			q.saves--
			q.dirty = err != nil
		case op.del != nil || op.attach != nil:
			q.dirty = false // the slot is empty, or a draft Slack has
		}
		q.mu.Unlock()
		if op.done != nil {
			op.done(err)
		}
	}
}

// draftKeyBusy is a draft key's saves queued or running, and whether the
// last one failed (the slot holds text Slack doesn't have).
func draftKeyBusy(key string) (saves int, dirty bool) {
	v, ok := draftOps.Load(key)
	if !ok {
		return 0, false
	}
	q := v.(*draftQueue)
	q.mu.Lock()
	defer q.mu.Unlock()
	return q.saves, q.dirty
}

func runDraftOp(key string, op draftOp) error {
	if op.attach != nil {
		serverDraftIDs.Store(key, *op.attach)
		return nil
	}
	cur := ""
	if v, ok := serverDraftIDs.Load(key); ok {
		cur = v.(string)
	}
	// in order with the user's input (a dispatch's default, taut's
	// inOrder): a plugin click after it can't go ahead (OwlMode decides in
	// the draft's patch whether it syncs), and it doesn't jump the renders
	// or a send queued before it
	ctx := context.Background()
	if op.stash != nil {
		args := *op.stash
		args.ID = ""
		if op.keep {
			args.ID = cur
		}
		_, err := syncDraft(ctx, op.api, args)
		if err == nil && op.keep {
			serverDraftIDs.Delete(key) // the draft is the stash's now
		}
		return err
	}
	if op.del != nil {
		if cur == "" {
			return nil
		}
		serverDraftIDs.Delete(key)
		args := *op.del
		args.Draft.ID = cur
		return deleteDraft(ctx, op.api, args)
	}
	args := *op.save
	args.ID = cur
	id, err := syncDraft(ctx, op.api, args)
	if err == nil && id != "" {
		serverDraftIDs.Store(key, id)
	}
	return err
}

// syncDraftSoon (re)starts the save timer after a change (UI goroutine).
func (c *Composer) syncDraftSoon() {
	if c.noDraft() || c.h == nil || c.h.Core() == nil {
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
	if !c.noDraft() && c.attachGen != attachGenOf(c.key()) {
		return // a composer from before a picked draft took the slot (stash_draft.go)
	}
	if c.ed.Version() == c.dsync.synced && c.attVer == c.dsync.syncedAtt {
		return
	}
	c.queueSave(nil)
}

// errDraftUploading: the composer holds only files still uploading, which
// a draft can't carry yet.
var errDraftUploading = errors.New("a file is still uploading")

// queueSave saves (or deletes, when empty) what the composer holds now;
// done, if set, hears the answer on the UI goroutine. A refused save
// leaves the composer unsynced and retries (saved).
func (c *Composer) queueSave(done func(error)) {
	v, av := c.ed.Version(), c.attVer
	c.dsync.synced, c.dsync.syncedAtt = v, av
	files := c.att.draftFileIDs()
	gifs := c.att.gifCount()
	if c.ed.Empty() && c.att.Len() == 0 && gifs == 0 {
		c.deleteServerDraft("composer_cleared")
		if done != nil {
			done(nil)
		}
		return
	}
	if c.ed.Empty() && len(files) == 0 && gifs == 0 {
		if done != nil { // only files still uploading: saved once they have ids
			done(errDraftUploading)
		}
		return
	}
	args := c.draftArgs()
	queueDraftOp(c.key(), draftOp{api: c.api, save: &args, done: func(err error) {
		c.later(func() {
			c.saved(v, av, err)
			if done != nil {
				done(err)
			}
		})
	}})
}

// saved is a save's answer (UI goroutine). A refused one leaves the
// composer unsynced, so the next change, flush or retry sends it again.
func (c *Composer) saved(v, av uint64, err error) {
	if err == nil {
		c.dsync.retryIn = 0
		return
	}
	if c.dsync.synced == v && c.dsync.syncedAtt == av {
		c.dsync.synced = unsyncedVer
	}
	c.retrySync()
}

// retrySync syncs again later, backing off, while the composer is open.
func (c *Composer) retrySync() {
	if c.closed {
		return
	}
	c.dsync.retryIn = min(max(2*c.dsync.retryIn, draftRetryMin), draftRetryMax)
	if c.dsync.retry != nil {
		c.dsync.retry.Stop()
	}
	// the timer holds the composer weakly: one its view dropped goes with
	// its App, and the text it held syncs from the next composer of its
	// key (New, a dirty key). closed is the UI goroutine's: read there.
	wc := weak.Make(c)
	c.dsync.retry = time.AfterFunc(c.dsync.retryIn, func() {
		if c := wc.Value(); c != nil {
			c.later(func() {
				if !c.closed {
					c.syncDraftNow()
				}
			})
		}
	})
}

// draftArgs is the server draft for what the composer holds now.
func (c *Composer) draftArgs() draftSyncArgs {
	files := c.att.draftFileIDs()
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
	return args
}

// deleteServerDraft removes the server draft, if there is one (after a
// send, or when the composer is emptied); one still being created is
// deleted once it exists.
func (c *Composer) deleteServerDraft(reason string) {
	if c.dsync.timer != nil {
		c.dsync.timer.Stop()
	}
	if !c.noDraft() && c.attachGen != attachGenOf(c.key()) {
		return // the slot is another draft's now (stash_draft.go)
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
		Text: a.Text, Blocks: a.Blocks, FileIDs: a.FileIDs, Attachments: a.Attachments, DateStashed: a.DateStashed})
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
