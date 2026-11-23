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

// quipCanvas is one canvas's model: one call at a time, a fetch asked
// for during one runs once more after it.
type quipCanvas struct {
	mu       sync.Mutex // the model; held for a call
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
	st := a.quipStates()
	st.mu.Lock()
	q := st.byID[id]
	if q == nil {
		q = &quipCanvas{}
		st.byID[id] = q
	}
	st.mu.Unlock()
	a.ensureCanvasWatch()
	q.mu.Lock()
	defer q.mu.Unlock()
	if q.doc != nil {
		return nil
	}
	if q.thread == "" {
		f, err := a.canvasFileInfo(ctx, id)
		if err != nil {
			q.err = err
			return err
		}
		if f.QuipThreadID == "" {
			q.err = ErrNoQuipThread
			return q.err
		}
		q.thread = f.QuipThreadID
	}
	d, err := a.quipLoad(ctx, q.thread)
	q.doc, q.err = d, err
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
	q.mu.Lock()
	defer q.mu.Unlock()
	if q.doc == nil {
		return nil
	}
	b, err := a.C.CanvasCall(ctx, "fetch-latest", []slack.CanvasField{{Name: "request_binary", Value: quip.MarshalBase64(q.doc.FetchRequest())}})
	if err != nil {
		return err
	}
	var r quip.FetchResponse
	if err := quip.Unmarshal(b, &r); err != nil {
		return fmt.Errorf("core: fetch-latest: %w", err)
	}
	changed, reload := q.doc.Apply(&r)
	if reload {
		d, err := a.quipLoad(ctx, q.thread)
		if err != nil {
			q.err = err
			return err
		}
		q.doc, q.err = d, nil
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
