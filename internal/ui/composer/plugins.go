package composer

import (
	"context"
	_ "embed"
	"encoding/json"
	"sync"
	"time"

	"github.com/some-du6e/yoloslackgoclient/internal/taut"
)

// The composer's part of the plugin runtime: a taut extension prelude
// (plugins.js) that gives the composer's boundaries the shapes Slack's
// components have in JS, where plugins find them by more than their name.
//
//   - TextyInput is a memo of a class with getNumCharsOverLimit, and its
//     ref is Slack's {instance: {quill}} (CharacterCounter filters on the
//     former and counts through the latter).
//   - __clackComposer.fileName reads an attachment's name through a real
//     File, whose name getter plugins may redefine (AnonymiseFileNames).
//
// Like every extension it costs nothing until the runtime starts.

//go:embed plugins.js
var pluginsPrelude string

// extended records the Tauts the prelude was registered with (SetTaut
// runs again after an account switch).
var extended sync.Map // *taut.Taut → struct{}

func extendTaut(t *taut.Taut) {
	if t == nil {
		return
	}
	if _, dup := extended.LoadOrStore(t, struct{}{}); dup {
		return
	}
	_ = t.Extend(taut.Extension{Name: "composer", Prelude: pluginsPrelude})
}

// uploadName is the name an attached file goes out under. Slack's upload
// reads File.name, which plugins may redefine (AnonymiseFileNames swaps in
// a random name); with the plugin runtime up the name is read from a real
// File there, else it is name. Call off the UI goroutine: it may wait for
// JS (bounded).
func uploadName(name, mime string) string {
	t := currentTaut()
	if t == nil || !t.Started() {
		return name
	}
	rt, err := t.JS()
	if err != nil {
		return name
	}
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	out, err := rt.Call(ctx, "__clackComposer.fileName", name, mime)
	if err != nil {
		return name
	}
	var s string
	if json.Unmarshal(out, &s) != nil || s == "" {
		return name
	}
	return attCleanName(s)
}

// ---- typing ended (currentUserEndedTyping) ----

// typingStopped is Slack's currentUserEndedTyping moment: the composer
// emptied (sent or cleared) after it reported typing. Clack sends no
// "stopped typing" event, so with no plugin patching the thunk this is a
// flag flip and nothing more.
func (c *Composer) typingStopped() {
	if !c.typingOn {
		return
	}
	c.typingOn = false
	if t := currentTaut(); t.ThunkPatched(thTypingEnded) {
		api, ch, ts := c.api, c.ch, c.ts
		go sendTypingEnded(t, api, ch, ts)
	}
}

func sendTypingEnded(t *taut.Taut, api coreAPI, ch, ts string) {
	_, _ = taut.Dispatch(context.Background(), t, thTypingEnded, typingArgs{ChannelID: ch, ThreadTs: ts},
		func(_ context.Context, a typingArgs) (any, error) {
			return nil, typingEndedNative(api, a.ChannelID, a.ThreadTs)
		})
}

// typingEndedNative is currentUserEndedTyping's native: Slack's realtime
// protocol has no "stopped" message, so it does nothing. A var so tests
// can see whether a plugin let it through.
var typingEndedNative = func(api coreAPI, ch, ts string) error { return nil }
