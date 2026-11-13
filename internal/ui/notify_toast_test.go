package ui

import (
	"context"
	"encoding/json"
	"encoding/xml"
	"io"
	"os"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/some-du6e/clack/internal/core"
	"github.com/some-du6e/clack/internal/fakeslack"
	"github.com/some-du6e/clack/internal/richtext"
	"github.com/some-du6e/clack/internal/ui/huddleui"
)

// The Windows toast. testdata/toast_slack.json holds sample payloads and
// what Slack desktop's shell made of them: gX, ch, buildNotificationId and
// EH.createToastNotification's toastXml (windows_com_activation on), each
// cut out of main.bundle.cjs 4.53.276 win32 (and boot-shared HAag) and run
// in node. Clack's toast now takes Chrome's layout instead (Karim,
// 2026-11-12), from Chromium's notification_template_builder.cc; Slack's
// XML still supplies the activation payloads Clack reads back.

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

// chromeToast is Chromium's BuildNotificationTemplate output (its
// unittests' kExpectedXml, without libxml's indentation) for a toast with
// the title, the body, an attribution line, Clack's icon as the logo,
// buttons and silent audio.
func chromeToast(launch, stamp, title, body, attribution, logo, actions string) string {
	x := `<toast launch="` + launch + `"`
	if stamp != "" {
		x += ` displayTimestamp="` + stamp + `"`
	}
	x += `><visual><binding template="ToastGeneric"><text>` + title + `</text><text>` + body + `</text>`
	if attribution != "" {
		x += `<text placement="attribution">` + attribution + `</text>`
	}
	if logo != "" {
		x += `<image placement="appLogoOverride" src="` + logo + `" hint-crop="none"/>`
	}
	if actions == "" {
		x += `</binding></visual><actions/>`
	} else {
		x += `</binding></visual><actions>` + actions + `</actions>`
	}
	return x + `<audio silent="true"/></toast>`
}

// TestToastXMLChromeLayout: a message's toast is Chrome's: the title as
// the webapp wrote it ("New message in #general", not the channel's name),
// the body, the workspace where Chrome has the origin, Clack's icon as an
// uncropped logo, the time; no header, no reply box, no line limits. A
// huddle invite keeps its buttons, and every toast parses as XML.
func TestToastXMLChromeLayout(t *testing.T) {
	d := loadToastSlack(t)
	when := time.Date(2026, 11, 12, 18, 4, 5, 0, time.FixedZone("x", 3600))
	note := func(name string) *desktopNote {
		s := d.Samples.Toasts[name]
		p := s.Payload
		body := p.Body
		if body == "" {
			body = p.Content
		}
		n := &desktopNote{ID: p.ID, GroupID: p.GroupID, Title: p.Title, Body: body, Team: p.TeamID,
			Workspace: s.State.Workspaces[p.TeamID].Name, Channel: p.Channel, Msg: p.Msg, ThreadTS: p.ThreadTS, TraceID: p.TraceID, When: when}
		for _, b := range p.Buttons {
			n.Buttons = append(n.Buttons, noteButton{Content: b.Content, LaunchURI: b.LaunchURI})
		}
		return n
	}
	// the launch arguments are Slack's (EH's toast), escaped once more for the attribute
	launch := func(name string) string { return toastAttrsRaw(d.Slack.XML[name], "launch") }
	logo := "C:/Users/ci/AppData/Local/Temp/Clack Notification Cache/clack.png"

	ch := note("channel")
	if got, want := ch.toastXML(logo), chromeToast(launch("channel"), "2026-11-12T17:04:05Z", "New message in #general",
		"Ada: hello &lt;world&gt; &amp; &quot;friends&quot; it&apos;s", "Fake Workspace", logo, ""); got != want {
		t.Errorf("channel:\n got %s\nwant %s", got, want)
	}
	reply := note("reply")
	if got, want := reply.toastXML(""), chromeToast(launch("reply"), "2026-11-12T17:04:05Z", "New message from Ada",
		"a reply &amp; more", "Fake &amp; Co", "", ""); got != want {
		t.Errorf("reply:\n got %s\nwant %s", got, want)
	}
	hud := note("huddle")
	join := `{&quot;type&quot;:&quot;action&quot;,&quot;launchUri&quot;:&quot;slack://huddle?id=D0FAKEDM001&amp;team=T0FAKE0001&quot;,&quot;id&quot;:&quot;T0FAKE0001_huddle_D0FAKEDM001_29541168&quot;,&quot;index&quot;:1}`
	if got, want := hud.toastXML(logo), chromeToast(launch("huddle"), "2026-11-12T17:04:05Z", "", "Ada is inviting you to a huddle",
		"Fake Workspace", logo, `<action activationType="system" content="Dismiss" arguments="dismiss"/>`+
			`<action activationType="foreground" content="Join" arguments="`+join+`"/>`); got != want {
		t.Errorf("huddle:\n got %s\nwant %s", got, want)
	}
	plain := note("plain")
	plain.When = time.Time{}
	if got, want := plain.toastXML(""), chromeToast(launch("plain"), "", "Clack", "plain", "", "", ""); got != want {
		t.Errorf("plain:\n got %s\nwant %s", got, want)
	}
	if got := toastAttrs(t, note("nonslack").toastXML(""), "action", "arguments"); len(got) != 1 || !strings.Contains(got[0], `"launchUri":"slack://noop"`) {
		t.Errorf("a non-slack: button: %q, want guardLaunchUri's slack://noop", got)
	}

	for name := range d.Samples.Toasts {
		x := note(name).toastXML(logo)
		for _, gone := range []string{"<header", "<input", "hint-maxLines", "hint-style", "hint-crop=\"circle\"", "activationType=\"foreground\" launch"} {
			if strings.Contains(x, gone) {
				t.Errorf("%s: Chrome's toast has no %s: %s", name, gone, x)
			}
		}
		if n := len(toastAttrs(t, x, "text", "")); n < 2 || n > 3 {
			t.Errorf("%s: %d <text>s, want the title, the body and at most the attribution", name, n)
		}
	}
}

// toastAttrsRaw is attr's first value on <toast> in x as written (still
// escaped).
func toastAttrsRaw(x, attr string) string {
	_, rest, _ := strings.Cut(x, " "+attr+`="`)
	v, _, _ := strings.Cut(rest, `"`)
	return v
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
// Windows hands it back (unescaped); attr "" lists each elem once.
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
			if attr == "" { // the element itself
				out = append(out, "")
				continue
			}
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
	a, ok := parseToastActivation(launch[0])
	if !ok || a.Type != "notification" || a.Team != "T0FAKE0001" || a.Channel != "D0FAKEDM001" || a.MessageID != "1762520100.000400" ||
		a.ThreadTS != "1762520099.000300" || a.NotificationID != "T0FAKE0001|msg|D0FAKEDM001|1762520100.000400|1762520099.000300" {
		t.Errorf("launch: %+v %t", a, ok)
	}
	acts := toastAttrs(t, d.Slack.XML["huddle"], "action", "arguments")
	if len(acts) != 2 || acts[0] != "dismiss" {
		t.Fatalf("huddle actions %q", acts)
	}
	if a, ok := parseToastActivation(acts[1]); !ok || a.Type != "action" || a.Index != 1 ||
		a.LaunchURI != "slack://huddle?id=D0FAKEDM001&team=T0FAKE0001" || a.ID != "T0FAKE0001_huddle_D0FAKEDM001_29541168" {
		t.Errorf("Join: %+v %t", a, ok)
	}
	// Clack's toast has no header; its arguments no longer parse
	hdr := toastAttrs(t, d.Slack.XML["channel"], "header", "arguments")
	if a, ok := parseToastActivation(hdr[0]); ok {
		t.Errorf("header: read as %+v", a)
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
		if a, ok := parseToastActivation(bad); ok {
			t.Errorf("%s: read as %+v", bad, a)
		}
	}
	if a, ok := parseToastActivation(`{"type":"action","launchUri":"slack://x","id":"n"}`); !ok || a.Index != -1 {
		t.Errorf("an action with no index: %+v %t", a, ok)
	}
}

// storeTeam gives the store the signed-in team a booted session has
// (fakeApp doesn't boot), and returns its id: the toast needs one (the
// attribution line; handleToastActivation requires a teamId).
func storeTeam(ca *core.App) string {
	ca.S.Lock()
	defer ca.S.Unlock()
	if ca.S.TeamID == "" {
		ca.S.TeamID, ca.S.TeamName = "T0FAKETEAM", "Fake Workspace"
	}
	return ca.S.TeamID
}

// noteApp is fakeApp with the backend a signed-in session has (its caches
// and context: Navigate fetches the conversation's tabs and members from
// the fake), the store's team, and rec recording the notifications.
func noteApp(t *testing.T) (*App, *core.App, *fakeslack.Server, *noteRec) {
	t.Helper()
	a, ca, fs := fakeApp(t, map[string]any{"mute_sounds": true})
	b := newBackend(ca, "")
	b.app = a
	b.ctx, b.cancel = context.WithCancel(context.Background())
	t.Cleanup(b.cancel)
	a.data = b
	storeTeam(ca)
	rec := &noteRec{}
	a.notify = rec
	return a, ca, fs, rec
}

// noteMsg is a message from Ada with text as its body (the notification
// reads the parsed text, Msg.Doc).
func noteMsg(ts, thread, text string) *core.Message {
	return &core.Message{TS: ts, ThreadTS: thread, User: "U0FAKEUSER1", Name: "Ada", Text: text,
		Doc: richtext.Doc{Blocks: richtext.FromMrkdwn(noResolver{}, text)}}
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
// toast's launch arguments or a button's.
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
	act, ok := parseToastActivation(args[i])
	if !ok {
		t.Fatalf("%s %d: arguments %q don't parse", elem, i, args[i])
	}
	n.Activated(act)
	a.drain()
}

// TestToastClickOpensConversation: a message's toast carries Slack's
// payload, and a click on it opens the conversation (a reply's, its
// thread).
func TestToastClickOpensConversation(t *testing.T) {
	a, ca, _, rec := noteApp(t)
	team := storeTeam(ca)
	msg := noteMsg("1762520099.000300", "", "hello")
	a.data.handle(a, core.Event{Type: "message", Channel: "D0NOTE0001", TS: msg.TS, ConvKind: "im", Msg: msg})
	n := rec.wait(t, a, 1)
	if n.Channel != "D0NOTE0001" || n.Msg != msg.TS || n.Title != "New message from Ada" || n.Body != "hello" || n.Workspace != "Fake Workspace" {
		t.Errorf("the note: %+v", n)
	}
	x := n.toastXML("")
	if got := toastAttrs(t, x, "input", ""); len(got) != 0 {
		t.Errorf("a message's toast has %d inputs, want no reply box: %s", len(got), x)
	}
	if got := toastAttrs(t, x, "action", ""); len(got) != 0 {
		t.Errorf("a message's toast has %d actions, want none (Chrome's): %s", len(got), x)
	}
	if got := toastAttrs(t, x, "text", "placement"); len(got) != 1 || got[0] != "attribution" || !strings.Contains(x, `<text placement="attribution">Fake Workspace</text>`) {
		t.Errorf("the attribution line: %q in %s", got, x)
	}
	if n.ID != team+"_"+msg.TS || n.GroupID != "" {
		t.Errorf("unassigned desktop_notification_actions: id %q group %q, want the shell's %q", n.ID, n.GroupID, team+"_"+msg.TS)
	}
	click(t, a, n, "toast", 0)
	if r := a.router.Current(); r.Channel() != "D0NOTE0001" || r.Thread() != "" {
		t.Errorf("a click on the toast opened %q, want the DM", r)
	}

	// a reply in a followed thread (desktop_notification): its thread
	ca.SetExperimentGroup("desktop_notification_actions", "on")
	reply := noteMsg("1762520100.000400", "1762520099.000300", "<!here> look")
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

// TestToastHuddleInvite: an invite's toast has no title, Slack's buttons
// (Dismiss, Join; Decline too under desktop_notification_actions, as a
// second dismiss), and Join joins.
func TestToastHuddleInvite(t *testing.T) {
	a, ca, _, rec := noteApp(t)
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
	if n.Title != "" || n.Body != "Ada is inviting you to a huddle" || !strings.HasPrefix(n.Msg, "huddle_D0NOTE0001_") || n.ID != team+"_"+n.Msg {
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
