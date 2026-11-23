package core

import (
	"context"
	"errors"
	"net/http"
	"testing"
	"time"

	"github.com/some-du6e/clack/internal/fakeslack"
	"github.com/some-du6e/clack/internal/quip"
	"github.com/some-du6e/clack/internal/slack"
)

func lastText(d *quip.Doc) (string, int64) {
	secs := d.Sections()
	s := secs[len(secs)-1]
	return quip.SectionRTML(s), *s.Sequence
}

// The model loads once with load-data and follows another client's edit:
// document_update, then fetch-latest.
func TestCanvasModelFollowsEdits(t *testing.T) {
	a, fs := canvasApp(t)
	ctx := context.Background()
	evs, stop := a.Subscribe()
	defer stop()
	a.SetRealtime(true)
	waitEvent(t, evs, 5*time.Second, func(ev Event) bool { return ev.Type == "connection" && ev.Connected })
	if err := a.OpenCanvas(ctx, fakeslack.CanvasShared); err != nil {
		t.Fatal(err)
	}
	if err := a.LoadCanvasModel(ctx, fakeslack.CanvasShared); err != nil {
		t.Fatal(err)
	}
	waitEvent(t, evs, 5*time.Second, func(ev Event) bool { return ev.Type == "canvas_model" && ev.ID == fakeslack.CanvasShared })
	var max0 int64
	a.CanvasModel(fakeslack.CanvasShared, func(d *quip.Doc) {
		if d == nil {
			t.Fatal("no model after LoadCanvasModel")
		}
		max0 = *d.Document.MaxSectionSequence
		if *d.Thread.ID != fs.QuipThreadID(fakeslack.CanvasShared) || len(d.Sections()) < 5 {
			t.Errorf("model of thread %s with %d sections", *d.Thread.ID, len(d.Sections()))
		}
	})
	if err := a.LoadCanvasModel(ctx, fakeslack.CanvasShared); err != nil || fs.CallCount("canvas/load-data") != 1 {
		t.Errorf("a second load: %v, %d load-data calls", err, fs.CallCount("canvas/load-data"))
	}

	fs.EditCanvasAsOther(fakeslack.CanvasShared, "Added by someone else")
	waitEvent(t, evs, 5*time.Second, func(ev Event) bool { return ev.Type == "canvas_model" && ev.ID == fakeslack.CanvasShared })
	a.CanvasModel(fakeslack.CanvasShared, func(d *quip.Doc) {
		if rt, seq := lastText(d); rt != "Added by someone else" || seq <= max0 {
			t.Errorf("last section %q at %d (was max %d)", rt, seq, max0)
		}
	})
	if fs.CallCount("canvas/fetch-latest") == 0 {
		t.Error("no fetch-latest")
	}
	for _, r := range fs.QuipRequests() {
		if r.Status != http.StatusOK || r.Path == "edit-document" {
			t.Errorf("%s answered %d (%s)", r.Path, r.Status, r.Why)
		}
	}
	if err := a.CloseCanvas(ctx, fakeslack.CanvasShared); err != nil {
		t.Fatal(err)
	}
	a.CanvasModel(fakeslack.CanvasShared, func(d *quip.Doc) {
		if d != nil {
			t.Error("the model outlived the last view")
		}
	})
}

// Opening the canvas catches up the model (Slack's backlog fetch after
// files.open), with no realtime socket at all.
func TestCanvasModelBacklogFetch(t *testing.T) {
	a, fs := canvasApp(t)
	ctx := context.Background()
	evs, stop := a.Subscribe()
	defer stop()
	if err := a.LoadCanvasModel(ctx, fakeslack.CanvasShared); err != nil {
		t.Fatal(err)
	}
	waitEvent(t, evs, 5*time.Second, func(ev Event) bool { return ev.Type == "canvas_model" })
	fs.EditCanvasAsOther(fakeslack.CanvasShared, "While nobody looked")
	if err := a.OpenCanvas(ctx, fakeslack.CanvasShared); err != nil {
		t.Fatal(err)
	}
	waitEvent(t, evs, 5*time.Second, func(ev Event) bool { return ev.Type == "canvas_model" && ev.ID == fakeslack.CanvasShared })
	a.CanvasModel(fakeslack.CanvasShared, func(d *quip.Doc) {
		if rt, _ := lastText(d); rt != "While nobody looked" {
			t.Errorf("last section %q", rt)
		}
	})
}

// A failed load is kept, and the next one tries again.
func TestCanvasModelLoadFailure(t *testing.T) {
	a, fs := canvasApp(t)
	ctx := context.Background()
	fs.SetFault("canvas/load-data", fakeslack.Fault{Status: http.StatusServiceUnavailable, Times: 1})
	err := a.LoadCanvasModel(ctx, fakeslack.CanvasShared)
	var he *slack.HTTPError
	if !errors.As(err, &he) || he.Status != http.StatusServiceUnavailable || !errors.As(a.CanvasModelError(fakeslack.CanvasShared), &he) {
		t.Fatalf("load = %v, kept %v", err, a.CanvasModelError(fakeslack.CanvasShared))
	}
	// a retry the viewer asks for goes out at once (the guard would
	// answer an identical failed call with its error for a second)
	if err := a.LoadCanvasModel(slack.NoBackoff(ctx), fakeslack.CanvasShared); err != nil || a.CanvasModelError(fakeslack.CanvasShared) != nil {
		t.Fatalf("the retry: %v", err)
	}
	if err := a.LoadCanvasModel(ctx, fakeslack.CanvasImage); !errors.Is(err, ErrNoQuipThread) {
		t.Errorf("a file that isn't a canvas: %v", err)
	}
}

// The model is held only to read or change it, never across a call: a
// reader doesn't wait on a fetch-latest in flight (the review of B1: the
// lock was held across load-data and fetch-latest, so CanvasModel, called
// from the frame, would have waited out the network).
func TestCanvasModelReadsDuringCalls(t *testing.T) {
	a, fs := canvasApp(t)
	ctx := context.Background()
	if err := a.LoadCanvasModel(ctx, fakeslack.CanvasShared); err != nil {
		t.Fatal(err)
	}
	q := a.quipCanvas(fakeslack.CanvasShared)
	if q == nil {
		t.Fatal("no model state after a load")
	}
	before := fs.CallCount("canvas/fetch-latest")
	fs.SetFault("canvas/fetch-latest", fakeslack.Fault{Delay: 2 * time.Second, Times: 1})
	done := make(chan error, 1)
	go func() { done <- a.fetchCanvasModel(ctx, fakeslack.CanvasShared, q) }()
	deadline := time.Now().Add(5 * time.Second)
	for fs.CallCount("canvas/fetch-latest") == before {
		if time.Now().After(deadline) {
			t.Fatal("fetch-latest never went out")
		}
		time.Sleep(5 * time.Millisecond)
	}
	start := time.Now()
	read := false
	a.CanvasModel(fakeslack.CanvasShared, func(d *quip.Doc) { read = d != nil })
	if waited := time.Since(start); !read || waited > 500*time.Millisecond {
		t.Errorf("CanvasModel during a fetch: read a model %v, after %v", read, waited)
	}
	if err := <-done; err != nil {
		t.Errorf("the fetch: %v", err)
	}
}
