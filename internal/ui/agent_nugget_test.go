package ui

import (
	"encoding/json"
	"image"
	"testing"
	"time"

	"gioui.org/layout"
	"gioui.org/op"

	"github.com/some-du6e/clack/internal/core"
	"github.com/some-du6e/clack/internal/slack"
)

// The thread footer's app nugget (agent_nugget.go) over an offline core:
// nothing without an app status or without app_thread_nugget_fe; the chip
// (app_nugget_refinement: 6px + 4px + a 22px line + 4px + 6px) or the
// shimmering row (8px + 22px + 8px); the label takes the loading
// messages in turn, else Slack's working text.
func TestAppThreadNugget(t *testing.T) {
	e, _ := codeShell(t)
	a, ca := e.a, e.ca
	ca.S.PutUsers([]slack.User{{ID: "UAPP", Name: "kyto", IsBot: true, RealName: "Kyto Agent"}})
	ca.SetExperimentGroup("app_thread_nugget_fe", "on")
	ca.SetExperimentGroup("app_nugget_refinement", "on")
	nug := func() int {
		ops := new(op.Ops)
		gtx := ttContext(ops, image.Pt(600, 200))
		gtx.Constraints = layout.Constraints{Max: image.Pt(600, 200)}
		return a.layoutAppThreadNugget(gtx, "D1", "1.0").Size.Y
	}
	if h := nug(); h != 0 {
		t.Errorf("no status: a %dpx nugget", h)
	}
	ca.Inject(slack.Event{Type: "ai_assistant_status", Raw: json.RawMessage(`{"type":"ai_assistant_status","channel_id":"D1",` +
		`"thread_ts":"1.0","bot_user_id":"UAPP","username":"Kyto","status":"is thinking…","loading_messages":["Reading…","Writing…"]}`)})
	if h := nug(); h != 42 {
		t.Errorf("chip nugget %dpx, want 42", h)
	}
	if bot, ok := ca.SuppressBotTyping("D1", "1.0"); !ok || bot != "UAPP" {
		t.Errorf("the composer keeps the app in its typing line (%q %v)", bot, ok)
	}
	ca.SetExperimentGroup("app_nugget_refinement", "off")
	if h := nug(); h != 38 {
		t.Errorf("row nugget %dpx, want 38", h)
	}
	for _, g := range []string{"off", ""} {
		ca.SetExperimentGroup("app_thread_nugget_fe", g)
		if h := nug(); h != 0 {
			t.Errorf("app_thread_nugget_fe %q: a %dpx nugget", g, h)
		}
	}

	n := core.AppNugget{Name: "Kyto", LoadingMessages: []string{"Reading…", "Writing…"}, Chip: true}
	t0 := time.Now()
	for _, c := range []struct {
		after time.Duration
		want  string
	}{{0, "Reading…"}, {2500 * time.Millisecond, "Writing…"}, {5 * time.Second, "Reading…"}} {
		if l := appNuggetLabel(n, t0, t0.Add(c.after)); l != c.want {
			t.Errorf("after %v: %q, want %q", c.after, l, c.want)
		}
	}
	n.LoadingMessages = nil
	if l := appNuggetLabel(n, t0, t0); l != "Kyto is working" {
		t.Errorf("chip label %q", l)
	}
	n.Chip = false
	if l := appNuggetLabel(n, t0, t0); l != "Kyto is working…" {
		t.Errorf("row label %q", l)
	}
}
