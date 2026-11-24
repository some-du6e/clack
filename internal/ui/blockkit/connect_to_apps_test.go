package blockkit

import (
	"context"
	"encoding/json"
	"net/url"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/some-du6e/clack/internal/core"
	"github.com/some-du6e/clack/internal/fakeslack"
	"github.com/some-du6e/clack/internal/slack"
	"github.com/some-du6e/clack/internal/store"
	"github.com/some-du6e/clack/internal/ui/theme"
)

// The message menu's Connect to apps: the account's message shortcuts in
// the frecency store's order (the "frecency" pref, which every device
// writes), the first three of them, then the browse row (JuvA e8 over
// 2LNc and qGxq; shortcuts.go).

// ctaActions are the team's app actions: five message shortcuts of two
// synthetic apps, and a global one.
var ctaActions = []any{
	map[string]any{"app_id": "A0CTAONE1", "app_name": "Synthetic Tracker", "team_id": fakeslack.TeamID, "actions": []any{
		map[string]any{"action_id": "X1", "app_id": "A0CTAONE1", "name": "File a bug", "type": "message_action", "callback_id": "x1"},
		map[string]any{"action_id": "X2", "app_id": "A0CTAONE1", "name": "Link to a bug", "type": "message_action", "callback_id": "x2"},
		map[string]any{"action_id": "G1", "app_id": "A0CTAONE1", "name": "New project", "type": "global_action", "callback_id": "g1"},
	}},
	map[string]any{"app_id": "A0CTATWO1", "app_name": "Synthetic Notes", "team_id": fakeslack.TeamID, "actions": []any{
		map[string]any{"action_id": "X3", "app_id": "A0CTATWO1", "name": "Save as a note", "type": "message_action", "callback_id": "x3"},
		map[string]any{"action_id": "X4", "app_id": "A0CTATWO1", "name": "Summarize", "type": "message_action", "callback_id": "x4"},
		map[string]any{"action_id": "X5", "app_id": "A0CTATWO1", "name": "Translate", "type": "message_action", "callback_id": "x5"},
	}},
}

// ctaFrecency is a frecency pref where the listed message actions were
// used count times each, the last a minute ago: the first listed scores
// highest.
func ctaFrecency(ids ...string) string {
	now := time.Now().UnixMilli()
	m := map[string]any{}
	for i, id := range ids {
		n := 9 - 2*i
		visits := make([]int64, 0, n)
		for k := 0; k < n && k < 10; k++ {
			visits = append(visits, now-int64(60000*(k+1)))
		}
		m[id] = map[string]any{"id": id, "count": n, "visits": visits}
	}
	b, _ := json.Marshal(m)
	return string(b)
}

type ctaEnv struct {
	app   *core.App
	fs    *fakeslack.Server
	h     *agentHost
	ch    string
	mu    sync.Mutex
	calls map[string][]url.Values
}

func (e *ctaEnv) called(method string) []url.Values {
	e.mu.Lock()
	defer e.mu.Unlock()
	return append([]url.Values(nil), e.calls[method]...)
}

func newCTAEnv(t *testing.T, actions []any, frecency string, setup ...func(fs *fakeslack.Server)) *ctaEnv {
	t.Helper()
	fs, err := fakeslack.Start(fakeslack.Small)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { fs.Close() })
	for _, f := range setup {
		f(fs)
	}
	e := &ctaEnv{fs: fs, calls: map[string][]url.Values{}}
	fs.SetHook(func(method string, f url.Values) (any, bool) {
		e.mu.Lock()
		e.calls[method] = append(e.calls[method], f)
		e.mu.Unlock()
		if method == "apps.actions.v2.list" {
			return map[string]any{"ok": true, "app_actions": actions}, true
		}
		return nil, false
	})
	if frecency != "" {
		fs.SetPrefs(map[string]any{"frecency": frecency})
	}
	app := core.New(slack.New(fs.Creds()), store.New(t.TempDir()))
	t.Cleanup(app.Shutdown)
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	defer cancel()
	if err := app.Boot(ctx); err != nil {
		t.Fatal(err)
	}
	if err := app.LoadFrecencyPrefs(ctx); err != nil {
		t.Fatal(err)
	}
	app.S.Lock()
	for id, c := range app.S.Convs {
		if c.IsChannel && !c.IsPrivate && !c.IsIM && !c.IsMPIM && !c.IsArchived && c.IsMember && (e.ch == "" || id < e.ch) {
			e.ch = id
		}
	}
	app.S.Unlock()
	if e.ch == "" {
		t.Fatal("no channel the viewer is in")
	}
	e.app, e.h = app, &agentHost{app: app, th: theme.New(false)}
	return e
}

// rows is the submenu for a message in the env's channel once the list
// is loaded: "label|right|qa" per row.
func (e *ctaEnv) rows(t *testing.T) []string {
	t.Helper()
	m := core.Message{Channel: e.ch, TS: "1762600000.000100", User: "U0FAKE00001", Text: "a synthetic message"}
	deadline := time.Now().Add(10 * time.Second)
	for {
		if ctaLoaded(e.app) {
			break
		}
		if time.Now().After(deadline) {
			t.Fatal("the shortcuts list never loaded")
		}
		time.Sleep(50 * time.Millisecond)
	}
	var out []string
	for _, it := range MessageShortcutItems(e.h, m) {
		out = append(out, it.Label+"|"+it.Right+"|"+it.QA)
	}
	return out
}

// The submenu lists the message shortcuts the frecency pref ranks first,
// in its order, then the rest in the list's order, three in all; with
// more than three the browse row opens the dialog.
func TestConnectToAppsOrderLikeSlack(t *testing.T) {
	e := newCTAEnv(t, ctaActions, ctaFrecency("MAX4", "MAX2"))
	got := strings.Join(e.rows(t), "\n")
	want := strings.Join([]string{
		"Summarize|Synthetic Notes|app_action_X4",
		"Link to a bug|Synthetic Tracker|app_action_X2",
		"File a bug|Synthetic Tracker|app_action_X1",
		"More message shortcuts…||app_message_actions_dialog",
	}, "\n")
	if got != want {
		t.Errorf("Connect to apps:\n%s\nwant:\n%s", got, want)
	}
	if n := len(e.called("users.prefs.set")); n != 0 {
		t.Errorf("opening the submenu wrote the prefs %d times", n)
	}
}

// The browse row by how many message shortcuts there are: three or fewer
// link to the app directory ("More message shortcuts…"), none says "Add
// a message shortcut…".
func TestConnectToAppsBrowseRowLikeSlack(t *testing.T) {
	two := []any{map[string]any{"app_id": "A0CTAONE1", "app_name": "Synthetic Tracker", "team_id": fakeslack.TeamID, "actions": []any{
		map[string]any{"action_id": "X1", "app_id": "A0CTAONE1", "name": "File a bug", "type": "message_action", "callback_id": "x1"},
		map[string]any{"action_id": "X2", "app_id": "A0CTAONE1", "name": "Link to a bug", "type": "message_action", "callback_id": "x2"},
	}}}
	for _, c := range []struct {
		name    string
		actions []any
		want    []string
	}{
		{"two", two, []string{"File a bug|Synthetic Tracker|app_action_X1", "Link to a bug|Synthetic Tracker|app_action_X2", "More message shortcuts…||app_directory_link"}},
		{"none", []any{}, []string{"Add a message shortcut…||app_directory_link"}},
	} {
		t.Run(c.name, func(t *testing.T) {
			e := newCTAEnv(t, c.actions, "")
			if got := e.rows(t); strings.Join(got, "\n") != strings.Join(c.want, "\n") {
				t.Errorf("rows %q, want %q", got, c.want)
			}
		})
	}
}

// A change another device made reaches the order through Slack's sync
// path: the pref_change for "frecency", then the pref read again.
func TestConnectToAppsFollowsOtherDevices(t *testing.T) {
	e := newCTAEnv(t, ctaActions, ctaFrecency("MAX4", "MAX2"))
	if r := e.rows(t); len(r) == 0 || !strings.HasPrefix(r[0], "Summarize|") {
		t.Fatalf("before: %q", r)
	}
	v := ctaFrecency("MAX5", "MAX3")
	e.fs.SetPrefs(map[string]any{"frecency": v})
	raw, _ := json.Marshal(map[string]any{"type": "pref_change", "name": "frecency", "value": v})
	e.app.Inject(slack.Event{Type: "pref_change", Raw: raw})
	deadline := time.Now().Add(10 * time.Second)
	for {
		r := e.rows(t)
		if len(r) >= 2 && strings.HasPrefix(r[0], "Translate|") && strings.HasPrefix(r[1], "Save as a note|") {
			break
		}
		if time.Now().After(deadline) {
			t.Fatalf("after the other device's change: %q, want Translate, then Save as a note", r)
		}
		time.Sleep(100 * time.Millisecond)
	}
}

// A viewer who can't run app actions (canUserRunAppAction, RS5d: a
// single-channel guest; a guest under commands_only_regular) gets no
// shortcut rows, but the browse row stays: JuvA e8 builds it outside
// RS5d's check, so the submenu is never Clack's disabled "No apps".
func TestConnectToAppsGuestsKeepBrowse(t *testing.T) {
	for _, c := range []struct {
		name  string
		prefs map[string]any
		guest func(u *slack.User)
	}{
		{"single-channel guest", nil, func(u *slack.User) { u.IsRestricted, u.IsUltraRestricted = true, true }},
		{"guest, commands_only_regular", map[string]any{"commands_only_regular": true}, func(u *slack.User) { u.IsRestricted = true }},
	} {
		t.Run(c.name, func(t *testing.T) {
			e := newCTAEnv(t, ctaActions, ctaFrecency("MAX4", "MAX2"), func(fs *fakeslack.Server) { fs.TeamPrefs = c.prefs })
			u, ok := e.app.S.User(e.app.S.SelfID())
			if !ok || u == nil {
				t.Fatal("no self user")
			}
			e.app.S.Lock()
			c.guest(u)
			e.app.S.Unlock()
			want := "More message shortcuts…||app_message_actions_dialog"
			if got := strings.Join(e.rows(t), "\n"); got != want {
				t.Errorf("Connect to apps for a %s:\n%s\nwant:\n%s", c.name, got, want)
			}
		})
	}
}
