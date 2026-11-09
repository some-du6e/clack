package demo

import (
	"context"

	"github.com/some-du6e/yoloslackgoclient/internal/fakeslack"
)

// World serves a Workspace through fakeslack's hooks and runs the bots.
// STUB: the engine agent replaces this file.
type World struct {
	fs   *fakeslack.Server
	ws   *Workspace
	opts Options
}

func newWorld(fs *fakeslack.Server, ws *Workspace, opts Options) *World {
	return &World{fs: fs, ws: ws, opts: opts}
}

// Start installs the hooks and starts the bots until ctx ends.
func (w *World) Start(ctx context.Context) {}

// Stop stops the bots.
func (w *World) Stop() {}
