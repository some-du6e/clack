package messages

// Who edited, who pinned (docs/MESSAGES-AUDIT.md "Edited by someone
// else", "Pinned by {member} link"), against Slack's CDN build
// 4d0c78085232: getMessageLabelValues (XSZB) hands EditedLabel (Y5SQ) an
// editedByUserId only for a workflow message, and PinnedLabel (GSoE)
// renders the pinner as MemberLink (25Rg). Synthetic only.

import (
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/some-du6e/clack/internal/core"
	"github.com/some-du6e/clack/internal/slack"
	"github.com/some-du6e/clack/internal/ui/theme"
)

func TestEditedByLikeSlack(t *testing.T) {
	app, _, ts := shotsApp(t)
	host := coreHost{app: app, th: theme.New(false)}
	mk := func(wf string) *row {
		e := entry{rev: 1, m: core.Message{TS: ts["b1"], Channel: tShots, User: "UANA00001", Edited: true, EditedTS: ts["b2"], EditedBy: "UBEN00001", WorkflowID: wf}}
		r := newRow(&e)
		r.rc = &rowCtx{h: host, selfID: "USELF"}
		return r
	}
	if got := mk("Wf0123456").rvOpts().EditedBy; got != "ben" {
		t.Errorf("a workflow message edited by ben: EditedLabel's name %q, want %q", got, "ben")
	}
	if got := mk("").rvOpts().EditedBy; got != "" {
		t.Errorf("a member's message: Slack passes no editedByUserId, got %q", got)
	}
}

// labelRec records what the labels drew this frame.
type labelRec struct {
	mu  sync.Mutex
	got []string
	bld map[string]bool
}

func (l *labelRec) hook() func(string, textStyle) {
	return func(txt string, s textStyle) {
		l.mu.Lock()
		l.got = append(l.got, txt)
		if l.bld == nil {
			l.bld = map[string]bool{}
		}
		l.bld[txt] = s.weight >= theme.Bold
		l.mu.Unlock()
	}
}

func TestPinnedByMemberLink(t *testing.T) {
	app, _, ts := shotsApp(t)
	root, _ := app.S.Find(tShots, ts["root"])
	r1 := slack.Message{Type: "message", TS: ts["r1"], User: "UBEN00001", Text: "Looks good.", ThreadTS: ts["root"], Blocks: shotRich(shotSection("Looks good."))}
	r1.PinnedTo, r1.PinnedInfo = []string{tShots}, &slack.PinnedInfo{Channel: tShots, PinnedBy: "UANA00001"}
	app.S.SetThread(tShots, ts["root"], []slack.Message{root, r1})
	host := &profileHost{coreHost: coreHost{app: app, th: theme.New(false)}}
	tv := NewThreadView(host, tShots, ts["root"])
	f := frameFn(tv)
	until(t, 5*time.Second, "the pinned reply", f, func() bool { r := tv.s.rows[ts["r1"]]; return r != nil && r.m.Pinned })
	var rec labelRec
	labelHook = rec.hook()
	f()
	labelHook = nil
	joined := strings.Join(rec.got, "|")
	if !strings.Contains(joined, "Pinned by |ana") {
		t.Fatalf("labels %q: want \"Pinned by \" then the member link \"ana\"", rec.got)
	}
	if !rec.bld["ana"] || rec.bld["Pinned by "] {
		t.Errorf("MemberName's strong: the name bold, the rest not (%v)", rec.bld)
	}
	// MemberLink's click opens the member's profile
	r := tv.s.rows[ts["r1"]]
	r.pinnedLink.Click()
	f()
	f()
	if host.count() != 1 || host.opened[0] != "UANA00001" {
		t.Errorf("the link opened %v, want ana's profile", host.opened)
	}
}
