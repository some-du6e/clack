package composer

import (
	"bytes"
	"encoding/json"
	"log"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/some-du6e/clack/internal/slack"
	"github.com/some-du6e/clack/internal/store"
	"github.com/some-du6e/clack/internal/ui/theme"
)

// lockedLog is a log sink the composer's goroutines may write while the
// test reads it.
type lockedLog struct {
	mu sync.Mutex
	b  bytes.Buffer
}

func (l *lockedLog) Write(p []byte) (int, error) {
	l.mu.Lock()
	defer l.mu.Unlock()
	return l.b.Write(p)
}

func (l *lockedLog) String() string {
	l.mu.Lock()
	defer l.mu.Unlock()
	return l.b.String()
}

// The Slack CLI's sign-in (docs/SLACKAUTHTICKET.md): `slack login` prints
// "/slackauthticket <ticket>" to run in any channel or DM. No Slack client
// has code for it. The composer's executeCommand (boot-render 5dbN) finds
// no local or listed command, so it goes to chat.command like any server
// command, in a thread too, and the modals Slack opens for it (the CLI's
// permissions, then the challenge code) answer the command's
// client_token. The ticket is a credential: it leaves only as the
// command's text, is never posted as a message and never reaches the log.
// Synthetic data only.
func TestSlackAuthTicketCommand(t *testing.T) {
	const ticket = "MDAwMDAwMDAtMDAwMC00MDAwLTgwMDAtMDAwMDAwMDAwMDAw" // base64 of a made-up UUID
	logged := &lockedLog{}
	prev := log.Writer()
	log.SetOutput(logged)
	t.Cleanup(func() { log.SetOutput(prev) })

	app, fs := syntheticApp(t, func(*store.Store) {})
	stubTyping(t)
	views, stop := app.Interactive().Subscribe()
	defer stop()
	h := &slickHost{app: app, th: theme.New(false)}
	for _, ts := range []string{"", "1700000000.000100"} {
		c := New(h, tChGen, ts)
		t.Cleanup(c.Close)
		d := NewDoc()
		d.Insert(0, "/slackauthticket "+ticket, Attr{})
		c.ed.SetDoc(d)
		before := len(fs.called("chat.command"))
		c.submit()
		for end := time.Now().Add(5 * time.Second); len(fs.called("chat.command")) == before && time.Now().Before(end); {
			time.Sleep(10 * time.Millisecond)
		}
		cmds := fs.called("chat.command")
		if len(cmds) != before+1 {
			t.Fatalf("thread %q: %d chat.command calls, want 1", ts, len(cmds)-before)
		}
		p := cmds[before]
		tok := p.Get("client_token")
		// executeCommand: the ticket goes out as the arguments' blocks, with no text
		tj, _ := json.Marshal(ticket)
		wantBlocks := `[{"type":"rich_text","elements":[{"type":"rich_text_section","elements":[{"type":"text","text":` + string(tj) + `}]}]}]`
		if p.Get("command") != "/slackauthticket" || p.Get("disp") != "/slackauthticket" || p.Has("text") || p.Get("blocks") != wantBlocks ||
			p.Get("channel") != tChGen || p.Get("thread_ts") != ts || !strings.HasPrefix(tok, "web-") {
			t.Errorf("thread %q: chat.command %v", ts, p)
		}
		if !c.ed.Doc().Empty() {
			t.Errorf("thread %q: the composer kept the command", ts)
		}

		// Slack's modal for the command arrives for that token. Its blocks
		// are Slack's (never seen live), so this one is made up.
		b, _ := json.Marshal(map[string]any{"type": "view_opened", "view_id": "V0CLI", "view_type": "modal", "client_token": tok,
			"timeout_range": 10000, "view": map[string]any{"id": "V0CLI", "type": "modal",
				"title": map[string]any{"type": "plain_text", "text": "Synthetic"}, "blocks": []any{}}})
		app.Inject(slack.Event{Type: "view_opened", Raw: b})
		timeout := time.After(2 * time.Second)
	wait:
		for {
			select {
			case ev := <-views:
				if ev.Kind != "opened" {
					continue
				}
				if ev.View == nil || ev.View.ID != "V0CLI" || ev.ClientToken != tok {
					t.Errorf("thread %q: opened %+v", ts, ev)
				}
				break wait
			case <-timeout:
				t.Fatalf("thread %q: the command's modal didn't open", ts)
			}
		}
	}
	time.Sleep(100 * time.Millisecond) // the commands' answers
	if n := len(fs.called("chat.postMessage")); n != 0 {
		t.Errorf("posted %d messages", n)
	}
	fs.mu.Lock()
	for _, c := range fs.calls {
		if c.Method == "chat.command" {
			continue
		}
		for k, vs := range c.Form {
			for _, v := range vs {
				if strings.Contains(v, ticket) {
					t.Errorf("%s sent the ticket in %s", c.Method, k)
				}
			}
		}
	}
	fs.mu.Unlock()
	if s := logged.String(); strings.Contains(s, ticket[:16]) {
		t.Errorf("the log has the ticket:\n%s", s)
	}
}
