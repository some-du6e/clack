package core

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"slices"
	"sync"
	"testing"

	"github.com/some-du6e/yoloslackgoclient/internal/fakeslack"
	"github.com/some-du6e/yoloslackgoclient/internal/slack"
	"github.com/some-du6e/yoloslackgoclient/internal/store"
)

// Broadcast keyword rules (broadcast.go), against Slack's selectors.
// Fixtures are the permissions/info answers a real account gets: a
// regular Hack Club member has neither CAN_AT_CHANNEL nor
// CAN_AT_EVERYONE anywhere (2026-11-08); a channel manager has the first.

var (
	cvPublic  = Conv{ID: "C1", Kind: "channel", NumMembers: 40}
	cvPrivate = Conv{ID: "G1", Kind: "private", NumMembers: 8}
	cvGeneral = Conv{ID: "C0", Kind: "channel", IsGeneral: true, NumMembers: 100000}
	cvMPIM    = Conv{ID: "G2", Kind: "mpim"}
	cvIM      = Conv{ID: "D1", Kind: "im"}

	permNone    = BroadcastPerms{Known: true}
	permChannel = BroadcastPerms{Known: true, AtChannel: true}
	permAll     = BroadcastPerms{Known: true, AtChannel: true, AtEveryone: true}
)

func TestBroadcastKeywords(t *testing.T) {
	for _, tc := range []struct {
		name string
		c    Conv
		p    BroadcastPerms
		want []string
	}{
		{"member, public", cvPublic, permNone, nil},
		{"unknown perms", cvPublic, BroadcastPerms{}, nil},
		{"manager, public", cvPublic, permChannel, []string{"channel", "here"}},
		{"manager, private", cvPrivate, permChannel, []string{"channel", "here"}},
		{"group DM", cvMPIM, permChannel, []string{"channel", "here"}},
		{"DM", cvIM, permAll, nil},
		{"everyone only in general", cvPublic, permAll, []string{"channel", "here"}},
		{"admin, general", cvGeneral, permAll, []string{"everyone", "channel", "here"}},
		{"general without CAN_AT_EVERYONE", cvGeneral, permChannel, []string{"channel", "here"}},
		{"CAN_AT_EVERYONE alone", cvGeneral, BroadcastPerms{Known: true, AtEveryone: true}, []string{"everyone"}},
	} {
		if got := BroadcastKeywords(tc.c, tc.p); !slices.Equal(got, tc.want) {
			t.Errorf("%s: %v, want %v", tc.name, got, tc.want)
		}
	}
}

func TestProhibitedBroadcast(t *testing.T) {
	for _, tc := range []struct {
		name string
		s    BroadcastSend
		want string // "" = allowed
	}{
		{"no keyword", BroadcastSend{Conv: cvPublic, Perms: permNone}, ""},
		{"allowed @channel", BroadcastSend{Conv: cvPublic, Perms: permChannel, Used: []string{"channel"}}, ""},
		{"owner restricted @channel", BroadcastSend{Conv: cvPublic, Perms: permNone, Used: []string{"channel"}},
			"A Workspace Owner has restricted the use of *@channel* messages."},
		{"@here named over @channel", BroadcastSend{Conv: cvPublic, Perms: permNone, Used: []string{"channel", "here"}},
			"A Workspace Owner has restricted the use of *@here* messages."},
		{"unknown perms block", BroadcastSend{Conv: cvPublic, Used: []string{"here"}},
			"A Workspace Owner has restricted the use of *@here* messages."},
		{"in a thread, even when allowed", BroadcastSend{Conv: cvPublic, Perms: permChannel, ThreadTS: "1.2", Used: []string{"channel"}},
			"Usage of *@channel* is restricted in this large channel."},
		{"disabled in the channel", BroadcastSend{Conv: cvPublic, Perms: permChannel, Disabled: true, Used: []string{"here"}},
			"*@here* has been disabled in this channel, so your message cannot be sent."},
		{"allowed @everyone", BroadcastSend{Conv: cvGeneral, Perms: permAll, Used: []string{"everyone"}}, ""},
		{"@everyone outside general", BroadcastSend{Conv: cvPublic, Perms: permAll, Used: []string{"everyone"}},
			"Usage of *@everyone* is restricted in the large general channel."},
		{"@everyone, no permission", BroadcastSend{Conv: cvGeneral, Perms: permChannel, Used: []string{"everyone"}},
			"A Workspace Owner has restricted the use of *@everyone* messages."},
		{"@everyone as a guest", BroadcastSend{Conv: cvGeneral, Perms: permChannel, Restricted: true, Used: []string{"everyone"}},
			"Your account is restricted, and you cannot send *@everyone* messages."},
		{"@everyone, channel disabled", BroadcastSend{Conv: cvGeneral, Perms: permAll, Disabled: true, Used: []string{"everyone"}},
			"*@everyone* has been disabled in this channel, so your message cannot be sent."},
		{"@here checked before @everyone", BroadcastSend{Conv: cvGeneral, Perms: BroadcastPerms{Known: true, AtEveryone: true}, Used: []string{"everyone", "here"}},
			"A Workspace Owner has restricted the use of *@here* messages."},
	} {
		got, no := ProhibitedBroadcast(tc.s)
		if no != (tc.want != "") || got != tc.want {
			t.Errorf("%s: %q %v, want %q", tc.name, got, no, tc.want)
		}
	}
}

// TestLoadBroadcastPerms: permissions/info through the edge API, cached
// an hour, shared by concurrent callers; nothing allowed when the
// answer lists nothing.
func TestLoadBroadcastPerms(t *testing.T) {
	fs, err := fakeslack.Start(fakeslack.Small)
	if err != nil {
		t.Fatal(err)
	}
	defer fs.Close()
	ids := fs.ChannelIDs()
	fs.Permissions = map[string][]string{ids[0]: {PermAtChannel}, ids[1]: {}}
	a := New(slack.New(fs.Creds()), store.New(""))
	ctx := context.Background()

	var wg sync.WaitGroup
	got := make([]BroadcastPerms, 4)
	for i := range got {
		wg.Add(1)
		go func() {
			defer wg.Done()
			got[i], _ = a.LoadBroadcastPerms(ctx, ids[0])
		}()
	}
	wg.Wait()
	for _, p := range got {
		if p != permChannel {
			t.Fatalf("perms %+v, want %+v", p, permChannel)
		}
	}
	if n := fs.CallCount("edge/permissions/info"); n != 1 {
		t.Errorf("%d permissions/info calls for one channel, want 1 (shared, then cached)", n)
	}
	if p := a.BroadcastPerms(ids[0]); p != permChannel {
		t.Errorf("cached %+v", p)
	}
	if p, err := a.LoadBroadcastPerms(ctx, ids[1]); err != nil || p != permNone {
		t.Errorf("restricted channel: %+v %v", p, err)
	}
}

// TestBroadcastPermsRequest: the request is Slack's fetchPermissions:
// channel_id for channels; team_ids (read from allowed_per_team) for a
// group DM on Grid; nothing asked for a DM.
func TestBroadcastPermsRequest(t *testing.T) {
	var mu sync.Mutex
	var bodies []map[string]any
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		b, _ := io.ReadAll(r.Body)
		var m map[string]any
		_ = json.Unmarshal(b, &m)
		mu.Lock()
		bodies = append(bodies, m)
		mu.Unlock()
		if r.URL.Path != "/E1/permissions/info" {
			http.NotFound(w, r)
			return
		}
		ans := map[string]any{"ok": true, "allowed": []string{"CAN_AT_CHANNEL"}, "allowed_per_team": map[string]any{}}
		if _, ok := m["team_ids"]; ok {
			ans = map[string]any{"ok": true, "allowed": []string{}, "allowed_per_team": map[string]any{"T1": []string{"CAN_AT_CHANNEL"}}}
		}
		_ = json.NewEncoder(w).Encode(ans)
	}))
	defer srv.Close()
	c := slack.New(slack.Creds{Token: "xoxc-test", Cookie: "xoxd-test", UserID: "U1", Team: "T1", EnterpriseID: "E1",
		TeamURL: srv.URL + "/", EnterpriseURL: srv.URL + "/"})
	c.EdgeURL = srv.URL + "/"
	s := store.New("")
	s.PutConv(slack.Conversation{ID: "C1", Name: "pub", IsChannel: true, IsMember: true})
	s.PutConv(slack.Conversation{ID: "G2", Name: "mpdm-a--b-1", IsMPIM: true, IsMember: true})
	s.PutConv(slack.Conversation{ID: "D1", IsIM: true, User: "U2", IsMember: true})
	s.Self = slack.User{ID: "U1"}
	a := New(c, s)
	ctx := context.Background()
	for _, ch := range []string{"C1", "G2", "D1"} {
		p, err := a.LoadBroadcastPerms(ctx, ch)
		if err != nil {
			t.Fatalf("%s: %v", ch, err)
		}
		if want := ch != "D1"; p.AtChannel != want || !p.Known {
			t.Errorf("%s: %+v", ch, p)
		}
	}
	mu.Lock()
	defer mu.Unlock()
	if len(bodies) != 2 {
		t.Fatalf("%d requests, want 2 (none for the DM)", len(bodies))
	}
	if bodies[0]["channel_id"] != "C1" || bodies[0]["user_id"] != "U1" {
		t.Errorf("channel request %v", bodies[0])
	}
	perms, _ := bodies[0]["permissions"].([]any)
	if len(perms) != 2 || perms[0] != PermAtChannel || perms[1] != PermAtEveryone {
		t.Errorf("permissions %v", bodies[0]["permissions"])
	}
	if teams, _ := bodies[1]["team_ids"].([]any); len(teams) != 1 || teams[0] != "T1" || bodies[1]["channel_id"] != nil {
		t.Errorf("group DM request %v", bodies[1])
	}
}

// TestLocalEphemeral: Slackbot's local message lands in the timeline (or
// thread), marked ephemeral, and is announced.
func TestLocalEphemeral(t *testing.T) {
	s := store.New("")
	s.PutConv(slack.Conversation{ID: "C1", Name: "pub", IsChannel: true, IsMember: true})
	s.PrependOlder("C1", []slack.Message{{Type: "message", TS: "1700000000.000100", User: "U2", Text: "synthetic"}}, false) // the open channel
	a := New(slack.New(slack.Creds{Token: "xoxc-test", UserID: "U1", Team: "T1"}), s)
	evs, stop := a.Subscribe()
	defer stop()
	ts := a.LocalEphemeral("C1", "", "A Workspace Owner has restricted the use of *@channel* messages.")
	m, ok := a.S.Find("C1", ts)
	if !ok || m.User != "USLACKBOT" {
		t.Fatalf("message %+v %v", m, ok)
	}
	if !a.Interactive().Ephemeral("C1", ts) {
		t.Error("not marked ephemeral")
	}
	for ev := range evs {
		if ev.Type == "message" && ev.TS == ts {
			break
		}
	}
	a.S.SetThread("C1", "1700000000.000100", []slack.Message{{Type: "message", TS: "1700000000.000100", ThreadTS: "1700000000.000100", User: "U2"}}) // the open thread
	rts := a.LocalEphemeral("C1", "1700000000.000100", "x")
	if th, _ := a.S.Thread("C1", "1700000000.000100"); len(th) == 0 || th[len(th)-1].TS != rts {
		t.Errorf("thread reply missing: %v", th)
	}
}
