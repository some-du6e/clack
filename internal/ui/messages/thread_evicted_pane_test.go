package messages

// Karim's #help thread (2026-11-24 22:31Z), the pane he saw: the thread
// read once, a bot's two "Only visible to you" replies held, then the
// message cache let the thread's root and replies go while #help's window
// kept the root. The pane opened again, from the reply bar or by a jump to
// a reply, must draw the root, its "N replies" divider, the 4 replies and
// the ephemerals in time order (Slack's thread history keeps every
// timestamp, its rows fetch what the store lost by id and read the root
// from the one store). It drew the ephemerals alone, then the replies
// without the root. Synthetic data on internal/fakeslack.

import (
	"fmt"
	"strings"
	"testing"
	"time"

	"github.com/some-du6e/clack/internal/core"
	"github.com/some-du6e/clack/internal/fakeslack"
	"github.com/some-du6e/clack/internal/slack"
	"github.com/some-du6e/clack/internal/store"
	"github.com/some-du6e/clack/internal/ui/theme"
)

// karimThread builds the state: ch's thread root with 4 replies, read
// whole, 2 ephemerals held, then evicted (the window keeps the root).
func karimThread(t *testing.T, app *core.App, fs *fakeslack.Server) (ch, root string, at func(int) string) {
	t.Helper()
	ch = fs.ChannelIDs()[1]
	base := time.Now().Add(-26 * time.Hour).Unix()
	at = func(i int) string { return fmt.Sprintf("%d.000100", base+int64(i)) }
	root = at(0)
	fs.PostAtQuiet(ch, root, "U0FAKE00001", "synthetic question")
	rootMsg := slack.Message{Type: "message", TS: root, ThreadTS: root, User: "U0FAKE00001", Text: "synthetic question",
		ReplyCount: 4, ReplyUsers: []string{"U0FAKE00002"}, LatestReply: at(4)}
	msgs := []slack.Message{rootMsg}
	for i := 1; i <= 4; i++ {
		text := fmt.Sprintf("synthetic answer %d", i)
		fs.ReplyAtQuiet(ch, root, at(i), "U0FAKE00002", text)
		msgs = append(msgs, slack.Message{Type: "message", TS: at(i), ThreadTS: root, User: "U0FAKE00002", Text: text})
	}
	app.S.SetThread(ch, root, msgs)
	app.S.SetLatest(ch, []slack.Message{rootMsg}, false)
	for i := 5; i <= 6; i++ { // the bot's two ephemerals, over the socket
		app.S.AddReply(ch, slack.Message{Type: "message", TS: at(i), ThreadTS: root, User: "U0FAKE00002",
			Text: fmt.Sprintf("synthetic only visible to you %d", i), IsEphemeral: true})
	}
	big := make([]slack.Message, 3500)
	for i := range big {
		big[i] = slack.Message{TS: fmt.Sprintf("%d.000100", base+3600+int64(i)), User: "U0FAKE00001", Text: "synthetic filler"}
	}
	app.S.SetLatest("C0FILLER01", big, true)
	app.S.EvictMessages(store.EvictInput{Now: time.Now(), Accessed: map[string]time.Time{}, Keep: map[string]bool{}, Pending: map[string]bool{},
		Conv:    func(string) (bool, bool) { return true, true },
		WholeOK: func(string) bool { return false },
		LastRead: func(c string) string {
			if c == ch {
				return "0000000000.000000" // #help's window unread: it keeps the root
			}
			return fmt.Sprintf("%d.000100", time.Now().Unix())
		},
	})
	if th, _ := app.S.Thread(ch, root); len(th) != 2 {
		t.Fatalf("control: the evicted thread's cache holds %d messages, want the 2 ephemerals", len(th))
	}
	return ch, root, at
}

// paneRows is the pane's list as Slack draws it: each message row's ts,
// "replies" for the "N replies" divider.
func paneRows(tv *ThreadView) string {
	var out []string
	for _, it := range tv.s.items {
		switch it.kind {
		case itMsg:
			out = append(out, it.key)
		case itReplies:
			out = append(out, "replies")
		}
	}
	return strings.Join(out, " ")
}

func TestThreadPaneAfterEvictionLikeSlack(t *testing.T) {
	for _, jump := range []bool{false, true} {
		name := "from the reply bar"
		if jump {
			name = "a jump to a reply"
		}
		t.Run(name, func(t *testing.T) {
			app, fs, _ := shotsApp(t)
			ch, root, at := karimThread(t, app, fs)
			if jump {
				SetThreadStart(app, ch, root, at(3)) // Activity, search or a notification
			}
			tv := NewThreadView(coreHost{app: app, th: theme.New(false)}, ch, root)
			want := strings.Join([]string{root, "replies", at(1), at(2), at(3), at(4), at(5), at(6)}, " ")
			f := frameFn(tv)
			deadline := time.Now().Add(10 * time.Second)
			for paneRows(tv) != want && time.Now().Before(deadline) {
				f()
				time.Sleep(10 * time.Millisecond)
			}
			if got := paneRows(tv); got != want {
				t.Errorf("the pane draws %q\nwant the root, its divider, the 4 replies and the 2 ephemerals: %q", got, want)
			}
		})
	}
}
