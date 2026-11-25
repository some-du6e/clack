package core

import (
	"context"
	"encoding/json"
	"fmt"
	"reflect"
	"runtime"
	"strings"
	"testing"
	"time"

	"github.com/some-du6e/clack/internal/fakeslack"
	"github.com/some-du6e/clack/internal/richtext"
	"github.com/some-du6e/clack/internal/slack"
	"github.com/some-du6e/clack/internal/store"
)

// atLeast4Procs runs the test with 4 Ps at least, and fails it where
// convertEach has no helpers (a machine of 2 cores or fewer): the test
// would compare one by one with itself.
func atLeast4Procs(t *testing.T) {
	if cap(convertHelpers) == 0 {
		t.Fatalf("convertEach has no helpers with GOMAXPROCS %d at start: it converts one by one", runtime.GOMAXPROCS(0))
	}
	if n := runtime.GOMAXPROCS(0); n < 4 {
		runtime.GOMAXPROCS(4)
		t.Cleanup(func() { runtime.GOMAXPROCS(n) })
	}
}

// A page past convertEachMin is converted on every core, each message on
// its own: the same messages, in the same order, as converting them one
// by one (#giant-wall's page of 28 walls took FromMessageIn 240 ms in a
// row). Run under -race in flake-check's history-convert.
func TestTimelineConvertsBigPagesLikeOneByOne(t *testing.T) {
	atLeast4Procs(t)
	ctx := context.Background()
	sz := fakeslack.Small
	sz.Giant = 12 // 11 walls of 908k characters and a short one
	fs, err := fakeslack.Start(sz)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { fs.Close() })
	a := New(slack.New(fs.Creds()), store.New(t.TempDir()))
	t.Cleanup(a.Shutdown)
	if err := a.Boot(ctx); err != nil {
		t.Fatal(err)
	}
	p, err := a.C.History(ctx, fakeslack.GiantWallID, slack.HistoryOpts{Limit: 28, Inclusive: true})
	if err != nil {
		t.Fatal(err)
	}
	size := 0
	for _, m := range p.Messages {
		size += len(m.Text) + len(m.Blocks)
	}
	if size < convertEachMin {
		t.Fatalf("the page is %d bytes, under convertEachMin: this test no longer converts on every core", size)
	}
	got := a.timeline(fakeslack.GiantWallID, p.Messages)
	var want []Message
	for i := range p.Messages {
		m := &p.Messages[i]
		if m.Subtype == "thread_broadcast" || m.ThreadTS == "" || m.ThreadTS == m.TS || m.ReplyBroadcast {
			want = append(want, a.message(fakeslack.GiantWallID, m))
		}
	}
	if len(got) != len(want) || len(got) == 0 {
		t.Fatalf("%d messages converted, want %d", len(got), len(want))
	}
	for i := range want {
		if !reflect.DeepEqual(got[i], want[i]) {
			t.Errorf("message %d (%s) converted on its own core differs from one by one", i, want[i].TS)
		}
	}
}

// A big page of rich messages converted on every core is the page one by
// one: mentions of people the store has and one it doesn't, a channel and
// a usergroup, a custom emoji and an alias, reactions, a bot sender,
// files, a canvas link and a workflow mention under a resolver
// (richtext.SetWorkflowSlugs, the ui's in the app). Converting asks for
// what it misses (members, canvases): the page is converted one by one
// until two conversions agree, every id known or not found, so the
// conversions compared read the same store.
func TestTimelineConvertsRichPagesLikeOneByOne(t *testing.T) {
	atLeast4Procs(t)
	ctx := context.Background()
	fs, err := fakeslack.Start(fakeslack.Small)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { fs.Close() })
	a := New(slack.New(fs.Creds()), store.New(t.TempDir()))
	t.Cleanup(a.Shutdown)
	if err := a.Boot(ctx); err != nil {
		t.Fatal(err)
	}
	var em struct {
		Emoji map[string]string `json:"emoji"`
	}
	if err := a.C.Call(ctx, "emoji.list", nil, &em); err != nil {
		t.Fatal(err)
	}
	custom, alias := "", ""
	for n, u := range em.Emoji {
		switch {
		case strings.HasPrefix(u, "alias:") && (alias == "" || n < alias):
			alias = n
		case !strings.HasPrefix(u, "alias:") && (custom == "" || n < custom):
			custom = n
		}
	}
	if custom == "" || alias == "" {
		t.Fatalf("control: the fake has no custom emoji (%q) or alias (%q)", custom, alias)
	}
	richtext.SetWorkflowSlugs(func(id string) (richtext.WorkflowSlug, bool) {
		if id == fakeslack.TriggerDeploy {
			return richtext.WorkflowSlug{Title: "Run deploy", Icon: fs.URL + "img/wf-deploy-96.png"}, true
		}
		return richtext.WorkflowSlug{}, false
	})
	t.Cleanup(func() { richtext.SetWorkflowSlugs(nil) })
	users, chans := fs.UserIDs(), fs.ChannelIDs()
	ch := chans[0]
	pad := strings.Repeat("synthetic words ", 7000) // 112 KB: twelve of them pass convertEachMin
	var msgs []slack.Message
	for i := 0; i < 12; i++ {
		who, other, where := users[i%len(users)], users[(i+3)%len(users)], chans[(i+1)%len(chans)]
		blocks := fmt.Sprintf(`[{"type":"rich_text","block_id":"b%d","elements":[{"type":"rich_text_section","elements":[`+
			`{"type":"text","text":"%s "},{"type":"user","user_id":"%s"},{"type":"user","user_id":"U0NOPE%04d"},`+
			`{"type":"channel","channel_id":"%s"},{"type":"usergroup","usergroup_id":"S0FAKE00001"},`+
			`{"type":"emoji","name":"%s"},{"type":"emoji","name":"%s"},{"type":"emoji","name":"thumbsup","unicode":"1f44d"},`+
			`{"type":"link","url":"https://fakeclub.slack.com/docs/%s/F0CANVAS%02d"},`+
			`{"type":"workflow_mention","function_trigger_id":"%s","workflow_id":"Wf0FAKE01","text":"Run deploy","url":"https://slack.com/shortcuts/%s/x"}]}]}]`,
			i, pad, who, i, where, custom, alias, fakeslack.TeamID, i, fakeslack.TriggerDeploy, fakeslack.TriggerDeploy)
		m := slack.Message{Type: "message", TS: fmt.Sprintf("17000%05d.000100", i), Text: pad + " <@" + who + "> <#" + where + "> :" + custom + ":",
			Blocks: json.RawMessage(blocks), Reactions: []slack.Reaction{{Name: custom, Count: 2, Users: []string{who, other}}, {Name: alias, Count: 1, Users: []string{other}}}}
		switch i % 3 {
		case 0:
			m.BotID, m.Username = "B0FAKERICH", "Rich Bot"
			m.BotProfile = &slack.BotProfile{ID: "B0FAKERICH", Name: "Rich Bot", AppID: "A0FAKERICH"}
		default:
			m.User = other
		}
		if i%4 == 1 {
			m.Files = []slack.File{{ID: fmt.Sprintf("F0FAKERICH%02d", i), Name: "notes.txt", Title: "notes", Mimetype: "text/plain", Filetype: "text"}}
		}
		msgs = append(msgs, m)
	}
	size := 0
	for _, m := range msgs {
		size += len(m.Text) + len(m.Blocks)
	}
	if size < convertEachMin {
		t.Fatalf("the page is %d bytes, under convertEachMin", size)
	}
	oneByOne := func() []Message {
		var out []Message
		for i := range msgs {
			out = append(out, a.message(ch, &msgs[i]))
		}
		return out
	}
	prev := oneByOne()
	for dl := time.Now().Add(15 * time.Second); ; {
		time.Sleep(300 * time.Millisecond)
		cur := oneByOne()
		if reflect.DeepEqual(cur, prev) {
			break
		}
		if time.Now().After(dl) {
			t.Fatal("the page converted one by one kept changing: the store's lookups never settled")
		}
		prev = cur
	}
	got := a.timeline(ch, msgs)
	want := oneByOne()
	if len(got) != len(want) {
		t.Fatalf("%d messages converted on every core, %d one by one", len(got), len(want))
	}
	for i := range want {
		if !reflect.DeepEqual(got[i], want[i]) {
			t.Errorf("message %d (%s) converted on its own core differs from one by one", i, want[i].TS)
		}
	}
}
