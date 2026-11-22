package views

// FileShares (file_shares.go) against fakeslack's #email-inbox: the
// loading pills, the conversations' cards and their click, the count
// style, and the empty state.

import (
	"image"
	"net/url"
	"strings"
	"sync"
	"testing"

	"gioui.org/layout"

	"github.com/some-du6e/clack/internal/core"
	"github.com/some-du6e/clack/internal/fakeslack"
	"github.com/some-du6e/clack/internal/slack"
	"github.com/some-du6e/clack/internal/store"
	"github.com/some-du6e/clack/internal/ui/theme"
)

// fsHost records the threads a card opens.
type fsHost struct {
	*vaHost
	mu      sync.Mutex
	threads []string
}

func (h *fsHost) OpenThread(ch, ts string) {
	h.mu.Lock()
	h.threads = append(h.threads, ch+"/"+ts)
	h.mu.Unlock()
}

// drawnTexts runs a frame and returns the strings it drew.
func drawnTexts(e *vaEnv) []string {
	var mu sync.Mutex
	var out []string
	prev := theme.DrawHook
	theme.DrawHook = func(s string) { mu.Lock(); out = append(out, s); mu.Unlock() }
	e.frame()
	theme.DrawHook = prev
	mu.Lock()
	defer mu.Unlock()
	return out
}

func hasText(ss []string, s string) bool {
	for _, x := range ss {
		if x == s {
			return true
		}
	}
	return false
}

// Review of karim-1118: the email FileDetails had no Shared in section.
// Slack's FileShareContext (2TKv) draws MissingText pills while
// files.getShares runs, then "Conversations {count}" over a card per
// share (0MIe: "Thread in #email-inbox") whose click opens the thread at
// the message; files_info_pane_count_style on bolds the word alone; a
// file with no references gets "No references yet", at least 70% of the
// window tall.
func TestFileSharesStates(t *testing.T) {
	sz := fakeslack.Small
	sz.Emails = true
	fs, err := fakeslack.Start(sz)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = fs.Close() })
	c := fs.EmailsCases()[0]
	release := make(chan struct{})
	var once sync.Once
	t.Cleanup(func() { once.Do(func() { close(release) }) })
	fs.SetHook(func(method string, f url.Values) (any, bool) {
		if method != "files.getShares" {
			return nil, false
		}
		switch f.Get("file_id") {
		case "F0NOREFS":
			none := map[string]any{"count": 0, "shares": map[string]any{}}
			return map[string]any{"ok": true, "conversation_shares": none, "tab_shares": none, "file_channel_shares": none}, true
		case c.FileID:
			<-release // fakeslack's answer once released
		}
		return nil, false
	})
	s := store.New("")
	s.PutUsers([]slack.User{{ID: fakeslack.SelfID, Name: "self", RealName: "Test Self"}})
	s.PutConv(slack.Conversation{ID: fakeslack.EmailsChannelID, Name: "email-inbox", IsChannel: true, IsMember: true, NumMembers: 4})
	s.SetLatest(fakeslack.EmailsChannelID, fs.EmailsHistory(), false)
	app := core.New(slack.New(fs.Creds()), s)
	app.ServeOffline()
	h := &fsHost{vaHost: &vaHost{slickHost: &slickHost{app: app, th: theme.New(false)}}}

	v := NewFileShares(h, c.FileID)
	var d layout.Dimensions
	e := &vaEnv{size: image.Pt(420, 900)}
	e.draw = func(gtx layout.Context) { d = v.Layout(gtx, 900) }
	e.until(t, "files.getShares", func() bool { return fs.CallCount("files.getShares") > 0 })
	if got := drawnTexts(e); len(got) != 0 || d.Size.Y != 16+16+8+8+4*22+16 {
		t.Errorf("loading drew %q, %dpx tall; want no text, the pills' 120px and 16px all round", got, d.Size.Y)
	}
	once.Do(func() { close(release) })
	e.until(t, "the shares", func() bool { _, done := v.Shares(); return done })
	got := drawnTexts(e)
	if !hasText(got, "Conversations 1") || !hasText(got, "Thread in #email-inbox") {
		t.Fatalf("the shares drew %q, want \"Conversations 1\" over the #email-inbox card", got)
	}
	e.click(image.Pt(200, d.Size.Y-16-12-14)) // in the card, above its padding
	h.mu.Lock()
	threads := strings.Join(h.threads, " ")
	h.mu.Unlock()
	if threads != fakeslack.EmailsChannelID+"/"+c.TS {
		t.Errorf("the card opened %q, want the thread at %s/%s", threads, fakeslack.EmailsChannelID, c.TS)
	}
	app.SetExperimentGroup(core.ExpFilesInfoPaneCountStyle, "on")
	if got := drawnTexts(e); !hasText(got, "Conversations") || hasText(got, "Conversations 1") {
		t.Errorf("files_info_pane_count_style on drew %q, want the word alone in bold", got)
	}

	v = NewFileShares(h, "F0NOREFS")
	e.until(t, "no shares", func() bool { _, done := v.Shares(); return done })
	got = drawnTexts(e)
	if !hasText(got, "No references yet") || !hasText(got, "When this file appears in a channel or conversation, it’ll also appear here.") {
		t.Errorf("the empty state drew %q", got)
	}
	if d.Size.Y < 900*70/100 {
		t.Errorf("the empty state is %dpx tall, want at least 70%% of the window's 900", d.Size.Y)
	}
}
