package blockkit

// Karim (#clack, 2026-11-21): Block Kit Builder's Send to Slack posts its
// template as you, and in Clack it "doesn't work": every button of it
// ended in the block's error, "This app is no longer installed." (core
// refuses blocks.actions without a service_id). Slack's message blocks'
// onAction (boot-shared, the message renderer: canDispatch = !!serviceId,
// serviceId = bot_id || (isSlackbotMessage && hasQuipMigrationFeedbackBlocks
// ? "BSLACKBOT")) returns before sendActions for a message with no service,
// unless the block is a widget: (then BSLACKBOT); its interactive-state
// wrapper (useBlockKitInteractiveState) sets the block loading, calls it,
// and takes the loading state off in the same tick when nothing comes
// back. So in Slack a person's buttons send nothing, show no spinner and
// no error, and a url button opens its link. Synthetic fixtures (the
// template is Slack's public example).

import (
	"context"
	"encoding/json"
	"fmt"
	"slices"
	"testing"
	"time"

	"github.com/some-du6e/clack/internal/core"
	"github.com/some-du6e/clack/internal/fakeslack"
	"github.com/some-du6e/clack/internal/richtext"
	"github.com/some-du6e/clack/internal/slack"
)

// builderMessage is the gallery's builder/preview as core serves it.
func builderMessage(t *testing.T, e *bkEnv) core.Message {
	t.Helper()
	ts := e.fs.GalleryTS(fakeslack.GalleryBuilderPreview)
	if ts == "" {
		t.Fatalf("no gallery message %s", fakeslack.GalleryBuilderPreview)
	}
	before := ""
	for range 10 {
		pg, err := e.h.a.History(context.Background(), fakeslack.GalleryChannelID, before)
		if err != nil {
			t.Fatal(err)
		}
		for _, m := range pg.Messages {
			if m.TS == ts {
				return m
			}
		}
		if len(pg.Messages) == 0 || !pg.HasMore {
			break
		}
		before = pg.Messages[0].TS // ascending: the next page is older
	}
	t.Fatalf("%s (%s) not in #%s's history", fakeslack.GalleryBuilderPreview, ts, fakeslack.GalleryChannelName)
	return core.Message{}
}

// TestBuilderPreviewButtons: the template posted by the viewer (no bot_id,
// bot_profile or app) draws its three buttons live. Farmhouse sends
// nothing and shows nothing; Kin Khao and Ler Ros open https://google.com
// and send nothing either.
func TestBuilderPreviewButtons(t *testing.T) {
	e := newBKEnv(t, false, false)
	m := builderMessage(t, e)
	if m.User != fakeslack.SelfID || m.BotID != "" || m.BotProfile != nil || m.Subtype != "" || m.IsBot {
		t.Fatalf("builder/preview: user %q bot %q profile %+v subtype %q, want the viewer's own message", m.User, m.BotID, m.BotProfile, m.Subtype)
	}
	var act *richtext.Block
	for i := range m.Doc.Blocks {
		if m.Doc.Blocks[i].Kind == richtext.Actions {
			act = &m.Doc.Blocks[i]
		}
	}
	if act == nil || len(act.Buttons) != 3 {
		t.Fatalf("no actions block with the template's 3 buttons (%+v)", act)
	}
	e.msgs["builder"] = m
	me := e.show("builder")
	n0 := len(e.fs.BlockActionCalls())
	var want []string
	for _, bt := range act.Buttons {
		w := e.clickEl(bt.BlockID, bt.ActionID)
		if w.busy || me.loading(bt.BlockID) {
			t.Errorf("%s: loading after the click; Slack takes the state off in the same tick", bt.Text)
		}
		e.noNewCall(n0, 400*time.Millisecond, bt.Text+" on a person's message")
		if len(me.errs) != 0 || w.busy || me.loading(bt.BlockID) {
			t.Errorf("%s: block errors %q, busy %v, loading %v; Slack shows nothing", bt.Text, me.errs, w.busy, me.loading(bt.BlockID))
		}
		if bt.URL != "" {
			want = append(want, bt.URL)
		}
		if !slices.Equal(e.urls, want) {
			t.Errorf("after %s: opened %q, want %q", bt.Text, e.urls, want)
		}
	}
	if !slices.Equal(want, []string{"https://google.com", "https://google.com"}) {
		t.Errorf("the template's url buttons: %q", want)
	}
	if len(e.h.toasts) != 0 {
		t.Errorf("toasts %q", e.h.toasts)
	}
}

// TestBlockActionsWithoutService: on a message with no bot_id, only a
// widget: block's action goes out (to Slackbot, BSLACKBOT); a Slackbot
// message's goes out as BSLACKBOT when it has a quip_migration_feedback
// block, and otherwise not at all.
func TestBlockActionsWithoutService(t *testing.T) {
	e := newBKEnv(t, false, false)
	const blocks = `[{"type":"actions","block_id":%q,"elements":[{"type":"button","action_id":"go","text":{"type":"plain_text","text":"Go"},"value":"v"}]}]`
	for i, tc := range []struct {
		name, user, bid, want string
	}{
		{"widget", fakeslack.SelfID, "widget:pins", "BSLACKBOT"},
		{"person", fakeslack.SelfID, "plain", ""},
		{"quip feedback", "USLACKBOT", "quip_migration_feedback_survey", "BSLACKBOT"},
		{"slackbot", "USLACKBOT", "plain", ""},
	} {
		raw := json.RawMessage(fmt.Sprintf(blocks, tc.bid))
		ts := fmt.Sprintf("1795228900.%06d", i+1)
		sm := slack.Message{Type: "message", TS: ts, User: tc.user, Text: tc.name, Blocks: raw}
		m := core.Message{TS: ts, Channel: fakeslack.GalleryChannelID, User: tc.user, Text: tc.name, Blocks: raw,
			Doc: richtext.FromMessage(e.h.a.S, &sm)}
		e.msgs[tc.name] = m
		me := e.show(tc.name)
		n0 := len(e.fs.BlockActionCalls())
		e.clickEl(tc.bid, "go")
		if tc.want == "" {
			e.noNewCall(n0, 400*time.Millisecond, tc.name)
			if len(me.errs) != 0 {
				t.Errorf("%s: block errors %q, want none", tc.name, me.errs)
			}
			continue
		}
		cs := e.calls(n0 + 1)
		c := cs[len(cs)-1]
		if got := c.Form.Get("service_id"); got != tc.want {
			t.Errorf("%s: service_id %q, want %q", tc.name, got, tc.want)
		}
		if c.Container["type"] != "message" || c.Container["message_ts"] != ts {
			t.Errorf("%s: container %v", tc.name, c.Container)
		}
		e.idle(tc.bid)
	}
}
