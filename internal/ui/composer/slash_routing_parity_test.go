package composer

import (
	"bytes"
	"context"
	"encoding/json"
	"maps"
	"slices"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/some-du6e/clack/internal/paritytest"
	"github.com/some-du6e/clack/internal/slack"
	"github.com/some-du6e/clack/internal/store"
	"github.com/some-du6e/clack/internal/ui/popups"
	"github.com/some-du6e/clack/internal/ui/theme"
)

// The slash-commands parity dimension's routing half (docs/PARITY.md): for
// each case of testdata/parity/slash-commands.json, where the composer's
// send takes a typed command, against what Slack's own send path does with
// it (tools/oracles/slash_commands.cjs runs handleSlashCommand and
// executeCommand over Slack's CDN build; `tools/parity.py --refresh
// slash-commands`).
//
// A case is the delta as handleSlashCommand gets it: prepareAndSendMessage's
// processed delta, mentions as text with a slackmention attribute (a typed
// #channel or @name that names one is a mention by then, as Clack's own
// processing makes it), the conversation (conversations.info), the thread
// it's typed in, and over the base: the viewer and the team, the people,
// the server's commands (client.appCommands' list, by name in their
// order), experiment groups, team prefs, permissions.info's answers for
// the conversation and the team, the channel membership Slack's store
// holds (membership) and users/counts' counts (the case's over the
// base's, one by one). The base is the composer tests' world
// (syntheticApp: Hack Club's team in its grid, the viewer USELF); a case
// may give another viewer, not another team. Experiment groups are the
// test account's under the snapshot's own on both sides: Clack's are
// core's defaults (TestDefaultExperimentsMatchAccount), the oracle's
// internal/core/testdata/account_experiments.json. Clack's fake answers
// edge permissions/info with the permissions allowed (and the @channel
// ones the composer tests' fake allows), channels/membership with the
// membership, and the case's state is in before the send, as the
// channel's views and the member autocomplete would have asked for it.
//
// The answer, as the oracle gives it: {"route": ...} with
//   - message: the composer sent it as a message (chat.postMessage) in the
//     case's conversation;
//   - edit: the /s/old/new/ edit of the last message (chat.update), its
//     fields from the parse the send used (editLastFromDoc); what the
//     chat.update carried isn't compared;
//   - server: chat.command, its command, disp, channel, thread_ts, team_id,
//     app_id, and blocks as "string" when sent (that blocks went, not
//     their content);
//   - local: a client command ran (no chat.command), by its canonical
//     name: on Clack's side the one the composer dispatched
//     (clientCommandRan, as the oracle marks Zrd3's action), or for the
//     ones core runs (/topic, /invite) what ran (the API call); with
//     dialog, the first dialog it opened (Slack's getModalName for it,
//     popups' ModalName), and api, the first write it sent (srWrites).
//     The commands the oracle doesn't follow into their own code are
//     unfollowed: only that they ran locally, and which, is compared;
//   - remind: the reminder dialog; feedback: Slackbot's ephemeral text (a
//     refusal, or a client command whose only answer that was).

// srIn is a case's inputs and the snapshot's base (the same fields).
type srIn struct {
	Self        json.RawMessage            `json:"self,omitempty"`
	Team        *srTeam                    `json:"team,omitempty"`
	Users       []json.RawMessage          `json:"users,omitempty"`
	Experiments map[string]string          `json:"experiments,omitempty"`
	TeamPrefs   map[string]json.RawMessage `json:"team_prefs,omitempty"`
	Permissions *srPerms                   `json:"permissions,omitempty"`
	Commands    srCommands                 `json:"commands,omitempty"`
	Membership  map[string]bool            `json:"membership,omitempty"`
	Counts      json.RawMessage            `json:"counts,omitempty"`
	Conv        json.RawMessage            `json:"conv,omitempty"`
	Ops         json.RawMessage            `json:"ops,omitempty"`
	ThreadTS    string                     `json:"thread_ts,omitempty"`
}

// srCommands is a snapshot's commands: a JSON object of commands by name,
// in its order (Slack's ingest reads the merged object in key order).
type srCommands []srCommand

type srCommand struct {
	Key string
	Raw json.RawMessage
}

func (cs *srCommands) UnmarshalJSON(b []byte) error {
	dec := json.NewDecoder(bytes.NewReader(b))
	if _, err := dec.Token(); err != nil { // {
		return err
	}
	*cs = nil
	for dec.More() {
		k, err := dec.Token()
		if err != nil {
			return err
		}
		var raw json.RawMessage
		if err := dec.Decode(&raw); err != nil {
			return err
		}
		*cs = append(*cs, srCommand{Key: k.(string), Raw: raw})
	}
	_, err := dec.Token() // }
	return err
}

// over is {...b, ...c}: b's keys in their places, c's values over them,
// c's new keys after.
func (b srCommands) over(c srCommands) srCommands {
	out := append(srCommands(nil), b...)
	for _, x := range c {
		if i := slices.IndexFunc(out, func(y srCommand) bool { return y.Key == x.Key }); i >= 0 {
			out[i].Raw = x.Raw
		} else {
			out = append(out, x)
		}
	}
	return out
}

type srPerms struct {
	Team    map[string]bool `json:"team,omitempty"`
	Channel map[string]bool `json:"channel,omitempty"`
}

type srTeam struct {
	ID           string `json:"id"`
	Domain       string `json:"domain"`
	EnterpriseID string `json:"enterprise_id,omitempty"`
}

// over is the case's inputs over the base, as the oracle merges them.
func (b srIn) over(c srIn) srIn {
	r := b
	if c.Self != nil {
		r.Self = c.Self
	}
	if c.Team != nil {
		r.Team = c.Team
	}
	r.Users = append(append([]json.RawMessage(nil), b.Users...), c.Users...)
	r.Experiments = srMerged(b.Experiments, c.Experiments)
	r.TeamPrefs = srMerged(b.TeamPrefs, c.TeamPrefs)
	r.Commands = b.Commands.over(c.Commands)
	r.Membership = srMerged(b.Membership, c.Membership)
	if c.Counts != nil {
		r.Counts = c.Counts
	}
	var bp, cp srPerms
	if b.Permissions != nil {
		bp = *b.Permissions
	}
	if c.Permissions != nil {
		cp = *c.Permissions
	}
	r.Permissions = &srPerms{Team: srMerged(bp.Team, cp.Team), Channel: srMerged(bp.Channel, cp.Channel)}
	r.Conv, r.Ops, r.ThreadTS = c.Conv, c.Ops, c.ThreadTS
	return r
}

func srMerged[V any](a, b map[string]V) map[string]V {
	r := maps.Clone(a)
	if r == nil {
		r = map[string]V{}
	}
	maps.Copy(r, b)
	return r
}

func srDecode(t *testing.T, what string, raw json.RawMessage, v any) {
	t.Helper()
	if err := json.Unmarshal(raw, v); err != nil {
		t.Fatalf("%s: %v", what, err)
	}
}

func TestSlashRoutingLikeSlack(t *testing.T) {
	s := paritytest.Load[srIn, map[string]any](t, "testdata/parity/slash-commands.json")
	var base srIn
	dec := json.NewDecoder(bytes.NewReader(s.Base))
	dec.DisallowUnknownFields()
	if err := dec.Decode(&base); err != nil {
		t.Fatalf("the snapshot's base: %v", err)
	}
	s.Run(t, paritytest.Check[srIn, map[string]any]{Clack: func(t *testing.T, in srIn) map[string]any {
		return clackSlashRoute(t, base.over(in))
	}, Same: srSame})
}

// srSame compares the routes; a client command the oracle doesn't follow
// is compared by its route and name alone.
func srSame(slack, clack map[string]any) bool {
	if slack["unfollowed"] == true {
		return slack["route"] == clack["route"] && slack["name"] == clack["name"]
	}
	a, _ := json.Marshal(slack)
	b, _ := json.Marshal(clack)
	return bytes.Equal(a, b)
}

// srOwnText is the viewer's last message in every case's conversation, with
// its rich_text blocks (an edit needs them), so that an /s/old/new/ edit
// has one to edit.
const srOwnText = "hello a synthetic note"

// clackSlashRoute types the case's delta into a composer over its state,
// sends it, and reads where the send took it.
func clackSlashRoute(t *testing.T, c srIn) map[string]any {
	t.Helper()
	var conv slack.Conversation
	srDecode(t, "conv", c.Conv, &conv)
	var users []slack.User
	for _, raw := range c.Users {
		var u slack.User
		srDecode(t, "users", raw, &u)
		users = append(users, u)
	}
	// the viewer and the team: syntheticApp's creds are the base's, so a
	// case may change who the viewer is but not their ids
	var self slack.User
	srDecode(t, "self", c.Self, &self)
	if self.ID != tSelf || c.Team == nil || c.Team.ID != tTeam || c.Team.EnterpriseID != tEnt {
		t.Fatalf("the viewer %s of team %+v: Clack's side signs in as %s of %s in %s", self.ID, c.Team, tSelf, tTeam, tEnt)
	}
	app, fs := syntheticApp(t, func(s *store.Store) {
		s.PutUsers(append(users, self))
		s.Lock()
		s.TeamID, s.TeamDomain = c.Team.ID, c.Team.Domain
		s.Unlock()
		s.PutConv(conv)
		s.SetLatest(conv.ID, []slack.Message{elMsg("1700000000.000050", tSelf, srOwnText, elBlocks(elText(srOwnText)))}, false)
	})
	app.S.Self = self
	cmds := make([]any, 0, len(c.Commands))
	for _, cmd := range c.Commands {
		var v any
		srDecode(t, "commands", cmd.Raw, &v)
		cmds = append(cmds, v)
	}
	fs.mu.Lock()
	if fs.reply == nil {
		fs.reply = map[string]any{}
	}
	// the membership Slack's store holds, and the count its leave reads
	members, nonMembers := []string{}, []string{}
	for _, u := range slices.Sorted(maps.Keys(c.Membership)) {
		if c.Membership[u] {
			members = append(members, u)
		} else {
			nonMembers = append(nonMembers, u)
		}
	}
	fs.reply["edge/channels/membership"] = map[string]any{"ok": true, "members": members, "non_members": nonMembers}
	if c.Counts != nil {
		var counts any
		srDecode(t, "counts", c.Counts, &counts)
		fs.reply["edge/users/counts"] = map[string]any{"ok": true, "counts": counts}
	} else {
		fs.reply["edge/users/counts"] = map[string]any{"ok": false, "error": "not_answered"}
	}
	// the newest messages an /s/old/new/ edit looks through, and the DM
	// /msg and /dm open: conversations.open with return_im answers the
	// whole IM, as Slack does
	fs.reply["conversations.history"] = map[string]any{"ok": true, "has_more": false,
		"messages": []any{map[string]any{"type": "message", "ts": "1700000000.000050", "user": tSelf, "text": srOwnText, "blocks": elBlocks(elText(srOwnText))}}}
	fs.reply["conversations.open"] = map[string]any{"ok": true, "channel": map[string]any{"id": srDM, "is_im": true, "user": srDMUser,
		"is_open": true, "created": 1600000000, "is_org_shared": false, "last_read": "0000000000.000000", "priority": 0}}
	// permissions.info: what the case allows, for the conversation and the
	// team alike (each ask gets one answer), and the composer fake's
	// @channel and @everyone
	allowed := []string{"CAN_AT_CHANNEL", "CAN_AT_EVERYONE"}
	if p := c.Permissions; p != nil {
		for _, m := range []map[string]bool{p.Channel, p.Team} {
			for _, k := range slices.Sorted(maps.Keys(m)) {
				if m[k] && !slices.Contains(allowed, k) {
					allowed = append(allowed, k)
				}
			}
		}
	}
	fs.reply["edge/permissions/info"] = map[string]any{"ok": true, "allowed": allowed, "allowed_per_team": map[string]any{}}
	fs.mu.Unlock()
	srServeCommands(fs, cmds) // loadClientAppCommands' list, in the merged object's order
	for k, v := range c.Experiments {
		app.SetExperimentGroup(k, v)
	}
	app.SetTeamPrefs(c.TeamPrefs)
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	if _, err := app.Commands(ctx, ""); err != nil {
		t.Fatalf("client.appCommands: %v", err)
	}
	// the channel's and the team's permissions, as the channel's views
	// asked for them when it opened, and the membership the member
	// autocomplete asked for
	srWarm(t, app, conv.ID, slices.Sorted(maps.Keys(c.Membership)))
	var markMu sync.Mutex
	mark := ""
	ranMark := func(name string) {
		markMu.Lock()
		defer markMu.Unlock()
		if mark == "" {
			mark = name
		}
	}
	srMark(&ranMark)
	t.Cleanup(func() { srMark(nil) })
	marked := func() string {
		markMu.Lock()
		defer markMu.Unlock()
		return mark
	}
	stubTyping(t)
	withTaut(t, nil)
	clearDraft(conv.ID, c.ThreadTS)
	h := &cmdHost{slickHost: slickHost{app: app, th: theme.New(false)}}
	comp := New(h, conv.ID, c.ThreadTS)
	t.Cleanup(func() {
		comp.Close()
		clearDraft(conv.ID, c.ThreadTS)
	})
	var ops []any
	srDecode(t, "ops", c.Ops, &ops)
	q := qFromSlackDelta(map[string]any{"ops": ops})
	doc, _ := qToDoc(q)
	events, stop := app.Subscribe()
	defer stop()
	fs.mu.Lock()
	from := len(fs.calls) // the send's calls start here
	fs.mu.Unlock()
	comp.ed.SetDoc(doc)
	comp.submit()

	feedback := ""
	took := func(settled bool) map[string]any {
		if calls := fs.called("chat.update"); len(calls) > 0 {
			e, _ := editLastFromDoc(doc) // the send's parse of the same doc
			return map[string]any{"route": "edit", "prev": e.Prev, "next": e.Next, "replaceAll": e.ReplaceAll, "ignoreCase": e.IgnoreCase}
		}
		if calls := fs.called("chat.command"); len(calls) > 0 {
			p := calls[0]
			o := map[string]any{"route": "server", "method": "chat.command"}
			for _, k := range []string{"command", "disp", "channel", "thread_ts", "team_id", "app_id", "text"} {
				if v := p.Get(k); v != "" {
					o[k] = v
				}
			}
			if p.Has("blocks") {
				o["blocks"] = "string"
			}
			return o
		}
		// a post here is the message; one to another conversation is
		// /msg's send of what follows the name, part of the client command
		for _, p := range fs.called("chat.postMessage") {
			if p.Get("channel") == conv.ID {
				return map[string]any{"route": "message"}
			}
		}
		h.mu.Lock()
		overlays := append([]popups.Overlay(nil), h.overlays...)
		h.mu.Unlock()
		for _, o := range overlays {
			if m, ok := o.(*popups.Modal); ok && m.Title() == "Reminder" {
				return map[string]any{"route": "remind"}
			}
		}
		// once the send and what it started are over: a client command that
		// did something (and may have answered with feedback of its own)
		// before Slackbot's feedback on a command that didn't, and one that
		// did nothing observable after that
		if !settled {
			return nil
		}
		r := srLocalRan(h, fs, conv.ID, from, marked())
		switch {
		case r.effects:
			return r.route()
		case feedback != "":
			return map[string]any{"route": "feedback", "text": feedback}
		case r.name != "":
			return r.route()
		}
		return nil
	}
	for end := time.Now().Add(5 * time.Second); time.Now().Before(end); time.Sleep(10 * time.Millisecond) {
		runRestoresIn(nil, true) // the box's own work too: no composer is laid out here
		popups.Pump()            // the leave flow's decision (composer tests have no pump overlay)
		// before the drain: a job's feedback is out by the time it is done
		settled := comp.jobsDone() && srPopupsIdle() && srQueueEmpty()
	drain:
		for {
			select {
			case ev := <-events:
				if ev.Type == "message" && ev.Msg != nil && ev.Msg.User == "USLACKBOT" && ev.Msg.Ephemeral && feedback == "" {
					feedback = ev.Msg.Text
				}
			default:
				break drain
			}
		}
		if o := took(settled); o != nil {
			// the job's outcome (a failed command's restore) reaches the UI
			// queue before the composer closes, never after cleanup
			for !comp.jobsDone() && time.Now().Before(end) {
				time.Sleep(10 * time.Millisecond)
			}
			if !comp.jobsDone() {
				t.Fatalf("the send job is still running")
			}
			runRestores()
			return o
		}
	}
	t.Logf("the send went nowhere: %s", strings.TrimSpace(doc.Text()))
	return map[string]any{"route": "none"}
}

// The DM /msg and /dm open (the cases mention alice).
const (
	srDM     = "D000ALICE"
	srDMUser = "U000ALICE"
)

// srWrites are the API calls a client command's answer names (the oracle's
// WRITES): the first of these sent is its api.
var srWrites = map[string]bool{"conversations.leave": true, "conversations.close": true, "conversations.setTopic": true,
	"conversations.kick": true, "conversations.rename": true, "conversations.archive": true, "conversations.join": true,
	"conversations.invite": true, "conversations.open": true, "chat.postMessage": true}

// srQueueEmpty reports that nothing waits for the composer's UI queue.
func srQueueEmpty() bool {
	restoreQ.Lock()
	defer restoreQ.Unlock()
	return len(restoreQ.fns) == 0
}

// srLocal is what a client command did: its canonical name (the mark, or
// the effects' for the ones core runs), the first dialog it opened and the
// first write it sent; effects says it did something observable.
type srLocal struct {
	name, dialog, api string
	effects           bool
}

func (r srLocal) route() map[string]any {
	if r.name == "" {
		return nil
	}
	o := map[string]any{"route": "local", "name": r.name}
	if r.dialog != "" {
		o["dialog"] = r.dialog
	}
	if r.api != "" {
		o["api"] = r.api
	}
	return o
}

// srLocalRan reads what the send's client command did from what ran: the
// composer's commandUI hooks (cmdHost), the dialogs it opened (popups'
// ModalName, Slack's getModalName for each), the navigations, and the
// calls from the send's first (from) on. The name is the mark (the
// command localCommand dispatched), else, for core's own client commands,
// the API call's (/topic: conversations.setTopic, /invite:
// conversations.invite); what another client command would have done
// joins the name with "+" (anything no client command does as
// "unattributed"), so it can't pass for one command.
func srLocalRan(h *cmdHost, fs *fakeSlack, ch string, from int, mark string) srLocal {
	h.mu.Lock()
	toggles, searches, invites := h.toggles, h.searches, len(h.invites)
	navs := append([]string(nil), h.navs...)
	overlays := append([]popups.Overlay(nil), h.overlays...)
	h.mu.Unlock()
	fs.mu.Lock()
	var calls []fakeCall
	if from < len(fs.calls) {
		calls = append(calls, fs.calls[from:]...)
	}
	fs.mu.Unlock()
	var r srLocal
	by := map[string]bool{} // the commands the effects point at
	note := func(cmd string) {
		by[cmd] = true
		r.effects = true
	}
	if toggles > 0 {
		note("/shortcuts") // ToggleKeyboardShortcuts
	}
	if searches > 0 {
		note("/search") // OpenSearchFromCommand: the popover
	}
	if invites > 0 {
		note("/invite") // inviteCommand's modal
	}
	opened := false
	for _, c := range calls {
		if c.Method == "conversations.open" {
			opened = true
		}
	}
	for _, rt := range navs {
		switch {
		case strings.HasPrefix(rt, "search"):
			note("/search") // SubmitSearch: the results page
		case rt == "directories?tab=channels":
			note("/open") // openCommand's channel browser
		case rt == "dms":
			note("/msg") // msgCommand's DMs
		case rt == "c/"+srDM && opened:
			note("/msg") // the DM msgCommand opened
		case strings.HasPrefix(rt, "c/") && !opened:
			// the channel openCommand showed (a /msg to a channel would read
			// so too; no case has one)
			note("/open")
		case rt == "threads":
			note("/leave") // leaving the conversation in view
		default:
			note("unattributed")
		}
	}
	dialogs := map[string]string{"RenameChannelModal": "/rename", "ArchiveChannelModal": "/archive", "UserFeedbackModal": "/feedback",
		"Connect(RemoveMemberDialog)": "/remove", "LeaveChannel": "/leave", "Connect(LeaveChannelError)": "/leave",
		"Connect(BaseArchiveDialog)": "/leave"}
	for _, o := range overlays {
		if inv, ok := o.(interface{ Invisible() bool }); ok && inv.Invisible() {
			continue // popups' async pump, which draws nothing
		}
		if _, ok := o.(*popups.CreateChannelModal); ok {
			note("/open") // openCommand, for a channel it can't find
			continue
		}
		n := srModalName(o)
		cmd, known := dialogs[n]
		if !known {
			note("unattributed")
			continue
		}
		note(cmd)
		if r.dialog == "" {
			r.dialog = n
		}
	}
	writer := map[string]string{"conversations.setTopic": "/topic", "conversations.rename": "/rename", "conversations.archive": "/archive",
		"conversations.leave": "/leave", "conversations.close": "/leave", "conversations.join": "/open", "conversations.kick": "/remove",
		"conversations.invite": "/invite", "conversations.open": "/msg"}
	for _, c := range calls {
		switch {
		case c.Method == "chat.postMessage" && c.Form.Get("channel") != ch:
			note("/msg") // msgSend: what followed the name, in the DM
		case writer[c.Method] != "":
			note(writer[c.Method])
		}
		if srWrites[c.Method] && r.api == "" {
			r.api = c.Method
		}
	}
	if mark != "" {
		// the command's own effects, and those of the commands its action
		// goes on to (/remove's own leave is /leave's)
		delete(by, mark)
		if mark == "/remove" {
			delete(by, "/leave")
		}
		r.name = strings.Join(append([]string{mark}, slices.Sorted(maps.Keys(by))...), "+")
		return r
	}
	r.name = strings.Join(slices.Sorted(maps.Keys(by)), "+")
	return r
}
