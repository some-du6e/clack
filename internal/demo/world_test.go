package demo

import (
	"context"
	"encoding/json"
	"net/url"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/coder/websocket"

	"github.com/some-du6e/yoloslackgoclient/internal/fakeslack"
	"github.com/some-du6e/yoloslackgoclient/internal/slack"
)

// The world against the fake over HTTP and its socket. Speed is high and
// the ambient timers are off (Quiet): replies to the viewer, polls, the
// agent and huddle rings are driven directly, so every test takes well
// under a second.

// testWorkspace is a small hand-built Workspace (Build may not be ready
// while the content is written; the world only needs the shape).
func testWorkspace(base string, now time.Time) *Workspace {
	ts := func(ago time.Duration, n int) string { return microTS(now.Add(-ago).UnixMicro() + int64(n)) }
	cast := []struct{ id, handle, name string }{
		{"U0DEMOGORD", "gordon", "Gordon Tibbs"}, {"U0DEMOPRIY", "priya", "Priya Raman"}, {"U0DEMODALE", "dale", "Dale Hurst"},
		{"U0DEMOMARI", "marisol", "Marisol Ortega"}, {"U0DEMOBEVO", "bev", "Bev Okafor"}, {"U0DEMOTHEO", "theo", "Theo Lindqvist"},
		{"U0DEMOWEND", "wendell", "Wendell Price"}, {"U0DEMORUTH", "ruth", "Ruth Abernathy"}, {"U0DEMOKIPM", "kip", "Kip Marlowe"},
		{"U0DEMONADI", "nadia", "Nadia Brooks"},
	}
	img := func(id string) slack.Profile {
		u := base + "img/demo/avatar/" + id
		return slack.Profile{Image24: u + "/24.png", Image48: u + "/48.png", Image72: u + "/72.png", Image192: u + "/192.png"}
	}
	ws := &Workspace{Now: now, Base: base, Emoji: map[string]string{"rip-lamby": base + "img/demo/emoji/rip-lamby.png"},
		History: map[string][]slack.Message{}, Replies: map[string]map[string][]slack.Message{}, Pins: map[string][]string{},
		Unread: map[string]Unread{}}
	self := slack.User{ID: SelfID, TeamID: TeamID, Name: "jordan", RealName: "Jordan Pike", Profile: img(SelfID)}
	self.Profile.FirstName, self.Profile.RealName, self.Profile.DisplayName = "Jordan", "Jordan Pike", "jordan"
	ws.Self = self
	ws.Users = append(ws.Users, self)
	ws.People = append(ws.People, Person{ID: SelfID, Handle: "jordan", Name: "Jordan Pike", Self: true})
	for _, c := range cast {
		p := img(c.id)
		p.RealName, p.DisplayName = c.name, c.handle
		p.FirstName = strings.Fields(c.name)[0]
		ws.Users = append(ws.Users, slack.User{ID: c.id, TeamID: TeamID, Name: c.handle, RealName: c.name, Profile: p, Presence: "active"})
		ws.People = append(ws.People, Person{ID: c.id, Handle: c.handle, Name: c.name,
			Statuses: []Status{{Emoji: ":coffee:", Text: "Refilling"}, {Emoji: ":printer:", Text: "Negotiating with Prinny"}}})
	}
	for _, b := range []Bot{
		{UserID: InkwellID, BotID: InkwellBotID, AppID: InkwellAppID, Name: "Inkwell", Desc: "AI coding agent", Color: "3b5bdb"},
		{UserID: PulseID, BotID: PulseBotID, AppID: PulseAppID, Name: "Pulse", Desc: "Polls and stand-ups", Color: "e8590c"},
	} {
		ws.Bots = append(ws.Bots, b)
		ws.Users = append(ws.Users, slack.User{ID: b.UserID, TeamID: TeamID, Name: strings.ToLower(b.Name), RealName: b.Name, IsBot: true,
			Profile: slack.Profile{RealName: b.Name, BotID: b.BotID, APIAppID: b.AppID}})
	}
	ch := func(id, name string) slack.Conversation {
		return slack.Conversation{ID: id, Name: name, IsChannel: true, IsMember: true, IsGeneral: id == GeneralID,
			Created: now.Add(-400 * 24 * time.Hour).Unix(), ContextTeamID: TeamID, Topic: &slack.Purpose{}, Purpose: &slack.Purpose{}}
	}
	ws.Convs = []slack.Conversation{ch(GeneralID, "general"), ch(RandomID, "random"), ch(SalesID, "sales-leaderboard"),
		ch(PartyID, "party-planning-crew"), ch(ITHelpID, "it-help"), ch(WareID, "warehouse"), ch(LunchID, "lunch-orders"),
		ch(KudosID, "kudos"), ch(SantaID, "secret-santa-2026"), ch(CodeID, "inkwell-order-portal")}
	ws.Convs[8].IsPrivate, ws.Convs[8].IsChannel, ws.Convs[8].IsGroup = true, false, true
	ws.Convs[9].Properties = json.RawMessage(`{"agent_session":{"status":"active","title":"` + DemoPRTitle + `"},"code_channel":{"repo":"` +
		DemoRepo + `","branch":"` + DemoBranch + `","pr_status":"open","ci_state":"success"}}`)
	for _, d := range []struct{ id, user string }{{"D0DEMOGORD", "U0DEMOGORD"}, {"D0DEMOPRIY", "U0DEMOPRIY"},
		{"D0DEMOMARI", "U0DEMOMARI"}, {"D0DEMOTHEO", "U0DEMOTHEO"}, {"D0DEMOKIPM", "U0DEMOKIPM"}, {"D0DEMOINKW", InkwellID}} {
		ws.Convs = append(ws.Convs, slack.Conversation{ID: d.id, IsIM: true, User: d.user, IsOpen: true, ContextTeamID: TeamID})
	}
	ws.Convs = append(ws.Convs, slack.Conversation{ID: GroupDMID, Name: "mpdm-priya--dale--kip--jordan-1", IsMPIM: true, IsGroup: true,
		IsPrivate: true, IsMember: true, IsOpen: true, Members: []string{SelfID, "U0DEMOPRIY", "U0DEMODALE", "U0DEMOKIPM"}})
	// #general: Gordon's announcement with a thread you replied in, and
	// a mention of you
	parent := slack.Message{Type: "message", TS: ts(3*time.Hour, 1), User: "U0DEMOGORD", Text: "RIP Lamby the laminator :rip-lamby:",
		ThreadTS: ts(3*time.Hour, 1), ReplyCount: 2, ReplyUsers: []string{"U0DEMOMARI", SelfID}, LatestReply: ts(2*time.Hour, 3)}
	ws.History[GeneralID] = []slack.Message{
		{Type: "message", TS: ts(time.Hour, 1), User: "U0DEMOPRIY", Text: "<@" + SelfID + "> you're on my team for the bet"},
		parent,
		{Type: "message", TS: ts(5*time.Hour, 1), User: SelfID, Text: "morning all", Reactions: []slack.Reaction{{Name: "wave", Count: 1, Users: []string{"U0DEMOGORD"}}}},
	}
	ws.Replies[GeneralID] = map[string][]slack.Message{parent.TS: {
		{Type: "message", TS: ts(2*time.Hour, 2), User: "U0DEMOMARI", Text: "it's been a good run", ThreadTS: parent.TS},
		{Type: "message", TS: ts(2*time.Hour, 3), User: SelfID, Text: "gone too soon", ThreadTS: parent.TS},
	}}
	ws.Pins[GeneralID] = []string{parent.TS}
	ws.Unread[GeneralID] = Unread{LastRead: ts(90*time.Minute, 0), Mentions: 1}
	ws.History["D0DEMOGORD"] = []slack.Message{{Type: "message", TS: ts(30*time.Minute, 1), User: "U0DEMOGORD", Text: "how's week 2?"}}
	ws.Unread["D0DEMOGORD"] = Unread{LastRead: ts(40*time.Minute, 0), Mentions: 1}
	ws.Sections = []Section{{ID: "L0DEMOSTAR", Type: "stars", Channels: []string{GeneralID}},
		{ID: "L0DEMOFUN", Name: "Fun", Emoji: "tada", Type: "standard", Channels: []string{RandomID, PartyID}},
		{ID: "L0DEMOCHAN", Type: "channels"}, {ID: "L0DEMODMS0", Type: "direct_messages"}}
	return ws
}

type testWorld struct {
	t  *testing.T
	fs *fakeslack.Server
	ws *Workspace
	w  *World
	c  *slack.Client
}

func newTestWorld(t *testing.T) *testWorld {
	t.Helper()
	fs, err := fakeslack.Start(fakeslack.Size{Users: 3}) // fewer and generate() indexes past the end
	if err != nil {
		t.Fatal(err)
	}
	now := time.Now()
	ws := testWorkspace(fs.URL, now)
	w := newWorld(fs, ws, Options{Speed: 400, Quiet: true, Now: now, Seed: 7, Logf: t.Logf})
	w.Start(context.Background())
	t.Cleanup(func() {
		w.Stop()
		fs.Close()
	})
	return &testWorld{t: t, fs: fs, ws: ws, w: w, c: slack.New(fs.Creds())}
}

func (tw *testWorld) call(method string, kv ...string) map[string]any {
	tw.t.Helper()
	p := url.Values{}
	for i := 0; i+1 < len(kv); i += 2 {
		p.Set(kv[i], kv[i+1])
	}
	var out map[string]any
	if err := tw.c.Call(context.Background(), method, p, &out); err != nil {
		tw.t.Fatalf("%s: %v", method, err)
	}
	return out
}

func (tw *testWorld) idle() {
	tw.t.Helper()
	if !tw.w.WaitIdle(5 * time.Second) {
		tw.t.Fatal("bots never went idle")
	}
}

// socket collects the realtime events the fake pushes.
type socket struct {
	mu  sync.Mutex
	evs []map[string]any
}

func (tw *testWorld) socket() *socket {
	tw.t.Helper()
	ctx, cancel := context.WithCancel(context.Background())
	c, _, err := websocket.Dial(ctx, "ws"+strings.TrimPrefix(tw.fs.URL, "http")+"ws", nil)
	if err != nil {
		tw.t.Fatal(err)
	}
	s := &socket{}
	hello := make(chan struct{})
	go func() {
		defer close(hello)
		for {
			_, b, err := c.Read(ctx)
			if err != nil {
				return
			}
			var ev map[string]any
			if json.Unmarshal(b, &ev) == nil {
				if ev["type"] == "hello" {
					hello <- struct{}{}
					continue
				}
				s.mu.Lock()
				s.evs = append(s.evs, ev)
				s.mu.Unlock()
			}
		}
	}()
	<-hello
	tw.t.Cleanup(func() {
		cancel()
		c.CloseNow()
	})
	return s
}

// wait is the first event f matches, waiting up to a second for it.
func (s *socket) wait(f func(map[string]any) bool) map[string]any {
	for deadline := time.Now().Add(time.Second); time.Now().Before(deadline); time.Sleep(5 * time.Millisecond) {
		s.mu.Lock()
		for _, e := range s.evs {
			if f(e) {
				s.mu.Unlock()
				return e
			}
		}
		s.mu.Unlock()
	}
	return nil
}

func (s *socket) types() map[string]int {
	// let the reader catch up with what was pushed
	for n, i := -1, 0; i < 100; i++ {
		s.mu.Lock()
		m := len(s.evs)
		s.mu.Unlock()
		if m == n {
			break
		}
		n = m
		time.Sleep(20 * time.Millisecond)
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	out := map[string]int{}
	for _, e := range s.evs {
		t, _ := e["type"].(string)
		if st, _ := e["subtype"].(string); st != "" && t == "message" {
			t += "/" + st
		}
		out[t]++
	}
	return out
}

func TestWorldBoot(t *testing.T) {
	tw := newTestWorld(t)
	if a := tw.call("auth.test"); a["team"] != TeamName || a["user_id"] != SelfID {
		t.Errorf("auth.test: %v", a)
	}
	team := tw.call("team.info")["team"].(map[string]any)
	icon := team["icon"].(map[string]any)
	if team["name"] != TeamName || !strings.HasSuffix(icon["image_88"].(string), "img/demo/team/88.png") {
		t.Errorf("team.info: %v", team)
	}
	chans := tw.call("users.conversations", "types", "public_channel,private_channel,mpim,im")["channels"].([]any)
	if len(chans) != len(tw.ws.Convs) {
		t.Errorf("users.conversations: %d channels, want %d", len(chans), len(tw.ws.Convs))
	}
	var counts slack.Counts
	if err := tw.c.Call(context.Background(), "client.counts", nil, &counts); err != nil {
		t.Fatal(err)
	}
	find := func(es []slack.CountEntry, id string) slack.CountEntry {
		for _, e := range es {
			if e.ID == id {
				return e
			}
		}
		t.Fatalf("client.counts: no %s", id)
		return slack.CountEntry{}
	}
	if g := find(counts.Channels, GeneralID); !g.HasUnreads || g.MentionCount != 1 {
		t.Errorf("general counts: %+v", g)
	}
	if d := find(counts.IMs, "D0DEMOGORD"); !d.HasUnreads || d.MentionCount != 1 {
		t.Errorf("gordon DM counts: %+v", d)
	}
	if r := find(counts.Channels, RandomID); r.HasUnreads {
		t.Errorf("random should be read: %+v", r)
	}
	if len(counts.MPIMs) != 1 {
		t.Errorf("mpims: %+v", counts.MPIMs)
	}
	// conversations.mark reads #general
	tw.call("conversations.mark", "channel", GeneralID, "ts", tw.ws.History[GeneralID][0].TS)
	if err := tw.c.Call(context.Background(), "client.counts", nil, &counts); err != nil {
		t.Fatal(err)
	}
	if g := find(counts.Channels, GeneralID); g.HasUnreads || g.MentionCount != 0 {
		t.Errorf("general after mark: %+v", g)
	}
	secs := tw.call("users.channelSections.list")["channel_sections"].([]any)
	if len(secs) != 4 || secs[0].(map[string]any)["next_channel_section_id"] != "L0DEMOFUN" {
		t.Errorf("sections: %v", secs)
	}
	if e := tw.call("emoji.list")["emoji"].(map[string]any); e["rip-lamby"] == nil {
		t.Errorf("emoji.list: %v", e)
	}
	var users struct {
		Results []slack.User `json:"results"`
	}
	if err := tw.c.Edge(context.Background(), "users/info", map[string]any{"updated_ids": map[string]int{"U0DEMOGORD": 0, SelfID: 0}}, &users); err != nil {
		t.Fatal(err)
	}
	if len(users.Results) != 2 {
		t.Errorf("edge users/info: %+v", users.Results)
	}
	var apps struct {
		Results []slack.User `json:"results"`
	}
	if err := tw.c.Edge(context.Background(), "users/list", map[string]any{"filter": "apps", "channels": []string{CodeID}}, &apps); err != nil {
		t.Fatal(err)
	}
	if len(apps.Results) != 1 || apps.Results[0].ID != InkwellID {
		t.Errorf("apps in the code channel: %+v", apps.Results)
	}
	// history pages newest first; replies have the parent first
	h := tw.call("conversations.history", "channel", GeneralID, "limit", "2")
	if ms := h["messages"].([]any); len(ms) != 2 || h["has_more"] != true {
		t.Errorf("history page: %v", h)
	}
	cur := h["response_metadata"].(map[string]any)["next_cursor"].(string)
	if ms := tw.call("conversations.history", "channel", GeneralID, "limit", "2", "cursor", cur)["messages"].([]any); len(ms) != 1 {
		t.Errorf("history page 2: %v", ms)
	}
	parent := tw.ws.History[GeneralID][1].TS
	rs := tw.call("conversations.replies", "channel", GeneralID, "ts", parent)["messages"].([]any)
	if len(rs) != 3 || rs[0].(map[string]any)["ts"] != parent {
		t.Errorf("replies: %v", rs)
	}
	if ps := tw.call("pins.list", "channel", GeneralID)["items"].([]any); len(ps) != 1 {
		t.Errorf("pins: %v", ps)
	}
	// the seeded Activity: the mention, the thread, the DM, the reaction
	feed := tw.call("activity.feed", "types", "at_user,thread_v2,dm,message_reaction")["items"].([]any)
	got := map[string]bool{}
	for _, it := range feed {
		got[it.(map[string]any)["item"].(map[string]any)["type"].(string)] = true
	}
	for _, typ := range []string{"at_user", "thread_v2", "dm", "message_reaction"} {
		if !got[typ] {
			t.Errorf("seeded activity has no %s: %v", typ, feed)
		}
	}
	if s := tw.call("search.modules.messages", "query", "lamby")["items"].([]any); len(s) != 1 {
		t.Errorf("search lamby: %v", s)
	}
}

func TestWorldReplies(t *testing.T) {
	tw := newTestWorld(t)
	sock := tw.socket()
	before := tw.w.ActivityCount()
	out := tw.call("chat.postMessage", "channel", GeneralID, "text", "hi gordon, did anyone fix the laminator?", "client_msg_id", "c-1")
	ts := out["ts"].(string)
	tw.idle()
	th := tw.w.Thread(GeneralID, ts)
	if len(th) == 0 {
		t.Fatal("nobody answered in the thread")
	}
	if th[0].User != "U0DEMOGORD" {
		t.Errorf("first answer from %s, want gordon (named)", th[0].User)
	}
	if strings.Contains(th[0].Text, "{") {
		t.Errorf("unexpanded placeholder: %q", th[0].Text)
	}
	if tw.w.ActivityCount() <= before {
		t.Error("a reply in your thread added no Activity")
	}
	rs := tw.call("conversations.replies", "channel", GeneralID, "ts", ts)["messages"].([]any)
	if len(rs) != len(th)+1 {
		t.Errorf("replies: %d, want %d", len(rs), len(th)+1)
	}
	types := sock.types()
	if types["message"] < 2 || types["user_typing"] == 0 || types["message/message_replied"] == 0 || types["activity"] == 0 {
		t.Errorf("realtime events: %v", types)
	}
	sock.mu.Lock()
	echoed := false
	for _, e := range sock.evs {
		if e["type"] == "message" && e["client_msg_id"] == "c-1" && e["channel"] == GeneralID {
			echoed = true
		}
	}
	sock.mu.Unlock()
	if !echoed {
		t.Error("no echo with the client_msg_id")
	}

	// a DM: answered top level by that person
	tw.w.Post(SelfID, "D0DEMOPRIY", "", "thanks for the help today!")
	tw.idle()
	h := tw.w.History("D0DEMOPRIY")
	if len(h) < 2 || h[0].User != "U0DEMOPRIY" || h[0].ThreadTS != "" {
		t.Errorf("DM answer: %+v", h)
	}
	// #it-help: Theo
	ts = tw.w.Post(SelfID, ITHelpID, "", "my wifi keeps dropping")
	tw.idle()
	if th := tw.w.Thread(ITHelpID, ts); len(th) == 0 || th[0].User != "U0DEMOTHEO" {
		t.Errorf("it-help answer: %+v", th)
	}
	// edits and deletes of your own message
	tw.call("chat.update", "channel", ITHelpID, "ts", ts, "text", "my wifi keeps dropping (fixed it)")
	if m, _ := tw.w.Message(ITHelpID, ts); m.Edited == nil || !strings.Contains(m.Text, "fixed") {
		t.Errorf("chat.update: %+v", m)
	}
	tw.call("reactions.add", "channel", ITHelpID, "timestamp", ts, "name", "tada")
	if m, _ := tw.w.Message(ITHelpID, ts); len(m.Reactions) == 0 {
		t.Error("reactions.add")
	}
}

func TestWorldPoll(t *testing.T) {
	tw := newTestWorld(t)
	ts := tw.w.PostPoll(LunchID)
	if ts == "" {
		t.Fatal("no poll")
	}
	opt := tw.w.scripts.Polls[0].Options[0].Value
	action, _ := json.Marshal([]any{map[string]any{"type": "button", "action_id": PollVoteAction, "block_id": PollOptionBlock + opt, "value": opt}})
	cont, _ := json.Marshal(map[string]any{"type": "message", "message_ts": ts, "channel_id": LunchID, "is_ephemeral": false})
	tw.call("blocks.actions", "service_id", PulseBotID, "app_id", PulseAppID, "actions", string(action), "container", string(cont))
	if v := tw.w.PollVotes(LunchID, ts); !hasStr(v[opt], SelfID) {
		t.Errorf("your vote: %v", v)
	}
	m, _ := tw.w.Message(LunchID, ts)
	if !strings.Contains(string(m.Blocks), "Voted") {
		t.Errorf("poll not rewritten: %s", m.Blocks)
	}
	tw.idle()
	n := 0
	for _, vs := range tw.w.PollVotes(LunchID, ts) {
		n += len(vs)
	}
	if n != len(tw.w.scripts.Polls[0].Votes)+1 {
		t.Errorf("%d votes, want the cast's %d and yours", n, len(tw.w.scripts.Polls[0].Votes))
	}
	close, _ := json.Marshal([]any{map[string]any{"type": "button", "action_id": PollCloseAction, "block_id": PollActionsBlock, "value": "close"}})
	tw.call("blocks.actions", "service_id", PulseBotID, "actions", string(close), "container", string(cont))
	m, _ = tw.w.Message(LunchID, ts)
	if !strings.Contains(string(m.Blocks), "Closed by") {
		t.Errorf("poll not closed: %s", m.Blocks)
	}
	tw.idle()
}

func TestWorldAgent(t *testing.T) {
	tw := newTestWorld(t)
	sock := tw.socket()
	ts := tw.w.Post(SelfID, CodeID, "", "what's the status of the PR?")
	tw.idle()
	th := tw.w.Thread(CodeID, ts)
	var reply *slack.Message
	for i := range th {
		if th[i].User == InkwellID {
			reply = &th[i]
		}
	}
	if reply == nil {
		t.Fatalf("Inkwell didn't answer: %+v", th)
	}
	if reply.StreamingState != "completed" || reply.BotID != InkwellBotID || !strings.Contains(string(reply.Blocks), "Thinking completed") {
		t.Errorf("final reply: %+v %s", reply, reply.Blocks)
	}
	types := sock.types()
	if types["ai_assistant_status"] < 2 || types["message/message_changed"] < 2 || types["ai_assistant_threads_suggested_prompts"] != 1 {
		t.Errorf("stream events: %v", types)
	}
	// a PR update moves CI and the agent session along
	tw.w.AgentProgress()
	tw.idle()
	if ci := tw.w.codeProp("ci_state"); ci == "" {
		t.Error("no ci_state")
	}
	if sock.types()["channel_updated"] == 0 {
		t.Error("no channel_updated")
	}
	diff := tw.call("client.codeChannels.getDiff", "channel_id", CodeID)["diff"].(map[string]any)
	if fs := diff["files"].([]any); len(fs) != 3 {
		t.Errorf("diff files: %v", fs)
	}
	if pr := tw.call("client.codeChannels.getPullRequest", "pr_url", DemoPRURL, "channel_id", CodeID); pr["pull_request"] == nil {
		t.Errorf("PR: %v", pr)
	}
}

func TestWorldHuddle(t *testing.T) {
	tw := newTestWorld(t)
	sock := tw.socket()
	tw.w.RingHuddle("gordon")
	tw.idle() // 30 s at Speed 400: missed
	if h := tw.w.History("D0DEMOGORD"); h[0].User != "U0DEMOGORD" || tsBefore(h[0].TS, tw.w.startTS) {
		t.Errorf("no message after the missed ring: %+v", h[0])
	}
	if ty := sock.types(); ty["huddle_invite"] != 1 || ty["huddle_invite_cancel"] != 1 {
		t.Errorf("ring events: %v", ty)
	}
	n := len(tw.w.History("D0DEMOPRIY"))
	tw.w.RingHuddle("priya")
	inv := sock.wait(func(e map[string]any) bool { return e["type"] == "huddle_invite" && e["channel_id"] == "D0DEMOPRIY" })
	if inv == nil || inv["sender_user_id"] != "U0DEMOPRIY" || inv["call_id"] == "" {
		t.Fatalf("invite: %v", inv)
	}
	tw.call("rooms.inviteResponse", "channel_id", "D0DEMOPRIY", "room_id", inv["call_id"].(string), "response", "decline")
	tw.idle()
	if h := tw.w.History("D0DEMOPRIY"); len(h) != n+1 || h[0].User != "U0DEMOPRIY" {
		t.Errorf("no reply to the decline: %+v", h)
	}
	var out map[string]any
	err := tw.c.Call(context.Background(), "rooms.join", url.Values{"channel_id": {"D0DEMOGORD"}}, &out)
	if err == nil || !strings.Contains(err.Error(), "huddles_unavailable_in_demo") {
		t.Errorf("rooms.join: %v", err)
	}
	tw.idle()
}

func TestWorldScenes(t *testing.T) {
	tw := newTestWorld(t)
	if len(tw.w.scripts.Scenes) == 0 || len(tw.w.scripts.Replies.Lines) == 0 {
		t.Fatal("no scripts loaded")
	}
	posted := 0
	for _, sc := range tw.w.scripts.Scenes[:min(3, len(tw.w.scripts.Scenes))] {
		tw.w.PlayScene(sc.ID)
	}
	tw.idle()
	for _, c := range tw.ws.Convs {
		for _, m := range tw.w.History(c.ID) {
			if tsBefore(tw.w.startTS, m.TS) {
				posted++
				if strings.Contains(m.Text, "{") && strings.Contains(m.Text, "}") {
					t.Errorf("unexpanded placeholder in %s: %q", c.ID, m.Text)
				}
			}
		}
	}
	if posted == 0 {
		t.Error("scenes posted nothing")
	}
	tw.w.SetStatus("U0DEMODALE", Status{Emoji: ":honey_pot:", Text: "Honey o'clock"})
	if u := tw.call("users.info", "user", "U0DEMODALE")["user"].(map[string]any); u["profile"].(map[string]any)["status_text"] != "Honey o'clock" {
		t.Errorf("status: %v", u)
	}
}

func TestWorldAmbient(t *testing.T) {
	fs, err := fakeslack.Start(fakeslack.Size{Users: 3})
	if err != nil {
		t.Fatal(err)
	}
	defer fs.Close()
	now := time.Now()
	ws := testWorkspace(fs.URL, now)
	// Speed 3000: the first scene within ~7 ms, a huddle ring within
	// ~80 ms, polls and Inkwell's updates within ~50 ms
	w := newWorld(fs, ws, Options{Speed: 3000, Now: now, Seed: 3})
	w.Start(context.Background())
	deadline := time.Now().Add(3 * time.Second)
	live := func() (msgs int, polls int) {
		w.mu.Lock()
		defer w.mu.Unlock()
		for _, h := range w.hist {
			for _, m := range h {
				if tsBefore(w.startTS, m.TS) {
					msgs++
				}
			}
		}
		return msgs, len(w.polls)
	}
	for time.Now().Before(deadline) {
		if m, p := live(); m > 10 && p > 0 {
			break
		}
		time.Sleep(20 * time.Millisecond)
	}
	t0 := time.Now()
	w.Stop()
	w.Stop() // twice is fine
	if d := time.Since(t0); d > time.Second {
		t.Errorf("Stop took %v", d)
	}
	if m, p := live(); m <= 10 || p == 0 {
		t.Errorf("ambient: %d live messages, %d polls", m, p)
	}
}

// TestWorldBuilt runs the world on the real content (Build): every
// conversation serves, the cast answers, nothing is Fake Club's.
func TestWorldBuilt(t *testing.T) {
	fs, err := fakeslack.Start(fakeslack.Size{Users: 3})
	if err != nil {
		t.Fatal(err)
	}
	defer fs.Close()
	now := time.Now()
	ws, err := Build(fs.URL, now)
	if err != nil || ws == nil {
		t.Skipf("Build: %v", err)
	}
	w := newWorld(fs, ws, Options{Speed: 400, Quiet: true, Now: now, Seed: 11, Logf: t.Logf})
	w.Start(context.Background())
	defer w.Stop()
	tw := &testWorld{t: t, fs: fs, ws: ws, w: w, c: slack.New(fs.Creds())}
	if a := tw.call("auth.test"); a["team"] != TeamName {
		t.Errorf("auth.test: %v", a)
	}
	for _, c := range ws.Convs {
		tw.call("conversations.info", "channel", c.ID)
		tw.call("conversations.history", "channel", c.ID, "limit", "50")
	}
	var counts slack.Counts
	if err := tw.c.Call(context.Background(), "client.counts", nil, &counts); err != nil {
		t.Fatal(err)
	}
	if n := len(counts.Channels) + len(counts.IMs) + len(counts.MPIMs); n != len(ws.Convs) {
		t.Errorf("client.counts: %d entries for %d conversations", n, len(ws.Convs))
	}
	tw.call("users.channelSections.list")
	tw.call("activity.feed", "types", activityAll)
	if len(w.scripts.Scenes) == 0 {
		t.Fatal("no scenes")
	}
	// every scene's speakers and channels resolve
	for _, sc := range w.scripts.Scenes {
		for _, st := range sc.Steps {
			if _, ok := w.person(st.Who); !ok && st.Who != "inkwell" {
				t.Errorf("scene %s: unknown speaker %q", sc.ID, st.Who)
			}
			if k := orStr(st.Channel, sc.Channel); k == "dm" {
				continue
			} else if id := w.bots.channelFor(k, ""); id == "" {
				t.Errorf("scene %s: unknown channel %q", sc.ID, k)
			} else {
				if _, ok := ws.Conv(id); !ok {
					t.Errorf("scene %s: channel %s isn't in the workspace", sc.ID, id)
				}
			}
		}
	}
	for h := range w.scripts.Replies.Lines {
		if _, ok := w.person(h); !ok {
			t.Errorf("replies: unknown person %q", h)
		}
	}
	ts := w.Post(SelfID, GeneralID, "", "hey everyone, who has the good stapler?")
	tw.idle()
	if th := w.Thread(GeneralID, ts); len(th) == 0 {
		t.Error("nobody answered in #general")
	}
	for _, id := range []string{"D0DEMOGORD", "D0DEMOINKW"} {
		w.Post(SelfID, id, "", "what's the status?")
	}
	tw.idle()
	if h := w.History("D0DEMOGORD"); h[0].User != "U0DEMOGORD" {
		t.Errorf("Gordon's DM: %+v", h[0])
	}
	if ts := w.PostPoll(LunchID); ts == "" {
		t.Error("no poll")
	}
	w.PostStandup(GeneralID)
	w.AgentProgress()
	w.RingHuddle("priya")
	tw.idle()
	for _, sc := range w.scripts.Scenes[:min(4, len(w.scripts.Scenes))] {
		w.PlayScene(sc.ID)
	}
	tw.idle()
}

const activityAll = "at_user,at_user_group,at_channel,at_everyone,keyword,thread_v2,message_reaction,bot_dm_bundle,dm,internal_channel_invite,channel"
