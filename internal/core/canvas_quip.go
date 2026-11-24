package core

// Quip's model of a canvas (docs/CANVAS-EDIT-PLAN.md "The save path"):
// what Slack's syncer holds of a canvas it edits. Core loads it with
// load-data and keeps it current the way Slack's client does: a
// document_update for the file runs fetch-latest (fetchThreadChanges),
// and so does opening the canvas (files.open's backlog fetch, 3uCk's
// openFile). The view still draws from the HTML export; the model is
// what saves are built on.

import (
	"context"
	"errors"
	"fmt"
	"log"
	"sync"
	"time"

	"github.com/some-du6e/clack/internal/quip"
	"github.com/some-du6e/clack/internal/slack"
)

type quipStates struct {
	mu   sync.Mutex
	byID map[string]*quipCanvas // by file id
}

// quipCanvas is one canvas's model: one call at a time (load, fetch or
// save), a fetch asked for during one runs once more after it. The model
// itself is held only to read or change it, never across a call, so a
// reader (CanvasModel, the frame) never waits on the network.
type quipCanvas struct {
	net      sync.Mutex // one call at a time; taken before mu
	mu       sync.Mutex // thread, doc and err
	thread   string
	doc      *quip.Doc
	err      error // the last load's failure
	fetching bool  // guarded by quipStates.mu
	again    bool  // guarded by quipStates.mu
}

var quipStatesByApp = newPerApp() // *App -> *quipStates

func (a *App) quipStates() *quipStates {
	if v, ok := quipStatesByApp.Load(a); ok {
		return v.(*quipStates)
	}
	v, _ := quipStatesByApp.LoadOrStore(a, &quipStates{byID: map[string]*quipCanvas{}})
	return v.(*quipStates)
}

func (a *App) quipCanvas(id string) *quipCanvas {
	st := a.quipStates()
	st.mu.Lock()
	defer st.mu.Unlock()
	return st.byID[id]
}

// ErrNoQuipThread is a canvas files.info gives no quip_thread_id for.
var ErrNoQuipThread = errors.New("core: the canvas has no Quip thread")

// LoadCanvasModel loads the canvas's Quip document with load-data, once:
// after it, the model follows the canvas's changes. A failed load is kept
// (CanvasModelError) and the next call tries again.
func (a *App) LoadCanvasModel(ctx context.Context, id string) error {
	q := a.quipCanvasFor(id)
	q.mu.Lock()
	loaded := q.doc != nil
	q.mu.Unlock()
	if loaded { // a call in flight (a save, a fetch) doesn't hold it up
		return nil
	}
	q.net.Lock()
	defer q.net.Unlock()
	return a.loadModelLocked(ctx, id, q)
}

// quipCanvasFor is the canvas's model state, made if core has none.
func (a *App) quipCanvasFor(id string) *quipCanvas {
	st := a.quipStates()
	st.mu.Lock()
	q := st.byID[id]
	if q == nil {
		q = &quipCanvas{}
		st.byID[id] = q
	}
	st.mu.Unlock()
	a.ensureCanvasWatch()
	return q
}

// loadModelLocked is LoadCanvasModel with q.net held.
func (a *App) loadModelLocked(ctx context.Context, id string, q *quipCanvas) error {
	q.mu.Lock()
	loaded, thread := q.doc != nil, q.thread
	q.mu.Unlock()
	if loaded {
		return nil
	}
	fail := func(err error) error {
		q.mu.Lock()
		q.err = err
		q.mu.Unlock()
		return err
	}
	if thread == "" {
		f, err := a.canvasFileInfo(ctx, id)
		if err != nil {
			return fail(err)
		}
		if f.QuipThreadID == "" {
			return fail(ErrNoQuipThread)
		}
		thread = f.QuipThreadID
	}
	d, err := a.quipLoad(ctx, thread)
	q.mu.Lock()
	q.thread = thread
	q.doc, q.err = d, err
	q.mu.Unlock()
	if err == nil {
		a.emit(Event{Type: "canvas_model", ID: id})
	}
	return err
}

func (a *App) quipLoad(ctx context.Context, thread string) (*quip.Doc, error) {
	b, err := a.C.CanvasCall(ctx, "load-data", []slack.CanvasField{{Name: "request_binary", Value: quip.MarshalBase64(quip.NewLoadRequest(thread))}})
	if err != nil {
		return nil, err
	}
	var w quip.LoadWrapper
	if err := quip.Unmarshal(b, &w); err != nil {
		return nil, fmt.Errorf("core: load-data: %w", err)
	}
	return quip.ReadLoad(thread, &w)
}

// CanvasModel calls f with the canvas's model while holding it (nil when
// core holds none); f must not keep d or anything in it.
func (a *App) CanvasModel(id string, f func(d *quip.Doc)) {
	q := a.quipCanvas(id)
	if q == nil {
		f(nil)
		return
	}
	q.mu.Lock()
	defer q.mu.Unlock()
	f(q.doc)
}

// CanvasModelError is the canvas model's last load failure (nil: none).
func (a *App) CanvasModelError(id string) error {
	q := a.quipCanvas(id)
	if q == nil {
		return nil
	}
	q.mu.Lock()
	defer q.mu.Unlock()
	return q.err
}

// followCanvasModel fetches what changed in a canvas core holds a model
// of (fetch-latest), in the background: one fetch at a time per canvas,
// and one more after it when asked for meanwhile.
func (a *App) followCanvasModel(id string) {
	st := a.quipStates()
	st.mu.Lock()
	q := st.byID[id]
	if q == nil {
		st.mu.Unlock()
		return
	}
	if q.fetching {
		q.again = true
		st.mu.Unlock()
		return
	}
	q.fetching = true
	st.mu.Unlock()
	go func() {
		for {
			ctx, cancel := context.WithTimeout(a.bg, 60*time.Second) // Slack's request timeout (OKwY)
			if err := a.fetchCanvasModel(ctx, id, q); err != nil && ctx.Err() == nil {
				log.Printf("core: canvas %s: fetch-latest: %s", id, slack.FailureDetail(err))
			}
			cancel()
			st.mu.Lock()
			if !q.again || a.bg.Err() != nil {
				q.fetching = false
				st.mu.Unlock()
				return
			}
			q.again = false
			st.mu.Unlock()
		}
	}()
}

// fetchCanvasModel is one fetch-latest for q, applied to its model; a
// reload Slack would do instead (backlog_reload_model_data, an index
// reload) loads the thread again.
func (a *App) fetchCanvasModel(ctx context.Context, id string, q *quipCanvas) error {
	q.net.Lock()
	defer q.net.Unlock()
	return a.fetchModelLocked(ctx, id, q)
}

// fetchModelLocked is fetchCanvasModel with q.net held.
func (a *App) fetchModelLocked(ctx context.Context, id string, q *quipCanvas) error {
	q.mu.Lock()
	if q.doc == nil {
		q.mu.Unlock()
		return nil
	}
	req, thread := quip.MarshalBase64(q.doc.FetchRequest()), q.thread
	q.mu.Unlock()
	b, err := a.C.CanvasCall(ctx, "fetch-latest", []slack.CanvasField{{Name: "request_binary", Value: req}})
	if err != nil {
		return err
	}
	var r quip.FetchResponse
	if err := quip.Unmarshal(b, &r); err != nil {
		return fmt.Errorf("core: fetch-latest: %w", err)
	}
	q.mu.Lock()
	changed, reload := q.doc.Apply(&r)
	q.mu.Unlock()
	if reload {
		d, err := a.quipLoad(ctx, thread)
		q.mu.Lock()
		if err != nil {
			q.err = err
		} else {
			q.doc, q.err = d, nil
		}
		q.mu.Unlock()
		if err != nil {
			return err
		}
		changed = []string{""}
	}
	if len(changed) > 0 {
		a.emit(Event{Type: "canvas_model", ID: id})
	}
	return nil
}

// dropCanvasModel forgets the canvas's model (its last view closed).
func (a *App) dropCanvasModel(id string) {
	st := a.quipStates()
	st.mu.Lock()
	delete(st.byID, id)
	st.mu.Unlock()
}
