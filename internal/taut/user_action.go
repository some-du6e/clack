package taut

import (
	"context"

	"github.com/some-du6e/clack/internal/jsrt"
)

// UserAction marks ctx's JS work as the user's (jsrt.UserBlocking): the
// send checks, send transforms and thunks a send dispatches with it run
// on the JS thread ahead of the renders, invalidations and events queued
// before them, as a browser runs input first. A message's pending copy
// waits for its send checks; it used to wait for the whole backlog too.
func UserAction(ctx context.Context) context.Context { return jsrt.UserBlocking(ctx) }
