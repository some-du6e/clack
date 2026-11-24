package messages

import (
	"image"
	"testing"
	"time"

	"github.com/some-du6e/clack/internal/core"
	"github.com/some-du6e/clack/internal/slack"
	"github.com/some-du6e/clack/internal/ui/popups"
	"github.com/some-du6e/clack/internal/ui/theme"
)

// More of Slack's action rules on messages that aren't ordinary ones
// (popups/actions_rules.go): the follow, reminder and Copy message rows,
// the right-click menu, and Delete on a send that never went out.

const (
	tRules       = "C0RULESMS"
	tsRulesPlain = "1700000601.000100"
	tsRulesJoin  = "1700000602.000100"
	tsRulesFile  = "1700000603.000100"
	tsRulesHid   = "1700000604.000100"
	tsRulesMod   = "1700000605.000100"
)

func newRulesEnv(t *testing.T) *ctxEnv {
	t.Helper()
	return newCtxEnvIn(t, tRules, []slack.Message{
		{Type: "message", TS: tsRulesPlain, User: "UALICE", Text: "a synthetic note"},
		{Type: "message", TS: tsRulesJoin, User: "UBOB", Subtype: "channel_join", Text: "<@UBOB> has joined the channel"},
		{Type: "message", TS: tsRulesFile, User: "UBOB", Subtype: "file_share", Files: []slack.File{{ID: "F0RULES01", Name: "plan.pdf", Title: "plan.pdf",
			Mimetype: "application/pdf", Filetype: "pdf", PrettyType: "PDF", Size: 1024}}},
		{Type: "message", TS: tsRulesHid, User: "UALICE", Subtype: "thread_hidden", Text: "a synthetic hidden thread"},
		{Type: "message", TS: tsRulesMod, User: "UALICE", Subtype: "moderated", Text: "This message was removed."},
	})
}

// The ⋮ menu's follow row (QdFb: canStartOrOpenThread), reminder row
// (canAddReminder's allow-list) and Copy message (canCopyMessageText: no
// automated subtype, text or blocks): an ordinary message has all three,
// a channel_join none, a file_share alone the reminder only (no thread
// starts from a file subtype; nothing to copy).
func TestMenuRowsByMessageKindLikeSlack(t *testing.T) {
	e := newRulesEnv(t)
	items := func(ts string) []popups.MenuItem {
		r := e.v.s.rows[ts]
		if r == nil {
			t.Fatalf("no row %s", ts)
		}
		return popups.NewMessageMenuOpts(e.h, image.Pt(400, 300), r.src, popups.MessageMenuOpts{}).Items()
	}
	for _, c := range []struct {
		name, ts             string
		follow, remind, copy bool
	}{
		{"ordinary", tsRulesPlain, true, true, true},
		{"channel_join", tsRulesJoin, false, false, false},
		{"file alone", tsRulesFile, false, true, false},
	} {
		its := items(c.ts)
		for _, row := range []struct {
			action string
			want   bool
		}{{"subscribe", c.follow}, {"remind", c.remind}, {"copy_text", c.copy}} {
			if _, ok := ephItem(its, row.action); ok != row.want {
				t.Errorf("%s: %s row %v, want %v", c.name, row.action, ok, row.want)
			}
		}
	}
}

// A right-click on a thread_hidden or moderated message opens nothing
// (MessageActionsContextMenu, DtN2); on an ordinary one the menu.
func TestContextMenuNotOnRestrictedMessages(t *testing.T) {
	e := newRulesEnv(t)
	e.rightClick(e.find(tsRulesPlain, "body text", onText))
	if len(e.h.menus()) != 1 {
		t.Fatalf("right-click on an ordinary message: %d menus", len(e.h.menus()))
	}
	for _, ts := range []string{tsRulesHid, tsRulesMod} {
		n := len(e.h.menus())
		e.rightClick(e.find(ts, "body text", onText))
		if ms := e.h.menus(); len(ms) != n {
			t.Errorf("right-click on %s opened %q", ts, labels(ms[len(ms)-1]))
		}
	}
}

// Delete message… on your own send that failed removes it here alone:
// its ts is the client's, so Slack's delete only animates it out
// (animateAndDeleteMessageApi: isTempTs) and chat.delete never goes out.
func TestFailedSendDeleteIsLocal(t *testing.T) {
	app, f := newTestApp(t)
	f.mu.Lock()
	f.fail["chat.postMessage"] = "fatal_error"
	f.mu.Unlock()
	const cid = "c0ffee00-0000-4000-8000-00000000f1d1"
	select {
	case err := <-ownSend(app, core.SendOpts{Channel: tGen, Text: "a synthetic send that fails", ClientMsgID: cid}):
		if err == nil {
			t.Fatal("the send didn't fail")
		}
	case <-time.After(20 * time.Second):
		t.Fatal("the send never answered")
	}
	e := &ctxEnv{t: t, h: &ctxHost{coreHost: coreHost{app: app, th: theme.New(false)}}, now: time.Now()}
	e.v = NewChannelView(e.h, tGen)
	e.v.Preview = true
	var failed *row
	until(t, 5*time.Second, "the failed send's row", e.frame, func() bool {
		for _, r := range e.v.s.rows {
			if r.src.Failed && r.src.ClientMsgID == cid {
				failed = r
				return true
			}
		}
		return false
	})
	it, ok := ephItem(popups.NewMessageMenuOpts(e.h, image.Pt(400, 300), failed.src, popups.MessageMenuOpts{}).Items(), "delete_message")
	if !ok || it.Do == nil {
		t.Fatal("no Delete message… on the failed send")
	}
	it.Do()
	var modal *popups.Modal
	for _, o := range e.h.overlays {
		if m, ok := o.(*popups.Modal); ok {
			modal = m
		}
	}
	if modal == nil || !modal.Press("Delete") {
		t.Fatal("Delete message… asked nothing")
	}
	until(t, 5*time.Second, "the failed send gone", e.frame, func() bool {
		for _, r := range e.v.s.rows {
			if r.src.ClientMsgID == cid {
				return false
			}
		}
		return true
	})
	time.Sleep(200 * time.Millisecond) // a chat.delete would have gone out by now
	if calls := f.called("chat.delete"); len(calls) != 0 {
		t.Errorf("chat.delete went out for the failed send: %v", calls)
	}
}
