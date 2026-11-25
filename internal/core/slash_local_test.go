package core

import (
	"context"
	"errors"
	"testing"

	"github.com/some-du6e/clack/internal/slack"
)

// Slack's client commands (Zrd3 getCommands) by name and alias, the
// command store's own names first.
func TestClientCommandNames(t *testing.T) {
	for typed, want := range map[string]string{
		"/keys": "/shortcuts", "/shortcuts": "/shortcuts", "/s": "/search", "/search": "/search", "/join": "/open",
		"/open": "/open", "/dm": "/msg", "/msg": "/msg", "/colour": "/color", "/part": "/leave", "/close": "/leave",
		"/kick": "/remove", "/feedback": "/feedback", "／keys": "/shortcuts",
	} {
		if got, ok := ClientCommand(typed); !ok || got != want {
			t.Errorf("ClientCommand(%q) = %q %v, want %q", typed, got, ok, want)
		}
	}
	for _, typed := range []string{"/away", "/active", "/mute", "/star", "/shrug", "/me", "/nope", ""} {
		if got, ok := ClientCommand(typed); ok {
			t.Errorf("ClientCommand(%q) = %q, want none (the server's)", typed, got)
		}
	}
	a, _ := testApp(t)
	a.mu.Lock()
	a.cmdCache = []slack.Command{{Name: "/giphy", Aliases: []string{"/gf", "/gif"}}, {Name: "/s3", CanonicalName: "/s3deploy"}}
	a.mu.Unlock()
	// the client commands come first (yrZ7 Qe): /gif is the client's even
	// where a server command answers to it
	for typed, want := range map[string]string{"/gf": "/giphy", "/gif": "/gif", "/s3": "/s3deploy", "/keys": "/shortcuts", "/away": "/away"} {
		if got := a.LocalCommand(typed); got != want {
			t.Errorf("LocalCommand(%q) = %q, want %q", typed, got, want)
		}
	}
}

// The /feedback dialog's Submit: feedback.submitUserFeedback with the
// fetcher's snake_case arguments; RateLimited reads 429 and ratelimited.
func TestSubmitUserFeedback(t *testing.T) {
	a, f := testApp(t)
	f.reply["feedback.submitUserFeedback"] = `{"ok":true}`
	if err := a.SubmitUserFeedback(context.Background(), "huddles", "The mute key is hard to find."); err != nil {
		t.Fatal(err)
	}
	p := f.last("feedback.submitUserFeedback")
	if p.Get("product_area") != "huddles" || p.Get("product_feedback") != "The mute key is hard to find." || p.Get("source") != "slash_feedback" {
		t.Errorf("feedback.submitUserFeedback %v", p)
	}
	for _, c := range []struct {
		err  error
		want bool
	}{
		{slack.RateLimited, true},
		{&slack.HTTPError{Status: 429}, true},
		{&slack.Error{Code: "ratelimited"}, true},
		{&slack.Error{Code: "invalid_arguments"}, false},
		{&slack.HTTPError{Status: 500}, false},
		{errors.New("other"), false},
	} {
		if got := RateLimited(c.err); got != c.want {
			t.Errorf("RateLimited(%v) = %v", c.err, got)
		}
	}
}

// slashLocalApp is testApp with the channels and people /open and /msg
// are tried on (member-shaped ids: getMemberById takes only those).
func slashLocalApp(t *testing.T) (*App, *fakeSlack) {
	t.Helper()
	a, f := testApp(t)
	a.S.PutUsers([]slack.User{{ID: "U0BOBBY0001", Name: "bobby"}, {ID: "U0GONE00001", Name: "gone", Deleted: true},
		// one per API error case: the client answers an identical retry of
		// a failed call with that failure (slack-client backoff)
		{ID: "U0CODE00001", Name: "code1"}, {ID: "U0CODE00002", Name: "code2"}, {ID: "U0CODE00003", Name: "code3"}})
	for _, c := range []slack.Conversation{
		{ID: "C0GENERAL1", Name: "general", IsChannel: true, IsMember: true},
		{ID: "C0RANDOM01", Name: "random", IsChannel: true},
		{ID: "C0RANDOM02", Name: "random-two", IsChannel: true},   // joins that fail (C0RANDOM01 gets joined), one
		{ID: "C0RANDOM03", Name: "random-three", IsChannel: true}, // request each: a failed call's retry gets its failure
		{ID: "C0OLDNEWS1", Name: "old-news", IsChannel: true, IsMember: true, IsArchived: true},
		{ID: "G0OLDSECR1", Name: "old-secrets", IsChannel: true, IsPrivate: true, IsMember: true, IsArchived: true},
		{ID: "C0OTHERWS1", Name: "elsewhere", IsChannel: true, ContextTeamID: "T0OTHERWS1"},
	} {
		a.S.PutConv(c)
	}
	f.reply["conversations.open"] = `{"ok":true,"channel":{"id":"D0BOBBY001","is_im":true,"user":"U0BOBBY0001"}}`
	f.reply["conversations.join"] = `{"ok":true,"channel":{"id":"C0RANDOM01","name":"random","is_channel":true,"is_member":true}}`
	f.reply["conversations.info"] = `{"ok":true,"channel":{"id":"C0RANDOM01","name":"random","is_channel":true,"is_member":true}}`
	f.reply["users.info"] = `{"ok":true,"user":{"id":"U0BOBBY0001","name":"bobby"}}`
	f.reply["info"] = `{"ok":true,"allowed":["CREATE_PUBLIC_CHANNEL"],"allowed_per_team":{}}` // edge permissions/info
	return a, f
}

func mention(id string) []CommandParam { return []CommandParam{{Type: "mention", Value: id}} }

// openCommand (8FiW): the browser, a person's DM, a channel joined unless
// the viewer is in it (an archived one shown anyway), the create-channel
// modal or Slack's texts for the rest; UNKNOWN_COMMAND for a guest.
func TestOpenCommand(t *testing.T) {
	ctx := context.Background()
	a, f := slashLocalApp(t)
	if tg, err := a.OpenCommand(ctx, "/open", nil, ""); err != nil || !tg.Browse {
		t.Errorf("no arguments: %+v %v", tg, err)
	}
	if tg, err := a.OpenCommand(ctx, "/open", mention("C0GENERAL1"), "general"); err != nil || tg.Conv != "C0GENERAL1" || countCalls(f, "conversations.join") != 0 {
		t.Errorf("a channel the viewer is in: %+v %v, %d joins", tg, err, countCalls(f, "conversations.join"))
	}
	if tg, err := a.OpenCommand(ctx, "/join", mention("C0RANDOM01"), "random"); err != nil || tg.Conv != "C0RANDOM01" ||
		f.last("conversations.join").Get("channel") != "C0RANDOM01" {
		t.Errorf("a channel to join: %+v %v", tg, err)
	}
	if tg, err := a.OpenCommand(ctx, "/open", mention("U0BOBBY0001"), ""); err != nil || tg.Conv != "D0BOBBY001" ||
		f.last("conversations.open").Get("users") != "U0BOBBY0001" {
		t.Errorf("a person: %+v %v", tg, err)
	}
	for _, params := range [][]CommandParam{{{Type: "string", Value: "plans"}}, mention("C0NOWHERE1")} {
		if tg, err := a.OpenCommand(ctx, "/open", params, "plans"); err != nil || !tg.Create || tg.Name != "plans" {
			t.Errorf("%v: %+v %v, want the create-channel modal", params, tg, err)
		}
	}
	f.reply["info"] = `{"ok":true,"allowed":[],"allowed_per_team":{}}`
	if _, err := a.OpenCommand(ctx, "/open", []CommandParam{{Type: "string", Value: "plans"}}, "plans"); feedbackOf(err) != "I couldn’t find a channel named “plans”, sorry!" {
		t.Errorf("can't create: %v", err)
	}
	if _, err := a.OpenCommand(ctx, "/join", mention("C0OTHERWS1"), "elsewhere"); feedbackOf(err) != "*/join* can’t open or join channels outside this workspace." {
		t.Errorf("another workspace: %v", err)
	}
	f.reply["conversations.join"] = `{"ok":false,"error":"is_archived"}`
	if tg, err := a.OpenCommand(ctx, "/open", mention("C0RANDOM02"), "random-two"); err != nil || tg.Conv != "C0RANDOM02" {
		t.Errorf("archived: %+v %v, want it shown", tg, err)
	}
	f.reply["conversations.join"] = `{"ok":false,"error":"method_not_supported_for_channel_type"}`
	if _, err := a.OpenCommand(ctx, "/open", mention("C0RANDOM03"), "random-three"); feedbackOf(err) != "There was a problem joining “random-three”, sorry!" {
		t.Errorf("join refused: %v", err)
	}
	f.reply["conversations.open"] = `{"ok":false,"error":"user_disabled"}`
	if _, err := a.OpenCommand(ctx, "/open", mention("U0BOBBY0001"), ""); feedbackOf(err) != "A valid member name is required." {
		t.Errorf("no DM: %v", err)
	}
	self, _ := a.S.User("USELF")
	guest := *self
	guest.IsRestricted = true
	a.S.PutUsers([]slack.User{guest})
	var ce *CommandError
	if _, err := a.OpenCommand(ctx, "/open", nil, ""); !errors.As(err, &ce) || ce.Kind != CmdUnknown {
		t.Errorf("a guest: %v, want UNKNOWN_COMMAND", err)
	}
}

// msgCommand (6bdk): the DMs, a person's DM, a channel joined; Slack's
// texts for no mention, a user group, a member Slack doesn't hold, a
// deactivated one with a message, an unknown, another workspace's or an
// archived channel with a message, and the API's errors.
func TestMsgCommand(t *testing.T) {
	ctx := context.Background()
	a, f := slashLocalApp(t)
	if tg, err := a.MsgCommand(ctx, "/msg", nil, false); err != nil || !tg.Browse {
		t.Errorf("no arguments: %+v %v", tg, err)
	}
	if tg, err := a.MsgCommand(ctx, "/msg", mention("U0BOBBY0001"), true); err != nil || tg.Conv != "D0BOBBY001" {
		t.Errorf("a person: %+v %v", tg, err)
	}
	if tg, err := a.MsgCommand(ctx, "/dm", mention("C0RANDOM01"), true); err != nil || tg.Conv != "C0RANDOM01" ||
		f.last("conversations.join").Get("channel") != "C0RANDOM01" {
		t.Errorf("a channel: %+v %v", tg, err)
	}
	if tg, err := a.MsgCommand(ctx, "/msg", mention("U0GONE00001"), false); err != nil || tg.Conv == "" {
		t.Errorf("a deactivated person, no message: %+v %v", tg, err)
	}
	if tg, err := a.MsgCommand(ctx, "/msg", mention("C0OLDNEWS1"), false); err != nil || tg.Conv != "C0OLDNEWS1" {
		t.Errorf("an archived channel, no message: %+v %v", tg, err)
	}
	for _, tc := range []struct {
		cmd    string
		params []CommandParam
		text   bool
		want   string
	}{
		{"/msg", []CommandParam{{Type: "string", Value: "hello"}}, true, "A valid member name is required."},
		{"/dm", mention("S0SNACKS01"), true, "*/dm* can’t send message to user groups."},
		{"/msg", mention("U0NOBODY001"), true, "A valid member name is required."},
		{"/msg", mention("U0GONE00001"), true, "That user has been deactivated."},
		{"/msg", mention("C0NOWHERE1"), true, "A valid channel name is required."},
		{"/msg", mention("C0OTHERWS1"), true, "*/msg* can’t send message to channels outside this workspace."},
		{"/msg", mention("C0OLDNEWS1"), true, "That channel has been archived."},
		{"/msg", mention("G0OLDSECR1"), true, "That private channel has been archived."},
	} {
		if _, err := a.MsgCommand(ctx, tc.cmd, tc.params, tc.text); feedbackOf(err) != tc.want {
			t.Errorf("%s %v: %v, want %q", tc.cmd, tc.params, err, tc.want)
		}
	}
	// a member each (an identical retry of a failed call gets that
	// failure), user_not_found last: the store then forgets its member
	for _, c := range []struct{ code, user, want string }{
		{"channel_not_found", "U0CODE00001", "A valid channel name is required."},
		{"access_denied", "U0CODE00002", "Hmm, something went wrong. Try again?"},
		{"user_not_found", "U0CODE00003", "A valid member name is required."},
	} {
		f.reply["conversations.open"] = `{"ok":false,"error":"` + c.code + `"}`
		if _, err := a.MsgCommand(ctx, "/msg", mention(c.user), true); feedbackOf(err) != c.want {
			t.Errorf("conversations.open %s: %v, want %q", c.code, err, c.want)
		}
	}
	f.reply["conversations.join"] = `{"ok":false,"error":"channel_not_found"}`
	if _, err := a.MsgCommand(ctx, "/msg", mention("C0RANDOM02"), true); feedbackOf(err) != "A valid channel name is required." {
		t.Errorf("join channel_not_found: %v", err)
	}
}

// A channel the viewer isn't in, picked from the composer's # list: only
// the list's answer (edge channels/search) carries it. Slack's
// getRemoteChannels upserts the hits into its channels store, so /join,
// /open and /msg find it: /join joins it and shows it, /msg joins it
// too, and another workspace's hit is turned down as such (not offered
// the create-channel modal under an existing channel's name).
// A channel a list or a search answered is a hit (store.ChannelHit), not
// a conversation Conv answers: ResolveConv still reads it with
// conversations.info, which has what a hit lacks (the purpose as set, the
// Slack Connect teams), while /open, /join and /msg find it. A hit keeps
// its workspace: a later hit that names none (the directory's) doesn't
// drop it, and one that names it adds it, so /open refuses another
// workspace's channel whichever list saw it first.
func TestListedChannelHits(t *testing.T) {
	ctx := context.Background()
	a, f := slashLocalApp(t)
	f.reply["search.modules.channels"] = `{"ok":true,"items":[` +
		`{"id":"C0LISTED01","name":"listed","is_member":false,"member_count":12,"purpose":{"value":"From the directory"}},` +
		`{"id":"C0LISTEDX1","name":"listed-there","is_member":false}],` +
		`"pagination":{"total_count":2,"page":1,"page_count":1}}`
	if pg, err := a.BrowseChannelsWith(ctx, ChannelBrowseOpts{Query: "listed"}); err != nil || len(pg.Channels) != 2 {
		t.Fatalf("Browse channels: %+v %v", pg, err)
	}
	if _, ok := a.Conv("C0LISTED01"); ok {
		t.Fatal("a directory hit is a conversation Conv answers: opening it reads no conversations.info")
	}
	if h, ok := a.S.ChannelHit("C0LISTED01"); !ok || h.Purpose == nil || h.Purpose.Value != "From the directory" {
		t.Errorf("the hit %+v %v: no purpose", h, ok)
	}
	f.reply["conversations.info"] = `{"ok":true,"channel":{"id":"C0LISTED01","name":"listed","is_channel":true,"is_ext_shared":true,` +
		`"connected_team_ids":["T1","T0PARTNER1"],"purpose":{"value":"Shared with a partner"}}}`
	infos := countCalls(f, "conversations.info")
	cv, ok := a.ResolveConv(ctx, "C0LISTED01")
	if !ok || countCalls(f, "conversations.info") != infos+1 || cv.Purpose != "Shared with a partner" || !cv.IsShared {
		t.Errorf("ResolveConv of a listed channel: %+v %v, %d reads", cv, ok, countCalls(f, "conversations.info")-infos)
	}

	// the # list names C0LISTEDX1's workspace after the directory didn't
	f.reply["search"] = `{"ok":true,"results":[{"id":"C0LISTEDX1","name":"listed-there","is_channel":true,"is_member":false,"context_team_id":"T0OTHERWS1"}]}`
	if _, err := a.AutocompleteChannels(ctx, "listed"); err != nil {
		t.Fatal(err)
	}
	joins := countCalls(f, "conversations.join")
	if _, err := a.OpenCommand(ctx, "/open", mention("C0LISTEDX1"), "listed-there"); feedbackOf(err) != "*/open* can’t open or join channels outside this workspace." ||
		countCalls(f, "conversations.join") != joins {
		t.Errorf("another workspace's listed channel: %v", err)
	}
	// a later directory answer without it keeps it
	if _, err := a.BrowseChannelsWith(ctx, ChannelBrowseOpts{Query: "listed"}); err != nil {
		t.Fatal(err)
	}
	if !a.FromAnotherTeam("C0LISTEDX1") {
		t.Error("a later hit without a workspace dropped the channel's")
	}
}

func TestPickedChannelOpens(t *testing.T) {
	ctx := context.Background()
	a, f := slashLocalApp(t)
	f.reply["search"] = `{"ok":true,"results":[` +
		`{"id":"C0PICKED01","name":"plans","is_channel":true,"is_member":false,"num_members":12},` +
		`{"id":"C0PICKED02","name":"plans-later","is_channel":true,"is_member":false},` +
		`{"id":"C0PICKEDX1","name":"plans-there","is_channel":true,"is_member":false,"context_team_id":"T0OTHERWS1"}]}`
	if _, ok := a.S.Conv("C0PICKED01"); ok {
		t.Fatal("the hit is in the sidebar set before the search")
	}
	hits, err := a.AutocompleteChannels(ctx, "plans")
	if err != nil || len(hits) != 3 {
		t.Fatalf("hits %+v %v", hits, err)
	}
	if _, ok := a.S.Conv("C0PICKED01"); ok {
		t.Error("the search put a channel the viewer isn't in into the sidebar set")
	}
	f.reply["conversations.join"] = `{"ok":true,"channel":{"id":"C0PICKED01","name":"plans","is_channel":true,"is_member":true}}`
	if tg, err := a.OpenCommand(ctx, "/join", mention("C0PICKED01"), "plans"); err != nil || tg.Conv != "C0PICKED01" || tg.Create ||
		f.last("conversations.join").Get("channel") != "C0PICKED01" {
		t.Errorf("/join a picked channel: %+v %v, joins %d", tg, err, countCalls(f, "conversations.join"))
	}
	f.reply["conversations.join"] = `{"ok":true,"channel":{"id":"C0PICKED02","name":"plans-later","is_channel":true,"is_member":true}}`
	if tg, err := a.MsgCommand(ctx, "/msg", mention("C0PICKED02"), true); err != nil || tg.Conv != "C0PICKED02" ||
		f.last("conversations.join").Get("channel") != "C0PICKED02" {
		t.Errorf("/msg a picked channel: %+v %v", tg, err)
	}
	joins := countCalls(f, "conversations.join")
	if _, err := a.OpenCommand(ctx, "/join", mention("C0PICKEDX1"), "plans-there"); feedbackOf(err) != "*/join* can’t open or join channels outside this workspace." ||
		countCalls(f, "conversations.join") != joins {
		t.Errorf("another workspace's hit: %v", err)
	}
}
