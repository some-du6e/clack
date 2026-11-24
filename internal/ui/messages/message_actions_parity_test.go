package messages

import (
	"bytes"
	"encoding/json"
	"go/ast"
	"go/parser"
	"go/printer"
	"go/token"
	"image"
	"maps"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"gioui.org/f32"

	"github.com/some-du6e/clack/internal/core"
	"github.com/some-du6e/clack/internal/paritytest"
	"github.com/some-du6e/clack/internal/slack"
	"github.com/some-du6e/clack/internal/store"
	"github.com/some-du6e/clack/internal/ui/blockkit"
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
// asks none and its rules assume the base's answers, so they're accepted
// here and not fed anywhere: a case that changes them (Slackbot AI on) is
// a divergence. A saved message carries message.saved as history has it;
// Clack takes saved state from saved.list and the saved events instead,
// so it gets the same item as a saved_added event (App.Inject).
//
// Clack's side runs Clack's own paths: the toolbar's rules
// (popups.ToolbarShown, quickReactionsShown, toolbarButtons), the ⋮ menu
// the toolbar opens (popups.NewMessageMenuOpts) and the list's
// right-click (listState.contextMenu, on the row off its content). The
// Threads view (internal/ui/views) imports this package, so its side is
// mirrored here and maThreadsView fails when the code it mirrors changes.

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
	// Connect to apps' rows, as the shell wires them (ui/blockkit_wire.go)
	prevShortcuts := popups.MessageShortcuts
	popups.MessageShortcuts = func(h popups.Host, m core.Message) []popups.MenuItem { return blockkit.MessageShortcutItems(h, m) }
	t.Cleanup(func() { popups.MessageShortcuts = prevShortcuts })
	maThreadsView(t)
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
	t.Cleanup(app.StopTraffic) // a saved event's lookup runs after it
	for _, raw := range []json.RawMessage{c.Root, c.Msg} {
		maSaved(t, app, conv.ID, raw)
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
	if w == popups.InThreadsView {
		// views/threads.go (maThreadsView checks it): its rows draw no hover
		// toolbar, so no ⋮ either, and a right-click off the content opens
		// messageMenu's NewMessageContextMenu
		return maOut{Menu: [][]string{}, Context: maGroups(popups.NewMessageContextMenu(h, at, msg, popups.InThreadsView, popups.MessageMenuOpts{}).Items())}
	}
	out := maOut{Context: maContext(t, h, rootTS, msg, w == popups.InThread)}
	h.overlays = nil
	out.Toolbar = maToolbar(app, &msg, w == popups.InThread)
	if out.Toolbar != nil && len(*out.Toolbar) > 0 && (*out.Toolbar)[len(*out.Toolbar)-1] == "more" {
		// the ⋮ button's menu (list.go's tbMore): its hooks don't change
		// its rows
		out.Menu = maGroups(popups.NewMessageMenuOpts(h, at, msg, popups.MessageMenuOpts{Where: w}).Items())
	} else {
		out.Menu = [][]string{} // no ⋮ to open it
	}
	return out
}

// maSaved hands Clack a saved message's item: history's message.saved
// (raw, the message as history has it), which saved.list lists too, as
// the saved_added event that brings it.
func maSaved(t *testing.T, app *core.App, ch string, raw json.RawMessage) {
	t.Helper()
	if len(raw) == 0 {
		return
	}
	var m struct {
		TS    string                     `json:"ts"`
		Saved map[string]json.RawMessage `json:"saved"`
	}
	maDecode(t, "saved", raw, &m)
	if m.Saved == nil {
		return
	}
	it := maps.Clone(m.Saved)
	for k, v := range map[string]string{"item_type": "message", "item_id": ch, "ts": m.TS} {
		b, _ := json.Marshal(v)
		it[k] = b
	}
	ev, err := json.Marshal(map[string]any{"type": "saved_added", "saved": it})
	if err != nil {
		t.Fatal(err)
	}
	app.Inject(slack.Event{Type: "saved_added", Raw: ev})
	if !app.SavedState(ch, m.TS) {
		t.Fatalf("saved_added left %s/%s unsaved", ch, m.TS)
	}
}

// maContext is the right-click menu the list opens on m: the list's own
// path (listState.contextMenu, contextMenuFor, messageMenu) for a press
// on m's row off its content, reactions and time, nothing selected, in a
// list holding just that row (a thread pane's when thread, whose root is
// rootTS). nil: no menu opens.
func maContext(t *testing.T, h *ctxHost, rootTS string, m core.Message, thread bool) [][]string {
	t.Helper()
	s := &listState{ch: m.Channel, thread: thread, rows: map[string]*row{m.TS: {src: m, m: m}},
		items: []item{{kind: itMsg, key: m.TS}}, heights: []int{40}, pos: []int{0}, visible: [2]int{0, 1}}
	if thread {
		s.ts = rootTS
	}
	s.ctx = ctxPress{pos: f32.Pt(8, 8), ok: true}
	s.contextMenu(h, image.Point{})
	ms := h.menus()
	switch len(ms) {
	case 0:
		return nil
	case 1:
		return maGroups(ms[0].Items())
	}
	t.Fatalf("a right-click opened %d menus", len(ms))
	return nil
}

// maThreadsViewMenu is views/threads.go's messageMenu, comments aside:
// clackMessageActions' Threads view side mirrors it.
const maThreadsViewMenu = `{
	ph, ok := v.h.(popups.Host)
	if !ok {
		v.h.OpenMessageMenu(image.Point{}, m)
		return
	}
	var at image.Point
	if pp, ok := v.h.(interface{ PointerPos() image.Point }); ok {
		at = pp.PointerPos()
	}
	if o, ok := v.contentMenu(gtx, ph, at, m); ok {
		ph.ShowOverlay(o)
		return
	}
	ph.ShowOverlay(popups.NewMessageContextMenu(ph, at, m, popups.InThreadsView, popups.MessageMenuOpts{}))
}`

// maThreadsViewToolbar are the toolbar's and the ⋮ menu's rules: the
// Threads view's code calls none while it draws no hover toolbar.
var maThreadsViewToolbar = map[string]bool{"ToolbarShown": true, "MoreShown": true, "NewMessageMenuOpts": true, "TodoShown": true, "AddToListShown": true}

// maThreadsView checks the Threads view's code that clackMessageActions
// mirrors: messageMenu's code is maThreadsViewMenu, and its files
// (threads*.go) call none of the toolbar's rules. Either changing fails
// here, until the mirror follows.
func maThreadsView(t *testing.T) {
	t.Helper()
	files, err := filepath.Glob(filepath.Join("..", "views", "threads*.go"))
	if err != nil || len(files) == 0 {
		t.Fatalf("the Threads view's files: %v %v", files, err)
	}
	fset := token.NewFileSet()
	found := false
	for _, f := range files {
		if strings.HasSuffix(f, "_test.go") {
			continue
		}
		src, err := os.ReadFile(f)
		if err != nil {
			t.Fatal(err)
		}
		file, err := parser.ParseFile(fset, f, src, 0)
		if err != nil {
			t.Fatal(err)
		}
		ast.Inspect(file, func(n ast.Node) bool {
			switch n := n.(type) {
			case *ast.FuncDecl:
				if n.Name.Name == "messageMenu" && n.Recv != nil && len(n.Recv.List) == 1 && maRecvType(n.Recv.List[0].Type) == "*Threads" {
					found = true
					var b bytes.Buffer
					if err := printer.Fprint(&b, fset, n.Body); err != nil {
						t.Fatal(err)
					}
					if strings.Join(strings.Fields(b.String()), " ") != strings.Join(strings.Fields(maThreadsViewMenu), " ") {
						t.Errorf("%s: the Threads view's right-click (messageMenu) changed; mirror it in clackMessageActions and maThreadsViewMenu:\n%s", f, b.String())
					}
				}
			case *ast.SelectorExpr:
				if x, ok := n.X.(*ast.Ident); ok && x.Name == "popups" && maThreadsViewToolbar[n.Sel.Name] {
					t.Errorf("%s: the Threads view calls popups.%s, a hover toolbar rule; clackMessageActions draws it no toolbar", fset.Position(n.Pos()), n.Sel.Name)
				}
			}
			return true
		})
	}
	if !found {
		t.Error("the Threads view has no (*Threads).messageMenu any more; mirror its right-click in clackMessageActions")
	}
}

// maRecvType is a receiver's type as written (*Threads).
func maRecvType(e ast.Expr) string {
	if s, ok := e.(*ast.StarExpr); ok {
		return "*" + maRecvType(s.X)
	}
	if id, ok := e.(*ast.Ident); ok {
		return id.Name
	}
	return ""
}

// maButtonIDs are the toolbar slots' Slack action ids (uAhD), which
// Slack's MessageActions keys each button by (renderButtonForAction's
// `${id}_button`); ⋮ is MessageActionButtonEllipsis, Slack's "more".
// Each slot is one Slack action in Clack (the to-do slot is Yoyn's
// addToLaterTodos, whose id stays add_to_later_todos as "Complete
// to-do"), so a slot that comes to offer another action (Slack's later,
// "Save for later", with later_todos off) needs its id picked here too.
var maButtonIDs = [tbCount]string{"add_reaction", "comment", "share_message", "add_to_later_todos", "add_to_list", "more"}

// maToolbar is the hover toolbar list.go draws for m (listState.toolbar):
// none when ToolbarShown says so, else the quick reactions when
// quickReactionsShown says so, then toolbarButtons' buttons.
func maToolbar(app *core.App, m *core.Message, thread bool) *[]string {
	if !popups.ToolbarShown(app, m) {
		return nil
	}
	out := []string{}
	if quickReactionsShown(app, m.Channel, m, thread) {
		out = append(out, "quick_reactions")
	}
	btns, label, _ := toolbarButtons(app, m, thread, nil)
	for _, b := range btns {
		if b == tbMore {
			out = append(out, "more")
			continue
		}
		out = append(out, maButtonIDs[b]+": "+label[b])
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
