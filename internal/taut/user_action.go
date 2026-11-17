package taut

import (
	"context"

	"github.com/some-du6e/clack/internal/jsrt"
)

// UserAction marks ctx's JS work as the user's (jsrt.UserBlocking): the
// send checks, send transforms and thunks a send dispatches with it, and
// the typing thunks, run on the JS thread ahead of the renders queued
// before them, in order with the user's clicks and keys (Bridge.Call,
// CallEvent and discrete DOM events take the same lane) and after the
// store updates and socket events queued before them (runtime.post), as
// a browser runs input first and in order. A message's pending copy waits
// for its send checks; it used to wait for the whole backlog too.
func UserAction(ctx context.Context) context.Context { return jsrt.UserBlocking(ctx) }
