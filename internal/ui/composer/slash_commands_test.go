package composer

import (
	"context"
	"encoding/json"
	"net/url"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/some-du6e/clack/internal/core"
	"github.com/some-du6e/clack/internal/slack"
	"github.com/some-du6e/clack/internal/store"
	"github.com/some-du6e/clack/internal/ui/popups"
	"github.com/some-du6e/clack/internal/ui/theme"
)

// The composer's whole slash-command path, through submit() and the send
// job, against the fake API (synthetic workspace): what chat.command gets
// is what Slack's executeCommand sends (blocks of the parsed arguments,
// no text; the expected blocks are Slack's own convertDeltaToBlocks
// output for the same delta, slash_oracle_data_test.go's harness), the
// local commands read the parsed mentions, and messages Slack posts stay
// messages.

// cmdHost records what the command path asks of the composer's host.
type cmdHost struct {
	slickHost
	mu       sync.Mutex
	overlays []popups.Overlay
	invites  []string
}

func (h *cmdHost) ShowOverlay(o popups.Overlay) {
	h.mu.Lock()
	defer h.mu.Unlock()
	h.overlays = append(h.overlays, o)
}

func (h *cmdHost) OpenInviteToChannel(ch string, users []string) {
	h.mu.Lock()
	defer h.mu.Unlock()
	h.invites = append(h.invites, ch+":"+strings.Join(users, ","))
}

const (
	tBobby = "U0BOBBY0001" // a member-shaped id: /invite classifies mentions as Slack does
	tReply = "1700000000.000100"
)

// cmdComposer is a composer in #general (a thread's when ts is set) over
// the fake API, the store seeded with Bobby and seed. Drafts are
// process-global: the box's is cleared before, and after Close saves
// whatever a failed command put back.
func cmdComposer(t *testing.T, ts string, seed ...func(s *store.Store)) (*Composer, *cmdHost, *fakeSlack) {
	t.Helper()
	app, fs := syntheticApp(t, append([]func(*store.Store){func(s *store.Store) {
		s.PutUsers([]slack.User{{ID: tBobby, Name: "bobby", RealName: "Bobby Tables", Profile: slack.Profile{DisplayName: "bobby", RealName: "Bobby Tables"}}})
	}}, seed...)...)
	stubTyping(t)
	withTaut(t, nil)
	clearDraft(tChGen, ts)
	h := &cmdHost{slickHost: slickHost{app: app, th: theme.New(false)}}
	c := New(h, tChGen, ts)
	t.Cleanup(func() {
		c.Close()
		clearDraft(tChGen, ts)
	})
	return c, h, fs
}

// cmdDoc builds a document of text and atoms.
func cmdDoc(parts ...any) *Doc {
	d := NewDoc()
	pos := 0
	for _, p := range parts {
		switch p := p.(type) {
		case string:
			pos = d.Insert(pos, p, Attr{})
		case *Atom:
			pos = d.InsertAtom(pos, p)
		}
	}
	return d
}

// submitWait submits d and waits for n calls of method.
func submitWait(t *testing.T, c *Composer, fs *fakeSlack, d *Doc, method string, n int) []url.Values {
	t.Helper()
	before := len(fs.called(method))
	c.ed.SetDoc(d)
	c.submit()
	for end := time.Now().Add(5 * time.Second); len(fs.called(method)) < before+n && time.Now().Before(end); {
		time.Sleep(10 * time.Millisecond)
	}
	calls := fs.called(method)
	if len(calls) < before+n {
		t.Fatalf("%d %s calls, want %d", len(calls)-before, method, n)
	}
	return calls[before:]
}

var (
	selfChip  = &Atom{Kind: AtomUser, ID: tSelf, Text: "@self"}
	aliceChip = &Atom{Kind: AtomUser, ID: "UALICE", Text: "@alice"}
	bobbyChip = &Atom{Kind: AtomUser, ID: tBobby, Text: "@bobby"}
)

// TestSlashCommandSendsSlacksBlocks: chips and typed names alike go out
// as their members (Karim's /remind with his own chip mentioned someone
// else when the plain words went out), /shrug and /me are the server's
// commands (no chat.postMessage, /me keeps its thread), and the picked
// app command carries its app_id, the typed word as disp.
func TestSlashCommandSendsSlacksBlocks(t *testing.T) {
	cases := []struct {
		name, ts   string
		d          *Doc
		pick       core.Command
		cmd, disp  string
		appID      string
		blocks     string
		postsCount int
	}{
		{name: "self chip", d: cmdDoc("/remind ", selfChip, " to stretch in 5 minutes"), cmd: "/remind", disp: "/remind",
			blocks: `[{"type":"rich_text","elements":[{"type":"rich_text_section","elements":[{"type":"user","user_id":"USELF"},{"type":"text","text":" to stretch in 5 minutes"}]}]}]`},
		{name: "typed name", d: cmdDoc("/remind @alice tomorrow"), cmd: "/remind", disp: "/remind",
			blocks: `[{"type":"rich_text","elements":[{"type":"rich_text_section","elements":[{"type":"user","user_id":"UALICE"},{"type":"text","text":" tomorrow"}]}]}]`},
		{name: "shrug", d: cmdDoc("/shrug ", aliceChip), cmd: "/shrug", disp: "/shrug",
			blocks: `[{"type":"rich_text","elements":[{"type":"rich_text_section","elements":[{"type":"user","user_id":"UALICE"}]}]}]`},
		{name: "me in a thread", ts: tReply, d: cmdDoc("/me waves at ", aliceChip), cmd: "/me", disp: "/me",
			blocks: `[{"type":"rich_text","elements":[{"type":"rich_text_section","elements":[{"type":"text","text":"waves at "},{"type":"user","user_id":"UALICE"}]}]}]`},
		{name: "app command", d: cmdDoc("/Deploy prod"), pick: core.Command{Name: "/deploy", AppID: "A0DEPLOY01"}, cmd: "/deploy", disp: "/Deploy", appID: "A0DEPLOY01",
			blocks: `[{"type":"rich_text","elements":[{"type":"rich_text_section","elements":[{"type":"text","text":"prod"}]}]}]`},
		{name: "app command in a thread", ts: tReply, d: cmdDoc("/deploy prod"), pick: core.Command{Name: "/deploy", AppID: "A0DEPLOY01"}, cmd: "/deploy", disp: "/deploy",
			blocks: `[{"type":"rich_text","elements":[{"type":"rich_text_section","elements":[{"type":"text","text":"prod"}]}]}]`},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			c, _, fs := cmdComposer(t, tc.ts)
			c.cmdPick = tc.pick
			p := submitWait(t, c, fs, tc.d, "chat.command", 1)[0]
			if p.Get("command") != tc.cmd || p.Get("disp") != tc.disp || p.Get("channel") != tChGen {
				t.Errorf("command %q disp %q channel %q, want %q %q %q", p.Get("command"), p.Get("disp"), p.Get("channel"), tc.cmd, tc.disp, tChGen)
			}
			if p.Has("text") {
				t.Errorf("chat.command carried text %q; executeCommand sends only blocks", p.Get("text"))
			}
			if ok, got, want := sameJSON(t, []byte(p.Get("blocks")), []byte(tc.blocks)); !ok {
				t.Errorf("blocks\n got %s\nwant %s", got, want)
			}
			if p.Get("thread_ts") != tc.ts {
				t.Errorf("thread_ts %q, want %q", p.Get("thread_ts"), tc.ts)
			}
			if p.Get("app_id") != tc.appID {
				t.Errorf("app_id %q, want %q (MessagePaneInput's slashCommandAppId; the thread's InputContainer passes none)", p.Get("app_id"), tc.appID)
			}
			if p.Get("client_token") == "" {
				t.Error("no client_token")
			}
			if n := len(fs.called("chat.postMessage")) + len(fs.called("chat.meMessage")); n != 0 {
				t.Errorf("%d messages posted for a command", n)
			}
			if !c.ed.Doc().Empty() {
				t.Errorf("the composer kept %q", c.ed.Doc().Text())
			}
		})
	}
}

// TestSlashInviteReadsMentions: the local /invite reads the people the
// parsed arguments mention, a chip or a typed name, and invites them;
// chat.command never runs.
func TestSlashInviteReadsMentions(t *testing.T) {
	for _, d := range []*Doc{cmdDoc("/invite ", bobbyChip), cmdDoc("/invite @bobby")} {
		c, _, fs := cmdComposer(t, "")
		p := submitWait(t, c, fs, d, "conversations.invite", 1)[0]
		if p.Get("channel") != tChGen || p.Get("users") != tBobby {
			t.Errorf("%q: conversations.invite %v, want users=%s", d.Text(), p, tBobby)
		}
		if n := len(fs.called("chat.command")); n != 0 {
			t.Errorf("%q: %d chat.command calls for the local /invite", d.Text(), n)
		}
	}
}

// TestSlashEscapesPostMessages: what Slack's slash test turns down is a
// message (its own error text says to put a space before the "/"): a
// leading space, "/" alone, "/ x", "//", inline code, a quote or code
// line first. None runs as a command; each is posted.
func TestSlashEscapesPostMessages(t *testing.T) {
	code := cmdDoc("/tmp is a dir")
	for i := 0; i < 4; i++ {
		code.Attrs[i].Style = Code
	}
	quote := cmdDoc("/remind me")
	quote.Lines[0] = Line{Kind: Quote}
	block := cmdDoc("/etc/hosts")
	block.Lines[0] = Line{Kind: CodeBlock}
	for _, d := range []*Doc{cmdDoc(" /remind me"), cmdDoc("/"), cmdDoc("/ x"), cmdDoc("//x"), code, quote, block} {
		c, _, fs := cmdComposer(t, "")
		txt := d.Text()
		submitWait(t, c, fs, d, "chat.postMessage", 1)
		if n := len(fs.called("chat.command")); n != 0 {
			t.Errorf("%q ran as a command (%d chat.command)", txt, n)
		}
	}
}

// TestSlashRemindAloneOpensModal: "/remind" without arguments is
// executeCommand's openLaterCustomModal (isMsgContext false): the
// Reminder dialog, no chat.command.
func TestSlashRemindAloneOpensModal(t *testing.T) {
	c, h, fs := cmdComposer(t, "")
	c.ed.SetDoc(cmdDoc("/remind"))
	c.submit()
	var n int
	for end := time.Now().Add(5 * time.Second); time.Now().Before(end); time.Sleep(10 * time.Millisecond) {
		runRestores()
		h.mu.Lock()
		n = len(h.overlays)
		h.mu.Unlock()
		if n > 0 {
			break
		}
	}
	if n != 1 {
		t.Fatalf("%d overlays, want the Reminder dialog", n)
	}
	h.mu.Lock()
	m, ok := h.overlays[0].(*popups.Modal)
	h.mu.Unlock()
	if !ok || m.Title() != "Reminder" {
		t.Fatalf("overlay %T, want the Reminder modal", h.overlays[0])
	}
	if k := len(fs.called("chat.command")); k != 0 {
		t.Errorf("%d chat.command calls for /remind alone", k)
	}
	// with arguments it is the server's
	p := submitWait(t, c, fs, cmdDoc("/remind me in 5 minutes"), "chat.command", 1)[0]
	if p.Get("command") != "/remind" {
		t.Errorf("chat.command %v", p)
	}
}

// TestSlashFailurePutsTextBack: a command that fails (unknown_command)
// leaves the composer with what was typed (InputContainer's catch:
// setContents when it's still empty); Slackbot says why.
func TestSlashFailurePutsTextBack(t *testing.T) {
	c, _, fs := cmdComposer(t, "")
	fs.mu.Lock()
	if fs.reply == nil {
		fs.reply = map[string]any{}
	}
	fs.reply["chat.command"] = map[string]any{"ok": false, "error": "unknown_command"}
	fs.mu.Unlock()
	submitWait(t, c, fs, cmdDoc("/nope hi"), "chat.command", 1)
	var got string
	for end := time.Now().Add(5 * time.Second); time.Now().Before(end); time.Sleep(10 * time.Millisecond) {
		runRestores()
		if got = c.ed.Doc().Text(); got != "" {
			break
		}
	}
	if got != "/nope hi" {
		t.Fatalf("composer %q after the failure, want the command back", got)
	}
	c.ed.SetDoc(NewDoc()) // nothing left for Close to save as the box's draft
}

// TestSlashBroadcastWarningFirst: a command with a keyword goes through
// the broadcast keyword warning first (prepareAndSendMessage's
// includeBroadcastKeywordWarning, ahead of handleSlashCommand): dismissed,
// nothing runs; sent, the command runs with the keyword as a broadcast
// element of its blocks.
func TestSlashBroadcastWarningFirst(t *testing.T) {
	app, fs := broadcastApp(t, nil)
	people(fs, 30)
	warnPrefs(app, map[string]any{"warn_before_at_channel": "always", "loud_channel_mentions_limit": 10000})
	d := cmdDoc("/ticket ping ", &Atom{Kind: AtomBroadcast, ID: "channel", Text: "@channel"}, " please")
	no := &confirmRecorder{ok: false}
	if err := warnSend(t, app, tChOpen, "", d, no); !isShoutyRooster(err) || no.n() != 1 {
		t.Fatalf("dismissed: %v, %d asks", err, no.n())
	}
	if n := len(fs.called("chat.command")); n != 0 {
		t.Fatalf("%d chat.command calls after Edit Message", n)
	}
	yes := &confirmRecorder{ok: true}
	if err := warnSend(t, app, tChOpen, "", d, yes); err != nil || yes.n() != 1 {
		t.Fatalf("sent: %v, %d asks", err, yes.n())
	}
	cmds := fs.called("chat.command")
	want := `[{"type":"rich_text","elements":[{"type":"rich_text_section","elements":[{"type":"text","text":"ping "},{"type":"broadcast","range":"channel"},{"type":"text","text":" please"}]}]}]`
	if len(cmds) != 1 || cmds[0].Get("command") != "/ticket" || cmds[0].Has("text") {
		t.Fatalf("chat.command %v", cmds)
	}
	if ok, got, w := sameJSON(t, []byte(cmds[0].Get("blocks")), []byte(want)); !ok {
		t.Errorf("blocks\n got %s\nwant %s", got, w)
	}
	if n := len(fs.called("chat.postMessage")); n != 0 {
		t.Errorf("%d posts", n)
	}
}

// TestSlashRelayedCommandLikeSlack: a delta a plugin hands back to
// prepareAndSendMessage runs as Slack runs it: its link and message slugs
// keep their URLs in the blocks (no text), and a full-width slash starts a
// command, kept in its word.
func TestSlashRelayedCommandLikeSlack(t *testing.T) {
	app, fs := syntheticApp(t)
	withTaut(t, nil)
	relay := func(ops string) url.Values {
		t.Helper()
		before := len(fs.called("chat.command"))
		j := &sendJob{api: coreAPI{app}, ch: tChGen, o: &outgoing{doc: NewDoc()}}
		if err := j.prepareAndSend(context.Background(), prepareSendArgs{ChannelID: tChGen, Delta: oracleDelta(t, ops)}); err != nil {
			t.Fatal(err)
		}
		cmds := fs.called("chat.command")
		if len(cmds) != before+1 {
			t.Fatalf("%d chat.command calls", len(cmds)-before)
		}
		return cmds[before]
	}
	p := relay(`[{"insert":"/ticket see "},{"insert":"example.com/docs/\u2026/guide","attributes":{"slackslug":{"type":"link","id":"https://example.com/docs/a/guide","url":"https://example.com/docs/a/guide","label":"example.com/docs/\u2026/guide"}}},{"insert":" and "},{"insert":"clod in #clack","attributes":{"slackslug":{"type":"message","id":"1700000000.000100","label":"clod in #clack","secondaryObjectId":"C0CFT52NEGM","url":"https://hackclub.slack.com/archives/C0CFT52NEGM/p1700000000000100","authorId":"U0CLOD00001"}}},{"insert":"\n"}]`)
	want := `[{"type":"rich_text","elements":[{"type":"rich_text_section","elements":[{"type":"text","text":"see "},{"type":"link","url":"https://example.com/docs/a/guide"},{"type":"text","text":" and "},{"type":"message_mention","message_ts":"1700000000.000100","channel_id":"C0CFT52NEGM","text":"https://hackclub.slack.com/archives/C0CFT52NEGM/p1700000000000100","url":"https://hackclub.slack.com/archives/C0CFT52NEGM/p1700000000000100","author_id":"U0CLOD00001"}]}]}]`
	if p.Get("command") != "/ticket" || p.Has("text") {
		t.Errorf("chat.command %v", p)
	}
	if ok, got, w := sameJSON(t, []byte(p.Get("blocks")), []byte(want)); !ok {
		t.Errorf("blocks\n got %s\nwant %s", got, w)
	}
	fw := string(rune(0xff0f)) + "bchannel"
	p = relay(`[{"insert":"\uff0fbchannel relay-aaaaaaaa\n"}]`)
	if p.Get("command") != fw || p.Get("disp") != fw || p.Get("blocks") != `[{"type":"rich_text","elements":[{"type":"rich_text_section","elements":[{"type":"text","text":"relay-aaaaaaaa"}]}]}]` {
		t.Errorf("full-width: chat.command %v", p)
	}
}

// sameJSON reports whether a and b are the same JSON value (key order
// aside), and both re-encoded.
func sameJSON(t *testing.T, a, b []byte) (bool, string, string) {
	t.Helper()
	var x, y any
	if len(a) == 0 {
		a = []byte("null")
	}
	if len(b) == 0 {
		b = []byte("null")
	}
	if err := json.Unmarshal(a, &x); err != nil {
		t.Fatalf("bad JSON %s: %v", a, err)
	}
	if err := json.Unmarshal(b, &y); err != nil {
		t.Fatalf("bad JSON %s: %v", b, err)
	}
	xa, _ := json.Marshal(x)
	ya, _ := json.Marshal(y)
	return string(xa) == string(ya), string(xa), string(ya)
}

func oracleDelta(t *testing.T, ops string) map[string]any {
	t.Helper()
	var v []any
	if err := json.Unmarshal([]byte(ops), &v); err != nil {
		t.Fatalf("ops %s: %v", ops, err)
	}
	return map[string]any{"ops": v}
}
