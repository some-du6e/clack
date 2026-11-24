package composer

import (
	"bytes"
	"context"
	"encoding/json"
	"maps"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/some-du6e/clack/internal/paritytest"
	"github.com/some-du6e/clack/internal/slack"
	"github.com/some-du6e/clack/internal/store"
	"github.com/some-du6e/clack/internal/ui/popups"
	"github.com/some-du6e/clack/internal/ui/theme"
)

// The slash-commands parity dimension's routing half (docs/PARITY.md): for
// each case of testdata/parity/slash-routing.json, where the composer's
// send takes a typed command, against what Slack's own send path does with
// it (tools/oracles/slash_commands.cjs runs handleSlashCommand and
// executeCommand over Slack's CDN build; `tools/parity.py --refresh
// slash-commands`).
//
// A case is the delta as handleSlashCommand gets it: prepareAndSendMessage's
// processed delta, mentions as text with a slackmention attribute (a typed
// #channel or @name that names one is a mention by then, as Clack's own
// processing makes it), the conversation (conversations.info), the thread
// it's typed in, and over the base: the people, commands.list's commands,
// experiment groups, team prefs and permissions.info's answers for the
// conversation and the team (the case's over the base's, one by one). The
// base is the composer tests' world (syntheticApp: Hack Club's team in its
// grid, the viewer USELF); Slack's answers are the same with other ids.
// Clack's fake answers edge permissions/info with the permissions allowed
// (and the @channel ones the composer tests' fake allows).
//
// The answer, as the oracle gives it: {"route": ...} with
//   - message: the composer sent it as a message (chat.postMessage);
//   - edit: the /s/old/new/ edit of the last message (chat.update), its
//     fields from the parse the send used;
//   - server: chat.command, its command, disp, channel, thread_ts, team_id,
//     app_id, and blocks as "string" (their JSON) when sent;
//   - local: a client command ran (no chat.command): it called the API, or
//     opened a dialog or a view; by the canonical name the send looked it
//     up by;
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
	Commands    map[string]json.RawMessage `json:"commands,omitempty"`
	Conv        json.RawMessage            `json:"conv,omitempty"`
	Ops         json.RawMessage            `json:"ops,omitempty"`
	ThreadTS    string                     `json:"thread_ts,omitempty"`
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
	r.Commands = srMerged(b.Commands, c.Commands)
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
	s := paritytest.Load[srIn, map[string]any](t, "testdata/parity/slash-routing.json")
	var base srIn
	dec := json.NewDecoder(bytes.NewReader(s.Base))
	dec.DisallowUnknownFields()
	if err := dec.Decode(&base); err != nil {
		t.Fatalf("the snapshot's base: %v", err)
	}
	s.Run(t, paritytest.Check[srIn, map[string]any]{Clack: func(t *testing.T, in srIn) map[string]any {
		return clackSlashRoute(t, base.over(in))
	}})
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
	app, fs := syntheticApp(t, func(s *store.Store) {
		s.PutUsers(users)
		s.PutConv(conv)
		s.SetLatest(conv.ID, []slack.Message{elMsg("1700000000.000050", tSelf, srOwnText, elBlocks(elText(srOwnText)))}, false)
	})
	cmds := map[string]any{}
	for k, raw := range c.Commands {
		var v any
		srDecode(t, "commands", raw, &v)
		cmds[k] = v
	}
	fs.mu.Lock()
	if fs.reply == nil {
		fs.reply = map[string]any{}
	}
	fs.reply["commands.list"] = map[string]any{"ok": true, "commands": cmds}
	// the newest messages an /s/old/new/ edit looks through, and the DM
	// /msg and /dm open
	fs.reply["conversations.history"] = map[string]any{"ok": true, "has_more": false,
		"messages": []any{map[string]any{"type": "message", "ts": "1700000000.000050", "user": tSelf, "text": srOwnText, "blocks": elBlocks(elText(srOwnText))}}}
	fs.reply["conversations.open"] = map[string]any{"ok": true, "channel": map[string]any{"id": "D000ALICE"}}
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
	for k, v := range c.Experiments {
		app.SetExperimentGroup(k, v)
	}
	app.SetTeamPrefs(c.TeamPrefs)
	if _, err := app.Commands(context.Background(), ""); err != nil {
		t.Fatalf("commands.list: %v", err)
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
	comp.ed.SetDoc(doc)
	comp.submit()

	feedback := ""
	took := func() map[string]any {
		if calls := fs.called("chat.update"); len(calls) > 0 {
			e, _ := parseEditLast(q)
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
		if len(fs.called("chat.postMessage")) > 0 {
			return map[string]any{"route": "message"}
		}
		h.mu.Lock()
		overlays, local := append([]popups.Overlay(nil), h.overlays...), h.toggles+h.searches+len(h.navs)+len(h.invites) > 0
		h.mu.Unlock()
		for _, o := range overlays {
			if m, ok := o.(*popups.Modal); ok && m.Title() == "Reminder" {
				return map[string]any{"route": "remind"}
			}
		}
		// a client command that ran (and may have answered with feedback
		// of its own) before Slackbot's feedback on a command that didn't
		for _, m := range []string{"conversations.setTopic", "conversations.rename", "conversations.archive", "conversations.leave", "conversations.close", "conversations.join", "conversations.invite", "conversations.open"} {
			local = local || len(fs.called(m)) > 0
		}
		if local || len(overlays) > 0 {
			return map[string]any{"route": "local", "name": app.LocalCommand(parseSlashArgs(linkifyQ(q)).Cmd)}
		}
		if feedback != "" {
			return map[string]any{"route": "feedback", "text": feedback}
		}
		return nil
	}
	for end := time.Now().Add(5 * time.Second); time.Now().Before(end); time.Sleep(10 * time.Millisecond) {
		runRestores()
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
		if o := took(); o != nil {
			return o
		}
	}
	t.Logf("the send went nowhere: %s", strings.TrimSpace(doc.Text()))
	return map[string]any{"route": "none"}
}
