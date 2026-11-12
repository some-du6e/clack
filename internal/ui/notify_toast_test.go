package ui

import (
	"encoding/json"
	"encoding/xml"
	"io"
	"net/url"
	"os"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/some-du6e/clack/internal/core"
	"github.com/some-du6e/clack/internal/ui/huddleui"
)

// The Windows toast against Slack's own code. testdata/toast_slack.json
// holds sample payloads and what Slack desktop's shell made of them: gX,
// ch, buildNotificationId and EH.createToastNotification's toastXml
// (windows_com_activation on), each cut out of main.bundle.cjs 4.53.276
// win32 (and boot-shared HAag) and run in node.

type toastSlackData struct {
	Samples struct {
		Toasts map[string]struct {
			State struct {
				Workspaces map[string]struct {
					Name string `json:"name"`
				} `json:"workspaces"`
			} `json:"state"`
			Payload struct {
				Title       string `json:"title"`
				Body        string `json:"body"`
				Content     string `json:"content"`
				TeamID      string `json:"teamId"`
				Channel     string `json:"channel"`
				ChannelName string `json:"channelName"`
				Msg         string `json:"msg"`
				ThreadTS    string `json:"thread_ts"`
				ID          string `json:"id"`
				GroupID     string `json:"groupId"`
				GroupTitle  string `json:"groupTitle"`
				Avatar      string `json:"avatarImage"`
				TraceID     string `json:"trace_id"`
				Grouped     bool   `json:"groupWindowsNotifications"`
				HasReply    *bool  `json:"hasReply"`
				Buttons     []struct {
					Content   string `json:"content"`
					LaunchURI string `json:"launchUri"`
				} `json:"actionButtons"`
			} `json:"payload"`
		} `json:"toasts"`
		Truncate map[string][]json.RawMessage `json:"truncate"`
		IDs      map[string][]string          `json:"ids"`
	} `json:"samples"`
	Slack struct {
		XML      map[string]string `json:"xml"`
		Truncate map[string]string `json:"truncate"`
		IDs      map[string]string `json:"ids"`
	} `json:"slack"`
}

func loadToastSlack(t *testing.T) toastSlackData {
	t.Helper()
	b, err := os.ReadFile("testdata/toast_slack.json")
	if err != nil {
		t.Fatal(err)
	}
	var d toastSlackData
	if err := json.Unmarshal(b, &d); err != nil {
		t.Fatal(err)
	}
	return d
}

// TestToastXMLMatchesSlack: Clack's toast XML for each payload is the
// shell's, byte for byte, and parses as XML.
func TestToastXMLMatchesSlack(t *testing.T) {
	d := loadToastSlack(t)
	if len(d.Samples.Toasts) < 6 {
		t.Fatalf("%d samples", len(d.Samples.Toasts))
	}
	for name, s := range d.Samples.Toasts {
		p := s.Payload
		body := p.Body
		if body == "" {
			body = p.Content
		}
		n := &desktopNote{ID: p.ID, GroupID: p.GroupID, GroupTitle: p.GroupTitle, Title: p.Title, Body: body, Team: p.TeamID,
			Workspace: s.State.Workspaces[p.TeamID].Name, Channel: p.Channel, ChannelName: p.ChannelName, Msg: p.Msg,
			ThreadTS: p.ThreadTS, TraceID: p.TraceID, Grouped: p.Grouped, HasReply: p.Channel != ""}
		if p.HasReply != nil {
			n.HasReply = *p.HasReply // gX: hasReply, else !!channel
		}
		for _, b := range p.Buttons {
			n.Buttons = append(n.Buttons, noteButton{Content: b.Content, LaunchURI: b.LaunchURI})
		}
		got, want := n.toastXML(p.Avatar), d.Slack.XML[name]
		if got != want {
			t.Errorf("%s:\n got %s\nwant %s", name, got, want)
		}
		dec := xml.NewDecoder(strings.NewReader(got))
		for {
			if _, err := dec.Token(); err == io.EOF {
				break
			} else if err != nil {
				t.Errorf("%s: not XML: %v", name, err)
				break
			}
		}
	}
}

// TestToastTruncateMatchesSlack: the shell's ch (words, the cap, UTF-16
// units, a cut through a surrogate pair).
func TestToastTruncateMatchesSlack(t *testing.T) {
	d := loadToastSlack(t)
	for name, args := range d.Samples.Truncate {
		var s string
		var lim, cap int
		_ = json.Unmarshal(args[0], &s)
		_ = json.Unmarshal(args[1], &lim)
		_ = json.Unmarshal(args[2], &cap) // null: no cap
		if got, want := shellTruncate(s, lim, cap), d.Slack.Truncate[name]; got != want {
			t.Errorf("%s: %q, want %q", name, got, want)
		}
	}
}

// TestNotificationIDMatchesSlack: buildNotificationId, with its digest
// past 64 characters, and parseNotificationId reading it back.
func TestNotificationIDMatchesSlack(t *testing.T) {
	d := loadToastSlack(t)
	for name, args := range d.Samples.IDs {
		got := buildNotificationID(args[0], args[1], args[2:]...)
		if want := d.Slack.IDs[name]; got != want {
			t.Errorf("%s: %q, want %q", name, got, want)
		}
		team, typ, payload, truncated := parseNotificationID(got)
		if team != args[0] || typ != args[1] {
			t.Errorf("%s: parsed %q %q", name, team, typ)
		}
		if truncated != strings.Contains(got, "~") || (!truncated && strings.Join(payload, "|") != strings.Join(args[2:], "|")) {
			t.Errorf("%s: parsed payload %v truncated %t", name, payload, truncated)
		}
	}
}

// toastAttrs is every attribute named attr on elem in a toast's XML, as
// Windows hands it back (unescaped).
func toastAttrs(t *testing.T, x, elem, attr string) []string {
	t.Helper()
	var out []string
	dec := xml.NewDecoder(strings.NewReader(x))
	for {
		tok, err := dec.Token()
		if err == io.EOF {
			return out
		}
		if err != nil {
			t.Fatalf("toast XML: %v", err)
		}
		if se, ok := tok.(xml.StartElement); ok && se.Name.Local == elem {
			for _, a := range se.Attr {
				if a.Name.Local == attr {
					out = append(out, a.Value)
				}
			}
		}
	}
}

// TestToastActivationArgs: handleToastActivation's payloads read back
// from Slack's own toasts, and the ones its schemas turn away.
func TestToastActivationArgs(t *testing.T) {
	d := loadToastSlack(t)
	launch := toastAttrs(t, d.Slack.XML["reply"], "toast", "launch")
	a, ok := parseToastActivation(launch[0], "")
	if !ok || a.Type != "notification" || a.Team != "T0FAKE0001" || a.Channel != "D0FAKEDM001" || a.MessageID != "1762520100.000400" ||
		a.ThreadTS != "1762520099.000300" || a.NotificationID != "T0FAKE0001|msg|D0FAKEDM001|1762520100.000400|1762520099.000300" {
		t.Errorf("launch: %+v %t", a, ok)
	}
	reply := toastAttrs(t, d.Slack.XML["reply"], "action", "arguments")
	if a, ok := parseToastActivation(reply[0], "on my way"); !ok || a.Reply != "on my way" || a.Channel != "D0FAKEDM001" {
		t.Errorf("the reply button: %+v %t", a, ok)
	}
	acts := toastAttrs(t, d.Slack.XML["huddle"], "action", "arguments")
	if len(acts) != 2 || acts[0] != "dismiss" {
		t.Fatalf("huddle actions %q", acts)
	}
	if a, ok := parseToastActivation(acts[1], ""); !ok || a.Type != "action" || a.Index != 1 ||
		a.LaunchURI != "slack://huddle?id=D0FAKEDM001&team=T0FAKE0001" || a.ID != "T0FAKE0001_huddle_D0FAKEDM001_29541168" {
		t.Errorf("Join: %+v %t", a, ok)
	}
	hdr := toastAttrs(t, d.Slack.XML["channel"], "header", "arguments")
	if a, ok := parseToastActivation(hdr[0], ""); !ok || a.Type != "header" || a.LaunchURI != "slack://channel?team=T0FAKE0001" {
		t.Errorf("header: %+v %t", a, ok)
	}
	for _, bad := range []string{
		``, `dismiss`, `[]`, `{"type":"nope"}`,
		`{"type":"notification","teamId":"T","channel":"C","messageId":"1.2","notificationId":"n"}`,                    // no traceId
		`{"type":"notification","teamId":"","channel":"C","messageId":"1.2","notificationId":"n","traceId":""}`,        // empty teamId
		`{"type":"notification","teamId":"T","channel":"C","messageId":"1.2","notificationId":"n","traceId":"","x":1}`, // another key
		`{"type":"notification","teamId":"T","channel":"C","messageId":"1.2","notificationId":"n","traceId":"","threadTimestamp":""}`,
		`{"type":"action","launchUri":"slack://x","id":"n","index":-1}`,
		`{"type":"action","launchUri":"slack://x","id":"n","index":1.5}`,
		`{"type":"action","launchUri":"","id":"n"}`,
		`{"type":"header","launchUri":"slack://x","id":"n"}`,
	} {
		if a, ok := parseToastActivation(bad, ""); ok {
			t.Errorf("%s: read as %+v", bad, a)
		}
	}
	if a, ok := parseToastActivation(`{"type":"action","launchUri":"slack://x","id":"n"}`, ""); !ok || a.Index != -1 {
		t.Errorf("an action with no index: %+v %t", a, ok)
	}
}

// storeTeam is the signed-in team's id as the notes read it.
func storeTeam(ca *core.App) string {
	ca.S.RLock()
	defer ca.S.RUnlock()
	return ca.S.TeamID
}

// noteRec records the notes a noteNotifier gets.
type noteRec struct {
	mu    sync.Mutex
	notes []*desktopNote
}

func (r *noteRec) Notify(title, body string) error {
	return r.NotifyNote(&desktopNote{Title: title, Body: body})
}
func (r *noteRec) NotifyActions(title, body string, _ []string, _ func(string)) error {
	return r.Notify(title, body)
}
func (r *noteRec) NotifyNote(n *desktopNote) error {
	r.mu.Lock()
	r.notes = append(r.notes, n)
	r.mu.Unlock()
	return nil
}

func (r *noteRec) wait(t *testing.T, a *App, n int) *desktopNote {
	t.Helper()
	deadline := time.Now().Add(10 * time.Second)
	for {
		a.drain()
		r.mu.Lock()
		got := len(r.notes)
		var last *desktopNote
		if got > 0 {
			last = r.notes[got-1]
		}
		r.mu.Unlock()
		if got >= n {
			return last
		}
		if time.Now().After(deadline) {
			t.Fatalf("%d notifications, want %d", got, n)
		}
		time.Sleep(10 * time.Millisecond)
	}
}

// click is the toast's activation as Windows would hand it back: the
// toast's launch arguments, a button's, or the header's.
func click(t *testing.T, a *App, n *desktopNote, elem string, i int) {
	t.Helper()
	attr := "arguments"
	if elem == "toast" {
		attr = "launch"
	}
	args := toastAttrs(t, n.toastXML(""), elem, attr)
	if i >= len(args) {
		t.Fatalf("the toast has %d %s elements: %s", len(args), elem, n.toastXML(""))
	}
	act, ok := parseToastActivation(args[i], "")
	if !ok {
		t.Fatalf("%s %d: arguments %q don't parse", elem, i, args[i])
	}
	n.Activated(act)
	a.drain()
}

// TestToastClickOpensConversation: a message's toast carries Slack's
// payload, and a click on it opens the conversation (a reply's, its
// thread); the header only raises the window.
func TestToastClickOpensConversation(t *testing.T) {
	a, ca, _ := fakeApp(t, map[string]any{"mute_sounds": true})
	rec := &noteRec{}
	a.notify = rec
	team := storeTeam(ca)
	msg := &core.Message{TS: "1762520099.000300", User: "U0FAKEUSER1", Name: "Ada", Text: "hello"}
	a.data.handle(a, core.Event{Type: "message", Channel: "D0NOTE0001", TS: msg.TS, ConvKind: "im", Msg: msg})
	n := rec.wait(t, a, 1)
	if n.Channel != "D0NOTE0001" || n.Msg != msg.TS || !n.HasReply || n.Title != "New message from Ada" || n.Body != "hello" {
		t.Errorf("the note: %+v", n)
	}
	if n.ID != team+"_"+msg.TS || n.GroupID != "" {
		t.Errorf("unassigned desktop_notification_actions: id %q group %q, want the shell's %q", n.ID, n.GroupID, team+"_"+msg.TS)
	}
	click(t, a, n, "header", 0)
	if r := a.router.Current(); r.Channel() == "D0NOTE0001" {
		t.Errorf("the header opened %s", r)
	}
	click(t, a, n, "toast", 0)
	if r := a.router.Current(); r.Channel() != "D0NOTE0001" || r.Thread() != "" {
		t.Errorf("a click on the toast opened %q, want the DM", r)
	}

	// a reply in a followed thread (desktop_notification): its thread
	ca.SetExperimentGroup("desktop_notification_actions", "on")
	reply := &core.Message{TS: "1762520100.000400", ThreadTS: "1762520099.000300", User: "U0FAKEUSER1", Name: "Ada", Text: "<!here> look"}
	a.data.handle(a, core.Event{Type: "message", Channel: "C0NOTE0001", TS: reply.TS, ConvKind: "channel", Msg: reply, Reply: true, Mention: true})
	n = rec.wait(t, a, 2)
	if want := buildNotificationID(team, "msg", "C0NOTE0001", reply.TS, reply.ThreadTS); n.ID != want || n.GroupID != buildNotificationID(team, "msg", "C0NOTE0001") {
		t.Errorf("desktop_notification_actions on: id %q group %q, want %q", n.ID, n.GroupID, want)
	}
	click(t, a, n, "toast", 0)
	if r := a.router.Current(); r.Channel() != "C0NOTE0001" || r.Thread() != reply.ThreadTS {
		t.Errorf("a click on the reply's toast opened %q, want its thread", r)
	}
}

// TestToastReplySends: the reply box sends to the conversation, in the
// thread for a reply's toast (delegate_replyToNotification), and leaves
// the window where it was.
func TestToastReplySends(t *testing.T) {
	a, _, fs := fakeApp(t, map[string]any{"mute_sounds": true})
	posted := make(chan url.Values, 4)
	fs.OnPost = func(f url.Values, _ map[string]any) { posted <- f }
	rec := &noteRec{}
	a.notify = rec
	reply := &core.Message{TS: "1762520100.000400", ThreadTS: "1762520099.000300", User: "U0FAKEUSER1", Name: "Ada", Text: "<!here> look"}
	a.data.handle(a, core.Event{Type: "message", Channel: "C0NOTE0001", TS: reply.TS, ConvKind: "channel", Msg: reply, Reply: true, Mention: true})
	n := rec.wait(t, a, 1)
	before := a.router.Current()
	args := toastAttrs(t, n.toastXML(""), "action", "arguments")
	if len(args) != 1 {
		t.Fatalf("a message's toast has %d actions, want the reply's ➤", len(args))
	}
	act, ok := parseToastActivation(args[0], "on my way")
	if !ok {
		t.Fatalf("the reply's arguments: %q", args[0])
	}
	n.Activated(act)
	a.drain()
	select {
	case f := <-posted:
		if f.Get("channel") != "C0NOTE0001" || f.Get("thread_ts") != reply.ThreadTS || f.Get("text") != "on my way" {
			t.Errorf("posted %v", f)
		}
		if !strings.Contains(f.Get("blocks"), `"on my way"`) {
			t.Errorf("blocks %s", f.Get("blocks"))
		}
	case <-time.After(10 * time.Second):
		t.Fatal("the reply wasn't sent")
	}
	if r := a.router.Current(); r != before {
		t.Errorf("the reply moved the window to %q", r)
	}
}

// TestToastHuddleInvite: an invite's toast has no title, Slack's buttons
// (Dismiss, Join; Decline too under desktop_notification_actions, as a
// second dismiss), and Join joins.
func TestToastHuddleInvite(t *testing.T) {
	a, ca, _ := fakeApp(t, map[string]any{"mute_sounds": true})
	rec := &noteRec{}
	a.notify = rec
	team := storeTeam(ca)
	joined := 0
	inv := func(buttons []huddleui.NotifyAction) huddleui.Notification {
		return huddleui.Notification{Title: "Fake Workspace", Body: "Ada is inviting you to a huddle", PlainBody: "Ada is inviting you to a huddle",
			Channel: "D0NOTE0001", User: "U0FAKEUSER1", Call: "R0FAKECALL1", Buttons: buttons}
	}
	join := func() { joined++ }
	two := []huddleui.NotifyAction{{Key: "Dismiss", Label: "Dismiss"}, {Key: "Join", Label: "Join", URI: "slack://huddle?id=D0NOTE0001&team=" + team, Do: join}}
	a.notifyActions(inv(two))
	n := rec.wait(t, a, 1)
	if n.Title != "" || n.Body != "Ada is inviting you to a huddle" || n.HasReply || !strings.HasPrefix(n.Msg, "huddle_D0NOTE0001_") || n.ID != team+"_"+n.Msg {
		t.Errorf("the invite's note: %+v", n)
	}
	x := n.toastXML("")
	if got := toastAttrs(t, x, "action", "content"); strings.Join(got, ",") != "Dismiss,Join" {
		t.Errorf("buttons %v", got)
	}
	if got := toastAttrs(t, x, "action", "activationType"); strings.Join(got, ",") != "system,foreground" {
		t.Errorf("activation types %v", got)
	}
	click(t, a, n, "action", 1)
	if joined != 1 {
		t.Errorf("Join ran %d times", joined)
	}
	click(t, a, n, "toast", 0)
	if r := a.router.Current(); r.Channel() != "D0NOTE0001" {
		t.Errorf("a click on the invite opened %q, want its conversation", r)
	}

	ca.SetExperimentGroup("desktop_notification_actions", "on")
	three := append(two, huddleui.NotifyAction{Key: "Decline", Label: "Decline"})
	a.notifyActions(inv(three))
	n = rec.wait(t, a, 2)
	if want := buildNotificationID(team, "huddle", "D0NOTE0001", "R0FAKECALL1"); n.ID != want {
		t.Errorf("id %q, want %q", n.ID, want)
	}
	if got := toastAttrs(t, n.toastXML(""), "action", "arguments"); len(got) != 3 || got[2] != "dismiss" {
		t.Errorf("Decline is Windows' dismiss: %v", got)
	}
}
