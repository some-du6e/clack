package taut

import (
	"encoding/json"
	"errors"
	"testing"
	"time"

	"github.com/some-du6e/yoloslackgoclient/internal/jsrt"
)

// Regression tests for bugs found running Taut's plugins live in Clack
// (docs/PLUGINS-live.md).

// Taut calls send checks as check(blocks, {channelId, threadTs}); HaikuWarning
// and OwlMode destructure the second argument, so a single context argument
// threw and the message went out unchecked.
func TestSendCheckArgs(t *testing.T) {
	h := newHarness(t)
	h.js(`
		globalThis.__seen = []
		api.composer.addSendCheck(async (blocks, { channelId, threadTs }) => {
			__seen.push({ n: blocks.length, type: blocks[0]?.type ?? '', ch: channelId, ts: threadTs ?? '' })
			return blocks.length !== 2
		})
		return 1`)
	eventually(t, 2*time.Second, "send check registered", h.tt.HasSendChecks)
	one := json.RawMessage(`[{"type":"rich_text","elements":[]}]`)
	two := json.RawMessage(`[{"type":"rich_text","elements":[]},{"type":"rich_text","elements":[]}]`)
	if !h.tt.SendCheck(h.ctx(), ComposerContext{Kind: "thread", ChannelID: chGen, ThreadTs: "1700000001.000100", Blocks: one}) {
		t.Error("a passing check held the message back")
	}
	if h.tt.SendCheck(h.ctx(), ComposerContext{Kind: "channel", ChannelID: chGen, Blocks: two}) {
		t.Error("a check returning false didn't hold the message back")
	}
	var seen []struct {
		N        int
		Type, Ch string
		Ts       string
	}
	h.jsInto(`return __seen`, &seen)
	if len(seen) != 2 || seen[0].N != 1 || seen[0].Type != "rich_text" || seen[0].Ch != chGen || seen[0].Ts != "1700000001.000100" ||
		seen[1].N != 2 || seen[1].Ts != "" {
		t.Errorf("checks saw %+v", seen)
	}
}

func TestErrWithStack(t *testing.T) {
	err := &jsrt.JSError{Name: "TypeError", Message: "not a function", Stack: "    at f (a.js:1)\n    at g (b.js:2)\n"}
	if got := errWithStack(err); got != "TypeError: not a function [at f (a.js:1) | at g (b.js:2)]" {
		t.Errorf("errWithStack = %q", got)
	}
	if got := errWithStack(errors.New("plain")); got != "plain" {
		t.Errorf("errWithStack(plain) = %q", got)
	}
	if got := logArg([]any{"MessageSender", []byte("{}")}); got != "MessageSender" {
		t.Errorf("logArg = %q", got)
	}
}
