package popups

import (
	"context"
	"encoding/json"
	"net/url"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/some-du6e/clack/internal/fakeslack"
	"github.com/some-du6e/clack/internal/richtext"
)

// Synthetic fixtures only.

// spTS is the ts the fake gives a sent prompt.
const spTS = "1700000100.000200"

// spBooted is a booted host whose fake answers chat.postMessage to a DM
// itself: the message when ok holds, else not_in_channel. Every call is
// recorded.
func spBooted(t *testing.T, ok *atomic.Bool) (*tzHost, *emCalls) {
	t.Helper()
	fs, err := fakeslack.Start(fakeslack.Small)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { fs.Close() })
	rec := &emCalls{calls: map[string][]url.Values{}}
	fs.SetHook(func(method string, form url.Values) (any, bool) {
		rec.mu.Lock()
		rec.calls[method] = append(rec.calls[method], form)
		rec.mu.Unlock()
		if method != "chat.postMessage" || !strings.HasPrefix(form.Get("channel"), "D") {
			return nil, false
		}
		if !ok.Load() {
			return map[string]any{"ok": false, "error": "not_in_channel"}, true
		}
		return map[string]any{"ok": true, "channel": form.Get("channel"), "ts": spTS,
			"message": map[string]any{"type": "message", "user": fakeslack.SelfID, "text": form.Get("text"), "ts": spTS}}, true
	})
	h := hcEnvOn(t, fs)
	ctx, cancel := context.WithCancel(context.Background())
	t.Cleanup(cancel)
	done := make(chan error, 1)
	go func() { done <- h.a.Boot(ctx) }()
	select {
	case err := <-done:
		if err != nil {
			t.Fatalf("boot: %v", err)
		}
	case <-time.After(30 * time.Second):
		t.Fatal("boot timed out")
	}
	return h, rec
}

func (h *tzHost) threadList() []string {
	h.mu.Lock()
	defer h.mu.Unlock()
	return append([]string(nil), h.threads...)
}

// Send to {agentName} (startAgentOrAssistantThread): the agent's DM
// opened, the prompt posted there as a verbatim mrkdwn section with
// agent_message_source_type shared_prompt_link (prompt_rich_text's block
// itself when it has one), and that message's thread opened. A send that
// fails is DeadEndLinkDialog's shared_prompt sentence with Ok.
func TestSharePromptSendOpensThread(t *testing.T) {
	var ok atomic.Bool
	ok.Store(true)
	h, rec := spBooted(t, &ok)
	const agent = "U0FAKEAFAG"
	SharePromptSend(h, &richtext.SharePrompt{Prompt: "Summarise this week", PromptID: "P1", BotUserID: agent, Shown: true})
	rec.wait(t, "conversations.open", map[string]string{"users": agent}, 10*time.Second)
	post := rec.wait(t, "chat.postMessage", map[string]string{"agent_message_source_type": "shared_prompt_link"}, 10*time.Second)
	var blocks []struct {
		Type string `json:"type"`
		Text struct {
			Type     string `json:"type"`
			Text     string `json:"text"`
			Verbatim bool   `json:"verbatim"`
		} `json:"text"`
	}
	if err := json.Unmarshal([]byte(post.Get("blocks")), &blocks); err != nil || len(blocks) != 1 || blocks[0].Type != "section" ||
		blocks[0].Text.Type != "mrkdwn" || blocks[0].Text.Text != "Summarise this week" || !blocks[0].Text.Verbatim {
		t.Errorf("blocks %s (%v): want one verbatim mrkdwn section of the prompt", post.Get("blocks"), err)
	}
	ch := post.Get("channel")
	maUntil(t, "the thread", func() bool { return len(h.threadList()) == 1 })
	if got := h.threadList()[0]; got != ch+"/"+spTS {
		t.Errorf("opened %q, want %s/%s", got, ch, spTS)
	}

	raw := `{"type":"rich_text","elements":[{"type":"rich_text_section","elements":[{"type":"text","text":"Summarise this week"}]}]}`
	SharePromptSend(h, &richtext.SharePrompt{Prompt: "Summarise this week", PromptID: "P2", BotUserID: agent, RichRaw: json.RawMessage(raw), Shown: true})
	maUntil(t, "the rich text send", func() bool {
		rec.mu.Lock()
		defer rec.mu.Unlock()
		ps := rec.calls["chat.postMessage"]
		return len(ps) == 2 && ps[1].Get("blocks") == "["+raw+"]"
	})

	ok.Store(false)
	SharePromptSend(h, &richtext.SharePrompt{Prompt: "Summarise this week", PromptID: "P3", BotUserID: agent, Shown: true})
	maUntil(t, "DeadEndLinkDialog", func() bool { m := h.topModal(); return m != nil && m.o.Title == "We’re unable to open this link" })
	m := h.topModal()
	if len(m.o.Buttons) != 1 || m.o.Buttons[0].Label != "Ok" || m.o.Buttons[0].Kind != BtnDanger {
		t.Errorf("buttons %+v, want Ok (danger)", m.o.Buttons)
	}
	if got := deadEndText("shared_prompt"); got != "This link opens an agent that you don’t have access to, or something else went wrong." {
		t.Errorf("sentence %q", got)
	}
	if n := len(h.threadList()); n != 2 {
		t.Errorf("%d threads opened, want 2 (the failed send opens none)", n)
	}
}
