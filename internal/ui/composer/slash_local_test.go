package composer

import (
	"net/url"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/some-du6e/clack/internal/slack"
	"github.com/some-du6e/clack/internal/store"
	"github.com/some-du6e/clack/internal/ui/popups"
)

// Slack's client commands the composer runs (slash_local.go), through
// submit() against the fake API: the command finds its action by name or
// alias, the app around the composer does what Slack's does, Slack's
// texts for what fails (as Slackbot here, the command back in the
// composer), and nothing goes to chat.command.

const (
	tChRand    = "C0RANDOM01" // public, the viewer not in it
	tChOld     = "C0OLDNEWS1" // archived
	tChOldPriv = "G0OLDSECR1" // private and archived
	tChOther   = "C0OTHERWS1" // another workspace's
	tGone      = "U0GONE0001" // deactivated
	tBobbyDM   = "D0BOBBY001"
	tGroup     = "S0SNACKS01"
)

// localSeed adds the channels and people the local commands are tried on.
func localSeed(s *store.Store) {
	s.PutConv(slack.Conversation{ID: tChRand, Name: "random", IsChannel: true, NumMembers: 40})
	s.PutConv(slack.Conversation{ID: tChOld, Name: "old-news", IsChannel: true, IsMember: true, IsArchived: true})
	s.PutConv(slack.Conversation{ID: tChOldPriv, Name: "old-secrets", IsChannel: true, IsPrivate: true, IsMember: true, IsArchived: true})
	s.PutConv(slack.Conversation{ID: tChOther, Name: "elsewhere", IsChannel: true, ContextTeamID: "T0OTHERWS1"})
	s.PutUsers([]slack.User{{ID: tGone, Name: "gone", Deleted: true, Profile: slack.Profile{DisplayName: "gone"}}})
	// loaded timelines: where Slackbot's feedback lands (slackbotSaid)
	for _, ch := range []string{tChGen, tBobbyDM} {
		s.PrependOlder(ch, []slack.Message{{Type: "message", TS: "1700000000.000100", User: "UALICE", Text: "synthetic"}}, false)
	}
}

// localComposer is cmdComposer with localSeed (and seed), conversations.open
// giving Bobby's DM and the joins answering with the channel.
func localComposer(t *testing.T, ts string, seed ...func(*store.Store)) (*Composer, *cmdHost, *fakeSlack) {
	t.Helper()
	c, h, fs := cmdComposer(t, ts, append([]func(*store.Store){localSeed}, seed...)...)
	fs.mu.Lock()
	if fs.reply == nil {
		fs.reply = map[string]any{}
	}
	fs.reply["conversations.open"] = map[string]any{"ok": true, "channel": map[string]any{"id": tBobbyDM, "is_im": true, "user": tBobby}}
	random := map[string]any{"id": tChRand, "name": "random", "is_channel": true, "is_member": true}
	fs.reply["conversations.join"] = map[string]any{"ok": true, "channel": random}
	fs.reply["conversations.info"] = map[string]any{"ok": true, "channel": random}
	fs.mu.Unlock()
	return c, h, fs
}

// waitUI runs what the command queued for the UI goroutine until cond
// holds (a box's own work too: no composer is laid out here).
func waitUI(t *testing.T, what string, cond func() bool) {
	t.Helper()
	for end := time.Now().Add(5 * time.Second); time.Now().Before(end); time.Sleep(10 * time.Millisecond) {
		runRestoresIn(nil, true)
		if cond() {
			return
		}
	}
	t.Fatalf("timed out waiting for %s", what)
}

func (h *cmdHost) sawNav(r string) bool {
	h.mu.Lock()
	defer h.mu.Unlock()
	return slices.Contains(h.navs, r)
}

func (h *cmdHost) counts() (toggles, searches int) {
	h.mu.Lock()
	defer h.mu.Unlock()
	return h.toggles, h.searches
}

// shown is the last overlay of type T the host was asked to show (an
// async call's pump shows too).
func shown[T popups.Overlay](h *cmdHost) (T, bool) {
	h.mu.Lock()
	defer h.mu.Unlock()
	for i := len(h.overlays) - 1; i >= 0; i-- {
		if o, ok := h.overlays[i].(T); ok {
			return o, true
		}
	}
	var zero T
	return zero, false
}

func (h *cmdHost) navCount() int {
	h.mu.Lock()
	defer h.mu.Unlock()
	return len(h.navs)
}

func (h *cmdHost) lastNav() string {
	h.mu.Lock()
	defer h.mu.Unlock()
	if len(h.navs) == 0 {
		return ""
	}
	return h.navs[len(h.navs)-1]
}

// slackbotSays waits for Slackbot's last temporary message in ch to be
// want, and for the composer to hold the command again.
func slackbotSays(t *testing.T, c *Composer, ch, want, typed string) {
	t.Helper()
	waitUI(t, "Slackbot's "+want, func() bool {
		m, ok := slackbotSaid(c.api.a, ch)
		return ok && m.Text == want && (typed == "" || c.ed.Doc().Text() == typed)
	})
}

func noCommandCalls(t *testing.T, fs *fakeSlack) {
	t.Helper()
	if n := len(fs.called("chat.command")); n != 0 {
		t.Errorf("%d chat.command calls for a client command", n)
	}
}

// TestSlashShortcutsTogglesPane: /shortcuts and its alias /keys
// (any case) toggle the Keyboard Shortcuts pane, in a thread too.
func TestSlashShortcutsTogglesPane(t *testing.T) {
	for _, ts := range []string{"", tReply} {
		c, h, fs := localComposer(t, ts)
		for i, w := range []string{"/shortcuts", "/keys", "/Keys"} {
			c.ed.SetDoc(cmdDoc(w))
			c.submit()
			waitUI(t, w, func() bool { n, _ := h.counts(); return n == i+1 })
			if !c.ed.Doc().Empty() {
				t.Errorf("%q stayed in the composer", c.ed.Doc().Text())
			}
		}
		noCommandCalls(t, fs)
	}
}

// TestSlashSearchRunsOrOpens: /search (/s) with text runs the search at
// once (openFromSlashCommand's commitSearch: the results page and
// search.save, no popover), a mention as its token; alone it opens the
// search popover on the box's query.
func TestSlashSearchRunsOrOpens(t *testing.T) {
	c, h, fs := localComposer(t, "")
	for _, tc := range []struct {
		d    *Doc
		want string
	}{
		{cmdDoc("/search zephyr checklist"), "zephyr checklist"},
		{cmdDoc("/s   zephyr    launch  "), "zephyr launch"},
		{cmdDoc("/search from:", bobbyChip, " \"launch  plan\""), "from:<@" + tBobby + "> \"launch  plan\""},
		{cmdDoc("/search in:", &Atom{Kind: AtomChannel, ID: tChGen, Text: "#general"}), "in:<#" + tChGen + "|general>"},
	} {
		txt := tc.d.Text()
		navs, saves := h.navCount(), len(fs.called("search.save"))
		c.ed.SetDoc(tc.d)
		c.submit()
		nav := "search?q=" + url.QueryEscape(tc.want)
		// this submit's own navigation and search.save, not an earlier one's
		waitUI(t, nav, func() bool { return h.navCount() == navs+1 && h.lastNav() == nav })
		waitUI(t, "search.save", func() bool {
			ss := fs.called("search.save")
			return len(ss) == saves+1 && ss[saves].Get("terms") == tc.want
		})
		if _, n := h.counts(); n != 0 {
			t.Errorf("%q opened the popover", txt)
		}
	}
	for i, w := range []string{"/search", "/s ", "/S"} {
		c.ed.SetDoc(cmdDoc(w))
		c.submit()
		waitUI(t, w+" opening the popover", func() bool { _, n := h.counts(); return n == i+1 })
	}
	noCommandCalls(t, fs)
}

// TestSlashOpen: /open (/join) without arguments is the channel browser;
// a channel the viewer is in is shown, one they aren't joined first (an
// archived one shown anyway), a person's DM opened; Slack's texts for a
// failed join, another workspace's channel and a person whose DM won't
// open.
func TestSlashOpen(t *testing.T) {
	c, h, fs := localComposer(t, "")
	run := func(d *Doc, nav string) {
		t.Helper()
		c.ed.SetDoc(d)
		c.submit()
		waitUI(t, nav, func() bool { return h.sawNav(nav) })
	}
	run(cmdDoc("/open"), "directories?tab=channels")
	run(cmdDoc("/join ", &Atom{Kind: AtomChannel, ID: tChGen, Text: "#general"}), "c/"+tChGen)
	if n := len(fs.called("conversations.join")); n != 0 {
		t.Errorf("%d joins for a channel the viewer is in", n)
	}
	run(cmdDoc("/open ", &Atom{Kind: AtomChannel, ID: tChRand, Text: "#random"}), "c/"+tChRand)
	if j := fs.called("conversations.join"); len(j) != 1 || j[0].Get("channel") != tChRand {
		t.Errorf("joins %v", j)
	}
	run(cmdDoc("/open ", bobbyChip), "c/"+tBobbyDM)
	if o := fs.called("conversations.open"); len(o) != 1 || o[0].Get("users") != tBobby {
		t.Errorf("conversations.open %v", o)
	}
	noCommandCalls(t, fs)

	c2, h2, fs2 := localComposer(t, "", func(s *store.Store) {
		s.PutConv(slack.Conversation{ID: "C0SHELVED1", Name: "shelved", IsChannel: true, IsArchived: true})
	})
	fs2.answer("conversations.join", map[string]any{"ok": false, "error": "is_archived"})
	c2.ed.SetDoc(cmdDoc("/open ", &Atom{Kind: AtomChannel, ID: "C0SHELVED1", Text: "#shelved"}))
	c2.submit()
	waitUI(t, "the archived channel", func() bool { return h2.sawNav("c/C0SHELVED1") })

	for _, tc := range []struct {
		name string
		d    *Doc
		fail map[string]map[string]any
		want string
	}{
		{"join refused", cmdDoc("/open ", &Atom{Kind: AtomChannel, ID: tChRand, Text: "#random"}),
			map[string]map[string]any{"conversations.join": {"ok": false, "error": "method_not_supported_for_channel_type"}},
			"There was a problem joining “random”, sorry!"},
		{"another workspace", cmdDoc("/join ", &Atom{Kind: AtomChannel, ID: tChOther, Text: "#elsewhere"}), nil,
			"*/join* can’t open or join channels outside this workspace."},
		{"no DM", cmdDoc("/open ", bobbyChip), map[string]map[string]any{"conversations.open": {"ok": false, "error": "user_disabled"}},
			"A valid member name is required."},
	} {
		t.Run(tc.name, func(t *testing.T) {
			c, h, fs := localComposer(t, "")
			for m, rep := range tc.fail {
				fs.answer(m, rep)
			}
			typed := tc.d.Text()
			c.ed.SetDoc(tc.d)
			c.submit()
			slackbotSays(t, c, tChGen, tc.want, typed)
			if n := h.navCount(); n != 0 {
				t.Errorf("navigated %d times", n)
			}
		})
	}
}

// TestSlashOpenUnknownChannel: a name /open can't find opens the
// create-channel modal with it when the viewer may create channels
// (rejectChannelNotFound), else Slackbot says it couldn't find it.
func TestSlashOpenUnknownChannel(t *testing.T) {
	c, h, fs := localComposer(t, "")
	fs.answer("edge/permissions/info", map[string]any{"ok": true, "allowed": []string{"CREATE_PUBLIC_CHANNEL"}, "allowed_per_team": map[string]any{}})
	c.ed.SetDoc(cmdDoc("/open #plans-2027"))
	c.submit()
	waitUI(t, "the create-channel modal", func() bool {
		_, ok := shown[*popups.CreateChannelModal](h)
		return ok
	})
	if m, _ := shown[*popups.CreateChannelModal](h); m.Name() != "plans-2027" {
		t.Errorf("create-channel name %q", m.Name())
	}

	c, _, fs = localComposer(t, "")
	fs.answer("edge/permissions/info", map[string]any{"ok": true, "allowed": []string{}, "allowed_per_team": map[string]any{}})
	c.ed.SetDoc(cmdDoc("/open plans-2027"))
	c.submit()
	slackbotSays(t, c, tChGen, "I couldn’t find a channel named “plans-2027”, sorry!", "/open plans-2027")
	noCommandCalls(t, fs)
}

// TestSlashOpenGuest: /open is UNKNOWN_COMMAND for a guest
// (isSlashCommandEnabled).
func TestSlashOpenGuest(t *testing.T) {
	c, h, fs := localComposer(t, "", func(s *store.Store) {
		s.PutUsers([]slack.User{{ID: tSelf, Name: "me", RealName: "Test Self", IsRestricted: true,
			Profile: slack.Profile{DisplayName: "self", RealName: "Test Self"}}})
	})
	c.ed.SetDoc(cmdDoc("/open"))
	c.submit()
	slackbotSays(t, c, tChGen, "*/open* is not a valid command. In Slack, all messages that start with the \"/\" character are interpreted as commands.\n\n"+
		"If you are trying to send a message and not run a command, try preceding the \"/\" with an empty space.", "/open")
	if n := h.navCount(); n != 0 {
		t.Errorf("navigated %d times", n)
	}
	noCommandCalls(t, fs)
}

// TestSlashMsg: /msg (/dm) without arguments is the DMs; with a person
// their DM opens and what follows goes there as a message, with a channel
// the channel (joined if need be); Slack's texts for the rest.
func TestSlashMsg(t *testing.T) {
	c, h, fs := localComposer(t, "")
	c.ed.SetDoc(cmdDoc("/msg"))
	c.submit()
	waitUI(t, "the DMs", func() bool { return h.sawNav("dms") })

	posts := func(ch string) []url.Values {
		var out []url.Values
		for _, p := range fs.called("chat.postMessage") {
			if p.Get("channel") == ch {
				out = append(out, p)
			}
		}
		return out
	}
	c.ed.SetDoc(cmdDoc("/msg ", bobbyChip, "  hello there  "))
	c.submit()
	waitUI(t, "Bobby's DM", func() bool { return h.sawNav("c/"+tBobbyDM) && len(posts(tBobbyDM)) == 1 })
	// the message as any (its block has a block_id of its own)
	if p := posts(tBobbyDM)[0]; p.Get("text") != "hello there" || !strings.Contains(p.Get("blocks"), `{"type":"text","text":"hello there"}`) {
		t.Errorf("the message: text %q blocks %s", p.Get("text"), p.Get("blocks"))
	}
	c.ed.SetDoc(cmdDoc("/dm ", &Atom{Kind: AtomChannel, ID: tChRand, Text: "#random"}, " hi all"))
	c.submit()
	waitUI(t, "#random", func() bool { return h.sawNav("c/"+tChRand) && len(posts(tChRand)) == 1 })
	if j := fs.called("conversations.join"); len(j) != 1 || j[0].Get("channel") != tChRand {
		t.Errorf("joins %v", j)
	}
	if got := posts(tChRand)[0].Get("text"); got != "hi all" {
		t.Errorf("#random got %q", got)
	}
	// a person alone: only the DM
	c.ed.SetDoc(cmdDoc("/msg ", bobbyChip))
	c.submit()
	waitUI(t, "the DM again", func() bool { return len(fs.called("conversations.open")) == 2 })
	if n := len(posts(tBobbyDM)); n != 1 {
		t.Errorf("%d messages to Bobby", n)
	}
	noCommandCalls(t, fs)

	gone := &Atom{Kind: AtomUser, ID: tGone, Text: "@gone"}
	for _, tc := range []struct {
		name string
		d    *Doc
		want string
	}{
		{"no mention", cmdDoc("/msg hello"), "A valid member name is required."},
		{"a user group", cmdDoc("/dm ", &Atom{Kind: AtomUsergroup, ID: tGroup, Text: "@snacks"}, " hi"), "*/dm* can’t send message to user groups."},
		{"deactivated", cmdDoc("/msg ", gone, " are you there"), "That user has been deactivated."},
		{"unknown channel", cmdDoc("/msg ", &Atom{Kind: AtomChannel, ID: "C0NOWHERE1", Text: "#nowhere"}, " hi"), "A valid channel name is required."},
		{"another workspace", cmdDoc("/msg ", &Atom{Kind: AtomChannel, ID: tChOther, Text: "#elsewhere"}, " hi"),
			"*/msg* can’t send message to channels outside this workspace."},
		{"archived", cmdDoc("/msg ", &Atom{Kind: AtomChannel, ID: tChOld, Text: "#old-news"}, " hi"), "That channel has been archived."},
		{"archived private", cmdDoc("/msg ", &Atom{Kind: AtomChannel, ID: tChOldPriv, Text: "old-secrets"}, " hi"),
			"That private channel has been archived."},
	} {
		t.Run(tc.name, func(t *testing.T) {
			c, h, fs := localComposer(t, "")
			typed := tc.d.Text()
			c.ed.SetDoc(tc.d)
			c.submit()
			slackbotSays(t, c, tChGen, tc.want, typed)
			if n := h.navCount(); n != 0 || len(fs.called("chat.postMessage")) != 0 {
				t.Errorf("navigated %d times, %d posts", n, len(fs.called("chat.postMessage")))
			}
		})
	}
	// a deactivated person without a message: their DM opens
	c3, h3, _ := localComposer(t, "")
	c3.ed.SetDoc(cmdDoc("/msg ", gone))
	c3.submit()
	waitUI(t, "the DM of a deactivated person", func() bool { return h3.sawNav("c/" + tBobbyDM) })
}

// TestSlashMsgSendFails: the message /msg sends failing is Slackbot's
// "Hmm, something went wrong. Try again?" in the DM; the command doesn't
// come back here.
func TestSlashMsgSendFails(t *testing.T) {
	c, h, fs := localComposer(t, "")
	fs.answer("chat.postMessage", map[string]any{"ok": false, "error": "msg_too_long"})
	c.ed.SetDoc(cmdDoc("/msg ", bobbyChip, " hello"))
	c.submit()
	waitUI(t, "Slackbot in the DM", func() bool {
		m, ok := slackbotSaid(c.api.a, tBobbyDM)
		return ok && m.Text == "Hmm, something went wrong. Try again?" && h.sawNav("c/"+tBobbyDM)
	})
	if !c.ed.Doc().Empty() {
		t.Errorf("the composer holds %q", c.ed.Doc().Text())
	}
}

// TestSlashFeedbackOpensDialog: /feedback under slash_feedback_in_modal is
// the feedback dialog; without it the command goes on to chat.command
// (Slack's Help pane form isn't Clack's).
func TestSlashFeedbackOpensDialog(t *testing.T) {
	c, h, fs := localComposer(t, "")
	c.api.a.SetExperimentGroup(popups.ExpSlashFeedbackInModal, "on")
	c.ed.SetDoc(cmdDoc("/feedback the sidebar is great"))
	c.submit()
	waitUI(t, "the feedback dialog", func() bool {
		_, ok := shown[*popups.UserFeedbackModal](h)
		return ok
	})
	noCommandCalls(t, fs)

	c.api.a.SetExperimentGroup(popups.ExpSlashFeedbackInModal, "off")
	if p := submitWait(t, c, fs, cmdDoc("/feedback"), "chat.command", 1)[0]; p.Get("command") != "/feedback" {
		t.Errorf("chat.command %v", p)
	}
}

// TestSlashServerOnlyCommands: /away, /active, /mute and /star have no
// local action in executeCommand: chat.command gets each as Slack sends
// it (the word, disp as typed, no text; blocks only for arguments).
func TestSlashServerOnlyCommands(t *testing.T) {
	c, _, fs := localComposer(t, "")
	for _, tc := range []struct {
		d               *Doc
		cmd, disp, blks string
	}{
		{cmdDoc("/away"), "/away", "/away", ""},
		{cmdDoc("/Active"), "/active", "/Active", ""},
		{cmdDoc("/mute"), "/mute", "/mute", ""},
		{cmdDoc("/star"), "/star", "/star", ""},
		{cmdDoc("/star this one"), "/star", "/star",
			`[{"type":"rich_text","elements":[{"type":"rich_text_section","elements":[{"type":"text","text":"this one"}]}]}]`},
	} {
		p := submitWait(t, c, fs, tc.d, "chat.command", 1)[0]
		if p.Get("command") != tc.cmd || p.Get("disp") != tc.disp || p.Get("channel") != tChGen || p.Has("text") || p.Get("client_token") == "" {
			t.Errorf("%s: chat.command %v", tc.disp, p)
		}
		if tc.blks == "" {
			if p.Has("blocks") {
				t.Errorf("%s: blocks %s without arguments", tc.disp, p.Get("blocks"))
			}
		} else if ok, got, want := sameJSON(t, []byte(p.Get("blocks")), []byte(tc.blks)); !ok {
			t.Errorf("%s: blocks %s, want %s", tc.disp, got, want)
		}
	}
	if n := len(fs.called("chat.postMessage")); n != 0 {
		t.Errorf("%d messages posted", n)
	}
}

// TestSlashLocalHelpers: openName, msgRest, qContentful and searchQueryOf
// on Slack's ops.
func TestSlashLocalHelpers(t *testing.T) {
	rest := func(parts ...any) qDelta {
		q, _ := qFromDoc(cmdDoc(parts...))
		return q
	}
	ch := &Atom{Kind: AtomChannel, ID: tChGen, Text: "#general"}
	for _, tc := range []struct {
		q    qDelta
		want string
	}{
		{rest("#plans  extra"), "plans  extra"},
		{rest(ch, " more"), "general"},
		{rest(bobbyChip, " more"), ""},
		{rest(""), ""},
	} {
		if got := openName(tc.q); got != tc.want {
			t.Errorf("openName %q, want %q", got, tc.want)
		}
	}
	if got := msgRest(rest(bobbyChip, bobbyChip, "  hi  ")); got.ops == nil || string(qText(got.ops, 0, false, false)) != "hi" {
		t.Errorf("msgRest %v", got.ops)
	}
	if got := msgRest(rest(bobbyChip)); len(got.ops) != 0 {
		t.Errorf("msgRest of a lone mention %v", got.ops)
	}
	if qContentful(rest("")) || !qContentful(rest(" ")) || !qContentful(rest(bobbyChip)) {
		t.Error("qContentful")
	}
	if got := searchQueryOf(rest("from:", bobbyChip, "  zephyr   ", "“go  live”")); got != "from:<@"+tBobby+"> zephyr “go  live”" {
		t.Errorf("searchQueryOf %q", got)
	}
}

// TestSlashMsgInTarget: /msg's message does in the conversation it goes
// to what a send there does. That conversation's composer gets the
// message's jump (OnSend with its client id) and the unsend window, the
// box /msg was typed in neither; a command the message turns out to be
// keeps its input in that composer (executeCommand's keep_input: its own
// channel's message pane input); a failed send puts the /msg command
// there too.
func TestSlashMsgInTarget(t *testing.T) {
	c, h, fs := localComposer(t, "")
	clearDraft(tBobbyDM, "")
	dm := New(h, tBobbyDM, "")
	t.Cleanup(func() {
		dm.Close()
		clearDraft(tBobbyDM, "")
	})
	var sends []string
	dm.OnSend = func(cid string, files bool) { sends = append(sends, cid) } // the UI goroutine's (waitUI)
	c.OnSend = func(string, bool) { t.Error("the box /msg was typed in jumped") }
	c.ed.SetDoc(cmdDoc("/msg ", bobbyChip, " hello there"))
	c.submit()
	waitUI(t, "the DM's jump and unsend window", func() bool { return len(sends) == 1 && !dm.unsendBy.IsZero() })
	if p := fs.called("chat.postMessage"); len(p) != 1 || sends[0] == "" || p[0].Get("client_msg_id") != sends[0] {
		t.Errorf("jumped to %q, the post %v", sends[0], p)
	}
	if !c.unsendBy.IsZero() {
		t.Error("the box /msg was typed in armed its unsend window")
	}

	// the message is a command answering keep_input
	fs.answer("chat.command", map[string]any{"ok": true, "keep_input": true})
	c.ed.SetDoc(cmdDoc("/msg ", bobbyChip, " /giphy cats"))
	c.submit()
	waitUI(t, "the command back in the DM's box", func() bool { return dm.ed.Doc().Text() == "/giphy cats" })
	if !c.ed.Doc().Empty() {
		t.Errorf("the box /msg was typed in holds %q", c.ed.Doc().Text())
	}

	// the send fails: Slackbot in the DM, the /msg command in its box
	dm.ed.SetDoc(NewDoc())
	fs.answer("chat.postMessage", map[string]any{"ok": false, "error": "msg_too_long"})
	d := cmdDoc("/msg ", bobbyChip, " hello")
	typed := d.Text()
	c.ed.SetDoc(d)
	c.submit()
	waitUI(t, "the command in the DM's box", func() bool {
		m, ok := slackbotSaid(c.api.a, tBobbyDM)
		return ok && m.Text == "Hmm, something went wrong. Try again?" && dm.ed.Doc().Text() == typed
	})
	if !c.ed.Doc().Empty() {
		t.Errorf("the box /msg was typed in holds %q", c.ed.Doc().Text())
	}
}

// TestCommandNavigatesFromItsBox: a command's navigation runs in the
// Layout of the box it was typed in (laterHere), where the host knows
// that box's view container, not in whichever box drains the queue first.
func TestCommandNavigatesFromItsBox(t *testing.T) {
	c, h, _ := localComposer(t, "")
	clearDraft(tChRand, "")
	other := New(h, tChRand, "")
	t.Cleanup(func() {
		other.Close()
		clearDraft(tChRand, "")
	})
	c.ed.SetDoc(cmdDoc("/open"))
	c.submit()
	queued := func() bool { return queuedHere(c) }
	for end := time.Now().Add(5 * time.Second); !queued() && time.Now().Before(end); time.Sleep(10 * time.Millisecond) {
		runRestoresIn(other, false)
	}
	if !queued() {
		t.Fatal("/open queued nothing for its box")
	}
	runRestoresIn(other, false)
	runRestores()
	if n := h.navCount(); n != 0 {
		t.Fatalf("another box's Layout navigated (%d)", n)
	}
	runRestoresIn(c, false)
	if !h.sawNav("directories?tab=channels") {
		t.Errorf("its own Layout didn't navigate: %v", h.navs)
	}
}

// TestSlashFeedbackDisabledOrg: under feedback_disabled_orgs' treatment
// /feedback is UNKNOWN_COMMAND (isSlashCommandEnabled): Slackbot's "not a
// valid command", no dialog, no chat.command.
func TestSlashFeedbackDisabledOrg(t *testing.T) {
	c, h, fs := localComposer(t, "")
	c.api.a.SetExperimentGroup(popups.ExpSlashFeedbackInModal, "on")
	c.api.a.SetExperimentGroup("feedback_disabled_orgs", "treatment")
	c.ed.SetDoc(cmdDoc("/feedback the sidebar"))
	c.submit()
	slackbotSays(t, c, tChGen, "*/feedback* is not a valid command. In Slack, all messages that start with the \"/\" character are interpreted as commands.\n\n"+
		"If you are trying to send a message and not run a command, try preceding the \"/\" with an empty space.", "/feedback the sidebar")
	if _, ok := shown[*popups.UserFeedbackModal](h); ok {
		t.Error("the dialog opened")
	}
	noCommandCalls(t, fs)
}

// TestSlashCollapseExpand: /collapse and /expand run in the client (no
// chat.command): in the channel's box every held message's previews
// collapse (or expand) under the box's view context, Channel, and
// Slackbot says so for good; in a thread's box only the thread's, under
// Thread, and Slackbot says nothing.
func TestSlashCollapseExpand(t *testing.T) {
	const old, reply = "1690000000.000200", "1690000000.000300"
	seed := func(s *store.Store) {
		s.PrependOlder(tChGen, []slack.Message{{Type: "message", TS: old, User: "UALICE", Files: []slack.File{{ID: "F0SHOT"}}}}, false)
		s.SetThread(tChGen, tReply, []slack.Message{{Type: "message", TS: tReply, User: "UALICE", Text: "root", ThreadTS: tReply},
			{Type: "message", TS: reply, User: "UALICE", ThreadTS: tReply, Files: []slack.File{{ID: "F0REPLY"}}}})
	}
	c, _, fs := localComposer(t, "", seed)
	expanded := func(k string) (bool, bool) { return expandedIn(c.api.a, k) }
	chKey := "inline_files_msg_1690000000_000200F0SHOTChannel"
	replyKey := "inline_files_msg_1690000000_000300F0REPLYChannel"
	c.ed.SetDoc(cmdDoc("/collapse"))
	c.submit()
	slackbotSays(t, c, tChGen, "I’ve collapsed all files in this channel for you.", "")
	for _, k := range []string{chKey, replyKey} {
		if on, ok := expanded(k); !ok || on {
			t.Errorf("/collapse: %s %v %v", k, on, ok)
		}
	}
	if !c.ed.Doc().Empty() {
		t.Errorf("the box holds %q", c.ed.Doc().Text())
	}
	c.ed.SetDoc(cmdDoc("/Expand"))
	c.submit()
	slackbotSays(t, c, tChGen, "I’ve expanded all files in this channel for you.", "")
	if on, _ := expanded(chKey); !on {
		t.Error("/expand didn't expand")
	}
	noCommandCalls(t, fs)

	tc, _, tfs := localComposer(t, tReply, seed)
	ta := tc.api.a
	threadKey := "inline_files_msg_1690000000_000300F0REPLYThread"
	tc.ed.SetDoc(cmdDoc("/collapse"))
	tc.submit()
	waitUI(t, "the thread's collapse", func() bool { on, ok := expandedIn(ta, threadKey); return ok && !on })
	if _, ok := expandedIn(ta, "inline_files_msg_1690000000_000200F0SHOTThread"); ok {
		t.Error("the thread's /collapse reached a channel message")
	}
	if m, ok := slackbotSaid(ta, tChGen); ok && strings.Contains(m.Text, "collapsed all files") {
		t.Errorf("Slackbot spoke for a thread's /collapse: %q", m.Text)
	}
	noCommandCalls(t, tfs)
}
