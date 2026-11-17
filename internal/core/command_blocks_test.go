package core

import (
	"context"
	"encoding/json"
	"testing"
)

// executeCommand (boot-render 5dbN) calls chatCommandApi with the
// arguments' blocks and no text, so chat.command never carries the plain
// words: the server reads mentions from the blocks. With no arguments it
// sends neither. Callers that only have text (a plugin's command, the local
// /invite's fallback) still send it.
func TestCommandSendsBlocksLikeSlack(t *testing.T) {
	a, f := testApp(t)
	f.reply["chat.command"] = `{"ok":true}`
	blocks := json.RawMessage(`[{"type":"rich_text","elements":[{"type":"rich_text_section","elements":[{"type":"text","text":"me "},{"type":"user","user_id":"U082DRXPMF1"},{"type":"text","text":" do that"}]}]}]`)
	ctx := context.Background()

	if _, err := a.Command(ctx, CommandOpts{Channel: "C1", Command: "/remind", Text: "me @karim do that", Blocks: blocks}); err != nil {
		t.Fatal(err)
	}
	p := f.last("chat.command")
	if p.Has("text") {
		t.Errorf("chat.command sent text %q with its blocks; Slack sends blocks only, and the plain words let the server match \"@karim\" to someone else", p.Get("text"))
	}
	if p.Get("blocks") != string(blocks) {
		t.Errorf("chat.command blocks = %q, want %s", p.Get("blocks"), blocks)
	}

	if _, err := a.Command(ctx, CommandOpts{Channel: "C1", Command: "/giphy"}); err != nil {
		t.Fatal(err)
	}
	if p := f.last("chat.command"); p.Has("text") || p.Has("blocks") {
		t.Errorf("a command with no arguments sent %v; Slack sends neither text nor blocks", p)
	}

	if _, err := a.Command(ctx, CommandOpts{Channel: "C1", Command: "/se", Text: "<@U1> anchor"}); err != nil {
		t.Fatal(err)
	}
	if p := f.last("chat.command"); p.Get("text") != "<@U1> anchor" || p.Has("blocks") {
		t.Errorf("a text-only caller sent %v", p)
	}
}
