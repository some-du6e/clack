package messages

import (
	"bytes"
	"encoding/json"
	"image"
	"maps"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/some-du6e/clack/internal/core"
	"github.com/some-du6e/clack/internal/paritytest"
	"github.com/some-du6e/clack/internal/slack"
	"github.com/some-du6e/clack/internal/store"
	"github.com/some-du6e/clack/internal/ui/popups"
	"github.com/some-du6e/clack/internal/ui/theme"
	"github.com/some-du6e/clack/internal/ui/uipref"
)

// The message-actions parity dimension (docs/PARITY.md): for each case
// of testdata/parity/message-actions.json, the hover toolbar, the ⋮ menu
// and the right-click menu Clack gives a message, against what Slack's
// own toolbar and menus give it (tools/oracles/message_actions.cjs runs
// them over Slack's CDN build; `tools/parity.py --refresh
// message-actions`).
//
// A case's inputs are API objects, the same ones on both sides: the
// viewer and the people (users.info), the conversation
// (conversations.info), the message and a reply's root (history),
// where it's shown, whether the viewer follows the thread, experiment
// groups, user prefs, team prefs, feature access and client.extras'
// reporting flag. The snapshot's base holds what every case shares (the
// viewer in a Grid workspace with Lists, Karim's experiment groups).
// Slack also reads permissions.info's answers (base.permissions: the
// viewer may delete their own messages, not apps'; no Slack AI); Clack
// asks none and its rules assume exactly that, so they're accepted here
// and not fed anywhere.

// maIn is a case's inputs (and the snapshot's base: the same fields).
type maIn struct {
	Self        json.RawMessage            `json:"self,omitempty"`
	Team        *maTeam                    `json:"team,omitempty"`
	Users       []json.RawMessage          `json:"users,omitempty"`
	Conv        json.RawMessage            `json:"conv,omitempty"`
	Msg         json.RawMessage            `json:"msg,omitempty"`
	Root        json.RawMessage            `json:"root,omitempty"`
	Where       string                     `json:"where,omitempty"` // channel (default), thread, threads
	Subscribed  bool                       `json:"subscribed,omitempty"`
	Experiments map[string]string          `json:"experiments,omitempty"`
	Prefs       map[string]json.RawMessage `json:"prefs,omitempty"`
	TeamPrefs   map[string]json.RawMessage `json:"team_prefs,omitempty"`
	Features    map[string]bool            `json:"feature_access,omitempty"`
	Permissions *struct {
		Team    map[string]bool `json:"team,omitempty"`
		Channel map[string]bool `json:"channel,omitempty"`
	} `json:"permissions,omitempty"`
	Reporting bool `json:"reporting,omitempty"`
}

type maTeam struct {
	ID           string `json:"id"`
	Domain       string `json:"domain"`
	EnterpriseID string `json:"enterprise_id,omitempty"`
}

// maOut is what a message gets: the toolbar's buttons in order
// ("quick_reactions", "id: label" per button, "more" for ⋮; null when it
// has no toolbar), and the ⋮ and right-click menus' groups, each row
// "id: label" with a submenu's rows after " > ".
type maOut struct {
	Toolbar *[]string  `json:"toolbar"`
	Menu    [][]string `json:"menu"`
	Context [][]string `json:"context"`
}

// over is the case's inputs over the base, as the oracle merges them.
func (b maIn) over(c maIn) maIn {
	r := b
	if c.Self != nil {
		r.Self = c.Self
	}
	if c.Team != nil {
		r.Team = c.Team
	}
	r.Users = append(append([]json.RawMessage(nil), b.Users...), c.Users...)
	r.Conv, r.Msg, r.Root, r.Where = c.Conv, c.Msg, c.Root, c.Where
	r.Subscribed, r.Reporting = b.Subscribed || c.Subscribed, b.Reporting || c.Reporting
	r.Experiments = merged(b.Experiments, c.Experiments)
	r.Prefs = merged(b.Prefs, c.Prefs)
	r.TeamPrefs = merged(b.TeamPrefs, c.TeamPrefs)
	r.Features = merged(b.Features, c.Features)
	return r
}

func merged[V any](a, b map[string]V) map[string]V {
	r := maps.Clone(a)
	if r == nil {
		r = map[string]V{}
	}
	maps.Copy(r, b)
	return r
}

func maDecode(t *testing.T, what string, raw json.RawMessage, v any) {
	t.Helper()
	if err := json.Unmarshal(raw, v); err != nil {
		t.Fatalf("%s: %v", what, err)
	}
}

func TestMessageActionsLikeSlack(t *testing.T) {
	t.Cleanup(uipref.Reset)
	s := paritytest.Load[maIn, maOut](t, "testdata/parity/message-actions.json")
	var base maIn
	dec := json.NewDecoder(bytes.NewReader(s.Base))
	dec.DisallowUnknownFields()
	if err := dec.Decode(&base); err != nil {
		t.Fatalf("the snapshot's base: %v", err)
	}
	// every API call answers ok and nothing else: the case's state is all
	// there is, as on Slack's side
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"ok":true}`))
	}))
	t.Cleanup(srv.Close)
	s.Run(t, paritytest.Check[maIn, maOut]{Clack: func(t *testing.T, in maIn) maOut {
		return clackMessageActions(t, srv.URL, base.over(in))
	}})
}

// clackMessageActions builds a core over the case's state and asks
// Clack's toolbar and menus about its message.
func clackMessageActions(t *testing.T, api string, c maIn) maOut {
	t.Helper()
	var self slack.User
	maDecode(t, "self", c.Self, &self)
	users := []slack.User{self}
	for _, raw := range c.Users {
		var u slack.User
		maDecode(t, "users", raw, &u)
		users = append(users, u)
	}
	var conv slack.Conversation
	maDecode(t, "conv", c.Conv, &conv)
	var m slack.Message
	maDecode(t, "msg", c.Msg, &m)
	var root *slack.Message
	if len(c.Root) > 0 {
		root = new(slack.Message)
		maDecode(t, "root", c.Root, root)
	}
	team := c.Team
	if team == nil {
		t.Fatal("no team")
	}
	cl := slack.New(slack.Creds{Token: "xoxc-test", Cookie: "xoxd-test", UserID: self.ID, Team: team.ID, EnterpriseID: team.EnterpriseID,
		TeamURL: api + "/", EnterpriseURL: api + "/"})
	cl.EdgeURL = api + "/edge/"
	st := store.New("")
	st.Lock()
	st.TeamID, st.TeamDomain = team.ID, team.Domain
	st.Unlock()
	st.PutUsers(users)
	st.PutConv(conv)
	// the conversation's history: a reply's root, and the message unless
	// it's a reply only in its thread; the thread: the root and the reply
	reply := m.ThreadTS != "" && m.ThreadTS != m.TS
	var hist []slack.Message
	if root != nil {
		hist = append(hist, *root)
	}
	if !reply || m.Subtype == "thread_broadcast" {
		hist = append(hist, m)
	}
	st.SetLatest(conv.ID, hist, false)
	if root != nil {
		st.SetThread(conv.ID, root.TS, []slack.Message{*root, m})
	}
	prefs, muted := store.ParsePrefs(c.Prefs)
	st.SetRawPrefs(c.Prefs)
	st.SetPrefs(prefs, muted)
	uipref.Set(prefs)
	app := core.New(cl, st)
	app.S.Self = self
	app.ServeOffline()
	for k, v := range c.Experiments {
		app.SetExperimentGroup(k, v)
	}
	app.SetTeamPrefs(c.TeamPrefs)
	app.SetFeatures(c.Features)
	app.SetContentReporting(c.Reporting)
	rootTS := m.ThreadTS
	if rootTS == "" {
		rootTS = m.TS
	}
	if c.Subscribed {
		st.SetThreadSub(conv.ID, rootTS, func(s *store.ThreadSub) { s.Subscribed = true })
	}
	stored, ok := st.Find(conv.ID, m.TS)
	if !ok {
		t.Fatalf("message %s isn't in the store", m.TS)
	}
	msg := app.MessageFromSlack(conv.ID, &stored)

	where := map[string]popups.MessageContext{"": popups.InChannel, "channel": popups.InChannel, "thread": popups.InThread, "threads": popups.InThreadsView}
	w, ok := where[c.Where]
	if !ok {
		t.Fatalf("where %q", c.Where)
	}
	h := &ctxHost{coreHost: coreHost{app: app, th: theme.New(false)}}
	at := image.Pt(400, 300)
	out := maOut{Context: maGroups(popups.NewMessageContextMenu(h, at, msg, w, popups.MessageMenuOpts{}).Items())}
	if w == popups.InThreadsView {
		// Clack's Threads view draws no hover toolbar, so no ⋮ either: only
		// its right-click menu (views/threads.go messageMenu) opens there
		out.Menu = [][]string{}
		return out
	}
	out.Toolbar = maToolbar(app, &msg, w == popups.InThread)
	if out.Toolbar != nil && len(*out.Toolbar) > 0 && (*out.Toolbar)[len(*out.Toolbar)-1] == "more" {
		out.Menu = maGroups(popups.NewMessageMenuOpts(h, at, msg, popups.MessageMenuOpts{Where: w}).Items())
	} else {
		out.Menu = [][]string{} // no ⋮ to open it
	}
	return out
}

// maToolbar is the hover toolbar list.go draws for m (listState.toolbar):
// none when ToolbarShown says so, else the quick reactions when they're
// on and the message takes reactions, then toolbarButtons' buttons.
func maToolbar(app *core.App, m *core.Message, thread bool) *[]string {
	if !popups.ToolbarShown(app, m) {
		return nil
	}
	out := []string{}
	if quickReactionsOn(app, m.Channel) && reactable(app, m, thread) {
		out = append(out, "quick_reactions")
	}
	btns, label, _ := toolbarButtons(app, m, thread, nil)
	for _, b := range btns {
		if b == tbMore {
			out = append(out, "more")
			continue
		}
		out = append(out, tbActions[b]+": "+label[b])
	}
	return &out
}

// maGroups is a menu's rows in groups (between its separators).
func maGroups(items []popups.MenuItem) [][]string {
	out := [][]string{}
	var g []string
	for _, it := range items {
		if it.Separator {
			if len(g) > 0 {
				out = append(out, g)
			}
			g = nil
			continue
		}
		g = append(g, maRow(it))
	}
	if len(g) > 0 {
		out = append(out, g)
	}
	return out
}

// maReminder are the reminder row's ids: Slack draws its presets with
// RemindSubmenu (40eY's row has no submenuActions), so neither side lists
// them.
var maReminder = map[string]bool{"remind": true, "remind_later": true}

func maRow(it popups.MenuItem) string {
	s := it.Action + ": " + it.Label
	if len(it.Sub) > 0 && !maReminder[it.Action] {
		var subs []string
		for _, x := range it.Sub {
			if !x.Separator {
				subs = append(subs, x.Action+": "+x.Label)
			}
		}
		s += " > " + strings.Join(subs, ", ")
	}
	return s
}
