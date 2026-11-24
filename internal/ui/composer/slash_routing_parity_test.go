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
// each case of testdata/parity/slash-commands.json, where the composer's
// send takes a typed command, against what Slack's own send path does with
// it (tools/oracles/slash_commands.cjs runs handleSlashCommand and
// executeCommand over Slack's CDN build; `tools/parity.py --refresh
// slash-commands`).
//
// A case is Slack's delta as its composer holds it (mentions as text with
// a slackmention attribute), the conversation (conversations.info), the
// thread it's typed in, and over the base: the people, commands.list's
// commands, experiment groups and team prefs. The base is the composer
// tests' world (syntheticApp: Hack Club's team in its grid, the viewer
// USELF); Slack's answers are the same with other ids. Slack also reads
// permissions.info's answers for the conversation (base.permissions); Clack
// asks none, so they're accepted here and not fed anywhere.
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
//     name, which Clack's side reads from what ran (srLocalRan: the host
//     hook or the API call each client command makes), not from the
//     typed name;
//   - remind: the reminder dialog; feedback: Slackbot's ephemeral text.

// srIn is a case's inputs and the snapshot's base (the same fields).
type srIn struct {
	Self        json.RawMessage            `json:"self,omitempty"`
	Team        *srTeam                    `json:"team,omitempty"`
	Users       []json.RawMessage          `json:"users,omitempty"`
	Experiments map[string]string          `json:"experiments,omitempty"`
	TeamPrefs   map[string]json.RawMessage `json:"team_prefs,omitempty"`
	Permissions *struct {
		Team    map[string]bool `json:"team,omitempty"`
		Channel map[string]bool `json:"channel,omitempty"`
	} `json:"permissions,omitempty"`
	Commands map[string]json.RawMessage `json:"commands,omitempty"`
	Conv     json.RawMessage            `json:"conv,omitempty"`
	Ops      json.RawMessage            `json:"ops,omitempty"`
	ThreadTS string                     `json:"thread_ts,omitempty"`
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
	// /msg and /dm open: conversations.open with return_im answers the
	// whole IM, as Slack does
	fs.reply["conversations.history"] = map[string]any{"ok": true, "has_more": false,
		"messages": []any{map[string]any{"type": "message", "ts": "1700000000.000050", "user": tSelf, "text": srOwnText, "blocks": elBlocks(elText(srOwnText))}}}
	fs.reply["conversations.open"] = map[string]any{"ok": true, "channel": map[string]any{"id": srDM, "is_im": true, "user": srDMUser,
		"is_open": true, "created": 1600000000, "is_org_shared": false, "last_read": "0000000000.000000", "priority": 0}}
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
		// a client command that ran (and may have answered with feedback
		// of its own) before Slackbot's feedback on a command that didn't
		if ran := srLocalRan(h, fs, conv.ID); ran != "" {
			return map[string]any{"route": "local", "name": ran}
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

// srLocalRan is the client command that ran, by its canonical name, read
// from what each one does (the composer's commandUI hooks and core's
// client commands, slash_local.go and writes.go's Command): "" when none
// did; several are joined by "+", and anything else a command did is
// "unattributed", so neither can pass for one command.
func srLocalRan(h *cmdHost, fs *fakeSlack, ch string) string {
	h.mu.Lock()
	toggles, searches, invites := h.toggles, h.searches, len(h.invites)
	navs := append([]string(nil), h.navs...)
	overlays := append([]popups.Overlay(nil), h.overlays...)
	h.mu.Unlock()
	ran := map[string]bool{}
	if toggles > 0 {
		ran["/shortcuts"] = true // ToggleKeyboardShortcuts
	}
	if searches > 0 {
		ran["/search"] = true // OpenSearchFromCommand: the popover
	}
	if invites > 0 || len(fs.called("conversations.invite")) > 0 {
		ran["/invite"] = true // inviteCommand
	}
	opened := len(fs.called("conversations.open")) > 0
	for _, r := range navs {
		switch {
		case strings.HasPrefix(r, "search"):
			ran["/search"] = true // SubmitSearch: the results page
		case r == "directories?tab=channels":
			ran["/open"] = true // openCommand's channel browser
		case r == "dms":
			ran["/msg"] = true // msgCommand's DMs
		case r == "c/"+srDM && opened:
			ran["/msg"] = true // the DM msgCommand opened
		case strings.HasPrefix(r, "c/") && !opened:
			// the channel openCommand showed (a /msg to a channel would read
			// so too; no case has one)
			ran["/open"] = true
		default:
			ran["unattributed"] = true
		}
	}
	for _, p := range fs.called("chat.postMessage") {
		if p.Get("channel") != ch {
			ran["/msg"] = true // msgSend: what followed the name, in the DM
		}
	}
	for _, o := range overlays {
		if inv, ok := o.(interface{ Invisible() bool }); ok && inv.Invisible() {
			continue // popups' async pump, which draws nothing
		}
		switch o.(type) {
		case *popups.UserFeedbackModal:
			ran["/feedback"] = true // feedbackCommand's dialog
		case *popups.CreateChannelModal:
			ran["/open"] = true // openCommand, for a channel it can't find
		default:
			ran["unattributed"] = true
		}
	}
	for m, cmd := range map[string]string{"conversations.setTopic": "/topic", "conversations.rename": "/rename", "conversations.archive": "/archive",
		"conversations.leave": "/leave", "conversations.join": "/open"} {
		if len(fs.called(m)) > 0 {
			ran[cmd] = true
		}
	}
	names := slices.Sorted(maps.Keys(ran))
	return strings.Join(names, "+")
}
