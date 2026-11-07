package composer

import (
	"context"
	"sync"
	"time"

	"github.com/some-du6e/yoloslackgoclient/internal/core"
	"github.com/some-du6e/yoloslackgoclient/internal/taut"
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
const draftSyncDelay = 1500 * time.Millisecond

// draftSync is one composer's server-draft state.
type draftSync struct {
	timer  *time.Timer
	synced uint64 // editor version last saved (UI goroutine)
	mu     sync.Mutex
	id     string // server draft id (Dr…), "" before the first save
	gen    uint64 // bumps on every queued op so stale results are dropped
}

// serverDraftIDs keeps each composer key's server draft id across composer
// instances (a channel switch drops and rebuilds composers).
var serverDraftIDs sync.Map // draftKey → string

func (c *Composer) serverDraftID() string {
	c.dsync.mu.Lock()
	defer c.dsync.mu.Unlock()
	if c.dsync.id == "" {
		if v, ok := serverDraftIDs.Load(c.key()); ok {
			c.dsync.id = v.(string)
		}
	}
	return c.dsync.id
}

func (c *Composer) setServerDraftID(id string) {
	c.dsync.mu.Lock()
	c.dsync.id = id
	c.dsync.mu.Unlock()
	if id == "" {
		serverDraftIDs.Delete(c.key())
	} else {
		serverDraftIDs.Store(c.key(), id)
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
// in the editor now (UI goroutine; the network work runs in a goroutine).
func (c *Composer) syncDraftNow() {
	v := c.ed.Version()
	if v == c.dsync.synced {
		return
	}
	c.dsync.synced = v
	if c.ed.Empty() {
		c.deleteServerDraft("composer_cleared")
		return
	}
	d := c.ed.Doc()
	args := draftSyncArgs{
		DraftID:   c.draftID(),
		ID:        c.serverDraftID(),
		ChannelID: c.ch,
		ThreadTs:  c.ts,
		Broadcast: c.broadcast && c.ts != "",
		Blocks:    d.RichTextBlocks(),
		Text:      d.Mrkdwn(),
	}
	if ops, ok := d.Delta()["ops"].([]any); ok {
		args.Ops = ops
	}
	gen := c.bumpDraftGen()
	api := c.api
	go func() {
		id, err := syncDraft(context.Background(), api, args)
		if err != nil || !c.draftGenIs(gen) {
			return
		}
		if id != "" {
			c.setServerDraftID(id)
		}
	}()
}

// deleteServerDraft removes the server draft, if there is one (after a
// send, or when the composer is emptied).
func (c *Composer) deleteServerDraft(reason string) {
	if c.dsync.timer != nil {
		c.dsync.timer.Stop()
	}
	c.dsync.synced = c.ed.Version()
	id := c.serverDraftID()
	c.bumpDraftGen()
	if id == "" {
		return
	}
	c.setServerDraftID("")
	api := c.api
	args := deleteDraftArgs{Draft: draftRef{ID: id, ClientDraftID: c.draftID()}, Reason: reason}
	go func() { _ = deleteDraft(context.Background(), api, args) }()
}

func (c *Composer) bumpDraftGen() uint64 {
	c.dsync.mu.Lock()
	defer c.dsync.mu.Unlock()
	c.dsync.gen++
	return c.dsync.gen
}

func (c *Composer) draftGenIs(g uint64) bool {
	c.dsync.mu.Lock()
	defer c.dsync.mu.Unlock()
	return c.dsync.gen == g
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
		Text: a.Text, Blocks: a.Blocks, FileIDs: a.FileIDs})
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
