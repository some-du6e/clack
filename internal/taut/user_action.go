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

// UserSend is UserAction for a send or an edit: its JS chain goes through
// Go between its steps (the patches call the native, which dispatches the
// send transforms and the sendMessage thunk), and while it awaits the
// native the plugin runtime starts no queued render for a moment
// (jsrt.UserChain), as Slack runs a send in one JS task.
func UserSend(ctx context.Context) context.Context { return jsrt.UserChain(ctx) }
