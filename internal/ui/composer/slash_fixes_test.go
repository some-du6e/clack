package composer

import (
	"context"
	"encoding/json"
	"strings"
	"testing"
	"time"

	"github.com/some-du6e/clack/internal/core"
	"github.com/some-du6e/clack/internal/slack"
	"github.com/some-du6e/clack/internal/store"
	"github.com/some-du6e/clack/internal/ui/theme"
	"github.com/some-du6e/clack/internal/ui/uipref"
)

// The review fixes of slash-commands-2 (wf_15b390b1-18e), through the
// composer against the fake API: Slack's processor runs once per send,
// over a store snapshot read only when a name is typed, with Slack's
// member filters and normalizeName; a failed schedule gives back the
// text as typed; silence_error resolves; /topic goes out cleaned; edits
// take keywords by the view.

const (
	tCarol  = "U0CAROL0001"
	tSamA   = "U0SAMA00001"
	tSamB   = "U0SAMB00001"
	tGoneU  = "U0GONE00001"
	tFlow   = "U0FLOW00001"
	tProf   = "U0PROF00001"
	tWalled = "U0WALL00001"
)

// fixSeed adds the people the filters and the single pass are tried on.
func fixSeed(s *store.Store) {
	p := func(name string) slack.Profile { return slack.Profile{DisplayName: name, RealName: name} }
	s.PutUsers([]slack.User{
		{ID: tCarol, Name: "carol", RealName: "Carol", Profile: p("carol")},
		{ID: tSamA, Name: "sam", RealName: "Sam Adams", Profile: slack.Profile{DisplayName: "sam", RealName: "Sam Adams"}},
		{ID: tSamB, Name: "sam", RealName: "Sam Brown", Profile: slack.Profile{DisplayName: "sam", RealName: "Sam Brown"}},
		{ID: tGoneU, Name: "gone", Deleted: true, Profile: p("gone")},
		{ID: tFlow, Name: "flowbot", IsBot: true, IsWorkflowBot: true, Profile: p("flowbot")},
	})
	s.PutUsers(barredUsers(p("pat"), p("walled")))
}

func (f *fakeSlack) answer(method string, rep map[string]any) {
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.reply == nil {
		f.reply = map[string]any{}
	}
	f.reply[method] = rep
}

func atoms(d *Doc) []*Atom {
	var out []*Atom
	for _, a := range d.Attrs {
		if a.Atom != nil {
			out = append(out, a.Atom)
		}
	}
	return out
}

// TestScheduleFailurePutsTypedTextBack: a schedule Slack refuses puts the
// text back as typed, a typed name text again, as a failed send does
// (o.orig), not the processed document's chip.
func TestScheduleFailurePutsTypedTextBack(t *testing.T) {
	c, _, fs := cmdComposer(t, "")
	fs.answer("drafts.create", map[string]any{"ok": false, "error": "invalid_date_scheduled"})
	c.ed.SetDoc(cmdDoc("ask @alice about it"))
	c.schedule(time.Now().Add(2 * time.Hour))
	untilUI(t, "the text back", func() bool { return !c.ed.Doc().Empty() })
	if d := c.ed.Doc(); d.Text() != "ask @alice about it" || len(atoms(d)) != 0 {
		t.Errorf("put back %q with %d chips, want the typed text", d.Text(), len(atoms(d)))
	}
	c.ed.SetDoc(NewDoc())
}

// TestSilenceErrorKeepsInputCleared: a command failing with
// silence_error resolves (executeCommand's catch returns): no Slackbot
// text, and the command doesn't come back.
func TestSilenceErrorKeepsInputCleared(t *testing.T) {
	c, _, fs := cmdComposer(t, "")
	fs.answer("chat.command", map[string]any{"ok": false, "error": "app_failed", "silence_error": true})
	submitWait(t, c, fs, cmdDoc("/quiet please"), "chat.command", 1)
	if armedWithin(c, time.Second) {
		t.Error("the failure opened the unsend window")
	}
	if got := c.ed.Doc().Text(); got != "" {
		t.Errorf("the composer got %q back", got)
	}
	if m, ok := slackbotSaid(c.api.a, tChGen); ok {
		t.Errorf("Slackbot said %q", m.Text)
	}
}

// TestSlashTopicSendsCleanedTopic: /topic sends the value as
// submitChannelTopic does (clean_message_text, phone numbers linked),
// with its reason.
func TestSlashTopicSendsCleanedTopic(t *testing.T) {
	c, _, fs := cmdComposer(t, "")
	fs.answer("conversations.setTopic", map[string]any{"ok": true, "channel": map[string]any{"id": tChGen, "name": "general"}})
	grantsIn(t, c, fs, tChGen, "SET_CHANNEL_TOPIC") // the header's topic check asked when the channel opened
	p := submitWait(t, c, fs, cmdDoc("/topic R&D <sync> at 10"), "conversations.setTopic", 1)[0]
	pref := uipref.Get()
	raw := TextyValue(cmdDoc("R&D <sync> at 10"))
	want := LinkifyPhoneNumbers(CleanMessageText(raw, pref.ConvertEmoticons && !pref.EmojiAsText))
	if p.Get("topic") != want || want == raw || p.Get("reason") != "submitChannelTopic" {
		t.Errorf("setTopic topic %q reason %q, want %q (not the raw %q)", p.Get("topic"), p.Get("reason"), want, raw)
	}
}

// TestSinglePassLeavesSecondName: a name right after a resolved one stays
// text, as Slack's one rNCs pass leaves it: "/invite @bobby/@carol"
// invites Bobby alone.
func TestSinglePassLeavesSecondName(t *testing.T) {
	c, _, fs := cmdComposer(t, "", fixSeed)
	p := submitWait(t, c, fs, cmdDoc("/invite @bobby/@carol"), "conversations.invite", 1)[0]
	if p.Get("users") != tBobby {
		t.Errorf("conversations.invite users %q, want %s alone", p.Get("users"), tBobby)
	}
}

// TestTypedNameFilters: searchLocal leaves out deactivated members,
// workflow bots, profile-only users and members an information barrier
// keeps away, so their names stay text; two members of one display name
// leave it text too (no frecency lead).
func TestTypedNameFilters(t *testing.T) {
	c, _, _ := cmdComposer(t, "", fixSeed)
	for _, typed := range []string{"hi @gone", "hi @flowbot", "hi @pat", "hi @walled", "hi @sam"} {
		if d := c.prepareMessage(cmdDoc(typed)); len(atoms(d)) != 0 {
			t.Errorf("%q became %v", typed, atoms(d)[0])
		}
	}
	if d := c.prepareMessage(cmdDoc("hi @carol")); len(atoms(d)) != 1 || atoms(d)[0].ID != tCarol {
		t.Errorf("@carol: %v", atoms(d))
	}
}

// TestSlugNormalizeFullToHalf: normalizeName's i18n_deburr has kN/G's
// fullToHalf: full-width letters are ASCII, katakana half width (a voiced
// one two characters), the ideographic space a space.
func TestSlugNormalizeFullToHalf(t *testing.T) {
	for in, want := range map[string]string{
		"\uff20\uff2a\uff2f\uff28\uff2e": "@john",
		"\u30ac\u30a4":                   "\uff76\uff9e\uff72",
		"a\u3000b":                       "a b",
		"Jos\u00e9":                      "jose",
	} {
		if got := slugNormalize(in); got != want {
			t.Errorf("slugNormalize(%q) = %q, want %q", in, got, want)
		}
	}
}

// TestStoreReadOnlyForNames: a message without a typed name reads no
// keywords, so no permissions/info goes out for it; a typed keyword asks.
func TestStoreReadOnlyForNames(t *testing.T) {
	c, _, fs := cmdComposer(t, "")
	perms := func() int { return len(fs.called("edge/permissions/info")) }
	c.prepareMessage(cmdDoc("plain words"))
	c.prepareMessage(cmdDoc("hi @alice"))
	for end := time.Now().Add(time.Second); time.Now().Before(end); time.Sleep(20 * time.Millisecond) {
		if perms() != 0 {
			t.Fatalf("%d permissions/info for messages without a keyword", perms())
		}
	}
	c.prepareMessage(cmdDoc("hi @here"))
	untilUI(t, "the keyword's permissions", func() bool { return perms() > 0 })
}

// TestTypedKeywordsThroughComposer: a typed @here the viewer may use
// becomes the keyword and gets the warning; in a thread, or without the
// permission, it stays text and nothing asks.
func TestTypedKeywordsThroughComposer(t *testing.T) {
	app, fs := broadcastApp(t, nil)
	people(fs, 30)
	warnPrefs(app, map[string]any{"warn_before_at_channel": "always", "loud_channel_mentions_limit": 10000})
	stubTyping(t)
	withTaut(t, nil)
	if _, err := app.LoadBroadcastPerms(context.Background(), tChOpen); err != nil {
		t.Fatal(err)
	}
	h := &cmdHost{slickHost: slickHost{app: app, th: theme.New(false)}}
	send := func(ts string) (*confirmRecorder, *Doc, error) {
		clearDraft(tChOpen, ts)
		c := New(h, tChOpen, ts)
		defer func() { c.Close(); clearDraft(tChOpen, ts) }()
		doc, st := prepareWithStore(c, cmdDoc("standup @here"))
		rec := &confirmRecorder{ok: false}
		j := &sendJob{api: coreAPI{app}, ch: tChOpen, ts: ts, o: processedOutgoing(doc, st), draftID: tChOpen,
			view: c.viewContext(), warn: ts == "", confirm: rec.confirm, store: st}
		ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cancel()
		return rec, doc, j.run(ctx)
	}
	rec, doc, err := send("")
	if as := atoms(doc); len(as) != 1 || as[0].Kind != AtomBroadcast || as[0].ID != "here" {
		t.Fatalf("typed @here: %v", as)
	}
	if !isShoutyRooster(err) || rec.n() != 1 {
		t.Errorf("the warning: %v, %d asks", err, rec.n())
	}
	rec, doc, _ = send("1700000000.000100")
	if len(atoms(doc)) != 0 || rec.n() != 0 {
		t.Errorf("in a thread: %v, %d asks", atoms(doc), rec.n())
	}
	if n := len(fs.called("chat.postMessage")); n != 1 {
		t.Errorf("%d posts, want the thread's only", n)
	}
}

// TestEditKeywordsFollowTheView: an edit takes keywords by where it is
// made (IHC0's inThread from the view context): a thread's root edited
// in the channel resolves a typed @here, the same edit in its thread
// doesn't.
func TestEditKeywordsFollowTheView(t *testing.T) {
	app, _ := broadcastApp(t, nil)
	if _, err := app.LoadBroadcastPerms(context.Background(), tChOpen); err != nil {
		t.Fatal(err)
	}
	h := &cmdHost{slickHost: slickHost{app: app, th: theme.New(false)}}
	root := "1700000000.000100"
	e := NewEdit(h, core.Message{Channel: tChOpen, TS: root, ThreadTS: root, User: tSelf, Text: "ping"}, nil)
	t.Cleanup(e.Close)
	if d := e.prepareMessage(cmdDoc("ping @here")); len(atoms(d)) != 1 || atoms(d)[0].Kind != AtomBroadcast {
		t.Errorf("edited in the channel: %v", atoms(d))
	}
	setEditInThread(e, true)
	if d := e.prepareMessage(cmdDoc("ping @here")); len(atoms(d)) != 0 {
		t.Errorf("edited in the thread: %v", atoms(d))
	}
}

// TestReminderDialogSaves: "/remind" alone's dialog saves the
// Description's blocks (saved.add item_type reminder).
func TestReminderDialogSaves(t *testing.T) {
	c, _, fs := cmdComposer(t, "")
	m, in := reminderDialog(c)
	if m == nil || in == nil || m.Title() != "Reminder" {
		t.Fatal("no Reminder dialog")
	}
	in.SetText("stretch")
	if !m.Press("Save") {
		t.Fatal("no Save")
	}
	untilUI(t, "saved.add", func() bool { return len(fs.called("saved.add")) == 1 })
	p := fs.called("saved.add")[0]
	var blocks []map[string]any
	if p.Get("item_type") != "reminder" || json.Unmarshal([]byte(p.Get("description")), &blocks) != nil ||
		!strings.Contains(p.Get("description"), `"text":"stretch"`) {
		t.Errorf("saved.add %v", p)
	}
}

// TestTypedChannelPicksOwnWorkspace: a typed #name picks the channel of
// that name that isn't another workspace's (createPotentialMentionChannel
// Object's isFromAnotherTeam, read from the store's context team), however
// the snapshot orders the two.
func TestTypedChannelPicksOwnWorkspace(t *testing.T) {
	c, _, _ := cmdComposer(t, "", func(s *store.Store) {
		s.PutConv(slack.Conversation{ID: "C0AAOTHER1", Name: "general", IsChannel: true, IsMember: true, ContextTeamID: "T0OTHERWS1"})
	})
	if as := atoms(c.prepareMessage(cmdDoc("see #general"))); len(as) != 1 || as[0].ID != tChGen {
		t.Errorf("#general became %v, want %s", as, tChGen)
	}
}
